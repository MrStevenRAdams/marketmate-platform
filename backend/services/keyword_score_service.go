package services

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
)

// ── Scoring Constants ─────────────────────────────────────────────────────────
// Total possible score: 100 points across 6 components.
const (
	maxTitleCoverage     = 25 // How many keywords appear anywhere in the title
	maxTitlePlacement    = 15 // How many keywords appear in the first 80 chars of title
	maxBulletCoverage    = 20 // Keywords appearing in bullet points
	maxDescriptionDepth  = 15 // Keywords appearing in description
	maxFieldCompleteness = 15 // Listing fields populated vs available
	maxLayerConfidence   = 10 // Quality/confidence of the keyword source layer
)

// ── Data Structures ───────────────────────────────────────────────────────────

// Listing is the product listing being scored.
// Fields map to standard Amazon listing attributes.
type Listing struct {
	Title       string
	Bullets     []string // bullet_points
	Description string
	// Optional structured fields — presence counted for field completeness
	Brand        string
	Manufacturer string
	PartNumber   string
	ModelNumber  string
	Color        string
	Size         string
	Material     string
	SearchTerms  []string // backend keywords
}

// ListingScore is the result of ScoreListing.
// Total is the sum of all component scores (max 100).
type ListingScore struct {
	Total             int            `json:"total"`
	TitleCoverage     int            `json:"title_coverage"`
	TitlePlacement    int            `json:"title_placement"`
	BulletCoverage    int            `json:"bullet_coverage"`
	DescriptionDepth  int            `json:"description_depth"`
	FieldCompleteness int            `json:"field_completeness"`
	LayerConfidence   int            `json:"layer_confidence"`
	GapKeywords       []KeywordEntry `json:"gap_keywords"` // high-value keywords not found in listing
}

// ── Service ───────────────────────────────────────────────────────────────────

// KeywordScoreService computes SEO scores for Amazon listings.
type KeywordScoreService struct {
	firestoreClient *firestore.Client
}

// NewKeywordScoreService creates a KeywordScoreService.
// firestoreClient may be nil — ScoreFromStoredData will return a zero score
// instead of failing.
func NewKeywordScoreService(firestoreClient *firestore.Client) *KeywordScoreService {
	return &KeywordScoreService{firestoreClient: firestoreClient}
}

// ScoreListing applies the 6-component scoring model to the given listing
// against the provided keyword set. Returns a ListingScore with per-component
// breakdown and a list of gap keywords (top keywords not present in the listing).
func (s *KeywordScoreService) ScoreListing(listing Listing, keywordSet *KeywordSet) ListingScore {
	if keywordSet == nil || len(keywordSet.Keywords) == 0 {
		return ListingScore{}
	}

	titleLower := strings.ToLower(listing.Title)
	titleFront := titleLower
	if len(titleFront) > 80 {
		titleFront = titleFront[:80]
	}

	bulletsLower := strings.ToLower(strings.Join(listing.Bullets, " "))
	descLower := strings.ToLower(listing.Description)

	// Take top keywords by search volume for scoring (max 20 to keep scoring stable)
	scoringKeywords := topKeywords(keywordSet.Keywords, 20)

	// ── Component 1: Title Coverage (25 pts) ─────────────────────────────────
	titleHits := 0
	for _, kw := range scoringKeywords {
		if strings.Contains(titleLower, strings.ToLower(kw.Keyword)) {
			titleHits++
		}
	}
	titleCoverage := scoreComponent(titleHits, len(scoringKeywords), maxTitleCoverage)

	// ── Component 2: Title Placement (15 pts) ────────────────────────────────
	// Keywords in the first 80 characters indicate strong relevance signals
	frontHits := 0
	for _, kw := range scoringKeywords {
		if strings.Contains(titleFront, strings.ToLower(kw.Keyword)) {
			frontHits++
		}
	}
	titlePlacement := scoreComponent(frontHits, len(scoringKeywords), maxTitlePlacement)

	// ── Component 3: Bullet Coverage (20 pts) ────────────────────────────────
	bulletHits := 0
	for _, kw := range scoringKeywords {
		if strings.Contains(bulletsLower, strings.ToLower(kw.Keyword)) {
			bulletHits++
		}
	}
	bulletCoverage := scoreComponent(bulletHits, len(scoringKeywords), maxBulletCoverage)

	// ── Component 4: Description Depth (15 pts) ──────────────────────────────
	descHits := 0
	for _, kw := range scoringKeywords {
		if strings.Contains(descLower, strings.ToLower(kw.Keyword)) {
			descHits++
		}
	}
	descriptionDepth := scoreComponent(descHits, len(scoringKeywords), maxDescriptionDepth)

	// ── Component 5: Field Completeness (15 pts) ─────────────────────────────
	// Count how many of the 9 optional structured fields are populated
	totalFields := 9
	populatedFields := 0
	if listing.Brand != "" {
		populatedFields++
	}
	if listing.Manufacturer != "" {
		populatedFields++
	}
	if listing.PartNumber != "" {
		populatedFields++
	}
	if listing.ModelNumber != "" {
		populatedFields++
	}
	if listing.Color != "" {
		populatedFields++
	}
	if listing.Size != "" {
		populatedFields++
	}
	if listing.Material != "" {
		populatedFields++
	}
	if len(listing.Bullets) >= 3 {
		populatedFields++
	}
	if len(listing.SearchTerms) > 0 {
		populatedFields++
	}
	fieldCompleteness := scoreComponent(populatedFields, totalFields, maxFieldCompleteness)

	// ── Component 6: Layer Confidence (10 pts) ───────────────────────────────
	// DataForSEO data (from real Amazon rankings) = full confidence.
	// AI-generated keywords = lower confidence.
	layerConfidence := layerConfidenceScore(keywordSet.SourceLayer)

	// ── Gap Keywords ─────────────────────────────────────────────────────────
	// High-value keywords (search volume > 500) not found anywhere in the listing
	var gapKeywords []KeywordEntry
	allListingText := titleLower + " " + bulletsLower + " " + descLower
	for _, kw := range keywordSet.Keywords {
		if kw.SearchVolume < 500 {
			continue
		}
		if !strings.Contains(allListingText, strings.ToLower(kw.Keyword)) {
			gapKeywords = append(gapKeywords, kw)
			if len(gapKeywords) >= 10 {
				break
			}
		}
	}

	total := titleCoverage + titlePlacement + bulletCoverage + descriptionDepth + fieldCompleteness + layerConfidence

	return ListingScore{
		Total:             total,
		TitleCoverage:     titleCoverage,
		TitlePlacement:    titlePlacement,
		BulletCoverage:    bulletCoverage,
		DescriptionDepth:  descriptionDepth,
		FieldCompleteness: fieldCompleteness,
		LayerConfidence:   layerConfidence,
		GapKeywords:       gapKeywords,
	}
}

// ── Internal helpers ──────────────────────────────────────────────────────────

// scoreComponent converts a hit count to a component score proportionally.
// e.g. 6 hits out of 20 keywords for a 25-point component = 7 points.
func scoreComponent(hits, total, maxScore int) int {
	if total == 0 {
		return 0
	}
	score := (hits * maxScore) / total
	if score > maxScore {
		return maxScore
	}
	return score
}

// layerConfidenceScore maps a source layer name to its confidence score.
func layerConfidenceScore(sourceLayer string) int {
	switch sourceLayer {
	case "dataforseo":
		return maxLayerConfidence // 10/10 — real Amazon rank data
	case "amazon_ads":
		return 8 // near-real Amazon intent data
	case "brand_analytics":
		return 9 // Amazon's own SQP data
	case "ai":
		return 5 // AI-inferred, no real volume data
	default:
		return 3
	}
}

// topKeywords returns the top n keywords sorted by search volume descending.
// If the set has fewer than n keywords, all are returned.
func topKeywords(keywords []KeywordEntry, n int) []KeywordEntry {
	// Simple partial sort: find the n highest search volume entries.
	// For the small sizes involved (≤100 keywords) a full sort is acceptable.
	sorted := make([]KeywordEntry, len(keywords))
	copy(sorted, keywords)

	// Bubble sort descending by SearchVolume (n ≤ 100 so this is fine)
	for i := 0; i < len(sorted)-1; i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].SearchVolume > sorted[i].SearchVolume {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	if n > len(sorted) {
		n = len(sorted)
	}
	return sorted[:n]
}

// ── ScoreFromStoredData ───────────────────────────────────────────────────────

// ScoreFromStoredData fetches a listing and its associated keyword cache entry
// from Firestore, scores the listing, writes the updated seo_score and
// seo_score_updated_at fields back, and returns the score.
//
// Called by KeywordCacheScheduler after refreshing a global cache entry so that
// affected listings see updated scores without any seller action.
//
// Returns a zero ListingScore (no error) when:
//   - firestoreClient is nil
//   - listing has no keyword_cache_key (not yet associated)
func (s *KeywordScoreService) ScoreFromStoredData(
	ctx context.Context,
	tenantID string,
	listingID string,
) (ListingScore, error) {
	if s.firestoreClient == nil {
		return ListingScore{}, nil
	}

	// 1. Fetch the listing document
	listingRef := s.firestoreClient.
		Collection("tenants").Doc(tenantID).
		Collection("listings").Doc(listingID)

	listingDoc, err := listingRef.Get(ctx)
	if err != nil {
		return ListingScore{}, fmt.Errorf("ScoreFromStoredData: fetch listing %s/%s: %w", tenantID, listingID, err)
	}

	listingData := listingDoc.Data()

	// 2. Extract keyword_cache_key
	cacheKey, _ := listingData["keyword_cache_key"].(string)
	if cacheKey == "" {
		// No keyword data yet — return zero score, not an error
		return ListingScore{}, nil
	}

	// 3. Fetch global cache entry
	var keywordSet *KeywordSet
	globalDoc, err := s.firestoreClient.Collection("global_keyword_cache").Doc(cacheKey).Get(ctx)
	if err == nil && globalDoc.Exists() {
		var ks KeywordSet
		if mapErr := globalDoc.DataTo(&ks); mapErr == nil {
			keywordSet = &ks
		}
	}

	// 4. Check for tenant-private override — prefer if more recent than global cache
	// Path: tenants/{tenantID}/products/{productID}/keyword_intelligence/{productID}
	// productID is derived from the listing's product_id field (falls back to listingID)
	productID, _ := listingData["product_id"].(string)
	if productID == "" {
		productID = listingID
	}
	privateRef := s.firestoreClient.
		Collection("tenants").Doc(tenantID).
		Collection("products").Doc(productID).
		Collection("keyword_intelligence").Doc(productID)

	privateDoc, privateErr := privateRef.Get(ctx)
	if privateErr == nil && privateDoc.Exists() {
		var privateSet KeywordSet
		if mapErr := privateDoc.DataTo(&privateSet); mapErr == nil {
			if keywordSet == nil || privateSet.LastRefreshed.After(keywordSet.LastRefreshed) {
				log.Printf("[ScoreFromStoredData] using tenant-private keyword data for %s/%s (more recent)", tenantID, listingID)
				keywordSet = &privateSet
			}
		}
	}

	if keywordSet == nil {
		// Cache entry not found — return zero score
		return ListingScore{}, nil
	}

	// 5. Build Listing struct from Firestore document fields
	listing := Listing{}
	listing.Title, _ = listingData["title"].(string)
	listing.Description, _ = listingData["description"].(string)
	listing.Brand, _ = listingData["brand"].(string)
	listing.Manufacturer, _ = listingData["manufacturer"].(string)
	listing.PartNumber, _ = listingData["part_number"].(string)
	listing.ModelNumber, _ = listingData["model_number"].(string)
	listing.Color, _ = listingData["color"].(string)
	listing.Size, _ = listingData["size"].(string)
	listing.Material, _ = listingData["material"].(string)

	if bullets, ok := listingData["bullet_points"].([]interface{}); ok {
		for _, b := range bullets {
			if s, ok := b.(string); ok {
				listing.Bullets = append(listing.Bullets, s)
			}
		}
	}
	if terms, ok := listingData["search_terms"].([]interface{}); ok {
		for _, t := range terms {
			if s, ok := t.(string); ok {
				listing.SearchTerms = append(listing.SearchTerms, s)
			}
		}
	}

	// 6. Score
	score := s.ScoreListing(listing, keywordSet)

	// 7. Write updated score back to the listing document
	now := time.Now()
	_, writeErr := listingRef.Update(ctx, []firestore.Update{
		{Path: "seo_score", Value: score.Total},
		{Path: "seo_score_updated_at", Value: now},
	})
	if writeErr != nil {
		// Non-fatal — log but return the score we computed
		log.Printf("[ScoreFromStoredData] write score failed for %s/%s: %v", tenantID, listingID, writeErr)
	}

	return score, nil
}
