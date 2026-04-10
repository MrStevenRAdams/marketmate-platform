// Package handlers — admin_handler.go
// Internal admin endpoints for platform cost visibility.
//
// GET /api/v1/admin/platform-cost-summary
//
// Auth: registered on the unauthenticated router group (same pattern as other
// /api/v1/admin/* routes in main.go) — caller must supply a valid Firebase
// token; the handler additionally verifies the caller's role via the middleware
// context key "role" (set by AuthMiddleware as CtxRole). Accepts "owner" or
// "admin" roles. The previous "is_admin" / "admin" claim keys were never set
// by AuthMiddleware and are replaced by this role-based check.
// Query params:
//
//	?days=30       — rolling window (default 30, max 90)
//	?tenant_id=xxx — optional single-tenant filter
package handlers

import (
	"log"
	"net/http"
	"sort"
	"strconv"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"google.golang.org/api/iterator"
)

// AdminHandler serves internal admin endpoints.
type AdminHandler struct {
	client *firestore.Client
}

// NewAdminHandler constructs the handler.
func NewAdminHandler(client *firestore.Client) *AdminHandler {
	return &AdminHandler{client: client}
}

// ── types ─────────────────────────────────────────────────────────────────────

type platformCostBySource struct {
	Calls   int     `json:"calls,omitempty"`
	Tokens  int     `json:"tokens,omitempty"`
	CostUSD float64 `json:"cost_usd"`
}

type tenantCostRow struct {
	TenantID          string  `json:"tenant_id"`
	PlatformCostUSD   float64 `json:"platform_cost_usd"`
	CreditsConsumed   float64 `json:"credits_consumed"`
	ListingsOptimised int     `json:"listings_optimised"`
}

// ── handler ───────────────────────────────────────────────────────────────────

// PlatformCostSummary handles GET /api/v1/admin/platform-cost-summary.
// It uses a Firestore collection-group query across all tenants' usage_events
// subcollections and aggregates cost by data source and by tenant.
func (h *AdminHandler) PlatformCostSummary(c *gin.Context) {
	ctx := c.Request.Context()

	// ── Admin role check ─────────────────────────────────────────────────────
	// AuthMiddleware sets the "role" context key (CtxRole constant = "role").
	// Only "owner" and "admin" roles may access platform-wide cost data.
	role := c.GetString("role")
	if role != "owner" && role != "admin" {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin access required"})
		return
	}

	// ── Query params ─────────────────────────────────────────────────────────
	days := 30
	if d := c.Query("days"); d != "" {
		if v, err := strconv.Atoi(d); err == nil && v > 0 {
			if v > 90 {
				v = 90
			}
			days = v
		}
	}
	filterTenant := c.Query("tenant_id")

	since := time.Now().UTC().AddDate(0, 0, -days)

	// ── Collection-group query ────────────────────────────────────────────────
	// Mirrors the pattern in tracking_sync_handler.go and ai_consolidation_handler.go.
	q := h.client.CollectionGroup("usage_events").
		Where("timestamp", ">=", since)
	if filterTenant != "" {
		q = q.Where("tenant_id", "==", filterTenant)
	}

	iter := q.Documents(ctx)
	defer iter.Stop()

	// Accumulators
	bySource := map[string]*platformCostBySource{}
	byTenant := map[string]*tenantCostRow{}
	totalCostUSD := 0.0

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Printf("[admin] platform-cost-summary query error: %v", err)
			break
		}

		data := doc.Data()

		tenantID, _ := data["tenant_id"].(string)
		if tenantID == "" {
			continue
		}
		dataSource, _ := data["data_source"].(string)
		if dataSource == "" {
			dataSource = "unknown"
		}
		platformCost := toFloat64(data["platform_cost_usd"])
		creditCost := toFloat64(data["credit_cost"])
		tokensUsed := toInt(data["tokens_used"])
		eventType, _ := data["event_type"].(string)
		listingID, _ := data["listing_id"].(string)

		totalCostUSD += platformCost

		// Aggregate by source
		if _, ok := bySource[dataSource]; !ok {
			bySource[dataSource] = &platformCostBySource{}
		}
		src := bySource[dataSource]
		src.CostUSD += platformCost
		src.Calls++
		src.Tokens += tokensUsed

		// Aggregate by tenant
		if _, ok := byTenant[tenantID]; !ok {
			byTenant[tenantID] = &tenantCostRow{TenantID: tenantID}
		}
		row := byTenant[tenantID]
		row.PlatformCostUSD += platformCost
		row.CreditsConsumed += creditCost
		if eventType == "ai_listing_optimise" && listingID != "" {
			row.ListingsOptimised++
		}
	}

	// ── Sort tenants by platform cost DESC ───────────────────────────────────
	tenantRows := make([]tenantCostRow, 0, len(byTenant))
	for _, row := range byTenant {
		tenantRows = append(tenantRows, *row)
	}
	sort.Slice(tenantRows, func(i, j int) bool {
		return tenantRows[i].PlatformCostUSD > tenantRows[j].PlatformCostUSD
	})

	// ── Build by_source response ──────────────────────────────────────────────
	sourceMap := map[string]platformCostBySource{}
	for k, v := range bySource {
		sourceMap[k] = *v
	}

	c.JSON(http.StatusOK, gin.H{
		"period_days":             days,
		"total_platform_cost_usd": totalCostUSD,
		"by_source":               sourceMap,
		"by_tenant":               tenantRows,
	})
}

// ── helpers ───────────────────────────────────────────────────────────────────

func toFloat64(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case int64:
		return float64(val)
	}
	return 0
}

func toInt(v interface{}) int {
	switch val := v.(type) {
	case int64:
		return int(val)
	case float64:
		return int(val)
	}
	return 0
}
