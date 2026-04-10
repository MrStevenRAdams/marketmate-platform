package handlers

// ============================================================================
// KAUFLAND ORDERS HANDLER
// ============================================================================
// Routes:
//   POST /kaufland/orders/import      → pull orders from Kaufland, save to MarketMate
//   GET  /kaufland/orders             → list raw Kaufland orders (debug/preview)
//   POST /kaufland/orders/:id/ship    → push tracking number to order unit
//   POST /kaufland/orders/:id/status  → (informational — status managed by Kaufland fulfilment)
// ============================================================================

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"module-a/marketplace/clients/kaufland"
	"module-a/models"
	"module-a/services"
)

type KauflandOrdersHandler struct {
	orderService       *services.OrderService
	marketplaceService *services.MarketplaceService
}

func NewKauflandOrdersHandler(
	orderService *services.OrderService,
	marketplaceService *services.MarketplaceService,
) *KauflandOrdersHandler {
	return &KauflandOrdersHandler{
		orderService:       orderService,
		marketplaceService: marketplaceService,
	}
}

// ── Client resolution ─────────────────────────────────────────────────────────

func (h *KauflandOrdersHandler) buildClient(ctx context.Context, tenantID, credentialID string) (*kaufland.Client, error) {
	cred, err := h.marketplaceService.GetCredential(ctx, tenantID, credentialID)
	if err != nil {
		return nil, fmt.Errorf("get credential: %w", err)
	}
	merged, err := h.marketplaceService.GetFullCredentials(ctx, cred)
	if err != nil {
		return nil, fmt.Errorf("merge credentials: %w", err)
	}

	clientKey := merged["client_key"]
	secretKey := merged["secret_key"]

	if clientKey == "" || secretKey == "" {
		return nil, fmt.Errorf("incomplete Kaufland credentials (client_key, secret_key required)")
	}

	return kaufland.NewClient(clientKey, secretKey), nil
}

// ── ImportKauflandOrders ──────────────────────────────────────────────────────

// ImportKauflandOrders is the core import function called by OrderHandler.processChannelImport.
func (h *KauflandOrdersHandler) ImportKauflandOrders(ctx context.Context, tenantID, credentialID string, createdAfter, createdBefore time.Time) (int, error) {
	log.Printf("[Kaufland Orders] Import for tenant=%s cred=%s from=%s to=%s",
		tenantID, credentialID, createdAfter.Format("2006-01-02"), createdBefore.Format("2006-01-02"))

	client, err := h.buildClient(ctx, tenantID, credentialID)
	if err != nil {
		return 0, err
	}

	orders, err := client.GetNewOrders()
	if err != nil {
		return 0, fmt.Errorf("fetch Kaufland orders: %w", err)
	}

	log.Printf("[Kaufland Orders] Fetched %d orders", len(orders))

	imported := 0
	for _, o := range orders {
		internal := convertKauflandOrder(o, credentialID)

		orderID, isNew, err := h.orderService.CreateOrder(ctx, tenantID, internal)
		if err != nil {
			log.Printf("[Kaufland Orders] Failed to save order %s: %v", o.OrderID, err)
			continue
		}
		if !isNew {
			log.Printf("[Kaufland Orders] Skipping duplicate order %s", o.OrderID)
			continue
		}

		for _, unit := range o.OrderUnits {
			line := &models.OrderLine{
				LineID:    unit.OrderUnitID,
				SKU:       unit.EAN,
				Title:     unit.ProductTitle,
				Quantity:  unit.Quantity,
				UnitPrice: models.Money{Amount: unit.Price, Currency: o.Currency},
				LineTotal: models.Money{Amount: unit.Price * float64(unit.Quantity), Currency: o.Currency},
			}
			if err := h.orderService.CreateOrderLine(ctx, tenantID, orderID, line); err != nil {
				log.Printf("[Kaufland Orders] Failed to save line item %s: %v", unit.OrderUnitID, err)
			}
		}

		imported++
		log.Printf("[Kaufland Orders] Imported order %s → %s", o.OrderID, orderID)
	}

	return imported, nil
}

// ── HTTP Handlers ─────────────────────────────────────────────────────────────

// TriggerImport handles POST /api/v1/kaufland/orders/import
func (h *KauflandOrdersHandler) TriggerImport(c *gin.Context) {
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
			if cr.Channel == "kaufland" && cr.Active {
				req.CredentialID = cr.CredentialID
				break
			}
		}
		if req.CredentialID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "no active Kaufland credential found"})
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
		n, err := h.ImportKauflandOrders(ctx, tenantID, req.CredentialID, from, now)
		if err != nil {
			log.Printf("[Kaufland Orders] Import error: %v", err)
		} else {
			log.Printf("[Kaufland Orders] Import complete: %d orders imported", n)
		}
	}()
}

// ListOrders handles GET /api/v1/kaufland/orders — raw Kaufland orders for debugging.
func (h *KauflandOrdersHandler) ListOrders(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	credentialID := c.Query("credential_id")

	if credentialID == "" {
		creds, err := h.marketplaceService.ListCredentials(c.Request.Context(), tenantID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		for _, cr := range creds {
			if cr.Channel == "kaufland" && cr.Active {
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

	status := c.Query("status")
	orders, _, err := client.GetOrders(status, 0, 50)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"error": err.Error(), "orders": []interface{}{}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"orders": orders, "count": len(orders)})
}

// PushTracking handles POST /api/v1/kaufland/orders/:id/ship
// :id is the order_unit_id (Kaufland fulfils per order unit, not per order).
func (h *KauflandOrdersHandler) PushTracking(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	orderUnitID := c.Param("id")
	if orderUnitID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "order_unit_id is required"})
		return
	}

	var req struct {
		CredentialID   string `json:"credential_id"`
		TrackingNumber string `json:"tracking_number" binding:"required"`
		CarrierCode    string `json:"carrier_code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tracking_number is required"})
		return
	}

	if req.CredentialID == "" {
		creds, _ := h.marketplaceService.ListCredentials(c.Request.Context(), tenantID)
		for _, cr := range creds {
			if cr.Channel == "kaufland" && cr.Active {
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

	carrierCode := req.CarrierCode
	if carrierCode == "" {
		carrierCode = "OTHER"
	}

	if err := client.FulfillOrderUnit(orderUnitID, req.TrackingNumber, carrierCode); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":              true,
		"order_unit_id":   orderUnitID,
		"tracking_number": req.TrackingNumber,
		"carrier_code":    carrierCode,
	})
}

// UpdateOrderStatus handles POST /api/v1/kaufland/orders/:id/status
// Note: On Kaufland, status transitions are driven by fulfilment (ship action) — this endpoint
// is provided for informational purposes and internal state tracking only.
func (h *KauflandOrdersHandler) UpdateOrderStatus(c *gin.Context) {
	orderID := c.Param("id")

	var req struct {
		Status string `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "status is required"})
		return
	}

	// Kaufland doesn't expose a direct status-update endpoint — status changes
	// happen via the fulfilment flow. We acknowledge the request without error.
	log.Printf("[Kaufland Orders] Status update requested for order %s → %s (Kaufland status is controlled by fulfilment)", orderID, req.Status)

	c.JSON(http.StatusOK, gin.H{
		"ok":       true,
		"order_id": orderID,
		"status":   req.Status,
		"note":     "Kaufland order status is controlled by fulfilment. Use the /ship endpoint to fulfil an order unit.",
	})
}

// ── Conversion helpers ────────────────────────────────────────────────────────

func convertKauflandOrder(o kaufland.Order, credentialID string) *models.Order {
	status := mapKauflandOrderStatus(o.Status)

	shippingAddr := models.Address{
		Name:         o.ShippingAddress.FirstName + " " + o.ShippingAddress.LastName,
		AddressLine1: o.ShippingAddress.Street + " " + o.ShippingAddress.HouseNo,
		City:         o.ShippingAddress.City,
		PostalCode:   o.ShippingAddress.Postcode,
		Country:      o.ShippingAddress.Country,
	}
	if shippingAddr.Name == " " {
		shippingAddr.Name = o.BillingAddress.FirstName + " " + o.BillingAddress.LastName
	}

	currency := o.Currency
	if currency == "" {
		currency = "EUR"
	}

	orderDate := o.CreatedAt
	if orderDate == "" {
		orderDate = time.Now().UTC().Format(time.RFC3339)
	}

	return &models.Order{
		ExternalOrderID:  o.OrderID,
		ChannelAccountID: credentialID,
		Channel:          "kaufland",
		Status:           status,
		PaymentStatus:    "captured",
		Customer: models.Customer{
			Name:  o.BillingAddress.FirstName + " " + o.BillingAddress.LastName,
			Email: o.BillingAddress.Email,
			Phone: o.BillingAddress.Phone,
		},
		ShippingAddress: shippingAddr,
		OrderDate:       orderDate,
		ImportedAt:      time.Now().UTC().Format(time.RFC3339),
		Totals: models.OrderTotals{
			GrandTotal: models.Money{Amount: o.TotalAmount, Currency: currency},
		},
	}
}

func mapKauflandOrderStatus(kauflandStatus string) string {
	switch kauflandStatus {
	case "need_to_be_sent":
		return "processing"
	case "sent":
		return "shipped"
	case "received":
		return "completed"
	case "cancelled":
		return "cancelled"
	case "returned":
		return "returned"
	default:
		return "imported"
	}
}
