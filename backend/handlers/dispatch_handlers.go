package handlers

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"

	"module-a/carriers"
	evriclient "module-a/marketplace/clients/evri"
	"module-a/models"
	"module-a/services"
)

// ============================================================================
// DISPATCH HANDLER (updated)
// ============================================================================
// Key changes from original:
//   - Shipment type now uses models.Shipment (not inline struct)
//   - Shipment records FulfilmentSourceID + FulfilmentSourceType
//   - tenantID read from middleware context (not raw header)
//   - Shipment stores OrderIDs []string (supports merged orders)
//   - MarketplaceReporting tracking array initialised on create
// ============================================================================

type DispatchHandler struct {
	client      *firestore.Client
	storage     storageUploader
	usage       *UsageInstrumentor
	templateSvc *services.TemplateService
}

// storageUploader is the minimal interface DispatchHandler needs from the storage service.
// It matches the methods on *services.StorageService used for manifest uploads.
type storageUploader interface {
	UploadWithPath(ctx context.Context, tenantID, entityType, entityID, subFolder, filename string, file io.Reader, contentType string) (string, string, error)
	GetSignedURL(ctx context.Context, path string, expiryMinutes int) (string, error)
}

func NewDispatchHandler(client *firestore.Client) *DispatchHandler {
	return &DispatchHandler{client: client, usage: NewUsageInstrumentor(nil)}
}

// SetTemplateService optionally wires in the TemplateService for automated email triggers.
func (h *DispatchHandler) SetTemplateService(svc *services.TemplateService) {
	h.templateSvc = svc
}

// SetStorageService optionally wires in a storage backend for manifest document uploads.
// If not called the handler will skip GCS upload and return the manifest data inline in the
// JSON response only (no persistent download URL).
func (h *DispatchHandler) SetStorageService(svc storageUploader) {
	h.storage = svc
}

func (h *DispatchHandler) tenantID(c *gin.Context) string {
	if tid := c.GetString("tenant_id"); tid != "" {
		return tid
	}
	return c.GetHeader("X-Tenant-Id")
}

// ============================================================================
// CARRIER MANAGEMENT
// ============================================================================

// ListCarriers GET /api/v1/dispatch/carriers
func (h *DispatchHandler) ListCarriers(c *gin.Context) {
	adapters := carriers.ListAdapters()
	c.JSON(http.StatusOK, gin.H{
		"carriers": adapters,
		"count":    len(adapters),
	})
}

// GetCarrierServices GET /api/v1/dispatch/carriers/:carrier_id/services
func (h *DispatchHandler) GetCarrierServices(c *gin.Context) {
	tenantID := h.tenantID(c)
	carrierID := c.Param("carrier_id")

	if tenantID == "" || carrierID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	adapter, exists := carriers.GetAdapter(carrierID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "carrier not found"})
		return
	}

	creds, err := h.getCarrierCredentials(c.Request.Context(), tenantID, carrierID)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error":  fmt.Sprintf("No credentials configured for carrier '%s'. Go to Settings → Carriers and connect this carrier first.", carrierID),
			"code":   "carrier_not_configured",
		})
		return
	}

	services, err := adapter.GetServices(c.Request.Context(), creds)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to get services: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"carrier":  carrierID,
		"services": services,
	})
}

// ============================================================================
// RATE SHOPPING
// ============================================================================

// GetRates POST /api/v1/dispatch/rates
func (h *DispatchHandler) GetRates(c *gin.Context) {
	tenantID := h.tenantID(c)
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant ID required"})
		return
	}

	var req carriers.RateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	carrierID := c.Query("carrier_id")

	var allRates []carriers.Rate

	if carrierID != "" {
		rates, err := h.getRatesFromCarrier(c.Request.Context(), tenantID, carrierID, req)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		allRates = append(allRates, rates...)
	} else {
		configuredCarriers, err := h.getConfiguredCarriers(c.Request.Context(), tenantID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get carriers"})
			return
		}

		for _, cid := range configuredCarriers {
			rates, err := h.getRatesFromCarrier(c.Request.Context(), tenantID, cid, req)
			if err != nil {
				log.Printf("Failed to get rates from %s: %v", cid, err)
				continue
			}
			allRates = append(allRates, rates...)
		}
	}

	if allRates == nil {
		allRates = []carriers.Rate{}
	}

	c.JSON(http.StatusOK, gin.H{
		"rates": allRates,
		"count": len(allRates),
	})
}

// ============================================================================
// SHIPMENT CREATION
// ============================================================================

// CreateShipment POST /api/v1/dispatch/shipments
func (h *DispatchHandler) CreateShipment(c *gin.Context) {
	tenantID := h.tenantID(c)
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant ID required"})
		return
	}

	var req struct {
		// Order reference — can be one order or multiple (merge)
		OrderID   string   `json:"order_id"`   // Single order (most common)
		OrderIDs  []string `json:"order_ids"`  // Multiple orders (merge)

		// Which order lines this shipment covers (order_id → line_ids)
		// If empty, assumed to cover all lines of the order
		OrderLines map[string][]string `json:"order_lines,omitempty"`

		// Fulfilment source
		FulfilmentSourceID   string `json:"fulfilment_source_id,omitempty"`
		FulfilmentSourceType string `json:"fulfilment_source_type,omitempty"`

		// Carrier
		CarrierID   string                   `json:"carrier_id"`
		ServiceCode string                   `json:"service_code"`
		FromAddress carriers.Address         `json:"from_address"`
		ToAddress   carriers.Address         `json:"to_address"`
		Parcels     []carriers.Parcel        `json:"parcels"`
		Options     carriers.ShipmentOptions `json:"options"`
		Reference   string                   `json:"reference"`

		// LabelCount overrides the product's labels_per_shipment field. Default 1.
		// When > 1, the response label_copies array contains LabelCount distinct
		// base64 label strings so the frontend can open one print tab per box.
		LabelCount int `json:"label_count,omitempty"`

		// IncludeReturnLabel — when true the carrier is asked to generate a return
		// label simultaneously. Passed through via Options.Extra.
		IncludeReturnLabel bool `json:"include_return_label,omitempty"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	// Normalise order IDs
	orderIDs := req.OrderIDs
	if len(orderIDs) == 0 && req.OrderID != "" {
		orderIDs = []string{req.OrderID}
	}
	if len(orderIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "order_id or order_ids required"})
		return
	}

	adapter, exists := carriers.GetAdapter(req.CarrierID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "carrier not found"})
		return
	}

	creds, err := h.getCarrierCredentials(c.Request.Context(), tenantID, req.CarrierID)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error":  fmt.Sprintf("No credentials configured for carrier '%s'. Go to Settings → Carriers and connect this carrier first.", req.CarrierID),
			"code":   "carrier_not_configured",
		})
		return
	}

	// ── Coverage pre-flight ────────────────────────────────────────────────────
	// Run Evri coverage checks before touching the API. Warnings are returned in
	// the response but do NOT block the shipment — the frontend surfaces them as
	// amber callouts and lets the user proceed.
	var coverageWarnings []interface{}
	if req.CarrierID == "evri" {
		for _, w := range evriclient.GetCoverageWarnings(
			req.ToAddress.Country,
			req.ToAddress.PostalCode,
			req.ServiceCode,
		) {
			coverageWarnings = append(coverageWarnings, w)
		}
	}

	// Pass return-label request through carrier options extra map
	if req.IncludeReturnLabel {
		if req.Options.Extra == nil {
			req.Options.Extra = make(map[string]interface{})
		}
		req.Options.Extra["include_return_label"] = true
	}

	// Create shipment with carrier
	shipmentReq := carriers.ShipmentRequest{
		ServiceCode: req.ServiceCode,
		FromAddress: req.FromAddress,
		ToAddress:   req.ToAddress,
		Parcels:     req.Parcels,
		Options:     req.Options,
		Reference:   req.Reference,
	}

	carrierResp, err := adapter.CreateShipment(c.Request.Context(), creds, shipmentReq)
	if err != nil {
		// Surface Evri coverage errors with a structured 422 so the frontend can
		// show the exact Evri message and a carrier-switch suggestion.
		if ce, ok := err.(*evriclient.EvriCoverageError); ok {
			c.JSON(http.StatusUnprocessableEntity, gin.H{
				"error":            "coverage_error",
				"carrier_message":  ce.Description,
				"error_code":       ce.ErrorCode,
				"suggestion":       "This postcode/country is not covered by Evri. Consider Royal Mail or DPD for this destination.",
				"coverage_warnings": coverageWarnings,
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to create shipment: %v", err)})
		return
	}

	// Build marketplace reporting entries (one per order)
	// These will be updated when tracking is submitted back to each channel
	var marketplaceReporting []models.MarketplaceTrackingReport
	for _, oid := range orderIDs {
		// Look up order to get channel info
		orderDoc, err := h.client.Collection("tenants").Doc(tenantID).Collection("orders").Doc(oid).Get(c.Request.Context())
		if err == nil {
			var order models.Order
			if orderDoc.DataTo(&order) == nil {
				marketplaceReporting = append(marketplaceReporting, models.MarketplaceTrackingReport{
					OrderID:          oid,
					Channel:          order.Channel,
					ChannelAccountID: order.ChannelAccountID,
					ExternalOrderID:  order.ExternalOrderID,
					Status:           models.TrackingReportPending,
					Attempts:         0,
				})
			}
		}
	}

	// Build parcel list for storage
	var shipmentParcels []models.ShipmentParcel
	for _, p := range req.Parcels {
		shipmentParcels = append(shipmentParcels, models.ShipmentParcel{
			Weight: p.Weight,
			Length: p.Length,
			Width:  p.Width,
			Height: p.Height,
		})
	}

	// Save shipment using proper model
	shipmentID := uuid.New().String()
	labelGenerated := time.Now()
	estimatedDelivery := carrierResp.EstimatedDelivery

	fromAddr := models.ShipmentAddress{
		Name:         req.FromAddress.Name,
		Company:      req.FromAddress.Company,
		AddressLine1: req.FromAddress.AddressLine1,
		AddressLine2: req.FromAddress.AddressLine2,
		City:         req.FromAddress.City,
		PostalCode:   req.FromAddress.PostalCode,
		Country:      req.FromAddress.Country,
		Phone:        req.FromAddress.Phone,
		Email:        req.FromAddress.Email,
	}
	toAddr := models.ShipmentAddress{
		Name:         req.ToAddress.Name,
		Company:      req.ToAddress.Company,
		AddressLine1: req.ToAddress.AddressLine1,
		AddressLine2: req.ToAddress.AddressLine2,
		City:         req.ToAddress.City,
		PostalCode:   req.ToAddress.PostalCode,
		Country:      req.ToAddress.Country,
		Phone:        req.ToAddress.Phone,
		Email:        req.ToAddress.Email,
	}

	shipment := models.Shipment{
		ShipmentID:           shipmentID,
		TenantID:             tenantID,
		OrderIDs:             orderIDs,
		OrderLines:           req.OrderLines,
		FulfilmentSourceID:   req.FulfilmentSourceID,
		FulfilmentSourceType: req.FulfilmentSourceType,
		CarrierID:            req.CarrierID,
		ServiceCode:          req.ServiceCode,
		ServiceName:          carrierResp.TrackingNumber, // Will be overwritten with service name if available
		TrackingNumber:       carrierResp.TrackingNumber,
		TrackingURL:          carrierResp.TrackingURL,
		LabelFormat:          carrierResp.LabelFormat,
		LabelData:            carrierResp.LabelData,
		FromAddress:          fromAddr,
		ToAddress:            toAddr,
		Parcels:              shipmentParcels,
		Cost:                 carrierResp.Cost.Amount,
		Currency:             carrierResp.Currency,
		Status:               models.ShipmentStatusLabelGenerated,
		MarketplaceReporting: marketplaceReporting,
		RequiresSignature:    req.Options.Signature,
		SaturdayDelivery:     req.Options.SaturdayDelivery,
		CreatedAt:            time.Now(),
		UpdatedAt:            time.Now(),
		LabelGeneratedAt:     &labelGenerated,
		EstimatedDelivery:    &estimatedDelivery,
	}

	_, err = h.client.Collection("tenants").Doc(tenantID).Collection("shipments").Doc(shipmentID).Set(c.Request.Context(), shipment)
	if err != nil {
		log.Printf("Failed to save shipment %s: %v", shipmentID, err)
		// Label was created — don't fail the request, just log
	}

	// Update all orders with shipment ID and despatched status
	for _, oid := range orderIDs {
		h.updateOrderDespatched(c.Request.Context(), tenantID, oid, shipmentID)
	}

	// Record label usage — non-blocking
	if h.usage != nil {
		h.usage.RecordShipmentLabel(c.Request.Context(), tenantID, "", req.CarrierID)
	}

	// Fire order_despatch automated email trigger for each order
	if h.templateSvc != nil {
		for _, oid := range orderIDs {
			orderDoc2, err2 := h.client.Collection("tenants").Doc(tenantID).Collection("orders").Doc(oid).Get(c.Request.Context())
			if err2 == nil {
				var dispatchedOrder models.Order
				if orderDoc2.DataTo(&dispatchedOrder) == nil && dispatchedOrder.Customer.Email != "" {
					go h.templateSvc.SendEventEmail(context.Background(), tenantID, models.TriggerEventOrderDespatch, &dispatchedOrder)
				}
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success":           true,
		"shipment_id":       shipmentID,
		"shipment":          shipment,
		"label_url":         carrierResp.LabelURL,
		"tracking_number":   carrierResp.TrackingNumber,
		"tracking_url":      carrierResp.TrackingURL,
		// label_copies — one base64 PDF string per physical box. Length equals
		// req.LabelCount (default 1). The frontend opens one print tab per entry.
		"label_copies":      buildLabelCopies(carrierResp.LabelData, req.LabelCount),
		"coverage_warnings": coverageWarnings,
		// return_label_base64 is populated when IncludeReturnLabel was true and
		// the carrier responded with a return label (Evri only at present).
		"return_label_base64": func() string {
			if rl, ok := carrierResp.Metadata["return_label_base64"]; ok {
				return rl
			}
			return ""
		}(),
	})
}

// ============================================================================
// SHIPMENT MANAGEMENT
// ============================================================================

// ListShipments GET /api/v1/dispatch/shipments
func (h *DispatchHandler) ListShipments(c *gin.Context) {
	tenantID := h.tenantID(c)
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant ID required"})
		return
	}

	ctx := c.Request.Context()
	q := h.client.Collection("tenants").Doc(tenantID).Collection("shipments").Query

	if status := c.Query("status"); status != "" {
		q = q.Where("status", "==", status)
	}
	if carrierID := c.Query("carrier_id"); carrierID != "" {
		q = q.Where("carrier_id", "==", carrierID)
	}
	if sourceID := c.Query("fulfilment_source_id"); sourceID != "" {
		q = q.Where("fulfilment_source_id", "==", sourceID)
	}

	q = q.OrderBy("created_at", firestore.Desc).Limit(100)

	iter := q.Documents(ctx)
	defer iter.Stop()

	var shipments []models.Shipment
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch shipments"})
			return
		}
		var shipment models.Shipment
		doc.DataTo(&shipment)
		shipments = append(shipments, shipment)
	}

	if shipments == nil {
		shipments = []models.Shipment{}
	}

	c.JSON(http.StatusOK, gin.H{
		"shipments": shipments,
		"count":     len(shipments),
	})
}

// GetShipment GET /api/v1/dispatch/shipments/:shipment_id
func (h *DispatchHandler) GetShipment(c *gin.Context) {
	tenantID := h.tenantID(c)
	shipmentID := c.Param("shipment_id")

	doc, err := h.client.Collection("tenants").Doc(tenantID).Collection("shipments").Doc(shipmentID).Get(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "shipment not found"})
		return
	}

	var shipment models.Shipment
	doc.DataTo(&shipment)
	c.JSON(http.StatusOK, shipment)
}

// GetTracking GET /api/v1/dispatch/shipments/:shipment_id/tracking
func (h *DispatchHandler) GetTracking(c *gin.Context) {
	tenantID := h.tenantID(c)
	shipmentID := c.Param("shipment_id")

	doc, err := h.client.Collection("tenants").Doc(tenantID).Collection("shipments").Doc(shipmentID).Get(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "shipment not found"})
		return
	}

	var shipment models.Shipment
	doc.DataTo(&shipment)

	adapter, exists := carriers.GetAdapter(shipment.CarrierID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "carrier not found"})
		return
	}

	creds, err := h.getCarrierCredentials(c.Request.Context(), tenantID, shipment.CarrierID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "carrier credentials not configured"})
		return
	}

	tracking, err := adapter.GetTracking(c.Request.Context(), creds, shipment.TrackingNumber)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to get tracking: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"shipment": shipment,
		"tracking": tracking,
	})
}

// VoidShipment DELETE /api/v1/dispatch/shipments/:shipment_id
func (h *DispatchHandler) VoidShipment(c *gin.Context) {
	tenantID := h.tenantID(c)
	shipmentID := c.Param("shipment_id")

	doc, err := h.client.Collection("tenants").Doc(tenantID).Collection("shipments").Doc(shipmentID).Get(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "shipment not found"})
		return
	}

	var shipment models.Shipment
	doc.DataTo(&shipment)

	adapter, exists := carriers.GetAdapter(shipment.CarrierID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "carrier not found"})
		return
	}

	creds, err := h.getCarrierCredentials(c.Request.Context(), tenantID, shipment.CarrierID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "carrier credentials not configured"})
		return
	}

	if err := adapter.VoidShipment(c.Request.Context(), creds, shipment.TrackingNumber); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to void: %v", err)})
		return
	}

	now := time.Now()
	_, err = doc.Ref.Update(c.Request.Context(), []firestore.Update{
		{Path: "status", Value: models.ShipmentStatusVoided},
		{Path: "updated_at", Value: now},
	})
	if err != nil {
		log.Printf("Failed to update shipment %s status to voided: %v", shipmentID, err)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Shipment voided successfully",
	})
}

// ============================================================================
// CARRIER CREDENTIALS
// ============================================================================

// SaveCarrierCredentials POST /api/v1/dispatch/carriers/:carrier_id/credentials
func (h *DispatchHandler) SaveCarrierCredentials(c *gin.Context) {
	tenantID := h.tenantID(c)
	carrierID := c.Param("carrier_id")

	var creds carriers.CarrierCredentials
	if err := c.ShouldBindJSON(&creds); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid credentials"})
		return
	}

	creds.CarrierID = carrierID

	// Validate credentials before saving
	adapter, exists := carriers.GetAdapter(carrierID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "carrier not found"})
		return
	}

	if err := adapter.ValidateCredentials(c.Request.Context(), creds); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":  "credential validation failed",
			"detail": err.Error(),
		})
		return
	}

	_, err := h.client.Collection("tenants").Doc(tenantID).Collection("carrier_credentials").Doc(carrierID).Set(c.Request.Context(), creds)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save credentials"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":    "Carrier credentials saved and validated",
		"carrier_id": carrierID,
	})
}

// ============================================================================
// HELPER METHODS
// ============================================================================

func (h *DispatchHandler) getCarrierCredentials(ctx context.Context, tenantID, carrierID string) (carriers.CarrierCredentials, error) {
	doc, err := h.client.Collection("tenants").Doc(tenantID).Collection("carrier_credentials").Doc(carrierID).Get(ctx)
	if err != nil {
		return carriers.CarrierCredentials{}, fmt.Errorf("credentials not found for %s", carrierID)
	}

	var creds carriers.CarrierCredentials
	doc.DataTo(&creds)
	return creds, nil
}

func (h *DispatchHandler) getConfiguredCarriers(ctx context.Context, tenantID string) ([]string, error) {
	iter := h.client.Collection("tenants").Doc(tenantID).Collection("carrier_credentials").Documents(ctx)
	defer iter.Stop()

	var configured []string
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		configured = append(configured, doc.Ref.ID)
	}
	return configured, nil
}

func (h *DispatchHandler) getRatesFromCarrier(ctx context.Context, tenantID, carrierID string, req carriers.RateRequest) ([]carriers.Rate, error) {
	adapter, exists := carriers.GetAdapter(carrierID)
	if !exists {
		return nil, fmt.Errorf("carrier not found: %s", carrierID)
	}

	creds, err := h.getCarrierCredentials(ctx, tenantID, carrierID)
	if err != nil {
		return nil, err
	}

	rateResp, err := adapter.GetRates(ctx, creds, req)
	if err != nil {
		return nil, err
	}

	return rateResp.Rates, nil
}

// buildLabelCopies returns a slice of base64-encoded label strings, one per box.
// count < 1 is treated as 1. All copies carry the same label data — each entry
// represents a distinct physical parcel and results in a separate print tab.
func buildLabelCopies(labelData []byte, count int) []string {
	if count < 1 {
		count = 1
	}
	if len(labelData) == 0 {
		return make([]string, count)
	}
	encoded := base64.StdEncoding.EncodeToString(labelData)
	copies := make([]string, count)
	for i := range copies {
		copies[i] = encoded
	}
	return copies
}

func (h *DispatchHandler) updateOrderDespatched(ctx context.Context, tenantID, orderID, shipmentID string) {
	updates := []firestore.Update{
		{Path: "status", Value: "fulfilled"},
		{Path: "sub_status", Value: "despatched"},
		{Path: "updated_at", Value: time.Now().Format(time.RFC3339)},
	}

	// Append shipment ID to the order's shipment_ids array
	orderDoc, err := h.client.Collection("tenants").Doc(tenantID).Collection("orders").Doc(orderID).Get(ctx)
	if err == nil {
		var order models.Order
		if orderDoc.DataTo(&order) == nil {
			existingIDs := order.ShipmentIDs
			existingIDs = append(existingIDs, shipmentID)
			updates = append(updates, firestore.Update{Path: "shipment_ids", Value: existingIDs})
		}
	}

	_, err = h.client.Collection("tenants").Doc(tenantID).Collection("orders").Doc(orderID).Update(ctx, updates)
	if err != nil {
		log.Printf("Failed to update order %s after despatch: %v", orderID, err)
	}
}

// ============================================================================
// CARRIER MANIFEST / END-OF-DAY
// ============================================================================

// CreateManifest POST /api/v1/dispatch/manifest
// Creates an end-of-day manifest for one carrier or all configured carriers.
// Request body (all fields optional):
//
//	{ "carrier_id": "royal-mail", "manifest_date": "2026-02-27" }
//
// If carrier_id is omitted, manifests are generated for every carrier that has
// despatched shipments on manifest_date (defaults to today).
func (h *DispatchHandler) CreateManifest(c *gin.Context) {
	tenantID := h.tenantID(c)
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant ID required"})
		return
	}

	var req struct {
		CarrierID    string `json:"carrier_id"`    // optional; empty = all carriers
		ManifestDate string `json:"manifest_date"` // YYYY-MM-DD; empty = today
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		// Ignore unmarshal errors — all fields are optional
		_ = err
	}

	if req.ManifestDate == "" {
		req.ManifestDate = time.Now().Format("2006-01-02")
	}

	// Parse start/end of the manifest date for Firestore range query
	loc := time.UTC
	dateStart, err := time.ParseInLocation("2006-01-02", req.ManifestDate, loc)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid manifest_date; expected YYYY-MM-DD"})
		return
	}
	dateEnd := dateStart.Add(24 * time.Hour)

	ctx := c.Request.Context()

	// Fetch all shipments despatched on the requested date
	q := h.client.Collection("tenants").Doc(tenantID).Collection("shipments").
		Where("created_at", ">=", dateStart).
		Where("created_at", "<", dateEnd)

	if req.CarrierID != "" {
		q = q.Where("carrier_id", "==", req.CarrierID)
	}

	iter := q.Documents(ctx)
	defer iter.Stop()

	// Group shipments by carrier
	type groupKey = string
	shipmentsByCarrier := make(map[groupKey][]models.Shipment)
	shipmentIDsByCarrier := make(map[groupKey][]string)

	for {
		doc, iterErr := iter.Next()
		if iterErr == iterator.Done {
			break
		}
		if iterErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch shipments"})
			return
		}
		var s models.Shipment
		_ = doc.DataTo(&s)
		if s.CarrierID == "" {
			continue
		}
		shipmentsByCarrier[s.CarrierID] = append(shipmentsByCarrier[s.CarrierID], s)
		shipmentIDsByCarrier[s.CarrierID] = append(shipmentIDsByCarrier[s.CarrierID], s.ShipmentID)
	}

	if len(shipmentsByCarrier) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"manifests": []interface{}{},
			"message":   "no despatched shipments found for the requested date/carrier",
		})
		return
	}

	var manifests []models.Manifest

	for carrierID, modelShipments := range shipmentsByCarrier {
		adapter, exists := carriers.GetAdapter(carrierID)
		if !exists {
			log.Printf("manifest: carrier adapter not found for %s", carrierID)
			continue
		}

		creds, credsErr := h.getCarrierCredentials(ctx, tenantID, carrierID)
		if credsErr != nil {
			log.Printf("manifest: credentials not found for %s: %v", carrierID, credsErr)
			continue
		}

		// Convert models.Shipment → carriers.ManifestShipment
		manifestShipments := make([]carriers.ManifestShipment, 0, len(modelShipments))
		var totalWeight float64
		var totalCost float64
		currency := "GBP"
		for _, ms := range modelShipments {
			wt := 0.0
			parcels := 0
			for _, p := range ms.Parcels {
				wt += p.Weight
				parcels++
			}
			totalWeight += wt
			totalCost += ms.Cost
			if ms.Currency != "" {
				currency = ms.Currency
			}
			manifestShipments = append(manifestShipments, carriers.ManifestShipment{
				ShipmentID:     ms.ShipmentID,
				TrackingNumber: ms.TrackingNumber,
				ServiceCode:    ms.ServiceCode,
				ServiceName:    ms.ServiceName,
				Reference:      strings.Join(ms.OrderIDs, ","),
				ToName:         ms.ToAddress.Name,
				ToPostalCode:   ms.ToAddress.PostalCode,
				ToCountry:      ms.ToAddress.Country,
				WeightKg:       wt,
				ParcelCount:    parcels,
				Cost:           ms.Cost,
				Currency:       ms.Currency,
				CreatedAt:      ms.CreatedAt,
			})
		}

		result, manifestErr := adapter.GenerateManifest(ctx, creds, manifestShipments)
		manifestID := uuid.New().String()
		now := time.Now()

		manifest := models.Manifest{
			ManifestID:    manifestID,
			TenantID:      tenantID,
			CarrierID:     carrierID,
			CarrierName:   adapter.GetMetadata().DisplayName,
			ShipmentIDs:   shipmentIDsByCarrier[carrierID],
			ShipmentCount: len(manifestShipments),
			TotalWeightKg: totalWeight,
			TotalCost:     totalCost,
			Currency:      currency,
			ManifestDate:  req.ManifestDate,
			CreatedAt:     now,
			UpdatedAt:     now,
		}

		if manifestErr != nil {
			manifest.Status = models.ManifestStatusFailed
			manifest.ErrorMessage = manifestErr.Error()
			manifest.DocumentFormat = "none"
		} else {
			manifest.Status = models.ManifestStatusGenerated
			manifest.DocumentFormat = result.Format

			// Upload manifest document to GCS if storage is configured
			if h.storage != nil && len(result.Data) > 0 {
				ext := result.Format
				filename := fmt.Sprintf("manifest_%s_%s.%s", carrierID, req.ManifestDate, ext)
				contentType := "text/csv"
				if ext == "pdf" {
					contentType = "application/pdf"
				}
				storagePath, downloadURL, uploadErr := h.storage.UploadWithPath(
					ctx, tenantID, "manifests", manifestID, "", filename,
					bytes.NewReader(result.Data), contentType,
				)
				if uploadErr != nil {
					log.Printf("manifest: failed to upload %s to GCS: %v", filename, uploadErr)
				} else {
					manifest.StoragePath = storagePath
					manifest.DownloadURL = downloadURL
				}
			} else if len(result.Data) > 0 {
				// No storage — embed as base64 in the manifest record (documents are
				// typically ≤200 KB so this is acceptable)
				manifest.DownloadURL = "data:" + func() string {
					if result.Format == "pdf" {
						return "application/pdf"
					}
					return "text/csv"
				}() + ";base64," + base64.StdEncoding.EncodeToString(result.Data)
			}
		}

		// Persist manifest record to Firestore
		_, dbErr := h.client.Collection("tenants").Doc(tenantID).
			Collection("manifests").Doc(manifestID).Set(ctx, manifest)
		if dbErr != nil {
			log.Printf("manifest: failed to save manifest %s: %v", manifestID, dbErr)
		}

		manifests = append(manifests, manifest)
	}

	c.JSON(http.StatusOK, gin.H{
		"manifests": manifests,
		"count":     len(manifests),
	})
}

// ListManifests GET /api/v1/dispatch/manifest/history
// Returns paginated manifest history for the tenant, most recent first.
// Query params: carrier_id (optional), limit (default 50), start_date / end_date (YYYY-MM-DD).
func (h *DispatchHandler) ListManifests(c *gin.Context) {
	tenantID := h.tenantID(c)
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant ID required"})
		return
	}

	ctx := c.Request.Context()
	q := h.client.Collection("tenants").Doc(tenantID).Collection("manifests").Query

	if cid := c.Query("carrier_id"); cid != "" {
		q = q.Where("carrier_id", "==", cid)
	}
	if sd := c.Query("start_date"); sd != "" {
		if t, parseErr := time.ParseInLocation("2006-01-02", sd, time.UTC); parseErr == nil {
			q = q.Where("created_at", ">=", t)
		}
	}
	if ed := c.Query("end_date"); ed != "" {
		if t, parseErr := time.ParseInLocation("2006-01-02", ed, time.UTC); parseErr == nil {
			q = q.Where("created_at", "<", t.Add(24*time.Hour))
		}
	}

	limit := 50
	q = q.OrderBy("created_at", firestore.Desc).Limit(limit)

	iter := q.Documents(ctx)
	defer iter.Stop()

	var manifests []models.Manifest
	for {
		doc, iterErr := iter.Next()
		if iterErr == iterator.Done {
			break
		}
		if iterErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch manifest history"})
			return
		}
		var m models.Manifest
		_ = doc.DataTo(&m)
		// Strip inline base64 data from list view to keep payload small
		if strings.HasPrefix(m.DownloadURL, "data:") {
			m.DownloadURL = "" // client must call GetManifest for download
		}
		manifests = append(manifests, m)
	}

	if manifests == nil {
		manifests = []models.Manifest{}
	}

	c.JSON(http.StatusOK, gin.H{
		"manifests": manifests,
		"count":     len(manifests),
	})
}

// GetManifest GET /api/v1/dispatch/manifest/:manifest_id
// Returns a single manifest record including download URL.
// If the document is stored in GCS a fresh signed URL is generated.
func (h *DispatchHandler) GetManifest(c *gin.Context) {
	tenantID := h.tenantID(c)
	manifestID := c.Param("manifest_id")

	if tenantID == "" || manifestID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	ctx := c.Request.Context()
	doc, err := h.client.Collection("tenants").Doc(tenantID).
		Collection("manifests").Doc(manifestID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "manifest not found"})
		return
	}

	var m models.Manifest
	_ = doc.DataTo(&m)

	// Refresh signed URL if we have a storage path and a storage service
	if h.storage != nil && m.StoragePath != "" {
		freshURL, urlErr := h.storage.GetSignedURL(ctx, m.StoragePath, 60)
		if urlErr == nil {
			m.DownloadURL = freshURL
		}
	}

	c.JSON(http.StatusOK, m)
}

// ============================================================================
// CARRIER SETTINGS — Additional endpoints for the Carrier Settings UI
// ============================================================================

// GetCarrierWithStatus GET /api/v1/dispatch/carriers/configured
// Returns all registered carriers merged with their configured status for this tenant
func (h *DispatchHandler) ListCarriersWithStatus(c *gin.Context) {
	tenantID := h.tenantID(c)
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant ID required"})
		return
	}

	// All registered adapters
	adapters := carriers.ListAdapters()

	// Which ones have credentials saved
	configured, _ := h.getConfiguredCarriers(c.Request.Context(), tenantID)
	configuredSet := make(map[string]bool, len(configured))
	for _, id := range configured {
		configuredSet[id] = true
	}

	type CarrierStatus struct {
		carriers.CarrierMetadata
		IsConfigured bool `json:"is_configured"`
		// CredentialFields tells the UI which fields this carrier needs
		CredentialFields []CredentialField `json:"credential_fields"`
	}

	result := make([]CarrierStatus, 0, len(adapters))
	for _, meta := range adapters {
		result = append(result, CarrierStatus{
			CarrierMetadata:  meta,
			IsConfigured:     configuredSet[meta.ID],
			CredentialFields: carrierCredentialFields(meta.ID),
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"carriers": result,
		"count":    len(result),
	})
}

// GetCarrierCredentialStatus GET /api/v1/dispatch/carriers/:carrier_id/credentials
// Returns whether credentials are saved (never returns the secret values)
func (h *DispatchHandler) GetCarrierCredentialStatus(c *gin.Context) {
	tenantID := h.tenantID(c)
	carrierID := c.Param("carrier_id")

	_, err := h.getCarrierCredentials(c.Request.Context(), tenantID, carrierID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"carrier_id":    carrierID,
			"is_configured": false,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"carrier_id":    carrierID,
		"is_configured": true,
	})
}

// DeleteCarrierCredentials DELETE /api/v1/dispatch/carriers/:carrier_id/credentials
// Removes saved credentials, effectively disconnecting the carrier
func (h *DispatchHandler) DeleteCarrierCredentials(c *gin.Context) {
	tenantID := h.tenantID(c)
	carrierID := c.Param("carrier_id")

	if tenantID == "" || carrierID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	_, err := h.client.Collection("tenants").Doc(tenantID).
		Collection("carrier_credentials").Doc(carrierID).Delete(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to remove carrier credentials"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":    "Carrier disconnected",
		"carrier_id": carrierID,
	})
}

// TestCarrierConnection POST /api/v1/dispatch/carriers/:carrier_id/test
// Re-validates saved credentials against the live carrier API
func (h *DispatchHandler) TestCarrierConnection(c *gin.Context) {
	tenantID := h.tenantID(c)
	carrierID := c.Param("carrier_id")

	adapter, exists := carriers.GetAdapter(carrierID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "carrier not found"})
		return
	}

	creds, err := h.getCarrierCredentials(c.Request.Context(), tenantID, carrierID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "No credentials configured for this carrier",
		})
		return
	}

	if err := adapter.ValidateCredentials(c.Request.Context(), creds); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("Connection test failed: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Connection successful",
	})
}

// ============================================================================
// CREDENTIAL FIELD DEFINITIONS
// ============================================================================
// Tells the frontend which fields to show in the setup form for each carrier.
// Labels and placeholder text are carrier-specific.

type CredentialField struct {
	Key         string `json:"key"`          // matches CarrierCredentials field name
	Label       string `json:"label"`        // UI display label
	Placeholder string `json:"placeholder"`  // input placeholder
	Type        string `json:"type"`         // "text" | "password" | "checkbox"
	Required    bool   `json:"required"`
	HelpText    string `json:"help_text,omitempty"`
}

func carrierCredentialFields(carrierID string) []CredentialField {
	switch carrierID {
	case "fedex":
		return []CredentialField{
			{Key: "api_key", Label: "Client ID", Placeholder: "From FedEx Developer Portal", Type: "text", Required: true,
				HelpText: "Found in your project at developer.fedex.com"},
			{Key: "password", Label: "Client Secret", Placeholder: "From FedEx Developer Portal", Type: "password", Required: true},
			{Key: "account_id", Label: "Account Number", Placeholder: "e.g. 123456789", Type: "text", Required: true,
				HelpText: "Your 9-digit FedEx shipping account number"},
			{Key: "is_sandbox", Label: "Use Sandbox (Test Mode)", Type: "checkbox", Required: false,
				HelpText: "Enable for testing. Disable for live shipments."},
		}
	case "evri":
		return []CredentialField{
			{Key: "account_id", Label: "Client ID", Placeholder: "e.g. 9866", Type: "text", Required: true,
				HelpText: "Your numeric Evri client ID from your account manager"},
			{Key: "extra.client_name", Label: "Client Name", Placeholder: "e.g. My Company Ltd", Type: "text", Required: true,
				HelpText: "Your Evri client name exactly as provided by your account manager"},
			{Key: "username", Label: "Username", Placeholder: "e.g. MyCompany-sit", Type: "text", Required: true},
			{Key: "password", Label: "Password", Placeholder: "API password", Type: "password", Required: true},
			{Key: "is_sandbox", Label: "Use SIT (Test Mode)", Type: "checkbox", Required: false,
				HelpText: "Enable for testing with SIT endpoint. Disable for live shipments."},
		}
	case "royal-mail":
		return []CredentialField{
			{Key: "api_key", Label: "API Key", Placeholder: "Paste your Click & Drop API key", Type: "password", Required: true,
				HelpText: "From Click & Drop: Settings > Integrations > Click & Drop API > expand row > copy Auth Key"},
			{Key: "trading_name", Label: "Trading Name", Placeholder: "e.g. My Shop Name", Type: "text", Required: false,
				HelpText: "Optional. Overrides your account default on labels. Set up in Click & Drop > Settings > Trading Names."},
			{Key: "is_oba_linked", Label: "OBA Account Linked", Type: "checkbox", Required: false,
				HelpText: "Label PDF download via API requires OBA linkage. Without OBA: orders are created and tracking numbers returned, but labels must be printed from parcel.royalmail.com. To link: Click & Drop > My Account > Your Profile > OBA Account Details."},
		}
	case "dpd":
		return []CredentialField{
			{Key: "username", Label: "Username", Placeholder: "DPD account username", Type: "text", Required: true},
			{Key: "password", Label: "Password", Placeholder: "DPD account password", Type: "password", Required: true},
			{Key: "account_id", Label: "Account Number", Placeholder: "e.g. 12345678", Type: "text", Required: true},
			{Key: "is_sandbox", Label: "Use Staging", Type: "checkbox", Required: false},
		}
	default:
		// Generic fallback
		return []CredentialField{
			{Key: "api_key", Label: "API Key", Placeholder: "API Key", Type: "password", Required: true},
			{Key: "account_id", Label: "Account ID", Placeholder: "Account ID", Type: "text", Required: false},
			{Key: "is_sandbox", Label: "Use Sandbox", Type: "checkbox", Required: false},
		}
	}
}
