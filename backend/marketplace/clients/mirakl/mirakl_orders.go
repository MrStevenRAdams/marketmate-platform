package mirakl

// ============================================================================
// MIRAKL ORDER API METHODS
// ============================================================================
// Implements Mirakl MMP Seller REST API — Orders section.
//
// Key endpoints used:
//   OR11  GET  /api/orders                          List orders (paginated)
//   OR21  PUT  /api/orders/{order_id}/accept        Accept/refuse order lines
//   OR23  PUT  /api/orders/{order_id}/tracking      Update carrier tracking
//   OR24  PUT  /api/orders/{order_id}/shipping      Validate shipment
//   OR28  PUT  /api/orders/{order_id}/refund        Refund order lines
//   OR29  PUT  /api/orders/{order_id}/cancel        Full order cancellation
//   OR30  PUT  /api/orders/{order_id}/cancel/lines  Cancel specific lines
// ============================================================================

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ============================================================================
// ORDER TYPES
// ============================================================================

// Order represents a Mirakl marketplace order (OR11 response shape)
type Order struct {
	OrderID           string      `json:"order_id"`           // Mirakl internal order ID
	OrderState        string      `json:"order_state"`         // STAGING, WAITING_ACCEPTANCE, WAITING_DEBIT, etc.
	OrderStateLabelEN string      `json:"order_state_label"`
	CustomerReference string      `json:"customer_reference_for_customer"`
	SellerReference   string      `json:"customer_reference_for_seller,omitempty"`
	DateCreated       string      `json:"date_created"`        // ISO 8601
	DateUpdated       string      `json:"date_updated"`
	LastUpdatedDate   string      `json:"last_updated_date,omitempty"`
	PaymentType       string      `json:"payment_type"`        // "PAY_ON_ACCEPTANCE", "PAY_ON_DELIVERY"
	Customer          Customer    `json:"customer"`
	BillingAddress    Address     `json:"billing_address"`
	ShippingAddress   Address     `json:"shipping_address"`
	OrderLines        []OrderLine `json:"order_lines"`
	Price             float64     `json:"price"`               // Grand total
	Currency          string      `json:"currency_iso_code"`
	ShippingPrice     float64     `json:"shipping_price"`
	CommissionFees    float64     `json:"commission_fees,omitempty"`
	ShippingDeadline  string      `json:"shipping_deadline,omitempty"`
	Promotions        []Promotion `json:"promotions,omitempty"`
}

// OrderLine represents a single item within an order
type OrderLine struct {
	OrderLineID     string  `json:"order_line_id"`
	OrderLineIndex  int     `json:"order_line_index"`
	ProductSKU      string  `json:"product_sku"`
	ShopSKU         string  `json:"shop_sku"`
	OfferID         string  `json:"offer_id"`
	Title           string  `json:"offer_title"`
	Quantity        int     `json:"quantity"`
	UnitPrice       float64 `json:"unit_price"`
	TotalPrice      float64 `json:"total_price"`
	ShippingPrice   float64 `json:"shipping_price"`
	Status          string  `json:"order_line_state"`   // "STAGING", "WAITING_ACCEPTANCE", "SHIPPING", etc.
	StatusLabel     string  `json:"order_line_state_label"`
	CanRefund       bool    `json:"can_refund"`
	CanCancel       bool    `json:"cancelable"`
	TaxAmount       float64 `json:"tax_amount,omitempty"`
	ShippingTaxAmt  float64 `json:"shipping_tax_amount,omitempty"`
	ShippingCarrier string  `json:"shipping_carrier,omitempty"`
	TrackingNumber  string  `json:"tracking_number,omitempty"`
	TrackingURL     string  `json:"tracking_url,omitempty"`
	Promotions      []Promotion `json:"promotions,omitempty"`
}

// Customer holds buyer name/contact from an order
type Customer struct {
	CustomerID    string `json:"customer_id"`
	DisplayName   string `json:"display_name"`
	Firstname     string `json:"firstname"`
	Lastname      string `json:"lastname"`
	Email         string `json:"email"`
	Locale        string `json:"locale,omitempty"`
}

// Address represents a billing or shipping address
type Address struct {
	Company    string `json:"company,omitempty"`
	Civility   string `json:"civility,omitempty"`
	Firstname  string `json:"firstname"`
	Lastname   string `json:"lastname"`
	Street1    string `json:"street_1"`
	Street2    string `json:"street_2,omitempty"`
	City       string `json:"city"`
	State      string `json:"state,omitempty"`
	ZipCode    string `json:"zip_code"`
	Country    string `json:"country"`    // ISO 2-letter
	Phone      string `json:"phone,omitempty"`
	Phone2     string `json:"phone_secondary,omitempty"`
}

// Promotion holds discount information on an order or order line
type Promotion struct {
	PromotionID    string  `json:"promotion_id"`
	Title          string  `json:"title,omitempty"`
	DiscountAmount float64 `json:"discount_amount"`
	Operator       bool    `json:"applied_by_operator,omitempty"`
}

// ============================================================================
// ORDER STATE CONSTANTS
// ============================================================================

const (
	OrderStateStaging           = "STAGING"
	OrderStateWaitingAcceptance = "WAITING_ACCEPTANCE"
	OrderStateWaitingDebit      = "WAITING_DEBIT"
	OrderStateShipping          = "SHIPPING"
	OrderStateShipped           = "SHIPPED"
	OrderStateReceived          = "RECEIVED"
	OrderStateClosed            = "CLOSED"
	OrderStateRefused           = "REFUSED"
	OrderStateCanceled          = "CANCELED"
	OrderStateIncidentOpen      = "INCIDENT_OPEN"
	OrderStateIncidentClosed    = "INCIDENT_CLOSED"
)

// ============================================================================
// LIST ORDERS (OR11)
// ============================================================================

// ListOrdersOptions are filters for OR11
type ListOrdersOptions struct {
	States       []string  // e.g. ["WAITING_ACCEPTANCE", "SHIPPING"]
	StartDate    time.Time // orders created after this date
	EndDate      time.Time // orders created before this date
	StartUpdate  time.Time // orders updated after this date
	Max          int       // items per page, default 10 max 100
	Offset       int
	ChannelCodes []string  // for multi-channel instances
}

// OrderListResponse is the paginated response from OR11
type OrderListResponse struct {
	Orders     []Order `json:"orders"`
	TotalCount int     `json:"total_count"`
}

// ListOrders calls OR11 — GET /api/orders
// Fetches orders with state/date filters and pagination.
func (c *Client) ListOrders(opts ListOrdersOptions) (*OrderListResponse, error) {
	q := url.Values{}

	if len(opts.States) > 0 {
		for _, s := range opts.States {
			q.Add("order_state_codes", s)
		}
	}
	if !opts.StartDate.IsZero() {
		q.Set("start_date", opts.StartDate.UTC().Format(time.RFC3339))
	}
	if !opts.EndDate.IsZero() {
		q.Set("end_date", opts.EndDate.UTC().Format(time.RFC3339))
	}
	if !opts.StartUpdate.IsZero() {
		q.Set("start_update_date", opts.StartUpdate.UTC().Format(time.RFC3339))
	}
	max := opts.Max
	if max <= 0 {
		max = 100
	}
	q.Set("max", strconv.Itoa(max))
	q.Set("offset", strconv.Itoa(opts.Offset))

	b, err := c.get("/api/orders", q)
	if err != nil {
		return nil, fmt.Errorf("ListOrders: %w", err)
	}
	var resp OrderListResponse
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, fmt.Errorf("ListOrders: parse: %w", err)
	}
	return &resp, nil
}

// FetchAllNewOrders fetches all orders in WAITING_ACCEPTANCE state,
// paginating through results automatically.
func (c *Client) FetchAllNewOrders(since time.Time) ([]Order, error) {
	var all []Order
	offset := 0
	for {
		resp, err := c.ListOrders(ListOrdersOptions{
			States:    []string{OrderStateWaitingAcceptance, OrderStateWaitingDebit, OrderStateShipping},
			StartDate: since,
			Max:       100,
			Offset:    offset,
		})
		if err != nil {
			return nil, err
		}
		all = append(all, resp.Orders...)
		if len(all) >= resp.TotalCount || len(resp.Orders) == 0 {
			break
		}
		offset += len(resp.Orders)
	}
	return all, nil
}

// ============================================================================
// ACCEPT ORDER LINES (OR21)
// ============================================================================

// AcceptanceDecision is the per-line decision for OR21
type AcceptanceDecision struct {
	ID       string `json:"id"`        // order_line_id
	Accepted bool   `json:"accepted"`
	Reason   string `json:"reason_code,omitempty"` // required if refused
}

// AcceptOrderLines calls OR21 — PUT /api/orders/{order_id}/accept
// Accepts or refuses individual order lines in WAITING_ACCEPTANCE status.
func (c *Client) AcceptOrderLines(orderID string, decisions []AcceptanceDecision) error {
	body := map[string]interface{}{
		"order_lines": decisions,
	}
	_, err := c.put("/api/orders/"+orderID+"/accept", body)
	if err != nil {
		return fmt.Errorf("AcceptOrderLines %s: %w", orderID, err)
	}
	return nil
}

// AcceptAllLines is a convenience wrapper that accepts every line in an order.
func (c *Client) AcceptAllLines(order Order) error {
	decisions := make([]AcceptanceDecision, 0, len(order.OrderLines))
	for _, line := range order.OrderLines {
		if line.Status == OrderStateWaitingAcceptance {
			decisions = append(decisions, AcceptanceDecision{
				ID:       line.OrderLineID,
				Accepted: true,
			})
		}
	}
	if len(decisions) == 0 {
		return nil // nothing to accept
	}
	return c.AcceptOrderLines(order.OrderID, decisions)
}

// ============================================================================
// TRACKING (OR23 + OR24)
// ============================================================================

// TrackingUpdate is the payload for OR23 — updating carrier/tracking on an order
type TrackingUpdate struct {
	CarrierCode    string `json:"carrier_code,omitempty"`    // registered carrier code from SH21
	CarrierName    string `json:"carrier_name,omitempty"`    // free-text if not registered
	TrackingNumber string `json:"tracking_number"`
	TrackingURL    string `json:"tracking_url,omitempty"`
}

// UpdateTracking calls OR23 — PUT /api/orders/{order_id}/tracking
// Sets carrier and tracking number. Does NOT mark as shipped (use ValidateShipment for that).
func (c *Client) UpdateTracking(orderID string, tracking TrackingUpdate) error {
	_, err := c.put("/api/orders/"+orderID+"/tracking", tracking)
	if err != nil {
		return fmt.Errorf("UpdateTracking %s: %w", orderID, err)
	}
	return nil
}

// ShipmentValidation is the payload for OR24
type ShipmentValidation struct {
	OrderLines     []ShippedLine `json:"order_lines,omitempty"`
	CarrierCode    string        `json:"carrier_code,omitempty"`
	CarrierName    string        `json:"carrier_name,omitempty"`
	TrackingNumber string        `json:"tracking_number,omitempty"`
	TrackingURL    string        `json:"tracking_url,omitempty"`
}

// ShippedLine allows per-line shipment validation
type ShippedLine struct {
	ID             string `json:"id"` // order_line_id
	Quantity       int    `json:"quantity,omitempty"`
	TrackingNumber string `json:"tracking_number,omitempty"`
	CarrierCode    string `json:"carrier_code,omitempty"`
	CarrierName    string `json:"carrier_name,omitempty"`
	TrackingURL    string `json:"tracking_url,omitempty"`
}

// ValidateShipment calls OR24 — PUT /api/orders/{order_id}/shipping
// Marks order as shipped. This transitions the order to SHIPPED state.
func (c *Client) ValidateShipment(orderID string, shipment ShipmentValidation) error {
	_, err := c.put("/api/orders/"+orderID+"/shipping", shipment)
	if err != nil {
		return fmt.Errorf("ValidateShipment %s: %w", orderID, err)
	}
	return nil
}

// PushTracking is the combined convenience method:
// 1. Sets tracking info (OR23)
// 2. Marks the order as shipped (OR24)
func (c *Client) PushTracking(orderID, carrierCode, carrierName, trackingNumber, trackingURL string) error {
	// Step 1: Set tracking
	if err := c.UpdateTracking(orderID, TrackingUpdate{
		CarrierCode:    carrierCode,
		CarrierName:    carrierName,
		TrackingNumber: trackingNumber,
		TrackingURL:    trackingURL,
	}); err != nil {
		return err
	}

	// Step 2: Validate shipment
	return c.ValidateShipment(orderID, ShipmentValidation{
		CarrierCode:    carrierCode,
		CarrierName:    carrierName,
		TrackingNumber: trackingNumber,
		TrackingURL:    trackingURL,
	})
}

// ============================================================================
// REFUNDS (OR28)
// ============================================================================

// RefundLine is the per-line refund request for OR28
type RefundLine struct {
	OrderLineID  string  `json:"order_line_id"`
	RefundAmount float64 `json:"amount"`
	ReasonCode   string  `json:"reason_code"`
	Message      string  `json:"message,omitempty"`
}

// RefundOrderLines calls OR28 — PUT /api/orders/{order_id}/refund
func (c *Client) RefundOrderLines(orderID string, lines []RefundLine) error {
	body := map[string]interface{}{
		"order_lines": lines,
	}
	_, err := c.put("/api/orders/"+orderID+"/refund", body)
	if err != nil {
		return fmt.Errorf("RefundOrderLines %s: %w", orderID, err)
	}
	return nil
}

// ============================================================================
// CANCELLATION (OR29, OR30)
// ============================================================================

// CancelOrder calls OR29 — PUT /api/orders/{order_id}/cancel
// Fully cancels an entire order.
func (c *Client) CancelOrder(orderID, reasonCode string) error {
	body := map[string]string{
		"reason_code": reasonCode,
	}
	_, err := c.put("/api/orders/"+orderID+"/cancel", body)
	if err != nil {
		return fmt.Errorf("CancelOrder %s: %w", orderID, err)
	}
	return nil
}

// CancelLineRequest is a single line cancellation for OR30
type CancelLineRequest struct {
	OrderLineID string `json:"id"`
	ReasonCode  string `json:"reason_code"`
}

// CancelOrderLines calls OR30 — PUT /api/orders/{order_id}/cancel/lines
func (c *Client) CancelOrderLines(orderID string, lines []CancelLineRequest) error {
	body := map[string]interface{}{
		"order_lines": lines,
	}
	_, err := c.put("/api/orders/"+orderID+"/cancel/lines", body)
	if err != nil {
		return fmt.Errorf("CancelOrderLines %s: %w", orderID, err)
	}
	return nil
}

// ============================================================================
// HELPERS
// ============================================================================

// FullCustomerName returns "Firstname Lastname" from a Customer
func (cu Customer) FullName() string {
	parts := []string{}
	if cu.Firstname != "" {
		parts = append(parts, cu.Firstname)
	}
	if cu.Lastname != "" {
		parts = append(parts, cu.Lastname)
	}
	if len(parts) == 0 {
		return cu.DisplayName
	}
	return strings.Join(parts, " ")
}

// FullAddressName returns the full recipient name from an address
func (a Address) FullName() string {
	parts := []string{}
	if a.Firstname != "" {
		parts = append(parts, a.Firstname)
	}
	if a.Lastname != "" {
		parts = append(parts, a.Lastname)
	}
	return strings.Join(parts, " ")
}
