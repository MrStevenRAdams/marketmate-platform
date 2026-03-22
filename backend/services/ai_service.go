package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// ============================================================================
// AI SERVICE — Hybrid: Gemini (mapping) + Claude (content generation)
// ============================================================================
// This service provides AI-powered listing generation using a two-phase approach:
//
// Phase 1 (Gemini Flash): Structured field mapping
//   - Maps PIM product data → marketplace-specific fields
//   - Category suggestion, attribute population
//   - Fast and cheap (~$0.10/1M tokens)
//
// Phase 2 (Claude Sonnet): Creative content generation
//   - Optimised titles, compelling descriptions, SEO bullet points
//   - Marketplace-aware tone and formatting
//   - Higher quality output (~$3/$15 per 1M tokens)
// ============================================================================

type AIService struct {
	geminiAPIKey   string
	geminiModel    string
	claudeAPIKey   string
	claudeModel    string
	httpClient     *http.Client
}

func NewAIService() *AIService {
	geminiKey := os.Getenv("GEMINI_API_KEY")
	claudeKey := os.Getenv("CLAUDE_API_KEY")

	geminiModel := os.Getenv("GEMINI_MODEL")
	if geminiModel == "" {
		geminiModel = "gemini-2.0-flash"
	}

	claudeModel := os.Getenv("CLAUDE_MODEL")
	if claudeModel == "" {
		claudeModel = "claude-sonnet-4-20250514"
	}

	if geminiKey == "" {
		log.Println("⚠️  GEMINI_API_KEY not set — AI mapping will be unavailable")
	}
	if claudeKey == "" {
		log.Println("⚠️  CLAUDE_API_KEY not set — AI content generation will be unavailable")
	}

	return &AIService{
		geminiAPIKey: geminiKey,
		geminiModel:  geminiModel,
		claudeAPIKey: claudeKey,
		claudeModel:  claudeModel,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// IsAvailable returns true if at least one AI provider is configured
func (s *AIService) IsAvailable() bool {
	return s.geminiAPIKey != "" || s.claudeAPIKey != ""
}

// HasGemini returns true if Gemini is configured
func (s *AIService) HasGemini() bool {
	return s.geminiAPIKey != ""
}

// HasClaude returns true if Claude is configured
func (s *AIService) HasClaude() bool {
	return s.claudeAPIKey != ""
}

// ============================================================================
// LISTING GENERATION DATA STRUCTURES
// ============================================================================

// AIProductInput is the product data sent to AI for listing generation
type AIProductInput struct {
	Title           string                 `json:"title"`
	Description     string                 `json:"description,omitempty"`
	Brand           string                 `json:"brand,omitempty"`
	SKU             string                 `json:"sku,omitempty"`
	KeyFeatures     []string               `json:"key_features,omitempty"`
	Categories      []string               `json:"categories,omitempty"`
	Tags            []string               `json:"tags,omitempty"`
	Attributes      map[string]interface{} `json:"attributes,omitempty"`
	Identifiers     map[string]string      `json:"identifiers,omitempty"`
	Dimensions      map[string]interface{} `json:"dimensions,omitempty"`
	Weight          map[string]interface{} `json:"weight,omitempty"`
	ImageURLs       []string               `json:"image_urls,omitempty"`
	EnrichedData    map[string]interface{} `json:"enriched_data,omitempty"`
	SourcePrice     float64                `json:"source_price,omitempty"`
	SourceCurrency  string                 `json:"source_currency,omitempty"`
}

// MarketplaceSchemaField represents a single attribute field from a marketplace schema
type MarketplaceSchemaField struct {
	Name          string   `json:"name"`
	DisplayName   string   `json:"display_name,omitempty"`
	DataType      string   `json:"data_type"` // string, number, enum, boolean
	Required      bool     `json:"required"`
	AllowedValues []string `json:"allowed_values,omitempty"` // for enum types
	MaxLength     int      `json:"max_length,omitempty"`
	Description   string   `json:"description,omitempty"`
}

// MarketplaceSchemaInput is the resolved schema for a specific marketplace + category
type MarketplaceSchemaInput struct {
	Channel      string                   `json:"channel"`
	CategoryID   string                   `json:"category_id"`
	CategoryName string                   `json:"category_name"`
	CategoryPath []string                 `json:"category_path,omitempty"`
	Fields       []MarketplaceSchemaField `json:"fields"`
}

// AIListingOutput is the generated listing content for a single marketplace
type AIListingOutput struct {
	Channel         string                 `json:"channel"`
	Title           string                 `json:"title"`
	Description     string                 `json:"description"`
	BulletPoints    []string               `json:"bullet_points,omitempty"`
	CategoryID      string                 `json:"category_id,omitempty"`
	CategoryName    string                 `json:"category_name,omitempty"`
	Attributes      map[string]interface{} `json:"attributes,omitempty"`
	SearchTerms     []string               `json:"search_terms,omitempty"`
	SuggestedPrice  float64                `json:"suggested_price,omitempty"`
	Condition       string                 `json:"condition,omitempty"`
	Confidence      float64                `json:"confidence"`
	Warnings        []string               `json:"warnings,omitempty"`
}

// AIGenerationResult holds the full result for one product across all channels
type AIGenerationResult struct {
	ProductID  string            `json:"product_id"`
	Listings   []AIListingOutput `json:"listings"`
	Error      string            `json:"error,omitempty"`
	DurationMS int64             `json:"duration_ms"`
}

// ============================================================================
// PHASE 1: GEMINI — STRUCTURED FIELD MAPPING
// ============================================================================

func (s *AIService) MapProductToMarketplaces(ctx context.Context, product AIProductInput, channels []string) ([]AIListingOutput, error) {
	if !s.HasGemini() {
		// Fallback: use Claude for everything
		return s.generateWithClaude(ctx, product, channels, "mapping")
	}

	prompt := s.buildMappingPrompt(product, channels)
	response, err := s.callGemini(ctx, prompt)
	if err != nil {
		log.Printf("[AI] Gemini mapping failed, falling back to Claude: %v", err)
		if s.HasClaude() {
			return s.generateWithClaude(ctx, product, channels, "mapping")
		}
		return nil, fmt.Errorf("gemini mapping failed and claude unavailable: %w", err)
	}

	var outputs []AIListingOutput
	if err := parseJSONFromResponse(response, &outputs); err != nil {
		return nil, fmt.Errorf("parse gemini mapping response: %w", err)
	}

	return outputs, nil
}

func (s *AIService) buildMappingPrompt(product AIProductInput, channels []string) string {
	productJSON, _ := json.MarshalIndent(product, "", "  ")

	channelSpecs := buildChannelSpecifications(channels)

	return fmt.Sprintf(`You are a marketplace listing data mapper. Given a product from a PIM system, map its data to the correct fields for each target marketplace.

PRODUCT DATA (this is the ONLY source of truth — do not invent any facts beyond what is stated here):
%s

TARGET MARKETPLACES: %s

%s

RULES:
- Map ONLY existing product data to each marketplace's required fields. Do not fabricate any product facts.
- Suggest the most appropriate category/product type for each marketplace based on what the product IS.
- Preserve all identifiers (EAN, UPC, ASIN, GTIN) exactly as provided.
- If a required field has no corresponding data in the product, set it to null and add a warning. Do NOT guess or assume values.
- Set confidence score 0.0-1.0 based on how complete the source data is (lower if many fields are missing).
- For price, use the source_price if available, otherwise null.
- For title: restructure existing facts for search visibility. Do not add features, materials, or claims not in the data.
- For description: reword existing information for clarity. Do not add unverified claims.
- For bullet points: extract stated facts only. If fewer than 5 facts exist, return fewer bullet points.
- For attributes: only populate with values directly supported by the product data.

Respond with ONLY a valid JSON array of listing objects. No markdown, no explanation, just JSON:
[
  {
    "channel": "marketplace_name",
    "title": "mapped title using only stated facts",
    "description": "factual description from product data",
    "bullet_points": ["stated fact 1", "stated fact 2"],
    "category_id": "suggested category ID or path",
    "category_name": "human readable category name",
    "attributes": {"key": "value from data or null"},
    "search_terms": ["term1", "term2"],
    "suggested_price": 0.00,
    "condition": "new",
    "confidence": 0.85,
    "warnings": ["any missing or unpopulated required fields"]
  }
]`, string(productJSON), strings.Join(channels, ", "), channelSpecs)
}

// ============================================================================
// PHASE 2: CLAUDE — CREATIVE CONTENT GENERATION
// ============================================================================

func (s *AIService) EnhanceListingContent(ctx context.Context, product AIProductInput, mappedListings []AIListingOutput) ([]AIListingOutput, error) {
	if !s.HasClaude() {
		// If no Claude, return mappings as-is (Gemini output is functional, just not optimised)
		log.Println("[AI] Claude not available — returning Gemini mappings without content enhancement")
		return mappedListings, nil
	}

	prompt := s.buildContentPrompt(product, mappedListings)
	response, err := s.callClaude(ctx, prompt)
	if err != nil {
		log.Printf("[AI] Claude content enhancement failed, using raw mappings: %v", err)
		return mappedListings, nil // Graceful degradation
	}

	var enhanced []AIListingOutput
	if err := parseJSONFromResponse(response, &enhanced); err != nil {
		log.Printf("[AI] Failed to parse Claude response, using raw mappings: %v", err)
		return mappedListings, nil
	}

	return enhanced, nil
}

func (s *AIService) buildContentPrompt(product AIProductInput, mappedListings []AIListingOutput) string {
	productJSON, _ := json.MarshalIndent(product, "", "  ")
	mappingsJSON, _ := json.MarshalIndent(mappedListings, "", "  ")

	return fmt.Sprintf(`You are an e-commerce listing optimiser. You will be given a product's source data and its initial marketplace field mappings. Your job is to improve the title, description, bullet points and search terms for better search visibility and conversion.

PRODUCT DATA (this is the ONLY source of truth):
%s

INITIAL MAPPINGS (from automated field mapping):
%s

CRITICAL RULES — READ CAREFULLY:

1. NEVER invent, fabricate, or assume ANY product facts that are not explicitly present in the PRODUCT DATA above. This includes:
   - Materials, ingredients, or composition
   - Dimensions, weight, or measurements
   - Features, capabilities, or functions
   - Age ranges, certifications, or safety ratings
   - Country of origin or manufacturing details
   - Compatibility or suitability claims
   If a fact is not in the product data, DO NOT include it in your output.

2. You MAY rephrase, restructure, and reword existing facts for better readability and keyword coverage. For example:
   - "Crocodile figure" → "Realistic Crocodile Animal Figure" (adding relevant search keywords)
   - Reordering information to front-load important keywords in titles
   - Breaking a long description into concise bullet points using only stated facts

3. You MAY add generic category-appropriate search terms that describe what the product IS (e.g. "toy", "figure", "collectible") but NEVER terms that imply specific features not in the data (e.g. "waterproof", "organic", "BPA-free").

4. Keep all field mappings, category IDs, identifiers, prices and attributes from the initial mapping unchanged. Only modify: title, description, bullet_points, search_terms.

5. If the product data is sparse or missing key details, keep the listing simple and factual. Add a warning like "Limited source data — listing may need manual review" rather than filling gaps with assumptions.

ENHANCEMENT GUIDELINES:
- Title: Restructure for search — front-load brand and product type, include key differentiators that ARE in the data. Respect marketplace character limits (Amazon: 200, eBay: 80, Temu: 120).
- Description: Rewrite for clarity and scannability using ONLY facts from the product data. Do not add claims.
- Bullet Points: Extract the most important STATED facts into 5 concise points. If fewer than 5 facts exist, use fewer bullet points rather than inventing content.
- Search Terms: Use synonyms and related category terms for what the product IS. Never include terms for features not evidenced in the data.

Respond with ONLY a valid JSON array matching the input structure. No markdown, no explanation:`, string(productJSON), string(mappingsJSON))
}

// ============================================================================
// COMBINED GENERATION: Map + Enhance in one call
// ============================================================================

// GenerateListings performs the full two-phase generation for a single product
func (s *AIService) GenerateListings(ctx context.Context, product AIProductInput, channels []string) (*AIGenerationResult, error) {
	start := time.Now()
	result := &AIGenerationResult{ProductID: product.SKU}

	// Phase 1: Map product to marketplace fields
	mappedListings, err := s.MapProductToMarketplaces(ctx, product, channels)
	if err != nil {
		result.Error = fmt.Sprintf("mapping failed: %v", err)
		result.DurationMS = time.Since(start).Milliseconds()
		return result, err
	}

	// Phase 2: Enhance content with Claude
	enhancedListings, err := s.EnhanceListingContent(ctx, product, mappedListings)
	if err != nil {
		// Non-fatal — use mapped listings without enhancement
		log.Printf("[AI] Content enhancement failed for %s: %v", product.SKU, err)
		enhancedListings = mappedListings
	}

	result.Listings = enhancedListings
	result.DurationMS = time.Since(start).Milliseconds()
	return result, nil
}

// GenerateListingsSinglePhase uses only one AI call when only one provider is available
// or for simple products where two phases would be overkill
func (s *AIService) GenerateListingsSinglePhase(ctx context.Context, product AIProductInput, channels []string) (*AIGenerationResult, error) {
	start := time.Now()
	result := &AIGenerationResult{ProductID: product.SKU}

	var listings []AIListingOutput
	var err error

	if s.HasClaude() {
		listings, err = s.generateWithClaude(ctx, product, channels, "full")
	} else if s.HasGemini() {
		listings, err = s.MapProductToMarketplaces(ctx, product, channels)
	} else {
		return nil, fmt.Errorf("no AI provider configured")
	}

	if err != nil {
		result.Error = err.Error()
		result.DurationMS = time.Since(start).Milliseconds()
		return result, err
	}

	result.Listings = listings
	result.DurationMS = time.Since(start).Milliseconds()
	return result, nil
}

// ============================================================================
// SCHEMA-AWARE GENERATION: Uses marketplace-resolved category + schema
// ============================================================================

// GenerateWithSchema performs AI generation with full knowledge of the marketplace
// schema (attribute names, types, required fields, allowed values). This produces
// much more accurate results because the AI fills in the EXACT fields the
// marketplace requires rather than guessing.
func (s *AIService) GenerateWithSchema(ctx context.Context, product AIProductInput, schema MarketplaceSchemaInput) (*AIGenerationResult, error) {
	start := time.Now()
	result := &AIGenerationResult{ProductID: product.SKU}

	prompt := s.buildSchemaAwarePrompt(product, schema)

	var response string
	var err error

	// Use Claude for schema-aware generation (better at structured output)
	if s.HasClaude() {
		response, err = s.callClaude(ctx, prompt)
	} else if s.HasGemini() {
		response, err = s.callGemini(ctx, prompt)
	} else {
		return nil, fmt.Errorf("no AI provider configured")
	}

	if err != nil {
		result.Error = fmt.Sprintf("schema-aware generation failed: %v", err)
		result.DurationMS = time.Since(start).Milliseconds()
		return result, err
	}

	var output AIListingOutput
	// Try parsing as single object first (since we're generating for one channel)
	if err := parseJSONFromResponse(response, &output); err != nil {
		// Try as array
		var outputs []AIListingOutput
		if err2 := parseJSONFromResponse(response, &outputs); err2 != nil {
			result.Error = fmt.Sprintf("parse AI response: %v / %v", err, err2)
			result.DurationMS = time.Since(start).Milliseconds()
			return result, fmt.Errorf("parse AI response: %w", err)
		}
		if len(outputs) > 0 {
			output = outputs[0]
		}
	}

	// Ensure channel is set
	if output.Channel == "" {
		output.Channel = schema.Channel
	}
	// Carry over category from schema
	if output.CategoryID == "" {
		output.CategoryID = schema.CategoryID
	}
	if output.CategoryName == "" {
		output.CategoryName = schema.CategoryName
	}

	result.Listings = []AIListingOutput{output}
	result.DurationMS = time.Since(start).Milliseconds()
	return result, nil
}

func (s *AIService) buildSchemaAwarePrompt(product AIProductInput, schema MarketplaceSchemaInput) string {
	productJSON, _ := json.MarshalIndent(product, "", "  ")

	// Build the attribute fields list for the prompt
	var requiredFields []string
	var optionalFields []string
	for _, f := range schema.Fields {
		fieldDesc := f.Name
		if f.DisplayName != "" && f.DisplayName != f.Name {
			fieldDesc += fmt.Sprintf(" (%s)", f.DisplayName)
		}
		if f.DataType != "" {
			fieldDesc += fmt.Sprintf(" [%s]", f.DataType)
		}
		if len(f.AllowedValues) > 0 {
			if len(f.AllowedValues) <= 10 {
				fieldDesc += fmt.Sprintf(" values: %s", strings.Join(f.AllowedValues, ", "))
			} else {
				fieldDesc += fmt.Sprintf(" values: %s, ... (%d total)", strings.Join(f.AllowedValues[:10], ", "), len(f.AllowedValues))
			}
		}
		if f.MaxLength > 0 {
			fieldDesc += fmt.Sprintf(" max:%d chars", f.MaxLength)
		}
		if f.Required {
			requiredFields = append(requiredFields, "  - "+fieldDesc)
		} else {
			optionalFields = append(optionalFields, "  - "+fieldDesc)
		}
	}

	// Limit optional fields shown (they can be hundreds)
	if len(optionalFields) > 30 {
		optionalFields = append(optionalFields[:30], fmt.Sprintf("  ... and %d more optional fields", len(optionalFields)-30))
	}

	channelRules := buildChannelSpecifications([]string{schema.Channel})

	return fmt.Sprintf(`You are an e-commerce listing creator. You will be given a product's source data and the EXACT marketplace schema (category + attribute fields) this product will be listed under. Your job is to create an accurate, search-optimised listing using ONLY facts present in the product data.

PRODUCT DATA (this is the ONLY source of truth):
%s

MARKETPLACE: %s
CATEGORY: %s (ID: %s)
%s

REQUIRED ATTRIBUTE FIELDS (populate ONLY if the answer is clearly evidenced in the product data — set to null otherwise):
%s

OPTIONAL ATTRIBUTE FIELDS (populate ONLY where the product data provides a clear answer):
%s

%s

CRITICAL RULES — FACTUAL ACCURACY:

1. ZERO HALLUCINATION POLICY: Every fact in the title, description, bullet points, and attributes MUST come directly from the PRODUCT DATA above. If a fact is not present in the data, you MUST NOT include it anywhere in the listing.

2. DO NOT fabricate or assume:
   - Materials, ingredients, or composition (e.g. don't say "plastic" unless the data says so)
   - Dimensions, weight, or size (e.g. don't say "compact" unless dimensions confirm it)
   - Features or capabilities (e.g. don't say "waterproof", "durable", "eco-friendly")
   - Age suitability, safety certifications, or ratings
   - Country of origin, manufacturer details
   - Compatibility with other products
   - Any subjective quality claims not supported by the data

3. YOU MAY:
   - Rephrase and restructure existing facts for better keyword coverage and readability
   - Add generic category terms to the title (e.g. "Figure", "Toy", "Set") that accurately describe WHAT the product is
   - Reorder information to front-load important search keywords
   - Use synonyms for stated facts (e.g. "kids" for "children" if age data exists)
   - Add factual search terms based on the product category and brand

4. ATTRIBUTE RULES:
   - For enum fields with allowed values: select the best match ONLY if the product data supports it. If uncertain, set to null.
   - For text fields: use only information from the product data.
   - For numeric fields: use exact values from the data. Do not estimate or round.
   - If a required field cannot be answered from the product data, set it to null and add a warning.

5. SPARSE DATA HANDLING:
   - If the product data has limited information, create a shorter listing with fewer bullet points rather than padding with assumptions.
   - Bullet points should only contain stated facts. If only 2-3 facts exist, return 2-3 bullet points, not 5.
   - Add a warning: "Limited source data — some required fields could not be populated"

6. SEARCH TERMS:
   - Include the brand name, product type, and category-appropriate synonyms
   - Include terms for what the product IS, not what it might do
   - Never include feature-based terms unless that feature is explicitly in the data

Respond with ONLY a valid JSON object (not an array). No markdown, no explanation:
{
  "channel": "%s",
  "title": "factual search-optimised title",
  "description": "factual product description using only stated information",
  "bullet_points": ["stated fact 1", "stated fact 2"],
  "category_id": "%s",
  "category_name": "%s",
  "attributes": {"field_name": "value from data or null"},
  "search_terms": ["brand", "product type", "category terms"],
  "condition": "new",
  "confidence": 0.85,
  "warnings": ["list any required fields that could not be populated"]
}`,
		string(productJSON),
		strings.ToUpper(schema.Channel),
		schema.CategoryName, schema.CategoryID,
		func() string {
			if len(schema.CategoryPath) > 0 {
				return "CATEGORY PATH: " + strings.Join(schema.CategoryPath, " > ")
			}
			return ""
		}(),
		strings.Join(requiredFields, "\n"),
		strings.Join(optionalFields, "\n"),
		channelRules,
		schema.Channel,
		schema.CategoryID,
		schema.CategoryName,
	)
}

func (s *AIService) callClaude(ctx context.Context, prompt string) (string, error) {
	body := map[string]interface{}{
		"model":      s.claudeModel,
		"max_tokens": 4096,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal claude request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("create claude request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", s.claudeAPIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("claude API call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read claude response: %w", err)
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("claude API error %d: %s", resp.StatusCode, string(respBody))
	}

	var claudeResp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(respBody, &claudeResp); err != nil {
		return "", fmt.Errorf("parse claude response: %w", err)
	}

	for _, block := range claudeResp.Content {
		if block.Type == "text" {
			return block.Text, nil
		}
	}

	return "", fmt.Errorf("no text content in claude response")
}

// generateWithClaude is the fallback when Gemini isn't available
func (s *AIService) generateWithClaude(ctx context.Context, product AIProductInput, channels []string, mode string) ([]AIListingOutput, error) {
	var prompt string
	if mode == "mapping" {
		prompt = s.buildMappingPrompt(product, channels)
	} else {
		// Full combined prompt
		prompt = s.buildFullGenerationPrompt(product, channels)
	}

	response, err := s.callClaude(ctx, prompt)
	if err != nil {
		return nil, err
	}

	var outputs []AIListingOutput
	if err := parseJSONFromResponse(response, &outputs); err != nil {
		return nil, fmt.Errorf("parse claude response: %w", err)
	}
	return outputs, nil
}

func (s *AIService) buildFullGenerationPrompt(product AIProductInput, channels []string) string {
	productJSON, _ := json.MarshalIndent(product, "", "  ")
	channelSpecs := buildChannelSpecifications(channels)

	return fmt.Sprintf(`You are an e-commerce listing creator. Given a product from a PIM system, create marketplace listings for each target channel.

PRODUCT DATA (this is the ONLY source of truth — do not invent any facts beyond what is stated here):
%s

TARGET MARKETPLACES: %s

%s

CRITICAL RULES — FACTUAL ACCURACY:

1. EVERY claim in the title, description, and bullet points MUST be directly traceable to a fact in the PRODUCT DATA above. If a fact is not present, DO NOT include it.
2. NEVER fabricate materials, dimensions, features, capabilities, certifications, age ranges, safety claims, compatibility, or any other product specification.
3. You MAY rephrase and restructure existing facts for better search visibility and readability.
4. You MAY add generic category search terms (e.g. "toy", "figure", "gift idea") but NEVER terms implying unverified features.
5. If the product data is limited, create a shorter, factual listing rather than a longer one padded with assumptions. Add a warning: "Limited source data — manual review recommended."
6. For attributes, only populate fields where the product data provides a clear answer. Leave others as null and add a warning.

For each marketplace, generate:
1. Title: Restructured for search keywords using ONLY stated facts. Respect character limits.
2. Description: Clear, factual product description. No invented claims.
3. Bullet Points: Key facts from the data. Use fewer than 5 if insufficient facts exist.
4. Category: Best-fit category name/path based on what the product IS.
5. Attributes: Mapped from product data only. Null for unknown fields.
6. Search Terms: Category-appropriate keywords and synonyms. No unverified feature terms.
7. Condition: Default "new" unless stated otherwise.
8. Confidence: 0.0-1.0 based on how complete the source data is.
9. Warnings: Flag any required fields that could not be populated from the data.

Respond with ONLY a valid JSON array. No markdown fences, no explanation:
[
  {
    "channel": "marketplace_name",
    "title": "factual optimised title",
    "description": "factual description",
    "bullet_points": ["stated fact 1", "stated fact 2"],
    "category_id": "category path or ID",
    "category_name": "readable category",
    "attributes": {},
    "search_terms": ["keyword1", "keyword2"],
    "suggested_price": 0.00,
    "condition": "new",
    "confidence": 0.85,
    "warnings": []
  }
]`, string(productJSON), strings.Join(channels, ", "), channelSpecs)
}

// ============================================================================
// GEMINI API CALL
// ============================================================================

func (s *AIService) callGemini(ctx context.Context, prompt string) (string, error) {
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s",
		s.geminiModel, s.geminiAPIKey)

	body := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"parts": []map[string]string{
					{"text": prompt},
				},
			},
		},
		"generationConfig": map[string]interface{}{
			"temperature":     0.3,
			"maxOutputTokens": 4096,
			"responseMimeType": "application/json",
		},
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal gemini request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("create gemini request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("gemini API call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read gemini response: %w", err)
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("gemini API error %d: %s", resp.StatusCode, string(respBody))
	}

	var geminiResp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(respBody, &geminiResp); err != nil {
		return "", fmt.Errorf("parse gemini response: %w", err)
	}

	if len(geminiResp.Candidates) > 0 && len(geminiResp.Candidates[0].Content.Parts) > 0 {
		return geminiResp.Candidates[0].Content.Parts[0].Text, nil
	}

	return "", fmt.Errorf("no content in gemini response")
}

// ============================================================================
// HELPERS
// ============================================================================

// parseJSONFromResponse extracts JSON from an AI response that may contain markdown fences
func parseJSONFromResponse(response string, target interface{}) error {
	// Strip markdown code fences if present
	cleaned := strings.TrimSpace(response)
	if strings.HasPrefix(cleaned, "```json") {
		cleaned = strings.TrimPrefix(cleaned, "```json")
		cleaned = strings.TrimSuffix(cleaned, "```")
		cleaned = strings.TrimSpace(cleaned)
	} else if strings.HasPrefix(cleaned, "```") {
		cleaned = strings.TrimPrefix(cleaned, "```")
		cleaned = strings.TrimSuffix(cleaned, "```")
		cleaned = strings.TrimSpace(cleaned)
	}

	if err := json.Unmarshal([]byte(cleaned), target); err != nil {
		// Try to find JSON array in the response
		start := strings.Index(cleaned, "[")
		end := strings.LastIndex(cleaned, "]")
		if start >= 0 && end > start {
			subset := cleaned[start : end+1]
			if err2 := json.Unmarshal([]byte(subset), target); err2 != nil {
				return fmt.Errorf("JSON parse failed: %w (original: %w)", err2, err)
			}
			return nil
		}
		return fmt.Errorf("no valid JSON found in response: %w", err)
	}
	return nil
}

// buildChannelSpecifications returns marketplace-specific rules for the prompt
func buildChannelSpecifications(channels []string) string {
	specs := make([]string, 0, len(channels))

	for _, ch := range channels {
		switch strings.ToLower(ch) {
		case "amazon":
			specs = append(specs, `AMAZON REQUIREMENTS:
- Title: Max 200 characters. Format: Brand + Product Type + Key Feature + Size/Color
- Description: HTML supported. Include product details, materials, care instructions
- Bullet Points: Exactly 5. Max 500 chars each. Start with CAPS keyword
- Required fields: item_name, brand, product_description, bullet_point, condition_type
- Category: Use Amazon product type/browse node path
- Identifiers: ASIN, EAN, UPC, or GTIN required`)

		case "ebay":
			specs = append(specs, `EBAY REQUIREMENTS:
- Title: Max 80 characters. Clear, descriptive, include brand and key specs
- Description: HTML supported. Include condition details and specifications
- Item Specifics: Brand, MPN, Type, Material, Color, Size as applicable
- Required fields: title, category, condition, format (FixedPrice/Auction)
- Category: Use eBay category ID or descriptive path
- Identifiers: EAN, UPC, or ISBN recommended`)

		case "temu":
			specs = append(specs, `TEMU REQUIREMENTS:
- Title: Max 120 characters. Feature-focused, clear product naming
- Description: Plain text only, no HTML. Concise and feature-focused
- Bullet Points: 3-5 key features
- Required fields: goodsName, catId, skuList with price
- Category: Use Temu category ID from their taxonomy
- Identifiers: Brand trademark if registered`)

		case "shopify":
			specs = append(specs, `SHOPIFY REQUIREMENTS:
- Title: SEO-friendly, natural language, no character limit but keep concise
- Description: HTML supported. Rich product description with features and benefits
- Tags: Comma-separated product tags for collections/search
- Required fields: title, body_html, vendor, product_type
- Category: Use Shopify product_type string
- Variants: Include if product has size/color options`)

		default:
			specs = append(specs, fmt.Sprintf(`%s: Use standard e-commerce listing fields (title, description, category, attributes)`, strings.ToUpper(ch)))
		}
	}

	return strings.Join(specs, "\n\n")
}

// GenerateText sends a raw prompt to the best available AI provider and
// returns the plain text response. Used by the template AI content modal.
func (s *AIService) GenerateText(ctx context.Context, prompt string) (string, error) {
	if s.HasGemini() {
		return s.callGemini(ctx, prompt)
	}
	if s.HasClaude() {
		return s.callClaude(ctx, prompt)
	}
	return "", fmt.Errorf("no AI provider configured")
}

// CallWithModel sends a prompt to a specific model by name.
// Gemini models: "gemini-2.0-flash", "gemini-1.5-pro", "gemini-2.0-flash-thinking-exp"
// Claude models:  "claude-haiku-4-5-20251001", "claude-sonnet-4-20250514", "claude-opus-4-20250514"
// Falls back to the best available provider if the requested model is unavailable.
func (s *AIService) CallWithModel(ctx context.Context, prompt, model string) (string, error) {
	isGemini := strings.HasPrefix(model, "gemini-")
	isClaude  := strings.HasPrefix(model, "claude-")

	if isGemini {
		if !s.HasGemini() {
			// Gemini not configured — fall back to Claude if available
			if s.HasClaude() {
				log.Printf("[AI] Gemini not configured, falling back to Claude for model %s", model)
				return s.callClaude(ctx, prompt)
			}
			return "", fmt.Errorf("model %s requested but GEMINI_API_KEY not set", model)
		}
		return s.callGeminiWithModel(ctx, prompt, model)
	}

	if isClaude {
		if !s.HasClaude() {
			// Claude not configured — fall back to Gemini if available
			if s.HasGemini() {
				log.Printf("[AI] Claude not configured, falling back to Gemini for model %s", model)
				return s.callGemini(ctx, prompt)
			}
			return "", fmt.Errorf("model %s requested but CLAUDE_API_KEY not set", model)
		}
		return s.callClaudeWithModel(ctx, prompt, model)
	}

	// Unknown model name — use best available
	log.Printf("[AI] Unknown model %q, using best available provider", model)
	return s.GenerateText(ctx, prompt)
}

// callGeminiWithModel calls Gemini with a specific model name
func (s *AIService) callGeminiWithModel(ctx context.Context, prompt, model string) (string, error) {
	// Temporarily swap model, call, restore
	original := s.geminiModel
	s.geminiModel = model
	resp, err := s.callGemini(ctx, prompt)
	s.geminiModel = original
	return resp, err
}

// callClaudeWithModel calls Claude with a specific model name
func (s *AIService) callClaudeWithModel(ctx context.Context, prompt, model string) (string, error) {
	original := s.claudeModel
	s.claudeModel = model
	resp, err := s.callClaude(ctx, prompt)
	s.claudeModel = original
	return resp, err
}

// GetModelCost returns an approximate cost estimate string for a model (for logging/UI)
func GetModelCost(model string) string {
	costs := map[string]string{
		"gemini-2.0-flash":             "~$0.005/product",
		"gemini-1.5-flash":             "~$0.003/product",
		"gemini-1.5-pro":               "~$0.020/product",
		"gemini-2.0-flash-thinking-exp":"~$0.010/product",
		"claude-haiku-4-5-20251001":    "~$0.010/product",
		"claude-sonnet-4-20250514":     "~$0.055/product",
		"claude-opus-4-20250514":       "~$0.200/product",
	}
	if c, ok := costs[model]; ok {
		return c
	}
	return "cost unknown"
}
