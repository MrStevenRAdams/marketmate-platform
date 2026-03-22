package handlers

import (
	"net/http"
	"sort"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"
)

// ============================================================================
// SUPPLIER RETURN HANDLER
// ============================================================================

type SupplierReturnHandler struct {
	client *firestore.Client
}

func NewSupplierReturnHandler(client *firestore.Client) *SupplierReturnHandler {
	return &SupplierReturnHandler{client: client}
}

type SupplierReturnLine struct {
	ProductID   string `firestore:"product_id"   json:"product_id"`
	SKU         string `firestore:"sku"          json:"sku"`
	Description string `firestore:"description"  json:"description"`
	QtyReturned int    `firestore:"qty_returned" json:"qty_returned"`
	Reason      string `firestore:"reason"       json:"reason"`
}

type SupplierReturn struct {
	ReturnID   string               `firestore:"return_id"   json:"return_id"`
	POID       string               `firestore:"po_id"       json:"po_id"`
	TenantID   string               `firestore:"tenant_id"   json:"tenant_id"`
	SupplierID string               `firestore:"supplier_id" json:"supplier_id"`
	Lines      []SupplierReturnLine `firestore:"lines"       json:"lines"`
	Status     string               `firestore:"status"      json:"status"` // pending|sent|confirmed
	Notes      string               `firestore:"notes"       json:"notes"`
	CreatedAt  time.Time            `firestore:"created_at"  json:"created_at"`
	UpdatedAt  time.Time            `firestore:"updated_at"  json:"updated_at"`
}

func (h *SupplierReturnHandler) col(tenantID string) *firestore.CollectionRef {
	return h.client.Collection("tenants").Doc(tenantID).Collection("supplier_returns")
}

// ── GET /api/v1/supplier-returns ─────────────────────────────────────────────

func (h *SupplierReturnHandler) List(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()

	var list []SupplierReturn
	iter := h.col(tenantID).OrderBy("created_at", firestore.Desc).Limit(200).Documents(ctx)
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list"})
			return
		}
		var sr SupplierReturn
		doc.DataTo(&sr)
		list = append(list, sr)
	}
	if list == nil {
		list = []SupplierReturn{}
	}
	c.JSON(http.StatusOK, gin.H{"supplier_returns": list})
}

// ── POST /api/v1/purchase-orders/:id/return ──────────────────────────────────

func (h *SupplierReturnHandler) CreateReturn(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	poID := c.Param("id")
	ctx := c.Request.Context()

	// Load the PO to get supplier info
	poDoc, err := h.client.Collection("tenants").Doc(tenantID).Collection("purchase_orders").Doc(poID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "purchase order not found"})
		return
	}
	poData := poDoc.Data()
	supplierID, _ := poData["supplier_id"].(string)

	var req struct {
		Lines []SupplierReturnLine `json:"lines" binding:"required"`
		Notes string               `json:"notes"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	now := time.Now()
	ret := SupplierReturn{
		ReturnID:   "sret_" + uuid.New().String(),
		POID:       poID,
		TenantID:   tenantID,
		SupplierID: supplierID,
		Lines:      req.Lines,
		Notes:      req.Notes,
		Status:     "pending",
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	// Deduct returned qty from inventory via inventory_adjustments
	for _, line := range req.Lines {
		if line.ProductID == "" || line.QtyReturned <= 0 {
			continue
		}
		adj := map[string]interface{}{
			"adjustment_id":   "adj_" + uuid.New().String(),
			"product_id":      line.ProductID,
			"product_sku":     line.SKU,
			"type":            "supplier_return",
			"delta":           -line.QtyReturned,
			"reason":          line.Reason,
			"reference":       "SR-" + ret.ReturnID,
			"po_id":           poID,
			"created_at":      now,
		}
		h.client.Collection("tenants").Doc(tenantID).Collection("inventory_adjustments").Doc(adj["adjustment_id"].(string)).Set(ctx, adj)
	}

	if _, err := h.col(tenantID).Doc(ret.ReturnID).Set(ctx, ret); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create return"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"supplier_return": ret})
}

// ============================================================================
// PICKLIST HANDLER
// ============================================================================

type PicklistHandler struct {
	client *firestore.Client
}

func NewPicklistHandler(client *firestore.Client) *PicklistHandler {
	return &PicklistHandler{client: client}
}

type PicklistItem struct {
	SKU          string `json:"sku"`
	ProductName  string `json:"product_name"`
	LocationPath string `json:"location_path"`
	Binrack      string `json:"binrack"`
	QtyNeeded    int    `json:"qty_needed"`
	OrderIDs     []string `json:"order_ids"`
}

// ── POST /api/v1/orders/picklist ─────────────────────────────────────────────

func (h *PicklistHandler) GeneratePicklist(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()

	var req struct {
		OrderIDs []string `json:"order_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Aggregate lines across all requested orders
	skuMap := map[string]*PicklistItem{}

	for _, orderID := range req.OrderIDs {
		linesIter := h.client.Collection("tenants").Doc(tenantID).Collection("order_lines").
			Where("order_id", "==", orderID).Documents(ctx)
		for {
			doc, err := linesIter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				break
			}
			data := doc.Data()
			sku, _ := data["sku"].(string)
			if sku == "" {
				continue
			}
			qty := 1
			if q, ok := data["quantity"].(int64); ok {
				qty = int(q)
			}
			productName, _ := data["product_name"].(string)
			locationPath, _ := data["location_path"].(string)

			if _, exists := skuMap[sku]; !exists {
				skuMap[sku] = &PicklistItem{
					SKU:          sku,
					ProductName:  productName,
					LocationPath: locationPath,
					QtyNeeded:    0,
					OrderIDs:     []string{},
				}
			}
			skuMap[sku].QtyNeeded += qty
			skuMap[sku].OrderIDs = append(skuMap[sku].OrderIDs, orderID)
		}
		linesIter.Stop()
	}

	// Sort by location path for efficient picking
	var items []PicklistItem
	for _, item := range skuMap {
		items = append(items, *item)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].LocationPath < items[j].LocationPath
	})

	c.JSON(http.StatusOK, gin.H{"picklist": items, "order_count": len(req.OrderIDs)})
}

// ============================================================================
// LABEL PRINTING HANDLER
// ============================================================================

type LabelPrintingHandler struct {
	client *firestore.Client
}

func NewLabelPrintingHandler(client *firestore.Client) *LabelPrintingHandler {
	return &LabelPrintingHandler{client: client}
}

// ── GET /api/v1/shipments/print-queue ────────────────────────────────────────

func (h *LabelPrintingHandler) GetPrintQueue(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()

	var shipments []map[string]interface{}
	iter := h.client.Collection("tenants").Doc(tenantID).Collection("shipments").
		Where("label_url", "!=", "").
		Where("label_printed", "==", false).
		Limit(200).Documents(ctx)
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		shipments = append(shipments, doc.Data())
	}
	if shipments == nil {
		shipments = []map[string]interface{}{}
	}
	c.JSON(http.StatusOK, gin.H{"shipments": shipments})
}

// ── POST /api/v1/shipments/print ─────────────────────────────────────────────

func (h *LabelPrintingHandler) PrintLabels(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()

	var req struct {
		ShipmentIDs []string `json:"shipment_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var labelURLs []string
	for _, sid := range req.ShipmentIDs {
		doc, err := h.client.Collection("tenants").Doc(tenantID).Collection("shipments").Doc(sid).Get(ctx)
		if err != nil {
			continue
		}
		data := doc.Data()
		if url, ok := data["label_url"].(string); ok && url != "" {
			labelURLs = append(labelURLs, url)
		}
		// Mark as printed
		doc.Ref.Update(ctx, []firestore.Update{
			{Path: "label_printed", Value: true},
			{Path: "label_printed_at", Value: time.Now()},
		})
	}

	if labelURLs == nil {
		labelURLs = []string{}
	}
	c.JSON(http.StatusOK, gin.H{"label_urls": labelURLs, "count": len(labelURLs)})
}
