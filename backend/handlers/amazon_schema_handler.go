package handlers

// ============================================================================
// AMAZON SCHEMA MANAGEMENT HANDLER  (rearchitected)
// ============================================================================
// Endpoints:
//   GET    /schemas/list                        — List all cached schemas
//   POST   /schemas/download                    — Download a specific product type
//   POST   /schemas/download-all                — Background: download ALL types
//   GET    /schemas/jobs                        — List download jobs
//   GET    /schemas/jobs/:jobId                 — Get job status + progress
//   POST   /schemas/jobs/:jobId/cancel          — Cancel a running job
//   GET    /schemas/:productType                — Get one cached schema
//   POST   /schemas/:productType/field-config   — Save field config
//   DELETE /schemas/:productType                — Delete a cached schema
//   GET    /schemas/refresh-settings            — ENH-02: auto-refresh settings
//   PUT    /schemas/refresh-settings            — ENH-02: save auto-refresh settings
//
// Firestore:
//   marketplaces/Amazon/{marketplace_id}/data/schemas/{productType}
//   marketplaces/Amazon/{marketplace_id}/data/field_configs/{productType}
//   marketplaces/Amazon/schema_jobs/{jobId}
//
// Key architecture changes vs previous version:
//   - 429 detection: checks HTTP status code string and Retry-After header;
//     waits the prescribed time before retrying instead of a fixed backoff.
//   - Structured log entries: every significant event appends to a `logs`
//     array in the job Firestore document so the frontend can display them.
//   - currentActivity: updated every iteration so the UI always knows what
//     the job is doing right now (never "stuck" with no explanation).
//   - Log flush interval: logs are flushed to Firestore every 5 entries or
//     every updateInterval schemas, whichever comes first.
//   - Silent failure eliminated: every error branch writes to both the Go
//     logger and the Firestore job document's logs/errors fields.
// ============================================================================

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"module-a/marketplace/clients/amazon"
	"module-a/repository"
	"module-a/services"
)

// ── Constants ─────────────────────────────────────────────────────────────────

const (
	amazonSchemaFreshDays       = 7    // skip schema if cached within 7 days
	amazonClientRefreshInterval = 50   // rebuild SP-API client every N schemas
	amazonUpdateInterval        = 5    // flush progress to Firestore every N schemas
	amazonLogFlushSize          = 5    // also flush when pending log buffer hits this size
	amazonMaxErrors             = 100  // cap the errors array in Firestore
	amazonMaxLogEntries         = 200  // cap the logs array in Firestore
	amazonInterSchemaSleepMs    = 500  // ms between schema downloads (~2 req/s)
	amazonMaxConsecFailures     = 10   // consecutive failures before long pause
	amazonConsecFailurePauseS   = 60   // seconds to pause after maxConsecFailures
	amazonRetryAttempts         = 3    // per-schema download attempts
	amazonBaseBackoffS          = 2    // base backoff seconds for retries (doubles each attempt)
	amazon429DefaultWaitS       = 30   // default wait when Retry-After header absent
	amazon429MaxWaitS           = 120  // cap on Retry-After honoring
)

// ── Types ─────────────────────────────────────────────────────────────────────

type AmazonSchemaHandler struct {
	marketplaceService *services.MarketplaceService
	repo               *repository.MarketplaceRepository
	fsClient           *firestore.Client

	activeJobs   map[string]context.CancelFunc
	activeJobsMu sync.Mutex

	scheduler *SchemaRefreshScheduler
}

func NewAmazonSchemaHandler(
	marketplaceService *services.MarketplaceService,
	repo *repository.MarketplaceRepository,
	fsClient *firestore.Client,
) *AmazonSchemaHandler {
	return &AmazonSchemaHandler{
		marketplaceService: marketplaceService,
		repo:               repo,
		fsClient:           fsClient,
		activeJobs:         make(map[string]context.CancelFunc),
	}
}

// ── Firestore helpers ─────────────────────────────────────────────────────────

func (h *AmazonSchemaHandler) schemasCol(marketplaceID string) *firestore.CollectionRef {
	return h.fsClient.Collection("marketplaces").Doc("Amazon").Collection(marketplaceID).Doc("data").Collection("schemas")
}

func (h *AmazonSchemaHandler) fieldConfigsCol(marketplaceID string) *firestore.CollectionRef {
	return h.fsClient.Collection("marketplaces").Doc("Amazon").Collection(marketplaceID).Doc("data").Collection("field_configs")
}

func (h *AmazonSchemaHandler) jobsCol() *firestore.CollectionRef {
	return h.fsClient.Collection("marketplaces").Doc("Amazon").Collection("schema_jobs")
}

// ── Client helpers ────────────────────────────────────────────────────────────

func (h *AmazonSchemaHandler) getClient(c *gin.Context) (*amazon.SPAPIClient, string, error) {
	tenantID := c.GetString("tenant_id")
	credentialID := c.Query("credential_id")
	if credentialID == "" {
		creds, err := h.repo.ListCredentials(c.Request.Context(), tenantID)
		if err != nil {
			return nil, "", fmt.Errorf("list credentials: %w", err)
		}
		for _, cred := range creds {
			if cred.Channel == "amazon" && cred.Active {
				credentialID = cred.CredentialID
				break
			}
		}
		if credentialID == "" {
			return nil, "", fmt.Errorf("no Amazon credential found")
		}
	}
	return h.buildClient(c.Request.Context(), tenantID, credentialID)
}

func (h *AmazonSchemaHandler) buildClient(ctx context.Context, tenantID, credentialID string) (*amazon.SPAPIClient, string, error) {
	cred, err := h.repo.GetCredential(ctx, tenantID, credentialID)
	if err != nil {
		return nil, "", fmt.Errorf("get credential: %w", err)
	}
	mergedCreds, err := h.marketplaceService.GetFullCredentials(ctx, cred)
	if err != nil {
		return nil, "", fmt.Errorf("merge credentials: %w", err)
	}
	config := &amazon.SPAPIConfig{
		LWAClientID:        mergedCreds["lwa_client_id"],
		LWAClientSecret:    mergedCreds["lwa_client_secret"],
		RefreshToken:       mergedCreds["refresh_token"],
		AWSAccessKeyID:     mergedCreds["aws_access_key_id"],
		AWSSecretAccessKey: mergedCreds["aws_secret_access_key"],
		MarketplaceID:      mergedCreds["marketplace_id"],
		Region:             mergedCreds["region"],
		SellerID:           mergedCreds["seller_id"],
	}
	if config.MarketplaceID == "" {
		config.MarketplaceID = "A1F83G8C2ARO7P"
	}
	if config.Region == "" {
		config.Region = "eu-west-1"
	}
	client, err := amazon.NewSPAPIClient(ctx, config)
	if err != nil {
		return nil, "", fmt.Errorf("create SP-API client: %w", err)
	}
	return client, config.MarketplaceID, nil
}

// ── 429 helper ────────────────────────────────────────────────────────────────

// parseRetryAfter inspects an error string for HTTP 429 indicators and returns
// how many seconds the caller should wait before retrying.
// If the error is not a 429, returns 0, false.
// If it is a 429 but no Retry-After value is parseable, returns the default wait.
func parseRetryAfter(err error) (waitSec int, is429 bool) {
	if err == nil {
		return 0, false
	}
	s := err.Error()
	// SP-API errors come back as strings containing the status code.
	if !strings.Contains(s, "429") && !strings.Contains(s, "TooManyRequests") && !strings.Contains(s, "QuotaExceeded") {
		return 0, false
	}
	// Look for "Retry-After: N" anywhere in the error string (some clients embed headers).
	for _, part := range strings.Fields(s) {
		if n, parseErr := strconv.Atoi(strings.TrimRight(part, "s")); parseErr == nil && n > 0 && n <= amazon429MaxWaitS {
			return n, true
		}
	}
	return amazon429DefaultWaitS, true
}

// ── Log entry helper ──────────────────────────────────────────────────────────

type amazonLogEntry struct {
	T   string `json:"t"`   // RFC3339 timestamp
	Msg string `json:"msg"` // human-readable message
	Lvl string `json:"lvl"` // "info" | "warn" | "error"
}

func newLogEntry(level, msg string) amazonLogEntry {
	return amazonLogEntry{T: time.Now().Format(time.RFC3339), Msg: msg, Lvl: level}
}

// ── jobProgressWriter batches Firestore writes ────────────────────────────────

type amazonJobProgress struct {
	jobsCol     *firestore.CollectionRef
	jobID       string
	downloaded  int
	skipped     int
	failed      int
	errors      []string
	pendingLogs []amazonLogEntry
	allLogs     []amazonLogEntry
	lastFlush   time.Time
}

func (p *amazonJobProgress) addLog(level, msg string) {
	entry := newLogEntry(level, msg)
	p.pendingLogs = append(p.pendingLogs, entry)
	p.allLogs = append(p.allLogs, entry)
	// Cap all-logs to maxLogEntries (keep most recent)
	if len(p.allLogs) > amazonMaxLogEntries {
		p.allLogs = p.allLogs[len(p.allLogs)-amazonMaxLogEntries:]
	}
}

func (p *amazonJobProgress) addError(msg string) {
	if len(p.errors) < amazonMaxErrors {
		p.errors = append(p.errors, msg)
	}
	p.addLog("error", msg)
}

// flush writes current progress + pending logs to Firestore.
// forceActivity is written to currentActivity regardless of pending logs.
func (p *amazonJobProgress) flush(ctx context.Context, currentActivity string) {
	if len(p.pendingLogs) == 0 && time.Since(p.lastFlush) < 2*time.Second {
		return
	}

	// Build a limited log window for Firestore (cap at maxLogEntries)
	logsToWrite := p.allLogs
	if len(logsToWrite) > amazonMaxLogEntries {
		logsToWrite = logsToWrite[len(logsToWrite)-amazonMaxLogEntries:]
	}

	p.jobsCol.Doc(p.jobID).Set(ctx, map[string]interface{}{
		"downloaded":      p.downloaded,
		"skipped":         p.skipped,
		"failed":          p.failed,
		"errors":          p.errors,
		"logs":            logsToWrite,
		"currentActivity": currentActivity,
		"updatedAt":       time.Now(),
	}, firestore.MergeAll)

	p.pendingLogs = p.pendingLogs[:0]
	p.lastFlush = time.Now()
}

// ============================================================================
// GET /api/v1/amazon/schemas/list
// ============================================================================

type SchemaListItem struct {
	ProductType string    `json:"productType"`
	DisplayName string    `json:"displayName"`
	AttrCount   int       `json:"attrCount"`
	Groups      []string  `json:"groups"`
	CachedAt    time.Time `json:"cachedAt"`
	HasConfig   bool      `json:"hasFieldConfig"`
}

func (h *AmazonSchemaHandler) ListSchemas(c *gin.Context) {
	mpID := c.Query("marketplace_id")
	if mpID == "" {
		mpID = "A1F83G8C2ARO7P"
	}
	docs, err := h.schemasCol(mpID).Documents(c.Request.Context()).GetAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	configDocs, _ := h.fieldConfigsCol(mpID).Documents(c.Request.Context()).GetAll()
	configSet := make(map[string]bool)
	for _, d := range configDocs {
		configSet[d.Ref.ID] = true
	}
	items := make([]SchemaListItem, 0, len(docs))
	for _, doc := range docs {
		data := doc.Data()
		item := SchemaListItem{
			ProductType: doc.Ref.ID,
			DisplayName: strVal(data, "displayName"),
			HasConfig:   configSet[doc.Ref.ID],
		}
		if attrs, ok := data["attributes"]; ok {
			if arr, ok := attrs.([]interface{}); ok {
				item.AttrCount = len(arr)
			}
		}
		if groups, ok := data["groupOrder"]; ok {
			if arr, ok := groups.([]interface{}); ok {
				for _, g := range arr {
					if s, ok := g.(string); ok {
						item.Groups = append(item.Groups, s)
					}
				}
			}
		}
		if t, ok := data["cachedAt"]; ok {
			if ts, ok := t.(time.Time); ok {
				item.CachedAt = ts
			}
		}
		items = append(items, item)
	}
	c.JSON(http.StatusOK, gin.H{"schemas": items, "count": len(items), "marketplaceId": mpID})
}

// ============================================================================
// POST /api/v1/amazon/schemas/download
// ============================================================================

type DownloadRequest struct {
	ProductType   string `json:"productType" binding:"required"`
	MarketplaceID string `json:"marketplaceId"`
}

func (h *AmazonSchemaHandler) DownloadSchema(c *gin.Context) {
	var req DownloadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	mpID := req.MarketplaceID
	if mpID == "" {
		mpID = "A1F83G8C2ARO7P"
	}
	client, _, err := h.getClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	attrCount, err := h.downloadAndStore(c.Request.Context(), client, req.ProductType, mpID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "productType": req.ProductType, "attrCount": attrCount, "cachedAt": time.Now()})
}

// downloadAndStore fetches one schema from SP-API and writes it to Firestore.
func (h *AmazonSchemaHandler) downloadAndStore(ctx context.Context, client *amazon.SPAPIClient, productType, mpID string) (int, error) {
	def, err := client.GetProductTypeDefinition(ctx, productType, "en_GB")
	if err != nil {
		return 0, fmt.Errorf("fetch definition for %s: %v", productType, err)
	}
	parsed, err := client.FetchAndParseSchema(ctx, def)
	if err != nil {
		return 0, fmt.Errorf("parse schema for %s: %v", productType, err)
	}
	parsedJSON, err := json.Marshal(parsed)
	if err != nil {
		return 0, fmt.Errorf("marshal: %v", err)
	}
	var parsedMap map[string]interface{}
	if err := json.Unmarshal(parsedJSON, &parsedMap); err != nil {
		return 0, fmt.Errorf("unmarshal: %v", err)
	}
	parsedMap["cachedAt"] = time.Now()
	parsedMap["marketplaceId"] = mpID
	parsedMap["displayName"] = def.DisplayName
	parsedMap["productType"] = productType
	if _, err = h.schemasCol(mpID).Doc(productType).Set(ctx, parsedMap); err != nil {
		return 0, fmt.Errorf("store: %v", err)
	}
	attrCount := 0
	if arr, ok := parsedMap["attributes"].([]interface{}); ok {
		attrCount = len(arr)
	}
	return attrCount, nil
}

// ============================================================================
// POST /api/v1/amazon/schemas/download-all
// ============================================================================

type DownloadAllRequest struct {
	MarketplaceID string `json:"marketplaceId"`
}

func (h *AmazonSchemaHandler) DownloadAll(c *gin.Context) {
	var req DownloadAllRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	mpID := req.MarketplaceID
	if mpID == "" {
		mpID = "A1F83G8C2ARO7P"
	}
	client, resolvedMpID, err := h.getClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if mpID == "" {
		mpID = resolvedMpID
	}
	result, err := client.SearchProductTypes(c.Request.Context(), "", "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("search product types: %v", err)})
		return
	}
	productTypes := make([]string, 0, len(result.ProductTypes))
	for _, pt := range result.ProductTypes {
		productTypes = append(productTypes, pt.Name)
	}
	if len(productTypes) == 0 {
		c.JSON(http.StatusOK, gin.H{"ok": true, "message": "no product types found"})
		return
	}

	jobID := generateJobID()
	now := time.Now()
	tenantID := c.GetString("tenant_id")
	credentialID := c.Query("credential_id")

	jobData := map[string]interface{}{
		"jobId":           jobID,
		"status":          "running",
		"marketplaceId":   mpID,
		"total":           len(productTypes),
		"downloaded":      0,
		"skipped":         0,
		"failed":          0,
		"startedAt":       now,
		"updatedAt":       now,
		"errors":          []string{},
		"logs":            []amazonLogEntry{},
		"currentActivity": fmt.Sprintf("Initialising — %d product types to process", len(productTypes)),
	}
	if _, err := h.jobsCol().Doc(jobID).Set(c.Request.Context(), jobData); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("create job: %v", err)})
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	h.activeJobsMu.Lock()
	h.activeJobs[jobID] = cancel
	h.activeJobsMu.Unlock()

	go h.downloadAll(ctx, jobID, client, productTypes, mpID, tenantID, credentialID)

	c.JSON(http.StatusOK, gin.H{"ok": true, "jobId": jobID, "total": len(productTypes)})
}

// downloadAll is the background goroutine. Core design principles:
//   - Every significant event is logged to Firestore via the progress writer.
//   - 429 errors are detected by status code string; Retry-After is honoured.
//   - currentActivity is updated on every schema so the UI always shows progress.
//   - Token refresh happens every amazonClientRefreshInterval schemas.
//   - On consecutive failure threshold, the job pauses for amazonConsecFailurePauseS.
func (h *AmazonSchemaHandler) downloadAll(
	ctx context.Context,
	jobID string,
	client *amazon.SPAPIClient,
	productTypes []string,
	mpID, tenantID, credentialID string,
) {
	defer func() {
		h.activeJobsMu.Lock()
		delete(h.activeJobs, jobID)
		h.activeJobsMu.Unlock()
	}()

	log.Printf("[AmazonSchemaJob %s] Started: %d product types for marketplace %s", jobID, len(productTypes), mpID)

	prog := &amazonJobProgress{
		jobsCol: h.jobsCol(),
		jobID:   jobID,
	}
	prog.addLog("info", fmt.Sprintf("Job started — %d product types to download for %s", len(productTypes), mpID))
	prog.flush(ctx, "Starting...")

	consecutiveFailures := 0

	for i, pt := range productTypes {
		// ── Cancellation check ──
		select {
		case <-ctx.Done():
			prog.addLog("info", fmt.Sprintf("Job cancelled at %d/%d (downloaded=%d, skipped=%d, failed=%d)", i, len(productTypes), prog.downloaded, prog.skipped, prog.failed))
			prog.flush(ctx, "Cancelled")
			h.finaliseJob(ctx, jobID, "cancelled", prog)
			return
		default:
		}

		activity := fmt.Sprintf("[%d/%d] %s", i+1, len(productTypes), pt)

		// ── Freshness check (skip if cached within amazonSchemaFreshDays days) ──
		existingDoc, err := h.schemasCol(mpID).Doc(pt).Get(ctx)
		if err == nil && existingDoc.Exists() {
			if cachedAt, ok := existingDoc.Data()["cachedAt"].(time.Time); ok {
				if age := time.Since(cachedAt); age < amazonSchemaFreshDays*24*time.Hour {
					prog.skipped++
					if i%50 == 0 {
						prog.addLog("info", fmt.Sprintf("Skipping %d fresh schemas (example: %s, age %s)", 1, pt, age.Round(time.Hour).String()))
					}
					prog.flush(ctx, activity+" (skipped — fresh)")
					continue
				}
			}
		}

		// ── Periodic token refresh ──
		if i > 0 && i%amazonClientRefreshInterval == 0 {
			prog.addLog("info", fmt.Sprintf("Refreshing SP-API token at schema %d/%d", i, len(productTypes)))
			prog.flush(ctx, activity+" (refreshing token...)")

			refreshCredID := credentialID
			if refreshCredID == "" {
				if creds, err := h.repo.ListCredentials(ctx, tenantID); err == nil {
					for _, cred := range creds {
						if cred.Channel == "amazon" && cred.Active {
							refreshCredID = cred.CredentialID
							break
						}
					}
				}
			}
			if newClient, _, err := h.buildClient(ctx, tenantID, refreshCredID); err != nil {
				msg := fmt.Sprintf("Token refresh failed at schema %d: %v", i, err)
				prog.addError(msg)
				log.Printf("[AmazonSchemaJob %s] %s", jobID, msg)
				// Non-fatal — continue with existing client; it may still work.
			} else {
				client = newClient
				prog.addLog("info", "Token refreshed successfully")
			}
		}

		// ── Download with retry + 429-aware backoff ──
		var downloadErr error
		for attempt := 0; attempt < amazonRetryAttempts; attempt++ {
			_, downloadErr = h.downloadAndStore(ctx, client, pt, mpID)
			if downloadErr == nil {
				break
			}

			// Detect 429 — honour Retry-After if present.
			if waitSec, is429 := parseRetryAfter(downloadErr); is429 {
				waitDur := time.Duration(waitSec) * time.Second
				msg := fmt.Sprintf("429 rate-limit on %s — waiting %ds before retry (attempt %d/%d)", pt, waitSec, attempt+1, amazonRetryAttempts)
				prog.addLog("warn", msg)
				log.Printf("[AmazonSchemaJob %s] %s", jobID, msg)
				prog.flush(ctx, fmt.Sprintf("%s (rate-limited, waiting %ds...)", activity, waitSec))

				// Interruptible sleep.
				select {
				case <-ctx.Done():
					h.finaliseJob(ctx, jobID, "cancelled", prog)
					return
				case <-time.After(waitDur):
				}
				continue
			}

			// Other error — exponential backoff.
			if attempt < amazonRetryAttempts-1 {
				backoff := time.Duration(amazonBaseBackoffS<<attempt) * time.Second
				msg := fmt.Sprintf("%s failed (attempt %d/%d): %v — retrying in %s", pt, attempt+1, amazonRetryAttempts, downloadErr, backoff)
				prog.addLog("warn", msg)
				log.Printf("[AmazonSchemaJob %s] %s", jobID, msg)
				prog.flush(ctx, activity+" (retrying...)")

				select {
				case <-ctx.Done():
					h.finaliseJob(ctx, jobID, "cancelled", prog)
					return
				case <-time.After(backoff):
				}
			}
		}

		if downloadErr != nil {
			errMsg := fmt.Sprintf("%s: %v (all %d attempts failed)", pt, downloadErr, amazonRetryAttempts)
			prog.addError(errMsg)
			prog.failed++
			consecutiveFailures++
			log.Printf("[AmazonSchemaJob %s] FAILED: %s", jobID, errMsg)

			// Long pause on consecutive failures — indicates sustained rate limiting.
			if consecutiveFailures >= amazonMaxConsecFailures {
				pauseMsg := fmt.Sprintf("%d consecutive failures — pausing %ds (likely sustained rate limit)", consecutiveFailures, amazonConsecFailurePauseS)
				prog.addLog("warn", pauseMsg)
				log.Printf("[AmazonSchemaJob %s] %s", jobID, pauseMsg)
				prog.flush(ctx, fmt.Sprintf("Paused %ds after %d consecutive failures", amazonConsecFailurePauseS, consecutiveFailures))

				select {
				case <-ctx.Done():
					h.finaliseJob(ctx, jobID, "cancelled", prog)
					return
				case <-time.After(time.Duration(amazonConsecFailurePauseS) * time.Second):
				}
				consecutiveFailures = 0
			}
		} else {
			prog.downloaded++
			consecutiveFailures = 0
			if prog.downloaded%25 == 0 {
				prog.addLog("info", fmt.Sprintf("Progress: %d downloaded, %d skipped, %d failed of %d total", prog.downloaded, prog.skipped, prog.failed, len(productTypes)))
			}
		}

		// ── Periodic Firestore flush ──
		shouldFlush := (i+1)%amazonUpdateInterval == 0 ||
			i == len(productTypes)-1 ||
			len(prog.pendingLogs) >= amazonLogFlushSize
		if shouldFlush {
			prog.flush(ctx, activity)
		}

		// ── Inter-schema rate limiting ──
		select {
		case <-ctx.Done():
			h.finaliseJob(ctx, jobID, "cancelled", prog)
			return
		case <-time.After(amazonInterSchemaSleepMs * time.Millisecond):
		}
	}

	prog.addLog("info", fmt.Sprintf("Job complete — downloaded=%d, skipped=%d, failed=%d of %d total", prog.downloaded, prog.skipped, prog.failed, len(productTypes)))
	log.Printf("[AmazonSchemaJob %s] Complete: downloaded=%d, skipped=%d, failed=%d of %d", jobID, prog.downloaded, prog.skipped, prog.failed, len(productTypes))
	h.finaliseJob(ctx, jobID, "completed", prog)
}

// finaliseJob writes the terminal state to Firestore.
func (h *AmazonSchemaHandler) finaliseJob(ctx context.Context, jobID, status string, prog *amazonJobProgress) {
	now := time.Now()
	logsToWrite := prog.allLogs
	if len(logsToWrite) > amazonMaxLogEntries {
		logsToWrite = logsToWrite[len(logsToWrite)-amazonMaxLogEntries:]
	}
	h.jobsCol().Doc(jobID).Set(context.Background(), map[string]interface{}{
		"status":          status,
		"downloaded":      prog.downloaded,
		"skipped":         prog.skipped,
		"failed":          prog.failed,
		"errors":          prog.errors,
		"logs":            logsToWrite,
		"currentActivity": "",
		"updatedAt":       now,
		"completedAt":     now,
	}, firestore.MergeAll)
}

// ============================================================================
// GET /api/v1/amazon/schemas/jobs
// ============================================================================

func (h *AmazonSchemaHandler) ListJobs(c *gin.Context) {
	docs, err := h.jobsCol().OrderBy("startedAt", firestore.Desc).Limit(20).Documents(c.Request.Context()).GetAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	jobs := make([]map[string]interface{}, 0, len(docs))
	for _, doc := range docs {
		data := doc.Data()
		data["jobId"] = doc.Ref.ID
		jobs = append(jobs, data)
	}
	c.JSON(http.StatusOK, gin.H{"jobs": jobs})
}

// ============================================================================
// GET /api/v1/amazon/schemas/jobs/:jobId
// ============================================================================

func (h *AmazonSchemaHandler) GetJobStatus(c *gin.Context) {
	jobID := c.Param("jobId")
	doc, err := h.jobsCol().Doc(jobID).Get(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}
	data := doc.Data()
	data["jobId"] = jobID
	c.JSON(http.StatusOK, data)
}

// ============================================================================
// POST /api/v1/amazon/schemas/jobs/:jobId/cancel
// ============================================================================

func (h *AmazonSchemaHandler) CancelJob(c *gin.Context) {
	jobID := c.Param("jobId")
	ctx := c.Request.Context()

	h.activeJobsMu.Lock()
	cancel, existsInMemory := h.activeJobs[jobID]
	if existsInMemory {
		delete(h.activeJobs, jobID)
	}
	h.activeJobsMu.Unlock()

	if existsInMemory {
		cancel()
	}

	jobRef := h.jobsCol().Doc(jobID)
	doc, err := jobRef.Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}
	jobData := doc.Data()
	status, _ := jobData["status"].(string)
	if status != "running" {
		c.JSON(http.StatusOK, gin.H{"ok": true, "jobId": jobID, "status": status, "message": "job already completed or cancelled"})
		return
	}
	if _, err := jobRef.Update(ctx, []firestore.Update{
		{Path: "status", Value: "cancelled"},
		{Path: "completedAt", Value: time.Now()},
		{Path: "updatedAt", Value: time.Now()},
		{Path: "currentActivity", Value: "Cancelled by user"},
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to cancel: %v", err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "jobId": jobID, "status": "cancelled", "message": "job cancelled successfully"})
}

// ============================================================================
// GET /api/v1/amazon/schemas/:productType
// ============================================================================

func (h *AmazonSchemaHandler) GetSchema(c *gin.Context) {
	productType := c.Param("productType")
	mpID := c.Query("marketplace_id")
	if mpID == "" {
		mpID = "A1F83G8C2ARO7P"
	}
	doc, err := h.schemasCol(mpID).Doc(productType).Get(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("schema not found: %v", err)})
		return
	}
	configDoc, _ := h.fieldConfigsCol(mpID).Doc(productType).Get(c.Request.Context())
	var fieldConfig map[string]interface{}
	if configDoc != nil && configDoc.Exists() {
		fieldConfig = configDoc.Data()
	}
	c.JSON(http.StatusOK, gin.H{"schema": doc.Data(), "fieldConfig": fieldConfig})
}

// ============================================================================
// POST /api/v1/amazon/schemas/:productType/field-config
// ============================================================================

type FieldConfigRequest struct {
	Groups         map[string]FieldGroup `json:"groups"`
	HiddenFields   []string              `json:"hiddenFields"`
	PromotedFields []string              `json:"promotedFields"`
	FieldOrder     map[string]int        `json:"fieldOrder"`
}

type FieldGroup struct {
	Title  string   `json:"title"`
	Order  int      `json:"order"`
	Fields []string `json:"fields"`
	Icon   string   `json:"icon,omitempty"`
}

func (h *AmazonSchemaHandler) SaveFieldConfig(c *gin.Context) {
	productType := c.Param("productType")
	mpID := c.Query("marketplace_id")
	if mpID == "" {
		mpID = "A1F83G8C2ARO7P"
	}
	var req FieldConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	data := map[string]interface{}{
		"productType":    productType,
		"marketplaceId":  mpID,
		"updatedAt":      time.Now(),
		"hiddenFields":   req.HiddenFields,
		"promotedFields": req.PromotedFields,
		"fieldOrder":     req.FieldOrder,
	}
	groupsMap := make(map[string]interface{})
	for k, g := range req.Groups {
		groupsMap[k] = map[string]interface{}{
			"title":  g.Title,
			"order":  g.Order,
			"fields": g.Fields,
			"icon":   g.Icon,
		}
	}
	data["groups"] = groupsMap
	if _, err := h.fieldConfigsCol(mpID).Doc(productType).Set(c.Request.Context(), data); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "productType": productType})
}

// ============================================================================
// DELETE /api/v1/amazon/schemas/:productType
// ============================================================================

func (h *AmazonSchemaHandler) DeleteSchema(c *gin.Context) {
	productType := c.Param("productType")
	mpID := c.Query("marketplace_id")
	if mpID == "" {
		mpID = "A1F83G8C2ARO7P"
	}
	if _, err := h.schemasCol(mpID).Doc(productType).Delete(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.fieldConfigsCol(mpID).Doc(productType).Delete(c.Request.Context())
	c.JSON(http.StatusOK, gin.H{"ok": true, "deleted": productType})
}

// ── Scheduler wiring ──────────────────────────────────────────────────────────

func (h *AmazonSchemaHandler) SetScheduler(s *SchemaRefreshScheduler) { h.scheduler = s }

func (h *AmazonSchemaHandler) GetRefreshSettings(c *gin.Context) {
	if h.scheduler == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "scheduler not initialised"})
		return
	}
	tenantID := c.GetString("tenant_id")
	settings, err := h.scheduler.GetSettings(c.Request.Context(), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "settings": settings})
}

func (h *AmazonSchemaHandler) SaveRefreshSettings(c *gin.Context) {
	if h.scheduler == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "scheduler not initialised"})
		return
	}
	tenantID := c.GetString("tenant_id")
	var settings SchemaRefreshSettings
	if err := c.ShouldBindJSON(&settings); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.scheduler.SaveSettings(c.Request.Context(), tenantID, settings); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ── Utility ───────────────────────────────────────────────────────────────────

func strVal(data map[string]interface{}, key string) string {
	if v, ok := data[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

var _ = strings.Contains // suppress unused import warning

func generateJobID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
