package handlers

import (
	"bytes"
	"log"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"sort"
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
// EXPORT / IMPORT HANDLER
// ============================================================================

type ExportHandler struct {
	exportService  *services.ExportService
	repo           *repository.FirestoreRepository
	productService *services.ProductService
	orderService   *services.OrderService
	storageService *services.StorageService
	fsClient       *firestore.Client
	usage          *UsageInstrumentor
}

func NewExportHandler(exportService *services.ExportService, repo *repository.FirestoreRepository, productService *services.ProductService) *ExportHandler {
	return &ExportHandler{exportService: exportService, repo: repo, productService: productService, usage: NewUsageInstrumentor(nil)}
}

// SetOrderService injects the order service (called after construction in main.go)
func (h *ExportHandler) SetOrderService(orderService *services.OrderService) {
	h.orderService = orderService
}

// SetStorageService injects the GCS storage service.
func (h *ExportHandler) SetStorageService(s *services.StorageService) {
	h.storageService = s
}

// SetFirestoreClient injects the Firestore client for export job tracking.
func (h *ExportHandler) SetFirestoreClient(client *firestore.Client) {
	h.fsClient = client
}

// ExportOrders handles GET /api/v1/orders/export
func (h *ExportHandler) ExportOrders(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing tenant_id"})
		return
	}
	if h.orderService == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "order service not available"})
		return
	}

	status := c.Query("status")
	channel := c.Query("channel")

	orders, _, err := h.orderService.ListOrders(c.Request.Context(), tenantID, services.OrderListOptions{
		Status:    status,
		Channel:   channel,
		Limit:     "1000",
		Offset:    "0",
		SortBy:    "created_at",
		SortOrder: "desc",
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var buf strings.Builder
	w := csv.NewWriter(&buf)
	w.Write([]string{
		"order_id", "external_order_id", "channel", "status", "payment_status",
		"order_date", "customer_name", "customer_email",
		"shipping_name", "shipping_address_line1", "shipping_city", "shipping_postcode", "shipping_country",
		"subtotal", "shipping", "tax", "grand_total", "currency",
		"created_at",
	})

	for _, o := range orders {
		grandTotal, currency, subtotal, shippingAmt, tax := "", "", "", "", ""
		if o.Totals.GrandTotal.Amount > 0 {
			grandTotal = fmt.Sprintf("%.2f", o.Totals.GrandTotal.Amount)
			currency = o.Totals.GrandTotal.Currency
		}
		if o.Totals.Subtotal.Amount > 0 {
			subtotal = fmt.Sprintf("%.2f", o.Totals.Subtotal.Amount)
		}
		if o.Totals.Shipping.Amount > 0 {
			shippingAmt = fmt.Sprintf("%.2f", o.Totals.Shipping.Amount)
		}
		if o.Totals.Tax.Amount > 0 {
			tax = fmt.Sprintf("%.2f", o.Totals.Tax.Amount)
		}
		w.Write([]string{
			o.OrderID, o.ExternalOrderID, o.Channel, o.Status, o.PaymentStatus,
			o.OrderDate, o.Customer.Name, o.Customer.Email,
			o.ShippingAddress.Name, o.ShippingAddress.AddressLine1, o.ShippingAddress.City,
			o.ShippingAddress.PostalCode, o.ShippingAddress.Country,
			subtotal, shippingAmt, tax, grandTotal, currency, o.CreatedAt,
		})
	}
	w.Flush()

	filename := fmt.Sprintf("orders_%s.csv", time.Now().Format("2006-01-02"))
	c.Header("Content-Disposition", "attachment; filename="+filename)
	c.Data(http.StatusOK, "text/csv; charset=utf-8", []byte(buf.String()))
}

// ============================================================================
// PRODUCT EXPORT
// ============================================================================



// ============================================================================
// BACKGROUND EXPORT SYSTEM
// ============================================================================
// POST /api/v1/export/queue   — queue a background export job
// GET  /api/v1/export/jobs    — list export jobs for the tenant
//
// Flow:
//  1. Client calls POST /export/queue with { type, format }
//  2. Handler creates a job doc in Firestore (status=queued), returns job_id immediately
//  3. Background goroutine builds the file, uploads to GCS, updates job to status=ready
//  4. Client polls GET /export/jobs to see when status=ready, then clicks download URL
//
// Jobs are stored at tenants/{tid}/export_jobs/{jobID}
// Files are stored at exports/{tenantID}/{jobID}/products_YYYYMMDD.csv (private GCS path)
// Signed URLs valid for 24 hours are generated on demand via GET /export/jobs

type exportJob struct {
	JobID       string    `json:"job_id" firestore:"job_id"`
	TenantID    string    `json:"tenant_id" firestore:"tenant_id"`
	Type        string    `json:"type" firestore:"type"`
	Format      string    `json:"format" firestore:"format"`
	Status      string    `json:"status" firestore:"status"` // queued|building|ready|failed
	RowCount    int       `json:"row_count,omitempty" firestore:"row_count,omitempty"`
	DownloadURL string    `json:"download_url,omitempty" firestore:"download_url,omitempty"`
	GCSPath     string    `json:"gcs_path,omitempty" firestore:"gcs_path,omitempty"`
	Error       string    `json:"error,omitempty" firestore:"error,omitempty"`
	CreatedAt   time.Time `json:"created_at" firestore:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" firestore:"updated_at"`
	ReadyAt     *time.Time `json:"ready_at,omitempty" firestore:"ready_at,omitempty"`
}

// QueueExport  POST /api/v1/export/queue
func (h *ExportHandler) QueueExport(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req struct {
		Type   string `json:"type" binding:"required"`
		Format string `json:"format"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Format == "" {
		req.Format = "csv"
	}

	if h.fsClient == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "export job store not available"})
		return
	}

	jobID := uuid.New().String()
	now := time.Now()
	job := exportJob{
		JobID:     jobID,
		TenantID:  tenantID,
		Type:      req.Type,
		Format:    req.Format,
		Status:    "queued",
		CreatedAt: now,
		UpdatedAt: now,
	}

	jobRef := h.fsClient.Collection("tenants").Doc(tenantID).Collection("export_jobs").Doc(jobID)
	if _, err := jobRef.Set(c.Request.Context(), job); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Fire background build — uses its own context so request deadline doesn't kill it
	go h.buildExportJob(tenantID, jobID, req.Type, req.Format)

	c.JSON(http.StatusOK, gin.H{"ok": true, "job_id": jobID, "status": "queued"})
}

// buildExportJob runs in a goroutine, builds the export file and uploads to GCS.
func (h *ExportHandler) buildExportJob(tenantID, jobID, exportType, format string) {
	ctx := context.Background()
	jobRef := h.fsClient.Collection("tenants").Doc(tenantID).Collection("export_jobs").Doc(jobID)

	// Mark as building
	jobRef.Update(ctx, []firestore.Update{
		{Path: "status", Value: "building"},
		{Path: "updated_at", Value: time.Now()},
	})

	var csvBytes []byte
	var filename string
	var rowCount int
	var buildErr error

	switch exportType {
	case "products":
		var result *services.ExportResult
		result, buildErr = h.exportService.ExportProducts(ctx, tenantID, map[string]interface{}{})
		if buildErr == nil {
			var buf bytes.Buffer
			w := csv.NewWriter(&buf)
			w.Write(result.Headers)
			for _, row := range result.Rows {
				w.Write(row)
			}
			w.Flush()
			csvBytes = buf.Bytes()
			filename = result.Filename
			rowCount = len(result.Rows)
		}
	case "prices":
		result, err := h.exportService.ExportPrices(ctx, tenantID)
		if err != nil {
			buildErr = err
		} else {
			var buf bytes.Buffer
			w := csv.NewWriter(&buf)
			w.Write(result.Headers)
			for _, row := range result.Rows { w.Write(row) }
			w.Flush()
			csvBytes = buf.Bytes()
			filename = result.Filename
			rowCount = len(result.Rows)
		}
	case "inventory_basic", "inventory_advanced":
		result, err := h.exportService.ExportStock(ctx, tenantID)
		if err != nil {
			buildErr = err
		} else {
			var buf bytes.Buffer
			w := csv.NewWriter(&buf)
			w.Write(result.Headers)
			for _, row := range result.Rows { w.Write(row) }
			w.Flush()
			csvBytes = buf.Bytes()
			filename = result.Filename
			rowCount = len(result.Rows)
		}
	default:
		buildErr = fmt.Errorf("unsupported export type: %s", exportType)
	}

	if buildErr != nil {
		log.Printf("[ExportJob] %s/%s failed: %v", tenantID, jobID, buildErr)
		jobRef.Update(ctx, []firestore.Update{
			{Path: "status", Value: "failed"},
			{Path: "error", Value: buildErr.Error()},
			{Path: "updated_at", Value: time.Now()},
		})
		return
	}

	// Upload to GCS
	gcsPath := fmt.Sprintf("exports/%s/%s/%s", tenantID, jobID, filename)
	var downloadURL string

	if h.storageService != nil {
		url, err := h.storageService.Upload(ctx, gcsPath, bytes.NewReader(csvBytes), "text/csv; charset=utf-8")
		if err != nil {
			log.Printf("[ExportJob] GCS upload failed for %s/%s: %v", tenantID, jobID, err)
			// Fall back — store base64 in Firestore (only for small exports)
			downloadURL = ""
		} else {
			downloadURL = url
		}
	}

	now := time.Now()
	updates := []firestore.Update{
		{Path: "status", Value: "ready"},
		{Path: "row_count", Value: rowCount},
		{Path: "download_url", Value: downloadURL},
		{Path: "gcs_path", Value: gcsPath},
		{Path: "ready_at", Value: now},
		{Path: "updated_at", Value: now},
	}
	jobRef.Update(ctx, updates)
	log.Printf("[ExportJob] %s/%s complete — %d rows, url=%s", tenantID, jobID, rowCount, downloadURL)
}

// ListExportJobs  GET /api/v1/export/jobs
func (h *ExportHandler) ListExportJobs(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	if h.fsClient == nil {
		c.JSON(http.StatusOK, gin.H{"jobs": []exportJob{}})
		return
	}

	iter := h.fsClient.Collection("tenants").Doc(tenantID).Collection("export_jobs").
		OrderBy("created_at", firestore.Desc).
		Limit(20).
		Documents(ctx)
	defer iter.Stop()

	var jobs []exportJob
	for {
		doc, err := iter.Next()
		if err != nil {
			break
		}
		var job exportJob
		if err := doc.DataTo(&job); err == nil {
			jobs = append(jobs, job)
		}
	}
	if jobs == nil {
		jobs = []exportJob{}
	}
	c.JSON(http.StatusOK, gin.H{"jobs": jobs})
}


// ============================================================================
// STREAMING PRODUCT EXPORT
// GET /api/v1/export/products/stream
// ============================================================================
// Uses a background context (no deadline) so large Firestore reads complete
// without being cancelled by the HTTP request deadline.

func (h *ExportHandler) StreamProductsCSV(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	// Background context — no deadline, so Firestore reads 27k+ docs without timeout
	ctx := context.Background()

	csvBytes, filename, err := h.exportService.ExportProductsCSV(ctx, tenantID, map[string]interface{}{})
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	c.Data(200, "text/csv; charset=utf-8", csvBytes)
}

func (h *ExportHandler) ExportProducts(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	format := c.DefaultQuery("format", "csv")
	filters := map[string]interface{}{}
	if s := c.Query("status"); s != "" {
		filters["status"] = s
	}

	switch format {
	case "csv":
		csvBytes, filename, err := h.exportService.ExportProductsCSV(c.Request.Context(), tenantID, filters)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		if h.usage != nil {
			h.usage.RecordDataExport(c.Request.Context(), tenantID, "", "products_csv")
		}
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
		c.Data(200, "text/csv; charset=utf-8", csvBytes)

	case "xlsx", "json":
		result, err := h.exportService.ExportProducts(c.Request.Context(), tenantID, filters)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		if h.usage != nil {
			h.usage.RecordDataExport(c.Request.Context(), tenantID, "", "products_xlsx")
		}
		c.JSON(200, gin.H{
			"ok": true, "format": "xlsx_data",
			"headers": result.Headers, "rows": result.Rows,
			"total":    result.Total,
			"filename": strings.Replace(result.Filename, ".csv", ".xlsx", 1),
			"exported_at": result.ExportedAt,
		})

	default:
		c.JSON(400, gin.H{"error": "format must be 'csv', 'xlsx', or 'json'"})
	}
}

func (h *ExportHandler) ExportPrices(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	result, err := h.exportService.ExportPrices(c.Request.Context(), tenantID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if c.DefaultQuery("format", "json") == "csv" {
		var buf strings.Builder
		w := csv.NewWriter(&buf)
		w.Write(result.Headers)
		for _, row := range result.Rows {
			w.Write(row)
		}
		w.Flush()
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", result.Filename))
		c.Data(200, "text/csv; charset=utf-8", []byte(buf.String()))
		return
	}
	c.JSON(200, gin.H{"ok": true, "headers": result.Headers, "rows": result.Rows, "total": result.Total})
}

func (h *ExportHandler) ExportStock(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	result, err := h.exportService.ExportStock(c.Request.Context(), tenantID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if c.DefaultQuery("format", "json") == "csv" {
		var buf strings.Builder
		w := csv.NewWriter(&buf)
		w.Write(result.Headers)
		for _, row := range result.Rows {
			w.Write(row)
		}
		w.Flush()
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", result.Filename))
		c.Data(200, "text/csv; charset=utf-8", []byte(buf.String()))
		return
	}
	c.JSON(200, gin.H{"ok": true, "headers": result.Headers, "rows": result.Rows, "total": result.Total})
}

func (h *ExportHandler) ExportTemplate(c *gin.Context) {
	c.JSON(200, gin.H{
		"ok":              true,
		"fixed_columns":   services.FixedColumns,
		"dynamic_columns": []string{"variant_attr_{key}", "image_N", "bundle_component_skus", "attribute_{key}"},
		"message":         "Fixed columns always present. Attribute columns use named format: attribute_colour, attribute_recommended_age etc. Variant attributes: variant_attr_colour etc. Old attribute_N_name/value format still accepted on import.",
	})
}

// ============================================================================
// DRY-RUN IMPORT (validate only, no writes)
// ============================================================================

type importRowResult struct {
	RowNum   int               `json:"row_num"`
	SKU      string            `json:"sku"`
	Action   string            `json:"action"` // create / update / skip
	Errors   map[string]string `json:"errors,omitempty"`
	Warnings map[string]string `json:"warnings,omitempty"`
}

func (h *ExportHandler) ImportDryRun(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	createNew := c.DefaultQuery("create_new", "true") == "true"

	rows, headers, err := h.parseUpload(c)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	results, summary := h.validateRows(c, tenantID, headers, rows, createNew)
	c.JSON(200, gin.H{
		"ok":      summary["errors"] == 0,
		"summary": summary,
		"rows":    results,
		"headers": headers,
		"message": fmt.Sprintf("Validated %d rows: %d creates, %d updates, %d errors", summary["total"], summary["creates"], summary["updates"], summary["errors"]),
	})
}

// ============================================================================
// IMPORT (execute after dry-run)
// ============================================================================

func (h *ExportHandler) ImportProducts(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	createNew := c.DefaultQuery("create_new", "true") == "true"

	rows, headers, err := h.parseUpload(c)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	results, summary := h.validateRows(c, tenantID, headers, rows, createNew)
	if summary["errors"].(int) > 0 {
		c.JSON(400, gin.H{"ok": false, "message": "Validation failed. Fix errors and retry.", "summary": summary, "rows": results})
		return
	}

	colIdx := map[string]int{}
	for i, h := range headers {
		colIdx[h] = i
	}

	created, updated, failed := 0, 0, 0

	// Pre-load existing SKU → product_id mappings
	skuToProductID := map[string]string{}
	existingProducts, _, _ := h.repo.ListProducts(c.Request.Context(), tenantID, map[string]interface{}{}, 0, 0)
	for _, p := range existingProducts {
		sku := ""
		if p.Attributes != nil {
			if s, ok := p.Attributes["source_sku"].(string); ok {
				sku = s
			}
		}
		if sku != "" {
			skuToProductID[sku] = p.ProductID
		}
	}
	existingVariants, _, _ := h.repo.ListVariants(c.Request.Context(), tenantID, map[string]interface{}{}, 0, 0)
	for _, v := range existingVariants {
		skuToProductID[v.SKU] = v.VariantID
	}

	// First pass: create/update parent, simple, and bundle products
	for _, result := range results {
		if result.Action == "skip" {
			continue
		}
		rowIdx := result.RowNum - 2
		if rowIdx < 0 || rowIdx >= len(rows) {
			continue
		}
		row := rows[rowIdx]
		ptype := getCol(row, colIdx, "product_type")
		sku := getCol(row, colIdx, "sku")
		if ptype == "variant" {
			continue // second pass
		}

		if result.Action == "create" {
			product := h.rowToProduct(row, colIdx, headers, tenantID)
			product.ProductID = uuid.New().String()
			if err := h.productService.CreateProduct(c.Request.Context(), product); err != nil {
				failed++
				continue
			}
			skuToProductID[sku] = product.ProductID
			created++
		} else if result.Action == "update" {
			productID := getCol(row, colIdx, "product_id")
			if productID == "" {
				productID = skuToProductID[sku]
			}
			if productID == "" {
				failed++
				continue
			}
			updates := h.rowToUpdates(row, colIdx, headers)
			if err := h.productService.UpdateProduct(c.Request.Context(), tenantID, productID, updates); err != nil {
				failed++
				continue
			}
			updated++
		}
	}

	// Second pass: create/update variants
	for _, result := range results {
		if result.Action == "skip" {
			continue
		}
		rowIdx := result.RowNum - 2
		if rowIdx < 0 || rowIdx >= len(rows) {
			continue
		}
		row := rows[rowIdx]
		ptype := getCol(row, colIdx, "product_type")
		if ptype != "variant" {
			continue
		}

		sku := getCol(row, colIdx, "sku")
		parentSKU := getCol(row, colIdx, "parent_sku")
		parentProductID := skuToProductID[parentSKU]
		if parentProductID == "" {
			failed++
			continue
		}

		if result.Action == "create" {
			variant := h.rowToVariant(row, colIdx, headers, tenantID, parentProductID)
			variant.VariantID = uuid.New().String()
			if err := h.repo.CreateVariant(c.Request.Context(), variant); err != nil {
				failed++
				continue
			}
			skuToProductID[sku] = variant.VariantID
			created++
		} else if result.Action == "update" {
			variantID := skuToProductID[sku]
			if variantID == "" {
				failed++
				continue
			}
			updates := h.rowToVariantUpdates(row, colIdx, headers)
			if err := h.repo.UpdateVariant(c.Request.Context(), tenantID, variantID, updates); err != nil {
				failed++
				continue
			}
			updated++
		}
	}

	c.JSON(200, gin.H{
		"ok":      failed == 0,
		"created": created,
		"updated": updated,
		"failed":  failed,
		"total":   created + updated + failed,
		"message": fmt.Sprintf("Import complete: %d created, %d updated, %d failed", created, updated, failed),
	})
}

// ============================================================================
// CSV PARSING
// ============================================================================

func (h *ExportHandler) parseUpload(c *gin.Context) ([][]string, []string, error) {
	file, _, err := c.Request.FormFile("file")
	if err != nil {
		return nil, nil, fmt.Errorf("no file uploaded: %w", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.LazyQuotes = true
	reader.TrimLeadingSpace = true

	allRows, err := reader.ReadAll()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse CSV: %w", err)
	}
	if len(allRows) < 2 {
		return nil, nil, fmt.Errorf("file must have a header row and at least one data row")
	}

	headers := allRows[0]
	for i := range headers {
		headers[i] = strings.TrimSpace(strings.ToLower(headers[i]))
	}
	return allRows[1:], headers, nil
}

// ============================================================================
// VALIDATION
// ============================================================================

func (h *ExportHandler) validateRows(c *gin.Context, tenantID string, headers []string, rows [][]string, createNew bool) ([]importRowResult, map[string]interface{}) {
	colIdx := map[string]int{}
	for i, h := range headers {
		colIdx[h] = i
	}

	existingProducts, _, _ := h.repo.ListProducts(c.Request.Context(), tenantID, map[string]interface{}{}, 0, 0)
	existingSKUs := map[string]string{}
	existingProductIDs := map[string]bool{}
	for _, p := range existingProducts {
		existingProductIDs[p.ProductID] = true
		if p.Attributes != nil {
			if s, ok := p.Attributes["source_sku"].(string); ok && s != "" {
				existingSKUs[s] = p.ProductID
			}
		}
	}
	existingVariants, _, _ := h.repo.ListVariants(c.Request.Context(), tenantID, map[string]interface{}{}, 0, 0)
	for _, v := range existingVariants {
		existingSKUs[v.SKU] = v.VariantID
	}

	fileSKUs := map[string]bool{}
	for _, row := range rows {
		if sku := getCol(row, colIdx, "sku"); sku != "" {
			fileSKUs[sku] = true
		}
	}

	var results []importRowResult
	creates, updates, errors_, skips := 0, 0, 0, 0

	for i, row := range rows {
		rowNum := i + 2
		sku := getCol(row, colIdx, "sku")
		productID := getCol(row, colIdx, "product_id")
		ptype := getCol(row, colIdx, "product_type")

		result := importRowResult{RowNum: rowNum, SKU: sku, Errors: map[string]string{}, Warnings: map[string]string{}}

		isExisting := false
		if productID != "" && existingProductIDs[productID] {
			isExisting = true
		}
		if !isExisting && sku != "" {
			_, isExisting = existingSKUs[sku]
		}

		if isExisting {
			result.Action = "update"
		} else if createNew {
			result.Action = "create"
		} else {
			result.Action = "skip"
			result.Warnings["sku"] = "SKU not found and create_new is disabled"
			skips++
			results = append(results, result)
			continue
		}

		if sku == "" && result.Action == "create" {
			result.Errors["sku"] = "SKU is required for new products"
		}
		if ptype == "" && result.Action == "create" {
			result.Errors["product_type"] = "product_type is required (simple, parent, variant, bundle)"
		} else if ptype != "" && ptype != "simple" && ptype != "parent" && ptype != "variant" && ptype != "bundle" {
			result.Errors["product_type"] = "Must be simple, parent, variant, or bundle"
		}

		title := getCol(row, colIdx, "title")
		if title == "" && result.Action == "create" && ptype != "variant" {
			result.Errors["title"] = "Title is required for new products"
		}

		if ptype == "variant" {
			parentSKU := getCol(row, colIdx, "parent_sku")
			if parentSKU == "" {
				result.Errors["parent_sku"] = "parent_sku is required for variants"
			} else if !fileSKUs[parentSKU] && existingSKUs[parentSKU] == "" {
				result.Errors["parent_sku"] = fmt.Sprintf("Parent SKU '%s' not found in file or database", parentSKU)
			}
		}

		if ptype == "bundle" {
			components := getCol(row, colIdx, "bundle_component_skus")
			if components == "" && result.Action == "create" {
				result.Errors["bundle_component_skus"] = "Bundle must have at least one component (format: SKU:QTY|SKU:QTY)"
			} else if components != "" {
				for _, part := range strings.Split(components, "|") {
					pieces := strings.Split(part, ":")
					if len(pieces) != 2 {
						result.Errors["bundle_component_skus"] = fmt.Sprintf("Invalid format '%s' — use SKU:QTY", part)
						break
					}
					if _, err := strconv.Atoi(pieces[1]); err != nil {
						result.Errors["bundle_component_skus"] = fmt.Sprintf("Invalid quantity in '%s'", part)
						break
					}
				}
			}
		}

		if price := getCol(row, colIdx, "list_price"); price != "" {
			if p, err := strconv.ParseFloat(price, 64); err != nil || p < 0 {
				result.Errors["list_price"] = "Must be a positive number"
			}
		}
		if qty := getCol(row, colIdx, "quantity"); qty != "" {
			if q, err := strconv.Atoi(qty); err != nil || q < 0 {
				result.Errors["quantity"] = "Must be a non-negative integer"
			}
		}
		if ean := getCol(row, colIdx, "ean"); ean != "" && len(ean) != 13 {
			result.Warnings["ean"] = fmt.Sprintf("EAN should be 13 digits (got %d)", len(ean))
		}

		if len(result.Errors) > 0 {
			errors_++
		} else if result.Action == "create" {
			creates++
		} else {
			updates++
		}
		results = append(results, result)
	}

	summary := map[string]interface{}{
		"total":   len(rows),
		"creates": creates,
		"updates": updates,
		"errors":  errors_,
		"skips":   skips,
	}
	return results, summary
}

// ============================================================================
// ROW → MODEL CONVERTERS
// ============================================================================

// extractAttributes reads freeform attribute values from a row, supporting both
// the new named-column format and the old name/value pair format for backward
// compatibility with exports generated before this change.
//
//   New format (preferred): "attribute_colour" → key="colour", value=cell value
//   Old format (still accepted): "attribute_3_name"+"attribute_3_value" pairs
//
// prefix is "attribute" for product attributes, "variant_attr" for variant attributes.
func extractAttributes(row []string, colIdx map[string]int, headers []string, prefix string) map[string]string {
	out := map[string]string{}
	for _, h := range headers {
		if !strings.HasPrefix(h, prefix+"_") {
			continue
		}
		rest := strings.TrimPrefix(h, prefix+"_")

		// Old format: "attribute_3_name" — rest is a digit followed by "_name"
		if strings.HasSuffix(rest, "_name") {
			middle := strings.TrimSuffix(rest, "_name")
			if _, err := strconv.Atoi(middle); err == nil {
				// This is an old-format name column — find its paired value column
				valCol := prefix + "_" + middle + "_value"
				name := strings.TrimSpace(getCol(row, colIdx, h))
				val  := strings.TrimSpace(getCol(row, colIdx, valCol))
				if name != "" && val != "" {
					out[name] = val
				}
			}
			continue
		}
		// Skip the value half of an old-format pair (already handled above)
		if strings.HasSuffix(rest, "_value") {
			middle := strings.TrimSuffix(rest, "_value")
			if _, err := strconv.Atoi(middle); err == nil {
				continue
			}
		}

		// New format: "attribute_colour" — rest is the attribute key directly
		val := strings.TrimSpace(getCol(row, colIdx, h))
		if val != "" {
			out[rest] = val
		}
	}
	return out
}

func (h *ExportHandler) rowToProduct(row []string, colIdx map[string]int, headers []string, tenantID string) *models.Product {
	p := &models.Product{
		TenantID:    tenantID,
		Title:       getCol(row, colIdx, "title"),
		ProductType: getCol(row, colIdx, "product_type"),
		Status:      getCol(row, colIdx, "status"),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if p.Status == "" {
		p.Status = "draft"
	}
	if p.ProductType == "" {
		p.ProductType = "simple"
	}

	if s := getCol(row, colIdx, "subtitle"); s != "" {
		p.Subtitle = &s
	}
	if s := getCol(row, colIdx, "description"); s != "" {
		p.Description = &s
	}
	if s := getCol(row, colIdx, "brand"); s != "" {
		p.Brand = &s
	}

	// Identifiers
	idents := &models.ProductIdentifiers{}
	hasIdent := false
	for _, pair := range []struct {
		col   string
		field **string
	}{
		{"ean", &idents.EAN}, {"upc", &idents.UPC}, {"asin", &idents.ASIN},
		{"isbn", &idents.ISBN}, {"mpn", &idents.MPN}, {"gtin", &idents.GTIN},
	} {
		if v := getCol(row, colIdx, pair.col); v != "" {
			s := v
			*pair.field = &s
			hasIdent = true
		}
	}
	if hasIdent {
		p.Identifiers = idents
	}

	if cats := getCol(row, colIdx, "categories"); cats != "" {
		p.CategoryIDs = strings.Split(cats, "|")
	}
	if tags := getCol(row, colIdx, "tags"); tags != "" {
		p.Tags = strings.Split(tags, "|")
	}
	if kf := getCol(row, colIdx, "key_features"); kf != "" {
		p.KeyFeatures = strings.Split(kf, "|")
	}
	if asID := getCol(row, colIdx, "attribute_set_id"); asID != "" {
		p.AttributeSetID = &asID
	}

	// Shipping dimensions
	sDims := &models.Dimensions{Unit: getCol(row, colIdx, "shipping_dimension_unit")}
	hasSDims := false
	if l := getCol(row, colIdx, "shipping_length"); l != "" {
		if f, err := strconv.ParseFloat(l, 64); err == nil { sDims.Length = &f; hasSDims = true }
	}
	if w := getCol(row, colIdx, "shipping_width"); w != "" {
		if f, err := strconv.ParseFloat(w, 64); err == nil { sDims.Width = &f; hasSDims = true }
	}
	if ht := getCol(row, colIdx, "shipping_height"); ht != "" {
		if f, err := strconv.ParseFloat(ht, 64); err == nil { sDims.Height = &f; hasSDims = true }
	}
	if hasSDims {
		if sDims.Unit == "" { sDims.Unit = "cm" }
		p.ShippingDimensions = sDims
	}
	if swv := getCol(row, colIdx, "shipping_weight_value"); swv != "" {
		if f, err := strconv.ParseFloat(swv, 64); err == nil {
			unit := getCol(row, colIdx, "shipping_weight_unit")
			if unit == "" { unit = "g" }
			p.ShippingWeight = &models.Weight{Value: &f, Unit: unit}
		}
	}

	// Lifecycle flags
	if getCol(row, colIdx, "use_serial_numbers") == "true" { p.UseSerialNumbers = true }
	if getCol(row, colIdx, "end_of_life") == "true"        { p.EndOfLife = true }

	// WMS
	if sgID := getCol(row, colIdx, "storage_group_id"); sgID != "" {
		p.StorageGroupID = sgID
	}

	// Supplier (primary)
	if supSKU := getCol(row, colIdx, "supplier_sku"); supSKU != "" {
		sup := models.ProductSupplier{
			SupplierSKU:  supSKU,
			SupplierName: getCol(row, colIdx, "supplier_name"),
			Currency:     getCol(row, colIdx, "supplier_currency"),
			IsDefault:    true,
			Priority:     1,
		}
		if cost := getCol(row, colIdx, "supplier_cost"); cost != "" {
			if f, err := strconv.ParseFloat(cost, 64); err == nil { sup.UnitCost = f }
		}
		if ltd := getCol(row, colIdx, "supplier_lead_time_days"); ltd != "" {
			if n, err := strconv.Atoi(ltd); err == nil { sup.LeadTimeDays = n }
		}
		p.Suppliers = []models.ProductSupplier{sup}
	}

	// Images: image_1 … image_N
	for _, hdr := range headers {
		if strings.HasPrefix(hdr, "image_") {
			if url := getCol(row, colIdx, hdr); url != "" {
				role := "gallery"
				if hdr == "image_1" {
					role = "primary_image"
				}
				p.Assets = append(p.Assets, models.ProductAsset{
					AssetID:   uuid.New().String(),
					URL:       url,
					Role:      role,
					SortOrder: len(p.Assets),
				})
			}
		}
	}

	// Attributes
	attrs := map[string]interface{}{}
	if sku := getCol(row, colIdx, "sku"); sku != "" {
		attrs["source_sku"] = sku
	}
	if price := getCol(row, colIdx, "list_price"); price != "" {
		if f, err := strconv.ParseFloat(price, 64); err == nil {
			attrs["source_price"] = f
		}
	}
	if curr := getCol(row, colIdx, "currency"); curr != "" {
		attrs["source_currency"] = curr
	}

	// Freeform attributes from attribute_N_name / attribute_N_value pairs
	for name, val := range extractAttributes(row, colIdx, headers, "attribute") {
		attrs[name] = val
	}

	if len(attrs) > 0 {
		p.Attributes = attrs
	}

	// Bundle components
	if p.ProductType == "bundle" {
		if comp := getCol(row, colIdx, "bundle_component_skus"); comp != "" {
			for i, part := range strings.Split(comp, "|") {
				pieces := strings.Split(part, ":")
				if len(pieces) == 2 {
					qty, _ := strconv.Atoi(pieces[1])
					p.BundleComponents = append(p.BundleComponents, models.BundleComponent{
						ComponentID: uuid.New().String(),
						ProductID:   pieces[0],
						Quantity:    qty,
						IsRequired:  true,
						SortOrder:   i,
					})
				}
			}
		}
	}

	return p
}

func (h *ExportHandler) rowToUpdates(row []string, colIdx map[string]int, headers []string) map[string]interface{} {
	updates := map[string]interface{}{}
	setStr := func(col, field string) {
		if v := getCol(row, colIdx, col); v != "" {
			updates[field] = v
		}
	}
	setStr("title", "title")
	setStr("subtitle", "subtitle")
	setStr("description", "description")
	setStr("brand", "brand")
	setStr("status", "status")
	setStr("attribute_set_id", "attribute_set_id")
	setStr("storage_group_id", "storage_group_id")

	if cats := getCol(row, colIdx, "categories"); cats != "" {
		updates["category_ids"] = strings.Split(cats, "|")
	}
	if tags := getCol(row, colIdx, "tags"); tags != "" {
		updates["tags"] = strings.Split(tags, "|")
	}
	if kf := getCol(row, colIdx, "key_features"); kf != "" {
		updates["key_features"] = strings.Split(kf, "|")
	}

	// Lifecycle flags — only update when explicitly set to "true" or "false"
	if v := getCol(row, colIdx, "use_serial_numbers"); v == "true" || v == "false" {
		updates["use_serial_numbers"] = v == "true"
	}
	if v := getCol(row, colIdx, "end_of_life"); v == "true" || v == "false" {
		updates["end_of_life"] = v == "true"
	}

	// Shipping dimensions
	sDims := map[string]interface{}{}
	if l := getCol(row, colIdx, "shipping_length"); l != "" {
		if f, err := strconv.ParseFloat(l, 64); err == nil { sDims["length"] = f }
	}
	if w := getCol(row, colIdx, "shipping_width"); w != "" {
		if f, err := strconv.ParseFloat(w, 64); err == nil { sDims["width"] = f }
	}
	if ht := getCol(row, colIdx, "shipping_height"); ht != "" {
		if f, err := strconv.ParseFloat(ht, 64); err == nil { sDims["height"] = f }
	}
	if u := getCol(row, colIdx, "shipping_dimension_unit"); u != "" { sDims["unit"] = u }
	if len(sDims) > 0 { updates["shipping_dimensions"] = sDims }

	if swv := getCol(row, colIdx, "shipping_weight_value"); swv != "" {
		if f, err := strconv.ParseFloat(swv, 64); err == nil {
			unit := getCol(row, colIdx, "shipping_weight_unit")
			if unit == "" { unit = "g" }
			updates["shipping_weight"] = map[string]interface{}{"value": f, "unit": unit}
		}
	}

	// Primary supplier
	if supSKU := getCol(row, colIdx, "supplier_sku"); supSKU != "" {
		sup := map[string]interface{}{
			"supplier_sku":  supSKU,
			"supplier_name": getCol(row, colIdx, "supplier_name"),
			"currency":      getCol(row, colIdx, "supplier_currency"),
			"is_default":    true,
			"priority":      1,
		}
		if cost := getCol(row, colIdx, "supplier_cost"); cost != "" {
			if f, err := strconv.ParseFloat(cost, 64); err == nil { sup["unit_cost"] = f }
		}
		if ltd := getCol(row, colIdx, "supplier_lead_time_days"); ltd != "" {
			if n, err := strconv.Atoi(ltd); err == nil { sup["lead_time_days"] = n }
		}
		updates["suppliers"] = []interface{}{sup}
	}

	// Freeform attributes
	attrs := map[string]interface{}{}
	for name, val := range extractAttributes(row, colIdx, headers, "attribute") {
		attrs[name] = val
	}
	if len(attrs) > 0 {
		updates["attributes"] = attrs
	}

	return updates
}

func (h *ExportHandler) rowToVariant(row []string, colIdx map[string]int, headers []string, tenantID, parentProductID string) *models.Variant {
	v := &models.Variant{
		TenantID:  tenantID,
		ProductID: parentProductID,
		SKU:       getCol(row, colIdx, "sku"),
		Status:    getCol(row, colIdx, "status"),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if v.Status == "" {
		v.Status = "active"
	}
	if t := getCol(row, colIdx, "title"); t != "" {
		v.Title = &t
	}
	if a := getCol(row, colIdx, "alias"); a != "" {
		v.Alias = &a
	}
	if b := getCol(row, colIdx, "barcode"); b != "" {
		v.Barcode = &b
	}

	idents := &models.ProductIdentifiers{}
	hasIdent := false
	for _, pair := range []struct {
		col   string
		field **string
	}{
		{"ean", &idents.EAN}, {"upc", &idents.UPC}, {"asin", &idents.ASIN},
		{"isbn", &idents.ISBN}, {"mpn", &idents.MPN}, {"gtin", &idents.GTIN},
	} {
		if val := getCol(row, colIdx, pair.col); val != "" {
			s := val
			*pair.field = &s
			hasIdent = true
		}
	}
	if hasIdent {
		v.Identifiers = idents
	}

	pricing := &models.VariantPricing{}
	hasPricing := false
	curr := getCol(row, colIdx, "currency")
	if curr == "" {
		curr = "GBP"
	}
	if lp := getCol(row, colIdx, "list_price"); lp != "" {
		if f, err := strconv.ParseFloat(lp, 64); err == nil {
			pricing.ListPrice = &models.Money{Amount: f, Currency: curr}
			hasPricing = true
		}
	}
	if rrp := getCol(row, colIdx, "rrp"); rrp != "" {
		if f, err := strconv.ParseFloat(rrp, 64); err == nil {
			pricing.RRP = &models.Money{Amount: f, Currency: curr}
			hasPricing = true
		}
	}
	if cost := getCol(row, colIdx, "cost_price"); cost != "" {
		if f, err := strconv.ParseFloat(cost, 64); err == nil {
			pricing.Cost = &models.Money{Amount: f, Currency: curr}
			hasPricing = true
		}
	}
	if hasPricing {
		v.Pricing = pricing
	}

	dims := &models.Dimensions{Unit: getCol(row, colIdx, "dimension_unit")}
	hasDims := false
	if l := getCol(row, colIdx, "length"); l != "" {
		if f, err := strconv.ParseFloat(l, 64); err == nil {
			dims.Length = &f
			hasDims = true
		}
	}
	if w := getCol(row, colIdx, "width"); w != "" {
		if f, err := strconv.ParseFloat(w, 64); err == nil {
			dims.Width = &f
			hasDims = true
		}
	}
	if ht := getCol(row, colIdx, "height"); ht != "" {
		if f, err := strconv.ParseFloat(ht, 64); err == nil {
			dims.Height = &f
			hasDims = true
		}
	}
	if hasDims {
		if dims.Unit == "" {
			dims.Unit = "cm"
		}
		v.Dimensions = dims
	}

	if wv := getCol(row, colIdx, "weight_value"); wv != "" {
		if f, err := strconv.ParseFloat(wv, 64); err == nil {
			unit := getCol(row, colIdx, "weight_unit")
			if unit == "" {
				unit = "g"
			}
			v.Weight = &models.Weight{Value: &f, Unit: unit}
		}
	}

	// Variant attributes from variant_attr_N_name / variant_attr_N_value pairs
	attrs := map[string]interface{}{}
	for name, val := range extractAttributes(row, colIdx, headers, "variant_attr") {
		attrs[name] = val
	}
	if len(attrs) > 0 {
		v.Attributes = attrs
	}

	return v
}

func (h *ExportHandler) rowToVariantUpdates(row []string, colIdx map[string]int, headers []string) map[string]interface{} {
	updates := map[string]interface{}{}
	setStr := func(col, field string) {
		if v := getCol(row, colIdx, col); v != "" { updates[field] = v }
	}
	setStr("title", "title")
	setStr("status", "status")
	setStr("sku", "sku")
	setStr("alias", "alias")
	setStr("barcode", "barcode")

	// Identifiers
	idents := map[string]interface{}{}
	for _, pair := range []struct{ col, field string }{
		{"ean", "ean"}, {"upc", "upc"}, {"asin", "asin"},
		{"isbn", "isbn"}, {"mpn", "mpn"}, {"gtin", "gtin"},
	} {
		if v := getCol(row, colIdx, pair.col); v != "" { idents[pair.field] = v }
	}
	if len(idents) > 0 { updates["identifiers"] = idents }

	// Pricing
	curr := getCol(row, colIdx, "currency")
	if curr == "" { curr = "GBP" }
	pricing := map[string]interface{}{}
	if lp := getCol(row, colIdx, "list_price"); lp != "" {
		if f, err := strconv.ParseFloat(lp, 64); err == nil {
			pricing["list_price"] = map[string]interface{}{"amount": f, "currency": curr}
		}
	}
	if rrp := getCol(row, colIdx, "rrp"); rrp != "" {
		if f, err := strconv.ParseFloat(rrp, 64); err == nil {
			pricing["rrp"] = map[string]interface{}{"amount": f, "currency": curr}
		}
	}
	if cost := getCol(row, colIdx, "cost_price"); cost != "" {
		if f, err := strconv.ParseFloat(cost, 64); err == nil {
			pricing["cost"] = map[string]interface{}{"amount": f, "currency": curr}
		}
	}
	if len(pricing) > 0 { updates["pricing"] = pricing }

	// Dimensions & weight
	dims := map[string]interface{}{}
	for _, pair := range []struct{ col, field string }{
		{"length", "length"}, {"width", "width"}, {"height", "height"},
	} {
		if v := getCol(row, colIdx, pair.col); v != "" {
			if f, err := strconv.ParseFloat(v, 64); err == nil { dims[pair.field] = f }
		}
	}
	if u := getCol(row, colIdx, "dimension_unit"); u != "" { dims["unit"] = u }
	if len(dims) > 0 { updates["dimensions"] = dims }

	if wv := getCol(row, colIdx, "weight_value"); wv != "" {
		if f, err := strconv.ParseFloat(wv, 64); err == nil {
			unit := getCol(row, colIdx, "weight_unit")
			if unit == "" { unit = "g" }
			updates["weight"] = map[string]interface{}{"value": f, "unit": unit}
		}
	}

	// Variant attributes
	attrs := map[string]interface{}{}
	for name, val := range extractAttributes(row, colIdx, headers, "variant_attr") {
		attrs[name] = val
	}
	if len(attrs) > 0 { updates["attributes"] = attrs }

	return updates
}

// ============================================================================
// STOCK IMPORT
// ============================================================================

func (h *ExportHandler) ImportStockDryRun(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	rows, headers, err := h.parseUpload(c)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	results, summary := h.validateStockRows(c, tenantID, headers, rows)
	c.JSON(200, gin.H{
		"ok":      summary["errors"] == 0,
		"summary": summary,
		"rows":    results,
		"message": fmt.Sprintf("Validated %d rows: %d valid, %d errors", summary["total"], summary["valid"], summary["errors"]),
	})
}

func (h *ExportHandler) ImportStock(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	rows, headers, err := h.parseUpload(c)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	results, summary := h.validateStockRows(c, tenantID, headers, rows)
	if summary["errors"].(int) > 0 {
		c.JSON(400, gin.H{"ok": false, "message": "Validation failed. Fix errors and retry.", "summary": summary, "rows": results})
		return
	}

	colIdx := map[string]int{}
	for i, h := range headers {
		colIdx[h] = i
	}
	skuMap := h.buildSKUMap(c, tenantID)

	updated, failed := 0, 0
	for i, row := range rows {
		sku := getCol(row, colIdx, "sku")
		qtyStr := getCol(row, colIdx, "quantity")
		qty, _ := strconv.Atoi(qtyStr)
		if results[i].Action == "skip" {
			continue
		}
		if variantID, ok := skuMap["variant:"+sku]; ok {
			if err := h.repo.UpdateVariant(c.Request.Context(), tenantID, variantID, map[string]interface{}{"quantity": qty, "updated_at": time.Now()}); err != nil {
				failed++
				continue
			}
			updated++
		} else if productID, ok := skuMap["product:"+sku]; ok {
			if err := h.repo.UpdateProduct(c.Request.Context(), tenantID, productID, map[string]interface{}{"attributes.quantity": qty, "updated_at": time.Now()}); err != nil {
				failed++
				continue
			}
			updated++
		} else {
			failed++
		}
	}

	c.JSON(200, gin.H{
		"ok":      failed == 0,
		"updated": updated,
		"failed":  failed,
		"total":   updated + failed,
		"message": fmt.Sprintf("Stock update complete: %d updated, %d failed", updated, failed),
	})
}

func (h *ExportHandler) validateStockRows(c *gin.Context, tenantID string, headers []string, rows [][]string) ([]importRowResult, map[string]interface{}) {
	colIdx := map[string]int{}
	for i, h := range headers {
		colIdx[h] = i
	}
	if _, ok := colIdx["sku"]; !ok {
		return nil, map[string]interface{}{"total": 0, "valid": 0, "errors": 1, "error": "Missing 'sku' column"}
	}
	if _, ok := colIdx["quantity"]; !ok {
		return nil, map[string]interface{}{"total": 0, "valid": 0, "errors": 1, "error": "Missing 'quantity' column"}
	}

	skuMap := h.buildSKUMap(c, tenantID)
	var results []importRowResult
	valid, errors_ := 0, 0

	for i, row := range rows {
		rowNum := i + 2
		sku := getCol(row, colIdx, "sku")
		qtyStr := getCol(row, colIdx, "quantity")
		result := importRowResult{RowNum: rowNum, SKU: sku, Action: "update", Errors: map[string]string{}, Warnings: map[string]string{}}

		if sku == "" {
			result.Errors["sku"] = "SKU is required"
		} else if _, okV := skuMap["variant:"+sku]; !okV {
			if _, okP := skuMap["product:"+sku]; !okP {
				result.Errors["sku"] = fmt.Sprintf("SKU '%s' not found in database", sku)
				result.Action = "skip"
			}
		}
		if qtyStr == "" {
			result.Errors["quantity"] = "Quantity is required"
		} else if q, err := strconv.Atoi(qtyStr); err != nil || q < 0 {
			result.Errors["quantity"] = "Must be a non-negative integer"
		}

		if len(result.Errors) > 0 {
			errors_++
		} else {
			valid++
		}
		results = append(results, result)
	}

	return results, map[string]interface{}{"total": len(rows), "valid": valid, "errors": errors_}
}

// ============================================================================
// PRICE IMPORT
// ============================================================================

func (h *ExportHandler) ImportPricesDryRun(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	rows, headers, err := h.parseUpload(c)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	results, summary := h.validatePriceRows(c, tenantID, headers, rows)
	c.JSON(200, gin.H{
		"ok":      summary["errors"] == 0,
		"summary": summary,
		"rows":    results,
		"message": fmt.Sprintf("Validated %d rows: %d valid, %d errors", summary["total"], summary["valid"], summary["errors"]),
	})
}

func (h *ExportHandler) ImportPrices(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	rows, headers, err := h.parseUpload(c)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	results, summary := h.validatePriceRows(c, tenantID, headers, rows)
	if summary["errors"].(int) > 0 {
		c.JSON(400, gin.H{"ok": false, "message": "Validation failed.", "summary": summary, "rows": results})
		return
	}

	colIdx := map[string]int{}
	for i, h := range headers {
		colIdx[h] = i
	}
	skuMap := h.buildSKUMap(c, tenantID)

	updated, failed := 0, 0
	for i, row := range rows {
		if results[i].Action == "skip" {
			continue
		}
		sku := getCol(row, colIdx, "sku")
		curr := getCol(row, colIdx, "currency")
		if curr == "" {
			curr = "GBP"
		}

		if variantID, ok := skuMap["variant:"+sku]; ok {
			updates := map[string]interface{}{"updated_at": time.Now()}
			if lp := getCol(row, colIdx, "list_price"); lp != "" {
				if f, err := strconv.ParseFloat(lp, 64); err == nil {
					updates["pricing.list_price"] = models.Money{Amount: f, Currency: curr}
				}
			}
			if rrp := getCol(row, colIdx, "rrp"); rrp != "" {
				if f, err := strconv.ParseFloat(rrp, 64); err == nil {
					updates["pricing.rrp"] = models.Money{Amount: f, Currency: curr}
				}
			}
			if cost := getCol(row, colIdx, "cost_price"); cost != "" {
				if f, err := strconv.ParseFloat(cost, 64); err == nil {
					updates["pricing.cost"] = models.Money{Amount: f, Currency: curr}
				}
			}
			if err := h.repo.UpdateVariant(c.Request.Context(), tenantID, variantID, updates); err != nil {
				failed++
				continue
			}
			updated++
		} else if productID, ok := skuMap["product:"+sku]; ok {
			updates := map[string]interface{}{"updated_at": time.Now()}
			if lp := getCol(row, colIdx, "list_price"); lp != "" {
				if f, err := strconv.ParseFloat(lp, 64); err == nil {
					updates["attributes.source_price"] = f
				}
			}
			if curr != "" {
				updates["attributes.source_currency"] = curr
			}
			if err := h.repo.UpdateProduct(c.Request.Context(), tenantID, productID, updates); err != nil {
				failed++
				continue
			}
			updated++
		} else {
			failed++
		}
	}

	c.JSON(200, gin.H{
		"ok": failed == 0, "updated": updated, "failed": failed, "total": updated + failed,
		"message": fmt.Sprintf("Price update complete: %d updated, %d failed", updated, failed),
	})
}

func (h *ExportHandler) validatePriceRows(c *gin.Context, tenantID string, headers []string, rows [][]string) ([]importRowResult, map[string]interface{}) {
	colIdx := map[string]int{}
	for i, h := range headers {
		colIdx[h] = i
	}
	if _, ok := colIdx["sku"]; !ok {
		return nil, map[string]interface{}{"total": 0, "valid": 0, "errors": 1, "error": "Missing 'sku' column"}
	}

	skuMap := h.buildSKUMap(c, tenantID)
	var results []importRowResult
	valid, errors_ := 0, 0

	for i, row := range rows {
		rowNum := i + 2
		sku := getCol(row, colIdx, "sku")
		result := importRowResult{RowNum: rowNum, SKU: sku, Action: "update", Errors: map[string]string{}, Warnings: map[string]string{}}

		if sku == "" {
			result.Errors["sku"] = "SKU is required"
		} else if _, okV := skuMap["variant:"+sku]; !okV {
			if _, okP := skuMap["product:"+sku]; !okP {
				result.Errors["sku"] = fmt.Sprintf("SKU '%s' not found", sku)
				result.Action = "skip"
			}
		}
		hasPrice := false
		for _, col := range []string{"list_price", "rrp", "cost_price", "sale_price"} {
			if v := getCol(row, colIdx, col); v != "" {
				if _, err := strconv.ParseFloat(v, 64); err != nil {
					result.Errors[col] = "Must be a valid number"
				} else {
					hasPrice = true
				}
			}
		}
		if !hasPrice && len(result.Errors) == 0 {
			result.Errors["list_price"] = "At least one price field is required"
		}

		if len(result.Errors) > 0 {
			errors_++
		} else {
			valid++
		}
		results = append(results, result)
	}
	return results, map[string]interface{}{"total": len(rows), "valid": valid, "errors": errors_}
}

// ============================================================================
// SHARED: SKU LOOKUP BUILDER
// ============================================================================

func (h *ExportHandler) buildSKUMap(c *gin.Context, tenantID string) map[string]string {
	skuMap := map[string]string{}
	products, _, _ := h.repo.ListProducts(c.Request.Context(), tenantID, map[string]interface{}{}, 0, 0)
	for _, p := range products {
		if p.Attributes != nil {
			if s, ok := p.Attributes["source_sku"].(string); ok && s != "" {
				skuMap["product:"+s] = p.ProductID
			}
			if s, ok := p.Attributes["sku"].(string); ok && s != "" {
				skuMap["product:"+s] = p.ProductID
			}
		}
		if p.Identifiers != nil && p.Identifiers.ASIN != nil && *p.Identifiers.ASIN != "" {
			skuMap["product:"+*p.Identifiers.ASIN] = p.ProductID
		}
	}
	variants, _, _ := h.repo.ListVariants(c.Request.Context(), tenantID, map[string]interface{}{}, 0, 0)
	for _, v := range variants {
		skuMap["variant:"+v.SKU] = v.VariantID
	}
	return skuMap
}

// ============================================================================
// HELPERS
// ============================================================================

func getCol(row []string, colIdx map[string]int, col string) string {
	if idx, ok := colIdx[col]; ok && idx < len(row) {
		return strings.TrimSpace(row[idx])
	}
	return ""
}

// ============================================================================
// UNIFIED EXPORT ENDPOINT  POST /api/v1/export
// ============================================================================

type unifiedExportRequest struct {
	Type            string   `json:"type"`
	Format          string   `json:"format"`
	ChannelIDs      []string `json:"channel_ids"`
	IncludeVariants bool     `json:"include_variants"`
	IncludeBundles  bool     `json:"include_bundles"`
}

func (h *ExportHandler) UnifiedExport(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req unifiedExportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "invalid request: " + err.Error()})
		return
	}
	if req.Format == "" {
		req.Format = "csv"
	}
	if req.Format != "csv" && req.Format != "xlsx" {
		c.JSON(400, gin.H{"error": "format must be csv or xlsx"})
		return
	}

	ctx := c.Request.Context()

	switch req.Type {
	case "products":
		filters := map[string]interface{}{}
		if req.Format == "xlsx" {
			result, err := h.exportService.ExportProducts(ctx, tenantID, filters)
			if err != nil {
				c.JSON(500, gin.H{"error": err.Error()})
				return
			}
			c.JSON(200, gin.H{"ok": true, "format": "xlsx_data", "headers": result.Headers, "rows": result.Rows, "filename": strings.Replace(result.Filename, ".csv", ".xlsx", 1)})
			return
		}
		csvBytes, filename, err := h.exportService.ExportProductsCSV(ctx, tenantID, filters)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
		c.Data(200, "text/csv; charset=utf-8", csvBytes)

	case "prices":
		result, err := h.exportService.ExportPrices(ctx, tenantID)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		if req.Format == "xlsx" {
			c.JSON(200, gin.H{"ok": true, "format": "xlsx_data", "headers": result.Headers, "rows": result.Rows, "filename": strings.Replace(result.Filename, ".csv", ".xlsx", 1)})
			return
		}
		var buf strings.Builder
		w := csv.NewWriter(&buf)
		w.Write(result.Headers)
		for _, row := range result.Rows {
			w.Write(row)
		}
		w.Flush()
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", result.Filename))
		c.Data(200, "text/csv; charset=utf-8", []byte(buf.String()))

	case "inventory_basic", "inventory_advanced":
		result, err := h.exportService.ExportStock(ctx, tenantID)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		if req.Format == "xlsx" {
			c.JSON(200, gin.H{"ok": true, "format": "xlsx_data", "headers": result.Headers, "rows": result.Rows, "filename": strings.Replace(result.Filename, ".csv", ".xlsx", 1)})
			return
		}
		var buf strings.Builder
		w := csv.NewWriter(&buf)
		w.Write(result.Headers)
		for _, row := range result.Rows {
			w.Write(row)
		}
		w.Flush()
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", result.Filename))
		c.Data(200, "text/csv; charset=utf-8", []byte(buf.String()))

	case "listings":
		c.JSON(501, gin.H{"error": "listings export not yet implemented — connect to marketplace repository"})

	default:
		c.JSON(400, gin.H{"error": "type must be: products, listings, prices, inventory_basic, inventory_advanced"})
	}
}

// Ensure unused imports don't cause compile errors
var _ = io.EOF
var _ = http.StatusOK
var _ = sort.Strings

// ============================================================================
// EXPORT: RMAs
// ============================================================================

func (h *ExportHandler) ExportRMAs(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	iter := h.repo.GetClient().Collection("tenants").Doc(tenantID).Collection("rmas").
		OrderBy("created_at", firestore.Desc).Limit(5000).Documents(ctx)

	var buf strings.Builder
	w := csv.NewWriter(&buf)
	w.Write([]string{
		"rma_id", "rma_number", "order_id", "order_number", "channel", "status",
		"rma_type", "customer_name", "customer_email",
		"refund_action", "refund_amount", "refund_currency",
		"notes", "created_at", "updated_at",
	})

	for {
		doc, err := iter.Next()
		if err != nil {
			break
		}
		d := doc.Data()
		str := func(key string) string {
			if v, ok := d[key]; ok {
				return fmt.Sprintf("%v", v)
			}
			return ""
		}
		customerName, customerEmail := "", ""
		if cust, ok := d["customer"].(map[string]interface{}); ok {
			customerName = fmt.Sprintf("%v", cust["name"])
			customerEmail = fmt.Sprintf("%v", cust["email"])
		}
		w.Write([]string{
			str("rma_id"), str("rma_number"), str("order_id"), str("order_number"),
			str("channel"), str("status"), str("rma_type"),
			customerName, customerEmail,
			str("refund_action"), str("refund_amount"), str("refund_currency"),
			str("notes"), str("created_at"), str("updated_at"),
		})
	}
	w.Flush()

	filename := fmt.Sprintf("rmas_%s.csv", time.Now().Format("2006-01-02"))
	c.Header("Content-Disposition", "attachment; filename="+filename)
	c.Data(http.StatusOK, "text/csv; charset=utf-8", []byte(buf.String()))
}

// ============================================================================
// EXPORT: Purchase Orders
// ============================================================================

func (h *ExportHandler) ExportPurchaseOrders(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	iter := h.repo.GetClient().Collection("tenants").Doc(tenantID).Collection("purchase_orders").
		OrderBy("created_at", firestore.Desc).Limit(5000).Documents(ctx)

	var buf strings.Builder
	w := csv.NewWriter(&buf)
	w.Write([]string{
		"po_id", "po_number", "supplier_name", "status", "order_method",
		"total_cost", "currency", "created_at", "sent_at", "expected_at",
	})

	for {
		doc, err := iter.Next()
		if err != nil {
			break
		}
		d := doc.Data()
		str := func(key string) string {
			if v, ok := d[key]; ok {
				return fmt.Sprintf("%v", v)
			}
			return ""
		}
		w.Write([]string{
			str("po_id"), str("po_number"), str("supplier_name"), str("status"), str("order_method"),
			str("total_cost"), str("currency"),
			str("created_at"), str("sent_at"), str("expected_at"),
		})
	}
	w.Flush()

	filename := fmt.Sprintf("purchase_orders_%s.csv", time.Now().Format("2006-01-02"))
	c.Header("Content-Disposition", "attachment; filename="+filename)
	c.Data(http.StatusOK, "text/csv; charset=utf-8", []byte(buf.String()))
}

// ============================================================================
// EXPORT: Shipments
// ============================================================================

func (h *ExportHandler) ExportShipments(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	iter := h.repo.GetClient().Collection("tenants").Doc(tenantID).Collection("shipments").
		OrderBy("created_at", firestore.Desc).Limit(5000).Documents(ctx)

	var buf strings.Builder
	w := csv.NewWriter(&buf)
	w.Write([]string{
		"shipment_id", "order_ids", "carrier_id", "service_code", "tracking_number",
		"status", "fulfilment_source_id", "fulfilment_source_type",
		"shipped_at", "created_at",
	})

	for {
		doc, err := iter.Next()
		if err != nil {
			break
		}
		d := doc.Data()
		str := func(key string) string {
			if v, ok := d[key]; ok {
				return fmt.Sprintf("%v", v)
			}
			return ""
		}
		orderIDsStr := ""
		if oids, ok := d["order_ids"].([]interface{}); ok {
			parts := make([]string, 0, len(oids))
			for _, o := range oids {
				parts = append(parts, fmt.Sprintf("%v", o))
			}
			orderIDsStr = strings.Join(parts, "|")
		}
		w.Write([]string{
			str("shipment_id"), orderIDsStr, str("carrier_id"), str("service_code"), str("tracking_number"),
			str("status"), str("fulfilment_source_id"), str("fulfilment_source_type"),
			str("shipped_at"), str("created_at"),
		})
	}
	w.Flush()

	filename := fmt.Sprintf("shipments_%s.csv", time.Now().Format("2006-01-02"))
	c.Header("Content-Disposition", "attachment; filename="+filename)
	c.Data(http.StatusOK, "text/csv; charset=utf-8", []byte(buf.String()))
}
