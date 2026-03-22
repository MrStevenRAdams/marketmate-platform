package adapters

import (
	"context"
	"html"
	"regexp"
	"fmt"
	"log"
	"strings"
	"time"

	"module-a/marketplace"
	"module-a/marketplace/clients/ebay"
)

// ============================================================================
// EBAY MARKETPLACE ADAPTER
// ============================================================================
// Import strategy:
//   1. If specific list types requested → GetSellerList (full details, 200/page, per-user rate limit)
//   2. Otherwise → try Inventory API (fast, structured, but only API-created items)
//   3. If 0 results → fall back to GetSellerList with ActiveList
// ============================================================================

type EbayAdapter struct {
	credentials marketplace.Credentials
	client      *ebay.Client
}

func NewEbayAdapter(ctx context.Context, credentials marketplace.Credentials) (marketplace.MarketplaceAdapter, error) {
	clientID := credentials.Data["client_id"]
	clientSecret := credentials.Data["client_secret"]
	devID := credentials.Data["dev_id"]
	oauthToken := credentials.Data["oauth_token"]
	refreshToken := credentials.Data["refresh_token"]

	if clientID == "" || clientSecret == "" {
		return nil, fmt.Errorf("client_id and client_secret are required for eBay")
	}

	production := credentials.Environment == "production"
	client := ebay.NewClient(clientID, clientSecret, devID, production)

	if refreshToken != "" {
		client.SetTokens("", refreshToken)
	} else if oauthToken != "" {
		client.SetTokens(oauthToken, "")
	} else {
		return nil, fmt.Errorf("oauth_token or refresh_token is required for eBay")
	}

	if username := credentials.Data["seller_username"]; username != "" {
		client.SellerUsername = username
	}

	return &EbayAdapter{
		credentials: credentials,
		client:      client,
	}, nil
}

// ============================================================================
// CONNECTION
// ============================================================================

func (e *EbayAdapter) Connect(ctx context.Context, creds marketplace.Credentials) error    { return nil }
func (e *EbayAdapter) Disconnect(ctx context.Context) error                                { return nil }
func (e *EbayAdapter) RefreshAuth(ctx context.Context) error { return e.client.RefreshAccessToken() }

func (e *EbayAdapter) TestConnection(ctx context.Context) error {
	_, err := e.client.GetInventoryItems(1, 0)
	return err
}

func (e *EbayAdapter) GetConnectionStatus(ctx context.Context) (*marketplace.ConnectionStatus, error) {
	_, err := e.client.GetInventoryItems(1, 0)
	return &marketplace.ConnectionStatus{
		IsConnected: err == nil, LastChecked: time.Now(), LastSuccessful: time.Now(),
		ErrorMessage: func() string { if err != nil { return err.Error() }; return "" }(),
	}, nil
}

// ============================================================================
// IMPORT (eBay → PIM)
// ============================================================================

func (e *EbayAdapter) FetchListings(ctx context.Context, filters marketplace.ImportFilters) ([]marketplace.MarketplaceProduct, error) {
	pageSize := filters.PageSize
	if pageSize < 1 || pageSize > 100 {
		pageSize = 50
	}

	log.Printf("[eBay Import] ========== FETCH LISTINGS START ==========")
	log.Printf("[eBay Import] Filters: ExternalIDs=%v, PageSize=%d", filters.ExternalIDs, pageSize)

	// ── Selective import by SKU/ItemID ──
	if len(filters.ExternalIDs) > 0 {
		return e.fetchByExternalIDs(filters.ExternalIDs)
	}

	// ── Try Inventory API first ──
	inventoryProducts, err := e.fetchViaInventoryAPI(pageSize)
	if err != nil {
		log.Printf("[eBay Import] Inventory API error (non-fatal): %v", err)
	}

	if len(inventoryProducts) > 0 {
		log.Printf("[eBay Import] Inventory API returned %d products — using these", len(inventoryProducts))
		return inventoryProducts, nil
	}

	// ── Inventory API returned 0 → fall back to GetSellerList ──
	log.Printf("[eBay Import] Inventory API returned 0 items — falling back to GetSellerList")
	log.Printf("[eBay Import] Will use GetSellerList with ActiveList filter")

	tradingProducts, err := e.fetchViaTradingAPI(filters.ProgressCallback, filters.ProductCallback, filters.EbayListTypes)
	if err != nil {
		return nil, fmt.Errorf("both Inventory API (0 items) and Trading API failed: %w", err)
	}

	log.Printf("[eBay Import] Trading API returned %d products total", len(tradingProducts))
	return tradingProducts, nil
}

// ── Selective import ──

func (e *EbayAdapter) fetchByExternalIDs(ids []string) ([]marketplace.MarketplaceProduct, error) {
	var allProducts []marketplace.MarketplaceProduct

	for _, id := range ids {
		// Try Inventory API first (treats ID as SKU)
		item, err := e.client.GetInventoryItem(id)
		if err == nil && item != nil && item.Product != nil {
			log.Printf("[eBay Import] Found SKU=%s via Inventory API", id)
			product := e.convertToProduct(item, id)
			e.enrichWithOfferData(&product, id)
			allProducts = append(allProducts, product)
			continue
		}

		// Try Trading API GetItem (treats ID as eBay item number)
		log.Printf("[eBay Import] SKU=%s not in Inventory API, trying Trading API GetItem...", id)
		tradingItem, err := e.client.TradingGetItem(id)
		if err == nil && tradingItem != nil {
			log.Printf("[eBay Import] Found item %s via Trading API: %s", id, tradingItem.Title)
			product := e.convertTradingDetailToProduct(tradingItem)
			allProducts = append(allProducts, product)
			continue
		}

		log.Printf("[eBay Import] Could not find ID=%s in either API: %v", id, err)
	}

	log.Printf("[eBay Import] Fetched %d products by ID", len(allProducts))
	return allProducts, nil
}

// ── Inventory API import ──

func (e *EbayAdapter) fetchViaInventoryAPI(pageSize int) ([]marketplace.MarketplaceProduct, error) {
	var allProducts []marketplace.MarketplaceProduct
	offset := 0

	for {
		page, err := e.client.GetInventoryItems(pageSize, offset)
		if err != nil {
			return allProducts, err
		}

		if len(page.InventoryItems) == 0 {
			break
		}

		log.Printf("[eBay Import] Inventory API: offset=%d, items=%d, total=%d", offset, len(page.InventoryItems), page.Total)

		for _, item := range page.InventoryItems {
			if item.SKU == "" {
				continue
			}
			product := e.convertToProduct(&item, item.SKU)
			e.enrichWithOfferData(&product, item.SKU)
			allProducts = append(allProducts, product)
		}

		offset += len(page.InventoryItems)
		if offset >= page.Total || len(page.InventoryItems) < pageSize {
			break
		}
	}

	return allProducts, nil
}

// ── Trading API import (primary method for Seller Hub listings) ──
// Uses GetSellerList which returns full item details (title, description, item
// specifics, variations, images, identifiers) in bulk — 200 items per page.
// Rate limit: 300 calls per 15 seconds PER SELLER (user-level, not app-level).
// This replaces the old GetMyeBaySelling + per-item GetItem pattern which
// consumed one app-level API call per listing.

func (e *EbayAdapter) fetchViaTradingAPI(progressCb func(string) bool, productCb func(marketplace.MarketplaceProduct) bool, listTypes []string) ([]marketplace.MarketplaceProduct, error) {
	var allProducts []marketplace.MarketplaceProduct

	// Default to ActiveList only
	if len(listTypes) == 0 {
		listTypes = []string{"ActiveList"}
	}

	if progressCb != nil && !progressCb("Fetching listings from eBay via GetSellerList...") {
		return nil, nil
	}

	for i, listType := range listTypes {
		label := listType
		switch listType {
		case "ActiveList":
			label = "active"
		case "UnsoldList":
			label = "unsold"
		case "SoldList":
			label = "sold"
		case "ScheduledList":
			label = "scheduled"
		}

		if progressCb != nil {
			progressCb(fmt.Sprintf("Fetching %s listings (%d/%d)...", label, i+1, len(listTypes)))
		}

		products, err := e.fetchViaGetSellerList(listType, label, progressCb, productCb)
		if err != nil {
			if listType == "ActiveList" {
				return nil, fmt.Errorf("%s: %w", listType, err)
			}
			log.Printf("[eBay Import] %s failed (non-fatal): %v", listType, err)
			continue
		}
		allProducts = append(allProducts, products...)
	}

	log.Printf("[eBay Import] GetSellerList totals: %d products from %d list types",
		len(allProducts), len(listTypes))

	return allProducts, nil
}

func (e *EbayAdapter) fetchViaGetSellerList(listType, label string, progressCb func(string) bool, productCb func(marketplace.MarketplaceProduct) bool) ([]marketplace.MarketplaceProduct, error) {
	var allProducts []marketplace.MarketplaceProduct

	// GetSellerList requires an EndTime date range filter.
	// For active listings: EndTime is in the future (GTC listings renew monthly).
	// For ended/unsold: EndTime is in the past.
	now := time.Now().UTC()
	var endTimeFrom, endTimeTo string

	switch listType {
	case "ActiveList":
		// Active GTC listings have EndTime far in the future
		endTimeFrom = now.Format(time.RFC3339)
		endTimeTo = now.AddDate(0, 6, 0).Format(time.RFC3339) // 6 months ahead
	case "UnsoldList":
		// Ended within the last 60 days
		endTimeFrom = now.AddDate(0, 0, -60).Format(time.RFC3339)
		endTimeTo = now.Format(time.RFC3339)
	case "SoldList":
		// Sold within the last 60 days
		endTimeFrom = now.AddDate(0, 0, -60).Format(time.RFC3339)
		endTimeTo = now.Format(time.RFC3339)
	default:
		endTimeFrom = now.Format(time.RFC3339)
		endTimeTo = now.AddDate(0, 6, 0).Format(time.RFC3339)
	}

	pageNumber := 1
	entriesPerPage := 200 // max with DetailLevel=ReturnAll
	maxPages := 50        // safety: 50 * 200 = 10,000 items max

	for pageNumber <= maxPages {
		page, err := e.client.GetSellerList(pageNumber, entriesPerPage, endTimeFrom, endTimeTo)
		if err != nil {
			if len(allProducts) > 0 {
				log.Printf("[eBay Import] GetSellerList %s error at page %d, returning %d items so far: %v",
					label, pageNumber, len(allProducts), err)
				return allProducts, nil
			}
			return nil, err
		}

		if len(page.Items) == 0 {
			break
		}

		// Signal total on first page
		if progressCb != nil {
			if pageNumber == 1 && page.TotalItems > 0 {
				progressCb(fmt.Sprintf("__total__:%d", page.TotalItems))
			}
			progressCb(fmt.Sprintf("Fetching %s listings — page %d/%d (%d items so far)",
				label, pageNumber, page.TotalPages, len(allProducts)+len(page.Items)))
		}

		// Each item already has full details (item specifics, variations, images, description)
		// — no need for per-item GetItem calls
		for itemIdx, detail := range page.Items {
			if detail.ItemID == "" {
				continue
			}

			// For SoldList, only include items that have actually sold
			if listType == "SoldList" && detail.QuantitySold == 0 {
				continue
			}

			product := e.convertTradingDetailToProduct(detail)

			if productCb != nil {
				if !productCb(product) {
					log.Printf("[eBay Import] Cancellation signal — stopping at page %d, item %d", pageNumber, itemIdx)
					return allProducts, nil
				}
			} else {
				allProducts = append(allProducts, product)
			}
		}

		log.Printf("[eBay Import] GetSellerList %s page %d: %d items (total so far: %d)",
			label, pageNumber, len(page.Items), len(allProducts))

		if pageNumber >= page.TotalPages {
			break
		}
		pageNumber++
	}

	return allProducts, nil
}

// ============================================================================
// ENRICHMENT
// ============================================================================

func (e *EbayAdapter) enrichWithOfferData(product *marketplace.MarketplaceProduct, sku string) {
	offers, err := e.client.GetOffers(sku)
	if err != nil || len(offers.Offers) == 0 {
		return
	}
	offer := offers.Offers[0]
	if offer.PricingSummary != nil && offer.PricingSummary.Price != nil {
		var price float64
		fmt.Sscanf(offer.PricingSummary.Price.Value, "%f", &price)
		product.Price = price
		product.Currency = offer.PricingSummary.Price.Currency
	}
	if offer.CategoryID != "" {
		product.Categories = append(product.Categories, offer.CategoryID)
	}
	if offer.Listing != nil && offer.Listing.ListingID != "" {
		product.ListingURL = fmt.Sprintf("https://www.ebay.co.uk/itm/%s", offer.Listing.ListingID)
		product.ExternalID = offer.Listing.ListingID
	}
}

// ============================================================================
// CONVERSION — Inventory API item → MarketplaceProduct
// ============================================================================

func (e *EbayAdapter) convertToProduct(item *ebay.InventoryItem, sku string) marketplace.MarketplaceProduct {
	product := marketplace.MarketplaceProduct{
		ExternalID:         sku,
		SKU:                sku,
		Condition:          strings.ToLower(item.Condition),
		FulfillmentChannel: "DEFAULT",
		RawData:            item.Raw,
	}

	if item.Product != nil {
		product.Title = item.Product.Title
		product.Description = stripHTML(item.Product.Description)
		product.Brand = item.Product.Brand

		for i, imgURL := range item.Product.ImageURLs {
			product.Images = append(product.Images, marketplace.ImageData{URL: imgURL, Position: i, IsMain: i == 0})
		}

		if len(item.Product.Aspects) > 0 {
			attrs := make(map[string]interface{})
			for key, values := range item.Product.Aspects {
				attrKey := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(key), " ", "_"))
				if len(values) == 1 {
					attrs[attrKey] = values[0]
				} else {
					attrs[attrKey] = values
				}
			}
			product.Attributes = attrs
		}

		if len(item.Product.EAN) > 0 {
			product.Identifiers.EAN = item.Product.EAN[0]
		}
		if len(item.Product.UPC) > 0 {
			product.Identifiers.UPC = item.Product.UPC[0]
		}
		if item.Product.MPN != "" {
			if product.Attributes == nil { product.Attributes = make(map[string]interface{}) }
			product.Attributes["mpn"] = item.Product.MPN
		}
	}

	if item.Availability != nil && item.Availability.ShipToLocationAvailability != nil {
		product.Quantity = item.Availability.ShipToLocationAvailability.Quantity
		product.IsInStock = product.Quantity > 0
	}

	if item.PackageWeightAndSize != nil {
		if d := item.PackageWeightAndSize.Dimensions; d != nil {
			product.Dimensions = &marketplace.Dimensions{Length: d.Length, Width: d.Width, Height: d.Height, Unit: strings.ToLower(d.Unit)}
		}
		if w := item.PackageWeightAndSize.Weight; w != nil {
			product.Weight = &marketplace.Weight{Value: w.Value, Unit: strings.ToLower(w.Unit)}
		}
	}

	return product
}

// ============================================================================
// CONVERSION — Trading API full item detail → MarketplaceProduct
// ============================================================================

func (e *EbayAdapter) convertTradingDetailToProduct(item *ebay.TradingItemDetail) marketplace.MarketplaceProduct {
	product := marketplace.MarketplaceProduct{
		ExternalID:  item.ItemID,
		SKU:         item.SKU,
		Title:       item.Title,
		Description: item.Description,
		Brand:       item.Brand,
		Price:       item.CurrentPrice,
		Currency:    item.Currency,
		Quantity:    item.QuantityAvailable,
		IsInStock:   item.QuantityAvailable > 0,
		Condition:   strings.ToLower(item.ConditionName),
		ListingURL:  item.ViewItemURL,
		Attributes:  make(map[string]interface{}),
	}

	// Use ItemID as SKU if no SKU set
	if product.SKU == "" {
		product.SKU = item.ItemID
	}

	// Images
	for i, imgURL := range item.PictureURL {
		product.Images = append(product.Images, marketplace.ImageData{URL: imgURL, Position: i, IsMain: i == 0})
	}

	// Category
	if item.CategoryID != "" {
		product.Categories = append(product.Categories, item.CategoryID)
	}

	// Identifiers
	if item.EAN != "" && item.EAN != "Does not apply" {
		product.Identifiers.EAN = item.EAN
	}
	if item.UPC != "" && item.UPC != "Does not apply" {
		product.Identifiers.UPC = item.UPC
	}
	if item.ISBN != "" && item.ISBN != "Does not apply" {
		product.Identifiers.ISBN = item.ISBN
	}

	// Item specifics → attributes
	for name, values := range item.ItemSpecifics {
		attrKey := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(name), " ", "_"))
		if len(values) == 1 {
			product.Attributes[attrKey] = values[0]
		} else {
			product.Attributes[attrKey] = values
		}
	}

	// Additional metadata
	if item.MPN != "" {
		product.Attributes["mpn"] = item.MPN
	}
	if item.Brand != "" {
		product.Attributes["brand"] = item.Brand
	}
	if item.CategoryName != "" {
		product.Attributes["category_name"] = item.CategoryName
	}
	if item.ListingType != "" {
		product.Attributes["listing_type"] = item.ListingType
	}
	if item.ListingStatus != "" {
		product.Attributes["listing_status"] = item.ListingStatus
	}
	if item.QuantitySold > 0 {
		product.Attributes["quantity_sold"] = item.QuantitySold
	}
	if item.ConditionID != "" {
		product.Attributes["condition_id"] = item.ConditionID
	}

	// Raw data map for extended data storage
	product.RawData = map[string]interface{}{
		"item_id":         item.ItemID,
		"sku":             item.SKU,
		"listing_status":  item.ListingStatus,
		"listing_type":    item.ListingType,
		"category_id":     item.CategoryID,
		"category_name":   item.CategoryName,
		"condition_id":    item.ConditionID,
		"condition_name":  item.ConditionName,
		"start_time":      item.StartTime,
		"end_time":        item.EndTime,
		"quantity":        item.Quantity,
		"quantity_available": item.QuantityAvailable,
		"quantity_sold":   item.QuantitySold,
		"price":           item.CurrentPrice,
		"currency":        item.Currency,
		"brand":           item.Brand,
		"mpn":             item.MPN,
		"ean":             item.EAN,
		"upc":             item.UPC,
		"isbn":            item.ISBN,
		"view_item_url":   item.ViewItemURL,
		"item_specifics":  item.ItemSpecifics,
	}

	// Variations
	for _, v := range item.Variations {
		attrs := make(map[string]interface{})
		for k, val := range v.VariationSpecifics {
			attrs[strings.ToLower(strings.ReplaceAll(strings.TrimSpace(k), " ", "_"))] = val
		}
		var varImages []marketplace.ImageData
		for i, imgURL := range v.PictureURL {
			varImages = append(varImages, marketplace.ImageData{URL: imgURL, Position: i, IsMain: i == 0})
		}
		variation := marketplace.Variation{
			ExternalID: item.ItemID + "_" + v.SKU,
			SKU:        v.SKU,
			Attributes: attrs,
			Price:      v.Price,
			Quantity:   v.QuantityAvailable,
			Images:     varImages,
		}
		product.Variations = append(product.Variations, variation)
	}

	return product
}

// ============================================================================
// CONVERSION — Trading API summary (from GetMyeBaySelling) → MarketplaceProduct
// ============================================================================
// Used as fallback when GetItem fails for a specific item.

func (e *EbayAdapter) convertTradingSummaryToProduct(item *ebay.TradingSellingItem) marketplace.MarketplaceProduct {
	product := marketplace.MarketplaceProduct{
		ExternalID:  item.ItemID,
		SKU:         item.SKU,
		Title:       item.Title,
		Price:       item.CurrentPrice,
		Currency:    item.Currency,
		Quantity:    item.QuantityAvailable,
		IsInStock:   item.QuantityAvailable > 0,
		Condition:   strings.ToLower(item.ConditionName),
		ListingURL:  item.ViewItemURL,
		Attributes:  make(map[string]interface{}),
	}

	if product.SKU == "" {
		product.SKU = item.ItemID
	}

	for i, imgURL := range item.PictureURL {
		product.Images = append(product.Images, marketplace.ImageData{URL: imgURL, Position: i, IsMain: i == 0})
	}

	if item.PrimaryCategory.CategoryID != "" {
		product.Categories = append(product.Categories, item.PrimaryCategory.CategoryID)
	}

	product.Attributes["listing_status"] = item.ListingStatus
	product.Attributes["listing_type"] = item.ListingType
	if item.PrimaryCategory.CategoryName != "" {
		product.Attributes["category_name"] = item.PrimaryCategory.CategoryName
	}
	if item.QuantitySold > 0 {
		product.Attributes["quantity_sold"] = item.QuantitySold
	}

	product.RawData = map[string]interface{}{
		"item_id":            item.ItemID,
		"sku":                item.SKU,
		"listing_status":     item.ListingStatus,
		"listing_type":       item.ListingType,
		"category_id":        item.PrimaryCategory.CategoryID,
		"category_name":      item.PrimaryCategory.CategoryName,
		"condition_id":       item.ConditionID,
		"condition_name":     item.ConditionName,
		"quantity":           item.Quantity,
		"quantity_available":  item.QuantityAvailable,
		"quantity_sold":      item.QuantitySold,
		"price":              item.CurrentPrice,
		"currency":           item.Currency,
		"start_time":         item.StartTime,
		"end_time":           item.EndTime,
		"watch_count":        item.WatchCount,
		"hit_count":          item.HitCount,
	}

	return product
}

// ============================================================================
// OTHER INTERFACE METHODS
// ============================================================================

func (e *EbayAdapter) FetchProduct(ctx context.Context, externalID string) (*marketplace.MarketplaceProduct, error) {
	// Try Inventory API (SKU)
	item, err := e.client.GetInventoryItem(externalID)
	if err == nil && item != nil {
		product := e.convertToProduct(item, externalID)
		return &product, nil
	}
	// Try Trading API GetItem (item number)
	detail, err := e.client.TradingGetItem(externalID)
	if err == nil && detail != nil {
		product := e.convertTradingDetailToProduct(detail)
		return &product, nil
	}
	return nil, fmt.Errorf("item %s not found via Inventory API or Trading API", externalID)
}

func (e *EbayAdapter) FetchProductImages(ctx context.Context, externalID string) ([]marketplace.ImageData, error) {
	product, err := e.FetchProduct(ctx, externalID)
	if err != nil {
		return nil, err
	}
	return product.Images, nil
}

func (e *EbayAdapter) FetchInventory(ctx context.Context, externalID string) (*marketplace.InventoryLevel, error) {
	item, err := e.client.GetInventoryItem(externalID)
	if err != nil {
		return nil, err
	}
	qty := 0
	if item.Availability != nil && item.Availability.ShipToLocationAvailability != nil {
		qty = item.Availability.ShipToLocationAvailability.Quantity
	}
	return &marketplace.InventoryLevel{ExternalID: externalID, Quantity: qty, UpdatedAt: time.Now()}, nil
}

func (e *EbayAdapter) CreateListing(ctx context.Context, listing marketplace.ListingData) (*marketplace.ListingResult, error) {
	item := &ebay.InventoryItem{
		Condition: "NEW",
		Product: &ebay.Product{Title: listing.Title, Description: listing.Description, ImageURLs: listing.Images},
		Availability: &ebay.Availability{ShipToLocationAvailability: &ebay.ShipToLocation{Quantity: listing.Quantity}},
	}
	sku := listing.Identifiers.ASIN
	if sku == "" { sku = listing.Identifiers.EAN }
	if sku == "" { sku = listing.ProductID }
	if err := e.client.CreateOrReplaceInventoryItem(sku, item); err != nil {
		return nil, fmt.Errorf("create inventory item: %w", err)
	}
	offer := &ebay.Offer{
		SKU: sku, MarketplaceID: "EBAY_GB", Format: "FIXED_PRICE", CategoryID: listing.CategoryID,
		PricingSummary: &ebay.PricingSummary{Price: &ebay.Amount{Value: fmt.Sprintf("%.2f", listing.Price), Currency: "GBP"}},
		AvailableQuantity: listing.Quantity,
	}
	offerID, err := e.client.CreateOffer(offer)
	if err != nil { return nil, fmt.Errorf("create offer: %w", err) }
	listingID, err := e.client.PublishOffer(offerID)
	if err != nil { return nil, fmt.Errorf("publish offer: %w", err) }
	return &marketplace.ListingResult{ExternalID: listingID, Status: "published", URL: fmt.Sprintf("https://www.ebay.co.uk/itm/%s", listingID)}, nil
}

func (e *EbayAdapter) UpdateListing(ctx context.Context, externalID string, updates marketplace.ListingData) error {
	return fmt.Errorf("update listing not yet implemented for eBay")
}

func (e *EbayAdapter) DeleteListing(ctx context.Context, externalID string) error {
	return e.client.DeleteInventoryItem(externalID)
}

func (e *EbayAdapter) PublishListing(ctx context.Context, externalID string) error {
	_, err := e.client.PublishOffer(externalID); return err
}

func (e *EbayAdapter) UnpublishListing(ctx context.Context, externalID string) error {
	return e.client.WithdrawOffer(externalID)
}

func (e *EbayAdapter) BulkCreateListings(ctx context.Context, listings []marketplace.ListingData) ([]marketplace.ListingResult, error) {
	var results []marketplace.ListingResult
	for _, l := range listings {
		result, err := e.CreateListing(ctx, l)
		if err != nil { results = append(results, marketplace.ListingResult{Status: "error"}) } else { results = append(results, *result) }
	}
	return results, nil
}

func (e *EbayAdapter) BulkUpdateListings(ctx context.Context, updates []marketplace.ListingUpdate) ([]marketplace.UpdateResult, error) {
	return nil, fmt.Errorf("bulk update not yet implemented for eBay")
}

func (e *EbayAdapter) GetListingStatus(ctx context.Context, externalID string) (*marketplace.ListingStatus, error) {
	offers, err := e.client.GetOffers(externalID)
	if err != nil { return nil, err }
	status := "unknown"; isActive := false
	if len(offers.Offers) > 0 { status = strings.ToLower(offers.Offers[0].Status); isActive = status == "published" }
	return &marketplace.ListingStatus{ExternalID: externalID, Status: status, IsActive: isActive, LastUpdated: time.Now()}, nil
}

func (e *EbayAdapter) SyncInventory(ctx context.Context, externalID string, quantity int) error {
	item := &ebay.InventoryItem{Availability: &ebay.Availability{ShipToLocationAvailability: &ebay.ShipToLocation{Quantity: quantity}}}
	return e.client.CreateOrReplaceInventoryItem(externalID, item)
}

func (e *EbayAdapter) SyncPrice(ctx context.Context, externalID string, price float64) error {
	return fmt.Errorf("price sync not yet implemented for eBay")
}

// ============================================================================
// METADATA
// ============================================================================

func (e *EbayAdapter) GetName() string       { return "ebay" }
func (e *EbayAdapter) GetDisplayName() string { return "eBay" }
func (e *EbayAdapter) GetSupportedFeatures() []string {
	return []string{"import", "listing_creation", "publish_unpublish", "listing_status", "inventory_sync", "trading_api_import"}
}
func (e *EbayAdapter) GetRequiredFields() []marketplace.RequiredField {
	return []marketplace.RequiredField{
		{Name: "title", Type: "string", Description: "Listing title (max 80 chars)"},
		{Name: "description", Type: "string", Description: "Listing description (HTML allowed)"},
		{Name: "price", Type: "number", Description: "Listing price"},
		{Name: "images", Type: "array", Description: "Product image URLs"},
		{Name: "category_id", Type: "string", Description: "eBay category ID"},
		{Name: "sku", Type: "string", Description: "Seller-defined SKU (max 50 chars)"},
		{Name: "condition", Type: "string", Description: "Item condition (NEW, USED, etc.)"},
	}
}
func (e *EbayAdapter) GetCategories(ctx context.Context) ([]marketplace.Category, error) {
	return nil, fmt.Errorf("use GET /api/v1/ebay/categories/suggest?q=<query> for eBay category suggestions")
}
func (e *EbayAdapter) ValidateListing(ctx context.Context, listing marketplace.ListingData) (*marketplace.ValidationResult, error) {
	result := &marketplace.ValidationResult{IsValid: true}
	if listing.Title == "" {
		result.IsValid = false
		result.Errors = append(result.Errors, marketplace.ValidationError{Code: "MISSING_TITLE", Field: "title", Message: "Title required", Severity: "error"})
	}
	if len(listing.Title) > 80 {
		result.Warnings = append(result.Warnings, marketplace.ValidationError{Code: "TITLE_TOO_LONG", Field: "title", Message: "Title exceeds 80 chars", Severity: "warning"})
	}
	if len(listing.Images) == 0 {
		result.IsValid = false
		result.Errors = append(result.Errors, marketplace.ValidationError{Code: "MISSING_IMAGES", Field: "images", Message: "At least one image required", Severity: "error"})
	}
	return result, nil
}

// CancelOrder — eBay order cancellation requires a case process not available via Trading API.
func (e *EbayAdapter) CancelOrder(ctx context.Context, externalOrderID string) error {
	return marketplace.ErrCancelNotSupported
}

// stripHTML removes HTML tags and decodes HTML entities, returning plain text.
var reHTMLTag = regexp.MustCompile(`<[^>]+>`)

func stripHTML(s string) string {
	s = reHTMLTag.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	s = strings.Join(strings.Fields(s), " ")
	return strings.TrimSpace(s)
}
