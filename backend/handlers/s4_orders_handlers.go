package handlers

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"module-a/marketplace/clients/bol"
	"module-a/marketplace/clients/lazada"
	"module-a/marketplace/clients/zalando"
	"module-a/models"
	"module-a/services"
)

// ============================================================================
// ZALANDO
// ============================================================================

type ZalandoOrdersHandler struct {
	orderService       *services.OrderService
	marketplaceService *services.MarketplaceService
}

func NewZalandoOrdersHandler(orderService *services.OrderService, marketplaceService *services.MarketplaceService) *ZalandoOrdersHandler {
	return &ZalandoOrdersHandler{orderService: orderService, marketplaceService: marketplaceService}
}

func (h *ZalandoOrdersHandler) buildClient(ctx context.Context, tenantID, credentialID string) (*zalando.Client, *models.MarketplaceCredential, error) {
	cred, err := h.marketplaceService.GetCredential(ctx, tenantID, credentialID)
	if err != nil {
		return nil, nil, fmt.Errorf("get credential: %w", err)
	}
	merged, err := h.marketplaceService.GetFullCredentials(ctx, cred)
	if err != nil {
		return nil, nil, fmt.Errorf("merge credentials: %w", err)
	}
	if merged["client_id"] == "" || merged["client_secret"] == "" {
		return nil, nil, fmt.Errorf("incomplete Zalando credentials")
	}
	return zalando.NewClient(merged["client_id"], merged["client_secret"], cred.Environment == "production"), cred, nil
}

func (h *ZalandoOrdersHandler) ImportZalandoOrders(ctx context.Context, tenantID, credentialID string, createdAfter, createdBefore time.Time) (int, error) {
	log.Printf("[Zalando Orders] Import tenant=%s cred=%s since=%s", tenantID, credentialID, createdAfter.Format(time.RFC3339))
	client, cred, err := h.buildClient(ctx, tenantID, credentialID)
	if err != nil {
		return 0, err
	}
	orders, err := client.GetOrders(createdAfter)
	if err != nil {
		return 0, fmt.Errorf("fetch Zalando orders: %w", err)
	}
	imported := 0
	for _, o := range orders {
		currency := o.Currency
		if currency == "" {
			currency = "EUR"
		}
		internal := &models.Order{
			TenantID:         tenantID,
			Channel:          "zalando",
			ChannelAccountID: credentialID,
			ExternalOrderID:  o.OrderID,
			Status:           mapZalandoStatus(o.Status),
			PaymentStatus:    "captured",
			OrderDate:        o.OrderDate,
			Customer:         models.Customer{Name: o.BillingAddr.Name},
			ShippingAddress: models.Address{
				Name:         o.DeliveryAddr.Name,
				AddressLine1: o.DeliveryAddr.AddressLine1,
				AddressLine2: o.DeliveryAddr.AddressLine2,
				City:         o.DeliveryAddr.City,
				PostalCode:   o.DeliveryAddr.PostalCode,
				Country:      o.DeliveryAddr.CountryCode,
			},
			BillingAddress: &models.Address{
				Name:         o.BillingAddr.Name,
				AddressLine1: o.BillingAddr.AddressLine1,
				AddressLine2: o.BillingAddr.AddressLine2,
				City:         o.BillingAddr.City,
				PostalCode:   o.BillingAddr.PostalCode,
				Country:      o.BillingAddr.CountryCode,
			},
			Totals: models.OrderTotals{
				GrandTotal: models.Money{Amount: o.TotalAmount, Currency: currency},
				Shipping:   models.Money{Amount: o.ShippingCost, Currency: currency},
			},
			InternalNotes: fmt.Sprintf("Zalando Order #%s", o.OrderID),
		}
		ApplyOrderImportConfig(internal, cred.Config.Orders)
		orderID, isNew, err := h.orderService.CreateOrder(ctx, tenantID, internal)
		if err != nil {
			log.Printf("[Zalando Orders] Failed to save order %s: %v", o.OrderID, err)
			continue
		}
		if !isNew {
			continue
		}
		for _, item := range o.LineItems {
			line := &models.OrderLine{
				LineID:    item.LineItemID,
				SKU:       item.EAN,
				Title:     item.Name,
				Quantity:  item.Quantity,
				UnitPrice: models.Money{Amount: item.UnitPrice, Currency: currency},
				LineTotal: models.Money{Amount: item.UnitPrice * float64(item.Quantity), Currency: currency},
				Status:    "pending",
			}
			if err := h.orderService.CreateOrderLine(ctx, tenantID, orderID, line); err != nil {
				log.Printf("[Zalando Orders] Failed to save line %s: %v", item.LineItemID, err)
			}
		}
		imported++
	}
	return imported, nil
}

func (h *ZalandoOrdersHandler) TriggerImport(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	var req struct {
		CredentialID string `json:"credential_id" binding:"required"`
		HoursBack    int    `json:"hours_back"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.HoursBack <= 0 {
		req.HoursBack = 48
	}
	now := time.Now().UTC()
	from := now.Add(-time.Duration(req.HoursBack) * time.Hour)
	c.JSON(http.StatusAccepted, gin.H{"status": "started", "from": from.Format(time.RFC3339)})
	go func() {
		ctx := context.Background()
		n, err := h.ImportZalandoOrders(ctx, tenantID, req.CredentialID, from, now)
		if err != nil {
			log.Printf("[Zalando Orders] Import error: %v", err)
		} else {
			log.Printf("[Zalando Orders] Import complete: %d orders", n)
		}
	}()
}

func (h *ZalandoOrdersHandler) ListOrders(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	opts := services.OrderListOptions{Channel: "zalando", Limit: c.DefaultQuery("limit", "50"), Offset: c.DefaultQuery("offset", "0")}
	orders, total, err := h.orderService.ListOrders(c.Request.Context(), tenantID, opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"orders": orders, "total": total})
}

func (h *ZalandoOrdersHandler) PushTracking(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	orderID := c.Param("id")
	var req struct {
		CredentialID   string   `json:"credential_id" binding:"required"`
		TrackingNumber string   `json:"tracking_number" binding:"required"`
		Carrier        string   `json:"carrier"`
		LineItemIDs    []string `json:"line_item_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	client, _, err := h.buildClient(c.Request.Context(), tenantID, req.CredentialID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := client.ShipOrder(orderID, req.TrackingNumber, req.Carrier, req.LineItemIDs); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "order_id": orderID, "tracking_number": req.TrackingNumber})
}

func mapZalandoStatus(s string) string {
	switch s {
	case "PENDING", "READY_FOR_FULFILLMENT":
		return "imported"
	case "SHIPPED":
		return "fulfilled"
	case "CANCELLED":
		return "cancelled"
	default:
		return "imported"
	}
}

// ============================================================================
// BOL.COM
// ============================================================================

type BolOrdersHandler struct {
	orderService       *services.OrderService
	marketplaceService *services.MarketplaceService
}

func NewBolOrdersHandler(orderService *services.OrderService, marketplaceService *services.MarketplaceService) *BolOrdersHandler {
	return &BolOrdersHandler{orderService: orderService, marketplaceService: marketplaceService}
}

func (h *BolOrdersHandler) buildClient(ctx context.Context, tenantID, credentialID string) (*bol.Client, *models.MarketplaceCredential, error) {
	cred, err := h.marketplaceService.GetCredential(ctx, tenantID, credentialID)
	if err != nil {
		return nil, nil, fmt.Errorf("get credential: %w", err)
	}
	merged, err := h.marketplaceService.GetFullCredentials(ctx, cred)
	if err != nil {
		return nil, nil, fmt.Errorf("merge credentials: %w", err)
	}
	if merged["client_id"] == "" || merged["client_secret"] == "" {
		return nil, nil, fmt.Errorf("incomplete Bol.com credentials")
	}
	return bol.NewClient(merged["client_id"], merged["client_secret"]), cred, nil
}

func (h *BolOrdersHandler) ImportBolOrders(ctx context.Context, tenantID, credentialID string, createdAfter, createdBefore time.Time) (int, error) {
	log.Printf("[Bol Orders] Import tenant=%s cred=%s since=%s", tenantID, credentialID, createdAfter.Format(time.RFC3339))
	client, cred, err := h.buildClient(ctx, tenantID, credentialID)
	if err != nil {
		return 0, err
	}
	summaries, err := client.GetAllOpenOrders()
	if err != nil {
		return 0, fmt.Errorf("fetch Bol.com orders: %w", err)
	}
	imported := 0
	for _, s := range summaries {
		if s.DateTimePlaced != "" {
			t, parseErr := time.Parse(time.RFC3339, s.DateTimePlaced)
			if parseErr == nil && (t.Before(createdAfter) || t.After(createdBefore)) {
				continue
			}
		}
		o, err := client.GetOrder(s.OrderID)
		if err != nil {
			log.Printf("[Bol Orders] Failed to fetch order %s: %v", s.OrderID, err)
			continue
		}
		currency := "EUR"
		internal := &models.Order{
			TenantID:         tenantID,
			Channel:          "bol",
			ChannelAccountID: credentialID,
			ExternalOrderID:  o.OrderID,
			Status:           "imported",
			PaymentStatus:    "captured",
			OrderDate:        o.DateTimeOrdered,
			Customer: models.Customer{
				Name:  o.BillingDetails.FirstName + " " + o.BillingDetails.Surname,
				Email: o.BillingDetails.Email,
			},
			ShippingAddress: models.Address{
				Name:         o.ShipmentDetails.FirstName + " " + o.ShipmentDetails.Surname,
				AddressLine1: o.ShipmentDetails.StreetName + " " + o.ShipmentDetails.HouseNumber,
				City:         o.ShipmentDetails.City,
				PostalCode:   o.ShipmentDetails.ZipCode,
				Country:      o.ShipmentDetails.CountryCode,
			},
			BillingAddress: &models.Address{
				Name:         o.BillingDetails.FirstName + " " + o.BillingDetails.Surname,
				AddressLine1: o.BillingDetails.StreetName + " " + o.BillingDetails.HouseNumber,
				City:         o.BillingDetails.City,
				PostalCode:   o.BillingDetails.ZipCode,
				Country:      o.BillingDetails.CountryCode,
			},
			InternalNotes: fmt.Sprintf("Bol.com Order #%s", o.OrderID),
		}
		ApplyOrderImportConfig(internal, cred.Config.Orders)
		orderID, isNew, err := h.orderService.CreateOrder(ctx, tenantID, internal)
		if err != nil {
			log.Printf("[Bol Orders] Failed to save order %s: %v", o.OrderID, err)
			continue
		}
		if !isNew {
			continue
		}
		var grandTotal float64
		for _, item := range o.OrderItems {
			lineTotal := item.UnitPrice * float64(item.Quantity)
			grandTotal += lineTotal
			line := &models.OrderLine{
				LineID:    item.OrderItemID,
				SKU:       item.EAN,
				Title:     item.Title,
				Quantity:  item.Quantity,
				UnitPrice: models.Money{Amount: item.UnitPrice, Currency: currency},
				LineTotal: models.Money{Amount: lineTotal, Currency: currency},
				Status:    "pending",
			}
			if err := h.orderService.CreateOrderLine(ctx, tenantID, orderID, line); err != nil {
				log.Printf("[Bol Orders] Failed to save line %s: %v", item.OrderItemID, err)
			}
		}
		internal.Totals = models.OrderTotals{GrandTotal: models.Money{Amount: grandTotal, Currency: currency}}
		imported++
	}
	return imported, nil
}

func (h *BolOrdersHandler) TriggerImport(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	var req struct {
		CredentialID string `json:"credential_id" binding:"required"`
		HoursBack    int    `json:"hours_back"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.HoursBack <= 0 {
		req.HoursBack = 48
	}
	now := time.Now().UTC()
	from := now.Add(-time.Duration(req.HoursBack) * time.Hour)
	c.JSON(http.StatusAccepted, gin.H{"status": "started", "from": from.Format(time.RFC3339)})
	go func() {
		ctx := context.Background()
		n, err := h.ImportBolOrders(ctx, tenantID, req.CredentialID, from, now)
		if err != nil {
			log.Printf("[Bol Orders] Import error: %v", err)
		} else {
			log.Printf("[Bol Orders] Import complete: %d orders", n)
		}
	}()
}

func (h *BolOrdersHandler) ListOrders(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	opts := services.OrderListOptions{Channel: "bol", Limit: c.DefaultQuery("limit", "50"), Offset: c.DefaultQuery("offset", "0")}
	orders, total, err := h.orderService.ListOrders(c.Request.Context(), tenantID, opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"orders": orders, "total": total})
}

func (h *BolOrdersHandler) PushTracking(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	orderItemID := c.Param("id")
	var req struct {
		CredentialID   string `json:"credential_id" binding:"required"`
		TrackingNumber string `json:"tracking_number" binding:"required"`
		Carrier        string `json:"carrier"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	client, _, err := h.buildClient(c.Request.Context(), tenantID, req.CredentialID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	shipReq := bol.ShipmentRequest{
		OrderItems: []bol.ShipmentItem{{OrderItemID: orderItemID, Quantity: 1}},
		Transport:  bol.Transport{TrackAndTrace: req.TrackingNumber, TransporterCode: req.Carrier},
	}
	if err := client.CreateShipment(shipReq); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "order_item_id": orderItemID, "tracking_number": req.TrackingNumber})
}

// ============================================================================
// LAZADA
// ============================================================================

type LazadaOrdersHandler struct {
	orderService       *services.OrderService
	marketplaceService *services.MarketplaceService
}

func NewLazadaOrdersHandler(orderService *services.OrderService, marketplaceService *services.MarketplaceService) *LazadaOrdersHandler {
	return &LazadaOrdersHandler{orderService: orderService, marketplaceService: marketplaceService}
}

func (h *LazadaOrdersHandler) buildClient(ctx context.Context, tenantID, credentialID string) (*lazada.Client, *models.MarketplaceCredential, error) {
	cred, err := h.marketplaceService.GetCredential(ctx, tenantID, credentialID)
	if err != nil {
		return nil, nil, fmt.Errorf("get credential: %w", err)
	}
	merged, err := h.marketplaceService.GetFullCredentials(ctx, cred)
	if err != nil {
		return nil, nil, fmt.Errorf("merge credentials: %w", err)
	}
	if merged["app_key"] == "" || merged["app_secret"] == "" || merged["access_token"] == "" {
		return nil, nil, fmt.Errorf("incomplete Lazada credentials (app_key, app_secret, access_token required)")
	}
	baseURL := merged["base_url"]
	if baseURL == "" {
		baseURL = "https://api.lazada.com.my/rest"
	}

	client := lazada.NewClient(merged["app_key"], merged["app_secret"], merged["access_token"], baseURL)

	// ── Proactive token expiry check ─────────────────────────────────────────
	// If token_expires_at is set and within 5 days, refresh now.
	// Also refresh reactively when TestConnection returns a 401-style error.
	shouldRefresh := false
	if cred.TokenExpiresAt != nil {
		daysUntilExpiry := time.Until(*cred.TokenExpiresAt).Hours() / 24
		if daysUntilExpiry < 5 {
			log.Printf("[Lazada] Token expires in %.1f days — proactive refresh", daysUntilExpiry)
			shouldRefresh = true
		}
	} else {
		// No expiry recorded: probe the API; if it fails, try refresh.
		if testErr := client.TestConnection(); testErr != nil {
			log.Printf("[Lazada] TestConnection failed (%v) — attempting token refresh", testErr)
			shouldRefresh = true
		}
	}

	if shouldRefresh {
		refreshToken := merged["refresh_token"]
		if refreshToken == "" {
			log.Printf("[Lazada] No refresh_token stored — cannot refresh; proceeding with existing token")
		} else {
			result, refreshErr := client.RefreshAccessToken(refreshToken)
			if refreshErr != nil {
				log.Printf("[Lazada] Token refresh failed: %v — proceeding with existing token", refreshErr)
			} else {
				// Persist new tokens back to Firestore via CredentialData
				cred.CredentialData["access_token"] = result.AccessToken
				cred.CredentialData["refresh_token"] = result.RefreshToken
				expiry := time.Now().UTC().Add(time.Duration(result.ExpiresIn) * time.Second)
				cred.TokenExpiresAt = &expiry
				if saveErr := h.marketplaceService.SaveCredential(ctx, cred); saveErr != nil {
					log.Printf("[Lazada] Failed to save refreshed token: %v", saveErr)
				}
				// Update the in-use client with the new access token
				client = lazada.NewClient(merged["app_key"], merged["app_secret"], result.AccessToken, baseURL)
				log.Printf("[Lazada] Token refreshed and saved for credential %s", credentialID)
			}
		}
	}

	return client, cred, nil
}

func (h *LazadaOrdersHandler) ImportLazadaOrders(ctx context.Context, tenantID, credentialID string, createdAfter, createdBefore time.Time) (int, error) {
	log.Printf("[Lazada Orders] Import tenant=%s cred=%s since=%s", tenantID, credentialID, createdAfter.Format(time.RFC3339))
	client, cred, err := h.buildClient(ctx, tenantID, credentialID)
	if err != nil {
		return 0, err
	}
	orders, err := client.GetOrders(createdAfter, createdBefore, []string{"pending", "ready_to_ship"})
	if err != nil {
		return 0, fmt.Errorf("fetch Lazada orders: %w", err)
	}
	imported := 0
	for _, o := range orders {
		currency := "USD"
		internal := &models.Order{
			TenantID:         tenantID,
			Channel:          "lazada",
			ChannelAccountID: credentialID,
			ExternalOrderID:  fmt.Sprintf("%d", o.OrderID),
			Status:           mapLazadaStatus(o.Status),
			PaymentStatus:    "captured",
			OrderDate:        o.CreatedAt,
			Customer: models.Customer{
				Name:  o.AddressBilling.FirstName + " " + o.AddressBilling.LastName,
				Phone: o.AddressBilling.Phone,
			},
			ShippingAddress: models.Address{
				Name:         o.AddressShipping.FirstName + " " + o.AddressShipping.LastName,
				AddressLine1: o.AddressShipping.Address1,
				AddressLine2: o.AddressShipping.Address2,
				City:         o.AddressShipping.City,
				PostalCode:   o.AddressShipping.PostCode,
				Country:      o.AddressShipping.Country,
			},
			BillingAddress: &models.Address{
				Name:         o.AddressBilling.FirstName + " " + o.AddressBilling.LastName,
				AddressLine1: o.AddressBilling.Address1,
				City:         o.AddressBilling.City,
				PostalCode:   o.AddressBilling.PostCode,
				Country:      o.AddressBilling.Country,
			},
			Totals:        models.OrderTotals{GrandTotal: models.Money{Amount: o.Price, Currency: currency}},
			InternalNotes: fmt.Sprintf("Lazada Order #%d", o.OrderID),
		}
		ApplyOrderImportConfig(internal, cred.Config.Orders)
		orderID, isNew, err := h.orderService.CreateOrder(ctx, tenantID, internal)
		if err != nil {
			log.Printf("[Lazada Orders] Failed to save order %d: %v", o.OrderID, err)
			continue
		}
		if !isNew {
			continue
		}
		items, err := client.GetOrderItems(o.OrderID)
		if err != nil {
			log.Printf("[Lazada Orders] Failed to fetch items for order %d: %v", o.OrderID, err)
		} else {
			for _, item := range items {
				line := &models.OrderLine{
					LineID:    fmt.Sprintf("%d", item.OrderItemID),
					SKU:       item.SellerSKU,
					Title:     item.Name,
					Quantity:  1,
					UnitPrice: models.Money{Amount: item.ItemPrice, Currency: currency},
					LineTotal: models.Money{Amount: item.ItemPrice, Currency: currency},
					Status:    "pending",
				}
				if err := h.orderService.CreateOrderLine(ctx, tenantID, orderID, line); err != nil {
					log.Printf("[Lazada Orders] Failed to save line %d: %v", item.OrderItemID, err)
				}
			}
		}
		imported++
	}
	return imported, nil
}

func (h *LazadaOrdersHandler) TriggerImport(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	var req struct {
		CredentialID string `json:"credential_id" binding:"required"`
		HoursBack    int    `json:"hours_back"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.HoursBack <= 0 {
		req.HoursBack = 48
	}
	now := time.Now().UTC()
	from := now.Add(-time.Duration(req.HoursBack) * time.Hour)
	c.JSON(http.StatusAccepted, gin.H{"status": "started", "from": from.Format(time.RFC3339)})
	go func() {
		ctx := context.Background()
		n, err := h.ImportLazadaOrders(ctx, tenantID, req.CredentialID, from, now)
		if err != nil {
			log.Printf("[Lazada Orders] Import error: %v", err)
		} else {
			log.Printf("[Lazada Orders] Import complete: %d orders", n)
		}
	}()
}

func (h *LazadaOrdersHandler) ListOrders(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	opts := services.OrderListOptions{Channel: "lazada", Limit: c.DefaultQuery("limit", "50"), Offset: c.DefaultQuery("offset", "0")}
	orders, total, err := h.orderService.ListOrders(c.Request.Context(), tenantID, opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"orders": orders, "total": total})
}

func (h *LazadaOrdersHandler) PushTracking(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	orderID := c.Param("id")
	var req struct {
		CredentialID     string  `json:"credential_id" binding:"required"`
		OrderItemIDs     []int64 `json:"order_item_ids" binding:"required"`
		TrackingNumber   string  `json:"tracking_number" binding:"required"`
		ShippingProvider string  `json:"shipping_provider"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	client, _, err := h.buildClient(c.Request.Context(), tenantID, req.CredentialID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	provider := req.ShippingProvider
	if provider == "" {
		provider = "MANUAL"
	}
	if err := client.SetReadyToShip(req.OrderItemIDs, provider, req.TrackingNumber); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "order_id": orderID, "tracking_number": req.TrackingNumber})
}

func mapLazadaStatus(s string) string {
	switch s {
	case "unpaid":
		return "pending_payment"
	case "pending", "ready_to_ship":
		return "imported"
	case "shipped":
		return "fulfilled"
	case "delivered":
		return "completed"
	case "canceled":
		return "cancelled"
	default:
		return "imported"
	}
}
