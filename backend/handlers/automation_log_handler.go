package handlers

import (
	"net/http"
	"strconv"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"google.golang.org/api/iterator"
)

// ============================================================================
// AUTOMATION LOG HANDLER — P0.3
// ============================================================================
// Paginated, filterable log of all background tasks.
// Aggregates from: marketplace_import_jobs, order_import_jobs,
//                  automation_executions, ai_generation_jobs
// ============================================================================

type AutomationLogHandler struct {
	client *firestore.Client
}

func NewAutomationLogHandler(client *firestore.Client) *AutomationLogHandler {
	return &AutomationLogHandler{client: client}
}

type AutomationLog struct {
	LogID            string    `json:"log_id"`
	RuleID           string    `json:"rule_id,omitempty"` // populated for automation_executions
	Type             string    `json:"type"`    // import|order_import|automation|ai_generation
	Channel          string    `json:"channel"` // amazon|ebay|temu|manual
	Source           string    `json:"source"`  // human-readable
	Status           string    `json:"status"`  // running|completed|error|cancelled
	StartedAt        time.Time `json:"started_at"`
	CompletedAt      time.Time `json:"completed_at,omitempty"`
	DurationSeconds  float64   `json:"duration_seconds"`
	RecordsProcessed int       `json:"records_processed"`
	ErrorMessage     string    `json:"error_message,omitempty"`
	Ack              bool      `json:"ack"`
	CollectionSource string    `json:"_collection,omitempty"` // internal — used for clear
}

// ── GET /api/v1/automation-logs ───────────────────────────────────────────────
// Query params: period (today|7d|30d|90d), type, status, page, page_size
func (h *AutomationLogHandler) List(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()

	// ── Parse filters ─────────────────────────────────────────────────────────
	period := c.DefaultQuery("period", "7d")
	filterType := c.DefaultQuery("type", "all")
	filterStatus := c.DefaultQuery("status", "all")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}

	since := periodToTime(period)

	// ── Collection definitions ────────────────────────────────────────────────
	type collectionDef struct {
		name      string
		logType   string
		timeField string
		toLog     func(id string, data map[string]interface{}) AutomationLog
	}
	collections := []collectionDef{
		{
			name:      "marketplace_import_jobs",
			logType:   "import",
			timeField: "created_at",
			toLog: func(id string, data map[string]interface{}) AutomationLog {
				l := AutomationLog{
					LogID:   id,
					Type:    "import",
					Channel: getString(data, "channel"),
					Source:  "Marketplace Import",
					Status:  mapLogStatus(getString(data, "status")),
					Ack:     getBool(data, "ack"),
				}
				if l.Channel != "" {
					l.Source = ucFirst(l.Channel) + " Import"
				}
				if v, ok := data["processed"].(int64); ok {
					l.RecordsProcessed = int(v)
				}
				l.StartedAt = getTime(data, "created_at")
				l.CompletedAt = getTime(data, "updated_at")
				l.DurationSeconds = durationSecs(l.StartedAt, l.CompletedAt)
				l.ErrorMessage = getString(data, "error")
				l.CollectionSource = "marketplace_import_jobs"
				return l
			},
		},
		{
			name:      "order_import_jobs",
			logType:   "order_import",
			timeField: "created_at",
			toLog: func(id string, data map[string]interface{}) AutomationLog {
				l := AutomationLog{
					LogID:   id,
					Type:    "order_import",
					Channel: getString(data, "channel"),
					Source:  "Order Import",
					Status:  mapLogStatus(getString(data, "status")),
					Ack:     getBool(data, "ack"),
				}
				if l.Channel != "" {
					l.Source = ucFirst(l.Channel) + " Orders"
				}
				if v, ok := data["orders_imported"].(int64); ok {
					l.RecordsProcessed = int(v)
				}
				l.StartedAt = getTime(data, "created_at")
				l.CompletedAt = getTime(data, "updated_at")
				l.DurationSeconds = durationSecs(l.StartedAt, l.CompletedAt)
				l.ErrorMessage = getString(data, "error")
				l.CollectionSource = "order_import_jobs"
				return l
			},
		},
		{
			name:      "automation_executions",
			logType:   "automation",
			timeField: "executed_at",
			toLog: func(id string, data map[string]interface{}) AutomationLog {
				l := AutomationLog{
					LogID:   id,
					Type:    "automation",
					Source:  "Automation Rule",
					Status:  mapLogStatus(getString(data, "result")),
					Ack:     getBool(data, "ack"),
				}
				if name := getString(data, "rule_name"); name != "" {
					l.Source = "Rule: " + name
				}
				l.StartedAt = getTime(data, "executed_at")
				l.CompletedAt = l.StartedAt
				l.RuleID = getString(data, "rule_id")
				l.ErrorMessage = getString(data, "error")
				l.CollectionSource = "automation_executions"
				return l
			},
		},
		{
			name:      "ai_generation_jobs",
			logType:   "ai_generation",
			timeField: "created_at",
			toLog: func(id string, data map[string]interface{}) AutomationLog {
				l := AutomationLog{
					LogID:   id,
					Type:    "ai_generation",
					Source:  "AI Listing Generation",
					Status:  mapLogStatus(getString(data, "status")),
					Ack:     getBool(data, "ack"),
				}
				if v, ok := data["processed"].(int64); ok {
					l.RecordsProcessed = int(v)
				}
				l.StartedAt = getTime(data, "created_at")
				l.CompletedAt = getTime(data, "updated_at")
				l.DurationSeconds = durationSecs(l.StartedAt, l.CompletedAt)
				l.ErrorMessage = getString(data, "error")
				l.CollectionSource = "ai_generation_jobs"
				return l
			},
		},
		{
			name:      "sent_mail_log",
			logType:   "email",
			timeField: "sent_at",
			toLog: func(id string, data map[string]interface{}) AutomationLog {
				l := AutomationLog{
					LogID:  id,
					Type:   "email",
					Source: "Email",
					Ack:    getBool(data, "ack"),
				}
				if tmpl := getString(data, "template_name"); tmpl != "" {
					l.Source = "Email: " + tmpl
				}
				rawStatus := getString(data, "status")
				switch rawStatus {
				case "sent":
					l.Status = "completed"
				case "failed":
					l.Status = "error"
				default:
					l.Status = mapLogStatus(rawStatus)
				}
				l.StartedAt = getTime(data, "sent_at")
				l.CompletedAt = l.StartedAt
				l.ErrorMessage = getString(data, "error_message")
				l.CollectionSource = "sent_mail_log"
				return l
			},
		},
		{
			name:      "export_jobs",
			logType:   "export",
			timeField: "created_at",
			toLog: func(id string, data map[string]interface{}) AutomationLog {
				l := AutomationLog{
					LogID:  id,
					Type:   "export",
					Source: "Export",
					Status: mapLogStatus(getString(data, "status")),
					Ack:    getBool(data, "ack"),
				}
				if t := getString(data, "export_type"); t != "" {
					l.Source = ucFirst(t) + " Export"
				}
				l.StartedAt = getTime(data, "created_at")
				l.CompletedAt = getTime(data, "updated_at")
				l.DurationSeconds = durationSecs(l.StartedAt, l.CompletedAt)
				l.ErrorMessage = getString(data, "error")
				l.CollectionSource = "export_jobs"
				return l
			},
		},
		{
			name:      "channel_sync_jobs",
			logType:   "channel_sync",
			timeField: "created_at",
			toLog: func(id string, data map[string]interface{}) AutomationLog {
				l := AutomationLog{
					LogID:   id,
					Type:    "channel_sync",
					Channel: getString(data, "channel"),
					Source:  "Channel Sync",
					Status:  mapLogStatus(getString(data, "status")),
					Ack:     getBool(data, "ack"),
				}
				if l.Channel != "" {
					l.Source = ucFirst(l.Channel) + " Sync"
				}
				l.StartedAt = getTime(data, "created_at")
				l.CompletedAt = getTime(data, "updated_at")
				l.DurationSeconds = durationSecs(l.StartedAt, l.CompletedAt)
				l.ErrorMessage = getString(data, "error")
				l.CollectionSource = "channel_sync_jobs"
				return l
			},
		},
		{
			name:      "listing_update_jobs",
			logType:   "listing_updates",
			timeField: "created_at",
			toLog: func(id string, data map[string]interface{}) AutomationLog {
				l := AutomationLog{
					LogID:   id,
					Type:    "listing_updates",
					Channel: getString(data, "channel"),
					Source:  "Listing Updates",
					Status:  mapLogStatus(getString(data, "status")),
					Ack:     getBool(data, "ack"),
				}
				if l.Channel != "" {
					l.Source = ucFirst(l.Channel) + " Listings"
				}
				l.StartedAt = getTime(data, "created_at")
				l.CompletedAt = getTime(data, "updated_at")
				l.DurationSeconds = durationSecs(l.StartedAt, l.CompletedAt)
				l.ErrorMessage = getString(data, "error")
				l.CollectionSource = "listing_update_jobs"
				return l
			},
		},
	}

	// ── Collect logs from all relevant collections ─────────────────────────────
	filterRuleID := c.Query("rule_id") // filter by automation rule ID

	var allLogs []AutomationLog
	for _, col := range collections {
		if filterType != "all" && col.logType != filterType {
			continue
		}
		iter := h.client.Collection("tenants").Doc(tenantID).Collection(col.name).
			Where(col.timeField, ">=", since).
			OrderBy(col.timeField, firestore.Desc).
			Limit(500).Documents(ctx)
		for {
			doc, err := iter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				break
			}
			l := col.toLog(doc.Ref.ID, doc.Data())
			// Apply status filter
			if filterStatus != "all" && l.Status != filterStatus {
				continue
			}
			// Apply rule_id filter (automation_executions only)
			if filterRuleID != "" && col.name == "automation_executions" && l.RuleID != filterRuleID {
				continue
			}
			allLogs = append(allLogs, l)
		}
		iter.Stop()
	}

	// Sort by started_at desc (already roughly sorted per collection, but merge)
	// Simple insertion of sorted slices — good enough for <500 items
	sortLogsByTime(allLogs)

	total := len(allLogs)
	start := (page - 1) * pageSize
	end := start + pageSize
	if start >= total {
		allLogs = []AutomationLog{}
	} else {
		if end > total {
			end = total
		}
		allLogs = allLogs[start:end]
	}

	c.JSON(http.StatusOK, gin.H{
		"logs":  allLogs,
		"total": total,
		"page":  page,
	})
}

// ── POST /api/v1/automation-logs/clear ────────────────────────────────────────
// Marks all error/failed tasks as acknowledged.
func (h *AutomationLogHandler) Clear(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()

	cols := []struct {
		name      string
		statusVal string
	}{
		{"marketplace_import_jobs", "failed"},
		{"order_import_jobs", "failed"},
		{"automation_executions", "failed"},
		{"ai_generation_jobs", "failed"},
		{"sent_mail_log", "failed"},
	}

	cleared := 0
	for _, col := range cols {
		iter := h.client.Collection("tenants").Doc(tenantID).Collection(col.name).
			Where("status", "==", col.statusVal).
			Where("ack", "==", false).
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

func periodToTime(period string) time.Time {
	now := time.Now()
	switch period {
	case "today":
		y, m, d := now.Date()
		return time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	case "30d":
		return now.Add(-30 * 24 * time.Hour)
	case "90d":
		return now.Add(-90 * 24 * time.Hour)
	default: // 7d
		return now.Add(-7 * 24 * time.Hour)
	}
}

func mapLogStatus(s string) string {
	switch s {
	case "running", "processing", "in_progress":
		return "running"
	case "queued", "pending":
		return "pending"
	case "completed", "done", "success", "success_with_errors":
		return "completed"
	case "failed", "error":
		return "error"
	case "cancelled", "canceled":
		return "cancelled"
	default:
		if s == "" {
			return "pending"
		}
		return s
	}
}

func getTime(data map[string]interface{}, key string) time.Time {
	if v, ok := data[key].(time.Time); ok {
		return v
	}
	return time.Time{}
}

func durationSecs(start, end time.Time) float64 {
	if start.IsZero() || end.IsZero() || end.Before(start) {
		return 0
	}
	return end.Sub(start).Seconds()
}

func ucFirst(s string) string {
	if len(s) == 0 {
		return s
	}
	return string([]byte{s[0] - 32}) + s[1:]
}

func sortLogsByTime(logs []AutomationLog) {
	// Simple bubble sort — log lists are small
	for i := 0; i < len(logs); i++ {
		for j := i + 1; j < len(logs); j++ {
			if logs[j].StartedAt.After(logs[i].StartedAt) {
				logs[i], logs[j] = logs[j], logs[i]
			}
		}
	}
}

// Retry re-queues a failed automation execution log entry for re-processing.
// It resets the status to "pending" so the automation engine picks it up again.
func (h *AutomationLogHandler) Retry(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	logID := c.Param("id")
	ctx := c.Request.Context()

	if logID == "" {
		c.JSON(400, gin.H{"error": "log id required"})
		return
	}

	// Find the log entry across known collections
	collections := []string{"automation_executions", "marketplace_import_jobs", "order_import_jobs"}
	for _, col := range collections {
		ref := h.client.Collection("tenants").Doc(tenantID).Collection(col).Doc(logID)
		doc, err := ref.Get(ctx)
		if err != nil || !doc.Exists() {
			continue
		}

		_, err = ref.Update(ctx, []firestore.Update{
			{Path: "status", Value: "pending"},
			{Path: "retried_at", Value: time.Now().Format(time.RFC3339)},
			{Path: "error", Value: ""},
		})
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"ok": true, "collection": col, "id": logID})
		return
	}

	c.JSON(404, gin.H{"error": "log entry not found"})
}
