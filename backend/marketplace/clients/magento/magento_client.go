package magento

// ============================================================================
// MAGENTO 2 REST API CLIENT
// ============================================================================
// Base URL:  {store_url}/rest/V1
// Auth:      Authorization: Bearer {integration_token}
// Docs:      https://developer.adobe.com/commerce/webapi/rest/
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

// ── Types ─────────────────────────────────────────────────────────────────────

type Client struct {
	StoreURL         string
	IntegrationToken string
	BaseURL          string
	HTTPClient       *http.Client
}

func NewClient(storeURL, integrationToken string) *Client {
	storeURL = strings.TrimRight(storeURL, "/")
	return &Client{
		StoreURL:         storeURL,
		IntegrationToken: integrationToken,
		BaseURL:          storeURL + "/rest/V1",
		HTTPClient:       &http.Client{Timeout: 30 * time.Second},
	}
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
	req.Header.Set("Authorization", "Bearer "+c.IntegrationToken)
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
		var magentoErr struct {
			Message string `json:"message"`
		}
		if jsonErr := json.Unmarshal(respBytes, &magentoErr); jsonErr == nil && magentoErr.Message != "" {
			return nil, resp.StatusCode, fmt.Errorf("Magento API error [HTTP %d]: %s", resp.StatusCode, magentoErr.Message)
		}
		return nil, resp.StatusCode, fmt.Errorf("Magento API HTTP %d: %s", resp.StatusCode, string(respBytes))
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

func (c *Client) deleteReq(path string) error {
	_, _, err := c.doRequest(http.MethodDelete, path, nil, nil)
	return err
}

// ── Product Types ─────────────────────────────────────────────────────────────

type ProductCustomAttribute struct {
	AttributeCode string      `json:"attribute_code"`
	Value         interface{} `json:"value"`
}

type StockItem struct {
	ItemID                 int     `json:"item_id,omitempty"`
	ProductID              int     `json:"product_id,omitempty"`
	StockID                int     `json:"stock_id,omitempty"`
	Qty                    float64 `json:"qty"`
	IsInStock              bool    `json:"is_in_stock"`
	IsQtyDecimal           bool    `json:"is_qty_decimal,omitempty"`
	ManageStock            bool    `json:"manage_stock"`
}

type ProductExtensionAttributes struct {
	StockItem *StockItem `json:"stock_item,omitempty"`
}

type MediaGalleryEntry struct {
	ID           int      `json:"id,omitempty"`
	MediaType    string   `json:"media_type"`
	Label        string   `json:"label,omitempty"`
	Position     int      `json:"position"`
	Disabled     bool     `json:"disabled"`
	Types        []string `json:"types,omitempty"`
	File         string   `json:"file,omitempty"`
	Content      *MediaEntryContent `json:"content,omitempty"`
}

type MediaEntryContent struct {
	Base64EncodedData string `json:"base64_encoded_data"`
	Type              string `json:"type"`
	Name              string `json:"name"`
}

type Product struct {
	ID                  int                         `json:"id,omitempty"`
	SKU                 string                      `json:"sku"`
	Name                string                      `json:"name"`
	AttributeSetID      int                         `json:"attribute_set_id,omitempty"`
	Price               float64                     `json:"price"`
	Status              int                         `json:"status,omitempty"` // 1=enabled, 2=disabled
	Visibility          int                         `json:"visibility,omitempty"` // 1=Not Visible, 2=Catalog, 3=Search, 4=Both
	TypeID              string                      `json:"type_id,omitempty"` // simple, configurable, virtual, etc.
	CreatedAt           string                      `json:"created_at,omitempty"`
	UpdatedAt           string                      `json:"updated_at,omitempty"`
	Weight              float64                     `json:"weight,omitempty"`
	ExtensionAttributes *ProductExtensionAttributes `json:"extension_attributes,omitempty"`
	CustomAttributes    []ProductCustomAttribute    `json:"custom_attributes,omitempty"`
	MediaGalleryEntries []MediaGalleryEntry         `json:"media_gallery_entries,omitempty"`
}

// GetCustomAttribute retrieves a custom attribute value by code.
func (p *Product) GetCustomAttribute(code string) string {
	for _, attr := range p.CustomAttributes {
		if attr.AttributeCode == code {
			if s, ok := attr.Value.(string); ok {
				return s
			}
			return fmt.Sprintf("%v", attr.Value)
		}
	}
	return ""
}

type ProductSearchResult struct {
	Items        []Product  `json:"items"`
	SearchCriteria interface{} `json:"search_criteria"`
	TotalCount   int        `json:"total_count"`
}

type Category struct {
	ID              int        `json:"id"`
	ParentID        int        `json:"parent_id"`
	Name            string     `json:"name"`
	IsActive        bool       `json:"is_active"`
	Position        int        `json:"position,omitempty"`
	Level           int        `json:"level"`
	ProductCount    int        `json:"product_count,omitempty"`
	ChildrenData    []Category `json:"children_data,omitempty"`
}

// ── Products ──────────────────────────────────────────────────────────────────

// GetProducts returns a paginated list of products.
// page is 1-indexed; pageSize max is 200.
func (c *Client) GetProducts(page, pageSize int) (*ProductSearchResult, error) {
	params := url.Values{
		"searchCriteria[pageSize]":    []string{strconv.Itoa(pageSize)},
		"searchCriteria[currentPage]": []string{strconv.Itoa(page)},
	}
	var result ProductSearchResult
	if err := c.getJSON("/products", params, &result); err != nil {
		return nil, fmt.Errorf("GetProducts: %w", err)
	}
	return &result, nil
}

// GetAllProducts iterates all pages and returns every product.
func (c *Client) GetAllProducts() ([]Product, error) {
	var all []Product
	page := 1
	const pageSize = 100
	for {
		result, err := c.GetProducts(page, pageSize)
		if err != nil {
			return nil, err
		}
		all = append(all, result.Items...)
		if len(result.Items) < pageSize {
			break
		}
		page++
	}
	return all, nil
}

// GetProduct returns a single product by SKU.
func (c *Client) GetProduct(sku string) (*Product, error) {
	var p Product
	encodedSKU := url.PathEscape(sku)
	if err := c.getJSON("/products/"+encodedSKU, nil, &p); err != nil {
		return nil, fmt.Errorf("GetProduct %s: %w", sku, err)
	}
	return &p, nil
}

// CreateProduct creates a new product and returns it.
func (c *Client) CreateProduct(req *Product) (*Product, error) {
	if req.AttributeSetID == 0 {
		req.AttributeSetID = 4 // Default attribute set
	}
	if req.TypeID == "" {
		req.TypeID = "simple"
	}
	if req.Status == 0 {
		req.Status = 1 // Enabled
	}
	if req.Visibility == 0 {
		req.Visibility = 4 // Catalog + Search
	}

	payload := map[string]interface{}{"product": req}
	var created Product
	if err := c.postJSON("/products", payload, &created); err != nil {
		return nil, fmt.Errorf("CreateProduct: %w", err)
	}
	log.Printf("[Magento] Created product SKU=%s ID=%d", created.SKU, created.ID)
	return &created, nil
}

// UpdateProduct updates an existing product by SKU.
func (c *Client) UpdateProduct(sku string, req *Product) (*Product, error) {
	encodedSKU := url.PathEscape(sku)
	payload := map[string]interface{}{"product": req}
	var updated Product
	if err := c.putJSON("/products/"+encodedSKU, payload, &updated); err != nil {
		return nil, fmt.Errorf("UpdateProduct %s: %w", sku, err)
	}
	return &updated, nil
}

// DeleteProduct permanently deletes a product by SKU.
func (c *Client) DeleteProduct(sku string) error {
	encodedSKU := url.PathEscape(sku)
	if err := c.deleteReq("/products/" + encodedSKU); err != nil {
		return fmt.Errorf("DeleteProduct %s: %w", sku, err)
	}
	return nil
}

// UpdateProductStock updates the stock quantity for a product SKU.
// Uses the stock items endpoint (stockId=1 is the default stock).
func (c *Client) UpdateProductStock(sku string, quantity int) error {
	encodedSKU := url.PathEscape(sku)
	payload := map[string]interface{}{
		"stockItem": map[string]interface{}{
			"qty":          quantity,
			"is_in_stock":  quantity > 0,
			"manage_stock": true,
		},
	}
	_, _, err := c.doRequest(http.MethodPut, "/products/"+encodedSKU+"/stockItems/1", payload, nil)
	if err != nil {
		return fmt.Errorf("UpdateProductStock %s: %w", sku, err)
	}
	return nil
}

// UpdateProductPrice updates only the price of a product by SKU.
func (c *Client) UpdateProductPrice(sku string, price float64) error {
	product := &Product{
		SKU:   sku,
		Price: price,
	}
	_, err := c.UpdateProduct(sku, product)
	return err
}

// UpdateProductStatus enables or disables a product (status: 1=enabled, 2=disabled).
func (c *Client) UpdateProductStatus(sku string, status int) error {
	product := &Product{
		SKU:    sku,
		Status: status,
	}
	_, err := c.UpdateProduct(sku, product)
	return err
}

// ── Categories ────────────────────────────────────────────────────────────────

// GetCategories returns the full category tree.
func (c *Client) GetCategories() (*Category, error) {
	var root Category
	if err := c.getJSON("/categories", nil, &root); err != nil {
		return nil, fmt.Errorf("GetCategories: %w", err)
	}
	return &root, nil
}

// FlattenCategories recursively flattens the category tree into a slice.
func FlattenCategories(root *Category) []Category {
	var result []Category
	var walk func(c Category)
	walk = func(c Category) {
		result = append(result, c)
		for _, child := range c.ChildrenData {
			walk(child)
		}
	}
	if root != nil {
		walk(*root)
	}
	return result
}

// ── Order Types ───────────────────────────────────────────────────────────────

type OrderAddress struct {
	AddressType string   `json:"address_type,omitempty"`
	City        string   `json:"city"`
	CountryID   string   `json:"country_id"`
	Email       string   `json:"email,omitempty"`
	Firstname   string   `json:"firstname"`
	Lastname    string   `json:"lastname"`
	Company     string   `json:"company,omitempty"`
	PostalCode  string   `json:"postcode"`
	Region      string   `json:"region,omitempty"`
	RegionCode  string   `json:"region_code,omitempty"`
	Street      []string `json:"street,omitempty"`
	Telephone   string   `json:"telephone,omitempty"`
}

type OrderItem struct {
	ItemID          int     `json:"item_id,omitempty"`
	OrderID         int     `json:"order_id,omitempty"`
	SKU             string  `json:"sku"`
	Name            string  `json:"name"`
	QtyOrdered      float64 `json:"qty_ordered"`
	QtyShipped      float64 `json:"qty_shipped,omitempty"`
	Price           float64 `json:"price"`
	RowTotal        float64 `json:"row_total,omitempty"`
	ProductID       int     `json:"product_id,omitempty"`
	ProductType     string  `json:"product_type,omitempty"`
}

type OrderPaymentAdditional struct {
	Method string `json:"method,omitempty"`
}

type Order struct {
	EntityID           int           `json:"entity_id,omitempty"`
	IncrementID        string        `json:"increment_id"`
	Status             string        `json:"status"`
	State              string        `json:"state,omitempty"`
	CreatedAt          string        `json:"created_at,omitempty"`
	UpdatedAt          string        `json:"updated_at,omitempty"`
	CustomerEmail      string        `json:"customer_email,omitempty"`
	CustomerFirstname  string        `json:"customer_firstname,omitempty"`
	CustomerLastname   string        `json:"customer_lastname,omitempty"`
	CustomerIsGuest    int           `json:"customer_is_guest,omitempty"`
	GrandTotal         float64       `json:"grand_total"`
	Subtotal           float64       `json:"subtotal,omitempty"`
	ShippingAmount     float64       `json:"shipping_amount,omitempty"`
	TaxAmount          float64       `json:"tax_amount,omitempty"`
	OrderCurrencyCode  string        `json:"order_currency_code,omitempty"`
	BillingAddress     *OrderAddress `json:"billing_address,omitempty"`
	ShippingAddress    *OrderAddress `json:"extension_attributes>shipping_assignments[0]>shipping>address,omitempty"`
	Items              []OrderItem   `json:"items,omitempty"`
	Payment            *OrderPayment `json:"payment,omitempty"`
	ShippingDescription string       `json:"shipping_description,omitempty"`
}

type OrderPayment struct {
	Method string `json:"method,omitempty"`
	Amount float64 `json:"amount_paid,omitempty"`
}

type OrderExtensionAttributes struct {
	ShippingAssignments []ShippingAssignment `json:"shipping_assignments,omitempty"`
}

type ShippingAssignment struct {
	Shipping ShippingDetail `json:"shipping"`
}

type ShippingDetail struct {
	Address *OrderAddress `json:"address,omitempty"`
	Method  string        `json:"method,omitempty"`
}

// OrderFull represents an order with extension attributes unpacked.
type OrderFull struct {
	EntityID           int                       `json:"entity_id,omitempty"`
	IncrementID        string                    `json:"increment_id"`
	Status             string                    `json:"status"`
	State              string                    `json:"state,omitempty"`
	CreatedAt          string                    `json:"created_at,omitempty"`
	UpdatedAt          string                    `json:"updated_at,omitempty"`
	CustomerEmail      string                    `json:"customer_email,omitempty"`
	CustomerFirstname  string                    `json:"customer_firstname,omitempty"`
	CustomerLastname   string                    `json:"customer_lastname,omitempty"`
	GrandTotal         float64                   `json:"grand_total"`
	ShippingAmount     float64                   `json:"shipping_amount,omitempty"`
	OrderCurrencyCode  string                    `json:"order_currency_code,omitempty"`
	BillingAddress     *OrderAddress             `json:"billing_address,omitempty"`
	ExtensionAttributes *OrderExtensionAttributes `json:"extension_attributes,omitempty"`
	Items              []OrderItem               `json:"items,omitempty"`
	Payment            *OrderPayment             `json:"payment,omitempty"`
	ShippingDescription string                   `json:"shipping_description,omitempty"`
}

func (o *OrderFull) GetShippingAddress() *OrderAddress {
	if o.ExtensionAttributes != nil && len(o.ExtensionAttributes.ShippingAssignments) > 0 {
		return o.ExtensionAttributes.ShippingAssignments[0].Shipping.Address
	}
	return o.BillingAddress
}

type OrderSearchResult struct {
	Items      []OrderFull `json:"items"`
	TotalCount int         `json:"total_count"`
}

// ── Orders ────────────────────────────────────────────────────────────────────

// GetOrders returns orders filtered by creation date and status.
// page is 1-indexed. Pass status="" for all.
func (c *Client) GetOrders(page, pageSize int, createdAfter, createdBefore, status string) (*OrderSearchResult, error) {
	params := url.Values{
		"searchCriteria[pageSize]":    []string{strconv.Itoa(pageSize)},
		"searchCriteria[currentPage]": []string{strconv.Itoa(page)},
		"searchCriteria[sortOrders][0][field]":     []string{"created_at"},
		"searchCriteria[sortOrders][0][direction]": []string{"DESC"},
	}

	filterIdx := 0
	if createdAfter != "" {
		params.Set(fmt.Sprintf("searchCriteria[filterGroups][%d][filters][0][field]", filterIdx), "created_at")
		params.Set(fmt.Sprintf("searchCriteria[filterGroups][%d][filters][0][value]", filterIdx), createdAfter)
		params.Set(fmt.Sprintf("searchCriteria[filterGroups][%d][filters][0][conditionType]", filterIdx), "gteq")
		filterIdx++
	}
	if createdBefore != "" {
		params.Set(fmt.Sprintf("searchCriteria[filterGroups][%d][filters][0][field]", filterIdx), "created_at")
		params.Set(fmt.Sprintf("searchCriteria[filterGroups][%d][filters][0][value]", filterIdx), createdBefore)
		params.Set(fmt.Sprintf("searchCriteria[filterGroups][%d][filters][0][conditionType]", filterIdx), "lteq")
		filterIdx++
	}
	if status != "" {
		params.Set(fmt.Sprintf("searchCriteria[filterGroups][%d][filters][0][field]", filterIdx), "status")
		params.Set(fmt.Sprintf("searchCriteria[filterGroups][%d][filters][0][value]", filterIdx), status)
		params.Set(fmt.Sprintf("searchCriteria[filterGroups][%d][filters][0][conditionType]", filterIdx), "eq")
	}

	var result OrderSearchResult
	if err := c.getJSON("/orders", params, &result); err != nil {
		return nil, fmt.Errorf("GetOrders: %w", err)
	}
	return &result, nil
}

// FetchNewOrders fetches orders created between after and before. Iterates all pages.
func (c *Client) FetchNewOrders(after, before time.Time, status string) ([]OrderFull, error) {
	afterStr := after.UTC().Format("2006-01-02 15:04:05")
	beforeStr := before.UTC().Format("2006-01-02 15:04:05")

	var all []OrderFull
	page := 1
	const pageSize = 50
	for {
		result, err := c.GetOrders(page, pageSize, afterStr, beforeStr, status)
		if err != nil {
			return nil, err
		}
		all = append(all, result.Items...)
		if len(result.Items) < pageSize {
			break
		}
		page++
	}
	return all, nil
}

// ── Shipment Types ────────────────────────────────────────────────────────────

type ShipmentItem struct {
	OrderItemID int     `json:"order_item_id"`
	Qty         float64 `json:"qty"`
}

type ShipmentTrack struct {
	OrderID         int    `json:"order_id,omitempty"`
	Title           string `json:"title"`
	TrackNumber     string `json:"track_number"`
	CarrierCode     string `json:"carrier_code"`
	Description     string `json:"description,omitempty"`
}

type ShipmentRequest struct {
	Items   []ShipmentItem  `json:"items,omitempty"`
	Tracks  []ShipmentTrack `json:"tracks,omitempty"`
	Comment *ShipmentComment `json:"comment,omitempty"`
}

type ShipmentComment struct {
	Comment    string `json:"comment"`
	IsVisible  int    `json:"is_visible_on_front"`
}

// ── Shipments ─────────────────────────────────────────────────────────────────

// CreateShipment creates a shipment for an order and returns the shipment ID.
func (c *Client) CreateShipment(orderID int, req *ShipmentRequest) (int, error) {
	payload := map[string]interface{}{"entity": req}
	var shipmentID int
	if err := c.postJSON(fmt.Sprintf("/order/%d/ship", orderID), payload, &shipmentID); err != nil {
		return 0, fmt.Errorf("CreateShipment orderID=%d: %w", orderID, err)
	}
	log.Printf("[Magento] Created shipment %d for order %d", shipmentID, orderID)
	return shipmentID, nil
}

// AddTrackToShipment adds a tracking number to an existing shipment.
func (c *Client) AddTrackToShipment(shipmentID int, track ShipmentTrack) error {
	payload := map[string]interface{}{"entity": track}
	_, _, err := c.doRequest(http.MethodPost, fmt.Sprintf("/shipment/%d/track", shipmentID), payload, nil)
	if err != nil {
		return fmt.Errorf("AddTrackToShipment shipmentID=%d: %w", shipmentID, err)
	}
	return nil
}

// PushTracking creates a shipment with tracking for a Magento order.
// Finds unshipped items automatically and creates the shipment in one call.
func (c *Client) PushTracking(orderEntityID int, trackingNumber, carrier, carrierTitle string) error {
	// Build shipment request with tracking embedded
	track := ShipmentTrack{
		Title:       carrierTitle,
		TrackNumber: trackingNumber,
		CarrierCode: strings.ToLower(strings.ReplaceAll(carrier, " ", "_")),
	}
	if track.CarrierCode == "" {
		track.CarrierCode = "custom"
	}
	if track.Title == "" {
		track.Title = carrier
	}

	req := &ShipmentRequest{
		Tracks: []ShipmentTrack{track},
	}

	_, err := c.CreateShipment(orderEntityID, req)
	return err
}

// ── Connection Test ───────────────────────────────────────────────────────────

// TestConnection performs a lightweight API call to verify credentials.
func (c *Client) TestConnection() error {
	params := url.Values{
		"searchCriteria[pageSize]":    []string{"1"},
		"searchCriteria[currentPage]": []string{"1"},
	}
	var result ProductSearchResult
	if err := c.getJSON("/products", params, &result); err != nil {
		return fmt.Errorf("Magento connection test failed: %w", err)
	}
	return nil
}

// GetStoreInfo returns basic store information.
func (c *Client) GetStoreInfo() (map[string]interface{}, error) {
	var info map[string]interface{}
	if err := c.getJSON("/store/storeConfigs", nil, &info); err != nil {
		return nil, fmt.Errorf("GetStoreInfo: %w", err)
	}
	return info, nil
}
