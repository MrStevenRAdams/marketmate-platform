package adapters

import (
	"context"
	"fmt"
	"log"
	"module-a/marketplace"
	"module-a/marketplace/clients/amazon"
	"strings"
	"time"
)

// ============================================================================
// AMAZON MARKETPLACE ADAPTER
// ============================================================================
// Production-ready adapter implementing the MarketplaceAdapter interface.
//
// Import strategy:
//   - Full import: Reports API (GET_MERCHANT_LISTINGS_ALL_DATA) for the
//     seller's complete inventory, then Catalog Items API to enrich each
//     product with images, identifiers, attributes, and sales ranks.
//   - Selective import: Catalog Items API directly for specified ASINs.
// ============================================================================

type AmazonAdapter struct {
	client      *amazon.SPAPIClient
	config      *amazon.SPAPIConfig
	credentials marketplace.Credentials
}

func NewAmazonAdapter(ctx context.Context, credentials marketplace.Credentials) (marketplace.MarketplaceAdapter, error) {
	config := &amazon.SPAPIConfig{
		LWAClientID:        credentials.Data["lwa_client_id"],
		LWAClientSecret:    credentials.Data["lwa_client_secret"],
		RefreshToken:       credentials.Data["refresh_token"],
		AWSAccessKeyID:     credentials.Data["aws_access_key_id"],
		AWSSecretAccessKey: credentials.Data["aws_secret_access_key"],
		MarketplaceID:      credentials.Data["marketplace_id"],
		Region:             credentials.Data["region"],
		SellerID:           credentials.Data["seller_id"],
		IsSandbox:          credentials.Environment == "sandbox",
	}

	client, err := amazon.NewSPAPIClient(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create SP-API client: %w", err)
	}

	return &AmazonAdapter{
		client:      client,
		config:      config,
		credentials: credentials,
	}, nil
}

// ============================================================================
// CONNECTION & AUTHENTICATION
// ============================================================================

func (a *AmazonAdapter) Connect(ctx context.Context, credentials marketplace.Credentials) error {
	return nil
}

func (a *AmazonAdapter) Disconnect(ctx context.Context) error {
	return nil
}

func (a *AmazonAdapter) TestConnection(ctx context.Context) error {
	_, err := a.client.SearchCatalogItems(ctx, "test", 1, "")
	if err != nil {
		return fmt.Errorf("catalog API test failed: %w", err)
	}
	if err := a.client.TestShippingAccess(ctx); err != nil {
		return fmt.Errorf("orders API test failed: %w", err)
	}
	return nil
}

func (a *AmazonAdapter) RefreshAuth(ctx context.Context) error {
	return nil
}

func (a *AmazonAdapter) GetConnectionStatus(ctx context.Context) (*marketplace.ConnectionStatus, error) {
	status := &marketplace.ConnectionStatus{
		IsConnected:    true,
		LastChecked:    time.Now(),
		LastSuccessful: time.Now(),
	}
	if err := a.TestConnection(ctx); err != nil {
		status.IsConnected = false
		status.ErrorMessage = err.Error()
	}
	return status, nil
}

// ============================================================================
// PRODUCT IMPORT
// ============================================================================

// FetchListings retrieves products from the seller's Amazon account.
//
// For selective imports (specific ASINs), it calls the Catalog Items API directly.
// For full imports, it uses the Reports API to get the seller's complete inventory,
// then enriches each product with full catalog data via ProductCallback so that
// progress tracking reflects truly completed (fully enriched) records.
func (a *AmazonAdapter) FetchListings(ctx context.Context, filters marketplace.ImportFilters) ([]marketplace.MarketplaceProduct, error) {
	if len(filters.ExternalIDs) > 0 {
		return a.fetchSelectiveProducts(ctx, filters.ExternalIDs)
	}
	return a.fetchFullCatalogViaReport(ctx, filters)
}

// fetchSelectiveProducts fetches specific ASINs with full catalog enrichment.
func (a *AmazonAdapter) fetchSelectiveProducts(ctx context.Context, asins []string) ([]marketplace.MarketplaceProduct, error) {
	var products []marketplace.MarketplaceProduct

	for _, asin := range asins {
		product, err := a.FetchProduct(ctx, asin)
		if err != nil {
			log.Printf("[Amazon] Warning: failed to fetch ASIN %s: %v", asin, err)
			continue
		}
		products = append(products, *product)
	}

	if len(products) == 0 && len(asins) > 0 {
		return nil, fmt.Errorf("failed to fetch any of the %d requested ASINs", len(asins))
	}
	return products, nil
}

// fetchFullCatalogViaReport uses the Reports API to get the seller's full inventory.
//
// The report provides all seller-specific data (SKU, ASIN, price, quantity, fulfillment
// channel, condition, status, image). Each product is streamed immediately via
// ProductCallback as soon as it is converted — no inline catalog enrichment — so that:
//
//  1. processed_items increments as each product is saved to Firestore, giving an
//     accurate X/Total counter on the import dashboard.
//  2. The import completes in seconds rather than hours (1 req per ASIN × 2 req/sec
//     would take ~14 minutes for 1,650 products and much longer for larger catalogs).
//  3. Catalog enrichment (full images, identifiers, attributes, sales ranks) is handled
//     separately by the enrichment queue after the import completes.
//
// The total is signalled via __total__:N immediately after the report downloads,
// before any products are streamed, so the dashboard shows 0/N from the start.
func (a *AmazonAdapter) fetchFullCatalogViaReport(ctx context.Context, filters marketplace.ImportFilters) ([]marketplace.MarketplaceProduct, error) {
	log.Printf("[Amazon] Starting full catalog import via Reports API")

	rows, err := a.client.RequestAndDownloadReport(ctx, amazon.ReportMerchantListingsAll)
	if err != nil {
		return nil, fmt.Errorf("report download failed: %w", err)
	}

	log.Printf("[Amazon] Report downloaded: %d listings. Streaming to import service...", len(rows))

	// Signal the true total immediately so the dashboard shows 0/N rather than 0/0.
	if filters.ProgressCallback != nil {
		filters.ProgressCallback(fmt.Sprintf("__total__:%d", len(rows)))
		filters.ProgressCallback(fmt.Sprintf("Importing %d products from Amazon report...", len(rows)))
	}

	// If no ProductCallback is registered, return the full slice (backwards-compat).
	if filters.ProductCallback == nil {
		var products []marketplace.MarketplaceProduct
		for _, row := range rows {
			products = append(products, a.convertReportRowToProduct(row))
		}
		log.Printf("[Amazon] Full catalog import complete (non-streaming): %d products", len(products))
		return products, nil
	}

	// Streaming mode: for each product in the report:
	//   • Already mapped (re-import): stream the basic report data immediately — no
	//     catalog API call needed. ProcessedItems ticks instantly, keeping the counter
	//     accurate for products we already have full data for.
	//   • New product: call GetCatalogItem to fetch full catalog data (images,
	//     identifiers, attributes, sales ranks), then stream. ProcessedItems only
	//     increments after enrichment completes, so the X/Total counter reflects
	//     genuinely enriched records.
	//
	// Circuit breaker: if the first 3 enrichment attempts all fail, we assume
	// the catalog API is unavailable or the credentials lack catalog scope, and
	// fall back to streaming all remaining products without enrichment. This
	// prevents burning through 1650 failing API calls at full speed.
	catalogAvailable := true
	consecutiveEnrichFailures := 0
	const enrichCircuitBreakerThreshold = 3

	for i, row := range rows {
		product := a.convertReportRowToProduct(row)

		// Determine if this ASIN was already imported and mapped
		alreadyMapped := filters.AlreadyMappedIDs != nil && filters.AlreadyMappedIDs[product.ExternalID]

		if !alreadyMapped && product.ExternalID != "" {
			if !catalogAvailable {
				// Circuit breaker is open — mark this product as failed so saveProduct
				// does NOT save a hollow shell to Firestore.
				product.RawData["_enrich_failed"] = true
				product.RawData["_enrich_error"] = "catalog API unavailable (circuit breaker open)"
				product.RawData["_request_url"] = fmt.Sprintf("/catalog/2022-04-01/items/%s", product.ExternalID)
			} else {
				// New product — enrich with Catalog Items API before saving
				if enriched, enrichErr := a.enrichWithCatalogData(ctx, &product); enrichErr != nil {
					consecutiveEnrichFailures++
					log.Printf("[Amazon] Catalog enrichment failed for ASIN %s (consecutive failures: %d): %v",
						product.ExternalID, consecutiveEnrichFailures, enrichErr)

					// Embed diagnostic fields into RawData so the import service can
					// record a structured ImportError with the full SP-API response.
					product.RawData["_enrich_failed"] = true
					product.RawData["_enrich_error"] = enrichErr.Error()
					product.RawData["_request_url"] = fmt.Sprintf("/catalog/2022-04-01/items/%s", product.ExternalID)
					if apiErr, ok := enrichErr.(*amazon.APIError); ok {
						product.RawData["_status_code"] = apiErr.StatusCode
						product.RawData["_response_body"] = apiErr.Body
					}

					if consecutiveEnrichFailures >= enrichCircuitBreakerThreshold {
						log.Printf("[Amazon] Circuit breaker tripped after %d consecutive enrichment failures — "+
							"catalog API unavailable or credentials lack catalog scope. "+
							"Remaining products will be held as failed, not saved.",
							enrichCircuitBreakerThreshold)
						catalogAvailable = false
					}
				} else {
					// Successful enrichment — reset the consecutive failure counter
					consecutiveEnrichFailures = 0
					product = *enriched
				}
			}
		}

		if !filters.ProductCallback(product) {
			log.Printf("[Amazon] Import cancelled by callback at product %d/%d", i+1, len(rows))
			return nil, nil
		}
	}

	if !catalogAvailable {
		log.Printf("[Amazon] Full catalog import complete (streaming, enrichment unavailable): %d products", len(rows))
	} else {
		log.Printf("[Amazon] Full catalog import complete (streaming, with enrichment): %d products", len(rows))
	}
	// Return nil — all products were handled via ProductCallback.
	return nil, nil
}

// convertReportRowToProduct creates a MarketplaceProduct from a report row.
// This gives us seller-specific data: SKU, price, quantity, fulfillment channel, status.
func (a *AmazonAdapter) convertReportRowToProduct(row amazon.ReportRow) marketplace.MarketplaceProduct {
	asin := row.ASIN1
	if asin == "" {
		asin = row.ProductID
	}

	// Determine fulfillment channel
	fulfillment := "MFN" // Default = merchant fulfilled
	if strings.Contains(strings.ToUpper(row.FulfillmentChannel), "AMAZON") {
		fulfillment = "AFN" // FBA
	} else if strings.EqualFold(row.FulfillmentChannel, "DEFAULT") {
		fulfillment = "MFN"
	}

	isInStock := row.Quantity > 0

	product := marketplace.MarketplaceProduct{
		ExternalID:         asin,
		SKU:                row.SellerSKU,
		Title:              row.ItemName,
		Description:        row.ItemDescription,
		Price:              row.Price,
		Quantity:           row.Quantity,
		Condition:          row.ItemCondition,
		FulfillmentChannel: fulfillment,
		IsInStock:          isInStock,
		Images:             []marketplace.ImageData{},
		Attributes:         make(map[string]interface{}),
		Categories:         []string{},
		Identifiers: marketplace.Identifiers{
			ASIN: asin,
		},
		RawData: map[string]interface{}{
			"seller_sku":              row.SellerSKU,
			"listing_id":             row.ListingID,
			"fulfillment_channel":    row.FulfillmentChannel,
			"status":                 row.Status,
			"open_date":              row.OpenDate,
			"item_condition":         row.ItemCondition,
			"product_id_type":        row.ProductIDType,
			"product_id":             row.ProductID,
			"pending_quantity":       row.PendingQuantity,
			"merchant_shipping_group": row.MerchantShippingGroup,
			"browse_path":            row.ZShopBrowsePath,
			"business_price":         row.BusinessPrice,
		},
	}

	// Set product identifier from report
	switch strings.ToUpper(row.ProductIDType) {
	case "ASIN":
		// Already set above
	case "UPC":
		product.Identifiers.UPC = row.ProductID
	case "EAN":
		product.Identifiers.EAN = row.ProductID
	case "ISBN":
		product.Identifiers.ISBN = row.ProductID
	}

	// Add primary image from report if available
	if row.ImageURL != "" {
		product.Images = append(product.Images, marketplace.ImageData{
			URL:    row.ImageURL,
			IsMain: true,
		})
	}

	// Browse path as category
	if row.ZShopBrowsePath != "" {
		product.Categories = append(product.Categories, row.ZShopBrowsePath)
	}

	return product
}

// enrichWithCatalogData fetches full catalog details and merges them into the product.
// The report provides seller-specific data; the catalog provides Amazon-wide data.
func (a *AmazonAdapter) enrichWithCatalogData(ctx context.Context, product *marketplace.MarketplaceProduct) (*marketplace.MarketplaceProduct, error) {
	catalogItem, err := a.client.GetCatalogItem(ctx, product.ExternalID)
	if err != nil {
		return nil, err
	}

	// Merge catalog data — catalog enriches, report data takes priority for seller-specific fields

	// Images: replace report's single image with full catalog image set
	if len(catalogItem.Images) > 0 {
		product.Images = []marketplace.ImageData{}
		position := 0
		for _, catalogImage := range catalogItem.Images {
			for _, img := range catalogImage.Images {
				product.Images = append(product.Images, marketplace.ImageData{
					URL:      img.Link,
					Position: position,
					Width:    img.Width,
					Height:   img.Height,
					IsMain:   catalogImage.Variant == "MAIN",
				})
				position++
			}
		}
	}

	// Identifiers: merge from catalog (more complete than report)
	for _, identifier := range catalogItem.Identifiers {
		for _, idValue := range identifier.Identifiers {
			switch idValue.IdentifierType {
			case "UPC":
				product.Identifiers.UPC = idValue.Identifier
			case "EAN":
				product.Identifiers.EAN = idValue.Identifier
			case "ISBN":
				product.Identifiers.ISBN = idValue.Identifier
			case "GTIN":
				product.Identifiers.GTIN = idValue.Identifier
			}
		}
	}

	// Summary: use catalog title/brand if report was empty
	if len(catalogItem.Summaries) > 0 {
		summary := catalogItem.Summaries[0]
		if product.Title == "" {
			product.Title = summary.ItemName
		}
		if product.Brand == "" {
			product.Brand = summary.Brand
		}
		// Always capture enriched summary fields
		if summary.Manufacturer != "" {
			product.RawData["manufacturer"] = summary.Manufacturer
		}
		if summary.ModelNumber != "" {
			product.RawData["model_number"] = summary.ModelNumber
		}
		if summary.PartNumber != "" {
			product.RawData["part_number"] = summary.PartNumber
		}
		if summary.Color != "" {
			product.RawData["color"] = summary.Color
		}
		if summary.Size != "" {
			product.RawData["size"] = summary.Size
		}
		if summary.Style != "" {
			product.RawData["style"] = summary.Style
		}
		if summary.ItemClassification != "" {
			product.RawData["item_classification"] = summary.ItemClassification
		}
		if summary.PackageQuantity > 0 {
			product.RawData["package_quantity"] = summary.PackageQuantity
		}
		if summary.BrowseClassification != nil {
			product.RawData["browse_classification"] = summary.BrowseClassification.DisplayName
			product.RawData["browse_classification_id"] = summary.BrowseClassification.ClassificationID
			product.Categories = append(product.Categories, summary.BrowseClassification.DisplayName)
		}
	}

	// Product types
	if len(catalogItem.ProductTypes) > 0 {
		product.RawData["product_type"] = catalogItem.ProductTypes[0].ProductType
	}

	// Sales ranks
	for _, sr := range catalogItem.SalesRanks {
		for _, cr := range sr.ClassificationRanks {
			product.RawData["sales_rank_"+cr.ClassificationID] = cr.Rank
			product.RawData["sales_rank_category_"+cr.ClassificationID] = cr.Title
		}
		for _, dr := range sr.DisplayGroupRanks {
			product.RawData["display_group_rank_"+dr.WebsiteDisplayGroup] = dr.Rank
		}
	}

	// Variations
	for _, v := range catalogItem.Variations {
		if v.Type == "PARENT" && len(v.ASINs) > 0 {
			for _, childASIN := range v.ASINs {
				product.Variations = append(product.Variations, marketplace.Variation{
					ExternalID: childASIN,
					Attributes: map[string]interface{}{"parent_asin": product.ExternalID},
				})
			}
		}
		product.RawData["variation_type"] = v.Type
	}

	// Vendor details
	for _, vd := range catalogItem.VendorDetails {
		if vd.ProductGroup != "" {
			product.RawData["product_group"] = vd.ProductGroup
		}
		if vd.BrandCode != "" {
			product.RawData["brand_code"] = vd.BrandCode
		}
		if vd.CategoryCode != "" {
			product.RawData["category_code"] = vd.CategoryCode
		}
	}

	// Full SP-API attributes (bullet points, descriptions, item specifics, etc.)
	if catalogItem.Attributes != nil {
		for k, v := range catalogItem.Attributes {
			product.Attributes[k] = v
		}
	}

	return product, nil
}

// FetchProduct retrieves a single product by ASIN with full catalog data.
func (a *AmazonAdapter) FetchProduct(ctx context.Context, asin string) (*marketplace.MarketplaceProduct, error) {
	catalogItem, err := a.client.GetCatalogItem(ctx, asin)
	if err != nil {
		return nil, err
	}
	product := a.convertCatalogItemToProduct(catalogItem)
	return product, nil
}

func (a *AmazonAdapter) FetchProductImages(ctx context.Context, asin string) ([]marketplace.ImageData, error) {
	catalogItem, err := a.client.GetCatalogItem(ctx, asin)
	if err != nil {
		return nil, err
	}

	var images []marketplace.ImageData
	position := 0
	for _, catalogImage := range catalogItem.Images {
		for _, img := range catalogImage.Images {
			images = append(images, marketplace.ImageData{
				URL:      img.Link,
				Position: position,
				Width:    img.Width,
				Height:   img.Height,
				IsMain:   catalogImage.Variant == "MAIN",
			})
			position++
		}
	}
	return images, nil
}

func (a *AmazonAdapter) FetchInventory(ctx context.Context, asin string) (*marketplace.InventoryLevel, error) {
	summaries, err := a.client.GetInventorySummaries(ctx)
	if err != nil {
		return nil, err
	}

	for _, summary := range summaries {
		if summary.ASIN == asin {
			return &marketplace.InventoryLevel{
				ExternalID: asin,
				Quantity:   summary.FulfillableQuantity,
				UpdatedAt:  time.Now(),
			}, nil
		}
	}

	return &marketplace.InventoryLevel{
		ExternalID: asin,
		Quantity:   0,
		UpdatedAt:  time.Now(),
	}, nil
}

// ============================================================================
// LISTING MANAGEMENT
// ============================================================================

func (a *AmazonAdapter) CreateListing(ctx context.Context, listing marketplace.ListingData) (*marketplace.ListingResult, error) {
	attributes := a.convertToAmazonAttributes(listing)
	productType := "PRODUCT"
	if pt, ok := listing.CustomFields["product_type"].(string); ok {
		productType = pt
	}

	sku := listing.Identifiers.ASIN
	if sku == "" && listing.CustomFields["sku"] != nil {
		sku = listing.CustomFields["sku"].(string)
	}

	err := a.client.CreateListing(ctx, sku, productType, attributes)
	if err != nil {
		return &marketplace.ListingResult{
			Status: "error",
			Errors: []marketplace.ValidationError{
				{Code: "CREATE_FAILED", Message: err.Error(), Severity: "error"},
			},
		}, err
	}

	return &marketplace.ListingResult{
		ExternalID: sku,
		SKU:        sku,
		Status:     "pending",
		CreatedAt:  time.Now(),
	}, nil
}

func (a *AmazonAdapter) UpdateListing(ctx context.Context, externalID string, updates marketplace.ListingData) error {
	attributes := a.convertToAmazonAttributes(updates)
	productType := "PRODUCT"
	if pt, ok := updates.CustomFields["product_type"].(string); ok {
		productType = pt
	}
	return a.client.CreateListing(ctx, externalID, productType, attributes)
}

func (a *AmazonAdapter) DeleteListing(ctx context.Context, externalID string) error {
	return fmt.Errorf("Amazon does not support listing deletion via API — use UnpublishListing instead")
}

func (a *AmazonAdapter) PublishListing(ctx context.Context, externalID string) error {
	return nil // Listings are published automatically on creation
}

func (a *AmazonAdapter) UnpublishListing(ctx context.Context, externalID string) error {
	return a.client.CreateListing(ctx, externalID, "PRODUCT", map[string]interface{}{
		"quantity": []map[string]interface{}{{"value": 0}},
	})
}

// ============================================================================
// BULK OPERATIONS
// ============================================================================

func (a *AmazonAdapter) BulkCreateListings(ctx context.Context, listings []marketplace.ListingData) ([]marketplace.ListingResult, error) {
	var results []marketplace.ListingResult
	for _, listing := range listings {
		result, err := a.CreateListing(ctx, listing)
		if err != nil {
			results = append(results, marketplace.ListingResult{Status: "error", Errors: []marketplace.ValidationError{{Code: "CREATE_FAILED", Message: err.Error(), Severity: "error"}}})
			continue
		}
		results = append(results, *result)
	}
	return results, nil
}

func (a *AmazonAdapter) BulkUpdateListings(ctx context.Context, updates []marketplace.ListingUpdate) ([]marketplace.UpdateResult, error) {
	var results []marketplace.UpdateResult
	for _, update := range updates {
		err := a.client.CreateListing(ctx, update.ExternalID, "PRODUCT", update.Updates)
		results = append(results, marketplace.UpdateResult{
			ExternalID: update.ExternalID,
			Success:    err == nil,
			UpdatedAt:  time.Now(),
		})
	}
	return results, nil
}

// ============================================================================
// SYNC & MONITORING
// ============================================================================

func (a *AmazonAdapter) GetListingStatus(ctx context.Context, sku string) (*marketplace.ListingStatus, error) {
	item, err := a.client.GetListingsItem(ctx, sku)
	if err != nil {
		return nil, err
	}

	isActive := false
	for _, s := range item.Status {
		if strings.EqualFold(s, "BUYABLE") {
			isActive = true
			break
		}
	}

	return &marketplace.ListingStatus{
		ExternalID:  sku,
		Status:      strings.Join(item.Status, ","),
		IsActive:    isActive,
		LastUpdated: time.Now(),
	}, nil
}

func (a *AmazonAdapter) SyncInventory(ctx context.Context, externalID string, quantity int) error {
	return a.client.CreateListing(ctx, externalID, "PRODUCT", map[string]interface{}{
		"quantity": []map[string]interface{}{{"value": quantity}},
	})
}

func (a *AmazonAdapter) SyncPrice(ctx context.Context, externalID string, price float64) error {
	return a.client.CreateListing(ctx, externalID, "PRODUCT", map[string]interface{}{
		"list_price": []map[string]interface{}{{"value": price, "currency": "GBP"}},
	})
}

// ============================================================================
// METADATA
// ============================================================================

func (a *AmazonAdapter) GetName() string { return "amazon" }

func (a *AmazonAdapter) GetDisplayName() string {
	region := strings.ToUpper(a.config.Region)
	switch region {
	case "EU-WEST-1":
		return "Amazon UK/EU"
	case "US-EAST-1":
		return "Amazon US"
	case "US-WEST-2":
		return "Amazon JP/AU"
	default:
		return "Amazon"
	}
}

func (a *AmazonAdapter) GetSupportedFeatures() []string {
	return []string{
		"import", "listing", "fba", "variations",
		"inventory_sync", "price_sync", "bulk_operations",
		"reports_import", "catalog_enrichment",
	}
}

func (a *AmazonAdapter) GetRequiredFields() []marketplace.RequiredField {
	return []marketplace.RequiredField{
		{Name: "product_id", Type: "string", Description: "UPC, EAN, ASIN, or ISBN"},
		{Name: "product_id_type", Type: "string", Description: "Type of product identifier"},
		{Name: "title", Type: "string", Description: "Product title (max 200 chars)"},
		{Name: "brand", Type: "string", Description: "Brand name"},
		{Name: "description", Type: "string", Description: "Product description"},
		{Name: "bullet_points", Type: "array", Description: "Up to 5 key features"},
		{Name: "price", Type: "number", Description: "Listing price"},
	}
}

func (a *AmazonAdapter) GetCategories(ctx context.Context) ([]marketplace.Category, error) {
	return []marketplace.Category{}, nil
}

func (a *AmazonAdapter) ValidateListing(ctx context.Context, listing marketplace.ListingData) (*marketplace.ValidationResult, error) {
	result := &marketplace.ValidationResult{IsValid: true}

	if listing.Title == "" {
		result.IsValid = false
		result.Errors = append(result.Errors, marketplace.ValidationError{
			Code: "MISSING_TITLE", Field: "title", Message: "Title is required", Severity: "error",
		})
	}
	if listing.Price <= 0 {
		result.IsValid = false
		result.Errors = append(result.Errors, marketplace.ValidationError{
			Code: "INVALID_PRICE", Field: "price", Message: "Price must be greater than 0", Severity: "error",
		})
	}
	if len(listing.Title) > 200 {
		result.Warnings = append(result.Warnings, marketplace.ValidationError{
			Code: "TITLE_TOO_LONG", Field: "title", Message: "Title exceeds 200 characters", Severity: "warning",
		})
	}
	return result, nil
}

// ============================================================================
// HELPERS
// ============================================================================

// convertCatalogItemToProduct converts a CatalogItem (no report data) into a MarketplaceProduct.
// Used for selective imports where we don't have report data.
func (a *AmazonAdapter) convertCatalogItemToProduct(item *amazon.CatalogItem) *marketplace.MarketplaceProduct {
	product := &marketplace.MarketplaceProduct{
		ExternalID:  item.ASIN,
		Images:      []marketplace.ImageData{},
		Attributes:  make(map[string]interface{}),
		Categories:  []string{},
		Identifiers: marketplace.Identifiers{ASIN: item.ASIN},
		RawData:     make(map[string]interface{}),
	}

	// Identifiers
	for _, identifier := range item.Identifiers {
		for _, idValue := range identifier.Identifiers {
			switch idValue.IdentifierType {
			case "UPC":
				product.Identifiers.UPC = idValue.Identifier
			case "EAN":
				product.Identifiers.EAN = idValue.Identifier
			case "ISBN":
				product.Identifiers.ISBN = idValue.Identifier
			case "GTIN":
				product.Identifiers.GTIN = idValue.Identifier
			}
		}
	}

	// Images
	position := 0
	for _, catalogImage := range item.Images {
		for _, img := range catalogImage.Images {
			product.Images = append(product.Images, marketplace.ImageData{
				URL: img.Link, Position: position, Width: img.Width, Height: img.Height,
				IsMain: catalogImage.Variant == "MAIN",
			})
			position++
		}
	}

	// Summary
	if len(item.Summaries) > 0 {
		s := item.Summaries[0]
		product.Title = s.ItemName
		product.Brand = s.Brand
		if s.Manufacturer != "" {
			product.RawData["manufacturer"] = s.Manufacturer
		}
		if s.ModelNumber != "" {
			product.RawData["model_number"] = s.ModelNumber
		}
		if s.PartNumber != "" {
			product.RawData["part_number"] = s.PartNumber
		}
		if s.Color != "" {
			product.RawData["color"] = s.Color
		}
		if s.Size != "" {
			product.RawData["size"] = s.Size
		}
		if s.Style != "" {
			product.RawData["style"] = s.Style
		}
		if s.ItemClassification != "" {
			product.RawData["item_classification"] = s.ItemClassification
		}
		if s.PackageQuantity > 0 {
			product.RawData["package_quantity"] = s.PackageQuantity
		}
		if s.BrowseClassification != nil {
			product.RawData["browse_classification"] = s.BrowseClassification.DisplayName
			product.Categories = append(product.Categories, s.BrowseClassification.DisplayName)
		}
	}

	// Product types
	if len(item.ProductTypes) > 0 {
		product.RawData["product_type"] = item.ProductTypes[0].ProductType
	}

	// Sales ranks
	for _, sr := range item.SalesRanks {
		for _, cr := range sr.ClassificationRanks {
			product.RawData["sales_rank_"+cr.ClassificationID] = cr.Rank
		}
		for _, dr := range sr.DisplayGroupRanks {
			product.RawData["display_group_rank_"+dr.WebsiteDisplayGroup] = dr.Rank
		}
	}

	// Variations
	for _, v := range item.Variations {
		if v.Type == "PARENT" {
			for _, childASIN := range v.ASINs {
				product.Variations = append(product.Variations, marketplace.Variation{
					ExternalID: childASIN,
					Attributes: map[string]interface{}{"parent_asin": item.ASIN},
				})
			}
		}
		product.RawData["variation_type"] = v.Type
	}

	// Vendor details
	for _, vd := range item.VendorDetails {
		if vd.ProductGroup != "" {
			product.RawData["product_group"] = vd.ProductGroup
		}
	}

	// Full attributes
	if item.Attributes != nil {
		for k, v := range item.Attributes {
			product.Attributes[k] = v
		}
	}

	return product
}

func (a *AmazonAdapter) convertToAmazonAttributes(listing marketplace.ListingData) map[string]interface{} {
	attributes := make(map[string]interface{})

	attributes["item_name"] = listing.Title
	if listing.CustomFields != nil {
		if brand, ok := listing.CustomFields["brand"].(string); ok {
			attributes["brand"] = []map[string]string{{"value": brand}}
		}
	}
	if listing.Description != "" {
		attributes["product_description"] = []map[string]string{{"value": listing.Description}}
	}
	if bulletPoints, ok := listing.CustomFields["bullet_points"].([]string); ok {
		attributes["bullet_point"] = bulletPoints
	}
	attributes["list_price"] = []map[string]interface{}{{"value": listing.Price, "currency": "GBP"}}

	if listing.Identifiers.UPC != "" {
		attributes["externally_assigned_product_identifier"] = []map[string]interface{}{
			{"type": "UPC", "value": listing.Identifiers.UPC},
		}
	}

	for key, value := range listing.CustomFields {
		if _, exists := attributes[key]; !exists {
			attributes[key] = value
		}
	}
	return attributes
}

// CancelOrder — Amazon SP-API order cancellation is not exposed via standard SP-API.
func (a *AmazonAdapter) CancelOrder(ctx context.Context, externalOrderID string) error {
	return marketplace.ErrCancelNotSupported
}
