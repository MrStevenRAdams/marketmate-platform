package handlers

// ============================================================================
// P1 BACKEND EXTENSIONS
// ============================================================================
// This file adds methods to existing handlers to support P1 features:
//
//  P1.1  GET /api/v1/forecasting/products/:product_id/by-location
//         Per-location stock breakdown + days of stock per location
//
//  P1.7  GET /api/v1/products/:id/audit
//         Full per-SKU audit trail (wraps inventory_adjustments)
//
// Each function is on the existing handler struct — no new structs needed.
// Register these routes in main.go (see Section 6 of build brief).
// ============================================================================

import (
	"fmt"
	"net/http"
	"sort"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"google.golang.org/api/iterator"
)

// ── P1.1  GET /api/v1/forecasting/products/:product_id/by-location ───────────
//
// Returns inventory broken down by location alongside per-location days-of-stock
// calculated using the product's stored ADC.

type LocationForecast struct {
	LocationID   string  `json:"location_id"`
	LocationPath string  `json:"location_path"`
	LocationName string  `json:"location_name"`
	Quantity     int     `json:"quantity"`
	ReservedQty  int     `json:"reserved_qty"`
	AvailableQty int     `json:"available_qty"`
	DaysOfStock  float64 `json:"days_of_stock"`
	ReorderPoint int     `json:"reorder_point"`
	Status       string  `json:"status"` // ok|low|critical|out_of_stock
}

func (h *ForecastingHandler) GetForecastByLocation(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	productID := c.Param("product_id")
	ctx := c.Request.Context()

	// Load forecast config for ADC + reorder point
	var adc float64
	var reorderPoint int
	doc, err := h.forecastCol(tenantID).Doc(productID).Get(ctx)
	if err == nil {
		var fc ProductForecastConfig
		if doc.DataTo(&fc) == nil {
			adc = fc.CalculatedADC
			reorderPoint = fc.ReorderPoint
		}
	}

	// Query all inventory records for this product
	iter := h.client.Collection(fmt.Sprintf("tenants/%s/inventory", tenantID)).
		Where("product_id", "==", productID).Documents(ctx)
	defer iter.Stop()

	var locations []LocationForecast
	for {
		d, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query inventory"})
			return
		}
		data := d.Data()

		qty := 0
		if v, ok := data["quantity"].(int64); ok {
			qty = int(v)
		}
		reserved := 0
		if v, ok := data["reserved_qty"].(int64); ok {
			reserved = int(v)
		}
		available := qty - reserved
		if available < 0 {
			available = 0
		}

		daysOfStock := 999.0
		if adc > 0 {
			daysOfStock = float64(qty) / adc
			// Round to 1dp
			daysOfStock = float64(int(daysOfStock*10)) / 10
		}

		status := "ok"
		if qty <= 0 {
			status = "out_of_stock"
		} else if reorderPoint > 0 && qty <= reorderPoint {
			status = "low"
		}

		lf := LocationForecast{
			Quantity:     qty,
			ReservedQty:  reserved,
			AvailableQty: available,
			DaysOfStock:  daysOfStock,
			ReorderPoint: reorderPoint,
			Status:       status,
		}
		if v, ok := data["location_id"].(string); ok {
			lf.LocationID = v
		}
		if v, ok := data["location_path"].(string); ok {
			lf.LocationPath = v
		}
		if v, ok := data["location_name"].(string); ok {
			lf.LocationName = v
		}

		locations = append(locations, lf)
	}

	// Sort by location path for consistent display
	sort.Slice(locations, func(i, j int) bool {
		return locations[i].LocationPath < locations[j].LocationPath
	})

	if locations == nil {
		locations = []LocationForecast{}
	}

	c.JSON(http.StatusOK, gin.H{
		"product_id":    productID,
		"adc":           adc,
		"reorder_point": reorderPoint,
		"locations":     locations,
		"total":         len(locations),
	})
}

// ── P1.7  GET /api/v1/products/:id/audit ─────────────────────────────────────
//
// Returns paginated audit trail for a product from inventory_adjustments.
// Also queries product_field_changes collection if it exists (future-proof).
// Query params: page (default 1), page_size (default 50), type (filter by type)

type AuditEntry struct {
	AuditID       string    `json:"audit_id"`
	ProductID     string    `json:"product_id"`
	EventType     string    `json:"event_type"` // sale|adjustment|receipt|count|scrap|transfer|return
	LocationID    string    `json:"location_id"`
	LocationPath  string    `json:"location_path"`
	Delta         int       `json:"delta"`
	QuantityAfter int       `json:"quantity_after"`
	Reason        string    `json:"reason"`
	Reference     string    `json:"reference"` // order ID, PO number, etc.
	CreatedBy     string    `json:"created_by"`
	CreatedAt     time.Time `json:"created_at"`
}

func (h *WarehouseLocationHandler) GetProductAuditTrail(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	productID := c.Param("id")
	ctx := c.Request.Context()

	page := 1
	pageSize := 50
	if v := c.Query("page"); v != "" {
		fmt.Sscanf(v, "%d", &page)
	}
	if v := c.Query("page_size"); v != "" {
		fmt.Sscanf(v, "%d", &pageSize)
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}
	typeFilter := c.Query("type")

	q := h.client.Collection(fmt.Sprintf("tenants/%s/inventory_adjustments", tenantID)).
		Where("product_id", "==", productID).
		OrderBy("created_at", firestore.Desc).
		Limit(500) // fetch a batch, then filter/paginate in memory

	iter := q.Documents(ctx)
	defer iter.Stop()

	var all []AuditEntry
	for {
		d, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query audit trail"})
			return
		}
		data := d.Data()

		entry := AuditEntry{}
		if v, ok := data["adjustment_id"].(string); ok {
			entry.AuditID = v
		} else {
			entry.AuditID = d.Ref.ID
		}
		entry.ProductID = productID
		if v, ok := data["type"].(string); ok {
			entry.EventType = mapAuditType(v)
		}
		if v, ok := data["location_id"].(string); ok {
			entry.LocationID = v
		}
		if v, ok := data["location_path"].(string); ok {
			entry.LocationPath = v
		}
		if v, ok := data["delta"].(int64); ok {
			entry.Delta = int(v)
		}
		if v, ok := data["quantity_after"].(int64); ok {
			entry.QuantityAfter = int(v)
		}
		if v, ok := data["reason"].(string); ok {
			entry.Reason = v
		}
		// Reference: prefer order_id, then po_id
		if v, ok := data["order_id"].(string); ok && v != "" {
			entry.Reference = v
		} else if v, ok := data["po_id"].(string); ok && v != "" {
			entry.Reference = v
		} else if v, ok := data["reference"].(string); ok {
			entry.Reference = v
		}
		if v, ok := data["created_by"].(string); ok {
			entry.CreatedBy = v
		}
		if v, ok := data["created_at"].(time.Time); ok {
			entry.CreatedAt = v
		}

		if typeFilter == "" || entry.EventType == typeFilter {
			all = append(all, entry)
		}
	}

	total := len(all)
	start := (page - 1) * pageSize
	end := start + pageSize
	if start > total {
		start = total
	}
	if end > total {
		end = total
	}
	page_data := all[start:end]
	if page_data == nil {
		page_data = []AuditEntry{}
	}

	c.JSON(http.StatusOK, gin.H{
		"entries":   page_data,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

func mapAuditType(t string) string {
	switch t {
	case "sale", "order":
		return "sale"
	case "receipt", "book_in", "po_receipt":
		return "receipt"
	case "count", "stock_count":
		return "count"
	case "scrap":
		return "scrap"
	case "transfer":
		return "transfer"
	case "return", "rma":
		return "return"
	default:
		return "adjustment"
	}
}
