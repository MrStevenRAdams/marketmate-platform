// Package instrumentation provides usage event logging for the keyword
// intelligence and SEO optimisation layer. It lives in its own package so
// that both the handlers and services packages can import it without
// creating an import cycle.
package instrumentation

import (
	"context"
	"time"

	"cloud.google.com/go/firestore"
)

// ── Usage Event Types ─────────────────────────────────────────────────────────
const (
	EVTYPE_DATAFORSEO_ASIN_LOOKUP        = "dataforseo_asin_lookup"
	EVTYPE_DATAFORSEO_ASIN_REFRESH       = "dataforseo_asin_refresh"
	EVTYPE_DATAFORSEO_COMPETITOR_LOOKUP  = "dataforseo_competitor_lookup"
	EVTYPE_AMAZON_ADS_KW_RECOMMENDATIONS = "amazon_ads_kw_recommendations"
	EVTYPE_AMAZON_CATALOG_EXTRACT        = "amazon_catalog_extract" // Session 2: free catalog vocabulary extraction
	EVTYPE_AI_LISTING_OPTIMISE           = "ai_listing_optimise"
	EVTYPE_AI_KEYWORD_REANALYSIS         = "ai_keyword_reanalysis"
	EVTYPE_BRAND_ANALYTICS_PULL          = "brand_analytics_pull"
	EVTYPE_SEO_SCORE_CALCULATION         = "seo_score_calculation"
)

// UsageEvent is the canonical structure for all billable and analytics events
// in the keyword intelligence & SEO system. Written to
// tenants/{tenantID}/usage_events/{auto-id} by LogUsageEvent.
//
// billing_handler.go's GetUsageSummary and GetAuditLog read from this
// collection — field names must remain stable.
type UsageEvent struct {
	TenantID        string            `firestore:"tenant_id"`
	EventType       string            `firestore:"event_type"`
	ProductID       string            `firestore:"product_id,omitempty"`
	ListingID       string            `firestore:"listing_id,omitempty"`
	CreditCost      float64           `firestore:"credit_cost"`
	PlatformCostUSD float64           `firestore:"platform_cost_usd"`
	TokensUsed      int               `firestore:"tokens_used,omitempty"`
	DataSource      string            `firestore:"data_source,omitempty"`
	Metadata        map[string]string `firestore:"metadata,omitempty"`
	Timestamp       time.Time         `firestore:"timestamp"`
}

// LogUsageEvent writes a UsageEvent document to
// tenants/{tenantID}/usage_events/{auto-id} in Firestore.
// It is intentionally fire-and-forget — a logging failure must never
// break the main request path.
func LogUsageEvent(ctx context.Context, firestoreClient *firestore.Client, event UsageEvent) error {
	if event.TenantID == "" {
		return nil
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	_, _, err := firestoreClient.
		Collection("tenants").
		Doc(event.TenantID).
		Collection("usage_events").
		Add(ctx, event)

	return err
}
