package handlers

// ============================================================================
// WALMART ORDERS HANDLER
// ============================================================================
// Routes:
//   POST /walmart/orders/import        → pull orders from Walmart, save to MarketMate
//   GET  /walmart/orders               → list raw Walmart orders (debug)
//   POST /walmart/orders/:id/ship      → push tracking to Walmart order
//   POST /walmart/orders/:id/acknowledge → acknowledge order receipt
// ============================================================================

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"module-a/marketplace/clients/walmart"
	"module-a/models"
	"module-a/services"
)

type WalmartOrdersHandler struct {
	orderService       *services.OrderService
	marketplaceService *services.MarketplaceService
}

func NewWalmartOrdersHandler(
	orderService *services.OrderService,
	marketplaceService *services.MarketplaceService,
) *WalmartOrdersHandler {
	return &WalmartOrdersHandler{
		orderService:       orderService,
		marketplaceService: marketplaceService,
	}
}

// ── Client resolution ─────────────────────────────────────────────────────────

func (h *WalmartOrdersHandler) buildClient(ctx context.Context, tenantID, credentialID string) (*walmart.Client, error) {
	cred, err := h.marketplaceService.GetCredential(ctx, tenantID, credentialID)
	if err != nil {
		return nil, fmt.Errorf("get credential: %w", err)
	}
	merged, err := h.marketplaceService.GetFullCredentials(ctx, cred)
	if err != nil {
		return nil, fmt.Errorf("merge credentials: %w", err)
	}

	clientID := merged["client_id"]
	clientSecret := merged["client_secret"]

	if clientID == "" || clientSecret == "" {
		return nil, fmt.Errorf("incomplete Walmart credentials (client_id and client_secret required)")
	}

	return walmart.NewClient(clientID, clientSecret), nil
}

// ── ImportWalmartOrders ───────────────────────────────────────────────────────

// ImportWalmartOrders is the core import function called by OrderHandler.processChannelImport.
func (h *WalmartOrdersHandler) ImportWalmartOrders(ctx context.Context, tenantID, credentialID string, createdAfter, createdBefore time.Time) (int, error) {
	log.Printf("[Walmart Orders] Import for tenant=%s cred=%s from=%s to=%s",
		tenantID, credentialID, createdAfter.Format("2006-01-02"), createdBefore.Format("2006-01-02"))

	client, err := h.buildClient(ctx, tenantID, credentialID)
	if err != nil {
		return 0, err
	}

	orders, err := client.FetchNewOrders(createdAfter, createdBefore)
	if err != nil {
		return 0, fmt.Errorf("fetch Walmart orders: %w", err)
	}

	log.Printf("[Walmart Orders] Fetched %d orders", len(orders))

	// Acknowledge orders so Walmart marks them as received
	var poIDs []string
	for _, o := range orders {
		poIDs = append(poIDs, o.PurchaseOrderID)
	}
	if len(poIDs) > 0 {
		if ackErr := client.AcknowledgeOrders(poIDs); ackErr != nil {
			log.Printf("[Walmart Orders] Warning: acknowledge failed: %v", ackErr)
		}
	}

	imported := 0
	for _, o := range orders {
		internal := convertWalmartOrder(o, credentialID)

		orderID, isNew, err := h.orderService.CreateOrder(ctx, tenantID, internal)
		if err != nil {
			log.Printf("[Walmart Orders] Failed to save order %s: %v", o.PurchaseOrderID, err)
			continue
		}
		if !isNew {
			log.Printf("[Walmart Orders] Skipping duplicate order %s", o.PurchaseOrderID)
			continue
		}

		for _, line := range o.OrderLines.OrderLine {
			qty := 1
			if line.OrderLineQuantity.Amount != "" {
				if q, err := strconv.Atoi(line.OrderLineQuantity.Amount); err == nil {
					qty = q
				}
			}

			// Find unit price from charges
			var unitPrice float64
			for _, charge := range line.Charges.Charge {
				if charge.ChargeType == "PRODUCT" {
					unitPrice = charge.ChargeAmount.Amount
					break
				}
			}

			currency := "USD"

			ol := &models.OrderLine{
				LineID:    line.LineNumber,
				SKU:       line.Item.SKU,
				Title:     line.Item.ProductName,
				Quantity:  qty,
				UnitPrice: models.Money{Amount: unitPrice, Currency: currency},
				LineTotal: models.Money{Amount: unitPrice * float64(qty), Currency: currency},
			}
			if err := h.orderService.CreateOrderLine(ctx, tenantID, orderID, ol); err != nil {
				log.Printf("[Walmart Orders] Failed to save line %s: %v", line.LineNumber, err)
			}
		}

		imported++
		log.Printf("[Walmart Orders] Imported order %s → %s", o.PurchaseOrderID, orderID)
	}

	return imported, nil
}

// ── HTTP Handlers ─────────────────────────────────────────────────────────────

// TriggerImport handles POST /api/v1/walmart/orders/import
func (h *WalmartOrdersHandler) TriggerImport(c *gin.Context) {
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
			if cr.Channel == "walmart" && cr.Active {
				req.CredentialID = cr.CredentialID
				break
			}
		}
		if req.CredentialID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "no active Walmart credential found"})
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
		n, err := h.ImportWalmartOrders(ctx, tenantID, req.CredentialID, from, now)
		if err != nil {
			log.Printf("[Walmart Orders] Import error: %v", err)
		} else {
			log.Printf("[Walmart Orders] Import complete: %d orders imported", n)
		}
	}()
}

// ListOrders handles GET /api/v1/walmart/orders
func (h *WalmartOrdersHandler) ListOrders(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	credentialID := c.Query("credential_id")

	if credentialID == "" {
		creds, err := h.marketplaceService.ListCredentials(c.Request.Context(), tenantID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		for _, cr := range creds {
			if cr.Channel == "walmart" && cr.Active {
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

	orders, err := client.FetchNewOrders(from, now)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"error": err.Error(), "orders": []interface{}{}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"orders": orders, "count": len(orders)})
}

// PushTracking handles POST /api/v1/walmart/orders/:id/ship
func (h *WalmartOrdersHandler) PushTracking(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	purchaseOrderID := c.Param("id")

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
			if cr.Channel == "walmart" && cr.Active {
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

	// Fetch order to get line numbers
	now := time.Now().UTC()
	orders, err := client.FetchNewOrders(now.Add(-30*24*time.Hour), now)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch order details: " + err.Error()})
		return
	}

	var targetOrder *walmart.WalmartOrder
	for i := range orders {
		if orders[i].PurchaseOrderID == purchaseOrderID {
			targetOrder = &orders[i]
			break
		}
	}

	if targetOrder == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "order not found: " + purchaseOrderID})
		return
	}

	if err := client.PushTracking(purchaseOrderID, targetOrder.OrderLines.OrderLine, req.TrackingNumber, req.Carrier, req.TrackingURL); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":               true,
		"purchase_order_id": purchaseOrderID,
		"tracking_number":  req.TrackingNumber,
		"carrier":          req.Carrier,
	})
}

// AcknowledgeOrder handles POST /api/v1/walmart/orders/:id/acknowledge
func (h *WalmartOrdersHandler) AcknowledgeOrder(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	purchaseOrderID := c.Param("id")
	credentialID := c.Query("credential_id")

	if credentialID == "" {
		creds, _ := h.marketplaceService.ListCredentials(c.Request.Context(), tenantID)
		for _, cr := range creds {
			if cr.Channel == "walmart" && cr.Active {
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

	if err := client.AcknowledgeOrders([]string{purchaseOrderID}); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "purchase_order_id": purchaseOrderID})
}

// ── Conversion helpers ────────────────────────────────────────────────────────

func convertWalmartOrder(o walmart.WalmartOrder, credentialID string) *models.Order {
	status := mapWalmartOrderStatus(o.Status)

	addr := o.ShippingInfo.PostalAddress
	shippingAddr := models.Address{
		Name:         addr.Name,
		AddressLine1: addr.Address1,
		AddressLine2: addr.Address2,
		City:         addr.City,
		State:        addr.State,
		PostalCode:   addr.PostalCode,
		Country:      addr.Country,
	}

	// Calculate totals from order lines
	var grandTotal float64
	for _, line := range o.OrderLines.OrderLine {
		for _, charge := range line.Charges.Charge {
			grandTotal += charge.ChargeAmount.Amount
		}
	}

	orderDate := o.OrderDate
	if orderDate == "" {
		orderDate = time.Now().UTC().Format(time.RFC3339)
	}

	order := &models.Order{
		ExternalOrderID:  o.PurchaseOrderID,
		ChannelAccountID: credentialID,
		Channel:          "walmart",
		Status:           status,
		PaymentStatus:    "captured",
		Customer: models.Customer{
			Name:  addr.Name,
			Email: o.CustomerEmail,
			Phone: o.ShippingInfo.Phone,
		},
		ShippingAddress: shippingAddr,
		OrderDate:       orderDate,
		ImportedAt:      time.Now().UTC().Format(time.RFC3339),
		Totals: models.OrderTotals{
			GrandTotal: models.Money{Amount: grandTotal, Currency: "USD"},
		},
	}

	return order
}

func mapWalmartOrderStatus(walmartStatus string) string {
	switch walmartStatus {
	case "Created":
		return "processing"
	case "Acknowledged":
		return "processing"
	case "Shipped":
		return "dispatched"
	case "Delivered":
		return "completed"
	case "Cancelled":
		return "cancelled"
	case "Refund":
		return "refunded"
	default:
		return "imported"
	}
}
