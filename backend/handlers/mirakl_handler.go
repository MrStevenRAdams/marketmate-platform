package handlers

// ============================================================================
// MIRAKL HANDLER
// ============================================================================
// Shared HTTP endpoints for ALL Mirakl-powered marketplaces.
// Routes are mounted under /api/v1/mirakl and /api/v1/{marketplace_id}.
//
// Endpoints:
//   GET  /mirakl/categories          - Fetch category tree from marketplace
//   POST /mirakl/offers/upsert       - Create or update offers (price+stock)
//   POST /mirakl/offers/delete       - Delete offers by SKU
//   GET  /mirakl/offers              - List seller's current offers
//   POST /mirakl/products/submit     - Submit product(s) to marketplace catalog
//   GET  /mirakl/carriers            - List registered shipping carriers
//   GET  /mirakl/health              - Test connection / verify API key
//   GET  /mirakl/shop                - Get seller account info
// ============================================================================

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"module-a/marketplace/clients/mirakl"
	"module-a/repository"
	"module-a/services"
)

// MiraklHandler handles shared Mirakl marketplace API endpoints.
type MiraklHandler struct {
	marketplaceService *services.MarketplaceService
	marketplaceRepo    *repository.MarketplaceRepository
}

// NewMiraklHandler creates a new MiraklHandler.
func NewMiraklHandler(marketplaceService *services.MarketplaceService, marketplaceRepo *repository.MarketplaceRepository) *MiraklHandler {
	return &MiraklHandler{
		marketplaceService: marketplaceService,
		marketplaceRepo:    marketplaceRepo,
	}
}

// ============================================================================
// CREDENTIAL RESOLUTION
// ============================================================================

// getMiraklClient resolves credentials and builds a Mirakl client.
// Credential lookup: ?credential_id= or X-Credential-Id header.
// The marketplace_id is resolved from the stored credential's channel field.
func (h *MiraklHandler) getMiraklClient(c *gin.Context) (*mirakl.Client, error) {
	tenantID := c.GetString("tenant_id")
	credentialID := c.Query("credential_id")
	if credentialID == "" {
		credentialID = c.GetHeader("X-Credential-Id")
	}
	if credentialID == "" {
		return nil, errMissingCredential
	}

	cred, err := h.marketplaceService.GetCredential(c.Request.Context(), tenantID, credentialID)
	if err != nil {
		return nil, err
	}

	merged, err := h.marketplaceService.GetFullCredentials(c.Request.Context(), cred)
	if err != nil {
		return nil, err
	}

	apiKey := merged["api_key"]
	if apiKey == "" {
		return nil, errInvalidCredential("api_key is missing")
	}

	// Derive marketplace_id: prefer explicit field, fall back to channel
	marketplaceID := merged["marketplace_id"]
	if marketplaceID == "" {
		marketplaceID = cred.Channel
	}

	baseURL := merged["base_url"]
	shopID := merged["shop_id"]

	return mirakl.NewClientForMarketplace(marketplaceID, apiKey, shopID, baseURL), nil
}

// ============================================================================
// HEALTH / CONNECTION TEST
// ============================================================================

// HealthCheck — GET /mirakl/health
// Tests the API key and instance URL. Returns shop info on success.
func (h *MiraklHandler) HealthCheck(c *gin.Context) {
	client, err := h.getMiraklClient(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	info, err := client.GetShopInfo()
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"connected": false,
			"error":     err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"connected": true,
		"shop_id":   info.ShopID,
		"shop_name": info.ShopName,
		"state":     info.State,
	})
}

// ============================================================================
// SHOP INFO
// ============================================================================

// GetShopInfo — GET /mirakl/shop
func (h *MiraklHandler) GetShopInfo(c *gin.Context) {
	client, err := h.getMiraklClient(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	info, err := client.GetShopInfo()
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"shop": info})
}

// ============================================================================
// CATEGORIES
// ============================================================================

// GetCategories — GET /mirakl/categories
// Returns the full category hierarchy for the marketplace instance.
func (h *MiraklHandler) GetCategories(c *gin.Context) {
	client, err := h.getMiraklClient(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	cats, err := client.GetCategories()
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"categories": cats,
		"count":      len(cats),
	})
}

// ============================================================================
// CARRIERS
// ============================================================================

// GetCarriers — GET /mirakl/carriers
// Returns shipping carriers registered on this Mirakl instance.
// Use the carrier codes when pushing tracking back (OR23).
func (h *MiraklHandler) GetCarriers(c *gin.Context) {
	client, err := h.getMiraklClient(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	carriers, err := client.GetCarriers()
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"carriers": carriers})
}

// ============================================================================
// OFFERS — LIST
// ============================================================================

// ListOffers — GET /mirakl/offers
// Lists the seller's current offers with optional pagination.
// Query params: offset (default 0), max (default 100), sku (filter by shop SKU)
func (h *MiraklHandler) ListOffers(c *gin.Context) {
	client, err := h.getMiraklClient(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	max, _ := strconv.Atoi(c.DefaultQuery("max", "100"))
	sku := c.Query("sku")

	resp, err := client.ListOffers(mirakl.ListOffersOptions{
		Offset: offset,
		Max:    max,
		SKU:    sku,
	})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"offers":      resp.Offers,
		"total_count": resp.TotalCount,
		"offset":      resp.Offset,
	})
}

// ============================================================================
// OFFERS — UPSERT
// ============================================================================

// upsertOffersRequest is the JSON body for UpsertOffers
type upsertOffersRequest struct {
	CredentialID string               `json:"credential_id"`
	Offers       []mirakl.OfferUpsert `json:"offers"`
}

// UpsertOffers — POST /mirakl/offers/upsert
// Creates or updates offers (price, quantity, state).
// Body: { credential_id, offers: [{shop-sku, price, quantity, ...}] }
func (h *MiraklHandler) UpsertOffers(c *gin.Context) {
	var req upsertOffersRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(req.Offers) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "offers array cannot be empty"})
		return
	}

	client, err := h.getMiraklClient(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	resp, err := client.UpsertOffers(req.Offers)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"offer_import_id": resp.OfferImportID,
		"count":           len(req.Offers),
	})
}

// ============================================================================
// OFFERS — DELETE
// ============================================================================

// deleteOffersRequest is the JSON body for DeleteOffers
type deleteOffersRequest struct {
	CredentialID string   `json:"credential_id"`
	SKUs         []string `json:"skus"` // Shop SKUs to delete
}

// DeleteOffers — POST /mirakl/offers/delete
// Removes offers by setting update-delete = "delete" for the given SKUs.
func (h *MiraklHandler) DeleteOffers(c *gin.Context) {
	var req deleteOffersRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(req.SKUs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "skus array cannot be empty"})
		return
	}

	client, err := h.getMiraklClient(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	upserts := make([]mirakl.OfferUpsert, 0, len(req.SKUs))
	for _, sku := range req.SKUs {
		upserts = append(upserts, mirakl.OfferUpsert{
			ShopSKU:      sku,
			UpdateDelete: "delete",
		})
	}

	resp, err := client.UpsertOffers(upserts)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"offer_import_id": resp.OfferImportID,
		"deleted_count":   len(req.SKUs),
	})
}

// ============================================================================
// PRODUCTS — SUBMIT TO CATALOG
// ============================================================================

// submitProductsRequest is the JSON body for SubmitProducts
type submitProductsRequest struct {
	CredentialID string                  `json:"credential_id"`
	Products     []mirakl.ProductPayload `json:"products"`
}

// SubmitProducts — POST /mirakl/products/submit
// Submits one or more products to the Mirakl marketplace catalog.
// Products are processed asynchronously; returns an import_id to poll.
//
// Each product requires: shop-sku, product-title, category, price, quantity.
// Optional: brand, description, media-url[], attributes[].
func (h *MiraklHandler) SubmitProducts(c *gin.Context) {
	var req submitProductsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(req.Products) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "products array cannot be empty"})
		return
	}

	client, err := h.getMiraklClient(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	status, err := client.ImportProducts(mirakl.ProductImportRequest{
		Products: req.Products,
	})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"import_id": status.ImportID,
		"status":    status.Status,
		"message":   "Product import submitted. Poll /mirakl/products/import/{import_id} for status.",
	})
}

// GetImportStatus — GET /mirakl/products/import/:import_id
// Polls the status of an asynchronous product import job (P42).
func (h *MiraklHandler) GetImportStatus(c *gin.Context) {
	importID := c.Param("import_id")
	if importID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "import_id is required"})
		return
	}

	client, err := h.getMiraklClient(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	status, err := client.GetImportStatus(importID)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"import_id":        status.ImportID,
		"status":           status.Status,
		"products_created": status.ProductsCreated,
		"products_updated": status.ProductsUpdated,
		"error_count":      status.ErrorCount,
		"lines_read":       status.LinesRead,
		"has_error_report": status.HasErrorReport,
	})
}

// ============================================================================
// INVOICES
// ============================================================================

// ListInvoices — GET /mirakl/invoices
// Returns accounting documents (invoices, credit notes) from the marketplace.
// Query params: start_date, end_date (ISO 8601 format)
func (h *MiraklHandler) ListInvoices(c *gin.Context) {
	client, err := h.getMiraklClient(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	invoices, err := client.ListInvoices(c.Query("start_date"), c.Query("end_date"))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"invoices": invoices,
		"count":    len(invoices),
	})
}

// ============================================================================
// ERROR HELPERS (shared with other mirakl handler files)
// ============================================================================

var errMissingCredential = errInvalidCredential("credential_id is required (query param or X-Credential-Id header)")

type credentialError string

func (e credentialError) Error() string { return string(e) }

func errInvalidCredential(msg string) error { return credentialError(msg) }
