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
// STORAGE GROUP HANDLER — P0.5
// ============================================================================
// Named groups that can be assigned to binracks and products.
// Stored in: tenants/{tenant_id}/storage_groups
// Pre-seeded defaults: Ambient, Chilled, Frozen, High Value, Short Life, Hazardous
// ============================================================================

type StorageGroupHandler struct {
	client *firestore.Client
}

func NewStorageGroupHandler(client *firestore.Client) *StorageGroupHandler {
	return &StorageGroupHandler{client: client}
}

type StorageGroup struct {
	GroupID     string    `firestore:"group_id"    json:"group_id"`
	TenantID    string    `firestore:"tenant_id"   json:"tenant_id"`
	Name        string    `firestore:"name"        json:"name"`
	Description string    `firestore:"description" json:"description"`
	CreatedAt   time.Time `firestore:"created_at"  json:"created_at"`
}

var defaultStorageGroups = []struct{ name, desc string }{
	{"Ambient", "Standard ambient-temperature storage"},
	{"Chilled", "Refrigerated storage (2–8°C)"},
	{"Frozen", "Frozen storage (< -18°C)"},
	{"High Value", "High-value items requiring additional security"},
	{"Short Life", "Products with short expiry windows"},
	{"Hazardous", "Hazardous materials requiring special handling"},
}

func (h *StorageGroupHandler) col(tenantID string) *firestore.CollectionRef {
	return h.client.Collection("tenants").Doc(tenantID).Collection("storage_groups")
}

// ── GET /api/v1/storage-groups ────────────────────────────────────────────────
func (h *StorageGroupHandler) List(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()

	var groups []StorageGroup
	iter := h.col(tenantID).OrderBy("name", firestore.Asc).Documents(ctx)
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list storage groups"})
			return
		}
		var g StorageGroup
		doc.DataTo(&g)
		groups = append(groups, g)
	}

	// Auto-seed defaults on first access
	if len(groups) == 0 {
		now := time.Now()
		for _, d := range defaultStorageGroups {
			g := StorageGroup{
				GroupID:     uuid.New().String(),
				TenantID:    tenantID,
				Name:        d.name,
				Description: d.desc,
				CreatedAt:   now,
			}
			h.col(tenantID).Doc(g.GroupID).Set(ctx, g)
			groups = append(groups, g)
		}
	}

	c.JSON(http.StatusOK, gin.H{"storage_groups": groups})
}

// ── POST /api/v1/storage-groups ───────────────────────────────────────────────
func (h *StorageGroupHandler) Create(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()

	var req struct {
		Name        string `json:"name"        binding:"required"`
		Description string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	g := StorageGroup{
		GroupID:     uuid.New().String(),
		TenantID:    tenantID,
		Name:        req.Name,
		Description: req.Description,
		CreatedAt:   time.Now(),
	}
	if _, err := h.col(tenantID).Doc(g.GroupID).Set(ctx, g); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create storage group"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"storage_group": g})
}

// ── PUT /api/v1/storage-groups/:id ────────────────────────────────────────────
func (h *StorageGroupHandler) Update(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	groupID := c.Param("id")
	ctx := c.Request.Context()

	var req struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var updates []firestore.Update
	if req.Name != nil {
		updates = append(updates, firestore.Update{Path: "name", Value: *req.Name})
	}
	if req.Description != nil {
		updates = append(updates, firestore.Update{Path: "description", Value: *req.Description})
	}
	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no fields to update"})
		return
	}

	if _, err := h.col(tenantID).Doc(groupID).Update(ctx, updates); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update storage group"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"updated": true})
}

// ── DELETE /api/v1/storage-groups/:id ─────────────────────────────────────────
func (h *StorageGroupHandler) Delete(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	groupID := c.Param("id")
	ctx := c.Request.Context()

	if _, err := h.col(tenantID).Doc(groupID).Delete(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete storage group"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}
