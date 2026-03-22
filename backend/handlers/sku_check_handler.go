package handlers

import (
	"net/http"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"google.golang.org/api/iterator"
)

// ============================================================================
// SKU CHECK HANDLER — FLD-15
// ============================================================================
// GET /api/v1/listings/check-sku?sku=X
// Non-blocking cross-marketplace duplicate SKU detection.
// Queries the listings Firestore collection for any existing listing with
// that SKU across all channels (by finding products with that SKU first).
// Returns { isDuplicate: bool, existingChannels: string[] }
// ============================================================================

type SKUCheckHandler struct {
	client *firestore.Client
}

func NewSKUCheckHandler(client *firestore.Client) *SKUCheckHandler {
	return &SKUCheckHandler{client: client}
}

// GET /api/v1/listings/check-sku?sku=X
func (h *SKUCheckHandler) CheckSKU(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	sku := c.Query("sku")
	if sku == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "sku query parameter is required"})
		return
	}

	ctx := c.Request.Context()

	// Step 1: Find all product IDs that have this SKU
	productIDs := []string{}

	// Check main products collection
	productIter := h.client.Collection("tenants").Doc(tenantID).Collection("products").
		Where("sku", "==", sku).
		Documents(ctx)
	for {
		doc, err := productIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		productIDs = append(productIDs, doc.Ref.ID)
	}

	// Also check variant SKUs — query products where a variant matches
	// (variants are stored as a subcollection; checking direct sku field on product covers the main case)

	if len(productIDs) == 0 {
		// No product found with this SKU — no duplicate
		c.JSON(http.StatusOK, gin.H{
			"isDuplicate":      false,
			"existingChannels": []string{},
		})
		return
	}

	// Step 2: For each product, find listings and collect channels
	channelSet := map[string]bool{}

	for _, productID := range productIDs {
		listingIter := h.client.Collection("tenants").Doc(tenantID).Collection("listings").
			Where("product_id", "==", productID).
			Documents(ctx)
		for {
			doc, listErr := listingIter.Next()
			if listErr == iterator.Done {
				break
			}
			if listErr != nil {
				break
			}
			data := doc.Data()
			if ch, ok := data["channel"].(string); ok && ch != "" {
				channelSet[ch] = true
			}
		}
	}

	channels := make([]string, 0, len(channelSet))
	for ch := range channelSet {
		channels = append(channels, ch)
	}

	c.JSON(http.StatusOK, gin.H{
		"isDuplicate":      len(channels) > 0,
		"existingChannels": channels,
	})
}
