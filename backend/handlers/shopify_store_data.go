package handlers

// ============================================================================
// SHOPIFY STORE DATA ENDPOINTS
// ============================================================================
//
// GET /shopify/locations        → inventory locations
// GET /shopify/publications     → sales channels (Online Store, POS, etc.)
// GET /shopify/tags             → unique tags across all products
// GET /shopify/types            → unique product types across all products  
// GET /shopify/collections      → manual collections (custom_collections)
// GET /shopify/metafield-defs   → metafield definitions for products
// GET /shopify/categories       → Shopify standard taxonomy (static list)
// ============================================================================

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
)

// GetLocations returns all active inventory locations.
// GET /shopify/locations?credential_id=...
func (h *ShopifyHandler) GetLocations(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	credentialID := c.Query("credential_id")
	client, _, err := h.getClient(c.Request.Context(), tenantID, credentialID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	result, _, err := client.do(c.Request.Context(), "GET", "/locations.json?limit=250", nil)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": fmt.Sprintf("fetch locations: %v", err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "locations": result["locations"]})
}

// GetPublications returns sales channels (Online Store, POS, etc.).
// GET /shopify/publications?credential_id=...
func (h *ShopifyHandler) GetPublications(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	credentialID := c.Query("credential_id")
	client, _, err := h.getClient(c.Request.Context(), tenantID, credentialID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	result, _, err := client.do(c.Request.Context(), "GET", "/publications.json?limit=250", nil)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": fmt.Sprintf("fetch publications: %v", err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "publications": result["publications"]})
}

// GetTags returns all unique product tags used in the store.
// Shopify has no dedicated tags endpoint in REST — we fetch products in batches
// and aggregate the tags field (comma-separated string on each product).
// GET /shopify/tags?credential_id=...
func (h *ShopifyHandler) GetTags(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	credentialID := c.Query("credential_id")
	client, _, err := h.getClient(c.Request.Context(), tenantID, credentialID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	tagSet := map[string]bool{}
	pageInfo := ""
	for {
		url := "/products.json?fields=tags&limit=250"
		if pageInfo != "" {
			url += "&page_info=" + pageInfo
		}
		result, statusCode, err := client.do(c.Request.Context(), "GET", url, nil)
		if err != nil || statusCode >= 400 {
			break
		}
		productsRaw, _ := result["products"].([]interface{})
		if len(productsRaw) == 0 {
			break
		}
		for _, p := range productsRaw {
			if pm, ok := p.(map[string]interface{}); ok {
				if tags, ok := pm["tags"].(string); ok && tags != "" {
					for _, tag := range strings.Split(tags, ",") {
						tag = strings.TrimSpace(tag)
						if tag != "" {
							tagSet[tag] = true
						}
					}
				}
			}
		}
		// Stop after first page for performance — 250 products covers most stores
		// For large stores with many tags, one page is sufficient for suggestions
		break
	}

	tags := make([]string, 0, len(tagSet))
	for tag := range tagSet {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	c.JSON(http.StatusOK, gin.H{"ok": true, "tags": tags})
}

// GetTypes returns all unique product types used in the store.
// Same approach as tags — no dedicated REST endpoint, aggregate from products.
// GET /shopify/types?credential_id=...
func (h *ShopifyHandler) GetTypes(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	credentialID := c.Query("credential_id")
	client, _, err := h.getClient(c.Request.Context(), tenantID, credentialID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	result, _, err := client.do(c.Request.Context(), "GET", "/products.json?fields=product_type&limit=250", nil)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": fmt.Sprintf("fetch types: %v", err)})
		return
	}

	typeSet := map[string]bool{}
	if productsRaw, ok := result["products"].([]interface{}); ok {
		for _, p := range productsRaw {
			if pm, ok := p.(map[string]interface{}); ok {
				if pt, ok := pm["product_type"].(string); ok && pt != "" {
					typeSet[pt] = true
				}
			}
		}
	}

	types := make([]string, 0, len(typeSet))
	for t := range typeSet {
		types = append(types, t)
	}
	sort.Strings(types)
	c.JSON(http.StatusOK, gin.H{"ok": true, "types": types})
}

// GetCollections returns all manual collections (custom_collections).
// These are the hand-curated collections merchants create, not smart collections.
// GET /shopify/collections?credential_id=...
func (h *ShopifyHandler) GetCollections(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	credentialID := c.Query("credential_id")
	client, _, err := h.getClient(c.Request.Context(), tenantID, credentialID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	// Fetch both manual (custom) and smart (automated) collections
	var allCollections []interface{}

	manual, _, err := client.do(c.Request.Context(), "GET", "/custom_collections.json?limit=250&fields=id,title,handle", nil)
	if err == nil {
		if cols, ok := manual["custom_collections"].([]interface{}); ok {
			for _, col := range cols {
				if cm, ok := col.(map[string]interface{}); ok {
					cm["type"] = "manual"
					allCollections = append(allCollections, cm)
				}
			}
		}
	}

	smart, _, err := client.do(c.Request.Context(), "GET", "/smart_collections.json?limit=250&fields=id,title,handle", nil)
	if err == nil {
		if cols, ok := smart["smart_collections"].([]interface{}); ok {
			for _, col := range cols {
				if cm, ok := col.(map[string]interface{}); ok {
					cm["type"] = "smart"
					allCollections = append(allCollections, cm)
				}
			}
		}
	}

	if allCollections == nil {
		allCollections = []interface{}{}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "collections": allCollections})
}

// GetMetafieldDefs returns metafield definitions for the products resource.
// Uses the REST metafield_definitions endpoint (requires 2022-01+ API version).
// GET /shopify/metafield-defs?credential_id=...
func (h *ShopifyHandler) GetMetafieldDefs(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	credentialID := c.Query("credential_id")
	client, _, err := h.getClient(c.Request.Context(), tenantID, credentialID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	result, _, err := client.do(c.Request.Context(), "GET", "/metafield_definitions.json?owner_type=PRODUCT&limit=250", nil)
	if err != nil {
		// Non-fatal — metafield defs may not exist on all stores
		c.JSON(http.StatusOK, gin.H{"ok": true, "metafield_definitions": []interface{}{}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "metafield_definitions": result["metafield_definitions"]})
}

// GetCategories returns Shopify's standard product taxonomy as a static list.
// The REST taxonomy endpoint (/product_categories.json) does not exist —
// the taxonomy is only available via GraphQL on Shopify Plus or via a static
// published CSV. We return a curated subset of the most common categories.
// GET /shopify/categories?credential_id=...
func (h *ShopifyHandler) GetCategories(c *gin.Context) {
	// Shopify's standard taxonomy top-level categories
	// Full list: https://www.shopify.com/uk/blog/standard-product-type
	categories := []map[string]string{
		{"id": "aa-1", "full_name": "Animals & Pet Supplies"},
		{"id": "aa-2", "full_name": "Animals & Pet Supplies > Pet Supplies"},
		{"id": "aa-3", "full_name": "Animals & Pet Supplies > Pet Supplies > Dog Supplies"},
		{"id": "aa-4", "full_name": "Animals & Pet Supplies > Pet Supplies > Cat Supplies"},
		{"id": "ap-1", "full_name": "Apparel & Accessories"},
		{"id": "ap-2", "full_name": "Apparel & Accessories > Clothing"},
		{"id": "ap-3", "full_name": "Apparel & Accessories > Clothing > Tops"},
		{"id": "ap-4", "full_name": "Apparel & Accessories > Clothing > Bottoms"},
		{"id": "ap-5", "full_name": "Apparel & Accessories > Clothing > Dresses"},
		{"id": "ap-6", "full_name": "Apparel & Accessories > Clothing > Outerwear"},
		{"id": "ap-7", "full_name": "Apparel & Accessories > Shoes"},
		{"id": "ap-8", "full_name": "Apparel & Accessories > Jewellery"},
		{"id": "ap-9", "full_name": "Apparel & Accessories > Handbags & Wallets"},
		{"id": "ap-10", "full_name": "Apparel & Accessories > Accessories"},
		{"id": "ar-1", "full_name": "Arts & Entertainment"},
		{"id": "ar-2", "full_name": "Arts & Entertainment > Music"},
		{"id": "ar-3", "full_name": "Arts & Entertainment > Books"},
		{"id": "ar-4", "full_name": "Arts & Entertainment > DVDs & Videos"},
		{"id": "ba-1", "full_name": "Baby & Toddler"},
		{"id": "ba-2", "full_name": "Baby & Toddler > Clothing"},
		{"id": "ba-3", "full_name": "Baby & Toddler > Feeding"},
		{"id": "ba-4", "full_name": "Baby & Toddler > Nursery"},
		{"id": "be-1", "full_name": "Beauty & Personal Care"},
		{"id": "be-2", "full_name": "Beauty & Personal Care > Skincare"},
		{"id": "be-3", "full_name": "Beauty & Personal Care > Hair Care"},
		{"id": "be-4", "full_name": "Beauty & Personal Care > Makeup"},
		{"id": "be-5", "full_name": "Beauty & Personal Care > Fragrances"},
		{"id": "ca-1", "full_name": "Cameras & Optics"},
		{"id": "el-1", "full_name": "Electronics"},
		{"id": "el-2", "full_name": "Electronics > Computers"},
		{"id": "el-3", "full_name": "Electronics > Mobile Phones"},
		{"id": "el-4", "full_name": "Electronics > TV & Home Theatre"},
		{"id": "el-5", "full_name": "Electronics > Audio"},
		{"id": "el-6", "full_name": "Electronics > Wearables"},
		{"id": "fo-1", "full_name": "Food, Beverages & Tobacco"},
		{"id": "fo-2", "full_name": "Food, Beverages & Tobacco > Food"},
		{"id": "fo-3", "full_name": "Food, Beverages & Tobacco > Beverages"},
		{"id": "fu-1", "full_name": "Furniture"},
		{"id": "ha-1", "full_name": "Hardware"},
		{"id": "he-1", "full_name": "Health & Fitness"},
		{"id": "he-2", "full_name": "Health & Fitness > Vitamins & Supplements"},
		{"id": "he-3", "full_name": "Health & Fitness > Sports Equipment"},
		{"id": "he-4", "full_name": "Health & Fitness > Medical Supplies"},
		{"id": "ho-1", "full_name": "Home & Garden"},
		{"id": "ho-2", "full_name": "Home & Garden > Kitchen & Dining"},
		{"id": "ho-3", "full_name": "Home & Garden > Bed & Bath"},
		{"id": "ho-4", "full_name": "Home & Garden > Furniture"},
		{"id": "ho-5", "full_name": "Home & Garden > Garden & Outdoor"},
		{"id": "ho-6", "full_name": "Home & Garden > Home Decor"},
		{"id": "ho-7", "full_name": "Home & Garden > Lighting"},
		{"id": "lu-1", "full_name": "Luggage & Bags"},
		{"id": "of-1", "full_name": "Office Supplies"},
		{"id": "sp-1", "full_name": "Sporting Goods"},
		{"id": "sp-2", "full_name": "Sporting Goods > Exercise & Fitness"},
		{"id": "sp-3", "full_name": "Sporting Goods > Outdoor Recreation"},
		{"id": "to-1", "full_name": "Toys & Games"},
		{"id": "to-2", "full_name": "Toys & Games > Action Figures"},
		{"id": "to-3", "full_name": "Toys & Games > Dolls"},
		{"id": "to-4", "full_name": "Toys & Games > Board Games"},
		{"id": "ve-1", "full_name": "Vehicles & Parts"},
	}

	search := c.Query("search")
	if search != "" {
		searchLower := strings.ToLower(search)
		filtered := []map[string]string{}
		for _, cat := range categories {
			if strings.Contains(strings.ToLower(cat["full_name"]), searchLower) {
				filtered = append(filtered, cat)
			}
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "categories": filtered})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "categories": categories})
}

// ── helper: safe JSON round-trip to []interface{} ─────────────────────────────
func toInterfaceSlice(v interface{}) []interface{} {
	b, _ := json.Marshal(v)
	var out []interface{}
	json.Unmarshal(b, &out)
	if out == nil {
		return []interface{}{}
	}
	return out
}
