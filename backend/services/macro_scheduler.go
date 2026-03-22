package services

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	"module-a/models"
)

// ============================================================================
// MACRO SCHEDULER — Session 7
// ============================================================================
// On startup, reads all enabled automation rules with a Schedule set.
// Uses a ticker/goroutine to evaluate which scheduled macros are due
// and executes them via the rule engine.
//
// Schedule types supported:
//   one_time   — run once at RunAt
//   daily      — run every day at TimeOfDay (HH:MM)
//   weekly     — run on DayOfWeek at TimeOfDay
//   monthly    — run on DayOfMonth at TimeOfDay
//   interval   — run every IntervalMinutes minutes
// ============================================================================

type MacroScheduler struct {
	client      *firestore.Client
	executor    *ActionExecutor
	templateSvc *TemplateService
	stopCh      chan struct{}
}

func NewMacroScheduler(client *firestore.Client, executor *ActionExecutor, templateSvc *TemplateService) *MacroScheduler {
	return &MacroScheduler{
		client:      client,
		executor:    executor,
		templateSvc: templateSvc,
		stopCh:      make(chan struct{}),
	}
}

// Start launches the scheduler goroutine. Call from main after service init.
func (s *MacroScheduler) Start(ctx context.Context) {
	go s.run(ctx)
	log.Println("[MacroScheduler] started")
}

// Stop signals the scheduler to shut down gracefully.
func (s *MacroScheduler) Stop() {
	close(s.stopCh)
}

func (s *MacroScheduler) run(ctx context.Context) {
	// Check every minute
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	// Run immediately on startup
	s.tick(ctx)

	for {
		select {
		case <-ticker.C:
			s.tick(ctx)
		case <-s.stopCh:
			log.Println("[MacroScheduler] stopped")
			return
		case <-ctx.Done():
			return
		}
	}
}

func (s *MacroScheduler) tick(ctx context.Context) {
	now := time.Now().UTC()

	// Fetch all tenants
	tenantIter := s.client.Collection("tenants").Documents(ctx)
	defer tenantIter.Stop()

	for {
		tenantDoc, err := tenantIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Printf("[MacroScheduler] error iterating tenants: %v", err)
			return
		}
		tenantID := tenantDoc.Ref.ID
		s.processScheduledRules(ctx, tenantID, now)
	}
}

func (s *MacroScheduler) processScheduledRules(ctx context.Context, tenantID string, now time.Time) {
	iter := s.client.Collection("tenants").Doc(tenantID).Collection("automation_rules").
		Where("enabled", "==", true).
		Documents(ctx)
	defer iter.Stop()

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		var rule models.AutomationRule
		if err := doc.DataTo(&rule); err != nil {
			continue
		}
		if rule.Schedule == nil {
			continue
		}
		if s.isDue(rule.Schedule, rule.LastRunAt, now) {
			s.executeRule(ctx, tenantID, &rule, now)
		}
	}
}

// isDue reports whether a scheduled rule should fire at the given time.
func (s *MacroScheduler) isDue(sched *models.MacroSchedule, lastRunAt string, now time.Time) bool {
	switch sched.Type {
	case "one_time":
		if sched.RunAt == nil {
			return false
		}
		if now.Before(*sched.RunAt) {
			return false
		}
		// Only fire if not already run
		return lastRunAt == ""

	case "interval":
		if sched.IntervalMinutes <= 0 {
			return false
		}
		if lastRunAt == "" {
			return true
		}
		last, err := time.Parse(time.RFC3339, lastRunAt)
		if err != nil {
			return true
		}
		return now.After(last.Add(time.Duration(sched.IntervalMinutes) * time.Minute))

	case "daily":
		return s.timeOfDayDue(sched.TimeOfDay, lastRunAt, now, func(t time.Time) bool {
			return true
		})

	case "weekly":
		dow := time.Weekday(sched.DayOfWeek)
		return s.timeOfDayDue(sched.TimeOfDay, lastRunAt, now, func(t time.Time) bool {
			return t.Weekday() == dow
		})

	case "monthly":
		dom := sched.DayOfMonth
		return s.timeOfDayDue(sched.TimeOfDay, lastRunAt, now, func(t time.Time) bool {
			return t.Day() == dom
		})
	}
	return false
}

func (s *MacroScheduler) timeOfDayDue(timeOfDay string, lastRunAt string, now time.Time, dateFn func(time.Time) bool) bool {
	if !dateFn(now) {
		return false
	}
	hh, mm := parseHHMM(timeOfDay)
	scheduledToday := time.Date(now.Year(), now.Month(), now.Day(), hh, mm, 0, 0, time.UTC)
	if now.Before(scheduledToday) {
		return false
	}
	if lastRunAt == "" {
		return true
	}
	last, err := time.Parse(time.RFC3339, lastRunAt)
	if err != nil {
		return true
	}
	// Fire if we haven't run since the scheduled time today
	return last.Before(scheduledToday)
}

func parseHHMM(s string) (int, int) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 9, 0
	}
	hh, _ := strconv.Atoi(parts[0])
	mm, _ := strconv.Atoi(parts[1])
	return hh, mm
}

func (s *MacroScheduler) executeRule(ctx context.Context, tenantID string, rule *models.AutomationRule, now time.Time) {
	log.Printf("[MacroScheduler] executing rule %s (%s) for tenant %s", rule.RuleID, rule.Name, tenantID)

	var execErr error

	// Dispatch to the appropriate macro executor based on MacroType
	switch rule.MacroType {
	case "low_stock_notification":
		execErr = s.runLowStockMacro(ctx, tenantID, rule)
	case "export_shipping_labels":
		execErr = s.runExportLabelsMacro(ctx, tenantID, rule)
	case "import_tracking":
		execErr = s.runImportTrackingMacro(ctx, tenantID, rule)
	default:
		// Generic macro: run as order rule against null order context
		log.Printf("[MacroScheduler] no specific executor for macro_type=%s, skipping", rule.MacroType)
	}

	nowStr := now.Format(time.RFC3339)
	ok := execErr == nil
	if execErr != nil {
		log.Printf("[MacroScheduler] rule %s failed: %v", rule.RuleID, execErr)
	}

	// Update last run stats
	s.client.Collection("tenants").Doc(tenantID).Collection("automation_rules").Doc(rule.RuleID).Update(ctx, []firestore.Update{
		{Path: "last_run_at", Value: nowStr},
		{Path: "last_run_ok", Value: ok},
		{Path: "run_count", Value: firestore.Increment(1)},
	})
}

// ── Macro-specific executors ─────────────────────────────────────────────────

func (s *MacroScheduler) runLowStockMacro(ctx context.Context, tenantID string, rule *models.AutomationRule) error {
	svc := NewLowStockMacroService(s.client)
	cfg, err := buildLowStockConfig(rule)
	if err != nil {
		return fmt.Errorf("build low stock config: %w", err)
	}
	return svc.Run(ctx, tenantID, cfg)
}

func buildLowStockConfig(rule *models.AutomationRule) (LowStockMacroConfig, error) {
	params := rule.Parameters
	if len(rule.Configurations) > 0 {
		for _, rc := range rule.Configurations {
			if rc.Enabled {
				params = rc.Params
				break
			}
		}
	}
	if params == nil {
		return LowStockMacroConfig{}, fmt.Errorf("no parameters configured")
	}
	cfg := LowStockMacroConfig{}
	cfg.AllLocations, _ = params["all_locations"].(bool)
	cfg.LocationName, _ = params["location_name"].(string)
	cfg.EmailHost, _ = params["email_host"].(string)
	cfg.EmailUser, _ = params["email_user"].(string)
	cfg.EmailPassword, _ = params["email_password"].(string)
	cfg.EmailTo, _ = params["email_to"].(string)
	if port, ok := params["email_port"].(int64); ok {
		cfg.EmailPort = int(port)
	} else if port, ok := params["email_port"].(float64); ok {
		cfg.EmailPort = int(port)
	}
	return cfg, nil
}

func (s *MacroScheduler) runExportLabelsMacro(ctx context.Context, tenantID string, rule *models.AutomationRule) error {
	svc := NewShippingLabelExportService(s.client, s.templateSvc)
	cfg, err := buildExportLabelsConfig(rule)
	if err != nil {
		return fmt.Errorf("build export labels config: %w", err)
	}
	return svc.Run(ctx, tenantID, cfg)
}

func buildExportLabelsConfig(rule *models.AutomationRule) (ShippingLabelExportConfig, error) {
	params := rule.Parameters
	if len(rule.Configurations) > 0 {
		for _, rc := range rule.Configurations {
			if rc.Enabled {
				params = rc.Params
				break
			}
		}
	}
	if params == nil {
		return ShippingLabelExportConfig{}, fmt.Errorf("no parameters configured")
	}
	cfg := ShippingLabelExportConfig{}
	cfg.DropboxAccessToken, _ = params["dropbox_access_token"].(string)
	cfg.FolderPath, _ = params["folder_path"].(string)
	cfg.Identifier, _ = params["identifier"].(string)
	cfg.Location, _ = params["location"].(string)
	cfg.IndividualFiles, _ = params["individual_files"].(bool)
	if bs, ok := params["batch_size"].(int64); ok {
		cfg.BatchSize = int(bs)
	} else if bs, ok := params["batch_size"].(float64); ok {
		cfg.BatchSize = int(bs)
	}
	return cfg, nil
}

func (s *MacroScheduler) runImportTrackingMacro(ctx context.Context, tenantID string, rule *models.AutomationRule) error {
	params := rule.Parameters
	carrier, _ := params["carrier"].(string)
	location, _ := params["location"].(string)
	autoProcess, _ := params["auto_process"].(bool)
	svc := NewTrackingMacroService(s.client)
	return svc.Run(ctx, tenantID, carrier, location, autoProcess)
}
