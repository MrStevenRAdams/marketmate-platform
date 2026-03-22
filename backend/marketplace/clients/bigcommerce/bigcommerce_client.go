package bigcommerce

// ============================================================================
// BIGCOMMERCE API CLIENT
// ============================================================================
// Base URL (V3): https://api.bigcommerce.com/stores/{store_hash}/v3
// Base URL (V2): https://api.bigcommerce.com/stores/{store_hash}/v2
// Auth:          X-Auth-Token: {access_token}
//                Content-Type: application/json
// Docs:          https://developer.bigcommerce.com/docs/rest-catalog
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
	"strings"
	"time"
)

// ── Client ────────────────────────────────────────────────────────────────────

type Client struct {
	StoreHash   string
	ClientID    string
	AccessToken string
	BaseV2      string
	BaseV3      string
	HTTPClient  *http.Client
}

func NewClient(storeHash, clientID, accessToken string) *Client {
	storeHash = strings.TrimSpace(storeHash)
	base := "https://api.bigcommerce.com/stores/" + storeHash
	return &Client{
		StoreHash:   storeHash,
		ClientID:    clientID,
		AccessToken: accessToken,
		BaseV2:      base + "/v2",
		BaseV3:      base + "/v3",
		HTTPClient:  &http.Client{Timeout: 30 * time.Second},
	}
}

// ── HTTP helpers ──────────────────────────────────────────────────────────────

func (c *Client) doRequest(method, fullURL string, body interface{}) ([]byte, int, error) {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, fullURL, reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("X-Auth-Token", c.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("execute request to %s: %w", fullURL, err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		var bcErr struct {
			Title  string `json:"title"`
			Detail string `json:"detail"`
			Errors map[string]string `json:"errors"`
		}
		if jsonErr := json.Unmarshal(respBytes, &bcErr); jsonErr == nil {
			msg := bcErr.Title
			if bcErr.Detail != "" {
				msg += ": " + bcErr.Detail
			}
			if msg != "" {
				return nil, resp.StatusCode, fmt.Errorf("BigCommerce API error [HTTP %d]: %s", resp.StatusCode, msg)
			}
		}
		return nil, resp.StatusCode, fmt.Errorf("BigCommerce API HTTP %d: %s", resp.StatusCode, string(respBytes))
	}

	return respBytes, resp.StatusCode, nil
}

func (c *Client) getJSON(fullURL string, out interface{}) error {
	b, _, err := c.doRequest(http.MethodGet, fullURL, nil)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func (c *Client) postJSON(fullURL string, payload interface{}, out interface{}) error {
	b, _, err := c.doRequest(http.MethodPost, fullURL, payload)
	if err != nil {
		return err
	}
	if out != nil {
		return json.Unmarshal(b, out)
	}
	return nil
}

func (c *Client) putJSON(fullURL string, payload interface{}, out interface{}) error {
	b, _, err := c.doRequest(http.MethodPut, fullURL, payload)
	if err != nil {
		return err
	}
	if out != nil {
		return json.Unmarshal(b, out)
	}
	return nil
}

func (c *Client) deleteReq(fullURL string) error {
	_, _, err := c.doRequest(http.MethodDelete, fullURL, nil)
	return err
}

// ── Types — Products (V3) ─────────────────────────────────────────────────────

type Product struct {
	ID                 int                 `json:"id,omitempty"`
	Name               string              `json:"name"`
	Type               string              `json:"type"` // "physical" | "digital"
	SKU                string              `json:"sku,omitempty"`
	Description        string              `json:"description,omitempty"`
	Weight             float64             `json:"weight"`
	Price              float64             `json:"price"`
	SalePrice          float64             `json:"sale_price,omitempty"`
	CostPrice          float64             `json:"cost_price,omitempty"`
	RetailPrice        float64             `json:"retail_price,omitempty"`
	InventoryLevel     int                 `json:"inventory_level,omitempty"`
	InventoryTracking  string              `json:"inventory_tracking,omitempty"` // "none" | "product" | "variant"
	IsVisible          bool                `json:"is_visible"`
	IsFeatured         bool                `json:"is_featured,omitempty"`
	Categories         []int               `json:"categories,omitempty"`
	BrandID            int                 `json:"brand_id,omitempty"`
	CustomURL          *ProductCustomURL   `json:"custom_url,omitempty"`
	Availability       string              `json:"availability,omitempty"` // "available" | "disabled" | "preorder"
	Condition          string              `json:"condition,omitempty"`    // "New" | "Used" | "Refurbished"
	IsConditionShown   bool                `json:"is_condition_shown,omitempty"`
	OrderQuantityMin   int                 `json:"order_quantity_minimum,omitempty"`
	OrderQuantityMax   int                 `json:"order_quantity_maximum,omitempty"`
	PageTitle          string              `json:"page_title,omitempty"`
	MetaDescription    string              `json:"meta_description,omitempty"`
	SearchKeywords     string              `json:"search_keywords,omitempty"`
	Images             []ProductImage      `json:"images,omitempty"`
	Variants           []ProductVariant    `json:"variants,omitempty"`
	CustomFields       []ProductCustomField `json:"custom_fields,omitempty"`
	DateCreated        string              `json:"date_created,omitempty"`
	DateModified       string              `json:"date_modified,omitempty"`
}

type ProductCustomURL struct {
	URL          string `json:"url"`
	IsCustomized bool   `json:"is_customized"`
}

type ProductImage struct {
	ID         int    `json:"id,omitempty"`
	ProductID  int    `json:"product_id,omitempty"`
	IsThumbnail bool  `json:"is_thumbnail,omitempty"`
	SortOrder  int    `json:"sort_order,omitempty"`
	Description string `json:"description,omitempty"`
	ImageURL   string `json:"image_url,omitempty"`
	URLStandard string `json:"url_standard,omitempty"`
	URLThumbnail string `json:"url_thumbnail,omitempty"`
}

type ProductVariant struct {
	ID             int     `json:"id,omitempty"`
	ProductID      int     `json:"product_id,omitempty"`
	SKU            string  `json:"sku,omitempty"`
	Price          float64 `json:"price,omitempty"`
	InventoryLevel int     `json:"inventory_level,omitempty"`
	IsVisible      bool    `json:"is_visible,omitempty"`
}

type ProductCustomField struct {
	ID    int    `json:"id,omitempty"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

// V3 paginated response wrapper
type V3Response struct {
	Data []json.RawMessage `json:"data"`
	Meta V3Meta            `json:"meta"`
}

type V3Meta struct {
	Pagination V3Pagination `json:"pagination"`
}

type V3Pagination struct {
	Total       int `json:"total"`
	Count       int `json:"count"`
	PerPage     int `json:"per_page"`
	CurrentPage int `json:"current_page"`
	TotalPages  int `json:"total_pages"`
}

type ProductListResponse struct {
	Data []Product  `json:"data"`
	Meta V3Meta     `json:"meta"`
}

type ProductResponse struct {
	Data Product `json:"data"`
}

// ── Types — Categories (V3) ───────────────────────────────────────────────────

type Category struct {
	ID          int    `json:"id"`
	ParentID    int    `json:"parent_id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	IsVisible   bool   `json:"is_visible"`
	SortOrder   int    `json:"sort_order,omitempty"`
	URL         string `json:"url,omitempty"`
}

type CategoryListResponse struct {
	Data []Category `json:"data"`
	Meta V3Meta     `json:"meta"`
}

// ── Types — Orders (V2) ───────────────────────────────────────────────────────

type Order struct {
	ID                   int         `json:"id"`
	DateCreated          string      `json:"date_created"`
	DateModified         string      `json:"date_modified"`
	DateShipped          string      `json:"date_shipped,omitempty"`
	StatusID             int         `json:"status_id"`
	Status               string      `json:"status"`
	CartID               string      `json:"cart_id,omitempty"`
	CustomerID           int         `json:"customer_id"`
	TotalIncTax          string      `json:"total_inc_tax"`
	TotalExTax           string      `json:"total_ex_tax"`
	ShippingCostIncTax   string      `json:"shipping_cost_inc_tax"`
	CurrencyCode         string      `json:"currency_code"`
	CustomerMessage      string      `json:"customer_message,omitempty"`
	StaffNotes           string      `json:"staff_notes,omitempty"`
	BillingAddress       OrderAddress `json:"billing_address"`
	ShippingAddresses    V2ResourceRef `json:"shipping_addresses"`
	Products             V2ResourceRef `json:"products"`
	PaymentMethod        string      `json:"payment_method,omitempty"`
	PaymentStatus        string      `json:"payment_status,omitempty"`
	RefundedAmount       string      `json:"refunded_amount,omitempty"`
	OrderSource          string      `json:"order_source,omitempty"`
	ExternalSource       string      `json:"external_source,omitempty"`
	ExternalID           string      `json:"external_id,omitempty"`
}

type V2ResourceRef struct {
	URL      string `json:"url,omitempty"`
	Resource string `json:"resource,omitempty"`
}

type OrderAddress struct {
	ID          int    `json:"id,omitempty"`
	OrderID     int    `json:"order_id,omitempty"`
	FirstName   string `json:"first_name"`
	LastName    string `json:"last_name"`
	Company     string `json:"company,omitempty"`
	Street1     string `json:"street_1"`
	Street2     string `json:"street_2,omitempty"`
	City        string `json:"city"`
	State       string `json:"state"`
	Zip         string `json:"zip"`
	Country     string `json:"country"`
	CountryISO2 string `json:"country_iso2"`
	Phone       string `json:"phone,omitempty"`
	Email       string `json:"email,omitempty"`
}

type OrderProduct struct {
	ID           int     `json:"id"`
	OrderID      int     `json:"order_id"`
	ProductID    int     `json:"product_id"`
	Name         string  `json:"name"`
	SKU          string  `json:"sku"`
	Quantity     int     `json:"quantity"`
	PriceIncTax  string  `json:"price_inc_tax"`
	PriceExTax   string  `json:"price_ex_tax"`
	TotalIncTax  string  `json:"total_inc_tax"`
	TotalExTax   string  `json:"total_ex_tax"`
	Type         string  `json:"type"`
}

type OrderShipmentLine struct {
	OrderProductID int `json:"order_product_id"`
	Quantity       int `json:"quantity"`
}

type CreateShipmentRequest struct {
	TrackingNumber string              `json:"tracking_number"`
	TrackingCarrier string             `json:"tracking_carrier,omitempty"`
	ShippingProvider string            `json:"shipping_provider,omitempty"`
	Comments       string              `json:"comments,omitempty"`
	OrderAddressID int                 `json:"order_address_id"`
	Items          []OrderShipmentLine `json:"items"`
}

type Shipment struct {
	ID             int                 `json:"id"`
	OrderID        int                 `json:"order_id"`
	TrackingNumber string              `json:"tracking_number"`
	TrackingCarrier string             `json:"tracking_carrier"`
	ShippingProvider string            `json:"shipping_provider,omitempty"`
	DateCreated    string              `json:"date_created"`
	Items          []OrderShipmentLine `json:"items"`
}

// ── Product Methods ───────────────────────────────────────────────────────────

// GetProducts returns a paginated list of V3 catalog products.
// page is 1-indexed; limit max is 250.
func (c *Client) GetProducts(page, limit int) (*ProductListResponse, error) {
	params := url.Values{
		"page":    []string{strconv.Itoa(page)},
		"limit":   []string{strconv.Itoa(limit)},
		"include": []string{"images,variants"},
	}
	fullURL := c.BaseV3 + "/catalog/products?" + params.Encode()

	var result ProductListResponse
	if err := c.getJSON(fullURL, &result); err != nil {
		return nil, fmt.Errorf("GetProducts page %d: %w", page, err)
	}
	return &result, nil
}

// GetAllProducts iterates all pages and returns every product.
func (c *Client) GetAllProducts() ([]Product, error) {
	var all []Product
	limit := 250
	page := 1
	for {
		result, err := c.GetProducts(page, limit)
		if err != nil {
			return nil, err
		}
		all = append(all, result.Data...)
		if result.Meta.Pagination.CurrentPage >= result.Meta.Pagination.TotalPages {
			break
		}
		page++
	}
	return all, nil
}

// GetProduct returns a single product by ID.
func (c *Client) GetProduct(id int) (*Product, error) {
	fullURL := fmt.Sprintf("%s/catalog/products/%d?include=images,variants", c.BaseV3, id)
	var result ProductResponse
	if err := c.getJSON(fullURL, &result); err != nil {
		return nil, fmt.Errorf("GetProduct %d: %w", id, err)
	}
	return &result.Data, nil
}

// CreateProduct creates a new product in BigCommerce.
func (c *Client) CreateProduct(req *Product) (*Product, error) {
	if req.Type == "" {
		req.Type = "physical"
	}
	if req.InventoryTracking == "" {
		req.InventoryTracking = "product"
	}
	fullURL := c.BaseV3 + "/catalog/products"
	var result ProductResponse
	if err := c.postJSON(fullURL, req, &result); err != nil {
		return nil, fmt.Errorf("CreateProduct: %w", err)
	}
	log.Printf("[BigCommerce] Created product ID=%d SKU=%s", result.Data.ID, result.Data.SKU)
	return &result.Data, nil
}

// UpdateProduct updates an existing product by ID.
func (c *Client) UpdateProduct(id int, req *Product) (*Product, error) {
	fullURL := fmt.Sprintf("%s/catalog/products/%d", c.BaseV3, id)
	var result ProductResponse
	if err := c.putJSON(fullURL, req, &result); err != nil {
		return nil, fmt.Errorf("UpdateProduct %d: %w", id, err)
	}
	return &result.Data, nil
}

// DeleteProduct permanently deletes a product by ID.
func (c *Client) DeleteProduct(id int) error {
	fullURL := fmt.Sprintf("%s/catalog/products/%d", c.BaseV3, id)
	if err := c.deleteReq(fullURL); err != nil {
		return fmt.Errorf("DeleteProduct %d: %w", id, err)
	}
	return nil
}

// UpdateProductStock updates the inventory level for a product.
func (c *Client) UpdateProductStock(id, quantity int) error {
	req := map[string]interface{}{
		"inventory_level":    quantity,
		"inventory_tracking": "product",
	}
	fullURL := fmt.Sprintf("%s/catalog/products/%d", c.BaseV3, id)
	if err := c.putJSON(fullURL, req, nil); err != nil {
		return fmt.Errorf("UpdateProductStock %d: %w", id, err)
	}
	return nil
}

// UpdateProductPrice updates the price for a product.
func (c *Client) UpdateProductPrice(id int, price float64) error {
	req := map[string]interface{}{"price": price}
	fullURL := fmt.Sprintf("%s/catalog/products/%d", c.BaseV3, id)
	if err := c.putJSON(fullURL, req, nil); err != nil {
		return fmt.Errorf("UpdateProductPrice %d: %w", id, err)
	}
	return nil
}

// SetProductVisible sets a product as visible (published) or hidden (unpublished).
func (c *Client) SetProductVisible(id int, visible bool) error {
	req := map[string]interface{}{"is_visible": visible}
	fullURL := fmt.Sprintf("%s/catalog/products/%d", c.BaseV3, id)
	if err := c.putJSON(fullURL, req, nil); err != nil {
		return fmt.Errorf("SetProductVisible %d: %w", id, err)
	}
	return nil
}

// ── Category Methods ──────────────────────────────────────────────────────────

// GetCategories returns all categories (paginated internally).
func (c *Client) GetCategories() ([]Category, error) {
	var all []Category
	limit := 250
	page := 1
	for {
		params := url.Values{
			"page":  []string{strconv.Itoa(page)},
			"limit": []string{strconv.Itoa(limit)},
		}
		fullURL := c.BaseV3 + "/catalog/categories?" + params.Encode()
		var result CategoryListResponse
		if err := c.getJSON(fullURL, &result); err != nil {
			return nil, fmt.Errorf("GetCategories page %d: %w", page, err)
		}
		all = append(all, result.Data...)
		if result.Meta.Pagination.CurrentPage >= result.Meta.Pagination.TotalPages {
			break
		}
		page++
	}
	return all, nil
}

// ── Order Methods (V2) ────────────────────────────────────────────────────────

// GetOrders returns orders from the V2 API, optionally filtering by minimum creation date.
// statusID: 0 = all, 11 = Awaiting Fulfillment, 9 = Awaiting Shipment, etc.
func (c *Client) GetOrders(page int, minDateCreated time.Time, statusID int) ([]Order, error) {
	params := url.Values{
		"page":  []string{strconv.Itoa(page)},
		"limit": []string{"50"},
		"sort":  []string{"date_created:desc"},
	}
	if !minDateCreated.IsZero() {
		// BigCommerce V2 uses RFC2822-like date: "Mon, 02 Jan 2006 15:04:05 +0000"
		params.Set("min_date_created", minDateCreated.UTC().Format(time.RFC1123Z))
	}
	if statusID > 0 {
		params.Set("status_id", strconv.Itoa(statusID))
	}

	fullURL := c.BaseV2 + "/orders?" + params.Encode()
	var orders []Order
	b, statusCode, err := c.doRequest(http.MethodGet, fullURL, nil)
	if err != nil {
		// 204 No Content means no orders — not an error
		if statusCode == 204 {
			return []Order{}, nil
		}
		return nil, fmt.Errorf("GetOrders page %d: %w", page, err)
	}
	if err := json.Unmarshal(b, &orders); err != nil {
		return nil, fmt.Errorf("GetOrders unmarshal: %w", err)
	}
	return orders, nil
}

// FetchNewOrders fetches all orders since the given time using multi-page iteration.
// It fetches open statuses: 11 (Awaiting Fulfillment), 9 (Awaiting Shipment), 1 (Pending).
func (c *Client) FetchNewOrders(after time.Time) ([]Order, error) {
	var all []Order

	// Status IDs to import: 1=Pending, 11=Awaiting Fulfillment, 9=Awaiting Shipment
	statusIDs := []int{1, 11, 9}

	for _, statusID := range statusIDs {
		page := 1
		for {
			orders, err := c.GetOrders(page, after, statusID)
			if err != nil {
				log.Printf("[BigCommerce] Warning: failed to fetch orders status=%d page=%d: %v", statusID, page, err)
				break
			}
			if len(orders) == 0 {
				break
			}
			all = append(all, orders...)
			if len(orders) < 50 {
				break // last page
			}
			page++
		}
	}
	return all, nil
}

// GetOrder returns a single order by ID (V2).
func (c *Client) GetOrder(id int) (*Order, error) {
	fullURL := fmt.Sprintf("%s/orders/%d", c.BaseV2, id)
	var order Order
	if err := c.getJSON(fullURL, &order); err != nil {
		return nil, fmt.Errorf("GetOrder %d: %w", id, err)
	}
	return &order, nil
}

// GetOrderProducts returns the line items for a V2 order.
func (c *Client) GetOrderProducts(orderID int) ([]OrderProduct, error) {
	fullURL := fmt.Sprintf("%s/orders/%d/products", c.BaseV2, orderID)
	var products []OrderProduct
	if err := c.getJSON(fullURL, &products); err != nil {
		return nil, fmt.Errorf("GetOrderProducts %d: %w", orderID, err)
	}
	return products, nil
}

// GetOrderShippingAddresses returns shipping addresses for a V2 order.
func (c *Client) GetOrderShippingAddresses(orderID int) ([]OrderAddress, error) {
	fullURL := fmt.Sprintf("%s/orders/%d/shipping_addresses", c.BaseV2, orderID)
	var addresses []OrderAddress
	if err := c.getJSON(fullURL, &addresses); err != nil {
		return nil, fmt.Errorf("GetOrderShippingAddresses %d: %w", orderID, err)
	}
	return addresses, nil
}

// CreateShipment creates a shipment (with tracking) for a V2 order.
// orderAddressID must be a valid shipping address ID from the order.
func (c *Client) CreateShipment(orderID int, req *CreateShipmentRequest) (*Shipment, error) {
	fullURL := fmt.Sprintf("%s/orders/%d/shipments", c.BaseV2, orderID)
	var shipment Shipment
	if err := c.postJSON(fullURL, req, &shipment); err != nil {
		return nil, fmt.Errorf("CreateShipment order=%d: %w", orderID, err)
	}
	log.Printf("[BigCommerce] Created shipment ID=%d for order %d tracking=%s",
		shipment.ID, orderID, req.TrackingNumber)
	return &shipment, nil
}

// ── TestConnection ────────────────────────────────────────────────────────────

// TestConnection verifies the API credentials by fetching the store information.
func (c *Client) TestConnection() error {
	fullURL := "https://api.bigcommerce.com/stores/" + c.StoreHash + "/v2/store"
	var storeInfo map[string]interface{}
	if err := c.getJSON(fullURL, &storeInfo); err != nil {
		return fmt.Errorf("BigCommerce connection test failed: %w", err)
	}
	if name, ok := storeInfo["name"].(string); ok {
		log.Printf("[BigCommerce] Connection OK — store: %s", name)
	}
	return nil
}
