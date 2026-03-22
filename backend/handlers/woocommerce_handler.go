package handlers

// ============================================================================
// WOOCOMMERCE HANDLER
// ============================================================================
// Routes:
//   POST /woocommerce/test            → test connection with given credentials
//   GET  /woocommerce/categories      → product categories tree
//   GET  /woocommerce/attributes      → product attributes
//   POST /woocommerce/prepare         → prepare listing draft from MarketMate product
//   POST /woocommerce/submit          → create product on WooCommerce
//   PUT  /woocommerce/products/:id    → update product
//   DELETE /woocommerce/products/:id  → delete product
//   GET  /woocommerce/products        → list products
// ============================================================================

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"module-a/marketplace/clients/woocommerce"
	"module-a/models"
	"module-a/repository"
	"module-a/services"
)

type WooCommerceHandler struct {
	marketplaceService *services.MarketplaceService
	repo               *repository.MarketplaceRepository
	productRepo        *repository.FirestoreRepository
}

func NewWooCommerceHandler(
	marketplaceService *services.MarketplaceService,
	repo *repository.MarketplaceRepository,
	productRepo *repository.FirestoreRepository,
) *WooCommerceHandler {
	return &WooCommerceHandler{
		marketplaceService: marketplaceService,
		repo:               repo,
		productRepo:        productRepo,
	}
}

// ── Credential resolution ─────────────────────────────────────────────────────

func (h *WooCommerceHandler) getWooClient(c *gin.Context) (*woocommerce.Client, string, error) {
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
			if cred.Channel == "woocommerce" && cred.Active {
				credentialID = cred.CredentialID
				break
			}
		}
		if credentialID == "" {
			return nil, "", fmt.Errorf("no WooCommerce credential found — please connect a WooCommerce store first")
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

	storeURL := merged["store_url"]
	consumerKey := merged["consumer_key"]
	consumerSecret := merged["consumer_secret"]

	if storeURL == "" || consumerKey == "" || consumerSecret == "" {
		return nil, "", fmt.Errorf("incomplete WooCommerce credentials: store_url, consumer_key, consumer_secret required")
	}

	client := woocommerce.NewClient(storeURL, consumerKey, consumerSecret)
	return client, credentialID, nil
}

// ── Test Connection ───────────────────────────────────────────────────────────

// TestConnection tests a WooCommerce connection with provided credentials (before saving).
// POST /woocommerce/test  { "store_url": "...", "consumer_key": "...", "consumer_secret": "..." }
func (h *WooCommerceHandler) TestConnection(c *gin.Context) {
	var req struct {
		StoreURL       string `json:"store_url" binding:"required"`
		ConsumerKey    string `json:"consumer_key" binding:"required"`
		ConsumerSecret string `json:"consumer_secret" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	client := woocommerce.NewClient(req.StoreURL, req.ConsumerKey, req.ConsumerSecret)
	if err := client.TestConnection(); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	// Get system status for extra info
	status, _ := client.GetSystemStatus()
	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"message": "WooCommerce connection successful",
		"status":  status,
	})
}

// ── Categories ────────────────────────────────────────────────────────────────

// GetCategories returns all product categories.
// GET /woocommerce/categories
func (h *WooCommerceHandler) GetCategories(c *gin.Context) {
	client, _, err := h.getWooClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error(), "categories": []interface{}{}})
		return
	}

	cats, err := client.GetCategories()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error(), "categories": []interface{}{}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "categories": cats})
}

// ── Attributes ────────────────────────────────────────────────────────────────

// GetAttributes returns all WooCommerce product attributes.
// GET /woocommerce/attributes
func (h *WooCommerceHandler) GetAttributes(c *gin.Context) {
	client, _, err := h.getWooClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	attrs, err := client.GetAttributes()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error(), "attributes": []interface{}{}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "attributes": attrs})
}

// ── Prepare & Submit ──────────────────────────────────────────────────────────

// PrepareListingDraft loads MarketMate product data and builds a WooCommerce draft.
// POST /woocommerce/prepare  { "product_id": "...", "credential_id": "..." }
func (h *WooCommerceHandler) PrepareListingDraft(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req struct {
		ProductID    string `json:"product_id" binding:"required"`
		CredentialID string `json:"credential_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	product, err := h.productRepo.GetProduct(c.Request.Context(), tenantID, req.ProductID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"ok": false, "error": "Product not found: " + err.Error()})
		return
	}

	var images []string
	if product.Attributes != nil {
		if imgRaw, ok := product.Attributes["images"]; ok {
			switch v := imgRaw.(type) {
			case []interface{}:
				for _, img := range v {
					if s, ok := img.(string); ok {
						images = append(images, s)
					}
				}
			case []string:
				images = v
			}
		}
	}

	draft := gin.H{
		"name":          product.Title,
		"description":   product.Description,
		"sku":           product.Attributes["source_sku"],
		"regular_price": product.Attributes["price"],
		"stock_quantity": product.Attributes["quantity"],
		"weight":        product.Attributes["weight_kg"],
		"dimensions": gin.H{
			"length": product.Attributes["length_cm"],
			"width":  product.Attributes["width_cm"],
			"height": product.Attributes["height_cm"],
		},
		"images":       images,
		"type":         "simple",
		"status":       "draft",
		"manage_stock": true,
		"variants":     loadChannelVariants(c.Request.Context(), h.productRepo, tenantID, req.ProductID, fmt.Sprintf("%v", product.Attributes["price"]), ""),
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":         true,
		"product_id": req.ProductID,
		"draft":      draft,
	})
}

// SubmitListing creates a product on WooCommerce.
// POST /woocommerce/submit
func (h *WooCommerceHandler) SubmitListing(c *gin.Context) {
	client, _, err := h.getWooClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	var req struct {
		woocommerce.Product
		Variants []ChannelVariantDraft `json:"variants"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "invalid payload: " + err.Error()})
		return
	}

	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "name is required"})
		return
	}

	// Default status to publish if not set
	if req.Status == "" {
		req.Status = "publish"
	}

	// Collect active variants
	activeVariants := make([]ChannelVariantDraft, 0)
	for _, v := range req.Variants {
		if v.Active {
			activeVariants = append(activeVariants, v)
		}
	}

	if len(activeVariants) >= 2 {
		// ── Variable product flow ───────────────────────────────────────
		req.Type = "variable"
		req.ManageStock = false // stock is managed per-variation

		// Build product-level attributes from combination keys (required for WC variations)
		attrMap := map[string][]string{}
		for _, v := range activeVariants {
			for k, val := range v.Combination {
				attrMap[k] = appendUnique(attrMap[k], val)
			}
		}
		attrs := []woocommerce.ProductAttribute{}
		for name, vals := range attrMap {
			attrs = append(attrs, woocommerce.ProductAttribute{
				Name:      name,
				Options:   vals,
				Variation: true,
				Visible:   true,
			})
		}
		req.Product.Attributes = attrs

		// Create the parent product
		created, err := client.CreateProduct(&req.Product)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"ok": false, "error": fmt.Sprintf("create variable product: %v", err)})
			return
		}
		log.Printf("[WooCommerce Submit] Variable product created: ID=%d", created.ID)

		// Create one variation per active variant
		type varResult struct {
			SKU         string `json:"sku"`
			VariationID int    `json:"variation_id,omitempty"`
			Error       string `json:"error,omitempty"`
		}
		submitted := []varResult{}
		warnings := []string{}

		for _, v := range activeVariants {
			qty := 0
			if q, err := strconv.Atoi(v.Stock); err == nil {
				qty = q
			}
			varAttrs := []woocommerce.VariationAttr{}
			for k, val := range v.Combination {
				varAttrs = append(varAttrs, woocommerce.VariationAttr{Name: k, Option: val})
			}
			variation := &woocommerce.ProductVariation{
				SKU:          v.SKU,
				RegularPrice: v.Price,
				StockQuantity: &qty,
				StockStatus:  "instock",
				Attributes:   varAttrs,
			}
			createdVar, err := client.CreateVariation(created.ID, variation)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("variant SKU %s: %v", v.SKU, err))
				submitted = append(submitted, varResult{SKU: v.SKU, Error: err.Error()})
				continue
			}
			submitted = append(submitted, varResult{SKU: v.SKU, VariationID: createdVar.ID})
			log.Printf("[WooCommerce Submit] Variation created: ID=%d SKU=%s", createdVar.ID, v.SKU)
		}

		// Orphan cleanup: if every variation failed, delete the parent product so
		// WooCommerce is not left with an empty variable product shell.
		successCount := 0
		for _, r := range submitted {
			if r.Error == "" {
				successCount++
			}
		}
		if successCount == 0 && len(submitted) > 0 {
			log.Printf("[WooCommerce Cleanup] All variations failed — deleting orphaned parent product ID=%d", created.ID)
			if delErr := client.DeleteProduct(created.ID); delErr != nil {
				warnings = append(warnings, fmt.Sprintf("all variations failed and parent cleanup also failed (ID=%d): %v", created.ID, delErr))
				log.Printf("[WooCommerce Cleanup] Failed to delete orphaned parent ID=%d: %v", created.ID, delErr)
			} else {
				warnings = append(warnings, fmt.Sprintf("all variations failed — orphaned parent product (ID=%d) was deleted", created.ID))
				log.Printf("[WooCommerce Cleanup] Deleted orphaned parent ID=%d", created.ID)
			}
			c.JSON(http.StatusOK, gin.H{
				"ok":       false,
				"error":    "All variations failed to create — parent product has been deleted to prevent orphans",
				"warnings": warnings,
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"ok":          true,
			"product_id":  created.ID,
			"permalink":   created.Permalink,
			"status":      created.Status,
			"type":        "variable",
			"variations":  submitted,
			"warnings":    warnings,
		})
		return
	}

	// ── Simple product (original behaviour) ─────────────────────────────
	if req.Type == "" {
		req.Type = "simple"
	}
	created, err := client.CreateProduct(&req.Product)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":         true,
		"product_id": created.ID,
		"permalink":  created.Permalink,
		"status":     created.Status,
	})
}

// UpdateProductListing updates an existing WooCommerce product.
// PUT /woocommerce/products/:id
func (h *WooCommerceHandler) UpdateProductListing(c *gin.Context) {
	client, _, err := h.getWooClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	idStr := c.Param("id")
	productID, err := strconv.Atoi(idStr)
	if err != nil || productID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "invalid product id"})
		return
	}

	var payload woocommerce.Product
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	updated, err := client.UpdateProduct(productID, &payload)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "product_id": updated.ID, "status": updated.Status})
}

// DeleteProduct removes a product from WooCommerce.
// DELETE /woocommerce/products/:id
func (h *WooCommerceHandler) DeleteProduct(c *gin.Context) {
	client, _, err := h.getWooClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	idStr := c.Param("id")
	productID, err := strconv.Atoi(idStr)
	if err != nil || productID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "invalid product id"})
		return
	}

	if err := client.DeleteProduct(productID); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "deleted": productID})
}

// GetProducts returns products from WooCommerce (paginated).
// GET /woocommerce/products?page=1&per_page=50&status=publish
func (h *WooCommerceHandler) GetProducts(c *gin.Context) {
	client, _, err := h.getWooClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error(), "products": []interface{}{}})
		return
	}

	page := 1
	perPage := 50
	if p := c.Query("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil {
			page = v
		}
	}
	if pp := c.Query("per_page"); pp != "" {
		if v, err := strconv.Atoi(pp); err == nil && v <= 100 {
			perPage = v
		}
	}
	status := c.Query("status")
	if status == "" {
		status = "any"
	}

	products, err := client.GetProducts(page, perPage, status)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error(), "products": []interface{}{}})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":       true,
		"products": products,
		"page":     page,
		"per_page": perPage,
	})
}

// ── Credential save (no OAuth needed) ────────────────────────────────────────

// SaveCredential saves WooCommerce credentials after verifying the connection.
// POST /woocommerce/connect
func (h *WooCommerceHandler) SaveCredential(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req struct {
		AccountName    string `json:"account_name"`
		StoreURL       string `json:"store_url" binding:"required"`
		ConsumerKey    string `json:"consumer_key" binding:"required"`
		ConsumerSecret string `json:"consumer_secret" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	// Test connection before saving
	client := woocommerce.NewClient(req.StoreURL, req.ConsumerKey, req.ConsumerSecret)
	if err := client.TestConnection(); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": "Connection test failed: " + err.Error()})
		return
	}

	accountName := req.AccountName
	if accountName == "" {
		accountName = req.StoreURL
	}

	credData := map[string]string{
		"store_url":       req.StoreURL,
		"consumer_key":    req.ConsumerKey,
		"consumer_secret": req.ConsumerSecret,
	}

	// Check for existing WooCommerce credential matching this store URL
	existingCreds, _ := h.repo.ListCredentials(c.Request.Context(), tenantID)
	credentialID := ""
	for _, ec := range existingCreds {
		if ec.Channel == "woocommerce" && ec.Active {
			if cd, ok := ec.CredentialData["store_url"]; ok && cd == req.StoreURL {
				credentialID = ec.CredentialID
				break
			}
		}
	}

	if credentialID != "" {
		existingCred, err := h.repo.GetCredential(c.Request.Context(), tenantID, credentialID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "failed to load existing credential: " + err.Error()})
			return
		}
		for k, v := range credData {
			existingCred.CredentialData[k] = v
		}
		existingCred.AccountName = accountName
		if err := h.repo.SaveCredential(c.Request.Context(), existingCred); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "failed to save credential: " + err.Error()})
			return
		}
	} else {
		credentialID = "cred-woocommerce-" + fmt.Sprintf("%d", time.Now().UnixMilli())
		newCred := &models.MarketplaceCredential{
			CredentialID:   credentialID,
			TenantID:       tenantID,
			Channel:        "woocommerce",
			AccountName:    accountName,
			Environment:    "production",
			Active:         true,
			CredentialData: credData,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}
		if err := h.repo.SaveCredential(c.Request.Context(), newCred); err != nil {
			log.Printf("[WooCommerce] Failed to create credential: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "failed to save credential: " + err.Error()})
			return
		}
	}

	log.Printf("[WooCommerce] Credential saved: %s for store %s", credentialID, req.StoreURL)
	c.JSON(http.StatusOK, gin.H{
		"ok":            true,
		"credential_id": credentialID,
		"message":       "WooCommerce store connected successfully",
	})
}
