package handlers

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"
)

// ============================================================================
// INVENTORY HANDLER
// ============================================================================

type InventoryHandler struct {
	client *firestore.Client
}

func NewInventoryHandler(client *firestore.Client) *InventoryHandler {
	return &InventoryHandler{client: client}
}

// ============================================================================
// INVENTORY DATA STRUCTURES
// ============================================================================

type InventoryItem struct {
	InventoryID    string          `firestore:"inventory_id" json:"inventory_id"`
	SKU            string          `firestore:"sku" json:"sku"`
	ProductName    string          `firestore:"product_name" json:"product_name"`
	VariantName    string          `firestore:"variant_name,omitempty" json:"variant_name,omitempty"`
	Locations      []LocationStock `firestore:"locations" json:"locations"`
	TotalOnHand    int             `firestore:"total_on_hand" json:"total_on_hand"`
	TotalReserved  int             `firestore:"total_reserved" json:"total_reserved"`
	TotalAvailable int             `firestore:"total_available" json:"total_available"`
	TotalInbound   int             `firestore:"total_inbound" json:"total_inbound"`
	SafetyStock    int             `firestore:"safety_stock" json:"safety_stock"`
	ReorderPoint   int             `firestore:"reorder_point" json:"reorder_point"`
	UpdatedAt      time.Time       `firestore:"updated_at" json:"updated_at"`
}

type LocationStock struct {
	LocationID   string `firestore:"location_id" json:"location_id"`
	LocationName string `firestore:"location_name" json:"location_name"`
	OnHand       int    `firestore:"on_hand" json:"on_hand"`
	Reserved     int    `firestore:"reserved" json:"reserved"`
	Available    int    `firestore:"available" json:"available"`
	Inbound      int    `firestore:"inbound" json:"inbound"`
	SafetyStock  int    `firestore:"safety_stock" json:"safety_stock"`
}

type Reservation struct {
	ReservationID string    `firestore:"reservation_id" json:"reservation_id"`
	SKU           string    `firestore:"sku" json:"sku"`
	LocationID    string    `firestore:"location_id" json:"location_id"`
	Quantity      int       `firestore:"quantity" json:"quantity"`
	OrderID       string    `firestore:"order_id" json:"order_id"`
	ShipmentID    string    `firestore:"shipment_id,omitempty" json:"shipment_id,omitempty"`
	Status        string    `firestore:"status" json:"status"` // active, released, expired
	CreatedAt     time.Time `firestore:"created_at" json:"created_at"`
	ExpiresAt     *time.Time `firestore:"expires_at,omitempty" json:"expires_at,omitempty"`
	ReleasedAt    *time.Time `firestore:"released_at,omitempty" json:"released_at,omitempty"`
}

type Movement struct {
	MovementID  string    `firestore:"movement_id" json:"movement_id"`
	SKU         string    `firestore:"sku" json:"sku"`
	LocationID  string    `firestore:"location_id" json:"location_id"`
	Type        string    `firestore:"type" json:"type"` // receipt, shipment, adjustment, transfer_in, transfer_out, return
	Quantity    int       `firestore:"quantity" json:"quantity"`
	ReasonCode  string    `firestore:"reason_code" json:"reason_code"`
	ReferenceID string    `firestore:"reference_id,omitempty" json:"reference_id,omitempty"`
	CreatedBy   string    `firestore:"created_by" json:"created_by"`
	CreatedAt   time.Time `firestore:"created_at" json:"created_at"`
	Notes       string    `firestore:"notes,omitempty" json:"notes,omitempty"`
}

type Location struct {
	LocationID  string `firestore:"location_id" json:"location_id"`
	Name        string `firestore:"name" json:"name"`
	Type        string `firestore:"type" json:"type"` // warehouse, 3pl, store, supplier
	Address     string `firestore:"address" json:"address"`
	Timezone    string `firestore:"timezone" json:"timezone"`
	Active      bool   `firestore:"active" json:"active"`
	CutOffTime  string `firestore:"cut_off_time,omitempty" json:"cut_off_time,omitempty"`
}

// ============================================================================
// REQUEST/RESPONSE STRUCTURES
// ============================================================================

type AdjustStockRequest struct {
	SKU        string `json:"sku"`
	LocationID string `json:"location_id"`
	Type       string `json:"type"` // adjustment, receipt, return
	Quantity   int    `json:"quantity"`
	ReasonCode string `json:"reason_code"`
	Notes      string `json:"notes"`
}

type TransferRequest struct {
	SKU          string `json:"sku"`
	FromLocation string `json:"from_location"`
	ToLocation   string `json:"to_location"`
	Quantity     int    `json:"quantity"`
	Notes        string `json:"notes"`
}

type ReservationRequest struct {
	SKU        string `json:"sku"`
	LocationID string `json:"location_id"`
	Quantity   int    `json:"quantity"`
	OrderID    string `json:"order_id"`
	ShipmentID string `json:"shipment_id,omitempty"`
}

// ============================================================================
// INVENTORY HANDLERS
// ============================================================================

// GetInventory retrieves inventory with optional filters
func (h *InventoryHandler) GetInventory(c *gin.Context) {
	tenantID := c.GetHeader("X-Tenant-Id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "X-Tenant-Id header required"})
		return
	}

	ctx := c.Request.Context()
	client := h.client

	// Build query
	query := client.Collection(fmt.Sprintf("tenants/%s/inventory", tenantID)).Query

	// Apply filters
	if locationID := c.Query("location_id"); locationID != "" {
		// Filter will be applied after fetching (Firestore array queries are complex)
	}
	if c.Query("low_stock") == "true" {
		// Will filter in memory
	}
	if c.Query("out_of_stock") == "true" {
		query = query.Where("total_available", "==", 0)
	}

	iter := query.Documents(ctx)
	var inventory []InventoryItem

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Printf("Error fetching inventory: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch inventory"})
			return
		}

		var item InventoryItem
		doc.DataTo(&item)
		
		// Apply in-memory filters
		if c.Query("low_stock") == "true" {
			if item.TotalAvailable > item.SafetyStock {
				continue
			}
		}
		if locationID := c.Query("location_id"); locationID != "" {
			hasLocation := false
			for _, loc := range item.Locations {
				if loc.LocationID == locationID {
					hasLocation = true
					break
				}
			}
			if !hasLocation {
				continue
			}
		}

		inventory = append(inventory, item)
	}

	c.JSON(http.StatusOK, gin.H{
		"inventory": inventory,
		"count":     len(inventory),
	})
}

// GetInventoryStats calculates inventory statistics
func (h *InventoryHandler) GetInventoryStats(c *gin.Context) {
	tenantID := c.GetHeader("X-Tenant-Id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "X-Tenant-Id header required"})
		return
	}

	ctx := c.Request.Context()
	client := h.client

	iter := client.Collection(fmt.Sprintf("tenants/%s/inventory", tenantID)).Documents(ctx)

	stats := map[string]int{
		"total_skus":        0,
		"total_on_hand":     0,
		"total_reserved":    0,
		"total_available":   0,
		"low_stock_count":   0,
		"out_of_stock_count": 0,
	}

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Printf("Error calculating stats: %v", err)
			continue
		}

		var item InventoryItem
		doc.DataTo(&item)

		stats["total_skus"]++
		stats["total_on_hand"] += item.TotalOnHand
		stats["total_reserved"] += item.TotalReserved
		stats["total_available"] += item.TotalAvailable

		if item.TotalAvailable == 0 {
			stats["out_of_stock_count"]++
		} else if item.TotalAvailable <= item.SafetyStock {
			stats["low_stock_count"]++
		}
	}

	c.JSON(http.StatusOK, gin.H{"stats": stats})
}

// AdjustStock handles manual stock adjustments
func (h *InventoryHandler) AdjustStock(c *gin.Context) {
	tenantID := c.GetHeader("X-Tenant-Id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "X-Tenant-Id header required"})
		return
	}

	var req AdjustStockRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	if req.SKU == "" || req.LocationID == "" || req.ReasonCode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "SKU, location_id, and reason_code are required"})
		return
	}

	ctx := c.Request.Context()
	client := h.client

	// Create movement record
	movementID := uuid.New().String()
	movement := Movement{
		MovementID:  movementID,
		SKU:         req.SKU,
		LocationID:  req.LocationID,
		Type:        req.Type,
		Quantity:    req.Quantity,
		ReasonCode:  req.ReasonCode,
		CreatedBy:   "current_user", // TODO: Get from auth
		CreatedAt:   time.Now(),
		Notes:       req.Notes,
	}

	_, err := client.Collection(fmt.Sprintf("tenants/%s/movements", tenantID)).
		Doc(movementID).
		Set(ctx, movement)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to record movement"})
		return
	}

	// Update inventory
	err = updateInventoryStock(ctx, client, tenantID, req.SKU, req.LocationID, req.Quantity)
	if err != nil {
		log.Printf("Failed to update inventory: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update inventory"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"movement": movement,
		"message":  fmt.Sprintf("Stock adjusted by %d for SKU %s", req.Quantity, req.SKU),
	})
}

// CreateTransfer handles stock transfers between locations
func (h *InventoryHandler) CreateTransfer(c *gin.Context) {
	tenantID := c.GetHeader("X-Tenant-Id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "X-Tenant-Id header required"})
		return
	}

	var req TransferRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	if req.SKU == "" || req.FromLocation == "" || req.ToLocation == "" || req.Quantity <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid transfer request"})
		return
	}

	ctx := c.Request.Context()
	client := h.client

	// Create transfer out movement
	transferOutID := uuid.New().String()
	transferOut := Movement{
		MovementID:  transferOutID,
		SKU:         req.SKU,
		LocationID:  req.FromLocation,
		Type:        "transfer_out",
		Quantity:    -req.Quantity,
		ReasonCode:  "transfer",
		CreatedBy:   "current_user",
		CreatedAt:   time.Now(),
		Notes:       req.Notes,
	}

	_, err := client.Collection(fmt.Sprintf("tenants/%s/movements", tenantID)).
		Doc(transferOutID).
		Set(ctx, transferOut)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create transfer out"})
		return
	}

	// Create transfer in movement
	transferInID := uuid.New().String()
	transferIn := Movement{
		MovementID:  transferInID,
		SKU:         req.SKU,
		LocationID:  req.ToLocation,
		Type:        "transfer_in",
		Quantity:    req.Quantity,
		ReasonCode:  "transfer",
		ReferenceID: transferOutID,
		CreatedBy:   "current_user",
		CreatedAt:   time.Now(),
		Notes:       req.Notes,
	}

	_, err = client.Collection(fmt.Sprintf("tenants/%s/movements", tenantID)).
		Doc(transferInID).
		Set(ctx, transferIn)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create transfer in"})
		return
	}

	// Update inventory for both locations
	err = updateInventoryStock(ctx, client, tenantID, req.SKU, req.FromLocation, -req.Quantity)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update source location"})
		return
	}

	err = updateInventoryStock(ctx, client, tenantID, req.SKU, req.ToLocation, req.Quantity)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update destination location"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": fmt.Sprintf("Transferred %d units of %s from %s to %s", 
			req.Quantity, req.SKU, req.FromLocation, req.ToLocation),
	})
}

// CreateReservation creates a stock reservation for an order
func (h *InventoryHandler) CreateReservation(c *gin.Context) {
	tenantID := c.GetHeader("X-Tenant-Id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "X-Tenant-Id header required"})
		return
	}

	var req ReservationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	ctx := c.Request.Context()
	client := h.client

	// Check available stock
	inventoryDoc, err := client.Collection(fmt.Sprintf("tenants/%s/inventory", tenantID)).
		Doc(req.SKU).
		Get(ctx)
	
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "SKU not found"})
		return
	}

	var inventory InventoryItem
	inventoryDoc.DataTo(&inventory)

	// Find location stock
	var locationStock *LocationStock
	for i, loc := range inventory.Locations {
		if loc.LocationID == req.LocationID {
			locationStock = &inventory.Locations[i]
			break
		}
	}

	if locationStock == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Location not found for this SKU"})
		return
	}

	if locationStock.Available < req.Quantity {
		c.JSON(http.StatusConflict, gin.H{
			"error":     "Insufficient stock",
			"available": locationStock.Available,
			"requested": req.Quantity,
		})
		return
	}

	// Create reservation
	reservationID := uuid.New().String()
	reservation := Reservation{
		ReservationID: reservationID,
		SKU:           req.SKU,
		LocationID:    req.LocationID,
		Quantity:      req.Quantity,
		OrderID:       req.OrderID,
		ShipmentID:    req.ShipmentID,
		Status:        "active",
		CreatedAt:     time.Now(),
	}

	_, err = client.Collection(fmt.Sprintf("tenants/%s/reservations", tenantID)).
		Doc(reservationID).
		Set(ctx, reservation)
	
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create reservation"})
		return
	}

	// Update inventory reserved count
	err = updateInventoryReservation(ctx, client, tenantID, req.SKU, req.LocationID, req.Quantity)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update inventory"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":     true,
		"reservation": reservation,
	})
}

// ReleaseReservation releases a stock reservation
func (h *InventoryHandler) ReleaseReservation(c *gin.Context) {
	tenantID := c.GetHeader("X-Tenant-Id")
	reservationID := c.Param("reservation_id")

	if tenantID == "" || reservationID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	ctx := c.Request.Context()
	client := h.client

	// Get reservation
	reservationDoc, err := client.Collection(fmt.Sprintf("tenants/%s/reservations", tenantID)).
		Doc(reservationID).
		Get(ctx)
	
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Reservation not found"})
		return
	}

	var reservation Reservation
	reservationDoc.DataTo(&reservation)

	if reservation.Status != "active" {
		c.JSON(http.StatusConflict, gin.H{"error": "Reservation is not active"})
		return
	}

	// Update reservation
	releasedAt := time.Now()
	_, err = reservationDoc.Ref.Update(ctx, []firestore.Update{
		{Path: "status", Value: "released"},
		{Path: "released_at", Value: releasedAt},
	})
	
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to release reservation"})
		return
	}

	// Update inventory
	err = updateInventoryReservation(ctx, client, tenantID, reservation.SKU, reservation.LocationID, -reservation.Quantity)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update inventory"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Reservation released",
	})
}

// GetLocations retrieves all warehouse locations
func (h *InventoryHandler) GetLocations(c *gin.Context) {
	tenantID := c.GetHeader("X-Tenant-Id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "X-Tenant-Id header required"})
		return
	}

	ctx := c.Request.Context()
	client := h.client

	iter := client.Collection(fmt.Sprintf("tenants/%s/locations", tenantID)).
		Where("active", "==", true).
		Documents(ctx)

	var locations []Location
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch locations"})
			return
		}

		var location Location
		doc.DataTo(&location)
		locations = append(locations, location)
	}

	c.JSON(http.StatusOK, gin.H{
		"locations": locations,
	})
}

// ============================================================================
// HELPER FUNCTIONS
// ============================================================================

func updateInventoryStock(ctx context.Context, client *firestore.Client, tenantID, sku, locationID string, quantityChange int) error {
	inventoryRef := client.Collection(fmt.Sprintf("tenants/%s/inventory", tenantID)).Doc(sku)
	
	return client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(inventoryRef)
		if err != nil {
			return err
		}

		var inventory InventoryItem
		doc.DataTo(&inventory)

		// Update location stock
		for i, loc := range inventory.Locations {
			if loc.LocationID == locationID {
				inventory.Locations[i].OnHand += quantityChange
				inventory.Locations[i].Available = inventory.Locations[i].OnHand - inventory.Locations[i].Reserved
				break
			}
		}

		// Recalculate totals
		inventory.TotalOnHand = 0
		inventory.TotalReserved = 0
		inventory.TotalAvailable = 0
		for _, loc := range inventory.Locations {
			inventory.TotalOnHand += loc.OnHand
			inventory.TotalReserved += loc.Reserved
			inventory.TotalAvailable += loc.Available
		}
		inventory.UpdatedAt = time.Now()

		return tx.Set(inventoryRef, inventory)
	})
}

func updateInventoryReservation(ctx context.Context, client *firestore.Client, tenantID, sku, locationID string, quantityChange int) error {
	inventoryRef := client.Collection(fmt.Sprintf("tenants/%s/inventory", tenantID)).Doc(sku)
	
	return client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(inventoryRef)
		if err != nil {
			return err
		}

		var inventory InventoryItem
		doc.DataTo(&inventory)

		// Update location reserved
		for i, loc := range inventory.Locations {
			if loc.LocationID == locationID {
				inventory.Locations[i].Reserved += quantityChange
				inventory.Locations[i].Available = inventory.Locations[i].OnHand - inventory.Locations[i].Reserved
				break
			}
		}

		// Recalculate totals
		inventory.TotalReserved = 0
		inventory.TotalAvailable = 0
		for _, loc := range inventory.Locations {
			inventory.TotalReserved += loc.Reserved
			inventory.TotalAvailable += loc.Available
		}
		inventory.UpdatedAt = time.Now()

		return tx.Set(inventoryRef, inventory)
	})
}

// ── GET /api/v1/inventory/combined?sku=<sku> ─────────────────────────────────
// Session 9: Aggregates stock across all fulfilment sources for a given SKU.

type CombinedStockLocation struct {
	LocationID   string `json:"location_id"`
	LocationName string `json:"location_name"`
	BinrackName  string `json:"binrack_name,omitempty"`
	Quantity     int    `json:"quantity"`
}

type CombinedStockResponse struct {
	SKU       string                  `json:"sku"`
	Total     int                     `json:"total"`
	Locations []CombinedStockLocation `json:"locations"`
}

func (h *InventoryHandler) GetCombinedStock(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	sku := c.Query("sku")
	ctx := c.Request.Context()

	if sku == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "sku query param required"})
		return
	}

	var locs []CombinedStockLocation
	total := 0

	iter := h.client.Collection("tenants").Doc(tenantID).Collection("inventory").
		Where("sku", "==", sku).
		Where("quantity", ">", 0).
		Documents(ctx)
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if err == iterator.Done { break }
		if err != nil { break }
		data := doc.Data()
		qty := 0
		if q, ok := data["quantity"].(int64); ok { qty = int(q) }
		locationID, _ := data["warehouse_id"].(string)
		locationName, _ := data["warehouse_name"].(string)
		binrackName, _ := data["binrack_name"].(string)
		locs = append(locs, CombinedStockLocation{
			LocationID:   locationID,
			LocationName: locationName,
			BinrackName:  binrackName,
			Quantity:     qty,
		})
		total += qty
	}

	if locs == nil { locs = []CombinedStockLocation{} }
	c.JSON(http.StatusOK, CombinedStockResponse{SKU: sku, Total: total, Locations: locs})
}
