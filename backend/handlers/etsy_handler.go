package handlers

// ============================================================================
// ETSY HANDLER
// ============================================================================
// Routes:
//   GET  /etsy/oauth/login               → generate PKCE consent URL
//   GET  /etsy/oauth/callback            → exchange code, save tokens
//   GET  /etsy/shop                      → shop info
//   GET  /etsy/taxonomy                  → full taxonomy tree
//   GET  /etsy/taxonomy/:id/properties   → taxonomy node properties
//   GET  /etsy/shipping-profiles         → shipping profiles
//   POST /etsy/images/upload             → proxy-upload image (base64)
//   POST /etsy/prepare                   → build listing draft from MarketMate product
//   POST /etsy/submit                    → create listing on Etsy
//   PUT  /etsy/listings/:id              → update listing
//   DELETE /etsy/listings/:id            → delete listing
//   GET  /etsy/listings                  → list all shop listings
// ============================================================================

import (
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"module-a/marketplace/clients/etsy"
	"module-a/models"
	"module-a/repository"
	"module-a/services"
)

type EtsyHandler struct {
	marketplaceService *services.MarketplaceService
	repo               *repository.MarketplaceRepository
	productRepo        *repository.FirestoreRepository
}

func NewEtsyHandler(
	marketplaceService *services.MarketplaceService,
	repo *repository.MarketplaceRepository,
	productRepo *repository.FirestoreRepository,
) *EtsyHandler {
	return &EtsyHandler{
		marketplaceService: marketplaceService,
		repo:               repo,
		productRepo:        productRepo,
	}
}

// ── Credential resolution ─────────────────────────────────────────────────────

func (h *EtsyHandler) getEtsyClient(c *gin.Context) (*etsy.Client, string, error) {
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
			if cred.Channel == "etsy" && cred.Active {
				credentialID = cred.CredentialID
				break
			}
		}
		if credentialID == "" {
			return nil, "", fmt.Errorf("no Etsy credential found — please connect your Etsy account first")
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

	apiKey := merged["client_id"]
	accessToken := merged["access_token"]
	refreshToken := merged["refresh_token"]
	shopIDStr := merged["shop_id"]

	if apiKey == "" {
		return nil, "", fmt.Errorf("client_id missing from Etsy credentials")
	}
	if accessToken == "" {
		return nil, "", fmt.Errorf("Etsy access_token missing — complete OAuth flow first")
	}

	var shopID int64
	if shopIDStr != "" {
		shopID, _ = strconv.ParseInt(shopIDStr, 10, 64)
	}

	client := etsy.NewClient(apiKey, accessToken, refreshToken, shopID)

	// Persist refreshed tokens back to Firestore automatically
	client.OnTokenRefresh = func(newAccess, newRefresh string, expiresIn int) {
		existingCred, err := h.repo.GetCredential(c.Request.Context(), tenantID, credentialID)
		if err != nil {
			log.Printf("[Etsy] WARNING: failed to load credential for token update: %v", err)
			return
		}
		existingCred.CredentialData["access_token"] = newAccess
		if newRefresh != "" {
			existingCred.CredentialData["refresh_token"] = newRefresh
		}
		existingCred.CredentialData["token_expires_at"] = time.Now().Add(time.Duration(expiresIn) * time.Second).Format(time.RFC3339)
		if err := h.repo.SaveCredential(c.Request.Context(), existingCred); err != nil {
			log.Printf("[Etsy] WARNING: failed to persist refreshed tokens: %v", err)
		} else {
			log.Printf("[Etsy] Token refreshed and persisted for credential %s", credentialID)
		}
	}

	return client, credentialID, nil
}

// ── OAuth ─────────────────────────────────────────────────────────────────────

// OAuthLogin generates the Etsy PKCE consent URL and stores the code_verifier.
// GET /etsy/oauth/login?account_name=MyShop
func (h *EtsyHandler) OAuthLogin(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	accountName := c.Query("account_name")
	if accountName == "" {
		accountName = "Etsy Shop"
	}

	apiKey := os.Getenv("ETSY_API_KEY")
	if apiKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "ETSY_API_KEY not configured on server"})
		return
	}

	redirectURI := os.Getenv("ETSY_REDIRECT_URI")
	if redirectURI == "" {
		scheme := "https"
		if c.Request.TLS == nil && strings.Contains(c.Request.Host, "localhost") {
			scheme = "http"
		}
		redirectURI = scheme + "://" + c.Request.Host + "/api/v1/etsy/oauth/callback"
	}

	// Generate PKCE pair
	codeVerifier, err := etsy.GenerateCodeVerifier()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "failed to generate code verifier"})
		return
	}

	// State encodes tenantID|accountName
	stateRaw := tenantID + "|" + accountName
	state := base64.URLEncoding.EncodeToString([]byte(stateRaw))

	// Persist verifier temporarily in a Firestore credential record so the callback can retrieve it
	// We store it in a "pending" credential that gets upgraded on callback success.
	verifierCredID := "pending-etsy-" + fmt.Sprintf("%d", time.Now().UnixMilli())
	pendingCred := &models.MarketplaceCredential{
		CredentialID: verifierCredID,
		TenantID:     tenantID,
		Channel:      "etsy-pending",
		AccountName:  accountName,
		Environment:  "production",
		Active:       false,
		CredentialData: map[string]string{
			"client_id":     apiKey,
			"code_verifier": codeVerifier,
			"redirect_uri":  redirectURI,
			"state":         state,
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := h.repo.SaveCredential(c.Request.Context(), pendingCred); err != nil {
		log.Printf("[Etsy OAuth] WARNING: could not persist pending verifier: %v", err)
	}

	client := etsy.NewClient(apiKey, "", "", 0)
	consentURL := client.GenerateOAuthURL(redirectURI, state, codeVerifier)

	c.JSON(http.StatusOK, gin.H{
		"ok":          true,
		"consent_url": consentURL,
		"state":       state,
		"message":     "Redirect the user to consent_url to authorise Etsy access",
	})
}

// OAuthCallback handles the Etsy redirect after user authorisation.
// GET /etsy/oauth/callback?code=...&state=...
func (h *EtsyHandler) OAuthCallback(c *gin.Context) {
	code := c.Query("code")
	state := c.Query("state")
	errParam := c.Query("error")

	errorHTML := func(msg string) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(`<html><body style="font-family:sans-serif;text-align:center;padding:60px">
			<h2 style="color:#E53238">❌ Etsy Authorization Failed</h2>
			<p>`+msg+`</p>
			<script>
				if (window.opener) {
					window.opener.postMessage({type:'etsy-oauth-error',error:'`+msg+`'}, '*');
					setTimeout(() => window.close(), 3000);
				}
			</script>
		</body></html>`))
	}

	if errParam != "" || code == "" {
		errorHTML("Authorization was denied or the code is missing.")
		return
	}

	// Decode state → tenantID
	stateBytes, _ := base64.URLEncoding.DecodeString(state)
	parts := strings.SplitN(string(stateBytes), "|", 2)
	tenantID := "tenant-demo"
	accountName := "Etsy Shop"
	if len(parts) >= 1 && parts[0] != "" {
		tenantID = parts[0]
	}
	if len(parts) >= 2 && parts[1] != "" {
		accountName = parts[1]
	}

	// Retrieve the code_verifier from the pending credential
	var codeVerifier, redirectURI string
	var apiKey string

	// Look for a pending etsy credential that matches the state
	allCreds, _ := h.repo.ListCredentials(c.Request.Context(), tenantID)
	var pendingCredID string
	for _, cred := range allCreds {
		if cred.Channel == "etsy-pending" && !cred.Active {
			if cred.CredentialData["state"] == state {
				codeVerifier = cred.CredentialData["code_verifier"]
				redirectURI = cred.CredentialData["redirect_uri"]
				apiKey = cred.CredentialData["client_id"]
				pendingCredID = cred.CredentialID
				break
			}
		}
	}

	// Fallback: use env vars
	if apiKey == "" {
		apiKey = os.Getenv("ETSY_API_KEY")
	}
	if redirectURI == "" {
		redirectURI = os.Getenv("ETSY_REDIRECT_URI")
	}
	if redirectURI == "" {
		scheme := "https"
		if c.Request.TLS == nil && strings.Contains(c.Request.Host, "localhost") {
			scheme = "http"
		}
		redirectURI = scheme + "://" + c.Request.Host + "/api/v1/etsy/oauth/callback"
	}

	if apiKey == "" {
		errorHTML("Server configuration error: ETSY_API_KEY not set.")
		return
	}
	if codeVerifier == "" {
		errorHTML("OAuth session expired or state mismatch — please try connecting again.")
		return
	}

	client := etsy.NewClient(apiKey, "", "", 0)

	// Exchange code for tokens
	tokens, err := client.ExchangeCodeForToken(code, codeVerifier, redirectURI)
	if err != nil {
		log.Printf("[Etsy OAuth] Token exchange failed: %v", err)
		errorHTML("Token exchange failed: " + err.Error())
		return
	}
	log.Printf("[Etsy OAuth] Tokens received for tenant %s", tenantID)

	// Get shop info
	client.AccessToken = tokens.AccessToken
	shop, err := client.GetShop()
	shopID := int64(0)
	shopName := accountName
	if err == nil && shop != nil {
		shopID = shop.ShopID
		if shop.ShopName != "" {
			shopName = shop.ShopName
		}
	} else {
		log.Printf("[Etsy OAuth] Could not retrieve shop (will retry later): %v", err)
	}

	credData := map[string]string{
		"client_id":        apiKey,
		"access_token":     tokens.AccessToken,
		"refresh_token":    tokens.RefreshToken,
		"shop_id":          strconv.FormatInt(shopID, 10),
		"shop_name":        shopName,
		"token_expires_at": time.Now().Add(time.Duration(tokens.ExpiresIn) * time.Second).Format(time.RFC3339),
	}

	// Check for existing active Etsy credential
	credentialID := ""
	for _, cred := range allCreds {
		if cred.Channel == "etsy" && cred.Active {
			credentialID = cred.CredentialID
			break
		}
	}

	if credentialID != "" {
		existingCred, err := h.repo.GetCredential(c.Request.Context(), tenantID, credentialID)
		if err != nil {
			errorHTML("Failed to load existing credential: " + err.Error())
			return
		}
		for k, v := range credData {
			existingCred.CredentialData[k] = v
		}
		existingCred.AccountName = shopName
		existingCred.UpdatedAt = time.Now()
		if err := h.repo.SaveCredential(c.Request.Context(), existingCred); err != nil {
			log.Printf("[Etsy OAuth] Failed to update credential: %v", err)
			errorHTML("Failed to save credentials: " + err.Error())
			return
		}
	} else {
		credentialID = "cred-etsy-" + fmt.Sprintf("%d", time.Now().UnixMilli())
		newCred := &models.MarketplaceCredential{
			CredentialID:   credentialID,
			TenantID:       tenantID,
			Channel:        "etsy",
			AccountName:    shopName,
			Environment:    "production",
			Active:         true,
			CredentialData: credData,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}
		if err := h.repo.SaveCredential(c.Request.Context(), newCred); err != nil {
			log.Printf("[Etsy OAuth] Failed to create credential: %v", err)
			errorHTML("Failed to save credentials: " + err.Error())
			return
		}
	}

	// Clean up pending credential
	if pendingCredID != "" {
		_ = h.repo.DeleteCredential(c.Request.Context(), tenantID, pendingCredID)
	}

	log.Printf("[Etsy OAuth] Credential saved: %s (shop: %s, shop_id: %d)", credentialID, shopName, shopID)

	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(`<html><body style="font-family:sans-serif;text-align:center;padding:60px">
		<h2 style="color:#F1641E">✅ Etsy Connected!</h2>
		<p>Shop: <strong>`+shopName+`</strong></p>
		<p>This window will close automatically.</p>
		<script>
			if (window.opener) {
				window.opener.postMessage({type:'etsy-oauth-success',shopName:'`+shopName+`',credentialId:'`+credentialID+`'}, '*');
				setTimeout(() => window.close(), 2000);
			}
		</script>
	</body></html>`))
}

// ── Shop ──────────────────────────────────────────────────────────────────────

// GetShop returns info about the connected Etsy shop.
// GET /etsy/shop
func (h *EtsyHandler) GetShop(c *gin.Context) {
	client, _, err := h.getEtsyClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}
	shop, err := client.GetShopByID(client.ShopID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "shop": shop})
}

// ── Taxonomy (categories) ─────────────────────────────────────────────────────

// GetTaxonomy returns the full Etsy seller taxonomy tree.
// GET /etsy/taxonomy
func (h *EtsyHandler) GetTaxonomy(c *gin.Context) {
	client, _, err := h.getEtsyClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error(), "nodes": []interface{}{}})
		return
	}
	nodes, err := client.GetTaxonomyNodes()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error(), "nodes": []interface{}{}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "nodes": nodes})
}

// GetTaxonomyProperties returns the properties for a taxonomy node.
// GET /etsy/taxonomy/:id/properties
func (h *EtsyHandler) GetTaxonomyProperties(c *gin.Context) {
	client, _, err := h.getEtsyClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}
	idStr := c.Param("id")
	var taxonomyID int64
	fmt.Sscanf(idStr, "%d", &taxonomyID)
	if taxonomyID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "invalid taxonomy id"})
		return
	}
	props, err := client.GetTaxonomyProperties(taxonomyID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error(), "properties": []interface{}{}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "properties": props})
}

// ── Shipping Profiles ─────────────────────────────────────────────────────────

// GetShippingProfiles returns all shipping profiles for the shop.
// GET /etsy/shipping-profiles
func (h *EtsyHandler) GetShippingProfiles(c *gin.Context) {
	client, _, err := h.getEtsyClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error(), "profiles": []interface{}{}})
		return
	}
	profiles, err := client.GetShippingProfiles()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error(), "profiles": []interface{}{}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "profiles": profiles})
}

// ── Image Upload ──────────────────────────────────────────────────────────────

// UploadImage fetches an image URL and re-encodes it as base64 for the Etsy API.
// POST /etsy/images/upload  { "url": "...", "listing_id": 123, "rank": 1 }
func (h *EtsyHandler) UploadImage(c *gin.Context) {
	client, _, err := h.getEtsyClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	var req struct {
		URL       string `json:"url" binding:"required"`
		ListingID int64  `json:"listing_id"`
		Rank      int    `json:"rank"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	// Fetch the image
	resp, err := http.Get(req.URL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "failed to fetch image: " + err.Error()})
		return
	}
	defer resp.Body.Close()
	imgBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "failed to read image: " + err.Error()})
		return
	}
	imgBase64 := base64.StdEncoding.EncodeToString(imgBytes)

	if req.ListingID == 0 {
		// Just return the base64 without uploading (will be uploaded post-creation)
		c.JSON(http.StatusOK, gin.H{"ok": true, "base64": imgBase64, "original_url": req.URL})
		return
	}

	rank := req.Rank
	if rank == 0 {
		rank = 1
	}
	img, err := client.UploadListingImage(req.ListingID, imgBase64, rank)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "image": img})
}

// ── Prepare & Submit ──────────────────────────────────────────────────────────

// PrepareListingDraft loads a product from MarketMate and builds an Etsy draft.
// POST /etsy/prepare  { "product_id": "...", "credential_id": "..." }
func (h *EtsyHandler) PrepareListingDraft(c *gin.Context) {
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

	fallbackPrice := ""
	if p, ok := product.Attributes["price"].(float64); ok {
		fallbackPrice = fmt.Sprintf("%.2f", p)
	}
	fallbackImage := ""
	if len(images) > 0 {
		fallbackImage = images[0]
	}

	draft := gin.H{
		"title":        product.Title,
		"description":  product.Description,
		"price":        product.Attributes["price"],
		"quantity":     product.Attributes["quantity"],
		"sku":          product.Attributes["source_sku"],
		"images":       images,
		"tags":         []string{},
		"materials":    []string{},
		"who_made":     "i_did",
		"when_made":    "made_to_order",
		"is_supply":    false,
		"taxonomy_id":  0,
		"variants":     loadChannelVariants(c.Request.Context(), h.productRepo, tenantID, req.ProductID, fallbackPrice, fallbackImage),
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":         true,
		"product_id": req.ProductID,
		"draft":      draft,
	})
}

// SubmitListing creates a new listing on Etsy.
// POST /etsy/submit
func (h *EtsyHandler) SubmitListing(c *gin.Context) {
	client, _, err := h.getEtsyClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	var payload struct {
		Title             string                `json:"title" binding:"required"`
		Description       string                `json:"description"`
		Price             float64               `json:"price" binding:"required"`
		Quantity          int                   `json:"quantity"`
		TaxonomyID        int64                 `json:"taxonomy_id" binding:"required"`
		WhoMade           string                `json:"who_made"`
		WhenMade          string                `json:"when_made"`
		IsSupply          bool                  `json:"is_supply"`
		ShippingProfileID int64                 `json:"shipping_profile_id"`
		Tags              []string              `json:"tags"`
		Materials         []string              `json:"materials"`
		Images            []string              `json:"images"`
		Variants          []ChannelVariantDraft `json:"variants"` // VAR-01
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "invalid payload: " + err.Error()})
		return
	}

	if payload.Quantity < 1 {
		payload.Quantity = 1
	}
	if payload.WhoMade == "" {
		payload.WhoMade = "i_did"
	}
	if payload.WhenMade == "" {
		payload.WhenMade = "made_to_order"
	}

	// Validate tags (max 13)
	if len(payload.Tags) > 13 {
		payload.Tags = payload.Tags[:13]
	}
	if len(payload.Materials) > 13 {
		payload.Materials = payload.Materials[:13]
	}

	req := &etsy.CreateListingRequest{
		Title:             payload.Title,
		Description:       payload.Description,
		Price:             payload.Price,
		Quantity:          payload.Quantity,
		TaxonomyID:        payload.TaxonomyID,
		WhoMade:           payload.WhoMade,
		WhenMade:          payload.WhenMade,
		IsSupply:          payload.IsSupply,
		ShippingProfileID: payload.ShippingProfileID,
		Tags:              payload.Tags,
		Materials:         payload.Materials,
	}

	listing, err := client.CreateListing(req)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	// VAR-01: If active variants were sent, update listing inventory with offerings.
	// Etsy does not support multi-variant listings natively at create time —
	// inventory must be set via a separate PUT after the listing exists.
	var inventoryWarning string
	activeVariants := make([]ChannelVariantDraft, 0)
	for _, v := range payload.Variants {
		if v.Active {
			activeVariants = append(activeVariants, v)
		}
	}
	if len(activeVariants) >= 2 {
		products := make([]etsy.ListingProduct, 0, len(activeVariants))
		for _, v := range activeVariants {
			priceFloat, _ := strconv.ParseFloat(v.Price, 64)
			priceInt := int(priceFloat * 100)
			qty := 1
			if q, err := strconv.Atoi(v.Stock); err == nil && q > 0 {
				qty = q
			}
			// Build property values from the variant combination map
			propVals := make([]etsy.PropertyValue, 0, len(v.Combination))
			for k, val := range v.Combination {
				propVals = append(propVals, etsy.PropertyValue{
					PropertyName: k,
					Values:       []string{val},
				})
			}
			products = append(products, etsy.ListingProduct{
				PropertyValues: propVals,
				Sku:            v.SKU,
				Offerings: []etsy.ListingOffering{
					{
						Price:     etsy.ListingOfferingPrice{Amount: priceInt, Divisor: 100, CurrencyCode: "GBP"},
						Quantity:  qty,
						IsEnabled: true,
					},
				},
			})
		}
		invReq := &etsy.ListingInventoryRequest{Products: products}
		if err := client.UpdateListingInventory(listing.ListingID, invReq); err != nil {
			inventoryWarning = fmt.Sprintf("Listing created but variant inventory update failed: %v", err)
			log.Printf("[Etsy Submit] WARNING: %s", inventoryWarning)
		} else {
			log.Printf("[Etsy Submit] Inventory updated with %d variant offerings", len(activeVariants))
		}
	}

	// Upload images sequentially
	for i, imgData := range payload.Images {
		if imgData == "" {
			continue
		}
		// imgData may be a URL or base64
		var imgBase64 string
		if strings.HasPrefix(imgData, "http") {
			resp, err := http.Get(imgData)
			if err == nil {
				defer resp.Body.Close()
				imgBytes, _ := io.ReadAll(resp.Body)
				imgBase64 = base64.StdEncoding.EncodeToString(imgBytes)
			}
		} else {
			imgBase64 = imgData
		}
		if imgBase64 != "" {
			if _, err := client.UploadListingImage(listing.ListingID, imgBase64, i+1); err != nil {
				log.Printf("[Etsy] WARNING: failed to upload image %d for listing %d: %v", i+1, listing.ListingID, err)
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":                true,
		"listing_id":        listing.ListingID,
		"state":             listing.State,
		"inventory_warning": inventoryWarning,
	})
}

// UpdateProductListing updates an existing Etsy listing.
// PUT /etsy/listings/:id
func (h *EtsyHandler) UpdateProductListing(c *gin.Context) {
	client, _, err := h.getEtsyClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	listingIDStr := c.Param("id")
	var listingID int64
	fmt.Sscanf(listingIDStr, "%d", &listingID)
	if listingID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "invalid listing id"})
		return
	}

	var payload etsy.UpdateListingRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	result, err := client.UpdateListing(listingID, &payload)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "listing_id": result.ListingID, "state": result.State})
}

// DeleteProductListing removes a listing from Etsy.
// DELETE /etsy/listings/:id
func (h *EtsyHandler) DeleteProductListing(c *gin.Context) {
	client, _, err := h.getEtsyClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	listingIDStr := c.Param("id")
	var listingID int64
	fmt.Sscanf(listingIDStr, "%d", &listingID)
	if listingID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "invalid listing id"})
		return
	}

	if err := client.DeleteListing(listingID); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "deleted": listingID})
}

// GetListings returns paginated listings from the Etsy shop.
// GET /etsy/listings?offset=0&limit=50
func (h *EtsyHandler) GetListings(c *gin.Context) {
	client, _, err := h.getEtsyClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error(), "listings": []interface{}{}})
		return
	}

	offset := 0
	limit := 50
	if s := c.Query("offset"); s != "" {
		fmt.Sscanf(s, "%d", &offset)
	}
	if s := c.Query("limit"); s != "" {
		fmt.Sscanf(s, "%d", &limit)
	}

	resp, err := client.GetListings(offset, limit)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error(), "listings": []interface{}{}})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":       true,
		"listings": resp.Results,
		"count":    resp.Count,
		"offset":   offset,
		"limit":    limit,
	})
}
