package services

import (
	"strconv"
	"strings"
	"sync"
	"context"
	"fmt"
	"log"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	"module-a/models"
)

// ============================================================================
// RULE ENGINE
// ============================================================================

// RuleEngine is the main orchestrator: load rules → filter by trigger → parse → evaluate → execute
type RuleEngine struct {
	client    *firestore.Client
	parser    *RuleParser
	evaluator *RuleEvaluator
	executor  *ActionExecutor
}

func NewRuleEngine(client *firestore.Client, smtp *SMTPConfig) *RuleEngine {
	return &RuleEngine{
		client:    client,
		parser:    NewRuleParser(),
		evaluator: NewRuleEvaluator(),
		executor:  NewActionExecutor(client, smtp),
	}
}

// NewRuleEngineWithTemplateService creates a RuleEngine where the notify action
// uses per-tenant Firestore SMTP config loaded via TemplateService.
func NewRuleEngineWithTemplateService(client *firestore.Client, smtp *SMTPConfig, templateSvc *TemplateService) *RuleEngine {
	return &RuleEngine{
		client:    client,
		parser:    NewRuleParser(),
		evaluator: NewRuleEvaluator(),
		executor:  NewActionExecutorWithTemplateService(client, smtp, templateSvc),
	}
}

// ── CRUD ──────────────────────────────────────────────────────────────────────

func (e *RuleEngine) CreateRule(ctx context.Context, tenantID string, rule *models.AutomationRule) error {
	now := time.Now().Format(time.RFC3339)
	rule.TenantID = tenantID
	rule.CreatedAt = now
	rule.UpdatedAt = now
	if rule.RuleID == "" {
		rule.RuleID = fmt.Sprintf("rule_%d", time.Now().UnixNano())
	}
	_, err := e.client.Collection("tenants").Doc(tenantID).
		Collection("automation_rules").Doc(rule.RuleID).Set(ctx, rule)
	return err
}

func (e *RuleEngine) GetRule(ctx context.Context, tenantID, ruleID string) (*models.AutomationRule, error) {
	doc, err := e.client.Collection("tenants").Doc(tenantID).
		Collection("automation_rules").Doc(ruleID).Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("rule not found")
	}
	var rule models.AutomationRule
	if err := doc.DataTo(&rule); err != nil {
		return nil, err
	}
	return &rule, nil
}

func (e *RuleEngine) UpdateRule(ctx context.Context, tenantID string, rule *models.AutomationRule) error {
	rule.UpdatedAt = time.Now().Format(time.RFC3339)
	_, err := e.client.Collection("tenants").Doc(tenantID).
		Collection("automation_rules").Doc(rule.RuleID).Set(ctx, rule)
	return err
}

func (e *RuleEngine) DeleteRule(ctx context.Context, tenantID, ruleID string) error {
	_, err := e.client.Collection("tenants").Doc(tenantID).
		Collection("automation_rules").Doc(ruleID).Delete(ctx)
	return err
}

func (e *RuleEngine) DuplicateRule(ctx context.Context, tenantID, ruleID string) (*models.AutomationRule, error) {
	original, err := e.GetRule(ctx, tenantID, ruleID)
	if err != nil {
		return nil, fmt.Errorf("rule not found")
	}
	now := time.Now().Format(time.RFC3339)
	copy := *original
	copy.RuleID = fmt.Sprintf("rule_%d", time.Now().UnixNano())
	copy.Name = "Copy of " + original.Name
	copy.CreatedAt = now
	copy.UpdatedAt = now
	copy.RunCount = 0
	copy.LastRunAt = ""
	copy.LastRunOK = false
	_, err = e.client.Collection("tenants").Doc(tenantID).
		Collection("automation_rules").Doc(copy.RuleID).Set(ctx, copy)
	if err != nil {
		return nil, err
	}
	return &copy, nil
}

func (e *RuleEngine) ToggleRule(ctx context.Context, tenantID, ruleID string, enabled bool) error {
	_, err := e.client.Collection("tenants").Doc(tenantID).
		Collection("automation_rules").Doc(ruleID).
		Update(ctx, []firestore.Update{
			{Path: "enabled", Value: enabled},
			{Path: "updated_at", Value: time.Now().Format(time.RFC3339)},
		})
	return err
}

func (e *RuleEngine) ListRules(ctx context.Context, tenantID, triggerFilter string) ([]models.AutomationRule, error) {
	q := e.client.Collection("tenants").Doc(tenantID).
		Collection("automation_rules").
		OrderBy("priority", firestore.Asc)

	iter := q.Documents(ctx)
	defer iter.Stop()

	var rules []models.AutomationRule
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		var rule models.AutomationRule
		if err := doc.DataTo(&rule); err != nil {
			log.Printf("[rule_engine] error decoding rule: %v", err)
			continue
		}
		// Filter by trigger if requested
		if triggerFilter != "" {
			found := false
			for _, t := range rule.Triggers {
				if string(t) == triggerFilter {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

// ── VALIDATION ────────────────────────────────────────────────────────────────

// ValidateScript parses the DSL and returns any errors / warnings.
func (e *RuleEngine) ValidateScript(script string) models.ValidationResult {
	parser := NewRuleParser()
	_, errs, warnings := parser.Parse(script)

	result := models.ValidationResult{
		Valid:    len(errs) == 0,
		Errors:   errs,
		Warnings: warnings,
	}
	if result.Errors == nil {
		result.Errors = []models.ValidationError{}
	}
	if result.Warnings == nil {
		result.Warnings = []models.ValidationError{}
	}
	return result
}

// ── EVALUATION (live or dry-run) ──────────────────────────────────────────────

// EvaluateForOrder loads all enabled rules for a trigger, evaluates them against the order,
// optionally executes matched actions, and records history.
func (e *RuleEngine) EvaluateForOrder(
	ctx context.Context,
	tenantID string,
	trigger models.TriggerEvent,
	order *models.Order,
	lines []models.OrderLine,
	dryRun bool,
) (*models.EvaluationReport, error) {
	start := time.Now()

	rules, err := e.ListRules(ctx, tenantID, string(trigger))
	if err != nil {
		return nil, fmt.Errorf("error loading rules: %v", err)
	}

	orderCtx := BuildOrderContext(order, lines)
	report := &models.EvaluationReport{
		OrderID:        order.OrderID,
		RulesEvaluated: 0,
		RulesMatched:   0,
	}

	skipRemaining := false

	for i, rule := range rules {
		if !rule.Enabled {
			continue
		}
		if skipRemaining {
			break
		}

		report.RulesEvaluated++

		parser := NewRuleParser()
		script, errs, _ := parser.Parse(rule.Script)
		if len(errs) > 0 {
			report.Results = append(report.Results, models.RuleResult{
				RuleIndex: i,
				RuleName:  rule.Name,
				Matched:   false,
				Error:     fmt.Sprintf("parse error: %v", errs[0].Message),
			})
			continue
		}

		for blockIdx, block := range script.Rules {
			matched, traces, err := e.evaluator.EvaluateRule(block, orderCtx)
			ruleResult := models.RuleResult{
				RuleIndex:       blockIdx,
				RuleName:        blockOrRuleName(block.Name, rule.Name, blockIdx),
				Matched:         matched,
				ConditionsTrace: traces,
			}

			if err != nil {
				ruleResult.Error = err.Error()
				report.Results = append(report.Results, ruleResult)
				continue
			}

			if matched {
				report.RulesMatched++
				var actionResults []models.ActionResult

				for _, action := range block.Actions {
					ar := e.executor.ExecuteAction(ctx, tenantID, action, orderCtx, dryRun)
					actionResults = append(actionResults, ar)

					if action.Name == "skip_remaining_rules" && !dryRun {
						skipRemaining = true
					}
				}
				ruleResult.ActionsWouldFire = actionResults
			}

			report.Results = append(report.Results, ruleResult)
		}

		// Record history if not a dry run
		if !dryRun {
			durationMS := time.Since(start).Milliseconds()
			e.recordRun(ctx, tenantID, rule.RuleID, trigger, order.OrderID, report, durationMS)
		}
	}

	return report, nil
}

// DryRunScript parses a raw script (not yet saved) and evaluates it against an order
func (e *RuleEngine) DryRunScript(
	ctx context.Context,
	tenantID string,
	script string,
	order *models.Order,
	lines []models.OrderLine,
) (*models.EvaluationReport, error) {
	parser := NewRuleParser()
	ast, errs, _ := parser.Parse(script)
	if len(errs) > 0 {
		return nil, fmt.Errorf("parse error at line %d: %s", errs[0].Line, errs[0].Message)
	}

	orderCtx := BuildOrderContext(order, lines)
	report := &models.EvaluationReport{
		OrderID:        order.OrderID,
		RulesEvaluated: len(ast.Rules),
	}

	for i, block := range ast.Rules {
		matched, traces, err := e.evaluator.EvaluateRule(block, orderCtx)
		ruleResult := models.RuleResult{
			RuleIndex:       i,
			RuleName:        block.Name,
			Matched:         matched,
			ConditionsTrace: traces,
		}
		if err != nil {
			ruleResult.Error = err.Error()
		}
		if matched {
			report.RulesMatched++
			for _, action := range block.Actions {
				ar := e.executor.ExecuteAction(ctx, tenantID, action, orderCtx, true)
				ruleResult.ActionsWouldFire = append(ruleResult.ActionsWouldFire, ar)
			}
		}
		report.Results = append(report.Results, ruleResult)
	}

	return report, nil
}

// ── HISTORY ───────────────────────────────────────────────────────────────────

func (e *RuleEngine) recordRun(
	ctx context.Context,
	tenantID, ruleID string,
	trigger models.TriggerEvent,
	orderID string,
	report *models.EvaluationReport,
	durationMS int64,
) {
	runID := fmt.Sprintf("run_%d", time.Now().UnixNano())
	var actionsExecuted []string
	var errs []string
	matched := false

	for _, res := range report.Results {
		if res.Matched {
			matched = true
		}
		for _, ar := range res.ActionsWouldFire {
			if !ar.Skipped {
				actionsExecuted = append(actionsExecuted, ar.Action)
			}
			if ar.Error != "" {
				errs = append(errs, ar.Error)
			}
		}
		if res.Error != "" {
			errs = append(errs, res.Error)
		}
	}

	run := models.AutomationRuleRun{
		RunID:           runID,
		RuleID:          ruleID,
		TenantID:        tenantID,
		TriggerEvent:    trigger,
		OrderID:         orderID,
		Matched:         matched,
		ActionsExecuted: actionsExecuted,
		Errors:          errs,
		ExecutedAt:      time.Now(),
		DurationMS:      durationMS,
	}

	_, err := e.client.Collection("tenants").Doc(tenantID).
		Collection("automation_rule_runs").Doc(runID).Set(ctx, run)
	if err != nil {
		log.Printf("[rule_engine] error recording run: %v", err)
	}

	// Update rule stats
	updates := []firestore.Update{
		{Path: "run_count", Value: firestore.Increment(1)},
		{Path: "last_run_at", Value: time.Now().Format(time.RFC3339)},
		{Path: "last_run_ok", Value: len(errs) == 0},
	}
	_, _ = e.client.Collection("tenants").Doc(tenantID).
		Collection("automation_rules").Doc(ruleID).Update(ctx, updates)
}

func (e *RuleEngine) GetHistory(ctx context.Context, tenantID, ruleID string) ([]models.AutomationRuleRun, error) {
	iter := e.client.Collection("tenants").Doc(tenantID).
		Collection("automation_rule_runs").
		Where("rule_id", "==", ruleID).
		OrderBy("executed_at", firestore.Desc).
		Limit(100).
		Documents(ctx)
	defer iter.Stop()

	var runs []models.AutomationRuleRun
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		var run models.AutomationRuleRun
		if err := doc.DataTo(&run); err != nil {
			continue
		}
		runs = append(runs, run)
	}
	return runs, nil
}

// ── HELPERS ───────────────────────────────────────────────────────────────────

func blockOrRuleName(blockName, ruleName string, idx int) string {
	if blockName != "" {
		return blockName
	}
	if idx == 0 {
		return ruleName
	}
	return fmt.Sprintf("%s (block %d)", ruleName, idx+1)
}

// ── CRON SCHEDULER ────────────────────────────────────────────────────────────

// CronScheduler manages scheduled automation rules using parsed cron expressions.
// It is a lightweight implementation that supports standard 5-field cron syntax:
// minute hour day-of-month month day-of-week
type CronScheduler struct {
	engine   *RuleEngine
	order    OrderLister
	stopCh   chan struct{}
	entries  []cronEntry
	mu       sync.Mutex
}

// OrderLister abstracts order listing for the cron scheduler
type OrderLister interface {
	ListOrders(ctx context.Context, tenantID string, opts OrderListOptions) ([]models.Order, int, error)
	GetOrderLines(ctx context.Context, tenantID, orderID string) ([]models.OrderLine, error)
}

type cronEntry struct {
	tenantID string
	ruleID   string
	expr     *cronExpr
}

// NewCronScheduler creates a scheduler. Call Start() to begin ticking.
func NewCronScheduler(engine *RuleEngine, orderSvc OrderLister) *CronScheduler {
	return &CronScheduler{
		engine: engine,
		order:  orderSvc,
		stopCh: make(chan struct{}),
	}
}

// LoadTenant loads (or reloads) all enabled SCHEDULE rules for a tenant.
func (s *CronScheduler) LoadTenant(ctx context.Context, tenantID string) error {
	iter := s.engine.client.Collection("tenants").Doc(tenantID).
		Collection("automation_rules").
		Where("enabled", "==", true).
		Documents(ctx)
	defer iter.Stop()

	s.mu.Lock()
	// Remove existing entries for this tenant
	filtered := s.entries[:0]
	for _, e := range s.entries {
		if e.tenantID != tenantID {
			filtered = append(filtered, e)
		}
	}
	s.entries = filtered
	s.mu.Unlock()

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return err
		}
		var rule models.AutomationRule
		if err := doc.DataTo(&rule); err != nil {
			continue
		}
		var hasSched bool
		for _, t := range rule.Triggers {
			if t == models.TriggerSchedule {
				hasSched = true
				break
			}
		}
		if !hasSched || rule.ScheduleCron == "" {
			continue
		}
		expr, err := parseCronExpr(rule.ScheduleCron)
		if err != nil {
			log.Printf("[cron] invalid cron %q for rule %s: %v", rule.ScheduleCron, rule.RuleID, err)
			continue
		}
		s.mu.Lock()
		s.entries = append(s.entries, cronEntry{tenantID: tenantID, ruleID: rule.RuleID, expr: expr})
		s.mu.Unlock()
		log.Printf("[cron] registered rule %s (%s) cron=%q", rule.RuleID, rule.Name, rule.ScheduleCron)
	}
	return nil
}

// RegisterRule adds or updates a single rule in the scheduler.
func (s *CronScheduler) RegisterRule(tenantID string, rule *models.AutomationRule) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Remove existing entry for this rule
	filtered := s.entries[:0]
	for _, e := range s.entries {
		if !(e.tenantID == tenantID && e.ruleID == rule.RuleID) {
			filtered = append(filtered, e)
		}
	}
	s.entries = filtered

	if !rule.Enabled {
		return
	}
	var hasSched bool
	for _, t := range rule.Triggers {
		if t == models.TriggerSchedule {
			hasSched = true
			break
		}
	}
	if !hasSched || rule.ScheduleCron == "" {
		return
	}
	expr, err := parseCronExpr(rule.ScheduleCron)
	if err != nil {
		log.Printf("[cron] invalid cron %q for rule %s: %v", rule.ScheduleCron, rule.RuleID, err)
		return
	}
	s.entries = append(s.entries, cronEntry{tenantID: tenantID, ruleID: rule.RuleID, expr: expr})
	log.Printf("[cron] registered rule %s cron=%q", rule.RuleID, rule.ScheduleCron)
}

// DeregisterRule removes a rule from the scheduler.
func (s *CronScheduler) DeregisterRule(tenantID, ruleID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	filtered := s.entries[:0]
	for _, e := range s.entries {
		if !(e.tenantID == tenantID && e.ruleID == ruleID) {
			filtered = append(filtered, e)
		}
	}
	s.entries = filtered
}

// Start begins the scheduler loop in a goroutine.
func (s *CronScheduler) Start() {
	go func() {
		// Align to the next whole minute
		now := time.Now()
		nextMinute := now.Truncate(time.Minute).Add(time.Minute)
		select {
		case <-time.After(time.Until(nextMinute)):
		case <-s.stopCh:
			return
		}
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		s.tick(time.Now())
		for {
			select {
			case t := <-ticker.C:
				s.tick(t)
			case <-s.stopCh:
				return
			}
		}
	}()
}

// Stop halts the scheduler.
func (s *CronScheduler) Stop() {
	close(s.stopCh)
}

func (s *CronScheduler) tick(t time.Time) {
	s.mu.Lock()
	entries := make([]cronEntry, len(s.entries))
	copy(entries, s.entries)
	s.mu.Unlock()

	for _, entry := range entries {
		if entry.expr.matches(t) {
			go s.runRule(entry.tenantID, entry.ruleID, t)
		}
	}
}

func (s *CronScheduler) runRule(tenantID, ruleID string, t time.Time) {
	ctx := context.Background()
	rule, err := s.engine.GetRule(ctx, tenantID, ruleID)
	if err != nil {
		log.Printf("[cron] rule %s not found: %v", ruleID, err)
		return
	}
	if !rule.Enabled {
		return
	}

	// Iterate open orders and evaluate the rule against each
	orders, _, err := s.order.ListOrders(ctx, tenantID, OrderListOptions{
		Status: "imported",
		Limit:  "500",
	})
	if err != nil {
		log.Printf("[cron] failed to list orders for tenant %s: %v", tenantID, err)
		return
	}

	log.Printf("[cron] firing rule %s against %d open orders at %s", ruleID, len(orders), t.Format(time.RFC3339))
	for _, order := range orders {
		o := order // capture
		lines, _ := s.order.GetOrderLines(ctx, tenantID, o.OrderID)
		_, err := s.engine.EvaluateForOrder(ctx, tenantID, models.TriggerSchedule, &o, lines, false)
		if err != nil {
			log.Printf("[cron] evaluate error for order %s: %v", o.OrderID, err)
		}
	}
}

// ── CRON EXPRESSION PARSER ────────────────────────────────────────────────────

type cronField struct {
	values map[int]bool
	any    bool
}

type cronExpr struct {
	minute  cronField
	hour    cronField
	dom     cronField // day of month
	month   cronField
	dow     cronField // day of week
}

func parseCronExpr(expr string) (*cronExpr, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return nil, fmt.Errorf("expected 5 fields, got %d", len(fields))
	}
	mins, err := parseCronField(fields[0], 0, 59)
	if err != nil { return nil, fmt.Errorf("minute: %w", err) }
	hours, err := parseCronField(fields[1], 0, 23)
	if err != nil { return nil, fmt.Errorf("hour: %w", err) }
	doms, err := parseCronField(fields[2], 1, 31)
	if err != nil { return nil, fmt.Errorf("dom: %w", err) }
	months, err := parseCronField(fields[3], 1, 12)
	if err != nil { return nil, fmt.Errorf("month: %w", err) }
	dows, err := parseCronField(fields[4], 0, 6)
	if err != nil { return nil, fmt.Errorf("dow: %w", err) }
	return &cronExpr{minute: mins, hour: hours, dom: doms, month: months, dow: dows}, nil
}

func parseCronField(s string, min, max int) (cronField, error) {
	if s == "*" {
		return cronField{any: true}, nil
	}
	f := cronField{values: make(map[int]bool)}
	// Handle */step
	if strings.HasPrefix(s, "*/") {
		step, err := strconv.Atoi(s[2:])
		if err != nil || step <= 0 {
			return f, fmt.Errorf("invalid step %q", s)
		}
		for i := min; i <= max; i += step {
			f.values[i] = true
		}
		return f, nil
	}
	// Handle comma-separated values / ranges
	for _, part := range strings.Split(s, ",") {
		if strings.Contains(part, "-") {
			bounds := strings.SplitN(part, "-", 2)
			lo, err1 := strconv.Atoi(bounds[0])
			hi, err2 := strconv.Atoi(bounds[1])
			if err1 != nil || err2 != nil || lo > hi {
				return f, fmt.Errorf("invalid range %q", part)
			}
			for i := lo; i <= hi; i++ {
				f.values[i] = true
			}
		} else {
			v, err := strconv.Atoi(part)
			if err != nil {
				return f, fmt.Errorf("invalid value %q", part)
			}
			f.values[v] = true
		}
	}
	return f, nil
}

func (f cronField) match(v int) bool {
	if f.any { return true }
	return f.values[v]
}

func (e *cronExpr) matches(t time.Time) bool {
	return e.minute.match(t.Minute()) &&
		e.hour.match(t.Hour()) &&
		e.dom.match(t.Day()) &&
		e.month.match(int(t.Month())) &&
		e.dow.match(int(t.Weekday()))
}
