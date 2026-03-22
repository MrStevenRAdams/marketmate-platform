package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"cloud.google.com/go/storage"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"
)

var (
	projectID        = os.Getenv("GCP_PROJECT_ID")
	encryptionKey    = os.Getenv("CREDENTIAL_ENCRYPTION_KEY")
	lwaTokenEndpoint = "https://api.amazon.com/auth/o2/token"
	catalogRateDelay = 600 * time.Millisecond // 600ms ≈ 1.6 req/s, safely under Amazon's 2 req/s limit
)

type EnrichPayload struct {
	TenantID     string       `json:"tenant_id"`
	JobID        string       `json:"job_id"`
	CredentialID string       `json:"credential_id"`
	Items        []EnrichItem `json:"items"`
}

type EnrichItem struct {
	ProductID string `json:"product_id"`
	ASIN      string `json:"asin"`
}

type SPAPICreds struct {
	LWAClientID     string
	LWAClientSecret string
	RefreshToken    string
	AWSAccessKeyID  string
	AWSSecretKey    string
	AWSSessionToken string
	AWSRegion       string
	Endpoint        string
	MarketplaceID   string
}

func HandleEnrichHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var payload EnrichPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Printf("[Enrich] ERROR: invalid payload: %v", err)
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	log.Printf("[Enrich] Processing %d items for job %s credential %s",
		len(payload.Items), payload.JobID, payload.CredentialID)
	if err := processEnrichBatch(ctx, payload); err != nil {
		log.Printf("[Enrich] ERROR: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func processEnrichBatch(ctx context.Context, payload EnrichPayload) error {
	fsClient, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		return fmt.Errorf("firestore init: %w", err)
	}
	defer fsClient.Close()

	// Check job not cancelled
	jobRef := fsClient.Collection("tenants").Doc(payload.TenantID).
		Collection("import_jobs").Doc(payload.JobID)
	jobDoc, err := jobRef.Get(ctx)
	if err != nil {
		// google.golang.org/api/iterator uses a different error type
		// Check for Firestore not-found via error string as grpc codes not imported
		if strings.Contains(err.Error(), "NotFound") || strings.Contains(err.Error(), "not found") {
			log.Printf("[Enrich] Job %s not found (deleted?) — skipping", payload.JobID)
			return nil
		}
		return fmt.Errorf("get job: %w", err)
	}
	if status, _ := jobDoc.Data()["status"].(string); status == "cancelled" {
		log.Printf("[Enrich] Job %s cancelled, skipping", payload.JobID)
		return nil
	}

	// Get and decrypt credential
	creds, err := getDecryptedCredentials(ctx, fsClient, payload.TenantID, payload.CredentialID)
	if err != nil {
		return fmt.Errorf("get credentials: %w", err)
	}
	globalKeys, _ := getGlobalKeys(ctx, fsClient, "amazon")
	spapiCreds := buildSPAPICreds(creds, globalKeys)

	// Get LWA access token
	accessToken, err := getLWAAccessToken(spapiCreds)
	if err != nil {
		return fmt.Errorf("LWA auth: %w", err)
	}
	log.Printf("[Enrich] Authenticated. Endpoint=%s Marketplace=%s", spapiCreds.Endpoint, spapiCreds.MarketplaceID)

	var enriched, failed, skipped int

	// logEnrichResult writes a single row to the job's enrich_log subcollection
	// so every outcome (enriched/failed/skipped) is traceable in Firestore.
	logEnrichResult := func(asin, productID, outcome, reason string) {
		fsClient.Collection("tenants").Doc(payload.TenantID).
			Collection("import_jobs").Doc(payload.JobID).
			Collection("enrich_log").NewDoc().Set(ctx, map[string]interface{}{
			"asin":       asin,
			"product_id": productID,
			"outcome":    outcome, // "enriched", "failed", "skipped"
			"reason":     reason,
			"batch":      payload.JobID,
			"logged_at":  time.Now(),
		})
	}

	for idx, item := range payload.Items {
		if item.ASIN == "" {
			log.Printf("[Enrich] Item idx=%d has empty ASIN (product_id=%s) — skipping", idx, item.ProductID)
			logEnrichResult("", item.ProductID, "skipped", "empty ASIN in enrich payload")
			skipped++
			continue
		}

		// Re-check cancellation every 5 items to bail early
		if idx > 0 && idx%5 == 0 {
			jSnap, _ := jobRef.Get(ctx)
			if jSnap != nil {
				if st, _ := jSnap.Data()["status"].(string); st == "cancelled" {
					log.Printf("[Enrich] Job %s cancelled mid-batch at item %d, stopping", payload.JobID, idx)
					return nil
				}
			}
		}

		// Verify product doc exists before calling SP-API.
		// Stale enrich tasks (from a previous import run) will have product IDs
		// that no longer exist — bail out early rather than wasting API quota.
		productRef := fsClient.Collection("tenants").Doc(payload.TenantID).
			Collection("products").Doc(item.ProductID)
		if _, err := productRef.Get(ctx); err != nil {
			if strings.Contains(err.Error(), "NotFound") || strings.Contains(err.Error(), "not found") {
				log.Printf("[Enrich] SKIP: product %s not found (stale task from previous import?), skipping ASIN %s", item.ProductID, item.ASIN)
				logEnrichResult(item.ASIN, item.ProductID, "skipped", "product doc not found — stale enrich task")
				skipped++
				continue
			}
		}

		// Idempotency: skip only if extended_data/amazon already has fetched_at.
		extRef := productRef.Collection("extended_data").Doc("amazon")
		if extSnap, err := extRef.Get(ctx); err == nil {
			data := extSnap.Data()
			if _, hasFetchedAt := data["fetched_at"]; hasFetchedAt {
				// Already enriched — count as enriched not skipped so the job
				// completion counter is accurate. This fires when a task is retried
				// after a partial run or when FBA rows generate duplicate enrich tasks.
				log.Printf("[Enrich] already done for ASIN %s product %s — counting as enriched", item.ASIN, item.ProductID)
				logEnrichResult(item.ASIN, item.ProductID, "enriched", "already done (idempotent)")
				enriched++
				continue
			}
		}

		// Rate limit: 2 req/s with burst of 2 for getCatalogItem
		time.Sleep(catalogRateDelay)

		// Single 2022 API call — returns summaries, images, attributes, identifiers, sales ranks
		catalog2022, st2022, err2022 := getCatalog2022(spapiCreds, accessToken, item.ASIN)
		if err2022 != nil {
			log.Printf("[Enrich] WARN: ASIN %s status=%d: %v", item.ASIN, st2022, err2022)
			logEnrichResult(item.ASIN, item.ProductID, "failed", fmt.Sprintf("catalog API status=%d: %v", st2022, err2022))
			failed++
			continue
		}
		if catalog2022 == nil {
			log.Printf("[Enrich] WARN: ASIN %s returned nil body", item.ASIN)
			logEnrichResult(item.ASIN, item.ProductID, "failed", "catalog API returned nil body")
			failed++
			continue
		}

		normalized := normalizeProduct(item.ASIN, catalog2022)
		if normalized == nil {
			log.Printf("[Enrich] WARN: ASIN %s no meaningful data extracted", item.ASIN)
			logEnrichResult(item.ASIN, item.ProductID, "failed", "normalizeProduct returned nil — no meaningful data in API response")
			failed++
			continue
		}

		// Mirror images to GCS synchronously and replace Amazon CDN URLs with
		// GCS URLs in the normalized map before writing to Firestore.
		// This ensures the product document always contains GCS URLs.
		if imgs, ok := normalized["images"].([]map[string]interface{}); ok && len(imgs) > 0 {
			for i, img := range imgs {
				if srcURL, _ := img["url"].(string); srcURL != "" {
					gcsURL := mirrorImageToGCS(ctx, payload.TenantID, item.ProductID, srcURL)
					if gcsURL != srcURL {
						// Replace Amazon URL with GCS URL in the map
						imgCopy := make(map[string]interface{})
						for k, v := range img {
							imgCopy[k] = v
						}
						imgCopy["url"] = gcsURL
						imgs[i] = imgCopy
					}
				}
			}
			normalized["images"] = imgs
		}

		bulletCount := 0
		if b, ok := normalized["bullets"].([]string); ok {
			bulletCount = len(b)
		}
		imageCount := 0
		if imgs, ok := normalized["images"].([]map[string]interface{}); ok {
			imageCount = len(imgs)
		}
		log.Printf("[Enrich] ASIN %s: title=%q brand=%q imgs=%d bullets=%d",
			item.ASIN, truncStr(fmt.Sprintf("%v", normalized["title"]), 50),
			normalized["brand"], imageCount, bulletCount)

		if err := updateProductWithEnrichedData(ctx, fsClient, payload.TenantID, item.ProductID, normalized); err != nil {
			log.Printf("[Enrich] WARN: update product %s: %v", item.ProductID, err)
			logEnrichResult(item.ASIN, item.ProductID, "failed", fmt.Sprintf("updateProductWithEnrichedData: %v", err))
			failed++
			continue
		}
		saveExtendedData(ctx, fsClient, payload.TenantID, item.ProductID, item.ASIN, catalog2022, normalized)
		logEnrichResult(item.ASIN, item.ProductID, "enriched", "ok")

		// Write child ASINs to the platform-wide ASIN map so any tenant importing
		// a child ASIN can find its parent without needing to call the API.
		upsertPlatformASINMap(ctx, fsClient, item.ASIN, catalog2022)

		// ── Variation parent linkage ──────────────────────────────────────────
		// The Amazon Catalog Items API relationships array contains parentAsins
		// for child products. Extract the parent ASIN and link the product doc
		// to its parent product, marking it as product_type="variation".
		if parentASIN := extractParentASIN(catalog2022, item.ASIN); parentASIN != "" {
			linkVariationToParent(ctx, fsClient, payload.TenantID, item.ProductID, item.ASIN, parentASIN)
		}

		enriched++
	}

	// ── Batch-end update + completion check (transactional) ──────────────
	// Increment failed count and check for completion inside a transaction.
	// This prevents the race condition where multiple concurrent enrich tasks
	// each read a stale count, think they're the last one, and all mark the
	// job complete prematurely. The transaction guarantees we only complete
	// the job once, when the real final counts are reached.
	log.Printf("[Enrich] Batch done: %d enriched, %d failed, %d skipped (already done)", enriched, failed, skipped)

	err = fsClient.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		jobSnap, err := tx.Get(jobRef)
		if err != nil {
			return err
		}
		d := jobSnap.Data()
		enrichTotal, _   := d["enrich_total_items"].(int64)
		enrichedSoFar, _ := d["enriched_items"].(int64)
		failedSoFar, _   := d["enrich_failed_items"].(int64)
		skippedSoFar, _  := d["enrich_skipped_items"].(int64)
		currentStatus, _ := d["status"].(string)

		updates := []firestore.Update{
			{Path: "updated_at", Value: time.Now()},
		}
		if enriched > 0 {
			updates = append(updates, firestore.Update{Path: "enriched_items", Value: firestore.Increment(enriched)})
			enrichedSoFar += int64(enriched)
		}
		if failed > 0 {
			updates = append(updates, firestore.Update{Path: "enrich_failed_items", Value: firestore.Increment(failed)})
			failedSoFar += int64(failed)
		}
		if skipped > 0 {
			updates = append(updates, firestore.Update{Path: "enrich_skipped_items", Value: firestore.Increment(skipped)})
			skippedSoFar += int64(skipped)
		}

		// Mark complete when enriched + failed + skipped reaches total
		if enrichTotal > 0 && currentStatus == "running" && (enrichedSoFar+failedSoFar+skippedSoFar) >= enrichTotal {
			now := time.Now()
			updates = append(updates,
				firestore.Update{Path: "status", Value: "completed"},
				firestore.Update{Path: "status_message", Value: fmt.Sprintf("Enrichment complete: %d enriched, %d skipped, %d failed", enrichedSoFar, skippedSoFar, failedSoFar)},
				firestore.Update{Path: "completed_at", Value: now},
			)
			log.Printf("[Enrich] Job %s COMPLETED — %d enriched, %d skipped, %d failed (total %d)", payload.JobID, enrichedSoFar, skippedSoFar, failedSoFar, enrichTotal)
		}

		return tx.Update(jobRef, updates)
	})
	if err != nil {
		log.Printf("[Enrich] WARN: completion transaction failed: %v", err)
	}

	return nil
}

// ============================================================================
// SP-API CATALOG CALLS (with SigV4)
// ============================================================================

func getCatalog2022(creds SPAPICreds, token, asin string) (map[string]interface{}, int, error) {
	path := fmt.Sprintf("/catalog/2022-04-01/items/%s", asin)
	attempts := []string{
		"attributes,images,identifiers,productTypes,relationships,salesRanks,summaries,variations",
		"attributes,images,identifiers,productTypes,relationships,summaries", // fallback without variations/salesRanks
		"attributes,images,summaries",                                        // last resort
	}
	for _, included := range attempts {
		params := map[string]string{
			"marketplaceIds": creds.MarketplaceID,
			"includedData":   included,
		}
		status, body, err := spapiGet(creds, token, path, params)
		if status == 200 {
			return body, status, nil
		}
		if err != nil {
			log.Printf("[Enrich] 2022 attempt '%s': status=%d err=%v", included, status, err)
		}
	}
	return nil, 0, fmt.Errorf("all 2022 attempts failed for %s", asin)
}

// getCatalog2020 removed — 2022 endpoint returns all needed data

// ============================================================================
// SP-API HTTP WITH RETRY (429 backoff, exponential + jitter)
// ============================================================================

// spapiGet performs a signed SP-API GET request with robust retry handling:
//   - Honours the Retry-After header on 429 responses
//   - Falls back to exponential backoff with jitter if Retry-After is absent
//   - Retries up to maxRetries times before giving up
//   - 400 InvalidInput is not retried — it means the ASIN/params are wrong
func spapiGet(creds SPAPICreds, token, path string, params map[string]string) (int, map[string]interface{}, error) {
	// maxRetries is kept low so tasks complete within the Cloud Run timeout even
	// under heavy rate limiting. Cloud Tasks (maxAttempts=100) re-dispatches the
	// whole task if it fails, providing outer retry resilience without timeouts.
	// Worst case inner retry time: 2+4+8+16s = ~30s per ASIN → 10 ASINs = ~5min max.
	const maxRetries = 3
	baseDelay := 2 * time.Second

	qs := canonicalQS(params)
	urlPathQS := path
	if qs != "" {
		urlPathQS = path + "?" + qs
	}
	fullURL := creds.Endpoint + urlPathQS

	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Re-sign on every attempt — SigV4 timestamps must be fresh
		headers := signV4(creds, token, "GET", path, qs)
		req, err := http.NewRequest("GET", fullURL, nil)
		if err != nil {
			return 0, nil, err
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			if attempt == maxRetries {
				return 0, nil, fmt.Errorf("request failed after %d attempts: %w", maxRetries+1, err)
			}
			time.Sleep(backoffDelay(attempt, baseDelay))
			continue
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		switch {
		case resp.StatusCode == 200:
			var result map[string]interface{}
			if err := json.Unmarshal(body, &result); err != nil {
				return resp.StatusCode, nil, fmt.Errorf("parse: %w", err)
			}
			return resp.StatusCode, result, nil

		case resp.StatusCode == 429:
			if attempt == maxRetries {
				return resp.StatusCode, nil, fmt.Errorf("rate limited after %d retries: %s", maxRetries+1, truncStr(string(body), 200))
			}
			// Honour Retry-After header if present
			delay := backoffDelay(attempt, baseDelay)
			if ra := resp.Header.Get("Retry-After"); ra != "" {
				if secs, err := time.ParseDuration(ra + "s"); err == nil && secs > delay {
					delay = secs
				}
			}
			log.Printf("[Enrich] 429 rate limited, retrying in %v (attempt %d/%d)", delay, attempt+1, maxRetries)
			time.Sleep(delay)

		case resp.StatusCode == 400:
			// InvalidInput — not a transient error, don't retry
			return resp.StatusCode, nil, fmt.Errorf("API 400: %s", truncStr(string(body), 200))

		case resp.StatusCode >= 500:
			// Server error — retry with backoff
			if attempt == maxRetries {
				return resp.StatusCode, nil, fmt.Errorf("server error %d after %d retries: %s", resp.StatusCode, maxRetries+1, truncStr(string(body), 200))
			}
			delay := backoffDelay(attempt, baseDelay)
			log.Printf("[Enrich] %d server error, retrying in %v (attempt %d/%d)", resp.StatusCode, delay, attempt+1, maxRetries)
			time.Sleep(delay)

		default:
			return resp.StatusCode, nil, fmt.Errorf("API %d: %s", resp.StatusCode, truncStr(string(body), 200))
		}
	}
	return 0, nil, fmt.Errorf("spapiGet: exhausted retries")
}

// backoffDelay returns exponential backoff with ±25% jitter.
// attempt=0 → ~baseDelay, attempt=1 → ~2×base, attempt=2 → ~4×base, etc.
func backoffDelay(attempt int, base time.Duration) time.Duration {
	exp := base * (1 << uint(attempt)) // base * 2^attempt
	// Cap at 20 seconds — with maxRetries=3 this keeps total task time well under timeout
	if exp > 20*time.Second {
		exp = 20 * time.Second
	}
	// Add ±25% jitter using current nanoseconds as a cheap random source
	jitterRange := int64(exp) / 4
	jitter := (time.Now().UnixNano() % (2*jitterRange + 1)) - jitterRange
	return exp + time.Duration(jitter)
}

// ============================================================================
// AWS SigV4 SIGNING (matching Python spapi.py _sign)
// ============================================================================

func signV4(creds SPAPICreds, accessToken, method, canonicalURI, canonicalQueryString string) map[string]string {
	t := time.Now().UTC()
	amzDate := t.Format("20060102T150405Z")
	dateStamp := t.Format("20060102")
	host := strings.TrimPrefix(strings.TrimPrefix(creds.Endpoint, "https://"), "http://")
	payloadHash := "UNSIGNED-PAYLOAD"

	type hdr struct{ k, v string }
	hdrs := []hdr{
		{"accept", "application/json"},
		{"host", host},
		{"user-agent", "marketmate-spapi/1.0"},
		{"x-amz-access-token", accessToken},
		{"x-amz-content-sha256", payloadHash},
	}
	if creds.AWSSessionToken != "" {
		hdrs = append(hdrs, hdr{"x-amz-security-token", creds.AWSSessionToken})
	}
	hdrs = append(hdrs, hdr{"x-amz-date", amzDate})
	sort.Slice(hdrs, func(i, j int) bool { return hdrs[i].k < hdrs[j].k })

	var canonHdrs strings.Builder
	var signedList []string
	for _, h := range hdrs {
		canonHdrs.WriteString(h.k + ":" + h.v + "\n")
		signedList = append(signedList, h.k)
	}
	signedHeaders := strings.Join(signedList, ";")

	canonReq := strings.Join([]string{method, canonicalURI, canonicalQueryString, canonHdrs.String(), signedHeaders, payloadHash}, "\n")
	algorithm := "AWS4-HMAC-SHA256"
	credScope := dateStamp + "/" + creds.AWSRegion + "/execute-api/aws4_request"
	strToSign := algorithm + "\n" + amzDate + "\n" + credScope + "\n" + sha256Hex([]byte(canonReq))

	kDate := hmacSHA256([]byte("AWS4"+creds.AWSSecretKey), []byte(dateStamp))
	kRegion := hmacSHA256(kDate, []byte(creds.AWSRegion))
	kService := hmacSHA256(kRegion, []byte("execute-api"))
	kSigning := hmacSHA256(kService, []byte("aws4_request"))
	sig := hex.EncodeToString(hmacSHA256(kSigning, []byte(strToSign)))

	auth := fmt.Sprintf("%s Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		algorithm, creds.AWSAccessKeyID, credScope, signedHeaders, sig)

	out := map[string]string{
		"host": host, "x-amz-date": amzDate, "x-amz-access-token": accessToken,
		"user-agent": "marketmate-spapi/1.0", "accept": "application/json",
		"x-amz-content-sha256": payloadHash, "Authorization": auth,
	}
	if creds.AWSSessionToken != "" {
		out["x-amz-security-token"] = creds.AWSSessionToken
	}
	return out
}

func canonicalQS(params map[string]string) string {
	if len(params) == 0 {
		return ""
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, rfc3986Encode(k)+"="+rfc3986Encode(params[k]))
	}
	return strings.Join(parts, "&")
}

func rfc3986Encode(s string) string {
	encoded := url.QueryEscape(s)
	encoded = strings.ReplaceAll(encoded, "+", "%20")
	return encoded
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// ============================================================================
// LWA TOKEN
// ============================================================================

func getLWAAccessToken(creds SPAPICreds) (string, error) {
	data := url.Values{
		"grant_type": {"refresh_token"}, "refresh_token": {creds.RefreshToken},
		"client_id": {creds.LWAClientID}, "client_secret": {creds.LWAClientSecret},
	}
	resp, err := http.PostForm(lwaTokenEndpoint, data)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var r map[string]interface{}
	json.Unmarshal(body, &r)
	if errMsg, ok := r["error"].(string); ok {
		return "", fmt.Errorf("LWA: %s: %v", errMsg, r["error_description"])
	}
	tok, _ := r["access_token"].(string)
	if tok == "" {
		return "", fmt.Errorf("no access_token in LWA response")
	}
	return tok, nil
}

// ============================================================================
// NORMALIZATION (matching Python _normalize)
// ============================================================================

func normalizeProduct(asin string, body2022 map[string]interface{}) map[string]interface{} {
	if body2022 == nil {
		return nil
	}

	attrs22 := toMap(body2022["attributes"])
	summaries22 := toSlice(body2022["summaries"])
	images22 := toSlice(body2022["images"])
	salesRanks22 := toSlice(body2022["salesRanks"])
	productTypes22 := toSlice(body2022["productTypes"])
	identifiers22 := toSlice(body2022["identifiers"])
	variations22 := toSlice(body2022["variations"])
	relationships22 := toSlice(body2022["relationships"])

	attrsFlat := flattenAttributes(attrs22)

	// Title
	title := summaryField(summaries22, "itemName")
	if title == "" {
		if v, ok := attrsFlat["item_name"].(string); ok {
			title = v
		}
	}

	// Brand
	brand := summaryField(summaries22, "brand")

	// Manufacturer
	manufacturer := summaryField(summaries22, "manufacturer")
	if manufacturer == "" {
		if v, ok := attrsFlat["manufacturer"].(string); ok {
			manufacturer = v
		}
	}

	// Bullets from raw attributes
	var bullets []string
	if bp, ok := attrs22["bullet_point"].([]interface{}); ok {
		for _, item := range bp {
			if m, ok := item.(map[string]interface{}); ok {
				if val, ok := m["value"].(string); ok && val != "" {
					bullets = append(bullets, val)
				}
			}
		}
	}

	// Description
	description := ""
	if pd, ok := attrs22["product_description"].([]interface{}); ok && len(pd) > 0 {
		if m, ok := pd[0].(map[string]interface{}); ok {
			if val, ok := m["value"].(string); ok {
				description = val
			}
		}
	}
	if description == "" {
		if id, ok := attrsFlat["item_description"].(string); ok {
			description = id
		}
	}
	if description == "" && len(bullets) > 0 {
		description = "• " + strings.Join(bullets, "\n• ")
	}

	images := extractImages(images22)
	identifiers := extractIdentifiers(identifiers22)
	salesRanks := extractSalesRanks(salesRanks22)

	productType := ""
	if len(productTypes22) > 0 {
		if pt := toMap(productTypes22[0]); pt != nil {
			productType, _ = pt["productType"].(string)
		}
	}

	dimensions := extractDimensions(attrs22)
	shippingDimensions := extractShippingDimensions(attrs22)
	weight := extractWeight(attrs22)

	color, _ := attrsFlat["color"].(string)
	if color == "" {
		color = summaryField(summaries22, "color")
	}
	size, _ := attrsFlat["size"].(string)
	if size == "" {
		size = summaryField(summaries22, "size")
	}
	style, _ := attrsFlat["style"].(string)
	modelNumber, _ := attrsFlat["model_number"].(string)
	if modelNumber == "" {
		modelNumber = summaryField(summaries22, "modelNumber")
	}
	partNumber := summaryField(summaries22, "partNumber")

	// Browse nodes
	var browseNodes []interface{}
	if len(summaries22) > 0 {
		if s := toMap(summaries22[0]); s != nil {
			browseNodes = toSlice(s["browseNodeIdentifiers"])
		}
	}

	// Only return if we got something useful
	if title == "" && brand == "" && len(images) == 0 && len(bullets) == 0 {
		return nil
	}

	return map[string]interface{}{
		"asin": asin, "title": title, "brand": brand, "manufacturer": manufacturer,
		"description": description, "bullets": bullets, "images": images,
		"identifiers": identifiers, "sales_ranks": salesRanks, "product_type": productType,
		"dimensions": dimensions, "shipping_dimensions": shippingDimensions,
		"weight": weight, "color": color, "size": size,
		"style": style, "model_number": modelNumber, "part_number": partNumber,
		"variations": variations22, "relationships": relationships22,
		"attributes_flat": attrsFlat, "browse_nodes": browseNodes,
	}
}

func flattenAttributes(attrs map[string]interface{}) map[string]interface{} {
	flat := make(map[string]interface{})
	for k, v := range attrs {
		switch val := v.(type) {
		case []interface{}:
			var values []interface{}
			for _, entry := range val {
				if m, ok := entry.(map[string]interface{}); ok {
					if v, exists := m["value"]; exists {
						values = append(values, v)
					} else if v, exists := m["display_value"]; exists {
						values = append(values, v)
					} else {
						values = append(values, entry)
					}
				} else {
					values = append(values, entry)
				}
			}
			if len(values) == 1 {
				flat[k] = values[0]
			} else if len(values) > 1 {
				flat[k] = values
			}
		default:
			flat[k] = v
		}
	}
	return flat
}

// mirrorImageToGCS downloads an image from srcURL and uploads it to GCS under
// tenants/{tenantID}/products/{productID}/images/{filename}.
// Returns the public GCS URL on success, or srcURL unchanged on any error so
// the import never fails due to an image problem.
func mirrorImageToGCS(ctx context.Context, tenantID, productID, srcURL string) string {
	bucketName := os.Getenv("GCS_BUCKET_NAME")
	if bucketName == "" {
		return srcURL
	}

	// Derive a stable filename from the URL path
	parsed, err := url.Parse(srcURL)
	if err != nil {
		return srcURL
	}
	filename := path.Base(parsed.Path)
	if filename == "" || filename == "." {
		filename = "image.jpg"
	}
	// Ensure it has an extension
	if !strings.Contains(filename, ".") {
		filename += ".jpg"
	}

	gcsPath := fmt.Sprintf("%s/products/%s/images/%s", tenantID, productID, filename)

	// Check if already uploaded (idempotent re-enrichment)
	gcsClient, err := storage.NewClient(ctx)
	if err != nil {
		log.Printf("[Enrich] GCS client error for %s: %v", filename, err)
		return srcURL
	}
	defer gcsClient.Close()

	bucket := gcsClient.Bucket(bucketName)

	// Check if object already exists — skip download if so
	_, err = bucket.Object(gcsPath).Attrs(ctx)
	if err == nil {
		// Already in GCS
		return fmt.Sprintf("https://storage.googleapis.com/%s/%s", bucketName, gcsPath)
	}

	// Download from Amazon CDN
	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Get(srcURL)
	if err != nil {
		log.Printf("[Enrich] Failed to download image %s: %v", srcURL, err)
		return srcURL
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[Enrich] Image download returned %d for %s", resp.StatusCode, srcURL)
		return srcURL
	}

	// Determine content type
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "image/jpeg"
	}

	// Upload to GCS
	obj := bucket.Object(gcsPath)
	w := obj.NewWriter(ctx)
	w.ContentType = contentType
	w.CacheControl = "public, max-age=31536000" // 1 year — images are immutable

	if _, err := io.Copy(w, resp.Body); err != nil {
		w.Close()
		log.Printf("[Enrich] GCS upload failed for %s: %v", gcsPath, err)
		return srcURL
	}
	if err := w.Close(); err != nil {
		log.Printf("[Enrich] GCS writer close failed for %s: %v", gcsPath, err)
		return srcURL
	}

	gcsURL := fmt.Sprintf("https://storage.googleapis.com/%s/%s", bucketName, gcsPath)
	log.Printf("[Enrich] ✓ image mirrored to GCS: %s", gcsPath)
	return gcsURL
}

func extractImages(imagesArr []interface{}) []map[string]interface{} {
	// Track the largest image per variant (MAIN, PT01, PT02, etc.)
	type imgEntry struct {
		url     string
		role    string
		variant string
		width   int
		height  int
		pixels  int
	}
	largest := make(map[string]*imgEntry)

	for _, imgGroup := range imagesArr {
		group := toMap(imgGroup)
		if group == nil {
			continue
		}
		for _, img := range toSlice(group["images"]) {
			imgMap := toMap(img)
			if imgMap == nil {
				continue
			}
			link, _ := imgMap["link"].(string)
			if link == "" {
				continue
			}
			variant, _ := imgMap["variant"].(string)
			if variant == "" {
				variant = "UNKNOWN"
			}
			height, _ := imgMap["height"].(float64)
			width, _ := imgMap["width"].(float64)
			pixels := int(height) * int(width)

			role := "gallery"
			if variant == "MAIN" {
				role = "primary_image"
			}

			existing, exists := largest[variant]
			if !exists || pixels > existing.pixels {
				largest[variant] = &imgEntry{
					url: link, role: role, variant: variant,
					width: int(width), height: int(height), pixels: pixels,
				}
			}
		}
	}

	// Convert to sorted slice — MAIN first, then PT01, PT02, etc.
	var assets []map[string]interface{}
	// Add MAIN first if it exists
	if main, ok := largest["MAIN"]; ok {
		assets = append(assets, map[string]interface{}{
			"url": main.url, "role": main.role, "variant": main.variant,
			"sort_order": 0, "width": main.width, "height": main.height,
		})
		delete(largest, "MAIN")
	}
	// Add remaining sorted by variant name
	keys := make([]string, 0, len(largest))
	for k := range largest {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		e := largest[k]
		assets = append(assets, map[string]interface{}{
			"url": e.url, "role": e.role, "variant": e.variant,
			"sort_order": len(assets), "width": e.width, "height": e.height,
		})
	}
	return assets
}

func extractIdentifiers(identifiersArr []interface{}) map[string]string {
	result := make(map[string]string)
	for _, idGroup := range identifiersArr {
		group := toMap(idGroup)
		if group == nil {
			continue
		}
		for _, id := range toSlice(group["identifiers"]) {
			idMap := toMap(id)
			if idMap == nil {
				continue
			}
			idType, _ := idMap["identifierType"].(string)
			idValue, _ := idMap["identifier"].(string)
			if idType == "" || idValue == "" {
				continue
			}
			switch strings.ToUpper(idType) {
			case "UPC", "UPC_A":
				result["upc"] = idValue
			case "EAN":
				result["ean"] = idValue
			case "ISBN":
				result["isbn"] = idValue
			case "GTIN":
				result["gtin"] = idValue
			}
		}
	}
	return result
}

func extractSalesRanks(salesRanksArr []interface{}) []map[string]interface{} {
	var ranks []map[string]interface{}
	for _, srGroup := range salesRanksArr {
		group := toMap(srGroup)
		if group == nil {
			continue
		}
		for _, cr := range toSlice(group["classificationRanks"]) {
			crMap := toMap(cr)
			if crMap == nil {
				continue
			}
			title, _ := crMap["title"].(string)
			rank, _ := crMap["rank"].(float64)
			if title != "" && rank > 0 {
				ranks = append(ranks, map[string]interface{}{"type": "classification", "title": title, "rank": int(rank)})
			}
		}
		for _, dr := range toSlice(group["displayGroupRanks"]) {
			drMap := toMap(dr)
			if drMap == nil {
				continue
			}
			title, _ := drMap["title"].(string)
			rank, _ := drMap["rank"].(float64)
			if title != "" && rank > 0 {
				ranks = append(ranks, map[string]interface{}{"type": "display_group", "title": title, "rank": int(rank)})
			}
		}
	}
	return ranks
}

func extractDimensions(attrs22 map[string]interface{}) map[string]interface{} {
	// Try item_dimensions first, fall back to item_package_dimensions
	raw := attrs22["item_dimensions"]
	if raw == nil {
		raw = attrs22["item_package_dimensions"]
	}
	return parseDimFieldsFromRaw(raw)
}

func extractShippingDimensions(attrs22 map[string]interface{}) map[string]interface{} {
	return parseDimFieldsFromRaw(attrs22["item_package_dimensions"])
}

// parseDimFieldsFromRaw handles the raw SP-API attributes structure where
// each dimension field is either:
//   - A map with {value, unit} keys (standard SP-API 2022 format)
//   - A slice containing such maps (marketplace-specific format)
//   - A direct float64 (already flattened)
//
// The raw input is either the dimension map directly, or a slice wrapping it.
func parseDimFieldsFromRaw(raw interface{}) map[string]interface{} {
	dims := make(map[string]interface{})
	if raw == nil {
		return dims
	}

	// Unwrap slice if needed — SP-API sometimes wraps in [{...}]
	var dimMap map[string]interface{}
	switch v := raw.(type) {
	case map[string]interface{}:
		dimMap = v
	case []interface{}:
		if len(v) > 0 {
			dimMap = toMap(v[0])
		}
	}
	if dimMap == nil {
		return dims
	}

	parseDimFieldsNormalized(dimMap, dims)
	return dims
}

// parseDimFieldsNormalized extracts length/width/height and normalizes the unit
// to a short form (cm, m, in, mm) compatible with the Product model
func parseDimFieldsNormalized(m map[string]interface{}, dims map[string]interface{}) {
	var detectedUnit string
	for _, field := range []string{"length", "width", "height"} {
		if val := m[field]; val != nil {
			switch v := val.(type) {
			case map[string]interface{}:
				if amt, ok := v["value"].(float64); ok {
					dims[field] = amt
					if unit, ok := v["unit"].(string); ok && unit != "" {
						detectedUnit = unit
					}
				}
			case float64:
				dims[field] = v
			}
		}
	}
	if unit, ok := m["unit"].(string); ok && unit != "" {
		detectedUnit = unit
	}
	// Normalize unit to short form
	if detectedUnit != "" {
		dims["unit"] = normalizeUnit(detectedUnit)
	}
}

func normalizeUnit(unit string) string {
	switch strings.ToLower(unit) {
	case "centimeters", "centimeter", "cm":
		return "cm"
	case "meters", "meter", "m":
		return "m"
	case "inches", "inch", "in":
		return "in"
	case "millimeters", "millimeter", "mm":
		return "mm"
	case "feet", "foot", "ft":
		return "ft"
	default:
		return unit
	}
}

func extractWeight(attrs22 map[string]interface{}) map[string]interface{} {
	weight := make(map[string]interface{})
	for _, key := range []string{"item_weight", "item_package_weight"} {
		raw := attrs22[key]
		if raw == nil {
			continue
		}
		// Unwrap slice if needed
		var weightMap map[string]interface{}
		switch v := raw.(type) {
		case map[string]interface{}:
			weightMap = v
		case []interface{}:
			if len(v) > 0 {
				weightMap = toMap(v[0])
			}
		case float64:
			// Already a scalar — unit unknown, store value only
			weight["value"] = v
			weight["source"] = key
			return weight
		}
		if weightMap == nil {
			continue
		}
		if amt, ok := weightMap["value"].(float64); ok {
			weight["value"] = amt
			if u, ok := weightMap["unit"].(string); ok && u != "" {
				weight["unit"] = normalizeUnit(u)
			}
			weight["source"] = key
			return weight
		}
	}
	return weight
}

// ============================================================================
// PRODUCT UPDATE
// ============================================================================

func updateProductWithEnrichedData(ctx context.Context, client *firestore.Client, tenantID, productID string, n map[string]interface{}) error {
	ref := client.Collection("tenants").Doc(tenantID).Collection("products").Doc(productID)

	// Check if this product has been enriched before (ie not first import)
	doc, err := ref.Get(ctx)
	if err != nil {
		return fmt.Errorf("product not found: %v", err)
	}
	_, previouslyEnriched := doc.Data()["enriched_at"]

	updates := []firestore.Update{
		{Path: "updated_at", Value: time.Now()},
		{Path: "enriched_at", Value: time.Now()},
	}

	// For first enrichment: write everything we can
	// For subsequent: only fill blanks (default until user setting is added)
	if !previouslyEnriched {
		// First time — write all basic detail fields
		if v, ok := n["title"].(string); ok && v != "" {
			updates = append(updates, firestore.Update{Path: "title", Value: v})
		}
		if v, ok := n["description"].(string); ok && v != "" {
			updates = append(updates, firestore.Update{Path: "description", Value: v})
		}
		if v, ok := n["brand"].(string); ok && v != "" {
			updates = append(updates, firestore.Update{Path: "brand", Value: v})
		}
		if imgs, ok := n["images"].([]map[string]interface{}); ok && len(imgs) > 0 {
			updates = append(updates, firestore.Update{Path: "assets", Value: imgs})
		}
		if ids, ok := n["identifiers"].(map[string]string); ok {
			for k, v := range ids {
				updates = append(updates, firestore.Update{Path: "identifiers." + k, Value: v})
			}
		}
		if dims, ok := n["dimensions"].(map[string]interface{}); ok && len(dims) > 0 {
			updates = append(updates, firestore.Update{Path: "dimensions", Value: dims})
		}
		if sdims, ok := n["shipping_dimensions"].(map[string]interface{}); ok && len(sdims) > 0 {
			updates = append(updates, firestore.Update{Path: "shipping_dimensions", Value: sdims})
		}
		if w, ok := n["weight"].(map[string]interface{}); ok && len(w) > 0 {
			updates = append(updates, firestore.Update{Path: "weight", Value: w})
		}
		if b, ok := n["bullets"].([]string); ok && len(b) > 0 {
			updates = append(updates, firestore.Update{Path: "attributes.bullet_points", Value: b})
		}
		// Additional attributes for basic details
		attrMap := map[string]string{
			"manufacturer": "manufacturer", "model_number": "model_number", "part_number": "part_number",
			"color": "color", "size": "size", "style": "style", "product_type": "amazon_product_type",
		}
		for nk, ak := range attrMap {
			if v, ok := n[nk].(string); ok && v != "" {
				updates = append(updates, firestore.Update{Path: "attributes." + ak, Value: v})
			}
		}
	} else {
		// Subsequent enrichment — only fill blanks
		existing := doc.Data()
		if v, ok := n["title"].(string); ok && v != "" {
			if et, _ := existing["title"].(string); et == "" {
				updates = append(updates, firestore.Update{Path: "title", Value: v})
			}
		}
		if v, ok := n["brand"].(string); ok && v != "" {
			if eb, _ := existing["brand"].(string); eb == "" {
				updates = append(updates, firestore.Update{Path: "brand", Value: v})
			}
		}
		if imgs, ok := n["images"].([]map[string]interface{}); ok && len(imgs) > 0 {
			if ea, _ := existing["assets"].([]interface{}); len(ea) == 0 {
				updates = append(updates, firestore.Update{Path: "assets", Value: imgs})
			}
		}
		if ids, ok := n["identifiers"].(map[string]string); ok {
			existingIds, _ := existing["identifiers"].(map[string]interface{})
			if existingIds == nil {
				existingIds = map[string]interface{}{}
			}
			for k, v := range ids {
				if _, exists := existingIds[k]; !exists {
					updates = append(updates, firestore.Update{Path: "identifiers." + k, Value: v})
				}
			}
		}
		if dims, ok := n["dimensions"].(map[string]interface{}); ok && len(dims) > 0 {
			if ed, _ := existing["dimensions"].(map[string]interface{}); len(ed) == 0 {
				updates = append(updates, firestore.Update{Path: "dimensions", Value: dims})
			}
		}
		if sdims, ok := n["shipping_dimensions"].(map[string]interface{}); ok && len(sdims) > 0 {
			if esd, _ := existing["shipping_dimensions"].(map[string]interface{}); len(esd) == 0 {
				updates = append(updates, firestore.Update{Path: "shipping_dimensions", Value: sdims})
			}
		}
		if b, ok := n["bullets"].([]string); ok && len(b) > 0 {
			attrs, _ := existing["attributes"].(map[string]interface{})
			if attrs == nil {
				attrs = map[string]interface{}{}
			}
			if _, hasBullets := attrs["bullet_points"]; !hasBullets {
				updates = append(updates, firestore.Update{Path: "attributes.bullet_points", Value: b})
			}
		}
	}

	_, err = ref.Update(ctx, updates)
	if err != nil {
		return fmt.Errorf("update product: %v", err)
	}

	// Also update the listing doc with enriched data for the listing detail page
	updateListingWithEnrichedData(ctx, client, tenantID, productID, n)

	return nil
}

// updateListingWithEnrichedData writes all enriched fields to the listing doc
func updateListingWithEnrichedData(ctx context.Context, client *firestore.Client, tenantID, productID string, n map[string]interface{}) {
	// Find the listing for this product
	iter := client.Collection("tenants").Doc(tenantID).Collection("listings").
		Where("product_id", "==", productID).Limit(1).Documents(ctx)
	doc, err := iter.Next()
	if err != nil {
		log.Printf("[Enrich] WARN: listing lookup for product %s: %v", productID, err)
		return
	}

	// Load blocked fields
	blockedFields := loadBlockedFields(ctx, client)

	// Build the enriched_data map for the listing — all fields except blocked ones
	enrichedData := make(map[string]interface{})

	// Core fields — always included
	if v, ok := n["title"].(string); ok && v != "" {
		enrichedData["title"] = v
	}
	if v, ok := n["brand"].(string); ok && v != "" {
		enrichedData["brand"] = v
	}
	if v, ok := n["manufacturer"].(string); ok && v != "" {
		enrichedData["manufacturer"] = v
	}
	if v, ok := n["description"].(string); ok && v != "" {
		enrichedData["description"] = v
	}
	if b, ok := n["bullets"].([]string); ok && len(b) > 0 {
		enrichedData["bullets"] = b
	}
	if imgs, ok := n["images"].([]map[string]interface{}); ok && len(imgs) > 0 {
		enrichedData["images"] = imgs
	}
	if dims, ok := n["dimensions"].(map[string]interface{}); ok && len(dims) > 0 {
		enrichedData["dimensions"] = dims
	}
	if sdims, ok := n["shipping_dimensions"].(map[string]interface{}); ok && len(sdims) > 0 {
		enrichedData["shipping_dimensions"] = sdims
	}
	if w, ok := n["weight"].(map[string]interface{}); ok && len(w) > 0 {
		enrichedData["weight"] = w
	}
	if ids, ok := n["identifiers"].(map[string]string); ok && len(ids) > 0 {
		enrichedData["identifiers"] = ids
	}
	if v, ok := n["asin"].(string); ok && v != "" {
		enrichedData["asin"] = v
	}
	if v, ok := n["product_type"].(string); ok && v != "" {
		enrichedData["product_type"] = v
	}
	if v, ok := n["color"].(string); ok && v != "" {
		enrichedData["color"] = v
	}
	if v, ok := n["size"].(string); ok && v != "" {
		enrichedData["size"] = v
	}
	if v, ok := n["style"].(string); ok && v != "" {
		enrichedData["style"] = v
	}
	if v, ok := n["model_number"].(string); ok && v != "" {
		enrichedData["model_number"] = v
	}
	if v, ok := n["part_number"].(string); ok && v != "" {
		enrichedData["part_number"] = v
	}
	if r, ok := n["sales_ranks"].([]map[string]interface{}); ok && len(r) > 0 {
		enrichedData["sales_ranks"] = r
	}
	if v := n["variations"]; v != nil {
		enrichedData["variations"] = v
	}
	if v := n["relationships"]; v != nil {
		enrichedData["relationships"] = v
	}

	// Flatten attributes, excluding blocked fields
	if af, ok := n["attributes_flat"].(map[string]interface{}); ok && len(af) > 0 {
		filteredAttrs := make(map[string]interface{})
		for k, v := range af {
			if !blockedFields[k] {
				filteredAttrs[k] = v
			}
		}
		enrichedData["attributes"] = filteredAttrs
	}

	listingUpdates := []firestore.Update{
		{Path: "enriched_data", Value: enrichedData},
		{Path: "enriched_at", Value: time.Now()},
		{Path: "updated_at", Value: time.Now()},
	}

	if _, err := doc.Ref.Update(ctx, listingUpdates); err != nil {
		log.Printf("[Enrich] WARN: listing update for product %s: %v", productID, err)
	} else {
		log.Printf("[Enrich] ✓ listing enriched for product %s", productID)
	}
}

// loadBlockedFields reads the global blocked fields list
func loadBlockedFields(ctx context.Context, client *firestore.Client) map[string]bool {
	blocked := make(map[string]bool)
	doc, err := client.Collection("platform_config").Doc("listing_field_blocks").Get(ctx)
	if err != nil {
		return blocked // No blocked fields configured yet
	}
	if fields, ok := doc.Data()["blocked_fields"].([]interface{}); ok {
		for _, f := range fields {
			if s, ok := f.(string); ok {
				blocked[s] = true
			}
		}
	}
	return blocked
}

func saveExtendedData(ctx context.Context, client *firestore.Client, tenantID, productID, asin string, raw2022 map[string]interface{}, normalized map[string]interface{}) {
	// Extended data is stored as a subcollection of the product doc:
	// /tenants/{tenantID}/products/{productID}/extended_data/amazon
	// This keeps channel data co-located with the product and avoids a
	// top-level collection that is hard to query and secure separately.
	// firestore.MergeAll ensures we never wipe data from other channels.
	data := map[string]interface{}{
		"product_id": productID,
		"updated_at": time.Now(),
		"asin":       asin,
		"source":     "amazon_catalog_api",
		"raw_2022":   raw2022,
		"normalized": normalized,
		"fetched_at": time.Now(),
	}
	// Subcollection path: products/{productID}/extended_data/amazon
	client.Collection("tenants").Doc(tenantID).
		Collection("products").Doc(productID).
		Collection("extended_data").Doc("amazon").Set(ctx, data, firestore.MergeAll)

	// Also write a lightweight summary to the top-level extended_data collection
	// (keyed by productID) so existing queries that read from there still work
	// during migration. The "amazon" key mirrors what was there before.
	summaryData := map[string]interface{}{
		"product_id": productID,
		"updated_at": time.Now(),
		"amazon": map[string]interface{}{
			"asin":       asin,
			"source":     "amazon_catalog_api",
			"normalized": normalized,
			"fetched_at": time.Now(),
		},
	}
	client.Collection("tenants").Doc(tenantID).
		Collection("extended_data").Doc(productID).Set(ctx, summaryData, firestore.MergeAll)
}

// ============================================================================
// PLATFORM ASIN MAP
// ============================================================================

// upsertPlatformASINMap writes variation relationship data to a platform-wide
// collection so any tenant importing a child ASIN can find its parent without
// needing to call the SP-API.
//
// Collection structure:
//   platform_asin_map/{parentASIN}
//     → { parent_asin, child_asins: [...], updated_at }
//   platform_asin_map/{parentASIN}/children/{childASIN}
//     → { child_asin, parent_asin, variation_attributes: {color, size, ...}, updated_at }
//
// Also writes the reverse lookup:
//   platform_asin_child_index/{childASIN}
//     → { child_asin, parent_asin, updated_at }
//
// This lets batch.go do a single doc read on import to find a child's parent.
func upsertPlatformASINMap(ctx context.Context, client *firestore.Client, asin string, raw2022 map[string]interface{}) {
	if raw2022 == nil {
		return
	}

	variations := toSlice(raw2022["variations"])

	// Collect child ASINs with their variation attributes from relationships.
	// Amazon 2022 API nests relationships two levels deep:
	//   relationships[].relationships[].childAsins
	type childInfo struct {
		asin  string
		attrs map[string]interface{}
	}
	var children []childInfo

	processRelEntry := func(relMap map[string]interface{}) {
		childASINs := toSlice(relMap["childAsins"])
		variationAttrs := toSlice(relMap["variationTheme"])
		var themeLabels []string
		for _, vt := range variationAttrs {
			if vtMap := toMap(vt); vtMap != nil {
				if attrs := toSlice(vtMap["attributes"]); attrs != nil {
					for _, a := range attrs {
						if s, ok := a.(string); ok {
							themeLabels = append(themeLabels, s)
						}
					}
				}
			}
		}
		for _, ca := range childASINs {
			if childASIN, ok := ca.(string); ok && childASIN != "" && childASIN != asin {
				children = append(children, childInfo{
					asin:  childASIN,
					attrs: map[string]interface{}{"variation_theme": themeLabels},
				})
			}
		}
	}

	for _, rel := range toSlice(raw2022["relationships"]) {
		outerMap := toMap(rel)
		if outerMap == nil {
			continue
		}
		// Flat structure (some categories)
		processRelEntry(outerMap)
		// Nested structure (Amazon 2022 standard)
		for _, inner := range toSlice(outerMap["relationships"]) {
			if innerMap := toMap(inner); innerMap != nil {
				processRelEntry(innerMap)
			}
		}
	}

	// Also pick up from variations array if relationships was empty
	for _, v := range variations {
		vMap := toMap(v)
		if vMap == nil {
			continue
		}
		childASIN, _ := vMap["asin"].(string)
		if childASIN == "" || childASIN == asin {
			continue
		}
		// Check not already added
		already := false
		for _, c := range children {
			if c.asin == childASIN {
				already = true
				break
			}
		}
		if !already {
			attrs := map[string]interface{}{}
			if dims := toSlice(vMap["dimensionValues"]); dims != nil {
				for _, d := range dims {
					if dm := toMap(d); dm != nil {
						name, _ := dm["name"].(string)
						val, _ := dm["value"].(string)
						if name != "" && val != "" {
							attrs[name] = val
						}
					}
				}
			}
			children = append(children, childInfo{asin: childASIN, attrs: attrs})
		}
	}

	if len(children) == 0 {
		// This ASIN has no children — write reverse lookup only if it might be a child itself
		// (handled by batch.go checking the child index on import)
		return
	}

	now := time.Now()
	parentRef := client.Collection("platform_asin_map").Doc(asin)

	// Build child ASIN list for the parent doc
	childASINList := make([]string, len(children))
	for i, c := range children {
		childASINList[i] = c.asin
	}

	// Write parent doc
	_, err := parentRef.Set(ctx, map[string]interface{}{
		"parent_asin": asin,
		"child_asins": childASINList,
		"updated_at":  now,
	}, firestore.MergeAll)
	if err != nil {
		log.Printf("[Enrich] WARN: platform_asin_map write for %s: %v", asin, err)
		return
	}

	// Write each child as a subcollection doc + reverse index
	for _, child := range children {
		childDoc := map[string]interface{}{
			"child_asin":           child.asin,
			"parent_asin":          asin,
			"variation_attributes": child.attrs,
			"updated_at":           now,
		}
		// Subcollection: platform_asin_map/{parentASIN}/children/{childASIN}
		parentRef.Collection("children").Doc(child.asin).Set(ctx, childDoc, firestore.MergeAll)

		// Reverse index: platform_asin_child_index/{childASIN}
		client.Collection("platform_asin_child_index").Doc(child.asin).Set(ctx, map[string]interface{}{
			"child_asin":  child.asin,
			"parent_asin": asin,
			"updated_at":  now,
		}, firestore.MergeAll)
	}

	log.Printf("[Enrich] platform_asin_map: %s → %d children written", asin, len(children))
}

// ============================================================================
// VARIATION HELPERS
// ============================================================================

// extractParentASIN reads the Amazon Catalog Items API relationships array for
// the given ASIN and returns the parent ASIN if this product is a variation child.
//
// The Catalog Items 2022 API returns relationships entries like:
//
//	{ "type": "VARIATION", "childAsins": [...], "parentAsins": ["B0PARENTXX"] }
//
// When the current ASIN is a child, parentAsins will be non-empty and childAsins
// will be empty (or absent). When it is a parent, childAsins will be populated.
// extractParentASIN reads the Amazon Catalog Items API relationships array.
// The 2022 API returns a two-level structure:
//
//	relationships: [
//	  { marketplaceId: "...", relationships: [
//	      { type: "VARIATION", parentAsins: ["B0PARENT"], variationTheme: {...} }
//	  ]}
//	]
//
// So we must descend into the inner relationships array to find parentAsins.
func extractParentASIN(raw2022 map[string]interface{}, currentASIN string) string {
	if raw2022 == nil {
		return ""
	}

	// checkRelEntry checks a single relationship entry for parentAsins
	checkRelEntry := func(relMap map[string]interface{}) string {
		parentAsins := toSlice(relMap["parentAsins"])
		childAsins := toSlice(relMap["childAsins"])
		if len(parentAsins) > 0 && len(childAsins) == 0 {
			if pa, ok := parentAsins[0].(string); ok && pa != "" && pa != currentASIN {
				return pa
			}
		}
		return ""
	}

	outer := toSlice(raw2022["relationships"])
	for _, rel := range outer {
		outerMap := toMap(rel)
		if outerMap == nil {
			continue
		}
		// Check if this outer entry itself has parentAsins (flat structure)
		if pa := checkRelEntry(outerMap); pa != "" {
			return pa
		}
		// Descend into nested relationships array (Amazon 2022 API structure)
		inner := toSlice(outerMap["relationships"])
		for _, innerRel := range inner {
			innerMap := toMap(innerRel)
			if innerMap == nil {
				continue
			}
			if pa := checkRelEntry(innerMap); pa != "" {
				return pa
			}
		}
	}

	// Also check the variations array — for some categories Amazon puts the
	// parent ASIN in variations[].asin where variationType is "PARENT"
	variations := toSlice(raw2022["variations"])
	for _, v := range variations {
		vMap := toMap(v)
		if vMap == nil {
			continue
		}
		vType, _ := vMap["type"].(string)
		if strings.EqualFold(vType, "PARENT") {
			if pa, ok := vMap["asin"].(string); ok && pa != "" && pa != currentASIN {
				return pa
			}
		}
	}
	return ""
}

// linkVariationToParent looks up or creates the parent product in Firestore and
// then updates the child product doc with:
//   - product_type = "variation"
//   - attributes.parent_asin = parentASIN
//   - parent_id = parentProductID (the Firestore product_id of the parent)
func linkVariationToParent(ctx context.Context, client *firestore.Client, tenantID, childProductID, childASIN, parentASIN string) {
	// Find the parent's product_id via import_mappings
	parentProductID := ""
	iter := client.Collection("tenants").Doc(tenantID).
		Collection("import_mappings").
		Where("channel", "==", "amazon").
		Where("external_id", "==", parentASIN).
		Limit(1).
		Documents(ctx)
	doc, err := iter.Next()
	if err == nil {
		if pid, ok := doc.Data()["product_id"].(string); ok && pid != "" {
			parentProductID = pid
		}
	}

	if parentProductID == "" {
		// Parent not yet imported — create a stub parent product so the link exists
		parentProductID = uuid.New().String()
		parentProduct := map[string]interface{}{
			"product_id":   parentProductID,
			"tenant_id":    tenantID,
			"title":        fmt.Sprintf("Parent product (%s)", parentASIN),
			"status":       "active",
			"product_type": "variable",
			"attributes":   map[string]interface{}{"parent_asin": parentASIN},
			"identifiers":  map[string]interface{}{"asin": parentASIN},
			"assets":       []interface{}{},
			"created_at":   time.Now(),
			"updated_at":   time.Now(),
		}
		_, err := client.Collection("tenants").Doc(tenantID).
			Collection("products").Doc(parentProductID).Set(ctx, parentProduct)
		if err != nil {
			log.Printf("[Enrich] WARN: could not create stub parent product for ASIN %s: %v", parentASIN, err)
			return
		}
		// Create mapping for parent
		mappingID := uuid.New().String()
		client.Collection("tenants").Doc(tenantID).
			Collection("import_mappings").Doc(mappingID).Set(ctx, map[string]interface{}{
			"mapping_id":         mappingID,
			"tenant_id":          tenantID,
			"channel":            "amazon",
			"channel_account_id": "",
			"external_id":        parentASIN,
			"product_id":         parentProductID,
			"sync_enabled":       true,
			"created_at":         time.Now(),
			"updated_at":         time.Now(),
		})
		log.Printf("[Enrich] Created stub parent product %s for parent ASIN %s (child ASIN %s)", parentProductID, parentASIN, childASIN)
	}

	// Update the child product
	childRef := client.Collection("tenants").Doc(tenantID).
		Collection("products").Doc(childProductID)
	_, err = childRef.Update(ctx, []firestore.Update{
		{Path: "product_type", Value: "variation"},
		{Path: "parent_id", Value: parentProductID},
		{Path: "attributes.parent_asin", Value: parentASIN},
		{Path: "updated_at", Value: time.Now()},
	})
	if err != nil {
		log.Printf("[Enrich] WARN: could not link child product %s to parent %s: %v", childProductID, parentProductID, err)
		return
	}

	// Also update the parent to be product_type="variable" if it isn't already
	client.Collection("tenants").Doc(tenantID).
		Collection("products").Doc(parentProductID).
		Update(ctx, []firestore.Update{
			{Path: "product_type", Value: "variable"},
			{Path: "updated_at", Value: time.Now()},
		})

	log.Printf("[Enrich] Linked child %s (ASIN %s) → parent %s (ASIN %s)", childProductID, childASIN, parentProductID, parentASIN)
}

// ============================================================================
// CREDENTIAL HELPERS
// ============================================================================

func buildSPAPICreds(userCreds, globalKeys map[string]string) SPAPICreds {
	merged := make(map[string]string)
	for k, v := range globalKeys {
		merged[k] = v
	}
	for k, v := range userCreds {
		if v != "" {
			merged[k] = v
		}
	}
	c := SPAPICreds{
		LWAClientID:     merged["lwa_client_id"],
		LWAClientSecret: merged["lwa_client_secret"],
		RefreshToken:    merged["refresh_token"],
		AWSAccessKeyID:  merged["aws_access_key_id"],
		AWSSecretKey:    merged["aws_secret_access_key"],
		AWSSessionToken: merged["aws_session_token"],
		AWSRegion:       merged["region"],
		Endpoint:        merged["sp_endpoint"],
		MarketplaceID:   merged["marketplace_id"],
	}
	if c.AWSRegion == "" {
		c.AWSRegion = "eu-west-1"
	}
	if c.Endpoint == "" {
		switch c.AWSRegion {
		case "eu-west-1":
			c.Endpoint = "https://sellingpartnerapi-eu.amazon.com"
		case "us-west-2":
			c.Endpoint = "https://sellingpartnerapi-fe.amazon.com"
		default:
			c.Endpoint = "https://sellingpartnerapi-na.amazon.com"
		}
	}
	if c.MarketplaceID == "" {
		c.MarketplaceID = "A1F83G8C2ARO7P" // UK default
	}
	return c
}

func getDecryptedCredentials(ctx context.Context, client *firestore.Client, tenantID, credentialID string) (map[string]string, error) {
	// Try the known tenant first, then fallback to scanning
	tenantsToTry := []string{tenantID}
	// Also try other known tenants as fallback
	for _, tid := range []string{"tenant-demo", "tenant-prod", "tenant-test"} {
		if tid != tenantID {
			tenantsToTry = append(tenantsToTry, tid)
		}
	}

	// Also try scanning collection
	tenantsIter := client.Collection("tenants").Documents(ctx)
	for {
		tenantDoc, err := tenantsIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		tenantsToTry = append(tenantsToTry, tenantDoc.Ref.ID)
	}

	// Deduplicate
	seen := make(map[string]bool)
	for _, tid := range tenantsToTry {
		if seen[tid] {
			continue
		}
		seen[tid] = true

		credDoc, err := client.Collection("tenants").Doc(tid).
			Collection("marketplace_credentials").Doc(credentialID).Get(ctx)
		if err != nil {
			continue
		}
		data := credDoc.Data()
		credData, _ := data["credential_data"].(map[string]interface{})

		// Get encrypted fields list
		var encryptedFields []string
		if ef, ok := data["encrypted_fields"].([]interface{}); ok {
			for _, f := range ef {
				if s, ok := f.(string); ok {
					encryptedFields = append(encryptedFields, s)
				}
			}
		}

		result := make(map[string]string)
		for k, v := range credData {
			s, ok := v.(string)
			if !ok {
				continue
			}
			isEnc := false
			for _, ef := range encryptedFields {
				if ef == k {
					isEnc = true
					break
				}
			}
			if isEnc && encryptionKey != "" {
				key := encryptionKey
				if len(key) > 32 {
					key = key[:32]
				}
				dec, err := decryptValue(s, []byte(key))
				if err != nil {
					log.Printf("[Enrich] WARN: decrypt %s failed: %v", k, err)
					result[k] = s
				} else {
					result[k] = dec
				}
			} else {
				result[k] = s
			}
		}
		log.Printf("[Enrich] Found credential %s under tenant %s (%d fields)", credentialID, tid, len(result))
		return result, nil
	}
	return nil, fmt.Errorf("credential %s not found", credentialID)
}

func getGlobalKeys(ctx context.Context, client *firestore.Client, channel string) (map[string]string, error) {
	doc, err := client.Collection("platform_config").Doc(channel).Get(ctx)
	if err != nil {
		return map[string]string{}, nil
	}
	keys, ok := doc.Data()["keys"].(map[string]interface{})
	if !ok {
		return map[string]string{}, nil
	}
	result := make(map[string]string)
	for k, v := range keys {
		if s, ok := v.(string); ok {
			result[k] = s
		}
	}
	return result, nil
}

func decryptValue(ciphertext string, key []byte) (string, error) {
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonceSize := aesGCM.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ct := data[:nonceSize], data[nonceSize:]
	pt, err := aesGCM.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}

// ============================================================================
// HELPERS
// ============================================================================

func toMap(v interface{}) map[string]interface{} {
	if m, ok := v.(map[string]interface{}); ok {
		return m
	}
	return nil
}

func toSlice(v interface{}) []interface{} {
	if s, ok := v.([]interface{}); ok {
		return s
	}
	return nil
}

func summaryField(summaries []interface{}, field string) string {
	if len(summaries) == 0 {
		return ""
	}
	if m := toMap(summaries[0]); m != nil {
		if v, ok := m[field].(string); ok {
			return v
		}
	}
	return ""
}

func truncStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
