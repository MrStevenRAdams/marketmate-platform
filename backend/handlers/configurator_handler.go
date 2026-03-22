package handlers

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"module-a/models"
	"module-a/services"
)

// ============================================================================
// CONFIGURATOR HANDLER — SESSION 1 (CFG-01, CFG-02, CFG-03)
// ============================================================================
// Endpoints:
//   GET    /configurators                        — list all for tenant (+ stats)
//   GET    /configurators/:id                    — full detail + linked listings
//   POST   /configurators                        — create
//   PUT    /configurators/:id                    — update
//   DELETE /configurators/:id                    — delete (?force=true to bypass guard)
//   POST   /configurators/:id/duplicate          — copy as "Copy of [name]"
//   POST   /configurators/:id/revise             — bulk push fields to linked listings
//   POST   /configurators/:id/assign-listings    — link listing IDs
//   POST   /configurators/:id/remove-listings    — unlink listing IDs
//   POST   /configurators/auto-select            — find best-matching configurator
// ============================================================================

type ConfiguratorHandler struct {
	svc *services.ConfiguratorService
}

func NewConfiguratorHandler(svc *services.ConfiguratorService) *ConfiguratorHandler {
	return &ConfiguratorHandler{svc: svc}
}

// ── GET /configurators ──────────────────────────────────────────────────────

func (h *ConfiguratorHandler) ListConfigurators(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant ID required"})
		return
	}

	channelFilter := c.Query("channel")

	cfgs, err := h.svc.ListConfigurators(c.Request.Context(), tenantID, channelFilter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"configurators": cfgs,
		"total":         len(cfgs),
	})
}

// ── GET /configurators/:id ──────────────────────────────────────────────────

func (h *ConfiguratorHandler) GetConfigurator(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	configuratorID := c.Param("id")

	detail, err := h.svc.GetConfiguratorDetail(c.Request.Context(), tenantID, configuratorID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("configurator not found: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"configurator": detail})
}

// ── POST /configurators ─────────────────────────────────────────────────────

func (h *ConfiguratorHandler) CreateConfigurator(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant ID required"})
		return
	}

	var cfg models.Configurator
	if err := c.ShouldBindJSON(&cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}

	if err := h.svc.CreateConfigurator(c.Request.Context(), tenantID, &cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"configurator": cfg})
}

// ── PUT /configurators/:id ──────────────────────────────────────────────────

func (h *ConfiguratorHandler) UpdateConfigurator(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	configuratorID := c.Param("id")

	var cfg models.Configurator
	if err := c.ShouldBindJSON(&cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}

	if err := h.svc.UpdateConfigurator(c.Request.Context(), tenantID, configuratorID, &cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"configurator": cfg})
}

// ── DELETE /configurators/:id ───────────────────────────────────────────────

func (h *ConfiguratorHandler) DeleteConfigurator(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	configuratorID := c.Param("id")
	force := c.Query("force") == "true"

	if err := h.svc.DeleteConfigurator(c.Request.Context(), tenantID, configuratorID, force); err != nil {
		// If not forced and linked listings exist, return 409 Conflict
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ── POST /configurators/:id/duplicate ──────────────────────────────────────

func (h *ConfiguratorHandler) DuplicateConfigurator(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	configuratorID := c.Param("id")

	copy, err := h.svc.DuplicateConfigurator(c.Request.Context(), tenantID, configuratorID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"configurator": copy})
}

// ── POST /configurators/:id/revise ─────────────────────────────────────────

func (h *ConfiguratorHandler) ReviseConfigurator(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	configuratorID := c.Param("id")

	var req struct {
		Fields []string `json:"fields" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "fields array is required"})
		return
	}

	// Validate field names
	for _, f := range req.Fields {
		if !models.ValidReviseFields[f] {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": fmt.Sprintf("invalid field %q; valid values: title, description, price, attributes, images, category, shipping", f),
			})
			return
		}
	}

	job, err := h.svc.ReviseConfigurator(c.Request.Context(), tenantID, configuratorID, req.Fields)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"job": job})
}

// ── POST /configurators/:id/assign-listings ─────────────────────────────────

func (h *ConfiguratorHandler) AssignListings(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	configuratorID := c.Param("id")

	var req struct {
		ListingIDs []string `json:"listing_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "listing_ids array is required"})
		return
	}

	if err := h.svc.AssignListings(c.Request.Context(), tenantID, configuratorID, req.ListingIDs); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "assigned": len(req.ListingIDs)})
}

// ── POST /configurators/:id/remove-listings ─────────────────────────────────

func (h *ConfiguratorHandler) RemoveListings(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	configuratorID := c.Param("id")

	var req struct {
		ListingIDs []string `json:"listing_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "listing_ids array is required"})
		return
	}

	if err := h.svc.RemoveListings(c.Request.Context(), tenantID, configuratorID, req.ListingIDs); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "removed": len(req.ListingIDs)})
}

// ── POST /configurators/auto-select ────────────────────────────────────────

func (h *ConfiguratorHandler) AutoSelect(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req struct {
		Channel    string `json:"channel" binding:"required"`
		CategoryID string `json:"category_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "channel is required"})
		return
	}

	configuratorID, err := h.svc.AutoSelect(c.Request.Context(), tenantID, req.Channel, req.CategoryID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"configurator_id": configuratorID})
}
