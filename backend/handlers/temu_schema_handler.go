package handlers

// ============================================================================
// TEMU SCHEMA CACHE HANDLER  (rearchitected)
// ============================================================================
// Endpoints:
//   GET    /schemas/list                        — List cached templates
//   GET    /schemas/stats                       — Cache statistics
//   POST   /schemas/sync                        — Background: walk full tree + download templates
//   POST   /schemas/sync-missing-roots          — Walk only root branches not yet in Firestore
//   POST   /schemas/resume                      — Download templates for categories already in Firestore
//   GET    /schemas/jobs                        — List sync jobs
//   GET    /schemas/jobs/:jobId                 — Get job status + progress
//   POST   /schemas/jobs/:jobId/cancel          — Cancel a running job
//   GET    /schemas/template/:catId             — Get cached template for one category
//   GET    /schemas/refresh-settings            — USP-04: auto-refresh settings
//   PUT    /schemas/refresh-settings            — USP-04: save auto-refresh settings
//
// Firestore:
//   marketplaces/Temu/templates/{catId}
//   marketplaces/Temu/categories/{catId}
//   marketplaces/Temu/meta/categories
//   marketplaces/Temu/schema_jobs/{jobId}
//
// Architecture changes vs previous version:
//   - 429 detection on GetCategories and GetTemplate calls.
//   - Structured log entries written to Firestore job document.
//   - currentActivity updated every leaf category in Phase 1 (walk) and
//     every template in Phase 2 (download).
//   - All time.Sleep calls wrapped in select + ctx.Done for fast cancellation.
//   - isCancelledInFirestore poll removed — relying entirely on ctx + Firestore
//     cancel is now consistent with Amazon and eBay handlers.
//   - Silent zero-leaf abort produces a detailed log entry explaining the cause.
//   - Cycle detection: visited map prevents Temu API cyclic references from
//     causing infinite recursion (observed in production at depth 400+).
//   - Hard depth limit (temuMaxTreeDepth=15) as secondary cycle guard.
//   - sync-missing-roots: seeds visited map from existing Firestore categories
//     so only unvisited root branches are walked, preserving existing data.
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
	"google.golang.org/api/iterator"
	"module-a/marketplace/clients/temu"
	"module-a/repository"
	"module-a/services"
)

// ── Constants ─────────────────────────────────────────────────────────────────

const (
	temuSchemaFreshDays       = 7    // skip template if cached within 7 days
	temuClientRefreshInterval = 50   // rebuild Temu client every N templates
	temuUpdateInterval        = 10   // flush progress every N templates (Phase 2)
	temuLogFlushSize          = 10   // also flush when pending log buffer hits this
	temuMaxErrors             = 100
	temuMaxLogEntries         = 200
	temuTreeNodeSleepMs       = 250  // ms between tree walk nodes (~4 req/s)
	temuInterTemplateSleepMs  = 100  // ms between template downloads
	temuMaxConsecFailures     = 10
	temuConsecFailurePauseS   = 60
	temuRetryAttempts         = 3
	temuBaseBackoffS          = 2
	temu429DefaultWaitS       = 30
	temu429MaxWaitS           = 120
	temuTreeCallTimeoutS      = 20  // per-call deadline for GetCategoriesCtx
	temuLeafFlushInterval     = 25  // flush tree-walk progress every N leaves found
	temuMaxTreeDepth          = 15  // abort branch if deeper — real Temu tree is max ~6 levels
)

// ── Types ─────────────────────────────────────────────────────────────────────

type TemuSchemaHandler struct {
	marketplaceService *services.MarketplaceService
	repo               *repository.MarketplaceRepository
	fsClient           *firestore.Client

	activeJobs   map[string]context.CancelFunc
	activeJobsMu sync.Mutex

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

// ── Firestore helpers ─────────────────────────────────────────────────────────

func (h *TemuSchemaHandler) templatesCol() *firestore.CollectionRef {
	return h.fsClient.Collection("marketplaces").Doc("Temu").Collection("templates")
}

func (h *TemuSchemaHandler) categoriesCol() *firestore.CollectionRef {
	return h.fsClient.Collection("marketplaces").Doc("Temu").Collection("categories")
}

func (h *TemuSchemaHandler) categoriesMetaDoc() *firestore.DocumentRef {
	return h.fsClient.Collection("marketplaces").Doc("Temu").Collection("meta").Doc("categories")
}

func (h *TemuSchemaHandler) jobsCol() *firestore.CollectionRef {
	return h.fsClient.Collection("marketplaces").Doc("Temu").Collection("schema_jobs")
}

// ── Client helpers ────────────────────────────────────────────────────────────

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

// ── 429 helper ────────────────────────────────────────────────────────────────

func temuParseRetryAfter(err error) (waitSec int, is429 bool) {
	if err == nil {
		return 0, false
	}
	s := err.Error()
	if !strings.Contains(s, "429") && !strings.Contains(s, "rate limit") && !strings.Contains(s, "too many requests") {
		return 0, false
	}
	for _, part := range strings.Fields(s) {
		if n, parseErr := strconv.Atoi(strings.TrimRight(part, "s")); parseErr == nil && n > 0 && n <= temu429MaxWaitS {
			return n, true
		}
	}
	return temu429DefaultWaitS, true
}

// ── Log / progress writer ─────────────────────────────────────────────────────

type temuLogEntry struct {
	T   string `json:"t"`
	Msg string `json:"msg"`
	Lvl string `json:"lvl"`
}

type temuJobProgress struct {
	jobsCol     *firestore.CollectionRef
	jobID       string
	downloaded  int
	skipped     int
	failed      int
	leafFound   int
	errors      []string
	pendingLogs []temuLogEntry
	allLogs     []temuLogEntry
	lastFlush   time.Time
}

func (p *temuJobProgress) addLog(level, msg string) {
	entry := temuLogEntry{T: time.Now().Format(time.RFC3339), Msg: msg, Lvl: level}
	p.pendingLogs = append(p.pendingLogs, entry)
	p.allLogs = append(p.allLogs, entry)
	if len(p.allLogs) > temuMaxLogEntries {
		p.allLogs = p.allLogs[len(p.allLogs)-temuMaxLogEntries:]
	}
}

func (p *temuJobProgress) addError(msg string) {
	if len(p.errors) < temuMaxErrors {
		p.errors = append(p.errors, msg)
	}
	p.addLog("error", msg)
}

func (p *temuJobProgress) flush(ctx context.Context, currentActivity string) {
	if len(p.pendingLogs) == 0 && time.Since(p.lastFlush) < 2*time.Second {
		return
	}
	logsToWrite := p.allLogs
	if len(logsToWrite) > temuMaxLogEntries {
		logsToWrite = logsToWrite[len(logsToWrite)-temuMaxLogEntries:]
	}
	p.jobsCol.Doc(p.jobID).Set(ctx, map[string]interface{}{
		"downloaded":      p.downloaded,
		"skipped":         p.skipped,
		"failed":          p.failed,
		"leafFound":       p.leafFound,
		"total":           p.leafFound,
		"errors":          p.errors,
		"logs":            logsToWrite,
		"currentActivity": currentActivity,
		"updatedAt":       time.Now(),
	}, firestore.MergeAll)
	p.pendingLogs = p.pendingLogs[:0]
	p.lastFlush = time.Now()
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
		item := TemplateListItem{CatID: getIntValue(data, "catId"), CatName: getStrValue(data, "catName")}
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
			if leaf, ok := doc.Data()["leaf"].(bool); ok && leaf {
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
		"jobId":           jobID,
		"status":          "running",
		"fullSync":        req.FullSync,
		"startedAt":       now,
		"updatedAt":       now,
		"downloaded":      0,
		"skipped":         0,
		"failed":          0,
		"total":           0,
		"leafFound":       0,
		"treeWalkDone":    false,
		"lastCatId":       0,
		"currentActivity": "Initialising...",
		"errors":          []string{},
		"logs":            []temuLogEntry{},
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

func (h *TemuSchemaHandler) Resume(c *gin.Context) {
	client, tenantID, credentialID, err := h.getClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := context.Background()
	log.Printf("[TemuResume] Reading leaf categories from Firestore...")

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
		c.JSON(http.StatusBadRequest, gin.H{"error": "no leaf categories found in Firestore — run a full sync first"})
		return
	}

	jobID := temuGenerateJobID()
	now := time.Now()
	jobData := map[string]interface{}{
		"jobId":           jobID,
		"status":          "running",
		"fullSync":        false,
		"startedAt":       now,
		"updatedAt":       now,
		"downloaded":      0,
		"skipped":         0,
		"failed":          0,
		"total":           len(leafCatIDs),
		"leafFound":       len(leafCatIDs),
		"treeWalkDone":    true,
		"lastCatId":       0,
		"currentActivity": fmt.Sprintf("Resuming — %d categories loaded from cache", len(leafCatIDs)),
		"errors":          []string{},
		"logs":            []temuLogEntry{},
		"resumedFrom":     "categories_subcollection",
	}
	if _, err := h.jobsCol().Doc(jobID).Set(ctx, jobData); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("create job: %v", err)})
		return
	}

	jobCtx, cancel := context.WithCancel(context.Background())
	h.activeJobsMu.Lock()
	h.activeJobs[jobID] = cancel
	h.activeJobsMu.Unlock()

	go h.runTemplateDownload(jobCtx, jobID, client, tenantID, credentialID, leafCatIDs, leafCatNames)

	c.JSON(http.StatusOK, gin.H{
		"ok":        true,
		"jobId":     jobID,
		"status":    "started",
		"leafCount": len(leafCatIDs),
		"message":   fmt.Sprintf("Resuming template download for %d categories (tree walk skipped)", len(leafCatIDs)),
	})
}

// ============================================================================
// POST /api/v1/temu/schemas/sync-missing-roots
// ============================================================================
// Walks only the root-level branches of the Temu category tree that have
// no children yet saved in Firestore. This is used to pick up root categories
// that were not reached in a previous walk that got stuck in a cycle.
//
// Strategy:
//   1. Fetch all root categories from the Temu API (parentID = nil).
//   2. For each root, check whether any category doc with that parentId exists
//      in Firestore. If yes, the branch was already walked — skip it.
//   3. Walk only the unvisited roots, with full cycle detection seeded from
//      every catId already in Firestore (so cross-branch cycles are also caught).
//   4. Write new leaves to the categories collection alongside existing ones.
//   5. After completion, hit Resume to download templates for everything.

func (h *TemuSchemaHandler) SyncMissingRoots(c *gin.Context) {
	client, tenantID, credentialID, err := h.getClient(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	jobID := temuGenerateJobID()
	now := time.Now()
	jobData := map[string]interface{}{
		"jobId":           jobID,
		"status":          "running",
		"fullSync":        false,
		"startedAt":       now,
		"updatedAt":       now,
		"downloaded":      0,
		"skipped":         0,
		"failed":          0,
		"total":           0,
		"leafFound":       0,
		"treeWalkDone":    false,
		"lastCatId":       0,
		"currentActivity": "Loading existing categories from Firestore...",
		"errors":          []string{},
		"logs":            []temuLogEntry{},
		"jobType":         "sync-missing-roots",
	}
	if _, err := h.jobsCol().Doc(jobID).Set(context.Background(), jobData); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("create job: %v", err)})
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	h.activeJobsMu.Lock()
	h.activeJobs[jobID] = cancel
	h.activeJobsMu.Unlock()

	go h.runSyncMissingRoots(ctx, jobID, client, tenantID, credentialID)
	c.JSON(http.StatusOK, gin.H{"ok": true, "jobId": jobID, "status": "started",
		"message": "Walking only root branches not yet in Firestore"})
}

// runSyncMissingRoots is the background goroutine for SyncMissingRoots.
func (h *TemuSchemaHandler) runSyncMissingRoots(ctx context.Context, jobID string, client *temu.Client, tenantID, credentialID string) {
	defer func() {
		h.activeJobsMu.Lock()
		delete(h.activeJobs, jobID)
		h.activeJobsMu.Unlock()
	}()

	log.Printf("[TemuSyncJob %s] sync-missing-roots started", jobID)

	prog := &temuJobProgress{jobsCol: h.jobsCol(), jobID: jobID}
	prog.addLog("info", "sync-missing-roots: loading existing categories from Firestore to seed visited map")
	prog.flush(ctx, "Loading existing categories from Firestore...")

	// ── Step 1: Seed visited map from every catId already in Firestore ──────
	// This ensures cross-branch cycles (where a category in an unvisited branch
	// points back to a category already stored from a previous walk) are caught.
	visited := make(map[int]bool)
	// Also build a set of parentIds present in Firestore so we can detect
	// which root branches already have children stored.
	knownParents := make(map[int]bool)

	{
		iter := h.categoriesCol().Documents(ctx)
		count := 0
		for {
			doc, err := iter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				msg := fmt.Sprintf("Error reading existing categories: %v — continuing with partial visited set", err)
				prog.addLog("warn", msg)
				log.Printf("[TemuSyncJob %s] %s", jobID, msg)
				break
			}
			data := doc.Data()
			catID := getIntValue(data, "catId")
			parentID := getIntValue(data, "parentId")
			if catID > 0 {
				visited[catID] = true
				count++
			}
			if parentID > 0 {
				knownParents[parentID] = true
			}
		}
		iter.Stop()
		msg := fmt.Sprintf("Seeded visited map with %d existing category IDs; %d known parent IDs", count, len(knownParents))
		prog.addLog("info", msg)
		log.Printf("[TemuSyncJob %s] %s", jobID, msg)
		prog.flush(ctx, msg)
	}

	// ── Step 2: Fetch root categories ────────────────────────────────────────
	prog.addLog("info", "Fetching root categories from Temu API...")
	prog.flush(ctx, "Fetching root categories...")

	var rootCats []temu.TemuCategory
	for attempt := 0; attempt < 3; attempt++ {
		callCtx, callCancel := context.WithTimeout(ctx, temuTreeCallTimeoutS*time.Second)
		var catErr error
		rootCats, catErr = client.GetCategoriesCtx(callCtx, nil)
		callCancel()
		if catErr == nil {
			break
		}
		if waitSec, is429 := temuParseRetryAfter(catErr); is429 {
			prog.addLog("warn", fmt.Sprintf("429 fetching roots — waiting %ds", waitSec))
			select {
			case <-ctx.Done():
				h.finaliseTemuJob(context.Background(), jobID, "cancelled", prog)
				return
			case <-time.After(time.Duration(waitSec) * time.Second):
			}
			continue
		}
		if attempt == 2 {
			msg := fmt.Sprintf("Failed to fetch root categories after 3 attempts: %v", catErr)
			prog.addError(msg)
			log.Printf("[TemuSyncJob %s] %s", jobID, msg)
			h.finaliseTemuJob(context.Background(), jobID, "failed", prog)
			return
		}
		select {
		case <-ctx.Done():
			h.finaliseTemuJob(context.Background(), jobID, "cancelled", prog)
			return
		case <-time.After(5 * time.Second):
		}
	}

	// ── Step 3: Identify which roots are missing ──────────────────────────────
	var missingRoots []temu.TemuCategory
	var skippedRoots []temu.TemuCategory
	for _, root := range rootCats {
		if knownParents[root.CatID] || visited[root.CatID] {
			skippedRoots = append(skippedRoots, root)
		} else {
			missingRoots = append(missingRoots, root)
		}
	}

	msg := fmt.Sprintf("Root categories: %d total, %d already in Firestore (skipping), %d missing (will walk)",
		len(rootCats), len(skippedRoots), len(missingRoots))
	prog.addLog("info", msg)
	log.Printf("[TemuSyncJob %s] %s", jobID, msg)

	for _, r := range skippedRoots {
		prog.addLog("info", fmt.Sprintf("  SKIP root: %d %s (already in Firestore)", r.CatID, r.CatName))
	}
	for _, r := range missingRoots {
		prog.addLog("info", fmt.Sprintf("  WALK root: %d %s", r.CatID, r.CatName))
	}
	prog.flush(ctx, msg)

	if len(missingRoots) == 0 {
		prog.addLog("info", "All root branches already in Firestore — nothing to walk. Use Resume to download templates.")
		log.Printf("[TemuSyncJob %s] No missing roots — done", jobID)
		h.finaliseTemuJob(context.Background(), jobID, "completed", prog)
		return
	}

	// ── Step 4: Walk only the missing roots ───────────────────────────────────
	// walkBranch is a local closure sharing the outer visited map.
	var walkBranch func(parentID *int, level int) error
	walkBranch = func(parentID *int, level int) error {
		select {
		case <-ctx.Done():
			return fmt.Errorf("cancelled")
		default:
		}

		if level > temuMaxTreeDepth {
			msg := fmt.Sprintf("Depth limit (%d) at parent=%v — skipping (possible cycle)", temuMaxTreeDepth, parentID)
			prog.addLog("warn", msg)
			log.Printf("[TemuSyncJob %s] %s", jobID, msg)
			return nil
		}

		var cats []temu.TemuCategory
		for attempt := 0; attempt < 3; attempt++ {
			callCtx, callCancel := context.WithTimeout(ctx, temuTreeCallTimeoutS*time.Second)
			var catErr error
			cats, catErr = client.GetCategoriesCtx(callCtx, parentID)
			callCancel()
			if catErr == nil {
				break
			}
			if waitSec, is429 := temuParseRetryAfter(catErr); is429 {
				msg := fmt.Sprintf("429 on GetCategories (parent=%v, attempt %d/3) — waiting %ds", parentID, attempt+1, waitSec)
				prog.addLog("warn", msg)
				log.Printf("[TemuSyncJob %s] %s", jobID, msg)
				prog.flush(ctx, fmt.Sprintf("Rate-limited — waiting %ds...", waitSec))
				select {
				case <-ctx.Done():
					return fmt.Errorf("cancelled")
				case <-time.After(time.Duration(waitSec) * time.Second):
				}
				if newClient, err := h.buildClient(ctx, tenantID, credentialID); err == nil {
					client = newClient
				}
				continue
			}
			log.Printf("[TemuSyncJob %s] GetCategories error (parent=%v, attempt %d/3): %v", jobID, parentID, attempt+1, catErr)
			if attempt == 1 {
				if newClient, err := h.buildClient(ctx, tenantID, credentialID); err == nil {
					client = newClient
				}
			}
			if attempt < 2 {
				select {
				case <-ctx.Done():
					return fmt.Errorf("cancelled")
				case <-time.After(5 * time.Second):
				}
			} else {
				msg := fmt.Sprintf("Skipping branch (parent=%v) after 3 failed attempts: %v", parentID, catErr)
				prog.addLog("warn", msg)
				log.Printf("[TemuSyncJob %s] %s", jobID, msg)
				return nil
			}
		}

		for _, cat := range cats {
			select {
			case <-ctx.Done():
				return fmt.Errorf("cancelled")
			default:
			}

			if visited[cat.CatID] {
				prog.addLog("warn", fmt.Sprintf("Cycle detected: catId %d (%s) already visited — skipping", cat.CatID, cat.CatName))
				log.Printf("[TemuSyncJob %s] Cycle: catId %d skipped", jobID, cat.CatID)
				continue
			}
			visited[cat.CatID] = true
			cat.Level = level

			if cat.Leaf {
				prog.leafFound++
				catIDStr := fmt.Sprintf("%d", cat.CatID)
				catDocData := map[string]interface{}{
					"catId":    cat.CatID,
					"catName":  cat.CatName,
					"parentId": cat.ParentID,
					"leaf":     true,
					"level":    cat.Level,
					"cachedAt": time.Now(),
				}
				if _, err := h.categoriesCol().Doc(catIDStr).Set(ctx, catDocData); err != nil {
					log.Printf("[TemuSyncJob %s] Warning: failed to write leaf %d: %v", jobID, cat.CatID, err)
				}
				if prog.leafFound%temuLeafFlushInterval == 0 {
					msg := fmt.Sprintf("Tree walk: %d new leaves found (current: %s, level %d)", prog.leafFound, cat.CatName, level)
					prog.addLog("info", msg)
					log.Printf("[TemuSyncJob %s] %s", jobID, msg)
					prog.flush(ctx, fmt.Sprintf("Walking missing roots: %d new leaves — %s", prog.leafFound, cat.CatName))
				}
			}

			if !cat.Leaf {
				childParentID := cat.CatID
				if err := walkBranch(&childParentID, level+1); err != nil {
					return err
				}
			}

			select {
			case <-ctx.Done():
				return fmt.Errorf("cancelled")
			case <-time.After(temuTreeNodeSleepMs * time.Millisecond):
			}
		}
		return nil
	}

	// Walk each missing root one at a time, logging progress per root.
	for i, root := range missingRoots {
		select {
		case <-ctx.Done():
			prog.addLog("info", fmt.Sprintf("Cancelled after walking %d/%d missing roots", i, len(missingRoots)))
			prog.flush(ctx, "Cancelled")
			h.finaliseTemuJob(context.Background(), jobID, "cancelled", prog)
			return
		default:
		}

		rootMsg := fmt.Sprintf("Walking root %d/%d: %s (%d)", i+1, len(missingRoots), root.CatName, root.CatID)
		prog.addLog("info", rootMsg)
		log.Printf("[TemuSyncJob %s] %s", jobID, rootMsg)
		prog.flush(ctx, rootMsg)

		visited[root.CatID] = true
		rootID := root.CatID
		if err := walkBranch(&rootID, 1); err != nil {
			if err.Error() == "cancelled" {
				prog.addLog("info", "Cancelled during branch walk")
				prog.flush(ctx, "Cancelled")
				h.finaliseTemuJob(context.Background(), jobID, "cancelled", prog)
				return
			}
			prog.addError(fmt.Sprintf("Root %s (%d) failed: %v", root.CatName, root.CatID, err))
		} else {
			prog.addLog("info", fmt.Sprintf("Root %s (%d) complete — total new leaves so far: %d", root.CatName, root.CatID, prog.leafFound))
		}
		prog.flush(ctx, rootMsg+" — done")
	}

	// Update meta doc with new total.
	existingMeta, _ := h.categoriesMetaDoc().Get(ctx)
	existingLeafCount := 0
	if existingMeta != nil && existingMeta.Exists() {
		if v, ok := existingMeta.Data()["leafCount"].(int64); ok {
			existingLeafCount = int(v)
		} else if v, ok := existingMeta.Data()["leafCount"].(float64); ok {
			existingLeafCount = int(v)
		}
	}
	newTotal := existingLeafCount + prog.leafFound
	h.categoriesMetaDoc().Set(context.Background(), map[string]interface{}{
		"leafCount":  newTotal,
		"totalCount": newTotal,
		"cachedAt":   time.Now(),
	}, firestore.MergeAll)

	summary := fmt.Sprintf("sync-missing-roots complete — %d new leaves found across %d missing roots. Total in Firestore: ~%d. Hit Resume to download templates.",
		prog.leafFound, len(missingRoots), newTotal)
	prog.addLog("info", summary)
	log.Printf("[TemuSyncJob %s] %s", jobID, summary)
	h.finaliseTemuJob(context.Background(), jobID, "completed", prog)
}

// ============================================================================
// runSync — Phase 1: streaming tree walk + Phase 2: template download
// ============================================================================

func (h *TemuSchemaHandler) runSync(ctx context.Context, jobID string, client *temu.Client, tenantID, credentialID string, fullSync bool) {
	defer func() {
		h.activeJobsMu.Lock()
		delete(h.activeJobs, jobID)
		h.activeJobsMu.Unlock()
	}()

	log.Printf("[TemuSyncJob %s] Started (fullSync=%v)", jobID, fullSync)

	prog := &temuJobProgress{jobsCol: h.jobsCol(), jobID: jobID}
	prog.addLog("info", fmt.Sprintf("Job started (fullSync=%v)", fullSync))
	prog.flush(ctx, "Phase 1: walking category tree...")

	// ── PHASE 1: Stream-walk category tree ──────────────────────────────────
	// Each leaf category is written to Firestore immediately. No in-memory
	// accumulation — Phase 2 reads them back via a streaming iterator.

	// visited tracks every catId seen during this walk to detect cycles.
	// Temu's API has been observed returning cyclic references that cause
	// infinite recursion (observed depth 400+). This map is the primary guard;
	// temuMaxTreeDepth is the secondary guard for any cycle that slips through
	// via a very long chain before revisiting a node.
	visited := make(map[int]bool)

	var walkTree func(parentID *int, level int) error
	walkTree = func(parentID *int, level int) error {
		select {
		case <-ctx.Done():
			return fmt.Errorf("cancelled")
		default:
		}

		// Hard depth limit — secondary guard against cycles.
		if level > temuMaxTreeDepth {
			msg := fmt.Sprintf("Depth limit (%d) reached at parent=%v — skipping branch (possible cycle)", temuMaxTreeDepth, parentID)
			prog.addLog("warn", msg)
			log.Printf("[TemuSyncJob %s] %s", jobID, msg)
			return nil
		}

		// Retry GetCategories up to 3 times with per-call deadline and 429 detection.
		var cats []temu.TemuCategory
		for attempt := 0; attempt < 3; attempt++ {
			callCtx, callCancel := context.WithTimeout(ctx, temuTreeCallTimeoutS*time.Second)
			var catErr error
			cats, catErr = client.GetCategoriesCtx(callCtx, parentID)
			callCancel()
			if catErr == nil {
				break
			}

			// 429 check.
			if waitSec, is429 := temuParseRetryAfter(catErr); is429 {
				msg := fmt.Sprintf("429 on GetCategories (parent=%v, attempt %d/3) — waiting %ds", parentID, attempt+1, waitSec)
				prog.addLog("warn", msg)
				log.Printf("[TemuSyncJob %s] %s", jobID, msg)
				prog.flush(ctx, fmt.Sprintf("Rate-limited on tree walk — waiting %ds...", waitSec))
				select {
				case <-ctx.Done():
					return fmt.Errorf("cancelled")
				case <-time.After(time.Duration(waitSec) * time.Second):
				}
				// Rebuild client on 429 to refresh token.
				if newClient, err := h.buildClient(ctx, tenantID, credentialID); err == nil {
					client = newClient
				}
				continue
			}

			log.Printf("[TemuSyncJob %s] GetCategories error (parent=%v, attempt %d/3): %v", jobID, parentID, attempt+1, catErr)
			if attempt == 1 {
				// Rebuild client on second non-429 failure (could be auth issue).
				if newClient, err := h.buildClient(ctx, tenantID, credentialID); err == nil {
					client = newClient
				}
			}
			if attempt < 2 {
				select {
				case <-ctx.Done():
					return fmt.Errorf("cancelled")
				case <-time.After(5 * time.Second):
				}
			} else {
				// All 3 failed — skip branch, do not abort the whole walk.
				msg := fmt.Sprintf("Skipping branch (parent=%v) after 3 failed attempts: %v", parentID, catErr)
				prog.addLog("warn", msg)
				log.Printf("[TemuSyncJob %s] %s", jobID, msg)
				return nil
			}
		}

		for _, cat := range cats {
			select {
			case <-ctx.Done():
				return fmt.Errorf("cancelled")
			default:
			}

			// Cycle detection — skip any catId already visited in this walk.
			if visited[cat.CatID] {
				msg := fmt.Sprintf("Cycle detected: catId %d (%s) already visited — skipping to prevent infinite loop", cat.CatID, cat.CatName)
				prog.addLog("warn", msg)
				log.Printf("[TemuSyncJob %s] %s", jobID, msg)
				continue
			}
			visited[cat.CatID] = true

			cat.Level = level

			if cat.Leaf {
				prog.leafFound++
				catIDStr := fmt.Sprintf("%d", cat.CatID)
				catDocData := map[string]interface{}{
					"catId":    cat.CatID,
					"catName":  cat.CatName,
					"parentId": cat.ParentID,
					"leaf":     true,
					"level":    cat.Level,
					"cachedAt": time.Now(),
				}
				// Synchronous write — keeps SDK RPC queue empty.
				if _, err := h.categoriesCol().Doc(catIDStr).Set(ctx, catDocData); err != nil {
					log.Printf("[TemuSyncJob %s] Warning: failed to write leaf category %d: %v", jobID, cat.CatID, err)
				}

				if prog.leafFound%temuLeafFlushInterval == 0 {
					msg := fmt.Sprintf("Tree walk: %d leaves found (current: %s, level %d)", prog.leafFound, cat.CatName, level)
					prog.addLog("info", msg)
					log.Printf("[TemuSyncJob %s] %s", jobID, msg)
					prog.flush(ctx, fmt.Sprintf("Walking tree: %d leaves found — %s", prog.leafFound, cat.CatName))
				}
			}

			if !cat.Leaf {
				childParentID := cat.CatID
				if err := walkTree(&childParentID, level+1); err != nil {
					return err
				}
			}

			select {
			case <-ctx.Done():
				return fmt.Errorf("cancelled")
			case <-time.After(temuTreeNodeSleepMs * time.Millisecond):
			}
		}
		return nil
	}

	if err := walkTree(nil, 0); err != nil {
		if err.Error() == "cancelled" {
			prog.addLog("info", fmt.Sprintf("Job cancelled during tree walk (leafFound=%d)", prog.leafFound))
			prog.flush(ctx, "Cancelled during tree walk")
			h.finaliseTemuJob(context.Background(), jobID, "cancelled", prog)
			return
		}
		msg := fmt.Sprintf("Tree walk failed: %v", err)
		prog.addError(msg)
		log.Printf("[TemuSyncJob %s] %s", jobID, msg)
		h.finaliseTemuJob(context.Background(), jobID, "failed", prog)
		return
	}

	log.Printf("[TemuSyncJob %s] Tree walk complete: %d leaf categories found", jobID, prog.leafFound)
	prog.addLog("info", fmt.Sprintf("Tree walk complete: %d leaf categories found", prog.leafFound))

	if prog.leafFound == 0 {
		msg := "Tree walk completed but found 0 leaf categories. Possible causes: (1) Temu API returned empty results, (2) invalid app_key/app_secret, (3) egress proxy blocked, (4) base_url misconfigured. Check Cloud Run logs for GetCategories errors above."
		prog.addError(msg)
		log.Printf("[TemuSyncJob %s] ABORTING: %s", jobID, msg)
		h.finaliseTemuJob(context.Background(), jobID, "failed", prog)
		return
	}

	// Update meta doc.
	h.categoriesMetaDoc().Set(context.Background(), map[string]interface{}{
		"leafCount":  prog.leafFound,
		"totalCount": prog.leafFound,
		"cachedAt":   time.Now(),
	}, firestore.MergeAll)

	h.jobsCol().Doc(jobID).Set(context.Background(), map[string]interface{}{
		"treeWalkDone":    true,
		"total":           prog.leafFound,
		"leafFound":       prog.leafFound,
		"currentActivity": fmt.Sprintf("Tree walk complete (%d leaves) — downloading templates...", prog.leafFound),
		"updatedAt":       time.Now(),
	}, firestore.MergeAll)

	// ── PHASE 2: streaming template download ──
	h.runTemplateDownloadStreaming(ctx, jobID, client, tenantID, credentialID, prog)
}

// ============================================================================
// runTemplateDownloadStreaming — Phase 2 (zero-accumulation)
// Reads leaf categories one at a time from a Firestore iterator.
// ============================================================================

func (h *TemuSchemaHandler) runTemplateDownloadStreaming(
	ctx context.Context,
	jobID string,
	client *temu.Client,
	tenantID, credentialID string,
	prog *temuJobProgress,
) {
	total := prog.leafFound
	log.Printf("[TemuSyncJob %s] Template download phase: %d categories", jobID, total)
	prog.addLog("info", fmt.Sprintf("Starting template download for %d leaf categories", total))

	processed := 0
	consecutiveFailures := 0

	iter := h.categoriesCol().Where("leaf", "==", true).Documents(ctx)
	defer iter.Stop()

	for {
		select {
		case <-ctx.Done():
			prog.addLog("info", fmt.Sprintf("Cancelled during template download at %d/%d", processed, total))
			prog.flush(ctx, "Cancelled")
			h.finaliseTemuJob(context.Background(), jobID, "cancelled", prog)
			return
		default:
		}

		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			msg := fmt.Sprintf("Firestore iterator error at template %d: %v", processed, err)
			prog.addError(msg)
			log.Printf("[TemuSyncJob %s] %s", jobID, msg)
			break
		}

		data := doc.Data()
		catID := getIntValue(data, "catId")
		catName := getStrValue(data, "catName")
		if catID == 0 {
			continue
		}
		catIDStr := fmt.Sprintf("%d", catID)
		processed++

		activity := fmt.Sprintf("[%d/%d] %s (%d)", processed, total, catName, catID)

		// Skip fresh templates.
		existingDoc, err := h.templatesCol().Doc(catIDStr).Get(ctx)
		if err == nil && existingDoc.Exists() {
			if cachedAt, ok := existingDoc.Data()["cachedAt"].(time.Time); ok {
				if time.Since(cachedAt) < temuSchemaFreshDays*24*time.Hour {
					prog.skipped++
					prog.flush(ctx, activity+" (skipped — fresh)")
					continue
				}
			}
		}

		// Periodic client refresh.
		if processed > 0 && processed%temuClientRefreshInterval == 0 {
			prog.addLog("info", fmt.Sprintf("Refreshing Temu client at template %d/%d", processed, total))
			if newClient, err := h.buildClient(ctx, tenantID, credentialID); err == nil {
				client = newClient
				prog.addLog("info", "Client refreshed")
			} else {
				prog.addLog("warn", fmt.Sprintf("Client refresh failed: %v", err))
			}
		}

		// Download template with retry + 429-aware backoff.
		template, err := h.downloadTemplateWithRetry(ctx, client, catID, temuRetryAttempts, jobID, prog)
		if err != nil {
			errMsg := fmt.Sprintf("%d (%s): %v", catID, catName, err)
			prog.addError(errMsg)
			prog.failed++
			consecutiveFailures++
			log.Printf("[TemuSyncJob %s] Template failed: %s", jobID, errMsg)
			if consecutiveFailures >= temuMaxConsecFailures {
				pauseMsg := fmt.Sprintf("%d consecutive failures — pausing %ds", consecutiveFailures, temuConsecFailurePauseS)
				prog.addLog("warn", pauseMsg)
				log.Printf("[TemuSyncJob %s] %s", jobID, pauseMsg)
				prog.flush(ctx, fmt.Sprintf("Paused %ds after consecutive failures", temuConsecFailurePauseS))
				select {
				case <-ctx.Done():
					h.finaliseTemuJob(context.Background(), jobID, "cancelled", prog)
					return
				case <-time.After(time.Duration(temuConsecFailurePauseS) * time.Second):
				}
				consecutiveFailures = 0
			}
		} else {
			templateData := buildTemuTemplateDoc(catID, catName, template)
			if _, err := h.templatesCol().Doc(catIDStr).Set(context.Background(), templateData); err != nil {
				errMsg := fmt.Sprintf("%d: store failed: %v", catID, err)
				prog.addError(errMsg)
				prog.failed++
			} else {
				prog.downloaded++
				consecutiveFailures = 0
				if prog.downloaded%50 == 0 {
					prog.addLog("info", fmt.Sprintf("Progress: %d downloaded, %d skipped, %d failed of %d", prog.downloaded, prog.skipped, prog.failed, total))
				}
			}
		}

		shouldFlush := processed%temuUpdateInterval == 0 ||
			len(prog.pendingLogs) >= temuLogFlushSize
		if shouldFlush {
			prog.flush(ctx, activity)
		}

		select {
		case <-ctx.Done():
			h.finaliseTemuJob(context.Background(), jobID, "cancelled", prog)
			return
		case <-time.After(temuInterTemplateSleepMs * time.Millisecond):
		}
	}

	prog.addLog("info", fmt.Sprintf("Job complete — downloaded=%d, skipped=%d, failed=%d of %d", prog.downloaded, prog.skipped, prog.failed, total))
	log.Printf("[TemuSyncJob %s] Complete: downloaded=%d, skipped=%d, failed=%d of %d", jobID, prog.downloaded, prog.skipped, prog.failed, total)
	h.finaliseTemuJob(context.Background(), jobID, "completed", prog)
}

// runTemplateDownload — used by Resume (pre-loaded bounded set from Firestore).
func (h *TemuSchemaHandler) runTemplateDownload(
	ctx context.Context,
	jobID string,
	client *temu.Client,
	tenantID, credentialID string,
	leafCatIDs []int,
	leafCatNames map[int]string,
) {
	log.Printf("[TemuSyncJob %s] Template download (resume): %d categories", jobID, len(leafCatIDs))

	prog := &temuJobProgress{
		jobsCol:   h.jobsCol(),
		jobID:     jobID,
		leafFound: len(leafCatIDs),
	}
	prog.addLog("info", fmt.Sprintf("Resumed — downloading templates for %d categories", len(leafCatIDs)))
	consecutiveFailures := 0

	for i, catID := range leafCatIDs {
		select {
		case <-ctx.Done():
			prog.addLog("info", fmt.Sprintf("Cancelled at %d/%d", i, len(leafCatIDs)))
			prog.flush(ctx, "Cancelled")
			h.finaliseTemuJob(context.Background(), jobID, "cancelled", prog)
			return
		default:
		}

		catName := leafCatNames[catID]
		catIDStr := fmt.Sprintf("%d", catID)
		activity := fmt.Sprintf("[%d/%d] %s (%d)", i+1, len(leafCatIDs), catName, catID)

		existingDoc, err := h.templatesCol().Doc(catIDStr).Get(ctx)
		if err == nil && existingDoc.Exists() {
			if cachedAt, ok := existingDoc.Data()["cachedAt"].(time.Time); ok {
				if time.Since(cachedAt) < temuSchemaFreshDays*24*time.Hour {
					prog.skipped++
					continue
				}
			}
		}

		if i > 0 && i%temuClientRefreshInterval == 0 {
			if newClient, err := h.buildClient(ctx, tenantID, credentialID); err == nil {
				client = newClient
			}
		}

		template, err := h.downloadTemplateWithRetry(ctx, client, catID, temuRetryAttempts, jobID, prog)
		if err != nil {
			errMsg := fmt.Sprintf("%d (%s): %v", catID, catName, err)
			prog.addError(errMsg)
			prog.failed++
			consecutiveFailures++
			if consecutiveFailures >= temuMaxConsecFailures {
				msg := fmt.Sprintf("%d consecutive failures — pausing %ds", consecutiveFailures, temuConsecFailurePauseS)
				prog.addLog("warn", msg)
				prog.flush(ctx, fmt.Sprintf("Paused %ds", temuConsecFailurePauseS))
				select {
				case <-ctx.Done():
					h.finaliseTemuJob(context.Background(), jobID, "cancelled", prog)
					return
				case <-time.After(time.Duration(temuConsecFailurePauseS) * time.Second):
				}
				consecutiveFailures = 0
			}
		} else {
			templateData := buildTemuTemplateDoc(catID, catName, template)
			if _, err := h.templatesCol().Doc(catIDStr).Set(context.Background(), templateData); err != nil {
				prog.addError(fmt.Sprintf("%d: store failed: %v", catID, err))
				prog.failed++
			} else {
				prog.downloaded++
				consecutiveFailures = 0
			}
		}

		shouldFlush := (i+1)%temuUpdateInterval == 0 ||
			i == len(leafCatIDs)-1 ||
			len(prog.pendingLogs) >= temuLogFlushSize
		if shouldFlush {
			prog.flush(ctx, activity)
		}

		select {
		case <-ctx.Done():
			h.finaliseTemuJob(context.Background(), jobID, "cancelled", prog)
			return
		case <-time.After(temuInterTemplateSleepMs * time.Millisecond):
		}
	}

	prog.addLog("info", fmt.Sprintf("Resume complete — downloaded=%d, skipped=%d, failed=%d of %d", prog.downloaded, prog.skipped, prog.failed, len(leafCatIDs)))
	h.finaliseTemuJob(context.Background(), jobID, "completed", prog)
}

// downloadTemplateWithRetry: retry loop with 429 detection and interruptible sleep.
func (h *TemuSchemaHandler) downloadTemplateWithRetry(
	ctx context.Context,
	client *temu.Client,
	catID int,
	maxRetries int,
	jobID string,
	prog *temuJobProgress,
) (map[string]interface{}, error) {
	backoff := time.Duration(temuBaseBackoffS) * time.Second

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

		// 429 — honour Retry-After.
		if waitSec, is429 := temuParseRetryAfter(err); is429 {
			waitDur := time.Duration(waitSec) * time.Second
			msg := fmt.Sprintf("429 on template %d — waiting %ds (attempt %d/%d)", catID, waitSec, i+1, maxRetries)
			prog.addLog("warn", msg)
			log.Printf("[TemuSyncJob %s] %s", jobID, msg)
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("cancelled during 429 backoff")
			case <-time.After(waitDur):
			}
			continue
		}

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
	return nil, fmt.Errorf("all %d attempts failed for template %d", maxRetries, catID)
}

// finaliseTemuJob writes terminal state to Firestore.
func (h *TemuSchemaHandler) finaliseTemuJob(ctx context.Context, jobID, status string, prog *temuJobProgress) {
	now := time.Now()
	logsToWrite := prog.allLogs
	if len(logsToWrite) > temuMaxLogEntries {
		logsToWrite = logsToWrite[len(logsToWrite)-temuMaxLogEntries:]
	}
	h.jobsCol().Doc(jobID).Set(ctx, map[string]interface{}{
		"status":          status,
		"downloaded":      prog.downloaded,
		"skipped":         prog.skipped,
		"failed":          prog.failed,
		"leafFound":       prog.leafFound,
		"errors":          prog.errors,
		"logs":            logsToWrite,
		"currentActivity": "",
		"updatedAt":       now,
		"completedAt":     now,
	}, firestore.MergeAll)
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
		{Path: "currentActivity", Value: "Cancelled by user"},
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

// ── Scheduler wiring ──────────────────────────────────────────────────────────

func (h *TemuSchemaHandler) SetScheduler(s *TemuSchemaRefreshScheduler) { h.scheduler = s }

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

// ── Utility ───────────────────────────────────────────────────────────────────

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

// buildTemuTemplateDoc normalises a raw template API response into the
// Firestore document shape used by PrepareTemuListing.
//
// The Temu bg.local.goods.template.get API (after GetTemplate unwraps result)
// returns:
//
//	{ "templateInfo": { "templateId": N, "goodsProperties": [...] }, ... }
//
// We extract goodsProperties from templateInfo (primary) with fallbacks for
// older cached shapes, then store the full raw template so the frontend
// parseTemplate function can also navigate it directly.
func buildTemuTemplateDoc(catID int, catName string, template map[string]interface{}) map[string]interface{} {
	var goodsProperties interface{}

	// Primary path: templateInfo.goodsProperties (current Temu API response)
	if ti, ok := template["templateInfo"].(map[string]interface{}); ok {
		if gp, ok := ti["goodsProperties"]; ok {
			goodsProperties = gp
		}
	}

	// Fallback: top-level goodsProperties (some older cached templates)
	if goodsProperties == nil {
		if gp, ok := template["goodsProperties"]; ok {
			goodsProperties = gp
		}
	}

	// Fallback: result.templateInfo.goodsProperties (full API response stored raw)
	if goodsProperties == nil {
		if result, ok := template["result"].(map[string]interface{}); ok {
			if ti, ok := result["templateInfo"].(map[string]interface{}); ok {
				if gp, ok := ti["goodsProperties"]; ok {
					goodsProperties = gp
				}
			}
			// Fallback: result.goodsProperties (legacy shape)
			if goodsProperties == nil {
				if gp, ok := result["goodsProperties"]; ok {
					goodsProperties = gp
				}
			}
		}
	}

	return map[string]interface{}{
		"catId":           catID,
		"catName":         catName,
		"goodsProperties": goodsProperties,
		"rawTemplate":     template,
		"cachedAt":        time.Now(),
	}
}
