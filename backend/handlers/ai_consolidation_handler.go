package handlers

// ============================================================================
// AI CONSOLIDATION HANDLER
// ============================================================================
//
// Routes:
//   POST /api/v1/ai/consolidate/product        — Single product (synchronous)
//   POST /api/v1/ai/consolidate/bulk           — Queue bulk consolidation
//   GET  /api/v1/ai/consolidate/jobs           — List consolidation jobs
//   GET  /api/v1/ai/consolidate/jobs/:id       — Job detail
//   POST /api/v1/ai/listings/generate          — Generate listings from consolidated data
//   POST /api/v1/ai/listings/auto-draft        — Run full pipeline (consolidate + generate)
//   GET  /api/v1/ai/consolidate/settings       — Get AI settings
//   PUT  /api/v1/ai/consolidate/settings       — Update AI settings
//   GET  /api/v1/ai/consolidate/product/:id    — Get consolidated record for a product
// ============================================================================

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"

	"module-a/models"
	"module-a/repository"
	"module-a/services"
)

type AIConsolidationHandler struct {
	consolidationSvc *services.AIConsolidationService
	listingGenSvc    *services.AIListingGenerationService
	mpRepo           *repository.MarketplaceRepository
	productSvc       *services.ProductService
	fsClient         *firestore.Client
	searchService    *services.SearchService
}

func NewAIConsolidationHandler(
	consolidationSvc *services.AIConsolidationService,
	listingGenSvc *services.AIListingGenerationService,
	mpRepo *repository.MarketplaceRepository,
	productSvc *services.ProductService,
	fsClient *firestore.Client,
	searchService *services.SearchService,
) *AIConsolidationHandler {
	return &AIConsolidationHandler{
		consolidationSvc: consolidationSvc,
		listingGenSvc:    listingGenSvc,
		mpRepo:           mpRepo,
		productSvc:       productSvc,
		fsClient:         fsClient,
		searchService:    searchService,
	}
}

// ─── POST /api/v1/ai/consolidate/product ─────────────────────────────────────

func (h *AIConsolidationHandler) ConsolidateProduct(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req struct {
		ProductID           string   `json:"product_id" binding:"required"`
		UseImageComparison  bool     `json:"use_image_comparison"`
		WriteBackToPIM      *bool    `json:"write_back_to_pim"`
		ConfidenceThreshold *float64 `json:"confidence_threshold"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Load tenant settings, then apply request overrides
	tenantSettings, _ := h.loadAISettings(c.Request.Context(), tenantID)
	opts := services.DefaultConsolidationOptions()
	opts.ConsolidationModel  = tenantSettings.ConsolidationModel
	opts.AutoEscalate        = tenantSettings.AutoEscalate
	opts.ConfidenceThreshold = tenantSettings.ConfidenceThreshold
	opts.UseImageComparison  = req.UseImageComparison
	opts.SkipIfConsolidated  = false // explicit call always re-runs
	if req.WriteBackToPIM != nil {
		opts.WriteBackToPIM = *req.WriteBackToPIM
	}
	if req.ConfidenceThreshold != nil {
		opts.ConfidenceThreshold = *req.ConfidenceThreshold
	}

	result, err := h.consolidationSvc.ConsolidateProduct(
		c.Request.Context(), tenantID, req.ProductID, opts,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if result.Meta.ReviewRequired {
		h.markListingsForReview(c.Request.Context(), tenantID, req.ProductID, result.Meta.ReviewReasons)
	}

	c.JSON(http.StatusOK, gin.H{
		"product_id":         result.ProductID,
		"review_required":    result.ReviewRequired,
		"pim_fields_set":     result.PIMLfieldsSet,
		"overall_confidence": result.Meta.Signals.Overall,
		"discarded_branches": result.Meta.DiscardedBranches,
		"flagged_branches":   result.Meta.FlaggedBranches,
		"conflicts":          len(result.Meta.Conflicts),
		"review_reasons":     result.Meta.ReviewReasons,
		"duration_ms":        result.Meta.DurationMS,
	})
}

// ─── POST /api/v1/ai/consolidate/bulk ────────────────────────────────────────

func (h *AIConsolidationHandler) BulkConsolidate(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req struct {
		ProductIDs          []string `json:"product_ids"`
		UseImageComparison  bool     `json:"use_image_comparison"`
		WriteBackToPIM      *bool    `json:"write_back_to_pim"`
		ConfidenceThreshold *float64 `json:"confidence_threshold"`
		Force               bool     `json:"force"`
	}
	c.ShouldBindJSON(&req)

	tenantSettings, _ := h.loadAISettings(c.Request.Context(), tenantID)
	opts := services.DefaultConsolidationOptions()
	opts.ConsolidationModel  = tenantSettings.ConsolidationModel
	opts.AutoEscalate        = tenantSettings.AutoEscalate
	opts.ConfidenceThreshold = tenantSettings.ConfidenceThreshold
	opts.UseImageComparison  = req.UseImageComparison
	opts.SkipIfConsolidated  = !req.Force
	if req.WriteBackToPIM != nil {
		opts.WriteBackToPIM = *req.WriteBackToPIM
	}
	if req.ConfidenceThreshold != nil {
		opts.ConfidenceThreshold = *req.ConfidenceThreshold
	}

	jobID := "consolidate_" + uuid.New().String()[:8]
	now := time.Now()

	h.fsClient.Collection("tenants").Doc(tenantID).
		Collection("consolidation_jobs").Doc(jobID).
		Set(c.Request.Context(), map[string]interface{}{
			"job_id":         jobID,
			"tenant_id":      tenantID,
			"status":         "running",
			"status_message": "Starting consolidation…",
			"total":          0,
			"processed":      0,
			"succeeded":      0,
			"failed":         0,
			"created_at":     now,
			"updated_at":     now,
			"started_at":     now,
		})

	go func() {
		ctx := context.Background()
		progressFn := func(processed, succeeded, failed int, msg string) {
			status := "running"
			updates := map[string]interface{}{
				"processed":      processed,
				"succeeded":      succeeded,
				"failed":         failed,
				"status_message": msg,
				"updated_at":     time.Now(),
			}
			if msg == "completed" || (len(msg) > 7 && msg[:7] == "failed:") {
				if failed > 0 && succeeded == 0 {
					status = "failed"
				} else {
					status = "completed"
				}
				updates["completed_at"] = time.Now()
			}
			updates["status"] = status
			h.fsClient.Collection("tenants").Doc(tenantID).
				Collection("consolidation_jobs").Doc(jobID).
				Set(ctx, updates, firestore.MergeAll)
		}
		h.consolidationSvc.BulkConsolidate(ctx, tenantID, opts, jobID, progressFn)
	}()

	c.JSON(http.StatusAccepted, gin.H{
		"job_id":  jobID,
		"message": "Consolidation job started",
	})
}

// ─── GET /api/v1/ai/consolidate/jobs ─────────────────────────────────────────

func (h *AIConsolidationHandler) ListJobs(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	iter := h.fsClient.Collection("tenants").Doc(tenantID).
		Collection("consolidation_jobs").
		OrderBy("created_at", firestore.Desc).
		Limit(20).
		Documents(c.Request.Context())
	defer iter.Stop()

	var jobs []map[string]interface{}
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		jobs = append(jobs, doc.Data())
	}
	if jobs == nil {
		jobs = []map[string]interface{}{}
	}

	c.JSON(http.StatusOK, gin.H{"jobs": jobs})
}

// ─── GET /api/v1/ai/consolidate/jobs/:id ─────────────────────────────────────

func (h *AIConsolidationHandler) GetJob(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	jobID := c.Param("id")

	doc, err := h.fsClient.Collection("tenants").Doc(tenantID).
		Collection("consolidation_jobs").Doc(jobID).
		Get(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"job": doc.Data()})
}

// ─── GET /api/v1/ai/consolidate/product/:product_id ──────────────────────────

func (h *AIConsolidationHandler) GetConsolidated(c *gin.Context) {
	tenantID  := c.GetString("tenant_id")
	productID := c.Param("product_id")
	ctx := c.Request.Context()

	// Direct subcollection read — product_id is known so no query needed
	consolidatedDoc, err := h.fsClient.Collection("tenants").Doc(tenantID).
		Collection("products").Doc(productID).
		Collection("extended_data").Doc("consolidated").
		Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no consolidated record found"})
		return
	}

	response := gin.H{
		"product_id":   productID,
		"consolidated": consolidatedDoc.Data(),
	}

	// Direct subcollection read for meta doc
	if metaDoc, err := h.fsClient.Collection("tenants").Doc(tenantID).
		Collection("products").Doc(productID).
		Collection("extended_data").Doc("consolidated_meta").
		Get(ctx); err == nil {
		response["meta"] = metaDoc.Data()
	}

	c.JSON(http.StatusOK, response)
}

// ─── POST /api/v1/ai/listings/generate ───────────────────────────────────────

func (h *AIConsolidationHandler) GenerateListings(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req struct {
		ProductID        string   `json:"product_id" binding:"required"`
		Channels         []string `json:"channels"`
		ChannelAccountID string   `json:"channel_account_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if len(req.Channels) == 0 {
		settings, _ := h.loadAISettings(c.Request.Context(), tenantID)
		if len(settings.Channels) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "No channels specified and none configured in AI Settings"})
			return
		}
		req.Channels = settings.Channels
	}

	result, err := h.generateListingsForProduct(c.Request.Context(), tenantID, req.ProductID, req.Channels)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	created := 0
	var listingIDs []string
	for _, l := range result.Listings {
		listing := h.listingFromAIOutput(tenantID, req.ProductID, req.ChannelAccountID, l)
		if err := h.mpRepo.SaveListing(c.Request.Context(), listing); err != nil {
			log.Printf("[GenerateListings] Failed to save listing %s/%s: %v", req.ProductID, l.Channel, err)
		} else {
			created++
			listingIDs = append(listingIDs, listing.ListingID)
			// Index to Typesense immediately so AI-generated listings are searchable.
			if h.searchService != nil {
				if indexErr := h.searchService.IndexListing(listing); indexErr != nil {
					log.Printf("[GenerateListings] Typesense index failed for listing %s: %v", listing.ListingID, indexErr)
				}
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"product_id":       req.ProductID,
		"listings_created": created,
		"listing_ids":      listingIDs,
		"duration_ms":      result.DurationMS,
		"split_calls":      result.SplitCalls,
	})
}

// ─── POST /api/v1/ai/listings/auto-draft ─────────────────────────────────────
// Full pipeline: consolidate → generate listings → save as ai_draft

func (h *AIConsolidationHandler) AutoDraft(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req struct {
		ProductID           string   `json:"product_id" binding:"required"`
		Channels            []string `json:"channels"`
		ChannelAccountID    string   `json:"channel_account_id"`
		UseImageComparison  bool     `json:"use_image_comparison"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tenantSettings, _ := h.loadAISettings(c.Request.Context(), tenantID)

	channels := req.Channels
	if len(channels) == 0 {
		if !tenantSettings.Enabled || len(tenantSettings.Channels) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "No channels specified and auto-draft not configured in Settings → AI"})
			return
		}
		channels = tenantSettings.Channels
	}

	// Phase 1: Consolidate
	opts := services.DefaultConsolidationOptions()
	opts.ConsolidationModel  = tenantSettings.ConsolidationModel
	opts.AutoEscalate        = tenantSettings.AutoEscalate
	opts.ConfidenceThreshold = tenantSettings.ConfidenceThreshold
	opts.UseImageComparison  = req.UseImageComparison || tenantSettings.UseImageComparison
	opts.SkipIfConsolidated  = false

	consolidateResult, err := h.consolidationSvc.ConsolidateProduct(
		c.Request.Context(), tenantID, req.ProductID, opts,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Consolidation failed: " + err.Error()})
		return
	}

	if consolidateResult.Meta.ReviewRequired {
		h.markListingsForReview(c.Request.Context(), tenantID, req.ProductID, consolidateResult.Meta.ReviewReasons)
	}

	// Phase 2: Generate listings
	genResult, err := h.generateListingsForProduct(c.Request.Context(), tenantID, req.ProductID, channels)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":         "Listing generation failed: " + err.Error(),
			"consolidation": consolidateResult.Meta,
		})
		return
	}

	// Phase 3: Save drafts
	created := 0
	var listingIDs []string
	for _, l := range genResult.Listings {
		listing := h.listingFromAIOutput(tenantID, req.ProductID, req.ChannelAccountID, l)
		if consolidateResult.Meta.ReviewRequired {
			listing.State = "ai_draft_review"
		}
		if err := h.mpRepo.SaveListing(c.Request.Context(), listing); err != nil {
			log.Printf("[AutoDraft] Failed to save listing %s/%s: %v", req.ProductID, l.Channel, err)
		} else {
			created++
			listingIDs = append(listingIDs, listing.ListingID)
			// Index to Typesense immediately so AI-generated listings are searchable.
			if h.searchService != nil {
				if indexErr := h.searchService.IndexListing(listing); indexErr != nil {
					log.Printf("[AutoDraft] Typesense index failed for listing %s: %v", listing.ListingID, indexErr)
				}
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"product_id":         req.ProductID,
		"overall_confidence": consolidateResult.Meta.Signals.Overall,
		"review_required":    consolidateResult.Meta.ReviewRequired,
		"listings_created":   created,
		"listing_ids":        listingIDs,
	})
}

// ─── GET /api/v1/ai/consolidate/settings ─────────────────────────────────────

func (h *AIConsolidationHandler) GetAISettings(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	settings, err := h.loadAISettings(c.Request.Context(), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, settings)
}

// ─── PUT /api/v1/ai/consolidate/settings ─────────────────────────────────────

func (h *AIConsolidationHandler) UpdateAISettings(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var settings services.AutoDraftSettings
	if err := c.ShouldBindJSON(&settings); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if settings.ConfidenceThreshold <= 0 {
		settings.ConfidenceThreshold = 0.70
	}
	if settings.ConsolidationModel == "" {
		settings.ConsolidationModel = "gemini-2.0-flash"
	}

	_, err := h.fsClient.Collection("tenants").Doc(tenantID).
		Collection("config").Doc("settings").
		Set(c.Request.Context(), map[string]interface{}{
			"ai": map[string]interface{}{
				"auto_draft_enabled":   settings.Enabled,
				"auto_draft_channels":  settings.Channels,
				"confidence_threshold": settings.ConfidenceThreshold,
				"use_image_comparison": settings.UseImageComparison,
				"consolidation_model":  settings.ConsolidationModel,
				"auto_escalate":        settings.AutoEscalate,
				"updated_at":           time.Now(),
			},
		}, firestore.MergeAll)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "settings": settings})
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func (h *AIConsolidationHandler) loadAISettings(ctx context.Context, tenantID string) (services.AutoDraftSettings, error) {
	defaults := services.DefaultAutoSettings()
	doc, err := h.fsClient.Collection("tenants").Doc(tenantID).
		Collection("config").Doc("settings").Get(ctx)
	if err != nil {
		return defaults, nil
	}
	data := doc.Data()
	aiRaw, ok := data["ai"].(map[string]interface{})
	if !ok {
		return defaults, nil
	}

	settings := defaults
	if v, ok := aiRaw["auto_draft_enabled"].(bool); ok {
		settings.Enabled = v
	}
	if v, ok := aiRaw["confidence_threshold"].(float64); ok && v > 0 {
		settings.ConfidenceThreshold = v
	}
	if v, ok := aiRaw["use_image_comparison"].(bool); ok {
		settings.UseImageComparison = v
	}
	if v, ok := aiRaw["consolidation_model"].(string); ok && v != "" {
		settings.ConsolidationModel = v
	}
	if v, ok := aiRaw["auto_escalate"].(bool); ok {
		settings.AutoEscalate = v
	}
	if v, ok := aiRaw["auto_draft_channels"].([]interface{}); ok {
		for _, ch := range v {
			if s, ok := ch.(string); ok {
				settings.Channels = append(settings.Channels, s)
			}
		}
	}
	return settings, nil
}

func (h *AIConsolidationHandler) generateListingsForProduct(
	ctx context.Context,
	tenantID, productID string,
	channels []string,
) (*services.MultiChannelListingResult, error) {

	product, err := h.productSvc.GetProduct(ctx, tenantID, productID)
	if err != nil {
		return nil, fmt.Errorf("load product: %w", err)
	}

	consolidated, _ := h.mpRepo.GetExtendedData(ctx, tenantID, productID, "consolidated")
	var consolidatedData map[string]interface{}
	if consolidated != nil {
		consolidatedData = consolidated.Data
	}

	baseInput := h.productToAIInput(product)

	req := services.ChannelGenerationRequest{
		TenantID:         tenantID,
		ProductID:        productID,
		Channels:         channels,
		ConsolidatedData: consolidatedData,
		BaseProduct:      baseInput,
	}

	return h.listingGenSvc.GenerateForAllChannels(ctx, req)
}

func (h *AIConsolidationHandler) productToAIInput(product *models.Product) services.AIProductInput {
	input := services.AIProductInput{
		Title:       product.Title,
		SKU:         product.SKU,
		KeyFeatures: product.KeyFeatures,
		Tags:        product.Tags,
		Attributes:  product.Attributes,
	}
	if product.Description != nil {
		input.Description = *product.Description
	}
	if product.Brand != nil {
		input.Brand = *product.Brand
	}
	if product.Identifiers != nil {
		input.Identifiers = make(map[string]string)
		if product.Identifiers.EAN != nil  { input.Identifiers["ean"] = *product.Identifiers.EAN }
		if product.Identifiers.ASIN != nil { input.Identifiers["asin"] = *product.Identifiers.ASIN }
		if product.Identifiers.UPC != nil  { input.Identifiers["upc"] = *product.Identifiers.UPC }
		if product.Identifiers.GTIN != nil { input.Identifiers["gtin"] = *product.Identifiers.GTIN }
		if product.Identifiers.MPN != nil  { input.Identifiers["mpn"] = *product.Identifiers.MPN }
	}
	for _, asset := range product.Assets {
		if asset.URL != "" {
			input.ImageURLs = append(input.ImageURLs, asset.URL)
		}
	}
	if product.Dimensions != nil {
		input.Dimensions = map[string]interface{}{
			"length": product.Dimensions.Length,
			"width":  product.Dimensions.Width,
			"height": product.Dimensions.Height,
			"unit":   product.Dimensions.Unit,
		}
	}
	if product.Weight != nil {
		input.Weight = map[string]interface{}{
			"value": product.Weight.Value,
			"unit":  product.Weight.Unit,
		}
	}
	return input
}

func (h *AIConsolidationHandler) listingFromAIOutput(
	tenantID, productID, channelAccountID string,
	l services.AIListingOutput,
) *models.Listing {
	now := time.Now()
	attrs := l.Attributes
	if attrs == nil {
		attrs = map[string]interface{}{}
	}
	// Store AI metadata in attributes for UI display
	attrs["_ai_confidence"] = l.Confidence
	attrs["_ai_warnings"]   = l.Warnings
	attrs["_bullet_points"] = l.BulletPoints
	attrs["_category_name"] = l.CategoryName
	attrs["_condition"]     = l.Condition

	listing := &models.Listing{
		ListingID:        uuid.New().String(),
		TenantID:         tenantID,
		ProductID:        productID,
		Channel:          l.Channel,
		ChannelAccountID: channelAccountID,
		State:            "ai_draft",
		Overrides: &models.ListingOverrides{
			Title:       l.Title,
			Description: l.Description,
			Attributes:  attrs,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	return listing
}

func (h *AIConsolidationHandler) markListingsForReview(
	ctx context.Context,
	tenantID, productID string,
	reasons []string,
) {
	iter := h.fsClient.Collection("tenants").Doc(tenantID).
		Collection("listings").
		Where("product_id", "==", productID).
		Where("state", "==", "ai_draft").
		Documents(ctx)
	defer iter.Stop()

	for {
		doc, err := iter.Next()
		if err != nil {
			break
		}
		doc.Ref.Update(ctx, []firestore.Update{
			{Path: "state", Value: "ai_draft_review"},
			{Path: "review_reasons", Value: reasons},
			{Path: "updated_at", Value: time.Now()},
		})
	}
}

func (h *AIConsolidationHandler) findProductsToConsolidate(
	ctx context.Context, tenantID string, productIDs []string, force bool,
) ([]string, error) {
	if len(productIDs) > 0 {
		return productIDs, nil
	}

	// CollectionGroup query across all products/{id}/extended_data subcollections.
	// Finds all enriched docs across the tenant. Requires Firestore index:
	// (collection_group=extended_data, tenant_id ASC, source ASC)
	iter := h.fsClient.CollectionGroup("extended_data").
		Where("tenant_id", "==", tenantID).
		Where("source", "in", []string{"ebay_browse", "ebay_browse_ean", "ebay_browse_epid", "amazon_catalog"}).
		Documents(ctx)
	defer iter.Stop()

	seen := make(map[string]bool)
	var ids []string
	for {
		doc, err := iter.Next()
		if err != nil {
			break
		}
		var ext models.ExtendedProductData
		if err := doc.DataTo(&ext); err != nil {
			continue
		}
		if ext.ProductID == "" || seen[ext.ProductID] {
			continue
		}
		seen[ext.ProductID] = true

		if !force {
			_, err := h.mpRepo.GetExtendedData(ctx, tenantID, ext.ProductID, "consolidated")
			if err == nil {
				continue
			}
		}
		ids = append(ids, ext.ProductID)
	}
	return ids, nil
}
