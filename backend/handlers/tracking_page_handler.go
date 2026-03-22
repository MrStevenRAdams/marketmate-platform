package handlers

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"google.golang.org/api/iterator"

	"module-a/carriers"
	"module-a/models"
	"module-a/services"
)

// TrackingPageHandler serves the public customer-facing tracking endpoint.
// It requires NO authentication — the route is registered outside the tenant
// middleware group so no X-Tenant-Id or Firebase token is needed.
//
// Route: GET /api/v1/public/track/:tracking_number
type TrackingPageHandler struct {
	client      *firestore.Client
	templateSvc *services.TemplateService
}

func NewTrackingPageHandler(client *firestore.Client, templateSvc *services.TemplateService) *TrackingPageHandler {
	return &TrackingPageHandler{
		client:      client,
		templateSvc: templateSvc,
	}
}

// GetPublicTracking looks up a shipment by tracking number across all tenants,
// retrieves live tracking events from the carrier, and returns the result along
// with the seller's branding (name, logo URL).
//
// GET /api/v1/public/track/:tracking_number
func (h *TrackingPageHandler) GetPublicTracking(c *gin.Context) {
	trackingNumber := c.Param("tracking_number")
	if trackingNumber == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tracking number is required"})
		return
	}

	ctx := c.Request.Context()

	// ── 1. Find the shipment by tracking number ───────────────────────────────
	// We use a collection-group query across all tenants' shipments collections.
	shipment, tenantID, err := h.findShipmentByTrackingNumber(ctx, trackingNumber)
	if err != nil || shipment == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "shipment not found for tracking number"})
		return
	}

	// ── 2. Fetch seller branding ──────────────────────────────────────────────
	sellerProfile, _ := h.templateSvc.GetSellerProfile(ctx, tenantID)

	// ── 3. Attempt live tracking from carrier ─────────────────────────────────
	// If the carrier adapter is unavailable or credentials are missing we still
	// return the shipment data — just without live tracking events.
	var trackingInfo *carriers.TrackingInfo
	if shipment.CarrierID != "" {
		adapter, exists := carriers.GetAdapter(shipment.CarrierID)
		if exists {
			creds, credErr := h.getCarrierCredsForTenant(ctx, tenantID, shipment.CarrierID)
			if credErr == nil {
				ti, trackErr := adapter.GetTracking(ctx, creds, shipment.TrackingNumber)
				if trackErr != nil {
					log.Printf("[TrackingPage] carrier GetTracking error for %s: %v", trackingNumber, trackErr)
				} else {
					trackingInfo = ti
				}
			}
		}
	}

	// ── 4. Build public response ──────────────────────────────────────────────
	type PublicSeller struct {
		Name    string `json:"name"`
		LogoURL string `json:"logo_url"`
		Website string `json:"website,omitempty"`
	}

	seller := PublicSeller{}
	if sellerProfile != nil {
		seller.Name = sellerProfile.Name
		seller.LogoURL = sellerProfile.LogoURL
		seller.Website = sellerProfile.Website
	}

	c.JSON(http.StatusOK, gin.H{
		"shipment": shipment,
		"tracking": trackingInfo,
		"seller":   seller,
	})
}

// findShipmentByTrackingNumber uses a Firestore collection-group query to
// locate the shipment document across all tenant sub-collections.
func (h *TrackingPageHandler) findShipmentByTrackingNumber(ctx context.Context, trackingNumber string) (*models.Shipment, string, error) {
	iter := h.client.CollectionGroup("shipments").
		Where("tracking_number", "==", trackingNumber).
		Limit(1).
		Documents(ctx)
	defer iter.Stop()

	doc, err := iter.Next()
	if err == iterator.Done {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("firestore query error: %w", err)
	}

	var shipment models.Shipment
	if err := doc.DataTo(&shipment); err != nil {
		return nil, "", fmt.Errorf("failed to parse shipment: %w", err)
	}

	// Extract tenant_id from the document path:
	// tenants/{tenant_id}/shipments/{shipment_id}
	tenantID := shipment.TenantID
	if tenantID == "" {
		// Fall back to parsing from doc path if field not populated
		refs := doc.Ref.Parent.Parent
		if refs != nil {
			tenantID = refs.ID
		}
	}

	return &shipment, tenantID, nil
}

// getCarrierCredsForTenant fetches stored carrier credentials for a specific tenant.
func (h *TrackingPageHandler) getCarrierCredsForTenant(ctx context.Context, tenantID, carrierID string) (carriers.CarrierCredentials, error) {
	doc, err := h.client.Collection("tenants").Doc(tenantID).
		Collection("carrier_credentials").Doc(carrierID).Get(ctx)
	if err != nil {
		return carriers.CarrierCredentials{}, fmt.Errorf("credentials not found for %s", carrierID)
	}
	var creds carriers.CarrierCredentials
	doc.DataTo(&creds)
	return creds, nil
}
