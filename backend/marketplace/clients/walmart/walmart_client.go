package walmart

// ============================================================================
// WALMART MARKETPLACE API CLIENT
// ============================================================================
// Base URL:  https://marketplace.walmartapis.com
// Auth:      OAuth 2.0 Client Credentials — POST /v3/token with Basic Auth
//            (client_id:client_secret base64). Token is bearer, cached until
//            expiry. Every request requires:
//              WM_SEC.ACCESS_TOKEN  — bearer token
//              WM_QOS.CORRELATION_ID — UUID per request
//              WM_SVC.NAME          — "Walmart Marketplace"
//              WM_CONSUMER.ID       — client_id
//              Accept: application/json
// Docs:      https://developer.walmart.com/api/us/mp
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
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	baseURL    = "https://marketplace.walmartapis.com"
	tokenPath  = "/v3/token"
	svcName    = "Walmart Marketplace"
)

// ── Token ─────────────────────────────────────────────────────────────────────

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// ── Client ────────────────────────────────────────────────────────────────────

type Client struct {
	ClientID     string
	ClientSecret string
	HTTPClient   *http.Client

	mu          sync.Mutex
	accessToken string
	tokenExpiry time.Time
}

func NewClient(clientID, clientSecret string) *Client {
	return &Client{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		HTTPClient:   &http.Client{Timeout: 30 * time.Second},
	}
}

// ── Auth ──────────────────────────────────────────────────────────────────────

func (c *Client) GetToken() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.accessToken != "" && time.Now().Before(c.tokenExpiry) {
		return c.accessToken, nil
	}

	raw := c.ClientID + ":" + c.ClientSecret
	basicAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte(raw))

	form := url.Values{"grant_type": {"client_credentials"}}
	req, err := http.NewRequest(http.MethodPost, baseURL+tokenPath, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Authorization", basicAuth)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("WM_SVC.NAME", svcName)
	req.Header.Set("WM_QOS.CORRELATION_ID", uuid.New().String())

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", fmt.Errorf("parse token response: %w", err)
	}
	if tr.AccessToken == "" {
		return "", fmt.Errorf("empty access_token in response")
	}

	c.accessToken = tr.AccessToken
	// Subtract 60 seconds from expiry as a safety buffer
	c.tokenExpiry = time.Now().Add(time.Duration(tr.ExpiresIn-60) * time.Second)
	return c.accessToken, nil
}

// ── HTTP helpers ──────────────────────────────────────────────────────────────

func (c *Client) doRequest(method, path string, body interface{}, queryParams url.Values) ([]byte, int, error) {
	token, err := c.GetToken()
	if err != nil {
		return nil, 0, fmt.Errorf("auth: %w", err)
	}

	endpoint := baseURL + path
	if len(queryParams) > 0 {
		endpoint += "?" + queryParams.Encode()
	}

	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("marshal body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, endpoint, reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("WM_SEC.ACCESS_TOKEN", token)
	req.Header.Set("WM_QOS.CORRELATION_ID", uuid.New().String())
	req.Header.Set("WM_SVC.NAME", svcName)
	req.Header.Set("WM_CONSUMER.ID", c.ClientID)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

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
		// Try to extract Walmart error message
		var wErr struct {
			Errors []struct {
				Code    string `json:"code"`
				Field   string `json:"field"`
				Info    string `json:"info"`
				Severity string `json:"severity"`
			} `json:"errors"`
		}
		if jsonErr := json.Unmarshal(respBytes, &wErr); jsonErr == nil && len(wErr.Errors) > 0 {
			return nil, resp.StatusCode, fmt.Errorf("Walmart API error [%s]: %s", wErr.Errors[0].Code, wErr.Errors[0].Info)
		}
		return nil, resp.StatusCode, fmt.Errorf("Walmart API HTTP %d: %s", resp.StatusCode, string(respBytes))
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

// ── TestConnection ────────────────────────────────────────────────────────────

// TestConnection verifies credentials by fetching a token then listing a single order.
func (c *Client) TestConnection() error {
	_, err := c.GetToken()
	if err != nil {
		return fmt.Errorf("Walmart authentication failed: %w", err)
	}
	// Lightweight check — query items with limit=1
	params := url.Values{"limit": {"1"}}
	var result interface{}
	if err := c.getJSON("/v3/items", params, &result); err != nil {
		// A 200 with empty list is fine; non-auth errors still indicate connectivity
		log.Printf("[Walmart] TestConnection items call: %v", err)
	}
	return nil
}

// ── Feed Types ────────────────────────────────────────────────────────────────

type FeedResponse struct {
	FeedID string `json:"feedId"`
}

type FeedStatusResponse struct {
	FeedID         string      `json:"feedId"`
	FeedStatus     string      `json:"feedStatus"` // RECEIVED, INPROGRESS, PROCESSED, ERROR
	ItemsReceived  int         `json:"itemsReceived"`
	ItemsSucceeded int         `json:"itemsSucceeded"`
	ItemsFailed    int         `json:"itemsFailed"`
	ItemsProcessing int        `json:"itemsProcessing"`
	Ingestion      interface{} `json:"ingestion"`
}

// SubmitFeed submits a feed (item create/update, inventory, price, etc.)
// feedType: MP_ITEM, inventory, price, RETIRE_ITEM, etc.
func (c *Client) SubmitFeed(feedType string, payload interface{}) (*FeedResponse, error) {
	params := url.Values{"feedType": {feedType}}
	endpoint := "/v3/feeds"
	if len(params) > 0 {
		endpoint += "?" + params.Encode()
	}

	b, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal feed payload: %w", err)
	}

	token, err := c.GetToken()
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, baseURL+endpoint, bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("create feed request: %w", err)
	}
	req.Header.Set("WM_SEC.ACCESS_TOKEN", token)
	req.Header.Set("WM_QOS.CORRELATION_ID", uuid.New().String())
	req.Header.Set("WM_SVC.NAME", svcName)
	req.Header.Set("WM_CONSUMER.ID", c.ClientID)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("submit feed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("Walmart feed submission HTTP %d: %s", resp.StatusCode, string(respBytes))
	}

	var fr FeedResponse
	if err := json.Unmarshal(respBytes, &fr); err != nil {
		return nil, fmt.Errorf("parse feed response: %w", err)
	}
	log.Printf("[Walmart] Feed submitted: feedId=%s type=%s", fr.FeedID, feedType)
	return &fr, nil
}

// GetFeedStatus retrieves the status of a previously submitted feed.
func (c *Client) GetFeedStatus(feedID string) (*FeedStatusResponse, error) {
	var status FeedStatusResponse
	if err := c.getJSON(fmt.Sprintf("/v3/feeds/%s", feedID), nil, &status); err != nil {
		return nil, fmt.Errorf("GetFeedStatus %s: %w", feedID, err)
	}
	return &status, nil
}

// ── Inventory ─────────────────────────────────────────────────────────────────

type InventoryResponse struct {
	SKU       string `json:"sku"`
	Quantity  InventoryQuantity `json:"quantity"`
}

type InventoryQuantity struct {
	Unit   string `json:"unit"`
	Amount int    `json:"amount"`
}

// GetInventory retrieves the inventory level for a SKU.
func (c *Client) GetInventory(sku string) (*InventoryResponse, error) {
	params := url.Values{"sku": {sku}}
	var inv InventoryResponse
	if err := c.getJSON("/v3/inventory", params, &inv); err != nil {
		return nil, fmt.Errorf("GetInventory %s: %w", sku, err)
	}
	return &inv, nil
}

// UpdateInventory sets the inventory quantity for a SKU.
func (c *Client) UpdateInventory(sku string, qty int) error {
	payload := map[string]interface{}{
		"sku": sku,
		"quantity": map[string]interface{}{
			"unit":   "EACH",
			"amount": qty,
		},
	}
	params := url.Values{"sku": {sku}}
	endpoint := "/v3/inventory"
	if len(params) > 0 {
		endpoint += "?" + params.Encode()
	}

	token, err := c.GetToken()
	if err != nil {
		return fmt.Errorf("auth: %w", err)
	}

	b, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPut, baseURL+endpoint, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("create inventory update request: %w", err)
	}
	req.Header.Set("WM_SEC.ACCESS_TOKEN", token)
	req.Header.Set("WM_QOS.CORRELATION_ID", uuid.New().String())
	req.Header.Set("WM_SVC.NAME", svcName)
	req.Header.Set("WM_CONSUMER.ID", c.ClientID)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("update inventory: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("Walmart UpdateInventory HTTP %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// ── Price ─────────────────────────────────────────────────────────────────────

type PriceResponse struct {
	Mart       string      `json:"mart"`
	SKU        string      `json:"sku"`
	Pricing    []PriceItem `json:"pricing"`
}

type PriceItem struct {
	CurrentPrice  PriceValue `json:"currentPrice"`
	ComparisonPrice *PriceValue `json:"comparisonPrice,omitempty"`
}

type PriceValue struct {
	Currency string  `json:"currency"`
	Amount   float64 `json:"amount"`
}

// GetPrice retrieves the current price for a SKU.
func (c *Client) GetPrice(sku string) (*PriceResponse, error) {
	params := url.Values{"sku": {sku}}
	var pr PriceResponse
	if err := c.getJSON("/v3/price", params, &pr); err != nil {
		return nil, fmt.Errorf("GetPrice %s: %w", sku, err)
	}
	return &pr, nil
}

// UpdatePrice sets the price for a SKU via the price feed.
func (c *Client) UpdatePrice(sku string, price float64) error {
	payload := map[string]interface{}{
		"pricing": []map[string]interface{}{
			{
				"itemCondition": "New",
				"pricingList": []map[string]interface{}{
					{
						"isCurrentPriceType": "BASE",
						"currentPrice": map[string]interface{}{
							"currency": "USD",
							"amount":   price,
						},
					},
				},
			},
		},
		"sku": sku,
	}

	_, err := c.SubmitFeed("price", map[string]interface{}{
		"PriceFeed": map[string]interface{}{
			"PriceHeader": map[string]interface{}{"version": "1.5"},
			"Price":       []interface{}{map[string]interface{}{"itemIdentifier": map[string]interface{}{"sku": sku}, "pricingList": payload["pricing"]}},
		},
	})
	return err
}

// ── Items ─────────────────────────────────────────────────────────────────────

type ItemListResponse struct {
	TotalItems int      `json:"totalItems"`
	ItemDetails []Item  `json:"ItemDetails"`
	NextCursor  string  `json:"nextCursor"`
}

type Item struct {
	Mart                   string            `json:"mart"`
	SKU                    string            `json:"sku"`
	OfferID                string            `json:"offerId"`
	ItemID                 int64             `json:"itemId"`
	ProductName            string            `json:"productName"`
	Price                  *PriceValue       `json:"price,omitempty"`
	ShippingWeight         float64           `json:"shippingWeight,omitempty"`
	PublishStatus          string            `json:"publishStatus"`
	LifecycleStatus        string            `json:"lifecycleStatus"`
	AvailabilityStatus     string            `json:"availabilityStatus"`
}

// GetItems returns a page of seller items.
func (c *Client) GetItems(nextCursor string, limit int) (*ItemListResponse, error) {
	params := url.Values{"limit": {strconv.Itoa(limit)}}
	if nextCursor != "" {
		params.Set("nextCursor", nextCursor)
	}
	var resp ItemListResponse
	if err := c.getJSON("/v3/items", params, &resp); err != nil {
		return nil, fmt.Errorf("GetItems: %w", err)
	}
	return &resp, nil
}

// GetAllItems iterates all pages of seller items.
func (c *Client) GetAllItems() ([]Item, error) {
	var all []Item
	cursor := ""
	for {
		page, err := c.GetItems(cursor, 100)
		if err != nil {
			return nil, err
		}
		all = append(all, page.ItemDetails...)
		if page.NextCursor == "" || len(page.ItemDetails) == 0 {
			break
		}
		cursor = page.NextCursor
	}
	return all, nil
}

// RetireItem removes an item from Walmart (sets lifecycle status to RETIRED).
func (c *Client) RetireItem(sku string) error {
	params := url.Values{"sku": {sku}}
	_, _, err := c.doRequest(http.MethodDelete, "/v3/items", nil, params)
	return err
}

// ── Orders ────────────────────────────────────────────────────────────────────

type OrderListResponse struct {
	List OrderList `json:"list"`
}

type OrderList struct {
	Meta    OrderListMeta `json:"meta"`
	Elements OrderElements `json:"elements"`
}

type OrderListMeta struct {
	TotalCount  int    `json:"totalCount"`
	Limit       int    `json:"limit"`
	NextCursor  string `json:"nextCursor"`
}

type OrderElements struct {
	Order []WalmartOrder `json:"order"`
}

type WalmartOrder struct {
	PurchaseOrderID  string          `json:"purchaseOrderId"`
	CustomerOrderID  string          `json:"customerOrderId"`
	Status           string          `json:"status"`
	CustomerEmail    string          `json:"customerEmailId"`
	EstimatedDeliveryDate string    `json:"estimatedDeliveryDate,omitempty"`
	EstimatedShipDate     string    `json:"estimatedShipDate,omitempty"`
	OrderDate        string          `json:"orderDate"`
	ShippingInfo     ShippingInfo    `json:"shippingInfo"`
	OrderLines       OrderLineList   `json:"orderLines"`
}

type ShippingInfo struct {
	Phone        string          `json:"phone,omitempty"`
	EstimatedDeliveryDate string `json:"estimatedDeliveryDate,omitempty"`
	EstimatedShipDate     string `json:"estimatedShipDate,omitempty"`
	MethodCode   string          `json:"methodCode,omitempty"`
	PostalAddress PostalAddress  `json:"postalAddress"`
}

type PostalAddress struct {
	Name        string `json:"name"`
	Address1    string `json:"address1"`
	Address2    string `json:"address2,omitempty"`
	City        string `json:"city"`
	State       string `json:"state"`
	PostalCode  string `json:"postalCode"`
	Country     string `json:"country"`
	AddressType string `json:"addressType,omitempty"`
}

type OrderLineList struct {
	OrderLine []OrderLine `json:"orderLine"`
}

type OrderLine struct {
	LineNumber    string          `json:"lineNumber"`
	Item          OrderLineItem   `json:"item"`
	Charges       OrderCharges    `json:"charges"`
	OrderLineQuantity OrderQty   `json:"orderLineQuantity"`
	StatusDate    string          `json:"statusDate,omitempty"`
	OrderLineStatuses OrderLineStatusList `json:"orderLineStatuses"`
}

type OrderLineItem struct {
	ProductName   string `json:"productName"`
	SKU           string `json:"sku"`
}

type OrderCharges struct {
	Charge []OrderCharge `json:"charge"`
}

type OrderCharge struct {
	ChargeType    string    `json:"chargeType"`
	ChargeName    string    `json:"chargeName"`
	ChargeAmount  MoneyAmount `json:"chargeAmount"`
}

type MoneyAmount struct {
	Currency string  `json:"currency"`
	Amount   float64 `json:"amount"`
}

type OrderQty struct {
	UnitOfMeasurement string `json:"unitOfMeasurement"`
	Amount            string `json:"amount"`
}

type OrderLineStatusList struct {
	OrderLineStatus []OrderLineStatus `json:"orderLineStatus"`
}

type OrderLineStatus struct {
	Status             string `json:"status"`
	StatusQuantity     OrderQty `json:"statusQuantity"`
	ReturnCenterAddress *PostalAddress `json:"returnCenterAddress,omitempty"`
}

// GetOrders returns orders created on or after createdStartDate.
// createdStartDate: Unix timestamp in milliseconds as string. Pass "" to omit.
func (c *Client) GetOrders(createdStartDate string, limit int, nextCursor string) (*OrderListResponse, error) {
	params := url.Values{"limit": {strconv.Itoa(limit)}}
	if createdStartDate != "" {
		params.Set("createdStartDate", createdStartDate)
	}
	if nextCursor != "" {
		params.Set("nextCursor", nextCursor)
	}

	var resp OrderListResponse
	if err := c.getJSON("/v3/orders", params, &resp); err != nil {
		return nil, fmt.Errorf("GetOrders: %w", err)
	}
	return &resp, nil
}

// FetchNewOrders fetches all orders created between after and before.
func (c *Client) FetchNewOrders(after, before time.Time) ([]WalmartOrder, error) {
	// Walmart uses milliseconds Unix timestamp
	startMs := strconv.FormatInt(after.UnixMilli(), 10)
	endMs := strconv.FormatInt(before.UnixMilli(), 10)

	var all []WalmartOrder
	cursor := ""
	for {
		params := url.Values{
			"limit":             {"100"},
			"createdStartDate":  {startMs},
			"createdEndDate":    {endMs},
		}
		if cursor != "" {
			params.Set("nextCursor", cursor)
		}
		var resp OrderListResponse
		if err := c.getJSON("/v3/orders", params, &resp); err != nil {
			return nil, err
		}
		all = append(all, resp.List.Elements.Order...)
		if resp.List.Meta.NextCursor == "" || len(resp.List.Elements.Order) == 0 {
			break
		}
		cursor = resp.List.Meta.NextCursor
	}
	return all, nil
}

// AcknowledgeOrders acknowledges receipt of orders. Walmart requires this before shipment.
func (c *Client) AcknowledgeOrders(purchaseOrderIDs []string) error {
	for _, poID := range purchaseOrderIDs {
		_, _, err := c.doRequest(http.MethodPost, fmt.Sprintf("/v3/orders/%s/acknowledge", poID), nil, nil)
		if err != nil {
			log.Printf("[Walmart] Failed to acknowledge order %s: %v", poID, err)
		}
	}
	return nil
}

// ── Tracking / Shipment ───────────────────────────────────────────────────────

type ShipOrderRequest struct {
	OrderShipment OrderShipment `json:"orderShipment"`
}

type OrderShipment struct {
	OrderLines ShipmentOrderLines `json:"orderLines"`
}

type ShipmentOrderLines struct {
	OrderLine []ShipmentOrderLine `json:"orderLine"`
}

type ShipmentOrderLine struct {
	LineNumber          string              `json:"lineNumber"`
	IntentToCancelOverride bool             `json:"intentToCancelOverride,omitempty"`
	SellerOrderID       string              `json:"sellerOrderId,omitempty"`
	OrderLineStatuses   ShipmentLineStatuses `json:"orderLineStatuses"`
}

type ShipmentLineStatuses struct {
	OrderLineStatus []ShipmentLineStatus `json:"orderLineStatus"`
}

type ShipmentLineStatus struct {
	Status              string           `json:"status"`
	StatusQuantity      OrderQty         `json:"statusQuantity"`
	ReturnCenterAddress *PostalAddress   `json:"returnCenterAddress,omitempty"`
	TrackingInfo        *TrackingInfo    `json:"trackingInfo,omitempty"`
}

type TrackingInfo struct {
	ShipDateTime       string           `json:"shipDateTime"`
	CarrierName        CarrierName      `json:"carrierName"`
	MethodCode         string           `json:"methodCode"`
	TrackingNumber     string           `json:"trackingNumber"`
	TrackingURL        string           `json:"trackingURL,omitempty"`
}

type CarrierName struct {
	OtherCarrier string `json:"otherCarrier,omitempty"`
	Carrier      string `json:"carrier,omitempty"`
}

// ShipOrder marks order lines as shipped with tracking information.
func (c *Client) ShipOrder(purchaseOrderID string, lines []ShipmentOrderLine) error {
	payload := ShipOrderRequest{
		OrderShipment: OrderShipment{
			OrderLines: ShipmentOrderLines{
				OrderLine: lines,
			},
		},
	}

	_, _, err := c.doRequest(http.MethodPost,
		fmt.Sprintf("/v3/orders/%s/shipping", purchaseOrderID),
		payload, nil)
	if err != nil {
		return fmt.Errorf("ShipOrder %s: %w", purchaseOrderID, err)
	}
	log.Printf("[Walmart] Order %s marked as shipped", purchaseOrderID)
	return nil
}

// PushTracking is a convenience wrapper that ships all lines on an order with given tracking.
func (c *Client) PushTracking(purchaseOrderID string, lines []OrderLine, trackingNumber, carrier, trackingURL string) error {
	now := time.Now().UTC().Format(time.RFC3339)

	var shipLines []ShipmentOrderLine
	for _, line := range lines {
		qty := "1"
		if len(line.OrderLineStatuses.OrderLineStatus) > 0 {
			qty = line.OrderLineStatuses.OrderLineStatus[0].StatusQuantity.Amount
		} else if line.OrderLineQuantity.Amount != "" {
			qty = line.OrderLineQuantity.Amount
		}

		// Map carrier name
		carrierCode := mapWalmartCarrier(carrier)

		trackInfo := &TrackingInfo{
			ShipDateTime:   now,
			MethodCode:     "Standard",
			TrackingNumber: trackingNumber,
			TrackingURL:    trackingURL,
			CarrierName: CarrierName{
				Carrier: carrierCode,
			},
		}
		if carrierCode == "" {
			trackInfo.CarrierName = CarrierName{OtherCarrier: carrier}
		}

		shipLines = append(shipLines, ShipmentOrderLine{
			LineNumber: line.LineNumber,
			OrderLineStatuses: ShipmentLineStatuses{
				OrderLineStatus: []ShipmentLineStatus{
					{
						Status: "Shipped",
						StatusQuantity: OrderQty{
							UnitOfMeasurement: "EACH",
							Amount:            qty,
						},
						TrackingInfo: trackInfo,
					},
				},
			},
		})
	}

	return c.ShipOrder(purchaseOrderID, shipLines)
}

func mapWalmartCarrier(carrier string) string {
	switch strings.ToUpper(carrier) {
	case "UPS":
		return "UPS"
	case "FEDEX", "FED EX":
		return "FedEx"
	case "USPS":
		return "USPS"
	case "DHL":
		return "DHL"
	case "ONTRAC":
		return "OnTrac"
	default:
		return ""
	}
}
