package handlers

// ============================================================================
// ETSY ORDERS HANDLER
// ============================================================================
// Routes:
//   POST /etsy/orders/import          → pull receipts from Etsy, save to MarketMate
//   GET  /etsy/orders                 → list raw receipts from Etsy (debug/manual)
//   POST /etsy/orders/:id/ship        → push tracking to Etsy receipt
// ============================================================================

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"module-a/marketplace/clients/etsy"
	"module-a/models"
	"module-a/services"
)

type EtsyOrdersHandler struct {
	orderService       *services.OrderService
	marketplaceService *services.MarketplaceService
}

func NewEtsyOrdersHandler(
	orderService *services.OrderService,
	marketplaceService *services.MarketplaceService,
) *EtsyOrdersHandler {
	return &EtsyOrdersHandler{
		orderService:       orderService,
		marketplaceService: marketplaceService,
	}
}

// ── Client resolution ─────────────────────────────────────────────────────────

func (h *EtsyOrdersHandler) buildEtsyClient(ctx context.Context, tenantID, credentialID string) (*etsy.Client, error) {
	cred, err := h.marketplaceService.GetCredential(ctx, tenantID, credentialID)
	if err != nil {
		return nil, fmt.Errorf("get credential: %w", err)
	}
	merged, err := h.marketplaceService.GetFullCredentials(ctx, cred)
	if err != nil {
		return nil, fmt.Errorf("merge credentials: %w", err)
	}

	apiKey := merged["client_id"]
	accessToken := merged["access_token"]
	refreshToken := merged["refresh_token"]
	shopIDStr := merged["shop_id"]

	if apiKey == "" || accessToken == "" {
		return nil, fmt.Errorf("incomplete Etsy credentials (client_id, access_token required)")
	}

	var shopID int64
	if shopIDStr != "" {
		shopID, _ = strconv.ParseInt(shopIDStr, 10, 64)
	}

	return etsy.NewClient(apiKey, accessToken, refreshToken, shopID), nil
}

// ── ImportEtsyOrders ──────────────────────────────────────────────────────────

// ImportEtsyOrders is the core import function called by OrderHandler.processChannelImport.
func (h *EtsyOrdersHandler) ImportEtsyOrders(ctx context.Context, tenantID, credentialID string, createdAfter, createdBefore time.Time) (int, error) {
	log.Printf("[Etsy Orders] Import for tenant=%s cred=%s from=%s to=%s",
		tenantID, credentialID, createdAfter.Format("2006-01-02"), createdBefore.Format("2006-01-02"))

	client, err := h.buildEtsyClient(ctx, tenantID, credentialID)
	if err != nil {
		return 0, err
	}

	receipts, err := client.FetchNewReceipts(createdAfter, createdBefore)
	if err != nil {
		return 0, fmt.Errorf("fetch Etsy receipts: %w", err)
	}

	log.Printf("[Etsy Orders] Fetched %d receipts", len(receipts))

	imported := 0
	for _, r := range receipts {
		internal := convertEtsyReceipt(r, credentialID)

		orderID, isNew, err := h.orderService.CreateOrder(ctx, tenantID, internal)
		if err != nil {
			log.Printf("[Etsy Orders] Failed to save receipt %d: %v", r.ReceiptID, err)
			continue
		}
		if !isNew {
			log.Printf("[Etsy Orders] Skipping duplicate receipt %d", r.ReceiptID)
			continue
		}

		// Save line items
		for _, tx := range r.Transactions {
			currency := r.TotalPrice.CurrencyCode
			unitPrice := 0.0
			if tx.Price.Divisor > 0 {
				unitPrice = float64(tx.Price.Amount) / float64(tx.Price.Divisor)
			}
			line := &models.OrderLine{
				LineID:    strconv.FormatInt(tx.TransactionID, 10),
				SKU:       tx.SKU,
				Title:     tx.Title,
				Quantity:  tx.Quantity,
				UnitPrice: models.Money{Amount: unitPrice, Currency: currency},
				LineTotal: models.Money{Amount: unitPrice * float64(tx.Quantity), Currency: currency},
			}
			if err := h.orderService.CreateOrderLine(ctx, tenantID, orderID, line); err != nil {
				log.Printf("[Etsy Orders] Failed to save line item %d: %v", tx.TransactionID, err)
			}
		}

		imported++
		log.Printf("[Etsy Orders] Imported receipt %d → %s", r.ReceiptID, orderID)
	}

	return imported, nil
}

// ── HTTP Handlers ─────────────────────────────────────────────────────────────

// TriggerImport handles POST /api/v1/etsy/orders/import
func (h *EtsyOrdersHandler) TriggerImport(c *gin.Context) {
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
			if cr.Channel == "etsy" && cr.Active {
				req.CredentialID = cr.CredentialID
				break
			}
		}
		if req.CredentialID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "no active Etsy credential found"})
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
		n, err := h.ImportEtsyOrders(ctx, tenantID, req.CredentialID, from, now)
		if err != nil {
			log.Printf("[Etsy Orders] Import error: %v", err)
		} else {
			log.Printf("[Etsy Orders] Import complete: %d orders imported", n)
		}
	}()
}

// ListOrders handles GET /api/v1/etsy/orders — raw Etsy receipts for debugging.
func (h *EtsyOrdersHandler) ListOrders(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	credentialID := c.Query("credential_id")

	if credentialID == "" {
		creds, err := h.marketplaceService.ListCredentials(c.Request.Context(), tenantID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		for _, cr := range creds {
			if cr.Channel == "etsy" && cr.Active {
				credentialID = cr.CredentialID
				break
			}
		}
	}

	client, err := h.buildEtsyClient(c.Request.Context(), tenantID, credentialID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	hoursBack := 48
	if hb := c.Query("hours_back"); hb != "" {
		if v, err := strconv.Atoi(hb); err == nil {
			hoursBack = v
		}
	}

	now := time.Now().UTC()
	from := now.Add(-time.Duration(hoursBack) * time.Hour)

	receipts, err := client.FetchNewReceipts(from, now)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"error": err.Error(), "orders": []interface{}{}})
		return
	}

	c.JSON(http.StatusOK, gin.H{"orders": receipts, "count": len(receipts)})
}

// PushTracking handles POST /api/v1/etsy/orders/:id/ship
// :id is the Etsy receipt_id (the external order ID)
func (h *EtsyOrdersHandler) PushTracking(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	receiptIDStr := c.Param("id")

	var receiptID int64
	fmt.Sscanf(receiptIDStr, "%d", &receiptID)
	if receiptID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid receipt_id"})
		return
	}

	var req struct {
		CredentialID   string `json:"credential_id"`
		TrackingNumber string `json:"tracking_number" binding:"required"`
		CarrierName    string `json:"carrier_name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tracking_number is required"})
		return
	}

	if req.CredentialID == "" {
		creds, _ := h.marketplaceService.ListCredentials(c.Request.Context(), tenantID)
		for _, cr := range creds {
			if cr.Channel == "etsy" && cr.Active {
				req.CredentialID = cr.CredentialID
				break
			}
		}
	}

	client, err := h.buildEtsyClient(c.Request.Context(), tenantID, req.CredentialID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	shipment, err := client.CreateReceiptShipment(receiptID, &etsy.CreateTrackingRequest{
		TrackingCode:      req.TrackingNumber,
		CarrierName:       req.CarrierName,
		OverwriteExisting: true,
	})
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":              true,
		"receipt_id":      receiptID,
		"tracking_number": req.TrackingNumber,
		"carrier_name":    req.CarrierName,
		"shipment":        shipment,
	})
}

// ── Conversion helpers ────────────────────────────────────────────────────────

func convertEtsyReceipt(r etsy.Receipt, credentialID string) *models.Order {
	status := mapEtsyReceiptStatus(r)

	totalPrice := 0.0
	if r.TotalPrice.Divisor > 0 {
		totalPrice = float64(r.TotalPrice.Amount) / float64(r.TotalPrice.Divisor)
	}
	shippingPrice := 0.0
	if r.TotalShipping.Divisor > 0 {
		shippingPrice = float64(r.TotalShipping.Amount) / float64(r.TotalShipping.Divisor)
	}
	currency := r.TotalPrice.CurrencyCode
	if currency == "" {
		currency = "USD"
	}

	shippingAddr := models.Address{
		Name:         r.ShipAddress.Name,
		AddressLine1: r.ShipAddress.FirstLine,
		AddressLine2: r.ShipAddress.SecondLine,
		City:         r.ShipAddress.City,
		State:        r.ShipAddress.State,
		PostalCode:   r.ShipAddress.Zip,
		Country:      r.ShipAddress.CountryISO,
	}

	return &models.Order{
		ExternalOrderID:  strconv.FormatInt(r.ReceiptID, 10),
		ChannelAccountID: credentialID,
		Channel:          "etsy",
		Status:           status,
		PaymentStatus:    "captured",
		Customer: models.Customer{
			Name:  r.ShipAddress.Name,
			Email: r.BuyerEmail,
		},
		ShippingAddress: shippingAddr,
		OrderDate:       time.Unix(r.CreateTimestamp, 0).Format(time.RFC3339),
		ImportedAt:      time.Now().UTC().Format(time.RFC3339),
		InternalNotes:   r.Message,
		Totals: models.OrderTotals{
			Shipping:   models.Money{Amount: shippingPrice, Currency: currency},
			GrandTotal: models.Money{Amount: totalPrice, Currency: currency},
		},
	}
}

func mapEtsyReceiptStatus(r etsy.Receipt) string {
	if r.Status == "cancelled" {
		return "cancelled"
	}
	if r.IsShipped {
		return "shipped"
	}
	if r.IsPaid {
		return "processing"
	}
	switch r.Status {
	case "open":
		return "processing"
	case "completed":
		return "completed"
	case "paid":
		return "processing"
	default:
		return "pending"
	}
}
