package marketplace

import (
	"context"
	"errors"
	"time"
)

// ErrCancelNotSupported is returned by CancelOrder implementations
// on channels that do not expose an order-cancellation API.
var ErrCancelNotSupported = errors.New("marketplace does not support order cancellation via API")

// ============================================================================
// MARKETPLACE ADAPTER INTERFACE
// ============================================================================
// This interface defines the contract that all marketplace integrations
// (Amazon, eBay, Shopify, Temu, etc.) must implement. This abstraction
// allows the platform to add new marketplaces by simply implementing
// this interface without changing core business logic.
// ============================================================================

type MarketplaceAdapter interface {
	// ========================================================================
	// CONNECTION & AUTHENTICATION
	// ========================================================================
	
	// Connect establishes connection to marketplace using provided credentials
	Connect(ctx context.Context, credentials Credentials) error
	
	// Disconnect terminates marketplace connection
	Disconnect(ctx context.Context) error
	
	// TestConnection verifies credentials and API connectivity
	TestConnection(ctx context.Context) error
	
	// RefreshAuth refreshes authentication tokens (for OAuth-based marketplaces)
	RefreshAuth(ctx context.Context) error
	
	// GetConnectionStatus returns current connection health
	GetConnectionStatus(ctx context.Context) (*ConnectionStatus, error)
	
	// ========================================================================
	// PRODUCT IMPORT (Marketplace → PIM)
	// ========================================================================
	
	// FetchListings retrieves products from marketplace based on filters
	FetchListings(ctx context.Context, filters ImportFilters) ([]MarketplaceProduct, error)
	
	// FetchProduct retrieves a single product by external ID
	FetchProduct(ctx context.Context, externalID string) (*MarketplaceProduct, error)
	
	// FetchProductImages downloads product images
	FetchProductImages(ctx context.Context, externalID string) ([]ImageData, error)
	
	// FetchInventory gets current inventory level from marketplace
	FetchInventory(ctx context.Context, externalID string) (*InventoryLevel, error)
	
	// ========================================================================
	// LISTING MANAGEMENT (PIM → Marketplace)
	// ========================================================================
	
	// CreateListing creates a new listing on the marketplace
	CreateListing(ctx context.Context, listing ListingData) (*ListingResult, error)
	
	// UpdateListing updates an existing listing
	UpdateListing(ctx context.Context, externalID string, updates ListingData) error
	
	// DeleteListing removes a listing from marketplace
	DeleteListing(ctx context.Context, externalID string) error
	
	// PublishListing activates a draft listing
	PublishListing(ctx context.Context, externalID string) error
	
	// UnpublishListing deactivates a published listing
	UnpublishListing(ctx context.Context, externalID string) error
	
	// ========================================================================
	// BULK OPERATIONS
	// ========================================================================
	
	// BulkCreateListings creates multiple listings in one operation
	BulkCreateListings(ctx context.Context, listings []ListingData) ([]ListingResult, error)
	
	// BulkUpdateListings updates multiple listings
	BulkUpdateListings(ctx context.Context, updates []ListingUpdate) ([]UpdateResult, error)
	
	// ========================================================================
	// SYNC & MONITORING
	// ========================================================================
	
	// GetListingStatus retrieves current status of a listing
	GetListingStatus(ctx context.Context, externalID string) (*ListingStatus, error)
	
	// SyncInventory updates inventory quantity on marketplace
	SyncInventory(ctx context.Context, externalID string, quantity int) error
	
	// SyncPrice updates price on marketplace
	SyncPrice(ctx context.Context, externalID string, price float64) error

	// CancelOrder pushes an order cancellation to the marketplace.
	// Returns an error if the channel does not support cancellation or the call fails.
	// Implementations that do not support cancellation should return
	// marketplace.ErrCancelNotSupported so callers can degrade gracefully.
	CancelOrder(ctx context.Context, externalOrderID string) error

	// ========================================================================
	// METADATA
	// ========================================================================
	
	// GetName returns marketplace adapter name (e.g., "amazon", "ebay")
	GetName() string
	
	// GetDisplayName returns user-friendly name (e.g., "Amazon US")
	GetDisplayName() string
	
	// GetSupportedFeatures returns list of supported capabilities
	GetSupportedFeatures() []string
	
	// GetRequiredFields returns mandatory fields for listing creation
	GetRequiredFields() []RequiredField
	
	// GetCategories retrieves marketplace category tree
	GetCategories(ctx context.Context) ([]Category, error)
	
	// ValidateListing validates listing data before submission
	ValidateListing(ctx context.Context, listing ListingData) (*ValidationResult, error)
}

// ============================================================================
// DATA STRUCTURES
// ============================================================================

// Credentials holds marketplace authentication data
type Credentials struct {
	MarketplaceID   string            `json:"marketplace_id"`
	Environment     string            `json:"environment"` // "production" | "sandbox"
	MarketplaceType string            `json:"marketplace_type"` // "amazon", "ebay", etc.
	Data            map[string]string `json:"data"` // Marketplace-specific credentials
	EncryptedFields []string          `json:"encrypted_fields,omitempty"`
}

// ConnectionStatus represents marketplace connection health
type ConnectionStatus struct {
	IsConnected      bool      `json:"is_connected"`
	LastChecked      time.Time `json:"last_checked"`
	LastSuccessful   time.Time `json:"last_successful,omitempty"`
	ErrorMessage     string    `json:"error_message,omitempty"`
	TokenExpiresAt   time.Time `json:"token_expires_at,omitempty"`
	RateLimitRemaining int     `json:"rate_limit_remaining,omitempty"`
}

// ImportFilters defines criteria for fetching products
type ImportFilters struct {
	PageSize           int       `json:"page_size"`
	PageToken          string    `json:"page_token,omitempty"`
	ModifiedSince      time.Time `json:"modified_since,omitempty"`
	SearchQuery        string    `json:"search_query,omitempty"`
	CategoryID         string    `json:"category_id,omitempty"`
	ExternalIDs        []string  `json:"external_ids,omitempty"`        // Specific products to fetch
	FulfillmentFilter  string    `json:"fulfillment_filter,omitempty"`  // "all", "fba", "merchant"
	StockFilter        string    `json:"stock_filter,omitempty"`        // "all", "in_stock"
	TemuStatusFilters  []int     `json:"temu_status_filters,omitempty"` // Temu goodsStatusFilterType: 1=Active/Inactive, 4=Incomplete, 5=Draft, 6=Deleted
	EbayListTypes      []string  `json:"ebay_list_types,omitempty"`     // eBay Trading API: ActiveList, UnsoldList, SoldList

	// ProgressCallback is called during long-running fetch operations to report
	// progress back to the import service (e.g. "Fetching page 3/7 (300 items)")
	// The service uses this to update the job's StatusMessage in Firestore.
	// Returns false if the fetch should stop (job cancelled).
	ProgressCallback func(message string) bool `json:"-"`

	// ProductCallback is called for each product as it is fetched, allowing the
	// service to save products incrementally rather than waiting for all to be fetched.
	// Returns false if the fetch should stop (job cancelled).
	ProductCallback func(product MarketplaceProduct) bool `json:"-"`

	// AlreadyMappedIDs is the set of external IDs (ASINs, eBay item IDs, etc.)
	// that already have an import mapping in the database. Adapters that do
	// per-product enrichment (e.g. Amazon Catalog Items API) use this to skip
	// enrichment for known products and stream them immediately, so that
	// ProcessedItems reflects: already-known = instant tick, new = tick after enrichment.
	AlreadyMappedIDs map[string]bool `json:"-"`
}

// MarketplaceProduct represents a product from any marketplace in standardized format
type MarketplaceProduct struct {
	ExternalID          string                 `json:"external_id"`       // ASIN, eBay Item ID, etc.
	SKU                 string                 `json:"sku,omitempty"`
	Title               string                 `json:"title"`
	Description         string                 `json:"description"`
	Brand               string                 `json:"brand,omitempty"`
	Price               float64                `json:"price"`
	Currency            string                 `json:"currency"`
	Quantity            int                    `json:"quantity"`
	Images              []ImageData            `json:"images"`
	Attributes          map[string]interface{} `json:"attributes,omitempty"`
	Categories          []string               `json:"categories,omitempty"`
	Identifiers         Identifiers            `json:"identifiers"`
	Variations          []Variation            `json:"variations,omitempty"` // For parent products
	Dimensions          *Dimensions            `json:"dimensions,omitempty"`
	Weight              *Weight                `json:"weight,omitempty"`
	Condition           string                 `json:"condition,omitempty"`
	ListingURL          string                 `json:"listing_url,omitempty"`
	FulfillmentChannel  string                 `json:"fulfillment_channel,omitempty"` // "FBA", "MFN" (merchant), "DEFAULT"
	IsInStock           bool                   `json:"is_in_stock"`
	MarketplaceStatus   string                 `json:"marketplace_status,omitempty"` // "active", "inactive", "deleted" etc — channel-specific status
	RawData             map[string]interface{} `json:"raw_data,omitempty"` // Original marketplace response
}

// ImageData represents product image information
type ImageData struct {
	URL      string `json:"url"`
	Position int    `json:"position"`
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
	IsMain   bool   `json:"is_main"`
}

// Identifiers holds product identification codes
type Identifiers struct {
	UPC  string `json:"upc,omitempty"`
	EAN  string `json:"ean,omitempty"`
	ISBN string `json:"isbn,omitempty"`
	ASIN string `json:"asin,omitempty"`
	GTIN string `json:"gtin,omitempty"`
}

// Variation represents a product variant (color, size, etc.)
type Variation struct {
	ExternalID string                 `json:"external_id"`
	SKU        string                 `json:"sku,omitempty"`
	Attributes map[string]interface{} `json:"attributes"` // color: "red", size: "large"
	Price      float64                `json:"price"`
	Quantity   int                    `json:"quantity"`
	Images     []ImageData            `json:"images,omitempty"`
}

// Dimensions represents product dimensions
type Dimensions struct {
	Length float64 `json:"length"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	Unit   string  `json:"unit"` // "cm", "in"
}

// Weight represents product weight
type Weight struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit"` // "kg", "lb", "g", "oz"
}

// InventoryLevel represents stock quantity from marketplace
type InventoryLevel struct {
	ExternalID string    `json:"external_id"`
	Quantity   int       `json:"quantity"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// ListingData represents data for creating/updating a listing
type ListingData struct {
	ProductID       string                 `json:"product_id"`
	VariantID       string                 `json:"variant_id,omitempty"`
	Title           string                 `json:"title"`
	Description     string                 `json:"description"`
	Price           float64                `json:"price"`
	Quantity        int                    `json:"quantity"`
	Images          []string               `json:"images"` // URLs or asset IDs
	CategoryID      string                 `json:"category_id"`
	Attributes      map[string]interface{} `json:"attributes,omitempty"`
	Identifiers     Identifiers            `json:"identifiers,omitempty"`
	Dimensions      *Dimensions            `json:"dimensions,omitempty"`
	Weight          *Weight                `json:"weight,omitempty"`
	Condition       string                 `json:"condition,omitempty"`
	ShippingProfile string                 `json:"shipping_profile,omitempty"`
	ReturnPolicy    string                 `json:"return_policy,omitempty"`
	CustomFields    map[string]interface{} `json:"custom_fields,omitempty"` // Marketplace-specific
}

// ListingResult represents the result of creating a listing
type ListingResult struct {
	ExternalID      string                 `json:"external_id"` // Marketplace listing ID
	SKU             string                 `json:"sku,omitempty"`
	URL             string                 `json:"url,omitempty"`
	Status          string                 `json:"status"` // "active", "pending", "error"
	CreatedAt       time.Time              `json:"created_at"`
	Errors          []ValidationError      `json:"errors,omitempty"`
	Warnings        []ValidationError      `json:"warnings,omitempty"`
	RawResponse     map[string]interface{} `json:"raw_response,omitempty"`
}

// ListingUpdate represents update data for bulk operations
type ListingUpdate struct {
	ExternalID string                 `json:"external_id"`
	Updates    map[string]interface{} `json:"updates"` // Fields to update
}

// UpdateResult represents the result of updating a listing
type UpdateResult struct {
	ExternalID  string            `json:"external_id"`
	Success     bool              `json:"success"`
	Errors      []ValidationError `json:"errors,omitempty"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// ListingStatus represents current listing state on marketplace
type ListingStatus struct {
	ExternalID       string    `json:"external_id"`
	Status           string    `json:"status"` // "active", "inactive", "out_of_stock", etc.
	IsActive         bool      `json:"is_active"`
	Quantity         int       `json:"quantity"`
	Price            float64   `json:"price"`
	LastUpdated      time.Time `json:"last_updated"`
	NeedsAttention   bool      `json:"needs_attention"`
	AttentionReasons []string  `json:"attention_reasons,omitempty"`
}

// Category represents a marketplace category
type Category struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	ParentID string     `json:"parent_id,omitempty"`
	Level    int        `json:"level"`
	Children []Category `json:"children,omitempty"`
}

// RequiredField defines a mandatory field for listing creation
type RequiredField struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"` // "string", "number", "boolean", "array"
	Description string   `json:"description"`
	Examples    []string `json:"examples,omitempty"`
}

// ValidationResult represents listing validation outcome
type ValidationResult struct {
	IsValid  bool              `json:"is_valid"`
	Errors   []ValidationError `json:"errors,omitempty"`
	Warnings []ValidationError `json:"warnings,omitempty"`
}

// ValidationError represents a validation issue
type ValidationError struct {
	Code        string `json:"code"`
	Field       string `json:"field,omitempty"`
	Message     string `json:"message"`
	Severity    string `json:"severity"` // "error", "warning"
	Remediation string `json:"remediation,omitempty"`
}
