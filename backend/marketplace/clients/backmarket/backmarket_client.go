package backmarket

// ============================================================================
// BACK MARKET REST API v1 CLIENT
// ============================================================================
// Auth: API key sent as "Authorization: Basic <key>" header.
// Base URL: https://www.backmarket.com/ws  (production)
//           https://preprod.backmarket.com/ws  (staging)
// Rate limit: 100 req/min per key. Responses paginated via "next" link.
// ============================================================================

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	ProdBaseURL    = "https://www.backmarket.com/ws"
	StagingBaseURL = "https://preprod.backmarket.com/ws"
)

type Client struct {
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
}

func NewClient(apiKey string, production bool) *Client {
	base := ProdBaseURL
	if !production {
		base = StagingBaseURL
	}
	return &Client{
		APIKey:  apiKey,
		BaseURL: base,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ── HTTP helpers ──────────────────────────────────────────────────────────────

func (c *Client) doRequest(method, path string, body interface{}) ([]byte, int, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("marshal body: %w", err)
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, c.BaseURL+path, reader)
	if err != nil {
		return nil, 0, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Authorization", "Basic "+c.APIKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return nil, resp.StatusCode, fmt.Errorf("back market API %d: %s", resp.StatusCode, string(respBody))
	}
	return respBody, resp.StatusCode, nil
}

// TestConnection validates the API key by fetching the seller profile.
func (c *Client) TestConnection() error {
	_, _, err := c.doRequest("GET", "/profile", nil)
	if err != nil {
		return fmt.Errorf("back market connection test: %w", err)
	}
	return nil
}

// ── ORDER TYPES ───────────────────────────────────────────────────────────────

type OrdersResponse struct {
	Count   int     `json:"count"`
	Next    string  `json:"next"`
	Results []Order `json:"results"`
}

type Order struct {
	OrderID          int             `json:"order_id"`
	Reference        string          `json:"reference"` // "BM-XXXXX"
	State            string          `json:"state"`     // "new","to_ship","shipped","cancelled","refunded"
	DateCreation     string          `json:"date_creation"`
	DateModification string          `json:"date_modification"`
	TotalPrice       float64         `json:"total_price"`
	Currency         string          `json:"currency"`
	BillingAddress   Address         `json:"billing_address"`
	ShippingAddress  Address         `json:"shipping_address"`
	Lines            []OrderLine     `json:"lines"`
	Shipping         ShippingDetails `json:"shipping"`
}

type Address struct {
	FirstName  string `json:"first_name"`
	LastName   string `json:"last_name"`
	Company    string `json:"company"`
	Street     string `json:"street"`
	Street2    string `json:"street2"`
	PostalCode string `json:"postal_code"`
	City       string `json:"city"`
	Country    string `json:"country"` // ISO 3166-1 alpha-2
	Phone      string `json:"phone"`
	Email      string `json:"email"`
}

type OrderLine struct {
	LineID     int     `json:"line_id"`
	SellerSKU  string  `json:"seller_sku"`
	ListingID  string  `json:"listing_id"`
	Title      string  `json:"title"`
	Quantity   int     `json:"quantity"`
	UnitPrice  float64 `json:"unit_price"`
	TotalPrice float64 `json:"total_price"`
	Currency   string  `json:"currency"`
	Grade      string  `json:"grade"` // "excellent","good","fair"
	ProductID  int     `json:"product_id"`
}

type ShippingDetails struct {
	CarrierName string `json:"carrier_name"`
	TrackingURL string `json:"tracking_url"`
	TrackingID  string `json:"tracking_id"`
	Mode        string `json:"mode"` // "standard","express"
}

// GetOrders fetches orders modified since `since`, filtered by states.
// Automatically paginates.
func (c *Client) GetOrders(since time.Time, states []string) ([]Order, error) {
	params := url.Values{}
	params.Set("date_modification", since.Format("2006-01-02T15:04:05"))
	for _, s := range states {
		params.Add("state", s)
	}
	params.Set("page_size", "50")

	var all []Order
	path := "/orders?" + params.Encode()
	for path != "" {
		body, _, err := c.doRequest("GET", path, nil)
		if err != nil {
			return nil, err
		}
		var resp OrdersResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("unmarshal orders: %w", err)
		}
		all = append(all, resp.Results...)
		if resp.Next == "" {
			break
		}
		parsed, err := url.Parse(resp.Next)
		if err != nil {
			break
		}
		path = parsed.RequestURI()
	}
	log.Printf("Back Market: fetched %d orders since %s", len(all), since.Format(time.RFC3339))
	return all, nil
}

// GetOrder fetches a single order by numeric ID.
func (c *Client) GetOrder(orderID int) (*Order, error) {
	body, _, err := c.doRequest("GET", "/orders/"+strconv.Itoa(orderID), nil)
	if err != nil {
		return nil, err
	}
	var o Order
	if err := json.Unmarshal(body, &o); err != nil {
		return nil, fmt.Errorf("unmarshal order: %w", err)
	}
	return &o, nil
}

// ── TRACKING ─────────────────────────────────────────────────────────────────

type ShipOrderRequest struct {
	Lines        []ShipLine `json:"lines"`
	TrackingCode string     `json:"tracking_code"`
	Carrier      string     `json:"carrier"`
	Mode         string     `json:"mode"`
}

type ShipLine struct {
	LineID   int `json:"line_id"`
	Quantity int `json:"quantity"`
}

// ShipOrder sends tracking details to Back Market for an order.
func (c *Client) ShipOrder(orderID int, req ShipOrderRequest) error {
	_, _, err := c.doRequest("POST", fmt.Sprintf("/orders/%d/ship", orderID), req)
	return err
}

// ── LISTINGS ─────────────────────────────────────────────────────────────────

type ListingsResponse struct {
	Count   int       `json:"count"`
	Next    string    `json:"next"`
	Results []Listing `json:"results"`
}

type Listing struct {
	ListingID   string  `json:"listing_id"`
	SellerSKU   string  `json:"sku"`
	ProductID   int     `json:"product_id"`
	Price       float64 `json:"price"`
	Currency    string  `json:"currency"`
	Stock       int     `json:"quantity"`
	Grade       string  `json:"grade"`
	Description string  `json:"description"`
	State       string  `json:"state"` // "active","inactive"
	Title       string  `json:"title"`
	ImageURL    string  `json:"image_url"`
}

// GetListings fetches all active listings for this seller account.
// Paginates automatically.
func (c *Client) GetListings(pageSize int) ([]Listing, error) {
	if pageSize <= 0 {
		pageSize = 50
	}
	params := url.Values{}
	params.Set("page_size", strconv.Itoa(pageSize))

	var all []Listing
	path := "/listings?" + params.Encode()
	for path != "" {
		body, _, err := c.doRequest("GET", path, nil)
		if err != nil {
			return nil, err
		}
		var resp ListingsResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("unmarshal listings: %w", err)
		}
		all = append(all, resp.Results...)
		if resp.Next == "" {
			break
		}
		parsed, err := url.Parse(resp.Next)
		if err != nil {
			break
		}
		path = parsed.RequestURI()
	}
	return all, nil
}

type CreateListingRequest struct {
	ProductID   int     `json:"product_id"`
	SellerSKU   string  `json:"sku"`
	Price       float64 `json:"price"`
	Currency    string  `json:"currency"`
	Stock       int     `json:"quantity"`
	Grade       string  `json:"grade"`
	Description string  `json:"description"`
}

// UpsertListing creates or updates a product offer on Back Market.
func (c *Client) UpsertListing(req CreateListingRequest) (*Listing, error) {
	body, _, err := c.doRequest("POST", "/listings", req)
	if err != nil {
		return nil, err
	}
	var l Listing
	if err := json.Unmarshal(body, &l); err != nil {
		return nil, fmt.Errorf("unmarshal listing: %w", err)
	}
	return &l, nil
}

// UpdateStock updates stock quantity for a listing.
func (c *Client) UpdateStock(listingID string, quantity int) error {
	_, _, err := c.doRequest("PATCH", "/listings/"+listingID, map[string]interface{}{"quantity": quantity})
	return err
}

// UpdatePrice updates price for a listing.
func (c *Client) UpdatePrice(listingID string, price float64) error {
	_, _, err := c.doRequest("PATCH", "/listings/"+listingID, map[string]interface{}{"price": price})
	return err
}

// DeactivateListing sets a listing to inactive.
func (c *Client) DeactivateListing(listingID string) error {
	_, _, err := c.doRequest("PATCH", "/listings/"+listingID, map[string]interface{}{"state": "inactive"})
	return err
}
