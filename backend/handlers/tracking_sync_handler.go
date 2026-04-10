// platform/backend/handlers/tracking_sync_handler.go
package handlers

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"google.golang.org/api/iterator"

	evriclient "module-a/marketplace/clients/evri"
)

// ============================================================================
// TRACKING SYNC HANDLER
// ============================================================================
// All Firestore paths follow the same pattern as dispatch_handlers.go:
//   tenants/{tenant_id}/shipments/{shipment_id}
// ============================================================================

type TrackingSyncHandler struct {
	client     *firestore.Client
	evriClient *evriclient.Client
}

func NewTrackingSyncHandler(client *firestore.Client) *TrackingSyncHandler {
	return &TrackingSyncHandler{
		client:     client,
		evriClient: evriclient.NewClient(),
	}
}

// shipments returns the tenant-scoped shipments collection reference.
// Path: tenants/{tenantID}/shipments
func (h *TrackingSyncHandler) shipments(tenantID string) *firestore.CollectionRef {
	return h.client.Collection("tenants").Doc(tenantID).Collection("shipments")
}

// ============================================================================
// STATUS MAPPING — Evri trackingPointId → MarketMate status
// ============================================================================

var evriTrackingPointMap = map[string]string{
	// Collection / pre-transit
	"COLLECTED":            "in_transit",
	"COLLECTION_SCHEDULED": "pre_transit",
	"LABEL_CREATED":        "pre_transit",
	"COLLECTION_MISSED":    "exception",

	// In transit
	"ARRIVED_AT_DEPOT":    "in_transit",
	"ARRIVED_AT_HUB":      "in_transit",
	"IN_TRANSIT":          "in_transit",
	"PARCEL_SORTED":       "in_transit",
	"DEPARTED_FROM_DEPOT": "in_transit",
	"DEPARTED_FROM_HUB":   "in_transit",

	// Out for delivery
	"OUT_FOR_DELIVERY": "out_for_delivery",
	"WITH_COURIER":     "out_for_delivery",

	// Delivered
	"DELIVERED":             "delivered",
	"DELIVERED_SAFE_PLACE":  "delivered",
	"DELIVERED_NEIGHBOUR":   "delivered",
	"DELIVERED_PARCEL_SHOP": "delivered",
	"DELIVERED_FRONT_DOOR":  "delivered",

	// Attempted
	"ATTEMPTED_DELIVERY": "attempted_delivery",
	"CARD_THROUGH_DOOR":  "attempted_delivery",
	"ACCESS_DENIED":      "attempted_delivery",

	// Exception
	"EXCEPTION":     "exception",
	"HELD_AT_DEPOT": "exception",
	"DAMAGED":       "exception",
	"LOST":          "exception",
	"MISDIRECTED":   "exception",

	// Return
	"RETURN_TO_SENDER_INITIATED": "return_in_progress",
	"RETURNED_TO_SENDER":         "returned",
	"RETURNING_TO_SENDER":        "return_in_progress",
}

func mapEvriStatus(trackingPointID string) string {
	if status, ok := evriTrackingPointMap[strings.ToUpper(trackingPointID)]; ok {
		return status
	}
	return "in_transit"
}

// terminalStatuses are end-states where further polling is unnecessary.
var terminalStatuses = map[string]bool{
	"delivered":  true,
	"returned":   true,
	"voided":     true,
	"cancelled":  true,
	"failed":     true,
}

// ============================================================================
// BACKGROUND SCHEDULER
// ============================================================================

// Run starts the 15-minute tracking sync goroutine. Call once on app startup.
func (h *TrackingSyncHandler) Run() {
	go func() {
		// Wait briefly so the app finishes initialising before the first sync.
		time.Sleep(2 * time.Minute)

		log.Println("[TrackingSync] scheduler started — polling every 15 minutes")
		ticker := time.NewTicker(15 * time.Minute)
		defer ticker.Stop()

		for {
			h.syncAllTenants()
			<-ticker.C
		}
	}()
}

func (h *TrackingSyncHandler) syncAllTenants() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	cutoff := time.Now().Add(-15 * time.Minute)

	// Firestore collection group query: matches tenants/{tenant_id}/shipments/*
	// filtered to active Evri shipments not synced in the last 15 minutes.
	shipIter := h.client.CollectionGroup("shipments").
		Where("carrier_id", "==", "evri").
		Where("last_tracked_at", "<", cutoff).
		Documents(ctx)
	defer shipIter.Stop()

	synced, skipped := 0, 0

	for {
		doc, err := shipIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Printf("[TrackingSync] iterator error: %v", err)
			break
		}

		data := doc.Data()

		// Skip terminal statuses
		if status, _ := data["status"].(string); terminalStatuses[status] {
			skipped++
			continue
		}

		barcode, _ := data["tracking_number"].(string)
		if barcode == "" {
			skipped++
			continue
		}

		// Path: tenants/{tenant_id}/shipments/{shipment_id}
		// doc.Ref.Parent is the "shipments" collection
		// doc.Ref.Parent.Parent is the tenant document (tenants/{tenant_id})
		tenantID := doc.Ref.Parent.Parent.ID
		shipmentID := doc.Ref.ID

		h.syncShipmentTracking(ctx, tenantID, shipmentID, barcode, data)
		synced++
	}

	if synced > 0 || skipped > 0 {
		log.Printf("[TrackingSync] cycle done — synced=%d skipped=%d", synced, skipped)
	}
}

// syncShipmentTracking fetches the latest Evri events for one barcode and
// writes the result back to tenants/{tenantID}/shipments/{shipmentID}.
func (h *TrackingSyncHandler) syncShipmentTracking(ctx context.Context, tenantID, shipmentID, barcode string, _ map[string]interface{}) {
	events, err := h.evriClient.GetTrackingEvents(ctx, barcode)
	if err != nil {
		log.Printf("[TrackingSync] GetTrackingEvents %s: %v", barcode, err)
		// Still refresh the timestamp so we don't hammer Evri on every cycle.
		_, _ = h.shipments(tenantID).Doc(shipmentID).Update(ctx, []firestore.Update{
			{Path: "last_tracked_at", Value: time.Now()},
		})
		return
	}

	if len(events) == 0 {
		_, _ = h.shipments(tenantID).Doc(shipmentID).Update(ctx, []firestore.Update{
			{Path: "last_tracked_at", Value: time.Now()},
		})
		return
	}

	// Find the most recent event
	latestEvent := events[0]
	for _, ev := range events {
		if ev.DateTime.After(latestEvent.DateTime) {
			latestEvent = ev
		}
	}

	newStatus := mapEvriStatus(latestEvent.TrackingPoint.TrackingPointID)

	// Serialise event history
	eventHistory := make([]map[string]interface{}, 0, len(events))
	for _, ev := range events {
		evt := map[string]interface{}{
			"date_time":         ev.DateTime,
			"description":       ev.TrackingPoint.Description,
			"tracking_point_id": ev.TrackingPoint.TrackingPointID,
		}
		if ev.Location != nil {
			evt["location_lat"] = ev.Location.Latitude
			evt["location_lng"] = ev.Location.Longitude
		}
		eventHistory = append(eventHistory, evt)
	}

	updates := []firestore.Update{
		{Path: "status", Value: newStatus},
		{Path: "tracking_events", Value: eventHistory},
		{Path: "last_tracked_at", Value: time.Now()},
	}

	// Fetch ETA for in-progress shipments
	if !terminalStatuses[newStatus] {
		if eta, err := h.evriClient.GetETA(ctx, barcode); err == nil && eta != nil {
			updates = append(updates, firestore.Update{
				Path: "tracking_eta",
				Value: map[string]interface{}{
					"display_string": eta.DisplayString,
					"from_date_time": eta.FromDateTime,
					"to_date_time":   eta.ToDateTime,
					"type":           eta.Type,
				},
			})
		}
	}

	if _, err := h.shipments(tenantID).Doc(shipmentID).Update(ctx, updates); err != nil {
		log.Printf("[TrackingSync] Firestore update %s/%s: %v", tenantID, shipmentID, err)
	}
}

// ============================================================================
// HTTP HANDLERS
// ============================================================================

// POST /api/v1/dispatch/shipments/:shipment_id/sync-tracking
// Manual trigger — syncs one shipment immediately and returns the result.
func (h *TrackingSyncHandler) SyncShipmentTracking(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	shipmentID := c.Param("shipment_id")

	if tenantID == "" || shipmentID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant_id and shipment_id required"})
		return
	}

	ctx := c.Request.Context()

	doc, err := h.shipments(tenantID).Doc(shipmentID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "shipment not found"})
		return
	}

	data := doc.Data()
	barcode, _ := data["tracking_number"].(string)
	if barcode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "shipment has no tracking number"})
		return
	}

	h.syncShipmentTracking(ctx, tenantID, shipmentID, barcode, data)

	// Re-fetch and return
	updated, err := h.shipments(tenantID).Doc(shipmentID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read updated shipment"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"shipment_id": shipmentID,
		"tracking":    buildTrackingResponse(updated.Data()),
	})
}

// GET /api/v1/dispatch/shipments/:shipment_id/tracking-detail
// Full tracking timeline for the order detail drawer.
// Signature and safe-place photo are only returned to authenticated tenant users.
func (h *TrackingSyncHandler) GetShipmentTracking(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	shipmentID := c.Param("shipment_id")

	if tenantID == "" || shipmentID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant_id and shipment_id required"})
		return
	}

	ctx := c.Request.Context()

	doc, err := h.shipments(tenantID).Doc(shipmentID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "shipment not found"})
		return
	}

	data := doc.Data()
	resp := buildTrackingResponse(data)

	// Proof of delivery assets — only for authenticated users (user_id set by auth middleware).
	// Safe-place photos and signatures must NEVER be returned to unauthenticated callers.
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusOK, resp)
		return
	}

	barcode, _ := data["tracking_number"].(string)
	status, _ := data["status"].(string)

	if barcode != "" && status == "delivered" {
		if sig, err := h.evriClient.GetSignature(ctx, barcode); err == nil && sig != nil {
			resp["signature"] = map[string]interface{}{
				"image_base64": sig.ImageBase64,
				"image_format": sig.ImageFormat,
				"printed_name": sig.PrintedName,
				"signed_at":    sig.SignedAt,
			}
		}

		if photo, err := h.evriClient.GetSafePlacePhoto(ctx, barcode); err == nil && photo != nil {
			resp["safe_place_photo"] = map[string]interface{}{
				"image_base64": photo.ImageBase64,
				"image_format": photo.ImageFormat,
				"taken_at":     photo.TakenAt,
			}
		}
	}

	c.JSON(http.StatusOK, resp)
}

// buildTrackingResponse converts a Firestore shipment document map into the
// structured JSON response used by both tracking HTTP endpoints.
func buildTrackingResponse(data map[string]interface{}) map[string]interface{} {
	resp := map[string]interface{}{
		"status":          data["status"],
		"tracking_number": data["tracking_number"],
		"carrier":         data["carrier_id"],
		"last_tracked_at": data["last_tracked_at"],
	}

	if events, ok := data["tracking_events"]; ok {
		resp["events"] = events
	} else {
		resp["events"] = []interface{}{}
	}

	if eta, ok := data["tracking_eta"]; ok {
		resp["eta"] = eta
	}

	return resp
}
