package handlers

// ============================================================================
// PRODUCT AI LOOKUP HANDLER
// ============================================================================
// POST /api/v1/products/ai-lookup
//
// Looks up a product by identifier (EAN, ASIN, UPC, ISBN) from a marketplace
// catalogue, creates a draft product record, runs extended data enrichment,
// and returns the product_id for the frontend to navigate to.
//
// EAN  → eBay Browse API (BrowseSearchByGTIN) → enrichment phases 2+3
// ASIN → Amazon SP-API GetCatalogItem
// UPC  → Amazon SP-API SearchCatalogItemsByIdentifier(identifiersType=UPC)
// ISBN → Amazon SP-API SearchCatalogItemsByIdentifier(identifiersType=ISBN)
//
// The created product has status="draft" and a flag
// extended_data.ai_lookup_draft=true. If the user cancels without saving,
// the frontend calls DELETE /products/:id to clean it up.
// ============================================================================

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"module-a/marketplace/clients/amazon"
	"module-a/marketplace/clients/ebay"
	"module-a/models"
	"module-a/repository"
	"module-a/services"
)

type ProductAILookupHandler struct {
	productService    *services.ProductService
	marketplaceRepo   *repository.MarketplaceRepository
	marketplaceSvc    *services.MarketplaceService
	ebayEnrichService *services.EbayEnrichmentService
	mpRepo            *repository.MarketplaceRepository
}

func NewProductAILookupHandler(
	productService *services.ProductService,
	marketplaceRepo *repository.MarketplaceRepository,
	marketplaceSvc *services.MarketplaceService,
	ebayEnrichService *services.EbayEnrichmentService,
) *ProductAILookupHandler {
	return &ProductAILookupHandler{
		productService:    productService,
		marketplaceRepo:   marketplaceRepo,
		marketplaceSvc:    marketplaceSvc,
		ebayEnrichService: ebayEnrichService,
		mpRepo:            marketplaceRepo,
	}
}

// AILookupRequest is the request body for POST /products/ai-lookup
type AILookupRequest struct {
	SKU             string `json:"sku" binding:"required"`
	IdentifierType  string `json:"identifierType" binding:"required"` // EAN, ASIN, UPC, ISBN
	IdentifierValue string `json:"identifierValue" binding:"required"`
	CredentialID    string `json:"credentialId"` // optional — auto-selected if empty
}

// AILookupPreview is the data returned before the product is committed
type AILookupPreview struct {
	ProductID   string `json:"productId"`
	Title       string `json:"title"`
	Brand       string `json:"brand,omitempty"`
	ImageURL    string `json:"imageUrl,omitempty"`
	Description string `json:"description,omitempty"`
	Source      string `json:"source"` // "ebay" or "amazon"
	Found       bool   `json:"found"`
}

// POST /api/v1/products/ai-lookup
func (h *ProductAILookupHandler) Lookup(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	var req AILookupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	req.IdentifierType = strings.ToUpper(strings.TrimSpace(req.IdentifierType))
	req.IdentifierValue = strings.TrimSpace(req.IdentifierValue)
	req.SKU = strings.TrimSpace(req.SKU)

	switch req.IdentifierType {
	case "EAN":
		h.lookupEAN(c, ctx, tenantID, req)
	case "ASIN":
		h.lookupAmazon(c, ctx, tenantID, req, "ASIN")
	case "UPC":
		h.lookupAmazon(c, ctx, tenantID, req, "UPC")
	case "ISBN":
		h.lookupAmazon(c, ctx, tenantID, req, "ISBN")
	default:
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "identifierType must be EAN, ASIN, UPC, or ISBN"})
	}
}

// ── EAN via eBay Browse API ───────────────────────────────────────────────────

func (h *ProductAILookupHandler) lookupEAN(c *gin.Context, ctx context.Context, tenantID string, req AILookupRequest) {
	ebayClient, err := h.getEbayClient(ctx, tenantID, req.CredentialID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "eBay connection required for EAN lookup: " + err.Error()})
		return
	}

	summaries, err := ebayClient.BrowseSearchByGTIN(req.IdentifierValue, "EBAY_GB", 10)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"ok": false, "error": "eBay lookup failed: " + err.Error()})
		return
	}
	if len(summaries) == 0 {
		c.JSON(http.StatusOK, gin.H{"ok": true, "found": false, "message": "No products found for EAN " + req.IdentifierValue})
		return
	}

	// Use the first result as the product template
	best := summaries[0]
	title := best.Title
	var imageURL string
	if best.Image != nil {
		imageURL = best.Image.ImageURL
	}

	// Create draft product
	productID, err := h.createDraftProduct(ctx, tenantID, req.SKU, title, "", imageURL, &models.ProductIdentifiers{
		EAN: strPtr(req.IdentifierValue),
	}, "ebay_ean_lookup")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "failed to create product: " + err.Error()})
		return
	}

	// Run enrichment async (phases 2+3) — don't block the response
	go func() {
		bgCtx := context.Background()
		result, err := h.ebayEnrichService.EnrichProduct(
			bgCtx, tenantID, productID,
			"",                  // no ebayItemID — skip phase 1
			req.IdentifierValue, // EAN
			req.CredentialID,
			ebayClient,
		)
		if err != nil {
			log.Printf("[AILookup] enrichment error for %s: %v", productID, err)
		} else {
			log.Printf("[AILookup] enrichment done for %s: %d branches, epid=%s", productID, len(result.BranchesWritten), result.EpidFound)
		}
	}()

	c.JSON(http.StatusOK, gin.H{
		"ok":        true,
		"found":     true,
		"productId": productID,
		"preview": AILookupPreview{
			ProductID: productID,
			Title:     title,
			ImageURL:  imageURL,
			Source:    "ebay",
			Found:     true,
		},
	})
}

// ── ASIN / UPC / ISBN via Amazon SP-API ──────────────────────────────────────

func (h *ProductAILookupHandler) lookupAmazon(c *gin.Context, ctx context.Context, tenantID string, req AILookupRequest, idType string) {
	amzClient, credID, _, err := h.getAmazonClient(ctx, tenantID, req.CredentialID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "Amazon connection required for " + idType + " lookup: " + err.Error()})
		return
	}
	_ = credID

	var item *amazon.CatalogItem

	if idType == "ASIN" {
		item, err = amzClient.GetCatalogItem(ctx, req.IdentifierValue)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"ok": false, "error": "Amazon ASIN lookup failed: " + err.Error()})
			return
		}
	} else {
		results, err := amzClient.SearchCatalogItemsByIdentifier(ctx, idType, req.IdentifierValue)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"ok": false, "error": "Amazon " + idType + " lookup failed: " + err.Error()})
			return
		}
		if results == nil || len(results.Items) == 0 {
			c.JSON(http.StatusOK, gin.H{"ok": true, "found": false, "message": "No products found for " + idType + " " + req.IdentifierValue})
			return
		}
		item = &results.Items[0]
	}

	if item == nil {
		c.JSON(http.StatusOK, gin.H{"ok": true, "found": false, "message": "No product found"})
		return
	}

	// ── Extract from Summaries ────────────────────────────────────────────────
	title := req.IdentifierValue
	brand := ""
	manufacturer := ""
	color := ""
	size := ""
	modelNumber := ""
	for _, s := range item.Summaries {
		if s.ItemName != "" && title == req.IdentifierValue {
			title = s.ItemName
		}
		if s.Brand != "" && brand == "" {
			brand = s.Brand
		}
		if s.Manufacturer != "" && manufacturer == "" {
			manufacturer = s.Manufacturer
		}
		if s.Color != "" && color == "" {
			color = s.Color
		}
		if s.Size != "" && size == "" {
			size = s.Size
		}
		if s.ModelNumber != "" && modelNumber == "" {
			modelNumber = s.ModelNumber
		}
	}

	// ── Extract from Attributes ───────────────────────────────────────────────
	// SP-API attributes are []interface{} arrays of {"value": ..., "language_tag": ...}
	attrStr := func(key string) string {
		if v, ok := item.Attributes[key]; ok {
			if arr, ok := v.([]interface{}); ok && len(arr) > 0 {
				if m, ok := arr[0].(map[string]interface{}); ok {
					if s, ok := m["value"].(string); ok {
						return s
					}
					// numeric value (e.g. package_quantity)
					if n, ok := m["value"].(float64); ok {
						return fmt.Sprintf("%g", n)
					}
				}
			}
		}
		return ""
	}

	// Fall back to attributes if summaries didn't have them
	if brand == "" {
		brand = attrStr("brand")
	}
	if manufacturer == "" {
		manufacturer = attrStr("manufacturer")
	}
	if color == "" {
		color = attrStr("color")
	}
	if modelNumber == "" {
		modelNumber = attrStr("model_number")
	}

	// Additional attributes not on CatalogSummary
	material := attrStr("material")
	itemName := attrStr("item_name") // sometimes has richer title than summaries

	// Use item_name as title fallback if summaries gave us nothing better
	if itemName != "" && title == req.IdentifierValue {
		title = itemName
	}

	// Description
	description := attrStr("product_description")

	// Bullet points → key_features
	var bulletPoints []string
	if bps, ok := item.Attributes["bullet_point"]; ok {
		if arr, ok := bps.([]interface{}); ok {
			for _, bp := range arr {
				if m, ok := bp.(map[string]interface{}); ok {
					if v, ok := m["value"].(string); ok && v != "" {
						bulletPoints = append(bulletPoints, v)
					}
				}
			}
		}
	}

	// Dimensions — try item_dimensions first, then item_package_dimensions
	// Amazon uses different keys depending on product type
	var dimensions *models.Dimensions
	var weight *models.Weight

	extractDimensions := func(attrKey string) *models.Dimensions {
		attr, ok := item.Attributes[attrKey]
		if !ok { return nil }
		arr, ok := attr.([]interface{})
		if !ok || len(arr) == 0 { return nil }
		m, ok := arr[0].(map[string]interface{})
		if !ok { return nil }
		dims := &models.Dimensions{Unit: "centimeters"}
		getDimVal := func(key string) *float64 {
			sub, ok := m[key].(map[string]interface{})
			if !ok { return nil }
			v, ok := sub["value"].(float64)
			if !ok { return nil }
			if u, ok := sub["unit"].(string); ok { dims.Unit = u }
			return &v
		}
		dims.Length = getDimVal("length")
		dims.Width  = getDimVal("width")
		dims.Height = getDimVal("height")
		if dims.Length == nil && dims.Width == nil && dims.Height == nil { return nil }
		return dims
	}
	if d := extractDimensions("item_dimensions"); d != nil {
		dimensions = d
	} else if d := extractDimensions("item_package_dimensions"); d != nil {
		dimensions = d
	}

	// Weight — try item_weight first, then item_package_weight
	extractWeight := func(attrKey string) *models.Weight {
		attr, ok := item.Attributes[attrKey]
		if !ok { return nil }
		arr, ok := attr.([]interface{})
		if !ok || len(arr) == 0 { return nil }
		m, ok := arr[0].(map[string]interface{})
		if !ok { return nil }
		v, ok := m["value"].(float64)
		if !ok { return nil }
		w := &models.Weight{Unit: "kilograms"}
		if u, ok := m["unit"].(string); ok { w.Unit = u }
		w.Value = &v
		return w
	}
	if w := extractWeight("item_weight"); w != nil {
		weight = w
	} else if w := extractWeight("item_package_weight"); w != nil {
		weight = w
	}

	// ── Pick best image (largest MAIN, fallback to any) ───────────────────────
	imageURL := pickBestImage(item.Images)

	// ── Detect variation type ─────────────────────────────────────────────────
	// ── Variation detection using item.Variations (requires variations in includedData) ──
	// CatalogVariation.Type == "PARENT" means this item IS a child (the parent ASIN is in ASINs)
	// CatalogVariation.Type == "CHILD"  means this item IS a parent (all children are in ASINs)
	variationType := ""
	var variationASINs []string
	parentASIN := ""

	for _, v := range item.Variations {
		switch v.Type {
		case "PARENT":
			// This ASIN is a child — fetch its parent then use parent's CHILD list
			if len(v.ASINs) > 0 {
				parentASIN = v.ASINs[0]
				variationType = "child"
				log.Printf("[AILookup] ASIN %s is a variant child of parent %s", item.ASIN, parentASIN)
			}
		case "CHILD":
			// This ASIN is the parent — v.ASINs are all the children
			variationASINs = v.ASINs
			variationType = "parent"
			log.Printf("[AILookup] ASIN %s is a variation parent with %d children", item.ASIN, len(v.ASINs))
		}
	}

	// If this is a child, fetch the parent and use its CHILD variation list
	if variationType == "child" && parentASIN != "" {
		parentItem, err := amzClient.GetCatalogItem(ctx, parentASIN)
		if err != nil {
			log.Printf("[AILookup] Could not fetch parent %s: %v — single product fallback", parentASIN, err)
			variationType = ""
		} else {
			item = parentItem
			for _, s := range parentItem.Summaries {
				if s.ItemName != "" { title = s.ItemName; break }
			}
			for _, v := range parentItem.Variations {
				if v.Type == "CHILD" && len(v.ASINs) > 0 {
					variationASINs = v.ASINs
					variationType = "parent"
					log.Printf("[AILookup] Parent %s has %d children", parentASIN, len(v.ASINs))
				}
			}
			if variationType != "parent" {
				variationType = "" // parent had no CHILD list
			}
		}
	}


	// ── Identifiers — pull EAN/UPC from the API response too ─────────────────
	identifiers := &models.ProductIdentifiers{}
	switch idType {
	case "ASIN":
		identifiers.ASIN = strPtr(req.IdentifierValue)
	case "UPC":
		identifiers.UPC = strPtr(req.IdentifierValue)
	case "ISBN":
		identifiers.ISBN = strPtr(req.IdentifierValue)
	}
	if item.ASIN != "" {
		identifiers.ASIN = strPtr(item.ASIN)
	}
	for _, idGroup := range item.Identifiers {
		for _, iv := range idGroup.Identifiers {
			switch strings.ToUpper(iv.IdentifierType) {
			case "EAN":
				if identifiers.EAN == nil { identifiers.EAN = strPtr(iv.Identifier) }
			case "UPC":
				if identifiers.UPC == nil { identifiers.UPC = strPtr(iv.Identifier) }
			case "ISBN":
				if identifiers.ISBN == nil { identifiers.ISBN = strPtr(iv.Identifier) }
			case "GTIN":
				if identifiers.GTIN == nil { identifiers.GTIN = strPtr(iv.Identifier) }
			}
		}
	}
	// Fallback: externally_assigned_product_identifier attribute
	// Amazon stores EAN/UPC here when it's not in the identifiers array
	if extIDs, ok := item.Attributes["externally_assigned_product_identifier"]; ok {
		if arr, ok := extIDs.([]interface{}); ok {
			for _, entry := range arr {
				if m, ok := entry.(map[string]interface{}); ok {
					val, _ := m["value"].(string)
					typ, _ := m["type"].(string)
					switch strings.ToLower(typ) {
					case "ean":
						if identifiers.EAN == nil && val != "" { identifiers.EAN = strPtr(val) }
					case "upc":
						if identifiers.UPC == nil && val != "" { identifiers.UPC = strPtr(val) }
					case "isbn":
						if identifiers.ISBN == nil && val != "" { identifiers.ISBN = strPtr(val) }
					}
				}
			}
		}
	}

	source := "amazon_" + strings.ToLower(idType) + "_lookup"

	// ── Variation product: create parent + all child variant products ─────────
	if variationType == "parent" && len(variationASINs) > 0 {
		h.createAmazonVariationProduct(c, ctx, tenantID, req, item, variationASINs,
			amzClient, title, brand, manufacturer, color, size, modelNumber,
			description, bulletPoints, imageURL, identifiers, dimensions, weight, source, idType)
		return
	}

	// ── Simple (non-variation) product ────────────────────────────────────────
	productID, err := h.createDraftProductFull(ctx, tenantID, req.SKU, title, brand, manufacturer,
		color, size, modelNumber, description, bulletPoints, imageURL, identifiers,
		dimensions, weight, source)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "failed to create product: " + err.Error()})
		return
	}
	// Save extra attributes not in the function signature
	if material != "" {
		h.productService.UpdateProduct(ctx, tenantID, productID, map[string]interface{}{
			"attributes.material": material,
		})
	}

	go h.saveAmazonExtendedData(tenantID, productID, req.IdentifierValue, idType, imageURL, description, bulletPoints, item)

	c.JSON(http.StatusOK, gin.H{
		"ok":        true,
		"found":     true,
		"productId": productID,
		"preview": AILookupPreview{
			ProductID: productID,
			Title:     title,
			Brand:     brand,
			ImageURL:  imageURL,
			Source:    "amazon",
			Found:     true,
		},
	})
}

// createAmazonVariationProduct creates a parent product + one child product per variant ASIN.
// The caller-provided ASIN becomes one of the children. Children are fetched concurrently
// but we cap concurrency at 5 to avoid SP-API rate limits.
func (h *ProductAILookupHandler) createAmazonVariationProduct(
	c *gin.Context, ctx context.Context, tenantID string,
	req AILookupRequest, parentItem *amazon.CatalogItem, childASINs []string,
	amzClient *amazon.SPAPIClient,
	parentTitle, brand, manufacturer, color, size, modelNumber, description string,
	bulletPoints []string, parentImageURL string,
	parentIdentifiers *models.ProductIdentifiers,
	dimensions *models.Dimensions, weight *models.Weight,
	source, idType string,
) {
	log.Printf("[AILookup] Creating variation product: parent %s with %d children", parentItem.ASIN, len(childASINs))

	// Create the parent product record
	parentProductID, err := h.createDraftProductFull(
		ctx, tenantID, req.SKU, parentTitle, brand, manufacturer,
		color, size, modelNumber, description, bulletPoints, parentImageURL,
		parentIdentifiers, dimensions, weight, source,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "failed to create parent product: " + err.Error()})
		return
	}
	// Override product type to parent
	h.productService.UpdateProduct(ctx, tenantID, parentProductID, map[string]interface{}{"product_type": "parent"})
	go h.saveAmazonExtendedData(tenantID, parentProductID, parentItem.ASIN, "ASIN", parentImageURL, description, bulletPoints, parentItem)

	// Fetch and create each child — cap at 5 concurrent goroutines
	type childResult struct {
		asin      string
		productID string
		err       error
	}

	sem := make(chan struct{}, 5)
	resultCh := make(chan childResult, len(childASINs))

	for i, childASIN := range childASINs {
		// Generate a child SKU: base SKU + index suffix
		childSKU := fmt.Sprintf("%s-%d", req.SKU, i+1)
		asin := childASIN
		go func() {
			sem <- struct{}{}
			defer func() { <-sem }()

			// Fetch this child's full data
			childItem, ferr := amzClient.GetCatalogItem(ctx, asin)
			if ferr != nil {
				log.Printf("[AILookup] Failed to fetch child ASIN %s: %v", asin, ferr)
				resultCh <- childResult{asin: asin, err: ferr}
				return
			}

			// Extract child-specific fields
			childTitle := parentTitle
			childColor := color
			childSize := size
			for _, s := range childItem.Summaries {
				if s.ItemName != "" { childTitle = s.ItemName }
				if s.Color != "" { childColor = s.Color }
				if s.Size != "" { childSize = s.Size }
			}
			childImage := pickBestImage(childItem.Images)
			if childImage == "" { childImage = parentImageURL }

			childIdentifiers := &models.ProductIdentifiers{ASIN: strPtr(asin)}
			for _, idGroup := range childItem.Identifiers {
				for _, iv := range idGroup.Identifiers {
					switch strings.ToUpper(iv.IdentifierType) {
					case "EAN":
						if childIdentifiers.EAN == nil { childIdentifiers.EAN = strPtr(iv.Identifier) }
					case "UPC":
						if childIdentifiers.UPC == nil { childIdentifiers.UPC = strPtr(iv.Identifier) }
					}
				}
			}

			childProductID, cerr := h.createDraftProductFull(
				ctx, tenantID, childSKU, childTitle, brand, manufacturer,
				childColor, childSize, modelNumber, description, bulletPoints,
				childImage, childIdentifiers, dimensions, weight, source,
			)
			if cerr != nil {
				resultCh <- childResult{asin: asin, err: cerr}
				return
			}

			// Link child to parent and set type to variant
			h.productService.UpdateProduct(ctx, tenantID, childProductID, map[string]interface{}{
				"product_type": "variant",
				"parent_id":    parentProductID,
			})
			go h.saveAmazonExtendedData(tenantID, childProductID, asin, "ASIN", childImage, description, bulletPoints, childItem)

			resultCh <- childResult{asin: asin, productID: childProductID}
		}()
	}

	// Collect results
	childCount := 0
	errCount := 0
	for range childASINs {
		r := <-resultCh
		if r.err != nil {
			errCount++
			log.Printf("[AILookup] Child %s failed: %v", r.asin, r.err)
		} else {
			childCount++
		}
	}

	log.Printf("[AILookup] Variation product created: parent=%s, children=%d/%d, errors=%d",
		parentProductID, childCount, len(childASINs), errCount)

	c.JSON(http.StatusOK, gin.H{
		"ok":        true,
		"found":     true,
		"productId": parentProductID,
		"isVariation": true,
		"childCount":  childCount,
		"preview": AILookupPreview{
			ProductID: parentProductID,
			Title:     parentTitle,
			Brand:     brand,
			ImageURL:  parentImageURL,
			Source:    "amazon",
			Found:     true,
		},
	})
}

// saveAmazonExtendedData saves full SP-API data to the extended_data subcollection.
func (h *ProductAILookupHandler) saveAmazonExtendedData(
	tenantID, productID, identifier, idType, imageURL, description string,
	bulletPoints []string, item *amazon.CatalogItem,
) {
	bgCtx := context.Background()
	extData := map[string]interface{}{
		"source":          "amazon_ai_lookup",
		"identifier":      identifier,
		"identifier_type": idType,
		"asin":            item.ASIN,
		"attributes":      item.Attributes,
		"summaries":       item.Summaries,
		"identifiers":     item.Identifiers,
		"fetched_at":      time.Now().Format(time.RFC3339),
	}
	if imageURL != "" { extData["main_image"] = imageURL }
	if len(bulletPoints) > 0 { extData["bullet_points"] = bulletPoints }
	if description != "" { extData["description"] = description }

	ext := &models.ExtendedProductData{
		SourceKey: fmt.Sprintf("amazon_asin_%s", identifier),
		ProductID: productID,
		TenantID:  tenantID,
		Source:    "amazon_ai_lookup",
		SourceID:  identifier,
		Data:      extData,
		FetchedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := h.mpRepo.SaveExtendedData(bgCtx, tenantID, ext); err != nil {
		log.Printf("[AILookup] failed to save extended data for %s: %v", productID, err)
	}
}

// pickBestImage selects the largest MAIN image from the SP-API image groups,
// falling back to the first image of any variant if MAIN is not present.
func pickBestImage(images []amazon.CatalogImage) string {
	var bestURL string
	bestArea := 0
	// First pass: MAIN variant only
	for _, group := range images {
		if group.Variant != "MAIN" {
			continue
		}
		for _, img := range group.Images {
			if area := img.Width * img.Height; area > bestArea {
				bestArea = area
				bestURL = img.Link
			}
		}
	}
	if bestURL != "" {
		return bestURL
	}
	// Fallback: any variant
	for _, group := range images {
		for _, img := range group.Images {
			if area := img.Width * img.Height; area > bestArea {
				bestArea = area
				bestURL = img.Link
			}
		}
	}
	return bestURL
}

// ── Shared helpers ────────────────────────────────────────────────────────────

// createDraftProduct is a convenience wrapper for simple cases (EAN/eBay).
func (h *ProductAILookupHandler) createDraftProduct(
	ctx context.Context,
	tenantID, sku, title, brand, imageURL string,
	identifiers *models.ProductIdentifiers,
	source string,
) (string, error) {
	return h.createDraftProductFull(ctx, tenantID, sku, title, brand, "", "", "", "", "", nil, imageURL, identifiers, nil, nil, source)
}

// createDraftProductFull creates a draft product with all enriched fields populated.
func (h *ProductAILookupHandler) createDraftProductFull(
	ctx context.Context,
	tenantID, sku, title, brand, manufacturer, color, size, modelNumber, description string,
	bulletPoints []string,
	imageURL string,
	identifiers *models.ProductIdentifiers,
	dimensions *models.Dimensions,
	weight *models.Weight,
	source string,
) (string, error) {
	productID := uuid.New().String()
	now := time.Now()

	product := &models.Product{
		ProductID:   productID,
		TenantID:    tenantID,
		SKU:         sku,
		Title:       title,
		Status:      "draft",
		ProductType: "simple",
		Identifiers: identifiers,
		CreatedAt:   now,
		UpdatedAt:   now,
		Attributes: map[string]interface{}{
			"ai_lookup_draft":  true,
			"ai_lookup_source": source,
		},
	}

	if brand != "" {
		product.Brand = strPtr(brand)
	}
	if description != "" {
		product.Description = strPtr(description)
	}
	if len(bulletPoints) > 0 {
		product.KeyFeatures = bulletPoints
	}
	if dimensions != nil {
		product.Dimensions = dimensions
	}
	if weight != nil {
		product.Weight = weight
	}

	// Stash additional fields in attributes map
	if manufacturer != "" {
		product.Attributes["manufacturer"] = manufacturer
	}
	if color != "" {
		product.Attributes["color"] = color
	}
	if size != "" {
		product.Attributes["size"] = size
	}
	if modelNumber != "" {
		product.Attributes["model_number"] = modelNumber
	}

	if imageURL != "" {
		product.Assets = []models.ProductAsset{
			{
				AssetID:   uuid.New().String(),
				URL:       imageURL,
				Role:      "primary_image",
				SortOrder: 0,
			},
		}
	}

	if err := h.productService.CreateProduct(ctx, product); err != nil {
		return "", err
	}

	// Populate extended_properties so the Extended tab on the product page shows
	// all the data that was pulled from the AI lookup source.
	extProps := map[string]string{}
	if brand != "" { extProps["brand"] = brand }
	if manufacturer != "" { extProps["manufacturer"] = manufacturer }
	if color != "" { extProps["color"] = color }
	if size != "" { extProps["size"] = size }
	if modelNumber != "" { extProps["model_number"] = modelNumber }
	if description != "" {
		d := description
		if len(d) > 500 { d = d[:500] }
		extProps["description"] = d
	}
	if identifiers != nil {
		if identifiers.EAN != nil && *identifiers.EAN != "" { extProps["ean"] = *identifiers.EAN }
		if identifiers.UPC != nil && *identifiers.UPC != "" { extProps["upc"] = *identifiers.UPC }
		if identifiers.ASIN != nil && *identifiers.ASIN != "" { extProps["asin"] = *identifiers.ASIN }
		if identifiers.ISBN != nil && *identifiers.ISBN != "" { extProps["isbn"] = *identifiers.ISBN }
		if identifiers.GTIN != nil && *identifiers.GTIN != "" { extProps["gtin"] = *identifiers.GTIN }
	}
	if dimensions != nil {
		unit := dimensions.Unit
		if unit == "" { unit = "cm" }
		if dimensions.Length != nil { extProps["dimension_length"] = fmt.Sprintf("%.2f %s", *dimensions.Length, unit) }
		if dimensions.Width != nil { extProps["dimension_width"] = fmt.Sprintf("%.2f %s", *dimensions.Width, unit) }
		if dimensions.Height != nil { extProps["dimension_height"] = fmt.Sprintf("%.2f %s", *dimensions.Height, unit) }
	}
	if weight != nil && weight.Value != nil {
		extProps["weight"] = fmt.Sprintf("%.3f %s", *weight.Value, weight.Unit)
	}
	if imageURL != "" { extProps["main_image_url"] = imageURL }
	if len(bulletPoints) > 0 {
		for i, bp := range bulletPoints {
			if i >= 10 { break }
			extProps[fmt.Sprintf("bullet_point_%d", i+1)] = bp
		}
	}
	extProps["ai_lookup_source"] = source

	if len(extProps) > 0 {
		h.productService.UpdateProduct(ctx, tenantID, productID, map[string]interface{}{
			"extended_properties": extProps,
		})
	}

	return productID, nil
}

func (h *ProductAILookupHandler) getEbayClient(ctx context.Context, tenantID, credentialID string) (*ebay.Client, error) {
	var cred *models.MarketplaceCredential
	if credentialID != "" {
		c, err := h.marketplaceRepo.GetCredential(ctx, tenantID, credentialID)
		if err != nil {
			return nil, err
		}
		cred = c
	} else {
		creds, err := h.marketplaceRepo.ListCredentials(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		for _, c := range creds {
			if (c.Channel == "ebay" || c.Channel == "ebay_sandbox") && c.Active {
				cp := c
				cred = &cp
				break
			}
		}
		if cred == nil {
			return nil, fmt.Errorf("no active eBay credential found")
		}
	}

	merged, err := h.marketplaceSvc.GetFullCredentials(ctx, cred)
	if err != nil {
		return nil, err
	}

	production := merged["environment"] != "sandbox"
	client := ebay.NewClient(merged["client_id"], merged["client_secret"], merged["dev_id"], production)
	if merged["access_token"] != "" {
		client.SetTokens(merged["access_token"], merged["refresh_token"])
	}
	return client, nil
}

func (h *ProductAILookupHandler) getAmazonClient(ctx context.Context, tenantID, credentialID string) (*amazon.SPAPIClient, string, string, error) {
	var cred *models.MarketplaceCredential
	if credentialID != "" {
		c, err := h.marketplaceRepo.GetCredential(ctx, tenantID, credentialID)
		if err != nil {
			return nil, "", "", err
		}
		cred = c
	} else {
		creds, err := h.marketplaceRepo.ListCredentials(ctx, tenantID)
		if err != nil {
			return nil, "", "", err
		}
		// Prefer 'amazon' over 'amazonnew' — amazon credentials are known working
		// with the SP-API. amazonnew OAuth credentials may have different role approvals.
		for _, preferred := range []string{"amazon", "amazonnew"} {
			for _, c := range creds {
				if c.Channel == preferred && c.Active {
					cp := c
					cred = &cp
					break
				}
			}
			if cred != nil {
				break
			}
		}
		if cred == nil {
			return nil, "", "", fmt.Errorf("no active Amazon credential found")
		}
	}

	merged, err := h.marketplaceSvc.GetFullCredentials(ctx, cred)
	if err != nil {
		return nil, "", "", err
	}

	config := &amazon.SPAPIConfig{
		LWAClientID:     merged["lwa_client_id"],
		LWAClientSecret: merged["lwa_client_secret"],
		RefreshToken:    merged["refresh_token"],
		MarketplaceID:   merged["marketplace_id"],
		SellerID:        merged["seller_id"],
	}

	// Log what we have (redact secrets)
	log.Printf("[AILookup] getAmazonClient: channel=%s lwa_client_id_set=%v refresh_token_set=%v marketplace_id=%s seller_id_set=%v",
		cred.Channel,
		config.LWAClientID != "",
		config.RefreshToken != "",
		config.MarketplaceID,
		config.SellerID != "",
	)
	if config.MarketplaceID == "" {
		config.MarketplaceID = "A1F83G8C2ARO7P"
	}
	// Always derive region from marketplace ID — don't trust stored region values
	// which may be wrong or empty. SP-API endpoints are strictly regional.
	euMarketplaces := map[string]bool{
		"A1F83G8C2ARO7P": true, // UK
		"A1PA6795UKMFR9": true, // DE
		"A13V1IB3VIYZZH": true, // FR
		"APJ6JRA9NG5V4":  true, // IT
		"A1RKKUPIHCS9HS": true, // ES
		"A1C3SOZRARQ6R3": true, // PL
		"A2NODRKZP88ZB9": true, // SE
		"A1805IZSGTT6HS": true, // NL
		"A2VIGQ35RCS4UG": true, // AE
	}
	feMarketplaces := map[string]bool{
		"A1VC38T7YXB528": true, // JP
		"A39IBJ37TRP1C6": true, // AU
		"A21TJRUUN4KGV":  true, // IN
	}
	if euMarketplaces[config.MarketplaceID] {
		config.Region = "eu-west-1"
	} else if feMarketplaces[config.MarketplaceID] {
		config.Region = "us-west-2"
	} else {
		config.Region = "us-east-1"
	}
	log.Printf("[AILookup] Amazon client: marketplace=%s region=%s", config.MarketplaceID, config.Region)

	client, err := amazon.NewSPAPIClient(ctx, config)
	if err != nil {
		return nil, "", "", fmt.Errorf("build Amazon client (marketplace=%s region=%s lwa_set=%v refresh_set=%v): %w",
			config.MarketplaceID, config.Region, config.LWAClientID != "", config.RefreshToken != "", err)
	}
	return client, cred.CredentialID, config.MarketplaceID, nil
}

func strPtr(s string) *string { return &s }
