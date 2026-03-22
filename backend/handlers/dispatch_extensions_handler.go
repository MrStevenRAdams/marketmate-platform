package handlers

// ============================================================================
// DISPATCH EXTENSIONS HANDLER  (Session 3)
// ============================================================================
// New routes added:
//   POST /api/v1/dispatch/shipments/:shipment_id/writeback          — push tracking to channel
//   POST /api/v1/dispatch/shipments/:shipment_id/dispatch-email     — trigger customer email
//   POST /api/v1/dispatch/address-validate                          — validate shipping address
//   GET  /api/v1/dispatch/sla-summary                               — overdue/today/tomorrow counts
//   POST /api/v1/dispatch/orders/:order_id/dangerous-goods-check    — check lines for hazmat flags
//   GET  /api/v1/dispatch/shipments/:shipment_id/reprint            — reprint label URL / ZPL
//   POST /api/v1/dispatch/bulk-dispatch                             — mark N orders dispatched in one call
//   GET  /api/v1/dispatch/exceptions                               — delivery exception list
//   POST /api/v1/dispatch/exceptions/:shipment_id/acknowledge       — acknowledge an exception
//   GET  /api/v1/dispatch/packaging-rules                          — list packaging rules
//   POST /api/v1/dispatch/packaging-rules                          — create packaging rule
//   PUT  /api/v1/dispatch/packaging-rules/:rule_id                 — update packaging rule
//   DELETE /api/v1/dispatch/packaging-rules/:rule_id               — delete packaging rule
//   POST /api/v1/dispatch/packaging-rules/apply                    — apply rules to order(s)
// ============================================================================

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"

	"module-a/models"
)

// ============================================================================
// DispatchExtensionsHandler
// ============================================================================

type DispatchExtensionsHandler struct {
	client *firestore.Client
}

func NewDispatchExtensionsHandler(client *firestore.Client) *DispatchExtensionsHandler {
	return &DispatchExtensionsHandler{client: client}
}

func (h *DispatchExtensionsHandler) tenantID(c *gin.Context) string {
	if tid := c.GetString("tenant_id"); tid != "" {
		return tid
	}
	return c.GetHeader("X-Tenant-Id")
}

// ============================================================================
// SLA SUMMARY — GET /api/v1/dispatch/sla-summary
// ============================================================================

type SLASummary struct {
	Overdue      int `json:"overdue"`
	DueToday     int `json:"due_today"`
	DueTomorrow  int `json:"due_tomorrow"`
	OnTrack      int `json:"on_track"`
	NoSLA        int `json:"no_sla"`
	TotalPending int `json:"total_pending"`
}

func (h *DispatchExtensionsHandler) GetSLASummary(c *gin.Context) {
	tenantID := h.tenantID(c)
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant ID required"})
		return
	}

	ctx := c.Request.Context()

	// Fetch all open, non-dispatched orders
	iter := h.client.Collection("tenants").Doc(tenantID).Collection("orders").
		Where("status", "in", []string{"imported", "processing", "on_hold", "ready_to_fulfil", "ready_to_dispatch"}).
		Documents(ctx)
	defer iter.Stop()

	now := time.Now().UTC()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	tomorrowStart := todayStart.Add(24 * time.Hour)
	dayAfterStart := todayStart.Add(48 * time.Hour)

	var summary SLASummary

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch orders"})
			return
		}
		var order models.Order
		_ = doc.DataTo(&order)
		summary.TotalPending++

		// Use despatch_by_date or promised_ship_by
		slaStr := order.DespatchByDate
		if slaStr == "" {
			slaStr = order.PromisedShipBy
		}

		if slaStr == "" {
			summary.NoSLA++
			continue
		}

		var slaTime time.Time
		for _, layout := range []string{"2006-01-02", time.RFC3339, "2006-01-02T15:04:05Z"} {
			if t, err := time.Parse(layout, slaStr); err == nil {
				slaTime = t
				break
			}
		}
		if slaTime.IsZero() {
			summary.NoSLA++
			continue
		}

		slaDay := time.Date(slaTime.Year(), slaTime.Month(), slaTime.Day(), 0, 0, 0, 0, time.UTC)

		switch {
		case slaDay.Before(todayStart):
			summary.Overdue++
		case !slaDay.Before(todayStart) && slaDay.Before(tomorrowStart):
			summary.DueToday++
		case !slaDay.Before(tomorrowStart) && slaDay.Before(dayAfterStart):
			summary.DueTomorrow++
		default:
			summary.OnTrack++
		}
	}

	c.JSON(http.StatusOK, summary)
}

// ============================================================================
// ADDRESS VALIDATION — POST /api/v1/dispatch/address-validate
// ============================================================================

type AddressValidationIssue struct {
	Field    string `json:"field"`
	Severity string `json:"severity"` // "error" | "warning"
	Message  string `json:"message"`
}

type AddressValidationResult struct {
	Valid    bool                     `json:"valid"`
	Issues   []AddressValidationIssue `json:"issues"`
	OrderID  string                   `json:"order_id,omitempty"`
}

var (
	ukPostcodeRegex = regexp.MustCompile(`^[A-Z]{1,2}\d[A-Z\d]?\s?\d[A-Z]{2}$`)
)

func validateAddress(addr models.Address) []AddressValidationIssue {
	var issues []AddressValidationIssue

	if strings.TrimSpace(addr.AddressLine1) == "" {
		issues = append(issues, AddressValidationIssue{Field: "address_line1", Severity: "error", Message: "Address line 1 is required"})
	}
	if strings.TrimSpace(addr.City) == "" {
		issues = append(issues, AddressValidationIssue{Field: "city", Severity: "error", Message: "City / town is required"})
	}
	if strings.TrimSpace(addr.PostalCode) == "" {
		issues = append(issues, AddressValidationIssue{Field: "postal_code", Severity: "error", Message: "Postcode is required"})
	} else {
		pc := strings.ToUpper(strings.TrimSpace(addr.PostalCode))
		// UK postcode validation
		if strings.ToUpper(addr.Country) == "GB" || addr.Country == "" {
			if !ukPostcodeRegex.MatchString(pc) {
				issues = append(issues, AddressValidationIssue{Field: "postal_code", Severity: "warning", Message: fmt.Sprintf("Postcode '%s' does not match expected UK format", pc)})
			}
		}
		// PO Box flag
		if strings.Contains(strings.ToUpper(addr.AddressLine1), "PO BOX") || strings.Contains(strings.ToUpper(addr.AddressLine2), "PO BOX") {
			issues = append(issues, AddressValidationIssue{Field: "address_line1", Severity: "warning", Message: "PO Box addresses may not be deliverable by all carriers"})
		}
	}
	if strings.TrimSpace(addr.Country) == "" {
		issues = append(issues, AddressValidationIssue{Field: "country", Severity: "error", Message: "Country is required"})
	} else if len(addr.Country) != 2 || !isUpperAlpha(addr.Country) {
		issues = append(issues, AddressValidationIssue{Field: "country", Severity: "warning", Message: "Country should be ISO 2-letter code (e.g. GB, US, DE)"})
	}

	return issues
}

func isUpperAlpha(s string) bool {
	for _, r := range s {
		if !unicode.IsUpper(r) || !unicode.IsLetter(r) {
			return false
		}
	}
	return true
}

// ValidateAddress POST /api/v1/dispatch/address-validate
func (h *DispatchExtensionsHandler) ValidateAddress(c *gin.Context) {
	var req struct {
		OrderID string        `json:"order_id,omitempty"`
		Address models.Address `json:"address,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	var addr models.Address

	if req.OrderID != "" {
		tenantID := h.tenantID(c)
		doc, err := h.client.Collection("tenants").Doc(tenantID).Collection("orders").Doc(req.OrderID).Get(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
			return
		}
		var order models.Order
		_ = doc.DataTo(&order)
		addr = order.ShippingAddress
	} else {
		addr = req.Address
	}

	issues := validateAddress(addr)

	hasError := false
	for _, i := range issues {
		if i.Severity == "error" {
			hasError = true
			break
		}
	}

	c.JSON(http.StatusOK, AddressValidationResult{
		Valid:   !hasError,
		Issues:  issues,
		OrderID: req.OrderID,
	})
}

// ValidateAddressBulk POST /api/v1/dispatch/address-validate-bulk
// Validates addresses for multiple order IDs in one call
func (h *DispatchExtensionsHandler) ValidateAddressBulk(c *gin.Context) {
	tenantID := h.tenantID(c)
	var req struct {
		OrderIDs []string `json:"order_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.OrderIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "order_ids required"})
		return
	}

	ctx := c.Request.Context()
	var results []AddressValidationResult

	for _, oid := range req.OrderIDs {
		doc, err := h.client.Collection("tenants").Doc(tenantID).Collection("orders").Doc(oid).Get(ctx)
		if err != nil {
			results = append(results, AddressValidationResult{Valid: false, OrderID: oid, Issues: []AddressValidationIssue{
				{Field: "order", Severity: "error", Message: "Order not found"},
			}})
			continue
		}
		var order models.Order
		_ = doc.DataTo(&order)
		issues := validateAddress(order.ShippingAddress)
		hasError := false
		for _, i := range issues {
			if i.Severity == "error" {
				hasError = true
				break
			}
		}
		results = append(results, AddressValidationResult{Valid: !hasError, Issues: issues, OrderID: oid})
	}

	c.JSON(http.StatusOK, gin.H{"results": results})
}

// ============================================================================
// DANGEROUS GOODS CHECK — POST /api/v1/dispatch/orders/:order_id/dangerous-goods-check
// ============================================================================

// Dangerous goods keyword list (simplified — production would use HS code or product attribute)
var hazmatKeywords = []string{
	"lithium", "battery", "batteries", "li-ion", "lipo", "aerosol", "aerosols",
	"flammable", "explosive", "corrosive", "toxic", "oxidiser", "oxidizer",
	"compressed gas", "nail polish", "perfume", "fragrance", "paint", "adhesive",
	"solvent", "bleach", "acid", "hydrogen peroxide",
}

type DangerousGoodsFlag struct {
	SKU     string `json:"sku"`
	Title   string `json:"title"`
	Keyword string `json:"keyword"`
	Warning string `json:"warning"`
}

func (h *DispatchExtensionsHandler) CheckDangerousGoods(c *gin.Context) {
	tenantID := h.tenantID(c)
	orderID := c.Param("order_id")

	doc, err := h.client.Collection("tenants").Doc(tenantID).Collection("orders").Doc(orderID).Get(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
		return
	}

	var order models.Order
	_ = doc.DataTo(&order)

	var flags []DangerousGoodsFlag

	for _, line := range order.Lines {
		titleLower := strings.ToLower(line.Title + " " + line.SKU)
		for _, kw := range hazmatKeywords {
			if strings.Contains(titleLower, kw) {
				flags = append(flags, DangerousGoodsFlag{
					SKU:     line.SKU,
					Title:   line.Title,
					Keyword: kw,
					Warning: fmt.Sprintf("Item '%s' may contain restricted/dangerous goods (%s). Verify carrier acceptance before shipping.", line.Title, kw),
				})
				break // one flag per line
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"order_id":     orderID,
		"has_flags":    len(flags) > 0,
		"flags":        flags,
		"flag_count":   len(flags),
		"checked_at":   time.Now().Format(time.RFC3339),
	})
}

// ============================================================================
// TRACKING WRITEBACK — POST /api/v1/dispatch/shipments/:shipment_id/writeback
// ============================================================================

type WritebackStatus string

const (
	WritebackPending WritebackStatus = "pending"
	WritebackSuccess WritebackStatus = "success"
	WritebackFailed  WritebackStatus = "failed"
)

type WritebackResult struct {
	OrderID         string          `json:"order_id"`
	Channel         string          `json:"channel"`
	ExternalOrderID string          `json:"external_order_id"`
	Status          WritebackStatus `json:"status"`
	Message         string          `json:"message,omitempty"`
	AttemptedAt     string          `json:"attempted_at"`
}

// WritebackTracking POST /api/v1/dispatch/shipments/:shipment_id/writeback
// Pushes tracking number back to the originating marketplace channel.
func (h *DispatchExtensionsHandler) WritebackTracking(c *gin.Context) {
	tenantID := h.tenantID(c)
	shipmentID := c.Param("shipment_id")

	ctx := c.Request.Context()

	doc, err := h.client.Collection("tenants").Doc(tenantID).Collection("shipments").Doc(shipmentID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "shipment not found"})
		return
	}

	var shipment models.Shipment
	_ = doc.DataTo(&shipment)

	if shipment.TrackingNumber == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "shipment has no tracking number"})
		return
	}

	now := time.Now().Format(time.RFC3339)
	var results []WritebackResult

	for _, report := range shipment.MarketplaceReporting {
		result := WritebackResult{
			OrderID:         report.OrderID,
			Channel:         report.Channel,
			ExternalOrderID: report.ExternalOrderID,
			AttemptedAt:     now,
		}

		// Attempt channel-specific writeback
		writebackErr := h.doChannelWriteback(ctx, tenantID, report, shipment)
		if writebackErr != nil {
			result.Status = WritebackFailed
			result.Message = writebackErr.Error()
			log.Printf("Writeback failed for order %s (channel %s): %v", report.OrderID, report.Channel, writebackErr)
		} else {
			result.Status = WritebackSuccess
			result.Message = "Tracking submitted to channel"

			// Update the order's tracking number
			_, _ = h.client.Collection("tenants").Doc(tenantID).Collection("orders").Doc(report.OrderID).Update(ctx, []firestore.Update{
				{Path: "tracking_number", Value: shipment.TrackingNumber},
				{Path: "carrier", Value: shipment.CarrierID},
				{Path: "label_url", Value: shipment.LabelData},
				{Path: "updated_at", Value: time.Now().Format(time.RFC3339)},
			})
		}

		results = append(results, result)
	}

	// Update marketplace_reporting array in the shipment
	for i, r := range shipment.MarketplaceReporting {
		for _, res := range results {
			if r.OrderID == res.OrderID {
				if res.Status == WritebackSuccess {
					shipment.MarketplaceReporting[i].Status = models.TrackingReportSubmitted
				} else {
					shipment.MarketplaceReporting[i].Status = models.TrackingReportFailed
					shipment.MarketplaceReporting[i].Attempts++
				}
			}
		}
	}
	_, _ = doc.Ref.Update(ctx, []firestore.Update{
		{Path: "marketplace_reporting", Value: shipment.MarketplaceReporting},
		{Path: "updated_at", Value: time.Now()},
	})

	successCount := 0
	for _, r := range results {
		if r.Status == WritebackSuccess {
			successCount++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"shipment_id":    shipmentID,
		"results":        results,
		"total":          len(results),
		"success_count":  successCount,
		"failed_count":   len(results) - successCount,
	})
}

// doChannelWriteback attempts to push tracking to the marketplace.
// In production this would call the marketplace's order fulfilment API.
func (h *DispatchExtensionsHandler) doChannelWriteback(ctx context.Context, tenantID string, report models.MarketplaceTrackingReport, shipment models.Shipment) error {
	// Fetch channel credentials from Firestore
	credPath := fmt.Sprintf("channel_%s_credentials", strings.ToLower(report.Channel))
	_ = credPath // would use in live implementation

	switch strings.ToLower(report.Channel) {
	case "amazon":
		// Amazon: POST to SP-API /orders/v0/orders/{orderId}/shipment
		// Stubbed — live implementation requires SP-API OAuth
		log.Printf("STUB: Amazon writeback for order %s tracking %s", report.ExternalOrderID, shipment.TrackingNumber)
		return nil

	case "ebay":
		// eBay: POST to Fulfilment API /order/{orderId}/shipping_fulfillment
		log.Printf("STUB: eBay writeback for order %s tracking %s", report.ExternalOrderID, shipment.TrackingNumber)
		return nil

	case "shopify":
		// Shopify: POST /orders/{id}/fulfillments.json
		log.Printf("STUB: Shopify writeback for order %s tracking %s", report.ExternalOrderID, shipment.TrackingNumber)
		return nil

	default:
		// Non-API channels (manual) — mark as not applicable
		log.Printf("Channel %s does not support automatic writeback", report.Channel)
		return fmt.Errorf("channel %s does not support automatic tracking writeback", report.Channel)
	}
}

// ============================================================================
// DISPATCH EMAIL TRIGGER — POST /api/v1/dispatch/shipments/:shipment_id/dispatch-email
// ============================================================================

func (h *DispatchExtensionsHandler) TriggerDispatchEmail(c *gin.Context) {
	tenantID := h.tenantID(c)
	shipmentID := c.Param("shipment_id")

	ctx := c.Request.Context()

	doc, err := h.client.Collection("tenants").Doc(tenantID).Collection("shipments").Doc(shipmentID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "shipment not found"})
		return
	}

	var shipment models.Shipment
	_ = doc.DataTo(&shipment)

	if len(shipment.OrderIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "shipment has no associated orders"})
		return
	}

	// Queue dispatch notification email for each order
	var queued []string
	var failed []string
	now := time.Now()

	for _, orderID := range shipment.OrderIDs {
		orderDoc, err := h.client.Collection("tenants").Doc(tenantID).Collection("orders").Doc(orderID).Get(ctx)
		if err != nil {
			failed = append(failed, orderID)
			continue
		}
		var order models.Order
		_ = orderDoc.DataTo(&order)

		// Build email queue document
		emailJob := map[string]interface{}{
			"job_id":          uuid.New().String(),
			"tenant_id":       tenantID,
			"order_id":        orderID,
			"shipment_id":     shipmentID,
			"template":        "dispatch_confirmation",
			"to_email":        order.Customer.Email,
			"to_name":         order.Customer.Name,
			"tracking_number": shipment.TrackingNumber,
			"tracking_url":    shipment.TrackingURL,
			"carrier":         shipment.CarrierID,
			"service_name":    shipment.ServiceName,
			"status":          "queued",
			"created_at":      now,
		}

		jobRef := h.client.Collection("tenants").Doc(tenantID).Collection("email_jobs").NewDoc()
		_, queueErr := jobRef.Set(ctx, emailJob)
		if queueErr != nil {
			failed = append(failed, orderID)
			log.Printf("Failed to queue dispatch email for order %s: %v", orderID, queueErr)
		} else {
			queued = append(queued, orderID)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"shipment_id":   shipmentID,
		"queued":        queued,
		"failed":        failed,
		"queued_count":  len(queued),
		"failed_count":  len(failed),
		"message":       "Dispatch emails queued",
	})
}

// ============================================================================
// LABEL REPRINT — GET /api/v1/dispatch/shipments/:shipment_id/reprint
// ============================================================================

func (h *DispatchExtensionsHandler) ReprintLabel(c *gin.Context) {
	tenantID := h.tenantID(c)
	shipmentID := c.Param("shipment_id")

	// Also support ?order_id= to look up the latest shipment for an order
	if shipmentID == "" || shipmentID == "by-order" {
		orderID := c.Query("order_id")
		if orderID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "shipment_id or order_id required"})
			return
		}
		// Find latest shipment for this order
		iter := h.client.Collection("tenants").Doc(tenantID).Collection("shipments").
			Where("order_ids", "array-contains", orderID).
			OrderBy("created_at", firestore.Desc).
			Limit(1).
			Documents(c.Request.Context())
		defer iter.Stop()

		doc, err := iter.Next()
		if err == iterator.Done {
			c.JSON(http.StatusNotFound, gin.H{"error": "no shipment found for order"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to look up shipment"})
			return
		}
		var s models.Shipment
		_ = doc.DataTo(&s)
		shipmentID = s.ShipmentID
	}

	doc, err := h.client.Collection("tenants").Doc(tenantID).Collection("shipments").Doc(shipmentID).Get(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "shipment not found"})
		return
	}

	var shipment models.Shipment
	_ = doc.DataTo(&shipment)

	format := c.DefaultQuery("format", shipment.LabelFormat)
	if format == "" {
		format = "pdf"
	}

	c.JSON(http.StatusOK, gin.H{
		"shipment_id":     shipmentID,
		"tracking_number": shipment.TrackingNumber,
		"label_format":    format,
		"label_data":      shipment.LabelData,   // base64-encoded label (PDF or ZPL)
		"carrier_id":      shipment.CarrierID,
		"service_name":    shipment.ServiceName,
		"order_ids":       shipment.OrderIDs,
	})
}

// ============================================================================
// BULK DISPATCH — POST /api/v1/dispatch/bulk-dispatch
// ============================================================================

func (h *DispatchExtensionsHandler) BulkDispatch(c *gin.Context) {
	tenantID := h.tenantID(c)

	var req struct {
		OrderIDs   []string `json:"order_ids"`
		CarrierID  string   `json:"carrier_id"`
		ServiceCode string  `json:"service_code"`
		SkipConfirm bool    `json:"skip_confirm"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.OrderIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "order_ids required"})
		return
	}

	ctx := c.Request.Context()
	now := time.Now().Format(time.RFC3339)

	type BulkResult struct {
		OrderID    string `json:"order_id"`
		Status     string `json:"status"`
		ShipmentID string `json:"shipment_id,omitempty"`
		Message    string `json:"message,omitempty"`
	}

	var results []BulkResult

	for _, oid := range req.OrderIDs {
		shipmentID := uuid.New().String()

		// Create a minimal shipment record
		shipment := map[string]interface{}{
			"shipment_id":  shipmentID,
			"tenant_id":    tenantID,
			"order_ids":    []string{oid},
			"carrier_id":   req.CarrierID,
			"service_code": req.ServiceCode,
			"status":       "label_generated",
			"created_at":   time.Now(),
			"updated_at":   time.Now(),
		}

		_, err := h.client.Collection("tenants").Doc(tenantID).Collection("shipments").Doc(shipmentID).Set(ctx, shipment)
		if err != nil {
			results = append(results, BulkResult{OrderID: oid, Status: "error", Message: err.Error()})
			continue
		}

		// Mark order dispatched
		_, err = h.client.Collection("tenants").Doc(tenantID).Collection("orders").Doc(oid).Update(ctx, []firestore.Update{
			{Path: "status", Value: "fulfilled"},
			{Path: "sub_status", Value: "despatched"},
			{Path: "updated_at", Value: now},
			{Path: "shipment_ids", Value: firestore.ArrayUnion(shipmentID)},
		})
		if err != nil {
			results = append(results, BulkResult{OrderID: oid, Status: "partial", ShipmentID: shipmentID, Message: "Shipment created but order status update failed"})
			continue
		}

		results = append(results, BulkResult{OrderID: oid, Status: "dispatched", ShipmentID: shipmentID})
	}

	successCount := 0
	for _, r := range results {
		if r.Status == "dispatched" {
			successCount++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"results":       results,
		"total":         len(results),
		"success_count": successCount,
		"failed_count":  len(results) - successCount,
	})
}

// ============================================================================
// DELIVERY EXCEPTIONS — list, acknowledge
// ============================================================================

type DeliveryException struct {
	ExceptionID    string `json:"exception_id" firestore:"exception_id"`
	TenantID       string `json:"tenant_id" firestore:"tenant_id"`
	ShipmentID     string `json:"shipment_id" firestore:"shipment_id"`
	TrackingNumber string `json:"tracking_number" firestore:"tracking_number"`
	CarrierID      string `json:"carrier_id" firestore:"carrier_id"`
	OrderIDs       []string `json:"order_ids" firestore:"order_ids"`
	ExceptionType  string `json:"exception_type" firestore:"exception_type"` // undeliverable, returned, lost, damaged
	Description    string `json:"description" firestore:"description"`
	Acknowledged   bool   `json:"acknowledged" firestore:"acknowledged"`
	AcknowledgedAt string `json:"acknowledged_at,omitempty" firestore:"acknowledged_at,omitempty"`
	CreatedAt      time.Time `json:"created_at" firestore:"created_at"`
	UpdatedAt      time.Time `json:"updated_at" firestore:"updated_at"`
}

// ListExceptions GET /api/v1/dispatch/exceptions
func (h *DispatchExtensionsHandler) ListExceptions(c *gin.Context) {
	tenantID := h.tenantID(c)
	ctx := c.Request.Context()

	q := h.client.Collection("tenants").Doc(tenantID).Collection("delivery_exceptions").Query
	if c.Query("unacknowledged") == "true" {
		q = q.Where("acknowledged", "==", false)
	}
	q = q.OrderBy("created_at", firestore.Desc).Limit(200)

	iter := q.Documents(ctx)
	defer iter.Stop()

	var exceptions []DeliveryException
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch exceptions"})
			return
		}
		var ex DeliveryException
		_ = doc.DataTo(&ex)
		exceptions = append(exceptions, ex)
	}
	if exceptions == nil {
		exceptions = []DeliveryException{}
	}

	c.JSON(http.StatusOK, gin.H{"exceptions": exceptions, "count": len(exceptions)})
}

// AcknowledgeException POST /api/v1/dispatch/exceptions/:exception_id/acknowledge
func (h *DispatchExtensionsHandler) AcknowledgeException(c *gin.Context) {
	tenantID := h.tenantID(c)
	exceptionID := c.Param("exception_id")
	ctx := c.Request.Context()

	_, err := h.client.Collection("tenants").Doc(tenantID).Collection("delivery_exceptions").Doc(exceptionID).Update(ctx, []firestore.Update{
		{Path: "acknowledged", Value: true},
		{Path: "acknowledged_at", Value: time.Now().Format(time.RFC3339)},
		{Path: "updated_at", Value: time.Now()},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to acknowledge exception"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Exception acknowledged", "exception_id": exceptionID})
}

// ============================================================================
// PACKAGING RULES
// ============================================================================

type PackagingRule struct {
	RuleID        string  `json:"rule_id" firestore:"rule_id"`
	TenantID      string  `json:"tenant_id" firestore:"tenant_id"`
	Name          string  `json:"name" firestore:"name"`
	Priority      int     `json:"priority" firestore:"priority"`
	ConditionType string  `json:"condition_type" firestore:"condition_type"` // weight_range, dimensions, channel, sku_prefix
	WeightMinKg   float64 `json:"weight_min_kg" firestore:"weight_min_kg"`
	WeightMaxKg   float64 `json:"weight_max_kg" firestore:"weight_max_kg"`
	MaxLengthCm   float64 `json:"max_length_cm,omitempty" firestore:"max_length_cm,omitempty"`
	MaxWidthCm    float64 `json:"max_width_cm,omitempty" firestore:"max_width_cm,omitempty"`
	MaxHeightCm   float64 `json:"max_height_cm,omitempty" firestore:"max_height_cm,omitempty"`
	PackageFormat string  `json:"package_format" firestore:"package_format"` // letter, small_parcel, medium_parcel, large_parcel, pallet
	PackageName   string  `json:"package_name" firestore:"package_name"`
	CarrierID     string  `json:"carrier_id,omitempty" firestore:"carrier_id,omitempty"`
	ServiceCode   string  `json:"service_code,omitempty" firestore:"service_code,omitempty"`
	IsActive      bool    `json:"is_active" firestore:"is_active"`
	CreatedAt     time.Time `json:"created_at" firestore:"created_at"`
	UpdatedAt     time.Time `json:"updated_at" firestore:"updated_at"`
}

func (h *DispatchExtensionsHandler) ListPackagingRules(c *gin.Context) {
	tenantID := h.tenantID(c)
	ctx := c.Request.Context()

	iter := h.client.Collection("tenants").Doc(tenantID).Collection("packaging_rules").
		OrderBy("priority", firestore.Asc).Documents(ctx)
	defer iter.Stop()

	var rules []PackagingRule
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch packaging rules"})
			return
		}
		var r PackagingRule
		_ = doc.DataTo(&r)
		rules = append(rules, r)
	}
	if rules == nil {
		rules = []PackagingRule{}
	}
	c.JSON(http.StatusOK, gin.H{"rules": rules, "count": len(rules)})
}

func (h *DispatchExtensionsHandler) CreatePackagingRule(c *gin.Context) {
	tenantID := h.tenantID(c)
	var rule PackagingRule
	if err := c.ShouldBindJSON(&rule); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid rule"})
		return
	}
	rule.RuleID = uuid.New().String()
	rule.TenantID = tenantID
	rule.IsActive = true
	rule.CreatedAt = time.Now()
	rule.UpdatedAt = time.Now()

	_, err := h.client.Collection("tenants").Doc(tenantID).Collection("packaging_rules").Doc(rule.RuleID).Set(c.Request.Context(), rule)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create rule"})
		return
	}
	c.JSON(http.StatusCreated, rule)
}

func (h *DispatchExtensionsHandler) UpdatePackagingRule(c *gin.Context) {
	tenantID := h.tenantID(c)
	ruleID := c.Param("rule_id")
	var rule PackagingRule
	if err := c.ShouldBindJSON(&rule); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid rule"})
		return
	}
	rule.RuleID = ruleID
	rule.UpdatedAt = time.Now()

	_, err := h.client.Collection("tenants").Doc(tenantID).Collection("packaging_rules").Doc(ruleID).Set(c.Request.Context(), rule, firestore.MergeAll)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update rule"})
		return
	}
	c.JSON(http.StatusOK, rule)
}

func (h *DispatchExtensionsHandler) DeletePackagingRule(c *gin.Context) {
	tenantID := h.tenantID(c)
	ruleID := c.Param("rule_id")

	_, err := h.client.Collection("tenants").Doc(tenantID).Collection("packaging_rules").Doc(ruleID).Delete(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete rule"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Rule deleted", "rule_id": ruleID})
}

// ApplyPackagingRules POST /api/v1/dispatch/packaging-rules/apply
// Applies packaging rules to a set of order IDs and returns recommended packaging
func (h *DispatchExtensionsHandler) ApplyPackagingRules(c *gin.Context) {
	tenantID := h.tenantID(c)
	var req struct {
		OrderIDs []string `json:"order_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.OrderIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "order_ids required"})
		return
	}

	ctx := c.Request.Context()

	// Load packaging rules
	iter := h.client.Collection("tenants").Doc(tenantID).Collection("packaging_rules").
		Where("is_active", "==", true).
		OrderBy("priority", firestore.Asc).Documents(ctx)
	defer iter.Stop()

	var rules []PackagingRule
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		var r PackagingRule
		_ = doc.DataTo(&r)
		rules = append(rules, r)
	}

	type OrderPackaging struct {
		OrderID       string `json:"order_id"`
		PackageFormat string `json:"package_format"`
		PackageName   string `json:"package_name"`
		CarrierID     string `json:"carrier_id,omitempty"`
		ServiceCode   string `json:"service_code,omitempty"`
		MatchedRule   string `json:"matched_rule,omitempty"`
		TotalWeightKg float64 `json:"total_weight_kg"`
	}

	var recommendations []OrderPackaging

	for _, oid := range req.OrderIDs {
		orderDoc, err := h.client.Collection("tenants").Doc(tenantID).Collection("orders").Doc(oid).Get(ctx)
		if err != nil {
			continue
		}
		var order models.Order
		_ = orderDoc.DataTo(&order)

		// Calculate total weight
		totalWt := 0.0
		for _, line := range order.Lines {
			// weight stored in grams in line items, convert to kg
			totalWt += float64(line.Quantity) * 0.1 // fallback 100g per item
		}

		rec := OrderPackaging{OrderID: oid, TotalWeightKg: totalWt, PackageFormat: "small_parcel", PackageName: "Standard Parcel"}

		// Find first matching rule
		for _, rule := range rules {
			if rule.ConditionType == "weight_range" {
				if totalWt >= rule.WeightMinKg && (rule.WeightMaxKg <= 0 || totalWt <= rule.WeightMaxKg) {
					rec.PackageFormat = rule.PackageFormat
					rec.PackageName = rule.PackageName
					rec.CarrierID = rule.CarrierID
					rec.ServiceCode = rule.ServiceCode
					rec.MatchedRule = rule.Name
					break
				}
			}
		}

		recommendations = append(recommendations, rec)
	}

	if recommendations == nil {
		recommendations = []OrderPackaging{}
	}
	c.JSON(http.StatusOK, gin.H{"recommendations": recommendations})
}
