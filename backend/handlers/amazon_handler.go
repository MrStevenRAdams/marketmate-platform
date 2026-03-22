package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"module-a/marketplace/clients/amazon"
	"module-a/models"
	"module-a/repository"
	"module-a/services"
)

// ============================================================================
// AMAZON HANDLER
// ============================================================================
// Handles Amazon SP-API listing endpoints:
// - Product type search + definition (equivalent to Temu category/template)
// - Listing preparation (PIM product → Amazon draft)
// - Listing submission (PUT to Listings Items API)
// - Catalog search (find existing ASINs)
// ============================================================================

type AmazonHandler struct {
	marketplaceService *services.MarketplaceService
	repo               *repository.MarketplaceRepository
	productRepo        *repository.FirestoreRepository
	globalConfigRepo   *repository.GlobalConfigRepository
}

func NewAmazonHandler(
	marketplaceService *services.MarketplaceService,
	repo *repository.MarketplaceRepository,
	productRepo *repository.FirestoreRepository,
	globalConfigRepo *repository.GlobalConfigRepository,
) *AmazonHandler {
	return &AmazonHandler{
		marketplaceService: marketplaceService,
		repo:               repo,
		productRepo:        productRepo,
		globalConfigRepo:   globalConfigRepo,
	}
}

// getAmazonClient resolves credentials and builds an SP-API client.
func (h *AmazonHandler) getAmazonClient(c *gin.Context) (*amazon.SPAPIClient, string, string, error) {
	tenantID := c.GetString("tenant_id")
	credentialID := c.Query("credential_id")

	if credentialID == "" {
		// Try to find first active Amazon credential
		creds, err := h.repo.ListCredentials(c.Request.Context(), tenantID)
		if err != nil {
			return nil, "", "", fmt.Errorf("list credentials: %w", err)
		}
		for _, cred := range creds {
			if cred.Channel == "amazon" && cred.Active {
				credentialID = cred.CredentialID
				break
			}
		}
		if credentialID == "" {
			return nil, "", "", fmt.Errorf("no Amazon credential found — please connect an Amazon account first")
		}
	}

	cred, err := h.repo.GetCredential(c.Request.Context(), tenantID, credentialID)
	if err != nil {
		return nil, "", "", fmt.Errorf("get credential: %w", err)
	}

	mergedCreds, err := h.marketplaceService.GetFullCredentials(c.Request.Context(), cred)
	if err != nil {
		return nil, "", "", fmt.Errorf("merge credentials: %w", err)
	}

	// Map credential fields to SP-API config
	config := &amazon.SPAPIConfig{
		LWAClientID:        mergedCreds["lwa_client_id"],
		LWAClientSecret:    mergedCreds["lwa_client_secret"],
		RefreshToken:       mergedCreds["refresh_token"],
		AWSAccessKeyID:     mergedCreds["aws_access_key_id"],
		AWSSecretAccessKey: mergedCreds["aws_secret_access_key"],
		MarketplaceID:      mergedCreds["marketplace_id"],
		Region:             mergedCreds["region"],
		SellerID:           mergedCreds["seller_id"],
	}

	// Defaults for UK
	if config.MarketplaceID == "" {
		config.MarketplaceID = "A1F83G8C2ARO7P" // Amazon UK
	}
	if config.Region == "" {
		config.Region = "eu-west-1"
	}

	if config.LWAClientID == "" || config.LWAClientSecret == "" || config.RefreshToken == "" {
		return nil, "", "", fmt.Errorf("incomplete Amazon credentials (need lwa_client_id, lwa_client_secret, refresh_token)")
	}

	client, err := amazon.NewSPAPIClient(c.Request.Context(), config)
	if err != nil {
		return nil, "", "", fmt.Errorf("create SP-API client: %w", err)
	}

	return client, credentialID, config.MarketplaceID, nil
}

// ============================================================================
// GET /api/v1/amazon/product-types/search
// ============================================================================
// Searches for Amazon product types matching keywords.

func (h *AmazonHandler) SearchProductTypes(c *gin.Context) {
	client, _, _, err := h.getAmazonClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	keywords := c.Query("keywords")
	itemName := c.Query("item_name")
	if keywords == "" && itemName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "keywords or item_name required"})
		return
	}

	result, err := client.SearchProductTypes(c.Request.Context(), keywords, itemName)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":           true,
		"productTypes": result.ProductTypes,
	})
}

// ============================================================================
// GET /api/v1/amazon/product-types/definition
// ============================================================================
// Returns the JSON Schema definition for a product type.

func (h *AmazonHandler) GetProductTypeDefinition(c *gin.Context) {
	client, _, _, err := h.getAmazonClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	productType := c.Query("product_type")
	if productType == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "product_type required"})
		return
	}

	definition, err := client.GetProductTypeDefinition(c.Request.Context(), productType, "en_GB")
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	// Try to get parsed schema from cache or fetch fresh
	mpID := c.Query("marketplace_id")
	if mpID == "" {
		mpID = "A1F83G8C2ARO7P" // default UK
	}
	parsedSchema, schemaErr := h.getCachedOrFetchSchema(c.Request.Context(), client, productType, mpID, definition)
	if schemaErr != nil {
		log.Printf("[Amazon] Schema fetch/parse failed for %s: %v", productType, schemaErr)
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":           true,
		"definition":   definition,
		"parsedSchema": parsedSchema,
	})
}

// ============================================================================
// GET /api/v1/amazon/catalog/search
// ============================================================================
// Searches the Amazon catalog for existing items.

func (h *AmazonHandler) SearchCatalog(c *gin.Context) {
	client, _, _, err := h.getAmazonClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	keywords := c.Query("keywords")
	if keywords == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "keywords required"})
		return
	}

	result, err := client.SearchCatalogItems(c.Request.Context(), keywords, 10, "")
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":    true,
		"items": result.Items,
		"total": result.NumberOfResults,
	})
}

// ============================================================================
// POST /api/v1/amazon/prepare
// ============================================================================
// Prepares an Amazon listing draft from PIM product data.
// Same pattern as Temu: loads product, extended data, calls APIs, returns draft.

type AmazonPrepareRequest struct {
	ProductID    string `json:"product_id" binding:"required"`
	CredentialID string `json:"credential_id"`
	ProductType  string `json:"product_type"` // Optional override
	ListingID    string `json:"listing_id"`   // Optional — pre-populate from existing imported listing
}

type AmazonDraft struct {
	// Core fields
	Title           string                   `json:"title"`
	Description     string                   `json:"description"`
	BulletPoints    []string                 `json:"bulletPoints"`
	Brand           string                   `json:"brand"`
	SKU             string                   `json:"sku"`
	Price           string                   `json:"price"` // e.g. "12.99"
	Currency        string                   `json:"currency"`
	Condition       string                   `json:"condition"` // new_new, used_like_new, etc.
	Images          []string                 `json:"images"`

	// Product type (category)
	ProductType     string                   `json:"productType"`
	ProductTypeName string                   `json:"productTypeName"`

	// Marketplace — needed for region-specific schemas, browse nodes, and attribute payloads
	MarketplaceID   string                   `json:"marketplaceId"`

	// Attributes from JSON Schema
	Attributes      map[string]interface{}   `json:"attributes"`

	// Fulfillment
	FulfillmentChannel string                `json:"fulfillmentChannel"` // DEFAULT (FBM) or AMAZON_NA/AMAZON_EU (FBA)

	// Dimensions & weight
	Length          string                   `json:"length"`
	Width           string                   `json:"width"`
	Height          string                   `json:"height"`
	Weight          string                   `json:"weight"`
	LengthUnit      string                   `json:"lengthUnit"`
	WeightUnit      string                   `json:"weightUnit"`

	// Identifiers
	EAN             string                   `json:"ean"`
	UPC             string                   `json:"upc"`
	ASIN            string                   `json:"asin"`

	// Existing listing?
	IsUpdate        bool                     `json:"isUpdate"`
	ExistingListing map[string]interface{}   `json:"existingListing,omitempty"`

	// Variants
	Variants        []AmazonVariantDraft     `json:"variants,omitempty"`
	VariationTheme  string                   `json:"variationTheme,omitempty"`

	// Phase 1 — Advanced pricing
	ListPrice       string                   `json:"listPrice,omitempty"`
	SalePrice       string                   `json:"salePrice,omitempty"`
	SalePriceStart  string                   `json:"salePriceStart,omitempty"`
	SalePriceEnd    string                   `json:"salePriceEnd,omitempty"`
	B2BPrice        string                   `json:"b2bPrice,omitempty"`
	B2BTier1Qty     string                   `json:"b2bTier1Qty,omitempty"`
	B2BTier1Price   string                   `json:"b2bTier1Price,omitempty"`
	B2BTier2Qty     string                   `json:"b2bTier2Qty,omitempty"`
	B2BTier2Price   string                   `json:"b2bTier2Price,omitempty"`
	B2BTier3Qty     string                   `json:"b2bTier3Qty,omitempty"`
	B2BTier3Price   string                   `json:"b2bTier3Price,omitempty"`

	// Phase 1 — Fulfillment details
	Quantity        string                   `json:"quantity,omitempty"`
	HandlingTime    string                   `json:"handlingTime,omitempty"`
	RestockDate     string                   `json:"restockDate,omitempty"`

	// Package dimensions
	PkgLength       string                   `json:"pkgLength,omitempty"`
	PkgWidth        string                   `json:"pkgWidth,omitempty"`
	PkgHeight       string                   `json:"pkgHeight,omitempty"`
	PkgWeight       string                   `json:"pkgWeight,omitempty"`
	PkgLengthUnit   string                   `json:"pkgLengthUnit,omitempty"`
	PkgWeightUnit   string                   `json:"pkgWeightUnit,omitempty"`

	// B2B tiers 4-5
	B2BTier4Qty     string                   `json:"b2bTier4Qty,omitempty"`
	B2BTier4Price   string                   `json:"b2bTier4Price,omitempty"`
	B2BTier5Qty     string                   `json:"b2bTier5Qty,omitempty"`
	B2BTier5Price   string                   `json:"b2bTier5Price,omitempty"`

	// Additional identifiers
	ISBN            string                   `json:"isbn,omitempty"`

	// Condition
	ConditionNote   string                   `json:"conditionNote,omitempty"`

	// Phase 2 — Shipping, Tax, Limits
	ShippingTemplate string                  `json:"shippingTemplate,omitempty"`
	ProductTaxCode   string                  `json:"productTaxCode,omitempty"`
	MaxOrderQty      string                  `json:"maxOrderQty,omitempty"`
	ReleaseDate      string                  `json:"releaseDate,omitempty"`
	MinPrice         string                  `json:"minPrice,omitempty"`
	MaxPrice         string                  `json:"maxPrice,omitempty"`

	// Browse nodes
	BrowseNode2     string                   `json:"browseNode2,omitempty"`

	// FLD-05 — "Use main item images only" toggle.
	// When true the submit handler skips per-variant image attributes so all
	// child listings inherit the parent product images.
	UseMainImagesOnly bool `json:"useMainImagesOnly,omitempty"`
}

type AmazonVariantDraft struct {
	ID          string            `json:"id"`
	SKU         string            `json:"sku"`
	Combination map[string]string `json:"combination"`
	Price       string            `json:"price"`
	Stock       string            `json:"stock"`
	Image       string            `json:"image"`
	Active      bool              `json:"active"`
	EAN         string            `json:"ean"`
	ASIN        string            `json:"asin,omitempty"`
}

type AmazonPrepareResponse struct {
	OK                bool                        `json:"ok"`
	Error             string                      `json:"error,omitempty"`
	Product           map[string]interface{}       `json:"product,omitempty"`
	Draft             *AmazonDraft                `json:"draft,omitempty"`
	ProductTypes      interface{}                 `json:"productTypes,omitempty"`
	Definition        interface{}                 `json:"definition,omitempty"`
	PropertyGroups    interface{}                 `json:"propertyGroups,omitempty"`
	ParsedSchema      *amazon.ParsedSchemaResult  `json:"parsedSchema,omitempty"`
	Restrictions      interface{}                 `json:"restrictions,omitempty"`
	// Debug
	DebugListing           interface{} `json:"debugListing,omitempty"`
	DebugExtendedData      interface{} `json:"debugExtendedData,omitempty"`
	DebugErrors            []string    `json:"debugErrors,omitempty"`
	DebugProductTypeSearch interface{} `json:"debugProductTypeSearch,omitempty"` // raw Amazon API response for product type search
}

func (h *AmazonHandler) PrepareAmazonListing(c *gin.Context) {
	// Panic recovery — catch any nil pointer dereferences etc.
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[Amazon Prepare] PANIC RECOVERED: %v", r)
			c.JSON(http.StatusOK, gin.H{
				"ok":    false,
				"error": fmt.Sprintf("Internal panic: %v", r),
			})
		}
	}()

	tenantID := c.GetString("tenant_id")
	log.Printf("[Amazon Prepare] START — tenant=%s", tenantID)

	var req AmazonPrepareRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[Amazon Prepare] Bad request: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}
	log.Printf("[Amazon Prepare] Request: product_id=%s, credential_id=%s, product_type=%s",
		req.ProductID, req.CredentialID, req.ProductType)

	// Track debug errors throughout
	var debugErrors []string

	// ── Step 1: Get SP-API client (non-fatal) ──
	if req.CredentialID != "" {
		c.Request.URL.RawQuery += "&credential_id=" + req.CredentialID
	}

	var client *amazon.SPAPIClient
	var credentialID string

	log.Printf("[Amazon Prepare] Step 1: Creating SP-API client...")
	clientCtx, clientCancel := context.WithTimeout(c.Request.Context(), 6*time.Second)
	origReq := c.Request
	c.Request = c.Request.WithContext(clientCtx)
	client, credentialID, marketplaceID, err := h.getAmazonClient(c)
	c.Request = origReq
	clientCancel()
	if err != nil {
		log.Printf("[Amazon Prepare] Step 1 FAILED: %v", err)
		debugErrors = append(debugErrors, fmt.Sprintf("SP-API client: %v", err))
	} else {
		log.Printf("[Amazon Prepare] Step 1 OK: client created, credentialID=%s", credentialID)
	}

	// ── Step 2: Load PIM product ──
	log.Printf("[Amazon Prepare] Step 2: Loading PIM product %s...", req.ProductID)
	productModel, err := h.productRepo.GetProduct(c.Request.Context(), tenantID, req.ProductID)
	if err != nil {
		log.Printf("[Amazon Prepare] Step 2 FAILED: %v", err)
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": fmt.Sprintf("product not found: %v", err)})
		return
	}

	// ── Step 2b: If this is a child variation, redirect to the parent ──
	// The listing form should always be created on the parent (variable) product —
	// the parent holds the variation theme and we load children as variants.
	var originalChild *models.Product // keep ref to original child for data backfill
	if productModel.ProductType == "variation" && productModel.ParentID != nil && *productModel.ParentID != "" {
		parentID := *productModel.ParentID
		originalChild = productModel
		log.Printf("[Amazon Prepare] Step 2b: product is a child variation, redirecting to parent %s", parentID)
		parentModel, parentErr := h.productRepo.GetProduct(c.Request.Context(), tenantID, parentID)
		if parentErr == nil && parentModel != nil {
			// Parent stub may be sparse — backfill key fields from child if missing
			if parentModel.Title == "" || parentModel.Title == originalChild.Title {
				parentModel.Title = originalChild.Title
			}
			if parentModel.Brand == nil && originalChild.Brand != nil {
				parentModel.Brand = originalChild.Brand
			}
			if len(parentModel.Assets) == 0 && len(originalChild.Assets) > 0 {
				parentModel.Assets = originalChild.Assets
			}
			if len(parentModel.KeyFeatures) == 0 && len(originalChild.KeyFeatures) > 0 {
				parentModel.KeyFeatures = originalChild.KeyFeatures
			}
			productModel = parentModel
			req.ProductID = parentID
			log.Printf("[Amazon Prepare] Step 2b OK: loaded parent product (title=%s)", parentModel.Title)
		} else {
			log.Printf("[Amazon Prepare] Step 2b WARN: could not load parent %s: %v — continuing with child", parentID, parentErr)
			originalChild = nil
		}
	}

	productBytes, _ := json.Marshal(productModel)
	var product map[string]interface{}
	json.Unmarshal(productBytes, &product)
	log.Printf("[Amazon Prepare] Step 2 OK: product loaded (title=%s, type=%s)", extractString(product, "title"), extractString(product, "product_type"))

	// ── Step 3: Load extended data ──
	log.Printf("[Amazon Prepare] Step 3: Loading extended data for product %s...", req.ProductID)
	var amazonRawData map[string]interface{}

	// Query extended_data by product_id — this finds the correct document regardless of source
	extData, err := h.repo.GetExtendedDataByProductID(c.Request.Context(), tenantID, req.ProductID)
	if err == nil && extData != nil {
		if dataField, ok := extData["data"].(map[string]interface{}); ok {
			amazonRawData = dataField
			log.Printf("[Amazon Prepare] Step 3 OK: extended data found with %d fields", len(amazonRawData))
		} else {
			log.Printf("[Amazon Prepare] Step 3: extended data found but no 'data' field inside")
		}
	} else {
		log.Printf("[Amazon Prepare] Step 3: No extended data for product (err=%v)", err)
	}

	// ── Step 4: Build draft ──
	log.Printf("[Amazon Prepare] Step 4: Building draft...")
	draft := buildAmazonDraft(product, amazonRawData)
	draft.MarketplaceID = marketplaceID

	// ── Step 4a: Auto-generate parent SKU if missing ──
	// Parent stubs created during import have no top-level sku field.
	// Resolution order:
	//   1. attributes.source_sku on the parent product itself (rare but possible)
	//   2. First child's attributes.source_sku (this is where import-batch always writes the seller SKU)
	//   3. Synthetic PARENT-{ASIN} / PARENT-{productID[:8]} as last resort
	if draft.SKU == "" {
		// 1. Check attributes.source_sku on the parent itself
		if attrs, ok := product["attributes"].(map[string]interface{}); ok {
			if s, _ := attrs["source_sku"].(string); s != "" {
				draft.SKU = s
				log.Printf("[Amazon Prepare] Step 4a: resolved parent SKU from attributes.source_sku=%s", draft.SKU)
			}
		}
	}
	if draft.SKU == "" && len(draft.Variants) > 0 {
		// 2. Take the first child's source_sku and strip any known size/colour suffixes
		//    to recover the base seller SKU (e.g. "DK002-RED-L" → "DK002").
		//    If the child SKU has no suffix pattern just use it as-is — better than PARENT-.
		firstChildSKU := draft.Variants[0].SKU // already resolved from attributes.source_sku in loadPIMVariants
		if firstChildSKU != "" {
			// Strip trailing -WORD or -WORD-WORD patterns (colour/size suffixes)
			base := firstChildSKU
			parts := strings.Split(firstChildSKU, "-")
			if len(parts) >= 2 {
				base = strings.Join(parts[:len(parts)-1], "-")
			}
			draft.SKU = base
			log.Printf("[Amazon Prepare] Step 4a: derived parent SKU=%s from first child SKU=%s", draft.SKU, firstChildSKU)
		}
	}
	if draft.SKU == "" {
		// 3. Synthetic fallback
		asinVal := extractString(product, "identifiers.asin")
		if asinVal == "" {
			if attrs, ok := product["identifiers"].(map[string]interface{}); ok {
				asinVal, _ = attrs["asin"].(string)
			}
		}
		if asinVal != "" {
			draft.SKU = "PARENT-" + asinVal
		} else {
			draft.SKU = "PARENT-" + req.ProductID[:8]
		}
		log.Printf("[Amazon Prepare] Step 4a: synthetic parent SKU=%s", draft.SKU)
	}

	log.Printf("[Amazon Prepare] Step 4 OK: draft built (SKU=%s, productType=%s, marketplace=%s)", draft.SKU, draft.ProductType, marketplaceID)

	// ── Step 4b: Overlay existing listing data (if listing_id provided) ──
	if req.ListingID != "" {
		log.Printf("[Amazon Prepare] Step 4b: Loading existing listing %s...", req.ListingID)
		existingListing, listErr := h.repo.GetListing(c.Request.Context(), tenantID, req.ListingID)
		if listErr == nil && existingListing != nil {
			if existingListing.FulfillmentChannel != "" {
				draft.FulfillmentChannel = existingListing.FulfillmentChannel
			}
			if existingListing.Price != nil && *existingListing.Price > 0 {
				draft.Price = fmt.Sprintf("%.2f", *existingListing.Price)
			}
			if existingListing.ChannelIdentifiers != nil && existingListing.ChannelIdentifiers.SKU != "" {
				draft.SKU = existingListing.ChannelIdentifiers.SKU
			}
			draft.IsUpdate = true
			log.Printf("[Amazon Prepare] Step 4b OK: overlaid listing data (FC=%s, SKU=%s)", existingListing.FulfillmentChannel, draft.SKU)
		} else {
			log.Printf("[Amazon Prepare] Step 4b: listing not found or error: %v", listErr)
		}
	}
	// ── Step 5: Search product types (only if client available) ──
	var productTypes interface{}
	var debugProductTypeSearch interface{}
	title := extractString(product, "title")
	if req.ProductType != "" {
		draft.ProductType = req.ProductType
		log.Printf("[Amazon Prepare] Step 5: Using override productType=%s", req.ProductType)
	} else if client != nil && draft.ProductType == "" && title != "" {
		log.Printf("[Amazon Prepare] Step 5: Searching product types for title=%s...", title)
		ptResult, ptErr := client.SearchProductTypes(c.Request.Context(), "", title)
		if ptErr != nil {
			log.Printf("[Amazon Prepare] Step 5 FAILED: %v", ptErr)
			debugErrors = append(debugErrors, fmt.Sprintf("SearchProductTypes(title): %v", ptErr))
		} else if ptResult != nil {
			// Capture raw response for debug panel
			debugProductTypeSearch = ptResult
			if len(ptResult.ProductTypes) > 0 {
				// Log the raw first result so we can see all fields
				firstRaw, _ := json.Marshal(ptResult.ProductTypes[0])
				log.Printf("[Amazon Prepare] Step 5: Raw first result: %s", string(firstRaw))
				productTypes = ptResult.ProductTypes
				draft.ProductType = ptResult.ProductTypes[0].GetProductType()
				draft.ProductTypeName = ptResult.ProductTypes[0].DisplayName
				log.Printf("[Amazon Prepare] Step 5 OK: found %d types, using '%s' (display: '%s')",
					len(ptResult.ProductTypes), draft.ProductType, draft.ProductTypeName)
			}
		}
		if draft.ProductType == "" {
			category := extractString(product, "category")
			if category != "" {
				log.Printf("[Amazon Prepare] Step 5: Retrying with category=%s...", category)
				ptResult2, ptErr2 := client.SearchProductTypes(c.Request.Context(), category, "")
				if ptErr2 != nil {
					debugErrors = append(debugErrors, fmt.Sprintf("SearchProductTypes(category): %v", ptErr2))
				} else if ptResult2 != nil && len(ptResult2.ProductTypes) > 0 {
					debugProductTypeSearch = ptResult2
					productTypes = ptResult2.ProductTypes
					draft.ProductType = ptResult2.ProductTypes[0].GetProductType()
					draft.ProductTypeName = ptResult2.ProductTypes[0].DisplayName
				}
			}
		}
	} else if client == nil && draft.ProductType == "" {
		log.Printf("[Amazon Prepare] Step 5: SKIP — no client, cannot search product types")
		debugErrors = append(debugErrors, "No SP-API client — cannot search product types")
	} else {
		log.Printf("[Amazon Prepare] Step 5: SKIP — productType already set or no title")
	}

	// ── Step 6: Fetch product type definition + parsed schema (cached) ──
	var definition interface{}
	var propertyGroups interface{}
	var parsedSchema *amazon.ParsedSchemaResult
	if client != nil && draft.ProductType != "" {
		log.Printf("[Amazon Prepare] Step 6: Fetching definition metadata for %s...", draft.ProductType)
		defResult, defErr := client.GetProductTypeDefinition(c.Request.Context(), draft.ProductType, "en_GB")
		if defErr != nil {
			log.Printf("[Amazon Prepare] Step 6 FAILED (definition): %v", defErr)
			debugErrors = append(debugErrors, fmt.Sprintf("GetProductTypeDefinition: %v", defErr))
		} else if defResult != nil {
			definition = defResult
			propertyGroups = defResult.PropertyGroups
			log.Printf("[Amazon Prepare] Step 6: Definition OK, groups=%d. Now fetching schema (cached)...", len(defResult.PropertyGroups))
			if draft.ProductTypeName == "" {
				draft.ProductTypeName = defResult.DisplayName
			}
			// Get parsed schema — from Firestore cache or fresh download+parse
			cached, schemaErr := h.getCachedOrFetchSchema(c.Request.Context(), client, draft.ProductType, marketplaceID, defResult)
			if schemaErr != nil {
				log.Printf("[Amazon Prepare] Step 6 FAILED (schema): %v", schemaErr)
				debugErrors = append(debugErrors, fmt.Sprintf("Schema fetch/parse: %v", schemaErr))
			} else if cached != nil {
				parsedSchema = cached
				log.Printf("[Amazon Prepare] Step 6 OK: %d attrs, %d rules, %d GPSR",
					len(parsedSchema.Attributes), len(parsedSchema.ConditionalRules), len(parsedSchema.GPSRAttributes))
			}
		}
	} else {
		log.Printf("[Amazon Prepare] Step 6: SKIP (client=%v, productType=%s)", client != nil, draft.ProductType)
		if client == nil {
			debugErrors = append(debugErrors, "No SP-API client — cannot fetch definition")
		} else {
			debugErrors = append(debugErrors, "No productType — use the Product Type picker")
		}
	}

	// ── Step 7: Check restrictions ──
	var restrictions interface{}
	if client != nil && draft.ASIN != "" {
		log.Printf("[Amazon Prepare] Step 7: Checking restrictions for ASIN=%s...", draft.ASIN)
		restrictResult, restrictErr := client.GetListingsRestrictions(c.Request.Context(), draft.ASIN, draft.Condition)
		if restrictErr != nil {
			debugErrors = append(debugErrors, fmt.Sprintf("GetListingsRestrictions: %v", restrictErr))
		} else if restrictResult != nil && len(restrictResult.Restrictions) > 0 {
			restrictions = restrictResult.Restrictions
		}
	}

	// ── Step 8: Check existing listing ──
	log.Printf("[Amazon Prepare] Step 8: Checking existing listings (credentialID=%s)...", credentialID)
	if credentialID != "" {
		existingListing, err := h.repo.FindListingByProductAndAccount(c.Request.Context(), tenantID, req.ProductID, credentialID)
		if err == nil && existingListing != nil {
			draft.IsUpdate = true
			if existingListing.ChannelIdentifiers != nil {
				if existingListing.ChannelIdentifiers.SKU != "" {
					draft.SKU = existingListing.ChannelIdentifiers.SKU
				}
				if existingListing.ChannelIdentifiers.ListingID != "" {
					draft.ASIN = existingListing.ChannelIdentifiers.ListingID
				}
			}
			if client != nil && draft.SKU != "" {
				existingData, getErr := client.GetListingsItemRaw(c.Request.Context(), draft.SKU)
				if getErr == nil && existingData != nil {
					draft.ExistingListing = existingData
					if attrs, ok := existingData["attributes"].(map[string]interface{}); ok {
						for k, v := range attrs {
							if _, exists := draft.Attributes[k]; !exists {
								draft.Attributes[k] = v
							}
						}
					}
				}
			}
			log.Printf("[Amazon Prepare] Step 8: existing listing found (isUpdate=true, SKU=%s)", draft.SKU)
		} else {
			log.Printf("[Amazon Prepare] Step 8: no existing listing found")
		}
	}

	// ── Step 9: Load variants ──
	log.Printf("[Amazon Prepare] Step 9: Loading variants...")
	draft.Variants = loadPIMVariants(c.Request.Context(), h.productRepo, tenantID, req.ProductID, draft)
	log.Printf("[Amazon Prepare] Step 9 OK: %d variants", len(draft.Variants))

	// ── Step 9b: Backfill bullet points from first child if parent has none ──
	// Parent stub products created during import are sparse — key_features live on children.
	if len(draft.BulletPoints) == 0 && len(draft.Variants) > 0 {
		for _, child := range draft.Variants {
			childExt, childExtErr := h.repo.GetExtendedDataByProductID(c.Request.Context(), tenantID, child.ID)
			if childExtErr != nil || childExt == nil {
				continue
			}
			if dataField, ok := childExt["data"].(map[string]interface{}); ok {
				if bps, ok := dataField["attributes"].(map[string]interface{}); ok {
					if bulletPoints, ok := bps["bullet_point"].([]interface{}); ok && len(bulletPoints) > 0 {
						for _, bp := range bulletPoints {
							if m, ok := bp.(map[string]interface{}); ok {
								if val, ok := m["value"].(string); ok && val != "" {
									draft.BulletPoints = append(draft.BulletPoints, val)
								}
							}
						}
						log.Printf("[Amazon Prepare] Step 9b: backfilled %d bullet points from child %s", len(draft.BulletPoints), child.ID)
						break // one child is enough
					}
				}
				// Also check key_features directly on child extended data
				if len(draft.BulletPoints) == 0 {
					if features, ok := dataField["key_features"].([]interface{}); ok {
						for _, f := range features {
							if s, ok := f.(string); ok && s != "" {
								draft.BulletPoints = append(draft.BulletPoints, s)
							}
						}
						if len(draft.BulletPoints) > 0 {
							log.Printf("[Amazon Prepare] Step 9b: backfilled %d key_features from child %s", len(draft.BulletPoints), child.ID)
							break
						}
					}
				}
			}
		}
	}

	// ── Step 10: Collect debug data ──
	log.Printf("[Amazon Prepare] Step 10: Collecting debug data...")
	var debugListing interface{}
	var debugExtendedData interface{}

	// Debug: find listing for this product (use the credential we already have)
	if credentialID != "" {
		dbgListing, dbgErr := h.repo.FindListingByProductAndAccount(c.Request.Context(), tenantID, req.ProductID, credentialID)
		if dbgErr == nil && dbgListing != nil {
			lBytes, _ := json.Marshal(dbgListing)
			var lMap map[string]interface{}
			json.Unmarshal(lBytes, &lMap)
			debugListing = lMap
		} else {
			debugErrors = append(debugErrors, fmt.Sprintf("No listing found for product %s / credential %s", req.ProductID, credentialID))
		}
	}

	// Debug: fetch extended data for this product (same query as Step 3 — reuse result if available)
	debugExtData, dbgExtErr := h.repo.GetExtendedDataByProductID(c.Request.Context(), tenantID, req.ProductID)
	if dbgExtErr == nil && debugExtData != nil {
		debugExtendedData = debugExtData
	} else if dbgExtErr != nil {
		debugErrors = append(debugErrors, fmt.Sprintf("Extended data query: %v", dbgExtErr))
	}
	log.Printf("[Amazon Prepare] Step 10 OK")

	// Log debug info
	log.Printf("[Amazon Prepare] DONE — product=%s, productType=%s, parsedSchema=%v, extData=%v, errors=%d",
		req.ProductID, draft.ProductType, parsedSchema != nil, debugExtendedData != nil, len(debugErrors))
	log.Printf("[Amazon Prepare] debugErrors: %v", debugErrors)

	c.JSON(http.StatusOK, AmazonPrepareResponse{
		OK:                     true,
		Product:                product,
		Draft:                  draft,
		ProductTypes:           productTypes,
		Definition:             definition,
		PropertyGroups:         propertyGroups,
		ParsedSchema:           parsedSchema,
		Restrictions:           restrictions,
		DebugListing:           debugListing,
		DebugExtendedData:      debugExtendedData,
		DebugErrors:            debugErrors,
		DebugProductTypeSearch: debugProductTypeSearch,
	})
}

// ============================================================================
// SCHEMA CACHING
// ============================================================================
// Amazon product type schemas are 5-20MB raw JSON. Parsing them into the
// frontend-friendly ParsedSchemaResult yields ~50-100KB. We cache the parsed
// result in Firestore (platform_config/amazon_schema_{productType}) with a
// 7-day TTL to avoid re-downloading on every page load.

// schemaCache is the Firestore document structure for cached parsed schemas.
type schemaCache struct {
	ProductType string                    `firestore:"productType" json:"productType"`
	CachedAt    time.Time                 `firestore:"cachedAt" json:"cachedAt"`
	ExpiresAt   time.Time                 `firestore:"expiresAt" json:"expiresAt"`
	Parsed      *amazon.ParsedSchemaResult `firestore:"parsed" json:"parsed"`
}

const schemaCacheTTL = 7 * 24 * time.Hour // 7 days

func (h *AmazonHandler) getCachedOrFetchSchema(
	ctx context.Context,
	client *amazon.SPAPIClient,
	productType string,
	marketplaceID string,
	definition *amazon.ProductTypeDefResponse,
) (*amazon.ParsedSchemaResult, error) {
	// Include marketplace in cache key — schemas differ by region
	cacheDocID := fmt.Sprintf("amazon_schema_%s_%s", strings.ToLower(productType), strings.ToLower(marketplaceID))

	// 1. Try Firestore cache first
	if h.globalConfigRepo != nil {
		firestoreClient := h.productRepo.GetClient()
		doc, err := firestoreClient.Collection("platform_config").Doc(cacheDocID).Get(ctx)
		if err == nil && doc.Exists() {
			var cached schemaCache
			if err := doc.DataTo(&cached); err == nil {
				if cached.Parsed != nil && time.Now().Before(cached.ExpiresAt) {
					log.Printf("[Schema Cache] HIT for %s (cached %s, expires %s)",
						productType, cached.CachedAt.Format(time.RFC3339), cached.ExpiresAt.Format(time.RFC3339))
					return cached.Parsed, nil
				}
				log.Printf("[Schema Cache] EXPIRED for %s (expired %s)", productType, cached.ExpiresAt.Format(time.RFC3339))
			}
		} else {
			log.Printf("[Schema Cache] MISS for %s", productType)
		}
	}

	// 2. Download and parse fresh schema
	if client == nil {
		return nil, fmt.Errorf("no SP-API client available to fetch schema")
	}
	if definition == nil {
		return nil, fmt.Errorf("no definition provided")
	}

	log.Printf("[Schema Cache] Fetching fresh schema for %s...", productType)
	parsed, err := client.FetchAndParseSchema(ctx, definition)
	if err != nil {
		return nil, fmt.Errorf("fetch and parse schema: %w", err)
	}
	log.Printf("[Schema Cache] Parsed: %d attrs, %d rules, %d GPSR",
		len(parsed.Attributes), len(parsed.ConditionalRules), len(parsed.GPSRAttributes))

	// 3. Cache in Firestore (best-effort — don't fail if caching fails)
	// Firestore max document size is 1MB. Check parsed size first.
	if h.globalConfigRepo != nil {
		parsedJSON, _ := json.Marshal(parsed)
		parsedSize := len(parsedJSON)
		log.Printf("[Schema Cache] Parsed size for %s: %d bytes (%d KB)", productType, parsedSize, parsedSize/1024)

		if parsedSize < 900_000 { // Leave headroom under 1MB limit
			firestoreClient := h.productRepo.GetClient()
			now := time.Now()
			cache := schemaCache{
				ProductType: productType,
				CachedAt:    now,
				ExpiresAt:   now.Add(schemaCacheTTL),
				Parsed:      parsed,
			}
			_, writeErr := firestoreClient.Collection("platform_config").Doc(cacheDocID).Set(ctx, cache)
			if writeErr != nil {
				log.Printf("[Schema Cache] Failed to write cache for %s: %v", productType, writeErr)
			} else {
				log.Printf("[Schema Cache] Cached %s (expires %s)", productType, cache.ExpiresAt.Format(time.RFC3339))
			}
		} else {
			log.Printf("[Schema Cache] SKIP cache for %s — parsed too large (%d KB, max ~900 KB)", productType, parsedSize/1024)
		}
	}

	return parsed, nil
}

// ============================================================================
// POST /api/v1/amazon/submit
// ============================================================================
// Submits the listing to Amazon via PUT /listings/2021-08-01/items/{sellerId}/{sku}.
// Always returns both request payload and response for debug panel.

type AmazonSubmitRequest struct {
	ProductID          string                 `json:"product_id" binding:"required"`
	CredentialID       string                 `json:"credential_id"`
	SKU                string                 `json:"sku" binding:"required"`
	ProductType        string                 `json:"productType" binding:"required"`
	Attributes         map[string]interface{} `json:"attributes" binding:"required"`
	Requirements       string                 `json:"requirements"`
	FulfillmentChannel string                 `json:"fulfillmentChannel"`
	// For variant products: child SKUs
	ChildListings      []AmazonChildListing   `json:"childListings,omitempty"`
}

type AmazonChildListing struct {
	SKU         string                 `json:"sku"`
	Attributes  map[string]interface{} `json:"attributes"`
}

func (h *AmazonHandler) SubmitAmazonListing(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req AmazonSubmitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	// Get client
	if req.CredentialID != "" {
		c.Request.URL.RawQuery += "&credential_id=" + req.CredentialID
	}
	client, credentialID, _, err := h.getAmazonClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	// Set default requirements
	requirements := req.Requirements
	if requirements == "" {
		requirements = "LISTING"
	}

	// Submit parent/main listing
	result, err := client.PutListingItem(c.Request.Context(), req.SKU, req.ProductType, req.Attributes, requirements)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"ok":    false,
			"error": err.Error(),
		})
		return
	}

	// Submit child listings for variants
	var childResults []map[string]interface{}
	for _, child := range req.ChildListings {
		if child.SKU == "" {
			continue
		}
		childResult, childErr := client.PutListingItem(c.Request.Context(), child.SKU, req.ProductType, child.Attributes, requirements)
		childEntry := map[string]interface{}{
			"sku":     child.SKU,
			"success": false,
		}
		if childErr == nil && childResult != nil {
			childEntry["success"] = childResult.Success
			childEntry["status"] = childResult.Status
			childEntry["issues"] = childResult.Issues
			childEntry["response"] = childResult.Response
		} else if childErr != nil {
			childEntry["error"] = childErr.Error()
		}
		childResults = append(childResults, childEntry)
	}

	// Save/update listing in Firestore
	now := time.Now()
	state := "published"
	if !result.Success {
		state = "error"
	}

	if credentialID == "" {
		credentialID = req.CredentialID
	}

	existingListing, _ := h.repo.FindListingByProductAndAccount(c.Request.Context(), tenantID, req.ProductID, credentialID)
	if existingListing != nil {
		// Update
		existingListing.State = state
		existingListing.ChannelIdentifiers = &models.ChannelIdentifiers{
			SKU: req.SKU,
		}
		existingListing.UpdatedAt = now
		if err := h.repo.UpdateListing(c.Request.Context(), existingListing); err != nil {
			log.Printf("[Amazon] WARNING: listing submitted but Firestore update failed: %v", err)
		}
	} else {
		// Create new
		listing := &models.Listing{
			ListingID:        fmt.Sprintf("amz-%s-%d", req.SKU, now.Unix()),
			TenantID:         tenantID,
			ProductID:        req.ProductID,
			Channel:          "amazon",
			ChannelAccountID: credentialID,
			State:            state,
			ChannelIdentifiers: &models.ChannelIdentifiers{
				SKU: req.SKU,
			},
			Overrides: &models.ListingOverrides{
				Title:           amzExtractAttrString(req.Attributes, "item_name"),
				CategoryMapping: req.ProductType,
			},
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := h.repo.CreateListing(c.Request.Context(), listing); err != nil {
			log.Printf("[Amazon] WARNING: listing submitted but Firestore create failed: %v", err)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":            result.Success,
		"status":        result.Status,
		"submissionId":  result.SubmissionID,
		"issues":        result.Issues,
		"request":       result.Request,
		"response":      result.Response,
		"childResults":  childResults,
	})
}

// ============================================================================
// GET /api/v1/amazon/listing
// ============================================================================
// Fetches an existing Amazon listing by SKU (raw).

func (h *AmazonHandler) GetListing(c *gin.Context) {
	client, _, _, err := h.getAmazonClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	sku := c.Query("sku")
	if sku == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "sku required"})
		return
	}

	data, err := client.GetListingsItemRaw(c.Request.Context(), sku)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"listing": data,
	})
}

// ============================================================================
// GET /api/v1/amazon/restrictions
// ============================================================================
// Checks whether the seller is restricted from listing a specific ASIN.
// Returns approval requirements, category gates, brand gates, etc.

func (h *AmazonHandler) CheckRestrictions(c *gin.Context) {
	client, _, _, err := h.getAmazonClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	asin := c.Query("asin")
	if asin == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "asin is required"})
		return
	}
	conditionType := c.Query("condition_type")

	result, err := client.GetListingsRestrictions(c.Request.Context(), asin, conditionType)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	// Determine if there are blocking restrictions
	hasBlocking := false
	for _, r := range result.Restrictions {
		if len(r.Reasons) > 0 {
			hasBlocking = true
			break
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":           true,
		"restricted":   hasBlocking,
		"restrictions": result.Restrictions,
	})
}

// ============================================================================
// POST /api/v1/amazon/validate
// ============================================================================
// Dry-run validation using mode=VALIDATION_PREVIEW.
// Same payload as submit, but nothing is persisted.

func (h *AmazonHandler) ValidateListing(c *gin.Context) {
	var req AmazonSubmitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	if req.CredentialID != "" {
		c.Request.URL.RawQuery += "&credential_id=" + req.CredentialID
	}
	client, _, _, err := h.getAmazonClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	result, err := client.ValidateListingPreview(c.Request.Context(), req.SKU, req.ProductType, req.Attributes)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"ok":    false,
			"error": err.Error(),
		})
		return
	}

	// Count errors vs warnings
	errorCount := 0
	warningCount := 0
	for _, iss := range result.Issues {
		if iss.Severity == "ERROR" {
			errorCount++
		} else {
			warningCount++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":           result.Success,
		"status":       result.Status,
		"issues":       result.Issues,
		"errorCount":   errorCount,
		"warningCount": warningCount,
		"request":      result.Request,
		"response":     result.Response,
	})
}

// ============================================================================
// HELPERS
// ============================================================================

// buildAmazonDraft maps PIM product + extended data into an AmazonDraft.
func buildAmazonDraft(product map[string]interface{}, amazonData map[string]interface{}) *AmazonDraft {
	draft := &AmazonDraft{
		Title:              extractString(product, "title"),
		Description:        extractString(product, "description"),
		BulletPoints:       []string{},
		Brand:              extractString(product, "brand"),
		SKU:                extractString(product, "sku"),
		Price:              "",
		Currency:           "GBP",
		Condition:          "new_new",
		Images:             []string{},
		Attributes:         map[string]interface{}{},
		FulfillmentChannel: "DEFAULT", // FBM
		LengthUnit:         "centimeters",
		WeightUnit:         "kilograms",
	}

	// Price from PIM
	if price, ok := product["price"].(float64); ok && price > 0 {
		draft.Price = fmt.Sprintf("%.2f", price)
	}

	// Bullet points from PIM
	if features, ok := product["key_features"].([]interface{}); ok {
		for _, f := range features {
			if s, ok := f.(string); ok && s != "" {
				draft.BulletPoints = append(draft.BulletPoints, s)
			}
		}
	}
	// Also try "bullet_points" top-level field
	if bps, ok := product["bullet_points"].([]interface{}); ok && len(draft.BulletPoints) == 0 {
		for _, bp := range bps {
			if s, ok := bp.(string); ok && s != "" {
				draft.BulletPoints = append(draft.BulletPoints, s)
			}
		}
	}
	// Also try "attributes.bullet_points" — set by import-enrich on product doc
	if len(draft.BulletPoints) == 0 {
		if attrs, ok := product["attributes"].(map[string]interface{}); ok {
			if bps, ok := attrs["bullet_points"].([]interface{}); ok {
				for _, bp := range bps {
					if s, ok := bp.(string); ok && s != "" {
						draft.BulletPoints = append(draft.BulletPoints, s)
					}
				}
			}
		}
	}

	// Images from PIM assets
	if assets, ok := product["assets"].([]interface{}); ok {
		for _, a := range assets {
			if m, ok := a.(map[string]interface{}); ok {
				if u, ok := m["url"].(string); ok && u != "" {
					draft.Images = append(draft.Images, u)
				}
			}
			if s, ok := a.(string); ok && s != "" {
				draft.Images = append(draft.Images, s)
			}
		}
	}
	// Also try "images" field directly
	if imgs, ok := product["images"].([]interface{}); ok && len(draft.Images) == 0 {
		for _, img := range imgs {
			if s, ok := img.(string); ok && s != "" {
				draft.Images = append(draft.Images, s)
			}
		}
	}

	// Dimensions from PIM — stored as nested struct with *float64 pointer fields
	// Firestore serialises *float64 as float64 in map; unit is a separate string field
	if dims, ok := product["dimensions"].(map[string]interface{}); ok {
		getDimF := func(key string) string {
			if v, ok := dims[key].(float64); ok && v > 0 {
				return fmt.Sprintf("%.2f", v)
			}
			return ""
		}
		if s := getDimF("length"); s != "" { draft.Length = s }
		if s := getDimF("width"); s != "" { draft.Width = s }
		if s := getDimF("height"); s != "" { draft.Height = s }
		if u, ok := dims["unit"].(string); ok && u != "" {
			draft.LengthUnit = u
		}
	}

	// Weight from PIM — stored as { value: float64, unit: string }
	if wObj, ok := product["weight"].(map[string]interface{}); ok {
		if v, ok := wObj["value"].(float64); ok && v > 0 {
			draft.Weight = fmt.Sprintf("%.3f", v)
		}
		if u, ok := wObj["unit"].(string); ok && u != "" {
			draft.WeightUnit = u
		}
	} else if w, ok := product["weight"].(float64); ok && w > 0 {
		// legacy flat float
		draft.Weight = fmt.Sprintf("%.3f", w)
	}

	// EAN/UPC from PIM — stored nested under identifiers: { ean: "...", upc: "..." }
	if ids, ok := product["identifiers"].(map[string]interface{}); ok {
		if ean, ok := ids["ean"].(string); ok && ean != "" {
			draft.EAN = ean
		}
		if upc, ok := ids["upc"].(string); ok && upc != "" {
			draft.UPC = upc
		}
		if isbn, ok := ids["isbn"].(string); ok && isbn != "" && draft.EAN == "" {
			draft.EAN = isbn
		}
	}
	// Also check legacy flat fields
	if draft.EAN == "" {
		if ean, ok := product["ean"].(string); ok && ean != "" { draft.EAN = ean }
	}
	if draft.EAN == "" {
		if barcode, ok := product["barcode"].(string); ok && barcode != "" { draft.EAN = barcode }
	}

	// Manufacturer / model from PIM attributes
	if attrs, ok := product["attributes"].(map[string]interface{}); ok {
		if mfr, ok := attrs["manufacturer"].(string); ok && mfr != "" {
			draft.Attributes["manufacturer"] = []map[string]interface{}{{"value": mfr}}
		}
		if mn, ok := attrs["model_number"].(string); ok && mn != "" {
			draft.Attributes["model_number"] = []map[string]interface{}{{"value": mn}}
		}
		if col, ok := attrs["color"].(string); ok && col != "" {
			draft.Attributes["color"] = []map[string]interface{}{{"value": col}}
		}
		if mat, ok := attrs["material"].(string); ok && mat != "" {
			draft.Attributes["material"] = []map[string]interface{}{{"value": mat}}
		}
		if sz, ok := attrs["size"].(string); ok && sz != "" {
			draft.Attributes["size"] = []map[string]interface{}{{"value": sz}}
		}
	}

	// Enrich from Amazon extended data (from previous import)
	if amazonData != nil {
		enrichFromAmazonData(draft, amazonData)
	}

	// Build initial Amazon attributes from draft fields
	buildInitialAttributes(draft)

	return draft
}

// enrichFromAmazonData overlays data from a previous Amazon catalog import.
func enrichFromAmazonData(draft *AmazonDraft, data map[string]interface{}) {
	// Title
	if itemName := amzExtractNestedString(data, "summaries", "itemName"); itemName != "" {
		draft.Title = itemName
	}

	// Brand
	if brand := amzExtractNestedString(data, "summaries", "brand"); brand != "" {
		draft.Brand = brand
	}

	// Product type
	if pts, ok := data["productTypes"].([]interface{}); ok && len(pts) > 0 {
		if pt, ok := pts[0].(map[string]interface{}); ok {
			if ptName, ok := pt["productType"].(string); ok && ptName != "" {
				draft.ProductType = ptName
			}
		}
	}

	// ASIN
	if asin, ok := data["asin"].(string); ok && asin != "" {
		draft.ASIN = asin
	}

	// Images from catalog
	if images, ok := data["images"].([]interface{}); ok {
		for _, imgSet := range images {
			if imgGroup, ok := imgSet.(map[string]interface{}); ok {
				if imgList, ok := imgGroup["images"].([]interface{}); ok {
					for _, img := range imgList {
						if imgDetail, ok := img.(map[string]interface{}); ok {
							if link, ok := imgDetail["link"].(string); ok && link != "" {
								// Only add if not already present
								found := false
								for _, existing := range draft.Images {
									if existing == link {
										found = true
										break
									}
								}
								if !found {
									draft.Images = append(draft.Images, link)
								}
							}
						}
					}
				}
			}
		}
	}

	// Attributes from catalog
	if attrs, ok := data["attributes"].(map[string]interface{}); ok {
		for k, v := range attrs {
			draft.Attributes[k] = v
		}
	}

	// Bullet points from attributes
	if bps, ok := data["attributes"].(map[string]interface{}); ok {
		if bulletPoints, ok := bps["bullet_point"].([]interface{}); ok && len(bulletPoints) > 0 {
			draft.BulletPoints = []string{}
			for _, bp := range bulletPoints {
				if m, ok := bp.(map[string]interface{}); ok {
					if val, ok := m["value"].(string); ok && val != "" {
						draft.BulletPoints = append(draft.BulletPoints, val)
					}
				}
			}
		}
	}

	// Also try pre-extracted bullet_points top-level field (written by AI lookup handler)
	if len(draft.BulletPoints) == 0 {
		if bps, ok := data["bullet_points"].([]interface{}); ok {
			for _, bp := range bps {
				if s, ok := bp.(string); ok && s != "" {
					draft.BulletPoints = append(draft.BulletPoints, s)
				}
			}
		}
	}

	// Description from pre-extracted field
	if draft.Description == "" {
		if desc, ok := data["description"].(string); ok && desc != "" {
			draft.Description = desc
		}
	}

	// EAN/UPC from identifiers array (SP-API format)
	// Extended data stores: identifiers: [{ Identifiers: [{ Identifier: "...", IdentifierType: "EAN" }], MarketplaceID: "..." }]
	if ids, ok := data["identifiers"].([]interface{}); ok {
		for _, idGroup := range ids {
			if g, ok := idGroup.(map[string]interface{}); ok {
				// Handle both capitalised (Firestore struct) and lowercase (JSON) keys
				innerIDs, _ := g["Identifiers"].([]interface{})
				if innerIDs == nil {
					innerIDs, _ = g["identifiers"].([]interface{})
				}
				for _, iv := range innerIDs {
					if m, ok := iv.(map[string]interface{}); ok {
						idVal, _ := m["Identifier"].(string)
						if idVal == "" { idVal, _ = m["identifier"].(string) }
						idType, _ := m["IdentifierType"].(string)
						if idType == "" { idType, _ = m["identifierType"].(string) }
						switch strings.ToUpper(idType) {
						case "EAN":
							if draft.EAN == "" && idVal != "" { draft.EAN = idVal }
						case "UPC":
							if draft.UPC == "" && idVal != "" { draft.UPC = idVal }
						}
					}
				}
			}
		}
	}

	// EAN from externally_assigned_product_identifier attribute (AI lookup extended data)
	if draft.EAN == "" {
		if attrs, ok := data["attributes"].(map[string]interface{}); ok {
			if extIDs, ok := attrs["externally_assigned_product_identifier"].([]interface{}); ok {
				for _, e := range extIDs {
					if m, ok := e.(map[string]interface{}); ok {
						if t, _ := m["type"].(string); strings.ToLower(t) == "ean" {
							if v, ok := m["value"].(string); ok && v != "" {
								draft.EAN = v
							}
						}
					}
				}
			}
		}
	}

	// Dimensions from item_package_dimensions attribute
	if draft.Length == "" {
		if attrs, ok := data["attributes"].(map[string]interface{}); ok {
			for _, dimKey := range []string{"item_dimensions", "item_package_dimensions"} {
				if dimAttr, ok := attrs[dimKey].([]interface{}); ok && len(dimAttr) > 0 {
					if m, ok := dimAttr[0].(map[string]interface{}); ok {
						getDimVal := func(key string) string {
							if sub, ok := m[key].(map[string]interface{}); ok {
								if v, ok := sub["value"].(float64); ok && v > 0 {
									if u, ok := sub["unit"].(string); ok { draft.LengthUnit = u }
									return fmt.Sprintf("%.2f", v)
								}
							}
							return ""
						}
						if s := getDimVal("length"); s != "" && draft.Length == "" { draft.Length = s }
						if s := getDimVal("width"); s != "" && draft.Width == "" { draft.Width = s }
						if s := getDimVal("height"); s != "" && draft.Height == "" { draft.Height = s }
					}
					break
				}
			}
		}
	}

	// Weight from item_package_weight attribute
	if draft.Weight == "" {
		if attrs, ok := data["attributes"].(map[string]interface{}); ok {
			for _, wKey := range []string{"item_weight", "item_package_weight"} {
				if wAttr, ok := attrs[wKey].([]interface{}); ok && len(wAttr) > 0 {
					if m, ok := wAttr[0].(map[string]interface{}); ok {
						if v, ok := m["value"].(float64); ok && v > 0 {
							draft.Weight = fmt.Sprintf("%.3f", v)
							if u, ok := m["unit"].(string); ok { draft.WeightUnit = u }
						}
					}
					break
				}
			}
		}
	}
}

// buildInitialAttributes maps draft fields into the Amazon attributes format.
// Amazon attributes use arrays of objects with marketplace_id and value.
func buildInitialAttributes(draft *AmazonDraft) {
	mp := draft.MarketplaceID
	if mp == "" {
		mp = "A1F83G8C2ARO7P" // Default UK
	}

	// item_name (title)
	if draft.Title != "" {
		if _, exists := draft.Attributes["item_name"]; !exists {
			draft.Attributes["item_name"] = []map[string]interface{}{
				{"value": draft.Title, "marketplace_id": mp},
			}
		}
	}

	// brand
	if draft.Brand != "" {
		if _, exists := draft.Attributes["brand"]; !exists {
			draft.Attributes["brand"] = []map[string]interface{}{
				{"value": draft.Brand},
			}
		}
	}

	// bullet_point
	if len(draft.BulletPoints) > 0 {
		if _, exists := draft.Attributes["bullet_point"]; !exists {
			bps := []map[string]interface{}{}
			for _, bp := range draft.BulletPoints {
				bps = append(bps, map[string]interface{}{"value": bp, "marketplace_id": mp})
			}
			draft.Attributes["bullet_point"] = bps
		}
	}

	// product_description
	if draft.Description != "" {
		if _, exists := draft.Attributes["product_description"]; !exists {
			draft.Attributes["product_description"] = []map[string]interface{}{
				{"value": draft.Description, "marketplace_id": mp},
			}
		}
	}

	// condition_type
	if draft.Condition != "" {
		if _, exists := draft.Attributes["condition_type"]; !exists {
			draft.Attributes["condition_type"] = []map[string]interface{}{
				{"value": draft.Condition},
			}
		}
	}

	// purchasable_offer (price)
	if draft.Price != "" {
		if _, exists := draft.Attributes["purchasable_offer"]; !exists {
			draft.Attributes["purchasable_offer"] = []map[string]interface{}{
				{
					"marketplace_id": mp,
					"currency":       draft.Currency,
					"our_price": []map[string]interface{}{
						{"schedule": []map[string]interface{}{
							{"value_with_tax": draft.Price},
						}},
					},
				},
			}
		}
	}

	// fulfillment_availability
	if draft.FulfillmentChannel != "" {
		if _, exists := draft.Attributes["fulfillment_availability"]; !exists {
			draft.Attributes["fulfillment_availability"] = []map[string]interface{}{
				{"fulfillment_channel_code": draft.FulfillmentChannel},
			}
		}
	}

	// main_product_image_locator (first image)
	if len(draft.Images) > 0 {
		if _, exists := draft.Attributes["main_product_image_locator"]; !exists {
			draft.Attributes["main_product_image_locator"] = []map[string]interface{}{
				{"media_location": draft.Images[0], "marketplace_id": mp},
			}
		}
	}

	// other_product_image_locator (remaining images)
	if len(draft.Images) > 1 {
		if _, exists := draft.Attributes["other_product_image_locator"]; !exists {
			otherImgs := []map[string]interface{}{}
			for _, img := range draft.Images[1:] {
				otherImgs = append(otherImgs, map[string]interface{}{"media_location": img, "marketplace_id": mp})
			}
			draft.Attributes["other_product_image_locator"] = otherImgs
		}
	}

	// externally_assigned_product_identifier (EAN/UPC)
	if draft.EAN != "" || draft.UPC != "" {
		if _, exists := draft.Attributes["externally_assigned_product_identifier"]; !exists {
			ids := []map[string]interface{}{}
			if draft.EAN != "" {
				ids = append(ids, map[string]interface{}{
					"type":           "ean",
					"value":          draft.EAN,
					"marketplace_id": mp,
				})
			}
			if draft.UPC != "" {
				ids = append(ids, map[string]interface{}{
					"type":           "upc",
					"value":          draft.UPC,
					"marketplace_id": mp,
				})
			}
			draft.Attributes["externally_assigned_product_identifier"] = ids
		}
	}
}

// loadPIMVariants loads variation children from the products collection.
// Amazon-imported variations are stored as products with product_type="variation"
// and parent_id pointing to the parent "variable" product — not in the variants subcollection.
func loadPIMVariants(ctx context.Context, repo *repository.FirestoreRepository, tenantID, productID string, draft *AmazonDraft) []AmazonVariantDraft {
	// Query products that are children of this parent
	children, _, err := repo.ListProducts(ctx, tenantID, map[string]interface{}{"parent_id": productID}, 100, 0)
	if err != nil {
		log.Printf("[loadPIMVariants] ListProducts(parent_id=%s) error: %v", productID, err)
		return nil
	}
	if len(children) == 0 {
		log.Printf("[loadPIMVariants] no children found for parent_id=%s", productID)
		return nil
	}
	log.Printf("[loadPIMVariants] found %d children for parent_id=%s", len(children), productID)

	// First pass: collect ALL variation axis keys across all children so no axis is missed
	// (some children may have style=parka while others don't — we still want the column)
	variationAxes := []string{"color", "size", "style", "flavour", "scent", "material"}
	axisKeySet := map[string]bool{}
	for _, child := range children {
		if child.Attributes == nil {
			continue
		}
		for _, axis := range variationAxes {
			if val, ok := child.Attributes[axis]; ok && val != nil && fmt.Sprintf("%v", val) != "" {
				axisKeySet[axis] = true
			}
		}
	}
	// Ordered axis keys — preserves column order
	orderedAxes := []string{}
	for _, axis := range variationAxes {
		if axisKeySet[axis] {
			orderedAxes = append(orderedAxes, axis)
		}
	}

	var result []AmazonVariantDraft
	for _, child := range children {
		// Build combination using the full union of axes so all rows have the same keys
		combination := map[string]string{}
		for _, axis := range orderedAxes {
			if child.Attributes != nil {
				if val, ok := child.Attributes[axis]; ok && val != nil {
					s := fmt.Sprintf("%v", val)
					if s != "" && s != "<nil>" {
						combination[axis] = s
					}
				}
			}
			if combination[axis] == "" {
				combination[axis] = "" // ensure key present for consistent columns
			}
		}

		// Fallback: if no known axes found, use all non-internal attributes
		if len(orderedAxes) == 0 && child.Attributes != nil {
			skip := map[string]bool{
				"parent_asin": true, "asin": true, "marketplace_id": true,
				"source_sku": true, "source_price": true, "source_currency": true,
				"source_quantity": true, "fulfillment_channel": true,
				"item_condition": true, "amazon_status": true, "bullet_points": true,
			}
			for k, val := range child.Attributes {
				if !skip[k] && val != nil {
					combination[k] = fmt.Sprintf("%v", val)
				}
			}
		}

		// SKU: prefer the child product's own SKU, fall back to source_sku from import
		sku := child.SKU
		if sku == "" && child.Attributes != nil {
			if s, ok := child.Attributes["source_sku"].(string); ok {
				sku = s
			}
		}

		// Price: prefer source_price from import attributes, fallback to parent
		price := draft.Price
		if child.Attributes != nil {
			if sp, ok := child.Attributes["source_price"].(float64); ok && sp > 0 {
				price = fmt.Sprintf("%.2f", sp)
			}
		}

		// Image: use child's first asset, fallback to parent's first image
		image := ""
		if len(child.Assets) > 0 && child.Assets[0].URL != "" {
			image = child.Assets[0].URL
		} else if len(draft.Images) > 0 {
			image = draft.Images[0]
		}

		// EAN
		ean := ""
		if child.Identifiers != nil {
			if child.Identifiers.EAN != nil && *child.Identifiers.EAN != "" {
				ean = *child.Identifiers.EAN
			}
		}

		// ASIN from identifiers or attributes
		asin := ""
		if child.Identifiers != nil && child.Identifiers.ASIN != nil {
			asin = *child.Identifiers.ASIN
		} else if child.Attributes != nil {
			if a, ok := child.Attributes["asin"].(string); ok {
				asin = a
			}
		}

		result = append(result, AmazonVariantDraft{
			ID:          child.ProductID,
			SKU:         sku,
			Combination: combination,
			Price:       price,
			Stock:       "0",
			Image:       image,
			Active:      child.Status != "inactive",
			EAN:         ean,
			ASIN:        asin,
		})
	}

	// Variation theme = union of all axis keys present across children
	if len(orderedAxes) > 0 {
		draft.VariationTheme = strings.Join(orderedAxes, "_")
	} else if len(result) > 0 && len(result[0].Combination) > 0 {
		keys := []string{}
		for k := range result[0].Combination {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		draft.VariationTheme = strings.Join(keys, "_")
	}

	return result
}

// amzExtractNestedString extracts a string from a nested structure (e.g. summaries array).
func amzExtractNestedString(m map[string]interface{}, arrayKey, fieldKey string) string {
	if arr, ok := m[arrayKey].([]interface{}); ok && len(arr) > 0 {
		if item, ok := arr[0].(map[string]interface{}); ok {
			if val, ok := item[fieldKey].(string); ok {
				return val
			}
		}
	}
	return ""
}

// amzExtractAttrString gets a string value from Amazon-style attributes.
func amzExtractAttrString(attrs map[string]interface{}, key string) string {
	if arr, ok := attrs[key].([]interface{}); ok && len(arr) > 0 {
		if m, ok := arr[0].(map[string]interface{}); ok {
			if val, ok := m["value"].(string); ok {
				return val
			}
		}
	}
	if arr, ok := attrs[key].([]map[string]interface{}); ok && len(arr) > 0 {
		if val, ok := arr[0]["value"].(string); ok {
			return val
		}
	}
	return ""
}

// ============================================================================
// DEBUG ENRICH ENDPOINT (TEMPORARY — remove after troubleshooting)
// ============================================================================
// POST /api/v1/amazon/debug-enrich
// Body: { "asin": "B08N5WRWNW", "credential_id": "...", "job_id": "..." }
//
// Fires a single SP-API catalog lookup for the given ASIN using the specified
// credential and returns the raw request details and full response body so you
// can see exactly what Amazon returns (or doesn't return) during enrichment.
// Useful for diagnosing: wrong credentials, throttling (429), bad marketplace
// IDs, or ASINs that return empty data.
// ============================================================================

type DebugEnrichRequest struct {
	ASIN         string `json:"asin"`
	CredentialID string `json:"credential_id"`
	JobID        string `json:"job_id,omitempty"`
}

type DebugEnrichResponse struct {
	OK      bool        `json:"ok"`
	ASIN    string      `json:"asin"`
	Debug   interface{} `json:"debug,omitempty"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
	DoneAt  string      `json:"done_at"`
}

func (h *AmazonHandler) DebugEnrich(c *gin.Context) {
	var req DebugEnrichRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "invalid body: " + err.Error()})
		return
	}
	if req.ASIN == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "asin is required"})
		return
	}
	if req.CredentialID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "credential_id is required"})
		return
	}

	tenantID := c.GetHeader("X-Tenant-Id")
	ctx := c.Request.Context()

	// Override the credential_id in the request context so getAmazonClient picks it up
	// We do this by temporarily setting a query param that getAmazonClient reads
	c.Request.Header.Set("X-Credential-Id", req.CredentialID)
	// Also set as query param for the credential lookup path
	q := c.Request.URL.Query()
	q.Set("credential_id", req.CredentialID)
	c.Request.URL.RawQuery = q.Encode()

	client, credentialID, marketplaceID, err := h.getAmazonClient(c)
	if err != nil {
		c.JSON(http.StatusOK, DebugEnrichResponse{
			OK:    false,
			ASIN:  req.ASIN,
			Error: fmt.Sprintf("credential error: %v", err),
			Debug: gin.H{
				"tenant_id":     tenantID,
				"credential_id": req.CredentialID,
				"note":          "Failed to build SP-API client — check credentials in Firestore",
			},
			DoneAt: time.Now().Format(time.RFC3339),
		})
		return
	}

	log.Printf("[DebugEnrich] tenant=%s cred=%s marketplace=%s ASIN=%s", tenantID, credentialID, marketplaceID, req.ASIN)

	t0 := time.Now()
	item, err := client.GetCatalogItem(ctx, req.ASIN)
	elapsed := time.Since(t0).Milliseconds()

	debugInfo := gin.H{
		"tenant_id":       tenantID,
		"credential_id":   credentialID,
		"marketplace_id":  marketplaceID,
		"asin":            req.ASIN,
		"request_url":     fmt.Sprintf("https://sellingpartnerapi-eu.amazon.com/catalog/2022-04-01/items/%s?includedData=attributes,identifiers,images,productTypes,salesRanks,summaries,variations&marketplaceIds=%s", req.ASIN, marketplaceID),
		"elapsed_ms":      elapsed,
		"note":            "Headers omitted for security — check logs for full SigV4 signature details",
	}

	if err != nil {
		c.JSON(http.StatusOK, DebugEnrichResponse{
			OK:    false,
			ASIN:  req.ASIN,
			Error: err.Error(),
			Debug: debugInfo,
			DoneAt: time.Now().Format(time.RFC3339),
		})
		return
	}

	// Check if the response has useful data
	hasData := item != nil
	var summaryTitle string
	if hasData && len(item.Summaries) > 0 {
		summaryTitle = item.Summaries[0].ItemName
	}

	c.JSON(http.StatusOK, DebugEnrichResponse{
		OK:   true,
		ASIN: req.ASIN,
		Debug: gin.H{
			"tenant_id":      tenantID,
			"credential_id":  credentialID,
			"marketplace_id": marketplaceID,
			"elapsed_ms":     elapsed,
			"has_data":       hasData,
			"summary_title":  summaryTitle,
			"request_url":    fmt.Sprintf("https://sellingpartnerapi-eu.amazon.com/catalog/2022-04-01/items/%s?includedData=attributes,identifiers,images,productTypes,salesRanks,summaries,variations&marketplaceIds=%s", req.ASIN, marketplaceID),
		},
		Data:   item,
		DoneAt: time.Now().Format(time.RFC3339),
	})
}
