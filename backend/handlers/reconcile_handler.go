package handlers

// ============================================================================
// SKU RECONCILIATION HANDLER  (S4 — Auto-link / Reconcile)
// ============================================================================
//
// Endpoints (registered in main.go under marketplace credentials group):
//   POST /marketplace/credentials/:id/auto-link          → RunAutoLink
//   GET  /marketplace/credentials/:id/reconcile          → GetReconcileState
//   POST /marketplace/credentials/:id/reconcile/confirm  → ConfirmReconcile
//   GET  /marketplace/credentials/:id/reconcile/export   → ExportUnmatched
//   POST /marketplace/credentials/:id/reconcile/import   → ImportResolutions
//   GET  /marketplace/credentials/:id/reconcile/history  → GetHistory  (NEW S7)
//
// Session 7 additions:
//   • Listing push to Back Market / Bol.com after ConfirmReconcile
//   • Zalando / Lazada marked requires_manual_publish
//   • Reconciliation history stored at /runs/{jobID} subcollection
// ============================================================================

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"module-a/marketplace"
	"module-a/marketplace/clients/backmarket"
	"module-a/marketplace/clients/bol"
	"module-a/models"
	"module-a/repository"
	"module-a/services"

	"github.com/xuri/excelize/v2"
)

// ── types ─────────────────────────────────────────────────────────────────────

type MatchType string

const (
	MatchTypeFull    MatchType = "full"
	MatchTypePartial MatchType = "partial"
	MatchTypeNone    MatchType = "none"
)

type ReconcileRow struct {
	ChannelSKU        string  `json:"channel_sku" firestore:"channel_sku"`
	ChannelTitle      string  `json:"channel_title" firestore:"channel_title"`
	ChannelImage      string  `json:"channel_image" firestore:"channel_image"`
	ChannelListingURL string  `json:"channel_listing_url" firestore:"channel_listing_url"`
	ChannelPrice      float64 `json:"channel_price" firestore:"channel_price"`
	ChannelStock      int     `json:"channel_stock" firestore:"channel_stock"`
	ExternalID        string  `json:"external_id" firestore:"external_id"`

	MatchType   MatchType `json:"match_type" firestore:"match_type"`
	MatchScore  float64   `json:"match_score" firestore:"match_score"`
	MatchReason string    `json:"match_reason" firestore:"match_reason"`

	InternalProductID string `json:"internal_product_id" firestore:"internal_product_id"`
	InternalSKU       string `json:"internal_sku" firestore:"internal_sku"`
	InternalTitle     string `json:"internal_title" firestore:"internal_title"`

	Decision string `json:"decision" firestore:"decision"`
	AIEnrich bool   `json:"ai_enrich" firestore:"ai_enrich"`

	// Push result fields (S7)
	PushStatus        string `json:"push_status,omitempty" firestore:"push_status,omitempty"`
	ExternalListingID string `json:"external_listing_id,omitempty" firestore:"external_listing_id,omitempty"`
	PushError         string `json:"push_error,omitempty" firestore:"push_error,omitempty"`
}

type ReconcileJob struct {
	JobID        string         `json:"job_id" firestore:"job_id"`
	CredentialID string         `json:"credential_id" firestore:"credential_id"`
	TenantID     string         `json:"tenant_id" firestore:"tenant_id"`
	Channel      string         `json:"channel" firestore:"channel"`
	Status       string         `json:"status" firestore:"status"`
	CreatedAt    time.Time      `json:"created_at" firestore:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at" firestore:"updated_at"`
	CompletedAt  *time.Time     `json:"completed_at,omitempty" firestore:"completed_at,omitempty"`
	Rows         []ReconcileRow `json:"rows" firestore:"rows"`

	TotalRows   int `json:"total_rows" firestore:"total_rows"`
	FullMatches int `json:"full_matches" firestore:"full_matches"`
	Partial     int `json:"partial" firestore:"partial"`
	Unmatched   int `json:"unmatched" firestore:"unmatched"`
	Confirmed   int `json:"confirmed" firestore:"confirmed"`

	// Push summary (S7)
	PushTotal          int `json:"push_total,omitempty" firestore:"push_total,omitempty"`
	PushSucceeded      int `json:"push_succeeded,omitempty" firestore:"push_succeeded,omitempty"`
	PushFailed         int `json:"push_failed,omitempty" firestore:"push_failed,omitempty"`
	PushManualRequired int `json:"push_manual_required,omitempty" firestore:"push_manual_required,omitempty"`
}

// ── handler ───────────────────────────────────────────────────────────────────

type ReconcileHandler struct {
	marketplaceService *services.MarketplaceService
	productService     *services.ProductService
	repo               *repository.MarketplaceRepository
	fsClient           *firestore.Client
	searchService      *services.SearchService
	amazonHandler      *AmazonHandler
	ebayEnrichHandler  *EbayEnrichmentHandler
}

func NewReconcileHandler(
	ms *services.MarketplaceService,
	repo *repository.MarketplaceRepository,
	fsRepo *repository.FirestoreRepository,
	ps *services.ProductService,
	ss *services.SearchService,
) *ReconcileHandler {
	return &ReconcileHandler{
		marketplaceService: ms,
		productService:     ps,
		repo:               repo,
		fsClient:           fsRepo.GetClient(),
		searchService:      ss,
	}
}

func (h *ReconcileHandler) SetAmazonHandler(ah *AmazonHandler) { h.amazonHandler = ah }
func (h *ReconcileHandler) SetEbayEnrichHandler(eh *EbayEnrichmentHandler) {
	h.ebayEnrichHandler = eh
}

// ── RunAutoLink ───────────────────────────────────────────────────────────────

func (h *ReconcileHandler) RunAutoLink(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	credentialID := c.Param("id")

	cred, err := h.marketplaceService.GetCredential(c.Request.Context(), tenantID, credentialID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "credential not found"})
		return
	}

	jobID := uuid.New().String()
	job := &ReconcileJob{
		JobID:        jobID,
		CredentialID: credentialID,
		TenantID:     tenantID,
		Channel:      cred.Channel,
		Status:       "running",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := h.saveJob(c.Request.Context(), tenantID, credentialID, job); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to initialise reconciliation job"})
		return
	}

	go h.runMatching(tenantID, credentialID, job)

	c.JSON(http.StatusAccepted, gin.H{
		"job_id":  jobID,
		"status":  "running",
		"message": "Reconciliation started — poll GET /reconcile for results",
	})
}

// ── GetReconcileState ─────────────────────────────────────────────────────────

func (h *ReconcileHandler) GetReconcileState(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	credentialID := c.Param("id")

	job, err := h.loadJob(c.Request.Context(), tenantID, credentialID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no reconciliation job found — run auto-link first"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": job})
}

// ── GetHistory (S7) ───────────────────────────────────────────────────────────
// GET /marketplace/credentials/:id/reconcile/history
// Returns last 50 runs sorted by created_at desc. Rows are stripped for size.

func (h *ReconcileHandler) GetHistory(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	credentialID := c.Param("id")

	iter := h.fsClient.Collection("tenants").Doc(tenantID).
		Collection("reconcile_jobs").Doc(credentialID).
		Collection("runs").
		OrderBy("created_at", firestore.Desc).
		Limit(50).
		Documents(c.Request.Context())
	defer iter.Stop()

	type RunSummary struct {
		JobID              string     `json:"job_id"`
		Channel            string     `json:"channel"`
		Status             string     `json:"status"`
		CreatedAt          time.Time  `json:"created_at"`
		CompletedAt        *time.Time `json:"completed_at,omitempty"`
		TotalRows          int        `json:"total_rows"`
		FullMatches        int        `json:"full_matches"`
		Partial            int        `json:"partial"`
		Unmatched          int        `json:"unmatched"`
		Confirmed          int        `json:"confirmed"`
		PushTotal          int        `json:"push_total,omitempty"`
		PushSucceeded      int        `json:"push_succeeded,omitempty"`
		PushFailed         int        `json:"push_failed,omitempty"`
		PushManualRequired int        `json:"push_manual_required,omitempty"`
	}

	var runs []RunSummary
	for {
		doc, err := iter.Next()
		if err != nil {
			break
		}
		var job ReconcileJob
		if err := doc.DataTo(&job); err != nil {
			continue
		}
		runs = append(runs, RunSummary{
			JobID:              job.JobID,
			Channel:            job.Channel,
			Status:             job.Status,
			CreatedAt:          job.CreatedAt,
			CompletedAt:        job.CompletedAt,
			TotalRows:          job.TotalRows,
			FullMatches:        job.FullMatches,
			Partial:            job.Partial,
			Unmatched:          job.Unmatched,
			Confirmed:          job.Confirmed,
			PushTotal:          job.PushTotal,
			PushSucceeded:      job.PushSucceeded,
			PushFailed:         job.PushFailed,
			PushManualRequired: job.PushManualRequired,
		})
	}

	c.JSON(http.StatusOK, gin.H{"data": runs, "total": len(runs)})
}

// ── ConfirmReconcile ──────────────────────────────────────────────────────────

func (h *ReconcileHandler) ConfirmReconcile(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	credentialID := c.Param("id")

	var req struct {
		Rows []struct {
			ChannelSKU        string `json:"channel_sku"`
			Decision          string `json:"decision"`
			InternalProductID string `json:"internal_product_id,omitempty"`
			AIEnrich          bool   `json:"ai_enrich,omitempty"`
		} `json:"rows" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	job, err := h.loadJob(ctx, tenantID, credentialID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no reconciliation job found"})
		return
	}

	cred, err := h.marketplaceService.GetCredential(ctx, tenantID, credentialID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "credential not found"})
		return
	}

	decisions := map[string]struct {
		Decision          string
		InternalProductID string
		AIEnrich          bool
	}{}
	for _, r := range req.Rows {
		decisions[r.ChannelSKU] = struct {
			Decision          string
			InternalProductID string
			AIEnrich          bool
		}{r.Decision, r.InternalProductID, r.AIEnrich}
	}

	config, err := h.repo.GetCredentialConfig(ctx, tenantID, credentialID)
	if err != nil || config == nil {
		empty := models.DefaultChannelConfig()
		config = &empty
	}

	confirmed := 0
	var enrichQueue []ReconcileRow
	var importAsNewRows []ReconcileRow

	for i := range job.Rows {
		row := &job.Rows[i]
		dec, hasDec := decisions[row.ChannelSKU]
		if !hasDec {
			continue
		}
		if dec.InternalProductID != "" {
			row.InternalProductID = dec.InternalProductID
		}
		row.Decision = dec.Decision
		row.AIEnrich = dec.AIEnrich

		switch dec.Decision {
		case "accepted":
			if row.InternalProductID == "" {
				continue
			}
			config.InventoryMappings = appendOrUpdateMapping(config.InventoryMappings, models.InventoryMapping{
				ChannelSKU:  row.ChannelSKU,
				InternalSKU: row.InternalSKU,
				ProductID:   row.InternalProductID,
			})
			confirmed++
			if cred.Channel == "amazon" || cred.Channel == "ebay" {
				enrichQueue = append(enrichQueue, *row)
			}

		case "new":
			productID, err := h.createDraftProduct(ctx, tenantID, *row, cred.Channel)
			if err != nil {
				log.Printf("Reconcile: failed to create draft product for %s: %v", row.ChannelSKU, err)
				continue
			}
			row.InternalProductID = productID
			config.InventoryMappings = appendOrUpdateMapping(config.InventoryMappings, models.InventoryMapping{
				ChannelSKU:  row.ChannelSKU,
				InternalSKU: row.ChannelSKU,
				ProductID:   productID,
			})
			confirmed++
			importAsNewRows = append(importAsNewRows, *row)
			if cred.Channel == "amazon" || cred.Channel == "ebay" {
				enrichQueue = append(enrichQueue, *row)
			}
		}
	}

	if err := h.repo.UpdateCredentialConfig(ctx, tenantID, credentialID, *config); err != nil {
		log.Printf("Reconcile: failed to update credential config: %v", err)
	}

	// ── Push listings to channel APIs (S7) ───────────────────────────────────
	pushTotal := len(importAsNewRows)
	pushed, failed, manual := 0, 0, 0

	for i := range importAsNewRows {
		row := &importAsNewRows[i]
		product, err := h.productService.GetProduct(ctx, tenantID, row.InternalProductID)
		if err != nil {
			log.Printf("Reconcile push: could not load product %s: %v", row.InternalProductID, err)
			row.PushStatus = "push_failed"
			row.PushError = fmt.Sprintf("product load failed: %v", err)
			failed++
			continue
		}

		var externalListingID string
		var pushErr error

		switch cred.Channel {
		case "backmarket":
			externalListingID, pushErr = h.pushToBackMarket(ctx, product, cred)
		case "bol":
			externalListingID, pushErr = h.pushToBol(ctx, product, cred)
		case "zalando", "lazada":
			row.PushStatus = "requires_manual_publish"
			manual++
			h.writePushResult(ctx, tenantID, credentialID, row.ChannelSKU, "requires_manual_publish", "")
			continue
		default:
			row.PushStatus = "requires_manual_publish"
			manual++
			continue
		}

		if pushErr != nil {
			log.Printf("Reconcile push: %s failed for %s: %v", cred.Channel, row.ChannelSKU, pushErr)
			row.PushStatus = "push_failed"
			row.PushError = pushErr.Error()
			failed++
			h.writePushResult(ctx, tenantID, credentialID, row.ChannelSKU, "push_failed", "")
		} else {
			row.PushStatus = "live"
			row.ExternalListingID = externalListingID
			pushed++
			h.writePushResult(ctx, tenantID, credentialID, row.ChannelSKU, "live", externalListingID)
			if h.searchService != nil {
				if idxErr := h.searchService.IndexProduct(product); idxErr != nil {
					log.Printf("Reconcile push: Typesense index failed for %s: %v", product.ProductID, idxErr)
				}
			}
		}
	}

	now := time.Now()
	job.Status = "confirmed"
	job.Confirmed = confirmed
	job.UpdatedAt = now
	job.CompletedAt = &now
	job.PushTotal = pushTotal
	job.PushSucceeded = pushed
	job.PushFailed = failed
	job.PushManualRequired = manual
	_ = h.saveJob(ctx, tenantID, credentialID, job)

	if len(enrichQueue) > 0 {
		go h.triggerEnrichment(tenantID, credentialID, cred.Channel, enrichQueue)
	}

	c.JSON(http.StatusOK, gin.H{
		"confirmed": confirmed,
		"status":    "confirmed",
		"message":   fmt.Sprintf("%d mappings written", confirmed),
		"push_summary": gin.H{
			"total":           pushTotal,
			"pushed":          pushed,
			"failed":          failed,
			"manual_required": manual,
		},
	})
}

// ── Back Market push ──────────────────────────────────────────────────────────

func (h *ReconcileHandler) pushToBackMarket(ctx context.Context, product *models.Product, cred *models.MarketplaceCredential) (string, error) {
	apiKey := cred.CredentialData["api_key"]
	if apiKey == "" {
		apiKey = cred.CredentialData["access_token"]
	}
	production := cred.Environment == "production"
	client := backmarket.NewClient(apiKey, production)

	description := ""
	if product.Description != nil {
		description = *product.Description
	}

	var bmProductID int
	if product.Attributes != nil {
		switch v := product.Attributes["backmarket_product_id"].(type) {
		case float64:
			bmProductID = int(v)
		case int:
			bmProductID = v
		case int64:
			bmProductID = int(v)
		}
	}

	grade := "good"
	if product.Attributes != nil {
		if v, ok := product.Attributes["backmarket_grade"].(string); ok && v != "" {
			grade = v
		} else if v, ok := product.Attributes["condition"].(string); ok && v != "" {
			grade = v
		}
	}

	var price float64
	if product.Attributes != nil {
		if v, ok := product.Attributes["price"].(float64); ok {
			price = v
		}
	}

	var stock int
	if product.Attributes != nil {
		if v, ok := product.Attributes["stock"].(float64); ok {
			stock = int(v)
		}
	}

	listing, err := client.UpsertListing(backmarket.CreateListingRequest{
		ProductID:   bmProductID,
		SellerSKU:   product.SKU,
		Price:       price,
		Currency:    "GBP",
		Stock:       stock,
		Grade:       grade,
		Description: description,
	})
	if err != nil {
		return "", fmt.Errorf("back market upsert listing: %w", err)
	}
	return listing.ListingID, nil
}

// ── Bol.com push ──────────────────────────────────────────────────────────────

func (h *ReconcileHandler) pushToBol(ctx context.Context, product *models.Product, cred *models.MarketplaceCredential) (string, error) {
	clientID := cred.CredentialData["client_id"]
	clientSecret := cred.CredentialData["client_secret"]
	if clientID == "" || clientSecret == "" {
		return "", fmt.Errorf("bol.com credential missing client_id or client_secret")
	}
	client := bol.NewClient(clientID, clientSecret)

	ean := ""
	if product.Identifiers != nil && product.Identifiers.EAN != nil {
		ean = *product.Identifiers.EAN
	}
	if ean == "" {
		return "", fmt.Errorf("product %s has no EAN — required for bol.com offers", product.ProductID)
	}

	var price float64
	if product.Attributes != nil {
		if v, ok := product.Attributes["price"].(float64); ok {
			price = v
		}
	}

	var stock int
	if product.Attributes != nil {
		if v, ok := product.Attributes["stock"].(float64); ok {
			stock = int(v)
		}
	}

	condition := "NEW"
	if product.Attributes != nil {
		if v, ok := product.Attributes["condition"].(string); ok && v != "" {
			condition = strings.ToUpper(v)
		}
	}

	payload := map[string]interface{}{
		"ean":              ean,
		"condition":        condition,
		"reference":        product.SKU,
		"fulfilmentMethod": "FBR",
		"pricing": map[string]interface{}{
			"bundlePrices": []map[string]interface{}{
				{"quantity": 1, "unitPrice": price},
			},
		},
		"stock": map[string]interface{}{
			"amount":            stock,
			"managedByRetailer": true,
		},
	}

	respBody, statusCode, err := client.DoRequestPublic("POST", "/retailer/offers", payload)
	if err != nil {
		return "", fmt.Errorf("bol.com create offer: %w", err)
	}
	if statusCode != 202 {
		return "", fmt.Errorf("bol.com create offer: unexpected status %d: %s", statusCode, string(respBody))
	}

	var processResp struct {
		ProcessStatusID string `json:"processStatusId"`
		Status          string `json:"status"`
		EntityID        string `json:"entityId"`
	}
	if err := json.Unmarshal(respBody, &processResp); err != nil {
		return "", fmt.Errorf("bol.com parse process status: %w", err)
	}

	if processResp.ProcessStatusID != "" {
		offerID, err := h.pollBolProcessStatus(client, processResp.ProcessStatusID)
		if err != nil {
			log.Printf("Reconcile: bol.com poll timeout for %s — storing pending", processResp.ProcessStatusID)
			return "pending:" + processResp.ProcessStatusID, nil
		}
		return offerID, nil
	}

	return processResp.EntityID, nil
}

func (h *ReconcileHandler) pollBolProcessStatus(client *bol.Client, processStatusID string) (string, error) {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(1 * time.Second)
		respBody, _, err := client.DoRequestPublic("GET", "/retailer/process-status/"+processStatusID, nil)
		if err != nil {
			continue
		}
		var status struct {
			Status   string `json:"status"`
			EntityID string `json:"entityId"`
		}
		if err := json.Unmarshal(respBody, &status); err != nil {
			continue
		}
		switch status.Status {
		case "SUCCESS":
			return status.EntityID, nil
		case "FAILURE", "TIMEOUT":
			return "", fmt.Errorf("bol.com process-status %s: %s", processStatusID, status.Status)
		}
	}
	return "", fmt.Errorf("bol.com process-status poll timed out after 10s")
}

// writePushResult persists push outcome to a lightweight Firestore document.
func (h *ReconcileHandler) writePushResult(ctx context.Context, tenantID, credentialID, channelSKU, status, externalListingID string) {
	docID := credentialID + "_" + channelSKU
	_, err := h.fsClient.Collection("tenants").Doc(tenantID).
		Collection("reconcile_push_results").Doc(docID).
		Set(ctx, map[string]interface{}{
			"channel_sku":         channelSKU,
			"credential_id":       credentialID,
			"push_status":         status,
			"external_listing_id": externalListingID,
			"updated_at":          time.Now(),
		})
	if err != nil {
		log.Printf("Reconcile: failed to write push result for %s: %v", channelSKU, err)
	}
}

// ── ExportUnmatched ───────────────────────────────────────────────────────────

func (h *ReconcileHandler) ExportUnmatched(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	credentialID := c.Param("id")

	job, err := h.loadJob(c.Request.Context(), tenantID, credentialID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no reconciliation job found"})
		return
	}

	f := excelize.NewFile()
	sheet := "Unmatched SKUs"
	f.SetSheetName("Sheet1", sheet)

	headers := []string{"Channel SKU", "Channel Title", "Channel Price", "Channel Stock", "Image URL", "Suggested Internal SKU", "Internal Product ID (fill in)", "Action (new/map/skip)"}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sheet, cell, h)
	}

	row := 2
	for _, r := range job.Rows {
		if r.MatchType == MatchTypeFull && r.Decision == "accepted" {
			continue
		}
		f.SetCellValue(sheet, fmt.Sprintf("A%d", row), r.ChannelSKU)
		f.SetCellValue(sheet, fmt.Sprintf("B%d", row), r.ChannelTitle)
		f.SetCellValue(sheet, fmt.Sprintf("C%d", row), r.ChannelPrice)
		f.SetCellValue(sheet, fmt.Sprintf("D%d", row), r.ChannelStock)
		f.SetCellValue(sheet, fmt.Sprintf("E%d", row), r.ChannelImage)
		f.SetCellValue(sheet, fmt.Sprintf("F%d", row), r.InternalSKU)
		f.SetCellValue(sheet, fmt.Sprintf("G%d", row), r.InternalProductID)
		f.SetCellValue(sheet, fmt.Sprintf("H%d", row), "")
		row++
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate XLSX"})
		return
	}

	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=unmatched_skus_%s.xlsx", credentialID))
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", buf.Bytes())
}

// ── ImportResolutions ─────────────────────────────────────────────────────────

func (h *ReconcileHandler) ImportResolutions(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	credentialID := c.Param("id")

	file, _, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required (multipart field 'file')"})
		return
	}
	defer file.Close()

	xlsxBytes, err := io.ReadAll(file)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read file"})
		return
	}

	f, err := excelize.OpenReader(bytes.NewReader(xlsxBytes))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid XLSX file"})
		return
	}

	sheetName := f.GetSheetName(0)
	rows, err := f.GetRows(sheetName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read sheet"})
		return
	}

	type resolution struct {
		Decision          string
		InternalProductID string
	}
	resolutions := map[string]resolution{}
	for i, row := range rows {
		if i == 0 {
			continue
		}
		if len(row) < 8 {
			continue
		}
		channelSKU := strings.TrimSpace(row[0])
		productID := strings.TrimSpace(row[6])
		action := strings.ToLower(strings.TrimSpace(row[7]))
		if channelSKU == "" || action == "" {
			continue
		}
		if action != "new" && action != "map" && action != "skip" {
			continue
		}
		decision := action
		if action == "map" {
			decision = "accepted"
		}
		resolutions[channelSKU] = resolution{Decision: decision, InternalProductID: productID}
	}

	if len(resolutions) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no valid resolutions found in file — check column H (Action) values: new / map / skip"})
		return
	}

	ctx := c.Request.Context()
	job, err := h.loadJob(ctx, tenantID, credentialID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no reconciliation job found"})
		return
	}
	cred, err := h.marketplaceService.GetCredential(ctx, tenantID, credentialID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "credential not found"})
		return
	}
	config, err := h.repo.GetCredentialConfig(ctx, tenantID, credentialID)
	if err != nil || config == nil {
		empty := models.DefaultChannelConfig()
		config = &empty
	}

	confirmed := 0
	var enrichQueue []ReconcileRow
	var importAsNewRows []ReconcileRow

	for i := range job.Rows {
		row := &job.Rows[i]
		res, ok := resolutions[row.ChannelSKU]
		if !ok {
			continue
		}
		if res.InternalProductID != "" {
			row.InternalProductID = res.InternalProductID
		}
		row.Decision = res.Decision
		switch res.Decision {
		case "accepted":
			if row.InternalProductID == "" {
				continue
			}
			config.InventoryMappings = appendOrUpdateMapping(config.InventoryMappings, models.InventoryMapping{
				ChannelSKU:  row.ChannelSKU,
				InternalSKU: row.InternalSKU,
				ProductID:   row.InternalProductID,
			})
			confirmed++
			if cred.Channel == "amazon" || cred.Channel == "ebay" {
				enrichQueue = append(enrichQueue, *row)
			}
		case "new":
			productID, err := h.createDraftProduct(ctx, tenantID, *row, cred.Channel)
			if err != nil {
				log.Printf("Reconcile import: draft product creation failed for %s: %v", row.ChannelSKU, err)
				continue
			}
			row.InternalProductID = productID
			config.InventoryMappings = appendOrUpdateMapping(config.InventoryMappings, models.InventoryMapping{
				ChannelSKU:  row.ChannelSKU,
				InternalSKU: row.ChannelSKU,
				ProductID:   productID,
			})
			confirmed++
			importAsNewRows = append(importAsNewRows, *row)
		}
	}

	_ = h.repo.UpdateCredentialConfig(ctx, tenantID, credentialID, *config)

	pushed, failed, manual := 0, 0, 0
	for i := range importAsNewRows {
		row := &importAsNewRows[i]
		product, err := h.productService.GetProduct(ctx, tenantID, row.InternalProductID)
		if err != nil {
			failed++
			continue
		}
		switch cred.Channel {
		case "backmarket":
			_, err = h.pushToBackMarket(ctx, product, cred)
		case "bol":
			_, err = h.pushToBol(ctx, product, cred)
		default:
			manual++
			continue
		}
		if err != nil {
			failed++
		} else {
			pushed++
			if h.searchService != nil {
				_ = h.searchService.IndexProduct(product)
			}
		}
	}

	now := time.Now()
	job.Confirmed = confirmed
	job.Status = "confirmed"
	job.UpdatedAt = now
	job.CompletedAt = &now
	job.PushTotal = len(importAsNewRows)
	job.PushSucceeded = pushed
	job.PushFailed = failed
	job.PushManualRequired = manual
	_ = h.saveJob(ctx, tenantID, credentialID, job)

	if len(enrichQueue) > 0 {
		go h.triggerEnrichment(tenantID, credentialID, cred.Channel, enrichQueue)
	}

	c.JSON(http.StatusOK, gin.H{
		"confirmed": confirmed,
		"status":    "confirmed",
		"push_summary": gin.H{
			"total": len(importAsNewRows), "pushed": pushed,
			"failed": failed, "manual_required": manual,
		},
	})
}

// ── Matching engine ───────────────────────────────────────────────────────────

func (h *ReconcileHandler) runMatching(tenantID, credentialID string, job *ReconcileJob) {
	ctx := context.Background()

	cred, err := h.marketplaceService.GetCredential(ctx, tenantID, credentialID)
	if err != nil {
		job.Status = "failed"
		_ = h.saveJob(ctx, tenantID, credentialID, job)
		return
	}

	mergedCreds, err := h.marketplaceService.GetFullCredentials(ctx, cred)
	if err != nil {
		log.Printf("Reconcile: failed to get credentials for %s: %v", credentialID, err)
		job.Status = "failed"
		_ = h.saveJob(ctx, tenantID, credentialID, job)
		return
	}
	adapter, err := marketplace.GetAdapter(ctx, cred.Channel, marketplace.Credentials{
		MarketplaceID:   cred.Channel,
		Environment:     cred.Environment,
		MarketplaceType: cred.Channel,
		Data:            mergedCreds,
	})
	if err != nil {
		log.Printf("Reconcile: no adapter for channel %s: %v", cred.Channel, err)
		job.Status = "failed"
		_ = h.saveJob(ctx, tenantID, credentialID, job)
		return
	}

	listings, err := adapter.FetchListings(ctx, marketplace.ImportFilters{})
	if err != nil {
		log.Printf("Reconcile: FetchListings failed for %s: %v", credentialID, err)
		job.Status = "failed"
		_ = h.saveJob(ctx, tenantID, credentialID, job)
		return
	}

	internalProducts, _, err := h.productService.ListProducts(ctx, tenantID, nil, 5000, 0)
	if err != nil {
		log.Printf("Reconcile: failed to load internal products: %v", err)
		job.Status = "failed"
		_ = h.saveJob(ctx, tenantID, credentialID, job)
		return
	}

	bySKU := map[string]*models.Product{}
	byEAN := map[string]*models.Product{}
	for i := range internalProducts {
		p := &internalProducts[i]
		bySKU[strings.ToLower(p.SKU)] = p
		if p.Identifiers != nil && p.Identifiers.EAN != nil {
			byEAN[strings.ToLower(*p.Identifiers.EAN)] = p
		}
	}

	var jobRows []ReconcileRow
	fullCount, partialCount, noneCount := 0, 0, 0

	for _, listing := range listings {
		row := ReconcileRow{
			ChannelSKU:   listing.SKU,
			ChannelTitle: listing.Title,
			ChannelPrice: listing.Price,
			ChannelStock: listing.Quantity,
			ExternalID:   listing.ExternalID,
		}
		if len(listing.Images) > 0 {
			row.ChannelImage = listing.Images[0].URL
		}

		if p, ok := bySKU[strings.ToLower(listing.SKU)]; ok {
			row.MatchType = MatchTypeFull
			row.MatchScore = 1.0
			row.MatchReason = "sku"
			row.InternalProductID = p.ProductID
			row.InternalSKU = p.SKU
			row.InternalTitle = p.Title
			row.Decision = "accepted"
			fullCount++
			jobRows = append(jobRows, row)
			continue
		}

		if listing.Identifiers.EAN != "" {
			if p, ok := byEAN[strings.ToLower(listing.Identifiers.EAN)]; ok {
				row.MatchType = MatchTypePartial
				row.MatchScore = 0.95
				row.MatchReason = "ean"
				row.InternalProductID = p.ProductID
				row.InternalSKU = p.SKU
				row.InternalTitle = p.Title
				partialCount++
				jobRows = append(jobRows, row)
				continue
			}
		}

		bestScore := 0.0
		var bestProduct *models.Product
		for i := range internalProducts {
			p := &internalProducts[i]
			score := jaroWinkler(strings.ToLower(listing.Title), strings.ToLower(p.Title))
			if score > bestScore {
				bestScore = score
				bestProduct = p
			}
		}
		if bestScore >= 0.85 && bestProduct != nil {
			row.MatchType = MatchTypePartial
			row.MatchScore = bestScore
			row.MatchReason = "title"
			row.InternalProductID = bestProduct.ProductID
			row.InternalSKU = bestProduct.SKU
			row.InternalTitle = bestProduct.Title
			partialCount++
		} else {
			row.MatchType = MatchTypeNone
			noneCount++
		}
		jobRows = append(jobRows, row)
	}

	now := time.Now()
	job.Rows = jobRows
	job.TotalRows = len(jobRows)
	job.FullMatches = fullCount
	job.Partial = partialCount
	job.Unmatched = noneCount
	job.Status = "complete"
	job.UpdatedAt = now
	job.CompletedAt = &now

	if err := h.saveJob(ctx, tenantID, credentialID, job); err != nil {
		log.Printf("Reconcile: failed to save job results: %v", err)
	}
	log.Printf("Reconcile: %s — %d full, %d partial, %d unmatched", credentialID, fullCount, partialCount, noneCount)
}

// ── createDraftProduct ────────────────────────────────────────────────────────

func (h *ReconcileHandler) createDraftProduct(ctx context.Context, tenantID string, row ReconcileRow, channel string) (string, error) {
	title := row.ChannelTitle
	if title == "" {
		title = row.ChannelSKU
	}
	product := &models.Product{
		ProductID:   uuid.New().String(),
		TenantID:    tenantID,
		Status:      "draft",
		SKU:         row.ChannelSKU,
		Title:       title,
		ProductType: "simple",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		Identifiers: &models.ProductIdentifiers{},
		Tags:        []string{"imported", "draft", "channel:" + channel},
	}

	if err := h.productService.CreateProduct(ctx, product); err != nil {
		return "", fmt.Errorf("create draft product: %w", err)
	}
	log.Printf("Reconcile: created draft product %s for channel SKU %s", product.ProductID, row.ChannelSKU)

	if h.searchService != nil {
		if indexErr := h.searchService.IndexProduct(product); indexErr != nil {
			log.Printf("Reconcile: Typesense index failed for draft product %s: %v", product.ProductID, indexErr)
		}
	}

	return product.ProductID, nil
}

// ── triggerEnrichment ─────────────────────────────────────────────────────────

func (h *ReconcileHandler) triggerEnrichment(tenantID, credentialID, channel string, rows []ReconcileRow) {
	ctx := context.Background()
	log.Printf("Reconcile: triggering enrichment for %d %s rows", len(rows), channel)

	switch channel {
	case "amazon":
		for _, row := range rows {
			if row.InternalProductID == "" {
				continue
			}
			h.triggerAmazonEnrichment(ctx, tenantID, credentialID, row)
		}
	case "ebay":
		for _, row := range rows {
			if row.InternalProductID == "" {
				continue
			}
			if h.ebayEnrichHandler == nil {
				log.Printf("Reconcile: eBay enrich handler not injected — skipping %s", row.ChannelSKU)
				continue
			}
			body, _ := json.Marshal(map[string]interface{}{
				"product_id":    row.InternalProductID,
				"ebay_item_id":  row.ExternalID,
				"credential_id": credentialID,
				"ai_enrich":     row.AIEnrich,
			})
			go func(payload []byte) {
				resp, err := http.Post("http://localhost:8080/api/v1/ebay/enrich/product",
					"application/json", bytes.NewReader(payload))
				if err != nil {
					log.Printf("Reconcile: eBay enrich call failed: %v", err)
					return
				}
				defer resp.Body.Close()
				log.Printf("Reconcile: eBay enrich triggered for product %s", row.InternalProductID)
			}(body)
		}
	}
}

func (h *ReconcileHandler) triggerAmazonEnrichment(ctx context.Context, tenantID, credentialID string, row ReconcileRow) {
	asin := row.ExternalID
	if asin == "" {
		asin = row.ChannelSKU
	}
	log.Printf("Reconcile: Amazon enrichment queued for product %s (ASIN: %s)", row.InternalProductID, asin)
	go func() {
		_ = h.storePendingEnrichmentNote(ctx, tenantID, row.InternalProductID, "amazon", asin)
	}()
}

func (h *ReconcileHandler) storePendingEnrichmentNote(ctx context.Context, tenantID, productID, channel, externalRef string) error {
	_, err := h.fsClient.Collection("tenants").Doc(tenantID).
		Collection("products").Doc(productID).
		Collection("extended_data").Doc(channel+"_pending").
		Set(ctx, map[string]interface{}{
			"status": "pending_enrichment", "channel": channel,
			"external_ref": externalRef, "queued_at": time.Now(), "source": "reconcile",
		})
	return err
}

// ── Firestore persistence ─────────────────────────────────────────────────────
// Two writes: active doc (fast reads) + history subcollection (/runs/{jobID})

func (h *ReconcileHandler) saveJob(ctx context.Context, tenantID, credentialID string, job *ReconcileJob) error {
	base := h.fsClient.Collection("tenants").Doc(tenantID).
		Collection("reconcile_jobs").Doc(credentialID)

	if _, err := base.Set(ctx, job); err != nil {
		return err
	}

	// History: strip rows array to keep documents small
	historySummary := *job
	historySummary.Rows = nil
	if _, err := base.Collection("runs").Doc(job.JobID).Set(ctx, historySummary); err != nil {
		log.Printf("Reconcile: failed to write history run %s: %v", job.JobID, err)
	}

	return nil
}

func (h *ReconcileHandler) loadJob(ctx context.Context, tenantID, credentialID string) (*ReconcileJob, error) {
	doc, err := h.fsClient.Collection("tenants").Doc(tenantID).
		Collection("reconcile_jobs").Doc(credentialID).
		Get(ctx)
	if err != nil {
		return nil, err
	}
	var job ReconcileJob
	if err := doc.DataTo(&job); err != nil {
		return nil, err
	}
	return &job, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func appendOrUpdateMapping(existing []models.InventoryMapping, m models.InventoryMapping) []models.InventoryMapping {
	for i, e := range existing {
		if e.ChannelSKU == m.ChannelSKU {
			existing[i] = m
			return existing
		}
	}
	return append(existing, m)
}

func jaroWinkler(s1, s2 string) float64 {
	jaro := jaroSimilarity(s1, s2)
	prefix := 0
	for i := 0; i < len(s1) && i < len(s2) && prefix < 4; i++ {
		if s1[i] == s2[i] {
			prefix++
		} else {
			break
		}
	}
	return jaro + float64(prefix)*0.1*(1-jaro)
}

func jaroSimilarity(s1, s2 string) float64 {
	if s1 == s2 {
		return 1.0
	}
	l1, l2 := len(s1), len(s2)
	if l1 == 0 || l2 == 0 {
		return 0.0
	}
	matchDist := int(math.Max(float64(l1), float64(l2))/2) - 1
	if matchDist < 0 {
		matchDist = 0
	}
	s1Matches := make([]bool, l1)
	s2Matches := make([]bool, l2)
	matches := 0
	transpositions := 0

	for i := 0; i < l1; i++ {
		start := int(math.Max(0, float64(i-matchDist)))
		end := int(math.Min(float64(i+matchDist+1), float64(l2)))
		for j := start; j < end; j++ {
			if s2Matches[j] || s1[i] != s2[j] {
				continue
			}
			s1Matches[i] = true
			s2Matches[j] = true
			matches++
			break
		}
	}
	if matches == 0 {
		return 0.0
	}
	k := 0
	for i := 0; i < l1; i++ {
		if !s1Matches[i] {
			continue
		}
		for !s2Matches[k] {
			k++
		}
		if s1[i] != s2[k] {
			transpositions++
		}
		k++
	}
	m := float64(matches)
	return (m/float64(l1) + m/float64(l2) + (m-float64(transpositions)/2)/m) / 3
}
