package kaufland

// ============================================================================
// KAUFLAND MARKETPLACE API CLIENT
// ============================================================================
// Base URL:  https://sellerapi.kaufland.de
// Auth:      HMAC-SHA256 signature per request.
//            Sign the string:  "{METHOD}\n{URI}\n{BODY}\n{TIMESTAMP}"
//            with secret_key using HMAC-SHA256, base64-encode the result.
//            Send as header:  HMac-Sha256: {signature}
//                             Shop-Client-Key: {client_key}
//                             Shop-Timestamp: {unix_timestamp}
// Docs:      https://sellerapi.kaufland.de/
// ============================================================================

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const baseURL = "https://sellerapi.kaufland.de"

// ── Types ─────────────────────────────────────────────────────────────────────

// Client is the Kaufland Seller API client.
type Client struct {
	ClientKey  string
	SecretKey  string
	HTTPClient *http.Client
}

func NewClient(clientKey, secretKey string) *Client {
	return &Client{
		ClientKey:  clientKey,
		SecretKey:  secretKey,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// ── Auth helpers ──────────────────────────────────────────────────────────────

// signature computes HMAC-SHA256 over "{method}\n{uri}\n{body}\n{timestamp}".
func (c *Client) signature(method, uri, body string, timestamp int64) string {
	message := fmt.Sprintf("%s\n%s\n%s\n%d", method, uri, body, timestamp)
	mac := hmac.New(sha256.New, []byte(c.SecretKey))
	mac.Write([]byte(message))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// ── HTTP helper ───────────────────────────────────────────────────────────────

func (c *Client) doRequest(method, path string, body interface{}, queryParams url.Values) ([]byte, int, error) {
	endpoint := baseURL + path
	if len(queryParams) > 0 {
		endpoint += "?" + queryParams.Encode()
	}

	var bodyStr string
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("marshal request body: %w", err)
		}
		bodyStr = string(b)
		reqBody = bytes.NewReader(b)
	}

	// Compute signature using the path (not the full URL with query params)
	timestamp := time.Now().Unix()
	sig := c.signature(method, baseURL+path, bodyStr, timestamp)

	req, err := http.NewRequest(method, endpoint, reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Shop-Client-Key", c.ClientKey)
	req.Header.Set("Shop-Timestamp", strconv.FormatInt(timestamp, 10))
	req.Header.Set("HMac-Sha256", sig)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("execute request to %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		var kErr struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		if jsonErr := json.Unmarshal(respBytes, &kErr); jsonErr == nil {
			msg := kErr.Message
			if msg == "" {
				msg = kErr.Error
			}
			if msg != "" {
				return nil, resp.StatusCode, fmt.Errorf("Kaufland API error [HTTP %d]: %s", resp.StatusCode, msg)
			}
		}
		return nil, resp.StatusCode, fmt.Errorf("Kaufland API error [HTTP %d]: %s", resp.StatusCode, string(respBytes))
	}

	return respBytes, resp.StatusCode, nil
}

// ── Response wrappers ─────────────────────────────────────────────────────────

type pagedResponse struct {
	Data  json.RawMessage `json:"data"`
	Pagination *struct {
		Total  int `json:"total"`
		Offset int `json:"offset"`
		Limit  int `json:"limit"`
	} `json:"pagination"`
}

// ── Category types ────────────────────────────────────────────────────────────

type Category struct {
	CategoryID   int    `json:"category_id"`
	Title        string `json:"title"`
	URL          string `json:"url"`
	ParentID     *int   `json:"parent_id"`
}

type CategoryAttribute struct {
	Name     string      `json:"name"`
	Type     string      `json:"type"`
	Required bool        `json:"is_mandatory"`
	Values   interface{} `json:"values"`
}

// ── Unit (listing) types ──────────────────────────────────────────────────────

type Unit struct {
	UnitID           string  `json:"id_offer"`
	EAN              string  `json:"ean"`
	ConditionID      int     `json:"condition"`
	ListingPriceAmount float64 `json:"listing_price"`
	MinimumPriceAmount float64 `json:"minimum_price"`
	Note             string  `json:"note"`
	Amount           int     `json:"amount"`
	Status           string  `json:"status"`
	ShippingGroup    string  `json:"shipping_group"`
	Handling         int     `json:"handling_time_in_days"`
	WarehouseCode    string  `json:"warehouse_code"`
}

type CreateUnitRequest struct {
	EAN                string  `json:"ean"`
	ConditionID        int     `json:"condition"`
	ListingPriceAmount float64 `json:"listing_price"`
	MinimumPriceAmount float64 `json:"minimum_price,omitempty"`
	Note               string  `json:"note,omitempty"`
	Amount             int     `json:"amount"`
	ShippingGroup      string  `json:"shipping_group,omitempty"`
	Handling           int     `json:"handling_time_in_days,omitempty"`
}

type UpdateUnitRequest struct {
	ListingPriceAmount float64 `json:"listing_price,omitempty"`
	MinimumPriceAmount float64 `json:"minimum_price,omitempty"`
	Amount             int     `json:"amount,omitempty"`
	Note               string  `json:"note,omitempty"`
	ShippingGroup      string  `json:"shipping_group,omitempty"`
	Handling           int     `json:"handling_time_in_days,omitempty"`
}

// ── Order types ───────────────────────────────────────────────────────────────

type Order struct {
	OrderID          string        `json:"order_id"`
	Status           string        `json:"status"`
	CreatedAt        string        `json:"created_at"`
	UpdatedAt        string        `json:"updated_at"`
	BillingAddress   Address       `json:"billing_address"`
	ShippingAddress  Address       `json:"shipping_address"`
	OrderUnits       []OrderUnit   `json:"order_units"`
	TotalAmount      float64       `json:"total_amount"`
	Currency         string        `json:"currency"`
}

type Address struct {
	FirstName string `json:"firstname"`
	LastName  string `json:"lastname"`
	Company   string `json:"company"`
	Street    string `json:"street"`
	HouseNo   string `json:"house_no"`
	Postcode  string `json:"postcode"`
	City      string `json:"city"`
	Country   string `json:"country"`
	Phone     string `json:"phone"`
	Email     string `json:"email"`
}

type OrderUnit struct {
	OrderUnitID    string  `json:"order_unit_id"`
	ProductTitle   string  `json:"product_title"`
	EAN            string  `json:"ean"`
	UnitID         string  `json:"id_offer"`
	Quantity       int     `json:"count"`
	Price          float64 `json:"price"`
	Status         string  `json:"status"`
}

type FulfillRequest struct {
	TrackingNumber string `json:"tracking_number"`
	CarrierCode    string `json:"carrier_code"`
}

// ── Categories ────────────────────────────────────────────────────────────────

// GetCategories fetches the top-level Kaufland category tree.
func (c *Client) GetCategories() ([]Category, error) {
	data, _, err := c.doRequest("GET", "/v2/categories", nil, url.Values{"limit": {"100"}})
	if err != nil {
		return nil, err
	}
	var resp pagedResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse categories: %w", err)
	}
	var cats []Category
	if err := json.Unmarshal(resp.Data, &cats); err != nil {
		return nil, fmt.Errorf("decode categories: %w", err)
	}
	return cats, nil
}

// GetCategoryAttributes returns mandatory and optional attributes for a category.
func (c *Client) GetCategoryAttributes(categoryID int) ([]CategoryAttribute, error) {
	path := fmt.Sprintf("/v2/categories/%d/attributes", categoryID)
	data, _, err := c.doRequest("GET", path, nil, nil)
	if err != nil {
		return nil, err
	}
	var resp pagedResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse category attributes: %w", err)
	}
	var attrs []CategoryAttribute
	if err := json.Unmarshal(resp.Data, &attrs); err != nil {
		return nil, fmt.Errorf("decode category attributes: %w", err)
	}
	return attrs, nil
}

// ── Units (listings) ──────────────────────────────────────────────────────────

// GetUnits returns all seller units (listings) with optional pagination.
func (c *Client) GetUnits(offset, limit int) ([]Unit, int, error) {
	params := url.Values{
		"offset": {strconv.Itoa(offset)},
		"limit":  {strconv.Itoa(limit)},
	}
	data, _, err := c.doRequest("GET", "/v2/units", nil, params)
	if err != nil {
		return nil, 0, err
	}
	var resp pagedResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, 0, fmt.Errorf("parse units: %w", err)
	}
	var units []Unit
	if err := json.Unmarshal(resp.Data, &units); err != nil {
		return nil, 0, fmt.Errorf("decode units: %w", err)
	}
	total := 0
	if resp.Pagination != nil {
		total = resp.Pagination.Total
	}
	return units, total, nil
}

// GetAllUnits fetches all units by paginating through the API.
func (c *Client) GetAllUnits() ([]Unit, error) {
	var all []Unit
	offset := 0
	limit := 100
	for {
		units, total, err := c.GetUnits(offset, limit)
		if err != nil {
			return nil, err
		}
		all = append(all, units...)
		offset += limit
		if offset >= total || len(units) == 0 {
			break
		}
	}
	log.Printf("[Kaufland] Fetched %d units total", len(all))
	return all, nil
}

// GetUnit returns a single unit by its ID.
func (c *Client) GetUnit(unitID string) (*Unit, error) {
	path := fmt.Sprintf("/v2/units/%s", unitID)
	data, _, err := c.doRequest("GET", path, nil, nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Data Unit `json:"data"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse unit: %w", err)
	}
	return &resp.Data, nil
}

// CreateUnit creates a new listing unit on Kaufland.
func (c *Client) CreateUnit(req CreateUnitRequest) (*Unit, error) {
	data, _, err := c.doRequest("POST", "/v2/units", req, nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Data Unit `json:"data"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse create unit response: %w", err)
	}
	return &resp.Data, nil
}

// UpdateUnit updates a listing unit by ID.
func (c *Client) UpdateUnit(unitID string, req UpdateUnitRequest) error {
	path := fmt.Sprintf("/v2/units/%s", unitID)
	_, _, err := c.doRequest("PATCH", path, req, nil)
	return err
}

// DeleteUnit deletes (removes) a listing unit by ID.
func (c *Client) DeleteUnit(unitID string) error {
	path := fmt.Sprintf("/v2/units/%s", unitID)
	_, _, err := c.doRequest("DELETE", path, nil, nil)
	return err
}

// UpdateUnitStock updates only the stock amount for a unit.
func (c *Client) UpdateUnitStock(unitID string, amount int) error {
	return c.UpdateUnit(unitID, UpdateUnitRequest{Amount: amount})
}

// UpdateUnitPrice updates only the listing price for a unit.
func (c *Client) UpdateUnitPrice(unitID string, price float64) error {
	return c.UpdateUnit(unitID, UpdateUnitRequest{ListingPriceAmount: price})
}

// ── Orders ────────────────────────────────────────────────────────────────────

// GetOrders fetches open orders, optionally filtered by status and date.
func (c *Client) GetOrders(status string, offset, limit int) ([]Order, int, error) {
	params := url.Values{
		"offset": {strconv.Itoa(offset)},
		"limit":  {strconv.Itoa(limit)},
	}
	if status != "" {
		params.Set("status", status)
	}
	data, _, err := c.doRequest("GET", "/v2/orders", nil, params)
	if err != nil {
		return nil, 0, err
	}
	var resp pagedResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, 0, fmt.Errorf("parse orders: %w", err)
	}
	var orders []Order
	if err := json.Unmarshal(resp.Data, &orders); err != nil {
		return nil, 0, fmt.Errorf("decode orders: %w", err)
	}
	total := 0
	if resp.Pagination != nil {
		total = resp.Pagination.Total
	}
	return orders, total, nil
}

// GetNewOrders fetches all unshipped/open orders by paginating.
func (c *Client) GetNewOrders() ([]Order, error) {
	var all []Order
	offset := 0
	limit := 100
	for {
		orders, total, err := c.GetOrders("need_to_be_sent", offset, limit)
		if err != nil {
			return nil, err
		}
		all = append(all, orders...)
		offset += limit
		if offset >= total || len(orders) == 0 {
			break
		}
	}
	return all, nil
}

// GetOrder fetches a single order by ID.
func (c *Client) GetOrder(orderID string) (*Order, error) {
	path := fmt.Sprintf("/v2/orders/%s", orderID)
	data, _, err := c.doRequest("GET", path, nil, nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Data Order `json:"data"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse order: %w", err)
	}
	return &resp.Data, nil
}

// FulfillOrderUnit marks an order unit as shipped with a tracking number.
// Kaufland uses PATCH /v2/order-units/{order_unit_id}/fulfillments
func (c *Client) FulfillOrderUnit(orderUnitID, trackingNumber, carrierCode string) error {
	path := fmt.Sprintf("/v2/order-units/%s/fulfillments", orderUnitID)
	req := FulfillRequest{
		TrackingNumber: trackingNumber,
		CarrierCode:    carrierCode,
	}
	_, _, err := c.doRequest("PATCH", path, req, nil)
	return err
}

// ── Test connection ───────────────────────────────────────────────────────────

// TestConnection verifies that the credentials are valid by fetching categories.
func (c *Client) TestConnection() error {
	_, _, err := c.doRequest("GET", "/v2/categories", nil, url.Values{"limit": {"1"}})
	if err != nil {
		return fmt.Errorf("Kaufland connection test failed: %w", err)
	}
	return nil
}
