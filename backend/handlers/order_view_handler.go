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
// ORDER VIEW HANDLER — B-007
// ============================================================================
// Saved order views — per-user column + filter presets.
// Stored in: tenants/{tenant_id}/order_views
// Scoped to user via user_id (set by tenant middleware from Firebase token).
// Follows the exact same pattern as inventory_view_handler.go
// ============================================================================

type OrderViewHandler struct {
	client *firestore.Client
}

func NewOrderViewHandler(client *firestore.Client) *OrderViewHandler {
	return &OrderViewHandler{client: client}
}

type OrderView struct {
	ViewID    string                 `firestore:"view_id"    json:"view_id"`
	TenantID  string                 `firestore:"tenant_id"  json:"tenant_id"`
	UserID    string                 `firestore:"user_id"    json:"user_id"`
	Name      string                 `firestore:"name"       json:"name"`
	Columns   []string               `firestore:"columns"    json:"columns"`
	Filters   map[string]interface{} `firestore:"filters"    json:"filters"`
	SortField string                 `firestore:"sort_field" json:"sort_field"`
	SortDir   string                 `firestore:"sort_dir"   json:"sort_dir"`
	Position  int                    `firestore:"position"   json:"position"`
	CreatedAt time.Time              `firestore:"created_at" json:"created_at"`
	UpdatedAt time.Time              `firestore:"updated_at" json:"updated_at"`
}

func (h *OrderViewHandler) col(tenantID string) *firestore.CollectionRef {
	return h.client.Collection("tenants").Doc(tenantID).Collection("order_views")
}

// ── GET /api/v1/order-views ──────────────────────────────────────────────────
func (h *OrderViewHandler) List(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	userID := c.GetString("user_id")
	ctx := c.Request.Context()

	q := h.col(tenantID).OrderBy("position", firestore.Asc)
	if userID != "" {
		q = h.col(tenantID).Where("user_id", "==", userID).OrderBy("position", firestore.Asc)
	}

	var views []OrderView
	iter := q.Documents(ctx)
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list order views"})
			return
		}
		var v OrderView
		doc.DataTo(&v)
		views = append(views, v)
	}
	if views == nil {
		views = []OrderView{}
	}
	c.JSON(http.StatusOK, gin.H{"views": views})
}

// ── POST /api/v1/order-views ─────────────────────────────────────────────────
func (h *OrderViewHandler) Create(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	userID := c.GetString("user_id")
	ctx := c.Request.Context()

	var req struct {
		Name      string                 `json:"name"       binding:"required"`
		Columns   []string               `json:"columns"`
		Filters   map[string]interface{} `json:"filters"`
		SortField string                 `json:"sort_field"`
		SortDir   string                 `json:"sort_dir"`
		Position  int                    `json:"position"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	now := time.Now()
	view := OrderView{
		ViewID:    uuid.New().String(),
		TenantID:  tenantID,
		UserID:    userID,
		Name:      req.Name,
		Columns:   req.Columns,
		Filters:   req.Filters,
		SortField: req.SortField,
		SortDir:   req.SortDir,
		Position:  req.Position,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if view.Columns == nil {
		view.Columns = []string{}
	}
	if view.Filters == nil {
		view.Filters = map[string]interface{}{}
	}

	_, err := h.col(tenantID).Doc(view.ViewID).Set(ctx, view)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create order view"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"view": view})
}

// ── PUT /api/v1/order-views/:id ───────────────────────────────────────────────
func (h *OrderViewHandler) Update(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	viewID := c.Param("id")
	ctx := c.Request.Context()

	var req struct {
		Name      *string                `json:"name"`
		Columns   []string               `json:"columns"`
		Filters   map[string]interface{} `json:"filters"`
		SortField *string                `json:"sort_field"`
		SortDir   *string                `json:"sort_dir"`
		Position  *int                   `json:"position"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	updates := []firestore.Update{{Path: "updated_at", Value: time.Now()}}
	if req.Name != nil {
		updates = append(updates, firestore.Update{Path: "name", Value: *req.Name})
	}
	if req.Columns != nil {
		updates = append(updates, firestore.Update{Path: "columns", Value: req.Columns})
	}
	if req.Filters != nil {
		updates = append(updates, firestore.Update{Path: "filters", Value: req.Filters})
	}
	if req.SortField != nil {
		updates = append(updates, firestore.Update{Path: "sort_field", Value: *req.SortField})
	}
	if req.SortDir != nil {
		updates = append(updates, firestore.Update{Path: "sort_dir", Value: *req.SortDir})
	}
	if req.Position != nil {
		updates = append(updates, firestore.Update{Path: "position", Value: *req.Position})
	}

	_, err := h.col(tenantID).Doc(viewID).Update(ctx, updates)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update order view"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"updated": true})
}

// ── DELETE /api/v1/order-views/:id ────────────────────────────────────────────
func (h *OrderViewHandler) Delete(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	viewID := c.Param("id")
	ctx := c.Request.Context()

	_, err := h.col(tenantID).Doc(viewID).Delete(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete order view"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}
