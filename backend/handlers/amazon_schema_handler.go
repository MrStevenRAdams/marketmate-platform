package handlers

// ============================================================================
// AMAZON SCHEMA MANAGEMENT HANDLER
// ============================================================================
// Endpoints for managing Amazon product type schemas:
//   - GET    /schemas/list                          — List all cached schemas
//   - POST   /schemas/download                      — Download a specific product type schema
//   - POST   /schemas/download-all                  — Background job: download ALL product types
//   - GET    /schemas/jobs                           — List download jobs
//   - GET    /schemas/jobs/:jobId                    — Get job status + progress
//   - POST   /schemas/jobs/:jobId/cancel             — Cancel a running job
//   - GET    /schemas/:productType                   — Get a single cached schema + field config
//   - POST   /schemas/:productType/field-config      — Save custom field grouping/ordering
//   - DELETE /schemas/:productType                   — Delete a cached schema
//
// Firestore structure:
//   marketplaces/Amazon/{marketplace_id}/data/schemas/{productType}
//   marketplaces/Amazon/{marketplace_id}/data/field_configs/{productType}
//   marketplaces/Amazon/schema_jobs/{jobId}
// ============================================================================

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"module-a/marketplace/clients/amazon"
	"module-a/repository"
	"module-a/services"
)

type AmazonSchemaHandler struct {
	marketplaceService *services.MarketplaceService
	repo               *repository.MarketplaceRepository
	fsClient           *firestore.Client

	// Track active jobs in memory for cancellation
	activeJobs   map[string]context.CancelFunc
	activeJobsMu sync.Mutex

	// ENH-02: optional auto-refresh scheduler
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

// ── Firestore path helpers ──

func (h *AmazonSchemaHandler) schemasCol(marketplaceID string) *firestore.CollectionRef {
	return h.fsClient.Collection("marketplaces").Doc("Amazon").Collection(marketplaceID).Doc("data").Collection("schemas")
}

func (h *AmazonSchemaHandler) fieldConfigsCol(marketplaceID string) *firestore.CollectionRef {
	return h.fsClient.Collection("marketplaces").Doc("Amazon").Collection(marketplaceID).Doc("data").Collection("field_configs")
}

func (h *AmazonSchemaHandler) jobsCol() *firestore.CollectionRef {
	return h.fsClient.Collection("marketplaces").Doc("Amazon").Collection("schema_jobs")
}

// ── Helper: get SP-API client from credential_id query param ──

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

// buildClient creates an SP-API client from tenantID + credentialID.
// Works outside of a gin context (used by background goroutine).
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

// ============================================================================
// GET /api/v1/amazon/schemas/list?marketplace_id=X
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

	c.JSON(http.StatusOK, gin.H{
		"ok":          true,
		"productType": req.ProductType,
		"attrCount":   attrCount,
		"cachedAt":    time.Now(),
	})
}

// downloadAndStore fetches a single schema from SP-API and stores it in Firestore.
// Shared by single download and background download-all.
func (h *AmazonSchemaHandler) downloadAndStore(ctx context.Context, client *amazon.SPAPIClient, productType, mpID string) (int, error) {
	def, err := client.GetProductTypeDefinition(ctx, productType, "en_GB")
	if err != nil {
		return 0, fmt.Errorf("fetch definition for %s: %v", productType, err)
	}

	parsed, err := client.FetchAndParseSchema(ctx, def)
	if err != nil {
		return 0, fmt.Errorf("parse schema for %s: %v", productType, err)
	}

	// Convert to JSON and back to get a map for Firestore
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

	_, err = h.schemasCol(mpID).Doc(productType).Set(ctx, parsedMap)
	if err != nil {
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

	// Search for all product types
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

	// Create job document
	jobID := generateJobID()
	now := time.Now()
	tenantID := c.GetString("tenant_id")
	credentialID := c.Query("credential_id")

	jobData := map[string]interface{}{
		"jobId":        jobID,
		"status":       "running",
		"marketplaceId": mpID,
		"total":        len(productTypes),
		"downloaded":   0,
		"skipped":      0,
		"failed":       0,
		"startedAt":    now,
		"updatedAt":    now,
		"errors":       []string{},
	}

	if _, err := h.jobsCol().Doc(jobID).Set(c.Request.Context(), jobData); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("create job: %v", err)})
		return
	}

	// Start background download
	ctx, cancel := context.WithCancel(context.Background())
	h.activeJobsMu.Lock()
	h.activeJobs[jobID] = cancel
	h.activeJobsMu.Unlock()

	go h.downloadAll(ctx, jobID, client, productTypes, mpID, tenantID, credentialID)

	c.JSON(http.StatusOK, gin.H{
		"ok":    true,
		"jobId": jobID,
		"total": len(productTypes),
	})
}

// downloadAll is the background goroutine that downloads all product type schemas
// IMPROVED VERSION with resume capability, token refresh, retry logic, and better error handling
func (h *AmazonSchemaHandler) downloadAll(ctx context.Context, jobID string, client *amazon.SPAPIClient, productTypes []string, mpID, tenantID, credentialID string) {
	defer func() {
		h.activeJobsMu.Lock()
		delete(h.activeJobs, jobID)
		h.activeJobsMu.Unlock()
	}()

	log.Printf("[SchemaDownloadAll] Job %s started: %d product types to process for %s", jobID, len(productTypes), mpID)

	downloaded := 0
	skipped := 0
	failed := 0
	errors := []string{}
	const maxErrors = 100
	const updateInterval = 5
	const clientRefreshInterval = 50 // rebuild client every 50 schemas
	consecutiveFailures := 0
	const maxConsecutiveFailures = 10

	for i, pt := range productTypes {
		select {
		case <-ctx.Done():
			now := time.Now()
			h.jobsCol().Doc(jobID).Set(context.Background(), map[string]interface{}{
				"status":      "cancelled",
				"downloaded":  downloaded,
				"skipped":     skipped,
				"failed":      failed,
				"errors":      errors,
				"updatedAt":   now,
				"completedAt": now,
			}, firestore.MergeAll)
			return
		default:
		}

		// RESUME CAPABILITY: Check if schema already exists and is fresh
		existingDoc, err := h.schemasCol(mpID).Doc(pt).Get(ctx)
		if err == nil && existingDoc.Exists() {
			data := existingDoc.Data()
			if cachedAt, ok := data["cachedAt"].(time.Time); ok {
				age := time.Since(cachedAt)
				if age < 7*24*time.Hour { // 7 days fresh
					skipped++
					log.Printf("[SchemaDownloadAll] Skipping %s (cached %v ago)", pt, age.Round(time.Hour))
					continue
				}
			}
		}

		// TOKEN REFRESH: Build a fresh client periodically
		if i > 0 && i%clientRefreshInterval == 0 {
			log.Printf("[SchemaDownloadAll] Refreshing client at schema %d/%d", i, len(productTypes))
			
			// If credentialID is empty, re-discover it
			refreshCredID := credentialID
			if refreshCredID == "" {
				creds, err := h.repo.ListCredentials(ctx, tenantID)
				if err == nil {
					for _, cred := range creds {
						if cred.Channel == "amazon" && cred.Active {
							refreshCredID = cred.CredentialID
							break
						}
					}
				}
			}
			
			newClient, _, err := h.buildClient(ctx, tenantID, refreshCredID)
			if err != nil {
				errMsg := fmt.Sprintf("%s: client refresh error: %v", pt, err)
				if len(errors) < maxErrors {
					errors = append(errors, errMsg)
				}
				failed++
				log.Printf("[SchemaDownloadAll] %s", errMsg)
				// Wait before retry to avoid hammering on auth errors
				time.Sleep(5 * time.Second)
				continue
			}
			client = newClient
		}

		// RETRY WITH BACKOFF: Try up to 3 times with exponential backoff
		var downloadErr error
		for attempt := 0; attempt < 3; attempt++ {
			_, downloadErr = h.downloadAndStore(ctx, client, pt, mpID)
			if downloadErr == nil {
				downloaded++
				consecutiveFailures = 0
				break
			}

			// Retry with exponential backoff
			if attempt < 2 {
				backoff := time.Duration(2<<attempt) * time.Second // 2s, 4s, 8s
				log.Printf("[SchemaDownloadAll] %s failed (attempt %d/3), retrying in %v: %v", pt, attempt+1, backoff, downloadErr)
				time.Sleep(backoff)
			}
		}

		if downloadErr != nil {
			errMsg := fmt.Sprintf("%s: %v (after 3 attempts)", pt, downloadErr)
			if len(errors) < maxErrors {
				errors = append(errors, errMsg)
			}
			failed++
			consecutiveFailures++
			log.Printf("[SchemaDownloadAll] Failed: %s", errMsg)

			// PAUSE ON CONSECUTIVE FAILURES: likely rate limited
			if consecutiveFailures >= maxConsecutiveFailures {
				log.Printf("[SchemaDownloadAll] Too many consecutive failures (%d), pausing 30s...", consecutiveFailures)
				time.Sleep(30 * time.Second)
				consecutiveFailures = 0
			}
		}

		// Update Firestore progress periodically
		if (i+1)%updateInterval == 0 || i == len(productTypes)-1 {
			h.jobsCol().Doc(jobID).Set(context.Background(), map[string]interface{}{
				"downloaded": downloaded,
				"skipped":    skipped,
				"failed":     failed,
				"errors":     errors,
				"updatedAt":  time.Now(),
			}, firestore.MergeAll)
		}

		// IMPROVED RATE LIMIT: 500ms gap = ~2/sec to stay well under 5 req/sec burst 10 limit
		time.Sleep(500 * time.Millisecond)
	}

	// Mark complete
	now := time.Now()
	h.jobsCol().Doc(jobID).Set(context.Background(), map[string]interface{}{
		"status":      "completed",
		"downloaded":  downloaded,
		"skipped":     skipped,
		"failed":      failed,
		"errors":      errors,
		"updatedAt":   now,
		"completedAt": now,
	}, firestore.MergeAll)

	log.Printf("[SchemaDownloadAll] Job %s completed: %d downloaded, %d skipped, %d failed out of %d", jobID, downloaded, skipped, failed, len(productTypes))
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

	// Try to cancel in-memory job first
	h.activeJobsMu.Lock()
	cancel, existsInMemory := h.activeJobs[jobID]
	if existsInMemory {
		delete(h.activeJobs, jobID)
	}
	h.activeJobsMu.Unlock()

	// If found in memory, signal cancellation
	if existsInMemory {
		cancel()
	}

	// ALWAYS update Firestore status (works even if job isn't in memory)
	jobRef := h.jobsCol().Doc(jobID)
	doc, err := jobRef.Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}

	jobData := doc.Data()
	status, _ := jobData["status"].(string)

	// Only cancel if job is still running
	if status != "running" {
		c.JSON(http.StatusOK, gin.H{
			"ok":      true,
			"jobId":   jobID,
			"status":  status,
			"message": "job already completed or cancelled",
		})
		return
	}

	// Update job status to cancelled in Firestore
	if _, err := jobRef.Update(ctx, []firestore.Update{
		{Path: "status", Value: "cancelled"},
		{Path: "completedAt", Value: time.Now()},
		{Path: "updatedAt", Value: time.Now()},
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to cancel job: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"jobId":   jobID,
		"status":  "cancelled",
		"message": "job cancelled successfully",
	})
}

// ============================================================================
// GET /api/v1/amazon/schemas/:productType?marketplace_id=X
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

	c.JSON(http.StatusOK, gin.H{
		"schema":      doc.Data(),
		"fieldConfig": fieldConfig,
	})
}

// ============================================================================
// POST /api/v1/amazon/schemas/:productType/field-config?marketplace_id=X
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

	_, err := h.fieldConfigsCol(mpID).Doc(productType).Set(c.Request.Context(), data)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "productType": productType})
}

// ============================================================================
// DELETE /api/v1/amazon/schemas/:productType?marketplace_id=X
// ============================================================================

func (h *AmazonSchemaHandler) DeleteSchema(c *gin.Context) {
	productType := c.Param("productType")
	mpID := c.Query("marketplace_id")
	if mpID == "" {
		mpID = "A1F83G8C2ARO7P"
	}

	_, err := h.schemasCol(mpID).Doc(productType).Delete(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.fieldConfigsCol(mpID).Doc(productType).Delete(c.Request.Context())

	c.JSON(http.StatusOK, gin.H{"ok": true, "deleted": productType})
}

// ── Utility ──

func strVal(data map[string]interface{}, key string) string {
	if v, ok := data[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// Suppress unused import warnings
var _ = strings.Contains

// generateJobID creates a random ID for background jobs.
func generateJobID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// ============================================================================
// GET  /api/v1/amazon/schemas/refresh-settings  — ENH-02
// PUT  /api/v1/amazon/schemas/refresh-settings  — ENH-02
// ============================================================================
// These endpoints expose the auto-refresh scheduler settings for the frontend
// SchemaCacheManager. A SchemaRefreshScheduler must be injected via
// SetScheduler before these handlers are called.
// ============================================================================

func (h *AmazonSchemaHandler) SetScheduler(s *SchemaRefreshScheduler) {
	h.scheduler = s
}

// GetRefreshSettings returns the current auto-refresh settings for the tenant.
func (h *AmazonSchemaHandler) GetRefreshSettings(c *gin.Context) {
	if h.scheduler == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "scheduler not initialised"})
		return
	}
	tenantID := c.GetString("tenant_id")
	settings, err := h.scheduler.getSettings(c.Request.Context(), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "settings": settings})
}

// SaveRefreshSettings persists updated auto-refresh settings for the tenant.
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
