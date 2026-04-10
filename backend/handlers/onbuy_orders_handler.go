package handlers

// ============================================================================
// ONBUY ORDERS HANDLER
// ============================================================================
// Routes:
//   POST /onbuy/orders/import      → pull orders from OnBuy, save to MarketMate
//   GET  /onbuy/orders             → list raw OnBuy orders (debug/preview)
//   POST /onbuy/orders/:id/ship    → push tracking to OnBuy order (dispatch)
//   POST /onbuy/orders/:id/ack     → acknowledge OnBuy order
// ============================================================================

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"module-a/marketplace/clients/onbuy"
	"module-a/models"
	"module-a/services"
)

type OnBuyOrdersHandler struct {
	orderService       *services.OrderService
	marketplaceService *services.MarketplaceService
}

func NewOnBuyOrdersHandler(
	orderService *services.OrderService,
	marketplaceService *services.MarketplaceService,
) *OnBuyOrdersHandler {
	return &OnBuyOrdersHandler{
		orderService:       orderService,
		marketplaceService: marketplaceService,
	}
}

// ── Client resolution ─────────────────────────────────────────────────────────

func (h *OnBuyOrdersHandler) buildClient(ctx context.Context, tenantID, credentialID string) (*onbuy.Client, error) {
	cred, err := h.marketplaceService.GetCredential(ctx, tenantID, credentialID)
	if err != nil {
		return nil, fmt.Errorf("get credential: %w", err)
	}
	merged, err := h.marketplaceService.GetFullCredentials(ctx, cred)
	if err != nil {
		return nil, fmt.Errorf("merge credentials: %w", err)
	}

	consumerKey := merged["consumer_key"]
	consumerSecret := merged["consumer_secret"]
	siteIDStr := merged["site_id"]

	if consumerKey == "" || consumerSecret == "" {
		return nil, fmt.Errorf("incomplete OnBuy credentials (consumer_key and consumer_secret required)")
	}

	siteID := 2000
	if siteIDStr != "" {
		if v, err := strconv.Atoi(siteIDStr); err == nil && v > 0 {
			siteID = v
		}
	}

	return onbuy.NewClient(consumerKey, consumerSecret, siteID), nil
}

// ── ImportOnBuyOrders ─────────────────────────────────────────────────────────

// ImportOnBuyOrders is the core import function called by OrderHandler.processChannelImport.
func (h *OnBuyOrdersHandler) ImportOnBuyOrders(ctx context.Context, tenantID, credentialID string, createdAfter, createdBefore time.Time) (int, error) {
	log.Printf("[OnBuy Orders] Import for tenant=%s cred=%s from=%s to=%s",
		tenantID, credentialID, createdAfter.Format("2006-01-02"), createdBefore.Format("2006-01-02"))

	client, err := h.buildClient(ctx, tenantID, credentialID)
	if err != nil {
		return 0, err
	}

	orders, err := client.FetchNewOrders(createdAfter)
	if err != nil {
		return 0, fmt.Errorf("fetch OnBuy orders: %w", err)
	}

	log.Printf("[OnBuy Orders] Fetched %d orders", len(orders))

	// Acknowledge all fetched orders before processing
	if len(orders) > 0 {
		var orderIDs []string
		for _, o := range orders {
			orderIDs = append(orderIDs, o.OrderID)
		}
		if err := client.AcknowledgeOrders(orderIDs); err != nil {
			log.Printf("[OnBuy Orders] Warning: could not acknowledge orders: %v", err)
		}
	}

	imported := 0
	for _, o := range orders {
		internal := convertOnBuyOrder(o, credentialID)

		orderID, isNew, err := h.orderService.CreateOrder(ctx, tenantID, internal)
		if err != nil {
			log.Printf("[OnBuy Orders] Failed to save order %s: %v", o.OrderID, err)
			continue
		}
		if !isNew {
			log.Printf("[OnBuy Orders] Skipping duplicate order %s", o.OrderID)
			continue
		}

		currency := o.CurrencyCode
		if currency == "" {
			currency = "GBP"
		}

		for _, item := range o.Lines {
			line := &models.OrderLine{
				LineID:    item.OrderLineID,
				SKU:       item.SKU,
				Title:     item.ProductName,
				Quantity:  item.Quantity,
				UnitPrice: models.Money{Amount: item.UnitPrice, Currency: currency},
				LineTotal: models.Money{Amount: item.LineTotal, Currency: currency},
			}
			if err := h.orderService.CreateOrderLine(ctx, tenantID, orderID, line); err != nil {
				log.Printf("[OnBuy Orders] Failed to save line item %s: %v", item.OrderLineID, err)
			}
		}

		imported++
		log.Printf("[OnBuy Orders] Imported order %s → %s", o.OrderID, orderID)
	}

	return imported, nil
}

// ── HTTP Handlers ─────────────────────────────────────────────────────────────

// TriggerImport handles POST /api/v1/onbuy/orders/import
func (h *OnBuyOrdersHandler) TriggerImport(c *gin.Context) {
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
			if cr.Channel == "onbuy" && cr.Active {
				req.CredentialID = cr.CredentialID
				break
			}
		}
		if req.CredentialID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "no active OnBuy credential found"})
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
		n, err := h.ImportOnBuyOrders(ctx, tenantID, req.CredentialID, from, now)
		if err != nil {
			log.Printf("[OnBuy Orders] Import error: %v", err)
		} else {
			log.Printf("[OnBuy Orders] Import complete: %d orders imported", n)
		}
	}()
}

// ListOrders handles GET /api/v1/onbuy/orders — raw OnBuy orders for debugging.
func (h *OnBuyOrdersHandler) ListOrders(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	credentialID := c.Query("credential_id")

	if credentialID == "" {
		creds, err := h.marketplaceService.ListCredentials(c.Request.Context(), tenantID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		for _, cr := range creds {
			if cr.Channel == "onbuy" && cr.Active {
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

	status := c.DefaultQuery("status", "awaiting_dispatch")
	page := 1
	if p := c.Query("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v > 0 {
			page = v
		}
	}

	orders, total, err := client.GetOrders(status, page)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"error": err.Error(), "orders": []interface{}{}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"orders": orders, "count": len(orders), "total": total})
}

// PushTracking handles POST /api/v1/onbuy/orders/:id/ship
// :id is the OnBuy order ID string
func (h *OnBuyOrdersHandler) PushTracking(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	orderID := c.Param("id")
	if orderID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "order id is required"})
		return
	}

	var req struct {
		CredentialID   string `json:"credential_id"`
		TrackingNumber string `json:"tracking_number" binding:"required"`
		Carrier        string `json:"carrier"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tracking_number is required"})
		return
	}

	if req.CredentialID == "" {
		creds, _ := h.marketplaceService.ListCredentials(c.Request.Context(), tenantID)
		for _, cr := range creds {
			if cr.Channel == "onbuy" && cr.Active {
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

	tracking := onbuy.DispatchPayload{
		TrackingNumber: req.TrackingNumber,
		Carrier:        req.Carrier,
	}

	if err := client.DispatchOrder(orderID, tracking); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":              true,
		"order_id":        orderID,
		"tracking_number": req.TrackingNumber,
		"carrier":         req.Carrier,
	})
}

// AcknowledgeOrder handles POST /api/v1/onbuy/orders/:id/ack
func (h *OnBuyOrdersHandler) AcknowledgeOrder(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	orderID := c.Param("id")
	if orderID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "order id is required"})
		return
	}

	credentialID := c.Query("credential_id")
	if credentialID == "" {
		creds, _ := h.marketplaceService.ListCredentials(c.Request.Context(), tenantID)
		for _, cr := range creds {
			if cr.Channel == "onbuy" && cr.Active {
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

	if err := client.AcknowledgeOrders([]string{orderID}); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "order_id": orderID, "message": "Order acknowledged"})
}

// ── Conversion helpers ────────────────────────────────────────────────────────

func convertOnBuyOrder(o onbuy.Order, credentialID string) *models.Order {
	currency := o.CurrencyCode
	if currency == "" {
		currency = "GBP"
	}

	orderDate := o.DateCreated
	if orderDate == "" {
		orderDate = time.Now().UTC().Format(time.RFC3339)
	}

	addr := models.Address{
		Name:         o.DeliveryAddress.Name,
		AddressLine1: o.DeliveryAddress.Line1,
		AddressLine2: o.DeliveryAddress.Line2,
		City:         o.DeliveryAddress.Town,
		State:        o.DeliveryAddress.County,
		PostalCode:   o.DeliveryAddress.Postcode,
		Country:      o.DeliveryAddress.CountryCode,
	}

	return &models.Order{
		ExternalOrderID:  o.OrderID,
		ChannelAccountID: credentialID,
		Channel:          "onbuy",
		Status:           mapOnBuyOrderStatus(o.Status),
		PaymentStatus:    "captured",
		Customer: models.Customer{
			Name:  o.BuyerName,
			Email: o.BuyerEmail,
		},
		ShippingAddress: addr,
		OrderDate:       orderDate,
		ImportedAt:      time.Now().UTC().Format(time.RFC3339),
		Totals: models.OrderTotals{
			Shipping:   models.Money{Amount: o.DeliveryCost, Currency: currency},
			GrandTotal: models.Money{Amount: o.OrderTotal, Currency: currency},
		},
		InternalNotes: fmt.Sprintf("OnBuy Order #%s", o.OrderID),
	}
}

func mapOnBuyOrderStatus(status string) string {
	switch status {
	case "awaiting_dispatch":
		return "imported"
	case "dispatched":
		return "shipped"
	case "complete":
		return "completed"
	case "cancelled":
		return "cancelled"
	case "payment_pending":
		return "pending_payment"
	default:
		return "imported"
	}
}
