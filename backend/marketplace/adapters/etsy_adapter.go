package adapters

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"module-a/marketplace"
	"module-a/marketplace/clients/etsy"
)

// ============================================================================
// ETSY MARKETPLACE ADAPTER
// ============================================================================
// Implements marketplace.MarketplaceAdapter for Etsy Open API v3.
// Credential fields: client_id, access_token, refresh_token, shop_id
// ============================================================================

type EtsyAdapter struct {
	credentials marketplace.Credentials
	client      *etsy.Client
}

func NewEtsyAdapter(ctx context.Context, credentials marketplace.Credentials) (marketplace.MarketplaceAdapter, error) {
	apiKey := credentials.Data["client_id"]
	accessToken := credentials.Data["access_token"]
	refreshToken := credentials.Data["refresh_token"]

	if apiKey == "" {
		return nil, fmt.Errorf("client_id is required for Etsy")
	}
	if accessToken == "" {
		return nil, fmt.Errorf("access_token is required — complete the OAuth flow first")
	}

	shopIDStr := credentials.Data["shop_id"]
	var shopID int64
	if shopIDStr != "" {
		shopID, _ = strconv.ParseInt(shopIDStr, 10, 64)
	}

	client := etsy.NewClient(apiKey, accessToken, refreshToken, shopID)

	return &EtsyAdapter{
		credentials: credentials,
		client:      client,
	}, nil
}

// ── Connection ────────────────────────────────────────────────────────────────

func (a *EtsyAdapter) Connect(ctx context.Context, credentials marketplace.Credentials) error {
	return nil
}

func (a *EtsyAdapter) Disconnect(ctx context.Context) error {
	return nil
}

func (a *EtsyAdapter) TestConnection(ctx context.Context) error {
	if a.client.ShopID == 0 {
		return fmt.Errorf("Etsy: shop_id not set — complete OAuth flow first")
	}
	_, err := a.client.GetShopByID(a.client.ShopID)
	if err != nil {
		return fmt.Errorf("Etsy connection test failed: %w", err)
	}
	return nil
}

func (a *EtsyAdapter) RefreshAuth(ctx context.Context) error {
	_, err := a.client.RefreshAccessToken()
	return err
}

func (a *EtsyAdapter) GetConnectionStatus(ctx context.Context) (*marketplace.ConnectionStatus, error) {
	err := a.TestConnection(ctx)
	return &marketplace.ConnectionStatus{
		IsConnected:  err == nil,
		LastChecked:  time.Now(),
		ErrorMessage: func() string { if err != nil { return err.Error() }; return "" }(),
	}, nil
}

// ── Product import ────────────────────────────────────────────────────────────

func (a *EtsyAdapter) FetchListings(ctx context.Context, filters marketplace.ImportFilters) ([]marketplace.MarketplaceProduct, error) {
	pageSize := filters.PageSize
	if pageSize <= 0 {
		pageSize = 100
	}
	offset := 0
	if filters.PageToken != "" {
		offset, _ = strconv.Atoi(filters.PageToken)
	}

	resp, err := a.client.GetListings(offset, pageSize)
	if err != nil {
		return nil, err
	}

	var products []marketplace.MarketplaceProduct
	for _, l := range resp.Results {
		products = append(products, etsyListingToMarketplace(l))
	}
	return products, nil
}

func (a *EtsyAdapter) FetchProduct(ctx context.Context, externalID string) (*marketplace.MarketplaceProduct, error) {
	listingID, err := strconv.ParseInt(externalID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid listing ID: %s", externalID)
	}
	resp, err := a.client.GetListings(0, 1)
	if err != nil {
		return nil, err
	}
	for _, l := range resp.Results {
		if l.ListingID == listingID {
			mp := etsyListingToMarketplace(l)
			return &mp, nil
		}
	}
	return nil, fmt.Errorf("listing %s not found", externalID)
}

func (a *EtsyAdapter) FetchProductImages(ctx context.Context, externalID string) ([]marketplace.ImageData, error) {
	mp, err := a.FetchProduct(ctx, externalID)
	if err != nil {
		return nil, err
	}
	return mp.Images, nil
}

func (a *EtsyAdapter) FetchInventory(ctx context.Context, externalID string) (*marketplace.InventoryLevel, error) {
	mp, err := a.FetchProduct(ctx, externalID)
	if err != nil {
		return nil, err
	}
	return &marketplace.InventoryLevel{
		ExternalID: externalID,
		Quantity:   mp.Quantity,
		UpdatedAt:  time.Now(),
	}, nil
}

// ── Listing management ────────────────────────────────────────────────────────

func (a *EtsyAdapter) CreateListing(ctx context.Context, listing marketplace.ListingData) (*marketplace.ListingResult, error) {
	catID, _ := strconv.ParseInt(listing.CategoryID, 10, 64)

	req := &etsy.CreateListingRequest{
		Title:       listing.Title,
		Description: listing.Description,
		Price:       listing.Price,
		Quantity:    listing.Quantity,
		TaxonomyID:  catID,
		WhoMade:     getStringAttr(listing.Attributes, "who_made", "i_did"),
		WhenMade:    getStringAttr(listing.Attributes, "when_made", "made_to_order"),
		IsSupply:    getBoolAttr(listing.Attributes, "is_supply", false),
	}

	if listing.ShippingProfile != "" {
		req.ShippingProfileID, _ = strconv.ParseInt(listing.ShippingProfile, 10, 64)
	}
	if tags, ok := listing.Attributes["tags"].([]string); ok {
		req.Tags = tags
	}
	if mats, ok := listing.Attributes["materials"].([]string); ok {
		req.Materials = mats
	}

	created, err := a.client.CreateListing(req)
	if err != nil {
		return nil, err
	}

	return &marketplace.ListingResult{
		ExternalID: strconv.FormatInt(created.ListingID, 10),
		Status:     mapEtsyState(created.State),
		CreatedAt:  time.Now(),
	}, nil
}

func (a *EtsyAdapter) UpdateListing(ctx context.Context, externalID string, updates marketplace.ListingData) error {
	listingID, err := strconv.ParseInt(externalID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid listing ID: %s", externalID)
	}
	req := &etsy.UpdateListingRequest{
		Title:       updates.Title,
		Description: updates.Description,
		Price:       updates.Price,
		Quantity:    updates.Quantity,
	}
	if updates.ShippingProfile != "" {
		req.ShippingProfileID, _ = strconv.ParseInt(updates.ShippingProfile, 10, 64)
	}
	_, err = a.client.UpdateListing(listingID, req)
	return err
}

func (a *EtsyAdapter) DeleteListing(ctx context.Context, externalID string) error {
	listingID, err := strconv.ParseInt(externalID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid listing ID: %s", externalID)
	}
	return a.client.DeleteListing(listingID)
}

func (a *EtsyAdapter) PublishListing(ctx context.Context, externalID string) error {
	listingID, err := strconv.ParseInt(externalID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid listing ID: %s", externalID)
	}
	_, err = a.client.PublishListing(listingID)
	return err
}

func (a *EtsyAdapter) UnpublishListing(ctx context.Context, externalID string) error {
	listingID, err := strconv.ParseInt(externalID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid listing ID: %s", externalID)
	}
	_, err = a.client.UpdateListing(listingID, &etsy.UpdateListingRequest{State: "inactive"})
	return err
}

// ── Bulk operations ───────────────────────────────────────────────────────────

func (a *EtsyAdapter) BulkCreateListings(ctx context.Context, listings []marketplace.ListingData) ([]marketplace.ListingResult, error) {
	var results []marketplace.ListingResult
	for _, l := range listings {
		result, err := a.CreateListing(ctx, l)
		if err != nil {
			results = append(results, marketplace.ListingResult{
				Status: "error",
				Errors: []marketplace.ValidationError{{Message: err.Error()}},
			})
			continue
		}
		results = append(results, *result)
	}
	return results, nil
}

func (a *EtsyAdapter) BulkUpdateListings(ctx context.Context, updates []marketplace.ListingUpdate) ([]marketplace.UpdateResult, error) {
	var results []marketplace.UpdateResult
	for _, u := range updates {
		listingData := marketplace.ListingData{
			Attributes: u.Updates,
		}
		if v, ok := u.Updates["title"].(string); ok { listingData.Title = v }
		if v, ok := u.Updates["description"].(string); ok { listingData.Description = v }
		if v, ok := u.Updates["price"].(float64); ok { listingData.Price = v }
		if v, ok := u.Updates["quantity"].(int); ok { listingData.Quantity = v }

		err := a.UpdateListing(ctx, u.ExternalID, listingData)
		results = append(results, marketplace.UpdateResult{
			ExternalID: u.ExternalID,
			Success:    err == nil,
			UpdatedAt:  time.Now(),
		})
		if err != nil {
			results[len(results)-1].Errors = []marketplace.ValidationError{{Message: err.Error()}}
		}
	}
	return results, nil
}

// ── Sync ──────────────────────────────────────────────────────────────────────

func (a *EtsyAdapter) GetListingStatus(ctx context.Context, externalID string) (*marketplace.ListingStatus, error) {
	mp, err := a.FetchProduct(ctx, externalID)
	if err != nil {
		return nil, err
	}
	return &marketplace.ListingStatus{
		ExternalID:  externalID,
		Status:      mp.RawData["state"].(string),
		IsActive:    mp.RawData["state"] == "active",
		Quantity:    mp.Quantity,
		Price:       mp.Price,
		LastUpdated: time.Now(),
	}, nil
}

func (a *EtsyAdapter) SyncInventory(ctx context.Context, externalID string, quantity int) error {
	return a.UpdateListing(ctx, externalID, marketplace.ListingData{Quantity: quantity})
}

func (a *EtsyAdapter) SyncPrice(ctx context.Context, externalID string, price float64) error {
	return a.UpdateListing(ctx, externalID, marketplace.ListingData{Price: price})
}

// ── Metadata ──────────────────────────────────────────────────────────────────

func (a *EtsyAdapter) GetName() string        { return "etsy" }
func (a *EtsyAdapter) GetDisplayName() string { return "Etsy" }
func (a *EtsyAdapter) GetSupportedFeatures() []string {
	return []string{"import", "listing", "order_sync", "tracking"}
}

func (a *EtsyAdapter) GetRequiredFields() []marketplace.RequiredField {
	return []marketplace.RequiredField{
		{Name: "title", Type: "string", Description: "Listing title (max 140 chars)"},
		{Name: "description", Type: "string", Description: "Listing description"},
		{Name: "price", Type: "number", Description: "Price in USD"},
		{Name: "quantity", Type: "number", Description: "Stock quantity"},
		{Name: "taxonomy_id", Type: "number", Description: "Etsy category (taxonomy) ID"},
		{Name: "who_made", Type: "string", Description: "Who made the item: i_did / collective / someone_else"},
		{Name: "when_made", Type: "string", Description: "When it was made: made_to_order / 2020_2025 / etc."},
		{Name: "shipping_profile_id", Type: "number", Description: "Shipping profile ID"},
	}
}

func (a *EtsyAdapter) GetCategories(ctx context.Context) ([]marketplace.Category, error) {
	nodes, err := a.client.GetTaxonomyNodes()
	if err != nil {
		return nil, err
	}
	return flattenTaxonomyNodes(nodes, 0), nil
}

func (a *EtsyAdapter) ValidateListing(ctx context.Context, listing marketplace.ListingData) (*marketplace.ValidationResult, error) {
	result := &marketplace.ValidationResult{IsValid: true}
	if listing.Title == "" {
		result.IsValid = false
		result.Errors = append(result.Errors, marketplace.ValidationError{Field: "title", Message: "Title is required"})
	}
	if len(listing.Title) > 140 {
		result.IsValid = false
		result.Errors = append(result.Errors, marketplace.ValidationError{Field: "title", Message: "Title must be 140 characters or less"})
	}
	if listing.Price <= 0 {
		result.IsValid = false
		result.Errors = append(result.Errors, marketplace.ValidationError{Field: "price", Message: "Price must be greater than 0"})
	}
	if listing.Quantity < 1 {
		result.IsValid = false
		result.Errors = append(result.Errors, marketplace.ValidationError{Field: "quantity", Message: "Quantity must be at least 1"})
	}
	if listing.CategoryID == "" {
		result.IsValid = false
		result.Errors = append(result.Errors, marketplace.ValidationError{Field: "taxonomy_id", Message: "Category is required"})
	}
	if len(listing.Images) == 0 {
		result.IsValid = false
		result.Errors = append(result.Errors, marketplace.ValidationError{Field: "images", Message: "At least one image is required"})
	}
	if len(listing.Images) > 10 {
		result.IsValid = false
		result.Errors = append(result.Errors, marketplace.ValidationError{Field: "images", Message: "Maximum 10 images allowed"})
	}
	return result, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func etsyListingToMarketplace(l etsy.Listing) marketplace.MarketplaceProduct {
	price := 0.0
	if l.Price.Divisor > 0 {
		price = float64(l.Price.Amount) / float64(l.Price.Divisor)
	}

	var images []marketplace.ImageData
	for i, img := range l.Images {
		images = append(images, marketplace.ImageData{
			URL:    img.URLFullxFull,
			IsMain: i == 0,
		})
	}

	rawData := map[string]interface{}{
		"state":       l.State,
		"listing_id":  strconv.FormatInt(l.ListingID, 10),
		"url":         l.URL,
		"tags":        l.Tags,
		"materials":   l.Materials,
		"who_made":    l.WhoMade,
		"when_made":   l.WhenMade,
		"taxonomy_id": strconv.FormatInt(l.TaxonomyID, 10),
		"shipping_profile_id": strconv.FormatInt(l.ShippingProfileID, 10),
	}

	return marketplace.MarketplaceProduct{
		ExternalID:  strconv.FormatInt(l.ListingID, 10),
		Title:       l.Title,
		Description: l.Description,
		Price:       price,
		Currency:    l.Price.CurrencyCode,
		Quantity:    l.Quantity,
		Images:      images,
		ListingURL:  l.URL,
		IsInStock:   l.Quantity > 0,
		RawData:     rawData,
	}
}

func mapEtsyState(state string) string {
	switch state {
	case "active":
		return "active"
	case "inactive":
		return "inactive"
	case "draft":
		return "draft"
	case "expired":
		return "expired"
	default:
		return state
	}
}

func flattenTaxonomyNodes(nodes []etsy.TaxonomyNode, parentID int64) []marketplace.Category {
	var result []marketplace.Category
	for _, n := range nodes {
		cat := marketplace.Category{
			ID:       strconv.FormatInt(n.ID, 10),
			Name:     n.Name,
			ParentID: strconv.FormatInt(n.ParentID, 10),
			Level:    n.Level,
		}
		if n.ParentID == 0 {
			cat.ParentID = ""
		}
		result = append(result, cat)
		if len(n.Children) > 0 {
			result = append(result, flattenTaxonomyNodes(n.Children, n.ID)...)
		}
	}
	return result
}

func getStringAttr(attrs map[string]interface{}, key, defaultVal string) string {
	if attrs == nil {
		return defaultVal
	}
	if v, ok := attrs[key].(string); ok && v != "" {
		return v
	}
	return defaultVal
}

func getBoolAttr(attrs map[string]interface{}, key string, defaultVal bool) bool {
	if attrs == nil {
		return defaultVal
	}
	if v, ok := attrs[key].(bool); ok {
		return v
	}
	return defaultVal
}

func (a *EtsyAdapter) CancelOrder(ctx context.Context, externalOrderID string) error {
	return marketplace.ErrCancelNotSupported
}
