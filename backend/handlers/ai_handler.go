package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	cloudtasks "cloud.google.com/go/cloudtasks/apiv2"
	taskspb "cloud.google.com/go/cloudtasks/apiv2/cloudtaskspb"
	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"
	"module-a/adapters/keyword"
	"module-a/instrumentation"
	"module-a/models"
	"module-a/repository"
	"module-a/services"
)

// ============================================================================
// AI LISTING GENERATION HANDLER
// ============================================================================
// Endpoints:
//
//	POST /ai/generate           — Generate listings for one product (sync)
//	POST /ai/generate/bulk      — Queue bulk generation job (async)
//	GET  /ai/generate/jobs      — List generation jobs
//	GET  /ai/generate/jobs/:id  — Get generation job status + results
//	POST /ai/generate/apply     — Apply AI-generated content to actual listings
//	GET  /ai/status             — Check AI provider availability
//
// Session 8: keyword context is now fetched and injected into both
// GenerateSingle and GenerateWithSchema. GenerateSingle routes through
// listingGenSvc.GenerateForAllChannels (same pattern as
// ai_consolidation_handler.go). GenerateWithSchema injects the keyword block
// into product.EnrichedData so the existing ai_service.go signature is
// unchanged.
//
// Session 9: credit gate added to GenerateSingle (1 credit × channels) and
// GenerateWithSchema (1 credit). Gate fires before any AI call. Usage events
// written to Firestore via logAIUsage — requires SetFirestoreClient and
// SetUsageService to be called from main.go after construction:
//
//	aiHandler.SetUsageService(usageService)
//	aiHandler.SetFirestoreClient(firestoreRepo.GetClient())
//
// ============================================================================

type AIHandler struct {
	aiService       *services.AIService
	productRepo     *repository.FirestoreRepository
	mpRepo          *repository.MarketplaceRepository
	productSvc      *services.ProductService
	listingSvc      *services.ListingService
	usage           *UsageInstrumentor
	kwSvc           *services.KeywordIntelligenceService // Session 8
	listingGenSvc   *services.AIListingGenerationService // Session 8
	usageService    *services.UsageService               // Session 9 — credit gating
	firestoreClient *firestore.Client                    // Session 9 — usage event logging
}

func NewAIHandler(
	aiService *services.AIService,
	productRepo *repository.FirestoreRepository,
	mpRepo *repository.MarketplaceRepository,
	productSvc *services.ProductService,
	listingSvc *services.ListingService,
) *AIHandler {
	return &AIHandler{
		aiService:   aiService,
		productRepo: productRepo,
		mpRepo:      mpRepo,
		productSvc:  productSvc,
		listingSvc:  listingSvc,
		usage:       NewUsageInstrumentor(nil),
	}
}

// SetKeywordService wires in the keyword intelligence service after construction.
func (h *AIHandler) SetKeywordService(svc *services.KeywordIntelligenceService) {
	h.kwSvc = svc
}

// SetListingGenService wires in the listing generation service after construction.
func (h *AIHandler) SetListingGenService(svc *services.AIListingGenerationService) {
	h.listingGenSvc = svc
}

// SetUsageService wires in the usage service for credit gating (Session 9).
// Call from main.go: aiHandler.SetUsageService(usageService)
func (h *AIHandler) SetUsageService(svc *services.UsageService) {
	h.usageService = svc
}

// SetFirestoreClient wires in the Firestore client for usage event logging
// (Session 9). Call from main.go: aiHandler.SetFirestoreClient(firestoreRepo.GetClient())
func (h *AIHandler) SetFirestoreClient(client *firestore.Client) {
	h.firestoreClient = client
}

// checkAndDeductCredits returns (balance, ok).
// If ok is false the caller must return HTTP 402 immediately and not proceed.
// When usageService is nil the gate is bypassed — preserves backward
// compatibility in environments where credit tracking is not yet wired.
func (h *AIHandler) checkAndDeductCredits(
	ctx context.Context,
	tenantID string,
	cost float64,
	eventType string,
	metadata map[string]string,
) (float64, bool) {
	if h.usageService == nil {
		return 0, true
	}
	ledger, err := h.usageService.GetCurrentLedger(ctx, tenantID)
	if err != nil || ledger == nil {
		return 0, true
	}
	var balance float64
	if ledger.CreditsRemaining != nil {
		balance = *ledger.CreditsRemaining
	} else {
		return 0, true // nil == unlimited plan
	}
	if balance < cost {
		return balance, false
	}
	return balance, true
}

// logAIUsage writes a usage event to Firestore. Fire-and-forget — a nil
// firestoreClient is silently skipped so this never blocks the request path.
func (h *AIHandler) logAIUsage(ctx context.Context, event instrumentation.UsageEvent) {
	if h.firestoreClient == nil {
		return
	}
	_ = instrumentation.LogUsageEvent(ctx, h.firestoreClient, event)
}

// ============================================================================
// STATUS ENDPOINT
// ============================================================================

func (h *AIHandler) Status(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"available":  h.aiService.IsAvailable(),
		"has_gemini": h.aiService.HasGemini(),
		"has_claude": h.aiService.HasClaude(),
		"mode":       getAIMode(h.aiService),
	})
}

func getAIMode(svc *services.AIService) string {
	if svc.HasGemini() && svc.HasClaude() {
		return "hybrid"
	}
	if svc.HasClaude() {
		return "claude_only"
	}
	if svc.HasGemini() {
		return "gemini_only"
	}
	return "unavailable"
}

// ============================================================================
// SINGLE PRODUCT GENERATION (SYNCHRONOUS)
// ============================================================================

type GenerateRequest struct {
	ProductID string   `json:"product_id" binding:"required"`
	Channels  []string `json:"channels" binding:"required"`
	Mode      string   `json:"mode,omitempty"`
}

// POST /ai/generate
// Session 9: credit gate — 1 credit per channel, checked before any AI call.
func (h *AIHandler) GenerateSingle(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req GenerateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if !h.aiService.IsAvailable() {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "AI service not configured. Set GEMINI_API_KEY and/or CLAUDE_API_KEY.",
		})
		return
	}

	input, err := h.buildAIInput(c.Request.Context(), tenantID, req.ProductID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Product not found or load failed: %v", err)})
		return
	}

	// Session 9 — credit gate: 1 credit per channel, fires before any AI work.
	creditCost := float64(len(req.Channels))
	if balance, ok := h.checkAndDeductCredits(
		c.Request.Context(), tenantID,
		creditCost,
		instrumentation.EVTYPE_AI_LISTING_OPTIMISE,
		map[string]string{"product_id": req.ProductID},
	); !ok {
		c.JSON(http.StatusPaymentRequired, gin.H{
			"error":    "insufficient_credits",
			"balance":  balance,
			"required": creditCost,
		})
		return
	}

	h.logAIUsage(c.Request.Context(), instrumentation.UsageEvent{
		TenantID:   tenantID,
		EventType:  instrumentation.EVTYPE_AI_LISTING_OPTIMISE,
		ProductID:  req.ProductID,
		CreditCost: creditCost,
		DataSource: "anthropic",
		Metadata:   map[string]string{"product_id": req.ProductID, "channels": strings.Join(req.Channels, ",")},
		Timestamp:  time.Now(),
	})

	// Session 8: keyword-aware path via listingGenSvc
	if h.listingGenSvc != nil {
		asin := ""
		if input.Identifiers != nil {
			asin = input.Identifiers["asin"]
		}

		var allListings []services.AIListingOutput
		for _, ch := range req.Channels {
			kwCtx := h.buildKeywordContext(c.Request.Context(), req.ProductID, asin, ch)
			channelReq := services.ChannelGenerationRequest{
				TenantID:    tenantID,
				ProductID:   req.ProductID,
				ASIN:        asin,
				Channels:    []string{ch},
				BaseProduct: *input,
			}
			chResult, chErr := h.listingGenSvc.GenerateForAllChannels(c.Request.Context(), channelReq, kwCtx)
			if chErr != nil {
				log.Printf("[AI] channel %s generation failed: %v", ch, chErr)
				allListings = append(allListings, services.AIListingOutput{
					Channel:  ch,
					Warnings: []string{fmt.Sprintf("Generation failed: %v", chErr)},
				})
				continue
			}
			if chResult != nil {
				allListings = append(allListings, chResult.Listings...)
			}
		}

		result := &services.AIGenerationResult{
			ProductID: req.ProductID,
			Listings:  allListings,
		}
		c.JSON(http.StatusOK, gin.H{
			"data":    result,
			"message": fmt.Sprintf("Generated %d listings", len(result.Listings)),
		})
		return
	}

	// Legacy path: aiService directly, no keyword context
	mode := req.Mode
	if mode == "" {
		mode = "hybrid"
	}

	var result *services.AIGenerationResult
	switch mode {
	case "fast":
		result, err = h.aiService.GenerateListingsSinglePhase(c.Request.Context(), *input, req.Channels)
	case "quality":
		result, err = h.aiService.GenerateListingsSinglePhase(c.Request.Context(), *input, req.Channels)
	default:
		result, err = h.aiService.GenerateListings(c.Request.Context(), *input, req.Channels)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Generation failed", "details": err.Error()})
		return
	}

	result.ProductID = req.ProductID
	c.JSON(http.StatusOK, gin.H{
		"data":    result,
		"message": fmt.Sprintf("Generated %d listings in %dms", len(result.Listings), result.DurationMS),
	})
}

// ============================================================================
// SCHEMA-AWARE GENERATION (SINGLE CHANNEL + SCHEMA)
// ============================================================================

type GenerateWithSchemaRequest struct {
	ProductID    string                            `json:"product_id" binding:"required"`
	Channel      string                            `json:"channel" binding:"required"`
	CategoryID   string                            `json:"category_id"`
	CategoryName string                            `json:"category_name"`
	CategoryPath []string                          `json:"category_path,omitempty"`
	Fields       []services.MarketplaceSchemaField `json:"fields"`
}

// POST /ai/generate-with-schema
// Session 9: credit gate — 1 credit per schema generation call.
func (h *AIHandler) GenerateWithSchema(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req GenerateWithSchemaRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if !h.aiService.IsAvailable() {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "AI service not configured. Set GEMINI_API_KEY and/or CLAUDE_API_KEY.",
		})
		return
	}

	input, err := h.buildAIInput(c.Request.Context(), tenantID, req.ProductID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Product not found: %v", err)})
		return
	}

	// Session 9 — credit gate: 1 credit.
	if balance, ok := h.checkAndDeductCredits(
		c.Request.Context(), tenantID,
		1.0,
		instrumentation.EVTYPE_AI_LISTING_OPTIMISE,
		map[string]string{"product_id": req.ProductID, "channel": req.Channel},
	); !ok {
		c.JSON(http.StatusPaymentRequired, gin.H{
			"error":    "insufficient_credits",
			"balance":  balance,
			"required": 1.0,
		})
		return
	}

	h.logAIUsage(c.Request.Context(), instrumentation.UsageEvent{
		TenantID:   tenantID,
		EventType:  instrumentation.EVTYPE_AI_LISTING_OPTIMISE,
		ProductID:  req.ProductID,
		CreditCost: 1.0,
		DataSource: "anthropic",
		Metadata:   map[string]string{"product_id": req.ProductID, "channel": req.Channel},
		Timestamp:  time.Now(),
	})

	// Session 8: inject keyword context into enriched_data.
	asin := ""
	if input.Identifiers != nil {
		asin = input.Identifiers["asin"]
	}
	kwCtx := h.buildKeywordContext(c.Request.Context(), req.ProductID, asin, req.Channel)
	if kwCtx != nil && len(kwCtx.Keywords) > 0 {
		if input.EnrichedData == nil {
			input.EnrichedData = map[string]interface{}{}
		}
		var kwLines []string
		kwLines = append(kwLines, "KEYWORD PLACEMENT INSTRUCTIONS:")
		if kwCtx.TitleMaxChars > 0 {
			kwLines = append(kwLines, fmt.Sprintf("Title character limit: %d", kwCtx.TitleMaxChars))
		}
		if kwCtx.TitleTemplate != "" {
			kwLines = append(kwLines, "Title must follow this structure: "+kwCtx.TitleTemplate)
		}
		kwLines = append(kwLines, "Keywords ranked by commercial importance (integrate naturally, do not list mechanically):")
		for i, kw := range kwCtx.Keywords {
			kwLines = append(kwLines, fmt.Sprintf("  %d. %s", i+1, kw))
		}
		kwLines = append(kwLines, "Placement rules:")
		kwLines = append(kwLines, "  - Keywords 1-2 MUST appear in the first 80 characters of the title")
		kwLines = append(kwLines, "  - Keywords 3-5 should appear in the first two bullet points or key features")
		kwLines = append(kwLines, "  - Keywords 6-10 should be distributed across remaining bullets and description opening")
		if kwCtx.BackendKeywords != "" {
			kwLines = append(kwLines, "Backend/search terms field: populate with: "+kwCtx.BackendKeywords)
		}
		if len(kwCtx.Tags) > 0 {
			kwLines = append(kwLines, "Tags: generate exactly these tags: "+strings.Join(kwCtx.Tags, ", "))
		}
		if len(kwCtx.ItemSpecificsSuggestions) > 0 {
			kwLines = append(kwLines, "Item specifics suggestions: "+strings.Join(kwCtx.ItemSpecificsSuggestions, ", "))
		}
		input.EnrichedData["keyword_context_hint"] = strings.Join(kwLines, "\n")
	}

	schema := services.MarketplaceSchemaInput{
		Channel:      req.Channel,
		CategoryID:   req.CategoryID,
		CategoryName: req.CategoryName,
		CategoryPath: req.CategoryPath,
		Fields:       req.Fields,
	}

	log.Printf("[AI] Schema-aware generation: product=%s channel=%s category=%s fields=%d keyword_ctx=%v",
		req.ProductID, req.Channel, req.CategoryName, len(req.Fields), kwCtx != nil)

	result, err := h.aiService.GenerateWithSchema(c.Request.Context(), *input, schema)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Generation failed", "details": err.Error()})
		return
	}

	result.ProductID = req.ProductID
	c.JSON(http.StatusOK, gin.H{
		"data":    result,
		"message": fmt.Sprintf("Generated listing with %d schema fields in %dms", len(req.Fields), result.DurationMS),
	})
}

// ============================================================================
// KEYWORD CONTEXT HELPER (Session 8)
// ============================================================================

func (h *AIHandler) buildKeywordContext(
	ctx context.Context,
	productID string,
	asin string,
	channel string,
) *services.KeywordContext {
	if h.kwSvc == nil {
		return nil
	}
	cacheKey := productID
	if asin != "" {
		cacheKey = asin
	}
	productInfo := services.ProductInfo{ASIN: asin}
	ks, err := h.kwSvc.GetOrCreateKeywordSet(ctx, cacheKey, productInfo)
	if err != nil {
		log.Printf("[AI] keyword set unavailable for %s: %v — continuing without keyword context", cacheKey, err)
		return nil
	}
	return keyword.Get(channel).Transform(ks)
}

// ============================================================================
// BULK GENERATION (ASYNC VIA CLOUD TASKS)
// ============================================================================

type BulkGenerateRequest struct {
	ProductIDs       []string `json:"product_ids" binding:"required"`
	Channels         []string `json:"channels" binding:"required"`
	ChannelAccountID string   `json:"channel_account_id" binding:"required"`
	Mode             string   `json:"mode,omitempty"`
	AutoApply        bool     `json:"auto_apply,omitempty"`
}

// POST /ai/generate/bulk
func (h *AIHandler) GenerateBulk(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req BulkGenerateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if !h.aiService.IsAvailable() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "AI service not configured"})
		return
	}
	if len(req.ProductIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No product IDs provided"})
		return
	}
	if len(req.ProductIDs) > 500 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Maximum 500 products per bulk generation"})
		return
	}

	job := &models.AIGenerationJob{
		JobID:            uuid.New().String(),
		TenantID:         tenantID,
		Status:           "pending",
		ProductIDs:       req.ProductIDs,
		Channels:         req.Channels,
		ChannelAccountID: req.ChannelAccountID,
		Mode:             req.Mode,
		AutoApply:        req.AutoApply,
		TotalProducts:    len(req.ProductIDs),
		ProcessedCount:   0,
		SuccessCount:     0,
		FailedCount:      0,
		Results:          []models.AIGenerationJobResult{},
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}
	if job.Mode == "" {
		job.Mode = "hybrid"
	}

	if err := h.mpRepo.SaveAIGenerationJob(c.Request.Context(), job); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create job"})
		return
	}

	if len(req.ProductIDs) <= 10 {
		go h.processGenerationJob(context.Background(), job)
	} else {
		go h.queueGenerationTasks(context.Background(), job)
	}

	c.JSON(http.StatusAccepted, gin.H{
		"data":    job,
		"message": fmt.Sprintf("Generation job queued for %d products across %d channels", len(req.ProductIDs), len(req.Channels)),
	})
}

// GET /ai/generate/jobs
func (h *AIHandler) ListJobs(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	jobs, err := h.mpRepo.ListAIGenerationJobs(c.Request.Context(), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": jobs})
}

// GET /ai/generate/jobs/:id
func (h *AIHandler) GetJob(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	jobID := c.Param("id")
	job, err := h.mpRepo.GetAIGenerationJob(c.Request.Context(), tenantID, jobID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Job not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": job})
}

// ============================================================================
// APPLY GENERATED CONTENT TO LISTINGS
// ============================================================================

type ApplyRequest struct {
	JobID string      `json:"job_id,omitempty"`
	Items []ApplyItem `json:"items" binding:"required"`
}

type ApplyItem struct {
	ProductID        string                 `json:"product_id" binding:"required"`
	Channel          string                 `json:"channel" binding:"required"`
	ChannelAccountID string                 `json:"channel_account_id" binding:"required"`
	Title            string                 `json:"title"`
	Description      string                 `json:"description"`
	BulletPoints     []string               `json:"bullet_points,omitempty"`
	Attributes       map[string]interface{} `json:"attributes,omitempty"`
	Price            *float64               `json:"price,omitempty"`
	Quantity         *int                   `json:"quantity,omitempty"`
}

// POST /ai/generate/apply
func (h *AIHandler) ApplyGenerated(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req ApplyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	results := make(map[string]interface{})
	created := 0
	failed := 0

	for _, item := range req.Items {
		key := fmt.Sprintf("%s_%s", item.ProductID, item.Channel)
		listing := &models.Listing{
			ListingID:        uuid.New().String(),
			TenantID:         tenantID,
			ProductID:        item.ProductID,
			Channel:          item.Channel,
			ChannelAccountID: item.ChannelAccountID,
			State:            "draft",
			Overrides: &models.ListingOverrides{
				Title:       item.Title,
				Description: item.Description,
				Attributes:  item.Attributes,
				Price:       item.Price,
				Quantity:    item.Quantity,
			},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		if err := h.mpRepo.SaveListing(c.Request.Context(), listing); err != nil {
			results[key] = gin.H{"success": false, "error": err.Error()}
			failed++
		} else {
			results[key] = gin.H{"success": true, "listing_id": listing.ListingID}
			created++
		}
	}

	c.JSON(http.StatusOK, gin.H{"results": results, "created": created, "failed": failed})
}

// ============================================================================
// JOB PROCESSING
// ============================================================================

func (h *AIHandler) processGenerationJob(ctx context.Context, job *models.AIGenerationJob) {
	log.Printf("[AI] Starting generation job %s: %d products × %d channels", job.JobID, len(job.ProductIDs), len(job.Channels))

	job.Status = "running"
	job.StatusMessage = "Starting AI generation..."
	now := time.Now()
	job.StartedAt = &now
	h.mpRepo.SaveAIGenerationJob(ctx, job)

	for i, productID := range job.ProductIDs {
		input, err := h.buildAIInput(ctx, job.TenantID, productID)
		if err != nil {
			job.Results = append(job.Results, models.AIGenerationJobResult{
				ProductID: productID, Status: "failed",
				Error: fmt.Sprintf("load product: %v", err),
			})
			job.FailedCount++
			job.ProcessedCount++
			continue
		}

		job.StatusMessage = fmt.Sprintf("Generating listings for product %d/%d...", i+1, len(job.ProductIDs))
		h.mpRepo.SaveAIGenerationJob(ctx, job)

		var result *services.AIGenerationResult
		switch job.Mode {
		case "fast":
			result, err = h.aiService.GenerateListingsSinglePhase(ctx, *input, job.Channels)
		default:
			result, err = h.aiService.GenerateListings(ctx, *input, job.Channels)
		}

		if err != nil {
			job.Results = append(job.Results, models.AIGenerationJobResult{
				ProductID: productID, Status: "failed", Error: err.Error(),
			})
			job.FailedCount++
		} else {
			jobResult := models.AIGenerationJobResult{
				ProductID:  productID,
				Status:     "success",
				Listings:   make([]models.AIGeneratedListing, 0, len(result.Listings)),
				DurationMS: result.DurationMS,
			}
			for _, l := range result.Listings {
				jobResult.Listings = append(jobResult.Listings, models.AIGeneratedListing{
					Channel: l.Channel, Title: l.Title, Description: l.Description,
					BulletPoints: l.BulletPoints, CategoryID: l.CategoryID,
					CategoryName: l.CategoryName, Attributes: l.Attributes,
					SearchTerms: l.SearchTerms, Price: l.SuggestedPrice,
					Confidence: l.Confidence, Warnings: l.Warnings,
				})
			}
			job.Results = append(job.Results, jobResult)
			job.SuccessCount++

			if job.AutoApply {
				for _, l := range result.Listings {
					listing := &models.Listing{
						ListingID: uuid.New().String(), TenantID: job.TenantID,
						ProductID: productID, Channel: l.Channel,
						ChannelAccountID: job.ChannelAccountID, State: "draft",
						Overrides: &models.ListingOverrides{
							Title: l.Title, Description: l.Description, Attributes: l.Attributes,
						},
						CreatedAt: time.Now(), UpdatedAt: time.Now(),
					}
					if err := h.mpRepo.SaveListing(ctx, listing); err != nil {
						log.Printf("[AI] Failed to auto-apply listing for %s/%s: %v", productID, l.Channel, err)
					}
				}
			}
		}

		job.ProcessedCount++
		job.UpdatedAt = time.Now()
		h.mpRepo.SaveAIGenerationJob(ctx, job)

		if i < len(job.ProductIDs)-1 {
			time.Sleep(500 * time.Millisecond)
		}
	}

	job.Status = "completed"
	job.StatusMessage = fmt.Sprintf("Completed: %d succeeded, %d failed", job.SuccessCount, job.FailedCount)
	completedAt := time.Now()
	job.CompletedAt = &completedAt
	job.UpdatedAt = time.Now()
	h.mpRepo.SaveAIGenerationJob(ctx, job)

	log.Printf("[AI] Job %s completed: %d/%d succeeded", job.JobID, job.SuccessCount, job.TotalProducts)
}

func (h *AIHandler) queueGenerationTasks(ctx context.Context, job *models.AIGenerationJob) {
	processFnURL := os.Getenv("AI_GENERATE_FUNCTION_URL")
	if processFnURL == "" {
		log.Printf("[AI] AI_GENERATE_FUNCTION_URL not set, processing job %s inline", job.JobID)
		h.processGenerationJob(ctx, job)
		return
	}

	projectID := os.Getenv("GCP_PROJECT_ID")
	region := os.Getenv("GCP_REGION")
	if region == "" {
		region = "us-central1"
	}

	queuePath := fmt.Sprintf("projects/%s/locations/%s/queues/ai-generate", projectID, region)

	tasksClient, err := cloudtasks.NewClient(ctx)
	if err != nil {
		log.Printf("[AI] Cloud Tasks client error, processing inline: %v", err)
		h.processGenerationJob(ctx, job)
		return
	}
	defer tasksClient.Close()

	projectNumber := os.Getenv("GCP_PROJECT_NUMBER")
	if projectNumber == "" {
		projectNumber = "487246736287"
	}
	saEmail := fmt.Sprintf("%s-compute@developer.gserviceaccount.com", projectNumber)

	batchSize := 5
	taskCount := 0
	perTaskDelay := 10 * time.Second

	for i := 0; i < len(job.ProductIDs); i += batchSize {
		end := i + batchSize
		if end > len(job.ProductIDs) {
			end = len(job.ProductIDs)
		}
		payload := map[string]interface{}{
			"tenant_id": job.TenantID, "job_id": job.JobID,
			"product_ids":        job.ProductIDs[i:end],
			"channels":           job.Channels,
			"channel_account_id": job.ChannelAccountID,
			"mode":               job.Mode,
			"auto_apply":         job.AutoApply,
		}
		body, _ := json.Marshal(payload)
		task := &taskspb.Task{
			MessageType: &taskspb.Task_HttpRequest{
				HttpRequest: &taskspb.HttpRequest{
					HttpMethod: taskspb.HttpMethod_POST,
					Url:        processFnURL,
					Headers:    map[string]string{"Content-Type": "application/json"},
					Body:       body,
					AuthorizationHeader: &taskspb.HttpRequest_OidcToken{
						OidcToken: &taskspb.OidcToken{ServiceAccountEmail: saEmail},
					},
				},
			},
			ScheduleTime: timestamppb.New(time.Now().Add(time.Duration(taskCount) * perTaskDelay)),
		}
		if _, err := tasksClient.CreateTask(ctx, &taskspb.CreateTaskRequest{
			Parent: queuePath, Task: task,
		}); err != nil {
			log.Printf("[AI] Failed to queue task batch %d: %v", taskCount, err)
		} else {
			taskCount++
		}
	}

	job.Status = "running"
	job.StatusMessage = fmt.Sprintf("Queued %d tasks for %d products", taskCount, len(job.ProductIDs))
	startedAt := time.Now()
	job.StartedAt = &startedAt
	h.mpRepo.SaveAIGenerationJob(ctx, job)
	log.Printf("[AI] Queued %d tasks for job %s", taskCount, job.JobID)
}

// ============================================================================
// BUILD AI INPUT FROM PRODUCT DATA
// ============================================================================

func (h *AIHandler) buildAIInput(ctx context.Context, tenantID, productID string) (*services.AIProductInput, error) {
	product, err := h.productSvc.GetProduct(ctx, tenantID, productID)
	if err != nil {
		return nil, fmt.Errorf("get product: %w", err)
	}

	input := &services.AIProductInput{
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
		if product.Identifiers.EAN != nil {
			input.Identifiers["ean"] = *product.Identifiers.EAN
		}
		if product.Identifiers.UPC != nil {
			input.Identifiers["upc"] = *product.Identifiers.UPC
		}
		if product.Identifiers.ASIN != nil {
			input.Identifiers["asin"] = *product.Identifiers.ASIN
		}
		if product.Identifiers.GTIN != nil {
			input.Identifiers["gtin"] = *product.Identifiers.GTIN
		}
		if product.Identifiers.MPN != nil {
			input.Identifiers["mpn"] = *product.Identifiers.MPN
		}
		if product.Identifiers.ISBN != nil {
			input.Identifiers["isbn"] = *product.Identifiers.ISBN
		}
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

	if product.Attributes != nil {
		if price, ok := product.Attributes["source_price"].(float64); ok {
			input.SourcePrice = price
		}
		if currency, ok := product.Attributes["source_currency"].(string); ok {
			input.SourceCurrency = currency
		}
	}

	extData, err := h.mpRepo.GetExtendedDataByProductID(ctx, tenantID, productID)
	if err == nil && extData != nil {
		input.EnrichedData = extData
	}

	return input, nil
}

// PromptDirect POST /api/v1/ai/prompt — free-form AI proxy for internal tooling.
func (h *AIHandler) PromptDirect(c *gin.Context) {
	var req struct {
		System string `json:"system"`
		Prompt string `json:"prompt" binding:"required"`
		Model  string `json:"model"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !h.aiService.IsAvailable() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "AI service not configured"})
		return
	}

	model := req.Model
	if model == "" {
		model = "gemini-2.0-flash"
	}
	combined := req.Prompt
	if req.System != "" {
		combined = req.System + "\n\n" + req.Prompt
	}
	text, err := h.aiService.CallWithModel(c.Request.Context(), combined, model)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"text": strings.TrimSpace(text)})
}
