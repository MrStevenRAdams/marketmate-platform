package handlers

// ============================================================================
// ORDER SYNC TASK HANDLER
// ============================================================================
// Handles the Cloud Tasks callback that performs a single poll cycle for one
// credential, then re-enqueues itself for the next cycle.
//
// Each credential with order sync enabled runs as its own independent task
// chain. There is no shared ticker, no sequential scan of all tenants.
//
// Route (no Firebase auth — reached only by Cloud Tasks):
//   POST /api/v1/internal/orders/sync-task
//
// Firestore job tracking (collection: order_sync_jobs)
// ─────────────────────────────────────────────────────
// One document per credential, keyed by credential_id. The ops monitor reads
// this collection to show chain health and surface broken chains to operators.
//
//   {
//     "job_id":            "cred_abc123",      // == credential_id
//     "tenant_id":         "tenant-demo",
//     "credential_id":     "cred_abc123",
//     "channel":           "shopify",
//     "account_name":      "My Shopify Store",
//     "status":            "active|running|broken|disabled",
//     "status_message":    "...",
//     "frequency_minutes": 30,
//     "last_run_at":       "2025-...",
//     "next_run_at":       "2025-...",
//     "last_imported":     4,
//     "created_at":        "2025-...",
//     "updated_at":        "2025-..."
//   }
//
// Status values:
//   active   — chain is healthy, next task is queued
//   running  — a task is currently executing
//   broken   — last re-enqueue failed; chain needs operator restart
//   disabled — order sync was turned off; record kept for history
//
// Lifecycle
// ─────────
//   EnableOrderSync  ─► writes "active" doc, EnqueueOrderSync(delay=0)
//                              │
//                              ▼
//              POST /internal/orders/sync-task
//                   ├── update doc → "running"
//                   ├── if !enabled → update doc → "disabled", stop
//                   ├── run import (processChannelImport)
//                   ├── update doc → "active" + next_run_at
//                   └── EnqueueOrderSync(delay=FrequencyMinutes)
//                         └── if re-enqueue fails → update doc → "broken"
// ============================================================================

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"module-a/repository"
	"module-a/services"
)

// OrderSyncHandler handles the Cloud Tasks callback for scheduled order polling.
type OrderSyncHandler struct {
	repo         *repository.MarketplaceRepository
	orderHandler *OrderHandler
	taskSvc      *services.TaskService
	fsClient     *firestore.Client
}

// NewOrderSyncHandler creates the handler.
// taskSvc may be nil in local dev — re-enqueuing is skipped and sync only
// fires when triggered manually via POST /api/v1/orders/import/now.
func NewOrderSyncHandler(
	repo *repository.MarketplaceRepository,
	orderHandler *OrderHandler,
	taskSvc *services.TaskService,
	fsClient *firestore.Client,
) *OrderSyncHandler {
	return &OrderSyncHandler{
		repo:         repo,
		orderHandler: orderHandler,
		taskSvc:      taskSvc,
		fsClient:     fsClient,
	}
}

// ============================================================================
// TASK CALLBACK
// ============================================================================

// ProcessSyncTask handles POST /api/v1/internal/orders/sync-task
func (h *OrderSyncHandler) ProcessSyncTask(c *gin.Context) {
	if !h.isAuthorised(c) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authorised"})
		return
	}

	var payload services.OrderSyncTaskPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload: " + err.Error()})
		return
	}
	if payload.TenantID == "" || payload.CredentialID == "" || payload.Channel == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant_id, credential_id and channel are required"})
		return
	}

	ctx := context.Background()

	// Mark as running so the monitor shows live status.
	h.updateSyncJob(ctx, payload.TenantID, payload.CredentialID, map[string]interface{}{
		"status":         "running",
		"status_message": "Import in progress",
		"updated_at":     time.Now().Format(time.RFC3339),
	})

	// Load credential to get current config.
	cred, err := h.repo.GetCredential(ctx, payload.TenantID, payload.CredentialID)
	if err != nil {
		log.Printf("[OrderSync] credential not found tenant=%s cred=%s: %v",
			payload.TenantID, payload.CredentialID, err)
		h.updateSyncJob(ctx, payload.TenantID, payload.CredentialID, map[string]interface{}{
			"status":         "broken",
			"status_message": fmt.Sprintf("Credential not found: %v", err),
			"updated_at":     time.Now().Format(time.RFC3339),
		})
		// 4xx — credential is gone, stop retrying.
		c.JSON(http.StatusNotFound, gin.H{"error": "credential not found"})
		return
	}

	cfg := cred.Config.Orders

	// Stop chain if sync was disabled since this task was enqueued.
	if !cfg.Enabled {
		log.Printf("[OrderSync] sync disabled for tenant=%s cred=%s — stopping chain",
			payload.TenantID, payload.CredentialID)
		h.updateSyncJob(ctx, payload.TenantID, payload.CredentialID, map[string]interface{}{
			"status":         "disabled",
			"status_message": "Order sync disabled by user",
			"updated_at":     time.Now().Format(time.RFC3339),
		})
		c.JSON(http.StatusOK, gin.H{"ok": true, "skipped": "order sync disabled"})
		return
	}

	// Run the import.
	lookbackHours := cfg.LookbackHours
	if lookbackHours <= 0 {
		lookbackHours = 24
	}
	now := time.Now().UTC()
	dateFrom := now.Add(-time.Duration(lookbackHours) * time.Hour).Format("2006-01-02")
	dateTo := now.Format("2006-01-02")

	log.Printf("[OrderSync] running import tenant=%s cred=%s channel=%s lookback=%dh",
		payload.TenantID, payload.CredentialID, payload.Channel, lookbackHours)

	jobID, err := h.orderHandler.orderService.StartOrderImport(
		ctx, payload.TenantID, payload.Channel, payload.CredentialID, dateFrom, dateTo,
	)
	if err != nil {
		log.Printf("[OrderSync] failed to start import tenant=%s cred=%s: %v",
			payload.TenantID, payload.CredentialID, err)
		h.updateSyncJob(ctx, payload.TenantID, payload.CredentialID, map[string]interface{}{
			"status":         "broken",
			"status_message": fmt.Sprintf("Import failed to start: %v", err),
			"updated_at":     time.Now().Format(time.RFC3339),
		})
		// 5xx — Cloud Tasks will retry with backoff.
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Blocking — Cloud Tasks allows up to 30 minutes per task.
	h.orderHandler.processChannelImport(
		payload.TenantID, jobID, payload.Channel, payload.CredentialID, dateFrom, dateTo,
	)

	h.repo.UpdateCredentialLastSync(ctx, payload.TenantID, payload.CredentialID, "success", "", 0)

	// Re-enqueue the next cycle.
	freqMins := cfg.FrequencyMinutes
	if freqMins <= 0 {
		freqMins = 30
	}
	nextDelay := time.Duration(freqMins) * time.Minute
	nextRunAt := now.Add(nextDelay).Format(time.RFC3339)

	if h.taskSvc != nil {
		if err := h.taskSvc.EnqueueOrderSync(
			ctx, payload.TenantID, payload.CredentialID, payload.Channel, nextDelay,
		); err != nil {
			log.Printf("[OrderSync] WARNING: failed to re-enqueue %s/%s: %v — chain broken",
				payload.TenantID, payload.CredentialID, err)
			h.updateSyncJob(ctx, payload.TenantID, payload.CredentialID, map[string]interface{}{
				"status":            "broken",
				"status_message":    fmt.Sprintf("Re-enqueue failed: %v — restart from the ops monitor", err),
				"last_run_at":       now.Format(time.RFC3339),
				"frequency_minutes": freqMins,
				"updated_at":        now.Format(time.RFC3339),
			})
		} else {
			h.updateSyncJob(ctx, payload.TenantID, payload.CredentialID, map[string]interface{}{
				"status":            "active",
				"status_message":    fmt.Sprintf("Next run in %d min", freqMins),
				"last_run_at":       now.Format(time.RFC3339),
				"next_run_at":       nextRunAt,
				"frequency_minutes": freqMins,
				"updated_at":        now.Format(time.RFC3339),
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":            true,
		"tenant_id":     payload.TenantID,
		"credential_id": payload.CredentialID,
		"channel":       payload.Channel,
		"next_in":       nextDelay.String(),
	})
}

// ============================================================================
// ENABLE / DISABLE
// ============================================================================

// EnableOrderSync seeds the task chain for a credential and writes the initial
// order_sync_jobs document so it appears in the ops monitor immediately.
func (h *OrderSyncHandler) EnableOrderSync(ctx context.Context, tenantID, credentialID, channel string) {
	// Load credential for display fields.
	accountName := channel
	freqMins := 30
	if cred, err := h.repo.GetCredential(ctx, tenantID, credentialID); err == nil {
		if cred.AccountName != "" {
			accountName = cred.AccountName
		}
		if cred.Config.Orders.FrequencyMinutes > 0 {
			freqMins = cred.Config.Orders.FrequencyMinutes
		}
	}

	now := time.Now()
	h.upsertSyncJob(ctx, tenantID, credentialID, map[string]interface{}{
		"job_id":            credentialID,
		"tenant_id":         tenantID,
		"credential_id":     credentialID,
		"channel":           channel,
		"account_name":      accountName,
		"status":            "active",
		"status_message":    "First run queued",
		"frequency_minutes": freqMins,
		"next_run_at":       now.Add(2 * time.Second).Format(time.RFC3339),
		"created_at":        now.Format(time.RFC3339),
		"updated_at":        now.Format(time.RFC3339),
	})

	if h.taskSvc == nil {
		log.Printf("[OrderSync] Cloud Tasks not available — %s/%s will not auto-poll", tenantID, credentialID)
		h.updateSyncJob(ctx, tenantID, credentialID, map[string]interface{}{
			"status":         "broken",
			"status_message": "Cloud Tasks not configured — set API_BASE_URL and redeploy",
			"updated_at":     now.Format(time.RFC3339),
		})
		return
	}

	if err := h.taskSvc.EnqueueOrderSync(ctx, tenantID, credentialID, channel, 0); err != nil {
		log.Printf("[OrderSync] failed to seed chain for %s/%s: %v", tenantID, credentialID, err)
		h.updateSyncJob(ctx, tenantID, credentialID, map[string]interface{}{
			"status":         "broken",
			"status_message": fmt.Sprintf("Failed to queue first task: %v", err),
			"updated_at":     now.Format(time.RFC3339),
		})
	} else {
		log.Printf("[OrderSync] chain started tenant=%s cred=%s channel=%s", tenantID, credentialID, channel)
	}
}

// ============================================================================
// BOOT SEED
// ============================================================================

// SeedAllActiveCredentials is called once at startup to restart any chains
// that were lost when the server last deployed. Staggers task launches to
// avoid a thundering herd on boot.
func (h *OrderSyncHandler) SeedAllActiveCredentials(ctx context.Context) {
	if h.taskSvc == nil {
		log.Println("[OrderSync] Cloud Tasks not available — skipping boot seed")
		return
	}

	creds, err := h.repo.ListAllActiveCredentials(ctx)
	if err != nil {
		log.Printf("[OrderSync] boot seed: failed to list credentials: %v", err)
		return
	}

	seeded := 0
	for i, cred := range creds {
		if !cred.Config.Orders.Enabled {
			continue
		}

		tenantID := cred.TenantID
		credID := cred.CredentialID
		channel := cred.Channel

		// 500ms stagger per credential so we don't burst Cloud Tasks on every deploy.
		stagger := time.Duration(i) * 500 * time.Millisecond
		go func() {
			time.Sleep(stagger)
			h.EnableOrderSync(ctx, tenantID, credID, channel)
		}()

		seeded++
	}

	log.Printf("[OrderSync] boot seed: queued %d order-sync chains", seeded)
}

// ============================================================================
// FIRESTORE HELPERS
// ============================================================================

// upsertSyncJob creates or fully replaces the order_sync_jobs document.
func (h *OrderSyncHandler) upsertSyncJob(ctx context.Context, tenantID, credentialID string, data map[string]interface{}) {
	if h.fsClient == nil {
		return
	}
	ref := h.fsClient.Collection("tenants").Doc(tenantID).
		Collection("order_sync_jobs").Doc(credentialID)
	if _, err := ref.Set(ctx, data); err != nil {
		log.Printf("[OrderSync] upsert sync job doc %s/%s: %v", tenantID, credentialID, err)
	}
}

// updateSyncJob applies a partial update to the order_sync_jobs document.
// If the document doesn't exist yet (e.g. a stale task firing after a wipe),
// it falls back to a merge-set so the monitor always has something to show.
func (h *OrderSyncHandler) updateSyncJob(ctx context.Context, tenantID, credentialID string, fields map[string]interface{}) {
	if h.fsClient == nil {
		return
	}
	var updates []firestore.Update
	for k, v := range fields {
		updates = append(updates, firestore.Update{Path: k, Value: v})
	}
	ref := h.fsClient.Collection("tenants").Doc(tenantID).
		Collection("order_sync_jobs").Doc(credentialID)
	if _, err := ref.Update(ctx, updates); err != nil {
		// Doc may not exist — merge-set as fallback.
		if _, err2 := ref.Set(ctx, fields, firestore.MergeAll); err2 != nil {
			log.Printf("[OrderSync] update sync job doc %s/%s: %v", tenantID, credentialID, err2)
		}
	}
}

// ============================================================================
// AUTHORISATION
// ============================================================================

// isAuthorised returns true if the request came from Cloud Tasks or carries
// the internal secret. Cloud Tasks sets X-CloudTasks-QueueName which cannot
// be spoofed by external callers on Cloud Run or App Engine.
func (h *OrderSyncHandler) isAuthorised(c *gin.Context) bool {
	if c.GetHeader("X-CloudTasks-QueueName") != "" {
		return true
	}
	secret := os.Getenv("INTERNAL_SECRET")
	if secret != "" && c.GetHeader("X-Internal-Secret") == secret {
		return true
	}
	if secret == "" && os.Getenv("ENVIRONMENT") != "production" {
		return true
	}
	return false
}
