package models

import "time"

// ============================================================================
// RMA MODEL
// Collection: tenants/{tenant_id}/rmas/{rma_id}
// ============================================================================

type RMA struct {
	RMAID             string    `json:"rma_id" firestore:"rma_id"`
	TenantID          string    `json:"tenant_id" firestore:"tenant_id"`
	RMANumber         string    `json:"rma_number" firestore:"rma_number"` // e.g. RMA-2026-0042
	OrderID           string    `json:"order_id" firestore:"order_id"`
	OrderNumber       string    `json:"order_number" firestore:"order_number"`
	Channel           string    `json:"channel" firestore:"channel"` // amazon | ebay | temu | manual
	ChannelAccountID  string    `json:"channel_account_id,omitempty" firestore:"channel_account_id,omitempty"`
	MarketplaceRMAID  string    `json:"marketplace_rma_id,omitempty" firestore:"marketplace_rma_id,omitempty"`

	// Status lifecycle:
	// requested → authorised → awaiting_return → received → inspected → resolved
	Status string `json:"status" firestore:"status"`

	// RMA type: return | exchange | resend | refund (Task 16)
	RMAType string `json:"rma_type,omitempty" firestore:"rma_type,omitempty"`

	Customer RMACustomer `json:"customer" firestore:"customer"`
	Lines    []RMALine   `json:"lines" firestore:"lines"`

	// Resolution
	RefundAction    string    `json:"refund_action,omitempty" firestore:"refund_action,omitempty"` // full_refund | partial_refund | exchange | credit_note | none
	RefundAmount    float64   `json:"refund_amount,omitempty" firestore:"refund_amount,omitempty"`
	RefundCurrency  string    `json:"refund_currency,omitempty" firestore:"refund_currency,omitempty"`
	RefundIssuedAt  *time.Time `json:"refund_issued_at,omitempty" firestore:"refund_issued_at,omitempty"`
	RefundReference string    `json:"refund_reference,omitempty" firestore:"refund_reference,omitempty"`

	// Resend workflow fields (Task 16)
	ResendShippingName     string `json:"resend_shipping_name,omitempty" firestore:"resend_shipping_name,omitempty"`
	ResendAddressLine1     string `json:"resend_address_line1,omitempty" firestore:"resend_address_line1,omitempty"`
	ResendAddressLine2     string `json:"resend_address_line2,omitempty" firestore:"resend_address_line2,omitempty"`
	ResendCity             string `json:"resend_city,omitempty" firestore:"resend_city,omitempty"`
	ResendPostalCode       string `json:"resend_postal_code,omitempty" firestore:"resend_postal_code,omitempty"`
	ResendCountry          string `json:"resend_country,omitempty" firestore:"resend_country,omitempty"`
	ResendShippingService  string `json:"resend_shipping_service,omitempty" firestore:"resend_shipping_service,omitempty"`

	// Channel refund submission tracking (Task 16)
	ChannelRefundSubmitted bool   `json:"channel_refund_submitted,omitempty" firestore:"channel_refund_submitted,omitempty"`
	ChannelRefundID        string `json:"channel_refund_id,omitempty" firestore:"channel_refund_id,omitempty"`

	TrackingNumber string `json:"tracking_number,omitempty" firestore:"tracking_number,omitempty"`
	LabelURL       string `json:"label_url,omitempty" firestore:"label_url,omitempty"`
	Notes          string `json:"notes,omitempty" firestore:"notes,omitempty"`

	// Activity log
	Timeline []RMAEvent `json:"timeline,omitempty" firestore:"timeline,omitempty"`

	CreatedBy  string     `json:"created_by" firestore:"created_by"` // user_id or "system"
	CreatedAt  time.Time  `json:"created_at" firestore:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at" firestore:"updated_at"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty" firestore:"resolved_at,omitempty"`
}

type RMACustomer struct {
	Name    string `json:"name" firestore:"name"`
	Email   string `json:"email,omitempty" firestore:"email,omitempty"`
	Address string `json:"address,omitempty" firestore:"address,omitempty"` // formatted string
}

type RMALine struct {
	LineID      string `json:"line_id" firestore:"line_id"`
	ProductID   string `json:"product_id,omitempty" firestore:"product_id,omitempty"`
	ProductName string `json:"product_name" firestore:"product_name"`
	SKU         string `json:"sku" firestore:"sku"`
	QtyRequested int   `json:"qty_requested" firestore:"qty_requested"`
	QtyReceived  int   `json:"qty_received" firestore:"qty_received"`

	// Customer-supplied reason
	ReasonCode   string `json:"reason_code,omitempty" firestore:"reason_code,omitempty"` // not_as_described | damaged | changed_mind | wrong_item | defective | other
	ReasonDetail string `json:"reason_detail,omitempty" firestore:"reason_detail,omitempty"`

	// Per-line refund (Task 16)
	RefundAmount   float64 `json:"refund_amount,omitempty" firestore:"refund_amount,omitempty"`
	RefundCurrency string  `json:"refund_currency,omitempty" firestore:"refund_currency,omitempty"`

	// Set after inspection
	Condition          string `json:"condition,omitempty" firestore:"condition,omitempty"`           // resaleable | damaged | missing | wrong_item
	Disposition        string `json:"disposition,omitempty" firestore:"disposition,omitempty"`        // restock | quarantine | write_off | return_to_supplier
	RestockLocationID  string `json:"restock_location_id,omitempty" firestore:"restock_location_id,omitempty"`
	RestockQty         int    `json:"restock_qty,omitempty" firestore:"restock_qty,omitempty"`
	PendingRestockQty  int    `json:"pending_restock_qty,omitempty" firestore:"pending_restock_qty,omitempty"` // simple inventory mode
	Restocked          bool   `json:"restocked,omitempty" firestore:"restocked,omitempty"`
}

type RMAEvent struct {
	EventID   string    `json:"event_id" firestore:"event_id"`
	Status    string    `json:"status" firestore:"status"`
	Note      string    `json:"note,omitempty" firestore:"note,omitempty"`
	CreatedBy string    `json:"created_by" firestore:"created_by"`
	CreatedAt time.Time `json:"created_at" firestore:"created_at"`
}

// ── RMA Status Constants ──────────────────────────────────────────────────────

const (
	RMAStatusRequested     = "requested"
	RMAStatusAuthorised    = "authorised"
	RMAStatusAwaitingReturn = "awaiting_return"
	RMAStatusReceived      = "received"
	RMAStatusInspected     = "inspected"
	RMAStatusResolved      = "resolved"
)

// ── Reason Codes ──────────────────────────────────────────────────────────────

const (
	RMAReasonNotAsDescribed = "not_as_described"
	RMAReasonDamaged        = "damaged"
	RMAReasonChangedMind    = "changed_mind"
	RMAReasonWrongItem      = "wrong_item"
	RMAReasonDefective      = "defective"
	RMAReasonOther          = "other"
)

// ── Condition Constants ───────────────────────────────────────────────────────

const (
	RMAConditionResaleable = "resaleable"
	RMAConditionDamaged    = "damaged"
	RMAConditionMissing    = "missing"
	RMAConditionWrongItem  = "wrong_item"
)

// ── Disposition Constants ─────────────────────────────────────────────────────

const (
	RMADispositionRestock          = "restock"
	RMADispositionQuarantine       = "quarantine"
	RMADispositionWriteOff         = "write_off"
	RMADispositionReturnToSupplier = "return_to_supplier"
)
