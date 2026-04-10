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
// BIN TYPE HANDLER
//
// Routes:
//   GET    /api/v1/settings/bin-types        List bin types
//   POST   /api/v1/settings/bin-types        Create bin type
//   PUT    /api/v1/settings/bin-types/:id    Update bin type
//   DELETE /api/v1/settings/bin-types/:id    Delete bin type
// ============================================================================

type BinTypeHandler struct {
	client *firestore.Client
}

func NewBinTypeHandler(client *firestore.Client) *BinTypeHandler {
	return &BinTypeHandler{client: client}
}

type BinType struct {
	ID                      string    `firestore:"id"                        json:"id"`
	TenantID                string    `firestore:"tenant_id"                 json:"tenant_id"`
	Name                    string    `firestore:"name"                      json:"name"`
	StandardType            string    `firestore:"standard_type"             json:"standard_type"` // Standard|Oversize|Refrigerated|Hazardous|Valuable
	Colour                  string    `firestore:"colour"                    json:"colour"`          // hex colour
	VolumetricTracking      bool      `firestore:"volumetric_tracking"       json:"volumetric_tracking"`
	DefaultStockAvailability string   `firestore:"default_stock_availability" json:"default_stock_availability"` // available|restricted|unchanged
	BoundLocation           string    `firestore:"bound_location"            json:"bound_location,omitempty"`
	CreatedAt               time.Time `firestore:"created_at"                json:"created_at"`
	UpdatedAt               time.Time `firestore:"updated_at"                json:"updated_at"`
}

func (h *BinTypeHandler) col(tenantID string) *firestore.CollectionRef {
	return h.client.Collection("tenants").Doc(tenantID).Collection("bin_types")
}

// GET /api/v1/settings/bin-types
func (h *BinTypeHandler) ListBinTypes(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()

	var list []BinType
	iter := h.col(tenantID).OrderBy("name", firestore.Asc).Documents(ctx)
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list bin types"})
			return
		}
		var bt BinType
		doc.DataTo(&bt)
		list = append(list, bt)
	}
	if list == nil {
		list = []BinType{}
	}
	c.JSON(http.StatusOK, gin.H{"bin_types": list})
}

// POST /api/v1/settings/bin-types
func (h *BinTypeHandler) CreateBinType(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()

	var req struct {
		Name                    string `json:"name" binding:"required"`
		StandardType            string `json:"standard_type"`
		Colour                  string `json:"colour"`
		VolumetricTracking      bool   `json:"volumetric_tracking"`
		DefaultStockAvailability string `json:"default_stock_availability"`
		BoundLocation           string `json:"bound_location"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	now := time.Now()
	bt := BinType{
		ID:                      "bt_" + uuid.New().String(),
		TenantID:                tenantID,
		Name:                    req.Name,
		StandardType:            req.StandardType,
		Colour:                  req.Colour,
		VolumetricTracking:      req.VolumetricTracking,
		DefaultStockAvailability: req.DefaultStockAvailability,
		BoundLocation:           req.BoundLocation,
		CreatedAt:               now,
		UpdatedAt:               now,
	}
	if bt.StandardType == "" {
		bt.StandardType = "Standard"
	}
	if bt.DefaultStockAvailability == "" {
		bt.DefaultStockAvailability = "unchanged"
	}
	if bt.Colour == "" {
		bt.Colour = "#607D8B"
	}

	if _, err := h.col(tenantID).Doc(bt.ID).Set(ctx, bt); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create bin type"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"bin_type": bt})
}

// PUT /api/v1/settings/bin-types/:id
func (h *BinTypeHandler) UpdateBinType(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	id := c.Param("id")
	ctx := c.Request.Context()

	doc, err := h.col(tenantID).Doc(id).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "bin type not found"})
		return
	}
	var bt BinType
	doc.DataTo(&bt)

	var req struct {
		Name                    *string `json:"name"`
		StandardType            *string `json:"standard_type"`
		Colour                  *string `json:"colour"`
		VolumetricTracking      *bool   `json:"volumetric_tracking"`
		DefaultStockAvailability *string `json:"default_stock_availability"`
		BoundLocation           *string `json:"bound_location"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Name != nil { bt.Name = *req.Name }
	if req.StandardType != nil { bt.StandardType = *req.StandardType }
	if req.Colour != nil { bt.Colour = *req.Colour }
	if req.VolumetricTracking != nil { bt.VolumetricTracking = *req.VolumetricTracking }
	if req.DefaultStockAvailability != nil { bt.DefaultStockAvailability = *req.DefaultStockAvailability }
	if req.BoundLocation != nil { bt.BoundLocation = *req.BoundLocation }
	bt.UpdatedAt = time.Now()

	if _, err := h.col(tenantID).Doc(id).Set(ctx, bt); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update bin type"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"bin_type": bt})
}

// DELETE /api/v1/settings/bin-types/:id
func (h *BinTypeHandler) DeleteBinType(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	id := c.Param("id")
	ctx := c.Request.Context()

	if _, err := h.col(tenantID).Doc(id).Delete(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete bin type"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}
