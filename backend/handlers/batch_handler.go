package handlers

import (
	"net/http"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"
)

// ============================================================================
// BATCH HANDLER — Batched Items (Expiry / Lot Tracking)
// ============================================================================

type BatchHandler struct {
	client *firestore.Client
}

func NewBatchHandler(client *firestore.Client) *BatchHandler {
	return &BatchHandler{client: client}
}

// ── Data model ───────────────────────────────────────────────────────────────

type Batch struct {
	BatchID          string     `firestore:"batch_id"           json:"batch_id"`
	TenantID         string     `firestore:"tenant_id"          json:"tenant_id"`
	ProductID        string     `firestore:"product_id"         json:"product_id"`
	SKU              string     `firestore:"sku"                json:"sku"`
	BatchNumber      string     `firestore:"batch_number"       json:"batch_number"`
	Quantity         int        `firestore:"quantity"           json:"quantity"`
	SellByDate       *time.Time `firestore:"sell_by_date"       json:"sell_by_date,omitempty"`
	ExpireOnDate     *time.Time `firestore:"expire_on_date"     json:"expire_on_date,omitempty"`
	PrioritySequence int        `firestore:"priority_sequence"  json:"priority_sequence"`
	Status           string     `firestore:"status"             json:"status"` // active | expired | consumed
	LocationID       string     `firestore:"location_id"        json:"location_id"`
	CreatedAt        time.Time  `firestore:"created_at"         json:"created_at"`
	UpdatedAt        time.Time  `firestore:"updated_at"         json:"updated_at"`
}

// ── Firestore helpers ─────────────────────────────────────────────────────────

func (h *BatchHandler) batchCol(tenantID, productID string) *firestore.CollectionRef {
	return h.client.Collection("tenants").Doc(tenantID).Collection("products").Doc(productID).Collection("batches")
}

// ── GET /api/v1/products/:id/batches ─────────────────────────────────────────

func (h *BatchHandler) ListBatches(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	productID := c.Param("id")
	ctx := c.Request.Context()

	var batches []Batch
	iter := h.batchCol(tenantID, productID).OrderBy("priority_sequence", firestore.Asc).OrderBy("expire_on_date", firestore.Asc).Documents(ctx)
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list batches"})
			return
		}
		var b Batch
		doc.DataTo(&b)
		batches = append(batches, b)
	}
	if batches == nil {
		batches = []Batch{}
	}
	c.JSON(http.StatusOK, gin.H{"batches": batches})
}

// ── POST /api/v1/products/:id/batches ────────────────────────────────────────

type CreateBatchRequest struct {
	SKU              string     `json:"sku"`
	BatchNumber      string     `json:"batch_number" binding:"required"`
	Quantity         int        `json:"quantity" binding:"required,min=0"`
	SellByDate       *time.Time `json:"sell_by_date,omitempty"`
	ExpireOnDate     *time.Time `json:"expire_on_date,omitempty"`
	PrioritySequence int        `json:"priority_sequence"`
	LocationID       string     `json:"location_id"`
}

func (h *BatchHandler) CreateBatch(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	productID := c.Param("id")
	ctx := c.Request.Context()

	var req CreateBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	now := time.Now()
	batch := Batch{
		BatchID:          "bat_" + uuid.New().String(),
		TenantID:         tenantID,
		ProductID:        productID,
		SKU:              req.SKU,
		BatchNumber:      req.BatchNumber,
		Quantity:         req.Quantity,
		SellByDate:       req.SellByDate,
		ExpireOnDate:     req.ExpireOnDate,
		PrioritySequence: req.PrioritySequence,
		Status:           "active",
		LocationID:       req.LocationID,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if _, err := h.batchCol(tenantID, productID).Doc(batch.BatchID).Set(ctx, batch); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create batch"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"batch": batch})
}

// ── PUT /api/v1/products/:id/batches/:batch_id ───────────────────────────────

type UpdateBatchRequest struct {
	Quantity         *int       `json:"quantity"`
	SellByDate       *time.Time `json:"sell_by_date,omitempty"`
	ExpireOnDate     *time.Time `json:"expire_on_date,omitempty"`
	PrioritySequence *int       `json:"priority_sequence"`
	Status           *string    `json:"status"`
	LocationID       *string    `json:"location_id"`
}

func (h *BatchHandler) UpdateBatch(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	productID := c.Param("id")
	batchID := c.Param("batch_id")
	ctx := c.Request.Context()

	doc, err := h.batchCol(tenantID, productID).Doc(batchID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "batch not found"})
		return
	}
	var batch Batch
	doc.DataTo(&batch)

	var req UpdateBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Quantity != nil {
		batch.Quantity = *req.Quantity
	}
	if req.SellByDate != nil {
		batch.SellByDate = req.SellByDate
	}
	if req.ExpireOnDate != nil {
		batch.ExpireOnDate = req.ExpireOnDate
	}
	if req.PrioritySequence != nil {
		batch.PrioritySequence = *req.PrioritySequence
	}
	if req.Status != nil {
		batch.Status = *req.Status
	}
	if req.LocationID != nil {
		batch.LocationID = *req.LocationID
	}
	batch.UpdatedAt = time.Now()

	if _, err := h.batchCol(tenantID, productID).Doc(batchID).Set(ctx, batch); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update batch"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"batch": batch})
}

// ── DELETE /api/v1/products/:id/batches/:batch_id ────────────────────────────

func (h *BatchHandler) DeleteBatch(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	productID := c.Param("id")
	batchID := c.Param("batch_id")
	ctx := c.Request.Context()

	if _, err := h.batchCol(tenantID, productID).Doc(batchID).Delete(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete batch"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}

// ── POST /api/v1/products/:id/batches/scan ───────────────────────────────────
// Accepts a barcode value (from a USB scanner or manual entry) and returns the
// matching batch for that product. Compatible with any scanner that outputs
// text — the scanner types into the barcode field as if it were a keyboard.

func (h *BatchHandler) ScanBatch(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	productID := c.Param("id")
	ctx := c.Request.Context()

	var req struct {
		Barcode string `json:"barcode" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Search batches by batch_number matching the barcode
	iter := h.batchCol(tenantID, productID).Where("batch_number", "==", req.Barcode).Limit(1).Documents(ctx)
	defer iter.Stop()
	doc, err := iter.Next()
	if err == iterator.Done {
		c.JSON(http.StatusNotFound, gin.H{"error": "no batch found with that barcode"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "scan failed"})
		return
	}
	var batch Batch
	doc.DataTo(&batch)
	c.JSON(http.StatusOK, gin.H{"batch": batch})
}
