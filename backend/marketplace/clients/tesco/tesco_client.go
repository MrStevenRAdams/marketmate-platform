package tesco

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ============================================================================
// TESCO MARKETPLACE SELLER API CLIENT
// ============================================================================
// Auth: OAuth 2.0 Client Credentials grant
// Base URL: https://api.tesco.com/marketplace/
// ============================================================================

const BaseURL = "https://api.tesco.com/marketplace/"
const tokenURL = "https://api.tesco.com/identity/4.0/authorization/token"

// Client is the Tesco Marketplace API client
type Client struct {
	ClientID     string
	ClientSecret string
	SellerID     string
	AccessToken  string
	TokenExpiry  time.Time
	HTTPClient   *http.Client
}

// NewClient creates a new Tesco API client
func NewClient(clientID, clientSecret, sellerID string) *Client {
	return &Client{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		SellerID:     sellerID,
		HTTPClient:   &http.Client{Timeout: 30 * time.Second},
	}
}

// tokenResponse is the OAuth token response
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	TokenType   string `json:"token_type"`
}

// GetAccessToken fetches or refreshes the OAuth access token
func (c *Client) GetAccessToken() (string, error) {
	if c.AccessToken != "" && time.Now().Before(c.TokenExpiry) {
		return c.AccessToken, nil
	}

	body := fmt.Sprintf("grant_type=client_credentials&client_id=%s&client_secret=%s",
		c.ClientID, c.ClientSecret)

	req, err := http.NewRequest("POST", tokenURL, bytes.NewBufferString(body))
	if err != nil {
		return "", fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("token request failed %d: %s", resp.StatusCode, string(b))
	}

	var tok tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return "", fmt.Errorf("decode token: %w", err)
	}

	c.AccessToken = tok.AccessToken
	c.TokenExpiry = time.Now().Add(time.Duration(tok.ExpiresIn-60) * time.Second)
	return c.AccessToken, nil
}

// do performs an authenticated API request
func (c *Client) do(method, path string, body interface{}) (map[string]interface{}, error) {
	token, err := c.GetAccessToken()
	if err != nil {
		return nil, err
	}

	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, BaseURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.SellerID != "" {
		req.Header.Set("X-Seller-ID", c.SellerID)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("tesco api error %d: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]interface{}
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &result); err != nil {
			return nil, fmt.Errorf("decode response: %w", err)
		}
	}
	return result, nil
}

// GetCategories returns the Tesco product category tree
func (c *Client) GetCategories() (map[string]interface{}, error) {
	return c.do("GET", "v1/categories", nil)
}

// GetProductTemplate returns required attributes for a category
func (c *Client) GetProductTemplate(categoryID string) (map[string]interface{}, error) {
	return c.do("GET", fmt.Sprintf("v1/categories/%s/template", categoryID), nil)
}

// SubmitProduct creates a new product listing on Tesco
func (c *Client) SubmitProduct(product map[string]interface{}) (map[string]interface{}, error) {
	return c.do("POST", "v1/products", product)
}

// UpdateProduct updates an existing product listing
func (c *Client) UpdateProduct(productID string, updates map[string]interface{}) (map[string]interface{}, error) {
	return c.do("PUT", fmt.Sprintf("v1/products/%s", productID), updates)
}

// DeleteProduct removes a product listing
func (c *Client) DeleteProduct(productID string) (map[string]interface{}, error) {
	return c.do("DELETE", fmt.Sprintf("v1/products/%s", productID), nil)
}

// GetOrders fetches orders in a date range
func (c *Client) GetOrders(from, to time.Time, page int) (map[string]interface{}, error) {
	path := fmt.Sprintf("v1/orders?from=%s&to=%s&page=%d&pageSize=100",
		from.Format(time.RFC3339), to.Format(time.RFC3339), page)
	return c.do("GET", path, nil)
}

// AcknowledgeOrder acknowledges receipt of an order
func (c *Client) AcknowledgeOrder(orderID string) (map[string]interface{}, error) {
	return c.do("POST", fmt.Sprintf("v1/orders/%s/acknowledge", orderID), nil)
}

// UpdateShipment pushes tracking information for an order
func (c *Client) UpdateShipment(orderID string, tracking map[string]interface{}) (map[string]interface{}, error) {
	return c.do("POST", fmt.Sprintf("v1/orders/%s/shipment", orderID), tracking)
}
