package handlers

// ============================================================================
// TEMU WIZARD HANDLER
// ============================================================================
//
// Routes:
//   GET  /api/v1/temu-wizard/status          — Current wizard stage for tenant
//   PUT  /api/v1/temu-wizard/status          — Update wizard stage
//   POST /api/v1/temu-wizard/generate-xlsx   — Generate pricing spreadsheet (excelize)
//   GET  /api/v1/temu-wizard/download-xlsx   — Serve the generated XLSX file
//   POST /api/v1/temu-wizard/upload-xlsx     — Parse uploaded XLSX, validate
//   POST /api/v1/temu-wizard/generate-listings       — Batch AI listing generation (sync)
//   POST /api/v1/temu-wizard/generate-listings-async — Async job for large batches
//   GET  /api/v1/temu-wizard/generation-job/:job_id  — Poll async job progress
// ============================================================================

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"

	"module-a/repository"
	"module-a/services"
)

// ─── Temu Brand Registry (static list for Sheet 2 dropdown validation) ──────

var temuBrandRegistry = []string{
	"Unbranded",
	"Generic",
	"OEM",
	"Custom Brand",
	"Private Label",
	"House Brand",
	"No Brand",
}

type TemuWizardHandler struct {
	client         *firestore.Client
	repo           *repository.MarketplaceRepository
	productService *services.ProductService
	aiService      *services.AIService
	storageService *services.StorageService // optional; nil falls back to Firestore blob
}

func NewTemuWizardHandler(
	client *firestore.Client,
	repo *repository.MarketplaceRepository,
	productService *services.ProductService,
	aiService *services.AIService,
) *TemuWizardHandler {
	return &TemuWizardHandler{
		client:         client,
		repo:           repo,
		productService: productService,
		aiService:      aiService,
	}
}

// SetStorageService wires the GCS storage service for XLSX uploads.
// When set, generated XLSX files are stored in GCS and served via signed URLs
// instead of being stored as Firestore blobs (which have a 1MB limit).
func (h *TemuWizardHandler) SetStorageService(svc *services.StorageService) {
	h.storageService = svc
}

// ─── GET /api/v1/temu-wizard/status ─────────────────────────────────────────

func (h *TemuWizardHandler) GetStatus(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	doc, err := h.client.Collection("tenants").Doc(tenantID).Get(ctx)
	if err != nil {
		c.JSON(200, gin.H{"stage": ""})
		return
	}
	data := doc.Data()
	stage, _ := data["temu_wizard_stage"].(string)
	sourceChannel, _ := data["source_channel"].(string)
	estimatedOrders, _ := data["estimated_monthly_orders"].(int64)

	c.JSON(200, gin.H{
		"stage":                    stage,
		"source_channel":           sourceChannel,
		"estimated_monthly_orders": estimatedOrders,
	})
}

// ─── PUT /api/v1/temu-wizard/status ─────────────────────────────────────────

func (h *TemuWizardHandler) UpdateStatus(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	var req struct {
		Stage string `json:"stage" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	validStages := map[string]bool{
		"connected": true, "importing": true, "awaiting_upload": true,
		"uploaded": true, "generating": true, "reviewing": true, "completed": true,
	}
	if !validStages[req.Stage] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid stage"})
		return
	}

	_, err := h.client.Collection("tenants").Doc(tenantID).Update(ctx, []firestore.Update{
		{Path: "temu_wizard_stage", Value: req.Stage},
		{Path: "updated_at", Value: time.Now()},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update stage"})
		return
	}

	c.JSON(200, gin.H{"stage": req.Stage})
}

// ─── POST /api/v1/temu-wizard/generate-xlsx ─────────────────────────────────
//
// Generates a real XLSX file using excelize with:
//   - Sheet 1 (Products): SKU, Title, Brand, Source Price, Temu Price, Temu Brand, Create Listing
//     + extra price columns for additional channels
//   - Sheet 2 (Temu Brand Registry): valid brand names for dropdown validation

func (h *TemuWizardHandler) GenerateXLSX(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	var req struct {
		SourceChannel      string   `json:"source_channel"`
		CredentialID       string   `json:"credential_id"`
		AdditionalChannels []string `json:"additional_channels"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Fetch all products for this tenant
	products, err := h.fetchAllProducts(ctx, tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to fetch products: %v", err)})
		return
	}

	if len(products) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No products found. Please import products first."})
		return
	}

	// ── Build the XLSX workbook ──────────────────────────────────────────────
	f := excelize.NewFile()
	defer f.Close()

	// Sheet 1: Products
	sheetName := "Products"
	f.SetSheetName("Sheet1", sheetName)

	// Build header row
	headers := []string{"SKU", "Title", "Brand", "Source Price", "Temu Price", "Temu Brand", "Create Listing"}
	for _, ch := range req.AdditionalChannels {
		name := strings.ToUpper(ch[:1]) + ch[1:]
		headers = append(headers, name+" Price")
	}

	// Header style
	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 11, Color: "FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"2E75B6"}},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		Border: []excelize.Border{
			{Type: "bottom", Color: "1F4E79", Style: 2},
		},
	})

	// Write headers
	for col, header := range headers {
		cell, _ := excelize.CoordinatesToCellName(col+1, 1)
		f.SetCellValue(sheetName, cell, header)
		f.SetCellStyle(sheetName, cell, cell, headerStyle)
	}

	// Set column widths
	colWidths := map[string]float64{
		"A": 18, "B": 45, "C": 20, "D": 14, "E": 14, "F": 22, "G": 14,
	}
	for col, w := range colWidths {
		f.SetColWidth(sheetName, col, col, w)
	}

	// Write product rows
	for i, p := range products {
		row := i + 2
		sku, _ := p["sku"].(string)
		title, _ := p["title"].(string)
		brand, _ := p["brand"].(string)
		price := extractPrice(p)

		f.SetCellValue(sheetName, cellName(1, row), sku)
		f.SetCellValue(sheetName, cellName(2, row), title)
		f.SetCellValue(sheetName, cellName(3, row), brand)
		f.SetCellValue(sheetName, cellName(4, row), price)
		f.SetCellValue(sheetName, cellName(5, row), "")  // Temu Price (empty)
		f.SetCellValue(sheetName, cellName(6, row), "")  // Temu Brand (empty)
		f.SetCellValue(sheetName, cellName(7, row), "N") // Create Listing default N
	}

	// Sheet 2: Temu Brand Registry
	brandSheet := "Temu Brand Registry"
	f.NewSheet(brandSheet)
	brandHeaderStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true, Size: 11},
		Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"F97316"}},
	})
	f.SetCellValue(brandSheet, "A1", "Brand Name")
	f.SetCellStyle(brandSheet, "A1", "A1", brandHeaderStyle)
	f.SetColWidth(brandSheet, "A", "A", 30)
	for i, brand := range temuBrandRegistry {
		f.SetCellValue(brandSheet, cellName(1, i+2), brand)
	}

	// Data validation: Temu Brand column dropdown from Sheet 2
	lastBrandRow := len(temuBrandRegistry) + 1
	brandRange := fmt.Sprintf("'Temu Brand Registry'!$A$2:$A$%d", lastBrandRow)
	lastProductRow := len(products) + 1
	temuBrandColStart := cellName(6, 2)
	temuBrandColEnd := cellName(6, lastProductRow)

	dvBrand := excelize.NewDataValidation(true)
	dvBrand.Sqref = fmt.Sprintf("%s:%s", temuBrandColStart, temuBrandColEnd)
	dvBrand.SetSqrefDropList(brandRange)
	dvBrand.ShowErrorMessage = true
	dvBrand.ErrorTitle = ptrStr("Invalid Brand")
	dvBrand.Error = ptrStr("Please select a brand from the Temu Brand Registry sheet.")
	f.AddDataValidation(sheetName, dvBrand)

	// Data validation: Create Listing column Y/N dropdown
	dvCreate := excelize.NewDataValidation(true)
	dvCreate.Sqref = fmt.Sprintf("%s:%s", cellName(7, 2), cellName(7, lastProductRow))
	dvCreate.SetDropList([]string{"Y", "N"})
	f.AddDataValidation(sheetName, dvCreate)

	// Write to buffer
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to write XLSX: %v", err)})
		return
	}

	// Store XLSX — prefer GCS (no 1MB Firestore limit), fall back to Firestore blob.
	gcsPath := ""
	gcsURL := ""
	if h.storageService != nil {
		objectPath := fmt.Sprintf("%s/wizard-xlsx/temu-pricing-%d.xlsx", tenantID, time.Now().Unix())
		publicURL, uploadErr := h.storageService.Upload(ctx, objectPath, bytes.NewReader(buf.Bytes()),
			"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
		if uploadErr != nil {
			log.Printf("[TemuWizard] GCS upload failed, falling back to Firestore blob: %v", uploadErr)
		} else {
			gcsPath = objectPath
			gcsURL = publicURL
		}
	}

	xlsxDoc := map[string]interface{}{
		"tenant_id":           tenantID,
		"product_count":       len(products),
		"source_channel":      req.SourceChannel,
		"additional_channels": req.AdditionalChannels,
		"created_at":          time.Now(),
		"status":              "ready",
	}
	if gcsPath != "" {
		// GCS path stored; DownloadXLSX will generate a signed URL on demand
		xlsxDoc["gcs_path"] = gcsPath
		xlsxDoc["gcs_url"] = gcsURL
	} else {
		// Fallback: store raw bytes in Firestore (≤1MB limit)
		xlsxDoc["xlsx_data"] = buf.Bytes()
	}
	docRef := h.client.Collection("tenants").Doc(tenantID).Collection("wizard_xlsx").Doc("latest")
	_, err = docRef.Set(ctx, xlsxDoc)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to store XLSX"})
		return
	}

	// Also store product rows for later reference by upload/generation
	rowsRef := h.client.Collection("tenants").Doc(tenantID).Collection("wizard_xlsx_rows")
	batch := h.client.Batch()
	for i, p := range products {
		sku, _ := p["sku"].(string)
		title, _ := p["title"].(string)
		brand, _ := p["brand"].(string)
		price := extractPrice(p)
		imageURL, _ := p["image_url"].(string)
		productID, _ := p["product_id"].(string)

		row := map[string]interface{}{
			"product_id":     productID,
			"sku":            sku,
			"title":          title,
			"brand":          brand,
			"price":          price,
			"image_url":      imageURL,
			"temu_price":     "",
			"temu_brand":     "",
			"create_listing": "N",
		}
		for _, ch := range req.AdditionalChannels {
			row[ch+"_price"] = ""
		}

		ref := rowsRef.Doc(fmt.Sprintf("row_%d", i))
		batch.Set(ref, row)
		if (i+1)%500 == 0 {
			if _, bErr := batch.Commit(ctx); bErr != nil {
				log.Printf("[TemuWizard] batch write error at row %d: %v", i, bErr)
			}
			batch = h.client.Batch()
		}
	}
	if _, bErr := batch.Commit(ctx); bErr != nil {
		log.Printf("[TemuWizard] final batch write error: %v", bErr)
	}

	downloadURL := fmt.Sprintf("/api/v1/temu-wizard/download-xlsx?t=%d", time.Now().Unix())

	c.JSON(200, gin.H{
		"download_url":  downloadURL,
		"product_count": len(products),
		"columns":       headers,
	})
}

// ─── GET /api/v1/temu-wizard/download-xlsx ──────────────────────────────────
//
// Serves the generated XLSX file for download.

func (h *TemuWizardHandler) DownloadXLSX(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	doc, err := h.client.Collection("tenants").Doc(tenantID).
		Collection("wizard_xlsx").Doc("latest").Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "No XLSX file found. Please generate first."})
		return
	}

	data := doc.Data()

	// GCS path takes priority — generate a short-lived signed URL and redirect.
	if gcsPath, ok := data["gcs_path"].(string); ok && gcsPath != "" && h.storageService != nil {
		signedURL, signErr := h.storageService.GetSignedURL(ctx, gcsPath, 15) // 15-minute expiry
		if signErr != nil {
			log.Printf("[TemuWizard] signed URL generation failed for %s: %v", gcsPath, signErr)
			// Fall through to Firestore blob fallback
		} else {
			c.Redirect(http.StatusTemporaryRedirect, signedURL)
			return
		}
	}

	// Fallback: serve bytes stored directly in Firestore
	xlsxBytes, ok := data["xlsx_data"].([]byte)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "XLSX data corrupt or missing. Please regenerate."})
		return
	}

	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("Content-Disposition", "attachment; filename=temu-pricing-template.xlsx")
	c.Data(200, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", xlsxBytes)
}

// ─── POST /api/v1/temu-wizard/upload-xlsx ───────────────────────────────────
//
// Parses uploaded XLSX using excelize. Reads each row, validates Temu Price
// and Temu Brand for rows where Create Listing = Y, and stores data in Firestore.

func (h *TemuWizardHandler) UploadXLSX(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No file uploaded"})
		return
	}
	defer file.Close()

	if !strings.HasSuffix(strings.ToLower(header.Filename), ".xlsx") &&
		!strings.HasSuffix(strings.ToLower(header.Filename), ".xls") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Only .xlsx and .xls files are accepted"})
		return
	}

	// Read the file into memory for excelize
	fileBytes, err := io.ReadAll(file)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read uploaded file"})
		return
	}

	// Parse with excelize
	xlsxFile, err := excelize.OpenReader(bytes.NewReader(fileBytes))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid XLSX file: %v", err)})
		return
	}
	defer xlsxFile.Close()

	// Read all rows from the Products sheet
	sheetName := "Products"
	rows, err := xlsxFile.GetRows(sheetName)
	if err != nil {
		// Fallback: try Sheet1
		rows, err = xlsxFile.GetRows("Sheet1")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Could not read Products sheet"})
			return
		}
	}

	if len(rows) < 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Spreadsheet has no product rows"})
		return
	}

	// Parse header to find column indices
	headerRow := rows[0]
	colIndex := map[string]int{}
	for i, hdr := range headerRow {
		colIndex[strings.TrimSpace(strings.ToLower(hdr))] = i
	}

	skuCol := colIndex["sku"]
	titleCol := colIndex["title"]
	brandCol := colIndex["brand"]
	srcPriceCol := colIndex["source price"]
	temuPriceCol := colIndex["temu price"]
	temuBrandCol := colIndex["temu brand"]
	createCol := colIndex["create listing"]

	totalRows := len(rows) - 1
	createCount := 0
	var validationErrors []string

	// Clear existing wizard_xlsx_rows for fresh upload
	existingIter := h.client.Collection("tenants").Doc(tenantID).
		Collection("wizard_xlsx_rows").Documents(ctx)
	delBatch := h.client.Batch()
	delCount := 0
	for {
		existDoc, iterErr := existingIter.Next()
		if iterErr != nil {
			break
		}
		delBatch.Delete(existDoc.Ref)
		delCount++
		if delCount%500 == 0 {
			delBatch.Commit(ctx)
			delBatch = h.client.Batch()
		}
	}
	existingIter.Stop()
	if delCount > 0 {
		delBatch.Commit(ctx)
	}

	// Process data rows and store in Firestore
	rowsRef := h.client.Collection("tenants").Doc(tenantID).Collection("wizard_xlsx_rows")
	batch := h.client.Batch()
	batchCount := 0

	for i, row := range rows[1:] {
		getVal := func(idx int) string {
			if idx < len(row) {
				return strings.TrimSpace(row[idx])
			}
			return ""
		}

		sku := getVal(skuCol)
		title := getVal(titleCol)
		brand := getVal(brandCol)
		srcPrice := getVal(srcPriceCol)
		temuPrice := getVal(temuPriceCol)
		temuBrand := getVal(temuBrandCol)
		createListing := strings.ToUpper(getVal(createCol))

		if createListing == "Y" {
			createCount++
			if temuPrice == "" {
				validationErrors = append(validationErrors,
					fmt.Sprintf("Row %d (%s): Temu Price is required when Create Listing = Y", i+2, sku))
			}
		}

		rowDoc := map[string]interface{}{
			"sku":            sku,
			"title":          title,
			"brand":          brand,
			"price":          srcPrice,
			"temu_price":     temuPrice,
			"temu_brand":     temuBrand,
			"create_listing": createListing,
		}

		ref := rowsRef.Doc(fmt.Sprintf("row_%d", i))
		batch.Set(ref, rowDoc)
		batchCount++
		if batchCount%500 == 0 {
			if _, bErr := batch.Commit(ctx); bErr != nil {
				log.Printf("[TemuWizard] upload batch write error: %v", bErr)
			}
			batch = h.client.Batch()
			batchCount = 0
		}
	}
	if batchCount > 0 {
		if _, bErr := batch.Commit(ctx); bErr != nil {
			log.Printf("[TemuWizard] upload final batch write error: %v", bErr)
		}
	}

	// Store upload record
	uploadDoc := map[string]interface{}{
		"tenant_id":    tenantID,
		"filename":     header.Filename,
		"size":         header.Size,
		"uploaded_at":  time.Now(),
		"status":       "processed",
		"total_rows":   totalRows,
		"create_count": createCount,
	}
	h.client.Collection("tenants").Doc(tenantID).
		Collection("wizard_uploads").Doc("latest").Set(ctx, uploadDoc)

	isValid := len(validationErrors) == 0

	c.JSON(200, gin.H{
		"total_rows":        totalRows,
		"create_count":      createCount,
		"valid":             isValid,
		"filename":          header.Filename,
		"validation_errors": validationErrors,
	})
}

// ─── POST /api/v1/temu-wizard/generate-listings ─────────────────────────────
//
// Batch-generates Temu listings via the AI service for up to max_products.
// If the wizard XLSX rows include price data for additional_channels, this
// endpoint also generates AI drafts for those channels (4.1 cross-marketplace).
// Each product consumes 1 credit per channel generated.

func (h *TemuWizardHandler) GenerateListings(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	var req struct {
		CredentialID       string   `json:"credential_id"`
		MaxProducts        int      `json:"max_products"`
		AdditionalChannels []string `json:"additional_channels"` // channels to also generate for
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.MaxProducts <= 0 {
		req.MaxProducts = 100
	}
	if req.MaxProducts > 100 {
		req.MaxProducts = 100
	}

	// Fetch products marked for listing creation from the wizard XLSX rows
	products, err := h.fetchWizardProducts(ctx, tenantID, req.MaxProducts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to fetch products: %v", err)})
		return
	}

	if len(products) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No products found for listing generation"})
		return
	}

	useAI := h.aiService != nil && h.aiService.IsAvailable()

	temuDrafts := make([]map[string]interface{}, 0, len(products))
	// additional_channel -> list of drafts
	additionalDrafts := map[string][]map[string]interface{}{}
	creditsConsumed := 0

	for _, p := range products {
		productID, _ := p["product_id"].(string)
		sku, _ := p["sku"].(string)
		title, _ := p["title"].(string)
		temuBrand, _ := p["temu_brand"].(string)
		temuPrice, _ := p["temu_price"].(string)
		srcPrice, _ := p["price"].(string)
		imageURL, _ := p["image_url"].(string)

		if productID == "" {
			productID = sku
		}

		// ── Temu primary listing ────────────────────────────────────────────
		consumed, _ := h.consumeCredit(ctx, tenantID)
		if !consumed {
			temuDrafts = append(temuDrafts, map[string]interface{}{
				"product_id": productID, "sku": sku, "title": title, "error": "No credits remaining",
			})
			continue
		}
		creditsConsumed++

		var temuDraft map[string]interface{}
		if useAI {
			aiProduct := services.AIProductInput{
				Title:       title,
				Brand:       temuBrand,
				SKU:         sku,
				ImageURLs:   wizardFilterEmpty(imageURL),
				SourcePrice: wizardParsePrice(srcPrice),
			}
			result, aiErr := h.aiService.GenerateListingsSinglePhase(ctx, aiProduct, []string{"temu"})
			if aiErr == nil && len(result.Listings) > 0 {
				l := result.Listings[0]
				temuDraft = map[string]interface{}{
					"product_id":       productID,
					"sku":              sku,
					"source_title":     title,
					"temu_title":       l.Title,
					"temu_description": l.Description,
					"temu_brand":       temuBrand,
					"temu_price":       temuPrice,
					"temu_category":    l.CategoryName,
					"bullet_points":    l.BulletPoints,
					"image_url":        imageURL,
					"status":           "draft",
					"created_at":       time.Now(),
					"ai_confidence":    l.Confidence,
				}
			}
		}
		if temuDraft == nil {
			// Fallback: simple title construction without AI
			t := title
			if temuBrand != "" {
				t = title + " - " + temuBrand
			}
			temuDraft = map[string]interface{}{
				"product_id":       productID,
				"sku":              sku,
				"source_title":     title,
				"temu_title":       t,
				"temu_description": fmt.Sprintf("High quality %s. Fast shipping. Great value.", title),
				"temu_brand":       temuBrand,
				"temu_price":       temuPrice,
				"temu_category":    "",
				"image_url":        imageURL,
				"status":           "draft",
				"created_at":       time.Now(),
			}
		}
		temuDrafts = append(temuDrafts, temuDraft)
		h.client.Collection("tenants").Doc(tenantID).
			Collection("temu_drafts").Doc(productID).Set(ctx, temuDraft)

		// ── Additional channels (cross-marketplace free listing, 4.1) ────────
		for _, ch := range req.AdditionalChannels {
			chPriceKey := ch + "_price"
			chPrice, _ := p[chPriceKey].(string)
			if chPrice == "" {
				// No price provided for this channel — skip
				continue
			}

			chConsumed, _ := h.consumeCredit(ctx, tenantID)
			if !chConsumed {
				log.Printf("[TemuWizard] out of credits for additional channel %s, product %s", ch, sku)
				continue
			}
			creditsConsumed++

			var chDraft map[string]interface{}
			if useAI {
				aiProduct := services.AIProductInput{
					Title:       title,
					Brand:       temuBrand,
					SKU:         sku,
					ImageURLs:   wizardFilterEmpty(imageURL),
					SourcePrice: wizardParsePrice(chPrice),
				}
				result, aiErr := h.aiService.GenerateListingsSinglePhase(ctx, aiProduct, []string{ch})
				if aiErr == nil && len(result.Listings) > 0 {
					l := result.Listings[0]
					chDraft = map[string]interface{}{
						"product_id":   productID,
						"sku":          sku,
						"channel":      ch,
						"source_title": title,
						"title":        l.Title,
						"description":  l.Description,
						"bullet_points": l.BulletPoints,
						"brand":        temuBrand,
						"price":        chPrice,
						"category":     l.CategoryName,
						"image_url":    imageURL,
						"status":       "draft",
						"created_at":   time.Now(),
						"ai_confidence": l.Confidence,
					}
				}
			}
			if chDraft == nil {
				chDraft = map[string]interface{}{
					"product_id":  productID,
					"sku":         sku,
					"channel":     ch,
					"source_title": title,
					"title":       title,
					"description": fmt.Sprintf("High quality %s. Fast shipping. Great value.", title),
					"brand":       temuBrand,
					"price":       chPrice,
					"image_url":   imageURL,
					"status":      "draft",
					"created_at":  time.Now(),
				}
			}

			// Normalise channel name to collection key e.g. "amazon" -> "amazon_drafts"
			collName := ch + "_drafts"
			h.client.Collection("tenants").Doc(tenantID).
				Collection(collName).Doc(productID).Set(ctx, chDraft)

			additionalDrafts[ch] = append(additionalDrafts[ch], chDraft)
		}
	}

	c.JSON(200, gin.H{
		"drafts":             temuDrafts,
		"additional_drafts":  additionalDrafts,
		"total":              len(temuDrafts),
		"credits_consumed":   creditsConsumed,
		"ai_used":            useAI,
	})
}

// wizardFilterEmpty returns a single-element slice if s is non-empty, else nil.
func wizardFilterEmpty(s string) []string {
	if s == "" { return nil }
	return []string{s}
}

// wizardParsePrice parses a price string like "9.99" to float64.
func wizardParsePrice(s string) float64 {
	var f float64
	fmt.Sscanf(s, "%f", &f)
	return f
}

// ─── GET /api/v1/temu/drafts/stats ──────────────────────────────────────────
//
// Returns listing draft counts by status for the Channel Command Centre.

func (h *TemuWizardHandler) GetDraftStats(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	iter := h.client.Collection("tenants").Doc(tenantID).
		Collection("temu_drafts").
		Documents(ctx)
	defer iter.Stop()

	counts := map[string]int{
		"draft": 0, "live": 0, "error": 0, "submitted": 0,
	}
	total := 0
	for {
		doc, err := iter.Next()
		if err != nil {
			break
		}
		total++
		data := doc.Data()
		status, _ := data["status"].(string)
		if status == "" {
			status = "draft"
		}
		counts[status]++
	}

	c.JSON(200, gin.H{
		"draft":     counts["draft"],
		"live":      counts["live"],
		"error":     counts["error"],
		"submitted": counts["submitted"],
		"total":     total,
	})
}

// ─── GET /api/v1/temu-wizard/all-drafts ─────────────────────────────────────
//
// Aggregates drafts across temu_drafts and all {channel}_drafts collections and
// returns them grouped by channel. Used by the Step 7 cross-marketplace review UI.
// Response shape:
//   {
//     "channels": ["temu", "amazon", "ebay", ...],
//     "drafts": {
//       "temu":   [ { product_id, sku, title, ... }, ... ],
//       "amazon": [ ... ],
//     },
//     "totals": { "temu": 12, "amazon": 8 }
//   }

func (h *TemuWizardHandler) GetAllDrafts(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	// Channels to aggregate. Always include temu; others are discovered from
	// additional_channels stored on the wizard_xlsx/latest document.
	channels := []string{"temu"}

	latestDoc, err := h.client.Collection("tenants").Doc(tenantID).
		Collection("wizard_xlsx").Doc("latest").Get(ctx)
	if err == nil {
		if raw, ok := latestDoc.Data()["additional_channels"].([]interface{}); ok {
			for _, v := range raw {
				if ch, ok := v.(string); ok && ch != "" && ch != "temu" {
					channels = append(channels, ch)
				}
			}
		}
	}

	allDrafts := map[string][]map[string]interface{}{}
	totals := map[string]int{}

	for _, ch := range channels {
		collName := ch + "_drafts"
		iter := h.client.Collection("tenants").Doc(tenantID).
			Collection(collName).
			Limit(500).
			Documents(ctx)

		var drafts []map[string]interface{}
		for {
			doc, iterErr := iter.Next()
			if iterErr != nil {
				break
			}
			d := doc.Data()
			// Normalise field names so the frontend can use a single shape.
			// temu_drafts use "temu_title"; other channels use "title".
			if _, hasTitle := d["title"]; !hasTitle {
				if t, ok := d["temu_title"].(string); ok {
					d["title"] = t
				}
			}
			if _, hasDesc := d["description"]; !hasDesc {
				if t, ok := d["temu_description"].(string); ok {
					d["description"] = t
				}
			}
			d["channel"] = ch
			drafts = append(drafts, d)
		}
		iter.Stop()

		allDrafts[ch] = drafts
		totals[ch] = len(drafts)
	}

	c.JSON(http.StatusOK, gin.H{
		"channels": channels,
		"drafts":   allDrafts,
		"totals":   totals,
	})
}

// ─── Private helpers ────────────────────────────────────────────────────────

func (h *TemuWizardHandler) fetchAllProducts(ctx context.Context, tenantID string) ([]map[string]interface{}, error) {
	iter := h.client.Collection("tenants").Doc(tenantID).
		Collection("products").
		Limit(5000).
		Documents(ctx)
	defer iter.Stop()

	var products []map[string]interface{}
	for {
		doc, err := iter.Next()
		if err != nil {
			break
		}
		data := doc.Data()
		products = append(products, data)
	}
	return products, nil
}

func (h *TemuWizardHandler) fetchWizardProducts(ctx context.Context, tenantID string, limit int) ([]map[string]interface{}, error) {
	iter := h.client.Collection("tenants").Doc(tenantID).
		Collection("wizard_xlsx_rows").
		Limit(limit * 2).
		Documents(ctx)
	defer iter.Stop()

	var products []map[string]interface{}
	for {
		doc, err := iter.Next()
		if err != nil {
			break
		}
		data := doc.Data()
		if cl, ok := data["create_listing"].(string); ok && strings.ToUpper(cl) == "Y" {
			products = append(products, data)
		}
		if len(products) >= limit {
			break
		}
	}

	if len(products) == 0 {
		return h.fetchAllProducts(ctx, tenantID)
	}
	return products, nil
}

func (h *TemuWizardHandler) consumeCredit(ctx context.Context, tenantID string) (bool, error) {
	tenantRef := h.client.Collection("tenants").Doc(tenantID)
	doc, err := tenantRef.Get(ctx)
	if err != nil {
		return false, err
	}

	data := doc.Data()
	freeUsed, _ := data["free_credits_used"].(int64)
	freeLimit, _ := data["free_credits_limit"].(int64)

	// Check purchased packs first
	packsIter := h.client.Collection("tenants").Doc(tenantID).
		Collection("credit_packs").
		Where("remaining", ">", 0).
		OrderBy("remaining", firestore.Asc).
		Limit(1).
		Documents(ctx)
	defer packsIter.Stop()

	packDoc, packErr := packsIter.Next()
	if packErr == nil {
		packData := packDoc.Data()
		remaining, _ := packData["remaining"].(int64)
		if remaining > 0 {
			packDoc.Ref.Update(ctx, []firestore.Update{
				{Path: "remaining", Value: remaining - 1},
				{Path: "updated_at", Value: time.Now()},
			})
			return true, nil
		}
	}

	// Fall back to free credits
	if freeUsed < freeLimit {
		tenantRef.Update(ctx, []firestore.Update{
			{Path: "free_credits_used", Value: freeUsed + 1},
			{Path: "updated_at", Value: time.Now()},
		})
		return true, nil
	}

	return false, nil
}

// cellName converts 1-based column and row to Excel cell name
func cellName(col, row int) string {
	name, _ := excelize.CoordinatesToCellName(col, row)
	return name
}

// extractPrice tries to get price as a string from various possible types
func extractPrice(p map[string]interface{}) string {
	if v, ok := p["price"].(string); ok {
		return v
	}
	if v, ok := p["price"].(float64); ok {
		return fmt.Sprintf("%.2f", v)
	}
	if v, ok := p["price"].(int64); ok {
		return fmt.Sprintf("%d", v)
	}
	return ""
}

func ptrStr(s string) *string {
	return &s
}

func additionalPriceColumns(channels []string) []string {
	cols := make([]string, 0, len(channels))
	for _, ch := range channels {
		name := strings.ToUpper(ch[:1]) + ch[1:]
		cols = append(cols, name+" Price")
	}
	return cols
}

// ─── POST /api/v1/temu-wizard/generate-listings-async ───────────────────────
//
// Kicks off listing generation as a background goroutine and returns a job_id
// immediately. The frontend polls GET /temu-wizard/generation-job/:job_id for
// progress. This avoids HTTP timeouts for large product batches (4.3).

func (h *TemuWizardHandler) GenerateListingsAsync(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	var req struct {
		CredentialID       string   `json:"credential_id"`
		MaxProducts        int      `json:"max_products"`
		AdditionalChannels []string `json:"additional_channels"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.MaxProducts <= 0 {
		req.MaxProducts = 500
	}

	// Create a job document in Firestore before spawning the goroutine.
	jobID := fmt.Sprintf("job_%d", time.Now().UnixNano())
	jobRef := h.client.Collection("tenants").Doc(tenantID).
		Collection("generation_jobs").Doc(jobID)

	jobDoc := map[string]interface{}{
		"job_id":              jobID,
		"tenant_id":           tenantID,
		"status":              "queued",
		"max_products":        req.MaxProducts,
		"additional_channels": req.AdditionalChannels,
		"total":               0,
		"completed":           0,
		"credits_consumed":    0,
		"error":               "",
		"created_at":          time.Now(),
		"updated_at":          time.Now(),
	}
	if _, err := jobRef.Set(ctx, jobDoc); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create job"})
		return
	}

	// Spawn background worker — uses a fresh context so it outlives the HTTP request.
	go h.runGenerationJob(jobID, tenantID, req.MaxProducts, req.AdditionalChannels)

	c.JSON(http.StatusAccepted, gin.H{
		"job_id":     jobID,
		"status":     "queued",
		"poll_url":   fmt.Sprintf("/api/v1/temu-wizard/generation-job/%s", jobID),
	})
}

// ─── GET /api/v1/temu-wizard/generation-job/:job_id ─────────────────────────
//
// Returns the current status and progress of a background generation job.

func (h *TemuWizardHandler) GetGenerationJob(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	jobID := c.Param("job_id")
	ctx := c.Request.Context()

	snap, err := h.client.Collection("tenants").Doc(tenantID).
		Collection("generation_jobs").Doc(jobID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}
	c.JSON(http.StatusOK, snap.Data())
}

// runGenerationJob is the background worker for GenerateListingsAsync.
// It mirrors the synchronous GenerateListings logic but writes incremental
// progress updates to the job document in Firestore.
func (h *TemuWizardHandler) runGenerationJob(jobID, tenantID string, maxProducts int, additionalChannels []string) {
	ctx := context.Background()
	jobRef := h.client.Collection("tenants").Doc(tenantID).
		Collection("generation_jobs").Doc(jobID)

	updateJob := func(fields map[string]interface{}) {
		fields["updated_at"] = time.Now()
		jobRef.Update(ctx, func() []firestore.Update {
			var updates []firestore.Update
			for k, v := range fields {
				updates = append(updates, firestore.Update{Path: k, Value: v})
			}
			return updates
		}())
	}

	// Mark running
	updateJob(map[string]interface{}{"status": "running"})

	products, err := h.fetchWizardProducts(ctx, tenantID, maxProducts)
	if err != nil {
		updateJob(map[string]interface{}{"status": "error", "error": err.Error()})
		return
	}
	if len(products) == 0 {
		updateJob(map[string]interface{}{"status": "error", "error": "no products found"})
		return
	}

	updateJob(map[string]interface{}{"total": len(products), "status": "running"})

	useAI := h.aiService != nil && h.aiService.IsAvailable()
	creditsConsumed := 0
	completed := 0

	for _, p := range products {
		productID, _ := p["product_id"].(string)
		sku, _ := p["sku"].(string)
		title, _ := p["title"].(string)
		temuBrand, _ := p["temu_brand"].(string)
		temuPrice, _ := p["temu_price"].(string)
		srcPrice, _ := p["price"].(string)
		imageURL, _ := p["image_url"].(string)
		if productID == "" {
			productID = sku
		}

		consumed, _ := h.consumeCredit(ctx, tenantID)
		if !consumed {
			log.Printf("[GenJob:%s] out of credits at product %s", jobID, sku)
			break
		}
		creditsConsumed++

		var temuDraft map[string]interface{}
		if useAI {
			aiProduct := services.AIProductInput{
				Title: title, Brand: temuBrand, SKU: sku,
				ImageURLs: wizardFilterEmpty(imageURL), SourcePrice: wizardParsePrice(srcPrice),
			}
			result, aiErr := h.aiService.GenerateListingsSinglePhase(ctx, aiProduct, []string{"temu"})
			if aiErr == nil && len(result.Listings) > 0 {
				l := result.Listings[0]
				temuDraft = map[string]interface{}{
					"product_id": productID, "sku": sku, "channel": "temu",
					"source_title": title, "temu_title": l.Title,
					"temu_description": l.Description, "temu_brand": temuBrand,
					"temu_price": temuPrice, "temu_category": l.CategoryName,
					"bullet_points": l.BulletPoints, "image_url": imageURL,
					"status": "draft", "created_at": time.Now(), "ai_confidence": l.Confidence,
				}
			}
		}
		if temuDraft == nil {
			t := title
			if temuBrand != "" {
				t = title + " - " + temuBrand
			}
			temuDraft = map[string]interface{}{
				"product_id": productID, "sku": sku, "channel": "temu",
				"source_title": title, "temu_title": t,
				"temu_description": fmt.Sprintf("High quality %s. Fast shipping. Great value.", title),
				"temu_brand": temuBrand, "temu_price": temuPrice,
				"image_url": imageURL, "status": "draft", "created_at": time.Now(),
			}
		}
		h.client.Collection("tenants").Doc(tenantID).
			Collection("temu_drafts").Doc(productID).Set(ctx, temuDraft)

		// Additional channels
		for _, ch := range additionalChannels {
			chPrice, _ := p[ch+"_price"].(string)
			if chPrice == "" {
				continue
			}
			chConsumed, _ := h.consumeCredit(ctx, tenantID)
			if !chConsumed {
				continue
			}
			creditsConsumed++
			var chDraft map[string]interface{}
			if useAI {
				aiProduct := services.AIProductInput{
					Title: title, Brand: temuBrand, SKU: sku,
					ImageURLs: wizardFilterEmpty(imageURL), SourcePrice: wizardParsePrice(chPrice),
				}
				result, aiErr := h.aiService.GenerateListingsSinglePhase(ctx, aiProduct, []string{ch})
				if aiErr == nil && len(result.Listings) > 0 {
					l := result.Listings[0]
					chDraft = map[string]interface{}{
						"product_id": productID, "sku": sku, "channel": ch,
						"source_title": title, "title": l.Title,
						"description": l.Description, "bullet_points": l.BulletPoints,
						"brand": temuBrand, "price": chPrice, "category": l.CategoryName,
						"image_url": imageURL, "status": "draft", "created_at": time.Now(),
					}
				}
			}
			if chDraft == nil {
				chDraft = map[string]interface{}{
					"product_id": productID, "sku": sku, "channel": ch,
					"source_title": title, "title": title,
					"description": fmt.Sprintf("High quality %s. Fast shipping. Great value.", title),
					"brand": temuBrand, "price": chPrice,
					"image_url": imageURL, "status": "draft", "created_at": time.Now(),
				}
			}
			h.client.Collection("tenants").Doc(tenantID).
				Collection(ch + "_drafts").Doc(productID).Set(ctx, chDraft)
		}

		completed++
		// Write progress every 10 products
		if completed%10 == 0 {
			updateJob(map[string]interface{}{
				"completed":        completed,
				"credits_consumed": creditsConsumed,
			})
		}
	}

	updateJob(map[string]interface{}{
		"status":           "completed",
		"completed":        completed,
		"credits_consumed": creditsConsumed,
	})
	log.Printf("[GenJob:%s] done: %d products, %d credits", jobID, completed, creditsConsumed)
}
