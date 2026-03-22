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
// EMAIL TEMPLATE HANDLER
//
// Routes:
//   GET    /api/v1/email-templates         List all templates
//   POST   /api/v1/email-templates         Create a template
//   GET    /api/v1/email-templates/:id     Get a template
//   PUT    /api/v1/email-templates/:id     Update a template
//   DELETE /api/v1/email-templates/:id     Delete a template
//
// Firestore collection: email_templates
//
// EmailTemplate document schema:
//   id           string    — unique template ID (UUID)
//   tenant_id    string    — tenant scope
//   name         string    — template name (e.g. "Order Confirmation")
//   type         string    — "order_confirmation" | "despatch_notification" | "rma_update" | "low_stock_alert"
//   subject      string    — email subject (supports {{variable}} placeholders)
//   body         string    — email body HTML (supports {{variable}} placeholders)
//   variables    []string  — list of supported placeholder variables
//   active       bool      — whether this template is in use
//   created_at   time.Time
//   updated_at   time.Time
// ============================================================================

type EmailTemplateHandler struct {
	client *firestore.Client
}

func NewEmailTemplateHandler(client *firestore.Client) *EmailTemplateHandler {
	return &EmailTemplateHandler{client: client}
}

type EmailTemplate struct {
	ID        string    `firestore:"id"         json:"id"`
	TenantID  string    `firestore:"tenant_id"  json:"tenant_id"`
	Name      string    `firestore:"name"       json:"name"`
	Type      string    `firestore:"type"       json:"type"`
	Subject   string    `firestore:"subject"    json:"subject"`
	Body      string    `firestore:"body"       json:"body"`
	Variables []string  `firestore:"variables"  json:"variables"`
	Active    bool      `firestore:"active"     json:"active"`
	CreatedAt time.Time `firestore:"created_at" json:"created_at"`
	UpdatedAt time.Time `firestore:"updated_at" json:"updated_at"`
}

// defaultVariables returns the standard placeholder variables for each template type.
var defaultVariables = map[string][]string{
	"order_confirmation":    {"{{order_id}}", "{{customer_name}}", "{{order_date}}", "{{order_total}}", "{{items}}"},
	"despatch_notification": {"{{order_id}}", "{{tracking_number}}", "{{carrier}}", "{{customer_name}}", "{{estimated_delivery}}"},
	"rma_update":            {"{{order_id}}", "{{rma_id}}", "{{rma_status}}", "{{customer_name}}", "{{refund_amount}}"},
	"low_stock_alert":       {"{{sku}}", "{{product_title}}", "{{current_stock}}", "{{reorder_point}}"},
}

func (h *EmailTemplateHandler) col(tenantID string) *firestore.CollectionRef {
	return h.client.Collection("tenants").Doc(tenantID).Collection("email_templates")
}

// ListEmailTemplates GET /api/v1/email-templates
func (h *EmailTemplateHandler) ListEmailTemplates(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	iter := h.col(tenantID).
		OrderBy("created_at", firestore.Asc).
		Documents(ctx)
	defer iter.Stop()

	var templates []EmailTemplate
	for {
		snap, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list templates"})
			return
		}
		var t EmailTemplate
		snap.DataTo(&t)
		templates = append(templates, t)
	}
	if templates == nil {
		templates = []EmailTemplate{}
	}
	c.JSON(http.StatusOK, gin.H{"templates": templates, "total": len(templates)})
}

// CreateEmailTemplate POST /api/v1/email-templates
func (h *EmailTemplateHandler) CreateEmailTemplate(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	var req struct {
		Name    string `json:"name"    binding:"required"`
		Type    string `json:"type"    binding:"required"`
		Subject string `json:"subject" binding:"required"`
		Body    string `json:"body"    binding:"required"`
		Active  bool   `json:"active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	id := uuid.New().String()
	now := time.Now().UTC()
	vars := defaultVariables[req.Type]
	if vars == nil {
		vars = []string{}
	}

	tmpl := EmailTemplate{
		ID:        id,
		TenantID:  tenantID,
		Name:      req.Name,
		Type:      req.Type,
		Subject:   req.Subject,
		Body:      req.Body,
		Variables: vars,
		Active:    req.Active,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if _, err := h.col(tenantID).Doc(id).Set(ctx, tmpl); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create template"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"template": tmpl})
}

// GetEmailTemplate GET /api/v1/email-templates/:id
func (h *EmailTemplateHandler) GetEmailTemplate(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	tmplID := c.Param("id")
	ctx := c.Request.Context()

	snap, err := h.col(tenantID).Doc(tmplID).Get(ctx)
	if err != nil || !snap.Exists() {
		c.JSON(http.StatusNotFound, gin.H{"error": "template not found"})
		return
	}
	var t EmailTemplate
	snap.DataTo(&t)
	c.JSON(http.StatusOK, gin.H{"template": t})
}

// UpdateEmailTemplate PUT /api/v1/email-templates/:id
func (h *EmailTemplateHandler) UpdateEmailTemplate(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	tmplID := c.Param("id")
	ctx := c.Request.Context()

	var req struct {
		Name    string `json:"name"`
		Subject string `json:"subject"`
		Body    string `json:"body"`
		Active  *bool  `json:"active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	updates := []firestore.Update{
		{Path: "updated_at", Value: time.Now().UTC()},
	}
	if req.Name != "" {
		updates = append(updates, firestore.Update{Path: "name", Value: req.Name})
	}
	if req.Subject != "" {
		updates = append(updates, firestore.Update{Path: "subject", Value: req.Subject})
	}
	if req.Body != "" {
		updates = append(updates, firestore.Update{Path: "body", Value: req.Body})
	}
	if req.Active != nil {
		updates = append(updates, firestore.Update{Path: "active", Value: *req.Active})
	}

	if _, err := h.col(tenantID).Doc(tmplID).Update(ctx, updates); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update template"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "template updated"})
}

// DeleteEmailTemplate DELETE /api/v1/email-templates/:id
func (h *EmailTemplateHandler) DeleteEmailTemplate(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	tmplID := c.Param("id")
	ctx := c.Request.Context()

	if _, err := h.col(tenantID).Doc(tmplID).Delete(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete template"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "template deleted"})
}
