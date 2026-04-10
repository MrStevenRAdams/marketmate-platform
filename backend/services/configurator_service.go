package services

import (
	"context"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"

	"module-a/models"
)

// ============================================================================
// CONFIGURATOR SERVICE — SESSION 1 (CFG-01, CFG-02, CFG-03)
// ============================================================================
// Handles all Configurator business logic and Firestore persistence.
//
// Firestore paths used:
//   tenants/{tenantID}/configurators/{configuratorID}
//   tenants/{tenantID}/configurator_listings/{configuratorID}_{listingID}
//   tenants/{tenantID}/listings/{listingID}          (read/write — existing)
//   tenants/{tenantID}/revise_jobs/{jobID}
// ============================================================================

type ConfiguratorService struct {
	client *firestore.Client
}

func NewConfiguratorService(client *firestore.Client) *ConfiguratorService {
	return &ConfiguratorService{client: client}
}

// ── Firestore path helpers ─────────────────────────────────────────────────

func (s *ConfiguratorService) configuratorsCol(tenantID string) *firestore.CollectionRef {
	return s.client.Collection("tenants").Doc(tenantID).Collection("configurators")
}

func (s *ConfiguratorService) configuratorListingsCol(tenantID string) *firestore.CollectionRef {
	return s.client.Collection("tenants").Doc(tenantID).Collection("configurator_listings")
}

func (s *ConfiguratorService) listingsCol(tenantID string) *firestore.CollectionRef {
	return s.client.Collection("tenants").Doc(tenantID).Collection("listings")
}

func (s *ConfiguratorService) reviseJobsCol(tenantID string) *firestore.CollectionRef {
	return s.client.Collection("tenants").Doc(tenantID).Collection("revise_jobs")
}

func joinDocID(configuratorID, listingID string) string {
	return configuratorID + "_" + listingID
}

// ============================================================================
// CREATE
// ============================================================================

func (s *ConfiguratorService) CreateConfigurator(ctx context.Context, tenantID string, cfg *models.Configurator) error {
	if cfg.Name == "" {
		return fmt.Errorf("configurator name is required")
	}
	if cfg.Channel == "" {
		return fmt.Errorf("channel is required")
	}

	cfg.TenantID = tenantID
	cfg.ConfiguratorID = "cfg_" + uuid.New().String()[:12]
	cfg.CreatedAt = time.Now()
	cfg.UpdatedAt = time.Now()

	_, err := s.configuratorsCol(tenantID).Doc(cfg.ConfiguratorID).Set(ctx, cfg)
	return err
}

// ============================================================================
// GET
// ============================================================================

func (s *ConfiguratorService) GetConfigurator(ctx context.Context, tenantID, configuratorID string) (*models.Configurator, error) {
	doc, err := s.configuratorsCol(tenantID).Doc(configuratorID).Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("configurator not found: %w", err)
	}
	var cfg models.Configurator
	if err := doc.DataTo(&cfg); err != nil {
		return nil, fmt.Errorf("parse configurator: %w", err)
	}
	return &cfg, nil
}

// ============================================================================
// GET DETAIL (with linked listings)
// ============================================================================

func (s *ConfiguratorService) GetConfiguratorDetail(ctx context.Context, tenantID, configuratorID string) (*models.ConfiguratorDetail, error) {
	cfg, err := s.GetConfigurator(ctx, tenantID, configuratorID)
	if err != nil {
		return nil, err
	}

	linkedListings, err := s.getLinkedListingDocs(ctx, tenantID, configuratorID)
	if err != nil {
		// Non-fatal — return configurator with empty listings
		linkedListings = []map[string]any{}
	}

	return &models.ConfiguratorDetail{
		Configurator:   *cfg,
		LinkedListings: linkedListings,
	}, nil
}

// ============================================================================
// LIST (with stats)
// ============================================================================

func (s *ConfiguratorService) ListConfigurators(ctx context.Context, tenantID string, channelFilter string) ([]models.ConfiguratorWithStats, error) {
	q := s.configuratorsCol(tenantID).OrderBy("updated_at", firestore.Desc)

	iter := q.Documents(ctx)
	defer iter.Stop()

	var results []models.ConfiguratorWithStats
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("list configurators: %w", err)
		}

		var cfg models.Configurator
		if err := doc.DataTo(&cfg); err != nil {
			continue
		}

		if channelFilter != "" && cfg.Channel != channelFilter {
			continue
		}

		// Compute stats by reading join collection
		stats := s.computeStats(ctx, tenantID, cfg.ConfiguratorID)

		results = append(results, models.ConfiguratorWithStats{
			Configurator:   cfg,
			ListingCount:   stats.listingCount,
			ErrorCount:     stats.errorCount,
			InProcessCount: stats.inProcessCount,
		})
	}

	if results == nil {
		results = []models.ConfiguratorWithStats{}
	}
	return results, nil
}

type configuratorStats struct {
	listingCount   int
	errorCount     int
	inProcessCount int
}

func (s *ConfiguratorService) computeStats(ctx context.Context, tenantID, configuratorID string) configuratorStats {
	var stats configuratorStats

	joinIter := s.configuratorListingsCol(tenantID).
		Where("configurator_id", "==", configuratorID).
		Documents(ctx)
	defer joinIter.Stop()

	var listingIDs []string
	for {
		doc, err := joinIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		var join models.ConfiguratorListing
		if err := doc.DataTo(&join); err != nil {
			continue
		}
		listingIDs = append(listingIDs, join.ListingID)
	}

	stats.listingCount = len(listingIDs)

	// Batch-fetch listings to compute error and in-process counts
	for _, lid := range listingIDs {
		doc, err := s.listingsCol(tenantID).Doc(lid).Get(ctx)
		if err != nil {
			continue
		}
		var listing map[string]any
		if err := doc.DataTo(&listing); err != nil {
			continue
		}
		state, _ := listing["state"].(string)
		if state == "error" {
			stats.errorCount++
		}
		if state == "pending" || state == "processing" {
			stats.inProcessCount++
		}
	}

	return stats
}

// ============================================================================
// UPDATE
// ============================================================================

func (s *ConfiguratorService) UpdateConfigurator(ctx context.Context, tenantID, configuratorID string, cfg *models.Configurator) error {
	if cfg.Name == "" {
		return fmt.Errorf("configurator name is required")
	}

	// Preserve immutable fields
	existing, err := s.GetConfigurator(ctx, tenantID, configuratorID)
	if err != nil {
		return err
	}

	cfg.ConfiguratorID = configuratorID
	cfg.TenantID = tenantID
	cfg.CreatedAt = existing.CreatedAt
	cfg.UpdatedAt = time.Now()

	_, err = s.configuratorsCol(tenantID).Doc(configuratorID).Set(ctx, cfg)
	return err
}

// ============================================================================
// DELETE
// ============================================================================

func (s *ConfiguratorService) DeleteConfigurator(ctx context.Context, tenantID, configuratorID string, force bool) error {
	// Check linked listing count
	joinIter := s.configuratorListingsCol(tenantID).
		Where("configurator_id", "==", configuratorID).
		Documents(ctx)
	defer joinIter.Stop()

	var joinDocs []*firestore.DocumentSnapshot
	for {
		doc, err := joinIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return fmt.Errorf("check linked listings: %w", err)
		}
		joinDocs = append(joinDocs, doc)
	}

	if len(joinDocs) > 0 && !force {
		return fmt.Errorf("configurator has %d linked listings; use force=true to delete anyway", len(joinDocs))
	}

	// Delete all join documents
	for _, doc := range joinDocs {
		if _, err := doc.Ref.Delete(ctx); err != nil {
			return fmt.Errorf("delete join doc: %w", err)
		}
	}

	// Delete the configurator document
	_, err := s.configuratorsCol(tenantID).Doc(configuratorID).Delete(ctx)
	return err
}

// ============================================================================
// DUPLICATE
// ============================================================================

func (s *ConfiguratorService) DuplicateConfigurator(ctx context.Context, tenantID, configuratorID string) (*models.Configurator, error) {
	original, err := s.GetConfigurator(ctx, tenantID, configuratorID)
	if err != nil {
		return nil, err
	}

	copy := *original
	copy.Name = "Copy of " + original.Name
	copy.ConfiguratorID = "" // will be generated by Create

	if err := s.CreateConfigurator(ctx, tenantID, &copy); err != nil {
		return nil, fmt.Errorf("duplicate: %w", err)
	}
	return &copy, nil
}

// ============================================================================
// ASSIGN LISTINGS
// ============================================================================

func (s *ConfiguratorService) AssignListings(ctx context.Context, tenantID, configuratorID string, listingIDs []string) error {
	// Verify configurator exists
	if _, err := s.GetConfigurator(ctx, tenantID, configuratorID); err != nil {
		return err
	}

	for _, listingID := range listingIDs {
		join := models.ConfiguratorListing{
			ConfiguratorID: configuratorID,
			ListingID:      listingID,
			CreatedAt:      time.Now(),
		}
		docID := joinDocID(configuratorID, listingID)
		if _, err := s.configuratorListingsCol(tenantID).Doc(docID).Set(ctx, join); err != nil {
			return fmt.Errorf("assign listing %s: %w", listingID, err)
		}
	}
	return nil
}

// ============================================================================
// REMOVE LISTINGS
// ============================================================================

func (s *ConfiguratorService) RemoveListings(ctx context.Context, tenantID, configuratorID string, listingIDs []string) error {
	for _, listingID := range listingIDs {
		docID := joinDocID(configuratorID, listingID)
		if _, err := s.configuratorListingsCol(tenantID).Doc(docID).Delete(ctx); err != nil {
			return fmt.Errorf("remove listing %s: %w", listingID, err)
		}
	}
	return nil
}

// ============================================================================
// AUTO-SELECT
// ============================================================================

func (s *ConfiguratorService) AutoSelect(ctx context.Context, tenantID, channel, categoryID string) (*string, error) {
	iter := s.configuratorsCol(tenantID).
		Where("channel", "==", channel).
		Documents(ctx)
	defer iter.Stop()

	var firstMatch *string
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("auto-select: %w", err)
		}

		var cfg models.Configurator
		if err := doc.DataTo(&cfg); err != nil {
			continue
		}

		// If a category filter is requested and this one matches, return immediately
		if categoryID != "" && cfg.CategoryID == categoryID {
			id := cfg.ConfiguratorID
			return &id, nil
		}

		// Otherwise record the first channel match as fallback
		if firstMatch == nil {
			id := cfg.ConfiguratorID
			firstMatch = &id
		}
	}

	return firstMatch, nil
}

// ============================================================================
// REVISE (bulk push fields to linked listings)
// ============================================================================

func (s *ConfiguratorService) ReviseConfigurator(ctx context.Context, tenantID, configuratorID string, fields []string) (*models.ReviseJob, error) {
	cfg, err := s.GetConfigurator(ctx, tenantID, configuratorID)
	if err != nil {
		return nil, err
	}

	linkedListings, err := s.getLinkedListingDocs(ctx, tenantID, configuratorID)
	if err != nil {
		return nil, fmt.Errorf("get linked listings: %w", err)
	}

	job := &models.ReviseJob{
		JobID:          "rev_" + uuid.New().String()[:12],
		TenantID:       tenantID,
		ConfiguratorID: configuratorID,
		Fields:         fields,
		Status:         "completed",
		Total:          len(linkedListings),
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	fieldSet := make(map[string]bool, len(fields))
	for _, f := range fields {
		fieldSet[f] = true
	}

	for _, listingDoc := range linkedListings {
		listingID, _ := listingDoc["listing_id"].(string)
		if listingID == "" {
			job.Failed++
			continue
		}

		// Build the update map — only write fields requested by the caller
		updates := buildReviseUpdates(cfg, listingDoc, fieldSet)
		if len(updates) == 0 {
			job.Succeeded++
			continue
		}

		// Write back to listing document
		docRef := s.listingsCol(tenantID).Doc(listingID)
		_, err := docRef.Set(ctx, updates, firestore.MergeAll)
		if err != nil {
			job.Failed++
			job.Errors = append(job.Errors, fmt.Sprintf("%s: %v", listingID, err))
			continue
		}
		job.Succeeded++
	}

	// Persist the job for audit/history purposes
	if _, err := s.reviseJobsCol(tenantID).Doc(job.JobID).Set(ctx, job); err != nil {
		// Non-fatal — job result is returned regardless
		_ = err
	}

	return job, nil
}

// buildReviseUpdates constructs the Firestore field updates to apply to a
// listing document based on which fields the caller selected. Updates are
// written into listing.overrides to preserve channel-specific overrides
// without clobbering the underlying product data.
func buildReviseUpdates(cfg *models.Configurator, listing map[string]any, fieldSet map[string]bool) map[string]any {
	updates := map[string]any{}

	// Existing overrides (may be nil)
	existingOverrides, _ := listing["overrides"].(map[string]any)
	if existingOverrides == nil {
		existingOverrides = map[string]any{}
	}
	newOverrides := make(map[string]any)
	for k, v := range existingOverrides {
		newOverrides[k] = v
	}

	if fieldSet["category"] && cfg.CategoryID != "" {
		newOverrides["category_mapping"] = cfg.CategoryID
		updates["category_path"] = cfg.CategoryPath
	}

	if fieldSet["shipping"] && len(cfg.ShippingDefaults) > 0 {
		updates["shipping_defaults"] = cfg.ShippingDefaults
	}

	if fieldSet["attributes"] && len(cfg.AttributeDefaults) > 0 {
		attrs := map[string]any{}
		for _, ad := range cfg.AttributeDefaults {
			if ad.Source == "default_value" && ad.DefaultValue != "" {
				attrs[ad.AttributeName] = ad.DefaultValue
			}
			// "extended_property" sources require product data lookup which
			// is handled at listing creation time, not during revise.
		}
		if len(attrs) > 0 {
			existingAttrs, _ := newOverrides["attributes"].(map[string]any)
			if existingAttrs == nil {
				existingAttrs = map[string]any{}
			}
			for k, v := range attrs {
				existingAttrs[k] = v
			}
			newOverrides["attributes"] = existingAttrs
		}
	}

	if len(newOverrides) > 0 {
		updates["overrides"] = newOverrides
	}

	if len(updates) > 0 {
		updates["updated_at"] = time.Now()
	}

	return updates
}

// ============================================================================
// INTERNAL HELPERS
// ============================================================================

// getLinkedListingDocs returns the full listing documents for all listings
// linked to a given configurator.
func (s *ConfiguratorService) getLinkedListingDocs(ctx context.Context, tenantID, configuratorID string) ([]map[string]any, error) {
	joinIter := s.configuratorListingsCol(tenantID).
		Where("configurator_id", "==", configuratorID).
		Documents(ctx)
	defer joinIter.Stop()

	var listingIDs []string
	for {
		doc, err := joinIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("list join docs: %w", err)
		}
		var join models.ConfiguratorListing
		if err := doc.DataTo(&join); err != nil {
			continue
		}
		listingIDs = append(listingIDs, join.ListingID)
	}

	var listings []map[string]any
	for _, lid := range listingIDs {
		doc, err := s.listingsCol(tenantID).Doc(lid).Get(ctx)
		if err != nil {
			// Listing may have been deleted; include a stub so caller can see it
			listings = append(listings, map[string]any{
				"listing_id": lid,
				"state":      "missing",
			})
			continue
		}
		var listing map[string]any
		if err := doc.DataTo(&listing); err != nil {
			continue
		}
		listings = append(listings, listing)
	}

	if listings == nil {
		listings = []map[string]any{}
	}
	return listings, nil
}
