package handlers

// ============================================================================
// ORDER POLLER
//
// Runs inside the backend process on a 15-minute tick. For every active
// credential that has Config.Orders.Enabled=true it checks whether the
// FrequencyMinutes window has elapsed since LastSync and, if so, fires an
// order import goroutine.
//
// This replaces the previous design where OrchestratorHandler.Orchestrate
// was meant to be called by Cloud Scheduler — that route was never registered.
// Having an in-process ticker is simpler to operate (no Cloud Scheduler job
// to configure) and still allows the external Cloud Scheduler approach later
// by calling POST /internal/orders/orchestrate.
//
// The Orchestrate HTTP handler is also kept so that:
//   a) operators can trigger a manual sweep from outside
//   b) a Cloud Scheduler job can be pointed at it for redundancy
//
// Routes registered in main.go (no auth — secured by INTERNAL_SECRET header):
//   POST /internal/orders/orchestrate
// ============================================================================

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"module-a/repository"
)

// OrderPoller holds the dependencies needed to poll all credentials.
type OrderPoller struct {
	repo         *repository.MarketplaceRepository
	orderHandler *OrderHandler
}

// NewOrderPoller creates a new poller. Call Start() to begin ticking.
func NewOrderPoller(repo *repository.MarketplaceRepository, orderHandler *OrderHandler) *OrderPoller {
	return &OrderPoller{repo: repo, orderHandler: orderHandler}
}

// Start launches the background polling goroutine. It fires immediately on
// startup so that any due credentials are synced without waiting 15 minutes,
// then ticks every 15 minutes thereafter.
func (p *OrderPoller) Start(ctx context.Context) {
	go func() {
		// Brief delay on startup so the server is fully initialised.
		time.Sleep(30 * time.Second)

		p.runSweep(ctx, false)

		ticker := time.NewTicker(15 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				p.runSweep(ctx, false)
			case <-ctx.Done():
				log.Println("[OrderPoller] context cancelled — stopping")
				return
			}
		}
	}()

	log.Println("✅ OrderPoller: background order sync started (15-minute interval)")
}

// runSweep iterates all active credentials and triggers imports for those
// that are due. isBackup doubles the frequency window and lookback period,
// used by the external Cloud Scheduler backup endpoint.
func (p *OrderPoller) runSweep(ctx context.Context, isBackup bool) {
	allCreds, err := p.repo.ListAllActiveCredentials(ctx)
	if err != nil {
		log.Printf("[OrderPoller] failed to list credentials: %v", err)
		return
	}

	triggered := 0
	skipped := 0
	now := time.Now().UTC()

	for _, cred := range allCreds {
		cfg := cred.Config.Orders

		if !cfg.Enabled {
			skipped++
			continue
		}

		freqMins := cfg.FrequencyMinutes
		if freqMins <= 0 {
			freqMins = 30
		}
		if isBackup {
			freqMins *= 2
		}

		// Check whether enough time has passed since the last successful sync.
		isDue := true
		if cfg.LastSync != "" {
			lastSync, err := time.Parse(time.RFC3339, cfg.LastSync)
			if err == nil {
				isDue = now.After(lastSync.Add(time.Duration(freqMins) * time.Minute))
			}
		}

		if !isDue {
			skipped++
			continue
		}

		lookbackHours := cfg.LookbackHours
		if lookbackHours <= 0 {
			lookbackHours = 24
		}
		if isBackup && lookbackHours < 48 {
			lookbackHours = 48
		}

		dateFrom := now.Add(-time.Duration(lookbackHours) * time.Hour).Format("2006-01-02")
		dateTo := now.Format("2006-01-02")

		// Capture loop variables for the goroutine.
		tenantID := cred.TenantID
		credID := cred.CredentialID
		channel := cred.Channel

		log.Printf("[OrderPoller] triggering sync tenant=%s channel=%s cred=%s", tenantID, channel, credID)

		go func() {
			sweepCtx := context.Background()
			jobID, err := p.orderHandler.orderService.StartOrderImport(sweepCtx, tenantID, channel, credID, dateFrom, dateTo)
			if err != nil {
				log.Printf("[OrderPoller] failed to start import %s/%s: %v", tenantID, credID, err)
				p.repo.UpdateCredentialLastSync(sweepCtx, tenantID, credID, "failed", err.Error(), 0)
				return
			}
			p.orderHandler.processChannelImport(tenantID, jobID, channel, credID, dateFrom, dateTo)
			p.repo.UpdateCredentialLastSync(sweepCtx, tenantID, credID, "success", "", 0)
		}()

		triggered++
	}

	log.Printf("[OrderPoller] sweep done: triggered=%d skipped=%d total=%d backup=%v",
		triggered, skipped, len(allCreds), isBackup)
}

// ============================================================================
// HTTP HANDLER — POST /internal/orders/orchestrate
//
// Allows an external Cloud Scheduler job (or operator) to trigger a sweep.
// Protected by a shared secret in X-Internal-Secret header.
// ============================================================================

// OrchestratorHandler exposes the sweep over HTTP for external triggers.
// Re-uses the OrderPoller internally so the logic is never duplicated.
type OrchestratorHandler struct {
	repo         *repository.MarketplaceRepository
	orderHandler *OrderHandler
	poller       *OrderPoller
}

// NewOrchestratorHandler creates the handler. Pass the same poller that was
// started in main so both paths share the same sweep logic.
func NewOrchestratorHandler(repo *repository.MarketplaceRepository, orderHandler *OrderHandler) *OrchestratorHandler {
	poller := NewOrderPoller(repo, orderHandler)
	return &OrchestratorHandler{
		repo:         repo,
		orderHandler: orderHandler,
		poller:       poller,
	}
}

// Orchestrate handles POST /internal/orders/orchestrate
// Called by Cloud Scheduler or operators. No tenant auth — protected by
// X-Internal-Secret header matching the INTERNAL_SECRET env var.
func (h *OrchestratorHandler) Orchestrate(c *gin.Context) {
	if !h.validateInternalSecret(c) {
		return
	}
	isBackup := c.Query("mode") == "backup"
	go h.poller.runSweep(context.Background(), isBackup)
	c.JSON(http.StatusAccepted, gin.H{"ok": true, "message": "sweep triggered", "backup": isBackup})
}

// ImportNow handles POST /api/v1/orders/import/now — manual download from UI.
func (h *OrchestratorHandler) ImportNow(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	var req struct {
		CredentialID  string `json:"credential_id" binding:"required"`
		Channel       string `json:"channel" binding:"required"`
		LookbackHours int    `json:"lookback_hours"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	lookback := req.LookbackHours
	if lookback <= 0 {
		lookback = 24
	}

	now := time.Now().UTC()
	dateFrom := now.Add(-time.Duration(lookback) * time.Hour).Format("2006-01-02")
	dateTo := now.Format("2006-01-02")

	ctx := context.Background()
	jobID, err := h.orderHandler.orderService.StartOrderImport(ctx, tenantID, req.Channel, req.CredentialID, dateFrom, dateTo)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start import: " + err.Error()})
		return
	}

	go func() {
		h.orderHandler.processChannelImport(tenantID, jobID, req.Channel, req.CredentialID, dateFrom, dateTo)
		h.repo.UpdateCredentialLastSync(context.Background(), tenantID, req.CredentialID, "success", "", 0)
	}()

	c.JSON(http.StatusAccepted, gin.H{"job_id": jobID, "status": "started", "message": "order import started"})
}

// validateInternalSecret checks the X-Internal-Secret header.
// Returns true (and writes no response) if valid.
// Returns false after writing a 401 if invalid.
func (h *OrchestratorHandler) validateInternalSecret(c *gin.Context) bool {
	secret := os.Getenv("INTERNAL_SECRET")
	if secret == "" {
		// No secret configured — allow (dev/test mode).
		return true
	}
	provided := c.GetHeader("X-Internal-Secret")
	if provided != secret {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid internal secret"})
		return false
	}
	return true
}
