package handlers

// ============================================================================
// SHOPIFY HANDLER
// ============================================================================
// OAuth 2.0 flow + credential management + orders + stock + webhooks.
//
// OAuth flow (public app):
//   GET  /shopify/oauth/login      → returns consent_url, state encodes tenant|account|shop
//   GET  /shopify/oauth/callback   → Shopify redirects here; exchanges code for token; saves credential
//
// Connection:
//   GET  /shopify/test             → verify access token still valid
//
// Orders:
//   POST /shopify/orders/import    → pull orders from Shopify into MarketMate
//   GET  /shopify/orders           → list orders from Firestore
//   POST /shopify/orders/:id/ship  → push tracking to Shopify
//
// Stock:
//   POST /shopify/stock            → update inventory level by SKU / inventory_item_id
//
// Webhooks:
//   POST /shopify/webhooks/register → register order webhooks on the Shopify store
//
// Multi-storefront:
//   Each connected Shopify store is a separate MarketplaceCredential (one per shop_domain).
//   credential_id query/body param selects which store; falls back to first active shopify cred.
// ============================================================================

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"module-a/models"
	"module-a/repository"
	"module-a/services"
)

// ── Handler struct ─────────────────────────────────────────────────────────

type ShopifyHandler struct {
	marketplaceService *services.MarketplaceService
	repo               *repository.MarketplaceRepository
	orderService       *services.OrderService
	productRepo        *repository.FirestoreRepository
}

func NewShopifyHandler(
	marketplaceService *services.MarketplaceService,
	repo *repository.MarketplaceRepository,
	orderService *services.OrderService,
	productRepo *repository.FirestoreRepository,
) *ShopifyHandler {
	return &ShopifyHandler{
		marketplaceService: marketplaceService,
		repo:               repo,
		orderService:       orderService,
		productRepo:        productRepo,
	}
}

// ── Internal Shopify API client ────────────────────────────────────────────

type shopifyClient struct {
	shopDomain  string
	accessToken string
	apiVersion  string
	httpClient  *http.Client
}

func newShopifyClient(shopDomain, accessToken, apiVersion string) *shopifyClient {
	if apiVersion == "" {
		apiVersion = "2026-01"
	}
	return &shopifyClient{
		shopDomain:  shopDomain,
		accessToken: accessToken,
		apiVersion:  apiVersion,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (s *shopifyClient) url(path string) string {
	return fmt.Sprintf("https://%s/admin/api/%s%s", s.shopDomain, s.apiVersion, path)
}

func (s *shopifyClient) do(ctx context.Context, method, path string, body interface{}) (map[string]interface{}, int, error) {
	var reqReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("marshal: %w", err)
		}
		reqReader = strings.NewReader(string(b))
	}
	req, err := http.NewRequestWithContext(ctx, method, s.url(path), reqReader)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("X-Shopify-Access-Token", s.accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	if resp.StatusCode >= 400 {
		msg, _ := json.Marshal(result)
		return result, resp.StatusCode, fmt.Errorf("shopify API %d: %s", resp.StatusCode, string(msg))
	}
	return result, resp.StatusCode, nil
}

// ── Credential resolution ──────────────────────────────────────────────────

func (h *ShopifyHandler) getClient(ctx context.Context, tenantID, credentialID string) (*shopifyClient, *models.MarketplaceCredential, error) {
	var cred *models.MarketplaceCredential

	if credentialID != "" {
		c, err := h.repo.GetCredential(ctx, tenantID, credentialID)
		if err != nil {
			return nil, nil, fmt.Errorf("get credential %s: %w", credentialID, err)
		}
		cred = c
	} else {
		creds, err := h.repo.ListCredentials(ctx, tenantID)
		if err != nil {
			return nil, nil, fmt.Errorf("list credentials: %w", err)
		}
		for i, c := range creds {
			if c.Channel == "shopify" && c.Active {
				cred = &creds[i]
				break
			}
		}
	}
	if cred == nil {
		return nil, nil, fmt.Errorf("no active Shopify credential found — please connect a Shopify store first")
	}

	shopDomain := cred.CredentialData["shop_domain"]
	accessToken := cred.CredentialData["access_token"]
	apiVersion := cred.CredentialData["api_version"]
	if shopDomain == "" || accessToken == "" {
		return nil, nil, fmt.Errorf("Shopify credential missing shop_domain or access_token")
	}

	return newShopifyClient(shopDomain, accessToken, apiVersion), cred, nil
}

// ============================================================================
// OAUTH — Public App flow
// ============================================================================
// Required env vars:
//   SHOPIFY_CLIENT_ID     — from your Shopify Partner app
//   SHOPIFY_CLIENT_SECRET — from your Shopify Partner app
//   SHOPIFY_REDIRECT_URI  — must match what's registered in Partners dashboard
//                           e.g. https://marketmate-api-xxx.run.app/api/v1/shopify/oauth/callback
//
// Scopes requested: read_products,write_products,read_orders,write_orders,
//                   read_inventory,write_inventory,read_fulfillments,write_fulfillments
// ============================================================================

const shopifyScopes = "read_products,write_products,read_orders,write_orders,read_inventory,write_inventory,read_fulfillments,write_fulfillments"

// OAuthLogin returns the Shopify consent URL for the given store.
// GET /shopify/oauth/login?shop=mystore.myshopify.com&account_name=My+Store
func (h *ShopifyHandler) OAuthLogin(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	shop := c.Query("shop")         // e.g. mystore.myshopify.com
	accountName := c.Query("account_name")

	if shop == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "shop parameter is required (e.g. mystore.myshopify.com)"})
		return
	}
	// Normalise — strip https:// if user pastes full URL
	shop = strings.TrimPrefix(shop, "https://")
	shop = strings.TrimPrefix(shop, "http://")
	shop = strings.TrimSuffix(shop, "/")

	clientID := os.Getenv("SHOPIFY_CLIENT_ID")
	redirectURI := os.Getenv("SHOPIFY_REDIRECT_URI")

	if clientID == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "SHOPIFY_CLIENT_ID not configured"})
		return
	}
	if redirectURI == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "SHOPIFY_REDIRECT_URI not configured"})
		return
	}
	if accountName == "" {
		accountName = shop
	}

	// State encodes tenantID|accountName|shop — base64 to survive URL encoding
	stateRaw := tenantID + "|" + accountName + "|" + shop
	state := base64.URLEncoding.EncodeToString([]byte(stateRaw))

	consentURL := fmt.Sprintf(
		"https://%s/admin/oauth/authorize?client_id=%s&scope=%s&redirect_uri=%s&state=%s",
		shop,
		url.QueryEscape(clientID),
		url.QueryEscape(shopifyScopes),
		url.QueryEscape(redirectURI),
		url.QueryEscape(state),
	)

	c.JSON(http.StatusOK, gin.H{
		"ok":          true,
		"consent_url": consentURL,
		"message":     "Redirect the user to consent_url to authorise Shopify access",
	})
}

// OAuthCallback handles Shopify's redirect after the merchant grants access.
// GET /shopify/oauth/callback?code=...&hmac=...&shop=...&state=...
func (h *ShopifyHandler) OAuthCallback(c *gin.Context) {
	code := c.Query("code")
	hmacParam := c.Query("hmac")
	shop := c.Query("shop")
	state := c.Query("state")

	clientID := os.Getenv("SHOPIFY_CLIENT_ID")
	clientSecret := os.Getenv("SHOPIFY_CLIENT_SECRET")
	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "https://marketmate.app"
	}

	failHTML := func(msg string) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(fmt.Sprintf(`
		<html><body style="font-family:sans-serif;text-align:center;padding:60px">
			<h2 style="color:#E53238">❌ Shopify Connection Failed</h2>
			<p>%s</p><p>Please close this window and try again.</p>
			<script>
				if (window.opener) {
					window.opener.postMessage({type:'shopify-oauth-error',error:'%s'},'*');
					setTimeout(()=>window.close(),4000);
				}
			</script>
		</body></html>`, msg, msg)))
	}

	if code == "" || shop == "" {
		failHTML("Authorization was denied or the request was invalid.")
		return
	}

	// ── Step 1: Verify HMAC signature from Shopify ─────────────────────
	// Shopify signs the callback params with the client secret.
	// We rebuild the message (all params except hmac, sorted alphabetically)
	// and verify the signature before trusting any data.
	if clientSecret != "" && hmacParam != "" {
		params := c.Request.URL.Query()
		var parts []string
		for k, vs := range params {
			if k == "hmac" {
				continue
			}
			parts = append(parts, k+"="+vs[0])
		}
		// Sort alphabetically
		for i := 0; i < len(parts); i++ {
			for j := i + 1; j < len(parts); j++ {
				if parts[i] > parts[j] {
					parts[i], parts[j] = parts[j], parts[i]
				}
			}
		}
		message := strings.Join(parts, "&")
		mac := hmac.New(sha256.New, []byte(clientSecret))
		mac.Write([]byte(message))
		expected := hex.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(expected), []byte(hmacParam)) {
			log.Printf("[Shopify OAuth] HMAC verification failed for shop %s", shop)
			failHTML("Security verification failed. Please try connecting again.")
			return
		}
	}

	// ── Step 2: Decode state ──────────────────────────────────────────
	stateBytes, _ := base64.URLEncoding.DecodeString(state)
	parts := strings.SplitN(string(stateBytes), "|", 3)
	tenantID := "tenant-demo"
	accountName := shop
	if len(parts) >= 1 && parts[0] != "" {
		tenantID = parts[0]
	}
	if len(parts) >= 2 && parts[1] != "" {
		accountName = parts[1]
	}

	// ── Step 3: Exchange code for permanent access token ──────────────
	tokenPayload := map[string]string{
		"client_id":     clientID,
		"client_secret": clientSecret,
		"code":          code,
	}
	tokenBody, _ := json.Marshal(tokenPayload)

	tokenReq, err := http.NewRequest("POST",
		fmt.Sprintf("https://%s/admin/oauth/access_token", shop),
		strings.NewReader(string(tokenBody)))
	if err != nil {
		failHTML("Failed to build token request.")
		return
	}
	tokenReq.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{Timeout: 15 * time.Second}
	tokenResp, err := httpClient.Do(tokenReq)
	if err != nil {
		log.Printf("[Shopify OAuth] Token exchange HTTP error: %v", err)
		failHTML("Token exchange failed: " + err.Error())
		return
	}
	defer tokenResp.Body.Close()

	var tokenData struct {
		AccessToken string `json:"access_token"`
		Scope       string `json:"scope"`
	}
	if err := json.NewDecoder(tokenResp.Body).Decode(&tokenData); err != nil || tokenData.AccessToken == "" {
		log.Printf("[Shopify OAuth] Token decode error: %v", err)
		failHTML("Could not decode access token from Shopify.")
		return
	}

	log.Printf("[Shopify OAuth] Got access token for shop=%s tenant=%s scopes=%s", shop, tenantID, tokenData.Scope)

	// ── Step 4: Fetch shop info (name, email, currency) ───────────────
	sc := newShopifyClient(shop, tokenData.AccessToken, "")
	shopInfo, _, err := sc.do(c.Request.Context(), "GET", "/shop.json", nil)
	shopName := accountName
	shopCurrency := "GBP"
	shopEmail := ""
	if err == nil {
		if si, ok := shopInfo["shop"].(map[string]interface{}); ok {
			if n, ok := si["name"].(string); ok && n != "" {
				shopName = n
			}
			if cur, ok := si["currency"].(string); ok && cur != "" {
				shopCurrency = cur
			}
			if em, ok := si["email"].(string); ok {
				shopEmail = em
			}
		}
	}

	// ── Step 5: Save or update credential ────────────────────────────
	ctx := c.Request.Context()
	creds, _ := h.repo.ListCredentials(ctx, tenantID)

	// Find existing credential for this specific shop domain (multi-storefront aware)
	var existingCredID string
	for _, cr := range creds {
		if cr.Channel == "shopify" && cr.CredentialData["shop_domain"] == shop {
			existingCredID = cr.CredentialID
			break
		}
	}

	credData := map[string]string{
		"shop_domain":   shop,
		"access_token":  tokenData.AccessToken,
		"scopes":        tokenData.Scope,
		"shop_name":     shopName,
		"shop_currency": shopCurrency,
		"shop_email":    shopEmail,
		"api_version":   "2026-01",
		"client_secret": clientSecret, // stored for webhook HMAC verification
	}

	if existingCredID != "" {
		existing, err := h.repo.GetCredential(ctx, tenantID, existingCredID)
		if err == nil {
			for k, v := range credData {
				existing.CredentialData[k] = v
			}
			existing.AccountName = shopName
			existing.UpdatedAt = time.Now()
			h.repo.SaveCredential(ctx, existing)
			log.Printf("[Shopify OAuth] Updated existing credential %s for shop %s", existingCredID, shop)
		}
	} else {
		credID := fmt.Sprintf("cred-shopify-%d", time.Now().UnixMilli())
		newCred := &models.MarketplaceCredential{
			CredentialID:   credID,
			TenantID:       tenantID,
			Channel:        "shopify",
			AccountName:    shopName,
			Environment:    "production",
			Active:         true,
			CredentialData: credData,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}
		if err := h.repo.SaveCredential(ctx, newCred); err != nil {
			log.Printf("[Shopify OAuth] Failed to save credential: %v", err)
			failHTML("Failed to save credential: " + err.Error())
			return
		}
		log.Printf("[Shopify OAuth] Created new credential %s for shop %s", credID, shop)
	}

	// ── Step 6: Register order webhooks automatically ─────────────────
	go func() {
		bgCtx := context.Background()
		client, cred, err := h.getClient(bgCtx, tenantID, "")
		if err != nil {
			log.Printf("[Shopify OAuth] Could not get client for webhook registration: %v", err)
			return
		}
		h.registerWebhooksForCred(bgCtx, client, cred)
	}()

	// ── Step 7: Return success page ───────────────────────────────────
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(fmt.Sprintf(`
	<html><body style="font-family:sans-serif;text-align:center;padding:60px">
		<h2 style="color:#4CAF50">&#x2705; Shopify Connected Successfully!</h2>
		<p><strong>%s</strong> has been authorised.</p>
		<p style="color:#666;font-size:14px">Email: %s &bull; Currency: %s</p>
		<p style="font-size:13px;color:#888">This window will close automatically…</p>
		<script>
			// Try postMessage immediately and on load
			function notify() {
				try {
					if (window.opener && !window.opener.closed) {
						window.opener.postMessage({type:'shopify-oauth-success',shop:'%s'},'*');
					}
				} catch(e) {}
			}
			notify();
			window.addEventListener('load', notify);
			// Also store in localStorage so parent can poll
			try { localStorage.setItem('shopify-oauth-result', JSON.stringify({type:'shopify-oauth-success',shop:'%s',ts:Date.now()})); } catch(e) {}
			setTimeout(function(){ notify(); window.close(); }, 2000);
		</script>
	</body></html>`, shopName, shopEmail, shopCurrency, shop, shop)))
}

// ============================================================================
// TEST CONNECTION
// ============================================================================

// TestConnection verifies the stored access token is still valid.
// GET /shopify/test?credential_id=...
func (h *ShopifyHandler) TestConnection(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	credentialID := c.Query("credential_id")

	client, cred, err := h.getClient(c.Request.Context(), tenantID, credentialID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	result, _, err := client.do(c.Request.Context(), "GET", "/shop.json", nil)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	shopName := cred.CredentialData["shop_domain"]
	if si, ok := result["shop"].(map[string]interface{}); ok {
		if n, ok := si["name"].(string); ok {
			shopName = n
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":           true,
		"shop":         shopName,
		"shop_domain":  cred.CredentialData["shop_domain"],
		"credential_id": cred.CredentialID,
	})
}

// ============================================================================
// ORDERS — Import
// ============================================================================

// Shopify order structs
type shopifyOrderResp struct {
	ID              int64                    `json:"id"`
	Name            string                   `json:"name"`           // e.g. #1001
	Email           string                   `json:"email"`
	FinancialStatus string                   `json:"financial_status"`
	FulfillmentStatus string                 `json:"fulfillment_status"`
	CreatedAt       string                   `json:"created_at"`
	UpdatedAt       string                   `json:"updated_at"`
	Currency        string                   `json:"currency"`
	TotalPrice      string                   `json:"total_price"`
	Customer        shopifyCustomerResp      `json:"customer"`
	LineItems       []shopifyLineItemResp    `json:"line_items"`
	ShippingAddress shopifyAddressResp       `json:"shipping_address"`
	Fulfillments    []shopifyFulfillmentResp `json:"fulfillments"`
}

type shopifyCustomerResp struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
}

type shopifyLineItemResp struct {
	ID        int64   `json:"id"`
	Title     string  `json:"title"`
	SKU       string  `json:"sku"`
	Quantity  int     `json:"quantity"`
	Price     string  `json:"price"`
	ProductID int64   `json:"product_id"`
	VariantID int64   `json:"variant_id"`
}

type shopifyAddressResp struct {
	Name    string `json:"name"`
	Address1 string `json:"address1"`
	City    string `json:"city"`
	Zip     string `json:"zip"`
	Country string `json:"country"`
}

type shopifyFulfillmentResp struct {
	TrackingNumber  string `json:"tracking_number"`
	TrackingCompany string `json:"tracking_company"`
	Status          string `json:"status"`
}

// ImportShopifyOrders is the service-layer import called by the OrderPoller and
// OrderWebhook. It matches the (ctx, tenantID, credID, from, to) → (int, error)
// signature expected by processChannelImport.
func (h *ShopifyHandler) ImportShopifyOrders(ctx context.Context, tenantID, credentialID string, createdAfter, createdBefore time.Time) (int, error) {
	client, cred, err := h.getClient(ctx, tenantID, credentialID)
	if err != nil {
		return 0, fmt.Errorf("get client: %w", err)
	}

	// Shopify uses ISO 8601 timestamps for created_at_min/max
	queryURL := fmt.Sprintf("/orders.json?status=any&limit=250&created_at_min=%s&created_at_max=%s",
		url.QueryEscape(createdAfter.Format(time.RFC3339)),
		url.QueryEscape(createdBefore.Format(time.RFC3339)),
	)

	result, _, err := client.do(ctx, "GET", queryURL, nil)
	if err != nil {
		return 0, fmt.Errorf("fetch orders: %w", err)
	}

	ordersRaw, _ := result["orders"].([]interface{})
	ordersJSON, _ := json.Marshal(ordersRaw)
	var orders []shopifyOrderResp
	if err := json.Unmarshal(ordersJSON, &orders); err != nil {
		return 0, fmt.Errorf("parse orders: %w", err)
	}

	imported := 0
	currency := cred.CredentialData["shop_currency"]
	if currency == "" {
		currency = "GBP"
	}

	for _, so := range orders {
		mmOrder := mapShopifyOrder(so, tenantID, cred.CredentialID)
		orderID, isNew, err := h.orderService.CreateOrder(ctx, tenantID, mmOrder)
		if err != nil {
			log.Printf("[Shopify Orders] Failed to save order %d: %v", so.ID, err)
			continue
		}
		if !isNew {
			continue
		}
		oc := so.Currency
		if oc == "" {
			oc = currency
		}
		for _, item := range so.LineItems {
			price, _ := strconv.ParseFloat(item.Price, 64)
			line := &models.OrderLine{
				LineID:    strconv.FormatInt(item.ID, 10),
				ProductID: strconv.FormatInt(item.ProductID, 10),
				SKU:       item.SKU,
				Title:     item.Title,
				Quantity:  item.Quantity,
				UnitPrice: models.Money{Amount: price, Currency: oc},
				LineTotal: models.Money{Amount: price * float64(item.Quantity), Currency: oc},
			}
			if err := h.orderService.CreateOrderLine(ctx, tenantID, orderID, line); err != nil {
				log.Printf("[Shopify Orders] Failed to save line for order %d: %v", so.ID, err)
			}
		}
		imported++
	}

	log.Printf("[Shopify Orders] Imported %d/%d orders for tenant=%s cred=%s", imported, len(orders), tenantID, credentialID)
	return imported, nil
}

// ImportOrders pulls orders from Shopify and saves them to MarketMate.
// POST /shopify/orders/import
// Body: { "credential_id": "...", "status": "any|open|closed", "limit": 50, "created_at_min": "2025-01-01" }
func (h *ShopifyHandler) ImportOrders(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req struct {
		CredentialID string `json:"credential_id"`
		Status       string `json:"status"`
		Limit        int    `json:"limit"`
		CreatedAtMin string `json:"created_at_min"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	client, cred, err := h.getClient(c.Request.Context(), tenantID, req.CredentialID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}
	status := req.Status
	if status == "" {
		status = "open"
	}

	queryURL := fmt.Sprintf("/orders.json?status=%s&limit=%d", status, limit)
	if req.CreatedAtMin != "" {
		queryURL += "&created_at_min=" + url.QueryEscape(req.CreatedAtMin)
	}

	result, _, err := client.do(c.Request.Context(), "GET", queryURL, nil)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": fmt.Sprintf("fetch orders: %v", err)})
		return
	}

	ordersRaw, _ := result["orders"].([]interface{})
	ordersJSON, _ := json.Marshal(ordersRaw)
	var orders []shopifyOrderResp
	if err := json.Unmarshal(ordersJSON, &orders); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": "failed to parse orders"})
		return
	}

	imported := 0
	var importErrors []string

	for _, so := range orders {
		mmOrder := mapShopifyOrder(so, tenantID, cred.CredentialID)

		orderID, isNew, err := h.orderService.CreateOrder(c.Request.Context(), tenantID, mmOrder)
		if err != nil {
			importErrors = append(importErrors, fmt.Sprintf("order %d: %v", so.ID, err))
			continue
		}
		if !isNew {
			continue
		}

		currency := so.Currency
		if currency == "" {
			currency = cred.CredentialData["shop_currency"]
		}
		if currency == "" {
			currency = "GBP"
		}

		for _, item := range so.LineItems {
			price, _ := strconv.ParseFloat(item.Price, 64)
			line := &models.OrderLine{
				LineID:    strconv.FormatInt(item.ID, 10),
				ProductID: strconv.FormatInt(item.ProductID, 10),
				SKU:       item.SKU,
				Title:     item.Title,
				Quantity:  item.Quantity,
				UnitPrice: models.Money{Amount: price, Currency: currency},
				LineTotal: models.Money{Amount: price * float64(item.Quantity), Currency: currency},
			}
			if err := h.orderService.CreateOrderLine(c.Request.Context(), tenantID, orderID, line); err != nil {
				log.Printf("[Shopify Orders] Failed to save line for order %d: %v", so.ID, err)
			}
		}

		imported++
		log.Printf("[Shopify Orders] Imported order %s (%d) → %s", so.Name, so.ID, orderID)
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":       true,
		"imported": imported,
		"total":    len(orders),
		"errors":   importErrors,
	})
}

// GetOrders lists Shopify orders stored in Firestore.
// GET /shopify/orders?credential_id=...&limit=50
func (h *ShopifyHandler) GetOrders(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	_ = c.Query("credential_id") // reserved for future per-store filtering
	limitStr := c.DefaultQuery("limit", "50")
	limit, _ := strconv.Atoi(limitStr)
	if limit <= 0 {
		limit = 50
	}

	opts := services.OrderListOptions{
		Channel: "shopify",
		Limit:   fmt.Sprintf("%d", limit),
	}
	orders, _, err := h.orderService.ListOrders(c.Request.Context(), tenantID, opts)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "orders": orders, "count": len(orders)})
}

// MarkShipped pushes tracking info to Shopify and creates a fulfillment.
// POST /shopify/orders/:id/ship
// Body: { "credential_id": "...", "tracking_number": "...", "tracking_company": "..." }
func (h *ShopifyHandler) MarkShipped(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	orderID := c.Param("id") // MarketMate order ID

	var req struct {
		CredentialID    string `json:"credential_id"`
		TrackingNumber  string `json:"tracking_number"`
		TrackingCompany string `json:"tracking_company"`
		NotifyCustomer  bool   `json:"notify_customer"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	// Load order to get external Shopify order ID
	order, err := h.orderService.GetOrder(c.Request.Context(), tenantID, orderID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": fmt.Sprintf("order not found: %v", err)})
		return
	}

	client, _, err := h.getClient(c.Request.Context(), tenantID, req.CredentialID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	// Get fulfillment orders first (required in Shopify API 2022-01+)
	foResult, _, err := client.do(c.Request.Context(), "GET",
		fmt.Sprintf("/orders/%s/fulfillment_orders.json", order.ExternalOrderID), nil)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": fmt.Sprintf("get fulfillment orders: %v", err)})
		return
	}

	var foIDs []int64
	if fos, ok := foResult["fulfillment_orders"].([]interface{}); ok {
		for _, fo := range fos {
			if fom, ok := fo.(map[string]interface{}); ok {
				if id, ok := fom["id"].(float64); ok {
					foIDs = append(foIDs, int64(id))
				}
			}
		}
	}
	if len(foIDs) == 0 {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": "no fulfillment orders found for this order"})
		return
	}

	// Build fulfillment items list
	var foItems []map[string]interface{}
	for _, foID := range foIDs {
		foItems = append(foItems, map[string]interface{}{
			"fulfillment_order_id": foID,
		})
	}

	fulfillmentPayload := map[string]interface{}{
		"fulfillment": map[string]interface{}{
			"line_items_by_fulfillment_order": foItems,
			"notify_customer": req.NotifyCustomer,
			"tracking_info": map[string]interface{}{
				"number":  req.TrackingNumber,
				"company": req.TrackingCompany,
			},
		},
	}

	_, _, err = client.do(c.Request.Context(), "POST", "/fulfillments.json", fulfillmentPayload)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": fmt.Sprintf("create fulfillment: %v", err)})
		return
	}

	// Update MarketMate order status to shipped
	if err := h.orderService.UpdateOrderStatus(c.Request.Context(), tenantID, orderID, "shipped", "", ""); err != nil {
		log.Printf("[Shopify Orders] Failed to update order status in Firestore: %v", err)
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "Fulfillment created successfully"})
}

// ============================================================================
// STOCK SYNC
// ============================================================================

// UpdateStock updates Shopify inventory by inventory_item_id + location.
// POST /shopify/stock
// Body: { "credential_id":"...", "inventory_item_id": 12345, "quantity": 10, "location_id": 67890 }
// If location_id is omitted, the first location is used automatically.
func (h *ShopifyHandler) UpdateStock(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req struct {
		CredentialID    string `json:"credential_id"`
		InventoryItemID int64  `json:"inventory_item_id"`
		SKU             string `json:"sku"`      // alternative to inventory_item_id
		ProductID       string `json:"product_id"` // alternative: Shopify product ID
		Quantity        int    `json:"quantity"`
		LocationID      int64  `json:"location_id"` // optional; auto-resolved if blank
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	client, _, err := h.getClient(c.Request.Context(), tenantID, req.CredentialID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	// Resolve inventory_item_id from SKU if needed
	inventoryItemID := req.InventoryItemID
	if inventoryItemID == 0 && req.SKU != "" {
		varResult, _, err := client.do(c.Request.Context(), "GET",
			fmt.Sprintf("/variants.json?fields=id,sku,inventory_item_id"), nil)
		if err == nil {
			if variants, ok := varResult["variants"].([]interface{}); ok {
				for _, v := range variants {
					if vm, ok := v.(map[string]interface{}); ok {
						if sku, _ := vm["sku"].(string); sku == req.SKU {
							if iid, ok := vm["inventory_item_id"].(float64); ok {
								inventoryItemID = int64(iid)
								break
							}
						}
					}
				}
			}
		}
	}
	if inventoryItemID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "inventory_item_id or a valid sku is required"})
		return
	}

	// Resolve location_id
	locationID := req.LocationID
	if locationID == 0 {
		locResult, _, err := client.do(c.Request.Context(), "GET", "/locations.json", nil)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"ok": false, "error": fmt.Sprintf("get locations: %v", err)})
			return
		}
		if locs, ok := locResult["locations"].([]interface{}); ok && len(locs) > 0 {
			if lm, ok := locs[0].(map[string]interface{}); ok {
				if id, ok := lm["id"].(float64); ok {
					locationID = int64(id)
				}
			}
		}
	}
	if locationID == 0 {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": "could not determine Shopify location"})
		return
	}

	body := map[string]interface{}{
		"location_id":       locationID,
		"inventory_item_id": inventoryItemID,
		"available":         req.Quantity,
	}
	_, _, err = client.do(c.Request.Context(), "POST", "/inventory_levels/set.json", body)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": fmt.Sprintf("set inventory level: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":                true,
		"inventory_item_id": inventoryItemID,
		"location_id":       locationID,
		"quantity":          req.Quantity,
	})
}

// ============================================================================
// WEBHOOKS
// ============================================================================

// RegisterWebhooks registers Shopify order webhooks for the given store.
// POST /shopify/webhooks/register
// Body: { "credential_id": "..." }
func (h *ShopifyHandler) RegisterWebhooks(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req struct {
		CredentialID string `json:"credential_id"`
	}
	c.ShouldBindJSON(&req)

	client, cred, err := h.getClient(c.Request.Context(), tenantID, req.CredentialID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	registered := h.registerWebhooksForCred(c.Request.Context(), client, cred)
	c.JSON(http.StatusOK, gin.H{"ok": true, "registered": registered})
}

func (h *ShopifyHandler) registerWebhooksForCred(ctx context.Context, client *shopifyClient, cred *models.MarketplaceCredential) []string {
	apiBaseURL := os.Getenv("API_BASE_URL")
	if apiBaseURL == "" {
		apiBaseURL = "https://marketmate-api-487246736287.europe-west2.run.app"
	}

	baseURL := fmt.Sprintf("%s/webhooks/orders/shopify?tenant=%s&cred=%s",
		apiBaseURL, cred.TenantID, cred.CredentialID)

	// GraphQL topic names (REST topics use "/" but GraphQL uses "_" and uppercase)
	type webhookSub struct {
		topic    string // GraphQL enum e.g. ORDERS_CREATE
		urlParam string // appended to baseURL for routing
	}
	subs := []webhookSub{
		{"ORDERS_CREATE", "orders%2Fcreate"},
		{"ORDERS_UPDATED", "orders%2Fupdated"},
		{"ORDERS_FULFILLED", "orders%2Ffulfilled"},
		{"ORDERS_CANCELLED", "orders%2Fcancelled"},
	}

	var registered []string
	for _, sub := range subs {
		callbackURL := baseURL + "&topic=" + sub.urlParam

		// Use GraphQL webhookSubscriptionCreate mutation
		// This works on all API versions including 2026-01
		query := fmt.Sprintf(`mutation {
			webhookSubscriptionCreate(
				topic: %s
				webhookSubscription: {
					callbackUrl: "%s"
					format: JSON
				}
			) {
				webhookSubscription { id }
				userErrors { field message }
			}
		}`, sub.topic, callbackURL)

		gqlPayload := map[string]interface{}{"query": query}
		gqlURL := fmt.Sprintf("https://%s/admin/api/%s/graphql.json", client.shopDomain, client.apiVersion)

		body, _ := json.Marshal(gqlPayload)
		req, err := http.NewRequestWithContext(ctx, "POST", gqlURL, strings.NewReader(string(body)))
		if err != nil {
			log.Printf("[Shopify Webhooks] Failed to build request for %s: %v", sub.topic, err)
			continue
		}
		req.Header.Set("X-Shopify-Access-Token", client.accessToken)
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.httpClient.Do(req)
		if err != nil {
			log.Printf("[Shopify Webhooks] HTTP error for %s: %v", sub.topic, err)
			continue
		}
		resp.Body.Close()

		registered = append(registered, sub.topic)
		log.Printf("[Shopify Webhooks] Registered via GraphQL: %s → %s", sub.topic, callbackURL)
	}
	return registered
}

// ============================================================================
// HELPERS
// ============================================================================

func mapShopifyOrder(so shopifyOrderResp, tenantID, credentialID string) *models.Order {
	now := time.Now().UTC().Format(time.RFC3339)

	customerName := strings.TrimSpace(so.Customer.FirstName + " " + so.Customer.LastName)
	if customerName == "" {
		customerName = so.ShippingAddress.Name
	}
	email := so.Email
	if email == "" {
		email = so.Customer.Email
	}

	totalPrice, _ := strconv.ParseFloat(so.TotalPrice, 64)
	currency := so.Currency
	if currency == "" {
		currency = "GBP"
	}

	trackingNumber := ""
	if len(so.Fulfillments) > 0 {
		trackingNumber = so.Fulfillments[0].TrackingNumber
	}

	orderDate := so.CreatedAt
	if orderDate == "" {
		orderDate = now
	}

	return &models.Order{
		OrderID:          fmt.Sprintf("shopify-%d", so.ID),
		TenantID:         tenantID,
		Channel:          "shopify",
		ChannelAccountID: credentialID,
		ExternalOrderID:  strconv.FormatInt(so.ID, 10),
		Status:           normaliseShopifyStatus(so.FinancialStatus, so.FulfillmentStatus),
		Customer: models.Customer{
			Name:  customerName,
			Email: email,
		},
		TrackingNumber: trackingNumber,
		Totals: models.OrderTotals{
			GrandTotal: models.Money{Amount: totalPrice, Currency: currency},
		},
		OrderDate:  orderDate,
		CreatedAt:  now,
		UpdatedAt:  now,
		ImportedAt: now,
	}
}

func normaliseShopifyStatus(financial, fulfillment string) string {
	if fulfillment == "fulfilled" {
		return "shipped"
	}
	switch financial {
	case "paid":
		return "processing"
	case "pending":
		return "pending"
	case "refunded", "voided":
		return "cancelled"
	default:
		return "pending"
	}
}
