package handlers

// ============================================================================
// MAGENTO 2 ORDERS HANDLER
// ============================================================================
// Routes:
//   POST /magento/orders/import     → pull orders from Magento, save to MarketMate
//   GET  /magento/orders            → list raw Magento orders (debug/preview)
//   POST /magento/orders/:id/ship   → push tracking to Magento order
//   POST /magento/orders/:id/status → update order status (informational)
// ============================================================================

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"module-a/marketplace/clients/magento"
	"module-a/models"
	"module-a/services"
)

type MagentoOrdersHandler struct {
	orderService       *services.OrderService
	marketplaceService *services.MarketplaceService
}

func NewMagentoOrdersHandler(
	orderService *services.OrderService,
	marketplaceService *services.MarketplaceService,
) *MagentoOrdersHandler {
	return &MagentoOrdersHandler{
		orderService:       orderService,
		marketplaceService: marketplaceService,
	}
}

// ── Client resolution ─────────────────────────────────────────────────────────

func (h *MagentoOrdersHandler) buildClient(ctx context.Context, tenantID, credentialID string) (*magento.Client, error) {
	cred, err := h.marketplaceService.GetCredential(ctx, tenantID, credentialID)
	if err != nil {
		return nil, fmt.Errorf("get credential: %w", err)
	}
	merged, err := h.marketplaceService.GetFullCredentials(ctx, cred)
	if err != nil {
		return nil, fmt.Errorf("merge credentials: %w", err)
	}

	storeURL := merged["store_url"]
	integrationToken := merged["integration_token"]

	if storeURL == "" || integrationToken == "" {
		return nil, fmt.Errorf("incomplete Magento credentials (store_url and integration_token required)")
	}

	return magento.NewClient(storeURL, integrationToken), nil
}

// ── ImportMagentoOrders ───────────────────────────────────────────────────────

// ImportMagentoOrders is the core import function called by OrderHandler.processChannelImport.
func (h *MagentoOrdersHandler) ImportMagentoOrders(ctx context.Context, tenantID, credentialID string, createdAfter, createdBefore time.Time) (int, error) {
	log.Printf("[Magento Orders] Import for tenant=%s cred=%s from=%s to=%s",
		tenantID, credentialID, createdAfter.Format("2006-01-02"), createdBefore.Format("2006-01-02"))

	client, err := h.buildClient(ctx, tenantID, credentialID)
	if err != nil {
		return 0, err
	}

	// Import pending + processing orders
	orders, err := client.FetchNewOrders(createdAfter, createdBefore, "pending")
	if err != nil {
		return 0, fmt.Errorf("fetch Magento orders: %w", err)
	}

	// Also fetch processing orders
	processingOrders, err := client.FetchNewOrders(createdAfter, createdBefore, "processing")
	if err != nil {
		log.Printf("[Magento Orders] Warning: could not fetch processing orders: %v", err)
	} else {
		orders = append(orders, processingOrders...)
	}

	log.Printf("[Magento Orders] Fetched %d orders total", len(orders))

	imported := 0
	for _, o := range orders {
		internal := convertMagentoOrder(o, credentialID)

		orderID, isNew, err := h.orderService.CreateOrder(ctx, tenantID, internal)
		if err != nil {
			log.Printf("[Magento Orders] Failed to save order %s: %v", o.IncrementID, err)
			continue
		}
		if !isNew {
			log.Printf("[Magento Orders] Skipping duplicate order %s", o.IncrementID)
			continue
		}

		for _, item := range o.Items {
			if item.ProductType == "configurable" {
				continue // skip parent configurable items — children carry the real data
			}
			line := &models.OrderLine{
				LineID:    strconv.Itoa(item.ItemID),
				SKU:       item.SKU,
				Title:     item.Name,
				Quantity:  int(item.QtyOrdered),
				UnitPrice: models.Money{Amount: item.Price, Currency: o.OrderCurrencyCode},
				LineTotal: models.Money{Amount: item.RowTotal, Currency: o.OrderCurrencyCode},
			}
			if err := h.orderService.CreateOrderLine(ctx, tenantID, orderID, line); err != nil {
				log.Printf("[Magento Orders] Failed to save line item %d: %v", item.ItemID, err)
			}
		}

		imported++
		log.Printf("[Magento Orders] Imported order %s → %s", o.IncrementID, orderID)
	}

	return imported, nil
}

// ── HTTP Handlers ─────────────────────────────────────────────────────────────

// TriggerImport handles POST /api/v1/magento/orders/import
func (h *MagentoOrdersHandler) TriggerImport(c *gin.Context) {
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
			if cr.Channel == "magento" && cr.Active {
				req.CredentialID = cr.CredentialID
				break
			}
		}
		if req.CredentialID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "no active Magento credential found"})
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
		n, err := h.ImportMagentoOrders(ctx, tenantID, req.CredentialID, from, now)
		if err != nil {
			log.Printf("[Magento Orders] Import error: %v", err)
		} else {
			log.Printf("[Magento Orders] Import complete: %d orders imported", n)
		}
	}()
}

// ListOrders handles GET /api/v1/magento/orders — raw Magento orders for debugging.
func (h *MagentoOrdersHandler) ListOrders(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	credentialID := c.Query("credential_id")

	if credentialID == "" {
		creds, err := h.marketplaceService.ListCredentials(c.Request.Context(), tenantID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		for _, cr := range creds {
			if cr.Channel == "magento" && cr.Active {
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

// PushTracking handles POST /api/v1/magento/orders/:id/ship
// :id is the Magento entity_id (integer)
func (h *MagentoOrdersHandler) PushTracking(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	orderIDStr := c.Param("id")
	orderID, err := strconv.Atoi(orderIDStr)
	if err != nil || orderID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid order id — must be a Magento entity_id integer"})
		return
	}

	var req struct {
		CredentialID   string `json:"credential_id"`
		TrackingNumber string `json:"tracking_number" binding:"required"`
		Carrier        string `json:"carrier"`
		CarrierTitle   string `json:"carrier_title"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tracking_number is required"})
		return
	}

	if req.CredentialID == "" {
		creds, _ := h.marketplaceService.ListCredentials(c.Request.Context(), tenantID)
		for _, cr := range creds {
			if cr.Channel == "magento" && cr.Active {
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

	carrierTitle := req.CarrierTitle
	if carrierTitle == "" {
		carrierTitle = req.Carrier
	}

	if err := client.PushTracking(orderID, req.TrackingNumber, req.Carrier, carrierTitle); err != nil {
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

// UpdateOrderStatus handles POST /api/v1/magento/orders/:id/status
// Note: Magento order status is primarily driven by shipment creation.
// This endpoint is provided for informational state updates.
func (h *MagentoOrdersHandler) UpdateOrderStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"message": "Magento order status is updated automatically when a shipment is created via /ship. No separate status update is needed.",
	})
}

// ── Conversion helpers ────────────────────────────────────────────────────────

func convertMagentoOrder(o magento.OrderFull, credentialID string) *models.Order {
	status := mapMagentoOrderStatus(o.Status)
	currency := o.OrderCurrencyCode
	if currency == "" {
		currency = "GBP"
	}

	orderDate := o.CreatedAt
	if orderDate == "" {
		orderDate = time.Now().UTC().Format(time.RFC3339)
	}

	// Customer name — prefer shipping address, fallback to order-level fields
	customerName := o.CustomerFirstname + " " + o.CustomerLastname
	customerEmail := o.CustomerEmail

	shippingAddr := models.Address{}
	shippingAddrRaw := o.GetShippingAddress()
	if shippingAddrRaw != nil {
		street := ""
		if len(shippingAddrRaw.Street) > 0 {
			street = shippingAddrRaw.Street[0]
		}
		street2 := ""
		if len(shippingAddrRaw.Street) > 1 {
			street2 = shippingAddrRaw.Street[1]
		}
		addrName := shippingAddrRaw.Firstname + " " + shippingAddrRaw.Lastname
		if addrName == " " {
			addrName = customerName
		}
		shippingAddr = models.Address{
			Name:         addrName,
			AddressLine1: street,
			AddressLine2: street2,
			City:         shippingAddrRaw.City,
			State:        shippingAddrRaw.Region,
			PostalCode:   shippingAddrRaw.PostalCode,
			Country:      shippingAddrRaw.CountryID,
		}
		if shippingAddrRaw.Email != "" {
			customerEmail = shippingAddrRaw.Email
		}
	}

	return &models.Order{
		ExternalOrderID:  strconv.Itoa(o.EntityID),
		ChannelAccountID: credentialID,
		Channel:          "magento",
		Status:           status,
		PaymentStatus:    "captured",
		Customer: models.Customer{
			Name:  customerName,
			Email: customerEmail,
		},
		ShippingAddress: shippingAddr,
		OrderDate:       orderDate,
		ImportedAt:      time.Now().UTC().Format(time.RFC3339),
		Totals: models.OrderTotals{
			Shipping:   models.Money{Amount: o.ShippingAmount, Currency: currency},
			GrandTotal: models.Money{Amount: o.GrandTotal, Currency: currency},
		},
		InternalNotes: "Magento Order: " + o.IncrementID, // store Magento increment_id (human-readable order number)
	}
}

func mapMagentoOrderStatus(magentoStatus string) string {
	switch magentoStatus {
	case "pending", "pending_payment":
		return "pending_payment"
	case "processing":
		return "processing"
	case "complete":
		return "completed"
	case "canceled":
		return "cancelled"
	case "holded":
		return "on_hold"
	case "closed":
		return "completed"
	case "fraud":
		return "on_hold"
	default:
		return "imported"
	}
}
