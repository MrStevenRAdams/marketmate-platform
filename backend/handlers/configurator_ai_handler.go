package handlers

// ============================================================================
// CONFIGURATOR AI SETUP HANDLER — SESSION F (USP-01)
// ============================================================================
// Endpoint:
//   POST /configurators/ai-setup
//     Request:  { channel, product_description, credential_id? }
//     Response: { ok, suggestion: { category_id, category_path,
//                                   attribute_defaults[], shipping_defaults{} } }
//
// The handler sends a structured prompt to the best available AI provider
// (Claude → Gemini fallback) and parses the JSON response into a suggestion
// object that the frontend wizard pre-populates into a new configurator form.
// ============================================================================

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"module-a/models"
	"module-a/repository"
	"module-a/services"
)

// ── Types ────────────────────────────────────────────────────────────────────

type ConfiguratorAISetupRequest struct {
	Channel            string `json:"channel" binding:"required"`
	ProductDescription string `json:"product_description" binding:"required"`
	CredentialID       string `json:"credential_id"`
}

// AIConfiguratorSuggestion is returned to the frontend wizard.
type AIConfiguratorSuggestion struct {
	CategoryID       string                 `json:"category_id"`
	CategoryPath     string                 `json:"category_path"`
	AttributeDefaults []models.AttributeDefault `json:"attribute_defaults"`
	ShippingDefaults  map[string]string      `json:"shipping_defaults"`
	Reasoning        string                 `json:"reasoning"`
}

// ── Handler ──────────────────────────────────────────────────────────────────

type ConfiguratorAIHandler struct {
	aiService   *services.AIService
	cfgService  *services.ConfiguratorService
	firestoreRepo *repository.FirestoreRepository
}

func NewConfiguratorAIHandler(
	aiService *services.AIService,
	cfgService *services.ConfiguratorService,
	firestoreRepo *repository.FirestoreRepository,
) *ConfiguratorAIHandler {
	return &ConfiguratorAIHandler{
		aiService:     aiService,
		cfgService:    cfgService,
		firestoreRepo: firestoreRepo,
	}
}

// ── POST /configurators/ai-setup ─────────────────────────────────────────────

func (h *ConfiguratorAIHandler) AISetup(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "tenant ID required"})
		return
	}

	if !h.aiService.IsAvailable() {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"ok":    false,
			"error": "no AI provider configured — add CLAUDE_API_KEY or GEMINI_API_KEY to environment",
		})
		return
	}

	var req ConfiguratorAISetupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	prompt := h.buildSetupPrompt(req.Channel, req.ProductDescription)

	raw, err := h.aiService.GenerateText(c.Request.Context(), prompt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": fmt.Sprintf("AI generation failed: %v", err)})
		return
	}

	suggestion, err := parseAISuggestion(raw)
	if err != nil {
		// Return the raw response alongside the error so the frontend can show
		// a fallback message rather than a blank state.
		c.JSON(http.StatusOK, gin.H{
			"ok":          false,
			"error":       fmt.Sprintf("AI response could not be parsed: %v", err),
			"raw_response": raw,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":         true,
		"suggestion": suggestion,
	})
}

// ── Prompt builder ────────────────────────────────────────────────────────────

func (h *ConfiguratorAIHandler) buildSetupPrompt(channel, description string) string {
	channelContext := channelListingContext(channel)

	return fmt.Sprintf(`You are a marketplace listing expert helping a seller configure a listing template for %s.

The seller described their product as:
"%s"

%s

Based on this product description, suggest the optimal configurator settings. You MUST respond with ONLY valid JSON — no preamble, no explanation, no markdown code fences. The JSON must match this exact schema:

{
  "category_id": "<the most appropriate category ID or leaf node ID as a string>",
  "category_path": "<human-readable breadcrumb, e.g. Electronics > Cameras > Digital Cameras>",
  "attribute_defaults": [
    {
      "attribute_name": "<attribute name as used by the channel>",
      "source": "default_value",
      "default_value": "<suggested value>"
    }
  ],
  "shipping_defaults": {
    "<key>": "<value>"
  },
  "reasoning": "<one sentence explaining your category choice>"
}

Rules:
- Include 3–8 of the most important attributes for this product type and channel.
- For attribute names use the channel's own naming conventions.
- For Amazon: category_id should be the product type string (e.g. "CAMERA") not a numeric ID.
- For eBay: category_id should be a numeric eBay category ID (e.g. "31388").
- For Shopify/WooCommerce: category_id is the collection or product type slug.
- shipping_defaults keys should be channel-relevant (e.g. "dispatch_time", "carrier", "service").
- Keep default_value values realistic and short.
- Never include any text outside the JSON object.`,
		channelDisplayName(channel),
		description,
		channelContext,
	)
}

// channelListingContext returns channel-specific guidance injected into the prompt.
func channelListingContext(channel string) string {
	switch strings.ToLower(channel) {
	case "amazon":
		return "Channel context: Amazon SP-API. Category IDs are product type strings (e.g. CONSUMER_ELECTRONICS, CAMERA). Important attributes include brand, manufacturer, model_name, color, material_type, item_weight."
	case "ebay":
		return "Channel context: eBay Inventory API (UK marketplace). Category IDs are numeric (e.g. 31388 for Digital Cameras). Important aspects include Brand, Model, MPN, Condition, Type, Color, Storage Capacity."
	case "shopify":
		return "Channel context: Shopify REST Admin API. Category ID is a product type string. Important attributes include vendor, product_type, tags. Shipping defaults include weight, requires_shipping."
	case "woocommerce":
		return "Channel context: WooCommerce REST API. Category ID is numeric. Important attributes include pa_color, pa_size, pa_material. Shipping defaults include weight, dimensions."
	case "etsy":
		return "Channel context: Etsy API v3. Category ID is taxonomy_id (numeric). Important attributes include who_made, when_made, is_supply, occasion, style, holiday."
	case "kaufland":
		return "Channel context: Kaufland Marketplace API. Category ID is a Kaufland category ID. Important attributes include brand, ean, model, color, material."
	case "tiktok":
		return "Channel context: TikTok Shop API. Category ID is a TikTok leaf category ID. Important attributes include brand, size_chart, material, pattern_type."
	case "walmart":
		return "Channel context: Walmart Marketplace API. Category ID is a product type string. Important attributes include brand, manufacturer, model_number, color, size."
	default:
		return fmt.Sprintf("Channel context: %s marketplace. Suggest appropriate category and key product attributes.", channelDisplayName(channel))
	}
}

func channelDisplayName(channel string) string {
	names := map[string]string{
		"amazon": "Amazon", "ebay": "eBay", "shopify": "Shopify",
		"woocommerce": "WooCommerce", "etsy": "Etsy", "kaufland": "Kaufland",
		"tiktok": "TikTok Shop", "walmart": "Walmart", "temu": "Temu",
		"bigcommerce": "BigCommerce", "magento": "Magento", "onbuy": "OnBuy",
	}
	if n, ok := names[strings.ToLower(channel)]; ok {
		return n
	}
	return channel
}

// ── Response parser ───────────────────────────────────────────────────────────

// parseAISuggestion extracts the JSON block from the raw AI response and
// deserialises it. It strips any accidental markdown fences.
func parseAISuggestion(raw string) (*AIConfiguratorSuggestion, error) {
	// Strip ```json ... ``` fences if present
	clean := strings.TrimSpace(raw)
	if idx := strings.Index(clean, "```json"); idx >= 0 {
		clean = clean[idx+7:]
		if end := strings.Index(clean, "```"); end >= 0 {
			clean = clean[:end]
		}
	} else if idx := strings.Index(clean, "```"); idx >= 0 {
		clean = clean[idx+3:]
		if end := strings.Index(clean, "```"); end >= 0 {
			clean = clean[:end]
		}
	}
	clean = strings.TrimSpace(clean)

	// Find the outermost JSON object
	start := strings.Index(clean, "{")
	end := strings.LastIndex(clean, "}")
	if start < 0 || end < 0 || end <= start {
		return nil, fmt.Errorf("no JSON object found in AI response")
	}
	clean = clean[start : end+1]

	// Unmarshal into a generic map first so we can handle attribute_defaults
	// regardless of whether source/ep_key/default_value sub-fields are present.
	var raw2 struct {
		CategoryID        string                 `json:"category_id"`
		CategoryPath      string                 `json:"category_path"`
		AttributeDefaults []json.RawMessage      `json:"attribute_defaults"`
		ShippingDefaults  map[string]string      `json:"shipping_defaults"`
		Reasoning         string                 `json:"reasoning"`
	}
	if err := json.Unmarshal([]byte(clean), &raw2); err != nil {
		return nil, fmt.Errorf("JSON unmarshal: %w", err)
	}

	suggestion := &AIConfiguratorSuggestion{
		CategoryID:       raw2.CategoryID,
		CategoryPath:     raw2.CategoryPath,
		ShippingDefaults: raw2.ShippingDefaults,
		Reasoning:        raw2.Reasoning,
	}
	if suggestion.ShippingDefaults == nil {
		suggestion.ShippingDefaults = map[string]string{}
	}

	for _, rawAttr := range raw2.AttributeDefaults {
		var attr models.AttributeDefault
		if err := json.Unmarshal(rawAttr, &attr); err != nil {
			continue // skip malformed entries
		}
		if attr.Source == "" {
			attr.Source = "default_value"
		}
		suggestion.AttributeDefaults = append(suggestion.AttributeDefaults, attr)
	}
	if suggestion.AttributeDefaults == nil {
		suggestion.AttributeDefaults = []models.AttributeDefault{}
	}

	return suggestion, nil
}
