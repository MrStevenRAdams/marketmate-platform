package handlers

// ============================================================================
// KAUFLAND HANDLER
// ============================================================================
// Routes:
//   POST   /kaufland/connect           → test + save credential
//   POST   /kaufland/test              → test connection with given credentials
//   GET    /kaufland/categories        → Kaufland category tree
//   POST   /kaufland/prepare           → build listing draft from MarketMate product
//   POST   /kaufland/submit            → create unit on Kaufland
//   PATCH  /kaufland/units/:id         → update a unit
//   DELETE /kaufland/units/:id         → delete a unit
//   GET    /kaufland/units             → list seller units
// ============================================================================

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"module-a/marketplace/clients/kaufland"
	"module-a/models"
	"module-a/repository"
	"module-a/services"
)

type KauflandHandler struct {
	marketplaceService *services.MarketplaceService
	repo               *repository.MarketplaceRepository
	productRepo        *repository.FirestoreRepository
}

func NewKauflandHandler(
	marketplaceService *services.MarketplaceService,
	repo *repository.MarketplaceRepository,
	productRepo *repository.FirestoreRepository,
) *KauflandHandler {
	return &KauflandHandler{
		marketplaceService: marketplaceService,
		repo:               repo,
		productRepo:        productRepo,
	}
}

// ── Credential resolution ─────────────────────────────────────────────────────

func (h *KauflandHandler) getKauflandClient(c *gin.Context) (*kaufland.Client, string, error) {
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
			if cred.Channel == "kaufland" && cred.Active {
				credentialID = cred.CredentialID
				break
			}
		}
		if credentialID == "" {
			return nil, "", fmt.Errorf("no Kaufland credential found — please connect Kaufland first")
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

	clientKey := merged["client_key"]
	secretKey := merged["secret_key"]

	if clientKey == "" || secretKey == "" {
		return nil, "", fmt.Errorf("incomplete Kaufland credentials: client_key and secret_key required")
	}

	client := kaufland.NewClient(clientKey, secretKey)
	return client, credentialID, nil
}

// ── Test Connection ───────────────────────────────────────────────────────────

// TestConnection tests Kaufland credentials before saving.
// POST /kaufland/test  { "client_key": "...", "secret_key": "..." }
func (h *KauflandHandler) TestConnection(c *gin.Context) {
	var req struct {
		ClientKey string `json:"client_key" binding:"required"`
		SecretKey string `json:"secret_key" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	client := kaufland.NewClient(req.ClientKey, req.SecretKey)
	if err := client.TestConnection(); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"message": "Kaufland connection successful",
	})
}

// ── Save Credential ───────────────────────────────────────────────────────────

// SaveCredential saves Kaufland credentials after verifying the connection.
// POST /kaufland/connect
func (h *KauflandHandler) SaveCredential(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req struct {
		AccountName string `json:"account_name"`
		ClientKey   string `json:"client_key" binding:"required"`
		SecretKey   string `json:"secret_key" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	// Test connection before saving
	client := kaufland.NewClient(req.ClientKey, req.SecretKey)
	if err := client.TestConnection(); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": "Connection test failed: " + err.Error()})
		return
	}

	accountName := req.AccountName
	if accountName == "" {
		accountName = "Kaufland"
	}

	credData := map[string]string{
		"client_key": req.ClientKey,
		"secret_key": req.SecretKey,
	}

	// Check for existing Kaufland credential matching this client_key
	existingCreds, _ := h.repo.ListCredentials(c.Request.Context(), tenantID)
	credentialID := ""
	for _, ec := range existingCreds {
		if ec.Channel == "kaufland" && ec.Active {
			if cd, ok := ec.CredentialData["client_key"]; ok && cd == req.ClientKey {
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
		credentialID = fmt.Sprintf("cred-kaufland-%d", time.Now().UnixMilli())
		newCred := &models.MarketplaceCredential{
			CredentialID:   credentialID,
			TenantID:       tenantID,
			Channel:        "kaufland",
			AccountName:    accountName,
			Environment:    "production",
			Active:         true,
			CredentialData: credData,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}
		if err := h.repo.SaveCredential(c.Request.Context(), newCred); err != nil {
			log.Printf("[Kaufland] Failed to create credential: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "failed to save credential: " + err.Error()})
			return
		}
	}

	log.Printf("[Kaufland] Credential saved: %s for client_key %s", credentialID, req.ClientKey)
	c.JSON(http.StatusOK, gin.H{
		"ok":            true,
		"credential_id": credentialID,
		"message":       "Kaufland connected successfully",
	})
}

// ── Categories ────────────────────────────────────────────────────────────────

// GetCategories returns the Kaufland category tree.
// GET /kaufland/categories
func (h *KauflandHandler) GetCategories(c *gin.Context) {
	client, _, err := h.getKauflandClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error(), "categories": []interface{}{}})
		return
	}

	cats, err := client.GetCategories()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error(), "categories": []interface{}{}})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":         true,
		"categories": cats,
		"count":      len(cats),
	})
}

// ── Prepare & Submit ──────────────────────────────────────────────────────────

// PrepareListingDraft loads MarketMate product data and builds a Kaufland unit draft.
// POST /kaufland/prepare  { "product_id": "...", "credential_id": "..." }
func (h *KauflandHandler) PrepareListingDraft(c *gin.Context) {
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

	// Extract price, defaulting to 0
	price := 0.0
	if p, ok := product.Attributes["price"]; ok {
		switch v := p.(type) {
		case float64:
			price = v
		case string:
			price, _ = strconv.ParseFloat(v, 64)
		}
	}

	qty := 0
	if q, ok := product.Attributes["quantity"]; ok {
		switch v := q.(type) {
		case float64:
			qty = int(v)
		case int:
			qty = v
		}
	}

	ean := ""
	if e, ok := product.Attributes["ean"]; ok {
		if s, ok := e.(string); ok {
			ean = s
		}
	}
	if ean == "" {
		if e, ok := product.Attributes["gtin"]; ok {
			if s, ok := e.(string); ok {
				ean = s
			}
		}
	}

	draft := gin.H{
		"ean":                   ean,
		"listing_price":         price,
		"minimum_price":         0.0,
		"amount":                qty,
		"condition":             1,
		"note":                  product.Description,
		"shipping_group":        "",
		"handling_time_in_days": 1,
		"variants":              loadChannelVariants(c.Request.Context(), h.productRepo, tenantID, req.ProductID, fmt.Sprintf("%.2f", price), ""),
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":         true,
		"product_id": req.ProductID,
		"draft":      draft,
	})
}

// SubmitUnit creates one or more units (listings) on Kaufland.
// POST /kaufland/submit
//
// When the payload contains a "variants" array with ≥2 active entries, one
// Kaufland Unit is created per active variant (each needs its own EAN). Variants
// without an EAN are skipped. When no variants are present, the original
// single-unit flow is used.
//
// ⚠️ Kaufland does not support grouped variation listings. Each variant is
// submitted as a separate product. A notice is shown in the frontend.
func (h *KauflandHandler) SubmitUnit(c *gin.Context) {
	client, _, err := h.getKauflandClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	// Accept a superset payload that includes optional variants array
	var req struct {
		kaufland.CreateUnitRequest
		Variants []ChannelVariantDraft `json:"variants"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "invalid payload: " + err.Error()})
		return
	}

	// Collect active variants with EANs
	activeVariants := make([]ChannelVariantDraft, 0)
	for _, v := range req.Variants {
		if v.Active {
			activeVariants = append(activeVariants, v)
		}
	}

	// Multi-unit path: ≥2 active variants provided
	if len(activeVariants) >= 2 {
		type unitResult struct {
			SKU    string `json:"sku"`
			EAN    string `json:"ean"`
			UnitID string `json:"unit_id,omitempty"`
			Error  string `json:"error,omitempty"`
		}
		submitted := []unitResult{}
		skipped := []unitResult{}
		errorsList := []unitResult{}

		for _, v := range activeVariants {
			if v.EAN == "" {
				skipped = append(skipped, unitResult{SKU: v.SKU, EAN: "", Error: "no EAN — Kaufland requires EAN per variant"})
				continue
			}
			price := req.ListingPriceAmount
			if p, err := strconv.ParseFloat(v.Price, 64); err == nil && p > 0 {
				price = p
			}
			qty := req.Amount
			if q, err := strconv.Atoi(v.Stock); err == nil && q >= 0 {
				qty = q
			}
			unitReq := kaufland.CreateUnitRequest{
				EAN:                v.EAN,
				ConditionID:        req.ConditionID,
				ListingPriceAmount: price,
				MinimumPriceAmount: req.MinimumPriceAmount,
				Note:               req.Note,
				Amount:             qty,
				ShippingGroup:      req.ShippingGroup,
				Handling:           req.Handling,
			}
			unit, err := client.CreateUnit(unitReq)
			if err != nil {
				errorsList = append(errorsList, unitResult{SKU: v.SKU, EAN: v.EAN, Error: err.Error()})
				continue
			}
			submitted = append(submitted, unitResult{SKU: v.SKU, EAN: v.EAN, UnitID: unit.UnitID})
			log.Printf("[Kaufland Submit] Unit created for variant SKU=%s EAN=%s unitID=%s", v.SKU, v.EAN, unit.UnitID)
		}

		c.JSON(http.StatusOK, gin.H{
			"ok":        len(submitted) > 0,
			"submitted": submitted,
			"skipped":   skipped,
			"errors":    errorsList,
			"message":   fmt.Sprintf("%d/%d variants submitted as individual units", len(submitted), len(activeVariants)),
		})
		return
	}

	// Single-unit path (original behaviour)
	if req.EAN == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "ean is required"})
		return
	}
	if req.ListingPriceAmount <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "listing_price must be greater than 0"})
		return
	}

	unit, err := client.CreateUnit(req.CreateUnitRequest)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"unit_id": unit.UnitID,
		"ean":     unit.EAN,
		"status":  unit.Status,
		"message": "Unit created on Kaufland",
	})
}

// UpdateUnit updates an existing unit.
// PATCH /kaufland/units/:id
func (h *KauflandHandler) UpdateUnit(c *gin.Context) {
	client, _, err := h.getKauflandClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	unitID := c.Param("id")
	if unitID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "unit id is required"})
		return
	}

	var req kaufland.UpdateUnitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "invalid payload: " + err.Error()})
		return
	}

	if err := client.UpdateUnit(unitID, req); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "unit_id": unitID, "message": "Unit updated"})
}

// DeleteUnit removes a unit from Kaufland.
// DELETE /kaufland/units/:id
func (h *KauflandHandler) DeleteUnit(c *gin.Context) {
	client, _, err := h.getKauflandClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	unitID := c.Param("id")
	if unitID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "unit id is required"})
		return
	}

	if err := client.DeleteUnit(unitID); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "deleted_unit_id": unitID})
}

// GetUnits returns a paginated list of seller units.
// GET /kaufland/units?offset=0&limit=50
func (h *KauflandHandler) GetUnits(c *gin.Context) {
	client, _, err := h.getKauflandClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error(), "units": []interface{}{}})
		return
	}

	offset := 0
	limit := 50
	if o := c.Query("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil {
			offset = v
		}
	}
	if l := c.Query("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 100 {
			limit = v
		}
	}

	units, total, err := client.GetUnits(offset, limit)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error(), "units": []interface{}{}})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":     true,
		"units":  units,
		"total":  total,
		"offset": offset,
		"limit":  limit,
	})
}
