package services

// ============================================================================
// MULTI-CHANNEL LISTING GENERATION SERVICE
// ============================================================================
//
// Generates listings for ALL enabled channels in a SINGLE AI call.
// Uses the consolidated extended_data record as the primary data source,
// falling back to the product record + raw branches if no consolidated record exists.
//
// CALL STRUCTURE
// ──────────────
// One Claude call receives:
//   - The base product
//   - The consolidated data record
//   - All marketplace schemas (required fields, allowed values, char limits)
//   - The active listing for each channel (if one already exists, for update mode)
//   - Optional: KeywordContext with ranked keywords for SEO injection
//
// Returns one AIListingOutput per channel, written as draft listings
// with state "ai_draft" (visually distinct from human-created drafts).
//
// TOKEN BUDGET MANAGEMENT
// ────────────────────────
// Amazon schema alone can be 200+ fields. We:
//   1. Send REQUIRED fields always
//   2. Send optional fields only if consolidated data can plausibly fill them
//   3. If payload > 60k tokens (estimated), split into per-channel calls
//
// AUTO-DRAFT SETTING
// ──────────────────
// Stored in tenants/{id}/config/settings → ai.auto_draft_channels ([]string)
// When a consolidation completes with confidence >= threshold, this service
// is called automatically for each channel in auto_draft_channels.
//
// KEYWORD CONTEXT (Session 2)
// ──────────────────────────
// Pass a non-nil *KeywordContext to inject keyword placement instructions into
// the system prompt. When nil, behaviour is identical to Session 1 — zero
// regression for existing callers. The keyword block is prepended before the
// channel schema sections so it applies globally to all channels.
// ============================================================================

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

// ─── Types ────────────────────────────────────────────────────────────────────

// ChannelGenerationRequest is everything needed to generate listings for one product
type ChannelGenerationRequest struct {
	TenantID         string
	ProductID        string
	ASIN             string // optional — used for DataForSEO enrichment check
	Channels         []string // which channels to generate for
	Schemas          []MarketplaceSchemaInput
	ConsolidatedData map[string]interface{}
	BaseProduct      AIProductInput
	Mode             string // "draft", "update"
}

// MultiChannelListingResult holds all generated listings for one product
type MultiChannelListingResult struct {
	ProductID  string
	Listings   []AIListingOutput
	DurationMS int64
	SplitCalls bool   // true if we had to split into multiple calls
	Error      string
}

// KeywordContext carries ranked keyword data to inject into listing generation prompts.
// Pass nil to use existing behaviour unchanged — zero regression for existing callers.
//
// Fields added in Session 3 (BackendKeywords, Tags, ItemSpecificsSuggestions,
// TitleTemplate) are all optional — zero values are safe for any channel that
// does not use them.
type KeywordContext struct {
	Keywords                 []string // ranked by commercial importance, index 0 = most important
	TargetChannel            string   // optional — used for channel-specific instructions
	TitleMaxChars            int      // optional — if set, used in placement instruction
	BackendKeywords          string   // Amazon: space-separated search terms for the backend field
	Tags                     []string // Etsy: max 13 tags, max 20 chars each
	ItemSpecificsSuggestions []string // eBay: suggested item specific values
	TitleTemplate            string   // Temu: structured title template string
}

// ─── Service ──────────────────────────────────────────────────────────────────

type AIListingGenerationService struct {
	aiSvc      *AIService
	kwIntelSvc *KeywordIntelligenceService
}

func NewAIListingGenerationService(aiSvc *AIService) *AIListingGenerationService {
	return &AIListingGenerationService{aiSvc: aiSvc}
}

// SetKeywordIntelligenceService wires the keyword intelligence service in after
// construction — avoids a circular dependency since both services are created
// close together in main.go.
func (s *AIListingGenerationService) SetKeywordIntelligenceService(kwIntelSvc *KeywordIntelligenceService) {
	s.kwIntelSvc = kwIntelSvc
}

// ─── GenerateForAllChannels ───────────────────────────────────────────────────

// GenerateForAllChannels generates marketplace listings for all channels in req.
// Pass a non-nil kwCtx to inject keyword placement instructions into the prompt.
// Existing callers pass nil — behaviour is identical to before Session 2.
func (s *AIListingGenerationService) GenerateForAllChannels(
	ctx context.Context,
	req ChannelGenerationRequest,
	kwCtx *KeywordContext,
) (*MultiChannelListingResult, error) {

	start := time.Now()
	result := &MultiChannelListingResult{ProductID: req.ProductID}

	if len(req.Channels) == 0 {
		result.Error = "no channels specified"
		return result, fmt.Errorf(result.Error)
	}

	// Ensure DataForSEO enrichment has run before generation.
	// Cache hit = free; cache miss = one DataForSEO API call.
	// Error is non-fatal: generation proceeds with whatever keyword data exists.
	if s.kwIntelSvc != nil && req.ASIN != "" {
		if err := s.kwIntelSvc.EnsureDataForSEOEnrichment(ctx, req.ASIN, req.TenantID, false); err != nil {
			log.Printf("[ListingGen] DataForSEO enrichment failed for %s, continuing: %v", req.ASIN, err)
		}
	}

	// Estimate if we need to split (rough heuristic: > 3 channels with large schemas)
	estimatedTokens := estimateTokens(req)
	log.Printf("[ListingGen] Product %s: %d channels, ~%d estimated tokens",
		req.ProductID, len(req.Channels), estimatedTokens)

	var listings []AIListingOutput
	var err error

	if estimatedTokens > 60000 && len(req.Channels) > 1 {
		// Split: generate each channel separately.
		// When a KeywordSet is available, the caller should use
		// keyword.Get(channel).Transform(keywordSet) to produce a
		// channel-specific kwCtx before calling GenerateForAllChannels,
		// or pass the kwCtx per channel via a future per-channel variant.
		// (See Session 4 for per-channel kwCtx plumbing.)
		result.SplitCalls = true
		log.Printf("[ListingGen] Splitting into %d per-channel calls", len(req.Channels))
		for _, channel := range req.Channels {
			var schema *MarketplaceSchemaInput
			for _, sc := range req.Schemas {
				if sc.Channel == channel {
					schema = &sc
					break
				}
			}
			channelListings, callErr := s.generateSingleChannel(ctx, req, channel, schema, kwCtx)
			if callErr != nil {
				log.Printf("[ListingGen] Channel %s failed: %v", channel, callErr)
				listings = append(listings, AIListingOutput{
					Channel:  channel,
					Warnings: []string{fmt.Sprintf("Generation failed: %v", callErr)},
				})
			} else {
				listings = append(listings, channelListings...)
			}
		}
	} else {
		// Single call for all channels
		listings, err = s.generateAllChannels(ctx, req, kwCtx)
		if err != nil {
			result.Error = err.Error()
			result.DurationMS = time.Since(start).Milliseconds()
			return result, err
		}
	}

	result.Listings = listings
	result.DurationMS = time.Since(start).Milliseconds()
	return result, nil
}

// ─── Single call for all channels ────────────────────────────────────────────

func (s *AIListingGenerationService) generateAllChannels(
	ctx context.Context,
	req ChannelGenerationRequest,
	kwCtx *KeywordContext,
) ([]AIListingOutput, error) {

	prompt := s.buildMultiChannelPrompt(req, kwCtx)
	response, err := s.aiSvc.callClaude(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("claude call: %w", err)
	}

	var listings []AIListingOutput
	if err := parseJSONFromResponse(response, &listings); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	return listings, nil
}

// ─── Per-channel fallback ─────────────────────────────────────────────────────

func (s *AIListingGenerationService) generateSingleChannel(
	ctx context.Context,
	req ChannelGenerationRequest,
	channel string,
	schema *MarketplaceSchemaInput,
	kwCtx *KeywordContext,
) ([]AIListingOutput, error) {

	singleReq := req
	singleReq.Channels = []string{channel}
	if schema != nil {
		singleReq.Schemas = []MarketplaceSchemaInput{*schema}
	} else {
		singleReq.Schemas = nil
	}

	return s.generateAllChannels(ctx, singleReq, kwCtx)
}

// ─── Multi-channel prompt ─────────────────────────────────────────────────────

func (s *AIListingGenerationService) buildMultiChannelPrompt(req ChannelGenerationRequest, kwCtx *KeywordContext) string {
	productJSON, _ := json.MarshalIndent(req.BaseProduct, "", "  ")

	// Consolidated data — this is the enriched record
	var enrichedSection string
	if len(req.ConsolidatedData) > 0 {
		// Select the most useful fields (keep token count reasonable)
		useful := extractUsefulConsolidatedFields(req.ConsolidatedData)
		usefulJSON, _ := json.MarshalIndent(useful, "", "  ")
		enrichedSection = fmt.Sprintf("\nENRICHED PRODUCT DATA (from cross-marketplace consolidation — use to fill attributes):\n%s", string(usefulJSON))
	}

	// Keyword context block — prepended before channel schemas when present
	keywordBlock := buildKeywordPromptBlock(kwCtx)

	// Build per-channel schema sections
	var schemaSections []string
	for _, schema := range req.Schemas {
		schemaSections = append(schemaSections, s.formatSchemaSection(schema))
	}

	channelList := strings.Join(req.Channels, ", ")

	return fmt.Sprintf(`You are an expert e-commerce listing specialist. You will generate optimised marketplace listings for ALL of the following channels in a single response.

BASE PRODUCT DATA (from PIM — this is the seller's own record, treat as authoritative):
%s
%s
%s
TARGET CHANNELS: %s

%s

─────────────────────────────────────────────────────────────────
GLOBAL RULES (apply to all channels)
─────────────────────────────────────────────────────────────────

FACTUAL ACCURACY — CRITICAL:
1. Every claim in titles, descriptions, and bullet points MUST be traceable to a fact in the product data above.
2. NEVER fabricate: materials, dimensions, weight, features, certifications, age ratings, safety claims, compatibility.
3. You MAY rephrase and restructure existing facts for better keyword coverage.
4. You MAY add generic category terms (e.g. "toy", "figure", "stool") that accurately describe WHAT the product IS.
5. If data is sparse, make a shorter factual listing rather than padding with assumptions.
6. Add a warning for any required field you cannot populate from the data.

ATTRIBUTE RULES:
- For enum fields: only select a value if the product data supports it. Otherwise null.
- For dimensions/weight: use ONLY values from the base product record (not from enriched data — those are unreliable).
- For identifiers (EAN, UPC, GTIN): use exactly as provided.

STATE: All generated listings should have state "ai_draft" — they require human review before publishing.

─────────────────────────────────────────────────────────────────
Respond with ONLY a valid JSON array — one object per channel, no markdown:
[
  {
    "channel": "channel_name",
    "state": "ai_draft",
    "title": "...",
    "description": "...",
    "bullet_points": ["...", "..."],
    "category_id": "...",
    "category_name": "...",
    "attributes": { "field": "value or null" },
    "search_terms": ["..."],
    "suggested_price": 0.00,
    "condition": "new",
    "confidence": 0.85,
    "warnings": ["any missing required fields"]
  }
]`,
		string(productJSON),
		enrichedSection,
		keywordBlock,
		channelList,
		strings.Join(schemaSections, "\n\n"),
	)
}

// ─── Keyword prompt block ─────────────────────────────────────────────────────

// buildKeywordPromptBlock formats the keyword placement instruction block for
// injection into the system prompt. Returns "" when kwCtx is nil or has no keywords.
func buildKeywordPromptBlock(kwCtx *KeywordContext) string {
	if kwCtx == nil || len(kwCtx.Keywords) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("KEYWORD PLACEMENT INSTRUCTIONS:\n")
	sb.WriteString(fmt.Sprintf("Title character limit: %d\n", kwCtx.TitleMaxChars))
	if kwCtx.TitleTemplate != "" {
		sb.WriteString(fmt.Sprintf("Title must follow this structure: %s\n", kwCtx.TitleTemplate))
	}
	sb.WriteString("Keywords ranked by commercial importance (integrate naturally, do not list mechanically):\n")
	for i, kw := range kwCtx.Keywords {
		sb.WriteString(fmt.Sprintf("  %d. %s\n", i+1, kw))
	}
	sb.WriteString("Placement rules:\n")
	sb.WriteString("  - Keywords 1-2 MUST appear in the first 80 characters of the title\n")
	sb.WriteString("  - Keywords 3-5 should appear in the first two bullet points or key features\n")
	sb.WriteString("  - Keywords 6-10 should be distributed across remaining bullets and description opening\n")
	if kwCtx.BackendKeywords != "" {
		sb.WriteString(fmt.Sprintf("Backend/search terms field: populate with: %s\n", kwCtx.BackendKeywords))
	}
	if len(kwCtx.Tags) > 0 {
		sb.WriteString(fmt.Sprintf("Tags: generate exactly these tags: %s\n", strings.Join(kwCtx.Tags, ", ")))
	}
	if len(kwCtx.ItemSpecificsSuggestions) > 0 {
		sb.WriteString(fmt.Sprintf("Item specifics suggestions: %s\n", strings.Join(kwCtx.ItemSpecificsSuggestions, ", ")))
	}
	return sb.String()
}

func (s *AIListingGenerationService) formatSchemaSection(schema MarketplaceSchemaInput) string {
	var required, optional []string

	for _, f := range schema.Fields {
		desc := f.Name
		if f.DisplayName != "" && f.DisplayName != f.Name {
			desc += fmt.Sprintf(" (%s)", f.DisplayName)
		}
		if f.DataType != "" {
			desc += fmt.Sprintf(" [%s]", f.DataType)
		}
		if len(f.AllowedValues) > 0 {
			shown := f.AllowedValues
			if len(shown) > 8 {
				shown = append(shown[:8], fmt.Sprintf("...%d more", len(f.AllowedValues)-8))
			}
			desc += fmt.Sprintf(" values:{%s}", strings.Join(shown, "|"))
		}
		if f.MaxLength > 0 {
			desc += fmt.Sprintf(" max:%d", f.MaxLength)
		}
		if f.Required {
			required = append(required, "  "+desc)
		} else {
			optional = append(optional, "  "+desc)
		}
	}

	// Cap optional fields shown
	if len(optional) > 25 {
		optional = append(optional[:25], fmt.Sprintf("  ...and %d more optional fields", len(optional)-25))
	}

	var categoryPath string
	if len(schema.CategoryPath) > 0 {
		categoryPath = "\nCategory path: " + strings.Join(schema.CategoryPath, " > ")
	}

	return fmt.Sprintf(`─── %s ───
Category: %s (ID: %s)%s
Required attributes:
%s
Optional attributes (populate only where product data clearly supports):
%s
%s`,
		strings.ToUpper(schema.Channel),
		schema.CategoryName, schema.CategoryID,
		categoryPath,
		strings.Join(required, "\n"),
		strings.Join(optional, "\n"),
		buildChannelSpecifications([]string{schema.Channel}),
	)
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// extractUsefulConsolidatedFields selects the most token-efficient subset
// of consolidated data to include in the prompt
func extractUsefulConsolidatedFields(data map[string]interface{}) map[string]interface{} {
	useful := make(map[string]interface{})

	// Always include these
	always := []string{
		"title", "short_description", "bullet_points", "key_features",
		"brand", "mpn", "model_number", "ean", "eans", "upc", "gtin", "epid",
		"all_images", "estimated_sold_quantity", "category_path",
	}
	for _, k := range always {
		if v, ok := data[k]; ok && v != nil {
			useful[k] = v
		}
	}

	// Include everything else except internal meta fields
	skip := map[string]bool{
		"consolidation_confidence": true, "branches_used": true,
		"branches_discarded": true, "needs_review": true,
		"consolidated_at": true, "enriched_at": true,
		"enrichment_phase": true, "ean_searched": true,
		"listing_url": true, "item_web_url": true,
	}
	for k, v := range data {
		if !skip[k] && !containsKey(useful, k) && v != nil {
			useful[k] = v
		}
	}

	return useful
}

func containsKey(m map[string]interface{}, k string) bool {
	_, ok := m[k]
	return ok
}

func estimateTokens(req ChannelGenerationRequest) int {
	// Rough estimate: product JSON + consolidated + schemas
	productBytes := 2000 // rough average
	consolidatedBytes := len(req.ConsolidatedData) * 50
	schemaBytes := 0
	for _, s := range req.Schemas {
		schemaBytes += len(s.Fields) * 60
	}
	// Divide by ~4 bytes per token
	return (productBytes + consolidatedBytes + schemaBytes) / 4
}
