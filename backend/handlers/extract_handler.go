package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	amazon "module-a/marketplace/clients/amazon"
	ebay "module-a/marketplace/clients/ebay"
	walmart "module-a/marketplace/clients/walmart"
	"module-a/models"
	"module-a/repository"
	"module-a/services"
)

// ============================================================================
// EXTRACT HANDLER — IMP-01 / CLM-01: Extract Inventory workflow
//                   CLM-02: Link existing listing to internal product
// ============================================================================
// Routes (registered in main.go under extractGroup):
//   GET  /extract/channels                          → list channels with creds that support extraction
//   GET  /extract/:channel/listings                 → browse live listings from channel (paginated)
//   POST /extract/:channel/listings/extract         → pull selected listing IDs into MarketMate drafts
//   POST /extract/listings/:listing_id/link         → CLM-02: link existing listing to internal product
// ============================================================================

type ExtractHandler struct {
	marketplaceService *services.MarketplaceService
	repo               *repository.MarketplaceRepository
	firestoreRepo      *repository.FirestoreRepository
	productService     *services.ProductService
}

func NewExtractHandler(
	marketplaceService *services.MarketplaceService,
	repo *repository.MarketplaceRepository,
	firestoreRepo *repository.FirestoreRepository,
	productService *services.ProductService,
) *ExtractHandler {
	return &ExtractHandler{
		marketplaceService: marketplaceService,
		repo:               repo,
		firestoreRepo:      firestoreRepo,
		productService:     productService,
	}
}

// ─── Response types ───────────────────────────────────────────────────────────

// ExtractChannel is an entry in the "supported channels" list.
type ExtractChannel struct {
	CredentialID string `json:"credential_id"`
	Channel      string `json:"channel"`
	AccountName  string `json:"account_name"`
	Active       bool   `json:"active"`
}

// ExtractListing represents a live listing retrieved from a channel API.
type ExtractListing struct {
	ExternalID  string            `json:"external_id"`  // SKU or listing ID on channel
	Title       string            `json:"title"`
	SKU         string            `json:"sku"`
	Price       float64           `json:"price,omitempty"`
	Quantity    int               `json:"quantity,omitempty"`
	Status      string            `json:"status"`
	ImageURL    string            `json:"image_url,omitempty"`
	ASIN        string            `json:"asin,omitempty"`
	Raw         map[string]interface{} `json:"raw,omitempty"`
}

// ExtractResult is returned after extracting listings into MarketMate.
type ExtractResult struct {
	Extracted int      `json:"extracted"`
	Skipped   int      `json:"skipped"`
	ListingIDs []string `json:"listing_ids"`
	Errors     []string `json:"errors,omitempty"`
}

// ─── GET /extract/channels ─────────────────────────────────────────────────
// Returns the list of channels that have credentials and support extraction.
// Supported channels: amazon, ebay, shopify, walmart.

func (h *ExtractHandler) ListExtractableChannels(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	creds, err := h.repo.ListCredentials(ctx, tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": err.Error()})
		return
	}

	supported := map[string]bool{
		"amazon":  true,
		"ebay":    true,
		"shopify": true,
		"walmart": true,
	}

	var channels []ExtractChannel
	for _, cred := range creds {
		if !supported[cred.Channel] || !cred.Active {
			continue
		}
		accountName := cred.AccountName
		if accountName == "" {
			accountName = cred.Channel + " account"
		}
		channels = append(channels, ExtractChannel{
			CredentialID: cred.CredentialID,
			Channel:      cred.Channel,
			AccountName:  accountName,
			Active:       cred.Active,
		})
	}

	if channels == nil {
		channels = []ExtractChannel{}
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "channels": channels})
}

// ─── GET /extract/:channel/listings ───────────────────────────────────────────
// Fetches live listings from the channel API.
// Query params: credential_id (required), limit (default 50), offset/cursor, search

func (h *ExtractHandler) BrowseChannelListings(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	channel := c.Param("channel")
	credentialID := c.Query("credential_id")
	limitStr := c.DefaultQuery("limit", "50")
	offsetStr := c.DefaultQuery("offset", "0")
	cursor := c.Query("cursor")
	search := c.Query("search")

	limit, _ := strconv.Atoi(limitStr)
	if limit < 1 || limit > 100 {
		limit = 50
	}
	offset, _ := strconv.Atoi(offsetStr)
	if offset < 0 {
		offset = 0
	}

	ctx := c.Request.Context()

	switch channel {
	case "amazon":
		h.browseAmazon(c, ctx, tenantID, credentialID, limit, cursor, search)
	case "ebay":
		h.browseEbay(c, ctx, tenantID, credentialID, limit, offset)
	case "shopify":
		h.browseShopify(c, ctx, tenantID, credentialID, limit, cursor, search)
	case "walmart":
		h.browseWalmart(c, ctx, tenantID, credentialID, limit, cursor)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": fmt.Sprintf("channel '%s' does not support extraction", channel)})
	}
}

func (h *ExtractHandler) browseAmazon(c *gin.Context, ctx context.Context, tenantID, credentialID string, limit int, cursor, search string) {
	client, credID, marketplaceID, err := h.getAmazonClientForExtract(ctx, tenantID, credentialID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}
	_ = credID

	// Use SearchCatalogItems if we have a search term, otherwise use report rows from SP-API
	// For browsing without search, we use GetInventorySummaries (fast, no report generation)
	var listings []ExtractListing
	var nextCursor string

	if search != "" {
		pageSize := limit
		if pageSize > 20 {
			pageSize = 20 // Amazon catalog search max
		}
		resp, err := client.SearchCatalogItems(ctx, search, pageSize, cursor)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "Amazon search failed: " + err.Error()})
			return
		}
		for _, item := range resp.Items {
			el := ExtractListing{
				ExternalID: item.ASIN,
				ASIN:       item.ASIN,
				Status:     "active",
			}
			// Extract title and brand from summaries
			for _, s := range item.Summaries {
				if s.MarketplaceID == marketplaceID || el.Title == "" {
					if s.ItemName != "" {
						el.Title = s.ItemName
					}
				}
			}
			// Extract image
			for _, imgGroup := range item.Images {
				if imgGroup.Variant == "MAIN" && len(imgGroup.Images) > 0 {
					el.ImageURL = imgGroup.Images[0].Link
					break
				}
			}
			if el.Title == "" {
				el.Title = item.ASIN
			}
			listings = append(listings, el)
		}
		if resp.Pagination != nil {
			nextCursor = resp.Pagination.NextToken
		}
	} else {
		// Browse seller's own inventory using FBA inventory summaries
		summaries, err := client.GetInventorySummaries(ctx)
		if err != nil {
			// Fallback: return empty with a message
			log.Printf("[Extract] Amazon GetInventorySummaries error for tenant %s: %v", tenantID, err)
			c.JSON(http.StatusOK, gin.H{
				"ok":       true,
				"listings": []ExtractListing{},
				"total":    0,
				"note":     "Use the search box to find listings by keyword, ASIN, or title.",
			})
			return
		}
		// Apply cursor-based pagination on summaries slice
		start := 0
		if cursor != "" {
			start, _ = strconv.Atoi(cursor)
		}
		end := start + limit
		if end > len(summaries) {
			end = len(summaries)
		}
		page := summaries[start:end]
		for _, s := range page {
			el := ExtractListing{
				ExternalID: s.ASIN,
				ASIN:       s.ASIN,
				SKU:        s.SellerSKU,
				Title:      s.FnSKU,
				Status:     "active",
				Quantity:   s.TotalQuantity,
				Raw:        map[string]interface{}{"fnsku": s.FnSKU, "condition": s.Condition},
			}
			if el.Title == "" {
				el.Title = s.ASIN
			}
			listings = append(listings, el)
		}
		if end < len(summaries) {
			nextCursor = strconv.Itoa(end)
		}
	}

	if listings == nil {
		listings = []ExtractListing{}
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":          true,
		"listings":    listings,
		"total":       len(listings),
		"next_cursor": nextCursor,
	})
}

func (h *ExtractHandler) browseEbay(c *gin.Context, ctx context.Context, tenantID, credentialID string, limit, offset int) {
	client, err := h.getEbayClientForExtract(ctx, tenantID, credentialID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	page, err := client.GetInventoryItems(limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "eBay API error: " + err.Error()})
		return
	}

	var listings []ExtractListing
	for _, item := range page.InventoryItems {
		el := ExtractListing{
			ExternalID: item.SKU,
			SKU:        item.SKU,
			Status:     "active",
		}
		if item.Product != nil {
			el.Title = item.Product.Title
			if len(item.Product.ImageURLs) > 0 {
				el.ImageURL = item.Product.ImageURLs[0]
			}
		}
		if item.Availability != nil && item.Availability.ShipToLocationAvailability != nil {
			el.Quantity = item.Availability.ShipToLocationAvailability.Quantity
		}
		if el.Title == "" {
			el.Title = item.SKU
		}
		listings = append(listings, el)
	}

	if listings == nil {
		listings = []ExtractListing{}
	}

	nextOffset := offset + limit
	var nextCursor string
	if nextOffset < page.Total {
		nextCursor = strconv.Itoa(nextOffset)
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":          true,
		"listings":    listings,
		"total":       page.Total,
		"next_cursor": nextCursor,
	})
}

func (h *ExtractHandler) browseShopify(c *gin.Context, ctx context.Context, tenantID, credentialID string, limit int, cursor, search string) {
	client, _, err := h.getShopifyClientForExtract(ctx, tenantID, credentialID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	// Build query params
	params := map[string]string{
		"limit":  strconv.Itoa(limit),
		"status": "active",
		"fields": "id,title,status,variants,image",
	}
	if search != "" {
		params["title"] = search
	}
	if cursor != "" {
		params["page_info"] = cursor
	}

	result, err := client.doWithHeaders(ctx, "GET", "/products.json", params, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "Shopify API error: " + err.Error()})
		return
	}
	headers := result.headers

	var listings []ExtractListing
	products, _ := result.body["products"].([]interface{})
	for _, p := range products {
		pm, ok := p.(map[string]interface{})
		if !ok {
			continue
		}
		el := ExtractListing{
			Status: "active",
		}
		if id, ok := pm["id"].(float64); ok {
			el.ExternalID = strconv.FormatInt(int64(id), 10)
		}
		el.Title, _ = pm["title"].(string)

		// Get price and SKU from first variant
		if variants, ok := pm["variants"].([]interface{}); ok && len(variants) > 0 {
			if v, ok := variants[0].(map[string]interface{}); ok {
				el.SKU, _ = v["sku"].(string)
				if priceStr, ok := v["price"].(string); ok {
					el.Price, _ = strconv.ParseFloat(priceStr, 64)
				}
				if qty, ok := v["inventory_quantity"].(float64); ok {
					el.Quantity = int(qty)
				}
			}
		}
		// Get image
		if img, ok := pm["image"].(map[string]interface{}); ok {
			el.ImageURL, _ = img["src"].(string)
		}
		if el.Title == "" {
			el.Title = el.ExternalID
		}
		listings = append(listings, el)
	}

	if listings == nil {
		listings = []ExtractListing{}
	}

	// Extract Shopify cursor pagination from Link header
	var nextCursor string
	linkHeader := headers.Get("Link")
	if strings.Contains(linkHeader, `rel="next"`) {
		// Parse page_info from Link header
		parts := strings.Split(linkHeader, ",")
		for _, part := range parts {
			if strings.Contains(part, `rel="next"`) {
				// Extract URL
				urlPart := strings.TrimSpace(strings.Split(part, ";")[0])
				urlPart = strings.Trim(urlPart, "<>")
				// Extract page_info param
				if idx := strings.Index(urlPart, "page_info="); idx >= 0 {
					rest := urlPart[idx+10:]
					if amp := strings.Index(rest, "&"); amp >= 0 {
						rest = rest[:amp]
					}
					nextCursor = rest
				}
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":          true,
		"listings":    listings,
		"total":       len(listings),
		"next_cursor": nextCursor,
	})
}

func (h *ExtractHandler) browseWalmart(c *gin.Context, ctx context.Context, tenantID, credentialID string, limit int, cursor string) {
	client, _, err := h.getWalmartClientForExtract(ctx, tenantID, credentialID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	resp, err := client.GetItems(cursor, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "Walmart API error: " + err.Error()})
		return
	}

	var listings []ExtractListing
	for _, item := range resp.ItemDetails {
		el := ExtractListing{
			ExternalID: item.SKU,
			SKU:        item.SKU,
			Title:      item.ProductName,
			Status:     strings.ToLower(item.PublishStatus),
		}
		if item.Price != nil {
			el.Price = item.Price.Amount
		}
		if el.Title == "" {
			el.Title = item.SKU
		}
		listings = append(listings, el)
	}

	if listings == nil {
		listings = []ExtractListing{}
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":          true,
		"listings":    listings,
		"total":       resp.TotalItems,
		"next_cursor": resp.NextCursor,
	})
}

// ─── POST /extract/:channel/listings/extract ──────────────────────────────────
// Takes a list of external_ids and creates MarketMate listing records for them.
// Body: { "credential_id": "...", "external_ids": ["...", "..."], "product_id": "..." (optional) }

func (h *ExtractHandler) ExtractListings(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	channel := c.Param("channel")
	ctx := c.Request.Context()

	var req struct {
		CredentialID string   `json:"credential_id" binding:"required"`
		ExternalIDs  []string `json:"external_ids" binding:"required"`
		ProductID    string   `json:"product_id"` // optional: link to existing product
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}
	if len(req.ExternalIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "external_ids must not be empty"})
		return
	}
	if len(req.ExternalIDs) > 50 {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "maximum 50 listings can be extracted at once"})
		return
	}

	result := &ExtractResult{}

	switch channel {
	case "amazon":
		h.extractAmazon(ctx, tenantID, req.CredentialID, req.ExternalIDs, req.ProductID, result)
	case "ebay":
		h.extractEbay(ctx, tenantID, req.CredentialID, req.ExternalIDs, req.ProductID, result)
	case "shopify":
		h.extractShopify(ctx, tenantID, req.CredentialID, req.ExternalIDs, req.ProductID, result)
	case "walmart":
		h.extractWalmart(ctx, tenantID, req.CredentialID, req.ExternalIDs, req.ProductID, result)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": fmt.Sprintf("channel '%s' does not support extraction", channel)})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":     true,
		"result": result,
	})
}

func (h *ExtractHandler) extractAmazon(ctx context.Context, tenantID, credentialID string, externalIDs []string, productID string, result *ExtractResult) {
	client, credID, marketplaceID, err := h.getAmazonClientForExtract(ctx, tenantID, credentialID)
	if err != nil {
		result.Errors = append(result.Errors, "auth: "+err.Error())
		return
	}

	for _, asin := range externalIDs {
		// Fetch full catalog item for enriched data
		item, err := client.GetCatalogItem(ctx, asin)
		if err != nil {
			log.Printf("[Extract] Amazon GetCatalogItem %s error: %v", asin, err)
			result.Errors = append(result.Errors, fmt.Sprintf("ASIN %s: %v", asin, err))
			result.Skipped++
			continue
		}

		// Build enriched_data from catalog item
		enriched := map[string]interface{}{
			"source":      "amazon_extract",
			"asin":        item.ASIN,
			"marketplace": marketplaceID,
			"attributes":  item.Attributes,
			"summaries":   item.Summaries,
			"extracted_at": time.Now().Format(time.RFC3339),
		}

		// Extract title
		title := asin
		for _, s := range item.Summaries {
			if s.ItemName != "" {
				title = s.ItemName
				break
			}
		}

		// Extract image URL
		var imageURL string
		for _, imgGroup := range item.Images {
			if imgGroup.Variant == "MAIN" && len(imgGroup.Images) > 0 {
				imageURL = imgGroup.Images[0].Link
				break
			}
		}
		if imageURL != "" {
			enriched["main_image"] = imageURL
		}

		// Extract product type
		var productType string
		if len(item.ProductTypes) > 0 {
			productType = item.ProductTypes[0].ProductType
		}

		listing := h.buildExtractedListing(tenantID, "amazon", credID, productID, &ExtractListing{
			ExternalID: asin,
			ASIN:       asin,
			Title:      title,
			Status:     "active",
		}, enriched)
		listing.Overrides = &models.ListingOverrides{
			Title:           title,
			CategoryMapping: productType,
		}
		listing.ChannelIdentifiers = &models.ChannelIdentifiers{
			ListingID: asin,
			SKU:       asin,
		}
		listing.MarketplaceID = marketplaceID

		if err := h.repo.CreateListing(ctx, listing); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("ASIN %s save: %v", asin, err))
			result.Skipped++
			continue
		}
		result.ListingIDs = append(result.ListingIDs, listing.ListingID)
		result.Extracted++
	}
}

func (h *ExtractHandler) extractEbay(ctx context.Context, tenantID, credentialID string, externalIDs []string, productID string, result *ExtractResult) {
	client, err := h.getEbayClientForExtract(ctx, tenantID, credentialID)
	if err != nil {
		result.Errors = append(result.Errors, "auth: "+err.Error())
		return
	}

	for _, sku := range externalIDs {
		item, err := client.GetInventoryItem(sku)
		if err != nil {
			log.Printf("[Extract] eBay GetInventoryItem %s error: %v", sku, err)
			result.Errors = append(result.Errors, fmt.Sprintf("SKU %s: %v", sku, err))
			result.Skipped++
			continue
		}

		// Build enriched data
		enriched := map[string]interface{}{
			"source":       "ebay_extract",
			"sku":          item.SKU,
			"condition":    item.Condition,
			"extracted_at": time.Now().Format(time.RFC3339),
		}

		title := sku
		var imageURL string
		var price float64
		var qty int

		if item.Product != nil {
			enriched["product"] = map[string]interface{}{
				"title":       item.Product.Title,
				"description": item.Product.Description,
				"aspects":     item.Product.Aspects,
				"brand":       item.Product.Brand,
				"mpn":         item.Product.MPN,
				"ean":         item.Product.EAN,
				"image_urls":  item.Product.ImageURLs,
			}
			if item.Product.Title != "" {
				title = item.Product.Title
			}
			if len(item.Product.ImageURLs) > 0 {
				imageURL = item.Product.ImageURLs[0]
			}
		}
		if item.Availability != nil && item.Availability.ShipToLocationAvailability != nil {
			qty = item.Availability.ShipToLocationAvailability.Quantity
			enriched["quantity"] = qty
		}
		if imageURL != "" {
			enriched["main_image"] = imageURL
		}

		listing := h.buildExtractedListing(tenantID, "ebay", credentialID, productID, &ExtractListing{
			ExternalID: sku,
			SKU:        sku,
			Title:      title,
			Price:      price,
			Quantity:   qty,
			Status:     "active",
		}, enriched)
		listing.Overrides = &models.ListingOverrides{
			Title: title,
		}
		listing.ChannelIdentifiers = &models.ChannelIdentifiers{SKU: sku}

		if err := h.repo.CreateListing(ctx, listing); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("SKU %s save: %v", sku, err))
			result.Skipped++
			continue
		}
		result.ListingIDs = append(result.ListingIDs, listing.ListingID)
		result.Extracted++
	}
}

func (h *ExtractHandler) extractShopify(ctx context.Context, tenantID, credentialID string, externalIDs []string, productID string, result *ExtractResult) {
	client, credID, err := h.getShopifyClientForExtract(ctx, tenantID, credentialID)
	if err != nil {
		result.Errors = append(result.Errors, "auth: "+err.Error())
		return
	}

	for _, shopifyProductID := range externalIDs {
		resp, err := client.doWithHeaders(ctx, "GET", fmt.Sprintf("/products/%s.json", shopifyProductID), nil, nil)
		if err != nil {
			log.Printf("[Extract] Shopify get product %s error: %v", shopifyProductID, err)
			result.Errors = append(result.Errors, fmt.Sprintf("product %s: %v", shopifyProductID, err))
			result.Skipped++
			continue
		}

		product, _ := resp.body["product"].(map[string]interface{})
		if product == nil {
			result.Errors = append(result.Errors, fmt.Sprintf("product %s: not found", shopifyProductID))
			result.Skipped++
			continue
		}

		title, _ := product["title"].(string)
		if title == "" {
			title = shopifyProductID
		}

		enriched := map[string]interface{}{
			"source":       "shopify_extract",
			"product_id":   shopifyProductID,
			"product":      product,
			"extracted_at": time.Now().Format(time.RFC3339),
		}

		// Get first variant for SKU and price
		var sku string
		var price float64
		var qty int
		var imageURL string

		if variants, ok := product["variants"].([]interface{}); ok && len(variants) > 0 {
			if v, ok := variants[0].(map[string]interface{}); ok {
				sku, _ = v["sku"].(string)
				if priceStr, ok := v["price"].(string); ok {
					price, _ = strconv.ParseFloat(priceStr, 64)
				}
				if q, ok := v["inventory_quantity"].(float64); ok {
					qty = int(q)
				}
			}
		}
		if img, ok := product["image"].(map[string]interface{}); ok {
			imageURL, _ = img["src"].(string)
		}
		if imageURL != "" {
			enriched["main_image"] = imageURL
		}

		listing := h.buildExtractedListing(tenantID, "shopify", credID, productID, &ExtractListing{
			ExternalID: shopifyProductID,
			SKU:        sku,
			Title:      title,
			Price:      price,
			Quantity:   qty,
			Status:     "active",
		}, enriched)
		if listing.Overrides == nil {
			listing.Overrides = &models.ListingOverrides{}
		}
		listing.Overrides.Title = title
		if price > 0 {
			listing.Overrides.Price = &price
		}
		listing.ChannelIdentifiers = &models.ChannelIdentifiers{
			ListingID: shopifyProductID,
			SKU:       sku,
		}

		if err := h.repo.CreateListing(ctx, listing); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("product %s save: %v", shopifyProductID, err))
			result.Skipped++
			continue
		}
		result.ListingIDs = append(result.ListingIDs, listing.ListingID)
		result.Extracted++
	}
}

func (h *ExtractHandler) extractWalmart(ctx context.Context, tenantID, credentialID string, externalIDs []string, productID string, result *ExtractResult) {
	_, credID, err := h.getWalmartClientForExtract(ctx, tenantID, credentialID)
	if err != nil {
		result.Errors = append(result.Errors, "auth: "+err.Error())
		return
	}

	// Walmart's get item API is by SKU; items come from browsing so we already have metadata.
	// We create listings from the external_ids (SKUs) with enriched_data to be filled later.
	for _, sku := range externalIDs {
		enriched := map[string]interface{}{
			"source":       "walmart_extract",
			"sku":          sku,
			"extracted_at": time.Now().Format(time.RFC3339),
		}

		listing := h.buildExtractedListing(tenantID, "walmart", credID, productID, &ExtractListing{
			ExternalID: sku,
			SKU:        sku,
			Title:      sku,
			Status:     "active",
		}, enriched)
		listing.ChannelIdentifiers = &models.ChannelIdentifiers{SKU: sku}

		if err := h.repo.CreateListing(ctx, listing); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("SKU %s save: %v", sku, err))
			result.Skipped++
			continue
		}
		result.ListingIDs = append(result.ListingIDs, listing.ListingID)
		result.Extracted++
	}
}

// buildExtractedListing creates a Listing record in "imported" state with enriched_data populated.
func (h *ExtractHandler) buildExtractedListing(tenantID, channel, credentialID, productID string, el *ExtractListing, enrichedData map[string]interface{}) *models.Listing {
	now := time.Now()
	enrichedAt := now
	listing := &models.Listing{
		ListingID:        uuid.New().String(),
		TenantID:         tenantID,
		Channel:          channel,
		ChannelAccountID: credentialID,
		State:            "imported",
		ProductID:        productID,
		EnrichedData:     enrichedData,
		EnrichedAt:       &enrichedAt,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	return listing
}

// ─── POST /extract/listings/:listing_id/link ──────────────────────────────────
// CLM-02: Links an existing imported listing to an internal MarketMate product.
// Body: { "product_id": "...", "product_sku": "..." }

func (h *ExtractHandler) LinkListingToProduct(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	listingID := c.Param("listing_id")
	ctx := c.Request.Context()

	var req struct {
		ProductID  string `json:"product_id" binding:"required"`
		ProductSKU string `json:"product_sku"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	// Verify the listing exists and belongs to this tenant
	listing, err := h.repo.GetListing(ctx, tenantID, listingID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"ok": false, "error": "listing not found"})
		return
	}

	// Update product_id and product_sku on the listing
	listingRef := h.firestoreRepo.GetClient().
		Collection("tenants").Doc(tenantID).
		Collection("listings").Doc(listingID)

	updates := []firestore.Update{
		{Path: "product_id", Value: req.ProductID},
		{Path: "updated_at", Value: time.Now()},
	}
	if req.ProductSKU != "" {
		updates = append(updates, firestore.Update{Path: "product_sku", Value: req.ProductSKU})
	}
	// If listing was unlinked (no product), promote to "ready"
	if listing.ProductID == "" && listing.State == "imported" {
		updates = append(updates, firestore.Update{Path: "state", Value: "ready"})
	}

	if _, err := listingRef.Update(ctx, updates); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "failed to update listing: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":         true,
		"listing_id": listingID,
		"product_id": req.ProductID,
		"message":    "Listing successfully linked to product",
	})
}

// ============================================================================
// INTERNAL: Channel client builders for Extract context
// ============================================================================

func (h *ExtractHandler) getAmazonClientForExtract(ctx context.Context, tenantID, credentialID string) (*amazon.SPAPIClient, string, string, error) {
	var cred *models.MarketplaceCredential
	if credentialID != "" {
		c, err := h.repo.GetCredential(ctx, tenantID, credentialID)
		if err != nil {
			return nil, "", "", fmt.Errorf("get credential: %w", err)
		}
		cred = c
	} else {
		creds, err := h.repo.ListCredentials(ctx, tenantID)
		if err != nil {
			return nil, "", "", fmt.Errorf("list credentials: %w", err)
		}
		for _, c := range creds {
			if c.Channel == "amazon" && c.Active {
				cp := c
				cred = &cp
				break
			}
		}
		if cred == nil {
			return nil, "", "", fmt.Errorf("no Amazon credential found")
		}
	}

	mergedCreds, err := h.marketplaceService.GetFullCredentials(ctx, cred)
	if err != nil {
		return nil, "", "", fmt.Errorf("merge credentials: %w", err)
	}

	config := &amazon.SPAPIConfig{
		LWAClientID:     mergedCreds["lwa_client_id"],
		LWAClientSecret: mergedCreds["lwa_client_secret"],
		RefreshToken:    mergedCreds["refresh_token"],
		MarketplaceID:   mergedCreds["marketplace_id"],
		Region:          mergedCreds["region"],
		SellerID:        mergedCreds["seller_id"],
	}
	if config.LWAClientID == "" || config.LWAClientSecret == "" || config.RefreshToken == "" {
		return nil, "", "", fmt.Errorf("incomplete Amazon credentials (need lwa_client_id, lwa_client_secret, refresh_token)")
	}
	if config.MarketplaceID == "" {
		config.MarketplaceID = "A1F83G8C2ARO7P" // default EU
	}

	client, err := amazon.NewSPAPIClient(ctx, config)
	if err != nil {
		return nil, "", "", fmt.Errorf("build Amazon client: %w", err)
	}
	return client, cred.CredentialID, config.MarketplaceID, nil
}

func (h *ExtractHandler) getEbayClientForExtract(ctx context.Context, tenantID, credentialID string) (*ebay.Client, error) {
	var cred *models.MarketplaceCredential
	if credentialID != "" {
		c, err := h.repo.GetCredential(ctx, tenantID, credentialID)
		if err != nil {
			return nil, fmt.Errorf("get credential: %w", err)
		}
		cred = c
	} else {
		creds, err := h.repo.ListCredentials(ctx, tenantID)
		if err != nil {
			return nil, fmt.Errorf("list credentials: %w", err)
		}
		for _, c := range creds {
			if c.Channel == "ebay" && c.Active {
				cp := c
				cred = &cp
				break
			}
		}
		if cred == nil {
			return nil, fmt.Errorf("no eBay credential found")
		}
	}

	mergedCreds, err := h.marketplaceService.GetFullCredentials(ctx, cred)
	if err != nil {
		return nil, fmt.Errorf("merge credentials: %w", err)
	}

	clientID := mergedCreds["client_id"]
	clientSecret := mergedCreds["client_secret"]
	devID := mergedCreds["dev_id"]
	production := mergedCreds["environment"] != "sandbox"

	client := ebay.NewClient(clientID, clientSecret, devID, production)
	accessToken := mergedCreds["access_token"]
	refreshToken := mergedCreds["refresh_token"]
	if accessToken != "" {
		client.SetTokens(accessToken, refreshToken)
	}
	return client, nil
}

// shopifyExtractClient wraps the embedded shopify client pattern with header support.
type shopifyExtractClient struct {
	shopDomain  string
	accessToken string
	apiVersion  string
	httpClient  *http.Client
}

type shopifyResult struct {
	body    map[string]interface{}
	headers http.Header
}

func (s *shopifyExtractClient) doWithHeaders(ctx context.Context, method, path string, queryParams map[string]string, body interface{}) (*shopifyResult, error) {
	apiVersion := s.apiVersion
	if apiVersion == "" {
		apiVersion = "2024-01"
	}
	fullURL := fmt.Sprintf("https://%s/admin/api/%s%s", s.shopDomain, apiVersion, path)

	if len(queryParams) > 0 {
		parts := make([]string, 0, len(queryParams))
		for k, v := range queryParams {
			parts = append(parts, k+"="+v)
		}
		fullURL += "?" + strings.Join(parts, "&")
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Shopify-Access-Token", s.accessToken)
	req.Header.Set("Content-Type", "application/json")

	client := s.httpClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("Shopify API %d for %s", resp.StatusCode, path)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return &shopifyResult{body: result, headers: resp.Header}, nil
}

func (h *ExtractHandler) getShopifyClientForExtract(ctx context.Context, tenantID, credentialID string) (*shopifyExtractClient, string, error) {
	var cred *models.MarketplaceCredential
	if credentialID != "" {
		c, err := h.repo.GetCredential(ctx, tenantID, credentialID)
		if err != nil {
			return nil, "", fmt.Errorf("get credential: %w", err)
		}
		cred = c
	} else {
		creds, err := h.repo.ListCredentials(ctx, tenantID)
		if err != nil {
			return nil, "", fmt.Errorf("list credentials: %w", err)
		}
		for _, c := range creds {
			if c.Channel == "shopify" && c.Active {
				cp := c
				cred = &cp
				break
			}
		}
		if cred == nil {
			return nil, "", fmt.Errorf("no Shopify credential found")
		}
	}

	shopDomain := cred.CredentialData["shop_domain"]
	accessToken := cred.CredentialData["access_token"]
	apiVersion := cred.CredentialData["api_version"]
	if shopDomain == "" || accessToken == "" {
		return nil, "", fmt.Errorf("Shopify credential missing shop_domain or access_token")
	}

	return &shopifyExtractClient{
		shopDomain:  shopDomain,
		accessToken: accessToken,
		apiVersion:  apiVersion,
	}, cred.CredentialID, nil
}

func (h *ExtractHandler) getWalmartClientForExtract(ctx context.Context, tenantID, credentialID string) (*walmart.Client, string, error) {
	var cred *models.MarketplaceCredential
	if credentialID != "" {
		c, err := h.repo.GetCredential(ctx, tenantID, credentialID)
		if err != nil {
			return nil, "", fmt.Errorf("get credential: %w", err)
		}
		cred = c
	} else {
		creds, err := h.repo.ListCredentials(ctx, tenantID)
		if err != nil {
			return nil, "", fmt.Errorf("list credentials: %w", err)
		}
		for _, c := range creds {
			if c.Channel == "walmart" && c.Active {
				cp := c
				cred = &cp
				break
			}
		}
		if cred == nil {
			return nil, "", fmt.Errorf("no Walmart credential found")
		}
	}

	mergedCreds, err := h.marketplaceService.GetFullCredentials(ctx, cred)
	if err != nil {
		return nil, "", fmt.Errorf("merge credentials: %w", err)
	}

	clientID := mergedCreds["client_id"]
	clientSecret := mergedCreds["client_secret"]
	if clientID == "" || clientSecret == "" {
		return nil, "", fmt.Errorf("incomplete Walmart credentials: client_id and client_secret required")
	}

	return walmart.NewClient(clientID, clientSecret), cred.CredentialID, nil
}
