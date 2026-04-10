package main

import (
	"compress/gzip"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"bytes"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	cloudtasks "cloud.google.com/go/cloudtasks/apiv2"
	taskspb "cloud.google.com/go/cloudtasks/apiv2/cloudtaskspb"
	"cloud.google.com/go/firestore"
	"golang.org/x/oauth2"
	"google.golang.org/grpc/codes"
	grpcStatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ============================================================================
// IMPORT ORCHESTRATOR CLOUD FUNCTION
// ============================================================================
// Triggered by Cloud Tasks queue "import-reports".
// Flow:
//   1. Read job config from Firestore
//   2. Authenticate with Amazon SP-API using the job's credentials
//   3. Request + poll + download the merchant listings report
//   4. Stream-parse TSV rows into batches of 100
//   5. For each batch, create a Cloud Task in "import-batches" queue
//   6. Update job.total_items and status as batches are queued
//
// Payload: { "tenant_id": "...", "job_id": "...", "credential_id": "..." }
// ============================================================================

var (
	projectID   = os.Getenv("GCP_PROJECT_ID")
	region      = os.Getenv("GCP_REGION")
	batchFnURL  = os.Getenv("BATCH_FUNCTION_URL") // URL of import-batch function
)

const (
	reportPollInterval = 15 * time.Second
	reportPollTimeout  = 7 * time.Minute // Keep under Cloud Run 540s timeout
	lwaTokenEndpoint   = "https://api.amazon.com/auth/o2/token"
)

// rampBatchSize returns the batch size. Flat 20 items per batch gives the user
// quick feedback without too many Cloud Tasks tasks for large catalogues.
func rampBatchSize(batchIndex int) int {
	return 20
}

// OrchestratorPayload is the HTTP payload sent by the backend when triggering
// the orchestrator. All fields beyond the three identifiers are informational
// — the orchestrator reads authoritative job config from Firestore — but
// capturing them here prevents silent drops and makes logs meaningful.
type OrchestratorPayload struct {
	TenantID          string   `json:"tenant_id"`
	JobID             string   `json:"job_id"`
	CredentialID      string   `json:"credential_id"`
	Channel           string   `json:"channel,omitempty"`
	JobType           string   `json:"job_type,omitempty"`
	ExternalIDs       []string `json:"external_ids,omitempty"`
	FulfillmentFilter string   `json:"fulfillment_filter,omitempty"`
	StockFilter       string   `json:"stock_filter,omitempty"`
	EnrichData        bool     `json:"enrich_data,omitempty"`
}

// BatchPayload is sent to the import-batch Cloud Function
type BatchPayload struct {
	TenantID     string       `json:"tenant_id"`
	JobID        string       `json:"job_id"`
	CredentialID string       `json:"credential_id"`
	BatchIndex   int          `json:"batch_index"`
	Products     []ReportRow  `json:"products"`
}

// ReportRow represents a single row from the Amazon merchant listings report
type ReportRow struct {
	ItemName           string `json:"item_name"`
	ItemDescription    string `json:"item_description"`
	SellerSKU          string `json:"seller_sku"`
	Price              string `json:"price"`
	Quantity           string `json:"quantity"`
	OpenDate           string `json:"open_date"`
	ImageURL           string `json:"image_url"`
	ASIN1              string `json:"asin1"`
	ASIN2              string `json:"asin2"`
	ASIN3              string `json:"asin3"`
	ProductID          string `json:"product_id"`
	ProductIDType      string `json:"product_id_type"`
	ItemCondition      string `json:"item_condition"`
	FulfillmentChannel string `json:"fulfillment_channel"`
	Status             string `json:"status"`
	// Additional fields captured from TSV
	ExtraFields        map[string]string `json:"extra_fields,omitempty"`
}

// HandleOrchestratorHTTP is the Cloud Function entry point
func HandleOrchestratorHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var payload OrchestratorPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Printf("[Orchestrator] ERROR: invalid payload: %v", err)
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	log.Printf("[Orchestrator] Starting job %s for tenant %s", payload.JobID, payload.TenantID)

	if err := runOrchestrator(ctx, payload); err != nil {
		log.Printf("[Orchestrator] ERROR: job %s failed: %v", payload.JobID, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func runOrchestrator(ctx context.Context, payload OrchestratorPayload) error {
	// Initialize Firestore
	fsClient, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		return fmt.Errorf("firestore init failed: %w", err)
	}
	defer fsClient.Close()

	jobRef := fsClient.Collection("tenants").Doc(payload.TenantID).
		Collection("import_jobs").Doc(payload.JobID)

	// CHECK: Is this job already cancelled or completed?
	if cancelled, _ := isJobCancelled(ctx, jobRef); cancelled {
		log.Printf("[Orchestrator] Job %s already cancelled/completed, skipping", payload.JobID)
		return nil
	}

	// Update status: downloading report
	updateJobStatus(ctx, jobRef, "running", "Connecting to Amazon SP-API...")

	// Get credentials from Firestore
	creds, err := getCredentials(ctx, fsClient, payload.TenantID, payload.CredentialID)
	if err != nil {
		updateJobFailed(ctx, jobRef, fmt.Sprintf("Failed to get credentials: %v", err))
		return err
	}

	// Get global platform keys — both "amazon" and "amazonnew" share the same
	// Amazon infrastructure keys (LWA client ID/secret, region, endpoint).
	globalKeys, err := getGlobalKeys(ctx, fsClient, "amazon")
	if err != nil {
		updateJobFailed(ctx, jobRef, fmt.Sprintf("Failed to get global keys: %v", err))
		return err
	}

	// Merge credentials
	mergedCreds := mergeCredentials(creds, globalKeys)

	// CHECK cancellation before slow operation
	if cancelled, _ := isJobCancelled(ctx, jobRef); cancelled {
		log.Printf("[Orchestrator] Job %s cancelled before report request", payload.JobID)
		return nil
	}

	// Authenticate with Amazon
	updateJobStatus(ctx, jobRef, "running", "Requesting inventory report from Amazon — this may take 1-3 minutes...")
	token, err := getAccessToken(ctx, mergedCreds)
	if err != nil {
		updateJobFailed(ctx, jobRef, fmt.Sprintf("Amazon auth failed: %v", err))
		return err
	}

	endpoint := mergedCreds["sp_endpoint"]
	if endpoint == "" {
		endpoint = "https://sellingpartnerapi-eu.amazon.com"
	}
	marketplaceID := mergedCreds["marketplace_id"]

	// Request report
	reportID, err := requestReport(ctx, endpoint, token, marketplaceID)
	if err != nil {
		updateJobFailed(ctx, jobRef, fmt.Sprintf("Report request failed: %v", err))
		return err
	}

	// Store report ID on job for tracking
	jobRef.Update(ctx, []firestore.Update{
		{Path: "report_id", Value: reportID},
		{Path: "status_message", Value: fmt.Sprintf("Report requested (ID: %s), polling for completion...", reportID)},
	})

	// Poll for report completion
	docID, err := pollReportDone(ctx, endpoint, token, reportID, jobRef)
	if err != nil {
		updateJobFailed(ctx, jobRef, fmt.Sprintf("Report polling failed: %v", err))
		return err
	}

	// CHECK cancellation after polling (which can take minutes)
	if cancelled, _ := isJobCancelled(ctx, jobRef); cancelled {
		log.Printf("[Orchestrator] Job %s cancelled during report polling", payload.JobID)
		return nil
	}

	// Get download URL
	updateJobStatus(ctx, jobRef, "running", "Report ready, downloading...")
	downloadURL, isGzipped, err := getReportDownloadURL(ctx, endpoint, token, docID)
	if err != nil {
		updateJobFailed(ctx, jobRef, fmt.Sprintf("Failed to get download URL: %v", err))
		return err
	}

	// Download and stream-parse report into Cloud Tasks batches
	updateJobStatus(ctx, jobRef, "running", "Parsing report and queuing import batches...")
	totalQueued, uniqueASINCount, err := streamReportToBatches(ctx, downloadURL, isGzipped, payload, jobRef)
	if err != nil {
		updateJobFailed(ctx, jobRef, fmt.Sprintf("Report streaming failed: %v", err))
		return err
	}

	// Only update status if not cancelled during streaming
	if cancelled, _ := isJobCancelled(ctx, jobRef); !cancelled {
		jobRef.Update(ctx, []firestore.Update{
			{Path: "status_message", Value: fmt.Sprintf("Queued %d products in batches. Processing...", totalQueued)},
			// total_items = unique ASINs (products), not total rows (which includes FBA duplicates).
			// enrich_total_items matches so the progress bar denominator is consistent.
			{Path: "total_items", Value: int64(uniqueASINCount)},
			{Path: "enrich_total_items", Value: int64(uniqueASINCount)}, // unique ASINs only — FBA rows share the same product/enrich task
		})
	}

	log.Printf("[Orchestrator] Job %s: queued %d products in batches", payload.JobID, totalQueued)
	return nil
}

// streamReportToBatches downloads the report and creates Cloud Tasks for each batch of rows
func streamReportToBatches(ctx context.Context, downloadURL string, isGzipped bool, payload OrchestratorPayload, jobRef *firestore.DocumentRef) (int, int, error) {
	resp, err := http.Get(downloadURL)
	if err != nil {
		return 0, 0, fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	// Buffer the raw (decompressed) report bytes so we can save them to GCS
	// for diagnostics — this lets us inspect the exact file Amazon sent us.
	var reportBuf bytes.Buffer

	var reader io.Reader = resp.Body
	if isGzipped {
		gzReader, err := gzip.NewReader(resp.Body)
		if err != nil {
			return 0, 0, fmt.Errorf("gzip decode failed: %w", err)
		}
		defer gzReader.Close()
		reader = io.TeeReader(gzReader, &reportBuf)
	} else {
		reader = io.TeeReader(resp.Body, &reportBuf)
	}

	csvReader := csv.NewReader(reader)
	csvReader.Comma = '\t'
	csvReader.LazyQuotes = true
	csvReader.FieldsPerRecord = -1

	// Read header
	headers, err := csvReader.Read()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to read headers: %w", err)
	}

	headerIndex := make(map[string]int)
	for i, h := range headers {
		headerIndex[strings.TrimSpace(strings.ToLower(h))] = i
	}

	// Initialize Cloud Tasks client with background context (not request ctx which has a deadline)
	bgCtx := context.Background()
	tasksClient, err := cloudtasks.NewClient(bgCtx)
	if err != nil {
		return 0, 0, fmt.Errorf("cloud tasks client failed: %w", err)
	}
	defer tasksClient.Close()

	queuePath := fmt.Sprintf("projects/%s/locations/%s/queues/import-batches", projectID, region)

	var batch []ReportRow
	batchIndex := 0
	scheduleCounter = 0 // Reset for this orchestrator run
	totalRows := 0
	uniqueASINs := make(map[string]bool) // Track unique ASINs for enrich_total_items
	seenASINs := make(map[string]bool)   // Deduplicate by ASIN:FC within this report

	for {
		record, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("[Orchestrator] Warning: skipping malformed row %d: %v", totalRows, err)
			continue
		}

		row := parseReportRow(record, headerIndex)
		if row.ASIN1 == "" && row.SellerSKU == "" {
			// Every row in a valid Amazon merchant listings report has both asin1
			// and seller-sku populated. If both are missing it means the TSV header
			// mapping failed — log loudly so we can diagnose the parsing issue.
			log.Printf("[Orchestrator] ERROR: row %d has no ASIN or SKU — likely a header mapping failure. Raw record length: %d", totalRows, len(record))
			continue
		}

		// Deduplicate by ASIN + fulfillment channel — FBM (MFN) and FBA (AFN)
		// are separate offers on Amazon with independent pricing and SKUs, so
		// both rows must pass through to create two listings under one product.
		fc := strings.ToUpper(row.FulfillmentChannel)
		normFC := "MFN"
		if strings.Contains(fc, "AMAZON") || strings.Contains(fc, "AFN") {
			normFC = "AFN"
		}
		dedupeKey := row.ASIN1 + ":" + normFC
		if dedupeKey == ":MFN" || dedupeKey == ":AFN" {
			dedupeKey = row.SellerSKU // Fall back to SKU for non-ASIN rows
		}
		if seenASINs[dedupeKey] {
			continue
		}
		seenASINs[dedupeKey] = true
		if row.ASIN1 != "" {
			uniqueASINs[row.ASIN1] = true // track unique products for enrich total
		}

		batch = append(batch, row)
		totalRows++

		if len(batch) >= rampBatchSize(batchIndex) {
			if err := enqueueBatch(ctx, tasksClient, queuePath, payload, batch, batchIndex); err != nil {
				log.Printf("[Orchestrator] ERROR: failed to enqueue batch %d: %v", batchIndex, err)
			}
			batchIndex++

			// Update status message only — total_items is set definitively at the end
			jobRef.Update(ctx, []firestore.Update{
				{Path: "status_message", Value: fmt.Sprintf("Parsing report... %d rows queued so far", totalRows)},
			})

			batch = nil
		}
	}

	// Flush remaining batch
	if len(batch) > 0 {
		if err := enqueueBatch(ctx, tasksClient, queuePath, payload, batch, batchIndex); err != nil {
			log.Printf("[Orchestrator] ERROR: failed to enqueue final batch %d: %v", batchIndex, err)
		}
		jobRef.Update(ctx, []firestore.Update{
			// total_items set definitively after streaming completes
		})
		totalRows += 0 // already counted
	}

	log.Printf("[Orchestrator] Parsed %d rows into %d batches", totalRows, batchIndex+1)

	// Save raw report to GCS for diagnostics: gs://marketmate/debug-reports/{jobID}.tsv
	// reportBuf was populated via TeeReader as the CSV reader consumed the stream.
	reportBytes := reportBuf.Bytes() // snapshot before upload consumes the buffer
	if len(reportBytes) > 0 {
		log.Printf("[Orchestrator] Saving report (%d bytes) to GCS: debug-reports/%s.tsv", len(reportBytes), payload.JobID)
		gcsURL := fmt.Sprintf("https://storage.googleapis.com/upload/storage/v1/b/marketmate/o?uploadType=media&name=debug-reports%%2F%s.tsv", payload.JobID)
		tokenReq, _ := http.NewRequest("GET", "http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token", nil)
		tokenReq.Header.Set("Metadata-Flavor", "Google")
		tokenResp, tokenErr := http.DefaultClient.Do(tokenReq)
		if tokenErr == nil {
			defer tokenResp.Body.Close()
			var tokenData map[string]interface{}
			if json.NewDecoder(tokenResp.Body).Decode(&tokenData) == nil {
				if tok, ok := tokenData["access_token"].(string); ok {
					reportReq, _ := http.NewRequestWithContext(ctx, "POST", gcsURL, bytes.NewReader(reportBytes))
					reportReq.Header.Set("Content-Type", "text/tab-separated-values")
					reportReq.Header.Set("Authorization", "Bearer "+tok)
					reportReq.ContentLength = int64(len(reportBytes))
					if reportResp, reportErr := http.DefaultClient.Do(reportReq); reportErr != nil || reportResp.StatusCode >= 300 {
						log.Printf("[Orchestrator] WARN: failed to save report to GCS status=%d err=%v", reportResp.StatusCode, reportErr)
					} else {
						reportResp.Body.Close()
						log.Printf("[Orchestrator] Report saved: gs://marketmate/debug-reports/%s.tsv", payload.JobID)
					}
				}
			}
		}
	} else {
		log.Printf("[Orchestrator] WARN: report buffer empty — TeeReader may not have captured data")
	}

	return totalRows, len(uniqueASINs), nil
}

// scheduleCounter tracks the actual enqueue order for spreading tasks
var scheduleCounter int64

func enqueueBatch(ctx context.Context, client *cloudtasks.Client, queuePath string, payload OrchestratorPayload, batch []ReportRow, batchIndex int) error {
	batchPayload := BatchPayload{
		TenantID:     payload.TenantID,
		JobID:        payload.JobID,
		CredentialID: payload.CredentialID,
		BatchIndex:   batchIndex,
		Products:     batch,
	}

	body, err := json.Marshal(batchPayload)
	if err != nil {
		return fmt.Errorf("marshal batch: %w", err)
	}

	// Cloud Tasks max payload is ~1MB. If too large, split in half and retry both.
	if len(body) > 900000 { // 900KB threshold to be safe
		if len(batch) <= 1 {
			// Single item too large - skip it, log the error
			sku := ""
			asin := ""
			if len(batch) == 1 {
				sku = batch[0].SellerSKU
				asin = batch[0].ASIN1
			}
			log.Printf("[Orchestrator] WARNING: Single item too large to enqueue (%d bytes), skipping SKU=%s ASIN=%s", len(body), sku, asin)
			return nil
		}
		mid := len(batch) / 2
		log.Printf("[Orchestrator] Batch %d too large (%d bytes, %d items), splitting into two", batchIndex, len(body), len(batch))
		err1 := enqueueBatch(ctx, client, queuePath, payload, batch[:mid], batchIndex)
		err2 := enqueueBatch(ctx, client, queuePath, payload, batch[mid:], batchIndex)
		if err1 != nil {
			return err1
		}
		return err2
	}

	// Use a simple counter for scheduling spread — avoids overflow from recursive splits
	counter := scheduleCounter
	scheduleCounter++
	delay := time.Duration(counter*100) * time.Millisecond
	// Cap at 10 minutes — Cloud Tasks limit is 720h but no need to spread that far
	if delay > 10*time.Minute {
		delay = 10 * time.Minute
	}

	task := &taskspb.Task{
		// Deterministic name allows targeted deletion on job cancel.
		// Format: {queue}/tasks/job-{jobID}-batch-{batchIndex}
		// Cloud Tasks requires the name to include the full queue path prefix.
		Name: fmt.Sprintf("%s/tasks/job-%s-batch-%d", queuePath, payload.JobID, batchIndex),
		MessageType: &taskspb.Task_HttpRequest{
			HttpRequest: &taskspb.HttpRequest{
				HttpMethod: taskspb.HttpMethod_POST,
				Url:        batchFnURL,
				Headers:    map[string]string{"Content-Type": "application/json"},
				Body:       body,
				AuthorizationHeader: &taskspb.HttpRequest_OidcToken{
					OidcToken: &taskspb.OidcToken{
						ServiceAccountEmail: fmt.Sprintf("%s-compute@developer.gserviceaccount.com",
							os.Getenv("GCP_PROJECT_NUMBER")),
					},
				},
			},
		},
		// Spread batches slightly to avoid Firestore hot-spotting
		ScheduleTime: timestamppb.New(time.Now().Add(delay)),
	}

	// Use background context to avoid request deadline being set on the task
	_, err = client.CreateTask(context.Background(), &taskspb.CreateTaskRequest{
		Parent: queuePath,
		Task:   task,
	})
	if err != nil && grpcStatus.Code(err) == codes.AlreadyExists {
		return nil // Task already queued (orchestrator retry) — fine
	}
	return err
}

// ============================================================================
// TSV PARSING
// ============================================================================

func parseReportRow(record []string, headerIndex map[string]int) ReportRow {
	get := func(name string) string {
		if idx, ok := headerIndex[name]; ok && idx < len(record) {
			return strings.TrimSpace(record[idx])
		}
		return ""
	}

	row := ReportRow{
		ItemName:           get("item-name"),
		ItemDescription:    truncateStr(get("item-description"), 2000),
		SellerSKU:          get("seller-sku"),
		Price:              get("price"),
		Quantity:           get("quantity"),
		OpenDate:           get("open-date"),
		ImageURL:           get("image-url"),
		ASIN1:              get("asin1"),
		ASIN2:              get("asin2"),
		ASIN3:              get("asin3"),
		ProductID:          get("product-id"),
		ProductIDType:      get("product-id-type"),
		ItemCondition:      get("item-condition"),
		FulfillmentChannel: get("fulfillment-channel"),
		Status:             get("status"),
	}

	// Capture any extra fields not in the struct (limit value size)
	knownFields := map[string]bool{
		"item-name": true, "item-description": true, "seller-sku": true,
		"price": true, "quantity": true, "open-date": true, "image-url": true,
		"asin1": true, "asin2": true, "asin3": true, "product-id": true,
		"product-id-type": true, "item-condition": true, "fulfillment-channel": true, "status": true,
	}
	for name, idx := range headerIndex {
		if !knownFields[name] && idx < len(record) {
			val := strings.TrimSpace(record[idx])
			if val != "" {
				if row.ExtraFields == nil {
					row.ExtraFields = make(map[string]string)
				}
				row.ExtraFields[name] = truncateStr(val, 500)
			}
		}
	}

	return row
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// ============================================================================
// AMAZON SP-API HELPERS
// ============================================================================

func getAccessToken(ctx context.Context, creds map[string]string) (string, error) {
	data := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {creds["refresh_token"]},
		"client_id":     {creds["lwa_client_id"]},
		"client_secret": {creds["lwa_client_secret"]},
	}

	resp, err := http.PostForm(lwaTokenEndpoint, data)
	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	var tokenResp oauth2.Token
	var rawResp map[string]interface{}
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &rawResp); err != nil {
		return "", fmt.Errorf("token parse failed: %w", err)
	}

	if errMsg, ok := rawResp["error"].(string); ok {
		return "", fmt.Errorf("token error: %s: %v", errMsg, rawResp["error_description"])
	}

	accessToken, ok := rawResp["access_token"].(string)
	if !ok {
		return "", fmt.Errorf("no access_token in response")
	}
	_ = tokenResp

	return accessToken, nil
}

func spAPIRequest(ctx context.Context, method, endpoint, path, token string, queryParams url.Values) ([]byte, error) {
	fullURL := endpoint + path
	if len(queryParams) > 0 {
		fullURL += "?" + queryParams.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-amz-access-token", token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("SP-API error %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

func requestReport(ctx context.Context, endpoint, token, marketplaceID string) (string, error) {
	reqBody := fmt.Sprintf(`{"reportType":"GET_MERCHANT_LISTINGS_ALL_DATA","marketplaceIds":["%s"]}`, marketplaceID)

	fullURL := endpoint + "/reports/2021-06-30/reports"
	req, err := http.NewRequestWithContext(ctx, "POST", fullURL, strings.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("x-amz-access-token", token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("create report failed %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	json.Unmarshal(body, &result)
	reportID, ok := result["reportId"].(string)
	if !ok {
		return "", fmt.Errorf("no reportId in response: %s", string(body))
	}

	log.Printf("[Orchestrator] Report requested: %s", reportID)
	return reportID, nil
}

func pollReportDone(ctx context.Context, endpoint, token, reportID string, jobRef *firestore.DocumentRef) (string, error) {
	deadline := time.Now().Add(reportPollTimeout)
	startTime := time.Now()
	pollNum := 0

	for time.Now().Before(deadline) {
		path := fmt.Sprintf("/reports/2021-06-30/reports/%s", reportID)
		body, err := spAPIRequest(ctx, "GET", endpoint, path, token, nil)
		if err != nil {
			return "", err
		}

		var report map[string]interface{}
		json.Unmarshal(body, &report)

		status, _ := report["processingStatus"].(string)
		pollNum++
		elapsedSec := int(time.Since(startTime).Seconds())
		log.Printf("[Orchestrator] Report %s status: %s (poll #%d, %ds elapsed)", reportID, status, pollNum, elapsedSec)

		// Write live status to Firestore every poll so the UI shows progress
		statusMsg := fmt.Sprintf("Amazon is preparing your inventory report... (%ds elapsed, checking every 15s)", elapsedSec)
		if status == "IN_PROGRESS" {
			statusMsg = fmt.Sprintf("Amazon is generating your report... (%ds elapsed)", elapsedSec)
		} else if status == "IN_QUEUE" {
			statusMsg = fmt.Sprintf("Waiting in Amazon report queue... (%ds elapsed)", elapsedSec)
		}
		jobRef.Update(ctx, []firestore.Update{
			{Path: "status_message", Value: statusMsg},
			{Path: "updated_at", Value: time.Now()},
		})

		switch status {
		case "DONE":
			docID, ok := report["reportDocumentId"].(string)
			if !ok {
				return "", fmt.Errorf("no reportDocumentId in completed report")
			}
			return docID, nil
		case "CANCELLED":
			return "", fmt.Errorf("report was cancelled by Amazon")
		case "FATAL":
			return "", fmt.Errorf("report processing failed on Amazon's side")
		}

		time.Sleep(reportPollInterval)
	}

	return "", fmt.Errorf("report polling timed out after %v", reportPollTimeout)
}

func getReportDownloadURL(ctx context.Context, endpoint, token, docID string) (string, bool, error) {
	path := fmt.Sprintf("/reports/2021-06-30/documents/%s", docID)
	body, err := spAPIRequest(ctx, "GET", endpoint, path, token, nil)
	if err != nil {
		return "", false, err
	}

	var doc map[string]interface{}
	json.Unmarshal(body, &doc)

	downloadURL, ok := doc["url"].(string)
	if !ok {
		return "", false, fmt.Errorf("no url in report document")
	}

	compressionAlgo, _ := doc["compressionAlgorithm"].(string)
	return downloadURL, compressionAlgo == "GZIP", nil
}

// ============================================================================
// FIRESTORE HELPERS
// ============================================================================

func getCredentials(ctx context.Context, client *firestore.Client, tenantID, credentialID string) (map[string]string, error) {
	doc, err := client.Collection("tenants").Doc(tenantID).
		Collection("marketplace_credentials").Doc(credentialID).Get(ctx)
	if err != nil {
		return nil, err
	}

	data := doc.Data()
	credData, ok := data["credential_data"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("no credential_data field")
	}

	// Get encrypted fields list
	var encryptedFields []string
	if ef, ok := data["encrypted_fields"].([]interface{}); ok {
		for _, f := range ef {
			if s, ok := f.(string); ok {
				encryptedFields = append(encryptedFields, s)
			}
		}
	}

	encKey := os.Getenv("CREDENTIAL_ENCRYPTION_KEY")

	// AES-256 requires exactly 32 bytes. Trim or reject to match backend behaviour.
	if len(encKey) > 32 {
		encKey = encKey[:32]
	}

	result := make(map[string]string)
	for k, v := range credData {
		s, ok := v.(string)
		if !ok {
			continue
		}

		// Check if this field is encrypted
		isEncrypted := false
		for _, ef := range encryptedFields {
			if ef == k {
				isEncrypted = true
				break
			}
		}

		if isEncrypted && encKey != "" {
			decrypted, err := decryptValue(s, []byte(encKey))
			if err != nil {
				log.Printf("[Orchestrator] WARN: failed to decrypt field %s: %v", k, err)
				result[k] = s // Use raw value as fallback
			} else {
				result[k] = decrypted
			}
		} else {
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
	nonce, ciphertextData := data[:nonceSize], data[nonceSize:]
	plaintext, err := aesGCM.Open(nil, nonce, ciphertextData, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func getGlobalKeys(ctx context.Context, client *firestore.Client, channel string) (map[string]string, error) {
	doc, err := client.Collection("platform_config").Doc(channel).Get(ctx)
	if err != nil {
		return map[string]string{}, nil // Not an error if no global keys
	}

	data := doc.Data()
	keys, ok := data["keys"].(map[string]interface{})
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

func mergeCredentials(userCreds, globalKeys map[string]string) map[string]string {
	merged := make(map[string]string)
	// Global keys as base
	for k, v := range globalKeys {
		merged[k] = v
	}
	// User credentials override
	for k, v := range userCreds {
		if v != "" {
			merged[k] = v
		}
	}
	return merged
}

func isJobCancelled(ctx context.Context, jobRef *firestore.DocumentRef) (bool, error) {
	doc, err := jobRef.Get(ctx)
	if err != nil {
		// Document not found means it was deleted — treat as cancelled so we stop processing
		if grpcStatus.Code(err) == codes.NotFound {
			log.Printf("[Orchestrator] Job doc %s not found (deleted?) — treating as cancelled", jobRef.ID)
			return true, nil
		}
		return false, err
	}
	status2, _ := doc.Data()["status"].(string)
	return status2 == "cancelled" || status2 == "completed" || status2 == "failed", nil
}

func updateJobStatus(ctx context.Context, jobRef *firestore.DocumentRef, status, message string) {
	// Don't overwrite cancelled/completed/failed status, and don't recreate deleted jobs
	doc, err := jobRef.Get(ctx)
	if err != nil {
		if status2 := grpcStatus.Code(err); status2 == codes.NotFound {
			log.Printf("[Orchestrator] Skipping status update — job doc deleted")
			return
		}
	} else {
		currentStatus, _ := doc.Data()["status"].(string)
		if currentStatus == "cancelled" || currentStatus == "completed" || currentStatus == "failed" {
			log.Printf("[Orchestrator] Skipping status update — job already %s", currentStatus)
			return
		}
	}

	jobRef.Update(ctx, []firestore.Update{
		{Path: "status", Value: status},
		{Path: "status_message", Value: message},
		{Path: "updated_at", Value: time.Now()},
	})
}

func updateJobFailed(ctx context.Context, jobRef *firestore.DocumentRef, message string) {
	// Don't overwrite cancelled status, and don't recreate deleted jobs
	doc, err := jobRef.Get(ctx)
	if err != nil {
		if grpcStatus.Code(err) == codes.NotFound {
			log.Printf("[Orchestrator] Skipping failed update — job doc deleted")
			return
		}
	} else {
		currentStatus, _ := doc.Data()["status"].(string)
		if currentStatus == "cancelled" || currentStatus == "completed" || currentStatus == "failed" {
			log.Printf("[Orchestrator] Skipping failed update — job already %s", currentStatus)
			return
		}
	}

	now := time.Now()
	jobRef.Update(ctx, []firestore.Update{
		{Path: "status", Value: "failed"},
		{Path: "status_message", Value: message},
		{Path: "completed_at", Value: now},
		{Path: "updated_at", Value: now},
	})
}

// unused but required for compilation
var _ sync.Mutex
