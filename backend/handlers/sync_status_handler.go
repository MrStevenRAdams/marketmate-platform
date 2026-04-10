package handlers

import (
	"net/http"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"google.golang.org/api/iterator"
)

// ============================================================================
// SYNC STATUS HANDLER
// ============================================================================
// Aggregates jobs from multiple collections into a single status panel.
// Collections queried: marketplace_import_jobs, order_import_jobs,
//                      automation_executions, ai_jobs
// ============================================================================

type SyncStatusHandler struct {
	client *firestore.Client
}

func NewSyncStatusHandler(client *firestore.Client) *SyncStatusHandler {
	return &SyncStatusHandler{client: client}
}

type SyncTask struct {
	TaskID    string    `json:"task_id"`
	Type      string    `json:"type"`    // import|order_import|automation|ai_generation
	Channel   string    `json:"channel"` // amazon|ebay|temu|manual
	Source    string    `json:"source"`  // human-readable source label
	Status    string    `json:"status"`  // running|pending|completed|error
	Progress  int       `json:"progress"`
	Total     int       `json:"total"`
	StartedAt time.Time `json:"started_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Error     string    `json:"error,omitempty"`
	Ack       bool      `json:"ack"` // has error been acknowledged
}

// ── GET /api/v1/sync/status ──────────────────────────────────────────────────

func (h *SyncStatusHandler) GetStatus(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()
	since := time.Now().Add(-24 * time.Hour)

	var tasks []SyncTask

	// ── Marketplace import jobs ───────────────────────────────────────────────
	iter := h.client.Collection("tenants").Doc(tenantID).Collection("marketplace_import_jobs").
		Where("created_at", ">=", since).
		OrderBy("created_at", firestore.Desc).
		Limit(100).Documents(ctx)
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		data := doc.Data()
		t := SyncTask{
			TaskID:  doc.Ref.ID,
			Type:    "import",
			Channel: getString(data, "channel"),
			Source:  "Marketplace Import",
			Status:  mapStatus(getString(data, "status")),
			Ack:     getBool(data, "ack"),
		}
		if t.Channel != "" {
			t.Source = t.Channel + " Import"
		}
		if v, ok := data["processed"].(int64); ok {
			t.Progress = int(v)
		}
		if v, ok := data["total"].(int64); ok {
			t.Total = int(v)
		}
		if v, ok := data["created_at"].(time.Time); ok {
			t.StartedAt = v
		}
		if v, ok := data["updated_at"].(time.Time); ok {
			t.UpdatedAt = v
		}
		if msg := getString(data, "error"); msg != "" {
			t.Error = msg
		}
		tasks = append(tasks, t)
	}
	iter.Stop()

	// ── Order import jobs ─────────────────────────────────────────────────────
	iter2 := h.client.Collection("tenants").Doc(tenantID).Collection("order_import_jobs").
		Where("created_at", ">=", since).
		OrderBy("created_at", firestore.Desc).
		Limit(100).Documents(ctx)
	for {
		doc, err := iter2.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		data := doc.Data()
		t := SyncTask{
			TaskID:  doc.Ref.ID,
			Type:    "order_import",
			Channel: getString(data, "channel"),
			Source:  "Order Import",
			Status:  mapStatus(getString(data, "status")),
			Ack:     getBool(data, "ack"),
		}
		if t.Channel != "" {
			t.Source = t.Channel + " Orders"
		}
		if v, ok := data["orders_imported"].(int64); ok {
			t.Progress = int(v)
		}
		if v, ok := data["created_at"].(time.Time); ok {
			t.StartedAt = v
		}
		if v, ok := data["updated_at"].(time.Time); ok {
			t.UpdatedAt = v
		}
		if msg := getString(data, "error"); msg != "" {
			t.Error = msg
		}
		tasks = append(tasks, t)
	}
	iter2.Stop()

	// ── Automation executions ─────────────────────────────────────────────────
	iter3 := h.client.Collection("tenants").Doc(tenantID).Collection("automation_executions").
		Where("executed_at", ">=", since).
		OrderBy("executed_at", firestore.Desc).
		Limit(50).Documents(ctx)
	for {
		doc, err := iter3.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		data := doc.Data()
		t := SyncTask{
			TaskID:  doc.Ref.ID,
			Type:    "automation",
			Source:  "Automation Rule",
			Status:  mapStatus(getString(data, "result")),
			Ack:     getBool(data, "ack"),
		}
		if name := getString(data, "rule_name"); name != "" {
			t.Source = "Rule: " + name
		}
		if v, ok := data["executed_at"].(time.Time); ok {
			t.StartedAt = v
			t.UpdatedAt = v
		}
		if msg := getString(data, "error"); msg != "" {
			t.Error = msg
		}
		tasks = append(tasks, t)
	}
	iter3.Stop()

	// ── AI generation jobs ────────────────────────────────────────────────────
	iter4 := h.client.Collection("tenants").Doc(tenantID).Collection("ai_generation_jobs").
		Where("created_at", ">=", since).
		OrderBy("created_at", firestore.Desc).
		Limit(50).Documents(ctx)
	for {
		doc, err := iter4.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		data := doc.Data()
		t := SyncTask{
			TaskID:  doc.Ref.ID,
			Type:    "ai_generation",
			Source:  "AI Listing Generation",
			Status:  mapStatus(getString(data, "status")),
			Ack:     getBool(data, "ack"),
		}
		if v, ok := data["processed"].(int64); ok {
			t.Progress = int(v)
		}
		if v, ok := data["total"].(int64); ok {
			t.Total = int(v)
		}
		if v, ok := data["created_at"].(time.Time); ok {
			t.StartedAt = v
		}
		if v, ok := data["updated_at"].(time.Time); ok {
			t.UpdatedAt = v
		}
		tasks = append(tasks, t)
	}
	iter4.Stop()

	if tasks == nil {
		tasks = []SyncTask{}
	}

	// Compute summary counts
	processing, pending, errors := 0, 0, 0
	for _, t := range tasks {
		switch t.Status {
		case "running":
			processing++
		case "pending":
			pending++
		case "error":
			if !t.Ack {
				errors++
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"tasks":      tasks,
		"processing": processing,
		"pending":    pending,
		"errors":     errors,
	})
}

// ── GET /api/v1/sync/channel-status ──────────────────────────────────────────
// Returns per-credential sync status for Orders, Inventory, and Listings.
// Keyed by credential_id. States: syncing | pending | error | not_configured

type ChannelSyncState struct {
	State     string `json:"state"`      // syncing|pending|error|not_configured
	ErrorMsg  string `json:"error_msg,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type CredentialSyncStatus struct {
	CredentialID string          `json:"credential_id"`
	Orders       ChannelSyncState `json:"orders"`
	Inventory    ChannelSyncState `json:"inventory"`
	Listings     ChannelSyncState `json:"listings"`
}

func (h *SyncStatusHandler) GetChannelSyncStatus(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()
	since := time.Now().Add(-2 * time.Hour)

	// Map of credentialID -> status
	statusMap := map[string]*CredentialSyncStatus{}

	ensureEntry := func(credID string) {
		if _, ok := statusMap[credID]; !ok {
			statusMap[credID] = &CredentialSyncStatus{
				CredentialID: credID,
				Orders:       ChannelSyncState{State: "not_configured"},
				Inventory:    ChannelSyncState{State: "not_configured"},
				Listings:     ChannelSyncState{State: "not_configured"},
			}
		}
	}

	mapToState := func(status, errMsg string) ChannelSyncState {
		switch mapStatus(status) {
		case "running":
			return ChannelSyncState{State: "syncing"}
		case "pending":
			return ChannelSyncState{State: "pending"}
		case "error":
			return ChannelSyncState{State: "error", ErrorMsg: errMsg}
		case "completed":
			return ChannelSyncState{State: "syncing"} // recently completed = healthy
		default:
			return ChannelSyncState{State: "not_configured"}
		}
	}

	// Order import jobs → Orders sync status
	iter := h.client.Collection("tenants").Doc(tenantID).Collection("order_import_jobs").
		Where("created_at", ">=", since).
		OrderBy("created_at", firestore.Desc).
		Limit(200).Documents(ctx)
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		data := doc.Data()
		credID := getString(data, "credential_id")
		if credID == "" {
			continue
		}
		ensureEntry(credID)
		// Only set if not already set from a more recent job
		if statusMap[credID].Orders.State == "not_configured" {
			statusMap[credID].Orders = mapToState(getString(data, "status"), getString(data, "error"))
		}
	}
	iter.Stop()

	// Marketplace import jobs → Listings sync status
	iter2 := h.client.Collection("tenants").Doc(tenantID).Collection("marketplace_import_jobs").
		Where("created_at", ">=", since).
		OrderBy("created_at", firestore.Desc).
		Limit(200).Documents(ctx)
	for {
		doc, err := iter2.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		data := doc.Data()
		credID := getString(data, "credential_id")
		if credID == "" {
			continue
		}
		ensureEntry(credID)
		if statusMap[credID].Listings.State == "not_configured" {
			statusMap[credID].Listings = mapToState(getString(data, "status"), getString(data, "error"))
		}
	}
	iter2.Stop()

	// Inventory sync logs → Inventory sync status
	iter3 := h.client.Collection("tenants").Doc(tenantID).Collection("inventory_sync_log").
		Where("created_at", ">=", since).
		OrderBy("created_at", firestore.Desc).
		Limit(200).Documents(ctx)
	for {
		doc, err := iter3.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		data := doc.Data()
		credID := getString(data, "credential_id")
		if credID == "" {
			continue
		}
		ensureEntry(credID)
		if statusMap[credID].Inventory.State == "not_configured" {
			statusMap[credID].Inventory = mapToState(getString(data, "status"), getString(data, "error"))
		}
	}
	iter3.Stop()

	// Convert map to slice
	result := make([]CredentialSyncStatus, 0, len(statusMap))
	for _, v := range statusMap {
		result = append(result, *v)
	}

	c.JSON(http.StatusOK, gin.H{"data": result})
}

// ── POST /api/v1/sync/errors/clear ───────────────────────────────────────────

func (h *SyncStatusHandler) ClearErrors(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()

	collections := []string{
		"marketplace_import_jobs",
		"order_import_jobs",
		"automation_executions",
		"ai_generation_jobs",
	}

	cleared := 0
	for _, col := range collections {
		iter := h.client.Collection("tenants").Doc(tenantID).Collection(col).
			Where("status", "==", "failed").
			Limit(200).Documents(ctx)
		for {
			doc, err := iter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				break
			}
			doc.Ref.Update(ctx, []firestore.Update{{Path: "ack", Value: true}})
			cleared++
		}
		iter.Stop()
	}

	c.JSON(http.StatusOK, gin.H{"cleared": cleared})
}

// ── helpers ───────────────────────────────────────────────────────────────────

func getString(data map[string]interface{}, key string) string {
	if v, ok := data[key].(string); ok {
		return v
	}
	return ""
}

func getBool(data map[string]interface{}, key string) bool {
	if v, ok := data[key].(bool); ok {
		return v
	}
	return false
}

func mapStatus(raw string) string {
	switch raw {
	case "running", "in_progress", "processing":
		return "running"
	case "pending", "queued", "waiting":
		return "pending"
	case "completed", "done", "success":
		return "completed"
	case "failed", "error", "errored":
		return "error"
	default:
		return raw
	}
}
