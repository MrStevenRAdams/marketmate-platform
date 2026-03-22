package handlers

import (
	"net/http"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"
)

// ============================================================================
// SCHEDULE HANDLER
//
// Routes:
//   GET    /api/v1/schedules        List schedules
//   POST   /api/v1/schedules        Create schedule
//   PUT    /api/v1/schedules/:id    Update schedule
//   DELETE /api/v1/schedules/:id    Delete schedule
// ============================================================================

type ScheduleHandler struct {
	client *firestore.Client
}

func NewScheduleHandler(client *firestore.Client) *ScheduleHandler {
	return &ScheduleHandler{client: client}
}

type Schedule struct {
	ID              string    `firestore:"id"               json:"id"`
	TenantID        string    `firestore:"tenant_id"        json:"tenant_id"`
	Name            string    `firestore:"name"             json:"name"`
	ScheduleType    string    `firestore:"schedule_type"    json:"schedule_type"` // one_time|daily|weekly|monthly|interval
	Timezone        string    `firestore:"timezone"         json:"timezone"`
	DaysOfWeek      []int     `firestore:"days_of_week"     json:"days_of_week,omitempty"`
	TimeOfDay       string    `firestore:"time_of_day"      json:"time_of_day,omitempty"` // HH:MM
	IntervalMinutes int       `firestore:"interval_minutes" json:"interval_minutes,omitempty"`
	RunAt           string    `firestore:"run_at"           json:"run_at,omitempty"` // ISO8601 for one_time
	Enabled         bool      `firestore:"enabled"          json:"enabled"`
	Action          string    `firestore:"action"           json:"action"`
	CreatedAt       time.Time `firestore:"created_at"       json:"created_at"`
	UpdatedAt       time.Time `firestore:"updated_at"       json:"updated_at"`
}

func (h *ScheduleHandler) col(tenantID string) *firestore.CollectionRef {
	return h.client.Collection("tenants").Doc(tenantID).Collection("schedules")
}

// GET /api/v1/schedules
func (h *ScheduleHandler) ListSchedules(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()

	var list []Schedule
	iter := h.col(tenantID).OrderBy("name", firestore.Asc).Documents(ctx)
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list schedules"})
			return
		}
		var s Schedule
		doc.DataTo(&s)
		list = append(list, s)
	}
	if list == nil {
		list = []Schedule{}
	}
	c.JSON(http.StatusOK, gin.H{"schedules": list})
}

// POST /api/v1/schedules
func (h *ScheduleHandler) CreateSchedule(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()

	var req struct {
		Name            string `json:"name" binding:"required"`
		ScheduleType    string `json:"schedule_type" binding:"required"`
		Timezone        string `json:"timezone"`
		DaysOfWeek      []int  `json:"days_of_week"`
		TimeOfDay       string `json:"time_of_day"`
		IntervalMinutes int    `json:"interval_minutes"`
		RunAt           string `json:"run_at"`
		Enabled         bool   `json:"enabled"`
		Action          string `json:"action"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	now := time.Now()
	s := Schedule{
		ID:              "sched_" + uuid.New().String(),
		TenantID:        tenantID,
		Name:            req.Name,
		ScheduleType:    req.ScheduleType,
		Timezone:        req.Timezone,
		DaysOfWeek:      req.DaysOfWeek,
		TimeOfDay:       req.TimeOfDay,
		IntervalMinutes: req.IntervalMinutes,
		RunAt:           req.RunAt,
		Enabled:         req.Enabled,
		Action:          req.Action,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if s.Timezone == "" {
		s.Timezone = "UTC"
	}
	if s.DaysOfWeek == nil {
		s.DaysOfWeek = []int{}
	}

	if _, err := h.col(tenantID).Doc(s.ID).Set(ctx, s); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create schedule"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"schedule": s})
}

// PUT /api/v1/schedules/:id
func (h *ScheduleHandler) UpdateSchedule(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	id := c.Param("id")
	ctx := c.Request.Context()

	doc, err := h.col(tenantID).Doc(id).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "schedule not found"})
		return
	}
	var s Schedule
	doc.DataTo(&s)

	var req struct {
		Name            *string `json:"name"`
		ScheduleType    *string `json:"schedule_type"`
		Timezone        *string `json:"timezone"`
		DaysOfWeek      []int   `json:"days_of_week"`
		TimeOfDay       *string `json:"time_of_day"`
		IntervalMinutes *int    `json:"interval_minutes"`
		RunAt           *string `json:"run_at"`
		Enabled         *bool   `json:"enabled"`
		Action          *string `json:"action"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Name != nil { s.Name = *req.Name }
	if req.ScheduleType != nil { s.ScheduleType = *req.ScheduleType }
	if req.Timezone != nil { s.Timezone = *req.Timezone }
	if req.DaysOfWeek != nil { s.DaysOfWeek = req.DaysOfWeek }
	if req.TimeOfDay != nil { s.TimeOfDay = *req.TimeOfDay }
	if req.IntervalMinutes != nil { s.IntervalMinutes = *req.IntervalMinutes }
	if req.RunAt != nil { s.RunAt = *req.RunAt }
	if req.Enabled != nil { s.Enabled = *req.Enabled }
	if req.Action != nil { s.Action = *req.Action }
	s.UpdatedAt = time.Now()

	if _, err := h.col(tenantID).Doc(id).Set(ctx, s); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update schedule"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"schedule": s})
}

// DELETE /api/v1/schedules/:id
func (h *ScheduleHandler) DeleteSchedule(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	id := c.Param("id")
	ctx := c.Request.Context()

	if _, err := h.col(tenantID).Doc(id).Delete(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete schedule"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}
