package handlers

import (
	"context"
	"fmt"
	"log"
	"time"
	"net/http"
	"strconv"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"module-a/marketplace"
	"module-a/models"
	"module-a/repository"
	"module-a/services"
)

// ============================================================================
// MARKETPLACE HANDLER - HTTP Endpoints for Module B
// ============================================================================

type MarketplaceHandler struct {
	marketplaceService *services.MarketplaceService
	repo               *repository.MarketplaceRepository
	importService      *services.ImportService
	listingService     *services.ListingService
	searchService      *services.SearchService // Typesense auto-sync
	orderSyncHandler   orderSyncEnabler        // optional — nil in tests
	fsClient           *firestore.Client       // for marketplaces collection
}

// orderSyncEnabler is a narrow interface so marketplace_handler doesn't import
// the full handlers package (which would be a circular import).
type orderSyncEnabler interface {
	EnableOrderSync(ctx context.Context, tenantID, credentialID, channel string)
}

func NewMarketplaceHandler(
	marketplaceService *services.MarketplaceService,
	repo *repository.MarketplaceRepository,
	importService *services.ImportService,
	listingService *services.ListingService,
	searchService *services.SearchService,
) *MarketplaceHandler {
	return &MarketplaceHandler{
		marketplaceService: marketplaceService,
		repo:               repo,
		importService:      importService,
		listingService:     listingService,
		searchService:      searchService,
	}
}

// SetOrderSyncHandler injects the order sync handler after construction.
// Called from main.go once both handlers are initialised.
func (h *MarketplaceHandler) SetOrderSyncHandler(osh orderSyncEnabler) {
	h.orderSyncHandler = osh
}

// SetFirestoreClient injects the Firestore client for the marketplace registry.
func (h *MarketplaceHandler) SetFirestoreClient(client *firestore.Client) {
	h.fsClient = client
}

// ============================================================================
// CREDENTIAL MANAGEMENT ENDPOINTS
// ============================================================================

// POST /api/v1/marketplace/credentials
func (h *MarketplaceHandler) CreateCredential(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req models.ConnectMarketplaceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	credential, err := h.marketplaceService.CreateCredential(c.Request.Context(), tenantID, req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to create credential",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"data": credential,
		"message": "Credential created successfully",
	})

	// Kick off an automatic pending_review import so products start downloading
	// immediately. Runs in the background — does not delay the response.
	go h.triggerAutoImport(context.Background(), tenantID, credential)
}

// triggerAutoImport fires an import job immediately after a channel is
// connected or reconnected. It runs in a goroutine so the connect response
// is not delayed.
//
// First connection for a channel → PendingReview=false: products go directly
// into the PIM catalogue so the user can start listing immediately.
//
// Second+ connection for the same channel → PendingReview=true: products land
// in pending_review status so the user can confirm duplicate mappings before
// they enter the catalogue.
func (h *MarketplaceHandler) triggerAutoImport(ctx context.Context, tenantID string, credential *models.MarketplaceCredential) {
	// pending_review=true whenever ANY products already exist in the tenant's
	// PIM catalogue, regardless of channel. The review workflow handles:
	//   - Exact/fuzzy matches (auto-matched by SKU/title) → user confirms
	//   - Unmatched → user links to existing product or imports as new
	//
	// Only the very first import ever (empty catalogue) goes directly into the
	// PIM without review, since there is nothing to match against.
	catalogueHasProducts := false
	fsClient := h.repo.GetFirestoreClient()
	if fsClient != nil {
		iter := fsClient.Collection("tenants").Doc(tenantID).
			Collection("products").
			Limit(1).
			Documents(ctx)
		doc, err := iter.Next()
		if err == nil && doc != nil {
			catalogueHasProducts = true
		}
		iter.Stop()
	}

	pendingReview := catalogueHasProducts
	log.Printf("[AutoImport] tenant=%s channel=%s credential=%s catalogueHasProducts=%v pendingReview=%v",
		tenantID, credential.Channel, credential.CredentialID, catalogueHasProducts, pendingReview)

	req := models.ImportProductsRequest{
		Channel:           credential.Channel,
		ChannelAccountID:  credential.CredentialID,
		JobType:           "auto_connect",
		FulfillmentFilter: "all",
		StockFilter:       "all",
		EnrichData:        true,
		PendingReview:     pendingReview,
	}

	job, err := h.importService.StartImportJob(ctx, tenantID, req)
	if err != nil {
		log.Printf("[AutoImport] Failed to start import job for %s/%s: %v", tenantID, credential.Channel, err)
		return
	}
	log.Printf("[AutoImport] Import job %s started for tenant=%s channel=%s (pending_review=%v)",
		job.JobID, tenantID, credential.Channel, pendingReview)
}

// GET /api/v1/marketplace/credentials
func (h *MarketplaceHandler) ListCredentials(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	credentials, err := h.marketplaceService.ListCredentials(c.Request.Context(), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": credentials})
}

// GET /api/v1/marketplace/credentials/:id
func (h *MarketplaceHandler) GetCredential(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	credentialID := c.Param("id")

	credential, err := h.marketplaceService.GetCredential(c.Request.Context(), tenantID, credentialID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Credential not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": credential})
}

// DELETE /api/v1/marketplace/credentials/:id
func (h *MarketplaceHandler) DeleteCredential(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	credentialID := c.Param("id")

	if err := h.marketplaceService.DeleteCredential(c.Request.Context(), tenantID, credentialID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Credential deleted successfully"})
}

// PATCH /api/v1/marketplace/credentials/:id
// Supports partial update of credential fields: active, inventory_sync_enabled
func (h *MarketplaceHandler) PatchCredential(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	credentialID := c.Param("id")
	ctx := c.Request.Context()

	var patch map[string]interface{}
	if err := c.ShouldBindJSON(&patch); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	docRef := h.repo.GetFirestoreClient().Collection("tenants").Doc(tenantID).
		Collection("marketplace_credentials").Doc(credentialID)

	// Build Firestore updates from allowed patch fields
	var updates []firestore.Update
	activatingNow := false
	if v, ok := patch["active"]; ok {
		updates = append(updates, firestore.Update{Path: "active", Value: v})
		if b, ok := v.(bool); ok && b {
			activatingNow = true
		}
	}
	if v, ok := patch["inventory_sync_enabled"]; ok {
		updates = append(updates, firestore.Update{Path: "inventory_sync_enabled", Value: v})
	}

	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No valid fields to update"})
		return
	}

	if _, err := docRef.Update(ctx, updates); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update credential: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "Credential updated"})

	// When a credential is re-activated, trigger auto-import if product import is enabled.
	if activatingNow {
		go func() {
			cred, err := h.repo.GetCredential(context.Background(), tenantID, credentialID)
			if err != nil {
				log.Printf("[PatchCredential] Could not load credential %s for auto-import: %v", credentialID, err)
				return
			}
			cfg, cfgErr := h.repo.GetCredentialConfig(context.Background(), tenantID, credentialID)
			// ImportEnabled defaults to true when:
			//   - no config doc exists yet (cfgErr or nil cfg)
			//   - config exists but Products field was never explicitly saved
			//     (zero value = false, but we treat that as "default on")
			// Only skip if the user has explicitly opened Product Import tab and
			// toggled it OFF (which saves import_enabled=false AND sets orders
			// frequency to a non-zero value, proving the config was user-saved).
			importEnabled := true
			if cfgErr == nil && cfg != nil && !cfg.Products.ImportEnabled {
				// Config was user-saved — respect the explicit off setting
				// But only if orders config has been touched too (proving it's a real save)
				if cfg.Orders.FrequencyMinutes > 0 {
					importEnabled = false
				}
				// Otherwise treat as default-on (Products field was zero-valued, not user-set)
			}
			if importEnabled {
				h.triggerAutoImport(context.Background(), tenantID, cred)
			} else {
				log.Printf("[PatchCredential] Product import explicitly disabled for credential %s — skipping", credentialID)
			}
		}()
	}
}

// PUT /api/v1/marketplace/credentials/:id/reconnect
// Re-saves credential data (new access token etc.) and re-runs TestConnection.
// Used by the Reconnect Account flow to update an existing credential in place
// rather than creating a duplicate.
func (h *MarketplaceHandler) ReconnectCredential(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	credentialID := c.Param("id")
	ctx := c.Request.Context()

	var req models.ConnectMarketplaceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Load existing credential to preserve fields not being updated
	existing, err := h.marketplaceService.GetCredential(ctx, tenantID, credentialID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Credential not found"})
		return
	}

	// Update account name if provided
	if req.AccountName != "" {
		existing.AccountName = req.AccountName
	}

	// Re-encrypt and merge new credential data
	for key, value := range req.Credentials {
		if value == "" || value == "••••••••" {
			continue // Skip masked/empty values — keep existing
		}
		if h.marketplaceService.IsSensitiveField(key) {
			encrypted, err := h.marketplaceService.Encrypt(value)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Encryption failed"})
				return
			}
			existing.CredentialData[key] = encrypted
			// Add to encrypted fields list if not already there
			found := false
			for _, f := range existing.EncryptedFields {
				if f == key { found = true; break }
			}
			if !found {
				existing.EncryptedFields = append(existing.EncryptedFields, key)
			}
		} else {
			existing.CredentialData[key] = value
		}
	}

	// Test the updated credential
	now := time.Now()
	testErr := h.marketplaceService.TestConnection(ctx, existing)

	if testErr != nil {
		existing.LastTestStatus = "failed"
		existing.LastErrorMessage = testErr.Error()
	} else {
		existing.LastTestStatus = "success"
		existing.LastErrorMessage = ""
		existing.LastTestedAt = &now
		existing.Active = true
		existing.Connected = true
	}
	existing.UpdatedAt = now

	// Save back to Firestore
	if err := h.marketplaceService.SaveCredential(ctx, existing); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save credential"})
		return
	}

	if testErr != nil {
		c.JSON(http.StatusOK, gin.H{
			"ok": false,
			"connected": false,
			"error": testErr.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok": true,
		"connected": true,
		"message": "Reconnected successfully",
		"credential_id": credentialID,
	})

	// Re-trigger auto import on reconnect so any new products are picked up.
	if existing != nil {
		go h.triggerAutoImport(context.Background(), tenantID, existing)
	}
}

// GET /api/v1/marketplace/credentials/:id/config
func (h *MarketplaceHandler) GetCredentialConfig(c *gin.Context) {
    tenantID := c.GetString("tenant_id")
    credentialID := c.Param("id")

    defaults := models.DefaultChannelConfig()
    config, err := h.repo.GetCredentialConfig(c.Request.Context(), tenantID, credentialID)
    if err != nil {
        // No config saved yet — return defaults
        c.JSON(http.StatusOK, gin.H{"data": defaults})
        return
    }

    // For credentials created before the Products field existed, Firestore returns
    // import_enabled=false (Go zero value). Apply the default (true) so the toggle
    // shows correctly. The user can explicitly turn it off and save to override.
    if !config.Products.ImportEnabled && config.Orders.FrequencyMinutes == 0 {
        config.Products.ImportEnabled = defaults.Products.ImportEnabled
    }
    // Always default import_enabled to true if it was never set
    // (safe heuristic: if the whole products config is zero-value, apply defaults)

    c.JSON(http.StatusOK, gin.H{"data": config})
}

// PATCH /api/v1/marketplace/credentials/:id/config
func (h *MarketplaceHandler) UpdateCredentialConfig(c *gin.Context) {
    tenantID := c.GetString("tenant_id")
    credentialID := c.Param("id")

    // Load existing config so we can detect the disabled→enabled transition.
    existing, _ := h.repo.GetCredentialConfig(c.Request.Context(), tenantID, credentialID)

    var config models.ChannelConfig
    if err := c.ShouldBindJSON(&config); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }

    if err := h.repo.UpdateCredentialConfig(c.Request.Context(), tenantID, credentialID, config); err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save config: " + err.Error()})
        return
    }

    // Seed the Cloud Tasks chain when order sync is turned on.
    // We only act on the false→true transition so saving an already-enabled
    // config does not immediately double-poll.
    wasEnabled := existing != nil && existing.Orders.Enabled
    if config.Orders.Enabled && !wasEnabled && h.orderSyncHandler != nil {
        cred, err := h.repo.GetCredential(c.Request.Context(), tenantID, credentialID)
        if err == nil {
            go h.orderSyncHandler.EnableOrderSync(
                context.Background(), tenantID, credentialID, cred.Channel,
            )
        }
    }

    c.JSON(http.StatusOK, gin.H{"message": "Configuration saved successfully", "data": config})
}

// POST /api/v1/marketplace/credentials/:id/test
func (h *MarketplaceHandler) TestConnection(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	credentialID := c.Param("id")
	ctx := c.Request.Context()
	now := time.Now()

	credential, err := h.marketplaceService.GetCredential(ctx, tenantID, credentialID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Credential not found"})
		return
	}

	if err := h.marketplaceService.TestConnection(ctx, credential); err != nil {
		// Update last_test_status in Firestore — never set active=false
		h.fsClient.Collection("tenants").Doc(tenantID).
			Collection("marketplace_credentials").Doc(credentialID).
			Update(ctx, []firestore.Update{
				{Path: "last_test_status", Value: "failed"},
				{Path: "last_error_message", Value: err.Error()},
				{Path: "last_tested_at", Value: now},
				{Path: "updated_at", Value: now},
			})
		c.JSON(http.StatusOK, gin.H{
			"connected": false,
			"error": err.Error(),
		})
		return
	}

	// Test succeeded — update status and try to get mall_id for Temu
	updates := []firestore.Update{
		{Path: "last_test_status", Value: "success"},
		{Path: "last_error_message", Value: ""},
		{Path: "last_tested_at", Value: now},
		{Path: "active", Value: true},
		{Path: "connected", Value: true},
		{Path: "updated_at", Value: now},
	}

	response := gin.H{
		"connected": true,
		"message":   "Connection successful",
	}

	// For Temu: fetch mall_id via GetMallInfo and persist it
	if credential.Channel == "temu" || credential.Channel == "temu_sandbox" {
		if mallInfo, err := h.marketplaceService.GetTemuMallInfo(ctx, credential); err == nil && mallInfo != nil {
			mallIDStr := fmt.Sprintf("%d", mallInfo.MallID)
			updates = append(updates, firestore.Update{Path: "mall_id", Value: mallIDStr})
			response["mall_id"] = mallIDStr
			response["mall_name"] = mallInfo.MallName
			log.Printf("[TestConnection] Temu mall_id=%s name=%s stored for %s/%s",
				mallIDStr, mallInfo.MallName, tenantID, credentialID)
		} else if err != nil {
			log.Printf("[TestConnection] Temu GetMallInfo failed (non-fatal): %v", err)
		}
	}

	h.fsClient.Collection("tenants").Doc(tenantID).
		Collection("marketplace_credentials").Doc(credentialID).
		Update(ctx, updates)

	c.JSON(http.StatusOK, response)
}


// ============================================================================
// CREDENTIAL AUDIT  POST /api/v1/marketplace/credentials/audit
// ============================================================================
// Tests every active credential across ALL tenants against the live API.
// Updates active=false + last_test_status="failed" for any that fail.
// Updates last_test_status="success" + last_tested_at for those that pass.
// Can also be called as a background task from the scheduler.
//
// Query params:
//   ?tenant_id=xxx  — audit only a specific tenant (optional)
//   ?fix=true       — mark failed credentials inactive (default true)

type CredentialAuditResult struct {
	TenantID     string `json:"tenant_id"`
	CredentialID string `json:"credential_id"`
	Channel      string `json:"channel"`
	AccountName  string `json:"account_name"`
	WasActive    bool   `json:"was_active"`
	NowActive    bool   `json:"now_active"`
	Status       string `json:"status"` // "ok" | "failed" | "skipped"
	Error        string `json:"error,omitempty"`
}

func (h *MarketplaceHandler) AuditAllCredentials(c *gin.Context) {
	ctx := c.Request.Context()
	fix := c.Query("fix") != "false" // default true
	filterTenant := c.Query("tenant_id")

	results, summary := h.runCredentialAudit(ctx, filterTenant, fix)
	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"results": results,
		"summary": summary,
	})
}

// RunCredentialAuditBackground is called by the scheduler.
func (h *MarketplaceHandler) RunCredentialAuditBackground(ctx context.Context) {
	results, summary := h.runCredentialAudit(ctx, "", true)
	log.Printf("[CredentialAudit] Complete: %+v (%d results)", summary, len(results))
}

func (h *MarketplaceHandler) runCredentialAudit(
	ctx context.Context,
	filterTenant string,
	fix bool,
) ([]CredentialAuditResult, map[string]int) {
	var results []CredentialAuditResult
	summary := map[string]int{"ok": 0, "failed": 0, "skipped": 0, "newly_inactive": 0}

	// Get all tenants (or just the filtered one)
	var tenantIDs []string
	if filterTenant != "" {
		tenantIDs = []string{filterTenant}
	} else {
		tenantIter := h.fsClient.Collection("tenants").Documents(ctx)
		defer tenantIter.Stop()
		for {
			doc, err := tenantIter.Next()
			if err != nil {
				break
			}
			tenantIDs = append(tenantIDs, doc.Ref.ID)
		}
	}

	for _, tenantID := range tenantIDs {
		creds, err := h.marketplaceService.ListCredentials(ctx, tenantID)
		if err != nil {
			log.Printf("[CredentialAudit] Failed to list credentials for %s: %v", tenantID, err)
			continue
		}

		for _, cred := range creds {
			credCopy := cred
			result := CredentialAuditResult{
				TenantID:     tenantID,
				CredentialID: cred.CredentialID,
				Channel:      cred.Channel,
				AccountName:  cred.AccountName,
				WasActive:    cred.Active,
				NowActive:    cred.Active,
			}

			// Test the connection
			testErr := h.marketplaceService.TestConnection(ctx, &credCopy)
			now := time.Now()

			if testErr != nil {
				result.Status = "failed"
				result.Error = testErr.Error()
				summary["failed"]++

				// IMPORTANT: Never set active=false from the audit.
				// Credentials with failed tokens must remain visible in the UI
				// so users can see them and use the Reconnect button.
				// last_test_status="failed" drives the UI reconnect state.
				h.fsClient.Collection("tenants").Doc(tenantID).
					Collection("marketplace_credentials").Doc(cred.CredentialID).
					Update(ctx, []firestore.Update{
						{Path: "last_test_status", Value: "failed"},
						{Path: "last_error_message", Value: testErr.Error()},
						{Path: "last_tested_at", Value: now},
						{Path: "updated_at", Value: now},
					})
				log.Printf("[CredentialAudit] ✗ FAILED (token issue, credential kept visible) %s/%s (%s — %s): %v",
					tenantID, cred.AccountName, cred.Channel, cred.CredentialID[:8], testErr)
			} else {
				result.Status = "ok"
				result.NowActive = true
				summary["ok"]++

				// Reactivate if it was previously failed/inactive
				updates := []firestore.Update{
					{Path: "last_test_status", Value: "success"},
					{Path: "last_error_message", Value: ""},
					{Path: "last_tested_at", Value: now},
					{Path: "updated_at", Value: now},
				}
				if !cred.Active {
					updates = append(updates, firestore.Update{Path: "active", Value: true})
					result.NowActive = true
					log.Printf("[CredentialAudit] ✓ REACTIVATED %s/%s (%s)",
						tenantID, cred.AccountName, cred.Channel)
				} else {
					log.Printf("[CredentialAudit] ✓ OK %s/%s (%s)",
						tenantID, cred.AccountName, cred.Channel)
				}
				h.fsClient.Collection("tenants").Doc(tenantID).
					Collection("marketplace_credentials").Doc(cred.CredentialID).
					Update(ctx, updates)
			}

			results = append(results, result)
		}
	}

	return results, summary
}

// ============================================================================
// IMPORT ENDPOINTS
// ============================================================================

// POST /api/v1/marketplace/import
func (h *MarketplaceHandler) StartImport(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req models.ImportProductsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Auto-detect pending_review when not explicitly set by the caller.
	// If the catalogue already has products, new imports go through review.
	// This mirrors the logic in triggerAutoImport on credential activation.
	if !req.PendingReview {
		fsClient := h.repo.GetFirestoreClient()
		if fsClient != nil {
			iter := fsClient.Collection("tenants").Doc(tenantID).
				Collection("products").
				Limit(1).
				Documents(c.Request.Context())
			doc, err := iter.Next()
			iter.Stop()
			if err == nil && doc != nil {
				req.PendingReview = true
				log.Printf("[StartImport] Auto-set pending_review=true for tenant=%s (catalogue has products)", tenantID)
			}
		}
	}

	job, err := h.importService.StartImportJob(c.Request.Context(), tenantID, req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"data":    job,
		"message": "Import job started",
	})
}

// GET /api/v1/marketplace/import/jobs
func (h *MarketplaceHandler) ListImportJobs(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	jobs, err := h.importService.ListImportJobs(c.Request.Context(), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": jobs})
}

// GET /api/v1/marketplace/import/jobs/:id
func (h *MarketplaceHandler) GetImportJob(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	jobID := c.Param("id")

	job, err := h.importService.GetImportJob(c.Request.Context(), tenantID, jobID)
	if err != nil {
		log.Printf("[Handler] GetImportJob failed tenant=%s job=%s: %v", tenantID, jobID, err)
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Job not found: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": job})
}

// POST /api/v1/marketplace/import/jobs/:id/cancel
func (h *MarketplaceHandler) CancelImportJob(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	jobID := c.Param("id")

	if err := h.importService.CancelImportJob(c.Request.Context(), tenantID, jobID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "cancelled", "message": "Import job cancelled"})
}

// DELETE /api/v1/marketplace/import/jobs/:id
func (h *MarketplaceHandler) DeleteImportJob(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	jobID := c.Param("id")

	if err := h.importService.DeleteImportJob(c.Request.Context(), tenantID, jobID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "deleted", "message": "Import job deleted"})
}

// ============================================================================
// LISTING ENDPOINTS
// ============================================================================

// POST /api/v1/marketplace/listings
func (h *MarketplaceHandler) CreateListing(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req models.CreateListingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	listing, err := h.listingService.CreateListing(c.Request.Context(), tenantID, req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Auto-sync to Typesense (best-effort)
	if h.searchService != nil {
		if err := h.searchService.IndexListing(listing); err != nil {
			log.Printf("⚠️  Typesense index failed for listing %s: %v", listing.ListingID, err)
		}
	}

	c.JSON(http.StatusCreated, gin.H{
		"data": listing,
		"message": "Listing created successfully",
	})
}

// GET /api/v1/marketplace/listings
func (h *MarketplaceHandler) ListListings(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	channel := c.Query("channel")
	productID := c.Query("product_id")

	// When product_id is supplied, query directly — don't paginate-then-filter-in-memory
	// which silently misses listings beyond the first page.
	if productID != "" {
		listings, err := h.repo.ListListingsByProductID(c.Request.Context(), tenantID, productID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		// Optional channel sub-filter
		if channel != "" {
			filtered := listings[:0]
			for _, l := range listings {
				if l.Channel == channel {
					filtered = append(filtered, l)
				}
			}
			listings = filtered
		}
		c.JSON(http.StatusOK, gin.H{
			"listings": listings,
			"data":     listings,
			"total":    len(listings),
		})
		return
	}

	// Pagination params
	limit := 50
	offset := 0
	if l := c.Query("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 200 {
			limit = v
		}
	}
	if o := c.Query("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil && v >= 0 {
			offset = v
		}
	}

	listings, total, err := h.listingService.ListListingsWithProductsPaginated(c.Request.Context(), tenantID, channel, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"listings": listings,
		"data":     listings,
		"total":    total,
		"limit":    limit,
		"offset":   offset,
	})
}

// GET /api/v1/marketplace/listings/unlisted
func (h *MarketplaceHandler) ListUnlisted(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	channel := c.Query("channel")

	if channel == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "channel query parameter is required"})
		return
	}

	products, total, err := h.listingService.GetUnlistedProducts(c.Request.Context(), tenantID, channel, 100, 0)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": products, "total": total})
}

// GET /api/v1/marketplace/listings/:id
func (h *MarketplaceHandler) GetListing(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	listingID := c.Param("id")

	listing, err := h.listingService.GetListing(c.Request.Context(), tenantID, listingID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Listing not found"})
		return
	}

	// Build enriched response — join product data + extended data
	response := gin.H{"listing": listing}

	// Fetch linked product
	if listing.ProductID != "" {
		product, err := h.listingService.GetLinkedProduct(c.Request.Context(), tenantID, listing.ProductID)
		if err == nil && product != nil {
			response["product"] = product
		}
	}

	// Fetch extended data for this product's ASIN
	if listing.ProductID != "" {
		extData, err := h.listingService.GetExtendedDataForProduct(c.Request.Context(), tenantID, listing.ProductID)
		if err == nil && extData != nil {
			response["extended_data"] = extData
		}
	}

	c.JSON(http.StatusOK, gin.H{"data": response})
}

// PATCH /api/v1/marketplace/listings/:id
func (h *MarketplaceHandler) UpdateListing(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	listingID := c.Param("id")

	listing, err := h.listingService.GetListing(c.Request.Context(), tenantID, listingID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Listing not found"})
		return
	}

	var updates models.ListingOverrides
	if err := c.ShouldBindJSON(&updates); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	listing.Overrides = &updates
	if err := h.listingService.UpdateListing(c.Request.Context(), listing); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Auto-sync to Typesense (best-effort)
	if h.searchService != nil {
		if err := h.searchService.IndexListing(listing); err != nil {
			log.Printf("⚠️  Typesense index failed for listing %s: %v", listing.ListingID, err)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"data": listing,
		"message": "Listing updated successfully",
	})
}

// DELETE /api/v1/marketplace/listings/:id
func (h *MarketplaceHandler) DeleteListing(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	listingID := c.Param("id")

	if err := h.listingService.DeleteListing(c.Request.Context(), tenantID, listingID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Remove from Typesense (best-effort)
	if h.searchService != nil {
		if err := h.searchService.DeleteListing(listingID); err != nil {
			log.Printf("⚠️  Typesense delete failed for listing %s: %v", listingID, err)
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "Listing deleted successfully"})
}

// POST /api/v1/marketplace/listings/:id/publish
func (h *MarketplaceHandler) PublishListing(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	listingID := c.Param("id")

	if err := h.listingService.PublishListing(c.Request.Context(), tenantID, listingID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Listing published successfully"})
}

// POST /api/v1/marketplace/listings/:id/validate
func (h *MarketplaceHandler) ValidateListing(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	listingID := c.Param("id")

	listing, err := h.listingService.GetListing(c.Request.Context(), tenantID, listingID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Listing not found"})
		return
	}

	validation, err := h.listingService.ValidateListing(c.Request.Context(), listing)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": validation})
}

// POST /api/v1/marketplace/listings/bulk/publish
func (h *MarketplaceHandler) BulkPublishListings(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req models.PublishListingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	results := make(map[string]interface{})
	for _, listingID := range req.ListingIDs {
		err := h.listingService.PublishListing(c.Request.Context(), tenantID, listingID)
		if err != nil {
			results[listingID] = gin.H{"success": false, "error": err.Error()}
		} else {
			results[listingID] = gin.H{"success": true}
		}
	}

	c.JSON(http.StatusOK, gin.H{"results": results})
}

// POST /api/v1/marketplace/listings/bulk/enrich
func (h *MarketplaceHandler) BulkEnrichListings(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req struct {
		ListingIDs []string `json:"listing_ids"` // specific listings, OR
		Mode       string   `json:"mode"`         // "all_unenriched" to catch up all
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var count int
	var err error

	if req.Mode == "all_unenriched" {
		count, err = h.listingService.EnrichAllUnenriched(c.Request.Context(), tenantID)
	} else if len(req.ListingIDs) > 0 {
		count, err = h.listingService.EnrichSelected(c.Request.Context(), tenantID, req.ListingIDs)
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Provide listing_ids or mode=all_unenriched"})
		return
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"queued": count, "message": fmt.Sprintf("Queued %d products for enrichment", count)})
}

// POST /api/v1/marketplace/listings/bulk/delete
func (h *MarketplaceHandler) BulkDeleteListings(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req struct {
		ListingIDs []string `json:"listing_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if len(req.ListingIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No listing IDs provided"})
		return
	}

	results := make(map[string]interface{})
	for _, listingID := range req.ListingIDs {
		err := h.listingService.DeleteListing(c.Request.Context(), tenantID, listingID)
		if err != nil {
			results[listingID] = gin.H{"success": false, "error": err.Error()}
		} else {
			results[listingID] = gin.H{"success": true}
			// Remove from Typesense (best-effort)
			if h.searchService != nil {
				if err := h.searchService.DeleteListing(listingID); err != nil {
					log.Printf("⚠️  Typesense delete failed for listing %s: %v", listingID, err)
				}
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{"results": results, "deleted": len(results)})
}

// ============================================================================
// ADAPTER METADATA ENDPOINTS
// ============================================================================

// GET /api/v1/marketplace/adapters
// Legacy endpoint — returns in-process metadata only (no Firestore merge).
func (h *MarketplaceHandler) ListAdapters(c *gin.Context) {
	adapters := marketplace.ListAllMetadata()
	c.JSON(http.StatusOK, gin.H{"data": adapters})
}

// GET /api/v1/marketplace/registry
// Returns the merged marketplace registry: in-process defaults overlaid with
// Firestore values from the `marketplaces` collection.  The Firestore document
// wins for: is_active, description, thumbnail_url, image_url, sort_order,
// credential_fields, adapter_type.  Channels present in Firestore but NOT in
// the in-process registry are included as "coming soon" entries (is_active may
// be false).
func (h *MarketplaceHandler) GetRegistry(c *gin.Context) {
	ctx := c.Request.Context()

	// Start with the in-process defaults keyed by ID.
	inProcess := marketplace.ListAllMetadata()
	byID := make(map[string]marketplace.AdapterMetadata, len(inProcess))
	for _, m := range inProcess {
		byID[m.ID] = m
	}

	// Overlay Firestore values when the client is available.
	if h.fsClient != nil {
		iter := h.fsClient.Collection("marketplace_registry").Documents(ctx)
		defer iter.Stop()
		for {
			snap, err := iter.Next()
			if err != nil {
				break // iterator.Done or real error — either way stop
			}
			var fs marketplace.AdapterMetadata
			if err := snap.DataTo(&fs); err != nil {
				log.Printf("[marketplace] registry: failed to decode %s: %v", snap.Ref.ID, err)
				continue
			}
			// Use Firestore document ID as canonical ID when the struct field is blank.
			if fs.ID == "" {
				fs.ID = snap.Ref.ID
			}

			base, exists := byID[fs.ID]
			if exists {
				// Merge: Firestore wins for the admin-managed fields.
				base.IsActive = fs.IsActive
				if fs.Description != "" {
					base.Description = fs.Description
				}
				if fs.ThumbnailURL != "" {
					base.ThumbnailURL = fs.ThumbnailURL
				}
				if fs.ImageURL != "" {
					base.ImageURL = fs.ImageURL
				}
				if fs.SortOrder != 0 {
					base.SortOrder = fs.SortOrder
				}
				if len(fs.CredentialFields) > 0 {
					base.CredentialFields = fs.CredentialFields
				}
				if fs.AdapterType != "" {
					base.AdapterType = fs.AdapterType
				}
				if fs.DisplayName != "" {
					base.DisplayName = fs.DisplayName
				}
				byID[fs.ID] = base
			} else {
				// Channel exists in Firestore but not in the in-process registry —
				// include it so it can appear as a future/coming-soon entry.
				byID[fs.ID] = fs
			}
		}
	}

	// Collect, sort (by sort_order then display_name), and return.
	result := make([]marketplace.AdapterMetadata, 0, len(byID))
	for _, m := range byID {
		result = append(result, m)
	}
	sortMarketplaces(result)

	c.JSON(http.StatusOK, gin.H{"data": result, "total": len(result)})
}

// PUT /api/v1/admin/marketplace/:id
// Upserts the Firestore document for a single marketplace, allowing admins to
// toggle is_active, set thumbnail_url / image_url, sort_order, description, etc.
// Protected by admin middleware — not accessible to regular tenant users.
func (h *MarketplaceHandler) AdminUpsertMarketplace(c *gin.Context) {
	if h.fsClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Firestore not available"})
		return
	}
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "marketplace id required"})
		return
	}

	var body marketplace.AdapterMetadata
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	body.ID = id // ensure the ID field matches the document path

	_, err := h.fsClient.Collection("marketplace_registry").Doc(id).Set(c.Request.Context(), body)
	if err != nil {
		log.Printf("[marketplace] admin upsert %s: %v", id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save marketplace"})
		return
	}

	log.Printf("[marketplace] admin upserted marketplace: %s (is_active=%v)", id, body.IsActive)
	c.JSON(http.StatusOK, gin.H{"message": "marketplace updated", "id": id, "data": body})
}

// sortMarketplaces sorts by SortOrder asc (0 treated as last), then DisplayName.
func sortMarketplaces(ms []marketplace.AdapterMetadata) {
	for i := 0; i < len(ms); i++ {
		for j := i + 1; j < len(ms); j++ {
			li, lj := ms[i].SortOrder, ms[j].SortOrder
			if li == 0 { li = 9999 }
			if lj == 0 { lj = 9999 }
			less := li < lj || (li == lj && ms[i].DisplayName < ms[j].DisplayName)
			if !less {
				ms[i], ms[j] = ms[j], ms[i]
			}
		}
	}
}

// GET /api/v1/marketplace/adapters/:id/fields
func (h *MarketplaceHandler) GetAdapterFields(c *gin.Context) {
	adapterID := c.Param("id")

	metadata, err := marketplace.GetMetadata(adapterID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Adapter not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": metadata})
}

// POST /api/v1/marketplace/listings/bulk/revise
func (h *MarketplaceHandler) BulkReviseListings(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req models.BulkReviseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if len(req.ListingIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No listing IDs provided"})
		return
	}
	if len(req.Fields) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "At least one field must be selected"})
		return
	}
	// Validate that price field has a value when selected
	for _, f := range req.Fields {
		if f == "price" && req.FieldValues.Price == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "field_values.price is required when 'price' is in fields"})
			return
		}
	}

	result, err := h.listingService.BulkReviseListings(
		c.Request.Context(), tenantID, req.ListingIDs, req.Fields, req.FieldValues,
	)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// ============================================================================
// BULK REVISE PREVIEW — USP-03
// ============================================================================

// BulkRevisePreviewItem describes the per-listing diff shown before applying.
type BulkRevisePreviewItem struct {
	ListingID string                 `json:"listing_id"`
	Title     string                 `json:"title"`
	Channel   string                 `json:"channel"`
	Current   map[string]interface{} `json:"current"`
	Proposed  map[string]interface{} `json:"proposed"`
}

// POST /api/v1/marketplace/listings/bulk/revise/preview
//
// Same request body as the real revise endpoint. Reads each listing from
// Firestore, computes the current vs proposed diff for the requested fields,
// and returns it without writing anything. Max 50 listings.
func (h *MarketplaceHandler) BulkRevisePreview(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req models.BulkReviseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if len(req.ListingIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no listing IDs provided"})
		return
	}
	if len(req.ListingIDs) > 50 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "preview supports up to 50 listings at a time"})
		return
	}
	if len(req.Fields) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "at least one field must be selected"})
		return
	}

	fieldSet := make(map[string]bool, len(req.Fields))
	for _, f := range req.Fields {
		fieldSet[f] = true
	}

	previews := make([]BulkRevisePreviewItem, 0, len(req.ListingIDs))

	for _, listingID := range req.ListingIDs {
		listing, err := h.listingService.GetListing(c.Request.Context(), tenantID, listingID)
		if err != nil {
			// Include a placeholder so the frontend knows this listing was skipped.
			previews = append(previews, BulkRevisePreviewItem{
				ListingID: listingID,
				Title:     "(listing not found)",
				Channel:   "",
				Current:   map[string]interface{}{},
				Proposed:  map[string]interface{}{},
			})
			continue
		}

		current := map[string]interface{}{}
		proposed := map[string]interface{}{}

		// Extract current values from overrides (with safe nil guard).
		if fieldSet["title"] {
			cur := ""
			if listing.Overrides != nil {
				cur = listing.Overrides.Title
			}
			current["title"] = cur
			proposed["title"] = req.FieldValues.Title
		}
		if fieldSet["description"] {
			cur := ""
			if listing.Overrides != nil {
				cur = listing.Overrides.Description
			}
			current["description"] = cur
			proposed["description"] = req.FieldValues.Description
		}
		if fieldSet["price"] && req.FieldValues.Price != nil {
			var cur *float64
			if listing.Overrides != nil {
				cur = listing.Overrides.Price
			}
			if cur != nil {
				current["price"] = fmt.Sprintf("%.2f", *cur)
			} else {
				current["price"] = "(not set)"
			}
			proposed["price"] = fmt.Sprintf("%.2f", *req.FieldValues.Price)
		}
		if fieldSet["attributes"] {
			cur := map[string]interface{}{}
			if listing.Overrides != nil && listing.Overrides.Attributes != nil {
				cur = listing.Overrides.Attributes
			}
			current["attributes"] = cur
			if req.FieldValues.Attributes != nil {
				proposed["attributes"] = req.FieldValues.Attributes
			} else {
				proposed["attributes"] = map[string]interface{}{}
			}
		}
		if fieldSet["images"] {
			cur := []string{}
			if listing.Overrides != nil && len(listing.Overrides.Images) > 0 {
				cur = listing.Overrides.Images
			}
			current["images"] = cur
			proposed["images"] = req.FieldValues.Images
		}

		// Build a human-readable title from overrides, falling back to product.
		displayTitle := listing.ListingID
		if listing.Overrides != nil && listing.Overrides.Title != "" {
			displayTitle = listing.Overrides.Title
		}

		previews = append(previews, BulkRevisePreviewItem{
			ListingID: listingID,
			Title:     displayTitle,
			Channel:   listing.Channel,
			Current:   current,
			Proposed:  proposed,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":       true,
		"count":    len(previews),
		"previews": previews,
	})
}
