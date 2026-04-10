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
	go h.RunAnalysis(context.Background(), tenantID, jobID, channel, credentialID)

	// Mark as analyzing
	jobRef.Update(ctx, []firestore.Update{
		{Path: "match_status", Value: "analyzing"},
		{Path: "updated_at", Value: time.Now()},
	})

	c.JSON(http.StatusAccepted, gin.H{"message": "analysis started", "job_id": jobID})
}

func (h *ImportMatchHandler) RunAnalysis(ctx context.Context, tenantID, jobID, channel, credentialID string) {
	jobRef := h.fsClient.Collection("tenants").Doc(tenantID).
		Collection("import_jobs").Doc(jobID)

	log.Printf("[MatchAnalysis] Starting for job %s tenant %s channel %s", jobID, tenantID, channel)
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

	// Pre-load ALL pending_imports for this tenant into memory.
	// This replaces the per-product Firestore lookup in the loop (which was doing
	// 580+ sequential round trips) with a single collection scan.
	pendingImportsCache := make(map[string]map[string]interface{}) // productID → data
	pendingIter := h.fsClient.Collection("tenants").Doc(tenantID).
		Collection("pending_imports").Documents(ctx)
	for {
		doc, err := pendingIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Printf("[MatchAnalysis] pending_imports scan error: %v", err)
			break
		}
		data := doc.Data()
		if pid, ok := data["product_id"].(string); ok && pid != "" {
			pendingImportsCache[pid] = data
		}
	}
	log.Printf("[MatchAnalysis] Pre-loaded %d pending_imports into cache", len(pendingImportsCache))

	// Load products to get their titles/images for comparison display.
	// Check pending_imports cache first, then fall back to products/ collection.
	productCache := make(map[string]map[string]interface{})
	getProduct := func(productID string) map[string]interface{} {
		if p, ok := productCache[productID]; ok {
			return p
		}
		// Check pending_imports cache first (most common case for pending_review jobs)
		if p, ok := pendingImportsCache[productID]; ok {
			productCache[productID] = p
			return p
		}
		// Fall back to products/ collection for confirmed products
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
	// Find all pending_review products for this channel by querying mappings with
	// source_collection = "pending_imports". This is set by processImportedProduct
	// for all pending_review jobs, making it a clean single-field filter with no
	// composite index requirement.
	jobSnap, err := jobRef.Get(ctx)
	if err != nil {
		log.Printf("[MatchAnalysis] Failed to get job: %v", err)
		return
	}
	jobData := jobSnap.Data()
	_ = jobData // channel/credential already extracted above

	// Query all Temu mappings for this channel.
	// We identify pending_imports products by:
	// 1. source_collection == "pending_imports" (new mappings), OR
	// 2. product_id exists in pending_imports collection (old mappings written before source_collection field was added)
	// We filter in-memory to avoid composite index requirements.
	newMappingIter := h.fsClient.Collection("tenants").Doc(tenantID).
		Collection("import_mappings").
		Where("channel", "==", channel).
		Documents(ctx)

	var rows []MatchResultRow
	processedExternalIDs := make(map[string]bool)
	totalMappings := 0
	skippedNotPending := 0
	skippedDuplicate := 0

	for {
		doc, err := newMappingIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Printf("[MatchAnalysis] mapping iter error: %v", err)
			break
		}
		totalMappings++
		data := doc.Data()
		extID, _ := data["external_id"].(string)
		productID, _ := data["product_id"].(string)
		sourceCollection, _ := data["source_collection"].(string)

		log.Printf("[MatchAnalysis] Mapping: extID=%s productID=%s sourceCollection=%q", extID, productID, sourceCollection)

		// Determine if this product is in pending_imports.
		isPending := sourceCollection == "pending_imports"
		if !isPending && sourceCollection == "" && productID != "" {
			pendingDoc, err := h.fsClient.Collection("tenants").Doc(tenantID).
				Collection("pending_imports").Doc(productID).Get(ctx)
			if err == nil && pendingDoc.Exists() {
				isPending = true
				log.Printf("[MatchAnalysis] Mapping %s: found in pending_imports via direct lookup", productID)
			}
		}

		// Only process mappings pointing to pending_imports
		if !isPending {
			skippedNotPending++
			continue
		}

		if extID == "" || processedExternalIDs[extID] {
			skippedDuplicate++
			continue
		}
		processedExternalIDs[extID] = true

		// Get the newly created product (may be in products/ or pending_imports/)
		product := getProduct(productID)
		title := ""
		imageURL := ""
		sku := ""
		if product != nil {
			title, _ = product["title"].(string)
			// SKU: top-level field (set by convertToPIMProduct)
			sku, _ = product["sku"].(string)
			if sku == "" {
				// Fallback: attributes.source_sku (older format)
				if attrs, ok := product["attributes"].(map[string]interface{}); ok {
					sku, _ = attrs["source_sku"].(string)
				}
			}
			// Image: from assets array
			if assets, ok := product["assets"].([]interface{}); ok && len(assets) > 0 {
				if a, ok := assets[0].(map[string]interface{}); ok {
					imageURL, _ = a["url"].(string)
				}
			}
		}
		// If product lookup failed, use the external_id as a fallback title so the
		// row is still written — the user can still make a decision on it.
		if title == "" {
			title = extID
			log.Printf("[MatchAnalysis] Warning: product %s not found in products/ or pending_imports/ — using externalID as title", productID)
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

	// Determine overall match status.
	// For pending_review jobs, ANY rows (including unmatched) require the user
	// to make a decision — they must either import as new or reject.
	// For non-pending jobs, only exact/fuzzy matches need review.
	hasExact := false
	hasFuzzy := false
	hasUnmatched := false
	for _, r := range rows {
		if r.MatchType == "exact" {
			hasExact = true
		}
		if r.MatchType == "fuzzy" {
			hasFuzzy = true
		}
		if r.MatchType == "none" || r.MatchType == "" {
			hasUnmatched = true
		}
	}

	matchStatus := "no_review_needed"
	if hasExact || hasFuzzy || hasUnmatched {
		matchStatus = "review_required"
	}

	jobRef.Update(ctx, []firestore.Update{
		{Path: "match_status", Value: matchStatus},
		{Path: "match_result_count", Value: len(rows)},
		{Path: "updated_at", Value: time.Now()},
	})

	log.Printf("[MatchAnalysis] Job %s: scanned %d mappings, skipped %d not-pending, %d duplicates, wrote %d rows, status=%s",
		jobID, totalMappings, skippedNotPending, skippedDuplicate, len(rows), matchStatus)
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

		// Look up the import mapping to find where the pending product lives
		pendingMapping, _ := h.repo.GetMappingByExternalID(ctx, tenantID, channel, row.ExternalID)
		sourceCollection := "products"
		pendingProductID := ""
		if pendingMapping != nil {
			pendingProductID = pendingMapping.ProductID
			if pendingMapping.SourceCollection != "" {
				sourceCollection = pendingMapping.SourceCollection
			}
		}

		// User accepted a match to an existing confirmed product.
		// The pending product is a duplicate — clean it up.
		if sourceCollection == "pending_imports" && pendingProductID != "" && pendingProductID != row.MatchedProductID {
			// Copy any enrichment data from pending to the matched product
			h.repo.CopyPendingExtendedDataToProduct(ctx, tenantID, pendingProductID, row.MatchedProductID)
			// Delete the pending import — it won't become a product
			if err := h.repo.DeletePendingImport(ctx, tenantID, pendingProductID); err != nil {
				log.Printf("[MatchAccept] Failed to delete pending import %s: %v", pendingProductID, err)
			}
			log.Printf("[MatchAccept] Merged pending import %s into existing product %s", pendingProductID, row.MatchedProductID)
		}

		// Create a listing record pointing to the confirmed matched product
		listingID := uuid.New().String()
		listing := map[string]interface{}{
			"listing_id":          listingID,
			"tenant_id":           tenantID,
			"product_id":          row.MatchedProductID,
			"channel":             channel,
			"channel_account_id":  credentialID,
			"state":               "published",
			"channel_identifiers": map[string]interface{}{
				"listing_id": row.ExternalID,
				"sku":        row.SKU,
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

		// Update the import mapping to point to the confirmed product in products/
		if pendingMapping != nil {
			h.fsClient.Collection("tenants").Doc(tenantID).
				Collection("import_mappings").Doc(pendingMapping.MappingID).
				Update(ctx, []firestore.Update{
					{Path: "product_id", Value: row.MatchedProductID},
					{Path: "source_collection", Value: "products"},
					{Path: "updated_at", Value: time.Now()},
				})
		} else {
			// No existing mapping — create one pointing to the matched product
			mappingID := uuid.New().String()
			h.fsClient.Collection("tenants").Doc(tenantID).
				Collection("import_mappings").Doc(mappingID).Set(ctx, map[string]interface{}{
				"mapping_id":        mappingID,
				"tenant_id":         tenantID,
				"channel":           channel,
				"channel_account_id": credentialID,
				"external_id":       row.ExternalID,
				"product_id":        row.MatchedProductID,
				"source_collection": "products",
				"sync_enabled":      true,
				"created_at":        time.Now(),
				"updated_at":        time.Now(),
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

// RejectMatches permanently rejects rows — deletes the pending import and its mapping.
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

	jobSnap, err := jobRef.Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}
	jobData := jobSnap.Data()
	channel, _ := jobData["channel"].(string)

	updated := 0
	for _, rowID := range req.RowIDs {
		doc, err := matchResultsRef.Doc(rowID).Get(ctx)
		if err != nil {
			continue
		}
		var row MatchResultRow
		doc.DataTo(&row)

		// Delete the pending import and its mapping
		pendingMapping, _ := h.repo.GetMappingByExternalID(ctx, tenantID, channel, row.ExternalID)
		if pendingMapping != nil {
			sourceCollection := pendingMapping.SourceCollection
			if sourceCollection == "" {
				sourceCollection = "products"
			}
			if sourceCollection == "pending_imports" {
				h.repo.DeletePendingImport(ctx, tenantID, pendingMapping.ProductID)
			}
			// Delete the mapping entirely — this external_id is no longer tracked
			h.fsClient.Collection("tenants").Doc(tenantID).
				Collection("import_mappings").Doc(pendingMapping.MappingID).Delete(ctx)
			log.Printf("[RejectMatches] Deleted pending import %s and mapping for external_id %s",
				pendingMapping.ProductID, row.ExternalID)
		}

		_, err = matchResultsRef.Doc(rowID).Update(ctx, []firestore.Update{
			{Path: "decision", Value: "rejected"},
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
		// Look up mapping to find the pending import
		pendingMapping, _ := h.repo.GetMappingByExternalID(ctx, tenantID, channel, row.ExternalID)
		sourceCollection := "products"
		pendingProductID := ""
		if pendingMapping != nil {
			pendingProductID = pendingMapping.ProductID
			if pendingMapping.SourceCollection != "" {
				sourceCollection = pendingMapping.SourceCollection
			}
		}

		var confirmedProductID string

		if sourceCollection == "pending_imports" && pendingProductID != "" {
			// Move the product from pending_imports → products, activating it
			movedID, err := h.repo.MovePendingToProducts(ctx, tenantID, pendingProductID, map[string]interface{}{
				"status":     "active",
				"updated_at": time.Now(),
			})
			if err != nil {
				log.Printf("[ImportAsNew] Failed to move pending import %s: %v", pendingProductID, err)
				continue
			}
			confirmedProductID = movedID

			// Update mapping to point to products collection
			if pendingMapping != nil {
				h.fsClient.Collection("tenants").Doc(tenantID).
					Collection("import_mappings").Doc(pendingMapping.MappingID).
					Update(ctx, []firestore.Update{
						{Path: "source_collection", Value: "products"},
						{Path: "updated_at", Value: time.Now()},
					})
			}
			log.Printf("[ImportAsNew] Moved pending import %s to products/", pendingProductID)
		} else {
			// Already in products (non-pending import, or fallback)
			confirmedProductID = pendingProductID
		}

		if confirmedProductID == "" {
			log.Printf("[ImportAsNew] No product ID found for external_id %s — skipping listing", row.ExternalID)
			continue
		}

		// Create the listing now that the product is confirmed
		listingID := uuid.New().String()
		h.fsClient.Collection("tenants").Doc(tenantID).
			Collection("listings").Doc(listingID).Set(ctx, map[string]interface{}{
			"listing_id":          listingID,
			"tenant_id":           tenantID,
			"product_id":          confirmedProductID,
			"channel":             channel,
			"channel_account_id":  credentialID,
			"state":               "published",
			"channel_identifiers": map[string]interface{}{
				"listing_id": row.ExternalID,
				"sku":        row.SKU,
			},
			"created_at": time.Now(),
			"updated_at": time.Now(),
		})

		// Mark row as decided
		matchResultsRef.Doc(row.RowID).Update(ctx, []firestore.Update{
			{Path: "decision", Value: "import_as_new"},
			{Path: "updated_at", Value: time.Now()},
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



// ============================================================================
// TENANT-WIDE PENDING REVIEW ENDPOINTS
// These are not job-scoped — they aggregate across ALL pending_review jobs
// for the tenant so the /products/review-mappings page has a single source.
// ============================================================================

// GetPendingReviewCount returns the number of items in the pending_imports collection
// (products awaiting review). Also includes undecided match_results rows for backwards compat.
// GET /marketplace/pending-review/count
func (h *ImportMatchHandler) GetPendingReviewCount(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()

	// Primary: count items in pending_imports collection
	pendingCount, err := h.repo.PendingImportCount(ctx, tenantID)
	if err != nil {
		// Fallback to match_results count if aggregation fails
		count, err2 := h.countPendingReviewRows(ctx, tenantID)
		if err2 != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"count": count})
		return
	}
	c.JSON(http.StatusOK, gin.H{"count": pendingCount})
}

// GetPendingReview returns all undecided match_results rows across all
// pending_review jobs for this tenant, grouped by match type.
// GET /marketplace/pending-review
func (h *ImportMatchHandler) GetPendingReview(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()

	result := MatchAnalysisResult{
		Exact:     []MatchResultRow{},
		Fuzzy:     []MatchResultRow{},
		Unmatched: []MatchResultRow{},
	}

	// Find all pending_review import jobs for this tenant
	jobsIter := h.fsClient.Collection("tenants").Doc(tenantID).
		Collection("import_jobs").
		Where("pending_review", "==", true).
		Documents(ctx)
	defer jobsIter.Stop()

	for {
		jobDoc, err := jobsIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed iterating jobs: " + err.Error()})
			return
		}

		jobData := jobDoc.Data()
		matchStatus, _ := jobData["match_status"].(string)
		// Only surface jobs that have rows to review
		if matchStatus != "review_required" && matchStatus != "analyzing" {
			continue
		}

		jobID := jobDoc.Ref.ID

		rowsIter := h.fsClient.Collection("tenants").Doc(tenantID).
			Collection("import_jobs").Doc(jobID).
			Collection("match_results").
			Where("decision", "==", "").
			Documents(ctx)

		for {
			rowDoc, err := rowsIter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				break
			}
			var row MatchResultRow
			if err := rowDoc.DataTo(&row); err != nil {
				continue
			}
			result.Total++
			switch row.MatchType {
			case "exact":
				result.Exact = append(result.Exact, row)
			case "fuzzy":
				result.Fuzzy = append(result.Fuzzy, row)
			default:
				result.Unmatched = append(result.Unmatched, row)
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"results": result,
		"total":   result.Total,
	})
}

func (h *ImportMatchHandler) countPendingReviewRows(ctx context.Context, tenantID string) (int, error) {
	jobsIter := h.fsClient.Collection("tenants").Doc(tenantID).
		Collection("import_jobs").
		Where("pending_review", "==", true).
		Documents(ctx)
	defer jobsIter.Stop()

	total := 0
	for {
		jobDoc, err := jobsIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return 0, err
		}

		jobData := jobDoc.Data()
		matchStatus, _ := jobData["match_status"].(string)
		if matchStatus != "review_required" && matchStatus != "analyzing" {
			continue
		}

		jobID := jobDoc.Ref.ID
		rowsIter := h.fsClient.Collection("tenants").Doc(tenantID).
			Collection("import_jobs").Doc(jobID).
			Collection("match_results").
			Where("decision", "==", "").
			Documents(ctx)

		for {
			_, err := rowsIter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				break
			}
			total++
		}
	}
	return total, nil
}
