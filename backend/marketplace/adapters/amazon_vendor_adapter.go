package adapters

import (
	"context"
	"fmt"
	"time"

	"module-a/marketplace"
	"module-a/marketplace/clients/amazon"
)

// ============================================================================
// AMAZON VENDOR CENTRAL ADAPTER
// ============================================================================
// Destination path:
//   platform/backend/marketplace/adapters/amazon_vendor_adapter.go
//
// Amazon Vendor Central (AVC) is a separate relationship from Seller Central.
// In AVC, Amazon is the buyer and you are the supplier. Amazon sends purchase
// orders which you must accept or reject via the Vendor Orders SP-API.
//
// This adapter implements the MarketplaceAdapter interface. Most methods
// return "not supported" errors because Vendor Central does not have listings,
// inventory sync, or pricing in the seller sense — it only has vendor orders.
//
// The adapter allows the user to connect their Vendor Central account via
// Marketplace Connections using their Vendor Central LWA credentials.
// ============================================================================

type AmazonVendorAdapter struct {
	client      *amazon.SPAPIClient
	credentials marketplace.Credentials
}

func NewAmazonVendorAdapter(ctx context.Context, credentials marketplace.Credentials) (marketplace.MarketplaceAdapter, error) {
	config := &amazon.SPAPIConfig{
		LWAClientID:        credentials.Data["lwa_client_id"],
		LWAClientSecret:    credentials.Data["lwa_client_secret"],
		RefreshToken:       credentials.Data["refresh_token"],
		AWSAccessKeyID:     credentials.Data["aws_access_key_id"],
		AWSSecretAccessKey: credentials.Data["aws_secret_access_key"],
		MarketplaceID:      credentials.Data["marketplace_id"],
		Region:             credentials.Data["region"],
		SellerID:           credentials.Data["vendor_id"], // Vendor uses vendor_id
		IsSandbox:          credentials.Environment == "sandbox",
	}

	if config.MarketplaceID == "" {
		config.MarketplaceID = "A1F83G8C2ARO7P" // Amazon UK default
	}
	if config.Region == "" {
		config.Region = "eu-west-1"
	}

	client, err := amazon.NewSPAPIClient(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Amazon Vendor SP-API client: %w", err)
	}

	return &AmazonVendorAdapter{
		client:      client,
		credentials: credentials,
	}, nil
}

// ── marketplace.MarketplaceAdapter interface ──────────────────────────────────

func (a *AmazonVendorAdapter) Connect(ctx context.Context, credentials marketplace.Credentials) error {
	return nil
}

func (a *AmazonVendorAdapter) Disconnect(ctx context.Context) error {
	return nil
}

// TestConnection verifies the credentials are valid by attempting to fetch
// a single vendor order. A valid credential returns orders (or an empty list);
// an invalid one returns an auth error.
func (a *AmazonVendorAdapter) TestConnection(ctx context.Context) error {
	// Fetch POs from the last 7 days with a limit of 1 — lightweight auth check
	since := time.Now().AddDate(0, 0, -7)
	_, err := a.client.GetVendorOrders(ctx, since, "")
	if err != nil {
		return fmt.Errorf("Amazon Vendor Central connection test failed: %w", err)
	}
	return nil
}

func (a *AmazonVendorAdapter) RefreshAuth(ctx context.Context) error {
	return nil
}

func (a *AmazonVendorAdapter) GetConnectionStatus(ctx context.Context) (*marketplace.ConnectionStatus, error) {
	status := &marketplace.ConnectionStatus{
		IsConnected: true,
		LastChecked: time.Now(),
	}
	if err := a.TestConnection(ctx); err != nil {
		status.IsConnected = false
		status.ErrorMessage = err.Error()
	} else {
		status.LastSuccessful = time.Now()
	}
	return status, nil
}

// FetchListings returns empty — Vendor Central has no seller listings.
func (a *AmazonVendorAdapter) FetchListings(ctx context.Context, filters marketplace.ImportFilters) ([]marketplace.MarketplaceProduct, error) {
	return []marketplace.MarketplaceProduct{}, nil
}

func (a *AmazonVendorAdapter) FetchProduct(ctx context.Context, externalID string) (*marketplace.MarketplaceProduct, error) {
	return nil, fmt.Errorf("FetchProduct not supported for Amazon Vendor Central")
}

func (a *AmazonVendorAdapter) FetchProductImages(ctx context.Context, externalID string) ([]marketplace.ImageData, error) {
	return nil, nil
}

func (a *AmazonVendorAdapter) FetchInventory(ctx context.Context, externalID string) (*marketplace.InventoryLevel, error) {
	return nil, fmt.Errorf("FetchInventory not supported for Amazon Vendor Central")
}

func (a *AmazonVendorAdapter) CreateListing(ctx context.Context, listing marketplace.ListingData) (*marketplace.ListingResult, error) {
	return nil, fmt.Errorf("CreateListing not supported for Amazon Vendor Central")
}

func (a *AmazonVendorAdapter) UpdateListing(ctx context.Context, externalID string, updates marketplace.ListingData) error {
	return fmt.Errorf("UpdateListing not supported for Amazon Vendor Central")
}

func (a *AmazonVendorAdapter) DeleteListing(ctx context.Context, externalID string) error {
	return fmt.Errorf("DeleteListing not supported for Amazon Vendor Central")
}

func (a *AmazonVendorAdapter) PublishListing(ctx context.Context, externalID string) error {
	return fmt.Errorf("PublishListing not supported for Amazon Vendor Central")
}

func (a *AmazonVendorAdapter) UnpublishListing(ctx context.Context, externalID string) error {
	return fmt.Errorf("UnpublishListing not supported for Amazon Vendor Central")
}

func (a *AmazonVendorAdapter) BulkCreateListings(ctx context.Context, listings []marketplace.ListingData) ([]marketplace.ListingResult, error) {
	return nil, fmt.Errorf("BulkCreateListings not supported for Amazon Vendor Central")
}

func (a *AmazonVendorAdapter) BulkUpdateListings(ctx context.Context, updates []marketplace.ListingUpdate) ([]marketplace.UpdateResult, error) {
	return nil, fmt.Errorf("BulkUpdateListings not supported for Amazon Vendor Central")
}

func (a *AmazonVendorAdapter) GetListingStatus(ctx context.Context, sku string) (*marketplace.ListingStatus, error) {
	return nil, fmt.Errorf("GetListingStatus not supported for Amazon Vendor Central")
}

func (a *AmazonVendorAdapter) SyncInventory(ctx context.Context, externalID string, quantity int) error {
	return fmt.Errorf("SyncInventory not supported for Amazon Vendor Central")
}

func (a *AmazonVendorAdapter) SyncPrice(ctx context.Context, externalID string, price float64) error {
	return fmt.Errorf("SyncPrice not supported for Amazon Vendor Central")
}

func (a *AmazonVendorAdapter) GetName() string {
	return "amazon_vendor"
}

func (a *AmazonVendorAdapter) GetDisplayName() string {
	return "Amazon Vendor Central"
}

func (a *AmazonVendorAdapter) GetSupportedFeatures() []string {
	return []string{"vendor_orders"}
}

func (a *AmazonVendorAdapter) GetRequiredFields() []marketplace.RequiredField {
	return []marketplace.RequiredField{
		{Name: "lwa_client_id", Type: "string", Description: "LWA Client ID from your Amazon Developer app"},
		{Name: "lwa_client_secret", Type: "string", Description: "LWA Client Secret from your Amazon Developer app"},
		{Name: "refresh_token", Type: "string", Description: "Vendor Central Refresh Token (from SP-API authorisation)"},
		{Name: "vendor_id", Type: "string", Description: "Your Vendor ID / Party ID from Vendor Central"},
		{Name: "marketplace_id", Type: "string", Description: "Amazon Marketplace ID (e.g. A1F83G8C2ARO7P for UK)"},
	}
}

func (a *AmazonVendorAdapter) GetCategories(ctx context.Context) ([]marketplace.Category, error) {
	return nil, nil
}

func (a *AmazonVendorAdapter) ValidateListing(ctx context.Context, listing marketplace.ListingData) (*marketplace.ValidationResult, error) {
	return &marketplace.ValidationResult{
		IsValid: false,
		Errors:  []marketplace.ValidationError{{Field: "general", Message: "Listing validation not supported for Amazon Vendor Central"}},
	}, nil
}

// GetSPAPIClient exposes the underlying client so vendor_order_handler.go
// can call Vendor-specific endpoints (GetVendorOrders, AcknowledgeVendorOrder).
func (a *AmazonVendorAdapter) GetSPAPIClient() *amazon.SPAPIClient {
	return a.client
}

// CancelOrder — Vendor Central does not expose order cancellation via API.
func (a *AmazonVendorAdapter) CancelOrder(ctx context.Context, externalOrderID string) error {
	return marketplace.ErrCancelNotSupported
}
