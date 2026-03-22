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
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"
	"module-a/models"
	"module-a/repository"
	"module-a/services"
)

// ============================================================================
// AI LISTING GENERATION HANDLER
// ============================================================================
// Endpoints:
//   POST /ai/generate           — Generate listings for one product (sync)
//   POST /ai/generate/bulk      — Queue bulk generation job (async)
//   GET  /ai/generate/jobs      — List generation jobs
//   GET  /ai/generate/jobs/:id  — Get generation job status + results
//   POST /ai/generate/apply     — Apply AI-generated content to actual listings
//   GET  /ai/status             — Check AI provider availability
// ============================================================================

type AIHandler struct {
	aiService   *services.AIService
	productRepo *repository.FirestoreRepository
	mpRepo      *repository.MarketplaceRepository
	productSvc  *services.ProductService
	listingSvc  *services.ListingService
	usage       *UsageInstrumentor
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
		usage:       NewUsageInstrumentor(nil), // wired via SetUsage after construction
	}
}

// ============================================================================
// STATUS ENDPOINT
// ============================================================================

// GET /ai/status
func (h *AIHandler) Status(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"available":     h.aiService.IsAvailable(),
		"has_gemini":    h.aiService.HasGemini(),
		"has_claude":    h.aiService.HasClaude(),
		"mode":          getAIMode(h.aiService),
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
	ProductID  string   `json:"product_id" binding:"required"`
	Channels   []string `json:"channels" binding:"required"`
	Mode       string   `json:"mode,omitempty"` // "hybrid" (default), "fast" (gemini only), "quality" (claude only)
}

// POST /ai/generate
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

	// Build AI input from product data
	input, err := h.buildAIInput(c.Request.Context(), tenantID, req.ProductID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Product not found or load failed: %v", err)})
		return
	}

	// Generate based on mode
	var result *services.AIGenerationResult
	mode := req.Mode
	if mode == "" {
		mode = "hybrid"
	}

	switch mode {
	case "fast":
		result, err = h.aiService.GenerateListingsSinglePhase(c.Request.Context(), *input, req.Channels)
	case "quality":
		result, err = h.aiService.GenerateListingsSinglePhase(c.Request.Context(), *input, req.Channels)
	default: // "hybrid"
		result, err = h.aiService.GenerateListings(c.Request.Context(), *input, req.Channels)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Generation failed",
			"details": err.Error(),
		})
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
	ProductID    string                         `json:"product_id" binding:"required"`
	Channel      string                         `json:"channel" binding:"required"`
	CategoryID   string                         `json:"category_id"`
	CategoryName string                         `json:"category_name"`
	CategoryPath []string                       `json:"category_path,omitempty"`
	Fields       []services.MarketplaceSchemaField `json:"fields"`
}

// POST /ai/generate-with-schema
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

	// Build AI input from product data
	input, err := h.buildAIInput(c.Request.Context(), tenantID, req.ProductID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Product not found: %v", err)})
		return
	}

	// Build the schema input
	schema := services.MarketplaceSchemaInput{
		Channel:      req.Channel,
		CategoryID:   req.CategoryID,
		CategoryName: req.CategoryName,
		CategoryPath: req.CategoryPath,
		Fields:       req.Fields,
	}

	log.Printf("[AI] Schema-aware generation: product=%s channel=%s category=%s fields=%d",
		req.ProductID, req.Channel, req.CategoryName, len(req.Fields))

	result, err := h.aiService.GenerateWithSchema(c.Request.Context(), *input, schema)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Generation failed",
			"details": err.Error(),
		})
		return
	}

	result.ProductID = req.ProductID

	c.JSON(http.StatusOK, gin.H{
		"data":    result,
		"message": fmt.Sprintf("Generated listing with %d schema fields in %dms", len(req.Fields), result.DurationMS),
	})
}

// ============================================================================
// BULK GENERATION (ASYNC VIA CLOUD TASKS)
// ============================================================================

type BulkGenerateRequest struct {
	ProductIDs       []string `json:"product_ids" binding:"required"`
	Channels         []string `json:"channels" binding:"required"`
	ChannelAccountID string   `json:"channel_account_id" binding:"required"`
	Mode             string   `json:"mode,omitempty"`
	AutoApply        bool     `json:"auto_apply,omitempty"` // Auto-create listings from results
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
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "AI service not configured",
		})
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

	// Create generation job
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

	// Save job to Firestore
	if err := h.mpRepo.SaveAIGenerationJob(c.Request.Context(), job); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create job"})
		return
	}

	// Process inline (in goroutine) for small batches, or queue via Cloud Tasks for large
	if len(req.ProductIDs) <= 10 {
		// Process inline in a goroutine
		go h.processGenerationJob(context.Background(), job)
	} else {
		// Queue via Cloud Tasks in batches
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
	JobID   string                `json:"job_id,omitempty"`
	Items   []ApplyItem           `json:"items" binding:"required"`
}

type ApplyItem struct {
	ProductID        string                   `json:"product_id" binding:"required"`
	Channel          string                   `json:"channel" binding:"required"`
	ChannelAccountID string                   `json:"channel_account_id" binding:"required"`
	Title            string                   `json:"title"`
	Description      string                   `json:"description"`
	BulletPoints     []string                 `json:"bullet_points,omitempty"`
	Attributes       map[string]interface{}   `json:"attributes,omitempty"`
	Price            *float64                 `json:"price,omitempty"`
	Quantity         *int                     `json:"quantity,omitempty"`
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

		// Create listing with AI-generated content as overrides
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

	c.JSON(http.StatusOK, gin.H{
		"results": results,
		"created": created,
		"failed":  failed,
	})
}

// ============================================================================
// JOB PROCESSING (runs in goroutine or Cloud Task)
// ============================================================================

func (h *AIHandler) processGenerationJob(ctx context.Context, job *models.AIGenerationJob) {
	log.Printf("[AI] Starting generation job %s: %d products × %d channels", job.JobID, len(job.ProductIDs), len(job.Channels))

	job.Status = "running"
	job.StatusMessage = "Starting AI generation..."
	now := time.Now()
	job.StartedAt = &now
	h.mpRepo.SaveAIGenerationJob(ctx, job)

	for i, productID := range job.ProductIDs {
		// Build input
		input, err := h.buildAIInput(ctx, job.TenantID, productID)
		if err != nil {
			job.Results = append(job.Results, models.AIGenerationJobResult{
				ProductID: productID,
				Status:    "failed",
				Error:     fmt.Sprintf("load product: %v", err),
			})
			job.FailedCount++
			job.ProcessedCount++
			continue
		}

		// Generate
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
				ProductID: productID,
				Status:    "failed",
				Error:     err.Error(),
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
					Channel:      l.Channel,
					Title:        l.Title,
					Description:  l.Description,
					BulletPoints: l.BulletPoints,
					CategoryID:   l.CategoryID,
					CategoryName: l.CategoryName,
					Attributes:   l.Attributes,
					SearchTerms:  l.SearchTerms,
					Price:        l.SuggestedPrice,
					Confidence:   l.Confidence,
					Warnings:     l.Warnings,
				})
			}
			job.Results = append(job.Results, jobResult)
			job.SuccessCount++

			// Token usage tracking not yet available on AIGenerationResult

			// Auto-apply if requested
			if job.AutoApply {
				for _, l := range result.Listings {
					listing := &models.Listing{
						ListingID:        uuid.New().String(),
						TenantID:         job.TenantID,
						ProductID:        productID,
						Channel:          l.Channel,
						ChannelAccountID: job.ChannelAccountID,
						State:            "draft",
						Overrides: &models.ListingOverrides{
							Title:       l.Title,
							Description: l.Description,
							Attributes:  l.Attributes,
						},
						CreatedAt: time.Now(),
						UpdatedAt: time.Now(),
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

		// Rate limiting — small pause between products to avoid API throttling
		if i < len(job.ProductIDs)-1 {
			time.Sleep(500 * time.Millisecond)
		}
	}

	// Complete
	job.Status = "completed"
	job.StatusMessage = fmt.Sprintf("Completed: %d succeeded, %d failed", job.SuccessCount, job.FailedCount)
	completedAt := time.Now()
	job.CompletedAt = &completedAt
	job.UpdatedAt = time.Now()
	h.mpRepo.SaveAIGenerationJob(ctx, job)

	log.Printf("[AI] Job %s completed: %d/%d succeeded", job.JobID, job.SuccessCount, job.TotalProducts)
}

// queueGenerationTasks splits large jobs into Cloud Tasks batches
func (h *AIHandler) queueGenerationTasks(ctx context.Context, job *models.AIGenerationJob) {
	processFnURL := os.Getenv("AI_GENERATE_FUNCTION_URL")
	if processFnURL == "" {
		// Fallback: process inline
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

	// Batch products: 5 per task to balance latency vs throughput
	batchSize := 5
	taskCount := 0
	perTaskDelay := 10 * time.Second

	for i := 0; i < len(job.ProductIDs); i += batchSize {
		end := i + batchSize
		if end > len(job.ProductIDs) {
			end = len(job.ProductIDs)
		}

		payload := map[string]interface{}{
			"tenant_id":          job.TenantID,
			"job_id":             job.JobID,
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
						OidcToken: &taskspb.OidcToken{
							ServiceAccountEmail: saEmail,
						},
					},
				},
			},
			ScheduleTime: timestamppb.New(time.Now().Add(time.Duration(taskCount) * perTaskDelay)),
		}

		if _, err := tasksClient.CreateTask(ctx, &taskspb.CreateTaskRequest{
			Parent: queuePath,
			Task:   task,
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
		Title:      product.Title,
		SKU:        product.SKU,
		KeyFeatures: product.KeyFeatures,
		Tags:       product.Tags,
		Attributes: product.Attributes,
	}

	if product.Description != nil {
		input.Description = *product.Description
	}
	if product.Brand != nil {
		input.Brand = *product.Brand
	}

	// Map identifiers
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

	// Map images
	for _, asset := range product.Assets {
		if asset.URL != "" {
			input.ImageURLs = append(input.ImageURLs, asset.URL)
		}
	}

	// Map dimensions
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

	// Extract source price from attributes
	if product.Attributes != nil {
		if price, ok := product.Attributes["source_price"].(float64); ok {
			input.SourcePrice = price
		}
		if currency, ok := product.Attributes["source_currency"].(string); ok {
			input.SourceCurrency = currency
		}
	}

	// Try to load enriched/extended data
	extData, err := h.mpRepo.GetExtendedDataByProductID(ctx, tenantID, productID)
	if err == nil && extData != nil {
		input.EnrichedData = extData
	}

	return input, nil
}

// PromptDirect POST /api/v1/ai/prompt
// Free-form AI proxy used by internal tooling (e.g. DataSeeder).
// Takes {system, prompt} and returns {text}.  Never exposed to end-users.
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
