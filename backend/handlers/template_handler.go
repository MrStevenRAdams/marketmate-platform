package handlers

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"module-a/models"
	"module-a/services"
)

// ============================================================================
// TEMPLATE HANDLER — Module L (Pagebuilder)
// ============================================================================
// Endpoints:
//   GET    /templates                         — list all templates (filter by ?type=)
//   POST   /templates                         — create / save template
//   GET    /templates/:id                     — load a template
//   PUT    /templates/:id                     — update template
//   DELETE /templates/:id                     — delete template
//   POST   /templates/:id/default             — mark as default for its type
//   GET    /templates/default/:type           — get default template for a type
//   POST   /templates/:id/render              — render with real order data → HTML
//   POST   /templates/:id/send               — render + send via email
//   GET    /settings/seller                   — get seller profile
//   PUT    /settings/seller                   — update seller profile
//   POST   /templates/ai/generate-text        — proxy AI text generation
// ============================================================================

type TemplateHandler struct {
	templateSvc *services.TemplateService
	orderSvc    *services.OrderService
	aiSvc       *services.AIService
	fsClient    *firestore.Client
}

func NewTemplateHandler(
	templateSvc *services.TemplateService,
	orderSvc *services.OrderService,
	aiSvc *services.AIService,
) *TemplateHandler {
	return &TemplateHandler{
		templateSvc: templateSvc,
		orderSvc:    orderSvc,
		aiSvc:       aiSvc,
	}
}

func NewTemplateHandlerWithClient(
	templateSvc *services.TemplateService,
	orderSvc *services.OrderService,
	aiSvc *services.AIService,
	fsClient *firestore.Client,
) *TemplateHandler {
	return &TemplateHandler{
		templateSvc: templateSvc,
		orderSvc:    orderSvc,
		aiSvc:       aiSvc,
		fsClient:    fsClient,
	}
}

// ============================================================================
// TEMPLATE CRUD
// ============================================================================

// ListTemplates GET /api/v1/templates
func (h *TemplateHandler) ListTemplates(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant ID required"})
		return
	}

	templateType := c.Query("type") // optional filter

	templates, err := h.templateSvc.ListTemplates(c.Request.Context(), tenantID, templateType)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"templates": templates,
		"total":     len(templates),
	})
}

// CreateTemplate POST /api/v1/templates
func (h *TemplateHandler) CreateTemplate(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant ID required"})
		return
	}

	var tpl models.Template
	if err := c.ShouldBindJSON(&tpl); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}

	if tpl.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "template name is required"})
		return
	}

	// Generate ID if not provided by client
	if tpl.TemplateID == "" {
		tpl.TemplateID = "tpl_" + uuid.New().String()[:12]
	}

	// Stamp version history entry
	tpl.History = append([]models.TemplateVersion{{
		Version:    tpl.Version,
		SavedAt:    time.Now().Format(time.RFC3339),
		BlockCount: 0, // client sends this
		SavedBy:    c.GetString("user_id"),
	}}, tpl.History...)

	if err := h.templateSvc.CreateTemplate(c.Request.Context(), tenantID, &tpl); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"template": tpl})
}

// GetTemplate GET /api/v1/templates/:id
func (h *TemplateHandler) GetTemplate(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	templateID := c.Param("id")

	tpl, err := h.templateSvc.GetTemplate(c.Request.Context(), tenantID, templateID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "template not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"template": tpl})
}

// UpdateTemplate PUT /api/v1/templates/:id
func (h *TemplateHandler) UpdateTemplate(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	templateID := c.Param("id")

	var tpl models.Template
	if err := c.ShouldBindJSON(&tpl); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}

	// Prepend new version entry to history
	newEntry := models.TemplateVersion{
		Version:    tpl.Version,
		SavedAt:    time.Now().Format(time.RFC3339),
		BlockCount: 0,
		SavedBy:    c.GetString("user_id"),
	}
	tpl.History = append([]models.TemplateVersion{newEntry}, tpl.History...)
	// Keep last 50 versions only
	if len(tpl.History) > 50 {
		tpl.History = tpl.History[:50]
	}

	if err := h.templateSvc.UpdateTemplate(c.Request.Context(), tenantID, templateID, &tpl); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"template": tpl})
}

// DeleteTemplate DELETE /api/v1/templates/:id
func (h *TemplateHandler) DeleteTemplate(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	templateID := c.Param("id")

	if err := h.templateSvc.DeleteTemplate(c.Request.Context(), tenantID, templateID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// SetDefault POST /api/v1/templates/:id/default
func (h *TemplateHandler) SetDefault(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	templateID := c.Param("id")

	if err := h.templateSvc.SetDefaultTemplate(c.Request.Context(), tenantID, templateID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ToggleTemplate PATCH /api/v1/templates/:id/toggle
func (h *TemplateHandler) ToggleTemplate(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant ID required"})
		return
	}
	templateID := c.Param("id")

	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.templateSvc.ToggleTemplate(c.Request.Context(), tenantID, templateID, body.Enabled); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"template_id": templateID, "enabled": body.Enabled})
}

// GetDefault GET /api/v1/templates/default/:type
func (h *TemplateHandler) GetDefault(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	templateType := models.TemplateType(c.Param("type"))

	tpl, err := h.templateSvc.GetDefaultTemplate(c.Request.Context(), tenantID, templateType)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("no default template for type %s", templateType)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"template": tpl})
}

// ============================================================================
// RENDERING
// ============================================================================

// RenderTemplate POST /api/v1/templates/:id/render
// Body: { "order_id": "...", "html_body": "..." (optional pre-rendered) }
// Returns: { "html": "...", "render_data": {...} }
func (h *TemplateHandler) RenderTemplate(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	templateID := c.Param("id")

	var req struct {
		OrderID        string `json:"order_id"`
		ShippingMethod string `json:"shipping_method"`
		HTMLBody       string `json:"html_body"` // if client pre-renders, just resolve merge tags
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	// Load template
	tpl, err := h.templateSvc.GetTemplate(c.Request.Context(), tenantID, templateID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "template not found"})
		return
	}

	// Load order if provided
	var renderData *models.TemplateRenderData
	if req.OrderID != "" {
		order, err := h.orderSvc.GetOrder(c.Request.Context(), tenantID, req.OrderID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
			return
		}
		linesSlice, err := h.orderSvc.GetOrderLines(c.Request.Context(), tenantID, req.OrderID)
		if err != nil {
			linesSlice = []models.OrderLine{}
		}
		linesPtrs := make([]*models.OrderLine, len(linesSlice))
		for i := range linesSlice { linesPtrs[i] = &linesSlice[i] }

		renderData, err = h.templateSvc.BuildRenderData(c.Request.Context(), tenantID, order, linesPtrs, req.ShippingMethod)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to build render data"})
			return
		}
	}

	// Resolve merge tags in client-supplied HTML
	resolvedHTML := req.HTMLBody
	if renderData != nil && resolvedHTML != "" {
		resolvedHTML = services.ResolveMergeTags(resolvedHTML, renderData)
	}

	c.JSON(http.StatusOK, gin.H{
		"template":    tpl,
		"render_data": renderData,
		"html":        resolvedHTML,
	})
}

// SendEmail POST /api/v1/templates/:id/send
// Body: { "order_id": "...", "to": "email@addr.com", "subject": "...", "html_body": "..." }
func (h *TemplateHandler) SendEmail(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	templateID := c.Param("id")

	var req struct {
		OrderID  string `json:"order_id"`
		To       string `json:"to" binding:"required"`
		Subject  string `json:"subject"`
		HTMLBody string `json:"html_body" binding:"required"` // pre-rendered by client
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "to and html_body are required"})
		return
	}

	// Load template for metadata
	tpl, err := h.templateSvc.GetTemplate(c.Request.Context(), tenantID, templateID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "template not found"})
		return
	}

	// Optionally resolve merge tags with real order data
	htmlBody := req.HTMLBody
	if req.OrderID != "" {
		order, err := h.orderSvc.GetOrder(c.Request.Context(), tenantID, req.OrderID)
		if err == nil {
			linesSlice2, _ := h.orderSvc.GetOrderLines(c.Request.Context(), tenantID, req.OrderID)
			linesPtrs2 := make([]*models.OrderLine, len(linesSlice2))
			for i := range linesSlice2 { linesPtrs2[i] = &linesSlice2[i] }
			renderData, err := h.templateSvc.BuildRenderData(c.Request.Context(), tenantID, order, linesPtrs2, "")
			if err == nil {
				htmlBody = services.ResolveMergeTags(htmlBody, renderData)
			}
		}
	}

	subject := req.Subject
	if subject == "" {
		subject = tpl.Name
	}

	// Log email_queued audit event for order emails
	if req.OrderID != "" && h.fsClient != nil {
		WriteOrderAuditEntry(h.fsClient, tenantID, req.OrderID, "email_queued", "system",
			fmt.Sprintf("Email send initiated: template=%s, to=%s, subject=%s", tpl.Name, req.To, subject))
	}

	if err := h.templateSvc.SendRawEmailForTenant(c.Request.Context(), tenantID, req.To, subject, htmlBody); err != nil {
		// Log failed send
		h.templateSvc.WriteSentMailLog(c.Request.Context(), tenantID, services.SentMailEntry{
			OrderID:      req.OrderID,
			TemplateID:   templateID,
			TemplateName: tpl.Name,
			Recipient:    req.To,
			Subject:      subject,
			Status:       "failed",
			ErrorMessage: err.Error(),
		})
		if req.OrderID != "" && h.fsClient != nil {
			WriteOrderAuditEntry(h.fsClient, tenantID, req.OrderID, "email_failed", "system",
				fmt.Sprintf("Email send failed: %s", err.Error()))
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to send email: " + err.Error()})
		return
	}

	// Log successful send
	h.templateSvc.WriteSentMailLog(c.Request.Context(), tenantID, services.SentMailEntry{
		OrderID:      req.OrderID,
		TemplateID:   templateID,
		TemplateName: tpl.Name,
		Recipient:    req.To,
		Subject:      subject,
		Status:       "sent",
	})
	if req.OrderID != "" && h.fsClient != nil {
		WriteOrderAuditEntry(h.fsClient, tenantID, req.OrderID, "email_sent", "system",
			fmt.Sprintf("Email sent successfully: template=%s, to=%s", tpl.Name, req.To))
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"to":      req.To,
		"subject": subject,
	})
}

// ============================================================================
// SELLER PROFILE
// ============================================================================

// GetSellerProfile GET /api/v1/settings/seller
func (h *TemplateHandler) GetSellerProfile(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	profile, err := h.templateSvc.GetSellerProfile(c.Request.Context(), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"seller": profile})
}

// UpdateSellerProfile PUT /api/v1/settings/seller
func (h *TemplateHandler) UpdateSellerProfile(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var profile models.SellerProfile
	if err := c.ShouldBindJSON(&profile); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	if err := h.templateSvc.UpdateSellerProfile(c.Request.Context(), tenantID, &profile); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"seller": profile})
}

// ============================================================================
// AI TEXT GENERATION PROXY
// ============================================================================

// GenerateText POST /api/v1/templates/ai/generate-text
// Proxies the AI content modal's generation through the backend so the
// API key is never exposed to the browser.
func (h *TemplateHandler) GenerateText(c *gin.Context) {
	var req struct {
		TemplateType   string `json:"template_type"`
		Tone           string `json:"tone"`
		Length         string `json:"length"`
		Prompt         string `json:"prompt" binding:"required"`
		CurrentContent string `json:"current_content"`
		MergeTagList   string `json:"merge_tag_list"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "prompt is required"})
		return
	}

	if !h.aiSvc.IsAvailable() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "AI service not configured"})
		return
	}

	if req.Tone == "" {
		req.Tone = "professional"
	}
	if req.Length == "" {
		req.Length = "medium"
	}

	text, err := h.templateSvc.GenerateTemplateText(
		c.Request.Context(),
		h.aiSvc,
		req.TemplateType,
		req.Tone,
		req.Length,
		req.Prompt,
		req.CurrentContent,
		req.MergeTagList,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Trim whitespace the model sometimes prepends/appends
	text = strings.TrimSpace(text)

	c.JSON(http.StatusOK, gin.H{"text": text})
}

// ============================================================================
// IMAGE UPLOAD
// ============================================================================

// UploadImage POST /api/v1/templates/upload-image
func (h *TemplateHandler) UploadImage(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	if tenantID == "" {
		tenantID = "default"
	}

	file, header, err := c.Request.FormFile("image")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "image file is required"})
		return
	}
	defer file.Close()

	allowed := map[string]string{
		"image/jpeg":    ".jpg",
		"image/png":     ".png",
		"image/gif":     ".gif",
		"image/webp":    ".webp",
		"image/svg+xml": ".svg",
	}
	ext := ".bin"
	contentType := header.Header.Get("Content-Type")
	if mapped, ok := allowed[contentType]; ok {
		ext = mapped
	} else if idx := strings.LastIndex(header.Filename, "."); idx >= 0 {
		ext = strings.ToLower(header.Filename[idx:])
	}

	fileID := uuid.New().String()
	filename := fileID + ext

	uploadDir := fmt.Sprintf("./uploads/templates/%s", tenantID)
	if mkErr := os.MkdirAll(uploadDir, 0755); mkErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create upload directory"})
		return
	}

	dst, dstErr := os.Create(fmt.Sprintf("%s/%s", uploadDir, filename))
	if dstErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save file"})
		return
	}
	defer dst.Close()

	if _, cpErr := io.Copy(dst, file); cpErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not write file"})
		return
	}

	url := fmt.Sprintf("/uploads/templates/%s/%s", tenantID, filename)
	c.JSON(http.StatusOK, gin.H{"url": url, "filename": filename})
}
