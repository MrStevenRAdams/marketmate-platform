package mirakl

// ============================================================================
// MIRAKL GENERIC SELLER API CLIENT
// ============================================================================
// Implements the Mirakl Marketplace Platform (MMP) Seller REST API.
// Reference: https://developer.mirakl.com/content/product/mmp/rest/seller/openapi3
//
// Auth:    API key in Authorization header (no OAuth)
// Base:    https://{instance}.mirakl.net
// Format:  JSON (Content-Type: application/json, Accept: application/json)
//
// This single client serves ALL Mirakl-powered marketplaces:
//   UK:    Tesco, B&Q (DIY.com), Superdrug, Debenhams, Decathlon UK,
//          Mountain Warehouse, H&M Home, JD Sports
//   EU:    Carrefour, MediaMarkt, Fnac Darty, Leroy Merlin, Maisons du Monde
//   US:    Macy's, Bloomingdale's, Best Buy Canada, Lowe's
//   Other: ASOS, Catch, Harvey Nichols, Joules, Secret Sales, Urban Outfitters
//
// Each marketplace gets its own instance URL and API key from the seller portal.
// ============================================================================

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ============================================================================
// KNOWN MIRAKL MARKETPLACE INSTANCES
// ============================================================================

// Instance holds the Mirakl base URL and display info for a known marketplace
type Instance struct {
	BaseURL     string
	DisplayName string
	Country     string
	Currency    string
	Categories  string // Primary product categories
}

// KnownInstances maps marketplace IDs to their Mirakl instance details.
// Sellers use these as defaults — the instance URL can always be overridden
// by storing a custom base_url in credentials.
var KnownInstances = map[string]Instance{
	// UK Marketplaces
	"tesco": {
		BaseURL:     "https://tesco.mirakl.net",
		DisplayName: "Tesco Marketplace",
		Country:     "GB",
		Currency:    "GBP",
		Categories:  "General merchandise, home, electricals, toys, sports",
	},
	"bandq": {
		BaseURL:     "https://diy.mirakl.net",
		DisplayName: "B&Q Marketplace",
		Country:     "GB",
		Currency:    "GBP",
		Categories:  "DIY, home improvement, garden, tools, lighting",
	},
	"superdrug": {
		BaseURL:     "https://superdrug.mirakl.net",
		DisplayName: "Superdrug Marketplace",
		Country:     "GB",
		Currency:    "GBP",
		Categories:  "Health, beauty, wellness, personal care",
	},
	"debenhams": {
		BaseURL:     "https://debenhams.mirakl.net",
		DisplayName: "Debenhams Marketplace",
		Country:     "GB",
		Currency:    "GBP",
		Categories:  "Fashion, beauty, home, electricals, sports",
	},
	"decathlon_uk": {
		BaseURL:     "https://decathlonuk.mirakl.net",
		DisplayName: "Decathlon UK Marketplace",
		Country:     "GB",
		Currency:    "GBP",
		Categories:  "Sport, outdoor, fitness",
	},
	"mountain_warehouse": {
		BaseURL:     "https://mountainwarehouse.mirakl.net",
		DisplayName: "Mountain Warehouse Marketplace",
		Country:     "GB",
		Currency:    "GBP",
		Categories:  "Outdoor, camping, hiking, clothing",
	},
	"jd_sports": {
		BaseURL:     "https://jdsports.mirakl.net",
		DisplayName: "JD Sports Marketplace",
		Country:     "GB",
		Currency:    "GBP",
		Categories:  "Sports fashion, footwear, clothing",
	},
	// EU Marketplaces
	"carrefour": {
		BaseURL:     "https://carrefour.mirakl.net",
		DisplayName: "Carrefour Marketplace",
		Country:     "FR",
		Currency:    "EUR",
		Categories:  "General merchandise, food, electronics",
	},
	"decathlon_fr": {
		BaseURL:     "https://decathlon.mirakl.net",
		DisplayName: "Decathlon France Marketplace",
		Country:     "FR",
		Currency:    "EUR",
		Categories:  "Sport, outdoor, fitness",
	},
	"fnac_darty": {
		BaseURL:     "https://fnacdarty.mirakl.net",
		DisplayName: "Fnac Darty Marketplace",
		Country:     "FR",
		Currency:    "EUR",
		Categories:  "Electronics, music, books, gaming",
	},
	"leroy_merlin": {
		BaseURL:     "https://leroymerlin.mirakl.net",
		DisplayName: "Leroy Merlin Marketplace",
		Country:     "FR",
		Currency:    "EUR",
		Categories:  "DIY, building, garden, home improvement",
	},
	"mediamarkt": {
		BaseURL:     "https://mediamarkt.mirakl.net",
		DisplayName: "MediaMarkt Marketplace",
		Country:     "DE",
		Currency:    "EUR",
		Categories:  "Consumer electronics, white goods, tech",
	},
	// Global
	"asos": {
		BaseURL:     "https://asos.mirakl.net",
		DisplayName: "ASOS Marketplace",
		Country:     "GB",
		Currency:    "GBP",
		Categories:  "Fashion, clothing, accessories",
	},
	"macys": {
		BaseURL:     "https://macys.mirakl.net",
		DisplayName: "Macy's Marketplace",
		Country:     "US",
		Currency:    "USD",
		Categories:  "Fashion, home, beauty, gifts",
	},
	"lowes": {
		BaseURL:     "https://lowes.mirakl.net",
		DisplayName: "Lowe's Marketplace",
		Country:     "US",
		Currency:    "USD",
		Categories:  "Home improvement, tools, appliances, garden",
	},
}

// ============================================================================
// CLIENT
// ============================================================================

// Client is the low-level Mirakl Seller API client.
// One client instance per marketplace credential.
type Client struct {
	baseURL    string // e.g. https://tesco.mirakl.net
	apiKey     string // Seller API key from Mirakl portal
	httpClient *http.Client
	shopID     string // Optional: for multi-shop accounts
}

// NewClient creates a new Mirakl client.
//
//	baseURL  — full instance URL e.g. "https://tesco.mirakl.net"
//	apiKey   — seller API key from Mirakl seller portal → username → API Key
//	shopID   — optional; required only when user has access to multiple shops
func NewClient(baseURL, apiKey, shopID string) *Client {
	// Normalise: strip trailing slash
	baseURL = strings.TrimRight(baseURL, "/")
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		shopID:  shopID,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// NewClientForMarketplace creates a client from a known marketplace ID.
// Falls back to custom base URL if the marketplace is not in KnownInstances.
func NewClientForMarketplace(marketplaceID, apiKey, shopID, customBaseURL string) *Client {
	baseURL := customBaseURL
	if baseURL == "" {
		if inst, ok := KnownInstances[marketplaceID]; ok {
			baseURL = inst.BaseURL
		}
	}
	if baseURL == "" {
		baseURL = fmt.Sprintf("https://%s.mirakl.net", marketplaceID)
	}
	return NewClient(baseURL, apiKey, shopID)
}

// ============================================================================
// HTTP HELPERS
// ============================================================================

// do executes an authenticated request against the Mirakl API.
func (c *Client) do(method, path string, query url.Values, body interface{}) ([]byte, int, error) {
	// Build URL
	fullURL := c.baseURL + path
	if len(query) > 0 {
		if c.shopID != "" && query.Get("shop_id") == "" {
			query.Set("shop_id", c.shopID)
		}
		fullURL += "?" + query.Encode()
	} else if c.shopID != "" {
		fullURL += "?shop_id=" + url.QueryEscape(c.shopID)
	}

	// Encode body
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("mirakl: marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, fullURL, bodyReader)
	if err != nil {
		return nil, 0, fmt.Errorf("mirakl: build request: %w", err)
	}

	// Auth: API key in Authorization header (no "Bearer" prefix)
	req.Header.Set("Authorization", c.apiKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("mirakl: http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("mirakl: read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		// Try to extract Mirakl error message
		var apiErr struct {
			Message string        `json:"message"`
			Errors  []interface{} `json:"errors"`
		}
		_ = json.Unmarshal(respBody, &apiErr)
		msg := apiErr.Message
		if msg == "" {
			msg = string(respBody)
		}
		return nil, resp.StatusCode, fmt.Errorf("mirakl: API error %d: %s", resp.StatusCode, msg)
	}

	return respBody, resp.StatusCode, nil
}

func (c *Client) get(path string, query url.Values) ([]byte, error) {
	b, _, err := c.do(http.MethodGet, path, query, nil)
	return b, err
}

func (c *Client) put(path string, body interface{}) ([]byte, error) {
	b, _, err := c.do(http.MethodPut, path, nil, body)
	return b, err
}

func (c *Client) post(path string, body interface{}) ([]byte, error) {
	b, _, err := c.do(http.MethodPost, path, nil, body)
	return b, err
}

// ============================================================================
// HEALTH CHECK
// ============================================================================

// V01 - Health Check. Returns nil if the platform is reachable and the API
// key is valid (any 2xx response).
func (c *Client) HealthCheck() error {
	_, err := c.get("/api/version", nil)
	return err
}

// ============================================================================
// STORE / ACCOUNT (A01)
// ============================================================================

// ShopInfo holds seller account information from Mirakl.
type ShopInfo struct {
	ShopID      int    `json:"shop_id"`
	ShopName    string `json:"shop_name"`
	Email       string `json:"email"`
	Description string `json:"description"`
	State       string `json:"state"` // "OPEN", "CLOSED", "SUSPENDED"
	IsPro       bool   `json:"is_professional"`
	WebSite     string `json:"web_site"`
}

// GetShopInfo calls A01 — GET /api/account
func (c *Client) GetShopInfo() (*ShopInfo, error) {
	b, err := c.get("/api/account", nil)
	if err != nil {
		return nil, fmt.Errorf("GetShopInfo: %w", err)
	}
	var info ShopInfo
	if err := json.Unmarshal(b, &info); err != nil {
		return nil, fmt.Errorf("GetShopInfo: parse: %w", err)
	}
	return &info, nil
}

// ============================================================================
// PLATFORM SETTINGS
// ============================================================================

// Category represents a single node in the marketplace category hierarchy.
type Category struct {
	Code       string     `json:"code"`
	Label      string     `json:"label"`
	Level      int        `json:"level"`
	LeafNode   bool       `json:"leaf_node"`
	ParentCode string     `json:"parent_code,omitempty"`
	Children   []Category `json:"children,omitempty"`
}

// hierarchyResponse is the raw API shape for H11
type hierarchyResponse struct {
	Hierarchies []struct {
		Code     string `json:"code"`
		Label    string `json:"label"`
		Level    int    `json:"level"`
		LeafNode bool   `json:"leaf_node"`
	} `json:"hierarchies"`
}

// GetCategories calls H11 — GET /api/hierarchies
// Returns the full flat list of categories; callers can nest them if needed.
func (c *Client) GetCategories() ([]Category, error) {
	b, err := c.get("/api/hierarchies", nil)
	if err != nil {
		return nil, fmt.Errorf("GetCategories: %w", err)
	}
	var raw hierarchyResponse
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("GetCategories: parse: %w", err)
	}
	cats := make([]Category, 0, len(raw.Hierarchies))
	for _, h := range raw.Hierarchies {
		cats = append(cats, Category{
			Code:     h.Code,
			Label:    h.Label,
			Level:    h.Level,
			LeafNode: h.LeafNode,
		})
	}
	return cats, nil
}

// ShippingCarrier holds a carrier registered on the Mirakl instance.
type ShippingCarrier struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

// GetCarriers calls SH21 — GET /api/shipping/carriers
func (c *Client) GetCarriers() ([]ShippingCarrier, error) {
	b, err := c.get("/api/shipping/carriers", nil)
	if err != nil {
		return nil, fmt.Errorf("GetCarriers: %w", err)
	}
	var raw struct {
		ShippingCarriers []ShippingCarrier `json:"shipping_carriers"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("GetCarriers: parse: %w", err)
	}
	return raw.ShippingCarriers, nil
}

// ============================================================================
// PRODUCTS (P31, P41, P42)
// ============================================================================

// ProductRef is the identifier pair used to look up a product
type ProductRef struct {
	Type  string // e.g. "EAN", "GTIN", "SKU", "SHOP_SKU"
	Value string
}

// ProductAttribute is a single attribute on a Mirakl product
type ProductAttribute struct {
	Code  string   `json:"code"`
	Value []string `json:"value"`
	Unit  string   `json:"unit,omitempty"`
}

// Product represents a Mirakl catalog product
type Product struct {
	ProductSKU  string             `json:"product_sku"`
	Title       string             `json:"product_title"`
	Description string             `json:"description"`
	Brand       string             `json:"brand"`
	CategoryCode string            `json:"category_code"`
	MediaURLs   []string           `json:"media_urls"`
	Attributes  []ProductAttribute `json:"attributes"`
	Active      bool               `json:"active"`
}

// ProductImportRequest is the payload for P41 — POST /api/products/imports
type ProductImportRequest struct {
	Products []ProductPayload `json:"products"`
}

// ProductPayload is the per-product data sent on import
type ProductPayload struct {
	ShopSKU      string             `json:"shop-sku"`
	Title        string             `json:"product-title"`
	Description  string             `json:"description,omitempty"`
	Brand        string             `json:"brand,omitempty"`
	CategoryCode string             `json:"category"`
	MediaURLs    []string           `json:"media-url,omitempty"`
	Attributes   []ProductAttribute `json:"attributes,omitempty"`
	// Offer fields (price + stock can be embedded in product import)
	Price       float64 `json:"price,omitempty"`
	Quantity    int     `json:"quantity,omitempty"`
	State       string  `json:"state,omitempty"` // "11" = new
	Description2 string `json:"description-2,omitempty"`
}

// ImportStatus is the response from P41 and P42
type ImportStatus struct {
	ImportID        string `json:"import_id"`
	Status          string `json:"status"` // "PENDING", "RUNNING", "COMPLETE", "FAILED"
	ErrorCount      int    `json:"error_count"`
	ProductsCreated int    `json:"products_created"`
	ProductsUpdated int    `json:"products_updated"`
	LinesRead       int    `json:"lines_read"`
	HasErrorReport  bool   `json:"has_error_report"`
}

// ImportProducts calls P41 — POST /api/products/imports
// Submits products to the Mirakl catalog.
func (c *Client) ImportProducts(req ProductImportRequest) (*ImportStatus, error) {
	b, err := c.post("/api/products/imports", req)
	if err != nil {
		return nil, fmt.Errorf("ImportProducts: %w", err)
	}
	var status ImportStatus
	if err := json.Unmarshal(b, &status); err != nil {
		return nil, fmt.Errorf("ImportProducts: parse: %w", err)
	}
	return &status, nil
}

// GetImportStatus calls P42 — GET /api/products/imports/{import}
func (c *Client) GetImportStatus(importID string) (*ImportStatus, error) {
	b, err := c.get("/api/products/imports/"+importID, nil)
	if err != nil {
		return nil, fmt.Errorf("GetImportStatus: %w", err)
	}
	var status ImportStatus
	if err := json.Unmarshal(b, &status); err != nil {
		return nil, fmt.Errorf("GetImportStatus: parse: %w", err)
	}
	return &status, nil
}

// ============================================================================
// OFFERS (OF21, OF22, OF24)
// ============================================================================

// Offer represents a seller offer (price + stock for a SKU)
type Offer struct {
	OfferID            string  `json:"offer_id"`
	ShopSKU            string  `json:"shop_sku"`
	ProductSKU         string  `json:"product_sku,omitempty"`
	ProductTitle       string  `json:"product_title,omitempty"`
	Price              float64 `json:"price"`
	DiscountPrice      float64 `json:"discount_price,omitempty"`
	Quantity           int     `json:"quantity"`
	State              string  `json:"state_code"` // e.g. "11" = new
	Active             bool    `json:"active"`
	ShippingTypeCode   string  `json:"shipping_type_code,omitempty"`
	MinShippingDays    int     `json:"min_shipping_days,omitempty"`
	MaxShippingDays    int     `json:"max_shipping_days,omitempty"`
	AllowedQuantity    string  `json:"allowed_quantity,omitempty"`
}

// OfferListResponse is the paginated response from OF21
type OfferListResponse struct {
	Offers         []Offer `json:"offers"`
	TotalCount     int     `json:"total_count"`
	Offset         int     `json:"offset"`
}

// ListOffersOptions filters for OF21
type ListOffersOptions struct {
	Offset   int
	Max      int    // default 10, max 100
	SKU      string // filter by shop_sku
	Active   *bool
}

// ListOffers calls OF21 — GET /api/offers
// Returns seller's current offers with pagination.
func (c *Client) ListOffers(opts ListOffersOptions) (*OfferListResponse, error) {
	q := url.Values{}
	if opts.Max > 0 {
		q.Set("max", strconv.Itoa(opts.Max))
	} else {
		q.Set("max", "100")
	}
	q.Set("offset", strconv.Itoa(opts.Offset))
	if opts.SKU != "" {
		q.Set("offer_query", opts.SKU)
	}

	b, err := c.get("/api/offers", q)
	if err != nil {
		return nil, fmt.Errorf("ListOffers: %w", err)
	}
	var resp OfferListResponse
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, fmt.Errorf("ListOffers: parse: %w", err)
	}
	return &resp, nil
}

// GetOffer calls OF22 — GET /api/offers/{offer_id}
func (c *Client) GetOffer(offerID string) (*Offer, error) {
	b, err := c.get("/api/offers/"+offerID, nil)
	if err != nil {
		return nil, fmt.Errorf("GetOffer: %w", err)
	}
	var offer Offer
	if err := json.Unmarshal(b, &offer); err != nil {
		return nil, fmt.Errorf("GetOffer: parse: %w", err)
	}
	return &offer, nil
}

// OfferUpsert is the payload for creating or updating a single offer via OF24.
// To delete: set UpdateDelete to "delete"
type OfferUpsert struct {
	ShopSKU        string  `json:"shop-sku"`
	ProductSKU     string  `json:"product-id,omitempty"`
	ProductIDType  string  `json:"product-id-type,omitempty"` // "SHOP_SKU", "EAN", "ISBN"
	UpdateDelete   string  `json:"update-delete,omitempty"`   // "update", "delete"
	Price          float64 `json:"price"`
	DiscountPrice  float64 `json:"discount-price,omitempty"`
	Quantity       int     `json:"quantity"`
	State          string  `json:"state,omitempty"` // "11" = new
	Description    string  `json:"product-description,omitempty"`
	ShippingType   string  `json:"shipping-type,omitempty"`
}

// UpsertOffersRequest is the body for OF24 — POST /api/offers
type UpsertOffersRequest struct {
	Offers []OfferUpsert `json:"offers"`
}

// UpsertOffersResponse is the result from OF24
type UpsertOffersResponse struct {
	OfferImportID string `json:"offer_import_id"`
}

// UpsertOffers calls OF24 — POST /api/offers
// Creates, updates or deletes offers in bulk (max 500 per call).
func (c *Client) UpsertOffers(offers []OfferUpsert) (*UpsertOffersResponse, error) {
	req := UpsertOffersRequest{Offers: offers}
	b, err := c.post("/api/offers", req)
	if err != nil {
		return nil, fmt.Errorf("UpsertOffers: %w", err)
	}
	var resp UpsertOffersResponse
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, fmt.Errorf("UpsertOffers: parse: %w", err)
	}
	return &resp, nil
}

// ============================================================================
// STOCK (STO01 via offer upsert)
// ============================================================================

// UpdateStock updates only the quantity for a given SKU.
// Uses UpsertOffers with just the quantity field (Mirakl only updates supplied fields).
func (c *Client) UpdateStock(shopSKU string, quantity int) error {
	_, err := c.UpsertOffers([]OfferUpsert{
		{
			ShopSKU:      shopSKU,
			UpdateDelete: "update",
			Quantity:     quantity,
			Price:        0, // not updating price
		},
	})
	return err
}

// UpdatePrice updates only the price for a given SKU.
func (c *Client) UpdatePrice(shopSKU string, price float64) error {
	_, err := c.UpsertOffers([]OfferUpsert{
		{
			ShopSKU:      shopSKU,
			UpdateDelete: "update",
			Price:        price,
			Quantity:     -1, // sentinel: not updating qty
		},
	})
	return err
}

// ============================================================================
// INVOICES (IV01)
// ============================================================================

// Invoice represents a Mirakl accounting document
type Invoice struct {
	ID          string  `json:"id"`
	Type        string  `json:"type"`     // "INVOICE", "CREDIT_NOTE"
	State       string  `json:"state"`    // "WAITING", "WAITING_DEBIT", "COMPLETE"
	Amount      float64 `json:"amount"`
	Currency    string  `json:"currency_iso_code"`
	DateCreated string  `json:"date_created"`
	DateUpdated string  `json:"date_updated"`
	DownloadURL string  `json:"download_url,omitempty"`
}

// ListInvoices calls IV01 — GET /api/invoices
func (c *Client) ListInvoices(startDate, endDate string) ([]Invoice, error) {
	q := url.Values{}
	if startDate != "" {
		q.Set("start_date", startDate)
	}
	if endDate != "" {
		q.Set("end_date", endDate)
	}
	q.Set("max", "100")
	b, err := c.get("/api/invoices", q)
	if err != nil {
		return nil, fmt.Errorf("ListInvoices: %w", err)
	}
	var raw struct {
		Invoices []Invoice `json:"invoices"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("ListInvoices: parse: %w", err)
	}
	return raw.Invoices, nil
}
