package handlers

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"module-a/marketplace/clients/tesco"
	"module-a/models"
	"module-a/services"
)

// ============================================================================
// TESCO ORDERS HANDLER
// ============================================================================

type TescoOrdersHandler struct {
	orderService       *services.OrderService
	marketplaceService *services.MarketplaceService
}

func NewTescoOrdersHandler(
	orderService *services.OrderService,
	marketplaceService *services.MarketplaceService,
) *TescoOrdersHandler {
	return &TescoOrdersHandler{
		orderService:       orderService,
		marketplaceService: marketplaceService,
	}
}

// getTescoOrdersClient builds a Tesco client from stored credentials
func (h *TescoOrdersHandler) getTescoOrdersClient(ctx context.Context, tenantID, credentialID string) (*tesco.Client, error) {
	cred, err := h.marketplaceService.GetCredential(ctx, tenantID, credentialID)
	if err != nil {
		return nil, fmt.Errorf("failed to get credentials: %w", err)
	}
	merged, err := h.marketplaceService.GetFullCredentials(ctx, cred)
	if err != nil {
		return nil, fmt.Errorf("failed to merge credentials: %w", err)
	}
	return tesco.NewClient(merged["client_id"], merged["client_secret"], merged["seller_id"]), nil
}

// ImportTescoOrders imports orders from Tesco API
func (h *TescoOrdersHandler) ImportTescoOrders(ctx context.Context, tenantID, credentialID string, createdAfter, createdBefore time.Time) (int, error) {
	log.Printf("[TescoOrders] import for tenant=%s cred=%s %s→%s", tenantID, credentialID,
		createdAfter.Format("2006-01-02"), createdBefore.Format("2006-01-02"))

	client, err := h.getTescoOrdersClient(ctx, tenantID, credentialID)
	if err != nil {
		return 0, err
	}

	orders, err := client.FetchNewOrders(createdAfter, createdBefore)
	if err != nil {
		return 0, fmt.Errorf("fetch tesco orders: %w", err)
	}

	imported := 0
	for _, to := range orders {
		order := h.tescoOrderToModel(to, tenantID, credentialID)
		_, isNew, err := h.orderService.CreateOrder(ctx, tenantID, &order)
		if err != nil {
			log.Printf("[TescoOrders] failed to save order %s: %v", to.OrderID, err)
			continue
		}
		if isNew {
			imported++
			// Acknowledge receipt with Tesco
			if _, ackErr := client.AcknowledgeOrder(to.OrderID); ackErr != nil {
				log.Printf("[TescoOrders] failed to acknowledge order %s: %v", to.OrderID, ackErr)
			}
		}
	}

	log.Printf("[TescoOrders] imported %d/%d orders", imported, len(orders))
	return imported, nil
}

// tescoOrderToModel converts a Tesco order to the internal Order model
func (h *TescoOrdersHandler) tescoOrderToModel(to tesco.Order, tenantID, credentialID string) models.Order {
	now := time.Now().Format(time.RFC3339)
	order := models.Order{
		TenantID:         tenantID,
		Channel:          "tesco",
		ChannelAccountID: credentialID,
		ExternalOrderID:  to.OrderID,
		Status:           "imported",
		PaymentStatus:    "captured",
		Customer: models.Customer{
			Name: to.CustomerName,
		},
		ShippingAddress: models.Address{
			Name:         to.CustomerName,
			AddressLine1: to.ShippingAddress.Line1,
			AddressLine2: to.ShippingAddress.Line2,
			City:         to.ShippingAddress.City,
			PostalCode:   to.ShippingAddress.PostalCode,
			Country:      to.ShippingAddress.Country,
		},
		Totals: models.OrderTotals{
			GrandTotal: models.Money{Amount: to.TotalAmount, Currency: to.Currency},
			Subtotal:   models.Money{Amount: to.TotalAmount, Currency: to.Currency},
		},
		OrderDate:  to.CreatedAt,
		CreatedAt:  now,
		UpdatedAt:  now,
		ImportedAt: now,
	}

	return order
}

// POST /tesco/orders/import — manual trigger
func (h *TescoOrdersHandler) TriggerImport(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req struct {
		CredentialID  string `json:"credential_id" binding:"required"`
		LookbackHours int    `json:"lookback_hours"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	lookback := req.LookbackHours
	if lookback <= 0 {
		lookback = 24
	}

	now := time.Now().UTC()
	from := now.Add(-time.Duration(lookback) * time.Hour)

	go func() {
		imported, err := h.ImportTescoOrders(context.Background(), tenantID, req.CredentialID, from, now)
		if err != nil {
			log.Printf("[TescoOrders] manual import failed: %v", err)
		} else {
			log.Printf("[TescoOrders] manual import complete: %d orders", imported)
		}
	}()

	c.JSON(http.StatusAccepted, gin.H{
		"status":  "started",
		"message": "Tesco order import started",
	})
}

// POST /tesco/orders/:id/tracking — push tracking to Tesco
func (h *TescoOrdersHandler) PushTracking(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	orderID := c.Param("id")

	var req struct {
		CredentialID   string `json:"credential_id" binding:"required"`
		TrackingNumber string `json:"tracking_number" binding:"required"`
		Carrier        string `json:"carrier"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Carrier == "" {
		req.Carrier = "OTHER"
	}

	client, err := h.getTescoOrdersClient(c.Request.Context(), tenantID, req.CredentialID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "failed to load credentials: " + err.Error()})
		return
	}

	if err := client.PushTracking(orderID, req.TrackingNumber, req.Carrier); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "tesco tracking push failed: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "order_id": orderID, "tracking_number": req.TrackingNumber})
}
