package handlers

// ============================================================================
// REPORT HANDLER — Query Data / Custom Report Builder
// Routes:
//   POST /api/v1/reports/run     — run a dynamic query and return rows
//   GET  /api/v1/reports/saved   — list saved report configs
//   POST /api/v1/reports/saved   — save a report config
// ============================================================================

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"
)

type ReportHandler struct {
	client *firestore.Client
}

func NewReportHandler(client *firestore.Client) *ReportHandler {
	return &ReportHandler{client: client}
}

// ============================================================================
// DATA STRUCTURES
// ============================================================================

type ReportFilter struct {
	Field    string `json:"field"`
	Operator string `json:"operator"` // eq, neq, contains, gt, lt, gte, lte
	Value    string `json:"value"`
}

type RunReportRequest struct {
	Entity   string         `json:"entity" binding:"required"` // orders | products | inventory | rmas
	Filters  []ReportFilter `json:"filters"`
	Fields   []string       `json:"fields"` // which columns to include; empty = all
	DateFrom string         `json:"date_from,omitempty"`
	DateTo   string         `json:"date_to,omitempty"`
}

type RunReportResponse struct {
	Entity  string                   `json:"entity"`
	Count   int                      `json:"count"`
	Columns []string                 `json:"columns"`
	Rows    []map[string]interface{} `json:"rows"`
}

type SavedReport struct {
	ReportID  string            `firestore:"report_id" json:"report_id"`
	TenantID  string            `firestore:"tenant_id" json:"tenant_id"`
	Name      string            `firestore:"name" json:"name"`
	Entity    string            `firestore:"entity" json:"entity"`
	Filters   []ReportFilter    `firestore:"filters" json:"filters"`
	Fields    []string          `firestore:"fields" json:"fields"`
	DateFrom  string            `firestore:"date_from,omitempty" json:"date_from,omitempty"`
	DateTo    string            `firestore:"date_to,omitempty" json:"date_to,omitempty"`
	CreatedAt string            `firestore:"created_at" json:"created_at"`
	UpdatedAt string            `firestore:"updated_at" json:"updated_at"`
}

type SaveReportRequest struct {
	Name     string         `json:"name" binding:"required"`
	Entity   string         `json:"entity" binding:"required"`
	Filters  []ReportFilter `json:"filters"`
	Fields   []string       `json:"fields"`
	DateFrom string         `json:"date_from,omitempty"`
	DateTo   string         `json:"date_to,omitempty"`
}

// ============================================================================
// FIELD DEFINITIONS PER ENTITY
// These are the extractable columns for each entity type.
// ============================================================================

var entityFields = map[string][]string{
	"orders": {
		"order_id", "external_order_id", "channel", "status", "sub_status",
		"customer_name", "customer_email",
		"shipping_city", "shipping_country",
		"grand_total", "currency",
		"payment_status", "fulfilment_source",
		"order_date", "created_at", "updated_at",
	},
	"products": {
		"product_id", "sku", "title", "status", "product_type",
		"brand", "weight", "length", "width", "height",
		"created_at", "updated_at",
	},
	"inventory": {
		"inventory_id", "sku", "product_name",
		"total_on_hand", "total_reserved", "total_available", "total_inbound",
		"safety_stock", "reorder_point", "updated_at",
	},
	"rmas": {
		"rma_id", "rma_number", "order_id", "channel", "status",
		"customer_name",
		"refund_action", "refund_amount", "refund_currency",
		"created_at",
	},
}

// ============================================================================
// POST /reports/run
// ============================================================================

func (h *ReportHandler) RunReport(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req RunReportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	knownFields, ok := entityFields[req.Entity]
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unknown entity: " + req.Entity})
		return
	}

	// Determine which columns to return
	columns := req.Fields
	if len(columns) == 0 {
		columns = knownFields
	}

	// Parse optional date range
	var dateFrom, dateTo time.Time
	if req.DateFrom != "" {
		dateFrom, _ = time.Parse("2006-01-02", req.DateFrom)
	}
	if req.DateTo != "" {
		dateTo, _ = time.Parse("2006-01-02", req.DateTo)
		dateTo = time.Date(dateTo.Year(), dateTo.Month(), dateTo.Day(), 23, 59, 59, 0, time.UTC)
	}

	rows, err := h.fetchRows(c, tenantID, req.Entity, req.Filters, columns, dateFrom, dateTo)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, RunReportResponse{
		Entity:  req.Entity,
		Count:   len(rows),
		Columns: columns,
		Rows:    rows,
	})
}

// fetchRows iterates the Firestore collection for the given entity, applies
// filters, and projects the requested columns.
func (h *ReportHandler) fetchRows(
	c *gin.Context,
	tenantID, entity string,
	filters []ReportFilter,
	columns []string,
	dateFrom, dateTo time.Time,
) ([]map[string]interface{}, error) {
	ctx := c.Request.Context()
	colPath := fmt.Sprintf("tenants/%s/%s", tenantID, entity)
	iter := h.client.Collection(colPath).Documents(ctx)
	defer iter.Stop()

	var rows []map[string]interface{}

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}

		raw := doc.Data()

		// Date range filter on created_at / updated_at
		if !dateFrom.IsZero() || !dateTo.IsZero() {
			tsStr, _ := raw["created_at"].(string)
			ts, parseErr := time.Parse(time.RFC3339, tsStr)
			if parseErr != nil {
				ts, parseErr = time.Parse("2006-01-02", tsStr)
			}
			if parseErr == nil {
				if !dateFrom.IsZero() && ts.Before(dateFrom) {
					continue
				}
				if !dateTo.IsZero() && ts.After(dateTo) {
					continue
				}
			}
		}

		// Flatten common nested fields for easier filtering
		flat := flattenDoc(raw, entity)

		// Apply user-defined filters
		if !applyFilters(flat, filters) {
			continue
		}

		// Project to requested columns
		row := map[string]interface{}{}
		for _, col := range columns {
			row[col] = flat[col]
		}
		rows = append(rows, row)
	}

	if rows == nil {
		rows = []map[string]interface{}{}
	}
	return rows, nil
}

// flattenDoc extracts nested fields into a flat string-keyed map for filtering.
func flattenDoc(raw map[string]interface{}, entity string) map[string]interface{} {
	flat := map[string]interface{}{}
	for k, v := range raw {
		flat[k] = v
	}

	switch entity {
	case "orders":
		if customer, ok := raw["customer"].(map[string]interface{}); ok {
			flat["customer_name"] = customer["name"]
			flat["customer_email"] = customer["email"]
		}
		if shipping, ok := raw["shipping_address"].(map[string]interface{}); ok {
			flat["shipping_city"] = shipping["city"]
			flat["shipping_country"] = shipping["country"]
		}
		if totals, ok := raw["totals"].(map[string]interface{}); ok {
			if gt, ok := totals["grand_total"].(map[string]interface{}); ok {
				flat["grand_total"] = gt["amount"]
				flat["currency"] = gt["currency"]
			}
		}
	case "rmas":
		if customer, ok := raw["customer"].(map[string]interface{}); ok {
			flat["customer_name"] = customer["name"]
		}
	}

	return flat
}

// applyFilters returns true if the row passes all filters.
func applyFilters(flat map[string]interface{}, filters []ReportFilter) bool {
	for _, f := range filters {
		val, exists := flat[f.Field]
		if !exists {
			if f.Operator == "eq" {
				return false
			}
			continue
		}
		valStr := fmt.Sprintf("%v", val)
		filterVal := f.Value

		switch f.Operator {
		case "eq":
			if !strings.EqualFold(valStr, filterVal) {
				return false
			}
		case "neq":
			if strings.EqualFold(valStr, filterVal) {
				return false
			}
		case "contains":
			if !strings.Contains(strings.ToLower(valStr), strings.ToLower(filterVal)) {
				return false
			}
		case "starts_with":
			if !strings.HasPrefix(strings.ToLower(valStr), strings.ToLower(filterVal)) {
				return false
			}
		case "gt", "lt", "gte", "lte":
			// Numeric comparison
			var numVal, numFilter float64
			fmt.Sscanf(valStr, "%f", &numVal)
			fmt.Sscanf(filterVal, "%f", &numFilter)
			switch f.Operator {
			case "gt":
				if !(numVal > numFilter) {
					return false
				}
			case "lt":
				if !(numVal < numFilter) {
					return false
				}
			case "gte":
				if !(numVal >= numFilter) {
					return false
				}
			case "lte":
				if !(numVal <= numFilter) {
					return false
				}
			}
		}
	}
	return true
}

// ============================================================================
// GET /reports/saved
// ============================================================================

func (h *ReportHandler) ListSavedReports(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	iter := h.client.Collection(fmt.Sprintf("tenants/%s/saved_reports", tenantID)).
		OrderBy("created_at", firestore.Desc).
		Limit(100).
		Documents(ctx)
	defer iter.Stop()

	var reports []SavedReport
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		var r SavedReport
		if err := doc.DataTo(&r); err == nil {
			reports = append(reports, r)
		}
	}
	if reports == nil {
		reports = []SavedReport{}
	}
	c.JSON(http.StatusOK, gin.H{"reports": reports})
}

// ============================================================================
// POST /reports/saved
// ============================================================================

func (h *ReportHandler) SaveReport(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	var req SaveReportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if _, ok := entityFields[req.Entity]; !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unknown entity: " + req.Entity})
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	report := SavedReport{
		ReportID:  uuid.New().String(),
		TenantID:  tenantID,
		Name:      req.Name,
		Entity:    req.Entity,
		Filters:   req.Filters,
		Fields:    req.Fields,
		DateFrom:  req.DateFrom,
		DateTo:    req.DateTo,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if report.Filters == nil {
		report.Filters = []ReportFilter{}
	}
	if report.Fields == nil {
		report.Fields = []string{}
	}

	_, err := h.client.Collection(fmt.Sprintf("tenants/%s/saved_reports", tenantID)).
		Doc(report.ReportID).
		Set(ctx, report)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, report)
}

// ============================================================================
// GET /reports/fields — helper: return available fields for an entity
// ============================================================================

func (h *ReportHandler) GetEntityFields(c *gin.Context) {
	entity := c.Param("entity")
	fields, ok := entityFields[entity]
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unknown entity"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"entity": entity, "fields": fields})
}
