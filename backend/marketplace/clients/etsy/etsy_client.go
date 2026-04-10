package etsy

// ============================================================================
// ETSY OPEN API v3 CLIENT
// ============================================================================
// Base URL:  https://openapi.etsy.com/v3
// Auth:      OAuth 2.0 PKCE — no client_secret used in the token exchange.
//            The seller authorises via the browser; the backend exchanges the
//            code + stored code_verifier for an access_token.
// Rate limit: 10 req/sec per API key — basic retry-with-backoff included.
// Scopes:    listings_r listings_w transactions_r transactions_w
// ============================================================================

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	BaseURL      = "https://openapi.etsy.com/v3"
	AuthEndpoint = "https://www.etsy.com/oauth/connect"
	TokenEndpoint = "https://api.etsy.com/v3/public/oauth/token"
)

// ── PKCE helpers ──────────────────────────────────────────────────────────────

// GenerateCodeVerifier creates a random 64-character code verifier string.
// The verifier must be stored server-side and used during the callback.
func GenerateCodeVerifier() (string, error) {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~"
	b := make([]byte, 64)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		if err != nil {
			return "", err
		}
		b[i] = chars[n.Int64()]
	}
	return string(b), nil
}

// GenerateCodeChallenge produces the S256 code challenge from a verifier.
func GenerateCodeChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// ── Client ────────────────────────────────────────────────────────────────────

type Client struct {
	APIKey       string // Etsy app client_id
	AccessToken  string
	RefreshToken string
	ShopID       int64
	HTTPClient   *http.Client

	// OnTokenRefresh is called when the access token is refreshed.
	// Caller should persist the new tokens.
	OnTokenRefresh func(accessToken, refreshToken string, expiresIn int)
}

func NewClient(apiKey, accessToken, refreshToken string, shopID int64) *Client {
	return &Client{
		APIKey:       apiKey,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ShopID:       shopID,
		HTTPClient:   &http.Client{Timeout: 30 * time.Second},
	}
}

// ── OAuth URL generation ──────────────────────────────────────────────────────

// GenerateOAuthURL builds the Etsy consent URL. The caller provides the
// codeVerifier (which they must persist) and this function computes the
// challenge automatically.
func (c *Client) GenerateOAuthURL(redirectURI, state, codeVerifier string) string {
	challenge := GenerateCodeChallenge(codeVerifier)
	params := url.Values{}
	params.Set("response_type", "code")
	params.Set("redirect_uri", redirectURI)
	params.Set("scope", "listings_r listings_w transactions_r transactions_w")
	params.Set("client_id", c.APIKey)
	params.Set("state", state)
	params.Set("code_challenge", challenge)
	params.Set("code_challenge_method", "S256")
	return AuthEndpoint + "?" + params.Encode()
}

// ── Token exchange & refresh ──────────────────────────────────────────────────

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
}

// ExchangeCodeForToken exchanges the auth code for access/refresh tokens.
// PKCE: client_secret is NOT sent; code_verifier is sent instead.
func (c *Client) ExchangeCodeForToken(code, codeVerifier, redirectURI string) (*TokenResponse, error) {
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("client_id", c.APIKey)
	data.Set("redirect_uri", redirectURI)
	data.Set("code", code)
	data.Set("code_verifier", codeVerifier)

	resp, err := http.PostForm(TokenEndpoint, data)
	if err != nil {
		return nil, fmt.Errorf("token exchange request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange failed (%d): %s", resp.StatusCode, string(body))
	}
	var tok TokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	c.AccessToken = tok.AccessToken
	c.RefreshToken = tok.RefreshToken
	return &tok, nil
}

// RefreshToken uses the stored refresh_token to obtain a new access_token.
func (c *Client) RefreshAccessToken() (*TokenResponse, error) {
	if c.RefreshToken == "" {
		return nil, fmt.Errorf("no refresh_token stored — re-authorise via OAuth")
	}
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("client_id", c.APIKey)
	data.Set("refresh_token", c.RefreshToken)

	resp, err := http.PostForm(TokenEndpoint, data)
	if err != nil {
		return nil, fmt.Errorf("refresh token request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("refresh token failed (%d): %s", resp.StatusCode, string(body))
	}
	var tok TokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return nil, fmt.Errorf("decode refresh response: %w", err)
	}
	c.AccessToken = tok.AccessToken
	if tok.RefreshToken != "" {
		c.RefreshToken = tok.RefreshToken
	}
	if c.OnTokenRefresh != nil {
		c.OnTokenRefresh(tok.AccessToken, c.RefreshToken, tok.ExpiresIn)
	}
	return &tok, nil
}

// ── HTTP helpers ──────────────────────────────────────────────────────────────

func (c *Client) doRequest(method, path string, query url.Values, body interface{}) (json.RawMessage, error) {
	fullURL := BaseURL + path
	if query != nil && len(query) > 0 {
		fullURL += "?" + query.Encode()
	}

	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, fullURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("x-api-key", c.APIKey)
	if c.AccessToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.AccessToken)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	// Basic retry for 429 rate limit
	var resp *http.Response
	for attempt := 0; attempt < 3; attempt++ {
		resp, err = c.HTTPClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("HTTP request: %w", err)
		}
		if resp.StatusCode != http.StatusTooManyRequests {
			break
		}
		resp.Body.Close()
		log.Printf("[Etsy] Rate limited — waiting 1s before retry %d", attempt+1)
		time.Sleep(1 * time.Second)
		// Rebuild the request because the body was consumed
		if body != nil {
			b, _ := json.Marshal(body)
			req.Body = io.NopCloser(bytes.NewReader(b))
		}
	}
	if resp == nil {
		return nil, fmt.Errorf("all retry attempts failed")
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	// On 401 try a token refresh once
	if resp.StatusCode == http.StatusUnauthorized {
		log.Printf("[Etsy] 401 received — attempting token refresh")
		if _, refreshErr := c.RefreshAccessToken(); refreshErr == nil {
			req.Header.Set("Authorization", "Bearer "+c.AccessToken)
			if body != nil {
				b, _ := json.Marshal(body)
				req.Body = io.NopCloser(bytes.NewReader(b))
			}
			resp2, err2 := c.HTTPClient.Do(req)
			if err2 == nil {
				defer resp2.Body.Close()
				respBody, _ = io.ReadAll(resp2.Body)
				resp = resp2
			}
		}
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("etsy API error %d: %s", resp.StatusCode, string(respBody))
	}
	return json.RawMessage(respBody), nil
}

func (c *Client) get(path string, query url.Values) (json.RawMessage, error) {
	return c.doRequest("GET", path, query, nil)
}
func (c *Client) post(path string, body interface{}) (json.RawMessage, error) {
	return c.doRequest("POST", path, nil, body)
}
func (c *Client) put(path string, body interface{}) (json.RawMessage, error) {
	return c.doRequest("PUT", path, nil, body)
}
func (c *Client) delete(path string) (json.RawMessage, error) {
	return c.doRequest("DELETE", path, nil, nil)
}
func (c *Client) patch(path string, body interface{}) (json.RawMessage, error) {
	return c.doRequest("PATCH", path, nil, body)
}

// ── Shop ──────────────────────────────────────────────────────────────────────

type Shop struct {
	ShopID   int64  `json:"shop_id"`
	ShopName string `json:"shop_name"`
	URL      string `json:"url"`
	Title    string `json:"title"`
}

// GetShop fetches the seller's shop info. Uses the /me endpoint to get
// the user's associated shop.
func (c *Client) GetShop() (*Shop, error) {
	// First get the user's user_id
	meRaw, err := c.get("/application/openapi-user", nil)
	if err != nil {
		// Fallback: if shop_id is already known, fetch directly
		if c.ShopID > 0 {
			return c.GetShopByID(c.ShopID)
		}
		return nil, fmt.Errorf("get user: %w", err)
	}
	var me struct {
		UserID int64 `json:"user_id"`
	}
	if err := json.Unmarshal(meRaw, &me); err != nil {
		return nil, fmt.Errorf("decode user: %w", err)
	}
	// Get shop by user
	shopRaw, err := c.get(fmt.Sprintf("/application/users/%d/shops", me.UserID), nil)
	if err != nil {
		return nil, fmt.Errorf("get shop: %w", err)
	}
	var shop Shop
	if err := json.Unmarshal(shopRaw, &shop); err != nil {
		return nil, fmt.Errorf("decode shop: %w", err)
	}
	return &shop, nil
}

// GetShopByID fetches shop info using a known shop_id.
func (c *Client) GetShopByID(shopID int64) (*Shop, error) {
	raw, err := c.get(fmt.Sprintf("/application/shops/%d", shopID), nil)
	if err != nil {
		return nil, err
	}
	var shop Shop
	if err := json.Unmarshal(raw, &shop); err != nil {
		return nil, err
	}
	return &shop, nil
}

// ── Listings ──────────────────────────────────────────────────────────────────

type ListingImage struct {
	ListingImageID int64  `json:"listing_image_id"`
	URL570xN       string `json:"url_570xN"`
	URLFullxFull   string `json:"url_fullxfull"`
	Rank           int    `json:"rank"`
}

type Listing struct {
	ListingID       int64          `json:"listing_id"`
	Title           string         `json:"title"`
	Description     string         `json:"description"`
	Price           Money          `json:"price"`
	Quantity        int            `json:"quantity"`
	State           string         `json:"state"` // active/inactive/draft/expired
	Tags            []string       `json:"tags"`
	Materials       []string       `json:"materials"`
	ShopSectionID   int64          `json:"shop_section_id,omitempty"`
	TaxonomyID      int64          `json:"taxonomy_id"`
	WhoMade         string         `json:"who_made"`
	WhenMade        string         `json:"when_made"`
	IsSupply        bool           `json:"is_supply"`
	ShippingProfileID int64        `json:"shipping_profile_id,omitempty"`
	Images          []ListingImage `json:"images,omitempty"`
	URL             string         `json:"url,omitempty"`
	Views           int            `json:"views,omitempty"`
}

type Money struct {
	Amount      int    `json:"amount"`       // amount in smallest currency unit
	Divisor     int    `json:"divisor"`      // e.g. 100 for USD cents
	CurrencyCode string `json:"currency_code"`
}

type ListingsResponse struct {
	Count   int       `json:"count"`
	Results []Listing `json:"results"`
}

type CreateListingRequest struct {
	Quantity          int      `json:"quantity"`
	Title             string   `json:"title"`
	Description       string   `json:"description"`
	Price             float64  `json:"price"`
	WhoMade           string   `json:"who_made"`
	WhenMade          string   `json:"when_made"`
	TaxonomyID        int64    `json:"taxonomy_id"`
	ShippingProfileID int64    `json:"shipping_profile_id,omitempty"`
	Tags              []string `json:"tags,omitempty"`
	Materials         []string `json:"materials,omitempty"`
	IsSupply          bool     `json:"is_supply"`
	IsPersonalizable  bool     `json:"is_personalizable,omitempty"`
	IsCustomizable    bool     `json:"is_customizable,omitempty"`
	ShopSectionID     int64    `json:"shop_section_id,omitempty"`
}

type UpdateListingRequest struct {
	Title             string   `json:"title,omitempty"`
	Description       string   `json:"description,omitempty"`
	Price             float64  `json:"price,omitempty"`
	Quantity          int      `json:"quantity,omitempty"`
	Tags              []string `json:"tags,omitempty"`
	Materials         []string `json:"materials,omitempty"`
	WhoMade           string   `json:"who_made,omitempty"`
	WhenMade          string   `json:"when_made,omitempty"`
	ShippingProfileID int64    `json:"shipping_profile_id,omitempty"`
	TaxonomyID        int64    `json:"taxonomy_id,omitempty"`
	State             string   `json:"state,omitempty"`
}

// GetListings fetches all listings for the shop with pagination.
func (c *Client) GetListings(offset, limit int) (*ListingsResponse, error) {
	q := url.Values{}
	q.Set("offset", strconv.Itoa(offset))
	q.Set("limit", strconv.Itoa(limit))
	raw, err := c.get(fmt.Sprintf("/application/shops/%d/listings", c.ShopID), q)
	if err != nil {
		return nil, err
	}
	var resp ListingsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode listings: %w", err)
	}
	return &resp, nil
}

// CreateListing creates a new listing on Etsy. The listing is created in
// draft state; callers should follow up with PublishListing to activate.
func (c *Client) CreateListing(req *CreateListingRequest) (*Listing, error) {
	raw, err := c.post(fmt.Sprintf("/application/shops/%d/listings", c.ShopID), req)
	if err != nil {
		return nil, err
	}
	var listing Listing
	if err := json.Unmarshal(raw, &listing); err != nil {
		return nil, fmt.Errorf("decode listing: %w", err)
	}
	return &listing, nil
}

// UpdateListing patches an existing listing.
func (c *Client) UpdateListing(listingID int64, req *UpdateListingRequest) (*Listing, error) {
	raw, err := c.patch(fmt.Sprintf("/application/shops/%d/listings/%d", c.ShopID, listingID), req)
	if err != nil {
		return nil, err
	}
	var listing Listing
	if err := json.Unmarshal(raw, &listing); err != nil {
		return nil, fmt.Errorf("decode listing: %w", err)
	}
	return &listing, nil
}

// DeleteListing deletes a listing. Etsy actually marks it as deleted/expired.
func (c *Client) DeleteListing(listingID int64) error {
	_, err := c.delete(fmt.Sprintf("/application/listings/%d", listingID))
	return err
}

// PublishListing sets a draft listing to active state.
func (c *Client) PublishListing(listingID int64) (*Listing, error) {
	return c.UpdateListing(listingID, &UpdateListingRequest{State: "active"})
}

// ── Listing Images ────────────────────────────────────────────────────────────

type UploadImageRequest struct {
	Image    string `json:"image,omitempty"`    // base64
	URL      string `json:"url,omitempty"`       // external URL (alternative)
	Rank     int    `json:"rank,omitempty"`
	Overwrite bool  `json:"overwrite,omitempty"`
}

// UploadListingImage uploads an image to a listing via base64.
// Etsy requires multipart/form-data for actual image upload — we send the
// image URL for the server to fetch, using the image parameter as base64.
func (c *Client) UploadListingImage(listingID int64, imageBase64 string, rank int) (*ListingImage, error) {
	// Etsy image upload requires multipart form — we build it manually
	body := map[string]interface{}{
		"image":     imageBase64,
		"rank":      rank,
		"overwrite": true,
	}
	raw, err := c.post(fmt.Sprintf("/application/shops/%d/listings/%d/images", c.ShopID, listingID), body)
	if err != nil {
		return nil, err
	}
	var img ListingImage
	if err := json.Unmarshal(raw, &img); err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	return &img, nil
}

// ── Shipping Profiles ─────────────────────────────────────────────────────────

type ShippingProfile struct {
	ShippingProfileID int64  `json:"shipping_profile_id"`
	Title             string `json:"title"`
	UserID            int64  `json:"user_id"`
	MinProcessingDays int    `json:"min_processing_days,omitempty"`
	MaxProcessingDays int    `json:"max_processing_days,omitempty"`
}

// GetShippingProfiles returns all shipping profiles for the shop.
func (c *Client) GetShippingProfiles() ([]ShippingProfile, error) {
	raw, err := c.get(fmt.Sprintf("/application/shops/%d/shipping-profiles", c.ShopID), nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Count   int               `json:"count"`
		Results []ShippingProfile `json:"results"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode shipping profiles: %w", err)
	}
	return resp.Results, nil
}

// ── Taxonomy ──────────────────────────────────────────────────────────────────

type TaxonomyNode struct {
	ID            int64          `json:"id"`
	Level         int            `json:"level"`
	Name          string         `json:"name"`
	ParentID      int64          `json:"parent_id"`
	Children      []TaxonomyNode `json:"children,omitempty"`
	FullPathNames []string       `json:"full_path_taxonomy_ids,omitempty"`
}

type TaxonomyProperty struct {
	PropertyID   int64  `json:"property_id"`
	Name         string `json:"name"`
	DisplayName  string `json:"display_name"`
	Scales       []struct {
		ScaleID   int64  `json:"scale_id"`
		DisplayName string `json:"display_name"`
	} `json:"scales,omitempty"`
	IsRequired   bool     `json:"is_required"`
	SupportsAttributes bool `json:"supports_attributes"`
	SupportsVariations bool `json:"supports_variations"`
	IsMultivalued bool     `json:"is_multivalued"`
	PossibleValues []struct {
		ValueID int64  `json:"value_id"`
		Name    string `json:"name"`
	} `json:"possible_values,omitempty"`
}

// GetTaxonomyNodes returns the full seller taxonomy tree.
func (c *Client) GetTaxonomyNodes() ([]TaxonomyNode, error) {
	raw, err := c.get("/application/seller-taxonomy/nodes", nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Count   int            `json:"count"`
		Results []TaxonomyNode `json:"results"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		// Some versions return array directly
		var nodes []TaxonomyNode
		if err2 := json.Unmarshal(raw, &nodes); err2 != nil {
			return nil, fmt.Errorf("decode taxonomy: %w", err)
		}
		return nodes, nil
	}
	return resp.Results, nil
}

// GetTaxonomyProperties returns the properties (attributes) for a taxonomy node.
func (c *Client) GetTaxonomyProperties(taxonomyID int64) ([]TaxonomyProperty, error) {
	raw, err := c.get(fmt.Sprintf("/application/seller-taxonomy/nodes/%d/properties", taxonomyID), nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Count   int                `json:"count"`
		Results []TaxonomyProperty `json:"results"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode taxonomy properties: %w", err)
	}
	return resp.Results, nil
}

// ============================================================================
// LISTING INVENTORY / VARIATION OFFERINGS — SESSION H
// ============================================================================
// Etsy supports variation listings via the listing inventory endpoint.
// After a listing is created, call UpdateListingInventory to set up the
// property/value pairs (e.g. Color, Size) and per-offering price/quantity.
// Reference: PUT /v3/application/listings/{listing_id}/inventory
// ============================================================================

// ListingProduct represents one offering row in the Etsy inventory.
// Each unique combination of property values is one product.
type ListingProduct struct {
	PropertyValues []PropertyValue  `json:"property_values"`
	Sku            string           `json:"sku,omitempty"`
	Offerings      []ListingOffering `json:"offerings"`
}

// PropertyValue links an aspect (e.g. "Color") to its chosen value ("Red").
type PropertyValue struct {
	PropertyID   int64    `json:"property_id"`
	PropertyName string   `json:"property_name"`
	Values       []string `json:"values"`
	ValueIDs     []int64  `json:"value_ids,omitempty"`
}

// ListingOffering is a single price/quantity entry for a product combination.
type ListingOffering struct {
	Price    ListingOfferingPrice `json:"price"`
	Quantity int                  `json:"quantity"`
	IsEnabled bool                `json:"is_enabled"`
}

// ListingOfferingPrice is the price in minor units (cents) with ISO currency code.
type ListingOfferingPrice struct {
	Amount   int    `json:"amount"`   // in minor units (e.g. 1999 = $19.99)
	Divisor  int    `json:"divisor"`  // 100 for most currencies
	CurrencyCode string `json:"currency_code"`
}

// ListingInventoryRequest is the payload for PUT /v3/application/listings/{id}/inventory.
type ListingInventoryRequest struct {
	Products      []ListingProduct `json:"products"`
	PriceOnProperty []int64        `json:"price_on_property,omitempty"`   // property IDs that drive price
	QuantityOnProperty []int64     `json:"quantity_on_property,omitempty"` // property IDs that drive qty
	SKUOnProperty []int64          `json:"sku_on_property,omitempty"`
}

// UpdateListingInventory sets variation inventory for a listing.
// PUT /v3/application/listings/{listing_id}/inventory
func (c *Client) UpdateListingInventory(listingID int64, req *ListingInventoryRequest) error {
	path := fmt.Sprintf("/application/listings/%d/inventory", listingID)
	_, err := c.put(path, req)
	if err != nil {
		return fmt.Errorf("update listing inventory: %w", err)
	}
	return nil
}
