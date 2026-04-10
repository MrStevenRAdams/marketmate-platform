package handlers

// ============================================================================
// SHOPWIRED ORDERS HANDLER
// ============================================================================
// Routes:
//   POST /shopwired/orders/import     → pull orders from ShopWired, save to MarketMate
//   GET  /shopwired/orders            → list raw ShopWired orders (debug)
//   POST /shopwired/orders/:id/ship   → push tracking number to order
//   POST /shopwired/orders/:id/status → update order status
// Webhook:
//   POST /webhooks/orders/shopwired   → receive ShopWired order webhook
// ============================================================================

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"module-a/marketplace/clients/shopwired"
	"module-a/models"
	"module-a/services"
)

type ShopWiredOrdersHandler struct {
	orderService       *services.OrderService
	marketplaceService *services.MarketplaceService
}

func NewShopWiredOrdersHandler(
	orderService *services.OrderService,
	marketplaceService *services.MarketplaceService,
) *ShopWiredOrdersHandler {
	return &ShopWiredOrdersHandler{
		orderService:       orderService,
		marketplaceService: marketplaceService,
	}
}

// ── Client resolution ─────────────────────────────────────────────────────────

func (h *ShopWiredOrdersHandler) buildClient(ctx context.Context, tenantID, credentialID string) (*shopwired.Client, error) {
	cred, err := h.marketplaceService.GetCredential(ctx, tenantID, credentialID)
	if err != nil {
		return nil, fmt.Errorf("get credential: %w", err)
	}
	merged, err := h.marketplaceService.GetFullCredentials(ctx, cred)
	if err != nil {
		return nil, fmt.Errorf("merge credentials: %w", err)
	}

	apiKey := merged["api_key"]
	apiSecret := merged["api_secret"]

	if apiKey == "" || apiSecret == "" {
		return nil, fmt.Errorf("incomplete ShopWired credentials (api_key and api_secret required)")
	}

	return shopwired.NewClient(apiKey, apiSecret), nil
}

// ── Import Orders ─────────────────────────────────────────────────────────────

// ImportShopWiredOrders is the service-layer method called by OrderPoller and
// OrderWebhook. Matches the (ctx, tenantID, credID, from, to) → (int, error)
// signature expected by processChannelImport.
func (h *ShopWiredOrdersHandler) ImportShopWiredOrders(ctx context.Context, tenantID, credentialID string, createdAfter, createdBefore time.Time) (int, error) {
	client, err := h.buildClient(ctx, tenantID, credentialID)
	if err != nil {
		return 0, fmt.Errorf("build client: %w", err)
	}

	// ShopWired doesn't support date filtering on ListOrders — fetch recent and filter
	orders, err := client.ListOrders(0, 250, "")
	if err != nil {
		return 0, fmt.Errorf("fetch orders: %w", err)
	}

	imported := 0
	for _, swOrder := range orders {
		// Filter by date window (CreatedAt is a string — parse and compare)
		if swOrder.CreatedAt != "" {
			orderTime, parseErr := time.Parse(time.RFC3339, swOrder.CreatedAt)
			if parseErr == nil {
				if orderTime.Before(createdAfter) || orderTime.After(createdBefore) {
					continue
				}
			}
		}

		mmOrder := mapShopWiredOrder(swOrder, tenantID, credentialID)
		orderID, isNew, err := h.orderService.CreateOrder(ctx, tenantID, mmOrder)
		if err != nil {
			log.Printf("[ShopWired Orders] Failed to save order %d: %v", swOrder.ID, err)
			continue
		}
		if !isNew {
			continue
		}

		currency := swOrder.Currency
		if currency == "" {
			currency = "GBP"
		}
		for i, item := range swOrder.Items {
			line := &models.OrderLine{
				LineID:    fmt.Sprintf("%d-%d", swOrder.ID, i),
				ProductID: strconv.Itoa(item.ProductID),
				SKU:       item.SKU,
				Title:     item.Title,
				Quantity:  item.Quantity,
				UnitPrice: models.Money{Amount: item.Price, Currency: currency},
				LineTotal: models.Money{Amount: item.Price * float64(item.Quantity), Currency: currency},
			}
			if err := h.orderService.CreateOrderLine(ctx, tenantID, orderID, line); err != nil {
				log.Printf("[ShopWired Orders] Failed to save line %d for order %d: %v", i, swOrder.ID, err)
			}
		}
		imported++
	}

	log.Printf("[ShopWired Orders] Imported %d orders for tenant=%s cred=%s", imported, tenantID, credentialID)
	return imported, nil
}

// ImportOrders pulls orders from ShopWired and saves them to MarketMate.
// POST /shopwired/orders/import  { "credential_id": "...", "status": "new" }
func (h *ShopWiredOrdersHandler) ImportOrders(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req struct {
		CredentialID string `json:"credential_id" binding:"required"`
		Status       string `json:"status"`
		MaxOrders    int    `json:"maxOrders"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	client, err := h.buildClient(c.Request.Context(), tenantID, req.CredentialID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	limit := req.MaxOrders
	if limit <= 0 {
		limit = 100
	}

	orders, err := client.ListOrders(0, limit, req.Status)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": fmt.Sprintf("fetch orders: %v", err)})
		return
	}

	imported := 0
	var importErrors []string

	for _, swOrder := range orders {
		mmOrder := mapShopWiredOrder(swOrder, tenantID, req.CredentialID)

		orderID, isNew, err := h.orderService.CreateOrder(c.Request.Context(), tenantID, mmOrder)
		if err != nil {
			importErrors = append(importErrors, fmt.Sprintf("order %d: %v", swOrder.ID, err))
			continue
		}
		if !isNew {
			continue
		}

		currency := swOrder.Currency
		if currency == "" {
			currency = "GBP"
		}

		for i, item := range swOrder.Items {
			line := &models.OrderLine{
				LineID:    fmt.Sprintf("%d-%d", swOrder.ID, i),
				ProductID: strconv.Itoa(item.ProductID),
				SKU:       item.SKU,
				Title:     item.Title,
				Quantity:  item.Quantity,
				UnitPrice: models.Money{Amount: item.Price, Currency: currency},
				LineTotal: models.Money{Amount: item.Price * float64(item.Quantity), Currency: currency},
			}
			if err := h.orderService.CreateOrderLine(c.Request.Context(), tenantID, orderID, line); err != nil {
				log.Printf("[ShopWired Orders] Failed to save line %d for order %d: %v", i, swOrder.ID, err)
			}
		}

		imported++
		log.Printf("[ShopWired Orders] Imported order %d → %s", swOrder.ID, orderID)
	}

	log.Printf("[ShopWired Orders] Imported %d orders for tenant %s", imported, tenantID)

	c.JSON(http.StatusOK, gin.H{
		"ok":       true,
		"imported": imported,
		"total":    len(orders),
		"errors":   importErrors,
	})
}

// ── List Orders ───────────────────────────────────────────────────────────────

// GetOrders lists raw ShopWired orders (for debugging / manual review).
// GET /shopwired/orders?credential_id=...&status=new
func (h *ShopWiredOrdersHandler) GetOrders(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	credentialID := c.Query("credential_id")
	if credentialID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "credential_id required"})
		return
	}

	client, err := h.buildClient(c.Request.Context(), tenantID, credentialID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	status := c.Query("status")
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	count, _ := strconv.Atoi(c.DefaultQuery("count", "50"))

	orders, err := client.ListOrders(offset, count, status)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error(), "orders": []interface{}{}})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "orders": orders, "count": len(orders)})
}

// ── Mark Shipped ──────────────────────────────────────────────────────────────

// MarkShipped pushes dispatch info (tracking number + carrier) to a ShopWired order.
// POST /shopwired/orders/:id/ship  { "credential_id": "...", "tracking_number": "...", "carrier": "Royal Mail" }
func (h *ShopWiredOrdersHandler) MarkShipped(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	orderIDStr := c.Param("id")
	orderID, err := strconv.Atoi(orderIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "invalid order id"})
		return
	}

	var req struct {
		CredentialID   string `json:"credential_id" binding:"required"`
		TrackingNumber string `json:"tracking_number"`
		Carrier        string `json:"carrier"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	client, err := h.buildClient(c.Request.Context(), tenantID, req.CredentialID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	if err := client.UpdateOrderStatus(orderID, "dispatched", req.TrackingNumber, req.Carrier); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":             true,
		"order_id":       orderID,
		"tracking_number": req.TrackingNumber,
		"carrier":        req.Carrier,
	})
}

// ── Update Order Status ───────────────────────────────────────────────────────

// UpdateOrderStatus sets an arbitrary status on a ShopWired order.
// POST /shopwired/orders/:id/status  { "credential_id": "...", "status": "completed" }
func (h *ShopWiredOrdersHandler) UpdateOrderStatus(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	orderIDStr := c.Param("id")
	orderID, err := strconv.Atoi(orderIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "invalid order id"})
		return
	}

	var req struct {
		CredentialID   string `json:"credential_id" binding:"required"`
		Status         string `json:"status" binding:"required"`
		TrackingNumber string `json:"tracking_number,omitempty"`
		Carrier        string `json:"carrier,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	client, err := h.buildClient(c.Request.Context(), tenantID, req.CredentialID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	if err := client.UpdateOrderStatus(orderID, req.Status, req.TrackingNumber, req.Carrier); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "order_id": orderID, "status": req.Status})
}

// ============================================================================
// MAPPING HELPER
// ============================================================================

func mapShopWiredOrder(swOrder shopwired.Order, tenantID, credentialID string) *models.Order {
	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)

	currency := swOrder.Currency
	if currency == "" {
		currency = "GBP"
	}

	orderDate := swOrder.CreatedAt
	if orderDate == "" {
		orderDate = nowStr
	}

	return &models.Order{
		OrderID:          fmt.Sprintf("shopwired-%d", swOrder.ID),
		TenantID:         tenantID,
		Channel:          "shopwired",
		ChannelAccountID: credentialID,
		ExternalOrderID:  strconv.Itoa(swOrder.ID),
		Status:           normaliseShopWiredStatus(swOrder.Status),
		Customer: models.Customer{
			Name:  swOrder.CustomerName,
			Email: swOrder.Email,
		},
		TrackingNumber: swOrder.TrackingNumber,
		Totals: models.OrderTotals{
			GrandTotal: models.Money{Amount: swOrder.Total, Currency: currency},
		},
		OrderDate:  orderDate,
		CreatedAt:  nowStr,
		UpdatedAt:  nowStr,
		ImportedAt: nowStr,
	}
}

func normaliseShopWiredStatus(status string) string {
	switch status {
	case "new", "pending":
		return "pending"
	case "processing", "in_progress":
		return "processing"
	case "dispatched", "shipped":
		return "dispatched"
	case "completed", "complete":
		return "completed"
	case "cancelled", "canceled":
		return "cancelled"
	case "refunded":
		return "refunded"
	default:
		return status
	}
}
