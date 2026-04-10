package handlers

// ============================================================================
// CANCELLATION ALERT HANDLER
//
// Manages marketplace cancellation alerts — created when a buyer cancels an
// order via webhook (eBay CANCELLATION_CREATED, Temu bg_cancel_order_status_change,
// Amazon ORDER_CHANGE). Behaviour is governed by the tenant's global cancellation
// policy stored in settings/notifications.
//
// Routes:
//   GET    /api/v1/cancellation-alerts                  List pending alerts
//   POST   /api/v1/cancellation-alerts/:id/acknowledge  Acknowledge with action
// ============================================================================

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// CancellationAlert represents a single marketplace cancellation that requires
// staff acknowledgement before it can be resolved.
type CancellationAlert struct {
	AlertID        string    `json:"alert_id" firestore:"alert_id"`
	TenantID       string    `json:"tenant_id" firestore:"tenant_id"`
	OrderID        string    `json:"order_id" firestore:"order_id"`
	OrderNumber    string    `json:"order_number" firestore:"order_number"`
	ExternalOrderID string   `json:"external_order_id" firestore:"external_order_id"`
	Channel        string    `json:"channel" firestore:"channel"`
	LabelPrinted   bool      `json:"label_printed" firestore:"label_printed"`
	// Status: "pending" | "returned_to_stock" | "shipped" | "under_review" | "auto_cancelled"
	Status         string    `json:"status" firestore:"status"`
	CancelReason   string    `json:"cancel_reason,omitempty" firestore:"cancel_reason,omitempty"`
	AcknowledgedBy string    `json:"acknowledged_by,omitempty" firestore:"acknowledged_by,omitempty"`
	AcknowledgedAt *time.Time `json:"acknowledged_at,omitempty" firestore:"acknowledged_at,omitempty"`
	CreatedAt      time.Time `json:"created_at" firestore:"created_at"`
	UpdatedAt      time.Time `json:"updated_at" firestore:"updated_at"`
}

// CancellationPolicy holds global cancellation behaviour settings.
// Stored under tenants/{tid}/settings/notifications_config.
type CancellationPolicy struct {
	// NoLabelAction: what to do when a cancellation arrives and no label has been printed.
	// "auto_cancel" | "block_label" | "none"   — default: "block_label"
	NoLabelAction string `json:"no_label_action" firestore:"no_label_action"`
	// LabelPrintedNotification: how to alert staff when a label has already been printed.
	// "onscreen" | "message" | "both"           — default: "both"
	LabelPrintedNotification string `json:"label_printed_notification" firestore:"label_printed_notification"`
}

func defaultCancellationPolicy() CancellationPolicy {
	return CancellationPolicy{
		NoLabelAction:            "block_label",
		LabelPrintedNotification: "both",
	}
}

// CancellationAlertHandler handles cancellation alert CRUD and acknowledgement.
type CancellationAlertHandler struct {
	client *firestore.Client
}

func NewCancellationAlertHandler(client *firestore.Client) *CancellationAlertHandler {
	return &CancellationAlertHandler{client: client}
}

// alertsCol returns the Firestore collection reference for a tenant's alerts.
func (h *CancellationAlertHandler) alertsCol(tenantID string) *firestore.CollectionRef {
	return h.client.Collection(fmt.Sprintf("tenants/%s/cancellation_alerts", tenantID))
}

// ── GET /api/v1/cancellation-alerts ──────────────────────────────────────────

// ListAlerts returns all pending (unacknowledged) cancellation alerts for the tenant.
func (h *CancellationAlertHandler) ListAlerts(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	iter := h.alertsCol(tenantID).
		Where("status", "==", "pending").
		OrderBy("created_at", firestore.Desc).
		Limit(100).
		Documents(ctx)
	defer iter.Stop()

	alerts := []CancellationAlert{}
	for {
		doc, err := iter.Next()
		if err != nil {
			break
		}
		var a CancellationAlert
		doc.DataTo(&a)
		alerts = append(alerts, a)
	}

	c.JSON(http.StatusOK, gin.H{"alerts": alerts, "count": len(alerts)})
}

// ── POST /api/v1/cancellation-alerts/:id/acknowledge ─────────────────────────

// AcknowledgeAlert records staff acknowledgement and acts on the chosen resolution.
//
//	{ "action": "returned_to_stock" | "shipped" }
//
// returned_to_stock: restores stock for each order line, sets order cancelled.
// shipped:           sets order status to "under_review" for manual follow-up.
func (h *CancellationAlertHandler) AcknowledgeAlert(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	alertID := c.Param("id")
	ctx := c.Request.Context()

	var req struct {
		Action string `json:"action" binding:"required"` // "returned_to_stock" | "shipped"
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Action != "returned_to_stock" && req.Action != "shipped" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "action must be 'returned_to_stock' or 'shipped'"})
		return
	}

	// Fetch the alert
	alertRef := h.alertsCol(tenantID).Doc(alertID)
	alertSnap, err := alertRef.Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "alert not found"})
		return
	}
	var alert CancellationAlert
	alertSnap.DataTo(&alert)

	if alert.Status != "pending" {
		c.JSON(http.StatusConflict, gin.H{"error": "alert already acknowledged"})
		return
	}

	now := time.Now()
	newAlertStatus := req.Action // "returned_to_stock" or "shipped"

	// ── Act on the order ──────────────────────────────────────────────────────
	if alert.OrderID != "" {
		orderRef := h.client.Collection(fmt.Sprintf("tenants/%s/orders", tenantID)).Doc(alert.OrderID)

		switch req.Action {
		case "returned_to_stock":
			// Cancel the order and restore stock for each line
			orderSnap, err := orderRef.Get(ctx)
			if err == nil {
				h.restoreStockForOrder(ctx, tenantID, alert.OrderID, orderSnap)
			}
			orderRef.Update(ctx, []firestore.Update{
				{Path: "status", Value: "cancelled"},
				{Path: "sub_status", Value: "returned_to_stock"},
				{Path: "updated_at", Value: now.Format(time.RFC3339)},
				{Path: "cancellation_alert_id", Value: alertID},
				{Path: "cancellation_acknowledged", Value: true},
			})

		case "shipped":
			// Flag for manual review — the item was already shipped
			orderRef.Update(ctx, []firestore.Update{
				{Path: "status", Value: "under_review"},
				{Path: "sub_status", Value: "cancelled_post_despatch"},
				{Path: "updated_at", Value: now.Format(time.RFC3339)},
				{Path: "cancellation_alert_id", Value: alertID},
				{Path: "cancellation_acknowledged", Value: true},
			})
			newAlertStatus = "under_review"
		}
	}

	// ── Update the alert ─────────────────────────────────────────────────────
	acknowledgedBy := c.GetString("user_id")
	if acknowledgedBy == "" {
		acknowledgedBy = "unknown"
	}
	alertRef.Update(ctx, []firestore.Update{
		{Path: "status", Value: newAlertStatus},
		{Path: "acknowledged_by", Value: acknowledgedBy},
		{Path: "acknowledged_at", Value: now},
		{Path: "updated_at", Value: now},
	})

	log.Printf("[CancellationAlert] Acknowledged alert=%s tenant=%s action=%s order=%s",
		alertID, tenantID, req.Action, alert.OrderID)

	c.JSON(http.StatusOK, gin.H{
		"ok":     true,
		"action": req.Action,
		"status": newAlertStatus,
	})
}

// restoreStockForOrder increments inventory quantities for each order line,
// writing an inventory adjustment record per line.
func (h *CancellationAlertHandler) restoreStockForOrder(
	ctx context.Context,
	tenantID, orderID string,
	orderSnap *firestore.DocumentSnapshot,
) {
	data := orderSnap.Data()
	linesRaw, _ := data["lines"].([]interface{})
	if len(linesRaw) == 0 {
		return
	}

	now := time.Now()

	for _, lineRaw := range linesRaw {
		line, ok := lineRaw.(map[string]interface{})
		if !ok {
			continue
		}
		productID, _ := line["product_id"].(string)
		qty := int64(0)
		switch v := line["quantity"].(type) {
		case int64:
			qty = v
		case float64:
			qty = int64(v)
		}
		if productID == "" || qty <= 0 {
			continue
		}

		// Find the first inventory record for this product to get location
		invIter := h.client.Collection(fmt.Sprintf("tenants/%s/inventory", tenantID)).
			Where("product_id", "==", productID).
			Limit(1).
			Documents(ctx)
		invDoc, err := invIter.Next()
		invIter.Stop()
		if err != nil {
			log.Printf("[CancellationAlert] No inventory record for product=%s tenant=%s", productID, tenantID)
			continue
		}

		locationID, _ := invDoc.Data()["location_id"].(string)
		inventoryDocID := productID + "__" + locationID
		inventoryRef := h.client.Collection(fmt.Sprintf("tenants/%s/inventory", tenantID)).Doc(inventoryDocID)

		// Increment quantity in a transaction
		err = h.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
			snap, err := tx.Get(inventoryRef)
			if err != nil {
				return err
			}
			current := int64(0)
			if v, ok := snap.Data()["quantity"].(int64); ok {
				current = v
			} else if v, ok := snap.Data()["quantity"].(float64); ok {
				current = int64(v)
			}
			available := int64(0)
			if v, ok := snap.Data()["available_qty"].(int64); ok {
				available = v
			} else if v, ok := snap.Data()["available_qty"].(float64); ok {
				available = int64(v)
			}
			return tx.Update(inventoryRef, []firestore.Update{
				{Path: "quantity", Value: current + qty},
				{Path: "available_qty", Value: available + qty},
				{Path: "updated_at", Value: now},
			})
		})
		if err != nil {
			log.Printf("[CancellationAlert] Stock restore failed product=%s: %v", productID, err)
			continue
		}

		// Write adjustment record
		adjID := uuid.New().String()
		h.client.Collection(fmt.Sprintf("tenants/%s/inventory_adjustments", tenantID)).Doc(adjID).Set(ctx, map[string]interface{}{
			"adjustment_id": adjID,
			"product_id":    productID,
			"location_id":   locationID,
			"delta":         qty,
			"type":          "cancellation_return",
			"reason":        "Marketplace cancellation acknowledged — returned to stock",
			"reference":     orderID,
			"order_id":      orderID,
			"created_at":    now,
		})

		log.Printf("[CancellationAlert] Restored %d units for product=%s order=%s", qty, productID, orderID)
	}
}

// ── GetPolicy / SavePolicy ────────────────────────────────────────────────────
// These are called by settings_handler.go — not registered as separate routes.

func GetCancellationPolicy(ctx context.Context, client *firestore.Client, tenantID string) CancellationPolicy {
	policy := defaultCancellationPolicy()
	doc, err := client.Collection("tenants").Doc(tenantID).
		Collection("settings").Doc("notifications_config").Get(ctx)
	if err != nil || !doc.Exists() {
		return policy
	}
	data := doc.Data()
	if v, ok := data["no_label_action"].(string); ok && v != "" {
		policy.NoLabelAction = v
	}
	if v, ok := data["label_printed_notification"].(string); ok && v != "" {
		policy.LabelPrintedNotification = v
	}
	return policy
}

func SaveCancellationPolicy(ctx context.Context, client *firestore.Client, tenantID string, policy CancellationPolicy) error {
	_, err := client.Collection("tenants").Doc(tenantID).
		Collection("settings").Doc("notifications_config").Set(ctx, map[string]interface{}{
		"no_label_action":             policy.NoLabelAction,
		"label_printed_notification":  policy.LabelPrintedNotification,
		"updated_at":                  time.Now(),
	}, firestore.MergeAll)
	return err
}

// ── CreateCancellationAlert ───────────────────────────────────────────────────
// Called from order_webhook_handler.go when a cancellation webhook arrives.
// Reads the tenant's policy, then either auto-cancels or creates a pending alert.

func CreateCancellationAlert(
	ctx context.Context,
	client *firestore.Client,
	tenantID, credID, channel, orderNumber, externalOrderID, cancelReason string,
) {
	if client == nil || tenantID == "" {
		return
	}

	// Look up the internal order
	orderID := ""
	labelPrinted := false
	orderIter := client.Collection(fmt.Sprintf("tenants/%s/orders", tenantID)).
		Where("external_order_id", "==", externalOrderID).
		Limit(1).Documents(ctx)
	orderDoc, err := orderIter.Next()
	orderIter.Stop()
	if err == nil && orderDoc.Exists() {
		orderID = orderDoc.Ref.ID
		if v, ok := orderDoc.Data()["label_generated"].(bool); ok {
			labelPrinted = v
		}
	}

	// Read tenant policy
	policy := GetCancellationPolicy(ctx, client, tenantID)

	now := time.Now()
	alertID := uuid.New().String()

	// If no label printed and policy is auto_cancel — cancel immediately, no alert needed
	if !labelPrinted && policy.NoLabelAction == "auto_cancel" && orderID != "" {
		client.Collection(fmt.Sprintf("tenants/%s/orders", tenantID)).Doc(orderID).Update(ctx, []firestore.Update{
			{Path: "status", Value: "cancelled"},
			{Path: "sub_status", Value: "auto_cancelled_by_marketplace"},
			{Path: "updated_at", Value: now.Format(time.RFC3339)},
		})
		// Write a resolved alert for audit trail
		client.Collection(fmt.Sprintf("tenants/%s/cancellation_alerts", tenantID)).Doc(alertID).Set(ctx, CancellationAlert{
			AlertID:         alertID,
			TenantID:        tenantID,
			OrderID:         orderID,
			OrderNumber:     orderNumber,
			ExternalOrderID: externalOrderID,
			Channel:         channel,
			LabelPrinted:    false,
			Status:          "auto_cancelled",
			CancelReason:    cancelReason,
			CreatedAt:       now,
			UpdatedAt:       now,
		})
		log.Printf("[CancellationAlert] Auto-cancelled order=%s tenant=%s", orderID, tenantID)
		return
	}

	// If no label printed and policy is block_label — update order to block printing
	if !labelPrinted && policy.NoLabelAction == "block_label" && orderID != "" {
		client.Collection(fmt.Sprintf("tenants/%s/orders", tenantID)).Doc(orderID).Update(ctx, []firestore.Update{
			{Path: "sub_status", Value: "cancellation_pending"},
			{Path: "label_blocked", Value: true},
			{Path: "updated_at", Value: now.Format(time.RFC3339)},
		})
	}

	// Create a pending alert for staff to acknowledge
	alert := CancellationAlert{
		AlertID:         alertID,
		TenantID:        tenantID,
		OrderID:         orderID,
		OrderNumber:     orderNumber,
		ExternalOrderID: externalOrderID,
		Channel:         channel,
		LabelPrinted:    labelPrinted,
		Status:          "pending",
		CancelReason:    cancelReason,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	_, err = client.Collection(fmt.Sprintf("tenants/%s/cancellation_alerts", tenantID)).
		Doc(alertID).Set(ctx, alert)
	if err != nil {
		log.Printf("[CancellationAlert] Failed to create alert: %v", err)
		return
	}

	log.Printf("[CancellationAlert] Created alert=%s tenant=%s order=%s labelPrinted=%v channel=%s",
		alertID, tenantID, orderID, labelPrinted, channel)
}
