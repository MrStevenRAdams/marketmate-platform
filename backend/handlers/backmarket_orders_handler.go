package handlers

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"module-a/marketplace/clients/backmarket"
	"module-a/models"
	"module-a/services"
)

type BackMarketOrdersHandler struct {
	orderService       *services.OrderService
	marketplaceService *services.MarketplaceService
}

func NewBackMarketOrdersHandler(orderService *services.OrderService, marketplaceService *services.MarketplaceService) *BackMarketOrdersHandler {
	return &BackMarketOrdersHandler{orderService: orderService, marketplaceService: marketplaceService}
}

func (h *BackMarketOrdersHandler) buildBMClient(ctx context.Context, tenantID, credentialID string) (*backmarket.Client, *models.MarketplaceCredential, error) {
	cred, err := h.marketplaceService.GetCredential(ctx, tenantID, credentialID)
	if err != nil {
		return nil, nil, fmt.Errorf("get credential: %w", err)
	}
	merged, err := h.marketplaceService.GetFullCredentials(ctx, cred)
	if err != nil {
		return nil, nil, fmt.Errorf("merge credentials: %w", err)
	}
	if merged["api_key"] == "" {
		return nil, nil, fmt.Errorf("incomplete Back Market credentials (api_key required)")
	}
	return backmarket.NewClient(merged["api_key"], cred.Environment == "production"), cred, nil
}

func (h *BackMarketOrdersHandler) ImportBackMarketOrders(ctx context.Context, tenantID, credentialID string, createdAfter, createdBefore time.Time) (int, error) {
	log.Printf("[BackMarket Orders] Import tenant=%s cred=%s since=%s", tenantID, credentialID, createdAfter.Format(time.RFC3339))
	client, cred, err := h.buildBMClient(ctx, tenantID, credentialID)
	if err != nil {
		return 0, err
	}
	bmOrders, err := client.GetOrders(createdAfter, []string{"new", "to_ship"})
	if err != nil {
		return 0, fmt.Errorf("fetch Back Market orders: %w", err)
	}
	imported := 0
	for _, bmo := range bmOrders {
		currency := bmo.Currency
		if currency == "" {
			currency = "EUR"
		}
		orderDate := bmo.DateCreation
		if orderDate == "" {
			orderDate = time.Now().UTC().Format(time.RFC3339)
		}
		internal := &models.Order{
			TenantID:         tenantID,
			Channel:          "backmarket",
			ChannelAccountID: credentialID,
			ExternalOrderID:  fmt.Sprintf("%d", bmo.OrderID),
			Status:           mapBMOrderStatus(bmo.State),
			PaymentStatus:    "captured",
			OrderDate:        orderDate,
			Customer: models.Customer{
				Name:  bmo.BillingAddress.FirstName + " " + bmo.BillingAddress.LastName,
				Email: bmo.BillingAddress.Email,
				Phone: bmo.BillingAddress.Phone,
			},
			ShippingAddress: models.Address{
				Name:         bmo.ShippingAddress.FirstName + " " + bmo.ShippingAddress.LastName,
				AddressLine1: bmo.ShippingAddress.Street,
				AddressLine2: bmo.ShippingAddress.Street2,
				City:         bmo.ShippingAddress.City,
				PostalCode:   bmo.ShippingAddress.PostalCode,
				Country:      bmo.ShippingAddress.Country,
			},
			BillingAddress: &models.Address{
				Name:         bmo.BillingAddress.FirstName + " " + bmo.BillingAddress.LastName,
				AddressLine1: bmo.BillingAddress.Street,
				AddressLine2: bmo.BillingAddress.Street2,
				City:         bmo.BillingAddress.City,
				PostalCode:   bmo.BillingAddress.PostalCode,
				Country:      bmo.BillingAddress.Country,
			},
			Totals: models.OrderTotals{
				GrandTotal: models.Money{Amount: bmo.TotalPrice, Currency: currency},
			},
			InternalNotes: fmt.Sprintf("Back Market Order #%d (ref: %s)", bmo.OrderID, bmo.Reference),
		}
		ApplyOrderImportConfig(internal, cred.Config.Orders)
		orderID, isNew, err := h.orderService.CreateOrder(ctx, tenantID, internal)
		if err != nil {
			log.Printf("[BackMarket Orders] Failed to save order %d: %v", bmo.OrderID, err)
			continue
		}
		if !isNew {
			continue
		}
		for _, line := range bmo.Lines {
			ol := &models.OrderLine{
				LineID:    fmt.Sprintf("%d", line.LineID),
				SKU:       line.SellerSKU,
				Title:     line.Title,
				Quantity:  line.Quantity,
				UnitPrice: models.Money{Amount: line.UnitPrice, Currency: currency},
				LineTotal: models.Money{Amount: line.TotalPrice, Currency: currency},
				Status:    "pending",
			}
			if err := h.orderService.CreateOrderLine(ctx, tenantID, orderID, ol); err != nil {
				log.Printf("[BackMarket Orders] Failed to save line %d: %v", line.LineID, err)
			}
		}
		imported++
	}
	return imported, nil
}

func (h *BackMarketOrdersHandler) TriggerImport(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	var req struct {
		CredentialID string `json:"credential_id" binding:"required"`
		HoursBack    int    `json:"hours_back"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.HoursBack <= 0 {
		req.HoursBack = 48
	}
	now := time.Now().UTC()
	from := now.Add(-time.Duration(req.HoursBack) * time.Hour)
	c.JSON(http.StatusAccepted, gin.H{"status": "started", "from": from.Format(time.RFC3339), "credential_id": req.CredentialID})
	go func() {
		ctx := context.Background()
		n, err := h.ImportBackMarketOrders(ctx, tenantID, req.CredentialID, from, now)
		if err != nil {
			log.Printf("[BackMarket Orders] Import error: %v", err)
		} else {
			log.Printf("[BackMarket Orders] Import complete: %d orders", n)
		}
	}()
}

func (h *BackMarketOrdersHandler) ListOrders(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	opts := services.OrderListOptions{Channel: "backmarket", Limit: c.DefaultQuery("limit", "50"), Offset: c.DefaultQuery("offset", "0")}
	orders, total, err := h.orderService.ListOrders(c.Request.Context(), tenantID, opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"orders": orders, "total": total})
}

func (h *BackMarketOrdersHandler) PushTracking(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	orderIDStr := c.Param("id")
	var req struct {
		CredentialID string `json:"credential_id" binding:"required"`
		TrackingCode string `json:"tracking_code" binding:"required"`
		Carrier      string `json:"carrier"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	bmOrderID, err := strconv.Atoi(orderIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "order ID must be numeric"})
		return
	}
	client, _, err := h.buildBMClient(c.Request.Context(), tenantID, req.CredentialID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	order, err := client.GetOrder(bmOrderID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch order: " + err.Error()})
		return
	}
	var shipLines []backmarket.ShipLine
	for _, line := range order.Lines {
		shipLines = append(shipLines, backmarket.ShipLine{LineID: line.LineID, Quantity: line.Quantity})
	}
	if err := client.ShipOrder(bmOrderID, backmarket.ShipOrderRequest{
		Lines: shipLines, TrackingCode: req.TrackingCode, Carrier: req.Carrier, Mode: "standard",
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "order_id": bmOrderID, "tracking_code": req.TrackingCode})
}

func mapBMOrderStatus(state string) string {
	switch state {
	case "new", "to_ship":
		return "imported"
	case "shipped":
		return "fulfilled"
	case "cancelled", "refunded":
		return "cancelled"
	default:
		return "imported"
	}
}
