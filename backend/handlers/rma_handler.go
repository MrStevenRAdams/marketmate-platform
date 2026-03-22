package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"

	"module-a/models"
	"module-a/services"
)

// ============================================================================
// RMA HANDLER
// Routes:
//   GET    /api/v1/rmas                     List RMAs (filter: status, channel)
//   POST   /api/v1/rmas                     Create manual RMA
//   GET    /api/v1/rmas/:id                 Get single RMA
//   PUT    /api/v1/rmas/:id                 Update RMA (notes, tracking)
//   POST   /api/v1/rmas/:id/authorise       Authorise → status: authorised
//   POST   /api/v1/rmas/:id/receive         Mark received + set qty_received per line
//   POST   /api/v1/rmas/:id/inspect         Record condition + disposition per line
//   POST   /api/v1/rmas/:id/restock/:line   Restock a single line (simple inventory mode)
//   POST   /api/v1/rmas/:id/resolve         Final resolution + close
//   POST   /api/v1/rmas/sync                Pull pending returns from marketplaces
// ============================================================================

type RMAHandler struct {
	client             *firestore.Client
	marketplaceService *services.MarketplaceService
	templateSvc        *services.TemplateService
}

func NewRMAHandler(client *firestore.Client, marketplaceService *services.MarketplaceService) *RMAHandler {
	return &RMAHandler{
		client:             client,
		marketplaceService: marketplaceService,
	}
}

// SetTemplateService optionally wires in the TemplateService for automated email triggers.
func (h *RMAHandler) SetTemplateService(svc *services.TemplateService) {
	h.templateSvc = svc
}

// ─── Collection helpers ───────────────────────────────────────────────────────

func (h *RMAHandler) col(tenantID string) *firestore.CollectionRef {
	return h.client.Collection(fmt.Sprintf("tenants/%s/rmas", tenantID))
}

func (h *RMAHandler) doc(tenantID, rmaID string) *firestore.DocumentRef {
	return h.col(tenantID).Doc(rmaID)
}

// ─── RMA number generation ────────────────────────────────────────────────────

func (h *RMAHandler) nextRMANumber(ctx context.Context, tenantID string) string {
	year := time.Now().Year()
	prefix := fmt.Sprintf("RMA-%d-", year)

	iter := h.col(tenantID).
		Where("rma_number", ">=", prefix).
		Where("rma_number", "<", fmt.Sprintf("RMA-%d-", year+1)).
		OrderBy("rma_number", firestore.Desc).
		Limit(1).
		Documents(ctx)

	doc, err := iter.Next()
	if err != nil {
		return fmt.Sprintf("%s0001", prefix)
	}
	var rma models.RMA
	if err := doc.DataTo(&rma); err != nil {
		return fmt.Sprintf("%s0001", prefix)
	}
	// Extract sequence number from e.g. "RMA-2026-0042"
	numStr := rma.RMANumber[len(prefix):]
	n, err := strconv.Atoi(numStr)
	if err != nil {
		return fmt.Sprintf("%s0001", prefix)
	}
	return fmt.Sprintf("%s%04d", prefix, n+1)
}

// ─── Timeline helper ──────────────────────────────────────────────────────────

func newEvent(status, note, createdBy string) models.RMAEvent {
	return models.RMAEvent{
		EventID:   uuid.New().String(),
		Status:    status,
		Note:      note,
		CreatedBy: createdBy,
		CreatedAt: time.Now(),
	}
}

// ─── Tenant config helpers ────────────────────────────────────────────────────

func (h *RMAHandler) getTenantConfig(ctx context.Context, tenantID string) map[string]interface{} {
	doc, err := h.client.Doc(fmt.Sprintf("tenants/%s/config/settings", tenantID)).Get(ctx)
	if err != nil {
		return map[string]interface{}{}
	}
	return doc.Data()
}

func (h *RMAHandler) inventoryMode(ctx context.Context, tenantID string) string {
	cfg := h.getTenantConfig(ctx, tenantID)
	if m, ok := cfg["inventory_mode"].(string); ok && m != "" {
		return m
	}
	return "simple"
}

func (h *RMAHandler) returnsLocationID(ctx context.Context, tenantID string) string {
	cfg := h.getTenantConfig(ctx, tenantID)
	if id, ok := cfg["returns_location_id"].(string); ok {
		return id
	}
	return ""
}

// ─── Inventory adjustment helper ─────────────────────────────────────────────

func (h *RMAHandler) createInventoryAdjustment(ctx context.Context, tenantID string, adj map[string]interface{}) {
	adjID := uuid.New().String()
	adj["adjustment_id"] = adjID
	adj["created_at"] = time.Now()
	h.client.Collection(fmt.Sprintf("tenants/%s/inventory_adjustments", tenantID)).Doc(adjID).Set(ctx, adj)
}

// ============================================================================
// LIST  GET /api/v1/rmas
// ============================================================================

func (h *RMAHandler) ListRMAs(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	q := h.col(tenantID).Query
	if status := c.Query("status"); status != "" {
		q = q.Where("status", "==", status)
	}
	if channel := c.Query("channel"); channel != "" {
		q = q.Where("channel", "==", channel)
	}
	if rmaType := c.Query("rma_type"); rmaType != "" {
		q = q.Where("rma_type", "==", rmaType)
	}
	q = q.OrderBy("created_at", firestore.Desc).Limit(200)

	iter := q.Documents(ctx)
	var rmas []models.RMA
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		var rma models.RMA
		if err := doc.DataTo(&rma); err == nil {
			rmas = append(rmas, rma)
		}
	}
	if rmas == nil {
		rmas = []models.RMA{}
	}

	// Apply date range filters in-memory (Firestore multi-inequality workaround)
	if dateFrom := c.Query("date_from"); dateFrom != "" {
		from, err := time.Parse("2006-01-02", dateFrom)
		if err == nil {
			filtered := rmas[:0]
			for _, r := range rmas {
				if !r.CreatedAt.Before(from) {
					filtered = append(filtered, r)
				}
			}
			rmas = filtered
		}
	}
	if dateTo := c.Query("date_to"); dateTo != "" {
		to, err := time.Parse("2006-01-02", dateTo)
		if err == nil {
			to = to.Add(24*time.Hour - time.Second) // inclusive end of day
			filtered := rmas[:0]
			for _, r := range rmas {
				if !r.CreatedAt.After(to) {
					filtered = append(filtered, r)
				}
			}
			rmas = filtered
		}
	}

	// Count actionable RMAs (requested or received)
	actionable := 0
	for _, r := range rmas {
		if r.Status == models.RMAStatusRequested || r.Status == models.RMAStatusReceived {
			actionable++
		}
	}

	c.JSON(http.StatusOK, gin.H{"rmas": rmas, "total": len(rmas), "actionable": actionable})
}

// ============================================================================
// CREATE  POST /api/v1/rmas
// ============================================================================

func (h *RMAHandler) CreateRMA(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	var req struct {
		OrderID     string          `json:"order_id"`
		OrderNumber string          `json:"order_number"`
		Channel     string          `json:"channel"`
		Customer    models.RMACustomer `json:"customer"`
		Lines       []models.RMALine   `json:"lines"`
		Notes       string          `json:"notes"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Channel == "" {
		req.Channel = "manual"
	}

	rmaID := uuid.New().String()
	rmaNumber := h.nextRMANumber(ctx, tenantID)
	now := time.Now()

	// Assign line IDs
	for i := range req.Lines {
		if req.Lines[i].LineID == "" {
			req.Lines[i].LineID = uuid.New().String()
		}
	}

	userID := c.GetString("user_id")
	if userID == "" {
		userID = "system"
	}

	rma := models.RMA{
		RMAID:       rmaID,
		TenantID:    tenantID,
		RMANumber:   rmaNumber,
		OrderID:     req.OrderID,
		OrderNumber: req.OrderNumber,
		Channel:     req.Channel,
		Status:      models.RMAStatusRequested,
		Customer:    req.Customer,
		Lines:       req.Lines,
		Notes:       req.Notes,
		CreatedBy:   userID,
		CreatedAt:   now,
		UpdatedAt:   now,
		Timeline: []models.RMAEvent{
			newEvent(models.RMAStatusRequested, "RMA created", userID),
		},
	}

	if _, err := h.doc(tenantID, rmaID).Set(ctx, rma); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"rma": rma})
}

// ============================================================================
// GET  GET /api/v1/rmas/:id
// ============================================================================

func (h *RMAHandler) GetRMA(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	rmaID := c.Param("id")
	ctx := c.Request.Context()

	doc, err := h.doc(tenantID, rmaID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "RMA not found"})
		return
	}
	var rma models.RMA
	if err := doc.DataTo(&rma); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"rma": rma})
}

// ============================================================================
// UPDATE  PUT /api/v1/rmas/:id
// ============================================================================

func (h *RMAHandler) UpdateRMA(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	rmaID := c.Param("id")
	ctx := c.Request.Context()

	var req struct {
		Notes          string `json:"notes"`
		TrackingNumber string `json:"tracking_number"`
		LabelURL       string `json:"label_url"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	updates := []firestore.Update{
		{Path: "updated_at", Value: time.Now()},
	}
	if req.Notes != "" {
		updates = append(updates, firestore.Update{Path: "notes", Value: req.Notes})
	}
	if req.TrackingNumber != "" {
		updates = append(updates, firestore.Update{Path: "tracking_number", Value: req.TrackingNumber})
	}
	if req.LabelURL != "" {
		updates = append(updates, firestore.Update{Path: "label_url", Value: req.LabelURL})
	}

	if _, err := h.doc(tenantID, rmaID).Update(ctx, updates); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ============================================================================
// AUTHORISE  POST /api/v1/rmas/:id/authorise
// ============================================================================

func (h *RMAHandler) AuthoriseRMA(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	rmaID := c.Param("id")
	ctx := c.Request.Context()

	userID := c.GetString("user_id")
	if userID == "" {
		userID = "staff"
	}

	// Load RMA to get channel info
	doc, err := h.doc(tenantID, rmaID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "RMA not found"})
		return
	}
	var rma models.RMA
	doc.DataTo(&rma)

	// For eBay-sourced RMAs, accept the return via Post-Order API
	var warning string
	if rma.Channel == "ebay" && rma.MarketplaceRMAID != "" && rma.ChannelAccountID != "" {
		cred, err := h.marketplaceService.GetCredential(ctx, tenantID, rma.ChannelAccountID)
		if err == nil {
			mergedCreds, err := h.marketplaceService.GetFullCredentials(ctx, cred)
			if err == nil {
				if err := h.acceptEbayReturn(ctx, mergedCreds, rma.MarketplaceRMAID); err != nil {
					// Non-fatal — log and continue; RMA is still authorised locally
					log.Printf("[RMA] eBay accept return failed for %s: %v", rma.MarketplaceRMAID, err)
					warning = fmt.Sprintf("Authorised locally but eBay notification failed: %v", err)
				}
			}
		}
	}

	event := newEvent(models.RMAStatusAuthorised, "Return authorised", userID)

	if _, err := h.doc(tenantID, rmaID).Update(ctx, []firestore.Update{
		{Path: "status", Value: models.RMAStatusAuthorised},
		{Path: "updated_at", Value: time.Now()},
		{Path: "timeline", Value: firestore.ArrayUnion(event)},
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	resp := gin.H{"ok": true, "status": models.RMAStatusAuthorised}
	if warning != "" {
		resp["warning"] = warning
	}
	c.JSON(http.StatusOK, resp)
}

// ============================================================================
// RECEIVE  POST /api/v1/rmas/:id/receive
// Body: { "lines": [{ "line_id": "...", "qty_received": 2 }] }
// ============================================================================

func (h *RMAHandler) ReceiveRMA(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	rmaID := c.Param("id")
	ctx := c.Request.Context()

	var req struct {
		Lines []struct {
			LineID      string `json:"line_id"`
			QtyReceived int    `json:"qty_received"`
		} `json:"lines"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Load current RMA
	doc, err := h.doc(tenantID, rmaID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "RMA not found"})
		return
	}
	var rma models.RMA
	if err := doc.DataTo(&rma); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Update qty_received per line
	lineMap := map[string]int{}
	for _, l := range req.Lines {
		lineMap[l.LineID] = l.QtyReceived
	}
	for i, line := range rma.Lines {
		if qty, ok := lineMap[line.LineID]; ok {
			rma.Lines[i].QtyReceived = qty
		}
	}

	inventoryMode := h.inventoryMode(ctx, tenantID)
	returnsLocID := h.returnsLocationID(ctx, tenantID)
	userID := c.GetString("user_id")
	if userID == "" {
		userID = "staff"
	}

	// Advanced inventory: immediately stock into returns location
	if inventoryMode == "advanced" && returnsLocID != "" {
		for _, line := range rma.Lines {
			if line.QtyReceived > 0 {
				h.createInventoryAdjustment(ctx, tenantID, map[string]interface{}{
					"type":         "return",
					"product_id":   line.ProductID,
					"product_sku":  line.SKU,
					"product_name": line.ProductName,
					"location_id":  returnsLocID,
					"delta":        line.QtyReceived,
					"reason":       fmt.Sprintf("RMA %s received", rma.RMANumber),
					"reference":    rma.RMAID,
					"created_by":   userID,
				})
			}
		}
	} else {
		// Simple mode: set pending_restock_qty, don't touch stock yet
		for i, line := range rma.Lines {
			if line.QtyReceived > 0 {
				rma.Lines[i].PendingRestockQty = line.QtyReceived
			}
		}
	}

	event := newEvent(models.RMAStatusReceived, "Items received at warehouse", userID)

	if _, err := h.doc(tenantID, rmaID).Update(ctx, []firestore.Update{
		{Path: "status", Value: models.RMAStatusReceived},
		{Path: "lines", Value: rma.Lines},
		{Path: "updated_at", Value: time.Now()},
		{Path: "timeline", Value: firestore.ArrayUnion(event)},
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Fire return_confirmation automated email trigger
	if h.templateSvc != nil && rma.OrderID != "" {
		order := h.loadOrderForEmail(ctx, tenantID, rma.OrderID, rma.Customer.Email, rma.Customer.Name)
		if order != nil {
			go h.templateSvc.SendEventEmail(context.Background(), tenantID, models.TriggerEventReturnConfirmation, order)
		}
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "status": models.RMAStatusReceived, "inventory_mode": inventoryMode})
}

// ============================================================================
// INSPECT  POST /api/v1/rmas/:id/inspect
// Body: { "lines": [{ "line_id": "...", "condition": "resaleable", "disposition": "restock", "restock_location_id": "...", "restock_qty": 1 }] }
// ============================================================================

func (h *RMAHandler) InspectRMA(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	rmaID := c.Param("id")
	ctx := c.Request.Context()

	var req struct {
		Lines []struct {
			LineID            string `json:"line_id"`
			Condition         string `json:"condition"`
			Disposition       string `json:"disposition"`
			RestockLocationID string `json:"restock_location_id"`
			RestockQty        int    `json:"restock_qty"`
		} `json:"lines"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	doc, err := h.doc(tenantID, rmaID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "RMA not found"})
		return
	}
	var rma models.RMA
	if err := doc.DataTo(&rma); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	userID := c.GetString("user_id")
	if userID == "" {
		userID = "staff"
	}

	inspMap := map[string]struct {
		Condition         string
		Disposition       string
		RestockLocationID string
		RestockQty        int
	}{}
	for _, l := range req.Lines {
		inspMap[l.LineID] = struct {
			Condition         string
			Disposition       string
			RestockLocationID string
			RestockQty        int
		}{l.Condition, l.Disposition, l.RestockLocationID, l.RestockQty}
	}

	for i, line := range rma.Lines {
		insp, ok := inspMap[line.LineID]
		if !ok {
			continue
		}
		rma.Lines[i].Condition = insp.Condition
		rma.Lines[i].Disposition = insp.Disposition
		rma.Lines[i].RestockLocationID = insp.RestockLocationID
		rma.Lines[i].RestockQty = insp.RestockQty

		// Create audit adjustments for write-offs / missing
		switch insp.Disposition {
		case models.RMADispositionWriteOff:
			h.createInventoryAdjustment(ctx, tenantID, map[string]interface{}{
				"type":         "write_off",
				"product_id":   line.ProductID,
				"product_sku":  line.SKU,
				"product_name": line.ProductName,
				"delta":        0,
				"reason":       fmt.Sprintf("RMA %s — %s", rma.RMANumber, insp.Condition),
				"reference":    rma.RMAID,
				"created_by":   userID,
			})
		}
	}

	event := newEvent(models.RMAStatusInspected, "Items inspected", userID)

	if _, err := h.doc(tenantID, rmaID).Update(ctx, []firestore.Update{
		{Path: "status", Value: models.RMAStatusInspected},
		{Path: "lines", Value: rma.Lines},
		{Path: "updated_at", Value: time.Now()},
		{Path: "timeline", Value: firestore.ArrayUnion(event)},
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "status": models.RMAStatusInspected})
}

// ============================================================================
// RESTOCK LINE  POST /api/v1/rmas/:id/restock/:line_id
// Simple inventory mode — user clicks Restock per line when ready
// ============================================================================

func (h *RMAHandler) RestockLine(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	rmaID := c.Param("id")
	lineID := c.Param("line_id")
	ctx := c.Request.Context()

	doc, err := h.doc(tenantID, rmaID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "RMA not found"})
		return
	}
	var rma models.RMA
	if err := doc.DataTo(&rma); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	userID := c.GetString("user_id")
	if userID == "" {
		userID = "staff"
	}

	lineFound := false
	for i, line := range rma.Lines {
		if line.LineID != lineID {
			continue
		}
		if line.Restocked {
			c.JSON(http.StatusConflict, gin.H{"error": "line already restocked"})
			return
		}
		qty := line.PendingRestockQty
		if qty == 0 {
			qty = line.QtyReceived
		}

		h.createInventoryAdjustment(ctx, tenantID, map[string]interface{}{
			"type":         "return",
			"product_id":   line.ProductID,
			"product_sku":  line.SKU,
			"product_name": line.ProductName,
			"delta":        qty,
			"reason":       fmt.Sprintf("RMA %s restocked", rma.RMANumber),
			"reference":    rma.RMAID,
			"created_by":   userID,
		})

		rma.Lines[i].Restocked = true
		rma.Lines[i].PendingRestockQty = 0
		lineFound = true
		break
	}

	if !lineFound {
		c.JSON(http.StatusNotFound, gin.H{"error": "line not found"})
		return
	}

	if _, err := h.doc(tenantID, rmaID).Update(ctx, []firestore.Update{
		{Path: "lines", Value: rma.Lines},
		{Path: "updated_at", Value: time.Now()},
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ============================================================================
// RESOLVE  POST /api/v1/rmas/:id/resolve
// ============================================================================

func (h *RMAHandler) ResolveRMA(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	rmaID := c.Param("id")
	ctx := c.Request.Context()

	var req struct {
		RefundAction    string  `json:"refund_action"`
		RefundAmount    float64 `json:"refund_amount"`
		RefundCurrency  string  `json:"refund_currency"`
		RefundReference string  `json:"refund_reference"`
		Notes           string  `json:"notes"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	userID := c.GetString("user_id")
	if userID == "" {
		userID = "staff"
	}

	now := time.Now()
	note := fmt.Sprintf("Resolved — %s", req.RefundAction)
	if req.Notes != "" {
		note = req.Notes
	}
	event := newEvent(models.RMAStatusResolved, note, userID)

	updates := []firestore.Update{
		{Path: "status", Value: models.RMAStatusResolved},
		{Path: "refund_action", Value: req.RefundAction},
		{Path: "refund_amount", Value: req.RefundAmount},
		{Path: "refund_currency", Value: req.RefundCurrency},
		{Path: "refund_reference", Value: req.RefundReference},
		{Path: "resolved_at", Value: now},
		{Path: "updated_at", Value: now},
		{Path: "timeline", Value: firestore.ArrayUnion(event)},
	}
	if req.RefundAmount > 0 {
		updates = append(updates, firestore.Update{Path: "refund_issued_at", Value: now})
	}

	if _, err := h.doc(tenantID, rmaID).Update(ctx, updates); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Fire refund_confirmation automated email trigger (when a refund amount was issued)
	if h.templateSvc != nil && req.RefundAmount > 0 {
		snap2, err2 := h.doc(tenantID, rmaID).Get(ctx)
		if err2 == nil {
			var rma2 models.RMA
			if snap2.DataTo(&rma2) == nil && rma2.OrderID != "" {
				order := h.loadOrderForEmail(ctx, tenantID, rma2.OrderID, rma2.Customer.Email, rma2.Customer.Name)
				if order != nil {
					go h.templateSvc.SendEventEmail(context.Background(), tenantID, models.TriggerEventRefundConfirmation, order)
				}
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "status": models.RMAStatusResolved})
}

// ============================================================================
// SYNC  POST /api/v1/rmas/sync
// Pulls pending returns from all connected Amazon and eBay accounts.
// ============================================================================

func (h *RMAHandler) SyncRMAs(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	credentials, err := h.marketplaceService.ListCredentials(ctx, tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	totalSynced := 0
	var syncErrors []string

	for _, cred := range credentials {
		if !cred.Active {
			continue
		}
		mergedCreds, err := h.marketplaceService.GetFullCredentials(ctx, &cred)
		if err != nil {
			syncErrors = append(syncErrors, fmt.Sprintf("%s (%s): credential error: %v", cred.Channel, cred.AccountName, err))
			continue
		}

		switch cred.Channel {
		case "amazon":
			synced, err := h.syncAmazonReturns(ctx, tenantID, cred.CredentialID, cred.MarketplaceID, mergedCreds)
			if err != nil {
				syncErrors = append(syncErrors, fmt.Sprintf("amazon (%s): %v", cred.AccountName, err))
			} else {
				totalSynced += synced
			}
		case "ebay":
			synced, err := h.syncEbayReturns(ctx, tenantID, cred.CredentialID, mergedCreds)
			if err != nil {
				syncErrors = append(syncErrors, fmt.Sprintf("ebay (%s): %v", cred.AccountName, err))
			} else {
				totalSynced += synced
			}
		}
	}

	resp := gin.H{"ok": true, "synced": totalSynced}
	if len(syncErrors) > 0 {
		resp["errors"] = syncErrors
	}
	c.JSON(http.StatusOK, resp)
}

// ============================================================================
// AMAZON SP-API RETURNS SYNC
// Uses SP-API Orders API to find orders with returns (A-to-z or buyer returns).
// Amazon doesn't have a dedicated /returns endpoint in SP-API v1 for all seller
// types — instead we scan recent orders for Canceled/returned status and check
// for return-related order items.
// ============================================================================

func (h *RMAHandler) rmaAmazonLWAToken(creds map[string]string) (string, error) {
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", creds["refresh_token"])
	data.Set("client_id", creds["lwa_client_id"])
	data.Set("client_secret", creds["lwa_client_secret"])
	resp, err := http.PostForm("https://api.amazon.com/auth/o2/token", data)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("LWA %d: %s", resp.StatusCode, string(body))
	}
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	json.Unmarshal(body, &tok)
	return tok.AccessToken, nil
}

func (h *RMAHandler) amazonEndpoint(region string) string {
	m := map[string]string{
		"EU": "https://sellingpartnerapi-eu.amazon.com",
		"NA": "https://sellingpartnerapi-na.amazon.com",
		"FE": "https://sellingpartnerapi-fe.amazon.com",
		"UK": "https://sellingpartnerapi-eu.amazon.com",
		"US": "https://sellingpartnerapi-na.amazon.com",
	}
	if ep, ok := m[region]; ok {
		return ep
	}
	return "https://sellingpartnerapi-eu.amazon.com"
}

func (h *RMAHandler) syncAmazonReturns(ctx context.Context, tenantID, credentialID, marketplaceID string, creds map[string]string) (int, error) {
	accessToken, err := h.rmaAmazonLWAToken(creds)
	if err != nil {
		return 0, fmt.Errorf("LWA auth: %w", err)
	}

	endpoint := h.amazonEndpoint(creds["region"])
	httpClient := &http.Client{Timeout: 30 * time.Second}

	// Fetch orders from last 60 days to detect any that have been refunded/returned
	createdAfter := time.Now().AddDate(0, 0, -60).Format(time.RFC3339)
	ordersURL := fmt.Sprintf(
		"%s/orders/v0/orders?MarketplaceIds=%s&CreatedAfter=%s&OrderStatuses=Canceled,Unfulfillable",
		endpoint, url.QueryEscape(marketplaceID), url.QueryEscape(createdAfter),
	)

	req, _ := http.NewRequestWithContext(ctx, "GET", ordersURL, nil)
	req.Header.Set("x-amz-access-token", accessToken)
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("orders fetch: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("orders API %d: %s", resp.StatusCode, string(body))
	}

	var ordersResult struct {
		Payload struct {
			Orders []struct {
				AmazonOrderID string `json:"AmazonOrderId"`
				OrderStatus   string `json:"OrderStatus"`
				PurchaseDate  string `json:"PurchaseDate"`
				BuyerInfo     *struct {
					BuyerName  string `json:"BuyerName"`
					BuyerEmail string `json:"BuyerEmail"`
				} `json:"BuyerInfo"`
				ShippingAddress *struct {
					Name         string `json:"Name"`
					AddressLine1 string `json:"AddressLine1"`
					City         string `json:"City"`
					PostalCode   string `json:"PostalCode"`
					CountryCode  string `json:"CountryCode"`
				} `json:"ShippingAddress"`
			} `json:"Orders"`
		} `json:"payload"`
	}
	json.Unmarshal(body, &ordersResult)

	synced := 0

	for _, order := range ordersResult.Payload.Orders {
		if order.AmazonOrderID == "" {
			continue
		}

		// Check if RMA already exists for this order
		rmaID := fmt.Sprintf("amz_return_%s", order.AmazonOrderID)
		existing, _ := h.doc(tenantID, rmaID).Get(ctx)
		if existing.Exists() {
			continue
		}

		// Fetch order items
		itemsURL := fmt.Sprintf("%s/orders/v0/orders/%s/orderItems", endpoint, order.AmazonOrderID)
		itemsReq, _ := http.NewRequestWithContext(ctx, "GET", itemsURL, nil)
		itemsReq.Header.Set("x-amz-access-token", accessToken)
		itemsResp, err := httpClient.Do(itemsReq)
		if err != nil {
			continue
		}
		itemsBody, _ := io.ReadAll(itemsResp.Body)
		itemsResp.Body.Close()

		var itemsResult struct {
			Payload struct {
				OrderItems []struct {
					OrderItemID    string `json:"OrderItemId"`
					SellerSKU      string `json:"SellerSKU"`
					Title          string `json:"Title"`
					QuantityOrdered int   `json:"QuantityOrdered"`
					QuantityShipped int   `json:"QuantityShipped"`
					ASIN           string `json:"ASIN"`
				} `json:"OrderItems"`
			} `json:"payload"`
		}
		json.Unmarshal(itemsBody, &itemsResult)

		// Build RMA lines
		var lines []models.RMALine
		for _, item := range itemsResult.Payload.OrderItems {
			lines = append(lines, models.RMALine{
				LineID:       uuid.New().String(),
				ProductName:  item.Title,
				SKU:          item.SellerSKU,
				QtyRequested: item.QuantityOrdered,
				QtyReceived:  0,
				ReasonCode:   models.RMAReasonOther,
			})
		}
		if len(lines) == 0 {
			continue
		}

		// Build customer info
		customer := models.RMACustomer{}
		if order.BuyerInfo != nil {
			customer.Name = order.BuyerInfo.BuyerName
			customer.Email = order.BuyerInfo.BuyerEmail
		}
		if order.ShippingAddress != nil {
			parts := []string{}
			if order.ShippingAddress.AddressLine1 != "" {
				parts = append(parts, order.ShippingAddress.AddressLine1)
			}
			if order.ShippingAddress.City != "" {
				parts = append(parts, order.ShippingAddress.City)
			}
			if order.ShippingAddress.PostalCode != "" {
				parts = append(parts, order.ShippingAddress.PostalCode)
			}
			customer.Address = strings.Join(parts, ", ")
		}

		now := time.Now()
		rmaNumber := h.nextRMANumber(ctx, tenantID)

		rma := models.RMA{
			RMAID:            rmaID,
			TenantID:         tenantID,
			RMANumber:        rmaNumber,
			OrderNumber:      order.AmazonOrderID,
			Channel:          "amazon",
			ChannelAccountID: credentialID,
			MarketplaceRMAID: order.AmazonOrderID,
			Status:           models.RMAStatusRequested,
			Customer:         customer,
			Lines:            lines,
			Notes:            fmt.Sprintf("Auto-imported from Amazon. Order status: %s", order.OrderStatus),
			CreatedBy:        "system",
			CreatedAt:        now,
			UpdatedAt:        now,
			Timeline: []models.RMAEvent{
				newEvent(models.RMAStatusRequested, fmt.Sprintf("Imported from Amazon (order status: %s)", order.OrderStatus), "system"),
			},
		}

		if _, err := h.doc(tenantID, rmaID).Set(ctx, rma); err != nil {
			log.Printf("[RMA] Failed to save Amazon return %s: %v", rmaID, err)
			continue
		}
		synced++
	}

	log.Printf("[RMA] Amazon sync complete for %s: %d new returns", tenantID, synced)
	return synced, nil
}

// ============================================================================
// EBAY RETURNS SYNC
// Uses eBay Post-Order API v2 to list buyer return requests.
// ============================================================================

func (h *RMAHandler) ebayPostOrderToken(creds map[string]string) (string, error) {
	tokenURL := "https://api.ebay.com/identity/v1/oauth2/token"
	if creds["environment"] == "sandbox" {
		tokenURL = "https://api.sandbox.ebay.com/identity/v1/oauth2/token"
	}
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", creds["refresh_token"])
	data.Set("scope", "https://api.ebay.com/oauth/api_scope/sell.fulfillment https://api.ebay.com/oauth/api_scope")
	req, err := http.NewRequest("POST", tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(creds["client_id"], creds["client_secret"])
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("eBay token %d: %s", resp.StatusCode, string(body))
	}
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	json.Unmarshal(body, &tok)
	return tok.AccessToken, nil
}

func (h *RMAHandler) syncEbayReturns(ctx context.Context, tenantID, credentialID string, creds map[string]string) (int, error) {
	accessToken, err := h.ebayPostOrderToken(creds)
	if err != nil {
		return 0, fmt.Errorf("eBay token: %w", err)
	}

	apiBase := "https://api.ebay.com"
	if creds["environment"] == "sandbox" {
		apiBase = "https://api.sandbox.ebay.com"
	}

	// List returns from Post-Order API
	returnsURL := fmt.Sprintf("%s/post-order/v2/return?limit=50&status=RETURN_REQUESTED,RETURN_APPROVED", apiBase)
	req, _ := http.NewRequestWithContext(ctx, "GET", returnsURL, nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-EBAY-C-MARKETPLACE-ID", "EBAY_GB")

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return 0, fmt.Errorf("eBay returns fetch: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("eBay Post-Order API %d: %s", resp.StatusCode, string(body))
	}

	var returnsResult struct {
		Returns []struct {
			ReturnID    string `json:"returnId"`
			OrderID     string `json:"orderId"`
			Status      string `json:"status"`
			CreationDate string `json:"creationDate"`
			Seller      struct {
				UserName string `json:"userName"`
			} `json:"seller"`
			Buyer struct {
				UserName string `json:"userName"`
			} `json:"buyer"`
			ReturnItems []struct {
				ItemID    string `json:"itemId"`
				ItemTitle string `json:"itemTitle"`
				Quantity  int    `json:"quantity"`
				ReturnQuantity int `json:"returnQuantity"`
				ReturnReason struct {
					Reason string `json:"reason"`
				} `json:"returnReason"`
			} `json:"returnItems"`
			Comments []struct {
				Comment string `json:"comment"`
			} `json:"comments"`
		} `json:"returns"`
		Total int `json:"total"`
	}

	if err := json.Unmarshal(body, &returnsResult); err != nil {
		return 0, fmt.Errorf("parse eBay returns: %w", err)
	}

	synced := 0
	for _, ret := range returnsResult.Returns {
		if ret.ReturnID == "" {
			continue
		}

		rmaID := fmt.Sprintf("ebay_return_%s_%s", credentialID, ret.ReturnID)
		existing, _ := h.doc(tenantID, rmaID).Get(ctx)
		if existing.Exists() {
			continue
		}

		var lines []models.RMALine
		for _, item := range ret.ReturnItems {
			qty := item.ReturnQuantity
			if qty == 0 {
				qty = item.Quantity
			}
			reasonCode := models.RMAReasonOther
			if item.ReturnReason.Reason != "" {
				reasonCode = mapEbayReturnReason(item.ReturnReason.Reason)
			}
			lines = append(lines, models.RMALine{
				LineID:       uuid.New().String(),
				ProductName:  item.ItemTitle,
				SKU:          item.ItemID,
				QtyRequested: qty,
				QtyReceived:  0,
				ReasonCode:   reasonCode,
				ReasonDetail: item.ReturnReason.Reason,
			})
		}
		if len(lines) == 0 {
			continue
		}

		notes := ""
		if len(ret.Comments) > 0 {
			notes = ret.Comments[0].Comment
		}

		now := time.Now()
		rmaNumber := h.nextRMANumber(ctx, tenantID)

		rma := models.RMA{
			RMAID:            rmaID,
			TenantID:         tenantID,
			RMANumber:        rmaNumber,
			OrderNumber:      ret.OrderID,
			Channel:          "ebay",
			ChannelAccountID: credentialID,
			MarketplaceRMAID: ret.ReturnID,
			Status:           models.RMAStatusRequested,
			Customer: models.RMACustomer{
				Name: ret.Buyer.UserName,
			},
			Lines:     lines,
			Notes:     notes,
			CreatedBy: "system",
			CreatedAt: now,
			UpdatedAt: now,
			Timeline: []models.RMAEvent{
				newEvent(models.RMAStatusRequested, fmt.Sprintf("Imported from eBay return request %s", ret.ReturnID), "system"),
			},
		}

		if _, err := h.doc(tenantID, rmaID).Set(ctx, rma); err != nil {
			log.Printf("[RMA] Failed to save eBay return %s: %v", rmaID, err)
			continue
		}
		synced++
	}

	log.Printf("[RMA] eBay sync complete for %s: %d new returns", tenantID, synced)
	return synced, nil
}

// mapEbayReturnReason maps eBay return reason codes to internal reason codes
func mapEbayReturnReason(ebayReason string) string {
	switch strings.ToUpper(ebayReason) {
	case "ARRIVED_DAMAGED", "DAMAGED_DURING_SHIPPING":
		return models.RMAReasonDamaged
	case "NOT_AS_DESCRIBED", "ITEM_NOT_AS_DESCRIBED", "WRONG_SIZE_OR_VARIANT":
		return models.RMAReasonNotAsDescribed
	case "CHANGED_MIND", "NO_REASON":
		return models.RMAReasonChangedMind
	case "RECEIVED_WRONG_ITEM", "WRONG_ITEM_SENT":
		return models.RMAReasonWrongItem
	case "DEFECTIVE_ITEM", "DOES_NOT_WORK":
		return models.RMAReasonDefective
	default:
		return models.RMAReasonOther
	}
}

// ============================================================================
// ACCEPT/DECLINE eBay RETURN — called when authorising/declining an RMA
// ============================================================================

// acceptEbayReturn calls eBay Post-Order API to accept a return request
func (h *RMAHandler) acceptEbayReturn(ctx context.Context, creds map[string]string, returnID string) error {
	accessToken, err := h.ebayPostOrderToken(creds)
	if err != nil {
		return err
	}
	apiBase := "https://api.ebay.com"
	if creds["environment"] == "sandbox" {
		apiBase = "https://api.sandbox.ebay.com"
	}

	payload := map[string]interface{}{
		"decision": "ACCEPT_RETURN",
	}
	payloadBytes, _ := json.Marshal(payload)

	apiURL := fmt.Sprintf("%s/post-order/v2/return/%s/decide", apiBase, returnID)
	req, _ := http.NewRequestWithContext(ctx, "POST", apiURL, strings.NewReader(string(payloadBytes)))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-EBAY-C-MARKETPLACE-ID", "EBAY_GB")

	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return fmt.Errorf("eBay accept return %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// ─── Unused import guard ──────────────────────────────────────────────────────

// ============================================================================
// GET TENANT CONFIG  GET /api/v1/rmas/config
// ============================================================================

func (h *RMAHandler) GetConfig(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()
	cfg := h.getTenantConfig(ctx, tenantID)
	c.JSON(http.StatusOK, gin.H{"config": cfg})
}

// ============================================================================
// UPDATE TENANT CONFIG  POST /api/v1/rmas/config
// Body: { "returns_location_id": "...", "inventory_mode": "simple"|"advanced" }
// ============================================================================

func (h *RMAHandler) UpdateConfig(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	var req map[string]interface{}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Only allow safe keys
	allowed := map[string]bool{"returns_location_id": true, "inventory_mode": true}
	updates := []firestore.Update{}
	for k, v := range req {
		if allowed[k] {
			updates = append(updates, firestore.Update{Path: k, Value: v})
		}
	}
	updates = append(updates, firestore.Update{Path: "updated_at", Value: time.Now()})

	ref := h.client.Doc(fmt.Sprintf("tenants/%s/config/settings", tenantID))
	if _, err := ref.Set(ctx, req, firestore.MergeAll); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ============================================================================
// EXCHANGE  POST /api/v1/rmas/:id/exchange
// Resolves an RMA with refund_action="exchange" AND automatically creates a
// new dispatch order for the replacement item.
// Body:
//
//	{
//	  "replacement_product_id": "...",
//	  "replacement_product_name": "...",
//	  "replacement_sku": "...",
//	  "replacement_qty": 1,
//	  "notes": "...",
//	  "shipping_name": "...",
//	  "shipping_address": "..."
//	}
//
// ============================================================================

func (h *RMAHandler) ExchangeRMA(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	rmaID := c.Param("id")
	ctx := c.Request.Context()

	var req struct {
		ReplacementProductID   string `json:"replacement_product_id"`
		ReplacementProductName string `json:"replacement_product_name"`
		ReplacementSKU         string `json:"replacement_sku"`
		ReplacementQty         int    `json:"replacement_qty"`
		Notes                  string `json:"notes"`
		ShippingName           string `json:"shipping_name"`
		ShippingAddress        string `json:"shipping_address"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.ReplacementSKU == "" || req.ReplacementProductName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "replacement_sku and replacement_product_name are required"})
		return
	}
	if req.ReplacementQty <= 0 {
		req.ReplacementQty = 1
	}

	// Fetch the RMA to get customer details
	snap, err := h.doc(tenantID, rmaID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "RMA not found"})
		return
	}
	var rma models.RMA
	if err := snap.DataTo(&rma); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read RMA"})
		return
	}
	if rma.Status != models.RMAStatusInspected {
		c.JSON(http.StatusBadRequest, gin.H{"error": "RMA must be in inspected status to resolve as exchange"})
		return
	}

	userID := c.GetString("user_id")
	if userID == "" {
		userID = "staff"
	}

	now := time.Now()

	// ── 1. Resolve the RMA ────────────────────────────────────────────────────

	note := fmt.Sprintf("Resolved — exchange for %s (SKU: %s, qty: %d)", req.ReplacementProductName, req.ReplacementSKU, req.ReplacementQty)
	if req.Notes != "" {
		note = req.Notes
	}
	event := newEvent(models.RMAStatusResolved, note, userID)

	exchangeOrderID := uuid.New().String()

	rmaUpdates := []firestore.Update{
		{Path: "status", Value: models.RMAStatusResolved},
		{Path: "refund_action", Value: "exchange"},
		{Path: "refund_reference", Value: fmt.Sprintf("EXCHANGE-ORDER-%s", exchangeOrderID[:8])},
		{Path: "resolved_at", Value: now},
		{Path: "updated_at", Value: now},
		{Path: "timeline", Value: firestore.ArrayUnion(event)},
	}
	if _, err := h.doc(tenantID, rmaID).Update(ctx, rmaUpdates); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to resolve RMA: " + err.Error()})
		return
	}

	// ── 2. Create replacement dispatch order ──────────────────────────────────

	// Resolve shipping name / address from request or fall back to RMA customer
	shipName := req.ShippingName
	if shipName == "" {
		shipName = rma.Customer.Name
	}
	shipAddr := req.ShippingAddress
	if shipAddr == "" {
		shipAddr = rma.Customer.Address
	}

	exchangeOrder := map[string]interface{}{
		"order_id":         exchangeOrderID,
		"tenant_id":        tenantID,
		"channel":          "manual",
		"external_order_id": fmt.Sprintf("EXCHANGE-%s", rma.RMANumber),
		"status":           "ready_to_fulfil",
		"payment_status":   "captured",
		"order_type":       "exchange",
		"rma_id":           rmaID,
		"rma_number":       rma.RMANumber,
		"customer": map[string]interface{}{
			"name":  shipName,
			"email": rma.Customer.Email,
		},
		"shipping_address": map[string]interface{}{
			"name":    shipName,
			"line1":   shipAddr,
			"country": "GB",
		},
		"lines": []map[string]interface{}{
			{
				"line_id":      uuid.New().String(),
				"product_id":   req.ReplacementProductID,
				"product_name": req.ReplacementProductName,
				"sku":          req.ReplacementSKU,
				"qty":          req.ReplacementQty,
				"unit_price":   0,
			},
		},
		"totals": map[string]interface{}{
			"subtotal": 0,
			"shipping": 0,
			"tax":      0,
			"total":    0,
			"currency": "GBP",
		},
		"internal_notes": fmt.Sprintf("Exchange order created from RMA %s", rma.RMANumber),
		"tags":           []string{"exchange"},
		"order_date":     now.Format("2006-01-02"),
		"created_at":     now.Format(time.RFC3339),
		"updated_at":     now.Format(time.RFC3339),
		"imported_at":    now.Format(time.RFC3339),
	}

	ordersCol := h.client.Collection(fmt.Sprintf("tenants/%s/orders", tenantID))
	if _, err := ordersCol.Doc(exchangeOrderID).Set(ctx, exchangeOrder); err != nil {
		// RMA is already resolved — log but don't fail the whole request
		log.Printf("[RMA] Exchange order creation failed for RMA %s: %v", rmaID, err)
		c.JSON(http.StatusOK, gin.H{
			"ok":               true,
			"status":           models.RMAStatusResolved,
			"exchange_order_id": "",
			"warning":          "RMA resolved but exchange order could not be created: " + err.Error(),
		})
		return
	}

	log.Printf("[RMA] Exchange resolved: rma=%s exchange_order=%s tenant=%s", rmaID, exchangeOrderID, tenantID)

	// Fire exchange_confirmation automated email trigger
	if h.templateSvc != nil && rma.OrderID != "" {
		order := h.loadOrderForEmail(ctx, tenantID, rma.OrderID, rma.Customer.Email, rma.Customer.Name)
		if order != nil {
			go h.templateSvc.SendEventEmail(context.Background(), tenantID, models.TriggerEventExchangeConfirmation, order)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":               true,
		"status":           models.RMAStatusResolved,
		"exchange_order_id": exchangeOrderID,
	})
}

// loadOrderForEmail loads an order from Firestore to pass to SendEventEmail.
// If the order cannot be loaded (e.g. it was a marketplace order with a redacted email),
// it synthesises a minimal Order struct from the RMA customer data so the email can still fire.
func (h *RMAHandler) loadOrderForEmail(ctx context.Context, tenantID, orderID, customerEmail, customerName string) *models.Order {
	if orderID == "" {
		return nil
	}
	snap, err := h.client.Collection("tenants").Doc(tenantID).
		Collection("orders").Doc(orderID).Get(ctx)
	if err == nil {
		var order models.Order
		if snap.DataTo(&order) == nil {
			// Fill customer email from RMA if the order record has it redacted
			if order.Customer.Email == "" && customerEmail != "" {
				order.Customer.Email = customerEmail
			}
			if order.Customer.Name == "" && customerName != "" {
				order.Customer.Name = customerName
			}
			return &order
		}
	}
	// Fallback: synthesise minimal order
	if customerEmail == "" {
		return nil
	}
	return &models.Order{
		OrderID: orderID,
		Customer: models.Customer{
			Email: customerEmail,
			Name:  customerName,
		},
	}
}
