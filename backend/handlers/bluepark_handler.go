package handlers

// ============================================================================
// BLUEPARK HANDLER
// ============================================================================
// Routes:
//   POST /bluepark/connect           → test + save credentials
//   POST /bluepark/listings/prepare  → load product from PIM, return pre-filled draft
//   POST /bluepark/listings/submit   → create or update a Bluepark product via REST API
//   GET  /bluepark/orders/import     → fetch recent orders and upsert into Firestore
// ============================================================================
// Auth: API Key — sent as X-API-Key header on all Bluepark API requests.
// Credential field: api_key
// Bluepark REST API base: https://api.bluepark.co.uk/v1
// ============================================================================

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"module-a/models"
	"module-a/repository"
	"module-a/services"
)

// ── Bluepark REST client ──────────────────────────────────────────────────────

const blueparkAPIBase = "https://api.bluepark.co.uk/v1"

type blueparkClient struct {
	apiKey string
	http   *http.Client
}

func newBlueparkClient(apiKey string) *blueparkClient {
	return &blueparkClient{
		apiKey: apiKey,
		http:   &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *blueparkClient) do(method, path string, body io.Reader) (*http.Response, error) {
	url := blueparkAPIBase + path
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return c.http.Do(req)
}

func (c *blueparkClient) testConnection() error {
	// Ping the products endpoint with limit=1 to verify credentials
	resp, err := c.do("GET", "/products?limit=1", nil)
	if err != nil {
		return fmt.Errorf("connection error: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("invalid API key — authentication failed (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("Bluepark API returned HTTP %d — check API key and account status", resp.StatusCode)
	}
	return nil
}

func (c *blueparkClient) createOrUpdateProduct(payload map[string]interface{}) (map[string]interface{}, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	resp, err := c.do("POST", "/products", strings.NewReader(string(b)))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("Bluepark API error (HTTP %d): %s", resp.StatusCode, string(body))
	}
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse Bluepark response: %w", err)
	}
	return result, nil
}

func (c *blueparkClient) fetchRecentOrders(hoursBack int) ([]map[string]interface{}, error) {
	since := time.Now().UTC().Add(-time.Duration(hoursBack) * time.Hour).Format(time.RFC3339)
	path := fmt.Sprintf("/orders?created_after=%s&limit=100", since)
	resp, err := c.do("GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("Bluepark orders API error (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var wrapper struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(body, &wrapper); err != nil {
		// Try as direct array
		var direct []map[string]interface{}
		if err2 := json.Unmarshal(body, &direct); err2 != nil {
			return nil, fmt.Errorf("failed to parse Bluepark orders response: %w", err)
		}
		return direct, nil
	}
	return wrapper.Data, nil
}

// ── Handler struct ────────────────────────────────────────────────────────────

type BlueparkHandler struct {
	marketplaceService *services.MarketplaceService
	repo               *repository.MarketplaceRepository
	productRepo        *repository.FirestoreRepository
	orderService       *services.OrderService
}

func NewBlueparkHandler(
	marketplaceService *services.MarketplaceService,
	repo *repository.MarketplaceRepository,
	productRepo *repository.FirestoreRepository,
	orderService *services.OrderService,
) *BlueparkHandler {
	return &BlueparkHandler{
		marketplaceService: marketplaceService,
		repo:               repo,
		productRepo:        productRepo,
		orderService:       orderService,
	}
}

// ── Credential resolution ─────────────────────────────────────────────────────

func (h *BlueparkHandler) getBlueparkClient(c *gin.Context) (*blueparkClient, string, error) {
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
			if cred.Channel == "bluepark" && cred.Active {
				credentialID = cred.CredentialID
				break
			}
		}
		if credentialID == "" {
			return nil, "", fmt.Errorf("no Bluepark credential found — please connect a Bluepark account first")
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

	apiKey := merged["api_key"]
	if apiKey == "" {
		return nil, "", fmt.Errorf("incomplete Bluepark credentials: api_key is required")
	}

	return newBlueparkClient(apiKey), credentialID, nil
}

// ── SaveCredential ─────────────────────────────────────────────────────────────

// SaveCredential tests + saves Bluepark credentials.
// POST /bluepark/connect
func (h *BlueparkHandler) SaveCredential(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req struct {
		AccountName string `json:"account_name"`
		APIKey      string `json:"api_key" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	client := newBlueparkClient(req.APIKey)
	if err := client.testConnection(); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": "Connection test failed: " + err.Error()})
		return
	}

	accountName := req.AccountName
	if accountName == "" {
		accountName = "Bluepark Store"
	}

	credData := map[string]string{"api_key": req.APIKey}

	// Upsert: check for existing active Bluepark credential
	existingCreds, _ := h.repo.ListCredentials(c.Request.Context(), tenantID)
	credentialID := ""
	for _, ec := range existingCreds {
		if ec.Channel == "bluepark" && ec.Active {
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
		credentialID = "cred-bluepark-" + fmt.Sprintf("%d", time.Now().UnixMilli())
		newCred := &models.MarketplaceCredential{
			CredentialID:   credentialID,
			TenantID:       tenantID,
			Channel:        "bluepark",
			AccountName:    accountName,
			Environment:    "production",
			Active:         true,
			CredentialData: credData,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}
		if err := h.repo.SaveCredential(c.Request.Context(), newCred); err != nil {
			log.Printf("[Bluepark] Failed to create credential: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "failed to save credential: " + err.Error()})
			return
		}
	}

	log.Printf("[Bluepark] Credential saved: %s", credentialID)
	c.JSON(http.StatusOK, gin.H{
		"ok":            true,
		"credential_id": credentialID,
		"message":       "Bluepark account connected successfully",
	})
}

// ── PrepareListingDraft ────────────────────────────────────────────────────────

// PrepareListingDraft loads a MarketMate product and builds a Bluepark-ready draft.
// POST /bluepark/listings/prepare  { "product_id": "...", "credential_id": "..." }
func (h *BlueparkHandler) PrepareListingDraft(c *gin.Context) {
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
		if w, ok := product.Attributes["weight_kg"].(float64); ok {
			weight = w
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

	barcode := ""
	if product.Attributes != nil {
		if b, ok := product.Attributes["barcode"].(string); ok {
			barcode = b
		}
		if barcode == "" {
			if b, ok := product.Attributes["ean"].(string); ok {
				barcode = b
			}
		}
	}

	brand := ""
	if product.Attributes != nil {
		if b, ok := product.Attributes["brand"].(string); ok {
			brand = b
		}
	}

	draft := gin.H{
		"name":        product.Title,
		"sku":         sku,
		"description": product.Description,
		"price":       price,
		"quantity":    qty,
		"weight":      weight,
		"barcode":     barcode,
		"brand":       brand,
		"images":      images,
		"status":      "active",
		"condition":   "new",
		// VAR-01: include PIM variants so the frontend can render the variant grid
		"variants": loadChannelVariants(c.Request.Context(), h.productRepo, tenantID, req.ProductID, fmt.Sprintf("%.2f", price), func() string {
			if len(images) > 0 {
				return images[0]
			}
			return ""
		}()),
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":         true,
		"product_id": req.ProductID,
		"draft":      draft,
	})
}

// ── SubmitListing ─────────────────────────────────────────────────────────────

// SubmitListing creates or updates a Bluepark product via their REST API.
// POST /bluepark/listings/submit
func (h *BlueparkHandler) SubmitListing(c *gin.Context) {
	client, _, err := h.getBlueparkClient(c)
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

	// VAR-01: mimicked variations — one product per active variant
	// Bluepark has no native variation grouping API.
	if rawVariants, ok := payload["variants"]; ok {
		if varSlice, ok := rawVariants.([]interface{}); ok {
			activeVariants := make([]ChannelVariantDraft, 0)
			for _, rv := range varSlice {
				if vm, ok := rv.(map[string]interface{}); ok {
					active, _ := vm["active"].(bool)
					if !active {
						continue
					}
					v := ChannelVariantDraft{
						ID:  fmt.Sprintf("%v", vm["id"]),
						SKU: fmt.Sprintf("%v", vm["sku"]),
					}
					v.Price, _ = vm["price"].(string)
					v.Stock, _ = vm["stock"].(string)
					v.EAN, _ = vm["ean"].(string)
					v.Active = true
					if combo, ok := vm["combination"].(map[string]interface{}); ok {
						v.Combination = make(map[string]string)
						for k, val := range combo {
							v.Combination[k] = fmt.Sprintf("%v", val)
						}
					}
					activeVariants = append(activeVariants, v)
				}
			}
			if len(activeVariants) >= 2 {
				type prodResult struct {
					SKU       string `json:"sku"`
					ProductID string `json:"product_id,omitempty"`
					Error     string `json:"error,omitempty"`
				}
				submitted := []prodResult{}
				errors := []prodResult{}
				basePrice, _ := payload["price"].(float64)
				baseQty := 0
				if q, ok := payload["quantity"].(float64); ok {
					baseQty = int(q)
				}
				delete(payload, "credential_id")
				delete(payload, "variants")
				for _, v := range activeVariants {
					varPayload := map[string]interface{}{}
					for k, val := range payload {
						varPayload[k] = val
					}
					varPayload["sku"] = v.SKU
					if v.EAN != "" {
						varPayload["barcode"] = v.EAN
					}
					if p, err := strconv.ParseFloat(v.Price, 64); err == nil && p > 0 {
						varPayload["price"] = p
					} else {
						varPayload["price"] = basePrice
					}
					if q, err := strconv.Atoi(v.Stock); err == nil && q >= 0 {
						varPayload["quantity"] = q
					} else {
						varPayload["quantity"] = baseQty
					}
					// Append combination label to name to differentiate products
					if len(v.Combination) > 0 {
						label := ""
						for _, val := range v.Combination {
							if label != "" {
								label += " / "
							}
							label += val
						}
						if label != "" {
							varPayload["name"] = name + " - " + label
						}
					}
					result, err := client.createOrUpdateProduct(varPayload)
					if err != nil {
						errors = append(errors, prodResult{SKU: v.SKU, Error: err.Error()})
						continue
					}
					pid := ""
					if id, ok := result["id"]; ok {
						pid = fmt.Sprintf("%v", id)
					}
					submitted = append(submitted, prodResult{SKU: v.SKU, ProductID: pid})
					log.Printf("[Bluepark Submit] Variant product created: SKU=%s id=%s", v.SKU, pid)
				}
				c.JSON(http.StatusOK, gin.H{
					"ok":        len(submitted) > 0,
					"submitted": submitted,
					"errors":    errors,
					"message":   fmt.Sprintf("%d/%d variants submitted as individual Bluepark products", len(submitted), len(activeVariants)),
				})
				return
			}
		}
	}

	// Single-product path (original behaviour)
	// Remove internal-only fields before forwarding
	delete(payload, "credential_id")
	delete(payload, "variants")

	result, err := client.createOrUpdateProduct(payload)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	productID := ""
	if id, ok := result["id"]; ok {
		productID = fmt.Sprintf("%v", id)
	}

	log.Printf("[Bluepark] Product submitted: id=%s name=%s", productID, name)
	c.JSON(http.StatusOK, gin.H{
		"ok":         true,
		"product_id": productID,
		"message":    "Bluepark product created/updated successfully",
		"result":     result,
	})
}

// ── ImportOrders ──────────────────────────────────────────────────────────────

// ImportOrders fetches recent Bluepark orders and upserts them into Firestore.
// GET /bluepark/orders/import
func (h *BlueparkHandler) ImportOrders(c *gin.Context) {
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
			if cr.Channel == "bluepark" && cr.Active {
				credentialID = cr.CredentialID
				break
			}
		}
		if credentialID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "no active Bluepark credential found"})
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

	apiKey := merged["api_key"]
	if apiKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "api_key not found in credential"})
		return
	}

	client := newBlueparkClient(apiKey)

	c.JSON(http.StatusAccepted, gin.H{
		"ok":            true,
		"status":        "started",
		"hours_back":    hoursBack,
		"credential_id": credentialID,
		"message":       "Bluepark order import started in background",
	})

	go func() {
		ctx := context.Background()
		n, errs := h.importBlueparkOrders(ctx, client, tenantID, credentialID, hoursBack)
		if len(errs) > 0 {
			log.Printf("[Bluepark Orders] Import completed with %d errors (imported %d orders)", len(errs), n)
			for _, e := range errs {
				log.Printf("[Bluepark Orders]   error: %v", e)
			}
		} else {
			log.Printf("[Bluepark Orders] Import complete: %d orders imported", n)
		}
	}()
}

func (h *BlueparkHandler) importBlueparkOrders(ctx context.Context, client *blueparkClient, tenantID, credentialID string, hoursBack int) (int, []error) {
	orders, err := client.fetchRecentOrders(hoursBack)
	if err != nil {
		return 0, []error{fmt.Errorf("fetch orders: %w", err)}
	}

	log.Printf("[Bluepark Orders] Fetched %d orders for tenant=%s", len(orders), tenantID)

	imported := 0
	var errs []error

	for _, o := range orders {
		externalID := fmt.Sprintf("%v", o["id"])
		if externalID == "" || externalID == "<nil>" {
			continue
		}

		orderDate, _ := o["created_at"].(string)
		if orderDate == "" {
			orderDate = time.Now().UTC().Format(time.RFC3339)
		}

		status := mapBlueparkOrderStatus(fmt.Sprintf("%v", o["status"]))

		// Customer
		customerName := ""
		customerEmail := ""
		if cust, ok := o["customer"].(map[string]interface{}); ok {
			firstName, _ := cust["first_name"].(string)
			lastName, _ := cust["last_name"].(string)
			customerName = strings.TrimSpace(firstName + " " + lastName)
			customerEmail, _ = cust["email"].(string)
		}

		// Shipping address
		addr := models.Address{}
		if sa, ok := o["shipping_address"].(map[string]interface{}); ok {
			addrLine1, _ := sa["address1"].(string)
			addrLine2, _ := sa["address2"].(string)
			city, _ := sa["city"].(string)
			county, _ := sa["county"].(string)
			postcode, _ := sa["postcode"].(string)
			country, _ := sa["country_iso2"].(string)
			name, _ := sa["name"].(string)
			if name == "" {
				name = customerName
			}
			addr = models.Address{
				Name:         name,
				AddressLine1: addrLine1,
				AddressLine2: addrLine2,
				City:         city,
				State:        county,
				PostalCode:   postcode,
				Country:      country,
			}
		}

		// Totals
		grandTotal := 0.0
		if t, ok := o["total"].(float64); ok {
			grandTotal = t
		}
		shippingCost := 0.0
		if s, ok := o["shipping_total"].(float64); ok {
			shippingCost = s
		}

		currency := "GBP"
		if cur, ok := o["currency"].(string); ok && cur != "" {
			currency = cur
		}

		internalOrder := &models.Order{
			ExternalOrderID:  externalID,
			ChannelAccountID: credentialID,
			Channel:          "bluepark",
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
			InternalNotes: fmt.Sprintf("Bluepark Order #%s", externalID),
		}

		_, isNew, err := h.orderService.CreateOrder(ctx, tenantID, internalOrder)
		if err != nil {
			errs = append(errs, fmt.Errorf("save order %s: %w", externalID, err))
			continue
		}
		if !isNew {
			log.Printf("[Bluepark Orders] Skipping duplicate order %s", externalID)
			continue
		}
		imported++
	}

	return imported, errs
}

func mapBlueparkOrderStatus(s string) string {
	switch strings.ToLower(s) {
	case "pending", "new":
		return "imported"
	case "processing", "picking", "packing":
		return "processing"
	case "shipped", "dispatched":
		return "shipped"
	case "complete", "completed":
		return "completed"
	case "cancelled", "canceled":
		return "cancelled"
	case "refunded":
		return "refunded"
	default:
		return "imported"
	}
}
