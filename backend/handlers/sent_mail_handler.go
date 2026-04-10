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
// SENT MAIL HANDLER
//
// Routes:
//   GET /api/v1/sent-mail   Paginated list of sent mail log entries
//
// Firestore collection: tenants/{tenantId}/sent_mail_log
//
// Query params: order_id, recipient, status, date_from, date_to, limit, offset
// ============================================================================

type SentMailHandler struct {
	client *firestore.Client
}

func NewSentMailHandler(client *firestore.Client) *SentMailHandler {
	return &SentMailHandler{client: client}
}

type SentMailLogEntry struct {
	ID           string    `firestore:"id"            json:"id"`
	OrderID      string    `firestore:"order_id"      json:"order_id"`
	TemplateID   string    `firestore:"template_id"   json:"template_id"`
	TemplateName string    `firestore:"template_name" json:"template_name"`
	Recipient    string    `firestore:"recipient"     json:"recipient"`
	Subject      string    `firestore:"subject"       json:"subject"`
	Status       string    `firestore:"status"        json:"status"` // sent | failed | pending
	ErrorMessage string    `firestore:"error_message" json:"error_message"`
	SentAt       time.Time `firestore:"sent_at"       json:"sent_at"`
}

func (h *SentMailHandler) col(tenantID string) *firestore.CollectionRef {
	return h.client.Collection("tenants").Doc(tenantID).Collection("sent_mail_log")
}

// ListSentMail GET /api/v1/sent-mail
// Query params: order_id, recipient, status, date_from (RFC3339 date), date_to (RFC3339 date),
//
//	limit (default 50), offset (default 0)
func (h *SentMailHandler) ListSentMail(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	filterStatus := c.Query("status")
	filterOrderID := c.Query("order_id")
	filterRecipient := c.Query("recipient")
	dateFrom := c.Query("date_from")
	dateTo := c.Query("date_to")

	// Always use the base ordered query and filter in-memory.
	// This avoids composite index requirements and ensures all filter combinations work correctly.
	q := h.col(tenantID).OrderBy("sent_at", firestore.Desc).Limit(1000)

	iter := q.Documents(ctx)
	defer iter.Stop()

	var fromTime, toTime time.Time
	if dateFrom != "" {
		fromTime, _ = time.Parse("2006-01-02", dateFrom)
	}
	if dateTo != "" {
		toTime, _ = time.Parse("2006-01-02", dateTo)
		if !toTime.IsZero() {
			// inclusive end-of-day
			toTime = toTime.Add(24*time.Hour - time.Second)
		}
	}

	var all []SentMailLogEntry
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list sent mail"})
			return
		}
		var entry SentMailLogEntry
		if err := doc.DataTo(&entry); err != nil {
			continue
		}

		// In-memory filters
		if filterStatus != "" && entry.Status != filterStatus {
			continue
		}
		if filterOrderID != "" && entry.OrderID != filterOrderID {
			continue
		}
		if filterRecipient != "" {
			if !containsCI(entry.Recipient, filterRecipient) {
				continue
			}
		}
		if !fromTime.IsZero() && entry.SentAt.Before(fromTime) {
			continue
		}
		if !toTime.IsZero() && entry.SentAt.After(toTime) {
			continue
		}

		all = append(all, entry)
	}

	total := len(all)
	start := offset
	end := offset + limit
	if start >= total {
		all = []SentMailLogEntry{}
	} else {
		if end > total {
			end = total
		}
		all = all[start:end]
	}
	if all == nil {
		all = []SentMailLogEntry{}
	}

	c.JSON(http.StatusOK, gin.H{
		"items": all,
		"total": total,
	})
}

// containsCI is a case-insensitive substring check.
func containsCI(s, substr string) bool {
	if substr == "" {
		return true
	}
	sl := len(s)
	subl := len(substr)
	if subl > sl {
		return false
	}
	for i := 0; i <= sl-subl; i++ {
		match := true
		for j := 0; j < subl; j++ {
			a, b := s[i+j], substr[j]
			if a >= 'A' && a <= 'Z' {
				a += 32
			}
			if b >= 'A' && b <= 'Z' {
				b += 32
			}
			if a != b {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
