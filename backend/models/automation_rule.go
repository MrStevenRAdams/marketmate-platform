package models

import "time"

// ============================================================================
// AUTOMATION RULE MODEL
// ============================================================================

// TriggerEvent represents the system event that can trigger rule evaluation
type TriggerEvent string

const (
	TriggerOrderCreated       TriggerEvent = "ORDER_CREATED"
	TriggerOrderStatusChanged TriggerEvent = "ORDER_STATUS_CHANGED"
	TriggerOrderTagged        TriggerEvent = "ORDER_TAGGED"
	TriggerShipmentCreated    TriggerEvent = "SHIPMENT_CREATED"
	TriggerShipmentFailed     TriggerEvent = "SHIPMENT_FAILED"
	TriggerInventoryLow       TriggerEvent = "INVENTORY_LOW"
	TriggerManual             TriggerEvent = "MANUAL"
	TriggerSchedule           TriggerEvent = "SCHEDULE"
)

// AutomationRule represents a DSL-based automation rule stored in Firestore
type AutomationRule struct {
	RuleID      string         `json:"rule_id" firestore:"rule_id"`
	TenantID    string         `json:"tenant_id" firestore:"tenant_id"`
	Name        string         `json:"name" firestore:"name"`
	Description string         `json:"description,omitempty" firestore:"description,omitempty"`
	Script      string         `json:"script" firestore:"script"`
	Triggers    []TriggerEvent `json:"triggers" firestore:"triggers"`
	Enabled      bool           `json:"enabled" firestore:"enabled"`
	Priority     int            `json:"priority" firestore:"priority"`
	ScheduleCron string         `json:"schedule_cron,omitempty" firestore:"schedule_cron,omitempty"`

	// Session 7: Macro Configuration System
	// MacroType links the rule to a built-in macro template (e.g. "low_stock_notification").
	MacroType string `json:"macro_type,omitempty" firestore:"macro_type,omitempty"`
	// Parameters holds the primary parameter values for the macro.
	Parameters map[string]interface{} `json:"parameters,omitempty" firestore:"parameters,omitempty"`
	// Schedule defines when a scheduled macro should run.
	Schedule *MacroSchedule `json:"schedule,omitempty" firestore:"schedule,omitempty"`
	// Configurations supports multiple named parameter sets per macro.
	Configurations []RuleConfig `json:"configurations,omitempty" firestore:"configurations,omitempty"`

	// Stats
	RunCount  int    `json:"run_count" firestore:"run_count"`
	LastRunAt string `json:"last_run_at,omitempty" firestore:"last_run_at,omitempty"`
	LastRunOK bool   `json:"last_run_ok" firestore:"last_run_ok"`

	CreatedAt string `json:"created_at" firestore:"created_at"`
	UpdatedAt string `json:"updated_at" firestore:"updated_at"`
	CreatedBy string `json:"created_by,omitempty" firestore:"created_by,omitempty"`
}

// AutomationRuleRun is an execution history entry stored in Firestore
type AutomationRuleRun struct {
	RunID           string        `json:"run_id" firestore:"run_id"`
	RuleID          string        `json:"rule_id" firestore:"rule_id"`
	TenantID        string        `json:"tenant_id" firestore:"tenant_id"`
	TriggerEvent    TriggerEvent  `json:"trigger_event" firestore:"trigger_event"`
	OrderID         string        `json:"order_id,omitempty" firestore:"order_id,omitempty"`
	Matched         bool          `json:"matched" firestore:"matched"`
	ActionsExecuted []string      `json:"actions_executed,omitempty" firestore:"actions_executed,omitempty"`
	Errors          []string      `json:"errors,omitempty" firestore:"errors,omitempty"`
	ExecutedAt      time.Time     `json:"executed_at" firestore:"executed_at"`
	DurationMS      int64         `json:"duration_ms" firestore:"duration_ms"`
}

// ============================================================================
// AST NODE TYPES
// ============================================================================

// NodeType identifies the kind of AST node
type NodeType string

const (
	NodeScript    NodeType = "Script"
	NodeRule      NodeType = "Rule"
	NodeAnd       NodeType = "And"
	NodeOr        NodeType = "Or"
	NodeCondition NodeType = "Condition"
	NodeAction    NodeType = "Action"
)

// RuleScript is the root AST node — holds all parsed WHEN/THEN blocks
type RuleScript struct {
	Rules []RuleBlock
}

// RuleBlock is a single WHEN ... THEN ... block
type RuleBlock struct {
	Name       string // extracted from comment on preceding line
	Condition  ConditionNode
	Actions    []ActionNode
	LineNumber int
}

// ConditionNode represents the WHEN clause
type ConditionNode struct {
	Type  NodeType      // NodeAnd | NodeOr | NodeCondition
	Left  *ConditionNode
	Right *ConditionNode
	Expr  *ExprNode // only set when Type == NodeCondition
}

// ExprNode is a single condition expression: field operator value
type ExprNode struct {
	Field    string  // e.g. "order.channel"
	Operator string  // "==", "!=", ">", ">=", "<", "<=", "IN", "NOT IN", "MATCHES", "NOT MATCHES"
	Value    string  // right-hand side, raw string
	LineNum  int
	ColNum   int
}

// ActionNode is a single action call in the THEN block
type ActionNode struct {
	Name      string   // e.g. "select_carrier"
	Params    []string // raw param strings
	IfCond    *ExprNode // optional inline IF condition
	LineNum   int
	ColNum    int
}

// ============================================================================
// EVALUATION / RESULT TYPES
// ============================================================================

// OrderContext is the flat evaluation context derived from an Order + lines
type OrderContext struct {
	// Order fields
	Channel          string
	TotalGBP         float64
	ShippingCostGBP  float64 // Shipping line value (Totals.Shipping.Amount)
	WeightGrams      float64
	ItemCount        int
	ShippingCountry  string
	ShippingPostcode string
	ShippingCity     string
	Status           string
	PaymentMethod    string
	PaymentStatus    string
	Tags             []string
	CustomerEmail    string

	// First/primary line fields
	LineSKU      string
	LineQuantity int
	LineTitle    string

	// Back-reference for function-style conditions
	AllSKUs []string

	// Dispatch date scheduling (Fix 2B)
	PlacedHour int    // 0–23, hour of day the order was placed (UTC)
	PlacedDate string // ISO date string "YYYY-MM-DD"

	// The original order (for actions to mutate)
	Order *Order
	Lines []OrderLine
}

// ValidationError describes a parse/validation error with position info
type ValidationError struct {
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Message  string `json:"message"`
	Severity string `json:"severity"` // "error" | "warning"
}

// ValidationResult is the response from POST /automation/rules/validate
type ValidationResult struct {
	Valid    bool              `json:"valid"`
	Errors   []ValidationError `json:"errors"`
	Warnings []ValidationError `json:"warnings"`
}

// ConditionTrace shows the evaluation trace of a single expression
type ConditionTrace struct {
	Expression string      `json:"expression"`
	Result     bool        `json:"result"`
	Value      interface{} `json:"value"`
}

// ActionResult describes what would happen (or happened) for one action
type ActionResult struct {
	Action  string   `json:"action"`
	Params  []string `json:"params"`
	DryRun  bool     `json:"dry_run,omitempty"`
	Skipped bool     `json:"skipped,omitempty"`
	Reason  string   `json:"reason,omitempty"`
	Error   string   `json:"error,omitempty"`
}

// RuleResult is the per-rule evaluation result in a dry run or live run
type RuleResult struct {
	RuleIndex        int              `json:"rule_index"`
	RuleName         string           `json:"rule_name,omitempty"`
	Matched          bool             `json:"matched"`
	ConditionsTrace  []ConditionTrace `json:"conditions_trace"`
	ActionsWouldFire []ActionResult   `json:"actions_would_fire,omitempty"`
	Error            string           `json:"error,omitempty"`
}

// EvaluationReport is the full dry-run or live-run report
type EvaluationReport struct {
	OrderID        string       `json:"order_id,omitempty"`
	RulesEvaluated int          `json:"rules_evaluated"`
	RulesMatched   int          `json:"rules_matched"`
	Results        []RuleResult `json:"results"`
}
