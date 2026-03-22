package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
	"google.golang.org/api/iterator"
	"module-a/marketplace/clients/temu"
	"module-a/models"
	"module-a/repository"
	"module-a/services"
)

// ============================================================================
// TEMU HANDLER
// ============================================================================
// Handles Temu-specific endpoints for the listing creation flow:
// - Category recommendation + browsing
// - Template fetching (required attributes for a category)
// - Shipping templates
// - Brand/trademark lookup
// - Product submission (bg.local.goods.add)
// - Listing preparation (map enriched data → Temu fields)
// ============================================================================

type TemuHandler struct {
	marketplaceService *services.MarketplaceService
	repo               *repository.MarketplaceRepository
	productRepo        *repository.FirestoreRepository
	fsClient           *firestore.Client
}

func NewTemuHandler(
	marketplaceService *services.MarketplaceService,
	repo *repository.MarketplaceRepository,
	productRepo *repository.FirestoreRepository,
) *TemuHandler {
	return &TemuHandler{
		marketplaceService: marketplaceService,
		repo:               repo,
		productRepo:        productRepo,
	}
}

func (h *TemuHandler) SetFirestoreClient(fs *firestore.Client) {
	h.fsClient = fs
}

func (h *TemuHandler) schemaCategoriesCol() *firestore.CollectionRef {
	return h.fsClient.Collection("marketplaces").Doc("Temu").Collection("categories")
}

func (h *TemuHandler) schemaTemplatesCol() *firestore.CollectionRef {
	return h.fsClient.Collection("marketplaces").Doc("Temu").Collection("templates")
}

// getTemuClient resolves credentials from Firestore and builds a Temu API client.
// Global keys (app_key, app_secret, base_url) come from platform_config.
// Per-tenant keys (access_token) come from the credential record.
// The MarketplaceService merges them together.
func (h *TemuHandler) getTemuClient(c *gin.Context) (*temu.Client, error) {
	tenantID := c.GetString("tenant_id")
	credentialID := c.Query("credential_id")

	if credentialID == "" {
		// Try to find first active Temu credential for this tenant
		creds, err := h.repo.ListCredentials(c.Request.Context(), tenantID)
		if err != nil {
			return nil, fmt.Errorf("list credentials: %w", err)
		}
		for _, cred := range creds {
			if cred.Channel == "temu" && cred.Active {
				credentialID = cred.CredentialID
				break
			}
		}
		if credentialID == "" {
			return nil, fmt.Errorf("no Temu credential found — please connect a Temu account first")
		}
	}

	cred, err := h.repo.GetCredential(c.Request.Context(), tenantID, credentialID)
	if err != nil {
		return nil, fmt.Errorf("get credential: %w", err)
	}

	// Merge global keys (app_key, app_secret, base_url) with per-tenant keys (access_token)
	mergedCreds, err := h.marketplaceService.GetFullCredentials(c.Request.Context(), cred)
	if err != nil {
		return nil, fmt.Errorf("merge credentials: %w", err)
	}

	baseURL := mergedCreds["base_url"]
	appKey := mergedCreds["app_key"]
	appSecret := mergedCreds["app_secret"]
	accessToken := mergedCreds["access_token"]

	if baseURL == "" {
		baseURL = temu.TemuBaseURLEU
	}
	if appKey == "" || appSecret == "" || accessToken == "" {
		return nil, fmt.Errorf("incomplete Temu credentials (need app_key, app_secret, access_token). Check global config and connection settings.")
	}

	return temu.NewClient(baseURL, appKey, appSecret, accessToken), nil
}

// ============================================================================
// POST /api/v1/temu/categories/recommend
// ============================================================================
// Takes a product title and returns recommended Temu categories.
// This is the primary entry point — avoids manual category drill-down.

func (h *TemuHandler) RecommendCategory(c *gin.Context) {
	client, err := h.getTemuClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	var req struct {
		GoodsName string `json:"goodsName"`
		Title     string `json:"title"` // Alias
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "goodsName or title required"})
		return
	}

	name := req.GoodsName
	if name == "" {
		name = req.Title
	}
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "goodsName is required"})
		return
	}

	cats, err := client.RecommendCategory(name)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error(), "items": []interface{}{}})
		return
	}

	// Enrich any leaf that is missing catName or CatPath using the Firestore
	// categories cache. This covers the edge case where cats.get returned no
	// children (the catId itself is a leaf) so we had no name from the API call.
	if h.fsClient != nil && len(cats) > 0 {
		ctx := c.Request.Context()
		for i := range cats {
			cat := &cats[i]

			// Fetch the category doc to get catName if missing
			if cat.CatName == "" {
				doc, err := h.schemaCategoriesCol().Doc(fmt.Sprintf("%d", cat.CatID)).Get(ctx)
				if err == nil && doc.Exists() {
					data := doc.Data()
					cat.CatName = getStrValue(data, "catName")
				}
			}

			// Build full ancestor path if not already populated by resolveToLeaves
			if len(cat.CatPath) == 0 {
				path := []string{}
				cur := cat.CatID
				seen := map[int]bool{}
				for j := 0; j < 10; j++ {
					if seen[cur] {
						break
					}
					seen[cur] = true
					doc, err := h.schemaCategoriesCol().Doc(fmt.Sprintf("%d", cur)).Get(ctx)
					if err != nil || !doc.Exists() {
						break
					}
					data := doc.Data()
					n := getStrValue(data, "catName")
					if n != "" {
						path = append([]string{n}, path...)
					}
					parentID := getIntValue(data, "parentId")
					if parentID == 0 {
						break
					}
					cur = parentID
				}
				if len(path) > 0 {
					cat.CatPath = path
				} else if cat.CatName != "" {
					// Firestore cache empty — at minimum show the leaf name
					cat.CatPath = []string{cat.CatName}
				}
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "items": cats})
}

// ============================================================================
// GET /api/v1/temu/categories?parentId=123
// ============================================================================
// Manual category browsing — drill-down tree. Used as fallback if user
// wants to change the auto-recommended category.

func (h *TemuHandler) GetCategories(c *gin.Context) {
	var parentID *int
	if raw := c.Query("parentId"); raw != "" {
		if pid, err := strconv.Atoi(raw); err == nil {
			parentID = &pid
		}
	}

	// ── Try Firestore cache first ──────────────────────────────────────────
	if h.fsClient != nil {
		ctx := c.Request.Context()
		var docs []*firestore.DocumentSnapshot
		var err error

		if parentID == nil {
			// Root level: parentId == 0 (Firestore stores as int64)
			docs, err = h.schemaCategoriesCol().Where("parentId", "==", int64(0)).Documents(ctx).GetAll()
		} else {
			// Child level: parentId == *parentID
			docs, err = h.schemaCategoriesCol().Where("parentId", "==", int64(*parentID)).Documents(ctx).GetAll()
		}

		log.Printf("[Temu GetCategories] Firestore query returned %d docs for parentId=%v (err=%v)", len(docs), parentID, err)

		if err == nil && len(docs) > 0 {
			cats := make([]temu.TemuCategory, 0, len(docs))
			for _, doc := range docs {
				data := doc.Data()
				cats = append(cats, temu.TemuCategory{
					CatID:    getIntValue(data, "catId"),
					CatName:  getStrValue(data, "catName"),
					ParentID: getIntValue(data, "parentId"),
					Leaf:     func() bool { b, _ := data["leaf"].(bool); return b }(),
					Level:    getIntValue(data, "level"),
				})
			}
			log.Printf("[Temu GetCategories] Cache hit: %d categories for parentId=%v", len(cats), parentID)
			c.JSON(http.StatusOK, gin.H{"ok": true, "items": cats, "source": "cache"})
			return
		}
		log.Printf("[Temu GetCategories] Cache empty for parentId=%v — falling back to live API", parentID)
	}

	// ── Fallback: live Temu API ────────────────────────────────────────────
	client, err := h.getTemuClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error(), "items": []interface{}{}})
		return
	}
	cats, err := client.GetCategories(parentID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error(), "items": []interface{}{}})
		return
	}
	log.Printf("[Temu GetCategories] Live API returned %d categories (parentId=%v)", len(cats), parentID)
	c.JSON(http.StatusOK, gin.H{"ok": true, "items": cats, "source": "api"})
}

// ============================================================================
// GET /api/v1/temu/category/path?catId=123
// ============================================================================
// Builds the full ancestor path for a given catId using the Firestore cache.
// Returns: { ok, path: ["Parent", "Sub", "Leaf"], catId: 123 }

func (h *TemuHandler) GetCategoryPath(c *gin.Context) {
	catIDStr := c.Query("catId")
	if catIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "catId is required"})
		return
	}
	catID, err := strconv.Atoi(catIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "catId must be an integer"})
		return
	}

	if h.fsClient == nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": "Firestore not available", "path": []string{}})
		return
	}

	ctx := c.Request.Context()
	path := []string{}
	cur := catID
	seen := map[int]bool{}

	for i := 0; i < 10; i++ { // max depth guard
		if seen[cur] {
			break
		}
		seen[cur] = true

		doc, err := h.schemaCategoriesCol().Doc(fmt.Sprintf("%d", cur)).Get(ctx)
		if err != nil || !doc.Exists() {
			break
		}
		data := doc.Data()
		name := getStrValue(data, "catName")
		if name != "" {
			path = append([]string{name}, path...) // prepend
		}
		parentID := getIntValue(data, "parentId")
		if parentID == 0 {
			break
		}
		cur = parentID
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "path": path, "catId": catID})
}

// ============================================================================
// GET /api/v1/temu/template?catId=123
// ============================================================================
// Fetches the attribute template for a leaf category.
// Returns the required/optional fields, spec parents, goods properties, etc.

func (h *TemuHandler) GetTemplate(c *gin.Context) {
	catIDStr := c.Query("catId")
	if catIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "catId is required"})
		return
	}
	catID, err := strconv.Atoi(catIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "catId must be an integer"})
		return
	}

	// ── Try Firestore template cache first ────────────────────────────────
	if h.fsClient != nil {
		doc, err := h.schemaTemplatesCol().Doc(catIDStr).Get(c.Request.Context())
		if err == nil && doc.Exists() {
			data := doc.Data()
			log.Printf("[Temu GetTemplate] Served template for catId=%d from Firestore cache", catID)
			c.JSON(http.StatusOK, gin.H{"ok": true, "template": data, "source": "cache"})
			return
		}
		log.Printf("[Temu GetTemplate] Cache miss for catId=%d — falling back to API", catID)
	}

	// ── Fallback: live Temu API ────────────────────────────────────────────
	client, err := h.getTemuClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}
	template, err := client.GetTemplate(catID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	log.Printf("[Temu GetTemplate] Served template for catId=%d from live API", catID)
	c.JSON(http.StatusOK, gin.H{"ok": true, "template": template, "source": "api"})
}

// ============================================================================
// GET /api/v1/temu/shipping-templates
// ============================================================================
// Returns available freight/shipping templates for the connected shop.

func (h *TemuHandler) GetShippingTemplates(c *gin.Context) {
	client, err := h.getTemuClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	templates, defaultID, err := client.GetShippingTemplates()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error(), "templates": []interface{}{}})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":        true,
		"templates": templates,
		"defaultId": defaultID,
	})
}

// ============================================================================
// POST /api/v1/temu/brand/trademark
// ============================================================================
// Looks up brand authorization for the seller's shop.

func (h *TemuHandler) LookupBrandTrademark(c *gin.Context) {
	client, err := h.getTemuClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	var req struct {
		BrandID   *int   `json:"brandId"`
		BrandName string `json:"brandName"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "brandId or brandName required"})
		return
	}

	result, err := client.LookupBrandTrademark(req.BrandID, &req.BrandName)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "result": result})
}

// ============================================================================
// GET /api/v1/temu/brands
// ============================================================================
// Returns all authorized brands for the seller's shop.
// Used by the frontend brand picker dropdown.

func (h *TemuHandler) ListBrands(c *gin.Context) {
	client, err := h.getTemuClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error(), "brands": []interface{}{}})
		return
	}

	brands, err := client.ListAllBrands()
	if err != nil {
		log.Printf("[Temu ListBrands] Error: %v", err)
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error(), "brands": []interface{}{}})
		return
	}

	log.Printf("[Temu ListBrands] Returning %d brands", len(brands))
	c.JSON(http.StatusOK, gin.H{"ok": true, "brands": brands})
}

// ============================================================================
// GET /api/v1/temu/compliance?catId=123
// ============================================================================
// Returns compliance requirements for a specific category.

func (h *TemuHandler) GetCompliance(c *gin.Context) {
	client, err := h.getTemuClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	catIDStr := c.Query("catId")
	if catIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "catId is required"})
		return
	}
	catID, err := strconv.Atoi(catIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "catId must be an integer"})
		return
	}

	rules, err := client.GetComplianceRules(catID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "rules": rules})
}

// ============================================================================
// POST /api/v1/temu/prepare
// ============================================================================
// The key endpoint for the listing creation flow.
// Takes a product ID, fetches enriched data, recommends a Temu category,
// fetches the template, and maps the enriched data to Temu fields.
// Returns a pre-filled listing draft the UI can render for review.

type TemuPrepareRequest struct {
	ProductID    string `json:"product_id" binding:"required"`
	CredentialID string `json:"credential_id"`
	CatID        *int   `json:"catId"` // Optional override — if not set, we auto-recommend
}

type TemuPrepareResponse struct {
	OK         bool                   `json:"ok"`
	Error      string                 `json:"error,omitempty"`
	Product    map[string]interface{} `json:"product,omitempty"`
	Category   *temu.TemuCategory     `json:"category,omitempty"`
	Template   map[string]interface{} `json:"template,omitempty"`
	Draft      *TemuListingDraft      `json:"draft,omitempty"`
	Compliance map[string]interface{} `json:"compliance,omitempty"`
}

type TemuListingDraft struct {
	GoodsID          int64                    `json:"goodsId,omitempty"`
	Title            string                   `json:"title"`
	Description      string                   `json:"description"`
	BulletPoints     []string                 `json:"bulletPoints"`
	CatID            int                      `json:"catId"`
	CatName          string                   `json:"catName"`
	CatPath          []string                 `json:"catPath,omitempty"`
	SKU              string                   `json:"sku"`
	Price            *TemuPrice               `json:"price"`
	Images           []string                 `json:"images"`
	Dimensions       *TemuDimensions          `json:"dimensions,omitempty"`
	Weight           *TemuWeight              `json:"weight,omitempty"`
	Quantity         int                      `json:"quantity"`
	GoodsProperties  []map[string]interface{} `json:"goodsProperties"`
	ShippingTemplate string                   `json:"shippingTemplate,omitempty"`
	Brand            map[string]interface{}   `json:"brand,omitempty"`
	Compliance       map[string]interface{}   `json:"compliance,omitempty"`
	FulfillmentType  int                      `json:"fulfillmentType"`
	ShipmentLimitDay int                      `json:"shipmentLimitDay"`
	OriginRegion1    string                   `json:"originRegion1,omitempty"`
	OriginRegion2    string                   `json:"originRegion2,omitempty"`

	// VAR-01 — loaded from PIM, returned to frontend variant grid
	Variants []ChannelVariantDraft `json:"variants,omitempty"`
}

type TemuPrice struct {
	BaseAmount string `json:"baseAmount"`
	ListAmount string `json:"listAmount,omitempty"`
	Currency   string `json:"currency"`
}

type TemuDimensions struct {
	LengthCM string `json:"lengthCm"`
	WidthCM  string `json:"widthCm"`
	HeightCM string `json:"heightCm"`
}

type TemuWeight struct {
	WeightG string `json:"weightG"`
}

func (h *TemuHandler) PrepareTemuListing(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req TemuPrepareRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	// 1. Get Temu client
	if req.CredentialID != "" {
		c.Request.URL.RawQuery += "&credential_id=" + req.CredentialID
	}
	client, err := h.getTemuClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	// 2. Fetch product from Firestore
	productModel, err := h.productRepo.GetProduct(c.Request.Context(), tenantID, req.ProductID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"ok": false, "error": fmt.Sprintf("product not found: %v", err)})
		return
	}

	// Convert product to map for the draft builder
	productBytes, _ := json.Marshal(productModel)
	var product map[string]interface{}
	json.Unmarshal(productBytes, &product)

	// 3. Fetch extended_data — try temu first (from import), then amazon_catalog
	var enrichedData map[string]interface{}
	var temuRawData map[string]interface{}

	// Try temu extended data first (most relevant for re-listing)
	temuExtData, err := h.repo.GetExtendedDataByProductID(c.Request.Context(), tenantID, req.ProductID)
	if err == nil && temuExtData != nil {
		// Store the raw temu data for the frontend
		if dataField, ok := temuExtData["data"].(map[string]interface{}); ok {
			temuRawData = dataField
		}
	}

	// Also try amazon_catalog enriched data
	extData, err := h.repo.GetExtendedData(c.Request.Context(), tenantID, req.ProductID, "amazon_catalog")
	if err == nil && extData != nil {
		extBytes, _ := json.Marshal(extData)
		json.Unmarshal(extBytes, &enrichedData)
	}

	// 4. Recommend or use provided category
	title := extractString(product, "title")
	var category *temu.TemuCategory

	if req.CatID != nil {
		// User provided a category override — look up catName and build path from Firestore
		catIDStr := fmt.Sprintf("%d", *req.CatID)
		category = &temu.TemuCategory{CatID: *req.CatID, Leaf: true}
		if h.fsClient != nil {
			if doc, ferr := h.schemaCategoriesCol().Doc(catIDStr).Get(c.Request.Context()); ferr == nil && doc.Exists() {
				data := doc.Data()
				category.CatName = getStrValue(data, "catName")
			}
			// Build full ancestor path
			path := []string{}
			cur := *req.CatID
			seen := map[int]bool{}
			for i := 0; i < 10; i++ {
				if seen[cur] {
					break
				}
				seen[cur] = true
				doc, ferr := h.schemaCategoriesCol().Doc(fmt.Sprintf("%d", cur)).Get(c.Request.Context())
				if ferr != nil || !doc.Exists() {
					break
				}
				data := doc.Data()
				name := getStrValue(data, "catName")
				if name != "" {
					path = append([]string{name}, path...)
				}
				parentID := getIntValue(data, "parentId")
				if parentID == 0 {
					break
				}
				cur = parentID
			}
			if len(path) > 0 {
				category.CatPath = path // store path on the category struct temporarily
			}
		}
	} else {
		// Auto-recommend from title
		cats, err := client.RecommendCategory(title)
		if err != nil {
			log.Printf("[Temu Prepare] category recommend failed: %v", err)
		}
		// Pick the first leaf category
		for i := range cats {
			if cats[i].Leaf {
				category = &cats[i]
				break
			}
		}
		if category == nil && len(cats) > 0 {
			category = &cats[0]
		}
	}

	// 5. Fetch template for the category — Firestore cache first, API fallback
	var template map[string]interface{}
	if category != nil {
		catIDStr := fmt.Sprintf("%d", category.CatID)
		if h.fsClient != nil {
			if doc, ferr := h.schemaTemplatesCol().Doc(catIDStr).Get(c.Request.Context()); ferr == nil && doc.Exists() {
				template = doc.Data()
				log.Printf("[Temu Prepare] Template for catId=%d loaded from Firestore cache", category.CatID)
			}
		}
		if template == nil {
			template, err = client.GetTemplate(category.CatID)
			if err != nil {
				log.Printf("[Temu Prepare] template fetch from API failed: %v", err)
			} else {
				log.Printf("[Temu Prepare] Template for catId=%d loaded from live API", category.CatID)
			}
		}
	}

	// 6. Build the draft by mapping enriched data → Temu fields
	draft := buildTemuDraft(product, enrichedData, category, template)

	// 6b. Look up existing Listing to get goodsId (stored at import time)
	if req.CredentialID != "" {
		existingListing, err := h.repo.FindListingByProductAndAccount(c.Request.Context(), tenantID, req.ProductID, req.CredentialID)
		if err == nil && existingListing != nil && existingListing.ChannelIdentifiers != nil {
			if lid := existingListing.ChannelIdentifiers.ListingID; lid != "" {
				if gid, err := strconv.ParseInt(lid, 10, 64); err == nil && gid > 0 {
					draft.GoodsID = gid
					log.Printf("[Temu Prepare] Found existing goodsId=%d from listing for product=%s", gid, req.ProductID)
				}
			}
		}
	}

	// 7. If we have raw Temu data from import, override draft with more accurate data
	if temuRawData != nil {

		// Override catId if we didn't get a user override and temu data has one
		if req.CatID == nil {
			if catIdRaw, ok := temuRawData["catId"].(float64); ok && catIdRaw > 0 {
				draft.CatID = int(catIdRaw)
				if category == nil || !category.Leaf {
					category = &temu.TemuCategory{CatID: int(catIdRaw), Leaf: true}
					// Re-fetch template with the correct category
					template, _ = client.GetTemplate(int(catIdRaw))
				}
			}
		}

		// Bullet points
		if bps, ok := temuRawData["bulletPoints"].([]interface{}); ok && len(bps) > 0 {
			draft.BulletPoints = nil
			for _, bp := range bps {
				if s, ok := bp.(string); ok {
					draft.BulletPoints = append(draft.BulletPoints, s)
				}
			}
		}
		// Also check snake_case version
		if bps, ok := temuRawData["bullet_points"].([]interface{}); ok && len(bps) > 0 && len(draft.BulletPoints) == 0 {
			for _, bp := range bps {
				if s, ok := bp.(string); ok {
					draft.BulletPoints = append(draft.BulletPoints, s)
				}
			}
		}

		// Description
		if desc, ok := temuRawData["goodsDesc"].(string); ok && desc != "" {
			draft.Description = desc
		}

		// Title
		if name, ok := temuRawData["goodsName"].(string); ok && name != "" {
			draft.Title = name
		}

		// Brand
		if tm, ok := temuRawData["goodsTrademark"].(map[string]interface{}); ok {
			draft.Brand = tm
		}

		// Properties
		if props, ok := temuRawData["goodsProperties"].([]interface{}); ok && len(props) > 0 {
			var mapped []map[string]interface{}
			for _, p := range props {
				if m, ok := p.(map[string]interface{}); ok {
					mapped = append(mapped, m)
				}
			}
			if len(mapped) > 0 {
				draft.GoodsProperties = mapped
			}
		}

		// Price from first SKU
		if skuList, ok := temuRawData["skuList"].([]interface{}); ok && len(skuList) > 0 {
			if firstSku, ok := skuList[0].(map[string]interface{}); ok {
				if rp, ok := firstSku["retailPrice"].(map[string]interface{}); ok {
					amt, _ := rp["amount"].(string)
					cur, _ := rp["currency"].(string)
					if amt != "" {
						draft.Price = &TemuPrice{BaseAmount: amt, Currency: cur}
					}
				}
				if lp, ok := firstSku["listPrice"].(map[string]interface{}); ok {
					amt, _ := lp["amount"].(string)
					if amt != "" && draft.Price != nil {
						draft.Price.ListAmount = amt
					}
				}
				if outSku, ok := firstSku["outSkuSn"].(string); ok && outSku != "" {
					draft.SKU = outSku
				}
				if express, ok := firstSku["productExpressInfo"].(map[string]interface{}); ok {
					if volInfo, ok := express["volumeInfo"].(map[string]interface{}); ok {
						l, _ := volInfo["length"].(string)
						w, _ := volInfo["width"].(string)
						hh, _ := volInfo["height"].(string)
						if l != "" || w != "" || hh != "" {
							draft.Dimensions = &TemuDimensions{LengthCM: l, WidthCM: w, HeightCM: hh}
						}
					}
					if wtInfo, ok := express["weightInfo"].(map[string]interface{}); ok {
						wt, _ := wtInfo["weight"].(string)
						if wt != "" {
							draft.Weight = &TemuWeight{WeightG: wt}
						}
					}
				}
			}
		}

		// Shipping template
		if sp, ok := temuRawData["goodsServicePromise"].(map[string]interface{}); ok {
			if costId, ok := sp["costTemplateId"].(string); ok && costId != "" {
				draft.ShippingTemplate = costId
			}
			if ft, ok := sp["fulfillmentType"].(float64); ok {
				draft.FulfillmentType = int(ft)
			}
			if sld, ok := sp["shipmentLimitDay"].(float64); ok {
				draft.ShipmentLimitDay = int(sld)
			}
		}

		// Origin info
		if oi, ok := temuRawData["goodsOriginInfo"].(map[string]interface{}); ok {
			if r1, ok := oi["originRegionName1"].(string); ok {
				draft.OriginRegion1 = r1
			}
			if r2, ok := oi["originRegionName2"].(string); ok {
				draft.OriginRegion2 = r2
			}
		}

		// Images from gallery
		if gallery, ok := temuRawData["goodsGallery"].(map[string]interface{}); ok {
			if detailImages, ok := gallery["detailImage"].([]interface{}); ok && len(detailImages) > 0 {
				var imgs []string
				for _, img := range detailImages {
					if s, ok := img.(string); ok && s != "" {
						imgs = append(imgs, s)
					}
				}
				if len(imgs) > 0 {
					draft.Images = imgs
				}
			}
		}

		// External product code
		if outSn, ok := temuRawData["outGoodsSn"].(string); ok && outSn != "" {
			draft.SKU = outSn
		}
	}

	// 8. Fetch compliance rules for the category
	var compliance map[string]interface{}
	if category != nil && category.CatID > 0 {
		compRules, err := client.GetComplianceRules(category.CatID)
		if err != nil {
			log.Printf("[Temu Prepare] compliance rules fetch failed (non-critical): %v", err)
		} else {
			compliance = compRules
		}
	}

	// 9. Build category path — priority: Firestore ancestry > temu raw data > catName fallback
	if category != nil && len(category.CatPath) > 0 {
		draft.CatPath = category.CatPath
	} else if temuRawData != nil {
		if catPath, ok := temuRawData["catPath"].([]interface{}); ok && len(catPath) > 0 {
			var pathStrs []string
			for _, p := range catPath {
				if s, ok := p.(string); ok {
					pathStrs = append(pathStrs, s)
				}
			}
			if len(pathStrs) > 0 {
				draft.CatPath = pathStrs
			}
		}
	}
	if len(draft.CatPath) == 0 && category != nil && category.CatName != "" {
		draft.CatPath = []string{category.CatName}
	}

	// VAR-01: load PIM variants into the draft
	if draft != nil {
		fallbackPrice := ""
		if draft.Price != nil {
			fallbackPrice = draft.Price.BaseAmount
		}
		fallbackImage := ""
		if len(draft.Images) > 0 {
			fallbackImage = draft.Images[0]
		}
		draft.Variants = loadChannelVariants(c.Request.Context(), h.productRepo, tenantID, req.ProductID, fallbackPrice, fallbackImage)
		log.Printf("[Temu Prepare] Loaded %d variants", len(draft.Variants))
	}

	c.JSON(http.StatusOK, TemuPrepareResponse{
		OK:         true,
		Product:    product,
		Category:   category,
		Template:   template,
		Draft:      draft,
		Compliance: compliance,
	})
}

// ============================================================================
// POST /api/v1/temu/submit
// ============================================================================
// Submits a reviewed listing draft to Temu (bg.local.goods.add).
// Called after the user has reviewed and optionally edited the prepared draft.

type TemuSubmitRequest struct {
	ProductID        string                   `json:"product_id" binding:"required"`
	CredentialID     string                   `json:"credential_id"`
	GoodsID          int64                    `json:"goodsId"`
	CatID            int                      `json:"catId" binding:"required"`
	Title            string                   `json:"title" binding:"required"`
	Description      string                   `json:"description"`
	BulletPoints     []string                 `json:"bulletPoints"`
	SKU              string                   `json:"sku" binding:"required"`
	Images           []string                 `json:"images" binding:"required"`
	Price            TemuPrice                `json:"price" binding:"required"`
	Dimensions       *TemuDimensions          `json:"dimensions"`
	Weight           *TemuWeight              `json:"weight"`
	Quantity         int                      `json:"quantity"`
	GoodsProperties  []map[string]interface{} `json:"goodsProperties"`
	ShippingTemplate string                   `json:"shippingTemplate" binding:"required"`
	Brand            map[string]interface{}   `json:"brand"`
	SpecIdList       []int                    `json:"specIdList"`
	FulfillmentType  int                      `json:"fulfillmentType"`
	PrepDays         int                      `json:"prepDays"`
	OriginInfo       map[string]interface{}   `json:"originInfo"`
	Compliance       map[string]interface{}   `json:"compliance"`

	// VAR-01 — Variation listings (Session H).
	// When len(Variants) >= 2 active entries, the submit handler builds one
	// skuObj per active variant into skuList[], passing them all in a single
	// Temu bg.local.goods.add call. Temu natively supports multi-SKU products.
	Variants []ChannelVariantDraft `json:"variants"`
}

func (h *TemuHandler) SubmitTemuListing(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req TemuSubmitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	// Get client
	if req.CredentialID != "" {
		c.Request.URL.RawQuery += "&credential_id=" + req.CredentialID
	}
	client, err := h.getTemuClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	// Upload images to Temu CDN
	temuImages, err := client.UploadImages(req.Images)
	if err != nil || len(temuImages) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"ok":    false,
			"error": fmt.Sprintf("image upload failed: %v", err),
		})
		return
	}

	// Resolve spec IDs if not provided
	specIdList := req.SpecIdList
	var existingSkuId int64

	// For updates, fetch existing product detail to get specIdList and skuId
	var existingDetail *temu.TemuGoodsDetail
	if req.GoodsID > 0 {
		detail, detailErr := client.GetGoodsDetail(req.GoodsID)
		if detailErr == nil && detail != nil {
			existingDetail = detail

			// Try struct first
			if len(detail.SkuList) > 0 {
				existingSku := detail.SkuList[0]
				existingSkuId = existingSku.SkuID
			}

			// Always try raw extraction (more reliable for skuId, specIdList)
			if detail.Raw != nil {
				if rawSkuList, ok := detail.Raw["skuList"].([]interface{}); ok && len(rawSkuList) > 0 {
					if rawSku, ok := rawSkuList[0].(map[string]interface{}); ok {
						// Extract skuId
						if existingSkuId == 0 {
							if sid, ok := rawSku["skuId"].(float64); ok {
								existingSkuId = int64(sid)
							}
						}
						// Extract specIdList directly from SKU
						if len(specIdList) == 0 {
							if rawSpecIds, ok := rawSku["specIdList"].([]interface{}); ok {
								for _, s := range rawSpecIds {
									if sid, ok := s.(float64); ok {
										specIdList = append(specIdList, int(sid))
									}
								}
								log.Printf("[Temu] Using specIdList from raw SKU: %v", specIdList)
							}
						}
						// Fallback: extract from specList entries
						if len(specIdList) == 0 {
							if rawSpecList, ok := rawSku["specList"].([]interface{}); ok {
								for _, spec := range rawSpecList {
									if specMap, ok := spec.(map[string]interface{}); ok {
										if sid, ok := specMap["specId"].(float64); ok {
											specIdList = append(specIdList, int(sid))
										}
									}
								}
								log.Printf("[Temu] Using specIdList from raw specList: %v", specIdList)
							}
						}
					}
				}
			}
			log.Printf("[Temu] Fetched existing detail: skuId=%d specIdList=%v", existingSkuId, specIdList)
		} else if detailErr != nil {
			log.Printf("[Temu] WARNING: Could not fetch existing detail for goodsId=%d: %v", req.GoodsID, detailErr)
		}
	}

	if len(specIdList) == 0 {
		// Fetch template to get userInputParentSpecList
		template, tplErr := client.GetTemplate(req.CatID)
		if tplErr == nil && template != nil {
			specIdList = resolveSpecIdsFromTemplate(client, req.CatID, template)
		}
		// Fallback: try without parentSpecId using common labels
		if len(specIdList) == 0 {
			for _, label := range []string{"Multicolor", "One Size", "Default", "Mixed Color", "Free Size"} {
				ids, err := client.GetSpecIDs(req.CatID, 0, label)
				if err == nil && len(ids) > 0 {
					specIdList = ids
					break
				}
			}
		}
		if len(specIdList) == 0 {
			log.Printf("[Temu] WARNING: Could not resolve specIdList for catId=%d", req.CatID)
		}
	}

	// Build SKU object
	skuSafe := req.SKU
	if len(skuSafe) > 40 {
		skuSafe = skuSafe[:40]
	}

	skuPrice := map[string]interface{}{
		"basePrice": map[string]interface{}{
			"amount":   req.Price.BaseAmount,
			"currency": req.Price.Currency,
		},
	}
	if req.Price.ListAmount != "" {
		skuPrice["listPrice"] = map[string]interface{}{
			"amount":   req.Price.ListAmount,
			"currency": req.Price.Currency,
		}
	} else {
		skuPrice["listPriceType"] = 1
	}

	// Ensure specIdList is never null (Temu rejects null)
	if specIdList == nil {
		specIdList = []int{}
	}

	skuObj := map[string]interface{}{}

	isUpdate := req.GoodsID > 0

	if isUpdate && existingSkuId > 0 {
		// For partial.update with existing SKU (per Temu docs):
		// - MUST include skuId, dimensions with values, units, images
		// - Can include listPrice
		// - Must NOT include basePrice, quantity, specIdList
		// - outSkuSn: if provided overwrites, if omitted retains original
		skuObj["skuId"] = existingSkuId
		skuObj["images"] = temuImages
		// listPrice can be updated
		if req.Price.ListAmount != "" {
			skuObj["listPrice"] = map[string]interface{}{
				"amount":   req.Price.ListAmount,
				"currency": req.Price.Currency,
			}
		}
		// Dimensions with actual values (required per docs example)
		length, width, height, weight := "1", "1", "1", "1"
		if req.Dimensions != nil {
			if req.Dimensions.LengthCM != "" { length = req.Dimensions.LengthCM }
			if req.Dimensions.WidthCM != "" { width = req.Dimensions.WidthCM }
			if req.Dimensions.HeightCM != "" { height = req.Dimensions.HeightCM }
		}
		if req.Weight != nil && req.Weight.WeightG != "" {
			weight = req.Weight.WeightG
		}
		skuObj["length"] = length
		skuObj["width"] = width
		skuObj["height"] = height
		skuObj["volumeUnit"] = "cm"
		skuObj["weight"] = weight
		skuObj["weightUnit"] = "g"
	} else {
		// For goods.add (new SKU):
		// MUST include basePrice, quantity, specIdList, outSkuSn, images
		skuObj["outSkuSn"] = skuSafe
		skuObj["images"] = temuImages
		skuObj["price"] = skuPrice
		skuObj["specIdList"] = specIdList
		// Quantity only for new SKUs — Temu requires at least 1 for goods.add
		quantity := req.Quantity
		if quantity <= 0 {
			quantity = 1 // Temu minimum for new listings
		}
		skuObj["quantity"] = fmt.Sprintf("%d", quantity)
	}

	// Dimensions only for new SKUs (updates should not include them)
	if !isUpdate {
		length, width, height, weight := "1", "1", "1", "1"
		if req.Dimensions != nil {
			if req.Dimensions.LengthCM != "" { length = req.Dimensions.LengthCM }
			if req.Dimensions.WidthCM != "" { width = req.Dimensions.WidthCM }
			if req.Dimensions.HeightCM != "" { height = req.Dimensions.HeightCM }
		}
		if req.Weight != nil && req.Weight.WeightG != "" {
			weight = req.Weight.WeightG
		}
		skuObj["length"] = length
		skuObj["width"] = width
		skuObj["height"] = height
		skuObj["volumeUnit"] = "cm"
		skuObj["weight"] = weight
		skuObj["weightUnit"] = "g"
	}

	// Build request
	fulfillmentType := req.FulfillmentType
	if fulfillmentType == 0 {
		fulfillmentType = 1 // Default: Merchant Fulfilled
	}
	prepDays := req.PrepDays
	if prepDays == 0 {
		prepDays = 1
	}

	// Ensure bulletPoints is never null (Temu rejects null arrays)
	bulletPoints := req.BulletPoints
	if bulletPoints == nil {
		bulletPoints = []string{}
	}

	// Ensure goodsProperties is never null
	goodsProperties := req.GoodsProperties
	if goodsProperties == nil {
		goodsProperties = []map[string]interface{}{}
	}

	// Filter out invalid properties (pid=0 or missing vid/value)
	filteredProps := []map[string]interface{}{}
	for _, p := range goodsProperties {
		pid, _ := p["pid"]
		if pid == nil || fmt.Sprintf("%v", pid) == "0" {
			continue
		}
		if _, hasVid := p["vid"]; !hasVid {
			if _, hasVal := p["value"]; !hasVal {
				continue
			}
		}
		filteredProps = append(filteredProps, p)
	}

	// For updates, ensure variant spec properties are included in goodsProperties
	// Temu REQUIRES: {"parentSpecId": X, "specId": Y, "value": "..."} in goodsProperties
	// even when modifying existing SKUs — without this, "Some SKU specifications are empty"
	if isUpdate && existingDetail != nil && existingDetail.Raw != nil {
		log.Printf("[Temu] Building spec properties for update. specIdList=%v, existingSkuId=%d", specIdList, existingSkuId)

		// Get parentSpecId for each specId by checking the SKU's specList from detail
		if rawSkuList, ok := existingDetail.Raw["skuList"].([]interface{}); ok && len(rawSkuList) > 0 {
			if rawSku, ok := rawSkuList[0].(map[string]interface{}); ok {
				// Log all keys on the SKU
				skuKeys := []string{}
				for k := range rawSku {
					skuKeys = append(skuKeys, k)
				}
				log.Printf("[Temu] Raw SKU keys: %v", skuKeys)

				// specList on the detail SKU may have both specId and parentSpecId
				if specListRaw, ok := rawSku["specList"].([]interface{}); ok {
					log.Printf("[Temu] Found specList on SKU with %d entries", len(specListRaw))
					for _, sl := range specListRaw {
						if slMap, ok := sl.(map[string]interface{}); ok {
							specEntry := map[string]interface{}{}
							if sid, ok := slMap["specId"].(float64); ok {
								specEntry["specId"] = int(sid)
							}
							if psid, ok := slMap["parentSpecId"].(float64); ok {
								specEntry["parentSpecId"] = int(psid)
							}
							if val, ok := slMap["specName"].(string); ok && val != "" {
								specEntry["value"] = val
							}
							if _, hasSpec := specEntry["specId"]; hasSpec {
								filteredProps = append(filteredProps, specEntry)
								log.Printf("[Temu] Added spec from SKU specList: %v", specEntry)
							}
						}
					}
				} else {
					log.Printf("[Temu] No specList found on SKU")
				}
			}
		} else {
			log.Printf("[Temu] No skuList in Raw detail")
		}

		// If specList didn't have parentSpecId, try getting it from the template
		hasCompleteSpec := false
		for _, fp := range filteredProps {
			if _, hasPS := fp["parentSpecId"]; hasPS {
				if _, hasSI := fp["specId"]; hasSI {
					hasCompleteSpec = true
					break
				}
			}
		}
		log.Printf("[Temu] hasCompleteSpec=%v, specIdList=%v", hasCompleteSpec, specIdList)

		if !hasCompleteSpec && len(specIdList) > 0 {
			// Fetch template to get parentSpecId mapping
			log.Printf("[Temu] Fetching template for catId=%d to get parentSpecId", req.CatID)
			template, tmplErr := client.GetTemplate(req.CatID)
			if tmplErr != nil {
				log.Printf("[Temu] Template fetch error: %v", tmplErr)
			} else if template != nil {
				// Navigate to userInputParentSpecList — try multiple paths
				var parentSpecs []interface{}
				paths := []string{}
				if tmplResult, ok := template["result"].(map[string]interface{}); ok {
					if tmplInfo, ok := tmplResult["templateInfo"].(map[string]interface{}); ok {
						if ps, ok := tmplInfo["userInputParentSpecList"].([]interface{}); ok {
							parentSpecs = ps
							paths = append(paths, "result.templateInfo.userInputParentSpecList")
						}
					}
				}
				if parentSpecs == nil {
					if tmplInfo, ok := template["templateInfo"].(map[string]interface{}); ok {
						if ps, ok := tmplInfo["userInputParentSpecList"].([]interface{}); ok {
							parentSpecs = ps
							paths = append(paths, "templateInfo.userInputParentSpecList")
						}
					}
				}
				// Try direct path
				if parentSpecs == nil {
					if ps, ok := template["userInputParentSpecList"].([]interface{}); ok {
						parentSpecs = ps
						paths = append(paths, "userInputParentSpecList")
					}
				}

				log.Printf("[Temu] Template paths tried: %v, found %d parentSpecs", paths, len(parentSpecs))

				if len(parentSpecs) > 0 {
					// Use the first parent spec (for single-SKU Style products)
					if psMap, ok := parentSpecs[0].(map[string]interface{}); ok {
						parentSpecId := 0
						if psid, ok := psMap["parentSpecId"].(float64); ok {
							parentSpecId = int(psid)
						}
						parentSpecName := ""
						if name, ok := psMap["parentSpecName"].(string); ok {
							parentSpecName = name
						}
						log.Printf("[Temu] First parentSpec: parentSpecId=%d name=%s", parentSpecId, parentSpecName)

						if parentSpecId > 0 {
							for _, sid := range specIdList {
								specEntry := map[string]interface{}{
									"parentSpecId": parentSpecId,
									"specId":       sid,
									"value":        "Standard",
								}
								filteredProps = append(filteredProps, specEntry)
								log.Printf("[Temu] Added spec from template: parentSpecId=%d specId=%d", parentSpecId, sid)
							}
						}
					}
				} else {
					// Log template top-level keys for debugging
					tmplKeys := []string{}
					for k := range template {
						tmplKeys = append(tmplKeys, k)
					}
					log.Printf("[Temu] Template top-level keys: %v", tmplKeys)
				}
			}
		}

		// Final fallback: if we STILL don't have a spec entry, add one with just specId
		// This shouldn't happen but prevents silent failure
		hasAnySpec := false
		for _, fp := range filteredProps {
			if _, hasSI := fp["specId"]; hasSI {
				hasAnySpec = true
				break
			}
		}
		if !hasAnySpec && len(specIdList) > 0 {
			log.Printf("[Temu] WARNING: Could not resolve parentSpecId. Adding specId-only entry as last resort.")
			for _, sid := range specIdList {
				filteredProps = append(filteredProps, map[string]interface{}{
					"specId": sid,
					"value":  "Standard",
				})
			}
		}

		log.Printf("[Temu] Final filteredProps count: %d", len(filteredProps))
		for i, fp := range filteredProps {
			log.Printf("[Temu] filteredProps[%d]: %v", i, fp)
		}
	}

	temuRequest := map[string]interface{}{
		"goodsBasic": map[string]interface{}{
			"catId":      req.CatID,
			"goodsName":  sanitizeTitle(req.Title),
		},
		"bulletPoints": bulletPoints,
		"goodsDesc":    req.Description,
		"goodsServicePromise": map[string]interface{}{
			"costTemplateId":   req.ShippingTemplate,
			"fulfillmentType":  fulfillmentType,
			"shipmentLimitDay": prepDays,
		},
	}

	// Only include skuList for new products with full data
	if !isUpdate {
		temuRequest["goodsBasic"].(map[string]interface{})["outGoodsSn"] = skuSafe
		temuRequest["goodsProperty"] = map[string]interface{}{
			"goodsProperties": filteredProps,
		}

		// VAR-01: build one skuObj per active variant when ≥2 are present
		activeVariants := make([]ChannelVariantDraft, 0)
		for _, v := range req.Variants {
			if v.Active {
				activeVariants = append(activeVariants, v)
			}
		}

		if len(activeVariants) >= 2 {
			// Build a skuObj per variant — each gets its own outSkuSn, price and image
			skuList := make([]interface{}, 0, len(activeVariants))
			for _, v := range activeVariants {
				varSku := v.SKU
				if len(varSku) > 40 {
					varSku = varSku[:40]
				}
				varSkuPrice := map[string]interface{}{
					"currency":  req.Price.Currency,
					"basePrice": map[string]interface{}{"amount": v.Price, "currency": req.Price.Currency},
				}
				if req.Price.ListAmount != "" {
					varSkuPrice["listPrice"] = map[string]interface{}{"amount": v.Price, "currency": req.Price.Currency}
					varSkuPrice["listPriceType"] = 1
				}
				varSkuObj := map[string]interface{}{
					"outSkuSn": varSku,
					"price":    varSkuPrice,
					"quantity": req.Quantity,
					"images":   temuImages, // all variants share main images; per-variant image is optional
					"volumeUnit": "cm",
					"weightUnit": "g",
					"length":     "1",
					"width":      "1",
					"height":     "1",
					"weight":     "1",
				}
				if req.Dimensions != nil {
					if req.Dimensions.LengthCM != "" { varSkuObj["length"] = req.Dimensions.LengthCM }
					if req.Dimensions.WidthCM != "" { varSkuObj["width"] = req.Dimensions.WidthCM }
					if req.Dimensions.HeightCM != "" { varSkuObj["height"] = req.Dimensions.HeightCM }
				}
				if req.Weight != nil && req.Weight.WeightG != "" {
					varSkuObj["weight"] = req.Weight.WeightG
				}
				// Add sale attribute from combination (e.g. Color, Size)
				if len(v.Combination) > 0 {
					saleAttrs := []map[string]interface{}{}
					for k, val := range v.Combination {
						saleAttrs = append(saleAttrs, map[string]interface{}{
							"specName":  k,
							"specValue": val,
						})
					}
					varSkuObj["saleAttributes"] = saleAttrs
				}
				skuList = append(skuList, varSkuObj)
			}
			temuRequest["skuList"] = skuList
			log.Printf("[Temu Submit] Multi-variant: %d SKUs in skuList", len(skuList))
		} else {
			temuRequest["skuList"] = []interface{}{skuObj}
		}
	} else {
		// For partial.update with existing SKU
		// MUST include goodsProperty with spec entries (parentSpecId, specId, value)
		// MUST include skuList with skuId, dimensions, images
		// Must NOT include basePrice, quantity, specIdList in SKU
		temuRequest["goodsProperty"] = map[string]interface{}{
			"goodsProperties": filteredProps,
		}

		if existingSkuId > 0 {
			updateSku := map[string]interface{}{
				"skuId":      existingSkuId,
				"images":     temuImages,
				"volumeUnit": "cm",
				"weightUnit": "g",
				"length":     "1",
				"width":      "1",
				"height":     "1",
				"weight":     "1",
			}
			if req.Dimensions != nil {
				if req.Dimensions.LengthCM != "" { updateSku["length"] = req.Dimensions.LengthCM }
				if req.Dimensions.WidthCM != "" { updateSku["width"] = req.Dimensions.WidthCM }
				if req.Dimensions.HeightCM != "" { updateSku["height"] = req.Dimensions.HeightCM }
			}
			if req.Weight != nil && req.Weight.WeightG != "" {
				updateSku["weight"] = req.Weight.WeightG
			}
			if req.Price.ListAmount != "" {
				updateSku["listPrice"] = map[string]interface{}{
					"amount":   req.Price.ListAmount,
					"currency": req.Price.Currency,
				}
			}
			temuRequest["skuList"] = []interface{}{updateSku}
		}
	}

	if req.Brand != nil && len(req.Brand) > 0 {
		temuRequest["goodsTrademark"] = req.Brand
	}
	// Origin info — build clean map, fallback to existing detail for updates
	// NOTE: Temu update API expects "originRegion1"/"originRegion2" (not "originRegionName1")
	var originToSend map[string]interface{}
	if r1, ok := req.OriginInfo["originRegionName1"].(string); ok && r1 != "" {
		originToSend = map[string]interface{}{"originRegion1": r1}
		if r2, ok := req.OriginInfo["originRegionName2"].(string); ok && r2 != "" {
			originToSend["originRegion2"] = r2
		}
	} else if existingDetail != nil && existingDetail.Raw != nil {
		// Pull from existing product for updates
		if oi, ok := existingDetail.Raw["goodsOriginInfo"].(map[string]interface{}); ok {
			// Try both field name variants
			r1 := ""
			if v, ok := oi["originRegionName1"].(string); ok && v != "" {
				r1 = v
			} else if v, ok := oi["originRegion1"].(string); ok && v != "" {
				r1 = v
			}
			if r1 != "" {
				originToSend = map[string]interface{}{"originRegion1": r1}
				r2 := ""
				if v, ok := oi["originRegionName2"].(string); ok && v != "" {
					r2 = v
				} else if v, ok := oi["originRegion2"].(string); ok && v != "" {
					r2 = v
				}
				if r2 != "" {
					originToSend["originRegion2"] = r2
				}
				log.Printf("[Temu] Using existing origin: %v", originToSend)
			}
		}
	}
	if originToSend != nil {
		temuRequest["goodsOriginInfo"] = originToSend
	}

	// Determine add vs update from goodsId passed by frontend
	existingGoodsID := req.GoodsID
	isUpdate = existingGoodsID > 0
	if isUpdate {
		// goodsId must be a TOP-LEVEL parameter for goods.update, not inside goodsBasic
		temuRequest["goodsId"] = existingGoodsID
		log.Printf("[Temu] Updating existing product goodsId=%d SKU=%s", existingGoodsID, skuSafe)
	} else {
		log.Printf("[Temu] Creating new product SKU=%s", skuSafe)
	}

	// Build the full request payload (for debug output)
	apiType := "bg.local.goods.add"
	if isUpdate {
		apiType = "bg.local.goods.partial.update"
	}
	debugRequest := map[string]interface{}{
		"type": apiType,
	}
	for k, v := range temuRequest {
		debugRequest[k] = v
	}

	// Debug mode: return the payload without submitting
	if c.Query("debug") == "1" || c.Query("debug") == "true" {
		c.JSON(http.StatusOK, gin.H{
			"ok":              true,
			"debug":           true,
			"isUpdate":        isUpdate,
			"existingGoodsId": existingGoodsID,
			"request":         debugRequest,
		})
		return
	}

	// Submit to Temu — add or update
	result, rawResponse, err := client.SubmitProduct(temuRequest, isUpdate)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"ok":       false,
			"error":    err.Error(),
			"isUpdate": isUpdate,
			"request":  debugRequest,
			"response": rawResponse,
		})
		return
	}

	// If update doesn't return goodsId, use the existing one
	if result.GoodsID == 0 && existingGoodsID > 0 {
		result.GoodsID = existingGoodsID
	}

	// Save listing to Firestore
	now := time.Now()
	if isUpdate && req.CredentialID != "" {
		// Update existing listing
		existingListing, _ := h.repo.FindListingByProductAndAccount(c.Request.Context(), tenantID, req.ProductID, req.CredentialID)
		if existingListing != nil {
			existingListing.State = "published"
			existingListing.ChannelIdentifiers = &models.ChannelIdentifiers{
				ListingID: fmt.Sprintf("%d", result.GoodsID),
				SKU:       req.SKU,
			}
			existingListing.Overrides = &models.ListingOverrides{
				Title:           req.Title,
				CategoryMapping: fmt.Sprintf("%d", req.CatID),
				Images:          temuImages,
				Price:           func() *float64 { p, _ := strconv.ParseFloat(req.Price.BaseAmount, 64); return &p }(),
				Quantity:        &req.Quantity,
			}
			existingListing.UpdatedAt = now
			if err := h.repo.UpdateListing(c.Request.Context(), existingListing); err != nil {
				log.Printf("[Temu] WARNING: product updated on Temu (goodsId=%d) but listing update failed: %v", result.GoodsID, err)
			}
		}
	} else {
		// Create new listing
		listing := &models.Listing{
			ListingID:        fmt.Sprintf("temu-%s-%d", req.SKU, now.Unix()),
			TenantID:         tenantID,
			ProductID:        req.ProductID,
			Channel:          "temu",
			ChannelAccountID: req.CredentialID,
			State:            "published",
			ChannelIdentifiers: &models.ChannelIdentifiers{
				ListingID: fmt.Sprintf("%d", result.GoodsID),
				SKU:       req.SKU,
			},
			Overrides: &models.ListingOverrides{
				Title:           req.Title,
				CategoryMapping: fmt.Sprintf("%d", req.CatID),
				Images:          temuImages,
				Price:           func() *float64 { p, _ := strconv.ParseFloat(req.Price.BaseAmount, 64); return &p }(),
				Quantity:        &req.Quantity,
			},
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := h.repo.CreateListing(c.Request.Context(), listing); err != nil {
			log.Printf("[Temu] WARNING: product submitted to Temu (goodsId=%d) but listing create failed: %v", result.GoodsID, err)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":       true,
		"isUpdate": isUpdate,
		"goodsId":  result.GoodsID,
		"skuInfo":  result.SkuInfoList,
		"request":  debugRequest,
		"response": rawResponse,
	})
}

// ============================================================================
// HELPER: Resolve specIdList from template's userInputParentSpecList
// ============================================================================

func resolveSpecIdsFromTemplate(client *temu.Client, catID int, template map[string]interface{}) []int {
	// Log the top-level keys for debugging
	topKeys := []string{}
	for k := range template {
		topKeys = append(topKeys, k)
	}
	log.Printf("[Temu Spec] Template top-level keys: %v", topKeys)

	// Try multiple paths to find userInputParentSpecList
	var parentSpecList []interface{}

	// Path 1: template.result.templateInfo.userInputParentSpecList
	if result, ok := template["result"].(map[string]interface{}); ok {
		if tplInfo, ok := result["templateInfo"].(map[string]interface{}); ok {
			if list, ok := tplInfo["userInputParentSpecList"].([]interface{}); ok {
				parentSpecList = list
			}
		}
		// Path 2: template.result.userInputParentSpecList
		if len(parentSpecList) == 0 {
			if list, ok := result["userInputParentSpecList"].([]interface{}); ok {
				parentSpecList = list
			}
		}
	}
	// Path 3: template.userInputParentSpecList (direct)
	if len(parentSpecList) == 0 {
		if list, ok := template["userInputParentSpecList"].([]interface{}); ok {
			parentSpecList = list
		}
	}
	// Path 4: template.templateInfo.userInputParentSpecList
	if len(parentSpecList) == 0 {
		if tplInfo, ok := template["templateInfo"].(map[string]interface{}); ok {
			if list, ok := tplInfo["userInputParentSpecList"].([]interface{}); ok {
				parentSpecList = list
			}
		}
	}

	if len(parentSpecList) == 0 {
		log.Printf("[Temu Spec] No userInputParentSpecList found in template for catId=%d", catID)
		// Dump a summary of the template structure to help debug
		templateBytes, _ := json.Marshal(template)
		if len(templateBytes) > 1000 {
			templateBytes = templateBytes[:1000]
		}
		log.Printf("[Temu Spec] Template snippet: %s", string(templateBytes))
		return nil
	}

	log.Printf("[Temu Spec] Found %d parent specs to resolve", len(parentSpecList))

	var allSpecIds []int

	for _, ps := range parentSpecList {
		psMap, ok := ps.(map[string]interface{})
		if !ok {
			continue
		}
		parentSpecId := 0
		if pid, ok := psMap["parentSpecId"].(float64); ok {
			parentSpecId = int(pid)
		}
		parentSpecName := ""
		if name, ok := psMap["parentSpecName"].(string); ok {
			parentSpecName = strings.ToLower(name)
		}

		log.Printf("[Temu Spec] Probing parentSpecId=%d name=%q", parentSpecId, parentSpecName)

		// Build candidate labels based on the parent type
		labels := []string{}
		switch {
		case strings.Contains(parentSpecName, "color") || strings.Contains(parentSpecName, "colour"):
			labels = []string{"Multicolor", "Multi Color", "Mixed Color", "Mixed", "Default"}
		case strings.Contains(parentSpecName, "size"):
			labels = []string{"One Size", "Free Size", "Default"}
		default:
			labels = []string{"Default", "One Size", "Standard", "Multicolor"}
		}

		resolved := false
		for _, label := range labels {
			ids, err := client.GetSpecIDs(catID, parentSpecId, label)
			if err == nil && len(ids) > 0 {
				allSpecIds = append(allSpecIds, ids...)
				log.Printf("[Temu Spec] Resolved parentSpecId=%d label=%q → specIds=%v", parentSpecId, label, ids)
				resolved = true
				break
			}
		}
		if !resolved {
			log.Printf("[Temu Spec] WARNING: Could not resolve parentSpecId=%d (%s)", parentSpecId, parentSpecName)
		}
	}

	return allSpecIds
}

// ============================================================================
// HELPER: Build draft from enriched product data
// ============================================================================

func buildTemuDraft(
	product map[string]interface{},
	enrichedData map[string]interface{},
	category *temu.TemuCategory,
	template map[string]interface{},
) *TemuListingDraft {
	draft := &TemuListingDraft{
		Quantity: 1,
	}

	// Extract basic fields from product
	draft.Title = extractString(product, "title")
	draft.Description = extractString(product, "description")
	draft.SKU = extractString(product, "sku")
	if draft.SKU == "" {
		draft.SKU = extractString(product, "asin")
	}

	// Category
	if category != nil {
		draft.CatID = category.CatID
		draft.CatName = category.CatName
	}

	// Images — from product assets array (PIM stores as assets with url field)
	if assets, ok := product["assets"].([]interface{}); ok {
		for _, asset := range assets {
			if assetMap, ok := asset.(map[string]interface{}); ok {
				if url, ok := assetMap["url"].(string); ok && url != "" {
					draft.Images = append(draft.Images, url)
				}
			}
		}
	}
	// Fallback: try product["images"] (some import flows use this)
	if len(draft.Images) == 0 {
		if imgs, ok := product["images"].([]interface{}); ok {
			for _, img := range imgs {
				if url, ok := img.(string); ok && url != "" {
					draft.Images = append(draft.Images, url)
				} else if imgMap, ok := img.(map[string]interface{}); ok {
					if url, ok := imgMap["url"].(string); ok && url != "" {
						draft.Images = append(draft.Images, url)
					}
				}
			}
		}
	}

	// Bullet points from PIM key_features
	if features, ok := product["key_features"].([]interface{}); ok {
		for _, f := range features {
			if s, ok := f.(string); ok && s != "" {
				draft.BulletPoints = append(draft.BulletPoints, s)
			}
		}
	}
	// Also check attributes.bullet_points (set by import-enrich)
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

	// Dimensions from PIM product
	if dims, ok := product["dimensions"].(map[string]interface{}); ok {
		draft.Dimensions = extractDimensions(dims)
	}
	if wt, ok := product["weight"].(map[string]interface{}); ok {
		draft.Weight = extractWeight(wt)
	}

	// Price — from product or enriched data
	if price, ok := product["price"].(float64); ok && price > 0 {
		draft.Price = &TemuPrice{
			BaseAmount: fmt.Sprintf("%.2f", price),
			Currency:   "GBP",
		}
	}

	// Bullet points from enriched data (supplement PIM if empty)
	if enrichedData != nil {
		if len(draft.BulletPoints) == 0 {
			if bullets, ok := enrichedData["bullet_points"].([]interface{}); ok {
				for _, b := range bullets {
					if s, ok := b.(string); ok {
						draft.BulletPoints = append(draft.BulletPoints, s)
					}
				}
			}
		}

		// Description override from enriched data
		if desc, ok := enrichedData["description"].(string); ok && desc != "" {
			draft.Description = desc
		}

		// Dimensions from enriched data (supplement PIM if empty)
		if draft.Dimensions == nil {
			if dims, ok := enrichedData["dimensions"].(map[string]interface{}); ok {
				draft.Dimensions = extractDimensions(dims)
			}
		}
		if draft.Weight == nil {
			if wt, ok := enrichedData["weight"].(map[string]interface{}); ok {
				draft.Weight = extractWeight(wt)
			}
		}

		// Images from enriched data (often higher quality)
		if imgs, ok := enrichedData["images"].([]interface{}); ok && len(imgs) > 0 {
			var enrichedImages []string
			for _, img := range imgs {
				if url, ok := img.(string); ok && url != "" {
					enrichedImages = append(enrichedImages, url)
				} else if imgMap, ok := img.(map[string]interface{}); ok {
					for _, key := range []string{"link", "url", "large", "original"} {
						if url, ok := imgMap[key].(string); ok && url != "" {
							enrichedImages = append(enrichedImages, url)
							break
						}
					}
				}
			}
			if len(enrichedImages) > 0 {
				draft.Images = enrichedImages
			}
		}
	}

	// Map enriched attributes to goods properties using the template
	if template != nil && enrichedData != nil {
		draft.GoodsProperties = mapAttributesToProperties(enrichedData, template)
	}

	return draft
}

// ============================================================================
// HELPER: Map enriched attributes → Temu goods properties
// ============================================================================

func mapAttributesToProperties(
	enrichedData map[string]interface{},
	template map[string]interface{},
) []map[string]interface{} {
	var props []map[string]interface{}

	// Get template property index
	templateInfo := extractNested(template, "templateInfo", "template", "info")
	if templateInfo == nil {
		return props
	}

	goodsProps, _ := templateInfo["goodsProperties"].([]interface{})
	if goodsProps == nil {
		return props
	}

	// Build enriched attribute lookup (lowercase keys)
	attrs := make(map[string]string)
	if attrMap, ok := enrichedData["attributes"].(map[string]interface{}); ok {
		for k, v := range attrMap {
			key := strings.ToLower(strings.TrimSpace(k))
			if s, ok := v.(string); ok {
				attrs[key] = s
			} else if arr, ok := v.([]interface{}); ok && len(arr) > 0 {
				if s, ok := arr[0].(string); ok {
					attrs[key] = s
				}
			}
		}
	}

	// Try to match each template property to an enriched attribute
	for _, propRaw := range goodsProps {
		prop, ok := propRaw.(map[string]interface{})
		if !ok {
			continue
		}

		propName := ""
		for _, key := range []string{"name", "propertyName"} {
			if n, ok := prop[key].(string); ok {
				propName = n
				break
			}
		}
		if propName == "" {
			continue
		}

		// Look for a matching enriched attribute
		propNameLower := strings.ToLower(strings.TrimSpace(propName))
		matchedValue, found := attrs[propNameLower]

		// Common name mappings
		if !found {
			aliases := map[string][]string{
				"material":    {"material_type", "material_composition", "fabric_type"},
				"color":       {"colour", "color_name"},
				"brand":       {"brand_name"},
				"item weight": {"weight", "product_weight"},
			}
			if alts, ok := aliases[propNameLower]; ok {
				for _, alt := range alts {
					if v, ok := attrs[alt]; ok {
						matchedValue = v
						found = true
						break
					}
				}
			}
		}

		if !found || matchedValue == "" {
			continue
		}

		// Build property row with the correct IDs from template
		row := map[string]interface{}{}

		if pid, ok := prop["pid"]; ok {
			row["pid"] = pid
		}
		if tpid, ok := prop["templatePid"]; ok {
			row["templatePid"] = tpid
		}
		if rpid, ok := prop["refPid"]; ok {
			row["refPid"] = rpid
		}

		// Try to match value to a VID from template options
		values, _ := prop["values"].([]interface{})
		matched := false
		for _, valRaw := range values {
			valMap, ok := valRaw.(map[string]interface{})
			if !ok {
				continue
			}

			label := ""
			for _, key := range []string{"value", "label", "name"} {
				if l, ok := valMap[key].(string); ok {
					label = l
					break
				}
			}

			if strings.EqualFold(label, matchedValue) {
				// Exact match — use VID
				for _, vidKey := range []string{"vid", "valueVid", "valueId", "id"} {
					if vid, ok := valMap[vidKey]; ok {
						row["vid"] = vid
						row["value"] = label
						matched = true
						break
					}
				}
				break
			}
		}

		if !matched {
			// Use as free text if template allows it
			row["value"] = matchedValue
		}

		if len(row) > 1 { // Has at least pid + value
			props = append(props, row)
		}
	}

	return props
}

// ============================================================================
// HELPERS
// ============================================================================

func extractString(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

func extractNested(m map[string]interface{}, keys ...string) map[string]interface{} {
	for _, k := range keys {
		if nested, ok := m[k].(map[string]interface{}); ok {
			return nested
		}
	}
	return nil
}

func extractDimensions(dims map[string]interface{}) *TemuDimensions {
	l := extractNumericStr(dims, "length", "item_length")
	w := extractNumericStr(dims, "width", "item_width")
	h := extractNumericStr(dims, "height", "item_height")
	unit := extractString(dims, "unit", "length_unit")

	if l == "" || w == "" || h == "" {
		return nil
	}

	// Convert to cm
	lCm := convertToCm(l, unit)
	wCm := convertToCm(w, unit)
	hCm := convertToCm(h, unit)

	return &TemuDimensions{
		LengthCM: lCm,
		WidthCM:  wCm,
		HeightCM: hCm,
	}
}

func extractWeight(wt map[string]interface{}) *TemuWeight {
	w := extractNumericStr(wt, "value", "weight", "item_weight")
	unit := extractString(wt, "unit", "weight_unit")

	if w == "" {
		return nil
	}

	grams := convertToGrams(w, unit)
	return &TemuWeight{WeightG: grams}
}

func extractNumericStr(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		v := m[k]
		if v == nil {
			continue
		}
		switch val := v.(type) {
		case string:
			if val != "" {
				return val
			}
		case float64:
			return fmt.Sprintf("%.2f", val)
		case int:
			return fmt.Sprintf("%d", val)
		case json.Number:
			return val.String()
		}
	}
	return ""
}

func convertToCm(value string, unit string) string {
	f, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return value
	}

	unit = strings.ToLower(strings.TrimSpace(unit))
	switch unit {
	case "mm", "millimeter", "millimeters":
		f *= 0.1
	case "m", "meter", "meters":
		f *= 100
	case "in", "inch", "inches":
		f *= 2.54
	case "ft", "foot", "feet":
		f *= 30.48
	}

	cm := int(math.Round(f))
	if cm < 1 {
		cm = 1
	}
	return fmt.Sprintf("%d", cm)
}

func convertToGrams(value string, unit string) string {
	f, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return value
	}

	unit = strings.ToLower(strings.TrimSpace(unit))
	switch unit {
	case "kg", "kilogram", "kilograms":
		f *= 1000
	case "lb", "lbs", "pound", "pounds":
		f *= 453.59237
	case "oz", "ounce", "ounces":
		f *= 28.349523125
	}

	g := int(math.Round(f))
	if g < 1 {
		g = 1
	}
	return fmt.Sprintf("%d", g)
}

func sanitizeTitle(s string) string {
	t := strings.TrimSpace(s)
	t = strings.ReplaceAll(t, "–", "-")
	t = strings.ReplaceAll(t, "—", "-")
	t = strings.ReplaceAll(t, "·", " ")
	t = strings.ReplaceAll(t, "×", "x")
	t = strings.ReplaceAll(t, "&", "and")
	// Collapse whitespace
	fields := strings.Fields(t)
	t = strings.Join(fields, " ")
	if len(t) > 500 {
		t = t[:500]
	}
	return t
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ============================================================================
// BRAND MAPPING — Firestore path: tenants/{tenantId}/temu_brand_mappings/mappings
// ============================================================================
// Stored per-tenant (not per-credential) so a second Temu account reuses
// the same mapping without the user having to redo it.

func (h *TemuHandler) brandMappingsDoc(tenantID string) *firestore.DocumentRef {
	return h.fsClient.Collection("tenants").Doc(tenantID).Collection("temu_brand_mappings").Doc("mappings")
}

// BrandMappingEntry is one row in the brand map.
type BrandMappingEntry struct {
	ProductBrand   string `json:"productBrand" firestore:"productBrand"`
	TemuBrandID    int64  `json:"temuBrandId" firestore:"temuBrandId"`
	TemuBrandName  string `json:"temuBrandName" firestore:"temuBrandName"`
	TrademarkID    int64  `json:"trademarkId" firestore:"trademarkId"`
	TrademarkBizID int64  `json:"trademarkBizId" firestore:"trademarkBizId"`
}

// ============================================================================
// GET /api/v1/temu/brand-mappings
// ============================================================================
// Returns:
//   - productBrands: distinct brand values from tenants/{id}/products
//   - temuBrands:    authorized brands from the Temu API
//   - mappings:      existing mapping rows from Firestore
func (h *TemuHandler) GetBrandMappings(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	// ── 1. Existing mappings from Firestore ───────────────────────────────
	var mappings []BrandMappingEntry
	if h.fsClient != nil {
		doc, err := h.brandMappingsDoc(tenantID).Get(c.Request.Context())
		if err == nil && doc.Exists() {
			if raw, ok := doc.Data()["mappings"]; ok {
				if arr, ok := raw.([]interface{}); ok {
					for _, item := range arr {
						if m, ok := item.(map[string]interface{}); ok {
							entry := BrandMappingEntry{
								ProductBrand:  getStrValue(m, "productBrand"),
								TemuBrandName: getStrValue(m, "temuBrandName"),
							}
							if v, ok := m["temuBrandId"].(int64); ok {
								entry.TemuBrandID = v
							} else if v, ok := m["temuBrandId"].(float64); ok {
								entry.TemuBrandID = int64(v)
							}
							if v, ok := m["trademarkId"].(int64); ok {
								entry.TrademarkID = v
							} else if v, ok := m["trademarkId"].(float64); ok {
								entry.TrademarkID = int64(v)
							}
							if v, ok := m["trademarkBizId"].(int64); ok {
								entry.TrademarkBizID = v
							} else if v, ok := m["trademarkBizId"].(float64); ok {
								entry.TrademarkBizID = int64(v)
							}
							mappings = append(mappings, entry)
						}
					}
				}
			}
		}
	}
	if mappings == nil {
		mappings = []BrandMappingEntry{}
	}

	// ── 2. Distinct product brands from Firestore ─────────────────────────
	productBrands := h.getDistinctProductBrands(c, tenantID)

	// ── 3. Temu authorised brands from the API ────────────────────────────
	var temuBrands []temu.BrandTrademarkFull
	client, err := h.getTemuClient(c)
	if err == nil {
		if brands, err := client.ListAllBrands(); err == nil {
			temuBrands = brands
		} else {
			log.Printf("[Temu GetBrandMappings] ListAllBrands error: %v", err)
		}
	} else {
		log.Printf("[Temu GetBrandMappings] getTemuClient error: %v", err)
	}
	if temuBrands == nil {
		temuBrands = []temu.BrandTrademarkFull{}
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":            true,
		"productBrands": productBrands,
		"temuBrands":    temuBrands,
		"mappings":      mappings,
	})
}

// ============================================================================
// PUT /api/v1/temu/brand-mappings
// ============================================================================
// Body: { mappings: [{ productBrand, temuBrandId, temuBrandName, trademarkId, trademarkBizId }] }
func (h *TemuHandler) SaveBrandMappings(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var body struct {
		Mappings []BrandMappingEntry `json:"mappings"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}
	if h.fsClient == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "Firestore not available"})
		return
	}

	// Convert to []interface{} for Firestore
	rows := make([]interface{}, len(body.Mappings))
	for i, m := range body.Mappings {
		rows[i] = map[string]interface{}{
			"productBrand":   m.ProductBrand,
			"temuBrandId":    m.TemuBrandID,
			"temuBrandName":  m.TemuBrandName,
			"trademarkId":    m.TrademarkID,
			"trademarkBizId": m.TrademarkBizID,
		}
	}

	_, err := h.brandMappingsDoc(tenantID).Set(c.Request.Context(), map[string]interface{}{
		"mappings":  rows,
		"updatedAt": time.Now(),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "saved": len(body.Mappings)})
}

// ============================================================================
// GET /api/v1/temu/brand-mappings/export
// ============================================================================
// Streams an xlsx file. Columns: A=Product Brand (locked), B=Temu Brand (dropdown).
// A hidden sheet "TemuBrands" holds brand|brandId|trademarkId|trademarkBizId
// so the importer can resolve the full brand object from the display name.
func (h *TemuHandler) ExportBrandMappings(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	// ── Load data ────────────────────────────────────────────────────────────
	var mappings []BrandMappingEntry
	if h.fsClient != nil {
		if doc, err := h.brandMappingsDoc(tenantID).Get(c.Request.Context()); err == nil && doc.Exists() {
			if raw, ok := doc.Data()["mappings"]; ok {
				if arr, ok := raw.([]interface{}); ok {
					for _, item := range arr {
						if m, ok := item.(map[string]interface{}); ok {
							entry := BrandMappingEntry{ProductBrand: getStrValue(m, "productBrand"), TemuBrandName: getStrValue(m, "temuBrandName")}
							if v, ok := m["temuBrandId"].(float64); ok { entry.TemuBrandID = int64(v) }
							mappings = append(mappings, entry)
						}
					}
				}
			}
		}
	}

	productBrands := h.getDistinctProductBrands(c, tenantID)

	var temuBrands []temu.BrandTrademarkFull
	if client, err := h.getTemuClient(c); err == nil {
		temuBrands, _ = client.ListAllBrands()
	}

	mappingLookup := map[string]BrandMappingEntry{}
	for _, m := range mappings {
		mappingLookup[strings.ToLower(m.ProductBrand)] = m
	}

	// ── Build xlsx with excelize ─────────────────────────────────────────────
	f := excelize.NewFile()
	defer f.Close()

	// Sheet 1: Brand Mapping (main sheet)
	mainSheet := "Brand Mapping"
	f.SetSheetName("Sheet1", mainSheet)

	// Header row styling
	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "FFFFFF", Family: "Arial", Size: 10},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"1A3A5C"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "left", Vertical: "center"},
	})
	lockedStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Family: "Arial", Size: 10, Color: "1A3A5C"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"F4F7FA"}, Pattern: 1},
		Alignment: &excelize.Alignment{Vertical: "center"},
	})
	matchedStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Family: "Arial", Size: 10, Color: "155724"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"D4EDDA"}, Pattern: 1},
		Alignment: &excelize.Alignment{Vertical: "center"},
	})
	autoStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Family: "Arial", Size: 10, Color: "856404"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"FFF3CD"}, Pattern: 1},
		Alignment: &excelize.Alignment{Vertical: "center"},
	})

	// Column widths
	f.SetColWidth(mainSheet, "A", "A", 38)
	f.SetColWidth(mainSheet, "B", "B", 38)
	f.SetColWidth(mainSheet, "C", "C", 14)

	// Headers
	f.SetCellValue(mainSheet, "A1", "Product Brand")
	f.SetCellValue(mainSheet, "B1", "Temu Brand")
	f.SetCellValue(mainSheet, "C1", "Status")
	f.SetCellStyle(mainSheet, "A1", "C1", headerStyle)
	f.SetRowHeight(mainSheet, 1, 22)

	// Sheet 2: TemuBrands (hidden reference)
	refSheet := "TemuBrands"
	f.NewSheet(refSheet)
	f.SetCellValue(refSheet, "A1", "brandName")
	f.SetCellValue(refSheet, "B1", "brandId")
	f.SetCellValue(refSheet, "C1", "trademarkId")
	f.SetCellValue(refSheet, "D1", "trademarkBizId")
	for i, tb := range temuBrands {
		row := i + 2
		f.SetCellValue(refSheet, fmt.Sprintf("A%d", row), tb.BrandName)
		f.SetCellValue(refSheet, fmt.Sprintf("B%d", row), tb.BrandID)
		f.SetCellValue(refSheet, fmt.Sprintf("C%d", row), tb.TrademarkID)
		f.SetCellValue(refSheet, fmt.Sprintf("D%d", row), tb.TrademarkBizID)
	}
	// Hide the reference sheet
	f.SetSheetVisible(refSheet, false)

	// Data rows + dropdown validation
	lastRefRow := len(temuBrands) + 1
	if lastRefRow < 2 { lastRefRow = 2 }
	dvFormula := fmt.Sprintf("TemuBrands!$A$2:$A$%d", lastRefRow)

	for i, pb := range productBrands {
		row := i + 2
		cellA := fmt.Sprintf("A%d", row)
		cellB := fmt.Sprintf("B%d", row)
		cellC := fmt.Sprintf("C%d", row)

		f.SetCellValue(mainSheet, cellA, pb)
		f.SetCellStyle(mainSheet, cellA, cellA, lockedStyle)

		// Add dropdown validation on column B
		dv := excelize.NewDataValidation(true)
		dv.Sqref = cellB
		dv.SetDropList([]string{dvFormula})
		f.AddDataValidation(mainSheet, dv)

		pbKey := strings.ToLower(pb)
		if mapped, ok := mappingLookup[pbKey]; ok && mapped.TemuBrandName != "" {
			f.SetCellValue(mainSheet, cellB, mapped.TemuBrandName)
			f.SetCellStyle(mainSheet, cellB, cellB, matchedStyle)
			f.SetCellValue(mainSheet, cellC, "✓ Saved")
		} else if match := fuzzyMatchGo(pb, temuBrands); match != nil {
			f.SetCellValue(mainSheet, cellB, match.BrandName)
			f.SetCellStyle(mainSheet, cellB, cellB, autoStyle)
			f.SetCellValue(mainSheet, cellC, "~ Auto")
		}
		f.SetRowHeight(mainSheet, row, 18)
	}

	// Freeze header row
	f.SetPanes(mainSheet, &excelize.Panes{
		Freeze:      true,
		YSplit:      1,
		TopLeftCell: "A2",
		ActivePane:  "bottomLeft",
	})

	if idx, err := f.GetSheetIndex(mainSheet); err == nil {
		f.SetActiveSheet(idx)
	}

	buf, err := f.WriteToBuffer()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "write xlsx: " + err.Error()})
		return
	}

	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("Content-Disposition", `attachment; filename="temu_brand_mapping.xlsx"`)
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", buf.Bytes())
}

// fuzzyMatchGo does a normalised contains match between a product brand and Temu brands.
func fuzzyMatchGo(productBrand string, temuBrands []temu.BrandTrademarkFull) *temu.BrandTrademarkFull {
	norm := func(s string) string {
		s = strings.ToLower(s)
		var out []byte
		for i := 0; i < len(s); i++ {
			if (s[i] >= 'a' && s[i] <= 'z') || (s[i] >= '0' && s[i] <= '9') {
				out = append(out, s[i])
			}
		}
		return string(out)
	}
	pb := norm(productBrand)
	for i := range temuBrands {
		tb := norm(temuBrands[i].BrandName)
		if tb == pb { return &temuBrands[i] }
	}
	for i := range temuBrands {
		tb := norm(temuBrands[i].BrandName)
		if tb != "" && (strings.Contains(pb, tb) || strings.Contains(tb, pb)) {
			return &temuBrands[i]
		}
	}
	return nil
}

// getDistinctProductBrands streams the products collection and returns sorted unique brand values.
func (h *TemuHandler) getDistinctProductBrands(c *gin.Context, tenantID string) []string {
	brands := []string{}
	if h.fsClient == nil { return brands }
	seen := map[string]bool{}
	iter := h.fsClient.Collection("tenants").Doc(tenantID).Collection("products").Select("brand").Documents(c.Request.Context())
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if err == iterator.Done { break }
		if err != nil { break }
		if b, ok := doc.Data()["brand"].(string); ok && b != "" && !seen[b] {
			seen[b] = true
			brands = append(brands, b)
		}
	}
	// Simple alphabetical sort
	for i := 0; i < len(brands); i++ {
		for j := i + 1; j < len(brands); j++ {
			if strings.ToLower(brands[i]) > strings.ToLower(brands[j]) {
				brands[i], brands[j] = brands[j], brands[i]
			}
		}
	}
	return brands
}

// ============================================================================
// POST /api/v1/temu/brand-mappings/import
// ============================================================================
// Accepts a multipart xlsx upload, parses with excelize, saves to Firestore.
func (h *TemuHandler) ImportBrandMappings(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "file required: " + err.Error()})
		return
	}

	// Write to temp file so excelize can open it
	tmpFile, err := os.CreateTemp("", "temu_brand_import_*.xlsx")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "temp file: " + err.Error()})
		return
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	src, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "open upload: " + err.Error()})
		return
	}
	defer src.Close()
	if _, err := io.Copy(tmpFile, src); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "write temp: " + err.Error()})
		return
	}
	tmpFile.Close()

	wb, err := excelize.OpenFile(tmpFile.Name())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "cannot open xlsx: " + err.Error()})
		return
	}
	defer wb.Close()

	// Build brand lookup from hidden TemuBrands sheet
	brandLookup := map[string]BrandMappingEntry{}
	if rows, err := wb.GetRows("TemuBrands"); err == nil {
		for _, row := range rows[1:] { // skip header
			if len(row) < 4 { continue }
			name := strings.TrimSpace(row[0])
			if name == "" { continue }
			entry := BrandMappingEntry{TemuBrandName: name}
			fmt.Sscanf(row[1], "%d", &entry.TemuBrandID)
			fmt.Sscanf(row[2], "%d", &entry.TrademarkID)
			fmt.Sscanf(row[3], "%d", &entry.TrademarkBizID)
			brandLookup[strings.ToLower(name)] = entry
		}
	}

	// Parse Brand Mapping sheet
	rows, err := wb.GetRows("Brand Mapping")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": `sheet "Brand Mapping" not found`})
		return
	}

	var resultMappings []BrandMappingEntry
	for _, row := range rows[1:] { // skip header
		if len(row) < 2 { continue }
		pb := strings.TrimSpace(row[0])
		tb := strings.TrimSpace(row[1])
		if pb == "" || tb == "" { continue }
		if ref, ok := brandLookup[strings.ToLower(tb)]; ok {
			resultMappings = append(resultMappings, BrandMappingEntry{
				ProductBrand:   pb,
				TemuBrandID:    ref.TemuBrandID,
				TemuBrandName:  ref.TemuBrandName,
				TrademarkID:    ref.TrademarkID,
				TrademarkBizID: ref.TrademarkBizID,
			})
		}
	}

	// Save to Firestore
	if h.fsClient == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "Firestore not available"})
		return
	}
	fsRows := make([]interface{}, len(resultMappings))
	for i, m := range resultMappings {
		fsRows[i] = map[string]interface{}{
			"productBrand":   m.ProductBrand,
			"temuBrandId":    m.TemuBrandID,
			"temuBrandName":  m.TemuBrandName,
			"trademarkId":    m.TrademarkID,
			"trademarkBizId": m.TrademarkBizID,
		}
	}
	if _, err := h.brandMappingsDoc(tenantID).Set(c.Request.Context(), map[string]interface{}{
		"mappings":  fsRows,
		"updatedAt": time.Now(),
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "imported": len(resultMappings)})
}
