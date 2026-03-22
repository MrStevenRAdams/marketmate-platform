package handlers

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"module-a/marketplace/clients/temu"
	"module-a/models"
	"module-a/services"
)

type TemuOrdersHandler struct {
	orderService       *services.OrderService
	marketplaceService *services.MarketplaceService
	usage              *UsageInstrumentor
}

func NewTemuOrdersHandler(orderService *services.OrderService, marketplaceService *services.MarketplaceService) *TemuOrdersHandler {
	return &TemuOrdersHandler{
		orderService:       orderService,
		marketplaceService: marketplaceService,
	}
}

// ImportTemuOrders imports orders from Temu API (V2)
func (h *TemuOrdersHandler) ImportTemuOrders(ctx context.Context, tenantID, credentialID string, createdAfter, createdBefore time.Time) (int, error) {
	log.Printf("Temu order import requested for tenant %s, credential %s", tenantID, credentialID)
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

	// Get base URL from credentials (same as working Temu handler)
	baseURL := mergedCreds["base_url"]
	if baseURL == "" {
		// Fallback to region-based URL if not in credentials
		if region, ok := mergedCreds["region"]; ok && region == "EU" {
			baseURL = temu.TemuBaseURLEU
		} else {
			baseURL = temu.TemuBaseURLUS
		}
	}

	// Determine region ID based on base URL
	regionID := 211 // Default US
	if strings.Contains(baseURL, "-eu.temu.com") {
		regionID = 1 // EU region
	}
	
	log.Printf("Using Temu base URL: %s, region: %d", baseURL, regionID)

	// Create Temu client
	client := temu.NewClient(
		baseURL,
		mergedCreds["app_key"],
		mergedCreds["app_secret"],
		mergedCreds["access_token"],
	)
	
	log.Printf("Fetching Temu orders from %s (V2 API), region: %d", createdAfter.Format(time.RFC3339), regionID)
	
	// Fetch orders using Temu V2 API
	ordersResp, err := client.GetOrders(temu.OrdersRequest{
		PageNumber:        1,
		PageSize:          100,
		CreateAfter:       createdAfter.Unix(),
		CreateBefore:      createdBefore.Unix(),
		ParentOrderStatus: 2, // 2 = To be shipped (unshipped orders)
		RegionID:          regionID,
	})
	if err != nil {
		log.Printf("ERROR: Failed to fetch Temu orders: %v", err)
		return 0, fmt.Errorf("failed to fetch orders from Temu: %w", err)
	}

	log.Printf("Fetched %d orders from Temu", len(ordersResp.PageItems))

	// Convert and save orders
	imported := 0
	for _, pageItem := range ordersResp.PageItems {
		log.Printf("Processing Temu order: %s", pageItem.ParentOrderMap.ParentOrderSn)
		
		// Fetch shipping info for this order
		shippingInfo, err := client.GetShippingInfo(pageItem.ParentOrderMap.ParentOrderSn)
		if err != nil {
			log.Printf("WARNING: Failed to fetch shipping info for %s: %v", pageItem.ParentOrderMap.ParentOrderSn, err)
			// Continue without shipping info
			shippingInfo = nil
		}

		// Convert to internal order format
		order := h.convertTemuOrderToInternal(pageItem.ParentOrderMap, shippingInfo, credentialID)
		
		log.Printf("Saving Temu order: %s (internal ID: %s)", pageItem.ParentOrderMap.ParentOrderSn, order.OrderID)
		
		orderID, isNew, err := h.orderService.CreateOrder(ctx, tenantID, order)
		if err != nil {
			log.Printf("ERROR: Failed to save order %s: %v", pageItem.ParentOrderMap.ParentOrderSn, err)
			continue
		}

		if !isNew {
			log.Printf("Skipping duplicate Temu order %s (already exists as %s)", pageItem.ParentOrderMap.ParentOrderSn, orderID)
			continue
		}

		// Save order line items
		log.Printf("Saving %d line items for order %s", len(pageItem.OrderList), order.OrderID)
		for _, lineItem := range pageItem.OrderList {
			line := h.convertTemuLineItemToOrderLine(lineItem)
			if err := h.orderService.CreateOrderLine(ctx, tenantID, order.OrderID, line); err != nil {
				log.Printf("ERROR: Failed to save line item %s: %v", lineItem.OrderSN, err)
			}
		}

		imported++
		log.Printf("Successfully imported Temu order %s (%d/%d)", pageItem.ParentOrderMap.ParentOrderSn, imported, len(ordersResp.PageItems))
	}

	log.Printf("Temu import complete: %d orders imported successfully", imported)

	// Record usage — non-blocking
	if h.usage != nil && imported > 0 {
		h.usage.RecordOrderImport(context.Background(), tenantID, "temu", imported, 0)
	}

	return imported, nil
}

// convertTemuOrderToInternal converts Temu V2 order to internal format
func (h *TemuOrdersHandler) convertTemuOrderToInternal(temuOrder temu.Order, shippingInfo *temu.ShippingInfo, credentialID string) *models.Order {
	order := &models.Order{
		OrderID:          fmt.Sprintf("temu_%s", temuOrder.ParentOrderSn),
		Channel:          "temu",
		ChannelAccountID: credentialID,
		ExternalOrderID:  temuOrder.ParentOrderSn,
		OrderDate:        time.Unix(temuOrder.ParentOrderTime, 0).Format(time.RFC3339),
		Status:           mapTemuStatus(temuOrder.ParentOrderStatus),
		PaymentStatus:    "captured", // Temu orders are pre-paid
		SLAAtRisk:        len(temuOrder.FulfillmentWarning) > 0,
	}

	// Map customer from shipping info
	if shippingInfo != nil {
		customerName := shippingInfo.ReceiptName
		if shippingInfo.AddressExtra != nil {
			customerName = fmt.Sprintf("%s %s", shippingInfo.AddressExtra.FirstName, shippingInfo.AddressExtra.LastName)
		}
		
		order.Customer = models.Customer{
			Name:  customerName,
			Email: shippingInfo.Mail,
			Phone: shippingInfo.Mobile,
		}

		// Map shipping address
		order.ShippingAddress = models.Address{
			Name:         shippingInfo.ReceiptName,
			AddressLine1: shippingInfo.AddressLine1,
			AddressLine2: shippingInfo.AddressLine2,
			City:         shippingInfo.RegionName3,
			State:        shippingInfo.RegionName2,
			PostalCode:   shippingInfo.PostCode,
			Country:      shippingInfo.RegionName1,
		}
	}

	// Temu doesn't provide order totals in list API - we'd need amount query API
	// For now, calculate from line items when they're saved
	order.Totals = models.OrderTotals{}

	return order
}

// convertTemuLineItemToOrderLine converts Temu V2 line item to internal format
func (h *TemuOrdersHandler) convertTemuLineItemToOrderLine(item temu.OrderLine) *models.OrderLine {
	line := &models.OrderLine{
		LineID:            item.OrderSN,
		SKU:               fmt.Sprintf("%d", item.SkuID),
		Title:             item.GoodsName,
		Quantity:          item.Quantity,
		FulfilledQuantity: item.Quantity - item.CanceledQuantityBeforeShipment,
		Status:            mapTemuLineItemStatus(item.OrderStatus),
		FulfilmentType:    item.FulfillmentType,
	}

	// Temu doesn't provide pricing in line items - would need amount query API
	// For now, leave pricing empty or calculate from order total

	return line
}

// Helper functions
func mapTemuStatus(parentOrderStatus int) string {
	// Temu parent order status codes (from docs)
	// Specific codes would need verification from API response
	statusMap := map[int]string{
		0: "imported",      // Pending
		1: "processing",    // Confirmed
		2: "processing",    // In fulfillment
		3: "fulfilled",     // Shipped
		4: "fulfilled",     // Delivered
		5: "cancelled",     // Cancelled
	}
	
	if status, ok := statusMap[parentOrderStatus]; ok {
		return status
	}
	return "imported"
}

func mapTemuLineItemStatus(orderStatus int) string {
	// Temu line item status codes
	statusMap := map[int]string{
		0: "pending",
		1: "processing",
		2: "processing",
		3: "fulfilled",
		4: "fulfilled",
		5: "cancelled",
	}
	
	if status, ok := statusMap[orderStatus]; ok {
		return status
	}
	return "pending"
}
