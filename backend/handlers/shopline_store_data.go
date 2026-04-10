package handlers

// ============================================================================
// SHOPLINE STORE DATA ENDPOINTS
// ============================================================================
//
// GET /shopline/locations    → inventory locations
// GET /shopline/channels     → sales channels (Online Store, etc.)
// GET /shopline/tags         → unique tags across all products
// GET /shopline/types        → unique product types across all products
// GET /shopline/collections  → product collections
// GET /shopline/categories   → Shopline standard category taxonomy
// ============================================================================

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
)

// GetLocations returns all active inventory locations.
// GET /shopline/locations?credential_id=...
func (h *ShoplineHandler) GetLocations(c *gin.Context) {
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

// GetChannels returns sales channels configured for the Shopline store.
// GET /shopline/channels?credential_id=...
func (h *ShoplineHandler) GetChannels(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	credentialID := c.Query("credential_id")
	client, _, err := h.getClient(c.Request.Context(), tenantID, credentialID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	result, _, err := client.do(c.Request.Context(), "GET", "/channels.json?limit=250", nil)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": fmt.Sprintf("fetch channels: %v", err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "channels": result["channels"]})
}

// GetTags returns all unique product tags used in the store.
// GET /shopline/tags?credential_id=...
func (h *ShoplineHandler) GetTags(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	credentialID := c.Query("credential_id")
	client, _, err := h.getClient(c.Request.Context(), tenantID, credentialID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	tagSet := map[string]bool{}

	result, _, err := client.do(c.Request.Context(), "GET", "/products.json?fields=tags&limit=250", nil)
	if err == nil {
		if productsRaw, ok := result["products"].([]interface{}); ok {
			for _, p := range productsRaw {
				if pm, ok := p.(map[string]interface{}); ok {
					// Shopline returns tags as an array OR comma-separated string
					switch tv := pm["tags"].(type) {
					case string:
						for _, tag := range strings.Split(tv, ",") {
							tag = strings.TrimSpace(tag)
							if tag != "" {
								tagSet[tag] = true
							}
						}
					case []interface{}:
						for _, t := range tv {
							if ts, ok := t.(string); ok && ts != "" {
								tagSet[ts] = true
							}
						}
					}
				}
			}
		}
	}

	tags := make([]string, 0, len(tagSet))
	for tag := range tagSet {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	c.JSON(http.StatusOK, gin.H{"ok": true, "tags": tags})
}

// GetTypes returns all unique product types used in the store.
// GET /shopline/types?credential_id=...
func (h *ShoplineHandler) GetTypes(c *gin.Context) {
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

// GetCollections returns all product collections from the Shopline store.
// GET /shopline/collections?credential_id=...
func (h *ShoplineHandler) GetCollections(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	credentialID := c.Query("credential_id")
	client, _, err := h.getClient(c.Request.Context(), tenantID, credentialID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	result, _, err := client.do(c.Request.Context(), "GET", "/collections.json?limit=250&fields=id,title,handle", nil)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": fmt.Sprintf("fetch collections: %v", err)})
		return
	}

	collections := result["collections"]
	if collections == nil {
		collections = []interface{}{}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "collections": collections})
}

// GetCategories returns Shopline's standard product category taxonomy.
// Shopline uses a category tree aligned with common e-commerce verticals.
// GET /shopline/categories?credential_id=...&search=...
func (h *ShoplineHandler) GetCategories(c *gin.Context) {
	categories := []map[string]string{
		{"id": "sl-cl-1", "full_name": "Clothing & Apparel"},
		{"id": "sl-cl-2", "full_name": "Clothing & Apparel > Women's Clothing"},
		{"id": "sl-cl-3", "full_name": "Clothing & Apparel > Men's Clothing"},
		{"id": "sl-cl-4", "full_name": "Clothing & Apparel > Children's Clothing"},
		{"id": "sl-cl-5", "full_name": "Clothing & Apparel > Underwear & Sleepwear"},
		{"id": "sl-cl-6", "full_name": "Clothing & Apparel > Sportswear"},
		{"id": "sl-sh-1", "full_name": "Shoes & Footwear"},
		{"id": "sl-sh-2", "full_name": "Shoes & Footwear > Women's Shoes"},
		{"id": "sl-sh-3", "full_name": "Shoes & Footwear > Men's Shoes"},
		{"id": "sl-sh-4", "full_name": "Shoes & Footwear > Children's Shoes"},
		{"id": "sl-sh-5", "full_name": "Shoes & Footwear > Sports Shoes"},
		{"id": "sl-ba-1", "full_name": "Bags & Accessories"},
		{"id": "sl-ba-2", "full_name": "Bags & Accessories > Handbags"},
		{"id": "sl-ba-3", "full_name": "Bags & Accessories > Backpacks"},
		{"id": "sl-ba-4", "full_name": "Bags & Accessories > Wallets"},
		{"id": "sl-ba-5", "full_name": "Bags & Accessories > Luggage"},
		{"id": "sl-jw-1", "full_name": "Jewellery & Watches"},
		{"id": "sl-jw-2", "full_name": "Jewellery & Watches > Necklaces"},
		{"id": "sl-jw-3", "full_name": "Jewellery & Watches > Earrings"},
		{"id": "sl-jw-4", "full_name": "Jewellery & Watches > Rings"},
		{"id": "sl-jw-5", "full_name": "Jewellery & Watches > Bracelets"},
		{"id": "sl-jw-6", "full_name": "Jewellery & Watches > Watches"},
		{"id": "sl-be-1", "full_name": "Beauty & Personal Care"},
		{"id": "sl-be-2", "full_name": "Beauty & Personal Care > Skincare"},
		{"id": "sl-be-3", "full_name": "Beauty & Personal Care > Makeup"},
		{"id": "sl-be-4", "full_name": "Beauty & Personal Care > Hair Care"},
		{"id": "sl-be-5", "full_name": "Beauty & Personal Care > Fragrances"},
		{"id": "sl-be-6", "full_name": "Beauty & Personal Care > Nail Care"},
		{"id": "sl-el-1", "full_name": "Electronics & Tech"},
		{"id": "sl-el-2", "full_name": "Electronics & Tech > Smartphones & Accessories"},
		{"id": "sl-el-3", "full_name": "Electronics & Tech > Computers & Tablets"},
		{"id": "sl-el-4", "full_name": "Electronics & Tech > Audio & Headphones"},
		{"id": "sl-el-5", "full_name": "Electronics & Tech > Cameras"},
		{"id": "sl-el-6", "full_name": "Electronics & Tech > Smart Home"},
		{"id": "sl-el-7", "full_name": "Electronics & Tech > Wearables"},
		{"id": "sl-ho-1", "full_name": "Home & Living"},
		{"id": "sl-ho-2", "full_name": "Home & Living > Furniture"},
		{"id": "sl-ho-3", "full_name": "Home & Living > Kitchen & Dining"},
		{"id": "sl-ho-4", "full_name": "Home & Living > Bedding & Bath"},
		{"id": "sl-ho-5", "full_name": "Home & Living > Home Decor"},
		{"id": "sl-ho-6", "full_name": "Home & Living > Lighting"},
		{"id": "sl-ho-7", "full_name": "Home & Living > Storage & Organisation"},
		{"id": "sl-sp-1", "full_name": "Sports & Outdoors"},
		{"id": "sl-sp-2", "full_name": "Sports & Outdoors > Exercise Equipment"},
		{"id": "sl-sp-3", "full_name": "Sports & Outdoors > Outdoor Recreation"},
		{"id": "sl-sp-4", "full_name": "Sports & Outdoors > Team Sports"},
		{"id": "sl-sp-5", "full_name": "Sports & Outdoors > Cycling"},
		{"id": "sl-to-1", "full_name": "Toys, Kids & Baby"},
		{"id": "sl-to-2", "full_name": "Toys, Kids & Baby > Toys"},
		{"id": "sl-to-3", "full_name": "Toys, Kids & Baby > Baby Care"},
		{"id": "sl-to-4", "full_name": "Toys, Kids & Baby > Baby Feeding"},
		{"id": "sl-to-5", "full_name": "Toys, Kids & Baby > Strollers & Car Seats"},
		{"id": "sl-pe-1", "full_name": "Pets & Animals"},
		{"id": "sl-pe-2", "full_name": "Pets & Animals > Dog Supplies"},
		{"id": "sl-pe-3", "full_name": "Pets & Animals > Cat Supplies"},
		{"id": "sl-pe-4", "full_name": "Pets & Animals > Bird Supplies"},
		{"id": "sl-fo-1", "full_name": "Food & Beverages"},
		{"id": "sl-fo-2", "full_name": "Food & Beverages > Snacks"},
		{"id": "sl-fo-3", "full_name": "Food & Beverages > Beverages"},
		{"id": "sl-fo-4", "full_name": "Food & Beverages > Health Foods"},
		{"id": "sl-au-1", "full_name": "Automotive"},
		{"id": "sl-au-2", "full_name": "Automotive > Car Accessories"},
		{"id": "sl-au-3", "full_name": "Automotive > Car Care"},
		{"id": "sl-of-1", "full_name": "Office & Stationery"},
		{"id": "sl-of-2", "full_name": "Office & Stationery > Office Supplies"},
		{"id": "sl-of-3", "full_name": "Office & Stationery > Stationery"},
		{"id": "sl-ar-1", "full_name": "Arts, Crafts & Hobbies"},
		{"id": "sl-ar-2", "full_name": "Arts, Crafts & Hobbies > Art Supplies"},
		{"id": "sl-ar-3", "full_name": "Arts, Crafts & Hobbies > Craft Supplies"},
		{"id": "sl-he-1", "full_name": "Health & Wellness"},
		{"id": "sl-he-2", "full_name": "Health & Wellness > Vitamins & Supplements"},
		{"id": "sl-he-3", "full_name": "Health & Wellness > Medical Devices"},
		{"id": "sl-he-4", "full_name": "Health & Wellness > Fitness Trackers"},
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
