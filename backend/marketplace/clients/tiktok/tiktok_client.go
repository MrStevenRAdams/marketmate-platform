package tiktok

// ============================================================================
// TIKTOK SHOP OPEN API CLIENT
// ============================================================================
// Base URL:  https://open-api.tiktokglobalshop.com
// Auth:      OAuth 2.0 — app_key + app_secret generate access_token via code exchange.
//            Every request is signed with HMAC-SHA256:
//              sign = HMAC-SHA256(app_secret, app_key + sorted_params_string + body + timestamp)
// Docs:      https://partner.tiktokshop.com/docv2/page/6502edd33d5f7402b9f8c4f0
// ============================================================================

import (
	"bytes"
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
	"strings"
	"time"
)

const (
	BaseURL       = "https://open-api.tiktokglobalshop.com"
	AuthURL       = "https://auth.tiktok-shops.com"
	APIVersion    = "202309"
)

// ── Client ────────────────────────────────────────────────────────────────────

type Client struct {
	AppKey      string
	AppSecret   string
	AccessToken string
	ShopID      string
	ShopCipher  string // some endpoints need cipher instead of shop_id
	HTTPClient  *http.Client

	// OnTokenRefresh is called when the access token is refreshed.
	// The caller should persist the new tokens to storage.
	OnTokenRefresh func(accessToken, refreshToken string, expiresIn int)
}

func NewClient(appKey, appSecret, accessToken, shopID string) *Client {
	return &Client{
		AppKey:      appKey,
		AppSecret:   appSecret,
		AccessToken: accessToken,
		ShopID:      shopID,
		HTTPClient:  &http.Client{Timeout: 30 * time.Second},
	}
}

// ── Request signing ───────────────────────────────────────────────────────────

// sign produces the HMAC-SHA256 signature required by TikTok Shop API.
// Algorithm:  HMAC-SHA256(app_secret,  app_key + path + sorted_query_params + body_string + timestamp)
// Reference:  https://partner.tiktokshop.com/docv2/page/6502edd33d5f7402b9f8c4f0
func (c *Client) sign(path string, queryParams map[string]string, body string, timestamp int64) string {
	// Sort query params and build param string (exclude sign, access_token)
	var keys []string
	for k := range queryParams {
		if k != "sign" && k != "access_token" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(k)
		sb.WriteString(queryParams[k])
	}

	// Final string: app_key + path + sorted_param_kv + body + timestamp
	message := c.AppKey + path + sb.String() + body + fmt.Sprintf("%d", timestamp)

	mac := hmac.New(sha256.New, []byte(c.AppSecret))
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}

// ── HTTP helpers ──────────────────────────────────────────────────────────────

type apiResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
	// Some endpoints wrap under request_id at top level
	RequestID string `json:"request_id"`
}

func (c *Client) get(path string, extraParams map[string]string) (json.RawMessage, error) {
	return c.request("GET", path, extraParams, nil)
}

func (c *Client) post(path string, extraParams map[string]string, body interface{}) (json.RawMessage, error) {
	return c.request("POST", path, extraParams, body)
}

func (c *Client) request(method, path string, extraParams map[string]string, body interface{}) (json.RawMessage, error) {
	timestamp := time.Now().Unix()

	// Base query params
	params := map[string]string{
		"app_key":   c.AppKey,
		"timestamp": fmt.Sprintf("%d", timestamp),
		"version":   APIVersion,
	}
	if c.ShopID != "" {
		params["shop_id"] = c.ShopID
	}
	if c.ShopCipher != "" {
		params["shop_cipher"] = c.ShopCipher
	}
	for k, v := range extraParams {
		params[k] = v
	}

	// Marshal body
	var bodyStr string
	var bodyBytes []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		bodyBytes = b
		bodyStr = string(b)
	}

	// Sign
	params["sign"] = c.sign(path, params, bodyStr, timestamp)
	params["access_token"] = c.AccessToken

	// Build URL
	u, _ := url.Parse(BaseURL + path)
	q := u.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()

	// Build request
	var req *http.Request
	var err error
	if body != nil {
		req, err = http.NewRequest(method, u.String(), bytes.NewReader(bodyBytes))
	} else {
		req, err = http.NewRequest(method, u.String(), nil)
	}
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-tts-access-token", c.AccessToken)

	// Debug log (mask sensitive values)
	log.Printf("[TikTok] %s %s", method, path)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	log.Printf("[TikTok] Response %d: %.400s", resp.StatusCode, string(respBytes))

	var apiResp apiResponse
	if err := json.Unmarshal(respBytes, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w (body: %.200s)", err, string(respBytes))
	}

	if apiResp.Code != 0 {
		return nil, fmt.Errorf("TikTok API error %d: %s", apiResp.Code, apiResp.Message)
	}

	return apiResp.Data, nil
}

// ── OAuth ─────────────────────────────────────────────────────────────────────

// GenerateOAuthURL produces the consent page URL for TikTok Shop OAuth.
func (c *Client) GenerateOAuthURL(redirectURI, state string) string {
	params := url.Values{}
	params.Set("app_key", c.AppKey)
	params.Set("redirect_uri", redirectURI)
	params.Set("state", state)
	return AuthURL + "/oauth/authorize?" + params.Encode()
}

// TokenResponse is returned by the code exchange and refresh endpoints.
type TokenResponse struct {
	AccessToken           string `json:"access_token"`
	AccessTokenExpireIn   int    `json:"access_token_expire_in"`
	RefreshToken          string `json:"refresh_token"`
	RefreshTokenExpireIn  int    `json:"refresh_token_expire_in"`
	OpenID                string `json:"open_id"`
	SellerName            string `json:"seller_name"`
	SellerBaseRegion      string `json:"seller_base_region"`
}

// ExchangeCodeForToken exchanges an OAuth authorization code for tokens.
func (c *Client) ExchangeCodeForToken(code string) (*TokenResponse, error) {
	body := map[string]string{
		"app_key":    c.AppKey,
		"app_secret": c.AppSecret,
		"auth_code":  code,
		"grant_type": "authorized_code",
	}

	bodyBytes, _ := json.Marshal(body)
	resp, err := c.HTTPClient.Post(
		BaseURL+"/api/v2/token/get",
		"application/json",
		bytes.NewReader(bodyBytes),
	)
	if err != nil {
		return nil, fmt.Errorf("token exchange: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Code    int           `json:"code"`
		Message string        `json:"message"`
		Data    TokenResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	if result.Code != 0 {
		return nil, fmt.Errorf("TikTok token exchange error %d: %s", result.Code, result.Message)
	}
	return &result.Data, nil
}

// RefreshToken refreshes an expired access token using the refresh token.
func (c *Client) RefreshToken(refreshToken string) (*TokenResponse, error) {
	body := map[string]string{
		"app_key":       c.AppKey,
		"app_secret":    c.AppSecret,
		"refresh_token": refreshToken,
		"grant_type":    "refresh_token",
	}

	bodyBytes, _ := json.Marshal(body)
	resp, err := c.HTTPClient.Post(
		BaseURL+"/api/v2/token/refresh",
		"application/json",
		bytes.NewReader(bodyBytes),
	)
	if err != nil {
		return nil, fmt.Errorf("token refresh: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Code    int           `json:"code"`
		Message string        `json:"message"`
		Data    TokenResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode refresh response: %w", err)
	}
	if result.Code != 0 {
		return nil, fmt.Errorf("TikTok token refresh error %d: %s", result.Code, result.Message)
	}

	tok := result.Data
	if c.OnTokenRefresh != nil {
		c.OnTokenRefresh(tok.AccessToken, tok.RefreshToken, tok.AccessTokenExpireIn)
	}
	return &tok, nil
}

// ── Shop ──────────────────────────────────────────────────────────────────────

type Shop struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Region string `json:"region"`
	Type   int    `json:"type"` // 1=local, 2=cross-border
	Cipher string `json:"cipher"`
}

// GetAuthorizedShops returns all shops the seller has authorized.
func (c *Client) GetAuthorizedShops() ([]Shop, error) {
	data, err := c.get("/api/v2/seller/global/shops", nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		Shops []Shop `json:"shops"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("unmarshal shops: %w", err)
	}
	return result.Shops, nil
}

// ── Categories ────────────────────────────────────────────────────────────────

type Category struct {
	ID            int64      `json:"id"`
	ParentID      int64      `json:"parent_id"`
	LocalName     string     `json:"local_name"`
	IsLeaf        bool       `json:"is_leaf"`
	PermissionStatuses []struct {
		Status string `json:"status"`
	} `json:"permission_statuses"`
}

// GetCategories fetches the category tree. Pass parentID=0 for root.
func (c *Client) GetCategories() ([]Category, error) {
	data, err := c.get("/api/v2/product/categories", map[string]string{"locale": "en-GB"})
	if err != nil {
		return nil, err
	}

	var result struct {
		CategoryList []Category `json:"category_list"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("unmarshal categories: %w", err)
	}
	return result.CategoryList, nil
}

// ── Brands ────────────────────────────────────────────────────────────────────

type Brand struct {
	ID          string `json:"id"`
	Name        string `json:"brand_name"`
	Status      string `json:"status"` // AVAILABLE, PENDING, etc.
	IsT1Brand   bool   `json:"is_t1_brand"`
}

// GetBrands returns all brands available to the seller.
func (c *Client) GetBrands(pageToken string) ([]Brand, string, error) {
	params := map[string]string{"page_size": "100"}
	if pageToken != "" {
		params["page_token"] = pageToken
	}
	data, err := c.get("/api/v2/product/brands", params)
	if err != nil {
		return nil, "", err
	}

	var result struct {
		Brands         []Brand `json:"brands"`
		NextPageToken  string  `json:"next_page_token"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, "", fmt.Errorf("unmarshal brands: %w", err)
	}
	return result.Brands, result.NextPageToken, nil
}

// GetAllBrands fetches all brands across pages.
func (c *Client) GetAllBrands() ([]Brand, error) {
	var all []Brand
	pageToken := ""
	for {
		brands, next, err := c.GetBrands(pageToken)
		if err != nil {
			return all, err // return what we have
		}
		all = append(all, brands...)
		if next == "" {
			break
		}
		pageToken = next
	}
	return all, nil
}

// ── Attributes ────────────────────────────────────────────────────────────────

type CategoryAttribute struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Type         int    `json:"type"` // 1=single select, 2=multi-select, 3=text
	IsMandatory  bool   `json:"is_mandatory"`
	IsSku        bool   `json:"is_sku"`
	Values       []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"values"`
	InputType string `json:"input_type"` // DROPDOWN, TEXT, etc.
}

// GetCategoryAttributes fetches required and optional attributes for a leaf category.
func (c *Client) GetCategoryAttributes(categoryID int64) ([]CategoryAttribute, error) {
	data, err := c.get("/api/v2/product/category_attributes", map[string]string{
		"category_id": fmt.Sprintf("%d", categoryID),
		"locale":      "en-GB",
	})
	if err != nil {
		return nil, err
	}

	var result struct {
		Attributes []CategoryAttribute `json:"attributes"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("unmarshal attributes: %w", err)
	}
	return result.Attributes, nil
}

// ── Shipping Templates ────────────────────────────────────────────────────────

type ShippingTemplate struct {
	ID   string `json:"template_id"`
	Name string `json:"name"`
}

// GetShippingTemplates returns the seller's shipping templates.
func (c *Client) GetShippingTemplates() ([]ShippingTemplate, error) {
	data, err := c.get("/api/v2/logistics/ship/ship_services", nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		ShippingTemplates []ShippingTemplate `json:"shipping_templates"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		// TikTok sometimes returns these under a different key
		var alt struct {
			Templates []ShippingTemplate `json:"templates"`
		}
		if err2 := json.Unmarshal(data, &alt); err2 == nil && len(alt.Templates) > 0 {
			return alt.Templates, nil
		}
		return nil, fmt.Errorf("unmarshal shipping templates: %w", err)
	}
	return result.ShippingTemplates, nil
}

// ── Products ──────────────────────────────────────────────────────────────────

// ProductImage for TikTok product images (uploaded separately via UploadImage).
type ProductImage struct {
	URI    string `json:"uri"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

// ProductSKU represents one variant of a TikTok product.
type ProductSKU struct {
	ID          string `json:"id,omitempty"` // empty on create
	OuterID     string `json:"seller_sku"`   // seller's own SKU
	SaleAttributes []struct {
		AttributeID    int64  `json:"attribute_id"`
		AttributeValue struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"attribute_value"`
		SkuImage ProductImage `json:"sku_img,omitempty"`
	} `json:"sales_attributes,omitempty"`
	Price struct {
		Currency         string `json:"currency"`
		OriginalPrice    string `json:"original_price"`
	} `json:"price"`
	Inventory []struct {
		Quantity    int    `json:"quantity"`
		WarehouseID string `json:"warehouse_id"`
	} `json:"inventory"`
}

// CreateProductRequest is the full payload for creating/updating a TikTok product.
type CreateProductRequest struct {
	Title          string         `json:"title"`
	Description    string         `json:"description"`
	CategoryID     int64          `json:"category_id"`
	BrandID        string         `json:"brand_id,omitempty"`
	MainImages     []ProductImage `json:"main_images"`
	Video          *struct {
		VideoID string `json:"id"`
	} `json:"video,omitempty"`
	SKUs           []ProductSKU   `json:"skus"`
	PackageWeight  struct {
		Unit  string  `json:"unit"` // KILOGRAM, POUND
		Value float64 `json:"value,string"`
	} `json:"package_weight,omitempty"`
	PackageDimensions struct {
		Height string `json:"height,omitempty"`
		Length string `json:"length,omitempty"`
		Width  string `json:"width,omitempty"`
		Unit   string `json:"unit,omitempty"` // CENTIMETER, INCH
	} `json:"package_dimensions,omitempty"`
	CertificationImages []ProductImage `json:"certification_images,omitempty"`
	Certifications      []struct {
		ID    string `json:"id"`
		Files []struct {
			Type string `json:"type"`
			URL  string `json:"url"`
		} `json:"files,omitempty"`
	} `json:"certifications,omitempty"`
	IsCOD            bool  `json:"is_cod_open,omitempty"`
	DeliveryServices []struct {
		ID string `json:"id"`
	} `json:"delivery_services,omitempty"`
	ShippingTemplateID string `json:"shipping_template_id,omitempty"`
	ProductAttributes  []struct {
		ID     int64 `json:"id"`
		Values []struct {
			ID   int64  `json:"id,omitempty"`
			Name string `json:"name"`
		} `json:"values"`
	} `json:"product_attributes,omitempty"`
}

type ProductResponse struct {
	ProductID string `json:"product_id"`
	SkuList   []struct {
		ID      string `json:"id"`
		SellerSKU string `json:"seller_sku"`
	} `json:"sku_list,omitempty"`
}

// CreateProduct creates a new product listing on TikTok Shop.
func (c *Client) CreateProduct(req *CreateProductRequest) (*ProductResponse, error) {
	data, err := c.post("/api/v2/product/products", nil, req)
	if err != nil {
		return nil, err
	}

	var result ProductResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("unmarshal create product response: %w", err)
	}
	return &result, nil
}

// UpdateProduct updates an existing product. productID must be set.
func (c *Client) UpdateProduct(productID string, req *CreateProductRequest) (*ProductResponse, error) {
	data, err := c.put("/api/v2/product/products/"+productID, nil, req)
	if err != nil {
		return nil, err
	}
	var result ProductResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("unmarshal update product response: %w", err)
	}
	return &result, nil
}

// DeleteProduct deletes a product from TikTok Shop.
func (c *Client) DeleteProduct(productIDs []string) error {
	body := map[string][]string{"product_ids": productIDs}
	_, err := c.delete("/api/v2/product/products", nil, body)
	return err
}

// GetProduct fetches a single product by ID.
func (c *Client) GetProduct(productID string) (map[string]interface{}, error) {
	data, err := c.get("/api/v2/product/products/"+productID, nil)
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	json.Unmarshal(data, &result)
	return result, nil
}

// GetProducts lists all products for the shop with pagination.
func (c *Client) GetProducts(pageToken string, pageSize int) ([]map[string]interface{}, string, int, error) {
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 100
	}
	params := map[string]string{
		"page_size": fmt.Sprintf("%d", pageSize),
	}
	if pageToken != "" {
		params["page_token"] = pageToken
	}

	data, err := c.post("/api/v2/product/products/search", params, map[string]interface{}{})
	if err != nil {
		return nil, "", 0, err
	}

	var result struct {
		Products      []map[string]interface{} `json:"products"`
		NextPageToken string                   `json:"next_page_token"`
		Total         int                      `json:"total_count"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, "", 0, fmt.Errorf("unmarshal products: %w", err)
	}
	return result.Products, result.NextPageToken, result.Total, nil
}

// ── Image Upload ──────────────────────────────────────────────────────────────

// UploadImageFromURL uploads an image to TikTok from a publicly accessible URL.
func (c *Client) UploadImageFromURL(imageURL string) (*ProductImage, error) {
	body := map[string]string{"url": imageURL}
	data, err := c.post("/api/v2/product/images/upload", nil, body)
	if err != nil {
		return nil, err
	}

	var result struct {
		URI    string `json:"uri"`
		Height int    `json:"height"`
		Width  int    `json:"width"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("unmarshal upload response: %w", err)
	}
	return &ProductImage{URI: result.URI, Width: result.Width, Height: result.Height}, nil
}

// ── Warehouses ────────────────────────────────────────────────────────────────

type Warehouse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type int    `json:"type"` // 1=fulfillment, 2=return
}

// GetWarehouses returns the seller's fulfillment warehouses.
func (c *Client) GetWarehouses() ([]Warehouse, error) {
	data, err := c.get("/api/v2/fulfillment/warehouses", nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		Warehouses []Warehouse `json:"warehouses"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("unmarshal warehouses: %w", err)
	}
	return result.Warehouses, nil
}

// ── HTTP method helpers ───────────────────────────────────────────────────────

// PutJSON is the public wrapper for making PUT requests. Used by the adapter for
// inventory and price sync operations where a full product update is not needed.
func (c *Client) PutJSON(path string, body interface{}) (json.RawMessage, error) {
	return c.put(path, nil, body)
}

func (c *Client) put(path string, extraParams map[string]string, body interface{}) (json.RawMessage, error) {
	timestamp := time.Now().Unix()
	params := c.baseParams(timestamp)
	for k, v := range extraParams {
		params[k] = v
	}

	var bodyStr string
	var bodyBytes []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		bodyBytes = b
		bodyStr = string(b)
	}

	params["sign"] = c.sign(path, params, bodyStr, timestamp)
	params["access_token"] = c.AccessToken

	u, _ := url.Parse(BaseURL + path)
	q := u.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequest("PUT", u.String(), bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-tts-access-token", c.AccessToken)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("PUT %s: %w", path, err)
	}
	defer resp.Body.Close()
	return c.parseResponse(resp)
}

func (c *Client) delete(path string, extraParams map[string]string, body interface{}) (json.RawMessage, error) {
	timestamp := time.Now().Unix()
	params := c.baseParams(timestamp)
	for k, v := range extraParams {
		params[k] = v
	}

	var bodyStr string
	var bodyBytes []byte
	if body != nil {
		b, _ := json.Marshal(body)
		bodyBytes = b
		bodyStr = string(b)
	}

	params["sign"] = c.sign(path, params, bodyStr, timestamp)
	params["access_token"] = c.AccessToken

	u, _ := url.Parse(BaseURL + path)
	q := u.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequest("DELETE", u.String(), bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-tts-access-token", c.AccessToken)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("DELETE %s: %w", path, err)
	}
	defer resp.Body.Close()
	return c.parseResponse(resp)
}

func (c *Client) baseParams(timestamp int64) map[string]string {
	params := map[string]string{
		"app_key":   c.AppKey,
		"timestamp": fmt.Sprintf("%d", timestamp),
		"version":   APIVersion,
	}
	if c.ShopID != "" {
		params["shop_id"] = c.ShopID
	}
	if c.ShopCipher != "" {
		params["shop_cipher"] = c.ShopCipher
	}
	return params
}

func (c *Client) parseResponse(resp *http.Response) (json.RawMessage, error) {
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	log.Printf("[TikTok] Response %d: %.400s", resp.StatusCode, string(respBytes))

	var apiResp apiResponse
	if err := json.Unmarshal(respBytes, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w (body: %.200s)", err, string(respBytes))
	}
	if apiResp.Code != 0 {
		return nil, fmt.Errorf("TikTok API error %d: %s", apiResp.Code, apiResp.Message)
	}
	return apiResp.Data, nil
}
