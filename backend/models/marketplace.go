package models

import "time"

// ============================================================================
// MODULE B MODELS - Listings & Marketplace Integration
// ============================================================================

// Listing represents a product listing on a marketplace channel
type Listing struct {
	ListingID         string                 `firestore:"listing_id" json:"listing_id"`
	TenantID          string                 `firestore:"tenant_id" json:"tenant_id"`
	
	// Source
	ProductID         string                 `firestore:"product_id" json:"product_id"`
	VariantID         string                 `firestore:"variant_id,omitempty" json:"variant_id,omitempty"`
	
	// Channel
	Channel           string                 `firestore:"channel" json:"channel"` // "amazon", "ebay", "temu", etc.
	ChannelAccountID  string                 `firestore:"channel_account_id" json:"channel_account_id"`
	MarketplaceID     string                 `firestore:"marketplace_id,omitempty" json:"marketplace_id,omitempty"` // e.g., "ATVPDKIKX0DER"
	
	// State
	State             string                 `firestore:"state" json:"state"` // "draft", "ready", "published", "paused", "ended", "error", "blocked"
	LifecycleState    string                 `firestore:"lifecycle_state,omitempty" json:"lifecycle_state,omitempty"`
	
	// Channel Identifiers (post-publish)
	ChannelIdentifiers *ChannelIdentifiers   `firestore:"channel_identifiers,omitempty" json:"channel_identifiers,omitempty"`
	
	// Overrides (channel-specific customizations)
	Overrides         *ListingOverrides      `firestore:"overrides,omitempty" json:"overrides,omitempty"`
	
	// Validation
	ValidationState   *ValidationState       `firestore:"validation_state,omitempty" json:"validation_state,omitempty"`
	
	// Health
	Health            *ListingHealth         `firestore:"health,omitempty" json:"health,omitempty"`
	
	// Versioning
	Version           int                    `firestore:"version,omitempty" json:"version,omitempty"`
	LastValidationAt  *time.Time             `firestore:"last_validation_at,omitempty" json:"last_validation_at,omitempty"`
	LastPublishedAt   *time.Time             `firestore:"last_published_at,omitempty" json:"last_published_at,omitempty"`
	LastSyncedAt      *time.Time             `firestore:"last_synced_at,omitempty" json:"last_synced_at,omitempty"`

	// Enriched data from catalog API (written by enrich function)
	EnrichedData      map[string]interface{} `firestore:"enriched_data,omitempty" json:"enriched_data,omitempty"`
	EnrichedAt        *time.Time             `firestore:"enriched_at,omitempty" json:"enriched_at,omitempty"`

	// Amazon import fields — written by import-batch
	Price              *float64 `firestore:"price,omitempty" json:"price,omitempty"`
	Quantity           *int     `firestore:"quantity,omitempty" json:"quantity,omitempty"`
	FulfillmentChannel string   `firestore:"fulfillment_channel,omitempty" json:"fulfillment_channel,omitempty"`

	// Timestamps
	CreatedAt         time.Time              `firestore:"created_at" json:"created_at"`
	UpdatedAt         time.Time              `firestore:"updated_at" json:"updated_at"`
}

type ChannelIdentifiers struct {
	ListingID string `firestore:"listing_id,omitempty" json:"listing_id,omitempty"` // External marketplace listing ID
	SKU       string `firestore:"sku,omitempty" json:"sku,omitempty"`
	URL       string `firestore:"url,omitempty" json:"url,omitempty"`
}

type ListingOverrides struct {
	Title           string                 `firestore:"title,omitempty" json:"title,omitempty"`
	Description     string                 `firestore:"description,omitempty" json:"description,omitempty"`
	CategoryMapping string                 `firestore:"category_mapping,omitempty" json:"category_mapping,omitempty"`
	Attributes      map[string]interface{} `firestore:"attributes,omitempty" json:"attributes,omitempty"`
	Images          []string               `firestore:"images,omitempty" json:"images,omitempty"` // Asset IDs in custom order
	Price           *float64               `firestore:"price,omitempty" json:"price,omitempty"`
	Quantity        *int                   `firestore:"quantity,omitempty" json:"quantity,omitempty"`
}

type ValidationState struct {
	Status        string             `firestore:"status" json:"status"` // "ok", "blocked", "warning", "unknown"
	Blockers      []ValidationIssue  `firestore:"blockers,omitempty" json:"blockers,omitempty"`
	Warnings      []ValidationIssue  `firestore:"warnings,omitempty" json:"warnings,omitempty"`
	RulesVersion  string             `firestore:"rules_version,omitempty" json:"rules_version,omitempty"`
	ValidatedAt   time.Time          `firestore:"validated_at" json:"validated_at"`
}

type ValidationIssue struct {
	Code        string `firestore:"code" json:"code"`
	Message     string `firestore:"message" json:"message"`
	FieldPath   string `firestore:"field_path,omitempty" json:"field_path,omitempty"`
	Severity    string `firestore:"severity" json:"severity"` // "error", "warning"
	Remediation string `firestore:"remediation,omitempty" json:"remediation,omitempty"`
}

type ListingHealth struct {
	Status             string     `firestore:"status" json:"status"` // "healthy", "needs_attention", "unknown"
	LastErrorCode      string     `firestore:"last_error_code,omitempty" json:"last_error_code,omitempty"`
	LastErrorCategory  string     `firestore:"last_error_category,omitempty" json:"last_error_category,omitempty"`
	LastErrorMessage   string     `firestore:"last_error_message,omitempty" json:"last_error_message,omitempty"`
	LastErrorAt        *time.Time `firestore:"last_error_at,omitempty" json:"last_error_at,omitempty"`
}

// ============================================================================
// IMPORT JOBS
// ============================================================================

type ImportJob struct {
	JobID            string                 `firestore:"job_id" json:"job_id"`
	TenantID         string                 `firestore:"tenant_id" json:"tenant_id"`
	
	// Configuration
	JobType          string                 `firestore:"job_type" json:"job_type"` // "full_import", "selective", "scheduled"
	Channel          string                 `firestore:"channel" json:"channel"` // "amazon", "ebay", etc.
	ChannelAccountID string                 `firestore:"channel_account_id" json:"channel_account_id"`
	AccountName      string                 `firestore:"account_name,omitempty" json:"account_name,omitempty"` // Human-readable account name
	
	// Filters
	Filters              map[string]interface{} `firestore:"filters,omitempty" json:"filters,omitempty"`
	ExternalIDs          []string               `firestore:"external_ids,omitempty" json:"external_ids,omitempty"` // For selective import
	FulfillmentFilter    string                 `firestore:"fulfillment_filter,omitempty" json:"fulfillment_filter,omitempty"` // "all", "fba", "merchant"
	StockFilter          string                 `firestore:"stock_filter,omitempty" json:"stock_filter,omitempty"` // "all", "in_stock"
	AIOptimize           bool                   `firestore:"ai_optimize,omitempty" json:"ai_optimize,omitempty"`
	EnrichData           bool                   `firestore:"enrich_data,omitempty" json:"enrich_data,omitempty"`
	SyncStock            bool                   `firestore:"sync_stock,omitempty" json:"sync_stock,omitempty"` // If true, import channel quantity to default warehouse
	TemuStatusFilters    []int                  `firestore:"temu_status_filters,omitempty" json:"temu_status_filters,omitempty"` // Temu goodsStatusFilterType values
	EbayListTypes        []string               `firestore:"ebay_list_types,omitempty" json:"ebay_list_types,omitempty"` // eBay Trading API list types: ActiveList, UnsoldList, SoldList
	
	// Status
	Status           string                 `firestore:"status" json:"status"` // "pending", "running", "completed", "failed", "cancelled"
	StatusMessage    string                 `firestore:"status_message,omitempty" json:"status_message,omitempty"` // Human-readable progress description
	ReportID         string                 `firestore:"report_id,omitempty" json:"report_id,omitempty"` // Amazon report ID (for crash recovery)
	
	// Progress
	TotalItems       int                    `firestore:"total_items" json:"total_items"`
	ProcessedItems   int                    `firestore:"processed_items" json:"processed_items"`
	SuccessfulItems  int                    `firestore:"successful_items" json:"successful_items"`
	FailedItems      int                    `firestore:"failed_items" json:"failed_items"`
	SkippedItems     int                    `firestore:"skipped_items" json:"skipped_items"`
	EnrichedItems      int                    `firestore:"enriched_items" json:"enriched_items"`
	EnrichFailedItems  int                    `firestore:"enrich_failed_items" json:"enrich_failed_items"`
	EnrichSkippedItems int                    `firestore:"enrich_skipped_items" json:"enrich_skipped_items"`
	EnrichTotalItems   int                    `firestore:"enrich_total_items" json:"enrich_total_items"`
	
	UpdatedItems     int                    `firestore:"updated_items" json:"updated_items"`

	// Results — NOTE: ImportedProducts/UpdatedProducts arrays removed to prevent Firestore doc bloat.
	// Use SuccessfulItems/UpdatedItems counters instead.
	ErrorLog         []ImportError          `firestore:"error_log,omitempty" json:"error_log,omitempty"`
	
	// Timing
	StartedAt        *time.Time             `firestore:"started_at,omitempty" json:"started_at,omitempty"`
	CompletedAt      *time.Time             `firestore:"completed_at,omitempty" json:"completed_at,omitempty"`
	
	// Metadata
	CreatedBy        string                 `firestore:"created_by,omitempty" json:"created_by,omitempty"`
	CreatedAt        time.Time              `firestore:"created_at" json:"created_at"`
	UpdatedAt        time.Time              `firestore:"updated_at" json:"updated_at"`
}

type ImportError struct {
	ExternalID   string    `firestore:"external_id" json:"external_id"`
	ErrorCode    string    `firestore:"error_code" json:"error_code"`
	Message      string    `firestore:"message" json:"message"`
	Timestamp    time.Time `firestore:"timestamp" json:"timestamp"`
	// Full SP-API / HTTP diagnostic fields — populated when available
	RequestURL   string    `firestore:"request_url,omitempty" json:"request_url,omitempty"`
	StatusCode   int       `firestore:"status_code,omitempty" json:"status_code,omitempty"`
	ResponseBody string    `firestore:"response_body,omitempty" json:"response_body,omitempty"`
}

// ============================================================================
// EXTENDED PRODUCT DATA
// ============================================================================
// Stores marketplace-specific data that doesn't map to standard PIM fields.
// Stored as a subcollection: products/{product_id}/extended_data/{source_key}
// This data is used by AI optimization to find additional item specifics.
// ============================================================================

type ExtendedProductData struct {
	// Identity
	SourceKey        string                 `firestore:"source_key" json:"source_key"`         // e.g. "amazon_B08N5WRWNW"
	ProductID        string                 `firestore:"product_id" json:"product_id"`
	TenantID         string                 `firestore:"tenant_id" json:"tenant_id"`

	// Source info
	Source           string                 `firestore:"source" json:"source"`                  // "amazon", "ebay", "web_scrape", etc.
	SourceID         string                 `firestore:"source_id" json:"source_id"`            // ASIN, eBay Item ID, etc.
	ChannelAccountID string                 `firestore:"channel_account_id" json:"channel_account_id"`

	// The actual extended data — key/value pairs of everything from the marketplace
	// that didn't get mapped to a standard Product field.
	// Examples: "bullet_point_1", "item_weight", "batteries_required", "country_of_origin"
	Data             map[string]interface{} `firestore:"data" json:"data"`

	// Metadata
	FetchedAt        time.Time              `firestore:"fetched_at" json:"fetched_at"`
	UpdatedAt        time.Time              `firestore:"updated_at" json:"updated_at"`
}

// ============================================================================
// IMPORT MAPPINGS
// ============================================================================

type ImportMapping struct {
	MappingID       string     `firestore:"mapping_id" json:"mapping_id"`
	TenantID        string     `firestore:"tenant_id" json:"tenant_id"`
	
	// Channel
	Channel         string     `firestore:"channel" json:"channel"`
	ChannelAccountID string    `firestore:"channel_account_id" json:"channel_account_id"`
	
	// Mapping
	ExternalID      string     `firestore:"external_id" json:"external_id"` // ASIN, eBay Item ID, etc.
	ProductID       string     `firestore:"product_id" json:"product_id"`
	VariantID       string     `firestore:"variant_id,omitempty" json:"variant_id,omitempty"`
	
	// Sync Settings
	SyncEnabled     bool       `firestore:"sync_enabled" json:"sync_enabled"`
	SyncFrequency   string     `firestore:"sync_frequency,omitempty" json:"sync_frequency,omitempty"` // "hourly", "daily", "weekly"
	LastSyncedAt    *time.Time `firestore:"last_synced_at,omitempty" json:"last_synced_at,omitempty"`
	NextSyncAt      *time.Time `firestore:"next_sync_at,omitempty" json:"next_sync_at,omitempty"`
	
	// Timestamps
	CreatedAt       time.Time  `firestore:"created_at" json:"created_at"`
	UpdatedAt       time.Time  `firestore:"updated_at" json:"updated_at"`
}

// ============================================================================
// MARKETPLACE CREDENTIALS
// ============================================================================


// ============================================================================
// CHANNEL CONFIG — per-credential settings for order sync, stock, shipping
// ============================================================================

type ChannelOrderConfig struct {
	Enabled          bool   `firestore:"enabled" json:"enabled"`
	FrequencyMinutes int    `firestore:"frequency_minutes" json:"frequency_minutes"` // 15, 30, 60, 360, 1440
	IncludeFBA       bool   `firestore:"include_fba" json:"include_fba"`
	StatusFilter     string `firestore:"status_filter" json:"status_filter"` // "Unshipped", "Unshipped,Pending", "all"
	LookbackHours    int    `firestore:"lookback_hours" json:"lookback_hours"` // hours to look back on each poll
	LastSync         string `firestore:"last_sync,omitempty" json:"last_sync,omitempty"` // RFC3339
	LastSyncStatus   string `firestore:"last_sync_status,omitempty" json:"last_sync_status,omitempty"` // "success", "failed"
	LastSyncCount    int    `firestore:"last_sync_count,omitempty" json:"last_sync_count,omitempty"`
	LastSyncError    string `firestore:"last_sync_error,omitempty" json:"last_sync_error,omitempty"`

	// S3: Per-channel order behaviour
	OrderPrefix              string `firestore:"order_prefix,omitempty" json:"order_prefix,omitempty"`                             // Prepended to external_order_id on import
	ValidateOnDownload       bool   `firestore:"validate_on_download" json:"validate_on_download"`                                 // Run automation rules immediately on import
	DownloadUnpaidOrders     bool   `firestore:"download_unpaid_orders" json:"download_unpaid_orders"`                             // Also import pending/unpaid orders
	ReserveUnpaidStock       bool   `firestore:"reserve_unpaid_stock" json:"reserve_unpaid_stock"`                                 // Reserve stock for unpaid orders
	DispatchNotesEnabled     bool   `firestore:"dispatch_notes_enabled" json:"dispatch_notes_enabled"`                             // Send dispatch notes to channel
	RefundNotesEnabled       bool   `firestore:"refund_notes_enabled" json:"refund_notes_enabled"`                                 // Send refund notes to channel
	CancellationNotesEnabled bool   `firestore:"cancellation_notes_enabled" json:"cancellation_notes_enabled"`                     // Send cancellation notes to channel
	ChannelTaxEnabled        bool   `firestore:"channel_tax_enabled" json:"channel_tax_enabled"`                                   // Use channel tax — do not recalculate
}

type ChannelStockConfig struct {
	ReservePending bool   `firestore:"reserve_pending" json:"reserve_pending"`
	LocationID     string `firestore:"location_id,omitempty" json:"location_id,omitempty"` // Route all orders from this channel to this warehouse location
}

type ChannelShippingConfig struct {
	UseAmazonBuyShipping bool   `firestore:"use_amazon_buy_shipping" json:"use_amazon_buy_shipping"`
	DefaultCarrier       string `firestore:"default_carrier,omitempty" json:"default_carrier,omitempty"`
	LabelFormat          string `firestore:"label_format,omitempty" json:"label_format,omitempty"` // "PDF", "ZPL"
	SellerFulfilledPrime bool   `firestore:"seller_fulfilled_prime" json:"seller_fulfilled_prime"`
}

// S3: Per-channel mapping structs

// PaymentMapping maps a channel payment method to an internal method label.
type PaymentMapping struct {
	ChannelMethod   string `firestore:"channel_method" json:"channel_method"`     // e.g. "PayPal Express"
	InternalMethod  string `firestore:"internal_method" json:"internal_method"`   // e.g. "PayPal", "Bank Transfer", "Credit Card"
}

// ShippingMapping maps a channel shipping service to an internal carrier + service code.
type ShippingMapping struct {
	ChannelService      string `firestore:"channel_service" json:"channel_service"`           // e.g. "Royal Mail 1st Class"
	InternalCarrierID   string `firestore:"internal_carrier_id" json:"internal_carrier_id"`   // e.g. "royal-mail"
	InternalServiceCode string `firestore:"internal_service_code" json:"internal_service_code"` // e.g. "RM1"
}

// InventoryMapping maps a channel SKU to an internal SKU / product.
type InventoryMapping struct {
	ChannelSKU  string `firestore:"channel_sku" json:"channel_sku"`     // SKU as listed on the channel
	InternalSKU string `firestore:"internal_sku" json:"internal_sku"`   // Internal platform SKU
	ProductID   string `firestore:"product_id,omitempty" json:"product_id,omitempty"` // Resolved product_id
}

type ChannelConfig struct {
	Orders   ChannelOrderConfig   `firestore:"orders" json:"orders"`
	Stock    ChannelStockConfig   `firestore:"stock" json:"stock"`
	Shipping ChannelShippingConfig `firestore:"shipping" json:"shipping"`

	// S3: Mapping tables
	PaymentMappings   []PaymentMapping   `firestore:"payment_mappings,omitempty" json:"payment_mappings,omitempty"`
	ShippingMappings  []ShippingMapping  `firestore:"shipping_mappings,omitempty" json:"shipping_mappings,omitempty"`
	InventoryMappings []InventoryMapping `firestore:"inventory_mappings,omitempty" json:"inventory_mappings,omitempty"`

	// Session 2 Task 2: Inventory sync settings
	InventorySync ChannelInventorySyncConfig `firestore:"inventory_sync,omitempty" json:"inventory_sync,omitempty"`
}

// ChannelInventorySyncConfig holds per-channel inventory push settings.
// Stored under the credential config doc at key "inventory_sync".
type ChannelInventorySyncConfig struct {
	// UpdateInventory enables/disables inventory sync pushes for this channel
	UpdateInventory bool `firestore:"update_inventory" json:"update_inventory"`
	// MaxQuantityToSync caps the quantity pushed to the channel (0 = no cap)
	MaxQuantityToSync int `firestore:"max_quantity_to_sync" json:"max_quantity_to_sync"`
	// MinStockLevel: never push stock below this level
	MinStockLevel int `firestore:"min_stock_level" json:"min_stock_level"`
	// LatencyBufferDays reduces available stock by N days of sales velocity
	LatencyBufferDays int `firestore:"latency_buffer_days" json:"latency_buffer_days"`
	// DefaultLatencyDays is the general buffer applied if no channel-level override
	DefaultLatencyDays int `firestore:"default_latency_days" json:"default_latency_days"`
	// LocationIDs is the list of warehouse location IDs contributing stock for this channel
	LocationIDs []string `firestore:"location_ids,omitempty" json:"location_ids,omitempty"`
}

// DefaultChannelConfig returns sensible defaults for a new credential
func DefaultChannelConfig() ChannelConfig {
	return ChannelConfig{
		Orders: ChannelOrderConfig{
			Enabled:          false,
			FrequencyMinutes: 30,
			IncludeFBA:       false,
			StatusFilter:     "Unshipped",
			LookbackHours:    24,
		},
		Stock: ChannelStockConfig{
			ReservePending: false,
		},
		Shipping: ChannelShippingConfig{
			UseAmazonBuyShipping: false,
			LabelFormat:          "PDF",
		},
		PaymentMappings:   []PaymentMapping{},
		ShippingMappings:  []ShippingMapping{},
		InventoryMappings: []InventoryMapping{},
		InventorySync: ChannelInventorySyncConfig{
			UpdateInventory:    false,
			MaxQuantityToSync:  0,
			MinStockLevel:      0,
			LatencyBufferDays:  0,
			DefaultLatencyDays: 0,
			LocationIDs:        []string{},
		},
	}
}

type MarketplaceCredential struct {
	CredentialID     string                 `firestore:"credential_id" json:"credential_id"`
	TenantID         string                 `firestore:"tenant_id" json:"tenant_id"`
	
	// Marketplace
	Channel          string                 `firestore:"channel" json:"channel"` // "amazon", "ebay", etc.
	AccountName      string                 `firestore:"account_name" json:"account_name"` // User-friendly name
	MarketplaceID    string                 `firestore:"marketplace_id,omitempty" json:"marketplace_id,omitempty"` // e.g., "ATVPDKIKX0DER"
	Environment      string                 `firestore:"environment" json:"environment"` // "production", "sandbox"
	
	// Credentials (encrypted in database)
	CredentialData   map[string]string      `firestore:"credential_data" json:"credential_data"`
	EncryptedFields  []string               `firestore:"encrypted_fields,omitempty" json:"encrypted_fields,omitempty"`
	
	// OAuth Tokens (if applicable)
	AccessToken      string                 `firestore:"access_token,omitempty" json:"-"` // Never return in JSON
	RefreshToken     string                 `firestore:"refresh_token,omitempty" json:"-"` // Never return in JSON
	TokenExpiresAt   *time.Time             `firestore:"token_expires_at,omitempty" json:"token_expires_at,omitempty"`
	
	// Status
	Active           bool                   `firestore:"active" json:"active"`
	LastTestedAt     *time.Time             `firestore:"last_tested_at,omitempty" json:"last_tested_at,omitempty"`
	LastTestStatus   string                 `firestore:"last_test_status,omitempty" json:"last_test_status,omitempty"` // "success", "failed"
	LastErrorMessage string                 `firestore:"last_error_message,omitempty" json:"last_error_message,omitempty"`
	
	// Channel configuration
	Config           ChannelConfig          `firestore:"config" json:"config"`

	// Timestamps
	CreatedAt        time.Time              `firestore:"created_at" json:"created_at"`
	UpdatedAt        time.Time              `firestore:"updated_at" json:"updated_at"`
}

// ============================================================================
// LISTING TEMPLATES
// ============================================================================

type ListingTemplate struct {
	TemplateID       string                 `firestore:"template_id" json:"template_id"`
	TenantID         string                 `firestore:"tenant_id" json:"tenant_id"`
	
	// Metadata
	Name             string                 `firestore:"name" json:"name"`
	Description      string                 `firestore:"description,omitempty" json:"description,omitempty"`
	Channel          string                 `firestore:"channel" json:"channel"`
	CategoryID       string                 `firestore:"category_id,omitempty" json:"category_id,omitempty"`
	
	// Template Data
	TemplateData     map[string]interface{} `firestore:"template_data" json:"template_data"`
	
	// Usage
	IsDefault        bool                   `firestore:"is_default" json:"is_default"`
	UsageCount       int                    `firestore:"usage_count" json:"usage_count"`
	
	// Channel configuration
	Config           ChannelConfig          `firestore:"config" json:"config"`

	// Timestamps
	CreatedAt        time.Time              `firestore:"created_at" json:"created_at"`
	UpdatedAt        time.Time              `firestore:"updated_at" json:"updated_at"`
}

// ============================================================================
// REQUEST/RESPONSE DTOs
// ============================================================================

type CreateListingRequest struct {
	ProductID        string                 `json:"product_id" binding:"required"`
	VariantID        string                 `json:"variant_id,omitempty"`
	Channel          string                 `json:"channel" binding:"required"`
	ChannelAccountID string                 `json:"channel_account_id" binding:"required"`
	Overrides        *ListingOverrides      `json:"overrides,omitempty"`
	AutoPublish      bool                   `json:"auto_publish,omitempty"`
}

type ImportProductsRequest struct {
	Channel            string   `json:"channel" binding:"required"`
	ChannelAccountID   string   `json:"channel_account_id" binding:"required"`
	JobType            string   `json:"job_type" binding:"required"` // "full_import", "selective"
	ExternalIDs        []string `json:"external_ids,omitempty"`      // For selective import (newline-separated on frontend)
	FulfillmentFilter  string   `json:"fulfillment_filter,omitempty"` // "all" (default), "fba", "merchant"
	StockFilter        string   `json:"stock_filter,omitempty"`       // "all" (default), "in_stock"
	AIOptimize         bool     `json:"ai_optimize,omitempty"`        // Flag for future AI listing optimization
	EnrichData         bool     `json:"enrich_data,omitempty"`        // Fetch extended data via catalog API
	AutoMap            bool     `json:"auto_map,omitempty"`           // Auto-create product mappings
	TemuStatusFilters  []int    `json:"temu_status_filters,omitempty"` // Temu goodsStatusFilterType values: 1=Active/Inactive, 4=Incomplete, 5=Draft, 6=Deleted
	EbayListTypes      []string `json:"ebay_list_types,omitempty"` // eBay list types: ActiveList, UnsoldList, SoldList
	SyncStock          bool     `json:"sync_stock,omitempty"`         // Import channel quantity to default warehouse (default: false)
}

type ConnectMarketplaceRequest struct {
	Channel       string            `json:"channel" binding:"required"`
	AccountName   string            `json:"account_name" binding:"required"`
	MarketplaceID string            `json:"marketplace_id,omitempty"`
	Environment   string            `json:"environment" binding:"required"` // "production", "sandbox"
	Credentials   map[string]string `json:"credentials" binding:"required"`
}

type PublishListingRequest struct {
	ListingIDs []string `json:"listing_ids" binding:"required"`
}

type SyncListingRequest struct {
	ListingIDs []string `json:"listing_ids" binding:"required"`
	SyncPrice  bool     `json:"sync_price"`
	SyncStock  bool     `json:"sync_stock"`
}

// BulkReviseRequest is the request body for POST /marketplace/listings/bulk/revise.
// Fields lists which override fields to write (allowed: title, description, price, attributes, images).
// FieldValues contains the values to write for each selected field.
type BulkReviseRequest struct {
	ListingIDs  []string             `json:"listing_ids"`
	Fields      []string             `json:"fields"`
	FieldValues BulkReviseFieldValues `json:"field_values"`
}

// BulkReviseFieldValues carries the explicit values to write into listing overrides.
// Only fields that appear in the parent BulkReviseRequest.Fields slice are applied.
type BulkReviseFieldValues struct {
	Title       string                 `json:"title,omitempty"`
	Description string                 `json:"description,omitempty"`
	Price       *float64               `json:"price,omitempty"`
	Attributes  map[string]interface{} `json:"attributes,omitempty"`
	Images      []string               `json:"images,omitempty"`
}

// BulkReviseResult summarises the outcome of a bulk revise operation.
type BulkReviseResult struct {
	Succeeded int                `json:"succeeded"`
	Failed    int                `json:"failed"`
	Errors    []BulkReviseError  `json:"errors,omitempty"`
}

// BulkReviseError records a per-listing failure during bulk revise.
type BulkReviseError struct {
	ListingID string `json:"listing_id"`
	Error     string `json:"error"`
}

// ============================================================================
// RESPONSE DTOs (joined data for API responses)
// ============================================================================

// ListingWithProduct is the response DTO that joins listing + product data
type ListingWithProduct struct {
	Listing

	// Joined product fields (read-only, from products collection)
	ProductTitle string  `json:"product_title,omitempty"`
	ProductBrand string  `json:"product_brand,omitempty"`
	ProductImage string  `json:"product_image,omitempty"`
	ProductPrice float64 `json:"product_price,omitempty"`
	ProductQty   int     `json:"product_qty,omitempty"`
	ProductSKU   string  `json:"product_sku,omitempty"`
}
