package handlers

// ============================================================================
// P2 PRODUCT EXTENSIONS
// ============================================================================
// DuplicateProduct  POST /api/v1/products/:id/duplicate
//   Creates a full copy of a product with status=draft and SKU "-COPY" suffix
//
// ProductExtHandler  holds a Firestore client for queries not covered by
// the ProductService abstraction.
//
// GetDueStock  GET /api/v1/products/:id/due-stock
//   Returns total qty on open PO lines for this product, broken down by PO.
// ============================================================================

import (
	"fmt"
	"net/http"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"
	"module-a/models"
)

// ── DuplicateProduct (method on existing ProductHandler) ─────────────────────

// DuplicateProduct creates a full copy of an existing product.
// POST /api/v1/products/:id/duplicate
func (h *ProductHandler) DuplicateProduct(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	productID := c.Param("id")

	src, err := h.ProductService.GetProduct(c.Request.Context(), tenantID, productID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Product not found"})
		return
	}

	now := time.Now()
	newID := uuid.New().String()
	copied := &models.Product{
		ProductID:          newID,
		TenantID:           tenantID,
		Status:             "draft",
		SKU:                src.SKU + "-COPY",
		Title:              "Copy of " + src.Title,
		Subtitle:           src.Subtitle,
		Description:        src.Description,
		Brand:              src.Brand,
		ProductType:        src.ProductType,
		Identifiers:        src.Identifiers,
		CategoryIDs:        src.CategoryIDs,
		Tags:               src.Tags,
		KeyFeatures:        src.KeyFeatures,
		AttributeSetID:     src.AttributeSetID,
		Attributes:         src.Attributes,
		Assets:             src.Assets,
		Dimensions:         src.Dimensions,
		Weight:             src.Weight,
		ShippingDimensions: src.ShippingDimensions,
		ShippingWeight:     src.ShippingWeight,
		BundleComponents:   src.BundleComponents,
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	if err := h.ProductService.CreateProduct(c.Request.Context(), copied); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to duplicate product"})
		return
	}

	if h.SearchService != nil {
		_ = h.SearchService.IndexProduct(copied)
	}

	c.JSON(http.StatusCreated, gin.H{
		"data":       copied,
		"product_id": newID,
		"message":    "Product duplicated successfully",
	})
}

// ── ProductExtHandler ─────────────────────────────────────────────────────────

// ProductExtHandler provides additional product endpoints that require
// direct Firestore access beyond what ProductService exposes.
type ProductExtHandler struct {
	client *firestore.Client
}

func NewProductExtHandler(client *firestore.Client) *ProductExtHandler {
	return &ProductExtHandler{client: client}
}

// DuePOLine summarises one open PO line contributing to due stock.
type DuePOLine struct {
	POID       string     `json:"po_id"`
	PONumber   string     `json:"po_number"`
	Supplier   string     `json:"supplier_name"`
	QtyDue     int        `json:"qty_due"`
	ExpectedAt *time.Time `json:"expected_at,omitempty"`
	Status     string     `json:"status"`
}

// GetDueStock returns total qty on open PO lines for a product.
// GET /api/v1/products/:id/due-stock
func (h *ProductExtHandler) GetDueStock(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	productID := c.Param("id")
	sku := c.Query("sku")

	iter := h.client.
		Collection(fmt.Sprintf("tenants/%s/purchase_orders", tenantID)).
		Where("status", "in", []string{"draft", "sent", "partially_received"}).
		Documents(c.Request.Context())
	defer iter.Stop()

	var lines []DuePOLine
	totalDue := 0

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query purchase orders"})
			return
		}

		var po struct {
			POID         string          `firestore:"po_id"`
			PONumber     string          `firestore:"po_number"`
			SupplierName string          `firestore:"supplier_name"`
			Status       string          `firestore:"status"`
			ExpectedAt   *time.Time      `firestore:"expected_at"`
			Lines        []models.POLine `firestore:"lines"`
		}
		if err := doc.DataTo(&po); err != nil {
			continue
		}

		for _, line := range po.Lines {
			match := (line.ProductID == productID) || (sku != "" && line.SKU == sku)
			if !match {
				continue
			}
			due := line.QtyOrdered - line.QtyReceived
			if due <= 0 {
				continue
			}
			totalDue += due
			lines = append(lines, DuePOLine{
				POID:       po.POID,
				PONumber:   po.PONumber,
				Supplier:   po.SupplierName,
				QtyDue:     due,
				ExpectedAt: po.ExpectedAt,
				Status:     po.Status,
			})
		}
	}

	if lines == nil {
		lines = []DuePOLine{}
	}

	c.JSON(http.StatusOK, gin.H{
		"total_due": totalDue,
		"lines":     lines,
	})
}
