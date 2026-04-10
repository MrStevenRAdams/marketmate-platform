package adapters

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"module-a/marketplace"
	"module-a/marketplace/clients/tiktok"
)

// ============================================================================
// TIKTOK SHOP MARKETPLACE ADAPTER
// ============================================================================
// Implements marketplace.MarketplaceAdapter for TikTok Shop.
// Credential fields: app_key, app_secret, access_token, refresh_token, shop_id
// ============================================================================

type TikTokAdapter struct {
	credentials marketplace.Credentials
	client      *tiktok.Client
}

func NewTikTokAdapter(ctx context.Context, credentials marketplace.Credentials) (marketplace.MarketplaceAdapter, error) {
	appKey := credentials.Data["app_key"]
	appSecret := credentials.Data["app_secret"]
	accessToken := credentials.Data["access_token"]
	shopID := credentials.Data["shop_id"]

	if appKey == "" || appSecret == "" {
		return nil, fmt.Errorf("app_key and app_secret are required for TikTok Shop")
	}
	if accessToken == "" {
		return nil, fmt.Errorf("access_token is required — complete the OAuth flow first")
	}

	client := tiktok.NewClient(appKey, appSecret, accessToken, shopID)

	return &TikTokAdapter{
		credentials: credentials,
		client:      client,
	}, nil
}

// ── Connection ────────────────────────────────────────────────────────────────

func (a *TikTokAdapter) Connect(ctx context.Context, credentials marketplace.Credentials) error {
	return nil
}

func (a *TikTokAdapter) Disconnect(ctx context.Context) error {
	return nil
}

func (a *TikTokAdapter) TestConnection(ctx context.Context) error {
	shops, err := a.client.GetAuthorizedShops()
	if err != nil {
		return fmt.Errorf("TikTok connection test failed: %w", err)
	}
	if len(shops) == 0 {
		return fmt.Errorf("TikTok: no authorized shops found")
	}
	return nil
}

func (a *TikTokAdapter) RefreshAuth(ctx context.Context) error {
	refreshToken := a.credentials.Data["refresh_token"]
	if refreshToken == "" {
		return fmt.Errorf("no refresh_token stored — re-authorize via OAuth")
	}
	_, err := a.client.RefreshToken(refreshToken)
	return err
}

func (a *TikTokAdapter) GetConnectionStatus(ctx context.Context) (*marketplace.ConnectionStatus, error) {
	err := a.TestConnection(ctx)
	return &marketplace.ConnectionStatus{
		IsConnected:  err == nil,
		ErrorMessage: func() string { if err != nil { return err.Error() }; return "" }(),
		LastChecked:  time.Now(),
	}, nil
}

// ── Product Import ────────────────────────────────────────────────────────────

func (a *TikTokAdapter) FetchListings(ctx context.Context, filters marketplace.ImportFilters) ([]marketplace.MarketplaceProduct, error) {
	var products []marketplace.MarketplaceProduct
	pageToken := filters.PageToken

	rawProducts, nextToken, _, err := a.client.GetProducts(pageToken, filters.PageSize)
	if err != nil {
		return nil, fmt.Errorf("TikTok FetchListings: %w", err)
	}

	for _, rp := range rawProducts {
		mp := convertRawProductToMarketplace(rp)
		products = append(products, mp)
	}

	// Store next page token via callback if set
	if filters.ProductCallback != nil {
		for _, p := range products {
			if !filters.ProductCallback(p) {
				break
			}
		}
	}
	_ = nextToken
	return products, nil
}

func (a *TikTokAdapter) FetchProduct(ctx context.Context, externalID string) (*marketplace.MarketplaceProduct, error) {
	raw, err := a.client.GetProduct(externalID)
	if err != nil {
		return nil, fmt.Errorf("TikTok FetchProduct %s: %w", externalID, err)
	}
	mp := convertRawProductToMarketplace(raw)
	return &mp, nil
}

func (a *TikTokAdapter) FetchProductImages(ctx context.Context, externalID string) ([]marketplace.ImageData, error) {
	raw, err := a.client.GetProduct(externalID)
	if err != nil {
		return nil, err
	}
	var images []marketplace.ImageData
	if imgs, ok := raw["main_images"].([]interface{}); ok {
		for i, img := range imgs {
			if m, ok := img.(map[string]interface{}); ok {
				if uri, ok := m["thumb_url"].(string); ok {
					images = append(images, marketplace.ImageData{
						URL:    uri,
						IsMain: i == 0,
					})
				}
			}
		}
	}
	return images, nil
}

func (a *TikTokAdapter) FetchInventory(ctx context.Context, externalID string) (*marketplace.InventoryLevel, error) {
	raw, err := a.client.GetProduct(externalID)
	if err != nil {
		return nil, err
	}
	totalQty := 0
	if skus, ok := raw["skus"].([]interface{}); ok {
		for _, sku := range skus {
			if s, ok := sku.(map[string]interface{}); ok {
				if invList, ok := s["inventory"].([]interface{}); ok {
					for _, inv := range invList {
						if m, ok := inv.(map[string]interface{}); ok {
							if qty, ok := m["quantity"].(float64); ok {
								totalQty += int(qty)
							}
						}
					}
				}
			}
		}
	}
	return &marketplace.InventoryLevel{
		ExternalID: externalID,
		Quantity:   totalQty,
	}, nil
}

// ── Listing Management ────────────────────────────────────────────────────────

func (a *TikTokAdapter) CreateListing(ctx context.Context, listing marketplace.ListingData) (*marketplace.ListingResult, error) {
	req, err := a.buildProductRequest(listing)
	if err != nil {
		return nil, fmt.Errorf("build TikTok product request: %w", err)
	}

	result, err := a.client.CreateProduct(req)
	if err != nil {
		return nil, fmt.Errorf("TikTok CreateProduct: %w", err)
	}

	return &marketplace.ListingResult{
		ExternalID: result.ProductID,
		Status:     "active",
	}, nil
}

func (a *TikTokAdapter) UpdateListing(ctx context.Context, externalID string, updates marketplace.ListingData) error {
	req, err := a.buildProductRequest(updates)
	if err != nil {
		return fmt.Errorf("build TikTok update request: %w", err)
	}
	_, err = a.client.UpdateProduct(externalID, req)
	return err
}

func (a *TikTokAdapter) DeleteListing(ctx context.Context, externalID string) error {
	return a.client.DeleteProduct([]string{externalID})
}

func (a *TikTokAdapter) PublishListing(ctx context.Context, externalID string) error {
	// TikTok products are live once created — no separate publish step needed
	return nil
}

func (a *TikTokAdapter) UnpublishListing(ctx context.Context, externalID string) error {
	return a.client.DeleteProduct([]string{externalID})
}

// ── Bulk Operations ───────────────────────────────────────────────────────────

func (a *TikTokAdapter) BulkCreateListings(ctx context.Context, listings []marketplace.ListingData) ([]marketplace.ListingResult, error) {
	var results []marketplace.ListingResult
	for _, l := range listings {
		r, err := a.CreateListing(ctx, l)
		if err != nil {
			results = append(results, marketplace.ListingResult{Status: "error"})
		} else {
			results = append(results, *r)
		}
	}
	return results, nil
}

func (a *TikTokAdapter) BulkUpdateListings(ctx context.Context, updates []marketplace.ListingUpdate) ([]marketplace.UpdateResult, error) {
	var results []marketplace.UpdateResult
	for _, u := range updates {
		listing := marketplace.ListingData{
			Title:    fmt.Sprintf("%v", u.Updates["title"]),
			Price:    toFloat64(u.Updates["price"]),
			Quantity: toInt(u.Updates["quantity"]),
		}
		err := a.UpdateListing(ctx, u.ExternalID, listing)
		r := marketplace.UpdateResult{ExternalID: u.ExternalID, Success: err == nil, UpdatedAt: time.Now()}
		if err != nil {
			r.Errors = []marketplace.ValidationError{{Field: "update", Message: err.Error()}}
		}
		results = append(results, r)
	}
	return results, nil
}

// ── Sync ──────────────────────────────────────────────────────────────────────

func (a *TikTokAdapter) GetListingStatus(ctx context.Context, externalID string) (*marketplace.ListingStatus, error) {
	raw, err := a.client.GetProduct(externalID)
	if err != nil {
		return nil, err
	}
	status := "active"
	if s, ok := raw["status"].(string); ok {
		status = s
	}
	return &marketplace.ListingStatus{ExternalID: externalID, Status: status}, nil
}

func (a *TikTokAdapter) SyncInventory(ctx context.Context, externalID string, quantity int) error {
	// TikTok inventory sync requires SKU-level update
	raw, err := a.client.GetProduct(externalID)
	if err != nil {
		return fmt.Errorf("get product for inventory sync: %w", err)
	}

	// Build update request preserving existing data but updating inventory
	if skus, ok := raw["skus"].([]interface{}); ok && len(skus) > 0 {
		// Get the warehouse ID from first SKU's inventory
		warehouseID := ""
		if sku, ok := skus[0].(map[string]interface{}); ok {
			if inv, ok := sku["inventory"].([]interface{}); ok && len(inv) > 0 {
				if m, ok := inv[0].(map[string]interface{}); ok {
					if wid, ok := m["warehouse_id"].(string); ok {
						warehouseID = wid
					}
				}
			}
		}
		if warehouseID == "" {
			return fmt.Errorf("could not determine warehouse_id for inventory sync")
		}

		body := map[string]interface{}{
			"skus": []map[string]interface{}{
				{
					"inventory": []map[string]interface{}{
						{
							"quantity":     quantity,
							"warehouse_id": warehouseID,
						},
					},
				},
			},
		}
		_, err = a.client.PutJSON("/api/v2/product/products/"+externalID, body)
		return err
	}
	return fmt.Errorf("no SKUs found for product %s", externalID)
}

func (a *TikTokAdapter) SyncPrice(ctx context.Context, externalID string, price float64) error {
	body := map[string]interface{}{
		"skus": []map[string]interface{}{
			{
				"price": map[string]interface{}{
					"original_price": strconv.FormatFloat(price, 'f', 2, 64),
				},
			},
		},
	}
	_, err := a.client.PutJSON("/api/v2/product/products/"+externalID, body)
	return err
}

// ── Metadata ──────────────────────────────────────────────────────────────────

func (a *TikTokAdapter) GetName() string        { return "tiktok" }
func (a *TikTokAdapter) GetDisplayName() string { return "TikTok Shop" }

func (a *TikTokAdapter) GetSupportedFeatures() []string {
	return []string{"import", "listing", "order_sync", "tracking", "inventory_sync", "price_sync"}
}

func (a *TikTokAdapter) GetRequiredFields() []marketplace.RequiredField {
	return []marketplace.RequiredField{
		{Name: "app_key", Type: "string", Description: "TikTok Shop App Key"},
		{Name: "app_secret", Type: "string", Description: "TikTok Shop App Secret"},
		{Name: "access_token", Type: "string", Description: "OAuth Access Token"},
		{Name: "refresh_token", Type: "string", Description: "OAuth Refresh Token"},
		{Name: "shop_id", Type: "string", Description: "TikTok Shop ID"},
	}
}

func (a *TikTokAdapter) GetCategories(ctx context.Context) ([]marketplace.Category, error) {
	cats, err := a.client.GetCategories()
	if err != nil {
		return nil, err
	}
	var result []marketplace.Category
	for _, c := range cats {
		result = append(result, marketplace.Category{
			ID:       strconv.FormatInt(c.ID, 10),
			Name:     c.LocalName,
			ParentID: strconv.FormatInt(c.ParentID, 10),
		})
	}
	return result, nil
}

func (a *TikTokAdapter) ValidateListing(ctx context.Context, listing marketplace.ListingData) (*marketplace.ValidationResult, error) {
	result := &marketplace.ValidationResult{IsValid: true}
	if listing.Title == "" {
		result.IsValid = false
		result.Errors = append(result.Errors, marketplace.ValidationError{Field: "title", Message: "Title is required"})
	}
	if len(listing.Title) > 255 {
		result.IsValid = false
		result.Errors = append(result.Errors, marketplace.ValidationError{Field: "title", Message: "Title must be 255 characters or less"})
	}
	if listing.Price <= 0 {
		result.IsValid = false
		result.Errors = append(result.Errors, marketplace.ValidationError{Field: "price", Message: "Price must be greater than 0"})
	}
	if listing.CategoryID == "" {
		result.IsValid = false
		result.Errors = append(result.Errors, marketplace.ValidationError{Field: "category_id", Message: "Category is required"})
	}
	if len(listing.Images) == 0 {
		result.IsValid = false
		result.Errors = append(result.Errors, marketplace.ValidationError{Field: "images", Message: "At least one image is required"})
	}
	return result, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func (a *TikTokAdapter) buildProductRequest(listing marketplace.ListingData) (*tiktok.CreateProductRequest, error) {
	catID, _ := strconv.ParseInt(listing.CategoryID, 10, 64)

	req := &tiktok.CreateProductRequest{
		Title:       listing.Title,
		Description: listing.Description,
		CategoryID:  catID,
	}
	if listing.Attributes != nil {
		if brandID, ok := listing.Attributes["brand_id"].(string); ok {
			req.BrandID = brandID
		}
	}

	// Images: upload each URL to TikTok's CDN
	for _, imgURL := range listing.Images {
		uploaded, err := a.client.UploadImageFromURL(imgURL)
		if err != nil {
			return nil, fmt.Errorf("upload image %s: %w", imgURL, err)
		}
		req.MainImages = append(req.MainImages, *uploaded)
	}

	// Get first warehouse for inventory
	warehouses, err := a.client.GetWarehouses()
	warehouseID := ""
	if err == nil && len(warehouses) > 0 {
		warehouseID = warehouses[0].ID
	}

	// Build SKU(s)
	sku := tiktok.ProductSKU{
		OuterID: func() string { if listing.Attributes != nil { if s, ok := listing.Attributes["sku"].(string); ok { return s } }; return "" }(),
	}
	sku.Price.Currency = "GBP"
	sku.Price.OriginalPrice = strconv.FormatFloat(listing.Price, 'f', 2, 64)
	if warehouseID != "" {
		sku.Inventory = append(sku.Inventory, struct {
			Quantity    int    `json:"quantity"`
			WarehouseID string `json:"warehouse_id"`
		}{Quantity: listing.Quantity, WarehouseID: warehouseID})
	}
	req.SKUs = []tiktok.ProductSKU{sku}

	// Shipping template
	if tmpl, ok := listing.Attributes["shipping_template_id"].(string); ok && tmpl != "" {
		req.ShippingTemplateID = tmpl
	}

	// Package weight
	if wt, ok := listing.Attributes["weight_kg"].(string); ok && wt != "" {
		if w, err := strconv.ParseFloat(wt, 64); err == nil {
			req.PackageWeight.Unit = "KILOGRAM"
			req.PackageWeight.Value = w
		}
	}

	return req, nil
}

func convertRawProductToMarketplace(raw map[string]interface{}) marketplace.MarketplaceProduct {
	mp := marketplace.MarketplaceProduct{
		RawData: raw,
	}
	if id, ok := raw["id"].(string); ok {
		mp.ExternalID = id
	}
	if title, ok := raw["title"].(string); ok {
		mp.Title = title
	}
	if desc, ok := raw["description"].(string); ok {
		mp.Description = desc
	}

	// Price — from first SKU
	if skus, ok := raw["skus"].([]interface{}); ok && len(skus) > 0 {
		if sku, ok := skus[0].(map[string]interface{}); ok {
			if price, ok := sku["price"].(map[string]interface{}); ok {
				if p, ok := price["original_price"].(string); ok {
					if f, err := strconv.ParseFloat(p, 64); err == nil {
						mp.Price = f
					}
				}
				if curr, ok := price["currency"].(string); ok {
					mp.Currency = curr
				}
			}
			if invList, ok := sku["inventory"].([]interface{}); ok {
				for _, inv := range invList {
					if m, ok := inv.(map[string]interface{}); ok {
						if qty, ok := m["quantity"].(float64); ok {
							mp.Quantity += int(qty)
						}
					}
				}
			}
			if sellerSKU, ok := sku["seller_sku"].(string); ok {
				mp.SKU = sellerSKU
			}
		}
	}

	mp.IsInStock = mp.Quantity > 0

	// Images
	if imgs, ok := raw["main_images"].([]interface{}); ok {
		for i, img := range imgs {
			if m, ok := img.(map[string]interface{}); ok {
				imgURL := ""
				if url, ok := m["thumb_url"].(string); ok {
					imgURL = url
				} else if url, ok := m["url_list"].([]interface{}); ok && len(url) > 0 {
					imgURL = fmt.Sprintf("%v", url[0])
				}
				if imgURL != "" {
					mp.Images = append(mp.Images, marketplace.ImageData{
						URL:    imgURL,
						IsMain: i == 0,
					})
				}
			}
		}
	}

	return mp
}

// ── Type conversion helpers ───────────────────────────────────────────────────

func toFloat64(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case string:
		f, _ := strconv.ParseFloat(n, 64)
		return f
	}
	return 0
}

func toInt(v interface{}) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case string:
		i, _ := strconv.Atoi(n)
		return i
	}
	return 0
}

func (a *TikTokAdapter) CancelOrder(ctx context.Context, externalOrderID string) error {
	return marketplace.ErrCancelNotSupported
}
