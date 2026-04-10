package adapters

// ============================================================================
// SHOPLINE MARKETPLACE ADAPTER
// ============================================================================
// Implements marketplace.MarketplaceAdapter for Shopline stores.
//
// Credentials required:
//   shop_id       — Shopline store ID (subdomain without .myshopline.com)
//   access_token  — OAuth access token from Shopline Partner Platform
//   api_version   — e.g. "v2" (defaults to "v2" if blank)
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

type ShoplineAdapter struct {
	credentials marketplace.Credentials
	shopID      string
	accessToken string
	apiVersion  string
	httpClient  *http.Client
}

func NewShoplineAdapter(_ context.Context, credentials marketplace.Credentials) (marketplace.MarketplaceAdapter, error) {
	shopID := credentials.Data["shop_id"]
	accessToken := credentials.Data["access_token"]
	apiVersion := credentials.Data["api_version"]
	if shopID == "" {
		return nil, fmt.Errorf("shop_id is required for Shopline")
	}
	if accessToken == "" {
		return nil, fmt.Errorf("access_token is required for Shopline")
	}
	if apiVersion == "" {
		apiVersion = "v2"
	}
	return &ShoplineAdapter{
		credentials: credentials,
		shopID:      shopID,
		accessToken: accessToken,
		apiVersion:  apiVersion,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (s *ShoplineAdapter) apiURL(path string) string {
	return fmt.Sprintf("https://open.shopline.io/api/%s/%s%s", s.apiVersion, s.shopID, path)
}

func (s *ShoplineAdapter) do(ctx context.Context, method, path string, body interface{}) ([]byte, int, error) {
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
	req.Header.Set("Authorization", "Bearer "+s.accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
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
		return data, resp.StatusCode, fmt.Errorf("shopline API error %d: %s", resp.StatusCode, string(data))
	}
	return data, resp.StatusCode, nil
}

// ── Shopline API types ─────────────────────────────────────────────────────

type shoplineProduct struct {
	ID          string            `json:"id"`
	Title       string            `json:"title"`
	BodyHTML    string            `json:"body_html"`
	Vendor      string            `json:"vendor"`
	ProductType string            `json:"product_type"`
	Handle      string            `json:"handle"`
	Status      string            `json:"status"`
	Tags        interface{}       `json:"tags"`
	Variants    []shoplineVariant `json:"variants"`
	Images      []shoplineImage   `json:"images"`
}

type shoplineVariant struct {
	ID                string  `json:"id"`
	SKU               string  `json:"sku"`
	Price             string  `json:"price"`
	InventoryQuantity int     `json:"inventory_quantity"`
	Weight            float64 `json:"weight"`
	WeightUnit        string  `json:"weight_unit"`
	Barcode           string  `json:"barcode"`
	Option1           string  `json:"option1"`
	Option2           string  `json:"option2"`
	Option3           string  `json:"option3"`
}

type shoplineImage struct {
	Src      string `json:"src"`
	Position int    `json:"position"`
}

// ── CONNECTION ─────────────────────────────────────────────────────────────

func (s *ShoplineAdapter) Connect(_ context.Context, _ marketplace.Credentials) error    { return nil }
func (s *ShoplineAdapter) Disconnect(_ context.Context) error                            { return nil }
func (s *ShoplineAdapter) RefreshAuth(_ context.Context) error                           { return nil }

func (s *ShoplineAdapter) TestConnection(ctx context.Context) error {
	_, _, err := s.do(ctx, "GET", "/shop.json", nil)
	return err
}

func (s *ShoplineAdapter) GetConnectionStatus(ctx context.Context) (*marketplace.ConnectionStatus, error) {
	err := s.TestConnection(ctx)
	st := &marketplace.ConnectionStatus{IsConnected: err == nil, LastChecked: time.Now()}
	if err != nil {
		st.ErrorMessage = err.Error()
	} else {
		st.LastSuccessful = time.Now()
	}
	return st, nil
}

// ── IMPORT ─────────────────────────────────────────────────────────────────

func (s *ShoplineAdapter) FetchListings(ctx context.Context, filters marketplace.ImportFilters) ([]marketplace.MarketplaceProduct, error) {
	limit := filters.PageSize
	if limit <= 0 || limit > 250 {
		limit = 250
	}
	statusFilter := "active,draft"
	if filters.StockFilter == "in_stock" {
		statusFilter = "active"
	}
	url := fmt.Sprintf("/products.json?status=%s&limit=%d", statusFilter, limit)
	if filters.SearchQuery != "" {
		url += "&title=" + filters.SearchQuery
	}
	data, _, err := s.do(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Products []shoplineProduct `json:"products"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	results := make([]marketplace.MarketplaceProduct, 0, len(resp.Products))
	for _, p := range resp.Products {
		mp := s.toMarketplaceProduct(p)
		results = append(results, mp)
		if filters.ProductCallback != nil && !filters.ProductCallback(mp) {
			break
		}
	}
	return results, nil
}

func (s *ShoplineAdapter) FetchProduct(ctx context.Context, externalID string) (*marketplace.MarketplaceProduct, error) {
	data, _, err := s.do(ctx, "GET", fmt.Sprintf("/products/%s.json", externalID), nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Product shoplineProduct `json:"product"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	mp := s.toMarketplaceProduct(resp.Product)
	return &mp, nil
}

func (s *ShoplineAdapter) FetchProductImages(ctx context.Context, externalID string) ([]marketplace.ImageData, error) {
	data, _, err := s.do(ctx, "GET", fmt.Sprintf("/products/%s/images.json", externalID), nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Images []shoplineImage `json:"images"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	var images []marketplace.ImageData
	for i, img := range resp.Images {
		images = append(images, marketplace.ImageData{URL: img.Src, Position: i + 1, IsMain: i == 0})
	}
	return images, nil
}

func (s *ShoplineAdapter) FetchInventory(ctx context.Context, externalID string) (*marketplace.InventoryLevel, error) {
	data, _, err := s.do(ctx, "GET", fmt.Sprintf("/inventory_levels.json?variant_ids=%s", externalID), nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		InventoryLevels []struct {
			Available int `json:"available"`
		} `json:"inventory_levels"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	qty := 0
	if len(resp.InventoryLevels) > 0 {
		qty = resp.InventoryLevels[0].Available
	}
	return &marketplace.InventoryLevel{ExternalID: externalID, Quantity: qty, UpdatedAt: time.Now()}, nil
}

// ── EXPORT ─────────────────────────────────────────────────────────────────

func (s *ShoplineAdapter) CreateListing(ctx context.Context, listing marketplace.ListingData) (*marketplace.ListingResult, error) {
	sku := ""
	if listing.Attributes != nil {
		if v, ok := listing.Attributes["sku"].(string); ok {
			sku = v
		}
	}
	brand := ""
	if listing.Attributes != nil {
		if v, ok := listing.Attributes["brand"].(string); ok {
			brand = v
		}
	}

	variantPayload := map[string]interface{}{
		"sku":   sku,
		"price": fmt.Sprintf("%.2f", listing.Price),
	}
	if listing.Quantity > 0 {
		variantPayload["inventory_quantity"] = listing.Quantity
		variantPayload["inventory_management"] = "shopline"
	}
	if listing.Weight != nil {
		variantPayload["weight"] = listing.Weight.Value
		variantPayload["weight_unit"] = listing.Weight.Unit
	}

	productPayload := map[string]interface{}{
		"title":     listing.Title,
		"body_html": listing.Description,
		"vendor":    brand,
		"status":    "active",
		"variants":  []map[string]interface{}{variantPayload},
	}

	if len(listing.Images) > 0 {
		imgs := make([]map[string]interface{}, 0, len(listing.Images))
		for i, src := range listing.Images {
			imgs = append(imgs, map[string]interface{}{"src": src, "position": i + 1})
		}
		productPayload["images"] = imgs
	}

	data, _, err := s.do(ctx, "POST", "/products.json", map[string]interface{}{"product": productPayload})
	if err != nil {
		return &marketplace.ListingResult{SKU: sku, Status: "error", Errors: []marketplace.ValidationError{{Field: "general", Message: err.Error()}}}, nil
	}

	var resp struct {
		Product struct {
			ID     string `json:"id"`
			Handle string `json:"handle"`
			Status string `json:"status"`
		} `json:"product"`
	}
	json.Unmarshal(data, &resp)

	return &marketplace.ListingResult{
		ExternalID: resp.Product.ID,
		SKU:        sku,
		Status:     resp.Product.Status,
		URL:        fmt.Sprintf("https://%s.myshopline.com/products/%s", s.shopID, resp.Product.Handle),
		CreatedAt:  time.Now(),
	}, nil
}

func (s *ShoplineAdapter) UpdateListing(ctx context.Context, externalID string, updates marketplace.ListingData) error {
	body := map[string]interface{}{"product": map[string]interface{}{"id": externalID, "title": updates.Title, "body_html": updates.Description}}
	_, _, err := s.do(ctx, "PUT", fmt.Sprintf("/products/%s.json", externalID), body)
	return err
}

func (s *ShoplineAdapter) DeleteListing(ctx context.Context, externalID string) error {
	_, _, err := s.do(ctx, "DELETE", fmt.Sprintf("/products/%s.json", externalID), nil)
	return err
}

func (s *ShoplineAdapter) PublishListing(ctx context.Context, externalID string) error {
	_, _, err := s.do(ctx, "PUT", fmt.Sprintf("/products/%s.json", externalID), map[string]interface{}{"product": map[string]interface{}{"id": externalID, "status": "active"}})
	return err
}

func (s *ShoplineAdapter) UnpublishListing(ctx context.Context, externalID string) error {
	_, _, err := s.do(ctx, "PUT", fmt.Sprintf("/products/%s.json", externalID), map[string]interface{}{"product": map[string]interface{}{"id": externalID, "status": "draft"}})
	return err
}

func (s *ShoplineAdapter) BulkCreateListings(ctx context.Context, listings []marketplace.ListingData) ([]marketplace.ListingResult, error) {
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
			results = append(results, marketplace.ListingResult{SKU: sku, Status: "error", Errors: []marketplace.ValidationError{{Field: "general", Message: err.Error()}}})
			continue
		}
		results = append(results, *r)
	}
	return results, nil
}

func (s *ShoplineAdapter) BulkUpdateListings(ctx context.Context, updates []marketplace.ListingUpdate) ([]marketplace.UpdateResult, error) {
	var results []marketplace.UpdateResult
	for _, u := range updates {
		title, _ := u.Updates["title"].(string)
		desc, _ := u.Updates["body_html"].(string)
		err := s.UpdateListing(ctx, u.ExternalID, marketplace.ListingData{Title: title, Description: desc})
		r := marketplace.UpdateResult{ExternalID: u.ExternalID, Success: err == nil, UpdatedAt: time.Now()}
		if err != nil {
			r.Errors = []marketplace.ValidationError{{Field: "general", Message: err.Error()}}
		}
		results = append(results, r)
	}
	return results, nil
}

// ── SYNC & MONITORING ──────────────────────────────────────────────────────

func (s *ShoplineAdapter) GetListingStatus(ctx context.Context, externalID string) (*marketplace.ListingStatus, error) {
	data, _, err := s.do(ctx, "GET", fmt.Sprintf("/products/%s.json?fields=id,status", externalID), nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Product struct {
			Status string `json:"status"`
		} `json:"product"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	return &marketplace.ListingStatus{ExternalID: externalID, Status: resp.Product.Status, IsActive: resp.Product.Status == "active"}, nil
}

func (s *ShoplineAdapter) SyncInventory(ctx context.Context, externalID string, quantity int) error {
	locsData, _, err := s.do(ctx, "GET", "/locations.json", nil)
	if err != nil {
		return err
	}
	var locsResp struct {
		Locations []struct {
			ID string `json:"id"`
		} `json:"locations"`
	}
	if err := json.Unmarshal(locsData, &locsResp); err != nil || len(locsResp.Locations) == 0 {
		return fmt.Errorf("could not determine Shopline location")
	}
	body := map[string]interface{}{"inventory": map[string]interface{}{"variant_id": externalID, "location_id": locsResp.Locations[0].ID, "quantity": quantity}}
	_, _, err = s.do(ctx, "POST", "/inventory_levels/set.json", body)
	return err
}

func (s *ShoplineAdapter) SyncPrice(ctx context.Context, externalID string, price float64) error {
	body := map[string]interface{}{"variant": map[string]interface{}{"price": fmt.Sprintf("%.2f", price)}}
	_, _, err := s.do(ctx, "PUT", fmt.Sprintf("/variants/%s.json", externalID), body)
	return err
}

func (s *ShoplineAdapter) CancelOrder(ctx context.Context, externalOrderID string) error {
	_, _, err := s.do(ctx, "POST", fmt.Sprintf("/orders/%s/cancel.json", externalOrderID), map[string]interface{}{"reason": "customer"})
	return err
}

// ── METADATA ───────────────────────────────────────────────────────────────

func (s *ShoplineAdapter) GetName() string        { return "shopline" }
func (s *ShoplineAdapter) GetDisplayName() string { return "Shopline" }
func (s *ShoplineAdapter) GetSupportedFeatures() []string {
	return []string{"import", "export", "inventory_sync", "price_sync", "publish", "unpublish"}
}
func (s *ShoplineAdapter) GetRequiredFields() []marketplace.RequiredField {
	return []marketplace.RequiredField{
		{Name: "title", Type: "string", Description: "Product title"},
		{Name: "price", Type: "number", Description: "Listing price"},
	}
}
func (s *ShoplineAdapter) GetCategories(_ context.Context) ([]marketplace.Category, error) {
	return []marketplace.Category{}, nil
}
func (s *ShoplineAdapter) ValidateListing(_ context.Context, listing marketplace.ListingData) (*marketplace.ValidationResult, error) {
	var errs []marketplace.ValidationError
	if listing.Title == "" {
		errs = append(errs, marketplace.ValidationError{Field: "title", Message: "Title is required", Severity: "error"})
	}
	if listing.Price <= 0 {
		errs = append(errs, marketplace.ValidationError{Field: "price", Message: "Price must be greater than 0", Severity: "error"})
	}
	return &marketplace.ValidationResult{IsValid: len(errs) == 0, Errors: errs}, nil
}

// ── HELPER ─────────────────────────────────────────────────────────────────

func (s *ShoplineAdapter) toMarketplaceProduct(p shoplineProduct) marketplace.MarketplaceProduct {
	var tagsStr string
	switch tv := p.Tags.(type) {
	case string:
		tagsStr = tv
	case []interface{}:
		for i, t := range tv {
			if ts, ok := t.(string); ok {
				if i > 0 {
					tagsStr += ","
				}
				tagsStr += ts
			}
		}
	}

	mp := marketplace.MarketplaceProduct{
		ExternalID:  p.ID,
		Title:       p.Title,
		Description: p.BodyHTML,
		Brand:       p.Vendor,
		Categories:  []string{p.ProductType},
		ListingURL:  fmt.Sprintf("https://%s.myshopline.com/products/%s", s.shopID, p.Handle),
		Attributes:  map[string]interface{}{"tags": tagsStr},
	}

	if p.Status == "active" {
		mp.IsInStock = true
	}

	if len(p.Variants) > 0 {
		v := p.Variants[0]
		mp.SKU = v.SKU
		if price, err := strconv.ParseFloat(v.Price, 64); err == nil {
			mp.Price = price
		}
		mp.Quantity = v.InventoryQuantity
		mp.IsInStock = v.InventoryQuantity > 0
		if v.Barcode != "" {
			mp.Identifiers = marketplace.Identifiers{EAN: v.Barcode}
		}
		if v.Weight > 0 {
			mp.Weight = &marketplace.Weight{Value: v.Weight, Unit: v.WeightUnit}
		}
	}

	for i, img := range p.Images {
		mp.Images = append(mp.Images, marketplace.ImageData{URL: img.Src, Position: i + 1, IsMain: i == 0})
	}

	if len(p.Variants) > 1 {
		for _, v := range p.Variants {
			price, _ := strconv.ParseFloat(v.Price, 64)
			mp.Variations = append(mp.Variations, marketplace.Variation{
				ExternalID: v.ID,
				SKU:        v.SKU,
				Price:      price,
				Quantity:   v.InventoryQuantity,
				Attributes: map[string]interface{}{"option1": v.Option1, "option2": v.Option2, "option3": v.Option3},
			})
		}
	}

	return mp
}
