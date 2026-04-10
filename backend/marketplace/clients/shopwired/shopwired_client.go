package shopwired

// ============================================================================
// SHOPWIRED REST API v1 CLIENT
// ============================================================================
// Base URL:  https://api.ecommerceapi.uk/v1
// Auth:      HTTP Basic — API Key as username, API Secret as password
// Docs:      https://shopwired.readme.io
// Rate limit: standard REST — no documented hard limit, be polite
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
	"strconv"
	"time"
)

const BaseURL = "https://api.ecommerceapi.uk/v1"

// ── Client ────────────────────────────────────────────────────────────────────

type Client struct {
	APIKey     string
	APISecret  string
	BaseURL    string
	HTTPClient *http.Client
}

func NewClient(apiKey, apiSecret string) *Client {
	return &Client{
		APIKey:     apiKey,
		APISecret:  apiSecret,
		BaseURL:    BaseURL,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// ── Auth ──────────────────────────────────────────────────────────────────────

func (c *Client) authHeader() string {
	raw := c.APIKey + ":" + c.APISecret
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(raw))
}

// ── HTTP helpers ──────────────────────────────────────────────────────────────

func (c *Client) doRequest(method, path string, body interface{}, queryParams url.Values) ([]byte, int, error) {
	endpoint := c.BaseURL + path
	if len(queryParams) > 0 {
		endpoint += "?" + queryParams.Encode()
	}

	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, endpoint, reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", c.authHeader())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		log.Printf("[ShopWired] %s %s → %d: %s", method, path, resp.StatusCode, string(data))
		return data, resp.StatusCode, fmt.Errorf("shopwired API error %d: %s", resp.StatusCode, string(data))
	}

	return data, resp.StatusCode, nil
}

func (c *Client) get(path string, params url.Values) ([]byte, error) {
	b, _, err := c.doRequest("GET", path, nil, params)
	return b, err
}

func (c *Client) post(path string, body interface{}) ([]byte, error) {
	b, _, err := c.doRequest("POST", path, body, nil)
	return b, err
}

func (c *Client) put(path string, body interface{}) ([]byte, error) {
	b, _, err := c.doRequest("PUT", path, body, nil)
	return b, err
}

func (c *Client) del(path string) error {
	_, _, err := c.doRequest("DELETE", path, nil, nil)
	return err
}

// ============================================================================
// TYPES
// ============================================================================

type Category struct {
	ID              int    `json:"id"`
	Title           string `json:"title"`
	Description     string `json:"description,omitempty"`
	Slug            string `json:"slug,omitempty"`
	URL             string `json:"url,omitempty"`
	Active          bool   `json:"active"`
	MetaTitle       string `json:"metaTitle,omitempty"`
	MetaDescription string `json:"metaDescription,omitempty"`
}

type Brand struct {
	ID              int    `json:"id"`
	Title           string `json:"title"`
	Description     string `json:"description,omitempty"`
	URL             string `json:"url,omitempty"`
	Active          bool   `json:"active"`
	MetaTitle       string `json:"metaTitle,omitempty"`
	MetaDescription string `json:"metaDescription,omitempty"`
}

type ProductImage struct {
	ID  int    `json:"id,omitempty"`
	URL string `json:"url"`
}

type Product struct {
	ID              int            `json:"id,omitempty"`
	Title           string         `json:"title"`
	Description     string         `json:"description,omitempty"`
	Description2    string         `json:"description2,omitempty"`
	Active          bool           `json:"active"`
	Price           float64        `json:"price"`
	SalePrice       float64        `json:"salePrice,omitempty"`
	ComparePrice    float64        `json:"comparePrice,omitempty"`
	SKU             string         `json:"sku,omitempty"`
	GTIN            string         `json:"gtin,omitempty"`
	MPN             string         `json:"mpn,omitempty"`
	Stock           int            `json:"stock,omitempty"`
	Weight          float64        `json:"weight,omitempty"`
	DeliveryPrice   float64        `json:"deliveryPrice,omitempty"`
	URL             string         `json:"url,omitempty"`
	MetaTitle       string         `json:"metaTitle,omitempty"`
	MetaDescription string         `json:"metaDescription,omitempty"`
	SearchKeywords  string         `json:"searchKeywords,omitempty"`
	New             bool           `json:"new,omitempty"`
	VatExclusive    bool           `json:"vatExclusive,omitempty"`
	Images          []ProductImage `json:"images,omitempty"`
	CategoryIDs     []int          `json:"categoryIds,omitempty"`
	BrandID         int            `json:"brandId,omitempty"`
}

type ProductOption struct {
	ID    int    `json:"id,omitempty"`
	Title string `json:"title"`
}

type ProductOptionValue struct {
	ID    int    `json:"id,omitempty"`
	Title string `json:"title"`
}

type ProductVariation struct {
	ID       int            `json:"id,omitempty"`
	SKU      string         `json:"sku,omitempty"`
	Price    float64        `json:"price,omitempty"`
	Stock    int            `json:"stock,omitempty"`
	Weight   float64        `json:"weight,omitempty"`
	GTIN     string         `json:"gtin,omitempty"`
	Active   bool           `json:"active"`
	Images   []ProductImage `json:"images,omitempty"`
}

type Order struct {
	ID             int         `json:"id"`
	Status         string      `json:"status"`
	CustomerName   string      `json:"customerName,omitempty"`
	Email          string      `json:"email,omitempty"`
	Total          float64     `json:"total"`
	Currency       string      `json:"currency,omitempty"`
	CreatedAt      string      `json:"createdAt,omitempty"`
	UpdatedAt      string      `json:"updatedAt,omitempty"`
	TrackingNumber string      `json:"trackingNumber,omitempty"`
	ShippingName   string      `json:"shippingName,omitempty"`
	Items          []OrderItem `json:"items,omitempty"`
	ShippingAddr   interface{} `json:"shippingAddress,omitempty"`
}

type OrderItem struct {
	ProductID int     `json:"productId"`
	Title     string  `json:"title"`
	SKU       string  `json:"sku,omitempty"`
	Quantity  int     `json:"quantity"`
	Price     float64 `json:"price"`
}

type StockUpdate struct {
	SKU      string `json:"sku"`
	Quantity int    `json:"quantity"`
}

type Webhook struct {
	ID     int    `json:"id,omitempty"`
	Topic  string `json:"topic"`
	URL    string `json:"url"`
	Active bool   `json:"active"`
}

type OrderStatusUpdate struct {
	Status         string `json:"status"`
	TrackingNumber string `json:"trackingNumber,omitempty"`
	ShippingCarrier string `json:"shippingCarrier,omitempty"`
}

// ============================================================================
// CONNECTION TEST
// ============================================================================

func (c *Client) TestConnection() error {
	params := url.Values{"count": {"1"}}
	_, err := c.get("/products", params)
	if err != nil {
		return fmt.Errorf("ShopWired connection test failed: %w", err)
	}
	return nil
}

// ============================================================================
// CATEGORIES
// ============================================================================

func (c *Client) ListCategories(offset, count int) ([]Category, error) {
	params := url.Values{
		"count":  {strconv.Itoa(count)},
		"offset": {strconv.Itoa(offset)},
	}
	data, err := c.get("/categories", params)
	if err != nil {
		return nil, err
	}
	var cats []Category
	if err := json.Unmarshal(data, &cats); err != nil {
		return nil, fmt.Errorf("unmarshal categories: %w", err)
	}
	return cats, nil
}

func (c *Client) GetAllCategories() ([]Category, error) {
	var all []Category
	offset := 0
	const pageSize = 50
	for {
		page, err := c.ListCategories(offset, pageSize)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		if len(page) < pageSize {
			break
		}
		offset += pageSize
	}
	return all, nil
}

func (c *Client) CreateCategory(title, slug string, parentIDs []int) (*Category, error) {
	body := map[string]interface{}{
		"title":   title,
		"active":  true,
	}
	if slug != "" {
		body["slug"] = slug
	}
	if len(parentIDs) > 0 {
		body["parents"] = parentIDs
	}
	data, err := c.post("/categories", body)
	if err != nil {
		return nil, err
	}
	var cat Category
	if err := json.Unmarshal(data, &cat); err != nil {
		return nil, fmt.Errorf("unmarshal created category: %w", err)
	}
	return &cat, nil
}

func (c *Client) GetOrCreateCategory(title string) (*Category, error) {
	cats, err := c.GetAllCategories()
	if err != nil {
		return nil, err
	}
	for i, cat := range cats {
		if cat.Title == title {
			return &cats[i], nil
		}
	}
	return c.CreateCategory(title, "", nil)
}

// ============================================================================
// BRANDS
// ============================================================================

func (c *Client) ListBrands(offset, count int) ([]Brand, error) {
	params := url.Values{
		"count":  {strconv.Itoa(count)},
		"offset": {strconv.Itoa(offset)},
	}
	data, err := c.get("/brands", params)
	if err != nil {
		return nil, err
	}
	var brands []Brand
	if err := json.Unmarshal(data, &brands); err != nil {
		return nil, fmt.Errorf("unmarshal brands: %w", err)
	}
	return brands, nil
}

func (c *Client) GetAllBrands() ([]Brand, error) {
	var all []Brand
	offset := 0
	const pageSize = 50
	for {
		page, err := c.ListBrands(offset, pageSize)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		if len(page) < pageSize {
			break
		}
		offset += pageSize
	}
	return all, nil
}

func (c *Client) CreateBrand(title string) (*Brand, error) {
	body := map[string]interface{}{
		"title":  title,
		"active": true,
	}
	data, err := c.post("/brands", body)
	if err != nil {
		return nil, err
	}
	var brand Brand
	if err := json.Unmarshal(data, &brand); err != nil {
		return nil, fmt.Errorf("unmarshal created brand: %w", err)
	}
	return &brand, nil
}

func (c *Client) GetOrCreateBrand(title string) (*Brand, error) {
	brands, err := c.GetAllBrands()
	if err != nil {
		return nil, err
	}
	for i, b := range brands {
		if b.Title == title {
			return &brands[i], nil
		}
	}
	return c.CreateBrand(title)
}

// ============================================================================
// PRODUCTS
// ============================================================================

func (c *Client) ListProducts(offset, count int) ([]Product, error) {
	params := url.Values{
		"count":  {strconv.Itoa(count)},
		"offset": {strconv.Itoa(offset)},
	}
	data, err := c.get("/products", params)
	if err != nil {
		return nil, err
	}
	var products []Product
	if err := json.Unmarshal(data, &products); err != nil {
		return nil, fmt.Errorf("unmarshal products: %w", err)
	}
	return products, nil
}

func (c *Client) GetProduct(id int) (*Product, error) {
	data, err := c.get(fmt.Sprintf("/products/%d", id), nil)
	if err != nil {
		return nil, err
	}
	var product Product
	if err := json.Unmarshal(data, &product); err != nil {
		return nil, fmt.Errorf("unmarshal product: %w", err)
	}
	return &product, nil
}

func (c *Client) CreateProduct(p map[string]interface{}) (*Product, error) {
	data, err := c.post("/products", p)
	if err != nil {
		return nil, err
	}
	var product Product
	if err := json.Unmarshal(data, &product); err != nil {
		return nil, fmt.Errorf("unmarshal created product: %w", err)
	}
	return &product, nil
}

func (c *Client) UpdateProduct(id int, p map[string]interface{}) (*Product, error) {
	data, err := c.put(fmt.Sprintf("/products/%d", id), p)
	if err != nil {
		return nil, err
	}
	var product Product
	if err := json.Unmarshal(data, &product); err != nil {
		return nil, fmt.Errorf("unmarshal updated product: %w", err)
	}
	return &product, nil
}

func (c *Client) DeleteProduct(id int) error {
	return c.del(fmt.Sprintf("/products/%d", id))
}

// ── Product Images ────────────────────────────────────────────────────────────

func (c *Client) AddProductImageURL(productID int, imageURL string) error {
	body := map[string]interface{}{"url": imageURL}
	_, err := c.post(fmt.Sprintf("/products/%d/images", productID), body)
	return err
}

// ── Product Options (for variations) ─────────────────────────────────────────

func (c *Client) CreateProductOption(productID int, title string) (*ProductOption, error) {
	body := map[string]interface{}{"title": title}
	data, err := c.post(fmt.Sprintf("/products/%d/options", productID), body)
	if err != nil {
		return nil, err
	}
	var opt ProductOption
	if err := json.Unmarshal(data, &opt); err != nil {
		return nil, fmt.Errorf("unmarshal option: %w", err)
	}
	return &opt, nil
}

func (c *Client) CreateProductOptionValue(productID, optionID int, title string) (*ProductOptionValue, error) {
	body := map[string]interface{}{"title": title}
	data, err := c.post(fmt.Sprintf("/products/%d/options/%d/values", productID, optionID), body)
	if err != nil {
		return nil, err
	}
	var val ProductOptionValue
	if err := json.Unmarshal(data, &val); err != nil {
		return nil, fmt.Errorf("unmarshal option value: %w", err)
	}
	return &val, nil
}

func (c *Client) ListProductVariations(productID int) ([]ProductVariation, error) {
	data, err := c.get(fmt.Sprintf("/products/%d/variations", productID), nil)
	if err != nil {
		return nil, err
	}
	var vars []ProductVariation
	if err := json.Unmarshal(data, &vars); err != nil {
		return nil, fmt.Errorf("unmarshal variations: %w", err)
	}
	return vars, nil
}

func (c *Client) UpdateProductVariation(productID, variationID int, update map[string]interface{}) error {
	_, err := c.put(fmt.Sprintf("/products/%d/variations/%d", productID, variationID), update)
	return err
}

// ============================================================================
// STOCK
// ============================================================================

func (c *Client) UpdateStock(sku string, quantity int) error {
	body := StockUpdate{SKU: sku, Quantity: quantity}
	_, err := c.put("/stock", body)
	return err
}

// ============================================================================
// ORDERS
// ============================================================================

func (c *Client) ListOrders(offset, count int, status string) ([]Order, error) {
	params := url.Values{
		"count":  {strconv.Itoa(count)},
		"offset": {strconv.Itoa(offset)},
	}
	if status != "" {
		params.Set("status", status)
	}
	data, err := c.get("/orders", params)
	if err != nil {
		return nil, err
	}
	var orders []Order
	if err := json.Unmarshal(data, &orders); err != nil {
		return nil, fmt.Errorf("unmarshal orders: %w", err)
	}
	return orders, nil
}

func (c *Client) GetOrder(id int) (*Order, error) {
	data, err := c.get(fmt.Sprintf("/orders/%d", id), nil)
	if err != nil {
		return nil, err
	}
	var order Order
	if err := json.Unmarshal(data, &order); err != nil {
		return nil, fmt.Errorf("unmarshal order: %w", err)
	}
	return &order, nil
}

func (c *Client) UpdateOrderStatus(id int, status, trackingNumber, carrier string) error {
	body := OrderStatusUpdate{Status: status}
	if trackingNumber != "" {
		body.TrackingNumber = trackingNumber
	}
	if carrier != "" {
		body.ShippingCarrier = carrier
	}
	_, err := c.post(fmt.Sprintf("/orders/%d/status", id), body)
	return err
}

// ============================================================================
// WEBHOOKS
// ============================================================================

func (c *Client) ListWebhooks() ([]Webhook, error) {
	data, err := c.get("/webhooks", nil)
	if err != nil {
		return nil, err
	}
	var webhooks []Webhook
	if err := json.Unmarshal(data, &webhooks); err != nil {
		return nil, fmt.Errorf("unmarshal webhooks: %w", err)
	}
	return webhooks, nil
}

func (c *Client) CreateWebhook(topic, webhookURL string) (*Webhook, error) {
	body := Webhook{
		Topic:  topic,
		URL:    webhookURL,
		Active: true,
	}
	data, err := c.post("/webhooks", body)
	if err != nil {
		return nil, err
	}
	var wh Webhook
	if err := json.Unmarshal(data, &wh); err != nil {
		return nil, fmt.Errorf("unmarshal webhook: %w", err)
	}
	return &wh, nil
}

func (c *Client) DeleteWebhook(id int) error {
	return c.del(fmt.Sprintf("/webhooks/%d", id))
}

// EnsureWebhook creates a webhook for the given topic+url only if one doesn't already exist.
func (c *Client) EnsureWebhook(topic, webhookURL string) (*Webhook, error) {
	existing, err := c.ListWebhooks()
	if err != nil {
		log.Printf("[ShopWired] Could not list webhooks to check for duplicates: %v", err)
	}
	for i, wh := range existing {
		if wh.Topic == topic && wh.URL == webhookURL {
			return &existing[i], nil
		}
	}
	return c.CreateWebhook(topic, webhookURL)
}
