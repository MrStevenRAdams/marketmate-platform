package handlers

import (
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"module-a/marketplace/clients/ebay"
	"module-a/models"
	"module-a/repository"
	"module-a/services"
)

// ============================================================================
// EBAY HANDLER
// ============================================================================
// Endpoints:
//   GET  /ebay/oauth/login         → Redirect user to eBay consent page
//   GET  /ebay/oauth/callback      → eBay redirects here with auth code
//   GET  /ebay/inventory           → List inventory items (paginated)
//   GET  /ebay/inventory/:sku      → Get single inventory item
//   GET  /ebay/offers/:sku         → Get offers for a SKU
//   GET  /ebay/policies            → Get business policies (payment, return, shipping)
//   GET  /ebay/locations           → Get inventory locations
//   GET  /ebay/categories/suggest  → Category suggestions from product title
// ============================================================================

type EbayHandler struct {
	marketplaceService *services.MarketplaceService
	repo               *repository.MarketplaceRepository
	productRepo        *repository.FirestoreRepository
}

func NewEbayHandler(
	marketplaceService *services.MarketplaceService,
	repo *repository.MarketplaceRepository,
	productRepo *repository.FirestoreRepository,
) *EbayHandler {
	return &EbayHandler{
		marketplaceService: marketplaceService,
		repo:               repo,
		productRepo:        productRepo,
	}
}

// ============================================================================
// CREDENTIAL RESOLUTION
// ============================================================================

func (h *EbayHandler) getEbayClient(c *gin.Context) (*ebay.Client, error) {
	tenantID := c.GetString("tenant_id")
	credentialID := c.Query("credential_id")

	if credentialID == "" {
		creds, err := h.repo.ListCredentials(c.Request.Context(), tenantID)
		if err != nil {
			return nil, fmt.Errorf("list credentials: %w", err)
		}
		for _, cred := range creds {
			if cred.Channel == "ebay" && cred.Active {
				credentialID = cred.CredentialID
				break
			}
		}
		if credentialID == "" {
			return nil, fmt.Errorf("no eBay credential found — please connect an eBay account first")
		}
	}

	cred, err := h.repo.GetCredential(c.Request.Context(), tenantID, credentialID)
	if err != nil {
		return nil, fmt.Errorf("get credential: %w", err)
	}

	// Merge global + per-tenant credentials
	mergedCreds, err := h.marketplaceService.GetFullCredentials(c.Request.Context(), cred)
	if err != nil {
		return nil, fmt.Errorf("merge credentials: %w", err)
	}

	clientID := mergedCreds["client_id"]
	clientSecret := mergedCreds["client_secret"]
	devID := mergedCreds["dev_id"]
	accessToken := mergedCreds["oauth_token"]
	refreshToken := mergedCreds["refresh_token"]

	if clientID == "" || clientSecret == "" {
		return nil, fmt.Errorf("incomplete eBay credentials (need client_id, client_secret)")
	}

	production := cred.Environment == "production"
	client := ebay.NewClient(clientID, clientSecret, devID, production)

	if refreshToken != "" {
		client.SetTokens("", refreshToken) // Let it refresh automatically
	} else if accessToken != "" {
		client.SetTokens(accessToken, "")
	}

	// Set callback to persist refreshed tokens back to Firestore
	client.OnTokenRefresh = func(newAccess, newRefresh string, expiresIn int) {
		existingCred, err := h.repo.GetCredential(c.Request.Context(), tenantID, credentialID)
		if err != nil {
			log.Printf("[eBay] WARNING: failed to get credential for token update: %v", err)
			return
		}
		existingCred.CredentialData["oauth_token"] = newAccess
		if newRefresh != "" {
			existingCred.CredentialData["refresh_token"] = newRefresh
		}
		if err := h.repo.SaveCredential(c.Request.Context(), existingCred); err != nil {
			log.Printf("[eBay] WARNING: failed to persist refreshed token: %v", err)
		}
	}

	return client, nil
}

// ============================================================================
// OAUTH ENDPOINTS
// ============================================================================

// OAuthLogin redirects the user to eBay's consent page
func (h *EbayHandler) OAuthLogin(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	accountName := c.Query("account_name")
	environment := c.Query("environment")
	if accountName == "" {
		accountName = "eBay Account"
	}
	if environment == "" {
		environment = "production"
	}

	// Get global eBay keys
	clientID := os.Getenv("EBAY_PROD_CLIENT_ID")
	ruName := os.Getenv("EBAY_RUNAME")

	if clientID == "" {
		c.JSON(400, gin.H{"error": "EBAY_PROD_CLIENT_ID not configured"})
		return
	}
	if ruName == "" {
		ruName = clientID
	}

	production := environment == "production"
	client := ebay.NewClient(clientID, "", "", production)

	// State parameter encodes tenant|accountName|environment
	stateRaw := tenantID + "|" + accountName + "|" + environment
	state := base64.URLEncoding.EncodeToString([]byte(stateRaw))

	consentURL := client.GetConsentURL(ruName, state)

	c.JSON(200, gin.H{
		"ok":          true,
		"consent_url": consentURL,
		"message":     "Redirect the user to consent_url to authorize eBay access",
	})
}

// OAuthCallback handles eBay's redirect after user consent
func (h *EbayHandler) OAuthCallback(c *gin.Context) {
	code := c.Query("code")
	state := c.Query("state")
	isSuccess := c.Query("isAuthSuccessful")

	if isSuccess == "false" || code == "" {
		c.Data(200, "text/html; charset=utf-8", []byte(`<html><body style="font-family:sans-serif;text-align:center;padding:60px">
			<h2 style="color:#E53238">❌ eBay Authorization Failed</h2>
			<p>Please try again from the Connections page.</p>
			<script>
				if (window.opener) {
					window.opener.postMessage({type:'ebay-oauth-error'}, '*');
					setTimeout(() => window.close(), 3000);
				}
			</script>
		</body></html>`))
		return
	}

	// Decode state: tenantID|accountName|environment
	stateBytes, _ := base64.URLEncoding.DecodeString(state)
	stateStr := string(stateBytes)
	parts := strings.SplitN(stateStr, "|", 3)
	tenantID := "tenant-demo"
	accountName := "eBay Account"
	environment := "production"
	if len(parts) >= 1 && parts[0] != "" {
		tenantID = parts[0]
	}
	if len(parts) >= 2 && parts[1] != "" {
		accountName = parts[1]
	}
	if len(parts) >= 3 && parts[2] != "" {
		environment = parts[2]
	}

	// Get global keys
	clientID := os.Getenv("EBAY_PROD_CLIENT_ID")
	clientSecret := os.Getenv("EBAY_PROD_CLIENT_SECRET")
	ruName := os.Getenv("EBAY_RUNAME")
	if ruName == "" {
		ruName = clientID
	}

	client := ebay.NewClient(clientID, clientSecret, "", environment == "production")

	// Exchange auth code for tokens
	tokenResp, err := client.ExchangeAuthCode(code, ruName)
	if err != nil {
		log.Printf("[eBay OAuth] Token exchange failed: %v", err)
		c.Data(200, "text/html; charset=utf-8", []byte(fmt.Sprintf(`<html><body style="font-family:sans-serif;text-align:center;padding:60px">
			<h2 style="color:#E53238">❌ eBay Token Exchange Failed</h2>
			<p>%s</p><p>Please try again.</p>
			<script>
				if (window.opener) {
					window.opener.postMessage({type:'ebay-oauth-error', error: '%s'}, '*');
					setTimeout(() => window.close(), 5000);
				}
			</script>
		</body></html>`, err.Error(), err.Error())))
		return
	}

	// Find or create the eBay credential for this tenant
	creds, _ := h.repo.ListCredentials(c.Request.Context(), tenantID)
	var credentialID string
	for _, cred := range creds {
		if cred.Channel == "ebay" && cred.Active {
			credentialID = cred.CredentialID
			break
		}
	}

	// Try to fetch seller username via Identity API
	client.SetTokens(tokenResp.AccessToken, tokenResp.RefreshToken)
	sellerUsername, err := client.GetSellerUsernamePublic()
	if err != nil {
		log.Printf("[eBay OAuth] Could not fetch seller username (non-fatal): %v", err)
	} else {
		log.Printf("[eBay OAuth] Got seller username: %s", sellerUsername)
	}

	if credentialID != "" {
		// Update existing credential with new tokens
		existingCred, err := h.repo.GetCredential(c.Request.Context(), tenantID, credentialID)
		if err == nil {
			existingCred.CredentialData["oauth_token"] = tokenResp.AccessToken
			if tokenResp.RefreshToken != "" {
				existingCred.CredentialData["refresh_token"] = tokenResp.RefreshToken
			}
			if sellerUsername != "" {
				existingCred.CredentialData["seller_username"] = sellerUsername
			}
			existingCred.AccountName = accountName
			existingCred.Environment = environment
			h.repo.SaveCredential(c.Request.Context(), existingCred)
		}
	} else {
		// Create new credential
		credID := "cred-ebay-" + fmt.Sprintf("%d", time.Now().UnixMilli())
		newCred := &models.MarketplaceCredential{
			CredentialID: credID,
			TenantID:     tenantID,
			Channel:      "ebay",
			AccountName:  accountName,
			Environment:  environment,
			Active:       true,
			CredentialData: map[string]string{
				"oauth_token": tokenResp.AccessToken,
			},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		if tokenResp.RefreshToken != "" {
			newCred.CredentialData["refresh_token"] = tokenResp.RefreshToken
		}
		if sellerUsername != "" {
			newCred.CredentialData["seller_username"] = sellerUsername
		}
		if err := h.repo.SaveCredential(c.Request.Context(), newCred); err != nil {
			log.Printf("[eBay OAuth] Failed to save credential: %v", err)
		}
	}

	log.Printf("[eBay OAuth] Successfully authorized for tenant %s, account '%s' (token expires in %ds, refresh expires in %ds)",
		tenantID, accountName, tokenResp.ExpiresIn, tokenResp.RefreshTokenExpiresIn)

	// Return a simple HTML page that closes itself
	c.Data(200, "text/html; charset=utf-8", []byte(`
		<html><body style="font-family:sans-serif;text-align:center;padding:60px">
			<h2 style="color:#4CAF50">✅ eBay Connected Successfully!</h2>
			<p>Your eBay account has been authorized.</p>
			<p style="color:#888">This window will close automatically...</p>
			<script>
				if (window.opener) {
					window.opener.postMessage({type:'ebay-oauth-success'}, '*');
					setTimeout(() => window.close(), 2000);
				}
			</script>
		</body></html>
	`))
}

// ============================================================================
// INVENTORY ENDPOINTS
// ============================================================================

// ListInventory returns paginated inventory items
func (h *EbayHandler) ListInventory(c *gin.Context) {
	client, err := h.getEbayClient(c)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	limit := 25
	offset := 0
	fmt.Sscanf(c.DefaultQuery("limit", "25"), "%d", &limit)
	fmt.Sscanf(c.DefaultQuery("offset", "0"), "%d", &offset)

	page, err := client.GetInventoryItems(limit, offset)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{
		"ok":    true,
		"total": page.Total,
		"size":  page.Size,
		"items": page.InventoryItems,
	})
}

// GetInventoryItem returns a single inventory item by SKU
func (h *EbayHandler) GetInventoryItem(c *gin.Context) {
	client, err := h.getEbayClient(c)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	sku := c.Param("sku")
	if sku == "" {
		c.JSON(400, gin.H{"error": "sku is required"})
		return
	}

	item, err := client.GetInventoryItem(sku)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"ok": true, "item": item})
}

// GetOffers returns offers for a SKU
func (h *EbayHandler) GetOffers(c *gin.Context) {
	client, err := h.getEbayClient(c)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	sku := c.Param("sku")
	page, err := client.GetOffers(sku)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"ok": true, "total": page.Total, "offers": page.Offers})
}

// ============================================================================
// BUSINESS POLICIES
// ============================================================================

// GetPolicies returns payment, return, and fulfillment policies
func (h *EbayHandler) GetPolicies(c *gin.Context) {
	client, err := h.getEbayClient(c)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	marketplace := c.DefaultQuery("marketplace", "EBAY_GB")

	fulfillment, fErr := client.GetFulfillmentPolicies(marketplace)
	payment, pErr := client.GetPaymentPolicies(marketplace)
	returns, rErr := client.GetReturnPolicies(marketplace)

	// Build error list if any
	var errors []string
	if fErr != nil {
		errors = append(errors, "fulfillment: "+fErr.Error())
	}
	if pErr != nil {
		errors = append(errors, "payment: "+pErr.Error())
	}
	if rErr != nil {
		errors = append(errors, "return: "+rErr.Error())
	}

	c.JSON(200, gin.H{
		"ok":                  len(errors) == 0,
		"errors":              errors,
		"fulfillment_policies": fulfillment,
		"payment_policies":     payment,
		"return_policies":      returns,
	})
}

// ============================================================================
// INVENTORY LOCATIONS
// ============================================================================

// GetLocations returns seller's inventory locations
func (h *EbayHandler) GetLocations(c *gin.Context) {
	client, err := h.getEbayClient(c)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	locations, err := client.GetInventoryLocations()
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"ok": true, "total": locations.Total, "locations": locations.Locations})
}

// ============================================================================
// CATEGORY SUGGESTIONS
// ============================================================================

// SuggestCategories returns eBay category suggestions for a query
func (h *EbayHandler) SuggestCategories(c *gin.Context) {
	client, err := h.getEbayClient(c)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	query := c.Query("q")
	marketplace := c.DefaultQuery("marketplace", "EBAY_GB")

	if query == "" {
		c.JSON(400, gin.H{"error": "q (search query) is required"})
		return
	}

	suggestions, err := client.GetCategorySuggestions(marketplace, query)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"ok": true, "suggestions": suggestions})
}

// ============================================================================
// CONNECTION TEST
// ============================================================================

// TestConnection tests the eBay connection by fetching inventory locations
func (h *EbayHandler) TestConnection(c *gin.Context) {
	client, err := h.getEbayClient(c)
	if err != nil {
		c.JSON(400, gin.H{"ok": false, "error": err.Error()})
		return
	}

	// Try to list inventory items (limit 1) as a connectivity test
	page, err := client.GetInventoryItems(1, 0)
	if err != nil {
		// Check if it's a token issue
		if strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "token") {
			c.JSON(200, gin.H{
				"ok":    false,
				"error": "Authentication failed — your eBay token may have expired. Please re-authorize.",
			})
			return
		}
		c.JSON(200, gin.H{"ok": false, "error": err.Error()})
		return
	}

	c.JSON(200, gin.H{
		"ok":      true,
		"message": fmt.Sprintf("Connected! Found %d inventory items.", page.Total),
	})
}
