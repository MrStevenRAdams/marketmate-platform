package handlers

// ============================================================================
// PIM IMPORT/EXPORT HANDLER  (import_pim_handler.go)
//
// This is the handler for the /import-export nav item — bulk CSV/XLSX
// management of the internal PIM catalogue.  It has NOTHING to do with
// /marketplace/import, pending_imports, or channel-specific flows.
//
// Routes (register in main.go alongside existing importHandler routes):
//   POST   /api/v1/pim/import/preview       — parse headers + auto-mapping
//   POST   /api/v1/pim/import/validate      — row-level validation
//   POST   /api/v1/pim/import/apply         — idempotent apply (background job)
//   GET    /api/v1/pim/import/status/:id    — poll job
//   DELETE /api/v1/pim/import/jobs/:id      — delete job record
//   GET    /api/v1/pim/import/history       — last 20 jobs
//   GET    /api/v1/pim/template             — download template (CSV or XLSX)
//   POST   /api/v1/pim/export               — queue export job
//   GET    /api/v1/pim/export/jobs          — list export jobs
// ============================================================================

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"

	"module-a/models"
	"module-a/repository"
	"module-a/services"
)

// ─── Handler ──────────────────────────────────────────────────────────────────

type PIMImportHandler struct {
	repo           *repository.FirestoreRepository
	productService *services.ProductService
	client         *firestore.Client
}

func NewPIMImportHandler(repo *repository.FirestoreRepository, productService *services.ProductService, client *firestore.Client) *PIMImportHandler {
	return &PIMImportHandler{repo: repo, productService: productService, client: client}
}

// ─── Column spec ──────────────────────────────────────────────────────────────

// pimRequiredCols are the only columns that are truly mandatory.
var pimRequiredCols = []string{"sku", "title"}

// pimOptionalCols lists every fixed optional column; dynamic attribute_ / variant_attr_ columns
// are accepted automatically without being in this list.
var pimOptionalCols = []string{
	"product_id", "product_type", "parent_sku",
	"delete", "active", "status",
	"alias", "barcode",
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
	"supplier_sku", "supplier_name", "supplier_cost", "supplier_currency", "supplier_lead_time_days",
	"image_1", "image_2", "image_3", "image_4", "image_5",
	"bundle_component_skus",
	// example attribute columns (shown in mapping UI suggestions; not exhaustive)
	"attribute_colour", "attribute_material", "attribute_size",
	"variant_attr_colour", "variant_attr_size",
}

// ─── Template ─────────────────────────────────────────────────────────────────

// GetTemplate  GET /api/v1/pim/template?format=csv|xlsx
func (h *PIMImportHandler) GetTemplate(c *gin.Context) {
	format := c.DefaultQuery("format", "csv")

	headers := []string{
		"delete", "active",
		"product_id", "product_type", "parent_sku", "sku",
		"title", "subtitle", "description", "brand", "status",
		"alias", "barcode",
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
		"supplier_sku", "supplier_name", "supplier_cost", "supplier_currency", "supplier_lead_time_days",
		"image_1", "image_2", "image_3", "image_4", "image_5",
		"bundle_component_skus",
		"attribute_colour", "attribute_material",
		"variant_attr_colour", "variant_attr_size",
	}

	// Three example rows: simple, variant parent+child, bundle
	examples := [][]string{
		// simple product
		{"N", "Y", "", "simple", "", "WIDGET-BLUE", "Blue Widget", "", "A high-quality blue widget", "AcmeCo", "", "", "",
			"1234567890123", "", "", "", "", "",
			"Widgets|Blue Widgets", "widget|blue", "Easy to assemble|Recycled packaging", "",
			"19.99", "GBP", "24.99", "8.00", "", "", "",
			"100",
			"0.5", "kg", "12", "8", "6", "cm",
			"0.65", "kg", "14", "10", "8", "cm",
			"N", "N", "",
			"SUP-001", "Acme Supplies Ltd", "6.50", "GBP", "7",
			"https://example.com/img1.jpg", "", "", "", "",
			"",
			"Blue", "Recycled Plastic",
			"Blue", "M",
		},
		// variation parent
		{"N", "Y", "", "parent", "", "TSHIRT-PARENT", "Classic T-Shirt", "", "Available in multiple sizes and colours", "FashionCo", "", "", "",
			"", "", "", "", "", "",
			"Clothing|T-Shirts", "tshirt|classic", "", "",
			"", "GBP", "29.99", "5.00", "", "", "",
			"",
			"0.2", "kg", "30", "20", "2", "cm",
			"0.25", "kg", "35", "25", "5", "cm",
			"N", "N", "",
			"", "", "", "", "",
			"https://example.com/tshirt.jpg", "", "", "", "",
			"",
			"", "",
			"", "",
		},
		// variation child
		{"N", "Y", "", "variant", "TSHIRT-PARENT", "TSHIRT-RED-M", "", "", "", "", "", "", "",
			"", "", "", "", "", "",
			"", "", "", "",
			"24.99", "GBP", "", "", "", "", "",
			"50",
			"", "", "", "", "", "",
			"", "", "", "", "", "",
			"N", "N", "",
			"", "", "", "", "",
			"", "", "", "", "",
			"",
			"Red", "",
			"Red", "M",
		},
		// bundle
		{"N", "Y", "", "bundle", "", "STARTER-KIT", "Widget Starter Kit", "", "Includes Blue Widget x2 and Red Widget x1", "AcmeCo", "", "", "",
			"", "", "", "", "", "",
			"Bundles", "bundle|starter", "", "",
			"39.99", "GBP", "49.99", "", "", "", "",
			"",
			"", "", "", "", "", "",
			"", "", "", "", "", "",
			"N", "N", "",
			"", "", "", "", "",
			"https://example.com/kit.jpg", "", "", "", "",
			"WIDGET-BLUE:2|WIDGET-RED:1",
			"", "",
			"", "",
		},
	}

	switch format {
	case "xlsx":
		f := excelize.NewFile()
		sheet := "Products"
		f.SetSheetName("Sheet1", sheet)
		// Header row with bold + light blue fill
		styleID, _ := f.NewStyle(&excelize.Style{
			Font:      &excelize.Font{Bold: true, Color: "FFFFFF"},
			Fill:      excelize.Fill{Type: "pattern", Color: []string{"2563EB"}, Pattern: 1},
			Alignment: &excelize.Alignment{WrapText: true},
		})
		for ci, h := range headers {
			cell, _ := excelize.CoordinatesToCellName(ci+1, 1)
			f.SetCellValue(sheet, cell, h)
			f.SetCellStyle(sheet, cell, cell, styleID)
		}
		// Data rows
		for ri, row := range examples {
			for ci, val := range row {
				if ci < len(headers) {
					cell, _ := excelize.CoordinatesToCellName(ci+1, ri+2)
					f.SetCellValue(sheet, cell, val)
				}
			}
		}
		// Freeze header row
		f.SetPanes(sheet, &excelize.Panes{
			Freeze:      true,
			YSplit:      1,
			TopLeftCell: "A2",
			ActivePane:  "bottomLeft",
		})
		f.SetColWidth(sheet, "A", "A", 8)  // delete
		f.SetColWidth(sheet, "B", "B", 8)  // active
		f.SetColWidth(sheet, "F", "F", 20) // sku

		var buf bytes.Buffer
		f.Write(&buf)
		c.Header("Content-Disposition", "attachment; filename=products_template.xlsx")
		c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", buf.Bytes())

	default: // csv
		var buf bytes.Buffer
		w := csv.NewWriter(&buf)
		w.Write(headers)
		for _, row := range examples {
			// Pad row to header length
			for len(row) < len(headers) {
				row = append(row, "")
			}
			w.Write(row[:len(headers)])
		}
		w.Flush()
		c.Header("Content-Disposition", "attachment; filename=products_template.csv")
		c.Data(http.StatusOK, "text/csv; charset=utf-8", buf.Bytes())
	}
}

// ─── Preview ──────────────────────────────────────────────────────────────────

// PreviewImport  POST /api/v1/pim/import/preview
func (h *PIMImportHandler) PreviewImport(c *gin.Context) {
	rows, headers, _, err := parsePIMFile(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	preview := rows
	if len(preview) > 5 {
		preview = rows[:5]
	}

	// Auto-map: exact match first, then strip underscores fuzzy
	allTarget := append(pimRequiredCols, pimOptionalCols...)
	autoMapping := map[string]string{}
	for _, tf := range allTarget {
		tfLower := strings.ToLower(tf)
		for _, fh := range headers {
			if strings.ToLower(fh) == tfLower {
				autoMapping[tf] = fh
				break
			}
		}
		if _, ok := autoMapping[tf]; !ok {
			tfStripped := strings.ReplaceAll(tfLower, "_", "")
			for _, fh := range headers {
				if strings.ReplaceAll(strings.ToLower(fh), "_", "") == tfStripped {
					autoMapping[tf] = fh
					break
				}
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"headers":         headers,
		"preview_rows":    preview,
		"required_fields": pimRequiredCols,
		"optional_fields": pimOptionalCols,
		"auto_mapping":    autoMapping,
	})
}

// ─── Validate ─────────────────────────────────────────────────────────────────

// ValidateImport  POST /api/v1/pim/import/validate
func (h *PIMImportHandler) ValidateImport(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	rows, headers, _, err := parsePIMFile(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if mapping := parseColumnMapping(c); len(mapping) > 0 {
		headers = applyColumnMapping(headers, mapping)
	}

	result, err := h.validatePIMRows(c.Request.Context(), tenantID, headers, rows)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

// ─── Apply ────────────────────────────────────────────────────────────────────

// ApplyImport  POST /api/v1/pim/import/apply
func (h *PIMImportHandler) ApplyImport(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	rows, headers, filename, err := parsePIMFile(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if mapping := parseColumnMapping(c); len(mapping) > 0 {
		headers = applyColumnMapping(headers, mapping)
	}

	ctx := c.Request.Context()
	result, err := h.validatePIMRows(ctx, tenantID, headers, rows)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if result.ErrorCount > 0 {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "validation failed", "validation": result})
		return
	}

	job := &ImportJob{
		JobID:     uuid.New().String(), TenantID: tenantID, ImportType: "pim_products",
		Filename:  filename, Status: JobStatusPending, TotalRows: result.TotalRows,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	jobRef := h.client.Doc(fmt.Sprintf("tenants/%s/import_jobs_csv/%s", tenantID, job.JobID))
	if _, err := jobRef.Set(ctx, job); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create job"})
		return
	}

	go func() {
		bgCtx := context.Background()
		h.processPIMImport(bgCtx, job, tenantID, headers, rows)
	}()

	c.JSON(http.StatusAccepted, gin.H{"ok": true, "job_id": job.JobID, "total_rows": result.TotalRows})
}

// ─── Status / History / Delete ────────────────────────────────────────────────

func (h *PIMImportHandler) GetImportStatus(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	jobID := c.Param("id")
	doc, err := h.client.Doc(fmt.Sprintf("tenants/%s/import_jobs_csv/%s", tenantID, jobID)).Get(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}
	var job ImportJob
	if err := doc.DataTo(&job); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, job)
}

func (h *PIMImportHandler) DeleteImportJob(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	jobID := c.Param("id")
	ref := h.client.Doc(fmt.Sprintf("tenants/%s/import_jobs_csv/%s", tenantID, jobID))
	if _, err := ref.Delete(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete job"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true, "job_id": jobID})
}

func (h *PIMImportHandler) GetImportHistory(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	iter := h.client.Collection(fmt.Sprintf("tenants/%s/import_jobs_csv", tenantID)).
		Where("import_type", "==", "pim_products").
		OrderBy("created_at", firestore.Desc).Limit(20).Documents(c.Request.Context())
	var jobs []ImportJob
	for {
		doc, err := iter.Next()
		if err != nil {
			break
		}
		var j ImportJob
		if doc.DataTo(&j) == nil {
			jobs = append(jobs, j)
		}
	}
	if jobs == nil {
		jobs = []ImportJob{}
	}
	c.JSON(http.StatusOK, gin.H{"jobs": jobs})
}

// ─── Validation ───────────────────────────────────────────────────────────────

func (h *PIMImportHandler) validatePIMRows(ctx context.Context, tenantID string, headers []string, rows [][]string) (*ValidationResult, error) {
	colIdx := buildColIdx(headers)
	result := &ValidationResult{TotalRows: len(rows)}

	// Build SKU map of existing products (product and variant)
	skuToProductID, skuToVariantID := h.buildPIMSKUMaps(ctx, tenantID)

	// First pass: collect all SKUs declared in THIS file (for parent_sku cross-reference)
	fileSKUs := map[string]bool{}
	for _, row := range rows {
		if sku := getColVal(row, colIdx, "sku"); sku != "" {
			fileSKUs[sku] = true
		}
	}

	for i, row := range rows {
		rowNum := i + 2
		sku := getColVal(row, colIdx, "sku")
		title := getColVal(row, colIdx, "title")
		ptype := strings.ToLower(getColVal(row, colIdx, "product_type"))
		parentSKU := getColVal(row, colIdx, "parent_sku")
		deleteFlag := parseBool(getColVal(row, colIdx, "delete"))

		hasErr := false

		if sku == "" {
			result.Errors = append(result.Errors, RowError{Row: rowNum, Column: "sku", Message: "SKU is required"})
			result.ErrorCount++
			continue
		}

		// If delete=Y, only validate SKU exists (warn if not)
		if deleteFlag {
			if _, ok := skuToProductID[sku]; !ok {
				if _, ok2 := skuToVariantID[sku]; !ok2 {
					result.Warnings = append(result.Warnings, RowWarning{Row: rowNum, Column: "sku", Message: fmt.Sprintf("SKU '%s' not found — delete row will be skipped", sku)})
					result.WarningCount++
				}
			}
			result.ValidRows++
			result.UpdateCount++
			continue
		}

		// Title required for new products (not for updates)
		isNew := skuToProductID[sku] == "" && skuToVariantID[sku] == ""
		if isNew && title == "" {
			result.Errors = append(result.Errors, RowError{Row: rowNum, Column: "title", Message: "Title is required for new products"})
			hasErr = true
		}

		// product_type validation
		if ptype != "" && ptype != "simple" && ptype != "parent" && ptype != "variant" && ptype != "bundle" {
			result.Errors = append(result.Errors, RowError{Row: rowNum, Column: "product_type", Message: "Must be simple, parent, variant, or bundle"})
			hasErr = true
		}

		// parent_sku required for variants
		if ptype == "variant" && parentSKU == "" {
			result.Errors = append(result.Errors, RowError{Row: rowNum, Column: "parent_sku", Message: "parent_sku is required for variant rows"})
			hasErr = true
		}

		// parent_sku must exist (in file or DB)
		if ptype == "variant" && parentSKU != "" {
			if _, inDB := skuToProductID[parentSKU]; !inDB {
				if !fileSKUs[parentSKU] {
					result.Errors = append(result.Errors, RowError{Row: rowNum, Column: "parent_sku", Message: fmt.Sprintf("parent_sku '%s' not found in file or catalogue", parentSKU)})
					hasErr = true
				}
			}
		}

		// Numeric field validation
		for _, col := range []string{"list_price", "rrp", "cost_price", "sale_price", "supplier_cost"} {
			if v := getColVal(row, colIdx, col); v != "" {
				if f, err := strconv.ParseFloat(v, 64); err != nil || f < 0 {
					result.Errors = append(result.Errors, RowError{Row: rowNum, Column: col, Message: "Must be a valid non-negative number"})
					hasErr = true
				}
			}
		}
		if v := getColVal(row, colIdx, "quantity"); v != "" {
			if q, err := strconv.Atoi(v); err != nil || q < 0 {
				result.Errors = append(result.Errors, RowError{Row: rowNum, Column: "quantity", Message: "Must be a non-negative integer"})
				hasErr = true
			}
		}

		if hasErr {
			result.ErrorCount++
			continue
		}

		result.ValidRows++
		if isNew {
			result.CreateCount++
		} else {
			result.UpdateCount++
		}
	}

	return result, nil
}

// ─── Background processing ────────────────────────────────────────────────────

func (h *PIMImportHandler) processPIMImport(ctx context.Context, job *ImportJob, tenantID string, headers []string, rows [][]string) {
	jobRef := h.client.Doc(fmt.Sprintf("tenants/%s/import_jobs_csv/%s", tenantID, job.JobID))

	tick := func(status ImportJobStatus, processed, created, updated, failed int, errs []RowError) {
		jobRef.Update(ctx, []firestore.Update{
			{Path: "status", Value: status},
			{Path: "processed_rows", Value: processed},
			{Path: "created_count", Value: created},
			{Path: "updated_count", Value: updated},
			{Path: "failed_count", Value: failed},
			{Path: "error_report", Value: errs},
			{Path: "updated_at", Value: time.Now()},
		})
	}

	tick(JobStatusProcessing, 0, 0, 0, 0, nil)

	colIdx := buildColIdx(headers)
	skuToProductID, skuToVariantID := h.buildPIMSKUMaps(ctx, tenantID)

	// First pass: build parent SKU→productID map including rows in this file
	// so that variants can reference a parent created in the same import.
	// We process parent/simple rows first, then variant rows.
	type pendingRow struct {
		rowNum int
		row    []string
	}

	var parentRows, variantRows []pendingRow
	for i, row := range rows {
		ptype := strings.ToLower(getColVal(row, colIdx, "product_type"))
		if ptype == "variant" {
			variantRows = append(variantRows, pendingRow{i + 2, row})
		} else {
			parentRows = append(parentRows, pendingRow{i + 2, row})
		}
	}

	created, updated, failed := 0, 0, 0
	var errorReport []RowError

	processRow := func(rowNum int, row []string) {
		sku := getColVal(row, colIdx, "sku")
		if sku == "" {
			failed++
			return
		}

		deleteFlag := parseBool(getColVal(row, colIdx, "delete"))

		// ── Soft delete ──
		if deleteFlag {
			if productID, ok := skuToProductID[sku]; ok {
				now := time.Now()
				deletedBy := "csv_import"
				h.productService.UpdateProduct(ctx, tenantID, productID, map[string]interface{}{
					"status":     "archived",
					"deleted_at": now,
					"deleted_by": deletedBy,
					"updated_at": now,
				})
				updated++
			} else if variantID, ok := skuToVariantID[sku]; ok {
				h.repo.UpdateVariant(ctx, tenantID, variantID, map[string]interface{}{
					"status":     "archived",
					"deleted_at": time.Now(),
					"updated_at": time.Now(),
				})
				updated++
			}
			// If not found: warning already captured in validate; just skip
			return
		}

		ptype := strings.ToLower(getColVal(row, colIdx, "product_type"))
		if ptype == "" {
			ptype = "simple"
		}

		// ── Resolve status from active / status columns ──
		status := resolveStatus(
			getColVal(row, colIdx, "active"),
			getColVal(row, colIdx, "status"),
		)

		// ── UPDATE existing product ──
		if productID, ok := skuToProductID[sku]; ok {
			updates := buildProductUpdates(row, colIdx, status)
			if err := h.productService.UpdateProduct(ctx, tenantID, productID, updates); err != nil {
				errorReport = append(errorReport, RowError{Row: rowNum, Column: "sku", Message: err.Error()})
				failed++
			} else {
				updated++
			}
			return
		}

		// ── UPDATE existing variant ──
		if variantID, ok := skuToVariantID[sku]; ok {
			updates := buildVariantUpdates(row, colIdx, status)
			if err := h.repo.UpdateVariant(ctx, tenantID, variantID, updates); err != nil {
				errorReport = append(errorReport, RowError{Row: rowNum, Column: "sku", Message: err.Error()})
				failed++
			} else {
				updated++
			}
			return
		}

		// ── CREATE new ──
		if ptype == "variant" {
			// Create as a Variant under its parent
			parentSKU := getColVal(row, colIdx, "parent_sku")
			parentID := skuToProductID[parentSKU]
			if parentID == "" {
				errorReport = append(errorReport, RowError{Row: rowNum, Column: "parent_sku", Message: fmt.Sprintf("parent product '%s' not found", parentSKU)})
				failed++
				return
			}
			v := buildNewVariant(sku, parentID, tenantID, row, colIdx, status)
			if err := h.repo.CreateVariant(ctx, v); err != nil {
				errorReport = append(errorReport, RowError{Row: rowNum, Column: "sku", Message: err.Error()})
				failed++
			} else {
				skuToVariantID[sku] = v.VariantID
				created++
			}
		} else {
			p := buildNewProduct(sku, ptype, tenantID, row, colIdx, status)
			if err := h.productService.CreateProduct(ctx, p); err != nil {
				errorReport = append(errorReport, RowError{Row: rowNum, Column: "sku", Message: err.Error()})
				failed++
			} else {
				skuToProductID[sku] = p.ProductID
				created++
			}
		}
	}

	// Process parent/simple/bundle rows first so variants can find their parents
	for i, pr := range parentRows {
		processRow(pr.rowNum, pr.row)
		if (i+1)%50 == 0 {
			tick(JobStatusProcessing, i+1, created, updated, failed, nil)
		}
	}
	for i, pr := range variantRows {
		processRow(pr.rowNum, pr.row)
		if (i+1)%50 == 0 {
			tick(JobStatusProcessing, len(parentRows)+i+1, created, updated, failed, nil)
		}
	}

	finalStatus := JobStatusDone
	if failed > 0 && created == 0 && updated == 0 {
		finalStatus = JobStatusFailed
	}
	tick(finalStatus, len(rows), created, updated, failed, errorReport)
}

// ─── Export ───────────────────────────────────────────────────────────────────

// ExportPIMProducts  POST /api/v1/pim/export
// Accepts JSON body: { "format": "csv"|"xlsx" }
// Returns the file inline (synchronous — suitable for typical catalogue sizes).
// For very large catalogues queue via the existing export_jobs system.
func (h *PIMImportHandler) ExportPIMProducts(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing tenant_id"})
		return
	}

	var req struct {
		Format string `json:"format"`
	}
	c.ShouldBindJSON(&req)
	if req.Format == "" {
		req.Format = c.DefaultQuery("format", "csv")
	}

	ctx := c.Request.Context()

	// Fetch all products (unpaginated — Cloud Run has generous memory)
	products, _, err := h.repo.ListProducts(ctx, tenantID, map[string]interface{}{}, 0, 0)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Fetch all variants
	variants, _, _ := h.repo.ListVariants(ctx, tenantID, map[string]interface{}{}, 0, 0)
	variantsByProductID := map[string][]models.Variant{}
	for _, v := range variants {
		variantsByProductID[v.ProductID] = append(variantsByProductID[v.ProductID], v)
	}

	// Build SKU → productID for parent lookup on variant rows
	skuToProductID := map[string]string{}
	for _, p := range products {
		if p.SKU != "" {
			skuToProductID[p.SKU] = p.ProductID
		}
	}
	productIDToSKU := map[string]string{}
	for sku, id := range skuToProductID {
		productIDToSKU[id] = sku
	}

	headers := []string{
		"delete", "active",
		"product_id", "product_type", "parent_sku", "sku",
		"title", "subtitle", "description", "brand", "status",
		"alias", "barcode",
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
		"supplier_sku", "supplier_name", "supplier_cost", "supplier_currency", "supplier_lead_time_days",
		"image_1", "image_2", "image_3", "image_4", "image_5",
		"bundle_component_skus",
	}

	// Collect dynamic attribute keys across all products
	attrKeys := collectAttributeKeys(products, variants)
	for _, k := range attrKeys {
		headers = append(headers, "attribute_"+k)
	}

	// Build all rows
	var allRows [][]string

	// Sort products for deterministic output
	sort.Slice(products, func(i, j int) bool { return products[i].SKU < products[j].SKU })

	for _, p := range products {
		// Skip archived (deleted) products unless they have a deleted_at (then export them with delete=Y)
		isDeleted := false
		if p.Status == "archived" {
			if _, hasDeletedAt := getAttrStr(p.Attributes, "deleted_at"); hasDeletedAt {
				isDeleted = true
			}
		}

		row := productToRow(p, productIDToSKU, attrKeys, isDeleted)
		allRows = append(allRows, row)

		// If this product has variants, add a child row per variant
		if pvariants, ok := variantsByProductID[p.ProductID]; ok {
			sort.Slice(pvariants, func(i, j int) bool { return pvariants[i].SKU < pvariants[j].SKU })
			for _, v := range pvariants {
				vrow := variantToRow(v, p.SKU, p.ProductID, attrKeys)
				allRows = append(allRows, vrow)
			}
		}
	}

	dateStr := time.Now().Format("2006-01-02")

	switch req.Format {
	case "xlsx":
		f := excelize.NewFile()
		sheet := "Products"
		f.SetSheetName("Sheet1", sheet)
		styleID, _ := f.NewStyle(&excelize.Style{
			Font: &excelize.Font{Bold: true, Color: "FFFFFF"},
			Fill: excelize.Fill{Type: "pattern", Color: []string{"2563EB"}, Pattern: 1},
		})
		for ci, h := range headers {
			cell, _ := excelize.CoordinatesToCellName(ci+1, 1)
			f.SetCellValue(sheet, cell, h)
			f.SetCellStyle(sheet, cell, cell, styleID)
		}
		for ri, row := range allRows {
			for ci, val := range row {
				if ci < len(headers) {
					cell, _ := excelize.CoordinatesToCellName(ci+1, ri+2)
					f.SetCellValue(sheet, cell, val)
				}
			}
		}
		f.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})
		var buf bytes.Buffer
		f.Write(&buf)
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=products_%s.xlsx", dateStr))
		c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", buf.Bytes())

	default: // csv
		var buf bytes.Buffer
		w := csv.NewWriter(&buf)
		w.Write(headers)
		for _, row := range allRows {
			// Pad to header length
			for len(row) < len(headers) {
				row = append(row, "")
			}
			w.Write(row[:len(headers)])
		}
		w.Flush()
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=products_%s.csv", dateStr))
		c.Data(http.StatusOK, "text/csv; charset=utf-8", buf.Bytes())
	}
}

// ─── Row builders ─────────────────────────────────────────────────────────────

func productToRow(p models.Product, productIDToSKU map[string]string, attrKeys []string, isDeleted bool) []string {
	deleteVal := "N"
	if isDeleted {
		deleteVal = "Y"
	}
	activeVal := "Y"
	if p.Status == "draft" || p.Status == "archived" {
		activeVal = "N"
	}

	parentSKU := ""
	if p.ParentID != nil {
		parentSKU = productIDToSKU[*p.ParentID]
	}

	alias := ""
	if p.Attributes != nil {
		if v, ok := p.Attributes["alias"].(string); ok {
			alias = v
		}
	}
	barcode := ""
	subtitle := pimPtrStr(p.Subtitle)
	description := pimPtrStr(p.Description)
	brand := pimPtrStr(p.Brand)

	var ean, upc, asin, isbn, mpn, gtin string
	if p.Identifiers != nil {
		ean = pimPtrStr(p.Identifiers.EAN)
		upc = pimPtrStr(p.Identifiers.UPC)
		asin = pimPtrStr(p.Identifiers.ASIN)
		isbn = pimPtrStr(p.Identifiers.ISBN)
		mpn = pimPtrStr(p.Identifiers.MPN)
		gtin = pimPtrStr(p.Identifiers.GTIN)
	}

	categories := strings.Join(p.CategoryIDs, "|")
	tags := strings.Join(p.Tags, "|")
	keyFeatures := strings.Join(p.KeyFeatures, "|")
	attributeSetID := pimPtrStr(p.AttributeSetID)

	// Pricing — stored in attributes map if not in dedicated fields yet
	listPrice, currency, rrp, costPrice, salePrice, saleStart, saleEnd := "", "GBP", "", "", "", "", ""

	weightVal, weightUnit := "", ""
	if p.Weight != nil && p.Weight.Value != nil {
		weightVal = fmt.Sprintf("%.4g", *p.Weight.Value)
		weightUnit = p.Weight.Unit
	}
	length, width, height, dimUnit := "", "", "", ""
	if p.Dimensions != nil {
		if p.Dimensions.Length != nil {
			length = fmt.Sprintf("%.4g", *p.Dimensions.Length)
		}
		if p.Dimensions.Width != nil {
			width = fmt.Sprintf("%.4g", *p.Dimensions.Width)
		}
		if p.Dimensions.Height != nil {
			height = fmt.Sprintf("%.4g", *p.Dimensions.Height)
		}
		dimUnit = p.Dimensions.Unit
	}
	swVal, swUnit, sl, sw2, sh, sdUnit := "", "", "", "", "", ""
	if p.ShippingWeight != nil && p.ShippingWeight.Value != nil {
		swVal = fmt.Sprintf("%.4g", *p.ShippingWeight.Value)
		swUnit = p.ShippingWeight.Unit
	}
	if p.ShippingDimensions != nil {
		if p.ShippingDimensions.Length != nil {
			sl = fmt.Sprintf("%.4g", *p.ShippingDimensions.Length)
		}
		if p.ShippingDimensions.Width != nil {
			sw2 = fmt.Sprintf("%.4g", *p.ShippingDimensions.Width)
		}
		if p.ShippingDimensions.Height != nil {
			sh = fmt.Sprintf("%.4g", *p.ShippingDimensions.Height)
		}
		sdUnit = p.ShippingDimensions.Unit
	}

	useSerial := boolToYN(p.UseSerialNumbers)
	endOfLife := boolToYN(p.EndOfLife)

	// Supplier (first supplier only for simplicity)
	supplierSKU, supplierName, supplierCost, supplierCurrency, supplierLead := "", "", "", "", ""
	if len(p.Suppliers) > 0 {
		s := p.Suppliers[0]
		supplierSKU = s.SupplierSKU
		supplierName = s.SupplierName
		if s.UnitCost > 0 {
			supplierCost = fmt.Sprintf("%.2f", s.UnitCost)
			supplierCurrency = s.Currency
		}
		if s.LeadTimeDays > 0 {
			supplierLead = strconv.Itoa(s.LeadTimeDays)
		}
	}

	// Images (up to 5)
	images := make([]string, 5)
	imgIdx := 0
	for _, asset := range p.Assets {
		if imgIdx >= 5 {
			break
		}
		if asset.Role == "primary_image" {
			images[0] = asset.URL
		} else if imgIdx < 5 {
			images[imgIdx] = asset.URL
			imgIdx++
		}
	}

	// Bundle components
	bundleComponents := ""
	if len(p.BundleComponents) > 0 {
		parts := make([]string, 0, len(p.BundleComponents))
		for _, bc := range p.BundleComponents {
			parts = append(parts, fmt.Sprintf("%s:%d", bc.ProductID, bc.Quantity))
		}
		bundleComponents = strings.Join(parts, "|")
	}

	row := []string{
		deleteVal, activeVal,
		p.ProductID, p.ProductType, parentSKU, p.SKU,
		p.Title, subtitle, description, brand, p.Status,
		alias, barcode,
		ean, upc, asin, isbn, mpn, gtin,
		categories, tags, keyFeatures, attributeSetID,
		listPrice, currency, rrp, costPrice,
		salePrice, saleStart, saleEnd,
		"", // quantity — not on product, comes from inventory collection
		weightVal, weightUnit, length, width, height, dimUnit,
		swVal, swUnit, sl, sw2, sh, sdUnit,
		useSerial, endOfLife, p.StorageGroupID,
		supplierSKU, supplierName, supplierCost, supplierCurrency, supplierLead,
		images[0], images[1], images[2], images[3], images[4],
		bundleComponents,
	}

	// Dynamic attribute columns
	for _, k := range attrKeys {
		val := ""
		if p.Attributes != nil {
			if v, ok := p.Attributes[k]; ok {
				val = fmt.Sprintf("%v", v)
			}
		}
		row = append(row, val)
	}

	return row
}

func variantToRow(v models.Variant, parentSKU, parentProductID string, attrKeys []string) []string {
	activeVal := "Y"
	if v.Status == "draft" || v.Status == "archived" {
		activeVal = "N"
	}

	alias := pimPtrStr(v.Alias)
	barcode := pimPtrStr(v.Barcode)
	title := pimPtrStr(v.Title)

	var ean, upc, asin, isbn, mpn, gtin string
	if v.Identifiers != nil {
		ean = pimPtrStr(v.Identifiers.EAN)
		upc = pimPtrStr(v.Identifiers.UPC)
		asin = pimPtrStr(v.Identifiers.ASIN)
		isbn = pimPtrStr(v.Identifiers.ISBN)
		mpn = pimPtrStr(v.Identifiers.MPN)
		gtin = pimPtrStr(v.Identifiers.GTIN)
	}

	listPrice, rrp, costPrice := "", "", ""
	if v.Pricing != nil {
		if v.Pricing.ListPrice != nil {
			listPrice = fmt.Sprintf("%.2f", v.Pricing.ListPrice.Amount)
		}
		if v.Pricing.RRP != nil {
			rrp = fmt.Sprintf("%.2f", v.Pricing.RRP.Amount)
		}
		if v.Pricing.Cost != nil {
			costPrice = fmt.Sprintf("%.2f", v.Pricing.Cost.Amount)
		}
	}

	weightVal, weightUnit := "", ""
	if v.Weight != nil && v.Weight.Value != nil {
		weightVal = fmt.Sprintf("%.4g", *v.Weight.Value)
		weightUnit = v.Weight.Unit
	}
	length, width, height, dimUnit := "", "", "", ""
	if v.Dimensions != nil {
		if v.Dimensions.Length != nil { length = fmt.Sprintf("%.4g", *v.Dimensions.Length) }
		if v.Dimensions.Width != nil { width = fmt.Sprintf("%.4g", *v.Dimensions.Width) }
		if v.Dimensions.Height != nil { height = fmt.Sprintf("%.4g", *v.Dimensions.Height) }
		dimUnit = v.Dimensions.Unit
	}

	row := []string{
		"N", activeVal,
		parentProductID, "variant", parentSKU, v.SKU,
		title, "", "", "", v.Status,
		alias, barcode,
		ean, upc, asin, isbn, mpn, gtin,
		"", "", "", "", // categories, tags, key_features, attribute_set_id — on parent
		listPrice, "GBP", rrp, costPrice, "", "", "",
		"", // quantity
		weightVal, weightUnit, length, width, height, dimUnit,
		"", "", "", "", "", "", // shipping dims — usually on parent
		"", "", "", // serial, eol, storage_group
		"", "", "", "", "", // supplier
		"", "", "", "", "", // images
		"", // bundle_components
	}

	// Dynamic attribute columns — variant_attr_* takes priority
	for _, k := range attrKeys {
		val := ""
		if v.Attributes != nil {
			if fv, ok := v.Attributes[k]; ok {
				val = fmt.Sprintf("%v", fv)
			}
		}
		row = append(row, val)
	}

	return row
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func buildNewProduct(sku, ptype, tenantID string, row []string, colIdx map[string]int, status string) *models.Product {
	p := &models.Product{
		ProductID:   uuid.New().String(),
		TenantID:    tenantID,
		SKU:         sku,
		ProductType: ptype,
		Status:      status,
		Title:       getColVal(row, colIdx, "title"),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if s := getColVal(row, colIdx, "subtitle"); s != "" { p.Subtitle = &s }
	if s := getColVal(row, colIdx, "description"); s != "" { p.Description = &s }
	if s := getColVal(row, colIdx, "brand"); s != "" { p.Brand = &s }
	if s := getColVal(row, colIdx, "alias"); s != "" { p.Attributes["alias"] = s }

	p.Identifiers = parseIdentifiers(row, colIdx)

	if s := getColVal(row, colIdx, "categories"); s != "" { p.CategoryIDs = strings.Split(s, "|") }
	if s := getColVal(row, colIdx, "tags"); s != "" { p.Tags = strings.Split(s, "|") }
	if s := getColVal(row, colIdx, "key_features"); s != "" { p.KeyFeatures = strings.Split(s, "|") }
	if s := getColVal(row, colIdx, "attribute_set_id"); s != "" { p.AttributeSetID = &s }

	p.Weight = parseWeight(row, colIdx, "weight_value", "weight_unit")
	p.Dimensions = parseDimensions(row, colIdx, "length", "width", "height", "dimension_unit")
	p.ShippingWeight = parseWeight(row, colIdx, "shipping_weight_value", "shipping_weight_unit")
	p.ShippingDimensions = parseDimensions(row, colIdx, "shipping_length", "shipping_width", "shipping_height", "shipping_dimension_unit")

	p.UseSerialNumbers = parseBool(getColVal(row, colIdx, "use_serial_numbers"))
	p.EndOfLife = parseBool(getColVal(row, colIdx, "end_of_life"))
	p.StorageGroupID = getColVal(row, colIdx, "storage_group_id")

	// Supplier
	if sn := getColVal(row, colIdx, "supplier_name"); sn != "" {
		cur := getColVal(row, colIdx, "supplier_currency")
		if cur == "" { cur = "GBP" }
		sup := models.ProductSupplier{
			SupplierName: sn,
			SupplierSKU:  getColVal(row, colIdx, "supplier_sku"),
			Currency:     cur,
			IsDefault:    true,
			Priority:     1,
		}
		if cost, err := strconv.ParseFloat(getColVal(row, colIdx, "supplier_cost"), 64); err == nil {
			sup.UnitCost = cost
		}
		if days, err := strconv.Atoi(getColVal(row, colIdx, "supplier_lead_time_days")); err == nil {
			sup.LeadTimeDays = days
		}
		p.Suppliers = []models.ProductSupplier{sup}
	}

	// Images
	for n := 1; n <= 5; n++ {
		if url := getColVal(row, colIdx, fmt.Sprintf("image_%d", n)); url != "" {
			role := "gallery"
			if n == 1 { role = "primary_image" }
			p.Assets = append(p.Assets, models.ProductAsset{
				AssetID:   uuid.New().String(), URL: url, Role: role, SortOrder: n - 1,
			})
		}
	}

	// Bundle components
	if ptype == "bundle" {
		if comp := getColVal(row, colIdx, "bundle_component_skus"); comp != "" {
			for ci, part := range strings.Split(comp, "|") {
				pieces := strings.SplitN(part, ":", 2)
				if len(pieces) == 2 {
					qty, _ := strconv.Atoi(pieces[1])
					p.BundleComponents = append(p.BundleComponents, models.BundleComponent{
						ComponentID: uuid.New().String(), ProductID: pieces[0], Quantity: qty, IsRequired: true, SortOrder: ci,
					})
				}
			}
		}
	}

	// Dynamic attribute columns
	p.Attributes = map[string]interface{}{}
	for col, idx := range colIdx {
		if strings.HasPrefix(col, "attribute_") {
			key := strings.TrimPrefix(col, "attribute_")
			if val := strings.TrimSpace(row[idx]); val != "" {
				p.Attributes[key] = val
			}
		}
	}

	return p
}

func buildNewVariant(sku, parentID, tenantID string, row []string, colIdx map[string]int, status string) *models.Variant {
	v := &models.Variant{
		VariantID: uuid.New().String(),
		TenantID:  tenantID,
		ProductID: parentID,
		SKU:       sku,
		Status:    status,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if s := getColVal(row, colIdx, "title"); s != "" { v.Title = &s }
	if s := getColVal(row, colIdx, "alias"); s != "" { v.Alias = &s }
	if s := getColVal(row, colIdx, "barcode"); s != "" { v.Barcode = &s }
	v.Identifiers = parseIdentifiers(row, colIdx)
	v.Weight = parseWeight(row, colIdx, "weight_value", "weight_unit")
	v.Dimensions = parseDimensions(row, colIdx, "length", "width", "height", "dimension_unit")

	// Pricing
	var vp models.VariantPricing
	hasPricing := false
	if lp, err := strconv.ParseFloat(getColVal(row, colIdx, "list_price"), 64); err == nil && lp > 0 {
		cur := getColVal(row, colIdx, "currency"); if cur == "" { cur = "GBP" }
		vp.ListPrice = &models.Money{Amount: lp, Currency: cur}
		hasPricing = true
	}
	if rrp, err := strconv.ParseFloat(getColVal(row, colIdx, "rrp"), 64); err == nil && rrp > 0 {
		cur := getColVal(row, colIdx, "currency"); if cur == "" { cur = "GBP" }
		vp.RRP = &models.Money{Amount: rrp, Currency: cur}
		hasPricing = true
	}
	if cost, err := strconv.ParseFloat(getColVal(row, colIdx, "cost_price"), 64); err == nil && cost > 0 {
		cur := getColVal(row, colIdx, "currency"); if cur == "" { cur = "GBP" }
		vp.Cost = &models.Money{Amount: cost, Currency: cur}
		hasPricing = true
	}
	if hasPricing { v.Pricing = &vp }

	// Dynamic variant_attr_ columns
	v.Attributes = map[string]interface{}{}
	for col, idx := range colIdx {
		if strings.HasPrefix(col, "variant_attr_") {
			key := strings.TrimPrefix(col, "variant_attr_")
			if val := strings.TrimSpace(row[idx]); val != "" {
				v.Attributes[key] = val
			}
		}
		// Also accept attribute_ columns on variant rows
		if strings.HasPrefix(col, "attribute_") {
			key := strings.TrimPrefix(col, "attribute_")
			if _, exists := v.Attributes[key]; !exists { // variant_attr_ takes precedence
				if val := strings.TrimSpace(row[idx]); val != "" {
					v.Attributes[key] = val
				}
			}
		}
	}

	return v
}

func buildProductUpdates(row []string, colIdx map[string]int, status string) map[string]interface{} {
	u := map[string]interface{}{"updated_at": time.Now()}
	if status != "" { u["status"] = status }
	if t := getColVal(row, colIdx, "title"); t != "" { u["title"] = t }
	if t := getColVal(row, colIdx, "subtitle"); t != "" { u["subtitle"] = t }
	if t := getColVal(row, colIdx, "description"); t != "" { u["description"] = t }
	if t := getColVal(row, colIdx, "brand"); t != "" { u["brand"] = t }
	if t := getColVal(row, colIdx, "alias"); t != "" { u["alias"] = t }
	if t := getColVal(row, colIdx, "storage_group_id"); t != "" { u["storage_group_id"] = t }
	if t := getColVal(row, colIdx, "categories"); t != "" { u["category_ids"] = strings.Split(t, "|") }
	if t := getColVal(row, colIdx, "tags"); t != "" { u["tags"] = strings.Split(t, "|") }
	if t := getColVal(row, colIdx, "key_features"); t != "" { u["key_features"] = strings.Split(t, "|") }
	// Dynamic attribute columns
	for col, idx := range colIdx {
		if strings.HasPrefix(col, "attribute_") {
			key := strings.TrimPrefix(col, "attribute_")
			if val := strings.TrimSpace(row[idx]); val != "" {
				u["attributes."+key] = val
			}
		}
	}
	return u
}

func buildVariantUpdates(row []string, colIdx map[string]int, status string) map[string]interface{} {
	u := map[string]interface{}{"updated_at": time.Now()}
	if status != "" { u["status"] = status }
	if t := getColVal(row, colIdx, "title"); t != "" { u["title"] = t }
	if t := getColVal(row, colIdx, "alias"); t != "" { u["alias"] = t }
	if t := getColVal(row, colIdx, "barcode"); t != "" { u["barcode"] = t }
	for col, idx := range colIdx {
		if strings.HasPrefix(col, "variant_attr_") {
			key := strings.TrimPrefix(col, "variant_attr_")
			if val := strings.TrimSpace(row[idx]); val != "" {
				u["attributes."+key] = val
			}
		}
	}
	return u
}

// ─── File parsing ─────────────────────────────────────────────────────────────

// parsePIMFile handles both CSV and XLSX uploads.
func parsePIMFile(c *gin.Context) (rows [][]string, headers []string, filename string, err error) {
	var file multipart.File
	var fh *multipart.FileHeader
	file, fh, err = c.Request.FormFile("file")
	if err != nil {
		return nil, nil, "", fmt.Errorf("no file uploaded: %w", err)
	}
	defer file.Close()
	filename = fh.Filename

	if strings.HasSuffix(strings.ToLower(filename), ".xlsx") {
		return parseXLSX(file, filename)
	}
	return parseCSV(file, filename, c.DefaultPostForm("delimiter", ","))
}

func parseCSV(r io.Reader, filename, delimiter string) (rows [][]string, headers []string, fn string, err error) {
	delim := ','
	switch delimiter {
	case "\t", "tab": delim = '\t'
	case ";": delim = ';'
	case "|": delim = '|'
	}
	cr := csv.NewReader(r)
	cr.Comma = delim
	cr.LazyQuotes = true
	cr.TrimLeadingSpace = true
	all, err := cr.ReadAll()
	if err != nil {
		return nil, nil, filename, fmt.Errorf("CSV parse error: %w", err)
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

func parseXLSX(r io.Reader, filename string) (rows [][]string, headers []string, fn string, err error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, nil, filename, err
	}
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, nil, filename, fmt.Errorf("XLSX parse error: %w", err)
	}
	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, nil, filename, fmt.Errorf("XLSX has no sheets")
	}
	allRows, err := f.GetRows(sheets[0])
	if err != nil {
		return nil, nil, filename, err
	}
	if len(allRows) < 2 {
		return nil, nil, filename, fmt.Errorf("XLSX must have a header row and at least one data row")
	}
	rawHeaders := allRows[0]
	for i := range rawHeaders {
		rawHeaders[i] = strings.TrimSpace(strings.ToLower(rawHeaders[i]))
	}
	return allRows[1:], rawHeaders, filename, nil
}

// ─── Small utilities ──────────────────────────────────────────────────────────

func (h *PIMImportHandler) buildPIMSKUMaps(ctx context.Context, tenantID string) (skuToProductID, skuToVariantID map[string]string) {
	skuToProductID = map[string]string{}
	skuToVariantID = map[string]string{}
	products, _, _ := h.repo.ListProducts(ctx, tenantID, map[string]interface{}{}, 0, 0)
	for _, p := range products {
		if p.SKU != "" {
			skuToProductID[p.SKU] = p.ProductID
		}
	}
	variants, _, _ := h.repo.ListVariants(ctx, tenantID, map[string]interface{}{}, 0, 0)
	for _, v := range variants {
		if v.SKU != "" {
			skuToVariantID[v.SKU] = v.VariantID
		}
	}
	return
}

func resolveStatus(activeCol, statusCol string) string {
	// active column wins if present
	a := strings.ToLower(strings.TrimSpace(activeCol))
	if a == "y" || a == "yes" || a == "true" || a == "1" {
		return "active"
	}
	if a == "n" || a == "no" || a == "false" || a == "0" {
		return "draft"
	}
	// Fall back to status column
	s := strings.ToLower(strings.TrimSpace(statusCol))
	switch s {
	case "active", "draft", "archived":
		return s
	case "inactive", "disabled":
		return "draft"
	}
	return "" // empty = don't overwrite
}

func parseBool(v string) bool {
	v = strings.ToLower(strings.TrimSpace(v))
	return v == "y" || v == "yes" || v == "true" || v == "1"
}

func boolToYN(b bool) string {
	if b { return "Y" }
	return "N"
}

func pimPtrStr(s *string) string {
	if s == nil { return "" }
	return *s
}

func getAttrStr(attrs map[string]interface{}, key string) (string, bool) {
	if attrs == nil { return "", false }
	if v, ok := attrs[key]; ok {
		return fmt.Sprintf("%v", v), true
	}
	return "", false
}

func parseIdentifiers(row []string, colIdx map[string]int) *models.ProductIdentifiers {
	pi := &models.ProductIdentifiers{}
	hasAny := false
	setStr := func(dest **string, col string) {
		if v := getColVal(row, colIdx, col); v != "" {
			*dest = &v
			hasAny = true
		}
	}
	setStr(&pi.EAN, "ean")
	setStr(&pi.UPC, "upc")
	setStr(&pi.ASIN, "asin")
	setStr(&pi.ISBN, "isbn")
	setStr(&pi.MPN, "mpn")
	setStr(&pi.GTIN, "gtin")
	if !hasAny { return nil }
	return pi
}

func parseWeight(row []string, colIdx map[string]int, valCol, unitCol string) *models.Weight {
	v := getColVal(row, colIdx, valCol)
	u := getColVal(row, colIdx, unitCol)
	if v == "" { return nil }
	f, err := strconv.ParseFloat(v, 64)
	if err != nil { return nil }
	if u == "" { u = "kg" }
	return &models.Weight{Value: &f, Unit: u}
}

func parseDimensions(row []string, colIdx map[string]int, lCol, wCol, hCol, uCol string) *models.Dimensions {
	l := getColVal(row, colIdx, lCol)
	w := getColVal(row, colIdx, wCol)
	h := getColVal(row, colIdx, hCol)
	u := getColVal(row, colIdx, uCol)
	if l == "" && w == "" && h == "" { return nil }
	d := &models.Dimensions{Unit: u}
	if lf, err := strconv.ParseFloat(l, 64); err == nil { d.Length = &lf }
	if wf, err := strconv.ParseFloat(w, 64); err == nil { d.Width = &wf }
	if hf, err := strconv.ParseFloat(h, 64); err == nil { d.Height = &hf }
	return d
}

func collectAttributeKeys(products []models.Product, variants []models.Variant) []string {
	keySet := map[string]bool{}
	skip := map[string]bool{"source_sku": true, "sku": true, "deleted_at": true, "deleted_by": true}
	for _, p := range products {
		for k := range p.Attributes {
			if !skip[k] { keySet[k] = true }
		}
	}
	for _, v := range variants {
		for k := range v.Attributes {
			if !skip[k] { keySet[k] = true }
		}
	}
	keys := make([]string, 0, len(keySet))
	for k := range keySet {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Prevent unused import errors if some utility funcs from other files aren't called here
var _ = io.EOF
var _ = http.StatusOK
