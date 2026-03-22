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
// POSTAGE DEFINITIONS HANDLER
// ============================================================================

type PostageDefinitionHandler struct {
	client *firestore.Client
}

func NewPostageDefinitionHandler(client *firestore.Client) *PostageDefinitionHandler {
	return &PostageDefinitionHandler{client: client}
}

// ── Data models ───────────────────────────────────────────────────────────────

type PostageRule struct {
	ConditionType  string  `firestore:"condition_type"  json:"condition_type"`  // weight_range|channel|destination_country
	ConditionValue string  `firestore:"condition_value" json:"condition_value"`
	WeightMin      float64 `firestore:"weight_min"      json:"weight_min"`
	WeightMax      float64 `firestore:"weight_max"      json:"weight_max"`
	CarrierID      string  `firestore:"carrier_id"      json:"carrier_id"`
	ServiceID      string  `firestore:"service_id"      json:"service_id"`
}

type PostageDefinition struct {
	DefinitionID     string        `firestore:"definition_id"      json:"definition_id"`
	TenantID         string        `firestore:"tenant_id"          json:"tenant_id"`
	Name             string        `firestore:"name"               json:"name"`
	Rules            []PostageRule `firestore:"rules"              json:"rules"`
	DefaultCarrierID string        `firestore:"default_carrier_id" json:"default_carrier_id"`
	DefaultServiceID string        `firestore:"default_service_id" json:"default_service_id"`
	CreatedAt        time.Time     `firestore:"created_at"         json:"created_at"`
	UpdatedAt        time.Time     `firestore:"updated_at"         json:"updated_at"`
}

// ── Firestore helper ──────────────────────────────────────────────────────────

func (h *PostageDefinitionHandler) col(tenantID string) *firestore.CollectionRef {
	return h.client.Collection("tenants").Doc(tenantID).Collection("postage_definitions")
}

// ── GET /api/v1/postage-definitions ──────────────────────────────────────────

func (h *PostageDefinitionHandler) List(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()

	var list []PostageDefinition
	iter := h.col(tenantID).OrderBy("name", firestore.Asc).Documents(ctx)
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list"})
			return
		}
		var d PostageDefinition
		doc.DataTo(&d)
		list = append(list, d)
	}
	if list == nil {
		list = []PostageDefinition{}
	}
	c.JSON(http.StatusOK, gin.H{"definitions": list})
}

// ── POST /api/v1/postage-definitions ─────────────────────────────────────────

func (h *PostageDefinitionHandler) Create(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()

	var req struct {
		Name             string        `json:"name" binding:"required"`
		Rules            []PostageRule `json:"rules"`
		DefaultCarrierID string        `json:"default_carrier_id"`
		DefaultServiceID string        `json:"default_service_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	now := time.Now()
	def := PostageDefinition{
		DefinitionID:     "pdef_" + uuid.New().String(),
		TenantID:         tenantID,
		Name:             req.Name,
		Rules:            req.Rules,
		DefaultCarrierID: req.DefaultCarrierID,
		DefaultServiceID: req.DefaultServiceID,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if def.Rules == nil {
		def.Rules = []PostageRule{}
	}

	if _, err := h.col(tenantID).Doc(def.DefinitionID).Set(ctx, def); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"definition": def})
}

// ── GET /api/v1/postage-definitions/:id ──────────────────────────────────────

func (h *PostageDefinitionHandler) Get(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	id := c.Param("id")
	ctx := c.Request.Context()

	doc, err := h.col(tenantID).Doc(id).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	var def PostageDefinition
	doc.DataTo(&def)
	c.JSON(http.StatusOK, gin.H{"definition": def})
}

// ── PUT /api/v1/postage-definitions/:id ──────────────────────────────────────

func (h *PostageDefinitionHandler) Update(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	id := c.Param("id")
	ctx := c.Request.Context()

	doc, err := h.col(tenantID).Doc(id).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	var def PostageDefinition
	doc.DataTo(&def)

	var req struct {
		Name             *string       `json:"name"`
		Rules            []PostageRule `json:"rules"`
		DefaultCarrierID *string       `json:"default_carrier_id"`
		DefaultServiceID *string       `json:"default_service_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Name != nil {
		def.Name = *req.Name
	}
	if req.Rules != nil {
		def.Rules = req.Rules
	}
	if req.DefaultCarrierID != nil {
		def.DefaultCarrierID = *req.DefaultCarrierID
	}
	if req.DefaultServiceID != nil {
		def.DefaultServiceID = *req.DefaultServiceID
	}
	def.UpdatedAt = time.Now()

	if _, err := h.col(tenantID).Doc(id).Set(ctx, def); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"definition": def})
}

// ── DELETE /api/v1/postage-definitions/:id ───────────────────────────────────

func (h *PostageDefinitionHandler) Delete(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	id := c.Param("id")
	ctx := c.Request.Context()

	if _, err := h.col(tenantID).Doc(id).Delete(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}

// ── POST /api/v1/postage-definitions/match ───────────────────────────────────

func (h *PostageDefinitionHandler) Match(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()

	var req struct {
		ProductID string  `json:"product_id"`
		Channel   string  `json:"channel"`
		Country   string  `json:"country"`
		WeightKg  float64 `json:"weight_kg"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Iterate definitions and find first matching rule
	iter := h.col(tenantID).Documents(ctx)
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		var def PostageDefinition
		doc.DataTo(&def)
		for _, rule := range def.Rules {
			matched := false
			switch rule.ConditionType {
			case "weight_range":
				matched = req.WeightKg >= rule.WeightMin && req.WeightKg <= rule.WeightMax
			case "channel":
				matched = rule.ConditionValue == req.Channel
			case "destination_country":
				matched = rule.ConditionValue == req.Country
			}
			if matched {
				c.JSON(http.StatusOK, gin.H{
					"matched":           true,
					"definition_id":     def.DefinitionID,
					"definition_name":   def.Name,
					"carrier_id":        rule.CarrierID,
					"service_id":        rule.ServiceID,
				})
				return
			}
		}
		// Check default
		if def.DefaultCarrierID != "" {
			c.JSON(http.StatusOK, gin.H{
				"matched":         true,
				"definition_id":   def.DefinitionID,
				"definition_name": def.Name,
				"carrier_id":      def.DefaultCarrierID,
				"service_id":      def.DefaultServiceID,
				"is_default":      true,
			})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"matched": false})
}
