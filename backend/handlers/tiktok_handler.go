package handlers

// ============================================================================
// TIKTOK SHOP HANDLER
// ============================================================================
// Routes:
//   GET  /tiktok/oauth/login        → generate consent URL
//   GET  /tiktok/oauth/callback     → exchange code, save tokens
//   GET  /tiktok/shops              → list authorized shops
//   GET  /tiktok/categories         → full category tree
//   GET  /tiktok/categories/:id/attributes → attributes for leaf category
//   GET  /tiktok/brands             → all authorized brands
//   GET  /tiktok/shipping-templates → shipping templates
//   GET  /tiktok/warehouses         → fulfillment warehouses
//   POST /tiktok/images/upload      → upload image from URL to TikTok CDN
//   POST /tiktok/prepare            → prepare listing draft from MarketMate product
//   POST /tiktok/submit             → create/update product on TikTok Shop
//   GET  /tiktok/products           → list products
//   DELETE /tiktok/products/:id     → delete product
// ============================================================================

import (
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"module-a/marketplace/clients/tiktok"
	"module-a/models"
	"module-a/repository"
	"module-a/services"
)

type TikTokHandler struct {
	marketplaceService *services.MarketplaceService
	repo               *repository.MarketplaceRepository
	productRepo        *repository.FirestoreRepository
}

func NewTikTokHandler(
	marketplaceService *services.MarketplaceService,
	repo *repository.MarketplaceRepository,
	productRepo *repository.FirestoreRepository,
) *TikTokHandler {
	return &TikTokHandler{
		marketplaceService: marketplaceService,
		repo:               repo,
		productRepo:        productRepo,
	}
}

// ── Credential resolution ─────────────────────────────────────────────────────

func (h *TikTokHandler) getTikTokClient(c *gin.Context) (*tiktok.Client, string, error) {
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
			if cred.Channel == "tiktok" && cred.Active {
				credentialID = cred.CredentialID
				break
			}
		}
		if credentialID == "" {
			return nil, "", fmt.Errorf("no TikTok Shop credential found — please connect a TikTok Shop account first")
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

	appKey := merged["app_key"]
	appSecret := merged["app_secret"]
	accessToken := merged["access_token"]
	shopID := merged["shop_id"]

	if appKey == "" || appSecret == "" {
		return nil, "", fmt.Errorf("incomplete TikTok credentials: app_key and app_secret required")
	}
	if accessToken == "" {
		return nil, "", fmt.Errorf("TikTok access_token missing — complete OAuth flow first")
	}

	client := tiktok.NewClient(appKey, appSecret, accessToken, shopID)

	// Set up token refresh callback to persist back to Firestore
	client.OnTokenRefresh = func(newAccess, newRefresh string, expiresIn int) {
		existingCred, err := h.repo.GetCredential(c.Request.Context(), tenantID, credentialID)
		if err != nil {
			log.Printf("[TikTok] WARNING: failed to load credential for token update: %v", err)
			return
		}
		existingCred.CredentialData["access_token"] = newAccess
		if newRefresh != "" {
			existingCred.CredentialData["refresh_token"] = newRefresh
		}
		existingCred.CredentialData["token_expires_at"] = time.Now().Add(time.Duration(expiresIn) * time.Second).Format(time.RFC3339)
		if err := h.repo.SaveCredential(c.Request.Context(), existingCred); err != nil {
			log.Printf("[TikTok] WARNING: failed to persist refreshed tokens: %v", err)
		} else {
			log.Printf("[TikTok] Token refreshed and persisted for credential %s", credentialID)
		}
	}

	return client, credentialID, nil
}

// ── OAuth ─────────────────────────────────────────────────────────────────────

// OAuthLogin returns the TikTok consent URL for the seller to authorize.
// GET /tiktok/oauth/login?account_name=MyShop
func (h *TikTokHandler) OAuthLogin(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	accountName := c.Query("account_name")
	if accountName == "" {
		accountName = "TikTok Shop"
	}

	appKey := os.Getenv("TIKTOK_APP_KEY")
	if appKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "TIKTOK_APP_KEY not configured on server"})
		return
	}

	redirectURI := os.Getenv("TIKTOK_REDIRECT_URI")
	if redirectURI == "" {
		// Build from request host
		scheme := "https"
		if c.Request.TLS == nil && c.Request.Host == "localhost:8080" {
			scheme = "http"
		}
		redirectURI = scheme + "://" + c.Request.Host + "/api/v1/tiktok/oauth/callback"
	}

	// State encodes tenantID|accountName
	stateRaw := tenantID + "|" + accountName
	state := base64.URLEncoding.EncodeToString([]byte(stateRaw))

	appSecret := os.Getenv("TIKTOK_APP_SECRET")
	client := tiktok.NewClient(appKey, appSecret, "", "")
	consentURL := client.GenerateOAuthURL(redirectURI, state)

	c.JSON(http.StatusOK, gin.H{
		"ok":          true,
		"consent_url": consentURL,
		"message":     "Redirect the user to consent_url to authorize TikTok Shop access",
	})
}

// OAuthCallback handles the redirect from TikTok after user authorization.
// GET /tiktok/oauth/callback?code=...&state=...
func (h *TikTokHandler) OAuthCallback(c *gin.Context) {
	code := c.Query("code")
	state := c.Query("state")
	errParam := c.Query("error")

	errorHTML := func(msg string) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(`<html><body style="font-family:sans-serif;text-align:center;padding:60px">
			<h2 style="color:#E53238">❌ TikTok Authorization Failed</h2>
			<p>`+msg+`</p>
			<script>
				if (window.opener) {
					window.opener.postMessage({type:'tiktok-oauth-error',error:'`+msg+`'}, '*');
					setTimeout(() => window.close(), 3000);
				}
			</script>
		</body></html>`))
	}

	if errParam != "" || code == "" {
		errorHTML("Authorization was denied or the code is missing.")
		return
	}

	// Decode state
	stateBytes, _ := base64.URLEncoding.DecodeString(state)
	parts := strings.SplitN(string(stateBytes), "|", 2)
	tenantID := "tenant-demo"
	accountName := "TikTok Shop"
	if len(parts) >= 1 && parts[0] != "" {
		tenantID = parts[0]
	}
	if len(parts) >= 2 && parts[1] != "" {
		accountName = parts[1]
	}

	appKey := os.Getenv("TIKTOK_APP_KEY")
	appSecret := os.Getenv("TIKTOK_APP_SECRET")
	if appKey == "" || appSecret == "" {
		errorHTML("Server configuration error: TIKTOK_APP_KEY or TIKTOK_APP_SECRET not set.")
		return
	}

	client := tiktok.NewClient(appKey, appSecret, "", "")

	// Exchange code for tokens
	tokens, err := client.ExchangeCodeForToken(code)
	if err != nil {
		log.Printf("[TikTok OAuth] Token exchange failed: %v", err)
		errorHTML("Token exchange failed: " + err.Error())
		return
	}

	log.Printf("[TikTok OAuth] Tokens received for seller: %s (region: %s)", tokens.SellerName, tokens.SellerBaseRegion)

	// Get shop list to pick the primary shop
	client.AccessToken = tokens.AccessToken
	shops, err := client.GetAuthorizedShops()
	shopID := ""
	shopName := tokens.SellerName
	if err == nil && len(shops) > 0 {
		shopID = shops[0].ID
		if shops[0].Name != "" {
			shopName = shops[0].Name
		}
	}

	// Upsert credential
	credData := map[string]string{
		"app_key":                appKey,
		"access_token":           tokens.AccessToken,
		"refresh_token":          tokens.RefreshToken,
		"shop_id":                shopID,
		"seller_name":            shopName,
		"token_expires_at":       time.Now().Add(time.Duration(tokens.AccessTokenExpireIn) * time.Second).Format(time.RFC3339),
		"refresh_expires_at":     time.Now().Add(time.Duration(tokens.RefreshTokenExpireIn) * time.Second).Format(time.RFC3339),
		"seller_base_region":     tokens.SellerBaseRegion,
	}

	// Check for existing TikTok credential to update
	existingCreds, _ := h.repo.ListCredentials(c.Request.Context(), tenantID)
	credentialID := ""
	for _, ec := range existingCreds {
		if ec.Channel == "tiktok" && ec.Active {
			credentialID = ec.CredentialID
			break
		}
	}

	if credentialID != "" {
		// Update existing credential
		existingCred, err := h.repo.GetCredential(c.Request.Context(), tenantID, credentialID)
		if err != nil {
			errorHTML("Failed to load existing credential: " + err.Error())
			return
		}
		for k, v := range credData {
			existingCred.CredentialData[k] = v
		}
		existingCred.AccountName = accountName
		if err := h.repo.SaveCredential(c.Request.Context(), existingCred); err != nil {
			log.Printf("[TikTok OAuth] Failed to update credential: %v", err)
			errorHTML("Failed to save credentials: " + err.Error())
			return
		}
	} else {
		// Create new credential
		credentialID = "cred-tiktok-" + fmt.Sprintf("%d", time.Now().UnixMilli())
		newCred := &models.MarketplaceCredential{
			CredentialID:   credentialID,
			TenantID:       tenantID,
			Channel:        "tiktok",
			AccountName:    accountName,
			Environment:    "production",
			Active:         true,
			CredentialData: credData,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}
		if err := h.repo.SaveCredential(c.Request.Context(), newCred); err != nil {
			log.Printf("[TikTok OAuth] Failed to create credential: %v", err)
			errorHTML("Failed to save credentials: " + err.Error())
			return
		}
	}

	log.Printf("[TikTok OAuth] Credential saved: %s (shop: %s, id: %s)", credentialID, shopName, shopID)

	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(`<html><body style="font-family:sans-serif;text-align:center;padding:60px">
		<h2 style="color:#00f2ea">✅ TikTok Shop Connected!</h2>
		<p>Shop: <strong>`+shopName+`</strong></p>
		<p>This window will close automatically.</p>
		<script>
			if (window.opener) {
				window.opener.postMessage({type:'tiktok-oauth-success',shopName:'`+shopName+`',credentialId:'`+credentialID+`'}, '*');
				setTimeout(() => window.close(), 2000);
			}
		</script>
	</body></html>`))
}

// ── Shops ─────────────────────────────────────────────────────────────────────

// GetShops returns authorized shops for the connected account.
// GET /tiktok/shops
func (h *TikTokHandler) GetShops(c *gin.Context) {
	client, _, err := h.getTikTokClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	shops, err := client.GetAuthorizedShops()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error(), "shops": []interface{}{}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "shops": shops})
}

// ── Categories ────────────────────────────────────────────────────────────────

// GetCategories returns the full TikTok Shop category tree.
// GET /tiktok/categories
func (h *TikTokHandler) GetCategories(c *gin.Context) {
	client, _, err := h.getTikTokClient(c)
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

// GetCategoryAttributes returns required attributes for a leaf category.
// GET /tiktok/categories/:id/attributes
func (h *TikTokHandler) GetCategoryAttributes(c *gin.Context) {
	client, _, err := h.getTikTokClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	idStr := c.Param("id")
	var catID int64
	fmt.Sscanf(idStr, "%d", &catID)
	if catID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "invalid category id"})
		return
	}

	attrs, err := client.GetCategoryAttributes(catID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error(), "attributes": []interface{}{}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "attributes": attrs})
}

// ── Brands ────────────────────────────────────────────────────────────────────

// GetBrands returns all authorized brands for the seller.
// GET /tiktok/brands
func (h *TikTokHandler) GetBrands(c *gin.Context) {
	client, _, err := h.getTikTokClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	brands, err := client.GetAllBrands()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error(), "brands": []interface{}{}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "brands": brands})
}

// ── Shipping ──────────────────────────────────────────────────────────────────

// GetShippingTemplates returns the seller's shipping templates.
// GET /tiktok/shipping-templates
func (h *TikTokHandler) GetShippingTemplates(c *gin.Context) {
	client, _, err := h.getTikTokClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	templates, err := client.GetShippingTemplates()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error(), "templates": []interface{}{}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "templates": templates})
}

// GetShippingProviders returns available carrier/shipping providers.
// GET /tiktok/shipping-providers
func (h *TikTokHandler) GetShippingProviders(c *gin.Context) {
	client, _, err := h.getTikTokClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	providers, err := client.GetShippingProviders()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error(), "providers": []interface{}{}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "providers": providers})
}

// ── Warehouses ────────────────────────────────────────────────────────────────

// GetWarehouses returns the seller's fulfillment warehouses.
// GET /tiktok/warehouses
func (h *TikTokHandler) GetWarehouses(c *gin.Context) {
	client, _, err := h.getTikTokClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	warehouses, err := client.GetWarehouses()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error(), "warehouses": []interface{}{}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "warehouses": warehouses})
}

// ── Image Upload ──────────────────────────────────────────────────────────────

// UploadImage uploads a product image from a URL to TikTok's CDN.
// POST /tiktok/images/upload  { "url": "https://..." }
func (h *TikTokHandler) UploadImage(c *gin.Context) {
	client, _, err := h.getTikTokClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	var req struct {
		URL string `json:"url" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "url is required"})
		return
	}

	img, err := client.UploadImageFromURL(req.URL)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "image": img})
}

// ── Prepare & Submit ──────────────────────────────────────────────────────────

// PrepareListingDraft loads product data from MarketMate and builds a TikTok draft.
// POST /tiktok/prepare  { "product_id": "...", "credential_id": "..." }
func (h *TikTokHandler) PrepareListingDraft(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req struct {
		ProductID    string `json:"product_id" binding:"required"`
		CredentialID string `json:"credential_id"`
		CategoryID   int64  `json:"category_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	// Load core product data
	product, err := h.productRepo.GetProduct(c.Request.Context(), tenantID, req.ProductID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"ok": false, "error": "Product not found: " + err.Error()})
		return
	}

	// Extract images from product attributes or extended data
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

	// Get category attributes if category is known
	var attributes []tiktok.CategoryAttribute
	if req.CategoryID > 0 {
		client, _, err := h.getTikTokClient(c)
		if err == nil {
			attributes, _ = client.GetCategoryAttributes(req.CategoryID)
		}
	}

	draft := gin.H{
		"title":       product.Title,
		"description": product.Description,
		"brand":       product.Brand,
		"sku":         product.Attributes["source_sku"],
		"price":       product.Attributes["price"],
		"quantity":    product.Attributes["quantity"],
		"images":      images,
		"category_id": req.CategoryID,
		"attributes":  attributes,
		"weight_kg":   product.Attributes["weight_kg"],
		"length_cm":   product.Attributes["length_cm"],
		"width_cm":    product.Attributes["width_cm"],
		"height_cm":   product.Attributes["height_cm"],
		"variants":    loadChannelVariants(c.Request.Context(), h.productRepo, tenantID, req.ProductID, fmt.Sprintf("%v", product.Attributes["price"]), ""),
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":         true,
		"product_id": req.ProductID,
		"draft":      draft,
	})
}

// SubmitListing creates or updates a product on TikTok Shop from a reviewed draft.
// POST /tiktok/submit
//
// When draft.variants contains ≥2 active entries, the SKUs array in the
// CreateProductRequest is built from those variants. The TikTok API natively
// supports multiple SKUs per product (sales_attributes for Size, Color etc.)
func (h *TikTokHandler) SubmitListing(c *gin.Context) {
	client, _, err := h.getTikTokClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	var req struct {
		tiktok.CreateProductRequest
		Variants []ChannelVariantDraft `json:"variants"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "invalid payload: " + err.Error()})
		return
	}

	// Validate required fields
	if req.Title == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "title is required"})
		return
	}
	if req.CategoryID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "category_id is required"})
		return
	}
	if len(req.MainImages) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "at least one main image is required"})
		return
	}

	// Build SKUs from active variants (VAR-01)
	activeVariants := make([]ChannelVariantDraft, 0)
	for _, v := range req.Variants {
		if v.Active {
			activeVariants = append(activeVariants, v)
		}
	}

	if len(activeVariants) >= 2 && len(req.SKUs) == 0 {
		warehouseID := ""
		if len(req.SKUs) > 0 && len(req.SKUs[0].Inventory) > 0 {
			warehouseID = req.SKUs[0].Inventory[0].WarehouseID
		}
		builtSKUs := make([]tiktok.ProductSKU, 0, len(activeVariants))
		for _, v := range activeVariants {
			qty := 0
			if q, err := strconv.Atoi(v.Stock); err == nil {
				qty = q
			}
			sku := tiktok.ProductSKU{
				OuterID: v.SKU,
				Price: struct {
					Currency      string `json:"currency"`
					OriginalPrice string `json:"original_price"`
				}{
					Currency:      "GBP",
					OriginalPrice: v.Price,
				},
				Inventory: []struct {
					Quantity    int    `json:"quantity"`
					WarehouseID string `json:"warehouse_id"`
				}{
					{Quantity: qty, WarehouseID: warehouseID},
				},
			}
			builtSKUs = append(builtSKUs, sku)
		}
		req.CreateProductRequest.SKUs = builtSKUs
	}

	if len(req.SKUs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "at least one SKU is required"})
		return
	}

	result, err := client.CreateProduct(&req.CreateProductRequest)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":         true,
		"product_id": result.ProductID,
		"sku_list":   result.SkuList,
	})
}

// UpdateProductListing updates an existing TikTok product.
// PUT /tiktok/products/:id
func (h *TikTokHandler) UpdateProductListing(c *gin.Context) {
	client, _, err := h.getTikTokClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	productID := c.Param("id")
	if productID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "product id required"})
		return
	}

	var payload tiktok.CreateProductRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	result, err := client.UpdateProduct(productID, &payload)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "product_id": result.ProductID})
}

// ── Product listing management ────────────────────────────────────────────────

// GetProducts returns paginated products from TikTok Shop.
// GET /tiktok/products?page_token=...&page_size=50
func (h *TikTokHandler) GetProducts(c *gin.Context) {
	client, _, err := h.getTikTokClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	pageToken := c.Query("page_token")
	pageSize := 50
	if s := c.Query("page_size"); s != "" {
		fmt.Sscanf(s, "%d", &pageSize)
	}

	products, nextToken, total, err := client.GetProducts(pageToken, pageSize)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error(), "products": []interface{}{}})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":              true,
		"products":        products,
		"next_page_token": nextToken,
		"total":           total,
	})
}

// DeleteProduct removes a product from TikTok Shop.
// DELETE /tiktok/products/:id
func (h *TikTokHandler) DeleteProduct(c *gin.Context) {
	client, _, err := h.getTikTokClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	productID := c.Param("id")
	if err := client.DeleteProduct([]string{productID}); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "deleted": productID})
}
