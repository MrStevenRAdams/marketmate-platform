package services

// ============================================================================
// KEYWORD CACHE SCHEDULER — Session 6
// ============================================================================
// Runs as a background goroutine started in main.go, following the same
// pattern as MacroScheduler.
//
// RunRefresh (daily via 24h ticker):
//   Queries global_keyword_cache for stale entries (last_refreshed > 30 days),
//   ordered by source_count DESC, limited to 500 per run.
//   - source_count >= 5: refresh via DataForSEO (cost absorbed by platform)
//   - source_count < 5:  refresh via AI (no DataForSEO cost)
//   After each cache refresh, propagates updated SEO scores to all tenant
//   listings that reference the cache key.
//
// RunBrandAnalyticsPull (weekly via 7×24h ticker):
//   Stub for SP-API Brand Analytics pull. Infrastructure is in place;
//   the actual API call will be wired when Brand Analytics role is approved.
// ============================================================================

import (
	"context"
	"log"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
)

// KeywordCacheScheduler refreshes stale entries in global_keyword_cache and
// propagates updated SEO scores to affected tenant listings.
type KeywordCacheScheduler struct {
	firestoreClient *firestore.Client
	kwIntelSvc      *KeywordIntelligenceService
	kwScoreSvc      *KeywordScoreService
	stopCh          chan struct{}
}

// NewKeywordCacheScheduler creates the scheduler.
// Dependencies mirror those available at the point of construction in main.go.
func NewKeywordCacheScheduler(
	firestoreClient *firestore.Client,
	kwIntelSvc *KeywordIntelligenceService,
	kwScoreSvc *KeywordScoreService,
) *KeywordCacheScheduler {
	return &KeywordCacheScheduler{
		firestoreClient: firestoreClient,
		kwIntelSvc:      kwIntelSvc,
		kwScoreSvc:      kwScoreSvc,
		stopCh:          make(chan struct{}),
	}
}

// Start launches two background goroutines: one for the daily cache refresh
// and one for the weekly Brand Analytics pull. Call from main after service init.
func (s *KeywordCacheScheduler) Start(ctx context.Context) {
	go s.runDaily(ctx)
	go s.runWeekly(ctx)
	log.Println("[KeywordCacheScheduler] started (daily refresh + weekly brand analytics)")
}

// Stop signals both goroutines to shut down gracefully.
func (s *KeywordCacheScheduler) Stop() {
	close(s.stopCh)
}

func (s *KeywordCacheScheduler) runDaily(ctx context.Context) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	// Run once shortly after startup so the first refresh isn't 24h away.
	// Use a short delay so main.go logging completes first.
	time.Sleep(30 * time.Second)
	if err := s.RunRefresh(ctx); err != nil {
		log.Printf("[KeywordCacheScheduler] initial daily refresh error: %v", err)
	}

	for {
		select {
		case <-ticker.C:
			if err := s.RunRefresh(ctx); err != nil {
				log.Printf("[KeywordCacheScheduler] daily refresh error: %v", err)
			}
		case <-s.stopCh:
			log.Println("[KeywordCacheScheduler] daily goroutine stopped")
			return
		case <-ctx.Done():
			return
		}
	}
}

func (s *KeywordCacheScheduler) runWeekly(ctx context.Context) {
	ticker := time.NewTicker(7 * 24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := s.RunBrandAnalyticsPull(ctx); err != nil {
				log.Printf("[KeywordCacheScheduler] weekly brand analytics pull error: %v", err)
			}
		case <-s.stopCh:
			log.Println("[KeywordCacheScheduler] weekly goroutine stopped")
			return
		case <-ctx.Done():
			return
		}
	}
}

// ── RunRefresh ────────────────────────────────────────────────────────────────

// RunRefresh is the core daily job. It:
//  1. Finds up to 500 stale cache entries ordered by source_count DESC
//  2. Refreshes each entry via DataForSEO (source_count >= 5) or AI (< 5)
//  3. Propagates updated SEO scores to all affected tenant listings
//
// Returns nil even when individual entries fail — failures are logged and
// counted in the summary. The caller should log any returned error.
func (s *KeywordCacheScheduler) RunRefresh(ctx context.Context) error {
	log.Println("[KeywordCacheScheduler] RunRefresh: starting")

	thirtyDaysAgo := time.Now().AddDate(0, 0, -30)

	// Step 1 — Find stale cache entries, prioritise by source_count DESC
	// so entries that benefit the most tenants are refreshed first.
	query := s.firestoreClient.Collection("global_keyword_cache").
		Where("last_refreshed", "<", thirtyDaysAgo).
		OrderBy("source_count", firestore.Desc).
		Limit(500)

	iter := query.Documents(ctx)
	defer iter.Stop()

	var (
		refreshedWithDataForSEO int
		refreshedWithAI         int
		failed                  int
	)

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Printf("[KeywordCacheScheduler] RunRefresh: query iteration error: %v", err)
			break
		}

		var entry KeywordSet
		if err := doc.DataTo(&entry); err != nil {
			log.Printf("[KeywordCacheScheduler] RunRefresh: DataTo error for %s: %v", doc.Ref.ID, err)
			failed++
			continue
		}

		cacheKey := doc.Ref.ID

		// Step 2 — Refresh entry
		if entry.SourceCount >= 5 {
			// High-value entry: refresh via DataForSEO, cost absorbed by platform.
			// force=false — rely on the existing TTL check inside EnsureDataForSEOEnrichment.
			// (The stale check above already confirms it is stale, but force=false preserves
			// the existing idempotency guard inside the service.)
			if refreshErr := s.kwIntelSvc.EnsureDataForSEOEnrichment(ctx, cacheKey, "platform", false); refreshErr != nil {
				log.Printf("[KeywordCacheScheduler] RunRefresh: DataForSEO refresh failed for %s: %v", cacheKey, refreshErr)
				failed++
				continue
			}
			log.Printf("[KeywordCacheScheduler] RunRefresh: DataForSEO refresh OK cacheKey=%s source_count=%d tenantID=platform", cacheKey, entry.SourceCount)
			refreshedWithDataForSEO++
		} else {
			// Low-value entry: refresh via AI — no DataForSEO cost.
			if _, refreshErr := s.kwIntelSvc.RefreshFromAI(ctx, entry.Category, cacheKey); refreshErr != nil {
				log.Printf("[KeywordCacheScheduler] RunRefresh: AI refresh failed for %s: %v", cacheKey, refreshErr)
				failed++
				continue
			}
			log.Printf("[KeywordCacheScheduler] RunRefresh: AI refresh OK cacheKey=%s source_count=%d", cacheKey, entry.SourceCount)
			refreshedWithAI++
		}

		// Step 3 — Propagate updated scores to all listings that reference this cache key.
		s.propagateScores(ctx, cacheKey)
	}

	log.Printf("[KeywordCacheScheduler] RunRefresh: complete — DataForSEO=%d AI=%d failed=%d",
		refreshedWithDataForSEO, refreshedWithAI, failed)

	return nil
}

// propagateScores finds all tenant listings that reference cacheKey and
// recomputes their SEO score using ScoreFromStoredData. Called after every
// successful cache refresh so sellers see updated scores overnight.
func (s *KeywordCacheScheduler) propagateScores(ctx context.Context, cacheKey string) {
	listingQuery := s.firestoreClient.CollectionGroup("listings").
		Where("keyword_cache_key", "==", cacheKey)

	iter := listingQuery.Documents(ctx)
	defer iter.Stop()

	var propagated, failed int

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Printf("[KeywordCacheScheduler] propagateScores: listing query error for %s: %v", cacheKey, err)
			break
		}

		// Extract tenantID and listingID from path: tenants/{tenantID}/listings/{listingID}
		parts := strings.Split(doc.Ref.Path, "/")
		// parts: ["projects", proj, "databases", db, "documents", "tenants", tenantID, "listings", listingID]
		// The collection path within the document tree is: tenants/{tenantID}/listings/{listingID}
		// Find "tenants" index then take +1 for tenantID and +3 for listingID
		tenantID := ""
		listingID := ""
		for i, p := range parts {
			if p == "tenants" && i+1 < len(parts) {
				tenantID = parts[i+1]
			}
			if p == "listings" && i+1 < len(parts) {
				listingID = parts[i+1]
			}
		}

		if tenantID == "" || listingID == "" {
			log.Printf("[KeywordCacheScheduler] propagateScores: could not extract IDs from path %s", doc.Ref.Path)
			failed++
			continue
		}

		if _, scoreErr := s.kwScoreSvc.ScoreFromStoredData(ctx, tenantID, listingID); scoreErr != nil {
			log.Printf("[KeywordCacheScheduler] propagateScores: score failed %s/%s: %v", tenantID, listingID, scoreErr)
			failed++
			continue
		}

		propagated++
	}

	if propagated > 0 || failed > 0 {
		log.Printf("[KeywordCacheScheduler] propagateScores: cacheKey=%s propagated=%d failed=%d",
			cacheKey, propagated, failed)
	}
}

// ── RunBrandAnalyticsPull ─────────────────────────────────────────────────────

// RunBrandAnalyticsPull is the weekly SP-API Brand Analytics pull.
// It iterates tenants with brand_analytics_enabled on their Amazon credential,
// fetches active ASINs, and calls RefreshFromBrandAnalytics for each.
//
// STATUS: Stub — the SP-API call is not wired until Brand Analytics role
// approval is obtained. All infrastructure is in place and testable.
func (s *KeywordCacheScheduler) RunBrandAnalyticsPull(ctx context.Context) error {
	log.Println("[KeywordCacheScheduler] RunBrandAnalyticsPull: starting (stub)")

	// Iterate all tenants
	tenantIter := s.firestoreClient.Collection("tenants").Documents(ctx)
	defer tenantIter.Stop()

	var processed, skipped, failed int

	for {
		tenantDoc, err := tenantIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Printf("[KeywordCacheScheduler] RunBrandAnalyticsPull: tenant iteration error: %v", err)
			break
		}
		tenantID := tenantDoc.Ref.ID

		// Find Amazon credentials with brand_analytics_enabled: true
		credIter := s.firestoreClient.
			Collection("tenants").Doc(tenantID).
			Collection("credentials").
			Where("channel", "in", []string{"amazon", "amazonnew"}).
			Documents(ctx)

		hasBrandAnalytics := false
		var eligibleCred map[string]interface{}

		for {
			credDoc, credErr := credIter.Next()
			if credErr == iterator.Done {
				break
			}
			if credErr != nil {
				break
			}
			data := credDoc.Data()
			enabled, _ := data["brand_analytics_enabled"].(bool)
			if enabled {
				hasBrandAnalytics = true
				eligibleCred = data
				break
			}
		}
		credIter.Stop()

		if !hasBrandAnalytics {
			skipped++
			continue
		}

		// Fetch active Amazon listings for this tenant
		listingIter := s.firestoreClient.
			Collection("tenants").Doc(tenantID).
			Collection("listings").
			Where("channel", "in", []string{"amazon", "amazonnew"}).
			Documents(ctx)

		for {
			listingDoc, listingErr := listingIter.Next()
			if listingErr == iterator.Done {
				break
			}
			if listingErr != nil {
				log.Printf("[KeywordCacheScheduler] RunBrandAnalyticsPull: listing iteration error tenant=%s: %v", tenantID, listingErr)
				break
			}

			listingData := listingDoc.Data()
			asin, _ := listingData["asin"].(string)
			if asin == "" {
				continue
			}

			productID, _ := listingData["product_id"].(string)
			if productID == "" {
				productID = listingDoc.Ref.ID
			}

			if pullErr := s.kwIntelSvc.RefreshFromBrandAnalytics(ctx, asin, tenantID, productID, eligibleCred); pullErr != nil {
				log.Printf("[KeywordCacheScheduler] RunBrandAnalyticsPull: failed asin=%s tenant=%s: %v", asin, tenantID, pullErr)
				failed++
				continue
			}
			processed++
		}
		listingIter.Stop()
	}

	log.Printf("[KeywordCacheScheduler] RunBrandAnalyticsPull: complete — processed=%d skipped=%d failed=%d",
		processed, skipped, failed)

	return nil
}

