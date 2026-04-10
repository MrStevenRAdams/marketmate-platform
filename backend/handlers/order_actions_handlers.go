package handlers

import (
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
// ORDER ACTIONS HANDLER
// ============================================================================

type OrderActionsHandler struct {
	client *firestore.Client
}

func NewOrderActionsHandler(client *firestore.Client) *OrderActionsHandler {
	return &OrderActionsHandler{client: client}
}

// ============================================================================
// DATA STRUCTURES
// ============================================================================

// OrderHold represents a hold on an order
type OrderHold struct {
	HoldID      string    `firestore:"hold_id" json:"hold_id"`
	OrderID     string    `firestore:"order_id" json:"order_id"`
	Reason      string    `firestore:"reason" json:"reason"`
	CreatedBy   string    `firestore:"created_by" json:"created_by"`
	CreatedAt   time.Time `firestore:"created_at" json:"created_at"`
	ReleasedAt  *time.Time `firestore:"released_at,omitempty" json:"released_at,omitempty"`
	ReleasedBy  string    `firestore:"released_by,omitempty" json:"released_by,omitempty"`
}

// OrderLock represents a lock on an order
type OrderLock struct {
	LockID    string    `firestore:"lock_id" json:"lock_id"`
	OrderID   string    `firestore:"order_id" json:"order_id"`
	LockedBy  string    `firestore:"locked_by" json:"locked_by"`
	LockedAt  time.Time `firestore:"locked_at" json:"locked_at"`
	Reason    string    `firestore:"reason,omitempty" json:"reason,omitempty"`
}

// OrderNote represents a note on an order
type OrderNote struct {
	NoteID     string    `firestore:"note_id" json:"note_id"`
	OrderID    string    `firestore:"order_id" json:"order_id"`
	Content    string    `firestore:"content" json:"content"`
	CreatedBy  string    `firestore:"created_by" json:"created_by"`
	CreatedAt  time.Time `firestore:"created_at" json:"created_at"`
	IsInternal bool      `firestore:"is_internal" json:"is_internal"`
}

// OrderTag represents a tag on an order
type OrderTag struct {
	OrderID string `firestore:"order_id" json:"order_id"`
	TagID   string `firestore:"tag_id" json:"tag_id"`
	AddedBy string `firestore:"added_by" json:"added_by"`
	AddedAt time.Time `firestore:"added_at" json:"added_at"`
}

// HoldOrdersRequest represents the request to hold orders
type HoldOrdersRequest struct {
	OrderIDs []string `json:"order_ids"`
	Reason   string   `json:"reason"`
}

// LockOrdersRequest represents the request to lock orders
type LockOrdersRequest struct {
	OrderIDs []string `json:"order_ids"`
	Reason   string   `json:"reason"`
}

// TagOrdersRequest represents the request to tag orders
type TagOrdersRequest struct {
	OrderIDs []string `json:"order_ids"`
	TagID    string   `json:"tag_id"`
}

// AddNoteRequest represents the request to add a note
type AddNoteRequest struct {
	Content    string `json:"content"`
	IsInternal bool   `json:"is_internal"`
}

// HoldOrders places holds on multiple orders
func (h *OrderActionsHandler) HoldOrders(c *gin.Context) {
	tenantID := c.GetHeader("X-Tenant-Id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "X-Tenant-Id header required"})
		return
	}

	var req HoldOrdersRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	if len(req.OrderIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No order IDs provided"})
		return
	}

	if req.Reason == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Reason is required"})
		return
	}

	ctx := c.Request.Context()
	client := h.client

	// Create holds for each order
	var holds []OrderHold
	for _, orderID := range req.OrderIDs {
		holdID := uuid.New().String()
		hold := OrderHold{
			HoldID:    holdID,
			OrderID:   orderID,
			Reason:    req.Reason,
			CreatedBy: "current_user", // TODO: Get from auth context
			CreatedAt: time.Now(),
		}

		// Save hold to Firestore
		_, err := client.Collection(fmt.Sprintf("tenants/%s/order_holds", tenantID)).
			Doc(holdID).
			Set(ctx, hold)
		if err != nil {
			log.Printf("Failed to create hold for order %s: %v", orderID, err)
			continue
		}

		// Update order status
		_, err = client.Collection(fmt.Sprintf("tenants/%s/orders", tenantID)).
			Doc(orderID).
			Update(ctx, []firestore.Update{
				{Path: "on_hold", Value: true},
				{Path: "hold_reason", Value: req.Reason},
				{Path: "updated_at", Value: time.Now()},
			})
		if err != nil {
			log.Printf("Failed to update order %s on_hold status: %v", orderID, err)
		}

		holds = append(holds, hold)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": fmt.Sprintf("%d orders placed on hold", len(holds)),
		"holds":   holds,
	})
}

// ReleaseHolds releases holds on multiple orders
func (h *OrderActionsHandler) ReleaseHolds(c *gin.Context) {
	tenantID := c.GetHeader("X-Tenant-Id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "X-Tenant-Id header required"})
		return
	}

	var req struct {
		OrderIDs []string `json:"order_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	ctx := c.Request.Context()
	client := h.client
	releasedAt := time.Now()

	for _, orderID := range req.OrderIDs {
		// Find active holds for this order
		iter := client.Collection(fmt.Sprintf("tenants/%s/order_holds", tenantID)).
			Where("order_id", "==", orderID).
			Where("released_at", "==", nil).
			Documents(ctx)

		for {
			doc, err := iter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				log.Printf("Error fetching holds: %v", err)
				continue
			}

			// Mark hold as released
			_, err = doc.Ref.Update(ctx, []firestore.Update{
				{Path: "released_at", Value: releasedAt},
				{Path: "released_by", Value: "current_user"},
			})
			if err != nil {
				log.Printf("Failed to release hold %s: %v", doc.Ref.ID, err)
			}
		}

		// Update order status
		_, err := client.Collection(fmt.Sprintf("tenants/%s/orders", tenantID)).
			Doc(orderID).
			Update(ctx, []firestore.Update{
				{Path: "on_hold", Value: false},
				{Path: "hold_reason", Value: ""},
				{Path: "updated_at", Value: time.Now()},
			})
		if err != nil {
			log.Printf("Failed to update order %s on_hold status: %v", orderID, err)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": fmt.Sprintf("%d holds released", len(req.OrderIDs)),
	})
}

// LockOrders locks multiple orders
func (h *OrderActionsHandler) LockOrders(c *gin.Context) {
	tenantID := c.GetHeader("X-Tenant-Id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "X-Tenant-Id header required"})
		return
	}

	var req LockOrdersRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	if len(req.OrderIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No order IDs provided"})
		return
	}

	ctx := c.Request.Context()
	client := h.client

	var locks []OrderLock
	for _, orderID := range req.OrderIDs {
		lockID := uuid.New().String()
		lock := OrderLock{
			LockID:   lockID,
			OrderID:  orderID,
			LockedBy: "current_user", // TODO: Get from auth context
			LockedAt: time.Now(),
			Reason:   req.Reason,
		}

		_, err := client.Collection(fmt.Sprintf("tenants/%s/order_locks", tenantID)).
			Doc(lockID).
			Set(ctx, lock)
		if err != nil {
			log.Printf("Failed to create lock for order %s: %v", orderID, err)
			continue
		}

		// Update order status
		_, err = client.Collection(fmt.Sprintf("tenants/%s/orders", tenantID)).
			Doc(orderID).
			Update(ctx, []firestore.Update{
				{Path: "locked", Value: true},
				{Path: "locked_by", Value: "current_user"},
				{Path: "lock_reason", Value: req.Reason},
				{Path: "updated_at", Value: time.Now()},
			})
		if err != nil {
			log.Printf("Failed to update order %s locked status: %v", orderID, err)
		}

		locks = append(locks, lock)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": fmt.Sprintf("%d orders locked", len(locks)),
		"locks":   locks,
	})
}

// UnlockOrders unlocks multiple orders
func (h *OrderActionsHandler) UnlockOrders(c *gin.Context) {
	tenantID := c.GetHeader("X-Tenant-Id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "X-Tenant-Id header required"})
		return
	}

	var req struct {
		OrderIDs []string `json:"order_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	ctx := c.Request.Context()
	client := h.client

	for _, orderID := range req.OrderIDs {
		// Delete locks for this order
		iter := client.Collection(fmt.Sprintf("tenants/%s/order_locks", tenantID)).
			Where("order_id", "==", orderID).
			Documents(ctx)

		for {
			doc, err := iter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				log.Printf("Error fetching locks: %v", err)
				continue
			}

			_, err = doc.Ref.Delete(ctx)
			if err != nil {
				log.Printf("Failed to delete lock %s: %v", doc.Ref.ID, err)
			}
		}

		// Update order status
		_, err := client.Collection(fmt.Sprintf("tenants/%s/orders", tenantID)).
			Doc(orderID).
			Update(ctx, []firestore.Update{
				{Path: "locked", Value: false},
				{Path: "locked_by", Value: ""},
				{Path: "lock_reason", Value: ""},
				{Path: "updated_at", Value: time.Now()},
			})
		if err != nil {
			log.Printf("Failed to update order %s locked status: %v", orderID, err)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": fmt.Sprintf("%d orders unlocked", len(req.OrderIDs)),
	})
}

// AddTagToOrders adds a tag to multiple orders by writing directly to the order document's tags[] array.
// Uses Firestore arrayUnion so the tag is visible in ListOrders immediately.
func (h *OrderActionsHandler) AddTagToOrders(c *gin.Context) {
	tenantID := c.GetHeader("X-Tenant-Id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "X-Tenant-Id header required"})
		return
	}

	var req TagOrdersRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	if len(req.OrderIDs) == 0 || req.TagID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Order IDs and tag ID required"})
		return
	}

	ctx := c.Request.Context()
	client := h.client
	updated := 0

	for _, orderID := range req.OrderIDs {
		_, err := client.Collection(fmt.Sprintf("tenants/%s/orders", tenantID)).
			Doc(orderID).
			Update(ctx, []firestore.Update{
				{Path: "tags", Value: firestore.ArrayUnion(req.TagID)},
				{Path: "updated_at", Value: time.Now()},
			})
		if err != nil {
			log.Printf("Failed to add tag to order %s: %v", orderID, err)
			continue
		}
		updated++
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": fmt.Sprintf("Tag added to %d orders", updated),
	})
}

// AddNoteToOrder adds a note to an order
func (h *OrderActionsHandler) AddNoteToOrder(c *gin.Context) {
	tenantID := c.GetHeader("X-Tenant-Id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "X-Tenant-Id header required"})
		return
	}

	orderID := c.Param("order_id")
	if orderID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Order ID required"})
		return
	}

	var req AddNoteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	if req.Content == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Note content required"})
		return
	}

	ctx := c.Request.Context()
	client := h.client

	noteID := uuid.New().String()
	note := OrderNote{
		NoteID:     noteID,
		OrderID:    orderID,
		Content:    req.Content,
		CreatedBy:  "current_user",
		CreatedAt:  time.Now(),
		IsInternal: req.IsInternal,
	}

	_, err := client.Collection(fmt.Sprintf("tenants/%s/order_notes", tenantID)).
		Doc(noteID).
		Set(ctx, note)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add note"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"note":    note,
	})
}

// GetOrderNotes retrieves all notes for an order
func (h *OrderActionsHandler) GetOrderNotes(c *gin.Context) {
	tenantID := c.GetHeader("X-Tenant-Id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "X-Tenant-Id header required"})
		return
	}

	orderID := c.Param("order_id")
	if orderID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Order ID required"})
		return
	}

	ctx := c.Request.Context()
	client := h.client

	iter := client.Collection(fmt.Sprintf("tenants/%s/order_notes", tenantID)).
		Where("order_id", "==", orderID).
		OrderBy("created_at", firestore.Desc).
		Documents(ctx)

	var notes []OrderNote
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch notes"})
			return
		}

		var note OrderNote
		doc.DataTo(&note)
		notes = append(notes, note)
	}

	c.JSON(http.StatusOK, gin.H{
		"notes": notes,
	})
}

// ============================================================================
// TASK 6: ORDER TAG DEFINITIONS  —  GET/POST /api/v1/settings/order-tags
// ============================================================================

// OrderTagDefinition is a named, shaped, coloured tag that can be assigned to orders.
type OrderTagDefinition struct {
	TagID     string    `firestore:"tag_id" json:"tag_id"`
	Name      string    `firestore:"name" json:"name"`
	Color     string    `firestore:"color" json:"color"`   // hex
	Shape     string    `firestore:"shape" json:"shape"`   // square|circle|triangle|star|diamond|flag
	IsDefault bool      `firestore:"is_default" json:"is_default"`
	CreatedAt time.Time `firestore:"created_at" json:"created_at"`
}

var defaultTagDefinitions = []OrderTagDefinition{
	{TagID: "tag-square",   Name: "Square",   Color: "#3b82f6", Shape: "square",   IsDefault: true},
	{TagID: "tag-circle",   Name: "Circle",   Color: "#22c55e", Shape: "circle",   IsDefault: true},
	{TagID: "tag-triangle", Name: "Triangle", Color: "#f59e0b", Shape: "triangle", IsDefault: true},
	{TagID: "tag-star",     Name: "Star",     Color: "#a855f7", Shape: "star",     IsDefault: true},
	{TagID: "tag-diamond",  Name: "Diamond",  Color: "#ef4444", Shape: "diamond",  IsDefault: true},
	{TagID: "tag-flag",     Name: "Flag",     Color: "#14b8a6", Shape: "flag",     IsDefault: true},
}

// ListTagDefinitions  GET /api/v1/settings/order-tags
func (h *OrderActionsHandler) ListTagDefinitions(c *gin.Context) {
	tenantID := c.GetHeader("X-Tenant-Id")
	if tenantID == "" {
		tenantID = c.GetString("tenant_id")
	}
	ctx := c.Request.Context()

	iter := h.client.Collection(fmt.Sprintf("tenants/%s/order_tag_definitions", tenantID)).
		OrderBy("created_at", firestore.Asc).Documents(ctx)

	var tags []OrderTagDefinition
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		var t OrderTagDefinition
		if doc.DataTo(&t) == nil {
			tags = append(tags, t)
		}
	}

	// If no custom tags defined yet, return the defaults
	if len(tags) == 0 {
		tags = defaultTagDefinitions
	}

	c.JSON(200, gin.H{"tags": tags})
}

// CreateTagDefinition  POST /api/v1/settings/order-tags
func (h *OrderActionsHandler) CreateTagDefinition(c *gin.Context) {
	tenantID := c.GetHeader("X-Tenant-Id")
	if tenantID == "" {
		tenantID = c.GetString("tenant_id")
	}
	ctx := c.Request.Context()

	var req struct {
		Name  string `json:"name" binding:"required"`
		Color string `json:"color"`
		Shape string `json:"shape"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if req.Color == "" {
		req.Color = "#6b7280"
	}
	if req.Shape == "" {
		req.Shape = "circle"
	}
	id := fmt.Sprintf("tag-%s", uuid.New().String()[:8])
	tag := OrderTagDefinition{
		TagID:     id,
		Name:      req.Name,
		Color:     req.Color,
		Shape:     req.Shape,
		IsDefault: false,
		CreatedAt: time.Now(),
	}
	if _, err := h.client.Collection(fmt.Sprintf("tenants/%s/order_tag_definitions", tenantID)).Doc(id).Set(ctx, tag); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(201, gin.H{"tag": tag})
}

// UpdateTagDefinition  PUT /api/v1/settings/order-tags/:id
func (h *OrderActionsHandler) UpdateTagDefinition(c *gin.Context) {
	tenantID := c.GetHeader("X-Tenant-Id")
	if tenantID == "" {
		tenantID = c.GetString("tenant_id")
	}
	tagID := c.Param("id")
	ctx := c.Request.Context()

	var req struct {
		Name  string `json:"name"`
		Color string `json:"color"`
		Shape string `json:"shape"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	updates := []firestore.Update{{Path: "updated_at", Value: time.Now()}}
	if req.Name != "" {
		updates = append(updates, firestore.Update{Path: "name", Value: req.Name})
	}
	if req.Color != "" {
		updates = append(updates, firestore.Update{Path: "color", Value: req.Color})
	}
	if req.Shape != "" {
		updates = append(updates, firestore.Update{Path: "shape", Value: req.Shape})
	}
	if _, err := h.client.Collection(fmt.Sprintf("tenants/%s/order_tag_definitions", tenantID)).Doc(tagID).Update(ctx, updates); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// DeleteTagDefinition  DELETE /api/v1/settings/order-tags/:id
func (h *OrderActionsHandler) DeleteTagDefinition(c *gin.Context) {
	tenantID := c.GetHeader("X-Tenant-Id")
	if tenantID == "" {
		tenantID = c.GetString("tenant_id")
	}
	tagID := c.Param("id")
	ctx := c.Request.Context()

	if _, err := h.client.Collection(fmt.Sprintf("tenants/%s/order_tag_definitions", tenantID)).Doc(tagID).Delete(ctx); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// RemoveTagFromOrders  DELETE /api/v1/orders/tags  (bulk tag removal)
// Uses Firestore arrayRemove to remove the tag directly from each order document.
func (h *OrderActionsHandler) RemoveTagFromOrders(c *gin.Context) {
	tenantID := c.GetHeader("X-Tenant-Id")
	if tenantID == "" {
		tenantID = c.GetString("tenant_id")
	}

	var req TagOrdersRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if len(req.OrderIDs) == 0 || req.TagID == "" {
		c.JSON(400, gin.H{"error": "Order IDs and tag ID required"})
		return
	}

	ctx := c.Request.Context()
	client := h.client
	removed := 0

	for _, orderID := range req.OrderIDs {
		_, err := client.Collection(fmt.Sprintf("tenants/%s/orders", tenantID)).
			Doc(orderID).
			Update(ctx, []firestore.Update{
				{Path: "tags", Value: firestore.ArrayRemove(req.TagID)},
				{Path: "updated_at", Value: time.Now()},
			})
		if err != nil {
			log.Printf("Failed to remove tag from order %s: %v", orderID, err)
			continue
		}
		removed++
	}
	c.JSON(200, gin.H{"removed": removed})
}

// GetOrderTags  GET /api/v1/orders/:id/tags
func (h *OrderActionsHandler) GetOrderTags(c *gin.Context) {
	tenantID := c.GetHeader("X-Tenant-Id")
	if tenantID == "" {
		tenantID = c.GetString("tenant_id")
	}
	orderID := c.Param("id")
	ctx := c.Request.Context()

	iter := h.client.Collection(fmt.Sprintf("tenants/%s/order_tags", tenantID)).
		Where("order_id", "==", orderID).Documents(ctx)

	var tags []OrderTag
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		var t OrderTag
		if doc.DataTo(&t) == nil {
			tags = append(tags, t)
		}
	}
	c.JSON(200, gin.H{"tags": tags})
}

// ============================================================================
// TASK 10: INVOICE PRINT STATUS  —  POST /api/v1/orders/:id/mark-invoice-printed
// ============================================================================

// MarkInvoicePrinted  POST /api/v1/orders/:id/mark-invoice-printed
func (h *OrderActionsHandler) MarkInvoicePrinted(c *gin.Context) {
	tenantID := c.GetHeader("X-Tenant-Id")
	if tenantID == "" {
		tenantID = c.GetString("tenant_id")
	}
	orderID := c.Param("id")
	ctx := c.Request.Context()

	_, err := h.client.Collection(fmt.Sprintf("tenants/%s/orders", tenantID)).Doc(orderID).
		Update(ctx, []firestore.Update{
			{Path: "invoice_printed", Value: true},
			{Path: "invoice_printed_at", Value: time.Now().Format(time.RFC3339)},
			{Path: "updated_at", Value: time.Now().Format(time.RFC3339)},
		})
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}
