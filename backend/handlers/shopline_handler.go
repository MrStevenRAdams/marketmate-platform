package handlers

// ============================================================================
// SHOPLINE HANDLER
// ============================================================================
// OAuth 2.0 flow + credential management + orders + stock + webhooks.
//
// Shopline uses OAuth 2.0 with partner app credentials.
// Docs: https://open.shopline.io/
//
// OAuth flow (public app):
//   GET  /shopline/oauth/login      → returns consent_url, state encodes tenant|account|shop
//   GET  /shopline/oauth/callback   → Shopline redirects here; exchanges code for token; saves credential
//
// Connection:
//   GET  /shopline/test             → verify access token still valid
//
// Orders:
//   POST /shopline/orders/import    → pull orders from Shopline into MarketMate
//   GET  /shopline/orders           → list orders from Firestore
//   POST /shopline/orders/:id/ship  → push tracking to Shopline
//
// Stock:
//   POST /shopline/stock            → update inventory level by SKU / variant_id
//
// Webhooks:
//   POST /shopline/webhooks/register → register order webhooks on the Shopline store
//
// Multi-storefront:
//   Each connected Shopline store is a separate MarketplaceCredential (one per shop_id).
//   credential_id query/body param selects which store; falls back to first active shopline cred.
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
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"module-a/models"
	"module-a/repository"
	"module-a/services"
)

// ── Handler struct ─────────────────────────────────────────────────────────

type ShoplineHandler struct {
	marketplaceService *services.MarketplaceService
	repo               *repository.MarketplaceRepository
	orderService       *services.OrderService
	productRepo        *repository.FirestoreRepository
}

func NewShoplineHandler(
	marketplaceService *services.MarketplaceService,
	repo *repository.MarketplaceRepository,
	orderService *services.OrderService,
	productRepo *repository.FirestoreRepository,
) *ShoplineHandler {
	return &ShoplineHandler{
		marketplaceService: marketplaceService,
		repo:               repo,
		orderService:       orderService,
		productRepo:        productRepo,
	}
}

// ── Internal Shopline API client ───────────────────────────────────────────
// Shopline's Open API base: https://open.shopline.io/api/v2/{shop_id}/...
// Auth header: Authorization: Bearer {access_token}

type shoplineClient struct {
	shopID      string
	accessToken string
	apiVersion  string
	httpClient  *http.Client
}

func newShoplineClient(shopID, accessToken, apiVersion string) *shoplineClient {
	if apiVersion == "" {
		apiVersion = "v2"
	}
	return &shoplineClient{
		shopID:      shopID,
		accessToken: accessToken,
		apiVersion:  apiVersion,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (s *shoplineClient) url(path string) string {
	return fmt.Sprintf("https://open.shopline.io/api/%s/%s%s", s.apiVersion, s.shopID, path)
}

func (s *shoplineClient) do(ctx context.Context, method, path string, body interface{}) (map[string]interface{}, int, error) {
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
	req.Header.Set("Authorization", "Bearer "+s.accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	if resp.StatusCode >= 400 {
		msg, _ := json.Marshal(result)
		return result, resp.StatusCode, fmt.Errorf("shopline API %d: %s", resp.StatusCode, string(msg))
	}
	return result, resp.StatusCode, nil
}

// ── Credential resolution ──────────────────────────────────────────────────

func (h *ShoplineHandler) getClient(ctx context.Context, tenantID, credentialID string) (*shoplineClient, *models.MarketplaceCredential, error) {
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
			if c.Channel == "shopline" && c.Active {
				cred = &creds[i]
				break
			}
		}
	}
	if cred == nil {
		return nil, nil, fmt.Errorf("no active Shopline credential found — please connect a Shopline store first")
	}

	shopID := cred.CredentialData["shop_id"]
	accessToken := cred.CredentialData["access_token"]
	apiVersion := cred.CredentialData["api_version"]
	if shopID == "" || accessToken == "" {
		return nil, nil, fmt.Errorf("shopline credential missing shop_id or access_token")
	}

	return newShoplineClient(shopID, accessToken, apiVersion), cred, nil
}

// ============================================================================
// OAUTH — Public App flow
// ============================================================================
// Required env vars:
//   SHOPLINE_CLIENT_ID     — from Shopline Partner Platform
//   SHOPLINE_CLIENT_SECRET — from Shopline Partner Platform
//   SHOPLINE_REDIRECT_URI  — must match registered URI
//                            e.g. https://marketmate-api-xxx.run.app/api/v1/shopline/oauth/callback
//
// Scopes requested:
//   read_products,write_products,read_orders,write_orders,
//   read_inventory,write_inventory,read_fulfillments,write_fulfillments
// ============================================================================

const shoplineScopes = "read_products,write_products,read_orders,write_orders,read_inventory,write_inventory,read_fulfillments,write_fulfillments"

// OAuthLogin returns the Shopline consent URL for the given store.
// GET /shopline/oauth/login?shop=mystore&account_name=My+Store
// shop param accepts either a shop ID or a myshopline.com subdomain.
func (h *ShoplineHandler) OAuthLogin(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	shop := c.Query("shop")
	accountName := c.Query("account_name")

	if shop == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "shop parameter is required (Shopline shop ID or subdomain)"})
		return
	}

	// Normalise — strip https:// and trailing slashes if user pastes full URL
	shop = strings.TrimPrefix(shop, "https://")
	shop = strings.TrimPrefix(shop, "http://")
	shop = strings.TrimSuffix(shop, "/")
	// Strip .myshopline.com if provided as full domain
	shop = strings.TrimSuffix(shop, ".myshopline.com")

	clientID := os.Getenv("SHOPLINE_CLIENT_ID")
	redirectURI := os.Getenv("SHOPLINE_REDIRECT_URI")

	if clientID == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "SHOPLINE_CLIENT_ID not configured"})
		return
	}
	if redirectURI == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "SHOPLINE_REDIRECT_URI not configured"})
		return
	}
	if accountName == "" {
		accountName = shop
	}

	// State encodes tenantID|accountName|shop — base64 to survive URL encoding
	stateRaw := tenantID + "|" + accountName + "|" + shop
	state := base64.URLEncoding.EncodeToString([]byte(stateRaw))

	// Shopline OAuth consent URL
	// See: https://open.shopline.io/docs/oauth
	consentURL := fmt.Sprintf(
		"https://open.shopline.io/oauth/authorize?client_id=%s&scope=%s&redirect_uri=%s&state=%s&shop=%s",
		url.QueryEscape(clientID),
		url.QueryEscape(shoplineScopes),
		url.QueryEscape(redirectURI),
		url.QueryEscape(state),
		url.QueryEscape(shop),
	)

	c.JSON(http.StatusOK, gin.H{
		"ok":          true,
		"consent_url": consentURL,
		"message":     "Redirect the user to consent_url to authorise Shopline access",
	})
}

// OAuthCallback handles Shopline's redirect after the merchant grants access.
// GET /shopline/oauth/callback?code=...&shop=...&state=...
func (h *ShoplineHandler) OAuthCallback(c *gin.Context) {
	code := c.Query("code")
	shop := c.Query("shop")
	state := c.Query("state")

	clientID := os.Getenv("SHOPLINE_CLIENT_ID")
	clientSecret := os.Getenv("SHOPLINE_CLIENT_SECRET")
	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "https://marketmate.app"
	}

	failHTML := func(msg string) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(fmt.Sprintf(`
		<html><body style="font-family:sans-serif;text-align:center;padding:60px">
			<h2 style="color:#E53238">❌ Shopline Connection Failed</h2>
			<p>%s</p><p>Please close this window and try again.</p>
			<script>
				if (window.opener) {
					window.opener.postMessage({type:'shopline-oauth-error',error:'%s'},'*');
					setTimeout(()=>window.close(),4000);
				}
			</script>
		</body></html>`, msg, msg)))
	}

	if code == "" || shop == "" {
		failHTML("Authorization was denied or the request was invalid.")
		return
	}

	// ── Step 1: Decode state ──────────────────────────────────────────
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

	// ── Step 2: Exchange code for access token ────────────────────────
	tokenPayload := map[string]string{
		"client_id":     clientID,
		"client_secret": clientSecret,
		"code":          code,
		"grant_type":    "authorization_code",
		"redirect_uri":  os.Getenv("SHOPLINE_REDIRECT_URI"),
	}
	tokenBody, _ := json.Marshal(tokenPayload)

	tokenReq, err := http.NewRequest("POST",
		"https://open.shopline.io/oauth/token",
		strings.NewReader(string(tokenBody)))
	if err != nil {
		failHTML("Failed to build token request.")
		return
	}
	tokenReq.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{Timeout: 15 * time.Second}
	tokenResp, err := httpClient.Do(tokenReq)
	if err != nil {
		log.Printf("[Shopline OAuth] Token exchange HTTP error: %v", err)
		failHTML("Token exchange failed: " + err.Error())
		return
	}
	defer tokenResp.Body.Close()

	var tokenData struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Scope        string `json:"scope"`
		ShopID       string `json:"shop_id"`
	}
	if err := json.NewDecoder(tokenResp.Body).Decode(&tokenData); err != nil || tokenData.AccessToken == "" {
		log.Printf("[Shopline OAuth] Token decode error: %v", err)
		failHTML("Could not decode access token from Shopline.")
		return
	}

	// Use shop_id from token response if available (more reliable)
	shopID := tokenData.ShopID
	if shopID == "" {
		shopID = shop
	}

	log.Printf("[Shopline OAuth] Got access token for shop=%s tenant=%s scopes=%s", shopID, tenantID, tokenData.Scope)

	// ── Step 3: Fetch shop info ───────────────────────────────────────
	sc := newShoplineClient(shopID, tokenData.AccessToken, "")
	shopInfo, _, err := sc.do(c.Request.Context(), "GET", "/shop.json", nil)
	shopName := accountName
	shopCurrency := "CNY"
	shopEmail := ""
	shopDomain := shopID + ".myshopline.com"

	if err == nil {
		// Shopline returns { "shop": { "name": "...", "email": "...", "currency": "...", "domain": "..." } }
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
			if d, ok := si["domain"].(string); ok && d != "" {
				shopDomain = d
			}
		}
	}

	// ── Step 4: Save or update credential ────────────────────────────
	ctx := c.Request.Context()
	creds, _ := h.repo.ListCredentials(ctx, tenantID)

	var existingCredID string
	for _, cr := range creds {
		if cr.Channel == "shopline" && cr.CredentialData["shop_id"] == shopID {
			existingCredID = cr.CredentialID
			break
		}
	}

	credData := map[string]string{
		"shop_id":       shopID,
		"shop_domain":   shopDomain,
		"access_token":  tokenData.AccessToken,
		"refresh_token": tokenData.RefreshToken,
		"scopes":        tokenData.Scope,
		"shop_name":     shopName,
		"shop_currency": shopCurrency,
		"shop_email":    shopEmail,
		"api_version":   "v2",
		"client_secret": clientSecret,
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
			log.Printf("[Shopline OAuth] Updated existing credential %s for shop %s", existingCredID, shopID)
		}
	} else {
		credID := fmt.Sprintf("cred-shopline-%d", time.Now().UnixMilli())
		newCred := &models.MarketplaceCredential{
			CredentialID:   credID,
			TenantID:       tenantID,
			Channel:        "shopline",
			AccountName:    shopName,
			Environment:    "production",
			Active:         true,
			CredentialData: credData,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}
		if err := h.repo.SaveCredential(ctx, newCred); err != nil {
			log.Printf("[Shopline OAuth] Failed to save credential: %v", err)
			failHTML("Failed to save credential: " + err.Error())
			return
		}
		log.Printf("[Shopline OAuth] Created new credential %s for shop %s", credID, shopID)
	}

	// ── Step 5: Register order webhooks automatically ─────────────────
	go func() {
		bgCtx := context.Background()
		client, cred, err := h.getClient(bgCtx, tenantID, "")
		if err != nil {
			log.Printf("[Shopline OAuth] Could not get client for webhook registration: %v", err)
			return
		}
		h.registerWebhooksForCred(bgCtx, client, cred)
	}()

	// ── Step 6: Return success page ───────────────────────────────────
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(fmt.Sprintf(`
	<html><body style="font-family:sans-serif;text-align:center;padding:60px">
		<h2 style="color:#4CAF50">&#x2705; Shopline Connected Successfully!</h2>
		<p><strong>%s</strong> has been authorised.</p>
		<p style="color:#666;font-size:14px">Email: %s &bull; Currency: %s</p>
		<p style="font-size:13px;color:#888">This window will close automatically…</p>
		<script>
			function notify() {
				try {
					if (window.opener && !window.opener.closed) {
						window.opener.postMessage({type:'shopline-oauth-success',shop:'%s'},'*');
					}
				} catch(e) {}
			}
			notify();
			window.addEventListener('load', notify);
			try { localStorage.setItem('shopline-oauth-result', JSON.stringify({type:'shopline-oauth-success',shop:'%s',ts:Date.now()})); } catch(e) {}
			setTimeout(function(){ notify(); window.close(); }, 2000);
		</script>
	</body></html>`, shopName, shopEmail, shopCurrency, shopID, shopID)))
}

// ============================================================================
// TEST CONNECTION
// ============================================================================

// TestConnection verifies the stored access token is still valid.
// GET /shopline/test?credential_id=...
func (h *ShoplineHandler) TestConnection(c *gin.Context) {
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

	shopName := cred.CredentialData["shop_id"]
	if si, ok := result["shop"].(map[string]interface{}); ok {
		if n, ok := si["name"].(string); ok {
			shopName = n
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":            true,
		"shop":          shopName,
		"shop_id":       cred.CredentialData["shop_id"],
		"shop_domain":   cred.CredentialData["shop_domain"],
		"credential_id": cred.CredentialID,
	})
}

// ============================================================================
// ORDERS — Import
// ============================================================================

// Shopline order structs — mirrors Shopline Open API v2 order schema
type shoplineOrderResp struct {
	ID              string                    `json:"id"`
	OrderNumber     string                    `json:"order_number"`
	Email           string                    `json:"email"`
	FinancialStatus string                    `json:"financial_status"`
	FulfillStatus   string                    `json:"fulfill_status"`
	CreatedAt       string                    `json:"created_at"`
	UpdatedAt       string                    `json:"updated_at"`
	Currency        string                    `json:"currency"`
	TotalPrice      string                    `json:"total_price"`
	Customer        shoplineCustomerResp      `json:"customer"`
	LineItems       []shoplineLineItemResp    `json:"line_items"`
	ShippingAddress shoplineAddressResp       `json:"shipping_address"`
	Fulfillments    []shoplineFulfillmentResp `json:"fulfillments"`
}

type shoplineCustomerResp struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
	Phone     string `json:"phone"`
}

type shoplineLineItemResp struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	SKU       string  `json:"sku"`
	Quantity  int     `json:"quantity"`
	Price     string  `json:"price"`
	ProductID string  `json:"product_id"`
	VariantID string  `json:"variant_id"`
}

type shoplineAddressResp struct {
	Name     string `json:"name"`
	Address1 string `json:"address1"`
	City     string `json:"city"`
	Zip      string `json:"zip"`
	Country  string `json:"country"`
	Phone    string `json:"phone"`
}

type shoplineFulfillmentResp struct {
	TrackingNumber  string `json:"tracking_number"`
	TrackingCompany string `json:"tracking_company"`
	Status          string `json:"status"`
}

// ImportShoplineOrders is the service-layer import called by the OrderPoller.
// Matches the (ctx, tenantID, credID, from, to) → (int, error) signature.
func (h *ShoplineHandler) ImportShoplineOrders(ctx context.Context, tenantID, credentialID string, createdAfter, createdBefore time.Time) (int, error) {
	client, cred, err := h.getClient(ctx, tenantID, credentialID)
	if err != nil {
		return 0, fmt.Errorf("get client: %w", err)
	}

	// Shopline uses Unix timestamps for time filtering
	queryURL := fmt.Sprintf("/orders.json?status=any&limit=250&created_at_min=%d&created_at_max=%d",
		createdAfter.Unix(), createdBefore.Unix())

	result, _, err := client.do(ctx, "GET", queryURL, nil)
	if err != nil {
		return 0, fmt.Errorf("fetch orders: %w", err)
	}

	ordersRaw, _ := result["orders"].([]interface{})
	ordersJSON, _ := json.Marshal(ordersRaw)
	var orders []shoplineOrderResp
	if err := json.Unmarshal(ordersJSON, &orders); err != nil {
		return 0, fmt.Errorf("parse orders: %w", err)
	}

	imported := 0
	currency := cred.CredentialData["shop_currency"]
	if currency == "" {
		currency = "CNY"
	}

	for _, so := range orders {
		mmOrder := mapShoplineOrder(so, tenantID, cred.CredentialID)
		orderID, isNew, err := h.orderService.CreateOrder(ctx, tenantID, mmOrder)
		if err != nil {
			log.Printf("[Shopline Orders] Failed to save order %s: %v", so.ID, err)
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
				LineID:    item.ID,
				ProductID: item.ProductID,
				SKU:       item.SKU,
				Title:     item.Title,
				Quantity:  item.Quantity,
				UnitPrice: models.Money{Amount: price, Currency: oc},
				LineTotal: models.Money{Amount: price * float64(item.Quantity), Currency: oc},
			}
			if err := h.orderService.CreateOrderLine(ctx, tenantID, orderID, line); err != nil {
				log.Printf("[Shopline Orders] Failed to save line for order %s: %v", so.ID, err)
			}
		}
		imported++
	}

	log.Printf("[Shopline Orders] Imported %d/%d orders for tenant=%s cred=%s", imported, len(orders), tenantID, credentialID)
	return imported, nil
}

// ImportOrders pulls orders from Shopline and saves them to MarketMate.
// POST /shopline/orders/import
// Body: { "credential_id": "...", "status": "any|open|closed", "limit": 50, "created_at_min": "2025-01-01" }
func (h *ShoplineHandler) ImportOrders(c *gin.Context) {
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
		// Parse and convert to Unix timestamp
		if t, err := time.Parse("2006-01-02", req.CreatedAtMin); err == nil {
			queryURL += fmt.Sprintf("&created_at_min=%d", t.Unix())
		} else {
			queryURL += "&created_at_min=" + url.QueryEscape(req.CreatedAtMin)
		}
	}

	result, _, err := client.do(c.Request.Context(), "GET", queryURL, nil)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": fmt.Sprintf("fetch orders: %v", err)})
		return
	}

	ordersRaw, _ := result["orders"].([]interface{})
	ordersJSON, _ := json.Marshal(ordersRaw)
	var orders []shoplineOrderResp
	if err := json.Unmarshal(ordersJSON, &orders); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": "failed to parse orders"})
		return
	}

	imported := 0
	var importErrors []string

	for _, so := range orders {
		mmOrder := mapShoplineOrder(so, tenantID, cred.CredentialID)

		orderID, isNew, err := h.orderService.CreateOrder(c.Request.Context(), tenantID, mmOrder)
		if err != nil {
			importErrors = append(importErrors, fmt.Sprintf("order %s: %v", so.ID, err))
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
			currency = "CNY"
		}

		for _, item := range so.LineItems {
			price, _ := strconv.ParseFloat(item.Price, 64)
			line := &models.OrderLine{
				LineID:    item.ID,
				ProductID: item.ProductID,
				SKU:       item.SKU,
				Title:     item.Title,
				Quantity:  item.Quantity,
				UnitPrice: models.Money{Amount: price, Currency: currency},
				LineTotal: models.Money{Amount: price * float64(item.Quantity), Currency: currency},
			}
			if err := h.orderService.CreateOrderLine(c.Request.Context(), tenantID, orderID, line); err != nil {
				log.Printf("[Shopline Orders] Failed to save line for order %s: %v", so.ID, err)
			}
		}

		imported++
		log.Printf("[Shopline Orders] Imported order %s (%s) → %s", so.OrderNumber, so.ID, orderID)
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":       true,
		"imported": imported,
		"total":    len(orders),
		"errors":   importErrors,
	})
}

// GetOrders lists Shopline orders stored in Firestore.
// GET /shopline/orders?credential_id=...&limit=50
func (h *ShoplineHandler) GetOrders(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	limitStr := c.DefaultQuery("limit", "50")
	limit, _ := strconv.Atoi(limitStr)
	if limit <= 0 {
		limit = 50
	}

	opts := services.OrderListOptions{
		Channel: "shopline",
		Limit:   fmt.Sprintf("%d", limit),
	}
	orders, _, err := h.orderService.ListOrders(c.Request.Context(), tenantID, opts)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "orders": orders, "count": len(orders)})
}

// MarkShipped pushes tracking info to Shopline and creates a fulfillment.
// POST /shopline/orders/:id/ship
// Body: { "credential_id": "...", "tracking_number": "...", "tracking_company": "..." }
func (h *ShoplineHandler) MarkShipped(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	orderID := c.Param("id")

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

	// Shopline fulfillment creation
	fulfillmentPayload := map[string]interface{}{
		"fulfillment": map[string]interface{}{
			"tracking_number":  req.TrackingNumber,
			"tracking_company": req.TrackingCompany,
			"notify_customer":  req.NotifyCustomer,
		},
	}

	_, _, err = client.do(c.Request.Context(), "POST",
		fmt.Sprintf("/orders/%s/fulfillments.json", order.ExternalOrderID),
		fulfillmentPayload)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": fmt.Sprintf("create fulfillment: %v", err)})
		return
	}

	if err := h.orderService.UpdateOrderStatus(c.Request.Context(), tenantID, orderID, "shipped", "", ""); err != nil {
		log.Printf("[Shopline Orders] Failed to update order status in Firestore: %v", err)
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "Fulfillment created successfully"})
}

// ============================================================================
// STOCK SYNC
// ============================================================================

// UpdateStock updates Shopline inventory by variant_id + location.
// POST /shopline/stock
// Body: { "credential_id":"...", "variant_id": "12345", "quantity": 10, "location_id": "67890" }
func (h *ShoplineHandler) UpdateStock(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req struct {
		CredentialID string `json:"credential_id"`
		VariantID    string `json:"variant_id"`
		SKU          string `json:"sku"`
		Quantity     int    `json:"quantity"`
		LocationID   string `json:"location_id"`
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

	variantID := req.VariantID

	// Resolve variant_id from SKU if needed
	if variantID == "" && req.SKU != "" {
		varResult, _, err := client.do(c.Request.Context(), "GET",
			"/variants.json?fields=id,sku&limit=250", nil)
		if err == nil {
			if variants, ok := varResult["variants"].([]interface{}); ok {
				for _, v := range variants {
					if vm, ok := v.(map[string]interface{}); ok {
						if sku, _ := vm["sku"].(string); sku == req.SKU {
							variantID, _ = vm["id"].(string)
							break
						}
					}
				}
			}
		}
	}
	if variantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "variant_id or a valid sku is required"})
		return
	}

	// Resolve location_id if not provided
	locationID := req.LocationID
	if locationID == "" {
		locResult, _, err := client.do(c.Request.Context(), "GET", "/locations.json", nil)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"ok": false, "error": fmt.Sprintf("get locations: %v", err)})
			return
		}
		if locs, ok := locResult["locations"].([]interface{}); ok && len(locs) > 0 {
			if lm, ok := locs[0].(map[string]interface{}); ok {
				locationID, _ = lm["id"].(string)
			}
		}
	}
	if locationID == "" {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": "could not determine Shopline location"})
		return
	}

	body := map[string]interface{}{
		"inventory": map[string]interface{}{
			"variant_id":  variantID,
			"location_id": locationID,
			"quantity":    req.Quantity,
		},
	}
	_, _, err = client.do(c.Request.Context(), "POST", "/inventory_levels/set.json", body)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": fmt.Sprintf("set inventory level: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":          true,
		"variant_id":  variantID,
		"location_id": locationID,
		"quantity":    req.Quantity,
	})
}

// ============================================================================
// WEBHOOKS
// ============================================================================

// RegisterWebhooks registers Shopline order webhooks for the given store.
// POST /shopline/webhooks/register
// Body: { "credential_id": "..." }
func (h *ShoplineHandler) RegisterWebhooks(c *gin.Context) {
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

func (h *ShoplineHandler) registerWebhooksForCred(ctx context.Context, client *shoplineClient, cred *models.MarketplaceCredential) []string {
	apiBaseURL := os.Getenv("API_BASE_URL")
	if apiBaseURL == "" {
		apiBaseURL = "https://marketmate-api-487246736287.europe-west2.run.app"
	}

	baseURL := fmt.Sprintf("%s/webhooks/orders/shopline?tenant=%s&cred=%s",
		apiBaseURL, cred.TenantID, cred.CredentialID)

	// Shopline webhook topics
	topics := []struct {
		topic    string
		urlParam string
	}{
		{"orders/create", "orders%2Fcreate"},
		{"orders/updated", "orders%2Fupdated"},
		{"orders/fulfilled", "orders%2Ffulfilled"},
		{"orders/cancelled", "orders%2Fcancelled"},
	}

	var registered []string
	for _, sub := range topics {
		callbackURL := baseURL + "&topic=" + sub.urlParam

		webhookPayload := map[string]interface{}{
			"webhook": map[string]interface{}{
				"topic":   sub.topic,
				"address": callbackURL,
				"format":  "json",
			},
		}

		_, _, err := client.do(ctx, "POST", "/webhooks.json", webhookPayload)
		if err != nil {
			log.Printf("[Shopline Webhooks] Failed to register %s: %v", sub.topic, err)
			continue
		}

		registered = append(registered, sub.topic)
		log.Printf("[Shopline Webhooks] Registered: %s → %s", sub.topic, callbackURL)
	}
	return registered
}

// ============================================================================
// HELPERS
// ============================================================================

func mapShoplineOrder(so shoplineOrderResp, tenantID, credentialID string) *models.Order {
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
		currency = "CNY"
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
		OrderID:          fmt.Sprintf("shopline-%s", so.ID),
		TenantID:         tenantID,
		Channel:          "shopline",
		ChannelAccountID: credentialID,
		ExternalOrderID:  so.ID,
		Status:           normaliseShoplineStatus(so.FinancialStatus, so.FulfillStatus),
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

func normaliseShoplineStatus(financial, fulfillment string) string {
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
