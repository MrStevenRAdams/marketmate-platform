package handlers

// ============================================================================
// PRICE SYNC HANDLER  —  P1.8
// ============================================================================
// Allows users to define price sync rules: when a product's retail_price
// changes, automatically push a new price to one or more channel listings.
//
// Data model: tenants/{tenant_id}/price_sync_rules/{rule_id}
//
// Routes (register in main.go):
//   GET    /api/v1/price-sync/rules
//   POST   /api/v1/price-sync/rules
//   PUT    /api/v1/price-sync/rules/:id
//   DELETE /api/v1/price-sync/rules/:id
//   POST   /api/v1/price-sync/trigger          manually trigger sync for products
//   GET    /api/v1/price-sync/log              recent sync log entries
// ============================================================================

import (
	"context"
	"fmt"
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

type PriceSyncHandler struct {
	client *firestore.Client
}

func NewPriceSyncHandler(client *firestore.Client) *PriceSyncHandler {
	return &PriceSyncHandler{client: client}
}

// syncPriceToChannel loads the marketplace adapter for a credential and
// calls SyncPrice for a single listing. This is the real channel API call.
func (h *PriceSyncHandler) syncPriceToChannel(ctx context.Context, tenantID, credentialID, channel, externalID string, price float64) error {
	// Load decrypted credential fields
	credDoc, err := h.client.Collection("tenants").Doc(tenantID).
		Collection("marketplace_credentials").Doc(credentialID).Get(ctx)
	if err != nil {
		return fmt.Errorf("load credential: %w", err)
	}
	var cred models.MarketplaceCredential
	if err := credDoc.DataTo(&cred); err != nil {
		return fmt.Errorf("parse credential: %w", err)
	}

	// Build plaintext credential map
	credData := make(map[string]string)
	if cm, ok := credDoc.Data()["credentials"].(map[string]interface{}); ok {
		for k, v := range cm {
			if s, ok := v.(string); ok {
				credData[k] = s
			}
		}
	}
	for _, field := range []string{"marketplace_id", "seller_id", "environment"} {
		if v, ok := credDoc.Data()[field].(string); ok && v != "" {
			credData[field] = v
		}
	}

	adapter, err := marketplace.GetAdapter(ctx, channel, marketplace.Credentials{
		MarketplaceID:   channel,
		Environment:     cred.Environment,
		MarketplaceType: channel,
		Data:            credData,
	})
	if err != nil {
		return fmt.Errorf("get adapter for %s: %w", channel, err)
	}

	return adapter.SyncPrice(ctx, externalID, price)
}

// PriceSyncScheduler runs a daily price reconciliation pass across all enabled rules.
type PriceSyncScheduler struct {
	handler  *PriceSyncHandler
	fsClient *firestore.Client
}

func NewPriceSyncScheduler(client *firestore.Client, handler *PriceSyncHandler) *PriceSyncScheduler {
	return &PriceSyncScheduler{handler: handler, fsClient: client}
}

// Run starts a daily price sync goroutine that fires at 01:00 UTC every day.
// (Offset from midnight to avoid contention with inventory full reconciliation at 00:02.)
func (s *PriceSyncScheduler) Run() {
	go func() {
		for {
			now := time.Now().UTC()
			nextRun := time.Date(now.Year(), now.Month(), now.Day()+1, 1, 0, 0, 0, time.UTC)
			wait := nextRun.Sub(now)
			log.Printf("[PriceSync] Daily run scheduled in %s (at %s UTC)",
				wait.Round(time.Minute).String(), nextRun.Format("2006-01-02 15:04"))
			timer := time.NewTimer(wait)
			<-timer.C
			timer.Stop()
			log.Println("[PriceSync] Starting daily price reconciliation...")
			s.runAllTenants()
			log.Println("[PriceSync] Daily price reconciliation complete")
		}
	}()
}

func (s *PriceSyncScheduler) runAllTenants() {
	ctx := context.Background()
	tenantsIter := s.fsClient.Collection("tenants").Documents(ctx)
	for {
		tenantDoc, err := tenantsIter.Next()
		if err != nil {
			break
		}
		tenantID := tenantDoc.Ref.ID
		// Trigger sync for all enabled rules for this tenant
		s.handler.syncAllRulesForTenant(ctx, tenantID)
	}
}

// syncAllRulesForTenant applies all enabled price rules for a tenant.
// Called by both the daily scheduler and the manual trigger endpoint.
func (h *PriceSyncHandler) syncAllRulesForTenant(ctx context.Context, tenantID string) {
	rulesIter := h.ruleCol(tenantID).Where("enabled", "==", true).Documents(ctx)
	defer rulesIter.Stop()
	for {
		d, err := rulesIter.Next()
		if err != nil {
			break
		}
		var rule PriceSyncRule
		if err := d.DataTo(&rule); err != nil {
			continue
		}
		h.applyRuleToListings(ctx, tenantID, &rule)
	}
}

// applyRuleToListings iterates all active listings for a rule's credential
// and pushes the adjusted price to the channel via the marketplace adapter.
func (h *PriceSyncHandler) applyRuleToListings(ctx context.Context, tenantID string, rule *PriceSyncRule) {
	// Load relevant listings (scoped or all)
	listingQuery := h.client.Collection("tenants").Doc(tenantID).
		Collection("listings").
		Where("credential_id", "==", rule.CredentialID).
		Where("status", "==", "active").
		Limit(500)

	iter := listingQuery.Documents(ctx)
	defer iter.Stop()

	now := time.Now()
	synced, failed := 0, 0

	for {
		doc, err := iter.Next()
		if err != nil {
			break
		}
		data := doc.Data()
		externalID, _ := data["external_id"].(string)
		sku, _ := data["sku"].(string)
		productID, _ := data["product_id"].(string)

		if externalID == "" {
			continue
		}

		// Skip if rule has a product scope and this listing is not in it
		if !rule.ApplyToAll && len(rule.ProductIDs) > 0 {
			inScope := false
			for _, pid := range rule.ProductIDs {
				if pid == productID {
					inScope = true
					break
				}
			}
			if !inScope {
				continue
			}
		}

		// Resolve base price from the listing or its product
		basePrice := h.resolveBasePrice(ctx, tenantID, productID, data)
		if basePrice <= 0 {
			continue
		}

		// Apply adjustment and rounding
		newPrice := applyPriceAdj(basePrice, rule.PriceAdjType, rule.PriceAdjValue, rule.RoundTo)
		if newPrice <= 0 {
			continue
		}

		// Push to channel via adapter
		syncErr := h.syncPriceToChannel(ctx, tenantID, rule.CredentialID, rule.Channel, externalID, newPrice)

		entry := PriceSyncLogEntry{
			LogID:     "psl_" + uuid.New().String(),
			TenantID:  tenantID,
			RuleID:    rule.RuleID,
			RuleName:  rule.Name,
			ProductID: productID,
			SKU:       sku,
			OldPrice:  basePrice,
			NewPrice:  newPrice,
			Channel:   rule.Channel,
			CreatedAt: now,
		}
		if syncErr != nil {
			entry.Status = "error"
			entry.ErrorMessage = syncErr.Error()
			log.Printf("[PriceSync] error syncing %s on %s for rule %s: %v",
				externalID, rule.Channel, rule.RuleID, syncErr)
			failed++
		} else {
			entry.Status = "success"
			synced++
		}
		h.logCol(tenantID).Doc(entry.LogID).Set(ctx, entry)
	}

	// Update last_run_at on the rule
	h.ruleCol(tenantID).Doc(rule.RuleID).Update(ctx, []firestore.Update{
		{Path: "last_run_at", Value: now},
	})
	log.Printf("[PriceSync] rule %s (%s): %d synced, %d failed", rule.RuleID, rule.Channel, synced, failed)
}

// resolveBasePrice determines the base price for a listing.
// It first checks the listing doc itself, then falls back to the product record.
func (h *PriceSyncHandler) resolveBasePrice(ctx context.Context, tenantID, productID string, listingData map[string]interface{}) float64 {
	// Try listing price first (the price currently shown on the channel)
	for _, key := range []string{"price", "list_price", "retail_price"} {
		switch v := listingData[key].(type) {
		case float64:
			if v > 0 {
				return v
			}
		case int64:
			if v > 0 {
				return float64(v)
			}
		}
	}

	// Fall back to the product record
	if productID == "" {
		return 0
	}
	prodDoc, err := h.client.Collection("tenants").Doc(tenantID).
		Collection("products").Doc(productID).Get(ctx)
	if err != nil {
		return 0
	}
	prodData := prodDoc.Data()
	for _, key := range []string{"retail_price", "list_price", "price"} {
		switch v := prodData[key].(type) {
		case float64:
			if v > 0 {
				return v
			}
		case int64:
			if v > 0 {
				return float64(v)
			}
		}
	}

	// Also check the source_price stored in attributes map
	if attrs, ok := prodData["attributes"].(map[string]interface{}); ok {
		if v, ok := attrs["source_price"].(float64); ok && v > 0 {
			return v
		}
	}
	return 0
}

// ── Data models ───────────────────────────────────────────────────────────────

type PriceSyncRule struct {
	RuleID          string    `firestore:"rule_id"          json:"rule_id"`
	TenantID        string    `firestore:"tenant_id"        json:"tenant_id"`
	Name            string    `firestore:"name"             json:"name"`
	Enabled         bool      `firestore:"enabled"          json:"enabled"`
	CredentialID    string    `firestore:"credential_id"    json:"credential_id"`    // which channel account
	Channel         string    `firestore:"channel"          json:"channel"`           // amazon|ebay|shopify|…
	PriceAdjType    string    `firestore:"price_adj_type"   json:"price_adj_type"`    // none|percent|fixed
	PriceAdjValue   float64   `firestore:"price_adj_value"  json:"price_adj_value"`   // e.g. 5.0 = +5%
	RoundTo         float64   `firestore:"round_to"         json:"round_to"`           // e.g. 0.99 or 0.00
	ApplyToAll      bool      `firestore:"apply_to_all"     json:"apply_to_all"`      // all SKUs or scoped
	ProductIDs      []string  `firestore:"product_ids"      json:"product_ids"`        // if not apply_to_all
	LastRunAt       *time.Time `firestore:"last_run_at"     json:"last_run_at,omitempty"`
	CreatedAt       time.Time `firestore:"created_at"       json:"created_at"`
	UpdatedAt       time.Time `firestore:"updated_at"       json:"updated_at"`
}

type PriceSyncLogEntry struct {
	LogID        string    `firestore:"log_id"        json:"log_id"`
	TenantID     string    `firestore:"tenant_id"     json:"tenant_id"`
	RuleID       string    `firestore:"rule_id"       json:"rule_id"`
	RuleName     string    `firestore:"rule_name"     json:"rule_name"`
	ProductID    string    `firestore:"product_id"    json:"product_id"`
	SKU          string    `firestore:"sku"           json:"sku"`
	OldPrice     float64   `firestore:"old_price"     json:"old_price"`
	NewPrice     float64   `firestore:"new_price"     json:"new_price"`
	Channel      string    `firestore:"channel"       json:"channel"`
	Status       string    `firestore:"status"        json:"status"` // success|error|skipped
	ErrorMessage string    `firestore:"error_message" json:"error_message,omitempty"`
	CreatedAt    time.Time `firestore:"created_at"    json:"created_at"`
}

func (h *PriceSyncHandler) ruleCol(tenantID string) *firestore.CollectionRef {
	return h.client.Collection("tenants").Doc(tenantID).Collection("price_sync_rules")
}

func (h *PriceSyncHandler) logCol(tenantID string) *firestore.CollectionRef {
	return h.client.Collection("tenants").Doc(tenantID).Collection("price_sync_log")
}

// ── GET /api/v1/price-sync/rules ─────────────────────────────────────────────

func (h *PriceSyncHandler) ListRules(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	iter := h.ruleCol(tenantID).OrderBy("created_at", firestore.Desc).Documents(c.Request.Context())
	defer iter.Stop()

	var rules []PriceSyncRule
	for {
		d, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list rules"})
			return
		}
		var r PriceSyncRule
		d.DataTo(&r)
		rules = append(rules, r)
	}
	if rules == nil {
		rules = []PriceSyncRule{}
	}
	c.JSON(http.StatusOK, gin.H{"rules": rules, "count": len(rules)})
}

// ── POST /api/v1/price-sync/rules ────────────────────────────────────────────

func (h *PriceSyncHandler) CreateRule(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	var req struct {
		Name          string   `json:"name" binding:"required"`
		CredentialID  string   `json:"credential_id" binding:"required"`
		Channel       string   `json:"channel" binding:"required"`
		PriceAdjType  string   `json:"price_adj_type"` // none|percent|fixed
		PriceAdjValue float64  `json:"price_adj_value"`
		RoundTo       float64  `json:"round_to"`
		ApplyToAll    bool     `json:"apply_to_all"`
		ProductIDs    []string `json:"product_ids"`
		Enabled       bool     `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	now := time.Now()
	rule := PriceSyncRule{
		RuleID:        "psr_" + uuid.New().String(),
		TenantID:      tenantID,
		Name:          req.Name,
		Enabled:       req.Enabled,
		CredentialID:  req.CredentialID,
		Channel:       req.Channel,
		PriceAdjType:  req.PriceAdjType,
		PriceAdjValue: req.PriceAdjValue,
		RoundTo:       req.RoundTo,
		ApplyToAll:    req.ApplyToAll,
		ProductIDs:    req.ProductIDs,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if rule.PriceAdjType == "" {
		rule.PriceAdjType = "none"
	}
	if rule.ProductIDs == nil {
		rule.ProductIDs = []string{}
	}
	if _, err := h.ruleCol(tenantID).Doc(rule.RuleID).Set(c.Request.Context(), rule); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create rule"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"rule": rule})
}

// ── PUT /api/v1/price-sync/rules/:id ─────────────────────────────────────────

func (h *PriceSyncHandler) UpdateRule(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ruleID := c.Param("id")
	ctx := c.Request.Context()

	doc, err := h.ruleCol(tenantID).Doc(ruleID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "rule not found"})
		return
	}
	var rule PriceSyncRule
	doc.DataTo(&rule)

	var req struct {
		Name          *string  `json:"name"`
		Enabled       *bool    `json:"enabled"`
		CredentialID  *string  `json:"credential_id"`
		Channel       *string  `json:"channel"`
		PriceAdjType  *string  `json:"price_adj_type"`
		PriceAdjValue *float64 `json:"price_adj_value"`
		RoundTo       *float64 `json:"round_to"`
		ApplyToAll    *bool    `json:"apply_to_all"`
		ProductIDs    []string `json:"product_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Name != nil {
		rule.Name = *req.Name
	}
	if req.Enabled != nil {
		rule.Enabled = *req.Enabled
	}
	if req.CredentialID != nil {
		rule.CredentialID = *req.CredentialID
	}
	if req.Channel != nil {
		rule.Channel = *req.Channel
	}
	if req.PriceAdjType != nil {
		rule.PriceAdjType = *req.PriceAdjType
	}
	if req.PriceAdjValue != nil {
		rule.PriceAdjValue = *req.PriceAdjValue
	}
	if req.RoundTo != nil {
		rule.RoundTo = *req.RoundTo
	}
	if req.ApplyToAll != nil {
		rule.ApplyToAll = *req.ApplyToAll
	}
	if req.ProductIDs != nil {
		rule.ProductIDs = req.ProductIDs
	}
	rule.UpdatedAt = time.Now()

	if _, err := h.ruleCol(tenantID).Doc(ruleID).Set(ctx, rule); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update rule"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"rule": rule})
}

// ── DELETE /api/v1/price-sync/rules/:id ──────────────────────────────────────

func (h *PriceSyncHandler) DeleteRule(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ruleID := c.Param("id")
	if _, err := h.ruleCol(tenantID).Doc(ruleID).Delete(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete rule"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}

// ── POST /api/v1/price-sync/trigger ──────────────────────────────────────────
// Manually trigger price sync for a list of product IDs (or all if empty).
// Reads current retail_price from each product, applies rule adjustments,
// then writes a log entry and updates the listing price field.

func (h *PriceSyncHandler) TriggerSync(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()

	var req struct {
		ProductIDs []string `json:"product_ids"` // empty = sync all
		RuleID     string   `json:"rule_id"`      // optional: specific rule only
	}
	c.ShouldBindJSON(&req)

	// Load enabled rules
	q := h.ruleCol(tenantID).Where("enabled", "==", true)
	if req.RuleID != "" {
		q = h.ruleCol(tenantID).Where("rule_id", "==", req.RuleID)
	}
	ruleIter := q.Documents(ctx)
	defer ruleIter.Stop()

	var rules []PriceSyncRule
	for {
		d, err := ruleIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load rules"})
			return
		}
		var r PriceSyncRule
		d.DataTo(&r)
		rules = append(rules, r)
	}

	if len(rules) == 0 {
		c.JSON(http.StatusOK, gin.H{"synced": 0, "message": "no enabled rules found"})
		return
	}

	synced := 0
	errors := 0
	now := time.Now()

	for _, rule := range rules {
		// Determine which products to sync
		productIDs := req.ProductIDs
		if len(productIDs) == 0 && !rule.ApplyToAll {
			productIDs = rule.ProductIDs
		}

		var products []map[string]interface{}
		if len(productIDs) > 0 {
			for _, pid := range productIDs {
				d, err := h.client.Collection(fmt.Sprintf("tenants/%s/products", tenantID)).Doc(pid).Get(ctx)
				if err == nil {
					data := d.Data()
					data["product_id"] = pid
					products = append(products, data)
				}
			}
		} else {
			// All products
			pIter := h.client.Collection(fmt.Sprintf("tenants/%s/products", tenantID)).
				Limit(500).Documents(ctx)
			for {
				d, err := pIter.Next()
				if err == iterator.Done {
					break
				}
				if err != nil {
					break
				}
				data := d.Data()
				data["product_id"] = d.Ref.ID
				products = append(products, data)
			}
			pIter.Stop()
		}

		for _, product := range products {
			pid, _ := product["product_id"].(string)
			sku, _ := product["sku"].(string)

			// Get current retail price from the product record
			var basePrice float64
			switch v := product["retail_price"].(type) {
			case float64:
				basePrice = v
			case int64:
				basePrice = float64(v)
			}
			// Also check attributes.source_price
			if basePrice <= 0 {
				if attrs, ok := product["attributes"].(map[string]interface{}); ok {
					if v, ok := attrs["source_price"].(float64); ok && v > 0 {
						basePrice = v
					}
				}
			}
			if basePrice <= 0 {
				continue
			}

			// Apply price adjustment rule
			newPrice := applyPriceAdj(basePrice, rule.PriceAdjType, rule.PriceAdjValue, rule.RoundTo)
			if newPrice <= 0 {
				continue
			}

			// Find the external listing ID for this product on this channel
			var externalID string
			listingIter := h.client.Collection(fmt.Sprintf("tenants/%s/listings", tenantID)).
				Where("product_id", "==", pid).
				Where("credential_id", "==", rule.CredentialID).
				Where("status", "==", "active").
				Limit(1).Documents(ctx)
			listingDoc, listingErr := listingIter.Next()
			listingIter.Stop()
			if listingErr == nil && listingDoc != nil {
				externalID, _ = listingDoc.Data()["external_id"].(string)
				if extSKU, ok := listingDoc.Data()["sku"].(string); ok && extSKU != "" {
					// eBay uses the seller SKU for offer lookups
					if externalID == "" {
						externalID = extSKU
					}
				}
			}
			if externalID == "" {
				// No active listing found for this product/credential — skip
				continue
			}

			// Push price to channel via the marketplace adapter
			syncErr := h.syncPriceToChannel(ctx, tenantID, rule.CredentialID, rule.Channel, externalID, newPrice)

			logEntry := PriceSyncLogEntry{
				LogID:        "psl_" + uuid.New().String(),
				TenantID:     tenantID,
				RuleID:       rule.RuleID,
				RuleName:     rule.Name,
				ProductID:    pid,
				SKU:          sku,
				OldPrice:     basePrice,
				NewPrice:     newPrice,
				Channel:      rule.Channel,
				CreatedAt:    now,
			}
			if syncErr != nil {
				logEntry.Status = "error"
				logEntry.ErrorMessage = syncErr.Error()
				log.Printf("[PriceSync] TriggerSync error for %s on %s: %v", externalID, rule.Channel, syncErr)
				errors++
			} else {
				logEntry.Status = "success"
				synced++
			}
			h.logCol(tenantID).Doc(logEntry.LogID).Set(ctx, logEntry)
		}

		// Update last_run_at on the rule
		h.ruleCol(tenantID).Doc(rule.RuleID).Update(ctx, []firestore.Update{
			{Path: "last_run_at", Value: now},
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"synced": synced,
		"errors": errors,
		"rules":  len(rules),
	})
}

// ── GET /api/v1/price-sync/log ────────────────────────────────────────────────

func (h *PriceSyncHandler) GetLog(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	limit := 100
	iter := h.logCol(tenantID).OrderBy("created_at", firestore.Desc).Limit(limit).Documents(c.Request.Context())
	defer iter.Stop()

	var entries []PriceSyncLogEntry
	for {
		d, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load log"})
			return
		}
		var e PriceSyncLogEntry
		d.DataTo(&e)
		entries = append(entries, e)
	}
	if entries == nil {
		entries = []PriceSyncLogEntry{}
	}
	c.JSON(http.StatusOK, gin.H{"entries": entries, "count": len(entries)})
}

// ── helpers ───────────────────────────────────────────────────────────────────

func applyPriceAdj(base float64, adjType string, adjValue float64, roundTo float64) float64 {
	price := base
	switch adjType {
	case "percent":
		price = base * (1 + adjValue/100)
	case "fixed":
		price = base + adjValue
	}
	if price < 0 {
		price = 0
	}
	// Rounding: e.g. roundTo=0.99 → 9.99, 19.99, etc.
	if roundTo > 0 && roundTo < 1 {
		price = float64(int(price)) + roundTo
		if price > base*1.5 && adjValue == 0 {
			price -= 1
		}
	}
	// Round to 2dp
	price = float64(int(price*100+0.5)) / 100
	return price
}
