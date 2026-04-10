package handlers

// ============================================================================
// WALMART HANDLER
// ============================================================================
// Routes:
//   POST /walmart/connect              → test + save credential
//   POST /walmart/test                 → test connection with given credentials
//   POST /walmart/prepare              → build listing draft from MarketMate product
//   POST /walmart/submit               → submit item feed to Walmart
//   GET  /walmart/feeds/:id            → poll feed status
//   PUT  /walmart/items/:sku/inventory → update inventory for a SKU
//   PUT  /walmart/items/:sku/price     → update price for a SKU
//   DELETE /walmart/items/:sku         → retire item from Walmart
//   GET  /walmart/items                → list seller items
// ============================================================================

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"module-a/marketplace/clients/walmart"
	"module-a/models"
	"module-a/repository"
	"module-a/services"
)

type WalmartHandler struct {
	marketplaceService *services.MarketplaceService
	repo               *repository.MarketplaceRepository
	productRepo        *repository.FirestoreRepository
}

func NewWalmartHandler(
	marketplaceService *services.MarketplaceService,
	repo *repository.MarketplaceRepository,
	productRepo *repository.FirestoreRepository,
) *WalmartHandler {
	return &WalmartHandler{
		marketplaceService: marketplaceService,
		repo:               repo,
		productRepo:        productRepo,
	}
}

// ── Credential resolution ─────────────────────────────────────────────────────

func (h *WalmartHandler) getWalmartClient(c *gin.Context) (*walmart.Client, string, error) {
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
			if cred.Channel == "walmart" && cred.Active {
				credentialID = cred.CredentialID
				break
			}
		}
		if credentialID == "" {
			return nil, "", fmt.Errorf("no Walmart credential found — please connect Walmart first")
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

	clientID := merged["client_id"]
	clientSecret := merged["client_secret"]

	if clientID == "" || clientSecret == "" {
		return nil, "", fmt.Errorf("incomplete Walmart credentials: client_id and client_secret required")
	}

	client := walmart.NewClient(clientID, clientSecret)
	return client, credentialID, nil
}

// ── Test Connection ───────────────────────────────────────────────────────────

// TestConnection tests Walmart credentials before saving.
// POST /walmart/test  { "client_id": "...", "client_secret": "..." }
func (h *WalmartHandler) TestConnection(c *gin.Context) {
	var req struct {
		ClientID     string `json:"client_id" binding:"required"`
		ClientSecret string `json:"client_secret" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	client := walmart.NewClient(req.ClientID, req.ClientSecret)
	if err := client.TestConnection(); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"message": "Walmart Marketplace connection successful",
	})
}

// ── Prepare & Submit ──────────────────────────────────────────────────────────

// PrepareListingDraft loads MarketMate product data and builds a Walmart item draft.
// POST /walmart/prepare  { "product_id": "...", "credential_id": "..." }
func (h *WalmartHandler) PrepareListingDraft(c *gin.Context) {
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

	draft := gin.H{
		"product_name":      product.Title,
		"short_description": product.Description,
		"sku":               product.Attributes["source_sku"],
		"price":             product.Attributes["price"],
		"quantity":          product.Attributes["quantity"],
		"brand":             product.Attributes["brand"],
		"images":            images,
		"upc":               "",
		"model_number":      "",
		"category":          "",
		"key_features":      []string{},
		"shipping_weight":   product.Attributes["weight_kg"],
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":         true,
		"product_id": req.ProductID,
		"draft":      draft,
	})
}

// SubmitItemFeed submits an MP_ITEM feed to Walmart.
// POST /walmart/submit
func (h *WalmartHandler) SubmitItemFeed(c *gin.Context) {
	client, _, err := h.getWalmartClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	var payload map[string]interface{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "invalid payload: " + err.Error()})
		return
	}

	// Wrap payload in MP_ITEM feed structure if not already wrapped
	feedPayload := payload
	if _, hasWrapper := payload["MPItemFeed"]; !hasWrapper {
		// Treat as a single item spec and wrap it
		feedPayload = map[string]interface{}{
			"MPItemFeed": map[string]interface{}{
				"MPItemFeedHeader": map[string]interface{}{"version": "4.7"},
				"MPItem":           []interface{}{payload},
			},
		}
	}

	feed, err := client.SubmitFeed("MP_ITEM", feedPayload)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"feed_id": feed.FeedID,
		"message": "Item feed submitted — use GET /walmart/feeds/" + feed.FeedID + " to check status",
	})
}

// GetFeedStatus returns the status of a submitted feed.
// GET /walmart/feeds/:id
func (h *WalmartHandler) GetFeedStatus(c *gin.Context) {
	client, _, err := h.getWalmartClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	feedID := c.Param("id")
	if feedID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "feed id is required"})
		return
	}

	status, err := client.GetFeedStatus(feedID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":     true,
		"status": status,
	})
}

// UpdateInventory updates stock quantity for a SKU.
// PUT /walmart/items/:sku/inventory  { "quantity": 10 }
func (h *WalmartHandler) UpdateInventory(c *gin.Context) {
	client, _, err := h.getWalmartClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	sku := c.Param("sku")
	var req struct {
		Quantity int `json:"quantity"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	if err := client.UpdateInventory(sku, req.Quantity); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "sku": sku, "quantity": req.Quantity})
}

// UpdatePrice updates the price for a SKU.
// PUT /walmart/items/:sku/price  { "price": 29.99 }
func (h *WalmartHandler) UpdatePrice(c *gin.Context) {
	client, _, err := h.getWalmartClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	sku := c.Param("sku")
	var req struct {
		Price float64 `json:"price"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	if err := client.UpdatePrice(sku, req.Price); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "sku": sku, "price": req.Price})
}

// RetireItem removes an item from Walmart.
// DELETE /walmart/items/:sku
func (h *WalmartHandler) RetireItem(c *gin.Context) {
	client, _, err := h.getWalmartClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	sku := c.Param("sku")
	if err := client.RetireItem(sku); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "retired_sku": sku})
}

// GetItems returns a list of seller items from Walmart.
// GET /walmart/items?cursor=...
func (h *WalmartHandler) GetItems(c *gin.Context) {
	client, _, err := h.getWalmartClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error(), "items": []interface{}{}})
		return
	}

	cursor := c.Query("cursor")
	items, err := client.GetItems(cursor, 50)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error(), "items": []interface{}{}})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":          true,
		"items":       items.ItemDetails,
		"total_items": items.TotalItems,
		"next_cursor": items.NextCursor,
	})
}

// ── Credential save ───────────────────────────────────────────────────────────

// SaveCredential saves Walmart credentials after verifying the connection.
// POST /walmart/connect
func (h *WalmartHandler) SaveCredential(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req struct {
		AccountName  string `json:"account_name"`
		ClientID     string `json:"client_id" binding:"required"`
		ClientSecret string `json:"client_secret" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	// Test connection before saving
	client := walmart.NewClient(req.ClientID, req.ClientSecret)
	if err := client.TestConnection(); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": "Connection test failed: " + err.Error()})
		return
	}

	accountName := req.AccountName
	if accountName == "" {
		accountName = "Walmart Marketplace"
	}

	credData := map[string]string{
		"client_id":     req.ClientID,
		"client_secret": req.ClientSecret,
	}

	// Check for existing Walmart credential matching this client_id
	existingCreds, _ := h.repo.ListCredentials(c.Request.Context(), tenantID)
	credentialID := ""
	for _, ec := range existingCreds {
		if ec.Channel == "walmart" && ec.Active {
			if cd, ok := ec.CredentialData["client_id"]; ok && cd == req.ClientID {
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
		credentialID = fmt.Sprintf("cred-walmart-%d", time.Now().UnixMilli())
		newCred := &models.MarketplaceCredential{
			CredentialID:   credentialID,
			TenantID:       tenantID,
			Channel:        "walmart",
			AccountName:    accountName,
			Environment:    "production",
			Active:         true,
			CredentialData: credData,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}
		if err := h.repo.SaveCredential(c.Request.Context(), newCred); err != nil {
			log.Printf("[Walmart] Failed to create credential: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "failed to save credential: " + err.Error()})
			return
		}
	}

	log.Printf("[Walmart] Credential saved: %s for client_id %s", credentialID, req.ClientID)
	c.JSON(http.StatusOK, gin.H{
		"ok":            true,
		"credential_id": credentialID,
		"message":       "Walmart Marketplace connected successfully",
	})
}
