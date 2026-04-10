package handlers

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"sort"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"google.golang.org/api/iterator"
)

type ForecastingHandler struct {
	client *firestore.Client
}

func NewForecastingHandler(client *firestore.Client) *ForecastingHandler {
	return &ForecastingHandler{client: client}
}

type ForecastingSettings struct {
	TenantID            string    `firestore:"tenant_id"             json:"tenant_id"`
	DefaultLookbackDays int       `firestore:"default_lookback_days" json:"default_lookback_days"`
	DefaultLeadTimeDays int       `firestore:"default_lead_time_days" json:"default_lead_time_days"`
	DefaultSafetyDays   int       `firestore:"default_safety_days"   json:"default_safety_days"`
	AutoRecalcEnabled   bool      `firestore:"auto_recalc_enabled"   json:"auto_recalc_enabled"`
	UpdatedAt           time.Time `firestore:"updated_at"            json:"updated_at"`
}

type ProductForecastConfig struct {
	ProductID           string     `firestore:"product_id"            json:"product_id"`
	TenantID            string     `firestore:"tenant_id"             json:"tenant_id"`
	SKU                 string     `firestore:"sku"                   json:"sku"`
	ProductName         string     `firestore:"product_name"          json:"product_name"`
	LookbackDays        *int       `firestore:"lookback_days"         json:"lookback_days,omitempty"`
	LeadTimeDays        *int       `firestore:"lead_time_days"        json:"lead_time_days,omitempty"`
	SafetyDays          *int       `firestore:"safety_days"           json:"safety_days,omitempty"`
	AvgDailyConsumption *float64   `firestore:"avg_daily_consumption" json:"avg_daily_consumption,omitempty"`
	Seasonality         []float64  `firestore:"seasonality"           json:"seasonality,omitempty"`
	IsJustInTime        bool       `firestore:"is_just_in_time"       json:"is_just_in_time"`
	CalculatedADC       float64    `firestore:"calculated_adc"        json:"calculated_adc"`
	CurrentStock        int        `firestore:"current_stock"         json:"current_stock"`
	DaysOfStock         float64    `firestore:"days_of_stock"         json:"days_of_stock"`
	ReorderPoint        int        `firestore:"reorder_point"         json:"reorder_point"`
	ReorderQty          int        `firestore:"reorder_qty"           json:"reorder_qty"`
	ForecastStatus      string     `firestore:"forecast_status"       json:"forecast_status"`
	LastCalculatedAt    *time.Time `firestore:"last_calculated_at"    json:"last_calculated_at,omitempty"`
	UpdatedAt           time.Time  `firestore:"updated_at"            json:"updated_at"`
}

func (h *ForecastingHandler) settingsRef(tenantID string) *firestore.DocumentRef {
	return h.client.Collection(fmt.Sprintf("tenants/%s/settings", tenantID)).Doc("forecasting")
}

func (h *ForecastingHandler) forecastCol(tenantID string) *firestore.CollectionRef {
	return h.client.Collection(fmt.Sprintf("tenants/%s/product_forecasts", tenantID))
}

func (h *ForecastingHandler) loadSettings(ctx context.Context, tenantID string) ForecastingSettings {
	settings := ForecastingSettings{
		TenantID: tenantID, DefaultLookbackDays: 90,
		DefaultLeadTimeDays: 14, DefaultSafetyDays: 7, AutoRecalcEnabled: true,
	}
	doc, err := h.settingsRef(tenantID).Get(ctx)
	if err == nil { doc.DataTo(&settings) }
	return settings
}

func (h *ForecastingHandler) GetSettings(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	c.JSON(http.StatusOK, gin.H{"settings": h.loadSettings(c.Request.Context(), tenantID)})
}

func (h *ForecastingHandler) UpdateSettings(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	var req struct {
		DefaultLookbackDays *int  `json:"default_lookback_days"`
		DefaultLeadTimeDays *int  `json:"default_lead_time_days"`
		DefaultSafetyDays   *int  `json:"default_safety_days"`
		AutoRecalcEnabled   *bool `json:"auto_recalc_enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil { c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()}); return }
	ctx := c.Request.Context()
	settings := h.loadSettings(ctx, tenantID)
	if req.DefaultLookbackDays != nil {
		if *req.DefaultLookbackDays < 7 || *req.DefaultLookbackDays > 365 { c.JSON(http.StatusBadRequest, gin.H{"error": "lookback_days must be 7–365"}); return }
		settings.DefaultLookbackDays = *req.DefaultLookbackDays
	}
	if req.DefaultLeadTimeDays != nil { settings.DefaultLeadTimeDays = *req.DefaultLeadTimeDays }
	if req.DefaultSafetyDays != nil { settings.DefaultSafetyDays = *req.DefaultSafetyDays }
	if req.AutoRecalcEnabled != nil { settings.AutoRecalcEnabled = *req.AutoRecalcEnabled }
	settings.UpdatedAt = time.Now()
	if _, err := h.settingsRef(tenantID).Set(ctx, settings); err != nil { c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save"}); return }
	c.JSON(http.StatusOK, gin.H{"settings": settings})
}

func (h *ForecastingHandler) GetDashboard(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()
	iter := h.forecastCol(tenantID).Documents(ctx)
	defer iter.Stop()
	var all []ProductForecastConfig
	for {
		doc, err := iter.Next()
		if err == iterator.Done { break }
		if err != nil { break }
		var fc ProductForecastConfig
		if doc.DataTo(&fc) == nil { all = append(all, fc) }
	}
	outOfStock, critical, low, healthy := 0, 0, 0, 0
	var criticalItems, lowItems []ProductForecastConfig
	for _, fc := range all {
		switch fc.ForecastStatus {
		case "out_of_stock": outOfStock++; criticalItems = append(criticalItems, fc)
		case "critical": critical++; criticalItems = append(criticalItems, fc)
		case "low": low++; lowItems = append(lowItems, fc)
		default: healthy++
		}
	}
	sort.Slice(criticalItems, func(i, j int) bool { return criticalItems[i].DaysOfStock < criticalItems[j].DaysOfStock })
	sort.Slice(lowItems, func(i, j int) bool { return lowItems[i].DaysOfStock < lowItems[j].DaysOfStock })
	if criticalItems == nil { criticalItems = []ProductForecastConfig{} }
	if lowItems == nil { lowItems = []ProductForecastConfig{} }
	maxLow := 20
	if len(lowItems) < maxLow { maxLow = len(lowItems) }
	c.JSON(http.StatusOK, gin.H{
		"summary": gin.H{"total_skus": len(all), "out_of_stock": outOfStock, "critical": critical, "low": low, "healthy": healthy},
		"critical_items": criticalItems, "low_items": lowItems[:maxLow],
	})
}

func (h *ForecastingHandler) ListProductForecasts(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()
	statusFilter := c.Query("status")
	var iter *firestore.DocumentIterator
	if statusFilter != "" {
		iter = h.forecastCol(tenantID).Where("forecast_status", "==", statusFilter).OrderBy("days_of_stock", firestore.Asc).Documents(ctx)
	} else {
		iter = h.forecastCol(tenantID).OrderBy("days_of_stock", firestore.Asc).Documents(ctx)
	}
	defer iter.Stop()
	var items []ProductForecastConfig
	for {
		doc, err := iter.Next()
		if err == iterator.Done { break }
		if err != nil { c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"}); return }
		var fc ProductForecastConfig
		if doc.DataTo(&fc) == nil { items = append(items, fc) }
	}
	if items == nil { items = []ProductForecastConfig{} }
	c.JSON(http.StatusOK, gin.H{"forecasts": items, "count": len(items)})
}

func (h *ForecastingHandler) GetProductForecast(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	productID := c.Param("product_id")
	doc, err := h.forecastCol(tenantID).Doc(productID).Get(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"forecast": ProductForecastConfig{ProductID: productID, TenantID: tenantID, ForecastStatus: "unconfigured"}})
		return
	}
	var fc ProductForecastConfig
	if err := doc.DataTo(&fc); err != nil { c.JSON(http.StatusInternalServerError, gin.H{"error": "parse failed"}); return }
	c.JSON(http.StatusOK, gin.H{"forecast": fc})
}

func (h *ForecastingHandler) UpdateProductForecast(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	productID := c.Param("product_id")
	ctx := c.Request.Context()
	var req struct {
		LookbackDays        *int      `json:"lookback_days"`
		LeadTimeDays        *int      `json:"lead_time_days"`
		SafetyDays          *int      `json:"safety_days"`
		AvgDailyConsumption *float64  `json:"avg_daily_consumption"`
		Seasonality         []float64 `json:"seasonality"`
		IsJustInTime        *bool     `json:"is_just_in_time"`
	}
	if err := c.ShouldBindJSON(&req); err != nil { c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()}); return }
	var fc ProductForecastConfig
	doc, err := h.forecastCol(tenantID).Doc(productID).Get(ctx)
	if err == nil { doc.DataTo(&fc) } else {
		if pdoc, _ := h.client.Collection(fmt.Sprintf("tenants/%s/products", tenantID)).Doc(productID).Get(ctx); pdoc != nil && pdoc.Exists() {
			fc.SKU, _ = pdoc.Data()["sku"].(string)
			fc.ProductName, _ = pdoc.Data()["title"].(string)
		}
		fc.ProductID = productID; fc.TenantID = tenantID
	}
	if req.LookbackDays != nil { fc.LookbackDays = req.LookbackDays }
	if req.LeadTimeDays != nil { fc.LeadTimeDays = req.LeadTimeDays }
	if req.SafetyDays != nil { fc.SafetyDays = req.SafetyDays }
	if req.AvgDailyConsumption != nil { fc.AvgDailyConsumption = req.AvgDailyConsumption }
	if req.Seasonality != nil { fc.Seasonality = req.Seasonality }
	if req.IsJustInTime != nil { fc.IsJustInTime = *req.IsJustInTime }
	fc.UpdatedAt = time.Now()
	h.recalculate(ctx, tenantID, &fc)
	if _, err := h.forecastCol(tenantID).Doc(productID).Set(ctx, fc); err != nil { c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save"}); return }
	c.JSON(http.StatusOK, gin.H{"forecast": fc})
}

func (h *ForecastingHandler) Recalculate(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	var req struct{ ProductIDs []string `json:"product_ids"` }
	c.ShouldBindJSON(&req)
	ctx := c.Request.Context()
	if len(req.ProductIDs) > 0 {
		updated := 0
		for _, pid := range req.ProductIDs {
			doc, err := h.forecastCol(tenantID).Doc(pid).Get(ctx)
			if err != nil { continue }
			var fc ProductForecastConfig
			if doc.DataTo(&fc) != nil { continue }
			h.recalculate(ctx, tenantID, &fc)
			h.forecastCol(tenantID).Doc(pid).Set(ctx, fc)
			updated++
		}
		c.JSON(http.StatusOK, gin.H{"updated": updated})
		return
	}
	iter := h.forecastCol(tenantID).Documents(ctx)
	defer iter.Stop()
	updated := 0
	for {
		doc, err := iter.Next()
		if err == iterator.Done { break }
		if err != nil { break }
		var fc ProductForecastConfig
		if doc.DataTo(&fc) == nil { h.recalculate(ctx, tenantID, &fc); h.forecastCol(tenantID).Doc(fc.ProductID).Set(ctx, fc); updated++ }
	}
	pIter := h.client.Collection(fmt.Sprintf("tenants/%s/products", tenantID)).Where("status", "==", "active").Documents(ctx)
	defer pIter.Stop()
	seeded := 0
	for {
		pdoc, err := pIter.Next()
		if err == iterator.Done { break }
		if err != nil { break }
		pid := pdoc.Ref.ID
		if existing, _ := h.forecastCol(tenantID).Doc(pid).Get(ctx); existing != nil && existing.Exists() { continue }
		sku, _ := pdoc.Data()["sku"].(string)
		title, _ := pdoc.Data()["title"].(string)
		fc := ProductForecastConfig{ProductID: pid, TenantID: tenantID, SKU: sku, ProductName: title, UpdatedAt: time.Now()}
		h.recalculate(ctx, tenantID, &fc)
		h.forecastCol(tenantID).Doc(pid).Set(ctx, fc)
		seeded++
	}
	c.JSON(http.StatusOK, gin.H{"updated": updated, "seeded": seeded})
}

func (h *ForecastingHandler) recalculate(ctx context.Context, tenantID string, fc *ProductForecastConfig) {
	settings := h.loadSettings(ctx, tenantID)
	lookback := settings.DefaultLookbackDays
	if fc.LookbackDays != nil { lookback = *fc.LookbackDays }
	leadTime := settings.DefaultLeadTimeDays
	if fc.LeadTimeDays != nil { leadTime = *fc.LeadTimeDays }
	safetyDays := settings.DefaultSafetyDays
	if fc.SafetyDays != nil { safetyDays = *fc.SafetyDays }

	adc := 0.0
	if fc.AvgDailyConsumption != nil && *fc.AvgDailyConsumption > 0 {
		adc = *fc.AvgDailyConsumption
	} else if fc.ProductID != "" {
		cutoff := time.Now().AddDate(0, 0, -lookback)
		orderIter := h.client.Collection(fmt.Sprintf("tenants/%s/order_lines", tenantID)).
			Where("product_id", "==", fc.ProductID).Where("created_at", ">=", cutoff).Documents(ctx)
		totalSold := 0
		for {
			ldoc, err := orderIter.Next()
			if err != nil { break }
			if qty, ok := ldoc.Data()["quantity"].(int64); ok { totalSold += int(qty) }
		}
		orderIter.Stop()
		if lookback > 0 { adc = float64(totalSold) / float64(lookback) }
	}
	fc.CalculatedADC = math.Round(adc*100) / 100
	if len(fc.Seasonality) == 52 {
		week := time.Now().YearDay() / 7
		if week < 52 { adc = adc * fc.Seasonality[week] }
	}
	currentStock := 0
	invIter := h.client.Collection(fmt.Sprintf("tenants/%s/inventory", tenantID)).
		Where("product_id", "==", fc.ProductID).Documents(ctx)
	for {
		idoc, err := invIter.Next()
		if err != nil { break }
		if qty, ok := idoc.Data()["quantity"].(int64); ok { currentStock += int(qty) }
	}
	invIter.Stop()
	fc.CurrentStock = currentStock
	if adc > 0 { fc.DaysOfStock = math.Round(float64(currentStock)/adc*10) / 10 } else { fc.DaysOfStock = 999 }
	fc.ReorderPoint = int(math.Ceil(float64(leadTime+safetyDays) * adc))
	fc.ReorderQty = int(math.Ceil(adc * 30))
	if fc.ReorderQty < 1 { fc.ReorderQty = 1 }
	if currentStock <= 0 { fc.ForecastStatus = "out_of_stock"
	} else if fc.ReorderPoint > 0 && currentStock <= fc.ReorderPoint {
		if fc.DaysOfStock <= float64(leadTime) { fc.ForecastStatus = "critical" } else { fc.ForecastStatus = "low" }
	} else { fc.ForecastStatus = "ok" }
	now := time.Now()
	fc.LastCalculatedAt = &now
	fc.UpdatedAt = now
}
