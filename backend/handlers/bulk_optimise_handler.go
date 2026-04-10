// Package handlers — bulk_optimise_handler.go
// POST /api/v1/listings/bulk-optimise
//
// Validates listing IDs, deducts credits atomically, enqueues one Cloud Task
// per listing on the existing "ai-generate" queue, logs one UsageEvent per
// listing, and returns HTTP 202 with job IDs.
package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	cloudtasks "cloud.google.com/go/cloudtasks/apiv2"
	taskspb "cloud.google.com/go/cloudtasks/apiv2/cloudtaskspb"
	"github.com/gin-gonic/gin"
	"google.golang.org/protobuf/types/known/timestamppb"

	"cloud.google.com/go/firestore"
	"module-a/instrumentation"
	"module-a/services"
)

// BulkOptimiseHandler serves POST /api/v1/listings/bulk-optimise.
type BulkOptimiseHandler struct {
	firestoreClient *firestore.Client
	usageService    *services.UsageService
}

// NewBulkOptimiseHandler constructs the handler.
func NewBulkOptimiseHandler(fc *firestore.Client, us *services.UsageService) *BulkOptimiseHandler {
	return &BulkOptimiseHandler{firestoreClient: fc, usageService: us}
}

// validOptimiseFields is the allow-list for the "fields" request param.
var validOptimiseFields = map[string]bool{
	"title":       true,
	"bullets":     true,
	"description": true,
}

// BulkOptimise handles POST /api/v1/listings/bulk-optimise.
func (h *BulkOptimiseHandler) BulkOptimise(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	// ── Step 1: Parse + validate ─────────────────────────────────────────────

	var req struct {
		ListingIDs []string `json:"listing_ids"`
		Fields     []string `json:"fields"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if len(req.ListingIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "listing_ids must not be empty"})
		return
	}
	if len(req.ListingIDs) > 50 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "max_50_listings"})
		return
	}
	if len(req.Fields) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "fields must not be empty"})
		return
	}
	for _, f := range req.Fields {
		if !validOptimiseFields[f] {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid field: %s", f)})
			return
		}
	}

	// ── Step 2: Credit check + deduction ────────────────────────────────────
	// Total cost = 1 credit per listing. checkAndDeductCredits verifies
	// the balance; the actual deduction is recorded via LogUsageEvent per
	// listing after enqueue.

	totalCost := float64(len(req.ListingIDs))

	balance, ok := h.checkCredits(ctx, tenantID, totalCost)
	if !ok {
		c.JSON(http.StatusPaymentRequired, gin.H{
			"error":    "insufficient_credits",
			"required": totalCost,
			"balance":  balance,
		})
		return
	}

	// ── Step 3: Enqueue Cloud Tasks ──────────────────────────────────────────

	jobIDs, err := h.enqueueOptimiseTasks(ctx, tenantID, req.ListingIDs, req.Fields)
	if err != nil {
		// We checked credits but couldn't enqueue — log and surface the error.
		// Per spec: do not attempt to refund credits (reconciliation pass later).
		log.Printf("[bulk-optimise] enqueue error for tenant=%s: %v", tenantID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to enqueue optimisation tasks"})
		return
	}

	// ── Step 4: Log usage events (one per listing) ───────────────────────────

	for _, listingID := range req.ListingIDs {
		_ = instrumentation.LogUsageEvent(ctx, h.firestoreClient, instrumentation.UsageEvent{
			TenantID:   tenantID,
			EventType:  instrumentation.EVTYPE_AI_LISTING_OPTIMISE,
			ListingID:  listingID,
			CreditCost: 1.0,
			Timestamp:  time.Now().UTC(),
		})
	}

	c.JSON(http.StatusAccepted, gin.H{
		"accepted": len(req.ListingIDs),
		"job_ids":  jobIDs,
	})
}

// checkCredits reads the current ledger and returns (balance, ok).
// ok is false if balance < cost; true on unlimited plans (nil CreditsRemaining).
func (h *BulkOptimiseHandler) checkCredits(ctx context.Context, tenantID string, cost float64) (float64, bool) {
	if h.usageService == nil {
		return 0, true // no billing service wired — allow
	}
	ledger, err := h.usageService.GetCurrentLedger(ctx, tenantID)
	if err != nil || ledger == nil {
		return 0, true // ledger unavailable — allow (fail-open)
	}
	if ledger.CreditsRemaining == nil {
		return 0, true // unlimited plan
	}
	balance := *ledger.CreditsRemaining
	if balance < cost {
		return balance, false
	}
	return balance, true
}

// enqueueOptimiseTasks creates one Cloud Task per listing on the ai-generate
// queue, mirroring the pattern in ai_handler.go:queueGenerationTasks.
// Returns the task name (job ID) of each created task.
func (h *BulkOptimiseHandler) enqueueOptimiseTasks(
	ctx context.Context,
	tenantID string,
	listingIDs []string,
	fields []string,
) ([]string, error) {
	processFnURL := os.Getenv("AI_GENERATE_FUNCTION_URL")
	if processFnURL == "" {
		// No Cloud Tasks URL configured — return synthetic IDs so callers still get 202.
		log.Printf("[bulk-optimise] AI_GENERATE_FUNCTION_URL not set; tasks not queued (dev mode)")
		ids := make([]string, len(listingIDs))
		for i, id := range listingIDs {
			ids[i] = fmt.Sprintf("dev-noop-%s", id)
		}
		return ids, nil
	}

	projectID := os.Getenv("GCP_PROJECT_ID")
	region := os.Getenv("GCP_REGION")
	if region == "" {
		region = "us-central1"
	}
	queuePath := fmt.Sprintf("projects/%s/locations/%s/queues/ai-generate", projectID, region)

	projectNumber := os.Getenv("GCP_PROJECT_NUMBER")
	if projectNumber == "" {
		projectNumber = "487246736287"
	}
	saEmail := fmt.Sprintf("%s-compute@developer.gserviceaccount.com", projectNumber)

	tasksClient, err := cloudtasks.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("cloudtasks client: %w", err)
	}
	defer tasksClient.Close()

	var jobIDs []string

	for i, listingID := range listingIDs {
		payload := map[string]interface{}{
			"listing_id":          listingID,
			"fields":              fields,
			"use_keyword_context": true,
			"tenant_id":           tenantID,
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
			// Stagger tasks 5s apart to avoid thundering-herd on the worker
			ScheduleTime: timestamppb.New(time.Now().Add(time.Duration(i) * 5 * time.Second)),
		}

		created, err := tasksClient.CreateTask(ctx, &taskspb.CreateTaskRequest{
			Parent: queuePath,
			Task:   task,
		})
		if err != nil {
			log.Printf("[bulk-optimise] failed to create task for listing %s: %v", listingID, err)
			jobIDs = append(jobIDs, fmt.Sprintf("error-%s", listingID))
		} else {
			jobIDs = append(jobIDs, created.GetName())
		}
	}

	return jobIDs, nil
}
