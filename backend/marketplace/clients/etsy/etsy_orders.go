package etsy

// ============================================================================
// ETSY ORDERS CLIENT
// ============================================================================
// Etsy uses the term "receipt" for orders.
// Tracking is pushed via POST /receipts/{receipt_id}/tracking_codes.
// ============================================================================

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// ── Receipt (Order) types ──────────────────────────────────────────────────────

type BuyerAddress struct {
	FirstLine   string `json:"first_line"`
	SecondLine  string `json:"second_line"`
	City        string `json:"city"`
	State       string `json:"state"`
	Zip         string `json:"zip"`
	CountryISO  string `json:"country_iso"`
	Name        string `json:"name"`
}

type ReceiptTransaction struct {
	TransactionID int64   `json:"transaction_id"`
	Title         string  `json:"title"`
	SKU           string  `json:"sku"`
	Quantity      int     `json:"quantity"`
	Price         Money   `json:"price"`
	ListingID     int64   `json:"listing_id"`
	ProductID     int64   `json:"product_id,omitempty"`
	ImageURL      string  `json:"image_url,omitempty"`
}

type Receipt struct {
	ReceiptID      int64                `json:"receipt_id"`
	Status         string               `json:"status"` // "open", "completed", "paid", "cancelled"
	IsShipped      bool                 `json:"is_shipped"`
	IsPaid         bool                 `json:"is_paid"`
	CreateTimestamp int64               `json:"create_timestamp"`
	UpdateTimestamp int64               `json:"update_timestamp"`
	ShipAddress    BuyerAddress         `json:"ship_address"`
	BuyerUserID    int64                `json:"buyer_user_id"`
	BuyerEmail     string               `json:"buyer_email,omitempty"`
	Message        string               `json:"message_from_buyer,omitempty"`
	Transactions   []ReceiptTransaction `json:"transactions"`
	TotalPrice     Money                `json:"total_price"`
	TotalShipping  Money                `json:"total_shipping_cost"`
	TotalTax       Money                `json:"total_tax_cost"`
	Shipments      []ReceiptShipment    `json:"shipments,omitempty"`
}

type ReceiptShipment struct {
	ReceiptShippingID int64  `json:"receipt_shipping_id"`
	TrackingCode      string `json:"tracking_code"`
	CarrierName       string `json:"carrier_name"`
	TrackingURL       string `json:"tracking_url,omitempty"`
}

type ReceiptsResponse struct {
	Count   int       `json:"count"`
	Results []Receipt `json:"results"`
}

// ── GetReceipts (paginated) ────────────────────────────────────────────────────

// GetReceipts fetches all receipts (orders) in a time window.
// minCreated / maxCreated are Unix timestamps (seconds).
func (c *Client) GetReceipts(minCreated, maxCreated int64, offset, limit int) (*ReceiptsResponse, error) {
	q := url.Values{}
	if minCreated > 0 {
		q.Set("min_created", strconv.FormatInt(minCreated, 10))
	}
	if maxCreated > 0 {
		q.Set("max_created", strconv.FormatInt(maxCreated, 10))
	}
	q.Set("offset", strconv.Itoa(offset))
	q.Set("limit", strconv.Itoa(limit))
	q.Set("was_paid", "true") // only paid orders

	raw, err := c.get(fmt.Sprintf("/application/shops/%d/receipts", c.ShopID), q)
	if err != nil {
		return nil, err
	}
	var resp ReceiptsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode receipts: %w", err)
	}
	return &resp, nil
}

// GetReceipt fetches a single receipt (order) by ID.
func (c *Client) GetReceipt(receiptID int64) (*Receipt, error) {
	raw, err := c.get(fmt.Sprintf("/application/shops/%d/receipts/%d", c.ShopID, receiptID), nil)
	if err != nil {
		return nil, err
	}
	var receipt Receipt
	if err := json.Unmarshal(raw, &receipt); err != nil {
		return nil, fmt.Errorf("decode receipt: %w", err)
	}
	return &receipt, nil
}

// FetchNewReceipts retrieves all paid, unshipped receipts created in a window.
// It paginates through all pages automatically.
func (c *Client) FetchNewReceipts(createdAfter, createdBefore time.Time) ([]Receipt, error) {
	var allReceipts []Receipt
	offset := 0
	const pageSize = 100

	minTS := createdAfter.Unix()
	maxTS := createdBefore.Unix()

	for {
		page, err := c.GetReceipts(minTS, maxTS, offset, pageSize)
		if err != nil {
			return nil, fmt.Errorf("fetch receipts page (offset=%d): %w", offset, err)
		}
		allReceipts = append(allReceipts, page.Results...)
		if len(allReceipts) >= page.Count || len(page.Results) < pageSize {
			break
		}
		offset += pageSize
	}
	return allReceipts, nil
}

// ── Tracking push-back ─────────────────────────────────────────────────────────

type CreateTrackingRequest struct {
	TrackingCode    string `json:"tracking_code"`
	CarrierName     string `json:"carrier_name"`
	SendBDC         bool   `json:"send_bdc,omitempty"`
	TrackingURL     string `json:"tracking_url,omitempty"`
	OverwriteExisting bool `json:"overwrite_existing,omitempty"`
}

// CreateReceiptShipment pushes tracking information back to an Etsy order.
func (c *Client) CreateReceiptShipment(receiptID int64, req *CreateTrackingRequest) (*ReceiptShipment, error) {
	raw, err := c.post(fmt.Sprintf("/application/shops/%d/receipts/%d/tracking_codes", c.ShopID, receiptID), req)
	if err != nil {
		return nil, err
	}
	var shipment ReceiptShipment
	if err := json.Unmarshal(raw, &shipment); err != nil {
		// Etsy sometimes returns the full receipt — try to extract shipment
		var receipt Receipt
		if err2 := json.Unmarshal(raw, &receipt); err2 == nil && len(receipt.Shipments) > 0 {
			return &receipt.Shipments[len(receipt.Shipments)-1], nil
		}
		return nil, fmt.Errorf("decode shipment: %w", err)
	}
	return &shipment, nil
}
