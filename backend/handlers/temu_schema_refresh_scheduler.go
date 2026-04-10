package handlers

// ============================================================================
// TEMU SCHEMA AUTO-REFRESH SCHEDULER
// ============================================================================
// The Temu schema cache is GLOBAL — the same category tree and attribute
// schemas apply to all tenants. This scheduler runs once globally, not once
// per tenant. Settings are stored in a single global document:
//
//   marketplaces/Temu/config/schema_refresh
//     enabled:         bool   (default false)
//     interval_days:   int    (default 7, range 1–90)
//     last_run_at:     time.Time
//
// The scheduler finds any active Temu credential across any tenant to use
// for the API call — the credential is only needed for authentication, not
// for tenant-specific data.
// ============================================================================

import (
	"context"
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

// globalSettingsDoc returns the single global settings document.
func (s *TemuSchemaRefreshScheduler) globalSettingsDoc() *firestore.DocumentRef {
	return s.fsClient.Collection("marketplaces").Doc("Temu").
		Collection("config").Doc("schema_refresh")
}

// tick checks the global settings and fires a sync if due.
func (s *TemuSchemaRefreshScheduler) tick(ctx context.Context) {
	settings, err := s.GetSettings(ctx, "")
	if err != nil {
		log.Printf("[TemuSchemaScheduler] failed to read global settings: %v", err)
		return
	}
	if !settings.Enabled {
		return
	}

	intervalDays := settings.IntervalDays
	if intervalDays <= 0 {
		intervalDays = 7
	}
	nextRun := settings.LastRunAt.Add(time.Duration(intervalDays) * 24 * time.Hour)
	if time.Now().Before(nextRun) {
		return
	}

	log.Printf("[TemuSchemaScheduler] Triggering scheduled global Temu schema refresh")
	if err := s.runRefresh(ctx); err != nil {
		log.Printf("[TemuSchemaScheduler] refresh failed: %v", err)
	}
}

// runRefresh finds any active Temu credential and starts a sync job.
func (s *TemuSchemaRefreshScheduler) runRefresh(ctx context.Context) error {
	tenantID, credentialID, err := s.findAnyTemuCredential(ctx)
	if err != nil {
		return err
	}
	if credentialID == "" {
		log.Printf("[TemuSchemaScheduler] No active Temu credential found — skipping")
		return nil
	}

	client, err := s.schemaHandler.buildClient(ctx, tenantID, credentialID)
	if err != nil {
		return err
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
		return err
	}

	jobCtx, cancel := context.WithCancel(context.Background())
	s.schemaHandler.activeJobsMu.Lock()
	s.schemaHandler.activeJobs[jobID] = cancel
	s.schemaHandler.activeJobsMu.Unlock()

	go s.schemaHandler.runSync(jobCtx, jobID, client, tenantID, credentialID, false)

	log.Printf("[TemuSchemaScheduler] Started job %s using tenant=%s credential=%s", jobID, tenantID, credentialID)
	return s.updateLastRunAt(ctx)
}

// findAnyTemuCredential scans all tenants for any active Temu credential.
func (s *TemuSchemaRefreshScheduler) findAnyTemuCredential(ctx context.Context) (string, string, error) {
	tenantsIter := s.fsClient.Collection("tenants").Documents(ctx)
	defer tenantsIter.Stop()

	for {
		tenantDoc, err := tenantsIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			continue
		}
		tid := tenantDoc.Ref.ID
		creds, err := s.schemaHandler.repo.ListCredentials(ctx, tid)
		if err != nil {
			continue
		}
		for _, c := range creds {
			if (c.Channel == "temu" || c.Channel == "temu_sandbox") && c.Active {
				return tid, c.CredentialID, nil
			}
		}
	}
	return "", "", nil
}

// ── Settings GET / SET ────────────────────────────────────────────────────────

func (s *TemuSchemaRefreshScheduler) settingsDoc(_ string) *firestore.DocumentRef {
	return s.globalSettingsDoc()
}

func (s *TemuSchemaRefreshScheduler) GetSettings(ctx context.Context, _ string) (TemuSchemaRefreshSettings, error) {
	doc, err := s.globalSettingsDoc().Get(ctx)
	if err != nil {
		return TemuSchemaRefreshSettings{Enabled: false, IntervalDays: 7}, nil
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

func (s *TemuSchemaRefreshScheduler) SaveSettings(ctx context.Context, _ string, settings TemuSchemaRefreshSettings) error {
	if settings.IntervalDays <= 0 {
		settings.IntervalDays = 7
	}
	if settings.IntervalDays > 90 {
		settings.IntervalDays = 90
	}
	_, err := s.globalSettingsDoc().Set(ctx, settings)
	return err
}

func (s *TemuSchemaRefreshScheduler) updateLastRunAt(ctx context.Context) error {
	_, err := s.globalSettingsDoc().Set(ctx, map[string]interface{}{
		"last_run_at": time.Now(),
	}, firestore.MergeAll)
	return err
}
