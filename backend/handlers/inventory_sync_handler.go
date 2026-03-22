package handlers

// ============================================================================
// INVENTORY SYNC HANDLER
// ============================================================================
// Handles manual and scheduled inventory stock-level pushes to channels.
//
// Endpoints:
//   POST /inventory-sync/trigger        - Trigger sync for one credential
//   POST /inventory-sync/trigger-all    - Trigger sync for all enabled credentials
//   GET  /inventory-sync/logs           - List recent sync log entries
//
// Background job: runs every 15 minutes via InventorySyncScheduler.Run()
// wired up in main.go.
//
// Firestore collections written:
//   tenants/{tenantID}/inventory_sync_log/{logID}
// ============================================================================

import (
	"context"
	"log"
	"net/http"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"
	"module-a/marketplace"
	"module-a/models"
)

// ── Handler ───────────────────────────────────────────────────────────────────

type InventorySyncHandler struct {
	client *firestore.Client
}

func NewInventorySyncHandler(client *firestore.Client) *InventorySyncHandler {
	return &InventorySyncHandler{client: client}
}

// ── Data structures ───────────────────────────────────────────────────────────

// InventorySyncLogEntry records the result of a single product sync attempt
// for one credential. Stored in inventory_sync_log collection.
type InventorySyncLogEntry struct {
	LogID        string    `firestore:"log_id" json:"log_id"`
	TenantID     string    `firestore:"tenant_id" json:"tenant_id"`
	CredentialID string    `firestore:"credential_id" json:"credential_id"`
	Channel      string    `firestore:"channel" json:"channel"`
	ProductID    string    `firestore:"product_id" json:"product_id"`
	SKU          string    `firestore:"sku" json:"sku"`
	ExternalID   string    `firestore:"external_id" json:"external_id"`
	QuantitySent int       `firestore:"quantity_sent" json:"quantity_sent"`
	Status       string    `firestore:"status" json:"status"` // success|error|skipped
	Error        string    `firestore:"error,omitempty" json:"error,omitempty"`
	CreatedAt    time.Time `firestore:"created_at" json:"created_at"`
}

// ── POST /api/v1/inventory-sync/trigger ──────────────────────────────────────
// Manually trigger a full stock push for a specific credential.

func (h *InventorySyncHandler) TriggerSync(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()

	var req struct {
		CredentialID string `json:"credential_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Load credential
	cred, err := h.loadCredential(ctx, tenantID, req.CredentialID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Credential not found: " + err.Error()})
		return
	}

	// Run in background so the HTTP request returns immediately
	go func() {
		bgCtx := context.Background()
		results := h.syncCredential(bgCtx, tenantID, cred)
		log.Printf("[InventorySync] Manual trigger for %s (%s): %d products synced", cred.AccountName, cred.Channel, results)
	}()

	c.JSON(http.StatusOK, gin.H{
		"ok":            true,
		"message":       "Inventory sync triggered",
		"credential_id": req.CredentialID,
	})
}

// ── POST /api/v1/inventory-sync/trigger-all ───────────────────────────────────
// Trigger inventory sync for all active credentials with inventory_sync_enabled.

func (h *InventorySyncHandler) TriggerAll(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()

	creds, err := h.loadInventorySyncEnabledCredentials(ctx, tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	go func() {
		bgCtx := context.Background()
		total := 0
		for _, cred := range creds {
			total += h.syncCredential(bgCtx, tenantID, &cred)
		}
		log.Printf("[InventorySync] trigger-all for tenant %s: %d credentials, %d products synced", tenantID, len(creds), total)
	}()

	c.JSON(http.StatusOK, gin.H{
		"ok":          true,
		"message":     "Inventory sync triggered for all enabled channels",
		"credentials": len(creds),
	})
}

// ── GET /api/v1/inventory-sync/logs ──────────────────────────────────────────
// Returns recent sync log entries for the tenant.

func (h *InventorySyncHandler) GetLogs(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()

	credentialID := c.Query("credential_id")
	since := time.Now().Add(-24 * time.Hour)

	query := h.client.Collection("tenants").Doc(tenantID).
		Collection("inventory_sync_log").
		Where("created_at", ">=", since).
		OrderBy("created_at", firestore.Desc).
		Limit(200)

	if credentialID != "" {
		query = h.client.Collection("tenants").Doc(tenantID).
			Collection("inventory_sync_log").
			Where("credential_id", "==", credentialID).
			Where("created_at", ">=", since).
			OrderBy("created_at", firestore.Desc).
			Limit(200)
	}

	iter := query.Documents(ctx)
	var logs []InventorySyncLogEntry
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		var entry InventorySyncLogEntry
		if err := doc.DataTo(&entry); err == nil {
			logs = append(logs, entry)
		}
	}
	iter.Stop()

	if logs == nil {
		logs = []InventorySyncLogEntry{}
	}

	c.JSON(http.StatusOK, gin.H{"data": logs})
}

// ── Core sync logic ───────────────────────────────────────────────────────────

// syncCredential pushes stock levels for all listings under this credential.
// Returns the number of products attempted.
func (h *InventorySyncHandler) syncCredential(ctx context.Context, tenantID string, cred *models.MarketplaceCredential) int {
	// Load the channel config to get inventory sync settings
	cfgDoc, err := h.client.Collection("tenants").Doc(tenantID).
		Collection("marketplace_credentials").Doc(cred.CredentialID).
		Collection("config").Doc("channel_config").Get(ctx)

	var invSync models.ChannelInventorySyncConfig
	if err == nil {
		var cfg models.ChannelConfig
		if merr := cfgDoc.DataTo(&cfg); merr == nil {
			invSync = cfg.InventorySync
		}
	}
	// Also try loading config directly from the credential doc's config sub-field
	// (the actual storage location used by UpdateCredentialConfig)
	credDoc, err := h.client.Collection("tenants").Doc(tenantID).
		Collection("marketplace_credentials").Doc(cred.CredentialID).Get(ctx)
	if err == nil {
		raw := credDoc.Data()
		if cfgRaw, ok := raw["config"].(map[string]interface{}); ok {
			if invRaw, ok := cfgRaw["inventory_sync"].(map[string]interface{}); ok {
				if v, ok := invRaw["update_inventory"].(bool); ok {
					invSync.UpdateInventory = v
				}
				if v, ok := invRaw["max_quantity_to_sync"].(int64); ok {
					invSync.MaxQuantityToSync = int(v)
				}
				if v, ok := invRaw["min_stock_level"].(int64); ok {
					invSync.MinStockLevel = int(v)
				}
				if v, ok := invRaw["latency_buffer_days"].(int64); ok {
					invSync.LatencyBufferDays = int(v)
				}
				if locs, ok := invRaw["location_ids"].([]interface{}); ok {
					for _, l := range locs {
						if ls, ok := l.(string); ok {
							invSync.LocationIDs = append(invSync.LocationIDs, ls)
						}
					}
				}
			}
		}
	}

	if !invSync.UpdateInventory {
		log.Printf("[InventorySync] Skipping %s (%s) — inventory sync not enabled", cred.AccountName, cred.Channel)
		return 0
	}

	// Build channel adapter
	credData, err := h.loadDecryptedCredentials(ctx, tenantID, cred)
	if err != nil {
		log.Printf("[InventorySync] Failed to load credentials for %s: %v", cred.CredentialID, err)
		return 0
	}

	adapter, err := marketplace.GetAdapter(ctx, cred.Channel, marketplace.Credentials{
		MarketplaceID:   cred.Channel,
		Environment:     cred.Environment,
		MarketplaceType: cred.Channel,
		Data:            credData,
	})
	if err != nil {
		log.Printf("[InventorySync] Failed to get adapter for %s: %v", cred.Channel, err)
		return 0
	}

	// Fetch listings for this credential
	iter := h.client.Collection("tenants").Doc(tenantID).
		Collection("listings").
		Where("credential_id", "==", cred.CredentialID).
		Where("status", "==", "active").
		Limit(500).
		Documents(ctx)

	count := 0
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}

		data := doc.Data()
		externalID := getString(data, "external_id")
		sku := getString(data, "sku")
		productID := getString(data, "product_id")

		if externalID == "" {
			continue
		}

		// Get available stock from inventory
		available := h.getAvailableStock(ctx, tenantID, sku, productID, invSync)

		// Apply min/max rules
		qty := available
		if invSync.MinStockLevel > 0 && qty < invSync.MinStockLevel {
			qty = 0 // push 0 rather than below min
		}
		if invSync.MaxQuantityToSync > 0 && qty > invSync.MaxQuantityToSync {
			qty = invSync.MaxQuantityToSync
		}

		// Push to channel
		syncErr := adapter.SyncInventory(ctx, externalID, qty)

		entry := InventorySyncLogEntry{
			LogID:        uuid.New().String(),
			TenantID:     tenantID,
			CredentialID: cred.CredentialID,
			Channel:      cred.Channel,
			ProductID:    productID,
			SKU:          sku,
			ExternalID:   externalID,
			QuantitySent: qty,
			CreatedAt:    time.Now(),
		}
		if syncErr != nil {
			entry.Status = "error"
			entry.Error = syncErr.Error()
			log.Printf("[InventorySync] Error syncing %s on %s: %v", externalID, cred.Channel, syncErr)
		} else {
			entry.Status = "success"
		}

		// Write log entry (best effort)
		h.client.Collection("tenants").Doc(tenantID).
			Collection("inventory_sync_log").Doc(entry.LogID).Set(ctx, entry)

		count++
	}
	iter.Stop()

	return count
}

// getAvailableStock reads the inventory item for a SKU and returns available stock,
// optionally filtered to specific locations and with latency buffer applied.
func (h *InventorySyncHandler) getAvailableStock(ctx context.Context, tenantID, sku, productID string, cfg models.ChannelInventorySyncConfig) int {
	// Query inventory by SKU
	query := h.client.Collection("tenants").Doc(tenantID).Collection("inventory").
		Where("sku", "==", sku).Limit(1)

	iter := query.Documents(ctx)
	doc, err := iter.Next()
	iter.Stop()
	if err != nil {
		// Try by product_id if SKU not found
		if productID != "" {
			query2 := h.client.Collection("tenants").Doc(tenantID).Collection("inventory").
				Where("product_id", "==", productID).Limit(1)
			iter2 := query2.Documents(ctx)
			doc, err = iter2.Next()
			iter2.Stop()
			if err != nil {
				return 0
			}
		} else {
			return 0
		}
	}

	data := doc.Data()

	// If specific locations are configured, sum only those
	if len(cfg.LocationIDs) > 0 {
		if locs, ok := data["locations"].([]interface{}); ok {
			locationSet := make(map[string]bool)
			for _, id := range cfg.LocationIDs {
				locationSet[id] = true
			}
			total := 0
			for _, l := range locs {
				if lm, ok := l.(map[string]interface{}); ok {
					locID := getString(lm, "location_id")
					if locationSet[locID] {
						if avail, ok := lm["available"].(int64); ok {
							total += int(avail)
						}
					}
				}
			}
			// Subtract active reservations
			reserved := GetReservedQuantity(ctx, h.client, tenantID, productID, sku)
			total -= reserved
			if total < 0 {
				total = 0
			}
			return h.applyLatencyBuffer(total, cfg)
		}
	}

	// Otherwise use total available
	available := 0
	if v, ok := data["total_available"].(int64); ok {
		available = int(v)
	}

	// Subtract active stock reservations to prevent overselling
	reserved := GetReservedQuantity(ctx, h.client, tenantID, productID, sku)
	available -= reserved
	if available < 0 {
		available = 0
	}

	return h.applyLatencyBuffer(available, cfg)
}

// applyLatencyBuffer reduces stock by estimated N days of sales velocity.
// Currently uses a simple deduction of latency_buffer_days as a flat unit;
// a full velocity calculation would require historical sales data.
func (h *InventorySyncHandler) applyLatencyBuffer(available int, cfg models.ChannelInventorySyncConfig) int {
	buffer := cfg.LatencyBufferDays
	if buffer == 0 {
		buffer = cfg.DefaultLatencyDays
	}
	if buffer > 0 {
		// Simple flat buffer: reduce by buffer days as units
		// In production this would multiply by daily sales velocity
		available -= buffer
	}
	if available < 0 {
		available = 0
	}
	return available
}

// loadCredential fetches a single credential doc from Firestore.
func (h *InventorySyncHandler) loadCredential(ctx context.Context, tenantID, credentialID string) (*models.MarketplaceCredential, error) {
	doc, err := h.client.Collection("tenants").Doc(tenantID).
		Collection("marketplace_credentials").Doc(credentialID).Get(ctx)
	if err != nil {
		return nil, err
	}
	var cred models.MarketplaceCredential
	if err := doc.DataTo(&cred); err != nil {
		return nil, err
	}
	return &cred, nil
}

// loadInventorySyncEnabledCredentials returns active credentials with inventory_sync_enabled=true.
func (h *InventorySyncHandler) loadInventorySyncEnabledCredentials(ctx context.Context, tenantID string) ([]models.MarketplaceCredential, error) {
	iter := h.client.Collection("tenants").Doc(tenantID).
		Collection("marketplace_credentials").
		Where("active", "==", true).
		Where("inventory_sync_enabled", "==", true).
		Documents(ctx)

	var creds []models.MarketplaceCredential
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			continue
		}
		var cred models.MarketplaceCredential
		if err := doc.DataTo(&cred); err == nil {
			creds = append(creds, cred)
		}
	}
	iter.Stop()
	return creds, nil
}

// loadDecryptedCredentials retrieves the plaintext credential fields for an adapter.
// Uses the encrypted_credentials map from the credential doc.
func (h *InventorySyncHandler) loadDecryptedCredentials(ctx context.Context, tenantID string, cred *models.MarketplaceCredential) (map[string]string, error) {
	doc, err := h.client.Collection("tenants").Doc(tenantID).
		Collection("marketplace_credentials").Doc(cred.CredentialID).Get(ctx)
	if err != nil {
		return nil, err
	}
	data := doc.Data()
	result := make(map[string]string)

	// Try plaintext credentials map first
	if cm, ok := data["credentials"].(map[string]interface{}); ok {
		for k, v := range cm {
			if s, ok := v.(string); ok {
				result[k] = s
			}
		}
	}

	// Add top-level fields that adapters commonly need
	for _, field := range []string{"marketplace_id", "seller_id", "environment"} {
		if v, ok := data[field].(string); ok && v != "" {
			result[field] = v
		}
	}

	return result, nil
}

// ── Scheduler ─────────────────────────────────────────────────────────────────

// InventorySyncScheduler runs a background goroutine that triggers inventory
// sync for all tenants' enabled channels every 15 minutes.
type InventorySyncScheduler struct {
	handler *InventorySyncHandler
	fsClient *firestore.Client
}

func NewInventorySyncScheduler(client *firestore.Client, handler *InventorySyncHandler) *InventorySyncScheduler {
	return &InventorySyncScheduler{handler: handler, fsClient: client}
}

func (s *InventorySyncScheduler) Run() {
	go func() {
		// Initial delay to allow server warm-up
		time.Sleep(3 * time.Minute)

		ticker := time.NewTicker(15 * time.Minute)
		defer ticker.Stop()

		s.runAllTenants()

		for range ticker.C {
			s.runAllTenants()
		}
	}()
}

func (s *InventorySyncScheduler) runAllTenants() {
	ctx := context.Background()
	log.Println("[InventorySync] Scheduled run starting...")

	tenantsIter := s.fsClient.Collection("tenants").Documents(ctx)
	tenantCount := 0
	productCount := 0

	for {
		tenantDoc, err := tenantsIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			continue
		}
		tenantID := tenantDoc.Ref.ID

		creds, err := s.handler.loadInventorySyncEnabledCredentials(ctx, tenantID)
		if err != nil || len(creds) == 0 {
			continue
		}

		tenantCount++
		for i := range creds {
			productCount += s.handler.syncCredential(ctx, tenantID, &creds[i])
		}
	}

	log.Printf("[InventorySync] Scheduled run complete: %d tenants, %d products synced", tenantCount, productCount)
}
