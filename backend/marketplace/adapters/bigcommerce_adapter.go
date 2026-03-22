package adapters

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"module-a/marketplace"
	"module-a/marketplace/clients/bigcommerce"
)

// ============================================================================
// BIGCOMMERCE MARKETPLACE ADAPTER
// ============================================================================
// Implements marketplace.MarketplaceAdapter for BigCommerce stores.
// Credential fields: store_hash, client_id, access_token
// External ID is the BigCommerce numeric product ID (as string).
// ============================================================================

type BigCommerceAdapter struct {
	credentials marketplace.Credentials
	client      *bigcommerce.Client
}

func NewBigCommerceAdapter(ctx context.Context, credentials marketplace.Credentials) (marketplace.MarketplaceAdapter, error) {
	storeHash := credentials.Data["store_hash"]
	clientID := credentials.Data["client_id"]
	accessToken := credentials.Data["access_token"]

	if storeHash == "" {
		return nil, fmt.Errorf("store_hash is required for BigCommerce")
	}
	if accessToken == "" {
		return nil, fmt.Errorf("access_token is required for BigCommerce")
	}

	client := bigcommerce.NewClient(storeHash, clientID, accessToken)

	return &BigCommerceAdapter{
		credentials: credentials,
		client:      client,
	}, nil
}

// ── Connection ────────────────────────────────────────────────────────────────

func (a *BigCommerceAdapter) Connect(ctx context.Context, credentials marketplace.Credentials) error {
	return nil
}

func (a *BigCommerceAdapter) Disconnect(ctx context.Context) error {
	return nil
}

func (a *BigCommerceAdapter) TestConnection(ctx context.Context) error {
	return a.client.TestConnection()
}

func (a *BigCommerceAdapter) RefreshAuth(ctx context.Context) error {
	// BigCommerce access tokens are long-lived API credentials — no refresh needed
	return nil
}

func (a *BigCommerceAdapter) GetConnectionStatus(ctx context.Context) (*marketplace.ConnectionStatus, error) {
	err := a.TestConnection(ctx)
	return &marketplace.ConnectionStatus{
		IsConnected:  err == nil,
		ErrorMessage: func() string { if err != nil { return err.Error() }; return "" }(),
		LastChecked:  time.Now(),
	}, nil
}

// ── Product Import ────────────────────────────────────────────────────────────

func (a *BigCommerceAdapter) FetchListings(ctx context.Context, filters marketplace.ImportFilters) ([]marketplace.MarketplaceProduct, error) {
	products, err := a.client.GetAllProducts()
	if err != nil {
		return nil, fmt.Errorf("BigCommerce FetchListings: %w", err)
	}

	var result []marketplace.MarketplaceProduct
	for _, p := range products {
		mp := convertBigCommerceProduct(p)
		result = append(result, mp)
		if filters.ProductCallback != nil {
			if !filters.ProductCallback(mp) {
				break
			}
		}
	}
	return result, nil
}

func (a *BigCommerceAdapter) FetchProduct(ctx context.Context, externalID string) (*marketplace.MarketplaceProduct, error) {
	id, err := strconv.Atoi(externalID)
	if err != nil {
		return nil, fmt.Errorf("BigCommerce externalID must be numeric product ID, got: %s", externalID)
	}
	p, err := a.client.GetProduct(id)
	if err != nil {
		return nil, fmt.Errorf("BigCommerce FetchProduct %s: %w", externalID, err)
	}
	mp := convertBigCommerceProduct(*p)
	return &mp, nil
}

func (a *BigCommerceAdapter) FetchProductImages(ctx context.Context, externalID string) ([]marketplace.ImageData, error) {
	mp, err := a.FetchProduct(ctx, externalID)
	if err != nil {
		return nil, err
	}
	return mp.Images, nil
}

func (a *BigCommerceAdapter) FetchInventory(ctx context.Context, externalID string) (*marketplace.InventoryLevel, error) {
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

func (a *BigCommerceAdapter) CreateListing(ctx context.Context, listing marketplace.ListingData) (*marketplace.ListingResult, error) {
	req := buildBigCommerceProductRequest(listing)
	created, err := a.client.CreateProduct(req)
	if err != nil {
		return nil, fmt.Errorf("BigCommerce CreateProduct: %w", err)
	}
	storeURL := bcStoreURL(a.credentials.Data["store_hash"])
	return &marketplace.ListingResult{
		ExternalID: strconv.Itoa(created.ID),
		Status:     bcVisibilityToStatus(created.IsVisible),
		URL:        storeURL + "/products/" + created.SKU,
	}, nil
}

func (a *BigCommerceAdapter) UpdateListing(ctx context.Context, externalID string, updates marketplace.ListingData) error {
	id, err := strconv.Atoi(externalID)
	if err != nil {
		return fmt.Errorf("invalid BigCommerce product ID: %s", externalID)
	}
	req := buildBigCommerceProductRequest(updates)
	_, err = a.client.UpdateProduct(id, req)
	return err
}

func (a *BigCommerceAdapter) DeleteListing(ctx context.Context, externalID string) error {
	id, err := strconv.Atoi(externalID)
	if err != nil {
		return fmt.Errorf("invalid BigCommerce product ID: %s", externalID)
	}
	return a.client.DeleteProduct(id)
}

func (a *BigCommerceAdapter) PublishListing(ctx context.Context, externalID string) error {
	id, err := strconv.Atoi(externalID)
	if err != nil {
		return fmt.Errorf("invalid BigCommerce product ID: %s", externalID)
	}
	return a.client.SetProductVisible(id, true)
}

func (a *BigCommerceAdapter) UnpublishListing(ctx context.Context, externalID string) error {
	id, err := strconv.Atoi(externalID)
	if err != nil {
		return fmt.Errorf("invalid BigCommerce product ID: %s", externalID)
	}
	return a.client.SetProductVisible(id, false)
}

// ── Bulk Operations ───────────────────────────────────────────────────────────

func (a *BigCommerceAdapter) BulkCreateListings(ctx context.Context, listings []marketplace.ListingData) ([]marketplace.ListingResult, error) {
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

func (a *BigCommerceAdapter) BulkUpdateListings(ctx context.Context, updates []marketplace.ListingUpdate) ([]marketplace.UpdateResult, error) {
	var results []marketplace.UpdateResult
	for _, u := range updates {
		listing := marketplace.ListingData{
			Title:    toBCString(u.Updates["title"]),
			Price:    toBCFloat64(u.Updates["price"]),
			Quantity: toBCInt(u.Updates["quantity"]),
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

func (a *BigCommerceAdapter) GetListingStatus(ctx context.Context, externalID string) (*marketplace.ListingStatus, error) {
	id, err := strconv.Atoi(externalID)
	if err != nil {
		return nil, fmt.Errorf("invalid BigCommerce product ID: %s", externalID)
	}
	p, err := a.client.GetProduct(id)
	if err != nil {
		return nil, err
	}
	return &marketplace.ListingStatus{
		ExternalID: externalID,
		Status:     bcVisibilityToStatus(p.IsVisible),
		IsActive:   p.IsVisible,
		Quantity:   p.InventoryLevel,
		Price:      p.Price,
	}, nil
}

func (a *BigCommerceAdapter) SyncInventory(ctx context.Context, externalID string, quantity int) error {
	id, err := strconv.Atoi(externalID)
	if err != nil {
		return fmt.Errorf("invalid BigCommerce product ID: %s", externalID)
	}
	return a.client.UpdateProductStock(id, quantity)
}

func (a *BigCommerceAdapter) SyncPrice(ctx context.Context, externalID string, price float64) error {
	id, err := strconv.Atoi(externalID)
	if err != nil {
		return fmt.Errorf("invalid BigCommerce product ID: %s", externalID)
	}
	return a.client.UpdateProductPrice(id, price)
}

// ── Metadata ──────────────────────────────────────────────────────────────────

func (a *BigCommerceAdapter) GetName() string        { return "bigcommerce" }
func (a *BigCommerceAdapter) GetDisplayName() string { return "BigCommerce" }

func (a *BigCommerceAdapter) GetSupportedFeatures() []string {
	return []string{"import", "listing", "order_sync", "inventory_sync", "price_sync", "tracking"}
}

func (a *BigCommerceAdapter) GetRequiredFields() []marketplace.RequiredField {
	return []marketplace.RequiredField{
		{Name: "store_hash", Type: "string", Description: "Your BigCommerce store hash (found in API settings)"},
		{Name: "client_id", Type: "string", Description: "BigCommerce API Client ID"},
		{Name: "access_token", Type: "password", Description: "BigCommerce API Access Token"},
	}
}

func (a *BigCommerceAdapter) GetCategories(ctx context.Context) ([]marketplace.Category, error) {
	cats, err := a.client.GetCategories()
	if err != nil {
		return nil, err
	}
	var result []marketplace.Category
	for _, c := range cats {
		if !c.IsVisible {
			continue
		}
		result = append(result, marketplace.Category{
			ID:       strconv.Itoa(c.ID),
			Name:     c.Name,
			ParentID: strconv.Itoa(c.ParentID),
		})
	}
	return result, nil
}

func (a *BigCommerceAdapter) ValidateListing(ctx context.Context, listing marketplace.ListingData) (*marketplace.ValidationResult, error) {
	result := &marketplace.ValidationResult{IsValid: true}
	if listing.Title == "" {
		result.IsValid = false
		result.Errors = append(result.Errors, marketplace.ValidationError{Field: "name", Message: "Product name is required"})
	}
	if listing.Price < 0 {
		result.IsValid = false
		result.Errors = append(result.Errors, marketplace.ValidationError{Field: "price", Message: "Price must be 0 or greater"})
	}
	return result, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func buildBigCommerceProductRequest(listing marketplace.ListingData) *bigcommerce.Product {
	product := &bigcommerce.Product{
		Name:              listing.Title,
		Type:              "physical",
		Price:             listing.Price,
		IsVisible:         true,
		InventoryLevel:    listing.Quantity,
		InventoryTracking: "product",
	}

	if listing.Description != "" {
		product.Description = listing.Description
	}

	if listing.CategoryID != "" {
		if catID, err := strconv.Atoi(listing.CategoryID); err == nil {
			product.Categories = []int{catID}
		}
	}

	if listing.Weight != nil {
		product.Weight = listing.Weight.Value
	}

	if listing.Attributes != nil {
		if sku, ok := listing.Attributes["sku"].(string); ok && sku != "" {
			product.SKU = sku
		}
		if t, ok := listing.Attributes["type"].(string); ok && t != "" {
			product.Type = t
		}
		if w, ok := listing.Attributes["weight"].(float64); ok && w > 0 {
			product.Weight = w
		}
		if metaDesc, ok := listing.Attributes["meta_description"].(string); ok {
			product.MetaDescription = metaDesc
		}
		if pageTitle, ok := listing.Attributes["page_title"].(string); ok {
			product.PageTitle = pageTitle
		}
		if condition, ok := listing.Attributes["condition"].(string); ok && condition != "" {
			product.Condition = condition
		}
		if availability, ok := listing.Attributes["availability"].(string); ok && availability != "" {
			product.Availability = availability
		}
		if visible, ok := listing.Attributes["is_visible"].(bool); ok {
			product.IsVisible = visible
		}
	}

	if listing.Condition != "" {
		product.Condition = listing.Condition
	}

	return product
}

func convertBigCommerceProduct(p bigcommerce.Product) marketplace.MarketplaceProduct {
	mp := marketplace.MarketplaceProduct{
		ExternalID:  strconv.Itoa(p.ID),
		Title:       p.Name,
		SKU:         p.SKU,
		Price:       p.Price,
		Description: p.Description,
		Quantity:    p.InventoryLevel,
		IsInStock:   p.InventoryLevel > 0,
	}

	for i, img := range p.Images {
		u := img.URLStandard
		if u == "" {
			u = img.ImageURL
		}
		if u == "" {
			continue
		}
		mp.Images = append(mp.Images, marketplace.ImageData{
			URL:      u,
			Position: img.SortOrder,
			IsMain:   i == 0 || img.IsThumbnail,
		})
	}

	mp.RawData = map[string]interface{}{
		"id":           p.ID,
		"sku":          p.SKU,
		"name":         p.Name,
		"is_visible":   p.IsVisible,
		"type":         p.Type,
		"price":        p.Price,
		"date_created": p.DateCreated,
		"date_modified": p.DateModified,
	}

	return mp
}

func bcVisibilityToStatus(visible bool) string {
	if visible {
		return "active"
	}
	return "inactive"
}

func bcStoreURL(storeHash string) string {
	return "https://store-" + storeHash + ".mybigcommerce.com"
}

func toBCString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func toBCFloat64(v interface{}) float64 {
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

func toBCInt(v interface{}) int {
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

func (a *BigCommerceAdapter) CancelOrder(ctx context.Context, externalOrderID string) error {
	return marketplace.ErrCancelNotSupported
}
