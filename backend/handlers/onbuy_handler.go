package handlers

// ============================================================================
// ONBUY HANDLER
// ============================================================================
// Routes:
//   POST   /onbuy/connect          → test + save credentials
//   POST   /onbuy/test             → test connection with given credentials
//   GET    /onbuy/categories       → OnBuy category list
//   GET    /onbuy/conditions       → OnBuy conditions list
//   POST   /onbuy/prepare          → prepare listing draft from MarketMate product
//   POST   /onbuy/submit           → create listing on OnBuy
//   PUT    /onbuy/listings/:id     → update listing
//   DELETE /onbuy/listings/:id     → delete listing
//   GET    /onbuy/listings         → list listings (paginated)
// ============================================================================

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"module-a/marketplace/clients/onbuy"
	"module-a/models"
	"module-a/repository"
	"module-a/services"
)

type OnBuyHandler struct {
	marketplaceService *services.MarketplaceService
	repo               *repository.MarketplaceRepository
	productRepo        *repository.FirestoreRepository
}

func NewOnBuyHandler(
	marketplaceService *services.MarketplaceService,
	repo *repository.MarketplaceRepository,
	productRepo *repository.FirestoreRepository,
) *OnBuyHandler {
	return &OnBuyHandler{
		marketplaceService: marketplaceService,
		repo:               repo,
		productRepo:        productRepo,
	}
}

// ── Credential resolution ─────────────────────────────────────────────────────

func (h *OnBuyHandler) getOnBuyClient(c *gin.Context) (*onbuy.Client, string, error) {
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
			if cred.Channel == "onbuy" && cred.Active {
				credentialID = cred.CredentialID
				break
			}
		}
		if credentialID == "" {
			return nil, "", fmt.Errorf("no OnBuy credential found — please connect an OnBuy account first")
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

	consumerKey := merged["consumer_key"]
	consumerSecret := merged["consumer_secret"]
	siteIDStr := merged["site_id"]

	if consumerKey == "" || consumerSecret == "" {
		return nil, "", fmt.Errorf("incomplete OnBuy credentials: consumer_key and consumer_secret are required")
	}

	siteID := 2000
	if siteIDStr != "" {
		if v, err := strconv.Atoi(siteIDStr); err == nil && v > 0 {
			siteID = v
		}
	}

	client := onbuy.NewClient(consumerKey, consumerSecret, siteID)
	return client, credentialID, nil
}

// ── Save Credential ───────────────────────────────────────────────────────────

// SaveCredential saves OnBuy credentials after verifying the connection.
// POST /onbuy/connect
func (h *OnBuyHandler) SaveCredential(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req struct {
		AccountName    string `json:"account_name"`
		ConsumerKey    string `json:"consumer_key" binding:"required"`
		ConsumerSecret string `json:"consumer_secret" binding:"required"`
		SiteID         string `json:"site_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	siteID := 2000
	if req.SiteID != "" {
		if v, err := strconv.Atoi(req.SiteID); err == nil && v > 0 {
			siteID = v
		}
	}

	// Test connection before saving
	client := onbuy.NewClient(req.ConsumerKey, req.ConsumerSecret, siteID)
	if err := client.TestConnection(); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": "Connection test failed: " + err.Error()})
		return
	}

	accountName := req.AccountName
	if accountName == "" {
		accountName = "OnBuy Account"
	}

	siteIDString := strconv.Itoa(siteID)

	credData := map[string]string{
		"consumer_key":    req.ConsumerKey,
		"consumer_secret": req.ConsumerSecret,
		"site_id":         siteIDString,
	}

	// Check for existing OnBuy credential for this consumer_key
	existingCreds, _ := h.repo.ListCredentials(c.Request.Context(), tenantID)
	credentialID := ""
	for _, ec := range existingCreds {
		if ec.Channel == "onbuy" && ec.Active {
			if cd, ok := ec.CredentialData["consumer_key"]; ok && cd == req.ConsumerKey {
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
		credentialID = "cred-onbuy-" + fmt.Sprintf("%d", time.Now().UnixMilli())
		newCred := &models.MarketplaceCredential{
			CredentialID:   credentialID,
			TenantID:       tenantID,
			Channel:        "onbuy",
			AccountName:    accountName,
			Environment:    "production",
			Active:         true,
			CredentialData: credData,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}
		if err := h.repo.SaveCredential(c.Request.Context(), newCred); err != nil {
			log.Printf("[OnBuy] Failed to create credential: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "failed to save credential: " + err.Error()})
			return
		}
	}

	log.Printf("[OnBuy] Credential saved: %s", credentialID)
	c.JSON(http.StatusOK, gin.H{
		"ok":            true,
		"credential_id": credentialID,
		"message":       "OnBuy account connected successfully",
	})
}

// ── Test Connection ───────────────────────────────────────────────────────────

// TestConnection tests an OnBuy connection with provided credentials (before saving).
// POST /onbuy/test  { "consumer_key": "...", "consumer_secret": "...", "site_id": "2000" }
func (h *OnBuyHandler) TestConnection(c *gin.Context) {
	var req struct {
		ConsumerKey    string `json:"consumer_key" binding:"required"`
		ConsumerSecret string `json:"consumer_secret" binding:"required"`
		SiteID         string `json:"site_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	siteID := 2000
	if req.SiteID != "" {
		if v, err := strconv.Atoi(req.SiteID); err == nil && v > 0 {
			siteID = v
		}
	}

	client := onbuy.NewClient(req.ConsumerKey, req.ConsumerSecret, siteID)
	if err := client.TestConnection(); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"message": "OnBuy connection successful",
	})
}

// ── Categories ────────────────────────────────────────────────────────────────

// GetCategories returns the OnBuy category list.
// GET /onbuy/categories
func (h *OnBuyHandler) GetCategories(c *gin.Context) {
	client, _, err := h.getOnBuyClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error(), "categories": []interface{}{}})
		return
	}

	cats, err := client.GetCategories(client.SiteID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error(), "categories": []interface{}{}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "categories": cats})
}

// GetConditions returns the OnBuy conditions list.
// GET /onbuy/conditions
func (h *OnBuyHandler) GetConditions(c *gin.Context) {
	client, _, err := h.getOnBuyClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error(), "conditions": []interface{}{}})
		return
	}

	conds, err := client.GetConditions()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error(), "conditions": []interface{}{}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "conditions": conds})
}

// ── Prepare & Submit ──────────────────────────────────────────────────────────

// PrepareListingDraft loads MarketMate product data and builds an OnBuy draft.
// POST /onbuy/prepare  { "product_id": "...", "credential_id": "..." }
func (h *OnBuyHandler) PrepareListingDraft(c *gin.Context) {
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

	draft := gin.H{
		"opc":                   "", // User must supply the OnBuy Product Code
		"sku":                   sku,
		"description":           product.Description,
		"price":                 price,
		"stock":                 qty,
		"condition_id":          "new",
		"delivery_template_id":  0,
		"site_id":               2000,
		"variants":              loadChannelVariants(c.Request.Context(), h.productRepo, tenantID, req.ProductID, fmt.Sprintf("%.2f", price), ""),
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":         true,
		"product_id": req.ProductID,
		"title":      product.Title,
		"draft":      draft,
	})
}

// SubmitListing creates one or more listings on OnBuy.
// POST /onbuy/submit
//
// When the payload contains a "variants" array with ≥2 active entries, one
// OnBuy listing is created per active variant. The OPC (OnBuy Product Code)
// is shared across all variants.
//
// ⚠️ OnBuy does not support variation listings. Each variant is submitted as
// an individual product listing. A notice is shown in the frontend.
func (h *OnBuyHandler) SubmitListing(c *gin.Context) {
	client, _, err := h.getOnBuyClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	var req struct {
		onbuy.Listing
		Variants []ChannelVariantDraft `json:"variants"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "invalid payload: " + err.Error()})
		return
	}

	if req.OPC == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "opc (OnBuy Product Code) is required"})
		return
	}
	if req.SiteID == 0 {
		req.SiteID = client.SiteID
	}

	// Collect active variants
	activeVariants := make([]ChannelVariantDraft, 0)
	for _, v := range req.Variants {
		if v.Active {
			activeVariants = append(activeVariants, v)
		}
	}

	// Multi-listing path
	if len(activeVariants) >= 2 {
		type listingResult struct {
			SKU       string `json:"sku"`
			ListingID string `json:"listing_id,omitempty"`
			Error     string `json:"error,omitempty"`
		}
		submitted := []listingResult{}
		errorsList := []listingResult{}

		for _, v := range activeVariants {
			price := req.Price
			if p, err := strconv.ParseFloat(v.Price, 64); err == nil && p > 0 {
				price = p
			}
			stock := req.Stock
			if s, err := strconv.Atoi(v.Stock); err == nil && s >= 0 {
				stock = s
			}
			listing := &onbuy.Listing{
				OPC:                req.OPC,
				SiteID:             req.SiteID,
				ConditionID:        req.ConditionID,
				Price:              price,
				Stock:              stock,
				SKU:                v.SKU,
				Description:        req.Description,
				DeliveryTemplateID: req.DeliveryTemplateID,
			}
			result, err := client.CreateListing(listing)
			if err != nil {
				errorsList = append(errorsList, listingResult{SKU: v.SKU, Error: err.Error()})
				continue
			}
			submitted = append(submitted, listingResult{SKU: v.SKU, ListingID: result.ListingID})
			log.Printf("[OnBuy Submit] Listing created for variant SKU=%s listingID=%s", v.SKU, result.ListingID)
		}

		c.JSON(http.StatusOK, gin.H{
			"ok":        len(submitted) > 0,
			"submitted": submitted,
			"errors":    errorsList,
			"message":   fmt.Sprintf("%d/%d variants submitted as individual listings", len(submitted), len(activeVariants)),
		})
		return
	}

	// Single listing path (original)
	if req.Price <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "price must be greater than 0"})
		return
	}
	result, err := client.CreateListing(&req.Listing)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":         true,
		"listing_id": result.ListingID,
		"message":    "Listing created successfully on OnBuy",
	})
}

// UpdateListing updates an existing OnBuy listing.
// PUT /onbuy/listings/:id
func (h *OnBuyHandler) UpdateListing(c *gin.Context) {
	client, _, err := h.getOnBuyClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	listingID := c.Param("id")
	if listingID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "listing id is required"})
		return
	}

	var payload map[string]interface{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	if err := client.UpdateListing(listingID, payload); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "listing_id": listingID})
}

// DeleteListing removes a listing from OnBuy.
// DELETE /onbuy/listings/:id
func (h *OnBuyHandler) DeleteListing(c *gin.Context) {
	client, _, err := h.getOnBuyClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	listingID := c.Param("id")
	if listingID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "listing id is required"})
		return
	}

	if err := client.DeleteListing(listingID); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "deleted_id": listingID})
}

// GetListings returns listings from OnBuy (paginated).
// GET /onbuy/listings?page=1
func (h *OnBuyHandler) GetListings(c *gin.Context) {
	client, _, err := h.getOnBuyClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error(), "listings": []interface{}{}})
		return
	}

	page := 1
	if p := c.Query("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v > 0 {
			page = v
		}
	}

	listings, err := client.GetProducts(page)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error(), "listings": []interface{}{}})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":       true,
		"listings": listings,
		"page":     page,
	})
}
