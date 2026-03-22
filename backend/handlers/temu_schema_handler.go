package handlers

// ============================================================================
// TEMU SCHEMA CACHE HANDLER (v2 — streaming walk + checkpoint/resume)
// ============================================================================
// Changes from v1:
//   - Tree walk writes each category to Firestore immediately (no RAM accumulation)
//   - Categories stored as individual docs in categories/ subcollection
//   - Job stores last_cat_id checkpoint — survives Cloud Run instance restarts
//   - Job total counter increments in real-time as leaf categories are discovered
//   - Improved progress logging: logs current category name during walk
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
	"google.golang.org/api/iterator"
	"module-a/marketplace/clients/temu"
	"module-a/repository"
	"module-a/services"
)

type TemuSchemaHandler struct {
	marketplaceService *services.MarketplaceService
	repo               *repository.MarketplaceRepository
	fsClient           *firestore.Client

	activeJobs   map[string]context.CancelFunc
	activeJobsMu sync.Mutex

	// USP-04: auto-refresh scheduler (injected after construction)
	scheduler *TemuSchemaRefreshScheduler
}

func NewTemuSchemaHandler(
	marketplaceService *services.MarketplaceService,
	repo *repository.MarketplaceRepository,
	fsClient *firestore.Client,
) *TemuSchemaHandler {
	return &TemuSchemaHandler{
		marketplaceService: marketplaceService,
		repo:               repo,
		fsClient:           fsClient,
		activeJobs:         make(map[string]context.CancelFunc),
	}
}

// ── Firestore path helpers ──────────────────────────────────────────────────

func (h *TemuSchemaHandler) templatesCol() *firestore.CollectionRef {
	return h.fsClient.Collection("marketplaces").Doc("Temu").Collection("templates")
}

// categoriesCol stores one document per category
func (h *TemuSchemaHandler) categoriesCol() *firestore.CollectionRef {
	return h.fsClient.Collection("marketplaces").Doc("Temu").Collection("categories")
}

// categoriesMetaDoc stores tree-level stats
func (h *TemuSchemaHandler) categoriesMetaDoc() *firestore.DocumentRef {
	return h.fsClient.Collection("marketplaces").Doc("Temu").Collection("meta").Doc("categories")
}

func (h *TemuSchemaHandler) jobsCol() *firestore.CollectionRef {
	return h.fsClient.Collection("marketplaces").Doc("Temu").Collection("schema_jobs")
}

// isCancelledInFirestore reads the job document and returns true if status is "cancelled".
// This is used by long-running goroutines to detect cancellation across Cloud Run instances,
// since in-memory context cancellation only works on the same instance that started the job.
func (h *TemuSchemaHandler) isCancelledInFirestore(jobID string) bool {
	doc, err := h.jobsCol().Doc(jobID).Get(context.Background())
	if err != nil {
		return false
	}
	status, _ := doc.Data()["status"].(string)
	return status == "cancelled"
}

// ── Temu client helpers ─────────────────────────────────────────────────────

func (h *TemuSchemaHandler) getClient(c *gin.Context) (*temu.Client, string, string, error) {
	tenantID := c.GetString("tenant_id")
	credentialID := c.Query("credential_id")

	if credentialID == "" {
		creds, err := h.repo.ListCredentials(context.Background(), tenantID)
		if err != nil {
			return nil, "", "", fmt.Errorf("list credentials: %w", err)
		}
		for _, cred := range creds {
			if cred.Channel == "temu" && cred.Active {
				credentialID = cred.CredentialID
				break
			}
		}
		if credentialID == "" {
			return nil, "", "", fmt.Errorf("no active Temu credential found — please check Marketplace Connections")
		}
	}

	client, err := h.buildClient(context.Background(), tenantID, credentialID)
	return client, tenantID, credentialID, err
}

func (h *TemuSchemaHandler) buildClient(ctx context.Context, tenantID, credentialID string) (*temu.Client, error) {
	cred, err := h.repo.GetCredential(ctx, tenantID, credentialID)
	if err != nil {
		return nil, fmt.Errorf("get credential: %w", err)
	}
	mergedCreds, err := h.marketplaceService.GetFullCredentials(ctx, cred)
	if err != nil {
		return nil, fmt.Errorf("merge credentials: %w", err)
	}
	baseURL := mergedCreds["base_url"]
	if baseURL == "" {
		baseURL = "https://openapi-b-eu.temu.com/openapi/router"
	}
	return temu.NewClient(baseURL, mergedCreds["app_key"], mergedCreds["app_secret"], mergedCreds["access_token"]), nil
}

// ============================================================================
// GET /api/v1/temu/schemas/list
// ============================================================================

type TemplateListItem struct {
	CatID         int       `json:"catId"`
	CatName       string    `json:"catName"`
	PropertyCount int       `json:"propertyCount"`
	CachedAt      time.Time `json:"cachedAt"`
}

func (h *TemuSchemaHandler) ListSchemas(c *gin.Context) {
	docs, err := h.templatesCol().Documents(c.Request.Context()).GetAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	items := make([]TemplateListItem, 0, len(docs))
	for _, doc := range docs {
		data := doc.Data()
		item := TemplateListItem{
			CatID:   getIntValue(data, "catId"),
			CatName: getStrValue(data, "catName"),
		}
		if props, ok := data["goodsProperties"]; ok {
			if arr, ok := props.([]interface{}); ok {
				item.PropertyCount = len(arr)
			}
		}
		if t, ok := data["cachedAt"]; ok {
			if ts, ok := t.(time.Time); ok {
				item.CachedAt = ts
			}
		}
		items = append(items, item)
	}
	c.JSON(http.StatusOK, gin.H{"schemas": items, "count": len(items)})
}

// ============================================================================
// GET /api/v1/temu/schemas/stats
// ============================================================================

func (h *TemuSchemaHandler) Stats(c *gin.Context) {
	ctx := c.Request.Context()

	totalCategories := 0
	leafCategories := 0
	lastTreeSync := time.Time{}

	metaDoc, err := h.categoriesMetaDoc().Get(ctx)
	if err == nil && metaDoc.Exists() {
		data := metaDoc.Data()
		if v, ok := data["totalCount"].(int64); ok {
			totalCategories = int(v)
		} else if v, ok := data["totalCount"].(float64); ok {
			totalCategories = int(v)
		}
		if v, ok := data["leafCount"].(int64); ok {
			leafCategories = int(v)
		} else if v, ok := data["leafCount"].(float64); ok {
			leafCategories = int(v)
		}
		if t, ok := data["cachedAt"].(time.Time); ok {
			lastTreeSync = t
		}
	} else {
		// Fall back to counting individual category docs
		iter := h.categoriesCol().Documents(ctx)
		for {
			doc, iterErr := iter.Next()
			if iterErr == iterator.Done {
				break
			}
			if iterErr != nil {
				break
			}
			totalCategories++
			data := doc.Data()
			if leaf, ok := data["leaf"].(bool); ok && leaf {
				leafCategories++
			}
		}
	}

	cachedTemplates := 0
	tIter := h.templatesCol().Documents(ctx)
	for {
		_, iterErr := tIter.Next()
		if iterErr == iterator.Done {
			break
		}
		if iterErr != nil {
			break
		}
		cachedTemplates++
	}

	c.JSON(http.StatusOK, gin.H{
		"totalCategories": totalCategories,
		"leafCategories":  leafCategories,
		"cachedTemplates": cachedTemplates,
		"lastSync":        lastTreeSync,
		"cachePercentage": temuCalculatePercentage(cachedTemplates, leafCategories),
	})
}

func temuCalculatePercentage(cached, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(cached) / float64(total) * 100
}

// ============================================================================
// POST /api/v1/temu/schemas/sync
// ============================================================================

type TemuSyncRequest struct {
	FullSync     bool   `json:"fullSync"`
	CredentialId string `json:"credentialId"`
}

func (h *TemuSchemaHandler) Sync(c *gin.Context) {
	var req TemuSyncRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Allow credential override from request body
	if req.CredentialId != "" {
		c.Request.URL.RawQuery = "credential_id=" + req.CredentialId
	}

	client, tenantID, credentialID, err := h.getClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	jobID := temuGenerateJobID()
	now := time.Now()

	jobData := map[string]interface{}{
		"jobId":          jobID,
		"status":         "running",
		"fullSync":       req.FullSync,
		"startedAt":      now,
		"updatedAt":      now,
		"downloaded":     0,
		"skipped":        0,
		"failed":         0,
		"total":          0,
		"leafFound":      0,
		"treeWalkDone":   false,
		"lastCatId":      0,
		"currentCatName": "",
		"errors":         []string{},
	}

	if _, err := h.jobsCol().Doc(jobID).Set(context.Background(), jobData); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("create job: %v", err)})
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	h.activeJobsMu.Lock()
	h.activeJobs[jobID] = cancel
	h.activeJobsMu.Unlock()

	go h.runSync(ctx, jobID, client, tenantID, credentialID, req.FullSync)

	c.JSON(http.StatusOK, gin.H{"ok": true, "jobId": jobID, "status": "started"})
}

// ============================================================================
// POST /api/v1/temu/schemas/resume
// ============================================================================
// Skips the tree walk entirely by reading leaf categories already stored in
// Firestore from a previous (failed) sync. Use this when a sync crashed
// during the tree walk phase but categories/ subcollection is populated.

func (h *TemuSchemaHandler) Resume(c *gin.Context) {
	client, tenantID, credentialID, err := h.getClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Read all leaf categories from the categories/ subcollection
	log.Printf("[TemuResume] Reading leaf categories from Firestore...")
	ctx := context.Background()

	iter := h.categoriesCol().Where("leaf", "==", true).Documents(ctx)
	defer iter.Stop()

	var leafCatIDs []int
	leafCatNames := map[int]string{}

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("reading categories: %v", err)})
			return
		}
		data := doc.Data()
		catID := getIntValue(data, "catId")
		catName := getStrValue(data, "catName")
		if catID > 0 {
			leafCatIDs = append(leafCatIDs, catID)
			leafCatNames[catID] = catName
		}
	}

	if len(leafCatIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "no leaf categories found in Firestore — run a full sync first",
		})
		return
	}

	log.Printf("[TemuResume] Found %d leaf categories — starting template download phase", len(leafCatIDs))

	// Create a fresh job, marked as tree walk already done
	jobID := temuGenerateJobID()
	now := time.Now()
	jobData := map[string]interface{}{
		"jobId":          jobID,
		"status":         "running",
		"fullSync":       false,
		"startedAt":      now,
		"updatedAt":      now,
		"downloaded":     0,
		"skipped":        0,
		"failed":         0,
		"total":          len(leafCatIDs),
		"leafFound":      len(leafCatIDs),
		"treeWalkDone":   true, // key flag — skip straight to Phase 2
		"lastCatId":      0,
		"currentCatName": fmt.Sprintf("Resumed — %d categories loaded from cache", len(leafCatIDs)),
		"errors":         []string{},
		"resumedFrom":    "categories_subcollection",
	}

	if _, err := h.jobsCol().Doc(jobID).Set(ctx, jobData); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("create job: %v", err)})
		return
	}

	jobCtx, cancel := context.WithCancel(context.Background())
	h.activeJobsMu.Lock()
	h.activeJobs[jobID] = cancel
	h.activeJobsMu.Unlock()

	// Jump straight to Phase 2 — pass the pre-loaded leaf data
	go h.runTemplateDownload(jobCtx, jobID, client, tenantID, credentialID, leafCatIDs, leafCatNames)

	c.JSON(http.StatusOK, gin.H{
		"ok":            true,
		"jobId":         jobID,
		"status":        "started",
		"leafCount":     len(leafCatIDs),
		"message":       fmt.Sprintf("Resuming template download for %d categories (tree walk skipped)", len(leafCatIDs)),
	})
}

// ============================================================================
// runSync — streaming walk + checkpoint
// ============================================================================

func (h *TemuSchemaHandler) runSync(ctx context.Context, jobID string, client *temu.Client, tenantID, credentialID string, fullSync bool) {
	defer func() {
		h.activeJobsMu.Lock()
		delete(h.activeJobs, jobID)
		h.activeJobsMu.Unlock()
	}()

	log.Printf("[TemuSyncJob %s] Starting sync (fullSync=%v)", jobID, fullSync)

	leafFound := 0
	errors := []string{}
	const maxErrors = 100

	// ── PHASE 1: Stream-walk category tree ──────────────────────────────────
	log.Printf("[TemuSyncJob %s] Walking category tree (streaming to Firestore)...", jobID)

	leafCatIDs := []int{}
	leafCatNames := map[int]string{}

	var walkTree func(parentID *int, level int) error
	walkTree = func(parentID *int, level int) error {
		select {
		case <-ctx.Done():
			return fmt.Errorf("cancelled")
		default:
		}

		cats, err := client.GetCategories(parentID)
		if err != nil {
			return fmt.Errorf("get categories (parent=%v): %w", parentID, err)
		}

		for _, cat := range cats {
			select {
			case <-ctx.Done():
				return fmt.Errorf("cancelled")
			default:
			}

			cat.Level = level

			// Write category to Firestore immediately — no memory accumulation
			catDocData := map[string]interface{}{
				"catId":    cat.CatID,
				"catName":  cat.CatName,
				"parentId": cat.ParentID,
				"leaf":     cat.Leaf,
				"level":    cat.Level,
				"cachedAt": time.Now(),
			}
			catIDStr := fmt.Sprintf("%d", cat.CatID)
			h.categoriesCol().Doc(catIDStr).Set(context.Background(), catDocData)

			if cat.Leaf {
				leafFound++
				leafCatIDs = append(leafCatIDs, cat.CatID)
				leafCatNames[cat.CatID] = cat.CatName

				// Update job live every 25 leaves found
				if leafFound%25 == 0 {
					// Check Firestore for cancellation (works across Cloud Run instances)
					if h.isCancelledInFirestore(jobID) {
						log.Printf("[TemuSyncJob %s] Cancellation detected via Firestore at leaf %d", jobID, leafFound)
						return fmt.Errorf("cancelled")
					}
					h.jobsCol().Doc(jobID).Set(context.Background(), map[string]interface{}{
						"leafFound":      leafFound,
						"total":          leafFound,
						"currentCatName": fmt.Sprintf("Walking: %s (level %d)", cat.CatName, level),
						"updatedAt":      time.Now(),
					}, firestore.MergeAll)
					log.Printf("[TemuSyncJob %s] Tree walk: %d leaves found (current: %s)", jobID, leafFound, cat.CatName)
				}
			}

			if !cat.Leaf {
				childParentID := cat.CatID
				if err := walkTree(&childParentID, level+1); err != nil {
					return err
				}
			}

			time.Sleep(100 * time.Millisecond)
		}
		return nil
	}

	if err := walkTree(nil, 0); err != nil {
		if err.Error() == "cancelled" {
			h.markJobCancelled(jobID, 0, 0, 0, leafFound, errors)
			return
		}
		errMsg := fmt.Sprintf("Failed to walk category tree: %v", err)
		log.Printf("[TemuSyncJob %s] %s", jobID, errMsg)
		h.jobsCol().Doc(jobID).Set(context.Background(), map[string]interface{}{
			"status":      "failed",
			"error":       errMsg,
			"updatedAt":   time.Now(),
			"completedAt": time.Now(),
		}, firestore.MergeAll)
		return
	}

	log.Printf("[TemuSyncJob %s] Tree walk complete: %d leaf categories", jobID, leafFound)

	// Abort if the tree walk produced nothing — this means GetCategories returned
	// empty results (API error, bad credentials, proxy issue etc). Do NOT proceed
	// to Phase 2 with an empty slice — it would silently "complete" with 0 downloads
	// and leave the categories collection empty with no diagnostic information.
	if leafFound == 0 {
		errMsg := "Tree walk completed but found 0 leaf categories — check Temu API credentials, egress proxy, and Cloud Run logs for GetCategories errors"
		log.Printf("[TemuSyncJob %s] ABORTING: %s", jobID, errMsg)
		h.jobsCol().Doc(jobID).Set(context.Background(), map[string]interface{}{
			"status":      "failed",
			"error":       errMsg,
			"updatedAt":   time.Now(),
			"completedAt": time.Now(),
		}, firestore.MergeAll)
		return
	}

	// Update meta doc — write BOTH leafCount and totalCount so Stats() returns correct values
	h.categoriesMetaDoc().Set(context.Background(), map[string]interface{}{
		"leafCount":  leafFound,
		"totalCount": leafFound,
		"cachedAt":   time.Now(),
	}, firestore.MergeAll)

	h.jobsCol().Doc(jobID).Set(context.Background(), map[string]interface{}{
		"treeWalkDone":   true,
		"total":          leafFound,
		"leafFound":      leafFound,
		"currentCatName": "Tree walk complete — downloading templates...",
		"updatedAt":      time.Now(),
	}, firestore.MergeAll)

	// ── PHASE 2: hand off to shared download runner ──────────────────────────
	h.runTemplateDownload(ctx, jobID, client, tenantID, credentialID, leafCatIDs, leafCatNames)
}

// ============================================================================
// runTemplateDownload — Phase 2 shared by runSync and Resume
// ============================================================================
// Downloads templates for the given leaf category IDs, writing a checkpoint
// to Firestore every 10 categories. Skips categories cached within 7 days
// (unless the original sync was fullSync — note: fullSync flag not passed here
// because Resume always runs incremental to avoid re-downloading fresh data).

func (h *TemuSchemaHandler) runTemplateDownload(
	ctx context.Context,
	jobID string,
	client *temu.Client,
	tenantID, credentialID string,
	leafCatIDs []int,
	leafCatNames map[int]string,
) {
	log.Printf("[TemuSyncJob %s] Downloading templates for %d leaf categories...", jobID, len(leafCatIDs))

	downloaded := 0
	skipped := 0
	failed := 0
	errors := []string{}
	const maxErrors = 100
	consecutiveFailures := 0
	const maxConsecutiveFailures = 10
	const updateInterval = 10

	for i, catID := range leafCatIDs {
		select {
		case <-ctx.Done():
			h.markJobCancelled(jobID, downloaded, skipped, failed, len(leafCatIDs), errors)
			return
		default:
		}

		catName := leafCatNames[catID]
		catIDStr := fmt.Sprintf("%d", catID)

		// Skip templates cached within the last 7 days (incremental behaviour)
		existingDoc, err := h.templatesCol().Doc(catIDStr).Get(ctx)
		if err == nil && existingDoc.Exists() {
			data := existingDoc.Data()
			if cachedAt, ok := data["cachedAt"].(time.Time); ok {
				if time.Since(cachedAt) < 7*24*time.Hour {
					skipped++
					continue
				}
			}
		}

		// Refresh client every 50 categories to avoid stale tokens
		if i > 0 && i%50 == 0 {
			if newClient, err := h.buildClient(ctx, tenantID, credentialID); err == nil {
				client = newClient
			}
		}

		template, err := h.downloadTemplateWithRetry(ctx, client, catID, 3)
		if err != nil {
			errMsg := fmt.Sprintf("%d (%s): %v", catID, catName, err)
			if len(errors) < maxErrors {
				errors = append(errors, errMsg)
			}
			failed++
			consecutiveFailures++
			log.Printf("[TemuSyncJob %s] Template failed: %s", jobID, errMsg)
			if consecutiveFailures >= maxConsecutiveFailures {
				log.Printf("[TemuSyncJob %s] %d consecutive failures — pausing 30s...", jobID, consecutiveFailures)
				time.Sleep(30 * time.Second)
				consecutiveFailures = 0
			}
		} else {
			var goodsProperties interface{}
			if props, ok := template["goodsProperties"]; ok {
				goodsProperties = props
			} else if result, ok := template["result"].(map[string]interface{}); ok {
				goodsProperties = result["goodsProperties"]
			}

			templateData := map[string]interface{}{
				"catId":           catID,
				"catName":         catName,
				"goodsProperties": goodsProperties,
				"rawTemplate":     template,
				"cachedAt":        time.Now(),
			}
			if _, err := h.templatesCol().Doc(catIDStr).Set(context.Background(), templateData); err != nil {
				if len(errors) < maxErrors {
					errors = append(errors, fmt.Sprintf("%d: store failed: %v", catID, err))
				}
				failed++
			} else {
				downloaded++
				consecutiveFailures = 0
			}
		}

		// Write checkpoint + progress every 10 categories
		if (i+1)%updateInterval == 0 || i == len(leafCatIDs)-1 {
			// Check Firestore for cancellation (works across Cloud Run instances)
			if h.isCancelledInFirestore(jobID) {
				log.Printf("[TemuSyncJob %s] Cancellation detected via Firestore at template %d/%d", jobID, i+1, len(leafCatIDs))
				h.markJobCancelled(jobID, downloaded, skipped, failed, len(leafCatIDs), errors)
				return
			}
			h.jobsCol().Doc(jobID).Set(context.Background(), map[string]interface{}{
				"downloaded":     downloaded,
				"skipped":        skipped,
				"failed":         failed,
				"errors":         errors,
				"lastCatId":      catID,
				"currentCatName": fmt.Sprintf("Downloading: %s (%d/%d)", catName, i+1, len(leafCatIDs)),
				"updatedAt":      time.Now(),
			}, firestore.MergeAll)
		}

		time.Sleep(100 * time.Millisecond)
	}

	now := time.Now()
	h.jobsCol().Doc(jobID).Set(context.Background(), map[string]interface{}{
		"status":         "completed",
		"downloaded":     downloaded,
		"skipped":        skipped,
		"failed":         failed,
		"errors":         errors,
		"lastCatId":      0,
		"currentCatName": "",
		"updatedAt":      now,
		"completedAt":    now,
	}, firestore.MergeAll)

	log.Printf("[TemuSyncJob %s] Completed: %d downloaded, %d skipped, %d failed out of %d", jobID, downloaded, skipped, failed, len(leafCatIDs))
}

func (h *TemuSchemaHandler) markJobCancelled(jobID string, downloaded, skipped, failed, leafFound int, errors []string) {
	now := time.Now()
	h.jobsCol().Doc(jobID).Set(context.Background(), map[string]interface{}{
		"status":      "cancelled",
		"downloaded":  downloaded,
		"skipped":     skipped,
		"failed":      failed,
		"leafFound":   leafFound,
		"errors":      errors,
		"updatedAt":   now,
		"completedAt": now,
	}, firestore.MergeAll)
}

func (h *TemuSchemaHandler) downloadTemplateWithRetry(ctx context.Context, client *temu.Client, catID int, maxRetries int) (map[string]interface{}, error) {
	var lastErr error
	backoff := 2 * time.Second
	for i := 0; i < maxRetries; i++ {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("cancelled")
		default:
		}
		template, err := client.GetTemplate(catID)
		if err == nil {
			return template, nil
		}
		lastErr = err
		if i < maxRetries-1 {
			time.Sleep(backoff)
			backoff *= 2
		}
	}
	return nil, lastErr
}

// ============================================================================
// GET /api/v1/temu/schemas/jobs
// ============================================================================

func (h *TemuSchemaHandler) ListJobs(c *gin.Context) {
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
// GET /api/v1/temu/schemas/jobs/:jobId
// ============================================================================

func (h *TemuSchemaHandler) GetJobStatus(c *gin.Context) {
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
// POST /api/v1/temu/schemas/jobs/:jobId/cancel
// ============================================================================

func (h *TemuSchemaHandler) CancelJob(c *gin.Context) {
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
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to cancel: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "jobId": jobID, "status": "cancelled", "message": "job cancelled successfully"})
}

// ============================================================================
// GET /api/v1/temu/schemas/template/:catId
// ============================================================================

func (h *TemuSchemaHandler) GetTemplate(c *gin.Context) {
	catID := c.Param("catId")
	doc, err := h.templatesCol().Doc(catID).Get(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("template not found: %v", err)})
		return
	}
	c.JSON(http.StatusOK, doc.Data())
}

// ── Utility ─────────────────────────────────────────────────────────────────

func getStrValue(data map[string]interface{}, key string) string {
	if v, ok := data[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getIntValue(data map[string]interface{}, key string) int {
	if v, ok := data[key]; ok {
		switch val := v.(type) {
		case int:
			return val
		case int64:
			return int(val)
		case float64:
			return int(val)
		}
	}
	return 0
}

func temuGenerateJobID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// ============================================================================
// AUTO-REFRESH SETTINGS ENDPOINTS — USP-04
// ============================================================================
// SetScheduler must be called before GetRefreshSettings / SaveRefreshSettings.

func (h *TemuSchemaHandler) SetScheduler(s *TemuSchemaRefreshScheduler) {
	h.scheduler = s
}

// GET /api/v1/temu/schemas/refresh-settings
func (h *TemuSchemaHandler) GetRefreshSettings(c *gin.Context) {
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

// PUT /api/v1/temu/schemas/refresh-settings
func (h *TemuSchemaHandler) SaveRefreshSettings(c *gin.Context) {
	if h.scheduler == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "scheduler not initialised"})
		return
	}
	tenantID := c.GetString("tenant_id")
	var settings TemuSchemaRefreshSettings
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
