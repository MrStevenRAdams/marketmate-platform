package handlers

// ============================================================================
// SHOPWIRED HANDLER
// ============================================================================
// Routes:
//   POST /shopwired/test              → test connection with given credentials
//   POST /shopwired/credentials       → save credentials
//   GET  /shopwired/categories        → list all categories (for picker)
//   GET  /shopwired/brands            → list all brands (for picker)
//   POST /shopwired/prepare           → build listing draft from MarketMate product
//   POST /shopwired/submit            → create/update product on ShopWired
//   PUT  /shopwired/products/:id      → update existing product
//   DELETE /shopwired/products/:id    → delete product
//   GET  /shopwired/products          → list products
// ============================================================================

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"module-a/marketplace/clients/shopwired"
	"module-a/models"
	"module-a/repository"
	"module-a/services"
)

type ShopWiredHandler struct {
	marketplaceService *services.MarketplaceService
	repo               *repository.MarketplaceRepository
	productRepo        *repository.FirestoreRepository
}

func NewShopWiredHandler(
	marketplaceService *services.MarketplaceService,
	repo *repository.MarketplaceRepository,
	productRepo *repository.FirestoreRepository,
) *ShopWiredHandler {
	return &ShopWiredHandler{
		marketplaceService: marketplaceService,
		repo:               repo,
		productRepo:        productRepo,
	}
}

// ── Credential resolution ─────────────────────────────────────────────────────

func (h *ShopWiredHandler) getClient(c *gin.Context) (*shopwired.Client, string, error) {
	tenantID := c.GetString("tenant_id")
	credentialID := c.Query("credential_id")
	if credentialID == "" {
		credentialID = c.GetHeader("X-Credential-Id")
	}

	if credentialID == "" {
		creds, err := h.repo.ListCredentials(c.Request.Context(), tenantID)
		if err != nil {
			return nil, "", fmt.Errorf("list credentials: %w", err)
		}
		for _, cred := range creds {
			if cred.Channel == "shopwired" && cred.Active {
				credentialID = cred.CredentialID
				break
			}
		}
		if credentialID == "" {
			return nil, "", fmt.Errorf("no ShopWired credential found — please connect a ShopWired store first")
		}
	}

	cred, err := h.repo.GetCredential(c.Request.Context(), tenantID, credentialID)
	if err != nil {
		return nil, "", fmt.Errorf("get credential: %w", err)
	}

	merged, err := h.marketplaceService.GetFullCredentials(c.Request.Context(), cred)
	if err != nil {
		return nil, "", fmt.Errorf("merge credentials: %w", err)
	}

	apiKey := merged["api_key"]
	apiSecret := merged["api_secret"]

	if apiKey == "" || apiSecret == "" {
		return nil, "", fmt.Errorf("incomplete ShopWired credentials: api_key and api_secret required")
	}

	client := shopwired.NewClient(apiKey, apiSecret)
	return client, credentialID, nil
}

// ── Test Connection ───────────────────────────────────────────────────────────

// TestConnection tests ShopWired credentials (before saving).
// POST /shopwired/test  { "api_key": "...", "api_secret": "..." }
func (h *ShopWiredHandler) TestConnection(c *gin.Context) {
	var req struct {
		APIKey    string `json:"api_key"`
		APISecret string `json:"api_secret"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "api_key and api_secret required"})
		return
	}

	if req.APIKey == "" || req.APISecret == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "api_key and api_secret are required"})
		return
	}

	client := shopwired.NewClient(req.APIKey, req.APISecret)
	if err := client.TestConnection(); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "ShopWired connection successful"})
}

// ── Save Credentials ──────────────────────────────────────────────────────────

// SaveCredential saves ShopWired credentials for the tenant.
// POST /shopwired/credentials  { "api_key": "...", "api_secret": "...", "store_name": "..." }
func (h *ShopWiredHandler) SaveCredential(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "tenant ID required"})
		return
	}

	var req struct {
		CredentialID string `json:"credential_id"` // if updating existing
		APIKey       string `json:"api_key"`
		APISecret    string `json:"api_secret"`
		StoreName    string `json:"store_name"` // display label
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	if req.APIKey == "" || req.APISecret == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "api_key and api_secret are required"})
		return
	}

	// Verify credentials before saving
	client := shopwired.NewClient(req.APIKey, req.APISecret)
	if err := client.TestConnection(); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": fmt.Sprintf("credentials rejected: %v", err)})
		return
	}

	now := time.Now()
	storeName := req.StoreName
	if storeName == "" {
		storeName = "ShopWired Store"
	}

	if req.CredentialID == "" {
		req.CredentialID = fmt.Sprintf("cred-shopwired-%d", time.Now().UnixMilli())
	}

	cred := &models.MarketplaceCredential{
		TenantID:    tenantID,
		Channel:     "shopwired",
		CredentialID: req.CredentialID,
		AccountName: storeName,
		Active:      true,
		CredentialData: map[string]string{
			"api_key":    req.APIKey,
			"api_secret": req.APISecret,
			"store_name": storeName,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := h.repo.SaveCredential(c.Request.Context(), cred); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": fmt.Sprintf("save credential: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":            true,
		"credential_id": cred.CredentialID,
		"message":       "ShopWired credentials saved successfully",
	})
}

// ── Categories ────────────────────────────────────────────────────────────────

// GetCategories returns all categories for the connected ShopWired store.
// GET /shopwired/categories
func (h *ShopWiredHandler) GetCategories(c *gin.Context) {
	client, _, err := h.getClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	cats, err := client.GetAllCategories()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error(), "categories": []interface{}{}})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "categories": cats, "count": len(cats)})
}

// ── Brands ────────────────────────────────────────────────────────────────────

// GetBrands returns all brands for the connected ShopWired store.
// GET /shopwired/brands
func (h *ShopWiredHandler) GetBrands(c *gin.Context) {
	client, _, err := h.getClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	brands, err := client.GetAllBrands()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error(), "brands": []interface{}{}})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "brands": brands, "count": len(brands)})
}

// ── Prepare Listing Draft ─────────────────────────────────────────────────────

type ShopWiredPrepareRequest struct {
	ProductID    string `json:"product_id" binding:"required"`
	CredentialID string `json:"credential_id"`
	CategoryID   int    `json:"category_id"` // optional override
	BrandID      int    `json:"brand_id"`    // optional override
}

type ShopWiredListingDraft struct {
	// Product fields
	Title           string   `json:"title"`
	Description     string   `json:"description"`
	SKU             string   `json:"sku"`
	Price           float64  `json:"price"`
	SalePrice       float64  `json:"salePrice,omitempty"`
	Stock           int      `json:"stock"`
	Weight          float64  `json:"weight,omitempty"`
	GTIN            string   `json:"gtin,omitempty"`
	MPN             string   `json:"mpn,omitempty"`
	MetaTitle       string   `json:"metaTitle,omitempty"`
	MetaDescription string   `json:"metaDescription,omitempty"`
	Active          bool     `json:"active"`
	Images          []string `json:"images"`
	// Resolved IDs
	CategoryID   int    `json:"categoryId,omitempty"`
	CategoryName string `json:"categoryName,omitempty"`
	BrandID      int    `json:"brandId,omitempty"`
	BrandName    string `json:"brandName,omitempty"`
	// Existing ShopWired product ID (if already listed)
	ShopWiredProductID int `json:"shopwiredProductId,omitempty"`
	// Variants
	Variants []ChannelVariantDraft `json:"variants,omitempty"`
}

// PrepareListingDraft builds a ShopWired listing draft from a MarketMate product.
// POST /shopwired/prepare
func (h *ShopWiredHandler) PrepareListingDraft(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req ShopWiredPrepareRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	if req.CredentialID != "" {
		c.Request.URL.RawQuery += "&credential_id=" + req.CredentialID
	}
	client, credentialID, err := h.getClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	// 1. Fetch product from Firestore
	product, err := h.productRepo.GetProduct(c.Request.Context(), tenantID, req.ProductID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"ok": false, "error": fmt.Sprintf("product not found: %v", err)})
		return
	}

	// 2. Build draft from product fields
	draft := buildShopWiredDraft(product)

	// 3. Resolve category — use override, or match by name, or leave empty
	var categories []shopwired.Category
	categories, err = client.GetAllCategories()
	if err != nil {
		log.Printf("[ShopWired Prepare] Could not fetch categories (non-fatal): %v", err)
	}

	if req.CategoryID > 0 {
		draft.CategoryID = req.CategoryID
		for _, cat := range categories {
			if cat.ID == req.CategoryID {
				draft.CategoryName = cat.Title
				break
			}
		}
	} else if len(product.Tags) > 0 {
		// Try matching first tag as category
		for _, cat := range categories {
			if strings.EqualFold(cat.Title, product.Tags[0]) {
				draft.CategoryID = cat.ID
				draft.CategoryName = cat.Title
				break
			}
		}
	}

	// 4. Resolve brand — use override, or match by brand field on product
	var brands []shopwired.Brand
	brands, err = client.GetAllBrands()
	if err != nil {
		log.Printf("[ShopWired Prepare] Could not fetch brands (non-fatal): %v", err)
	}

	if req.BrandID > 0 {
		draft.BrandID = req.BrandID
		for _, b := range brands {
			if b.ID == req.BrandID {
				draft.BrandName = b.Title
				break
			}
		}
	} else if product.Brand != nil && *product.Brand != "" {
		for _, b := range brands {
			if strings.EqualFold(b.Title, *product.Brand) {
				draft.BrandID = b.ID
				draft.BrandName = b.Title
				break
			}
		}
	}

	// 5. Check for existing ShopWired listing
	if credentialID != "" {
		existing, _ := h.repo.FindListingByProductAndAccount(c.Request.Context(), tenantID, req.ProductID, credentialID)
		if existing != nil && existing.ChannelIdentifiers != nil {
			if pid, err := strconv.Atoi(existing.ChannelIdentifiers.ListingID); err == nil && pid > 0 {
				draft.ShopWiredProductID = pid
			}
		}
	}

	// 6. Load PIM variants
	fallbackImage := ""
	if len(draft.Images) > 0 {
		fallbackImage = draft.Images[0]
	}
	draft.Variants = loadChannelVariants(
		c.Request.Context(), h.productRepo, tenantID, req.ProductID,
		fmt.Sprintf("%.2f", draft.Price), fallbackImage,
	)

	c.JSON(http.StatusOK, gin.H{
		"ok":         true,
		"draft":      draft,
		"categories": categories,
		"brands":     brands,
	})
}

// ── Submit Listing ────────────────────────────────────────────────────────────

type ShopWiredSubmitRequest struct {
	ProductID    string `json:"product_id" binding:"required"`
	CredentialID string `json:"credential_id"`

	// ShopWired product ID — if set, performs an update instead of create
	ShopWiredProductID int `json:"shopwiredProductId"`

	Title           string  `json:"title" binding:"required"`
	Description     string  `json:"description"`
	SKU             string  `json:"sku"`
	Price           float64 `json:"price"`
	SalePrice       float64 `json:"salePrice,omitempty"`
	Stock           int     `json:"stock"`
	Weight          float64 `json:"weight,omitempty"`
	GTIN            string  `json:"gtin,omitempty"`
	MPN             string  `json:"mpn,omitempty"`
	MetaTitle       string  `json:"metaTitle,omitempty"`
	MetaDescription string  `json:"metaDescription,omitempty"`
	Active          bool    `json:"active"`

	CategoryID   int    `json:"categoryId,omitempty"`
	CategoryName string `json:"categoryName,omitempty"` // used for lookup-or-create
	BrandID      int    `json:"brandId,omitempty"`
	BrandName    string `json:"brandName,omitempty"` // used for lookup-or-create

	Images   []string              `json:"images"`
	Variants []ChannelVariantDraft `json:"variants,omitempty"`
}

// SubmitListing creates or updates a product on ShopWired.
// POST /shopwired/submit
func (h *ShopWiredHandler) SubmitListing(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req ShopWiredSubmitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	if req.CredentialID != "" {
		c.Request.URL.RawQuery += "&credential_id=" + req.CredentialID
	}
	client, credentialID, err := h.getClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	// Resolve category ID (lookup-or-create if name supplied)
	categoryID := req.CategoryID
	if categoryID == 0 && req.CategoryName != "" {
		cat, catErr := client.GetOrCreateCategory(req.CategoryName)
		if catErr != nil {
			log.Printf("[ShopWired Submit] category lookup/create failed: %v", catErr)
		} else {
			categoryID = cat.ID
		}
	}

	// Resolve brand ID (lookup-or-create if name supplied)
	brandID := req.BrandID
	if brandID == 0 && req.BrandName != "" {
		brand, brandErr := client.GetOrCreateBrand(req.BrandName)
		if brandErr != nil {
			log.Printf("[ShopWired Submit] brand lookup/create failed: %v", brandErr)
		} else {
			brandID = brand.ID
		}
	}

	// Build the product payload
	payload := map[string]interface{}{
		"title":       req.Title,
		"description": req.Description,
		"price":       req.Price,
		"active":      req.Active,
	}
	if req.SKU != "" {
		payload["sku"] = req.SKU
	}
	if req.SalePrice > 0 {
		payload["salePrice"] = req.SalePrice
	}
	if req.Stock > 0 {
		payload["stock"] = req.Stock
	}
	if req.Weight > 0 {
		payload["weight"] = req.Weight
	}
	if req.GTIN != "" {
		payload["gtin"] = req.GTIN
	}
	if req.MPN != "" {
		payload["mpn"] = req.MPN
	}
	if req.MetaTitle != "" {
		payload["metaTitle"] = req.MetaTitle
	}
	if req.MetaDescription != "" {
		payload["metaDescription"] = req.MetaDescription
	}
	if categoryID > 0 {
		payload["categoryIds"] = []int{categoryID}
	}
	if brandID > 0 {
		payload["brandId"] = brandID
	}

	isUpdate := req.ShopWiredProductID > 0
	var shopwiredProductID int

	now := time.Now()

	if isUpdate {
		// Update existing product
		shopwiredProductID = req.ShopWiredProductID
		updated, err := client.UpdateProduct(shopwiredProductID, payload)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"ok": false, "error": fmt.Sprintf("update product failed: %v", err)})
			return
		}
		shopwiredProductID = updated.ID
		log.Printf("[ShopWired] Updated product id=%d title=%q", shopwiredProductID, req.Title)
	} else {
		// Create new product
		created, err := client.CreateProduct(payload)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"ok": false, "error": fmt.Sprintf("create product failed: %v", err)})
			return
		}
		shopwiredProductID = created.ID
		log.Printf("[ShopWired] Created product id=%d title=%q", shopwiredProductID, req.Title)
	}

	// Upload images
	for _, imgURL := range req.Images {
		if err := client.AddProductImageURL(shopwiredProductID, imgURL); err != nil {
			log.Printf("[ShopWired] Image upload failed for product %d (non-fatal): %v", shopwiredProductID, err)
		}
	}

	// Handle variants — create options + values if present
	activeVariants := filterActiveVariants(req.Variants)
	if len(activeVariants) >= 2 && !isUpdate {
		if err := h.createVariations(client, shopwiredProductID, activeVariants); err != nil {
			log.Printf("[ShopWired] Variation setup failed (non-fatal): %v", err)
		}
	}

	// Persist listing to Firestore
	listingIDStr := strconv.Itoa(shopwiredProductID)

	if isUpdate && credentialID != "" {
		existing, _ := h.repo.FindListingByProductAndAccount(c.Request.Context(), tenantID, req.ProductID, credentialID)
		if existing != nil {
			existing.State = "published"
			existing.ChannelIdentifiers = &models.ChannelIdentifiers{
				ListingID: listingIDStr,
				SKU:       req.SKU,
			}
			existing.Overrides = &models.ListingOverrides{
				Title:           req.Title,
				CategoryMapping: strconv.Itoa(categoryID),
				Images:          req.Images,
				Price:           &req.Price,
				Quantity:        &req.Stock,
			}
			existing.UpdatedAt = now
			if err := h.repo.UpdateListing(c.Request.Context(), existing); err != nil {
				log.Printf("[ShopWired] WARNING: listing update in Firestore failed: %v", err)
			}
		}
	} else {
		listing := &models.Listing{
			ListingID:        fmt.Sprintf("shopwired-%s-%d", req.SKU, now.Unix()),
			TenantID:         tenantID,
			ProductID:        req.ProductID,
			Channel:          "shopwired",
			ChannelAccountID: credentialID,
			State:            "published",
			ChannelIdentifiers: &models.ChannelIdentifiers{
				ListingID: listingIDStr,
				SKU:       req.SKU,
			},
			Overrides: &models.ListingOverrides{
				Title:           req.Title,
				CategoryMapping: strconv.Itoa(categoryID),
				Images:          req.Images,
				Price:           &req.Price,
				Quantity:        &req.Stock,
			},
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := h.repo.CreateListing(c.Request.Context(), listing); err != nil {
			log.Printf("[ShopWired] WARNING: listing create in Firestore failed: %v", err)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":                 true,
		"isUpdate":           isUpdate,
		"shopwiredProductId": shopwiredProductID,
	})
}

// ── Update Product ────────────────────────────────────────────────────────────

// UpdateProductListing updates a product by ShopWired product ID.
// PUT /shopwired/products/:id
func (h *ShopWiredHandler) UpdateProductListing(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "invalid product id"})
		return
	}

	client, _, err := h.getClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	var payload map[string]interface{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	updated, err := client.UpdateProduct(id, payload)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "product": updated})
}

// ── Delete Product ────────────────────────────────────────────────────────────

// DeleteProduct deletes a product from ShopWired.
// DELETE /shopwired/products/:id
func (h *ShopWiredHandler) DeleteProduct(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "invalid product id"})
		return
	}

	client, _, err := h.getClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	if err := client.DeleteProduct(id); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "deleted": id})
}

// ── List Products ─────────────────────────────────────────────────────────────

// GetProducts returns a page of products from ShopWired.
// GET /shopwired/products?offset=0&count=50
func (h *ShopWiredHandler) GetProducts(c *gin.Context) {
	client, _, err := h.getClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	count, _ := strconv.Atoi(c.DefaultQuery("count", "50"))

	products, err := client.ListProducts(offset, count)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error(), "products": []interface{}{}})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "products": products, "count": len(products)})
}

// ── Update Stock ──────────────────────────────────────────────────────────────

// UpdateStock updates stock for a SKU on ShopWired.
// POST /shopwired/stock  { "sku": "...", "quantity": 42 }
func (h *ShopWiredHandler) UpdateStock(c *gin.Context) {
	client, _, err := h.getClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	var req struct {
		SKU      string `json:"sku" binding:"required"`
		Quantity int    `json:"quantity"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	if err := client.UpdateStock(req.SKU, req.Quantity); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "sku": req.SKU, "quantity": req.Quantity})
}

// ── Webhook Management ────────────────────────────────────────────────────────

// RegisterWebhooks registers the standard order webhooks for this store.
// POST /shopwired/webhooks/register  { "base_url": "https://marketmate-api..." }
func (h *ShopWiredHandler) RegisterWebhooks(c *gin.Context) {
	client, credentialID, err := h.getClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	var req struct {
		BaseURL string `json:"base_url"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.BaseURL == "" {
		// Fall back to the default API URL
		req.BaseURL = "https://marketmate-api-487246736287.europe-west2.run.app"
	}

	topics := []string{"order.created", "order.finalized", "order.updated"}
	var registered []shopwired.Webhook
	var errs []string

	for _, topic := range topics {
		webhookURL := fmt.Sprintf("%s/webhooks/orders/shopwired?credential_id=%s", req.BaseURL, credentialID)
		wh, err := client.EnsureWebhook(topic, webhookURL)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", topic, err))
			continue
		}
		registered = append(registered, *wh)
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":         len(errs) == 0,
		"registered": registered,
		"errors":     errs,
	})
}

// ============================================================================
// HELPERS
// ============================================================================

// buildShopWiredDraft maps a PIM product to a ShopWiredListingDraft.
func buildShopWiredDraft(product *models.Product) *ShopWiredListingDraft {
	draft := &ShopWiredListingDraft{
		Title:  product.Title,
		SKU:    product.SKU,
		Active: true,
		Stock:  1,
	}

	if product.Description != nil {
		draft.Description = *product.Description
	}
	if product.Brand != nil {
		draft.BrandName = *product.Brand
	}

	// Identifiers
	if product.Identifiers != nil {
		if product.Identifiers.EAN != nil && *product.Identifiers.EAN != "" {
			draft.GTIN = *product.Identifiers.EAN
		} else if product.Identifiers.UPC != nil && *product.Identifiers.UPC != "" {
			draft.GTIN = *product.Identifiers.UPC
		}
		if product.Identifiers.MPN != nil && *product.Identifiers.MPN != "" {
			draft.MPN = *product.Identifiers.MPN
		}
	}

	// Weight
	if product.Weight != nil && product.Weight.Value != nil {
		draft.Weight = *product.Weight.Value
	}

	// Meta
	draft.MetaTitle = product.Title

	// Gather images from assets
	for _, asset := range product.Assets {
		if asset.URL != "" {
			draft.Images = append(draft.Images, asset.URL)
		}
	}

	return draft
}

// createVariations creates option/value combos for a multi-variant product.
func (h *ShopWiredHandler) createVariations(client *shopwired.Client, productID int, variants []ChannelVariantDraft) error {
	// Collect unique option keys (e.g. "Size", "Colour")
	optionKeys := []string{}
	seen := map[string]bool{}
	for _, v := range variants {
		for k := range v.Combination {
			if !seen[k] {
				seen[k] = true
				optionKeys = append(optionKeys, k)
			}
		}
	}

	// Create each option and its values
	optionIDMap := map[string]int{}
	for _, key := range optionKeys {
		opt, err := client.CreateProductOption(productID, key)
		if err != nil {
			return fmt.Errorf("create option %q: %w", key, err)
		}
		optionIDMap[key] = opt.ID

		// Collect unique values for this option
		valueSeen := map[string]bool{}
		for _, v := range variants {
			if val, ok := v.Combination[key]; ok && !valueSeen[val] {
				valueSeen[val] = true
				if _, err := client.CreateProductOptionValue(productID, opt.ID, val); err != nil {
					log.Printf("[ShopWired] Could not create option value %q for option %q: %v", val, key, err)
				}
			}
		}
	}

	// Update auto-generated variation records with SKU/price/stock
	varRecords, err := client.ListProductVariations(productID)
	if err != nil || len(varRecords) == 0 {
		return nil // Non-fatal — ShopWired may not have generated them yet
	}

	for i, varRecord := range varRecords {
		if i >= len(variants) {
			break
		}
		v := variants[i]
		update := map[string]interface{}{
			"active": v.Active,
		}
		if v.SKU != "" {
			update["sku"] = v.SKU
		}
		if v.Price != "" {
			if p, err := strconv.ParseFloat(v.Price, 64); err == nil && p > 0 {
				update["price"] = p
			}
		}
		if err := client.UpdateProductVariation(productID, varRecord.ID, update); err != nil {
			log.Printf("[ShopWired] Could not update variation %d: %v", varRecord.ID, err)
		}
	}

	return nil
}

// filterActiveVariants returns only active variants.
func filterActiveVariants(variants []ChannelVariantDraft) []ChannelVariantDraft {
	var active []ChannelVariantDraft
	for _, v := range variants {
		if v.Active {
			active = append(active, v)
		}
	}
	return active
}
