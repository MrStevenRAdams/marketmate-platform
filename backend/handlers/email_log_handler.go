package handlers

import (
	"net/http"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"google.golang.org/api/iterator"
)

// ============================================================================
// EMAIL LOG HANDLER
//
// Routes:
//   GET /api/v1/email-logs   List sent email logs for the current tenant
//
// Firestore collection: email_logs
//
// EmailLog document schema:
//   id            string    — unique log entry ID
//   tenant_id     string    — tenant scope
//   template_id   string    — which template was used
//   template_type string    — template type (for display without join)
//   recipient     string    — destination email address
//   subject       string    — resolved subject line
//   status        string    — "sent" | "failed"
//   error         string    — error message if status == "failed"
//   order_id      string    — related order ID (if applicable)
//   sent_at       time.Time — when the email was sent/attempted
// ============================================================================

type EmailLogHandler struct {
	client *firestore.Client
}

func NewEmailLogHandler(client *firestore.Client) *EmailLogHandler {
	return &EmailLogHandler{client: client}
}

type EmailLog struct {
	ID           string    `firestore:"id"            json:"id"`
	TenantID     string    `firestore:"tenant_id"     json:"tenant_id"`
	TemplateID   string    `firestore:"template_id"   json:"template_id"`
	TemplateType string    `firestore:"template_type" json:"template_type"`
	Recipient    string    `firestore:"recipient"     json:"recipient"`
	Subject      string    `firestore:"subject"       json:"subject"`
	Status       string    `firestore:"status"        json:"status"` // sent | failed
	Error        string    `firestore:"error,omitempty" json:"error,omitempty"`
	OrderID      string    `firestore:"order_id,omitempty" json:"order_id,omitempty"`
	SentAt       time.Time `firestore:"sent_at"       json:"sent_at"`
}

func (h *EmailLogHandler) col(tenantID string) *firestore.CollectionRef {
	return h.client.Collection("tenants").Doc(tenantID).Collection("email_logs")
}

// ListEmailLogs GET /api/v1/email-logs
// Query params: limit (default 50), status ("sent" | "failed"), template_type
func (h *EmailLogHandler) ListEmailLogs(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	q := h.col(tenantID).
		OrderBy("sent_at", firestore.Desc).
		Limit(100)

	statusFilter := c.Query("status")
	if statusFilter != "" {
		q = h.col(tenantID).
			Where("status", "==", statusFilter).
			OrderBy("sent_at", firestore.Desc).
			Limit(100)
	}

	iter := q.Documents(ctx)
	defer iter.Stop()

	var logs []EmailLog
	for {
		snap, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list email logs"})
			return
		}
		var l EmailLog
		snap.DataTo(&l)
		logs = append(logs, l)
	}
	if logs == nil {
		logs = []EmailLog{}
	}
	c.JSON(http.StatusOK, gin.H{"logs": logs, "total": len(logs)})
}
