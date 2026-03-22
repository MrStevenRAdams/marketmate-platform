package onbuy

// ============================================================================
// ONBUY API CLIENT
// ============================================================================
// Base URL:  https://api.onbuy.com/v4
// Auth:      Two-step — POST {consumer_key, consumer_secret} to /auth/request-token
//            → short-lived access_token (Bearer). Cached in struct with mutex.
// Docs:      https://docs.onbuy.com
// site_id:   2000 = OnBuy UK (default)
// ============================================================================

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

const baseURL = "https://api.onbuy.com/v4"

// ── Client ────────────────────────────────────────────────────────────────────

type Client struct {
	ConsumerKey    string
	ConsumerSecret string
	SiteID         int

	mu          sync.Mutex
	accessToken string
	tokenExpiry time.Time

	HTTPClient *http.Client
}

func NewClient(consumerKey, consumerSecret string, siteID int) *Client {
	if siteID == 0 {
		siteID = 2000 // OnBuy UK
	}
	return &Client{
		ConsumerKey:    consumerKey,
		ConsumerSecret: consumerSecret,
		SiteID:         siteID,
		HTTPClient:     &http.Client{Timeout: 30 * time.Second},
	}
}

// ── Token Management ──────────────────────────────────────────────────────────

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

// GetToken fetches a new access token from OnBuy.
func (c *Client) GetToken() (string, error) {
	payload := map[string]string{
		"consumer_key":    c.ConsumerKey,
		"consumer_secret": c.ConsumerSecret,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal token request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, baseURL+"/auth/request-token", bytes.NewReader(b))
	if err != nil {
		return "", fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("execute token request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read token response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("OnBuy auth error [HTTP %d]: %s", resp.StatusCode, string(body))
	}

	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", fmt.Errorf("parse token response: %w", err)
	}
	if tr.AccessToken == "" {
		return "", fmt.Errorf("OnBuy returned empty access_token")
	}

	return tr.AccessToken, nil
}

// RefreshTokenIfNeeded checks whether the cached token is still valid and
// fetches a new one if it has expired (or will expire within 30s).
func (c *Client) RefreshTokenIfNeeded() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.accessToken != "" && time.Now().Add(30*time.Second).Before(c.tokenExpiry) {
		return nil // token still valid
	}

	log.Printf("[OnBuy] Refreshing access token")
	token, err := c.GetToken()
	if err != nil {
		return fmt.Errorf("refresh token: %w", err)
	}

	c.accessToken = token
	// OnBuy tokens are typically valid for 1 hour; default to 55 min if ExpiresIn not cached
	c.tokenExpiry = time.Now().Add(55 * time.Minute)
	return nil
}

// getAccessToken returns the cached token, refreshing if necessary.
func (c *Client) getAccessToken() (string, error) {
	if err := c.RefreshTokenIfNeeded(); err != nil {
		return "", err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.accessToken, nil
}

// ── HTTP helpers ──────────────────────────────────────────────────────────────

func (c *Client) doRequest(method, path string, body interface{}) ([]byte, int, error) {
	token, err := c.getAccessToken()
	if err != nil {
		return nil, 0, fmt.Errorf("get access token: %w", err)
	}

	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, baseURL+path, reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("execute request to %s: %w", path, err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		var apiErr struct {
			Message string `json:"message"`
			Error   string `json:"error"`
		}
		if jsonErr := json.Unmarshal(respBytes, &apiErr); jsonErr == nil {
			msg := apiErr.Message
			if msg == "" {
				msg = apiErr.Error
			}
			if msg != "" {
				return nil, resp.StatusCode, fmt.Errorf("OnBuy API error [HTTP %d]: %s", resp.StatusCode, msg)
			}
		}
		return nil, resp.StatusCode, fmt.Errorf("OnBuy API HTTP %d: %s", resp.StatusCode, string(respBytes))
	}

	return respBytes, resp.StatusCode, nil
}

func (c *Client) getJSON(path string, out interface{}) error {
	b, _, err := c.doRequest(http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func (c *Client) postJSON(path string, payload interface{}, out interface{}) error {
	b, _, err := c.doRequest(http.MethodPost, path, payload)
	if err != nil {
		return err
	}
	if out != nil {
		return json.Unmarshal(b, out)
	}
	return nil
}

func (c *Client) putJSON(path string, payload interface{}, out interface{}) error {
	b, _, err := c.doRequest(http.MethodPut, path, payload)
	if err != nil {
		return err
	}
	if out != nil {
		return json.Unmarshal(b, out)
	}
	return nil
}

func (c *Client) deleteReq(path string) error {
	_, _, err := c.doRequest(http.MethodDelete, path, nil)
	return err
}

// ── Types ─────────────────────────────────────────────────────────────────────

type Category struct {
	CategoryID   int    `json:"category_id"`
	Name         string `json:"name"`
	ParentID     int    `json:"parent_id"`
	HasChildren  bool   `json:"has_children"`
	SiteID       int    `json:"site_id"`
}

type CategoryListResponse struct {
	Success bool       `json:"success"`
	Data    []Category `json:"data"`
}

type CategoryFeature struct {
	FeatureID   int    `json:"feature_id"`
	Name        string `json:"name"`
	Required    bool   `json:"required"`
	Type        string `json:"type"` // "text", "list", etc.
	Values      []string `json:"values,omitempty"`
}

type CategoryFeaturesResponse struct {
	Success bool              `json:"success"`
	Data    []CategoryFeature `json:"data"`
}

type Condition struct {
	ConditionID string `json:"condition_id"`
	Name        string `json:"name"`
}

type ConditionsResponse struct {
	Success bool        `json:"success"`
	Data    []Condition `json:"data"`
}

// Listing represents an OnBuy listing (product mapped to an OPC).
type Listing struct {
	ListingID          string  `json:"listing_id,omitempty"`
	OPC                string  `json:"opc"`
	SiteID             int     `json:"site_id"`
	ConditionID        string  `json:"condition_id"`
	Price              float64 `json:"price"`
	Stock              int     `json:"stock"`
	DeliveryTemplateID int     `json:"delivery_template_id,omitempty"`
	SKU                string  `json:"sku,omitempty"`
	Description        string  `json:"description,omitempty"`
	FeaturedPrice      float64 `json:"featured_price,omitempty"`
	GroupID            string  `json:"group_id,omitempty"`
}

type ListingResponse struct {
	Success bool      `json:"success"`
	Data    []Listing `json:"data"`
}

type ListingResult struct {
	ListingID string `json:"listing_id"`
	Success   bool   `json:"success"`
	Message   string `json:"message,omitempty"`
}

type CreateListingResponse struct {
	Success bool          `json:"success"`
	Data    ListingResult `json:"data"`
}

type OrderAddress struct {
	Name       string `json:"name"`
	Line1      string `json:"line_1"`
	Line2      string `json:"line_2"`
	Town       string `json:"town"`
	County     string `json:"county"`
	Postcode   string `json:"postcode"`
	CountryCode string `json:"country_code"`
}

type OrderLine struct {
	OrderLineID    string  `json:"order_line_id"`
	OPC            string  `json:"opc"`
	SKU            string  `json:"sku"`
	ProductName    string  `json:"product_name"`
	Quantity       int     `json:"quantity"`
	UnitPrice      float64 `json:"unit_price"`
	LineTotal      float64 `json:"line_total"`
	ConditionID    string  `json:"condition_id"`
}

type Order struct {
	OrderID         string       `json:"order_id"`
	Status          string       `json:"status"` // "awaiting_dispatch", "dispatched", etc.
	DateCreated     string       `json:"date"`
	BuyerName       string       `json:"buyer_name"`
	BuyerEmail      string       `json:"buyer_email"`
	DeliveryAddress OrderAddress `json:"delivery_address"`
	Lines           []OrderLine  `json:"lines"`
	OrderTotal      float64      `json:"order_total"`
	DeliveryCost    float64      `json:"delivery_cost"`
	CurrencyCode    string       `json:"currency_code"`
}

type OrderListResponse struct {
	Success bool    `json:"success"`
	Data    []Order `json:"data"`
	Total   int     `json:"total"`
}

type DispatchPayload struct {
	TrackingNumber string `json:"tracking_number"`
	Carrier        string `json:"carrier"`
}

// ── Categories ────────────────────────────────────────────────────────────────

func (c *Client) GetCategories(siteID int) ([]Category, error) {
	if siteID == 0 {
		siteID = c.SiteID
	}
	var resp CategoryListResponse
	path := fmt.Sprintf("/categories?site_id=%d&limit=500", siteID)
	if err := c.getJSON(path, &resp); err != nil {
		return nil, fmt.Errorf("GetCategories: %w", err)
	}
	return resp.Data, nil
}

func (c *Client) GetCategoryFeatures(categoryID int) ([]CategoryFeature, error) {
	var resp CategoryFeaturesResponse
	path := fmt.Sprintf("/categories/%d/features?site_id=%d", categoryID, c.SiteID)
	if err := c.getJSON(path, &resp); err != nil {
		return nil, fmt.Errorf("GetCategoryFeatures: %w", err)
	}
	return resp.Data, nil
}

func (c *Client) GetConditions() ([]Condition, error) {
	var resp ConditionsResponse
	if err := c.getJSON("/conditions", &resp); err != nil {
		return nil, fmt.Errorf("GetConditions: %w", err)
	}
	return resp.Data, nil
}

// ── Listings ──────────────────────────────────────────────────────────────────

func (c *Client) GetProducts(page int) ([]Listing, error) {
	if page < 1 {
		page = 1
	}
	var resp ListingResponse
	path := fmt.Sprintf("/listings?site_id=%d&limit=100&offset=%d", c.SiteID, (page-1)*100)
	if err := c.getJSON(path, &resp); err != nil {
		return nil, fmt.Errorf("GetProducts: %w", err)
	}
	return resp.Data, nil
}

func (c *Client) CreateListing(listing *Listing) (*ListingResult, error) {
	if listing.SiteID == 0 {
		listing.SiteID = c.SiteID
	}
	var resp CreateListingResponse
	if err := c.postJSON("/listings", listing, &resp); err != nil {
		return nil, fmt.Errorf("CreateListing: %w", err)
	}
	if !resp.Success {
		return nil, fmt.Errorf("OnBuy CreateListing: %s", resp.Data.Message)
	}
	return &resp.Data, nil
}

func (c *Client) UpdateListing(listingID string, update map[string]interface{}) error {
	path := fmt.Sprintf("/listings/%s", listingID)
	return c.putJSON(path, update, nil)
}

func (c *Client) DeleteListing(listingID string) error {
	path := fmt.Sprintf("/listings/%s", listingID)
	return c.deleteReq(path)
}

// ── Orders ────────────────────────────────────────────────────────────────────

func (c *Client) GetOrders(status string, page int) ([]Order, int, error) {
	if page < 1 {
		page = 1
	}
	if status == "" {
		status = "awaiting_dispatch"
	}
	var resp OrderListResponse
	path := fmt.Sprintf("/orders?site_id=%d&status=%s&limit=100&offset=%d",
		c.SiteID, status, (page-1)*100)
	if err := c.getJSON(path, &resp); err != nil {
		return nil, 0, fmt.Errorf("GetOrders: %w", err)
	}
	return resp.Data, resp.Total, nil
}

// FetchNewOrders retrieves awaiting_dispatch orders created after the given time.
func (c *Client) FetchNewOrders(after time.Time) ([]Order, error) {
	allOrders, _, err := c.GetOrders("awaiting_dispatch", 1)
	if err != nil {
		return nil, err
	}

	var filtered []Order
	for _, o := range allOrders {
		if o.DateCreated == "" {
			filtered = append(filtered, o)
			continue
		}
		// Try common OnBuy date formats
		for _, layout := range []string{"2006-01-02T15:04:05Z", "2006-01-02 15:04:05", "2006-01-02"} {
			t, err := time.Parse(layout, o.DateCreated)
			if err == nil {
				if t.After(after) {
					filtered = append(filtered, o)
				}
				break
			}
		}
	}
	return filtered, nil
}

func (c *Client) AcknowledgeOrders(orderIDs []string) error {
	payload := map[string]interface{}{
		"site_id":   c.SiteID,
		"order_ids": orderIDs,
	}
	return c.postJSON("/orders/acknowledge", payload, nil)
}

func (c *Client) DispatchOrder(orderID string, tracking DispatchPayload) error {
	path := fmt.Sprintf("/orders/%s/dispatch", orderID)
	payload := map[string]interface{}{
		"site_id":         c.SiteID,
		"tracking_number": tracking.TrackingNumber,
		"carrier":         tracking.Carrier,
	}
	return c.postJSON(path, payload, nil)
}

// ── Connection Test ───────────────────────────────────────────────────────────

// TestConnection verifies that credentials are valid by fetching a token.
func (c *Client) TestConnection() error {
	// Force a fresh token fetch (bypass cache)
	token, err := c.GetToken()
	if err != nil {
		return fmt.Errorf("OnBuy connection failed: %w", err)
	}
	if token == "" {
		return fmt.Errorf("OnBuy connection failed: empty token returned")
	}

	// Cache it
	c.mu.Lock()
	c.accessToken = token
	c.tokenExpiry = time.Now().Add(55 * time.Minute)
	c.mu.Unlock()

	log.Printf("[OnBuy] TestConnection successful")
	return nil
}
