package handlers

// ============================================================================
// AUTO REORDER HANDLER — SESSION 9 (3.6)
// ============================================================================
// Endpoints:
//   POST /forecasting/auto-reorder/run        — trigger a check now
//   GET  /forecasting/auto-reorder/log        — fetch recent run history
//   GET  /forecasting/auto-reorder/settings   — get auto_reorder settings
//   PUT  /forecasting/auto-reorder/settings   — update auto_reorder settings
//
// On each run the handler:
//   1. Calls the same channel-demand logic as GetChannelDemand
//   2. Finds products below threshold that are NOT snoozed
//   3. If auto_reorder is enabled AND a default_supplier_id is configured,
//      creates a draft PO for each qualifying product
//   4. Writes a run summary to tenants/{id}/auto_reorder_log/{run_id}
// ============================================================================

import (
	"fmt"
	"math"
	"net/http"
	"sort"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ─── Types ────────────────────────────────────────────────────────────────────

type AutoReorderSettings struct {
	Enabled           bool   `firestore:"enabled"            json:"enabled"`
	DefaultSupplierID string `firestore:"default_supplier_id" json:"default_supplier_id"`
	DefaultSupplierName string `firestore:"default_supplier_name" json:"default_supplier_name,omitempty"`
	MinStockPctAlert  int    `firestore:"min_stock_pct_alert" json:"min_stock_pct_alert"` // not used in run logic, stored for UI
	UpdatedAt         string `firestore:"updated_at"         json:"updated_at"`
}

type AutoReorderLogEntry struct {
	RunID          string                 `firestore:"run_id"          json:"run_id"`
	TenantID       string                 `firestore:"tenant_id"       json:"tenant_id"`
	TriggeredBy    string                 `firestore:"triggered_by"    json:"triggered_by"` // "manual" | "scheduled"
	StartedAt      time.Time              `firestore:"started_at"      json:"started_at"`
	CompletedAt    time.Time              `firestore:"completed_at"    json:"completed_at"`
	ProductsChecked int                   `firestore:"products_checked" json:"products_checked"`
	AlertsFound    int                    `firestore:"alerts_found"    json:"alerts_found"`
	POsCreated     int                    `firestore:"pos_created"     json:"pos_created"`
	POIDs          []string               `firestore:"po_ids"          json:"po_ids"`
	Skipped        int                    `firestore:"skipped"         json:"skipped"` // snoozed or no supplier
	AutoReorderOn  bool                   `firestore:"auto_reorder_on" json:"auto_reorder_on"`
	AlertItems     []AutoReorderAlertItem `firestore:"alert_items"     json:"alert_items"`
	Error          string                 `firestore:"error,omitempty" json:"error,omitempty"`
}

type AutoReorderAlertItem struct {
	ProductID   string `firestore:"product_id"   json:"product_id"`
	SKU         string `firestore:"sku"          json:"sku"`
	ProductName string `firestore:"product_name" json:"product_name"`
	CurrentStock int   `firestore:"current_stock" json:"current_stock"`
	ReorderPoint int   `firestore:"reorder_point" json:"reorder_point"`
	ReorderQty  int   `firestore:"reorder_qty"  json:"reorder_qty"`
	TopChannel  string `firestore:"top_channel"  json:"top_channel"`
	Action      string `firestore:"action"       json:"action"` // "po_created" | "skipped_snoozed" | "skipped_no_supplier" | "skipped_auto_reorder_off"
	POID        string `firestore:"po_id,omitempty" json:"po_id,omitempty"`
}

// AutoReorderHandler extends ForecastingHandler — reuses the same client.
// We define it as a separate struct wrapping ForecastingHandler so it can
// call its internal helpers.

type AutoReorderHandler struct {
	fh *ForecastingHandler
}

func NewAutoReorderHandler(fh *ForecastingHandler) *AutoReorderHandler {
	return &AutoReorderHandler{fh: fh}
}

func (h *AutoReorderHandler) settingsRef(tenantID string) *firestore.DocumentRef {
	return h.fh.client.Collection(fmt.Sprintf("tenants/%s/settings", tenantID)).Doc("auto_reorder")
}

func (h *AutoReorderHandler) logCol(tenantID string) *firestore.CollectionRef {
	return h.fh.client.Collection(fmt.Sprintf("tenants/%s/auto_reorder_log", tenantID))
}

func (h *AutoReorderHandler) loadAutoReorderSettings(tenantID string, ctx interface{ Deadline() (time.Time, bool) }) AutoReorderSettings {
	// Use a standard context.Context — gin's request context satisfies this
	s := AutoReorderSettings{
		Enabled:          false,
		MinStockPctAlert: 20,
	}
	// We get the context from the gin.Context via c.Request.Context() at call site,
	// so just use the fh.client directly here with a background context fallback.
	return s
}

// ─── GET /forecasting/auto-reorder/settings ──────────────────────────────────

func (h *AutoReorderHandler) GetSettings(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()

	s := AutoReorderSettings{Enabled: false, MinStockPctAlert: 20}
	doc, err := h.settingsRef(tenantID).Get(ctx)
	if err == nil {
		doc.DataTo(&s)
	}

	c.JSON(http.StatusOK, gin.H{"settings": s})
}

// ─── PUT /forecasting/auto-reorder/settings ──────────────────────────────────

func (h *AutoReorderHandler) UpdateSettings(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()

	var req AutoReorderSettings
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	if _, err := h.settingsRef(tenantID).Set(ctx, req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save settings"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"settings": req})
}

// ─── GET /forecasting/auto-reorder/log ───────────────────────────────────────

func (h *AutoReorderHandler) GetLog(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()

	iter := h.logCol(tenantID).OrderBy("started_at", firestore.Desc).Limit(50).Documents(ctx)
	defer iter.Stop()

	var entries []AutoReorderLogEntry
	for {
		doc, err := iter.Next()
		if err != nil {
			break
		}
		var entry AutoReorderLogEntry
		doc.DataTo(&entry)
		entries = append(entries, entry)
	}
	if entries == nil {
		entries = []AutoReorderLogEntry{}
	}

	c.JSON(http.StatusOK, gin.H{"log": entries, "count": len(entries)})
}

// ─── POST /forecasting/auto-reorder/run ──────────────────────────────────────
// Core logic: runs the channel-demand check and optionally creates POs.

func (h *AutoReorderHandler) RunCheck(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()

	triggeredBy := "manual"
	var body struct {
		TriggeredBy string `json:"triggered_by"`
	}
	if err := c.ShouldBindJSON(&body); err == nil && body.TriggeredBy != "" {
		triggeredBy = body.TriggeredBy
	}

	runID := "ar_" + uuid.New().String()
	startedAt := time.Now().UTC()

	// ── Load auto-reorder settings ────────────────────────────────────────────
	settings := AutoReorderSettings{Enabled: false}
	if doc, err := h.settingsRef(tenantID).Get(ctx); err == nil {
		doc.DataTo(&settings)
	}

	// ── Load forecasting settings for defaults ────────────────────────────────
	fcastSettings := h.fh.loadSettings(ctx, tenantID)

	lookbackDays := 30
	now := time.Now().UTC()
	since := now.AddDate(0, 0, -lookbackDays)

	// ── Load all product forecasts ────────────────────────────────────────────
	type forecastInfo struct {
		currentStock int
		reorderPoint int
		leadTime     int
		reorderQty   int
		sku          string
		name         string
		snoozeUntil  time.Time
	}
	forecastMap := map[string]*forecastInfo{} // productID -> info

	fcIter := h.fh.forecastCol(tenantID).Documents(ctx)
	defer fcIter.Stop()
	for {
		doc, err := fcIter.Next()
		if err != nil {
			break
		}
		var fc ProductForecastConfig
		doc.DataTo(&fc)

		leadTime := fcastSettings.DefaultLeadTimeDays
		if fc.LeadTimeDays != nil {
			leadTime = *fc.LeadTimeDays
		}
		snoozeUntil := time.Time{}
		if raw := doc.Data(); raw != nil {
			if su, ok := raw["snooze_until"].(time.Time); ok {
				snoozeUntil = su
			}
		}
		forecastMap[fc.ProductID] = &forecastInfo{
			currentStock: fc.CurrentStock,
			reorderPoint: fc.ReorderPoint,
			leadTime:     leadTime,
			reorderQty:   fc.ReorderQty,
			sku:          fc.SKU,
			name:         fc.ProductName,
			snoozeUntil:  snoozeUntil,
		}
	}

	// ── Scan orders for the last 30 days ─────────────────────────────────────
	type skuChannelKey struct {
		sku     string
		channel string
	}
	type skuChannelAgg struct {
		units      int
		orderCount int
	}
	aggMap := map[skuChannelKey]*skuChannelAgg{}

	ordersIter := h.fh.client.Collection(fmt.Sprintf("tenants/%s/orders", tenantID)).Documents(ctx)
	defer ordersIter.Stop()
	for {
		doc, err := ordersIter.Next()
		if err != nil {
			break
		}
		data := doc.Data()
		tsStr, _ := data["created_at"].(string)
		ts, err2 := time.Parse(time.RFC3339, tsStr)
		if err2 != nil {
			ts, _ = time.Parse("2006-01-02", tsStr)
		}
		if ts.Before(since) {
			continue
		}
		if status, _ := data["status"].(string); status == "cancelled" {
			continue
		}
		channel, _ := data["channel"].(string)
		if channel == "" {
			channel = "unknown"
		}
		linesRaw, _ := data["lines"].([]interface{})
		for _, lr := range linesRaw {
			lm, ok := lr.(map[string]interface{})
			if !ok {
				continue
			}
			sku, _ := lm["sku"].(string)
			if sku == "" {
				continue
			}
			qty := 0
			if q, ok := lm["quantity"].(int64); ok {
				qty = int(q)
			} else if q, ok := lm["quantity"].(float64); ok {
				qty = int(q)
			}
			key := skuChannelKey{sku: sku, channel: channel}
			if _, ok := aggMap[key]; !ok {
				aggMap[key] = &skuChannelAgg{}
			}
			aggMap[key].units += qty
			aggMap[key].orderCount++
		}
	}

	// ── Group by SKU, compute velocities ─────────────────────────────────────
	skuChannelMap := map[string]map[string]*skuChannelAgg{}
	for k, v := range aggMap {
		if _, ok := skuChannelMap[k.sku]; !ok {
			skuChannelMap[k.sku] = map[string]*skuChannelAgg{}
		}
		skuChannelMap[k.sku][k.channel] = v
	}

	skuToProductID := map[string]string{}
	for pid, fi := range forecastMap {
		skuToProductID[fi.sku] = pid
	}

	// ── Identify alerts ───────────────────────────────────────────────────────
	type alert struct {
		productID    string
		sku          string
		name         string
		currentStock int
		reorderPoint int
		reorderQty   int
		topChannel   string
		snoozed      bool
	}
	var alerts []alert

	for sku, channelData := range skuChannelMap {
		pid := skuToProductID[sku]
		fi := forecastMap[pid]
		if fi == nil {
			fi = &forecastInfo{sku: sku, leadTime: fcastSettings.DefaultLeadTimeDays}
		}

		maxVelocity := 0.0
		topChannel := ""
		for ch, agg := range channelData {
			v := float64(agg.units) / float64(lookbackDays)
			if v > maxVelocity {
				maxVelocity = v
				topChannel = ch
			}
		}

		reorderThreshold := fi.reorderPoint
		if reorderThreshold == 0 && maxVelocity > 0 {
			reorderThreshold = int(math.Ceil(maxVelocity * float64(fi.leadTime) * 1.2))
		}
		if reorderThreshold == 0 {
			continue
		}

		belowThreshold := fi.currentStock < reorderThreshold
		if !belowThreshold {
			continue
		}

		snoozed := !fi.snoozeUntil.IsZero() && fi.snoozeUntil.After(now)

		reorderQty := fi.reorderQty
		if reorderQty == 0 && maxVelocity > 0 {
			reorderQty = int(math.Ceil(maxVelocity * 30))
		}

		alerts = append(alerts, alert{
			productID:    pid,
			sku:          sku,
			name:         fi.name,
			currentStock: fi.currentStock,
			reorderPoint: reorderThreshold,
			reorderQty:   reorderQty,
			topChannel:   topChannel,
			snoozed:      snoozed,
		})
	}

	sort.Slice(alerts, func(i, j int) bool {
		return alerts[i].currentStock < alerts[j].currentStock
	})

	// ── Create POs if auto-reorder is enabled ─────────────────────────────────
	var poIDs []string
	var logItems []AutoReorderAlertItem
	posCreated := 0
	skipped := 0

	// Group non-snoozed alerts into a single draft PO (one PO per run)
	var poLines []map[string]interface{}
	var poAlerts []alert

	for _, a := range alerts {
		item := AutoReorderAlertItem{
			ProductID:    a.productID,
			SKU:          a.sku,
			ProductName:  a.name,
			CurrentStock: a.currentStock,
			ReorderPoint: a.reorderPoint,
			ReorderQty:   a.reorderQty,
			TopChannel:   a.topChannel,
		}

		if a.snoozed {
			item.Action = "skipped_snoozed"
			skipped++
		} else if !settings.Enabled {
			item.Action = "skipped_auto_reorder_off"
			skipped++
		} else if settings.DefaultSupplierID == "" {
			item.Action = "skipped_no_supplier"
			skipped++
		} else {
			// Queue for PO creation
			item.Action = "po_created"
			poLines = append(poLines, map[string]interface{}{
				"line_id":      "pol_" + uuid.New().String(),
				"product_id":   a.productID,
				"internal_sku": a.sku,
				"description":  a.name,
				"qty_ordered":  a.reorderQty,
				"qty_received": 0,
				"unit_cost":    0,
			})
			poAlerts = append(poAlerts, a)
		}

		logItems = append(logItems, item)
	}

	// Create the single consolidated PO if we have lines
	if len(poLines) > 0 && settings.Enabled && settings.DefaultSupplierID != "" {
		poID := "po_" + uuid.New().String()
		poNumber := "PO-AUTO-" + now.Format("060102") + "-" + poID[:6]
		po := map[string]interface{}{
			"po_id":          poID,
			"po_number":      poNumber,
			"tenant_id":      tenantID,
			"supplier_id":    settings.DefaultSupplierID,
			"type":           "standard",
			"order_method":   "auto_reorder",
			"lines":          poLines,
			"status":         "draft",
			"total_cost":     0,
			"internal_notes": fmt.Sprintf("Auto-generated by reorder check run %s on %s", runID, now.Format("2006-01-02 15:04")),
			"created_at":     now,
			"updated_at":     now,
			"source":         "auto_reorder",
			"run_id":         runID,
		}

		if _, err := h.fh.client.Collection("tenants").Doc(tenantID).
			Collection("purchase_orders").Doc(poID).Set(ctx, po); err == nil {
			poIDs = append(poIDs, poID)
			posCreated = len(poLines)

			// Back-fill po_id into log items
			poAlertIDs := map[string]bool{}
			for _, a := range poAlerts {
				poAlertIDs[a.productID] = true
			}
			for i := range logItems {
				if poAlertIDs[logItems[i].ProductID] {
					logItems[i].POID = poID
				}
			}
		}
	}

	// ── Write run log ─────────────────────────────────────────────────────────
	completedAt := time.Now().UTC()
	if logItems == nil {
		logItems = []AutoReorderAlertItem{}
	}
	if poIDs == nil {
		poIDs = []string{}
	}

	logEntry := AutoReorderLogEntry{
		RunID:           runID,
		TenantID:        tenantID,
		TriggeredBy:     triggeredBy,
		StartedAt:       startedAt,
		CompletedAt:     completedAt,
		ProductsChecked: len(forecastMap),
		AlertsFound:     len(alerts),
		POsCreated:      posCreated,
		POIDs:           poIDs,
		Skipped:         skipped,
		AutoReorderOn:   settings.Enabled,
		AlertItems:      logItems,
	}

	h.logCol(tenantID).Doc(runID).Set(ctx, logEntry)

	c.JSON(http.StatusOK, gin.H{
		"run_id":           runID,
		"triggered_by":     triggeredBy,
		"products_checked": len(forecastMap),
		"alerts_found":     len(alerts),
		"pos_created":      posCreated,
		"skipped":          skipped,
		"po_ids":           poIDs,
		"auto_reorder_on":  settings.Enabled,
		"alert_items":      logItems,
		"duration_ms":      completedAt.Sub(startedAt).Milliseconds(),
	})
}
