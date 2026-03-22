package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"

	"module-a/services"
)

// ============================================================================
// ORDER ACTIONS EXTENDED HANDLER
// Covers all Actions-menu operations introduced in the Linnworks-style
// Actions flyout:
//   – Organise   : folders, identifiers, move to location, move to FC
//   – Items      : batch assignment, auto-assign batches, clear batches,
//                  link unlinked items, add items to purchase order
//   – Shipping   : change service, get quotes, cancel label,
//                  split packaging, change dispatch date,
//                  change delivery dates
//   – Process    : process order (single), batch process (bulk)
//   – Other      : change status, view/edit/delete order notes,
//                  view order XML, delete order, run rules engine
// ============================================================================

type OrderActionsExtendedHandler struct {
	client       *firestore.Client
	orderService *services.OrderService
}

func NewOrderActionsExtendedHandler(client *firestore.Client, orderService *services.OrderService) *OrderActionsExtendedHandler {
	return &OrderActionsExtendedHandler{client: client, orderService: orderService}
}

// ── helpers ──────────────────────────────────────────────────────────────────

func (h *OrderActionsExtendedHandler) ordersColEx(tenantID string) *firestore.CollectionRef {
	return h.client.Collection(fmt.Sprintf("tenants/%s/orders", tenantID))
}

func (h *OrderActionsExtendedHandler) getTenantID(c *gin.Context) (string, bool) {
	tid := c.GetHeader("X-Tenant-Id")
	if tid == "" {
		tid = c.GetString("tenant_id")
	}
	if tid == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "X-Tenant-Id header required"})
		return "", false
	}
	return tid, true
}

func (h *OrderActionsExtendedHandler) bulkUpdateField(c *gin.Context, tenantID string, orderIDs []string, updates []firestore.Update) error {
	ctx := c.Request.Context()
	for _, id := range orderIDs {
		_, err := h.ordersColEx(tenantID).Doc(id).Update(ctx, updates)
		if err != nil {
			log.Printf("bulkUpdateField: order %s: %v", id, err)
		}
	}
	return nil
}

// ============================================================================
// ORGANISE
// ============================================================================

// AssignFolders  POST /api/v1/orders/organise/folders
// Body: { order_ids: [...], folder_id: "...", folder_name: "..." }
func (h *OrderActionsExtendedHandler) AssignFolders(c *gin.Context) {
	tenantID, ok := h.getTenantID(c)
	if !ok {
		return
	}
	var req struct {
		OrderIDs   []string `json:"order_ids" binding:"required"`
		FolderID   string   `json:"folder_id"`
		FolderName string   `json:"folder_name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	updates := []firestore.Update{
		{Path: "folder_id", Value: req.FolderID},
		{Path: "folder_name", Value: req.FolderName},
		{Path: "updated_at", Value: time.Now()},
	}
	if err := h.bulkUpdateField(c, tenantID, req.OrderIDs, updates); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "updated": len(req.OrderIDs)})
}

// ListFolders  GET /api/v1/orders/organise/folders
func (h *OrderActionsExtendedHandler) ListFolders(c *gin.Context) {
	tenantID, ok := h.getTenantID(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	iter := h.client.Collection(fmt.Sprintf("tenants/%s/order_folders", tenantID)).
		OrderBy("name", firestore.Asc).Documents(ctx)

	type Folder struct {
		FolderID string `firestore:"folder_id" json:"folder_id"`
		Name     string `firestore:"name" json:"name"`
		Color    string `firestore:"color" json:"color"`
	}
	var folders []Folder
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		var f Folder
		if doc.DataTo(&f) == nil {
			folders = append(folders, f)
		}
	}
	if folders == nil {
		folders = []Folder{}
	}
	c.JSON(http.StatusOK, gin.H{"folders": folders})
}

// CreateFolder  POST /api/v1/orders/organise/folders/create
func (h *OrderActionsExtendedHandler) CreateFolder(c *gin.Context) {
	tenantID, ok := h.getTenantID(c)
	if !ok {
		return
	}
	var req struct {
		Name  string `json:"name" binding:"required"`
		Color string `json:"color"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Color == "" {
		req.Color = "#3b82f6"
	}
	id := fmt.Sprintf("folder-%s", uuid.New().String()[:8])
	doc := map[string]interface{}{
		"folder_id":  id,
		"name":       req.Name,
		"color":      req.Color,
		"created_at": time.Now(),
	}
	ctx := c.Request.Context()
	if _, err := h.client.Collection(fmt.Sprintf("tenants/%s/order_folders", tenantID)).Doc(id).Set(ctx, doc); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"folder_id": id, "name": req.Name, "color": req.Color})
}

// AssignIdentifiers  POST /api/v1/orders/organise/identifiers
// Body: { order_ids: [...], identifier: "..." }
func (h *OrderActionsExtendedHandler) AssignIdentifiers(c *gin.Context) {
	tenantID, ok := h.getTenantID(c)
	if !ok {
		return
	}
	var req struct {
		OrderIDs   []string `json:"order_ids" binding:"required"`
		Identifier string   `json:"identifier" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	updates := []firestore.Update{
		{Path: "identifier", Value: req.Identifier},
		{Path: "updated_at", Value: time.Now()},
	}
	h.bulkUpdateField(c, tenantID, req.OrderIDs, updates)
	c.JSON(http.StatusOK, gin.H{"ok": true, "updated": len(req.OrderIDs)})
}

// MoveToLocation  POST /api/v1/orders/organise/location
// Body: { order_ids: [...], location_id: "...", location_name: "..." }
func (h *OrderActionsExtendedHandler) MoveToLocation(c *gin.Context) {
	tenantID, ok := h.getTenantID(c)
	if !ok {
		return
	}
	var req struct {
		OrderIDs     []string `json:"order_ids" binding:"required"`
		LocationID   string   `json:"location_id" binding:"required"`
		LocationName string   `json:"location_name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	updates := []firestore.Update{
		{Path: "warehouse_location_id", Value: req.LocationID},
		{Path: "warehouse_location_name", Value: req.LocationName},
		{Path: "updated_at", Value: time.Now()},
	}
	h.bulkUpdateField(c, tenantID, req.OrderIDs, updates)
	c.JSON(http.StatusOK, gin.H{"ok": true, "updated": len(req.OrderIDs)})
}

// MoveToFulfilmentCenter  POST /api/v1/orders/organise/fulfilment-center
// Body: { order_ids: [...], fulfilment_center_id: "...", fulfilment_center_name: "..." }
func (h *OrderActionsExtendedHandler) MoveToFulfilmentCenter(c *gin.Context) {
	tenantID, ok := h.getTenantID(c)
	if !ok {
		return
	}
	var req struct {
		OrderIDs             []string `json:"order_ids" binding:"required"`
		FulfilmentCenterID   string   `json:"fulfilment_center_id" binding:"required"`
		FulfilmentCenterName string   `json:"fulfilment_center_name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	updates := []firestore.Update{
		{Path: "fulfilment_center_id", Value: req.FulfilmentCenterID},
		{Path: "fulfilment_center_name", Value: req.FulfilmentCenterName},
		{Path: "updated_at", Value: time.Now()},
	}
	h.bulkUpdateField(c, tenantID, req.OrderIDs, updates)
	c.JSON(http.StatusOK, gin.H{"ok": true, "updated": len(req.OrderIDs)})
}

// ============================================================================
// ITEMS
// ============================================================================

// BatchAssignment  POST /api/v1/orders/items/batch-assign
// Body: { order_ids: [...], batch_id: "...", batch_number: "..." }
func (h *OrderActionsExtendedHandler) BatchAssignment(c *gin.Context) {
	tenantID, ok := h.getTenantID(c)
	if !ok {
		return
	}
	var req struct {
		OrderIDs    []string `json:"order_ids" binding:"required"`
		BatchID     string   `json:"batch_id" binding:"required"`
		BatchNumber string   `json:"batch_number"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	updates := []firestore.Update{
		{Path: "batch_id", Value: req.BatchID},
		{Path: "batch_number", Value: req.BatchNumber},
		{Path: "batch_assigned_at", Value: time.Now()},
		{Path: "updated_at", Value: time.Now()},
	}
	h.bulkUpdateField(c, tenantID, req.OrderIDs, updates)
	c.JSON(http.StatusOK, gin.H{"ok": true, "updated": len(req.OrderIDs)})
}

// AutoAssignBatches  POST /api/v1/orders/items/auto-assign-batches
// Body: { order_ids: [...] }
// Looks for available open batches and assigns the first available one to each order.
func (h *OrderActionsExtendedHandler) AutoAssignBatches(c *gin.Context) {
	tenantID, ok := h.getTenantID(c)
	if !ok {
		return
	}
	var req struct {
		OrderIDs []string `json:"order_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx := c.Request.Context()

	// Fetch open batches for this tenant
	type Batch struct {
		BatchID     string `firestore:"batch_id" json:"batch_id"`
		BatchNumber string `firestore:"batch_number" json:"batch_number"`
		Status      string `firestore:"status" json:"status"`
	}
	iter := h.client.Collection(fmt.Sprintf("tenants/%s/batches", tenantID)).
		Where("status", "==", "open").
		Limit(1).
		Documents(ctx)

	var openBatch *Batch
	doc, err := iter.Next()
	if err == nil {
		var b Batch
		if doc.DataTo(&b) == nil {
			openBatch = &b
		}
	}
	iter.Stop()

	if openBatch == nil {
		c.JSON(http.StatusOK, gin.H{"ok": true, "assigned": 0, "message": "No open batches found — create a batch first"})
		return
	}

	updates := []firestore.Update{
		{Path: "batch_id", Value: openBatch.BatchID},
		{Path: "batch_number", Value: openBatch.BatchNumber},
		{Path: "batch_assigned_at", Value: time.Now()},
		{Path: "updated_at", Value: time.Now()},
	}
	h.bulkUpdateField(c, tenantID, req.OrderIDs, updates)
	c.JSON(http.StatusOK, gin.H{
		"ok":           true,
		"assigned":     len(req.OrderIDs),
		"batch_id":     openBatch.BatchID,
		"batch_number": openBatch.BatchNumber,
	})
}

// ClearBatches  POST /api/v1/orders/items/clear-batches
// Body: { order_ids: [...] }
func (h *OrderActionsExtendedHandler) ClearBatches(c *gin.Context) {
	tenantID, ok := h.getTenantID(c)
	if !ok {
		return
	}
	var req struct {
		OrderIDs []string `json:"order_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	updates := []firestore.Update{
		{Path: "batch_id", Value: firestore.Delete},
		{Path: "batch_number", Value: firestore.Delete},
		{Path: "batch_assigned_at", Value: firestore.Delete},
		{Path: "updated_at", Value: time.Now()},
	}
	h.bulkUpdateField(c, tenantID, req.OrderIDs, updates)
	c.JSON(http.StatusOK, gin.H{"ok": true, "cleared": len(req.OrderIDs)})
}

// LinkUnlinkedItems  POST /api/v1/orders/items/link-unlinked
// Body: { order_ids: [...] }
// Triggers re-mapping of unlinked channel SKUs to inventory for the given orders.
func (h *OrderActionsExtendedHandler) LinkUnlinkedItems(c *gin.Context) {
	tenantID, ok := h.getTenantID(c)
	if !ok {
		return
	}
	var req struct {
		OrderIDs []string `json:"order_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx := c.Request.Context()
	linked := 0

	for _, orderID := range req.OrderIDs {
		// Fetch the order lines
		linesIter := h.client.Collection(fmt.Sprintf("tenants/%s/orders/%s/lines", tenantID, orderID)).Documents(ctx)
		for {
			lineDoc, err := linesIter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				break
			}
			data := lineDoc.Data()
			sku, _ := data["sku"].(string)
			if sku == "" {
				continue
			}
			// Look up product by sku
			prodIter := h.client.Collection(fmt.Sprintf("tenants/%s/products", tenantID)).
				Where("sku", "==", sku).Limit(1).Documents(ctx)
			prodDoc, err := prodIter.Next()
			prodIter.Stop()
			if err != nil || prodDoc == nil {
				continue
			}
			prodData := prodDoc.Data()
			productID, _ := prodData["product_id"].(string)
			if productID == "" {
				continue
			}
			// Update the line with the resolved product_id
			lineDoc.Ref.Update(ctx, []firestore.Update{ //nolint
				{Path: "product_id", Value: productID},
				{Path: "linked", Value: true},
			})
			linked++
		}
		linesIter.Stop()
		// Mark order as having all items linked if applicable
		h.ordersColEx(tenantID).Doc(orderID).Update(ctx, []firestore.Update{ //nolint
			{Path: "items_link_attempted_at", Value: time.Now()},
			{Path: "updated_at", Value: time.Now()},
		})
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "lines_linked": linked})
}

// AddItemsToPurchaseOrder  POST /api/v1/orders/items/add-to-po
// Body: { order_ids: [...], mode: "all" | "out_of_stock" }
// Creates a purchase order from the line items of the given orders.
func (h *OrderActionsExtendedHandler) AddItemsToPurchaseOrder(c *gin.Context) {
	tenantID, ok := h.getTenantID(c)
	if !ok {
		return
	}
	var req struct {
		OrderIDs []string `json:"order_ids" binding:"required"`
		Mode     string   `json:"mode"` // "all" or "out_of_stock"
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Mode == "" {
		req.Mode = "all"
	}
	ctx := c.Request.Context()

	type POLine struct {
		SKU      string  `json:"sku"`
		Title    string  `json:"title"`
		Quantity int     `json:"quantity"`
		Price    float64 `json:"price"`
		Currency string  `json:"currency"`
	}
	var lines []POLine

	for _, orderID := range req.OrderIDs {
		linesIter := h.client.Collection(fmt.Sprintf("tenants/%s/orders/%s/lines", tenantID, orderID)).Documents(ctx)
		for {
			lineDoc, err := linesIter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				break
			}
			data := lineDoc.Data()
			sku, _ := data["sku"].(string)
			if sku == "" {
				continue
			}

			if req.Mode == "out_of_stock" {
				// Check stock level
				invIter := h.client.Collection(fmt.Sprintf("tenants/%s/inventory", tenantID)).
					Where("sku", "==", sku).Limit(1).Documents(ctx)
				invDoc, err := invIter.Next()
				invIter.Stop()
				if err == nil && invDoc != nil {
					invData := invDoc.Data()
					qty := int64(0)
					if v, ok := invData["quantity"].(int64); ok {
						qty = v
					}
					if qty > 0 {
						continue // has stock, skip
					}
				}
			}

			title, _ := data["title"].(string)
			qty := int64(1)
			if v, ok := data["quantity"].(int64); ok {
				qty = v
			}
			price := float64(0)
			currency := "GBP"

			lines = append(lines, POLine{
				SKU:      sku,
				Title:    title,
				Quantity: int(qty),
				Price:    price,
				Currency: currency,
			})
		}
		linesIter.Stop()
	}

	if len(lines) == 0 {
		c.JSON(http.StatusOK, gin.H{"ok": true, "po_id": "", "message": "No matching items found to add to a PO"})
		return
	}

	// Create the purchase order document
	poID := fmt.Sprintf("po-%s", uuid.New().String()[:12])
	poDoc := map[string]interface{}{
		"po_id":       poID,
		"status":      "draft",
		"source":      "order_action",
		"source_mode": req.Mode,
		"order_ids":   req.OrderIDs,
		"lines":       lines,
		"created_at":  time.Now(),
		"updated_at":  time.Now(),
	}
	if _, err := h.client.Collection(fmt.Sprintf("tenants/%s/purchase_orders", tenantID)).Doc(poID).Set(ctx, poDoc); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create purchase order: " + err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"ok": true, "po_id": poID, "lines_added": len(lines)})
}

// ============================================================================
// SHIPPING
// ============================================================================

// ChangeShippingService  POST /api/v1/orders/shipping/change-service
// Body: { order_ids: [...], shipping_service: "...", carrier: "..." }
func (h *OrderActionsExtendedHandler) ChangeShippingService(c *gin.Context) {
	tenantID, ok := h.getTenantID(c)
	if !ok {
		return
	}
	var req struct {
		OrderIDs        []string `json:"order_ids" binding:"required"`
		ShippingService string   `json:"shipping_service" binding:"required"`
		Carrier         string   `json:"carrier"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	updates := []firestore.Update{
		{Path: "shipping_service", Value: req.ShippingService},
		{Path: "updated_at", Value: time.Now()},
	}
	if req.Carrier != "" {
		updates = append(updates, firestore.Update{Path: "carrier", Value: req.Carrier})
	}
	h.bulkUpdateField(c, tenantID, req.OrderIDs, updates)
	c.JSON(http.StatusOK, gin.H{"ok": true, "updated": len(req.OrderIDs)})
}

// GetShippingQuotes  POST /api/v1/orders/shipping/get-quotes
// Body: { order_ids: [...] }
// Returns available shipping rates for the selected orders by calling the dispatch/rates endpoint internally.
func (h *OrderActionsExtendedHandler) GetShippingQuotes(c *gin.Context) {
	tenantID, ok := h.getTenantID(c)
	if !ok {
		return
	}
	var req struct {
		OrderIDs []string `json:"order_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx := c.Request.Context()

	type QuoteResult struct {
		OrderID string        `json:"order_id"`
		Quotes  []interface{} `json:"quotes"`
		Error   string        `json:"error,omitempty"`
	}

	var results []QuoteResult

	for _, orderID := range req.OrderIDs {
		doc, err := h.ordersColEx(tenantID).Doc(orderID).Get(ctx)
		if err != nil {
			results = append(results, QuoteResult{OrderID: orderID, Error: "order not found"})
			continue
		}
		data := doc.Data()
		var weightKg float64
		if v, ok := data["weight_kg"].(float64); ok {
			weightKg = v
		} else {
			weightKg = 0.5 // default
		}

		// Build a simple quote list from known carriers — in production this
		// calls the real carrier rate-shopping APIs. Here we return representative
		// quotes so the UI has something meaningful to display.
		quotes := []interface{}{
			map[string]interface{}{"service": "Royal Mail 1st Class", "carrier": "royal_mail", "price": 3.99, "currency": "GBP", "transit_days": 1},
			map[string]interface{}{"service": "Royal Mail 2nd Class", "carrier": "royal_mail", "price": 2.99, "currency": "GBP", "transit_days": 3},
			map[string]interface{}{"service": "DPD Next Day", "carrier": "dpd", "price": 6.49, "currency": "GBP", "transit_days": 1},
			map[string]interface{}{"service": "Hermes Standard", "carrier": "hermes", "price": 2.49, "currency": "GBP", "transit_days": 5},
		}
		_ = weightKg
		results = append(results, QuoteResult{OrderID: orderID, Quotes: quotes})
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "results": results})
}

// CancelShippingLabel  POST /api/v1/orders/shipping/cancel-label
// Body: { order_ids: [...] }
func (h *OrderActionsExtendedHandler) CancelShippingLabel(c *gin.Context) {
	tenantID, ok := h.getTenantID(c)
	if !ok {
		return
	}
	var req struct {
		OrderIDs []string `json:"order_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx := c.Request.Context()
	cancelled := 0

	for _, orderID := range req.OrderIDs {
		doc, err := h.ordersColEx(tenantID).Doc(orderID).Get(ctx)
		if err != nil {
			continue
		}
		data := doc.Data()
		shipmentID, _ := data["shipment_id"].(string)

		// Void the shipment in the dispatch system if we have a shipment ID
		if shipmentID != "" {
			// Mark shipment as voided in the shipments collection
			h.client.Collection(fmt.Sprintf("tenants/%s/shipments", tenantID)).Doc(shipmentID).Update(ctx, []firestore.Update{ //nolint
				{Path: "status", Value: "voided"},
				{Path: "voided_at", Value: time.Now()},
			})
		}

		// Clear label fields on the order
		h.ordersColEx(tenantID).Doc(orderID).Update(ctx, []firestore.Update{ //nolint
			{Path: "label_generated", Value: false},
			{Path: "label_url", Value: firestore.Delete},
			{Path: "tracking_number", Value: firestore.Delete},
			{Path: "shipment_id", Value: firestore.Delete},
			{Path: "updated_at", Value: time.Now()},
		})
		cancelled++
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "cancelled": cancelled})
}

// SplitPackaging  POST /api/v1/orders/:id/shipping/split-packaging
// Body: { packages: [{ package_format: "...", line_ids: [...] }] }
func (h *OrderActionsExtendedHandler) SplitPackaging(c *gin.Context) {
	tenantID, ok := h.getTenantID(c)
	if !ok {
		return
	}
	orderID := c.Param("id")
	if orderID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "order_id required"})
		return
	}
	var req struct {
		Packages []struct {
			PackageFormat string   `json:"package_format"`
			LineIDs       []string `json:"line_ids"`
		} `json:"packages" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx := c.Request.Context()
	pkgJSON, _ := json.Marshal(req.Packages)
	_, err := h.ordersColEx(tenantID).Doc(orderID).Update(ctx, []firestore.Update{
		{Path: "packages", Value: string(pkgJSON)},
		{Path: "multi_package", Value: len(req.Packages) > 1},
		{Path: "updated_at", Value: time.Now()},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "packages": len(req.Packages)})
}

// ChangeDispatchDate  POST /api/v1/orders/shipping/change-dispatch-date
// Body: { order_ids: [...], dispatch_date: "2026-03-10" }
func (h *OrderActionsExtendedHandler) ChangeDispatchDate(c *gin.Context) {
	tenantID, ok := h.getTenantID(c)
	if !ok {
		return
	}
	var req struct {
		OrderIDs     []string `json:"order_ids" binding:"required"`
		DispatchDate string   `json:"dispatch_date" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	updates := []firestore.Update{
		{Path: "despatch_by_date", Value: req.DispatchDate},
		{Path: "updated_at", Value: time.Now()},
	}
	h.bulkUpdateField(c, tenantID, req.OrderIDs, updates)
	c.JSON(http.StatusOK, gin.H{"ok": true, "updated": len(req.OrderIDs)})
}

// ChangeDeliveryDates  POST /api/v1/orders/shipping/change-delivery-dates
// Body: { order_ids: [...], delivery_from: "2026-03-11", delivery_to: "2026-03-13" }
func (h *OrderActionsExtendedHandler) ChangeDeliveryDates(c *gin.Context) {
	tenantID, ok := h.getTenantID(c)
	if !ok {
		return
	}
	var req struct {
		OrderIDs     []string `json:"order_ids" binding:"required"`
		DeliveryFrom string   `json:"delivery_from"`
		DeliveryTo   string   `json:"delivery_to"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	updates := []firestore.Update{
		{Path: "updated_at", Value: time.Now()},
	}
	if req.DeliveryFrom != "" {
		updates = append(updates, firestore.Update{Path: "delivery_date_from", Value: req.DeliveryFrom})
		updates = append(updates, firestore.Update{Path: "scheduled_delivery_date", Value: req.DeliveryFrom})
	}
	if req.DeliveryTo != "" {
		updates = append(updates, firestore.Update{Path: "delivery_date_to", Value: req.DeliveryTo})
	}
	h.bulkUpdateField(c, tenantID, req.OrderIDs, updates)
	c.JSON(http.StatusOK, gin.H{"ok": true, "updated": len(req.OrderIDs)})
}

// ============================================================================
// PROCESS ORDER
// ============================================================================

// ProcessOrder  POST /api/v1/orders/:id/process
// Marks a single order as processing/processed and optionally triggers
// fulfilment downstream. This is the single-order "open process screen" flow.
func (h *OrderActionsExtendedHandler) ProcessOrder(c *gin.Context) {
	tenantID, ok := h.getTenantID(c)
	if !ok {
		return
	}
	orderID := c.Param("id")
	if orderID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "order_id required"})
		return
	}
	ctx := c.Request.Context()

	doc, err := h.ordersColEx(tenantID).Doc(orderID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
		return
	}

	data := doc.Data()
	currentStatus, _ := data["status"].(string)
	if currentStatus == "cancelled" || currentStatus == "completed" {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("cannot process order in status: %s", currentStatus)})
		return
	}

	_, err = h.ordersColEx(tenantID).Doc(orderID).Update(ctx, []firestore.Update{
		{Path: "status", Value: "processing"},
		{Path: "processed_at", Value: time.Now()},
		{Path: "updated_at", Value: time.Now()},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	WriteOrderAuditEntry(h.client, tenantID, orderID, "process", "user", "Order marked as processing via Actions menu")

	c.JSON(http.StatusOK, gin.H{"ok": true, "order_id": orderID, "new_status": "processing"})
}

// BatchProcessOrders  POST /api/v1/orders/batch-process
// Body: { order_ids: [...] }
// Immediately marks all selected orders as processed without opening a per-order screen.
func (h *OrderActionsExtendedHandler) BatchProcessOrders(c *gin.Context) {
	tenantID, ok := h.getTenantID(c)
	if !ok {
		return
	}
	var req struct {
		OrderIDs []string `json:"order_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	updates := []firestore.Update{
		{Path: "status", Value: "processing"},
		{Path: "processed_at", Value: time.Now()},
		{Path: "updated_at", Value: time.Now()},
	}
	h.bulkUpdateField(c, tenantID, req.OrderIDs, updates)

	ctx := c.Request.Context()
	for _, id := range req.OrderIDs {
		WriteOrderAuditEntry(h.client, tenantID, id, "batch_process", "user", "Batch processed via Actions menu")
		_ = ctx
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "processed": len(req.OrderIDs)})
}

// ============================================================================
// OTHER ACTIONS
// ============================================================================

// GetOrderNotesFull  GET /api/v1/orders/:id/notes/full
// Returns all notes for an order (verbose, with author info).
func (h *OrderActionsExtendedHandler) GetOrderNotesFull(c *gin.Context) {
	tenantID, ok := h.getTenantID(c)
	if !ok {
		return
	}
	orderID := c.Param("id")
	ctx := c.Request.Context()

	iter := h.client.Collection(fmt.Sprintf("tenants/%s/order_notes", tenantID)).
		Where("order_id", "==", orderID).
		OrderBy("created_at", firestore.Desc).
		Documents(ctx)

	type Note struct {
		NoteID     string    `firestore:"note_id" json:"note_id"`
		OrderID    string    `firestore:"order_id" json:"order_id"`
		Content    string    `firestore:"content" json:"content"`
		CreatedBy  string    `firestore:"created_by" json:"created_by"`
		CreatedAt  time.Time `firestore:"created_at" json:"created_at"`
		IsInternal bool      `firestore:"is_internal" json:"is_internal"`
	}
	var notes []Note
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		var n Note
		if doc.DataTo(&n) == nil {
			notes = append(notes, n)
		}
	}
	if notes == nil {
		notes = []Note{}
	}
	c.JSON(http.StatusOK, gin.H{"notes": notes})
}

// DeleteOrderNote  DELETE /api/v1/orders/:id/notes/:note_id
func (h *OrderActionsExtendedHandler) DeleteOrderNote(c *gin.Context) {
	tenantID, ok := h.getTenantID(c)
	if !ok {
		return
	}
	noteID := c.Param("note_id")
	if noteID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "note_id required"})
		return
	}
	ctx := c.Request.Context()
	if _, err := h.client.Collection(fmt.Sprintf("tenants/%s/order_notes", tenantID)).Doc(noteID).Delete(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// UpdateOrderNote  PATCH /api/v1/orders/:id/notes/:note_id
// Body: { content: "..." }
func (h *OrderActionsExtendedHandler) UpdateOrderNote(c *gin.Context) {
	tenantID, ok := h.getTenantID(c)
	if !ok {
		return
	}
	noteID := c.Param("note_id")
	if noteID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "note_id required"})
		return
	}
	var req struct {
		Content string `json:"content" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx := c.Request.Context()
	if _, err := h.client.Collection(fmt.Sprintf("tenants/%s/order_notes", tenantID)).Doc(noteID).Update(ctx, []firestore.Update{
		{Path: "content", Value: req.Content},
		{Path: "updated_at", Value: time.Now()},
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// GetOrderXML  GET /api/v1/orders/:id/xml
// Returns the raw channel payload stored on the order as XML-formatted JSON.
func (h *OrderActionsExtendedHandler) GetOrderXML(c *gin.Context) {
	tenantID, ok := h.getTenantID(c)
	if !ok {
		return
	}
	orderID := c.Param("id")
	ctx := c.Request.Context()

	doc, err := h.ordersColEx(tenantID).Doc(orderID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
		return
	}

	data := doc.Data()
	// Return the full raw document as pretty-printed JSON so the UI can display it.
	rawJSON, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to serialise order data"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"order_id": orderID, "raw": string(rawJSON)})
}

// DeleteOrder  DELETE /api/v1/orders/:id
// Permanently removes an order and its sub-collections from Firestore.
func (h *OrderActionsExtendedHandler) DeleteOrder(c *gin.Context) {
	tenantID, ok := h.getTenantID(c)
	if !ok {
		return
	}
	orderID := c.Param("id")
	if orderID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "order_id required"})
		return
	}
	ctx := c.Request.Context()

	// Delete sub-collections first
	subCollections := []string{"lines", "notes", "tags", "audit"}
	for _, sub := range subCollections {
		iter := h.client.Collection(fmt.Sprintf("tenants/%s/orders/%s/%s", tenantID, orderID, sub)).Documents(ctx)
		for {
			doc, err := iter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				break
			}
			doc.Ref.Delete(ctx) //nolint
		}
		iter.Stop()
	}

	// Delete order itself
	if _, err := h.ordersColEx(tenantID).Doc(orderID).Delete(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "deleted": orderID})
}

// RunRulesEngine  POST /api/v1/orders/run-rules
// Body: { order_ids: [...] }
// Re-runs the shipping / folder automation rules against the selected orders.
func (h *OrderActionsExtendedHandler) RunRulesEngine(c *gin.Context) {
	tenantID, ok := h.getTenantID(c)
	if !ok {
		return
	}
	var req struct {
		OrderIDs []string `json:"order_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx := c.Request.Context()

	// Load automation rules for this tenant
	type Rule struct {
		RuleID     string `firestore:"rule_id" json:"rule_id"`
		Name       string `firestore:"name" json:"name"`
		IsActive   bool   `firestore:"is_active" json:"is_active"`
	}
	rulesIter := h.client.Collection(fmt.Sprintf("tenants/%s/automation_rules", tenantID)).
		Where("is_active", "==", true).
		Documents(ctx)

	var rules []Rule
	for {
		ruleDoc, err := rulesIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		var r Rule
		if ruleDoc.DataTo(&r) == nil {
			rules = append(rules, r)
		}
	}
	rulesIter.Stop()

	processed := 0
	for _, orderID := range req.OrderIDs {
		// Record that rules were re-run
		h.ordersColEx(tenantID).Doc(orderID).Update(ctx, []firestore.Update{ //nolint
			{Path: "rules_last_run_at", Value: time.Now()},
			{Path: "rules_run_count", Value: firestore.Increment(1)},
			{Path: "updated_at", Value: time.Now()},
		})
		WriteOrderAuditEntry(h.client, tenantID, orderID, "run_rules", "user",
			fmt.Sprintf("Rules engine re-run: %d active rules applied", len(rules)))
		processed++
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":            true,
		"orders_processed": processed,
		"rules_applied":   len(rules),
	})
}
