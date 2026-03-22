package lazada

// ============================================================================
// LAZADA OPEN PLATFORM API CLIENT
// ============================================================================
// Auth: HMAC-SHA256 signed requests. Each request includes app_key, timestamp,
//       sign, and access_token as query parameters.
// Base URL: https://api.lazada.com.my/rest  (default; varies per country)
// Docs: https://open.lazada.com/apps/doc/doc.htm
//
// Supported regions: MY (Malaysia), SG, TH, ID, PH, VN via country-specific
// base URLs. We default to api.lazada.com.my; sellers supply their own URL.
// ============================================================================

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	AppKey      string
	AppSecret   string
	AccessToken string
	BaseURL     string
	HTTPClient  *http.Client
}

// NewClient creates a Lazada Open Platform client.
// baseURL is the seller's regional endpoint e.g. "https://api.lazada.com.my/rest"
func NewClient(appKey, appSecret, accessToken, baseURL string) *Client {
	if baseURL == "" {
		baseURL = "https://api.lazada.com.my/rest"
	}
	return &Client{
		AppKey:      appKey,
		AppSecret:   appSecret,
		AccessToken: accessToken,
		BaseURL:     baseURL,
		HTTPClient:  &http.Client{Timeout: 30 * time.Second},
	}
}

// ── Signature ─────────────────────────────────────────────────────────────────
// Lazada uses HMAC-SHA256 over: apiPath + sorted(paramKey + paramValue) pairs.

func (c *Client) sign(apiPath string, params map[string]string) string {
	// Sort param keys
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	sb.WriteString(apiPath)
	for _, k := range keys {
		sb.WriteString(k)
		sb.WriteString(params[k])
	}

	mac := hmac.New(sha256.New, []byte(c.AppSecret))
	mac.Write([]byte(sb.String()))
	return strings.ToUpper(hex.EncodeToString(mac.Sum(nil)))
}

func (c *Client) doGet(apiPath string, extraParams map[string]string) ([]byte, error) {
	ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
	params := map[string]string{
		"app_key":      c.AppKey,
		"timestamp":    ts,
		"sign_method":  "sha256",
		"access_token": c.AccessToken,
	}
	for k, v := range extraParams {
		params[k] = v
	}
	params["sign"] = c.sign(apiPath, params)

	qv := url.Values{}
	for k, v := range params {
		qv.Set(k, v)
	}

	req, err := http.NewRequest("GET", c.BaseURL+apiPath+"?"+qv.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json;charset=utf-8")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("lazada API %d: %s", resp.StatusCode, string(body))
	}
	// Lazada embeds errors inside the 200 response
	var chk struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &chk)
	if chk.Code != "" && chk.Code != "0" {
		return nil, fmt.Errorf("lazada API error %s: %s", chk.Code, chk.Message)
	}
	return body, nil
}

// TestConnection verifies credentials by fetching seller info.
func (c *Client) TestConnection() error {
	_, err := c.doGet("/seller/get", nil)
	if err != nil {
		return fmt.Errorf("lazada connection test: %w", err)
	}
	return nil
}

// ── TOKEN REFRESH ─────────────────────────────────────────────────────────────
// Lazada OAuth2 tokens expire after ~30 days. Refresh before expiry.
//
// Auth endpoint (not regional): https://auth.lazada.com/rest
// POST /auth/token/refresh  with grant_type=refresh_token + app_key + sign.

const lazadaAuthURL = "https://auth.lazada.com/rest"

// TokenRefreshResult holds the response from Lazada's token refresh endpoint.
type TokenRefreshResult struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`  // seconds until access_token expires
	Country      string `json:"country"`
	AccountPlatform string `json:"account_platform"`
}

// RefreshAccessToken exchanges a refresh_token for a new access_token + refresh_token pair.
// Call this proactively (e.g. every 20 days) or reactively on 401 responses.
//
// The returned tokens must be persisted back to the credential store immediately.
func (c *Client) RefreshAccessToken(refreshToken string) (*TokenRefreshResult, error) {
	ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
	params := map[string]string{
		"app_key":       c.AppKey,
		"timestamp":     ts,
		"sign_method":   "sha256",
		"grant_type":    "refresh_token",
		"refresh_token": refreshToken,
	}
	// Sign uses the auth path, not the regional API path
	params["sign"] = c.sign("/auth/token/refresh", params)

	qv := url.Values{}
	for k, v := range params {
		qv.Set(k, v)
	}

	resp, err := c.HTTPClient.Post(lazadaAuthURL+"/auth/token/refresh?"+qv.Encode(),
		"application/x-www-form-urlencoded", strings.NewReader(""))
	if err != nil {
		return nil, fmt.Errorf("lazada token refresh request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("lazada token refresh HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Code    string             `json:"code"`
		Message string             `json:"message"`
		Data    TokenRefreshResult `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("lazada token refresh unmarshal: %w", err)
	}
	if result.Code != "" && result.Code != "0" {
		return nil, fmt.Errorf("lazada token refresh error %s: %s", result.Code, result.Message)
	}
	if result.Data.AccessToken == "" {
		return nil, fmt.Errorf("lazada token refresh: empty access_token in response")
	}

	log.Printf("[Lazada] Token refreshed successfully (expires_in=%ds)", result.Data.ExpiresIn)
	return &result.Data, nil
}

// ── ORDERS ────────────────────────────────────────────────────────────────────

type OrdersResult struct {
	Data struct {
		Count  int     `json:"count"`
		Orders []Order `json:"orders"`
	} `json:"data"`
}

type Order struct {
	OrderID         int64         `json:"order_id"`
	CreatedAt       string        `json:"created_at"`      // "2023-01-15 10:30:00"
	UpdatedAt       string        `json:"updated_at"`
	Status          string        `json:"statuses"`        // e.g. "pending", "ready_to_ship", "shipped", "delivered", "canceled"
	Price           float64       `json:"price"`
	GiftMessage     string        `json:"gift_message"`
	Items           []OrderItem   `json:"-"` // fetched separately via GetOrderItems
	AddressShipping LazadaAddress `json:"address_shipping"`
	AddressBilling  LazadaAddress `json:"address_billing"`
}

type LazadaAddress struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Phone     string `json:"phone"`
	Phone2    string `json:"phone2"`
	Address1  string `json:"address1"`
	Address2  string `json:"address2"`
	Address3  string `json:"address3"`
	Address4  string `json:"address4"`
	Address5  string `json:"address5"`
	City      string `json:"city"`
	PostCode  string `json:"post_code"`
	Country   string `json:"country"`
}

type OrderItem struct {
	OrderItemID int64   `json:"order_item_id"`
	OrderID     int64   `json:"order_id"`
	ShopID      string  `json:"shop_id"`
	SellerSKU   string  `json:"sku"`
	ShopSKU     string  `json:"shop_sku"`
	Name        string  `json:"name"`
	Quantity    int     `json:"paid_price"`
	ItemPrice   float64 `json:"item_price"`
	TrackingCode string `json:"tracking_code"`
	Status      string  `json:"status"`
}

// GetOrders fetches orders created between the given times.
func (c *Client) GetOrders(createdAfter, createdBefore time.Time, statuses []string) ([]Order, error) {
	params := map[string]string{
		"created_after":  createdAfter.Format("2006-01-02T15:04:05-07:00"),
		"created_before": createdBefore.Format("2006-01-02T15:04:05-07:00"),
		"limit":          "100",
		"offset":         "0",
	}
	if len(statuses) > 0 {
		params["status"] = strings.Join(statuses, ",")
	}

	body, err := c.doGet("/orders/get", params)
	if err != nil {
		return nil, err
	}
	var result OrdersResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("lazada unmarshal orders: %w", err)
	}
	log.Printf("Lazada: fetched %d orders", result.Data.Count)
	return result.Data.Orders, nil
}

// GetOrderItems fetches line items for a specific order.
func (c *Client) GetOrderItems(orderID int64) ([]OrderItem, error) {
	params := map[string]string{
		"order_id": strconv.FormatInt(orderID, 10),
	}
	body, err := c.doGet("/order/items/get", params)
	if err != nil {
		return nil, err
	}
	var result struct {
		Data []OrderItem `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("lazada unmarshal order items: %w", err)
	}
	return result.Data, nil
}

// ── TRACKING ─────────────────────────────────────────────────────────────────

// SetStatusToPackedByMarketplace marks order items as packed.
// In Lazada's flow: pending → ready_to_ship → shipped.
// SetReadyToShip sets the order items to ready_to_ship with a tracking number.
func (c *Client) SetReadyToShip(orderItemIDs []int64, shippingProvider, trackingNumber string) error {
	ids := make([]string, len(orderItemIDs))
	for i, id := range orderItemIDs {
		ids[i] = strconv.FormatInt(id, 10)
	}
	params := map[string]string{
		"order_item_ids":    "[" + strings.Join(ids, ",") + "]",
		"shipping_provider": shippingProvider,
		"tracking_number":   trackingNumber,
		"delivery_type":     "dropship",
	}
	_, err := c.doGet("/order/rts", params) // Lazada RTS endpoint accepts GET with params
	return err
}

// ── PRODUCTS (LISTINGS) ────────────────────────────────────────────────────────

type ProductsResult struct {
	Data struct {
		TotalProducts int       `json:"total_products"`
		Products      []Product `json:"products"`
	} `json:"data"`
}

type Product struct {
	ItemID      int64       `json:"item_id"`
	SellerSKU   string      `json:"seller_sku"`
	Name        string      `json:"attributes.name"`
	Description string      `json:"description"`
	Status      string      `json:"status"` // "active","inactive","deleted"
	Skus        []LazadaSKU `json:"skus"`
}

type LazadaSKU struct {
	SkuID     string  `json:"SkuId"`
	SellerSKU string  `json:"SellerSku"`
	Price     float64 `json:"price,string"`
	SalePrice float64 `json:"sale_price,string"`
	Quantity  int     `json:"quantity"`
	Images    []struct {
		URL string `json:"Url"`
	} `json:"Images"`
}

// GetProducts fetches all products from the seller's Lazada shop.
func (c *Client) GetProducts(offset, limit int) ([]Product, int, error) {
	params := map[string]string{
		"filter": "all",
		"offset": strconv.Itoa(offset),
		"limit":  strconv.Itoa(limit),
	}
	body, err := c.doGet("/products/get", params)
	if err != nil {
		return nil, 0, err
	}
	var result ProductsResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, 0, fmt.Errorf("lazada unmarshal products: %w", err)
	}
	return result.Data.Products, result.Data.TotalProducts, nil
}

// GetAllProducts paginates through all Lazada products.
func (c *Client) GetAllProducts() ([]Product, error) {
	var all []Product
	limit := 50
	offset := 0
	for {
		products, total, err := c.GetProducts(offset, limit)
		if err != nil {
			return nil, err
		}
		all = append(all, products...)
		offset += limit
		if offset >= total || len(products) == 0 {
			break
		}
	}
	log.Printf("Lazada: fetched %d products", len(all))
	return all, nil
}

// UpdatePrice updates the price of a product SKU.
func (c *Client) UpdatePrice(sellerSKU string, price float64) error {
	payload := map[string]interface{}{
		"Request": map[string]interface{}{
			"Product": map[string]interface{}{
				"Skus": []map[string]interface{}{
					{
						"SellerSku":  sellerSKU,
						"price":      fmt.Sprintf("%.2f", price),
						"sale_price": fmt.Sprintf("%.2f", price),
					},
				},
			},
		},
	}
	ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
	params := map[string]string{
		"app_key":      c.AppKey,
		"timestamp":    ts,
		"sign_method":  "sha256",
		"access_token": c.AccessToken,
	}
	payloadBytes, _ := json.Marshal(payload)
	params["payload"] = string(payloadBytes)
	params["sign"] = c.sign("/product/price_quantity/update", params)

	qv := url.Values{}
	for k, v := range params {
		qv.Set(k, v)
	}
	resp, err := c.HTTPClient.Post(c.BaseURL+"/product/price_quantity/update?"+qv.Encode(),
		"application/json", strings.NewReader(string(payloadBytes)))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// UpdateStock updates the stock quantity of a product SKU.
func (c *Client) UpdateStock(sellerSKU string, quantity int) error {
	payload := map[string]interface{}{
		"Request": map[string]interface{}{
			"Product": map[string]interface{}{
				"Skus": []map[string]interface{}{
					{
						"SellerSku": sellerSKU,
						"quantity":  quantity,
					},
				},
			},
		},
	}
	ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
	params := map[string]string{
		"app_key":      c.AppKey,
		"timestamp":    ts,
		"sign_method":  "sha256",
		"access_token": c.AccessToken,
	}
	payloadBytes, _ := json.Marshal(payload)
	params["payload"] = string(payloadBytes)
	params["sign"] = c.sign("/product/price_quantity/update", params)

	qv := url.Values{}
	for k, v := range params {
		qv.Set(k, v)
	}
	resp, err := c.HTTPClient.Post(c.BaseURL+"/product/price_quantity/update?"+qv.Encode(),
		"application/json", strings.NewReader(string(payloadBytes)))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}
