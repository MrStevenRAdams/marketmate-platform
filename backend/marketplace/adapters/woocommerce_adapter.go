package adapters

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"module-a/marketplace"
	"module-a/marketplace/clients/woocommerce"
)

// ============================================================================
// WOOCOMMERCE MARKETPLACE ADAPTER
// ============================================================================
// Implements marketplace.MarketplaceAdapter for WooCommerce stores.
// Credential fields: store_url, consumer_key, consumer_secret
// ============================================================================

type WooCommerceAdapter struct {
	credentials marketplace.Credentials
	client      *woocommerce.Client
}

func NewWooCommerceAdapter(ctx context.Context, credentials marketplace.Credentials) (marketplace.MarketplaceAdapter, error) {
	storeURL := credentials.Data["store_url"]
	consumerKey := credentials.Data["consumer_key"]
	consumerSecret := credentials.Data["consumer_secret"]

	if storeURL == "" {
		return nil, fmt.Errorf("store_url is required for WooCommerce")
	}
	if consumerKey == "" || consumerSecret == "" {
		return nil, fmt.Errorf("consumer_key and consumer_secret are required for WooCommerce")
	}

	client := woocommerce.NewClient(storeURL, consumerKey, consumerSecret)

	return &WooCommerceAdapter{
		credentials: credentials,
		client:      client,
	}, nil
}

// ── Connection ────────────────────────────────────────────────────────────────

func (a *WooCommerceAdapter) Connect(ctx context.Context, credentials marketplace.Credentials) error {
	return nil
}

func (a *WooCommerceAdapter) Disconnect(ctx context.Context) error {
	return nil
}

func (a *WooCommerceAdapter) TestConnection(ctx context.Context) error {
	return a.client.TestConnection()
}

func (a *WooCommerceAdapter) RefreshAuth(ctx context.Context) error {
	// WooCommerce uses static API keys — no token refresh needed
	return nil
}

func (a *WooCommerceAdapter) GetConnectionStatus(ctx context.Context) (*marketplace.ConnectionStatus, error) {
	err := a.TestConnection(ctx)
	return &marketplace.ConnectionStatus{
		IsConnected:  err == nil,
		ErrorMessage: func() string { if err != nil { return err.Error() }; return "" }(),
		LastChecked:  time.Now(),
	}, nil
}

// ── Product Import ────────────────────────────────────────────────────────────

func (a *WooCommerceAdapter) FetchListings(ctx context.Context, filters marketplace.ImportFilters) ([]marketplace.MarketplaceProduct, error) {
	products, err := a.client.GetAllProducts("publish")
	if err != nil {
		return nil, fmt.Errorf("WooCommerce FetchListings: %w", err)
	}

	var result []marketplace.MarketplaceProduct
	for _, p := range products {
		mp := convertWooProduct(p)
		result = append(result, mp)
		if filters.ProductCallback != nil {
			if !filters.ProductCallback(mp) {
				break
			}
		}
	}
	return result, nil
}

func (a *WooCommerceAdapter) FetchProduct(ctx context.Context, externalID string) (*marketplace.MarketplaceProduct, error) {
	id, err := strconv.Atoi(externalID)
	if err != nil {
		return nil, fmt.Errorf("invalid WooCommerce product ID %q: %w", externalID, err)
	}
	p, err := a.client.GetProduct(id)
	if err != nil {
		return nil, fmt.Errorf("WooCommerce FetchProduct %s: %w", externalID, err)
	}
	mp := convertWooProduct(*p)
	return &mp, nil
}

func (a *WooCommerceAdapter) FetchProductImages(ctx context.Context, externalID string) ([]marketplace.ImageData, error) {
	mp, err := a.FetchProduct(ctx, externalID)
	if err != nil {
		return nil, err
	}
	return mp.Images, nil
}

func (a *WooCommerceAdapter) FetchInventory(ctx context.Context, externalID string) (*marketplace.InventoryLevel, error) {
	mp, err := a.FetchProduct(ctx, externalID)
	if err != nil {
		return nil, err
	}
	return &marketplace.InventoryLevel{
		ExternalID: externalID,
		Quantity:   mp.Quantity,
	}, nil
}

// ── Listing Management ────────────────────────────────────────────────────────

func (a *WooCommerceAdapter) CreateListing(ctx context.Context, listing marketplace.ListingData) (*marketplace.ListingResult, error) {
	req := buildWooProductRequest(listing)
	created, err := a.client.CreateProduct(req)
	if err != nil {
		return nil, fmt.Errorf("WooCommerce CreateProduct: %w", err)
	}
	return &marketplace.ListingResult{
		ExternalID: strconv.Itoa(created.ID),
		Status:     created.Status,
		URL:        created.Permalink,
	}, nil
}

func (a *WooCommerceAdapter) UpdateListing(ctx context.Context, externalID string, updates marketplace.ListingData) error {
	id, err := strconv.Atoi(externalID)
	if err != nil {
		return fmt.Errorf("invalid WooCommerce product ID %q: %w", externalID, err)
	}
	req := buildWooProductRequest(updates)
	_, err = a.client.UpdateProduct(id, req)
	return err
}

func (a *WooCommerceAdapter) DeleteListing(ctx context.Context, externalID string) error {
	id, err := strconv.Atoi(externalID)
	if err != nil {
		return fmt.Errorf("invalid WooCommerce product ID %q: %w", externalID, err)
	}
	return a.client.DeleteProduct(id)
}

func (a *WooCommerceAdapter) PublishListing(ctx context.Context, externalID string) error {
	id, err := strconv.Atoi(externalID)
	if err != nil {
		return fmt.Errorf("invalid WooCommerce product ID %q: %w", externalID, err)
	}
	payload := &woocommerce.Product{Status: "publish"}
	_, err = a.client.UpdateProduct(id, payload)
	return err
}

func (a *WooCommerceAdapter) UnpublishListing(ctx context.Context, externalID string) error {
	id, err := strconv.Atoi(externalID)
	if err != nil {
		return fmt.Errorf("invalid WooCommerce product ID %q: %w", externalID, err)
	}
	payload := &woocommerce.Product{Status: "draft"}
	_, err = a.client.UpdateProduct(id, payload)
	return err
}

// ── Bulk ──────────────────────────────────────────────────────────────────────

func (a *WooCommerceAdapter) BulkCreateListings(ctx context.Context, listings []marketplace.ListingData) ([]marketplace.ListingResult, error) {
	var results []marketplace.ListingResult
	for _, l := range listings {
		r, err := a.CreateListing(ctx, l)
		if err != nil {
			results = append(results, marketplace.ListingResult{Status: "error"})
		} else {
			results = append(results, *r)
		}
	}
	return results, nil
}

func (a *WooCommerceAdapter) BulkUpdateListings(ctx context.Context, updates []marketplace.ListingUpdate) ([]marketplace.UpdateResult, error) {
	var results []marketplace.UpdateResult
	for _, u := range updates {
		listing := marketplace.ListingData{
			Title:    fmt.Sprintf("%v", u.Updates["title"]),
			Price:    toWooFloat64(u.Updates["price"]),
			Quantity: toWooInt(u.Updates["quantity"]),
		}
		err := a.UpdateListing(ctx, u.ExternalID, listing)
		r := marketplace.UpdateResult{ExternalID: u.ExternalID, Success: err == nil, UpdatedAt: time.Now()}
		if err != nil {
			r.Errors = []marketplace.ValidationError{{Field: "update", Message: err.Error()}}
		}
		results = append(results, r)
	}
	return results, nil
}

// ── Sync ──────────────────────────────────────────────────────────────────────

func (a *WooCommerceAdapter) GetListingStatus(ctx context.Context, externalID string) (*marketplace.ListingStatus, error) {
	mp, err := a.FetchProduct(ctx, externalID)
	if err != nil {
		return nil, err
	}
	status := "active"
	if mp.RawData != nil {
		if s, ok := mp.RawData["status"].(string); ok && s != "" {
			status = s
		}
	}
	return &marketplace.ListingStatus{ExternalID: externalID, Status: status}, nil
}

func (a *WooCommerceAdapter) SyncInventory(ctx context.Context, externalID string, quantity int) error {
	id, err := strconv.Atoi(externalID)
	if err != nil {
		return fmt.Errorf("invalid WooCommerce product ID %q: %w", externalID, err)
	}
	return a.client.UpdateProductStock(id, quantity)
}

func (a *WooCommerceAdapter) SyncPrice(ctx context.Context, externalID string, price float64) error {
	id, err := strconv.Atoi(externalID)
	if err != nil {
		return fmt.Errorf("invalid WooCommerce product ID %q: %w", externalID, err)
	}
	return a.client.UpdateProductPrice(id, price)
}

// ── Metadata ──────────────────────────────────────────────────────────────────

func (a *WooCommerceAdapter) GetName() string        { return "woocommerce" }
func (a *WooCommerceAdapter) GetDisplayName() string { return "WooCommerce" }

func (a *WooCommerceAdapter) GetSupportedFeatures() []string {
	return []string{"import", "listing", "order_sync", "inventory_sync", "price_sync", "tracking"}
}

func (a *WooCommerceAdapter) GetRequiredFields() []marketplace.RequiredField {
	return []marketplace.RequiredField{
		{Name: "store_url", Type: "string", Description: "Your WooCommerce store URL (e.g. https://mystore.com)"},
		{Name: "consumer_key", Type: "string", Description: "WooCommerce REST API Consumer Key"},
		{Name: "consumer_secret", Type: "password", Description: "WooCommerce REST API Consumer Secret"},
	}
}

func (a *WooCommerceAdapter) GetCategories(ctx context.Context) ([]marketplace.Category, error) {
	cats, err := a.client.GetCategories()
	if err != nil {
		return nil, err
	}
	var result []marketplace.Category
	for _, c := range cats {
		result = append(result, marketplace.Category{
			ID:       strconv.Itoa(c.ID),
			Name:     c.Name,
			ParentID: strconv.Itoa(c.Parent),
		})
	}
	return result, nil
}

func (a *WooCommerceAdapter) ValidateListing(ctx context.Context, listing marketplace.ListingData) (*marketplace.ValidationResult, error) {
	result := &marketplace.ValidationResult{IsValid: true}
	if listing.Title == "" {
		result.IsValid = false
		result.Errors = append(result.Errors, marketplace.ValidationError{Field: "title", Message: "Title is required"})
	}
	if listing.Price < 0 {
		result.IsValid = false
		result.Errors = append(result.Errors, marketplace.ValidationError{Field: "price", Message: "Price must be 0 or greater"})
	}
	return result, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func buildWooProductRequest(listing marketplace.ListingData) *woocommerce.Product {
	qty := listing.Quantity
	priceStr := strconv.FormatFloat(listing.Price, 'f', 2, 64)

	product := &woocommerce.Product{
		Name:          listing.Title,
		Description:   listing.Description,
		RegularPrice:  priceStr,
		ManageStock:   true,
		StockQuantity: &qty,
		Status:        "publish",
		Type:          "simple",
	}

	if listing.Attributes != nil {
		if sku, ok := listing.Attributes["sku"].(string); ok && sku != "" {
			product.SKU = sku
		}
		if salePrice, ok := listing.Attributes["sale_price"].(string); ok && salePrice != "" {
			product.SalePrice = salePrice
		}
		if weight, ok := listing.Attributes["weight"].(string); ok {
			product.Weight = weight
		}
		if length, ok := listing.Attributes["length"].(string); ok {
			product.Dimensions.Length = length
		}
		if width, ok := listing.Attributes["width"].(string); ok {
			product.Dimensions.Width = width
		}
		if height, ok := listing.Attributes["height"].(string); ok {
			product.Dimensions.Height = height
		}
		if productType, ok := listing.Attributes["type"].(string); ok && productType != "" {
			product.Type = productType
		}
		if downloadable, ok := listing.Attributes["downloadable"].(bool); ok {
			product.Downloadable = downloadable
		}
		if virtual, ok := listing.Attributes["virtual"].(bool); ok {
			product.Virtual = virtual
		}
		if shortDesc, ok := listing.Attributes["short_description"].(string); ok {
			product.ShortDescription = shortDesc
		}
	}

	// Categories
	if listing.CategoryID != "" {
		catID, err := strconv.Atoi(listing.CategoryID)
		if err == nil {
			product.Categories = []woocommerce.ProductCategory{{ID: catID}}
		}
	}

	// Images
	for _, imgURL := range listing.Images {
		product.Images = append(product.Images, woocommerce.ProductImage{Src: imgURL})
	}

	return product
}

func convertWooProduct(p woocommerce.Product) marketplace.MarketplaceProduct {
	mp := marketplace.MarketplaceProduct{
		ExternalID:  strconv.Itoa(p.ID),
		Title:       p.Name,
		Description: p.Description,
		SKU:         p.SKU,
	}

	if p.Price != "" {
		if f, err := strconv.ParseFloat(p.Price, 64); err == nil {
			mp.Price = f
		}
	}

	if p.StockQuantity != nil {
		mp.Quantity = *p.StockQuantity
	}
	mp.IsInStock = p.StockStatus != "outofstock" && (p.StockQuantity == nil || *p.StockQuantity > 0)

	for i, img := range p.Images {
		mp.Images = append(mp.Images, marketplace.ImageData{
			URL:    img.Src,
			IsMain: i == 0,
		})
	}

	mp.RawData = map[string]interface{}{
		"id":             p.ID,
		"name":           p.Name,
		"status":         p.Status,
		"type":           p.Type,
		"sku":            p.SKU,
		"regular_price":  p.RegularPrice,
		"sale_price":     p.SalePrice,
		"stock_quantity": p.StockQuantity,
		"permalink":      p.Permalink,
	}

	return mp
}

func toWooFloat64(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case string:
		f, _ := strconv.ParseFloat(n, 64)
		return f
	}
	return 0
}

func toWooInt(v interface{}) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case string:
		i, _ := strconv.Atoi(n)
		return i
	}
	return 0
}

func (a *WooCommerceAdapter) CancelOrder(ctx context.Context, externalOrderID string) error {
	return marketplace.ErrCancelNotSupported
}
