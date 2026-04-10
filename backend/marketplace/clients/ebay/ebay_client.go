package ebay

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ============================================================================
// EBAY REST API CLIENT
// ============================================================================
// Handles OAuth2 token management and all eBay REST API calls.
// Uses the Inventory API for import/export and Identity API for OAuth.
// ============================================================================

const (
	ProdAPIRoot  = "https://api.ebay.com"
	ProdAuthURL  = "https://auth.ebay.com/oauth2/authorize"
	ProdTokenURL = "https://api.ebay.com/identity/v1/oauth2/token"

	SandboxAPIRoot  = "https://api.sandbox.ebay.com"
	SandboxAuthURL  = "https://auth.sandbox.ebay.com/oauth2/authorize"
	SandboxTokenURL = "https://api.sandbox.ebay.com/identity/v1/oauth2/token"

	// Scopes we need for sell operations
	ScopeSellInventory         = "https://api.ebay.com/oauth/api_scope/sell.inventory"
	ScopeSellInventoryReadonly = "https://api.ebay.com/oauth/api_scope/sell.inventory.readonly"
	ScopeSellAccount           = "https://api.ebay.com/oauth/api_scope/sell.account"
	ScopeSellFulfillment       = "https://api.ebay.com/oauth/api_scope/sell.fulfillment"
	ScopeSellMarketing         = "https://api.ebay.com/oauth/api_scope/sell.marketing"
	ScopeAPIScope              = "https://api.ebay.com/oauth/api_scope"
	ScopeCommerceIdentity      = "https://api.ebay.com/oauth/api_scope/commerce.identity.readonly"
)

// DefaultScopes is the set of scopes we request during OAuth consent
var DefaultScopes = []string{
	ScopeAPIScope,
	ScopeSellInventory,
	ScopeSellAccount,
	ScopeSellFulfillment,
	ScopeSellMarketing,
}

// Client is the eBay REST API client
type Client struct {
	ClientID     string
	ClientSecret string
	DevID        string
	APIRoot      string
	AuthURL      string
	TokenURL     string
	RuName       string // Redirect URL name (configured in eBay developer portal)

	// Token management
	AccessToken  string
	RefreshToken string
	TokenExpiry  time.Time
	mu           sync.RWMutex

	// Callback for persisting refreshed tokens
	OnTokenRefresh func(accessToken, refreshToken string, expiresIn int)

	// Seller identity (populated from credentials or Identity API)
	SellerUsername string

	HTTPClient *http.Client
}

// NewClient creates a new eBay API client
func NewClient(clientID, clientSecret, devID string, production bool) *Client {
	c := &Client{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		DevID:        devID,
		HTTPClient:   &http.Client{Timeout: 30 * time.Second},
	}
	if production {
		c.APIRoot = ProdAPIRoot
		c.AuthURL = ProdAuthURL
		c.TokenURL = ProdTokenURL
	} else {
		c.APIRoot = SandboxAPIRoot
		c.AuthURL = SandboxAuthURL
		c.TokenURL = SandboxTokenURL
	}
	return c
}

// SetTokens sets the current access and refresh tokens
func (c *Client) SetTokens(accessToken, refreshToken string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.AccessToken = accessToken
	c.RefreshToken = refreshToken
	// If we don't know expiry, assume expired so it will refresh on next call
	if accessToken != "" {
		c.TokenExpiry = time.Now().Add(2 * time.Hour) // Default 2hr
	}
}

// ============================================================================
// OAUTH2 FLOWS
// ============================================================================

// GetConsentURL builds the eBay consent URL that the user should visit
func (c *Client) GetConsentURL(ruName, state string) string {
	scopes := url.QueryEscape(strings.Join(DefaultScopes, " "))
	return fmt.Sprintf("%s?client_id=%s&redirect_uri=%s&response_type=code&scope=%s&state=%s&prompt=login",
		c.AuthURL, c.ClientID, ruName, scopes, state)
}

// TokenResponse is the response from eBay's token endpoint
type TokenResponse struct {
	AccessToken           string `json:"access_token"`
	ExpiresIn             int    `json:"expires_in"`
	RefreshToken          string `json:"refresh_token"`
	RefreshTokenExpiresIn int    `json:"refresh_token_expires_in"`
	TokenType             string `json:"token_type"`
}

// ExchangeAuthCode exchanges an authorization code for access + refresh tokens
func (c *Client) ExchangeAuthCode(authCode, ruName string) (*TokenResponse, error) {
	data := url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {authCode},
		"redirect_uri": {ruName},
	}

	return c.tokenRequest(data)
}

// RefreshAccessToken uses the refresh token to get a new access token
func (c *Client) RefreshAccessToken() error {
	c.mu.RLock()
	refreshToken := c.RefreshToken
	c.mu.RUnlock()

	if refreshToken == "" {
		return fmt.Errorf("no refresh token available")
	}

	data := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"scope":         {strings.Join(DefaultScopes, " ")},
	}

	resp, err := c.tokenRequest(data)
	if err != nil {
		return fmt.Errorf("refresh token failed: %w", err)
	}

	c.mu.Lock()
	c.AccessToken = resp.AccessToken
	c.TokenExpiry = time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second)
	if resp.RefreshToken != "" {
		c.RefreshToken = resp.RefreshToken
	}
	c.mu.Unlock()

	// Notify callback if set
	if c.OnTokenRefresh != nil {
		c.OnTokenRefresh(resp.AccessToken, c.RefreshToken, resp.ExpiresIn)
	}

	log.Printf("[eBay] Token refreshed, expires in %d seconds", resp.ExpiresIn)
	return nil
}

func (c *Client) tokenRequest(data url.Values) (*TokenResponse, error) {
	credentials := base64.StdEncoding.EncodeToString(
		[]byte(c.ClientID + ":" + c.ClientSecret),
	)

	req, err := http.NewRequest("POST", c.TokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Basic "+credentials)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("token request failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}

	return &tokenResp, nil
}

// ensureValidToken refreshes the token if it's expired or about to expire
func (c *Client) ensureValidToken() error {
	c.mu.RLock()
	needsRefresh := c.AccessToken == "" || time.Now().After(c.TokenExpiry.Add(-5*time.Minute))
	c.mu.RUnlock()

	if needsRefresh && c.RefreshToken != "" {
		return c.RefreshAccessToken()
	}
	return nil
}

// ============================================================================
// HTTP HELPERS
// ============================================================================

func (c *Client) doRequest(method, path string, body io.Reader) ([]byte, int, error) {
	if err := c.ensureValidToken(); err != nil {
		return nil, 0, fmt.Errorf("token refresh: %w", err)
	}

	fullURL := c.APIRoot + path

	req, err := http.NewRequest(method, fullURL, body)
	if err != nil {
		return nil, 0, err
	}

	c.mu.RLock()
	req.Header.Set("Authorization", "Bearer "+c.AccessToken)
	c.mu.RUnlock()

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Language", "en-GB")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}

	return respBody, resp.StatusCode, nil
}

func (c *Client) get(path string) ([]byte, error) {
	body, status, err := c.doRequest("GET", path, nil)
	if err != nil {
		log.Printf("[eBay Client] GET %s failed (transport): %v", path, err)
		return nil, err
	}
	if status >= 400 {
		log.Printf("[eBay Client] GET %s returned HTTP %d: %s", path, status, truncate(string(body), 500))
		return nil, fmt.Errorf("eBay API error (HTTP %d): %s", status, truncate(string(body), 500))
	}
	return body, nil
}

func (c *Client) put(path string, payload interface{}) ([]byte, int, error) {
	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, err
	}
	return c.doRequest("PUT", path, strings.NewReader(string(jsonBody)))
}

func (c *Client) post(path string, payload interface{}) ([]byte, int, error) {
	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, err
	}
	return c.doRequest("POST", path, strings.NewReader(string(jsonBody)))
}

func (c *Client) delete(path string) (int, error) {
	_, status, err := c.doRequest("DELETE", path, nil)
	return status, err
}

// ============================================================================
// INVENTORY API - ITEMS
// ============================================================================

// InventoryItem is an eBay inventory item
type InventoryItem struct {
	SKU                string                 `json:"sku,omitempty"`
	Locale             string                 `json:"locale,omitempty"`
	Product            *Product               `json:"product,omitempty"`
	Condition          string                 `json:"condition,omitempty"`
	ConditionDescription string               `json:"conditionDescription,omitempty"`
	Availability       *Availability          `json:"availability,omitempty"`
	PackageWeightAndSize *PackageWeightAndSize `json:"packageWeightAndSize,omitempty"`
	GroupIDs           []string               `json:"groupIds,omitempty"`
	InventoryItemGroupKeys []string           `json:"inventoryItemGroupKeys,omitempty"`
	Raw                map[string]interface{} `json:"-"`
}

type Product struct {
	Title       string                 `json:"title,omitempty"`
	Description string                 `json:"description,omitempty"`
	Aspects     map[string][]string    `json:"aspects,omitempty"`
	Brand       string                 `json:"brand,omitempty"`
	MPN         string                 `json:"mpn,omitempty"`
	EAN         []string               `json:"ean,omitempty"`
	UPC         []string               `json:"upc,omitempty"`
	ISBN        []string               `json:"isbn,omitempty"`
	EPID        string                 `json:"epid,omitempty"`
	ImageURLs   []string               `json:"imageUrls,omitempty"`
}

type Availability struct {
	ShipToLocationAvailability *ShipToLocation `json:"shipToLocationAvailability,omitempty"`
}

type ShipToLocation struct {
	Quantity int `json:"quantity,omitempty"`
}

type PackageWeightAndSize struct {
	Dimensions *Dimensions `json:"dimensions,omitempty"`
	Weight     *Weight     `json:"weight,omitempty"`
	PackageType string    `json:"packageType,omitempty"`
}

type Dimensions struct {
	Height float64 `json:"height,omitempty"`
	Length float64 `json:"length,omitempty"`
	Width  float64 `json:"width,omitempty"`
	Unit   string  `json:"unit,omitempty"` // INCH, FEET, CENTIMETER, METER
}

type Weight struct {
	Value float64 `json:"value,omitempty"`
	Unit  string  `json:"unit,omitempty"` // POUND, KILOGRAM, OUNCE, GRAM
}

// InventoryItemsPage is a paginated list of inventory items
type InventoryItemsPage struct {
	Total             int              `json:"total"`
	Size              int              `json:"size"`
	Href              string           `json:"href"`
	Next              string           `json:"next"`
	Prev              string           `json:"prev"`
	InventoryItems    []InventoryItem  `json:"inventoryItems"`
}

// GetInventoryItems returns a paginated list of inventory items
func (c *Client) GetInventoryItems(limit, offset int) (*InventoryItemsPage, error) {
	if limit < 1 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}

	path := fmt.Sprintf("/sell/inventory/v1/inventory_item?limit=%d&offset=%d", limit, offset)
	log.Printf("[eBay Client] GET %s%s", c.APIRoot, path)
	body, err := c.get(path)
	if err != nil {
		log.Printf("[eBay Client] GetInventoryItems ERROR: %v", err)
		return nil, err
	}

	// Log raw response (truncated)
	rawStr := string(body)
	if len(rawStr) > 1000 {
		rawStr = rawStr[:1000] + "... (truncated)"
	}
	log.Printf("[eBay Client] GetInventoryItems raw response (%d bytes): %s", len(body), rawStr)

	var page InventoryItemsPage
	if err := json.Unmarshal(body, &page); err != nil {
		log.Printf("[eBay Client] GetInventoryItems parse error: %v", err)
		return nil, fmt.Errorf("parse inventory items: %w", err)
	}

	log.Printf("[eBay Client] GetInventoryItems parsed: total=%d, items=%d", page.Total, len(page.InventoryItems))
	return &page, nil
}

// GetInventoryItem returns a single inventory item by SKU
func (c *Client) GetInventoryItem(sku string) (*InventoryItem, error) {
	path := fmt.Sprintf("/sell/inventory/v1/inventory_item/%s", url.PathEscape(sku))
	body, err := c.get(path)
	if err != nil {
		return nil, err
	}

	var item InventoryItem
	if err := json.Unmarshal(body, &item); err != nil {
		return nil, fmt.Errorf("parse inventory item: %w", err)
	}
	item.SKU = sku

	// Also keep raw data
	var raw map[string]interface{}
	json.Unmarshal(body, &raw)
	item.Raw = raw

	return &item, nil
}

// CreateOrReplaceInventoryItem creates or updates an inventory item
func (c *Client) CreateOrReplaceInventoryItem(sku string, item *InventoryItem) error {
	path := fmt.Sprintf("/sell/inventory/v1/inventory_item/%s", url.PathEscape(sku))
	_, status, err := c.put(path, item)
	if err != nil {
		return err
	}
	if status != 200 && status != 204 {
		return fmt.Errorf("create/replace inventory item: HTTP %d", status)
	}
	return nil
}

// DeleteInventoryItem removes an inventory item by SKU
func (c *Client) DeleteInventoryItem(sku string) error {
	path := fmt.Sprintf("/sell/inventory/v1/inventory_item/%s", url.PathEscape(sku))
	status, err := c.delete(path)
	if err != nil {
		return err
	}
	if status != 204 {
		return fmt.Errorf("delete inventory item: HTTP %d", status)
	}
	return nil
}

// ============================================================================
// INVENTORY API - OFFERS
// ============================================================================

// Offer represents an eBay offer (becomes a listing when published)
type Offer struct {
	OfferID                 string              `json:"offerId,omitempty"`
	SKU                     string              `json:"sku"`
	MarketplaceID           string              `json:"marketplaceId"`           // EBAY_GB, EBAY_US, etc.
	Format                  string              `json:"format"`                   // FIXED_PRICE or AUCTION
	CategoryID              string              `json:"categoryId"`
	ListingDescription      string              `json:"listingDescription,omitempty"`
	ListingDuration         string              `json:"listingDuration,omitempty"` // GTC for fixed price
	AvailableQuantity       int                 `json:"availableQuantity,omitempty"`
	PricingSummary          *PricingSummary      `json:"pricingSummary,omitempty"`
	ListingPolicies         *ListingPolicies     `json:"listingPolicies,omitempty"`
	MerchantLocationKey     string              `json:"merchantLocationKey,omitempty"`
	IncludeCatalogProductDetails bool           `json:"includeCatalogProductDetails,omitempty"`
	Status                  string              `json:"status,omitempty"`
	Listing                 *ListingRef          `json:"listing,omitempty"`
}

type PricingSummary struct {
	Price                  *Amount        `json:"price,omitempty"`
	MinimumAdvertisedPrice *Amount        `json:"minimumAdvertisedPrice,omitempty"`
	PricingTiers           []PricingTier  `json:"pricingTiers,omitempty"`
}

// PricingTier defines a quantity-based price break for eBay Volume Pricing
type PricingTier struct {
	MinQuantity int     `json:"minQuantity"`
	Price       *Amount `json:"price"`
}

type Amount struct {
	Value    string `json:"value"`
	Currency string `json:"currency"`
}

type ListingPolicies struct {
	PaymentPolicyID    string `json:"paymentPolicyId,omitempty"`
	ReturnPolicyID     string `json:"returnPolicyId,omitempty"`
	FulfillmentPolicyID string `json:"fulfillmentPolicyId,omitempty"`
}

type ListingRef struct {
	ListingID    string `json:"listingId,omitempty"`
	ListingStatus string `json:"listingStatus,omitempty"`
}

// OffersPage is a paginated list of offers
type OffersPage struct {
	Total  int     `json:"total"`
	Size   int     `json:"size"`
	Href   string  `json:"href"`
	Next   string  `json:"next"`
	Offers []Offer `json:"offers"`
}

// GetOffers returns offers for a specific SKU
func (c *Client) GetOffers(sku string) (*OffersPage, error) {
	path := fmt.Sprintf("/sell/inventory/v1/offer?sku=%s", url.QueryEscape(sku))
	body, err := c.get(path)
	if err != nil {
		return nil, err
	}

	var page OffersPage
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, fmt.Errorf("parse offers: %w", err)
	}
	return &page, nil
}

// CreateOffer creates a new offer (draft listing)
func (c *Client) CreateOffer(offer *Offer) (string, error) {
	body, status, err := c.post("/sell/inventory/v1/offer", offer)
	if err != nil {
		return "", err
	}
	if status != 201 {
		return "", fmt.Errorf("create offer: HTTP %d: %s", status, truncate(string(body), 500))
	}

	var result struct {
		OfferID string `json:"offerId"`
	}
	json.Unmarshal(body, &result)
	return result.OfferID, nil
}

// PublishOffer publishes an offer to create an active listing
func (c *Client) PublishOffer(offerID string) (string, error) {
	path := fmt.Sprintf("/sell/inventory/v1/offer/%s/publish", offerID)
	body, status, err := c.post(path, nil)
	if err != nil {
		return "", err
	}
	if status != 200 {
		return "", fmt.Errorf("publish offer: HTTP %d: %s", status, truncate(string(body), 500))
	}

	var result struct {
		ListingID string `json:"listingId"`
	}
	json.Unmarshal(body, &result)
	return result.ListingID, nil
}

// WithdrawOffer withdraws (unpublishes) an offer
func (c *Client) WithdrawOffer(offerID string) error {
	path := fmt.Sprintf("/sell/inventory/v1/offer/%s/withdraw", offerID)
	_, status, err := c.post(path, nil)
	if err != nil {
		return err
	}
	if status != 200 && status != 204 {
		return fmt.Errorf("withdraw offer: HTTP %d", status)
	}
	return nil
}

// UpdateOfferPrice patches the price on an existing offer using the eBay Inventory API.
// offerID is the eBay offer ID (not the listing ID). currency should be the ISO code
// matching the offer's marketplace (e.g. "GBP" for EBAY_GB, "USD" for EBAY_US).
// This uses a partial-update PUT which preserves all other offer fields.
func (c *Client) UpdateOfferPrice(offerID string, price float64, currency string) error {
	if offerID == "" {
		return fmt.Errorf("update offer price: offerID is required")
	}
	if price <= 0 {
		return fmt.Errorf("update offer price: price must be positive, got %f", price)
	}
	if currency == "" {
		currency = "GBP"
	}
	patch := map[string]interface{}{
		"pricingSummary": map[string]interface{}{
			"price": map[string]interface{}{
				"value":    fmt.Sprintf("%.2f", price),
				"currency": currency,
			},
		},
	}
	path := fmt.Sprintf("/sell/inventory/v1/offer/%s", url.PathEscape(offerID))
	_, status, err := c.put(path, patch)
	if err != nil {
		return fmt.Errorf("update offer price: %w", err)
	}
	// eBay returns 200 on success for offer updates
	if status != 200 && status != 204 {
		return fmt.Errorf("update offer price: HTTP %d", status)
	}
	return nil
}

// GetOfferBySKU fetches the first active offer for a given SKU.
// Returns nil, nil if no offer exists (not an error — caller can skip).
func (c *Client) GetOfferBySKU(sku string) (*Offer, error) {
	page, err := c.GetOffers(sku)
	if err != nil {
		return nil, err
	}
	if page == nil || len(page.Offers) == 0 {
		return nil, nil
	}
	// Prefer published offers; fall back to any offer
	for i := range page.Offers {
		if page.Offers[i].Status == "PUBLISHED" {
			return &page.Offers[i], nil
		}
	}
	return &page.Offers[0], nil
}

// ============================================================================
// INVENTORY API - LOCATIONS
// ============================================================================

// InventoryLocation is a seller's inventory location
type InventoryLocation struct {
	MerchantLocationKey    string           `json:"merchantLocationKey,omitempty"`
	Name                   string           `json:"name,omitempty"`
	MerchantLocationStatus string           `json:"merchantLocationStatus,omitempty"`
	Location               *LocationDetail  `json:"location,omitempty"`
	LocationTypes          []string         `json:"locationTypes,omitempty"`
}

type LocationDetail struct {
	Address *Address `json:"address,omitempty"`
}

type Address struct {
	AddressLine1 string `json:"addressLine1,omitempty"`
	City         string `json:"city,omitempty"`
	StateOrProvince string `json:"stateOrProvince,omitempty"`
	PostalCode   string `json:"postalCode,omitempty"`
	Country      string `json:"country,omitempty"` // ISO 3166-1 alpha-2
}

type LocationsPage struct {
	Total     int                 `json:"total"`
	Locations []InventoryLocation `json:"locations"`
}

// GetInventoryLocations returns the seller's inventory locations
func (c *Client) GetInventoryLocations() (*LocationsPage, error) {
	body, err := c.get("/sell/inventory/v1/location?limit=100&offset=0")
	if err != nil {
		return nil, err
	}
	var page LocationsPage
	json.Unmarshal(body, &page)
	return &page, nil
}

// ============================================================================
// ACCOUNT API - BUSINESS POLICIES
// ============================================================================

// FulfillmentPolicy represents a shipping policy
type FulfillmentPolicy struct {
	FulfillmentPolicyID string `json:"fulfillmentPolicyId"`
	Name                string `json:"name"`
	MarketplaceID       string `json:"marketplaceId"`
}

// PaymentPolicy represents a payment policy
type PaymentPolicy struct {
	PaymentPolicyID string `json:"paymentPolicyId"`
	Name            string `json:"name"`
	MarketplaceID   string `json:"marketplaceId"`
}

// ReturnPolicy represents a return policy
type ReturnPolicy struct {
	ReturnPolicyID string `json:"returnPolicyId"`
	Name           string `json:"name"`
	MarketplaceID  string `json:"marketplaceId"`
}

// GetFulfillmentPolicies returns the seller's shipping policies for a marketplace
func (c *Client) GetFulfillmentPolicies(marketplaceID string) ([]FulfillmentPolicy, error) {
	path := fmt.Sprintf("/sell/account/v1/fulfillment_policy?marketplace_id=%s", marketplaceID)
	body, err := c.get(path)
	if err != nil {
		return nil, err
	}
	var result struct {
		FulfillmentPolicies []FulfillmentPolicy `json:"fulfillmentPolicies"`
	}
	json.Unmarshal(body, &result)
	return result.FulfillmentPolicies, nil
}

// GetPaymentPolicies returns payment policies for a marketplace
func (c *Client) GetPaymentPolicies(marketplaceID string) ([]PaymentPolicy, error) {
	path := fmt.Sprintf("/sell/account/v1/payment_policy?marketplace_id=%s", marketplaceID)
	body, err := c.get(path)
	if err != nil {
		return nil, err
	}
	var result struct {
		PaymentPolicies []PaymentPolicy `json:"paymentPolicies"`
	}
	json.Unmarshal(body, &result)
	return result.PaymentPolicies, nil
}

// GetReturnPolicies returns return policies for a marketplace
func (c *Client) GetReturnPolicies(marketplaceID string) ([]ReturnPolicy, error) {
	path := fmt.Sprintf("/sell/account/v1/return_policy?marketplace_id=%s", marketplaceID)
	body, err := c.get(path)
	if err != nil {
		return nil, err
	}
	var result struct {
		ReturnPolicies []ReturnPolicy `json:"returnPolicies"`
	}
	json.Unmarshal(body, &result)
	return result.ReturnPolicies, nil
}

// ============================================================================
// TAXONOMY API - CATEGORIES
// ============================================================================

// GetCategorySuggestions returns category suggestions for a search term
func (c *Client) GetCategorySuggestions(marketplaceID, query string) ([]CategorySuggestion, error) {
	path := fmt.Sprintf("/commerce/taxonomy/v1/category_tree/0/get_category_suggestions?q=%s",
		url.QueryEscape(query))

	// Marketplace-specific category trees
	treeID := getTreeID(marketplaceID)
	if treeID != "" {
		path = fmt.Sprintf("/commerce/taxonomy/v1/category_tree/%s/get_category_suggestions?q=%s",
			treeID, url.QueryEscape(query))
	}

	body, err := c.get(path)
	if err != nil {
		return nil, err
	}

	var result struct {
		CategorySuggestions []CategorySuggestion `json:"categorySuggestions"`
	}
	json.Unmarshal(body, &result)
	return result.CategorySuggestions, nil
}

type CategorySuggestion struct {
	Category         CategoryRef `json:"category"`
	CategoryTreeNodeLevel int    `json:"categoryTreeNodeLevel"`
	Relevancy        string     `json:"relevancy"`
}

type CategoryRef struct {
	CategoryID   string `json:"categoryId"`
	CategoryName string `json:"categoryName"`
}

func getTreeID(marketplaceID string) string {
	trees := map[string]string{
		"EBAY_GB": "3",
		"EBAY_US": "0",
		"EBAY_DE": "77",
		"EBAY_AU": "15",
		"EBAY_CA": "2",
		"EBAY_FR": "71",
		"EBAY_IT": "101",
		"EBAY_ES": "186",
	}
	return trees[marketplaceID]
}

// ============================================================================
// CATALOG API - PRODUCT SEARCH
// ============================================================================

// CatalogProduct represents a product from the eBay Catalog Product Search API
type CatalogProduct struct {
	EPID          string   `json:"epid"`
	Title         string   `json:"title"`
	ImageURL      string   `json:"imageUrl,omitempty"`
	GTINs         []string `json:"gtins,omitempty"`
	Brand         string   `json:"brand,omitempty"`
	CategoryName  string   `json:"categoryName,omitempty"`
}

// CatalogSearch searches the eBay product catalogue by keyword or GTIN.
// Uses the sell/catalog/v1_beta/product_summary/search endpoint.
// Returns at most 10 results. Gracefully returns an empty slice on API errors
// (e.g. when the seller's OAuth scope does not include catalog access).
func (c *Client) CatalogSearch(query, gtin, marketplaceID string) ([]CatalogProduct, error) {
	if marketplaceID == "" {
		marketplaceID = "EBAY_GB"
	}

	params := url.Values{}
	params.Set("marketplace_id", marketplaceID)
	params.Set("limit", "10")
	if gtin != "" {
		params.Set("gtin", gtin)
	} else if query != "" {
		params.Set("q", query)
	} else {
		return []CatalogProduct{}, nil
	}

	path := "/sell/catalog/v1_beta/product_summary/search?" + params.Encode()
	log.Printf("[eBay Client] CatalogSearch: %s%s", c.APIRoot, path)

	body, err := c.get(path)
	if err != nil {
		log.Printf("[eBay Client] CatalogSearch failed (non-fatal): %v", err)
		// Return empty rather than an error — catalog access is optional
		return []CatalogProduct{}, nil
	}

	var resp struct {
		ProductSummaries []struct {
			EPID   string `json:"epid"`
			Title  string `json:"title"`
			Image  struct {
				ImageURL string `json:"imageUrl"`
			} `json:"image"`
			GTINs      []string `json:"gtins"`
			Brand      string   `json:"brand"`
			Categories []struct {
				CategoryName string `json:"categoryName"`
			} `json:"categories"`
		} `json:"productSummaries"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		log.Printf("[eBay Client] CatalogSearch parse error (non-fatal): %v", err)
		return []CatalogProduct{}, nil
	}

	products := make([]CatalogProduct, 0, len(resp.ProductSummaries))
	for _, ps := range resp.ProductSummaries {
		p := CatalogProduct{
			EPID:     ps.EPID,
			Title:    ps.Title,
			ImageURL: ps.Image.ImageURL,
			GTINs:    ps.GTINs,
			Brand:    ps.Brand,
		}
		if len(ps.Categories) > 0 {
			p.CategoryName = ps.Categories[0].CategoryName
		}
		products = append(products, p)
	}
	return products, nil
}

// ============================================================================
// INVENTORY ITEM GROUP — variation listings (SESSION H)
// ============================================================================
// An InventoryItemGroup groups multiple inventory item SKUs under one parent
// listing. The group is published via a single offer whose inventoryItemGroupKey
// references the group. Each child SKU must be created as an inventory item
// first (via CreateOrReplaceInventoryItem), then the group is created/replaced,
// and finally one offer is created for the group.
// ============================================================================

// InventoryItemGroup represents an eBay variation group.
type InventoryItemGroup struct {
	InventoryItemGroupKey string              `json:"inventoryItemGroupKey"`
	Title                 string              `json:"title"`
	Description           string              `json:"description,omitempty"`
	Aspects               map[string][]string `json:"aspects,omitempty"`    // shared item specifics
	VariesBy              *VariesBy           `json:"variesBy,omitempty"`
	ImageUrls             []string            `json:"imageUrls,omitempty"`
}

// VariesBy defines which aspects differentiate the variants (e.g. Color, Size).
type VariesBy struct {
	AspectNameValues  []AspectNameValue `json:"aspectsImageVariesBy,omitempty"` // aspects whose images differ
	Specifications    []VariationSpec   `json:"specifications,omitempty"`
}

// AspectNameValue is an aspect name with all its possible values.
type AspectNameValue struct {
	LocalizedAspectName string   `json:"localizedAspectName"`
	Values              []string `json:"values"`
}

// VariationSpec maps an aspect name to all its values across the variant set.
type VariationSpec struct {
	Name   string   `json:"name"`
	Values []string `json:"values"`
}

// CreateOrReplaceInventoryItemGroup creates or updates an inventory item group.
// PUT /sell/inventory/v1/inventory_item_group/{inventoryItemGroupKey}
func (c *Client) CreateOrReplaceInventoryItemGroup(groupKey string, group *InventoryItemGroup) error {
	path := fmt.Sprintf("/sell/inventory/v1/inventory_item_group/%s", groupKey)
	body, statusCode, err := c.doWithStatus("PUT", path, group)
	if err != nil {
		return fmt.Errorf("create/replace inventory item group: %w", err)
	}
	if statusCode >= 400 {
		return fmt.Errorf("inventory item group API error %d: %s", statusCode, truncate(string(body), 400))
	}
	return nil
}

// CreateGroupOffer creates an offer for an inventory item group (variation listing).
// The offer payload mirrors a regular offer but uses inventoryItemGroupKey instead of sku.
type GroupOffer struct {
	MarketplaceID          string                 `json:"marketplaceId"`
	Format                 string                 `json:"format"` // FIXED_PRICE
	ListingDescription     string                 `json:"listingDescription,omitempty"`
	AvailableQuantity      int                    `json:"availableQuantity,omitempty"`
	CategoryID             string                 `json:"categoryId,omitempty"`
	ListingPolicies        *ListingPolicies       `json:"listingPolicies,omitempty"`
	MerchantLocationKey    string                 `json:"merchantLocationKey,omitempty"`
	PricingSummary         *PricingSummary        `json:"pricingSummary,omitempty"`
	InventoryItemGroupKey  string                 `json:"inventoryItemGroupKey"`
}

// CreateGroupOffer creates an offer for a variation group and returns the offerID.
// POST /sell/inventory/v1/offer
func (c *Client) CreateGroupOffer(offer *GroupOffer) (string, error) {
	path := "/sell/inventory/v1/offer"
	body, statusCode, err := c.doWithStatus("POST", path, offer)
	if err != nil {
		return "", fmt.Errorf("create group offer: %w", err)
	}
	if statusCode >= 400 {
		return "", fmt.Errorf("create group offer API error %d: %s", statusCode, truncate(string(body), 400))
	}
	var resp struct {
		OfferID string `json:"offerId"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("unmarshal create group offer response: %w", err)
	}
	return resp.OfferID, nil
}

// doWithStatus performs an authenticated HTTP request and returns (body, statusCode, error).
// Unlike the private do() method, it exposes the HTTP status code so callers can
// distinguish 4xx from transport errors.
func (c *Client) doWithStatus(method, path string, body interface{}) ([]byte, int, error) {
	fullURL := c.APIRoot + path
	log.Printf("[eBay Client] %s %s", method, fullURL)

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
	req.Header.Set("Authorization", "Bearer "+c.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Language", "en-US")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("http do: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response body: %w", err)
	}
	return respBody, resp.StatusCode, nil
}

// ============================================================================
// UTILITY
// ============================================================================

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
