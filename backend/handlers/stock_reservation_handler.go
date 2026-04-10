package handlers

// ============================================================================
// STOCK RESERVATION HANDLER — Overselling Prevention
// ============================================================================
// When an order is imported from any channel, a reservation is created
// against each SKU/product so stock cannot be double-allocated.
//
// Reservations are released when the order is despatched or cancelled.
// The inventory sync handler subtracts active reservations from available
// stock before pushing quantities to channels.
//
// Firestore collection: tenants/{tenantID}/stock_reservations/{reservationID}
//
// Endpoints:
//   POST /stock-reservations              - Create reservation for an order
//   GET  /stock-reservations/:product_id  - Get reservations for a product
//   POST /stock-reservations/:id/release  - Manually release a reservation
//   DELETE /stock-reservations/:id        - Delete a reservation
// ============================================================================

import (
	"context"
	"log"
	"net/http"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"
)

// ── Handler ───────────────────────────────────────────────────────────────────

type StockReservationHandler struct {
	client *firestore.Client
}

func NewStockReservationHandler(client *firestore.Client) *StockReservationHandler {
	return &StockReservationHandler{client: client}
}

// ── Data structures ───────────────────────────────────────────────────────────

// StockReservation records a quantity hold against a product/SKU for an order.
// Fields are intentionally flat for easy Firestore querying.
type StockReservation struct {
	ReservationID string    `firestore:"reservation_id" json:"reservation_id"`
	TenantID      string    `firestore:"tenant_id" json:"tenant_id"`
	ProductID     string    `firestore:"product_id" json:"product_id"`
	SKU           string    `firestore:"sku" json:"sku"`
	Channel       string    `firestore:"channel" json:"channel"`       // source channel e.g. "amazon"
	OrderID       string    `firestore:"order_id" json:"order_id"`     // internal order ID
	ExternalOrderID string  `firestore:"external_order_id" json:"external_order_id"`
	Quantity      int       `firestore:"quantity" json:"quantity"`
	Status        string    `firestore:"status" json:"status"`         // active | released | cancelled
	CreatedAt     time.Time `firestore:"created_at" json:"created_at"`
	ReleasedAt    *time.Time `firestore:"released_at,omitempty" json:"released_at,omitempty"`
	ReleasedBy    string    `firestore:"released_by,omitempty" json:"released_by,omitempty"` // "despatch" | "cancellation" | "manual"
}

// ── POST /api/v1/stock-reservations ──────────────────────────────────────────
// Creates reservations for all line items in an order.
// Called automatically by the order import flow and also available manually.

func (h *StockReservationHandler) CreateReservation(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()

	var req struct {
		OrderID         string `json:"order_id" binding:"required"`
		ExternalOrderID string `json:"external_order_id"`
		Channel         string `json:"channel"`
		Lines           []struct {
			ProductID string `json:"product_id"`
			SKU       string `json:"sku"`
			Quantity  int    `json:"quantity"`
		} `json:"lines" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	created := 0
	var reservationIDs []string

	for _, line := range req.Lines {
		if line.Quantity <= 0 || (line.ProductID == "" && line.SKU == "") {
			continue
		}

		res := StockReservation{
			ReservationID:   uuid.New().String(),
			TenantID:        tenantID,
			ProductID:       line.ProductID,
			SKU:             line.SKU,
			Channel:         req.Channel,
			OrderID:         req.OrderID,
			ExternalOrderID: req.ExternalOrderID,
			Quantity:        line.Quantity,
			Status:          "active",
			CreatedAt:       time.Now(),
		}

		_, err := h.client.Collection("tenants").Doc(tenantID).
			Collection("stock_reservations").Doc(res.ReservationID).Set(ctx, res)
		if err != nil {
			log.Printf("[StockReservation] Failed to create reservation for order %s SKU %s: %v", req.OrderID, line.SKU, err)
			continue
		}
		reservationIDs = append(reservationIDs, res.ReservationID)
		created++
	}

	c.JSON(http.StatusCreated, gin.H{
		"ok":              true,
		"created":         created,
		"reservation_ids": reservationIDs,
	})
}

// ── GET /api/v1/stock-reservations/:product_id ───────────────────────────────
// Returns all active reservations for a given product ID or SKU.

func (h *StockReservationHandler) GetReservationsByProduct(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()
	productID := c.Param("product_id")

	var reservations []StockReservation

	// Query by product_id
	iter := h.client.Collection("tenants").Doc(tenantID).
		Collection("stock_reservations").
		Where("product_id", "==", productID).
		Where("status", "==", "active").
		OrderBy("created_at", firestore.Desc).
		Limit(200).Documents(ctx)

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		var r StockReservation
		if err := doc.DataTo(&r); err == nil {
			reservations = append(reservations, r)
		}
	}
	iter.Stop()

	// Also query by SKU if a sku param is provided
	if sku := c.Query("sku"); sku != "" {
		iter2 := h.client.Collection("tenants").Doc(tenantID).
			Collection("stock_reservations").
			Where("sku", "==", sku).
			Where("status", "==", "active").
			OrderBy("created_at", firestore.Desc).
			Limit(200).Documents(ctx)

		for {
			doc, err := iter2.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				break
			}
			var r StockReservation
			if err := doc.DataTo(&r); err == nil {
				// Deduplicate by reservation_id
				found := false
				for _, existing := range reservations {
					if existing.ReservationID == r.ReservationID {
						found = true
						break
					}
				}
				if !found {
					reservations = append(reservations, r)
				}
			}
		}
		iter2.Stop()
	}

	if reservations == nil {
		reservations = []StockReservation{}
	}

	// Compute total reserved quantity
	total := 0
	for _, r := range reservations {
		total += r.Quantity
	}

	c.JSON(http.StatusOK, gin.H{
		"data":            reservations,
		"total_reserved":  total,
		"product_id":      productID,
	})
}

// ── POST /api/v1/stock-reservations/:id/release ───────────────────────────────
// Manually release a specific reservation.

func (h *StockReservationHandler) ReleaseReservation(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()
	reservationID := c.Param("id")

	var req struct {
		ReleasedBy string `json:"released_by"` // "despatch" | "cancellation" | "manual"
	}
	c.ShouldBindJSON(&req)
	if req.ReleasedBy == "" {
		req.ReleasedBy = "manual"
	}

	now := time.Now()
	_, err := h.client.Collection("tenants").Doc(tenantID).
		Collection("stock_reservations").Doc(reservationID).
		Update(ctx, []firestore.Update{
			{Path: "status", Value: "released"},
			{Path: "released_at", Value: now},
			{Path: "released_by", Value: req.ReleasedBy},
		})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to release reservation: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "Reservation released"})
}

// ── DELETE /api/v1/stock-reservations/:id ────────────────────────────────────

func (h *StockReservationHandler) DeleteReservation(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()
	reservationID := c.Param("id")

	_, err := h.client.Collection("tenants").Doc(tenantID).
		Collection("stock_reservations").Doc(reservationID).Delete(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ── Shared utility: ReserveOrderStock ────────────────────────────────────────
// Called internally by order import handlers to create reservations
// immediately when an order is saved. Pass the Firestore client and
// the order details; this is a best-effort call — import continues
// even if reservation fails.

func ReserveOrderStock(ctx context.Context, client *firestore.Client, tenantID, orderID, externalOrderID, channel string, lines []struct {
	ProductID string
	SKU       string
	Quantity  int
}) {
	for _, line := range lines {
		if line.Quantity <= 0 || (line.ProductID == "" && line.SKU == "") {
			continue
		}
		res := StockReservation{
			ReservationID:   uuid.New().String(),
			TenantID:        tenantID,
			ProductID:       line.ProductID,
			SKU:             line.SKU,
			Channel:         channel,
			OrderID:         orderID,
			ExternalOrderID: externalOrderID,
			Quantity:        line.Quantity,
			Status:          "active",
			CreatedAt:       time.Now(),
		}
		_, err := client.Collection("tenants").Doc(tenantID).
			Collection("stock_reservations").Doc(res.ReservationID).Set(ctx, res)
		if err != nil {
			log.Printf("[StockReservation] auto-reserve failed for order %s SKU %s: %v", orderID, line.SKU, err)
		}
	}
}

// ── ReleaseOrderReservations ─────────────────────────────────────────────────
// Called by despatch and cancellation handlers to release all active
// reservations for a given order. Best-effort; logs failures.

func ReleaseOrderReservations(ctx context.Context, client *firestore.Client, tenantID, orderID, reason string) {
	iter := client.Collection("tenants").Doc(tenantID).
		Collection("stock_reservations").
		Where("order_id", "==", orderID).
		Where("status", "==", "active").
		Documents(ctx)

	now := time.Now()
	released := 0
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		_, err = doc.Ref.Update(ctx, []firestore.Update{
			{Path: "status", Value: "released"},
			{Path: "released_at", Value: now},
			{Path: "released_by", Value: reason},
		})
		if err != nil {
			log.Printf("[StockReservation] Failed to release reservation %s: %v", doc.Ref.ID, err)
		} else {
			released++
		}
	}
	iter.Stop()
	if released > 0 {
		log.Printf("[StockReservation] Released %d reservations for order %s (%s)", released, orderID, reason)
	}
}

// ── GetReservedQuantity ───────────────────────────────────────────────────────
// Returns total active reserved quantity for a product/SKU.
// Used by the inventory sync handler to subtract reservations before pushing.

func GetReservedQuantity(ctx context.Context, client *firestore.Client, tenantID, productID, sku string) int {
	total := 0

	if productID != "" {
		iter := client.Collection("tenants").Doc(tenantID).
			Collection("stock_reservations").
			Where("product_id", "==", productID).
			Where("status", "==", "active").
			Documents(ctx)
		for {
			doc, err := iter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				break
			}
			data := doc.Data()
			if q, ok := data["quantity"].(int64); ok {
				total += int(q)
			}
		}
		iter.Stop()
	} else if sku != "" {
		iter := client.Collection("tenants").Doc(tenantID).
			Collection("stock_reservations").
			Where("sku", "==", sku).
			Where("status", "==", "active").
			Documents(ctx)
		for {
			doc, err := iter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				break
			}
			data := doc.Data()
			if q, ok := data["quantity"].(int64); ok {
				total += int(q)
			}
		}
		iter.Stop()
	}

	return total
}
