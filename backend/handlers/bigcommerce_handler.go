package handlers

// ============================================================================
// BIGCOMMERCE HANDLER
// ============================================================================
// Routes:
//   POST   /bigcommerce/connect          → test + save credentials
//   POST   /bigcommerce/test             → test connection with given credentials
//   GET    /bigcommerce/categories       → product category list
//   POST   /bigcommerce/prepare          → prepare listing draft from MarketMate product
//   POST   /bigcommerce/submit           → create product on BigCommerce
//   PUT    /bigcommerce/products/:id     → update product
//   DELETE /bigcommerce/products/:id     → delete product
//   GET    /bigcommerce/products         → list products (paginated)
// ============================================================================

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"module-a/marketplace/clients/bigcommerce"
	"module-a/models"
	"module-a/repository"
	"module-a/services"
)

type BigCommerceHandler struct {
	marketplaceService *services.MarketplaceService
	repo               *repository.MarketplaceRepository
	productRepo        *repository.FirestoreRepository
}

func NewBigCommerceHandler(
	marketplaceService *services.MarketplaceService,
	repo *repository.MarketplaceRepository,
	productRepo *repository.FirestoreRepository,
) *BigCommerceHandler {
	return &BigCommerceHandler{
		marketplaceService: marketplaceService,
		repo:               repo,
		productRepo:        productRepo,
	}
}

// ── Credential resolution ─────────────────────────────────────────────────────

func (h *BigCommerceHandler) getBigCommerceClient(c *gin.Context) (*bigcommerce.Client, string, error) {
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
			if cred.Channel == "bigcommerce" && cred.Active {
				credentialID = cred.CredentialID
				break
			}
		}
		if credentialID == "" {
			return nil, "", fmt.Errorf("no BigCommerce credential found — please connect a BigCommerce store first")
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

	storeHash := merged["store_hash"]
	clientID := merged["client_id"]
	accessToken := merged["access_token"]

	if storeHash == "" || accessToken == "" {
		return nil, "", fmt.Errorf("incomplete BigCommerce credentials: store_hash and access_token are required")
	}

	client := bigcommerce.NewClient(storeHash, clientID, accessToken)
	return client, credentialID, nil
}

// ── Save Credential ───────────────────────────────────────────────────────────

// SaveCredential saves BigCommerce credentials after verifying the connection.
// POST /bigcommerce/connect
func (h *BigCommerceHandler) SaveCredential(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req struct {
		AccountName string `json:"account_name"`
		StoreHash   string `json:"store_hash" binding:"required"`
		ClientID    string `json:"client_id"`
		AccessToken string `json:"access_token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	// Test connection before saving
	client := bigcommerce.NewClient(req.StoreHash, req.ClientID, req.AccessToken)
	if err := client.TestConnection(); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": "Connection test failed: " + err.Error()})
		return
	}

	accountName := req.AccountName
	if accountName == "" {
		accountName = "BigCommerce Store (" + req.StoreHash + ")"
	}

	credData := map[string]string{
		"store_hash":   req.StoreHash,
		"client_id":    req.ClientID,
		"access_token": req.AccessToken,
	}

	// Check for existing BigCommerce credential matching this store hash
	existingCreds, _ := h.repo.ListCredentials(c.Request.Context(), tenantID)
	credentialID := ""
	for _, ec := range existingCreds {
		if ec.Channel == "bigcommerce" && ec.Active {
			if cd, ok := ec.CredentialData["store_hash"]; ok && cd == req.StoreHash {
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
		credentialID = "cred-bigcommerce-" + fmt.Sprintf("%d", time.Now().UnixMilli())
		newCred := &models.MarketplaceCredential{
			CredentialID:   credentialID,
			TenantID:       tenantID,
			Channel:        "bigcommerce",
			AccountName:    accountName,
			Environment:    "production",
			Active:         true,
			CredentialData: credData,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}
		if err := h.repo.SaveCredential(c.Request.Context(), newCred); err != nil {
			log.Printf("[BigCommerce] Failed to create credential: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "failed to save credential: " + err.Error()})
			return
		}
	}

	log.Printf("[BigCommerce] Credential saved: %s for store hash %s", credentialID, req.StoreHash)
	c.JSON(http.StatusOK, gin.H{
		"ok":            true,
		"credential_id": credentialID,
		"message":       "BigCommerce store connected successfully",
	})
}

// ── Test Connection ───────────────────────────────────────────────────────────

// TestConnection tests a BigCommerce connection with provided credentials (before saving).
// POST /bigcommerce/test  { "store_hash": "...", "client_id": "...", "access_token": "..." }
func (h *BigCommerceHandler) TestConnection(c *gin.Context) {
	var req struct {
		StoreHash   string `json:"store_hash" binding:"required"`
		ClientID    string `json:"client_id"`
		AccessToken string `json:"access_token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	client := bigcommerce.NewClient(req.StoreHash, req.ClientID, req.AccessToken)
	if err := client.TestConnection(); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"message": "BigCommerce connection successful",
	})
}

// ── Categories ────────────────────────────────────────────────────────────────

// GetCategories returns the BigCommerce category list.
// GET /bigcommerce/categories
func (h *BigCommerceHandler) GetCategories(c *gin.Context) {
	client, _, err := h.getBigCommerceClient(c)
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

// ── Prepare & Submit ──────────────────────────────────────────────────────────

// PrepareListingDraft loads MarketMate product data and builds a BigCommerce draft.
// POST /bigcommerce/prepare  { "product_id": "...", "credential_id": "..." }
func (h *BigCommerceHandler) PrepareListingDraft(c *gin.Context) {
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

	// Extract images
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

	// Build SKU
	sku := ""
	if product.Attributes != nil {
		if s, ok := product.Attributes["source_sku"].(string); ok {
			sku = s
		}
	}
	if sku == "" {
		sku = req.ProductID
	}

	price := 0.0
	if product.Attributes != nil {
		if p, ok := product.Attributes["price"].(float64); ok {
			price = p
		}
	}

	qty := 0
	if product.Attributes != nil {
		switch q := product.Attributes["quantity"].(type) {
		case float64:
			qty = int(q)
		case int:
			qty = q
		}
	}

	weight := 0.0
	if product.Attributes != nil {
		if w, ok := product.Attributes["weight_kg"].(float64); ok {
			weight = w
		}
	}

	draft := gin.H{
		"name":            product.Title,
		"sku":             sku,
		"description":     product.Description,
		"price":           price,
		"inventory_level": qty,
		"weight":          weight,
		"type":            "physical",
		"is_visible":      true,
		"availability":    "available",
		"condition":       "New",
		"images":          images,
		"variants":        loadChannelVariants(c.Request.Context(), h.productRepo, tenantID, req.ProductID, fmt.Sprintf("%.2f", price), ""),
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":         true,
		"product_id": req.ProductID,
		"draft":      draft,
	})
}

// SubmitListing creates a product on BigCommerce.
// POST /bigcommerce/submit
//
// When the payload includes a "variants" array with ≥2 active entries, the
// BigCommerce product is created with native variants (one per active entry).
func (h *BigCommerceHandler) SubmitListing(c *gin.Context) {
	client, _, err := h.getBigCommerceClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	var req struct {
		bigcommerce.Product
		Variants []ChannelVariantDraft `json:"variants"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "invalid payload: " + err.Error()})
		return
	}

	// Build native BC variants from active ChannelVariantDrafts
	activeVariants := make([]ChannelVariantDraft, 0)
	for _, v := range req.Variants {
		if v.Active {
			activeVariants = append(activeVariants, v)
		}
	}

	if len(activeVariants) >= 2 {
		bcVariants := make([]bigcommerce.ProductVariant, 0, len(activeVariants))
		for _, v := range activeVariants {
			bv := bigcommerce.ProductVariant{
				SKU:       v.SKU,
				IsVisible: true,
			}
			if p, err := strconv.ParseFloat(v.Price, 64); err == nil && p > 0 {
				bv.Price = p
			}
			if s, err := strconv.Atoi(v.Stock); err == nil && s >= 0 {
				bv.InventoryLevel = s
			}
			bcVariants = append(bcVariants, bv)
		}
		req.Product.Variants = bcVariants
		if req.Product.InventoryTracking == "" {
			req.Product.InventoryTracking = "variant"
		}
	}

	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "name is required"})
		return
	}

	created, err := client.CreateProduct(&req.Product)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":         true,
		"id":         created.ID,
		"sku":        created.SKU,
		"is_visible": created.IsVisible,
	})
}

// UpdateProductListing updates an existing BigCommerce product.
// PUT /bigcommerce/products/:id
func (h *BigCommerceHandler) UpdateProductListing(c *gin.Context) {
	client, _, err := h.getBigCommerceClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "id must be a valid numeric BigCommerce product ID"})
		return
	}

	var payload bigcommerce.Product
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	updated, err := client.UpdateProduct(id, &payload)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "id": updated.ID, "sku": updated.SKU, "is_visible": updated.IsVisible})
}

// DeleteProduct removes a product from BigCommerce.
// DELETE /bigcommerce/products/:id
func (h *BigCommerceHandler) DeleteProduct(c *gin.Context) {
	client, _, err := h.getBigCommerceClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "id must be a valid numeric BigCommerce product ID"})
		return
	}

	if err := client.DeleteProduct(id); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "deleted_id": id})
}

// GetProducts returns products from BigCommerce (paginated).
// GET /bigcommerce/products?page=1&limit=50
func (h *BigCommerceHandler) GetProducts(c *gin.Context) {
	client, _, err := h.getBigCommerceClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error(), "products": []interface{}{}})
		return
	}

	page := 1
	limit := 50
	if p := c.Query("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v > 0 {
			page = v
		}
	}
	if l := c.Query("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 250 {
			limit = v
		}
	}

	result, err := client.GetProducts(page, limit)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error(), "products": []interface{}{}})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":          true,
		"products":    result.Data,
		"total_count": result.Meta.Pagination.Total,
		"page":        page,
		"limit":       limit,
		"total_pages": result.Meta.Pagination.TotalPages,
	})
}
