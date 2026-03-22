package models

// Order represents a customer order from a marketplace
type Order struct {
	// Identity
	OrderID  string `json:"order_id" firestore:"order_id"`
	TenantID string `json:"tenant_id" firestore:"tenant_id"`

	// Channel Context
	Channel          string `json:"channel" firestore:"channel"`                       // "amazon", "ebay", "temu"
	ChannelAccountID string `json:"channel_account_id" firestore:"channel_account_id"` // Which marketplace account
	ExternalOrderID  string `json:"external_order_id" firestore:"external_order_id"`   // Marketplace order ID
	MarketplaceRegion string `json:"marketplace_region,omitempty" firestore:"marketplace_region,omitempty"`

	// Customer (snapshot)
	Customer Customer `json:"customer" firestore:"customer"`

	// Addresses (snapshots)
	ShippingAddress Address  `json:"shipping_address" firestore:"shipping_address"`
	BillingAddress  *Address `json:"billing_address,omitempty" firestore:"billing_address,omitempty"`

	// Status & Lifecycle
	Status    string `json:"status" firestore:"status"`         // imported, processing, on_hold, ready_to_fulfil, fulfilled, cancelled
	SubStatus string `json:"sub_status,omitempty" firestore:"sub_status,omitempty"`

	// Financial Snapshot
	Totals OrderTotals `json:"totals" firestore:"totals"`

	// Payment
	PaymentStatus string `json:"payment_status" firestore:"payment_status"` // pending, authorized, captured, failed
	PaymentMethod string `json:"payment_method,omitempty" firestore:"payment_method,omitempty"`

	// Fulfilment
	FulfilmentSource string `json:"fulfilment_source,omitempty" firestore:"fulfilment_source,omitempty"` // stock, dropship, network, mixed
	WarehouseID      string `json:"warehouse_id,omitempty" firestore:"warehouse_id,omitempty"`
	SupplierID       string `json:"supplier_id,omitempty" firestore:"supplier_id,omitempty"`

	// Parent/Child Relationships
	ParentOrderID string   `json:"parent_order_id,omitempty" firestore:"parent_order_id,omitempty"`
	ChildOrderIDs []string `json:"child_order_ids,omitempty" firestore:"child_order_ids,omitempty"`

	// Tags & Notes
	Tags          []string `json:"tags,omitempty" firestore:"tags,omitempty"`
	InternalNotes string   `json:"internal_notes,omitempty" firestore:"internal_notes,omitempty"`
	BuyerNotes    string   `json:"buyer_notes,omitempty" firestore:"buyer_notes,omitempty"`

	// Organise — Folder assignment
	FolderID   string `json:"folder_id,omitempty" firestore:"folder_id,omitempty"`
	FolderName string `json:"folder_name,omitempty" firestore:"folder_name,omitempty"`

	// Organise — Identifier (free-text reference code, e.g. BATCH-001, PRIORITY)
	Identifier string `json:"identifier,omitempty" firestore:"identifier,omitempty"`

	// Organise — Warehouse location override (specific bin/zone within warehouse)
	WarehouseLocationID   string `json:"warehouse_location_id,omitempty" firestore:"warehouse_location_id,omitempty"`
	WarehouseLocationName string `json:"warehouse_location_name,omitempty" firestore:"warehouse_location_name,omitempty"`

	// Organise — Fulfilment centre override (which FC/warehouse handles this order)
	FulfilmentCenterID   string `json:"fulfilment_center_id,omitempty" firestore:"fulfilment_center_id,omitempty"`
	FulfilmentCenterName string `json:"fulfilment_center_name,omitempty" firestore:"fulfilment_center_name,omitempty"`

	// SLA & Dates
	OrderDate              string `json:"order_date" firestore:"order_date"`
	PromisedShipBy         string `json:"promised_ship_by,omitempty" firestore:"promised_ship_by,omitempty"`
	DespatchByDate         string `json:"despatch_by_date,omitempty" firestore:"despatch_by_date,omitempty"`
	ScheduledDeliveryDate  string `json:"scheduled_delivery_date,omitempty" firestore:"scheduled_delivery_date,omitempty"`
	SLAAtRisk              bool   `json:"sla_at_risk" firestore:"sla_at_risk"`

	// Linked Entities
	ShipmentIDs    []string    `json:"shipment_ids,omitempty" firestore:"shipment_ids,omitempty"`
	ReservationIDs []string    `json:"reservation_ids,omitempty" firestore:"reservation_ids,omitempty"`
	Lines          []OrderLine `json:"lines,omitempty" firestore:"lines,omitempty"` // embedded lines (if stored inline)

	// Shipping
	ShippingService string `json:"shipping_service,omitempty" firestore:"shipping_service,omitempty"`
	Carrier         string `json:"carrier,omitempty" firestore:"carrier,omitempty"`

	// Print Tracking (Task 10, 11)
	LabelGenerated  bool   `json:"label_generated,omitempty" firestore:"label_generated,omitempty"`
	LabelURL        string `json:"label_url,omitempty" firestore:"label_url,omitempty"`
	TrackingNumber  string `json:"tracking_number,omitempty" firestore:"tracking_number,omitempty"`
	InvoicePrinted  bool   `json:"invoice_printed,omitempty" firestore:"invoice_printed,omitempty"`

	// Timestamps
	CreatedAt  string `json:"created_at" firestore:"created_at"`
	UpdatedAt  string `json:"updated_at" firestore:"updated_at"`
	ImportedAt string `json:"imported_at" firestore:"imported_at"`

	// PII Encryption — see services.PIIService
	// When pii_encrypted=true, Customer/ShippingAddress/BillingAddress are zeroed
	// and PII lives in the *_enc ciphertext fields below.
	CustomerEnc      string `json:"customer_enc,omitempty" firestore:"customer_enc,omitempty"`
	ShippingEnc      string `json:"shipping_enc,omitempty" firestore:"shipping_enc,omitempty"`
	BillingEnc       string `json:"billing_enc,omitempty" firestore:"billing_enc,omitempty"`
	EmailToken       string `json:"pii_email_token,omitempty" firestore:"pii_email_token,omitempty"`
	NameToken        string `json:"pii_name_token,omitempty" firestore:"pii_name_token,omitempty"`
	PostcodeToken    string `json:"pii_postcode_token,omitempty" firestore:"pii_postcode_token,omitempty"`
	PhoneToken       string `json:"pii_phone_token,omitempty" firestore:"pii_phone_token,omitempty"`
	PIIEncrypted     bool   `json:"pii_encrypted,omitempty" firestore:"pii_encrypted,omitempty"`
}

// Customer information snapshot
type Customer struct {
	Name  string `json:"name" firestore:"name"`
	Email string `json:"email,omitempty" firestore:"email,omitempty"`
	Phone string `json:"phone,omitempty" firestore:"phone,omitempty"`
}

// Address information
type Address struct {
	Name         string `json:"name" firestore:"name"`
	AddressLine1 string `json:"address_line1" firestore:"address_line1"`
	AddressLine2 string `json:"address_line2,omitempty" firestore:"address_line2,omitempty"`
	City         string `json:"city" firestore:"city"`
	State        string `json:"state,omitempty" firestore:"state,omitempty"`
	PostalCode   string `json:"postal_code" firestore:"postal_code"`
	Country      string `json:"country" firestore:"country"` // ISO 2-letter
}

// OrderTotals financial information
type OrderTotals struct {
	Subtotal         Money  `json:"subtotal" firestore:"subtotal"`
	Tax              Money  `json:"tax" firestore:"tax"`
	Shipping         Money  `json:"shipping" firestore:"shipping"`
	PostageTax       Money  `json:"postage_tax,omitempty" firestore:"postage_tax,omitempty"`
	Discount         Money  `json:"discount" firestore:"discount"`
	GrandTotal       Money  `json:"grand_total" firestore:"grand_total"`
	BaseCurrencyTotal *Money `json:"base_currency_total,omitempty" firestore:"base_currency_total,omitempty"`
}

// Money represents a monetary value with currency and optional FX information
// This is the single source of truth for the Money type across the entire models package
type Money struct {
	Amount          float64 `json:"amount" firestore:"amount"`
	Currency        string  `json:"currency" firestore:"currency"`                                   // ISO 4217 (e.g., "GBP", "USD")
	FXRateUsed      float64 `json:"fx_rate_used,omitempty" firestore:"fx_rate_used,omitempty"`       // Exchange rate if converted
	FXRateTimestamp string  `json:"fx_rate_timestamp,omitempty" firestore:"fx_rate_timestamp,omitempty"` // When rate was obtained
}

// OrderLine represents a line item in an order
type OrderLine struct {
	LineID    string `json:"line_id" firestore:"line_id"`
	SKU       string `json:"sku" firestore:"sku"`
	ProductID string `json:"product_id,omitempty" firestore:"product_id,omitempty"`
	VariantID string `json:"variant_id,omitempty" firestore:"variant_id,omitempty"`
	Title     string `json:"title,omitempty" firestore:"title,omitempty"`

	// Quantities
	Quantity          int `json:"quantity" firestore:"quantity"`
	FulfilledQuantity int `json:"fulfilled_quantity" firestore:"fulfilled_quantity"`
	CancelledQuantity int `json:"cancelled_quantity" firestore:"cancelled_quantity"`

	// Pricing Snapshot
	UnitPrice Money   `json:"unit_price" firestore:"unit_price"`
	LineTotal Money   `json:"line_total" firestore:"line_total"`
	Tax       Money   `json:"tax" firestore:"tax"`
	TaxRate   float64 `json:"tax_rate,omitempty" firestore:"tax_rate,omitempty"` // e.g. 0.20 for 20% VAT

	// Fulfilment
	FulfilmentType     string `json:"fulfilment_type" firestore:"fulfilment_type"`                               // stock, dropship, network
	FulfilmentSourceID string `json:"fulfilment_source_id,omitempty" firestore:"fulfilment_source_id,omitempty"` // warehouse_id or supplier_id

	// Status
	Status string `json:"status" firestore:"status"` // pending, allocated, fulfilled, cancelled
}

// OrderImportJob represents an order import background job
type OrderImportJob struct {
	JobID            string                 `json:"job_id" firestore:"job_id"`
	TenantID         string                 `json:"tenant_id" firestore:"tenant_id"`
	Type             string                 `json:"type" firestore:"type"` // "order_import"
	Channel          string                 `json:"channel" firestore:"channel"`
	ChannelAccountID string                 `json:"channel_account_id" firestore:"channel_account_id"`
	DateFrom         string                 `json:"date_from,omitempty" firestore:"date_from,omitempty"`
	DateTo           string                 `json:"date_to,omitempty" firestore:"date_to,omitempty"`
	Status           string                 `json:"status" firestore:"status"` // pending, running, completed, failed
	OrdersImported   int                    `json:"orders_imported,omitempty" firestore:"orders_imported,omitempty"`
	OrdersFailed     int                    `json:"orders_failed,omitempty" firestore:"orders_failed,omitempty"`
	Errors           []string               `json:"errors,omitempty" firestore:"errors,omitempty"`
	CreatedAt        string                 `json:"created_at" firestore:"created_at"`
	UpdatedAt        string                 `json:"updated_at" firestore:"updated_at"`
	StartedAt        string                 `json:"started_at,omitempty" firestore:"started_at,omitempty"`
	CompletedAt      string                 `json:"completed_at,omitempty" firestore:"completed_at,omitempty"`
	Progress         map[string]interface{} `json:"progress,omitempty" firestore:"progress,omitempty"`
}