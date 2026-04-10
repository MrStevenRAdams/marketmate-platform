package handlers

// ============================================================================
// TRACKING WEBHOOK HANDLER — SESSION 3 (Task 5)
// ============================================================================
// Accepts inbound webhooks from Royal Mail, DPD, and Evri to update shipment
// tracking status in real time without polling.
//
// Routes (PUBLIC — no X-Tenant-Id required, registered on router not api group):
//   POST /webhooks/tracking/royal-mail  — HMAC-SHA256 verified
//   POST /webhooks/tracking/dpd         — Bearer token verified
//   POST /webhooks/tracking/evri        — Shared secret header verified
//
// On receipt:
//   1. Verify carrier-specific signature
//   2. Store raw payload in tracking_webhooks collection (audit)
//   3. Look up shipment by tracking number across all tenants
//   4. Append tracking event and update tracking_status on the shipment
//   5. Fire in-app notification if status is "delivered" or "failed"
// ============================================================================

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"
)

// ─── Types ────────────────────────────────────────────────────────────────────

// TrackingWebhookEvent is stored in Firestore for audit.
type TrackingWebhookEvent struct {
	WebhookID      string    `firestore:"webhook_id"      json:"webhook_id"`
	Carrier        string    `firestore:"carrier"         json:"carrier"`
	TrackingNumber string    `firestore:"tracking_number" json:"tracking_number"`
	Status         string    `firestore:"status"          json:"status"`
	TenantID       string    `firestore:"tenant_id"       json:"tenant_id"`
	ShipmentID     string    `firestore:"shipment_id"     json:"shipment_id"`
	RawPayload     string    `firestore:"raw_payload"     json:"raw_payload"`
	ReceivedAt     time.Time `firestore:"received_at"     json:"received_at"`
	Processed      bool      `firestore:"processed"       json:"processed"`
	Error          string    `firestore:"error,omitempty" json:"error,omitempty"`
}

// StoredTrackingEvent is appended to the shipment's tracking_events array.
type StoredTrackingEvent struct {
	Timestamp   time.Time `firestore:"timestamp"            json:"timestamp"`
	Status      string    `firestore:"status"               json:"status"`
	Description string    `firestore:"description"          json:"description"`
	Location    string    `firestore:"location,omitempty"   json:"location,omitempty"`
	Carrier     string    `firestore:"carrier"              json:"carrier"`
	WebhookID   string    `firestore:"webhook_id,omitempty" json:"webhook_id,omitempty"`
}

// ─── Handler ──────────────────────────────────────────────────────────────────

type TrackingWebhookHandler struct {
	client              *firestore.Client
	notificationHandler *NotificationHandler
}

func NewTrackingWebhookHandler(client *firestore.Client, notifHandler *NotificationHandler) *TrackingWebhookHandler {
	return &TrackingWebhookHandler{
		client:              client,
		notificationHandler: notifHandler,
	}
}

// ─── POST /webhooks/tracking/royal-mail ──────────────────────────────────────
// Verified by HMAC-SHA256 of the raw body using ROYAL_MAIL_WEBHOOK_SECRET.
// Royal Mail sends the signature in the X-RoyalMail-Signature header.

func (h *TrackingWebhookHandler) RoyalMailWebhook(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read body"})
		return
	}

	// Signature verification
	secret := os.Getenv("ROYAL_MAIL_WEBHOOK_SECRET")
	if secret != "" {
		sig := c.GetHeader("X-RoyalMail-Signature")
		if !verifyHMACSHA256(body, sig, secret) {
			log.Printf("[TrackingWebhook] Royal Mail HMAC verification failed")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid signature"})
			return
		}
	}

	// Parse Royal Mail payload
	// Royal Mail Click & Drop / OBA webhook schema
	var payload struct {
		TrackingNumber string `json:"trackingNumber"`
		Events         []struct {
			EventCode   string `json:"eventCode"`
			EventName   string `json:"eventName"`
			EventDate   string `json:"eventDate"`
			Location    string `json:"location"`
			Description string `json:"description"`
		} `json:"events"`
		// Alternate flat structure
		EventCode   string `json:"eventCode"`
		EventName   string `json:"eventName"`
		EventDate   string `json:"eventDate"`
		Location    string `json:"location"`
	}

	if err := json.Unmarshal(body, &payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
		return
	}

	trackingNumber := payload.TrackingNumber
	if trackingNumber == "" {
		// Try top-level fields (some RM formats are flat)
		c.JSON(http.StatusBadRequest, gin.H{"error": "no tracking number in payload"})
		return
	}

	// Build normalised events
	var events []StoredTrackingEvent
	latestStatus := ""
	latestDescription := ""

	for _, e := range payload.Events {
		ts, _ := time.Parse(time.RFC3339, e.EventDate)
		if ts.IsZero() {
			ts = time.Now().UTC()
		}
		status := normaliseRoyalMailStatus(e.EventCode)
		if latestStatus == "" {
			latestStatus = status
			latestDescription = e.EventName
		}
		events = append(events, StoredTrackingEvent{
			Timestamp:   ts,
			Status:      status,
			Description: coalesce(e.Description, e.EventName),
			Location:    e.Location,
			Carrier:     "royal-mail",
		})
	}

	// Fallback: flat event
	if len(events) == 0 && payload.EventCode != "" {
		ts, _ := time.Parse(time.RFC3339, payload.EventDate)
		if ts.IsZero() {
			ts = time.Now().UTC()
		}
		latestStatus = normaliseRoyalMailStatus(payload.EventCode)
		latestDescription = payload.EventName
		events = append(events, StoredTrackingEvent{
			Timestamp:   ts,
			Status:      latestStatus,
			Description: payload.EventName,
			Location:    payload.Location,
			Carrier:     "royal-mail",
		})
	}

	h.process(c, "royal-mail", trackingNumber, latestStatus, latestDescription, events, string(body))
}

// ─── POST /webhooks/tracking/dpd ─────────────────────────────────────────────
// Verified by Bearer token in the Authorization header.
// Token must match DPD_WEBHOOK_TOKEN env var.

func (h *TrackingWebhookHandler) DPDWebhook(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read body"})
		return
	}

	// Bearer token verification
	token := os.Getenv("DPD_WEBHOOK_TOKEN")
	if token != "" {
		authHeader := c.GetHeader("Authorization")
		expectedBearer := "Bearer " + token
		if authHeader != expectedBearer {
			log.Printf("[TrackingWebhook] DPD bearer token verification failed")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}
	}

	// Parse DPD payload
	var payload struct {
		ParcelNumber string `json:"parcelNumber"`
		// DPD also uses consignmentNumber for the tracking reference
		ConsignmentNumber string `json:"consignmentNumber"`
		StatusCode        string `json:"statusCode"`
		StatusDescription string `json:"statusDescription"`
		EventDateTime     string `json:"eventDateTime"`
		Location          struct {
			Name string `json:"name"`
		} `json:"location"`
	}

	if err := json.Unmarshal(body, &payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
		return
	}

	trackingNumber := payload.ParcelNumber
	if trackingNumber == "" {
		trackingNumber = payload.ConsignmentNumber
	}
	if trackingNumber == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no tracking number in payload"})
		return
	}

	ts, _ := time.Parse(time.RFC3339, payload.EventDateTime)
	if ts.IsZero() {
		ts = time.Now().UTC()
	}

	status := normaliseDPDStatus(payload.StatusCode)
	events := []StoredTrackingEvent{
		{
			Timestamp:   ts,
			Status:      status,
			Description: payload.StatusDescription,
			Location:    payload.Location.Name,
			Carrier:     "dpd",
		},
	}

	h.process(c, "dpd", trackingNumber, status, payload.StatusDescription, events, string(body))
}

// ─── POST /webhooks/tracking/evri ────────────────────────────────────────────
// Verified by shared secret in the X-Evri-Signature header (plain comparison).

func (h *TrackingWebhookHandler) EvriWebhook(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read body"})
		return
	}

	// Shared secret verification
	secret := os.Getenv("EVRI_WEBHOOK_SECRET")
	if secret != "" {
		provided := c.GetHeader("X-Evri-Signature")
		if provided != secret {
			log.Printf("[TrackingWebhook] Evri shared secret verification failed")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid signature"})
			return
		}
	}

	// Parse Evri payload
	var payload struct {
		ParcelCode string `json:"parcelCode"`
		// Evri sometimes uses trackingReference
		TrackingReference string `json:"trackingReference"`
		Status            struct {
			Code        string `json:"code"`
			Label       string `json:"label"`
			Description string `json:"description"`
		} `json:"status"`
		EventTime string `json:"eventTime"`
		Depot     struct {
			Name string `json:"name"`
		} `json:"depot"`
	}

	if err := json.Unmarshal(body, &payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
		return
	}

	trackingNumber := payload.ParcelCode
	if trackingNumber == "" {
		trackingNumber = payload.TrackingReference
	}
	if trackingNumber == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no tracking number in payload"})
		return
	}

	ts, _ := time.Parse(time.RFC3339, payload.EventTime)
	if ts.IsZero() {
		ts = time.Now().UTC()
	}

	status := normaliseEvriStatus(payload.Status.Code)
	description := coalesce(payload.Status.Description, payload.Status.Label)
	events := []StoredTrackingEvent{
		{
			Timestamp:   ts,
			Status:      status,
			Description: description,
			Location:    payload.Depot.Name,
			Carrier:     "evri",
		},
	}

	h.process(c, "evri", trackingNumber, status, description, events, string(body))
}

// ─── Core processing ──────────────────────────────────────────────────────────

func (h *TrackingWebhookHandler) process(
	c *gin.Context,
	carrier, trackingNumber, status, description string,
	events []StoredTrackingEvent,
	rawPayload string,
) {
	ctx := c.Request.Context()
	webhookID := "twh_" + uuid.New().String()
	receivedAt := time.Now().UTC()

	// ── Store raw webhook for audit ────────────────────────────────────────
	webhookDoc := TrackingWebhookEvent{
		WebhookID:      webhookID,
		Carrier:        carrier,
		TrackingNumber: trackingNumber,
		Status:         status,
		RawPayload:     rawPayload,
		ReceivedAt:     receivedAt,
		Processed:      false,
	}
	// Store in a global (non-tenant) collection for auditability
	h.client.Collection("tracking_webhooks").Doc(webhookID).Set(ctx, webhookDoc)

	// ── Find shipment by tracking number across all tenants ────────────────
	tenantID, shipmentID, err := h.findShipment(ctx, trackingNumber)
	if err != nil || shipmentID == "" {
		log.Printf("[TrackingWebhook] %s: tracking number %s not found — stored for audit", carrier, trackingNumber)
		// Still return 200 so the carrier doesn't retry
		c.JSON(http.StatusOK, gin.H{"received": true, "matched": false})
		return
	}

	// Tag webhook with tenant/shipment
	h.client.Collection("tracking_webhooks").Doc(webhookID).Update(ctx, []firestore.Update{
		{Path: "tenant_id", Value: tenantID},
		{Path: "shipment_id", Value: shipmentID},
	})

	// ── Update shipment in Firestore ───────────────────────────────────────
	shipmentRef := h.client.Collection("tenants").Doc(tenantID).
		Collection("shipments").Doc(shipmentID)

	// Load existing events so we can append (Firestore arrays need full replacement)
	shipSnap, err := shipmentRef.Get(ctx)
	if err != nil {
		log.Printf("[TrackingWebhook] failed to load shipment %s: %v", shipmentID, err)
		c.JSON(http.StatusOK, gin.H{"received": true, "matched": true, "error": "failed to load shipment"})
		return
	}

	shipData := shipSnap.Data()
	existingEventsRaw, _ := shipData["tracking_events"].([]interface{})

	// Append new events (convert to []interface{} for Firestore)
	newEventsRaw := existingEventsRaw
	for _, ev := range events {
		newEventsRaw = append(newEventsRaw, map[string]interface{}{
			"timestamp":   ev.Timestamp,
			"status":      ev.Status,
			"description": ev.Description,
			"location":    ev.Location,
			"carrier":     ev.Carrier,
			"webhook_id":  webhookID,
		})
	}

	// Map status to shipment status field
	shipmentStatus := mapToShipmentStatus(status)

	updates := []firestore.Update{
		{Path: "tracking_status", Value: status},
		{Path: "tracking_events", Value: newEventsRaw},
		{Path: "updated_at", Value: receivedAt},
	}
	// Only update shipment lifecycle status for terminal states
	if shipmentStatus != "" {
		updates = append(updates, firestore.Update{Path: "status", Value: shipmentStatus})
		if shipmentStatus == "delivered" {
			updates = append(updates, firestore.Update{Path: "delivered_at", Value: receivedAt})
		}
	}

	if _, err := shipmentRef.Update(ctx, updates); err != nil {
		log.Printf("[TrackingWebhook] failed to update shipment %s: %v", shipmentID, err)
		h.client.Collection("tracking_webhooks").Doc(webhookID).Update(ctx, []firestore.Update{
			{Path: "error", Value: err.Error()},
		})
		c.JSON(http.StatusOK, gin.H{"received": true, "matched": true, "error": "failed to update shipment"})
		return
	}

	// Mark webhook as processed
	h.client.Collection("tracking_webhooks").Doc(webhookID).Update(ctx, []firestore.Update{
		{Path: "processed", Value: true},
	})

	// ── Fire in-app notification for delivered/failed ──────────────────────
	if status == "delivered" || status == "failed" || status == "exception" {
		emoji := "📦"
		notifType := "tracking_update"
		if status == "delivered" {
			emoji = "✅"
		} else {
			emoji = "⚠️"
			notifType = "tracking_issue"
		}
		msg := fmt.Sprintf("%s Shipment %s: %s via %s", emoji, trackingNumber, description, strings.ToUpper(carrier))
		h.notificationHandler.CreateNotification(tenantID, notifType, msg)
	}

	log.Printf("[TrackingWebhook] %s: processed webhook for tracking %s (shipment %s, tenant %s, status: %s)",
		carrier, trackingNumber, shipmentID, tenantID, status)

	c.JSON(http.StatusOK, gin.H{
		"received":        true,
		"matched":         true,
		"webhook_id":      webhookID,
		"shipment_id":     shipmentID,
		"tracking_status": status,
	})
}

// findShipment looks up a shipment by tracking number across all tenants.
// Returns (tenantID, shipmentID, error).
func (h *TrackingWebhookHandler) findShipment(ctx context.Context, trackingNumber string) (string, string, error) {
	// Firestore collection group query across all tenants' shipments subcollections
	iter := h.client.CollectionGroup("shipments").
		Where("tracking_number", "==", trackingNumber).
		Limit(1).
		Documents(ctx)
	defer iter.Stop()

	doc, err := iter.Next()
	if err == iterator.Done {
		return "", "", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("query error: %w", err)
	}

	data := doc.Data()
	tenantID, _ := data["tenant_id"].(string)
	shipmentID, _ := data["shipment_id"].(string)
	if shipmentID == "" {
		shipmentID = doc.Ref.ID
	}

	return tenantID, shipmentID, nil
}

// ─── Signature verification helpers ──────────────────────────────────────────

func verifyHMACSHA256(body []byte, providedSig, secret string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	// Support "sha256=<hash>" prefix used by some carriers
	providedSig = strings.TrimPrefix(providedSig, "sha256=")
	return hmac.Equal([]byte(expected), []byte(providedSig))
}

// ─── Status normalisation ─────────────────────────────────────────────────────

func normaliseRoyalMailStatus(code string) string {
	code = strings.ToUpper(code)
	switch {
	case code == "EVNI" || code == "IIDEE" || strings.Contains(code, "DELIVERED"):
		return "delivered"
	case code == "EVND" || strings.Contains(code, "NOT_DELIVERED") || strings.Contains(code, "FAILED"):
		return "failed"
	case code == "EVCO" || strings.Contains(code, "COLLECTED"):
		return "in_transit"
	case code == "EVCM" || strings.Contains(code, "DEPOT") || strings.Contains(code, "TRANSIT"):
		return "in_transit"
	case code == "EVOA" || strings.Contains(code, "OUT_FOR_DELIVERY"):
		return "out_for_delivery"
	case code == "EVRTS" || strings.Contains(code, "RETURN"):
		return "returned"
	default:
		return "in_transit"
	}
}

func normaliseDPDStatus(code string) string {
	code = strings.ToUpper(code)
	switch {
	case strings.Contains(code, "DELIVERED") || code == "DEL":
		return "delivered"
	case strings.Contains(code, "FAILED") || code == "DEL_FAIL" || code == "MISSED":
		return "failed"
	case strings.Contains(code, "OUT") || code == "OFD":
		return "out_for_delivery"
	case strings.Contains(code, "RETURN"):
		return "returned"
	case strings.Contains(code, "EXCEPTION") || strings.Contains(code, "DAMAGED"):
		return "exception"
	default:
		return "in_transit"
	}
}

func normaliseEvriStatus(code string) string {
	code = strings.ToUpper(code)
	switch {
	case strings.Contains(code, "DELIVERED"):
		return "delivered"
	case strings.Contains(code, "FAILED") || strings.Contains(code, "UNSUCCESSFUL"):
		return "failed"
	case strings.Contains(code, "OUT_FOR") || strings.Contains(code, "ON_VEHICLE"):
		return "out_for_delivery"
	case strings.Contains(code, "RETURN"):
		return "returned"
	case strings.Contains(code, "EXCEPTION"):
		return "exception"
	default:
		return "in_transit"
	}
}

// mapToShipmentStatus converts a normalised tracking status to a shipment lifecycle status.
// Only terminal states update the shipment's main status field.
func mapToShipmentStatus(trackingStatus string) string {
	switch trackingStatus {
	case "delivered":
		return "delivered"
	case "failed", "exception":
		return "failed"
	case "returned":
		return "returned"
	default:
		return "" // non-terminal — don't override shipment status
	}
}

func coalesce(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
