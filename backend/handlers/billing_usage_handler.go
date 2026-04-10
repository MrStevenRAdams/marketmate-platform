package handlers

// ============================================================================
// BILLING USAGE HANDLER — Session 11
// ============================================================================
// Separate from billing_handler.go (which reads credit_ledger + audit_log).
// These three endpoints read tenants/{id}/usage_events — the collection
// written by instrumentation.LogUsageEvent throughout Sessions 1–10.
//
// Routes registered in main.go under the authenticated api group:
//   GET /billing/usage-summary
//   GET /billing/audit-log
//   GET /billing/credit-usage-breakdown
//
// All three share the same BillingHandler receiver so no new handler struct
// is needed and main.go wiring stays unchanged.

import (
	"net/http"
	"strconv"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"

	"module-a/instrumentation"
)

// ── Period helpers ────────────────────────────────────────────────────────────

// periodBounds returns (start, end) for the requested period string.
//
//	"current_month" / ""  → first day of current UTC month … now
//	"last_month"          → first day of last UTC month … last day of last month
//	"last_30"             → now-30d … now
//	"last_90"             → now-90d … now
func periodBounds(period string) (start, end time.Time) {
	now := time.Now().UTC()
	switch period {
	case "last_month":
		firstOfThisMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		end = firstOfThisMonth.Add(-time.Second)
		start = time.Date(end.Year(), end.Month(), 1, 0, 0, 0, 0, time.UTC)
	case "last_30":
		end = now
		start = now.AddDate(0, 0, -30)
	case "last_90":
		end = now
		start = now.AddDate(0, 0, -90)
	default: // "current_month" or anything else
		start = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		end = now
	}
	return
}

// ── Response types ────────────────────────────────────────────────────────────

type creditBreakdownEntry struct {
	Count   int     `json:"count"`
	Credits float64 `json:"credits"`
}

type usageSummaryResponse struct {
	PeriodStart     time.Time                       `json:"period_start"`
	PeriodEnd       time.Time                       `json:"period_end"`
	CreditsUsed     float64                         `json:"credits_used"`
	CreditsRemaining *float64                       `json:"credits_remaining,omitempty"`
	CreditBreakdown map[string]creditBreakdownEntry `json:"credit_breakdown"`
}

type auditLogEvent struct {
	ID          string            `json:"id"`
	Timestamp   time.Time         `json:"timestamp"`
	EventType   string            `json:"event_type"`
	CreditCost  float64           `json:"credit_cost"`
	ProductID   string            `json:"product_id,omitempty"`
	ListingID   string            `json:"listing_id,omitempty"`
	DataSource  string            `json:"data_source,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type auditLogResponse struct {
	Events     []auditLogEvent `json:"events"`
	NextCursor string          `json:"next_cursor,omitempty"`
}

// ── GetUsageSummaryV2 — GET /api/v1/billing/usage-summary ─────────────────────
//
// Query params:
//   ?period=current_month (default) | last_month | last_30 | last_90
//
// Reads tenants/{tenantID}/usage_events, groups by event_type, returns totals
// and a per-operation credit breakdown. platform_absorbed = events with credit_cost 0.
func (h *BillingHandler) GetUsageSummaryV2(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	periodParam := c.DefaultQuery("period", "current_month")
	start, end := periodBounds(periodParam)

	ctx := c.Request.Context()

	iter := h.client.
		Collection("tenants").Doc(tenantID).
		Collection("usage_events").
		Where("timestamp", ">=", start).
		Where("timestamp", "<=", end).
		Documents(ctx)
	defer iter.Stop()

	breakdown := map[string]creditBreakdownEntry{}
	var totalCredits float64
	platformCount := 0

	for {
		snap, err := iter.Next()
		if err != nil {
			break
		}
		var ev instrumentation.UsageEvent
		if err := snap.DataTo(&ev); err != nil {
			continue
		}

		if ev.CreditCost == 0 {
			platformCount++
			// Count in platform_absorbed bucket but no credits
			pb := breakdown["platform_absorbed"]
			pb.Count++
			breakdown["platform_absorbed"] = pb
			continue
		}

		totalCredits += ev.CreditCost
		entry := breakdown[ev.EventType]
		entry.Count++
		entry.Credits += ev.CreditCost
		breakdown[ev.EventType] = entry
	}

	// Ensure platform_absorbed credits always read as 0 (it is, but be explicit)
	if pb, ok := breakdown["platform_absorbed"]; ok {
		pb.Credits = 0
		breakdown["platform_absorbed"] = pb
	}

	// Try to fetch remaining credits from the ledger for this period's plan info
	var remaining *float64
	if h.usageService != nil {
		period := start.Format("2006-01")
		if ledger, err := h.usageService.GetLedger(ctx, tenantID, period); err == nil && ledger != nil {
			remaining = ledger.CreditsRemaining
		}
	}

	c.JSON(http.StatusOK, usageSummaryResponse{
		PeriodStart:     start,
		PeriodEnd:       end,
		CreditsUsed:     totalCredits,
		CreditsRemaining: remaining,
		CreditBreakdown: breakdown,
	})
}

// ── GetAuditLogV2 — GET /api/v1/billing/audit-log ────────────────────────────
//
// Query params:
//   ?limit=50 (default, max 200)
//   ?before=<RFC3339 timestamp> — cursor for pagination
//   ?event_type=<type> — filter to one event type
//
// Returns events from usage_events ordered by timestamp DESC.
// Pagination: include ?before=<next_cursor value> to get the next page.
func (h *BillingHandler) GetAuditLogV2(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	limit := 50
	if l := c.Query("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}

	ctx := c.Request.Context()

	q := h.client.
		Collection("tenants").Doc(tenantID).
		Collection("usage_events").
		OrderBy("timestamp", firestore.Desc).
		Limit(limit)

	if et := c.Query("event_type"); et != "" {
		q = q.Where("event_type", "==", et)
	}

	if before := c.Query("before"); before != "" {
		if t, err := time.Parse(time.RFC3339, before); err == nil {
			q = q.Where("timestamp", "<", t)
		}
	}

	iter := q.Documents(ctx)
	defer iter.Stop()

	events := []auditLogEvent{}
	var lastTimestamp time.Time

	for {
		snap, err := iter.Next()
		if err != nil {
			break
		}
		var ev instrumentation.UsageEvent
		if err := snap.DataTo(&ev); err != nil {
			continue
		}
		events = append(events, auditLogEvent{
			ID:         snap.Ref.ID,
			Timestamp:  ev.Timestamp,
			EventType:  ev.EventType,
			CreditCost: ev.CreditCost,
			ProductID:  ev.ProductID,
			ListingID:  ev.ListingID,
			DataSource: ev.DataSource,
			Metadata:   ev.Metadata,
		})
		lastTimestamp = ev.Timestamp
	}

	resp := auditLogResponse{Events: events}
	if len(events) == limit {
		// There may be more — provide cursor
		resp.NextCursor = lastTimestamp.UTC().Format(time.RFC3339)
	}

	c.JSON(http.StatusOK, resp)
}

// ── GetCreditUsageBreakdown — GET /api/v1/billing/credit-usage-breakdown ──────
//
// Thin wrapper around GetUsageSummaryV2 that returns only credit_breakdown.
// Useful for chart components that don't need the full summary.
// Accepts same ?period= params.
func (h *BillingHandler) GetCreditUsageBreakdown(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	periodParam := c.DefaultQuery("period", "current_month")
	start, end := periodBounds(periodParam)

	ctx := c.Request.Context()

	iter := h.client.
		Collection("tenants").Doc(tenantID).
		Collection("usage_events").
		Where("timestamp", ">=", start).
		Where("timestamp", "<=", end).
		Documents(ctx)
	defer iter.Stop()

	breakdown := map[string]creditBreakdownEntry{}

	for {
		snap, err := iter.Next()
		if err != nil {
			break
		}
		var ev instrumentation.UsageEvent
		if err := snap.DataTo(&ev); err != nil {
			continue
		}

		if ev.CreditCost == 0 {
			pb := breakdown["platform_absorbed"]
			pb.Count++
			// Credits stay 0
			breakdown["platform_absorbed"] = pb
			continue
		}

		entry := breakdown[ev.EventType]
		entry.Count++
		entry.Credits += ev.CreditCost
		breakdown[ev.EventType] = entry
	}

	if pb, ok := breakdown["platform_absorbed"]; ok {
		pb.Credits = 0
		breakdown["platform_absorbed"] = pb
	}

	c.JSON(http.StatusOK, gin.H{"credit_breakdown": breakdown, "period_start": start, "period_end": end})
}
