package adapters

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"module-a/marketplace"
	"module-a/marketplace/clients/walmart"
)

// ============================================================================
// WALMART MARKETPLACE ADAPTER
// ============================================================================
// Implements marketplace.MarketplaceAdapter for Walmart Marketplace.
// Credential fields: client_id, client_secret
// Auth: OAuth 2.0 Client Credentials
// ============================================================================

type WalmartAdapter struct {
	credentials marketplace.Credentials
	client      *walmart.Client
}

func NewWalmartAdapter(ctx context.Context, credentials marketplace.Credentials) (marketplace.MarketplaceAdapter, error) {
	clientID := credentials.Data["client_id"]
	clientSecret := credentials.Data["client_secret"]

	if clientID == "" {
		return nil, fmt.Errorf("client_id is required for Walmart")
	}
	if clientSecret == "" {
		return nil, fmt.Errorf("client_secret is required for Walmart")
	}

	client := walmart.NewClient(clientID, clientSecret)

	return &WalmartAdapter{
		credentials: credentials,
		client:      client,
	}, nil
}

// ── Connection ────────────────────────────────────────────────────────────────

func (a *WalmartAdapter) Connect(ctx context.Context, credentials marketplace.Credentials) error {
	return nil
}

func (a *WalmartAdapter) Disconnect(ctx context.Context) error {
	return nil
}

func (a *WalmartAdapter) TestConnection(ctx context.Context) error {
	return a.client.TestConnection()
}

func (a *WalmartAdapter) RefreshAuth(ctx context.Context) error {
	// Token is refreshed automatically inside the client on expiry
	_, err := a.client.GetToken()
	return err
}

func (a *WalmartAdapter) GetConnectionStatus(ctx context.Context) (*marketplace.ConnectionStatus, error) {
	err := a.TestConnection(ctx)
	return &marketplace.ConnectionStatus{
		IsConnected:  err == nil,
		ErrorMessage: func() string { if err != nil { return err.Error() }; return "" }(),
		LastChecked:  time.Now(),
	}, nil
}

// ── Product Import ────────────────────────────────────────────────────────────

func (a *WalmartAdapter) FetchListings(ctx context.Context, filters marketplace.ImportFilters) ([]marketplace.MarketplaceProduct, error) {
	items, err := a.client.GetAllItems()
	if err != nil {
		return nil, fmt.Errorf("Walmart FetchListings: %w", err)
	}

	var result []marketplace.MarketplaceProduct
	for _, item := range items {
		mp := convertWalmartItem(item)
		result = append(result, mp)
		if filters.ProductCallback != nil {
			if !filters.ProductCallback(mp) {
				break
			}
		}
	}
	return result, nil
}

func (a *WalmartAdapter) FetchProduct(ctx context.Context, externalID string) (*marketplace.MarketplaceProduct, error) {
	// Walmart item fetch is by SKU via the items endpoint with a filter
	items, err := a.client.GetAllItems()
	if err != nil {
		return nil, fmt.Errorf("Walmart FetchProduct %s: %w", externalID, err)
	}
	for _, item := range items {
		if item.SKU == externalID || strconv.FormatInt(item.ItemID, 10) == externalID {
			mp := convertWalmartItem(item)
			return &mp, nil
		}
	}
	return nil, fmt.Errorf("Walmart item not found: %s", externalID)
}

func (a *WalmartAdapter) FetchProductImages(ctx context.Context, externalID string) ([]marketplace.ImageData, error) {
	mp, err := a.FetchProduct(ctx, externalID)
	if err != nil {
		return nil, err
	}
	return mp.Images, nil
}

func (a *WalmartAdapter) FetchInventory(ctx context.Context, externalID string) (*marketplace.InventoryLevel, error) {
	inv, err := a.client.GetInventory(externalID)
	if err != nil {
		return nil, fmt.Errorf("Walmart FetchInventory %s: %w", externalID, err)
	}
	return &marketplace.InventoryLevel{
		ExternalID: externalID,
		Quantity:   inv.Quantity.Amount,
	}, nil
}

// ── Listing Management ────────────────────────────────────────────────────────

func (a *WalmartAdapter) CreateListing(ctx context.Context, listing marketplace.ListingData) (*marketplace.ListingResult, error) {
	payload := buildWalmartItemFeed(listing)
	feed, err := a.client.SubmitFeed("MP_ITEM", payload)
	if err != nil {
		return nil, fmt.Errorf("Walmart CreateListing: %w", err)
	}
	return &marketplace.ListingResult{
		ExternalID: feed.FeedID,
		Status:     "feed_submitted",
		URL:        fmt.Sprintf("https://marketplace.walmartapis.com/v3/feeds/%s", feed.FeedID),
	}, nil
}

func (a *WalmartAdapter) UpdateListing(ctx context.Context, externalID string, updates marketplace.ListingData) error {
	payload := buildWalmartItemFeed(updates)
	_, err := a.client.SubmitFeed("MP_ITEM", payload)
	return err
}

func (a *WalmartAdapter) DeleteListing(ctx context.Context, externalID string) error {
	return a.client.RetireItem(externalID)
}

func (a *WalmartAdapter) PublishListing(ctx context.Context, externalID string) error {
	// Walmart items are published by submitting an MP_ITEM feed with lifecycleStatus ACTIVE
	payload := map[string]interface{}{
		"MPItemFeed": map[string]interface{}{
			"MPItemFeedHeader": map[string]interface{}{"version": "4.7"},
			"MPItem": []map[string]interface{}{
				{
					"sku":             externalID,
					"lifecycleStatus": "ACTIVE",
				},
			},
		},
	}
	_, err := a.client.SubmitFeed("MP_ITEM", payload)
	return err
}

func (a *WalmartAdapter) UnpublishListing(ctx context.Context, externalID string) error {
	return a.client.RetireItem(externalID)
}

// ── Bulk ──────────────────────────────────────────────────────────────────────

func (a *WalmartAdapter) BulkCreateListings(ctx context.Context, listings []marketplace.ListingData) ([]marketplace.ListingResult, error) {
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

func (a *WalmartAdapter) BulkUpdateListings(ctx context.Context, updates []marketplace.ListingUpdate) ([]marketplace.UpdateResult, error) {
	var results []marketplace.UpdateResult
	for _, u := range updates {
		listing := marketplace.ListingData{
			Title:    fmt.Sprintf("%v", u.Updates["title"]),
			Price:    toWalmartFloat64(u.Updates["price"]),
			Quantity: toWalmartInt(u.Updates["quantity"]),
		}
		err := a.UpdateListing(ctx, u.ExternalID, listing)
		result := marketplace.UpdateResult{
			ExternalID: u.ExternalID,
			Success:    err == nil,
		}
		if err != nil {
			result.Errors = []marketplace.ValidationError{{Message: err.Error()}}
		}
		results = append(results, result)
	}
	return results, nil
}

// ── Status ────────────────────────────────────────────────────────────────────

func (a *WalmartAdapter) GetListingStatus(ctx context.Context, externalID string) (*marketplace.ListingStatus, error) {
	mp, err := a.FetchProduct(ctx, externalID)
	if err != nil {
		return nil, err
	}
	status := mp.RawData["publishStatus"]
	if s, ok := status.(string); ok {
		return &marketplace.ListingStatus{ExternalID: externalID, Status: s}, nil
	}
	return &marketplace.ListingStatus{ExternalID: externalID, Status: "unknown"}, nil
}

func (a *WalmartAdapter) SyncInventory(ctx context.Context, externalID string, quantity int) error {
	return a.client.UpdateInventory(externalID, quantity)
}

func (a *WalmartAdapter) SyncPrice(ctx context.Context, externalID string, price float64) error {
	return a.client.UpdatePrice(externalID, price)
}

// ── Metadata ──────────────────────────────────────────────────────────────────

func (a *WalmartAdapter) GetName() string        { return "walmart" }
func (a *WalmartAdapter) GetDisplayName() string { return "Walmart Marketplace" }

func (a *WalmartAdapter) GetSupportedFeatures() []string {
	return []string{"import", "listing", "order_sync", "inventory_sync", "price_sync", "tracking"}
}

func (a *WalmartAdapter) GetRequiredFields() []marketplace.RequiredField {
	return []marketplace.RequiredField{
		{Name: "client_id", Type: "string", Description: "Walmart Marketplace API Client ID"},
		{Name: "client_secret", Type: "password", Description: "Walmart Marketplace API Client Secret"},
	}
}

func (a *WalmartAdapter) GetCategories(ctx context.Context) ([]marketplace.Category, error) {
	// Walmart category taxonomy is large; return a curated top-level list
	categories := []marketplace.Category{
		{ID: "3944", Name: "Electronics"},
		{ID: "3951", Name: "Clothing, Shoes & Accessories"},
		{ID: "5438", Name: "Home & Garden"},
		{ID: "976759", Name: "Toys"},
		{ID: "4044", Name: "Sports & Outdoors"},
		{ID: "1085666", Name: "Baby"},
		{ID: "5429", Name: "Health & Beauty"},
		{ID: "5427", Name: "Automotive"},
		{ID: "4104", Name: "Books"},
		{ID: "3961", Name: "Food"},
	}
	return categories, nil
}

func (a *WalmartAdapter) ValidateListing(ctx context.Context, listing marketplace.ListingData) (*marketplace.ValidationResult, error) {
	result := &marketplace.ValidationResult{IsValid: true}
	if listing.Title == "" {
		result.IsValid = false
		result.Errors = append(result.Errors, marketplace.ValidationError{Field: "title", Message: "Product name is required"})
	}
	if listing.Price <= 0 {
		result.IsValid = false
		result.Errors = append(result.Errors, marketplace.ValidationError{Field: "price", Message: "Price must be greater than 0"})
	}
	if listing.Attributes != nil {
		if upc, ok := listing.Attributes["upc"].(string); !ok || upc == "" {
			result.Errors = append(result.Errors, marketplace.ValidationError{Field: "upc", Message: "UPC/GTIN is strongly recommended for Walmart"})
		}
	}
	return result, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func buildWalmartItemFeed(listing marketplace.ListingData) map[string]interface{} {
	sku := listing.Identifiers.UPC // fallback
	if listing.Attributes != nil {
		if s, ok := listing.Attributes["sku"].(string); ok && s != "" {
			sku = s
		}
	}

	item := map[string]interface{}{
		"sku":         sku,
		"productName": listing.Title,
		"price": map[string]interface{}{
			"currency": "USD",
			"amount":   listing.Price,
		},
		"ShippingWeight": map[string]interface{}{
			"measure": "1",
			"unit":    "LB",
		},
	}

	if listing.Description != "" {
		item["shortDescription"] = listing.Description
	}

	if listing.Attributes != nil {
		if upc, ok := listing.Attributes["upc"].(string); ok && upc != "" {
			item["upc"] = upc
		}
		if brand, ok := listing.Attributes["brand"].(string); ok && brand != "" {
			item["brand"] = brand
		}
		if model, ok := listing.Attributes["model_number"].(string); ok && model != "" {
			item["modelNumber"] = model
		}
		if cat, ok := listing.Attributes["category"].(string); ok && cat != "" {
			item["category"] = cat
		}
		if features, ok := listing.Attributes["key_features"].([]interface{}); ok {
			var bullets []string
			for _, f := range features {
				if s, ok := f.(string); ok {
					bullets = append(bullets, s)
				}
			}
			if len(bullets) > 0 {
				item["keyFeatures"] = bullets
			}
		}
		if weight, ok := listing.Attributes["shipping_weight"].(float64); ok {
			item["ShippingWeight"] = map[string]interface{}{
				"measure": fmt.Sprintf("%.2f", weight),
				"unit":    "LB",
			}
		}
	}

	if len(listing.Images) > 0 {
		item["mainImageUrl"] = listing.Images[0]
		if len(listing.Images) > 1 {
			item["additionalImageUrls"] = listing.Images[1:]
		}
	}

	return map[string]interface{}{
		"MPItemFeed": map[string]interface{}{
			"MPItemFeedHeader": map[string]interface{}{"version": "4.7"},
			"MPItem":           []interface{}{item},
		},
	}
}

func convertWalmartItem(item walmart.Item) marketplace.MarketplaceProduct {
	mp := marketplace.MarketplaceProduct{
		ExternalID: item.SKU,
		Title:      item.ProductName,
		SKU:        item.SKU,
	}

	if item.Price != nil {
		mp.Price = item.Price.Amount
	}

	mp.RawData = map[string]interface{}{
		"itemId":             item.ItemID,
		"sku":                item.SKU,
		"productName":        item.ProductName,
		"publishStatus":      item.PublishStatus,
		"lifecycleStatus":    item.LifecycleStatus,
		"availabilityStatus": item.AvailabilityStatus,
		"offerId":            item.OfferID,
	}

	return mp
}

func toWalmartFloat64(v interface{}) float64 {
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

func toWalmartInt(v interface{}) int {
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

func (a *WalmartAdapter) CancelOrder(ctx context.Context, externalOrderID string) error {
	return marketplace.ErrCancelNotSupported
}
