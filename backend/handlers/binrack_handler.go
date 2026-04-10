package handlers

import (
	"context"
	"net/http"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"
)

// ============================================================================
// BINRACK / WMS HANDLER
// ============================================================================

type BinrackHandler struct {
	client *firestore.Client
}

func NewBinrackHandler(client *firestore.Client) *BinrackHandler {
	return &BinrackHandler{client: client}
}

// ── Data models ───────────────────────────────────────────────────────────────

type Binrack struct {
	BinrackID    string    `firestore:"binrack_id"    json:"binrack_id"`
	TenantID     string    `firestore:"tenant_id"     json:"tenant_id"`
	LocationID   string    `firestore:"location_id"   json:"location_id"`
	Name         string    `firestore:"name"          json:"name"`
	BinrackType  string    `firestore:"binrack_type"  json:"binrack_type"` // pick|replenishment|long_term|bulk
	Capacity     int       `firestore:"capacity"      json:"capacity"`
	CurrentFill  int       `firestore:"current_fill"  json:"current_fill"`
	ZoneID       string    `firestore:"zone_id"       json:"zone_id"`
	Barcode      string    `firestore:"barcode"       json:"barcode"`
	Status       string    `firestore:"status"        json:"status"` // available|occupied|locked (extended from active|inactive)
	CreatedAt    time.Time `firestore:"created_at"    json:"created_at"`
	UpdatedAt    time.Time `firestore:"updated_at"    json:"updated_at"`

	// Session 4: Storage Group
	StorageGroupID string `firestore:"storage_group_id" json:"storage_group_id,omitempty"`
	BinTypeID      string `firestore:"bin_type_id"      json:"bin_type_id,omitempty"`

	// Session 5: Extended fields
	LengthCm       float64  `firestore:"length_cm"        json:"length_cm,omitempty"`
	WidthCm        float64  `firestore:"width_cm"         json:"width_cm,omitempty"`
	HeightCm       float64  `firestore:"height_cm"        json:"height_cm,omitempty"`
	MaxWeightKg    float64  `firestore:"max_weight_kg"    json:"max_weight_kg,omitempty"`
	MaxVolumeCm3   float64  `firestore:"max_volume_cm3"   json:"max_volume_cm3,omitempty"`
	ItemRestrictions []string `firestore:"item_restrictions" json:"item_restrictions,omitempty"`
	Aisle          string   `firestore:"aisle"            json:"aisle,omitempty"`
	Section        string   `firestore:"section"          json:"section,omitempty"`
	Level          string   `firestore:"level"            json:"level,omitempty"`
	BinNumber      string   `firestore:"bin_number"       json:"bin_number,omitempty"`
}

type WarehouseZone struct {
	ZoneID     string    `firestore:"zone_id"     json:"zone_id"`
	TenantID   string    `firestore:"tenant_id"   json:"tenant_id"`
	Name       string    `firestore:"name"        json:"name"`
	LocationID string    `firestore:"location_id" json:"location_id"`
	ZoneType   string    `firestore:"zone_type"   json:"zone_type,omitempty"`  // Standard|Refrigerated|Hazardous|Valuable|High Shelf|Floor
	Colour     string    `firestore:"colour"      json:"colour,omitempty"`      // hex colour e.g. #4CAF50
	CreatedAt  time.Time `firestore:"created_at"  json:"created_at"`
}

// ── Firestore helpers ─────────────────────────────────────────────────────────

func (h *BinrackHandler) binrackCol(tenantID string) *firestore.CollectionRef {
	return h.client.Collection("tenants").Doc(tenantID).Collection("binracks")
}

func (h *BinrackHandler) zoneCol(tenantID string) *firestore.CollectionRef {
	return h.client.Collection("tenants").Doc(tenantID).Collection("warehouse_zones")
}

// ── POST /api/v1/locations/:id/binracks ──────────────────────────────────────

func (h *BinrackHandler) CreateBinrack(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	locationID := c.Param("id")
	ctx := c.Request.Context()

	var req struct {
		Name             string   `json:"name" binding:"required"`
		BinrackType      string   `json:"binrack_type"`
		Capacity         int      `json:"capacity"`
		ZoneID           string   `json:"zone_id"`
		Barcode          string   `json:"barcode"`
		StorageGroupID   string   `json:"storage_group_id"`
		BinTypeID        string   `json:"bin_type_id"`
		LengthCm         float64  `json:"length_cm"`
		WidthCm          float64  `json:"width_cm"`
		HeightCm         float64  `json:"height_cm"`
		MaxWeightKg      float64  `json:"max_weight_kg"`
		MaxVolumeCm3     float64  `json:"max_volume_cm3"`
		ItemRestrictions []string `json:"item_restrictions"`
		Aisle            string   `json:"aisle"`
		Section          string   `json:"section"`
		Level            string   `json:"level"`
		BinNumber        string   `json:"bin_number"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	now := time.Now()
	br := Binrack{
		BinrackID:        "bin_" + uuid.New().String(),
		TenantID:         tenantID,
		LocationID:       locationID,
		Name:             req.Name,
		BinrackType:      req.BinrackType,
		Capacity:         req.Capacity,
		ZoneID:           req.ZoneID,
		Barcode:          req.Barcode,
		Status:           "available",
		StorageGroupID:   req.StorageGroupID,
		BinTypeID:        req.BinTypeID,
		LengthCm:         req.LengthCm,
		WidthCm:          req.WidthCm,
		HeightCm:         req.HeightCm,
		MaxWeightKg:      req.MaxWeightKg,
		MaxVolumeCm3:     req.MaxVolumeCm3,
		ItemRestrictions: req.ItemRestrictions,
		Aisle:            req.Aisle,
		Section:          req.Section,
		Level:            req.Level,
		BinNumber:        req.BinNumber,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if br.BinrackType == "" {
		br.BinrackType = "pick"
	}
	if br.ItemRestrictions == nil {
		br.ItemRestrictions = []string{}
	}

	if _, err := h.binrackCol(tenantID).Doc(br.BinrackID).Set(ctx, br); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create binrack"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"binrack": br})
}

// ── GET /api/v1/locations/:id/binracks ───────────────────────────────────────

func (h *BinrackHandler) ListBinracks(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	locationID := c.Param("id")
	ctx := c.Request.Context()

	var list []Binrack
	iter := h.binrackCol(tenantID).Where("location_id", "==", locationID).Documents(ctx)
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list binracks"})
			return
		}
		var br Binrack
		doc.DataTo(&br)
		list = append(list, br)
	}
	if list == nil {
		list = []Binrack{}
	}
	c.JSON(http.StatusOK, gin.H{"binracks": list})
}

// ── PUT /api/v1/binracks/:binrack_id ─────────────────────────────────────────

func (h *BinrackHandler) UpdateBinrack(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	binrackID := c.Param("binrack_id")
	ctx := c.Request.Context()

	doc, err := h.binrackCol(tenantID).Doc(binrackID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "binrack not found"})
		return
	}
	var br Binrack
	doc.DataTo(&br)

	var req struct {
		Name             *string  `json:"name"`
		BinrackType      *string  `json:"binrack_type"`
		Capacity         *int     `json:"capacity"`
		ZoneID           *string  `json:"zone_id"`
		Barcode          *string  `json:"barcode"`
		Status           *string  `json:"status"`
		StorageGroupID   *string  `json:"storage_group_id"`
		BinTypeID        *string  `json:"bin_type_id"`
		LengthCm         *float64 `json:"length_cm"`
		WidthCm          *float64 `json:"width_cm"`
		HeightCm         *float64 `json:"height_cm"`
		MaxWeightKg      *float64 `json:"max_weight_kg"`
		MaxVolumeCm3     *float64 `json:"max_volume_cm3"`
		ItemRestrictions []string `json:"item_restrictions"`
		Aisle            *string  `json:"aisle"`
		Section          *string  `json:"section"`
		Level            *string  `json:"level"`
		BinNumber        *string  `json:"bin_number"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Name != nil { br.Name = *req.Name }
	if req.BinrackType != nil { br.BinrackType = *req.BinrackType }
	if req.Capacity != nil { br.Capacity = *req.Capacity }
	if req.ZoneID != nil { br.ZoneID = *req.ZoneID }
	if req.Barcode != nil { br.Barcode = *req.Barcode }
	if req.Status != nil { br.Status = *req.Status }
	if req.StorageGroupID != nil { br.StorageGroupID = *req.StorageGroupID }
	if req.BinTypeID != nil { br.BinTypeID = *req.BinTypeID }
	if req.LengthCm != nil { br.LengthCm = *req.LengthCm }
	if req.WidthCm != nil { br.WidthCm = *req.WidthCm }
	if req.HeightCm != nil { br.HeightCm = *req.HeightCm }
	if req.MaxWeightKg != nil { br.MaxWeightKg = *req.MaxWeightKg }
	if req.MaxVolumeCm3 != nil { br.MaxVolumeCm3 = *req.MaxVolumeCm3 }
	if req.ItemRestrictions != nil { br.ItemRestrictions = req.ItemRestrictions }
	if req.Aisle != nil { br.Aisle = *req.Aisle }
	if req.Section != nil { br.Section = *req.Section }
	if req.Level != nil { br.Level = *req.Level }
	if req.BinNumber != nil { br.BinNumber = *req.BinNumber }
	br.UpdatedAt = time.Now()

	if _, err := h.binrackCol(tenantID).Doc(binrackID).Set(ctx, br); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"binrack": br})
}

// ── DELETE /api/v1/binracks/:binrack_id ──────────────────────────────────────

func (h *BinrackHandler) DeleteBinrack(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	binrackID := c.Param("binrack_id")
	ctx := c.Request.Context()

	if _, err := h.binrackCol(tenantID).Doc(binrackID).Delete(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}

// ── POST /api/v1/stock/move ───────────────────────────────────────────────────

type StockMoveRequest struct {
	ProductID    string `json:"product_id" binding:"required"`
	FromBinrack  string `json:"from_binrack" binding:"required"`
	ToBinrack    string `json:"to_binrack" binding:"required"`
	Quantity     int    `json:"quantity" binding:"required,min=1"`
	Reason       string `json:"reason"`
}

func (h *BinrackHandler) MoveStock(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()

	var req StockMoveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Firestore transaction to move stock between binracks
	err := h.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		fromRef := h.binrackCol(tenantID).Doc(req.FromBinrack)
		toRef := h.binrackCol(tenantID).Doc(req.ToBinrack)

		fromDoc, err := tx.Get(fromRef)
		if err != nil {
			return err
		}
		toDoc, err := tx.Get(toRef)
		if err != nil {
			return err
		}

		var from, to Binrack
		fromDoc.DataTo(&from)
		toDoc.DataTo(&to)

		if from.CurrentFill < req.Quantity {
			return &insufficientStockError{available: from.CurrentFill, requested: req.Quantity}
		}

		from.CurrentFill -= req.Quantity
		to.CurrentFill += req.Quantity
		from.UpdatedAt = time.Now()
		to.UpdatedAt = time.Now()

		tx.Set(fromRef, from)
		tx.Set(toRef, to)
		return nil
	})

	if err != nil {
		if ise, ok := err.(*insufficientStockError); ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "insufficient stock", "available": ise.available, "requested": ise.requested})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "stock move failed: " + err.Error()})
		return
	}

	// Log the stock move to stock_moves collection (Session 6)
	moveID := "sm_" + uuid.New().String()
	movedBy := c.GetString("user_id")
	if movedBy == "" { movedBy = "system" }
	_, _ = h.client.Collection("tenants").Doc(tenantID).Collection("stock_moves").Doc(moveID).Set(ctx, map[string]interface{}{
		"id":           moveID,
		"tenant_id":    tenantID,
		"from_binrack": req.FromBinrack,
		"to_binrack":   req.ToBinrack,
		"sku":          req.ProductID,
		"quantity":     req.Quantity,
		"status":       "complete",
		"moved_at":     time.Now(),
		"moved_by":     movedBy,
		"reason":       req.Reason,
	})

	c.JSON(http.StatusOK, gin.H{"moved": true, "product_id": req.ProductID, "quantity": req.Quantity, "move_id": moveID})
}

type insufficientStockError struct {
	available int
	requested int
}

func (e *insufficientStockError) Error() string {
	return "insufficient stock"
}

// ── GET /api/v1/warehouse/replenishment ──────────────────────────────────────

func (h *BinrackHandler) GetReplenishment(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()

	// Return pick bins where current_fill is less than 20% of capacity
	var list []Binrack
	iter := h.binrackCol(tenantID).
		Where("binrack_type", "==", "pick").
		Where("status", "==", "active").
		Documents(ctx)
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		var br Binrack
		doc.DataTo(&br)
		if br.Capacity > 0 && br.CurrentFill < br.Capacity/5 {
			list = append(list, br)
		}
	}
	if list == nil {
		list = []Binrack{}
	}
	c.JSON(http.StatusOK, gin.H{"replenishment_needed": list})
}

// ── ZONES ─────────────────────────────────────────────────────────────────────

func (h *BinrackHandler) ListZones(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()

	var zones []WarehouseZone
	iter := h.zoneCol(tenantID).Documents(ctx)
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		var z WarehouseZone
		doc.DataTo(&z)
		zones = append(zones, z)
	}
	if zones == nil {
		zones = []WarehouseZone{}
	}
	c.JSON(http.StatusOK, gin.H{"zones": zones})
}

func (h *BinrackHandler) CreateZone(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()

	var req struct {
		Name       string `json:"name" binding:"required"`
		LocationID string `json:"location_id"`
		ZoneType   string `json:"zone_type"`
		Colour     string `json:"colour"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	zone := WarehouseZone{
		ZoneID:     "zone_" + uuid.New().String(),
		TenantID:   tenantID,
		Name:       req.Name,
		LocationID: req.LocationID,
		ZoneType:   req.ZoneType,
		Colour:     req.Colour,
		CreatedAt:  time.Now(),
	}
	if _, err := h.zoneCol(tenantID).Doc(zone.ZoneID).Set(ctx, zone); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create zone"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"zone": zone})
}

func (h *BinrackHandler) DeleteZone(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	zoneID := c.Param("zone_id")
	ctx := c.Request.Context()

	if _, err := h.zoneCol(tenantID).Doc(zoneID).Delete(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete zone"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}

// ── PUT /api/v1/warehouse/zones/:zone_id ─────────────────────────────────────
func (h *BinrackHandler) UpdateZone(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	zoneID := c.Param("zone_id")
	ctx := c.Request.Context()

	doc, err := h.zoneCol(tenantID).Doc(zoneID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "zone not found"})
		return
	}
	var zone WarehouseZone
	doc.DataTo(&zone)

	var req struct {
		Name     *string `json:"name"`
		ZoneType *string `json:"zone_type"`
		Colour   *string `json:"colour"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Name != nil { zone.Name = *req.Name }
	if req.ZoneType != nil { zone.ZoneType = *req.ZoneType }
	if req.Colour != nil { zone.Colour = *req.Colour }

	if _, err := h.zoneCol(tenantID).Doc(zoneID).Set(ctx, zone); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update zone"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"zone": zone})
}

// ── GET /api/v1/warehouse/binrack/:id/items ──────────────────────────────────
func (h *BinrackHandler) GetBinrackItems(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	binrackID := c.Param("id")
	ctx := c.Request.Context()

	type BinrackItem struct {
		SKU         string `firestore:"sku"          json:"sku"`
		ProductName string `firestore:"product_name" json:"product_name"`
		Quantity    int    `firestore:"quantity"     json:"quantity"`
		BinrackID   string `firestore:"binrack_id"   json:"binrack_id"`
		BatchID     string `firestore:"batch_id"     json:"batch_id,omitempty"`
	}

	var items []BinrackItem
	iter := h.client.Collection("tenants").Doc(tenantID).Collection("inventory").
		Where("binrack_id", "==", binrackID).
		Where("quantity", ">", 0).
		Documents(ctx)
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if err == iterator.Done { break }
		if err != nil { break }
		var item BinrackItem
		doc.DataTo(&item)
		items = append(items, item)
	}
	if items == nil { items = []BinrackItem{} }
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// ── GET /api/v1/stock/moves ──────────────────────────────────────────────────
// Session 6: List stock moves with optional filter tabs (all|today|this_week)

type StockMove struct {
	ID          string      `firestore:"id"           json:"id"`
	TenantID    string      `firestore:"tenant_id"    json:"tenant_id"`
	FromBinrack string      `firestore:"from_binrack" json:"from_binrack"`
	ToBinrack   string      `firestore:"to_binrack"   json:"to_binrack"`
	SKU         string      `firestore:"sku"          json:"sku"`
	Quantity    int         `firestore:"quantity"     json:"quantity"`
	Status      string      `firestore:"status"       json:"status"` // complete
	MovedAt     interface{} `firestore:"moved_at"     json:"moved_at"`
	MovedBy     string      `firestore:"moved_by"     json:"moved_by"`
	Reason      string      `firestore:"reason"       json:"reason"`
}

func (h *BinrackHandler) ListStockMoves(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()

	var moves []StockMove
	iter := h.client.Collection("tenants").Doc(tenantID).Collection("stock_moves").
		OrderBy("moved_at", firestore.Desc).Limit(500).Documents(ctx)
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if err == iterator.Done { break }
		if err != nil { break }
		var m StockMove
		doc.DataTo(&m)
		moves = append(moves, m)
	}
	if moves == nil { moves = []StockMove{} }
	c.JSON(http.StatusOK, gin.H{"moves": moves})
}
