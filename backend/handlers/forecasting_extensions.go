package handlers

// ============================================================================
// FORECASTING EXTENSIONS
// ============================================================================
// These endpoints extend the existing ForecastingHandler to add:
//   5.11  Auto-create PO from forecasting screen
//   5.11  Days on Hand forecast method (handled via UpdateProductForecast)
//   S8    Channel-level demand signals + reorder alerts
//
// These methods are added to ForecastingHandler in this extension file.
// The ForecastingHandler struct is defined in forecasting_handler.go.
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

// ── POST /api/v1/forecasting/create-po ───────────────────────────────────────

type ForecastCreatePOLine struct {
	ProductID   string `json:"product_id" binding:"required"`
	SKU         string `json:"sku"`
	Description string `json:"description"`
	Quantity    int    `json:"qty" binding:"required,min=1"`
}

type ForecastCreatePORequest struct {
	SupplierID string                 `json:"supplier_id" binding:"required"`
	Lines      []ForecastCreatePOLine `json:"lines" binding:"required"`
	Notes      string                 `json:"notes"`
}

func (h *ForecastingHandler) CreatePOFromForecast(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()

	var req ForecastCreatePORequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	poID := "po_" + uuid.New().String()
	poNumber := "PO-FCST-" + time.Now().Format("060102") + "-" + poID[:6]
	now := time.Now()

	poLines := make([]map[string]interface{}, 0, len(req.Lines))
	var totalCost float64
	for _, l := range req.Lines {
		lineID := "pol_" + uuid.New().String()
		poLines = append(poLines, map[string]interface{}{
			"line_id":       lineID,
			"product_id":    l.ProductID,
			"internal_sku":  l.SKU,
			"description":   l.Description,
			"qty_ordered":   l.Quantity,
			"qty_received":  0,
			"unit_cost":     0,
		})
	}

	po := map[string]interface{}{
		"po_id":           poID,
		"po_number":       poNumber,
		"tenant_id":       tenantID,
		"supplier_id":     req.SupplierID,
		"type":            "standard",
		"order_method":    "manual",
		"lines":           poLines,
		"status":          "draft",
		"total_cost":      totalCost,
		"notes":           req.Notes,
		"internal_notes":  "Auto-generated from forecasting screen",
		"created_at":      now,
		"updated_at":      now,
		"source":          "forecasting",
	}

	if _, err := h.client.Collection("tenants").Doc(tenantID).Collection("purchase_orders").Doc(poID).Set(ctx, po); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create purchase order"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"po_id":     poID,
		"po_number": poNumber,
		"lines":     len(poLines),
	})
}

// ── GET /api/v1/forecasting/products/:product_id/chart ───────────────────────
// Returns 60-day projection data for the product forecast chart

func (h *ForecastingHandler) GetForecastChart(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	productID := c.Param("product_id")
	ctx := c.Request.Context()

	doc, err := h.forecastCol(tenantID).Doc(productID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "forecast not found for product"})
		return
	}
	var fc ProductForecastConfig
	doc.DataTo(&fc)

	type DataPoint struct {
		Day          int     `json:"day"`
		Date         string  `json:"date"`
		ProjectedQty float64 `json:"projected_qty"`
		ReorderPoint int     `json:"reorder_point"`
	}

	points := make([]DataPoint, 0, 61)
	stock := float64(fc.CurrentStock)
	today := time.Now()

	for d := 0; d <= 60; d++ {
		date := today.AddDate(0, 0, d)
		if d > 0 {
			stock -= fc.CalculatedADC
			if stock < 0 {
				stock = 0
			}
		}
		points = append(points, DataPoint{
			Day:          d,
			Date:         date.Format("2006-01-02"),
			ProjectedQty: stock,
			ReorderPoint: fc.ReorderPoint,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"product_id":    productID,
		"current_stock": fc.CurrentStock,
		"adc":           fc.CalculatedADC,
		"reorder_point": fc.ReorderPoint,
		"data":          points,
	})
}

// ============================================================================
// SESSION 8: CHANNEL-LEVEL DEMAND SIGNALS + REORDER ALERTS
// GET /api/v1/forecasting/channel-demand
// ============================================================================

type ChannelVelocity struct {
	Channel       string  `json:"channel"`
	AvgDailySales float64 `json:"avg_daily_sales"`
	Forecast30d   float64 `json:"forecast_30d"`
	OrderCount    int     `json:"order_count"`
}

type ProductChannelDemand struct {
	ProductID        string            `json:"product_id"`
	SKU              string            `json:"sku"`
	ProductName      string            `json:"product_name"`
	CurrentStock     int               `json:"current_stock"`
	ReorderPoint     int               `json:"reorder_point"`
	LeadTimeDays     int               `json:"lead_time_days"`
	BelowThreshold   bool              `json:"below_threshold"`
	ReorderQty       int               `json:"reorder_qty"`
	TopChannel       string            `json:"top_channel"`
	SnoozedUntil     string            `json:"snoozed_until,omitempty"`
	ChannelBreakdown []ChannelVelocity `json:"channel_breakdown"`
}

type ChannelDemandResponse struct {
	Period   string                 `json:"period"`
	Products []ProductChannelDemand `json:"products"`
	Alerts   []ProductChannelDemand `json:"alerts"`
}

func (h *ForecastingHandler) GetChannelDemand(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()

	lookbackDays := 30
	now := time.Now().UTC()
	since := now.AddDate(0, 0, -lookbackDays)

	// Load all forecasts for threshold / stock data
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

	fcIter := h.forecastCol(tenantID).Documents(ctx)
	defer fcIter.Stop()
	for {
		doc, err := fcIter.Next()
		if err != nil {
			break
		}
		var fc ProductForecastConfig
		doc.DataTo(&fc)
		leadTime := 14
		if fc.LeadTimeDays != nil {
			leadTime = *fc.LeadTimeDays
		}
		// Read snooze_until from raw data
		snoozeUntil := time.Time{}
		raw := doc.Data()
		if su, ok := raw["snooze_until"].(time.Time); ok {
			snoozeUntil = su
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

	// Scan orders for the last 30 days, group by SKU x channel
	type skuChannelKey struct {
		sku     string
		channel string
	}
	type skuChannelAgg struct {
		units      int
		orderCount int
	}
	aggMap := map[skuChannelKey]*skuChannelAgg{}

	ordersIter := h.client.Collection(fmt.Sprintf("tenants/%s/orders", tenantID)).Documents(ctx)
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
		status, _ := data["status"].(string)
		if status == "cancelled" {
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

	// Group agg by SKU
	skuChannelMap := map[string]map[string]*skuChannelAgg{}
	for k, v := range aggMap {
		if _, ok := skuChannelMap[k.sku]; !ok {
			skuChannelMap[k.sku] = map[string]*skuChannelAgg{}
		}
		skuChannelMap[k.sku][k.channel] = v
	}

	// Reverse lookup: sku -> productID
	skuToProductID := map[string]string{}
	for pid, fi := range forecastMap {
		skuToProductID[fi.sku] = pid
	}

	var allProducts []ProductChannelDemand
	var alerts []ProductChannelDemand

	for sku, channelData := range skuChannelMap {
		pid := skuToProductID[sku]
		fi := forecastMap[pid]
		if fi == nil {
			fi = &forecastInfo{sku: sku, leadTime: 14}
		}

		var breakdown []ChannelVelocity
		maxVelocity := 0.0
		topChannel := ""

		for ch, agg := range channelData {
			avgDaily := float64(agg.units) / float64(lookbackDays)
			cv := ChannelVelocity{
				Channel:       ch,
				AvgDailySales: math.Round(avgDaily*100) / 100,
				Forecast30d:   math.Round(avgDaily*30*100) / 100,
				OrderCount:    agg.orderCount,
			}
			breakdown = append(breakdown, cv)
			if avgDaily > maxVelocity {
				maxVelocity = avgDaily
				topChannel = ch
			}
		}

		sort.Slice(breakdown, func(i, j int) bool {
			return breakdown[i].AvgDailySales > breakdown[j].AvgDailySales
		})

		// Reorder threshold = max_channel_velocity × lead_time × 1.2 safety factor
		reorderThreshold := fi.reorderPoint
		if reorderThreshold == 0 && maxVelocity > 0 {
			reorderThreshold = int(math.Ceil(maxVelocity * float64(fi.leadTime) * 1.2))
		}

		belowThreshold := fi.currentStock < reorderThreshold && reorderThreshold > 0

		// Check snooze
		snoozed := !fi.snoozeUntil.IsZero() && fi.snoozeUntil.After(now)
		snoozedUntilStr := ""
		if snoozed {
			snoozedUntilStr = fi.snoozeUntil.Format(time.RFC3339)
		}

		reorderQty := fi.reorderQty
		if reorderQty == 0 && maxVelocity > 0 {
			reorderQty = int(math.Ceil(maxVelocity * 30))
		}

		pd := ProductChannelDemand{
			ProductID:        pid,
			SKU:              sku,
			ProductName:      fi.name,
			CurrentStock:     fi.currentStock,
			ReorderPoint:     reorderThreshold,
			LeadTimeDays:     fi.leadTime,
			BelowThreshold:   belowThreshold,
			ReorderQty:       reorderQty,
			TopChannel:       topChannel,
			SnoozedUntil:     snoozedUntilStr,
			ChannelBreakdown: breakdown,
		}

		allProducts = append(allProducts, pd)
		if belowThreshold && !snoozed {
			alerts = append(alerts, pd)
		}
	}

	sort.Slice(allProducts, func(i, j int) bool {
		return allProducts[i].CurrentStock < allProducts[j].CurrentStock
	})
	sort.Slice(alerts, func(i, j int) bool {
		return alerts[i].CurrentStock < alerts[j].CurrentStock
	})

	c.JSON(http.StatusOK, ChannelDemandResponse{
		Period:   fmt.Sprintf("%dd", lookbackDays),
		Products: allProducts,
		Alerts:   alerts,
	})
}

// ── POST /api/v1/forecasting/reorder-alerts/:product_id/snooze ───────────────
// Snoozes a reorder alert for 7 days

func (h *ForecastingHandler) SnoozeReorderAlert(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	productID := c.Param("product_id")
	ctx := c.Request.Context()

	snoozeUntil := time.Now().UTC().AddDate(0, 0, 7)

	_, err := h.forecastCol(tenantID).Doc(productID).Set(ctx, map[string]interface{}{
		"snooze_until": snoozeUntil,
		"updated_at":   time.Now(),
	}, firestore.MergeAll)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to snooze alert"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"product_id":    productID,
		"snoozed_until": snoozeUntil.Format(time.RFC3339),
	})
}
