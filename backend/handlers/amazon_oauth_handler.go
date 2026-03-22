package handlers

// ============================================================================
// AMAZON SP-API OAUTH HANDLER
// ============================================================================
// Implements the Login with Amazon (LWA) OAuth 2.0 flow for SP-API.
//
// Flow:
//   1. GET /amazon/oauth/login?marketplace_id=...&account_name=...
//      → Returns consent_url. Frontend opens this in a popup.
//
//   2. Amazon redirects to:
//      GET /api/v1/amazon/oauth/callback?spapi_oauth_code=...&state=...&selling_partner_id=...
//      → Exchanges code for refresh token, saves credential.
//
// Credential storage (per-tenant, NOT global):
//   Each credential record stores lwa_client_id + lwa_client_secret + refresh_token
//   + seller_id + marketplace_id so it works independently of whatever is in
//   platform_config/amazon. This means multiple apps can coexist.
//
// Required env vars:
//   AMAZON_LWA_CLIENT_ID     — your app's LWA client ID
//   AMAZON_LWA_CLIENT_SECRET — your app's LWA client secret
//   AMAZON_REDIRECT_URI      — must match Allowed Return URLs in Seller Central
//
// ============================================================================

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"module-a/models"
	"module-a/repository"
	"module-a/services"
)

// ── Handler ───────────────────────────────────────────────────────────────────

type AmazonOAuthHandler struct {
	marketplaceService *services.MarketplaceService
	repo               *repository.MarketplaceRepository
}

func NewAmazonOAuthHandler(
	marketplaceService *services.MarketplaceService,
	repo *repository.MarketplaceRepository,
) *AmazonOAuthHandler {
	return &AmazonOAuthHandler{
		marketplaceService: marketplaceService,
		repo:               repo,
	}
}

// ── Marketplace reference data ────────────────────────────────────────────────

type amazonMarketplace struct {
	ID       string
	Name     string
	Region   string
	Endpoint string
	SellURL  string // Seller Central URL for this region
}

var amazonMarketplaces = map[string]amazonMarketplace{
	"A1F83G8C2ARO7P": {
		ID: "A1F83G8C2ARO7P", Name: "Amazon UK", Region: "eu-west-1",
		Endpoint: "https://sellingpartnerapi-eu.amazon.com",
		SellURL:  "https://sellercentral.amazon.co.uk",
	},
	"ATVPDKIKX0DER": {
		ID: "ATVPDKIKX0DER", Name: "Amazon US", Region: "us-east-1",
		Endpoint: "https://sellingpartnerapi-na.amazon.com",
		SellURL:  "https://sellercentral.amazon.com",
	},
	"A1PA6795UKMFR9": {
		ID: "A1PA6795UKMFR9", Name: "Amazon DE", Region: "eu-west-1",
		Endpoint: "https://sellingpartnerapi-eu.amazon.com",
		SellURL:  "https://sellercentral.amazon.de",
	},
	"A13V1IB3VIYZZH": {
		ID: "A13V1IB3VIYZZH", Name: "Amazon FR", Region: "eu-west-1",
		Endpoint: "https://sellingpartnerapi-eu.amazon.com",
		SellURL:  "https://sellercentral.amazon.fr",
	},
	"APJ6JRA9NG5V4": {
		ID: "APJ6JRA9NG5V4", Name: "Amazon IT", Region: "eu-west-1",
		Endpoint: "https://sellingpartnerapi-eu.amazon.com",
		SellURL:  "https://sellercentral.amazon.it",
	},
	"A1RKKUPIHCS9HS": {
		ID: "A1RKKUPIHCS9HS", Name: "Amazon ES", Region: "eu-west-1",
		Endpoint: "https://sellingpartnerapi-eu.amazon.com",
		SellURL:  "https://sellercentral.amazon.es",
	},
}

// getSellCentralURL returns the seller central URL for a marketplace
func getSellCentralURL(marketplaceID string) string {
	if m, ok := amazonMarketplaces[marketplaceID]; ok {
		return m.SellURL
	}
	return "https://sellercentral.amazon.co.uk" // default UK
}

// ============================================================================
// OAuthLogin — GET /amazon/oauth/login
// ============================================================================
// Returns the Seller Central consent URL for the given marketplace.
// Query params:
//   marketplace_id — Shopify marketplace ID (default: A1F83G8C2ARO7P = UK)
//   account_name   — Human label for this connection
//
func (h *AmazonOAuthHandler) OAuthLogin(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	marketplaceID := c.DefaultQuery("marketplace_id", "A1F83G8C2ARO7P")
	accountName := c.DefaultQuery("account_name", "Amazon Account")

	clientID := os.Getenv("AMAZON_LWA_CLIENT_ID")
	redirectURI := os.Getenv("AMAZON_REDIRECT_URI")
	appID := os.Getenv("AMAZON_APP_ID") // amzn1.sp.solution.xxx

	if clientID == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "AMAZON_LWA_CLIENT_ID not configured"})
		return
	}
	if redirectURI == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "AMAZON_REDIRECT_URI not configured"})
		return
	}
	if appID == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "AMAZON_APP_ID not configured"})
		return
	}

	// State encodes tenantID|accountName|marketplaceID
	stateRaw := tenantID + "|" + accountName + "|" + marketplaceID
	state := base64.URLEncoding.EncodeToString([]byte(stateRaw))

	sellCentralURL := getSellCentralURL(marketplaceID)

	// Amazon SP-API OAuth consent URL
	// version=beta is required for draft/development apps
	consentURL := fmt.Sprintf(
		"%s/apps/authorize/consent?application_id=%s&state=%s&version=beta",
		sellCentralURL,
		url.QueryEscape(appID),
		url.QueryEscape(state),
	)

	c.JSON(http.StatusOK, gin.H{
		"ok":             true,
		"consent_url":    consentURL,
		"marketplace_id": marketplaceID,
		"message":        "Redirect the user to consent_url to authorise Amazon SP-API access",
	})
}

// ============================================================================
// PublicConnect — GET /api/v1/amazonnew/connect  (NO AUTH REQUIRED)
// ============================================================================
// A shareable link that immediately redirects to the Amazon consent page.
// The tenant_id is embedded in the URL so no login is needed.
// Send this URL to a seller and they land directly on Amazon's auth screen.
//
// Example:
//   https://marketmate-api-xxx.run.app/api/v1/amazonnew/connect?tenant=tenant-10007&marketplace_id=A1F83G8C2ARO7P&account_name=My+Store
//
func (h *AmazonOAuthHandler) PublicConnect(c *gin.Context) {
	tenantID := c.Query("tenant")
	marketplaceID := c.DefaultQuery("marketplace_id", "A1F83G8C2ARO7P")
	accountName := c.DefaultQuery("account_name", "Amazon Account")

	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant parameter is required"})
		return
	}

	appID := os.Getenv("AMAZON_APP_ID")
	redirectURI := os.Getenv("AMAZON_REDIRECT_URI")

	if appID == "" || redirectURI == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Amazon app not configured"})
		return
	}

	stateRaw := tenantID + "|" + accountName + "|" + marketplaceID
	state := base64.URLEncoding.EncodeToString([]byte(stateRaw))
	sellCentralURL := getSellCentralURL(marketplaceID)

	consentURL := fmt.Sprintf(
		"%s/apps/authorize/consent?application_id=%s&state=%s&version=beta",
		sellCentralURL,
		url.QueryEscape(appID),
		url.QueryEscape(state),
	)

	// Direct redirect — no JSON, no login screen, straight to Amazon
	c.Redirect(http.StatusFound, consentURL)
}

// ============================================================================
// OAuthCallback — GET /api/v1/amazon/oauth/callback
// ============================================================================
// Amazon redirects here after the seller grants access.
// Query params from Amazon:
//   spapi_oauth_code    — auth code to exchange for refresh token
//   selling_partner_id  — the seller ID (MerchantToken)
//   state               — our state blob
//
func (h *AmazonOAuthHandler) OAuthCallback(c *gin.Context) {
	code := c.Query("spapi_oauth_code")
	sellerID := c.Query("selling_partner_id")
	state := c.Query("state")

	clientID := os.Getenv("AMAZON_LWA_CLIENT_ID")
	clientSecret := os.Getenv("AMAZON_LWA_CLIENT_SECRET")
	redirectURI := os.Getenv("AMAZON_REDIRECT_URI")
	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "https://marketmate.app"
	}

	failHTML := func(msg string) {
		escaped := strings.ReplaceAll(msg, "'", "\\'")
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(fmt.Sprintf(`
		<html><body style="font-family:sans-serif;text-align:center;padding:60px">
			<h2 style="color:#E53238">❌ Amazon Connection Failed</h2>
			<p>%s</p>
			<p>Please close this window and try again.</p>
			<script>
				if (window.opener) {
					window.opener.postMessage({type:'amazonnew-oauth-error',error:'%s'},'*');
					setTimeout(()=>window.close(),4000);
				}
			</script>
		</body></html>`, msg, escaped)))
	}

	if code == "" {
		failHTML("Amazon did not return an authorisation code. Please try again.")
		return
	}

	// ── Decode state ──────────────────────────────────────────────────────
	stateBytes, _ := base64.URLEncoding.DecodeString(state)
	parts := strings.SplitN(string(stateBytes), "|", 3)
	tenantID := "tenant-demo"
	accountName := "Amazon Account"
	marketplaceID := "A1F83G8C2ARO7P"
	if len(parts) >= 1 && parts[0] != "" {
		tenantID = parts[0]
	}
	if len(parts) >= 2 && parts[1] != "" {
		accountName = parts[1]
	}
	if len(parts) >= 3 && parts[2] != "" {
		marketplaceID = parts[2]
	}

	// ── Exchange code for refresh token ───────────────────────────────────
	tokenData := url.Values{}
	tokenData.Set("grant_type", "authorization_code")
	tokenData.Set("code", code)
	tokenData.Set("redirect_uri", redirectURI)
	tokenData.Set("client_id", clientID)
	tokenData.Set("client_secret", clientSecret)

	tokenReq, err := http.NewRequest("POST",
		"https://api.amazon.com/auth/o2/token",
		strings.NewReader(tokenData.Encode()))
	if err != nil {
		failHTML("Failed to build token exchange request.")
		return
	}
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	httpClient := &http.Client{Timeout: 15 * time.Second}
	tokenResp, err := httpClient.Do(tokenReq)
	if err != nil {
		log.Printf("[AmazonNew OAuth] Token exchange error: %v", err)
		failHTML("Token exchange failed: " + err.Error())
		return
	}
	defer tokenResp.Body.Close()

	body, _ := io.ReadAll(tokenResp.Body)
	var tokenResult struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &tokenResult); err != nil || tokenResult.RefreshToken == "" {
		log.Printf("[AmazonNew OAuth] Token decode error: %v body: %s", err, string(body))
		msg := "Could not decode refresh token from Amazon."
		if tokenResult.ErrorDesc != "" {
			msg = tokenResult.ErrorDesc
		}
		failHTML(msg)
		return
	}

	log.Printf("[AmazonNew OAuth] Got refresh token for seller=%s tenant=%s marketplace=%s",
		sellerID, tenantID, marketplaceID)

	// ── Resolve marketplace info ───────────────────────────────────────────
	mp := amazonMarketplaces[marketplaceID]
	region := mp.Region
	endpoint := mp.Endpoint
	mpName := mp.Name
	if region == "" {
		region = "eu-west-1"
		endpoint = "https://sellingpartnerapi-eu.amazon.com"
		mpName = "Amazon"
	}

	// ── Save credential ───────────────────────────────────────────────────
	// Store per-tenant so it uses this app's LWA keys, not the global ones.
	// This allows multiple Amazon apps to coexist.
	ctx := context.Background()
	creds, _ := h.repo.ListCredentials(ctx, tenantID)

	// Find existing credential for this seller+marketplace
	var existingCredID string
	for _, cr := range creds {
		if cr.Channel == "amazonnew" &&
			cr.CredentialData["seller_id"] == sellerID &&
			cr.CredentialData["marketplace_id"] == marketplaceID {
			existingCredID = cr.CredentialID
			break
		}
	}

	credData := map[string]string{
		// LWA app keys stored per-tenant so they override global keys
		"lwa_client_id":     clientID,
		"lwa_client_secret": clientSecret,
		// Per-seller tokens
		"refresh_token":  tokenResult.RefreshToken,
		"seller_id":      sellerID,
		"marketplace_id": marketplaceID,
		"region":         region,
		"sp_endpoint":    endpoint,
	}

	if existingCredID != "" {
		existing, err := h.repo.GetCredential(ctx, tenantID, existingCredID)
		if err == nil {
			for k, v := range credData {
				existing.CredentialData[k] = v
			}
			existing.AccountName = accountName
			existing.UpdatedAt = time.Now()
			h.repo.SaveCredential(ctx, existing)
			log.Printf("[AmazonNew OAuth] Updated credential %s for seller %s", existingCredID, sellerID)
		}
	} else {
		credID := fmt.Sprintf("cred-amazonnew-%d", time.Now().UnixMilli())
		newCred := &models.MarketplaceCredential{
			CredentialID:   credID,
			TenantID:       tenantID,
			Channel:        "amazonnew",
			AccountName:    accountName,
			Environment:    "production",
			Active:         true,
			CredentialData: credData,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}
		if err := h.repo.SaveCredential(ctx, newCred); err != nil {
			log.Printf("[AmazonNew OAuth] Failed to save credential: %v", err)
			failHTML("Failed to save credential: " + err.Error())
			return
		}
		log.Printf("[AmazonNew OAuth] Created credential %s for seller %s", credID, sellerID)
	}

	// ── Success page ──────────────────────────────────────────────────────
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(fmt.Sprintf(`
	<html><body style="font-family:sans-serif;text-align:center;padding:60px">
		<h2 style="color:#4CAF50">&#x2705; Amazon Connected Successfully!</h2>
		<p><strong>%s</strong> has been authorised.</p>
		<p style="color:#666;font-size:14px">Seller ID: %s &bull; Marketplace: %s</p>
		<p style="font-size:13px;color:#888">This window will close automatically…</p>
		<script>
			function notify() {
				try {
					if (window.opener && !window.opener.closed) {
						window.opener.postMessage({type:'amazonnew-oauth-success',seller_id:'%s'},'*');
					}
				} catch(e) {}
			}
			notify();
			window.addEventListener('load', notify);
			try { localStorage.setItem('amazonnew-oauth-result', JSON.stringify({type:'amazonnew-oauth-success',ts:Date.now()})); } catch(e) {}
			setTimeout(function(){ notify(); window.close(); }, 2000);
		</script>
	</body></html>`, accountName, sellerID, mpName, sellerID)))
}
