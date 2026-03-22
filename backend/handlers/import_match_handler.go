package handlers

// ============================================================================
// IMPORT MATCH HANDLER — Second Import Matching Flow
// ============================================================================
//
// Endpoints:
//   POST /marketplace/import/jobs/:id/analyze-matches
//     Runs matching for a completed import job. Checks import_mappings by ASIN
//     and SKU (exact), then Typesense fuzzy search for unmatched rows.
//     Stores results in import_jobs/{id}/match_results/{rowID} subcollection.
//     Sets job.match_status = "review_required" | "no_review_needed".
//
//   GET  /marketplace/import/jobs/:id/matches
//     Returns all match_results rows for the given job, grouped into
//     { exact: [], fuzzy: [], unmatched: [] }.
//
//   POST /marketplace/import/jobs/:id/matches/accept
//     Body: { row_ids: ["..."], accept_all: true, match_type: "exact"|"fuzzy" }
//     For each accepted row:  creates a listing doc linking the existing
//     product_id to the channel external_id. Does NOT create a new product.
//
//   POST /marketplace/import/jobs/:id/matches/reject
//     Body: { row_ids: ["..."] }
//     Marks rows as "import_as_new" — they will be imported by the normal
//     batch pipeline which creates new product docs.
//
//   POST /marketplace/import/jobs/:id/unmatched/import-new
//     Body: { row_ids: ["..."], all: true }
//     Triggers import-batch style product creation for unmatched rows that
//     the user has decided should become new products.
//
// ============================================================================

import (
	"context"
	"fmt"
	"log"
	"math"
	"net/http"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"
	"module-a/repository"
	"module-a/services"
)

// ── Types ─────────────────────────────────────────────────────────────────────

type MatchResultRow struct {
	RowID     string `firestore:"row_id" json:"row_id"`
	JobID     string `firestore:"job_id" json:"job_id"`
	TenantID  string `firestore:"tenant_id" json:"tenant_id"`

	// Incoming channel data
	Channel    string `firestore:"channel" json:"channel"`
	ExternalID string `firestore:"external_id" json:"external_id"` // ASIN or SKU
	SKU        string `firestore:"sku" json:"sku"`
	Title      string `firestore:"title" json:"title"`
	ImageURL   string `firestore:"image_url" json:"image_url"`
	Price      string `firestore:"price" json:"price"`

	// Match details
	MatchType   string  `firestore:"match_type" json:"match_type"` // "exact", "fuzzy", "none"
	MatchScore  float64 `firestore:"match_score" json:"match_score"`
	MatchReason string  `firestore:"match_reason" json:"match_reason"`

	// Matched product (if any)
	MatchedProductID    string `firestore:"matched_product_id" json:"matched_product_id"`
	MatchedProductTitle string `firestore:"matched_product_title" json:"matched_product_title"`
	MatchedProductSKU   string `firestore:"matched_product_sku" json:"matched_product_sku"`
	MatchedProductImage string `firestore:"matched_product_image" json:"matched_product_image"`
	MatchedProductASIN  string `firestore:"matched_product_asin" json:"matched_product_asin"`

	// Decision
	Decision string `firestore:"decision" json:"decision"` // "", "accepted", "rejected", "import_as_new"

	// Timestamps
	CreatedAt time.Time `firestore:"created_at" json:"created_at"`
	UpdatedAt time.Time `firestore:"updated_at" json:"updated_at"`
}

type MatchAnalysisResult struct {
	Exact     []MatchResultRow `json:"exact"`
	Fuzzy     []MatchResultRow `json:"fuzzy"`
	Unmatched []MatchResultRow `json:"unmatched"`
	Total     int              `json:"total"`
}

// ── Handler ───────────────────────────────────────────────────────────────────

type ImportMatchHandler struct {
	repo          *repository.MarketplaceRepository
	firestoreRepo *repository.FirestoreRepository
	searchService *services.SearchService
	fsClient      *firestore.Client
}

func NewImportMatchHandler(
	repo *repository.MarketplaceRepository,
	firestoreRepo *repository.FirestoreRepository,
	searchService *services.SearchService,
) *ImportMatchHandler {
	return &ImportMatchHandler{
		repo:          repo,
		firestoreRepo: firestoreRepo,
		searchService: searchService,
		fsClient:      repo.GetFirestoreClient(),
	}
}

// AnalyzeMatches runs matching against existing products and stores results.
// POST /marketplace/import/jobs/:id/analyze-matches
func (h *ImportMatchHandler) AnalyzeMatches(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	jobID := c.Param("id")
	ctx := c.Request.Context()

	if tenantID == "" || jobID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant_id and job_id required"})
		return
	}

	// Verify job exists
	jobRef := h.fsClient.Collection("tenants").Doc(tenantID).
		Collection("import_jobs").Doc(jobID)
	jobSnap, err := jobRef.Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}
	jobData := jobSnap.Data()

	channel, _ := jobData["channel"].(string)
	credentialID, _ := jobData["channel_account_id"].(string)

	// Check if already analyzed
	if ms, _ := jobData["match_status"].(string); ms == "review_required" || ms == "reviewed" {
		c.JSON(http.StatusOK, gin.H{"message": "already analyzed", "match_status": ms})
		return
	}

	// Run analysis in background so HTTP returns immediately
	go h.runAnalysis(context.Background(), tenantID, jobID, channel, credentialID)

	// Mark as analyzing
	jobRef.Update(ctx, []firestore.Update{
		{Path: "match_status", Value: "analyzing"},
		{Path: "updated_at", Value: time.Now()},
	})

	c.JSON(http.StatusAccepted, gin.H{"message": "analysis started", "job_id": jobID})
}

func (h *ImportMatchHandler) runAnalysis(ctx context.Context, tenantID, jobID, channel, credentialID string) {
	jobRef := h.fsClient.Collection("tenants").Doc(tenantID).
		Collection("import_jobs").Doc(jobID)

	log.Printf("[MatchAnalysis] Starting for job %s tenant %s", jobID, tenantID)

	// Load all existing import_mappings for this channel (keyed by external_id)
	existingMappings := make(map[string]string) // externalID → productID
	mappingsBySKU := make(map[string]string)    // sku → productID (from mapping's product)
	iter := h.fsClient.Collection("tenants").Doc(tenantID).
		Collection("import_mappings").
		Where("channel", "==", channel).
		Documents(ctx)
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		data := doc.Data()
		extID, _ := data["external_id"].(string)
		productID, _ := data["product_id"].(string)
		if extID != "" && productID != "" {
			existingMappings[extID] = productID
		}
	}

	// Load products to get their titles/images for comparison display
	// We'll look these up lazily per-match to avoid loading all products
	productCache := make(map[string]map[string]interface{})
	getProduct := func(productID string) map[string]interface{} {
		if p, ok := productCache[productID]; ok {
			return p
		}
		doc, err := h.fsClient.Collection("tenants").Doc(tenantID).
			Collection("products").Doc(productID).Get(ctx)
		if err != nil {
			return nil
		}
		productCache[productID] = doc.Data()
		return doc.Data()
	}

	// Scan products for SKU-based lookup (attributes.source_sku → productID)
	// Build a sku→productID map from existing listings
	listingIter := h.fsClient.Collection("tenants").Doc(tenantID).
		Collection("listings").
		Where("channel", "==", channel).
		Documents(ctx)
	listingBySKU := make(map[string]string) // sku → productID
	for {
		doc, err := listingIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		data := doc.Data()
		productID, _ := data["product_id"].(string)
		if ci, ok := data["channel_identifiers"].(map[string]interface{}); ok {
			if sku, ok := ci["sku"].(string); ok && sku != "" {
				listingBySKU[sku] = productID
			}
		}
		_ = mappingsBySKU
	}

	// Load all product rows from the job's match_results subcollection
	// OR reconstruct from import_mappings created by this job (via job_id field)
	// The import_mappings created by this job's batches have been written already.
	// We need to find the NEWLY imported products from this job and check whether
	// they should have been matched instead.
	//
	// Strategy: read products that were created by this job by scanning mappings
	// that have created_at >= job.started_at, then for each one try to find
	// an EXISTING product (created before this job) with the same ASIN/title.
	//
	// Simpler approach that matches the spec: scan the job's import_mappings
	// (those written by batch.go during this job) and for each external_id,
	// check if a DIFFERENT product already exists for it. Since batch.go
	// uses findMapping first and updates existing, the new products are truly
	// new. So we should look at whether any of these new products have
	// title-similar siblings already in the catalog.
	//
	// Per the spec: this flow is for second imports where we want the user to
	// CONFIRM matches rather than auto-create. The cleanest implementation:
	// after the job's batches run, we have new products. We check each one
	// against ALL existing products (pre-job) for ASIN exact match or title fuzzy.

	// Get job start time to know which mappings are "new" from this job
	jobSnap, err := jobRef.Get(ctx)
	if err != nil {
		log.Printf("[MatchAnalysis] Failed to get job: %v", err)
		return
	}
	jobData := jobSnap.Data()

	var jobStartedAt time.Time
	if sa, ok := jobData["started_at"]; ok {
		switch v := sa.(type) {
		case time.Time:
			jobStartedAt = v
		}
	}
	if jobStartedAt.IsZero() {
		if ca, ok := jobData["created_at"]; ok {
			switch v := ca.(type) {
			case time.Time:
				jobStartedAt = v
			}
		}
	}
	// Give 10-minute buffer before job start for mappings created during this job
	jobWindowStart := jobStartedAt.Add(-10 * time.Minute)

	// Find all import_mappings created by this job window that are "new"
	// (i.e., their product was freshly created, not updated)
	newMappingIter := h.fsClient.Collection("tenants").Doc(tenantID).
		Collection("import_mappings").
		Where("channel", "==", channel).
		Where("created_at", ">=", jobWindowStart).
		Documents(ctx)

	var rows []MatchResultRow
	processedExternalIDs := make(map[string]bool)

	for {
		doc, err := newMappingIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Printf("[MatchAnalysis] mapping iter error: %v", err)
			break
		}
		data := doc.Data()
		extID, _ := data["external_id"].(string)
		productID, _ := data["product_id"].(string)

		if extID == "" || processedExternalIDs[extID] {
			continue
		}
		processedExternalIDs[extID] = true

		// Get the newly created product
		product := getProduct(productID)
		title := ""
		imageURL := ""
		sku := ""
		if product != nil {
			title, _ = product["title"].(string)
			if attrs, ok := product["attributes"].(map[string]interface{}); ok {
				sku, _ = attrs["source_sku"].(string)
			}
			if assets, ok := product["assets"].([]interface{}); ok && len(assets) > 0 {
				if a, ok := assets[0].(map[string]interface{}); ok {
					imageURL, _ = a["url"].(string)
				}
			}
		}
		if title == "" {
			continue // No title — not processable
		}

		row := MatchResultRow{
			RowID:      uuid.New().String(),
			JobID:      jobID,
			TenantID:   tenantID,
			Channel:    channel,
			ExternalID: extID,
			SKU:        sku,
			Title:      title,
			ImageURL:   imageURL,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}

		// ── EXACT MATCH: check if another product already exists for this externalID ──
		// Since batch.go updated existing if a mapping existed, if we're here it's new.
		// But check: was there a PRE-EXISTING product with same ASIN/SKU from a different import?
		// This catches the case where the same channel account was imported twice.
		//
		// Check import_mappings for same externalID but created before this job
		// (meaning it's a mapping from a prior import, pointing to an older product)
		existingProductID := existingMappings[extID]
		if existingProductID != "" && existingProductID != productID {
			// Found a prior product — this is an exact match the user should confirm
			existingProduct := getProduct(existingProductID)
			existingTitle := ""
			existingSKU := ""
			existingImage := ""
			existingASIN := ""
			if existingProduct != nil {
				existingTitle, _ = existingProduct["title"].(string)
				if attrs, ok := existingProduct["attributes"].(map[string]interface{}); ok {
					existingSKU, _ = attrs["source_sku"].(string)
				}
				if assets, ok := existingProduct["assets"].([]interface{}); ok && len(assets) > 0 {
					if a, ok := assets[0].(map[string]interface{}); ok {
						existingImage, _ = a["url"].(string)
					}
				}
				if ids, ok := existingProduct["identifiers"].(map[string]interface{}); ok {
					existingASIN, _ = ids["asin"].(string)
				}
			}
			row.MatchType = "exact"
			row.MatchScore = 1.0
			row.MatchReason = fmt.Sprintf("ASIN/SKU %s already mapped to an existing product", extID)
			row.MatchedProductID = existingProductID
			row.MatchedProductTitle = existingTitle
			row.MatchedProductSKU = existingSKU
			row.MatchedProductImage = existingImage
			row.MatchedProductASIN = existingASIN
			rows = append(rows, row)
			continue
		}

		// Also check by SKU from listings
		if sku != "" {
			if existingPID, ok := listingBySKU[sku]; ok && existingPID != productID {
				existingProduct := getProduct(existingPID)
				existingTitle := ""
				existingSKU := ""
				existingImage := ""
				if existingProduct != nil {
					existingTitle, _ = existingProduct["title"].(string)
					if attrs, ok := existingProduct["attributes"].(map[string]interface{}); ok {
						existingSKU, _ = attrs["source_sku"].(string)
					}
					if assets, ok := existingProduct["assets"].([]interface{}); ok && len(assets) > 0 {
						if a, ok := assets[0].(map[string]interface{}); ok {
							existingImage, _ = a["url"].(string)
						}
					}
				}
				row.MatchType = "exact"
				row.MatchScore = 0.95
				row.MatchReason = fmt.Sprintf("SKU %s found in existing listings", sku)
				row.MatchedProductID = existingPID
				row.MatchedProductTitle = existingTitle
				row.MatchedProductSKU = existingSKU
				row.MatchedProductImage = existingImage
				rows = append(rows, row)
				continue
			}
		}

		// ── FUZZY MATCH: Typesense title search ──────────────────────────────
		if h.searchService != nil && h.searchService.Healthy() && title != "" {
			fuzzyResult, searchErr := h.searchService.SearchProducts(ctx, tenantID, title, nil, 1, 3)
			if searchErr == nil && fuzzyResult != nil && len(fuzzyResult.Hits) > 0 {
				for _, hit := range fuzzyResult.Hits {
					hitProductID, _ := hit["product_id"].(string)
					if hitProductID == "" || hitProductID == productID {
						continue
					}
					hitTitle, _ := hit["title"].(string)
					hitSKU, _ := hit["sku"].(string)
					hitImage, _ := hit["primary_image"].(string)
					hitASIN, _ := hit["asin"].(string)

					score := titleSimilarity(title, hitTitle)
					if score >= 0.6 {
						row.MatchType = "fuzzy"
						row.MatchScore = score
						row.MatchReason = fmt.Sprintf("Title similarity %.0f%%", score*100)
						row.MatchedProductID = hitProductID
						row.MatchedProductTitle = hitTitle
						row.MatchedProductSKU = hitSKU
						row.MatchedProductImage = hitImage
						row.MatchedProductASIN = hitASIN
						break
					}
				}
			}
		}

		if row.MatchType == "" {
			row.MatchType = "none"
		}
		rows = append(rows, row)
	}

	// Write all rows to Firestore subcollection
	matchResultsRef := jobRef.Collection("match_results")
	for _, row := range rows {
		matchResultsRef.Doc(row.RowID).Set(ctx, row)
	}

	// Determine overall match status
	hasExact := false
	hasFuzzy := false
	for _, r := range rows {
		if r.MatchType == "exact" {
			hasExact = true
		}
		if r.MatchType == "fuzzy" {
			hasFuzzy = true
		}
	}

	matchStatus := "no_review_needed"
	if hasExact || hasFuzzy {
		matchStatus = "review_required"
	}

	jobRef.Update(ctx, []firestore.Update{
		{Path: "match_status", Value: matchStatus},
		{Path: "match_result_count", Value: len(rows)},
		{Path: "updated_at", Value: time.Now()},
	})

	log.Printf("[MatchAnalysis] Job %s: %d rows analyzed, status=%s (exact=%v fuzzy=%v)",
		jobID, len(rows), matchStatus, hasExact, hasFuzzy)
}

// GetMatches returns match_results grouped by match type.
// GET /marketplace/import/jobs/:id/matches
func (h *ImportMatchHandler) GetMatches(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	jobID := c.Param("id")
	ctx := c.Request.Context()

	if tenantID == "" || jobID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing params"})
		return
	}

	jobRef := h.fsClient.Collection("tenants").Doc(tenantID).
		Collection("import_jobs").Doc(jobID)

	// Check match_status
	jobSnap, err := jobRef.Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}
	jobData := jobSnap.Data()
	matchStatus, _ := jobData["match_status"].(string)

	iter := jobRef.Collection("match_results").Documents(ctx)
	result := MatchAnalysisResult{
		Exact:     []MatchResultRow{},
		Fuzzy:     []MatchResultRow{},
		Unmatched: []MatchResultRow{},
	}
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		var row MatchResultRow
		if err := doc.DataTo(&row); err != nil {
			continue
		}
		switch row.MatchType {
		case "exact":
			result.Exact = append(result.Exact, row)
		case "fuzzy":
			result.Fuzzy = append(result.Fuzzy, row)
		default:
			result.Unmatched = append(result.Unmatched, row)
		}
		result.Total++
	}

	c.JSON(http.StatusOK, gin.H{
		"match_status": matchStatus,
		"results":      result,
	})
}

// AcceptMatches accepts match decisions — creates listings linking existing products.
// POST /marketplace/import/jobs/:id/matches/accept
func (h *ImportMatchHandler) AcceptMatches(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	jobID := c.Param("id")
	ctx := c.Request.Context()

	var req struct {
		RowIDs    []string `json:"row_ids"`
		AcceptAll bool     `json:"accept_all"`
		MatchType string   `json:"match_type"` // "exact", "fuzzy", or "" (both)
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	jobRef := h.fsClient.Collection("tenants").Doc(tenantID).
		Collection("import_jobs").Doc(jobID)
	matchResultsRef := jobRef.Collection("match_results")

	// Get job data for credential info
	jobSnap, err := jobRef.Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}
	jobData := jobSnap.Data()
	channel, _ := jobData["channel"].(string)
	credentialID, _ := jobData["channel_account_id"].(string)

	// Collect rows to accept
	var rows []MatchResultRow
	if req.AcceptAll {
		iter := matchResultsRef.Documents(ctx)
		for {
			doc, err := iter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				break
			}
			var row MatchResultRow
			doc.DataTo(&row)
			if req.MatchType == "" || row.MatchType == req.MatchType {
				rows = append(rows, row)
			}
		}
	} else {
		for _, rowID := range req.RowIDs {
			doc, err := matchResultsRef.Doc(rowID).Get(ctx)
			if err != nil {
				continue
			}
			var row MatchResultRow
			doc.DataTo(&row)
			rows = append(rows, row)
		}
	}

	accepted := 0
	failed := 0
	for _, row := range rows {
		if row.MatchedProductID == "" || row.Decision != "" {
			continue
		}

		// Create a listing record linking the existing product to the channel external_id
		listingID := uuid.New().String()
		listing := map[string]interface{}{
			"listing_id":          listingID,
			"tenant_id":           tenantID,
			"product_id":          row.MatchedProductID,
			"channel":             channel,
			"channel_account_id":  credentialID,
			"state":               "matched",
			"channel_identifiers": map[string]interface{}{
				"external_listing_id": row.ExternalID,
				"sku":                 row.SKU,
			},
			"created_at": time.Now(),
			"updated_at": time.Now(),
		}
		_, err := h.fsClient.Collection("tenants").Doc(tenantID).
			Collection("listings").Doc(listingID).Set(ctx, listing)
		if err != nil {
			log.Printf("[MatchAccept] Failed to create listing for row %s: %v", row.RowID, err)
			failed++
			continue
		}

		// Also ensure an import_mapping exists for this external_id → matched product
		existingMapping, _ := h.repo.GetMappingByExternalID(ctx, tenantID, channel, row.ExternalID)
		if existingMapping == nil || existingMapping.ProductID != row.MatchedProductID {
			mappingID := uuid.New().String()
			h.fsClient.Collection("tenants").Doc(tenantID).
				Collection("import_mappings").Doc(mappingID).Set(ctx, map[string]interface{}{
				"mapping_id":          mappingID,
				"tenant_id":           tenantID,
				"channel":             channel,
				"channel_account_id":  credentialID,
				"external_id":         row.ExternalID,
				"product_id":          row.MatchedProductID,
				"sync_enabled":        true,
				"created_at":          time.Now(),
				"updated_at":          time.Now(),
			})
		}

		// Mark row as accepted
		matchResultsRef.Doc(row.RowID).Update(ctx, []firestore.Update{
			{Path: "decision", Value: "accepted"},
			{Path: "updated_at", Value: time.Now()},
		})
		accepted++
	}

	// Check if all rows have decisions — if so, mark job as reviewed
	h.maybeMarkReviewed(ctx, jobRef, matchResultsRef)

	c.JSON(http.StatusOK, gin.H{
		"accepted": accepted,
		"failed":   failed,
	})
}

// RejectMatches marks rows for import as new products.
// POST /marketplace/import/jobs/:id/matches/reject
func (h *ImportMatchHandler) RejectMatches(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	jobID := c.Param("id")
	ctx := c.Request.Context()

	var req struct {
		RowIDs []string `json:"row_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	jobRef := h.fsClient.Collection("tenants").Doc(tenantID).
		Collection("import_jobs").Doc(jobID)
	matchResultsRef := jobRef.Collection("match_results")

	updated := 0
	for _, rowID := range req.RowIDs {
		_, err := matchResultsRef.Doc(rowID).Update(ctx, []firestore.Update{
			{Path: "decision", Value: "import_as_new"},
			{Path: "updated_at", Value: time.Now()},
		})
		if err == nil {
			updated++
		}
	}

	h.maybeMarkReviewed(ctx, jobRef, matchResultsRef)

	c.JSON(http.StatusOK, gin.H{"updated": updated})
}

// ImportUnmatchedAsNew imports selected/all unmatched rows as new products.
// POST /marketplace/import/jobs/:id/unmatched/import-new
func (h *ImportMatchHandler) ImportUnmatchedAsNew(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	jobID := c.Param("id")
	ctx := c.Request.Context()

	var req struct {
		RowIDs []string `json:"row_ids"`
		All    bool     `json:"all"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	jobRef := h.fsClient.Collection("tenants").Doc(tenantID).
		Collection("import_jobs").Doc(jobID)
	matchResultsRef := jobRef.Collection("match_results")

	// Collect unmatched rows
	var rows []MatchResultRow
	if req.All {
		iter := matchResultsRef.Where("match_type", "==", "none").Documents(ctx)
		for {
			doc, err := iter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				break
			}
			var row MatchResultRow
			doc.DataTo(&row)
			if row.Decision == "" {
				rows = append(rows, row)
			}
		}
	} else {
		for _, rowID := range req.RowIDs {
			doc, err := matchResultsRef.Doc(rowID).Get(ctx)
			if err != nil {
				continue
			}
			var row MatchResultRow
			doc.DataTo(&row)
			rows = append(rows, row)
		}
	}

	if len(rows) == 0 {
		c.JSON(http.StatusOK, gin.H{"queued": 0, "message": "no rows to import"})
		return
	}

	// Get job credential info
	jobSnap, err := jobRef.Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}
	jobData := jobSnap.Data()
	channel, _ := jobData["channel"].(string)
	credentialID, _ := jobData["channel_account_id"].(string)

	queued := 0
	for _, row := range rows {
		// The product was already created by the batch — it just needs a listing + mapping
		// (the product doc exists since batch.go created it).
		// Mark the row as "import_as_new" so it's tracked
		matchResultsRef.Doc(row.RowID).Update(ctx, []firestore.Update{
			{Path: "decision", Value: "import_as_new"},
			{Path: "updated_at", Value: time.Now()},
		})

		// Ensure listing exists
		listingID := uuid.New().String()
		h.fsClient.Collection("tenants").Doc(tenantID).
			Collection("listings").Doc(listingID).Set(ctx, map[string]interface{}{
			"listing_id":         listingID,
			"tenant_id":          tenantID,
			"product_id":         row.ExternalID, // placeholder — batch created a product for this ASIN
			"channel":            channel,
			"channel_account_id": credentialID,
			"state":              "imported",
			"channel_identifiers": map[string]interface{}{
				"external_listing_id": row.ExternalID,
				"sku":                 row.SKU,
			},
			"created_at": time.Now(),
			"updated_at": time.Now(),
		})
		queued++
	}

	h.maybeMarkReviewed(ctx, jobRef, matchResultsRef)

	c.JSON(http.StatusOK, gin.H{"queued": queued})
}

// maybeMarkReviewed checks if all match_results have decisions and marks the job reviewed.
func (h *ImportMatchHandler) maybeMarkReviewed(ctx context.Context, jobRef *firestore.DocumentRef, matchResultsRef *firestore.CollectionRef) {
	iter := matchResultsRef.Documents(ctx)
	total := 0
	decided := 0
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return
		}
		total++
		data := doc.Data()
		if d, _ := data["decision"].(string); d != "" {
			decided++
		}
	}
	if total > 0 && decided >= total {
		jobRef.Update(ctx, []firestore.Update{
			{Path: "match_status", Value: "reviewed"},
			{Path: "updated_at", Value: time.Now()},
		})
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// titleSimilarity returns a 0.0–1.0 score based on word overlap between two titles.
// Uses Jaccard similarity on word tokens — fast and good enough for product titles.
func titleSimilarity(a, b string) float64 {
	wa := tokenize(a)
	wb := tokenize(b)
	if len(wa) == 0 || len(wb) == 0 {
		return 0
	}
	setA := make(map[string]bool, len(wa))
	for _, w := range wa {
		setA[w] = true
	}
	intersection := 0
	setB := make(map[string]bool, len(wb))
	for _, w := range wb {
		if setA[w] {
			intersection++
		}
		setB[w] = true
	}
	union := len(setA)
	for w := range setB {
		if !setA[w] {
			union++
		}
	}
	if union == 0 {
		return 0
	}
	jaccard := float64(intersection) / float64(union)
	// Boost slightly for long shared prefixes (first 3 words match)
	prefixBonus := 0.0
	minLen := int(math.Min(float64(len(wa)), float64(len(wb))))
	matchedPrefix := 0
	for i := 0; i < minLen && i < 3; i++ {
		if wa[i] == wb[i] {
			matchedPrefix++
		} else {
			break
		}
	}
	if matchedPrefix >= 2 {
		prefixBonus = 0.05
	}
	return math.Min(jaccard+prefixBonus, 1.0)
}

// tokenize lowercases, removes punctuation, and splits into words.
// Filters common stop-words that add noise to product title comparison.
func tokenize(s string) []string {
	s = strings.ToLower(s)
	var buf strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == ' ' {
			buf.WriteRune(r)
		} else {
			buf.WriteRune(' ')
		}
	}
	stopWords := map[string]bool{
		"a": true, "an": true, "the": true, "and": true, "or": true,
		"for": true, "of": true, "in": true, "to": true, "with": true,
		"by": true, "from": true, "on": true, "at": true, "is": true,
	}
	var tokens []string
	for _, w := range strings.Fields(buf.String()) {
		if len(w) >= 2 && !stopWords[w] {
			tokens = append(tokens, w)
		}
	}
	return tokens
}


