package bol

// ============================================================================
// BOL.COM RETAILER API v10 CLIENT
// ============================================================================
// Auth: OAuth2 client_credentials flow using Basic auth on token endpoint.
// Base URL: https://api.bol.com/retailer  (production)
//           https://api.bol.com/retailer  (same, use Accept: application/vnd.retailer.v10+json)
// Token URL: https://login.bol.com/token
// Docs: https://api.bol.com/retailer/public/redoc/v10
// ============================================================================

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const (
	BaseURL       = "https://api.bol.com/retailer"
	TokenEndpoint = "https://login.bol.com/token"
	APIVersion    = "application/vnd.retailer.v10+json"
)

type Client struct {
	ClientID     string
	ClientSecret string
	accessToken  string
	tokenExpiry  time.Time
	mu           sync.RWMutex
	HTTPClient   *http.Client
}

func NewClient(clientID, clientSecret string) *Client {
	return &Client{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		HTTPClient:   &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) ensureToken() error {
	c.mu.RLock()
	valid := c.accessToken != "" && time.Now().Before(c.tokenExpiry)
	c.mu.RUnlock()
	if valid {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	data := url.Values{}
	data.Set("grant_type", "client_credentials")

	req, err := http.NewRequest("POST", TokenEndpoint, bytes.NewBufferString(data.Encode()))
	if err != nil {
		return fmt.Errorf("bol token request: %w", err)
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(c.ClientID + ":" + c.ClientSecret))
	req.Header.Set("Authorization", "Basic "+encoded)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("bol token fetch: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var tok struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tok); err != nil || tok.AccessToken == "" {
		return fmt.Errorf("bol token parse failed: %s", string(body))
	}
	c.accessToken = tok.AccessToken
	c.tokenExpiry = time.Now().Add(time.Duration(tok.ExpiresIn-60) * time.Second)
	return nil
}

func (c *Client) doRequest(method, path string, body interface{}) ([]byte, int, error) {
	if err := c.ensureToken(); err != nil {
		return nil, 0, err
	}
	var reader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, BaseURL+path, reader)
	if err != nil {
		return nil, 0, err
	}
	c.mu.RLock()
	req.Header.Set("Authorization", "Bearer "+c.accessToken)
	c.mu.RUnlock()
	req.Header.Set("Accept", APIVersion)
	if body != nil {
		req.Header.Set("Content-Type", APIVersion)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, resp.StatusCode, fmt.Errorf("bol.com API %d: %s", resp.StatusCode, string(respBody))
	}
	return respBody, resp.StatusCode, nil
}

// TestConnection verifies credentials by fetching the retailer account info.
func (c *Client) TestConnection() error {
	_, _, err := c.doRequest("GET", "/retailer/account", nil)
	if err != nil {
		return fmt.Errorf("bol.com connection test: %w", err)
	}
	return nil
}

// ── ORDERS ────────────────────────────────────────────────────────────────────

type OrdersResponse struct {
	Orders []OrderSummary `json:"orders"`
}

type OrderSummary struct {
	OrderID        string `json:"orderId"`
	DateTimePlaced string `json:"dateTimePlaced"` // ISO 8601
	Status         string `json:"status"`         // "OPEN","SHIPPED","CANCELLED"
}

type Order struct {
	OrderID        string      `json:"orderId"`
	DateTimeOrdered string     `json:"dateTimeOrdered"`
	Status         string      `json:"status"`
	BillingDetails BolAddress  `json:"billingDetails"`
	ShipmentDetails BolAddress `json:"shipmentDetails"`
	OrderItems     []OrderItem `json:"orderItems"`
}

type BolAddress struct {
	FirstName   string `json:"firstName"`
	Surname     string `json:"surname"`
	SurnamePre  string `json:"surnamePre"`
	StreetName  string `json:"streetName"`
	HouseNumber string `json:"houseNumber"`
	HouseNumberExtension string `json:"houseNumberExtension"`
	ZipCode     string `json:"zipCode"`
	City        string `json:"city"`
	CountryCode string `json:"countryCode"` // ISO 3166-1 alpha-2
	Email       string `json:"email"`
}

type OrderItem struct {
	OrderItemID string  `json:"orderItemId"`
	EAN         string  `json:"ean"`
	Title       string  `json:"title"`
	Quantity    int     `json:"quantity"`
	UnitPrice   float64 `json:"unitPrice"`
	LatestDeliveryDate string `json:"latestDeliveryDate"`
}

// GetOrders fetches open orders from bol.com.
func (c *Client) GetOrders(page int) ([]OrderSummary, error) {
	path := fmt.Sprintf("/retailer/orders?page=%d&fulfilment-method=FBR", page)
	body, _, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	var resp OrdersResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("bol.com unmarshal orders: %w", err)
	}
	return resp.Orders, nil
}

// GetOrder fetches a single order's full details.
func (c *Client) GetOrder(orderID string) (*Order, error) {
	body, _, err := c.doRequest("GET", "/retailer/orders/"+orderID, nil)
	if err != nil {
		return nil, err
	}
	var o Order
	if err := json.Unmarshal(body, &o); err != nil {
		return nil, fmt.Errorf("bol.com unmarshal order: %w", err)
	}
	return &o, nil
}

// GetAllOpenOrders fetches all open FBR orders across pages.
func (c *Client) GetAllOpenOrders() ([]OrderSummary, error) {
	var all []OrderSummary
	page := 1
	for {
		orders, err := c.GetOrders(page)
		if err != nil {
			return nil, err
		}
		if len(orders) == 0 {
			break
		}
		all = append(all, orders...)
		if len(orders) < 50 { // bol.com returns max 50 per page
			break
		}
		page++
	}
	log.Printf("Bol.com: fetched %d open orders", len(all))
	return all, nil
}

// ── TRACKING / SHIPMENT ───────────────────────────────────────────────────────

type ShipmentRequest struct {
	OrderItems    []ShipmentItem `json:"orderItems"`
	ShippingLabel *ShippingLabel `json:"shippingLabel,omitempty"`
	Transport     Transport      `json:"transport"`
}

type ShipmentItem struct {
	OrderItemID string `json:"orderItemId"`
	Quantity    int    `json:"quantity"`
}

type ShippingLabel struct {
	LabelID int `json:"labelId"`
}

type Transport struct {
	TrackAndTrace string `json:"trackAndTrace"`
	TransporterCode string `json:"transporterCode"` // e.g. "TNT","DHL","DPD"
}

// CreateShipment sends tracking/shipment confirmation to bol.com.
func (c *Client) CreateShipment(req ShipmentRequest) error {
	_, _, err := c.doRequest("POST", "/retailer/shipments", req)
	return err
}

// ── OFFERS (LISTINGS) ─────────────────────────────────────────────────────────

type Offer struct {
	OfferID           string  `json:"offerId"`
	EAN               string  `json:"ean"`
	Reference         string  `json:"reference"` // Retailer's own SKU/reference
	OnHoldByRetailer  bool    `json:"onHoldByRetailer"`
	Stock             int     `json:"stock"`
	Price             float64 `json:"price"`
	Title             string  `json:"title"`
	Fulfilment        string  `json:"fulfilment"` // "FBR" or "FBB"
}

type OffersResponse struct {
	Offers []Offer `json:"offers"`
}

// GetOffers fetches all retailer offers (listings).
func (c *Client) GetOffers() ([]Offer, error) {
	// bol.com uses a report export flow for bulk offers; simplified direct fetch here
	body, _, err := c.doRequest("GET", "/retailer/offers", nil)
	if err != nil {
		return nil, err
	}
	var resp OffersResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("bol.com unmarshal offers: %w", err)
	}
	return resp.Offers, nil
}

// UpdateStock updates stock for an offer.
func (c *Client) UpdateStock(offerID string, quantity int) error {
	payload := map[string]interface{}{
		"amount": quantity,
		"managedByRetailer": true,
	}
	_, _, err := c.doRequest("PUT", "/retailer/offers/"+offerID+"/stock", payload)
	return err
}

// UpdatePrice updates price for an offer.
func (c *Client) UpdatePrice(offerID string, price float64) error {
	payload := map[string]interface{}{
		"bundlePrices": []map[string]interface{}{
			{"quantity": 1, "unitPrice": price},
		},
	}
	_, _, err := c.doRequest("PUT", "/retailer/offers/"+offerID+"/price", payload)
	return err
}

// DeactivateOffer puts an offer on hold.
func (c *Client) DeactivateOffer(offerID string) error {
	payload := map[string]interface{}{
		"onHoldByRetailer": true,
	}
	_, _, err := c.doRequest("PATCH", "/retailer/offers/"+offerID, payload)
	return err
}

// DoRequestPublic exposes doRequest for callers outside this package that need
// to call endpoints not yet wrapped (e.g. the reconcile handler's offer push).
func (c *Client) DoRequestPublic(method, path string, body interface{}) ([]byte, int, error) {
	return c.doRequest(method, path, body)
}
