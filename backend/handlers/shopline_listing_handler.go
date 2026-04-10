package handlers

// ============================================================================
// SHOPLINE LISTING HANDLER
// ============================================================================
// Endpoints for Shopline listing creation with variant and pricing support.
//
// Routes (register in main.go under shoplineGroup):
//   POST /shopline/prepare    → auto-map PIM product to a ShoplineDraft
//   POST /shopline/submit     → create/update Shopline product + price tiers
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

type ShoplineListingHandler struct {
	marketplaceService *services.MarketplaceService
	repo               *repository.MarketplaceRepository
	productRepo        *repository.FirestoreRepository
}

func NewShoplineListingHandler(
	marketplaceService *services.MarketplaceService,
	repo *repository.MarketplaceRepository,
	productRepo *repository.FirestoreRepository,
) *ShoplineListingHandler {
	return &ShoplineListingHandler{
		marketplaceService: marketplaceService,
		repo:               repo,
		productRepo:        productRepo,
	}
}

// ── Request / Response types ───────────────────────────────────────────────

type ShoplinePrepareRequest struct {
	ProductID    string `json:"product_id" binding:"required"`
	CredentialID string `json:"credential_id"`
}

// ShoplinePricingTier — quantity break row (mirrors Shopify PRC-01 pattern)
type ShoplinePricingTier struct {
	MinQty       int    `json:"minQty"`
	PricePerUnit string `json:"pricePerUnit"`
}

type ShoplineDraft struct {
	// Core fields
	Title       string `json:"title"`
	Description string `json:"description"` // HTML allowed
	Vendor      string `json:"vendor"`
	ProductType string `json:"productType"`
	Tags        string `json:"tags"`
	TagsList    []string `json:"tagsList,omitempty"`

	// Variant / pricing
	SKU            string `json:"sku"`
	Barcode        string `json:"barcode"`
	Price          string `json:"price"`
	CompareAtPrice string `json:"compareAtPrice"`
	Quantity       string `json:"quantity"`

	// Weight / shipping
	WeightValue string `json:"weightValue"`
	WeightUnit  string `json:"weightUnit"` // g | kg | lb | oz

	// Images (URLs)
	Images   []string `json:"images"`
	ImageAlts []string `json:"imageAlts,omitempty"`

	// Status: active | draft
	Status string `json:"status"`

	// PRC-01 — Quantity-based pricing tiers (stored in metafields / custom pricing rules)
	PricingTiers []ShoplinePricingTier `json:"pricingTiers"`

	// FLD-02 — Bullet points (up to 8); prepended to description as <ul> on submit
	BulletPoints []string `json:"bulletPoints"`

	// VAR-01 — Variation listings
	// When len(Variants) > 0, creates a Shopline product with multiple variants.
	Variants []ChannelVariantDraft `json:"variants,omitempty"`

	// Shopline-specific: custom attributes (metafields equivalent)
	CustomAttributes []struct {
		Key   string `json:"key"`
		Value string `json:"value"`
		Type  string `json:"type"` // text | number | boolean | json
	} `json:"customAttributes,omitempty"`

	// Tax
	Taxable bool `json:"taxable"`

	// Inventory
	InventoryManaged    bool   `json:"inventoryManaged"`
	InventoryLocationID string `json:"inventoryLocationId,omitempty"`

	// Shipping
	RequiresShipping bool   `json:"requiresShipping"`
	CountryOfOrigin  string `json:"countryOfOrigin,omitempty"`
	HSCode           string `json:"hsCode,omitempty"`

	// SEO
	SEOTitle       string `json:"seoTitle,omitempty"`
	SEODescription string `json:"seoDescription,omitempty"`
	SEOHandle      string `json:"seoHandle,omitempty"` // URL slug

	// Category
	CategoryID   string `json:"categoryId,omitempty"`
	CategoryName string `json:"categoryName,omitempty"`

	// Collections to add the product to
	CollectionIDs []string `json:"collectionIds,omitempty"`

	// Sales channels to publish to
	ChannelIDs []string `json:"channelIds,omitempty"`

	// Cost / profit (stored internally)
	CostPerItem string `json:"costPerItem,omitempty"`

	// Update context
	IsUpdate          bool   `json:"isUpdate"`
	ExistingProductID string `json:"existingProductId"`
	ExistingVariantID string `json:"existingVariantId"`
}

type ShoplinePrepareResponse struct {
	OK          bool           `json:"ok"`
	Error       string         `json:"error,omitempty"`
	Product     interface{}    `json:"product,omitempty"`
	Draft       *ShoplineDraft `json:"draft,omitempty"`
	DebugErrors []string       `json:"debugErrors,omitempty"`
}

type ShoplineSubmitRequest struct {
	ProductID    string        `json:"product_id"`
	CredentialID string        `json:"credential_id"`
	Draft        ShoplineDraft `json:"draft"`
	Publish      bool          `json:"publish"` // true → status: active
}

type ShoplineSubmitResponse struct {
	OK                  bool     `json:"ok"`
	Error               string   `json:"error,omitempty"`
	ShoplineProductID   string   `json:"shoplineProductId,omitempty"`
	ShoplineVariantID   string   `json:"shoplineVariantId,omitempty"`
	URL                 string   `json:"url,omitempty"`
	PricingRulesCreated int      `json:"pricingRulesCreated"`
	Warnings            []string `json:"warnings,omitempty"`
}

// ── Shopline API client ────────────────────────────────────────────────────

type shoplineAPIClient struct {
	shopID      string
	accessToken string
	apiVersion  string
	httpClient  *http.Client
}

func newShoplineAPIClient(shopID, accessToken, apiVersion string) *shoplineAPIClient {
	if apiVersion == "" {
		apiVersion = "v2"
	}
	return &shoplineAPIClient{
		shopID:      shopID,
		accessToken: accessToken,
		apiVersion:  apiVersion,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (s *shoplineAPIClient) url(path string) string {
	return fmt.Sprintf("https://open.shopline.io/api/%s/%s%s", s.apiVersion, s.shopID, path)
}

func (s *shoplineAPIClient) do(ctx context.Context, method, path string, body interface{}) (map[string]interface{}, error) {
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
	req.Header.Set("Authorization", "Bearer "+s.accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

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
		return nil, fmt.Errorf("shopline API %d: %s", resp.StatusCode, string(errMsg))
	}
	return result, nil
}

// getShoplineClient resolves credentials and returns an API client.
func (h *ShoplineListingHandler) getShoplineClient(ctx context.Context, tenantID, credentialID string) (*shoplineAPIClient, string, error) {
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
			if c.Channel == "shopline" {
				cred = &c
				break
			}
		}
	}
	if cred == nil {
		return nil, "", fmt.Errorf("no Shopline credential found")
	}

	shopID := cred.CredentialData["shop_id"]
	accessToken := cred.CredentialData["access_token"]
	apiVersion := cred.CredentialData["api_version"]
	if shopID == "" || accessToken == "" {
		return nil, "", fmt.Errorf("shopline credential missing shop_id or access_token")
	}

	client := newShoplineAPIClient(shopID, accessToken, apiVersion)
	return client, cred.CredentialID, nil
}

// ============================================================================
// POST /api/v1/shopline/prepare
// ============================================================================
// Loads the PIM product and auto-maps fields into a ShoplineDraft.
// Checks for an existing Shopline product with matching SKU.

func (h *ShoplineListingHandler) PrepareShoplineListing(c *gin.Context) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[Shopline Prepare] PANIC: %v", r)
			c.JSON(http.StatusOK, gin.H{"ok": false, "error": fmt.Sprintf("internal panic: %v", r)})
		}
	}()

	tenantID := c.GetString("tenant_id")

	var req ShoplinePrepareRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	var debugErrors []string

	// ── Step 1: Load PIM product ──────────────────────────────────────
	productModel, err := h.productRepo.GetProduct(c.Request.Context(), tenantID, req.ProductID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": fmt.Sprintf("product not found: %v", err)})
		return
	}
	productBytes, _ := json.Marshal(productModel)
	var product map[string]interface{}
	json.Unmarshal(productBytes, &product)
	log.Printf("[Shopline Prepare] Product loaded: %s", extractString(product, "title"))

	// ── Step 2: Build draft from PIM data ─────────────────────────────
	draft := buildShoplineDraft(product)

	// ── Step 3: Try to resolve Shopline client ─────────────────────────
	client, _, err := h.getShoplineClient(c.Request.Context(), tenantID, req.CredentialID)
	if err != nil {
		debugErrors = append(debugErrors, fmt.Sprintf("Shopline client: %v — you can still fill in the form manually", err))
		c.JSON(http.StatusOK, ShoplinePrepareResponse{
			OK:          true,
			Product:     product,
			Draft:       draft,
			DebugErrors: debugErrors,
		})
		return
	}

	// ── Step 4: Check for existing Shopline product by SKU ─────────────
	if draft.SKU != "" {
		searchURL := fmt.Sprintf("/products.json?sku=%s&fields=id,title,handle,variants,status&limit=5",
			draft.SKU)
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
							prodID, _ := pm["id"].(string)
							if prodIDf, ok := pm["id"].(float64); ok {
								prodID = fmt.Sprintf("%.0f", prodIDf)
							}
							varID, _ := vm["id"].(string)
							if varIDf, ok := vm["id"].(float64); ok {
								varID = fmt.Sprintf("%.0f", varIDf)
							}
							draft.IsUpdate = true
							draft.ExistingProductID = prodID
							draft.ExistingVariantID = varID
							log.Printf("[Shopline Prepare] Found existing product %s / variant %s for SKU %s", prodID, varID, draft.SKU)
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

	// ── Step 5: Load PIM variants (VAR-01) ─────────────────────────────
	variantSourceID := req.ProductID
	if productModel.ProductType == "variant" && productModel.ParentID != nil && *productModel.ParentID != "" {
		log.Printf("[Shopline Prepare] Product %s is a variant child — resolving parent %s", req.ProductID, *productModel.ParentID)
		parent, err := h.productRepo.GetProduct(c.Request.Context(), tenantID, *productModel.ParentID)
		if err == nil {
			parentBytes, _ := json.Marshal(parent)
			var parentMap map[string]interface{}
			json.Unmarshal(parentBytes, &parentMap)
			parentDraft := buildShoplineDraft(parentMap)
			parentDraft.IsUpdate = draft.IsUpdate
			parentDraft.ExistingProductID = draft.ExistingProductID
			parentDraft.ExistingVariantID = draft.ExistingVariantID
			draft = parentDraft
			variantSourceID = *productModel.ParentID
		}
	}

	fallbackImage := ""
	if len(draft.Images) > 0 {
		fallbackImage = draft.Images[0]
	}
	draft.Variants = loadChannelVariants(c.Request.Context(), h.productRepo, tenantID, variantSourceID, draft.Price, fallbackImage)
	log.Printf("[Shopline Prepare] Loaded %d variants for source product %s", len(draft.Variants), variantSourceID)

	c.JSON(http.StatusOK, ShoplinePrepareResponse{
		OK:          true,
		Product:     product,
		Draft:       draft,
		DebugErrors: debugErrors,
	})
}

// ============================================================================
// POST /api/v1/shopline/submit
// ============================================================================
// Creates or updates a Shopline product. Saves a listing record to Firestore.

func (h *ShoplineListingHandler) SubmitShoplineListing(c *gin.Context) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[Shopline Submit] PANIC: %v", r)
			c.JSON(http.StatusOK, gin.H{"ok": false, "error": fmt.Sprintf("internal panic: %v", r)})
		}
	}()

	tenantID := c.GetString("tenant_id")

	var req ShoplineSubmitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}
	draft := req.Draft
	if draft.SKU == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "SKU is required"})
		return
	}

	client, credID, err := h.getShoplineClient(c.Request.Context(), tenantID, req.CredentialID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": fmt.Sprintf("Shopline client: %v", err)})
		return
	}

	var warnings []string
	resp := ShoplineSubmitResponse{OK: true}
	ctx := c.Request.Context()

	status := "draft"
	if req.Publish {
		status = "active"
	}

	// ── Step 1: Build description with bullet points ──────────────────
	bodyHTML := draft.Description
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
		if bodyHTML != "" {
			sb.WriteString(bodyHTML)
		}
		bodyHTML = sb.String()
	}

	// ── Step 2: Build product payload ────────────────────────────────
	// Shopline product API mirrors Shopify's structure closely
	productPayload := map[string]interface{}{
		"title":        draft.Title,
		"body_html":    bodyHTML,
		"vendor":       draft.Vendor,
		"product_type": draft.ProductType,
		"status":       status,
	}

	// Tags — Shopline accepts array or comma-separated string
	if len(draft.TagsList) > 0 {
		productPayload["tags"] = draft.TagsList
	} else if draft.Tags != "" {
		productPayload["tags"] = draft.Tags
	}

	// SEO
	if draft.SEOTitle != "" || draft.SEODescription != "" || draft.SEOHandle != "" {
		seo := map[string]interface{}{}
		if draft.SEOTitle != "" {
			seo["title"] = draft.SEOTitle
		}
		if draft.SEODescription != "" {
			seo["description"] = draft.SEODescription
		}
		if draft.SEOHandle != "" {
			productPayload["handle"] = draft.SEOHandle
		}
		productPayload["seo"] = seo
	}

	// Category
	if draft.CategoryID != "" {
		productPayload["category_id"] = draft.CategoryID
	}

	// Images
	if len(draft.Images) > 0 {
		images := make([]map[string]interface{}, 0, len(draft.Images))
		for i, src := range draft.Images {
			img := map[string]interface{}{
				"src":      src,
				"position": i + 1,
			}
			if len(draft.ImageAlts) > i && draft.ImageAlts[i] != "" {
				img["alt"] = draft.ImageAlts[i]
			}
			images = append(images, img)
		}
		productPayload["images"] = images
	}

	// ── Step 3: Build variants ────────────────────────────────────────
	activeVariants := make([]ChannelVariantDraft, 0)
	for _, v := range draft.Variants {
		if v.Active {
			activeVariants = append(activeVariants, v)
		}
	}

	if len(activeVariants) >= 2 {
		// Validate Shopline variant constraints (max 3 option dimensions, 100 variants)
		optionKeys := map[string]bool{}
		for _, v := range activeVariants {
			for k := range v.Combination {
				optionKeys[k] = true
			}
		}
		if len(optionKeys) > 3 {
			c.JSON(http.StatusOK, gin.H{
				"ok":    false,
				"error": fmt.Sprintf("Shopline supports a maximum of 3 variation options. This product has %d options.", len(optionKeys)),
			})
			return
		}
		if len(activeVariants) > 100 {
			c.JSON(http.StatusOK, gin.H{
				"ok":           false,
				"error":        fmt.Sprintf("Shopline supports a maximum of 100 variants per product. This product has %d active variants.", len(activeVariants)),
				"variant_count": len(activeVariants),
				"over_limit":   true,
			})
			return
		}

		// Build options + variants
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
		for _, v := range activeVariants {
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
					vp["inventory_management"] = "shopline"
				}
			}
			for j, name := range optionNames {
				key := fmt.Sprintf("option%d", j+1)
				vp[key] = v.Combination[name]
			}
			variantsArr = append(variantsArr, vp)
		}
		productPayload["variants"] = variantsArr

	} else {
		// Single-variant path
		variantPayload := map[string]interface{}{
			"sku":              draft.SKU,
			"price":            draft.Price,
			"taxable":          draft.Taxable,
			"requires_shipping": draft.RequiresShipping,
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
					variantPayload["inventory_management"] = "shopline"
				}
			}
		}
		if draft.CountryOfOrigin != "" {
			variantPayload["country_code_of_origin"] = draft.CountryOfOrigin
		}
		if draft.HSCode != "" {
			variantPayload["harmonized_system_code"] = draft.HSCode
		}
		if draft.CostPerItem != "" {
			variantPayload["cost"] = draft.CostPerItem
		}
		productPayload["variants"] = []map[string]interface{}{variantPayload}
	}

	// ── Step 4: Create or update the Shopline product ─────────────────
	var shoplineProductID string
	var shoplineVariantID string
	var shoplineDomain string

	if draft.IsUpdate && draft.ExistingProductID != "" {
		// UPDATE
		productPayload["id"] = draft.ExistingProductID
		body := map[string]interface{}{"product": productPayload}
		_, err := client.do(ctx, "PUT", fmt.Sprintf("/products/%s.json", draft.ExistingProductID), body)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"ok": false, "error": fmt.Sprintf("update product: %v", err)})
			return
		}
		shoplineProductID = draft.ExistingProductID
		shoplineVariantID = draft.ExistingVariantID
		log.Printf("[Shopline Submit] Updated product %s", shoplineProductID)
	} else {
		// CREATE
		body := map[string]interface{}{"product": productPayload}
		result, err := client.do(ctx, "POST", "/products.json", body)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"ok": false, "error": fmt.Sprintf("create product: %v", err)})
			return
		}
		if p, ok := result["product"].(map[string]interface{}); ok {
			switch id := p["id"].(type) {
			case string:
				shoplineProductID = id
			case float64:
				shoplineProductID = fmt.Sprintf("%.0f", id)
			}
			if variants, ok := p["variants"].([]interface{}); ok && len(variants) > 0 {
				if v, ok := variants[0].(map[string]interface{}); ok {
					switch id := v["id"].(type) {
					case string:
						shoplineVariantID = id
					case float64:
						shoplineVariantID = fmt.Sprintf("%.0f", id)
					}
				}
			}
			if handle, ok := p["handle"].(string); ok && handle != "" {
				shoplineDomain = client.shopID + ".myshopline.com"
				resp.URL = fmt.Sprintf("https://%s/products/%s", shoplineDomain, handle)
			}
		}
		log.Printf("[Shopline Submit] Created product %s / variant %s", shoplineProductID, shoplineVariantID)
	}

	resp.ShoplineProductID = shoplineProductID
	resp.ShoplineVariantID = shoplineVariantID

	// ── Step 5: Save custom attributes (metafields) ───────────────────
	if shoplineProductID != "" && len(draft.CustomAttributes) > 0 {
		for _, attr := range draft.CustomAttributes {
			if attr.Key == "" || attr.Value == "" {
				continue
			}
			t := attr.Type
			if t == "" {
				t = "text"
			}
			attrPayload := map[string]interface{}{
				"custom_attribute": map[string]interface{}{
					"key":        attr.Key,
					"value":      attr.Value,
					"type":       t,
					"product_id": shoplineProductID,
				},
			}
			if _, err := client.do(ctx, "POST", "/custom_attributes.json", attrPayload); err != nil {
				warnings = append(warnings, fmt.Sprintf("custom attribute %s: %v", attr.Key, err))
			}
		}
	}

	// ── Step 6: Add to collections ────────────────────────────────────
	if shoplineProductID != "" && len(draft.CollectionIDs) > 0 {
		for _, colID := range draft.CollectionIDs {
			collectPayload := map[string]interface{}{
				"collect": map[string]interface{}{
					"product_id":    shoplineProductID,
					"collection_id": colID,
				},
			}
			if _, err := client.do(ctx, "POST", "/collects.json", collectPayload); err != nil {
				warnings = append(warnings, fmt.Sprintf("add to collection %s: %v", colID, err))
			}
		}
	}

	// ── Step 7: Publish to selected channels ──────────────────────────
	if shoplineProductID != "" && len(draft.ChannelIDs) > 0 {
		for _, chanID := range draft.ChannelIDs {
			pubPayload := map[string]interface{}{
				"channel_product": map[string]interface{}{
					"channel_id":  chanID,
					"product_id":  shoplineProductID,
				},
			}
			if _, err := client.do(ctx, "POST", "/channel_products.json", pubPayload); err != nil {
				warnings = append(warnings, fmt.Sprintf("publish to channel %s: %v", chanID, err))
			}
		}
	}

	// ── Step 8: Handle pricing tiers via metafields ───────────────────
	// Shopline supports tiered pricing via custom pricing rules or metafields.
	// We store the tier data as a metafield for display in the storefront theme.
	pricingRulesCreated := 0
	if shoplineProductID != "" && len(draft.PricingTiers) > 0 {
		tiersJSON, _ := json.Marshal(draft.PricingTiers)
		tiersPayload := map[string]interface{}{
			"custom_attribute": map[string]interface{}{
				"key":        "pricing_tiers",
				"value":      string(tiersJSON),
				"type":       "json",
				"product_id": shoplineProductID,
			},
		}
		if _, err := client.do(ctx, "POST", "/custom_attributes.json", tiersPayload); err != nil {
			warnings = append(warnings, fmt.Sprintf("pricing tiers storage: %v", err))
		} else {
			pricingRulesCreated = len(draft.PricingTiers)
		}
	}

	resp.PricingRulesCreated = pricingRulesCreated
	resp.Warnings = warnings

	// ── Step 9: Save listing record to Firestore ──────────────────────
	if req.ProductID != "" && shoplineProductID != "" {
		listing := &models.Listing{
			ListingID:        fmt.Sprintf("shopline_%s_%s", tenantID, shoplineProductID),
			TenantID:         tenantID,
			ProductID:        req.ProductID,
			Channel:          "shopline",
			ChannelAccountID: credID,
			State:            status,
			ChannelIdentifiers: &models.ChannelIdentifiers{
				ListingID: shoplineProductID,
			},
			Overrides: &models.ListingOverrides{
				Title: draft.Title,
			},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		if err := h.repo.SaveListing(ctx, listing); err != nil {
			warnings = append(warnings, fmt.Sprintf("listing record save failed (non-fatal): %v", err))
		}
	}

	log.Printf("[Shopline Submit] DONE — productID=%s, pricingRules=%d, warnings=%d",
		shoplineProductID, pricingRulesCreated, len(warnings))
	c.JSON(http.StatusOK, resp)
}

// ============================================================================
// buildShoplineDraft — maps a PIM product map to a ShoplineDraft
// ============================================================================

func buildShoplineDraft(product map[string]interface{}) *ShoplineDraft {
	draft := &ShoplineDraft{
		Title:       extractString(product, "title"),
		Description: extractString(product, "description"),
		Vendor:      extractString(product, "brand"),
		SKU:         extractString(product, "sku"),
		Status:      "draft",
	}

	// Price — prefer list_price → rrp fallback
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
		if draft.Price != "" {
			if rrp := extractNested(pricing, "rrp"); rrp != nil {
				if amt := extractNumericStr(rrp, "amount"); amt != "" && amt != draft.Price {
					draft.CompareAtPrice = amt
				}
			}
		}
	}
	if draft.Price == "" {
		draft.Price = extractNumericStr(product, "retail_price", "price", "list_price")
	}

	// Quantity
	draft.Quantity = extractNumericStr(product, "stock_quantity", "quantity", "inventory_quantity")

	// Identifiers → Barcode
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
				if u := extractString(am, "url"); u != "" {
					draft.Images = append(draft.Images, u)
				}
			}
		}
	}

	// Tags from attributes
	if attrs, ok := product["attributes"].(map[string]interface{}); ok {
		var tagParts []string
		for k, v := range attrs {
			if val, ok := v.(string); ok && val != "" {
				tagParts = append(tagParts, fmt.Sprintf("%s:%s", k, val))
			}
		}
		draft.Tags = strings.Join(tagParts, ", ")
	}

	// Initialise empty slices
	draft.PricingTiers = []ShoplinePricingTier{}
	draft.BulletPoints = []string{}
	draft.TagsList = []string{}

	return draft
}
