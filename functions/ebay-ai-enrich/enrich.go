package main

// ============================================================================
// EBAY AI ENRICHMENT CLOUD FUNCTION  (Session 7.3)
// ============================================================================
//
// Triggered by Cloud Tasks queue: ebay-ai-enrich
// Task payload: { tenant_id, product_id, ebay_credential_id }
//
// Flow:
//   1. Load eBay credential from Firestore → get OAuth token
//   2. Load product from Firestore → get title / EAN
//   3. Call eBay Browse API — search by EAN first, fall back to title
//   4. Extract: brand, mpn, aspects (colour, size, material), item specifics, category
//   5. Write to tenants/{tenantID}/products/{productID}/extended_data/{docID}
//      with source = "ebay"
//   6. Set product enrichment_status = "enriched"
//   7. On failure: Cloud Tasks retries up to max_attempts; after that
//      this function marks enrichment_status = "enrichment_failed"
//
// Required env vars:
//   GCP_PROJECT_ID  — Google Cloud project ID
// ============================================================================

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
)

var (
	projectID         = os.Getenv("GCP_PROJECT_ID")
	ebayBrowseBaseURL = "https://api.ebay.com/buy/browse/v1"
	ebayAuthURL       = "https://api.ebay.com/identity/v1/oauth2/token"
)

// ── Task payload ──────────────────────────────────────────────────────────────

type EnrichTaskPayload struct {
	TenantID         string `json:"tenant_id"`
	ProductID        string `json:"product_id"`
	EbayCredentialID string `json:"ebay_credential_id"`
	EbayItemID       string `json:"ebay_item_id,omitempty"` // pre-known item ID, optional
}

// ── HTTP entry point ──────────────────────────────────────────────────────────

func HandleEnrichHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var payload EnrichTaskPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Printf("[EbayAIEnrich] Failed to decode payload: %v", err)
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	if payload.TenantID == "" || payload.ProductID == "" || payload.EbayCredentialID == "" {
		http.Error(w, "missing required fields: tenant_id, product_id, ebay_credential_id", http.StatusBadRequest)
		return
	}

	log.Printf("[EbayAIEnrich] Processing product %s for tenant %s", payload.ProductID, payload.TenantID)

	if err := enrichProduct(ctx, payload); err != nil {
		log.Printf("[EbayAIEnrich] Enrichment failed for product %s: %v", payload.ProductID, err)
		// Return 500 → Cloud Tasks will retry
		http.Error(w, fmt.Sprintf("enrichment failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "enriched", "product_id": payload.ProductID})
}

// ── Core enrichment logic ─────────────────────────────────────────────────────

func enrichProduct(ctx context.Context, payload EnrichTaskPayload) error {
	fs, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		return fmt.Errorf("firestore client: %w", err)
	}
	defer fs.Close()

	// Step 1: load eBay OAuth token
	oauthToken, err := loadEbayToken(ctx, fs, payload.TenantID, payload.EbayCredentialID)
	if err != nil {
		return fmt.Errorf("load ebay token: %w", err)
	}

	// Step 2: load product
	product, err := loadProduct(ctx, fs, payload.TenantID, payload.ProductID)
	if err != nil {
		return fmt.Errorf("load product: %w", err)
	}

	// Step 3: call eBay Browse API
	var browseResult *EbayBrowseItem

	// Try EAN first
	if ean := extractEAN(product); ean != "" {
		result, err := searchByEAN(oauthToken, ean)
		if err != nil {
			log.Printf("[EbayAIEnrich] EAN search error for %s: %v", payload.ProductID, err)
		} else if result != nil {
			browseResult = result
			log.Printf("[EbayAIEnrich] Matched via EAN for product %s (eBay item %s)", payload.ProductID, result.ItemID)
		}
	}

	// Fall back to title search
	if browseResult == nil {
		if title := extractTitle(product); title != "" {
			result, err := searchByTitle(oauthToken, title)
			if err != nil {
				log.Printf("[EbayAIEnrich] Title search error for %s: %v", payload.ProductID, err)
			} else if result != nil {
				browseResult = result
				log.Printf("[EbayAIEnrich] Matched via title for product %s (eBay item %s)", payload.ProductID, result.ItemID)
			}
		}
	}

	if browseResult == nil {
		// No match on eBay — not a retryable error; mark permanently
		_ = updateEnrichmentStatus(ctx, fs, payload.TenantID, payload.ProductID, "enrichment_failed")
		log.Printf("[EbayAIEnrich] No eBay match for product %s — marked enrichment_failed", payload.ProductID)
		return nil
	}

	// Step 4: extract structured data
	extracted := extractAspects(browseResult)

	// Step 5: write to extended_data
	docID := fmt.Sprintf("ebay_%d", time.Now().UnixNano())
	extData := map[string]interface{}{
		"source":        "ebay",
		"source_id":     browseResult.ItemID,
		"product_id":    payload.ProductID,
		"tenant_id":     payload.TenantID,
		"enriched_at":   time.Now(),
		"data":          extracted,
		// Denormalised top-level fields for fast reads
		"brand":         extracted["brand"],
		"mpn":           extracted["mpn"],
		"colour":        extracted["colour"],
		"material":      extracted["material"],
		"category":      browseResult.CategoryPath,
		"condition":     browseResult.Condition,
	}

	_, err = fs.Collection("tenants").Doc(payload.TenantID).
		Collection("products").Doc(payload.ProductID).
		Collection("extended_data").Doc(docID).
		Set(ctx, extData)
	if err != nil {
		return fmt.Errorf("write extended_data: %w", err)
	}

	// Step 6: update enrichment_status → "enriched"
	if err := updateEnrichmentStatus(ctx, fs, payload.TenantID, payload.ProductID, "enriched"); err != nil {
		// Non-fatal — data was written; log and continue
		log.Printf("[EbayAIEnrich] Failed to update enrichment_status for %s: %v", payload.ProductID, err)
	}

	log.Printf("[EbayAIEnrich] Successfully enriched product %s", payload.ProductID)
	return nil
}

// ── eBay Browse API ───────────────────────────────────────────────────────────

type EbayBrowseResponse struct {
	ItemSummaries []EbayBrowseItem `json:"itemSummaries"`
	Total         int              `json:"total"`
}

type EbayBrowseItem struct {
	ItemID           string       `json:"itemId"`
	Title            string       `json:"title"`
	Condition        string       `json:"condition"`
	CategoryID       string       `json:"categoryId"`
	CategoryPath     string       `json:"categoryPath"`
	ShortDescription string       `json:"shortDescription"`
	Image            *EbayImage   `json:"image"`
	Price            *EbayPrice   `json:"price"`
	LocalizedAspects []EbayAspect `json:"localizedAspects"`
}

type EbayImage struct{ ImageURL string `json:"imageUrl"` }
type EbayPrice struct {
	Value    string `json:"value"`
	Currency string `json:"currency"`
}
type EbayAspect struct {
	Name   string   `json:"name"`
	Values []string `json:"values"`
}

func searchByEAN(token, ean string) (*EbayBrowseItem, error) {
	params := url.Values{}
	params.Set("q", ean)
	params.Set("limit", "1")
	return doBrowseSearch(token, params)
}

func searchByTitle(token, title string) (*EbayBrowseItem, error) {
	if len(title) > 100 {
		title = title[:100]
	}
	params := url.Values{}
	params.Set("q", title)
	params.Set("limit", "1")
	params.Set("filter", "buyingOptions:{FIXED_PRICE}")
	return doBrowseSearch(token, params)
}

func doBrowseSearch(token string, params url.Values) (*EbayBrowseItem, error) {
	req, err := http.NewRequest("GET", ebayBrowseBaseURL+"/item_summary/search?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-EBAY-C-MARKETPLACE-ID", "EBAY_GB")
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("eBay Browse API %d: %s", resp.StatusCode, string(body))
	}

	var browseResp EbayBrowseResponse
	if err := json.Unmarshal(body, &browseResp); err != nil {
		return nil, err
	}
	if len(browseResp.ItemSummaries) == 0 {
		return nil, nil
	}
	return &browseResp.ItemSummaries[0], nil
}

// ── Aspect extraction ─────────────────────────────────────────────────────────

// Canonical name map for common eBay aspect names
var aspectNormalize = map[string]string{
	"brand":                          "brand",
	"manufacturer":                   "brand",
	"mpn":                            "mpn",
	"manufacturer part number":       "mpn",
	"colour":                         "colour",
	"color":                          "colour",
	"size":                           "size",
	"material":                       "material",
	"model":                          "model",
	"type":                           "type",
	"compatible brand":               "compatible_brand",
	"country/region of manufacture":  "country_of_manufacture",
	"item weight":                    "weight",
	"unit of sale":                   "unit_of_sale",
}

func extractAspects(item *EbayBrowseItem) map[string]interface{} {
	data := map[string]interface{}{
		"ebay_item_id":  item.ItemID,
		"title":         item.Title,
		"condition":     item.Condition,
		"category_id":   item.CategoryID,
		"category_path": item.CategoryPath,
		"description":   item.ShortDescription,
	}
	if item.Image != nil {
		data["primary_image"] = item.Image.ImageURL
	}
	if item.Price != nil {
		data["price"] = item.Price.Value
		data["currency"] = item.Price.Currency
	}

	rawAspects := map[string]string{}
	for _, aspect := range item.LocalizedAspects {
		if len(aspect.Values) == 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(aspect.Name))
		rawAspects[key] = aspect.Values[0]
		if canonical, ok := aspectNormalize[key]; ok {
			data[canonical] = aspect.Values[0]
		} else {
			data[key] = aspect.Values[0]
		}
	}
	data["raw_aspects"] = rawAspects

	return data
}

// ── Firestore helpers ─────────────────────────────────────────────────────────

func loadProduct(ctx context.Context, fs *firestore.Client, tenantID, productID string) (map[string]interface{}, error) {
	doc, err := fs.Collection("tenants").Doc(tenantID).
		Collection("products").Doc(productID).Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("get product %s: %w", productID, err)
	}
	return doc.Data(), nil
}

func extractEAN(product map[string]interface{}) string {
	if ids, ok := product["identifiers"].(map[string]interface{}); ok {
		if ean, ok := ids["ean"].(string); ok && ean != "" {
			return ean
		}
	}
	if b, ok := product["barcode"].(string); ok && b != "" {
		return b
	}
	return ""
}

func extractTitle(product map[string]interface{}) string {
	if t, ok := product["title"].(string); ok {
		return t
	}
	return ""
}

func updateEnrichmentStatus(ctx context.Context, fs *firestore.Client, tenantID, productID, status string) error {
	_, err := fs.Collection("tenants").Doc(tenantID).
		Collection("products").Doc(productID).
		Update(ctx, []firestore.Update{
			{Path: "enrichment_status", Value: status},
			{Path: "updated_at", Value: time.Now()},
		})
	return err
}

// ── eBay OAuth ────────────────────────────────────────────────────────────────

func loadEbayToken(ctx context.Context, fs *firestore.Client, tenantID, credentialID string) (string, error) {
	// Try direct doc lookup first
	doc, err := fs.Collection("tenants").Doc(tenantID).
		Collection("marketplace_credentials").Doc(credentialID).
		Get(ctx)
	if err != nil {
		// Fallback: query by credential_id field
		iter := fs.Collection("tenants").Doc(tenantID).
			Collection("marketplace_credentials").
			Where("credential_id", "==", credentialID).
			Limit(1).
			Documents(ctx)
		defer iter.Stop()
		doc2, err2 := iter.Next()
		if err2 != nil {
			return "", fmt.Errorf("credential %s not found", credentialID)
		}
		doc = doc2
	}

	data := doc.Data()

	// Use stored access_token if not expired
	if token, ok := data["access_token"].(string); ok && token != "" {
		expired := false
		if expiresAt, ok := data["token_expires_at"].(time.Time); ok {
			expired = time.Now().After(expiresAt.Add(-5 * time.Minute))
		}
		if !expired {
			return token, nil
		}
	}

	// Try refresh via credential_data
	credData, _ := data["credential_data"].(map[string]interface{})
	if credData == nil {
		// Try oauth_token as direct bearer (some credentials store it here)
		if token, ok := data["credential_data"].(map[string]string); ok {
			if t, ok := token["oauth_token"]; ok && t != "" {
				return t, nil
			}
		}
		return "", fmt.Errorf("no usable token found for credential %s", credentialID)
	}

	clientID, _ := credData["client_id"].(string)
	clientSecret, _ := credData["client_secret"].(string)
	refreshToken, _ := credData["refresh_token"].(string)

	if clientID == "" || clientSecret == "" {
		if t, ok := credData["oauth_token"].(string); ok && t != "" {
			return t, nil
		}
		return "", fmt.Errorf("credential %s missing client_id or client_secret", credentialID)
	}

	newToken, expiresIn, err := refreshEbayToken(clientID, clientSecret, refreshToken)
	if err != nil {
		return "", fmt.Errorf("refresh token: %w", err)
	}

	// Persist refreshed token
	expiresAt := time.Now().Add(time.Duration(expiresIn) * time.Second)
	_, _ = doc.Ref.Update(ctx, []firestore.Update{
		{Path: "access_token", Value: newToken},
		{Path: "token_expires_at", Value: expiresAt},
		{Path: "updated_at", Value: time.Now()},
	})

	return newToken, nil
}

func refreshEbayToken(clientID, clientSecret, refreshToken string) (string, int, error) {
	formData := url.Values{}
	if refreshToken != "" {
		formData.Set("grant_type", "refresh_token")
		formData.Set("refresh_token", refreshToken)
		formData.Set("scope", "https://api.ebay.com/oauth/api_scope/buy.item.bulk")
	} else {
		formData.Set("grant_type", "client_credentials")
		formData.Set("scope", "https://api.ebay.com/oauth/api_scope")
	}

	req, err := http.NewRequest("POST", ebayAuthURL, strings.NewReader(formData.Encode()))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(clientID+":"+clientSecret)))

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var tok struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		Error       string `json:"error"`
	}
	if err := json.Unmarshal(body, &tok); err != nil || tok.AccessToken == "" {
		return "", 0, fmt.Errorf("eBay token response: %s", string(body))
	}
	return tok.AccessToken, tok.ExpiresIn, nil
}

// Keep iterator import used
var _ = iterator.Done
