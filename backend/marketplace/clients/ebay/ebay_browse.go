package ebay

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
)

// ============================================================================
// SELLER USERNAME RESOLUTION
// ============================================================================
// Tries multiple strategies in order:
//   1. Cached on Client.SellerUsername (from credentials or previous call)
//   2. Identity API (requires commerce.identity.readonly scope)
//   3. Trading API GetUser (works with base OAuth scope)
// ============================================================================

func (c *Client) getSellerUsername() (string, error) {
	// 1. Already cached
	if c.SellerUsername != "" {
		return c.SellerUsername, nil
	}

	// 2. Try Identity API (REST, needs commerce.identity.readonly scope)
	body, status, err := c.doRequest("GET", "/commerce/identity/v1/user/", nil)
	if err == nil && status < 400 {
		var identity struct {
			Username string `json:"username"`
		}
		if json.Unmarshal(body, &identity) == nil && identity.Username != "" {
			log.Printf("[eBay] Got seller username from Identity API: %s", identity.Username)
			c.SellerUsername = identity.Username
			return identity.Username, nil
		}
	}
	log.Printf("[eBay] Identity API unavailable (status=%d), trying Trading API...", status)

	// 3. Try Trading API GetUser
	username, err := c.TradingGetUser()
	if err == nil && username != "" {
		c.SellerUsername = username
		return username, nil
	}
	log.Printf("[eBay] Trading API GetUser failed: %v", err)

	return "", fmt.Errorf("could not determine seller username — please add 'seller_username' to your eBay credential settings")
}

func (c *Client) GetSellerUsernamePublic() (string, error) {
	return c.getSellerUsername()
}

// ============================================================================
// BROWSE API — TYPES & INDIVIDUAL ITEM LOOKUPS
// ============================================================================
// The Browse API is still used for individual item lookups (e.g. selective
// import by item ID). Bulk import now uses the Trading API instead since
// it returns ALL listings including those with 0 stock.
// ============================================================================

type BrowseItem struct {
	ItemID               string                  `json:"itemId"`
	Title                string                  `json:"title"`
	ShortDescription     string                  `json:"shortDescription,omitempty"`
	Description          string                  `json:"description,omitempty"`
	Price                *BrowsePrice            `json:"price,omitempty"`
	Image                *BrowseImage            `json:"image,omitempty"`
	AdditionalImages     []BrowseImage           `json:"additionalImages,omitempty"`
	Condition            string                  `json:"condition,omitempty"`
	ConditionID          string                  `json:"conditionId,omitempty"`
	ConditionDescription string                  `json:"conditionDescription,omitempty"`
	ItemWebURL           string                  `json:"itemWebUrl,omitempty"`
	Seller               *BrowseSeller           `json:"seller,omitempty"`
	Categories           []BrowseCategory        `json:"categories,omitempty"`
	Brand                string                  `json:"brand,omitempty"`
	MPN                  string                  `json:"mpn,omitempty"`
	EAN                  []string                `json:"ean,omitempty"`
	UPC                  []string                `json:"upc,omitempty"`
	ISBN                 []string                `json:"isbn,omitempty"`
	EpID                 string                  `json:"epid,omitempty"`
	GTIN                 string                  `json:"gtin,omitempty"`
	ItemLocation         *BrowseLocation         `json:"itemLocation,omitempty"`
	BuyingOptions        []string                `json:"buyingOptions,omitempty"`
	LegacyItemID         string                  `json:"legacyItemId,omitempty"`
	Quantity             int                     `json:"quantity,omitempty"`
	QuantitySold         int                     `json:"quantitySold,omitempty"`
	LocalizedAspects     []BrowseLocalizedAspect `json:"localizedAspects,omitempty"`
	CategoryPath         string                  `json:"categoryPath,omitempty"`
	CategoryID           string                  `json:"categoryId,omitempty"`
	SKU                  string                  `json:"sku,omitempty"`
	EstimatedAvailabilities []BrowseAvailability `json:"estimatedAvailabilities,omitempty"`
	Raw                  map[string]interface{}  `json:"-"`
}

type BrowsePrice struct {
	Value    string `json:"value"`
	Currency string `json:"currency"`
}

type BrowseImage struct {
	ImageURL string `json:"imageUrl"`
	Height   int    `json:"height,omitempty"`
	Width    int    `json:"width,omitempty"`
}

type BrowseSeller struct {
	Username           string `json:"username"`
	FeedbackPercentage string `json:"feedbackPercentage,omitempty"`
	FeedbackScore      int    `json:"feedbackScore,omitempty"`
}

type BrowseCategory struct {
	CategoryID   string `json:"categoryId"`
	CategoryName string `json:"categoryName,omitempty"`
}

type BrowseLocation struct {
	City            string `json:"city,omitempty"`
	StateOrProvince string `json:"stateOrProvince,omitempty"`
	PostalCode      string `json:"postalCode,omitempty"`
	Country         string `json:"country,omitempty"`
}

type BrowseLocalizedAspect struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

type BrowseAvailability struct {
	AvailabilityThreshold       int    `json:"availabilityThreshold,omitempty"`
	AvailabilityThresholdType   string `json:"availabilityThresholdType,omitempty"`
	EstimatedAvailabilityStatus string `json:"estimatedAvailabilityStatus,omitempty"`
	EstimatedSoldQuantity       int    `json:"estimatedSoldQuantity,omitempty"`
}

// BrowseGetItem fetches full item details from the Browse API
func (c *Client) BrowseGetItem(itemID string, marketplaceID string) (*BrowseItem, error) {
	path := fmt.Sprintf("/buy/browse/v1/item/%s", url.PathEscape(itemID))

	headers := map[string]string{}
	if marketplaceID != "" {
		headers["X-EBAY-C-MARKETPLACE-ID"] = marketplaceID
	}

	body, status, err := c.doRequestWithHeaders("GET", path, nil, headers)
	if err != nil {
		return nil, err
	}
	if status >= 400 {
		return nil, fmt.Errorf("browse getItem: HTTP %d: %s", status, truncate(string(body), 500))
	}

	var item BrowseItem
	if err := json.Unmarshal(body, &item); err != nil {
		return nil, fmt.Errorf("parse browse item: %w", err)
	}

	var rawMap map[string]interface{}
	json.Unmarshal(body, &rawMap)
	item.Raw = rawMap

	return &item, nil
}

// BrowseGetItemByLegacyID fetches a full item using its traditional eBay item number
func (c *Client) BrowseGetItemByLegacyID(legacyItemID string) (*BrowseItem, error) {
	path := fmt.Sprintf("/buy/browse/v1/item/get_item_by_legacy_id?legacy_item_id=%s",
		url.QueryEscape(legacyItemID))

	body, status, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	if status >= 400 {
		return nil, fmt.Errorf("browse getItemByLegacyId: HTTP %d: %s", status, truncate(string(body), 500))
	}

	var item BrowseItem
	if err := json.Unmarshal(body, &item); err != nil {
		return nil, fmt.Errorf("parse browse item: %w", err)
	}

	var rawMap map[string]interface{}
	json.Unmarshal(body, &rawMap)
	item.Raw = rawMap

	return &item, nil
}

// ============================================================================
// BROWSE API — EAN/GTIN SEARCH & PRODUCT ENRICHMENT
// ============================================================================

// BrowseItemSummary is the condensed item returned by search results
type BrowseItemSummary struct {
	ItemID           string         `json:"itemId"`
	Title            string         `json:"title"`
	Price            *BrowsePrice   `json:"price,omitempty"`
	Image            *BrowseImage   `json:"image,omitempty"`
	Condition        string         `json:"condition,omitempty"`
	ConditionID      string         `json:"conditionId,omitempty"`
	ItemWebURL       string         `json:"itemWebUrl,omitempty"`
	Seller           *BrowseSeller  `json:"seller,omitempty"`
	Categories       []BrowseCategory `json:"categories,omitempty"`
	CategoryPath     string         `json:"categoryPath,omitempty"`
	EpID             string         `json:"epid,omitempty"`
	GTIN             string         `json:"gtin,omitempty"`
	LegacyItemID     string         `json:"legacyItemId,omitempty"`
	QuantitySold     int            `json:"quantitySold,omitempty"`
}

// BrowseSearchResponse is the response from item_summary/search
type BrowseSearchResponse struct {
	Href          string              `json:"href"`
	Total         int                 `json:"total"`
	Next          string              `json:"next,omitempty"`
	Limit         int                 `json:"limit"`
	Offset        int                 `json:"offset"`
	ItemSummaries []BrowseItemSummary `json:"itemSummaries"`
	Warnings      []interface{}       `json:"warnings,omitempty"`
}

// BrowseSearchByGTIN searches for all eBay listings matching an EAN/UPC/GTIN.
// Returns up to maxResults listings across all sellers.
// marketplaceID examples: EBAY_GB, EBAY_US, EBAY_DE
func (c *Client) BrowseSearchByGTIN(gtin string, marketplaceID string, maxResults int) ([]BrowseItemSummary, error) {
	if maxResults <= 0 || maxResults > 200 {
		maxResults = 50
	}
	if marketplaceID == "" {
		marketplaceID = "EBAY_GB"
	}

	params := url.Values{}
	params.Set("gtin", gtin)
	params.Set("limit", fmt.Sprintf("%d", maxResults))
	params.Set("fieldgroups", "MATCHING_ITEMS,EXTENDED")

	path := "/buy/browse/v1/item_summary/search?" + params.Encode()

	headers := map[string]string{
		"X-EBAY-C-MARKETPLACE-ID": marketplaceID,
	}

	body, status, err := c.doRequestWithHeaders("GET", path, nil, headers)
	if err != nil {
		return nil, fmt.Errorf("browse search by GTIN: %w", err)
	}
	if status == 404 || status == 204 {
		return nil, nil // no results
	}
	if status >= 400 {
		return nil, fmt.Errorf("browse search: HTTP %d: %s", status, truncate(string(body), 400))
	}

	var resp BrowseSearchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse browse search response: %w", err)
	}

	log.Printf("[eBay Browse] GTIN=%s marketplace=%s → %d total results, returning %d",
		gtin, marketplaceID, resp.Total, len(resp.ItemSummaries))

	return resp.ItemSummaries, nil
}

// BrowseGetItemsByEPID fetches items grouped by eBay Product ID (epid).
// This returns the canonical eBay catalogue entry shared across all sellers.
func (c *Client) BrowseGetItemsByEPID(epid string, marketplaceID string) (*BrowseItem, error) {
	if marketplaceID == "" {
		marketplaceID = "EBAY_GB"
	}

	path := fmt.Sprintf("/buy/browse/v1/item/get_items_by_item_group?item_group_id=%s",
		url.QueryEscape(epid))

	headers := map[string]string{
		"X-EBAY-C-MARKETPLACE-ID": marketplaceID,
	}

	body, status, err := c.doRequestWithHeaders("GET", path, nil, headers)
	if err != nil {
		return nil, fmt.Errorf("browse getItemsByEPID: %w", err)
	}
	if status >= 400 {
		return nil, fmt.Errorf("browse getItemsByEPID HTTP %d: %s", status, truncate(string(body), 400))
	}

	var item BrowseItem
	if err := json.Unmarshal(body, &item); err != nil {
		return nil, fmt.Errorf("parse browse item group: %w", err)
	}

	return &item, nil
}

