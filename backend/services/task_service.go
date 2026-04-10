package services

// ============================================================================
// TASK SERVICE — Cloud Tasks wrapper for async order processing
// ============================================================================
// When a new order is imported (CreateOrder returns isNew == true), the order
// handler calls EnqueueWorkflowProcessing to push a task onto the Cloud Tasks
// queue. This decouples the channel import (which must respond quickly to avoid
// SP-API / eBay timeouts) from workflow evaluation (which may be slower).
//
// Cloud Tasks handles retries automatically. The handler for the task is:
//   POST /api/v1/orders/:id/process-workflows
//
// Required GCP setup (one-time):
//   gcloud tasks queues create marketmate-workflow-queue --location=us-central1
//
// Required IAM (one-time):
//   gcloud projects add-iam-policy-binding marketmate-486116 \
//     --member="serviceAccount:<run-sa>@developer.gserviceaccount.com" \
//     --role="roles/cloudtasks.enqueuer"
//
// Environment variables:
//   CLOUD_TASKS_PROJECT  — GCP project ID (default: marketmate-486116)
//   CLOUD_TASKS_LOCATION — Cloud Tasks location (default: us-central1)
//   CLOUD_TASKS_QUEUE    — Queue name (default: marketmate-workflow-queue)
//   API_BASE_URL         — Internal base URL of this service, used for task target
//                          e.g. https://marketmate-api-487246736287.us-central1.run.app
// ============================================================================

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	cloudtasks "cloud.google.com/go/cloudtasks/apiv2"
	taskspb "cloud.google.com/go/cloudtasks/apiv2/cloudtaskspb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	defaultProject  = "marketmate-486116"
	defaultLocation = "us-central1"
	defaultQueue    = "marketmate-workflow-queue"

	// How long Cloud Tasks will wait before delivering the task.
	// Small delay lets the Firestore write fully propagate before the engine
	// reads the order back.
	taskDeliveryDelay = 2 * time.Second

	// How many times Cloud Tasks should retry on failure (in addition to first attempt).
	taskMaxAttempts = 3

	// HTTP method for the task handler
	taskHTTPMethod = taskspb.HttpMethod_POST
)

// TaskService wraps the Cloud Tasks client.
type TaskService struct {
	client   *cloudtasks.Client
	project  string
	location string
	queue    string
	apiBase  string
}

// NewTaskService creates a new TaskService, connecting to Cloud Tasks.
// Returns an error if the Cloud Tasks client cannot be initialised.
// In development (no credentials), this will return an error — callers
// should handle gracefully and skip task enqueuing.
func NewTaskService(ctx context.Context) (*TaskService, error) {
	client, err := cloudtasks.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("cloud tasks client: %w", err)
	}

	project := envOr("CLOUD_TASKS_PROJECT", defaultProject)
	location := envOr("CLOUD_TASKS_LOCATION", defaultLocation)
	queue := envOr("CLOUD_TASKS_QUEUE", defaultQueue)
	apiBase := envOr("API_BASE_URL", "https://marketmate-api-487246736287.us-central1.run.app")

	return &TaskService{
		client:   client,
		project:  project,
		location: location,
		queue:    queue,
		apiBase:  apiBase,
	}, nil
}

func (s *TaskService) Close() {
	if s.client != nil {
		if err := s.client.Close(); err != nil {
			log.Printf("[tasks] warning: failed to close client: %v", err)
		}
	}
}

// queuePath returns the fully-qualified Cloud Tasks queue path.
func (s *TaskService) queuePath() string {
	return fmt.Sprintf("projects/%s/locations/%s/queues/%s",
		s.project, s.location, s.queue)
}

// ============================================================================
// ENQUEUE WORKFLOW PROCESSING
// ============================================================================

// WorkflowTaskPayload is serialised as the HTTP body of the Cloud Task.
// The process-workflows handler doesn't currently read the body (it uses
// the URL param for order ID), but we include it for logging/debugging.
type WorkflowTaskPayload struct {
	TenantID  string `json:"tenant_id"`
	OrderID   string `json:"order_id"`
	EnqueuedAt string `json:"enqueued_at"`
}

// EnqueueWorkflowProcessing pushes a task to trigger workflow evaluation for
// a newly-imported order. The task calls:
//   POST /api/v1/orders/{orderID}/process-workflows
// with X-Tenant-Id header set.
//
// This is fire-and-forget — the error is logged but not propagated to the
// caller, because failing to enqueue should never prevent an order from being
// saved.
func (s *TaskService) EnqueueWorkflowProcessing(ctx context.Context, tenantID, orderID string) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("task service not initialised")
	}

	url := fmt.Sprintf("%s/api/v1/orders/%s/process-workflows", s.apiBase, orderID)

	payload := WorkflowTaskPayload{
		TenantID:   tenantID,
		OrderID:    orderID,
		EnqueuedAt: time.Now().Format(time.RFC3339),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	scheduleTime := time.Now().Add(taskDeliveryDelay)

	req := &taskspb.CreateTaskRequest{
		Parent: s.queuePath(),
		Task: &taskspb.Task{
			MessageType: &taskspb.Task_HttpRequest{
				HttpRequest: &taskspb.HttpRequest{
					HttpMethod: taskHTTPMethod,
					Url:        url,
					Headers: map[string]string{
						"Content-Type": "application/json",
						"X-Tenant-Id": tenantID,
					},
					Body: body,
				},
			},
			ScheduleTime: timestamppb.New(scheduleTime),
			// Unique name to prevent duplicate tasks for the same order.
			// Cloud Tasks deduplicates tasks with the same name for ~1 hour.
			Name: fmt.Sprintf("%s/tasks/wf-%s-%s", s.queuePath(), tenantID, orderID),
		},
	}

	task, err := s.client.CreateTask(ctx, req)
	if err != nil {
		return fmt.Errorf("create task: %w", err)
	}

	log.Printf("[tasks] enqueued workflow processing for order %s/%s → task %s",
		tenantID, orderID, task.GetName())
	return nil
}

// ============================================================================
// ENQUEUE EBAY ENRICHMENT
// ============================================================================

// EbayEnrichTaskPayload is the body sent to POST /api/v1/internal/ebay/enrich/task
type EbayEnrichTaskPayload struct {
	TenantID     string `json:"tenant_id"`
	ProductID    string `json:"product_id"`
	EbayItemID   string `json:"ebay_item_id"`
	EAN          string `json:"ean,omitempty"`
	CredentialID string `json:"credential_id"`
	EnqueuedAt   string `json:"enqueued_at"`
}

// EnqueueEbayEnrichment pushes a single-product enrichment task onto the
// ebay-ai-enrich queue. The queue name comes from CLOUD_TASKS_QUEUE_EBAY_ENRICH.
// Fire-and-forget: errors are logged but never returned to the caller.
func (s *TaskService) EnqueueEbayEnrichment(ctx context.Context, p EbayEnrichTaskPayload) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("task service not initialised")
	}

	queue := envOr("CLOUD_TASKS_QUEUE_EBAY_ENRICH",
		fmt.Sprintf("projects/%s/locations/%s/queues/ebay-ai-enrich", s.project, s.location))

	p.EnqueuedAt = time.Now().Format(time.RFC3339)
	body, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal enrich payload: %w", err)
	}

	url := fmt.Sprintf("%s/api/v1/internal/ebay/enrich/task", s.apiBase)

	// Unique task name deduplicates concurrent re-queues for the same product
	// (Cloud Tasks ignores duplicates for ~1 hour after the first creation).
	taskName := fmt.Sprintf("%s/tasks/enrich-%s-%s", queue, p.TenantID, p.ProductID)

	req := &taskspb.CreateTaskRequest{
		Parent: queue,
		Task: &taskspb.Task{
			Name: taskName,
			MessageType: &taskspb.Task_HttpRequest{
				HttpRequest: &taskspb.HttpRequest{
					HttpMethod: taskspb.HttpMethod_POST,
					Url:        url,
					Headers: map[string]string{
						"Content-Type": "application/json",
						"X-Tenant-Id":  p.TenantID,
					},
					Body: body,
				},
			},
			// Small delay so Firestore extended_data write has time to propagate
			ScheduleTime: timestamppb.New(time.Now().Add(5 * time.Second)),
		},
	}

	task, err := s.client.CreateTask(ctx, req)
	if err != nil {
		return fmt.Errorf("create enrich task: %w", err)
	}

	log.Printf("[tasks] queued eBay enrichment for product %s/%s → %s",
		p.TenantID, p.ProductID, task.GetName())
	return nil
}

// ============================================================================
// ORDER SYNC TASKS
// ============================================================================
//
// Each active credential with order sync enabled gets its own independent
// self-rescheduling Cloud Tasks chain. The flow is:
//
//   1. EnqueueOrderSync is called once per credential (on enable, on deploy,
//      or on credential creation). It creates a task scheduled for "now".
//
//   2. The task handler at POST /api/v1/internal/orders/sync-task:
//      a. Runs the order import for that credential.
//      b. Calls EnqueueOrderSync again, scheduled FrequencyMinutes in the
//         future — perpetuating the chain.
//
// Each task name includes a Unix timestamp so that re-enqueues never collide
// with the 4-hour Cloud Tasks dedup window on named tasks.
//
// If a task fails, Cloud Tasks retries it (with backoff). The chain naturally
// self-heals. To stop polling, set Config.Orders.Enabled=false; the handler
// checks this before re-enqueueing.
//
// Required GCP setup (one-time):
//   gcloud tasks queues create marketmate-order-sync \
//     --location=us-central1 \
//     --max-concurrent-dispatches=500 \
//     --max-attempts=5
//
// Environment variables:
//   CLOUD_TASKS_QUEUE_ORDER_SYNC — full queue path, or defaults to
//                                  projects/<proj>/locations/<loc>/queues/marketmate-order-sync
// ============================================================================

// OrderSyncTaskPayload is the body sent to POST /api/v1/internal/orders/sync-task
type OrderSyncTaskPayload struct {
	TenantID     string `json:"tenant_id"`
	CredentialID string `json:"credential_id"`
	Channel      string `json:"channel"`
	EnqueuedAt   string `json:"enqueued_at"`
}

// orderSyncQueuePath returns the fully-qualified queue path for order sync tasks.
func (s *TaskService) orderSyncQueuePath() string {
	explicit := os.Getenv("CLOUD_TASKS_QUEUE_ORDER_SYNC")
	if explicit != "" {
		return explicit
	}
	return fmt.Sprintf("projects/%s/locations/%s/queues/marketmate-order-sync",
		s.project, s.location)
}

// EnqueueOrderSync enqueues a single order-sync task for one credential,
// scheduled to fire after the given delay.
//
// The task name embeds the scheduled Unix timestamp so it never collides with
// the 4-hour named-task dedup window. Cloud Tasks will reject a duplicate name
// for ~4 hours after creation, so using a timestamp suffix guarantees
// uniqueness even for the same credential being re-enqueued every 15 minutes.
func (s *TaskService) EnqueueOrderSync(ctx context.Context, tenantID, credentialID, channel string, delay time.Duration) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("task service not initialised")
	}

	scheduleAt := time.Now().Add(delay)

	payload := OrderSyncTaskPayload{
		TenantID:     tenantID,
		CredentialID: credentialID,
		Channel:      channel,
		EnqueuedAt:   time.Now().Format(time.RFC3339),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal order sync payload: %w", err)
	}

	url := fmt.Sprintf("%s/api/v1/internal/orders/sync-task", s.apiBase)

	// Timestamp suffix makes the name unique for every scheduling slot, so
	// re-enqueuing from the task handler never hits the 4-hour dedup window.
	taskName := fmt.Sprintf("%s/tasks/order-sync-%s-%s-%d",
		s.orderSyncQueuePath(), tenantID, credentialID, scheduleAt.Unix())

	req := &taskspb.CreateTaskRequest{
		Parent: s.orderSyncQueuePath(),
		Task: &taskspb.Task{
			Name: taskName,
			MessageType: &taskspb.Task_HttpRequest{
				HttpRequest: &taskspb.HttpRequest{
					HttpMethod: taskspb.HttpMethod_POST,
					Url:        url,
					Headers: map[string]string{
						"Content-Type": "application/json",
						"X-Tenant-Id":  tenantID,
					},
					Body: body,
				},
			},
			ScheduleTime: timestamppb.New(scheduleAt),
		},
	}

	task, err := s.client.CreateTask(ctx, req)
	if err != nil {
		return fmt.Errorf("create order sync task: %w", err)
	}

	log.Printf("[tasks] scheduled order sync for %s/%s in %v → %s",
		tenantID, credentialID, delay.Round(time.Second), task.GetName())
	return nil
}

// ============================================================================
// HELPERS
// ============================================================================

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
