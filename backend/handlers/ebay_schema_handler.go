package handlers

// ============================================================================
// EBAY SCHEMA CACHE HANDLER
// ============================================================================
// Endpoints for managing eBay category trees and item aspects (attributes):
//   - GET    /schemas/list                          — List cached aspect schemas
//   - GET    /schemas/stats                         — Cache statistics
//   - POST   /schemas/sync                          — Background job: sync category tree + aspects
//   - GET    /schemas/jobs                          — List sync jobs
//   - GET    /schemas/jobs/:jobId                   — Get job status + progress
//   - POST   /schemas/jobs/:jobId/cancel            — Cancel a running job
//   - GET    /schemas/category/:categoryId          — Get cached aspects for one category
//
// Firestore structure:
//   marketplaces/eBay/{marketplace_id}/data/category_tree  — full flattened tree
//   marketplaces/eBay/{marketplace_id}/data/aspects/{categoryId}
//   marketplaces/eBay/schema_jobs/{jobId}
// ============================================================================

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"module-a/marketplace/clients/ebay"
	"module-a/repository"
	"module-a/services"
	"google.golang.org/api/iterator"
)

type EbaySchemaHandler struct {
	marketplaceService *services.MarketplaceService
	repo               *repository.MarketplaceRepository
	fsClient           *firestore.Client

	// Track active jobs in memory for cancellation
	activeJobs   map[string]context.CancelFunc
	activeJobsMu sync.Mutex

	// USP-04: auto-refresh scheduler (injected after construction)
	scheduler *EbaySchemaRefreshScheduler
}

func NewEbaySchemaHandler(
	marketplaceService *services.MarketplaceService,
	repo *repository.MarketplaceRepository,
	fsClient *firestore.Client,
) *EbaySchemaHandler {
	return &EbaySchemaHandler{
		marketplaceService: marketplaceService,
		repo:               repo,
		fsClient:           fsClient,
		activeJobs:         make(map[string]context.CancelFunc),
	}
}

// ── Firestore path helpers ──

func (h *EbaySchemaHandler) aspectsCol(marketplaceID string) *firestore.CollectionRef {
	return h.fsClient.Collection("marketplaces").Doc("eBay").Collection(marketplaceID).Doc("data").Collection("aspects")
}

func (h *EbaySchemaHandler) categoryTreeDoc(marketplaceID string) *firestore.DocumentRef {
	return h.fsClient.Collection("marketplaces").Doc("eBay").Collection(marketplaceID).Doc("data").Collection("meta").Doc("category_tree")
}

func (h *EbaySchemaHandler) jobsCol() *firestore.CollectionRef {
	return h.fsClient.Collection("marketplaces").Doc("eBay").Collection("schema_jobs")
}

// ── Helper: get eBay client from credential_id query param ──

func (h *EbaySchemaHandler) getClient(c *gin.Context) (*ebay.Client, string, error) {
	tenantID := c.GetString("tenant_id")
	credentialID := c.Query("credential_id")

	if credentialID == "" {
		// Use context.Background() — not c.Request.Context() which dies when response is sent
		creds, err := h.repo.ListCredentials(context.Background(), tenantID)
		if err != nil {
			return nil, "", fmt.Errorf("list credentials: %w", err)
		}
		for _, cred := range creds {
			if cred.Channel == "ebay" && cred.Active {
				credentialID = cred.CredentialID
				break
			}
		}
		if credentialID == "" {
			return nil, "", fmt.Errorf("no active eBay credential found for this account — please check Marketplace Connections")
		}
	}

	return h.buildClient(context.Background(), tenantID, credentialID)
}

// buildClient creates an eBay client from tenantID + credentialID
func (h *EbaySchemaHandler) buildClient(ctx context.Context, tenantID, credentialID string) (*ebay.Client, string, error) {
	cred, err := h.repo.GetCredential(ctx, tenantID, credentialID)
	if err != nil {
		return nil, "", fmt.Errorf("get credential: %w", err)
	}
	mergedCreds, err := h.marketplaceService.GetFullCredentials(ctx, cred)
	if err != nil {
		return nil, "", fmt.Errorf("merge credentials: %w", err)
	}

	marketplaceID := mergedCreds["marketplace_id"]
	if marketplaceID == "" {
		marketplaceID = "EBAY_GB"
	}

	production := true
	if env, ok := mergedCreds["environment"]; ok && env == "sandbox" {
		production = false
	}

	client := ebay.NewClient(
		mergedCreds["client_id"],
		mergedCreds["client_secret"],
		mergedCreds["dev_id"],
		production,
	)
	client.SetTokens(mergedCreds["access_token"], mergedCreds["refresh_token"])

	return client, marketplaceID, nil
}

// ============================================================================
// GET /api/v1/ebay/schemas/list?marketplace_id=X
// ============================================================================

type AspectListItem struct {
	CategoryID   string    `json:"categoryId"`
	CategoryName string    `json:"categoryName"`
	AspectCount  int       `json:"aspectCount"`
	CachedAt     time.Time `json:"cachedAt"`
}

func (h *EbaySchemaHandler) ListSchemas(c *gin.Context) {
	mpID := c.Query("marketplace_id")
	if mpID == "" {
		mpID = "EBAY_GB"
	}

	docs, err := h.aspectsCol(mpID).Documents(c.Request.Context()).GetAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	items := make([]AspectListItem, 0, len(docs))
	for _, doc := range docs {
		data := doc.Data()
		item := AspectListItem{
			CategoryID:   doc.Ref.ID,
			CategoryName: getStringValue(data, "categoryName"),
		}
		if aspects, ok := data["aspects"]; ok {
			if arr, ok := aspects.([]interface{}); ok {
				item.AspectCount = len(arr)
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
// GET /api/v1/ebay/schemas/stats?marketplace_id=X
// ============================================================================

func (h *EbaySchemaHandler) Stats(c *gin.Context) {
	mpID := c.Query("marketplace_id")
	if mpID == "" {
		mpID = "EBAY_GB"
	}

	// Get category tree stats
	treeDoc, err := h.categoryTreeDoc(mpID).Get(c.Request.Context())
	totalCategories := 0
	leafCategories := 0
	lastTreeSync := time.Time{}

	if err == nil && treeDoc.Exists() {
		data := treeDoc.Data()
		if cats, ok := data["categories"]; ok {
			if arr, ok := cats.([]interface{}); ok {
				totalCategories = len(arr)
				// Count leaf categories
				for _, cat := range arr {
					if catMap, ok := cat.(map[string]interface{}); ok {
						if leaf, ok := catMap["leaf"].(bool); ok && leaf {
							leafCategories++
						}
					}
				}
			}
		}
		if t, ok := data["cachedAt"]; ok {
			if ts, ok := t.(time.Time); ok {
				lastTreeSync = ts
			}
		}
	}

	// Count cached aspects
	cachedAspects := 0
	iter := h.aspectsCol(mpID).Documents(c.Request.Context())
	for {
		_, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		cachedAspects++
	}

	c.JSON(http.StatusOK, gin.H{
		"marketplaceId":    mpID,
		"totalCategories":  totalCategories,
		"leafCategories":   leafCategories,
		"cachedAspects":    cachedAspects,
		"lastSync":         lastTreeSync,
		"cachePercentage":  ebayCalculatePercentage(cachedAspects, leafCategories),
	})
}

func ebayCalculatePercentage(cached, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(cached) / float64(total) * 100
}

// ============================================================================
// POST /api/v1/ebay/schemas/sync
// ============================================================================

type EbaySyncRequest struct {
	MarketplaceID string `json:"marketplaceId"`
	FullSync      bool   `json:"fullSync"` // if false, only sync stale/missing
}

func (h *EbaySchemaHandler) Sync(c *gin.Context) {
	var req EbaySyncRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	mpID := req.MarketplaceID
	if mpID == "" {
		mpID = "EBAY_GB"
	}

	client, resolvedMpID, err := h.getClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if mpID == "" {
		mpID = resolvedMpID
	}

	// Create job document
	jobID := ebayGenerateJobID()
	now := time.Now()
	tenantID := c.GetString("tenant_id")
	credentialID := c.Query("credential_id")

	jobData := map[string]interface{}{
		"jobId":         jobID,
		"status":        "running",
		"marketplaceId": mpID,
		"fullSync":      req.FullSync,
		"startedAt":     now,
		"updatedAt":     now,
		"downloaded":    0,
		"skipped":       0,
		"failed":        0,
		"total":         0,
		"errors":        []string{},
	}

	if _, err := h.jobsCol().Doc(jobID).Set(context.Background(), jobData); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("create job: %v", err)})
		return
	}

	// Start background sync
	ctx, cancel := context.WithCancel(context.Background())
	h.activeJobsMu.Lock()
	h.activeJobs[jobID] = cancel
	h.activeJobsMu.Unlock()

	go h.runSync(ctx, jobID, client, mpID, tenantID, credentialID, req.FullSync)

	c.JSON(http.StatusOK, gin.H{
		"ok":    true,
		"jobId": jobID,
		"status": "started",
	})
}

// runSync is the background goroutine that downloads category tree + aspects
func (h *EbaySchemaHandler) runSync(ctx context.Context, jobID string, client *ebay.Client, mpID, tenantID, credentialID string, fullSync bool) {
	defer func() {
		h.activeJobsMu.Lock()
		delete(h.activeJobs, jobID)
		h.activeJobsMu.Unlock()
	}()

	log.Printf("[EbaySyncJob %s] Starting sync for %s (fullSync=%v)", jobID, mpID, fullSync)

	downloaded := 0
	skipped := 0
	failed := 0
	errors := []string{}
	const maxErrors = 100
	const updateInterval = 50 // update Firestore every 50 categories

	// STEP 1: Download category tree
	log.Printf("[EbaySyncJob %s] Downloading category tree...", jobID)
	categoryTree, err := client.GetCategoryTree(mpID)
	if err != nil {
		errMsg := fmt.Sprintf("Failed to fetch category tree: %v", err)
		log.Printf("[EbaySyncJob %s] %s", jobID, errMsg)
		h.jobsCol().Doc(jobID).Set(context.Background(), map[string]interface{}{
			"status":      "failed",
			"error":       errMsg,
			"updatedAt":   time.Now(),
			"completedAt": time.Now(),
		}, firestore.MergeAll)
		return
	}

	// Flatten the tree and store
	flatTree := flattenCategoryTree(categoryTree)
	leafCategories := []map[string]interface{}{}
	for _, cat := range flatTree {
		if leaf, ok := cat["leaf"].(bool); ok && leaf {
			leafCategories = append(leafCategories, cat)
		}
	}

	treeData := map[string]interface{}{
		"marketplaceId": mpID,
		"treeId":        ebay.GetTreeIDPublic(mpID),
		"categories":    flatTree,
		"cachedAt":      time.Now(),
		"totalCount":    len(flatTree),
		"leafCount":     len(leafCategories),
	}

	if _, err := h.categoryTreeDoc(mpID).Set(context.Background(), treeData); err != nil {
		errMsg := fmt.Sprintf("Failed to store category tree: %v", err)
		log.Printf("[EbaySyncJob %s] %s", jobID, errMsg)
		h.jobsCol().Doc(jobID).Set(context.Background(), map[string]interface{}{
			"status":      "failed",
			"error":       errMsg,
			"updatedAt":   time.Now(),
			"completedAt": time.Now(),
		}, firestore.MergeAll)
		return
	}

	log.Printf("[EbaySyncJob %s] Category tree cached: %d total, %d leaf", jobID, len(flatTree), len(leafCategories))

	// Update job with total count
	h.jobsCol().Doc(jobID).Set(context.Background(), map[string]interface{}{
		"total":        len(leafCategories),
		"treeDownloaded": true,
		"updatedAt":    time.Now(),
	}, firestore.MergeAll)

	// STEP 2: Download aspects for each leaf category
	log.Printf("[EbaySyncJob %s] Downloading aspects for %d leaf categories...", jobID, len(leafCategories))

	consecutiveFailures := 0
	const maxConsecutiveFailures = 10

	for i, catData := range leafCategories {
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

		categoryID, _ := catData["categoryId"].(string)
		categoryName, _ := catData["categoryName"].(string)

		if categoryID == "" {
			continue
		}

		// Check if we should skip (freshness check)
		if !fullSync {
			existingDoc, err := h.aspectsCol(mpID).Doc(categoryID).Get(ctx)
			if err == nil && existingDoc.Exists() {
				data := existingDoc.Data()
				if cachedAt, ok := data["cachedAt"].(time.Time); ok {
					age := time.Since(cachedAt)
					if age < 7*24*time.Hour { // 7 days fresh
						skipped++
						continue
					}
				}
			}
		}

		// Rebuild client periodically to handle token refresh
		if i > 0 && i%100 == 0 {
			newClient, _, err := h.buildClient(ctx, tenantID, credentialID)
			if err != nil {
				errMsg := fmt.Sprintf("Token refresh failed at category %d: %v", i, err)
				log.Printf("[EbaySyncJob %s] %s", jobID, errMsg)
				if len(errors) < maxErrors {
					errors = append(errors, errMsg)
				}
				failed++
				consecutiveFailures++
				if consecutiveFailures >= maxConsecutiveFailures {
					log.Printf("[EbaySyncJob %s] Too many consecutive failures, pausing 30s...", jobID)
					time.Sleep(30 * time.Second)
					consecutiveFailures = 0
				}
				continue
			}
			client = newClient
		}

		// Download aspects with retry
		aspects, err := h.downloadAspectsWithRetry(ctx, client, mpID, categoryID, 3)
		if err != nil {
			errMsg := fmt.Sprintf("%s (%s): %v", categoryID, categoryName, err)
			if len(errors) < maxErrors {
				errors = append(errors, errMsg)
			}
			failed++
			consecutiveFailures++
			log.Printf("[EbaySyncJob %s] Failed: %s", jobID, errMsg)

			if consecutiveFailures >= maxConsecutiveFailures {
				log.Printf("[EbaySyncJob %s] Too many consecutive failures, pausing 30s...", jobID)
				time.Sleep(30 * time.Second)
				consecutiveFailures = 0
			}
		} else {
			// Store aspects
			aspectData := map[string]interface{}{
				"categoryId":    categoryID,
				"categoryName":  categoryName,
				"marketplaceId": mpID,
				"aspects":       aspects,
				"cachedAt":      time.Now(),
			}
			if _, err := h.aspectsCol(mpID).Doc(categoryID).Set(context.Background(), aspectData); err != nil {
				errMsg := fmt.Sprintf("%s: store failed: %v", categoryID, err)
				if len(errors) < maxErrors {
					errors = append(errors, errMsg)
				}
				failed++
			} else {
				downloaded++
				consecutiveFailures = 0
			}
		}

		// Update progress periodically
		if (i+1)%updateInterval == 0 || i == len(leafCategories)-1 {
			h.jobsCol().Doc(jobID).Set(context.Background(), map[string]interface{}{
				"downloaded": downloaded,
				"skipped":    skipped,
				"failed":     failed,
				"errors":     errors,
				"updatedAt":  time.Now(),
			}, firestore.MergeAll)
		}

		// Rate limit: 200ms between calls (5 req/sec eBay limit)
		time.Sleep(200 * time.Millisecond)
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

	log.Printf("[EbaySyncJob %s] Completed: %d downloaded, %d skipped, %d failed out of %d", jobID, downloaded, skipped, failed, len(leafCategories))
}

// downloadAspectsWithRetry retries up to maxRetries times with exponential backoff
func (h *EbaySchemaHandler) downloadAspectsWithRetry(ctx context.Context, client *ebay.Client, mpID, categoryID string, maxRetries int) ([]ebay.ItemAspect, error) {
	var lastErr error
	backoff := 2 * time.Second

	for i := 0; i < maxRetries; i++ {
		aspects, err := client.GetItemAspectsForCategory(mpID, categoryID)
		if err == nil {
			return aspects, nil
		}
		lastErr = err
		
		if i < maxRetries-1 {
			time.Sleep(backoff)
			backoff *= 2
		}
	}

	return nil, lastErr
}

// flattenCategoryTree converts the eBay tree response to a flat array
func flattenCategoryTree(tree *ebay.CategoryTreeResponse) []map[string]interface{} {
	var result []map[string]interface{}
	
	var flatten func(node ebay.CategoryTreeNode, parentID string, level int)
	flatten = func(node ebay.CategoryTreeNode, parentID string, level int) {
		cat := map[string]interface{}{
			"categoryId":   node.CategoryID,
			"categoryName": node.CategoryName,
			"level":        level,
			"leaf":         node.LeafCategory,
		}
		if parentID != "" {
			cat["parentId"] = parentID
		}
		result = append(result, cat)

		for _, child := range node.ChildNodes {
			flatten(child, node.CategoryID, level+1)
		}
	}

	if tree != nil && tree.RootNode.CategoryID != "" {
		flatten(tree.RootNode, "", 0)
	}

	return result
}

// ============================================================================
// GET /api/v1/ebay/schemas/jobs
// ============================================================================

func (h *EbaySchemaHandler) ListJobs(c *gin.Context) {
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
// GET /api/v1/ebay/schemas/jobs/:jobId
// ============================================================================

func (h *EbaySchemaHandler) GetJobStatus(c *gin.Context) {
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
// POST /api/v1/ebay/schemas/jobs/:jobId/cancel
// ============================================================================

func (h *EbaySchemaHandler) CancelJob(c *gin.Context) {
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
// GET /api/v1/ebay/schemas/category/:categoryId?marketplace_id=X
// ============================================================================

func (h *EbaySchemaHandler) GetCategoryAspects(c *gin.Context) {
	categoryID := c.Param("categoryId")
	mpID := c.Query("marketplace_id")
	if mpID == "" {
		mpID = "EBAY_GB"
	}

	doc, err := h.aspectsCol(mpID).Doc(categoryID).Get(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("aspects not found: %v", err)})
		return
	}

	c.JSON(http.StatusOK, doc.Data())
}

// ── Utility ──

func getStringValue(data map[string]interface{}, key string) string {
	if v, ok := data[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// ebayGenerateJobID creates a random ID for background jobs
func ebayGenerateJobID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// ============================================================================
// AUTO-REFRESH SETTINGS ENDPOINTS — USP-04
// ============================================================================
// These endpoints expose the EbaySchemaRefreshScheduler settings so the
// SchemaCacheManager frontend panel can toggle auto-refresh on/off.
// SetScheduler must be called before these handlers are used.

func (h *EbaySchemaHandler) SetScheduler(s *EbaySchemaRefreshScheduler) {
	h.scheduler = s
}

// GET /api/v1/ebay/schemas/refresh-settings
func (h *EbaySchemaHandler) GetRefreshSettings(c *gin.Context) {
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

// PUT /api/v1/ebay/schemas/refresh-settings
func (h *EbaySchemaHandler) SaveRefreshSettings(c *gin.Context) {
	if h.scheduler == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "scheduler not initialised"})
		return
	}
	tenantID := c.GetString("tenant_id")
	var settings EbaySchemaRefreshSettings
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
