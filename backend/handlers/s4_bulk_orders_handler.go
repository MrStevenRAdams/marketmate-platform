package handlers

// ============================================================================
// S4 BULK ORDER OPERATIONS HANDLER — Session 6.2
// ============================================================================
// Bulk operations for S4 channel order handlers:
//   POST /backmarket/orders/bulk/ship  — bulk ship with tracking
//   GET  /backmarket/orders/bulk/export — export orders as xlsx-ready JSON
//   POST /zalando/orders/bulk/ship
//   GET  /zalando/orders/bulk/export
//   POST /bol/orders/bulk/ship
//   GET  /bol/orders/bulk/export
//   POST /lazada/orders/bulk/ship
//   GET  /lazada/orders/bulk/export
//   POST /orders/packing-slip          — HTML packing slip for any channel
// ============================================================================

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"module-a/marketplace/clients/backmarket"
	"module-a/marketplace/clients/bol"
	"module-a/marketplace/clients/lazada"
	"module-a/marketplace/clients/zalando"
	"module-a/models"
	"module-a/services"
)

// ── Shared request/response types ────────────────────────────────────────────

type BulkShipItem struct {
	OrderID        string `json:"order_id" binding:"required"`
	TrackingNumber string `json:"tracking_number" binding:"required"`
	Carrier        string `json:"carrier"`
}

type BulkShipResult struct {
	OrderID string `json:"order_id"`
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
}

type BulkShipRequest struct {
	CredentialID string         `json:"credential_id" binding:"required"`
	Items        []BulkShipItem `json:"items" binding:"required,min=1"`
}

// ============================================================================
// BACK MARKET BULK OPERATIONS
// ============================================================================

type BackMarketBulkHandler struct {
	orderService       *services.OrderService
	marketplaceService *services.MarketplaceService
}

func NewBackMarketBulkHandler(orderService *services.OrderService, marketplaceService *services.MarketplaceService) *BackMarketBulkHandler {
	return &BackMarketBulkHandler{orderService: orderService, marketplaceService: marketplaceService}
}

func (h *BackMarketBulkHandler) buildClient(c *gin.Context, credentialID string) (*backmarket.Client, error) {
	tenantID := c.GetString("tenant_id")
	cred, err := h.marketplaceService.GetCredential(c.Request.Context(), tenantID, credentialID)
	if err != nil {
		return nil, err
	}
	merged, err := h.marketplaceService.GetFullCredentials(c.Request.Context(), cred)
	if err != nil {
		return nil, err
	}
	if merged["api_key"] == "" {
		return nil, fmt.Errorf("incomplete Back Market credentials")
	}
	return backmarket.NewClient(merged["api_key"], cred.Environment == "production"), nil
}

// BulkShip — POST /backmarket/orders/bulk/ship
func (h *BackMarketBulkHandler) BulkShip(c *gin.Context) {
	var req BulkShipRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	client, err := h.buildClient(c, req.CredentialID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	results := make([]BulkShipResult, 0, len(req.Items))
	for _, item := range req.Items {
		orderIDInt, parseErr := strconv.Atoi(item.OrderID)
		if parseErr != nil {
			results = append(results, BulkShipResult{OrderID: item.OrderID, OK: false, Error: "invalid order_id format"})
			continue
		}
		shipReq := backmarket.ShipOrderRequest{
			TrackingCode: item.TrackingNumber,
			Carrier:      item.Carrier,
			Mode:         "carrier_tracking",
		}
		if shipErr := client.ShipOrder(orderIDInt, shipReq); shipErr != nil {
			log.Printf("[BackMarket BulkShip] order %s: %v", item.OrderID, shipErr)
			results = append(results, BulkShipResult{OrderID: item.OrderID, OK: false, Error: shipErr.Error()})
		} else {
			results = append(results, BulkShipResult{OrderID: item.OrderID, OK: true})
		}
	}

	okCount := 0
	for _, r := range results {
		if r.OK {
			okCount++
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"results":     results,
		"total":       len(results),
		"success":     okCount,
		"failed":      len(results) - okCount,
	})
}

// BulkExport — GET /backmarket/orders/bulk/export
// Returns headers + rows JSON; frontend converts to XLSX.
func (h *BackMarketBulkHandler) BulkExport(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	opts := services.OrderListOptions{
		Channel: "backmarket",
		Limit:   c.DefaultQuery("limit", "500"),
		Offset:  c.DefaultQuery("offset", "0"),
		Status:  c.Query("status"),
	}
	orders, total, err := h.orderService.ListOrders(c.Request.Context(), tenantID, opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	headers := []string{"Order ID", "External Order ID", "Status", "Order Date", "Customer Name", "Customer Email", "Shipping Name", "Shipping Address", "Shipping City", "Shipping Postcode", "Shipping Country", "Grand Total", "Currency", "Channel Account"}
	rows := make([][]string, 0, len(orders))
	for _, o := range orders {
		currency := o.Totals.GrandTotal.Currency
		rows = append(rows, []string{
			o.OrderID,
			o.ExternalOrderID,
			o.Status,
			o.OrderDate,
			o.Customer.Name,
			o.Customer.Email,
			o.ShippingAddress.Name,
			strings.TrimSpace(o.ShippingAddress.AddressLine1 + " " + o.ShippingAddress.AddressLine2),
			o.ShippingAddress.City,
			o.ShippingAddress.PostalCode,
			o.ShippingAddress.Country,
			fmt.Sprintf("%.2f", o.Totals.GrandTotal.Amount),
			currency,
			o.ChannelAccountID,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":         true,
		"headers":    headers,
		"rows":       rows,
		"total":      total,
		"filename":   fmt.Sprintf("backmarket_orders_%s.xlsx", time.Now().Format("20060102")),
		"exported_at": time.Now().UTC().Format(time.RFC3339),
	})
}

// ============================================================================
// ZALANDO BULK OPERATIONS
// ============================================================================

type ZalandoBulkHandler struct {
	orderService       *services.OrderService
	marketplaceService *services.MarketplaceService
}

func NewZalandoBulkHandler(orderService *services.OrderService, marketplaceService *services.MarketplaceService) *ZalandoBulkHandler {
	return &ZalandoBulkHandler{orderService: orderService, marketplaceService: marketplaceService}
}

func (h *ZalandoBulkHandler) buildClient(c *gin.Context, credentialID string) (*zalando.Client, error) {
	tenantID := c.GetString("tenant_id")
	cred, err := h.marketplaceService.GetCredential(c.Request.Context(), tenantID, credentialID)
	if err != nil {
		return nil, err
	}
	merged, err := h.marketplaceService.GetFullCredentials(c.Request.Context(), cred)
	if err != nil {
		return nil, err
	}
	if merged["client_id"] == "" || merged["client_secret"] == "" {
		return nil, fmt.Errorf("incomplete Zalando credentials")
	}
	return zalando.NewClient(merged["client_id"], merged["client_secret"], cred.Environment == "production"), nil
}

// BulkShip — POST /zalando/orders/bulk/ship
func (h *ZalandoBulkHandler) BulkShip(c *gin.Context) {
	var req struct {
		CredentialID string `json:"credential_id" binding:"required"`
		Items        []struct {
			OrderID        string   `json:"order_id" binding:"required"`
			TrackingNumber string   `json:"tracking_number" binding:"required"`
			Carrier        string   `json:"carrier"`
			LineItemIDs    []string `json:"line_item_ids"`
		} `json:"items" binding:"required,min=1"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	client, err := h.buildClient(c, req.CredentialID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	results := make([]BulkShipResult, 0, len(req.Items))
	for _, item := range req.Items {
		if shipErr := client.ShipOrder(item.OrderID, item.TrackingNumber, item.Carrier, item.LineItemIDs); shipErr != nil {
			log.Printf("[Zalando BulkShip] order %s: %v", item.OrderID, shipErr)
			results = append(results, BulkShipResult{OrderID: item.OrderID, OK: false, Error: shipErr.Error()})
		} else {
			results = append(results, BulkShipResult{OrderID: item.OrderID, OK: true})
		}
	}

	okCount := 0
	for _, r := range results {
		if r.OK {
			okCount++
		}
	}
	c.JSON(http.StatusOK, gin.H{"results": results, "total": len(results), "success": okCount, "failed": len(results) - okCount})
}

// BulkExport — GET /zalando/orders/bulk/export
func (h *ZalandoBulkHandler) BulkExport(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	opts := services.OrderListOptions{
		Channel: "zalando",
		Limit:   c.DefaultQuery("limit", "500"),
		Offset:  c.DefaultQuery("offset", "0"),
		Status:  c.Query("status"),
	}
	orders, total, err := h.orderService.ListOrders(c.Request.Context(), tenantID, opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	headers := []string{"Order ID", "External Order ID", "Status", "Order Date", "Customer Name", "Shipping Name", "Shipping Address", "Shipping City", "Shipping Postcode", "Shipping Country", "Grand Total", "Currency"}
	rows := make([][]string, 0, len(orders))
	for _, o := range orders {
		rows = append(rows, []string{
			o.OrderID, o.ExternalOrderID, o.Status, o.OrderDate, o.Customer.Name,
			o.ShippingAddress.Name,
			strings.TrimSpace(o.ShippingAddress.AddressLine1 + " " + o.ShippingAddress.AddressLine2),
			o.ShippingAddress.City, o.ShippingAddress.PostalCode, o.ShippingAddress.Country,
			fmt.Sprintf("%.2f", o.Totals.GrandTotal.Amount),
			o.Totals.GrandTotal.Currency,
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"ok": true, "headers": headers, "rows": rows, "total": total,
		"filename":    fmt.Sprintf("zalando_orders_%s.xlsx", time.Now().Format("20060102")),
		"exported_at": time.Now().UTC().Format(time.RFC3339),
	})
}

// ============================================================================
// BOL.COM BULK OPERATIONS
// ============================================================================

type BolBulkHandler struct {
	orderService       *services.OrderService
	marketplaceService *services.MarketplaceService
}

func NewBolBulkHandler(orderService *services.OrderService, marketplaceService *services.MarketplaceService) *BolBulkHandler {
	return &BolBulkHandler{orderService: orderService, marketplaceService: marketplaceService}
}

func (h *BolBulkHandler) buildClient(c *gin.Context, credentialID string) (*bol.Client, error) {
	tenantID := c.GetString("tenant_id")
	cred, err := h.marketplaceService.GetCredential(c.Request.Context(), tenantID, credentialID)
	if err != nil {
		return nil, err
	}
	merged, err := h.marketplaceService.GetFullCredentials(c.Request.Context(), cred)
	if err != nil {
		return nil, err
	}
	if merged["client_id"] == "" || merged["client_secret"] == "" {
		return nil, fmt.Errorf("incomplete Bol.com credentials")
	}
	return bol.NewClient(merged["client_id"], merged["client_secret"]), nil
}

// BulkShip — POST /bol/orders/bulk/ship
func (h *BolBulkHandler) BulkShip(c *gin.Context) {
	var req struct {
		CredentialID string `json:"credential_id" binding:"required"`
		Items        []struct {
			OrderItemID    string `json:"order_id" binding:"required"` // Bol ships by order_item_id
			TrackingNumber string `json:"tracking_number" binding:"required"`
			Carrier        string `json:"carrier"`
		} `json:"items" binding:"required,min=1"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	client, err := h.buildClient(c, req.CredentialID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	results := make([]BulkShipResult, 0, len(req.Items))
	for _, item := range req.Items {
		shipReq := bol.ShipmentRequest{
			OrderItems: []bol.ShipmentItem{{OrderItemID: item.OrderItemID, Quantity: 1}},
			Transport:  bol.Transport{TrackAndTrace: item.TrackingNumber, TransporterCode: item.Carrier},
		}
		if shipErr := client.CreateShipment(shipReq); shipErr != nil {
			log.Printf("[Bol BulkShip] item %s: %v", item.OrderItemID, shipErr)
			results = append(results, BulkShipResult{OrderID: item.OrderItemID, OK: false, Error: shipErr.Error()})
		} else {
			results = append(results, BulkShipResult{OrderID: item.OrderItemID, OK: true})
		}
	}

	okCount := 0
	for _, r := range results {
		if r.OK {
			okCount++
		}
	}
	c.JSON(http.StatusOK, gin.H{"results": results, "total": len(results), "success": okCount, "failed": len(results) - okCount})
}

// BulkExport — GET /bol/orders/bulk/export
func (h *BolBulkHandler) BulkExport(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	opts := services.OrderListOptions{
		Channel: "bol",
		Limit:   c.DefaultQuery("limit", "500"),
		Offset:  c.DefaultQuery("offset", "0"),
		Status:  c.Query("status"),
	}
	orders, total, err := h.orderService.ListOrders(c.Request.Context(), tenantID, opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	headers := []string{"Order ID", "External Order ID", "Status", "Order Date", "Customer Name", "Customer Email", "Shipping Name", "Shipping Address", "Shipping City", "Shipping Postcode", "Shipping Country", "Grand Total", "Currency"}
	rows := make([][]string, 0, len(orders))
	for _, o := range orders {
		rows = append(rows, []string{
			o.OrderID, o.ExternalOrderID, o.Status, o.OrderDate, o.Customer.Name, o.Customer.Email,
			o.ShippingAddress.Name,
			strings.TrimSpace(o.ShippingAddress.AddressLine1 + " " + o.ShippingAddress.AddressLine2),
			o.ShippingAddress.City, o.ShippingAddress.PostalCode, o.ShippingAddress.Country,
			fmt.Sprintf("%.2f", o.Totals.GrandTotal.Amount),
			o.Totals.GrandTotal.Currency,
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"ok": true, "headers": headers, "rows": rows, "total": total,
		"filename":    fmt.Sprintf("bol_orders_%s.xlsx", time.Now().Format("20060102")),
		"exported_at": time.Now().UTC().Format(time.RFC3339),
	})
}

// ============================================================================
// LAZADA BULK OPERATIONS
// ============================================================================

type LazadaBulkHandler struct {
	orderService       *services.OrderService
	marketplaceService *services.MarketplaceService
}

func NewLazadaBulkHandler(orderService *services.OrderService, marketplaceService *services.MarketplaceService) *LazadaBulkHandler {
	return &LazadaBulkHandler{orderService: orderService, marketplaceService: marketplaceService}
}

func (h *LazadaBulkHandler) buildClient(c *gin.Context, credentialID string) (*lazada.Client, error) {
	tenantID := c.GetString("tenant_id")
	cred, err := h.marketplaceService.GetCredential(c.Request.Context(), tenantID, credentialID)
	if err != nil {
		return nil, err
	}
	merged, err := h.marketplaceService.GetFullCredentials(c.Request.Context(), cred)
	if err != nil {
		return nil, err
	}
	if merged["app_key"] == "" || merged["app_secret"] == "" || merged["access_token"] == "" {
		return nil, fmt.Errorf("incomplete Lazada credentials")
	}
	baseURL := merged["base_url"]
	if baseURL == "" {
		baseURL = "https://api.lazada.com.my/rest"
	}
	return lazada.NewClient(merged["app_key"], merged["app_secret"], merged["access_token"], baseURL), nil
}

// BulkShip — POST /lazada/orders/bulk/ship
func (h *LazadaBulkHandler) BulkShip(c *gin.Context) {
	var req struct {
		CredentialID string `json:"credential_id" binding:"required"`
		Items        []struct {
			OrderID        string  `json:"order_id" binding:"required"`
			OrderItemIDs   []int64 `json:"order_item_ids" binding:"required"`
			TrackingNumber string  `json:"tracking_number" binding:"required"`
			Carrier        string  `json:"carrier"`
		} `json:"items" binding:"required,min=1"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	client, err := h.buildClient(c, req.CredentialID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	results := make([]BulkShipResult, 0, len(req.Items))
	for _, item := range req.Items {
		provider := item.Carrier
		if provider == "" {
			provider = "MANUAL"
		}
		if shipErr := client.SetReadyToShip(item.OrderItemIDs, provider, item.TrackingNumber); shipErr != nil {
			log.Printf("[Lazada BulkShip] order %s: %v", item.OrderID, shipErr)
			results = append(results, BulkShipResult{OrderID: item.OrderID, OK: false, Error: shipErr.Error()})
		} else {
			results = append(results, BulkShipResult{OrderID: item.OrderID, OK: true})
		}
	}

	okCount := 0
	for _, r := range results {
		if r.OK {
			okCount++
		}
	}
	c.JSON(http.StatusOK, gin.H{"results": results, "total": len(results), "success": okCount, "failed": len(results) - okCount})
}

// BulkExport — GET /lazada/orders/bulk/export
func (h *LazadaBulkHandler) BulkExport(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	opts := services.OrderListOptions{
		Channel: "lazada",
		Limit:   c.DefaultQuery("limit", "500"),
		Offset:  c.DefaultQuery("offset", "0"),
		Status:  c.Query("status"),
	}
	orders, total, err := h.orderService.ListOrders(c.Request.Context(), tenantID, opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	headers := []string{"Order ID", "External Order ID", "Status", "Order Date", "Customer Name", "Customer Phone", "Shipping Name", "Shipping Address", "Shipping City", "Shipping Postcode", "Shipping Country", "Grand Total", "Currency"}
	rows := make([][]string, 0, len(orders))
	for _, o := range orders {
		rows = append(rows, []string{
			o.OrderID, o.ExternalOrderID, o.Status, o.OrderDate, o.Customer.Name, o.Customer.Phone,
			o.ShippingAddress.Name,
			strings.TrimSpace(o.ShippingAddress.AddressLine1 + " " + o.ShippingAddress.AddressLine2),
			o.ShippingAddress.City, o.ShippingAddress.PostalCode, o.ShippingAddress.Country,
			fmt.Sprintf("%.2f", o.Totals.GrandTotal.Amount),
			o.Totals.GrandTotal.Currency,
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"ok": true, "headers": headers, "rows": rows, "total": total,
		"filename":    fmt.Sprintf("lazada_orders_%s.xlsx", time.Now().Format("20060102")),
		"exported_at": time.Now().UTC().Format(time.RFC3339),
	})
}

// ============================================================================
// PACKING SLIP HANDLER (channel-agnostic)
// ============================================================================
// POST /orders/packing-slip
// Body: { "order_id": "...", "shipping_method": "Royal Mail 48" }
// Returns: { "html": "..." }
//
// Uses the existing TemplateService + BuildRenderData pipeline.
// If no packing_slip template is found, generates a sensible default.

type PackingSlipHandler struct {
	orderService    *services.OrderService
	templateService *services.TemplateService
}

func NewPackingSlipHandler(orderService *services.OrderService, templateService *services.TemplateService) *PackingSlipHandler {
	return &PackingSlipHandler{orderService: orderService, templateService: templateService}
}

func (h *PackingSlipHandler) GeneratePackingSlip(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	var req struct {
		OrderID        string `json:"order_id" binding:"required"`
		ShippingMethod string `json:"shipping_method"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()

	order, err := h.orderService.GetOrder(ctx, tenantID, req.OrderID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "order not found: " + err.Error()})
		return
	}

	lineValues, err := h.orderService.GetOrderLines(ctx, tenantID, req.OrderID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load order lines: " + err.Error()})
		return
	}
	// BuildRenderData takes []*models.OrderLine
	lines := make([]*models.OrderLine, len(lineValues))
	for i := range lineValues {
		lines[i] = &lineValues[i]
	}

	renderData, err := h.templateService.BuildRenderData(ctx, tenantID, order, lines, req.ShippingMethod)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to build render data: " + err.Error()})
		return
	}

	// Generate packing slip HTML using built-in template.
	// (The Template model uses a JSON block system rather than raw HTMLContent,
	// so we always render with the built-in HTML generator here.)
	html := buildDefaultPackingSlipHTML(renderData)

	c.JSON(http.StatusOK, gin.H{
		"html":     html,
		"order_id": req.OrderID,
		"channel":  order.Channel,
	})
}

// buildDefaultPackingSlipHTML generates a clean built-in packing slip.
func buildDefaultPackingSlipHTML(data *models.TemplateRenderData) string {
	var sb strings.Builder

	sb.WriteString(`<!DOCTYPE html><html><head><meta charset="utf-8">
<title>Packing Slip</title>
<style>
  body { font-family: Arial, sans-serif; font-size: 12px; color: #333; margin: 0; padding: 20px; }
  .header { display: flex; justify-content: space-between; margin-bottom: 20px; border-bottom: 2px solid #333; padding-bottom: 12px; }
  .seller h2 { margin: 0 0 4px; font-size: 16px; }
  .order-meta { text-align: right; }
  .order-meta h3 { margin: 0 0 4px; font-size: 14px; }
  .addresses { display: flex; gap: 40px; margin-bottom: 20px; }
  .address-box { flex: 1; }
  .address-box h4 { margin: 0 0 6px; font-size: 11px; text-transform: uppercase; color: #666; letter-spacing: 0.05em; }
  table { width: 100%; border-collapse: collapse; margin-bottom: 20px; }
  th { background: #f5f5f5; text-align: left; padding: 6px 8px; font-size: 11px; border-bottom: 1px solid #ddd; }
  td { padding: 6px 8px; border-bottom: 1px solid #eee; vertical-align: top; }
  .totals { text-align: right; margin-top: 10px; }
  .totals p { margin: 2px 0; }
  .totals strong { display: inline-block; width: 100px; text-align: left; }
  .shipping-method { margin-top: 10px; font-size: 11px; color: #666; }
  @media print { body { padding: 0; } }
</style>
</head><body>`)

	// Header
	sb.WriteString(`<div class="header"><div class="seller">`)
	if data.Seller.LogoURL != "" {
		sb.WriteString(fmt.Sprintf(`<img src="%s" style="height:40px;margin-bottom:6px;" alt="logo"><br>`, data.Seller.LogoURL))
	}
	sb.WriteString(fmt.Sprintf(`<h2>%s</h2>`, data.Seller.Name))
	if data.Seller.Address != "" {
		sb.WriteString(fmt.Sprintf(`<div>%s</div>`, strings.ReplaceAll(data.Seller.Address, "\n", "<br>")))
	}
	if data.Seller.VATNumber != "" {
		sb.WriteString(fmt.Sprintf(`<div>VAT: %s</div>`, data.Seller.VATNumber))
	}
	sb.WriteString(`</div><div class="order-meta">`)
	sb.WriteString(`<h3>PACKING SLIP</h3>`)
	sb.WriteString(fmt.Sprintf(`<div><strong>Order:</strong> %s</div>`, data.Order.ID))
	sb.WriteString(fmt.Sprintf(`<div><strong>Date:</strong> %s</div>`, data.Order.Date))
	sb.WriteString(fmt.Sprintf(`<div><strong>Status:</strong> %s</div>`, data.Order.Status))
	sb.WriteString(`</div></div>`)

	// Addresses
	sb.WriteString(`<div class="addresses">`)
	sb.WriteString(`<div class="address-box"><h4>Ship To</h4>`)
	sb.WriteString(fmt.Sprintf(`<strong>%s</strong><br>`, data.Shipping.Name))
	if data.Shipping.AddressLine1 != "" {
		sb.WriteString(fmt.Sprintf(`%s<br>`, data.Shipping.AddressLine1))
	}
	if data.Shipping.AddressLine2 != "" {
		sb.WriteString(fmt.Sprintf(`%s<br>`, data.Shipping.AddressLine2))
	}
	sb.WriteString(fmt.Sprintf(`%s %s<br>%s</div>`, data.Shipping.City, data.Shipping.PostalCode, data.Shipping.Country))
	sb.WriteString(`<div class="address-box"><h4>Customer</h4>`)
	sb.WriteString(fmt.Sprintf(`<strong>%s</strong>`, data.Customer.Name))
	if data.Customer.Email != "" {
		sb.WriteString(fmt.Sprintf(`<br>%s`, data.Customer.Email))
	}
	if data.Customer.Phone != "" {
		sb.WriteString(fmt.Sprintf(`<br>%s`, data.Customer.Phone))
	}
	sb.WriteString(`</div></div>`)

	// Lines table
	sb.WriteString(`<table><thead><tr><th>SKU</th><th>Description</th><th>Qty</th><th>Unit Price</th><th>Line Total</th></tr></thead><tbody>`)
	for _, line := range data.Lines {
		sb.WriteString(fmt.Sprintf(`<tr><td>%s</td><td>%s</td><td>%d</td><td>%s</td><td>%s</td></tr>`,
			line.SKU, line.Title, line.Quantity, line.UnitPrice, line.LineTotal))
	}
	sb.WriteString(`</tbody></table>`)

	// Totals
	sb.WriteString(`<div class="totals">`)
	if data.Order.Subtotal != "" && data.Order.Subtotal != "£0.00" && data.Order.Subtotal != "$0.00" {
		sb.WriteString(fmt.Sprintf(`<p><strong>Subtotal:</strong> %s</p>`, data.Order.Subtotal))
	}
	if data.Order.ShippingCost != "" && data.Order.ShippingCost != "£0.00" && data.Order.ShippingCost != "$0.00" {
		sb.WriteString(fmt.Sprintf(`<p><strong>Shipping:</strong> %s</p>`, data.Order.ShippingCost))
	}
	if data.Order.Tax != "" && data.Order.Tax != "£0.00" && data.Order.Tax != "$0.00" {
		sb.WriteString(fmt.Sprintf(`<p><strong>Tax:</strong> %s</p>`, data.Order.Tax))
	}
	sb.WriteString(fmt.Sprintf(`<p style="font-size:14px;font-weight:bold;"><strong>Total:</strong> %s</p>`, data.Order.Total))
	sb.WriteString(`</div>`)

	if data.Shipping.Method != "" {
		sb.WriteString(fmt.Sprintf(`<div class="shipping-method">Shipping method: %s</div>`, data.Shipping.Method))
	}

	sb.WriteString(`</body></html>`)
	return sb.String()
}
