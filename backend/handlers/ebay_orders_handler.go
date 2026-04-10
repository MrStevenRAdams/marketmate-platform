package handlers

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"module-a/marketplace/clients/ebay"
	"module-a/models"
	"module-a/services"
)

type EbayOrdersHandler struct {
	orderService       *services.OrderService
	marketplaceService *services.MarketplaceService
	usage              *UsageInstrumentor
}

func NewEbayOrdersHandler(orderService *services.OrderService, marketplaceService *services.MarketplaceService) *EbayOrdersHandler {
	return &EbayOrdersHandler{
		orderService:       orderService,
		marketplaceService: marketplaceService,
	}
}

// ImportEbayOrders imports orders from eBay Fulfillment API
func (h *EbayOrdersHandler) ImportEbayOrders(ctx context.Context, tenantID, credentialID string, createdAfter, createdBefore time.Time) (int, error) {
	log.Printf("eBay order import requested for tenant %s, credential %s", tenantID, credentialID)
	log.Printf("Date range: %s to %s", createdAfter.Format("2006-01-02"), createdBefore.Format("2006-01-02"))
	
	// Get marketplace credentials
	cred, err := h.marketplaceService.GetCredential(ctx, tenantID, credentialID)
	if err != nil {
		return 0, fmt.Errorf("failed to get credentials: %w", err)
	}

	// Get merged credentials (global + tenant-specific)
	mergedCreds, err := h.marketplaceService.GetFullCredentials(ctx, cred)
	if err != nil {
		return 0, fmt.Errorf("failed to merge credentials: %w", err)
	}

	// Create eBay client
	client := ebay.NewClient(
		mergedCreds["client_id"],
		mergedCreds["client_secret"],
		mergedCreds["dev_id"],
		cred.Environment == "production",
	)
	
	// Set tokens
	client.AccessToken = mergedCreds["access_token"]
	client.RefreshToken = mergedCreds["refresh_token"]
	
	log.Printf("Fetching eBay orders from %s", createdAfter.Format(time.RFC3339))
	
	// Fetch orders using eBay Fulfillment API
	ordersResp, err := client.GetOrdersByCreationDate(createdAfter, 100)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch orders from eBay: %w", err)
	}

	log.Printf("Fetched %d orders from eBay", len(ordersResp.Orders))

	// Convert and save orders
	imported := 0
	for _, ebayOrder := range ordersResp.Orders {
		// Convert to internal order format
		order := h.convertEbayOrderToInternal(ebayOrder, credentialID)
		
		orderID, isNew, err := h.orderService.CreateOrder(ctx, tenantID, order)
		if err != nil {
			log.Printf("Failed to save order %s: %v", ebayOrder.OrderID, err)
			continue
		}

		if !isNew {
			log.Printf("Skipping duplicate eBay order %s (already exists as %s)", ebayOrder.OrderID, orderID)
			continue
		}

		// Save order line items
		for _, lineItem := range ebayOrder.LineItems {
			line := h.convertEbayLineItemToOrderLine(lineItem)
			if err := h.orderService.CreateOrderLine(ctx, tenantID, order.OrderID, line); err != nil {
				log.Printf("Failed to save line item %s: %v", lineItem.LineItemID, err)
			}
		}

		imported++
	}

	// Record usage — non-blocking
	if h.usage != nil && imported > 0 {
		h.usage.RecordOrderImport(context.Background(), tenantID, "ebay", imported, 0)
	}

	return imported, nil
}

// convertEbayOrderToInternal converts eBay order to internal format
func (h *EbayOrdersHandler) convertEbayOrderToInternal(ebayOrder ebay.Order, credentialID string) *models.Order {
	order := &models.Order{
		OrderID:          fmt.Sprintf("ebay_%s", ebayOrder.OrderID),
		Channel:          "ebay",
		ChannelAccountID: credentialID,
		ExternalOrderID:  ebayOrder.OrderID,
		OrderDate:        ebayOrder.CreationDate,
		Status:           mapEbayStatus(ebayOrder.OrderFulfillmentStatus),
		PaymentStatus:    mapEbayPaymentStatus(ebayOrder.OrderPaymentStatus),
		SLAAtRisk:        false,
	}

	// Map customer (buyer)
	if ebayOrder.Buyer != nil {
		order.Customer = models.Customer{
			Name:  ebayOrder.Buyer.Username,
		}
		
		if ebayOrder.Buyer.BuyerRegistrationAddress != nil && ebayOrder.Buyer.BuyerRegistrationAddress.FullName != "" {
			order.Customer.Name = ebayOrder.Buyer.BuyerRegistrationAddress.FullName
		}
	}

	// Map shipping address
	if len(ebayOrder.FulfillmentStartInstructions) > 0 {
		instruction := ebayOrder.FulfillmentStartInstructions[0]
		if instruction.ShippingStep != nil && instruction.ShippingStep.ShipTo != nil {
			shipTo := instruction.ShippingStep.ShipTo
			address := models.Address{}
			
			if shipTo.FullName != "" {
				address.Name = shipTo.FullName
			}
			
			if shipTo.ContactAddress != nil {
				address.AddressLine1 = shipTo.ContactAddress.AddressLine1
				address.AddressLine2 = shipTo.ContactAddress.AddressLine2
				address.City = shipTo.ContactAddress.City
				address.State = shipTo.ContactAddress.StateOrProvince
				address.PostalCode = shipTo.ContactAddress.PostalCode
				address.Country = shipTo.ContactAddress.CountryCode
			}
			
			order.ShippingAddress = address
		}
	}

	// Map totals
	if ebayOrder.PricingSummary != nil {
		order.Totals = models.OrderTotals{}
		
		if ebayOrder.PricingSummary.Total != nil {
			var amount float64
			fmt.Sscanf(ebayOrder.PricingSummary.Total.Value, "%f", &amount)
			order.Totals.GrandTotal = models.Money{
				Amount:   amount,
				Currency: ebayOrder.PricingSummary.Total.Currency,
			}
		}
		
		if ebayOrder.PricingSummary.Subtotal != nil {
			var amount float64
			fmt.Sscanf(ebayOrder.PricingSummary.Subtotal.Value, "%f", &amount)
			order.Totals.Subtotal = models.Money{
				Amount:   amount,
				Currency: ebayOrder.PricingSummary.Subtotal.Currency,
			}
		}
		
		if ebayOrder.PricingSummary.DeliveryCost != nil {
			var amount float64
			fmt.Sscanf(ebayOrder.PricingSummary.DeliveryCost.Value, "%f", &amount)
			order.Totals.Shipping = models.Money{
				Amount:   amount,
				Currency: ebayOrder.PricingSummary.DeliveryCost.Currency,
			}
		}
		
		if ebayOrder.PricingSummary.Tax != nil {
			var amount float64
			fmt.Sscanf(ebayOrder.PricingSummary.Tax.Value, "%f", &amount)
			order.Totals.Tax = models.Money{
				Amount:   amount,
				Currency: ebayOrder.PricingSummary.Tax.Currency,
			}
		}
	}

	return order
}

// convertEbayLineItemToOrderLine converts eBay line item to internal format
func (h *EbayOrdersHandler) convertEbayLineItemToOrderLine(item ebay.LineItem) *models.OrderLine {
	line := &models.OrderLine{
		LineID:         item.LineItemID,
		SKU:            item.SKU,
		Title:          item.Title,
		Quantity:       item.Quantity,
		Status:         mapEbayLineItemStatus(item.LineItemFulfillmentStatus),
		FulfilmentType: "stock",
	}

	// Map pricing
	if item.LineItemCost != nil {
		var amount float64
		fmt.Sscanf(item.LineItemCost.Value, "%f", &amount)
		line.UnitPrice = models.Money{
			Amount:   amount,
			Currency: item.LineItemCost.Currency,
		}
	}

	if item.Total != nil {
		var amount float64
		fmt.Sscanf(item.Total.Value, "%f", &amount)
		line.LineTotal = models.Money{
			Amount:   amount,
			Currency: item.Total.Currency,
		}
	}

	if item.Tax != nil {
		var taxAmount float64
		fmt.Sscanf(item.Tax.Value, "%f", &taxAmount)
		line.Tax = models.Money{
			Amount:   taxAmount,
			Currency: item.Tax.Currency,
		}
	}

	return line
}

// TriggerImport handles POST /api/v1/ebay/orders/import
// Follows the same pattern as TikTok, Etsy, WooCommerce etc.
func (h *EbayOrdersHandler) TriggerImport(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req struct {
		CredentialID string `json:"credential_id"`
		HoursBack    int    `json:"hours_back"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}
	if req.HoursBack <= 0 {
		req.HoursBack = 24
	}
	if req.HoursBack > 720 {
		req.HoursBack = 720
	}

	// Auto-resolve credential if not specified.
	if req.CredentialID == "" {
		creds, err := h.marketplaceService.ListCredentials(c.Request.Context(), tenantID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		for _, cr := range creds {
			if cr.Channel == "ebay" && cr.Active {
				req.CredentialID = cr.CredentialID
				break
			}
		}
		if req.CredentialID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "no active eBay credential found"})
			return
		}
	}

	now := time.Now().UTC()
	from := now.Add(-time.Duration(req.HoursBack) * time.Hour)

	c.JSON(http.StatusAccepted, gin.H{
		"status":        "started",
		"hours_back":    req.HoursBack,
		"from":          from.Format(time.RFC3339),
		"credential_id": req.CredentialID,
	})

	go func() {
		n, err := h.ImportEbayOrders(context.Background(), tenantID, req.CredentialID, from, now)
		if err != nil {
			log.Printf("[eBay Orders] TriggerImport error: %v", err)
		} else {
			log.Printf("[eBay Orders] TriggerImport complete: %d orders imported", n)
		}
	}()
}

// Helper functions
func mapEbayStatus(ebayStatus string) string {
	statusMap := map[string]string{
		"NOT_STARTED":    "imported",
		"IN_PROGRESS":    "processing",
		"FULFILLED":      "fulfilled",
		"PARTIALLY_FULFILLED": "processing",
	}
	
	if status, ok := statusMap[ebayStatus]; ok {
		return status
	}
	return "imported"
}

func mapEbayPaymentStatus(paymentStatus string) string {
	statusMap := map[string]string{
		"PAID":    "captured",
		"PENDING": "pending",
		"FAILED":  "failed",
	}
	
	if status, ok := statusMap[paymentStatus]; ok {
		return status
	}
	return "pending"
}

func mapEbayLineItemStatus(lineItemStatus string) string {
	statusMap := map[string]string{
		"NOT_STARTED": "pending",
		"IN_PROGRESS": "processing",
		"FULFILLED":   "fulfilled",
	}
	
	if status, ok := statusMap[lineItemStatus]; ok {
		return status
	}
	return "pending"
}
