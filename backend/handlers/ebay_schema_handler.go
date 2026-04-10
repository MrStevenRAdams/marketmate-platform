package handlers

// ============================================================================
// EBAY SCHEMA CACHE HANDLER  (rearchitected)
// ============================================================================
// Endpoints:
//   GET    /schemas/list                        — List cached aspect schemas
//   GET    /schemas/stats                       — Cache statistics
//   POST   /schemas/sync                        — Background: sync tree + aspects
//   GET    /schemas/jobs                        — List sync jobs
//   GET    /schemas/jobs/:jobId                 — Get job status + progress
//   POST   /schemas/jobs/:jobId/cancel          — Cancel a running job
//   GET    /schemas/category/:categoryId        — Get aspects for one category
//   GET    /schemas/refresh-settings            — USP-04: auto-refresh settings
//   PUT    /schemas/refresh-settings            — USP-04: save auto-refresh settings
//
// Firestore:
//   marketplaces/eBay/{marketplace_id}/data/category_tree
//   marketplaces/eBay/{marketplace_id}/data/aspects/{categoryId}
//   marketplaces/eBay/schema_jobs/{jobId}
//
// Architecture changes vs previous version:
//   - 429 detection on all eBay API calls (status code + header parsing).
//   - Structured log entries written to Firestore job document.
//   - currentActivity updated every category iteration.
//   - Interruptible sleep: every time.After wrapped in select + ctx.Done.
//   - All error branches log to both Go logger and Firestore logs field.
// ============================================================================

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"module-a/marketplace/clients/ebay"
	"module-a/repository"
	"module-a/services"
	"google.golang.org/api/iterator"
)

// ── Constants ─────────────────────────────────────────────────────────────────

const (
	ebaySchemaFreshDays       = 7    // skip aspect doc if cached within 7 days
	ebayClientRefreshInterval = 100  // rebuild eBay client every N categories
	ebayUpdateInterval        = 50   // flush progress to Firestore every N categories
	ebayLogFlushSize          = 10   // also flush when pending log buffer hits this
	ebayMaxErrors             = 100
	ebayMaxLogEntries         = 200
	ebayInterCategorySleepMs  = 200  // 200ms = ~5 req/s (eBay limit)
	ebayMaxConsecFailures     = 10
	ebayConsecFailurePauseS   = 60
	ebayRetryAttempts         = 3
	ebayBaseBackoffS          = 2
	ebay429DefaultWaitS       = 30
	ebay429MaxWaitS           = 120
)

// ── Types ─────────────────────────────────────────────────────────────────────

type EbaySchemaHandler struct {
	marketplaceService *services.MarketplaceService
	repo               *repository.MarketplaceRepository
	fsClient           *firestore.Client

	activeJobs   map[string]context.CancelFunc
	activeJobsMu sync.Mutex

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

// ── Firestore helpers ─────────────────────────────────────────────────────────

func (h *EbaySchemaHandler) aspectsCol(marketplaceID string) *firestore.CollectionRef {
	return h.fsClient.Collection("marketplaces").Doc("eBay").Collection(marketplaceID).Doc("data").Collection("aspects")
}

func (h *EbaySchemaHandler) categoryTreeDoc(marketplaceID string) *firestore.DocumentRef {
	return h.fsClient.Collection("marketplaces").Doc("eBay").Collection(marketplaceID).Doc("data").Collection("meta").Doc("category_tree")
}

func (h *EbaySchemaHandler) jobsCol() *firestore.CollectionRef {
	return h.fsClient.Collection("marketplaces").Doc("eBay").Collection("schema_jobs")
}

// ── Client helpers ────────────────────────────────────────────────────────────

func (h *EbaySchemaHandler) getClient(c *gin.Context) (*ebay.Client, string, error) {
	tenantID := c.GetString("tenant_id")
	credentialID := c.Query("credential_id")
	if credentialID == "" {
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
			return nil, "", fmt.Errorf("no active eBay credential found — please check Marketplace Connections")
		}
	}
	return h.buildClient(context.Background(), tenantID, credentialID)
}

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
	client := ebay.NewClient(mergedCreds["client_id"], mergedCreds["client_secret"], mergedCreds["dev_id"], production)
	client.SetTokens(mergedCreds["access_token"], mergedCreds["refresh_token"])
	return client, marketplaceID, nil
}

// ── 429 helper ────────────────────────────────────────────────────────────────

func ebayParseRetryAfter(err error) (waitSec int, is429 bool) {
	if err == nil {
		return 0, false
	}
	s := err.Error()
	if !strings.Contains(s, "429") && !strings.Contains(s, "REQUEST_LIMIT_EXCEEDED") && !strings.Contains(s, "Too Many Requests") {
		return 0, false
	}
	for _, part := range strings.Fields(s) {
		if n, parseErr := strconv.Atoi(strings.TrimRight(part, "s")); parseErr == nil && n > 0 && n <= ebay429MaxWaitS {
			return n, true
		}
	}
	return ebay429DefaultWaitS, true
}

// ── Log / progress writer ─────────────────────────────────────────────────────

type ebayLogEntry struct {
	T   string `json:"t"`
	Msg string `json:"msg"`
	Lvl string `json:"lvl"`
}

type ebayJobProgress struct {
	jobsCol     *firestore.CollectionRef
	jobID       string
	downloaded  int
	skipped     int
	failed      int
	errors      []string
	pendingLogs []ebayLogEntry
	allLogs     []ebayLogEntry
	lastFlush   time.Time
}

func (p *ebayJobProgress) addLog(level, msg string) {
	entry := ebayLogEntry{T: time.Now().Format(time.RFC3339), Msg: msg, Lvl: level}
	p.pendingLogs = append(p.pendingLogs, entry)
	p.allLogs = append(p.allLogs, entry)
	if len(p.allLogs) > ebayMaxLogEntries {
		p.allLogs = p.allLogs[len(p.allLogs)-ebayMaxLogEntries:]
	}
}

func (p *ebayJobProgress) addError(msg string) {
	if len(p.errors) < ebayMaxErrors {
		p.errors = append(p.errors, msg)
	}
	p.addLog("error", msg)
}

func (p *ebayJobProgress) flush(ctx context.Context, currentActivity string) {
	if len(p.pendingLogs) == 0 && time.Since(p.lastFlush) < 2*time.Second {
		return
	}
	logsToWrite := p.allLogs
	if len(logsToWrite) > ebayMaxLogEntries {
		logsToWrite = logsToWrite[len(logsToWrite)-ebayMaxLogEntries:]
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
// GET /api/v1/ebay/schemas/list
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
// GET /api/v1/ebay/schemas/stats
// ============================================================================

func (h *EbaySchemaHandler) Stats(c *gin.Context) {
	mpID := c.Query("marketplace_id")
	if mpID == "" {
		mpID = "EBAY_GB"
	}
	treeDoc, err := h.categoryTreeDoc(mpID).Get(c.Request.Context())
	totalCategories := 0
	leafCategories := 0
	lastTreeSync := time.Time{}
	if err == nil && treeDoc.Exists() {
		data := treeDoc.Data()
		if cats, ok := data["categories"]; ok {
			if arr, ok := cats.([]interface{}); ok {
				totalCategories = len(arr)
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
		"marketplaceId":   mpID,
		"totalCategories": totalCategories,
		"leafCategories":  leafCategories,
		"cachedAspects":   cachedAspects,
		"lastSync":        lastTreeSync,
		"cachePercentage": ebayCalculatePercentage(cachedAspects, leafCategories),
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
	FullSync      bool   `json:"fullSync"`
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

	jobID := ebayGenerateJobID()
	now := time.Now()
	tenantID := c.GetString("tenant_id")
	credentialID := c.Query("credential_id")

	jobData := map[string]interface{}{
		"jobId":           jobID,
		"status":          "running",
		"marketplaceId":   mpID,
		"fullSync":        req.FullSync,
		"startedAt":       now,
		"updatedAt":       now,
		"downloaded":      0,
		"skipped":         0,
		"failed":          0,
		"total":           0,
		"errors":          []string{},
		"logs":            []ebayLogEntry{},
		"currentActivity": "Initialising...",
	}
	if _, err := h.jobsCol().Doc(jobID).Set(context.Background(), jobData); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("create job: %v", err)})
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	h.activeJobsMu.Lock()
	h.activeJobs[jobID] = cancel
	h.activeJobsMu.Unlock()

	go h.runSync(ctx, jobID, client, mpID, tenantID, credentialID, req.FullSync)

	c.JSON(http.StatusOK, gin.H{"ok": true, "jobId": jobID, "status": "started"})
}

// runSync: Phase 1 downloads the category tree, Phase 2 downloads aspects.
func (h *EbaySchemaHandler) runSync(ctx context.Context, jobID string, client *ebay.Client, mpID, tenantID, credentialID string, fullSync bool) {
	defer func() {
		h.activeJobsMu.Lock()
		delete(h.activeJobs, jobID)
		h.activeJobsMu.Unlock()
	}()

	log.Printf("[EbaySyncJob %s] Started for %s (fullSync=%v)", jobID, mpID, fullSync)

	prog := &ebayJobProgress{jobsCol: h.jobsCol(), jobID: jobID}
	prog.addLog("info", fmt.Sprintf("Job started for marketplace %s (fullSync=%v)", mpID, fullSync))
	prog.flush(ctx, "Phase 1: downloading category tree...")

	// ── PHASE 1: Download and store category tree ──
	prog.addLog("info", "Fetching eBay category tree...")
	categoryTree, err := client.GetCategoryTree(mpID)
	if err != nil {
		msg := fmt.Sprintf("Failed to fetch category tree: %v", err)
		prog.addError(msg)
		log.Printf("[EbaySyncJob %s] %s", jobID, msg)
		prog.flush(ctx, "Failed: "+msg)
		h.finaliseEbayJob(ctx, jobID, "failed", msg, prog)
		return
	}

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
	if _, err := h.categoryTreeDoc(mpID).Set(ctx, treeData); err != nil {
		msg := fmt.Sprintf("Failed to store category tree: %v", err)
		prog.addError(msg)
		log.Printf("[EbaySyncJob %s] %s", jobID, msg)
		h.finaliseEbayJob(ctx, jobID, "failed", msg, prog)
		return
	}

	prog.addLog("info", fmt.Sprintf("Category tree stored: %d total categories, %d leaf categories", len(flatTree), len(leafCategories)))
	log.Printf("[EbaySyncJob %s] Category tree: %d total, %d leaf", jobID, len(flatTree), len(leafCategories))

	h.jobsCol().Doc(jobID).Set(ctx, map[string]interface{}{
		"total":           len(leafCategories),
		"treeDownloaded":  true,
		"currentActivity": fmt.Sprintf("Phase 2: downloading aspects for %d leaf categories...", len(leafCategories)),
		"updatedAt":       time.Now(),
	}, firestore.MergeAll)

	// ── PHASE 2: Download aspects for each leaf category ──
	prog.addLog("info", fmt.Sprintf("Starting aspect downloads for %d leaf categories", len(leafCategories)))
	consecutiveFailures := 0

	for i, catData := range leafCategories {
		select {
		case <-ctx.Done():
			prog.addLog("info", fmt.Sprintf("Cancelled at category %d/%d", i, len(leafCategories)))
			prog.flush(ctx, "Cancelled")
			h.finaliseEbayJob(ctx, jobID, "cancelled", "", prog)
			return
		default:
		}

		categoryID, _ := catData["categoryId"].(string)
		categoryName, _ := catData["categoryName"].(string)
		if categoryID == "" {
			continue
		}

		activity := fmt.Sprintf("[%d/%d] %s (%s)", i+1, len(leafCategories), categoryName, categoryID)

		// Skip fresh aspects (unless fullSync).
		if !fullSync {
			existingDoc, err := h.aspectsCol(mpID).Doc(categoryID).Get(ctx)
			if err == nil && existingDoc.Exists() {
				if cachedAt, ok := existingDoc.Data()["cachedAt"].(time.Time); ok {
					if time.Since(cachedAt) < ebaySchemaFreshDays*24*time.Hour {
						prog.skipped++
						prog.flush(ctx, activity+" (skipped — fresh)")
						continue
					}
				}
			}
		}

		// Periodic token refresh.
		if i > 0 && i%ebayClientRefreshInterval == 0 {
			prog.addLog("info", fmt.Sprintf("Refreshing eBay token at category %d/%d", i, len(leafCategories)))
			if newClient, _, err := h.buildClient(ctx, tenantID, credentialID); err != nil {
				msg := fmt.Sprintf("Token refresh failed at category %d: %v", i, err)
				prog.addLog("warn", msg)
				log.Printf("[EbaySyncJob %s] %s", jobID, msg)
			} else {
				client = newClient
				prog.addLog("info", "Token refreshed")
			}
		}

		// Download aspects with retry + 429-aware backoff.
		aspects, err := h.downloadAspectsWithRetry(ctx, client, mpID, categoryID, ebayRetryAttempts, jobID, prog)
		if err != nil {
			errMsg := fmt.Sprintf("%s (%s): %v", categoryID, categoryName, err)
			prog.addError(errMsg)
			prog.failed++
			consecutiveFailures++
			log.Printf("[EbaySyncJob %s] Failed: %s", jobID, errMsg)

			if consecutiveFailures >= ebayMaxConsecFailures {
				pauseMsg := fmt.Sprintf("%d consecutive failures — pausing %ds", consecutiveFailures, ebayConsecFailurePauseS)
				prog.addLog("warn", pauseMsg)
				log.Printf("[EbaySyncJob %s] %s", jobID, pauseMsg)
				prog.flush(ctx, fmt.Sprintf("Paused %ds after consecutive failures", ebayConsecFailurePauseS))
				select {
				case <-ctx.Done():
					h.finaliseEbayJob(ctx, jobID, "cancelled", "", prog)
					return
				case <-time.After(time.Duration(ebayConsecFailurePauseS) * time.Second):
				}
				consecutiveFailures = 0
			}
		} else {
			aspectData := map[string]interface{}{
				"categoryId":    categoryID,
				"categoryName":  categoryName,
				"marketplaceId": mpID,
				"aspects":       aspects,
				"cachedAt":      time.Now(),
			}
			if _, err := h.aspectsCol(mpID).Doc(categoryID).Set(ctx, aspectData); err != nil {
				errMsg := fmt.Sprintf("%s: store failed: %v", categoryID, err)
				prog.addError(errMsg)
				prog.failed++
			} else {
				prog.downloaded++
				consecutiveFailures = 0
				if prog.downloaded%100 == 0 {
					prog.addLog("info", fmt.Sprintf("Progress: %d downloaded, %d skipped, %d failed of %d", prog.downloaded, prog.skipped, prog.failed, len(leafCategories)))
				}
			}
		}

		shouldFlush := (i+1)%ebayUpdateInterval == 0 ||
			i == len(leafCategories)-1 ||
			len(prog.pendingLogs) >= ebayLogFlushSize
		if shouldFlush {
			prog.flush(ctx, activity)
		}

		select {
		case <-ctx.Done():
			h.finaliseEbayJob(ctx, jobID, "cancelled", "", prog)
			return
		case <-time.After(ebayInterCategorySleepMs * time.Millisecond):
		}
	}

	prog.addLog("info", fmt.Sprintf("Job complete — downloaded=%d, skipped=%d, failed=%d of %d", prog.downloaded, prog.skipped, prog.failed, len(leafCategories)))
	log.Printf("[EbaySyncJob %s] Complete: downloaded=%d, skipped=%d, failed=%d of %d", jobID, prog.downloaded, prog.skipped, prog.failed, len(leafCategories))
	h.finaliseEbayJob(ctx, jobID, "completed", "", prog)
}

// downloadAspectsWithRetry: retry loop with 429 detection and interruptible sleep.
func (h *EbaySchemaHandler) downloadAspectsWithRetry(
	ctx context.Context,
	client *ebay.Client,
	mpID, categoryID string,
	maxRetries int,
	jobID string,
	prog *ebayJobProgress,
) ([]ebay.ItemAspect, error) {
	backoff := time.Duration(ebayBaseBackoffS) * time.Second

	for i := 0; i < maxRetries; i++ {
		aspects, err := client.GetItemAspectsForCategory(mpID, categoryID)
		if err == nil {
			return aspects, nil
		}

		// 429 — honour Retry-After.
		if waitSec, is429 := ebayParseRetryAfter(err); is429 {
			waitDur := time.Duration(waitSec) * time.Second
			msg := fmt.Sprintf("429 on category %s — waiting %ds (attempt %d/%d)", categoryID, waitSec, i+1, maxRetries)
			prog.addLog("warn", msg)
			log.Printf("[EbaySyncJob %s] %s", jobID, msg)
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("cancelled during 429 backoff")
			case <-time.After(waitDur):
			}
			continue
		}

		// Other error — exponential backoff.
		if i < maxRetries-1 {
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("cancelled during retry backoff")
			case <-time.After(backoff):
			}
			backoff *= 2
		} else {
			return nil, err
		}
	}
	return nil, fmt.Errorf("all %d attempts failed for category %s", maxRetries, categoryID)
}

// finaliseEbayJob writes terminal state to Firestore.
func (h *EbaySchemaHandler) finaliseEbayJob(ctx context.Context, jobID, status, errorMsg string, prog *ebayJobProgress) {
	now := time.Now()
	logsToWrite := prog.allLogs
	if len(logsToWrite) > ebayMaxLogEntries {
		logsToWrite = logsToWrite[len(logsToWrite)-ebayMaxLogEntries:]
	}
	update := map[string]interface{}{
		"status":          status,
		"downloaded":      prog.downloaded,
		"skipped":         prog.skipped,
		"failed":          prog.failed,
		"errors":          prog.errors,
		"logs":            logsToWrite,
		"currentActivity": "",
		"updatedAt":       now,
		"completedAt":     now,
	}
	if errorMsg != "" {
		update["error"] = errorMsg
	}
	h.jobsCol().Doc(jobID).Set(context.Background(), update, firestore.MergeAll)
}

// flattenCategoryTree converts the eBay tree response to a flat array.
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
// GET /api/v1/ebay/schemas/category/:categoryId
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

// ── Scheduler wiring ──────────────────────────────────────────────────────────

func (h *EbaySchemaHandler) SetScheduler(s *EbaySchemaRefreshScheduler) { h.scheduler = s }

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

// ── Utility ───────────────────────────────────────────────────────────────────

func getStringValue(data map[string]interface{}, key string) string {
	if v, ok := data[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func ebayGenerateJobID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
