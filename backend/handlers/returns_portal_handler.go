package handlers

// ============================================================================
// RETURNS PORTAL HANDLER — SESSION 3 (Task 6)
// ============================================================================
// Self-service returns portal for end-customers.  No session / tenant auth —
// the tenant is identified by the :tenant_id path segment. Order lookup uses
// postcode PII token matching (same mechanism as the internal order search).
//
// Routes (PUBLIC — no X-Tenant-Id / tenant middleware):
//   GET  /api/v1/public/returns/config/:tenant_id   — branding + portal config
//   POST /api/v1/public/returns/lookup              — find order by number + postcode
//   POST /api/v1/public/returns/submit              — create RMA from portal
//   GET  /api/v1/public/returns/rma/:rma_number     — customer status check
// ============================================================================

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"

	"module-a/models"
	"module-a/services"
)

// ─── Handler ──────────────────────────────────────────────────────────────────

type ReturnsPortalHandler struct {
	client     *firestore.Client
	piiService *services.PIIService
}

func NewReturnsPortalHandler(client *firestore.Client, piiService *services.PIIService) *ReturnsPortalHandler {
	return &ReturnsPortalHandler{
		client:     client,
		piiService: piiService,
	}
}

// ─── Types ────────────────────────────────────────────────────────────────────

type PortalConfig struct {
	TenantID       string `json:"tenant_id"`
	CompanyName    string `json:"company_name"`
	Enabled        bool   `json:"enabled"`
	PolicyText     string `json:"policy_text,omitempty"`
	WindowDays     int    `json:"window_days"`        // how many days after purchase returns are accepted
	RequireReason  bool   `json:"require_reason"`
	AllowExchange  bool   `json:"allow_exchange"`
}

type PortalOrderLine struct {
	LineID      string `json:"line_id"`
	ProductName string `json:"product_name"`
	SKU         string `json:"sku"`
	Quantity    int    `json:"quantity"`
}

type PortalOrder struct {
	OrderID     string            `json:"order_id"`
	OrderNumber string            `json:"order_number"`
	Channel     string            `json:"channel"`
	OrderDate   string            `json:"order_date"`
	Lines       []PortalOrderLine `json:"lines"`
}

// ─── GET /api/v1/public/returns/config/:tenant_id ────────────────────────────

func (h *ReturnsPortalHandler) GetConfig(c *gin.Context) {
	tenantID := c.Param("tenant_id")
	ctx := c.Request.Context()

	// Load tenant document for company name
	companyName := "Returns Portal"
	tenantDoc, err := h.client.Collection("tenants").Doc(tenantID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "tenant not found"})
		return
	}
	tenantData := tenantDoc.Data()
	if name, ok := tenantData["name"].(string); ok && name != "" {
		companyName = name
	}

	// Load portal-specific config from tenants/{id}/config/returns_portal
	cfg := PortalConfig{
		TenantID:    tenantID,
		CompanyName: companyName,
		Enabled:     true,
		WindowDays:  30,
		RequireReason: true,
		AllowExchange: false,
	}

	cfgDoc, err := h.client.Collection("tenants").Doc(tenantID).
		Collection("config").Doc("returns_portal").Get(ctx)
	if err == nil {
		data := cfgDoc.Data()
		if v, ok := data["enabled"].(bool); ok {
			cfg.Enabled = v
		}
		if v, ok := data["policy_text"].(string); ok {
			cfg.PolicyText = v
		}
		if v, ok := data["window_days"].(int64); ok && v > 0 {
			cfg.WindowDays = int(v)
		}
		if v, ok := data["require_reason"].(bool); ok {
			cfg.RequireReason = v
		}
		if v, ok := data["allow_exchange"].(bool); ok {
			cfg.AllowExchange = v
		}
	}

	if !cfg.Enabled {
		c.JSON(http.StatusForbidden, gin.H{"error": "returns portal is not enabled for this store"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"config": cfg})
}

// ─── POST /api/v1/public/returns/lookup ──────────────────────────────────────
// Body: { tenant_id, order_number, postcode }
// Returns: order lines safe for display (no PII beyond what was submitted)

func (h *ReturnsPortalHandler) LookupOrder(c *gin.Context) {
	ctx := c.Request.Context()

	var req struct {
		TenantID    string `json:"tenant_id" binding:"required"`
		OrderNumber string `json:"order_number" binding:"required"`
		Postcode    string `json:"postcode" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "order number and postcode are required"})
		return
	}

	req.OrderNumber = strings.TrimSpace(req.OrderNumber)
	req.Postcode = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(req.Postcode), " ", ""))

	// Verify tenant exists
	if _, err := h.client.Collection("tenants").Doc(req.TenantID).Get(ctx); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "store not found"})
		return
	}

	// Generate postcode token (same HMAC as internal search)
	postcodeToken := h.piiService.SearchToken(req.Postcode)

	// Query orders by order number (external_order_id) within this tenant
	ordersCol := h.client.Collection("tenants").Doc(req.TenantID).Collection("orders")

	// Try external_order_id first, then order_id
	var matchDoc *firestore.DocumentSnapshot

	iter := ordersCol.Where("external_order_id", "==", req.OrderNumber).
		Limit(5).Documents(ctx)
	defer iter.Stop()

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "lookup failed"})
			return
		}
		data := doc.Data()
		// Verify postcode token matches
		storedToken, _ := data["pii_postcode_token"].(string)
		if storedToken != "" && storedToken == postcodeToken {
			matchDoc = doc
			break
		}
		// Fallback: if no PII encryption, compare raw postcode
		if addr, ok := data["shipping_address"].(map[string]interface{}); ok {
			rawPostcode, _ := addr["postal_code"].(string)
			rawNorm := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(rawPostcode), " ", ""))
			if rawNorm == req.Postcode {
				matchDoc = doc
				break
			}
		}
	}

	if matchDoc == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no order found matching that order number and postcode"})
		return
	}

	data := matchDoc.Data()

	// Check returns window (configurable per tenant)
	windowDays := 30
	cfgDoc, err := h.client.Collection("tenants").Doc(req.TenantID).
		Collection("config").Doc("returns_portal").Get(ctx)
	if err == nil {
		if v, ok := cfgDoc.Data()["window_days"].(int64); ok && v > 0 {
			windowDays = int(v)
		}
	}

	orderDateStr, _ := data["order_date"].(string)
	if orderDateStr == "" {
		orderDateStr, _ = data["created_at"].(string)
	}
	if orderDateStr != "" {
		orderDate, parseErr := time.Parse(time.RFC3339, orderDateStr)
		if parseErr != nil {
			orderDate, parseErr = time.Parse("2006-01-02", orderDateStr)
		}
		if parseErr == nil && time.Since(orderDate) > time.Duration(windowDays)*24*time.Hour {
			c.JSON(http.StatusUnprocessableEntity, gin.H{
				"error": fmt.Sprintf("this order is outside the %d-day returns window", windowDays),
			})
			return
		}
	}

	// Build safe response — extract lines, don't return PII
	orderID, _ := data["order_id"].(string)
	if orderID == "" {
		orderID = matchDoc.Ref.ID
	}
	channel, _ := data["channel"].(string)

	var lines []PortalOrderLine
	linesRaw, _ := data["lines"].([]interface{})
	for _, lr := range linesRaw {
		lm, ok := lr.(map[string]interface{})
		if !ok {
			continue
		}
		lineID, _ := lm["line_id"].(string)
		if lineID == "" {
			lineID = uuid.New().String()
		}
		title, _ := lm["title"].(string)
		sku, _ := lm["sku"].(string)
		qty := 1
		if q, ok := lm["quantity"].(int64); ok {
			qty = int(q)
		}
		lines = append(lines, PortalOrderLine{
			LineID:      lineID,
			ProductName: title,
			SKU:         sku,
			Quantity:    qty,
		})
	}

	// Also check sub-collection lines if the top-level is empty
	if len(lines) == 0 {
		linesIter := h.client.Collection("tenants").Doc(req.TenantID).
			Collection("orders").Doc(orderID).Collection("lines").Limit(50).Documents(ctx)
		defer linesIter.Stop()
		for {
			ldoc, lerr := linesIter.Next()
			if lerr == iterator.Done {
				break
			}
			if lerr != nil {
				break
			}
			ld := ldoc.Data()
			lineID, _ := ld["line_id"].(string)
			if lineID == "" {
				lineID = ldoc.Ref.ID
			}
			title, _ := ld["title"].(string)
			sku, _ := ld["sku"].(string)
			qty := 1
			if q, ok := ld["quantity"].(int64); ok {
				qty = int(q)
			}
			lines = append(lines, PortalOrderLine{
				LineID:      lineID,
				ProductName: title,
				SKU:         sku,
				Quantity:    qty,
			})
		}
	}

	order := PortalOrder{
		OrderID:     orderID,
		OrderNumber: req.OrderNumber,
		Channel:     channel,
		OrderDate:   orderDateStr,
		Lines:       lines,
	}

	c.JSON(http.StatusOK, gin.H{"order": order})
}

// ─── POST /api/v1/public/returns/submit ──────────────────────────────────────
// Body: { tenant_id, order_id, order_number, customer_name, customer_email,
//         lines: [{ line_id, product_name, sku, qty_requested, reason_code, reason_detail }] }

func (h *ReturnsPortalHandler) SubmitReturn(c *gin.Context) {
	ctx := c.Request.Context()

	var req struct {
		TenantID    string `json:"tenant_id" binding:"required"`
		OrderID     string `json:"order_id"`
		OrderNumber string `json:"order_number"`
		CustomerName  string `json:"customer_name"`
		CustomerEmail string `json:"customer_email"`
		Lines []struct {
			LineID       string `json:"line_id"`
			ProductName  string `json:"product_name"`
			SKU          string `json:"sku"`
			QtyRequested int    `json:"qty_requested"`
			ReasonCode   string `json:"reason_code"`
			ReasonDetail string `json:"reason_detail"`
		} `json:"lines" binding:"required,min=1"`
		Notes string `json:"notes"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	if len(req.Lines) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "at least one return line is required"})
		return
	}

	// Verify tenant exists
	if _, err := h.client.Collection("tenants").Doc(req.TenantID).Get(ctx); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "store not found"})
		return
	}

	// Build RMA document
	rmaID := uuid.New().String()
	rmaNumber := h.nextRMANumber(ctx, req.TenantID)
	now := time.Now()

	rmaLines := make([]models.RMALine, 0, len(req.Lines))
	for _, l := range req.Lines {
		lineID := l.LineID
		if lineID == "" {
			lineID = uuid.New().String()
		}
		qty := l.QtyRequested
		if qty < 1 {
			qty = 1
		}
		rmaLines = append(rmaLines, models.RMALine{
			LineID:       lineID,
			ProductName:  l.ProductName,
			SKU:          l.SKU,
			QtyRequested: qty,
			ReasonCode:   l.ReasonCode,
			ReasonDetail: l.ReasonDetail,
		})
	}

	rma := models.RMA{
		RMAID:       rmaID,
		TenantID:    req.TenantID,
		RMANumber:   rmaNumber,
		OrderID:     req.OrderID,
		OrderNumber: req.OrderNumber,
		Channel:     "portal",
		Status:      models.RMAStatusRequested,
		Customer: models.RMACustomer{
			Name:  req.CustomerName,
			Email: req.CustomerEmail,
		},
		Lines:     rmaLines,
		Notes:     req.Notes,
		CreatedBy: "portal",
		CreatedAt: now,
		UpdatedAt: now,
		Timeline: []models.RMAEvent{
			{
				EventID:   uuid.New().String(),
				Status:    models.RMAStatusRequested,
				Note:      "Return request submitted via customer portal",
				CreatedBy: "portal",
				CreatedAt: now,
			},
		},
	}

	rmaRef := h.client.Collection("tenants").Doc(req.TenantID).Collection("rmas").Doc(rmaID)
	if _, err := rmaRef.Set(ctx, rma); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create return request"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"rma_id":     rmaID,
		"rma_number": rmaNumber,
		"status":     models.RMAStatusRequested,
		"message":    fmt.Sprintf("Your return request %s has been received. Please keep this reference number.", rmaNumber),
	})
}

// ─── GET /api/v1/public/returns/rma/:rma_number ──────────────────────────────
// Customer status check — requires tenant_id as query param.

func (h *ReturnsPortalHandler) GetRMAStatus(c *gin.Context) {
	tenantID := c.Query("tenant_id")
	rmaNumber := c.Param("rma_number")
	ctx := c.Request.Context()

	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant_id is required"})
		return
	}

	iter := h.client.Collection("tenants").Doc(tenantID).Collection("rmas").
		Where("rma_number", "==", rmaNumber).
		Limit(1).Documents(ctx)
	defer iter.Stop()

	doc, err := iter.Next()
	if err == iterator.Done {
		c.JSON(http.StatusNotFound, gin.H{"error": "return request not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "lookup failed"})
		return
	}

	var rma models.RMA
	doc.DataTo(&rma)

	// Return a customer-safe subset — no internal notes or inspection details
	c.JSON(http.StatusOK, gin.H{
		"rma_number": rma.RMANumber,
		"status":     rma.Status,
		"created_at": rma.CreatedAt,
		"updated_at": rma.UpdatedAt,
		"lines": func() []map[string]interface{} {
			out := make([]map[string]interface{}, 0, len(rma.Lines))
			for _, l := range rma.Lines {
				out = append(out, map[string]interface{}{
					"product_name":  l.ProductName,
					"sku":           l.SKU,
					"qty_requested": l.QtyRequested,
					"reason_code":   l.ReasonCode,
				})
			}
			return out
		}(),
		"status_label": rmaStatusLabel(rma.Status),
	})
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// nextRMANumber generates the next sequential RMA number for the tenant.
// Mirrors the logic in rma_handler.go to stay consistent.
func (h *ReturnsPortalHandler) nextRMANumber(ctx interface {
	Deadline() (time.Time, bool)
	Done() <-chan struct{}
	Err() error
	Value(key interface{}) interface{}
}, tenantID string) string {
	year := time.Now().Year()
	prefix := fmt.Sprintf("RMA-%d-", year)

	iter := h.client.Collection(fmt.Sprintf("tenants/%s/rmas", tenantID)).
		Where("rma_number", ">=", prefix).
		Where("rma_number", "<", fmt.Sprintf("RMA-%d-", year+1)).
		OrderBy("rma_number", firestore.Desc).
		Limit(1).
		Documents(ctx)
	defer iter.Stop()

	doc, err := iter.Next()
	if err != nil {
		return fmt.Sprintf("%s0001", prefix)
	}
	var rma models.RMA
	if err := doc.DataTo(&rma); err != nil {
		return fmt.Sprintf("%s0001", prefix)
	}
	numStr := rma.RMANumber[len(prefix):]
	n := 0
	fmt.Sscanf(numStr, "%d", &n)
	return fmt.Sprintf("%s%04d", prefix, n+1)
}

func rmaStatusLabel(status string) string {
	switch status {
	case models.RMAStatusRequested:
		return "Return requested — awaiting authorisation"
	case models.RMAStatusAuthorised:
		return "Return authorised — please send your item(s) back"
	case models.RMAStatusAwaitingReturn:
		return "Awaiting receipt of your return"
	case models.RMAStatusReceived:
		return "Items received — being inspected"
	case models.RMAStatusInspected:
		return "Inspection complete"
	case models.RMAStatusResolved:
		return "Return resolved"
	default:
		return status
	}
}
