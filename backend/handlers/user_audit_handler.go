package handlers

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"

	"module-a/models"
)

// ============================================================================
// USER AUDIT LOG HANDLER — Session 6
// ============================================================================
// GET /api/v1/user-audit-log
//    Query: event_type, actor_uid, target_uid, date_from, date_to, limit, offset
// ============================================================================

type UserAuditHandler struct {
	client *firestore.Client
}

func NewUserAuditHandler(client *firestore.Client) *UserAuditHandler {
	return &UserAuditHandler{client: client}
}

func (h *UserAuditHandler) auditCol(tenantID string) *firestore.CollectionRef {
	return h.client.Collection("tenants").Doc(tenantID).Collection("user_audit_log")
}

// ListUserAuditLog GET /api/v1/user-audit-log
func (h *UserAuditHandler) ListUserAuditLog(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	callerRole := models.Role(c.GetString("role"))
	if !callerRole.Can("manage_users") {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin or owner required"})
		return
	}

	ctx := c.Request.Context()

	// Parse query params
	eventType := c.Query("event_type")
	actorUID := c.Query("actor_uid")
	targetUID := c.Query("target_uid")
	dateFrom := c.Query("date_from")
	dateTo := c.Query("date_to")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if limit < 1 || limit > 200 {
		limit = 50
	}

	q := h.auditCol(tenantID).OrderBy("created_at", firestore.Desc)

	if eventType != "" {
		q = h.auditCol(tenantID).Where("event_type", "==", eventType).OrderBy("created_at", firestore.Desc)
	} else if actorUID != "" {
		q = h.auditCol(tenantID).Where("actor_uid", "==", actorUID).OrderBy("created_at", firestore.Desc)
	} else if targetUID != "" {
		q = h.auditCol(tenantID).Where("target_uid", "==", targetUID).OrderBy("created_at", firestore.Desc)
	}

	if dateFrom != "" {
		if from, err := parseFlexDate(dateFrom); err == nil {
			q = q.Where("created_at", ">=", from)
		}
	}
	if dateTo != "" {
		if to, err := parseFlexDate(dateTo); err == nil {
			q = q.Where("created_at", "<=", to)
		}
	}

	iter := q.Documents(ctx)
	defer iter.Stop()

	var events []models.UserAuditEvent
	idx := 0
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list audit log"})
			return
		}
		if idx < offset {
			idx++
			continue
		}
		if len(events) >= limit {
			break
		}
		var ev models.UserAuditEvent
		if err := doc.DataTo(&ev); err != nil {
			continue
		}
		events = append(events, ev)
		idx++
	}

	if events == nil {
		events = []models.UserAuditEvent{}
	}

	c.JSON(http.StatusOK, gin.H{
		"events": events,
		"count":  len(events),
		"offset": offset,
		"limit":  limit,
	})
}

// WriteUserAuditEvent writes a user management audit event to Firestore.
// Call this from any handler that performs user management actions.
func WriteUserAuditEvent(
	client *firestore.Client,
	tenantID string,
	actorUID string,
	actorEmail string,
	eventType string,
	targetUID string,
	targetEmail string,
	metadata map[string]interface{},
) {
	ctx := context.Background()
	eventID := "uae_" + uuid.New().String()
	ev := models.UserAuditEvent{
		ID:          eventID,
		TenantID:    tenantID,
		ActorUID:    actorUID,
		ActorEmail:  actorEmail,
		EventType:   models.UserAuditEventType(eventType),
		TargetUID:   targetUID,
		TargetEmail: targetEmail,
		Metadata:    metadata,
		CreatedAt:   time.Now().UTC(),
	}
	if _, err := client.Collection("tenants").Doc(tenantID).
		Collection("user_audit_log").Doc(eventID).Set(ctx, ev); err != nil {
		log.Printf("[UserAudit] failed to write event %s: %v", eventType, err)
	}
}

// UpdateLastLogin updates GlobalUser.LastLoginAt for the given user.
func UpdateLastLogin(client *firestore.Client, userID string) {
	ctx := context.Background()
	if _, err := client.Collection("global_users").Doc(userID).Update(ctx, []firestore.Update{
		{Path: "last_login_at", Value: time.Now().UTC()},
	}); err != nil {
		log.Printf("[UserAudit] failed to update last_login_at for %s: %v", userID, err)
	}
}
