package models

import "time"

// ============================================================================
// SHIPMENT MODEL
// ============================================================================
// A Shipment represents a single physical parcel being despatched.
// One order can have multiple shipments (split fulfilment).
// Multiple orders can share one shipment (merged orders).
//
// Firestore: tenants/{tenant_id}/shipments/{shipment_id}
// The order document references shipment IDs via shipment_ids []string
// ============================================================================

type Shipment struct {
	ShipmentID string `json:"shipment_id" firestore:"shipment_id"`
	TenantID   string `json:"tenant_id" firestore:"tenant_id"`

	// Source order(s) — normally one, multiple for merged orders
	OrderIDs []string `json:"order_ids" firestore:"order_ids"`

	// Which line items from the order(s) this shipment covers
	// Maps order_id → []line_id
	OrderLines map[string][]string `json:"order_lines" firestore:"order_lines"`

	// Fulfilment source this shipment originates from
	FulfilmentSourceID   string `json:"fulfilment_source_id" firestore:"fulfilment_source_id"`
	FulfilmentSourceType string `json:"fulfilment_source_type" firestore:"fulfilment_source_type"` // own_warehouse, 3pl, fba, dropship

	// Merge group — populated if this shipment covers merged orders
	ShipmentGroupID string `json:"shipment_group_id,omitempty" firestore:"shipment_group_id,omitempty"`

	// Carrier & service — empty until label generated
	CarrierID   string `json:"carrier_id,omitempty" firestore:"carrier_id,omitempty"`
	ServiceCode string `json:"service_code,omitempty" firestore:"service_code,omitempty"`
	ServiceName string `json:"service_name,omitempty" firestore:"service_name,omitempty"`

	// Label & tracking — empty until generated
	TrackingNumber string `json:"tracking_number,omitempty" firestore:"tracking_number,omitempty"`
	TrackingURL    string `json:"tracking_url,omitempty" firestore:"tracking_url,omitempty"`
	LabelURL       string `json:"label_url,omitempty" firestore:"label_url,omitempty"`
	LabelFormat    string `json:"label_format,omitempty" firestore:"label_format,omitempty"` // PDF, ZPL, PNG
	LabelData      []byte `json:"label_data,omitempty" firestore:"label_data,omitempty"`      // Raw label bytes if stored

	// Addresses
	FromAddress ShipmentAddress `json:"from_address" firestore:"from_address"`
	ToAddress   ShipmentAddress `json:"to_address" firestore:"to_address"`

	// Parcel details
	Parcels []ShipmentParcel `json:"parcels" firestore:"parcels"`

	// Cost
	Cost     float64 `json:"cost,omitempty" firestore:"cost,omitempty"`
	Currency string  `json:"currency,omitempty" firestore:"currency,omitempty"`

	// Status lifecycle
	// planned → label_generated → despatched → delivered | failed | returned | voided
	Status string `json:"status" firestore:"status"`

	// Marketplace reporting — has tracking been sent back to the marketplace?
	// One entry per order_id in order_ids
	MarketplaceReporting []MarketplaceTrackingReport `json:"marketplace_reporting,omitempty" firestore:"marketplace_reporting,omitempty"`

	// Workflow that assigned this carrier/source
	WorkflowID          string `json:"workflow_id,omitempty" firestore:"workflow_id,omitempty"`
	WorkflowExecutionID string `json:"workflow_execution_id,omitempty" firestore:"workflow_execution_id,omitempty"`

	// Special handling flags
	RequiresSignature  bool `json:"requires_signature" firestore:"requires_signature"`
	SaturdayDelivery   bool `json:"saturday_delivery" firestore:"saturday_delivery"`
	InsuranceValue     float64 `json:"insurance_value,omitempty" firestore:"insurance_value,omitempty"`

	// Timestamps
	CreatedAt         time.Time  `json:"created_at" firestore:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at" firestore:"updated_at"`
	LabelGeneratedAt  *time.Time `json:"label_generated_at,omitempty" firestore:"label_generated_at,omitempty"`
	DespatchedAt      *time.Time `json:"despatched_at,omitempty" firestore:"despatched_at,omitempty"`
	DeliveredAt       *time.Time `json:"delivered_at,omitempty" firestore:"delivered_at,omitempty"`
	EstimatedDelivery *time.Time `json:"estimated_delivery,omitempty" firestore:"estimated_delivery,omitempty"`
}

// ShipmentAddress is the address on a shipment label.
// Separate from models.Address to include company and phone which labels need.
type ShipmentAddress struct {
	Name         string `json:"name" firestore:"name"`
	Company      string `json:"company,omitempty" firestore:"company,omitempty"`
	AddressLine1 string `json:"address_line1" firestore:"address_line1"`
	AddressLine2 string `json:"address_line2,omitempty" firestore:"address_line2,omitempty"`
	AddressLine3 string `json:"address_line3,omitempty" firestore:"address_line3,omitempty"`
	City         string `json:"city" firestore:"city"`
	County       string `json:"county,omitempty" firestore:"county,omitempty"`
	PostalCode   string `json:"postal_code" firestore:"postal_code"`
	Country      string `json:"country" firestore:"country"` // ISO 2-letter
	Phone        string `json:"phone,omitempty" firestore:"phone,omitempty"`
	Email        string `json:"email,omitempty" firestore:"email,omitempty"`
}

// ShipmentParcel represents a single physical parcel within a shipment.
// A multi-parcel shipment generates one tracking number per parcel with most carriers.
type ShipmentParcel struct {
	Weight      float64 `json:"weight" firestore:"weight"`           // kg
	Length      float64 `json:"length" firestore:"length"`           // cm
	Width       float64 `json:"width" firestore:"width"`             // cm
	Height      float64 `json:"height" firestore:"height"`           // cm
	Description string  `json:"description,omitempty" firestore:"description,omitempty"`
	Reference   string  `json:"reference,omitempty" firestore:"reference,omitempty"` // Parcel-level ref
}

// MarketplaceTrackingReport records whether tracking has been submitted back
// to a marketplace channel for a specific order.
type MarketplaceTrackingReport struct {
	OrderID          string     `json:"order_id" firestore:"order_id"`
	Channel          string     `json:"channel" firestore:"channel"`
	ChannelAccountID string     `json:"channel_account_id" firestore:"channel_account_id"`
	ExternalOrderID  string     `json:"external_order_id" firestore:"external_order_id"`
	Status           string     `json:"status" firestore:"status"` // pending, submitted, confirmed, failed
	ReportedAt       *time.Time `json:"reported_at,omitempty" firestore:"reported_at,omitempty"`
	Error            string     `json:"error,omitempty" firestore:"error,omitempty"`
	Attempts         int        `json:"attempts" firestore:"attempts"`
}

// ============================================================================
// SHIPMENT GROUP MODEL
// ============================================================================
// A ShipmentGroup links multiple orders that have been merged into one shipment.
// The original orders are NOT modified — they retain their own IDs and records.
// This group is the coordination point for label generation and marketplace reporting.
//
// Firestore: tenants/{tenant_id}/shipment_groups/{group_id}
// ============================================================================

type ShipmentGroup struct {
	GroupID  string `json:"group_id" firestore:"group_id"`
	TenantID string `json:"tenant_id" firestore:"tenant_id"`

	// Orders in this merge group
	OrderIDs []string `json:"order_ids" firestore:"order_ids"`

	// The single shipment generated for this group
	ShipmentID string `json:"shipment_id,omitempty" firestore:"shipment_id,omitempty"`

	// Status
	Status string `json:"status" firestore:"status"` // pending, shipped, cancelled

	// Who merged and why
	MergedBy     string `json:"merged_by" firestore:"merged_by"`         // "workflow" or user ID
	MergeReason  string `json:"merge_reason,omitempty" firestore:"merge_reason,omitempty"`

	// Timestamps
	CreatedAt time.Time  `json:"created_at" firestore:"created_at"`
	UpdatedAt time.Time  `json:"updated_at" firestore:"updated_at"`
	ShippedAt *time.Time `json:"shipped_at,omitempty" firestore:"shipped_at,omitempty"`
}

// ============================================================================
// PURCHASE ORDER MODEL
// ============================================================================
// A PurchaseOrder is created when an order line routes to a dropship source.
// It instructs the supplier to ship directly to the customer.
//
// Firestore: tenants/{tenant_id}/purchase_orders/{po_id}
// ============================================================================

type PurchaseOrder struct {
	POID     string `json:"po_id" firestore:"po_id"`
	TenantID string `json:"tenant_id" firestore:"tenant_id"`

	// PO number (user-facing, sequential)
	PONumber string `json:"po_number" firestore:"po_number"`

	// Supplier
	SupplierID         string `json:"supplier_id" firestore:"supplier_id"`
	SupplierName       string `json:"supplier_name,omitempty" firestore:"supplier_name,omitempty"`
	FulfilmentSourceID string `json:"fulfilment_source_id" firestore:"fulfilment_source_id"`

	// PO type and ordering method
	Type        string `json:"type" firestore:"type"`         // standard, dropship
	OrderMethod string `json:"order_method" firestore:"order_method"` // email, webhook, manual, ftp

	// Source orders — which orders triggered this PO
	OrderIDs []string `json:"order_ids" firestore:"order_ids"`

	// Line items on this PO
	Lines []POLine `json:"lines" firestore:"lines"`

	// Goods-in receipts (appended, never edited)
	Receipts []POReceipt `json:"receipts,omitempty" firestore:"receipts,omitempty"`

	// Delivery address (the customer's address for dropship)
	DeliverToAddress ShipmentAddress `json:"deliver_to_address" firestore:"deliver_to_address"`

	// Status lifecycle
	// draft → sent → partially_received | received | cancelled
	Status string `json:"status" firestore:"status"`

	// Send method used
	SentVia string `json:"sent_via,omitempty" firestore:"sent_via,omitempty"` // email, webhook, manual

	// Dropship only
	DropshipOrderID string `json:"dropship_order_id,omitempty" firestore:"dropship_order_id,omitempty"`

	// Tracking from supplier (if they provide it)
	TrackingNumber string `json:"tracking_number,omitempty" firestore:"tracking_number,omitempty"`
	TrackingURL    string `json:"tracking_url,omitempty" firestore:"tracking_url,omitempty"`
	CarrierName    string `json:"carrier_name,omitempty" firestore:"carrier_name,omitempty"`

	// Financials
	TotalCost float64 `json:"total_cost,omitempty" firestore:"total_cost,omitempty"`
	Currency  string  `json:"currency,omitempty" firestore:"currency,omitempty"`

	// Notes
	SupplierNotes string `json:"supplier_notes,omitempty" firestore:"supplier_notes,omitempty"`
	InternalNotes string `json:"internal_notes,omitempty" firestore:"internal_notes,omitempty"`
	Notes         string `json:"notes,omitempty" firestore:"notes,omitempty"`

	// Timestamps
	CreatedAt      time.Time  `json:"created_at" firestore:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at" firestore:"updated_at"`
	SentAt         *time.Time `json:"sent_at,omitempty" firestore:"sent_at,omitempty"`
	ExpectedAt     *time.Time `json:"expected_at,omitempty" firestore:"expected_at,omitempty"`
	ShippedAt      *time.Time `json:"shipped_at,omitempty" firestore:"shipped_at,omitempty"`
	AcknowledgedAt *time.Time `json:"acknowledged_at,omitempty" firestore:"acknowledged_at,omitempty"`
	DeliveredAt    *time.Time `json:"delivered_at,omitempty" firestore:"delivered_at,omitempty"`

	// Audit
	CreatedBy string `json:"created_by,omitempty" firestore:"created_by,omitempty"`
}

type POLine struct {
	LineID      string  `json:"line_id" firestore:"line_id"`
	OrderID     string  `json:"order_id,omitempty" firestore:"order_id,omitempty"`
	OrderLineID string  `json:"order_line_id,omitempty" firestore:"order_line_id,omitempty"`
	ProductID   string  `json:"product_id,omitempty" firestore:"product_id,omitempty"`
	SKU         string  `json:"sku" firestore:"sku"` // internal SKU
	InternalSKU string  `json:"internal_sku,omitempty" firestore:"internal_sku,omitempty"`
	SupplierSKU string  `json:"supplier_sku,omitempty" firestore:"supplier_sku,omitempty"`
	Description string  `json:"description,omitempty" firestore:"description,omitempty"`
	Title       string  `json:"title" firestore:"title"`
	QtyOrdered  int     `json:"qty_ordered" firestore:"qty_ordered"`
	QtyReceived int     `json:"qty_received" firestore:"qty_received"`
	Quantity    int     `json:"quantity" firestore:"quantity"` // alias for QtyOrdered for backwards compat
	UnitCost    float64 `json:"unit_cost,omitempty" firestore:"unit_cost,omitempty"`
	Currency    string  `json:"currency,omitempty" firestore:"currency,omitempty"`
	Notes       string  `json:"notes,omitempty" firestore:"notes,omitempty"`
}

// POReceipt is appended to a PO when goods are received.
type POReceipt struct {
	ReceiptID   string          `json:"receipt_id" firestore:"receipt_id"`
	ReceivedAt  time.Time       `json:"received_at" firestore:"received_at"`
	ReceivedBy  string          `json:"received_by" firestore:"received_by"`
	Lines       []ReceiptLine   `json:"lines" firestore:"lines"`
	Notes       string          `json:"notes,omitempty" firestore:"notes,omitempty"`
}

// ReceiptLine records how many units were received for one PO line.
type ReceiptLine struct {
	LineID      string `json:"line_id" firestore:"line_id"`
	QtyReceived int    `json:"qty_received" firestore:"qty_received"`
	Variance    int    `json:"variance" firestore:"variance"` // positive=overage, negative=shortage
	Notes       string `json:"notes,omitempty" firestore:"notes,omitempty"`
}

// ============================================================================
// SHIPMENT STATUS CONSTANTS
// ============================================================================

const (
	ShipmentStatusPlanned        = "planned"
	ShipmentStatusLabelGenerated = "label_generated"
	ShipmentStatusDespatched     = "despatched"
	ShipmentStatusDelivered      = "delivered"
	ShipmentStatusFailed         = "failed"
	ShipmentStatusReturned       = "returned"
	ShipmentStatusVoided         = "voided"
)

const (
	POStatusDraft             = "draft"
	POStatusSent              = "sent"
	POStatusAcknowledged      = "acknowledged"
	POStatusPartiallyReceived = "partially_received"
	POStatusReceived          = "received"
	POStatusShipped           = "shipped"
	POStatusDelivered         = "delivered"
	POStatusCancelled         = "cancelled"

	POTypeStandard = "standard"
	POTypeDropship = "dropship"

	POOrderMethodEmail   = "email"
	POOrderMethodFTP     = "ftp"
	POOrderMethodWebhook = "webhook"
	POOrderMethodManual  = "manual"
)

const (
	TrackingReportPending   = "pending"
	TrackingReportSubmitted = "submitted"
	TrackingReportConfirmed = "confirmed"
	TrackingReportFailed    = "failed"
)
