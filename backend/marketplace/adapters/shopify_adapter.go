package adapters

// ============================================================================
// SHOPIFY MARKETPLACE ADAPTER
// ============================================================================
// Implements marketplace.MarketplaceAdapter for Shopify stores.
//
// Credentials required:
//   shop_domain   — e.g. "mystore.myshopify.com"
//   access_token  — Admin API access token (generated in Shopify Partners or
//                   via OAuth install flow)
//   api_version   — e.g. "2024-01" (defaults to "2024-01" if blank)
//
// Register in main.go init():
//   marketplace.Register("shopify", adapters.NewShopifyAdapter, marketplace.AdapterMetadata{...})
// ============================================================================

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"module-a/marketplace"
)

// ── Adapter struct ─────────────────────────────────────────────────────────

type ShopifyAdapter struct {
	credentials marketplace.Credentials
	shopDomain  string
	accessToken string
	apiVersion  string
	httpClient  *http.Client
}

func NewShopifyAdapter(_ context.Context, credentials marketplace.Credentials) (marketplace.MarketplaceAdapter, error) {
	shopDomain := credentials.Data["shop_domain"]
	accessToken := credentials.Data["access_token"]
	apiVersion := credentials.Data["api_version"]

	if shopDomain == "" {
		return nil, fmt.Errorf("shop_domain is required for Shopify")
	}
	if accessToken == "" {
		return nil, fmt.Errorf("access_token is required for Shopify")
	}
	if apiVersion == "" {
		apiVersion = "2024-01"
	}

	return &ShopifyAdapter{
		credentials: credentials,
		shopDomain:  shopDomain,
		accessToken: accessToken,
		apiVersion:  apiVersion,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// ── HTTP helper ────────────────────────────────────────────────────────────

func (s *ShopifyAdapter) apiURL(path string) string {
	return fmt.Sprintf("https://%s/admin/api/%s%s", s.shopDomain, s.apiVersion, path)
}

func (s *ShopifyAdapter) do(ctx context.Context, method, path string, body interface{}) ([]byte, int, error) {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("marshal request: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, s.apiURL(path), reqBody)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("X-Shopify-Access-Token", s.accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}

	if resp.StatusCode >= 400 {
		return data, resp.StatusCode, fmt.Errorf("shopify API error %d: %s", resp.StatusCode, string(data))
	}
	return data, resp.StatusCode, nil
}

// ── Shopify API types ──────────────────────────────────────────────────────

type shopifyProduct struct {
	ID          int64                  `json:"id"`
	Title       string                 `json:"title"`
	BodyHTML    string                 `json:"body_html"`
	Vendor      string                 `json:"vendor"`
	ProductType string                 `json:"product_type"`
	Handle      string                 `json:"handle"`
	Status      string                 `json:"status"` // active|draft|archived
	Tags        string                 `json:"tags"`
	Variants    []shopifyVariant       `json:"variants"`
	Images      []shopifyImage         `json:"images"`
	Options     []shopifyOption        `json:"options"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

type shopifyVariant struct {
	ID                   int64   `json:"id"`
	ProductID            int64   `json:"product_id"`
	Title                string  `json:"title"`
	SKU                  string  `json:"sku"`
	Price                string  `json:"price"`
	CompareAtPrice       string  `json:"compare_at_price"`
	InventoryQuantity    int     `json:"inventory_quantity"`
	InventoryItemID      int64   `json:"inventory_item_id"`
	Weight               float64 `json:"weight"`
	WeightUnit           string  `json:"weight_unit"`
	Barcode              string  `json:"barcode"`
	FulfillmentService   string  `json:"fulfillment_service"`
	InventoryManagement  string  `json:"inventory_management"`
	InventoryPolicy      string  `json:"inventory_policy"`
	RequiresShipping     bool    `json:"requires_shipping"`
	Option1              string  `json:"option1"`
	Option2              string  `json:"option2"`
	Option3              string  `json:"option3"`
}

type shopifyImage struct {
	ID        int64    `json:"id"`
	Src       string   `json:"src"`
	Alt       string   `json:"alt"`
	Position  int      `json:"position"`
	VariantIDs []int64 `json:"variant_ids"`
}

type shopifyOption struct {
	ID       int64    `json:"id"`
	Name     string   `json:"name"`
	Position int      `json:"position"`
	Values   []string `json:"values"`
}

type shopifyInventoryLevel struct {
	InventoryItemID int64 `json:"inventory_item_id"`
	LocationID      int64 `json:"location_id"`
	Available       int   `json:"available"`
}

// ── CONNECTION ─────────────────────────────────────────────────────────────

func (s *ShopifyAdapter) Connect(_ context.Context, _ marketplace.Credentials) error    { return nil }
func (s *ShopifyAdapter) Disconnect(_ context.Context) error                            { return nil }
func (s *ShopifyAdapter) RefreshAuth(_ context.Context) error                           { return nil } // Shopify uses static tokens

func (s *ShopifyAdapter) TestConnection(ctx context.Context) error {
	_, _, err := s.do(ctx, "GET", "/shop.json", nil)
	return err
}

func (s *ShopifyAdapter) GetConnectionStatus(ctx context.Context) (*marketplace.ConnectionStatus, error) {
	err := s.TestConnection(ctx)
	status := &marketplace.ConnectionStatus{
		IsConnected: err == nil,
		LastChecked: time.Now(),
	}
	if err != nil {
		status.ErrorMessage = err.Error()
	} else {
		status.LastSuccessful = time.Now()
	}
	return status, nil
}

// ── IMPORT: Marketplace → PIM ──────────────────────────────────────────────

func (s *ShopifyAdapter) FetchListings(ctx context.Context, filters marketplace.ImportFilters) ([]marketplace.MarketplaceProduct, error) {
	limit := filters.PageSize
	if limit <= 0 || limit > 250 {
		limit = 250
	}

	statusFilter := "active,draft"
	if filters.StockFilter == "in_stock" {
		statusFilter = "active"
	}
	url := fmt.Sprintf("/products.json?limit=%d&status=%s", limit, statusFilter)

	data, _, err := s.do(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Products []shopifyProduct `json:"products"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal products: %w", err)
	}

	var products []marketplace.MarketplaceProduct
	for _, p := range resp.Products {
		products = append(products, s.toMarketplaceProduct(p))
	}
	return products, nil
}

func (s *ShopifyAdapter) FetchProduct(ctx context.Context, externalID string) (*marketplace.MarketplaceProduct, error) {
	data, _, err := s.do(ctx, "GET", fmt.Sprintf("/products/%s.json", externalID), nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Product shopifyProduct `json:"product"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	mp := s.toMarketplaceProduct(resp.Product)
	return &mp, nil
}

func (s *ShopifyAdapter) FetchProductImages(ctx context.Context, externalID string) ([]marketplace.ImageData, error) {
	data, _, err := s.do(ctx, "GET", fmt.Sprintf("/products/%s/images.json", externalID), nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Images []shopifyImage `json:"images"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	var images []marketplace.ImageData
	for i, img := range resp.Images {
		images = append(images, marketplace.ImageData{
			URL:      img.Src,
			Position: i + 1,
			IsMain:   i == 0,
		})
	}
	return images, nil
}

func (s *ShopifyAdapter) FetchInventory(ctx context.Context, externalID string) (*marketplace.InventoryLevel, error) {
	// externalID is the Shopify inventory_item_id
	data, _, err := s.do(ctx, "GET", fmt.Sprintf("/inventory_levels.json?inventory_item_ids=%s", externalID), nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		InventoryLevels []shopifyInventoryLevel `json:"inventory_levels"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	total := 0
	for _, l := range resp.InventoryLevels {
		total += l.Available
	}
	return &marketplace.InventoryLevel{
		Quantity:  total,
		UpdatedAt: time.Now(),
	}, nil
}

// ── LISTING MANAGEMENT: PIM → Marketplace ─────────────────────────────────

func (s *ShopifyAdapter) CreateListing(ctx context.Context, listing marketplace.ListingData) (*marketplace.ListingResult, error) {
	price := fmt.Sprintf("%.2f", listing.Price)
	sku := ""
	if listing.Attributes != nil {
		if v, ok := listing.Attributes["sku"].(string); ok {
			sku = v
		}
	}
	vendor := ""
	if listing.Attributes != nil {
		if v, ok := listing.Attributes["brand"].(string); ok {
			vendor = v
		}
	}
	body := map[string]interface{}{
		"product": map[string]interface{}{
			"title":        listing.Title,
			"body_html":    listing.Description,
			"vendor":       vendor,
			"product_type": listing.CategoryID,
			"status":       "draft",
			"variants": []map[string]interface{}{{
				"sku":   sku,
				"price": price,
			}},
		},
	}
	data, _, err := s.do(ctx, "POST", "/products.json", body)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Product shopifyProduct `json:"product"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	return &marketplace.ListingResult{
		ExternalID: strconv.FormatInt(resp.Product.ID, 10),
		URL:        fmt.Sprintf("https://%s/products/%s", s.shopDomain, resp.Product.Handle),
		Status:     resp.Product.Status,
	}, nil
}

func (s *ShopifyAdapter) UpdateListing(ctx context.Context, externalID string, updates marketplace.ListingData) error {
	body := map[string]interface{}{
		"product": map[string]interface{}{
			"id":        externalID,
			"title":     updates.Title,
			"body_html": updates.Description,
		},
	}
	_, _, err := s.do(ctx, "PUT", fmt.Sprintf("/products/%s.json", externalID), body)
	return err
}

func (s *ShopifyAdapter) DeleteListing(ctx context.Context, externalID string) error {
	_, _, err := s.do(ctx, "DELETE", fmt.Sprintf("/products/%s.json", externalID), nil)
	return err
}

func (s *ShopifyAdapter) PublishListing(ctx context.Context, externalID string) error {
	body := map[string]interface{}{"product": map[string]interface{}{"id": externalID, "status": "active"}}
	_, _, err := s.do(ctx, "PUT", fmt.Sprintf("/products/%s.json", externalID), body)
	return err
}

func (s *ShopifyAdapter) UnpublishListing(ctx context.Context, externalID string) error {
	body := map[string]interface{}{"product": map[string]interface{}{"id": externalID, "status": "draft"}}
	_, _, err := s.do(ctx, "PUT", fmt.Sprintf("/products/%s.json", externalID), body)
	return err
}

// ── BULK OPERATIONS ────────────────────────────────────────────────────────

func (s *ShopifyAdapter) BulkCreateListings(ctx context.Context, listings []marketplace.ListingData) ([]marketplace.ListingResult, error) {
	var results []marketplace.ListingResult
	for _, l := range listings {
		r, err := s.CreateListing(ctx, l)
		if err != nil {
			sku := ""
			if l.Attributes != nil {
				if v, ok := l.Attributes["sku"].(string); ok {
					sku = v
				}
			}
			results = append(results, marketplace.ListingResult{
				SKU:    sku,
				Status: "error",
				Errors: []marketplace.ValidationError{{Field: "general", Message: err.Error()}},
			})
			continue
		}
		results = append(results, *r)
	}
	return results, nil
}

func (s *ShopifyAdapter) BulkUpdateListings(ctx context.Context, updates []marketplace.ListingUpdate) ([]marketplace.UpdateResult, error) {
	var results []marketplace.UpdateResult
	for _, u := range updates {
		// ListingUpdate uses Updates map, convert to ListingData for our UpdateListing method
		title, _ := u.Updates["title"].(string)
		desc, _ := u.Updates["body_html"].(string)
		err := s.UpdateListing(ctx, u.ExternalID, marketplace.ListingData{Title: title, Description: desc})
		result := marketplace.UpdateResult{
			ExternalID: u.ExternalID,
			Success:    err == nil,
			UpdatedAt:  time.Now(),
		}
		if err != nil {
			result.Errors = []marketplace.ValidationError{{Field: "general", Message: err.Error()}}
		}
		results = append(results, result)
	}
	return results, nil
}

// ── SYNC & MONITORING ──────────────────────────────────────────────────────

func (s *ShopifyAdapter) GetListingStatus(ctx context.Context, externalID string) (*marketplace.ListingStatus, error) {
	data, _, err := s.do(ctx, "GET", fmt.Sprintf("/products/%s.json?fields=id,status,published_at", externalID), nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Product struct {
			ID          int64     `json:"id"`
			Status      string    `json:"status"`
			PublishedAt time.Time `json:"published_at"`
		} `json:"product"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	return &marketplace.ListingStatus{
		ExternalID: externalID,
		Status:     resp.Product.Status,
		IsActive:   resp.Product.Status == "active",
	}, nil
}

func (s *ShopifyAdapter) SyncInventory(ctx context.Context, externalID string, quantity int) error {
	// externalID here is inventory_item_id. We need to find the location first.
	// This is a simplified version — in production you'd cache the location ID.
	locsData, _, err := s.do(ctx, "GET", "/locations.json", nil)
	if err != nil {
		return err
	}
	var locsResp struct {
		Locations []struct {
			ID int64 `json:"id"`
		} `json:"locations"`
	}
	if err := json.Unmarshal(locsData, &locsResp); err != nil || len(locsResp.Locations) == 0 {
		return fmt.Errorf("could not determine Shopify location")
	}
	locationID := locsResp.Locations[0].ID

	body := map[string]interface{}{
		"location_id":       locationID,
		"inventory_item_id": externalID,
		"available":         quantity,
	}
	_, _, err = s.do(ctx, "POST", "/inventory_levels/set.json", body)
	return err
}

func (s *ShopifyAdapter) SyncPrice(ctx context.Context, externalID string, price float64) error {
	// Get variant ID first
	data, _, err := s.do(ctx, "GET", fmt.Sprintf("/products/%s/variants.json", externalID), nil)
	if err != nil {
		return err
	}
	var resp struct {
		Variants []shopifyVariant `json:"variants"`
	}
	if err := json.Unmarshal(data, &resp); err != nil || len(resp.Variants) == 0 {
		return fmt.Errorf("no variants found for product %s", externalID)
	}
	variantID := resp.Variants[0].ID
	body := map[string]interface{}{
		"variant": map[string]interface{}{
			"id":    variantID,
			"price": fmt.Sprintf("%.2f", price),
		},
	}
	_, _, err = s.do(ctx, "PUT", fmt.Sprintf("/variants/%d.json", variantID), body)
	return err
}

// ── METADATA ───────────────────────────────────────────────────────────────

func (s *ShopifyAdapter) GetName() string        { return "shopify" }
func (s *ShopifyAdapter) GetDisplayName() string { return "Shopify" }
func (s *ShopifyAdapter) GetSupportedFeatures() []string {
	return []string{"import", "export", "inventory_sync", "price_sync", "publish", "unpublish"}
}
func (s *ShopifyAdapter) GetRequiredFields() []marketplace.RequiredField {
	return []marketplace.RequiredField{
		{Name: "title", Type: "string", Description: "Product title"},
		{Name: "price", Type: "number", Description: "Listing price"},
	}
}
func (s *ShopifyAdapter) GetCategories(_ context.Context) ([]marketplace.Category, error) {
	return []marketplace.Category{}, nil // Shopify uses custom product types, not a category tree
}
func (s *ShopifyAdapter) ValidateListing(_ context.Context, listing marketplace.ListingData) (*marketplace.ValidationResult, error) {
	var errs []marketplace.ValidationError
	if listing.Title == "" {
		errs = append(errs, marketplace.ValidationError{Field: "title", Message: "Title is required", Severity: "error"})
	}
	if listing.Price <= 0 {
		errs = append(errs, marketplace.ValidationError{Field: "price", Message: "Price must be greater than 0", Severity: "error"})
	}
	return &marketplace.ValidationResult{IsValid: len(errs) == 0, Errors: errs}, nil
}

// ── HELPER: convert Shopify product to marketplace.MarketplaceProduct ──────

func (s *ShopifyAdapter) toMarketplaceProduct(p shopifyProduct) marketplace.MarketplaceProduct {
	attrs := map[string]interface{}{"tags": p.Tags}

	mp := marketplace.MarketplaceProduct{
		ExternalID:  strconv.FormatInt(p.ID, 10),
		Title:       p.Title,
		Description: p.BodyHTML,
		Brand:       p.Vendor,
		Categories:  []string{p.ProductType},
		ListingURL:  fmt.Sprintf("https://%s/products/%s", s.shopDomain, p.Handle),
		Attributes:  attrs,
	}

	// Map status
	switch p.Status {
	case "active":
		mp.IsInStock = true
	}

	// Primary variant
	if len(p.Variants) > 0 {
		v := p.Variants[0]
		mp.SKU = v.SKU
		if price, err := strconv.ParseFloat(v.Price, 64); err == nil {
			mp.Price = price
		}
		mp.Quantity = v.InventoryQuantity
		mp.IsInStock = v.InventoryQuantity > 0
		// Store barcode in EAN (best-effort)
		if v.Barcode != "" {
			mp.Identifiers = marketplace.Identifiers{EAN: v.Barcode}
		}
		if v.Weight > 0 {
			mp.Weight = &marketplace.Weight{Value: v.Weight, Unit: v.WeightUnit}
		}
	}

	// Images
	for i, img := range p.Images {
		isMain := i == 0
		mp.Images = append(mp.Images, marketplace.ImageData{
			URL:      img.Src,
			Position: i + 1,
			IsMain:   isMain,
		})
	}

	// Multi-variant products
	if len(p.Variants) > 1 {
		for _, v := range p.Variants {
			price, _ := strconv.ParseFloat(v.Price, 64)
			mp.Variations = append(mp.Variations, marketplace.Variation{
				ExternalID: strconv.FormatInt(v.ID, 10),
				SKU:        v.SKU,
				Price:      price,
				Quantity:   v.InventoryQuantity,
				Attributes: map[string]interface{}{"option1": v.Option1, "option2": v.Option2},
			})
		}
	}

	return mp
}

// CancelOrder — Shopify supports order cancellation via REST API.
// Full implementation deferred; returns ErrCancelNotSupported until client method is added.
func (s *ShopifyAdapter) CancelOrder(ctx context.Context, externalOrderID string) error {
	return marketplace.ErrCancelNotSupported
}
