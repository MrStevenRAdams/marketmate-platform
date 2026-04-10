package services

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"module-a/instrumentation"
)

// ── Data Structures ───────────────────────────────────────────────────────────

// KeywordEntry is a single keyword with all enrichment data we may have.
type KeywordEntry struct {
	Keyword         string  `firestore:"keyword"           json:"keyword"`
	Score           float64 `firestore:"score"             json:"score"`
	SearchVolume    int     `firestore:"search_volume"     json:"search_volume"`
	OrganicRank     int     `firestore:"organic_rank"      json:"organic_rank"`
	BidEstimateLow  float64 `firestore:"bid_estimate_low"  json:"bid_estimate_low"`
	BidEstimateHigh float64 `firestore:"bid_estimate_high" json:"bid_estimate_high"`
	SourceLayer     string  `firestore:"source_layer"      json:"source_layer"` // "dataforseo"|"amazon_ads"|"amazon_catalog"|"ai"
}

// KeywordSet is the cached keyword intelligence result for a product / ASIN.
// Stored in the global_keyword_cache collection.
type KeywordSet struct {
	CacheKey      string         `firestore:"cache_key"      json:"cache_key"`
	Keywords      []KeywordEntry `firestore:"keywords"       json:"keywords"`
	SourceLayer   string         `firestore:"source_layer"   json:"source_layer"`
	SourceCount   int            `firestore:"source_count"   json:"source_count"`
	LastRefreshed time.Time      `firestore:"last_refreshed" json:"last_refreshed"`
	Category      string         `firestore:"category"       json:"category"`
}

// ProductInfo carries the minimal product metadata needed to build a keyword set.
type ProductInfo struct {
	ASIN     string
	Title    string
	Category string
}

// cacheTTL is how long a global cache entry is considered fresh.
const cacheTTL = 30 * 24 * time.Hour

// ── Service ───────────────────────────────────────────────────────────────────

// KeywordIntelligenceService orchestrates keyword retrieval, caching, and
// usage event logging for the SEO optimisation layer.
type KeywordIntelligenceService struct {
	firestoreClient *firestore.Client
	dataForSEO      *DataForSEOClient
	aiService       *AIService
	amazonAds       *AmazonAdsClient // Session 2 — nil if not configured, handled gracefully
}

// NewKeywordIntelligenceService creates the service. All clients except
// firestoreClient may be nil — the service degrades gracefully.
func NewKeywordIntelligenceService(
	firestoreClient *firestore.Client,
	dataForSEO *DataForSEOClient,
	aiService *AIService,
	amazonAds *AmazonAdsClient,
) *KeywordIntelligenceService {
	return &KeywordIntelligenceService{
		firestoreClient: firestoreClient,
		dataForSEO:      dataForSEO,
		aiService:       aiService,
		amazonAds:       amazonAds,
	}
}

// GetOrCreateKeywordSet returns a fresh KeywordSet from the global cache if
// one exists and is less than 30 days old. Otherwise it triggers a refresh.
func (s *KeywordIntelligenceService) GetOrCreateKeywordSet(
	ctx context.Context,
	cacheKey string,
	productInfo ProductInfo,
) (*KeywordSet, error) {
	doc, err := s.firestoreClient.
		Collection("global_keyword_cache").
		Doc(cacheKey).
		Get(ctx)

	if err == nil && doc.Exists() {
		var cached KeywordSet
		if mapErr := doc.DataTo(&cached); mapErr == nil {
			if time.Since(cached.LastRefreshed) < cacheTTL {
				return &cached, nil
			}
		}
	}

	if productInfo.ASIN != "" && s.dataForSEO != nil {
		return s.RefreshFromDataForSEO(ctx, productInfo.ASIN)
	}

	return s.RefreshFromAI(ctx, productInfo.Category, productInfo.Title)
}

// RefreshFromDataForSEO calls the DataForSEO client, builds a KeywordSet,
// writes it to global_keyword_cache, and logs a usage event.
func (s *KeywordIntelligenceService) RefreshFromDataForSEO(
	ctx context.Context,
	asin string,
) (*KeywordSet, error) {
	if s.dataForSEO == nil {
		return nil, fmt.Errorf("keyword_intelligence: DataForSEO client not initialised")
	}

	ranked, err := s.dataForSEO.GetRankedKeywords(ctx, asin)
	if err != nil {
		return nil, fmt.Errorf("keyword_intelligence: DataForSEO lookup failed: %w", err)
	}

	entries := make([]KeywordEntry, 0, len(ranked))
	for _, r := range ranked {
		entries = append(entries, KeywordEntry{
			Keyword:      r.Keyword,
			SearchVolume: r.SearchVolume,
			OrganicRank:  r.OrganicRank,
			SourceLayer:  "dataforseo",
		})
	}

	set := &KeywordSet{
		CacheKey:      asin,
		Keywords:      entries,
		SourceLayer:   "dataforseo",
		SourceCount:   len(entries),
		LastRefreshed: time.Now(),
	}

	if writeErr := s.writeToCache(ctx, asin, set); writeErr != nil {
		_ = writeErr
	}

	_ = instrumentation.LogUsageEvent(ctx, s.firestoreClient, instrumentation.UsageEvent{
		EventType:       instrumentation.EVTYPE_DATAFORSEO_ASIN_LOOKUP,
		ProductID:       asin,
		CreditCost:      0,
		PlatformCostUSD: 0.01,
		DataSource:      "dataforseo",
		Metadata: map[string]string{
			"asin":          asin,
			"keyword_count": fmt.Sprintf("%d", len(entries)),
		},
		Timestamp: time.Now(),
	})

	return set, nil
}

// RefreshFromAI calls the AI service with a keyword generation prompt, builds
// a KeywordSet, writes it to global_keyword_cache, and logs a usage event.
func (s *KeywordIntelligenceService) RefreshFromAI(
	ctx context.Context,
	category, title string,
) (*KeywordSet, error) {
	if s.aiService == nil || !s.aiService.IsAvailable() {
		return nil, fmt.Errorf("keyword_intelligence: AI service not available")
	}

	prompt := fmt.Sprintf(
		"You are an Amazon SEO expert. Generate the top 20 search keywords for this Amazon product.\n\nCategory: %s\nTitle: %s\n\nRespond with one keyword per line only — no numbering, no bullet points, no extra text. Focus on high search volume terms, long-tail buyer intent phrases, category-specific terminology, and feature/benefit keywords.",
		category, title,
	)

	result, err := s.aiService.GenerateText(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("keyword_intelligence: AI generation failed: %w", err)
	}

	entries := parseAIKeywordResult(result)
	cacheKey := buildAICacheKey(category, title)

	set := &KeywordSet{
		CacheKey:      cacheKey,
		Keywords:      entries,
		SourceLayer:   "ai",
		SourceCount:   len(entries),
		LastRefreshed: time.Now(),
		Category:      category,
	}

	if writeErr := s.writeToCache(ctx, cacheKey, set); writeErr != nil {
		_ = writeErr
	}

	_ = instrumentation.LogUsageEvent(ctx, s.firestoreClient, instrumentation.UsageEvent{
		EventType:  instrumentation.EVTYPE_AI_KEYWORD_REANALYSIS,
		CreditCost: 0.5,
		DataSource: "anthropic",
		Metadata: map[string]string{
			"category":      category,
			"title":         title,
			"keyword_count": fmt.Sprintf("%d", len(entries)),
		},
		Timestamp: time.Now(),
	})

	return set, nil
}

// ── Session 2: EnrichFromCatalogData ─────────────────────────────────────────

// EnrichFromCatalogData extracts keyword vocabulary from a GetCatalogItem
// response already fetched during import — zero additional API calls.
// Called from the /internal/keyword-intelligence/enrich endpoint which is
// invoked by import-enrich after a successful GetCatalogItem call.
//
// Skips write if a cache entry fresher than 30 days already exists.
// CreditCost: 0, PlatformCostUSD: 0 — completely free.
func (s *KeywordIntelligenceService) EnrichFromCatalogData(
	ctx context.Context,
	asin string,
	catalogData map[string]interface{},
) error {
	// Skip if the cache is already fresh — don't overwrite richer data
	doc, err := s.firestoreClient.Collection("global_keyword_cache").Doc(asin).Get(ctx)
	if err == nil && doc.Exists() {
		var cached KeywordSet
		if mapErr := doc.DataTo(&cached); mapErr == nil {
			if time.Since(cached.LastRefreshed) < cacheTTL {
				return nil
			}
		}
	}

	entries := extractCatalogKeywords(catalogData)
	if len(entries) == 0 {
		return nil
	}

	category := extractCatalogCategory(catalogData)

	set := &KeywordSet{
		CacheKey:      asin,
		Keywords:      entries,
		SourceLayer:   "amazon_catalog",
		SourceCount:   len(entries),
		LastRefreshed: time.Now(),
		Category:      category,
	}

	if writeErr := s.writeToCache(ctx, asin, set); writeErr != nil {
		return fmt.Errorf("catalog_extract: write cache: %w", writeErr)
	}

	_ = instrumentation.LogUsageEvent(ctx, s.firestoreClient, instrumentation.UsageEvent{
		EventType:       instrumentation.EVTYPE_AMAZON_CATALOG_EXTRACT,
		ProductID:       asin,
		CreditCost:      0,
		PlatformCostUSD: 0,
		DataSource:      "amazon_catalog",
		Metadata: map[string]string{
			"asin":          asin,
			"keyword_count": fmt.Sprintf("%d", len(entries)),
			"category":      category,
		},
		Timestamp: time.Now(),
	})

	return nil
}

// ── Session 2: RefreshFromAmazonAdsAPI ───────────────────────────────────────

// RefreshFromAmazonAdsAPI fetches keyword recommendations from the Amazon
// Advertising API and merges bid estimates into the global cache entry.
// Returns nil silently if no amazon_ads credential exists for the tenant.
// CreditCost: 0, PlatformCostUSD: 0 — absorbed as platform cost during import.
func (s *KeywordIntelligenceService) RefreshFromAmazonAdsAPI(
	ctx context.Context,
	asin string,
	tenantID string,
) error {
	if s.amazonAds == nil {
		return nil // client not configured — skip silently
	}

	creds, err := GetAmazonAdsCreds(ctx, s.firestoreClient, tenantID)
	if err != nil {
		return fmt.Errorf("amazon_ads: fetch creds: %w", err)
	}
	if creds == nil {
		return nil // no credential for this tenant — skip silently
	}

	recommendations, err := s.amazonAds.GetKeywordRecommendations(ctx, asin, creds)
	if err != nil {
		return fmt.Errorf("amazon_ads: recommendations: %w", err)
	}
	if len(recommendations) == 0 {
		return nil
	}

	// Read existing KeywordSet or start with an empty one
	existing := &KeywordSet{
		CacheKey:    asin,
		Keywords:    []KeywordEntry{},
		SourceLayer: "amazon_ads",
	}
	doc, err := s.firestoreClient.Collection("global_keyword_cache").Doc(asin).Get(ctx)
	if err == nil && doc.Exists() {
		var cached KeywordSet
		if mapErr := doc.DataTo(&cached); mapErr == nil {
			existing = &cached
		}
	}

	existing.Keywords = mergeAdsRecommendations(existing.Keywords, recommendations)

	// Promote source layer
	if existing.SourceLayer == "amazon_catalog" || existing.SourceLayer == "" {
		existing.SourceLayer = "amazon_ads"
	}
	existing.SourceCount = len(existing.Keywords)
	existing.LastRefreshed = time.Now()

	if writeErr := s.writeToCache(ctx, asin, existing); writeErr != nil {
		return fmt.Errorf("amazon_ads: write cache: %w", writeErr)
	}

	_ = instrumentation.LogUsageEvent(ctx, s.firestoreClient, instrumentation.UsageEvent{
		TenantID:        tenantID,
		EventType:       instrumentation.EVTYPE_AMAZON_ADS_KW_RECOMMENDATIONS,
		ProductID:       asin,
		CreditCost:      0,
		PlatformCostUSD: 0,
		DataSource:      "amazon_ads",
		Metadata: map[string]string{
			"asin":                 asin,
			"recommendation_count": fmt.Sprintf("%d", len(recommendations)),
		},
		Timestamp: time.Now(),
	})

	return nil
}

// ── Session 2: EnsureDataForSEOEnrichment ────────────────────────────────────

// EnsureDataForSEOEnrichment checks the global cache and calls DataForSEO only
// if the entry is absent or stale and doesn't already contain dataforseo data.
//
// CALL SITES:
//   - GenerateListing() in ai_listing_generation_service.go → force=false
//   - Manual refresh endpoint in keyword_intelligence_handler.go → force=true
//   - KeywordCacheScheduler.RunRefresh → force=false (relies on TTL check)
//
// When force=true the TTL check is skipped and DataForSEO is always called.
// CreditCost: 0 (absorbed), PlatformCostUSD: 0.012 per call.
func (s *KeywordIntelligenceService) EnsureDataForSEOEnrichment(
	ctx context.Context,
	asin string,
	tenantID string,
	force bool,
) error {
	if s.dataForSEO == nil {
		return nil
	}

	// Cache hit check — if dataforseo data present and fresh, skip (unless forced)
	doc, err := s.firestoreClient.Collection("global_keyword_cache").Doc(asin).Get(ctx)
	if !force && err == nil && doc.Exists() {
		var cached KeywordSet
		if mapErr := doc.DataTo(&cached); mapErr == nil {
			if strings.Contains(cached.SourceLayer, "dataforseo") && time.Since(cached.LastRefreshed) < cacheTTL {
				return nil
			}
		}
	}

	ranked, err := s.dataForSEO.GetRankedKeywords(ctx, asin)
	if err != nil {
		return fmt.Errorf("ensure_dataforseo: lookup: %w", err)
	}

	// Read existing set to merge into, or start fresh
	existing := &KeywordSet{
		CacheKey: asin,
		Keywords: []KeywordEntry{},
	}
	if doc != nil && doc.Exists() {
		var cached KeywordSet
		if mapErr := doc.DataTo(&cached); mapErr == nil {
			existing = &cached
		}
	}

	// Build lookup index for case-insensitive merge
	index := make(map[string]int, len(existing.Keywords))
	for i, e := range existing.Keywords {
		index[strings.ToLower(e.Keyword)] = i
	}

	for _, r := range ranked {
		lower := strings.ToLower(r.Keyword)
		if idx, found := index[lower]; found {
			existing.Keywords[idx].SearchVolume = r.SearchVolume
			existing.Keywords[idx].OrganicRank = r.OrganicRank
			existing.Keywords[idx].SourceLayer = "dataforseo"
		} else {
			newEntry := KeywordEntry{
				Keyword:      r.Keyword,
				SearchVolume: r.SearchVolume,
				OrganicRank:  r.OrganicRank,
				Score:        float64(r.SearchVolume) / 1000.0,
				SourceLayer:  "dataforseo",
			}
			existing.Keywords = append(existing.Keywords, newEntry)
			index[lower] = len(existing.Keywords) - 1
		}
	}

	// DataForSEO provides real volume — sort by SearchVolume DESC primary
	sortKeywordsVolumeFirst(existing.Keywords)

	existing.SourceLayer = "dataforseo"
	existing.SourceCount = len(existing.Keywords)
	existing.LastRefreshed = time.Now()

	if writeErr := s.writeToCache(ctx, asin, existing); writeErr != nil {
		return fmt.Errorf("ensure_dataforseo: write cache: %w", writeErr)
	}

	_ = instrumentation.LogUsageEvent(ctx, s.firestoreClient, instrumentation.UsageEvent{
		TenantID:        tenantID,
		EventType:       instrumentation.EVTYPE_DATAFORSEO_ASIN_LOOKUP,
		ProductID:       asin,
		CreditCost:      0,
		PlatformCostUSD: 0.012,
		DataSource:      "dataforseo",
		Metadata: map[string]string{
			"asin":          asin,
			"keyword_count": fmt.Sprintf("%d", len(ranked)),
		},
		Timestamp: time.Now(),
	})

	return nil
}

// ── Internal helpers ──────────────────────────────────────────────────────────

func (s *KeywordIntelligenceService) writeToCache(ctx context.Context, cacheKey string, set *KeywordSet) error {
	_, err := s.firestoreClient.
		Collection("global_keyword_cache").
		Doc(cacheKey).
		Set(ctx, set)
	return err
}

// extractCatalogKeywords builds keyword entries from a raw GetCatalogItem response.
// Scoring weights: title phrases (0.9) > bullets (0.7) > generic keywords (0.5) > product type (0.4)
func extractCatalogKeywords(data map[string]interface{}) []KeywordEntry {
	var entries []KeywordEntry
	seen := make(map[string]bool)

	addEntry := func(kw string, score float64) {
		kw = strings.TrimSpace(kw)
		if kw == "" || len(kw) < 3 {
			return
		}
		lower := strings.ToLower(kw)
		if seen[lower] {
			return
		}
		seen[lower] = true
		entries = append(entries, KeywordEntry{
			Keyword:     kw,
			Score:       score,
			SourceLayer: "amazon_catalog",
		})
	}

	// 1. Title — from summaries[].itemName — tokenise into meaningful phrases
	if summaries, ok := data["summaries"].([]interface{}); ok {
		for _, s := range summaries {
			sm := toMapIface(s)
			if title, ok := sm["itemName"].(string); ok && title != "" {
				for _, phrase := range tokeniseTitle(title) {
					addEntry(phrase, 0.9)
				}
				break // use first summary only
			}
		}
	}

	// 2. attributes.bullet_point[].value
	if attrs, ok := data["attributes"].(map[string]interface{}); ok {
		if bullets, ok := attrs["bullet_point"].([]interface{}); ok {
			for _, b := range bullets {
				bm := toMapIface(b)
				if val, ok := bm["value"].(string); ok && val != "" {
					addEntry(val, 0.7)
				}
			}
		}

		// 3. attributes.generic_keyword[].value — split compound strings
		if gkws, ok := attrs["generic_keyword"].([]interface{}); ok {
			for _, g := range gkws {
				gm := toMapIface(g)
				if val, ok := gm["value"].(string); ok && val != "" {
					for _, kw := range splitKeywordString(val) {
						addEntry(kw, 0.5)
					}
				}
			}
		}
	}

	// 4. productTypes[0].productType — human-readable product type
	if pts, ok := data["productTypes"].([]interface{}); ok && len(pts) > 0 {
		pt := toMapIface(pts[0])
		if ptName, ok := pt["productType"].(string); ok && ptName != "" {
			human := strings.ToLower(strings.ReplaceAll(ptName, "_", " "))
			addEntry(human, 0.4)
		}
	}

	return entries
}

// extractCatalogCategory pulls browseClassification.displayName from summaries.
func extractCatalogCategory(data map[string]interface{}) string {
	summaries, ok := data["summaries"].([]interface{})
	if !ok || len(summaries) == 0 {
		return ""
	}
	sm := toMapIface(summaries[0])
	if bc, ok := sm["browseClassification"].(map[string]interface{}); ok {
		if dn, ok := bc["displayName"].(string); ok {
			return dn
		}
	}
	return ""
}

// tokeniseTitle splits a product title into 2+ word phrases.
// Splits on commas, dashes and pipes (common Amazon title separators).
func tokeniseTitle(title string) []string {
	var phrases []string
	seen := make(map[string]bool)

	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || seen[strings.ToLower(s)] {
			return
		}
		seen[strings.ToLower(s)] = true
		phrases = append(phrases, s)
	}

	// Full title first (truncated to 80 chars) as the primary phrase
	add(truncStr(title, 80))

	for _, part := range strings.FieldsFunc(title, func(r rune) bool {
		return r == ',' || r == '-' || r == '|'
	}) {
		part = strings.TrimSpace(part)
		words := strings.Fields(part)
		if len(words) >= 2 {
			add(part)
		}
		// Also add leading 3-word sub-phrase for long parts
		if len(words) >= 4 {
			add(strings.Join(words[:3], " "))
		}
	}
	return phrases
}

// splitKeywordString splits a generic_keyword string on commas and semicolons,
// keeping only multi-word phrases (single words are usually too generic).
func splitKeywordString(s string) []string {
	var kws []string
	for _, part := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ';'
	}) {
		kw := strings.TrimSpace(part)
		if len(strings.Fields(kw)) >= 2 {
			kws = append(kws, kw)
		}
	}
	return kws
}

// mergeAdsRecommendations merges bid estimates from the Amazon Ads API into
// the existing keyword slice. Matching is case-insensitive.
// New keywords from Ads are appended; result sorted BidEstimateHigh DESC.
func mergeAdsRecommendations(existing []KeywordEntry, recs []AdsKeywordRecommendation) []KeywordEntry {
	index := make(map[string]int, len(existing))
	for i, e := range existing {
		index[strings.ToLower(e.Keyword)] = i
	}

	for _, rec := range recs {
		lower := strings.ToLower(rec.Keyword)
		if idx, found := index[lower]; found {
			existing[idx].BidEstimateLow = rec.BidLow
			existing[idx].BidEstimateHigh = rec.BidHigh
			existing[idx].SourceLayer = "amazon_ads"
		} else {
			newEntry := KeywordEntry{
				Keyword:         rec.Keyword,
				Score:           rec.BidHigh,
				BidEstimateLow:  rec.BidLow,
				BidEstimateHigh: rec.BidHigh,
				SourceLayer:     "amazon_ads",
			}
			existing = append(existing, newEntry)
			index[lower] = len(existing) - 1
		}
	}

	sortKeywordsBidFirst(existing)
	return existing
}

// sortKeywordsBidFirst sorts in place: BidEstimateHigh DESC, then SearchVolume DESC.
func sortKeywordsBidFirst(entries []KeywordEntry) {
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0; j-- {
			a, b := entries[j-1], entries[j]
			if b.BidEstimateHigh > a.BidEstimateHigh ||
				(b.BidEstimateHigh == a.BidEstimateHigh && b.SearchVolume > a.SearchVolume) {
				entries[j-1], entries[j] = entries[j], entries[j-1]
			} else {
				break
			}
		}
	}
}

// sortKeywordsVolumeFirst sorts in place: SearchVolume DESC, then BidEstimateHigh DESC.
func sortKeywordsVolumeFirst(entries []KeywordEntry) {
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0; j-- {
			a, b := entries[j-1], entries[j]
			if b.SearchVolume > a.SearchVolume ||
				(b.SearchVolume == a.SearchVolume && b.BidEstimateHigh > a.BidEstimateHigh) {
				entries[j-1], entries[j] = entries[j], entries[j-1]
			} else {
				break
			}
		}
	}
}

func parseAIKeywordResult(result string) []KeywordEntry {
	lines := strings.Split(strings.TrimSpace(result), "\n")
	entries := make([]KeywordEntry, 0, len(lines))
	for _, line := range lines {
		kw := strings.TrimSpace(line)
		if kw == "" {
			continue
		}
		kw = strings.TrimLeft(kw, "-•*0123456789. ")
		kw = strings.TrimSpace(kw)
		if kw == "" {
			continue
		}
		entries = append(entries, KeywordEntry{
			Keyword:     kw,
			SourceLayer: "ai",
		})
	}
	return entries
}

func buildAICacheKey(category, title string) string {
	titleSlug := strings.ToLower(strings.ReplaceAll(title, " ", "_"))
	if len(titleSlug) > 40 {
		titleSlug = titleSlug[:40]
	}
	catSlug := strings.ToLower(strings.ReplaceAll(category, " ", "_"))
	return fmt.Sprintf("ai_%s_%s", catSlug, titleSlug)
}

func toMapIface(v interface{}) map[string]interface{} {
	if m, ok := v.(map[string]interface{}); ok {
		return m
	}
	return map[string]interface{}{}
}

func truncStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

// ── Session 5: IngestCSV ──────────────────────────────────────────────────────

// IngestCSV parses an uploaded CSV file and merges keyword entries into both
// the tenant-private keyword_intelligence sub-collection and (if the product
// has an ASIN) the global keyword cache.
//
// Supported sourceType values:
//   - "brand_analytics" — Amazon Brand Analytics report
//   - "terapeak"        — eBay Terapeak keyword report
//   - "generic"         — One keyword per line, optional volume in second column
func (s *KeywordIntelligenceService) IngestCSV(
	ctx context.Context,
	tenantID string,
	productID string,
	csvBytes []byte,
	sourceType string,
) error {
	r := csv.NewReader(bytes.NewReader(csvBytes))
	r.TrimLeadingSpace = true
	r.LazyQuotes = true

	records, err := r.ReadAll()
	if err != nil {
		return fmt.Errorf("ingest_csv: parse: %w", err)
	}
	if len(records) < 2 {
		return fmt.Errorf("ingest_csv: no data rows in CSV")
	}

	header := make(map[string]int, len(records[0]))
	for i, h := range records[0] {
		header[strings.ToLower(strings.TrimSpace(h))] = i
	}

	var entries []KeywordEntry

	switch sourceType {
	case "brand_analytics":
		// Headers: "Search Query", "Search Frequency Rank", "Click Share", ...
		kwCol := colIndex(header, "search query", "keyword", "query")
		rankCol := colIndex(header, "search frequency rank", "rank", "sfr")
		if kwCol < 0 {
			return fmt.Errorf("ingest_csv: brand_analytics: cannot find keyword column in headers: %v", records[0])
		}
		for _, row := range records[1:] {
			if kwCol >= len(row) {
				continue
			}
			kw := strings.TrimSpace(row[kwCol])
			if kw == "" {
				continue
			}
			rank := 0
			if rankCol >= 0 && rankCol < len(row) {
				rank, _ = strconv.Atoi(strings.TrimSpace(row[rankCol]))
			}
			// Normalise rank to a pseudo-volume: rank 1 → 10000, rank 1000 → ~100
			volume := 0
			if rank > 0 {
				volume = 10000 / rank
				if volume < 1 {
					volume = 1
				}
			}
			entries = append(entries, KeywordEntry{
				Keyword:      kw,
				SearchVolume: volume,
				OrganicRank:  rank,
				Score:        float64(volume) / 10000.0,
				SourceLayer:  "brand_analytics",
			})
		}

	case "terapeak":
		// Headers: "Keywords", "Total Listings", "Sold Items", ...
		kwCol := colIndex(header, "keywords", "keyword", "search term")
		soldCol := colIndex(header, "sold items", "sold", "total sold")
		if kwCol < 0 {
			return fmt.Errorf("ingest_csv: terapeak: cannot find keyword column in headers: %v", records[0])
		}
		for _, row := range records[1:] {
			if kwCol >= len(row) {
				continue
			}
			kw := strings.TrimSpace(row[kwCol])
			if kw == "" {
				continue
			}
			sold := 0
			if soldCol >= 0 && soldCol < len(row) {
				sold, _ = strconv.Atoi(strings.TrimSpace(row[soldCol]))
			}
			entries = append(entries, KeywordEntry{
				Keyword:      kw,
				SearchVolume: sold,
				Score:        float64(sold) / 1000.0,
				SourceLayer:  "terapeak",
			})
		}

	case "generic":
		// One keyword per line; optional second column is volume integer
		// No header detection needed — treat every row as data
		startRow := 1
		if len(records) > 0 {
			// Detect if first row looks like a header
			first := strings.ToLower(strings.TrimSpace(records[0][0]))
			if first == "keyword" || first == "keywords" || first == "search term" {
				startRow = 1
			} else {
				startRow = 0 // no header
			}
		}
		for _, row := range records[startRow:] {
			if len(row) == 0 {
				continue
			}
			kw := strings.TrimSpace(row[0])
			if kw == "" {
				continue
			}
			volume := 0
			if len(row) >= 2 {
				volume, _ = strconv.Atoi(strings.TrimSpace(row[1]))
			}
			entries = append(entries, KeywordEntry{
				Keyword:      kw,
				SearchVolume: volume,
				Score:        float64(volume) / 1000.0,
				SourceLayer:  "generic_upload",
			})
		}

	default:
		return fmt.Errorf("ingest_csv: unknown source_type: %s", sourceType)
	}

	if len(entries) == 0 {
		return fmt.Errorf("ingest_csv: no keywords extracted from CSV")
	}

	// ── Write to tenant-private collection ───────────────────────────────────
	privateDocRef := s.firestoreClient.
		Collection("tenants").Doc(tenantID).
		Collection("products").Doc(productID).
		Collection("keyword_intelligence").Doc(productID)

	privateSet := &KeywordSet{
		CacheKey:      productID,
		Keywords:      entries,
		SourceLayer:   sourceType,
		SourceCount:   len(entries),
		LastRefreshed: time.Now(),
	}

	if _, err := privateDocRef.Set(ctx, privateSet); err != nil {
		return fmt.Errorf("ingest_csv: write private doc: %w", err)
	}

	// ── Merge into global cache if we have a cache key ─────────────────────
	// Fetch product to get ASIN — best-effort, non-blocking on failure
	cacheKey := productID // fallback
	productDoc, pdErr := s.firestoreClient.
		Collection("tenants").Doc(tenantID).
		Collection("products").Doc(productID).
		Get(ctx)
	if pdErr == nil {
		if asin, ok := productDoc.Data()["asin"].(string); ok && asin != "" {
			cacheKey = asin
		} else if ids, ok := productDoc.Data()["identifiers"].(map[string]interface{}); ok {
			if asin, ok := ids["asin"].(string); ok && asin != "" {
				cacheKey = asin
			}
		}
	}

	// Read existing global cache, merge
	existing := &KeywordSet{CacheKey: cacheKey, Keywords: []KeywordEntry{}}
	doc, _ := s.firestoreClient.Collection("global_keyword_cache").Doc(cacheKey).Get(ctx)
	if doc != nil && doc.Exists() {
		_ = doc.DataTo(existing)
	}

	index := make(map[string]int, len(existing.Keywords))
	for i, e := range existing.Keywords {
		index[strings.ToLower(e.Keyword)] = i
	}
	for _, entry := range entries {
		lower := strings.ToLower(entry.Keyword)
		if idx, found := index[lower]; found {
			if entry.SearchVolume > 0 {
				existing.Keywords[idx].SearchVolume = entry.SearchVolume
			}
			existing.Keywords[idx].SourceLayer = entry.SourceLayer
		} else {
			existing.Keywords = append(existing.Keywords, entry)
			index[lower] = len(existing.Keywords) - 1
		}
	}

	existing.SourceCount = len(existing.Keywords)
	existing.LastRefreshed = time.Now()
	// Only upgrade source_layer if we have a higher-quality source
	if sourceType == "brand_analytics" && existing.SourceLayer == "ai" {
		existing.SourceLayer = "brand_analytics"
	}

	if writeErr := s.writeToCache(ctx, cacheKey, existing); writeErr != nil {
		// Non-fatal — private write already succeeded
		fmt.Printf("[kw-ingest] warn: global cache write failed product=%s: %v\n", productID, writeErr)
	}

	_ = instrumentation.LogUsageEvent(ctx, s.firestoreClient, instrumentation.UsageEvent{
		TenantID:        tenantID,
		EventType:       instrumentation.EVTYPE_BRAND_ANALYTICS_PULL,
		ProductID:       productID,
		CreditCost:      0,
		PlatformCostUSD: 0,
		DataSource:      sourceType,
		Metadata: map[string]string{
			"source_type":   sourceType,
			"keyword_count": fmt.Sprintf("%d", len(entries)),
		},
		Timestamp: time.Now(),
	})

	return nil
}

// colIndex finds the first matching column name in a header map (case-insensitive).
// Accepts multiple candidate names; returns -1 if none found.
func colIndex(header map[string]int, candidates ...string) int {
	for _, c := range candidates {
		if idx, ok := header[strings.ToLower(c)]; ok {
			return idx
		}
	}
	return -1
}

// ForceRefreshFromDataForSEO is the same as EnsureDataForSEOEnrichment but
// bypasses the freshness check, always querying DataForSEO.
// Called from the manual refresh endpoint.
func (s *KeywordIntelligenceService) ForceRefreshFromDataForSEO(
	ctx context.Context,
	asin string,
	tenantID string,
) error {
	if s.dataForSEO == nil {
		// Fall back to AI refresh if DataForSEO not configured.
		// Fetch title/category from global cache to improve AI quality.
		category := ""
		title := asin // last resort
		doc, err := s.firestoreClient.Collection("global_keyword_cache").Doc(asin).Get(ctx)
		if err == nil && doc.Exists() {
			var cached KeywordSet
			if mapErr := doc.DataTo(&cached); mapErr == nil {
				category = cached.Category
			}
		}
		_, aiErr := s.RefreshFromAI(ctx, category, title)
		return aiErr
	}

	ranked, err := s.dataForSEO.GetRankedKeywords(ctx, asin)
	if err != nil {
		return fmt.Errorf("force_refresh: lookup: %w", err)
	}

	existing := &KeywordSet{CacheKey: asin, Keywords: []KeywordEntry{}}
	doc, _ := s.firestoreClient.Collection("global_keyword_cache").Doc(asin).Get(ctx)
	if doc != nil && doc.Exists() {
		_ = doc.DataTo(existing)
	}

	index := make(map[string]int, len(existing.Keywords))
	for i, e := range existing.Keywords {
		index[strings.ToLower(e.Keyword)] = i
	}
	for _, r := range ranked {
		lower := strings.ToLower(r.Keyword)
		if idx, found := index[lower]; found {
			existing.Keywords[idx].SearchVolume = r.SearchVolume
			existing.Keywords[idx].OrganicRank = r.OrganicRank
			existing.Keywords[idx].SourceLayer = "dataforseo"
		} else {
			newEntry := KeywordEntry{
				Keyword:      r.Keyword,
				SearchVolume: r.SearchVolume,
				OrganicRank:  r.OrganicRank,
				Score:        float64(r.SearchVolume) / 1000.0,
				SourceLayer:  "dataforseo",
			}
			existing.Keywords = append(existing.Keywords, newEntry)
			index[lower] = len(existing.Keywords) - 1
		}
	}

	sortKeywordsVolumeFirst(existing.Keywords)
	existing.SourceLayer = "dataforseo"
	existing.SourceCount = len(existing.Keywords)
	existing.LastRefreshed = time.Now()

	if writeErr := s.writeToCache(ctx, asin, existing); writeErr != nil {
		return fmt.Errorf("force_refresh: write cache: %w", writeErr)
	}

	_ = instrumentation.LogUsageEvent(ctx, s.firestoreClient, instrumentation.UsageEvent{
		TenantID:        tenantID,
		EventType:       instrumentation.EVTYPE_DATAFORSEO_ASIN_REFRESH,
		ProductID:       asin,
		CreditCost:      0.25,
		PlatformCostUSD: 0.012,
		DataSource:      "dataforseo",
		Metadata: map[string]string{
			"asin":          asin,
			"keyword_count": fmt.Sprintf("%d", len(ranked)),
			"force":         "true",
		},
		Timestamp: time.Now(),
	})

	return nil
}

// ── Session 6: RefreshFromBrandAnalytics ─────────────────────────────────────

// RefreshFromBrandAnalytics fetches keyword performance data from the Amazon
// SP-API Brand Analytics report for the given ASIN and writes results to the
// tenant-private keyword_intelligence sub-collection.
//
// Results are NEVER written to the global cache — Brand Analytics data is
// tenant-private (it reflects that tenant's own search query performance).
//
// STATUS: Stub — the actual SP-API call is pending Brand Analytics role
// approval. The function logs what it would do and returns nil so that the
// scheduler infrastructure is testable end-to-end without API access.
//
// Call site: KeywordCacheScheduler.RunBrandAnalyticsPull
func (s *KeywordIntelligenceService) RefreshFromBrandAnalytics(
	ctx context.Context,
	asin string,
	tenantID string,
	productID string,
	creds map[string]interface{},
) error {
	// Guard: check brand_analytics_enabled on the credential document.
	// If the field is absent, treat as false and return immediately.
	enabled, _ := creds["brand_analytics_enabled"].(bool)
	if !enabled {
		return nil
	}

	// Stub: log the intended SP-API call without making a real request.
	// The date range follows Amazon's recommended 30-day rolling window.
	endDate := time.Now().Format("2006-01-02")
	startDate := time.Now().AddDate(0, 0, -30).Format("2006-01-02")
	log.Printf(
		"[RefreshFromBrandAnalytics] STUB: would call GET_BRAND_ANALYTICS_SEARCH_QUERY_PERFORMANCE_REPORT "+
			"asin=%s tenant=%s productID=%s dateRange=%s..%s",
		asin, tenantID, productID, startDate, endDate,
	)

	// When the SP-API call is wired, results will be written here:
	//   tenants/{tenantID}/products/{productID}/keyword_intelligence/{productID}
	// The KeywordSet written will have SourceLayer "amazon_brand_analytics".
	// It must never be written to global_keyword_cache.

	// Log zero-cost usage event so the scheduler run is attributable in analytics.
	_ = instrumentation.LogUsageEvent(ctx, s.firestoreClient, instrumentation.UsageEvent{
		TenantID:        tenantID,
		EventType:       instrumentation.EVTYPE_BRAND_ANALYTICS_PULL,
		ProductID:       asin,
		CreditCost:      0,
		PlatformCostUSD: 0,
		DataSource:      "amazon_brand_analytics",
		Metadata: map[string]string{
			"asin":       asin,
			"product_id": productID,
			"status":     "stub_no_api_call",
			"date_range": startDate + ".." + endDate,
		},
		Timestamp: time.Now(),
	})

	return nil
}
