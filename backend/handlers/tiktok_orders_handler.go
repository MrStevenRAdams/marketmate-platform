package handlers

// ============================================================================
// TIKTOK SHOP ORDERS HANDLER
// ============================================================================
// Routes:
//   POST /tiktok/orders/import       → pull orders from TikTok, save to MarketMate
//   GET  /tiktok/orders              → list raw orders from TikTok (debug/manual)
//   POST /tiktok/orders/:id/ship     → confirm shipment + tracking
//   POST /tiktok/orders/:id/cancel   → cancel an order
//   GET  /tiktok/orders/reasons      → available cancel reasons
// ============================================================================

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"module-a/marketplace/clients/tiktok"
	"module-a/models"
	"module-a/services"
)

type TikTokOrdersHandler struct {
	orderService       *services.OrderService
	marketplaceService *services.MarketplaceService
}

func NewTikTokOrdersHandler(
	orderService *services.OrderService,
	marketplaceService *services.MarketplaceService,
) *TikTokOrdersHandler {
	return &TikTokOrdersHandler{
		orderService:       orderService,
		marketplaceService: marketplaceService,
	}
}

// ── Client resolution ─────────────────────────────────────────────────────────

func (h *TikTokOrdersHandler) buildClient(ctx context.Context, tenantID, credentialID string) (*tiktok.Client, error) {
	cred, err := h.marketplaceService.GetCredential(ctx, tenantID, credentialID)
	if err != nil {
		return nil, fmt.Errorf("get credential: %w", err)
	}
	merged, err := h.marketplaceService.GetFullCredentials(ctx, cred)
	if err != nil {
		return nil, fmt.Errorf("merge credentials: %w", err)
	}

	appKey := merged["app_key"]
	appSecret := merged["app_secret"]
	accessToken := merged["access_token"]
	shopID := merged["shop_id"]

	if appKey == "" || appSecret == "" || accessToken == "" {
		return nil, fmt.Errorf("incomplete TikTok credentials (app_key, app_secret, access_token required)")
	}

	return tiktok.NewClient(appKey, appSecret, accessToken, shopID), nil
}

// ── ImportTikTokOrders ────────────────────────────────────────────────────────

// ImportTikTokOrders is the core import function called by OrderHandler.processChannelImport.
func (h *TikTokOrdersHandler) ImportTikTokOrders(ctx context.Context, tenantID, credentialID string, createdAfter, createdBefore time.Time) (int, error) {
	log.Printf("[TikTok Orders] Import for tenant=%s cred=%s from=%s to=%s",
		tenantID, credentialID, createdAfter.Format("2006-01-02"), createdBefore.Format("2006-01-02"))

	client, err := h.buildClient(ctx, tenantID, credentialID)
	if err != nil {
		return 0, err
	}

	// Fetch all orders updated in the window — AWAITING_SHIPMENT is the primary state to action
	orders, err := client.FetchNewOrders(
		createdAfter.Unix(),
		createdBefore.Unix(),
		tiktok.OrderStatusAwaitingShip,
	)
	if err != nil {
		return 0, fmt.Errorf("fetch TikTok orders: %w", err)
	}

	log.Printf("[TikTok Orders] Fetched %d orders", len(orders))

	imported := 0
	for _, o := range orders {
		internal := convertTikTokOrder(o, credentialID)

		orderID, isNew, err := h.orderService.CreateOrder(ctx, tenantID, internal)
		if err != nil {
			log.Printf("[TikTok Orders] Failed to save order %s: %v", o.ID, err)
			continue
		}
		if !isNew {
			log.Printf("[TikTok Orders] Skipping duplicate order %s", o.ID)
			continue
		}

		// Save line items
		for _, item := range o.LineItems {
			line := &models.OrderLine{
				LineID:    item.ID,
				SKU:       item.SKU,
				Title:         item.ProductName,
				Quantity:      item.Quantity,
				UnitPrice: models.Money{
					Amount:   parseMoneyString(item.SalePrice),
					Currency: o.PaymentInfo.Currency,
				},
				LineTotal: models.Money{
					Amount:   parseMoneyString(item.SalePrice) * float64(item.Quantity),
					Currency: o.PaymentInfo.Currency,
				},
			}
			if err := h.orderService.CreateOrderLine(ctx, tenantID, orderID, line); err != nil {
				log.Printf("[TikTok Orders] Failed to save line item %s: %v", item.ID, err)
			}
		}

		imported++
		log.Printf("[TikTok Orders] Imported order %s → %s", o.ID, orderID)
	}

	return imported, nil
}

// ── HTTP Handlers ─────────────────────────────────────────────────────────────

// TriggerImport handles POST /api/v1/tiktok/orders/import
func (h *TikTokOrdersHandler) TriggerImport(c *gin.Context) {
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

	// Resolve credential if not specified
	if req.CredentialID == "" {
		creds, err := h.marketplaceService.ListCredentials(c.Request.Context(), tenantID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		for _, cr := range creds {
			if cr.Channel == "tiktok" && cr.Active {
				req.CredentialID = cr.CredentialID
				break
			}
		}
		if req.CredentialID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "no active TikTok credential found"})
			return
		}
	}

	now := time.Now().UTC()
	from := now.Add(-time.Duration(req.HoursBack) * time.Hour)

	c.JSON(http.StatusAccepted, gin.H{
		"status":       "started",
		"hours_back":   req.HoursBack,
		"from":         from.Format(time.RFC3339),
		"credential_id": req.CredentialID,
	})

	// Run import asynchronously
	go func() {
		ctx := context.Background()
		n, err := h.ImportTikTokOrders(ctx, tenantID, req.CredentialID, from, now)
		if err != nil {
			log.Printf("[TikTok Orders] Import error: %v", err)
		} else {
			log.Printf("[TikTok Orders] Import complete: %d orders imported", n)
		}
	}()
}

// ListOrders handles GET /api/v1/tiktok/orders — raw TikTok orders for debugging.
func (h *TikTokOrdersHandler) ListOrders(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	credentialID := c.Query("credential_id")

	if credentialID == "" {
		creds, err := h.marketplaceService.ListCredentials(c.Request.Context(), tenantID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		for _, cr := range creds {
			if cr.Channel == "tiktok" && cr.Active {
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

	orders, err := client.FetchNewOrders(from.Unix(), now.Unix(), status)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"error": err.Error(), "orders": []interface{}{}})
		return
	}

	c.JSON(http.StatusOK, gin.H{"orders": orders, "count": len(orders)})
}

// PushTracking handles POST /api/v1/tiktok/orders/:id/ship
func (h *TikTokOrdersHandler) PushTracking(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	orderID := c.Param("id")

	var req struct {
		CredentialID   string `json:"credential_id"`
		PackageID      string `json:"package_id"`
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
			if cr.Channel == "tiktok" && cr.Active {
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

	if err := client.PushTracking(orderID, req.PackageID, req.TrackingNumber, req.Carrier); err != nil {
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

// CancelOrder handles POST /api/v1/tiktok/orders/:id/cancel
func (h *TikTokOrdersHandler) CancelOrder(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	orderID := c.Param("id")

	var req struct {
		CredentialID  string `json:"credential_id"`
		CancelReason  string `json:"cancel_reason"`
		CancelReasonKey string `json:"cancel_reason_key"`
	}
	c.ShouldBindJSON(&req)

	if req.CredentialID == "" {
		creds, _ := h.marketplaceService.ListCredentials(c.Request.Context(), tenantID)
		for _, cr := range creds {
			if cr.Channel == "tiktok" && cr.Active {
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

	if err := client.CancelOrder(orderID, req.CancelReason, req.CancelReasonKey); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "order_id": orderID})
}

// GetCancelReasons handles GET /api/v1/tiktok/orders/reasons
func (h *TikTokOrdersHandler) GetCancelReasons(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	creds, _ := h.marketplaceService.ListCredentials(c.Request.Context(), tenantID)
	credentialID := ""
	for _, cr := range creds {
		if cr.Channel == "tiktok" && cr.Active {
			credentialID = cr.CredentialID
			break
		}
	}
	if credentialID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no TikTok credential found"})
		return
	}

	client, err := h.buildClient(c.Request.Context(), tenantID, credentialID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	reasons, err := client.GetCancelReasons()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error(), "reasons": []interface{}{}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "reasons": reasons})
}

// ── Conversion helpers ────────────────────────────────────────────────────────

func convertTikTokOrder(o tiktok.Order, credentialID string) *models.Order {
	status := mapTikTokOrderStatus(o.Status)

	shippingAddr := models.Address{
		Name:         o.ShippingAddress.FullName,
		AddressLine1: o.ShippingAddress.AddressLine1,
		AddressLine2: o.ShippingAddress.AddressLine2,
		City:         o.ShippingAddress.City,
		State:        o.ShippingAddress.State,
		PostalCode:   o.ShippingAddress.PostalCode,
		Country:      o.ShippingAddress.CountryCode,
	}

	grandTotal := parseMoneyString(o.PaymentInfo.TotalAmount)
	shipping := parseMoneyString(o.PaymentInfo.ShippingFee)
	currency := o.PaymentInfo.Currency
	if currency == "" {
		currency = o.Currency
	}

	order := &models.Order{
		ExternalOrderID:  o.ID,
		ChannelAccountID: credentialID,
		Channel:          "tiktok",
		Status:           status,
		PaymentStatus:    "captured",
		Customer: models.Customer{
			Name:  o.ShippingAddress.FullName,
			Phone: o.ShippingAddress.PhoneNumber,
		},
		ShippingAddress: shippingAddr,
		OrderDate:       time.Unix(o.CreateTime, 0).Format(time.RFC3339),
		ImportedAt:      time.Now().UTC().Format(time.RFC3339),
		Totals: models.OrderTotals{
			Shipping:   models.Money{Amount: shipping, Currency: currency},
			GrandTotal: models.Money{Amount: grandTotal, Currency: currency},
		},
	}

	return order
}

func mapTikTokOrderStatus(tikTokStatus string) string {
	switch tikTokStatus {
	case tiktok.OrderStatusUnpaid:
		return "pending_payment"
	case tiktok.OrderStatusOnHold:
		return "on_hold"
	case tiktok.OrderStatusAwaitingShip:
		return "processing"
	case tiktok.OrderStatusAwaitingColl:
		return "ready_to_ship"
	case tiktok.OrderStatusInTransit:
		return "shipped"
	case tiktok.OrderStatusDelivered:
		return "delivered"
	case tiktok.OrderStatusCompleted:
		return "completed"
	case tiktok.OrderStatusCancelled:
		return "cancelled"
	default:
		return "imported"
	}
}

func parseMoneyString(s string) float64 {
	if s == "" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return f
}
