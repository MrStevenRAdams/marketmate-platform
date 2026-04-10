package woocommerce

// ============================================================================
// WOOCOMMERCE REST API v3 CLIENT
// ============================================================================
// Base URL:  {store_url}/wp-json/wc/v3
// Auth:      Basic Auth with consumer_key:consumer_secret (base64-encoded).
//            Works over HTTPS. For non-HTTPS, OAuth 1.0a is needed but
//            production WooCommerce stores must use HTTPS.
// Docs:      https://woocommerce.github.io/woocommerce-rest-api-docs/
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
	"strings"
	"time"
)

// ── Types ─────────────────────────────────────────────────────────────────────

type Client struct {
	StoreURL        string
	ConsumerKey     string
	ConsumerSecret  string
	BaseURL         string
	HTTPClient      *http.Client
}

func NewClient(storeURL, consumerKey, consumerSecret string) *Client {
	// Normalise store URL — strip trailing slash
	storeURL = strings.TrimRight(storeURL, "/")
	return &Client{
		StoreURL:       storeURL,
		ConsumerKey:    consumerKey,
		ConsumerSecret: consumerSecret,
		BaseURL:        storeURL + "/wp-json/wc/v3",
		HTTPClient:     &http.Client{Timeout: 30 * time.Second},
	}
}

// ── Auth ──────────────────────────────────────────────────────────────────────

func (c *Client) authHeader() string {
	raw := c.ConsumerKey + ":" + c.ConsumerSecret
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
		return nil, 0, fmt.Errorf("execute request to %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		// Try to extract WooCommerce error message
		var wooErr struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		if jsonErr := json.Unmarshal(respBytes, &wooErr); jsonErr == nil && wooErr.Message != "" {
			return nil, resp.StatusCode, fmt.Errorf("WooCommerce API error [%s]: %s", wooErr.Code, wooErr.Message)
		}
		return nil, resp.StatusCode, fmt.Errorf("WooCommerce API HTTP %d: %s", resp.StatusCode, string(respBytes))
	}

	return respBytes, resp.StatusCode, nil
}

func (c *Client) getJSON(path string, params url.Values, out interface{}) error {
	b, _, err := c.doRequest(http.MethodGet, path, nil, params)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func (c *Client) postJSON(path string, payload interface{}, out interface{}) error {
	b, _, err := c.doRequest(http.MethodPost, path, payload, nil)
	if err != nil {
		return err
	}
	if out != nil {
		return json.Unmarshal(b, out)
	}
	return nil
}

func (c *Client) putJSON(path string, payload interface{}, out interface{}) error {
	b, _, err := c.doRequest(http.MethodPut, path, payload, nil)
	if err != nil {
		return err
	}
	if out != nil {
		return json.Unmarshal(b, out)
	}
	return nil
}

func (c *Client) deleteJSON(path string) error {
	params := url.Values{"force": []string{"true"}}
	_, _, err := c.doRequest(http.MethodDelete, path, nil, params)
	return err
}

// ── Product Types ─────────────────────────────────────────────────────────────

type ProductImage struct {
	ID  int    `json:"id,omitempty"`
	Src string `json:"src"`
	Alt string `json:"alt,omitempty"`
}

type ProductCategory struct {
	ID   int    `json:"id"`
	Name string `json:"name,omitempty"`
	Slug string `json:"slug,omitempty"`
}

type ProductDimensions struct {
	Length string `json:"length,omitempty"`
	Width  string `json:"width,omitempty"`
	Height string `json:"height,omitempty"`
}

type ProductAttribute struct {
	ID        int      `json:"id,omitempty"`
	Name      string   `json:"name"`
	Options   []string `json:"options"`
	Visible   bool     `json:"visible"`
	Variation bool     `json:"variation"`
}

type ProductVariation struct {
	ID             int              `json:"id,omitempty"`
	SKU            string           `json:"sku,omitempty"`
	Price          string           `json:"price,omitempty"`
	RegularPrice   string           `json:"regular_price,omitempty"`
	SalePrice      string           `json:"sale_price,omitempty"`
	StockQuantity  *int             `json:"stock_quantity,omitempty"`
	StockStatus    string           `json:"stock_status,omitempty"`
	Attributes     []VariationAttr  `json:"attributes,omitempty"`
}

type VariationAttr struct {
	ID     int    `json:"id,omitempty"`
	Name   string `json:"name"`
	Option string `json:"option"`
}

type Product struct {
	ID                int                `json:"id,omitempty"`
	Name              string             `json:"name"`
	Slug              string             `json:"slug,omitempty"`
	Status            string             `json:"status,omitempty"` // draft, pending, private, publish
	Type              string             `json:"type,omitempty"`   // simple, variable, grouped, external
	Description       string             `json:"description,omitempty"`
	ShortDescription  string             `json:"short_description,omitempty"`
	SKU               string             `json:"sku,omitempty"`
	Price             string             `json:"price,omitempty"`
	RegularPrice      string             `json:"regular_price,omitempty"`
	SalePrice         string             `json:"sale_price,omitempty"`
	ManageStock       bool               `json:"manage_stock,omitempty"`
	StockQuantity     *int               `json:"stock_quantity,omitempty"`
	StockStatus       string             `json:"stock_status,omitempty"`
	Weight            string             `json:"weight,omitempty"`
	Dimensions        ProductDimensions  `json:"dimensions,omitempty"`
	Categories        []ProductCategory  `json:"categories,omitempty"`
	Images            []ProductImage     `json:"images,omitempty"`
	Attributes        []ProductAttribute `json:"attributes,omitempty"`
	Downloadable      bool               `json:"downloadable,omitempty"`
	Virtual           bool               `json:"virtual,omitempty"`
	TaxStatus         string             `json:"tax_status,omitempty"`
	TaxClass          string             `json:"tax_class,omitempty"`
	Permalink         string             `json:"permalink,omitempty"`
	DateCreated       string             `json:"date_created,omitempty"`
	DateModified      string             `json:"date_modified,omitempty"`
}

type Category struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Slug   string `json:"slug"`
	Parent int    `json:"parent"`
	Count  int    `json:"count"`
}

// ── Products ──────────────────────────────────────────────────────────────────

// GetProducts returns a paginated list of products.
// page is 1-indexed; perPage max is 100.
func (c *Client) GetProducts(page, perPage int, status string) ([]Product, error) {
	params := url.Values{
		"page":     []string{strconv.Itoa(page)},
		"per_page": []string{strconv.Itoa(perPage)},
	}
	if status != "" {
		params.Set("status", status)
	}

	var products []Product
	if err := c.getJSON("/products", params, &products); err != nil {
		return nil, fmt.Errorf("GetProducts: %w", err)
	}
	return products, nil
}

// GetAllProducts iterates all pages and returns every product.
func (c *Client) GetAllProducts(status string) ([]Product, error) {
	var all []Product
	page := 1
	for {
		batch, err := c.GetProducts(page, 100, status)
		if err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if len(batch) < 100 {
			break
		}
		page++
	}
	return all, nil
}

// GetProduct returns a single product by ID.
func (c *Client) GetProduct(productID int) (*Product, error) {
	var p Product
	if err := c.getJSON(fmt.Sprintf("/products/%d", productID), nil, &p); err != nil {
		return nil, fmt.Errorf("GetProduct %d: %w", productID, err)
	}
	return &p, nil
}

// CreateProduct creates a new product and returns the created product.
func (c *Client) CreateProduct(req *Product) (*Product, error) {
	var created Product
	if err := c.postJSON("/products", req, &created); err != nil {
		return nil, fmt.Errorf("CreateProduct: %w", err)
	}
	log.Printf("[WooCommerce] Created product %d: %s", created.ID, created.Name)
	return &created, nil
}

// UpdateProduct updates an existing product by ID.
func (c *Client) UpdateProduct(productID int, req *Product) (*Product, error) {
	var updated Product
	if err := c.putJSON(fmt.Sprintf("/products/%d", productID), req, &updated); err != nil {
		return nil, fmt.Errorf("UpdateProduct %d: %w", productID, err)
	}
	return &updated, nil
}

// DeleteProduct permanently deletes a product.
func (c *Client) DeleteProduct(productID int) error {
	if err := c.deleteJSON(fmt.Sprintf("/products/%d", productID)); err != nil {
		return fmt.Errorf("DeleteProduct %d: %w", productID, err)
	}
	return nil
}

// UpdateProductStock updates only the stock quantity for a product.
func (c *Client) UpdateProductStock(productID, quantity int) error {
	payload := map[string]interface{}{
		"manage_stock":   true,
		"stock_quantity": quantity,
	}
	_, _, err := c.doRequest(http.MethodPut, fmt.Sprintf("/products/%d", productID), payload, nil)
	return err
}

// UpdateProductPrice updates only the regular price of a product.
func (c *Client) UpdateProductPrice(productID int, price float64) error {
	priceStr := strconv.FormatFloat(price, 'f', 2, 64)
	payload := map[string]interface{}{
		"regular_price": priceStr,
	}
	_, _, err := c.doRequest(http.MethodPut, fmt.Sprintf("/products/%d", productID), payload, nil)
	return err
}

// ── Categories ────────────────────────────────────────────────────────────────

// GetCategories returns all product categories (paginated internally).
func (c *Client) GetCategories() ([]Category, error) {
	var all []Category
	page := 1
	for {
		params := url.Values{
			"page":     []string{strconv.Itoa(page)},
			"per_page": []string{"100"},
		}
		var batch []Category
		if err := c.getJSON("/products/categories", params, &batch); err != nil {
			return nil, fmt.Errorf("GetCategories: %w", err)
		}
		all = append(all, batch...)
		if len(batch) < 100 {
			break
		}
		page++
	}
	return all, nil
}

// ── Attributes ────────────────────────────────────────────────────────────────

type Attribute struct {
	ID          int      `json:"id"`
	Name        string   `json:"name"`
	Slug        string   `json:"slug"`
	Type        string   `json:"type"`
	OrderBy     string   `json:"order_by"`
	HasArchives bool     `json:"has_archives"`
}

func (c *Client) GetAttributes() ([]Attribute, error) {
	var attrs []Attribute
	if err := c.getJSON("/products/attributes", nil, &attrs); err != nil {
		return nil, fmt.Errorf("GetAttributes: %w", err)
	}
	return attrs, nil
}

// ── Connection Test ───────────────────────────────────────────────────────────

// TestConnection performs a lightweight call (GET /system_status) to verify credentials.
func (c *Client) TestConnection() error {
	params := url.Values{"per_page": []string{"1"}}
	var result interface{}
	if err := c.getJSON("/products", params, &result); err != nil {
		return fmt.Errorf("WooCommerce connection test failed: %w", err)
	}
	return nil
}

// GetSystemStatus returns WooCommerce system status (version, etc.)
func (c *Client) GetSystemStatus() (map[string]interface{}, error) {
	var status map[string]interface{}
	if err := c.getJSON("/system_status", nil, &status); err != nil {
		return nil, fmt.Errorf("GetSystemStatus: %w", err)
	}
	return status, nil
}

// ── Order Types ───────────────────────────────────────────────────────────────

type OrderBilling struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Company   string `json:"company,omitempty"`
	Address1  string `json:"address_1"`
	Address2  string `json:"address_2,omitempty"`
	City      string `json:"city"`
	State     string `json:"state"`
	Postcode  string `json:"postcode"`
	Country   string `json:"country"`
	Email     string `json:"email,omitempty"`
	Phone     string `json:"phone,omitempty"`
}

type OrderShipping struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Company   string `json:"company,omitempty"`
	Address1  string `json:"address_1"`
	Address2  string `json:"address_2,omitempty"`
	City      string `json:"city"`
	State     string `json:"state"`
	Postcode  string `json:"postcode"`
	Country   string `json:"country"`
}

type OrderLineItem struct {
	ID          int    `json:"id,omitempty"`
	Name        string `json:"name"`
	ProductID   int    `json:"product_id"`
	VariationID int    `json:"variation_id,omitempty"`
	Quantity    int    `json:"quantity"`
	SKU         string `json:"sku,omitempty"`
	Price       string `json:"price,omitempty"`
	Total       string `json:"total,omitempty"`
	Subtotal    string `json:"subtotal,omitempty"`
}

type OrderShippingLine struct {
	ID          int    `json:"id,omitempty"`
	MethodTitle string `json:"method_title"`
	Total       string `json:"total"`
}

type OrderMetaData struct {
	ID    int    `json:"id,omitempty"`
	Key   string `json:"key"`
	Value string `json:"value"`
}

type Order struct {
	ID                 int                 `json:"id,omitempty"`
	Number             string              `json:"number,omitempty"`
	Status             string              `json:"status"` // pending, processing, on-hold, completed, cancelled, refunded, failed
	Currency           string              `json:"currency,omitempty"`
	DateCreated        string              `json:"date_created,omitempty"`
	DateModified       string              `json:"date_modified,omitempty"`
	Total              string              `json:"total,omitempty"`
	ShippingTotal      string              `json:"shipping_total,omitempty"`
	TotalTax           string              `json:"total_tax,omitempty"`
	Billing            OrderBilling        `json:"billing"`
	Shipping           OrderShipping       `json:"shipping"`
	LineItems          []OrderLineItem     `json:"line_items,omitempty"`
	ShippingLines      []OrderShippingLine `json:"shipping_lines,omitempty"`
	MetaData           []OrderMetaData     `json:"meta_data,omitempty"`
	CustomerNote       string              `json:"customer_note,omitempty"`
	PaymentMethod      string              `json:"payment_method,omitempty"`
	PaymentMethodTitle string              `json:"payment_method_title,omitempty"`
	TransactionID      string              `json:"transaction_id,omitempty"`
}

type OrderNote struct {
	ID             int    `json:"id,omitempty"`
	Note           string `json:"note"`
	CustomerNote   bool   `json:"customer_note,omitempty"`
	DateCreated    string `json:"date_created,omitempty"`
}

// ── Orders ────────────────────────────────────────────────────────────────────

// GetOrders returns orders filtered by date range and status.
// after/before are RFC3339 strings (ISO 8601). Pass empty string to omit.
func (c *Client) GetOrders(after, before, status string, page, perPage int) ([]Order, error) {
	params := url.Values{
		"page":     []string{strconv.Itoa(page)},
		"per_page": []string{strconv.Itoa(perPage)},
		"orderby":  []string{"date"},
		"order":    []string{"desc"},
	}
	if after != "" {
		params.Set("after", after)
	}
	if before != "" {
		params.Set("before", before)
	}
	if status != "" {
		params.Set("status", status)
	}

	var orders []Order
	if err := c.getJSON("/orders", params, &orders); err != nil {
		return nil, fmt.Errorf("GetOrders: %w", err)
	}
	return orders, nil
}

// GetOrder returns a single order by ID.
func (c *Client) GetOrder(orderID int) (*Order, error) {
	var o Order
	if err := c.getJSON(fmt.Sprintf("/orders/%d", orderID), nil, &o); err != nil {
		return nil, fmt.Errorf("GetOrder %d: %w", orderID, err)
	}
	return &o, nil
}

// UpdateOrderStatus changes the status of an order.
func (c *Client) UpdateOrderStatus(orderID int, status string) error {
	payload := map[string]string{"status": status}
	_, _, err := c.doRequest(http.MethodPut, fmt.Sprintf("/orders/%d", orderID), payload, nil)
	return err
}

// UpdateOrderMetaData sets or updates meta fields on an order (used for tracking).
func (c *Client) UpdateOrderMetaData(orderID int, meta []OrderMetaData) error {
	payload := map[string]interface{}{"meta_data": meta}
	_, _, err := c.doRequest(http.MethodPut, fmt.Sprintf("/orders/%d", orderID), payload, nil)
	return err
}

// CreateOrderNote adds a note to an order (customer-visible or private).
func (c *Client) CreateOrderNote(orderID int, note string, customerNote bool) (*OrderNote, error) {
	payload := OrderNote{
		Note:         note,
		CustomerNote: customerNote,
	}
	var created OrderNote
	if err := c.postJSON(fmt.Sprintf("/orders/%d/notes", orderID), payload, &created); err != nil {
		return nil, fmt.Errorf("CreateOrderNote %d: %w", orderID, err)
	}
	return &created, nil
}

// FetchNewOrders fetches orders created between after and before (inclusive).
// Iterates all pages. Pass status="" for all statuses.
func (c *Client) FetchNewOrders(after, before time.Time, status string) ([]Order, error) {
	afterStr := after.UTC().Format(time.RFC3339)
	beforeStr := before.UTC().Format(time.RFC3339)

	var all []Order
	page := 1
	for {
		batch, err := c.GetOrders(afterStr, beforeStr, status, page, 100)
		if err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if len(batch) < 100 {
			break
		}
		page++
	}
	return all, nil
}

// PushTracking sets tracking information on a WooCommerce order.
// WooCommerce has no native tracking endpoint — we write standard meta fields
// and add an order note so the seller and customer can see it.
func (c *Client) PushTracking(orderID int, trackingNumber, carrier, trackingURL string) error {
	meta := []OrderMetaData{
		{Key: "_tracking_number", Value: trackingNumber},
		{Key: "_tracking_provider", Value: carrier},
	}
	if trackingURL != "" {
		meta = append(meta, OrderMetaData{Key: "_tracking_url", Value: trackingURL})
	}

	if err := c.UpdateOrderMetaData(orderID, meta); err != nil {
		return fmt.Errorf("set tracking meta: %w", err)
	}

	note := fmt.Sprintf("Order shipped. Tracking number: %s. Carrier: %s.", trackingNumber, carrier)
	if trackingURL != "" {
		note += fmt.Sprintf(" Track at: %s", trackingURL)
	}
	if _, err := c.CreateOrderNote(orderID, note, true); err != nil {
		// Non-fatal — tracking meta already set
		log.Printf("[WooCommerce] Warning: could not add order note for order %d: %v", orderID, err)
	}

	return nil
}

// ============================================================================
// PRODUCT VARIATIONS — SESSION H
// ============================================================================
// WooCommerce variable products support multiple variations, each with its own
// SKU, price, stock, and attribute combination.
// Reference: POST /wp-json/wc/v3/products/{product_id}/variations
// ============================================================================

// CreateVariation creates a child variation under an existing variable product.
// Returns the created variation (including the variation ID assigned by WooCommerce).
func (c *Client) CreateVariation(productID int, variation *ProductVariation) (*ProductVariation, error) {
	path := fmt.Sprintf("/products/%d/variations", productID)
	var result ProductVariation
	if err := c.postJSON(path, variation, &result); err != nil {
		return nil, fmt.Errorf("create variation for product %d: %w", productID, err)
	}
	return &result, nil
}
