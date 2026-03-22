package handlers

// ============================================================================
// TEMU SCHEMA AUTO-REFRESH SCHEDULER — SESSION F (USP-04)
// ============================================================================
// Periodically re-runs the Temu template sync for every tenant that has
// auto-refresh enabled, on a configurable interval.
//
// Settings stored per-tenant in Firestore:
//   tenants/{tenantID}/settings/temu_schema_refresh
//     enabled:         bool   (default false)
//     interval_days:   int    (default 7, range 1–90)
//     last_run_at:     time.Time
//
// Follows the same pattern as SchemaRefreshScheduler (schema_scheduler.go)
// established in ENH-02, and EbaySchemaRefreshScheduler (USP-04 eBay).
// ============================================================================

import (
	"context"
	"fmt"
	"log"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
)

// TemuSchemaRefreshSettings mirrors the Firestore settings document shape.
type TemuSchemaRefreshSettings struct {
	Enabled      bool      `firestore:"enabled"       json:"enabled"`
	IntervalDays int       `firestore:"interval_days" json:"interval_days"`
	LastRunAt    time.Time `firestore:"last_run_at"   json:"last_run_at"`
}

// ── TemuSchemaRefreshScheduler ────────────────────────────────────────────────

type TemuSchemaRefreshScheduler struct {
	fsClient      *firestore.Client
	schemaHandler *TemuSchemaHandler
}

func NewTemuSchemaRefreshScheduler(
	fsClient *firestore.Client,
	schemaHandler *TemuSchemaHandler,
) *TemuSchemaRefreshScheduler {
	return &TemuSchemaRefreshScheduler{
		fsClient:      fsClient,
		schemaHandler: schemaHandler,
	}
}

// Run starts the background goroutine. 3-minute warm-up then hourly ticks.
func (s *TemuSchemaRefreshScheduler) Run() {
	go func() {
		time.Sleep(3 * time.Minute)
		ctx := context.Background()
		s.tick(ctx)

		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			s.tick(ctx)
		}
	}()
}

func (s *TemuSchemaRefreshScheduler) tick(ctx context.Context) {
	tenantIDs, err := s.listTenantIDs(ctx)
	if err != nil {
		log.Printf("[TemuSchemaScheduler] failed to list tenants: %v", err)
		return
	}

	for _, tenantID := range tenantIDs {
		settings, err := s.GetSettings(ctx, tenantID)
		if err != nil {
			log.Printf("[TemuSchemaScheduler] tenant=%s: failed to read settings: %v", tenantID, err)
			continue
		}
		if !settings.Enabled {
			continue
		}

		intervalDays := settings.IntervalDays
		if intervalDays <= 0 {
			intervalDays = 7
		}
		nextRun := settings.LastRunAt.Add(time.Duration(intervalDays) * 24 * time.Hour)
		if time.Now().Before(nextRun) {
			continue
		}

		log.Printf("[TemuSchemaScheduler] tenant=%s: triggering scheduled Temu schema refresh", tenantID)
		if err := s.runRefresh(ctx, tenantID); err != nil {
			log.Printf("[TemuSchemaScheduler] tenant=%s: refresh failed: %v", tenantID, err)
		}
	}
}

func (s *TemuSchemaRefreshScheduler) runRefresh(ctx context.Context, tenantID string) error {
	// Find the first active Temu credential for this tenant.
	creds, err := s.schemaHandler.repo.ListCredentials(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("list credentials: %w", err)
	}
	var credentialID string
	for _, c := range creds {
		if c.Channel == "temu" && c.Active {
			credentialID = c.CredentialID
			break
		}
	}
	if credentialID == "" {
		return fmt.Errorf("no active Temu credential found for tenant %s", tenantID)
	}

	client, err := s.schemaHandler.buildClient(ctx, tenantID, credentialID)
	if err != nil {
		return fmt.Errorf("build Temu client: %w", err)
	}

	jobID := temuGenerateJobID()
	now := time.Now()
	jobData := map[string]interface{}{
		"jobId":          jobID,
		"status":         "running",
		"fullSync":       false,
		"startedAt":      now,
		"updatedAt":      now,
		"downloaded":     0,
		"skipped":        0,
		"failed":         0,
		"total":          0,
		"leafFound":      0,
		"treeWalkDone":   false,
		"lastCatId":      0,
		"currentCatName": "",
		"errors":         []string{},
		"triggeredBy":    "scheduler",
	}
	if _, err := s.schemaHandler.jobsCol().Doc(jobID).Set(ctx, jobData); err != nil {
		return fmt.Errorf("create job doc: %w", err)
	}

	jobCtx, cancel := context.WithCancel(context.Background())
	s.schemaHandler.activeJobsMu.Lock()
	s.schemaHandler.activeJobs[jobID] = cancel
	s.schemaHandler.activeJobsMu.Unlock()

	go s.schemaHandler.runSync(jobCtx, jobID, client, tenantID, credentialID, false)

	log.Printf("[TemuSchemaScheduler] tenant=%s: started job %s", tenantID, jobID)
	return s.updateLastRunAt(ctx, tenantID)
}

// ── Settings GET / SET ────────────────────────────────────────────────────────

func (s *TemuSchemaRefreshScheduler) settingsDoc(tenantID string) *firestore.DocumentRef {
	return s.fsClient.Collection("tenants").Doc(tenantID).Collection("settings").Doc("temu_schema_refresh")
}

// GetSettings returns the refresh settings for a tenant (with safe defaults).
func (s *TemuSchemaRefreshScheduler) GetSettings(ctx context.Context, tenantID string) (TemuSchemaRefreshSettings, error) {
	doc, err := s.settingsDoc(tenantID).Get(ctx)
	if err != nil {
		return TemuSchemaRefreshSettings{
			Enabled:      false,
			IntervalDays: 7,
		}, nil
	}
	var cfg TemuSchemaRefreshSettings
	if err := doc.DataTo(&cfg); err != nil {
		return TemuSchemaRefreshSettings{}, err
	}
	if cfg.IntervalDays <= 0 {
		cfg.IntervalDays = 7
	}
	return cfg, nil
}

// SaveSettings persists the refresh settings for a tenant.
func (s *TemuSchemaRefreshScheduler) SaveSettings(ctx context.Context, tenantID string, settings TemuSchemaRefreshSettings) error {
	if settings.IntervalDays <= 0 {
		settings.IntervalDays = 7
	}
	if settings.IntervalDays > 90 {
		settings.IntervalDays = 90
	}
	_, err := s.settingsDoc(tenantID).Set(ctx, settings)
	return err
}

func (s *TemuSchemaRefreshScheduler) updateLastRunAt(ctx context.Context, tenantID string) error {
	_, err := s.settingsDoc(tenantID).Set(ctx, map[string]interface{}{
		"last_run_at": time.Now(),
	}, firestore.MergeAll)
	return err
}

func (s *TemuSchemaRefreshScheduler) listTenantIDs(ctx context.Context) ([]string, error) {
	iter := s.fsClient.Collection("tenants").Documents(ctx)
	defer iter.Stop()
	var ids []string
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		ids = append(ids, doc.Ref.ID)
	}
	return ids, nil
}
