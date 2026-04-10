package handlers

// ============================================================================
// AMAZON SCHEMA AUTO-REFRESH SCHEDULER
// ============================================================================
// The Amazon schema cache is GLOBAL — all product types and field definitions
// are shared across all tenants. This scheduler runs once globally using any
// active Amazon credential for API auth only.
//
// Settings stored in a single global document:
//   marketplaces/Amazon/config/schema_refresh
//     enabled:         bool   (default false)
//     interval_days:   int    (default 7, range 1–90)
//     marketplace_id:  string (default "A1F83G8C2ARO7P")
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

// SchemaRefreshSettings represents the global auto-refresh preferences.
type SchemaRefreshSettings struct {
	Enabled       bool      `firestore:"enabled"        json:"enabled"`
	IntervalDays  int       `firestore:"interval_days"  json:"interval_days"`
	MarketplaceID string    `firestore:"marketplace_id" json:"marketplace_id"`
	LastRunAt     time.Time `firestore:"last_run_at"    json:"last_run_at"`
}

type SchemaRefreshScheduler struct {
	fsClient      *firestore.Client
	schemaHandler *AmazonSchemaHandler
}

func NewSchemaRefreshScheduler(
	fsClient *firestore.Client,
	schemaHandler *AmazonSchemaHandler,
) *SchemaRefreshScheduler {
	return &SchemaRefreshScheduler{
		fsClient:      fsClient,
		schemaHandler: schemaHandler,
	}
}

func (s *SchemaRefreshScheduler) Run() {
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

func (s *SchemaRefreshScheduler) globalSettingsDoc() *firestore.DocumentRef {
	return s.fsClient.Collection("marketplaces").Doc("Amazon").
		Collection("config").Doc("schema_refresh")
}

func (s *SchemaRefreshScheduler) tick(ctx context.Context) {
	settings, err := s.GetSettings(ctx, "")
	if err != nil {
		log.Printf("[SchemaScheduler] failed to read global settings: %v", err)
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
	log.Printf("[SchemaScheduler] Triggering scheduled global Amazon schema refresh")
	if err := s.runRefresh(ctx, settings); err != nil {
		log.Printf("[SchemaScheduler] refresh failed: %v", err)
	}
}

func (s *SchemaRefreshScheduler) runRefresh(ctx context.Context, settings SchemaRefreshSettings) error {
	mpID := settings.MarketplaceID
	if mpID == "" {
		mpID = "A1F83G8C2ARO7P"
	}

	// Find any active Amazon credential across all tenants
	tenantID, credentialID, err := s.findAnyAmazonCredential(ctx)
	if err != nil {
		return err
	}
	if credentialID == "" {
		log.Printf("[SchemaScheduler] No active Amazon credential found — skipping")
		return nil
	}

	client, resolvedMpID, err := s.schemaHandler.buildClient(ctx, tenantID, credentialID)
	if err != nil {
		return fmt.Errorf("build client: %w", err)
	}
	if mpID == "" {
		mpID = resolvedMpID
	}

	result, err := client.SearchProductTypes(ctx, "", "")
	if err != nil {
		return fmt.Errorf("search product types: %w", err)
	}
	productTypes := make([]string, 0, len(result.ProductTypes))
	for _, pt := range result.ProductTypes {
		productTypes = append(productTypes, pt.Name)
	}
	if len(productTypes) == 0 {
		log.Printf("[SchemaScheduler] No product types found — skipping")
		return s.updateLastRunAt(ctx)
	}

	jobID := generateJobID()
	now := time.Now()
	jobData := map[string]interface{}{
		"jobId":         jobID,
		"status":        "running",
		"marketplaceId": mpID,
		"total":         len(productTypes),
		"downloaded":    0,
		"skipped":       0,
		"failed":        0,
		"startedAt":     now,
		"updatedAt":     now,
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

	go s.schemaHandler.downloadAll(jobCtx, jobID, client, productTypes, mpID, tenantID, credentialID)

	log.Printf("[SchemaScheduler] Started job %s using tenant=%s credential=%s (%d product types)", jobID, tenantID, credentialID, len(productTypes))
	return s.updateLastRunAt(ctx)
}

func (s *SchemaRefreshScheduler) findAnyAmazonCredential(ctx context.Context) (string, string, error) {
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
			if c.Channel == "amazon" && c.Active {
				return tid, c.CredentialID, nil
			}
		}
	}
	return "", "", nil
}

// ── Settings GET / SET ────────────────────────────────────────────────────────
// tenantID param kept for API handler compatibility — ignored internally.

func (s *SchemaRefreshScheduler) settingsDoc(_ string) *firestore.DocumentRef {
	return s.globalSettingsDoc()
}

func (s *SchemaRefreshScheduler) GetSettings(ctx context.Context, _ string) (SchemaRefreshSettings, error) {
	doc, err := s.globalSettingsDoc().Get(ctx)
	if err != nil {
		return SchemaRefreshSettings{Enabled: false, IntervalDays: 7, MarketplaceID: "A1F83G8C2ARO7P"}, nil
	}
	var cfg SchemaRefreshSettings
	if err := doc.DataTo(&cfg); err != nil {
		return SchemaRefreshSettings{}, err
	}
	if cfg.IntervalDays <= 0 {
		cfg.IntervalDays = 7
	}
	return cfg, nil
}

func (s *SchemaRefreshScheduler) SaveSettings(ctx context.Context, _ string, settings SchemaRefreshSettings) error {
	if settings.IntervalDays <= 0 {
		settings.IntervalDays = 7
	}
	if settings.IntervalDays > 90 {
		settings.IntervalDays = 90
	}
	_, err := s.globalSettingsDoc().Set(ctx, settings)
	return err
}

func (s *SchemaRefreshScheduler) updateLastRunAt(ctx context.Context) error {
	_, err := s.globalSettingsDoc().Set(ctx, map[string]interface{}{
		"last_run_at": time.Now(),
	}, firestore.MergeAll)
	return err
}
