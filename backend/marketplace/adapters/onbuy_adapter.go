package adapters

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"module-a/marketplace"
	"module-a/marketplace/clients/onbuy"
)

// ============================================================================
// ONBUY MARKETPLACE ADAPTER
// ============================================================================
// Implements marketplace.MarketplaceAdapter for OnBuy.
// Credential fields: consumer_key, consumer_secret, site_id (default 2000)
// Auth: Two-step — POST to /auth/request-token to get short-lived Bearer token.
// External ID is the OnBuy listing_id (string).
// ============================================================================

type OnBuyAdapter struct {
	credentials marketplace.Credentials
	client      *onbuy.Client
}

func NewOnBuyAdapter(ctx context.Context, credentials marketplace.Credentials) (marketplace.MarketplaceAdapter, error) {
	consumerKey := credentials.Data["consumer_key"]
	consumerSecret := credentials.Data["consumer_secret"]
	siteIDStr := credentials.Data["site_id"]

	if consumerKey == "" {
		return nil, fmt.Errorf("consumer_key is required for OnBuy")
	}
	if consumerSecret == "" {
		return nil, fmt.Errorf("consumer_secret is required for OnBuy")
	}

	siteID := 2000 // OnBuy UK default
	if siteIDStr != "" {
		if v, err := strconv.Atoi(siteIDStr); err == nil && v > 0 {
			siteID = v
		}
	}

	client := onbuy.NewClient(consumerKey, consumerSecret, siteID)

	return &OnBuyAdapter{
		credentials: credentials,
		client:      client,
	}, nil
}

func (a *OnBuyAdapter) GetName() string        { return "onbuy" }
func (a *OnBuyAdapter) GetDisplayName() string { return "OnBuy" }

func (a *OnBuyAdapter) GetSupportedFeatures() []string {
	return []string{"import", "listing", "order_sync", "inventory_sync", "price_sync", "tracking"}
}

// ── Connection ────────────────────────────────────────────────────────────────

func (a *OnBuyAdapter) Connect(ctx context.Context, credentials marketplace.Credentials) error {
	return nil
}

func (a *OnBuyAdapter) Disconnect(ctx context.Context) error {
	return nil
}

func (a *OnBuyAdapter) TestConnection(ctx context.Context) error {
	return a.client.TestConnection()
}

func (a *OnBuyAdapter) RefreshAuth(ctx context.Context) error {
	return a.client.RefreshTokenIfNeeded()
}

func (a *OnBuyAdapter) GetConnectionStatus(ctx context.Context) (*marketplace.ConnectionStatus, error) {
	err := a.TestConnection(ctx)
	return &marketplace.ConnectionStatus{
		IsConnected:  err == nil,
		ErrorMessage: func() string {
			if err != nil {
				return err.Error()
			}
			return ""
		}(),
		LastChecked: time.Now(),
	}, nil
}

// ── Product Import ────────────────────────────────────────────────────────────

func (a *OnBuyAdapter) FetchListings(ctx context.Context, filters marketplace.ImportFilters) ([]marketplace.MarketplaceProduct, error) {
	listings, err := a.client.GetProducts(1)
	if err != nil {
		return nil, fmt.Errorf("OnBuy FetchListings: %w", err)
	}

	var result []marketplace.MarketplaceProduct
	for _, l := range listings {
		result = append(result, a.convertListingToProduct(l))
	}
	return result, nil
}

func (a *OnBuyAdapter) FetchProduct(ctx context.Context, externalID string) (*marketplace.MarketplaceProduct, error) {
	// OnBuy does not expose a single-listing GET by ID in V4;
	// retrieve the full page and find by listing_id.
	listings, err := a.client.GetProducts(1)
	if err != nil {
		return nil, fmt.Errorf("OnBuy FetchProduct: %w", err)
	}
	for _, l := range listings {
		if l.ListingID == externalID {
			p := a.convertListingToProduct(l)
			return &p, nil
		}
	}
	return nil, fmt.Errorf("OnBuy FetchProduct: listing %s not found", externalID)
}

func (a *OnBuyAdapter) FetchProductImages(ctx context.Context, externalID string) ([]marketplace.ImageData, error) {
	// OnBuy listings reference product images via the OPC catalogue; no direct image API.
	return []marketplace.ImageData{}, nil
}

func (a *OnBuyAdapter) FetchInventory(ctx context.Context, externalID string) (*marketplace.InventoryLevel, error) {
	p, err := a.FetchProduct(ctx, externalID)
	if err != nil {
		return nil, err
	}
	qty := 0
	if q, ok := p.Attributes["stock"].(float64); ok {
		qty = int(q)
	}
	return &marketplace.InventoryLevel{
		ExternalID: externalID,
		Quantity:   qty,
		UpdatedAt:  time.Now(),
	}, nil
}

// ── Listing Management ────────────────────────────────────────────────────────

func (a *OnBuyAdapter) CreateListing(ctx context.Context, listing marketplace.ListingData) (*marketplace.ListingResult, error) {
	opc := ""
	if v, ok := listing.CustomFields["opc"].(string); ok {
		opc = v
	}
	if opc == "" {
		return nil, fmt.Errorf("OnBuy CreateListing: opc (OnBuy Product Code) is required in custom_fields")
	}

	conditionID := "new"
	if listing.Condition != "" {
		conditionID = listing.Condition
	}

	deliveryTemplateID := 0
	if v, ok := listing.CustomFields["delivery_template_id"].(float64); ok {
		deliveryTemplateID = int(v)
	}

	l := &onbuy.Listing{
		OPC:                opc,
		SiteID:             a.client.SiteID,
		ConditionID:        conditionID,
		Price:              listing.Price,
		Stock:              listing.Quantity,
		DeliveryTemplateID: deliveryTemplateID,
		SKU:                func() string { if v, ok := listing.Attributes["sku"].(string); ok { return v }; return "" }(),
		Description:        listing.Description,
	}

	res, err := a.client.CreateListing(l)
	if err != nil {
		return nil, fmt.Errorf("OnBuy CreateListing: %w", err)
	}

	return &marketplace.ListingResult{
		ExternalID: res.ListingID,
		Status:     "active",
		CreatedAt:  time.Now(),
	}, nil
}

func (a *OnBuyAdapter) UpdateListing(ctx context.Context, externalID string, updates marketplace.ListingData) error {
	payload := map[string]interface{}{
		"price": updates.Price,
		"stock": updates.Quantity,
	}
	if updates.Description != "" {
		payload["description"] = updates.Description
	}
	return a.client.UpdateListing(externalID, payload)
}

func (a *OnBuyAdapter) DeleteListing(ctx context.Context, externalID string) error {
	return a.client.DeleteListing(externalID)
}

func (a *OnBuyAdapter) PublishListing(ctx context.Context, externalID string) error {
	return a.client.UpdateListing(externalID, map[string]interface{}{
		"published": true,
	})
}

func (a *OnBuyAdapter) UnpublishListing(ctx context.Context, externalID string) error {
	return a.client.UpdateListing(externalID, map[string]interface{}{
		"published": false,
	})
}

// ── Bulk Operations ───────────────────────────────────────────────────────────

func (a *OnBuyAdapter) BulkCreateListings(ctx context.Context, listings []marketplace.ListingData) ([]marketplace.ListingResult, error) {
	var results []marketplace.ListingResult
	for _, l := range listings {
		res, err := a.CreateListing(ctx, l)
		if err != nil {
			results = append(results, marketplace.ListingResult{
				ExternalID: "",
				Status:     "error",
				Errors:     []marketplace.ValidationError{{Message: err.Error(), Severity: "error"}},
			})
			continue
		}
		results = append(results, *res)
	}
	return results, nil
}

func (a *OnBuyAdapter) BulkUpdateListings(ctx context.Context, updates []marketplace.ListingUpdate) ([]marketplace.UpdateResult, error) {
	var results []marketplace.UpdateResult
	for _, u := range updates {
		err := a.client.UpdateListing(u.ExternalID, u.Updates)
		if err != nil {
			results = append(results, marketplace.UpdateResult{
				ExternalID: u.ExternalID,
				Success:    false,
				Errors:     []marketplace.ValidationError{{Message: err.Error(), Severity: "error"}},
				UpdatedAt:  time.Now(),
			})
			continue
		}
		results = append(results, marketplace.UpdateResult{
			ExternalID: u.ExternalID,
			Success:    true,
			UpdatedAt:  time.Now(),
		})
	}
	return results, nil
}

// ── Inventory & Pricing Sync ──────────────────────────────────────────────────

func (a *OnBuyAdapter) SyncInventory(ctx context.Context, externalID string, quantity int) error {
	return a.client.UpdateListing(externalID, map[string]interface{}{
		"stock": quantity,
	})
}

func (a *OnBuyAdapter) SyncPrice(ctx context.Context, externalID string, price float64) error {
	return a.client.UpdateListing(externalID, map[string]interface{}{
		"price": price,
	})
}

func (a *OnBuyAdapter) BulkSyncInventory(ctx context.Context, updates map[string]int) error {
	for externalID, qty := range updates {
		if err := a.SyncInventory(ctx, externalID, qty); err != nil {
			return fmt.Errorf("BulkSyncInventory: listing %s: %w", externalID, err)
		}
	}
	return nil
}

func (a *OnBuyAdapter) BulkSyncPrices(ctx context.Context, updates map[string]float64) error {
	for externalID, price := range updates {
		if err := a.SyncPrice(ctx, externalID, price); err != nil {
			return fmt.Errorf("BulkSyncPrices: listing %s: %w", externalID, err)
		}
	}
	return nil
}

// ── Listing Status & Validation ───────────────────────────────────────────────

func (a *OnBuyAdapter) GetListingStatus(ctx context.Context, externalID string) (*marketplace.ListingStatus, error) {
	p, err := a.FetchProduct(ctx, externalID)
	if err != nil {
		return nil, err
	}
	qty := 0
	if q, ok := p.Attributes["stock"].(float64); ok {
		qty = int(q)
	}
	price := 0.0
	if pr, ok := p.Attributes["price"].(float64); ok {
		price = pr
	}
	return &marketplace.ListingStatus{
		ExternalID:  externalID,
		Status:      "active",
		IsActive:    true,
		Quantity:    qty,
		Price:       price,
		LastUpdated: time.Now(),
	}, nil
}

func (a *OnBuyAdapter) ValidateListing(ctx context.Context, listing marketplace.ListingData) (*marketplace.ValidationResult, error) {
	var errs []marketplace.ValidationError
	if listing.Price <= 0 {
		errs = append(errs, marketplace.ValidationError{Field: "price", Message: "price must be greater than 0", Severity: "error"})
	}
	if opc, ok := listing.CustomFields["opc"].(string); !ok || opc == "" {
		errs = append(errs, marketplace.ValidationError{Field: "opc", Message: "OnBuy Product Code (opc) is required", Severity: "error"})
	}
	return &marketplace.ValidationResult{
		IsValid: len(errs) == 0,
		Errors:  errs,
	}, nil
}

func (a *OnBuyAdapter) GetRequiredFields() []marketplace.RequiredField {
	return []marketplace.RequiredField{
		{Name: "opc", Type: "string", Description: "OnBuy Product Code — links your listing to the OnBuy catalogue"},
		{Name: "price", Type: "number", Description: "Listing price (GBP)"},
		{Name: "stock", Type: "number", Description: "Stock quantity"},
		{Name: "condition_id", Type: "string", Description: "Item condition (new / used_like_new / used_very_good / used_good / used_acceptable / refurbished)"},
		{Name: "consumer_key", Type: "string", Description: "OnBuy API Consumer Key"},
		{Name: "consumer_secret", Type: "password", Description: "OnBuy API Consumer Secret"},
	}
}

func (a *OnBuyAdapter) GetCategories(ctx context.Context) ([]marketplace.Category, error) {
	cats, err := a.client.GetCategories(a.client.SiteID)
	if err != nil {
		return nil, err
	}
	var result []marketplace.Category
	for _, c := range cats {
		result = append(result, marketplace.Category{
			ID:       strconv.Itoa(c.CategoryID),
			Name:     c.Name,
			ParentID: strconv.Itoa(c.ParentID),
		})
	}
	return result, nil
}

// ── Internal helpers ──────────────────────────────────────────────────────────

func (a *OnBuyAdapter) convertListingToProduct(l onbuy.Listing) marketplace.MarketplaceProduct {
	return marketplace.MarketplaceProduct{
		ExternalID: l.ListingID,
		SKU:        l.SKU,
		Title:      l.OPC, // OPC is the catalogue reference; title not returned from listing endpoint
		Description: l.Description,
		Price:      l.Price,
		Quantity:   l.Stock,
		IsInStock:  l.Stock > 0,
		Attributes: map[string]interface{}{
			"opc":                 l.OPC,
			"condition_id":        l.ConditionID,
			"delivery_template_id": l.DeliveryTemplateID,
			"price":               l.Price,
			"stock":               float64(l.Stock),
		},
	}
}

func (a *OnBuyAdapter) CancelOrder(ctx context.Context, externalOrderID string) error {
	return marketplace.ErrCancelNotSupported
}
