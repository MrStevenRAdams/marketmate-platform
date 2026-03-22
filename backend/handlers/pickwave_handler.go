package handlers

import (
	"net/http"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"
)

// ============================================================================
// PICKWAVE HANDLER — Full wave picking management (Session 7)
//
// Routes:
//   GET    /api/v1/pickwaves                    List pickwaves
//   POST   /api/v1/pickwaves                    Generate pickwave from orders
//   GET    /api/v1/pickwaves/:id                Get pickwave detail with lines
//   PUT    /api/v1/pickwaves/:id                Update status or assigned user
//   DELETE /api/v1/pickwaves/:id                Cancel pickwave
//   PUT    /api/v1/pickwaves/:id/lines/:line_id Update a line
// ============================================================================

type PickwaveHandler struct {
	client *firestore.Client
}

func NewPickwaveHandler(client *firestore.Client) *PickwaveHandler {
	return &PickwaveHandler{client: client}
}

type PickwaveLine struct {
	ID             string `firestore:"id"              json:"id"`
	PickwaveID     string `firestore:"pickwave_id"     json:"pickwave_id"`
	OrderID        string `firestore:"order_id"        json:"order_id"`
	SKU            string `firestore:"sku"             json:"sku"`
	ProductName    string `firestore:"product_name"    json:"product_name"`
	Quantity       int    `firestore:"quantity"        json:"quantity"`
	BinrackID      string `firestore:"binrack_id"      json:"binrack_id"`
	BinrackName    string `firestore:"binrack_name"    json:"binrack_name"`
	Status         string `firestore:"status"          json:"status"`          // pending|picked|short
	PickedQuantity int    `firestore:"picked_quantity" json:"picked_quantity"`
	Title          string `firestore:"title"           json:"title,omitempty"`
	BinLocation    string `firestore:"bin_location"    json:"bin_location,omitempty"`
	Picked         int    `firestore:"picked"          json:"picked,omitempty"`
}

type Pickwave struct {
	ID             string         `firestore:"id"               json:"id"`
	TenantID       string         `firestore:"tenant_id"        json:"tenant_id"`
	Name           string         `firestore:"name"             json:"name"`
	Status         string         `firestore:"status"           json:"status"` // draft|in_progress|complete|despatched|cancelled
	Type           string         `firestore:"type"             json:"type"`
	Grouping       string         `firestore:"grouping"         json:"grouping"`
	AssignedUserID string         `firestore:"assigned_user_id" json:"assigned_user_id,omitempty"`
	MaxOrders      int            `firestore:"max_orders"       json:"max_orders"`
	MaxItems       int            `firestore:"max_items"        json:"max_items"`
	SortBy         string         `firestore:"sort_by"          json:"sort_by"`
	ShowNextOnly   bool           `firestore:"show_next_only"   json:"show_next_only"`
	OrderIDs       []string       `firestore:"order_ids"        json:"order_ids"`
	OrderCount     int            `firestore:"order_count"      json:"order_count"`
	ItemCount      int            `firestore:"item_count"       json:"item_count"`
	Lines          []PickwaveLine `firestore:"lines"            json:"lines,omitempty"`
	CreatedAt      time.Time      `firestore:"created_at"       json:"created_at"`
	UpdatedAt      time.Time      `firestore:"updated_at"       json:"updated_at"`
}

func (h *PickwaveHandler) col(tenantID string) *firestore.CollectionRef {
	return h.client.Collection("tenants").Doc(tenantID).Collection("pickwaves")
}

// GET /api/v1/pickwaves
func (h *PickwaveHandler) ListPickwaves(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()
	statusFilter := c.Query("status")

	var iter *firestore.DocumentIterator
	if statusFilter != "" {
		iter = h.col(tenantID).Where("status", "==", statusFilter).
			OrderBy("created_at", firestore.Desc).Limit(200).Documents(ctx)
	} else {
		iter = h.col(tenantID).OrderBy("created_at", firestore.Desc).Limit(200).Documents(ctx)
	}
	defer iter.Stop()

	var waves []Pickwave
	for {
		snap, err := iter.Next()
		if err == iterator.Done { break }
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list pickwaves"})
			return
		}
		var w Pickwave
		snap.DataTo(&w)
		w.Lines = nil
		waves = append(waves, w)
	}
	if waves == nil { waves = []Pickwave{} }
	c.JSON(http.StatusOK, gin.H{"pickwaves": waves, "total": len(waves)})
}

// POST /api/v1/pickwaves
func (h *PickwaveHandler) CreatePickwave(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	var req struct {
		OrderIDs       []string `json:"order_ids" binding:"required"`
		Name           string   `json:"name"`
		Type           string   `json:"type"`
		Grouping       string   `json:"grouping"`
		MaxOrders      int      `json:"max_orders"`
		MaxItems       int      `json:"max_items"`
		SortBy         string   `json:"sort_by"`
		ShowNextOnly   bool     `json:"show_next_only"`
		AssignedUserID string   `json:"assigned_user_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.OrderIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "order_ids required"})
		return
	}

	id := "pw_" + uuid.New().String()
	name := req.Name
	if name == "" {
		name = "Wave " + time.Now().Format("2006-01-02 15:04")
	}

	orderIDs := req.OrderIDs
	if req.MaxOrders > 0 && len(orderIDs) > req.MaxOrders {
		orderIDs = orderIDs[:req.MaxOrders]
	}

	var lines []PickwaveLine
	for _, orderID := range orderIDs {
		orderSnap, err := h.client.Collection("tenants").Doc(tenantID).
			Collection("orders").Doc(orderID).Get(ctx)
		if err != nil || !orderSnap.Exists() { continue }
		rawData := orderSnap.Data()
		lineItems, ok := rawData["line_items"].([]interface{})
		if !ok { continue }
		for _, li := range lineItems {
			item, ok := li.(map[string]interface{})
			if !ok { continue }
			sku, _ := item["sku"].(string)
			title, _ := item["title"].(string)
			productName, _ := item["product_name"].(string)
			if productName == "" { productName = title }
			qty := 0
			if q, ok := item["quantity"].(int64); ok { qty = int(q) }
			binrackID, _ := item["binrack_id"].(string)
			binrackName, _ := item["bin_location"].(string)

			lines = append(lines, PickwaveLine{
				ID:          "pwl_" + uuid.New().String(),
				PickwaveID:  id,
				OrderID:     orderID,
				SKU:         sku,
				ProductName: productName,
				Quantity:    qty,
				BinrackID:   binrackID,
				BinrackName: binrackName,
				Status:      "pending",
				Title:       productName,
				BinLocation: binrackName,
			})
		}
	}

	if req.MaxItems > 0 && len(lines) > req.MaxItems {
		lines = lines[:req.MaxItems]
	}

	sortBy := req.SortBy
	if sortBy == "" { sortBy = "sku" }
	sortPickwaveLines(lines, sortBy)
	if lines == nil { lines = []PickwaveLine{} }

	pickwaveType := req.Type
	if pickwaveType == "" { pickwaveType = "multi_sku" }
	grouping := req.Grouping
	if grouping == "" { grouping = "single_order" }

	now := time.Now().UTC()
	wave := Pickwave{
		ID:             id,
		TenantID:       tenantID,
		Name:           name,
		Status:         "draft",
		Type:           pickwaveType,
		Grouping:       grouping,
		AssignedUserID: req.AssignedUserID,
		MaxOrders:      req.MaxOrders,
		MaxItems:       req.MaxItems,
		SortBy:         sortBy,
		ShowNextOnly:   req.ShowNextOnly,
		OrderIDs:       orderIDs,
		OrderCount:     len(orderIDs),
		ItemCount:      len(lines),
		Lines:          lines,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if _, err := h.col(tenantID).Doc(id).Set(ctx, wave); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create pickwave"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"pickwave": wave})
}

func sortPickwaveLines(lines []PickwaveLine, sortBy string) {
	for i := 1; i < len(lines); i++ {
		for j := i; j > 0; j-- {
			var a, b string
			switch sortBy {
			case "binrack":
				a, b = lines[j-1].BinrackName, lines[j].BinrackName
			case "alphabetical":
				a, b = lines[j-1].ProductName, lines[j].ProductName
			default:
				a, b = lines[j-1].SKU, lines[j].SKU
			}
			if a > b {
				lines[j-1], lines[j] = lines[j], lines[j-1]
			} else {
				break
			}
		}
	}
}

// GET /api/v1/pickwaves/:id
func (h *PickwaveHandler) GetPickwave(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	waveID := c.Param("id")
	ctx := c.Request.Context()

	snap, err := h.col(tenantID).Doc(waveID).Get(ctx)
	if err != nil || !snap.Exists() {
		c.JSON(http.StatusNotFound, gin.H{"error": "pickwave not found"})
		return
	}
	var wave Pickwave
	snap.DataTo(&wave)
	if wave.Lines == nil { wave.Lines = []PickwaveLine{} }
	if wave.OrderIDs == nil { wave.OrderIDs = []string{} }
	c.JSON(http.StatusOK, gin.H{"pickwave": wave})
}

// PUT /api/v1/pickwaves/:id
func (h *PickwaveHandler) UpdatePickwave(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	waveID := c.Param("id")
	ctx := c.Request.Context()

	snap, err := h.col(tenantID).Doc(waveID).Get(ctx)
	if err != nil || !snap.Exists() {
		c.JSON(http.StatusNotFound, gin.H{"error": "pickwave not found"})
		return
	}
	var wave Pickwave
	snap.DataTo(&wave)

	var req struct {
		Status         *string `json:"status"`
		AssignedUserID *string `json:"assigned_user_id"`
		Name           *string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	allowed := map[string]bool{
		"draft": true, "in_progress": true, "complete": true,
		"despatched": true, "cancelled": true,
		// legacy aliases
		"open": true, "picking": true,
	}
	if req.Status != nil {
		if !allowed[*req.Status] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid status"})
			return
		}
		wave.Status = *req.Status
	}
	if req.AssignedUserID != nil { wave.AssignedUserID = *req.AssignedUserID }
	if req.Name != nil { wave.Name = *req.Name }
	wave.UpdatedAt = time.Now().UTC()

	if _, err := h.col(tenantID).Doc(waveID).Set(ctx, wave); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"pickwave": wave})
}

// DELETE /api/v1/pickwaves/:id
func (h *PickwaveHandler) DeletePickwave(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	waveID := c.Param("id")
	ctx := c.Request.Context()

	if _, err := h.col(tenantID).Doc(waveID).Update(ctx, []firestore.Update{
		{Path: "status", Value: "cancelled"},
		{Path: "updated_at", Value: time.Now().UTC()},
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to cancel pickwave"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}

// PUT /api/v1/pickwaves/:id/lines/:line_id
func (h *PickwaveHandler) UpdatePickwaveLine(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	waveID := c.Param("id")
	lineID := c.Param("line_id")
	ctx := c.Request.Context()

	snap, err := h.col(tenantID).Doc(waveID).Get(ctx)
	if err != nil || !snap.Exists() {
		c.JSON(http.StatusNotFound, gin.H{"error": "pickwave not found"})
		return
	}
	var wave Pickwave
	snap.DataTo(&wave)

	var req struct {
		Status         *string `json:"status"`
		PickedQuantity *int    `json:"picked_quantity"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	lineFound := false
	for i, line := range wave.Lines {
		if line.ID == lineID {
			if req.Status != nil { wave.Lines[i].Status = *req.Status }
			if req.PickedQuantity != nil {
				wave.Lines[i].PickedQuantity = *req.PickedQuantity
				wave.Lines[i].Picked = *req.PickedQuantity
				if *req.PickedQuantity >= line.Quantity {
					wave.Lines[i].Status = "picked"
				} else if *req.PickedQuantity > 0 {
					wave.Lines[i].Status = "short"
				}
			}
			lineFound = true
			break
		}
	}

	if !lineFound {
		c.JSON(http.StatusNotFound, gin.H{"error": "line not found"})
		return
	}

	wave.UpdatedAt = time.Now().UTC()
	if _, err := h.col(tenantID).Doc(waveID).Set(ctx, wave); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update line"})
		return
	}

	var updatedLine PickwaveLine
	for _, l := range wave.Lines {
		if l.ID == lineID { updatedLine = l; break }
	}
	c.JSON(http.StatusOK, gin.H{"line": updatedLine})
}

// Alias for backward compatibility
func (h *PickwaveHandler) UpdatePickwaveStatus(c *gin.Context) {
	h.UpdatePickwave(c)
}
