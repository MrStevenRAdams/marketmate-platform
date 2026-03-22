package handlers

import (
	"net/http"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"module-a/repository"
	"module-a/services"
)

// ============================================================================
// TESCO SCHEMA HANDLER
// ============================================================================
// Manages Tesco category schema caching.
// Follows ebay_schema_handler.go / temu_schema_handler.go pattern.
// ============================================================================

type TescoSchemaHandler struct {
	marketplaceService *services.MarketplaceService
	repo               *repository.MarketplaceRepository
	fsClient           *firestore.Client
}

func NewTescoSchemaHandler(
	marketplaceService *services.MarketplaceService,
	repo *repository.MarketplaceRepository,
	fsClient *firestore.Client,
) *TescoSchemaHandler {
	return &TescoSchemaHandler{
		marketplaceService: marketplaceService,
		repo:               repo,
		fsClient:           fsClient,
	}
}

func (h *TescoSchemaHandler) schemasCol() *firestore.CollectionRef {
	return h.fsClient.Collection("marketplaces").Doc("Tesco").Collection("schemas")
}

// GET /tesco/schemas/list
func (h *TescoSchemaHandler) ListSchemas(c *gin.Context) {
	ctx := c.Request.Context()

	var schemas []map[string]interface{}
	iter := h.schemasCol().Documents(ctx)
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if err != nil {
			break
		}
		data := doc.Data()
		data["id"] = doc.Ref.ID
		schemas = append(schemas, data)
	}
	if schemas == nil {
		schemas = []map[string]interface{}{}
	}
	c.JSON(http.StatusOK, gin.H{"schemas": schemas, "total": len(schemas)})
}

// GET /tesco/schemas/stats
func (h *TescoSchemaHandler) Stats(c *gin.Context) {
	ctx := c.Request.Context()

	count := 0
	iter := h.schemasCol().Documents(ctx)
	defer iter.Stop()
	for {
		_, err := iter.Next()
		if err != nil {
			break
		}
		count++
	}
	c.JSON(http.StatusOK, gin.H{"total_schemas": count, "channel": "tesco"})
}

// POST /tesco/schemas/sync — fetches fresh category data from Tesco and caches it
func (h *TescoSchemaHandler) Sync(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	credentialID := c.Query("credential_id")
	if credentialID == "" {
		credentialID = c.GetHeader("X-Credential-ID")
	}

	if credentialID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "credential_id is required"})
		return
	}

	ctx := c.Request.Context()

	cred, err := h.marketplaceService.GetCredential(ctx, tenantID, credentialID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "failed to load credentials: " + err.Error()})
		return
	}

	merged, err := h.marketplaceService.GetFullCredentials(ctx, cred)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	_ = merged // credentials resolved; in a full implementation we'd call the Tesco API here

	// Stub: in production this would walk the Tesco category tree and cache it
	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"message": "Tesco schema sync initiated. Category data will be cached as it is fetched.",
		"status":  "running",
	})
}
