package adapters

// ============================================================================
// SESSION 4 — NEW CHANNEL ADAPTERS
// ============================================================================
// Back Market · Zalando · Bol.com · Lazada
// Each implements marketplace.MarketplaceAdapter.
// ============================================================================

import (
	"context"
	"fmt"
	"module-a/marketplace"
	"module-a/marketplace/clients/backmarket"
	"module-a/marketplace/clients/bol"
	"module-a/marketplace/clients/lazada"
	"module-a/marketplace/clients/zalando"
	"strings"
	"time"
)

// ============================================================================
// BACK MARKET
// ============================================================================

type BackMarketAdapter struct {
	client      *backmarket.Client
	credentials marketplace.Credentials
}

func NewBackMarketAdapter(ctx context.Context, credentials marketplace.Credentials) (marketplace.MarketplaceAdapter, error) {
	apiKey := credentials.Data["api_key"]
	if apiKey == "" {
		return nil, fmt.Errorf("back market: api_key is required")
	}
	return &BackMarketAdapter{
		client:      backmarket.NewClient(apiKey, credentials.Environment == "production"),
		credentials: credentials,
	}, nil
}

func (a *BackMarketAdapter) Connect(ctx context.Context, credentials marketplace.Credentials) error    { return nil }
func (a *BackMarketAdapter) Disconnect(ctx context.Context) error                                      { return nil }
func (a *BackMarketAdapter) RefreshAuth(ctx context.Context) error                                     { return nil }
func (a *BackMarketAdapter) GetName() string                                                           { return "backmarket" }
func (a *BackMarketAdapter) GetDisplayName() string                                                    { return "Back Market" }

func (a *BackMarketAdapter) TestConnection(ctx context.Context) error {
	return a.client.TestConnection()
}

func (a *BackMarketAdapter) GetConnectionStatus(ctx context.Context) (*marketplace.ConnectionStatus, error) {
	s := &marketplace.ConnectionStatus{IsConnected: true, LastChecked: time.Now()}
	if err := a.TestConnection(ctx); err != nil {
		s.IsConnected = false
		s.ErrorMessage = err.Error()
	} else {
		s.LastSuccessful = time.Now()
	}
	return s, nil
}

func (a *BackMarketAdapter) FetchListings(ctx context.Context, filters marketplace.ImportFilters) ([]marketplace.MarketplaceProduct, error) {
	listings, err := a.client.GetListings(50)
	if err != nil {
		return nil, err
	}
	var products []marketplace.MarketplaceProduct
	for _, l := range listings {
		if filters.StockFilter == "in_stock" && l.Stock <= 0 {
			continue
		}
		mp := marketplace.MarketplaceProduct{
			ExternalID:         l.ListingID,
			SKU:                l.SellerSKU,
			Title:              l.Title,
			Description:        l.Description,
			Price:              l.Price,
			Currency:           l.Currency,
			Quantity:           l.Stock,
			IsInStock:          l.Stock > 0,
			FulfillmentChannel: "merchant",
			Attributes: map[string]interface{}{
				"grade":      l.Grade,
				"product_id": l.ProductID,
				"state":      l.State,
			},
		}
		if l.ImageURL != "" {
			mp.Images = []marketplace.ImageData{{URL: l.ImageURL, IsMain: true}}
		}
		products = append(products, mp)
		if filters.ProductCallback != nil && !filters.ProductCallback(mp) {
			break
		}
	}
	return products, nil
}

func (a *BackMarketAdapter) FetchProduct(ctx context.Context, externalID string) (*marketplace.MarketplaceProduct, error) {
	listings, err := a.client.GetListings(50)
	if err != nil {
		return nil, err
	}
	for _, l := range listings {
		if l.ListingID == externalID {
			mp := marketplace.MarketplaceProduct{ExternalID: l.ListingID, SKU: l.SellerSKU, Title: l.Title, Price: l.Price, Currency: l.Currency, Quantity: l.Stock}
			return &mp, nil
		}
	}
	return nil, fmt.Errorf("back market: listing %s not found", externalID)
}

func (a *BackMarketAdapter) FetchProductImages(ctx context.Context, externalID string) ([]marketplace.ImageData, error) {
	return nil, nil
}

func (a *BackMarketAdapter) FetchInventory(ctx context.Context, externalID string) (*marketplace.InventoryLevel, error) {
	listings, err := a.client.GetListings(50)
	if err != nil {
		return nil, err
	}
	for _, l := range listings {
		if l.ListingID == externalID {
			return &marketplace.InventoryLevel{ExternalID: externalID, Quantity: l.Stock, UpdatedAt: time.Now()}, nil
		}
	}
	return nil, fmt.Errorf("back market: listing %s not found", externalID)
}

func (a *BackMarketAdapter) CreateListing(ctx context.Context, listing marketplace.ListingData) (*marketplace.ListingResult, error) {
	productID := 0
	grade := "good"
	if listing.CustomFields != nil {
		if pid, ok := listing.CustomFields["product_id"].(float64); ok {
			productID = int(pid)
		}
		if g, ok := listing.CustomFields["grade"].(string); ok {
			grade = g
		}
	}
	result, err := a.client.UpsertListing(backmarket.CreateListingRequest{
		ProductID: productID, SellerSKU: listing.ProductID,
		Price: listing.Price, Currency: "GBP",
		Stock: listing.Quantity, Grade: grade, Description: listing.Description,
	})
	if err != nil {
		return nil, err
	}
	return &marketplace.ListingResult{ExternalID: result.ListingID, SKU: result.SellerSKU, Status: "active", CreatedAt: time.Now()}, nil
}

func (a *BackMarketAdapter) UpdateListing(ctx context.Context, externalID string, updates marketplace.ListingData) error {
	if err := a.client.UpdatePrice(externalID, updates.Price); err != nil {
		return err
	}
	return a.client.UpdateStock(externalID, updates.Quantity)
}

func (a *BackMarketAdapter) DeleteListing(ctx context.Context, externalID string) error {
	return a.client.DeactivateListing(externalID)
}

func (a *BackMarketAdapter) PublishListing(ctx context.Context, externalID string) error   { return nil }
func (a *BackMarketAdapter) UnpublishListing(ctx context.Context, externalID string) error {
	return a.client.DeactivateListing(externalID)
}

func (a *BackMarketAdapter) BulkCreateListings(ctx context.Context, listings []marketplace.ListingData) ([]marketplace.ListingResult, error) {
	var results []marketplace.ListingResult
	for _, l := range listings {
		res, err := a.CreateListing(ctx, l)
		if err != nil {
			results = append(results, marketplace.ListingResult{SKU: l.ProductID, Status: "error", Errors: []marketplace.ValidationError{{Code: "CREATE_FAILED", Message: err.Error(), Severity: "error"}}})
		} else {
			results = append(results, *res)
		}
	}
	return results, nil
}

func (a *BackMarketAdapter) BulkUpdateListings(ctx context.Context, updates []marketplace.ListingUpdate) ([]marketplace.UpdateResult, error) {
	var results []marketplace.UpdateResult
	for _, u := range updates {
		err := a.SyncInventory(ctx, u.ExternalID, 0)
		results = append(results, marketplace.UpdateResult{ExternalID: u.ExternalID, Success: err == nil, UpdatedAt: time.Now()})
	}
	return results, nil
}

func (a *BackMarketAdapter) GetListingStatus(ctx context.Context, externalID string) (*marketplace.ListingStatus, error) {
	listings, err := a.client.GetListings(50)
	if err != nil {
		return nil, err
	}
	for _, l := range listings {
		if l.ListingID == externalID {
			return &marketplace.ListingStatus{ExternalID: externalID, Status: l.State, IsActive: l.State == "active", Quantity: l.Stock, Price: l.Price, LastUpdated: time.Now()}, nil
		}
	}
	return nil, fmt.Errorf("back market: listing %s not found", externalID)
}

func (a *BackMarketAdapter) SyncInventory(ctx context.Context, externalID string, quantity int) error {
	return a.client.UpdateStock(externalID, quantity)
}

func (a *BackMarketAdapter) SyncPrice(ctx context.Context, externalID string, price float64) error {
	return a.client.UpdatePrice(externalID, price)
}

func (a *BackMarketAdapter) GetSupportedFeatures() []string {
	return []string{"listing", "order_sync", "tracking", "inventory_sync", "price_sync"}
}

func (a *BackMarketAdapter) GetRequiredFields() []marketplace.RequiredField {
	return []marketplace.RequiredField{
		{Name: "product_id", Type: "number", Description: "Back Market catalogue product ID"},
		{Name: "grade", Type: "string", Description: "Condition grade", Examples: []string{"excellent", "good", "fair"}},
		{Name: "price", Type: "number", Description: "Listing price"},
		{Name: "quantity", Type: "number", Description: "Available stock"},
		{Name: "description", Type: "string", Description: "Condition description"},
	}
}

func (a *BackMarketAdapter) GetCategories(ctx context.Context) ([]marketplace.Category, error) {
	return []marketplace.Category{
		{ID: "smartphones", Name: "Smartphones", Level: 1},
		{ID: "laptops", Name: "Laptops & Computers", Level: 1},
		{ID: "tablets", Name: "Tablets", Level: 1},
		{ID: "headphones", Name: "Headphones & Speakers", Level: 1},
		{ID: "cameras", Name: "Cameras", Level: 1},
		{ID: "gaming", Name: "Gaming", Level: 1},
	}, nil
}

func (a *BackMarketAdapter) ValidateListing(ctx context.Context, listing marketplace.ListingData) (*marketplace.ValidationResult, error) {
	var errs []marketplace.ValidationError
	if listing.Price <= 0 {
		errs = append(errs, marketplace.ValidationError{Code: "INVALID_PRICE", Field: "price", Message: "Price must be > 0", Severity: "error"})
	}
	if listing.Description == "" {
		errs = append(errs, marketplace.ValidationError{Code: "MISSING_DESCRIPTION", Field: "description", Message: "Condition description is required", Severity: "error"})
	}
	return &marketplace.ValidationResult{IsValid: len(errs) == 0, Errors: errs}, nil
}

// ============================================================================
// ZALANDO
// ============================================================================

type ZalandoAdapter struct {
	client      *zalando.Client
	credentials marketplace.Credentials
}

func NewZalandoAdapter(ctx context.Context, credentials marketplace.Credentials) (marketplace.MarketplaceAdapter, error) {
	clientID := credentials.Data["client_id"]
	clientSecret := credentials.Data["client_secret"]
	if clientID == "" || clientSecret == "" {
		return nil, fmt.Errorf("zalando: client_id and client_secret are required")
	}
	return &ZalandoAdapter{
		client:      zalando.NewClient(clientID, clientSecret, credentials.Environment == "production"),
		credentials: credentials,
	}, nil
}

func (a *ZalandoAdapter) Connect(ctx context.Context, credentials marketplace.Credentials) error    { return nil }
func (a *ZalandoAdapter) Disconnect(ctx context.Context) error                                      { return nil }
func (a *ZalandoAdapter) RefreshAuth(ctx context.Context) error                                     { return nil }
func (a *ZalandoAdapter) GetName() string                                                           { return "zalando" }
func (a *ZalandoAdapter) GetDisplayName() string                                                    { return "Zalando" }

func (a *ZalandoAdapter) TestConnection(ctx context.Context) error { return a.client.TestConnection() }

func (a *ZalandoAdapter) GetConnectionStatus(ctx context.Context) (*marketplace.ConnectionStatus, error) {
	s := &marketplace.ConnectionStatus{IsConnected: true, LastChecked: time.Now()}
	if err := a.TestConnection(ctx); err != nil {
		s.IsConnected = false
		s.ErrorMessage = err.Error()
	} else {
		s.LastSuccessful = time.Now()
	}
	return s, nil
}

func (a *ZalandoAdapter) FetchListings(ctx context.Context, filters marketplace.ImportFilters) ([]marketplace.MarketplaceProduct, error) {
	articles, err := a.client.GetArticles()
	if err != nil {
		return nil, err
	}
	var products []marketplace.MarketplaceProduct
	for _, art := range articles {
		if filters.StockFilter == "in_stock" && art.Stock <= 0 {
			continue
		}
		mp := marketplace.MarketplaceProduct{
			ExternalID: art.ArticleID, SKU: art.EAN, Title: art.Name,
			Price: art.Price, Currency: art.Currency, Quantity: art.Stock,
			IsInStock: art.Stock > 0, FulfillmentChannel: "merchant",
			Identifiers: marketplace.Identifiers{EAN: art.EAN},
		}
		if art.ImageURL != "" {
			mp.Images = []marketplace.ImageData{{URL: art.ImageURL, IsMain: true}}
		}
		products = append(products, mp)
		if filters.ProductCallback != nil && !filters.ProductCallback(mp) {
			break
		}
	}
	return products, nil
}

func (a *ZalandoAdapter) FetchProduct(ctx context.Context, externalID string) (*marketplace.MarketplaceProduct, error) {
	articles, err := a.client.GetArticles()
	if err != nil {
		return nil, err
	}
	for _, art := range articles {
		if art.ArticleID == externalID {
			mp := marketplace.MarketplaceProduct{ExternalID: art.ArticleID, SKU: art.EAN, Title: art.Name, Price: art.Price}
			return &mp, nil
		}
	}
	return nil, fmt.Errorf("zalando: article %s not found", externalID)
}

func (a *ZalandoAdapter) FetchProductImages(ctx context.Context, externalID string) ([]marketplace.ImageData, error) {
	return nil, nil
}

func (a *ZalandoAdapter) FetchInventory(ctx context.Context, externalID string) (*marketplace.InventoryLevel, error) {
	articles, err := a.client.GetArticles()
	if err != nil {
		return nil, err
	}
	for _, art := range articles {
		if art.ArticleID == externalID {
			return &marketplace.InventoryLevel{ExternalID: externalID, Quantity: art.Stock, UpdatedAt: time.Now()}, nil
		}
	}
	return nil, fmt.Errorf("zalando: article %s not found", externalID)
}

func (a *ZalandoAdapter) CreateListing(ctx context.Context, listing marketplace.ListingData) (*marketplace.ListingResult, error) {
	return nil, fmt.Errorf("zalando: listing creation via API not supported — use Zalando Partner Portal")
}

func (a *ZalandoAdapter) UpdateListing(ctx context.Context, externalID string, updates marketplace.ListingData) error {
	if err := a.client.UpdatePrice(externalID, updates.Price); err != nil {
		return err
	}
	return a.client.UpdateStock(externalID, updates.Quantity)
}

func (a *ZalandoAdapter) DeleteListing(ctx context.Context, externalID string) error {
	return a.client.DeactivateArticle(externalID)
}

func (a *ZalandoAdapter) PublishListing(ctx context.Context, externalID string) error   { return nil }
func (a *ZalandoAdapter) UnpublishListing(ctx context.Context, externalID string) error {
	return a.client.DeactivateArticle(externalID)
}

func (a *ZalandoAdapter) BulkCreateListings(ctx context.Context, listings []marketplace.ListingData) ([]marketplace.ListingResult, error) {
	return nil, fmt.Errorf("zalando: bulk listing creation not supported via API")
}

func (a *ZalandoAdapter) BulkUpdateListings(ctx context.Context, updates []marketplace.ListingUpdate) ([]marketplace.UpdateResult, error) {
	var results []marketplace.UpdateResult
	for _, u := range updates {
		err := a.SyncInventory(ctx, u.ExternalID, 0)
		results = append(results, marketplace.UpdateResult{ExternalID: u.ExternalID, Success: err == nil, UpdatedAt: time.Now()})
	}
	return results, nil
}

func (a *ZalandoAdapter) GetListingStatus(ctx context.Context, externalID string) (*marketplace.ListingStatus, error) {
	articles, err := a.client.GetArticles()
	if err != nil {
		return nil, err
	}
	for _, art := range articles {
		if art.ArticleID == externalID {
			return &marketplace.ListingStatus{ExternalID: externalID, IsActive: art.Active, Quantity: art.Stock, Price: art.Price, LastUpdated: time.Now()}, nil
		}
	}
	return nil, fmt.Errorf("zalando: article %s not found", externalID)
}

func (a *ZalandoAdapter) SyncInventory(ctx context.Context, externalID string, quantity int) error {
	return a.client.UpdateStock(externalID, quantity)
}

func (a *ZalandoAdapter) SyncPrice(ctx context.Context, externalID string, price float64) error {
	return a.client.UpdatePrice(externalID, price)
}

func (a *ZalandoAdapter) GetSupportedFeatures() []string {
	return []string{"listing", "order_sync", "tracking", "inventory_sync", "price_sync"}
}

func (a *ZalandoAdapter) GetRequiredFields() []marketplace.RequiredField {
	return []marketplace.RequiredField{
		{Name: "ean", Type: "string", Description: "EAN barcode (required by Zalando)"},
		{Name: "price", Type: "number", Description: "Article price"},
	}
}

func (a *ZalandoAdapter) GetCategories(ctx context.Context) ([]marketplace.Category, error) {
	return []marketplace.Category{
		{ID: "clothing", Name: "Clothing", Level: 1},
		{ID: "shoes", Name: "Shoes", Level: 1},
		{ID: "accessories", Name: "Accessories", Level: 1},
		{ID: "sports", Name: "Sports & Outdoors", Level: 1},
		{ID: "beauty", Name: "Beauty", Level: 1},
	}, nil
}

func (a *ZalandoAdapter) ValidateListing(ctx context.Context, listing marketplace.ListingData) (*marketplace.ValidationResult, error) {
	var errs []marketplace.ValidationError
	if listing.Price <= 0 {
		errs = append(errs, marketplace.ValidationError{Code: "INVALID_PRICE", Field: "price", Message: "Price must be > 0", Severity: "error"})
	}
	return &marketplace.ValidationResult{IsValid: len(errs) == 0, Errors: errs}, nil
}

// ============================================================================
// BOL.COM
// ============================================================================

type BolAdapter struct {
	client      *bol.Client
	credentials marketplace.Credentials
}

func NewBolAdapter(ctx context.Context, credentials marketplace.Credentials) (marketplace.MarketplaceAdapter, error) {
	clientID := credentials.Data["client_id"]
	clientSecret := credentials.Data["client_secret"]
	if clientID == "" || clientSecret == "" {
		return nil, fmt.Errorf("bol.com: client_id and client_secret are required")
	}
	return &BolAdapter{
		client:      bol.NewClient(clientID, clientSecret),
		credentials: credentials,
	}, nil
}

func (a *BolAdapter) Connect(ctx context.Context, credentials marketplace.Credentials) error    { return nil }
func (a *BolAdapter) Disconnect(ctx context.Context) error                                      { return nil }
func (a *BolAdapter) RefreshAuth(ctx context.Context) error                                     { return nil }
func (a *BolAdapter) GetName() string                                                           { return "bol" }
func (a *BolAdapter) GetDisplayName() string                                                    { return "Bol.com" }

func (a *BolAdapter) TestConnection(ctx context.Context) error { return a.client.TestConnection() }

func (a *BolAdapter) GetConnectionStatus(ctx context.Context) (*marketplace.ConnectionStatus, error) {
	s := &marketplace.ConnectionStatus{IsConnected: true, LastChecked: time.Now()}
	if err := a.TestConnection(ctx); err != nil {
		s.IsConnected = false
		s.ErrorMessage = err.Error()
	} else {
		s.LastSuccessful = time.Now()
	}
	return s, nil
}

func (a *BolAdapter) FetchListings(ctx context.Context, filters marketplace.ImportFilters) ([]marketplace.MarketplaceProduct, error) {
	offers, err := a.client.GetOffers()
	if err != nil {
		return nil, err
	}
	var products []marketplace.MarketplaceProduct
	for _, o := range offers {
		if filters.StockFilter == "in_stock" && o.Stock <= 0 {
			continue
		}
		mp := marketplace.MarketplaceProduct{
			ExternalID: o.OfferID, SKU: o.Reference, Title: o.Title,
			Price: o.Price, Currency: "EUR", Quantity: o.Stock,
			IsInStock: o.Stock > 0, FulfillmentChannel: "merchant",
			Identifiers: marketplace.Identifiers{EAN: o.EAN},
		}
		products = append(products, mp)
		if filters.ProductCallback != nil && !filters.ProductCallback(mp) {
			break
		}
	}
	return products, nil
}

func (a *BolAdapter) FetchProduct(ctx context.Context, externalID string) (*marketplace.MarketplaceProduct, error) {
	offers, err := a.client.GetOffers()
	if err != nil {
		return nil, err
	}
	for _, o := range offers {
		if o.OfferID == externalID {
			mp := marketplace.MarketplaceProduct{ExternalID: o.OfferID, SKU: o.Reference, Title: o.Title, Price: o.Price, Currency: "EUR"}
			return &mp, nil
		}
	}
	return nil, fmt.Errorf("bol.com: offer %s not found", externalID)
}

func (a *BolAdapter) FetchProductImages(ctx context.Context, externalID string) ([]marketplace.ImageData, error) {
	return nil, nil
}

func (a *BolAdapter) FetchInventory(ctx context.Context, externalID string) (*marketplace.InventoryLevel, error) {
	offers, err := a.client.GetOffers()
	if err != nil {
		return nil, err
	}
	for _, o := range offers {
		if o.OfferID == externalID {
			return &marketplace.InventoryLevel{ExternalID: externalID, Quantity: o.Stock, UpdatedAt: time.Now()}, nil
		}
	}
	return nil, fmt.Errorf("bol.com: offer %s not found", externalID)
}

func (a *BolAdapter) CreateListing(ctx context.Context, listing marketplace.ListingData) (*marketplace.ListingResult, error) {
	return nil, fmt.Errorf("bol.com: offer creation via API requires product to be in bol.com catalogue first")
}

func (a *BolAdapter) UpdateListing(ctx context.Context, externalID string, updates marketplace.ListingData) error {
	if err := a.client.UpdatePrice(externalID, updates.Price); err != nil {
		return err
	}
	return a.client.UpdateStock(externalID, updates.Quantity)
}

func (a *BolAdapter) DeleteListing(ctx context.Context, externalID string) error {
	return a.client.DeactivateOffer(externalID)
}

func (a *BolAdapter) PublishListing(ctx context.Context, externalID string) error   { return nil }
func (a *BolAdapter) UnpublishListing(ctx context.Context, externalID string) error {
	return a.client.DeactivateOffer(externalID)
}

func (a *BolAdapter) BulkCreateListings(ctx context.Context, listings []marketplace.ListingData) ([]marketplace.ListingResult, error) {
	return nil, fmt.Errorf("bol.com: bulk listing creation not yet supported")
}

func (a *BolAdapter) BulkUpdateListings(ctx context.Context, updates []marketplace.ListingUpdate) ([]marketplace.UpdateResult, error) {
	var results []marketplace.UpdateResult
	for _, u := range updates {
		err := a.SyncInventory(ctx, u.ExternalID, 0)
		results = append(results, marketplace.UpdateResult{ExternalID: u.ExternalID, Success: err == nil, UpdatedAt: time.Now()})
	}
	return results, nil
}

func (a *BolAdapter) GetListingStatus(ctx context.Context, externalID string) (*marketplace.ListingStatus, error) {
	offers, err := a.client.GetOffers()
	if err != nil {
		return nil, err
	}
	for _, o := range offers {
		if o.OfferID == externalID {
			return &marketplace.ListingStatus{ExternalID: externalID, IsActive: !o.OnHoldByRetailer, Quantity: o.Stock, Price: o.Price, LastUpdated: time.Now()}, nil
		}
	}
	return nil, fmt.Errorf("bol.com: offer %s not found", externalID)
}

func (a *BolAdapter) SyncInventory(ctx context.Context, externalID string, quantity int) error {
	return a.client.UpdateStock(externalID, quantity)
}

func (a *BolAdapter) SyncPrice(ctx context.Context, externalID string, price float64) error {
	return a.client.UpdatePrice(externalID, price)
}

func (a *BolAdapter) GetSupportedFeatures() []string {
	return []string{"listing", "order_sync", "tracking", "inventory_sync", "price_sync"}
}

func (a *BolAdapter) GetRequiredFields() []marketplace.RequiredField {
	return []marketplace.RequiredField{
		{Name: "ean", Type: "string", Description: "EAN barcode"},
		{Name: "price", Type: "number", Description: "Offer price"},
	}
}

func (a *BolAdapter) GetCategories(ctx context.Context) ([]marketplace.Category, error) {
	return []marketplace.Category{
		{ID: "electronics", Name: "Electronics", Level: 1},
		{ID: "books", Name: "Books", Level: 1},
		{ID: "toys", Name: "Toys & Games", Level: 1},
		{ID: "home", Name: "Home & Garden", Level: 1},
		{ID: "fashion", Name: "Fashion", Level: 1},
	}, nil
}

func (a *BolAdapter) ValidateListing(ctx context.Context, listing marketplace.ListingData) (*marketplace.ValidationResult, error) {
	var errs []marketplace.ValidationError
	if listing.Price <= 0 {
		errs = append(errs, marketplace.ValidationError{Code: "INVALID_PRICE", Field: "price", Message: "Price must be > 0", Severity: "error"})
	}
	return &marketplace.ValidationResult{IsValid: len(errs) == 0, Errors: errs}, nil
}

// ============================================================================
// LAZADA
// ============================================================================

type LazadaAdapter struct {
	client      *lazada.Client
	credentials marketplace.Credentials
}

func NewLazadaAdapter(ctx context.Context, credentials marketplace.Credentials) (marketplace.MarketplaceAdapter, error) {
	appKey := credentials.Data["app_key"]
	appSecret := credentials.Data["app_secret"]
	accessToken := credentials.Data["access_token"]
	baseURL := credentials.Data["base_url"]
	if appKey == "" || appSecret == "" {
		return nil, fmt.Errorf("lazada: app_key and app_secret are required")
	}
	return &LazadaAdapter{
		client:      lazada.NewClient(appKey, appSecret, accessToken, baseURL),
		credentials: credentials,
	}, nil
}

func (a *LazadaAdapter) Connect(ctx context.Context, credentials marketplace.Credentials) error    { return nil }
func (a *LazadaAdapter) Disconnect(ctx context.Context) error                                      { return nil }
func (a *LazadaAdapter) RefreshAuth(ctx context.Context) error                                     { return nil }
func (a *LazadaAdapter) GetName() string                                                           { return "lazada" }
func (a *LazadaAdapter) GetDisplayName() string                                                    { return "Lazada" }

func (a *LazadaAdapter) TestConnection(ctx context.Context) error { return a.client.TestConnection() }

func (a *LazadaAdapter) GetConnectionStatus(ctx context.Context) (*marketplace.ConnectionStatus, error) {
	s := &marketplace.ConnectionStatus{IsConnected: true, LastChecked: time.Now()}
	if err := a.TestConnection(ctx); err != nil {
		s.IsConnected = false
		s.ErrorMessage = err.Error()
	} else {
		s.LastSuccessful = time.Now()
	}
	return s, nil
}

func (a *LazadaAdapter) FetchListings(ctx context.Context, filters marketplace.ImportFilters) ([]marketplace.MarketplaceProduct, error) {
	products, err := a.client.GetAllProducts()
	if err != nil {
		return nil, err
	}
	var mps []marketplace.MarketplaceProduct
	for _, p := range products {
		if p.Status == "deleted" {
			continue
		}
		// Use first SKU for top-level product info
		var price float64
		var qty int
		var imageURL string
		var sku string
		if len(p.Skus) > 0 {
			price = p.Skus[0].Price
			qty = p.Skus[0].Quantity
			sku = p.Skus[0].SellerSKU
			if len(p.Skus[0].Images) > 0 {
				imageURL = p.Skus[0].Images[0].URL
			}
		}
		if sku == "" {
			sku = p.SellerSKU
		}
		if filters.StockFilter == "in_stock" && qty <= 0 {
			continue
		}
		mp := marketplace.MarketplaceProduct{
			ExternalID: fmt.Sprintf("%d", p.ItemID), SKU: sku,
			Title: p.Name, Description: p.Description,
			Price: price, Currency: "USD", Quantity: qty,
			IsInStock: qty > 0, FulfillmentChannel: "merchant",
		}
		if imageURL != "" {
			mp.Images = []marketplace.ImageData{{URL: imageURL, IsMain: true}}
		}
		mps = append(mps, mp)
		if filters.ProductCallback != nil && !filters.ProductCallback(mp) {
			break
		}
	}
	return mps, nil
}

func (a *LazadaAdapter) FetchProduct(ctx context.Context, externalID string) (*marketplace.MarketplaceProduct, error) {
	products, err := a.client.GetAllProducts()
	if err != nil {
		return nil, err
	}
	for _, p := range products {
		if fmt.Sprintf("%d", p.ItemID) == externalID {
			mp := marketplace.MarketplaceProduct{ExternalID: externalID, SKU: p.SellerSKU, Title: p.Name}
			return &mp, nil
		}
	}
	return nil, fmt.Errorf("lazada: product %s not found", externalID)
}

func (a *LazadaAdapter) FetchProductImages(ctx context.Context, externalID string) ([]marketplace.ImageData, error) {
	return nil, nil
}

func (a *LazadaAdapter) FetchInventory(ctx context.Context, externalID string) (*marketplace.InventoryLevel, error) {
	products, err := a.client.GetAllProducts()
	if err != nil {
		return nil, err
	}
	for _, p := range products {
		if fmt.Sprintf("%d", p.ItemID) == externalID {
			qty := 0
			if len(p.Skus) > 0 {
				qty = p.Skus[0].Quantity
			}
			return &marketplace.InventoryLevel{ExternalID: externalID, Quantity: qty, UpdatedAt: time.Now()}, nil
		}
	}
	return nil, fmt.Errorf("lazada: product %s not found", externalID)
}

func (a *LazadaAdapter) CreateListing(ctx context.Context, listing marketplace.ListingData) (*marketplace.ListingResult, error) {
	return nil, fmt.Errorf("lazada: listing creation via API not yet implemented — use Lazada Seller Center")
}

func (a *LazadaAdapter) UpdateListing(ctx context.Context, externalID string, updates marketplace.ListingData) error {
	if err := a.client.UpdatePrice(externalID, updates.Price); err != nil {
		return err
	}
	return a.client.UpdateStock(externalID, updates.Quantity)
}

func (a *LazadaAdapter) DeleteListing(ctx context.Context, externalID string) error { return nil }
func (a *LazadaAdapter) PublishListing(ctx context.Context, externalID string) error { return nil }
func (a *LazadaAdapter) UnpublishListing(ctx context.Context, externalID string) error { return nil }

func (a *LazadaAdapter) BulkCreateListings(ctx context.Context, listings []marketplace.ListingData) ([]marketplace.ListingResult, error) {
	return nil, fmt.Errorf("lazada: bulk listing creation not yet implemented")
}

func (a *LazadaAdapter) BulkUpdateListings(ctx context.Context, updates []marketplace.ListingUpdate) ([]marketplace.UpdateResult, error) {
	var results []marketplace.UpdateResult
	for _, u := range updates {
		err := a.SyncInventory(ctx, u.ExternalID, 0)
		results = append(results, marketplace.UpdateResult{ExternalID: u.ExternalID, Success: err == nil, UpdatedAt: time.Now()})
	}
	return results, nil
}

func (a *LazadaAdapter) GetListingStatus(ctx context.Context, externalID string) (*marketplace.ListingStatus, error) {
	products, err := a.client.GetAllProducts()
	if err != nil {
		return nil, err
	}
	for _, p := range products {
		if fmt.Sprintf("%d", p.ItemID) == externalID {
			qty := 0
			if len(p.Skus) > 0 {
				qty = p.Skus[0].Quantity
			}
			return &marketplace.ListingStatus{ExternalID: externalID, IsActive: p.Status == "active", Quantity: qty, LastUpdated: time.Now()}, nil
		}
	}
	return nil, fmt.Errorf("lazada: product %s not found", externalID)
}

func (a *LazadaAdapter) SyncInventory(ctx context.Context, externalID string, quantity int) error {
	return a.client.UpdateStock(externalID, quantity)
}

func (a *LazadaAdapter) SyncPrice(ctx context.Context, externalID string, price float64) error {
	return a.client.UpdatePrice(externalID, price)
}

func (a *LazadaAdapter) GetSupportedFeatures() []string {
	return []string{"listing", "order_sync", "tracking", "inventory_sync", "price_sync"}
}

func (a *LazadaAdapter) GetRequiredFields() []marketplace.RequiredField {
	return []marketplace.RequiredField{
		{Name: "app_key", Type: "string", Description: "Lazada Open Platform App Key"},
		{Name: "app_secret", Type: "string", Description: "Lazada Open Platform App Secret"},
		{Name: "access_token", Type: "string", Description: "Seller access token from OAuth flow"},
	}
}

func (a *LazadaAdapter) GetCategories(ctx context.Context) ([]marketplace.Category, error) {
	return []marketplace.Category{
		{ID: "mobiles", Name: "Mobiles & Tablets", Level: 1},
		{ID: "electronics", Name: "Consumer Electronics", Level: 1},
		{ID: "fashion", Name: "Men's & Women's Fashion", Level: 1},
		{ID: "home", Name: "Home & Living", Level: 1},
		{ID: "health", Name: "Health & Beauty", Level: 1},
		{ID: "sports", Name: "Sports & Travel", Level: 1},
	}, nil
}

func (a *LazadaAdapter) ValidateListing(ctx context.Context, listing marketplace.ListingData) (*marketplace.ValidationResult, error) {
	var errs []marketplace.ValidationError
	if listing.Price <= 0 {
		errs = append(errs, marketplace.ValidationError{Code: "INVALID_PRICE", Field: "price", Message: "Price must be > 0", Severity: "error"})
	}
	if listing.Title == "" {
		errs = append(errs, marketplace.ValidationError{Code: "MISSING_TITLE", Field: "title", Message: "Title is required", Severity: "error"})
	}
	return &marketplace.ValidationResult{IsValid: len(errs) == 0, Errors: errs}, nil
}

// ── shared helper: normalise state strings ────────────────────────────────────

func normChannelState(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// ── CancelOrder stubs for S4 channels ────────────────────────────────────────

func (a *BackMarketAdapter) CancelOrder(ctx context.Context, externalOrderID string) error {
	return marketplace.ErrCancelNotSupported
}

func (a *ZalandoAdapter) CancelOrder(ctx context.Context, externalOrderID string) error {
	return marketplace.ErrCancelNotSupported
}

func (a *BolAdapter) CancelOrder(ctx context.Context, externalOrderID string) error {
	return marketplace.ErrCancelNotSupported
}

func (a *LazadaAdapter) CancelOrder(ctx context.Context, externalOrderID string) error {
	return marketplace.ErrCancelNotSupported
}
