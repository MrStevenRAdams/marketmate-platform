package handlers

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"

	"module-a/models"
	"module-a/services"
)

// ============================================================================
// AUTOMATION HANDLER — Module G Extension
// ============================================================================

type AutomationHandler struct {
	engine    *services.RuleEngine
	order     *services.OrderService
	usage     *UsageInstrumentor
	scheduler *services.CronScheduler
}

func NewAutomationHandler(engine *services.RuleEngine, orderService *services.OrderService) *AutomationHandler {
	return &AutomationHandler{
		engine: engine,
		order:  orderService,
		usage:  NewUsageInstrumentor(nil),
	}
}

func NewAutomationHandlerWithScheduler(engine *services.RuleEngine, orderService *services.OrderService, scheduler *services.CronScheduler) *AutomationHandler {
	return &AutomationHandler{
		engine:    engine,
		order:     orderService,
		usage:     NewUsageInstrumentor(nil),
		scheduler: scheduler,
	}
}

// ── LIST ──────────────────────────────────────────────────────────────────────

// GET /api/v1/automation/rules
func (h *AutomationHandler) ListRules(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant ID required"})
		return
	}

	triggerFilter := c.Query("trigger")
	rules, err := h.engine.ListRules(c.Request.Context(), tenantID, triggerFilter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if rules == nil {
		rules = []models.AutomationRule{}
	}
	c.JSON(http.StatusOK, gin.H{"rules": rules, "count": len(rules)})
}

// ── CREATE ────────────────────────────────────────────────────────────────────

// POST /api/v1/automation/rules
func (h *AutomationHandler) CreateRule(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant ID required"})
		return
	}

	var rule models.AutomationRule
	if err := c.ShouldBindJSON(&rule); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Pre-validate the script before saving
	if rule.Script != "" {
		result := h.engine.ValidateScript(rule.Script)
		if !result.Valid {
			c.JSON(http.StatusUnprocessableEntity, gin.H{
				"error":      "rule script has errors",
				"validation": result,
			})
			return
		}
	}

	rule.CreatedBy = c.GetString("user_id")
	if err := h.engine.CreateRule(c.Request.Context(), tenantID, &rule); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if h.scheduler != nil {
		h.scheduler.RegisterRule(tenantID, &rule)
	}

	c.JSON(http.StatusCreated, rule)
}

// ── GET ───────────────────────────────────────────────────────────────────────

// GET /api/v1/automation/rules/:id
func (h *AutomationHandler) GetRule(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant ID required"})
		return
	}

	ruleID := c.Param("id")
	rule, err := h.engine.GetRule(c.Request.Context(), tenantID, ruleID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "rule not found"})
		return
	}

	c.JSON(http.StatusOK, rule)
}

// ── UPDATE ────────────────────────────────────────────────────────────────────

// PUT /api/v1/automation/rules/:id
func (h *AutomationHandler) UpdateRule(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant ID required"})
		return
	}

	ruleID := c.Param("id")

	// Ensure rule exists
	existing, err := h.engine.GetRule(c.Request.Context(), tenantID, ruleID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "rule not found"})
		return
	}

	var updated models.AutomationRule
	if err := c.ShouldBindJSON(&updated); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate script if changed
	if updated.Script != "" && updated.Script != existing.Script {
		result := h.engine.ValidateScript(updated.Script)
		if !result.Valid {
			c.JSON(http.StatusUnprocessableEntity, gin.H{
				"error":      "rule script has errors",
				"validation": result,
			})
			return
		}
	}

	updated.RuleID = ruleID
	updated.TenantID = tenantID
	updated.CreatedAt = existing.CreatedAt
	updated.CreatedBy = existing.CreatedBy

	if err := h.engine.UpdateRule(c.Request.Context(), tenantID, &updated); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if h.scheduler != nil {
		h.scheduler.RegisterRule(tenantID, &updated)
	}

	c.JSON(http.StatusOK, updated)
}

// ── DELETE ────────────────────────────────────────────────────────────────────

// DELETE /api/v1/automation/rules/:id
func (h *AutomationHandler) DeleteRule(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant ID required"})
		return
	}

	ruleID := c.Param("id")
	if err := h.engine.DeleteRule(c.Request.Context(), tenantID, ruleID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if h.scheduler != nil {
		h.scheduler.DeregisterRule(tenantID, ruleID)
	}

	c.JSON(http.StatusOK, gin.H{"deleted": true})
}

// ── TOGGLE ────────────────────────────────────────────────────────────────────

// PATCH /api/v1/automation/rules/:id/toggle
func (h *AutomationHandler) ToggleRule(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant ID required"})
		return
	}

	ruleID := c.Param("id")

	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.engine.ToggleRule(c.Request.Context(), tenantID, ruleID, body.Enabled); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if h.scheduler != nil {
		if body.Enabled {
			rule, err := h.engine.GetRule(c.Request.Context(), tenantID, ruleID)
			if err == nil {
				h.scheduler.RegisterRule(tenantID, rule)
			}
		} else {
			h.scheduler.DeregisterRule(tenantID, ruleID)
		}
	}

	c.JSON(http.StatusOK, gin.H{"rule_id": ruleID, "enabled": body.Enabled})
}

// ── VALIDATE ─────────────────────────────────────────────────────────────────

// POST /api/v1/automation/rules/validate
func (h *AutomationHandler) ValidateRule(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant ID required"})
		return
	}

	var body struct {
		Script string `json:"script"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result := h.engine.ValidateScript(body.Script)
	c.JSON(http.StatusOK, result)
}

// ── TEST (DRY RUN) ────────────────────────────────────────────────────────────

// POST /api/v1/automation/rules/test
func (h *AutomationHandler) TestRule(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant ID required"})
		return
	}

	var body struct {
		Script  string `json:"script"`
		RuleID  string `json:"rule_id"`
		OrderID string `json:"order_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Resolve script: either provided directly or from a saved rule
	script := body.Script
	if script == "" && body.RuleID != "" {
		rule, err := h.engine.GetRule(c.Request.Context(), tenantID, body.RuleID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "rule not found"})
			return
		}
		script = rule.Script
	}
	if script == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "script or rule_id required"})
		return
	}

	// Resolve order: use provided order ID, or fall back to a sample order
	var order *models.Order
	var lines []models.OrderLine

	if body.OrderID != "" {
		o, err := h.order.GetOrder(c.Request.Context(), tenantID, body.OrderID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
			return
		}
		order = o
		lines, _ = h.order.GetOrderLines(c.Request.Context(), tenantID, body.OrderID)
	} else {
		order, lines = sampleOrder()
	}

	report, err := h.engine.DryRunScript(c.Request.Context(), tenantID, script, order, lines)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}

	h.usage.RecordAPICall(c.Request.Context(), tenantID, "rule_engine", "rule_engine")
	c.JSON(http.StatusOK, report)
}

// ── HISTORY ───────────────────────────────────────────────────────────────────

// GET /api/v1/automation/rules/:id/history
func (h *AutomationHandler) GetRuleHistory(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant ID required"})
		return
	}

	ruleID := c.Param("id")
	runs, err := h.engine.GetHistory(c.Request.Context(), tenantID, ruleID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if runs == nil {
		runs = []models.AutomationRuleRun{}
	}
	c.JSON(http.StatusOK, gin.H{"rule_id": ruleID, "runs": runs})
}

// ── MANUAL TRIGGER ────────────────────────────────────────────────────────────

// POST /api/v1/automation/trigger/:event
func (h *AutomationHandler) TriggerEvent(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant ID required"})
		return
	}

	event := models.TriggerEvent(c.Param("event"))

	var body struct {
		OrderID string `json:"order_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if body.OrderID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "order_id required"})
		return
	}

	order, err := h.order.GetOrder(c.Request.Context(), tenantID, body.OrderID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
		return
	}
	lines, _ := h.order.GetOrderLines(c.Request.Context(), tenantID, body.OrderID)

	report, err := h.engine.EvaluateForOrder(c.Request.Context(), tenantID, event, order, lines, false)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.usage.RecordAPICall(c.Request.Context(), tenantID, "rule_engine", "rule_engine")
	c.JSON(http.StatusOK, report)
}

// ── METADATA ──────────────────────────────────────────────────────────────────

// GET /api/v1/automation/actions
// Returns platform actions plus dispatch_actions for the Evri carrier integration.
// Dispatch actions are returned as a separate key to avoid type conflicts with the
// typed []services.ActionMeta slice returned by GetActionMetadata().
func (h *AutomationHandler) ListActions(c *gin.Context) {
	dispatchActions := []map[string]interface{}{
		{
			"type":        "dispatch.create_shipment",
			"label":       "Auto-Dispatch Shipment",
			"description": "Automatically dispatch an order using the specified carrier and service.",
			"parameters": []map[string]interface{}{
				{"key": "carrier_code", "type": "string", "label": "Carrier code (e.g. evri)", "required": true},
				{"key": "service_code", "type": "string", "label": "Service code (e.g. PARCEL)", "required": true},
				{"key": "include_return_label", "type": "bool", "label": "Generate return label simultaneously", "required": false},
			},
		},
		{
			"type":        "dispatch.generate_return_label",
			"label":       "Generate Return Label",
			"description": "Create a return label for an existing shipment on this order.",
			"parameters": []map[string]interface{}{
				{"key": "notify_customer_email", "type": "bool", "label": "Email return label to customer", "required": false},
				{"key": "notify_customer_sms", "type": "bool", "label": "SMS return label link to customer", "required": false},
			},
		},
		{
			"type":        "dispatch.notify_tracking",
			"label":       "Push Tracking to Marketplace",
			"description": "Submit the tracking number back to the order's sales channel(s). Leave channel blank to notify all channels.",
			"parameters": []map[string]interface{}{
				{"key": "channel", "type": "string", "label": "Channel to notify (leave blank for all)", "required": false},
			},
		},
	}
	c.JSON(http.StatusOK, gin.H{
		"actions":          services.GetActionMetadata(),
		"dispatch_actions": dispatchActions,
	})
}

// GET /api/v1/automation/fields
// Returns platform fields plus dispatch_fields for the Evri carrier integration.
// Shipment fields are returned as a separate key to avoid type conflicts with the
// typed []services.FieldMeta slice returned by GetFieldMetadata().
func (h *AutomationHandler) ListFields(c *gin.Context) {
	shipmentFields := []map[string]interface{}{
		{"key": "shipment.carrier", "label": "Carrier", "type": "string",
			"hint": "Carrier code, e.g. evri, royal_mail, dpd"},
		{"key": "shipment.service", "label": "Carrier Service", "type": "string",
			"hint": "Service code, e.g. PARCEL, PARCEL_NEXT_DAY"},
		{"key": "shipment.tracking_number", "label": "Tracking Number", "type": "string"},
		{"key": "shipment.status", "label": "Shipment Status", "type": "string",
			"hint": "pre_transit, in_transit, out_for_delivery, delivered, attempted_delivery, exception, return_in_progress, returned"},
		{"key": "shipment.destination_country", "label": "Destination Country", "type": "string",
			"hint": "ISO 2-letter code, e.g. GB, US, DE"},
		{"key": "shipment.postcode", "label": "Destination Postcode", "type": "string"},
		{"key": "shipment.has_signature", "label": "Has Signature POD", "type": "bool"},
		{"key": "shipment.has_safe_place_photo", "label": "Has Safe Place Photo", "type": "bool"},
		{"key": "shipment.exception_description", "label": "Exception Description", "type": "string"},
	}
	c.JSON(http.StatusOK, gin.H{
		"fields":          services.GetFieldMetadata(),
		"dispatch_fields": shipmentFields,
	})
}

// ── SHIPMENT TRIGGER EVENTS ───────────────────────────────────────────────────

// FireShipmentEvent evaluates all enabled automation rules against a shipment
// lifecycle event. It is called internally by the dispatch handler and tracking
// sync scheduler — not exposed as an HTTP endpoint.
//
// Supported eventTypes:
//   - "shipment.created"          — fired on successful despatch
//     payload: carrier, service, tracking_number, destination_country, postcode, order_id
//   - "shipment.tracking_updated" — fired when Evri tracking status changes
//     payload: tracking_number, old_status, new_status, carrier, order_id
//   - "shipment.delivered"        — fired on first delivered scan
//     payload: tracking_number, delivered_at, has_signature, has_safe_place_photo, order_id
//   - "shipment.exception"        — fired on exception/held status
//     payload: tracking_number, exception_description, carrier, order_id
func (h *AutomationHandler) FireShipmentEvent(tenantID, eventType string, payload map[string]interface{}) {
	if h.engine == nil {
		return
	}

	ctx := context.Background()

	// Build a synthetic Order so the existing EvaluateForOrder engine can process
	// the event. Shipment fields are injected via Attributes so rule scripts can
	// read them as order.attributes["shipment.carrier"] etc.
	// Encode the shipment payload into Tags using "shipment.key=value" format.
	// models.Order has no generic Attributes map, so Tags is the appropriate
	// extensible string field available on all Order structs.
	tags := []string{"shipment_event:" + eventType}
	for k, v := range payload {
		tags = append(tags, fmt.Sprintf("shipment.%s=%v", k, v))
	}

	syntheticOrder := &models.Order{
		OrderID: func() string {
			if v, ok := payload["order_id"].(string); ok {
				return v
			}
			return "shipment-event-" + eventType
		}(),
		Channel: "system",
		Status:  eventType,
		Tags:    tags,
		ShippingAddress: models.Address{
			Country: func() string {
				if v, ok := payload["destination_country"].(string); ok {
					return v
				}
				return ""
			}(),
			PostalCode: func() string {
				if v, ok := payload["postcode"].(string); ok {
					return v
				}
				return ""
			}(),
		},
	}

	_, _ = h.engine.EvaluateForOrder(ctx, tenantID, models.TriggerEvent(eventType), syntheticOrder, nil, false)
}

// ── DUPLICATE ─────────────────────────────────────────────────────────────────

// POST /api/v1/automation/rules/:id/duplicate
func (h *AutomationHandler) DuplicateRule(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant ID required"})
		return
	}

	ruleID := c.Param("id")
	newRule, err := h.engine.DuplicateRule(c.Request.Context(), tenantID, ruleID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if h.scheduler != nil {
		h.scheduler.RegisterRule(tenantID, newRule)
	}

	c.JSON(http.StatusCreated, newRule)
}

// ── SAMPLE ORDER ──────────────────────────────────────────────────────────────

func sampleOrder() (*models.Order, []models.OrderLine) {
	order := &models.Order{
		OrderID:  "sample-order",
		Channel:  "amazon",
		Status:   "imported",
		Tags:     []string{},
		Customer: models.Customer{Email: "customer@example.com"},
		ShippingAddress: models.Address{
			Country:    "GB",
			PostalCode: "SW1A 1AA",
			City:       "London",
		},
		Totals: models.OrderTotals{
			GrandTotal: models.Money{Amount: 99.99, Currency: "GBP"},
		},
		PaymentMethod: "card",
		PaymentStatus: "captured",
	}
	lines := []models.OrderLine{
		{LineID: "line-1", SKU: "SKU-001", Title: "Sample Product", Quantity: 2, Status: "pending"},
	}
	return order, lines
}

// smtpFromEnv builds an SMTPConfig from environment variables
func smtpFromEnv() *services.SMTPConfig {
	host := os.Getenv("SMTP_HOST")
	if host == "" {
		return nil
	}
	return &services.SMTPConfig{
		Host:     host,
		Port:     os.Getenv("SMTP_PORT"),
		User:     os.Getenv("SMTP_USER"),
		Password: os.Getenv("SMTP_PASSWORD"),
		From:     os.Getenv("SMTP_FROM"),
	}
}
