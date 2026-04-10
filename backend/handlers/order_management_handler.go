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

	"module-a/marketplace"
	"module-a/models"
	"module-a/services"
)

// ============================================================================
// ORDER MANAGEMENT HANDLER — B-002, B-003, B-004, B-005, B-006
// ============================================================================
// Provides: manual order creation, order editing, order merge, order split,
// and order cancellation with reason codes.
// ============================================================================

type OrderManagementHandler struct {
	client             *firestore.Client
	orderService       *services.OrderService
	marketplaceService *services.MarketplaceService
	templateSvc        *services.TemplateService
}

func NewOrderManagementHandler(client *firestore.Client, orderService *services.OrderService, marketplaceService *services.MarketplaceService) *OrderManagementHandler {
	return &OrderManagementHandler{client: client, orderService: orderService, marketplaceService: marketplaceService}
}

// SetTemplateService optionally wires in the TemplateService for automated email triggers.
func (h *OrderManagementHandler) SetTemplateService(svc *services.TemplateService) {
	h.templateSvc = svc
}

func (h *OrderManagementHandler) ordersCol(tenantID string) *firestore.CollectionRef {
	return h.client.Collection("tenants").Doc(tenantID).Collection("orders")
}

func (h *OrderManagementHandler) linesCol(tenantID, orderID string) *firestore.CollectionRef {
	return h.client.Collection("tenants").Doc(tenantID).Collection("orders").Doc(orderID).Collection("lines")
}

func (h *OrderManagementHandler) auditCol(tenantID, orderID string) *firestore.CollectionRef {
	return h.client.Collection("tenants").Doc(tenantID).Collection("orders").Doc(orderID).Collection("audit_trail")
}

// addAuditEntry appends an entry to the order's audit trail.
func (h *OrderManagementHandler) addAuditEntry(tenantID, orderID, action, performedBy, notes string) {
	entry := map[string]interface{}{
		"audit_id":     uuid.New().String(),
		"action":       action,
		"performed_by": performedBy,
		"notes":        notes,
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	ctx := context.Background()
	h.auditCol(tenantID, orderID).Doc(entry["audit_id"].(string)).Set(ctx, entry) //nolint
}

// WriteOrderAuditEntry is an exported helper for writing order audit trail entries
// from other handlers (e.g. email sends). Valid email actions:
//   - "email_queued"  — send was initiated
//   - "email_sent"    — send succeeded
//   - "email_failed"  — send failed (include error in notes)
func WriteOrderAuditEntry(client *firestore.Client, tenantID, orderID, action, performedBy, notes string) {
	if tenantID == "" || orderID == "" {
		return
	}
	entry := map[string]interface{}{
		"audit_id":     uuid.New().String(),
		"action":       action,
		"performed_by": performedBy,
		"notes":        notes,
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	ctx := context.Background()
	client.Collection("tenants").Doc(tenantID).
		Collection("orders").Doc(orderID).
		Collection("audit_trail").Doc(entry["audit_id"].(string)).
		Set(ctx, entry) //nolint
}

// ── B-002: POST /api/v1/orders (manual creation) ─────────────────────────────

type ManualOrderLineItemReq struct {
	SKU      string  `json:"sku" binding:"required"`
	Title    string  `json:"title"`
	Quantity int     `json:"quantity" binding:"required,min=1"`
	Price    float64 `json:"price"`
	Currency string  `json:"currency"`
}

type CreateManualOrderRequest struct {
	Channel         string                   `json:"channel"`
	CustomerName    string                   `json:"customer_name" binding:"required"`
	CustomerEmail   string                   `json:"customer_email"`
	CustomerPhone   string                   `json:"customer_phone"`
	ShippingAddress models.Address           `json:"shipping_address" binding:"required"`
	BillingAddress  *models.Address          `json:"billing_address"`
	LineItems       []ManualOrderLineItemReq  `json:"line_items" binding:"required,min=1"`
	ShippingMethod  string                   `json:"shipping_method"`
	Notes           string                   `json:"notes"`
}

func (h *OrderManagementHandler) CreateManualOrder(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	userID := c.GetString("user_id")
	ctx := c.Request.Context()

	var req CreateManualOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Channel == "" {
		req.Channel = "direct"
	}
	currency := "GBP"

	// Build order
	now := time.Now().Format(time.RFC3339)
	orderID := fmt.Sprintf("ord_%d", time.Now().UnixNano())

	var subtotalAmount float64
	for _, li := range req.LineItems {
		subtotalAmount += li.Price * float64(li.Quantity)
		if li.Currency != "" {
			currency = li.Currency
		}
	}

	order := models.Order{
		OrderID:  orderID,
		TenantID: tenantID,
		Channel:  req.Channel,
		ExternalOrderID: fmt.Sprintf("manual-%s", orderID),
		Customer: models.Customer{
			Name:  req.CustomerName,
			Email: req.CustomerEmail,
			Phone: req.CustomerPhone,
		},
		ShippingAddress: req.ShippingAddress,
		BillingAddress:  req.BillingAddress,
		Status:          "imported",
		PaymentStatus:   "captured",
		Totals: models.OrderTotals{
			Subtotal:   models.Money{Amount: subtotalAmount, Currency: currency},
			GrandTotal: models.Money{Amount: subtotalAmount, Currency: currency},
		},
		InternalNotes: req.Notes,
		OrderDate:     now,
		CreatedAt:     now,
		UpdatedAt:     now,
		ImportedAt:    now,
	}

	// Write order doc
	if _, err := h.ordersCol(tenantID).Doc(orderID).Set(ctx, order); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create order: " + err.Error()})
		return
	}

	// Write line items
	for _, li := range req.LineItems {
		lineID := uuid.New().String()
		liCurrency := currency
		if li.Currency != "" {
			liCurrency = li.Currency
		}
		lineTotal := li.Price * float64(li.Quantity)
		lineDoc := models.OrderLine{
			LineID:    lineID,
			SKU:       li.SKU,
			Title:     li.Title,
			Quantity:  li.Quantity,
			UnitPrice: models.Money{Amount: li.Price, Currency: liCurrency},
			LineTotal: models.Money{Amount: lineTotal, Currency: liCurrency},
			Status:    "pending",
		}
		if _, err := h.linesCol(tenantID, orderID).Doc(lineID).Set(ctx, lineDoc); err != nil {
			log.Printf("[CreateManualOrder] failed to write line %s: %v", lineID, err)
		}
	}

	// Audit
	h.addAuditEntry(tenantID, orderID, "created_manually", userID, "Manual order created via UI")

	// Fire order_confirmation automated email trigger
	if h.templateSvc != nil {
		go h.templateSvc.SendEventEmail(context.Background(), tenantID, models.TriggerEventOrderConfirmation, &order)
	}

	c.JSON(http.StatusCreated, gin.H{"order": order, "order_id": orderID})
}

// ── B-003: PATCH /api/v1/orders/:id (edit order) ─────────────────────────────

func (h *OrderManagementHandler) UpdateOrder(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	orderID := c.Param("id")
	userID := c.GetString("user_id")
	ctx := c.Request.Context()

	var req struct {
		CustomerName  *string          `json:"customer_name"`
		CustomerEmail *string          `json:"customer_email"`
		CustomerPhone *string          `json:"customer_phone"`
		ShippingAddress *models.Address `json:"shipping_address"`
		BillingAddress  *models.Address `json:"billing_address"`
		Notes         *string          `json:"notes"`
		LineItems     []struct {
			LineID    string                  `json:"line_id"`
			Title     string                  `json:"title"`
			SKU       string                  `json:"sku"`
			Quantity  int                     `json:"quantity"`
			UnitPrice *models.Money     `json:"unit_price"`
			TaxRate   *float64                `json:"tax_rate"`
		} `json:"line_items"`
		Shipping *models.Money `json:"shipping"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	updates := []firestore.Update{
		{Path: "updated_at", Value: time.Now().Format(time.RFC3339)},
	}

	var auditDetails string

	if req.CustomerName != nil {
		updates = append(updates, firestore.Update{Path: "customer.name", Value: *req.CustomerName})
		auditDetails += fmt.Sprintf("name→%s; ", *req.CustomerName)
	}
	if req.CustomerEmail != nil {
		updates = append(updates, firestore.Update{Path: "customer.email", Value: *req.CustomerEmail})
		auditDetails += "email updated; "
	}
	if req.CustomerPhone != nil {
		updates = append(updates, firestore.Update{Path: "customer.phone", Value: *req.CustomerPhone})
		auditDetails += "phone updated; "
	}
	if req.ShippingAddress != nil {
		updates = append(updates, firestore.Update{Path: "shipping_address", Value: *req.ShippingAddress})
		auditDetails += "shipping address updated; "
	}
	if req.BillingAddress != nil {
		updates = append(updates, firestore.Update{Path: "billing_address", Value: *req.BillingAddress})
		auditDetails += "billing address updated; "
	}
	if req.Notes != nil {
		updates = append(updates, firestore.Update{Path: "internal_notes", Value: *req.Notes})
		auditDetails += "notes updated; "
	}
	if len(req.LineItems) > 0 {
		// Rebuild the line_items array with updated prices/quantities/tax
		lines := make([]map[string]interface{}, len(req.LineItems))
		for i, l := range req.LineItems {
			line := map[string]interface{}{
				"line_id":  l.LineID,
				"title":    l.Title,
				"sku":      l.SKU,
				"quantity": l.Quantity,
			}
			if l.UnitPrice != nil {
				line["unit_price"] = map[string]interface{}{"amount": l.UnitPrice.Amount, "currency": l.UnitPrice.Currency}
				lineTotal := float64(l.Quantity) * l.UnitPrice.Amount
				taxAmount := 0.0
				if l.TaxRate != nil {
					taxAmount = lineTotal * (*l.TaxRate)
					line["tax_rate"] = *l.TaxRate
					line["tax"] = map[string]interface{}{"amount": taxAmount, "currency": l.UnitPrice.Currency}
				}
				line["line_total"] = map[string]interface{}{"amount": lineTotal + taxAmount, "currency": l.UnitPrice.Currency}
			}
			lines[i] = line
		}
		updates = append(updates, firestore.Update{Path: "line_items", Value: lines})
		auditDetails += fmt.Sprintf("%d line items updated; ", len(req.LineItems))

		// Recalculate totals
		var subtotal, totalTax float64
		currency := "GBP"
		for _, l := range req.LineItems {
			if l.UnitPrice != nil {
				lineNet := float64(l.Quantity) * l.UnitPrice.Amount
				subtotal += lineNet
				currency = l.UnitPrice.Currency
				if l.TaxRate != nil {
					totalTax += lineNet * (*l.TaxRate)
				}
			}
		}
		shippingAmount := 0.0
		if req.Shipping != nil {
			shippingAmount = req.Shipping.Amount
		}
		updates = append(updates,
			firestore.Update{Path: "totals.subtotal", Value: map[string]interface{}{"amount": subtotal, "currency": currency}},
			firestore.Update{Path: "totals.tax", Value: map[string]interface{}{"amount": totalTax, "currency": currency}},
			firestore.Update{Path: "totals.grand_total", Value: map[string]interface{}{"amount": subtotal + totalTax + shippingAmount, "currency": currency}},
		)
	}
	if req.Shipping != nil {
		updates = append(updates, firestore.Update{Path: "totals.shipping", Value: map[string]interface{}{"amount": req.Shipping.Amount, "currency": req.Shipping.Currency}})
		auditDetails += fmt.Sprintf("shipping→%.2f; ", req.Shipping.Amount)
	}

	if len(updates) <= 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no fields to update"})
		return
	}

	if _, err := h.ordersCol(tenantID).Doc(orderID).Update(ctx, updates); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update order: " + err.Error()})
		return
	}

	h.addAuditEntry(tenantID, orderID, "order_edited", userID, auditDetails)
	c.JSON(http.StatusOK, gin.H{"updated": true})
}

// ── B-004: POST /api/v1/orders/merge ─────────────────────────────────────────

func (h *OrderManagementHandler) MergeOrders(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	userID := c.GetString("user_id")
	ctx := c.Request.Context()

	var req struct {
		PrimaryOrderID    string   `json:"primary_order_id" binding:"required"`
		OrderIDsToMerge   []string `json:"order_ids_to_merge" binding:"required,min=1"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Verify primary order exists
	primarySnap, err := h.ordersCol(tenantID).Doc(req.PrimaryOrderID).Get(ctx)
	if err != nil || !primarySnap.Exists() {
		c.JSON(http.StatusNotFound, gin.H{"error": "primary order not found"})
		return
	}

	mergedRefs := []string{}

	for _, mergeID := range req.OrderIDsToMerge {
		if mergeID == req.PrimaryOrderID {
			continue // skip self
		}

		// Fetch lines from the order to merge
		linesIter := h.linesCol(tenantID, mergeID).Documents(ctx)
		defer linesIter.Stop()
		for {
			lineDoc, lErr := linesIter.Next()
			if lErr == iterator.Done {
				break
			}
			if lErr != nil {
				log.Printf("[MergeOrders] error iterating lines for %s: %v", mergeID, lErr)
				continue
			}
			var line models.OrderLine
			lineDoc.DataTo(&line)
			// Write to primary order's lines
			newLineID := uuid.New().String()
			line.LineID = newLineID
			if _, wErr := h.linesCol(tenantID, req.PrimaryOrderID).Doc(newLineID).Set(ctx, line); wErr != nil {
				log.Printf("[MergeOrders] failed to copy line %s: %v", lineDoc.Ref.ID, wErr)
			}
		}

		// Cancel/archive the merged order
		cancelNote := fmt.Sprintf("merged into %s", req.PrimaryOrderID)
		h.ordersCol(tenantID).Doc(mergeID).Update(ctx, []firestore.Update{ //nolint
			{Path: "status", Value: "cancelled"},
			{Path: "sub_status", Value: "merged"},
			{Path: "internal_notes", Value: cancelNote},
			{Path: "updated_at", Value: time.Now().Format(time.RFC3339)},
		})
		h.addAuditEntry(tenantID, mergeID, "order_merged", userID, cancelNote)
		mergedRefs = append(mergedRefs, mergeID)
	}

	// Update primary order's child references
	h.ordersCol(tenantID).Doc(req.PrimaryOrderID).Update(ctx, []firestore.Update{ //nolint
		{Path: "child_order_ids", Value: mergedRefs},
		{Path: "updated_at", Value: time.Now().Format(time.RFC3339)},
	})
	h.addAuditEntry(tenantID, req.PrimaryOrderID, "orders_merged_into_this", userID,
		fmt.Sprintf("merged in: %v", mergedRefs))

	// Return updated primary
	updatedSnap, _ := h.ordersCol(tenantID).Doc(req.PrimaryOrderID).Get(ctx)
	var primaryOrder map[string]interface{}
	updatedSnap.DataTo(&primaryOrder)

	c.JSON(http.StatusOK, gin.H{
		"primary_order": primaryOrder,
		"merged_ids":    mergedRefs,
	})
}

// ── B-005: POST /api/v1/orders/:id/split ─────────────────────────────────────

type SplitLineItemReq struct {
	LineID   string `json:"line_id" binding:"required"`
	Quantity int    `json:"quantity" binding:"required,min=1"`
}

func (h *OrderManagementHandler) SplitOrder(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	orderID := c.Param("id")
	userID := c.GetString("user_id")
	ctx := c.Request.Context()

	var req struct {
		LineItemsForNewOrder []SplitLineItemReq `json:"line_items_for_new_order" binding:"required,min=1"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Fetch parent order
	parentSnap, err := h.ordersCol(tenantID).Doc(orderID).Get(ctx)
	if err != nil || !parentSnap.Exists() {
		c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
		return
	}
	var parentOrder models.Order
	parentSnap.DataTo(&parentOrder)

	// Build a map of requested line splits by line_id
	splitMap := map[string]int{}
	for _, sl := range req.LineItemsForNewOrder {
		splitMap[sl.LineID] = sl.Quantity
	}

	// Create new child order
	now := time.Now().Format(time.RFC3339)
	newOrderID := fmt.Sprintf("ord_%d", time.Now().UnixNano())
	newOrder := parentOrder
	newOrder.OrderID = newOrderID
	newOrder.ExternalOrderID = fmt.Sprintf("split-%s", newOrderID)
	newOrder.ParentOrderID = orderID
	newOrder.ChildOrderIDs = nil
	newOrder.Status = "imported"
	newOrder.CreatedAt = now
	newOrder.UpdatedAt = now
	newOrder.ImportedAt = now
	// Reset totals — will be recalculated from new lines
	newOrder.Totals = models.OrderTotals{}

	if _, wErr := h.ordersCol(tenantID).Doc(newOrderID).Set(ctx, newOrder); wErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create split order: " + wErr.Error()})
		return
	}

	var newSubtotal float64
	currency := "GBP"

	// Move requested lines from parent to new order
	for lineID, qty := range splitMap {
		lineSnap, lErr := h.linesCol(tenantID, orderID).Doc(lineID).Get(ctx)
		if lErr != nil || !lineSnap.Exists() {
			log.Printf("[SplitOrder] line %s not found on order %s", lineID, orderID)
			continue
		}
		var line models.OrderLine
		lineSnap.DataTo(&line)

		if qty > line.Quantity {
			qty = line.Quantity
		}

		remaining := line.Quantity - qty

		// Write split portion to new order
		newLine := line
		newLine.LineID = uuid.New().String()
		newLine.Quantity = qty
		newLine.LineTotal = models.Money{
			Amount:   line.UnitPrice.Amount * float64(qty),
			Currency: line.UnitPrice.Currency,
		}
		h.linesCol(tenantID, newOrderID).Doc(newLine.LineID).Set(ctx, newLine) //nolint
		newSubtotal += newLine.LineTotal.Amount
		currency = newLine.LineTotal.Currency

		if remaining <= 0 {
			// Remove line from parent
			h.linesCol(tenantID, orderID).Doc(lineID).Delete(ctx) //nolint
		} else {
			// Update quantity on parent
			line.Quantity = remaining
			line.LineTotal = models.Money{
				Amount:   line.UnitPrice.Amount * float64(remaining),
				Currency: line.UnitPrice.Currency,
			}
			h.linesCol(tenantID, orderID).Doc(lineID).Set(ctx, line) //nolint
		}
	}

	// Update new order totals
	h.ordersCol(tenantID).Doc(newOrderID).Update(ctx, []firestore.Update{ //nolint
		{Path: "totals.subtotal", Value: models.Money{Amount: newSubtotal, Currency: currency}},
		{Path: "totals.grand_total", Value: models.Money{Amount: newSubtotal, Currency: currency}},
	})

	// Update parent to reference child
	h.ordersCol(tenantID).Doc(orderID).Update(ctx, []firestore.Update{ //nolint
		{Path: "child_order_ids", Value: firestore.ArrayUnion(newOrderID)},
		{Path: "updated_at", Value: now},
	})

	h.addAuditEntry(tenantID, orderID, "order_split", userID, fmt.Sprintf("split into %s", newOrderID))
	h.addAuditEntry(tenantID, newOrderID, "order_split_from", userID, fmt.Sprintf("split from %s", orderID))

	c.JSON(http.StatusCreated, gin.H{
		"original_order_id": orderID,
		"new_order_id":      newOrderID,
	})
}

// ── B-006: POST /api/v1/orders/:id/cancel ────────────────────────────────────

func (h *OrderManagementHandler) CancelOrder(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	orderID := c.Param("id")
	userID := c.GetString("user_id")
	ctx := c.Request.Context()

	var req struct {
		Reason string `json:"reason" binding:"required"`
		Notes  string `json:"notes"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	allowedReasons := map[string]bool{
		"customer_request": true,
		"out_of_stock":     true,
		"fraud":            true,
		"duplicate":        true,
		"other":            true,
	}
	if !allowedReasons[req.Reason] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid reason code; must be one of: customer_request, out_of_stock, fraud, duplicate, other"})
		return
	}

	// Check order exists
	orderSnap, err := h.ordersCol(tenantID).Doc(orderID).Get(ctx)
	if err != nil || !orderSnap.Exists() {
		c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
		return
	}

	var order models.Order
	orderSnap.DataTo(&order)

	if order.Status == "cancelled" {
		c.JSON(http.StatusConflict, gin.H{"error": "order is already cancelled"})
		return
	}

	note := req.Reason
	if req.Notes != "" {
		note = req.Reason + ": " + req.Notes
	}

	// Update order status
	updates := []firestore.Update{
		{Path: "status", Value: "cancelled"},
		{Path: "sub_status", Value: req.Reason},
		{Path: "internal_notes", Value: note},
		{Path: "updated_at", Value: time.Now().Format(time.RFC3339)},
	}
	if _, uErr := h.ordersCol(tenantID).Doc(orderID).Update(ctx, updates); uErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to cancel order: " + uErr.Error()})
		return
	}

	// Release inventory reservations associated with this order
	resIter := h.client.Collection("tenants").Doc(tenantID).Collection("inventory_reservations").
		Where("order_id", "==", orderID).
		Where("status", "==", "reserved").
		Documents(ctx)
	defer resIter.Stop()
	for {
		resDoc, rErr := resIter.Next()
		if rErr == iterator.Done {
			break
		}
		if rErr != nil {
			log.Printf("[CancelOrder] error iterating reservations: %v", rErr)
			break
		}
		resDoc.Ref.Update(ctx, []firestore.Update{ //nolint
			{Path: "status", Value: "released"},
			{Path: "released_at", Value: time.Now().Format(time.RFC3339)},
		})
	}

	// Task 12: Restock inventory — return cancelled line quantities to available stock
	go func() {
		bgCtx := context.Background()
		// Fetch order lines to get SKUs and quantities
		linesIter := h.client.Collection("tenants").Doc(tenantID).Collection("orders").
			Doc(orderID).Collection("lines").Documents(bgCtx)
		defer linesIter.Stop()

		for {
			lineDoc, lErr := linesIter.Next()
			if lErr == iterator.Done {
				break
			}
			if lErr != nil {
				log.Printf("[CancelOrder] error fetching lines for restock: %v", lErr)
				break
			}
			var line models.OrderLine
			if err := lineDoc.DataTo(&line); err != nil {
				continue
			}
			qtyToRestock := line.Quantity - line.CancelledQuantity
			if qtyToRestock <= 0 {
				continue
			}
			// Find inventory record by fulfilment source and product
			srcID := line.FulfilmentSourceID
			if srcID == "" {
				// Fall back to order-level warehouse
				srcID = order.WarehouseID
			}
			invQ := h.client.Collection("tenants").Doc(tenantID).Collection("inventory").Query
			if line.ProductID != "" {
				invQ = invQ.Where("product_id", "==", line.ProductID)
			} else if line.SKU != "" {
				invQ = invQ.Where("sku", "==", line.SKU)
			} else {
				continue
			}
			if srcID != "" {
				invQ = invQ.Where("source_id", "==", srcID)
			}
			invIter := invQ.Limit(1).Documents(bgCtx)
			invDoc, invErr := invIter.Next()
			invIter.Stop()
			if invErr != nil {
				// No inventory record found — skip silently
				continue
			}
			// Increment available_qty
			data := invDoc.Data()
			currentAvail, _ := data["available_qty"].(int64)
			currentTotal, _ := data["quantity"].(int64)
			invDoc.Ref.Update(bgCtx, []firestore.Update{
				{Path: "available_qty", Value: int(currentAvail) + qtyToRestock},
				{Path: "quantity", Value: int(currentTotal) + qtyToRestock},
				{Path: "updated_at", Value: time.Now()},
			})
			log.Printf("[CancelOrder] restocked %d units for SKU=%s order=%s", qtyToRestock, line.SKU, orderID)
		}
	}()

	h.addAuditEntry(tenantID, orderID, "order_cancelled", userID, note)

	// Push cancellation to the originating channel if possible.
	go func() {
		bgCtx := context.Background()
		// Fetch the order to get Channel and ChannelAccountID.
		orderDoc, fetchErr := h.ordersCol(tenantID).Doc(orderID).Get(bgCtx)
		if fetchErr != nil {
			log.Printf("[CancelOrder] could not fetch order for channel push: %v", fetchErr)
			return
		}
		var order models.Order
		if mapErr := orderDoc.DataTo(&order); mapErr != nil {
			log.Printf("[CancelOrder] could not map order data: %v", mapErr)
			return
		}
		if order.Channel == "" || order.ChannelAccountID == "" {
			log.Printf("[CancelOrder] order %s has no channel info — skipping channel push", orderID)
			return
		}
		cred, credErr := h.marketplaceService.GetCredential(bgCtx, tenantID, order.ChannelAccountID)
		if credErr != nil {
			log.Printf("[CancelOrder] could not get credential %s: %v", order.ChannelAccountID, credErr)
			return
		}
		fullCreds, credsErr := h.marketplaceService.GetFullCredentials(bgCtx, cred)
		if credsErr != nil {
			log.Printf("[CancelOrder] could not decrypt credentials: %v", credsErr)
			return
		}
		adapter, adapterErr := marketplace.GetAdapter(bgCtx, order.Channel, marketplace.Credentials{
			MarketplaceID:   order.Channel,
			Environment:     cred.Environment,
			MarketplaceType: order.Channel,
			Data:            fullCreds,
		})
		if adapterErr != nil {
			log.Printf("[CancelOrder] no adapter for channel %s: %v", order.Channel, adapterErr)
			return
		}
		if cancelErr := adapter.CancelOrder(bgCtx, order.ExternalOrderID); cancelErr != nil {
			if cancelErr == marketplace.ErrCancelNotSupported {
				log.Printf("[CancelOrder] channel %s does not support cancellation via API — order %s cancelled locally only", order.Channel, orderID)
			} else {
				log.Printf("[CancelOrder] channel push failed for order %s on %s: %v", orderID, order.Channel, cancelErr)
			}
			return
		}
		log.Printf("[CancelOrder] successfully pushed cancellation to %s for order %s", order.Channel, orderID)
	}()

	c.JSON(http.StatusOK, gin.H{"cancelled": true, "order_id": orderID, "reason": req.Reason})
}
