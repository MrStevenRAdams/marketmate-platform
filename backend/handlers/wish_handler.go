package handlers

// ============================================================================
// WISH HANDLER
// ============================================================================
// Routes:
//   POST /wish/connect               → test + save credentials
//   POST /wish/listings/prepare      → load product from PIM, return pre-filled draft
//   POST /wish/listings/submit       → create or update a Wish product via REST API
//   GET  /wish/orders/import         → fetch recent orders and upsert into Firestore
// ============================================================================
// Auth: Access token — sent as Authorization: Bearer <access_token> header.
// Credential field: access_token
// Wish Merchant API base: https://merchant.wish.com/api/v3
// Docs: https://merchant.wish.com/documentation/v3
// ============================================================================

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"module-a/models"
	"module-a/repository"
	"module-a/services"
)

// ── Wish REST client ──────────────────────────────────────────────────────────

const wishAPIBase = "https://merchant.wish.com/api/v3"

type wishClient struct {
	accessToken string
	http        *http.Client
}

func newWishClient(accessToken string) *wishClient {
	return &wishClient{
		accessToken: accessToken,
		http:        &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *wishClient) do(method, path string, body io.Reader) (*http.Response, error) {
	url := wishAPIBase + path
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return c.http.Do(req)
}

func (c *wishClient) testConnection() error {
	// Ping the merchant info endpoint to verify token
	resp, err := c.do("GET", "/merchant", nil)
	if err != nil {
		return fmt.Errorf("connection error: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("invalid access token — authentication failed (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("Wish API returned HTTP %d — check access token and merchant account status", resp.StatusCode)
	}
	return nil
}

func (c *wishClient) createOrUpdateProduct(payload map[string]interface{}) (map[string]interface{}, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	// Wish uses POST /products to create; PUT /products/{id} to update
	// We check for an existing product_id in the payload
	productID, _ := payload["product_id"].(string)
	var resp *http.Response
	if productID != "" {
		resp, err = c.do("PUT", "/products/"+productID, strings.NewReader(string(b)))
	} else {
		resp, err = c.do("POST", "/products", strings.NewReader(string(b)))
	}
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("Wish API error (HTTP %d): %s", resp.StatusCode, string(bodyBytes))
	}
	var result map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to parse Wish response: %w", err)
	}
	return result, nil
}

func (c *wishClient) fetchRecentOrders(hoursBack int) ([]map[string]interface{}, error) {
	since := time.Now().UTC().Add(-time.Duration(hoursBack) * time.Hour).Unix()
	path := fmt.Sprintf("/orders?since=%d&limit=100", since)
	resp, err := c.do("GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("Wish orders API error (HTTP %d): %s", resp.StatusCode, string(bodyBytes))
	}

	// Wish wraps results in {"data": [...]} or {"result": [...]}
	var wrapper struct {
		Data   []map[string]interface{} `json:"data"`
		Result []map[string]interface{} `json:"result"`
	}
	if err := json.Unmarshal(bodyBytes, &wrapper); err != nil {
		var direct []map[string]interface{}
		if err2 := json.Unmarshal(bodyBytes, &direct); err2 != nil {
			return nil, fmt.Errorf("failed to parse Wish orders response: %w", err)
		}
		return direct, nil
	}
	if len(wrapper.Data) > 0 {
		return wrapper.Data, nil
	}
	return wrapper.Result, nil
}

// ── Handler struct ────────────────────────────────────────────────────────────

type WishHandler struct {
	marketplaceService *services.MarketplaceService
	repo               *repository.MarketplaceRepository
	productRepo        *repository.FirestoreRepository
	orderService       *services.OrderService
}

func NewWishHandler(
	marketplaceService *services.MarketplaceService,
	repo *repository.MarketplaceRepository,
	productRepo *repository.FirestoreRepository,
	orderService *services.OrderService,
) *WishHandler {
	return &WishHandler{
		marketplaceService: marketplaceService,
		repo:               repo,
		productRepo:        productRepo,
		orderService:       orderService,
	}
}

// ── Credential resolution ─────────────────────────────────────────────────────

func (h *WishHandler) getWishClient(c *gin.Context) (*wishClient, string, error) {
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
			if cred.Channel == "wish" && cred.Active {
				credentialID = cred.CredentialID
				break
			}
		}
		if credentialID == "" {
			return nil, "", fmt.Errorf("no Wish credential found — please connect a Wish merchant account first")
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

	accessToken := merged["access_token"]
	if accessToken == "" {
		return nil, "", fmt.Errorf("incomplete Wish credentials: access_token is required")
	}

	return newWishClient(accessToken), credentialID, nil
}

// ── SaveCredential ─────────────────────────────────────────────────────────────

// SaveCredential tests + saves Wish credentials.
// POST /wish/connect
func (h *WishHandler) SaveCredential(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req struct {
		AccountName string `json:"account_name"`
		AccessToken string `json:"access_token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	client := newWishClient(req.AccessToken)
	if err := client.testConnection(); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": "Connection test failed: " + err.Error()})
		return
	}

	accountName := req.AccountName
	if accountName == "" {
		accountName = "Wish Merchant Account"
	}

	credData := map[string]string{"access_token": req.AccessToken}

	// Upsert: check for existing active Wish credential
	existingCreds, _ := h.repo.ListCredentials(c.Request.Context(), tenantID)
	credentialID := ""
	for _, ec := range existingCreds {
		if ec.Channel == "wish" && ec.Active {
			credentialID = ec.CredentialID
			break
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
		credentialID = "cred-wish-" + fmt.Sprintf("%d", time.Now().UnixMilli())
		newCred := &models.MarketplaceCredential{
			CredentialID:   credentialID,
			TenantID:       tenantID,
			Channel:        "wish",
			AccountName:    accountName,
			Environment:    "production",
			Active:         true,
			CredentialData: credData,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}
		if err := h.repo.SaveCredential(c.Request.Context(), newCred); err != nil {
			log.Printf("[Wish] Failed to create credential: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "failed to save credential: " + err.Error()})
			return
		}
	}

	log.Printf("[Wish] Credential saved: %s", credentialID)
	c.JSON(http.StatusOK, gin.H{
		"ok":            true,
		"credential_id": credentialID,
		"message":       "Wish merchant account connected successfully",
	})
}

// ── PrepareListingDraft ────────────────────────────────────────────────────────

// PrepareListingDraft loads a MarketMate product and builds a Wish-ready draft.
// POST /wish/listings/prepare  { "product_id": "...", "credential_id": "..." }
func (h *WishHandler) PrepareListingDraft(c *gin.Context) {
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

	// ── Extract attributes ────────────────────────────────────────────────────

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
		// Wish expects weight in grams
		if w, ok := product.Attributes["weight_kg"].(float64); ok {
			weight = w * 1000 // convert kg → grams
		}
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

	brand := ""
	if product.Attributes != nil {
		if b, ok := product.Attributes["brand"].(string); ok {
			brand = b
		}
	}

	landedCost := price // shipping included in landed cost on Wish
	shippingPrice := 0.0

	mainImage := ""
	if len(images) > 0 {
		mainImage = images[0]
	}

	// Wish requires variants — build a single default variant
	variant := gin.H{
		"sku":            sku,
		"price":          price,
		"shipping":       shippingPrice,
		"inventory":      qty,
		"weight":         weight,
		"landed_cost":    landedCost,
		"main_image":     mainImage,
		"enabled":        true,
	}

	draft := gin.H{
		"name":              product.Title,
		"description":       product.Description,
		"sku":               sku,
		"price":             price,
		"shipping":          shippingPrice,
		"inventory":         qty,
		"weight":            weight,                     // grams
		"brand":             brand,
		"main_image":        mainImage,
		"extra_images":      images,
		"tags":              "",                         // user can populate
		"is_shipping_only":  false,
		"enabled":           true,
		"variants":          []interface{}{variant},
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":         true,
		"product_id": req.ProductID,
		"draft":      draft,
		"note":       "Wish requires at least one variant. The default variant has been pre-filled from your PIM data.",
	})
}

// ── SubmitListing ─────────────────────────────────────────────────────────────

// SubmitListing creates or updates a Wish product via their REST API.
// POST /wish/listings/submit
func (h *WishHandler) SubmitListing(c *gin.Context) {
	client, _, err := h.getWishClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	var payload map[string]interface{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "invalid payload: " + err.Error()})
		return
	}

	name, _ := payload["name"].(string)
	if strings.TrimSpace(name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "name is required"})
		return
	}

	// Remove internal-only fields before forwarding
	delete(payload, "credential_id")

	result, err := client.createOrUpdateProduct(payload)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	productID := ""
	if id, ok := result["id"]; ok {
		productID = fmt.Sprintf("%v", id)
	}

	log.Printf("[Wish] Product submitted: id=%s name=%s", productID, name)
	c.JSON(http.StatusOK, gin.H{
		"ok":         true,
		"product_id": productID,
		"message":    "Wish product created/updated successfully",
		"result":     result,
	})
}

// ── ImportOrders ──────────────────────────────────────────────────────────────

// ImportOrders fetches recent Wish orders and upserts them into Firestore.
// GET /wish/orders/import
func (h *WishHandler) ImportOrders(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	credentialID := c.Query("credential_id")
	if credentialID == "" {
		credentialID = c.GetHeader("X-Credential-Id")
	}

	hoursBack := 24
	if hb := c.Query("hours_back"); hb != "" {
		var v int
		if _, err := fmt.Sscanf(hb, "%d", &v); err == nil && v > 0 {
			hoursBack = v
		}
	}

	if credentialID == "" {
		creds, err := h.repo.ListCredentials(c.Request.Context(), tenantID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": err.Error()})
			return
		}
		for _, cr := range creds {
			if cr.Channel == "wish" && cr.Active {
				credentialID = cr.CredentialID
				break
			}
		}
		if credentialID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "no active Wish credential found"})
			return
		}
	}

	cred, err := h.repo.GetCredential(c.Request.Context(), tenantID, credentialID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "failed to load credential: " + err.Error()})
		return
	}
	merged, err := h.marketplaceService.GetFullCredentials(c.Request.Context(), cred)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "failed to merge credentials: " + err.Error()})
		return
	}

	accessToken := merged["access_token"]
	if accessToken == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "access_token not found in credential"})
		return
	}

	client := newWishClient(accessToken)

	c.JSON(http.StatusAccepted, gin.H{
		"ok":            true,
		"status":        "started",
		"hours_back":    hoursBack,
		"credential_id": credentialID,
		"message":       "Wish order import started in background",
	})

	go func() {
		ctx := context.Background()
		n, errs := h.importWishOrders(ctx, client, tenantID, credentialID, hoursBack)
		if len(errs) > 0 {
			log.Printf("[Wish Orders] Import completed with %d errors (imported %d orders)", len(errs), n)
			for _, e := range errs {
				log.Printf("[Wish Orders]   error: %v", e)
			}
		} else {
			log.Printf("[Wish Orders] Import complete: %d orders imported", n)
		}
	}()
}

func (h *WishHandler) importWishOrders(ctx context.Context, client *wishClient, tenantID, credentialID string, hoursBack int) (int, []error) {
	orders, err := client.fetchRecentOrders(hoursBack)
	if err != nil {
		return 0, []error{fmt.Errorf("fetch orders: %w", err)}
	}

	log.Printf("[Wish Orders] Fetched %d orders for tenant=%s", len(orders), tenantID)

	imported := 0
	var errs []error

	for _, o := range orders {
		externalID := fmt.Sprintf("%v", o["id"])
		if externalID == "" || externalID == "<nil>" {
			continue
		}

		orderDate, _ := o["created_at"].(string)
		if orderDate == "" {
			if ts, ok := o["time_created"].(float64); ok {
				orderDate = time.Unix(int64(ts), 0).UTC().Format(time.RFC3339)
			} else {
				orderDate = time.Now().UTC().Format(time.RFC3339)
			}
		}

		status := mapWishOrderStatus(fmt.Sprintf("%v", o["state"]))

		// Customer
		customerName := ""
		customerEmail := ""
		if buyer, ok := o["buyer"].(map[string]interface{}); ok {
			name, _ := buyer["name"].(string)
			customerName = name
			customerEmail, _ = buyer["email"].(string)
		}

		// Shipping address — Wish nests it in "shipping_detail"
		addr := models.Address{}
		if sd, ok := o["shipping_detail"].(map[string]interface{}); ok {
			name, _ := sd["name"].(string)
			if name == "" {
				name = customerName
			}
			addr = models.Address{
				Name:         name,
				AddressLine1: fmt.Sprintf("%v", sd["street_address1"]),
				AddressLine2: fmt.Sprintf("%v", sd["street_address2"]),
				City:         fmt.Sprintf("%v", sd["city"]),
				State:        fmt.Sprintf("%v", sd["state"]),
				PostalCode:   fmt.Sprintf("%v", sd["zipcode"]),
				Country:      fmt.Sprintf("%v", sd["country"]),
			}
		}

		// Totals — Wish reports in USD by default
		grandTotal := 0.0
		if t, ok := o["price"].(float64); ok {
			grandTotal = t
		}
		shippingCost := 0.0
		if s, ok := o["shipping"].(float64); ok {
			shippingCost = s
		}

		currency := "USD"
		if cur, ok := o["currency"].(string); ok && cur != "" {
			currency = cur
		}

		internalOrder := &models.Order{
			ExternalOrderID:  externalID,
			ChannelAccountID: credentialID,
			Channel:          "wish",
			Status:           status,
			PaymentStatus:    "captured",
			Customer: models.Customer{
				Name:  customerName,
				Email: customerEmail,
			},
			ShippingAddress: addr,
			OrderDate:       orderDate,
			ImportedAt:      time.Now().UTC().Format(time.RFC3339),
			Totals: models.OrderTotals{
				Shipping:   models.Money{Amount: shippingCost, Currency: currency},
				GrandTotal: models.Money{Amount: grandTotal, Currency: currency},
			},
			InternalNotes: fmt.Sprintf("Wish Order #%s", externalID),
		}

		_, isNew, err := h.orderService.CreateOrder(ctx, tenantID, internalOrder)
		if err != nil {
			errs = append(errs, fmt.Errorf("save order %s: %w", externalID, err))
			continue
		}
		if !isNew {
			log.Printf("[Wish Orders] Skipping duplicate order %s", externalID)
			continue
		}
		imported++
	}

	return imported, errs
}

func mapWishOrderStatus(s string) string {
	switch strings.ToLower(s) {
	case "pending", "approved":
		return "imported"
	case "shipped":
		return "shipped"
	case "completed", "delivered":
		return "completed"
	case "refunded", "partially_refunded":
		return "refunded"
	case "cancelled", "canceled":
		return "cancelled"
	case "require_review":
		return "on_hold"
	default:
		return "imported"
	}
}
