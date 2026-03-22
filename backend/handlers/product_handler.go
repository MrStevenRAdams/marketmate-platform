package handlers

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"module-a/models"
	"module-a/services"
)

type ProductHandler struct {
	ProductService *services.ProductService
	SearchService  *services.SearchService // Typesense auto-sync
}

func NewProductHandler(productService *services.ProductService, searchService *services.SearchService) *ProductHandler {
	return &ProductHandler{
		ProductService: productService,
		SearchService:  searchService,
	}
}

// CreateProduct creates a new product
func (h *ProductHandler) CreateProduct(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req models.CreateProductRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	product := &models.Product{
		ProductID:          uuid.New().String(),
		TenantID:           tenantID,
		Status:             "draft",
		SKU:                req.SKU,
		Title:              req.Title,
		Subtitle:           req.Subtitle,
		Description:        req.Description,
		Brand:              req.Brand,
		ProductType:        req.ProductType,
		ParentID:           req.ParentID,
		Identifiers:        req.Identifiers,
		CategoryIDs:        req.CategoryIDs,
		Tags:               req.Tags,
		KeyFeatures:        req.KeyFeatures,
		AttributeSetID:     req.AttributeSetID,
		Attributes:         req.Attributes,
		Assets:             req.Assets,
		Dimensions:         req.Dimensions,
		Weight:             req.Weight,
		ShippingDimensions: req.ShippingDimensions,
		ShippingWeight:     req.ShippingWeight,
		BundleComponents:   req.BundleComponents,
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}

	if err := h.ProductService.CreateProduct(c.Request.Context(), product); err != nil {
		log.Printf("Failed to create product: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create product"})
		return
	}

	// Auto-sync to Typesense (best-effort, don't fail the request)
	if h.SearchService != nil {
		if err := h.SearchService.IndexProduct(product); err != nil {
			log.Printf("⚠️  Typesense index failed for product %s: %v", product.ProductID, err)
		}
	}

	c.JSON(http.StatusCreated, gin.H{"data": product})
}

// GetProduct retrieves a product by ID
func (h *ProductHandler) GetProduct(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	productID := c.Param("id")

	product, err := h.ProductService.GetProduct(c.Request.Context(), tenantID, productID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Product not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": product})
}

// UpdateProduct updates a product
func (h *ProductHandler) UpdateProduct(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	productID := c.Param("id")

	var req models.UpdateProductRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	updates := make(map[string]interface{})
	if req.SKU != nil {
		updates["sku"] = *req.SKU
	}
	if req.Title != nil {
		updates["title"] = *req.Title
	}
	if req.Subtitle != nil {
		updates["subtitle"] = *req.Subtitle
	}
	if req.Description != nil {
		updates["description"] = *req.Description
	}
	if req.Brand != nil {
		updates["brand"] = *req.Brand
	}
	if req.Status != nil {
		updates["status"] = *req.Status
	}
	if req.CategoryIDs != nil {
		updates["category_ids"] = req.CategoryIDs
	}
	if req.Tags != nil {
		updates["tags"] = req.Tags
	}
	if req.KeyFeatures != nil {
		updates["key_features"] = req.KeyFeatures
	}
	if req.Attributes != nil {
		updates["attributes"] = req.Attributes
	}
	if req.Assets != nil {
		updates["assets"] = req.Assets
	}
	if req.Dimensions != nil {
		updates["dimensions"] = req.Dimensions
	}
	if req.Weight != nil {
		updates["weight"] = req.Weight
	}
	if req.ShippingDimensions != nil {
		updates["shipping_dimensions"] = req.ShippingDimensions
	}
	if req.ShippingWeight != nil {
		updates["shipping_weight"] = req.ShippingWeight
	}

	if err := h.ProductService.UpdateProduct(c.Request.Context(), tenantID, productID, updates); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update product"})
		return
	}

	// Auto-sync to Typesense (best-effort — re-fetch updated product for full doc)
	if h.SearchService != nil {
		if updated, err := h.ProductService.GetProduct(c.Request.Context(), tenantID, productID); err == nil {
			if err := h.SearchService.IndexProduct(updated); err != nil {
				log.Printf("⚠️  Typesense index failed for product %s: %v", productID, err)
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "Product updated successfully"})
}

// DeleteProduct deletes a product
func (h *ProductHandler) DeleteProduct(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	productID := c.Param("id")

	if err := h.ProductService.DeleteProduct(c.Request.Context(), tenantID, productID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete product"})
		return
	}

	// Remove from Typesense (best-effort)
	if h.SearchService != nil {
		if err := h.SearchService.DeleteProduct(productID); err != nil {
			log.Printf("⚠️  Typesense delete failed for product %s: %v", productID, err)
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "Product deleted successfully"})
}

// ListProducts lists products with pagination
func (h *ProductHandler) ListProducts(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	// Parse query parameters
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	status := c.Query("status")
	search := c.Query("search")
	barcode := c.Query("barcode")
	parentID := c.Query("parent_id")
	parentASIN := c.Query("parent_asin")

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	offset := (page - 1) * pageSize

	filters := make(map[string]interface{})
	if status != "" {
		filters["status"] = status
	}
	if search != "" {
		filters["search"] = search
	}
	if barcode != "" {
		filters["barcode"] = barcode
	}
	if parentID != "" {
		filters["parent_id"] = parentID
	}
	if parentASIN != "" {
		filters["parent_asin"] = parentASIN
	}

	products, total, err := h.ProductService.ListProducts(c.Request.Context(), tenantID, filters, pageSize, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list products"})
		return
	}

	totalPages := int(total) / pageSize
	if int(total)%pageSize != 0 {
		totalPages++
	}

	c.JSON(http.StatusOK, gin.H{
		"data": products,
		"pagination": gin.H{
			"total":       total,
			"page":        page,
			"page_size":   pageSize,
			"total_pages": totalPages,
		},
	})
}

// CreateVariant creates a new variant for a product
func (h *ProductHandler) CreateVariant(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	productID := c.Param("id")

	var req models.CreateVariantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	variant := &models.Variant{
		VariantID:   uuid.New().String(),
		TenantID:    tenantID,
		ProductID:   productID,
		SKU:         req.SKU,
		Alias:       req.Alias,
		Barcode:     req.Barcode,
		Title:       req.Title,
		Identifiers: req.Identifiers,
		Attributes:  req.Attributes,
		Status:      "active",
		Pricing:     req.Pricing,
		Dimensions:  req.Dimensions,
		Weight:      req.Weight,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := h.ProductService.CreateVariant(c.Request.Context(), variant); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create variant"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": variant})
}

// ListVariants lists variants for a product
func (h *ProductHandler) ListVariants(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	productID := c.Param("id")

	filters := map[string]interface{}{
		"product_id": productID,
	}

	variants, total, err := h.ProductService.ListVariants(c.Request.Context(), tenantID, filters, 100, 0)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list variants"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": variants,
		"pagination": gin.H{
			"total": total,
		},
	})
}

// ListAllVariants lists all variants
func (h *ProductHandler) ListAllVariants(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	offset := (page - 1) * pageSize

	variants, total, err := h.ProductService.ListVariants(c.Request.Context(), tenantID, map[string]interface{}{}, pageSize, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list variants"})
		return
	}

	totalPages := int(total) / pageSize
	if int(total)%pageSize != 0 {
		totalPages++
	}

	c.JSON(http.StatusOK, gin.H{
		"data": variants,
		"pagination": gin.H{
			"total":       total,
			"page":        page,
			"page_size":   pageSize,
			"total_pages": totalPages,
		},
	})
}

// GetVariant retrieves a variant by ID
func (h *ProductHandler) GetVariant(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	variantID := c.Param("id")

	variant, err := h.ProductService.GetVariant(c.Request.Context(), tenantID, variantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Variant not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": variant})
}

// UpdateVariant updates a variant
func (h *ProductHandler) UpdateVariant(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	variantID := c.Param("id")

	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.ProductService.UpdateVariant(c.Request.Context(), tenantID, variantID, updates); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update variant"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Variant updated successfully"})
}

// DeleteVariant deletes a variant
func (h *ProductHandler) DeleteVariant(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	variantID := c.Param("id")

	if err := h.ProductService.DeleteVariant(c.Request.Context(), tenantID, variantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete variant"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Variant deleted successfully"})
}

// GenerateVariants generates variants from attribute combinations
func (h *ProductHandler) GenerateVariants(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	productID := c.Param("id")

	var req models.GenerateVariantsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Generate all combinations
	var combinations []map[string]string
	attributeNames := make([]string, 0, len(req.Attributes))
	attributeValues := make([][]string, 0, len(req.Attributes))

	for name, values := range req.Attributes {
		attributeNames = append(attributeNames, name)
		attributeValues = append(attributeValues, values)
	}

	// Generate combinations recursively
	var generate func(int, map[string]string)
	generate = func(index int, current map[string]string) {
		if index == len(attributeNames) {
			combo := make(map[string]string)
			for k, v := range current {
				combo[k] = v
			}
			combinations = append(combinations, combo)
			return
		}

		name := attributeNames[index]
		for _, value := range attributeValues[index] {
			current[name] = value
			generate(index+1, current)
		}
	}

	generate(0, make(map[string]string))

	// Create variants
	var createdVariants []models.Variant
	for i, combo := range combinations {
		// Generate SKU from pattern or use default
		sku := req.SKUPattern
		if sku == "" {
			sku = fmt.Sprintf("%s-VAR-%d", productID[:8], i+1)
		} else {
			for _, value := range combo {
				sku = fmt.Sprintf("%s-%s", sku, value)
			}
		}

		variant := &models.Variant{
			VariantID:  uuid.New().String(),
			TenantID:   tenantID,
			ProductID:  productID,
			SKU:        sku,
			Attributes: make(map[string]interface{}),
			Status:     "active",
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}

		for k, v := range combo {
			variant.Attributes[k] = v
		}

		if err := h.ProductService.CreateVariant(c.Request.Context(), variant); err != nil {
			log.Printf("Failed to create variant: %v", err)
			continue
		}

		createdVariants = append(createdVariants, *variant)
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": fmt.Sprintf("Generated %d variants", len(createdVariants)),
		"data":    createdVariants,
	})
}

// CATEGORY METHODS

// CreateCategory creates a new category
func (h *ProductHandler) CreateCategory(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req struct {
		Name        string   `json:"name" binding:"required"`
		Slug        string   `json:"slug"`
		ParentID    *string  `json:"parent_id"`
		Description *string  `json:"description"`
		Images      []models.CategoryImage `json:"images"`
		SortOrder   int      `json:"sort_order"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	category := &models.Category{
		CategoryID:  uuid.New().String(),
		TenantID:    tenantID,
		Name:        req.Name,
		Slug:        req.Slug,
		ParentID:    req.ParentID,
		Description: req.Description,
		Images:      req.Images,
		SortOrder:   req.SortOrder,
		Active:      true,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := h.ProductService.CreateCategory(c.Request.Context(), category); err != nil {
		log.Printf("❌ Failed to create category: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to create category: %v", err)})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": category})
}

// GetCategory retrieves a category by ID
func (h *ProductHandler) GetCategory(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	categoryID := c.Param("id")

	category, err := h.ProductService.GetCategory(c.Request.Context(), tenantID, categoryID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Category not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": category})
}

// UpdateCategory updates a category
func (h *ProductHandler) UpdateCategory(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	categoryID := c.Param("id")

	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.ProductService.UpdateCategory(c.Request.Context(), tenantID, categoryID, updates); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update category"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Category updated successfully"})
}

// DeleteCategory deletes a category
func (h *ProductHandler) DeleteCategory(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	categoryID := c.Param("id")

	if err := h.ProductService.DeleteCategory(c.Request.Context(), tenantID, categoryID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete category"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Category deleted successfully"})
}

// ListCategories lists all categories
func (h *ProductHandler) ListCategories(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	categories, err := h.ProductService.ListCategories(c.Request.Context(), tenantID)
	if err != nil {
		log.Printf("❌ Failed to list categories: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to list categories: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": categories})
}

// GetCategoryTree returns categories in hierarchical tree structure
func (h *ProductHandler) GetCategoryTree(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	tree, err := h.ProductService.GetCategoryTree(c.Request.Context(), tenantID)
	if err != nil {
		log.Printf("❌ Failed to get category tree: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to get category tree: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": tree})
}

// BULK OPERATIONS

// BulkCreateProducts creates multiple products in a single request.
// POST /products/bulk
// Body: {"products": [{...}, {...}]}
func (h *ProductHandler) BulkCreateProducts(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	var body struct {
		Products []models.Product `json:"products" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}
	if len(body.Products) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "products array is empty"})
		return
	}

	type Result struct {
		ProductID string `json:"product_id,omitempty"`
		Title     string `json:"title,omitempty"`
		Error     string `json:"error,omitempty"`
		Success   bool   `json:"success"`
	}
	results := make([]Result, len(body.Products))

	for i, p := range body.Products {
		if p.Title == "" || p.SKU == "" {
			results[i] = Result{Title: p.Title, Success: false, Error: "title and sku are required"}
			continue
		}
		p.TenantID = tenantID
		if p.ProductID == "" {
			p.ProductID = uuid.New().String()
		}
		if err := h.ProductService.CreateProduct(ctx, &p); err != nil {
			results[i] = Result{Title: p.Title, Success: false, Error: err.Error()}
			continue
		}
		if indexErr := h.SearchService.IndexProduct(&p); indexErr != nil {
			log.Printf("[BulkCreateProducts] indexing failed for %s: %v", p.ProductID, indexErr)
		}
		results[i] = Result{ProductID: p.ProductID, Title: p.Title, Success: true}
	}

	successCount := 0
	for _, r := range results {
		if r.Success {
			successCount++
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"results":       results,
		"total":         len(body.Products),
		"success_count": successCount,
		"error_count":   len(body.Products) - successCount,
	})
}

// BulkUpdateProducts updates multiple products in a single request.
// PATCH /products/bulk
// Body: {"updates": [{"product_id": "...", "updates": {...}}, ...]}
func (h *ProductHandler) BulkUpdateProducts(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	var body struct {
		Updates []struct {
			ProductID string                 `json:"product_id" binding:"required"`
			Updates   map[string]interface{} `json:"updates"    binding:"required"`
		} `json:"updates" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}
	if len(body.Updates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "updates array is empty"})
		return
	}

	type Result struct {
		ProductID string `json:"product_id"`
		Error     string `json:"error,omitempty"`
		Success   bool   `json:"success"`
	}
	results := make([]Result, len(body.Updates))

	for i, u := range body.Updates {
		if err := h.ProductService.UpdateProduct(ctx, tenantID, u.ProductID, u.Updates); err != nil {
			results[i] = Result{ProductID: u.ProductID, Success: false, Error: err.Error()}
			continue
		}
		// Re-fetch to index the updated document.
		if updated, fetchErr := h.ProductService.GetProduct(ctx, tenantID, u.ProductID); fetchErr == nil {
			if indexErr := h.SearchService.IndexProduct(updated); indexErr != nil {
				log.Printf("[BulkUpdateProducts] indexing failed for %s: %v", u.ProductID, indexErr)
			}
		}
		results[i] = Result{ProductID: u.ProductID, Success: true}
	}

	successCount := 0
	for _, r := range results {
		if r.Success {
			successCount++
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"results":       results,
		"total":         len(body.Updates),
		"success_count": successCount,
		"error_count":   len(body.Updates) - successCount,
	})
}

// JOB METHODS

// GetJob retrieves a job by ID
func (h *ProductHandler) GetJob(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	jobID := c.Param("id")

	job, err := h.ProductService.GetJob(c.Request.Context(), tenantID, jobID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Job not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": job})
}

// ListJobs lists all jobs
func (h *ProductHandler) ListJobs(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	offset := (page - 1) * pageSize

	jobs, total, err := h.ProductService.ListJobs(c.Request.Context(), tenantID, pageSize, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list jobs"})
		return
	}

	totalPages := int(total) / pageSize
	if int(total)%pageSize != 0 {
		totalPages++
	}

	c.JSON(http.StatusOK, gin.H{
		"data": jobs,
		"pagination": gin.H{
			"total":       total,
			"page":        page,
			"page_size":   pageSize,
			"total_pages": totalPages,
		},
	})
}
