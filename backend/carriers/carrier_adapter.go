package carriers

import (
	"context"
	"time"
)

// ============================================================================
// CARRIER ADAPTER INTERFACE
// ============================================================================
// This interface allows external developers to easily integrate new carriers
// by implementing these methods. Each carrier adapter is self-contained.
// ============================================================================

// CarrierAdapter is the interface that all carrier integrations must implement
type CarrierAdapter interface {
	// GetMetadata returns carrier information for UI display
	GetMetadata() CarrierMetadata

	// ValidateCredentials checks if the provided credentials are valid
	ValidateCredentials(ctx context.Context, creds CarrierCredentials) error

	// GetServices returns available shipping services for this carrier
	GetServices(ctx context.Context, creds CarrierCredentials) ([]ShippingService, error)

	// GetRates retrieves shipping rates for a shipment
	GetRates(ctx context.Context, creds CarrierCredentials, req RateRequest) (*RateResponse, error)

	// CreateShipment generates a shipping label and tracking number
	CreateShipment(ctx context.Context, creds CarrierCredentials, req ShipmentRequest) (*ShipmentResponse, error)

	// VoidShipment cancels a shipment and voids the label
	VoidShipment(ctx context.Context, creds CarrierCredentials, trackingNumber string) error

	// GetTracking retrieves tracking information
	GetTracking(ctx context.Context, creds CarrierCredentials, trackingNumber string) (*TrackingInfo, error)

	// GenerateManifest creates an end-of-day manifest/close-out for the given shipments.
	// Carriers that don't support manifesting should return a ManifestResult with
	// Format "csv" and a locally-generated CSV payload so the operator still has a
	// summary document.
	GenerateManifest(ctx context.Context, creds CarrierCredentials, shipments []ManifestShipment) (*ManifestResult, error)

	// SupportsFeature checks if carrier supports a specific feature
	SupportsFeature(feature CarrierFeature) bool
}

// ============================================================================
// MANIFEST TYPES
// ============================================================================

// ManifestShipment is the lightweight shipment summary passed into GenerateManifest.
// It contains only the fields each carrier manifest call needs.
type ManifestShipment struct {
	ShipmentID     string    `json:"shipment_id"`
	TrackingNumber string    `json:"tracking_number"`
	ServiceCode    string    `json:"service_code"`
	ServiceName    string    `json:"service_name"`
	Reference      string    `json:"reference"`       // order reference / order ID
	ToName         string    `json:"to_name"`
	ToPostalCode   string    `json:"to_postal_code"`
	ToCountry      string    `json:"to_country"`
	WeightKg       float64   `json:"weight_kg"`
	ParcelCount    int       `json:"parcel_count"`
	Cost           float64   `json:"cost"`
	Currency       string    `json:"currency"`
	CreatedAt      time.Time `json:"created_at"`
}

// ManifestResult is what a carrier returns after a manifest/close-out call.
type ManifestResult struct {
	// ManifestID is the carrier's own reference for this manifest (may be empty for
	// carriers that don't issue IDs, e.g. locally-generated CSVs).
	ManifestID string `json:"manifest_id"`

	// CarrierID identifies which carrier produced this manifest.
	CarrierID string `json:"carrier_id"`

	// Format is the file type: "pdf", "csv", or "zpl".
	Format string `json:"format"`

	// Data contains the raw manifest bytes (PDF binary or CSV text).
	// Stored in GCS by the handler; this field is populated only on creation.
	Data []byte `json:"data,omitempty"`

	// DownloadURL is set by the handler after uploading Data to GCS.
	DownloadURL string `json:"download_url,omitempty"`

	// ShipmentCount is how many shipments are included in this manifest.
	ShipmentCount int `json:"shipment_count"`

	// CreatedAt is when the manifest was generated.
	CreatedAt time.Time `json:"created_at"`
}

// ============================================================================
// CARRIER METADATA
// ============================================================================

type CarrierMetadata struct {
	ID          string   `json:"id"`           // Unique identifier (e.g., "royal-mail")
	Name        string   `json:"name"`         // Display name (e.g., "Royal Mail")
	DisplayName string   `json:"display_name"` // Full name for UI
	Country     string   `json:"country"`      // Primary country (e.g., "GB")
	Logo        string   `json:"logo"`         // Logo URL or icon
	Website     string   `json:"website"`      // Carrier website
	SupportURL  string   `json:"support_url"`  // Support/docs URL
	Features    []string `json:"features"`     // Supported features
	IsActive    bool     `json:"is_active"`    // Whether adapter is enabled
}

// CarrierFeature represents supported carrier capabilities
type CarrierFeature string

const (
	FeatureRateQuotes      CarrierFeature = "rate_quotes"       // Can get shipping rates
	FeatureTracking        CarrierFeature = "tracking"          // Has tracking
	FeatureSignature       CarrierFeature = "signature"         // Supports signature on delivery
	FeatureInsurance       CarrierFeature = "insurance"         // Can add insurance
	FeatureSaturdayDelivery CarrierFeature = "saturday_delivery" // Saturday delivery
	FeaturePickup          CarrierFeature = "pickup"            // Supports pickup requests
	FeatureInternational   CarrierFeature = "international"     // International shipping
	FeaturePOBox           CarrierFeature = "po_box"            // Can deliver to PO boxes
	FeatureManifest        CarrierFeature = "manifest"          // End-of-day manifests
	FeatureCustoms         CarrierFeature = "customs"           // Customs documentation
	FeatureVoid            CarrierFeature = "void"              // Can void labels
)

// ============================================================================
// CREDENTIALS
// ============================================================================

type CarrierCredentials struct {
	CarrierID string                 `json:"carrier_id"`
	APIKey    string                 `json:"api_key,omitempty"`
	Username  string                 `json:"username,omitempty"`
	Password  string                 `json:"password,omitempty"`
	AccountID string                 `json:"account_id,omitempty"`
	Extra     map[string]interface{} `json:"extra,omitempty"` // Carrier-specific fields
	IsSandbox bool                   `json:"is_sandbox"`      // Test/production mode
}

// ============================================================================
// SHIPPING SERVICES
// ============================================================================

type ShippingService struct {
	Code        string  `json:"code"`         // Service code (e.g., "1ST_CLASS")
	Name        string  `json:"name"`         // Display name
	Description string  `json:"description"`  // Service description
	Domestic    bool    `json:"domestic"`     // Domestic shipping
	International bool    `json:"international"` // International shipping
	EstimatedDays int     `json:"estimated_days"` // Transit time
	MaxWeight   float64 `json:"max_weight"`   // Max weight in kg
	Features    []string `json:"features"`    // Service features
}

// ============================================================================
// RATE REQUESTS & RESPONSES
// ============================================================================

type RateRequest struct {
	FromAddress Address         `json:"from_address"`
	ToAddress   Address         `json:"to_address"`
	Parcels     []Parcel        `json:"parcels"`
	Services    []string        `json:"services,omitempty"` // Specific services to quote
	Currency    string          `json:"currency"`           // Preferred currency
	Options     ShipmentOptions `json:"options"`
}

type RateResponse struct {
	Rates []Rate `json:"rates"`
}

type Rate struct {
	ServiceCode   string    `json:"service_code"`
	ServiceName   string    `json:"service_name"`
	Cost          Money     `json:"cost"`
	Currency      string    `json:"currency"`
	EstimatedDays int       `json:"estimated_days"`
	DeliveryBy    time.Time `json:"delivery_by,omitempty"`
	Carrier       string    `json:"carrier"`
}

// ============================================================================
// SHIPMENT REQUESTS & RESPONSES
// ============================================================================

type ShipmentRequest struct {
	ServiceCode   string          `json:"service_code"`
	FromAddress   Address         `json:"from_address"`
	ToAddress     Address         `json:"to_address"`
	ReturnAddress Address         `json:"return_address,omitempty"`
	Parcels       []Parcel        `json:"parcels"`
	Options       ShipmentOptions `json:"options"`
	Reference     string          `json:"reference,omitempty"` // Order/shipment reference
	Description   string          `json:"description"`         // Contents description
}

type ShipmentResponse struct {
	TrackingNumber  string            `json:"tracking_number"`
	LabelURL        string            `json:"label_url"`         // URL to download label
	LabelFormat     string            `json:"label_format"`      // PDF, PNG, ZPL, etc.
	LabelData       []byte            `json:"label_data,omitempty"` // Base64 encoded label
	TrackingURL     string            `json:"tracking_url,omitempty"`
	Cost            Money             `json:"cost"`
	Currency        string            `json:"currency"`
	CarrierRef      string            `json:"carrier_ref,omitempty"` // Carrier's reference
	EstimatedDelivery time.Time       `json:"estimated_delivery,omitempty"`
	CustomsDocs     []CustomsDocument `json:"customs_docs,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

// ============================================================================
// TRACKING
// ============================================================================

type TrackingInfo struct {
	TrackingNumber string          `json:"tracking_number"`
	Status         TrackingStatus  `json:"status"`
	StatusDetail   string          `json:"status_detail"`
	Events         []TrackingEvent `json:"events"`
	EstimatedDelivery time.Time    `json:"estimated_delivery,omitempty"`
	ActualDelivery    time.Time    `json:"actual_delivery,omitempty"`
	SignedBy          string        `json:"signed_by,omitempty"`
	Location          string        `json:"location,omitempty"`
}

type TrackingStatus string

const (
	TrackingStatusUnknown     TrackingStatus = "unknown"
	TrackingStatusPreTransit  TrackingStatus = "pre_transit"  // Label created
	TrackingStatusInTransit   TrackingStatus = "in_transit"   // In carrier network
	TrackingStatusOutForDelivery TrackingStatus = "out_for_delivery"
	TrackingStatusDelivered   TrackingStatus = "delivered"
	TrackingStatusException   TrackingStatus = "exception"    // Delay, damage, etc.
	TrackingStatusReturned    TrackingStatus = "returned"
	TrackingStatusCancelled   TrackingStatus = "cancelled"
)

type TrackingEvent struct {
	Timestamp   time.Time `json:"timestamp"`
	Status      string    `json:"status"`
	Description string    `json:"description"`
	Location    string    `json:"location,omitempty"`
}

// ============================================================================
// SUPPORTING STRUCTURES
// ============================================================================

type Address struct {
	Name        string `json:"name"`
	Company     string `json:"company,omitempty"`
	AddressLine1 string `json:"address_line1"`
	AddressLine2 string `json:"address_line2,omitempty"`
	AddressLine3 string `json:"address_line3,omitempty"`
	City        string `json:"city"`
	State       string `json:"state,omitempty"`
	PostalCode  string `json:"postal_code"`
	Country     string `json:"country"` // ISO 2-letter code
	Phone       string `json:"phone,omitempty"`
	Email       string `json:"email,omitempty"`
}

type Parcel struct {
	Length float64 `json:"length"` // cm
	Width  float64 `json:"width"`  // cm
	Height float64 `json:"height"` // cm
	Weight float64 `json:"weight"` // kg
	Description string `json:"description,omitempty"`
}

type ShipmentOptions struct {
	Signature      bool              `json:"signature"`
	SaturdayDelivery bool            `json:"saturday_delivery"`
	Insurance      *Insurance        `json:"insurance,omitempty"`
	Customs        *CustomsInfo      `json:"customs,omitempty"`
	LabelFormat    string            `json:"label_format,omitempty"` // PDF, PNG, ZPL
	Notifications  *Notifications    `json:"notifications,omitempty"`
	Extra          map[string]interface{} `json:"extra,omitempty"`
}

type Insurance struct {
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
}

type CustomsInfo struct {
	ContentsType   string         `json:"contents_type"`   // merchandise, gift, sample, etc.
	InvoiceNumber  string         `json:"invoice_number,omitempty"`
	Items          []CustomsItem  `json:"items"`
	NonDelivery    string         `json:"non_delivery,omitempty"` // return, abandon
}

type CustomsItem struct {
	Description    string  `json:"description"`
	Quantity       int     `json:"quantity"`
	Value          float64 `json:"value"`
	Weight         float64 `json:"weight"`
	HSCode         string  `json:"hs_code,omitempty"`
	OriginCountry  string  `json:"origin_country"`
}

type CustomsDocument struct {
	Type string `json:"type"` // commercial_invoice, customs_declaration, etc.
	URL  string `json:"url"`
	Data []byte `json:"data,omitempty"`
}

type Notifications struct {
	Email []string `json:"email,omitempty"`
	SMS   []string `json:"sms,omitempty"`
}

type Money struct {
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
}

// ============================================================================
// CARRIER REGISTRY
// ============================================================================
// Global registry that holds all available carrier adapters
// External developers register their adapters here during init()
// ============================================================================

var registry = make(map[string]CarrierAdapter)

// Register adds a carrier adapter to the global registry
func Register(adapter CarrierAdapter) {
	metadata := adapter.GetMetadata()
	registry[metadata.ID] = adapter
}

// GetAdapter retrieves a carrier adapter by ID
func GetAdapter(carrierID string) (CarrierAdapter, bool) {
	adapter, exists := registry[carrierID]
	return adapter, exists
}

// ListAdapters returns all registered carrier adapters
func ListAdapters() []CarrierMetadata {
	var adapters []CarrierMetadata
	for _, adapter := range registry {
		adapters = append(adapters, adapter.GetMetadata())
	}
	return adapters
}

// ============================================================================
// EXAMPLE: How external developers would add a new carrier
// ============================================================================

/*
package mycarrier

import (
	"context"
	"module-a/carriers"
)

type MyCarrierAdapter struct {
	// carrier-specific fields
}

func init() {
	// Register the adapter when package is imported
	carriers.Register(&MyCarrierAdapter{})
}

func (a *MyCarrierAdapter) GetMetadata() carriers.CarrierMetadata {
	return carriers.CarrierMetadata{
		ID:          "my-carrier",
		Name:        "My Carrier",
		DisplayName: "My Awesome Carrier",
		Country:     "GB",
		Features:    []string{"tracking", "international"},
		IsActive:    true,
	}
}

func (a *MyCarrierAdapter) CreateShipment(ctx context.Context, creds carriers.CarrierCredentials, req carriers.ShipmentRequest) (*carriers.ShipmentResponse, error) {
	// 1. Call carrier API
	// 2. Parse response
	// 3. Return standardized ShipmentResponse
	return &carriers.ShipmentResponse{
		TrackingNumber: "TRACK123",
		LabelURL:       "https://carrier.com/label.pdf",
		Cost:           carriers.Money{Amount: 4.50, Currency: "GBP"},
	}, nil
}

// ... implement other interface methods
*/
