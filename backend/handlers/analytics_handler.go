package handlers

// ============================================================================
// ANALYTICS HANDLER
// Routes:
//   GET /api/v1/analytics/overview       — total orders, revenue, units (period)
//   GET /api/v1/analytics/orders         — by status, by channel, by day
//   GET /api/v1/analytics/revenue        — by channel, by day, avg order value
//   GET /api/v1/analytics/top-products   — top 20 SKUs by units and revenue
//   GET /api/v1/analytics/inventory      — low stock / out-of-stock / overstock
//   GET /api/v1/analytics/returns        — return rate by channel and product
// ============================================================================

import (
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"google.golang.org/api/iterator"

	"module-a/models"
)

type AnalyticsHandler struct {
	client *firestore.Client
}

func NewAnalyticsHandler(client *firestore.Client) *AnalyticsHandler {
	return &AnalyticsHandler{client: client}
}

// ============================================================================
// HELPERS
// ============================================================================

// parsePeriod returns start/end times for the requested period.
// Query params: period=today|7d|30d|90d  OR  date_from + date_to (YYYY-MM-DD).
func parsePeriod(c *gin.Context) (time.Time, time.Time) {
	now := time.Now().UTC()
	end := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 0, time.UTC)

	if from := c.Query("date_from"); from != "" {
		if to := c.Query("date_to"); to != "" {
			start, err1 := time.Parse("2006-01-02", from)
			finish, err2 := time.Parse("2006-01-02", to)
			if err1 == nil && err2 == nil {
				finish = time.Date(finish.Year(), finish.Month(), finish.Day(), 23, 59, 59, 0, time.UTC)
				return start, finish
			}
		}
	}

	period := c.DefaultQuery("period", "30d")
	var start time.Time
	switch period {
	case "today":
		start = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	case "7d":
		start = end.AddDate(0, 0, -6)
		start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
	case "90d":
		start = end.AddDate(0, 0, -89)
		start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
	default: // 30d
		start = end.AddDate(0, 0, -29)
		start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
	}
	return start, end
}

// orderInRange returns true if the order's created_at falls within [start, end].
func orderInRange(order models.Order, start, end time.Time) bool {
	ts, err := time.Parse(time.RFC3339, order.CreatedAt)
	if err != nil {
		// try date-only
		ts, err = time.Parse("2006-01-02", order.CreatedAt)
		if err != nil {
			return false
		}
	}
	return !ts.Before(start) && !ts.After(end)
}

// dayKey returns "YYYY-MM-DD" for a timestamp string.
func dayKey(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		t2, err2 := time.Parse("2006-01-02", ts)
		if err2 != nil {
			return ts[:10]
		}
		return t2.Format("2006-01-02")
	}
	return t.Format("2006-01-02")
}

// ============================================================================
// GET /analytics/overview
// ============================================================================

type OverviewResponse struct {
	Period        string  `json:"period"`
	DateFrom      string  `json:"date_from"`
	DateTo        string  `json:"date_to"`
	TotalOrders   int     `json:"total_orders"`
	TotalRevenue  float64 `json:"total_revenue"`
	Currency      string  `json:"currency"`
	AvgOrderValue float64 `json:"avg_order_value"`
	UnitsDispatched int   `json:"units_dispatched"`
	OrdersByStatus map[string]int `json:"orders_by_status"`
}

func (h *AnalyticsHandler) GetOverview(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	start, end := parsePeriod(c)
	period := c.DefaultQuery("period", "30d")

	resp := OverviewResponse{
		Period:         period,
		DateFrom:       start.Format("2006-01-02"),
		DateTo:         end.Format("2006-01-02"),
		OrdersByStatus: map[string]int{},
		Currency:       "GBP",
	}

	iter := h.client.Collection(fmt.Sprintf("tenants/%s/orders", tenantID)).Documents(c.Request.Context())
	defer iter.Stop()

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		var order models.Order
		if err := doc.DataTo(&order); err != nil {
			continue
		}
		if !orderInRange(order, start, end) {
			continue
		}

		resp.TotalOrders++
		resp.TotalRevenue += order.Totals.GrandTotal.Amount
		if order.Totals.GrandTotal.Currency != "" {
			resp.Currency = order.Totals.GrandTotal.Currency
		}
		resp.OrdersByStatus[order.Status]++

		if order.Status == "fulfilled" || order.Status == "dispatched" {
			raw := doc.Data()
			linesRaw, _ := raw["lines"].([]interface{})
			for _, lr := range linesRaw {
				lm, ok := lr.(map[string]interface{})
				if !ok {
					continue
				}
				if q, ok := lm["quantity"].(int64); ok {
					resp.UnitsDispatched += int(q)
				} else if q, ok := lm["quantity"].(float64); ok {
					resp.UnitsDispatched += int(q)
				}
			}
		}
	}

	if resp.TotalOrders > 0 {
		resp.AvgOrderValue = math.Round(resp.TotalRevenue/float64(resp.TotalOrders)*100) / 100
	}
	resp.TotalRevenue = math.Round(resp.TotalRevenue*100) / 100

	c.JSON(http.StatusOK, resp)
}

// ============================================================================
// GET /analytics/orders
// ============================================================================

type OrdersAnalyticsResponse struct {
	DateFrom     string           `json:"date_from"`
	DateTo       string           `json:"date_to"`
	ByStatus     map[string]int   `json:"by_status"`
	ByChannel    map[string]int   `json:"by_channel"`
	ByDay        []DayCount       `json:"by_day"`
}

type DayCount struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

func (h *AnalyticsHandler) GetOrdersAnalytics(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	start, end := parsePeriod(c)

	resp := OrdersAnalyticsResponse{
		DateFrom:  start.Format("2006-01-02"),
		DateTo:    end.Format("2006-01-02"),
		ByStatus:  map[string]int{},
		ByChannel: map[string]int{},
	}
	dayMap := map[string]int{}

	iter := h.client.Collection(fmt.Sprintf("tenants/%s/orders", tenantID)).Documents(c.Request.Context())
	defer iter.Stop()

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		var order models.Order
		if err := doc.DataTo(&order); err != nil {
			continue
		}
		if !orderInRange(order, start, end) {
			continue
		}

		resp.ByStatus[order.Status]++
		ch := order.Channel
		if ch == "" {
			ch = "unknown"
		}
		resp.ByChannel[ch]++
		dayMap[dayKey(order.CreatedAt)]++
	}

	// Build sorted daily series
	days := make([]DayCount, 0, len(dayMap))
	for d, cnt := range dayMap {
		days = append(days, DayCount{Date: d, Count: cnt})
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Date < days[j].Date })
	resp.ByDay = days

	c.JSON(http.StatusOK, resp)
}

// ============================================================================
// GET /analytics/revenue
// ============================================================================

type RevenueAnalyticsResponse struct {
	DateFrom      string             `json:"date_from"`
	DateTo        string             `json:"date_to"`
	Currency      string             `json:"currency"`
	TotalRevenue  float64            `json:"total_revenue"`
	AvgOrderValue float64            `json:"avg_order_value"`
	ByChannel     map[string]float64 `json:"by_channel"`
	ByDay         []DayRevenue       `json:"by_day"`
}

type DayRevenue struct {
	Date    string  `json:"date"`
	Revenue float64 `json:"revenue"`
	Orders  int     `json:"orders"`
}

func (h *AnalyticsHandler) GetRevenueAnalytics(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	start, end := parsePeriod(c)

	resp := RevenueAnalyticsResponse{
		DateFrom:  start.Format("2006-01-02"),
		DateTo:    end.Format("2006-01-02"),
		Currency:  "GBP",
		ByChannel: map[string]float64{},
	}
	type dayData struct {
		revenue float64
		orders  int
	}
	dayMap := map[string]*dayData{}
	totalOrders := 0

	iter := h.client.Collection(fmt.Sprintf("tenants/%s/orders", tenantID)).Documents(c.Request.Context())
	defer iter.Stop()

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		var order models.Order
		if err := doc.DataTo(&order); err != nil {
			continue
		}
		if !orderInRange(order, start, end) {
			continue
		}
		if order.Status == "cancelled" {
			continue
		}

		amount := order.Totals.GrandTotal.Amount
		resp.TotalRevenue += amount
		if order.Totals.GrandTotal.Currency != "" {
			resp.Currency = order.Totals.GrandTotal.Currency
		}
		totalOrders++

		ch := order.Channel
		if ch == "" {
			ch = "unknown"
		}
		resp.ByChannel[ch] += amount

		dk := dayKey(order.CreatedAt)
		if _, ok := dayMap[dk]; !ok {
			dayMap[dk] = &dayData{}
		}
		dayMap[dk].revenue += amount
		dayMap[dk].orders++
	}

	if totalOrders > 0 {
		resp.AvgOrderValue = math.Round(resp.TotalRevenue/float64(totalOrders)*100) / 100
	}
	resp.TotalRevenue = math.Round(resp.TotalRevenue*100) / 100
	for k := range resp.ByChannel {
		resp.ByChannel[k] = math.Round(resp.ByChannel[k]*100) / 100
	}

	days := make([]DayRevenue, 0, len(dayMap))
	for d, dd := range dayMap {
		days = append(days, DayRevenue{Date: d, Revenue: math.Round(dd.revenue*100) / 100, Orders: dd.orders})
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Date < days[j].Date })
	resp.ByDay = days

	c.JSON(http.StatusOK, resp)
}

// ============================================================================
// GET /analytics/top-products
// ============================================================================

type TopProductsResponse struct {
	DateFrom string           `json:"date_from"`
	DateTo   string           `json:"date_to"`
	Currency string           `json:"currency"`
	Products []TopProductItem `json:"products"`
}

type TopProductItem struct {
	SKU      string  `json:"sku"`
	Title    string  `json:"title"`
	Units    int     `json:"units"`
	Revenue  float64 `json:"revenue"`
	Orders   int     `json:"orders"`
}

func (h *AnalyticsHandler) GetTopProducts(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	start, end := parsePeriod(c)

	type skuAgg struct {
		title   string
		units   int
		revenue float64
		orders  map[string]bool
	}
	skuMap := map[string]*skuAgg{}
	currency := "GBP"

	iter := h.client.Collection(fmt.Sprintf("tenants/%s/orders", tenantID)).Documents(c.Request.Context())
	defer iter.Stop()

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}

		var order models.Order
		if err := doc.DataTo(&order); err != nil {
			continue
		}
		if !orderInRange(order, start, end) {
			continue
		}
		if order.Status == "cancelled" {
			continue
		}
		if order.Totals.GrandTotal.Currency != "" {
			currency = order.Totals.GrandTotal.Currency
		}

		// Extract lines from raw map (embedded in the order doc)
		raw := doc.Data()
		linesRaw, _ := raw["lines"].([]interface{})
		for _, lr := range linesRaw {
			lm, ok := lr.(map[string]interface{})
			if !ok {
				continue
			}
			sku, _ := lm["sku"].(string)
			if sku == "" {
				continue
			}
			title, _ := lm["title"].(string)
			qty := 0
			if q, ok := lm["quantity"].(int64); ok {
				qty = int(q)
			} else if q, ok := lm["quantity"].(float64); ok {
				qty = int(q)
			}
			lineRev := 0.0
			if lt, ok := lm["line_total"].(map[string]interface{}); ok {
				if a, ok := lt["amount"].(float64); ok {
					lineRev = a
				}
			}

			if _, exists := skuMap[sku]; !exists {
				skuMap[sku] = &skuAgg{title: title, orders: map[string]bool{}}
			}
			skuMap[sku].units += qty
			skuMap[sku].revenue += lineRev
			skuMap[sku].orders[order.OrderID] = true
			if title != "" && skuMap[sku].title == "" {
				skuMap[sku].title = title
			}
		}
	}

	products := make([]TopProductItem, 0, len(skuMap))
	for sku, agg := range skuMap {
		products = append(products, TopProductItem{
			SKU:     sku,
			Title:   agg.title,
			Units:   agg.units,
			Revenue: math.Round(agg.revenue*100) / 100,
			Orders:  len(agg.orders),
		})
	}
	sort.Slice(products, func(i, j int) bool { return products[i].Units > products[j].Units })
	if len(products) > 20 {
		products = products[:20]
	}

	c.JSON(http.StatusOK, TopProductsResponse{
		DateFrom: start.Format("2006-01-02"),
		DateTo:   end.Format("2006-01-02"),
		Currency: currency,
		Products: products,
	})
}

// ============================================================================
// GET /analytics/inventory
// ============================================================================

type InventoryHealthResponse struct {
	TotalSKUs    int `json:"total_skus"`
	OutOfStock   int `json:"out_of_stock"`
	LowStock     int `json:"low_stock"`
	Healthy      int `json:"healthy"`
	Overstock    int `json:"overstock"`
}

func (h *AnalyticsHandler) GetInventoryHealth(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	resp := InventoryHealthResponse{}

	iter := h.client.Collection(fmt.Sprintf("tenants/%s/inventory", tenantID)).Documents(c.Request.Context())
	defer iter.Stop()

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		var item struct {
			TotalAvailable int `firestore:"total_available"`
			TotalOnHand    int `firestore:"total_on_hand"`
			ReorderPoint   int `firestore:"reorder_point"`
			SafetyStock    int `firestore:"safety_stock"`
		}
		if err := doc.DataTo(&item); err != nil {
			continue
		}

		resp.TotalSKUs++
		avail := item.TotalAvailable
		reorder := item.ReorderPoint
		if reorder == 0 {
			reorder = item.SafetyStock
		}

		switch {
		case avail <= 0:
			resp.OutOfStock++
		case reorder > 0 && avail <= reorder:
			resp.LowStock++
		case reorder > 0 && avail >= reorder*5:
			resp.Overstock++
		default:
			resp.Healthy++
		}
	}

	c.JSON(http.StatusOK, resp)
}

// ============================================================================
// GET /analytics/returns
// ============================================================================

type ReturnsAnalyticsResponse struct {
	DateFrom       string             `json:"date_from"`
	DateTo         string             `json:"date_to"`
	TotalRMAs      int                `json:"total_rmas"`
	ByChannel      map[string]int     `json:"by_channel"`
	ByReasonCode   map[string]int     `json:"by_reason_code"`
	TopReturnedSKUs []ReturnedSKU     `json:"top_returned_skus"`
}

type ReturnedSKU struct {
	SKU         string `json:"sku"`
	ProductName string `json:"product_name"`
	QtyReturned int    `json:"qty_returned"`
	RMACount    int    `json:"rma_count"`
}

func (h *AnalyticsHandler) GetReturnsAnalytics(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	start, end := parsePeriod(c)

	resp := ReturnsAnalyticsResponse{
		DateFrom:     start.Format("2006-01-02"),
		DateTo:       end.Format("2006-01-02"),
		ByChannel:    map[string]int{},
		ByReasonCode: map[string]int{},
	}

	type skuData struct {
		name string
		qty  int
		rmas int
	}
	skuMap := map[string]*skuData{}

	iter := h.client.Collection(fmt.Sprintf("tenants/%s/rmas", tenantID)).Documents(c.Request.Context())
	defer iter.Stop()

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		var rma models.RMA
		if err := doc.DataTo(&rma); err != nil {
			continue
		}

		// Filter by created_at (time.Time on RMA model)
		if rma.CreatedAt.Before(start) || rma.CreatedAt.After(end) {
			continue
		}

		resp.TotalRMAs++
		ch := rma.Channel
		if ch == "" {
			ch = "unknown"
		}
		resp.ByChannel[ch]++

		for _, line := range rma.Lines {
			rc := line.ReasonCode
			if rc == "" {
				rc = "other"
			}
			resp.ByReasonCode[rc]++

			sku := line.SKU
			if sku == "" {
				continue
			}
			if _, ok := skuMap[sku]; !ok {
				skuMap[sku] = &skuData{name: line.ProductName}
			}
			skuMap[sku].qty += line.QtyRequested
			skuMap[sku].rmas++
		}
	}

	skus := make([]ReturnedSKU, 0, len(skuMap))
	for sku, d := range skuMap {
		skus = append(skus, ReturnedSKU{SKU: sku, ProductName: d.name, QtyReturned: d.qty, RMACount: d.rmas})
	}
	sort.Slice(skus, func(i, j int) bool { return skus[i].QtyReturned > skus[j].QtyReturned })
	if len(skus) > 20 {
		skus = skus[:20]
	}
	resp.TopReturnedSKUs = skus

	c.JSON(http.StatusOK, resp)
}


// ============================================================================
// GET /analytics/home
// Returns a single combined payload for the home dashboard:
//   - today's order count, revenue, dispatched count
//   - low-stock item count
//   - open orders by status (map)
//   - revenue by channel for last 30 days
//   - top 5 consumed SKUs (by units) for last 30 days
//   - last 10 imported orders (activity feed)
// ============================================================================

type HomeActivityOrder struct {
	OrderID      string  `json:"order_id"`
	Channel      string  `json:"channel"`
	CustomerName string  `json:"customer_name"`
	Total        float64 `json:"total"`
	Currency     string  `json:"currency"`
	Status       string  `json:"status"`
	ImportedAt   string  `json:"imported_at"`
}

type HomeConsumedSKU struct {
	SKU           string  `json:"sku"`
	Title         string  `json:"title"`
	UnitsConsumed int     `json:"units_consumed"`
	Revenue       float64 `json:"revenue"`
}

type HomeDashboardResponse struct {
	// KPI tiles
	OrdersToday     int     `json:"orders_today"`
	RevenueToday    float64 `json:"revenue_today"`
	Currency        string  `json:"currency"`
	DispatchedToday int     `json:"dispatched_today"`
	LowStockCount   int     `json:"low_stock_count"`

	// Open orders by status
	OpenOrdersByStatus map[string]int `json:"open_orders_by_status"`

	// Revenue by channel (last 30d)
	RevenueByChannel map[string]float64 `json:"revenue_by_channel"`

	// Top 5 consumed SKUs (last 30d)
	TopConsumedSKUs []HomeConsumedSKU `json:"top_consumed_skus"`

	// Last 10 imported orders
	RecentActivity []HomeActivityOrder `json:"recent_activity"`
}

func (h *AnalyticsHandler) GetHome(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()
	now := time.Now().UTC()

	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	todayEnd := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 0, time.UTC)
	thirtyDaysAgo := todayEnd.AddDate(0, 0, -29)
	thirtyStart := time.Date(thirtyDaysAgo.Year(), thirtyDaysAgo.Month(), thirtyDaysAgo.Day(), 0, 0, 0, 0, time.UTC)

	resp := HomeDashboardResponse{
		Currency:           "GBP",
		OpenOrdersByStatus: map[string]int{},
		RevenueByChannel:   map[string]float64{},
		TopConsumedSKUs:    []HomeConsumedSKU{},
		RecentActivity:     []HomeActivityOrder{},
	}

	// ── Pass 1: Scan all orders ──────────────────────────────────────────────
	type skuAgg struct {
		title   string
		units   int
		revenue float64
	}
	skuMap := map[string]*skuAgg{}

	// Collect last 10 imported orders (sorted by imported_at desc)
	type importedEntry struct {
		importedAt time.Time
		order      HomeActivityOrder
	}
	var recentBuf []importedEntry

	openStatuses := map[string]bool{
		"imported":        true,
		"processing":      true,
		"on_hold":         true,
		"ready_to_fulfil": true,
	}

	iter := h.client.Collection(fmt.Sprintf("tenants/%s/orders", tenantID)).Documents(ctx)
	defer iter.Stop()

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		var order models.Order
		if err := doc.DataTo(&order); err != nil {
			continue
		}

		// Parse created_at for today checks
		createdAt, _ := time.Parse(time.RFC3339, order.CreatedAt)
		if createdAt.IsZero() {
			createdAt, _ = time.Parse("2006-01-02", order.CreatedAt)
		}

		// --- KPI: today's orders & revenue
		if !createdAt.Before(todayStart) && !createdAt.After(todayEnd) {
			if order.Status != "cancelled" {
				resp.OrdersToday++
				resp.RevenueToday += order.Totals.GrandTotal.Amount
				if order.Totals.GrandTotal.Currency != "" {
					resp.Currency = order.Totals.GrandTotal.Currency
				}
			}
		}

		// --- KPI: dispatched today
		if (order.Status == "fulfilled" || order.Status == "dispatched") &&
			!createdAt.Before(todayStart) && !createdAt.After(todayEnd) {
			resp.DispatchedToday++
		}

		// --- Open orders by status
		if openStatuses[order.Status] {
			resp.OpenOrdersByStatus[order.Status]++
		}

		// --- Revenue by channel (last 30d)
		if !createdAt.Before(thirtyStart) && !createdAt.After(todayEnd) && order.Status != "cancelled" {
			ch := order.Channel
			if ch == "" {
				ch = "unknown"
			}
			resp.RevenueByChannel[ch] += order.Totals.GrandTotal.Amount
		}

		// --- Top consumed SKUs (last 30d, fulfilled/dispatched orders)
		if !createdAt.Before(thirtyStart) && !createdAt.After(todayEnd) &&
			(order.Status == "fulfilled" || order.Status == "dispatched") {
			raw := doc.Data()
			linesRaw, _ := raw["lines"].([]interface{})
			for _, lr := range linesRaw {
				lm, ok := lr.(map[string]interface{})
				if !ok {
					continue
				}
				sku, _ := lm["sku"].(string)
				if sku == "" {
					continue
				}
				title, _ := lm["title"].(string)
				qty := 0
				if q, ok := lm["quantity"].(int64); ok {
					qty = int(q)
				} else if q, ok := lm["quantity"].(float64); ok {
					qty = int(q)
				}
				lineRev := 0.0
				if lt, ok := lm["line_total"].(map[string]interface{}); ok {
					if a, ok := lt["amount"].(float64); ok {
						lineRev = a
					}
				}
				if _, exists := skuMap[sku]; !exists {
					skuMap[sku] = &skuAgg{title: title}
				}
				skuMap[sku].units += qty
				skuMap[sku].revenue += lineRev
				if title != "" && skuMap[sku].title == "" {
					skuMap[sku].title = title
				}
			}
		}

		// --- Recent activity feed (all orders, collect all then sort)
		importedAt, _ := time.Parse(time.RFC3339, order.ImportedAt)
		if importedAt.IsZero() {
			importedAt = createdAt
		}
		entry := importedEntry{
			importedAt: importedAt,
			order: HomeActivityOrder{
				OrderID:      order.OrderID,
				Channel:      order.Channel,
				CustomerName: order.Customer.Name,
				Total:        math.Round(order.Totals.GrandTotal.Amount*100) / 100,
				Currency:     order.Totals.GrandTotal.Currency,
				Status:       order.Status,
				ImportedAt:   order.ImportedAt,
			},
		}
		if entry.order.Currency == "" {
			entry.order.Currency = resp.Currency
		}
		recentBuf = append(recentBuf, entry)
	}

	// Round revenue fields
	resp.RevenueToday = math.Round(resp.RevenueToday*100) / 100
	for k := range resp.RevenueByChannel {
		resp.RevenueByChannel[k] = math.Round(resp.RevenueByChannel[k]*100) / 100
	}

	// Build top 5 consumed SKUs
	skuList := make([]HomeConsumedSKU, 0, len(skuMap))
	for sku, agg := range skuMap {
		skuList = append(skuList, HomeConsumedSKU{
			SKU:           sku,
			Title:         agg.title,
			UnitsConsumed: agg.units,
			Revenue:       math.Round(agg.revenue*100) / 100,
		})
	}
	sort.Slice(skuList, func(i, j int) bool { return skuList[i].UnitsConsumed > skuList[j].UnitsConsumed })
	if len(skuList) > 5 {
		skuList = skuList[:5]
	}
	resp.TopConsumedSKUs = skuList

	// Build recent activity (last 10 by imported_at desc)
	sort.Slice(recentBuf, func(i, j int) bool { return recentBuf[i].importedAt.After(recentBuf[j].importedAt) })
	if len(recentBuf) > 10 {
		recentBuf = recentBuf[:10]
	}
	for _, e := range recentBuf {
		resp.RecentActivity = append(resp.RecentActivity, e.order)
	}

	// ── Pass 2: Low stock count from inventory collection ───────────────────
	invIter := h.client.Collection(fmt.Sprintf("tenants/%s/inventory", tenantID)).Documents(ctx)
	defer invIter.Stop()
	for {
		doc, err := invIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		var item struct {
			TotalAvailable int `firestore:"total_available"`
			ReorderPoint   int `firestore:"reorder_point"`
			SafetyStock    int `firestore:"safety_stock"`
		}
		if err := doc.DataTo(&item); err != nil {
			continue
		}
		reorder := item.ReorderPoint
		if reorder == 0 {
			reorder = item.SafetyStock
		}
		if reorder == 0 {
			reorder = 5 // sensible default when no reorder point set
		}
		if item.TotalAvailable <= reorder {
			resp.LowStockCount++
		}
	}

	c.JSON(http.StatusOK, resp)
}

// ============================================================================
// GET /analytics/stock-consumption?period=7d|30d|90d
// Returns top 20 SKUs by units fulfilled in the period.
// Each item: sku, title, units_consumed, revenue.
// ============================================================================

type StockConsumptionItem struct {
	SKU           string  `json:"sku"`
	Title         string  `json:"title"`
	UnitsConsumed int     `json:"units_consumed"`
	Revenue       float64 `json:"revenue"`
}

type StockConsumptionResponse struct {
	Period   string                 `json:"period"`
	DateFrom string                 `json:"date_from"`
	DateTo   string                 `json:"date_to"`
	Currency string                 `json:"currency"`
	Items    []StockConsumptionItem `json:"items"`
}

func (h *AnalyticsHandler) GetStockConsumption(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	start, end := parsePeriod(c)
	period := c.DefaultQuery("period", "30d")
	currency := "GBP"

	type skuAgg struct {
		title   string
		units   int
		revenue float64
	}
	skuMap := map[string]*skuAgg{}

	iter := h.client.Collection(fmt.Sprintf("tenants/%s/orders", tenantID)).Documents(c.Request.Context())
	defer iter.Stop()

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		var order models.Order
		if err := doc.DataTo(&order); err != nil {
			continue
		}
		// Only fulfilled/dispatched orders in range
		if order.Status != "fulfilled" && order.Status != "dispatched" {
			continue
		}
		if !orderInRange(order, start, end) {
			continue
		}
		if order.Totals.GrandTotal.Currency != "" {
			currency = order.Totals.GrandTotal.Currency
		}

		raw := doc.Data()
		linesRaw, _ := raw["lines"].([]interface{})
		for _, lr := range linesRaw {
			lm, ok := lr.(map[string]interface{})
			if !ok {
				continue
			}
			sku, _ := lm["sku"].(string)
			if sku == "" {
				continue
			}
			title, _ := lm["title"].(string)
			qty := 0
			if q, ok := lm["quantity"].(int64); ok {
				qty = int(q)
			} else if q, ok := lm["quantity"].(float64); ok {
				qty = int(q)
			}
			lineRev := 0.0
			if lt, ok := lm["line_total"].(map[string]interface{}); ok {
				if a, ok := lt["amount"].(float64); ok {
					lineRev = a
				}
			}
			if _, exists := skuMap[sku]; !exists {
				skuMap[sku] = &skuAgg{title: title}
			}
			skuMap[sku].units += qty
			skuMap[sku].revenue += lineRev
			if title != "" && skuMap[sku].title == "" {
				skuMap[sku].title = title
			}
		}
	}

	items := make([]StockConsumptionItem, 0, len(skuMap))
	for sku, agg := range skuMap {
		items = append(items, StockConsumptionItem{
			SKU:           sku,
			Title:         agg.title,
			UnitsConsumed: agg.units,
			Revenue:       math.Round(agg.revenue*100) / 100,
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].UnitsConsumed > items[j].UnitsConsumed })
	if len(items) > 20 {
		items = items[:20]
	}

	c.JSON(http.StatusOK, StockConsumptionResponse{
		Period:   period,
		DateFrom: start.Format("2006-01-02"),
		DateTo:   end.Format("2006-01-02"),
		Currency: currency,
		Items:    items,
	})
}

// ============================================================================
// GET /analytics/inventory-dashboard
// Returns per-location stock summary: location_id, name, sku_count,
// total_on_hand, total_reserved, total_available, total_value.
// total_value = sum(on_hand * cost_price) joined from products collection.
// ============================================================================

type InventoryDashboardLocation struct {
	LocationID    string  `json:"location_id"`
	Name          string  `json:"name"`
	SKUCount      int     `json:"sku_count"`
	TotalOnHand   int     `json:"total_on_hand"`
	TotalReserved int     `json:"total_reserved"`
	TotalAvailable int    `json:"total_available"`
	TotalValue    float64 `json:"total_value"`
}

type InventoryDashboardResponse struct {
	TotalSKUs    int                          `json:"total_skus"`
	TotalValue   float64                      `json:"total_value"`
	OutOfStock   int                          `json:"out_of_stock_count"`
	Locations    []InventoryDashboardLocation `json:"locations"`
}

func (h *AnalyticsHandler) GetInventoryDashboard(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	// Build a cost_price lookup map from products
	costMap := map[string]float64{}
	prodIter := h.client.Collection(fmt.Sprintf("tenants/%s/products", tenantID)).Documents(ctx)
	defer prodIter.Stop()
	for {
		doc, err := prodIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		data := doc.Data()
		sku, _ := data["sku"].(string)
		if sku == "" {
			continue
		}
		cp := 0.0
		if v, ok := data["cost_price"].(float64); ok {
			cp = v
		}
		costMap[sku] = cp
	}

	// Aggregate inventory records by location
	type locAgg struct {
		name      string
		skus      map[string]bool
		onHand    int
		reserved  int
		available int
		value     float64
	}
	locMap := map[string]*locAgg{}

	invIter := h.client.Collection(fmt.Sprintf("tenants/%s/inventory", tenantID)).Documents(ctx)
	defer invIter.Stop()
	outOfStock := 0
	allSKUs := map[string]bool{}

	for {
		doc, err := invIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		var rec InventoryRecord
		if err := doc.DataTo(&rec); err != nil {
			continue
		}

		locID := rec.LocationID
		if locID == "" {
			locID = "unassigned"
		}
		if _, ok := locMap[locID]; !ok {
			locMap[locID] = &locAgg{name: rec.LocationName, skus: map[string]bool{}}
		}

		// Determine SKU from product lookup via product_id
		data := doc.Data()
		sku, _ := data["sku"].(string)

		locMap[locID].skus[rec.ProductID] = true
		locMap[locID].onHand += rec.Quantity
		locMap[locID].reserved += rec.ReservedQty
		locMap[locID].available += rec.AvailableQty
		if sku != "" {
			locMap[locID].value += float64(rec.Quantity) * costMap[sku]
			allSKUs[sku] = true
		}
		if rec.AvailableQty <= 0 {
			outOfStock++
		}
	}

	// Build sorted response
	locations := make([]InventoryDashboardLocation, 0, len(locMap))
	totalValue := 0.0
	for locID, agg := range locMap {
		totalValue += agg.value
		locations = append(locations, InventoryDashboardLocation{
			LocationID:     locID,
			Name:           agg.name,
			SKUCount:       len(agg.skus),
			TotalOnHand:    agg.onHand,
			TotalReserved:  agg.reserved,
			TotalAvailable: agg.available,
			TotalValue:     math.Round(agg.value*100) / 100,
		})
	}
	sort.Slice(locations, func(i, j int) bool {
		return locations[i].TotalOnHand > locations[j].TotalOnHand
	})

	c.JSON(http.StatusOK, InventoryDashboardResponse{
		TotalSKUs:  len(allSKUs),
		TotalValue: math.Round(totalValue*100) / 100,
		OutOfStock: outOfStock,
		Locations:  locations,
	})
}

// ============================================================================
// GET /analytics/order-dashboard
// Returns open orders grouped by status with counts, plus daily order volume
// for the last 30 days.
// ============================================================================

type OrderDashboardDayVolume struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

type OrderDashboardResponse struct {
	ByStatus    map[string]int            `json:"by_status"`
	DailyVolume []OrderDashboardDayVolume `json:"daily_volume"`
	OldestOpen  []OrderDashboardOrder     `json:"oldest_open"`
}

type OrderDashboardOrder struct {
	OrderID     string  `json:"order_id"`
	Channel     string  `json:"channel"`
	Status      string  `json:"status"`
	Total       float64 `json:"total"`
	Currency    string  `json:"currency"`
	OrderDate   string  `json:"order_date"`
	CreatedAt   string  `json:"created_at"`
	SLAAtRisk   bool    `json:"sla_at_risk"`
}

func (h *AnalyticsHandler) GetOrderDashboard(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	now := time.Now().UTC()
	thirtyStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -29)
	todayEnd := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 0, time.UTC)

	openStatuses := map[string]bool{
		"imported":        true,
		"processing":      true,
		"on_hold":         true,
		"ready_to_fulfil": true,
		"parked":          true,
	}

	byStatus := map[string]int{}
	dayMap := map[string]int{}
	var openOrders []OrderDashboardOrder

	iter := h.client.Collection(fmt.Sprintf("tenants/%s/orders", tenantID)).Documents(ctx)
	defer iter.Stop()

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		var order models.Order
		if err := doc.DataTo(&order); err != nil {
			continue
		}

		// Count open statuses (all time)
		if openStatuses[order.Status] {
			byStatus[order.Status]++
		}

		// Daily volume for last 30d
		createdAt, _ := time.Parse(time.RFC3339, order.CreatedAt)
		if createdAt.IsZero() {
			createdAt, _ = time.Parse("2006-01-02", order.CreatedAt)
		}
		if !createdAt.Before(thirtyStart) && !createdAt.After(todayEnd) {
			dayMap[dayKey(order.CreatedAt)]++
		}

		// Collect open orders for oldest-open list
		if openStatuses[order.Status] {
			openOrders = append(openOrders, OrderDashboardOrder{
				OrderID:   order.OrderID,
				Channel:   order.Channel,
				Status:    order.Status,
				Total:     math.Round(order.Totals.GrandTotal.Amount*100) / 100,
				Currency:  order.Totals.GrandTotal.Currency,
				OrderDate: order.OrderDate,
				CreatedAt: order.CreatedAt,
				SLAAtRisk: order.SLAAtRisk,
			})
		}
	}

	// Sort open orders oldest first
	sort.Slice(openOrders, func(i, j int) bool {
		return openOrders[i].CreatedAt < openOrders[j].CreatedAt
	})
	if len(openOrders) > 20 {
		openOrders = openOrders[:20]
	}

	// Build sorted daily volume
	days := make([]OrderDashboardDayVolume, 0, len(dayMap))
	for d, cnt := range dayMap {
		days = append(days, OrderDashboardDayVolume{Date: d, Count: cnt})
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Date < days[j].Date })

	c.JSON(http.StatusOK, OrderDashboardResponse{
		ByStatus:    byStatus,
		DailyVolume: days,
		OldestOpen:  openOrders,
	})
}

// ============================================================================
// GET /analytics/pivot/fields
// Returns available group_by fields and metrics per entity.
// ============================================================================

type PivotFieldsResponse struct {
	Entities map[string]PivotEntityFields `json:"entities"`
}

type PivotEntityFields struct {
	GroupByFields []PivotField `json:"group_by_fields"`
	Metrics       []PivotField `json:"metrics"`
}

type PivotField struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

func (h *AnalyticsHandler) GetPivotFields(c *gin.Context) {
	resp := PivotFieldsResponse{
		Entities: map[string]PivotEntityFields{
			"orders": {
				GroupByFields: []PivotField{
					{Key: "channel", Label: "Channel"},
					{Key: "status", Label: "Status"},
					{Key: "payment_status", Label: "Payment Status"},
					{Key: "fulfilment_source", Label: "Fulfilment Source"},
					{Key: "shipping_country", Label: "Shipping Country"},
					{Key: "order_date", Label: "Order Date (Day)"},
				},
				Metrics: []PivotField{
					{Key: "count", Label: "Order Count"},
					{Key: "revenue", Label: "Revenue"},
					{Key: "avg_order_value", Label: "Avg Order Value"},
					{Key: "units", Label: "Units Ordered"},
				},
			},
			"products": {
				GroupByFields: []PivotField{
					{Key: "product_type", Label: "Product Type"},
					{Key: "brand", Label: "Brand"},
					{Key: "status", Label: "Status"},
				},
				Metrics: []PivotField{
					{Key: "count", Label: "SKU Count"},
					{Key: "total_on_hand", Label: "Total On Hand"},
					{Key: "total_available", Label: "Total Available"},
				},
			},
			"inventory": {
				GroupByFields: []PivotField{
					{Key: "location_name", Label: "Location"},
					{Key: "source_id", Label: "Source"},
				},
				Metrics: []PivotField{
					{Key: "count", Label: "Record Count"},
					{Key: "total_on_hand", Label: "Total On Hand"},
					{Key: "total_reserved", Label: "Total Reserved"},
					{Key: "total_available", Label: "Total Available"},
				},
			},
			"rmas": {
				GroupByFields: []PivotField{
					{Key: "channel", Label: "Channel"},
					{Key: "status", Label: "Status"},
					{Key: "refund_action", Label: "Refund Action"},
				},
				Metrics: []PivotField{
					{Key: "count", Label: "RMA Count"},
					{Key: "refund_amount", Label: "Refund Amount"},
				},
			},
		},
	}
	c.JSON(http.StatusOK, resp)
}

// pivotLeaf is a package-level type used by RunPivot and its helper functions.
type leaf struct {
	count  int
	metric float64
}

// ============================================================================
// POST /analytics/pivot
// Accepts: { entity, group_by: [field,...], metrics: [metric,...], date_from, date_to }
// Returns a hierarchical result tree (max 3 levels of grouping).
// ============================================================================

type PivotRequest struct {
	Entity   string   `json:"entity" binding:"required"`
	GroupBy  []string `json:"group_by" binding:"required"`
	Metrics  []string `json:"metrics"`
	DateFrom string   `json:"date_from"`
	DateTo   string   `json:"date_to"`
}

type PivotNode struct {
	Key         string      `json:"key"`
	Label       string      `json:"label"`
	Count       int         `json:"count"`
	MetricValue float64     `json:"metric_value"`
	Children    []PivotNode `json:"children,omitempty"`
}

type PivotResponse struct {
	Entity   string      `json:"entity"`
	GroupBy  []string    `json:"group_by"`
	Metrics  []string    `json:"metrics"`
	DateFrom string      `json:"date_from"`
	DateTo   string      `json:"date_to"`
	Nodes    []PivotNode `json:"nodes"`
	Total    PivotNode   `json:"total"`
}

func (h *AnalyticsHandler) RunPivot(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	var req PivotRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Enforce max 3 grouping levels
	groupBy := req.GroupBy
	if len(groupBy) > 3 {
		groupBy = groupBy[:3]
	}
	if len(groupBy) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "at least one group_by field required"})
		return
	}

	// Parse date range
	var start, end time.Time
	if req.DateFrom != "" {
		start, _ = time.Parse("2006-01-02", req.DateFrom)
	}
	if req.DateTo != "" {
		end, _ = time.Parse("2006-01-02", req.DateTo)
		end = time.Date(end.Year(), end.Month(), end.Day(), 23, 59, 59, 0, time.UTC)
	}
	if start.IsZero() {
		start = time.Now().UTC().AddDate(0, 0, -30)
	}
	if end.IsZero() {
		end = time.Now().UTC()
	}

	primaryMetric := "count"
	if len(req.Metrics) > 0 {
		primaryMetric = req.Metrics[0]
	}

	// Collect raw rows from Firestore
	type rawRow struct {
		keys        []string // group key values in order
		count       int
		metricValue float64
	}

	// nested map: level0key -> level1key -> level2key -> {count, metric}
	// Use a flat map with composite key
	flatMap := map[string]*leaf{}

	collection := fmt.Sprintf("tenants/%s/orders", tenantID)
	switch req.Entity {
	case "products":
		collection = fmt.Sprintf("tenants/%s/products", tenantID)
	case "inventory":
		collection = fmt.Sprintf("tenants/%s/inventory", tenantID)
	case "rmas":
		collection = fmt.Sprintf("tenants/%s/rmas", tenantID)
	}

	iter := h.client.Collection(collection).Documents(ctx)
	defer iter.Stop()

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}

		data := doc.Data()

		// Date filter (for orders and rmas)
		if req.Entity == "orders" || req.Entity == "rmas" {
			tsStr, _ := data["created_at"].(string)
			if tsStr == "" {
				if t, ok := data["created_at"].(time.Time); ok {
					tsStr = t.Format(time.RFC3339)
				}
			}
			ts, err := time.Parse(time.RFC3339, tsStr)
			if err != nil {
				ts, _ = time.Parse("2006-01-02", tsStr)
			}
			if !ts.IsZero() && (ts.Before(start) || ts.After(end)) {
				continue
			}
		}

		// Extract group key values
		keys := make([]string, len(groupBy))
		for i, field := range groupBy {
			val := extractField(data, field)
			if val == "" {
				val = "unknown"
			}
			keys[i] = val
		}

		// Build composite key (max 3 levels)
		compositeKey := keys[0]
		if len(keys) > 1 {
			compositeKey += "\x00" + keys[1]
		}
		if len(keys) > 2 {
			compositeKey += "\x00" + keys[2]
		}

		if _, ok := flatMap[compositeKey]; !ok {
			flatMap[compositeKey] = &leaf{}
		}
		flatMap[compositeKey].count++

		// Compute metric value
		switch primaryMetric {
		case "revenue":
			if totals, ok := data["totals"].(map[string]interface{}); ok {
				if gt, ok := totals["grand_total"].(map[string]interface{}); ok {
					if amt, ok := gt["amount"].(float64); ok {
						flatMap[compositeKey].metric += amt
					}
				}
			}
		case "refund_amount":
			if v, ok := data["refund_amount"].(float64); ok {
				flatMap[compositeKey].metric += v
			}
		case "total_on_hand":
			if v, ok := data["total_on_hand"].(int64); ok {
				flatMap[compositeKey].metric += float64(v)
			} else if v, ok := data["quantity"].(int64); ok {
				flatMap[compositeKey].metric += float64(v)
			}
		case "total_available":
			if v, ok := data["total_available"].(int64); ok {
				flatMap[compositeKey].metric += float64(v)
			} else if v, ok := data["available_qty"].(int64); ok {
				flatMap[compositeKey].metric += float64(v)
			}
		default: // count
			flatMap[compositeKey].metric = float64(flatMap[compositeKey].count)
		}
	}

	// Build hierarchical tree from flat map
	// Level structure depends on number of groupBy fields
	numLevels := len(groupBy)

	// level0 -> level1 -> level2
	type l2key struct{ k0, k1, k2 string }
	l2Map := map[l2key]*leaf{}
	for ck, v := range flatMap {
		parts := splitComposite(ck)
		k := l2key{}
		if len(parts) > 0 {
			k.k0 = parts[0]
		}
		if len(parts) > 1 {
			k.k1 = parts[1]
		}
		if len(parts) > 2 {
			k.k2 = parts[2]
		}
		l2Map[k] = v
	}

	// Group by level 0
	l0Map := map[string]map[string]map[string]*leaf{}
	for k, v := range l2Map {
		if _, ok := l0Map[k.k0]; !ok {
			l0Map[k.k0] = map[string]map[string]*leaf{}
		}
		if _, ok := l0Map[k.k0][k.k1]; !ok {
			l0Map[k.k0][k.k1] = map[string]*leaf{}
		}
		l0Map[k.k0][k.k1][k.k2] = v
	}

	nodes := []PivotNode{}
	totalCount := 0
	totalMetric := 0.0

	l0Keys := sortedKeys(l0Map)
	for _, k0 := range l0Keys {
		l1Map := l0Map[k0]
		node0 := PivotNode{Key: k0, Label: k0}

		if numLevels >= 2 {
			l1Keys := sortedStringKeys(l1Map)
			for _, k1 := range l1Keys {
				l2m := l1Map[k1]
				node1 := PivotNode{Key: k1, Label: k1}

				if numLevels >= 3 {
					l2Keys := sortedStringLeafKeys(l2m)
					for _, k2 := range l2Keys {
						v := l2m[k2]
						node2 := PivotNode{
							Key:         k2,
							Label:       k2,
							Count:       v.count,
							MetricValue: math.Round(v.metric*100) / 100,
						}
						node1.Count += v.count
						node1.MetricValue += v.metric
						node1.Children = append(node1.Children, node2)
					}
					node1.MetricValue = math.Round(node1.MetricValue*100) / 100
				} else {
					for _, v := range l2m {
						node1.Count += v.count
						node1.MetricValue += v.metric
					}
					node1.MetricValue = math.Round(node1.MetricValue*100) / 100
				}
				node0.Count += node1.Count
				node0.MetricValue += node1.MetricValue
				node0.Children = append(node0.Children, node1)
			}
			node0.MetricValue = math.Round(node0.MetricValue*100) / 100
		} else {
			for _, l1m := range l1Map {
				for _, v := range l1m {
					node0.Count += v.count
					node0.MetricValue += v.metric
				}
			}
			node0.MetricValue = math.Round(node0.MetricValue*100) / 100
		}

		totalCount += node0.Count
		totalMetric += node0.MetricValue
		nodes = append(nodes, node0)
	}

	c.JSON(http.StatusOK, PivotResponse{
		Entity:   req.Entity,
		GroupBy:  groupBy,
		Metrics:  req.Metrics,
		DateFrom: start.Format("2006-01-02"),
		DateTo:   end.Format("2006-01-02"),
		Nodes:    nodes,
		Total: PivotNode{
			Key:         "total",
			Label:       "Total",
			Count:       totalCount,
			MetricValue: math.Round(totalMetric*100) / 100,
		},
	})
}

// ── Pivot helpers ─────────────────────────────────────────────────────────────

func extractField(data map[string]interface{}, field string) string {
	// Direct field
	if v, ok := data[field]; ok {
		switch val := v.(type) {
		case string:
			return val
		case bool:
			if val {
				return "true"
			}
			return "false"
		case int64:
			return fmt.Sprintf("%d", val)
		case float64:
			return fmt.Sprintf("%.0f", val)
		case time.Time:
			return val.Format("2006-01-02")
		}
	}
	// Nested: shipping_country -> shipping_address.country
	if field == "shipping_country" {
		if sa, ok := data["shipping_address"].(map[string]interface{}); ok {
			if v, ok := sa["country"].(string); ok {
				return v
			}
		}
	}
	if field == "customer_name" {
		if cust, ok := data["customer"].(map[string]interface{}); ok {
			if v, ok := cust["name"].(string); ok {
				return v
			}
		}
	}
	if field == "order_date" {
		if v, ok := data["order_date"].(string); ok && len(v) >= 10 {
			return v[:10]
		}
		if v, ok := data["created_at"].(string); ok && len(v) >= 10 {
			return v[:10]
		}
	}
	return ""
}

func splitComposite(s string) []string {
	result := []string{}
	current := ""
	for _, r := range s {
		if r == '\x00' {
			result = append(result, current)
			current = ""
		} else {
			current += string(r)
		}
	}
	result = append(result, current)
	return result
}

func sortedKeys(m map[string]map[string]map[string]*leaf) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedStringKeys(m map[string]map[string]*leaf) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedStringLeafKeys(m map[string]*leaf) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ============================================================================
// SESSION 8: CHANNEL P&L
// GET /analytics/channel-pnl?period=30d&date_from=&date_to=
// ============================================================================

// Default fee schedules per channel (overridable via tenant settings in future)
var channelFeeSchedule = map[string]float64{
	"ebay":        0.129,
	"amazon":      0.150,
	"backmarket":  0.100,
	"bol":         0.100,
	"zalando":     0.200,
	"lazada":      0.050,
	"temu":        0.080,
	"shopify":     0.020,
	"etsy":        0.065,
	"onbuy":       0.090,
	"tiktok":      0.050,
}

type ChannelPnLItem struct {
	Channel      string  `json:"channel"`
	GrossRevenue float64 `json:"gross_revenue"`
	FeeRate      float64 `json:"fee_rate"`
	EstFees      float64 `json:"est_fees"`
	NetRevenue   float64 `json:"net_revenue"`
	COGS         float64 `json:"cogs"`
	EstMargin    float64 `json:"est_margin"`
	MarginPct    float64 `json:"margin_pct"`
	OrderCount   int     `json:"order_count"`
	Currency     string  `json:"currency"`
}

type ChannelPnLResponse struct {
	DateFrom string           `json:"date_from"`
	DateTo   string           `json:"date_to"`
	Currency string           `json:"currency"`
	Channels []ChannelPnLItem `json:"channels"`
	Totals   ChannelPnLItem   `json:"totals"`
}

func (h *AnalyticsHandler) ChannelPnL(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()
	start, end := parsePeriod(c)

	// Build SKU -> cost_price map from products
	costMap := map[string]float64{}
	prodIter := h.client.Collection(fmt.Sprintf("tenants/%s/products", tenantID)).Documents(ctx)
	defer prodIter.Stop()
	for {
		doc, err := prodIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		data := doc.Data()
		sku, _ := data["sku"].(string)
		if sku == "" {
			continue
		}
		if cp, ok := data["cost_price"].(float64); ok {
			costMap[sku] = cp
		}
	}

	type channelAgg struct {
		grossRevenue float64
		cogs         float64
		orderCount   int
		currency     string
	}
	aggMap := map[string]*channelAgg{}

	iter := h.client.Collection(fmt.Sprintf("tenants/%s/orders", tenantID)).Documents(ctx)
	defer iter.Stop()

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		var order models.Order
		if err := doc.DataTo(&order); err != nil {
			continue
		}
		if !orderInRange(order, start, end) {
			continue
		}
		if order.Status == "cancelled" {
			continue
		}

		ch := order.Channel
		if ch == "" {
			ch = "unknown"
		}
		if _, ok := aggMap[ch]; !ok {
			aggMap[ch] = &channelAgg{}
		}
		agg := aggMap[ch]
		agg.grossRevenue += order.Totals.GrandTotal.Amount
		agg.orderCount++
		if order.Totals.GrandTotal.Currency != "" {
			agg.currency = order.Totals.GrandTotal.Currency
		}

		// Compute COGS from order lines
		raw := doc.Data()
		linesRaw, _ := raw["lines"].([]interface{})
		for _, lr := range linesRaw {
			lm, ok := lr.(map[string]interface{})
			if !ok {
				continue
			}
			sku, _ := lm["sku"].(string)
			qty := 0
			if q, ok := lm["quantity"].(int64); ok {
				qty = int(q)
			} else if q, ok := lm["quantity"].(float64); ok {
				qty = int(q)
			}
			if sku != "" && qty > 0 {
				agg.cogs += costMap[sku] * float64(qty)
			}
		}
	}

	currency := "GBP"
	channels := make([]ChannelPnLItem, 0, len(aggMap))
	var totGross, totFees, totNet, totCOGS float64
	totOrders := 0

	for ch, agg := range aggMap {
		if agg.currency != "" {
			currency = agg.currency
		}
		feeRate := channelFeeSchedule[ch]
		if feeRate == 0 {
			feeRate = 0.10 // default 10%
		}
		estFees := math.Round(agg.grossRevenue*feeRate*100) / 100
		netRevenue := math.Round((agg.grossRevenue-estFees)*100) / 100
		estMargin := math.Round((netRevenue-agg.cogs)*100) / 100
		marginPct := 0.0
		if netRevenue > 0 {
			marginPct = math.Round((estMargin/netRevenue)*10000) / 100
		}

		channels = append(channels, ChannelPnLItem{
			Channel:      ch,
			GrossRevenue: math.Round(agg.grossRevenue*100) / 100,
			FeeRate:      feeRate,
			EstFees:      estFees,
			NetRevenue:   netRevenue,
			COGS:         math.Round(agg.cogs*100) / 100,
			EstMargin:    estMargin,
			MarginPct:    marginPct,
			OrderCount:   agg.orderCount,
			Currency:     currency,
		})

		totGross += agg.grossRevenue
		totFees += estFees
		totNet += netRevenue
		totCOGS += agg.cogs
		totOrders += agg.orderCount
	}

	sort.Slice(channels, func(i, j int) bool {
		return channels[i].GrossRevenue > channels[j].GrossRevenue
	})

	totMargin := math.Round((totNet-totCOGS)*100) / 100
	totMarginPct := 0.0
	if totNet > 0 {
		totMarginPct = math.Round((totMargin/totNet)*10000) / 100
	}

	c.JSON(http.StatusOK, ChannelPnLResponse{
		DateFrom: start.Format("2006-01-02"),
		DateTo:   end.Format("2006-01-02"),
		Currency: currency,
		Channels: channels,
		Totals: ChannelPnLItem{
			Channel:      "total",
			GrossRevenue: math.Round(totGross*100) / 100,
			EstFees:      math.Round(totFees*100) / 100,
			NetRevenue:   math.Round(totNet*100) / 100,
			COGS:         math.Round(totCOGS*100) / 100,
			EstMargin:    totMargin,
			MarginPct:    totMarginPct,
			OrderCount:   totOrders,
			Currency:     currency,
		},
	})
}

// ============================================================================
// SESSION 8: LISTING HEALTH BULK
// GET /analytics/listing-health
// Returns products sorted by health score ascending (worst first)
// ============================================================================

type ListingHealthItem struct {
	ProductID string `json:"product_id"`
	SKU       string `json:"sku"`
	Title     string `json:"title"`
	Score     int    `json:"score"`
	Breakdown map[string]int `json:"breakdown"`
}

type ListingHealthResponse struct {
	Total    int                 `json:"total"`
	Products []ListingHealthItem `json:"products"`
}

func (h *AnalyticsHandler) ListingHealthBulk(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	var items []ListingHealthItem

	iter := h.client.Collection(fmt.Sprintf("tenants/%s/products", tenantID)).Documents(ctx)
	defer iter.Stop()

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		data := doc.Data()

		productID := doc.Ref.ID
		sku, _ := data["sku"].(string)
		title, _ := data["title"].(string)

		score, breakdown := computeHealthScore(data)
		items = append(items, ListingHealthItem{
			ProductID: productID,
			SKU:       sku,
			Title:     title,
			Score:     score,
			Breakdown: breakdown,
		})
	}

	// Sort worst first
	sort.Slice(items, func(i, j int) bool {
		return items[i].Score < items[j].Score
	})

	c.JSON(http.StatusOK, ListingHealthResponse{
		Total:    len(items),
		Products: items,
	})
}

// ============================================================================
// SESSION 8: SINGLE PRODUCT HEALTH SCORE
// GET /products/:id/health-score
// ============================================================================

func (h *AnalyticsHandler) GetProductHealthScore(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	productID := c.Param("id")
	ctx := c.Request.Context()

	doc, err := h.client.Collection(fmt.Sprintf("tenants/%s/products", tenantID)).Doc(productID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "product not found"})
		return
	}

	data := doc.Data()
	score, breakdown := computeHealthScore(data)

	c.JSON(http.StatusOK, gin.H{
		"product_id": productID,
		"score":      score,
		"breakdown":  breakdown,
	})
}

// computeHealthScore calculates a 0-100 score for a product document.
// Criteria:
//   title length > 60 chars  → 25 pts
//   description > 200 chars  → 25 pts
//   images >= 4              → 25 pts
//   has price (> 0)          → 15 pts
//   has barcode              → 10 pts
func computeHealthScore(data map[string]interface{}) (int, map[string]int) {
	breakdown := map[string]int{
		"title":       0,
		"description": 0,
		"images":      0,
		"price":       0,
		"barcode":     0,
	}

	// Title
	title, _ := data["title"].(string)
	if len(title) > 60 {
		breakdown["title"] = 25
	} else if len(title) > 0 {
		breakdown["title"] = int(float64(len(title)) / 60 * 25)
	}

	// Description
	desc, _ := data["description"].(string)
	if len(desc) > 200 {
		breakdown["description"] = 25
	} else if len(desc) > 0 {
		breakdown["description"] = int(float64(len(desc)) / 200 * 25)
	}

	// Images
	imageCount := 0
	if imgs, ok := data["images"].([]interface{}); ok {
		imageCount = len(imgs)
	}
	if imageCount >= 4 {
		breakdown["images"] = 25
	} else if imageCount > 0 {
		breakdown["images"] = imageCount * 6 // ~6 pts per image up to 24
	}

	// Price
	price := 0.0
	if p, ok := data["price"].(float64); ok {
		price = p
	}
	if price > 0 {
		breakdown["price"] = 15
	}

	// Barcode
	barcode, _ := data["barcode"].(string)
	if barcode == "" {
		barcode, _ = data["ean"].(string)
	}
	if barcode != "" {
		breakdown["barcode"] = 10
	}

	total := 0
	for _, v := range breakdown {
		total += v
	}
	if total > 100 {
		total = 100
	}

	return total, breakdown
}

// ============================================================================
// SESSION 8: RECONCILIATION HEALTH
// GET /analytics/reconciliation-health
// Returns per-channel match rate for last 12 reconciliation runs
// ============================================================================

type ReconcileRunSummary struct {
	JobID       string  `json:"job_id"`
	Channel     string  `json:"channel"`
	CreatedAt   string  `json:"created_at"`
	Total       int     `json:"total"`
	Matched     int     `json:"matched"`
	MatchRate   float64 `json:"match_rate"`
	PushTotal   int     `json:"push_total"`
	PushSuccess int     `json:"push_succeeded"`
}

type ReconciliationHealthResponse struct {
	Channels map[string][]ReconcileRunSummary `json:"channels"`
}

func (h *AnalyticsHandler) ReconciliationHealth(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	result := map[string][]ReconcileRunSummary{}

	// Iterate reconcile_jobs top-level docs (one per credential)
	jobsIter := h.client.Collection(fmt.Sprintf("tenants/%s/reconcile_jobs", tenantID)).Documents(ctx)
	defer jobsIter.Stop()

	for {
		jobDoc, err := jobsIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		credentialID := jobDoc.Ref.ID

		// Get last 12 runs subcollection
		runsIter := jobDoc.Ref.Collection("runs").
			OrderBy("created_at", firestore.Desc).
			Limit(12).
			Documents(ctx)

		for {
			runDoc, err := runsIter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				break
			}
			data := runDoc.Data()

			channel, _ := data["channel"].(string)
			if channel == "" {
				channel = credentialID
			}

			total := 0
			matched := 0
			if v, ok := data["total"].(int64); ok {
				total = int(v)
			}
			if v, ok := data["matched"].(int64); ok {
				matched = int(v)
			}
			pushTotal := 0
			pushSucceeded := 0
			if v, ok := data["push_total"].(int64); ok {
				pushTotal = int(v)
			}
			if v, ok := data["push_succeeded"].(int64); ok {
				pushSucceeded = int(v)
			}

			matchRate := 0.0
			if total > 0 {
				matchRate = math.Round(float64(matched)/float64(total)*10000) / 100
			}

			createdAt := ""
			if v, ok := data["created_at"].(string); ok {
				createdAt = v
			}

			summary := ReconcileRunSummary{
				JobID:       runDoc.Ref.ID,
				Channel:     channel,
				CreatedAt:   createdAt,
				Total:       total,
				Matched:     matched,
				MatchRate:   matchRate,
				PushTotal:   pushTotal,
				PushSuccess: pushSucceeded,
			}

			result[channel] = append(result[channel], summary)
		}
		runsIter.Stop()
	}

	c.JSON(http.StatusOK, ReconciliationHealthResponse{Channels: result})
}

// ============================================================================
// SESSION 4: ORDER REPORTING
// ============================================================================

// ── GET /analytics/reports/orders-by-channel ────────────────────────────────
// Volume, revenue, and avg order value per channel for a period.

type OrdersByChannelItem struct {
	Channel       string  `json:"channel"`
	OrderCount    int     `json:"order_count"`
	Revenue       float64 `json:"revenue"`
	AvgOrderValue float64 `json:"avg_order_value"`
	Currency      string  `json:"currency"`
}

type OrdersByChannelResponse struct {
	DateFrom string                `json:"date_from"`
	DateTo   string                `json:"date_to"`
	Currency string                `json:"currency"`
	Channels []OrdersByChannelItem `json:"channels"`
}

func (h *AnalyticsHandler) GetOrdersByChannel(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	start, end := parsePeriod(c)

	type agg struct {
		count    int
		revenue  float64
		currency string
	}
	m := map[string]*agg{}
	currency := "GBP"

	iter := h.client.Collection(fmt.Sprintf("tenants/%s/orders", tenantID)).Documents(c.Request.Context())
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		var order models.Order
		if err := doc.DataTo(&order); err != nil {
			continue
		}
		if !orderInRange(order, start, end) || order.Status == "cancelled" {
			continue
		}
		ch := order.Channel
		if ch == "" {
			ch = "unknown"
		}
		if _, ok := m[ch]; !ok {
			m[ch] = &agg{}
		}
		m[ch].count++
		m[ch].revenue += order.Totals.GrandTotal.Amount
		if order.Totals.GrandTotal.Currency != "" {
			m[ch].currency = order.Totals.GrandTotal.Currency
			currency = order.Totals.GrandTotal.Currency
		}
	}

	items := make([]OrdersByChannelItem, 0, len(m))
	for ch, a := range m {
		cur := a.currency
		if cur == "" {
			cur = currency
		}
		aov := 0.0
		if a.count > 0 {
			aov = math.Round(a.revenue/float64(a.count)*100) / 100
		}
		items = append(items, OrdersByChannelItem{
			Channel:       ch,
			OrderCount:    a.count,
			Revenue:       math.Round(a.revenue*100) / 100,
			AvgOrderValue: aov,
			Currency:      cur,
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Revenue > items[j].Revenue })
	c.JSON(http.StatusOK, OrdersByChannelResponse{
		DateFrom: start.Format("2006-01-02"),
		DateTo:   end.Format("2006-01-02"),
		Currency: currency,
		Channels: items,
	})
}

// ── GET /analytics/reports/orders-by-date ───────────────────────────────────
// Daily/weekly/monthly order volume + revenue with trend series.

type OrdersByDatePoint struct {
	Period  string  `json:"period"`
	Orders  int     `json:"orders"`
	Revenue float64 `json:"revenue"`
}

type OrdersByDateResponse struct {
	DateFrom    string              `json:"date_from"`
	DateTo      string              `json:"date_to"`
	Granularity string              `json:"granularity"`
	Currency    string              `json:"currency"`
	Points      []OrdersByDatePoint `json:"points"`
}

func (h *AnalyticsHandler) GetOrdersByDate(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	start, end := parsePeriod(c)
	granularity := c.DefaultQuery("granularity", "daily") // daily | weekly | monthly

	type bucket struct {
		orders  int
		revenue float64
	}
	m := map[string]*bucket{}
	currency := "GBP"

	iter := h.client.Collection(fmt.Sprintf("tenants/%s/orders", tenantID)).Documents(c.Request.Context())
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		var order models.Order
		if err := doc.DataTo(&order); err != nil {
			continue
		}
		if !orderInRange(order, start, end) || order.Status == "cancelled" {
			continue
		}
		if order.Totals.GrandTotal.Currency != "" {
			currency = order.Totals.GrandTotal.Currency
		}

		ts, _ := time.Parse(time.RFC3339, order.CreatedAt)
		if ts.IsZero() {
			ts, _ = time.Parse("2006-01-02", order.CreatedAt)
		}
		var key string
		switch granularity {
		case "weekly":
			year, week := ts.ISOWeek()
			key = fmt.Sprintf("%d-W%02d", year, week)
		case "monthly":
			key = ts.Format("2006-01")
		default:
			key = ts.Format("2006-01-02")
		}
		if _, ok := m[key]; !ok {
			m[key] = &bucket{}
		}
		m[key].orders++
		m[key].revenue += order.Totals.GrandTotal.Amount
	}

	points := make([]OrdersByDatePoint, 0, len(m))
	for k, b := range m {
		points = append(points, OrdersByDatePoint{Period: k, Orders: b.orders, Revenue: math.Round(b.revenue*100) / 100})
	}
	sort.Slice(points, func(i, j int) bool { return points[i].Period < points[j].Period })

	c.JSON(http.StatusOK, OrdersByDateResponse{
		DateFrom:    start.Format("2006-01-02"),
		DateTo:      end.Format("2006-01-02"),
		Granularity: granularity,
		Currency:    currency,
		Points:      points,
	})
}

// ── GET /analytics/reports/orders-by-product ────────────────────────────────
// Top sellers, slow movers, revenue per SKU.

type OrdersByProductItem struct {
	SKU        string  `json:"sku"`
	Title      string  `json:"title"`
	Units      int     `json:"units"`
	Revenue    float64 `json:"revenue"`
	OrderCount int     `json:"order_count"`
	Currency   string  `json:"currency"`
}

type OrdersByProductResponse struct {
	DateFrom   string                `json:"date_from"`
	DateTo     string                `json:"date_to"`
	Currency   string                `json:"currency"`
	TopSellers []OrdersByProductItem `json:"top_sellers"`
	SlowMovers []OrdersByProductItem `json:"slow_movers"`
}

func (h *AnalyticsHandler) GetOrdersByProduct(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	start, end := parsePeriod(c)

	type agg struct {
		title    string
		units    int
		revenue  float64
		orders   map[string]bool
		currency string
	}
	skuMap := map[string]*agg{}
	currency := "GBP"

	iter := h.client.Collection(fmt.Sprintf("tenants/%s/orders", tenantID)).Documents(c.Request.Context())
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		var order models.Order
		if err := doc.DataTo(&order); err != nil {
			continue
		}
		if !orderInRange(order, start, end) || order.Status == "cancelled" {
			continue
		}
		if order.Totals.GrandTotal.Currency != "" {
			currency = order.Totals.GrandTotal.Currency
		}
		raw := doc.Data()
		linesRaw, _ := raw["lines"].([]interface{})
		for _, lr := range linesRaw {
			lm, ok := lr.(map[string]interface{})
			if !ok {
				continue
			}
			sku, _ := lm["sku"].(string)
			if sku == "" {
				continue
			}
			title, _ := lm["title"].(string)
			qty := 0
			if q, ok := lm["quantity"].(int64); ok {
				qty = int(q)
			} else if q, ok := lm["quantity"].(float64); ok {
				qty = int(q)
			}
			lineRev := 0.0
			if lt, ok := lm["line_total"].(map[string]interface{}); ok {
				if a, ok := lt["amount"].(float64); ok {
					lineRev = a
				}
			}
			if _, ok := skuMap[sku]; !ok {
				skuMap[sku] = &agg{title: title, orders: map[string]bool{}}
			}
			skuMap[sku].units += qty
			skuMap[sku].revenue += lineRev
			skuMap[sku].orders[order.OrderID] = true
			if title != "" && skuMap[sku].title == "" {
				skuMap[sku].title = title
			}
		}
	}

	items := make([]OrdersByProductItem, 0, len(skuMap))
	for sku, a := range skuMap {
		items = append(items, OrdersByProductItem{
			SKU:        sku,
			Title:      a.title,
			Units:      a.units,
			Revenue:    math.Round(a.revenue*100) / 100,
			OrderCount: len(a.orders),
			Currency:   currency,
		})
	}

	topSellers := make([]OrdersByProductItem, len(items))
	copy(topSellers, items)
	sort.Slice(topSellers, func(i, j int) bool { return topSellers[i].Units > topSellers[j].Units })
	if len(topSellers) > 20 {
		topSellers = topSellers[:20]
	}

	slowMovers := make([]OrdersByProductItem, len(items))
	copy(slowMovers, items)
	sort.Slice(slowMovers, func(i, j int) bool { return slowMovers[i].Units < slowMovers[j].Units })
	if len(slowMovers) > 20 {
		slowMovers = slowMovers[:20]
	}

	c.JSON(http.StatusOK, OrdersByProductResponse{
		DateFrom:   start.Format("2006-01-02"),
		DateTo:     end.Format("2006-01-02"),
		Currency:   currency,
		TopSellers: topSellers,
		SlowMovers: slowMovers,
	})
}

// ── GET /analytics/reports/despatch-performance ─────────────────────────────
// % dispatched on time, breakdown by SLA band.

type DespatchPerformanceResponse struct {
	DateFrom           string  `json:"date_from"`
	DateTo             string  `json:"date_to"`
	TotalDispatched    int     `json:"total_dispatched"`
	OnTime             int     `json:"on_time"`
	OnTimePercent      float64 `json:"on_time_percent"`
	Overdue            int     `json:"overdue"`
	OverduePercent     float64 `json:"overdue_percent"`
	DueToday           int     `json:"due_today"`
	DueTomorrow        int     `json:"due_tomorrow"`
	OnTrack            int     `json:"on_track"`
	NoSLA              int     `json:"no_sla"`
}

func (h *AnalyticsHandler) GetDespatchPerformance(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	start, end := parsePeriod(c)
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 0, time.UTC)
	tomorrow := today.AddDate(0, 0, 1)

	resp := DespatchPerformanceResponse{
		DateFrom: start.Format("2006-01-02"),
		DateTo:   end.Format("2006-01-02"),
	}

	iter := h.client.Collection(fmt.Sprintf("tenants/%s/orders", tenantID)).Documents(c.Request.Context())
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		var order models.Order
		if err := doc.DataTo(&order); err != nil {
			continue
		}
		if !orderInRange(order, start, end) {
			continue
		}

		if order.Status == "fulfilled" || order.Status == "dispatched" {
			resp.TotalDispatched++
			// Check if dispatched on time via SLAAtRisk flag (false = was on time)
			if !order.SLAAtRisk {
				resp.OnTime++
			} else {
				resp.Overdue++
			}
			continue
		}

		// For open orders, classify by SLA band
		despatchBy := order.DespatchByDate
		if despatchBy == "" {
			despatchBy = order.PromisedShipBy
		}
		if despatchBy == "" {
			resp.NoSLA++
			continue
		}
		dbt, err := time.Parse("2006-01-02", despatchBy)
		if err != nil {
			dbt, err = time.Parse(time.RFC3339, despatchBy)
			if err != nil {
				resp.NoSLA++
				continue
			}
		}
		dbt = time.Date(dbt.Year(), dbt.Month(), dbt.Day(), 23, 59, 59, 0, time.UTC)
		switch {
		case dbt.Before(today):
			resp.Overdue++
		case dbt.Before(tomorrow) || dbt.Equal(today):
			resp.DueToday++
		case dbt.Before(tomorrow.AddDate(0, 0, 1)):
			resp.DueTomorrow++
		default:
			resp.OnTrack++
		}
	}

	if resp.TotalDispatched > 0 {
		resp.OnTimePercent = math.Round(float64(resp.OnTime)/float64(resp.TotalDispatched)*10000) / 100
		resp.OverduePercent = math.Round(float64(resp.Overdue)/float64(resp.TotalDispatched)*10000) / 100
	}

	c.JSON(http.StatusOK, resp)
}

// ============================================================================
// SESSION 4: RETURNS & RMA ANALYTICS
// ============================================================================

// ── GET /analytics/reports/returns-analytics ────────────────────────────────
// Return rate by channel, product, reason code; refund value; resolution time; heatmap.

type ReturnsReportItem struct {
	Key        string  `json:"key"`
	Count      int     `json:"count"`
	Rate       float64 `json:"rate,omitempty"`
	TotalUnits int     `json:"total_units,omitempty"`
}

type ReturnsReportResponse struct {
	DateFrom            string              `json:"date_from"`
	DateTo              string              `json:"date_to"`
	TotalRMAs           int                 `json:"total_rmas"`
	TotalRefundValue    float64             `json:"total_refund_value"`
	Currency            string              `json:"currency"`
	AvgResolutionDays   float64             `json:"avg_resolution_days"`
	ByChannel           []ReturnsReportItem `json:"by_channel"`
	ByProduct           []ReturnsReportItem `json:"by_product"`
	ByReasonCode        []ReturnsReportItem `json:"by_reason_code"`
	ReasonCodeHeatmap   []ReasonHeatmapCell `json:"reason_code_heatmap"`
}

type ReasonHeatmapCell struct {
	Channel    string `json:"channel"`
	ReasonCode string `json:"reason_code"`
	Count      int    `json:"count"`
}

func (h *AnalyticsHandler) GetReturnsReport(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	start, end := parsePeriod(c)

	channelMap := map[string]int{}
	productMap := map[string]int{}
	reasonMap := map[string]int{}
	heatmap := map[string]map[string]int{} // channel -> reason -> count

	totalRefund := 0.0
	currency := "GBP"
	totalRMAs := 0
	resolutionDays := []float64{}

	iter := h.client.Collection(fmt.Sprintf("tenants/%s/rmas", tenantID)).Documents(c.Request.Context())
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		var rma models.RMA
		if err := doc.DataTo(&rma); err != nil {
			continue
		}
		if rma.CreatedAt.Before(start) || rma.CreatedAt.After(end) {
			continue
		}
		totalRMAs++
		ch := rma.Channel
		if ch == "" {
			ch = "unknown"
		}
		channelMap[ch]++
		totalRefund += rma.RefundAmount
		if rma.RefundCurrency != "" {
			currency = rma.RefundCurrency
		}
		if rma.ResolvedAt != nil {
			days := rma.ResolvedAt.Sub(rma.CreatedAt).Hours() / 24
			resolutionDays = append(resolutionDays, days)
		}
		if _, ok := heatmap[ch]; !ok {
			heatmap[ch] = map[string]int{}
		}
		for _, line := range rma.Lines {
			rc := line.ReasonCode
			if rc == "" {
				rc = "other"
			}
			reasonMap[rc]++
			heatmap[ch][rc]++
			sku := line.SKU
			if sku != "" {
				productMap[sku]++
			}
		}
	}

	avgRes := 0.0
	if len(resolutionDays) > 0 {
		sum := 0.0
		for _, d := range resolutionDays {
			sum += d
		}
		avgRes = math.Round(sum/float64(len(resolutionDays))*10) / 10
	}

	toItems := func(m map[string]int) []ReturnsReportItem {
		items := make([]ReturnsReportItem, 0, len(m))
		for k, v := range m {
			items = append(items, ReturnsReportItem{Key: k, Count: v})
		}
		sort.Slice(items, func(i, j int) bool { return items[i].Count > items[j].Count })
		return items
	}

	cells := []ReasonHeatmapCell{}
	for ch, reasons := range heatmap {
		for rc, cnt := range reasons {
			cells = append(cells, ReasonHeatmapCell{Channel: ch, ReasonCode: rc, Count: cnt})
		}
	}
	sort.Slice(cells, func(i, j int) bool { return cells[i].Count > cells[j].Count })

	c.JSON(http.StatusOK, ReturnsReportResponse{
		DateFrom:          start.Format("2006-01-02"),
		DateTo:            end.Format("2006-01-02"),
		TotalRMAs:         totalRMAs,
		TotalRefundValue:  math.Round(totalRefund*100) / 100,
		Currency:          currency,
		AvgResolutionDays: avgRes,
		ByChannel:         toItems(channelMap),
		ByProduct:         toItems(productMap),
		ByReasonCode:      toItems(reasonMap),
		ReasonCodeHeatmap: cells,
	})
}

// ============================================================================
// SESSION 4: FINANCIAL REPORTING
// ============================================================================

// ── GET /analytics/reports/financial ────────────────────────────────────────
// VAT by rate band, gross margin estimate, shipping cost report.

type VATBandItem struct {
	RateLabel    string  `json:"rate_label"`
	TaxRate      float64 `json:"tax_rate"`
	OrderCount   int     `json:"order_count"`
	NetRevenue   float64 `json:"net_revenue"`
	OutputVAT    float64 `json:"output_vat"`
	Currency     string  `json:"currency"`
}

type FinancialReportResponse struct {
	DateFrom           string        `json:"date_from"`
	DateTo             string        `json:"date_to"`
	Currency           string        `json:"currency"`
	TotalRevenue       float64       `json:"total_revenue"`
	TotalCOGS          float64       `json:"total_cogs"`
	GrossMargin        float64       `json:"gross_margin"`
	GrossMarginPct     float64       `json:"gross_margin_pct"`
	TotalOutputVAT     float64       `json:"total_output_vat"`
	TotalShippingCost  float64       `json:"total_shipping_cost"`   // carrier spend (from shipments)
	TotalShippingCharged float64     `json:"total_shipping_charged"` // charged to customers
	VATBands           []VATBandItem `json:"vat_bands"`
}

func (h *AnalyticsHandler) GetFinancialReport(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()
	start, end := parsePeriod(c)

	// Build SKU -> cost_price map
	costMap := map[string]float64{}
	prodIter := h.client.Collection(fmt.Sprintf("tenants/%s/products", tenantID)).Documents(ctx)
	defer prodIter.Stop()
	for {
		doc, err := prodIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		data := doc.Data()
		sku, _ := data["sku"].(string)
		if sku == "" {
			continue
		}
		if cp, ok := data["cost_price"].(float64); ok {
			costMap[sku] = cp
		}
	}

	// Buckets by tax rate
	type vatBucket struct {
		orders   int
		net      float64
		vat      float64
		currency string
	}
	vatMap := map[float64]*vatBucket{}
	totalRev := 0.0
	totalCOGS := 0.0
	totalVAT := 0.0
	totalShippingCharged := 0.0
	currency := "GBP"

	iter := h.client.Collection(fmt.Sprintf("tenants/%s/orders", tenantID)).Documents(ctx)
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		var order models.Order
		if err := doc.DataTo(&order); err != nil {
			continue
		}
		if !orderInRange(order, start, end) || order.Status == "cancelled" {
			continue
		}
		if order.Totals.GrandTotal.Currency != "" {
			currency = order.Totals.GrandTotal.Currency
		}
		totalRev += order.Totals.GrandTotal.Amount
		totalShippingCharged += order.Totals.Shipping.Amount
		totalVAT += order.Totals.Tax.Amount

		// Bucket by tax rate from lines
		raw := doc.Data()
		linesRaw, _ := raw["lines"].([]interface{})
		lineTaxRate := 0.0
		for _, lr := range linesRaw {
			lm, ok := lr.(map[string]interface{})
			if !ok {
				continue
			}
			if tr, ok := lm["tax_rate"].(float64); ok && tr > 0 {
				lineTaxRate = tr
			}
			sku, _ := lm["sku"].(string)
			qty := 0
			if q, ok := lm["quantity"].(int64); ok {
				qty = int(q)
			} else if q, ok := lm["quantity"].(float64); ok {
				qty = int(q)
			}
			if sku != "" && qty > 0 {
				totalCOGS += costMap[sku] * float64(qty)
			}
		}

		// Use order-level tax_rate if no line-level — derive from totals if needed
		taxRate := lineTaxRate
		if taxRate == 0 && order.Totals.GrandTotal.Amount > 0 {
			taxRate = math.Round(order.Totals.Tax.Amount/(order.Totals.GrandTotal.Amount-order.Totals.Tax.Amount)*100) / 100
		}
		vatRate := math.Round(taxRate*100) / 100
		if _, ok := vatMap[vatRate]; !ok {
			vatMap[vatRate] = &vatBucket{currency: currency}
		}
		vatMap[vatRate].orders++
		vatMap[vatRate].net += order.Totals.GrandTotal.Amount - order.Totals.Tax.Amount
		vatMap[vatRate].vat += order.Totals.Tax.Amount
	}

	// Shipping cost from shipments collection
	totalShippingCost := 0.0
	shipIter := h.client.Collection(fmt.Sprintf("tenants/%s/shipments", tenantID)).Documents(ctx)
	defer shipIter.Stop()
	for {
		doc, err := shipIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		data := doc.Data()
		// Check if shipment is in period via created_at
		var shipTime time.Time
		if ts, ok := data["created_at"].(string); ok {
			shipTime, _ = time.Parse(time.RFC3339, ts)
		}
		if !shipTime.IsZero() && (shipTime.Before(start) || shipTime.After(end)) {
			continue
		}
		if cost, ok := data["total_price"].(float64); ok {
			totalShippingCost += cost
		} else if cost, ok := data["carrier_cost"].(float64); ok {
			totalShippingCost += cost
		}
	}

	grossMargin := totalRev - totalCOGS
	grossMarginPct := 0.0
	if totalRev > 0 {
		grossMarginPct = math.Round(grossMargin/totalRev*10000) / 100
	}

	bands := make([]VATBandItem, 0, len(vatMap))
	for rate, b := range vatMap {
		label := "Exempt/Zero"
		if rate >= 0.20 {
			label = "Standard (20%)"
		} else if rate > 0 {
			label = fmt.Sprintf("Reduced (%.0f%%)", rate*100)
		}
		bands = append(bands, VATBandItem{
			RateLabel:  label,
			TaxRate:    rate,
			OrderCount: b.orders,
			NetRevenue: math.Round(b.net*100) / 100,
			OutputVAT:  math.Round(b.vat*100) / 100,
			Currency:   currency,
		})
	}
	sort.Slice(bands, func(i, j int) bool { return bands[i].TaxRate > bands[j].TaxRate })

	c.JSON(http.StatusOK, FinancialReportResponse{
		DateFrom:             start.Format("2006-01-02"),
		DateTo:               end.Format("2006-01-02"),
		Currency:             currency,
		TotalRevenue:         math.Round(totalRev*100) / 100,
		TotalCOGS:            math.Round(totalCOGS*100) / 100,
		GrossMargin:          math.Round(grossMargin*100) / 100,
		GrossMarginPct:       grossMarginPct,
		TotalOutputVAT:       math.Round(totalVAT*100) / 100,
		TotalShippingCost:    math.Round(totalShippingCost*100) / 100,
		TotalShippingCharged: math.Round(totalShippingCharged*100) / 100,
		VATBands:             bands,
	})
}

// ============================================================================
// SESSION 4: OPERATIONAL DASHBOARDS
// ============================================================================

// ── GET /analytics/operational ───────────────────────────────────────────────
// Live order pipeline + SLA health + channel health.

type OrderPipelineStage struct {
	Status string `json:"status"`
	Count  int    `json:"count"`
}

type SLABandCount struct {
	Band  string `json:"band"`
	Count int    `json:"count"`
}

type ChannelHealthItem struct {
	Channel          string `json:"channel"`
	TotalOrders      int    `json:"total_orders"`
	ErrorOrders      int    `json:"error_orders"`
	ErrorRate        float64 `json:"error_rate"`
	LastOrderAt      string `json:"last_order_at"`
}

type OperationalDashboardResponse struct {
	OrderPipeline []OrderPipelineStage `json:"order_pipeline"`
	SLAHealth     []SLABandCount       `json:"sla_health"`
	ChannelHealth []ChannelHealthItem  `json:"channel_health"`
}

func (h *AnalyticsHandler) GetOperationalDashboard(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 0, time.UTC)
	tomorrow := today.AddDate(0, 0, 1)
	dayAfter := tomorrow.AddDate(0, 0, 1)

	statusOrder := []string{"imported", "processing", "on_hold", "ready_to_fulfil", "parked"}
	pipeline := map[string]int{}
	slaBands := map[string]int{
		"overdue":      0,
		"due_today":    0,
		"due_tomorrow": 0,
		"on_track":     0,
		"no_sla":       0,
	}

	type chData struct {
		total   int
		errors  int
		lastAt  string
	}
	channelMap := map[string]*chData{}

	iter := h.client.Collection(fmt.Sprintf("tenants/%s/orders", tenantID)).Documents(ctx)
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		var order models.Order
		if err := doc.DataTo(&order); err != nil {
			continue
		}

		openStatuses := map[string]bool{
			"imported": true, "processing": true, "on_hold": true,
			"ready_to_fulfil": true, "parked": true,
		}

		if openStatuses[order.Status] {
			pipeline[order.Status]++
		}

		// SLA band for open non-cancelled orders
		if order.Status != "fulfilled" && order.Status != "dispatched" && order.Status != "cancelled" {
			despatchBy := order.DespatchByDate
			if despatchBy == "" {
				despatchBy = order.PromisedShipBy
			}
			if despatchBy == "" {
				slaBands["no_sla"]++
			} else {
				dbt, err := time.Parse("2006-01-02", despatchBy)
				if err != nil {
					dbt, _ = time.Parse(time.RFC3339, despatchBy)
				}
				dbt = time.Date(dbt.Year(), dbt.Month(), dbt.Day(), 23, 59, 59, 0, time.UTC)
				switch {
				case dbt.Before(today):
					slaBands["overdue"]++
				case dbt.Before(tomorrow) || dbt.Equal(today):
					slaBands["due_today"]++
				case dbt.Before(dayAfter):
					slaBands["due_tomorrow"]++
				default:
					slaBands["on_track"]++
				}
			}
		}

		// Channel health
		ch := order.Channel
		if ch == "" {
			ch = "unknown"
		}
		if _, ok := channelMap[ch]; !ok {
			channelMap[ch] = &chData{}
		}
		channelMap[ch].total++
		if order.Status == "on_hold" || order.SubStatus == "error" {
			channelMap[ch].errors++
		}
		if order.CreatedAt > channelMap[ch].lastAt {
			channelMap[ch].lastAt = order.CreatedAt
		}
	}

	pipelineResp := make([]OrderPipelineStage, 0, len(statusOrder))
	for _, s := range statusOrder {
		pipelineResp = append(pipelineResp, OrderPipelineStage{Status: s, Count: pipeline[s]})
	}

	slaResp := []SLABandCount{
		{Band: "overdue", Count: slaBands["overdue"]},
		{Band: "due_today", Count: slaBands["due_today"]},
		{Band: "due_tomorrow", Count: slaBands["due_tomorrow"]},
		{Band: "on_track", Count: slaBands["on_track"]},
		{Band: "no_sla", Count: slaBands["no_sla"]},
	}

	channelResp := make([]ChannelHealthItem, 0, len(channelMap))
	for ch, d := range channelMap {
		errRate := 0.0
		if d.total > 0 {
			errRate = math.Round(float64(d.errors)/float64(d.total)*10000) / 100
		}
		channelResp = append(channelResp, ChannelHealthItem{
			Channel:     ch,
			TotalOrders: d.total,
			ErrorOrders: d.errors,
			ErrorRate:   errRate,
			LastOrderAt: d.lastAt,
		})
	}
	sort.Slice(channelResp, func(i, j int) bool { return channelResp[i].TotalOrders > channelResp[j].TotalOrders })

	c.JSON(http.StatusOK, OperationalDashboardResponse{
		OrderPipeline: pipelineResp,
		SLAHealth:     slaResp,
		ChannelHealth: channelResp,
	})
}

// ── GET /analytics/reports/export ────────────────────────────────────────────
// CSV export for any report type.
// Query params: report=orders-by-channel|orders-by-date|orders-by-product|financial|returns
// Same period params as other reports.

// csvEscape wraps a string in quotes and escapes internal quotes for CSV.
func csvEscape(s string) string {
	return "\"" + strings.ReplaceAll(s, "\"", "\"\"") + "\""
}

func (h *AnalyticsHandler) ExportReportCSV(c *gin.Context) {
	report := c.DefaultQuery("report", "orders-by-channel")
	tenantID := c.GetString("tenant_id")
	start, end := parsePeriod(c)
	ctx := c.Request.Context()

	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"marketmate-%s-%s.csv\"", report, start.Format("20060102")))
	// BOM for Excel UTF-8
	c.Writer.Write([]byte("\xef\xbb\xbf"))

	writeRow := func(fields ...string) {
		escaped := make([]string, len(fields))
		for i, f := range fields {
			escaped[i] = csvEscape(f)
		}
		c.String(200, strings.Join(escaped, ",")+"\n")
	}

	switch report {

	// ── Orders by Channel ─────────────────────────────────────────────────────
	case "orders-by-channel":
		writeRow("Channel", "Order Count", "Revenue", "Avg Order Value", "Currency")
		type agg struct{ count int; revenue float64; currency string }
		m := map[string]*agg{}
		iter := h.client.Collection(fmt.Sprintf("tenants/%s/orders", tenantID)).Documents(ctx)
		defer iter.Stop()
		for {
			doc, err := iter.Next()
			if err == iterator.Done { break }
			if err != nil { break }
			var order models.Order
			if err := doc.DataTo(&order); err != nil { continue }
			if !orderInRange(order, start, end) || order.Status == "cancelled" { continue }
			ch := order.Channel; if ch == "" { ch = "unknown" }
			if _, ok := m[ch]; !ok { m[ch] = &agg{} }
			m[ch].count++
			m[ch].revenue += order.Totals.GrandTotal.Amount
			if order.Totals.GrandTotal.Currency != "" { m[ch].currency = order.Totals.GrandTotal.Currency }
		}
		keys := make([]string, 0, len(m))
		for k := range m { keys = append(keys, k) }
		sort.Strings(keys)
		for _, ch := range keys {
			a := m[ch]
			aov := 0.0; if a.count > 0 { aov = a.revenue / float64(a.count) }
			writeRow(ch, fmt.Sprintf("%d", a.count), fmt.Sprintf("%.2f", a.revenue), fmt.Sprintf("%.2f", aov), a.currency)
		}

	// ── Orders by Date ────────────────────────────────────────────────────────
	case "orders-by-date":
		granularity := c.DefaultQuery("granularity", "daily")
		writeRow("Period", "Orders", "Revenue", "Currency")
		type bucket struct{ orders int; revenue float64 }
		m := map[string]*bucket{}
		currency := "GBP"
		iter := h.client.Collection(fmt.Sprintf("tenants/%s/orders", tenantID)).Documents(ctx)
		defer iter.Stop()
		for {
			doc, err := iter.Next()
			if err == iterator.Done { break }
			if err != nil { break }
			var order models.Order
			if err := doc.DataTo(&order); err != nil { continue }
			if !orderInRange(order, start, end) || order.Status == "cancelled" { continue }
			if order.Totals.GrandTotal.Currency != "" { currency = order.Totals.GrandTotal.Currency }
			ts, _ := time.Parse(time.RFC3339, order.CreatedAt)
			if ts.IsZero() { ts, _ = time.Parse("2006-01-02", order.CreatedAt) }
			var key string
			switch granularity {
			case "weekly":
				yr, wk := ts.ISOWeek(); key = fmt.Sprintf("%d-W%02d", yr, wk)
			case "monthly":
				key = ts.Format("2006-01")
			default:
				key = ts.Format("2006-01-02")
			}
			if _, ok := m[key]; !ok { m[key] = &bucket{} }
			m[key].orders++; m[key].revenue += order.Totals.GrandTotal.Amount
		}
		keys := make([]string, 0, len(m))
		for k := range m { keys = append(keys, k) }
		sort.Strings(keys)
		for _, k := range keys {
			b := m[k]
			writeRow(k, fmt.Sprintf("%d", b.orders), fmt.Sprintf("%.2f", b.revenue), currency)
		}

	// ── Orders by Product ─────────────────────────────────────────────────────
	case "orders-by-product":
		writeRow("SKU", "Title", "Units Sold", "Revenue", "Order Count", "Currency")
		type agg struct{ title string; units int; revenue float64; orders map[string]bool }
		skuMap := map[string]*agg{}
		currency := "GBP"
		iter := h.client.Collection(fmt.Sprintf("tenants/%s/orders", tenantID)).Documents(ctx)
		defer iter.Stop()
		for {
			doc, err := iter.Next()
			if err == iterator.Done { break }
			if err != nil { break }
			var order models.Order
			if err := doc.DataTo(&order); err != nil { continue }
			if !orderInRange(order, start, end) || order.Status == "cancelled" { continue }
			if order.Totals.GrandTotal.Currency != "" { currency = order.Totals.GrandTotal.Currency }
			raw := doc.Data()
			linesRaw, _ := raw["lines"].([]interface{})
			for _, lr := range linesRaw {
				lm, ok := lr.(map[string]interface{}); if !ok { continue }
				sku, _ := lm["sku"].(string); if sku == "" { continue }
				title, _ := lm["title"].(string)
				qty := 0
				if q, ok := lm["quantity"].(int64); ok { qty = int(q) } else if q, ok := lm["quantity"].(float64); ok { qty = int(q) }
				lineRev := 0.0
				if lt, ok := lm["line_total"].(map[string]interface{}); ok { if a, ok := lt["amount"].(float64); ok { lineRev = a } }
				if _, ok := skuMap[sku]; !ok { skuMap[sku] = &agg{title: title, orders: map[string]bool{}} }
				skuMap[sku].units += qty; skuMap[sku].revenue += lineRev
				skuMap[sku].orders[order.OrderID] = true
				if title != "" && skuMap[sku].title == "" { skuMap[sku].title = title }
			}
		}
		type row struct{ sku string; a *agg }
		rows := make([]row, 0, len(skuMap))
		for sku, a := range skuMap { rows = append(rows, row{sku, a}) }
		sort.Slice(rows, func(i, j int) bool { return rows[i].a.units > rows[j].a.units })
		for _, r := range rows {
			writeRow(r.sku, r.a.title, fmt.Sprintf("%d", r.a.units), fmt.Sprintf("%.2f", r.a.revenue), fmt.Sprintf("%d", len(r.a.orders)), currency)
		}

	// ── Despatch Performance ──────────────────────────────────────────────────
	case "despatch-performance":
		writeRow("Metric", "Value")
		now := time.Now().UTC()
		today := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 0, time.UTC)
		tomorrow := today.AddDate(0, 0, 1)
		dayAfter := tomorrow.AddDate(0, 0, 1)
		totDispatched, onTime, overdue, dueToday, dueTomorrow, onTrack, noSLA := 0, 0, 0, 0, 0, 0, 0
		iter := h.client.Collection(fmt.Sprintf("tenants/%s/orders", tenantID)).Documents(ctx)
		defer iter.Stop()
		for {
			doc, err := iter.Next()
			if err == iterator.Done { break }
			if err != nil { break }
			var order models.Order
			if err := doc.DataTo(&order); err != nil { continue }
			if !orderInRange(order, start, end) { continue }
			if order.Status == "fulfilled" || order.Status == "dispatched" {
				totDispatched++
				if !order.SLAAtRisk { onTime++ } else { overdue++ }
				continue
			}
			dby := order.DespatchByDate; if dby == "" { dby = order.PromisedShipBy }
			if dby == "" { noSLA++; continue }
			dbt, err := time.Parse("2006-01-02", dby)
			if err != nil { dbt, _ = time.Parse(time.RFC3339, dby) }
			dbt = time.Date(dbt.Year(), dbt.Month(), dbt.Day(), 23, 59, 59, 0, time.UTC)
			switch {
			case dbt.Before(today): overdue++
			case dbt.Before(tomorrow) || dbt.Equal(today): dueToday++
			case dbt.Before(dayAfter): dueTomorrow++
			default: onTrack++
			}
		}
		onTimePct := 0.0; if totDispatched > 0 { onTimePct = float64(onTime)/float64(totDispatched)*100 }
		writeRow("Total Dispatched", fmt.Sprintf("%d", totDispatched))
		writeRow("On Time", fmt.Sprintf("%d", onTime))
		writeRow("On Time %", fmt.Sprintf("%.1f", onTimePct))
		writeRow("Overdue", fmt.Sprintf("%d", overdue))
		writeRow("Due Today", fmt.Sprintf("%d", dueToday))
		writeRow("Due Tomorrow", fmt.Sprintf("%d", dueTomorrow))
		writeRow("On Track", fmt.Sprintf("%d", onTrack))
		writeRow("No SLA", fmt.Sprintf("%d", noSLA))

	// ── Returns ───────────────────────────────────────────────────────────────
	case "returns":
		writeRow("RMA ID", "Order ID", "Channel", "Status", "Refund Amount", "Currency", "Reason Codes", "Created At", "Resolved At")
		iter := h.client.Collection(fmt.Sprintf("tenants/%s/rmas", tenantID)).Documents(ctx)
		defer iter.Stop()
		for {
			doc, err := iter.Next()
			if err == iterator.Done { break }
			if err != nil { break }
			var rma models.RMA
			if err := doc.DataTo(&rma); err != nil { continue }
			if rma.CreatedAt.Before(start) || rma.CreatedAt.After(end) { continue }
			reasons := []string{}
			for _, l := range rma.Lines { if l.ReasonCode != "" { reasons = append(reasons, l.ReasonCode) } }
			resolvedAt := ""
			if rma.ResolvedAt != nil { resolvedAt = rma.ResolvedAt.Format("2006-01-02") }
			writeRow(
				rma.RMAID, rma.OrderID, rma.Channel, rma.Status,
				fmt.Sprintf("%.2f", rma.RefundAmount), rma.RefundCurrency,
				strings.Join(reasons, "; "),
				rma.CreatedAt.Format("2006-01-02"), resolvedAt,
			)
		}

	// ── Financial ─────────────────────────────────────────────────────────────
	case "financial":
		writeRow("Order ID", "Channel", "Order Date", "Grand Total", "Tax Amount", "Shipping Charged", "Currency", "VAT Rate Band")
		iter := h.client.Collection(fmt.Sprintf("tenants/%s/orders", tenantID)).Documents(ctx)
		defer iter.Stop()
		for {
			doc, err := iter.Next()
			if err == iterator.Done { break }
			if err != nil { break }
			var order models.Order
			if err := doc.DataTo(&order); err != nil { continue }
			if !orderInRange(order, start, end) || order.Status == "cancelled" { continue }
			// Derive VAT rate
			lineTaxRate := 0.0
			raw := doc.Data()
			if linesRaw, ok := raw["lines"].([]interface{}); ok {
				for _, lr := range linesRaw {
					if lm, ok := lr.(map[string]interface{}); ok {
						if tr, ok := lm["tax_rate"].(float64); ok && tr > 0 { lineTaxRate = tr; break }
					}
				}
			}
			taxRate := lineTaxRate
			if taxRate == 0 && order.Totals.GrandTotal.Amount > 0 {
				taxRate = math.Round(order.Totals.Tax.Amount/(order.Totals.GrandTotal.Amount-order.Totals.Tax.Amount)*100) / 100
			}
			vatBand := "Exempt/Zero"
			if taxRate >= 0.20 { vatBand = "Standard (20%)" } else if taxRate > 0 { vatBand = fmt.Sprintf("Reduced (%.0f%%)", taxRate*100) }
			writeRow(
				order.OrderID, order.Channel, order.OrderDate,
				fmt.Sprintf("%.2f", order.Totals.GrandTotal.Amount),
				fmt.Sprintf("%.2f", order.Totals.Tax.Amount),
				fmt.Sprintf("%.2f", order.Totals.Shipping.Amount),
				order.Totals.GrandTotal.Currency, vatBand,
			)
		}

	default:
		c.String(200, fmt.Sprintf("unknown report: %s\n", report))
	}
}

// ============================================================================
// SESSION 4 GAP: CHANNEL SYNC HEALTH
// GET /analytics/operational/channel-sync
// Aggregates order_import_jobs + marketplace_import_jobs + inventory_sync_log
// per channel, returning: total runs, error count, last sync time, last error.
// ============================================================================

type ChannelSyncHealthItem struct {
	Channel        string `json:"channel"`
	CredentialID   string `json:"credential_id"`
	TotalRuns      int    `json:"total_runs"`
	ErrorCount     int    `json:"error_count"`
	ErrorRate      float64 `json:"error_rate"`
	LastSyncAt     string `json:"last_sync_at"`
	LastSyncStatus string `json:"last_sync_status"`
	LastErrorMsg   string `json:"last_error_msg,omitempty"`
}

type ChannelSyncHealthResponse struct {
	AsOf     string                  `json:"as_of"`
	Channels []ChannelSyncHealthItem `json:"channels"`
}

func (h *AnalyticsHandler) GetChannelSyncHealth(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	// Look back 7 days by default; allow ?hours=N override
	hours := 168 // 7 days
	if h := c.Query("hours"); h != "" {
		fmt.Sscanf(h, "%d", &hours)
	}
	since := time.Now().Add(-time.Duration(hours) * time.Hour)

	type channelAgg struct {
		credentialID   string
		channel        string
		total          int
		errors         int
		lastSyncAt     time.Time
		lastSyncStatus string
		lastErrorMsg   string
	}

	// key: channel (or credentialID if channel blank)
	aggMap := map[string]*channelAgg{}

	upsert := func(credID, channel, status, errMsg string, ts time.Time) {
		key := channel
		if key == "" {
			key = credID
		}
		if _, ok := aggMap[key]; !ok {
			aggMap[key] = &channelAgg{credentialID: credID, channel: channel}
		}
		a := aggMap[key]
		if credID != "" && a.credentialID == "" {
			a.credentialID = credID
		}
		if channel != "" && a.channel == "" {
			a.channel = channel
		}
		a.total++
		mapped := mapStatus(status)
		if mapped == "error" {
			a.errors++
			if a.lastErrorMsg == "" {
				a.lastErrorMsg = errMsg
			}
		}
		if ts.After(a.lastSyncAt) {
			a.lastSyncAt = ts
			a.lastSyncStatus = mapped
			if mapped == "error" {
				a.lastErrorMsg = errMsg
			}
		}
	}

	// ── order_import_jobs ─────────────────────────────────────────────────────
	iter := h.client.Collection(fmt.Sprintf("tenants/%s/order_import_jobs", tenantID)).
		Where("created_at", ">=", since).
		OrderBy("created_at", firestore.Desc).
		Limit(500).Documents(ctx)
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		data := doc.Data()
		ts, _ := data["created_at"].(time.Time)
		if ts.IsZero() {
			if s, ok := data["created_at"].(string); ok {
				ts, _ = time.Parse(time.RFC3339, s)
			}
		}
		upsert(getString(data, "credential_id"), getString(data, "channel"),
			getString(data, "status"), getString(data, "error"), ts)
	}
	iter.Stop()

	// ── marketplace_import_jobs ───────────────────────────────────────────────
	iter2 := h.client.Collection(fmt.Sprintf("tenants/%s/marketplace_import_jobs", tenantID)).
		Where("created_at", ">=", since).
		OrderBy("created_at", firestore.Desc).
		Limit(500).Documents(ctx)
	for {
		doc, err := iter2.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		data := doc.Data()
		ts, _ := data["created_at"].(time.Time)
		if ts.IsZero() {
			if s, ok := data["created_at"].(string); ok {
				ts, _ = time.Parse(time.RFC3339, s)
			}
		}
		upsert(getString(data, "credential_id"), getString(data, "channel"),
			getString(data, "status"), getString(data, "error"), ts)
	}
	iter2.Stop()

	// ── inventory_sync_log ────────────────────────────────────────────────────
	iter3 := h.client.Collection(fmt.Sprintf("tenants/%s/inventory_sync_log", tenantID)).
		Where("created_at", ">=", since).
		OrderBy("created_at", firestore.Desc).
		Limit(500).Documents(ctx)
	for {
		doc, err := iter3.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		data := doc.Data()
		ts, _ := data["created_at"].(time.Time)
		if ts.IsZero() {
			if s, ok := data["created_at"].(string); ok {
				ts, _ = time.Parse(time.RFC3339, s)
			}
		}
		ch := getString(data, "channel")
		if ch == "" {
			ch = getString(data, "source") // some logs use "source"
		}
		upsert(getString(data, "credential_id"), ch,
			getString(data, "status"), getString(data, "error"), ts)
	}
	iter3.Stop()

	items := make([]ChannelSyncHealthItem, 0, len(aggMap))
	for _, a := range aggMap {
		errRate := 0.0
		if a.total > 0 {
			errRate = math.Round(float64(a.errors)/float64(a.total)*10000) / 100
		}
		lastAt := ""
		if !a.lastSyncAt.IsZero() {
			lastAt = a.lastSyncAt.Format(time.RFC3339)
		}
		items = append(items, ChannelSyncHealthItem{
			Channel:        a.channel,
			CredentialID:   a.credentialID,
			TotalRuns:      a.total,
			ErrorCount:     a.errors,
			ErrorRate:      errRate,
			LastSyncAt:     lastAt,
			LastSyncStatus: a.lastSyncStatus,
			LastErrorMsg:   a.lastErrorMsg,
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].TotalRuns > items[j].TotalRuns })

	c.JSON(http.StatusOK, ChannelSyncHealthResponse{
		AsOf:     time.Now().UTC().Format(time.RFC3339),
		Channels: items,
	})
}

// ============================================================================
// SESSION 4 GAP: WAREHOUSE THROUGHPUT
// GET /analytics/operational/throughput?date=YYYY-MM-DD
// Counts shipments created per hour for a given day (default: today).
// Each bucket: hour (0-23), count of shipments created in that hour.
// Also returns pick/pack proxy: orders moved to ready_to_fulfil/fulfilled
// per hour on the same day, derived from order updated_at timestamps.
// ============================================================================

type ThroughputHour struct {
	Hour       int    `json:"hour"`       // 0–23
	Label      string `json:"label"`      // "09:00"
	Dispatched int    `json:"dispatched"` // shipments created this hour
	Fulfilled  int    `json:"fulfilled"`  // orders fulfilled this hour (proxy for packed)
}

type WarehouseThroughputResponse struct {
	Date            string           `json:"date"`
	TotalDispatched int              `json:"total_dispatched"`
	TotalFulfilled  int              `json:"total_fulfilled"`
	PeakHour        int              `json:"peak_hour"`
	PeakCount       int              `json:"peak_count"`
	Hourly          []ThroughputHour `json:"hourly"`
}

func (h *AnalyticsHandler) GetWarehouseThroughput(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	// Parse target date
	dateStr := c.DefaultQuery("date", time.Now().UTC().Format("2006-01-02"))
	targetDate, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		targetDate = time.Now().UTC()
	}
	dayStart := time.Date(targetDate.Year(), targetDate.Month(), targetDate.Day(), 0, 0, 0, 0, time.UTC)
	dayEnd := dayStart.Add(24 * time.Hour)

	// hourly buckets
	dispatched := make([]int, 24)
	fulfilled := make([]int, 24)

	// ── Shipments: count by created_at hour ───────────────────────────────────
	shipIter := h.client.Collection(fmt.Sprintf("tenants/%s/shipments", tenantID)).Documents(ctx)
	defer shipIter.Stop()
	for {
		doc, err := shipIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		data := doc.Data()
		var ts time.Time
		if t, ok := data["created_at"].(time.Time); ok {
			ts = t
		} else if s, ok := data["created_at"].(string); ok {
			ts, _ = time.Parse(time.RFC3339, s)
		}
		if ts.IsZero() || ts.Before(dayStart) || !ts.Before(dayEnd) {
			continue
		}
		dispatched[ts.Hour()]++
	}

	// ── Orders: count fulfilled/dispatched by updated_at hour ─────────────────
	ordIter := h.client.Collection(fmt.Sprintf("tenants/%s/orders", tenantID)).Documents(ctx)
	defer ordIter.Stop()
	for {
		doc, err := ordIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		var order models.Order
		if err := doc.DataTo(&order); err != nil {
			continue
		}
		if order.Status != "fulfilled" && order.Status != "dispatched" {
			continue
		}
		// Use updated_at as proxy for when order was fulfilled
		ts, err := time.Parse(time.RFC3339, order.UpdatedAt)
		if err != nil {
			ts, err = time.Parse("2006-01-02", order.UpdatedAt)
			if err != nil {
				continue
			}
		}
		if ts.Before(dayStart) || !ts.Before(dayEnd) {
			continue
		}
		fulfilled[ts.Hour()]++
	}

	// Build response
	hourly := make([]ThroughputHour, 24)
	totalDisp, totalFulf := 0, 0
	peakHour, peakCount := 0, 0
	for h := 0; h < 24; h++ {
		hourly[h] = ThroughputHour{
			Hour:       h,
			Label:      fmt.Sprintf("%02d:00", h),
			Dispatched: dispatched[h],
			Fulfilled:  fulfilled[h],
		}
		totalDisp += dispatched[h]
		totalFulf += fulfilled[h]
		if dispatched[h] > peakCount {
			peakCount = dispatched[h]
			peakHour = h
		}
	}

	c.JSON(http.StatusOK, WarehouseThroughputResponse{
		Date:            dateStr,
		TotalDispatched: totalDisp,
		TotalFulfilled:  totalFulf,
		PeakHour:        peakHour,
		PeakCount:       peakCount,
		Hourly:          hourly,
	})
}
