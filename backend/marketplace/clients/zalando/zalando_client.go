package zalando

// ============================================================================
// ZALANDO ZDIRECT PARTNER API CLIENT
// ============================================================================
// Auth: OAuth2 client_credentials flow.
// Base URL: https://api.merchants.zalando.com/v1  (production)
//           https://sandbox-api.merchants.zalando.com/v1  (sandbox)
// Docs: https://developers.merchants.zalando.com
// ============================================================================

import (
	"bytes"
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
	ProdBaseURL    = "https://api.merchants.zalando.com/v1"
	SandboxBaseURL = "https://sandbox-api.merchants.zalando.com/v1"
	TokenURL       = "https://auth.merchants.zalando.com/oauth2/token"
)

type Client struct {
	ClientID     string
	ClientSecret string
	BaseURL      string
	accessToken  string
	tokenExpiry  time.Time
	mu           sync.RWMutex
	HTTPClient   *http.Client
}

func NewClient(clientID, clientSecret string, production bool) *Client {
	base := ProdBaseURL
	if !production {
		base = SandboxBaseURL
	}
	return &Client{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		BaseURL:      base,
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
	data.Set("client_id", c.ClientID)
	data.Set("client_secret", c.ClientSecret)

	resp, err := c.HTTPClient.PostForm(TokenURL, data)
	if err != nil {
		return fmt.Errorf("zalando token request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var tok struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tok); err != nil || tok.AccessToken == "" {
		return fmt.Errorf("zalando token parse failed: %s", string(body))
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
	req, err := http.NewRequest(method, c.BaseURL+path, reader)
	if err != nil {
		return nil, 0, err
	}
	c.mu.RLock()
	req.Header.Set("Authorization", "Bearer "+c.accessToken)
	c.mu.RUnlock()
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, resp.StatusCode, fmt.Errorf("zalando API %d: %s", resp.StatusCode, string(respBody))
	}
	return respBody, resp.StatusCode, nil
}

// TestConnection verifies credentials by fetching the merchant profile.
func (c *Client) TestConnection() error {
	_, _, err := c.doRequest("GET", "/merchants/me", nil)
	if err != nil {
		return fmt.Errorf("zalando connection test: %w", err)
	}
	return nil
}

// ── ORDERS ────────────────────────────────────────────────────────────────────

type Order struct {
	OrderID      string      `json:"order_id"`
	OrderDate    string      `json:"order_date"` // RFC3339
	Status       string      `json:"status"`     // "PENDING","READY_FOR_FULFILLMENT","SHIPPED","CANCELLED"
	Currency     string      `json:"currency"`
	TotalAmount  float64     `json:"total_amount"`
	ShippingCost float64     `json:"shipping_cost"`
	BillingAddr  ZAddress    `json:"billing_address"`
	DeliveryAddr ZAddress    `json:"delivery_address"`
	LineItems    []ZLineItem `json:"line_items"`
}

type ZAddress struct {
	Name         string `json:"name"`
	AddressLine1 string `json:"address_line_1"`
	AddressLine2 string `json:"address_line_2"`
	City         string `json:"city"`
	PostalCode   string `json:"zip_code"`
	CountryCode  string `json:"country_code"`
}

type ZLineItem struct {
	LineItemID string  `json:"line_item_id"`
	EAN        string  `json:"ean"`
	ArticleID  string  `json:"article_id"`
	Name       string  `json:"name"`
	Quantity   int     `json:"quantity"`
	UnitPrice  float64 `json:"unit_price"`
}

// GetOrders fetches orders created after `since`.
func (c *Client) GetOrders(since time.Time) ([]Order, error) {
	path := fmt.Sprintf("/orders?created_after=%s&page_size=50", url.QueryEscape(since.Format(time.RFC3339)))
	body, _, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Orders []Order `json:"orders"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("zalando unmarshal orders: %w", err)
	}
	log.Printf("Zalando: fetched %d orders since %s", len(resp.Orders), since.Format(time.RFC3339))
	return resp.Orders, nil
}

// ShipOrder sends tracking information for a Zalando order.
func (c *Client) ShipOrder(orderID, trackingCode, carrier string, lineItemIDs []string) error {
	payload := map[string]interface{}{
		"tracking_number": trackingCode,
		"carrier":         carrier,
		"line_item_ids":   lineItemIDs,
	}
	_, _, err := c.doRequest("POST", "/orders/"+orderID+"/shipments", payload)
	return err
}

// ── ARTICLES (LISTINGS) ────────────────────────────────────────────────────────

type Article struct {
	ArticleID string  `json:"article_id"`
	EAN       string  `json:"ean"`
	Name      string  `json:"name"`
	Price     float64 `json:"price"`
	Currency  string  `json:"currency"`
	Stock     int     `json:"stock"`
	Active    bool    `json:"active"`
	ImageURL  string  `json:"image_url"`
}

// GetArticles fetches all merchant articles (listings), paginating automatically.
func (c *Client) GetArticles() ([]Article, error) {
	var all []Article
	page := 1
	for {
		path := fmt.Sprintf("/articles?page_size=100&page=%d", page)
		body, _, err := c.doRequest("GET", path, nil)
		if err != nil {
			return nil, err
		}
		var resp struct {
			Articles   []Article `json:"articles"`
			TotalCount int       `json:"total_count"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("zalando unmarshal articles: %w", err)
		}
		all = append(all, resp.Articles...)
		if len(all) >= resp.TotalCount || len(resp.Articles) == 0 {
			break
		}
		page++
	}
	return all, nil
}

// UpdateStock updates the stock quantity for an article.
func (c *Client) UpdateStock(articleID string, quantity int) error {
	_, _, err := c.doRequest("PATCH", "/articles/"+articleID, map[string]interface{}{"stock": quantity})
	return err
}

// UpdatePrice updates the price for an article.
func (c *Client) UpdatePrice(articleID string, price float64) error {
	_, _, err := c.doRequest("PATCH", "/articles/"+articleID, map[string]interface{}{"price": price})
	return err
}

// DeactivateArticle sets an article to inactive.
func (c *Client) DeactivateArticle(articleID string) error {
	_, _, err := c.doRequest("PATCH", "/articles/"+articleID, map[string]interface{}{"active": false})
	return err
}
