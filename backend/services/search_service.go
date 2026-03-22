package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"module-a/models"
	"module-a/repository"
)

// ============================================================================
// TYPESENSE SEARCH SERVICE
// ============================================================================
// Provides full-text search for products and listings via Typesense.
// Uses direct HTTP calls — no external Go client library required.
// ============================================================================

type SearchService struct {
	host         string
	apiKey       string
	httpClient   *http.Client
	healthClient *http.Client
	repo         *repository.FirestoreRepository
}

func NewSearchService(repo *repository.FirestoreRepository) *SearchService {
	host := os.Getenv("TYPESENSE_URL")
	if host == "" {
		host = "http://localhost:8108"
	}
	apiKey := os.Getenv("TYPESENSE_API_KEY")
	if apiKey == "" {
		apiKey = "marketmate-ts-key"
	}

	return &SearchService{
		host:   strings.TrimRight(host, "/"),
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 120 * time.Second, // Bulk sync can take a while with 500+ products
		},
		healthClient: &http.Client{
			Timeout: 5 * time.Second, // Fast timeout for health checks only
		},
		repo: repo,
	}
}

// ============================================================================
// HEALTH CHECK
// ============================================================================

func (s *SearchService) Healthy() bool {
	healthy, _ := s.HealthyWithError()
	return healthy
}

func (s *SearchService) HealthyWithError() (bool, string) {
	req, err := http.NewRequest("GET", s.host+"/health", nil)
	if err != nil {
		return false, fmt.Sprintf("build request: %v", err)
	}
	req.Header.Set("X-TYPESENSE-API-KEY", s.apiKey)
	resp, err := s.healthClient.Do(req)
	if err != nil {
		return false, fmt.Sprintf("connect to %s: %v", s.host, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return false, fmt.Sprintf("HTTP %d from %s", resp.StatusCode, s.host)
	}
	return true, ""
}

// UpdateHost swaps the Typesense URL at runtime without restarting the server.
// Called by the reconnect endpoint in search_handler.go.
func (s *SearchService) UpdateHost(newURL string) {
	s.host = strings.TrimRight(newURL, "/")
}

// ============================================================================
// COLLECTION SCHEMAS
// ============================================================================

// schemaHash returns a short fingerprint of a collection schema so we can
// detect when the schema has changed between deployments.
func schemaHash(schema map[string]interface{}) string {
	b, _ := json.Marshal(schema)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8]) // 16-char hex — enough to detect changes
}

// EnsureCollections creates Typesense collections if they don't exist, or
// recreates them only when the schema has changed since the last startup.
// Returns true if any collection was (re)created and a full reindex is needed.
func (s *SearchService) EnsureCollections() (bool, error) {
	schemas := []map[string]interface{}{
		productCollectionSchema(),
		listingCollectionSchema(),
	}

	reindexNeeded := false

	for _, schema := range schemas {
		name := schema["name"].(string)
		wantHash := schemaHash(schema)

		// Check whether the collection already exists with the right schema version.
		// We embed the hash as a synthetic field name "schema_version_<hash>" that
		// Typesense stores as a no-op optional field — its presence signals the version.
		versionField := "schema_version_" + wantHash
		existsResp, err := s.doRequest("GET", "/collections/"+name, nil)
		if err == nil && existsResp.StatusCode == 200 {
			body, _ := io.ReadAll(existsResp.Body)
			existsResp.Body.Close()
			// Check if the version marker field is present in the existing schema
			if strings.Contains(string(body), versionField) {
				log.Printf("[Search] Collection '%s' schema up-to-date (hash %s) — skipping recreate", name, wantHash[:8])
				continue
			}
			// Schema changed — drop and recreate
			log.Printf("[Search] Collection '%s' schema changed (want %s) — recreating", name, wantHash[:8])
			dropResp, _ := s.doRequest("DELETE", "/collections/"+name, nil)
			if dropResp != nil {
				dropResp.Body.Close()
			}
		} else {
			if existsResp != nil {
				existsResp.Body.Close()
			}
			log.Printf("[Search] Collection '%s' not found — creating fresh", name)
		}

		// Inject the version marker as an optional field so future startups
		// can detect whether the schema matches without comparing field-by-field.
		fields, _ := schema["fields"].([]map[string]interface{})
		fields = append(fields, map[string]interface{}{
			"name":     versionField,
			"type":     "string",
			"optional": true,
			"index":    false,
		})
		schema["fields"] = fields

		body, _ := json.Marshal(schema)
		resp, err := s.doRequest("POST", "/collections", body)
		if err != nil {
			return reindexNeeded, fmt.Errorf("create collection %s: %w", name, err)
		}
		if resp.StatusCode == 201 || resp.StatusCode == 200 {
			resp.Body.Close()
			log.Printf("[Search] ✅ Created collection '%s' (schema %s)", name, wantHash[:8])
			reindexNeeded = true
		} else {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return reindexNeeded, fmt.Errorf("create collection %s: status %d: %s", name, resp.StatusCode, string(b))
		}
	}
	return reindexNeeded, nil
}

func productCollectionSchema() map[string]interface{} {
	return map[string]interface{}{
		"name": "products",
		"fields": []map[string]interface{}{
			{"name": "product_id", "type": "string"},
			{"name": "tenant_id", "type": "string", "facet": true},
			{"name": "title", "type": "string"},
			{"name": "brand", "type": "string", "optional": true, "facet": true},
			{"name": "sku", "type": "string", "optional": true, "infix": true},
			{"name": "status", "type": "string", "facet": true},
			{"name": "product_type", "type": "string", "facet": true},
			{"name": "image_url", "type": "string", "optional": true, "index": false},
			{"name": "created_at", "type": "int64", "sort": true},
			{"name": "updated_at", "type": "int64", "sort": true},
			// Identifiers for search
			{"name": "asin", "type": "string", "optional": true, "infix": true},
			{"name": "ean", "type": "string", "optional": true, "infix": true},
			{"name": "upc", "type": "string", "optional": true, "infix": true},
			{"name": "parent_id", "type": "string", "optional": true, "facet": true},
			{"name": "parent_asin", "type": "string", "optional": true, "facet": true},
			// Attributes for search
			{"name": "manufacturer", "type": "string", "optional": true, "facet": true},
			{"name": "color", "type": "string", "optional": true, "facet": true},
			{"name": "size", "type": "string", "optional": true, "facet": true},
		},
		"default_sorting_field": "created_at",
	}
}

func listingCollectionSchema() map[string]interface{} {
	return map[string]interface{}{
		"name": "listings",
		"fields": []map[string]interface{}{
			{"name": "listing_id", "type": "string"},
			{"name": "tenant_id", "type": "string", "facet": true},
			{"name": "product_id", "type": "string"},
			{"name": "product_title", "type": "string", "optional": true},
			{"name": "product_brand", "type": "string", "optional": true, "facet": true},
			{"name": "product_sku", "type": "string", "optional": true},
			{"name": "product_image", "type": "string", "optional": true, "index": false},
			{"name": "channel", "type": "string", "facet": true},
			{"name": "state", "type": "string", "facet": true},
			{"name": "channel_sku", "type": "string", "optional": true},
			{"name": "asin", "type": "string", "optional": true},
			{"name": "created_at", "type": "int64", "sort": true},
			{"name": "updated_at", "type": "int64", "sort": true},
		},
		"default_sorting_field": "created_at",
	}
}

// ============================================================================
// PRODUCT INDEXING
// ============================================================================

func (s *SearchService) IndexProduct(p *models.Product) error {
	doc := productToSearchDoc(p)
	body, _ := json.Marshal(doc)
	resp, err := s.doRequest("POST", "/collections/products/documents?action=upsert", body)
	if err != nil {
		return fmt.Errorf("index product %s: %w", p.ProductID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("index product %s: status %d: %s", p.ProductID, resp.StatusCode, string(b))
	}
	return nil
}

func (s *SearchService) DeleteProduct(productID string) error {
	resp, err := s.doRequest("DELETE", "/collections/products/documents/"+productID, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func productToSearchDoc(p *models.Product) map[string]interface{} {
	doc := map[string]interface{}{
		"id":           p.ProductID,
		"product_id":   p.ProductID,
		"tenant_id":    p.TenantID,
		"title":        p.Title,
		"status":       p.Status,
		"product_type": p.ProductType,
		"created_at":   p.CreatedAt.Unix(),
		"updated_at":   p.UpdatedAt.Unix(),
	}

	// SKU — check both sku field and attributes.source_sku
	sku := p.SKU
	if sku == "" {
		if ss, ok := p.Attributes["source_sku"].(string); ok {
			sku = ss
		}
	}
	if sku != "" {
		doc["sku"] = sku
	}

	if p.Description != nil && *p.Description != "" {
		doc["description"] = *p.Description
	}
	if p.Brand != nil && *p.Brand != "" {
		doc["brand"] = *p.Brand
	}

	// Primary image
	if len(p.Assets) > 0 {
		for _, a := range p.Assets {
			if a.Role == "primary_image" {
				doc["image_url"] = a.URL
				break
			}
		}
		if _, ok := doc["image_url"]; !ok {
			doc["image_url"] = p.Assets[0].URL
		}
	}

	// Identifiers
	if p.Identifiers != nil {
		if p.Identifiers.ASIN != nil && *p.Identifiers.ASIN != "" {
			doc["asin"] = *p.Identifiers.ASIN
		}
		if p.Identifiers.EAN != nil && *p.Identifiers.EAN != "" {
			doc["ean"] = *p.Identifiers.EAN
		}
		if p.Identifiers.UPC != nil && *p.Identifiers.UPC != "" {
			doc["upc"] = *p.Identifiers.UPC
		}
	}

	// Variation family links
	if p.ParentID != nil && *p.ParentID != "" {
		doc["parent_id"] = *p.ParentID
	}
	if p.Attributes != nil {
		if v, ok := p.Attributes["parent_asin"].(string); ok && v != "" {
			doc["parent_asin"] = v
		}
	}

	// Searchable attributes
	if p.Attributes != nil {
		if v, ok := p.Attributes["manufacturer"].(string); ok && v != "" {
			doc["manufacturer"] = v
		}
		if v, ok := p.Attributes["color"].(string); ok && v != "" {
			doc["color"] = v
		}
		if v, ok := p.Attributes["size"].(string); ok && v != "" {
			doc["size"] = v
		}
	}

	return doc
}

// ============================================================================
// LISTING INDEXING
// ============================================================================

func (s *SearchService) IndexListing(l *models.Listing) error {
	doc := listingToSearchDoc(l)
	body, _ := json.Marshal(doc)
	resp, err := s.doRequest("POST", "/collections/listings/documents?action=upsert", body)
	if err != nil {
		return fmt.Errorf("index listing %s: %w", l.ListingID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("index listing %s: status %d: %s", l.ListingID, resp.StatusCode, string(b))
	}
	return nil
}

func (s *SearchService) DeleteListing(listingID string) error {
	resp, err := s.doRequest("DELETE", "/collections/listings/documents/"+listingID, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func listingToSearchDoc(l *models.Listing) map[string]interface{} {
	doc := map[string]interface{}{
		"id":         l.ListingID,
		"listing_id": l.ListingID,
		"tenant_id":  l.TenantID,
		"product_id": l.ProductID,
		"channel":    l.Channel,
		"state":      l.State,
		"created_at": l.CreatedAt.Unix(),
		"updated_at": l.UpdatedAt.Unix(),
	}

	// Channel-specific identifiers
	if l.ChannelIdentifiers != nil {
		if l.ChannelIdentifiers.SKU != "" {
			doc["channel_sku"] = l.ChannelIdentifiers.SKU
		}
	}

	// Pull denormalized data from enriched_data
	if ed := l.EnrichedData; ed != nil {
		if v, ok := ed["title"].(string); ok && v != "" {
			doc["product_title"] = v
		}
		if v, ok := ed["brand"].(string); ok && v != "" {
			doc["product_brand"] = v
		}
		if v, ok := ed["asin"].(string); ok && v != "" {
			doc["asin"] = v
		}
	}

	return doc
}

// ============================================================================
// SEARCH
// ============================================================================

type SearchResult struct {
	Hits     []map[string]interface{} `json:"hits"`
	Found    int                      `json:"found"`
	Page     int                      `json:"page"`
	OutOf    int                      `json:"out_of"`
	FacetCounts []interface{}         `json:"facet_counts,omitempty"`
}

func (s *SearchService) SearchProducts(ctx context.Context, tenantID, query string, filters map[string]string, page, perPage int) (*SearchResult, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 250 {
		perPage = 20
	}

	params := url.Values{}
	params.Set("q", query)
	params.Set("query_by", "sku,asin,ean,upc,title,brand,manufacturer,color,size")
	params.Set("query_by_weights", "15,12,12,12,10,6,4,3,3")

	// Detect SKU/code-like queries: contain hyphens, are all-caps alphanumeric, or contain digits
	isCodeQuery := strings.ContainsAny(query, "-") ||
		(len(query) <= 15 && strings.ContainsAny(query, "0123456789"))

	if isCodeQuery {
		params.Set("num_typos", "0,0,0,0,1,1,1,1,1")
		params.Set("infix", "always,fallback,fallback,fallback,off,off,off,off,off")
		params.Set("prefix", "false,false,false,false,true,true,true,true,true")
	} else {
		params.Set("num_typos", "0,0,0,0,2,1,1,1,1")
		params.Set("prefix", "true")
	}
	params.Set("page", fmt.Sprintf("%d", page))
	params.Set("per_page", fmt.Sprintf("%d", perPage))
	params.Set("sort_by", "created_at:desc")

	// Always filter by tenant
	filterParts := []string{fmt.Sprintf("tenant_id:=%s", tenantID)}
	if status, ok := filters["status"]; ok && status != "" {
		filterParts = append(filterParts, fmt.Sprintf("status:=%s", status))
	}
	if brand, ok := filters["brand"]; ok && brand != "" {
		filterParts = append(filterParts, fmt.Sprintf("brand:=%s", brand))
	}
	if productType, ok := filters["product_type"]; ok && productType != "" {
		filterParts = append(filterParts, fmt.Sprintf("product_type:=%s", productType))
	}
	if parentID, ok := filters["parent_id"]; ok && parentID != "" {
		filterParts = append(filterParts, fmt.Sprintf("parent_id:=%s", parentID))
	}
	if parentASIN, ok := filters["parent_asin"]; ok && parentASIN != "" {
		filterParts = append(filterParts, fmt.Sprintf("parent_asin:=%s", parentASIN))
	}
	params.Set("filter_by", strings.Join(filterParts, " && "))

	// Facets for sidebar filtering
	params.Set("facet_by", "status,product_type,brand,manufacturer,color")

	resp, err := s.doRequest("GET", "/collections/products/documents/search?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("search products: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("search products: status %d: %s", resp.StatusCode, string(body))
	}

	var tsResult struct {
		Found    int `json:"found"`
		Page     int `json:"page"`
		OutOf    int `json:"out_of"`
		Hits     []struct {
			Document map[string]interface{} `json:"document"`
		} `json:"hits"`
		FacetCounts []interface{} `json:"facet_counts"`
	}
	if err := json.Unmarshal(body, &tsResult); err != nil {
		return nil, fmt.Errorf("parse search response: %w", err)
	}

	hits := make([]map[string]interface{}, len(tsResult.Hits))
	for i, h := range tsResult.Hits {
		hits[i] = h.Document
	}

	return &SearchResult{
		Hits:        hits,
		Found:       tsResult.Found,
		Page:        tsResult.Page,
		OutOf:       tsResult.OutOf,
		FacetCounts: tsResult.FacetCounts,
	}, nil
}

func (s *SearchService) SearchListings(ctx context.Context, tenantID, query string, filters map[string]string, page, perPage int) (*SearchResult, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 250 {
		perPage = 20
	}

	params := url.Values{}
	params.Set("q", query)
	params.Set("query_by", "product_title,product_sku,product_brand,channel_sku,asin")
	params.Set("page", fmt.Sprintf("%d", page))
	params.Set("per_page", fmt.Sprintf("%d", perPage))
	params.Set("sort_by", "created_at:desc")

	filterParts := []string{fmt.Sprintf("tenant_id:=%s", tenantID)}
	if ch, ok := filters["channel"]; ok && ch != "" {
		filterParts = append(filterParts, fmt.Sprintf("channel:=%s", ch))
	}
	if state, ok := filters["state"]; ok && state != "" {
		filterParts = append(filterParts, fmt.Sprintf("state:=%s", state))
	}
	params.Set("filter_by", strings.Join(filterParts, " && "))
	params.Set("facet_by", "channel,state,product_brand")

	resp, err := s.doRequest("GET", "/collections/listings/documents/search?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("search listings: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("search listings: status %d: %s", resp.StatusCode, string(body))
	}

	var tsResult struct {
		Found int `json:"found"`
		Page  int `json:"page"`
		OutOf int `json:"out_of"`
		Hits  []struct {
			Document map[string]interface{} `json:"document"`
		} `json:"hits"`
		FacetCounts []interface{} `json:"facet_counts"`
	}
	if err := json.Unmarshal(body, &tsResult); err != nil {
		return nil, fmt.Errorf("parse search response: %w", err)
	}

	hits := make([]map[string]interface{}, len(tsResult.Hits))
	for i, h := range tsResult.Hits {
		hits[i] = h.Document
	}

	return &SearchResult{
		Hits:        hits,
		Found:       tsResult.Found,
		Page:        tsResult.Page,
		OutOf:       tsResult.OutOf,
		FacetCounts: tsResult.FacetCounts,
	}, nil
}

// ============================================================================
// BULK SYNC — Index all products/listings for a tenant
// ============================================================================

func (s *SearchService) SyncAllProducts(ctx context.Context, tenantID string) (int, error) {
	log.Printf("[Search] Starting full product sync for tenant %s", tenantID)

	// Ensure collections exist before attempting to purge or import.
	if _, err := s.EnsureCollections(); err != nil {
		return 0, fmt.Errorf("ensure collections: %w", err)
	}


	// Delete all existing Typesense documents for this tenant before re-indexing.
	// This ensures that products deleted from Firestore are removed from search results.
	purgeResp, err := s.doRequest("DELETE",
		"/collections/products/documents?filter_by=tenant_id:="+tenantID, nil)
	if err != nil {
		log.Printf("[Search] Warning: failed to purge products for tenant %s: %v", tenantID, err)
	} else {
		purgeResp.Body.Close()
		log.Printf("[Search] Purged existing products for tenant %s from index", tenantID)
	}

	var indexed int
	pageSize := 500 // larger batches = fewer round-trips for large catalogs
	offset := 0

	for {
		products, _, err := s.repo.ListProducts(ctx, tenantID, nil, pageSize, offset)
		if err != nil {
			return indexed, fmt.Errorf("list products at offset %d: %w", offset, err)
		}

		if len(products) == 0 {
			break
		}

		// Batch import via JSONL
		var lines []string
		for _, p := range products {
			doc := productToSearchDoc(&p)
			b, _ := json.Marshal(doc)
			lines = append(lines, string(b))
		}
		jsonl := strings.Join(lines, "\n")

		resp, err := s.doRequest("POST", "/collections/products/documents/import?action=upsert", []byte(jsonl))
		if err != nil {
			return indexed, fmt.Errorf("import batch at offset %d: %w", offset, err)
		}
		if resp.StatusCode != 200 {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return indexed, fmt.Errorf("import batch at offset %d: status %d: %s", offset, resp.StatusCode, string(b))
		}
		resp.Body.Close()

		indexed += len(products)
		offset += pageSize
		log.Printf("[Search] Indexed %d products so far (batch offset %d)", indexed, offset)

		// Stop when we get a partial page — that means we've reached the end.
		// Avoids relying on the count aggregation (which silently returns 0 on error)
		// that previously caused the loop to break after just the first page.
		if len(products) < pageSize {
			break
		}
	}

	log.Printf("[Search] ✅ Product sync complete: %d indexed for tenant %s", indexed, tenantID)
	return indexed, nil
}

func (s *SearchService) SyncAllListings(ctx context.Context, tenantID string, listings []models.Listing) (int, error) {
	log.Printf("[Search] Starting full listing sync for tenant %s", tenantID)

	// Purge existing listing index for this tenant before re-indexing
	purgeResp, err := s.doRequest("DELETE",
		"/collections/listings/documents?filter_by=tenant_id:="+tenantID, nil)
	if err != nil {
		log.Printf("[Search] Warning: failed to purge listings for tenant %s: %v", tenantID, err)
	} else {
		purgeResp.Body.Close()
		log.Printf("[Search] Purged existing listings for tenant %s from index", tenantID)
	}

	if len(listings) == 0 {
		return 0, nil
	}

	var lines []string
	for _, l := range listings {
		doc := listingToSearchDoc(&l)
		b, _ := json.Marshal(doc)
		lines = append(lines, string(b))
	}
	jsonl := strings.Join(lines, "\n")

	resp, err := s.doRequest("POST", "/collections/listings/documents/import?action=upsert", []byte(jsonl))
	if err != nil {
		return 0, fmt.Errorf("import listings: %w", err)
	}
	resp.Body.Close()

	log.Printf("[Search] ✅ Listing sync complete: %d indexed for tenant %s", len(listings), tenantID)
	return len(listings), nil
}

// ============================================================================
// HTTP HELPER
// ============================================================================

func (s *SearchService) doRequest(method, path string, body []byte) (*http.Response, error) {
	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}

	req, err := http.NewRequest(method, s.host+path, reqBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-TYPESENSE-API-KEY", s.apiKey)
	req.Header.Set("Content-Type", "application/json")

	return s.httpClient.Do(req)
}

// ListListingsPaginated is handled via MarketplaceRepository in the search handler
