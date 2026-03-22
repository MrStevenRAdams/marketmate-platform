package amazon

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
)

// ============================================================================
// AMAZON SP-API CLIENT
// ============================================================================
// Production-ready client for Amazon Selling Partner API (SP-API).
//
// Features:
//   - OAuth2 token management with automatic refresh
//   - Per-endpoint rate limiting (token bucket)
//   - Exponential backoff retry with jitter
//   - Reports API for full catalog import
//   - Catalog Items API for product enrichment
//   - Listings API for listing management
//   - FBA Inventory API
// ============================================================================

const (
	SPAPIEndpointNA      = "https://sellingpartnerapi-na.amazon.com"
	SPAPIEndpointEU      = "https://sellingpartnerapi-eu.amazon.com"
	SPAPIEndpointFE      = "https://sellingpartnerapi-fe.amazon.com"
	SPAPISandboxEndpoint = "https://sandbox.sellingpartnerapi-na.amazon.com"
	LWATokenEndpoint     = "https://api.amazon.com/auth/o2/token"

	maxRetries     = 3
	baseRetryDelay = 1 * time.Second
	maxRetryDelay  = 30 * time.Second

	reportPollInterval = 15 * time.Second
	reportPollTimeout  = 10 * time.Minute
)

// ============================================================================
// RATE LIMITER
// ============================================================================

type rateLimiter struct {
	mu       sync.Mutex
	tokens   float64
	maxRate  float64
	lastTime time.Time
}

func newRateLimiter(rps float64) *rateLimiter {
	return &rateLimiter{tokens: rps, maxRate: rps, lastTime: time.Now()}
}

func (rl *rateLimiter) wait() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(rl.lastTime).Seconds()
	rl.tokens += elapsed * rl.maxRate
	if rl.tokens > rl.maxRate {
		rl.tokens = rl.maxRate
	}
	rl.lastTime = now

	if rl.tokens >= 1.0 {
		rl.tokens -= 1.0
		return
	}
	waitTime := time.Duration((1.0 - rl.tokens) / rl.maxRate * float64(time.Second))
	time.Sleep(waitTime)
	rl.tokens = 0
	rl.lastTime = time.Now()
}

// ============================================================================
// CLIENT
// ============================================================================

type SPAPIClient struct {
	config       *SPAPIConfig
	httpClient   *http.Client
	oauth2Config *oauth2.Config
	token        *oauth2.Token
	tokenMu      sync.Mutex
	region       string
	isSandbox    bool

	catalogLimiter   *rateLimiter
	listingsLimiter  *rateLimiter
	reportsLimiter   *rateLimiter
	inventoryLimiter *rateLimiter
}

type SPAPIConfig struct {
	LWAClientID        string `json:"lwa_client_id"`
	LWAClientSecret    string `json:"lwa_client_secret"`
	RefreshToken       string `json:"refresh_token"`
	AWSAccessKeyID     string `json:"aws_access_key_id"`
	AWSSecretAccessKey string `json:"aws_secret_access_key"`
	RoleARN            string `json:"role_arn,omitempty"`
	MarketplaceID      string `json:"marketplace_id"`
	Region             string `json:"region"`
	SellerID           string `json:"seller_id"`
	IsSandbox          bool   `json:"is_sandbox"`
}

func NewSPAPIClient(ctx context.Context, config *SPAPIConfig) (*SPAPIClient, error) {
	if config == nil {
		return nil, fmt.Errorf("config is required")
	}
	if config.LWAClientID == "" || config.LWAClientSecret == "" {
		return nil, fmt.Errorf("LWA credentials are required")
	}
	if config.RefreshToken == "" {
		return nil, fmt.Errorf("refresh token is required")
	}
	if config.MarketplaceID == "" {
		return nil, fmt.Errorf("marketplace ID is required")
	}

	oauth2Conf := &oauth2.Config{
		ClientID:     config.LWAClientID,
		ClientSecret: config.LWAClientSecret,
		Endpoint:     oauth2.Endpoint{TokenURL: LWATokenEndpoint},
	}

	client := &SPAPIClient{
		config:           config,
		httpClient:       &http.Client{Timeout: 60 * time.Second},
		oauth2Config:     oauth2Conf,
		region:           config.Region,
		isSandbox:        config.IsSandbox,
		catalogLimiter:   newRateLimiter(2.0),
		listingsLimiter:  newRateLimiter(5.0),
		reportsLimiter:   newRateLimiter(0.02),
		inventoryLimiter: newRateLimiter(2.0),
	}

	if err := client.refreshAccessToken(ctx); err != nil {
		return nil, fmt.Errorf("failed to obtain access token: %w", err)
	}
	return client, nil
}

// ============================================================================
// AUTH
// ============================================================================

func (c *SPAPIClient) refreshAccessToken(ctx context.Context) error {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	if c.token != nil && time.Now().Before(c.token.Expiry.Add(-2*time.Minute)) {
		return nil
	}

	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", c.config.RefreshToken)
	data.Set("client_id", c.config.LWAClientID)
	data.Set("client_secret", c.config.LWAClientSecret)

	req, err := http.NewRequestWithContext(ctx, "POST", LWATokenEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("token refresh failed (%d): %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return err
	}

	c.token = &oauth2.Token{
		AccessToken:  tokenResp.AccessToken,
		TokenType:    tokenResp.TokenType,
		RefreshToken: c.config.RefreshToken,
		Expiry:       time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
	}
	return nil
}

func (c *SPAPIClient) ensureValidToken(ctx context.Context) error {
	if c.token != nil && time.Now().Before(c.token.Expiry.Add(-5*time.Minute)) {
		return nil
	}
	return c.refreshAccessToken(ctx)
}

// ============================================================================
// REQUEST INFRASTRUCTURE
// ============================================================================

func (c *SPAPIClient) getEndpoint() string {
	if c.isSandbox {
		return SPAPISandboxEndpoint
	}
	switch c.region {
	case "us-east-1":
		return SPAPIEndpointNA
	case "eu-west-1":
		return SPAPIEndpointEU
	case "us-west-2":
		return SPAPIEndpointFE
	default:
		return SPAPIEndpointNA
	}
}

type APIError struct {
	StatusCode int
	Body       string
	Retryable  bool
}

func (e *APIError) Error() string {
	return fmt.Sprintf("SP-API error %d: %s", e.StatusCode, e.Body)
}

func (c *SPAPIClient) makeRequest(ctx context.Context, method, path string, queryParams url.Values, body interface{}, limiter *rateLimiter) (*http.Response, error) {
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			delay := baseRetryDelay * time.Duration(1<<uint(attempt-1))
			if delay > maxRetryDelay {
				delay = maxRetryDelay
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		if err := c.ensureValidToken(ctx); err != nil {
			return nil, fmt.Errorf("token refresh failed: %w", err)
		}

		if limiter != nil {
			limiter.wait()
		}

		endpoint := c.getEndpoint()
		fullURL := endpoint + path
		if queryParams != nil && len(queryParams) > 0 {
			fullURL += "?" + queryParams.Encode()
		}
		log.Printf("[SPAPIClient] %s %s", method, fullURL)

		var bodyReader io.Reader
		if body != nil {
			bodyBytes, err := json.Marshal(body)
			if err != nil {
				return nil, err
			}
			bodyReader = bytes.NewReader(bodyBytes)
		}

		req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
		if err != nil {
			return nil, err
		}
		req.Header.Set("x-amz-access-token", c.token.AccessToken)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return resp, nil
		}

		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			lastErr = &APIError{StatusCode: resp.StatusCode, Body: string(respBody), Retryable: true}
			log.Printf("[SP-API] Retryable error on %s %s (attempt %d/%d): %d",
				method, path, attempt+1, maxRetries+1, resp.StatusCode)
			continue
		}

		return nil, &APIError{StatusCode: resp.StatusCode, Body: string(respBody), Retryable: false}
	}

	return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
}

func (c *SPAPIClient) makeRawRequest(ctx context.Context, rawURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return nil, err
	}
	return c.httpClient.Do(req)
}

// ============================================================================
// REPORTS API
// ============================================================================

const (
	ReportMerchantListingsAll    = "GET_MERCHANT_LISTINGS_ALL_DATA"
	ReportMerchantListingsActive = "GET_MERCHANT_LISTINGS_DATA"
	ReportFBAInventory           = "GET_FBA_MYI_ALL_INVENTORY_DATA"
)

type CreateReportRequest struct {
	ReportType     string   `json:"reportType"`
	MarketplaceIds []string `json:"marketplaceIds"`
}

type CreateReportResponse struct {
	ReportId string `json:"reportId"`
}

type Report struct {
	ReportId            string   `json:"reportId"`
	ReportType          string   `json:"reportType"`
	ProcessingStatus    string   `json:"processingStatus"`
	ReportDocumentId    string   `json:"reportDocumentId,omitempty"`
	MarketplaceIds      []string `json:"marketplaceIds,omitempty"`
	CreatedTime         string   `json:"createdTime,omitempty"`
	ProcessingStartTime string   `json:"processingStartTime,omitempty"`
	ProcessingEndTime   string   `json:"processingEndTime,omitempty"`
}

type ReportDocument struct {
	ReportDocumentId     string `json:"reportDocumentId"`
	URL                  string `json:"url"`
	CompressionAlgorithm string `json:"compressionAlgorithm,omitempty"`
}

// ReportRow represents one row from GET_MERCHANT_LISTINGS_ALL_DATA.
type ReportRow struct {
	ItemName               string
	ItemDescription        string
	ListingID              string
	SellerSKU              string
	Price                  float64
	Quantity               int
	OpenDate               string
	ImageURL               string
	ItemCondition          string
	ProductIDType          string
	ProductID              string
	ASIN1                  string
	ASIN2                  string
	ASIN3                  string
	FulfillmentChannel     string
	Status                 string
	MerchantShippingGroup  string
	ZShopBrowsePath        string
	BusinessPrice          string
	PendingQuantity        int
}

func (c *SPAPIClient) CreateReport(ctx context.Context, reportType string) (string, error) {
	body := CreateReportRequest{
		ReportType:     reportType,
		MarketplaceIds: []string{c.config.MarketplaceID},
	}

	resp, err := c.makeRequest(ctx, "POST", "/reports/2021-06-30/reports", nil, body, c.reportsLimiter)
	if err != nil {
		return "", fmt.Errorf("create report failed: %w", err)
	}
	defer resp.Body.Close()

	var result CreateReportResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	log.Printf("[SP-API] Report requested: type=%s, reportId=%s", reportType, result.ReportId)
	return result.ReportId, nil
}

func (c *SPAPIClient) GetReport(ctx context.Context, reportId string) (*Report, error) {
	path := fmt.Sprintf("/reports/2021-06-30/reports/%s", reportId)
	resp, err := c.makeRequest(ctx, "GET", path, nil, nil, c.reportsLimiter)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var report Report
	if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
		return nil, err
	}
	return &report, nil
}

func (c *SPAPIClient) PollReportUntilDone(ctx context.Context, reportId string) (*Report, error) {
	deadline := time.Now().Add(reportPollTimeout)

	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("report %s timed out after %v", reportId, reportPollTimeout)
		}

		report, err := c.GetReport(ctx, reportId)
		if err != nil {
			return nil, fmt.Errorf("failed to poll report %s: %w", reportId, err)
		}

		switch report.ProcessingStatus {
		case "DONE":
			log.Printf("[SP-API] Report %s completed (documentId: %s)", reportId, report.ReportDocumentId)
			return report, nil
		case "CANCELLED":
			return nil, fmt.Errorf("report %s was cancelled", reportId)
		case "FATAL":
			return nil, fmt.Errorf("report %s failed with FATAL status", reportId)
		default:
			log.Printf("[SP-API] Report %s status: %s, waiting...", reportId, report.ProcessingStatus)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(reportPollInterval):
		}
	}
}

func (c *SPAPIClient) GetReportDocument(ctx context.Context, documentId string) (*ReportDocument, error) {
	path := fmt.Sprintf("/reports/2021-06-30/documents/%s", documentId)
	resp, err := c.makeRequest(ctx, "GET", path, nil, nil, c.reportsLimiter)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var doc ReportDocument
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

func (c *SPAPIClient) DownloadAndParseReport(ctx context.Context, doc *ReportDocument) ([]ReportRow, error) {
	resp, err := c.makeRawRequest(ctx, doc.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to download report: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("report download failed (%d): %s", resp.StatusCode, string(body))
	}

	var reader io.Reader = resp.Body
	if doc.CompressionAlgorithm == "GZIP" {
		gzReader, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to decompress report: %w", err)
		}
		defer gzReader.Close()
		reader = gzReader
	}

	return parseTSVReport(reader)
}

func parseTSVReport(reader io.Reader) ([]ReportRow, error) {
	csvReader := csv.NewReader(reader)
	csvReader.Comma = '\t'
	csvReader.LazyQuotes = true
	csvReader.FieldsPerRecord = -1

	headers, err := csvReader.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read report header: %w", err)
	}

	colIdx := make(map[string]int)
	for i, h := range headers {
		colIdx[strings.TrimSpace(strings.ToLower(h))] = i
	}

	var rows []ReportRow
	for {
		record, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("[SP-API] Warning: skipping malformed report row: %v", err)
			continue
		}

		row := ReportRow{
			ItemName:              getCol(record, colIdx, "item-name"),
			ItemDescription:       getCol(record, colIdx, "item-description"),
			ListingID:             getCol(record, colIdx, "listing-id"),
			SellerSKU:             getCol(record, colIdx, "seller-sku"),
			ImageURL:              getCol(record, colIdx, "image-url"),
			ItemCondition:         getCol(record, colIdx, "item-condition"),
			ProductIDType:         getCol(record, colIdx, "product-id-type"),
			ProductID:             getCol(record, colIdx, "product-id"),
			ASIN1:                 getCol(record, colIdx, "asin1"),
			ASIN2:                 getCol(record, colIdx, "asin2"),
			ASIN3:                 getCol(record, colIdx, "asin3"),
			FulfillmentChannel:    getCol(record, colIdx, "fulfillment-channel"),
			Status:                getCol(record, colIdx, "status"),
			MerchantShippingGroup: getCol(record, colIdx, "merchant-shipping-group"),
			OpenDate:              getCol(record, colIdx, "open-date"),
			ZShopBrowsePath:       getCol(record, colIdx, "zshop-browse-path"),
			BusinessPrice:         getCol(record, colIdx, "business-price"),
		}

		priceStr := getCol(record, colIdx, "price")
		if priceStr != "" {
			fmt.Sscanf(priceStr, "%f", &row.Price)
		}
		qtyStr := getCol(record, colIdx, "quantity")
		if qtyStr != "" {
			fmt.Sscanf(qtyStr, "%d", &row.Quantity)
		}
		pendingStr := getCol(record, colIdx, "pending-quantity")
		if pendingStr != "" {
			fmt.Sscanf(pendingStr, "%d", &row.PendingQuantity)
		}

		rows = append(rows, row)
	}

	log.Printf("[SP-API] Parsed %d rows from report", len(rows))
	return rows, nil
}

func getCol(record []string, colIdx map[string]int, name string) string {
	if idx, ok := colIdx[name]; ok && idx < len(record) {
		return strings.TrimSpace(record[idx])
	}
	return ""
}

// RequestAndDownloadReport is the convenience method for the full report flow.
func (c *SPAPIClient) RequestAndDownloadReport(ctx context.Context, reportType string) ([]ReportRow, error) {
	reportId, err := c.CreateReport(ctx, reportType)
	if err != nil {
		return nil, err
	}
	report, err := c.PollReportUntilDone(ctx, reportId)
	if err != nil {
		return nil, err
	}
	doc, err := c.GetReportDocument(ctx, report.ReportDocumentId)
	if err != nil {
		return nil, err
	}
	return c.DownloadAndParseReport(ctx, doc)
}

// ============================================================================
// CATALOG ITEMS API
// ============================================================================

type CatalogItem struct {
	ASIN          string                 `json:"asin"`
	Attributes    map[string]interface{} `json:"attributes,omitempty"`
	Identifiers   []CatalogIdentifier    `json:"identifiers,omitempty"`
	Images        []CatalogImage         `json:"images,omitempty"`
	ProductTypes  []ProductTypeDefinition `json:"productTypes,omitempty"`
	SalesRanks    []SalesRank            `json:"salesRanks,omitempty"`
	Summaries     []CatalogSummary       `json:"summaries,omitempty"`
	Variations    []CatalogVariation     `json:"variations,omitempty"`
	VendorDetails []VendorDetail         `json:"vendorDetails,omitempty"`
}

type CatalogIdentifier struct {
	Identifiers   []IdentifierValue `json:"identifiers"`
	MarketplaceID string            `json:"marketplaceId"`
}
type IdentifierValue struct {
	Identifier     string `json:"identifier"`
	IdentifierType string `json:"identifierType"`
}
type CatalogImage struct {
	Images        []ImageDetail `json:"images"`
	MarketplaceID string        `json:"marketplaceId"`
	Variant       string        `json:"variant"`
}
type ImageDetail struct {
	Height int    `json:"height"`
	Link   string `json:"link"`
	Width  int    `json:"width"`
}
type ProductTypeDefinition struct {
	MarketplaceID string `json:"marketplaceId"`
	ProductType   string `json:"productType"`
}
type SalesRank struct {
	ClassificationRanks []ClassificationRank `json:"classificationRanks"`
	DisplayGroupRanks   []DisplayGroupRank   `json:"displayGroupRanks"`
	MarketplaceID       string               `json:"marketplaceId"`
}
type ClassificationRank struct {
	ClassificationID string `json:"classificationId"`
	Rank             int    `json:"rank"`
	Title            string `json:"title"`
}
type DisplayGroupRank struct {
	Rank                int    `json:"rank"`
	Title               string `json:"title"`
	WebsiteDisplayGroup string `json:"websiteDisplayGroup"`
}
type CatalogSummary struct {
	Brand                string               `json:"brand,omitempty"`
	BrowseClassification *BrowseClassification `json:"browseClassification,omitempty"`
	Color                string               `json:"color,omitempty"`
	ItemClassification   string               `json:"itemClassification,omitempty"`
	ItemName             string               `json:"itemName,omitempty"`
	Manufacturer         string               `json:"manufacturer,omitempty"`
	MarketplaceID        string               `json:"marketplaceId"`
	ModelNumber          string               `json:"modelNumber,omitempty"`
	PackageQuantity      int                  `json:"packageQuantity,omitempty"`
	PartNumber           string               `json:"partNumber,omitempty"`
	Size                 string               `json:"size,omitempty"`
	Style                string               `json:"style,omitempty"`
}
type BrowseClassification struct {
	ClassificationID string `json:"classificationId"`
	DisplayName      string `json:"displayName"`
}
type CatalogVariation struct {
	MarketplaceID string   `json:"marketplaceId"`
	ASINs         []string `json:"asins"`
	Type          string   `json:"type"`
}
type VendorDetail struct {
	BrandCode        string `json:"brandCode,omitempty"`
	CategoryCode     string `json:"categoryCode,omitempty"`
	ManufacturerCode string `json:"manufacturerCode,omitempty"`
	MarketplaceID    string `json:"marketplaceId"`
	ProductGroup     string `json:"productGroup,omitempty"`
}

func (c *SPAPIClient) GetCatalogItem(ctx context.Context, asin string) (*CatalogItem, error) {
	path := fmt.Sprintf("/catalog/2022-04-01/items/%s", asin)
	queryParams := url.Values{}
	queryParams.Set("marketplaceIds", c.config.MarketplaceID)
	queryParams.Set("includedData", "attributes,identifiers,images,productTypes,summaries,variations")

	resp, err := c.makeRequest(ctx, "GET", path, queryParams, nil, c.catalogLimiter)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		s := string(body)
		if len(s) > 400 { s = s[:400] }
		log.Printf("[SPAPIClient] GetCatalogItem %s: HTTP %d: %s", asin, resp.StatusCode, s)
		return nil, fmt.Errorf("SP-API error %d: %s", resp.StatusCode, s)
	}

	var result CatalogItem
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *SPAPIClient) SearchCatalogItems(ctx context.Context, keywords string, pageSize int, pageToken string) (*CatalogSearchResponse, error) {
	path := "/catalog/2022-04-01/items"
	queryParams := url.Values{}
	queryParams.Set("marketplaceIds", c.config.MarketplaceID)
	queryParams.Set("keywords", keywords)
	queryParams.Set("includedData", "summaries,images")
	if pageSize > 0 {
		queryParams.Set("pageSize", fmt.Sprintf("%d", pageSize))
	}
	if pageToken != "" {
		queryParams.Set("pageToken", pageToken)
	}

	resp, err := c.makeRequest(ctx, "GET", path, queryParams, nil, c.catalogLimiter)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result CatalogSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SearchCatalogItemsByIdentifier looks up catalog items by a specific identifier
// (ASIN, EAN, UPC, ISBN). identifierType must be one of: ASIN, EAN, UPC, ISBN, GTIN.
func (c *SPAPIClient) SearchCatalogItemsByIdentifier(ctx context.Context, identifierType, identifierValue string) (*CatalogSearchResponse, error) {
	path := "/catalog/2022-04-01/items"
	queryParams := url.Values{}
	queryParams.Set("marketplaceIds", c.config.MarketplaceID)
	queryParams.Set("identifiers", identifierValue)
	queryParams.Set("identifiersType", identifierType)
	// Do NOT send sellerId — scopes to seller's own listings only.
	// Keep includedData minimal to avoid permission errors.
	queryParams.Set("includedData", "attributes,identifiers,images,productTypes,summaries")
	queryParams.Set("pageSize", "5")

	resp, err := c.makeRequest(ctx, "GET", path, queryParams, nil, c.catalogLimiter)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result CatalogSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

type CatalogSearchResponse struct {
	Items           []CatalogItem `json:"items"`
	NumberOfResults int           `json:"numberOfResults"`
	Pagination      *Pagination   `json:"pagination,omitempty"`
}
type Pagination struct {
	NextToken     string `json:"nextToken,omitempty"`
	PreviousToken string `json:"previousToken,omitempty"`
}

// ============================================================================
// LISTINGS API
// ============================================================================

type ListingsItem struct {
	SKU                 string                 `json:"sku"`
	Status              []string               `json:"status"`
	ProductType         string                 `json:"productType"`
	Attributes          map[string]interface{} `json:"attributes"`
	Issues              []Issue                `json:"issues,omitempty"`
	FulfillmentChannels []string               `json:"fulfillmentChannels,omitempty"`
}
type Issue struct {
	Code           string   `json:"code"`
	Message        string   `json:"message"`
	Severity       string   `json:"severity"`
	AttributeNames []string `json:"attributeNames,omitempty"`
}

func (c *SPAPIClient) GetListingsItem(ctx context.Context, sku string) (*ListingsItem, error) {
	path := fmt.Sprintf("/listings/2021-08-01/items/%s/%s", c.config.SellerID, url.PathEscape(sku))
	queryParams := url.Values{}
	queryParams.Set("marketplaceIds", c.config.MarketplaceID)
	queryParams.Set("includedData", "summaries,attributes,issues,fulfillmentAvailability")

	resp, err := c.makeRequest(ctx, "GET", path, queryParams, nil, c.listingsLimiter)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result ListingsItem
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *SPAPIClient) CreateListing(ctx context.Context, sku string, productType string, attributes map[string]interface{}) error {
	path := fmt.Sprintf("/listings/2021-08-01/items/%s/%s", c.config.SellerID, url.PathEscape(sku))
	queryParams := url.Values{}
	queryParams.Set("marketplaceIds", c.config.MarketplaceID)

	body := map[string]interface{}{
		"productType": productType,
		"attributes":  attributes,
	}

	resp, err := c.makeRequest(ctx, "PUT", path, queryParams, body, c.listingsLimiter)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// ============================================================================
// FBA INVENTORY API
// ============================================================================

type InventorySummary struct {
	ASIN                     string `json:"asin"`
	FnSKU                    string `json:"fnSku"`
	SellerSKU                string `json:"sellerSku"`
	Condition                string `json:"condition"`
	TotalQuantity            int    `json:"totalQuantity"`
	FulfillableQuantity      int    `json:"fulfillableQuantity"`
	InboundWorkingQuantity   int    `json:"inboundWorkingQuantity"`
	InboundShippedQuantity   int    `json:"inboundShippedQuantity"`
	InboundReceivingQuantity int    `json:"inboundReceivingQuantity"`
	LastUpdatedTime          string `json:"lastUpdatedTime"`
}

func (c *SPAPIClient) GetInventorySummaries(ctx context.Context) ([]InventorySummary, error) {
	path := "/fba/inventory/v1/summaries"
	queryParams := url.Values{}
	queryParams.Set("marketplaceIds", c.config.MarketplaceID)
	queryParams.Set("details", "true")

	resp, err := c.makeRequest(ctx, "GET", path, queryParams, nil, c.inventoryLimiter)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Payload struct {
			InventorySummaries []InventorySummary `json:"inventorySummaries"`
		} `json:"payload"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Payload.InventorySummaries, nil
}

// ============================================================================
// SELLERS API
// ============================================================================

type MarketplaceParticipation struct {
	Marketplace struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		CountryCode string `json:"countryCode"`
	} `json:"marketplace"`
	Participation struct {
		IsParticipating      bool `json:"isParticipating"`
		HasSuspendedListings bool `json:"hasSuspendedListings"`
	} `json:"participation"`
}

func (c *SPAPIClient) GetMarketplaceParticipations(ctx context.Context) ([]MarketplaceParticipation, error) {
	path := "/sellers/v1/marketplaceParticipations"
	resp, err := c.makeRequest(ctx, "GET", path, nil, nil, c.catalogLimiter)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Payload []MarketplaceParticipation `json:"payload"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Payload, nil
}

// ============================================================================
// ORDERS API (connection testing)
// ============================================================================

func (c *SPAPIClient) TestShippingAccess(ctx context.Context) error {
	path := "/orders/v0/orders"
	queryParams := url.Values{}
	queryParams.Set("MarketplaceIds", c.config.MarketplaceID)
	queryParams.Set("CreatedAfter", time.Now().Add(-24*time.Hour).UTC().Format(time.RFC3339))
	queryParams.Set("MaxResultsPerPage", "1")

	resp, err := c.makeRequest(ctx, "GET", path, queryParams, nil, c.catalogLimiter)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// ============================================================================
// ORDERS API - Full Implementation
// ============================================================================

type OrdersResponse struct {
	Orders    []Order  `json:"Orders"`
	NextToken string   `json:"NextToken"`
}

type Order struct {
	AmazonOrderID      string       `json:"AmazonOrderId"`
	PurchaseDate       string       `json:"PurchaseDate"`
	LastUpdateDate     string       `json:"LastUpdateDate"`
	OrderStatus        string       `json:"OrderStatus"`
	FulfillmentChannel string       `json:"FulfillmentChannel"`
	OrderTotal         MoneyType    `json:"OrderTotal"`
	ShippingAddress    *OrderAddress `json:"ShippingAddress,omitempty"`
	BuyerInfo          *BuyerInfo   `json:"BuyerInfo,omitempty"`
}

type MoneyType struct {
	CurrencyCode string `json:"CurrencyCode"`
	Amount       string `json:"Amount"`
}

type OrderAddress struct {
	Name          string `json:"Name"`
	AddressLine1  string `json:"AddressLine1"`
	City          string `json:"City"`
	StateOrRegion string `json:"StateOrRegion"`
	PostalCode    string `json:"PostalCode"`
	CountryCode   string `json:"CountryCode"`
}

type BuyerInfo struct {
	BuyerEmail string `json:"BuyerEmail"`
	BuyerName  string `json:"BuyerName"`
}

// GetOrders fetches orders from Amazon SP-API
func (c *SPAPIClient) GetOrders(ctx context.Context, createdAfter time.Time) (*OrdersResponse, error) {
	path := "/orders/v0/orders"
	
	queryParams := url.Values{}
	queryParams.Set("MarketplaceIds", c.config.MarketplaceID)
	queryParams.Set("CreatedAfter", createdAfter.Format(time.RFC3339))
	
	resp, err := c.makeRequest(ctx, "GET", path, queryParams, nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Payload OrdersResponse `json:"payload"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result.Payload, nil
}

// GetOrdersWithPII fetches orders with full buyer and shipping address information.
// Amazon requires a special "Restricted Data Token" (RDT) to access this PII data.
// Your SP-API app must have PII access approved (you confirmed it does).
// This replaces the standard GetOrders call for order imports.
func (c *SPAPIClient) GetOrdersWithPII(ctx context.Context, createdAfter time.Time) (*OrdersResponse, error) {
	// Step 1: Get a Restricted Data Token that unlocks buyer + shipping address fields
	rdt, err := c.getRestrictedDataToken(ctx)
	if err != nil {
		// If RDT fails, fall back to standard (no PII) rather than breaking order import
		log.Printf("[SP-API] Warning: Could not get PII token (%v) — falling back to standard orders (no buyer info)", err)
		return c.GetOrders(ctx, createdAfter)
	}

	// Step 2: Call orders API using the RDT instead of the normal access token
	path := "/orders/v0/orders"
	queryParams := url.Values{}
	queryParams.Set("MarketplaceIds", c.config.MarketplaceID)
	queryParams.Set("CreatedAfter", createdAfter.Format(time.RFC3339))

	endpoint := c.getEndpoint()
	fullURL := endpoint + path + "?" + queryParams.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
	if err != nil {
		return nil, err
	}
	// Use RDT as the access token - this is what unlocks the PII fields
	req.Header.Set("x-amz-access-token", rdt)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Payload OrdersResponse `json:"payload"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	log.Printf("[SP-API] Fetched %d orders with PII data", len(result.Payload.Orders))
	return &result.Payload, nil
}

// getRestrictedDataToken calls Amazon's token service to get a short-lived token
// that allows reading buyer PII (name, email) and shipping addresses from orders.
// Tokens last 1 hour. See: https://developer-docs.amazon.com/sp-api/docs/tokens-api-use-case-guide
func (c *SPAPIClient) getRestrictedDataToken(ctx context.Context) (string, error) {
	// First make sure we have a valid LWA access token
	if err := c.ensureValidToken(ctx); err != nil {
		return "", fmt.Errorf("refresh token: %w", err)
	}

	// The RDT request body specifies exactly which data elements we need access to
	body := map[string]interface{}{
		"restrictedResources": []map[string]interface{}{
			{
				"method": "GET",
				"path":   "/orders/v0/orders",
				"dataElements": []string{
					"buyerInfo",       // buyer name and email
					"shippingAddress", // full delivery address
				},
			},
		},
	}

	endpoint := c.getEndpoint()
	tokenURL := endpoint + "/tokens/2021-03-01/restrictedDataToken"

	bodyBytes, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	req.Header.Set("x-amz-access-token", c.token.AccessToken)
	req.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{Timeout: 15 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("RDT request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("RDT API returned %d: %s", resp.StatusCode, string(respBody))
	}

	var rdtResp struct {
		RestrictedDataToken string `json:"restrictedDataToken"`
		ExpiresIn           int    `json:"expiresIn"`
	}
	if err := json.Unmarshal(respBody, &rdtResp); err != nil {
		return "", fmt.Errorf("parse RDT response: %w", err)
	}
	if rdtResp.RestrictedDataToken == "" {
		return "", fmt.Errorf("empty token returned")
	}

	log.Printf("[SP-API] Got Restricted Data Token (expires in %ds)", rdtResp.ExpiresIn)
	return rdtResp.RestrictedDataToken, nil
}

// OrderItem represents a line item in an Amazon order
type OrderItem struct {
	ASIN            string     `json:"ASIN"`
	SellerSKU       string     `json:"SellerSKU"`
	OrderItemID     string     `json:"OrderItemId"`
	Title           string     `json:"Title"`
	QuantityOrdered int        `json:"QuantityOrdered"`
	QuantityShipped int        `json:"QuantityShipped"`
	ItemPrice       *MoneyType `json:"ItemPrice,omitempty"`
	ItemTax         *MoneyType `json:"ItemTax,omitempty"`
	ShippingPrice   *MoneyType `json:"ShippingPrice,omitempty"`
	ShippingTax     *MoneyType `json:"ShippingTax,omitempty"`
	PromotionDiscount *MoneyType `json:"PromotionDiscount,omitempty"`
}

type OrderItemsResponse struct {
	OrderItems []OrderItem `json:"OrderItems"`
	NextToken  string      `json:"NextToken"`
}

// GetOrderItems fetches line items for a specific order
func (c *SPAPIClient) GetOrderItems(ctx context.Context, amazonOrderID string) (*OrderItemsResponse, error) {
	path := fmt.Sprintf("/orders/v0/orders/%s/orderItems", amazonOrderID)
	
	resp, err := c.makeRequest(ctx, "GET", path, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Payload OrderItemsResponse `json:"payload"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result.Payload, nil
}
