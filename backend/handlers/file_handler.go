package handlers

import (
	"net/http"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"module-a/services"
)

type FileHandler struct {
	storageService *services.StorageService
}

func NewFileHandler(storageService *services.StorageService) *FileHandler {
	return &FileHandler{
		storageService: storageService,
	}
}

// UploadFile handles file uploads
// POST /api/v1/upload
func (h *FileHandler) UploadFile(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	// Get file from multipart form
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"code":    "INVALID_REQUEST",
				"message": "No file provided",
			},
		})
		return
	}
	defer file.Close()

	// Get metadata from form
	entityType := c.PostForm("entity_type")   // e.g., "products", "categories"
	entityID := c.PostForm("entity_id")       // e.g., "SKU-001", "cat-electronics"
	subFolder := c.PostForm("sub_folder")     // e.g., "images", "files"

	// Validate required fields
	if entityType == "" || entityID == "" || subFolder == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"code":    "INVALID_REQUEST",
				"message": "entity_type, entity_id, and sub_folder are required",
			},
		})
		return
	}

	// Sanitize filename
	filename := services.SanitizeFilename(header.Filename)

	// Determine content type
	contentType := services.GetContentType(filename)

	// Upload to GCS
	url, path, err := h.storageService.UploadWithPath(
		c.Request.Context(),
		tenantID,
		entityType,
		entityID,
		subFolder,
		filename,
		file,
		contentType,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"code":    "UPLOAD_FAILED",
				"message": err.Error(),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"url":          url,
			"path":         path,
			"filename":     filename,
			"content_type": contentType,
			"size":         header.Size,
		},
	})
}

// DeleteFile handles file deletion
// DELETE /api/v1/files
func (h *FileHandler) DeleteFile(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	// Get path from query or body
	path := c.Query("path")
	if path == "" {
		var req struct {
			Path string `json:"path" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{
					"code":    "INVALID_REQUEST",
					"message": "path is required",
				},
			})
			return
		}
		path = req.Path
	}

	// Validate tenant access
	if !h.storageService.ValidateTenantAccess(tenantID, path) {
		c.JSON(http.StatusForbidden, gin.H{
			"error": gin.H{
				"code":    "ACCESS_DENIED",
				"message": "You don't have permission to delete this file",
			},
		})
		return
	}

	// Delete from GCS
	if err := h.storageService.Delete(c.Request.Context(), path); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"code":    "DELETE_FAILED",
				"message": err.Error(),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "File deleted successfully",
	})
}

// DeleteMultipleFiles handles batch file deletion
// POST /api/v1/files/delete-batch
func (h *FileHandler) DeleteMultipleFiles(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req struct {
		Paths []string `json:"paths" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"code":    "INVALID_REQUEST",
				"message": err.Error(),
			},
		})
		return
	}

	// Validate all paths belong to tenant
	for _, path := range req.Paths {
		if !h.storageService.ValidateTenantAccess(tenantID, path) {
			c.JSON(http.StatusForbidden, gin.H{
				"error": gin.H{
					"code":    "ACCESS_DENIED",
					"message": "One or more paths don't belong to your tenant",
				},
			})
			return
		}
	}

	// Delete all files
	if err := h.storageService.DeleteMultiple(c.Request.Context(), req.Paths); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"code":    "DELETE_FAILED",
				"message": err.Error(),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Files deleted successfully",
		"count":   len(req.Paths),
	})
}

// ListFiles lists files in a folder
// GET /api/v1/files/list?entity_type=products&entity_id=SKU-001&sub_folder=images
func (h *FileHandler) ListFiles(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	entityType := c.Query("entity_type")
	entityID := c.Query("entity_id")
	subFolder := c.Query("sub_folder")

	// Build prefix
	var prefix string
	if entityType != "" && entityID != "" && subFolder != "" {
		prefix = filepath.Join(tenantID, entityType, entityID, subFolder) + "/"
	} else if entityType != "" && entityID != "" {
		prefix = filepath.Join(tenantID, entityType, entityID) + "/"
	} else if entityType != "" {
		prefix = filepath.Join(tenantID, entityType) + "/"
	} else {
		prefix = tenantID + "/"
	}

	files, err := h.storageService.List(c.Request.Context(), prefix)
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
			"files":  files,
			"count":  len(files),
			"prefix": prefix,
		},
	})
}
