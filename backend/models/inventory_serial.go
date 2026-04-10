package models

import "time"

// ============================================================================
// SERIAL NUMBER TRACKING
// ============================================================================
//
// When a Product has UseSerialNumbers = true, every stock movement must supply
// a slice of serial numbers equal in length to the quantity being moved.
//
// Serial records are stored in:
//   tenants/{tenant_id}/serial_numbers/{serial_id}
//
// They are indexed by both SKU and serial_number for fast lookup.

// SerialNumber tracks the lifecycle of a single serialised unit.
type SerialNumber struct {
	SerialID     string    `json:"serial_id"     firestore:"serial_id"`
	TenantID     string    `json:"tenant_id"     firestore:"tenant_id"`
	SKU          string    `json:"sku"           firestore:"sku"`
	ProductID    string    `json:"product_id"    firestore:"product_id"`
	SerialNumber string    `json:"serial_number" firestore:"serial_number"`

	// Current state
	Status     string `json:"status"      firestore:"status"`      // in_stock, dispatched, scrapped, transferred
	LocationID string `json:"location_id" firestore:"location_id"` // current location (empty if dispatched)
	BinrackID  string `json:"binrack_id"  firestore:"binrack_id"`  // current binrack (optional)

	// Audit trail
	ReceivedAt   *time.Time `json:"received_at,omitempty"   firestore:"received_at,omitempty"`
	DispatchedAt *time.Time `json:"dispatched_at,omitempty" firestore:"dispatched_at,omitempty"`
	ScrapppedAt  *time.Time `json:"scrapped_at,omitempty"   firestore:"scrapped_at,omitempty"`
	OrderID      string     `json:"order_id,omitempty"      firestore:"order_id,omitempty"`      // set on dispatch
	ShipmentID   string     `json:"shipment_id,omitempty"   firestore:"shipment_id,omitempty"`  // set on dispatch

	CreatedAt time.Time `json:"created_at" firestore:"created_at"`
	UpdatedAt time.Time `json:"updated_at" firestore:"updated_at"`
}

// ─── Request / response helpers ──────────────────────────────────────────────

// SerialNumbersForMovement is embedded in every stock movement request when the
// product has UseSerialNumbers = true.  The slice must have exactly Quantity
// elements — the backend rejects requests where len(SerialNumbers) != Quantity.
//
// Used by:
//   AdjustStockRequest       (manual adjustment / receipt / return)
//   TransferRequest          (warehouse transfer)
//   CreateScrap request      (scrap / write-off)
//   PO receive lines         (stock-in)
//   Shipment creation        (despatch)
type SerialNumbersForMovement struct {
	// SerialNumbers is a list of unique serial number strings, one per unit.
	// Required when the product has use_serial_numbers = true.
	// Length must equal the quantity field of the parent request.
	SerialNumbers []string `json:"serial_numbers,omitempty"`
}

// ─── Extend existing movement request types ──────────────────────────────────
// (These are defined here as separate extension structs so they can be embedded
//  or merged with the existing AdjustStockRequest etc. in inventory_handlers.go
//  without a rewrite of that file.)

// AdjustStockRequestV2 extends the existing AdjustStockRequest with serial support.
type AdjustStockRequestV2 struct {
	SKU        string `json:"sku"`
	LocationID string `json:"location_id"`
	Type       string `json:"type"`     // adjustment, receipt, return
	Quantity   int    `json:"quantity"`
	ReasonCode string `json:"reason_code"`
	Notes      string `json:"notes"`
	SerialNumbersForMovement
}

// TransferRequestV2 extends TransferRequest with serial support.
type TransferRequestV2 struct {
	SKU          string `json:"sku"`
	FromLocation string `json:"from_location"`
	ToLocation   string `json:"to_location"`
	Quantity     int    `json:"quantity"`
	Notes        string `json:"notes"`
	SerialNumbersForMovement
}

// ScrapRequestV2 extends the inline scrap request struct with serial support.
type ScrapRequestV2 struct {
	ProductID  string  `json:"product_id"  binding:"required"`
	LocationID string  `json:"location_id" binding:"required"`
	Quantity   int     `json:"quantity"    binding:"required"`
	Reason     string  `json:"reason"      binding:"required"`
	Notes      string  `json:"notes"`
	ScrapValue float64 `json:"scrap_value"`
	Currency   string  `json:"currency"`
	SerialNumbersForMovement
}

// POReceiveLineV2 extends the PO receive line with per-line serials.
// The existing StockIn frontend sends batch info; serial_numbers is additive.
type POReceiveLineV2 struct {
	LineID          string `json:"line_id"`
	QtyReceived     int    `json:"qty_received"`
	BinrackID       string `json:"binrack_id,omitempty"`
	SerialNumbersForMovement
}

// ShipmentRequestV2 extends the shipment creation payload.
// serial_numbers is a map of SKU → []serial so multi-line orders work correctly.
type ShipmentRequestV2 struct {
	OrderID      string              `json:"order_id"`
	CarrierID    string              `json:"carrier_id"`
	ServiceCode  string              `json:"service_code"`
	LabelFormat  string              `json:"label_format"`
	// SerialNumbers maps each serialised SKU in the order to its serial numbers.
	// E.g.: { "SKU-001": ["SN-A", "SN-B"], "SKU-002": ["SN-C"] }
	SerialNumbers map[string][]string `json:"serial_numbers,omitempty"`
}
