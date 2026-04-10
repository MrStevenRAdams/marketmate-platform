package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
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
// PURCHASE ORDER HANDLER
// ============================================================================

type PurchaseOrderHandler struct {
	client *firestore.Client
}

func NewPurchaseOrderHandler(client *firestore.Client) *PurchaseOrderHandler {
	return &PurchaseOrderHandler{client: client}
}

func (h *PurchaseOrderHandler) tenantID(c *gin.Context) string {
	if tid := c.GetString("tenant_id"); tid != "" {
		return tid
	}
	return c.GetHeader("X-Tenant-Id")
}

func (h *PurchaseOrderHandler) poCollection(tenantID string) *firestore.CollectionRef {
	return h.client.Collection("tenants").Doc(tenantID).Collection("purchase_orders")
}

// ============================================================================
// AUTO-GENERATE PO NUMBER
// ============================================================================

func (h *PurchaseOrderHandler) generatePONumber(ctx *gin.Context, tenantID string) string {
	year := time.Now().Year()
	// Count existing POs for this tenant this year to get sequence
	q := h.poCollection(tenantID).Where("created_at", ">=", time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)).
		Where("created_at", "<", time.Date(year+1, 1, 1, 0, 0, 0, 0, time.UTC))
	docs := q.Documents(ctx.Request.Context())
	count := 0
	for {
		_, err := docs.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		count++
	}
	docs.Stop()
	return fmt.Sprintf("PO-%d-%04d", year, count+1)
}

// ============================================================================
// CREATE PO — POST /api/v1/purchase-orders
// ============================================================================

type CreatePORequest struct {
	SupplierID  string         `json:"supplier_id" binding:"required"`
	Type        string         `json:"type"`         // standard | dropship
	OrderMethod string         `json:"order_method"` // email | webhook | manual | ftp
	Lines       []CreatePOLine `json:"lines"`
	ExpectedAt  *time.Time     `json:"expected_at,omitempty"`
	Notes       string         `json:"notes,omitempty"`
	Currency    string         `json:"currency,omitempty"`
	OrderIDs    []string       `json:"order_ids,omitempty"`
}

type CreatePOLine struct {
	ProductID   string  `json:"product_id,omitempty"`
	InternalSKU string  `json:"internal_sku"`
	SupplierSKU string  `json:"supplier_sku,omitempty"`
	Description string  `json:"description,omitempty"`
	QtyOrdered  int     `json:"qty_ordered" binding:"required,min=1"`
	UnitCost    float64 `json:"unit_cost,omitempty"`
	Currency    string  `json:"currency,omitempty"`
	Notes       string  `json:"notes,omitempty"`
}

func (h *PurchaseOrderHandler) CreatePO(c *gin.Context) {
	tenantID := h.tenantID(c)
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant ID required"})
		return
	}

	var req CreatePORequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	ctx := c.Request.Context()

	// Look up supplier
	supDoc, err := h.client.Collection("tenants").Doc(tenantID).Collection("suppliers").Doc(req.SupplierID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "supplier not found"})
		return
	}
	var supplier models.Supplier
	supDoc.DataTo(&supplier)

	poType := req.Type
	if poType == "" {
		poType = models.POTypeStandard
	}
	orderMethod := req.OrderMethod
	if orderMethod == "" {
		orderMethod = supplier.OrderMethod
	}
	if orderMethod == "" {
		orderMethod = models.POOrderMethodManual
	}
	currency := req.Currency
	if currency == "" {
		currency = supplier.Currency
	}
	if currency == "" {
		currency = "GBP"
	}

	poID := "po_" + uuid.New().String()
	poNumber := h.generatePONumber(c, tenantID)

	// Build lines
	now := time.Now()
	lines := make([]models.POLine, 0, len(req.Lines))
	var totalCost float64
	for _, l := range req.Lines {
		lineID := "ln_" + uuid.New().String()
		lineCurrency := l.Currency
		if lineCurrency == "" {
			lineCurrency = currency
		}
		line := models.POLine{
			LineID:      lineID,
			ProductID:   l.ProductID,
			InternalSKU: l.InternalSKU,
			SKU:         l.InternalSKU,
			SupplierSKU: l.SupplierSKU,
			Description: l.Description,
			Title:       l.Description,
			QtyOrdered:  l.QtyOrdered,
			Quantity:    l.QtyOrdered,
			QtyReceived: 0,
			UnitCost:    l.UnitCost,
			Currency:    lineCurrency,
			Notes:       l.Notes,
		}
		lines = append(lines, line)
		totalCost += l.UnitCost * float64(l.QtyOrdered)
	}

	po := models.PurchaseOrder{
		POID:         poID,
		TenantID:     tenantID,
		PONumber:     poNumber,
		SupplierID:   req.SupplierID,
		SupplierName: supplier.Name,
		Type:         poType,
		OrderMethod:  orderMethod,
		Status:       models.POStatusDraft,
		Lines:        lines,
		Currency:     currency,
		TotalCost:    totalCost,
		Notes:        req.Notes,
		OrderIDs:     req.OrderIDs,
		CreatedAt:    now,
		UpdatedAt:    now,
		ExpectedAt:   req.ExpectedAt,
	}

	_, err = h.poCollection(tenantID).Doc(poID).Set(ctx, po)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create purchase order"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"purchase_order": po,
		"message":        "Purchase order created as draft",
	})
}

// ============================================================================
// LIST POs — GET /api/v1/purchase-orders
// ============================================================================

func (h *PurchaseOrderHandler) ListPOs(c *gin.Context) {
	tenantID := h.tenantID(c)
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant ID required"})
		return
	}

	ctx := c.Request.Context()
	q := h.poCollection(tenantID).Query

	if status := c.Query("status"); status != "" {
		q = q.Where("status", "==", status)
	}
	if supplierID := c.Query("supplier_id"); supplierID != "" {
		q = q.Where("supplier_id", "==", supplierID)
	}
	if poType := c.Query("type"); poType != "" {
		q = q.Where("type", "==", poType)
	}

	q = q.OrderBy("created_at", firestore.Desc).Limit(200)

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

// ============================================================================
// GET PO — GET /api/v1/purchase-orders/:id
// ============================================================================

func (h *PurchaseOrderHandler) GetPO(c *gin.Context) {
	tenantID := h.tenantID(c)
	poID := c.Param("id")

	doc, err := h.poCollection(tenantID).Doc(poID).Get(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "purchase order not found"})
		return
	}

	var po models.PurchaseOrder
	doc.DataTo(&po)
	c.JSON(http.StatusOK, po)
}

// ============================================================================
// UPDATE PO — PUT /api/v1/purchase-orders/:id (draft only)
// ============================================================================

func (h *PurchaseOrderHandler) UpdatePO(c *gin.Context) {
	tenantID := h.tenantID(c)
	poID := c.Param("id")
	ctx := c.Request.Context()

	doc, err := h.poCollection(tenantID).Doc(poID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "purchase order not found"})
		return
	}

	var existing models.PurchaseOrder
	doc.DataTo(&existing)

	if existing.Status != models.POStatusDraft {
		c.JSON(http.StatusBadRequest, gin.H{"error": "only draft purchase orders can be edited"})
		return
	}

	var req CreatePORequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	// Rebuild lines
	lines := make([]models.POLine, 0, len(req.Lines))
	currency := req.Currency
	if currency == "" {
		currency = existing.Currency
	}
	var totalCost float64
	for _, l := range req.Lines {
		lineID := "ln_" + uuid.New().String()
		lineCurrency := l.Currency
		if lineCurrency == "" {
			lineCurrency = currency
		}
		line := models.POLine{
			LineID:      lineID,
			ProductID:   l.ProductID,
			InternalSKU: l.InternalSKU,
			SKU:         l.InternalSKU,
			SupplierSKU: l.SupplierSKU,
			Description: l.Description,
			Title:       l.Description,
			QtyOrdered:  l.QtyOrdered,
			Quantity:    l.QtyOrdered,
			QtyReceived: 0,
			UnitCost:    l.UnitCost,
			Currency:    lineCurrency,
			Notes:       l.Notes,
		}
		lines = append(lines, line)
		totalCost += l.UnitCost * float64(l.QtyOrdered)
	}

	updates := []firestore.Update{
		{Path: "lines", Value: lines},
		{Path: "total_cost", Value: totalCost},
		{Path: "currency", Value: currency},
		{Path: "notes", Value: req.Notes},
		{Path: "updated_at", Value: time.Now()},
	}
	if req.ExpectedAt != nil {
		updates = append(updates, firestore.Update{Path: "expected_at", Value: req.ExpectedAt})
	}
	if req.OrderMethod != "" {
		updates = append(updates, firestore.Update{Path: "order_method", Value: req.OrderMethod})
	}

	_, err = h.poCollection(tenantID).Doc(poID).Update(ctx, updates)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update purchase order"})
		return
	}

	// Return updated
	updatedDoc, _ := h.poCollection(tenantID).Doc(poID).Get(ctx)
	var updated models.PurchaseOrder
	updatedDoc.DataTo(&updated)
	c.JSON(http.StatusOK, gin.H{"purchase_order": updated, "message": "Purchase order updated"})
}

// ============================================================================
// SEND PO — POST /api/v1/purchase-orders/:id/send
// ============================================================================

func (h *PurchaseOrderHandler) SendPO(c *gin.Context) {
	tenantID := h.tenantID(c)
	poID := c.Param("id")
	ctx := c.Request.Context()

	doc, err := h.poCollection(tenantID).Doc(poID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "purchase order not found"})
		return
	}

	var po models.PurchaseOrder
	doc.DataTo(&po)

	if po.Status != models.POStatusDraft {
		c.JSON(http.StatusBadRequest, gin.H{"error": "only draft purchase orders can be sent"})
		return
	}

	// Look up supplier to get contact email and order method
	supDoc, err := h.client.Collection("tenants").Doc(tenantID).Collection("suppliers").Doc(po.SupplierID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "supplier not found"})
		return
	}
	var supplier models.Supplier
	supDoc.DataTo(&supplier)

	orderMethod := po.OrderMethod
	if orderMethod == "" {
		orderMethod = supplier.OrderMethod
	}
	if orderMethod == "" {
		orderMethod = models.POOrderMethodManual
	}

	now := time.Now()
	var sendErr error

	switch orderMethod {
	case models.POOrderMethodEmail:
		sendErr = h.sendPOByEmail(po, supplier)
	case models.POOrderMethodWebhook:
		sendErr = h.sendPOByWebhook(po, supplier)
	case models.POOrderMethodManual:
		// No outbound call, just mark as sent
	case models.POOrderMethodFTP:
		c.JSON(http.StatusBadRequest, gin.H{"error": "FTP not yet supported"})
		return
	default:
		// Unknown method — treat as manual
		log.Printf("[PO] Unknown order_method %q for PO %s — treating as manual", orderMethod, poID)
	}

	if sendErr != nil {
		log.Printf("[PO] Failed to send PO %s via %s: %v", poID, orderMethod, sendErr)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to send purchase order: " + sendErr.Error()})
		return
	}

	_, err = h.poCollection(tenantID).Doc(poID).Update(ctx, []firestore.Update{
		{Path: "status", Value: models.POStatusSent},
		{Path: "sent_at", Value: now},
		{Path: "sent_via", Value: orderMethod},
		{Path: "updated_at", Value: now},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update PO status"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":  fmt.Sprintf("Purchase order sent via %s", orderMethod),
		"po_id":    poID,
		"status":   models.POStatusSent,
		"sent_via": orderMethod,
	})
}

func (h *PurchaseOrderHandler) sendPOByEmail(po models.PurchaseOrder, supplier models.Supplier) error {
	if supplier.Email == "" {
		return fmt.Errorf("supplier has no email address configured")
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<h2>Purchase Order: %s</h2>", po.PONumber))
	sb.WriteString(fmt.Sprintf("<p><strong>Date:</strong> %s</p>", time.Now().Format("02 Jan 2006")))
	if po.ExpectedAt != nil {
		sb.WriteString(fmt.Sprintf("<p><strong>Expected Delivery:</strong> %s</p>", po.ExpectedAt.Format("02 Jan 2006")))
	}
	if po.Notes != "" {
		sb.WriteString(fmt.Sprintf("<p><strong>Notes:</strong> %s</p>", po.Notes))
	}
	sb.WriteString("<table border='1' cellpadding='6' cellspacing='0' style='border-collapse:collapse;width:100%'>")
	sb.WriteString("<thead><tr><th>SKU</th><th>Supplier SKU</th><th>Description</th><th>Qty</th><th>Unit Cost</th><th>Total</th></tr></thead><tbody>")
	for _, l := range po.Lines {
		lineTotal := l.UnitCost * float64(l.QtyOrdered)
		sb.WriteString(fmt.Sprintf("<tr><td>%s</td><td>%s</td><td>%s</td><td>%d</td><td>%.2f %s</td><td>%.2f %s</td></tr>",
			l.InternalSKU, l.SupplierSKU, l.Description, l.QtyOrdered, l.UnitCost, l.Currency, lineTotal, l.Currency))
	}
	sb.WriteString(fmt.Sprintf("</tbody></table><p><strong>Total: %.2f %s</strong></p>", po.TotalCost, po.Currency))

	subject := fmt.Sprintf("Purchase Order %s", po.PONumber)
	return services.SendRawEmail(supplier.Email, subject, sb.String())
}

func (h *PurchaseOrderHandler) sendPOByWebhook(po models.PurchaseOrder, supplier models.Supplier) error {
	if supplier.WebhookConfig == nil || supplier.WebhookConfig.URL == "" {
		return fmt.Errorf("supplier has no webhook endpoint configured")
	}

	payload, err := json.Marshal(po)
	if err != nil {
		return fmt.Errorf("failed to marshal PO: %w", err)
	}

	method := supplier.WebhookConfig.Method
	if method == "" {
		method = "POST"
	}

	req, err := http.NewRequest(method, supplier.WebhookConfig.URL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range supplier.WebhookConfig.Headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// ============================================================================
// RECEIVE GOODS-IN — POST /api/v1/purchase-orders/:id/receive
// ============================================================================

type ReceiveGoodsRequest struct {
	Lines []ReceiveLineInput `json:"lines" binding:"required"`
	Notes string             `json:"notes,omitempty"`
}

type ReceiveLineInput struct {
	LineID      string `json:"line_id" binding:"required"`
	QtyReceived int    `json:"qty_received" binding:"required,min=0"`
	Notes       string `json:"notes,omitempty"`
}

func (h *PurchaseOrderHandler) ReceiveGoods(c *gin.Context) {
	tenantID := h.tenantID(c)
	poID := c.Param("id")
	ctx := c.Request.Context()

	doc, err := h.poCollection(tenantID).Doc(poID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "purchase order not found"})
		return
	}

	var po models.PurchaseOrder
	doc.DataTo(&po)

	if po.Status == models.POStatusCancelled {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot receive goods on a cancelled PO"})
		return
	}
	// NOTE: "received" status POs can still accept additional deliveries (overages, split shipments)

	var req ReceiveGoodsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	// Map line_id → line for efficient lookup
	lineMap := make(map[string]*models.POLine, len(po.Lines))
	for i := range po.Lines {
		lineMap[po.Lines[i].LineID] = &po.Lines[i]
	}

	receiptID := "rcpt_" + uuid.New().String()
	now := time.Now()

	receiptLines := make([]models.ReceiptLine, 0, len(req.Lines))
	variances := make(map[string]int)

	for _, rl := range req.Lines {
		line, ok := lineMap[rl.LineID]
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("line %s not found on this PO", rl.LineID)})
			return
		}
		expectedRemaining := line.QtyOrdered - line.QtyReceived
		variance := rl.QtyReceived - expectedRemaining

		receiptLines = append(receiptLines, models.ReceiptLine{
			LineID:      rl.LineID,
			QtyReceived: rl.QtyReceived,
			Variance:    variance,
			Notes:       rl.Notes,
		})
		variances[rl.LineID] = variance

		// Update the line's received qty
		line.QtyReceived += rl.QtyReceived

		// Create inventory movement for each line received
		if rl.QtyReceived > 0 {
			h.createInventoryMovement(c, tenantID, line.SKU, rl.QtyReceived, po.POID, po.PONumber)
		}
	}

	receipt := models.POReceipt{
		ReceiptID:  receiptID,
		ReceivedAt: now,
		ReceivedBy: c.GetString("user_id"),
		Lines:      receiptLines,
		Notes:      req.Notes,
	}

	// Determine new status
	allFullyReceived := true
	anyReceived := false
	for _, line := range po.Lines {
		if line.QtyReceived >= line.QtyOrdered {
			anyReceived = true
		} else {
			allFullyReceived = false
			if line.QtyReceived > 0 {
				anyReceived = true
			}
		}
	}

	newStatus := po.Status
	if allFullyReceived {
		newStatus = models.POStatusReceived
	} else if anyReceived {
		newStatus = models.POStatusPartiallyReceived
	}

	// Append receipt and update lines + status
	updatedReceipts := append(po.Receipts, receipt)
	_, err = h.poCollection(tenantID).Doc(poID).Update(ctx, []firestore.Update{
		{Path: "lines", Value: po.Lines},
		{Path: "receipts", Value: updatedReceipts},
		{Path: "status", Value: newStatus},
		{Path: "updated_at", Value: now},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to record receipt"})
		return
	}

	// Build variance summary
	var varianceSummary []gin.H
	for _, rl := range receiptLines {
		varianceSummary = append(varianceSummary, gin.H{
			"line_id":      rl.LineID,
			"qty_received": rl.QtyReceived,
			"variance":     rl.Variance,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"message":          "Goods received successfully",
		"receipt_id":       receiptID,
		"po_id":            poID,
		"new_status":       newStatus,
		"variance_summary": varianceSummary,
	})
}

// createInventoryMovement records a goods-in movement in the inventory collection.
func (h *PurchaseOrderHandler) createInventoryMovement(c *gin.Context, tenantID, sku string, qty int, poID, poNumber string) {
	ctx := c.Request.Context()
	movementID := "mv_" + uuid.New().String()
	movement := map[string]interface{}{
		"movement_id":  movementID,
		"sku":          sku,
		"location_id":  "default",
		"type":         "receipt",
		"quantity":     qty,
		"reason_code":  "po_receipt",
		"reference_id": poID,
		"created_by":   c.GetString("user_id"),
		"created_at":   time.Now(),
		"notes":        fmt.Sprintf("PO %s goods-in", poNumber),
	}
	_, err := h.client.Collection("tenants").Doc(tenantID).Collection("movements").Doc(movementID).Set(ctx, movement)
	if err != nil {
		log.Printf("[PO] Failed to record inventory movement for SKU %s PO %s: %v", sku, poID, err)
	}

	// Also increment inventory on_hand
	_ = updateInventoryStock(ctx, h.client, tenantID, sku, "default", qty)
}

// ============================================================================
// CANCEL PO — POST /api/v1/purchase-orders/:id/cancel
// ============================================================================

func (h *PurchaseOrderHandler) CancelPO(c *gin.Context) {
	tenantID := h.tenantID(c)
	poID := c.Param("id")
	ctx := c.Request.Context()

	doc, err := h.poCollection(tenantID).Doc(poID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "purchase order not found"})
		return
	}

	var po models.PurchaseOrder
	doc.DataTo(&po)

	if po.Status == models.POStatusCancelled {
		c.JSON(http.StatusBadRequest, gin.H{"error": "purchase order is already cancelled"})
		return
	}
	if po.Status == models.POStatusReceived {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot cancel a fully received purchase order"})
		return
	}

	_, err = h.poCollection(tenantID).Doc(poID).Update(ctx, []firestore.Update{
		{Path: "status", Value: models.POStatusCancelled},
		{Path: "updated_at", Value: time.Now()},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to cancel purchase order"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Purchase order cancelled", "po_id": poID})
}

// ============================================================================
// ADD TRACKING — POST /api/v1/purchase-orders/:id/tracking
// ============================================================================

type AddTrackingRequest struct {
	TrackingNumber string `json:"tracking_number" binding:"required"`
	TrackingURL    string `json:"tracking_url,omitempty"`
	CarrierName    string `json:"carrier_name,omitempty"`
}

func (h *PurchaseOrderHandler) AddTracking(c *gin.Context) {
	tenantID := h.tenantID(c)
	poID := c.Param("id")
	ctx := c.Request.Context()

	_, err := h.poCollection(tenantID).Doc(poID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "purchase order not found"})
		return
	}

	var req AddTrackingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tracking_number is required"})
		return
	}

	now := time.Now()
	updates := []firestore.Update{
		{Path: "tracking_number", Value: req.TrackingNumber},
		{Path: "shipped_at", Value: now},
		{Path: "updated_at", Value: now},
	}
	if req.TrackingURL != "" {
		updates = append(updates, firestore.Update{Path: "tracking_url", Value: req.TrackingURL})
	}
	if req.CarrierName != "" {
		updates = append(updates, firestore.Update{Path: "carrier_name", Value: req.CarrierName})
	}

	_, err = h.poCollection(tenantID).Doc(poID).Update(ctx, updates)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to add tracking"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":         "Tracking number added",
		"po_id":           poID,
		"tracking_number": req.TrackingNumber,
	})
}

// ============================================================================
// AUTO-GENERATE POs — POST /api/v1/purchase-orders/auto-generate
// ============================================================================

type AutoGenerateRequest struct {
	Source string `json:"source"` // "low_stock" | "awaiting_stock" | "" (both)
}

func (h *PurchaseOrderHandler) AutoGenerate(c *gin.Context) {
	tenantID := h.tenantID(c)
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant ID required"})
		return
	}

	var req AutoGenerateRequest
	c.ShouldBindJSON(&req)

	ctx := c.Request.Context()
	createdPOIDs := []string{}
	warnings := []string{}

	// ── 1. Low stock products ──
	if req.Source == "" || req.Source == "low_stock" {
		productIter := h.client.Collection("tenants").Doc(tenantID).Collection("products").Documents(ctx)
		defer productIter.Stop()

		// Group low-stock products by default supplier
		// supplierID → []POLine
		type lineAccum struct {
			supplierName string
			orderMethod  string
			lines        []CreatePOLine
		}
		supplierGroups := map[string]*lineAccum{}

		for {
			doc, err := productIter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				log.Printf("[PO] auto-generate: error iterating products: %v", err)
				break
			}

			var product models.Product
			if err := doc.DataTo(&product); err != nil {
				continue
			}

			// Read reorder_point and quantity from attributes map
			attrs := product.Attributes
			reorderPoint := 0
			currentQty := 0
			if attrs != nil {
				if v, ok := attrs["reorder_point"]; ok {
					switch val := v.(type) {
					case int64:
						reorderPoint = int(val)
					case float64:
						reorderPoint = int(val)
					case int:
						reorderPoint = val
					}
				}
				if v, ok := attrs["quantity"]; ok {
					switch val := v.(type) {
					case int64:
						currentQty = int(val)
					case float64:
						currentQty = int(val)
					case int:
						currentQty = val
					}
				}
			}

			if reorderPoint == 0 || currentQty >= reorderPoint {
				continue
			}

			// Find default supplier from attributes
			var defaultSupplier *models.ProductSupplier
			if suppliersRaw, ok := attrs["suppliers"]; ok {
				suppliersJSON, _ := json.Marshal(suppliersRaw)
				var productSuppliers []models.ProductSupplier
				if json.Unmarshal(suppliersJSON, &productSuppliers) == nil {
					for i := range productSuppliers {
						if productSuppliers[i].IsDefault {
							defaultSupplier = &productSuppliers[i]
							break
						}
					}
					// Fallback: highest priority (lowest Priority value)
					if defaultSupplier == nil && len(productSuppliers) > 0 {
						defaultSupplier = &productSuppliers[0]
						for i := range productSuppliers {
							if productSuppliers[i].Priority < defaultSupplier.Priority {
								defaultSupplier = &productSuppliers[i]
							}
						}
					}
				}
			}

			if defaultSupplier == nil {
				msg := fmt.Sprintf("product %s (%s) has no default supplier configured — skipped", product.SKU, product.ProductID)
				log.Printf("[PO] auto-generate: %s", msg)
				warnings = append(warnings, msg)
				continue
			}

			qtyToOrder := reorderPoint - currentQty + reorderPoint // order enough to reach 2x reorder point
			if qtyToOrder <= 0 {
				qtyToOrder = reorderPoint
			}

			line := CreatePOLine{
				ProductID:   product.ProductID,
				InternalSKU: product.SKU,
				SupplierSKU: defaultSupplier.SupplierSKU,
				Description: product.Title,
				QtyOrdered:  qtyToOrder,
				UnitCost:    defaultSupplier.UnitCost,
				Currency:    defaultSupplier.Currency,
			}

			group, exists := supplierGroups[defaultSupplier.SupplierID]
			if !exists {
				supplierGroups[defaultSupplier.SupplierID] = &lineAccum{
					supplierName: defaultSupplier.SupplierName,
					lines:        []CreatePOLine{line},
				}
			} else {
				group.lines = append(group.lines, line)
			}
		}

		// Create one PO per supplier group
		for supplierID, group := range supplierGroups {
			poID, poNum, err := h.createDraftPO(c, tenantID, supplierID, group.supplierName, group.lines)
			if err != nil {
				log.Printf("[PO] auto-generate: failed to create PO for supplier %s: %v", supplierID, err)
				warnings = append(warnings, fmt.Sprintf("failed to create PO for supplier %s: %v", supplierID, err))
				continue
			}
			log.Printf("[PO] auto-generate: created %s (%s) for supplier %s with %d lines", poNum, poID, supplierID, len(group.lines))
			createdPOIDs = append(createdPOIDs, poID)
		}
	}

	// ── 2. Orders awaiting stock ──
	if req.Source == "" || req.Source == "awaiting_stock" {
		ordersIter := h.client.Collection("tenants").Doc(tenantID).Collection("orders").
			Where("status", "==", "awaiting_stock").Documents(ctx)
		defer ordersIter.Stop()

		type awaitingGroup struct {
			supplierName string
			lines        []CreatePOLine
			orderIDs     []string
		}
		awaitingGroups := map[string]*awaitingGroup{}

		for {
			orderDoc, err := ordersIter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				break
			}

			var orderData map[string]interface{}
			orderDoc.DataTo(&orderData)
			orderID := orderDoc.Ref.ID

			linesRaw, _ := orderData["lines"].([]interface{})
			for _, lr := range linesRaw {
				lineMap, ok := lr.(map[string]interface{})
				if !ok {
					continue
				}
				sku, _ := lineMap["sku"].(string)
				if sku == "" {
					continue
				}
				qty := 0
				switch v := lineMap["quantity"].(type) {
				case int64:
					qty = int(v)
				case float64:
					qty = int(v)
				}
				if qty <= 0 {
					continue
				}

				// Find product by SKU to get default supplier
				prodIter := h.client.Collection("tenants").Doc(tenantID).Collection("products").
					Where("sku", "==", sku).Limit(1).Documents(ctx)
				prodDoc, prodErr := prodIter.Next()
				prodIter.Stop()
				if prodErr != nil {
					continue
				}
				var product models.Product
				prodDoc.DataTo(&product)

				var defaultSupplier *models.ProductSupplier
				if suppliersRaw, ok := product.Attributes["suppliers"]; ok {
					suppliersJSON, _ := json.Marshal(suppliersRaw)
					var productSuppliers []models.ProductSupplier
					if json.Unmarshal(suppliersJSON, &productSuppliers) == nil {
						for i := range productSuppliers {
							if productSuppliers[i].IsDefault {
								defaultSupplier = &productSuppliers[i]
								break
							}
						}
					}
				}

				if defaultSupplier == nil {
					continue
				}

				line := CreatePOLine{
					ProductID:   product.ProductID,
					InternalSKU: sku,
					SupplierSKU: defaultSupplier.SupplierSKU,
					Description: product.Title,
					QtyOrdered:  qty,
					UnitCost:    defaultSupplier.UnitCost,
					Currency:    defaultSupplier.Currency,
				}

				group, exists := awaitingGroups[defaultSupplier.SupplierID]
				if !exists {
					awaitingGroups[defaultSupplier.SupplierID] = &awaitingGroup{
						supplierName: defaultSupplier.SupplierName,
						lines:        []CreatePOLine{line},
						orderIDs:     []string{orderID},
					}
				} else {
					group.lines = append(group.lines, line)
					// Append order ID if not already present
					found := false
					for _, id := range group.orderIDs {
						if id == orderID {
							found = true
							break
						}
					}
					if !found {
						group.orderIDs = append(group.orderIDs, orderID)
					}
				}
			}
		}

		for supplierID, group := range awaitingGroups {
			poID, poNum, err := h.createDraftPOWithOrders(c, tenantID, supplierID, group.supplierName, group.lines, group.orderIDs)
			if err != nil {
				log.Printf("[PO] auto-generate awaiting_stock: failed for supplier %s: %v", supplierID, err)
				continue
			}
			log.Printf("[PO] auto-generate awaiting_stock: created %s (%s)", poNum, poID)
			createdPOIDs = append(createdPOIDs, poID)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"message":         fmt.Sprintf("Auto-generate complete: %d purchase order(s) created as draft", len(createdPOIDs)),
		"created_po_ids":  createdPOIDs,
		"created_count":   len(createdPOIDs),
		"warnings":        warnings,
	})
}

// createDraftPO is a helper to create a draft PO and return its ID and number.
func (h *PurchaseOrderHandler) createDraftPO(c *gin.Context, tenantID, supplierID, supplierName string, lines []CreatePOLine) (string, string, error) {
	return h.createDraftPOWithOrders(c, tenantID, supplierID, supplierName, lines, nil)
}

func (h *PurchaseOrderHandler) createDraftPOWithOrders(c *gin.Context, tenantID, supplierID, supplierName string, lines []CreatePOLine, orderIDs []string) (string, string, error) {
	ctx := c.Request.Context()

	// Look up supplier for order method
	supDoc, err := h.client.Collection("tenants").Doc(tenantID).Collection("suppliers").Doc(supplierID).Get(ctx)
	orderMethod := models.POOrderMethodManual
	if err == nil {
		var supplier models.Supplier
		supDoc.DataTo(&supplier)
		if supplier.OrderMethod != "" {
			orderMethod = supplier.OrderMethod
		}
	}

	poID := "po_" + uuid.New().String()
	poNumber := h.generatePONumber(c, tenantID)
	now := time.Now()

	poLines := make([]models.POLine, 0, len(lines))
	var totalCost float64
	for _, l := range lines {
		lineID := "ln_" + uuid.New().String()
		poLines = append(poLines, models.POLine{
			LineID:      lineID,
			ProductID:   l.ProductID,
			InternalSKU: l.InternalSKU,
			SKU:         l.InternalSKU,
			SupplierSKU: l.SupplierSKU,
			Description: l.Description,
			Title:       l.Description,
			QtyOrdered:  l.QtyOrdered,
			Quantity:    l.QtyOrdered,
			QtyReceived: 0,
			UnitCost:    l.UnitCost,
			Currency:    l.Currency,
		})
		totalCost += l.UnitCost * float64(l.QtyOrdered)
	}

	po := models.PurchaseOrder{
		POID:         poID,
		TenantID:     tenantID,
		PONumber:     poNumber,
		SupplierID:   supplierID,
		SupplierName: supplierName,
		Type:         models.POTypeStandard,
		OrderMethod:  orderMethod,
		Status:       models.POStatusDraft,
		Lines:        poLines,
		TotalCost:    totalCost,
		OrderIDs:     orderIDs,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	_, err = h.client.Collection("tenants").Doc(tenantID).Collection("purchase_orders").Doc(poID).Set(ctx, po)
	if err != nil {
		return "", "", err
	}

	return poID, poNumber, nil
}

// GetDueInByProduct returns the total quantity on open PO lines (ordered minus received)
// grouped by SKU. Used by the product list "Due In" column.
// GET /api/v1/purchase-orders/due-in?skus=SKU1,SKU2,...
func (h *PurchaseOrderHandler) GetDueInByProduct(c *gin.Context) {
	tenantID := h.tenantID(c)
	skusParam := c.Query("skus")
	var filterSKUs map[string]bool
	if skusParam != "" {
		filterSKUs = make(map[string]bool)
		for _, s := range strings.Split(skusParam, ",") {
			if s = strings.TrimSpace(s); s != "" {
				filterSKUs[s] = true
			}
		}
	}

	ctx := c.Request.Context()
	// Query only open/sent POs (not received/cancelled)
	iter := h.poCollection(tenantID).
		Where("status", "in", []string{"draft", "sent", "partially_received"}).
		Documents(ctx)

	dueIn := make(map[string]int) // sku → qty due
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read POs"})
			return
		}
		var po models.PurchaseOrder
		if err := doc.DataTo(&po); err != nil {
			continue
		}
		for _, line := range po.Lines {
			sku := line.SKU
			if sku == "" {
				sku = line.InternalSKU
			}
			if filterSKUs != nil && !filterSKUs[sku] {
				continue
			}
			outstanding := line.QtyOrdered - line.QtyReceived
			if outstanding < 0 {
				outstanding = 0
			}
			dueIn[sku] += outstanding
		}
	}

	c.JSON(http.StatusOK, gin.H{"due_in": dueIn})
}
