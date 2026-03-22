package handlers

import (
	"context"
	"net/http"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"
)

// ============================================================================
// NOTIFICATION HANDLER
//
// Routes:
//   GET  /api/v1/notifications           List recent system notifications
//   POST /api/v1/notifications/mark-read Mark one or all notifications read
//
// Firestore collection: notifications
//
// Notification document schema:
//   id         string    — unique notification ID (UUID)
//   tenant_id  string    — tenant this notification belongs to
//   type       string    — "sync_error" | "low_stock" | "automation_failure" | "system"
//   message    string    — human-readable message text
//   read       bool      — whether the notification has been read
//   created_at time.Time — when the notification was created
// ============================================================================

type NotificationHandler struct {
	client *firestore.Client
}

func NewNotificationHandler(client *firestore.Client) *NotificationHandler {
	return &NotificationHandler{client: client}
}

// SystemNotification mirrors the Firestore document schema above.
type SystemNotification struct {
	ID        string    `firestore:"id"         json:"id"`
	TenantID  string    `firestore:"tenant_id"  json:"tenant_id"`
	Type      string    `firestore:"type"       json:"type"`
	Message   string    `firestore:"message"    json:"message"`
	Read      bool      `firestore:"read"       json:"read"`
	CreatedAt time.Time `firestore:"created_at" json:"created_at"`
}

func (h *NotificationHandler) col(tenantID string) *firestore.CollectionRef {
	return h.client.Collection("notifications")
}

// GetNotifications GET /api/v1/notifications
// Returns the 50 most recent notifications for the current tenant.
func (h *NotificationHandler) GetNotifications(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	iter := h.col(tenantID).
		Where("tenant_id", "==", tenantID).
		OrderBy("created_at", firestore.Desc).
		Limit(50).
		Documents(ctx)
	defer iter.Stop()

	var notifications []SystemNotification
	unreadCount := 0

	for {
		snap, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list notifications"})
			return
		}
		var n SystemNotification
		if err := snap.DataTo(&n); err != nil {
			continue
		}
		notifications = append(notifications, n)
		if !n.Read {
			unreadCount++
		}
	}

	if notifications == nil {
		notifications = []SystemNotification{}
	}

	c.JSON(http.StatusOK, gin.H{
		"notifications": notifications,
		"unread_count":  unreadCount,
		"total":         len(notifications),
	})
}

// MarkNotificationsRead POST /api/v1/notifications/mark-read
// Body (JSON):
//
//	{ "id": "specific-id" }           — marks one notification read
//	{ "all": true }                    — marks all notifications read for tenant
func (h *NotificationHandler) MarkNotificationsRead(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	var req struct {
		ID  string `json:"id"`
		All bool   `json:"all"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.All {
		// Mark all unread notifications for this tenant as read
		iter := h.col(tenantID).
			Where("tenant_id", "==", tenantID).
			Where("read", "==", false).
			Documents(ctx)
		defer iter.Stop()

		batch := h.client.Batch()
		count := 0
		for {
			snap, err := iter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list notifications"})
				return
			}
			batch.Update(snap.Ref, []firestore.Update{
				{Path: "read", Value: true},
			})
			count++
		}
		if count > 0 {
			if _, err := batch.Commit(ctx); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to mark notifications read"})
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{"message": "all notifications marked read", "count": count})
		return
	}

	if req.ID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id or all required"})
		return
	}

	// Mark single notification read
	snap, err := h.col(tenantID).Doc(req.ID).Get(ctx)
	if err != nil || !snap.Exists() {
		c.JSON(http.StatusNotFound, gin.H{"error": "notification not found"})
		return
	}

	var n SystemNotification
	snap.DataTo(&n)
	if n.TenantID != tenantID {
		c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
		return
	}

	if _, err := h.col(tenantID).Doc(req.ID).Update(ctx, []firestore.Update{
		{Path: "read", Value: true},
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to mark notification read"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "notification marked read"})
}

// CreateNotification is an internal helper called by other handlers (e.g. sync, automation)
// to insert a system notification. Not exposed as an HTTP route.
func (h *NotificationHandler) CreateNotification(tenantID, notifType, message string) {
	ctx := context.Background()
	id := uuid.New().String()
	n := SystemNotification{
		ID:        id,
		TenantID:  tenantID,
		Type:      notifType,
		Message:   message,
		Read:      false,
		CreatedAt: time.Now().UTC(),
	}
	_, _ = h.col(tenantID).Doc(id).Set(ctx, n)
}
