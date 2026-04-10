package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"module-a/models"
	"module-a/services"
)

type AttributeHandler struct {
	attributeService *services.AttributeService
}

func NewAttributeHandler(attributeService *services.AttributeService) *AttributeHandler {
	return &AttributeHandler{
		attributeService: attributeService,
	}
}

// Attribute Endpoints

// ListAttributes GET /api/v1/attributes
func (h *AttributeHandler) ListAttributes(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	attributes, err := h.attributeService.ListAttributes(c.Request.Context(), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"code":    "LIST_FAILED",
				"message": err.Error(),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"data":  attributes,
			"count": len(attributes),
		},
	})
}

// CreateAttribute POST /api/v1/attributes
func (h *AttributeHandler) CreateAttribute(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req models.SimpleAttribute
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"code":    "INVALID_REQUEST",
				"message": err.Error(),
			},
		})
		return
	}

	// Generate ID if not provided
	if req.ID == "" {
		req.ID = uuid.New().String()
	}

	attribute, err := h.attributeService.CreateAttribute(c.Request.Context(), tenantID, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"code":    "CREATE_FAILED",
				"message": err.Error(),
			},
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"data": attribute,
	})
}

// GetAttribute GET /api/v1/attributes/:id
func (h *AttributeHandler) GetAttribute(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	id := c.Param("id")

	attribute, err := h.attributeService.GetAttribute(c.Request.Context(), tenantID, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{
				"code":    "NOT_FOUND",
				"message": err.Error(),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": attribute,
	})
}

// UpdateAttribute PATCH /api/v1/attributes/:id
func (h *AttributeHandler) UpdateAttribute(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	id := c.Param("id")

	var req models.SimpleAttribute
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"code":    "INVALID_REQUEST",
				"message": err.Error(),
			},
		})
		return
	}

	req.ID = id

	attribute, err := h.attributeService.UpdateAttribute(c.Request.Context(), tenantID, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"code":    "UPDATE_FAILED",
				"message": err.Error(),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": attribute,
	})
}

// DeleteAttribute DELETE /api/v1/attributes/:id
func (h *AttributeHandler) DeleteAttribute(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	id := c.Param("id")

	if err := h.attributeService.DeleteAttribute(c.Request.Context(), tenantID, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"code":    "DELETE_FAILED",
				"message": err.Error(),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"deleted": true,
		},
	})
}

// Attribute Set Endpoints

// ListAttributeSets GET /api/v1/attribute-sets
func (h *AttributeHandler) ListAttributeSets(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	sets, err := h.attributeService.ListAttributeSets(c.Request.Context(), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"code":    "LIST_FAILED",
				"message": err.Error(),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"data":  sets,
			"count": len(sets),
		},
	})
}

// CreateAttributeSet POST /api/v1/attribute-sets
func (h *AttributeHandler) CreateAttributeSet(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req models.SimpleAttributeSet
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"code":    "INVALID_REQUEST",
				"message": err.Error(),
			},
		})
		return
	}

	// Generate ID if not provided
	if req.ID == "" {
		req.ID = uuid.New().String()
	}

	set, err := h.attributeService.CreateAttributeSet(c.Request.Context(), tenantID, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"code":    "CREATE_FAILED",
				"message": err.Error(),
			},
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"data": set,
	})
}

// GetAttributeSet GET /api/v1/attribute-sets/:id
func (h *AttributeHandler) GetAttributeSet(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	id := c.Param("id")

	set, err := h.attributeService.GetAttributeSet(c.Request.Context(), tenantID, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{
				"code":    "NOT_FOUND",
				"message": err.Error(),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": set,
	})
}

// UpdateAttributeSet PATCH /api/v1/attribute-sets/:id
func (h *AttributeHandler) UpdateAttributeSet(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	id := c.Param("id")

	var req models.SimpleAttributeSet
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"code":    "INVALID_REQUEST",
				"message": err.Error(),
			},
		})
		return
	}

	req.ID = id

	set, err := h.attributeService.UpdateAttributeSet(c.Request.Context(), tenantID, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"code":    "UPDATE_FAILED",
				"message": err.Error(),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": set,
	})
}

// DeleteAttributeSet DELETE /api/v1/attribute-sets/:id
func (h *AttributeHandler) DeleteAttributeSet(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	id := c.Param("id")

	if err := h.attributeService.DeleteAttributeSet(c.Request.Context(), tenantID, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"code":    "DELETE_FAILED",
				"message": err.Error(),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"deleted": true,
		},
	})
}
