package handlers

// ============================================================================
// BIGCOMMERCE ORDERS HANDLER
// ============================================================================
// Routes:
//   POST /bigcommerce/orders/import     → pull orders from BigCommerce, save to MarketMate
//   GET  /bigcommerce/orders            → list raw BigCommerce orders (debug/preview)
//   POST /bigcommerce/orders/:id/ship   → push tracking to BigCommerce order
//   POST /bigcommerce/orders/:id/status → informational status update
// ============================================================================

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"module-a/marketplace/clients/bigcommerce"
	"module-a/models"
	"module-a/services"
)

type BigCommerceOrdersHandler struct {
	orderService       *services.OrderService
	marketplaceService *services.MarketplaceService
}

func NewBigCommerceOrdersHandler(
	orderService *services.OrderService,
	marketplaceService *services.MarketplaceService,
) *BigCommerceOrdersHandler {
	return &BigCommerceOrdersHandler{
		orderService:       orderService,
		marketplaceService: marketplaceService,
	}
}

// ── Client resolution ─────────────────────────────────────────────────────────

func (h *BigCommerceOrdersHandler) buildClient(ctx context.Context, tenantID, credentialID string) (*bigcommerce.Client, error) {
	cred, err := h.marketplaceService.GetCredential(ctx, tenantID, credentialID)
	if err != nil {
		return nil, fmt.Errorf("get credential: %w", err)
	}
	merged, err := h.marketplaceService.GetFullCredentials(ctx, cred)
	if err != nil {
		return nil, fmt.Errorf("merge credentials: %w", err)
	}

	storeHash := merged["store_hash"]
	clientID := merged["client_id"]
	accessToken := merged["access_token"]

	if storeHash == "" || accessToken == "" {
		return nil, fmt.Errorf("incomplete BigCommerce credentials (store_hash and access_token required)")
	}

	return bigcommerce.NewClient(storeHash, clientID, accessToken), nil
}

// ── ImportBigCommerceOrders ───────────────────────────────────────────────────

// ImportBigCommerceOrders is the core import function called by OrderHandler.processChannelImport.
func (h *BigCommerceOrdersHandler) ImportBigCommerceOrders(ctx context.Context, tenantID, credentialID string, createdAfter, createdBefore time.Time) (int, error) {
	log.Printf("[BigCommerce Orders] Import for tenant=%s cred=%s from=%s to=%s",
		tenantID, credentialID, createdAfter.Format("2006-01-02"), createdBefore.Format("2006-01-02"))

	client, err := h.buildClient(ctx, tenantID, credentialID)
	if err != nil {
		return 0, err
	}

	orders, err := client.FetchNewOrders(createdAfter)
	if err != nil {
		return 0, fmt.Errorf("fetch BigCommerce orders: %w", err)
	}

	log.Printf("[BigCommerce Orders] Fetched %d orders total", len(orders))

	imported := 0
	for _, o := range orders {
		// Fetch line items for this order
		products, err := client.GetOrderProducts(o.ID)
		if err != nil {
			log.Printf("[BigCommerce Orders] Warning: could not fetch products for order %d: %v", o.ID, err)
		}

		// Fetch shipping address
		var shippingAddr *bigcommerce.OrderAddress
		shippingAddrs, err := client.GetOrderShippingAddresses(o.ID)
		if err == nil && len(shippingAddrs) > 0 {
			shippingAddr = &shippingAddrs[0]
		}

		internal := convertBigCommerceOrder(o, shippingAddr, credentialID)

		orderID, isNew, err := h.orderService.CreateOrder(ctx, tenantID, internal)
		if err != nil {
			log.Printf("[BigCommerce Orders] Failed to save order %d: %v", o.ID, err)
			continue
		}
		if !isNew {
			log.Printf("[BigCommerce Orders] Skipping duplicate order %d", o.ID)
			continue
		}

		for _, item := range products {
			unitPrice := parsePrice(item.PriceIncTax)
			lineTotal := parsePrice(item.TotalIncTax)
			currency := o.CurrencyCode
			if currency == "" {
				currency = "GBP"
			}
			line := &models.OrderLine{
				LineID:    strconv.Itoa(item.ID),
				SKU:       item.SKU,
				Title:     item.Name,
				Quantity:  item.Quantity,
				UnitPrice: models.Money{Amount: unitPrice, Currency: currency},
				LineTotal: models.Money{Amount: lineTotal, Currency: currency},
			}
			if err := h.orderService.CreateOrderLine(ctx, tenantID, orderID, line); err != nil {
				log.Printf("[BigCommerce Orders] Failed to save line item %d: %v", item.ID, err)
			}
		}

		imported++
		log.Printf("[BigCommerce Orders] Imported order %d → %s", o.ID, orderID)
	}

	return imported, nil
}

// ── HTTP Handlers ─────────────────────────────────────────────────────────────

// TriggerImport handles POST /api/v1/bigcommerce/orders/import
func (h *BigCommerceOrdersHandler) TriggerImport(c *gin.Context) {
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

	if req.CredentialID == "" {
		creds, err := h.marketplaceService.ListCredentials(c.Request.Context(), tenantID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		for _, cr := range creds {
			if cr.Channel == "bigcommerce" && cr.Active {
				req.CredentialID = cr.CredentialID
				break
			}
		}
		if req.CredentialID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "no active BigCommerce credential found"})
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
		ctx := context.Background()
		n, err := h.ImportBigCommerceOrders(ctx, tenantID, req.CredentialID, from, now)
		if err != nil {
			log.Printf("[BigCommerce Orders] Import error: %v", err)
		} else {
			log.Printf("[BigCommerce Orders] Import complete: %d orders imported", n)
		}
	}()
}

// ListOrders handles GET /api/v1/bigcommerce/orders — raw BigCommerce orders for debugging.
func (h *BigCommerceOrdersHandler) ListOrders(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	credentialID := c.Query("credential_id")

	if credentialID == "" {
		creds, err := h.marketplaceService.ListCredentials(c.Request.Context(), tenantID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		for _, cr := range creds {
			if cr.Channel == "bigcommerce" && cr.Active {
				credentialID = cr.CredentialID
				break
			}
		}
	}

	client, err := h.buildClient(c.Request.Context(), tenantID, credentialID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	hoursBack := 48
	if h := c.Query("hours_back"); h != "" {
		if v, err := strconv.Atoi(h); err == nil {
			hoursBack = v
		}
	}

	now := time.Now().UTC()
	from := now.Add(-time.Duration(hoursBack) * time.Hour)

	orders, err := client.FetchNewOrders(from)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"error": err.Error(), "orders": []interface{}{}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"orders": orders, "count": len(orders)})
}

// PushTracking handles POST /api/v1/bigcommerce/orders/:id/ship
// :id is the BigCommerce order ID (integer)
func (h *BigCommerceOrdersHandler) PushTracking(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	orderIDStr := c.Param("id")
	orderID, err := strconv.Atoi(orderIDStr)
	if err != nil || orderID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid order id — must be a BigCommerce order integer ID"})
		return
	}

	var req struct {
		CredentialID    string `json:"credential_id"`
		TrackingNumber  string `json:"tracking_number" binding:"required"`
		Carrier         string `json:"carrier"`
		ShippingProvider string `json:"shipping_provider"`
		Comments        string `json:"comments"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tracking_number is required"})
		return
	}

	if req.CredentialID == "" {
		creds, _ := h.marketplaceService.ListCredentials(c.Request.Context(), tenantID)
		for _, cr := range creds {
			if cr.Channel == "bigcommerce" && cr.Active {
				req.CredentialID = cr.CredentialID
				break
			}
		}
	}

	client, err := h.buildClient(c.Request.Context(), tenantID, req.CredentialID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get the order's shipping addresses to find orderAddressID
	shippingAddrs, err := client.GetOrderShippingAddresses(orderID)
	if err != nil || len(shippingAddrs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "could not retrieve shipping address for order"})
		return
	}

	// Get order products to build shipment items
	products, err := client.GetOrderProducts(orderID)
	if err != nil || len(products) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "could not retrieve products for order"})
		return
	}

	// Build items list — ship all products in this order
	var items []bigcommerce.OrderShipmentLine
	for _, p := range products {
		items = append(items, bigcommerce.OrderShipmentLine{
			OrderProductID: p.ID,
			Quantity:       p.Quantity,
		})
	}

	shipmentReq := &bigcommerce.CreateShipmentRequest{
		TrackingNumber:   req.TrackingNumber,
		TrackingCarrier:  req.Carrier,
		ShippingProvider: req.ShippingProvider,
		Comments:         req.Comments,
		OrderAddressID:   shippingAddrs[0].ID,
		Items:            items,
	}

	shipment, err := client.CreateShipment(orderID, shipmentReq)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":              true,
		"shipment_id":     shipment.ID,
		"order_id":        orderID,
		"tracking_number": req.TrackingNumber,
		"carrier":         req.Carrier,
	})
}

// UpdateOrderStatus handles POST /api/v1/bigcommerce/orders/:id/status
// BigCommerce order status is updated automatically when a shipment is created.
func (h *BigCommerceOrdersHandler) UpdateOrderStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"message": "BigCommerce order status is updated automatically when a shipment is created via /ship. No separate status update is needed.",
	})
}

// ── Conversion helpers ────────────────────────────────────────────────────────

func convertBigCommerceOrder(o bigcommerce.Order, shippingAddr *bigcommerce.OrderAddress, credentialID string) *models.Order {
	status := mapBigCommerceOrderStatus(o.StatusID)
	currency := o.CurrencyCode
	if currency == "" {
		currency = "GBP"
	}

	orderDate := o.DateCreated
	if orderDate == "" {
		orderDate = time.Now().UTC().Format(time.RFC3339)
	}

	customerName := o.BillingAddress.FirstName + " " + o.BillingAddress.LastName
	customerEmail := o.BillingAddress.Email

	// Use shipping address if available
	addr := models.Address{}
	if shippingAddr != nil {
		name := shippingAddr.FirstName + " " + shippingAddr.LastName
		if name == " " {
			name = customerName
		}
		addr = models.Address{
			Name:         name,
			AddressLine1: shippingAddr.Street1,
			AddressLine2: shippingAddr.Street2,
			City:         shippingAddr.City,
			State:        shippingAddr.State,
			PostalCode:   shippingAddr.Zip,
			Country:      shippingAddr.CountryISO2,
		}
		if shippingAddr.Email != "" {
			customerEmail = shippingAddr.Email
		}
	} else {
		// Fall back to billing address
		addr = models.Address{
			Name:         customerName,
			AddressLine1: o.BillingAddress.Street1,
			AddressLine2: o.BillingAddress.Street2,
			City:         o.BillingAddress.City,
			State:        o.BillingAddress.State,
			PostalCode:   o.BillingAddress.Zip,
			Country:      o.BillingAddress.CountryISO2,
		}
	}

	grandTotal := parsePrice(o.TotalIncTax)
	shippingCost := parsePrice(o.ShippingCostIncTax)

	return &models.Order{
		ExternalOrderID:  strconv.Itoa(o.ID),
		ChannelAccountID: credentialID,
		Channel:          "bigcommerce",
		Status:           status,
		PaymentStatus:    mapBigCommercePaymentStatus(o.PaymentStatus),
		Customer: models.Customer{
			Name:  customerName,
			Email: customerEmail,
		},
		ShippingAddress: addr,
		OrderDate:       orderDate,
		ImportedAt:      time.Now().UTC().Format(time.RFC3339),
		Totals: models.OrderTotals{
			Shipping:   models.Money{Amount: shippingCost, Currency: currency},
			GrandTotal: models.Money{Amount: grandTotal, Currency: currency},
		},
		InternalNotes: fmt.Sprintf("BigCommerce Order #%d", o.ID),
	}
}

func mapBigCommerceOrderStatus(statusID int) string {
	switch statusID {
	case 1: // Pending
		return "pending_payment"
	case 5, 9: // Awaiting Payment / Awaiting Shipment
		return "processing"
	case 11: // Awaiting Fulfillment
		return "imported"
	case 2: // Shipped
		return "shipped"
	case 10: // Completed
		return "completed"
	case 4: // Cancelled
		return "cancelled"
	case 12: // Awaiting Pickup
		return "processing"
	case 6: // Declined
		return "cancelled"
	case 8: // Disputed
		return "on_hold"
	case 7: // Refunded
		return "refunded"
	default:
		return "imported"
	}
}

func mapBigCommercePaymentStatus(paymentStatus string) string {
	switch paymentStatus {
	case "authorized", "captured":
		return "captured"
	case "partially_refunded", "refunded":
		return "refunded"
	case "pending":
		return "pending"
	default:
		return "captured"
	}
}

// parsePrice converts a BigCommerce price string (e.g. "29.99") to float64.
func parsePrice(s string) float64 {
	if s == "" {
		return 0
	}
	f, _ := strconv.ParseFloat(s, 64)
	return f
}
