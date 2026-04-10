package models

import "time"

// ============================================================================
// FULFILMENT SOURCE MODEL
// ============================================================================
// A FulfilmentSource is any place from which stock can be despatched.
// This replaces the thin Location model and drives all workflow decisions.
//
// Types and their behaviour:
//   own_warehouse  → MarketMate generates label, stock tracked here
//   3pl            → MarketMate generates label OR 3PL generates, configurable
//   fba            → No action — Amazon fulfils, no label needed
//   dropship       → Creates purchase order to supplier, no label from here
//   virtual        → Used for bundling/kitting rules, no physical location
//
// Firestore: tenants/{tenant_id}/fulfilment_sources/{source_id}
// ============================================================================

type FulfilmentSource struct {
	// Identity
	SourceID   string `json:"source_id" firestore:"source_id"`
	TenantID   string `json:"tenant_id" firestore:"tenant_id"`
	Name       string `json:"name" firestore:"name"`
	Code       string `json:"code" firestore:"code"` // Short internal code e.g. "LON-01"
	Type       string `json:"type" firestore:"type"` // own_warehouse, 3pl, fba, dropship, virtual

	// State
	Active  bool `json:"active" firestore:"active"`
	Default bool `json:"default" firestore:"default"` // Fallback if no workflow matches

	// Physical address (not required for fba/virtual/dropship)
	Address *SourceAddress `json:"address,omitempty" firestore:"address,omitempty"`

	// Operating hours — used by workflow to check cutoff feasibility
	OperatingHours *OperatingHours `json:"operating_hours,omitempty" firestore:"operating_hours,omitempty"`

	// Label generation — how does despatch work from this source?
	LabelConfig LabelConfig `json:"label_config" firestore:"label_config"`

	// Carriers available at this source (ordered by priority)
	// Workflow uses this to constrain carrier assignment
	AvailableCarriers []SourceCarrier `json:"available_carriers,omitempty" firestore:"available_carriers,omitempty"`

	// Dropship-specific — only populated when type == "dropship"
	DropshipConfig *DropshipConfig `json:"dropship_config,omitempty" firestore:"dropship_config,omitempty"`

	// 3PL-specific — API/email integration for pick instructions
	ThreePLConfig *ThreePLConfig `json:"three_pl_config,omitempty" firestore:"three_pl_config,omitempty"`

	// FBA-specific — which Amazon marketplace this FBA node serves
	FBAConfig *FBAConfig `json:"fba_config,omitempty" firestore:"fba_config,omitempty"`

	// Inventory settings
	InventoryTracked bool   `json:"inventory_tracked" firestore:"inventory_tracked"` // false for dropship/fba
	InventoryMode    string `json:"inventory_mode" firestore:"inventory_mode"`       // real_time, daily_sync, manual

	// Session 9 enhancements
	CurrencyOverride    string `json:"currency_override,omitempty" firestore:"currency_override,omitempty"` // ISO 4217
	IsFulfilmentCentre  bool   `json:"is_fulfilment_centre" firestore:"is_fulfilment_centre"`

	// Geographical region — used by workflow for nearest-source logic
	Latitude  *float64 `json:"latitude,omitempty" firestore:"latitude,omitempty"`
	Longitude *float64 `json:"longitude,omitempty" firestore:"longitude,omitempty"`
	Region    string   `json:"region,omitempty" firestore:"region,omitempty"` // e.g. "north", "south"

	// Performance metrics (updated by background job)
	Performance *SourcePerformance `json:"performance,omitempty" firestore:"performance,omitempty"`

	// Metadata
	Tags      []string          `json:"tags,omitempty" firestore:"tags,omitempty"`
	ExtraData map[string]string `json:"extra_data,omitempty" firestore:"extra_data,omitempty"`

	// Timestamps
	CreatedAt time.Time  `json:"created_at" firestore:"created_at"`
	UpdatedAt time.Time  `json:"updated_at" firestore:"updated_at"`
	DeletedAt *time.Time `json:"deleted_at,omitempty" firestore:"deleted_at,omitempty"`
}

// SourceAddress is a full postal address for a fulfilment source.
// Stored as a struct rather than a string for use in label generation.
type SourceAddress struct {
	CompanyName  string   `json:"company_name" firestore:"company_name"`
	AddressLine1 string   `json:"address_line1" firestore:"address_line1"`
	AddressLine2 string   `json:"address_line2,omitempty" firestore:"address_line2,omitempty"`
	City         string   `json:"city" firestore:"city"`
	County       string   `json:"county,omitempty" firestore:"county,omitempty"`
	PostalCode   string   `json:"postal_code" firestore:"postal_code"`
	Country      string   `json:"country" firestore:"country"` // ISO 2-letter
	Phone        string   `json:"phone,omitempty" firestore:"phone,omitempty"`
	Email        string   `json:"email,omitempty" firestore:"email,omitempty"`
}

// OperatingHours defines when a source can despatch and its daily cutoff.
type OperatingHours struct {
	Timezone    string   `json:"timezone" firestore:"timezone"`       // e.g. "Europe/London"
	CutoffTime  string   `json:"cutoff_time" firestore:"cutoff_time"` // "14:30" — orders after this miss today's run
	WorkingDays []int    `json:"working_days" firestore:"working_days"` // 1=Mon ... 7=Sun
	Holidays    []string `json:"holidays,omitempty" firestore:"holidays,omitempty"` // "2026-12-25"
}

// LabelConfig controls how shipping labels are generated from this source.
type LabelConfig struct {
	// Who generates the label?
	Mode string `json:"mode" firestore:"mode"`
	// own         → MarketMate calls carrier API, generates label
	// third_party → 3PL generates their own label, we just send pick instruction
	// none        → No label (FBA, dropship)

	// Default label format for this source
	LabelFormat string `json:"label_format,omitempty" firestore:"label_format,omitempty"` // PDF, ZPL, PNG

	// Return address override (if different from source address)
	ReturnAddressOverride *SourceAddress `json:"return_address_override,omitempty" firestore:"return_address_override,omitempty"`

	// Auto-despatch: generate label immediately on workflow match (vs manual batch)
	AutoDespatch bool `json:"auto_despatch" firestore:"auto_despatch"`
}

// SourceCarrier represents a carrier that is available at a specific source,
// with source-specific cutoff times that may differ from the carrier's global cutoff.
type SourceCarrier struct {
	CarrierID  string `json:"carrier_id" firestore:"carrier_id"`   // matches carriers registry ID
	Enabled    bool   `json:"enabled" firestore:"enabled"`
	Priority   int    `json:"priority" firestore:"priority"`         // 1 = highest priority at this source
	CutoffTime string `json:"cutoff_time,omitempty" firestore:"cutoff_time,omitempty"` // Overrides carrier global cutoff
	AccountRef string `json:"account_ref,omitempty" firestore:"account_ref,omitempty"` // Carrier account number at this location
}

// DropshipConfig holds supplier contact and ordering details.
// Used when type == "dropship" to know how to send purchase orders.
type DropshipConfig struct {
	SupplierID      string `json:"supplier_id" firestore:"supplier_id"` // ref to suppliers collection
	SupplierName    string `json:"supplier_name" firestore:"supplier_name"`

	// How orders are sent to this supplier
	OrderMethod string `json:"order_method" firestore:"order_method"`
	// email      → Send formatted PO email
	// api        → POST to supplier's API endpoint
	// manual     → User downloads CSV/prints PO manually
	// marketmate → Supplier is on MarketMate platform (future)

	// Pooling behaviour
	PoolOrders     bool   `json:"pool_orders" firestore:"pool_orders"`           // If true, hold orders until batch time
	PoolSchedule   string `json:"pool_schedule,omitempty" firestore:"pool_schedule,omitempty"` // Cron: "0 9 * * 1-5" = 9am weekdays
	SendImmediately bool  `json:"send_immediately" firestore:"send_immediately"` // Override pooling for urgent orders

	// Email config (when order_method == "email")
	EmailConfig *DropshipEmailConfig `json:"email_config,omitempty" firestore:"email_config,omitempty"`

	// API config (when order_method == "api")
	APIConfig *DropshipAPIConfig `json:"api_config,omitempty" firestore:"api_config,omitempty"`

	// Lead time in working days (shown to user, used for SLA calculation)
	LeadTimeDays int `json:"lead_time_days" firestore:"lead_time_days"`

	// Tracking: does supplier provide tracking back to us?
	ProvidesTracking bool `json:"provides_tracking" firestore:"provides_tracking"`
}

type DropshipEmailConfig struct {
	ToAddresses  []string `json:"to_addresses" firestore:"to_addresses"`
	CCAddresses  []string `json:"cc_addresses,omitempty" firestore:"cc_addresses,omitempty"`
	SubjectTemplate string `json:"subject_template" firestore:"subject_template"` // e.g. "PO-{po_number} from MarketMate"
}

type DropshipAPIConfig struct {
	Endpoint    string            `json:"endpoint" firestore:"endpoint"`
	Method      string            `json:"method" firestore:"method"` // POST, PUT
	AuthType    string            `json:"auth_type" firestore:"auth_type"` // none, api_key, basic, oauth2
	Headers     map[string]string `json:"headers,omitempty" firestore:"headers,omitempty"`
	// Credentials stored encrypted in separate collection, not here
}

// ThreePLConfig holds integration settings for third-party logistics providers.
type ThreePLConfig struct {
	ProviderName string `json:"provider_name" firestore:"provider_name"`

	// Integration method
	IntegrationMethod string `json:"integration_method" firestore:"integration_method"`
	// api   → REST/SOAP API
	// sftp  → Drop files on SFTP
	// email → Send pick list email
	// edi   → EDI messages

	// Does the 3PL generate their own labels, or do we send labels?
	GeneratesOwnLabels bool `json:"generates_own_labels" firestore:"generates_own_labels"`

	// If we generate labels, does the 3PL need them embedded in pick instructions?
	EmbedLabelsInPickInstructions bool `json:"embed_labels_in_pick_instructions" firestore:"embed_labels_in_pick_instructions"`

	// API config if integration_method == "api"
	APIEndpoint string `json:"api_endpoint,omitempty" firestore:"api_endpoint,omitempty"`

	// Contact for manual/email integrations
	ContactEmail string `json:"contact_email,omitempty" firestore:"contact_email,omitempty"`
}

// FBAConfig holds Amazon FBA marketplace routing config.
// When an order is routed to an FBA source, no label is generated —
// the marketplace is informed that it's FBA and handles fulfilment.
type FBAConfig struct {
	MarketplaceID    string `json:"marketplace_id" firestore:"marketplace_id"`       // e.g. "A1F83G8C2ARO7P" (UK)
	SellerID         string `json:"seller_id" firestore:"seller_id"`
	FulfillmentCenters []string `json:"fulfillment_centers,omitempty" firestore:"fulfillment_centers,omitempty"`
}

// SourcePerformance is updated by background analytics jobs.
type SourcePerformance struct {
	OnTimeDespatchRate float64   `json:"on_time_despatch_rate" firestore:"on_time_despatch_rate"`
	AvgDespatchHours   float64   `json:"avg_despatch_hours" firestore:"avg_despatch_hours"`
	AccuracyRate       float64   `json:"accuracy_rate" firestore:"accuracy_rate"`
	LastUpdated        time.Time `json:"last_updated" firestore:"last_updated"`
}

// ============================================================================
// SUPPLIER MODEL
// ============================================================================
// A Supplier is a company that provides goods for dropshipping.
// Multiple FulfilmentSources of type "dropship" may reference the same Supplier.
//
// Firestore: tenants/{tenant_id}/suppliers/{supplier_id}
// ============================================================================

type Supplier struct {
	SupplierID string `json:"supplier_id" firestore:"supplier_id"`
	TenantID   string `json:"tenant_id" firestore:"tenant_id"`
	Name       string `json:"name" firestore:"name"`
	Code       string `json:"code" firestore:"code"` // Internal reference code

	// Contact
	ContactName string `json:"contact_name,omitempty" firestore:"contact_name,omitempty"`
	Email       string `json:"email,omitempty" firestore:"email,omitempty"`
	Phone       string `json:"phone,omitempty" firestore:"phone,omitempty"`
	Website     string `json:"website,omitempty" firestore:"website,omitempty"`

	// Address
	Address *SourceAddress `json:"address,omitempty" firestore:"address,omitempty"`

	// Lead time
	LeadTimeDays int `json:"lead_time_days,omitempty" firestore:"lead_time_days,omitempty"`

	// Phase 2: MarketMate platform account
	MarketmateAccountID string `json:"marketmate_account_id,omitempty" firestore:"marketmate_account_id,omitempty"`
	OnPlatform          bool   `json:"on_platform" firestore:"on_platform"`

	// Status
	Active bool `json:"active" firestore:"active"`

	// --- Order Placement ---
	// OrderMethod: "email" | "ftp" | "webhook" | "manual"
	OrderMethod   string                 `json:"order_method,omitempty" firestore:"order_method,omitempty"`
	EmailConfig   *SupplierEmailConfig   `json:"email_config,omitempty" firestore:"email_config,omitempty"`
	FTPConfig     *SupplierFTPConfig     `json:"ftp_config,omitempty" firestore:"ftp_config,omitempty"`
	WebhookConfig *SupplierWebhookConfig `json:"webhook_config,omitempty" firestore:"webhook_config,omitempty"`

	// CSV template for order export
	CSVTemplate *SupplierCSVTemplate `json:"csv_template,omitempty" firestore:"csv_template,omitempty"`

	// --- Financial ---
	Currency         string               `json:"currency" firestore:"currency"` // ISO 4217
	PaymentTermsDays int                  `json:"payment_terms_days" firestore:"payment_terms_days"`
	PaymentMethod    string               `json:"payment_method,omitempty" firestore:"payment_method,omitempty"` // bank_transfer | card | paypal | credit
	BankDetails      *SupplierBankDetails `json:"bank_details,omitempty" firestore:"bank_details,omitempty"`
	VATNumber        string               `json:"vat_number,omitempty" firestore:"vat_number,omitempty"`
	CompanyRegNumber string               `json:"company_reg_number,omitempty" firestore:"company_reg_number,omitempty"`
	CreditLimit      float64              `json:"credit_limit,omitempty" firestore:"credit_limit,omitempty"`
	MinOrderValue    float64              `json:"min_order_value,omitempty" firestore:"min_order_value,omitempty"`

	// Notes
	Notes string   `json:"notes,omitempty" firestore:"notes,omitempty"`
	Tags  []string `json:"tags,omitempty" firestore:"tags,omitempty"`

	// Timestamps
	CreatedAt time.Time `json:"created_at" firestore:"created_at"`
	UpdatedAt time.Time `json:"updated_at" firestore:"updated_at"`
}

// SupplierEmailConfig holds email ordering configuration.
type SupplierEmailConfig struct {
	ToAddresses     []string `json:"to_addresses" firestore:"to_addresses"`
	CCAddresses     []string `json:"cc_addresses,omitempty" firestore:"cc_addresses,omitempty"`
	SubjectTemplate string   `json:"subject_template" firestore:"subject_template"` // "PO-{po_number} from MarketMate"
	AttachCSV       bool     `json:"attach_csv" firestore:"attach_csv"`
	AttachPDF       bool     `json:"attach_pdf" firestore:"attach_pdf"`
	BodyTemplate    string   `json:"body_template,omitempty" firestore:"body_template,omitempty"`
}

// SupplierFTPConfig holds FTP/SFTP ordering configuration.
// Password is stored encrypted; never stored in plaintext.
type SupplierFTPConfig struct {
	Host        string `json:"host" firestore:"host"`
	Port        int    `json:"port" firestore:"port"`
	Username    string `json:"username" firestore:"username"`
	PasswordEnc string `json:"password_enc,omitempty" firestore:"password_enc,omitempty"` // AES-256-GCM encrypted
	Path        string `json:"path" firestore:"path"`                                      // remote directory
	Protocol    string `json:"protocol" firestore:"protocol"`                              // "ftp" | "sftp"
}

// SupplierWebhookConfig holds webhook ordering configuration.
// Secret is stored encrypted; never stored in plaintext.
type SupplierWebhookConfig struct {
	URL        string            `json:"url" firestore:"url"`
	Method     string            `json:"method" firestore:"method"`                           // POST | PUT
	AuthType   string            `json:"auth_type" firestore:"auth_type"`                     // none | api_key | basic | bearer
	AuthHeader string            `json:"auth_header,omitempty" firestore:"auth_header,omitempty"` // header name for api_key
	SecretEnc  string            `json:"secret_enc,omitempty" firestore:"secret_enc,omitempty"`   // AES-256-GCM encrypted
	Headers    map[string]string `json:"headers,omitempty" firestore:"headers,omitempty"`
	Format     string            `json:"format" firestore:"format"` // "json" | "xml"
}

// SupplierBankDetails holds bank payment information.
// AccountNumber is stored encrypted.
type SupplierBankDetails struct {
	AccountName      string `json:"account_name" firestore:"account_name"`
	AccountNumberEnc string `json:"account_number_enc,omitempty" firestore:"account_number_enc,omitempty"` // AES-256-GCM encrypted
	SortCode         string `json:"sort_code,omitempty" firestore:"sort_code,omitempty"`
	IBAN             string `json:"iban,omitempty" firestore:"iban,omitempty"`
	BIC              string `json:"bic,omitempty" firestore:"bic,omitempty"`
	BankName         string `json:"bank_name,omitempty" firestore:"bank_name,omitempty"`
}

// SupplierCSVTemplate defines how to format CSV order exports for this supplier.
type SupplierCSVTemplate struct {
	// ColumnMap: our field name -> supplier's column header
	// e.g. {"supplier_sku": "Item Code", "qty_ordered": "Qty", "unit_cost": "Unit Price"}
	ColumnMap  map[string]string `json:"column_map" firestore:"column_map"`
	Delimiter  string            `json:"delimiter" firestore:"delimiter"`    // "," | "\t" | ";"
	HasHeader  bool              `json:"has_header" firestore:"has_header"`
	DateFormat string            `json:"date_format" firestore:"date_format"` // e.g. "DD/MM/YYYY"
}

// ============================================================================
// FULFILMENT SOURCE TYPE CONSTANTS
// ============================================================================

const (
	SourceTypeOwnWarehouse = "own_warehouse"
	SourceType3PL          = "3pl"
	SourceTypeFBA          = "fba"
	SourceTypeDropship     = "dropship"
	SourceTypeVirtual      = "virtual"
)

const (
	LabelModeOwn        = "own"         // MarketMate generates label
	LabelModeThirdParty = "third_party" // 3PL generates label
	LabelModeNone       = "none"        // No label (FBA, dropship)
)

const (
	OrderMethodEmail      = "email"
	OrderMethodAPI        = "api"
	OrderMethodManual     = "manual"
	OrderMethodMarketmate = "marketmate"
)

const (
	InventoryModeRealTime  = "real_time"
	InventoryModeDailySync = "daily_sync"
	InventoryModeManual    = "manual"
)
