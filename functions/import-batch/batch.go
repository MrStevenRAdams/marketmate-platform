package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	cloudtasks "cloud.google.com/go/cloudtasks/apiv2"
	taskspb "cloud.google.com/go/cloudtasks/apiv2/cloudtaskspb"
	"cloud.google.com/go/firestore"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	grpcStatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ============================================================================
// IMPORT BATCH CLOUD FUNCTION
// ============================================================================
// Triggered by Cloud Tasks queue "import-batches".
// Processes a batch of ~100 products from an Amazon report:
//   1. Check job not cancelled
//   2. For each product row: check dedup → create product → create mapping → create listing
//   3. Atomically increment job counters
//   4. If all batches done (processed == total), mark job completed
//
// Payload: { tenant_id, job_id, credential_id, batch_index, products: [...] }
// ============================================================================

var projectID = os.Getenv("GCP_PROJECT_ID")

// BatchPayload matches the orchestrator's output
type BatchPayload struct {
	TenantID     string      `json:"tenant_id"`
	JobID        string      `json:"job_id"`
	CredentialID string      `json:"credential_id"`
	BatchIndex   int         `json:"batch_index"`
	Products     []ReportRow `json:"products"`
}

type ReportRow struct {
	ItemName           string            `json:"item_name"`
	ItemDescription    string            `json:"item_description"`
	SellerSKU          string            `json:"seller_sku"`
	Price              string            `json:"price"`
	Quantity           string            `json:"quantity"`
	OpenDate           string            `json:"open_date"`
	ImageURL           string            `json:"image_url"`
	ASIN1              string            `json:"asin1"`
	ASIN2              string            `json:"asin2"`
	ASIN3              string            `json:"asin3"`
	ProductID          string            `json:"product_id"`
	ProductIDType      string            `json:"product_id_type"`
	ItemCondition      string            `json:"item_condition"`
	FulfillmentChannel string            `json:"fulfillment_channel"`
	Status             string            `json:"status"`
	ExtraFields        map[string]string `json:"extra_fields,omitempty"`
}

// HandleBatchHTTP is the Cloud Function entry point
func HandleBatchHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var payload BatchPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Printf("[Batch] ERROR: invalid payload: %v", err)
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	log.Printf("[Batch] Processing batch %d for job %s (%d products)",
		payload.BatchIndex, payload.JobID, len(payload.Products))

	if err := processBatch(ctx, payload); err != nil {
		log.Printf("[Batch] ERROR: batch %d job %s: %v", payload.BatchIndex, payload.JobID, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func processBatch(ctx context.Context, payload BatchPayload) error {
	fsClient, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		return fmt.Errorf("firestore init: %w", err)
	}
	defer fsClient.Close()

	jobRef := fsClient.Collection("tenants").Doc(payload.TenantID).
		Collection("import_jobs").Doc(payload.JobID)

	// Check if job is cancelled (or deleted) before doing work
	jobDoc, err := jobRef.Get(ctx)
	if err != nil {
		if grpcStatus.Code(err) == codes.NotFound {
			log.Printf("[Batch] Job %s not found (deleted?) — skipping batch %d", payload.JobID, payload.BatchIndex)
			return nil // return nil so Cloud Tasks doesn't retry
		}
		return fmt.Errorf("get job: %w", err)
	}
	jobData := jobDoc.Data()
	if status, _ := jobData["status"].(string); status == "cancelled" {
		log.Printf("[Batch] Job %s is cancelled, skipping batch %d", payload.JobID, payload.BatchIndex)
		return nil
	}

	// Read job-level flags needed throughout processing
	syncStock, _ := jobData["sync_stock"].(bool)

	var newCount, updateCount, failCount int
	var importedIDs []string
	var updatedIDs []string
	var errorLog []map[string]interface{}
	asinMap := make(map[string]string) // productID → ASIN

	for batchIdx, row := range payload.Products {
		// Check cancellation every 5 products so a mid-execution cancel stops quickly.
		if batchIdx > 0 && batchIdx%5 == 0 {
			jSnap, _ := jobRef.Get(ctx)
			if jSnap != nil {
				if st, _ := jSnap.Data()["status"].(string); st == "cancelled" {
					log.Printf("[Batch] Job %s cancelled mid-batch at product %d, stopping", payload.JobID, batchIdx)
					return nil
				}
			}
		}

		asin := row.ASIN1
		// NOTE: Do NOT fall back to row.ProductID — that field can be EAN/UPC/ISBN
		// depending on product-id-type, and passing those to the catalog API causes 400s.
		sku := row.SellerSKU
		if asin == "" && sku == "" {
			// This should never happen — every Amazon merchant listings row has both
			// fields. If it does, it indicates a parsing bug in the orchestrator.
			log.Printf("[Batch] ERROR: row in batch %d has no ASIN or SKU — parsing bug upstream", payload.BatchIndex)
			failCount++
			continue
		}

		// Determine fulfillment channel for this row
		rowFC := "MFN"
		if fc := strings.ToUpper(row.FulfillmentChannel); strings.Contains(fc, "AMAZON") || strings.Contains(fc, "AFN") {
			rowFC = "AFN"
		}

		// externalID for the mapping is ASIN:FC — uniquely identifies each offer.
		// Product lookup uses ASIN only so FBM and FBA rows share the same product doc.
		mappingExternalID := asin + ":" + rowFC
		if asin == "" {
			mappingExternalID = sku // non-ASIN rows fall back to SKU
		}
		externalID := asin
		if externalID == "" {
			externalID = sku
		}

		// Check for existing mapping by ASIN:FC (exact offer dedup)
		existing, err := findMapping(ctx, fsClient, payload.TenantID, "amazon", mappingExternalID)
		if err == nil && existing != nil {
			// This exact offer (ASIN + fulfillment channel) already exists — update product
			productID := existing["product_id"].(string)
			updates := buildProductUpdates(row, syncStock)
			productRef := fsClient.Collection("tenants").Doc(payload.TenantID).
				Collection("products").Doc(productID)
			if _, err := productRef.Update(ctx, updates); err != nil {
				log.Printf("[Batch] Warning: update product %s failed: %v", productID, err)
			}
			if mappingID, ok := existing["mapping_id"].(string); ok {
				fsClient.Collection("tenants").Doc(payload.TenantID).
					Collection("import_mappings").Doc(mappingID).
					Update(ctx, []firestore.Update{{Path: "updated_at", Value: time.Now()}})
			}
			updateCount++
			updatedIDs = append(updatedIDs, productID)
			asinMap[productID] = externalID
			continue
		}

		// No mapping for this ASIN:FC yet. Check if the product already exists
		// via the other channel's mapping (e.g. FBA row arriving after FBM was created).
		// If so, skip product creation and jump straight to listing+mapping creation.
		var existingProductID string
		if asin != "" {
			sibling, sibErr := findMapping(ctx, fsClient, payload.TenantID, "amazon", externalID+":MFN")
			if sibErr != nil || sibling == nil {
				sibling, sibErr = findMapping(ctx, fsClient, payload.TenantID, "amazon", externalID+":AFN")
			}
			if sibErr == nil && sibling != nil {
				if pid, ok := sibling["product_id"].(string); ok && pid != "" {
					existingProductID = pid
					log.Printf("[Batch] ASIN %s (%s): product already exists via sibling mapping, adding second listing", asin, rowFC)
				}
			}
		}

		// Variation handling: if this product has a parent ASIN, look up or create the
		// parent product so all children share the same parent_id. This prevents the
		// same variation family from being created as disconnected single products.
		parentASIN := ""
		if row.ExtraFields != nil {
			if pa, ok := row.ExtraFields["parent-asin"]; ok && pa != "" && pa != row.ASIN1 {
				parentASIN = pa
			}
		}
		// If the report didn't include a parent-asin, check the platform-wide child index.
		// This is populated by the enrich function when it processes a parent ASIN and
		// discovers its children via the SP-API relationships field.
		if parentASIN == "" && asin != "" {
			if indexDoc, err := fsClient.Collection("platform_asin_child_index").Doc(asin).Get(ctx); err == nil {
				parentASIN, _ = indexDoc.Data()["parent_asin"].(string)
				if parentASIN != "" {
					log.Printf("[Batch] Found parent ASIN %s for child %s via platform index", parentASIN, asin)
				}
			}
		}
		var parentProductID string
		if parentASIN != "" {
			// Try to find an existing mapping for the parent ASIN
			parentMapping, parentErr := findMapping(ctx, fsClient, payload.TenantID, "amazon", parentASIN)
			if parentErr == nil && parentMapping != nil {
				parentProductID, _ = parentMapping["product_id"].(string)
			} else {
				// Create a stub parent product so all children can reference it
				parentProductID = uuid.New().String()
				parentProduct := map[string]interface{}{
					"product_id":   parentProductID,
					"tenant_id":    payload.TenantID,
					"title":        row.ItemName, // Will be updated by enrichment
					"status":       "active",
					"product_type": "variable",
					"attributes":   map[string]interface{}{"parent_asin": parentASIN},
					"identifiers":  map[string]interface{}{"asin": parentASIN},
					"assets":       []interface{}{},
					"created_at":   time.Now(),
					"updated_at":   time.Now(),
				}
				parentRef := fsClient.Collection("tenants").Doc(payload.TenantID).
					Collection("products").Doc(parentProductID)
				if _, err := parentRef.Set(ctx, parentProduct); err != nil {
					log.Printf("[Batch] WARN: could not create parent product for ASIN %s: %v", parentASIN, err)
					parentProductID = "" // clear so child still gets created
				} else {
					// Create mapping for parent so next sibling finds it
					parentMappingID := uuid.New().String()
					fsClient.Collection("tenants").Doc(payload.TenantID).
						Collection("import_mappings").Doc(parentMappingID).Set(ctx, map[string]interface{}{
						"mapping_id":         parentMappingID,
						"tenant_id":          payload.TenantID,
						"channel":            "amazon",
						"channel_account_id": payload.CredentialID,
						"external_id":        parentASIN,
						"product_id":         parentProductID,
						"sync_enabled":       true,
						"created_at":         time.Now(),
						"updated_at":         time.Now(),
					})
					log.Printf("[Batch] Created parent product %s for parent ASIN %s", parentProductID, parentASIN)
					// Queue parent ASIN for enrichment too
					asinMap[parentProductID] = parentASIN
					importedIDs = append(importedIDs, parentProductID)
				}
			}
		}

		// Create or reuse product doc
		var productID string
		if existingProductID != "" {
			// Product already exists from sibling channel row (FBM/FBA pair) — reuse it
			productID = existingProductID
		} else {
			// New product — create it
			productID = uuid.New().String()
			product := buildProduct(row, payload.TenantID, productID, syncStock)
			if parentProductID != "" {
				product["parent_id"] = parentProductID
			}
			productRef := fsClient.Collection("tenants").Doc(payload.TenantID).
				Collection("products").Doc(productID)
			if _, err := productRef.Set(ctx, product); err != nil {
				failCount++
				errMsg := fmt.Sprintf("CreateProduct failed ASIN=%s SKU=%s: %v", externalID, sku, err)
				log.Printf("[Batch] ERROR: %s", errMsg)
				errorLog = append(errorLog, map[string]interface{}{
					"external_id":   externalID,
					"error_code":    "CREATE_FAILED",
					"message":       errMsg,
					"timestamp":     time.Now(),
				})
				continue
			}
		}

		// Create mapping keyed by ASIN:FC so FBM and FBA are independently addressable
		mappingID := uuid.New().String()
		mapping := map[string]interface{}{
			"mapping_id":              mappingID,
			"tenant_id":               payload.TenantID,
			"channel":                 "amazon",
			"channel_account_id":      payload.CredentialID,
			"external_id":             mappingExternalID, // ASIN:MFN or ASIN:AFN
			"asin":                    externalID,        // raw ASIN for lookups
			"fulfillment_channel":     rowFC,
			"product_id":              productID,
			"sync_enabled":            true,
			"created_at":              time.Now(),
			"updated_at":              time.Now(),
		}
		fsClient.Collection("tenants").Doc(payload.TenantID).
			Collection("import_mappings").Doc(mappingID).Set(ctx, mapping)

		// Create listing record — one per fulfillment channel offer
		price, _ := strconv.ParseFloat(row.Price, 64)
		qty, _ := strconv.Atoi(row.Quantity)
		if !syncStock {
			qty = 0
		}
		listingID := uuid.New().String()
		listing := map[string]interface{}{
			"listing_id":          listingID,
			"tenant_id":           payload.TenantID,
			"product_id":          productID,
			"channel":             "amazon",
			"channel_account_id":  payload.CredentialID,
			"state":               "imported",
			"fulfillment_channel": rowFC, // MFN or AFN
			"price":               price,
			"quantity":            qty,
			"channel_identifiers": map[string]interface{}{
				"external_listing_id": mappingExternalID, // ASIN:MFN or ASIN:AFN
				"asin":                externalID,
				"sku":                 sku,
			},
			"created_at": time.Now(),
			"updated_at": time.Now(),
		}
		fsClient.Collection("tenants").Doc(payload.TenantID).
			Collection("listings").Doc(listingID).Set(ctx, listing)

		// Only count the product as "imported" once (for the first channel row).
		// The second row (sibling channel) adds a listing but not a new product.
		if existingProductID == "" {
			newCount++
			importedIDs = append(importedIDs, productID)
			asinMap[productID] = externalID
		} else {
			log.Printf("[Batch] ASIN %s: added %s listing %s to existing product %s", externalID, rowFC, listingID, productID)
		}
	}

	// Atomically update job counters
	updates := []firestore.Update{
		{Path: "processed_items", Value: firestore.Increment(len(payload.Products))},
		{Path: "successful_items", Value: firestore.Increment(newCount + updateCount)},
		{Path: "failed_items", Value: firestore.Increment(failCount)},
		{Path: "updated_at", Value: time.Now()},
	}

	// NOTE: We only store counters on the job doc, NOT arrays of product IDs.
	// Storing 57K+ IDs in arrays hits Firestore's index entry limit.
	// Product IDs are tracked via import_mappings collection instead.
	if len(errorLog) > 0 {
		errs := make([]interface{}, len(errorLog))
		for i, e := range errorLog {
			errs[i] = e
		}
		// Only keep last 50 errors to avoid bloating the doc
		if len(errs) > 50 {
			errs = errs[:50]
		}
		updates = append(updates, firestore.Update{Path: "error_log", Value: firestore.ArrayUnion(errs...)})
	}

	if _, err := jobRef.Update(ctx, updates); err != nil {
		log.Printf("[Batch] ERROR: failed to update job counters: %v", err)
	}

	// Queue enrichment BEFORE completion check — enrichment must be queued first
	// so the enrich function can mark the job complete when all items are done.
	enrichData, _ := jobData["enrich_data"].(bool)
	allProductIDs := append(importedIDs, updatedIDs...)
	if enrichData && len(allProductIDs) > 0 {
		queueEnrichmentForBatch(ctx, fsClient, payload, allProductIDs, asinMap)
	}

	// Only mark completed here if enrichment is disabled.
	// If enrichment is enabled, the enrich function marks completion.
	if !enrichData {
		checkAndCompleteJob(ctx, jobRef)
	}

	log.Printf("[Batch] Batch %d done: %d new, %d updated, %d failed",
		payload.BatchIndex, newCount, updateCount, failCount)
	return nil
}

func checkAndCompleteJob(ctx context.Context, jobRef *firestore.DocumentRef) {
	doc, err := jobRef.Get(ctx)
	if err != nil {
		return
	}
	data := doc.Data()

	totalItems, _ := toInt(data["total_items"])
	processedItems, _ := toInt(data["processed_items"])
	status, _ := data["status"].(string)

	if status == "cancelled" {
		return
	}

	if processedItems >= totalItems && totalItems > 0 {
		successfulItems, _ := toInt(data["successful_items"])
		failedItems, _ := toInt(data["failed_items"])

		now := time.Now()
		jobRef.Update(ctx, []firestore.Update{
			{Path: "status", Value: "completed"},
			{Path: "status_message", Value: fmt.Sprintf("Complete: %d imported, %d failed",
				successfulItems, failedItems)},
			{Path: "completed_at", Value: now},
			{Path: "updated_at", Value: now},
		})
		log.Printf("[Batch] Job marked COMPLETED: %d/%d processed", processedItems, totalItems)
	}
}

func toInt(v interface{}) (int, bool) {
	switch val := v.(type) {
	case int64:
		return int(val), true
	case float64:
		return int(val), true
	case int:
		return val, true
	default:
		return 0, false
	}
}

// ============================================================================
// PRODUCT BUILDING
// ============================================================================

func buildProduct(row ReportRow, tenantID, productID string, syncStock bool) map[string]interface{} {
	price, _ := strconv.ParseFloat(row.Price, 64)
	qty, _ := strconv.Atoi(row.Quantity)
	if !syncStock {
		qty = 0
	}

	fulfillment := "MFN"
	fc := strings.ToUpper(row.FulfillmentChannel)
	if strings.Contains(fc, "AMAZON") || strings.Contains(fc, "AFN") {
		fulfillment = "FBA"
	}

	// Detect variation: the Amazon merchant listing report includes a "parent-asin"
	// column (captured in ExtraFields). If present and different from this row's
	// own ASIN, this is a child variation — mark it so the UI and enrich function
	// can group it under the parent rather than treat it as a standalone product.
	parentASIN := ""
	if row.ExtraFields != nil {
		if pa, ok := row.ExtraFields["parent-asin"]; ok && pa != "" && pa != row.ASIN1 {
			parentASIN = pa
		}
	}
	productType := "simple"
	if parentASIN != "" {
		productType = "variation"
	}

	product := map[string]interface{}{
		"product_id":   productID,
		"tenant_id":    tenantID,
		"title":        row.ItemName,
		"description":  row.ItemDescription,
		"status":       "active",
		"product_type": productType,
		"attributes": map[string]interface{}{
			"source_sku":            row.SellerSKU,
			"source_price":          price,
			"source_currency":       "GBP", // Will be overridden by report data if available
			"source_quantity":       qty,
			"fulfillment_channel":   fulfillment,
			"item_condition":        row.ItemCondition,
			"amazon_status":         row.Status,
		},
		"identifiers": map[string]interface{}{},
		"assets":      []interface{}{},
		"created_at":  time.Now(),
		"updated_at":  time.Now(),
	}

	// Identifiers
	if row.ASIN1 != "" {
		product["identifiers"].(map[string]interface{})["asin"] = row.ASIN1
	}
	if row.ProductIDType == "4" && row.ProductID != "" { // EAN
		product["identifiers"].(map[string]interface{})["ean"] = row.ProductID
	}
	if row.ProductIDType == "1" && row.ProductID != "" { // UPC/GTIN
		product["identifiers"].(map[string]interface{})["upc"] = row.ProductID
	}

	// Only set parent_asin when this is actually a variation child
	if parentASIN != "" {
		product["attributes"].(map[string]interface{})["parent_asin"] = parentASIN
	}

	// Image
	if row.ImageURL != "" {
		product["assets"] = []interface{}{
			map[string]interface{}{
				"asset_id":   uuid.New().String(),
				"url":        row.ImageURL,
				"role":       "primary_image",
				"sort_order": 0,
			},
		}
	}

	return product
}

func buildProductUpdates(row ReportRow, syncStock bool) []firestore.Update {
	price, _ := strconv.ParseFloat(row.Price, 64)
	qty, _ := strconv.Atoi(row.Quantity)
	if !syncStock {
		qty = 0
	}

	updates := []firestore.Update{
		{Path: "title", Value: row.ItemName},
		{Path: "attributes.source_price", Value: price},
		{Path: "attributes.source_quantity", Value: qty},
		{Path: "attributes.amazon_status", Value: row.Status},
		{Path: "updated_at", Value: time.Now()},
	}

	if row.ItemDescription != "" {
		updates = append(updates, firestore.Update{Path: "description", Value: row.ItemDescription})
	}
	if pa, ok := row.ExtraFields["parent-asin"]; ok && pa != "" && pa != row.ASIN1 {
		updates = append(updates, firestore.Update{Path: "attributes.parent_asin", Value: pa})
		updates = append(updates, firestore.Update{Path: "product_type", Value: "variation"})
	}

	return updates
}

func findMapping(ctx context.Context, client *firestore.Client, tenantID, channel, externalID string) (map[string]interface{}, error) {
	iter := client.Collection("tenants").Doc(tenantID).
		Collection("import_mappings").
		Where("channel", "==", channel).
		Where("external_id", "==", externalID).
		Limit(1).
		Documents(ctx)

	doc, err := iter.Next()
	if err != nil {
		return nil, err
	}
	return doc.Data(), nil
}

// ============================================================================
// ENRICHMENT QUEUING (Credential Pooling)
// ============================================================================

type EnrichItem struct {
	ProductID string `json:"product_id"`
	ASIN      string `json:"asin"`
}

type EnrichPayload struct {
	TenantID     string       `json:"tenant_id"`
	JobID        string       `json:"job_id"`
	CredentialID string       `json:"credential_id"`
	Items        []EnrichItem `json:"items"`
}

func queueEnrichmentForBatch(ctx context.Context, fsClient *firestore.Client, payload BatchPayload, productIDs []string, asinMap map[string]string) {
	enrichFnURL := os.Getenv("ENRICH_FUNCTION_URL")
	if enrichFnURL == "" {
		log.Printf("[Batch] ENRICH_FUNCTION_URL not set, skipping enrichment")
		return
	}

	projectID := os.Getenv("GCP_PROJECT_ID")
	region := os.Getenv("GCP_REGION")
	projectNumber := os.Getenv("GCP_PROJECT_NUMBER")

	// Get all active Amazon credentials for pooling, filtered to same marketplace
	// as the job's own credential so we don't mix UK/US/EU accounts.
	marketplaceID := getCredentialMarketplaceID(ctx, fsClient, payload.TenantID, payload.CredentialID)
	pooledCreds := getAllAmazonCredentialsByMarketplace(ctx, fsClient, marketplaceID)
	if len(pooledCreds) == 0 {
		log.Printf("[Batch] No active Amazon credentials found for marketplace %s, using job credential", marketplaceID)
		pooledCreds = []string{payload.CredentialID}
	}

	// Build enrich items — deduplicate by product_id so FBM+FBA rows for the
	// same ASIN don't generate two enrich tasks for the same product doc.
	var items []EnrichItem
	seenProductIDs := make(map[string]bool)
	for _, pid := range productIDs {
		if seenProductIDs[pid] {
			continue
		}
		seenProductIDs[pid] = true
		asin := asinMap[pid]
		if asin != "" {
			items = append(items, EnrichItem{ProductID: pid, ASIN: asin})
		}
	}

	if len(items) == 0 {
		return
	}

	// Create Cloud Tasks client with background context (avoid request deadline propagation)
	tasksClient, err := cloudtasks.NewClient(context.Background())
	if err != nil {
		log.Printf("[Batch] ERROR: cloud tasks client for enrichment: %v", err)
		return
	}
	defer tasksClient.Close()

	queuePath := fmt.Sprintf("projects/%s/locations/%s/queues/enrich-products", projectID, region)
	saEmail := fmt.Sprintf("%s-compute@developer.gserviceaccount.com", projectNumber)

	// Distribute items across pooled credentials (round-robin)
	// Each enrichment task gets ~10 ASINs and one credential
	// With 2 API calls per ASIN at ~0.55s delay each, one task takes ~11-12s
	// Schedule tasks so the same credential isn't hit concurrently
	enrichBatchSize := 10 // Reduced from 50: each task ~6s API + Firestore, safely under Cloud Run 300s timeout
	credCount := len(pooledCreds)
	// How many tasks per credential round
	totalTasks := (len(items) + enrichBatchSize - 1) / enrichBatchSize
	// Each enrich task processes 10 items at 600ms each = ~6s of API calls.
	// Space tasks 10s apart per credential.
	perTaskDuration := 10 * time.Second

	for i := 0; i < len(items); i += enrichBatchSize {
		end := i + enrichBatchSize
		if end > len(items) {
			end = len(items)
		}
		batchItems := items[i:end]

		taskIndex := i / enrichBatchSize
		// Round-robin credential selection
		credID := pooledCreds[taskIndex%credCount]

		// Calculate schedule delay: tasks for the same credential are spaced out
		// Task 0 → cred 0 at T+0, task 1 → cred 1 at T+0, ...
		// Task N → cred 0 at T+12s, task N+1 → cred 1 at T+12s, ...
		credRound := taskIndex / credCount
		scheduleDelay := time.Duration(credRound) * perTaskDuration

		enrichPayload := EnrichPayload{
			TenantID:     payload.TenantID,
			JobID:        payload.JobID,
			CredentialID: credID,
			Items:        batchItems,
		}

		body, _ := json.Marshal(enrichPayload)

		task := &taskspb.Task{
			// Deterministic name: job-{jobID}-enrich-{batchIndex}-{taskIndex}
			// Includes batchIndex so tasks from different batches don't collide.
			Name:             fmt.Sprintf("%s/tasks/job-%s-enrich-%d-%d", queuePath, payload.JobID, payload.BatchIndex, taskIndex),
			DispatchDeadline: durationpb.New(1500 * time.Second), // 25 min — matches Cloud Run timeout (1800s) with headroom for rate-limit retries
			MessageType: &taskspb.Task_HttpRequest{
				HttpRequest: &taskspb.HttpRequest{
					HttpMethod: taskspb.HttpMethod_POST,
					Url:        enrichFnURL,
					Headers:    map[string]string{"Content-Type": "application/json"},
					Body:       body,
					AuthorizationHeader: &taskspb.HttpRequest_OidcToken{
						OidcToken: &taskspb.OidcToken{
							ServiceAccountEmail: saEmail,
						},
					},
				},
			},
			ScheduleTime: timestamppb.New(time.Now().Add(scheduleDelay)),
		}

		if _, err := tasksClient.CreateTask(context.Background(), &taskspb.CreateTaskRequest{
			Parent: queuePath,
			Task:   task,
		}); err != nil {
			if grpcStatus.Code(err) == codes.AlreadyExists {
				// Task already queued (batch retry) — this is fine, skip silently
			} else {
				log.Printf("[Batch] ERROR: queue enrich task: %v", err)
			}
		}
	}

	// enrich_total_items is set once by the orchestrator (not here) to avoid the
	// race condition where early enrich tasks complete before all batch tasks have
	// incremented the total, causing premature job completion.

	log.Printf("[Batch] Queued enrichment for %d items in %d tasks across %d credentials (est. %v total)",
		len(items), totalTasks, credCount, time.Duration(((totalTasks/credCount)+1))*perTaskDuration)
}

// getCredentialMarketplaceID looks up the marketplace_id field from a credential doc.
func getCredentialMarketplaceID(ctx context.Context, client *firestore.Client, tenantID, credentialID string) string {
	doc, err := client.Collection("tenants").Doc(tenantID).
		Collection("marketplace_credentials").Doc(credentialID).Get(ctx)
	if err != nil {
		return ""
	}
	data := doc.Data()
	// Try credential_data.marketplace_id first, then top-level marketplace_id
	if cd, ok := data["credential_data"].(map[string]interface{}); ok {
		if mid, ok := cd["marketplace_id"].(string); ok && mid != "" {
			return mid
		}
	}
	if mid, ok := data["marketplace_id"].(string); ok {
		return mid
	}
	return ""
}

// getAllAmazonCredentialsByMarketplace returns all active Amazon credential IDs
// across all tenants that match the given marketplace ID.
// Searches both "amazon" and "amazonnew" channels so OAuth credentials are
// included in the enrichment pool alongside manually-configured ones.
// Cross-tenant pooling is intentional — it parallelises enrichment across
// all available credentials, each respecting their own SP-API rate limits.
func getAllAmazonCredentialsByMarketplace(ctx context.Context, client *firestore.Client, marketplaceID string) []string {
	var credIDs []string
	seen := make(map[string]bool)

	tenantsIter := client.Collection("tenants").Documents(ctx)
	for {
		tenantDoc, err := tenantsIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		tid := tenantDoc.Ref.ID

		for _, channel := range []string{"amazon", "amazonnew"} {
			credIter := client.Collection("tenants").Doc(tid).
				Collection("marketplace_credentials").
				Where("channel", "==", channel).
				Where("active", "==", true).
				Documents(ctx)

			for {
				credDoc, err := credIter.Next()
				if err == iterator.Done {
					break
				}
				if err != nil {
					break
				}
				credID := credDoc.Ref.ID
				if seen[credID] {
					continue
				}

				// Filter by marketplace if we have one
				if marketplaceID != "" {
					data := credDoc.Data()
					var credMarketplace string
					if cd, ok := data["credential_data"].(map[string]interface{}); ok {
						credMarketplace, _ = cd["marketplace_id"].(string)
					}
					if credMarketplace == "" {
						credMarketplace, _ = data["marketplace_id"].(string)
					}
					if credMarketplace != marketplaceID {
						continue
					}
				}

				seen[credID] = true
				credIDs = append(credIDs, credID)
			}
		}
	}

	return credIDs
}
