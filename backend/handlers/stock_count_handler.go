package handlers

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"
)

// ============================================================================
// STOCK COUNT HANDLER
// ============================================================================
// Routes:
//   POST   /api/v1/stock-counts               — Create a new stock count session
//   GET    /api/v1/stock-counts               — List all stock count sessions
//   GET    /api/v1/stock-counts/:id           — Get a stock count session (with lines)
//   POST   /api/v1/stock-counts/:id/lines     — Add/update counted quantity for a SKU
//   POST   /api/v1/stock-counts/:id/commit    — Commit count: apply variance adjustments
//   POST   /api/v1/stock-counts/:id/cancel    — Cancel an in-progress count
//   DELETE /api/v1/stock-counts/:id           — Delete a draft/cancelled count
// ============================================================================

type StockCountHandler struct {
	client *firestore.Client
}

func NewStockCountHandler(client *firestore.Client) *StockCountHandler {
	return &StockCountHandler{client: client}
}

// ── Data Structures ──────────────────────────────────────────────────────────

type StockCountSession struct {
	CountID     string           `firestore:"count_id"     json:"count_id"`
	TenantID    string           `firestore:"tenant_id"    json:"tenant_id"`
	Name        string           `firestore:"name"         json:"name"`
	Status      string           `firestore:"status"       json:"status"` // draft, in_progress, committed, cancelled
	LocationID  string           `firestore:"location_id"  json:"location_id"`
	LocationName string          `firestore:"location_name" json:"location_name"`
	Lines       []StockCountLine `firestore:"lines"        json:"lines"`
	Notes       string           `firestore:"notes"        json:"notes,omitempty"`
	CreatedBy   string           `firestore:"created_by"   json:"created_by"`
	CommittedBy string           `firestore:"committed_by" json:"committed_by,omitempty"`
	CreatedAt   time.Time        `firestore:"created_at"   json:"created_at"`
	UpdatedAt   time.Time        `firestore:"updated_at"   json:"updated_at"`
	CommittedAt *time.Time       `firestore:"committed_at" json:"committed_at,omitempty"`
	TotalSKUs   int              `firestore:"total_skus"   json:"total_skus"`
	CountedSKUs int              `firestore:"counted_skus" json:"counted_skus"`
	Variances   int              `firestore:"variances"    json:"variances"` // number of lines with variance != 0
}

type StockCountLine struct {
	LineID      string    `firestore:"line_id"       json:"line_id"`
	ProductID   string    `firestore:"product_id"    json:"product_id"`
	SKU         string    `firestore:"sku"           json:"sku"`
	ProductName string    `firestore:"product_name"  json:"product_name"`
	Expected    int       `firestore:"expected"      json:"expected"`   // stock level at session start
	Counted     *int      `firestore:"counted"       json:"counted"`    // nil = not yet counted
	Variance    *int      `firestore:"variance"      json:"variance"`   // counted - expected
	Notes       string    `firestore:"notes"         json:"notes,omitempty"`
	CountedAt   *time.Time `firestore:"counted_at"   json:"counted_at,omitempty"`
	CountedBy   string    `firestore:"counted_by"    json:"counted_by,omitempty"`
}

// ── Helper ───────────────────────────────────────────────────────────────────

func (h *StockCountHandler) col(tenantID string) *firestore.CollectionRef {
	return h.client.Collection(fmt.Sprintf("tenants/%s/stock_counts", tenantID))
}

func tenantIDFromCtx(c *gin.Context) string {
	if tid := c.GetString("tenant_id"); tid != "" {
		return tid
	}
	return c.GetHeader("X-Tenant-Id")
}

// ── POST /api/v1/stock-counts ─────────────────────────────────────────────────

func (h *StockCountHandler) CreateCount(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant ID required"})
		return
	}

	var req struct {
		Name       string   `json:"name"        binding:"required"`
		LocationID string   `json:"location_id" binding:"required"`
		SKUs       []string `json:"skus"`  // if empty, loads all inventory at location
		Notes      string   `json:"notes"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()

	// Resolve location name
	locationName := req.LocationID
	locDoc, err := h.client.Collection(fmt.Sprintf("tenants/%s/warehouse_locations", tenantID)).
		Doc(req.LocationID).Get(ctx)
	if err == nil {
		if n, ok := locDoc.Data()["name"].(string); ok {
			locationName = n
		}
	}

	// Build lines from current inventory at location
	var lines []StockCountLine
	invQuery := h.client.Collection(fmt.Sprintf("tenants/%s/inventory_records", tenantID)).
		Where("location_id", "==", req.LocationID)

	if len(req.SKUs) > 0 {
		invQuery = invQuery.Where("sku", "in", req.SKUs)
	}

	iter := invQuery.Documents(ctx)
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Printf("stock_count: inventory query error: %v", err)
			break
		}
		data := doc.Data()
		sku, _ := data["sku"].(string)
		productID, _ := data["product_id"].(string)
		productName, _ := data["product_name"].(string)
		onHand := 0
		if v, ok := data["on_hand"].(int64); ok {
			onHand = int(v)
		}
		lines = append(lines, StockCountLine{
			LineID:      uuid.New().String(),
			ProductID:   productID,
			SKU:         sku,
			ProductName: productName,
			Expected:    onHand,
		})
	}

	now := time.Now()
	session := StockCountSession{
		CountID:      uuid.New().String(),
		TenantID:     tenantID,
		Name:         req.Name,
		Status:       "in_progress",
		LocationID:   req.LocationID,
		LocationName: locationName,
		Lines:        lines,
		Notes:        req.Notes,
		CreatedBy:    c.GetString("user_id"),
		CreatedAt:    now,
		UpdatedAt:    now,
		TotalSKUs:    len(lines),
		CountedSKUs:  0,
		Variances:    0,
	}

	if _, err := h.col(tenantID).Doc(session.CountID).Set(ctx, session); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create stock count"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"stock_count": session})
}

// ── GET /api/v1/stock-counts ─────────────────────────────────────────────────

func (h *StockCountHandler) ListCounts(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant ID required"})
		return
	}

	ctx := c.Request.Context()
	status := c.Query("status")

	query := h.col(tenantID).OrderBy("created_at", firestore.Desc)
	if status != "" {
		query = h.col(tenantID).Where("status", "==", status).OrderBy("created_at", firestore.Desc)
	}

	iter := query.Documents(ctx)
	defer iter.Stop()

	var counts []StockCountSession
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list stock counts"})
			return
		}
		var s StockCountSession
		if err := doc.DataTo(&s); err == nil {
			// Don't return lines in list view for performance
			s.Lines = nil
			counts = append(counts, s)
		}
	}
	if counts == nil {
		counts = []StockCountSession{}
	}

	c.JSON(http.StatusOK, gin.H{"stock_counts": counts, "count": len(counts)})
}

// ── GET /api/v1/stock-counts/:id ─────────────────────────────────────────────

func (h *StockCountHandler) GetCount(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	id := c.Param("id")

	doc, err := h.col(tenantID).Doc(id).Get(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "stock count not found"})
		return
	}

	var session StockCountSession
	if err := doc.DataTo(&session); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse stock count"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"stock_count": session})
}

// ── POST /api/v1/stock-counts/:id/lines ──────────────────────────────────────
// Record or update a counted quantity for a line

func (h *StockCountHandler) UpdateLine(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	id := c.Param("id")
	ctx := c.Request.Context()

	var req struct {
		LineID  string `json:"line_id"  binding:"required"`
		Counted int    `json:"counted"`
		Notes   string `json:"notes"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	doc, err := h.col(tenantID).Doc(id).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "stock count not found"})
		return
	}

	var session StockCountSession
	if err := doc.DataTo(&session); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse"})
		return
	}

	if session.Status != "in_progress" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "can only update lines on in_progress counts"})
		return
	}

	now := time.Now()
	userID := c.GetString("user_id")
	variance := req.Counted - 0 // default

	updated := false
	for i, line := range session.Lines {
		if line.LineID == req.LineID {
			variance = req.Counted - line.Expected
			session.Lines[i].Counted = &req.Counted
			session.Lines[i].Variance = &variance
			session.Lines[i].Notes = req.Notes
			session.Lines[i].CountedAt = &now
			session.Lines[i].CountedBy = userID
			updated = true
			break
		}
	}

	if !updated {
		c.JSON(http.StatusNotFound, gin.H{"error": "line not found"})
		return
	}

	// Recount stats
	counted := 0
	variances := 0
	for _, l := range session.Lines {
		if l.Counted != nil {
			counted++
			if l.Variance != nil && *l.Variance != 0 {
				variances++
			}
		}
	}
	session.CountedSKUs = counted
	session.Variances = variances
	session.UpdatedAt = now

	if _, err := h.col(tenantID).Doc(id).Set(ctx, session); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"stock_count": session})
}

// ── POST /api/v1/stock-counts/:id/commit ─────────────────────────────────────
// Apply all variances as stock adjustments, mark count as committed

func (h *StockCountHandler) CommitCount(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	id := c.Param("id")
	ctx := c.Request.Context()

	doc, err := h.col(tenantID).Doc(id).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "stock count not found"})
		return
	}

	var session StockCountSession
	if err := doc.DataTo(&session); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse"})
		return
	}

	if session.Status != "in_progress" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "count is not in progress"})
		return
	}

	adjustmentsApplied := 0

	// Apply adjustments for each counted line with a variance
	for _, line := range session.Lines {
		if line.Counted == nil || line.Variance == nil || *line.Variance == 0 {
			continue
		}

		// Write a stock adjustment record (the same model as AdjustStockV2)
		inventoryDocID := line.ProductID + "__" + session.LocationID
		inventoryRef := h.client.Collection(fmt.Sprintf("tenants/%s/inventory", tenantID)).Doc(inventoryDocID)
		adjustmentID := uuid.New().String()
		adjustmentRef := h.client.Collection(fmt.Sprintf("tenants/%s/inventory_adjustments", tenantID)).Doc(adjustmentID)
		delta := *line.Variance
		lineRef := line // capture

		adjErr := h.client.RunTransaction(ctx, func(ctx2 context.Context, tx *firestore.Transaction) error {
			doc, err := tx.Get(inventoryRef)
			qty := 0
			if err == nil {
				if v, ok := doc.Data()["quantity"].(int64); ok {
					qty = int(v)
				}
			}
			newQty := qty + delta
			if newQty < 0 {
				newQty = 0
			}
			if err := tx.Set(inventoryRef, map[string]interface{}{
				"product_id":    lineRef.ProductID,
				"location_id":   session.LocationID,
				"quantity":      newQty,
				"available_qty": newQty,
				"updated_at":    time.Now(),
			}, firestore.MergeAll); err != nil {
				return err
			}
			return tx.Set(adjustmentRef, map[string]interface{}{
				"adjustment_id": adjustmentID,
				"tenant_id":     tenantID,
				"product_id":    lineRef.ProductID,
				"sku":           lineRef.SKU,
				"location_id":   session.LocationID,
				"delta":         delta,
				"type":          "stock_count",
				"reason":        fmt.Sprintf("Stock count: %s", session.Name),
				"reference_id":  id,
				"created_by":    c.GetString("user_id"),
				"created_at":    time.Now(),
			})
		})
		if adjErr != nil {
			log.Printf("stock_count commit: adjustment failed for SKU %s: %v", line.SKU, adjErr)
		} else {
			adjustmentsApplied++
		}
	}

	now := time.Now()
	userID := c.GetString("user_id")
	session.Status = "committed"
	session.CommittedAt = &now
	session.CommittedBy = userID
	session.UpdatedAt = now

	if _, err := h.col(tenantID).Doc(id).Set(ctx, session); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to commit"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"stock_count":          session,
		"adjustments_applied":  adjustmentsApplied,
	})
}

// ── POST /api/v1/stock-counts/:id/cancel ─────────────────────────────────────

func (h *StockCountHandler) CancelCount(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	id := c.Param("id")
	ctx := c.Request.Context()

	_, err := h.col(tenantID).Doc(id).Update(ctx, []firestore.Update{
		{Path: "status", Value: "cancelled"},
		{Path: "updated_at", Value: time.Now()},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to cancel"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"cancelled": true})
}

// ── DELETE /api/v1/stock-counts/:id ──────────────────────────────────────────

func (h *StockCountHandler) DeleteCount(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	id := c.Param("id")

	if _, err := h.col(tenantID).Doc(id).Delete(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}
