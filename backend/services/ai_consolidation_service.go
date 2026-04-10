package services

// ============================================================================
// AI CONSOLIDATION SERVICE
// ============================================================================
//
// PURPOSE
// ───────
// After enrichment collects data from multiple sources (your own eBay listing,
// other sellers' listings for the same EAN, eBay catalogue, Amazon catalogue),
// this service runs a single AI call that:
//
//   1. IDENTITY VERIFICATION — Multi-signal check that all branches are for
//      the same physical product:
//        • Title semantic similarity  (e.g. "bar stool" vs "bathroom tap" → discard)
//        • Category consistency       (Furniture vs Plumbing → hard discard)
//        • Key attribute consistency  (colour, brand, model)
//        • Dimensions/weight flagged  (unreliable — soft flag only, never discard)
//        • Image comparison           (optional, only when title/category uncertain)
//        • Price plausibility         (3× outliers flagged, not discarded)
//
//   2. DEDUPLICATION — When 5 branches all say "recommended_age: 3+",
//      that becomes one value. When branches disagree, the most common value
//      wins; conflicts are preserved in full for audit.
//
//   3. CONSOLIDATION — A single clean record is written as source_key
//      "consolidated" in the extended_data collection. This is what the
//      listing generator reads from.
//
//   4. PIM WRITEBACK — Conservative: only fills PIM fields that are currently
//      empty. Never overwrites existing data. Dimensions/weight are skipped
//      unless the product has no dimensions at all.
//
// FIRESTORE OUTPUT
// ────────────────
//   extended_data/consolidated          — the clean merged record
//   extended_data/consolidated_meta     — verification signals, conflicts,
//                                         discarded branches, model used
//
// JOB TRACKING
// ────────────
//   consolidation_jobs/{job_id}         — per-tenant job with full progress
//
// ============================================================================

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"module-a/models"
	"module-a/repository"
)

// ─── Models ──────────────────────────────────────────────────────────────────

// ConsolidationJob tracks a bulk consolidation run
type ConsolidationJob struct {
	JobID       string     `firestore:"job_id" json:"job_id"`
	TenantID    string     `firestore:"tenant_id" json:"tenant_id"`
	Status      string     `firestore:"status" json:"status"`
	StatusMessage string   `firestore:"status_message,omitempty" json:"status_message,omitempty"`
	Total       int        `firestore:"total" json:"total"`
	Processed   int        `firestore:"processed" json:"processed"`
	Succeeded   int        `firestore:"succeeded" json:"succeeded"`
	Failed      int        `firestore:"failed" json:"failed"`
	Skipped     int        `firestore:"skipped" json:"skipped"` // no extended data
	CreatedAt   time.Time  `firestore:"created_at" json:"created_at"`
	UpdatedAt   time.Time  `firestore:"updated_at" json:"updated_at"`
	StartedAt   *time.Time `firestore:"started_at,omitempty" json:"started_at,omitempty"`
	CompletedAt *time.Time `firestore:"completed_at,omitempty" json:"completed_at,omitempty"`
}

// ConsolidationSignals are the per-signal confidence scores
type ConsolidationSignals struct {
	TitleSimilarity      float64 `firestore:"title_similarity" json:"title_similarity"`
	CategoryConsistency  float64 `firestore:"category_consistency" json:"category_consistency"`
	AttributeConsistency float64 `firestore:"attribute_consistency" json:"attribute_consistency"`
	ImageSimilarity      float64 `firestore:"image_similarity" json:"image_similarity"`   // -1 if not run
	PricePlausibility    float64 `firestore:"price_plausibility" json:"price_plausibility"`
	Overall              float64 `firestore:"overall" json:"overall"`
}

// ConsolidatedBranch is the AI's verdict on one source branch
type ConsolidatedBranch struct {
	SourceKey  string `firestore:"source_key" json:"source_key"`
	Decision   string `firestore:"decision" json:"decision"`   // "keep", "discard", "flag"
	Confidence float64 `firestore:"confidence" json:"confidence"`
	Reason     string `firestore:"reason,omitempty" json:"reason,omitempty"`
}

// ConsolidationConflict records a field where branches disagreed
type ConsolidationConflict struct {
	Field    string            `firestore:"field" json:"field"`
	Values   map[string]string `firestore:"values" json:"values"`   // sourceKey → value
	Resolved string            `firestore:"resolved" json:"resolved"` // chosen value
	Note     string            `firestore:"note,omitempty" json:"note,omitempty"`
}

// ConsolidationMeta is stored alongside the consolidated record
type ConsolidationMeta struct {
	ProductID         string                  `firestore:"product_id" json:"product_id"`
	TenantID          string                  `firestore:"tenant_id" json:"tenant_id"`
	Signals           ConsolidationSignals    `firestore:"signals" json:"signals"`
	BranchDecisions   []ConsolidatedBranch    `firestore:"branch_decisions" json:"branch_decisions"`
	Conflicts         []ConsolidationConflict `firestore:"conflicts" json:"conflicts"`
	TotalBranches     int                     `firestore:"total_branches" json:"total_branches"`
	DiscardedBranches int                     `firestore:"discarded_branches" json:"discarded_branches"`
	FlaggedBranches   int                     `firestore:"flagged_branches" json:"flagged_branches"`
	ReviewRequired    bool                    `firestore:"review_required" json:"review_required"`
	ReviewReasons     []string                `firestore:"review_reasons,omitempty" json:"review_reasons,omitempty"`
	ConsolidatedAt    time.Time               `firestore:"consolidated_at" json:"consolidated_at"`
	AIModel           string                  `firestore:"ai_model" json:"ai_model"`
	DurationMS        int64                   `firestore:"duration_ms" json:"duration_ms"`
}

// ConsolidationResult is returned from ConsolidateProduct
type ConsolidationResult struct {
	ProductID      string
	Meta           *ConsolidationMeta
	Consolidated   map[string]interface{}
	PIMLfieldsSet  []string // PIM fields that were written back
	ReviewRequired bool
	Error          string
}

// ─── Service ─────────────────────────────────────────────────────────────────

type AIConsolidationService struct {
	ai          *AIService
	repo        *repository.MarketplaceRepository
	productRepo *repository.FirestoreRepository
}

func NewAIConsolidationService(
	ai *AIService,
	repo *repository.MarketplaceRepository,
	productRepo *repository.FirestoreRepository,
) *AIConsolidationService {
	return &AIConsolidationService{ai: ai, repo: repo, productRepo: productRepo}
}

// ─── ConsolidateProduct ───────────────────────────────────────────────────────
//
// Full consolidation for a single product. Reads all extended_data branches,
// calls AI to verify/deduplicate/merge, writes consolidated record, and
// optionally writes back to PIM (conservative mode — missing fields only).

func (s *AIConsolidationService) ConsolidateProduct(
	ctx context.Context,
	tenantID, productID string,
	opts ConsolidationOptions,
) (*ConsolidationResult, error) {

	start := time.Now()
	result := &ConsolidationResult{ProductID: productID}

	// 1. Load all extended_data branches
	branches, err := s.repo.ListExtendedData(ctx, tenantID, productID)
	if err != nil {
		return nil, fmt.Errorf("list extended data: %w", err)
	}

	// Skip products that haven't been enriched at all
	if len(branches) == 0 {
		return nil, fmt.Errorf("no extended data found for product %s", productID)
	}

	// Filter out any existing consolidated records — we regenerate them
	var sourceBranches []models.ExtendedProductData
	for _, b := range branches {
		if b.SourceKey != "consolidated" && b.SourceKey != "consolidated_meta" {
			sourceBranches = append(sourceBranches, b)
		}
	}

	if len(sourceBranches) == 0 {
		return nil, fmt.Errorf("only consolidated records found, no source data")
	}

	// 2. Load the product from PIM for context
	product, err := s.productRepo.GetProduct(ctx, tenantID, productID)
	if err != nil {
		return nil, fmt.Errorf("get product: %w", err)
	}

	// 3. Build image URLs from product assets for optional vision check
	var imageURLs []string
	if opts.UseImageComparison {
		for _, asset := range product.Assets {
			if asset.URL != "" {
				imageURLs = append(imageURLs, asset.URL)
			}
		}
		// Also collect image URLs from the ebay_item_* branch if we have one
		for _, b := range sourceBranches {
			if strings.HasPrefix(b.SourceKey, "ebay_item_") {
				if url, ok := b.Data["image_url"].(string); ok && url != "" {
					imageURLs = append(imageURLs, url)
				}
				if extras, ok := b.Data["additional_images"].([]interface{}); ok {
					for _, e := range extras {
						if u, ok := e.(string); ok {
							imageURLs = append(imageURLs, u)
						}
					}
				}
				break
			}
		}
	}

	// 4. Build the consolidation prompt
	prompt := s.buildConsolidationPrompt(ctx, product, sourceBranches, imageURLs, opts)

	// 5. Call AI — model chosen per tenant settings, default is Gemini Flash
	model := opts.ConsolidationModel
	if model == "" {
		model = "gemini-2.0-flash"
	}
	response, err := s.ai.CallWithModel(ctx, prompt, model)
	if err != nil {
		return nil, fmt.Errorf("AI consolidation call (%s): %w", model, err)
	}

	// Auto-escalate to Claude if confidence came back low and escalation is enabled
	aiOutput, err := parseConsolidationResponse(response)
	if err != nil {
		return nil, fmt.Errorf("parse consolidation response (%s): %w", model, err)
	}

	// Auto-escalation: if Gemini returned low confidence, retry with Claude
	if opts.AutoEscalate &&
		strings.HasPrefix(model, "gemini-") &&
		aiOutput.Signals.Overall < opts.ConfidenceThreshold {
		log.Printf("[Consolidation] %s: Gemini confidence=%.2f < %.2f, escalating to Claude",
			productID, aiOutput.Signals.Overall, opts.ConfidenceThreshold)
		claudeModel := "claude-sonnet-4-20250514"
		escalatedResponse, escalateErr := s.ai.CallWithModel(ctx, prompt, claudeModel)
		if escalateErr != nil {
			log.Printf("[Consolidation] Escalation failed, using Gemini result: %v", escalateErr)
		} else {
			if escalatedOutput, parseErr := parseConsolidationResponse(escalatedResponse); parseErr == nil {
				aiOutput = escalatedOutput
				model = claudeModel + " (escalated)"
				log.Printf("[Consolidation] %s: Escalated — Claude confidence=%.2f", productID, aiOutput.Signals.Overall)
			}
		}
	}

	// (aiOutput already parsed above — skip second parse)
	_ = response // already consumed

	// 6. aiOutput already parsed above (with optional escalation)

	// 7. Build metadata
	meta := &ConsolidationMeta{
		ProductID:         productID,
		TenantID:          tenantID,
		Signals:           aiOutput.Signals,
		BranchDecisions:   aiOutput.BranchDecisions,
		Conflicts:         aiOutput.Conflicts,
		TotalBranches:     len(sourceBranches),
		ConsolidatedAt:    time.Now(),
		AIModel:           model,
		DurationMS:        time.Since(start).Milliseconds(),
	}

	for _, bd := range aiOutput.BranchDecisions {
		if bd.Decision == "discard" {
			meta.DiscardedBranches++
		} else if bd.Decision == "flag" {
			meta.FlaggedBranches++
		}
	}

	// Review required if: overall confidence < threshold, or flagged branches, or signals indicate issues
	meta.ReviewRequired = aiOutput.Signals.Overall < opts.ConfidenceThreshold ||
		meta.FlaggedBranches > 0 ||
		aiOutput.ReviewRequired

	meta.ReviewReasons = aiOutput.ReviewReasons
	result.Meta = meta
	result.ReviewRequired = meta.ReviewRequired

	// 8. Save consolidated data record
	consolidatedData := aiOutput.Consolidated
	consolidatedData["consolidated_at"] = time.Now().Format(time.RFC3339)
	consolidatedData["branch_count"] = len(sourceBranches)
	consolidatedData["discarded_count"] = meta.DiscardedBranches
	consolidatedData["overall_confidence"] = aiOutput.Signals.Overall
	consolidatedData["review_required"] = meta.ReviewRequired

	consolidated := &models.ExtendedProductData{
		SourceKey: "consolidated",
		ProductID: productID,
		TenantID:  tenantID,
		Source:    "ai_consolidation",
		SourceID:  productID,
		Data:      consolidatedData,
		FetchedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := s.repo.SaveExtendedData(ctx, tenantID, consolidated); err != nil {
		return nil, fmt.Errorf("save consolidated record: %w", err)
	}

	// 9. Save meta record separately (keeps the main record clean)
	metaData := map[string]interface{}{}
	metaBytes, _ := json.Marshal(meta)
	json.Unmarshal(metaBytes, &metaData)

	metaRecord := &models.ExtendedProductData{
		SourceKey: "consolidated_meta",
		ProductID: productID,
		TenantID:  tenantID,
		Source:    "ai_consolidation_meta",
		SourceID:  productID,
		Data:      metaData,
		FetchedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := s.repo.SaveExtendedData(ctx, tenantID, metaRecord); err != nil {
		log.Printf("[Consolidation] Warning: failed to save meta for %s: %v", productID, err)
	}

	result.Consolidated = consolidatedData

	// 10. PIM writeback (conservative — missing fields only)
	if opts.WriteBackToPIM && !meta.ReviewRequired {
		written, err := s.writeBackToPIM(ctx, tenantID, productID, product, consolidatedData, aiOutput.PIMMapping)
		if err != nil {
			log.Printf("[Consolidation] PIM writeback warning for %s: %v", productID, err)
		}
		result.PIMLfieldsSet = written
	}

	log.Printf("[Consolidation] %s: %d branches, %d discarded, confidence=%.2f, review=%v, %dms",
		productID, len(sourceBranches), meta.DiscardedBranches,
		aiOutput.Signals.Overall, meta.ReviewRequired, meta.DurationMS)

	return result, nil
}

// ─── Options ─────────────────────────────────────────────────────────────────

type ConsolidationOptions struct {
	ConfidenceThreshold float64 // default 0.70
	UseImageComparison  bool    // default false (costs more tokens)
	WriteBackToPIM      bool    // default true (conservative mode)
	SkipIfConsolidated  bool    // skip products that already have a "consolidated" branch
	// Model selection — defaults to gemini-2.0-flash (cheap). Override to
	// "claude-sonnet-4-20250514" for high-value catalogues or when auto-escalating
	// after a low-confidence Flash result.
	ConsolidationModel  string  // "gemini-2.0-flash" | "claude-sonnet-4-20250514" | ""=default
	// AutoEscalate: if Gemini confidence < threshold, automatically retry with Claude Sonnet
	AutoEscalate        bool
}

func DefaultConsolidationOptions() ConsolidationOptions {
	return ConsolidationOptions{
		ConfidenceThreshold: 0.70,
		UseImageComparison:  false,
		WriteBackToPIM:      true,
		SkipIfConsolidated:  true,
		ConsolidationModel:  "gemini-2.0-flash", // cheap default
		AutoEscalate:        false,
	}
}

// ─── AutoDraftSettings — stored per tenant in config/settings.ai ─────────────

// AutoDraftSettings controls how consolidation and auto-draft behave for a tenant.
type AutoDraftSettings struct {
	Enabled             bool     `firestore:"auto_draft_enabled" json:"auto_draft_enabled"`
	ConfidenceThreshold float64  `firestore:"confidence_threshold" json:"confidence_threshold"`
	UseImageComparison  bool     `firestore:"use_image_comparison" json:"use_image_comparison"`
	Channels            []string `firestore:"auto_draft_channels" json:"auto_draft_channels"`
	// ConsolidationModel is the AI model used for consolidation.
	// "gemini-2.0-flash"        — cheap, fast, good for most products (~$0.005/product)
	// "gemini-1.5-pro"          — better reasoning, ~$0.02/product
	// "claude-sonnet-4-20250514" — best quality, ~$0.05/product — use sparingly
	ConsolidationModel  string   `firestore:"consolidation_model" json:"consolidation_model"`
	// AutoEscalate: if Gemini confidence < threshold, automatically retry with Claude
	AutoEscalate        bool     `firestore:"auto_escalate" json:"auto_escalate"`
}

func DefaultAutoSettings() AutoDraftSettings {
	return AutoDraftSettings{
		Enabled:             false,
		ConfidenceThreshold: 0.70,
		UseImageComparison:  false,
		Channels:            []string{},
		ConsolidationModel:  "gemini-2.0-flash",
		AutoEscalate:        false,
	}
}

// ─── Prompt builder ───────────────────────────────────────────────────────────

// ebaySchema holds required/recommended aspects fetched from Firestore for
// injection into the consolidation prompt so Gemini knows exactly which fields
// eBay mandates for this category.
type ebayAspectSchema struct {
	Required    []string // aspects where AspectRequired == true
	Recommended []string // aspects where AspectUsage == "RECOMMENDED"
}

// loadEbayCategorySchema reads the pre-synced aspects from Firestore for the
// eBay category that the product belongs to. Returns an empty schema (not an
// error) when no category can be determined or schema isn't synced yet.
func (s *AIConsolidationService) loadEbayCategorySchema(ctx context.Context, branches []models.ExtendedProductData) ebayAspectSchema {
	var schema ebayAspectSchema

	// Dig out categoryID and marketplaceID from the ebay_item_* or ebay_epid_* branch
	categoryID := ""
	marketplaceID := "EBAY_GB"
	for _, b := range branches {
		if strings.HasPrefix(b.SourceKey, "ebay_item_") || strings.HasPrefix(b.SourceKey, "ebay_epid_") {
			if cid, ok := b.Data["category_id"].(string); ok && cid != "" {
				categoryID = cid
			}
			if mid, ok := b.Data["marketplace_id"].(string); ok && mid != "" {
				marketplaceID = mid
			}
			if categoryID != "" {
				break
			}
		}
	}
	if categoryID == "" {
		return schema
	}

	// Path: marketplaces/eBay/{marketplaceID}/data/aspects/{categoryID}
	docRef := s.repo.GetFirestoreClient().
		Collection("marketplaces").Doc("eBay").
		Collection(marketplaceID).Doc("data").
		Collection("aspects").Doc(categoryID)

	snap, err := docRef.Get(ctx)
	if err != nil || !snap.Exists() {
		return schema
	}

	// The doc stores aspects as an array field called "aspects"
	raw := snap.Data()
	aspectsRaw, ok := raw["aspects"].([]interface{})
	if !ok {
		return schema
	}

	for _, a := range aspectsRaw {
		m, ok := a.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := m["localizedAspectName"].(string)
		if name == "" {
			continue
		}
		constraint, _ := m["aspectConstraint"].(map[string]interface{})
		if constraint == nil {
			continue
		}
		required, _ := constraint["aspectRequired"].(bool)
		usage, _ := constraint["aspectUsage"].(string)

		if required {
			schema.Required = append(schema.Required, name)
		} else if usage == "RECOMMENDED" {
			schema.Recommended = append(schema.Recommended, name)
		}
	}

	return schema
}

func (s *AIConsolidationService) buildConsolidationPrompt(
	ctx context.Context,
	product *models.Product,
	branches []models.ExtendedProductData,
	imageURLs []string,
	opts ConsolidationOptions,
) string {

	// Load eBay category schema (required/recommended aspects) from Firestore.
	// This tells Gemini exactly which fields eBay mandates for this category.
	ebaySchema := s.loadEbayCategorySchema(ctx, branches)

	// Serialise product (strip nulls for brevity)
	productSummary := map[string]interface{}{
		"product_id": product.ProductID,
		"title":      product.Title,
		"sku":        product.SKU,
	}
	if product.Brand != nil {
		productSummary["brand"] = *product.Brand
	}
	if product.Description != nil {
		productSummary["description"] = *product.Description
	}
	if product.Identifiers != nil {
		ids := map[string]string{}
		if product.Identifiers.EAN != nil  { ids["ean"] = *product.Identifiers.EAN }
		if product.Identifiers.ASIN != nil { ids["asin"] = *product.Identifiers.ASIN }
		if product.Identifiers.UPC != nil  { ids["upc"] = *product.Identifiers.UPC }
		if len(ids) > 0 { productSummary["identifiers"] = ids }
	}
	if product.Dimensions != nil {
		productSummary["dimensions"] = product.Dimensions
	}
	if product.Weight != nil {
		productSummary["weight"] = product.Weight
	}
	if len(product.Attributes) > 0 {
		productSummary["attributes"] = product.Attributes
	}
	productJSON, _ := json.MarshalIndent(productSummary, "", "  ")

	// Serialise each branch — cap data size per branch to keep prompt manageable
	var branchesJSON []string
	for _, b := range branches {
		entry := map[string]interface{}{
			"source_key": b.SourceKey,
			"source":     b.Source,
			"source_id":  b.SourceID,
			"data":       truncateBranchData(b.Data, 60), // max 60 keys
		}
		j, _ := json.MarshalIndent(entry, "", "  ")
		branchesJSON = append(branchesJSON, string(j))
	}

	var imageSection string
	if opts.UseImageComparison && len(imageURLs) > 0 {
		imageSection = fmt.Sprintf(`
IMAGE URLS FOR COMPARISON (first image is from the seller's own listing, others from cross-listings):
%s

Use these images as an additional identity signal. If images are clearly different product types, 
treat that branch as a discard regardless of EAN match.
`, strings.Join(imageURLs[:minInt(len(imageURLs), 5)], "\n"))
	}

	// Build eBay schema section for prompt injection
	var schemaSection string
	if len(ebaySchema.Required) > 0 || len(ebaySchema.Recommended) > 0 {
		var sb strings.Builder
		sb.WriteString("\n─────────────────────────────────────────────────────────────────\n")
		sb.WriteString("EBAY CATEGORY SCHEMA — fields the listing MUST or SHOULD include:\n")
		if len(ebaySchema.Required) > 0 {
			sb.WriteString(fmt.Sprintf("  REQUIRED (%d): %s\n", len(ebaySchema.Required), strings.Join(ebaySchema.Required, ", ")))
		}
		if len(ebaySchema.Recommended) > 0 {
			sb.WriteString(fmt.Sprintf("  RECOMMENDED (%d): %s\n", len(ebaySchema.Recommended), strings.Join(ebaySchema.Recommended, ", ")))
		}
		sb.WriteString("\nWhen building the consolidated record, specifically attempt to populate ALL required\n")
		sb.WriteString("fields and as many recommended fields as possible from the source branches.\n")
		sb.WriteString("If a required field cannot be determined, include it in review_reasons.\n")
		sb.WriteString("─────────────────────────────────────────────────────────────────\n")
		schemaSection = sb.String()
	}

	return fmt.Sprintf(`You are a product data quality analyst for an e-commerce platform.

You will be given:
1. A product record from the seller's own PIM system (authoritative)
2. Multiple "extended data" branches collected from various marketplace sources for that product

Your job is to:
A) Verify each branch is actually for the same physical product
B) Deduplicate and merge all data into one clean consolidated record
C) Map relevant fields back to the PIM product schema

─────────────────────────────────────────────────────────────────
SELLER'S OWN PRODUCT (this is the ground truth — never discard this):
%s
─────────────────────────────────────────────────────────────────

SOURCE BRANCHES TO EVALUATE (%d branches):
[%s]
%s
%s
─────────────────────────────────────────────────────────────────

VERIFICATION RULES:

1. IDENTITY SIGNALS — evaluate each branch for:
   • title_similarity: Are the titles semantically the same product type? (0.0–1.0)
     - "Bar Stool" vs "Counter Height Stool" → 0.95 (same product, different wording)
     - "Bar Stool" vs "Bathroom Tap" → 0.02 (clearly different product)
   • category_consistency: Do the categories match? (0.0–1.0)
     - Both in "Furniture > Bar Stools" → 1.0
     - One in "Furniture", one in "Plumbing" → 0.0 (hard discard)
   • attribute_consistency: Do key shared attributes agree? (0.0–1.0)
     - Consider: colour, brand, model, material
     - IGNORE dimensions and weight — these are frequently wrong (sellers guess)
     - Flag dimension conflicts but do NOT use them as discard criteria
   • price_plausibility: Is the price within 3× of the seller's own price? (0.0–1.0)
     - Flag outliers but do not discard on price alone
   • image_similarity: (only if images provided) Do images show the same product? (0.0–1.0, -1 if not run)

2. DISCARD CRITERIA (set decision = "discard"):
   • category AND title both fail (scores both below 0.3) → clear wrong product
   • Only one fails → set decision = "flag" and add to review_reasons

3. DEDUPLICATION RULES:
   • When multiple branches have the same field with the same value → keep one
   • When branches disagree on a value:
     - Most common value wins
     - Record the conflict with all values and which was chosen
   • For dimensions/weight: if only one branch has them, include but mark as "unverified"
   • Prefer the seller's own data (source = "ebay" or "amazon") over cross-listing data
   • Prefer canonical catalogue data (source = "ebay_browse_epid" or "amazon_catalog") over individual listings
   • For bullet_points: merge unique points across branches, deduplicate similar ones

4. CONSOLIDATED RECORD — produce a clean, flat key/value map with:
   • title (best version — clear, search-optimised)
   • description (best available)  
   • brand
   • bullet_points (array, deduplicated)
   • key_features (array)
   • material, colour, size (if available and consistent)
   • recommended_age (if available)
   • all item_specifics / localized_aspects merged
   • category_path (best available)
   • epid (eBay product ID, if any branch has one)
   • eans, upcs, gtins (arrays of all unique identifiers found)
   • estimated_sold_quantity (from eBay cross-listings if available)
   • additional_images (array of URLs, deduplicated)
   • Any other relevant product attributes

5. PIM MAPPING — identify which consolidated fields map to PIM schema fields:
   (Only suggest fields that are currently EMPTY/NULL in the seller's product)
   PIM fields available: title, description, brand, key_features, tags, 
   attributes (map), dimensions (length/width/height/unit), weight (value/unit)
   For dimensions/weight: ONLY suggest if the product currently has no dimensions at all.
   Dimensions are often wrong in marketplace data — mark as "needs_verification" if uncertain.

─────────────────────────────────────────────────────────────────

Respond with ONLY a valid JSON object. No markdown, no explanation:

{
  "signals": {
    "title_similarity": 0.0,
    "category_consistency": 0.0,
    "attribute_consistency": 0.0,
    "image_similarity": -1,
    "price_plausibility": 0.0,
    "overall": 0.0
  },
  "branch_decisions": [
    {
      "source_key": "ebay_item_123456",
      "decision": "keep|discard|flag",
      "confidence": 0.95,
      "reason": "optional explanation, required if discard or flag"
    }
  ],
  "conflicts": [
    {
      "field": "material",
      "values": {"ebay_item_123": "plastic", "ebay_ean_xxx_seller1": "ABS plastic"},
      "resolved": "ABS plastic",
      "note": "More specific term preferred"
    }
  ],
  "review_required": false,
  "review_reasons": [],
  "consolidated": {
    "title": "...",
    "description": "...",
    "bullet_points": ["..."],
    "key_features": ["..."]
  },
  "pim_mapping": {
    "description": {"value": "...", "confidence": 0.9, "needs_verification": false},
    "brand": {"value": "...", "confidence": 0.95, "needs_verification": false},
    "key_features": {"value": ["..."], "confidence": 0.85, "needs_verification": false}
  }
}`,
		string(productJSON),
		len(branches),
		strings.Join(branchesJSON, ",\n"),
		imageSection,
		schemaSection,
	)
}

// ─── AI response parser ───────────────────────────────────────────────────────

type consolidationAIResponse struct {
	Signals         ConsolidationSignals    `json:"signals"`
	BranchDecisions []ConsolidatedBranch    `json:"branch_decisions"`
	Conflicts       []ConsolidationConflict `json:"conflicts"`
	ReviewRequired  bool                    `json:"review_required"`
	ReviewReasons   []string                `json:"review_reasons"`
	Consolidated    map[string]interface{}  `json:"consolidated"`
	PIMMapping      map[string]pimField     `json:"pim_mapping"`
}

type pimField struct {
	Value            interface{} `json:"value"`
	Confidence       float64     `json:"confidence"`
	NeedsVerification bool       `json:"needs_verification"`
}

func parseConsolidationResponse(response string) (*consolidationAIResponse, error) {
	var result consolidationAIResponse
	if err := parseJSONFromResponse(response, &result); err != nil {
		return nil, fmt.Errorf("consolidation JSON parse: %w", err)
	}
	if result.Consolidated == nil {
		return nil, fmt.Errorf("AI returned no consolidated record")
	}
	return &result, nil
}

// ─── PIM writeback ────────────────────────────────────────────────────────────

func (s *AIConsolidationService) writeBackToPIM(
	ctx context.Context,
	tenantID, productID string,
	product *models.Product,
	consolidated map[string]interface{},
	pimMapping map[string]pimField,
) ([]string, error) {

	updates := map[string]interface{}{}
	var written []string

	for field, mapped := range pimMapping {
		// Skip low-confidence mappings
		if mapped.Confidence < 0.75 {
			continue
		}

		switch field {
		case "description":
			if product.Description == nil {
				s := fmt.Sprintf("%v", mapped.Value)
				updates["description"] = s
				written = append(written, "description")
			}
		case "brand":
			if product.Brand == nil {
				s := fmt.Sprintf("%v", mapped.Value)
				updates["brand"] = s
				written = append(written, "brand")
			}
		case "key_features":
			if len(product.KeyFeatures) == 0 {
				if arr, ok := toStringSlice(mapped.Value); ok {
					updates["key_features"] = arr
					written = append(written, "key_features")
				}
			}
		case "tags":
			if len(product.Tags) == 0 {
				if arr, ok := toStringSlice(mapped.Value); ok {
					updates["tags"] = arr
					written = append(written, "tags")
				}
			}
		case "dimensions":
			// Only write if product has NO dimensions at all, and mapping is not needs_verification
			if product.Dimensions == nil && !mapped.NeedsVerification {
				if dimMap, ok := mapped.Value.(map[string]interface{}); ok {
					updates["dimensions"] = dimMap
					written = append(written, "dimensions")
				}
			}
		case "weight":
			if product.Weight == nil && !mapped.NeedsVerification {
				if wMap, ok := mapped.Value.(map[string]interface{}); ok {
					updates["weight"] = wMap
					written = append(written, "weight")
				}
			}
		default:
			// Generic attribute writeback — only if not already set
			if product.Attributes == nil || product.Attributes[field] == nil {
				if updates["attributes"] == nil {
					updates["attributes"] = make(map[string]interface{})
				}
				if attrMap, ok := updates["attributes"].(map[string]interface{}); ok {
					attrMap[field] = mapped.Value
					written = append(written, "attributes."+field)
				}
			}
		}
	}

	// Always write back epid and eans if product doesn't have them
	if epid, ok := consolidated["epid"].(string); ok && epid != "" {
		if product.Attributes == nil || product.Attributes["epid"] == nil {
			if updates["attributes"] == nil {
				updates["attributes"] = map[string]interface{}{}
			}
			updates["attributes"].(map[string]interface{})["epid"] = epid
			written = append(written, "attributes.epid")
		}
	}

	if len(updates) == 0 {
		return nil, nil
	}

	updates["updated_at"] = time.Now()
	updates["enriched_at"] = time.Now()

	if err := s.productRepo.UpdateProduct(ctx, tenantID, productID, updates); err != nil {
		return written, fmt.Errorf("update product: %w", err)
	}

	return written, nil
}

// ─── Bulk consolidation ───────────────────────────────────────────────────────

// BulkConsolidate runs consolidation for all enriched products in a tenant.
// Runs as a background goroutine; progress is tracked in the consolidation_jobs collection.
func (s *AIConsolidationService) BulkConsolidate(
	ctx context.Context,
	tenantID string,
	opts ConsolidationOptions,
	jobID string,
	progressFn func(processed, succeeded, failed int, msg string),
) {

	// Find all products that have extended data but no "consolidated" branch yet
	products, err := s.findProductsNeedingConsolidation(ctx, tenantID, opts.SkipIfConsolidated)
	if err != nil {
		log.Printf("[Consolidation] BulkConsolidate: find products failed: %v", err)
		progressFn(0, 0, 0, "failed: "+err.Error())
		return
	}

	if len(products) == 0 {
		progressFn(0, 0, 0, "no products need consolidation")
		return
	}

	succeeded := 0
	failed := 0

	for i, productID := range products {
		msg := fmt.Sprintf("Consolidating %d/%d — %s", i+1, len(products), productID)
		progressFn(i, succeeded, failed, msg)

		_, err := s.ConsolidateProduct(ctx, tenantID, productID, opts)
		if err != nil {
			log.Printf("[Consolidation] Failed %s: %v", productID, err)
			failed++
		} else {
			succeeded++
		}

		// Rate limit: ~1 consolidation per 2 seconds (each is one full Claude call)
		time.Sleep(2 * time.Second)

		if i%5 == 0 {
			progressFn(i+1, succeeded, failed, msg)
		}
	}

	progressFn(len(products), succeeded, failed, "completed")
}

func (s *AIConsolidationService) findProductsNeedingConsolidation(
	ctx context.Context, tenantID string, skipIfConsolidated bool,
) ([]string, error) {
	// Get all products that have at least one extended_data branch
	enrichedProducts := make(map[string]bool)
	consolidatedProducts := make(map[string]bool)

	// CollectionGroup query across all products/{id}/extended_data subcollections.
	// This finds every extended_data doc for this tenant across all products
	// without needing to enumerate products first.
	// Requires Firestore index: (collection_group=extended_data, tenant_id ASC)
	iter := s.repo.GetFirestoreClient().CollectionGroup("extended_data").
		Where("tenant_id", "==", tenantID).
		Documents(ctx)
	defer iter.Stop()

	for {
		doc, err := iter.Next()
		if err != nil {
			break
		}
		data := doc.Data()
		pid, _ := data["product_id"].(string)
		sk, _ := data["source_key"].(string)
		if pid == "" {
			// Derive product_id from the document path: .../products/{pid}/extended_data/{docid}
			pathParts := strings.Split(doc.Ref.Path, "/")
			for i, p := range pathParts {
				if p == "products" && i+1 < len(pathParts) {
					pid = pathParts[i+1]
					break
				}
			}
		}
		if pid == "" {
			continue
		}
		enrichedProducts[pid] = true
		if sk == "consolidated" && skipIfConsolidated {
			consolidatedProducts[pid] = true
		}
	}

	var result []string
	for pid := range enrichedProducts {
		if !consolidatedProducts[pid] {
			result = append(result, pid)
		}
	}
	return result, nil
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// truncateBranchData limits a branch's data map to the most relevant keys
func truncateBranchData(data map[string]interface{}, maxKeys int) map[string]interface{} {
	if len(data) <= maxKeys {
		return data
	}

	// Priority keys to always include
	priority := []string{
		"title", "description", "brand", "material", "colour", "color",
		"recommended_age", "ean", "eans", "upc", "gtin", "epid",
		"category_path", "category_id", "condition",
		"bullet_points", "key_features",
		"price_value", "price_currency",
		"seller_username", "seller_feedback_score",
		"image_url", "estimated_sold_quantity",
		"localized_aspects", "item_specifics",
		"enrichment_phase",
	}

	result := make(map[string]interface{})
	for _, k := range priority {
		if v, ok := data[k]; ok {
			result[k] = v
		}
	}

	// Fill remaining slots with other keys
	for k, v := range data {
		if len(result) >= maxKeys {
			break
		}
		if _, already := result[k]; !already {
			result[k] = v
		}
	}
	return result
}

func toStringSlice(v interface{}) ([]string, bool) {
	switch arr := v.(type) {
	case []interface{}:
		result := make([]string, 0, len(arr))
		for _, item := range arr {
			result = append(result, fmt.Sprintf("%v", item))
		}
		return result, true
	case []string:
		return arr, true
	}
	return nil, false
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
