package handlers

// ============================================================================
// WOOCOMMERCE ORDERS HANDLER
// ============================================================================
// Routes:
//   POST /woocommerce/orders/import    → pull orders from WooCommerce, save to MarketMate
//   GET  /woocommerce/orders           → list raw WooCommerce orders (debug)
//   POST /woocommerce/orders/:id/ship  → push tracking number to order
//   POST /woocommerce/orders/:id/status → update order status
// ============================================================================

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"module-a/marketplace/clients/woocommerce"
	"module-a/models"
	"module-a/services"
)

type WooCommerceOrdersHandler struct {
	orderService       *services.OrderService
	marketplaceService *services.MarketplaceService
}

func NewWooCommerceOrdersHandler(
	orderService *services.OrderService,
	marketplaceService *services.MarketplaceService,
) *WooCommerceOrdersHandler {
	return &WooCommerceOrdersHandler{
		orderService:       orderService,
		marketplaceService: marketplaceService,
	}
}

// ── Client resolution ─────────────────────────────────────────────────────────

func (h *WooCommerceOrdersHandler) buildClient(ctx context.Context, tenantID, credentialID string) (*woocommerce.Client, error) {
	cred, err := h.marketplaceService.GetCredential(ctx, tenantID, credentialID)
	if err != nil {
		return nil, fmt.Errorf("get credential: %w", err)
	}
	merged, err := h.marketplaceService.GetFullCredentials(ctx, cred)
	if err != nil {
		return nil, fmt.Errorf("merge credentials: %w", err)
	}

	storeURL := merged["store_url"]
	consumerKey := merged["consumer_key"]
	consumerSecret := merged["consumer_secret"]

	if storeURL == "" || consumerKey == "" || consumerSecret == "" {
		return nil, fmt.Errorf("incomplete WooCommerce credentials (store_url, consumer_key, consumer_secret required)")
	}

	return woocommerce.NewClient(storeURL, consumerKey, consumerSecret), nil
}

// ── ImportWooCommerceOrders ───────────────────────────────────────────────────

// ImportWooCommerceOrders is the core import function called by OrderHandler.processChannelImport.
func (h *WooCommerceOrdersHandler) ImportWooCommerceOrders(ctx context.Context, tenantID, credentialID string, createdAfter, createdBefore time.Time) (int, error) {
	log.Printf("[WooCommerce Orders] Import for tenant=%s cred=%s from=%s to=%s",
		tenantID, credentialID, createdAfter.Format("2006-01-02"), createdBefore.Format("2006-01-02"))

	client, err := h.buildClient(ctx, tenantID, credentialID)
	if err != nil {
		return 0, err
	}

	orders, err := client.FetchNewOrders(createdAfter, createdBefore, "processing")
	if err != nil {
		return 0, fmt.Errorf("fetch WooCommerce orders: %w", err)
	}

	log.Printf("[WooCommerce Orders] Fetched %d orders", len(orders))

	imported := 0
	for _, o := range orders {
		internal := convertWooOrder(o, credentialID)

		orderID, isNew, err := h.orderService.CreateOrder(ctx, tenantID, internal)
		if err != nil {
			log.Printf("[WooCommerce Orders] Failed to save order %d: %v", o.ID, err)
			continue
		}
		if !isNew {
			log.Printf("[WooCommerce Orders] Skipping duplicate order %d", o.ID)
			continue
		}

		for _, item := range o.LineItems {
			price := parseWooMoneyString(item.Price)
			total := parseWooMoneyString(item.Total)
			line := &models.OrderLine{
				LineID:    strconv.Itoa(item.ID),
				SKU:       item.SKU,
				Title:     item.Name,
				Quantity:  item.Quantity,
				UnitPrice: models.Money{Amount: price, Currency: o.Currency},
				LineTotal: models.Money{Amount: total, Currency: o.Currency},
			}
			if err := h.orderService.CreateOrderLine(ctx, tenantID, orderID, line); err != nil {
				log.Printf("[WooCommerce Orders] Failed to save line item %d: %v", item.ID, err)
			}
		}

		imported++
		log.Printf("[WooCommerce Orders] Imported order %d → %s", o.ID, orderID)
	}

	return imported, nil
}

// ── HTTP Handlers ─────────────────────────────────────────────────────────────

// TriggerImport handles POST /api/v1/woocommerce/orders/import
func (h *WooCommerceOrdersHandler) TriggerImport(c *gin.Context) {
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
			if cr.Channel == "woocommerce" && cr.Active {
				req.CredentialID = cr.CredentialID
				break
			}
		}
		if req.CredentialID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "no active WooCommerce credential found"})
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
		n, err := h.ImportWooCommerceOrders(ctx, tenantID, req.CredentialID, from, now)
		if err != nil {
			log.Printf("[WooCommerce Orders] Import error: %v", err)
		} else {
			log.Printf("[WooCommerce Orders] Import complete: %d orders imported", n)
		}
	}()
}

// ListOrders handles GET /api/v1/woocommerce/orders — raw WooCommerce orders for debugging.
func (h *WooCommerceOrdersHandler) ListOrders(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	credentialID := c.Query("credential_id")

	if credentialID == "" {
		creds, err := h.marketplaceService.ListCredentials(c.Request.Context(), tenantID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		for _, cr := range creds {
			if cr.Channel == "woocommerce" && cr.Active {
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

	status := c.Query("status")
	now := time.Now().UTC()
	from := now.Add(-time.Duration(hoursBack) * time.Hour)

	orders, err := client.FetchNewOrders(from, now, status)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"error": err.Error(), "orders": []interface{}{}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"orders": orders, "count": len(orders)})
}

// PushTracking handles POST /api/v1/woocommerce/orders/:id/ship
func (h *WooCommerceOrdersHandler) PushTracking(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	orderIDStr := c.Param("id")
	orderID, err := strconv.Atoi(orderIDStr)
	if err != nil || orderID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid order id"})
		return
	}

	var req struct {
		CredentialID   string `json:"credential_id"`
		TrackingNumber string `json:"tracking_number" binding:"required"`
		Carrier        string `json:"carrier"`
		TrackingURL    string `json:"tracking_url"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tracking_number is required"})
		return
	}

	if req.CredentialID == "" {
		creds, _ := h.marketplaceService.ListCredentials(c.Request.Context(), tenantID)
		for _, cr := range creds {
			if cr.Channel == "woocommerce" && cr.Active {
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

	if err := client.PushTracking(orderID, req.TrackingNumber, req.Carrier, req.TrackingURL); err != nil {
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

// UpdateOrderStatus handles POST /api/v1/woocommerce/orders/:id/status
func (h *WooCommerceOrdersHandler) UpdateOrderStatus(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	orderIDStr := c.Param("id")
	orderID, err := strconv.Atoi(orderIDStr)
	if err != nil || orderID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid order id"})
		return
	}

	var req struct {
		CredentialID string `json:"credential_id"`
		Status       string `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "status is required"})
		return
	}

	if req.CredentialID == "" {
		creds, _ := h.marketplaceService.ListCredentials(c.Request.Context(), tenantID)
		for _, cr := range creds {
			if cr.Channel == "woocommerce" && cr.Active {
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

	if err := client.UpdateOrderStatus(orderID, req.Status); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "order_id": orderID, "status": req.Status})
}

// ── Conversion helpers ────────────────────────────────────────────────────────

func convertWooOrder(o woocommerce.Order, credentialID string) *models.Order {
	status := mapWooOrderStatus(o.Status)

	shippingAddr := models.Address{
		Name:         o.Shipping.FirstName + " " + o.Shipping.LastName,
		AddressLine1: o.Shipping.Address1,
		AddressLine2: o.Shipping.Address2,
		City:         o.Shipping.City,
		State:        o.Shipping.State,
		PostalCode:   o.Shipping.Postcode,
		Country:      o.Shipping.Country,
	}
	if shippingAddr.Name == " " {
		shippingAddr.Name = o.Billing.FirstName + " " + o.Billing.LastName
	}

	grandTotal := parseWooMoneyString(o.Total)
	shipping := parseWooMoneyString(o.ShippingTotal)
	currency := o.Currency
	if currency == "" {
		currency = "GBP"
	}

	orderDate := o.DateCreated
	if orderDate == "" {
		orderDate = time.Now().UTC().Format(time.RFC3339)
	}

	order := &models.Order{
		ExternalOrderID:  strconv.Itoa(o.ID),
		ChannelAccountID: credentialID,
		Channel:          "woocommerce",
		Status:           status,
		PaymentStatus:    "captured",
		Customer: models.Customer{
			Name:  o.Billing.FirstName + " " + o.Billing.LastName,
			Email: o.Billing.Email,
			Phone: o.Billing.Phone,
		},
		ShippingAddress: shippingAddr,
		OrderDate:       orderDate,
		ImportedAt:      time.Now().UTC().Format(time.RFC3339),
		Totals: models.OrderTotals{
			Shipping:   models.Money{Amount: shipping, Currency: currency},
			GrandTotal: models.Money{Amount: grandTotal, Currency: currency},
		},
	}

	return order
}

func mapWooOrderStatus(wooStatus string) string {
	switch wooStatus {
	case "pending":
		return "pending_payment"
	case "processing":
		return "processing"
	case "on-hold":
		return "on_hold"
	case "completed":
		return "completed"
	case "cancelled":
		return "cancelled"
	case "refunded":
		return "refunded"
	case "failed":
		return "failed"
	default:
		return "imported"
	}
}

func parseWooMoneyString(s string) float64 {
	if s == "" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return f
}
