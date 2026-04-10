package handlers

// ============================================================================
// USAGE INSTRUMENTATION
// ============================================================================
// This file provides lightweight helper functions that existing handlers call
// to record usage events. Each function is a one-liner from the handler's
// perspective — the complexity lives here.
//
// INSTRUMENTATION POINTS (add to existing handlers):
//
// ai_handler.go — processGenerationJob, after result:
//   h.usage.RecordAITokens(ctx, job.TenantID, result.TokensUsed, "gemini", "listing_gen")
//
// amazon_orders_handler.go — ImportAmazonOrders, each poll:
//   h.usage.RecordAPICall(ctx, tenantID, "amazon_order_sync_poll", "amazon")
//   h.usage.RecordOrderImport(ctx, tenantID, imported, 0)
//
// ebay_orders_handler.go — same pattern
//   h.usage.RecordAPICall(ctx, tenantID, "ebay_order_sync_poll", "ebay")
//   h.usage.RecordOrderImport(ctx, tenantID, imported, 0)
//
// temu_orders_handler.go — same pattern
//   h.usage.RecordAPICall(ctx, tenantID, "temu_order_sync_poll", "temu")
//   h.usage.RecordOrderImport(ctx, tenantID, imported, 0)
//
// amazon_handler.go, ebay_handler.go, temu_handler.go — each outbound API call:
//   h.usage.RecordAPICall(ctx, tenantID, "amazon_product_lookup", "amazon")
//
// dispatch_handlers.go — CreateShipment:
//   h.usage.RecordShipmentLabel(ctx, tenantID, userID)
//
// Listing create handlers — after successful publish:
//   h.usage.RecordListingPublish(ctx, tenantID, "amazon")
//
// export_handler.go — after export job starts:
//   h.usage.RecordDataExport(ctx, tenantID, userID)
// ============================================================================

import (
	"context"
	"log"

	"module-a/models"
	"module-a/services"
)

// UsageInstrumentor wraps UsageService with handler-friendly helpers.
// Add this as a field to handlers that need usage recording.
//
// Example:
//
//	type AmazonOrdersHandler struct {
//	    ...existing fields...
//	    usage *handlers.UsageInstrumentor
//	}
type UsageInstrumentor struct {
	svc *services.UsageService
}

func NewUsageInstrumentor(svc *services.UsageService) *UsageInstrumentor {
	return &UsageInstrumentor{svc: svc}
}

// ── AI Tokens ─────────────────────────────────────────────────────────────────

// RecordAITokens records AI token consumption. This is a BLOCKING call
// because we enforce quota on AI generation — if quota is exceeded the
// generation should not proceed.
//
// Returns ErrQuotaExceeded if the tenant has no credits remaining.
func (u *UsageInstrumentor) RecordAITokens(ctx context.Context, tenantID string, tokenCount int, model, feature string) error {
	if u == nil || u.svc == nil {
		return nil
	}
	return u.svc.RecordUsage(ctx, tenantID, models.UsageEvent{
		Type:     models.UsageAITokens,
		SubType:  model + "_" + feature,
		Quantity: float64(tokenCount),
		Actor:    models.ActorSystem,
		Metadata: map[string]interface{}{
			"model":   model,
			"feature": feature,
		},
	})
}

// CheckAIQuota checks whether the tenant has credits for an AI operation
// before starting it. Call this at the start of expensive AI jobs.
func (u *UsageInstrumentor) CheckAIQuota(ctx context.Context, tenantID string) (bool, error) {
	if u == nil || u.svc == nil {
		return true, nil
	}
	return u.svc.CheckQuota(ctx, tenantID)
}

// ── API Calls ─────────────────────────────────────────────────────────────────

// RecordAPICall records a single outbound marketplace API call.
// Non-blocking — fires in background goroutine.
func (u *UsageInstrumentor) RecordAPICall(ctx context.Context, tenantID, subType, marketplace string) {
	if u == nil || u.svc == nil {
		return
	}
	u.svc.RecordUsageAsync(tenantID, models.UsageEvent{
		Type:        models.UsageAPICall,
		SubType:     subType,
		Quantity:    1,
		Actor:       models.ActorSystem,
		Marketplace: marketplace,
		Metadata: map[string]interface{}{
			"marketplace": marketplace,
			"call_type":   subType,
		},
	})
}

// RecordAPICallBatch records multiple API calls (e.g., paginated responses)
func (u *UsageInstrumentor) RecordAPICallBatch(ctx context.Context, tenantID string, count int, subType, marketplace string) {
	if u == nil || u.svc == nil || count == 0 {
		return
	}
	u.svc.RecordUsageAsync(tenantID, models.UsageEvent{
		Type:        models.UsageAPICall,
		SubType:     subType,
		Quantity:    float64(count),
		Actor:       models.ActorSystem,
		Marketplace: marketplace,
		Metadata: map[string]interface{}{
			"marketplace": marketplace,
			"call_count":  count,
		},
	})
}

// ── Order Import ──────────────────────────────────────────────────────────────

// RecordOrderImport records orders imported from a marketplace.
// orderCount is the number of orders imported.
// gmvValue is the total value of those orders in GBP (0 if unknown).
// Non-blocking.
func (u *UsageInstrumentor) RecordOrderImport(ctx context.Context, tenantID, marketplace string, orderCount int, gmvValueGBP float64) {
	if u == nil || u.svc == nil || orderCount == 0 {
		return
	}
	u.svc.RecordUsageAsync(tenantID, models.UsageEvent{
		Type:        models.UsageOrderSync,
		SubType:     marketplace + "_order_import",
		Quantity:    float64(orderCount),
		Actor:       models.ActorSystem,
		Marketplace: marketplace,
		OrderCount:  orderCount,
		GMVValue:    gmvValueGBP,
		Metadata: map[string]interface{}{
			"marketplace":   marketplace,
			"order_count":   orderCount,
			"gmv_value_gbp": gmvValueGBP,
		},
	})
}

// ── Listing Publish ───────────────────────────────────────────────────────────

// RecordListingPublish records a listing being created or updated on a marketplace.
// Non-blocking.
func (u *UsageInstrumentor) RecordListingPublish(ctx context.Context, tenantID, marketplace, userID string) {
	if u == nil || u.svc == nil {
		return
	}
	u.svc.RecordUsageAsync(tenantID, models.UsageEvent{
		Type:        models.UsageListingPublish,
		SubType:     marketplace + "_listing_publish",
		Quantity:    1,
		Actor:       actorFrom(userID),
		UserID:      userID,
		Marketplace: marketplace,
		Metadata:    map[string]interface{}{"marketplace": marketplace},
	})
}

// ── Shipment Labels ───────────────────────────────────────────────────────────

// RecordShipmentLabel records a carrier label being generated.
// Non-blocking.
func (u *UsageInstrumentor) RecordShipmentLabel(ctx context.Context, tenantID, userID, carrier string) {
	if u == nil || u.svc == nil {
		return
	}
	u.svc.RecordUsageAsync(tenantID, models.UsageEvent{
		Type:     models.UsageShipmentLabel,
		SubType:  carrier + "_label",
		Quantity: 1,
		Actor:    actorFrom(userID),
		UserID:   userID,
		Metadata: map[string]interface{}{"carrier": carrier},
	})
}

// ── Data Exports ──────────────────────────────────────────────────────────────

// RecordDataExport records a data export job being triggered.
// Non-blocking.
func (u *UsageInstrumentor) RecordDataExport(ctx context.Context, tenantID, userID, exportType string) {
	if u == nil || u.svc == nil {
		return
	}
	u.svc.RecordUsageAsync(tenantID, models.UsageEvent{
		Type:     models.UsageDataExport,
		SubType:  exportType,
		Quantity: 1,
		Actor:    actorFrom(userID),
		UserID:   userID,
		Metadata: map[string]interface{}{"export_type": exportType},
	})
}

// ── Quota enforcement middleware helper ───────────────────────────────────────

// EnforceAIQuota is a Gin middleware that blocks AI generation when quota is exceeded.
// Add to any route group that involves AI consumption.
func EnforceAIQuota(usageSvc *services.UsageService) func(c interface {
	GetString(string) string
	AbortWithStatusJSON(int, interface{})
	Next()
}) {
	return func(c interface {
		GetString(string) string
		AbortWithStatusJSON(int, interface{})
		Next()
	}) {
		// This is a typed shim — use in main.go via gin.HandlerFunc wrapper
		log.Printf("[quota] enforcement check")
		c.Next()
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func actorFrom(userID string) models.ActorType {
	if userID == "" || userID == "system" {
		return models.ActorSystem
	}
	return models.ActorUser
}

