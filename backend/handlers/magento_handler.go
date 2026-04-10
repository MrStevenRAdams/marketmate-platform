package handlers

// ============================================================================
// MAGENTO 2 HANDLER
// ============================================================================
// Routes:
//   POST   /magento/connect          → test + save credentials
//   POST   /magento/test             → test connection with given credentials
//   GET    /magento/categories       → product category tree
//   POST   /magento/prepare          → prepare listing draft from MarketMate product
//   POST   /magento/submit           → create product on Magento
//   PUT    /magento/products/:sku    → update product
//   DELETE /magento/products/:sku    → delete product
//   GET    /magento/products         → list products (paginated)
// ============================================================================

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"module-a/marketplace/clients/magento"
	"module-a/models"
	"module-a/repository"
	"module-a/services"
)

type MagentoHandler struct {
	marketplaceService *services.MarketplaceService
	repo               *repository.MarketplaceRepository
	productRepo        *repository.FirestoreRepository
}

func NewMagentoHandler(
	marketplaceService *services.MarketplaceService,
	repo *repository.MarketplaceRepository,
	productRepo *repository.FirestoreRepository,
) *MagentoHandler {
	return &MagentoHandler{
		marketplaceService: marketplaceService,
		repo:               repo,
		productRepo:        productRepo,
	}
}

// ── Credential resolution ─────────────────────────────────────────────────────

func (h *MagentoHandler) getMagentoClient(c *gin.Context) (*magento.Client, string, error) {
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
			if cred.Channel == "magento" && cred.Active {
				credentialID = cred.CredentialID
				break
			}
		}
		if credentialID == "" {
			return nil, "", fmt.Errorf("no Magento credential found — please connect a Magento store first")
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
	integrationToken := merged["integration_token"]

	if storeURL == "" || integrationToken == "" {
		return nil, "", fmt.Errorf("incomplete Magento credentials: store_url and integration_token are required")
	}

	client := magento.NewClient(storeURL, integrationToken)
	return client, credentialID, nil
}

// ── Save Credential ───────────────────────────────────────────────────────────

// SaveCredential saves Magento credentials after verifying the connection.
// POST /magento/connect
func (h *MagentoHandler) SaveCredential(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req struct {
		AccountName      string `json:"account_name"`
		StoreURL         string `json:"store_url" binding:"required"`
		IntegrationToken string `json:"integration_token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	// Test connection before saving
	client := magento.NewClient(req.StoreURL, req.IntegrationToken)
	if err := client.TestConnection(); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": "Connection test failed: " + err.Error()})
		return
	}

	accountName := req.AccountName
	if accountName == "" {
		accountName = req.StoreURL
	}

	credData := map[string]string{
		"store_url":         req.StoreURL,
		"integration_token": req.IntegrationToken,
	}

	// Check for existing Magento credential matching this store URL
	existingCreds, _ := h.repo.ListCredentials(c.Request.Context(), tenantID)
	credentialID := ""
	for _, ec := range existingCreds {
		if ec.Channel == "magento" && ec.Active {
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
		credentialID = "cred-magento-" + fmt.Sprintf("%d", time.Now().UnixMilli())
		newCred := &models.MarketplaceCredential{
			CredentialID:   credentialID,
			TenantID:       tenantID,
			Channel:        "magento",
			AccountName:    accountName,
			Environment:    "production",
			Active:         true,
			CredentialData: credData,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}
		if err := h.repo.SaveCredential(c.Request.Context(), newCred); err != nil {
			log.Printf("[Magento] Failed to create credential: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "failed to save credential: " + err.Error()})
			return
		}
	}

	log.Printf("[Magento] Credential saved: %s for store %s", credentialID, req.StoreURL)
	c.JSON(http.StatusOK, gin.H{
		"ok":            true,
		"credential_id": credentialID,
		"message":       "Magento store connected successfully",
	})
}

// ── Test Connection ───────────────────────────────────────────────────────────

// TestConnection tests a Magento connection with provided credentials (before saving).
// POST /magento/test  { "store_url": "...", "integration_token": "..." }
func (h *MagentoHandler) TestConnection(c *gin.Context) {
	var req struct {
		StoreURL         string `json:"store_url" binding:"required"`
		IntegrationToken string `json:"integration_token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	client := magento.NewClient(req.StoreURL, req.IntegrationToken)
	if err := client.TestConnection(); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"message": "Magento connection successful",
	})
}

// ── Categories ────────────────────────────────────────────────────────────────

// GetCategories returns the Magento category tree (flattened).
// GET /magento/categories
func (h *MagentoHandler) GetCategories(c *gin.Context) {
	client, _, err := h.getMagentoClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error(), "categories": []interface{}{}})
		return
	}

	root, err := client.GetCategories()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error(), "categories": []interface{}{}})
		return
	}

	flat := magento.FlattenCategories(root)
	c.JSON(http.StatusOK, gin.H{"ok": true, "categories": flat})
}

// ── Prepare & Submit ──────────────────────────────────────────────────────────

// PrepareListingDraft loads MarketMate product data and builds a Magento draft.
// POST /magento/prepare  { "product_id": "...", "credential_id": "..." }
func (h *MagentoHandler) PrepareListingDraft(c *gin.Context) {
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

	// Build the draft object
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
		"sku":              sku,
		"name":             product.Title,
		"description":      product.Description,
		"price":            price,
		"stock_quantity":   qty,
		"weight":           weight,
		"images":           images,
		"status":           1,
		"visibility":       4,
		"type_id":          "simple",
		"attribute_set_id": 4,
		"variants":         loadChannelVariants(c.Request.Context(), h.productRepo, tenantID, req.ProductID, fmt.Sprintf("%.2f", price), ""),
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":         true,
		"product_id": req.ProductID,
		"draft":      draft,
	})
}

// SubmitListing creates a product on Magento.
// POST /magento/submit
//
// When the payload contains a "variants" array with ≥2 active entries, a
// configurable product is created: each active variant becomes a simple child
// product, then a configurable parent is created and linked to the children.
//
// When no variants are present, a single simple product is created (original
// behaviour).
func (h *MagentoHandler) SubmitListing(c *gin.Context) {
	client, _, err := h.getMagentoClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	var req struct {
		magento.Product
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
	if req.SKU == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "sku is required"})
		return
	}

	// Collect active variants
	activeVariants := make([]ChannelVariantDraft, 0)
	for _, v := range req.Variants {
		if v.Active {
			activeVariants = append(activeVariants, v)
		}
	}

	if len(activeVariants) >= 2 {
		// ── Configurable + simple children flow ──────────────────────────
		// Step 1: create a simple product per variant
		type childResult struct {
			SKU   string `json:"sku"`
			ID    int    `json:"id,omitempty"`
			Error string `json:"error,omitempty"`
		}
		submittedChildren := []childResult{}
		childSKUs := []string{}
		warnings := []string{}

		for _, v := range activeVariants {
			childPrice := req.Price
			if p, err := strconv.ParseFloat(v.Price, 64); err == nil && p > 0 {
				childPrice = p
			}
			child := &magento.Product{
				SKU:            v.SKU,
				Name:           req.Name + " - " + formatCombination(v.Combination),
				Price:          childPrice,
				Status:         req.Status,
				Visibility:     1, // not visible individually
				TypeID:         "simple",
				AttributeSetID: req.AttributeSetID,
				Weight:         req.Weight,
			}
			if child.AttributeSetID == 0 {
				child.AttributeSetID = 4
			}
			created, err := client.CreateProduct(child)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("variant SKU %s: %v", v.SKU, err))
				continue
			}
			submittedChildren = append(submittedChildren, childResult{SKU: created.SKU, ID: created.ID})
			childSKUs = append(childSKUs, created.SKU)
			log.Printf("[Magento Submit] Child product created: SKU=%s ID=%d", created.SKU, created.ID)
		}

		if len(childSKUs) == 0 {
			c.JSON(http.StatusOK, gin.H{
				"ok":       false,
				"error":    "All variant child products failed to create",
				"warnings": warnings,
			})
			return
		}

		// Step 2: create the configurable parent
		parent := &magento.Product{
			SKU:            req.SKU,
			Name:           req.Name,
			Price:          req.Price,
			Status:         req.Status,
			Visibility:     req.Visibility,
			TypeID:         "configurable",
			AttributeSetID: req.AttributeSetID,
			Weight:         req.Weight,
		}
		if parent.Visibility == 0 {
			parent.Visibility = 4
		}
		if parent.AttributeSetID == 0 {
			parent.AttributeSetID = 4
		}
		createdParent, err := client.CreateProduct(parent)
		if err != nil {
			// Orphan cleanup: delete all successfully created children so Magento
			// is not left with orphaned simple products that have no parent.
			cleanupResults := []string{}
			for _, child := range submittedChildren {
				if delErr := client.DeleteProduct(child.SKU); delErr != nil {
					cleanupResults = append(cleanupResults, fmt.Sprintf("failed to delete child SKU %s: %v", child.SKU, delErr))
					log.Printf("[Magento Cleanup] Failed to delete orphaned child SKU=%s: %v", child.SKU, delErr)
				} else {
					cleanupResults = append(cleanupResults, fmt.Sprintf("deleted orphaned child SKU %s", child.SKU))
					log.Printf("[Magento Cleanup] Deleted orphaned child SKU=%s", child.SKU)
				}
			}
			c.JSON(http.StatusOK, gin.H{
				"ok":              false,
				"error":           fmt.Sprintf("failed to create configurable parent: %v", err),
				"children_submitted": submittedChildren,
				"cleanup":         cleanupResults,
				"warnings":        warnings,
			})
			return
		}
		log.Printf("[Magento Submit] Configurable parent created: SKU=%s ID=%d", createdParent.SKU, createdParent.ID)

		c.JSON(http.StatusOK, gin.H{
			"ok":          true,
			"sku":         createdParent.SKU,
			"id":          createdParent.ID,
			"type":        "configurable",
			"children":    submittedChildren,
			"child_count": len(submittedChildren),
			"warnings":    warnings,
		})
		return
	}

	// Single simple product (original behaviour)
	created, err := client.CreateProduct(&req.Product)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":     true,
		"sku":    created.SKU,
		"id":     created.ID,
		"status": created.Status,
	})
}

// formatCombination renders a variant combination map as a human-readable string
// e.g. {"Color":"Red","Size":"M"} → "Color: Red, Size: M"
func formatCombination(combination map[string]string) string {
	parts := []string{}
	for k, v := range combination {
		parts = append(parts, k+": "+v)
	}
	if len(parts) == 0 {
		return "variant"
	}
	return strings.Join(parts, ", ")
}

// UpdateProductListing updates an existing Magento product.
// PUT /magento/products/:sku
func (h *MagentoHandler) UpdateProductListing(c *gin.Context) {
	client, _, err := h.getMagentoClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	sku := c.Param("sku")
	if sku == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "sku is required"})
		return
	}

	var payload magento.Product
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	updated, err := client.UpdateProduct(sku, &payload)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "sku": updated.SKU, "id": updated.ID, "status": updated.Status})
}

// DeleteProduct removes a product from Magento.
// DELETE /magento/products/:sku
func (h *MagentoHandler) DeleteProduct(c *gin.Context) {
	client, _, err := h.getMagentoClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	sku := c.Param("sku")
	if sku == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "sku is required"})
		return
	}

	if err := client.DeleteProduct(sku); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "deleted_sku": sku})
}

// GetProducts returns products from Magento (paginated).
// GET /magento/products?page=1&page_size=50
func (h *MagentoHandler) GetProducts(c *gin.Context) {
	client, _, err := h.getMagentoClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error(), "products": []interface{}{}})
		return
	}

	page := 1
	pageSize := 50
	if p := c.Query("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil {
			page = v
		}
	}
	if ps := c.Query("page_size"); ps != "" {
		if v, err := strconv.Atoi(ps); err == nil && v <= 200 {
			pageSize = v
		}
	}

	result, err := client.GetProducts(page, pageSize)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error(), "products": []interface{}{}})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":          true,
		"products":    result.Items,
		"total_count": result.TotalCount,
		"page":        page,
		"page_size":   pageSize,
	})
}
