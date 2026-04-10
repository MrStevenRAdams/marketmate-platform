package handlers

// ============================================================================
// OPS HANDLER — Cross-tenant system observability
// ============================================================================
//
// Provides a single endpoint that aggregates ALL job types across ALL tenants
// into one response. This is the backend for the Operations Console UI.
//
// GET /api/v1/admin/ops/jobs
//   Query params:
//     tenant_id  — filter to specific tenant (optional)
//     status     — filter by status: running|pending|failed|completed|all (default: all)
//     type       — filter by job type (optional)
//     limit      — max jobs per type per tenant (default 50)
//
// Response:
//   {
//     "tenants": [...],
//     "summary": { "total": N, "running": N, "stuck": N, "failed": N },
//     "job_groups": [
//       {
//         "type": "import",
//         "label": "Product Imports",
//         "jobs": [...]
//       },
//       ...
//     ]
//   }
//
// POST /api/v1/admin/ops/jobs/:tenant_id/:collection/:job_id/cancel
// POST /api/v1/admin/ops/jobs/:tenant_id/:collection/:job_id/retry
// DELETE /api/v1/admin/ops/jobs/:tenant_id/:collection/:job_id
// ============================================================================

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"google.golang.org/api/iterator"
)

type OpsHandler struct {
	client      *firestore.Client
	syncHandler orderSyncRestarter // optional — injected after construction
}

// orderSyncRestarter is a narrow interface so ops_handler doesn't import the
// full handlers package (which would be a circular import).
type orderSyncRestarter interface {
	EnableOrderSync(ctx context.Context, tenantID, credentialID, channel string)
}

func NewOpsHandler(client *firestore.Client) *OpsHandler {
	return &OpsHandler{client: client}
}

// SetOrderSyncHandler injects the order sync handler after construction.
// Called from main.go once both handlers are initialised.
func (h *OpsHandler) SetOrderSyncHandler(s orderSyncRestarter) {
	h.syncHandler = s
}

// opsJob is the unified job shape returned to the frontend regardless of source collection
type opsJob struct {
	// Identity
	JobID      string `json:"job_id"`
	TenantID   string `json:"tenant_id"`
	TenantName string `json:"tenant_name"`
	Collection string `json:"collection"` // which Firestore collection this came from
	JobType    string `json:"job_type"`   // normalised type label

	// What it is
	Channel     string `json:"channel,omitempty"`
	AccountName string `json:"account_name,omitempty"`
	Description string `json:"description"` // human readable one-liner

	// Status
	Status        string `json:"status"`
	StatusMessage string `json:"status_message,omitempty"`
	IsStuck       bool   `json:"is_stuck"`

	// Progress
	Total     int `json:"total"`
	Processed int `json:"processed"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
	Skipped   int `json:"skipped"`

	// Timing
	CreatedAt   string  `json:"created_at"`
	StartedAt   string  `json:"started_at,omitempty"`
	UpdatedAt   string  `json:"updated_at"`
	CompletedAt string  `json:"completed_at,omitempty"`
	ElapsedSecs float64 `json:"elapsed_secs,omitempty"`

	// Raw data for drill-down
	Raw map[string]interface{} `json:"raw,omitempty"`

	// Actions available
	CanCancel bool `json:"can_cancel"`
	CanRetry  bool `json:"can_retry"`
	CanDelete bool `json:"can_delete"`
}

type jobGroup struct {
	Type  string   `json:"type"`
	Label string   `json:"label"`
	Icon  string   `json:"icon"`
	Jobs  []opsJob `json:"jobs"`
}

type opsSummary struct {
	Total   int `json:"total"`
	Running int `json:"running"`
	Pending int `json:"pending"`
	Stuck   int `json:"stuck"`
	Failed  int `json:"failed"`
	Done    int `json:"done"`
}

// jobCollectionSpec defines a collection to scrape and how to normalise it
type jobCollectionSpec struct {
	Collection string
	GroupType  string
	GroupLabel string
	Icon       string
}

var jobCollections = []jobCollectionSpec{
	{"import_jobs", "import", "Product Imports", "📦"},
	{"import_jobs_csv", "import_csv", "CSV Imports", "📄"},
	{"ebay_enrich_jobs", "ebay_enrich", "eBay Enrichment", "🔍"},
	{"ai_generation_jobs", "ai_gen", "AI Listing Generation", "✨"},
	{"schema_jobs", "schema", "Schema Sync", "🗂️"},
	{"order_sync_jobs", "order_sync", "Order Sync Chains", "🔄"},
	{"jobs", "background", "Background Jobs", "⚙️"},
}

// ── GET /api/v1/admin/ops/jobs ────────────────────────────────────────────────

func (h *OpsHandler) ListAllJobs(c *gin.Context) {
	ctx := c.Request.Context()

	filterTenant := c.Query("tenant_id")
	filterStatus := c.Query("status") // "active", "failed", "all"
	if filterStatus == "" {
		filterStatus = "all"
	}

	// 1. Get all tenants
	tenants, err := h.getAllTenants(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list tenants: " + err.Error()})
		return
	}

	targets := tenants
	if filterTenant != "" {
		for _, t := range tenants {
			if t["tenant_id"] == filterTenant {
				targets = []map[string]interface{}{t}
				break
			}
		}
	}

	// 2. Scrape all job collections across all tenants in parallel
	type result struct {
		tenantID   string
		tenantName string
		spec       jobCollectionSpec
		jobs       []opsJob
	}

	ch := make(chan result, len(targets)*len(jobCollections))
	var wg sync.WaitGroup

	for _, t := range targets {
		tid, _ := t["tenant_id"].(string)
		tname, _ := t["name"].(string)
		if tname == "" {
			tname = tid
		}

		for _, spec := range jobCollections {
			wg.Add(1)
			go func(tenantID, tenantName string, s jobCollectionSpec) {
				defer wg.Done()
				jobs := h.scrapeCollection(ctx, tenantID, tenantName, s)
				ch <- result{tenantID, tenantName, s, jobs}
			}(tid, tname, spec)
		}
	}

	wg.Wait()
	close(ch)

	// 3. Assemble into groups
	groupMap := make(map[string]*jobGroup)
	for _, spec := range jobCollections {
		groupMap[spec.GroupType] = &jobGroup{
			Type:  spec.GroupType,
			Label: spec.GroupLabel,
			Icon:  spec.Icon,
			Jobs:  []opsJob{},
		}
	}

	summary := opsSummary{}
	now := time.Now()

	for r := range ch {
		g := groupMap[r.spec.GroupType]
		for _, job := range r.jobs {
			// Apply status filter
			if filterStatus == "active" && job.Status != "running" && job.Status != "pending" {
				continue
			}
			if filterStatus == "failed" && job.Status != "failed" {
				continue
			}

			g.Jobs = append(g.Jobs, job)
			summary.Total++

			switch job.Status {
			case "running":
				summary.Running++
			case "pending":
				summary.Pending++
			case "failed":
				summary.Failed++
			case "completed":
				summary.Done++
			}

			// Detect stuck: active job not updated in 30 min
			if (job.Status == "running" || job.Status == "pending") && job.UpdatedAt != "" {
				if t, err := time.Parse(time.RFC3339, job.UpdatedAt); err == nil {
					if now.Sub(t) > 30*time.Minute {
						summary.Stuck++
					}
				}
			}
		}
	}

	// Sort each group: active first, then by updated_at desc
	for _, g := range groupMap {
		sortJobs(g.Jobs)
	}

	// Build ordered group list, skip empty groups
	var groups []jobGroup
	for _, spec := range jobCollections {
		g := groupMap[spec.GroupType]
		if len(g.Jobs) > 0 {
			groups = append(groups, *g)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"tenants":    tenants,
		"summary":    summary,
		"job_groups": groups,
		"fetched_at": time.Now().Format(time.RFC3339),
	})
}

// ── POST /api/v1/admin/ops/jobs/:tenant_id/:collection/:job_id/cancel ─────────

func (h *OpsHandler) CancelJob(c *gin.Context) {
	tenantID   := c.Param("tenant_id")
	collection := c.Param("collection")
	jobID      := c.Param("job_id")

	if !isSafeCollection(collection) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unknown collection"})
		return
	}

	ctx := c.Request.Context()
	ref := h.client.Collection("tenants").Doc(tenantID).Collection(collection).Doc(jobID)

	_, err := ref.Update(ctx, []firestore.Update{
		{Path: "status", Value: "cancelled"},
		{Path: "status_message", Value: "Cancelled by operator via ops console"},
		{Path: "updated_at", Value: time.Now()},
		{Path: "completed_at", Value: time.Now()},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[Ops] Cancelled job %s/%s/%s", tenantID, collection, jobID)
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "Job cancelled"})
}

// ── POST /api/v1/admin/ops/jobs/:tenant_id/:collection/:job_id/retry ──────────
//
// For order_sync_jobs: re-seeds the Cloud Tasks chain for that credential.
// For all other job types: resets the job to pending so it can be re-run
// (the actual re-execution is channel-specific; this marks it retryable and
// leaves the import handler to pick it up, or operators can trigger manually).

func (h *OpsHandler) RetryJob(c *gin.Context) {
	tenantID   := c.Param("tenant_id")
	collection := c.Param("collection")
	jobID      := c.Param("job_id")

	if !isSafeCollection(collection) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unknown collection"})
		return
	}

	ctx := c.Request.Context()

	// ── Order sync chain restart ──────────────────────────────────────────────
	if collection == "order_sync_jobs" {
		doc, err := h.client.Collection("tenants").Doc(tenantID).
			Collection("order_sync_jobs").Doc(jobID).Get(ctx)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "order sync job not found"})
			return
		}

		raw := doc.Data()
		credentialID := coerceString(raw, "credential_id", "")
		channel      := coerceString(raw, "channel", "")

		if credentialID == "" || channel == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "job document missing credential_id or channel"})
			return
		}

		// Mark as restarting in Firestore so the UI reflects it immediately.
		_, _ = h.client.Collection("tenants").Doc(tenantID).
			Collection("order_sync_jobs").Doc(jobID).
			Update(ctx, []firestore.Update{
				{Path: "status", Value: "active"},
				{Path: "status_message", Value: "Chain restarted by operator via ops console"},
				{Path: "restarted_at", Value: time.Now().Format(time.RFC3339)},
				{Path: "updated_at", Value: time.Now().Format(time.RFC3339)},
			})

		if h.syncHandler != nil {
			go h.syncHandler.EnableOrderSync(context.Background(), tenantID, credentialID, channel)
			log.Printf("[Ops] Restarted order sync chain %s/%s (cred=%s channel=%s)",
				tenantID, jobID, credentialID, channel)
			c.JSON(http.StatusOK, gin.H{"ok": true, "message": "Order sync chain restarted"})
		} else {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": "Cloud Tasks not available — order sync handler not initialised",
			})
		}
		return
	}

	// ── Generic job reset ─────────────────────────────────────────────────────
	ref := h.client.Collection("tenants").Doc(tenantID).Collection(collection).Doc(jobID)

	_, err := ref.Update(ctx, []firestore.Update{
		{Path: "status", Value: "pending"},
		{Path: "status_message", Value: "Reset to pending by operator via ops console"},
		{Path: "updated_at", Value: time.Now().Format(time.RFC3339)},
		{Path: "completed_at", Value: nil},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[Ops] Reset job to pending: %s/%s/%s", tenantID, collection, jobID)
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "Job reset to pending"})
}

// ── DELETE /api/v1/admin/ops/jobs/:tenant_id/:collection/:job_id ──────────────

func (h *OpsHandler) DeleteJob(c *gin.Context) {
	tenantID   := c.Param("tenant_id")
	collection := c.Param("collection")
	jobID      := c.Param("job_id")

	if !isSafeCollection(collection) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unknown collection"})
		return
	}

	ctx := c.Request.Context()
	_, err := h.client.Collection("tenants").Doc(tenantID).Collection(collection).Doc(jobID).Delete(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[Ops] Deleted job %s/%s/%s", tenantID, collection, jobID)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ── GET /api/v1/admin/ops/jobs/:tenant_id/:collection/:job_id ─────────────────
// Returns full raw job document for drill-down

func (h *OpsHandler) GetJobDetail(c *gin.Context) {
	tenantID   := c.Param("tenant_id")
	collection := c.Param("collection")
	jobID      := c.Param("job_id")

	if !isSafeCollection(collection) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unknown collection"})
		return
	}

	ctx := c.Request.Context()
	doc, err := h.client.Collection("tenants").Doc(tenantID).Collection(collection).Doc(jobID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"job_id":     jobID,
		"tenant_id":  tenantID,
		"collection": collection,
		"data":       doc.Data(),
	})
}

// ── Private helpers ───────────────────────────────────────────────────────────

func (h *OpsHandler) getAllTenants(ctx context.Context) ([]map[string]interface{}, error) {
	iter := h.client.Collection("tenants").OrderBy("created_at", firestore.Asc).Documents(ctx)
	defer iter.Stop()

	var tenants []map[string]interface{}
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		data := doc.Data()
		if _, ok := data["tenant_id"]; !ok {
			data["tenant_id"] = doc.Ref.ID
		}
		tenants = append(tenants, data)
	}
	return tenants, nil
}

func (h *OpsHandler) scrapeCollection(ctx context.Context, tenantID, tenantName string, spec jobCollectionSpec) []opsJob {
	iter := h.client.Collection("tenants").Doc(tenantID).
		Collection(spec.Collection).
		OrderBy("created_at", firestore.Desc).
		Limit(100).
		Documents(ctx)
	defer iter.Stop()

	var jobs []opsJob
	now := time.Now()

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Printf("[Ops] scrape %s/%s: %v", tenantID, spec.Collection, err)
			break
		}

		raw := doc.Data()
		job := normaliseJob(raw, doc.Ref.ID, tenantID, tenantName, spec, now)
		jobs = append(jobs, job)
	}

	return jobs
}

// normaliseJob converts any raw Firestore job document into a consistent opsJob shape
func normaliseJob(raw map[string]interface{}, docID, tenantID, tenantName string, spec jobCollectionSpec, now time.Time) opsJob {
	j := opsJob{
		JobID:      coerceString(raw, "job_id", "jobId", docID),
		TenantID:   tenantID,
		TenantName: tenantName,
		Collection: spec.Collection,
		JobType:    spec.GroupType,
		Raw:        raw,
	}

	j.Status        = coerceString(raw, "status", "Status", "pending")
	j.StatusMessage = coerceString(raw, "status_message", "statusMessage", "")
	j.Channel       = coerceString(raw, "channel", "Channel", "")
	j.AccountName   = coerceString(raw, "account_name", "accountName", "")

	// Normalise timing fields — different collections use different key names
	j.CreatedAt   = coerceTime(raw, "created_at", "createdAt")
	j.StartedAt   = coerceTime(raw, "started_at", "startedAt")
	j.UpdatedAt   = coerceTime(raw, "updated_at", "updatedAt")
	j.CompletedAt = coerceTime(raw, "completed_at", "completedAt")

	// Progress — different collections use different field names
	j.Total     = coerceInt(raw, "total_items", "total", "totalProducts", "total_products")
	j.Processed = coerceInt(raw, "processed_items", "processed", "processedCount", "processed_count")
	j.Succeeded = coerceInt(raw, "successful_items", "succeeded", "successCount", "success_count")
	j.Failed    = coerceInt(raw, "failed_items", "failed", "failedCount", "failed_count")
	j.Skipped   = coerceInt(raw, "skipped_items", "skipped")

	// Elapsed time
	if j.StartedAt != "" {
		if t, err := time.Parse(time.RFC3339, j.StartedAt); err == nil {
			end := now
			if j.CompletedAt != "" {
				if ct, err2 := time.Parse(time.RFC3339, j.CompletedAt); err2 == nil {
					end = ct
				}
			}
			j.ElapsedSecs = end.Sub(t).Seconds()
		}
	}

	// Stuck detection: active and no update in 30 min
	if (j.Status == "running" || j.Status == "pending") && j.UpdatedAt != "" {
		if t, err := time.Parse(time.RFC3339, j.UpdatedAt); err == nil {
			j.IsStuck = now.Sub(t) > 30*time.Minute
		}
	}

	// Human-readable description
	j.Description = buildDescription(j, raw)

	// Available actions
	active := j.Status == "running" || j.Status == "pending"
	j.CanCancel = active && j.JobType != "order_sync" // chains don't support cancel; disable from config
	j.CanRetry  = j.Status == "failed" || j.Status == "cancelled" ||
		(j.JobType == "order_sync" && (j.Status == "broken" || j.Status == "disabled"))
	j.CanDelete = !active

	return j
}

func buildDescription(j opsJob, raw map[string]interface{}) string {
	switch j.JobType {
	case "import":
		jt := coerceString(raw, "job_type", "jobType", "import")
		acct := j.AccountName
		if acct == "" {
			acct = j.Channel
		}
		return fmt.Sprintf("%s · %s", jt, acct)
	case "ebay_enrich":
		total := coerceInt(raw, "total", "total_products")
		return fmt.Sprintf("eBay Browse enrichment · %d products", total)
	case "ai_gen":
		channels, _ := raw["channels"].([]interface{})
		if len(channels) > 0 {
			chStr := fmt.Sprintf("%v", channels[0])
			if len(channels) > 1 {
				chStr += fmt.Sprintf(" +%d", len(channels)-1)
			}
			return fmt.Sprintf("AI listing generation · %s", chStr)
		}
		return "AI listing generation"
	case "order_sync":
		freq := coerceInt(raw, "frequency_minutes", "frequencyMinutes")
		acct := j.AccountName
		if acct == "" {
			acct = j.Channel
		}
		if freq > 0 {
			return fmt.Sprintf("Order sync · %s · every %d min", acct, freq)
		}
		return fmt.Sprintf("Order sync · %s", acct)
	case "schema":
		mp := coerceString(raw, "marketplace_id", "marketplaceId", "")
		return fmt.Sprintf("Schema sync · %s", mp)
	default:
		t := coerceString(raw, "type", "Type", j.JobType)
		return t
	}
}

func isSafeCollection(c string) bool {
	safe := map[string]bool{
		"import_jobs": true, "import_jobs_csv": true,
		"ebay_enrich_jobs": true, "ai_generation_jobs": true,
		"schema_jobs": true, "jobs": true, "order_sync_jobs": true,
	}
	return safe[c]
}

func sortJobs(jobs []opsJob) {
	// Insertion sort: active first, then by updated_at desc (small N, fine)
	for i := 1; i < len(jobs); i++ {
		for j := i; j > 0; j-- {
			ai := jobs[j-1].Status == "running" || jobs[j-1].Status == "pending"
			aj := jobs[j].Status == "running" || jobs[j].Status == "pending"
			if ai == aj {
				if jobs[j].UpdatedAt > jobs[j-1].UpdatedAt {
					jobs[j], jobs[j-1] = jobs[j-1], jobs[j]
				} else {
					break
				}
			} else if aj && !ai {
				jobs[j], jobs[j-1] = jobs[j-1], jobs[j]
			} else {
				break
			}
		}
	}
}

// ── Field coercion helpers ────────────────────────────────────────────────────
// Different job collections use camelCase vs snake_case and different field names.
// These helpers try multiple keys and return sensible defaults.

func coerceString(m map[string]interface{}, keys ...string) string {
	// Last key is the default value if no key matches
	for i, k := range keys {
		if i == len(keys)-1 {
			return k // last arg = default
		}
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

func coerceInt(m map[string]interface{}, keys ...string) int {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch n := v.(type) {
			case int64:
				return int(n)
			case float64:
				return int(n)
			case int:
				return n
			}
		}
	}
	return 0
}

func coerceTime(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch t := v.(type) {
			case time.Time:
				if !t.IsZero() {
					return t.Format(time.RFC3339)
				}
			case string:
				if t != "" {
					return t
				}
			}
		}
	}
	return ""
}

// ============================================================================
// POST /api/v1/admin/ops/copy-products
// ============================================================================
// Copies products (and their extended_data subcollection) from one tenant to
// another by SKU list (max 500 SKUs). Listings are NOT copied.
// Request body:
//   { "source_tenant": "tenant-xxx", "dest_tenant": "tenant-yyy", "skus": ["SKU1", "SKU2"] }
// Response streams progress as newline-delimited JSON events.

func (h *OpsHandler) CopyProducts(c *gin.Context) {
	var req struct {
		SourceTenant string   `json:"source_tenant"`
		DestTenant   string   `json:"dest_tenant"`
		SKUs         []string `json:"skus"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.SourceTenant == "" || req.DestTenant == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "source_tenant and dest_tenant are required"})
		return
	}
	if req.SourceTenant == req.DestTenant {
		c.JSON(http.StatusBadRequest, gin.H{"error": "source and destination tenants must be different"})
		return
	}
	if len(req.SKUs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "at least one SKU is required"})
		return
	}
	if len(req.SKUs) > 500 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "maximum 500 SKUs per copy operation"})
		return
	}

	ctx := c.Request.Context()
	skuSet := make(map[string]bool, len(req.SKUs))
	for _, s := range req.SKUs {
		if s != "" {
			skuSet[strings.TrimSpace(s)] = true
		}
	}

	type result struct {
		SKU     string `json:"sku"`
		Status  string `json:"status"`  // "copied", "skipped", "error"
		Message string `json:"message,omitempty"`
	}

	var results []result
	copied, skipped, errCount := 0, 0, 0

	// Iterate all source products and filter by SKU
	iter := h.client.Collection("tenants").Doc(req.SourceTenant).
		Collection("products").Documents(ctx)
	defer iter.Stop()

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read source products: " + err.Error()})
			return
		}

		data := doc.Data()
		sku, _ := data["sku"].(string)
		if !skuSet[sku] {
			continue
		}

		// Check if product with this SKU already exists in destination
		existIter := h.client.Collection("tenants").Doc(req.DestTenant).
			Collection("products").Where("sku", "==", sku).Limit(1).Documents(ctx)
		existDoc, existErr := existIter.Next()
		existIter.Stop()
		if existErr == nil && existDoc != nil {
			results = append(results, result{SKU: sku, Status: "skipped", Message: "already exists in destination"})
			skipped++
			delete(skuSet, sku) // mark as processed
			continue
		}

		// Generate new product ID for destination
		newProductID := doc.Ref.ID // reuse same ID — keeps references consistent; override if collision needed
		// Override tenant_id and product_id
		data["tenant_id"] = req.DestTenant
		data["product_id"] = newProductID

		// Write product document to destination
		destRef := h.client.Collection("tenants").Doc(req.DestTenant).
			Collection("products").Doc(newProductID)
		if _, writeErr := destRef.Set(ctx, data); writeErr != nil {
			results = append(results, result{SKU: sku, Status: "error", Message: writeErr.Error()})
			errCount++
			delete(skuSet, sku)
			continue
		}

		// Copy all extended_data subcollection documents
		extIter := doc.Ref.Collection("extended_data").Documents(ctx)
		for {
			extDoc, extErr := extIter.Next()
			if extErr == iterator.Done {
				break
			}
			if extErr != nil {
				break
			}
			extData := extDoc.Data()
			extData["tenant_id"] = req.DestTenant
			extData["product_id"] = newProductID
			if _, extWriteErr := destRef.Collection("extended_data").Doc(extDoc.Ref.ID).Set(ctx, extData); extWriteErr != nil {
				log.Printf("[CopyProducts] WARNING: failed to copy extended_data/%s for sku=%s: %v", extDoc.Ref.ID, sku, extWriteErr)
			}
		}
		extIter.Stop()

		results = append(results, result{SKU: sku, Status: "copied"})
		copied++
		delete(skuSet, sku)
	}

	// Any SKUs left in the set were not found in the source
	for sku := range skuSet {
		results = append(results, result{SKU: sku, Status: "error", Message: "not found in source tenant"})
		errCount++
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":      errCount == 0,
		"copied":  copied,
		"skipped": skipped,
		"errors":  errCount,
		"results": results,
	})
}
