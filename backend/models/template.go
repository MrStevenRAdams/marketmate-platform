package models

import "time"

// ============================================================================
// MODULE L — PAGEBUILDER TEMPLATES
// ============================================================================

// TemplateType defines what kind of document the template produces
type TemplateType string

const (
	TemplateTypeInvoice           TemplateType = "invoice"
	TemplateTypePackingSlip       TemplateType = "packing_slip"
	TemplateTypePostageLabel      TemplateType = "postage_label"
	TemplateTypeEmail             TemplateType = "email"
	TemplateTypeEbayListing       TemplateType = "ebay_listing"
	TemplateTypeCustom            TemplateType = "custom"
	TemplateTypeAmazonVCS         TemplateType = "amazon_vcs"
	TemplateTypePickingList       TemplateType = "picking_list"
	TemplateTypePackingList       TemplateType = "packing_list"
	TemplateTypeStockItemLabel    TemplateType = "stock_item_label"
	TemplateTypePurchaseOrder     TemplateType = "purchase_order"
	TemplateTypeConsignment       TemplateType = "consignment"
	TemplateTypeWarehouseTransfer TemplateType = "warehouse_transfer"
)

// TemplateCanvasPreset holds default canvas dimensions for a template type.
type TemplateCanvasPreset struct {
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	Unit   string  `json:"unit"`
}

// DefaultCanvasPresets maps each template type to its default canvas settings.
// Document types default to A4 (210×297mm); label types default to 4×6in.
var DefaultCanvasPresets = map[TemplateType]TemplateCanvasPreset{
	TemplateTypeInvoice:           {Width: 210, Height: 297, Unit: "mm"},
	TemplateTypePackingSlip:       {Width: 210, Height: 297, Unit: "mm"},
	TemplateTypePostageLabel:      {Width: 4, Height: 6, Unit: "in"},
	TemplateTypeEmail:             {Width: 600, Height: 0, Unit: "px"}, // height 0 = auto
	TemplateTypeEbayListing:       {Width: 800, Height: 0, Unit: "px"},
	TemplateTypeCustom:            {Width: 800, Height: 1000, Unit: "px"},
	TemplateTypeAmazonVCS:         {Width: 210, Height: 297, Unit: "mm"},
	TemplateTypePickingList:       {Width: 210, Height: 297, Unit: "mm"},
	TemplateTypePackingList:       {Width: 210, Height: 297, Unit: "mm"},
	TemplateTypeStockItemLabel:    {Width: 4, Height: 6, Unit: "in"},
	TemplateTypePurchaseOrder:     {Width: 210, Height: 297, Unit: "mm"},
	TemplateTypeConsignment:       {Width: 210, Height: 297, Unit: "mm"},
	TemplateTypeWarehouseTransfer: {Width: 210, Height: 297, Unit: "mm"},
}

// TriggerTypeAutomated indicates an email template is triggered automatically by an order event.
// TriggerTypeManual is declared in workflow.go — use that constant for the "manual" value.
const (
	TriggerTypeAutomated = "automated"
)

// TriggerEvent constants for the five standard automated email events
const (
	TriggerEventOrderConfirmation    = "order_confirmation"
	TriggerEventOrderDespatch        = "order_despatch"
	TriggerEventReturnConfirmation   = "return_confirmation"
	TriggerEventRefundConfirmation   = "refund_confirmation"
	TriggerEventExchangeConfirmation = "exchange_confirmation"
)

// OutputFormat is what the template renders to
type OutputFormat string

const (
	OutputFormatHTML OutputFormat = "HTML"
	OutputFormatPDF  OutputFormat = "PDF"
	OutputFormatMJML OutputFormat = "MJML"
)

// CanvasSettings mirrors the frontend canvas state
type CanvasSettings struct {
	Width           interface{} `json:"width" firestore:"width"`     // int or "auto"
	Height          interface{} `json:"height" firestore:"height"`   // int or "auto"
	Unit            string      `json:"unit" firestore:"unit"`       // mm, px, in
	BackgroundColor string      `json:"backgroundColor" firestore:"backgroundColor"`
}

// GridSettings mirrors the frontend grid/ruler state
type GridSettings struct {
	ShowRulers   bool    `json:"showRulers" firestore:"showRulers"`
	ShowGrid     bool    `json:"showGrid" firestore:"showGrid"`
	SnapEnabled  bool    `json:"snapEnabled" firestore:"snapEnabled"`
	GridSpacing  float64 `json:"gridSpacing" firestore:"gridSpacing"`
	GridStyle    string  `json:"gridStyle" firestore:"gridStyle"`
}

// TemplateVersion is a lightweight history entry (not full snapshot)
type TemplateVersion struct {
	Version    int    `json:"version" firestore:"version"`
	SavedAt    string `json:"savedAt" firestore:"savedAt"`
	BlockCount int    `json:"blockCount" firestore:"blockCount"`
	SavedBy    string `json:"savedBy,omitempty" firestore:"savedBy,omitempty"`
}

// Template is the full pagebuilder template document stored in Firestore
type Template struct {
	TemplateID   string           `json:"id" firestore:"id"`
	TenantID     string           `json:"tenant_id" firestore:"tenant_id"`
	Name         string           `json:"name" firestore:"name"`
	Type         TemplateType     `json:"type" firestore:"type"`
	OutputFormat OutputFormat     `json:"output_format" firestore:"output_format"`
	Theme        string           `json:"theme" firestore:"theme"`
	Canvas       CanvasSettings   `json:"canvas" firestore:"canvas"`
	Grid         GridSettings     `json:"grid" firestore:"grid"`
	Blocks       interface{}      `json:"blocks" firestore:"blocks"` // Raw JSON — opaque to backend
	Version      int              `json:"version" firestore:"version"`
	History      []TemplateVersion `json:"history,omitempty" firestore:"history,omitempty"`
	IsDefault    bool             `json:"is_default" firestore:"is_default"` // default template for its type
	Enabled      bool             `json:"enabled" firestore:"enabled"`        // enable/disable toggle (default true)
	TriggerType     string           `json:"trigger_type,omitempty" firestore:"trigger_type,omitempty"`         // "manual" | "automated"
	TriggerEvent    string           `json:"trigger_event,omitempty" firestore:"trigger_event,omitempty"`       // e.g. "order_confirmation"
	VirtualPrinter  string           `json:"virtual_printer,omitempty" firestore:"virtual_printer,omitempty"`   // assigned virtual printer name
	PrintBehaviour  string           `json:"print_behaviour,omitempty" firestore:"print_behaviour,omitempty"`   // "print_on_save" | "print_on_dispatch" | "manual_only"
	CreatedAt       time.Time        `json:"created_at" firestore:"created_at"`
	UpdatedAt    time.Time        `json:"updated_at" firestore:"updated_at"`
	CreatedBy    string           `json:"created_by,omitempty" firestore:"created_by,omitempty"`
}

// SellerProfile holds the tenant's business details used as merge tag data
type SellerProfile struct {
	TenantID   string    `json:"tenant_id" firestore:"tenant_id"`
	Name       string    `json:"name" firestore:"name"`
	Address    string    `json:"address" firestore:"address"`
	Phone      string    `json:"phone" firestore:"phone"`
	Email      string    `json:"email" firestore:"email"`
	LogoURL    string    `json:"logo_url" firestore:"logo_url"`
	VATNumber  string    `json:"vat_number" firestore:"vat_number"`
	Website    string    `json:"website,omitempty" firestore:"website,omitempty"`
	UpdatedAt  time.Time `json:"updated_at" firestore:"updated_at"`

	// Regional & Display Preferences (Session 1)
	DefaultWarehouseCountry string `json:"default_warehouse_country,omitempty" firestore:"default_warehouse_country,omitempty"`
	MyCountry               string `json:"my_country,omitempty" firestore:"my_country,omitempty"`
	DefaultCurrency         string `json:"default_currency,omitempty" firestore:"default_currency,omitempty"`
	WeightUnit              string `json:"weight_unit,omitempty" firestore:"weight_unit,omitempty"`           // g, kg, oz, lbs
	DimensionUnit           string `json:"dimension_unit,omitempty" firestore:"dimension_unit,omitempty"`     // cm, inches
	Timezone                string `json:"timezone,omitempty" firestore:"timezone,omitempty"`
	DSTEnabled              bool   `json:"dst_enabled" firestore:"dst_enabled"`
	DateFormat              string `json:"date_format,omitempty" firestore:"date_format,omitempty"`           // DD/MM/YYYY, MM/DD/YYYY, YYYY-MM-DD
	TaxForDirectOrders      string `json:"tax_for_direct_orders,omitempty" firestore:"tax_for_direct_orders,omitempty"` // include, exclude, none
	LocaliseCurrencies      bool   `json:"localise_currencies" firestore:"localise_currencies"`

	// Currency settings (Session 2)
	BaseCurrency string `json:"base_currency,omitempty" firestore:"base_currency,omitempty"`
}

// TemplateRenderData is the context passed to the merge tag resolver
type TemplateRenderData struct {
	Order    OrderRenderData    `json:"order"`
	Customer CustomerRenderData `json:"customer"`
	Shipping ShippingRenderData `json:"shipping"`
	Seller   SellerRenderData   `json:"seller"`
	Lines    []LineRenderData   `json:"lines"`
	Custom   map[string]string  `json:"custom"`
}

type OrderRenderData struct {
	ID                string `json:"id"`
	Date              string `json:"date"`
	Status            string `json:"status"`
	Total             string `json:"total"`
	Subtotal          string `json:"subtotal"`
	Tax               string `json:"tax"`
	ShippingCost      string `json:"shipping_cost"`
	Notes             string `json:"notes"`
	// Session 3: new data tags
	NumericID         string `json:"numeric_id"`
	ExternalReference string `json:"external_reference"`
	ProcessedDate     string `json:"processed_date"`
	DispatchByDate    string `json:"dispatch_by_date"`
	TrackingNumber    string `json:"tracking_number"`
	Vendor            string `json:"vendor"`
	Currency          string `json:"currency"`
	PaymentMethod     string `json:"payment_method"`
}

type CustomerRenderData struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Phone string `json:"phone"`
}

type ShippingRenderData struct {
	Name         string `json:"name"`
	AddressLine1 string `json:"address_line1"`
	AddressLine2 string `json:"address_line2"`
	AddressLine3 string `json:"address_line3"` // Session 3: new tag
	City         string `json:"city"`
	State        string `json:"state"`
	PostalCode   string `json:"postal_code"`
	Country      string `json:"country"`
	Method       string `json:"method"`
}

type SellerRenderData struct {
	Name      string `json:"name"`
	Address   string `json:"address"`
	Phone     string `json:"phone"`
	Email     string `json:"email"`
	LogoURL   string `json:"logo_url"`
	VATNumber string `json:"vat_number"`
}

type LineRenderData struct {
	SKU         string `json:"sku"`
	Title       string `json:"title"`
	Quantity    int    `json:"quantity"`
	UnitPrice   string `json:"unit_price"`
	LineTotal   string `json:"line_total"`
	// Session 3: new data tags
	BatchNumber string `json:"batch_number"`
	BinRack     string `json:"bin_rack"`
	Weight      string `json:"weight"`
}
