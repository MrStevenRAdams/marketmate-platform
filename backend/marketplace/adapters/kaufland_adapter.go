package adapters

import (
	"context"
	"fmt"
	"time"

	"module-a/marketplace"
	"module-a/marketplace/clients/kaufland"
)

// ============================================================================
// KAUFLAND MARKETPLACE ADAPTER
// ============================================================================
// Implements marketplace.MarketplaceAdapter for Kaufland Germany.
// Credential fields: client_key, secret_key
// Auth: HMAC-SHA256 per-request signature
// ============================================================================

type KauflandAdapter struct {
	credentials marketplace.Credentials
	client      *kaufland.Client
}

func NewKauflandAdapter(ctx context.Context, credentials marketplace.Credentials) (marketplace.MarketplaceAdapter, error) {
	clientKey := credentials.Data["client_key"]
	secretKey := credentials.Data["secret_key"]

	if clientKey == "" {
		return nil, fmt.Errorf("client_key is required for Kaufland")
	}
	if secretKey == "" {
		return nil, fmt.Errorf("secret_key is required for Kaufland")
	}

	client := kaufland.NewClient(clientKey, secretKey)

	return &KauflandAdapter{
		credentials: credentials,
		client:      client,
	}, nil
}

// ── Connection ────────────────────────────────────────────────────────────────

func (a *KauflandAdapter) Connect(ctx context.Context, credentials marketplace.Credentials) error {
	return nil
}

func (a *KauflandAdapter) Disconnect(ctx context.Context) error {
	return nil
}

func (a *KauflandAdapter) TestConnection(ctx context.Context) error {
	return a.client.TestConnection()
}

func (a *KauflandAdapter) RefreshAuth(ctx context.Context) error {
	// Kaufland uses per-request HMAC — no token refresh needed
	return nil
}

func (a *KauflandAdapter) GetConnectionStatus(ctx context.Context) (*marketplace.ConnectionStatus, error) {
	err := a.TestConnection(ctx)
	return &marketplace.ConnectionStatus{
		IsConnected:  err == nil,
		ErrorMessage: func() string { if err != nil { return err.Error() }; return "" }(),
		LastChecked:  time.Now(),
	}, nil
}

// ── Product Import ────────────────────────────────────────────────────────────

func (a *KauflandAdapter) FetchListings(ctx context.Context, filters marketplace.ImportFilters) ([]marketplace.MarketplaceProduct, error) {
	units, err := a.client.GetAllUnits()
	if err != nil {
		return nil, fmt.Errorf("Kaufland FetchListings: %w", err)
	}

	var result []marketplace.MarketplaceProduct
	for _, u := range units {
		mp := convertKauflandUnit(u)
		result = append(result, mp)
		if filters.ProductCallback != nil {
			if !filters.ProductCallback(mp) {
				break
			}
		}
	}
	return result, nil
}

func (a *KauflandAdapter) FetchProduct(ctx context.Context, externalID string) (*marketplace.MarketplaceProduct, error) {
	unit, err := a.client.GetUnit(externalID)
	if err != nil {
		return nil, fmt.Errorf("Kaufland FetchProduct %s: %w", externalID, err)
	}
	mp := convertKauflandUnit(*unit)
	return &mp, nil
}

func (a *KauflandAdapter) FetchProductImages(ctx context.Context, externalID string) ([]marketplace.ImageData, error) {
	// Kaufland units don't carry image URLs through the listing API
	return []marketplace.ImageData{}, nil
}

func (a *KauflandAdapter) FetchInventory(ctx context.Context, externalID string) (*marketplace.InventoryLevel, error) {
	unit, err := a.client.GetUnit(externalID)
	if err != nil {
		return nil, fmt.Errorf("Kaufland FetchInventory %s: %w", externalID, err)
	}
	return &marketplace.InventoryLevel{
		ExternalID: externalID,
		Quantity:   unit.Amount,
	}, nil
}

// ── Listing Management ────────────────────────────────────────────────────────

func (a *KauflandAdapter) CreateListing(ctx context.Context, listing marketplace.ListingData) (*marketplace.ListingResult, error) {
	req := kaufland.CreateUnitRequest{
		EAN:                listing.Identifiers.EAN,
		ListingPriceAmount: listing.Price,
		Amount:             listing.Quantity,
		ConditionID:        1, // 1 = new
	}

	if listing.Description != "" {
		req.Note = listing.Description
	}

	if listing.Attributes != nil {
		if cond, ok := listing.Attributes["condition_id"].(float64); ok {
			req.ConditionID = int(cond)
		}
		if sg, ok := listing.Attributes["shipping_group"].(string); ok {
			req.ShippingGroup = sg
		}
		if h, ok := listing.Attributes["handling_time_in_days"].(float64); ok {
			req.Handling = int(h)
		}
		if mp, ok := listing.Attributes["minimum_price"].(float64); ok {
			req.MinimumPriceAmount = mp
		}
	}

	unit, err := a.client.CreateUnit(req)
	if err != nil {
		return nil, fmt.Errorf("Kaufland CreateListing: %w", err)
	}

	return &marketplace.ListingResult{
		ExternalID: unit.UnitID,
		Status:     unit.Status,
		URL:        fmt.Sprintf("https://www.kaufland.de/product/%s", unit.EAN),
	}, nil
}

func (a *KauflandAdapter) UpdateListing(ctx context.Context, externalID string, updates marketplace.ListingData) error {
	req := kaufland.UpdateUnitRequest{}
	if updates.Price > 0 {
		req.ListingPriceAmount = updates.Price
	}
	if updates.Quantity > 0 {
		req.Amount = updates.Quantity
	}
	if updates.Attributes != nil {
		if sg, ok := updates.Attributes["shipping_group"].(string); ok {
			req.ShippingGroup = sg
		}
		if h, ok := updates.Attributes["handling_time_in_days"].(float64); ok {
			req.Handling = int(h)
		}
	}
	return a.client.UpdateUnit(externalID, req)
}

func (a *KauflandAdapter) DeleteListing(ctx context.Context, externalID string) error {
	return a.client.DeleteUnit(externalID)
}

func (a *KauflandAdapter) PublishListing(ctx context.Context, externalID string) error {
	// Kaufland units become active once stock > 0; set amount to 1 if currently 0
	unit, err := a.client.GetUnit(externalID)
	if err != nil {
		return err
	}
	if unit.Amount == 0 {
		return a.client.UpdateUnitStock(externalID, 1)
	}
	return nil
}

func (a *KauflandAdapter) UnpublishListing(ctx context.Context, externalID string) error {
	// Setting amount to 0 deactivates the unit
	return a.client.UpdateUnitStock(externalID, 0)
}

// ── Bulk ──────────────────────────────────────────────────────────────────────

func (a *KauflandAdapter) BulkCreateListings(ctx context.Context, listings []marketplace.ListingData) ([]marketplace.ListingResult, error) {
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

func (a *KauflandAdapter) BulkUpdateListings(ctx context.Context, updates []marketplace.ListingUpdate) ([]marketplace.UpdateResult, error) {
	var results []marketplace.UpdateResult
	for _, u := range updates {
		listing := marketplace.ListingData{
			Price:    toKauflandFloat64(u.Updates["price"]),
			Quantity: toKauflandInt(u.Updates["quantity"]),
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

func (a *KauflandAdapter) GetListingStatus(ctx context.Context, externalID string) (*marketplace.ListingStatus, error) {
	unit, err := a.client.GetUnit(externalID)
	if err != nil {
		return nil, err
	}
	return &marketplace.ListingStatus{ExternalID: externalID, Status: unit.Status}, nil
}

func (a *KauflandAdapter) SyncInventory(ctx context.Context, externalID string, quantity int) error {
	return a.client.UpdateUnitStock(externalID, quantity)
}

func (a *KauflandAdapter) SyncPrice(ctx context.Context, externalID string, price float64) error {
	return a.client.UpdateUnitPrice(externalID, price)
}

// ── Metadata ──────────────────────────────────────────────────────────────────

func (a *KauflandAdapter) GetName() string        { return "kaufland" }
func (a *KauflandAdapter) GetDisplayName() string { return "Kaufland" }

func (a *KauflandAdapter) GetSupportedFeatures() []string {
	return []string{"import", "listing", "order_sync", "inventory_sync", "price_sync", "tracking"}
}

func (a *KauflandAdapter) GetRequiredFields() []marketplace.RequiredField {
	return []marketplace.RequiredField{
		{Name: "client_key", Type: "string", Description: "Kaufland Seller API Client Key"},
		{Name: "secret_key", Type: "password", Description: "Kaufland Seller API Secret Key"},
	}
}

func (a *KauflandAdapter) GetCategories(ctx context.Context) ([]marketplace.Category, error) {
	cats, err := a.client.GetCategories()
	if err != nil {
		return nil, err
	}
	var result []marketplace.Category
	for _, cat := range cats {
		result = append(result, marketplace.Category{
			ID:   fmt.Sprintf("%d", cat.CategoryID),
			Name: cat.Title,
		})
	}
	return result, nil
}

func (a *KauflandAdapter) ValidateListing(ctx context.Context, listing marketplace.ListingData) (*marketplace.ValidationResult, error) {
	result := &marketplace.ValidationResult{IsValid: true}
	if listing.Identifiers.EAN == "" {
		result.IsValid = false
		result.Errors = append(result.Errors, marketplace.ValidationError{Field: "ean", Message: "EAN is required for Kaufland listings"})
	}
	if listing.Price <= 0 {
		result.IsValid = false
		result.Errors = append(result.Errors, marketplace.ValidationError{Field: "price", Message: "Listing price must be greater than 0"})
	}
	if listing.Quantity < 0 {
		result.IsValid = false
		result.Errors = append(result.Errors, marketplace.ValidationError{Field: "quantity", Message: "Stock amount cannot be negative"})
	}
	return result, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func convertKauflandUnit(u kaufland.Unit) marketplace.MarketplaceProduct {
	mp := marketplace.MarketplaceProduct{
		ExternalID: u.UnitID,
		Title:      u.EAN, // Kaufland units are EAN-based; title comes from product catalogue
		SKU:        u.EAN,
		Price:      u.ListingPriceAmount,
		Quantity:   u.Amount,
	}
	mp.RawData = map[string]interface{}{
		"unit_id":       u.UnitID,
		"ean":           u.EAN,
		"condition":     u.ConditionID,
		"listing_price": u.ListingPriceAmount,
		"minimum_price": u.MinimumPriceAmount,
		"amount":        u.Amount,
		"status":        u.Status,
		"shipping_group": u.ShippingGroup,
		"handling_time": u.Handling,
	}
	return mp
}

func toKauflandFloat64(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case string:
		f := 0.0
		fmt.Sscanf(n, "%f", &f)
		return f
	}
	return 0
}

func toKauflandInt(v interface{}) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case string:
		i := 0
		fmt.Sscanf(n, "%d", &i)
		return i
	}
	return 0
}

func (a *KauflandAdapter) CancelOrder(ctx context.Context, externalOrderID string) error {
	return marketplace.ErrCancelNotSupported
}
