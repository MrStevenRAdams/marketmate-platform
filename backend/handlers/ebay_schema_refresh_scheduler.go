package handlers

// ============================================================================
// EBAY SCHEMA AUTO-REFRESH SCHEDULER
// ============================================================================
// The eBay schema cache is GLOBAL — the same category tree and aspect
// definitions apply to all tenants. This scheduler runs once globally using
// any active eBay credential for API auth only.
//
// Settings stored in a single global document:
//   marketplaces/eBay/config/schema_refresh
//     enabled:         bool   (default false)
//     interval_days:   int    (default 7, range 1–90)
//     marketplace_id:  string (default "EBAY_GB")
//     last_run_at:     time.Time
// ============================================================================

import (
	"context"
	"fmt"
	"log"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
)

// EbaySchemaRefreshSettings mirrors the Firestore settings document shape.
type EbaySchemaRefreshSettings struct {
	Enabled       bool      `firestore:"enabled"        json:"enabled"`
	IntervalDays  int       `firestore:"interval_days"  json:"interval_days"`
	MarketplaceID string    `firestore:"marketplace_id" json:"marketplace_id"`
	LastRunAt     time.Time `firestore:"last_run_at"    json:"last_run_at"`
}

type EbaySchemaRefreshScheduler struct {
	fsClient      *firestore.Client
	schemaHandler *EbaySchemaHandler
}

func NewEbaySchemaRefreshScheduler(
	fsClient *firestore.Client,
	schemaHandler *EbaySchemaHandler,
) *EbaySchemaRefreshScheduler {
	return &EbaySchemaRefreshScheduler{
		fsClient:      fsClient,
		schemaHandler: schemaHandler,
	}
}

func (s *EbaySchemaRefreshScheduler) Run() {
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

func (s *EbaySchemaRefreshScheduler) globalSettingsDoc() *firestore.DocumentRef {
	return s.fsClient.Collection("marketplaces").Doc("eBay").
		Collection("config").Doc("schema_refresh")
}

func (s *EbaySchemaRefreshScheduler) tick(ctx context.Context) {
	settings, err := s.GetSettings(ctx, "")
	if err != nil {
		log.Printf("[EbaySchemaScheduler] failed to read global settings: %v", err)
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
	log.Printf("[EbaySchemaScheduler] Triggering scheduled global eBay schema refresh")
	if err := s.runRefresh(ctx, settings); err != nil {
		log.Printf("[EbaySchemaScheduler] refresh failed: %v", err)
	}
}

func (s *EbaySchemaRefreshScheduler) runRefresh(ctx context.Context, settings EbaySchemaRefreshSettings) error {
	mpID := settings.MarketplaceID
	if mpID == "" {
		mpID = "EBAY_GB"
	}

	// Find any active eBay credential across all tenants
	tenantID, credentialID, err := s.findAnyEbayCredential(ctx)
	if err != nil {
		return err
	}
	if credentialID == "" {
		log.Printf("[EbaySchemaScheduler] No active eBay credential found — skipping")
		return nil
	}

	client, resolvedMpID, err := s.schemaHandler.buildClient(ctx, tenantID, credentialID)
	if err != nil {
		return fmt.Errorf("build eBay client: %w", err)
	}
	if mpID == "" {
		mpID = resolvedMpID
	}

	jobID := ebayGenerateJobID()
	now := time.Now()
	jobData := map[string]interface{}{
		"jobId":         jobID,
		"status":        "running",
		"marketplaceId": mpID,
		"fullSync":      false,
		"startedAt":     now,
		"updatedAt":     now,
		"downloaded":    0,
		"skipped":       0,
		"failed":        0,
		"total":         0,
		"errors":        []string{},
		"triggeredBy":   "scheduler",
	}
	if _, err := s.schemaHandler.jobsCol().Doc(jobID).Set(ctx, jobData); err != nil {
		return fmt.Errorf("create job doc: %w", err)
	}

	jobCtx, cancel := context.WithCancel(context.Background())
	s.schemaHandler.activeJobsMu.Lock()
	s.schemaHandler.activeJobs[jobID] = cancel
	s.schemaHandler.activeJobsMu.Unlock()

	go s.schemaHandler.runSync(jobCtx, jobID, client, mpID, tenantID, credentialID, false)

	log.Printf("[EbaySchemaScheduler] Started job %s using tenant=%s credential=%s (marketplace=%s)", jobID, tenantID, credentialID, mpID)
	return s.updateLastRunAt(ctx)
}

func (s *EbaySchemaRefreshScheduler) findAnyEbayCredential(ctx context.Context) (string, string, error) {
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
			if c.Channel == "ebay" && c.Active {
				return tid, c.CredentialID, nil
			}
		}
	}
	return "", "", nil
}

// ── Settings GET / SET ────────────────────────────────────────────────────────
// tenantID param kept for API handler compatibility — ignored internally.

func (s *EbaySchemaRefreshScheduler) settingsDoc(_ string) *firestore.DocumentRef {
	return s.globalSettingsDoc()
}

func (s *EbaySchemaRefreshScheduler) GetSettings(ctx context.Context, _ string) (EbaySchemaRefreshSettings, error) {
	doc, err := s.globalSettingsDoc().Get(ctx)
	if err != nil {
		return EbaySchemaRefreshSettings{Enabled: false, IntervalDays: 7, MarketplaceID: "EBAY_GB"}, nil
	}
	var cfg EbaySchemaRefreshSettings
	if err := doc.DataTo(&cfg); err != nil {
		return EbaySchemaRefreshSettings{}, err
	}
	if cfg.IntervalDays <= 0 {
		cfg.IntervalDays = 7
	}
	return cfg, nil
}

func (s *EbaySchemaRefreshScheduler) SaveSettings(ctx context.Context, _ string, settings EbaySchemaRefreshSettings) error {
	if settings.IntervalDays <= 0 {
		settings.IntervalDays = 7
	}
	if settings.IntervalDays > 90 {
		settings.IntervalDays = 90
	}
	_, err := s.globalSettingsDoc().Set(ctx, settings)
	return err
}

func (s *EbaySchemaRefreshScheduler) updateLastRunAt(ctx context.Context) error {
	_, err := s.globalSettingsDoc().Set(ctx, map[string]interface{}{
		"last_run_at": time.Now(),
	}, firestore.MergeAll)
	return err
}
