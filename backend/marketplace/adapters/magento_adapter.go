package adapters

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"module-a/marketplace"
	"module-a/marketplace/clients/magento"
)

// ============================================================================
// MAGENTO 2 MARKETPLACE ADAPTER
// ============================================================================
// Implements marketplace.MarketplaceAdapter for Magento 2 stores.
// Credential fields: store_url, integration_token
// ============================================================================

type MagentoAdapter struct {
	credentials marketplace.Credentials
	client      *magento.Client
}

func NewMagentoAdapter(ctx context.Context, credentials marketplace.Credentials) (marketplace.MarketplaceAdapter, error) {
	storeURL := credentials.Data["store_url"]
	integrationToken := credentials.Data["integration_token"]

	if storeURL == "" {
		return nil, fmt.Errorf("store_url is required for Magento")
	}
	if integrationToken == "" {
		return nil, fmt.Errorf("integration_token is required for Magento")
	}

	client := magento.NewClient(storeURL, integrationToken)

	return &MagentoAdapter{
		credentials: credentials,
		client:      client,
	}, nil
}

// ── Connection ────────────────────────────────────────────────────────────────

func (a *MagentoAdapter) Connect(ctx context.Context, credentials marketplace.Credentials) error {
	return nil
}

func (a *MagentoAdapter) Disconnect(ctx context.Context) error {
	return nil
}

func (a *MagentoAdapter) TestConnection(ctx context.Context) error {
	return a.client.TestConnection()
}

func (a *MagentoAdapter) RefreshAuth(ctx context.Context) error {
	// Magento integration tokens are static — no token refresh needed
	return nil
}

func (a *MagentoAdapter) GetConnectionStatus(ctx context.Context) (*marketplace.ConnectionStatus, error) {
	err := a.TestConnection(ctx)
	return &marketplace.ConnectionStatus{
		IsConnected:  err == nil,
		ErrorMessage: func() string { if err != nil { return err.Error() }; return "" }(),
		LastChecked:  time.Now(),
	}, nil
}

// ── Product Import ────────────────────────────────────────────────────────────

func (a *MagentoAdapter) FetchListings(ctx context.Context, filters marketplace.ImportFilters) ([]marketplace.MarketplaceProduct, error) {
	products, err := a.client.GetAllProducts()
	if err != nil {
		return nil, fmt.Errorf("Magento FetchListings: %w", err)
	}

	var result []marketplace.MarketplaceProduct
	for _, p := range products {
		mp := convertMagentoProduct(p)
		result = append(result, mp)
		if filters.ProductCallback != nil {
			if !filters.ProductCallback(mp) {
				break
			}
		}
	}
	return result, nil
}

func (a *MagentoAdapter) FetchProduct(ctx context.Context, externalID string) (*marketplace.MarketplaceProduct, error) {
	// externalID is the SKU for Magento
	p, err := a.client.GetProduct(externalID)
	if err != nil {
		return nil, fmt.Errorf("Magento FetchProduct %s: %w", externalID, err)
	}
	mp := convertMagentoProduct(*p)
	return &mp, nil
}

func (a *MagentoAdapter) FetchProductImages(ctx context.Context, externalID string) ([]marketplace.ImageData, error) {
	mp, err := a.FetchProduct(ctx, externalID)
	if err != nil {
		return nil, err
	}
	return mp.Images, nil
}

func (a *MagentoAdapter) FetchInventory(ctx context.Context, externalID string) (*marketplace.InventoryLevel, error) {
	mp, err := a.FetchProduct(ctx, externalID)
	if err != nil {
		return nil, err
	}
	return &marketplace.InventoryLevel{
		ExternalID: externalID,
		Quantity:   mp.Quantity,
	}, nil
}

// ── Listing Management ────────────────────────────────────────────────────────

func (a *MagentoAdapter) CreateListing(ctx context.Context, listing marketplace.ListingData) (*marketplace.ListingResult, error) {
	req := buildMagentoProductRequest(listing)
	created, err := a.client.CreateProduct(req)
	if err != nil {
		return nil, fmt.Errorf("Magento CreateProduct: %w", err)
	}
	return &marketplace.ListingResult{
		ExternalID: created.SKU,
		Status:     magentoStatusToString(created.Status),
		URL:        a.credentials.Data["store_url"] + "/catalog/product/view/id/" + strconv.Itoa(created.ID),
	}, nil
}

func (a *MagentoAdapter) UpdateListing(ctx context.Context, externalID string, updates marketplace.ListingData) error {
	req := buildMagentoProductRequest(updates)
	req.SKU = externalID // externalID is the canonical SKU for Magento
	_, err := a.client.UpdateProduct(externalID, req)
	return err
}

func (a *MagentoAdapter) DeleteListing(ctx context.Context, externalID string) error {
	return a.client.DeleteProduct(externalID)
}

func (a *MagentoAdapter) PublishListing(ctx context.Context, externalID string) error {
	return a.client.UpdateProductStatus(externalID, 1) // 1 = enabled
}

func (a *MagentoAdapter) UnpublishListing(ctx context.Context, externalID string) error {
	return a.client.UpdateProductStatus(externalID, 2) // 2 = disabled
}

// ── Bulk ──────────────────────────────────────────────────────────────────────

func (a *MagentoAdapter) BulkCreateListings(ctx context.Context, listings []marketplace.ListingData) ([]marketplace.ListingResult, error) {
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

func (a *MagentoAdapter) BulkUpdateListings(ctx context.Context, updates []marketplace.ListingUpdate) ([]marketplace.UpdateResult, error) {
	var results []marketplace.UpdateResult
	for _, u := range updates {
		listing := marketplace.ListingData{
			Title:    fmt.Sprintf("%v", u.Updates["title"]),
			Price:    toMagentoFloat64(u.Updates["price"]),
			Quantity: toMagentoInt(u.Updates["quantity"]),
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

func (a *MagentoAdapter) GetListingStatus(ctx context.Context, externalID string) (*marketplace.ListingStatus, error) {
	p, err := a.client.GetProduct(externalID)
	if err != nil {
		return nil, err
	}
	return &marketplace.ListingStatus{
		ExternalID: externalID,
		Status:     magentoStatusToString(p.Status),
	}, nil
}

func (a *MagentoAdapter) SyncInventory(ctx context.Context, externalID string, quantity int) error {
	return a.client.UpdateProductStock(externalID, quantity)
}

func (a *MagentoAdapter) SyncPrice(ctx context.Context, externalID string, price float64) error {
	return a.client.UpdateProductPrice(externalID, price)
}

// ── Metadata ──────────────────────────────────────────────────────────────────

func (a *MagentoAdapter) GetName() string        { return "magento" }
func (a *MagentoAdapter) GetDisplayName() string { return "Magento 2" }

func (a *MagentoAdapter) GetSupportedFeatures() []string {
	return []string{"import", "listing", "order_sync", "inventory_sync", "price_sync", "tracking"}
}

func (a *MagentoAdapter) GetRequiredFields() []marketplace.RequiredField {
	return []marketplace.RequiredField{
		{Name: "store_url", Type: "string", Description: "Your Magento 2 store URL (e.g. https://mystore.com)"},
		{Name: "integration_token", Type: "password", Description: "Magento Integration Access Token"},
	}
}

func (a *MagentoAdapter) GetCategories(ctx context.Context) ([]marketplace.Category, error) {
	root, err := a.client.GetCategories()
	if err != nil {
		return nil, err
	}
	flat := magento.FlattenCategories(root)
	var result []marketplace.Category
	for _, c := range flat {
		if !c.IsActive {
			continue
		}
		result = append(result, marketplace.Category{
			ID:       strconv.Itoa(c.ID),
			Name:     c.Name,
			ParentID: strconv.Itoa(c.ParentID),
		})
	}
	return result, nil
}

func (a *MagentoAdapter) ValidateListing(ctx context.Context, listing marketplace.ListingData) (*marketplace.ValidationResult, error) {
	result := &marketplace.ValidationResult{IsValid: true}
	if listing.Title == "" {
		result.IsValid = false
		result.Errors = append(result.Errors, marketplace.ValidationError{Field: "title", Message: "Product name is required"})
	}
	sku := ""
	if listing.Attributes != nil {
		if s, ok := listing.Attributes["sku"].(string); ok {
			sku = s
		}
	}
	if sku == "" {
		result.IsValid = false
		result.Errors = append(result.Errors, marketplace.ValidationError{Field: "sku", Message: "SKU is required for Magento products"})
	}
	if listing.Price < 0 {
		result.IsValid = false
		result.Errors = append(result.Errors, marketplace.ValidationError{Field: "price", Message: "Price must be 0 or greater"})
	}
	return result, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func buildMagentoProductRequest(listing marketplace.ListingData) *magento.Product {
	qty := listing.Quantity
	qtyFloat := float64(qty)
	isInStock := qty > 0

	// SKU comes from Attributes map (ListingData has no top-level SKU field)
	sku := ""
	if listing.Attributes != nil {
		if s, ok := listing.Attributes["sku"].(string); ok {
			sku = s
		}
	}

	product := &magento.Product{
		SKU:            sku,
		Name:           listing.Title,
		Price:          listing.Price,
		Status:         1, // Enabled
		Visibility:     4, // Catalog + Search
		TypeID:         "simple",
		AttributeSetID: 4, // Default
		ExtensionAttributes: &magento.ProductExtensionAttributes{
			StockItem: &magento.StockItem{
				Qty:         qtyFloat,
				IsInStock:   isInStock,
				ManageStock: true,
			},
		},
	}

	// Custom attributes from listing.Attributes
	if listing.Attributes != nil {
		if desc, ok := listing.Attributes["description"].(string); ok && desc != "" {
			product.CustomAttributes = append(product.CustomAttributes,
				magento.ProductCustomAttribute{AttributeCode: "description", Value: desc})
		}
		if shortDesc, ok := listing.Attributes["short_description"].(string); ok && shortDesc != "" {
			product.CustomAttributes = append(product.CustomAttributes,
				magento.ProductCustomAttribute{AttributeCode: "short_description", Value: shortDesc})
		}
		if metaTitle, ok := listing.Attributes["meta_title"].(string); ok && metaTitle != "" {
			product.CustomAttributes = append(product.CustomAttributes,
				magento.ProductCustomAttribute{AttributeCode: "meta_title", Value: metaTitle})
		}
		if urlKey, ok := listing.Attributes["url_key"].(string); ok && urlKey != "" {
			product.CustomAttributes = append(product.CustomAttributes,
				magento.ProductCustomAttribute{AttributeCode: "url_key", Value: urlKey})
		}
		if weight, ok := listing.Attributes["weight"].(float64); ok && weight > 0 {
			product.Weight = weight
		}
		if categoryID, ok := listing.Attributes["category_ids"].(string); ok && categoryID != "" {
			product.CustomAttributes = append(product.CustomAttributes,
				magento.ProductCustomAttribute{AttributeCode: "category_ids", Value: []string{categoryID}})
		}
	}

	// Category from listing.CategoryID
	if listing.CategoryID != "" {
		product.CustomAttributes = append(product.CustomAttributes,
			magento.ProductCustomAttribute{AttributeCode: "category_ids", Value: []string{listing.CategoryID}})
	}

	// Description from listing.Description
	if listing.Description != "" {
		// Check it's not already set via attributes
		found := false
		for _, attr := range product.CustomAttributes {
			if attr.AttributeCode == "description" {
				found = true
				break
			}
		}
		if !found {
			product.CustomAttributes = append(product.CustomAttributes,
				magento.ProductCustomAttribute{AttributeCode: "description", Value: listing.Description})
		}
	}

	// URL key from title if not set
	hasURLKey := false
	for _, attr := range product.CustomAttributes {
		if attr.AttributeCode == "url_key" {
			hasURLKey = true
			break
		}
	}
	if !hasURLKey && listing.Title != "" {
		urlKey := strings.ToLower(listing.Title)
		urlKey = strings.ReplaceAll(urlKey, " ", "-")
		product.CustomAttributes = append(product.CustomAttributes,
			magento.ProductCustomAttribute{AttributeCode: "url_key", Value: urlKey})
	}

	return product
}

func convertMagentoProduct(p magento.Product) marketplace.MarketplaceProduct {
	mp := marketplace.MarketplaceProduct{
		ExternalID:  p.SKU,
		Title:       p.Name,
		SKU:         p.SKU,
		Price:       p.Price,
		Description: p.GetCustomAttribute("description"),
	}

	// Stock
	if p.ExtensionAttributes != nil && p.ExtensionAttributes.StockItem != nil {
		mp.Quantity = int(p.ExtensionAttributes.StockItem.Qty)
		mp.IsInStock = p.ExtensionAttributes.StockItem.IsInStock
	}

	// Images
	for i, entry := range p.MediaGalleryEntries {
		if entry.Disabled {
			continue
		}
		mp.Images = append(mp.Images, marketplace.ImageData{
			URL:    entry.File,
			IsMain: i == 0,
		})
	}

	mp.RawData = map[string]interface{}{
		"id":         p.ID,
		"sku":        p.SKU,
		"name":       p.Name,
		"status":     p.Status,
		"type_id":    p.TypeID,
		"price":      p.Price,
		"created_at": p.CreatedAt,
		"updated_at": p.UpdatedAt,
	}

	return mp
}

func magentoStatusToString(status int) string {
	if status == 1 {
		return "enabled"
	}
	return "disabled"
}

func toMagentoFloat64(v interface{}) float64 {
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

func toMagentoInt(v interface{}) int {
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

func (a *MagentoAdapter) CancelOrder(ctx context.Context, externalOrderID string) error {
	return marketplace.ErrCancelNotSupported
}
