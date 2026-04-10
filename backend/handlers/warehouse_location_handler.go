package handlers

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"
)

// ============================================================================
// WAREHOUSE LOCATION HANDLER
// ============================================================================

type WarehouseLocationHandler struct {
	client *firestore.Client
}

func NewWarehouseLocationHandler(client *firestore.Client) *WarehouseLocationHandler {
	return &WarehouseLocationHandler{client: client}
}

// ============================================================================
// DATA STRUCTURES
// ============================================================================

type WarehouseLocation struct {
	LocationID string    `firestore:"location_id" json:"location_id"`
	TenantID   string    `firestore:"tenant_id" json:"tenant_id"`
	Name       string    `firestore:"name" json:"name"`
	ParentID   string    `firestore:"parent_id" json:"parent_id"`
	SourceID   string    `firestore:"source_id" json:"source_id"`
	Path       string    `firestore:"path" json:"path"`
	Depth      int       `firestore:"depth" json:"depth"`
	IsLeaf     bool      `firestore:"is_leaf" json:"is_leaf"`
	SortOrder  int       `firestore:"sort_order" json:"sort_order"`
	Barcode    string    `firestore:"barcode" json:"barcode"`
	Active     bool      `firestore:"active" json:"active"`
	CreatedAt  time.Time `firestore:"created_at" json:"created_at"`
	UpdatedAt  time.Time `firestore:"updated_at" json:"updated_at"`
}

type WarehouseLocationNode struct {
	WarehouseLocation
	Children []*WarehouseLocationNode `json:"children,omitempty"`
	Stock    int                      `json:"stock,omitempty"`
}

type InventoryRecord struct {
	InventoryID   string    `firestore:"inventory_id" json:"inventory_id"`
	ProductID     string    `firestore:"product_id" json:"product_id"`
	LocationID    string    `firestore:"location_id" json:"location_id"`
	LocationName  string    `firestore:"location_name" json:"location_name"`
	LocationPath  string    `firestore:"location_path" json:"location_path"`
	SourceID      string    `firestore:"source_id" json:"source_id"`
	Quantity      int       `firestore:"quantity" json:"quantity"`
	ReservedQty   int       `firestore:"reserved_qty" json:"reserved_qty"`
	AvailableQty  int       `firestore:"available_qty" json:"available_qty"`
	UpdatedAt     time.Time `firestore:"updated_at" json:"updated_at"`
}

type InventoryAdjustment struct {
	AdjustmentID   string    `firestore:"adjustment_id" json:"adjustment_id"`
	ProductID      string    `firestore:"product_id" json:"product_id"`
	ProductSKU     string    `firestore:"product_sku" json:"product_sku"`
	ProductName    string    `firestore:"product_name" json:"product_name"`
	LocationID     string    `firestore:"location_id" json:"location_id"`
	LocationPath   string    `firestore:"location_path" json:"location_path"`
	Type           string    `firestore:"type" json:"type"`
	Delta          int       `firestore:"delta" json:"delta"`
	QuantityBefore int       `firestore:"quantity_before" json:"quantity_before"`
	QuantityAfter  int       `firestore:"quantity_after" json:"quantity_after"`
	Reason         string    `firestore:"reason" json:"reason"`
	Reference      string    `firestore:"reference" json:"reference"`
	PoID           string    `firestore:"po_id" json:"po_id"`
	OrderID        string    `firestore:"order_id" json:"order_id"`
	CreatedBy      string    `firestore:"created_by" json:"created_by"`
	CreatedAt      time.Time `firestore:"created_at" json:"created_at"`
}

// ============================================================================
// REQUEST STRUCTURES
// ============================================================================

type CreateLocationRequest struct {
	Name      string `json:"name" binding:"required"`
	ParentID  string `json:"parent_id"`
	SourceID  string `json:"source_id" binding:"required"`
	SortOrder int    `json:"sort_order"`
	Barcode   string `json:"barcode"`
}

type UpdateLocationRequest struct {
	Name      *string `json:"name"`
	SortOrder *int    `json:"sort_order"`
	Barcode   *string `json:"barcode"`
}

type NewAdjustStockRequest struct {
	ProductID  string `json:"product_id" binding:"required"`
	LocationID string `json:"location_id" binding:"required"`
	Delta      int    `json:"delta"`
	Reason     string `json:"reason"`
	Reference  string `json:"reference"`
	Type       string `json:"type"` // defaults to "adjustment"
	PoID       string `json:"po_id"`
	OrderID    string `json:"order_id"`
}

type TransferStockRequest struct {
	ProductID      string `json:"product_id" binding:"required"`
	FromLocationID string `json:"from_location_id" binding:"required"`
	ToLocationID   string `json:"to_location_id" binding:"required"`
	Quantity       int    `json:"quantity" binding:"required"`
	Reason         string `json:"reason"`
}

// ============================================================================
// LOCATION CRUD
// ============================================================================

// ListLocations returns the full tree for a given source_id
func (h *WarehouseLocationHandler) ListLocations(c *gin.Context) {
	tenantID := c.GetHeader("X-Tenant-Id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "X-Tenant-Id header required"})
		return
	}

	sourceID := c.Query("source_id")
	ctx := c.Request.Context()

	q := h.client.Collection(fmt.Sprintf("tenants/%s/warehouse_locations", tenantID)).
		Where("active", "==", true)
	if sourceID != "" {
		q = q.Where("source_id", "==", sourceID)
	}

	iter := q.Documents(ctx)
	var locations []WarehouseLocation
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Printf("Error fetching locations: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch locations"})
			return
		}
		var loc WarehouseLocation
		doc.DataTo(&loc)
		locations = append(locations, loc)
	}

	// Optionally fetch stock counts for leaf nodes
	stockMap := map[string]int{}
	invIter := h.client.Collection(fmt.Sprintf("tenants/%s/inventory", tenantID)).Documents(ctx)
	for {
		doc, err := invIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		var inv InventoryRecord
		doc.DataTo(&inv)
		stockMap[inv.LocationID] += inv.Quantity
	}

	tree := buildLocationTree(locations, stockMap)

	c.JSON(http.StatusOK, gin.H{
		"locations": tree,
		"count":     len(locations),
	})
}

// GetLocationChildren returns immediate children of a location
func (h *WarehouseLocationHandler) GetLocationChildren(c *gin.Context) {
	tenantID := c.GetHeader("X-Tenant-Id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "X-Tenant-Id header required"})
		return
	}

	locationID := c.Param("id")
	ctx := c.Request.Context()

	iter := h.client.Collection(fmt.Sprintf("tenants/%s/warehouse_locations", tenantID)).
		Where("parent_id", "==", locationID).
		Where("active", "==", true).
		Documents(ctx)

	var children []WarehouseLocation
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch children"})
			return
		}
		var loc WarehouseLocation
		doc.DataTo(&loc)
		children = append(children, loc)
	}

	sort.Slice(children, func(i, j int) bool {
		return children[i].SortOrder < children[j].SortOrder
	})

	c.JSON(http.StatusOK, gin.H{"children": children})
}

// CreateLocation creates a new warehouse location
func (h *WarehouseLocationHandler) CreateLocation(c *gin.Context) {
	tenantID := c.GetHeader("X-Tenant-Id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "X-Tenant-Id header required"})
		return
	}

	var req CreateLocationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	now := time.Now()
	locationID := uuid.New().String()

	var depth int
	var path string
	var parentPath string

	if req.ParentID != "" {
		// Fetch parent to compute depth & path
		parentDoc, err := h.client.Collection(fmt.Sprintf("tenants/%s/warehouse_locations", tenantID)).
			Doc(req.ParentID).Get(ctx)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Parent location not found"})
			return
		}
		var parent WarehouseLocation
		parentDoc.DataTo(&parent)
		depth = parent.Depth + 1
		parentPath = parent.Path

		// Mark parent as non-leaf since it now has a child
		_, err = parentDoc.Ref.Update(ctx, []firestore.Update{
			{Path: "is_leaf", Value: false},
			{Path: "updated_at", Value: now},
		})
		if err != nil {
			log.Printf("Failed to update parent is_leaf: %v", err)
		}
	}

	slug := locationSlugify(req.Name)
	if parentPath != "" {
		path = parentPath + "/" + slug
	} else {
		path = slug
	}

	loc := WarehouseLocation{
		LocationID: locationID,
		TenantID:   tenantID,
		Name:       req.Name,
		ParentID:   req.ParentID,
		SourceID:   req.SourceID,
		Path:       path,
		Depth:      depth,
		IsLeaf:     true, // starts as leaf until a child is added
		SortOrder:  req.SortOrder,
		Barcode:    req.Barcode,
		Active:     true,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	_, err := h.client.Collection(fmt.Sprintf("tenants/%s/warehouse_locations", tenantID)).
		Doc(locationID).Set(ctx, loc)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create location"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"location": loc})
}

// UpdateLocation updates name, sort_order, or barcode
func (h *WarehouseLocationHandler) UpdateLocation(c *gin.Context) {
	tenantID := c.GetHeader("X-Tenant-Id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "X-Tenant-Id header required"})
		return
	}

	locationID := c.Param("id")
	var req UpdateLocationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	var updates []firestore.Update
	updates = append(updates, firestore.Update{Path: "updated_at", Value: time.Now()})

	if req.Name != nil {
		updates = append(updates, firestore.Update{Path: "name", Value: *req.Name})
	}
	if req.SortOrder != nil {
		updates = append(updates, firestore.Update{Path: "sort_order", Value: *req.SortOrder})
	}
	if req.Barcode != nil {
		updates = append(updates, firestore.Update{Path: "barcode", Value: *req.Barcode})
	}

	_, err := h.client.Collection(fmt.Sprintf("tenants/%s/warehouse_locations", tenantID)).
		Doc(locationID).Update(ctx, updates)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update location"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// DeleteLocation deactivates a location (only if no stock and no active children)
func (h *WarehouseLocationHandler) DeleteLocation(c *gin.Context) {
	tenantID := c.GetHeader("X-Tenant-Id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "X-Tenant-Id header required"})
		return
	}

	locationID := c.Param("id")
	ctx := c.Request.Context()

	// Check for active children
	childIter := h.client.Collection(fmt.Sprintf("tenants/%s/warehouse_locations", tenantID)).
		Where("parent_id", "==", locationID).
		Where("active", "==", true).
		Limit(1).Documents(ctx)
	childDoc, _ := childIter.Next()
	if childDoc != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "Cannot delete location with active children"})
		return
	}

	// Check for stock
	stockIter := h.client.Collection(fmt.Sprintf("tenants/%s/inventory", tenantID)).
		Where("location_id", "==", locationID).
		Limit(1).Documents(ctx)
	stockDoc, _ := stockIter.Next()
	if stockDoc != nil {
		var inv InventoryRecord
		stockDoc.DataTo(&inv)
		if inv.Quantity > 0 {
			c.JSON(http.StatusConflict, gin.H{"error": "Cannot delete location with stock"})
			return
		}
	}

	_, err := h.client.Collection(fmt.Sprintf("tenants/%s/warehouse_locations", tenantID)).
		Doc(locationID).Update(ctx, []firestore.Update{
		{Path: "active", Value: false},
		{Path: "updated_at", Value: time.Now()},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete location"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ============================================================================
// INVENTORY QUERIES
// ============================================================================

// GetInventoryV2 returns inventory records with filters
func (h *WarehouseLocationHandler) GetInventoryV2(c *gin.Context) {
	tenantID := c.GetHeader("X-Tenant-Id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "X-Tenant-Id header required"})
		return
	}

	ctx := c.Request.Context()
	q := h.client.Collection(fmt.Sprintf("tenants/%s/inventory", tenantID)).Query

	productID := c.Query("product_id")
	locationID := c.Query("location_id")
	sourceID := c.Query("source_id")

	if productID != "" {
		q = q.Where("product_id", "==", productID)
	}
	if locationID != "" {
		q = q.Where("location_id", "==", locationID)
	}
	if sourceID != "" {
		q = q.Where("source_id", "==", sourceID)
	}

	iter := q.Documents(ctx)
	var records []InventoryRecord
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch inventory"})
			return
		}
		var rec InventoryRecord
		doc.DataTo(&rec)
		records = append(records, rec)
	}

	c.JSON(http.StatusOK, gin.H{"inventory": records, "count": len(records)})
}

// GetProductInventory returns all locations for a product
func (h *WarehouseLocationHandler) GetProductInventory(c *gin.Context) {
	tenantID := c.GetHeader("X-Tenant-Id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "X-Tenant-Id header required"})
		return
	}

	productID := c.Param("product_id")
	ctx := c.Request.Context()

	iter := h.client.Collection(fmt.Sprintf("tenants/%s/inventory", tenantID)).
		Where("product_id", "==", productID).
		Documents(ctx)

	var records []InventoryRecord
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch inventory"})
			return
		}
		var rec InventoryRecord
		doc.DataTo(&rec)
		records = append(records, rec)
	}

	c.JSON(http.StatusOK, gin.H{"inventory": records, "count": len(records)})
}

// ============================================================================
// STOCK ADJUSTMENT
// ============================================================================

// AdjustStockV2 performs a delta-based stock adjustment using the new model
func (h *WarehouseLocationHandler) AdjustStockV2(c *gin.Context) {
	tenantID := c.GetHeader("X-Tenant-Id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "X-Tenant-Id header required"})
		return
	}

	var req NewAdjustStockRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Delta == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "delta cannot be zero"})
		return
	}

	adjType := req.Type
	if adjType == "" {
		adjType = "adjustment"
	}
	if adjType == "adjustment" && req.Reason == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "reason is required for adjustments"})
		return
	}

	ctx := c.Request.Context()

	// Validate location exists and is a leaf
	locDoc, err := h.client.Collection(fmt.Sprintf("tenants/%s/warehouse_locations", tenantID)).
		Doc(req.LocationID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Location not found"})
		return
	}
	var loc WarehouseLocation
	locDoc.DataTo(&loc)
	if !loc.IsLeaf {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Stock can only be assigned to leaf locations"})
		return
	}

	// Fetch product for denormalised fields
	productSKU := ""
	productName := ""
	productDoc, err := h.client.Collection(fmt.Sprintf("tenants/%s/products", tenantID)).
		Doc(req.ProductID).Get(ctx)
	if err == nil {
		productSKU, _ = productDoc.Data()["sku"].(string)
		productName, _ = productDoc.Data()["title"].(string)
	}

	inventoryDocID := req.ProductID + "__" + req.LocationID
	inventoryRef := h.client.Collection(fmt.Sprintf("tenants/%s/inventory", tenantID)).Doc(inventoryDocID)
	adjustmentID := uuid.New().String()
	adjustmentRef := h.client.Collection(fmt.Sprintf("tenants/%s/inventory_adjustments", tenantID)).Doc(adjustmentID)

	var resultRec InventoryRecord

	err = h.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		// Get or initialise inventory record
		doc, err := tx.Get(inventoryRef)
		var rec InventoryRecord
		if err != nil {
			// New record
			rec = InventoryRecord{
				InventoryID:  inventoryDocID,
				ProductID:    req.ProductID,
				LocationID:   req.LocationID,
				LocationName: loc.Name,
				LocationPath: loc.Path,
				SourceID:     loc.SourceID,
				Quantity:     0,
				ReservedQty:  0,
				AvailableQty: 0,
			}
		} else {
			doc.DataTo(&rec)
		}

		qBefore := rec.Quantity
		qAfter := qBefore + req.Delta

		if qAfter < 0 {
			return fmt.Errorf("insufficient stock: current=%d, delta=%d", qBefore, req.Delta)
		}

		rec.Quantity = qAfter
		rec.AvailableQty = qAfter - rec.ReservedQty
		if rec.AvailableQty < 0 {
			rec.AvailableQty = 0
		}
		rec.UpdatedAt = time.Now()

		adjustment := InventoryAdjustment{
			AdjustmentID:   adjustmentID,
			ProductID:      req.ProductID,
			ProductSKU:     productSKU,
			ProductName:    productName,
			LocationID:     req.LocationID,
			LocationPath:   loc.Path,
			Type:           adjType,
			Delta:          req.Delta,
			QuantityBefore: qBefore,
			QuantityAfter:  qAfter,
			Reason:         req.Reason,
			Reference:      req.Reference,
			PoID:           req.PoID,
			OrderID:        req.OrderID,
			CreatedBy:      "system",
			CreatedAt:      time.Now(),
		}

		if err := tx.Set(inventoryRef, rec); err != nil {
			return err
		}
		if err := tx.Set(adjustmentRef, adjustment); err != nil {
			return err
		}

		resultRec = rec
		return nil
	})

	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":   true,
		"inventory": resultRec,
	})
}

// TransferStock moves stock between two leaf locations
func (h *WarehouseLocationHandler) TransferStock(c *gin.Context) {
	tenantID := c.GetHeader("X-Tenant-Id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "X-Tenant-Id header required"})
		return
	}

	var req TransferStockRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Quantity <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "quantity must be positive"})
		return
	}

	ctx := c.Request.Context()

	// Validate both locations
	fromLocDoc, err := h.client.Collection(fmt.Sprintf("tenants/%s/warehouse_locations", tenantID)).
		Doc(req.FromLocationID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "From location not found"})
		return
	}
	var fromLoc WarehouseLocation
	fromLocDoc.DataTo(&fromLoc)
	if !fromLoc.IsLeaf {
		c.JSON(http.StatusBadRequest, gin.H{"error": "From location must be a leaf node"})
		return
	}

	toLocDoc, err := h.client.Collection(fmt.Sprintf("tenants/%s/warehouse_locations", tenantID)).
		Doc(req.ToLocationID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "To location not found"})
		return
	}
	var toLoc WarehouseLocation
	toLocDoc.DataTo(&toLoc)
	if !toLoc.IsLeaf {
		c.JSON(http.StatusBadRequest, gin.H{"error": "To location must be a leaf node"})
		return
	}

	fromInvID := req.ProductID + "__" + req.FromLocationID
	toInvID := req.ProductID + "__" + req.ToLocationID
	fromRef := h.client.Collection(fmt.Sprintf("tenants/%s/inventory", tenantID)).Doc(fromInvID)
	toRef := h.client.Collection(fmt.Sprintf("tenants/%s/inventory", tenantID)).Doc(toInvID)

	refID := uuid.New().String()
	outAdjID := uuid.New().String()
	inAdjID := uuid.New().String()
	now := time.Now()

	err = h.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		// Get from record
		fromDoc, err := tx.Get(fromRef)
		var fromRec InventoryRecord
		if err != nil {
			return fmt.Errorf("no inventory record for source location")
		}
		fromDoc.DataTo(&fromRec)

		if fromRec.Quantity < req.Quantity {
			return fmt.Errorf("insufficient stock: available=%d, requested=%d", fromRec.Quantity, req.Quantity)
		}

		// Get or init to record
		toDoc, err := tx.Get(toRef)
		var toRec InventoryRecord
		if err != nil {
			toRec = InventoryRecord{
				InventoryID:  toInvID,
				ProductID:    req.ProductID,
				LocationID:   req.ToLocationID,
				LocationName: toLoc.Name,
				LocationPath: toLoc.Path,
				SourceID:     toLoc.SourceID,
			}
		} else {
			toDoc.DataTo(&toRec)
		}

		fromBefore := fromRec.Quantity
		toBefore := toRec.Quantity

		fromRec.Quantity -= req.Quantity
		fromRec.AvailableQty = fromRec.Quantity - fromRec.ReservedQty
		fromRec.UpdatedAt = now

		toRec.Quantity += req.Quantity
		toRec.AvailableQty = toRec.Quantity - toRec.ReservedQty
		toRec.UpdatedAt = now

		outAdj := InventoryAdjustment{
			AdjustmentID:   outAdjID,
			ProductID:      req.ProductID,
			LocationID:     req.FromLocationID,
			LocationPath:   fromLoc.Path,
			Type:           "transfer",
			Delta:          -req.Quantity,
			QuantityBefore: fromBefore,
			QuantityAfter:  fromRec.Quantity,
			Reason:         req.Reason,
			Reference:      refID,
			CreatedBy:      "system",
			CreatedAt:      now,
		}
		inAdj := InventoryAdjustment{
			AdjustmentID:   inAdjID,
			ProductID:      req.ProductID,
			LocationID:     req.ToLocationID,
			LocationPath:   toLoc.Path,
			Type:           "transfer",
			Delta:          req.Quantity,
			QuantityBefore: toBefore,
			QuantityAfter:  toRec.Quantity,
			Reason:         req.Reason,
			Reference:      refID,
			CreatedBy:      "system",
			CreatedAt:      now,
		}

		adjCol := h.client.Collection(fmt.Sprintf("tenants/%s/inventory_adjustments", tenantID))

		if err := tx.Set(fromRef, fromRec); err != nil {
			return err
		}
		if err := tx.Set(toRef, toRec); err != nil {
			return err
		}
		if err := tx.Set(adjCol.Doc(outAdjID), outAdj); err != nil {
			return err
		}
		if err := tx.Set(adjCol.Doc(inAdjID), inAdj); err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":      true,
		"transfer_ref": refID,
		"message":      fmt.Sprintf("Transferred %d units", req.Quantity),
	})
}

// ============================================================================
// ADJUSTMENT AUDIT TRAIL
// ============================================================================

// GetAdjustments returns the adjustment history, filtered by product or location
func (h *WarehouseLocationHandler) GetAdjustments(c *gin.Context) {
	tenantID := c.GetHeader("X-Tenant-Id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "X-Tenant-Id header required"})
		return
	}

	ctx := c.Request.Context()
	productID := c.Query("product_id")
	locationID := c.Query("location_id")

	q := h.client.Collection(fmt.Sprintf("tenants/%s/inventory_adjustments", tenantID)).
		OrderBy("created_at", firestore.Desc)

	if productID != "" {
		q = q.Where("product_id", "==", productID)
	} else if locationID != "" {
		q = q.Where("location_id", "==", locationID)
	}

	limitStr := c.Query("limit")
	limit := 50
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}
	q = q.Limit(limit)

	iter := q.Documents(ctx)
	var adjustments []InventoryAdjustment
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch adjustments"})
			return
		}
		var adj InventoryAdjustment
		doc.DataTo(&adj)
		adjustments = append(adjustments, adj)
	}

	c.JSON(http.StatusOK, gin.H{"adjustments": adjustments, "count": len(adjustments)})
}

// ============================================================================
// CSV IMPORT
// ============================================================================

// ImportBasicInventory handles CSV import of SKU, Quantity
func (h *WarehouseLocationHandler) ImportBasicInventory(c *gin.Context) {
	tenantID := c.GetHeader("X-Tenant-Id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "X-Tenant-Id header required"})
		return
	}

	file, _, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file field required"})
		return
	}
	defer file.Close()

	ctx := c.Request.Context()

	// Find default source
	sourceIter := h.client.Collection(fmt.Sprintf("tenants/%s/fulfilment_sources", tenantID)).
		Where("default", "==", true).Limit(1).Documents(ctx)
	sourceDoc, err := sourceIter.Next()
	defaultSourceID := ""
	defaultSourceName := ""
	if err == nil {
		defaultSourceID, _ = sourceDoc.Data()["source_id"].(string)
		defaultSourceName, _ = sourceDoc.Data()["name"].(string)
	}

	// Find or create a default root location for the default source
	defaultLocationID := ""
	defaultLocationPath := ""
	if defaultSourceID != "" {
		locIter := h.client.Collection(fmt.Sprintf("tenants/%s/warehouse_locations", tenantID)).
			Where("source_id", "==", defaultSourceID).
			Where("depth", "==", 0).
			Where("active", "==", true).
			Limit(1).Documents(ctx)
		locDoc, lerr := locIter.Next()
		if lerr == nil {
			var loc WarehouseLocation
			locDoc.DataTo(&loc)
			defaultLocationID = loc.LocationID
			defaultLocationPath = loc.Path
		} else {
			// Create a default root location
			defaultLocationID = uuid.New().String()
			defaultLocationPath = locationSlugify(defaultSourceName)
			rootLoc := WarehouseLocation{
				LocationID: defaultLocationID,
				TenantID:   tenantID,
				Name:       defaultSourceName,
				ParentID:   "",
				SourceID:   defaultSourceID,
				Path:       defaultLocationPath,
				Depth:      0,
				IsLeaf:     true,
				SortOrder:  0,
				Active:     true,
				CreatedAt:  time.Now(),
				UpdatedAt:  time.Now(),
			}
			h.client.Collection(fmt.Sprintf("tenants/%s/warehouse_locations", tenantID)).
				Doc(defaultLocationID).Set(ctx, rootLoc)
		}
	}

	reader := csv.NewReader(file)
	headers, err := reader.Read()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read CSV headers"})
		return
	}

	skuIdx, qtyIdx := -1, -1
	for i, h := range headers {
		switch strings.ToLower(strings.TrimSpace(h)) {
		case "sku":
			skuIdx = i
		case "quantity", "qty":
			qtyIdx = i
		}
	}
	if skuIdx < 0 || qtyIdx < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "CSV must have SKU and Quantity columns"})
		return
	}

	processed := 0
	var errors []string

	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			errors = append(errors, fmt.Sprintf("row read error: %v", err))
			continue
		}
		if len(row) <= skuIdx || len(row) <= qtyIdx {
			errors = append(errors, "row has insufficient columns")
			continue
		}

		sku := strings.TrimSpace(row[skuIdx])
		qtyStr := strings.TrimSpace(row[qtyIdx])
		qty, convErr := strconv.Atoi(qtyStr)
		if convErr != nil {
			errors = append(errors, fmt.Sprintf("SKU %s: invalid quantity '%s'", sku, qtyStr))
			continue
		}

		// Find product by SKU
		prodIter := h.client.Collection(fmt.Sprintf("tenants/%s/products", tenantID)).
			Where("sku", "==", sku).Limit(1).Documents(ctx)
		prodDoc, prodErr := prodIter.Next()
		if prodErr != nil {
			errors = append(errors, fmt.Sprintf("SKU %s: product not found", sku))
			continue
		}
		productID, _ := prodDoc.Data()["product_id"].(string)
		productName, _ := prodDoc.Data()["title"].(string)

		if defaultLocationID == "" {
			errors = append(errors, fmt.Sprintf("SKU %s: no default warehouse configured", sku))
			continue
		}

		// Apply adjustment
		inventoryDocID := productID + "__" + defaultLocationID
		inventoryRef := h.client.Collection(fmt.Sprintf("tenants/%s/inventory", tenantID)).Doc(inventoryDocID)
		adjustmentID := uuid.New().String()
		adjustmentRef := h.client.Collection(fmt.Sprintf("tenants/%s/inventory_adjustments", tenantID)).Doc(adjustmentID)

		txErr := h.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
			doc, err := tx.Get(inventoryRef)
			var rec InventoryRecord
			if err != nil {
				rec = InventoryRecord{
					InventoryID:  inventoryDocID,
					ProductID:    productID,
					LocationID:   defaultLocationID,
					LocationName: defaultSourceName,
					LocationPath: defaultLocationPath,
					SourceID:     defaultSourceID,
				}
			} else {
				doc.DataTo(&rec)
			}

			qBefore := rec.Quantity
			rec.Quantity += qty
			rec.AvailableQty = rec.Quantity - rec.ReservedQty
			rec.UpdatedAt = time.Now()

			adj := InventoryAdjustment{
				AdjustmentID:   adjustmentID,
				ProductID:      productID,
				ProductSKU:     sku,
				ProductName:    productName,
				LocationID:     defaultLocationID,
				LocationPath:   defaultLocationPath,
				Type:           "adjustment",
				Delta:          qty,
				QuantityBefore: qBefore,
				QuantityAfter:  rec.Quantity,
				Reason:         "CSV import",
				CreatedBy:      "system",
				CreatedAt:      time.Now(),
			}

			if err := tx.Set(inventoryRef, rec); err != nil {
				return err
			}
			return tx.Set(adjustmentRef, adj)
		})

		if txErr != nil {
			errors = append(errors, fmt.Sprintf("SKU %s: %v", sku, txErr))
			continue
		}

		processed++
	}

	c.JSON(http.StatusOK, gin.H{
		"processed": processed,
		"errors":    errors,
	})
}

// ============================================================================
// HELPERS
// ============================================================================

func buildLocationTree(locations []WarehouseLocation, stockMap map[string]int) []*WarehouseLocationNode {
	nodeMap := make(map[string]*WarehouseLocationNode)
	for i := range locations {
		node := &WarehouseLocationNode{
			WarehouseLocation: locations[i],
			Children:          []*WarehouseLocationNode{},
		}
		if locations[i].IsLeaf {
			node.Stock = stockMap[locations[i].LocationID]
		}
		nodeMap[locations[i].LocationID] = node
	}

	var roots []*WarehouseLocationNode
	for _, node := range nodeMap {
		if node.ParentID == "" {
			roots = append(roots, node)
		} else if parent, ok := nodeMap[node.ParentID]; ok {
			parent.Children = append(parent.Children, node)
		} else {
			roots = append(roots, node)
		}
	}

	sortNodes(roots)
	return roots
}

func sortNodes(nodes []*WarehouseLocationNode) {
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].SortOrder < nodes[j].SortOrder
	})
	for _, n := range nodes {
		sortNodes(n.Children)
	}
}

func locationSlugify(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "-")
	var out strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			out.WriteRune(r)
		}
	}
	return out.String()
}

// ============================================================================
// SESSION 8: WAREHOUSE ALLOCATION RULES
// ============================================================================
// Allocation rules determine which warehouse fulfils which channel's orders.
// Routes:
//   GET    /warehouses/allocation-rules
//   POST   /warehouses/allocation-rules
//   PUT    /warehouses/allocation-rules/:id
//   DELETE /warehouses/allocation-rules/:id
// ============================================================================

type WarehouseAllocationRule struct {
	RuleID      string    `firestore:"rule_id" json:"rule_id"`
	TenantID    string    `firestore:"tenant_id" json:"tenant_id"`
	WarehouseID string    `firestore:"warehouse_id" json:"warehouse_id"`
	Name        string    `firestore:"name" json:"name"`
	Channels    []string  `firestore:"channels" json:"channels"` // ["amazon","ebay"] or ["*"] for all
	Priority    int       `firestore:"priority" json:"priority"` // lower = higher priority
	MinStock    int       `firestore:"min_stock" json:"min_stock"`
	Active      bool      `firestore:"active" json:"active"`
	CreatedAt   time.Time `firestore:"created_at" json:"created_at"`
	UpdatedAt   time.Time `firestore:"updated_at" json:"updated_at"`
}

type CreateAllocationRuleRequest struct {
	WarehouseID string   `json:"warehouse_id" binding:"required"`
	Name        string   `json:"name" binding:"required"`
	Channels    []string `json:"channels" binding:"required"`
	Priority    int      `json:"priority"`
	MinStock    int      `json:"min_stock"`
}

type UpdateAllocationRuleRequest struct {
	Name        *string  `json:"name"`
	Channels    []string `json:"channels"`
	Priority    *int     `json:"priority"`
	MinStock    *int     `json:"min_stock"`
	Active      *bool    `json:"active"`
}

func (h *WarehouseLocationHandler) allocationRulesCol(tenantID string) *firestore.CollectionRef {
	return h.client.Collection(fmt.Sprintf("tenants/%s/warehouse_allocation_rules", tenantID))
}

// GET /warehouses/allocation-rules
func (h *WarehouseLocationHandler) ListAllocationRules(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	var rules []WarehouseAllocationRule

	iter := h.allocationRulesCol(tenantID).OrderBy("priority", firestore.Asc).Documents(ctx)
	defer iter.Stop()

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list allocation rules"})
			return
		}
		var rule WarehouseAllocationRule
		if err := doc.DataTo(&rule); err != nil {
			continue
		}
		rules = append(rules, rule)
	}

	if rules == nil {
		rules = []WarehouseAllocationRule{}
	}

	c.JSON(http.StatusOK, gin.H{"rules": rules})
}

// POST /warehouses/allocation-rules
func (h *WarehouseLocationHandler) CreateAllocationRule(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	var req CreateAllocationRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ruleID := "rule_" + uuid.New().String()
	now := time.Now()

	rule := WarehouseAllocationRule{
		RuleID:      ruleID,
		TenantID:    tenantID,
		WarehouseID: req.WarehouseID,
		Name:        req.Name,
		Channels:    req.Channels,
		Priority:    req.Priority,
		MinStock:    req.MinStock,
		Active:      true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if _, err := h.allocationRulesCol(tenantID).Doc(ruleID).Set(ctx, rule); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create allocation rule"})
		return
	}

	c.JSON(http.StatusCreated, rule)
}

// PUT /warehouses/allocation-rules/:id
func (h *WarehouseLocationHandler) UpdateAllocationRule(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ruleID := c.Param("id")
	ctx := c.Request.Context()

	var req UpdateAllocationRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	updates := []firestore.Update{
		{Path: "updated_at", Value: time.Now()},
	}
	if req.Name != nil {
		updates = append(updates, firestore.Update{Path: "name", Value: *req.Name})
	}
	if req.Channels != nil {
		updates = append(updates, firestore.Update{Path: "channels", Value: req.Channels})
	}
	if req.Priority != nil {
		updates = append(updates, firestore.Update{Path: "priority", Value: *req.Priority})
	}
	if req.MinStock != nil {
		updates = append(updates, firestore.Update{Path: "min_stock", Value: *req.MinStock})
	}
	if req.Active != nil {
		updates = append(updates, firestore.Update{Path: "active", Value: *req.Active})
	}

	if _, err := h.allocationRulesCol(tenantID).Doc(ruleID).Update(ctx, updates); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update allocation rule"})
		return
	}

	// Return updated doc
	doc, err := h.allocationRulesCol(tenantID).Doc(ruleID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"rule_id": ruleID})
		return
	}
	var rule WarehouseAllocationRule
	doc.DataTo(&rule)
	c.JSON(http.StatusOK, rule)
}

// DELETE /warehouses/allocation-rules/:id
func (h *WarehouseLocationHandler) DeleteAllocationRule(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ruleID := c.Param("id")
	ctx := c.Request.Context()

	if _, err := h.allocationRulesCol(tenantID).Doc(ruleID).Delete(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete allocation rule"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"deleted": ruleID})
}

// ── GetEffectiveWarehouseForChannel ──────────────────────────────────────────
// Helper: given a channel name and current stock levels per warehouse,
// returns the ordered list of warehouse IDs to fulfil from (priority order,
// respecting min_stock thresholds).
// Used internally by stock-push / fulfilment logic.

type WarehouseStockLevel struct {
	WarehouseID string
	Available   int
}

func (h *WarehouseLocationHandler) GetEffectiveWarehousesForChannel(
	ctx context.Context,
	tenantID string,
	channel string,
	stockLevels []WarehouseStockLevel,
) []string {
	stockMap := map[string]int{}
	for _, s := range stockLevels {
		stockMap[s.WarehouseID] = s.Available
	}

	var rules []WarehouseAllocationRule
	iter := h.allocationRulesCol(tenantID).
		Where("active", "==", true).
		OrderBy("priority", firestore.Asc).
		Documents(ctx)
	defer iter.Stop()

	for {
		doc, err := iter.Next()
		if err != nil {
			break
		}
		var rule WarehouseAllocationRule
		if err := doc.DataTo(&rule); err != nil {
			continue
		}

		// Check if rule applies to this channel
		applies := false
		for _, ch := range rule.Channels {
			if ch == "*" || ch == channel {
				applies = true
				break
			}
		}
		if !applies {
			continue
		}

		// Check min_stock threshold
		avail := stockMap[rule.WarehouseID]
		if rule.MinStock > 0 && avail < rule.MinStock {
			continue
		}

		rules = append(rules, rule)
	}

	result := make([]string, 0, len(rules))
	for _, r := range rules {
		result = append(result, r.WarehouseID)
	}
	return result
}
