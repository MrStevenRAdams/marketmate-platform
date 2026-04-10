package ebay

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// ============================================================================
// EBAY CLIENT — LISTING EXTENSIONS
// ============================================================================
// Additional client methods needed for listing creation flow:
//   - GetItemAspectsForCategory — Taxonomy API (required/recommended aspects)
//   - UpdateOffer              — PUT /sell/inventory/v1/offer/{offerId}
//   - GetOffer                 — GET /sell/inventory/v1/offer/{offerId}
//   - doRequestWithHeaders     — Supports marketplace-specific headers
//   - GetConditionPolicies     — Condition values per category
//   - GetCategoryTree          — Get full category tree for a marketplace
// ============================================================================

// doRequestWithHeaders is like doRequest but allows extra headers (e.g. X-EBAY-C-MARKETPLACE-ID)
func (c *Client) doRequestWithHeaders(method, path string, body io.Reader, headers map[string]string) ([]byte, int, error) {
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

	for k, v := range headers {
		req.Header.Set(k, v)
	}

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

// getWithMarketplace performs a GET with X-EBAY-C-MARKETPLACE-ID header
func (c *Client) getWithMarketplace(path, marketplaceID string) ([]byte, error) {
	headers := map[string]string{}
	if marketplaceID != "" {
		headers["X-EBAY-C-MARKETPLACE-ID"] = marketplaceID
	}
	body, status, err := c.doRequestWithHeaders("GET", path, nil, headers)
	if err != nil {
		return nil, err
	}
	if status >= 400 {
		return nil, fmt.Errorf("eBay API error (HTTP %d): %s", status, truncate(string(body), 500))
	}
	return body, nil
}

// ============================================================================
// TAXONOMY API — ITEM ASPECTS
// ============================================================================

// ItemAspectConstraint describes whether an aspect is required, single/multi, free-text/selection
type ItemAspectConstraint struct {
	AspectRequired          bool   `json:"aspectRequired"`
	AspectMode              string `json:"aspectMode"`              // FREE_TEXT, SELECTION_ONLY
	AspectUsage             string `json:"aspectUsage"`             // RECOMMENDED, OPTIONAL
	ItemToAspectCardinality string `json:"itemToAspectCardinality"` // SINGLE, MULTI
	ExpectedRequiredByDate  string `json:"expectedRequiredByDate,omitempty"`
}

// AspectValue is a single allowed value for an item aspect
type AspectValue struct {
	LocalizedValue string `json:"localizedValue"`
}

// ItemAspect is a single item specific (aspect) for a category
type ItemAspect struct {
	LocalizedAspectName string               `json:"localizedAspectName"`
	AspectConstraint    ItemAspectConstraint  `json:"aspectConstraint"`
	AspectValues        []AspectValue         `json:"aspectValues,omitempty"`
}

// GetItemAspectsForCategory returns the required/recommended item specifics for an eBay category
func (c *Client) GetItemAspectsForCategory(marketplaceID, categoryID string) ([]ItemAspect, error) {
	treeID := getTreeID(marketplaceID)
	if treeID == "" {
		treeID = "0" // default to US
	}

	path := fmt.Sprintf("/commerce/taxonomy/v1/category_tree/%s/get_item_aspects_for_category?category_id=%s",
		treeID, url.QueryEscape(categoryID))

	body, err := c.getWithMarketplace(path, marketplaceID)
	if err != nil {
		return nil, err
	}

	var result struct {
		Aspects []ItemAspect `json:"aspects"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse item aspects: %w", err)
	}

	return result.Aspects, nil
}

// ============================================================================
// TAXONOMY API — CATEGORY TREE
// ============================================================================

// CategoryTreeResponse is the full category tree for a marketplace
type CategoryTreeResponse struct {
	CategoryTreeID      string           `json:"categoryTreeId"`
	CategoryTreeVersion string           `json:"categoryTreeVersion"`
	RootNode            CategoryTreeNode `json:"rootCategoryNode"`
}

// CategoryTreeNode represents a node in the category tree (hierarchical)
type CategoryTreeNode struct {
	CategoryID   string             `json:"categoryId"`
	CategoryName string             `json:"categoryName"`
	LeafCategory bool               `json:"leafCategoryTreeNode"`
	ChildNodes   []CategoryTreeNode `json:"childCategoryTreeNodes,omitempty"`
}

// GetCategoryTree fetches the complete category tree for a marketplace
// This returns ALL categories in a hierarchical structure
func (c *Client) GetCategoryTree(marketplaceID string) (*CategoryTreeResponse, error) {
	treeID := getTreeID(marketplaceID)
	if treeID == "" {
		treeID = "0" // default to US
	}

	path := fmt.Sprintf("/commerce/taxonomy/v1/category_tree/%s", treeID)
	
	body, err := c.getWithMarketplace(path, marketplaceID)
	if err != nil {
		return nil, err
	}

	var tree CategoryTreeResponse
	if err := json.Unmarshal(body, &tree); err != nil {
		return nil, fmt.Errorf("parse category tree: %w", err)
	}

	return &tree, nil
}

// ============================================================================
// INVENTORY API — OFFER EXTENSIONS
// ============================================================================

// UpdateOffer updates an existing offer
func (c *Client) UpdateOffer(offerID string, offer *Offer) error {
	path := fmt.Sprintf("/sell/inventory/v1/offer/%s", offerID)
	jsonBody, err := json.Marshal(offer)
	if err != nil {
		return err
	}
	body, status, err := c.doRequest("PUT", path, strings.NewReader(string(jsonBody)))
	if err != nil {
		return err
	}
	if status != 200 && status != 204 {
		return fmt.Errorf("update offer: HTTP %d: %s", status, truncate(string(body), 500))
	}
	return nil
}

// GetOffer returns a single offer by ID
func (c *Client) GetOffer(offerID string) (*Offer, error) {
	path := fmt.Sprintf("/sell/inventory/v1/offer/%s", offerID)
	body, err := c.get(path)
	if err != nil {
		return nil, err
	}

	var offer Offer
	if err := json.Unmarshal(body, &offer); err != nil {
		return nil, fmt.Errorf("parse offer: %w", err)
	}
	return &offer, nil
}

// CreateOrReplaceInventoryItemFull creates/updates an inventory item with full error detail
func (c *Client) CreateOrReplaceInventoryItemFull(sku string, item *InventoryItem) (string, error) {
	path := fmt.Sprintf("/sell/inventory/v1/inventory_item/%s", url.PathEscape(sku))
	body, status, err := c.put(path, item)
	if err != nil {
		return "", err
	}
	if status != 200 && status != 204 {
		return string(body), fmt.Errorf("create/replace inventory item: HTTP %d: %s", status, truncate(string(body), 500))
	}
	return string(body), nil
}

// ============================================================================
// EXTENDED OFFER FIELDS
// ============================================================================

// ExtendedOffer adds fields not in the basic Offer struct (for full listing creation)
type ExtendedOffer struct {
	OfferID                     string           `json:"offerId,omitempty"`
	SKU                         string           `json:"sku"`
	MarketplaceID               string           `json:"marketplaceId"`
	Format                      string           `json:"format"`
	CategoryID                  string           `json:"categoryId"`
	SecondaryCategoryID         string           `json:"secondaryCategoryId,omitempty"`
	ListingDescription          string           `json:"listingDescription,omitempty"`
	ListingDuration             string           `json:"listingDuration,omitempty"`
	AvailableQuantity           int              `json:"availableQuantity,omitempty"`
	PricingSummary              *PricingSummary   `json:"pricingSummary,omitempty"`
	ListingPolicies             *ListingPolicies  `json:"listingPolicies,omitempty"`
	MerchantLocationKey         string           `json:"merchantLocationKey,omitempty"`
	IncludeCatalogProductDetails bool            `json:"includeCatalogProductDetails,omitempty"`
	HideBuyerDetails            bool             `json:"hideBuyerDetails,omitempty"`
	Subtitle                    string           `json:"subtitle,omitempty"`
	ListingStartDate            string           `json:"listingStartDate,omitempty"`
	LotSize                     int              `json:"lotSize,omitempty"`
	ExtendedProducerResponsibility *EPRInfo      `json:"extendedProducerResponsibility,omitempty"`
	Tax                         *TaxInfo         `json:"tax,omitempty"`
	StoreCategoryNames          []string         `json:"storeCategoryNames,omitempty"`
	BestOfferTerms              *BestOfferTerms  `json:"bestOfferTerms,omitempty"`
	PromotedListings            *PromotedListingSettings `json:"promotedListings,omitempty"` // PRC-04
}

// BestOfferTerms configures Best Offer settings
type BestOfferTerms struct {
	BestOfferEnabled    bool    `json:"bestOfferEnabled"`
	AutoAcceptPrice     *Amount `json:"autoAcceptPrice,omitempty"`
	AutoDeclinePrice    *Amount `json:"autoDeclinePrice,omitempty"`
}

// PromotedListingSettings configures Promoted Listings (PRC-04)
type PromotedListingSettings struct {
	PromotedListingType string `json:"promotedListingType"` // "COST_PER_SALE"
	BidPercentage       string `json:"bidPercentage"`       // e.g. "5.0"
}

// TaxInfo for VAT configuration
type TaxInfo struct {
	VatPercentage float64 `json:"vatPercentage,omitempty"`
	ApplyTax      bool    `json:"applyTax,omitempty"`
}

// EPRInfo for extended producer responsibility
type EPRInfo struct {
	// Stub for future EU compliance
}

// CreateExtendedOffer creates an offer with all extended fields
func (c *Client) CreateExtendedOffer(offer *ExtendedOffer) (string, error) {
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

// UpdateExtendedOffer updates an existing offer with all extended fields
func (c *Client) UpdateExtendedOffer(offerID string, offer *ExtendedOffer) error {
	path := fmt.Sprintf("/sell/inventory/v1/offer/%s", offerID)
	jsonBody, err := json.Marshal(offer)
	if err != nil {
		return err
	}
	body, status, err := c.doRequest("PUT", path, strings.NewReader(string(jsonBody)))
	if err != nil {
		return err
	}
	if status != 200 && status != 204 {
		return fmt.Errorf("update offer: HTTP %d: %s", status, truncate(string(body), 500))
	}
	return nil
}

// ============================================================================
// MARKETPLACE ID HELPERS
// ============================================================================

// MarketplaceCurrency returns the default currency for a marketplace
func MarketplaceCurrency(marketplaceID string) string {
	currencies := map[string]string{
		"EBAY_GB": "GBP",
		"EBAY_US": "USD",
		"EBAY_DE": "EUR",
		"EBAY_FR": "EUR",
		"EBAY_IT": "EUR",
		"EBAY_ES": "EUR",
		"EBAY_AU": "AUD",
		"EBAY_CA": "CAD",
	}
	if c, ok := currencies[marketplaceID]; ok {
		return c
	}
	return "GBP"
}

// GetTreeIDPublic exposes the category tree ID mapping for external use
func GetTreeIDPublic(marketplaceID string) string {
	return getTreeID(marketplaceID)
}

// MarketplaceCountry returns the default country code for a marketplace
func MarketplaceCountry(marketplaceID string) string {
	countries := map[string]string{
		"EBAY_GB": "GB",
		"EBAY_US": "US",
		"EBAY_DE": "DE",
		"EBAY_FR": "FR",
		"EBAY_IT": "IT",
		"EBAY_ES": "ES",
		"EBAY_AU": "AU",
		"EBAY_CA": "CA",
	}
	if c, ok := countries[marketplaceID]; ok {
		return c
	}
	return "GB"
}
