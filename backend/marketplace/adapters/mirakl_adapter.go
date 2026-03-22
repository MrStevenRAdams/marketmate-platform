package adapters

// ============================================================================
// MIRAKL GENERIC MARKETPLACE ADAPTER
// ============================================================================
// Implements marketplace.MarketplaceAdapter for ANY Mirakl-powered marketplace.
//
// One adapter type, many marketplace instances. The marketplace is identified
// by the "marketplace_id" field in credentials, which maps to a known Mirakl
// instance URL (or a custom "base_url" can be provided).
//
// Supported via this single adapter:
//   UK:  tesco, bandq, superdrug, debenhams, decathlon_uk, mountain_warehouse,
//        jd_sports
//   EU:  carrefour, decathlon_fr, fnac_darty, leroy_merlin, mediamarkt
//   US:  macys, lowes
//   Other: asos
//
// Required credential fields:
//   api_key        — Seller API key from Mirakl portal (Profile → API Key)
//   marketplace_id — One of the known IDs above (e.g. "tesco", "bandq")
//   base_url       — Optional: override if using a custom Mirakl instance
//   shop_id        — Optional: required only for multi-shop accounts
// ============================================================================

import (
	"context"
	"fmt"
	"time"

	"module-a/marketplace"
	"module-a/marketplace/clients/mirakl"
)

// MiraklAdapter is the generic adapter for all Mirakl-powered marketplaces.
type MiraklAdapter struct {
	credentials   marketplace.Credentials
	client        *mirakl.Client
	marketplaceID string // e.g. "tesco", "bandq"
	displayName   string
}

// NewMiraklAdapter creates a new Mirakl adapter from credentials.
// This is the factory function registered with marketplace.Register for each
// Mirakl-powered marketplace ID.
func NewMiraklAdapter(ctx context.Context, credentials marketplace.Credentials) (marketplace.MarketplaceAdapter, error) {
	apiKey := credentials.Data["api_key"]
	if apiKey == "" {
		return nil, fmt.Errorf("mirakl: api_key is required")
	}

	marketplaceID := credentials.Data["marketplace_id"]
	if marketplaceID == "" {
		// Fall back to the credential's marketplace ID
		marketplaceID = credentials.MarketplaceID
	}

	baseURL := credentials.Data["base_url"]
	shopID := credentials.Data["shop_id"]

	client := mirakl.NewClientForMarketplace(marketplaceID, apiKey, shopID, baseURL)

	displayName := credentials.Data["display_name"]
	if displayName == "" {
		if inst, ok := mirakl.KnownInstances[marketplaceID]; ok {
			displayName = inst.DisplayName
		} else {
			displayName = "Mirakl Marketplace"
		}
	}

	return &MiraklAdapter{
		credentials:   credentials,
		client:        client,
		marketplaceID: marketplaceID,
		displayName:   displayName,
	}, nil
}

// ============================================================================
// CONNECTION & AUTHENTICATION
// ============================================================================

func (a *MiraklAdapter) Connect(ctx context.Context, credentials marketplace.Credentials) error {
	return nil
}

func (a *MiraklAdapter) Disconnect(ctx context.Context) error {
	return nil
}

// TestConnection calls V01 health check to verify credentials and connectivity.
func (a *MiraklAdapter) TestConnection(ctx context.Context) error {
	if err := a.client.HealthCheck(); err != nil {
		return fmt.Errorf("mirakl connection test failed for %s: %w", a.marketplaceID, err)
	}
	return nil
}

// RefreshAuth — Mirakl uses static API keys, no token refresh needed.
func (a *MiraklAdapter) RefreshAuth(ctx context.Context) error {
	return nil
}

func (a *MiraklAdapter) GetConnectionStatus(ctx context.Context) (*marketplace.ConnectionStatus, error) {
	err := a.TestConnection(ctx)
	connected := err == nil
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}
	return &marketplace.ConnectionStatus{
		IsConnected:  connected,
		ErrorMessage: errMsg,
		LastChecked:  time.Now(),
	}, nil
}

// ============================================================================
// PRODUCT IMPORT — Mirakl → PIM
// ============================================================================

// FetchListings retrieves the seller's current active offers from Mirakl.
func (a *MiraklAdapter) FetchListings(ctx context.Context, filters marketplace.ImportFilters) ([]marketplace.MarketplaceProduct, error) {
	var products []marketplace.MarketplaceProduct
	offset := 0
	for {
		resp, err := a.client.ListOffers(mirakl.ListOffersOptions{
			Max:    100,
			Offset: offset,
		})
		if err != nil {
			return nil, fmt.Errorf("FetchListings: %w", err)
		}
		for _, offer := range resp.Offers {
			products = append(products, marketplace.MarketplaceProduct{
				ExternalID: offer.OfferID,
				SKU:        offer.ShopSKU,
				Title:      offer.ProductTitle,
				Price:      offer.Price,
				Quantity:   offer.Quantity,
				IsInStock:  offer.Quantity > 0,
				Identifiers: marketplace.Identifiers{},
			})
		}
		if len(products) >= resp.TotalCount || len(resp.Offers) == 0 {
			break
		}
		offset += len(resp.Offers)
	}
	return products, nil
}

// FetchProduct retrieves a single offer by offer ID.
func (a *MiraklAdapter) FetchProduct(ctx context.Context, externalID string) (*marketplace.MarketplaceProduct, error) {
	offer, err := a.client.GetOffer(externalID)
	if err != nil {
		return nil, fmt.Errorf("FetchProduct: %w", err)
	}
	return &marketplace.MarketplaceProduct{
		ExternalID: offer.OfferID,
		SKU:        offer.ShopSKU,
		Title:      offer.ProductTitle,
		Price:      offer.Price,
		Quantity:   offer.Quantity,
		IsInStock:  offer.Quantity > 0,
		Identifiers: marketplace.Identifiers{},
	}, nil
}

// FetchProductImages — Mirakl does not expose per-offer image retrieval via seller API.
func (a *MiraklAdapter) FetchProductImages(ctx context.Context, externalID string) ([]marketplace.ImageData, error) {
	return nil, nil
}

// FetchInventory returns the current stock quantity for a given offer.
func (a *MiraklAdapter) FetchInventory(ctx context.Context, externalID string) (*marketplace.InventoryLevel, error) {
	offer, err := a.client.GetOffer(externalID)
	if err != nil {
		return nil, fmt.Errorf("FetchInventory: %w", err)
	}
	return &marketplace.InventoryLevel{
		ExternalID: offer.OfferID,
		Quantity:   offer.Quantity,
	}, nil
}

// ============================================================================
// LISTING MANAGEMENT — PIM → Mirakl
// ============================================================================

// CreateListing creates a product in the Mirakl catalog AND creates an offer
// (price + stock) for it in a single API call using P41 (products import).
func (a *MiraklAdapter) CreateListing(ctx context.Context, listing marketplace.ListingData) (*marketplace.ListingResult, error) {
	// Validate first
	vr, err := a.ValidateListing(ctx, listing)
	if err != nil {
		return nil, fmt.Errorf("CreateListing: validation: %w", err)
	}
	if !vr.IsValid {
		errs := make([]marketplace.ValidationError, len(vr.Errors))
		copy(errs, vr.Errors)
		return &marketplace.ListingResult{
			Status:    "error",
			Errors:    errs,
			CreatedAt: time.Now(),
		}, nil
	}

	// Build product payload
	payload := mirakl.ProductPayload{
		ShopSKU:      listing.ProductID,
		Title:        listing.Title,
		Description:  listing.Description,
		CategoryCode: listing.CategoryID,
		Price:        listing.Price,
		Quantity:     listing.Quantity,
		State:        "11", // "11" = new condition
	}

	if listing.Attributes != nil {
		if brand, ok := listing.Attributes["brand"].(string); ok {
			payload.Brand = brand
		}
	}

	for i, imgURL := range listing.Images {
		if i >= 8 {
			break // Mirakl typically allows 8 images
		}
		payload.MediaURLs = append(payload.MediaURLs, imgURL)
	}

	// Add EAN/GTIN as attribute if present
	var attrs []mirakl.ProductAttribute
	if listing.Identifiers.EAN != "" {
		attrs = append(attrs, mirakl.ProductAttribute{
			Code:  "ean",
			Value: []string{listing.Identifiers.EAN},
		})
	}
	if listing.Identifiers.GTIN != "" {
		attrs = append(attrs, mirakl.ProductAttribute{
			Code:  "gtin",
			Value: []string{listing.Identifiers.GTIN},
		})
	}
	// Pass through any extra attributes from custom fields
	if listing.CustomFields != nil {
		for k, v := range listing.CustomFields {
			if strVal, ok := v.(string); ok && strVal != "" {
				attrs = append(attrs, mirakl.ProductAttribute{
					Code:  k,
					Value: []string{strVal},
				})
			}
		}
	}
	payload.Attributes = attrs

	// Submit product import
	importResp, err := a.client.ImportProducts(mirakl.ProductImportRequest{
		Products: []mirakl.ProductPayload{payload},
	})
	if err != nil {
		return nil, fmt.Errorf("CreateListing: %w", err)
	}

	return &marketplace.ListingResult{
		ExternalID: importResp.ImportID, // Mirakl uses import ID as the job reference
		SKU:        listing.ProductID,
		Status:     "pending", // Mirakl processes imports asynchronously
		CreatedAt:  time.Now(),
	}, nil
}

// UpdateListing updates an existing offer's price and/or quantity via OF24.
func (a *MiraklAdapter) UpdateListing(ctx context.Context, externalID string, updates marketplace.ListingData) error {
	upsert := mirakl.OfferUpsert{
		ShopSKU:      updates.ProductID,
		UpdateDelete: "update",
		Price:        updates.Price,
		Quantity:     updates.Quantity,
	}
	if _, err := a.client.UpsertOffers([]mirakl.OfferUpsert{upsert}); err != nil {
		return fmt.Errorf("UpdateListing: %w", err)
	}
	return nil
}

// DeleteListing removes an offer by setting update-delete = "delete".
func (a *MiraklAdapter) DeleteListing(ctx context.Context, externalID string) error {
	upsert := mirakl.OfferUpsert{
		ShopSKU:      externalID,
		UpdateDelete: "delete",
	}
	if _, err := a.client.UpsertOffers([]mirakl.OfferUpsert{upsert}); err != nil {
		return fmt.Errorf("DeleteListing: %w", err)
	}
	return nil
}

// PublishListing re-activates a previously suspended offer.
// In Mirakl, active/inactive is controlled via the offer quantity — setting
// quantity > 0 effectively publishes it.
func (a *MiraklAdapter) PublishListing(ctx context.Context, externalID string) error {
	return nil // Mirakl auto-activates offers with quantity > 0
}

// UnpublishListing sets quantity to 0, which hides it from the storefront.
func (a *MiraklAdapter) UnpublishListing(ctx context.Context, externalID string) error {
	return a.SyncInventory(ctx, externalID, 0)
}

// ============================================================================
// BULK OPERATIONS
// ============================================================================

func (a *MiraklAdapter) BulkCreateListings(ctx context.Context, listings []marketplace.ListingData) ([]marketplace.ListingResult, error) {
	var results []marketplace.ListingResult
	for _, l := range listings {
		r, err := a.CreateListing(ctx, l)
		if err != nil {
			results = append(results, marketplace.ListingResult{
				Status:    "error",
				Errors:    []marketplace.ValidationError{{Message: err.Error(), Severity: "error"}},
				CreatedAt: time.Now(),
			})
		} else {
			results = append(results, *r)
		}
	}
	return results, nil
}

func (a *MiraklAdapter) BulkUpdateListings(ctx context.Context, updates []marketplace.ListingUpdate) ([]marketplace.UpdateResult, error) {
	var upserts []mirakl.OfferUpsert
	for _, u := range updates {
		upsert := mirakl.OfferUpsert{
			ShopSKU:      u.ExternalID,
			UpdateDelete: "update",
		}
		if p, ok := u.Updates["price"].(float64); ok {
			upsert.Price = p
		}
		if q, ok := u.Updates["quantity"].(int); ok {
			upsert.Quantity = q
		}
		upserts = append(upserts, upsert)
	}

	if len(upserts) == 0 {
		return nil, nil
	}

	_, err := a.client.UpsertOffers(upserts)
	results := make([]marketplace.UpdateResult, len(updates))
	for i, u := range updates {
		results[i] = marketplace.UpdateResult{
			ExternalID: u.ExternalID,
			Success:    err == nil,
			UpdatedAt:  time.Now(),
		}
		if err != nil {
			results[i].Errors = []marketplace.ValidationError{{Message: err.Error(), Severity: "error"}}
		}
	}
	return results, nil
}

// ============================================================================
// SYNC & MONITORING
// ============================================================================

func (a *MiraklAdapter) GetListingStatus(ctx context.Context, externalID string) (*marketplace.ListingStatus, error) {
	offer, err := a.client.GetOffer(externalID)
	if err != nil {
		return nil, fmt.Errorf("GetListingStatus: %w", err)
	}
	return &marketplace.ListingStatus{
		ExternalID:  offer.OfferID,
		Status:      map[bool]string{true: "active", false: "inactive"}[offer.Active],
		IsActive:    offer.Active,
		Quantity:    offer.Quantity,
		Price:       offer.Price,
		LastUpdated: time.Now(),
	}, nil
}

// SyncInventory updates the offer quantity via OF24.
func (a *MiraklAdapter) SyncInventory(ctx context.Context, externalID string, quantity int) error {
	if err := a.client.UpdateStock(externalID, quantity); err != nil {
		return fmt.Errorf("SyncInventory: %w", err)
	}
	return nil
}

// SyncPrice updates the offer price via OF24.
func (a *MiraklAdapter) SyncPrice(ctx context.Context, externalID string, price float64) error {
	if err := a.client.UpdatePrice(externalID, price); err != nil {
		return fmt.Errorf("SyncPrice: %w", err)
	}
	return nil
}

// ============================================================================
// METADATA
// ============================================================================

func (a *MiraklAdapter) GetName() string {
	return a.marketplaceID
}

func (a *MiraklAdapter) GetDisplayName() string {
	return a.displayName
}

func (a *MiraklAdapter) GetSupportedFeatures() []string {
	return []string{
		"listing",
		"order_sync",
		"tracking",
		"inventory_sync",
		"price_sync",
		"bulk_update",
		"cancellation",
		"refunds",
	}
}

func (a *MiraklAdapter) GetRequiredFields() []marketplace.RequiredField {
	return []marketplace.RequiredField{
		{Name: "product_id", Type: "string", Description: "Your unique Shop SKU"},
		{Name: "title", Type: "string", Description: "Product title"},
		{Name: "category_id", Type: "string", Description: "Mirakl category code from the marketplace category tree"},
		{Name: "price", Type: "number", Description: "Selling price in marketplace currency"},
		{Name: "quantity", Type: "number", Description: "Available stock quantity"},
	}
}

func (a *MiraklAdapter) GetCategories(ctx context.Context) ([]marketplace.Category, error) {
	cats, err := a.client.GetCategories()
	if err != nil {
		return nil, fmt.Errorf("GetCategories: %w", err)
	}
	result := make([]marketplace.Category, 0, len(cats))
	for _, c := range cats {
		result = append(result, marketplace.Category{
			ID:       c.Code,
			Name:     c.Label,
			ParentID: c.ParentCode,
			Level:    c.Level,
		})
	}
	return result, nil
}

func (a *MiraklAdapter) ValidateListing(ctx context.Context, listing marketplace.ListingData) (*marketplace.ValidationResult, error) {
	var errs []marketplace.ValidationError

	if listing.ProductID == "" {
		errs = append(errs, marketplace.ValidationError{
			Code:        "MISSING_SKU",
			Field:       "product_id",
			Message:     "Shop SKU is required",
			Severity:    "error",
			Remediation: "Set product_id to your unique Shop SKU",
		})
	}
	if listing.Title == "" {
		errs = append(errs, marketplace.ValidationError{
			Code:     "MISSING_TITLE",
			Field:    "title",
			Message:  "Product title is required",
			Severity: "error",
		})
	}
	if listing.Price <= 0 {
		errs = append(errs, marketplace.ValidationError{
			Code:     "INVALID_PRICE",
			Field:    "price",
			Message:  "Price must be greater than 0",
			Severity: "error",
		})
	}
	if listing.CategoryID == "" {
		errs = append(errs, marketplace.ValidationError{
			Code:        "MISSING_CATEGORY",
			Field:       "category_id",
			Message:     "Mirakl category code is required",
			Severity:    "error",
			Remediation: "Fetch categories from /mirakl/categories to find the correct code",
		})
	}

	return &marketplace.ValidationResult{
		IsValid:  len(errs) == 0,
		Errors:   errs,
	}, nil
}

// ============================================================================
// MIRAKL-SPECIFIC FACTORY FUNCTIONS
// ============================================================================
// One named factory per marketplace — allows each to be registered separately
// in main.go with its own AdapterMetadata while sharing the same implementation.

func newMiraklAdapterForID(marketplaceID string) func(context.Context, marketplace.Credentials) (marketplace.MarketplaceAdapter, error) {
	return func(ctx context.Context, creds marketplace.Credentials) (marketplace.MarketplaceAdapter, error) {
		// Inject the marketplace_id so NewMiraklAdapter knows which instance to use
		if creds.Data == nil {
			creds.Data = map[string]string{}
		}
		if creds.Data["marketplace_id"] == "" {
			creds.Data["marketplace_id"] = marketplaceID
		}
		return NewMiraklAdapter(ctx, creds)
	}
}

// Individual named factories registered in main.go:
var (
	NewTescoAdapter          = newMiraklAdapterForID("tesco")
	NewBandQAdapter          = newMiraklAdapterForID("bandq")
	NewSuperdrugAdapter      = newMiraklAdapterForID("superdrug")
	NewDebenhamsAdapter      = newMiraklAdapterForID("debenhams")
	NewDecathlonUKAdapter    = newMiraklAdapterForID("decathlon_uk")
	NewMountainWarehouseAdapter = newMiraklAdapterForID("mountain_warehouse")
	NewJDSportsAdapter       = newMiraklAdapterForID("jd_sports")
	NewCarrefourAdapter      = newMiraklAdapterForID("carrefour")
	NewDecathlonFRAdapter    = newMiraklAdapterForID("decathlon_fr")
	NewFnacDartyAdapter      = newMiraklAdapterForID("fnac_darty")
	NewLeroyMerlinAdapter    = newMiraklAdapterForID("leroy_merlin")
	NewMediaMarktAdapter     = newMiraklAdapterForID("mediamarkt")
	NewASOSAdapter           = newMiraklAdapterForID("asos")
	NewMacysAdapter          = newMiraklAdapterForID("macys")
	NewLowesAdapter          = newMiraklAdapterForID("lowes")
)

func (a *MiraklAdapter) CancelOrder(ctx context.Context, externalOrderID string) error {
	return marketplace.ErrCancelNotSupported
}
