package handlers

// ============================================================================
// MIRAKL ORDERS HANDLER
// ============================================================================
// HTTP endpoints for order operations across ALL Mirakl-powered marketplaces.
//
// Endpoints:
//   POST /mirakl/orders/import         - Import new orders into MarketMate
//   GET  /mirakl/orders                - List orders from Mirakl (raw)
//   POST /mirakl/orders/:id/accept     - Accept order lines
//   POST /mirakl/orders/:id/tracking   - Push tracking + validate shipment
//   POST /mirakl/orders/:id/refund     - Issue a refund on order lines
//   POST /mirakl/orders/:id/cancel     - Cancel an entire order
// ============================================================================

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"module-a/marketplace/clients/mirakl"
	"module-a/models"
	"module-a/services"
)

// MiraklOrdersHandler handles Mirakl order lifecycle endpoints.
type MiraklOrdersHandler struct {
	orderService       *services.OrderService
	marketplaceService *services.MarketplaceService
}

// NewMiraklOrdersHandler creates a new MiraklOrdersHandler.
func NewMiraklOrdersHandler(
	orderService *services.OrderService,
	marketplaceService *services.MarketplaceService,
) *MiraklOrdersHandler {
	return &MiraklOrdersHandler{
		orderService:       orderService,
		marketplaceService: marketplaceService,
	}
}

// ============================================================================
// CREDENTIAL RESOLUTION
// ============================================================================

func (h *MiraklOrdersHandler) getMiraklClient(ctx context.Context, tenantID, credentialID string) (*mirakl.Client, string, error) {
	cred, err := h.marketplaceService.GetCredential(ctx, tenantID, credentialID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to load credentials: %w", err)
	}

	merged, err := h.marketplaceService.GetFullCredentials(ctx, cred)
	if err != nil {
		return nil, "", fmt.Errorf("failed to resolve credentials: %w", err)
	}

	apiKey := merged["api_key"]
	if apiKey == "" {
		return nil, "", fmt.Errorf("api_key is missing from credentials")
	}

	marketplaceID := merged["marketplace_id"]
	if marketplaceID == "" {
		marketplaceID = cred.Channel
	}
	baseURL := merged["base_url"]
	shopID := merged["shop_id"]

	client := mirakl.NewClientForMarketplace(marketplaceID, apiKey, shopID, baseURL)
	return client, marketplaceID, nil
}

// ============================================================================
// IMPORT ORDERS
// ============================================================================

// TriggerImport — POST /mirakl/orders/import
// Fetches new orders from Mirakl and saves them to MarketMate.
// Accepts: WAITING_ACCEPTANCE, WAITING_DEBIT, SHIPPING states.
// Auto-accepts orders in WAITING_ACCEPTANCE (configurable via auto_accept field).
//
// Body:
//
//	{
//	  "credential_id": "...",
//	  "hours_back":    48,       // how far back to look (default 48)
//	  "auto_accept":   true      // automatically accept orders (default true)
//	}
func (h *MiraklOrdersHandler) TriggerImport(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req struct {
		CredentialID string `json:"credential_id"`
		HoursBack    int    `json:"hours_back"`
		AutoAccept   *bool  `json:"auto_accept"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.CredentialID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "credential_id is required"})
		return
	}
	if req.HoursBack <= 0 {
		req.HoursBack = 48
	}
	autoAccept := true
	if req.AutoAccept != nil {
		autoAccept = *req.AutoAccept
	}

	// Run import in background, return 202
	c.JSON(http.StatusAccepted, gin.H{
		"status":  "running",
		"message": fmt.Sprintf("Importing Mirakl orders from last %dh", req.HoursBack),
	})

	go func() {
		ctx := context.Background()
		since := time.Now().Add(-time.Duration(req.HoursBack) * time.Hour)

		client, marketplaceID, err := h.getMiraklClient(ctx, tenantID, req.CredentialID)
		if err != nil {
			log.Printf("[MiraklOrders] credential error for tenant %s: %v", tenantID, err)
			return
		}

		orders, err := client.FetchAllNewOrders(since)
		if err != nil {
			log.Printf("[MiraklOrders] fetch orders failed for %s/%s: %v", tenantID, marketplaceID, err)
			return
		}

		log.Printf("[MiraklOrders] fetched %d orders from %s for tenant %s", len(orders), marketplaceID, tenantID)

		imported := 0
		for _, o := range orders {
			internalOrder := h.miraklOrderToModel(o, tenantID, req.CredentialID, marketplaceID)
			if _, _, err := h.orderService.CreateOrder(ctx, tenantID, internalOrder); err != nil {
				log.Printf("[MiraklOrders] failed to save order %s: %v", o.OrderID, err)
				continue
			}
			imported++

			// Auto-accept if configured and order is in WAITING_ACCEPTANCE
			if autoAccept && o.OrderState == mirakl.OrderStateWaitingAcceptance {
				if err := client.AcceptAllLines(o); err != nil {
					log.Printf("[MiraklOrders] failed to auto-accept order %s: %v", o.OrderID, err)
				}
			}
		}

		log.Printf("[MiraklOrders] imported %d/%d orders for tenant %s from %s",
			imported, len(orders), tenantID, marketplaceID)
	}()
}

// ============================================================================
// LIST ORDERS (RAW)
// ============================================================================

// ListOrders — GET /mirakl/orders
// Returns raw orders from Mirakl (not persisted). Useful for debugging.
// Query: credential_id, states (comma-separated), hours_back
func (h *MiraklOrdersHandler) ListOrders(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	credentialID := c.Query("credential_id")
	if credentialID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "credential_id is required"})
		return
	}

	client, _, err := h.getMiraklClient(c.Request.Context(), tenantID, credentialID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	hoursBack := 48
	if h := c.Query("hours_back"); h != "" {
		if v, err := miraklParseInt(h); err == nil && v > 0 {
			hoursBack = v
		}
	}

	resp, err := client.ListOrders(mirakl.ListOrdersOptions{
		States:    []string{mirakl.OrderStateWaitingAcceptance, mirakl.OrderStateShipping, mirakl.OrderStateWaitingDebit},
		StartDate: time.Now().Add(-time.Duration(hoursBack) * time.Hour),
		Max:       100,
	})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"orders":      resp.Orders,
		"total_count": resp.TotalCount,
	})
}

// ============================================================================
// ACCEPT ORDER LINES
// ============================================================================

// AcceptOrder — POST /mirakl/orders/:id/accept
// Accepts all lines in a WAITING_ACCEPTANCE order.
// Body: { credential_id, refuse_line_ids: [...] }  (refuse_line_ids optional)
func (h *MiraklOrdersHandler) AcceptOrder(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	orderID := c.Param("id")

	var req struct {
		CredentialID   string   `json:"credential_id"`
		RefuseLineIDs  []string `json:"refuse_line_ids"`
		RefuseReasonCode string `json:"refuse_reason_code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	client, _, err := h.getMiraklClient(c.Request.Context(), tenantID, req.CredentialID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	// Build decisions: all accept by default, refuse specified lines
	refuseSet := make(map[string]bool)
	for _, id := range req.RefuseLineIDs {
		refuseSet[id] = true
	}

	// Fetch the order to get line IDs
	resp, err := client.ListOrders(mirakl.ListOrdersOptions{
		States: []string{mirakl.OrderStateWaitingAcceptance},
		Max:    1,
	})
	if err != nil || len(resp.Orders) == 0 {
		// Accept blindly if we can't fetch (OR21 requires line IDs)
		// Alternative: pass line IDs in request body for production use
		c.JSON(http.StatusBadRequest, gin.H{"error": "could not retrieve order lines; pass line_ids in request body"})
		return
	}

	// Find matching order
	var targetOrder *mirakl.Order
	for i := range resp.Orders {
		if resp.Orders[i].OrderID == orderID {
			targetOrder = &resp.Orders[i]
			break
		}
	}
	if targetOrder == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "order not found in WAITING_ACCEPTANCE state"})
		return
	}

	decisions := make([]mirakl.AcceptanceDecision, 0, len(targetOrder.OrderLines))
	for _, line := range targetOrder.OrderLines {
		accepted := !refuseSet[line.OrderLineID]
		d := mirakl.AcceptanceDecision{
			ID:       line.OrderLineID,
			Accepted: accepted,
		}
		if !accepted && req.RefuseReasonCode != "" {
			d.Reason = req.RefuseReasonCode
		}
		decisions = append(decisions, d)
	}

	if err := client.AcceptOrderLines(orderID, decisions); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"order_id": orderID,
		"accepted": len(decisions) - len(req.RefuseLineIDs),
		"refused":  len(req.RefuseLineIDs),
	})
}

// ============================================================================
// PUSH TRACKING
// ============================================================================

// PushTracking — POST /mirakl/orders/:id/tracking
// Sets tracking number on the Mirakl order (OR23) then validates shipment (OR24).
//
// Body:
//
//	{
//	  "credential_id":    "...",
//	  "carrier_code":     "DPD",        // from /mirakl/carriers
//	  "carrier_name":     "DPD",        // free-text fallback
//	  "tracking_number":  "12345678",
//	  "tracking_url":     "https://..."  // optional
//	}
func (h *MiraklOrdersHandler) PushTracking(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	orderID := c.Param("id")

	var req struct {
		CredentialID   string `json:"credential_id"`
		CarrierCode    string `json:"carrier_code"`
		CarrierName    string `json:"carrier_name"`
		TrackingNumber string `json:"tracking_number"`
		TrackingURL    string `json:"tracking_url"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.TrackingNumber == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tracking_number is required"})
		return
	}

	client, _, err := h.getMiraklClient(c.Request.Context(), tenantID, req.CredentialID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	if err := client.PushTracking(orderID, req.CarrierCode, req.CarrierName, req.TrackingNumber, req.TrackingURL); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "tracking push failed: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"order_id":        orderID,
		"tracking_number": req.TrackingNumber,
		"status":          "shipped",
	})
}

// ============================================================================
// REFUND
// ============================================================================

// RefundOrder — POST /mirakl/orders/:id/refund
//
// Body:
//
//	{
//	  "credential_id": "...",
//	  "lines": [
//	    { "order_line_id": "...", "amount": 9.99, "reason_code": "35", "message": "optional" }
//	  ]
//	}
func (h *MiraklOrdersHandler) RefundOrder(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	orderID := c.Param("id")

	var req struct {
		CredentialID string               `json:"credential_id"`
		Lines        []mirakl.RefundLine  `json:"lines"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(req.Lines) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "lines array cannot be empty"})
		return
	}

	client, _, err := h.getMiraklClient(c.Request.Context(), tenantID, req.CredentialID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	if err := client.RefundOrderLines(orderID, req.Lines); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"order_id":     orderID,
		"lines_refunded": len(req.Lines),
	})
}

// ============================================================================
// CANCEL
// ============================================================================

// CancelOrder — POST /mirakl/orders/:id/cancel
//
// Body: { "credential_id": "...", "reason_code": "34" }
func (h *MiraklOrdersHandler) CancelOrder(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	orderID := c.Param("id")

	var req struct {
		CredentialID string `json:"credential_id"`
		ReasonCode   string `json:"reason_code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	client, _, err := h.getMiraklClient(c.Request.Context(), tenantID, req.CredentialID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	if err := client.CancelOrder(orderID, req.ReasonCode); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"order_id": orderID,
		"status":   "cancelled",
	})
}

// ============================================================================
// MODEL CONVERSION
// ============================================================================

// miraklOrderToModel converts a Mirakl order to the internal models.Order.
func (h *MiraklOrdersHandler) miraklOrderToModel(o mirakl.Order, tenantID, credentialID, marketplaceID string) *models.Order {
	now := time.Now().UTC().Format(time.RFC3339)

	// Map Mirakl order state to MarketMate status
	status := "imported"
	switch o.OrderState {
	case mirakl.OrderStateWaitingAcceptance:
		status = "imported"
	case mirakl.OrderStateWaitingDebit, mirakl.OrderStateShipping:
		status = "processing"
	case mirakl.OrderStateShipped:
		status = "fulfilled"
	case mirakl.OrderStateCanceled, mirakl.OrderStateRefused:
		status = "cancelled"
	case mirakl.OrderStateClosed:
		status = "fulfilled"
	}

	order := &models.Order{
		TenantID:         tenantID,
		Channel:          marketplaceID,
		ChannelAccountID: credentialID,
		ExternalOrderID:  o.OrderID,
		Status:           status,
		PaymentStatus:    "captured",
		OrderDate:        o.DateCreated,
		ImportedAt:       now,
		CreatedAt:        now,
		UpdatedAt:        now,
		Customer: models.Customer{
			Name:  o.Customer.FullName(),
			Email: o.Customer.Email,
		},
		ShippingAddress: models.Address{
			Name:         o.ShippingAddress.FullName(),
			AddressLine1: o.ShippingAddress.Street1,
			AddressLine2: o.ShippingAddress.Street2,
			City:         o.ShippingAddress.City,
			State:        o.ShippingAddress.State,
			PostalCode:   o.ShippingAddress.ZipCode,
			Country:      o.ShippingAddress.Country,
		},
		Totals: models.OrderTotals{
			GrandTotal: models.Money{Amount: o.Price, Currency: o.Currency},
			Subtotal:   models.Money{Amount: o.Price - o.ShippingPrice, Currency: o.Currency},
			Shipping:   models.Money{Amount: o.ShippingPrice, Currency: o.Currency},
		},
	}

	// Billing address (same as shipping if not provided separately)
	if o.BillingAddress.ZipCode != "" {
		billing := models.Address{
			Name:         o.BillingAddress.FullName(),
			AddressLine1: o.BillingAddress.Street1,
			AddressLine2: o.BillingAddress.Street2,
			City:         o.BillingAddress.City,
			State:        o.BillingAddress.State,
			PostalCode:   o.BillingAddress.ZipCode,
			Country:      o.BillingAddress.Country,
		}
		order.BillingAddress = &billing
	}

	return order
}

// ============================================================================
// HELPERS
// ============================================================================

func miraklParseInt(s string) (int, error) {
	v := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("invalid integer: %s", s)
		}
		v = v*10 + int(c-'0')
	}
	return v, nil
}
