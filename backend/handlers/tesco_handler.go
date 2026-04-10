package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"module-a/marketplace/clients/tesco"
	"module-a/repository"
	"module-a/services"
)

// ============================================================================
// TESCO HANDLER
// ============================================================================
// Handles Tesco-specific listing and category endpoints.
// NOTE: The live Tesco integration uses the generic Mirakl handler (/api/v1/mirakl).
// This handler is retained for legacy compatibility but its routes are not
// currently registered in main.go. Do not add a TescoListingCreate.tsx frontend
// page pointing at /tesco/* — use the Mirakl handler instead.
// ============================================================================

type TescoHandler struct {
	marketplaceService *services.MarketplaceService
	repo               *repository.MarketplaceRepository
	productRepo        *repository.FirestoreRepository
}

func NewTescoHandler(
	marketplaceService *services.MarketplaceService,
	repo *repository.MarketplaceRepository,
	productRepo *repository.FirestoreRepository,
) *TescoHandler {
	return &TescoHandler{
		marketplaceService: marketplaceService,
		repo:               repo,
		productRepo:        productRepo,
	}
}

// getTescoClient resolves credentials and builds a Tesco API client
func (h *TescoHandler) getTescoClient(c *gin.Context) (*tesco.Client, error) {
	tenantID := c.GetString("tenant_id")
	credentialID := c.Query("credential_id")
	if credentialID == "" {
		credentialID = c.GetHeader("X-Credential-ID")
	}

	cred, err := h.marketplaceService.GetCredential(c.Request.Context(), tenantID, credentialID)
	if err != nil {
		return nil, err
	}

	merged, err := h.marketplaceService.GetFullCredentials(c.Request.Context(), cred)
	if err != nil {
		return nil, err
	}

	return tesco.NewClient(merged["client_id"], merged["client_secret"], merged["seller_id"]), nil
}

// GET /tesco/categories
func (h *TescoHandler) GetCategories(c *gin.Context) {
	client, err := h.getTescoClient(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "failed to load credentials: " + err.Error()})
		return
	}

	categories, err := client.GetCategories()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"categories": categories})
}

// GET /tesco/template?category_id=xxx
func (h *TescoHandler) GetTemplate(c *gin.Context) {
	categoryID := c.Query("category_id")
	if categoryID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "category_id is required"})
		return
	}

	client, err := h.getTescoClient(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "failed to load credentials: " + err.Error()})
		return
	}

	template, err := client.GetProductTemplate(categoryID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"template": template})
}

// POST /tesco/prepare — maps enriched product data to Tesco listing fields
func (h *TescoHandler) PrepareTescoListing(c *gin.Context) {
	var req struct {
		ProductID  string                 `json:"product_id"`
		CategoryID string                 `json:"category_id"`
		Fields     map[string]interface{} `json:"fields"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Build the prepared payload from provided fields
	payload := map[string]interface{}{
		"product_id":  req.ProductID,
		"category_id": req.CategoryID,
	}
	for k, v := range req.Fields {
		payload[k] = v
	}

	c.JSON(http.StatusOK, gin.H{"prepared": payload, "ready_to_submit": true})
}

// POST /tesco/submit — submits a listing to Tesco
func (h *TescoHandler) SubmitTescoListing(c *gin.Context) {
	var req struct {
		GTIN        string                 `json:"gtin"`
		Title       string                 `json:"title" binding:"required"`
		Description string                 `json:"description"`
		Price       float64                `json:"price" binding:"required"`
		Currency    string                 `json:"currency"`
		CategoryID  string                 `json:"category_id"`
		Brand       string                 `json:"brand"`
		PackSize    string                 `json:"pack_size"`
		Department  string                 `json:"department"`
		ImageURLs   []string               `json:"image_urls"`
		SupplierInfo map[string]interface{} `json:"supplier_info"`
		Extra       map[string]interface{} `json:"extra"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Currency == "" {
		req.Currency = "GBP"
	}

	client, err := h.getTescoClient(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "failed to load credentials: " + err.Error()})
		return
	}

	payload := map[string]interface{}{
		"title":       req.Title,
		"description": req.Description,
		"price":       req.Price,
		"currency":    req.Currency,
		"categoryId":  req.CategoryID,
		"brand":       req.Brand,
		"packSize":    req.PackSize,
		"department":  req.Department,
		"imageUrls":   req.ImageURLs,
	}
	if req.GTIN != "" {
		payload["gtin"] = req.GTIN
	}
	if req.SupplierInfo != nil {
		payload["supplierInfo"] = req.SupplierInfo
	}
	for k, v := range req.Extra {
		payload[k] = v
	}

	resp, err := client.SubmitProduct(payload)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "tesco api error: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "response": resp})
}
