package handlers

// ============================================================================
// SCHEMA AUTO-REFRESH SCHEDULER — ENH-02
// ============================================================================
// Periodically re-runs the Amazon schema download-all job for every tenant
// that has an active Amazon credential, on a configurable interval.
//
// Settings are stored per-tenant in Firestore:
//   tenants/{tenantID}/settings/schema_refresh
//     enabled:         bool   (default false)
//     interval_days:   int    (default 7, range 1–90)
//     marketplace_id:  string (default "A1F83G8C2ARO7P")
//     last_run_at:     time.Time
//
// The scheduler ticks every hour and triggers a job for any tenant whose
// (last_run_at + interval_days) is in the past, or has never run.
// ============================================================================

import (
	"context"
	"fmt"
	"log"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
)

// SchemaRefreshSettings represents a tenant's auto-refresh preferences.
type SchemaRefreshSettings struct {
	Enabled       bool      `firestore:"enabled"        json:"enabled"`
	IntervalDays  int       `firestore:"interval_days"  json:"interval_days"`
	MarketplaceID string    `firestore:"marketplace_id" json:"marketplace_id"`
	LastRunAt     time.Time `firestore:"last_run_at"    json:"last_run_at"`
}

// ── SchemaRefreshScheduler ────────────────────────────────────────────────────

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

// Run starts the background goroutine. It fires immediately after a short
// warm-up delay, then ticks every hour.
func (s *SchemaRefreshScheduler) Run() {
	go func() {
		// Allow service warm-up before first check
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

// tick iterates all tenants and fires a refresh job for those that are due.
func (s *SchemaRefreshScheduler) tick(ctx context.Context) {
	tenantIDs, err := s.listTenantIDs(ctx)
	if err != nil {
		log.Printf("[SchemaScheduler] failed to list tenants: %v", err)
		return
	}

	for _, tenantID := range tenantIDs {
		settings, err := s.getSettings(ctx, tenantID)
		if err != nil {
			log.Printf("[SchemaScheduler] tenant=%s: failed to read settings: %v", tenantID, err)
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
			continue // not due yet
		}

		log.Printf("[SchemaScheduler] tenant=%s: triggering scheduled schema refresh", tenantID)
		if err := s.runRefresh(ctx, tenantID, settings); err != nil {
			log.Printf("[SchemaScheduler] tenant=%s: refresh failed: %v", tenantID, err)
		}
	}
}

// runRefresh triggers a schema download-all job for a single tenant.
func (s *SchemaRefreshScheduler) runRefresh(ctx context.Context, tenantID string, settings SchemaRefreshSettings) error {
	mpID := settings.MarketplaceID
	if mpID == "" {
		mpID = "A1F83G8C2ARO7P"
	}

	// Find the first active Amazon credential for this tenant
	creds, err := s.schemaHandler.repo.ListCredentials(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("list credentials: %w", err)
	}
	var credentialID string
	for _, c := range creds {
		if c.Channel == "amazon" && c.Active {
			credentialID = c.CredentialID
			break
		}
	}
	if credentialID == "" {
		return fmt.Errorf("no active amazon credential found for tenant %s", tenantID)
	}

	// Build an SP-API client without a gin.Context
	client, resolvedMpID, err := s.schemaHandler.buildClient(ctx, tenantID, credentialID)
	if err != nil {
		return fmt.Errorf("build client: %w", err)
	}
	if mpID == "" {
		mpID = resolvedMpID
	}

	// Search for all product types
	result, err := client.SearchProductTypes(ctx, "", "")
	if err != nil {
		return fmt.Errorf("search product types: %w", err)
	}
	productTypes := make([]string, 0, len(result.ProductTypes))
	for _, pt := range result.ProductTypes {
		productTypes = append(productTypes, pt.Name)
	}
	if len(productTypes) == 0 {
		log.Printf("[SchemaScheduler] tenant=%s: no product types found, skipping", tenantID)
		return s.updateLastRunAt(ctx, tenantID)
	}

	// Create and start the download job
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

	// Fire and forget — same as the HTTP-triggered path
	go s.schemaHandler.downloadAll(jobCtx, jobID, client, productTypes, mpID, tenantID, credentialID)

	log.Printf("[SchemaScheduler] tenant=%s: started job %s (%d product types)", tenantID, jobID, len(productTypes))

	// Update last_run_at so this tenant is not immediately re-triggered
	return s.updateLastRunAt(ctx, tenantID)
}

// ── Firestore helpers ─────────────────────────────────────────────────────────

func (s *SchemaRefreshScheduler) settingsDoc(tenantID string) *firestore.DocumentRef {
	return s.fsClient.Collection("tenants").Doc(tenantID).Collection("settings").Doc("schema_refresh")
}

// GetSettings returns the refresh settings for a tenant (with safe defaults).
func (s *SchemaRefreshScheduler) getSettings(ctx context.Context, tenantID string) (SchemaRefreshSettings, error) {
	doc, err := s.settingsDoc(tenantID).Get(ctx)
	if err != nil {
		// Document not yet created — return defaults
		return SchemaRefreshSettings{
			Enabled:       false,
			IntervalDays:  7,
			MarketplaceID: "A1F83G8C2ARO7P",
		}, nil
	}
	var s2 SchemaRefreshSettings
	if err := doc.DataTo(&s2); err != nil {
		return SchemaRefreshSettings{}, err
	}
	if s2.IntervalDays <= 0 {
		s2.IntervalDays = 7
	}
	return s2, nil
}

// SaveSettings persists refresh settings for a tenant.
func (s *SchemaRefreshScheduler) SaveSettings(ctx context.Context, tenantID string, settings SchemaRefreshSettings) error {
	if settings.IntervalDays <= 0 {
		settings.IntervalDays = 7
	}
	if settings.IntervalDays > 90 {
		settings.IntervalDays = 90
	}
	_, err := s.settingsDoc(tenantID).Set(ctx, settings)
	return err
}

func (s *SchemaRefreshScheduler) updateLastRunAt(ctx context.Context, tenantID string) error {
	_, err := s.settingsDoc(tenantID).Set(ctx, map[string]interface{}{
		"last_run_at": time.Now(),
	}, firestore.MergeAll)
	return err
}

func (s *SchemaRefreshScheduler) listTenantIDs(ctx context.Context) ([]string, error) {
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
