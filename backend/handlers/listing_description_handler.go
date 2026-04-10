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
// LISTING DESCRIPTION HANDLER — P0.1
// ============================================================================
// Per-product per-channel prices, titles and HTML descriptions.
// Stored in: tenants/{tenant_id}/products/{product_id}/listing_descriptions
// Keyed by:  {channel}_{credential_id}
// ============================================================================

type ListingDescriptionHandler struct {
	client *firestore.Client
}

func NewListingDescriptionHandler(client *firestore.Client) *ListingDescriptionHandler {
	return &ListingDescriptionHandler{client: client}
}

type ListingDescription struct {
	DescriptionID string    `firestore:"description_id" json:"description_id"`
	ProductID     string    `firestore:"product_id"     json:"product_id"`
	TenantID      string    `firestore:"tenant_id"      json:"tenant_id"`
	CredentialID  string    `firestore:"credential_id"  json:"credential_id"`
	Channel       string    `firestore:"channel"        json:"channel"`
	AccountName   string    `firestore:"account_name"   json:"account_name"`
	Title         string    `firestore:"title"          json:"title"`
	Description   string    `firestore:"description"    json:"description"`
	Price         float64   `firestore:"price"          json:"price"`
	SyncStatus    string    `firestore:"sync_status"    json:"sync_status"` // pending|success|error|no_change|save_required
	LastSyncedAt  time.Time `firestore:"last_synced_at,omitempty" json:"last_synced_at,omitempty"`
	UpdatedAt     time.Time `firestore:"updated_at"     json:"updated_at"`
}

func (h *ListingDescriptionHandler) ldCol(tenantID, productID string) *firestore.CollectionRef {
	return h.client.Collection("tenants").Doc(tenantID).Collection("products").Doc(productID).Collection("listing_descriptions")
}

// ── GET /api/v1/products/:id/listing-descriptions ────────────────────────────
func (h *ListingDescriptionHandler) List(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	productID := c.Param("id")
	ctx := c.Request.Context()

	var list []ListingDescription
	iter := h.ldCol(tenantID, productID).Documents(ctx)
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list listing descriptions"})
			return
		}
		var ld ListingDescription
		doc.DataTo(&ld)
		list = append(list, ld)
	}
	if list == nil {
		list = []ListingDescription{}
	}
	c.JSON(http.StatusOK, gin.H{"listing_descriptions": list})
}

// ── PUT /api/v1/products/:id/listing-descriptions/:description_id ─────────────
// Creates or fully replaces the channel description document.
func (h *ListingDescriptionHandler) Upsert(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	productID := c.Param("id")
	descriptionID := c.Param("description_id")
	ctx := c.Request.Context()

	var req struct {
		CredentialID string  `json:"credential_id"`
		Channel      string  `json:"channel"`
		AccountName  string  `json:"account_name"`
		Title        string  `json:"title"`
		Description  string  `json:"description"`
		Price        float64 `json:"price"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Auto-generate a description ID if the client sends "new"
	if descriptionID == "new" || descriptionID == "" {
		descriptionID = uuid.New().String()
	}

	ld := ListingDescription{
		DescriptionID: descriptionID,
		ProductID:     productID,
		TenantID:      tenantID,
		CredentialID:  req.CredentialID,
		Channel:       req.Channel,
		AccountName:   req.AccountName,
		Title:         req.Title,
		Description:   req.Description,
		Price:         req.Price,
		SyncStatus:    "save_required",
		UpdatedAt:     time.Now(),
	}

	_, err := h.ldCol(tenantID, productID).Doc(descriptionID).Set(ctx, ld)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save listing description"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"listing_description": ld})
}

// ── DELETE /api/v1/products/:id/listing-descriptions/:description_id ──────────
func (h *ListingDescriptionHandler) Delete(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	productID := c.Param("id")
	descriptionID := c.Param("description_id")
	ctx := c.Request.Context()

	_, err := h.ldCol(tenantID, productID).Doc(descriptionID).Delete(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete listing description"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}
