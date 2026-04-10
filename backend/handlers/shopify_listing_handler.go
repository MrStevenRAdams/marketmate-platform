package handlers

// ============================================================================
// SHOPIFY LISTING HANDLER — PRC-01
// ============================================================================
// Endpoints for Shopify listing creation with quantity-based pricing tiers.
//
// Routes (register in main.go under shopifyGroup):
//   POST /shopify/prepare    → auto-map PIM product to a ShopifyDraft
//   POST /shopify/submit     → create/update Shopify product + price rules
// ============================================================================

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"module-a/models"
	"module-a/repository"
	"module-a/services"
)

// ── Handler struct ─────────────────────────────────────────────────────────

type ShopifyListingHandler struct {
	marketplaceService *services.MarketplaceService
	repo               *repository.MarketplaceRepository
	productRepo        *repository.FirestoreRepository
}

func NewShopifyListingHandler(
	marketplaceService *services.MarketplaceService,
	repo *repository.MarketplaceRepository,
	productRepo *repository.FirestoreRepository,
) *ShopifyListingHandler {
	return &ShopifyListingHandler{
		marketplaceService: marketplaceService,
		repo:               repo,
		productRepo:        productRepo,
	}
}

// ── Request / Response types ───────────────────────────────────────────────

type ShopifyPrepareRequest struct {
	ProductID    string `json:"product_id" binding:"required"`
	CredentialID string `json:"credential_id"`
}

// ShopifyPricingTier — quantity break row (mirrors EbayPricingTier, PRC-01)
type ShopifyPricingTier struct {
	MinQty       int    `json:"minQty"`
	PricePerUnit string `json:"pricePerUnit"`
}

type ShopifyDraft struct {
	// Core fields
	Title       string `json:"title"`
	Description string `json:"description"` // HTML allowed (Shopify body_html)
	Vendor      string `json:"vendor"`      // brand → vendor
	ProductType string `json:"productType"` // optional category/type string
	Tags        string `json:"tags"`

	// Variant / pricing
	SKU            string `json:"sku"`
	Barcode        string `json:"barcode"`
	Price          string `json:"price"`          // base unit price
	CompareAtPrice string `json:"compareAtPrice"` // RRP / was-price
	Quantity       string `json:"quantity"`

	// Weight / shipping
	WeightValue string `json:"weightValue"`
	WeightUnit  string `json:"weightUnit"` // g | kg | lb | oz

	// Images (URLs)
	Images []string `json:"images"`

	// Status: active | draft
	Status string `json:"status"`

	// PRC-01 — Quantity-based pricing tiers
	// Serialised to Shopify Price Rules API on submit.
	PricingTiers []ShopifyPricingTier `json:"pricingTiers"`

	// FLD-01 — Payment methods annotation (stored in overrides; not sent to Shopify API).
	// e.g. ["PayPal", "Credit/Debit Card", "Apple Pay"]
	PaymentMethods []string `json:"paymentMethods"`

	// FLD-02 — Bullet points (up to 8).
	// On submit these are prepended to body_html as a <ul> block.
	BulletPoints []string `json:"bulletPoints"`

	// VAR-01 — Variation listings (Session H).
	// When len(Variants) > 0 the submit handler creates one Shopify product with
	// multiple variants (options + variants array). When empty, the single-variant
	// flow is used.
	Variants []ChannelVariantDraft `json:"variants,omitempty"`

	// FLD-13 — Metafields
	Metafields []struct {
		Namespace string `json:"namespace"`
		Key       string `json:"key"`
		Value     string `json:"value"`
		Type      string `json:"type"`
	} `json:"metafields,omitempty"`

	// Image alt text
	ImageAlts []string `json:"imageAlts,omitempty"`

	// NEW: Tax
	Taxable bool `json:"taxable"`

	// NEW: Unit pricing (EU/UK)
	UnitPriceMeasure       string `json:"unitPriceMeasure,omitempty"`       // e.g. "100"
	UnitPriceMeasurementUnit string `json:"unitPriceMeasurementUnit,omitempty"` // e.g. "ml"
	UnitPriceQuantityUnit  string `json:"unitPriceQuantityUnit,omitempty"`  // e.g. "cl"

	// NEW: Cost / profit / margin (stored in metafields, not sent to Shopify directly)
	CostPerItem string `json:"costPerItem,omitempty"`

	// NEW: Inventory location
	InventoryLocationID string `json:"inventoryLocationId,omitempty"` // Shopify location ID
	InventoryManaged    bool   `json:"inventoryManaged"`              // false = don't track

	// NEW: Shipping
	RequiresShipping bool   `json:"requiresShipping"`
	CountryOfOrigin  string `json:"countryOfOrigin,omitempty"`  // ISO 3166-1 alpha-2
	HSCode           string `json:"hsCode,omitempty"`           // Harmonized System code

	// NEW: Product category (Shopify taxonomy ID)
	CategoryID string `json:"categoryId,omitempty"`

	// NEW: Sales channels to publish to (publication IDs)
	PublicationIDs []string `json:"publicationIds,omitempty"`

	// NEW: Collections to add the product to
	CollectionIDs []string `json:"collectionIds,omitempty"`

	// NEW: SEO
	SEOTitle       string `json:"seoTitle,omitempty"`
	SEODescription string `json:"seoDescription,omitempty"`

	// Update context — populated when an existing Shopify product is found
	IsUpdate          bool   `json:"isUpdate"`
	ExistingProductID string `json:"existingProductId"` // Shopify product ID
	ExistingVariantID string `json:"existingVariantId"`  // First variant ID
}

type ShopifyPrepareResponse struct {
	OK           bool          `json:"ok"`
	Error        string        `json:"error,omitempty"`
	Product      interface{}   `json:"product,omitempty"`
	Draft        *ShopifyDraft `json:"draft,omitempty"`
	DebugErrors  []string      `json:"debugErrors,omitempty"`
}

type ShopifySubmitRequest struct {
	ProductID    string       `json:"product_id"`
	CredentialID string       `json:"credential_id"`
	Draft        ShopifyDraft `json:"draft"`
	Publish      bool         `json:"publish"` // true → status: active
}

type ShopifySubmitResponse struct {
	OK                bool     `json:"ok"`
	Error             string   `json:"error,omitempty"`
	ShopifyProductID  string   `json:"shopifyProductId,omitempty"`
	ShopifyVariantID  string   `json:"shopifyVariantId,omitempty"`
	URL               string   `json:"url,omitempty"`
	PriceRulesCreated int      `json:"priceRulesCreated"`
	Warnings          []string `json:"warnings,omitempty"`
}

// ── Shopify API client (thin wrapper around adapter do()) ──────────────────

type shopifyAPIClient struct {
	shopDomain  string
	accessToken string
	apiVersion  string
	httpClient  *http.Client
}

func newShopifyAPIClient(shopDomain, accessToken, apiVersion string) *shopifyAPIClient {
	if apiVersion == "" {
		apiVersion = "2024-01"
	}
	return &shopifyAPIClient{
		shopDomain:  shopDomain,
		accessToken: accessToken,
		apiVersion:  apiVersion,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (s *shopifyAPIClient) url(path string) string {
	return fmt.Sprintf("https://%s/admin/api/%s%s", s.shopDomain, s.apiVersion, path)
}

func (s *shopifyAPIClient) do(ctx context.Context, method, path string, body interface{}) (map[string]interface{}, error) {
	var reqBytes []byte
	var err error
	if body != nil {
		reqBytes, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal: %w", err)
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, s.url(path), strings.NewReader(string(reqBytes)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Shopify-Access-Token", s.accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if resp.StatusCode >= 400 {
		errMsg, _ := json.Marshal(result)
		return nil, fmt.Errorf("shopify API %d: %s", resp.StatusCode, string(errMsg))
	}
	return result, nil
}

// getShopifyClient resolves credentials and returns an API client.
func (h *ShopifyListingHandler) getShopifyClient(ctx context.Context, tenantID, credentialID string) (*shopifyAPIClient, string, error) {
	var cred *models.MarketplaceCredential
	if credentialID != "" {
		c, err := h.repo.GetCredential(ctx, tenantID, credentialID)
		if err != nil {
			return nil, "", fmt.Errorf("get credential %s: %w", credentialID, err)
		}
		cred = c
	} else {
		creds, err := h.repo.ListCredentials(ctx, tenantID)
		if err != nil {
			return nil, "", fmt.Errorf("list credentials: %w", err)
		}
		for _, c := range creds {
			if c.Channel == "shopify" {
				cred = &c
				break
			}
		}
	}
	if cred == nil {
		return nil, "", fmt.Errorf("no Shopify credential found")
	}

	shopDomain := cred.CredentialData["shop_domain"]
	accessToken := cred.CredentialData["access_token"]
	apiVersion := cred.CredentialData["api_version"]
	if shopDomain == "" || accessToken == "" {
		return nil, "", fmt.Errorf("Shopify credential missing shop_domain or access_token")
	}

	client := newShopifyAPIClient(shopDomain, accessToken, apiVersion)
	return client, cred.CredentialID, nil
}

// ============================================================================
// POST /api/v1/shopify/prepare
// ============================================================================
// Loads the PIM product and auto-maps fields into a ShopifyDraft.
// Checks for an existing Shopify product with matching SKU tag.

func (h *ShopifyListingHandler) PrepareShopifyListing(c *gin.Context) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[Shopify Prepare] PANIC: %v", r)
			c.JSON(http.StatusOK, gin.H{"ok": false, "error": fmt.Sprintf("internal panic: %v", r)})
		}
	}()

	tenantID := c.GetString("tenant_id")

	var req ShopifyPrepareRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	var debugErrors []string

	// ── Step 1: Load PIM product ──────────────────────────────────────────
	productModel, err := h.productRepo.GetProduct(c.Request.Context(), tenantID, req.ProductID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": fmt.Sprintf("product not found: %v", err)})
		return
	}
	productBytes, _ := json.Marshal(productModel)
	var product map[string]interface{}
	json.Unmarshal(productBytes, &product)
	log.Printf("[Shopify Prepare] Product loaded: %s", extractString(product, "title"))

	// ── Step 2: Build draft from PIM data ────────────────────────────────
	draft := buildShopifyDraft(product)

	// ── Step 3: Try to resolve Shopify client ────────────────────────────
	client, _, err := h.getShopifyClient(c.Request.Context(), tenantID, req.CredentialID)
	if err != nil {
		debugErrors = append(debugErrors, fmt.Sprintf("Shopify client: %v — you can still fill in the form manually", err))
		c.JSON(http.StatusOK, ShopifyPrepareResponse{
			OK:          true,
			Product:     product,
			Draft:       draft,
			DebugErrors: debugErrors,
		})
		return
	}

	// ── Step 4: Check for existing Shopify product by SKU ────────────────
	if draft.SKU != "" {
		searchURL := fmt.Sprintf("/products.json?fields=id,title,handle,variants,status&limit=5")
		existing, err := client.do(c.Request.Context(), "GET", searchURL, nil)
		if err == nil {
			if products, ok := existing["products"].([]interface{}); ok {
				for _, p := range products {
					pm, ok := p.(map[string]interface{})
					if !ok {
						continue
					}
					variants, _ := pm["variants"].([]interface{})
					for _, v := range variants {
						vm, ok := v.(map[string]interface{})
						if !ok {
							continue
						}
						sku, _ := vm["sku"].(string)
						if sku == draft.SKU {
							prodID := fmt.Sprintf("%.0f", pm["id"].(float64))
							varID := fmt.Sprintf("%.0f", vm["id"].(float64))
							draft.IsUpdate = true
							draft.ExistingProductID = prodID
							draft.ExistingVariantID = varID
							log.Printf("[Shopify Prepare] Found existing product %s / variant %s for SKU %s", prodID, varID, draft.SKU)
							break
						}
					}
					if draft.IsUpdate {
						break
					}
				}
			}
		} else {
			debugErrors = append(debugErrors, fmt.Sprintf("existing product lookup: %v", err))
		}
	}

	// ── Step 5: Load PIM variants (VAR-01) ────────────────────────────────
	// If the product is a variant child, resolve to the parent first so we
	// can load the whole family. The parent's data fills the listing fields;
	// the variants populate the variant grid.
	variantSourceID := req.ProductID
	if productModel.ProductType == "variant" && productModel.ParentID != nil && *productModel.ParentID != "" {
		log.Printf("[Shopify Prepare] Product %s is a variant child — resolving parent %s", req.ProductID, *productModel.ParentID)
		parent, err := h.productRepo.GetProduct(c.Request.Context(), tenantID, *productModel.ParentID)
		if err == nil {
			// Rebuild draft from the parent so title/description/images come from the parent
			parentBytes, _ := json.Marshal(parent)
			var parentMap map[string]interface{}
			json.Unmarshal(parentBytes, &parentMap)
			parentDraft := buildShopifyDraft(parentMap)
			// Preserve the IsUpdate state we detected above
			parentDraft.IsUpdate = draft.IsUpdate
			parentDraft.ExistingProductID = draft.ExistingProductID
			parentDraft.ExistingVariantID = draft.ExistingVariantID
			draft = parentDraft
			variantSourceID = *productModel.ParentID
			log.Printf("[Shopify Prepare] Rebuilt draft from parent %s", variantSourceID)
		} else {
			log.Printf("[Shopify Prepare] Could not load parent %s: %v — using child data", *productModel.ParentID, err)
		}
	}

	fallbackImage := ""
	if len(draft.Images) > 0 {
		fallbackImage = draft.Images[0]
	}
	draft.Variants = loadChannelVariants(c.Request.Context(), h.productRepo, tenantID, variantSourceID, draft.Price, fallbackImage)
	log.Printf("[Shopify Prepare] Loaded %d variants for source product %s", len(draft.Variants), variantSourceID)

	c.JSON(http.StatusOK, ShopifyPrepareResponse{
		OK:          true,
		Product:     product,
		Draft:       draft,
		DebugErrors: debugErrors,
	})
}

// ============================================================================
// POST /api/v1/shopify/submit
// ============================================================================
// Creates or updates a Shopify product, then serialises pricing tiers into
// Shopify Price Rules (one PriceRule + DiscountCode per tier).
// Saves a listing record to Firestore on success.

func (h *ShopifyListingHandler) SubmitShopifyListing(c *gin.Context) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[Shopify Submit] PANIC: %v", r)
			c.JSON(http.StatusOK, gin.H{"ok": false, "error": fmt.Sprintf("internal panic: %v", r)})
		}
	}()

	tenantID := c.GetString("tenant_id")

	var req ShopifySubmitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}
	draft := req.Draft
	if draft.SKU == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "SKU is required"})
		return
	}

	client, credID, err := h.getShopifyClient(c.Request.Context(), tenantID, req.CredentialID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": fmt.Sprintf("Shopify client: %v", err)})
		return
	}

	var warnings []string
	resp := ShopifySubmitResponse{OK: true}
	ctx := c.Request.Context()

	status := "draft"
	if req.Publish {
		status = "active"
	}

	// ── Step 1: Build Shopify product payload ────────────────────────────

	// FLD-02: prepend bullet points as <ul> to body_html if provided
	body_html := draft.Description
	if len(draft.BulletPoints) > 0 {
		var sb strings.Builder
		sb.WriteString("<ul>")
		for _, bp := range draft.BulletPoints {
			if bp != "" {
				sb.WriteString("<li>")
				sb.WriteString(bp)
				sb.WriteString("</li>")
			}
		}
		sb.WriteString("</ul>")
		if body_html != "" {
			sb.WriteString(body_html)
		}
		body_html = sb.String()
	}

	productPayload := map[string]interface{}{
		"title":        draft.Title,
		"body_html":    body_html,
		"vendor":       draft.Vendor,
		"product_type": draft.ProductType,
		"tags":         draft.Tags,
		"status":       status,
	}

	// Category (Shopify product taxonomy)
	if draft.CategoryID != "" {
		productPayload["product_category"] = map[string]interface{}{
			"product_taxonomy_node_id": draft.CategoryID,
		}
	}

	// SEO
	if draft.SEOTitle != "" || draft.SEODescription != "" {
		seo := map[string]interface{}{}
		if draft.SEOTitle != "" { seo["title"] = draft.SEOTitle }
		if draft.SEODescription != "" { seo["description"] = draft.SEODescription }
		productPayload["seo"] = seo
	}

	// Build image array
	if len(draft.Images) > 0 {
		images := make([]map[string]interface{}, 0, len(draft.Images))
		for i, src := range draft.Images {
			images = append(images, map[string]interface{}{
				"src":      src,
				"position": i + 1,
			})
		}
		productPayload["images"] = images
	}

	// Determine active variants for multi-variant path
	activeVariants := make([]ChannelVariantDraft, 0)
	for _, v := range draft.Variants {
		if v.Active {
			activeVariants = append(activeVariants, v)
		}
	}

	// Validate Shopify variant constraints
	if len(activeVariants) >= 2 {
		// Count unique option keys
		optionKeys := map[string]bool{}
		for _, v := range activeVariants {
			for k := range v.Combination {
				optionKeys[k] = true
			}
		}
		if len(optionKeys) > 3 {
			c.JSON(http.StatusOK, gin.H{
				"ok":    false,
				"error": fmt.Sprintf("Shopify supports a maximum of 3 variation options (e.g. Size, Colour, Material). This product has %d options. Please reduce the number of options before listing on Shopify.", len(optionKeys)),
			})
			return
		}
		if len(activeVariants) > 100 {
			c.JSON(http.StatusOK, gin.H{
				"ok":    false,
				"error": fmt.Sprintf("Shopify supports a maximum of 100 variants per product. This product has %d active variants. Please use the 'split into multiple listings' option in the listing form.", len(activeVariants)),
				"variant_count": len(activeVariants),
				"over_limit": true,
			})
			return
		}
	}

	if len(activeVariants) >= 2 {
		// ── Multi-variant path: build options[] + variants[] ─────────────
		// Collect option names from combination keys
		optionNames := []string{}
		if len(activeVariants) > 0 {
			for k := range activeVariants[0].Combination {
				optionNames = appendUnique(optionNames, k)
			}
		}
		options := make([]map[string]interface{}, 0, len(optionNames))
		for i, name := range optionNames {
			options = append(options, map[string]interface{}{"name": name, "position": i + 1})
		}
		productPayload["options"] = options

		variantsArr := make([]map[string]interface{}, 0, len(activeVariants))
		for i, v := range activeVariants {
			vp := map[string]interface{}{
				"sku":   v.SKU,
				"price": v.Price,
			}
			if v.EAN != "" {
				vp["barcode"] = v.EAN
			}
			if v.Stock != "" {
				if qty, err := strconv.Atoi(v.Stock); err == nil {
					vp["inventory_quantity"] = qty
					vp["inventory_management"] = "shopify"
				}
			}
			// Map combination keys to option1/option2/option3
			for j, name := range optionNames {
				key := fmt.Sprintf("option%d", j+1)
				vp[key] = v.Combination[name]
			}
			_ = i
			variantsArr = append(variantsArr, vp)
		}
		productPayload["variants"] = variantsArr
	} else {
		// ── Single-variant path (original behaviour) ──────────────────────
		variantPayload := map[string]interface{}{
			"sku":   draft.SKU,
			"price": draft.Price,
		}
		if draft.CompareAtPrice != "" {
			variantPayload["compare_at_price"] = draft.CompareAtPrice
		}
		if draft.Barcode != "" {
			variantPayload["barcode"] = draft.Barcode
		}
		if draft.WeightValue != "" {
			if wv, err := strconv.ParseFloat(draft.WeightValue, 64); err == nil {
				variantPayload["weight"] = wv
				variantPayload["weight_unit"] = draft.WeightUnit
			}
		}
		if draft.Quantity != "" {
			if qty, err := strconv.Atoi(draft.Quantity); err == nil {
				variantPayload["inventory_quantity"] = qty
				if draft.InventoryManaged {
					variantPayload["inventory_management"] = "shopify"
				} else {
					variantPayload["inventory_management"] = ""
				}
			}
		}
		// Tax
		variantPayload["taxable"] = draft.Taxable
		// Requires shipping
		variantPayload["requires_shipping"] = draft.RequiresShipping
		// Country of origin + HS code
		if draft.CountryOfOrigin != "" {
			variantPayload["country_code_of_origin"] = draft.CountryOfOrigin
		}
		if draft.HSCode != "" {
			variantPayload["harmonized_system_code"] = draft.HSCode
		}
		// Unit pricing (EU/UK)
		if draft.UnitPriceMeasure != "" && draft.UnitPriceMeasurementUnit != "" {
			variantPayload["unit_price_measurement"] = map[string]interface{}{
				"measured_type":          "volume",
				"quantity_value":         1.0,
				"quantity_unit":          draft.UnitPriceQuantityUnit,
				"reference_value":        draft.UnitPriceMeasure,
				"reference_unit":         draft.UnitPriceMeasurementUnit,
			}
		}
		// Cost per item
		if draft.CostPerItem != "" {
			variantPayload["cost"] = draft.CostPerItem
		}
		productPayload["variants"] = []map[string]interface{}{variantPayload}
	}

	// ── Step 2: Create or update the Shopify product ─────────────────────
	var shopifyProductID string
	var shopifyVariantID string

	if draft.IsUpdate && draft.ExistingProductID != "" {
		// UPDATE
		productPayload["id"] = draft.ExistingProductID
		body := map[string]interface{}{"product": productPayload}
		result, err := client.do(ctx, "PUT", fmt.Sprintf("/products/%s.json", draft.ExistingProductID), body)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"ok": false, "error": fmt.Sprintf("update product: %v", err)})
			return
		}
		shopifyProductID = draft.ExistingProductID
		shopifyVariantID = draft.ExistingVariantID

		// For single-variant update: also patch the variant directly
		if len(activeVariants) < 2 && shopifyVariantID != "" {
			variantPayload := map[string]interface{}{
				"sku":   draft.SKU,
				"price": draft.Price,
			}
			varBody := map[string]interface{}{"variant": variantPayload}
			if _, err := client.do(ctx, "PUT", fmt.Sprintf("/variants/%s.json", shopifyVariantID), varBody); err != nil {
				warnings = append(warnings, fmt.Sprintf("variant update partial failure: %v", err))
			}
		}
		_ = result
		log.Printf("[Shopify Submit] Updated product %s", shopifyProductID)

	} else {
		// CREATE
		body := map[string]interface{}{"product": productPayload}
		result, err := client.do(ctx, "POST", "/products.json", body)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"ok": false, "error": fmt.Sprintf("create product: %v", err)})
			return
		}
		if p, ok := result["product"].(map[string]interface{}); ok {
			shopifyProductID = fmt.Sprintf("%.0f", p["id"].(float64))
			if variants, ok := p["variants"].([]interface{}); ok && len(variants) > 0 {
				if v, ok := variants[0].(map[string]interface{}); ok {
					shopifyVariantID = fmt.Sprintf("%.0f", v["id"].(float64))
				}
			}
			if handle, ok := p["handle"].(string); ok {
				resp.URL = fmt.Sprintf("https://%s/products/%s", client.shopDomain, handle)
			}
		}
		log.Printf("[Shopify Submit] Created product %s / variant %s", shopifyProductID, shopifyVariantID)
	}

	resp.ShopifyProductID = shopifyProductID
	resp.ShopifyVariantID = shopifyVariantID

	// ── Step 2b: Submit metafields ────────────────────────────────────────
	if shopifyProductID != "" && len(draft.Metafields) > 0 {
		for _, mf := range draft.Metafields {
			if mf.Key == "" || mf.Value == "" {
				continue
			}
			ns := mf.Namespace
			if ns == "" { ns = "custom" }
			mfPayload := map[string]interface{}{
				"metafield": map[string]interface{}{
					"namespace": ns,
					"key":       mf.Key,
					"value":     mf.Value,
					"type":      mf.Type,
					"owner_id":    shopifyProductID,
					"owner_resource": "product",
				},
			}
			if _, err := client.do(ctx, "POST", "/metafields.json", mfPayload); err != nil {
				warnings = append(warnings, fmt.Sprintf("metafield %s.%s: %v", ns, mf.Key, err))
			}
		}
	}

	// Add to collections (custom only — smart collections manage their own membership)
	if shopifyProductID != "" && len(draft.CollectionIDs) > 0 {
		for _, colID := range draft.CollectionIDs {
			// Skip smart collections — prefixed with "smart:" by the frontend
			if strings.HasPrefix(colID, "smart:") {
				log.Printf("[Shopify Submit] Skipping smart collection %s — membership is rule-based", colID)
				continue
			}
			collectPayload := map[string]interface{}{
				"collect": map[string]interface{}{
					"product_id":    shopifyProductID,
					"collection_id": colID,
				},
			}
			if _, err := client.do(ctx, "POST", "/collects.json", collectPayload); err != nil {
				// 403 = smart collection — skip silently
				if strings.Contains(err.Error(), "403") || strings.Contains(err.Error(), "SmartCollection") {
					log.Printf("[Shopify Submit] Skipping smart collection %s (403 — rule-based)", colID)
					continue
				}
				warnings = append(warnings, fmt.Sprintf("add to collection %s: %v", colID, err))
			}
		}
	}

	// ── Step 2c: Publish to selected sales channels ───────────────────────
	if shopifyProductID != "" && len(draft.PublicationIDs) > 0 {
		for _, pubID := range draft.PublicationIDs {
			pubPayload := map[string]interface{}{
				"collect": map[string]interface{}{
					"publication_id": pubID,
				},
			}
			if _, err := client.do(ctx, "POST",
				fmt.Sprintf("/products/%s/publications.json", shopifyProductID),
				pubPayload); err != nil {
				warnings = append(warnings, fmt.Sprintf("publish to channel %s: %v", pubID, err))
			}
		}
	}

	// ── Step 2d: Set inventory location (if managed + location chosen) ────
	if shopifyProductID != "" && shopifyVariantID != "" &&
		draft.InventoryManaged && draft.InventoryLocationID != "" && draft.Quantity != "" {
		if qty, err := strconv.Atoi(draft.Quantity); err == nil {
			// First get the inventory_item_id from the variant
			varResult, err := client.do(ctx, "GET",
				fmt.Sprintf("/variants/%s.json", shopifyVariantID), nil)
			if err == nil {
				if vd, ok := varResult["variant"].(map[string]interface{}); ok {
					if iid, ok := vd["inventory_item_id"].(float64); ok {
						invPayload := map[string]interface{}{
							"location_id":       draft.InventoryLocationID,
							"inventory_item_id": int64(iid),
							"available":         qty,
						}
						if _, err := client.do(ctx, "POST", "/inventory_levels/set.json", invPayload); err != nil {
							warnings = append(warnings, fmt.Sprintf("set inventory level: %v", err))
						}
					}
				}
			}
		}
	}

	// ── Step 3: Serialise pricing tiers → Shopify Price Rules (PRC-01) ──
	// Shopify does not support inline tiered pricing on variants. Each tier
	// becomes a PriceRule (FixedAmount discount off the base price, min qty)
	// paired with a DiscountCode. Codes are generated as:
	//   TIER_{SKU}_{minQty}  e.g. TIER_ABC123_5
	// This matches the "Buy N or more, get £X each" UX in the form.
	priceRulesCreated := 0
	basePrice, basePriceErr := strconv.ParseFloat(draft.Price, 64)
	if len(draft.PricingTiers) > 0 && basePriceErr != nil {
		warnings = append(warnings, "Could not parse base price — skipping pricing tiers")
	} else if len(draft.PricingTiers) > 0 {
		for _, tier := range draft.PricingTiers {
			tierPrice, err := strconv.ParseFloat(tier.PricePerUnit, 64)
			if err != nil || tier.MinQty < 2 {
				warnings = append(warnings, fmt.Sprintf("Skipping invalid tier (minQty=%d, price=%q): %v", tier.MinQty, tier.PricePerUnit, err))
				continue
			}
			discount := basePrice - tierPrice
			if discount <= 0 {
				warnings = append(warnings, fmt.Sprintf("Tier minQty=%d price (%s) not lower than base price — skipped", tier.MinQty, tier.PricePerUnit))
				continue
			}

			// Create PriceRule
			priceRule := map[string]interface{}{
				"price_rule": map[string]interface{}{
					"title":              fmt.Sprintf("Buy %d or more: £%.2f each (%s)", tier.MinQty, tierPrice, draft.SKU),
					"target_type":        "line_item",
					"target_selection":   "entitled",
					"allocation_method":  "each",
					"value_type":         "fixed_amount",
					"value":              fmt.Sprintf("-%.2f", discount),
					"customer_selection": "all",
					"starts_at":          time.Now().UTC().Format(time.RFC3339),
					"entitlement_product_ids": []string{shopifyProductID},
					"prerequisite_quantity_range": map[string]interface{}{
						"greater_than_or_equal_to": tier.MinQty,
					},
				},
			}

			prResult, err := client.do(ctx, "POST", "/price_rules.json", priceRule)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("Tier minQty=%d price rule creation failed: %v", tier.MinQty, err))
				continue
			}

			// Extract price rule ID and create discount code
			var priceRuleID string
			if pr, ok := prResult["price_rule"].(map[string]interface{}); ok {
				priceRuleID = fmt.Sprintf("%.0f", pr["id"].(float64))
			}
			if priceRuleID == "" {
				warnings = append(warnings, fmt.Sprintf("Tier minQty=%d: could not extract price rule ID", tier.MinQty))
				continue
			}

			discountCode := map[string]interface{}{
				"discount_code": map[string]interface{}{
					"code": fmt.Sprintf("TIER_%s_%d", strings.ToUpper(draft.SKU), tier.MinQty),
				},
			}
			if _, err := client.do(ctx, "POST", fmt.Sprintf("/price_rules/%s/discount_codes.json", priceRuleID), discountCode); err != nil {
				warnings = append(warnings, fmt.Sprintf("Tier minQty=%d discount code creation failed: %v", tier.MinQty, err))
				// Non-fatal — price rule still exists
			}
			priceRulesCreated++
		}
	}

	resp.PriceRulesCreated = priceRulesCreated
	resp.Warnings = warnings

	// ── Step 4: Save listing record to Firestore ────────────────────────
	if req.ProductID != "" && shopifyProductID != "" {
		shopifyOverrides := &models.ListingOverrides{
			Title: draft.Title,
		}
		if len(draft.PaymentMethods) > 0 {
			shopifyOverrides.Attributes = map[string]interface{}{
				"payment_methods": draft.PaymentMethods,
			}
		}
		listing := &models.Listing{
			ListingID:        fmt.Sprintf("shopify_%s_%s", tenantID, shopifyProductID),
			TenantID:         tenantID,
			ProductID:        req.ProductID,
			Channel:          "shopify",
			ChannelAccountID: credID,
			State:            status,
			ChannelIdentifiers: &models.ChannelIdentifiers{
				ListingID: shopifyProductID,
			},
			Overrides: shopifyOverrides,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		if err := h.repo.SaveListing(ctx, listing); err != nil {
			warnings = append(warnings, fmt.Sprintf("listing record save failed (non-fatal): %v", err))
		}
	}

	log.Printf("[Shopify Submit] DONE — productID=%s, priceRules=%d, warnings=%d",
		shopifyProductID, priceRulesCreated, len(warnings))
	c.JSON(http.StatusOK, resp)
}

// ============================================================================
// buildShopifyDraft — maps a PIM product map to a ShopifyDraft
// ============================================================================

func buildShopifyDraft(product map[string]interface{}) *ShopifyDraft {
	draft := &ShopifyDraft{
		Title:       extractString(product, "title"),
		Description: extractString(product, "description"),
		Vendor:      extractString(product, "brand"),
		SKU:         extractString(product, "sku"),
		Status:      "draft",
	}

	// Price — prefer list_price → rrp fallback from nested pricing struct
	if pricing := extractNested(product, "pricing"); pricing != nil {
		if lp := extractNested(pricing, "list_price"); lp != nil {
			if amt := extractNumericStr(lp, "amount"); amt != "" {
				draft.Price = amt
			}
		}
		if draft.Price == "" {
			if rrp := extractNested(pricing, "rrp"); rrp != nil {
				if amt := extractNumericStr(rrp, "amount"); amt != "" {
					draft.Price = amt
				}
			}
		}
		// CompareAtPrice from RRP if both list_price and rrp set
		if draft.Price != "" {
			if rrp := extractNested(pricing, "rrp"); rrp != nil {
				if amt := extractNumericStr(rrp, "amount"); amt != "" && amt != draft.Price {
					draft.CompareAtPrice = amt
				}
			}
		}
	}

	// Also try flat retail_price / price fields (legacy)
	if draft.Price == "" {
		draft.Price = extractNumericStr(product, "retail_price", "price", "list_price")
	}

	// Quantity
	draft.Quantity = extractNumericStr(product, "stock_quantity", "quantity", "inventory_quantity")

	// Identifiers → Barcode (EAN preferred)
	if ids := extractNested(product, "identifiers"); ids != nil {
		draft.Barcode = extractString(ids, "ean", "upc", "barcode")
	}
	if draft.Barcode == "" {
		draft.Barcode = extractString(product, "ean", "barcode")
	}

	// Weight
	if wt := extractNested(product, "shipping_weight", "weight"); wt != nil {
		draft.WeightValue = extractNumericStr(wt, "value")
		draft.WeightUnit = extractString(wt, "unit")
	}

	// Images
	if assets, ok := product["assets"].([]interface{}); ok {
		for _, a := range assets {
			if am, ok := a.(map[string]interface{}); ok {
				if url := extractString(am, "url"); url != "" {
					draft.Images = append(draft.Images, url)
				}
			}
		}
	}

	// Tags: build from attributes if present
	if attrs, ok := product["attributes"].(map[string]interface{}); ok {
		var tagParts []string
		for k, v := range attrs {
			switch val := v.(type) {
			case string:
				if val != "" {
					tagParts = append(tagParts, fmt.Sprintf("%s:%s", k, val))
				}
			}
		}
		draft.Tags = strings.Join(tagParts, ", ")
	}

	// Initialise empty slices so form renders correctly
	draft.PricingTiers = []ShopifyPricingTier{}
	draft.BulletPoints = []string{}
	draft.PaymentMethods = []string{}

	return draft
}
