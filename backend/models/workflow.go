package models

import "time"

// ============================================================================
// WORKFLOW MODEL
// ============================================================================
// A Workflow is a named set of conditions and actions that automatically
// route orders to the right fulfilment source and carrier.
//
// Evaluation order:
//  1. All active workflows for the tenant are loaded, sorted by priority desc
//  2. Each workflow's conditions are evaluated against the order (AND logic)
//  3. First workflow where ALL conditions match has its actions executed
//  4. If no workflow matches, the default fulfilment source is used
//
// Firestore: tenants/{tenant_id}/workflows/{workflow_id}
// ============================================================================

type Workflow struct {
	WorkflowID  string `json:"workflow_id" firestore:"workflow_id"`
	TenantID    string `json:"tenant_id" firestore:"tenant_id"`
	Name        string `json:"name" firestore:"name"`
	Description string `json:"description,omitempty" firestore:"description,omitempty"`

	// Priority: higher number = evaluated first. Range 1–1000.
	Priority int    `json:"priority" firestore:"priority"`
	Status   string `json:"status" firestore:"status"` // draft, active, paused, archived

	// Trigger: what event causes this workflow to be evaluated?
	Trigger WorkflowTrigger `json:"trigger" firestore:"trigger"`

	// Conditions: ALL must match (AND logic)
	// Within a condition group, OR logic can be achieved via the "in" operator
	Conditions []WorkflowCondition `json:"conditions" firestore:"conditions"`

	// Actions: executed in order if all conditions match
	Actions []WorkflowAction `json:"actions" firestore:"actions"`

	// Settings
	Settings WorkflowSettings `json:"settings" firestore:"settings"`

	// Statistics (updated by background job)
	Stats WorkflowStats `json:"stats" firestore:"stats"`

	// Audit
	CreatedAt      time.Time  `json:"created_at" firestore:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at" firestore:"updated_at"`
	CreatedBy      string     `json:"created_by" firestore:"created_by"`
	LastExecutedAt *time.Time `json:"last_executed_at,omitempty" firestore:"last_executed_at,omitempty"`
}

type WorkflowTrigger struct {
	// Type of event that fires this workflow
	Type string `json:"type" firestore:"type"`
	// order.imported         → fires when any new order is saved
	// order.status_changed   → fires when order status changes
	// manual                 → user-triggered only
	// schedule               → cron-based re-evaluation

	// Optional: only fire for specific channels
	Channels []string `json:"channels,omitempty" firestore:"channels,omitempty"` // ["amazon", "ebay"]

	// Optional: only fire for specific statuses (for status_changed trigger)
	Statuses []string `json:"statuses,omitempty" firestore:"statuses,omitempty"`

	// Cron schedule (for schedule trigger)
	Schedule string `json:"schedule,omitempty" firestore:"schedule,omitempty"`
}

type WorkflowSettings struct {
	StopOnError bool   `json:"stop_on_error" firestore:"stop_on_error"`
	TestMode    bool   `json:"test_mode" firestore:"test_mode"` // Evaluate but don't execute actions
	LogLevel    string `json:"log_level" firestore:"log_level"` // minimal, normal, verbose
}

type WorkflowStats struct {
	TotalEvaluated     int     `json:"total_evaluated" firestore:"total_evaluated"`
	TotalMatched       int     `json:"total_matched" firestore:"total_matched"`
	TotalExecuted      int     `json:"total_executed" firestore:"total_executed"`
	TotalFailed        int     `json:"total_failed" firestore:"total_failed"`
	AvgExecutionTimeMs float64 `json:"avg_execution_time_ms" firestore:"avg_execution_time_ms"`
}

// ============================================================================
// CONDITION TYPES
// ============================================================================
// WorkflowCondition is a discriminated union — the Type field determines
// which sub-fields are relevant. All unused sub-fields are omitempty.
// ============================================================================

type WorkflowCondition struct {
	// Type determines which fields below are used
	Type string `json:"type" firestore:"type"`
	// geography, order_value, weight, item_count, sku, channel,
	// tag, time, customer, product_attribute, fulfilment_type

	// --- GEOGRAPHY conditions ---
	// Type: "geography"
	// Field: "country", "postcode_prefix", "region", "city"
	GeoField    string   `json:"geo_field,omitempty" firestore:"geo_field,omitempty"`
	GeoOperator string   `json:"geo_operator,omitempty" firestore:"geo_operator,omitempty"` // equals, not_equals, in, not_in, starts_with
	GeoValue    string   `json:"geo_value,omitempty" firestore:"geo_value,omitempty"`       // single value
	GeoValues   []string `json:"geo_values,omitempty" firestore:"geo_values,omitempty"`     // for in/not_in

	// --- ORDER VALUE conditions ---
	// Type: "order_value"
	// Field: "grand_total", "subtotal", "shipping_paid"
	ValueField    string  `json:"value_field,omitempty" firestore:"value_field,omitempty"`
	ValueOperator string  `json:"value_operator,omitempty" firestore:"value_operator,omitempty"` // gt, lt, gte, lte, eq, between
	ValueAmount   float64 `json:"value_amount,omitempty" firestore:"value_amount,omitempty"`
	ValueMin      float64 `json:"value_min,omitempty" firestore:"value_min,omitempty"` // for between
	ValueMax      float64 `json:"value_max,omitempty" firestore:"value_max,omitempty"` // for between
	ValueCurrency string  `json:"value_currency,omitempty" firestore:"value_currency,omitempty"` // GBP, USD — blank = any

	// --- WEIGHT conditions ---
	// Type: "weight"
	// Field: "total_weight", "heaviest_item"
	WeightField    string  `json:"weight_field,omitempty" firestore:"weight_field,omitempty"`
	WeightOperator string  `json:"weight_operator,omitempty" firestore:"weight_operator,omitempty"` // gt, lt, gte, lte, between
	WeightValue    float64 `json:"weight_value,omitempty" firestore:"weight_value,omitempty"`       // kg
	WeightMin      float64 `json:"weight_min,omitempty" firestore:"weight_min,omitempty"`
	WeightMax      float64 `json:"weight_max,omitempty" firestore:"weight_max,omitempty"`

	// --- ITEM COUNT conditions ---
	// Type: "item_count"
	// Field: "total_quantity", "distinct_skus", "line_count"
	ItemCountField    string `json:"item_count_field,omitempty" firestore:"item_count_field,omitempty"`
	ItemCountOperator string `json:"item_count_operator,omitempty" firestore:"item_count_operator,omitempty"` // gt, lt, gte, lte, eq, between
	ItemCountValue    int    `json:"item_count_value,omitempty" firestore:"item_count_value,omitempty"`
	ItemCountMin      int    `json:"item_count_min,omitempty" firestore:"item_count_min,omitempty"`
	ItemCountMax      int    `json:"item_count_max,omitempty" firestore:"item_count_max,omitempty"`

	// --- SKU conditions ---
	// Type: "sku"
	// Matches if ANY line item in the order matches
	SKUOperator string   `json:"sku_operator,omitempty" firestore:"sku_operator,omitempty"` // equals, in, starts_with, contains, not_in
	SKUValue    string   `json:"sku_value,omitempty" firestore:"sku_value,omitempty"`
	SKUValues   []string `json:"sku_values,omitempty" firestore:"sku_values,omitempty"`
	// Scope: "any_line" (default) or "all_lines"
	SKUScope string `json:"sku_scope,omitempty" firestore:"sku_scope,omitempty"`

	// --- CHANNEL conditions ---
	// Type: "channel"
	ChannelOperator string   `json:"channel_operator,omitempty" firestore:"channel_operator,omitempty"` // equals, in, not_in
	ChannelValue    string   `json:"channel_value,omitempty" firestore:"channel_value,omitempty"`
	ChannelValues   []string `json:"channel_values,omitempty" firestore:"channel_values,omitempty"`

	// --- TAG conditions ---
	// Type: "tag"
	TagOperator string   `json:"tag_operator,omitempty" firestore:"tag_operator,omitempty"` // has, not_has, has_any, has_all
	TagValues   []string `json:"tag_values,omitempty" firestore:"tag_values,omitempty"`

	// --- FULFILMENT TYPE conditions ---
	// Type: "fulfilment_type"
	// Checks if ALL / ANY / NONE of the order lines are a given type
	FulfilmentTypeOperator string `json:"fulfilment_type_operator,omitempty" firestore:"fulfilment_type_operator,omitempty"` // all_are, any_are, none_are
	FulfilmentTypeValue    string `json:"fulfilment_type_value,omitempty" firestore:"fulfilment_type_value,omitempty"`       // stock, dropship, fba

	// --- TIME conditions ---
	// Type: "time"
	// Field: "order_time", "current_time", "promised_ship_by"
	TimeField    string `json:"time_field,omitempty" firestore:"time_field,omitempty"`
	TimeOperator string `json:"time_operator,omitempty" firestore:"time_operator,omitempty"` // before, after, between, within_hours
	TimeValue    string `json:"time_value,omitempty" firestore:"time_value,omitempty"`       // "14:00" or RFC3339
	TimeValueEnd string `json:"time_value_end,omitempty" firestore:"time_value_end,omitempty"`
	TimeHours    int    `json:"time_hours,omitempty" firestore:"time_hours,omitempty"` // for within_hours
	TimeTimezone string `json:"time_timezone,omitempty" firestore:"time_timezone,omitempty"`

	// --- PRODUCT ATTRIBUTE conditions ---
	// Type: "product_attribute"
	// Checks a named attribute on the product record (e.g. "brand", "category")
	AttributeKey      string `json:"attribute_key,omitempty" firestore:"attribute_key,omitempty"`
	AttributeOperator string `json:"attribute_operator,omitempty" firestore:"attribute_operator,omitempty"` // equals, contains, in
	AttributeValue    string `json:"attribute_value,omitempty" firestore:"attribute_value,omitempty"`

	// --- CUSTOMER conditions ---
	// Type: "customer"
	CustomerField    string `json:"customer_field,omitempty" firestore:"customer_field,omitempty"` // email, name
	CustomerOperator string `json:"customer_operator,omitempty" firestore:"customer_operator,omitempty"` // equals, contains, in
	CustomerValue    string `json:"customer_value,omitempty" firestore:"customer_value,omitempty"`

	// --- PAYMENT STATUS conditions ---
	// Type: "payment_status"
	PaymentStatusValue string `json:"payment_status_value,omitempty" firestore:"payment_status_value,omitempty"` // captured, pending, etc.

	// Negation: if true, the result of this condition is inverted
	Negate bool `json:"negate,omitempty" firestore:"negate,omitempty"`

	// Human-readable label shown in the UI
	Label string `json:"label,omitempty" firestore:"label,omitempty"`
}

// ============================================================================
// ACTION TYPES
// ============================================================================
// WorkflowAction is also a discriminated union on Type.
// Actions are executed in order; failure behaviour controlled by StopOnError.
// ============================================================================

type WorkflowAction struct {
	// Type determines which fields below are used
	Type string `json:"type" firestore:"type"`
	// assign_fulfilment_source  → route order to a specific source
	// assign_carrier            → assign carrier + service
	// assign_carrier_cheapest   → pick cheapest available carrier at source
	// assign_carrier_fastest    → pick fastest available carrier at source
	// split_by_fulfilment_type  → split dropship lines into separate fulfilment record
	// merge_check               → check if order can merge with pending orders to same address
	// set_priority              → set order priority field
	// add_tag                   → add tag to order
	// remove_tag                → remove tag from order
	// hold_order                → place order on hold with reason
	// send_notification         → send email/webhook notification
	// require_signature         → flag shipment to require signature
	// saturday_delivery         → flag shipment for Saturday delivery
	// set_insurance             → set insurance value on shipment

	// --- ASSIGN FULFILMENT SOURCE ---
	// Type: "assign_fulfilment_source"
	FulfilmentSourceID string `json:"fulfilment_source_id,omitempty" firestore:"fulfilment_source_id,omitempty"`
	// Strategy for auto-selection (used when source_id is blank)
	FulfilmentStrategy string `json:"fulfilment_strategy,omitempty" firestore:"fulfilment_strategy,omitempty"`
	// specific, nearest, cheapest_shipping, inventory_balanced

	// --- ASSIGN CARRIER ---
	// Type: "assign_carrier"
	CarrierID   string `json:"carrier_id,omitempty" firestore:"carrier_id,omitempty"`
	ServiceCode string `json:"service_code,omitempty" firestore:"service_code,omitempty"`
	// Fallback if primary carrier/service unavailable
	FallbackCarrierID   string `json:"fallback_carrier_id,omitempty" firestore:"fallback_carrier_id,omitempty"`
	FallbackServiceCode string `json:"fallback_service_code,omitempty" firestore:"fallback_service_code,omitempty"`

	// --- SET PRIORITY ---
	// Type: "set_priority"
	Priority string `json:"priority,omitempty" firestore:"priority,omitempty"` // low, normal, high, express

	// --- ADD/REMOVE TAG ---
	// Type: "add_tag" or "remove_tag"
	TagValue string `json:"tag_value,omitempty" firestore:"tag_value,omitempty"`

	// --- HOLD ORDER ---
	// Type: "hold_order"
	HoldReason string `json:"hold_reason,omitempty" firestore:"hold_reason,omitempty"`

	// --- SEND NOTIFICATION ---
	// Type: "send_notification"
	NotificationEmail   string `json:"notification_email,omitempty" firestore:"notification_email,omitempty"`
	NotificationWebhook string `json:"notification_webhook,omitempty" firestore:"notification_webhook,omitempty"`
	NotificationMessage string `json:"notification_message,omitempty" firestore:"notification_message,omitempty"`

	// --- SIGNATURE / SATURDAY / INSURANCE ---
	// Type: "require_signature", "saturday_delivery", "set_insurance"
	InsuranceValue float64 `json:"insurance_value,omitempty" firestore:"insurance_value,omitempty"`
	// 0 = use order value as insurance amount

	// --- ASSIGN TO PICKWAVE ---
	// Type: "assign_to_pickwave"
	// Creates or adds to a pickwave with the given configuration.
	// PickwaveName: template for naming, supports {date}, {wave_number} placeholders
	// PickwaveGrouping: "single_order" | "multi_order" | "sku_batch"
	// PickwaveSortBy: "sku" | "bin_location" | "order_date"
	// PickwaveMaxOrders: 0 = unlimited, >0 = cap per wave (starts a new wave when full)
	// PickwaveMaxItems: 0 = unlimited, >0 = cap per wave
	// PickwaveAssignUser: optional user ID to auto-assign the wave to
	PickwaveName       string `json:"pickwave_name,omitempty" firestore:"pickwave_name,omitempty"`
	PickwaveGrouping   string `json:"pickwave_grouping,omitempty" firestore:"pickwave_grouping,omitempty"`
	PickwaveSortBy     string `json:"pickwave_sort_by,omitempty" firestore:"pickwave_sort_by,omitempty"`
	PickwaveMaxOrders  int    `json:"pickwave_max_orders,omitempty" firestore:"pickwave_max_orders,omitempty"`
	PickwaveMaxItems   int    `json:"pickwave_max_items,omitempty" firestore:"pickwave_max_items,omitempty"`
	PickwaveAssignUser string `json:"pickwave_assign_user,omitempty" firestore:"pickwave_assign_user,omitempty"`

	// Human-readable label shown in the UI
	Label string `json:"label,omitempty" firestore:"label,omitempty"`
}

// ============================================================================
// WORKFLOW EXECUTION MODEL
// ============================================================================
// Records every time the workflow engine evaluates an order.
// Provides complete audit trail for debugging and analytics.
//
// Firestore: tenants/{tenant_id}/workflow_executions/{execution_id}
// ============================================================================

type WorkflowExecution struct {
	ExecutionID string `json:"execution_id" firestore:"execution_id"`
	TenantID    string `json:"tenant_id" firestore:"tenant_id"`

	// The order being evaluated
	OrderID string `json:"order_id" firestore:"order_id"`

	// Trigger type that fired this execution
	TriggerType string `json:"trigger_type" firestore:"trigger_type"`

	// Which workflow matched (blank if none matched)
	MatchedWorkflowID   string `json:"matched_workflow_id,omitempty" firestore:"matched_workflow_id,omitempty"`
	MatchedWorkflowName string `json:"matched_workflow_name,omitempty" firestore:"matched_workflow_name,omitempty"`

	// Full results of condition evaluation for each workflow tested
	WorkflowResults []WorkflowEvalResult `json:"workflow_results" firestore:"workflow_results"`

	// Actions executed (from the matching workflow)
	ActionResults []ActionExecResult `json:"action_results,omitempty" firestore:"action_results,omitempty"`

	// Overall outcome
	Status string `json:"status" firestore:"status"`
	// matched_and_executed, matched_test_mode, no_match, failed, skipped

	// Error detail (if status == failed)
	Error string `json:"error,omitempty" firestore:"error,omitempty"`

	// Performance
	DurationMs int64 `json:"duration_ms" firestore:"duration_ms"`

	// Timestamps
	TriggeredAt time.Time  `json:"triggered_at" firestore:"triggered_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty" firestore:"completed_at,omitempty"`
}

// WorkflowEvalResult captures the evaluation of one workflow against the order
type WorkflowEvalResult struct {
	WorkflowID   string                `json:"workflow_id" firestore:"workflow_id"`
	WorkflowName string                `json:"workflow_name" firestore:"workflow_name"`
	Priority     int                   `json:"priority" firestore:"priority"`
	Matched      bool                  `json:"matched" firestore:"matched"`
	Conditions   []ConditionEvalResult `json:"conditions" firestore:"conditions"`
}

// ConditionEvalResult captures the evaluation of one condition
type ConditionEvalResult struct {
	Type        string      `json:"type" firestore:"type"`
	Label       string      `json:"label,omitempty" firestore:"label,omitempty"`
	Matched     bool        `json:"matched" firestore:"matched"`
	ActualValue interface{} `json:"actual_value,omitempty" firestore:"actual_value,omitempty"`
	Reason      string      `json:"reason,omitempty" firestore:"reason,omitempty"`
}

// ActionExecResult captures the result of one action execution
type ActionExecResult struct {
	Type      string `json:"type" firestore:"type"`
	Label     string `json:"label,omitempty" firestore:"label,omitempty"`
	Success   bool   `json:"success" firestore:"success"`
	Result    string `json:"result,omitempty" firestore:"result,omitempty"`
	Error     string `json:"error,omitempty" firestore:"error,omitempty"`
	DurationMs int64 `json:"duration_ms" firestore:"duration_ms"`
}

// ============================================================================
// WORKFLOW CONDITION TYPE CONSTANTS
// ============================================================================

const (
	ConditionTypeGeography        = "geography"
	ConditionTypeOrderValue       = "order_value"
	ConditionTypeWeight           = "weight"
	ConditionTypeItemCount        = "item_count"
	ConditionTypeSKU              = "sku"
	ConditionTypeChannel          = "channel"
	ConditionTypeTag              = "tag"
	ConditionTypeFulfilmentType   = "fulfilment_type"
	ConditionTypeTime             = "time"
	ConditionTypeProductAttribute = "product_attribute"
	ConditionTypeCustomer         = "customer"
	ConditionTypePaymentStatus    = "payment_status"
)

// ============================================================================
// WORKFLOW ACTION TYPE CONSTANTS
// ============================================================================

const (
	ActionTypeAssignFulfilmentSource = "assign_fulfilment_source"
	ActionTypeAssignCarrier          = "assign_carrier"
	ActionTypeAssignCarrierCheapest  = "assign_carrier_cheapest"
	ActionTypeAssignCarrierFastest   = "assign_carrier_fastest"
	ActionTypeSplitByFulfilmentType  = "split_by_fulfilment_type"
	ActionTypeMergeCheck             = "merge_check"
	ActionTypeSetPriority            = "set_priority"
	ActionTypeAddTag                 = "add_tag"
	ActionTypeRemoveTag              = "remove_tag"
	ActionTypeHoldOrder              = "hold_order"
	ActionTypeSendNotification       = "send_notification"
	ActionTypeRequireSignature       = "require_signature"
	ActionTypeSaturdayDelivery       = "saturday_delivery"
	ActionTypeSetInsurance           = "set_insurance"
	ActionTypeAssignToPickwave       = "assign_to_pickwave"
)

// ============================================================================
// WORKFLOW STATUS CONSTANTS
// ============================================================================

const (
	WorkflowStatusDraft    = "draft"
	WorkflowStatusActive   = "active"
	WorkflowStatusPaused   = "paused"
	WorkflowStatusArchived = "archived"
)

const (
	TriggerTypeOrderImported     = "order.imported"
	TriggerTypeOrderStatusChange = "order.status_changed"
	TriggerTypeManual            = "manual"
	TriggerTypeSchedule          = "schedule"
)

const (
	ExecutionStatusMatchedExecuted = "matched_and_executed"
	ExecutionStatusMatchedTestMode = "matched_test_mode"
	ExecutionStatusNoMatch         = "no_match"
	ExecutionStatusFailed          = "failed"
	ExecutionStatusSkipped         = "skipped"
)
