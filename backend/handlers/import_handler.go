package handlers

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"module-a/models"
	"module-a/repository"
	"module-a/services"
)

// ============================================================================
// IMPORT HANDLER
// Routes:
//   POST   /api/v1/import/validate           — validate file, return row-level errors
//   POST   /api/v1/import/preview            — parse file headers + first 3 rows + auto-mapping
//   POST   /api/v1/import/apply              — apply validated import (background job)
//   GET    /api/v1/import/status/:job_id     — poll import job status
//   DELETE /api/v1/import/jobs/:id           — delete an import job record
//   GET    /api/v1/import/templates/:type    — download template CSV
//   GET    /api/v1/import/history            — last 20 import jobs for this tenant
// ============================================================================

type ImportHandler struct {
	repo           *repository.FirestoreRepository
	productService *services.ProductService
	client         *firestore.Client
}

func NewImportHandler(repo *repository.FirestoreRepository, productService *services.ProductService, client *firestore.Client) *ImportHandler {
	return &ImportHandler{repo: repo, productService: productService, client: client}
}

// ─── FileConfig ───────────────────────────────────────────────────────────────

type FileConfig struct {
	Delimiter    string
	Encoding     string
	HasHeaderRow bool
	EscapeChar   string
}

func fileConfigFromForm(c *gin.Context) FileConfig {
	return FileConfig{
		Delimiter:    c.DefaultPostForm("delimiter", ","),
		Encoding:     c.DefaultPostForm("encoding", "utf-8"),
		HasHeaderRow: c.DefaultPostForm("has_header_row", "true") != "false",
		EscapeChar:   c.DefaultPostForm("escape_char", ""),
	}
}

// ─── Job / Validation Structures ─────────────────────────────────────────────

type ImportJobStatus string

const (
	JobStatusPending    ImportJobStatus = "pending"
	JobStatusProcessing ImportJobStatus = "processing"
	JobStatusDone       ImportJobStatus = "done"
	JobStatusFailed     ImportJobStatus = "failed"
)

type ImportJob struct {
	JobID         string          `json:"job_id" firestore:"job_id"`
	TenantID      string          `json:"tenant_id" firestore:"tenant_id"`
	ImportType    string          `json:"import_type" firestore:"import_type"`
	Filename      string          `json:"filename" firestore:"filename"`
	Status        ImportJobStatus `json:"status" firestore:"status"`
	TotalRows     int             `json:"total_rows" firestore:"total_rows"`
	ProcessedRows int             `json:"processed_rows" firestore:"processed_rows"`
	CreatedCount  int             `json:"created_count" firestore:"created_count"`
	UpdatedCount  int             `json:"updated_count" firestore:"updated_count"`
	FailedCount   int             `json:"failed_count" firestore:"failed_count"`
	ErrorReport   []RowError      `json:"error_report,omitempty" firestore:"error_report,omitempty"`
	CreatedAt     time.Time       `json:"created_at" firestore:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at" firestore:"updated_at"`
}

type RowError struct {
	Row     int    `json:"row" firestore:"row"`
	Column  string `json:"column" firestore:"column"`
	Message string `json:"message" firestore:"message"`
}

type RowWarning struct {
	Row     int    `json:"row" firestore:"row"`
	Column  string `json:"column" firestore:"column"`
	Message string `json:"message" firestore:"message"`
}

type ValidationResult struct {
	TotalRows        int          `json:"total_rows"`
	ValidRows        int          `json:"valid_rows"`
	CreateCount      int          `json:"create_count"`
	UpdateCount      int          `json:"update_count"`
	ErrorCount       int          `json:"error_count"`
	WarningCount     int          `json:"warning_count"`
	Errors           []RowError   `json:"errors"`
	Warnings         []RowWarning `json:"warnings"`
	UnknownLocations []string     `json:"unknown_locations,omitempty"`
}

// ─── Preview ──────────────────────────────────────────────────────────────────

// PreviewImport POST /api/v1/import/preview
// Returns file headers, first 3 data rows, required/optional field lists, and auto-mapping.
func (h *ImportHandler) PreviewImport(c *gin.Context) {
	importType := c.PostForm("type")
	if importType == "" {
		c.JSON(400, gin.H{"error": "type field required"})
		return
	}
	cfg := fileConfigFromForm(c)
	rows, headers, _, err := parseUploadFileWithConfig(c, cfg)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	previewRows := rows
	if len(previewRows) > 3 {
		previewRows = rows[:3]
	}

	requiredFields, optionalFields := importTypeFields(importType)

	// Auto-map: for each target field, find matching file header (exact, then no-underscore fuzzy)
	autoMapping := map[string]string{}
	for _, tf := range append(requiredFields, optionalFields...) {
		tfLower := strings.ToLower(tf)
		for _, fh := range headers {
			if strings.ToLower(fh) == tfLower {
				autoMapping[tf] = fh
				break
			}
		}
		if _, found := autoMapping[tf]; !found {
			tfStripped := strings.ReplaceAll(tfLower, "_", "")
			for _, fh := range headers {
				if strings.ReplaceAll(strings.ToLower(fh), "_", "") == tfStripped {
					autoMapping[tf] = fh
					break
				}
			}
		}
	}

	c.JSON(200, gin.H{
		"headers":         headers,
		"preview_rows":    previewRows,
		"required_fields": requiredFields,
		"optional_fields": optionalFields,
		"auto_mapping":    autoMapping,
	})
}

func importTypeFields(importType string) (required []string, optional []string) {
	switch importType {
	case "products":
		// Required: sku + title are the minimum for any product row.
		// Optional: all other fixed columns, plus any attribute_{key} / variant_attr_{key} columns
		// the user's file happens to contain — those are auto-detected during import.
		// The old numbered pair format (attribute_1_name/value) is still accepted for backward compat.
		return []string{"sku", "title"},
			[]string{
				"product_id", "product_type", "parent_sku", "status",
				"subtitle", "description", "brand",
				"ean", "upc", "asin", "isbn", "mpn", "gtin",
				"categories", "tags", "key_features", "attribute_set_id",
				"list_price", "currency", "rrp", "cost_price",
				"sale_price", "sale_start", "sale_end",
				"quantity",
				"weight_value", "weight_unit",
				"length", "width", "height", "dimension_unit",
				"shipping_weight_value", "shipping_weight_unit",
				"shipping_length", "shipping_width", "shipping_height", "shipping_dimension_unit",
				"use_serial_numbers", "end_of_life", "storage_group_id",
				"alias", "barcode",
				"supplier_sku", "supplier_name", "supplier_cost",
				"supplier_currency", "supplier_lead_time_days",
				"image_1", "image_2", "image_3", "image_4", "image_5",
				"bundle_component_skus",
				// Attribute columns are dynamic — any column starting with
				// "attribute_" or "variant_attr_" is automatically parsed.
				// The examples below help the column-mapping UI show suggestions.
				"attribute_colour", "attribute_material", "attribute_size",
				"variant_attr_colour", "variant_attr_size",
			}
	case "listings":
		return []string{"sku", "channel", "price"}, []string{"currency", "status"}
	case "prices":
		return []string{"sku"}, []string{"price_ebay", "price_amazon", "price_temu", "rrp", "cost_price"}
	case "inventory_basic", "inventory_delta":
		return []string{"sku", "quantity"}, []string{}
	case "inventory_advanced":
		return []string{"sku", "warehouse", "location_path", "quantity"}, []string{}
	case "orders":
		return []string{"order_reference", "sku", "quantity"},
			[]string{"received_date", "despatch_by_date", "ship_name", "ship_address1", "ship_city", "ship_postcode", "ship_country", "unit_price", "currency", "shipping_service", "payment_status"}
	case "binrack_zone":
		return []string{"binrack_name", "zone_name"}, []string{}
	case "binrack_create_update":
		return []string{"name"}, []string{"barcode", "binrack_type", "zone_name", "aisle", "section", "level", "bin_number", "capacity"}
	case "binrack_item_restriction":
		return []string{"binrack_name", "sku"}, []string{}
	case "binrack_storage_group":
		return []string{"binrack_name", "storage_group_name"}, []string{}
	case "stock_migration":
		return []string{"sku", "quantity"}, []string{"warehouse_id", "binrack_name"}
	default:
		return []string{}, []string{}
	}
}

// ─── Validate ─────────────────────────────────────────────────────────────────

func (h *ImportHandler) ValidateImport(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	importType := c.PostForm("type")
	if importType == "" {
		c.JSON(400, gin.H{"error": "type field required"})
		return
	}

	cfg := fileConfigFromForm(c)
	colMappingOverride := parseColumnMapping(c)
	rows, headers, filename, err := parseUploadFileWithConfig(c, cfg)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	_ = filename
	if len(colMappingOverride) > 0 {
		headers = applyColumnMapping(headers, colMappingOverride)
	}

	var result *ValidationResult
	ctx := c.Request.Context()
	switch importType {
	case "products":
		result, err = h.validateProducts(ctx, tenantID, headers, rows)
	case "listings":
		result, err = h.validateListings(ctx, tenantID, headers, rows)
	case "prices":
		result, err = h.validatePrices(ctx, tenantID, headers, rows)
	case "inventory_basic":
		result, err = h.validateInventoryBasic(ctx, tenantID, headers, rows)
	case "inventory_delta":
		result, err = h.validateInventoryDelta(ctx, tenantID, headers, rows)
	case "inventory_advanced":
		result, err = h.validateInventoryAdvanced(ctx, tenantID, headers, rows)
	case "orders":
		result, err = h.validateOrders(ctx, tenantID, headers, rows)
	default:
		c.JSON(400, gin.H{"error": "unknown import type: " + importType})
		return
	}
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, result)
}

// ─── Apply ────────────────────────────────────────────────────────────────────

func (h *ImportHandler) ApplyImport(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	importType := c.PostForm("type")
	confirmUnknownLocations := c.PostForm("confirm_unknown_locations") == "true"
	if importType == "" {
		c.JSON(400, gin.H{"error": "type field required"})
		return
	}

	cfg := fileConfigFromForm(c)
	colMappingOverride := parseColumnMapping(c)
	rows, headers, filename, err := parseUploadFileWithConfig(c, cfg)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if len(colMappingOverride) > 0 {
		headers = applyColumnMapping(headers, colMappingOverride)
	}

	ctx := c.Request.Context()
	var valResult *ValidationResult
	switch importType {
	case "products":
		valResult, err = h.validateProducts(ctx, tenantID, headers, rows)
	case "listings":
		valResult, err = h.validateListings(ctx, tenantID, headers, rows)
	case "prices":
		valResult, err = h.validatePrices(ctx, tenantID, headers, rows)
	case "inventory_basic":
		valResult, err = h.validateInventoryBasic(ctx, tenantID, headers, rows)
	case "inventory_delta":
		valResult, err = h.validateInventoryDelta(ctx, tenantID, headers, rows)
	case "inventory_advanced":
		valResult, err = h.validateInventoryAdvanced(ctx, tenantID, headers, rows)
	case "orders":
		valResult, err = h.validateOrders(ctx, tenantID, headers, rows)
	default:
		c.JSON(400, gin.H{"error": "unknown import type"})
		return
	}
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if valResult.ErrorCount > 0 {
		c.JSON(422, gin.H{"error": "validation failed", "validation": valResult})
		return
	}
	if importType == "inventory_advanced" && len(valResult.UnknownLocations) > 0 && !confirmUnknownLocations {
		c.JSON(409, gin.H{
			"error": "unknown_locations", "unknown_locations": valResult.UnknownLocations,
			"message": "Unknown warehouse locations found. Set confirm_unknown_locations=true to proceed.",
		})
		return
	}

	job := &ImportJob{
		JobID: uuid.New().String(), TenantID: tenantID, ImportType: importType, Filename: filename,
		Status: JobStatusPending, TotalRows: valResult.TotalRows, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	jobRef := h.client.Doc(fmt.Sprintf("tenants/%s/import_jobs_csv/%s", tenantID, job.JobID))
	if _, err := jobRef.Set(ctx, job); err != nil {
		c.JSON(500, gin.H{"error": "failed to create job: " + err.Error()})
		return
	}

	go func() {
		bgCtx := context.Background()
		h.processImport(bgCtx, job, importType, tenantID, headers, rows, valResult.UnknownLocations, filename)
	}()

	c.JSON(202, gin.H{"ok": true, "job_id": job.JobID, "message": fmt.Sprintf("Import queued: %d rows", valResult.TotalRows)})
}

// ─── Status ───────────────────────────────────────────────────────────────────

func (h *ImportHandler) GetImportStatus(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	jobID := c.Param("job_id")
	ctx := c.Request.Context()
	doc, err := h.client.Doc(fmt.Sprintf("tenants/%s/import_jobs_csv/%s", tenantID, jobID)).Get(ctx)
	if err != nil {
		c.JSON(404, gin.H{"error": "job not found"})
		return
	}
	var job ImportJob
	if err := doc.DataTo(&job); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, job)
}

// ─── Delete Import Job ────────────────────────────────────────────────────────

// DeleteImportJob DELETE /api/v1/import/jobs/:id
func (h *ImportHandler) DeleteImportJob(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	jobID := c.Param("id")
	ctx := c.Request.Context()
	ref := h.client.Doc(fmt.Sprintf("tenants/%s/import_jobs_csv/%s", tenantID, jobID))
	if _, err := ref.Get(ctx); err != nil {
		c.JSON(404, gin.H{"error": "job not found"})
		return
	}
	if _, err := ref.Delete(ctx); err != nil {
		c.JSON(500, gin.H{"error": "failed to delete job: " + err.Error()})
		return
	}
	c.JSON(200, gin.H{"deleted": true, "job_id": jobID})
}

// ─── History ──────────────────────────────────────────────────────────────────

func (h *ImportHandler) GetImportHistory(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()
	iter := h.client.Collection(fmt.Sprintf("tenants/%s/import_jobs_csv", tenantID)).
		OrderBy("created_at", firestore.Desc).Limit(20).Documents(ctx)
	var jobs []ImportJob
	for {
		doc, err := iter.Next()
		if err != nil {
			break
		}
		var job ImportJob
		if err := doc.DataTo(&job); err == nil {
			jobs = append(jobs, job)
		}
	}
	if jobs == nil {
		jobs = []ImportJob{}
	}
	c.JSON(200, gin.H{"jobs": jobs})
}

// ─── Templates ────────────────────────────────────────────────────────────────

func (h *ImportHandler) GetTemplate(c *gin.Context) {
	templateType := c.Param("type")
	templates := map[string]struct {
		filename string
		headers  []string
		example  []string
	}{
		"orders": {
			filename: "orders_template.csv",
			headers:  []string{"order_reference", "received_date", "despatch_by_date", "ship_name", "ship_address1", "ship_address2", "ship_city", "ship_postcode", "ship_country", "bill_name", "bill_address1", "bill_city", "bill_postcode", "bill_country", "sku", "quantity", "unit_price", "currency", "shipping_service", "payment_status", "tax_amount"},
			example:  []string{"ORD-001", "2026-03-01", "2026-03-03", "John Smith", "123 High Street", "", "London", "SW1A 1AA", "GB", "John Smith", "123 High Street", "London", "SW1A 1AA", "GB", "MY-SKU-001", "2", "19.99", "GBP", "Royal Mail 2nd Class", "captured", "4.00"},
		},
		"products": {
			filename: "products_template.csv",
			// Columns match the v2 export format: named attribute columns, all PIM fields.
			// Add your own attribute columns with the attribute_{key} naming pattern.
			headers: []string{
				"product_id", "product_type", "parent_sku", "sku",
				"title", "subtitle", "description", "brand", "status",
				"ean", "upc", "asin", "isbn", "mpn", "gtin",
				"categories", "tags", "key_features", "attribute_set_id",
				"list_price", "currency", "rrp", "cost_price",
				"sale_price", "sale_start", "sale_end",
				"quantity",
				"weight_value", "weight_unit",
				"length", "width", "height", "dimension_unit",
				"shipping_weight_value", "shipping_weight_unit",
				"shipping_length", "shipping_width", "shipping_height", "shipping_dimension_unit",
				"use_serial_numbers", "end_of_life", "storage_group_id",
				"alias", "barcode",
				"supplier_sku", "supplier_name", "supplier_cost",
				"supplier_currency", "supplier_lead_time_days",
				"image_1", "image_2", "image_3", "image_4", "image_5",
				"bundle_component_skus",
				"attribute_colour", "attribute_material",
				"variant_attr_colour", "variant_attr_size",
			},
			example: []string{
				"", "simple", "", "MY-SKU-001",
				"Blue Widget", "", "A high quality widget", "AcmeCo", "active",
				"1234567890123", "", "", "", "", "",
				"Widgets|Blue Widgets", "widget|blue", "Easy to assemble|Recycled packaging", "",
				"19.99", "GBP", "24.99", "8.00",
				"", "", "",
				"100",
				"0.5", "kg",
				"12", "8", "6", "cm",
				"0.65", "kg",
				"14", "10", "8", "cm",
				"", "", "",
				"", "",
				"SUP-SKU-001", "Acme Supplies Ltd", "6.50", "GBP", "7",
				"https://example.com/img1.jpg", "", "", "", "",
				"",
				"Blue", "Recycled Plastic",
				"Blue", "M",
			},
		},
		"listings": {
			filename: "listings_template.csv",
			headers:  []string{"sku", "channel", "price", "currency", "status"},
			example:  []string{"MY-SKU-001", "amazon", "19.99", "GBP", "active"},
		},
		"prices": {
			filename: "prices_template.csv",
			headers:  []string{"sku", "price_ebay", "price_amazon", "price_temu", "rrp", "cost_price"},
			example:  []string{"MY-SKU-001", "19.99", "24.99", "15.99", "29.99", "8.00"},
		},
		"inventory_basic": {
			filename: "inventory_basic_template.csv",
			headers:  []string{"sku", "quantity"},
			example:  []string{"MY-SKU-001", "42"},
		},
		"inventory_delta": {
			filename: "inventory_delta_template.csv",
			headers:  []string{"sku", "quantity"},
			example:  []string{"MY-SKU-001", "5"},
		},
		"inventory_advanced": {
			filename: "inventory_advanced_template.csv",
			headers:  []string{"sku", "warehouse", "location_path", "quantity"},
			example:  []string{"MY-SKU-001", "London Warehouse", "Bay 1/Shelf 3/Bin 5", "42"},
		},
	}
	tmpl, ok := templates[templateType]
	if !ok {
		c.JSON(400, gin.H{"error": "unknown template type"})
		return
	}
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	w.Write(tmpl.headers)
	w.Write(tmpl.example)
	w.Flush()
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", tmpl.filename))
	c.Data(200, "text/csv; charset=utf-8", buf.Bytes())
}

// ─── Validation helpers ───────────────────────────────────────────────────────

func (h *ImportHandler) validateProducts(ctx context.Context, tenantID string, headers []string, rows [][]string) (*ValidationResult, error) {
	colIdx := buildColIdx(headers)
	result := &ValidationResult{TotalRows: len(rows)}
	existing, _, _ := h.repo.ListProducts(ctx, tenantID, map[string]interface{}{}, 0, 0)
	existingSKUs := map[string]bool{}
	for _, p := range existing {
		if p.Attributes != nil {
			if s, ok := p.Attributes["source_sku"].(string); ok && s != "" {
				existingSKUs[s] = true
			}
		}
	}
	for i, row := range rows {
		rowNum := i + 2
		sku := getColVal(row, colIdx, "sku")
		title := getColVal(row, colIdx, "title")
		ptype := getColVal(row, colIdx, "product_type")
		if sku == "" {
			result.Errors = append(result.Errors, RowError{Row: rowNum, Column: "sku", Message: "SKU is required"})
			result.ErrorCount++
			continue
		}
		if title == "" {
			result.Errors = append(result.Errors, RowError{Row: rowNum, Column: "title", Message: "Title is required"})
			result.ErrorCount++
			continue
		}
		if ptype != "" && ptype != "simple" && ptype != "parent" && ptype != "variant" && ptype != "bundle" {
			result.Errors = append(result.Errors, RowError{Row: rowNum, Column: "product_type", Message: "Must be simple, parent, variant, or bundle"})
			result.ErrorCount++
			continue
		}
		if existingSKUs[sku] {
			result.UpdateCount++
		} else {
			result.CreateCount++
		}
		result.ValidRows++
	}
	return result, nil
}

func (h *ImportHandler) validateListings(ctx context.Context, tenantID string, headers []string, rows [][]string) (*ValidationResult, error) {
	colIdx := buildColIdx(headers)
	result := &ValidationResult{TotalRows: len(rows)}
	knownChannels := map[string]bool{"amazon": true, "ebay": true, "temu": true, "shopify": true, "tesco": true}
	skuMap := buildSKUMapCtx(ctx, h.repo, tenantID)
	for i, row := range rows {
		rowNum := i + 2
		sku := getColVal(row, colIdx, "sku")
		channel := getColVal(row, colIdx, "channel")
		priceStr := getColVal(row, colIdx, "price")
		hasErr := false
		if sku == "" {
			result.Errors = append(result.Errors, RowError{Row: rowNum, Column: "sku", Message: "SKU is required"})
			hasErr = true
		} else if _, okV := skuMap["variant:"+sku]; !okV {
			if _, okP := skuMap["product:"+sku]; !okP {
				result.Errors = append(result.Errors, RowError{Row: rowNum, Column: "sku", Message: fmt.Sprintf("SKU '%s' not found", sku)})
				hasErr = true
			}
		}
		if channel == "" {
			result.Errors = append(result.Errors, RowError{Row: rowNum, Column: "channel", Message: "Channel is required"})
			hasErr = true
		} else if !knownChannels[strings.ToLower(channel)] {
			result.Warnings = append(result.Warnings, RowWarning{Row: rowNum, Column: "channel", Message: fmt.Sprintf("Unknown channel '%s'", channel)})
			result.WarningCount++
		}
		if priceStr == "" {
			result.Errors = append(result.Errors, RowError{Row: rowNum, Column: "price", Message: "Price is required"})
			hasErr = true
		} else if p, err := strconv.ParseFloat(priceStr, 64); err != nil || p <= 0 {
			result.Errors = append(result.Errors, RowError{Row: rowNum, Column: "price", Message: "Price must be a positive number"})
			hasErr = true
		}
		if hasErr {
			result.ErrorCount++
		} else {
			result.ValidRows++
			result.CreateCount++
		}
	}
	return result, nil
}

func (h *ImportHandler) validatePrices(ctx context.Context, tenantID string, headers []string, rows [][]string) (*ValidationResult, error) {
	colIdx := buildColIdx(headers)
	result := &ValidationResult{TotalRows: len(rows)}
	skuMap := buildSKUMapCtx(ctx, h.repo, tenantID)
	priceFields := []string{"price_ebay", "price_amazon", "price_temu", "rrp", "cost_price", "list_price"}
	for i, row := range rows {
		rowNum := i + 2
		sku := getColVal(row, colIdx, "sku")
		hasErr := false
		if sku == "" {
			result.Errors = append(result.Errors, RowError{Row: rowNum, Column: "sku", Message: "SKU is required"})
			hasErr = true
		} else if _, okV := skuMap["variant:"+sku]; !okV {
			if _, okP := skuMap["product:"+sku]; !okP {
				result.Errors = append(result.Errors, RowError{Row: rowNum, Column: "sku", Message: fmt.Sprintf("SKU '%s' not found", sku)})
				hasErr = true
			}
		}
		for _, f := range priceFields {
			if v := getColVal(row, colIdx, f); v != "" {
				if p, err := strconv.ParseFloat(v, 64); err != nil || p < 0 {
					result.Errors = append(result.Errors, RowError{Row: rowNum, Column: f, Message: "Must be a valid non-negative number"})
					hasErr = true
				}
			}
		}
		if hasErr {
			result.ErrorCount++
		} else {
			result.ValidRows++
			result.UpdateCount++
		}
	}
	return result, nil
}

func (h *ImportHandler) validateInventoryBasic(ctx context.Context, tenantID string, headers []string, rows [][]string) (*ValidationResult, error) {
	colIdx := buildColIdx(headers)
	result := &ValidationResult{TotalRows: len(rows)}
	skuMap := buildSKUMapCtx(ctx, h.repo, tenantID)
	if _, ok := colIdx["sku"]; !ok {
		return nil, fmt.Errorf("missing required column: sku")
	}
	if _, ok := colIdx["quantity"]; !ok {
		return nil, fmt.Errorf("missing required column: quantity")
	}
	for i, row := range rows {
		rowNum := i + 2
		sku := getColVal(row, colIdx, "sku")
		qtyStr := getColVal(row, colIdx, "quantity")
		hasErr := false
		if sku == "" {
			result.Errors = append(result.Errors, RowError{Row: rowNum, Column: "sku", Message: "SKU is required"})
			hasErr = true
		} else if _, okV := skuMap["variant:"+sku]; !okV {
			if _, okP := skuMap["product:"+sku]; !okP {
				result.Errors = append(result.Errors, RowError{Row: rowNum, Column: "sku", Message: fmt.Sprintf("SKU '%s' not found", sku)})
				hasErr = true
			}
		}
		if qtyStr == "" {
			result.Errors = append(result.Errors, RowError{Row: rowNum, Column: "quantity", Message: "Quantity is required"})
			hasErr = true
		} else if q, err := strconv.Atoi(qtyStr); err != nil || q < 0 {
			result.Errors = append(result.Errors, RowError{Row: rowNum, Column: "quantity", Message: "Must be a non-negative integer"})
			hasErr = true
		}
		if hasErr {
			result.ErrorCount++
		} else {
			result.ValidRows++
			result.UpdateCount++
		}
	}
	return result, nil
}

// validateInventoryDelta validates the inventory_delta import type.
// Quantity may be positive (add stock) or negative (remove stock) — any integer is valid.
func (h *ImportHandler) validateInventoryDelta(ctx context.Context, tenantID string, headers []string, rows [][]string) (*ValidationResult, error) {
	colIdx := buildColIdx(headers)
	result := &ValidationResult{TotalRows: len(rows)}
	skuMap := buildSKUMapCtx(ctx, h.repo, tenantID)
	if _, ok := colIdx["sku"]; !ok {
		return nil, fmt.Errorf("missing required column: sku")
	}
	if _, ok := colIdx["quantity"]; !ok {
		return nil, fmt.Errorf("missing required column: quantity")
	}
	for i, row := range rows {
		rowNum := i + 2
		sku := getColVal(row, colIdx, "sku")
		qtyStr := getColVal(row, colIdx, "quantity")
		hasErr := false
		if sku == "" {
			result.Errors = append(result.Errors, RowError{Row: rowNum, Column: "sku", Message: "SKU is required"})
			hasErr = true
		} else if _, okV := skuMap["variant:"+sku]; !okV {
			if _, okP := skuMap["product:"+sku]; !okP {
				result.Errors = append(result.Errors, RowError{Row: rowNum, Column: "sku", Message: fmt.Sprintf("SKU '%s' not found", sku)})
				hasErr = true
			}
		}
		if qtyStr == "" {
			result.Errors = append(result.Errors, RowError{Row: rowNum, Column: "quantity", Message: "Quantity is required"})
			hasErr = true
		} else if _, err := strconv.Atoi(qtyStr); err != nil {
			// Delta allows negative integers; only reject non-numeric values
			result.Errors = append(result.Errors, RowError{Row: rowNum, Column: "quantity", Message: "Must be a valid integer (positive to add stock, negative to subtract)"})
			hasErr = true
		}
		if hasErr {
			result.ErrorCount++
		} else {
			result.ValidRows++
			result.UpdateCount++
		}
	}
	return result, nil
}

func (h *ImportHandler) validateInventoryAdvanced(ctx context.Context, tenantID string, headers []string, rows [][]string) (*ValidationResult, error) {
	colIdx := buildColIdx(headers)
	result := &ValidationResult{TotalRows: len(rows)}
	skuMap := buildSKUMapCtx(ctx, h.repo, tenantID)

	knownWarehouses := map[string]bool{}
	fsIter := h.client.Collection(fmt.Sprintf("tenants/%s/fulfilment_sources", tenantID)).Documents(ctx)
	for {
		doc, err := fsIter.Next()
		if err != nil {
			break
		}
		var fs models.FulfilmentSource
		if err := doc.DataTo(&fs); err == nil {
			knownWarehouses[strings.ToLower(fs.Name)] = true
			knownWarehouses[strings.ToLower(fs.Code)] = true
		}
	}

	knownLocations := map[string]bool{}
	locIter := h.client.CollectionGroup("warehouse_locations").Where("tenant_id", "==", tenantID).Documents(ctx)
	for {
		doc, err := locIter.Next()
		if err != nil {
			break
		}
		var loc struct {
			Path string `firestore:"path"`
			Name string `firestore:"name"`
		}
		if err := doc.DataTo(&loc); err == nil {
			knownLocations[strings.ToLower(loc.Path)] = true
			knownLocations[strings.ToLower(loc.Name)] = true
		}
	}

	unknownLocSet := map[string]bool{}
	for i, row := range rows {
		rowNum := i + 2
		sku := getColVal(row, colIdx, "sku")
		warehouse := getColVal(row, colIdx, "warehouse")
		locationPath := getColVal(row, colIdx, "location_path")
		qtyStr := getColVal(row, colIdx, "quantity")
		hasErr := false
		if sku == "" {
			result.Errors = append(result.Errors, RowError{Row: rowNum, Column: "sku", Message: "SKU is required"})
			hasErr = true
		} else if _, okV := skuMap["variant:"+sku]; !okV {
			if _, okP := skuMap["product:"+sku]; !okP {
				result.Errors = append(result.Errors, RowError{Row: rowNum, Column: "sku", Message: fmt.Sprintf("SKU '%s' not found", sku)})
				hasErr = true
			}
		}
		if warehouse == "" {
			result.Errors = append(result.Errors, RowError{Row: rowNum, Column: "warehouse", Message: "Warehouse is required"})
			hasErr = true
		} else if !knownWarehouses[strings.ToLower(warehouse)] {
			result.Warnings = append(result.Warnings, RowWarning{Row: rowNum, Column: "warehouse", Message: fmt.Sprintf("Warehouse '%s' not found", warehouse)})
			result.WarningCount++
		}
		if locationPath == "" {
			result.Errors = append(result.Errors, RowError{Row: rowNum, Column: "location_path", Message: "Location path is required"})
			hasErr = true
		} else if !knownLocations[strings.ToLower(locationPath)] {
			result.Warnings = append(result.Warnings, RowWarning{Row: rowNum, Column: "location_path", Message: fmt.Sprintf("Location '%s' not found — will be created", locationPath)})
			result.WarningCount++
			unknownLocSet[locationPath] = true
		}
		if qtyStr == "" {
			result.Errors = append(result.Errors, RowError{Row: rowNum, Column: "quantity", Message: "Quantity is required"})
			hasErr = true
		} else if q, err := strconv.Atoi(qtyStr); err != nil || q < 0 {
			result.Errors = append(result.Errors, RowError{Row: rowNum, Column: "quantity", Message: "Must be a non-negative integer"})
			hasErr = true
		}
		if hasErr {
			result.ErrorCount++
		} else {
			result.ValidRows++
			result.UpdateCount++
		}
	}
	for loc := range unknownLocSet {
		result.UnknownLocations = append(result.UnknownLocations, loc)
	}
	return result, nil
}

// ─── Background processing ────────────────────────────────────────────────────

func (h *ImportHandler) processImport(ctx context.Context, job *ImportJob, importType, tenantID string, headers []string, rows [][]string, unknownLocations []string, filename string) {
	jobRef := h.client.Doc(fmt.Sprintf("tenants/%s/import_jobs_csv/%s", tenantID, job.JobID))
	updateStatus := func(status ImportJobStatus, processed, created, updated, failed int, errs []RowError) {
		jobRef.Update(ctx, []firestore.Update{
			{Path: "status", Value: status}, {Path: "processed_rows", Value: processed},
			{Path: "created_count", Value: created}, {Path: "updated_count", Value: updated},
			{Path: "failed_count", Value: failed}, {Path: "error_report", Value: errs},
			{Path: "updated_at", Value: time.Now()},
		})
	}
	updateStatus(JobStatusProcessing, 0, 0, 0, 0, nil)
	colIdx := buildColIdx(headers)
	skuMap := buildSKUMapCtx(ctx, h.repo, tenantID)
	created, updated, failed := 0, 0, 0
	var errorReport []RowError

	switch importType {
	case "products":
		for i, row := range rows {
			rowNum := i + 2
			sku := getColVal(row, colIdx, "sku")
			if sku == "" {
				failed++
				continue
			}
			if _, okV := skuMap["variant:"+sku]; okV {
				updates := map[string]interface{}{"updated_at": time.Now()}
				if t := getColVal(row, colIdx, "title"); t != "" {
					updates["title"] = t
				}
				if s := getColVal(row, colIdx, "status"); s != "" {
					updates["status"] = s
				}
				if varID := skuMap["variant:"+sku]; varID != "" {
					if err := h.repo.UpdateVariant(ctx, tenantID, varID, updates); err != nil {
						errorReport = append(errorReport, RowError{Row: rowNum, Column: "sku", Message: err.Error()})
						failed++
					} else {
						updated++
					}
				}
			} else if _, okP := skuMap["product:"+sku]; okP {
				updates := map[string]interface{}{"updated_at": time.Now()}
				if t := getColVal(row, colIdx, "title"); t != "" {
					updates["title"] = t
				}
				if s := getColVal(row, colIdx, "status"); s != "" {
					updates["status"] = s
				}
				if b := getColVal(row, colIdx, "brand"); b != "" {
					updates["brand"] = b
				}
				if desc := getColVal(row, colIdx, "description"); desc != "" {
					updates["description"] = desc
				}
				// Freeform attributes from attribute_N_name / attribute_N_value pairs
				for n := 1; n <= 25; n++ {
					nameKey := fmt.Sprintf("attribute_%d_name", n)
					valKey := fmt.Sprintf("attribute_%d_value", n)
					attrName := getColVal(row, colIdx, nameKey)
					attrVal := getColVal(row, colIdx, valKey)
					if attrName != "" && attrVal != "" {
						updates["attributes."+attrName] = attrVal
					}
				}
				if prodID := skuMap["product:"+sku]; prodID != "" {
					if err := h.productService.UpdateProduct(ctx, tenantID, prodID, updates); err != nil {
						errorReport = append(errorReport, RowError{Row: rowNum, Column: "sku", Message: err.Error()})
						failed++
					} else {
						updated++
					}
				}
			} else {
				title := getColVal(row, colIdx, "title")
				ptype := getColVal(row, colIdx, "product_type")
				if ptype == "" {
					ptype = "simple"
				}
				status := getColVal(row, colIdx, "status")
				if status == "" {
					status = "draft"
				}
				attrs := map[string]interface{}{"source_sku": sku}

				// Freeform attributes from attribute_N_name / attribute_N_value pairs
				for n := 1; n <= 25; n++ {
					nameKey := fmt.Sprintf("attribute_%d_name", n)
					valKey := fmt.Sprintf("attribute_%d_value", n)
					attrName := getColVal(row, colIdx, nameKey)
					attrVal := getColVal(row, colIdx, valKey)
					if attrName != "" && attrVal != "" {
						attrs[attrName] = attrVal
					}
				}

				p := &models.Product{
					ProductID: uuid.New().String(), TenantID: tenantID, Title: title,
					ProductType: ptype, Status: status, Attributes: attrs,
					CreatedAt: time.Now(), UpdatedAt: time.Now(),
				}
				if brand := getColVal(row, colIdx, "brand"); brand != "" {
					p.Brand = &brand
				}
				if desc := getColVal(row, colIdx, "description"); desc != "" {
					p.Description = &desc
				}
				if subtitle := getColVal(row, colIdx, "subtitle"); subtitle != "" {
					p.Subtitle = &subtitle
				}
				if cats := getColVal(row, colIdx, "categories"); cats != "" {
					p.CategoryIDs = strings.Split(cats, "|")
				}
				if tags := getColVal(row, colIdx, "tags"); tags != "" {
					p.Tags = strings.Split(tags, "|")
				}

				// Images
				for n := 1; n <= 5; n++ {
					imgCol := fmt.Sprintf("image_%d", n)
					if url := getColVal(row, colIdx, imgCol); url != "" {
						role := "gallery"
						if n == 1 {
							role = "primary_image"
						}
						p.Assets = append(p.Assets, models.ProductAsset{
							AssetID: uuid.New().String(), URL: url, Role: role, SortOrder: n - 1,
						})
					}
				}

				// Bundle components
				if ptype == "bundle" {
					if comp := getColVal(row, colIdx, "bundle_component_skus"); comp != "" {
						for ci, part := range strings.Split(comp, "|") {
							pieces := strings.Split(part, ":")
							if len(pieces) == 2 {
								qty, _ := strconv.Atoi(pieces[1])
								p.BundleComponents = append(p.BundleComponents, models.BundleComponent{
									ComponentID: uuid.New().String(), ProductID: pieces[0], Quantity: qty, IsRequired: true, SortOrder: ci,
								})
							}
						}
					}
				}

				if err := h.productService.CreateProduct(ctx, p); err != nil {
					errorReport = append(errorReport, RowError{Row: rowNum, Column: "sku", Message: err.Error()})
					failed++
				} else {
					created++
					skuMap["product:"+sku] = p.ProductID
				}
			}
			if (i+1)%50 == 0 {
				updateStatus(JobStatusProcessing, i+1, created, updated, failed, nil)
			}
		}

	case "prices":
		priceChannelMap := map[string]string{"price_ebay": "ebay", "price_amazon": "amazon", "price_temu": "temu"}
		for i, row := range rows {
			sku := getColVal(row, colIdx, "sku")
			if sku == "" {
				failed++
				continue
			}
			updates := map[string]interface{}{"updated_at": time.Now()}
			if rrp := getColVal(row, colIdx, "rrp"); rrp != "" {
				if f, err := strconv.ParseFloat(rrp, 64); err == nil {
					updates["pricing.rrp"] = models.Money{Amount: f, Currency: "GBP"}
				}
			}
			if cost := getColVal(row, colIdx, "cost_price"); cost != "" {
				if f, err := strconv.ParseFloat(cost, 64); err == nil {
					updates["pricing.cost"] = models.Money{Amount: f, Currency: "GBP"}
				}
			}
			for col := range priceChannelMap {
				if v := getColVal(row, colIdx, col); v != "" {
					if f, err := strconv.ParseFloat(v, 64); err == nil {
						updates["attributes."+col] = f
					}
				}
			}
			if varID, ok := skuMap["variant:"+sku]; ok {
				if err := h.repo.UpdateVariant(ctx, tenantID, varID, updates); err != nil {
					errorReport = append(errorReport, RowError{Row: i + 2, Column: "sku", Message: err.Error()})
					failed++
				} else {
					updated++
				}
			} else if prodID, ok := skuMap["product:"+sku]; ok {
				if err := h.productService.UpdateProduct(ctx, tenantID, prodID, updates); err != nil {
					errorReport = append(errorReport, RowError{Row: i + 2, Column: "sku", Message: err.Error()})
					failed++
				} else {
					updated++
				}
			} else {
				failed++
			}
		}

	case "inventory_basic":
		sourceIter := h.client.Collection(fmt.Sprintf("tenants/%s/fulfilment_sources", tenantID)).
			Where("default", "==", true).Limit(1).Documents(ctx)
		sourceDoc, err := sourceIter.Next()
		defaultSourceID := ""
		if err == nil {
			defaultSourceID = sourceDoc.Ref.ID
		}
		for i, row := range rows {
			sku := getColVal(row, colIdx, "sku")
			qtyStr := getColVal(row, colIdx, "quantity")
			qty, _ := strconv.Atoi(qtyStr)
			invIter := h.client.Collection(fmt.Sprintf("tenants/%s/inventory", tenantID)).Where("sku", "==", sku).Limit(1).Documents(ctx)
			invDoc, fetchErr := invIter.Next()
			if fetchErr != nil {
				h.client.Collection(fmt.Sprintf("tenants/%s/inventory", tenantID)).Doc(uuid.New().String()).Set(ctx, map[string]interface{}{
					"sku": sku, "total_on_hand": qty, "total_available": qty,
					"total_reserved": 0, "source_id": defaultSourceID, "updated_at": time.Now(),
				})
				created++
			} else {
				invDoc.Ref.Update(ctx, []firestore.Update{
					{Path: "total_on_hand", Value: qty}, {Path: "total_available", Value: qty}, {Path: "updated_at", Value: time.Now()},
				})
				updated++
			}
			adjustmentReason := fmt.Sprintf("CSV inventory import %s %s", filename, time.Now().Format("2006-01-02"))
			h.client.Collection(fmt.Sprintf("tenants/%s/inventory_adjustments", tenantID)).Doc(uuid.New().String()).Set(ctx, map[string]interface{}{
				"sku": sku, "quantity": qty, "reason": adjustmentReason,
				"source_id": defaultSourceID, "type": "csv_import", "created_at": time.Now(),
			})
			if (i+1)%50 == 0 {
				updateStatus(JobStatusProcessing, i+1, created, updated, failed, nil)
			}
		}

	case "inventory_delta":
		// Load default source
		sourceIter := h.client.Collection(fmt.Sprintf("tenants/%s/fulfilment_sources", tenantID)).
			Where("default", "==", true).Limit(1).Documents(ctx)
		sourceDoc, err := sourceIter.Next()
		defaultSourceID := ""
		if err == nil {
			defaultSourceID = sourceDoc.Ref.ID
		}
		for i, row := range rows {
			sku := getColVal(row, colIdx, "sku")
			qtyStr := getColVal(row, colIdx, "quantity")
			delta, _ := strconv.Atoi(qtyStr)

			invIter := h.client.Collection(fmt.Sprintf("tenants/%s/inventory", tenantID)).Where("sku", "==", sku).Limit(1).Documents(ctx)
			invDoc, fetchErr := invIter.Next()
			if fetchErr != nil {
				// No existing doc — use delta as initial value, floor at 0
				newQty := delta
				if newQty < 0 {
					newQty = 0
				}
				h.client.Collection(fmt.Sprintf("tenants/%s/inventory", tenantID)).Doc(uuid.New().String()).Set(ctx, map[string]interface{}{
					"sku": sku, "total_on_hand": newQty, "total_available": newQty,
					"total_reserved": 0, "source_id": defaultSourceID, "updated_at": time.Now(),
				})
				created++
			} else {
				// Read current quantity, apply delta
				data := invDoc.Data()
				current := 0
				if v, ok := data["total_on_hand"]; ok {
					switch t := v.(type) {
					case int64:
						current = int(t)
					case float64:
						current = int(t)
					case int:
						current = t
					}
				}
				newQty := current + delta
				if newQty < 0 {
					newQty = 0
				}
				invDoc.Ref.Update(ctx, []firestore.Update{
					{Path: "total_on_hand", Value: newQty}, {Path: "total_available", Value: newQty}, {Path: "updated_at", Value: time.Now()},
				})
				updated++
			}
			adjustmentReason := fmt.Sprintf("CSV delta import %s %s", filename, time.Now().Format("2006-01-02"))
			h.client.Collection(fmt.Sprintf("tenants/%s/inventory_adjustments", tenantID)).Doc(uuid.New().String()).Set(ctx, map[string]interface{}{
				"sku": sku, "quantity": delta, "reason": adjustmentReason,
				"source_id": defaultSourceID, "type": "csv_delta_import", "created_at": time.Now(),
			})
			if (i+1)%50 == 0 {
				updateStatus(JobStatusProcessing, i+1, created, updated, failed, nil)
			}
		}

	case "inventory_advanced":
		for _, locPath := range unknownLocations {
			parts := strings.Split(locPath, "/")
			locName := parts[len(parts)-1]
			h.client.Collection(fmt.Sprintf("tenants/%s/warehouse_locations", tenantID)).Doc(uuid.New().String()).Set(ctx, map[string]interface{}{
				"name": locName, "path": locPath, "tenant_id": tenantID, "created_at": time.Now(), "auto_created": true,
			})
		}
		for i, row := range rows {
			sku := getColVal(row, colIdx, "sku")
			warehouse := getColVal(row, colIdx, "warehouse")
			locationPath := getColVal(row, colIdx, "location_path")
			qtyStr := getColVal(row, colIdx, "quantity")
			qty, _ := strconv.Atoi(qtyStr)
			adjustmentReason := fmt.Sprintf("CSV advanced inventory import %s %s", filename, time.Now().Format("2006-01-02"))
			h.client.Collection(fmt.Sprintf("tenants/%s/inventory_adjustments", tenantID)).Doc(uuid.New().String()).Set(ctx, map[string]interface{}{
				"sku": sku, "quantity": qty, "warehouse": warehouse, "location_path": locationPath,
				"reason": adjustmentReason, "type": "csv_advanced_import", "created_at": time.Now(),
			})
			updated++
			if (i+1)%50 == 0 {
				updateStatus(JobStatusProcessing, i+1, created, updated, failed, nil)
			}
		}

	case "orders":
		skuMap := buildSKUMapCtx(ctx, h.repo, tenantID)
		channelSKUMap := h.buildChannelSKUMap(ctx, tenantID)
		c, u, f, errs := h.processOrderImport(ctx, tenantID, colIdx, rows, skuMap, channelSKUMap)
		created += c
		updated += u
		failed += f
		errorReport = append(errorReport, errs...)

	case "binrack_zone":
		// CSV: binrack_name,zone_name
		for i, row := range rows {
			rowNum := i + 2
			binrackName := getColVal(row, colIdx, "binrack_name")
			zoneName := getColVal(row, colIdx, "zone_name")
			if binrackName == "" || zoneName == "" {
				errorReport = append(errorReport, RowError{Row: rowNum, Column: "binrack_name", Message: "binrack_name and zone_name required"})
				failed++
				continue
			}
			// Find binrack by name
			binIter := h.client.Collection("tenants").Doc(tenantID).Collection("binracks").
				Where("name", "==", binrackName).Limit(1).Documents(ctx)
			binDoc, binErr := binIter.Next()
			binIter.Stop()
			if binErr != nil {
				errorReport = append(errorReport, RowError{Row: rowNum, Column: "binrack_name", Message: "binrack not found: " + binrackName})
				failed++
				continue
			}
			// Find zone by name
			zoneIter := h.client.Collection("tenants").Doc(tenantID).Collection("warehouse_zones").
				Where("name", "==", zoneName).Limit(1).Documents(ctx)
			zoneDoc, zoneErr := zoneIter.Next()
			zoneIter.Stop()
			if zoneErr != nil {
				errorReport = append(errorReport, RowError{Row: rowNum, Column: "zone_name", Message: "zone not found: " + zoneName})
				failed++
				continue
			}
			zoneData := zoneDoc.Data()
			zoneID, _ := zoneData["zone_id"].(string)
			if _, err := binDoc.Ref.Update(ctx, []firestore.Update{
				{Path: "zone_id", Value: zoneID},
				{Path: "updated_at", Value: time.Now()},
			}); err != nil {
				failed++
			} else {
				updated++
			}
		}

	case "binrack_create_update":
		// CSV: name,barcode,binrack_type,zone_name,aisle,section,level,bin_number,capacity
		for i, row := range rows {
			rowNum := i + 2
			name := getColVal(row, colIdx, "name")
			if name == "" {
				errorReport = append(errorReport, RowError{Row: rowNum, Column: "name", Message: "name required"})
				failed++
				continue
			}
			// Check if binrack exists
			existIter := h.client.Collection("tenants").Doc(tenantID).Collection("binracks").
				Where("name", "==", name).Limit(1).Documents(ctx)
			existDoc, existErr := existIter.Next()
			existIter.Stop()

			cap := 0
			if capStr := getColVal(row, colIdx, "capacity"); capStr != "" {
				fmt.Sscanf(capStr, "%d", &cap)
			}

			if existErr != nil {
				// Create new
				newID := "bin_" + uuid.New().String()
				_, err := h.client.Collection("tenants").Doc(tenantID).Collection("binracks").Doc(newID).Set(ctx, map[string]interface{}{
					"binrack_id":   newID,
					"tenant_id":    tenantID,
					"name":         name,
					"barcode":      getColVal(row, colIdx, "barcode"),
					"binrack_type": getColVal(row, colIdx, "binrack_type"),
					"aisle":        getColVal(row, colIdx, "aisle"),
					"section":      getColVal(row, colIdx, "section"),
					"level":        getColVal(row, colIdx, "level"),
					"bin_number":   getColVal(row, colIdx, "bin_number"),
					"capacity":     cap,
					"status":       "available",
					"created_at":   time.Now(),
					"updated_at":   time.Now(),
				})
				if err != nil { failed++ } else { created++ }
			} else {
				// Update existing
				updates := []firestore.Update{{Path: "updated_at", Value: time.Now()}}
				if v := getColVal(row, colIdx, "barcode"); v != "" { updates = append(updates, firestore.Update{Path: "barcode", Value: v}) }
				if v := getColVal(row, colIdx, "binrack_type"); v != "" { updates = append(updates, firestore.Update{Path: "binrack_type", Value: v}) }
				if v := getColVal(row, colIdx, "aisle"); v != "" { updates = append(updates, firestore.Update{Path: "aisle", Value: v}) }
				if v := getColVal(row, colIdx, "section"); v != "" { updates = append(updates, firestore.Update{Path: "section", Value: v}) }
				if v := getColVal(row, colIdx, "level"); v != "" { updates = append(updates, firestore.Update{Path: "level", Value: v}) }
				if v := getColVal(row, colIdx, "bin_number"); v != "" { updates = append(updates, firestore.Update{Path: "bin_number", Value: v}) }
				if cap > 0 { updates = append(updates, firestore.Update{Path: "capacity", Value: cap}) }
				if _, err := existDoc.Ref.Update(ctx, updates); err != nil { failed++ } else { updated++ }
			}
		}

	case "binrack_item_restriction":
		// CSV: binrack_name,sku
		for i, row := range rows {
			rowNum := i + 2
			binrackName := getColVal(row, colIdx, "binrack_name")
			sku := getColVal(row, colIdx, "sku")
			if binrackName == "" || sku == "" {
				errorReport = append(errorReport, RowError{Row: rowNum, Column: "binrack_name", Message: "binrack_name and sku required"})
				failed++
				continue
			}
			binIter := h.client.Collection("tenants").Doc(tenantID).Collection("binracks").
				Where("name", "==", binrackName).Limit(1).Documents(ctx)
			binDoc, binErr := binIter.Next()
			binIter.Stop()
			if binErr != nil {
				errorReport = append(errorReport, RowError{Row: rowNum, Column: "binrack_name", Message: "binrack not found: " + binrackName})
				failed++
				continue
			}
			data := binDoc.Data()
			existing, _ := data["item_restrictions"].([]interface{})
			newList := make([]string, 0, len(existing)+1)
			for _, v := range existing {
				if s, ok := v.(string); ok { newList = append(newList, s) }
			}
			// Only append if not already present
			alreadyHas := false
			for _, s := range newList {
				if s == sku { alreadyHas = true; break }
			}
			if !alreadyHas { newList = append(newList, sku) }
			if _, err := binDoc.Ref.Update(ctx, []firestore.Update{
				{Path: "item_restrictions", Value: newList},
				{Path: "updated_at", Value: time.Now()},
			}); err != nil { failed++ } else { updated++ }
		}

	case "binrack_storage_group":
		// CSV: binrack_name,storage_group_name
		for i, row := range rows {
			rowNum := i + 2
			binrackName := getColVal(row, colIdx, "binrack_name")
			groupName := getColVal(row, colIdx, "storage_group_name")
			if binrackName == "" || groupName == "" {
				errorReport = append(errorReport, RowError{Row: rowNum, Column: "binrack_name", Message: "binrack_name and storage_group_name required"})
				failed++
				continue
			}
			binIter := h.client.Collection("tenants").Doc(tenantID).Collection("binracks").
				Where("name", "==", binrackName).Limit(1).Documents(ctx)
			binDoc, binErr := binIter.Next()
			binIter.Stop()
			if binErr != nil {
				errorReport = append(errorReport, RowError{Row: rowNum, Column: "binrack_name", Message: "binrack not found: " + binrackName})
				failed++
				continue
			}
			groupIter := h.client.Collection("tenants").Doc(tenantID).Collection("storage_groups").
				Where("name", "==", groupName).Limit(1).Documents(ctx)
			groupDoc, groupErr := groupIter.Next()
			groupIter.Stop()
			if groupErr != nil {
				errorReport = append(errorReport, RowError{Row: rowNum, Column: "storage_group_name", Message: "storage group not found: " + groupName})
				failed++
				continue
			}
			groupData := groupDoc.Data()
			groupID, _ := groupData["id"].(string)
			if groupID == "" { groupID, _ = groupData["group_id"].(string) }
			if _, err := binDoc.Ref.Update(ctx, []firestore.Update{
				{Path: "storage_group_id", Value: groupID},
				{Path: "updated_at", Value: time.Now()},
			}); err != nil { failed++ } else { updated++ }
		}

	case "stock_migration":
		// CSV: sku,warehouse_id,binrack_name,quantity
		// Requires confirm_migration=true in request body
		// Note: confirmMigration flag checked before processImport is called
		for i, row := range rows {
			rowNum := i + 2
			sku := getColVal(row, colIdx, "sku")
			warehouseID := getColVal(row, colIdx, "warehouse_id")
			binrackName := getColVal(row, colIdx, "binrack_name")
			qtyStr := getColVal(row, colIdx, "quantity")
			if sku == "" || qtyStr == "" {
				errorReport = append(errorReport, RowError{Row: rowNum, Column: "sku", Message: "sku and quantity required"})
				failed++
				continue
			}
			qty := 0
			fmt.Sscanf(qtyStr, "%d", &qty)
			binrackID := ""
			if binrackName != "" {
				binIter := h.client.Collection("tenants").Doc(tenantID).Collection("binracks").
					Where("name", "==", binrackName).Limit(1).Documents(ctx)
				binDoc, binErr := binIter.Next()
				binIter.Stop()
				if binErr == nil {
					d := binDoc.Data()
					binrackID, _ = d["binrack_id"].(string)
				}
			}
			// Upsert inventory record (destructive overwrite)
			invID := "inv_" + tenantID + "_" + sku + "_" + warehouseID
			if _, err := h.client.Collection("tenants").Doc(tenantID).Collection("inventory").Doc(invID).Set(ctx, map[string]interface{}{
				"sku":         sku,
				"warehouse_id": warehouseID,
				"binrack_id":  binrackID,
				"binrack_name": binrackName,
				"quantity":    qty,
				"updated_at":  time.Now(),
			}); err != nil { failed++ } else { updated++ }
		}
	}

	finalStatus := JobStatusDone
	if failed > 0 && created == 0 && updated == 0 {
		finalStatus = JobStatusFailed
	}
	updateStatus(finalStatus, len(rows), created, updated, failed, errorReport)
}

// ─── Shared helpers ───────────────────────────────────────────────────────────

func parseUploadFileWithConfig(c *gin.Context, cfg FileConfig) (rows [][]string, headers []string, filename string, err error) {
	var file multipart.File
	var fileHeader *multipart.FileHeader
	file, fileHeader, err = c.Request.FormFile("file")
	if err != nil {
		return nil, nil, "", fmt.Errorf("no file uploaded: %w", err)
	}
	defer file.Close()
	filename = fileHeader.Filename

	var reader io.Reader = file
	// Determine delimiter rune
	delim := ','
	switch cfg.Delimiter {
	case "\t", "tab":
		delim = '\t'
	case ";":
		delim = ';'
	case "|":
		delim = '|'
	}

	csvReader := csv.NewReader(reader)
	csvReader.Comma = delim
	csvReader.LazyQuotes = true
	csvReader.TrimLeadingSpace = true

	all, err := csvReader.ReadAll()
	if err != nil {
		return nil, nil, filename, fmt.Errorf("failed to parse CSV: %w", err)
	}

	if !cfg.HasHeaderRow {
		if len(all) == 0 {
			return nil, nil, filename, fmt.Errorf("file is empty")
		}
		numCols := len(all[0])
		headers = make([]string, numCols)
		for i := range headers {
			headers[i] = fmt.Sprintf("col_%d", i+1)
		}
		return all, headers, filename, nil
	}

	if len(all) < 2 {
		return nil, nil, filename, fmt.Errorf("file must have a header row and at least one data row")
	}
	rawHeaders := all[0]
	for i := range rawHeaders {
		rawHeaders[i] = strings.TrimSpace(strings.ToLower(rawHeaders[i]))
	}
	return all[1:], rawHeaders, filename, nil
}

// parseColumnMapping reads the column_mapping JSON field from the form body.
// Format: {"targetField": "fileHeader", ...}
func parseColumnMapping(c *gin.Context) map[string]string {
	raw := c.PostForm("column_mapping")
	if raw == "" {
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil
	}
	return m
}

// applyColumnMapping rewrites headers using the user-supplied mapping (targetField → fileHeader).
func applyColumnMapping(headers []string, mapping map[string]string) []string {
	// Invert: fileHeader → targetField
	inverse := map[string]string{}
	for tf, fh := range mapping {
		inverse[strings.ToLower(fh)] = tf
	}
	result := make([]string, len(headers))
	copy(result, headers)
	for i, h := range result {
		if tf, ok := inverse[strings.ToLower(h)]; ok {
			result[i] = tf
		}
	}
	return result
}

// parseUploadFile is kept for backward compatibility (uses default CSV settings)
func parseUploadFile(c *gin.Context) (rows [][]string, headers []string, filename string, err error) {
	return parseUploadFileWithConfig(c, FileConfig{Delimiter: ",", Encoding: "utf-8", HasHeaderRow: true})
}

func buildColIdx(headers []string) map[string]int {
	idx := map[string]int{}
	for i, h := range headers {
		idx[h] = i
	}
	return idx
}

func getColVal(row []string, colIdx map[string]int, col string) string {
	if i, ok := colIdx[col]; ok && i < len(row) {
		return strings.TrimSpace(row[i])
	}
	return ""
}

func buildSKUMapCtx(ctx context.Context, repo *repository.FirestoreRepository, tenantID string) map[string]string {
	skuMap := map[string]string{}
	products, _, _ := repo.ListProducts(ctx, tenantID, map[string]interface{}{}, 0, 0)
	for _, p := range products {
		if p.Attributes != nil {
			if s, ok := p.Attributes["source_sku"].(string); ok && s != "" {
				skuMap["product:"+s] = p.ProductID
			}
			if s, ok := p.Attributes["sku"].(string); ok && s != "" {
				skuMap["product:"+s] = p.ProductID
			}
		}
	}
	variants, _, _ := repo.ListVariants(ctx, tenantID, map[string]interface{}{}, 0, 0)
	for _, v := range variants {
		skuMap["variant:"+v.SKU] = v.VariantID
	}
	return skuMap
}

var _ = json.Marshal
var _ = io.EOF

// ─── Order CSV Import ─────────────────────────────────────────────────────────

func (h *ImportHandler) validateOrders(ctx context.Context, tenantID string, headers []string, rows [][]string) (*ValidationResult, error) {
	colIdx := buildColIdx(headers)
	result := &ValidationResult{TotalRows: len(rows)}
	for _, col := range []string{"order_reference", "sku", "quantity"} {
		if _, ok := colIdx[col]; !ok {
			return nil, fmt.Errorf("missing required column: %s", col)
		}
	}
	skuMap := buildSKUMapCtx(ctx, h.repo, tenantID)
	channelSKUMap := h.buildChannelSKUMap(ctx, tenantID)
	for i, row := range rows {
		rowNum := i + 2
		ref := getColVal(row, colIdx, "order_reference")
		sku := getColVal(row, colIdx, "sku")
		qtyStr := getColVal(row, colIdx, "quantity")
		hasErr := false
		if ref == "" {
			result.Errors = append(result.Errors, RowError{Row: rowNum, Column: "order_reference", Message: "Order reference is required"})
			hasErr = true
		}
		if sku == "" {
			result.Errors = append(result.Errors, RowError{Row: rowNum, Column: "sku", Message: "SKU is required"})
			hasErr = true
		} else if !h.matchSKU(sku, skuMap, channelSKUMap) {
			result.Warnings = append(result.Warnings, RowWarning{Row: rowNum, Column: "sku", Message: fmt.Sprintf("SKU '%s' not found — order will be imported as unlinked", sku)})
			result.WarningCount++
		}
		if qtyStr == "" {
			result.Errors = append(result.Errors, RowError{Row: rowNum, Column: "quantity", Message: "Quantity is required"})
			hasErr = true
		} else if q, err := strconv.Atoi(qtyStr); err != nil || q <= 0 {
			result.Errors = append(result.Errors, RowError{Row: rowNum, Column: "quantity", Message: "Quantity must be a positive integer"})
			hasErr = true
		}
		if hasErr {
			result.ErrorCount++
		} else {
			result.ValidRows++
			result.CreateCount++
		}
	}
	return result, nil
}

func (h *ImportHandler) matchSKU(sku string, skuMap map[string]string, channelSKUMap map[string]string) bool {
	if _, ok := skuMap["variant:"+sku]; ok {
		return true
	}
	if _, ok := skuMap["product:"+sku]; ok {
		return true
	}
	skuLower := strings.ToLower(sku)
	for k := range skuMap {
		knownSKU := strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(k, "variant:"), "product:"))
		if strings.Contains(knownSKU, skuLower) || strings.Contains(skuLower, knownSKU) {
			return true
		}
	}
	if _, ok := channelSKUMap[strings.ToLower(sku)]; ok {
		return true
	}
	return false
}

func (h *ImportHandler) buildChannelSKUMap(ctx context.Context, tenantID string) map[string]string {
	m := map[string]string{}
	iter := h.client.Collection(fmt.Sprintf("tenants/%s/listings", tenantID)).Documents(ctx)
	for {
		doc, err := iter.Next()
		if err != nil {
			break
		}
		data := doc.Data()
		if channelSKU, ok := data["channel_sku"].(string); ok && channelSKU != "" {
			if internalSKU, ok := data["sku"].(string); ok && internalSKU != "" {
				m[strings.ToLower(channelSKU)] = internalSKU
			}
		}
	}
	return m
}

func (h *ImportHandler) processOrderImport(ctx context.Context, tenantID string, colIdx map[string]int, rows [][]string, skuMap map[string]string, channelSKUMap map[string]string) (int, int, int, []RowError) {
	created, updated, failed := 0, 0, 0
	var errorReport []RowError
	type rowGroup struct{ rows [][]string }
	orderGroups := map[string]*rowGroup{}
	orderRefs := []string{}
	for _, row := range rows {
		ref := getColVal(row, colIdx, "order_reference")
		if ref == "" {
			failed++
			continue
		}
		if _, exists := orderGroups[ref]; !exists {
			orderGroups[ref] = &rowGroup{}
			orderRefs = append(orderRefs, ref)
		}
		orderGroups[ref].rows = append(orderGroups[ref].rows, row)
	}
	for _, ref := range orderRefs {
		group := orderGroups[ref]
		firstRow := group.rows[0]
		receivedDate := getColVal(firstRow, colIdx, "received_date")
		despatchByDate := getColVal(firstRow, colIdx, "despatch_by_date")
		shippingService := getColVal(firstRow, colIdx, "shipping_service")
		paymentStatus := getColVal(firstRow, colIdx, "payment_status")
		currency := getColVal(firstRow, colIdx, "currency")
		if currency == "" {
			currency = "GBP"
		}
		shipAddr := models.Address{
			Name: getColVal(firstRow, colIdx, "ship_name"), AddressLine1: getColVal(firstRow, colIdx, "ship_address1"),
			AddressLine2: getColVal(firstRow, colIdx, "ship_address2"), City: getColVal(firstRow, colIdx, "ship_city"),
			PostalCode: getColVal(firstRow, colIdx, "ship_postcode"), Country: getColVal(firstRow, colIdx, "ship_country"),
		}
		if shipAddr.Country == "" {
			shipAddr.Country = "GB"
		}
		billAddr := &models.Address{
			Name: getColVal(firstRow, colIdx, "bill_name"), AddressLine1: getColVal(firstRow, colIdx, "bill_address1"),
			City: getColVal(firstRow, colIdx, "bill_city"), PostalCode: getColVal(firstRow, colIdx, "bill_postcode"),
			Country: getColVal(firstRow, colIdx, "bill_country"),
		}
		if billAddr.Name == "" && billAddr.AddressLine1 == "" {
			billAddr = nil
		}
		var lines []models.OrderLine
		var subtotalAmt, taxAmt float64
		for _, row := range group.rows {
			sku := getColVal(row, colIdx, "sku")
			qty, _ := strconv.Atoi(getColVal(row, colIdx, "quantity"))
			price, _ := strconv.ParseFloat(getColVal(row, colIdx, "unit_price"), 64)
			resolvedSKU := sku
			if mapped, ok := channelSKUMap[strings.ToLower(sku)]; ok {
				resolvedSKU = mapped
			}
			lineTax, _ := strconv.ParseFloat(getColVal(row, colIdx, "tax_amount"), 64)
			line := models.OrderLine{
				LineID: uuid.New().String(), SKU: resolvedSKU, Title: resolvedSKU, Quantity: qty,
				UnitPrice: models.Money{Amount: price, Currency: currency},
				LineTotal: models.Money{Amount: price * float64(qty), Currency: currency},
				Tax:       models.Money{Amount: lineTax, Currency: currency}, Status: "pending",
			}
			if varID, ok := skuMap["variant:"+resolvedSKU]; ok {
				line.VariantID = varID
			} else if prodID, ok := skuMap["product:"+resolvedSKU]; ok {
				line.ProductID = prodID
			}
			lines = append(lines, line)
			subtotalAmt += line.LineTotal.Amount
			taxAmt += lineTax
		}
		if totalTax, err := strconv.ParseFloat(getColVal(firstRow, colIdx, "tax_amount"), 64); err == nil && len(group.rows) == 1 {
			taxAmt = totalTax
		}
		orderDate := receivedDate
		if orderDate == "" {
			orderDate = time.Now().Format("2006-01-02")
		}
		now := time.Now().Format(time.RFC3339)
		order := models.Order{
			OrderID: uuid.New().String(), TenantID: tenantID, ExternalOrderID: ref,
			Channel: "csv_import", Status: "imported", ShippingAddress: shipAddr, BillingAddress: billAddr,
			PaymentStatus: paymentStatus, ShippingService: shippingService,
			OrderDate: orderDate, DespatchByDate: despatchByDate,
			CreatedAt: now, UpdatedAt: now, ImportedAt: now,
			Totals: models.OrderTotals{
				Subtotal:   models.Money{Amount: subtotalAmt, Currency: currency},
				Tax:        models.Money{Amount: taxAmt, Currency: currency},
				GrandTotal: models.Money{Amount: subtotalAmt + taxAmt, Currency: currency},
			},
			Lines: lines,
		}
		if shipAddr.Name != "" {
			order.Customer = models.Customer{Name: shipAddr.Name}
		}
		existingIter := h.client.Collection(fmt.Sprintf("tenants/%s/orders", tenantID)).Where("external_order_id", "==", ref).Limit(1).Documents(ctx)
		existingDoc, existErr := existingIter.Next()
		existingIter.Stop()
		if existErr != nil {
			if _, err := h.client.Collection(fmt.Sprintf("tenants/%s/orders", tenantID)).Doc(order.OrderID).Set(ctx, order); err != nil {
				errorReport = append(errorReport, RowError{Column: "order_reference", Message: fmt.Sprintf("Failed to create order %s: %v", ref, err)})
				failed++
				continue
			}
			for _, line := range lines {
				h.client.Collection(fmt.Sprintf("tenants/%s/orders/%s/lines", tenantID, order.OrderID)).Doc(line.LineID).Set(ctx, line) //nolint
			}
			created++
		} else {
			updates := []firestore.Update{{Path: "updated_at", Value: now}, {Path: "lines", Value: lines}}
			if shippingService != "" {
				updates = append(updates, firestore.Update{Path: "shipping_service", Value: shippingService})
			}
			if _, err := existingDoc.Ref.Update(ctx, updates); err != nil {
				errorReport = append(errorReport, RowError{Column: "order_reference", Message: fmt.Sprintf("Failed to update order %s: %v", ref, err)})
				failed++
				continue
			}
			updated++
		}
	}
	return created, updated, failed, errorReport
}
