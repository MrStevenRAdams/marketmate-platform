package adapters

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"module-a/marketplace"
	"module-a/marketplace/clients/temu"
)

// ============================================================================
// TEMU MARKETPLACE ADAPTER
// ============================================================================
// Implements the MarketplaceAdapter interface for Temu marketplace.
// Uses the Temu Open Platform API via the temu.Client.
// ============================================================================

type TemuAdapter struct {
	credentials marketplace.Credentials
	client      *temu.Client
}

// NewTemuAdapter creates a new Temu marketplace adapter
func NewTemuAdapter(ctx context.Context, credentials marketplace.Credentials) (marketplace.MarketplaceAdapter, error) {
	appKey := credentials.Data["app_key"]
	appSecret := credentials.Data["app_secret"]
	accessToken := credentials.Data["access_token"]
	baseURL := credentials.Data["base_url"]

	if appKey == "" || appSecret == "" {
		return nil, fmt.Errorf("app_key and app_secret are required for Temu")
	}
	if accessToken == "" {
		return nil, fmt.Errorf("access_token is required for Temu")
	}
	if baseURL == "" {
		baseURL = temu.TemuBaseURLEU
	}

	client := temu.NewClient(baseURL, appKey, appSecret, accessToken)

	return &TemuAdapter{
		credentials: credentials,
		client:      client,
	}, nil
}

// ============================================================================
// CONNECTION & AUTHENTICATION
// ============================================================================

func (t *TemuAdapter) Connect(ctx context.Context, credentials marketplace.Credentials) error {
	return nil
}

func (t *TemuAdapter) Disconnect(ctx context.Context) error {
	return nil
}

func (t *TemuAdapter) TestConnection(ctx context.Context) error {
	// Use category list as connectivity test — mall info is IP-restricted on some accounts
	_, err := t.client.GetCategories(nil)
	if err != nil {
		return fmt.Errorf("Temu connection test failed: %w", err)
	}
	return nil
}

func (t *TemuAdapter) RefreshAuth(ctx context.Context) error {
	// Temu access tokens are long-lived, no refresh needed
	return nil
}

func (t *TemuAdapter) GetConnectionStatus(ctx context.Context) (*marketplace.ConnectionStatus, error) {
	err := t.TestConnection(ctx)
	return &marketplace.ConnectionStatus{
		IsConnected:    err == nil,
		LastChecked:    time.Now(),
		LastSuccessful: time.Now(),
		ErrorMessage: func() string {
			if err != nil {
				return err.Error()
			}
			return ""
		}(),
	}, nil
}

// ============================================================================
// PRODUCT IMPORT (Temu → PIM)
// ============================================================================

func (t *TemuAdapter) FetchListings(ctx context.Context, filters marketplace.ImportFilters) ([]marketplace.MarketplaceProduct, error) {
	pageSize := filters.PageSize
	if pageSize < 1 || pageSize > 100 {
		pageSize = 50
	}

	var allProducts []marketplace.MarketplaceProduct

	// If specific external IDs requested, fetch each directly
	if len(filters.ExternalIDs) > 0 {
		for _, idStr := range filters.ExternalIDs {
			var goodsID int64
			if _, err := fmt.Sscanf(idStr, "%d", &goodsID); err != nil {
				log.Printf("[Temu Import] Skipping invalid goodsId: %s", idStr)
				continue
			}
			detail, err := t.client.GetGoodsDetail(goodsID)
			if err != nil {
				log.Printf("[Temu Import] Failed to fetch goodsId=%d: %v", goodsID, err)
				continue
			}
			product := t.convertDetailToProduct(detail)
			allProducts = append(allProducts, product)
		}
		log.Printf("[Temu Import] Fetched %d products by ID", len(allProducts))
		return allProducts, nil
	}

	// Determine which status filters to query
	// goodsStatusFilterType: 1=Active/Inactive, 4=Incomplete, 5=Draft, 6=Deleted
	statusFilters := filters.TemuStatusFilters
	if len(statusFilters) == 0 {
		statusFilters = []int{1} // Default: Active/Inactive products
	}

	statusLabels := map[int]string{1: "Active/Inactive", 4: "Incomplete", 5: "Draft", 6: "Deleted"}
	seen := make(map[string]bool) // Deduplicate across status filters

	// Full import — iterate each status filter, paginate through all products
	for _, statusFilter := range statusFilters {
		label := statusLabels[statusFilter]
		if label == "" {
			label = fmt.Sprintf("status_%d", statusFilter)
		}
		log.Printf("[Temu Import] Fetching products with goodsStatusFilterType=%d (%s)", statusFilter, label)

		page := 1
		statusTotal := 0
		for {
			// Report progress so the UI shows something is happening
			if filters.ProgressCallback != nil {
				if !filters.ProgressCallback(fmt.Sprintf("Fetching %s products from Temu — page %d...", label, page)) {
					log.Printf("[Temu Import] Cancellation signal received at page %d — stopping fetch", page)
					return allProducts, nil
				}
			}

			listPage, err := t.client.ListGoods(page, pageSize, statusFilter)
			if err != nil {
				log.Printf("[Temu Import] Error fetching status=%d page=%d: %v", statusFilter, page, err)
				return allProducts, fmt.Errorf("list goods (status=%d) page %d: %w", statusFilter, page, err)
			}

			if len(listPage.GoodsList) == 0 {
				break
			}

			log.Printf("[Temu Import] Status=%d (%s) page %d: %d products (total reported: %d)", statusFilter, label, page, len(listPage.GoodsList), listPage.Total)

			// Report total count to service so progress bar works correctly
			if listPage.Total > 0 && filters.ProgressCallback != nil {
				filters.ProgressCallback(fmt.Sprintf("__total__:%d", listPage.Total))
			}

			newThisPage := 0
			for _, goods := range listPage.GoodsList {
				goodsKey := fmt.Sprintf("%d", goods.GoodsID)
				if seen[goodsKey] {
					continue // Skip duplicates across status filters
				}
				seen[goodsKey] = true
				newThisPage++

				// Fetch full detail for each product
				var product marketplace.MarketplaceProduct
				detail, err := t.client.GetGoodsDetail(goods.GoodsID)
				if err != nil {
					log.Printf("[Temu Import] WARNING: failed to get detail for goodsId=%d (%s): %v", goods.GoodsID, goods.GoodsName, err)
					product = t.convertListItemToProduct(&goods)
				} else {
					product = t.convertDetailToProduct(detail)
				}

				// Save immediately via callback (incremental) or buffer for batch return
				if filters.ProductCallback != nil {
					if !filters.ProductCallback(product) {
						log.Printf("[Temu Import] Cancellation signal from ProductCallback — stopping")
						return allProducts, nil
					}
				} else {
					allProducts = append(allProducts, product)
				}
				statusTotal++
			}

			// Stop if: API returned a partial page, we hit the reported total,
			// OR no new products were found this page (API looping / deduplication exhausted)
			if len(listPage.GoodsList) < pageSize {
				log.Printf("[Temu Import] Partial page (%d < %d) — end of results", len(listPage.GoodsList), pageSize)
				break
			}
			if listPage.Total > 0 && statusTotal >= listPage.Total {
				log.Printf("[Temu Import] Reached reported total (%d)", listPage.Total)
				break
			}
			if newThisPage == 0 {
				log.Printf("[Temu Import] No new products on page %d — API may be cycling, stopping", page)
				break
			}
			page++
		}

		log.Printf("[Temu Import] Status=%d (%s): fetched %d products", statusFilter, label, statusTotal)
	}

	log.Printf("[Temu Import] Total products fetched across all statuses: %d", len(allProducts))
	return allProducts, nil
}

func (t *TemuAdapter) FetchProduct(ctx context.Context, externalID string) (*marketplace.MarketplaceProduct, error) {
	var goodsID int64
	if _, err := fmt.Sscanf(externalID, "%d", &goodsID); err != nil {
		return nil, fmt.Errorf("invalid Temu goodsId: %s", externalID)
	}

	detail, err := t.client.GetGoodsDetail(goodsID)
	if err != nil {
		return nil, err
	}

	product := t.convertDetailToProduct(detail)
	return &product, nil
}

func (t *TemuAdapter) FetchProductImages(ctx context.Context, externalID string) ([]marketplace.ImageData, error) {
	var goodsID int64
	if _, err := fmt.Sscanf(externalID, "%d", &goodsID); err != nil {
		return nil, fmt.Errorf("invalid Temu goodsId: %s", externalID)
	}

	detail, err := t.client.GetGoodsDetail(goodsID)
	if err != nil {
		return nil, err
	}

	return t.extractImages(detail), nil
}

func (t *TemuAdapter) FetchInventory(ctx context.Context, externalID string) (*marketplace.InventoryLevel, error) {
	var goodsID int64
	if _, err := fmt.Sscanf(externalID, "%d", &goodsID); err != nil {
		return nil, fmt.Errorf("invalid Temu goodsId: %s", externalID)
	}

	skus, err := t.client.ListSKUs(goodsID)
	if err != nil {
		return nil, err
	}

	totalStock := 0
	for _, sku := range skus {
		totalStock += sku.Stock
	}

	return &marketplace.InventoryLevel{
		ExternalID: externalID,
		Quantity:   totalStock,
		UpdatedAt:  time.Now(),
	}, nil
}

// ============================================================================
// CONVERSION: Temu → Marketplace standard types
// ============================================================================

func (t *TemuAdapter) convertDetailToProduct(detail *temu.TemuGoodsDetail) marketplace.MarketplaceProduct {
	product := marketplace.MarketplaceProduct{
		ExternalID:  fmt.Sprintf("%d", detail.GoodsID),
		Title:       detail.GoodsName,
		Description: detail.GoodsDesc,
		SKU:         detail.OutGoodsSn,
		Categories:  []string{fmt.Sprintf("%d", detail.CatID)},
		Condition:   "new",
		IsInStock:   detail.OnSale == 1,
		ListingURL:  fmt.Sprintf("https://www.temu.com/goods-%d.html", detail.GoodsID),
		RawData:     detail.Raw,
	}

	// If no external SKU, use Temu's internal code
	if product.SKU == "" {
		product.SKU = detail.GoodsSn
	}

	// Images
	product.Images = t.extractImages(detail)

	// Brand
	if detail.BrandInfo != nil {
		if bn, ok := detail.BrandInfo["brandName"].(string); ok {
			product.Brand = bn
		}
	}

	// Properties → Attributes
	if len(detail.GoodsProperty) > 0 {
		attrs := make(map[string]interface{})
		for _, prop := range detail.GoodsProperty {
			name := ""
			value := ""
			for _, nk := range []string{"propertyName", "name"} {
				if n, ok := prop[nk].(string); ok {
					name = n
					break
				}
			}
			for _, vk := range []string{"propertyValue", "value"} {
				if v, ok := prop[vk].(string); ok {
					value = v
					break
				}
			}
			if name != "" && value != "" {
				key := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(name), " ", "_"))
				attrs[key] = value
			}
		}
		product.Attributes = attrs
	}

	// Bullet points as attribute
	if len(detail.BulletPoints) > 0 {
		if product.Attributes == nil {
			product.Attributes = make(map[string]interface{})
		}
		product.Attributes["bullet_points"] = detail.BulletPoints
	}

	// SKU data — price, quantity, variations
	if len(detail.SkuList) > 0 {
		firstSKU := detail.SkuList[0]

		// Price from first SKU
		product.Price = t.extractPrice(firstSKU.Price)
		product.Currency = t.extractCurrency(firstSKU.Price)

		// Total stock across all SKUs
		totalStock := 0
		for _, sku := range detail.SkuList {
			totalStock += sku.Stock
		}
		product.Quantity = totalStock

		// Dimensions from first SKU
		product.Dimensions = t.extractDimensions(firstSKU)
		product.Weight = t.extractWeight(firstSKU)

		// If multiple SKUs → variations
		if len(detail.SkuList) > 1 {
			for _, sku := range detail.SkuList {
				variation := marketplace.Variation{
					ExternalID: fmt.Sprintf("%d", sku.SkuID),
					SKU:        sku.OutSkuSn,
					Price:      t.extractPrice(sku.Price),
					Quantity:   sku.Stock,
					Attributes: make(map[string]interface{}),
				}
				// Extract spec attributes (color, size, etc.)
				for _, spec := range sku.SpecList {
					specName := ""
					specValue := ""
					if n, ok := spec["specName"].(string); ok {
						specName = n
					} else if n, ok := spec["parentName"].(string); ok {
						specName = n
					}
					if v, ok := spec["specValue"].(string); ok {
						specValue = v
					} else if v, ok := spec["name"].(string); ok {
						specValue = v
					}
					if specName != "" && specValue != "" {
						variation.Attributes[strings.ToLower(specName)] = specValue
					}
				}
				if sku.ImageUrl != "" {
					variation.Images = []marketplace.ImageData{{URL: sku.ImageUrl, Position: 0, IsMain: true}}
				}
				product.Variations = append(product.Variations, variation)
			}
		}
	}

	// Fulfillment channel
	product.FulfillmentChannel = "DEFAULT"

	// Category name if available
	if detail.CatName != "" {
		product.Categories = append(product.Categories, detail.CatName)
	}

	return product
}

func (t *TemuAdapter) convertListItemToProduct(goods *temu.TemuGoods) marketplace.MarketplaceProduct {
	return marketplace.MarketplaceProduct{
		ExternalID:  fmt.Sprintf("%d", goods.GoodsID),
		Title:       goods.GoodsName,
		SKU:         goods.OutGoodsSn,
		Categories:  []string{fmt.Sprintf("%d", goods.CatID)},
		Condition:   "new",
		IsInStock:   goods.OnSale == 1,
		Images: func() []marketplace.ImageData {
			if goods.MainImageUrl != "" {
				return []marketplace.ImageData{{URL: goods.MainImageUrl, Position: 0, IsMain: true}}
			}
			return nil
		}(),
		FulfillmentChannel: "DEFAULT",
	}
}

func (t *TemuAdapter) extractImages(detail *temu.TemuGoodsDetail) []marketplace.ImageData {
	var images []marketplace.ImageData
	pos := 0

	// Main image first
	if detail.MainImageUrl != "" {
		images = append(images, marketplace.ImageData{URL: detail.MainImageUrl, Position: pos, IsMain: true})
		pos++
	}

	// Carousel images
	seen := map[string]bool{}
	if detail.MainImageUrl != "" {
		seen[detail.MainImageUrl] = true
	}
	for _, url := range detail.CarouselImage {
		if url != "" && !seen[url] {
			images = append(images, marketplace.ImageData{URL: url, Position: pos})
			seen[url] = true
			pos++
		}
	}

	// Detail images (additional)
	for _, url := range detail.DetailImage {
		if url != "" && !seen[url] {
			images = append(images, marketplace.ImageData{URL: url, Position: pos})
			seen[url] = true
			pos++
		}
	}

	return images
}

func (t *TemuAdapter) extractPrice(priceMap map[string]interface{}) float64 {
	// Try basePrice.amount, then price, then salePrice
	for _, key := range []string{"basePrice", "salePrice", "price"} {
		if sub, ok := priceMap[key].(map[string]interface{}); ok {
			if amt, ok := sub["amount"].(float64); ok {
				return amt / 100 // Temu often stores in cents
			}
			if amt, ok := sub["amount"].(string); ok {
				var f float64
				fmt.Sscanf(amt, "%f", &f)
				return f
			}
		}
		if amt, ok := priceMap[key].(float64); ok {
			return amt / 100
		}
	}
	return 0
}

func (t *TemuAdapter) extractCurrency(priceMap map[string]interface{}) string {
	for _, key := range []string{"basePrice", "salePrice", "price"} {
		if sub, ok := priceMap[key].(map[string]interface{}); ok {
			if cur, ok := sub["currency"].(string); ok {
				return cur
			}
		}
	}
	return "GBP"
}

func (t *TemuAdapter) extractDimensions(sku temu.TemuSKU) *marketplace.Dimensions {
	l := toFloat(sku.Length)
	w := toFloat(sku.Width)
	h := toFloat(sku.Height)
	if l == 0 && w == 0 && h == 0 {
		return nil
	}
	return &marketplace.Dimensions{Length: l, Width: w, Height: h, Unit: "cm"}
}

func (t *TemuAdapter) extractWeight(sku temu.TemuSKU) *marketplace.Weight {
	w := toFloat(sku.Weight)
	if w == 0 {
		return nil
	}
	return &marketplace.Weight{Value: w, Unit: "g"}
}

func toFloat(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case int:
		return float64(val)
	case string:
		var f float64
		fmt.Sscanf(val, "%f", &f)
		return f
	}
	return 0
}

// ============================================================================
// LISTING MANAGEMENT (PIM → Temu)
// ============================================================================
// Note: Actual listing creation goes through the TemuHandler's /temu/submit
// endpoint, which has the full field mapping logic. These adapter methods
// provide the generic interface.

func (t *TemuAdapter) CreateListing(ctx context.Context, listing marketplace.ListingData) (*marketplace.ListingResult, error) {
	return nil, fmt.Errorf("use POST /api/v1/temu/submit for Temu listing creation (requires category mapping)")
}

func (t *TemuAdapter) UpdateListing(ctx context.Context, externalID string, updates marketplace.ListingData) error {
	return fmt.Errorf("update listing not yet implemented for Temu")
}

func (t *TemuAdapter) DeleteListing(ctx context.Context, externalID string) error {
	return fmt.Errorf("delete listing not yet implemented for Temu")
}

func (t *TemuAdapter) PublishListing(ctx context.Context, externalID string) error {
	var goodsID int64
	fmt.Sscanf(externalID, "%d", &goodsID)
	return t.client.SetSaleStatus(goodsID, true)
}

func (t *TemuAdapter) UnpublishListing(ctx context.Context, externalID string) error {
	var goodsID int64
	fmt.Sscanf(externalID, "%d", &goodsID)
	return t.client.SetSaleStatus(goodsID, false)
}

// ============================================================================
// BULK OPERATIONS
// ============================================================================

func (t *TemuAdapter) BulkCreateListings(ctx context.Context, listings []marketplace.ListingData) ([]marketplace.ListingResult, error) {
	return nil, fmt.Errorf("use POST /api/v1/temu/submit for Temu listing creation")
}

func (t *TemuAdapter) BulkUpdateListings(ctx context.Context, updates []marketplace.ListingUpdate) ([]marketplace.UpdateResult, error) {
	return nil, fmt.Errorf("bulk update not yet implemented for Temu")
}

// ============================================================================
// SYNC & MONITORING
// ============================================================================

func (t *TemuAdapter) GetListingStatus(ctx context.Context, externalID string) (*marketplace.ListingStatus, error) {
	var goodsID int64
	fmt.Sscanf(externalID, "%d", &goodsID)

	status, err := t.client.GetPublishStatus(goodsID)
	if err != nil {
		return nil, err
	}

	temuStatus := "unknown"
	isActive := false
	if s, ok := status["status"].(float64); ok {
		switch int(s) {
		case 0:
			temuStatus = "editing"
		case 1:
			temuStatus = "reviewing"
		case 2:
			temuStatus = "on_sale"
			isActive = true
		case 3:
			temuStatus = "off_sale"
		case 4:
			temuStatus = "rejected"
		}
	}

	return &marketplace.ListingStatus{
		ExternalID:  externalID,
		Status:      temuStatus,
		IsActive:    isActive,
		LastUpdated: time.Now(),
	}, nil
}

func (t *TemuAdapter) SyncInventory(ctx context.Context, externalID string, quantity int) error {
	return fmt.Errorf("inventory sync not yet implemented for Temu")
}

func (t *TemuAdapter) SyncPrice(ctx context.Context, externalID string, price float64) error {
	return fmt.Errorf("price sync not yet implemented for Temu")
}

// ============================================================================
// METADATA
// ============================================================================

func (t *TemuAdapter) GetName() string {
	return "temu"
}

func (t *TemuAdapter) GetDisplayName() string {
	return "Temu"
}

func (t *TemuAdapter) GetSupportedFeatures() []string {
	return []string{
		"import",
		"listing_creation",
		"publish_unpublish",
		"listing_status",
	}
}

func (t *TemuAdapter) GetRequiredFields() []marketplace.RequiredField {
	return []marketplace.RequiredField{
		{Name: "title", Type: "string", Description: "Product title (goodsName)"},
		{Name: "description", Type: "string", Description: "Product description"},
		{Name: "price", Type: "number", Description: "Product price"},
		{Name: "images", Type: "array", Description: "Product images (minimum 1)"},
		{Name: "category_id", Type: "string", Description: "Temu category ID (catId)"},
		{Name: "sku", Type: "string", Description: "Seller SKU (outGoodsSn)"},
		{Name: "shipping_template", Type: "string", Description: "Freight template ID"},
	}
}

func (t *TemuAdapter) GetCategories(ctx context.Context) ([]marketplace.Category, error) {
	cats, err := t.client.GetCategories(nil)
	if err != nil {
		return nil, err
	}

	var result []marketplace.Category
	for _, cat := range cats {
		result = append(result, marketplace.Category{
			ID:       fmt.Sprintf("%d", cat.CatID),
			Name:     cat.CatName,
			ParentID: fmt.Sprintf("%d", cat.ParentID),
		})
	}

	return result, nil
}

func (t *TemuAdapter) ValidateListing(ctx context.Context, listing marketplace.ListingData) (*marketplace.ValidationResult, error) {
	result := &marketplace.ValidationResult{
		IsValid:  true,
		Errors:   []marketplace.ValidationError{},
		Warnings: []marketplace.ValidationError{},
	}

	if listing.Title == "" {
		result.IsValid = false
		result.Errors = append(result.Errors, marketplace.ValidationError{
			Code: "MISSING_TITLE", Field: "title", Message: "Title is required", Severity: "error",
		})
	}

	if len(listing.Images) == 0 {
		result.IsValid = false
		result.Errors = append(result.Errors, marketplace.ValidationError{
			Code: "MISSING_IMAGES", Field: "images", Message: "At least one image is required", Severity: "error",
		})
	}

	if listing.CategoryID == "" {
		result.IsValid = false
		result.Errors = append(result.Errors, marketplace.ValidationError{
			Code: "MISSING_CATEGORY", Field: "category_id", Message: "Category is required", Severity: "error",
		})
	}

	if listing.ShippingProfile == "" {
		result.Warnings = append(result.Warnings, marketplace.ValidationError{
			Code: "MISSING_SHIPPING", Field: "shipping_profile", Message: "Shipping template recommended", Severity: "warning",
		})
	}

	return result, nil
}

func (t *TemuAdapter) CancelOrder(ctx context.Context, externalOrderID string) error {
	return marketplace.ErrCancelNotSupported
}
