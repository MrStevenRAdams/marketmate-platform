package handlers

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"

	"module-a/models"
)

// ============================================================================
// FULFILMENT SOURCE HANDLER
// ============================================================================

type FulfilmentSourceHandler struct {
	client *firestore.Client
}

func NewFulfilmentSourceHandler(client *firestore.Client) *FulfilmentSourceHandler {
	return &FulfilmentSourceHandler{client: client}
}

func (h *FulfilmentSourceHandler) tenantID(c *gin.Context) string {
	if tid := c.GetString("tenant_id"); tid != "" {
		return tid
	}
	return c.GetHeader("X-Tenant-Id")
}

// ============================================================================
// FULFILMENT SOURCES
// ============================================================================

// ListSources GET /api/v1/fulfilment-sources
func (h *FulfilmentSourceHandler) ListSources(c *gin.Context) {
	tenantID := h.tenantID(c)
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant ID required"})
		return
	}

	ctx := c.Request.Context()
	q := h.client.Collection("tenants").Doc(tenantID).Collection("fulfilment_sources").Query

	// Filters
	if sourceType := c.Query("type"); sourceType != "" {
		q = q.Where("type", "==", sourceType)
	}
	if activeOnly := c.Query("active"); activeOnly == "true" {
		q = q.Where("active", "==", true)
	}

	iter := q.Documents(ctx)
	defer iter.Stop()

	var sources []models.FulfilmentSource
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch sources"})
			return
		}
		var src models.FulfilmentSource
		if err := doc.DataTo(&src); err != nil {
			log.Printf("Failed to unmarshal source %s: %v", doc.Ref.ID, err)
			continue
		}
		sources = append(sources, src)
	}

	if sources == nil {
		sources = []models.FulfilmentSource{}
	}

	c.JSON(http.StatusOK, gin.H{
		"sources": sources,
		"count":   len(sources),
	})
}

// GetSource GET /api/v1/fulfilment-sources/:id
func (h *FulfilmentSourceHandler) GetSource(c *gin.Context) {
	tenantID := h.tenantID(c)
	sourceID := c.Param("id")

	doc, err := h.client.Collection("tenants").Doc(tenantID).Collection("fulfilment_sources").Doc(sourceID).Get(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "source not found"})
		return
	}

	var src models.FulfilmentSource
	if err := doc.DataTo(&src); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse source"})
		return
	}

	c.JSON(http.StatusOK, src)
}

// CreateSource POST /api/v1/fulfilment-sources
func (h *FulfilmentSourceHandler) CreateSource(c *gin.Context) {
	tenantID := h.tenantID(c)
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant ID required"})
		return
	}

	var src models.FulfilmentSource
	if err := c.ShouldBindJSON(&src); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid source: " + err.Error()})
		return
	}

	// Validation
	if src.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}
	if src.Type == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "type is required (own_warehouse, 3pl, fba, dropship, virtual)"})
		return
	}
	validTypes := map[string]bool{
		models.SourceTypeOwnWarehouse: true,
		models.SourceType3PL:         true,
		models.SourceTypeFBA:         true,
		models.SourceTypeDropship:    true,
		models.SourceTypeVirtual:     true,
	}
	if !validTypes[src.Type] {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid type: %s", src.Type)})
		return
	}

	// Set defaults based on type
	src.SourceID = "fs_" + uuid.New().String()
	src.TenantID = tenantID
	src.CreatedAt = time.Now()
	src.UpdatedAt = time.Now()

	if !src.Active {
		src.Active = true // Default to active
	}

	// Set default label config based on type
	if src.LabelConfig.Mode == "" {
		switch src.Type {
		case models.SourceTypeOwnWarehouse:
			src.LabelConfig.Mode = models.LabelModeOwn
		case models.SourceType3PL:
			src.LabelConfig.Mode = models.LabelModeThirdParty
		case models.SourceTypeFBA, models.SourceTypeDropship:
			src.LabelConfig.Mode = models.LabelModeNone
		}
	}

	// Set inventory tracking default
	switch src.Type {
	case models.SourceTypeOwnWarehouse, models.SourceType3PL:
		if src.InventoryMode == "" {
			src.InventoryMode = models.InventoryModeRealTime
			src.InventoryTracked = true
		}
	case models.SourceTypeDropship, models.SourceTypeFBA:
		src.InventoryTracked = false
	}

	// If this is being set as default, unset any existing default
	if src.Default {
		h.clearDefault(c, tenantID, "")
	}

	_, err := h.client.Collection("tenants").Doc(tenantID).Collection("fulfilment_sources").Doc(src.SourceID).Set(c.Request.Context(), src)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save source"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"source_id": src.SourceID,
		"name":      src.Name,
		"type":      src.Type,
		"message":   "Fulfilment source created successfully",
	})
}

// UpdateSource PATCH /api/v1/fulfilment-sources/:id
func (h *FulfilmentSourceHandler) UpdateSource(c *gin.Context) {
	tenantID := h.tenantID(c)
	sourceID := c.Param("id")

	ref := h.client.Collection("tenants").Doc(tenantID).Collection("fulfilment_sources").Doc(sourceID)
	if _, err := ref.Get(c.Request.Context()); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "source not found"})
		return
	}

	var src models.FulfilmentSource
	if err := c.ShouldBindJSON(&src); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid update body"})
		return
	}

	// If setting as default, clear existing default first
	if src.Default {
		h.clearDefault(c, tenantID, sourceID)
	}

	src.UpdatedAt = time.Now()

	// Merge update — use Set with MergeAll to avoid overwriting fields not included
	_, err := ref.Set(c.Request.Context(), src, firestore.MergeAll)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update source"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Source updated", "source_id": sourceID})
}

// DeleteSource DELETE /api/v1/fulfilment-sources/:id
func (h *FulfilmentSourceHandler) DeleteSource(c *gin.Context) {
	tenantID := h.tenantID(c)
	sourceID := c.Param("id")

	// Soft delete
	_, err := h.client.Collection("tenants").Doc(tenantID).Collection("fulfilment_sources").Doc(sourceID).
		Update(c.Request.Context(), []firestore.Update{
			{Path: "active", Value: false},
			{Path: "deleted_at", Value: time.Now()},
			{Path: "updated_at", Value: time.Now()},
		})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to deactivate source"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Source deactivated", "source_id": sourceID})
}

// SetDefaultSource POST /api/v1/fulfilment-sources/:id/set-default
func (h *FulfilmentSourceHandler) SetDefaultSource(c *gin.Context) {
	tenantID := h.tenantID(c)
	sourceID := c.Param("id")

	ctx := c.Request.Context()

	// Clear existing default
	h.clearDefault(c, tenantID, sourceID)

	// Set new default
	_, err := h.client.Collection("tenants").Doc(tenantID).Collection("fulfilment_sources").Doc(sourceID).
		Update(ctx, []firestore.Update{
			{Path: "default", Value: true},
			{Path: "updated_at", Value: time.Now()},
		})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to set default"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Default source updated", "source_id": sourceID})
}

// ListSuppliers GET /api/v1/suppliers
func (h *FulfilmentSourceHandler) ListSuppliers(c *gin.Context) {
	tenantID := h.tenantID(c)
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant ID required"})
		return
	}

	ctx := c.Request.Context()
	q := h.client.Collection("tenants").Doc(tenantID).Collection("suppliers").Query

	if activeOnly := c.Query("active"); activeOnly == "true" {
		q = q.Where("active", "==", true)
	}

	iter := q.Documents(ctx)
	defer iter.Stop()

	var suppliers []models.Supplier
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch suppliers"})
			return
		}
		var sup models.Supplier
		if err := doc.DataTo(&sup); err != nil {
			continue
		}
		suppliers = append(suppliers, sup)
	}

	if suppliers == nil {
		suppliers = []models.Supplier{}
	}

	c.JSON(http.StatusOK, gin.H{"suppliers": suppliers, "count": len(suppliers)})
}

// GetSupplier GET /api/v1/suppliers/:id
func (h *FulfilmentSourceHandler) GetSupplier(c *gin.Context) {
	tenantID := h.tenantID(c)
	supplierID := c.Param("id")

	doc, err := h.client.Collection("tenants").Doc(tenantID).Collection("suppliers").Doc(supplierID).Get(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "supplier not found"})
		return
	}

	var sup models.Supplier
	doc.DataTo(&sup)
	c.JSON(http.StatusOK, sup)
}

// CreateSupplier POST /api/v1/suppliers
func (h *FulfilmentSourceHandler) CreateSupplier(c *gin.Context) {
	tenantID := h.tenantID(c)
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant ID required"})
		return
	}

	var sup models.Supplier
	if err := c.ShouldBindJSON(&sup); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid supplier: " + err.Error()})
		return
	}

	if sup.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "supplier name is required"})
		return
	}

	sup.SupplierID = "sup_" + uuid.New().String()
	sup.TenantID = tenantID
	sup.Active = true
	sup.CreatedAt = time.Now()
	sup.UpdatedAt = time.Now()

	if sup.Currency == "" {
		sup.Currency = "GBP"
	}

	_, err := h.client.Collection("tenants").Doc(tenantID).Collection("suppliers").Doc(sup.SupplierID).Set(c.Request.Context(), sup)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save supplier"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"supplier_id": sup.SupplierID,
		"name":        sup.Name,
		"message":     "Supplier created successfully",
	})
}

// UpdateSupplier PATCH /api/v1/suppliers/:id
func (h *FulfilmentSourceHandler) UpdateSupplier(c *gin.Context) {
	tenantID := h.tenantID(c)
	supplierID := c.Param("id")

	ref := h.client.Collection("tenants").Doc(tenantID).Collection("suppliers").Doc(supplierID)
	if _, err := ref.Get(c.Request.Context()); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "supplier not found"})
		return
	}

	var sup models.Supplier
	if err := c.ShouldBindJSON(&sup); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid update body"})
		return
	}

	sup.UpdatedAt = time.Now()
	_, err := ref.Set(c.Request.Context(), sup, firestore.MergeAll)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update supplier"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Supplier updated", "supplier_id": supplierID})
}

// DeleteSupplier DELETE /api/v1/suppliers/:id
func (h *FulfilmentSourceHandler) DeleteSupplier(c *gin.Context) {
	tenantID := h.tenantID(c)
	supplierID := c.Param("id")

	_, err := h.client.Collection("tenants").Doc(tenantID).Collection("suppliers").Doc(supplierID).
		Update(c.Request.Context(), []firestore.Update{
			{Path: "active", Value: false},
			{Path: "updated_at", Value: time.Now()},
		})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to deactivate supplier"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Supplier deactivated"})
}

// ============================================================================
// PURCHASE ORDERS
// ============================================================================

// ListPurchaseOrders GET /api/v1/purchase-orders
func (h *FulfilmentSourceHandler) ListPurchaseOrders(c *gin.Context) {
	tenantID := h.tenantID(c)
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant ID required"})
		return
	}

	ctx := c.Request.Context()
	q := h.client.Collection("tenants").Doc(tenantID).Collection("purchase_orders").Query

	if status := c.Query("status"); status != "" {
		q = q.Where("status", "==", status)
	}
	if supplierID := c.Query("supplier_id"); supplierID != "" {
		q = q.Where("supplier_id", "==", supplierID)
	}

	q = q.OrderBy("created_at", firestore.Desc).Limit(100)

	iter := q.Documents(ctx)
	defer iter.Stop()

	var pos []models.PurchaseOrder
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch purchase orders"})
			return
		}
		var po models.PurchaseOrder
		doc.DataTo(&po)
		pos = append(pos, po)
	}

	if pos == nil {
		pos = []models.PurchaseOrder{}
	}

	c.JSON(http.StatusOK, gin.H{"purchase_orders": pos, "count": len(pos)})
}

// GetPurchaseOrder GET /api/v1/purchase-orders/:id
func (h *FulfilmentSourceHandler) GetPurchaseOrder(c *gin.Context) {
	tenantID := h.tenantID(c)
	poID := c.Param("id")

	doc, err := h.client.Collection("tenants").Doc(tenantID).Collection("purchase_orders").Doc(poID).Get(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "purchase order not found"})
		return
	}

	var po models.PurchaseOrder
	doc.DataTo(&po)
	c.JSON(http.StatusOK, po)
}

// UpdatePurchaseOrderStatus PATCH /api/v1/purchase-orders/:id/status
func (h *FulfilmentSourceHandler) UpdatePurchaseOrderStatus(c *gin.Context) {
	tenantID := h.tenantID(c)
	poID := c.Param("id")

	var req struct {
		Status         string `json:"status"`
		TrackingNumber string `json:"tracking_number,omitempty"`
		CarrierName    string `json:"carrier_name,omitempty"`
		Notes          string `json:"notes,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Status == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "status required"})
		return
	}

	updates := []firestore.Update{
		{Path: "status", Value: req.Status},
		{Path: "updated_at", Value: time.Now()},
	}

	now := time.Now()
	switch req.Status {
	case models.POStatusSent:
		updates = append(updates, firestore.Update{Path: "sent_at", Value: now})
	case models.POStatusAcknowledged:
		updates = append(updates, firestore.Update{Path: "acknowledged_at", Value: now})
	case models.POStatusShipped:
		updates = append(updates, firestore.Update{Path: "shipped_at", Value: now})
		if req.TrackingNumber != "" {
			updates = append(updates, firestore.Update{Path: "tracking_number", Value: req.TrackingNumber})
		}
		if req.CarrierName != "" {
			updates = append(updates, firestore.Update{Path: "carrier_name", Value: req.CarrierName})
		}
	case models.POStatusDelivered:
		updates = append(updates, firestore.Update{Path: "delivered_at", Value: now})
	}

	if req.Notes != "" {
		updates = append(updates, firestore.Update{Path: "internal_notes", Value: req.Notes})
	}

	_, err := h.client.Collection("tenants").Doc(tenantID).Collection("purchase_orders").Doc(poID).Update(c.Request.Context(), updates)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update purchase order"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Purchase order updated", "po_id": poID, "status": req.Status})
}

// ============================================================================
// HELPER
// ============================================================================

func (h *FulfilmentSourceHandler) clearDefault(c *gin.Context, tenantID, excludeSourceID string) {
	ctx := c.Request.Context()
	iter := h.client.Collection("tenants").Doc(tenantID).Collection("fulfilment_sources").
		Where("default", "==", true).Documents(ctx)
	defer iter.Stop()

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		if doc.Ref.ID == excludeSourceID {
			continue
		}
		_, err = doc.Ref.Update(ctx, []firestore.Update{
			{Path: "default", Value: false},
			{Path: "updated_at", Value: time.Now()},
		})
		if err != nil {
			log.Printf("Failed to clear default flag on source %s: %v", doc.Ref.ID, err)
		}
	}
}
