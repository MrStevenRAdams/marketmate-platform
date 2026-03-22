package handlers

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"

	"module-a/models"
	"module-a/services"
)

// ============================================================================
// WORKFLOW HANDLER
// ============================================================================

type WorkflowHandler struct {
	engine *services.WorkflowEngine
	client *firestore.Client
}

func NewWorkflowHandler(engine *services.WorkflowEngine, client *firestore.Client) *WorkflowHandler {
	return &WorkflowHandler{engine: engine, client: client}
}

func (h *WorkflowHandler) tenantID(c *gin.Context) string {
	// Prefer middleware-set value; fall back to header for backward compat
	if tid := c.GetString("tenant_id"); tid != "" {
		return tid
	}
	return c.GetHeader("X-Tenant-Id")
}

// ============================================================================
// WORKFLOW CRUD
// ============================================================================

// ListWorkflows GET /api/v1/workflows
func (h *WorkflowHandler) ListWorkflows(c *gin.Context) {
	tenantID := h.tenantID(c)
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant ID required"})
		return
	}

	ctx := c.Request.Context()
	q := h.client.Collection("tenants").Doc(tenantID).Collection("workflows").Query

	// Optional filters
	if status := c.Query("status"); status != "" {
		q = q.Where("status", "==", status)
	}

	q = q.OrderBy("priority", firestore.Desc)

	iter := q.Documents(ctx)
	defer iter.Stop()

	var workflows []models.Workflow
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch workflows"})
			return
		}
		var wf models.Workflow
		if err := doc.DataTo(&wf); err != nil {
			log.Printf("Failed to unmarshal workflow %s: %v", doc.Ref.ID, err)
			continue
		}
		workflows = append(workflows, wf)
	}

	if workflows == nil {
		workflows = []models.Workflow{}
	}

	c.JSON(http.StatusOK, gin.H{
		"workflows": workflows,
		"count":     len(workflows),
	})
}

// GetWorkflow GET /api/v1/workflows/:id
func (h *WorkflowHandler) GetWorkflow(c *gin.Context) {
	tenantID := h.tenantID(c)
	workflowID := c.Param("id")

	doc, err := h.client.Collection("tenants").Doc(tenantID).Collection("workflows").Doc(workflowID).Get(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "workflow not found"})
		return
	}

	var wf models.Workflow
	if err := doc.DataTo(&wf); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse workflow"})
		return
	}

	c.JSON(http.StatusOK, wf)
}

// CreateWorkflow POST /api/v1/workflows
func (h *WorkflowHandler) CreateWorkflow(c *gin.Context) {
	tenantID := h.tenantID(c)
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant ID required"})
		return
	}

	var wf models.Workflow
	if err := c.ShouldBindJSON(&wf); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid workflow: " + err.Error()})
		return
	}

	// Validation
	if wf.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "workflow name is required"})
		return
	}
	if len(wf.Actions) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "workflow must have at least one action"})
		return
	}

	// Set defaults
	wf.WorkflowID = "wf_" + uuid.New().String()
	wf.TenantID = tenantID
	wf.Status = models.WorkflowStatusDraft
	wf.CreatedAt = time.Now()
	wf.UpdatedAt = time.Now()
	if wf.Priority == 0 {
		wf.Priority = 100
	}
	if wf.Trigger.Type == "" {
		wf.Trigger.Type = models.TriggerTypeOrderImported
	}
	if wf.Settings.LogLevel == "" {
		wf.Settings.LogLevel = "normal"
	}

	_, err := h.client.Collection("tenants").Doc(tenantID).Collection("workflows").Doc(wf.WorkflowID).Set(c.Request.Context(), wf)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save workflow"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"workflow_id": wf.WorkflowID,
		"status":      wf.Status,
		"message":     "Workflow created successfully",
	})
}

// UpdateWorkflow PATCH /api/v1/workflows/:id
func (h *WorkflowHandler) UpdateWorkflow(c *gin.Context) {
	tenantID := h.tenantID(c)
	workflowID := c.Param("id")

	// Verify it exists
	ref := h.client.Collection("tenants").Doc(tenantID).Collection("workflows").Doc(workflowID)
	if _, err := ref.Get(c.Request.Context()); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "workflow not found"})
		return
	}

	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid update body"})
		return
	}

	// Always set updated_at
	updates["updated_at"] = time.Now()

	// Build firestore updates
	fsUpdates := make([]firestore.Update, 0, len(updates))
	for k, v := range updates {
		fsUpdates = append(fsUpdates, firestore.Update{Path: k, Value: v})
	}

	if _, err := ref.Update(c.Request.Context(), fsUpdates); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update workflow"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Workflow updated", "workflow_id": workflowID})
}

// DeleteWorkflow DELETE /api/v1/workflows/:id
func (h *WorkflowHandler) DeleteWorkflow(c *gin.Context) {
	tenantID := h.tenantID(c)
	workflowID := c.Param("id")

	// Soft delete — set status to archived
	_, err := h.client.Collection("tenants").Doc(tenantID).Collection("workflows").Doc(workflowID).
		Update(c.Request.Context(), []firestore.Update{
			{Path: "status", Value: models.WorkflowStatusArchived},
			{Path: "updated_at", Value: time.Now()},
		})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete workflow"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Workflow archived"})
}

// ============================================================================
// WORKFLOW OPERATIONS
// ============================================================================

// ActivateWorkflow POST /api/v1/workflows/:id/activate
func (h *WorkflowHandler) ActivateWorkflow(c *gin.Context) {
	h.setWorkflowStatus(c, models.WorkflowStatusActive)
}

// PauseWorkflow POST /api/v1/workflows/:id/pause
func (h *WorkflowHandler) PauseWorkflow(c *gin.Context) {
	h.setWorkflowStatus(c, models.WorkflowStatusPaused)
}

func (h *WorkflowHandler) setWorkflowStatus(c *gin.Context, newStatus string) {
	tenantID := h.tenantID(c)
	workflowID := c.Param("id")

	_, err := h.client.Collection("tenants").Doc(tenantID).Collection("workflows").Doc(workflowID).
		Update(c.Request.Context(), []firestore.Update{
			{Path: "status", Value: newStatus},
			{Path: "updated_at", Value: time.Now()},
		})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to set status to %s", newStatus)})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"workflow_id": workflowID,
		"status":      newStatus,
		"message":     fmt.Sprintf("Workflow %s", newStatus),
	})
}

// DuplicateWorkflow POST /api/v1/workflows/:id/duplicate
func (h *WorkflowHandler) DuplicateWorkflow(c *gin.Context) {
	tenantID := h.tenantID(c)
	workflowID := c.Param("id")

	// Load original
	doc, err := h.client.Collection("tenants").Doc(tenantID).Collection("workflows").Doc(workflowID).Get(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "workflow not found"})
		return
	}

	var original models.Workflow
	if err := doc.DataTo(&original); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse workflow"})
		return
	}

	// Create copy
	copy := original
	copy.WorkflowID = "wf_" + uuid.New().String()
	copy.Name = "Copy of " + original.Name
	copy.Status = models.WorkflowStatusDraft
	copy.CreatedAt = time.Now()
	copy.UpdatedAt = time.Now()
	copy.Stats = models.WorkflowStats{} // Reset stats

	_, err = h.client.Collection("tenants").Doc(tenantID).Collection("workflows").Doc(copy.WorkflowID).Set(c.Request.Context(), copy)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to duplicate workflow"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"workflow_id": copy.WorkflowID,
		"name":        copy.Name,
		"message":     "Workflow duplicated successfully",
	})
}

// ============================================================================
// TESTING & SIMULATION
// ============================================================================

// TestWorkflow POST /api/v1/workflows/:id/test
// Tests a workflow against a real order ID or a sample order payload
func (h *WorkflowHandler) TestWorkflow(c *gin.Context) {
	tenantID := h.tenantID(c)
	workflowID := c.Param("id")

	var req struct {
		OrderID     string        `json:"order_id"`      // Test against real order
		SampleOrder *models.Order `json:"order"`         // Test against sample order
		SampleLines []models.OrderLine `json:"lines"`    // Sample order lines
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	ctx := c.Request.Context()

	// Load the workflow
	doc, err := h.client.Collection("tenants").Doc(tenantID).Collection("workflows").Doc(workflowID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "workflow not found"})
		return
	}

	var wf models.Workflow
	if err := doc.DataTo(&wf); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse workflow"})
		return
	}

	// Get order and lines
	var order *models.Order
	var lines []models.OrderLine

	if req.OrderID != "" {
		// Real order
		orderDoc, err := h.client.Collection("tenants").Doc(tenantID).Collection("orders").Doc(req.OrderID).Get(ctx)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
			return
		}
		var o models.Order
		orderDoc.DataTo(&o)
		order = &o

		// Load lines
		linesIter := h.client.Collection("tenants").Doc(tenantID).Collection("orders").Doc(req.OrderID).Collection("lines").Documents(ctx)
		defer linesIter.Stop()
		for {
			ldoc, err := linesIter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				break
			}
			var line models.OrderLine
			ldoc.DataTo(&line)
			lines = append(lines, line)
		}
	} else if req.SampleOrder != nil {
		order = req.SampleOrder
		lines = req.SampleLines
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "provide either order_id or order sample"})
		return
	}

	// Evaluate workflow (read-only — no actions executed)
	start := time.Now()

	// Use a local engine instance for test evaluation
	testEngine := services.NewWorkflowEngine(h.engine.GetRepo())
	evalResult, matched := testEngine.EvaluateWorkflowPublic(&wf, order, lines)

	response := gin.H{
		"workflow_id":   workflowID,
		"workflow_name": wf.Name,
		"matched":       matched,
		"conditions":    evalResult.Conditions,
		"execution_time_ms": time.Since(start).Milliseconds(),
	}

	if matched {
		response["message"] = "This workflow would match this order"
		response["actions"] = wf.Actions
	} else {
		response["message"] = "This workflow does not match this order"
	}

	c.JSON(http.StatusOK, response)
}

// SimulateOrder POST /api/v1/workflows/simulate
// Simulates an order against ALL active workflows and shows which would match
func (h *WorkflowHandler) SimulateOrder(c *gin.Context) {
	tenantID := h.tenantID(c)

	var req struct {
		OrderID     string             `json:"order_id"`
		SampleOrder *models.Order      `json:"order"`
		SampleLines []models.OrderLine `json:"lines"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	ctx := c.Request.Context()

	var order *models.Order
	var lines []models.OrderLine

	if req.OrderID != "" {
		orderDoc, err := h.client.Collection("tenants").Doc(tenantID).Collection("orders").Doc(req.OrderID).Get(ctx)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
			return
		}
		var o models.Order
		orderDoc.DataTo(&o)
		order = &o
	} else if req.SampleOrder != nil {
		order = req.SampleOrder
		lines = req.SampleLines
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "provide either order_id or order sample"})
		return
	}

	// Load all active workflows
	iter := h.client.Collection("tenants").Doc(tenantID).Collection("workflows").
		Where("status", "==", models.WorkflowStatusActive).
		OrderBy("priority", firestore.Desc).
		Documents(ctx)
	defer iter.Stop()

	testEngine := services.NewWorkflowEngine(h.engine.GetRepo())
	type SimResult struct {
		WorkflowID   string                       `json:"workflow_id"`
		WorkflowName string                       `json:"workflow_name"`
		Priority     int                          `json:"priority"`
		WouldMatch   bool                         `json:"would_match"`
		WouldExecute bool                         `json:"would_execute"` // true only for first match
		Conditions   []models.ConditionEvalResult `json:"conditions"`
	}

	var results []SimResult
	firstMatchFound := false

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}

		var wf models.Workflow
		if err := doc.DataTo(&wf); err != nil {
			continue
		}

		evalResult, matched := testEngine.EvaluateWorkflowPublic(&wf, order, lines)
		wouldExecute := matched && !firstMatchFound
		if matched {
			firstMatchFound = true
		}

		results = append(results, SimResult{
			WorkflowID:   wf.WorkflowID,
			WorkflowName: wf.Name,
			Priority:     wf.Priority,
			WouldMatch:   matched,
			WouldExecute: wouldExecute,
			Conditions:   evalResult.Conditions,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"workflows_evaluated": len(results),
		"first_match_found":   firstMatchFound,
		"results":             results,
	})
}

// ============================================================================
// EXECUTION HISTORY
// ============================================================================

// GetExecutions GET /api/v1/workflows/:id/executions
func (h *WorkflowHandler) GetExecutions(c *gin.Context) {
	tenantID := h.tenantID(c)
	workflowID := c.Param("id")

	ctx := c.Request.Context()
	iter := h.client.Collection("tenants").Doc(tenantID).Collection("workflow_executions").
		Where("matched_workflow_id", "==", workflowID).
		OrderBy("triggered_at", firestore.Desc).
		Limit(50).
		Documents(ctx)
	defer iter.Stop()

	var executions []models.WorkflowExecution
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch executions"})
			return
		}
		var exec models.WorkflowExecution
		doc.DataTo(&exec)
		executions = append(executions, exec)
	}

	if executions == nil {
		executions = []models.WorkflowExecution{}
	}

	c.JSON(http.StatusOK, gin.H{
		"executions": executions,
		"count":      len(executions),
	})
}

// GetExecution GET /api/v1/workflows/executions/:id
func (h *WorkflowHandler) GetExecution(c *gin.Context) {
	tenantID := h.tenantID(c)
	executionID := c.Param("id")

	doc, err := h.client.Collection("tenants").Doc(tenantID).Collection("workflow_executions").Doc(executionID).Get(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "execution not found"})
		return
	}

	var exec models.WorkflowExecution
	doc.DataTo(&exec)
	c.JSON(http.StatusOK, exec)
}

// ============================================================================
// BULK OPERATIONS
// ============================================================================

// BulkActivate POST /api/v1/workflows/bulk/activate
func (h *WorkflowHandler) BulkActivate(c *gin.Context) {
	h.bulkSetStatus(c, models.WorkflowStatusActive)
}

// BulkPause POST /api/v1/workflows/bulk/pause
func (h *WorkflowHandler) BulkPause(c *gin.Context) {
	h.bulkSetStatus(c, models.WorkflowStatusPaused)
}

func (h *WorkflowHandler) bulkSetStatus(c *gin.Context, newStatus string) {
	tenantID := h.tenantID(c)

	var req struct {
		WorkflowIDs []string `json:"workflow_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.WorkflowIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "workflow_ids required"})
		return
	}

	updated := 0
	for _, wfID := range req.WorkflowIDs {
		_, err := h.client.Collection("tenants").Doc(tenantID).Collection("workflows").Doc(wfID).
			Update(c.Request.Context(), []firestore.Update{
				{Path: "status", Value: newStatus},
				{Path: "updated_at", Value: time.Now()},
			})
		if err != nil {
			log.Printf("Failed to update workflow %s: %v", wfID, err)
			continue
		}
		updated++
	}

	c.JSON(http.StatusOK, gin.H{
		"updated": updated,
		"total":   len(req.WorkflowIDs),
		"status":  newStatus,
	})
}


// ReorderWorkflows PATCH /api/v1/workflows/reorder
// Accepts an ordered list of workflow IDs (highest priority first) and
// updates each workflow's priority field accordingly.
func (h *WorkflowHandler) ReorderWorkflows(c *gin.Context) {
	tenantID := h.tenantID(c)

	var req struct {
		OrderedIDs []string `json:"ordered_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.OrderedIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ordered_ids required"})
		return
	}

	total := len(req.OrderedIDs)
	updated := 0
	for i, wfID := range req.OrderedIDs {
		// First ID gets highest priority (total), last gets 1
		priority := total - i
		_, err := h.client.Collection("tenants").Doc(tenantID).Collection("workflows").Doc(wfID).
			Update(c.Request.Context(), []firestore.Update{
				{Path: "priority", Value: priority},
				{Path: "updated_at", Value: time.Now()},
			})
		if err != nil {
			log.Printf("[WorkflowHandler] ReorderWorkflows: failed to update %s: %v", wfID, err)
			continue
		}
		updated++
	}

	c.JSON(http.StatusOK, gin.H{
		"updated": updated,
		"total":   total,
	})
}

// ProcessOrderWorkflows POST /api/v1/orders/:id/process-workflows
// Manually triggers workflow processing for a specific order
func (h *WorkflowHandler) ProcessOrderWorkflows(c *gin.Context) {
	tenantID := h.tenantID(c)
	orderID := c.Param("id")

	result, err := h.engine.ProcessOrder(c.Request.Context(), tenantID, orderID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":    "workflow processing failed",
			"detail":   err.Error(),
			"order_id": orderID,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"order_id":     orderID,
		"matched":      result.Matched,
		"workflow_id":  result.WorkflowID,
		"execution_id": result.ExecutionID,
		"duration_ms":  result.DurationMs,
		"test_mode":    result.TestMode,
	})
}
