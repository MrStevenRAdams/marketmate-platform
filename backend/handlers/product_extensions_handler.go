package handlers

import (
	"fmt"
	"net/http"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"google.golang.org/api/iterator"
)

// ============================================================================
// PRODUCT EXTENSIONS HANDLER
// ============================================================================
// Provides endpoints for:
//   5.6  Extended Properties  GET/PUT /products/:id/extended-properties
//   5.7  Product Identifiers  PUT /products/:id/identifiers
//   5.10 Stock History        GET /products/:id/stock-history
//   5.13 Supplier Codes       handled via existing supplier fields + product update
//   5.14 Item Stats           GET /products/:id/stats
// ============================================================================

type ProductExtensionsHandler struct {
	client *firestore.Client
}

func NewProductExtensionsHandler(client *firestore.Client) *ProductExtensionsHandler {
	return &ProductExtensionsHandler{client: client}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func (h *ProductExtensionsHandler) productRef(tenantID, productID string) *firestore.DocumentRef {
	return h.client.Collection("tenants").Doc(tenantID).Collection("products").Doc(productID)
}

// ── GET /api/v1/products/:id/extended-properties ─────────────────────────────

func (h *ProductExtensionsHandler) GetExtendedProperties(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	productID := c.Param("id")
	ctx := c.Request.Context()

	doc, err := h.productRef(tenantID, productID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "product not found"})
		return
	}
	data := doc.Data()
	props, _ := data["extended_properties"].(map[string]interface{})
	if props == nil {
		props = map[string]interface{}{}
	}
	c.JSON(http.StatusOK, gin.H{"extended_properties": props})
}

// ── PUT /api/v1/products/:id/extended-properties ─────────────────────────────

func (h *ProductExtensionsHandler) UpdateExtendedProperties(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	productID := c.Param("id")
	ctx := c.Request.Context()

	var req struct {
		Properties map[string]string `json:"properties"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	_, err := h.productRef(tenantID, productID).Update(ctx, []firestore.Update{
		{Path: "extended_properties", Value: req.Properties},
		{Path: "updated_at", Value: time.Now()},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"extended_properties": req.Properties})
}

// ── PUT /api/v1/products/:id/identifiers ─────────────────────────────────────

func (h *ProductExtensionsHandler) UpdateIdentifiers(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	productID := c.Param("id")
	ctx := c.Request.Context()

	var req map[string]interface{}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	_, err := h.productRef(tenantID, productID).Update(ctx, []firestore.Update{
		{Path: "identifiers", Value: req},
		{Path: "updated_at", Value: time.Now()},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update identifiers"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"identifiers": req})
}

// ── GET /api/v1/products/:id/stock-history ────────────────────────────────────

func (h *ProductExtensionsHandler) GetStockHistory(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	productID := c.Param("id")
	ctx := c.Request.Context()

	limitStr := c.DefaultQuery("limit", "100")
	limit := 100
	if n, err := parseInt(limitStr); err == nil && n > 0 && n <= 500 {
		limit = n
	}

	var adjustments []map[string]interface{}
	iter := h.client.Collection("tenants").Doc(tenantID).Collection("inventory_adjustments").
		Where("product_id", "==", productID).
		OrderBy("created_at", firestore.Desc).
		Limit(limit).
		Documents(ctx)
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		adjustments = append(adjustments, doc.Data())
	}
	if adjustments == nil {
		adjustments = []map[string]interface{}{}
	}
	c.JSON(http.StatusOK, gin.H{"adjustments": adjustments, "total": len(adjustments)})
}

// ── GET /api/v1/products/:id/stats ───────────────────────────────────────────

type ProductStats struct {
	Sold30d       int     `json:"sold_30d"`
	Sold90d       int     `json:"sold_90d"`
	Sold365d      int     `json:"sold_365d"`
	Revenue30d    float64 `json:"revenue_30d"`
	Revenue90d    float64 `json:"revenue_90d"`
	Revenue365d   float64 `json:"revenue_365d"`
	ReturnCount   int     `json:"return_count"`
	AvgSalePrice  float64 `json:"avg_sale_price"`
}

func (h *ProductExtensionsHandler) GetStats(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	productID := c.Param("id")
	ctx := c.Request.Context()

	now := time.Now()
	cutoff365 := now.AddDate(-1, 0, 0)

	var stats ProductStats
	var totalQty float64
	var totalRevenue float64
	var saleCount int

	iter := h.client.Collection("tenants").Doc(tenantID).Collection("order_lines").
		Where("product_id", "==", productID).
		Where("created_at", ">=", cutoff365).
		Documents(ctx)
	defer iter.Stop()

	cutoff30 := now.AddDate(0, 0, -30)
	cutoff90 := now.AddDate(0, 0, -90)

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		data := doc.Data()
		qty := getFloat(data, "quantity")
		price := getFloat(data, "unit_price")
		lineTotal := qty * price

		var createdAt time.Time
		if v, ok := data["created_at"].(time.Time); ok {
			createdAt = v
		}

		stats.Sold365d += int(qty)
		stats.Revenue365d += lineTotal

		if createdAt.After(cutoff90) {
			stats.Sold90d += int(qty)
			stats.Revenue90d += lineTotal
		}
		if createdAt.After(cutoff30) {
			stats.Sold30d += int(qty)
			stats.Revenue30d += lineTotal
		}

		totalQty += qty
		totalRevenue += lineTotal
		saleCount++
	}

	// Count returns from RMA lines
	iterRMA := h.client.Collection("tenants").Doc(tenantID).Collection("rmas").
		Where("lines", "array-contains", map[string]interface{}{"product_id": productID}).
		Documents(ctx)
	for {
		_, err := iterRMA.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		stats.ReturnCount++
	}
	iterRMA.Stop()

	if saleCount > 0 && totalQty > 0 {
		stats.AvgSalePrice = totalRevenue / totalQty
	}

	c.JSON(http.StatusOK, gin.H{"stats": stats})
}

// ── GET /api/v1/products/:id/ktypes ──────────────────────────────────────────

func (h *ProductExtensionsHandler) GetKTypes(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	productID := c.Param("id")
	ctx := c.Request.Context()

	doc, err := h.productRef(tenantID, productID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "product not found"})
		return
	}
	data := doc.Data()
	ktypes, _ := data["ktypes"].([]interface{})
	if ktypes == nil {
		ktypes = []interface{}{}
	}
	c.JSON(http.StatusOK, gin.H{"ktypes": ktypes})
}

// ── PUT /api/v1/products/:id/ktypes ──────────────────────────────────────────

func (h *ProductExtensionsHandler) UpdateKTypes(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	productID := c.Param("id")
	ctx := c.Request.Context()

	var req struct {
		KTypes []map[string]interface{} `json:"ktypes"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	_, err := h.productRef(tenantID, productID).Update(ctx, []firestore.Update{
		{Path: "ktypes", Value: req.KTypes},
		{Path: "updated_at", Value: time.Now()},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update ktypes"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ktypes": req.KTypes})
}

// ── GET /api/v1/products/:id/wms-config ──────────────────────────────────────
// Returns WMS settings for this product plus all binracks currently assigned.

func (h *ProductExtensionsHandler) GetWMSConfig(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	productID := c.Param("id")
	ctx := c.Request.Context()

	doc, err := h.productRef(tenantID, productID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "product not found"})
		return
	}
	data := doc.Data()

	storageGroup, _ := data["storage_group"].(string)
	binrackRestrictions, _ := data["binrack_restrictions"].([]interface{})
	allowedBinrackTypes, _ := data["allowed_binrack_types"].([]interface{})

	// Convert []interface{} → []string
	toStrSlice := func(raw []interface{}) []string {
		out := make([]string, 0, len(raw))
		for _, v := range raw {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}

	// Query binracks that hold this product
	var assignedBinracks []map[string]interface{}
	iter := h.client.Collection("tenants").Doc(tenantID).Collection("binracks").
		Where("assigned_product_ids", "array-contains", productID).
		Documents(ctx)
	defer iter.Stop()
	for {
		bdoc, berr := iter.Next()
		if berr == iterator.Done {
			break
		}
		if berr != nil {
			break
		}
		assignedBinracks = append(assignedBinracks, bdoc.Data())
	}
	if assignedBinracks == nil {
		assignedBinracks = []map[string]interface{}{}
	}

	c.JSON(http.StatusOK, gin.H{
		"storage_group":         storageGroup,
		"binrack_restrictions":  toStrSlice(binrackRestrictions),
		"allowed_binrack_types": toStrSlice(allowedBinrackTypes),
		"assigned_binracks":     assignedBinracks,
	})
}

// ── PUT /api/v1/products/:id/wms-config ──────────────────────────────────────

func (h *ProductExtensionsHandler) UpdateWMSConfig(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	productID := c.Param("id")
	ctx := c.Request.Context()

	var req struct {
		StorageGroup        string   `json:"storage_group"`
		BinrackRestrictions []string `json:"binrack_restrictions"`
		AllowedBinrackTypes []string `json:"allowed_binrack_types"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	updates := []firestore.Update{
		{Path: "storage_group", Value: req.StorageGroup},
		{Path: "updated_at", Value: time.Now()},
	}
	if req.BinrackRestrictions != nil {
		updates = append(updates, firestore.Update{Path: "binrack_restrictions", Value: req.BinrackRestrictions})
	}
	if req.AllowedBinrackTypes != nil {
		updates = append(updates, firestore.Update{Path: "allowed_binrack_types", Value: req.AllowedBinrackTypes})
	}

	if _, err := h.productRef(tenantID, productID).Update(ctx, updates); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update WMS config"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"updated": true})
}

// ── GET /api/v1/products/:id/ai-debug ────────────────────────────────────────
// Returns the full product document + all extended_data subcollection branches.
// Shows raw SP-API / eBay response data for debugging AI product creation.

func (h *ProductExtensionsHandler) GetAIDebug(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	productID := c.Param("id")
	ctx := c.Request.Context()

	// Full product document
	doc, err := h.productRef(tenantID, productID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "product not found"})
		return
	}

	// All extended_data branches (one per source key: ebay_ean_xxx, amazon_asin_xxx etc)
	var branches []map[string]interface{}
	iter := h.productRef(tenantID, productID).Collection("extended_data").Documents(ctx)
	defer iter.Stop()
	for {
		bdoc, berr := iter.Next()
		if berr == iterator.Done {
			break
		}
		if berr != nil {
			break
		}
		branch := bdoc.Data()
		branch["_source_key"] = bdoc.Ref.ID
		branches = append(branches, branch)
	}
	if branches == nil {
		branches = []map[string]interface{}{}
	}

	c.JSON(http.StatusOK, gin.H{
		"product":       doc.Data(),
		"extended_data": branches,
		"branch_count":  len(branches),
	})
}


// ── helpers ───────────────────────────────────────────────────────────────────

func parseInt(s string) (int, error) {
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err
}

func getFloat(data map[string]interface{}, key string) float64 {
	switch v := data[key].(type) {
	case float64:
		return v
	case int64:
		return float64(v)
	}
	return 0
}
