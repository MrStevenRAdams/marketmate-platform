package adapters

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"module-a/marketplace"
	"module-a/marketplace/clients/shopwired"
)

// ============================================================================
// SHOPWIRED MARKETPLACE ADAPTER
// ============================================================================

type ShopWiredAdapter struct {
	credentials marketplace.Credentials
	client      *shopwired.Client
}

func NewShopWiredAdapter(ctx context.Context, credentials marketplace.Credentials) (marketplace.MarketplaceAdapter, error) {
	apiKey := credentials.Data["api_key"]
	apiSecret := credentials.Data["api_secret"]
	if apiKey == "" || apiSecret == "" {
		return nil, fmt.Errorf("api_key and api_secret are required for ShopWired")
	}
	return &ShopWiredAdapter{
		credentials: credentials,
		client:      shopwired.NewClient(apiKey, apiSecret),
	}, nil
}

func (a *ShopWiredAdapter) Connect(ctx context.Context, credentials marketplace.Credentials) error { return nil }
func (a *ShopWiredAdapter) Disconnect(ctx context.Context) error                                   { return nil }
func (a *ShopWiredAdapter) RefreshAuth(ctx context.Context) error                                  { return nil }

func (a *ShopWiredAdapter) TestConnection(ctx context.Context) error {
	return a.client.TestConnection()
}

func (a *ShopWiredAdapter) GetConnectionStatus(ctx context.Context) (*marketplace.ConnectionStatus, error) {
	now := time.Now()
	err := a.TestConnection(ctx)
	st := &marketplace.ConnectionStatus{IsConnected: err == nil, LastChecked: now}
	if err != nil {
		st.ErrorMessage = err.Error()
	} else {
		st.LastSuccessful = now
	}
	return st, nil
}

// ── Product Import ────────────────────────────────────────────────────────────

func (a *ShopWiredAdapter) FetchListings(ctx context.Context, filters marketplace.ImportFilters) ([]marketplace.MarketplaceProduct, error) {
	pageSize := filters.PageSize
	if pageSize <= 0 {
		pageSize = 50
	}
	products, err := a.client.ListProducts(0, pageSize)
	if err != nil {
		return nil, err
	}
	var result []marketplace.MarketplaceProduct
	for _, p := range products {
		result = append(result, swConvertProduct(p))
	}
	return result, nil
}

func (a *ShopWiredAdapter) FetchProduct(ctx context.Context, externalID string) (*marketplace.MarketplaceProduct, error) {
	id, err := strconv.Atoi(externalID)
	if err != nil {
		return nil, fmt.Errorf("invalid ShopWired product id %q", externalID)
	}
	p, err := a.client.GetProduct(id)
	if err != nil {
		return nil, err
	}
	mp := swConvertProduct(*p)
	return &mp, nil
}

func (a *ShopWiredAdapter) FetchProductImages(ctx context.Context, externalID string) ([]marketplace.ImageData, error) {
	id, err := strconv.Atoi(externalID)
	if err != nil {
		return nil, fmt.Errorf("invalid ShopWired product id %q", externalID)
	}
	p, err := a.client.GetProduct(id)
	if err != nil {
		return nil, err
	}
	var images []marketplace.ImageData
	for i, img := range p.Images {
		images = append(images, marketplace.ImageData{URL: img.URL, Position: i, IsMain: i == 0})
	}
	return images, nil
}

func (a *ShopWiredAdapter) FetchInventory(ctx context.Context, externalID string) (*marketplace.InventoryLevel, error) {
	id, err := strconv.Atoi(externalID)
	if err != nil {
		return nil, fmt.Errorf("invalid ShopWired product id %q", externalID)
	}
	p, err := a.client.GetProduct(id)
	if err != nil {
		return nil, err
	}
	return &marketplace.InventoryLevel{ExternalID: externalID, Quantity: p.Stock, UpdatedAt: time.Now()}, nil
}

// ── Listing Management ────────────────────────────────────────────────────────

func (a *ShopWiredAdapter) CreateListing(ctx context.Context, listing marketplace.ListingData) (*marketplace.ListingResult, error) {
	product, err := a.client.CreateProduct(swBuildPayload(listing))
	if err != nil {
		return nil, fmt.Errorf("create ShopWired product: %w", err)
	}
	return &marketplace.ListingResult{ExternalID: strconv.Itoa(product.ID), URL: product.URL, Status: "published"}, nil
}

func (a *ShopWiredAdapter) UpdateListing(ctx context.Context, externalID string, updates marketplace.ListingData) error {
	id, err := strconv.Atoi(externalID)
	if err != nil {
		return fmt.Errorf("invalid ShopWired product id %q: %w", externalID, err)
	}
	_, err = a.client.UpdateProduct(id, swBuildPayload(updates))
	return err
}

func (a *ShopWiredAdapter) DeleteListing(ctx context.Context, externalID string) error {
	id, err := strconv.Atoi(externalID)
	if err != nil {
		return fmt.Errorf("invalid ShopWired product id %q: %w", externalID, err)
	}
	return a.client.DeleteProduct(id)
}

func (a *ShopWiredAdapter) PublishListing(ctx context.Context, externalID string) error {
	id, err := strconv.Atoi(externalID)
	if err != nil {
		return fmt.Errorf("invalid ShopWired product id %q: %w", externalID, err)
	}
	_, err = a.client.UpdateProduct(id, map[string]interface{}{"active": true})
	return err
}

func (a *ShopWiredAdapter) UnpublishListing(ctx context.Context, externalID string) error {
	id, err := strconv.Atoi(externalID)
	if err != nil {
		return fmt.Errorf("invalid ShopWired product id %q: %w", externalID, err)
	}
	_, err = a.client.UpdateProduct(id, map[string]interface{}{"active": false})
	return err
}

// ── Bulk Operations ───────────────────────────────────────────────────────────

func (a *ShopWiredAdapter) BulkCreateListings(ctx context.Context, listings []marketplace.ListingData) ([]marketplace.ListingResult, error) {
	var results []marketplace.ListingResult
	for _, l := range listings {
		res, err := a.CreateListing(ctx, l)
		if err != nil {
			results = append(results, marketplace.ListingResult{Errors: []marketplace.ValidationError{{Message: err.Error(), Severity: "error"}}})
			continue
		}
		results = append(results, *res)
	}
	return results, nil
}

func (a *ShopWiredAdapter) BulkUpdateListings(ctx context.Context, updates []marketplace.ListingUpdate) ([]marketplace.UpdateResult, error) {
	var results []marketplace.UpdateResult
	for _, u := range updates {
		listing := marketplace.ListingData{}
		if u.Updates != nil {
			if t, ok := u.Updates["title"].(string); ok { listing.Title = t }
			if p, ok := u.Updates["price"].(float64); ok { listing.Price = p }
			if d, ok := u.Updates["description"].(string); ok { listing.Description = d }
		}
		err := a.UpdateListing(ctx, u.ExternalID, listing)
		if err != nil {
			results = append(results, marketplace.UpdateResult{ExternalID: u.ExternalID, Success: false, Errors: []marketplace.ValidationError{{Message: err.Error(), Severity: "error"}}})
			continue
		}
		results = append(results, marketplace.UpdateResult{ExternalID: u.ExternalID, Success: true, UpdatedAt: time.Now()})
	}
	return results, nil
}

// ── Sync & Monitoring ─────────────────────────────────────────────────────────

func (a *ShopWiredAdapter) GetListingStatus(ctx context.Context, externalID string) (*marketplace.ListingStatus, error) {
	id, err := strconv.Atoi(externalID)
	if err != nil {
		return nil, fmt.Errorf("invalid ShopWired product id %q", externalID)
	}
	p, err := a.client.GetProduct(id)
	if err != nil {
		return nil, err
	}
	status := "inactive"
	if p.Active {
		status = "published"
	}
	return &marketplace.ListingStatus{ExternalID: externalID, Status: status, IsActive: p.Active, Quantity: p.Stock, Price: p.Price, LastUpdated: time.Now()}, nil
}

func (a *ShopWiredAdapter) SyncInventory(ctx context.Context, externalID string, quantity int) error {
	return a.client.UpdateStock(externalID, quantity)
}

func (a *ShopWiredAdapter) SyncPrice(ctx context.Context, externalID string, price float64) error {
	id, err := strconv.Atoi(externalID)
	if err != nil {
		return fmt.Errorf("invalid ShopWired product id %q", externalID)
	}
	_, err = a.client.UpdateProduct(id, map[string]interface{}{"price": price})
	return err
}

func (a *ShopWiredAdapter) CancelOrder(ctx context.Context, externalOrderID string) error {
	id, err := strconv.Atoi(externalOrderID)
	if err != nil {
		return fmt.Errorf("invalid ShopWired order id %q", externalOrderID)
	}
	return a.client.UpdateOrderStatus(id, "cancelled", "", "")
}

// ── Metadata ──────────────────────────────────────────────────────────────────

func (a *ShopWiredAdapter) GetName() string        { return "shopwired" }
func (a *ShopWiredAdapter) GetDisplayName() string { return "ShopWired" }

func (a *ShopWiredAdapter) GetSupportedFeatures() []string {
	return []string{"listing", "order_sync", "inventory_sync", "price_sync", "tracking"}
}

func (a *ShopWiredAdapter) GetRequiredFields() []marketplace.RequiredField {
	return []marketplace.RequiredField{
		{Name: "api_key", Type: "text", Description: "ShopWired API Key"},
		{Name: "api_secret", Type: "password", Description: "ShopWired API Secret"},
	}
}

func (a *ShopWiredAdapter) GetCategories(ctx context.Context) ([]marketplace.Category, error) {
	cats, err := a.client.GetAllCategories()
	if err != nil {
		return nil, err
	}
	var result []marketplace.Category
	for _, c := range cats {
		result = append(result, marketplace.Category{ID: strconv.Itoa(c.ID), Name: c.Title})
	}
	return result, nil
}

func (a *ShopWiredAdapter) ValidateListing(ctx context.Context, listing marketplace.ListingData) (*marketplace.ValidationResult, error) {
	var errs []marketplace.ValidationError
	if listing.Title == "" {
		errs = append(errs, marketplace.ValidationError{Field: "title", Message: "title is required", Severity: "error"})
	}
	if listing.Price <= 0 {
		errs = append(errs, marketplace.ValidationError{Field: "price", Message: "price must be greater than 0", Severity: "error"})
	}
	return &marketplace.ValidationResult{IsValid: len(errs) == 0, Errors: errs}, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func swBuildPayload(listing marketplace.ListingData) map[string]interface{} {
	payload := map[string]interface{}{"title": listing.Title, "price": listing.Price, "active": true}
	if listing.Description != "" {
		payload["description"] = listing.Description
	}
	if sku, ok := listing.Attributes["sku"].(string); ok && sku != "" {
		payload["sku"] = sku
	}
	if listing.Quantity > 0 {
		payload["stock"] = listing.Quantity
	}
	if listing.CategoryID != "" {
		if id, err := strconv.Atoi(listing.CategoryID); err == nil {
			payload["categoryIds"] = []int{id}
		}
	}
	if listing.Attributes != nil {
		if brandID, ok := listing.Attributes["brand_id"]; ok {
			payload["brandId"] = brandID
		}
	}
	return payload
}

func swConvertProduct(p shopwired.Product) marketplace.MarketplaceProduct {
	mp := marketplace.MarketplaceProduct{
		ExternalID:  strconv.Itoa(p.ID),
		SKU:         p.SKU,
		Title:       p.Title,
		Description: p.Description,
		Price:       p.Price,
		Currency:    "GBP",
		Quantity:    p.Stock,
		IsInStock:   p.Stock > 0,
		ListingURL:  p.URL,
		Identifiers: marketplace.Identifiers{GTIN: p.GTIN},
	}
	for i, img := range p.Images {
		mp.Images = append(mp.Images, marketplace.ImageData{URL: img.URL, Position: i, IsMain: i == 0})
	}
	return mp
}
