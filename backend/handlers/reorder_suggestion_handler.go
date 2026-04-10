package handlers

// ============================================================================
// REORDER SUGGESTION HANDLER — SESSION 3 (Task 4)
// ============================================================================
// Generates draft purchase order line suggestions when inventory items fall
// at or below their reorder_point. Suggestions sit in a pending state until
// staff approve them (converting to a real PO line) or dismiss them.
//
// Firestore: tenants/{t}/reorder_suggestions/{suggestion_id}
//
// Routes:
//   POST /purchase-orders/suggestions/generate  — scan inventory, create suggestions
//   GET  /purchase-orders/suggestions           — list pending suggestions
//   POST /purchase-orders/suggestions/:id/approve — convert to confirmed PO line
//   POST /purchase-orders/suggestions/:id/dismiss — mark dismissed
// ============================================================================

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

// ─── Types ────────────────────────────────────────────────────────────────────

type ReorderSuggestion struct {
	SuggestionID  string    `firestore:"suggestion_id"  json:"suggestion_id"`
	TenantID      string    `firestore:"tenant_id"      json:"tenant_id"`
	SKU           string    `firestore:"sku"            json:"sku"`
	ProductID     string    `firestore:"product_id"     json:"product_id"`
	ProductName   string    `firestore:"product_name"   json:"product_name"`
	CurrentStock  int       `firestore:"current_stock"  json:"current_stock"`
	ReorderPoint  int       `firestore:"reorder_point"  json:"reorder_point"`
	SuggestedQty  int       `firestore:"suggested_qty"  json:"suggested_qty"`
	SupplierID    string    `firestore:"supplier_id"    json:"supplier_id"`
	SupplierName  string    `firestore:"supplier_name"  json:"supplier_name"`
	SupplierSKU   string    `firestore:"supplier_sku"   json:"supplier_sku"`
	UnitCost      float64   `firestore:"unit_cost"      json:"unit_cost"`
	Currency      string    `firestore:"currency"       json:"currency"`
	Status        string    `firestore:"status"         json:"status"` // pending | approved | dismissed
	ApprovedPOID  string    `firestore:"approved_po_id,omitempty"  json:"approved_po_id,omitempty"`
	ApprovedPONum string    `firestore:"approved_po_number,omitempty" json:"approved_po_number,omitempty"`
	CreatedAt     time.Time `firestore:"created_at"     json:"created_at"`
	UpdatedAt     time.Time `firestore:"updated_at"     json:"updated_at"`
}

// ─── Handler ──────────────────────────────────────────────────────────────────

type ReorderSuggestionHandler struct {
	client            *firestore.Client
	purchaseOrderHandler *PurchaseOrderHandler
}

func NewReorderSuggestionHandler(client *firestore.Client, poHandler *PurchaseOrderHandler) *ReorderSuggestionHandler {
	return &ReorderSuggestionHandler{
		client:            client,
		purchaseOrderHandler: poHandler,
	}
}

func (h *ReorderSuggestionHandler) col(tenantID string) *firestore.CollectionRef {
	return h.client.Collection("tenants").Doc(tenantID).Collection("reorder_suggestions")
}

func (h *ReorderSuggestionHandler) tenantID(c *gin.Context) string {
	if tid := c.GetString("tenant_id"); tid != "" {
		return tid
	}
	return c.GetHeader("X-Tenant-Id")
}

// ─── GET /purchase-orders/suggestions ────────────────────────────────────────

func (h *ReorderSuggestionHandler) ListSuggestions(c *gin.Context) {
	tenantID := h.tenantID(c)
	ctx := c.Request.Context()

	statusFilter := c.Query("status")
	if statusFilter == "" {
		statusFilter = "pending"
	}

	q := h.col(tenantID).Where("status", "==", statusFilter).OrderBy("created_at", firestore.Desc).Limit(200)
	iter := q.Documents(ctx)
	defer iter.Stop()

	var suggestions []ReorderSuggestion
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list suggestions"})
			return
		}
		var s ReorderSuggestion
		doc.DataTo(&s)
		suggestions = append(suggestions, s)
	}
	if suggestions == nil {
		suggestions = []ReorderSuggestion{}
	}

	c.JSON(http.StatusOK, gin.H{
		"suggestions": suggestions,
		"count":       len(suggestions),
		"status":      statusFilter,
	})
}

// ─── POST /purchase-orders/suggestions/generate ──────────────────────────────
// Scans inventory for items at/below reorder_point and creates pending
// suggestions. Skips SKUs that already have a pending suggestion.

func (h *ReorderSuggestionHandler) GenerateSuggestions(c *gin.Context) {
	tenantID := h.tenantID(c)
	ctx := c.Request.Context()

	created, skipped, err := h.generateForTenant(ctx, tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"created":  created,
		"skipped":  skipped,
		"message":  fmt.Sprintf("Generated %d new suggestion(s), %d skipped (already pending)", created, skipped),
	})
}

// generateForTenant is the core logic — callable from both the HTTP handler
// and from the stock alert scheduler goroutine.
func (h *ReorderSuggestionHandler) generateForTenant(ctx context.Context, tenantID string) (created, skipped int, err error) {
	// ── Load existing pending suggestions to avoid duplicates ─────────────
	existingPending := map[string]bool{} // SKU -> true
	pendingIter := h.col(tenantID).Where("status", "==", "pending").Documents(ctx)
	defer pendingIter.Stop()
	for {
		doc, e := pendingIter.Next()
		if e == iterator.Done {
			break
		}
		if e != nil {
			break
		}
		data := doc.Data()
		if s, ok := data["sku"].(string); ok {
			existingPending[s] = true
		}
	}

	// ── Scan inventory for low-stock items ────────────────────────────────
	inventoryCol := h.client.Collection("tenants").Doc(tenantID).Collection("inventory")
	invIter := inventoryCol.Where("reorder_point", ">", 0).Documents(ctx)
	defer invIter.Stop()

	for {
		doc, e := invIter.Next()
		if e == iterator.Done {
			break
		}
		if e != nil {
			err = fmt.Errorf("iterate inventory: %w", e)
			return
		}

		data := doc.Data()
		available, _ := data["total_available"].(int64)
		reorderPoint, _ := data["reorder_point"].(int64)

		if reorderPoint == 0 || available > reorderPoint {
			continue
		}

		sku, _ := data["sku"].(string)
		if sku == "" {
			continue
		}

		// Skip if already pending
		if existingPending[sku] {
			skipped++
			continue
		}

		productID, _ := data["product_id"].(string)
		productName, _ := data["product_name"].(string)

		// ── Look up preferred supplier from product suppliers array ────────
		var supplierID, supplierName, supplierSKU, currency string
		var unitCost float64

		if productID != "" {
			prodDoc, e2 := h.client.Collection("tenants").Doc(tenantID).
				Collection("products").Doc(productID).Get(ctx)
			if e2 == nil {
				prodData := prodDoc.Data()
				if suppliersRaw, ok := prodData["suppliers"].([]interface{}); ok {
					// Find the default/lowest-priority supplier
					bestPriority := 9999
					for _, sr := range suppliersRaw {
						sm, ok2 := sr.(map[string]interface{})
						if !ok2 {
							continue
						}
						priority := 9999
						if p, ok3 := sm["priority"].(int64); ok3 {
							priority = int(p)
						}
						isDefault, _ := sm["is_default"].(bool)
						if isDefault {
							priority = 0
						}
						if priority < bestPriority {
							bestPriority = priority
							supplierID, _ = sm["supplier_id"].(string)
							supplierName, _ = sm["supplier_name"].(string)
							supplierSKU, _ = sm["supplier_sku"].(string)
							if c2, ok3 := sm["unit_cost"].(float64); ok3 {
								unitCost = c2
							}
							currency, _ = sm["currency"].(string)
						}
					}
				}
			}
		}

		if currency == "" {
			currency = "GBP"
		}

		// Suggested qty = max(reorder_point * 2 - available, 1)
		suggestedQty := int(reorderPoint)*2 - int(available)
		if suggestedQty < 1 {
			suggestedQty = 1
		}

		suggestionID := "sugg_" + uuid.New().String()
		now := time.Now().UTC()
		suggestion := ReorderSuggestion{
			SuggestionID: suggestionID,
			TenantID:     tenantID,
			SKU:          sku,
			ProductID:    productID,
			ProductName:  productName,
			CurrentStock: int(available),
			ReorderPoint: int(reorderPoint),
			SuggestedQty: suggestedQty,
			SupplierID:   supplierID,
			SupplierName: supplierName,
			SupplierSKU:  supplierSKU,
			UnitCost:     unitCost,
			Currency:     currency,
			Status:       "pending",
			CreatedAt:    now,
			UpdatedAt:    now,
		}

		if _, e2 := h.col(tenantID).Doc(suggestionID).Set(ctx, suggestion); e2 != nil {
			log.Printf("[ReorderSuggestion] failed to write suggestion for %s/%s: %v", tenantID, sku, e2)
			continue
		}

		existingPending[sku] = true
		created++
	}

	return
}

// GenerateForTenantBackground is called from the stock alert scheduler.
func (h *ReorderSuggestionHandler) GenerateForTenantBackground(tenantID string) {
	ctx := context.Background()
	created, skipped, err := h.generateForTenant(ctx, tenantID)
	if err != nil {
		log.Printf("[ReorderSuggestion] generate error for tenant %s: %v", tenantID, err)
		return
	}
	if created > 0 {
		log.Printf("[ReorderSuggestion] tenant %s: %d new suggestion(s), %d skipped", tenantID, created, skipped)
	}
}

// ─── POST /purchase-orders/suggestions/:id/approve ───────────────────────────
// Converts a pending suggestion into a draft PO (or appends to an existing
// draft PO for the same supplier if one exists).

func (h *ReorderSuggestionHandler) ApproveSuggestion(c *gin.Context) {
	tenantID := h.tenantID(c)
	suggestionID := c.Param("id")
	ctx := c.Request.Context()

	// Load suggestion
	doc, err := h.col(tenantID).Doc(suggestionID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "suggestion not found"})
		return
	}
	var s ReorderSuggestion
	doc.DataTo(&s)

	if s.Status != "pending" {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("suggestion is already %s", s.Status)})
		return
	}

	// Allow caller to override qty
	var req struct {
		Qty        int    `json:"qty"`
		SupplierID string `json:"supplier_id"`
	}
	c.ShouldBindJSON(&req) // ignore parse error — all fields optional

	approveQty := s.SuggestedQty
	if req.Qty > 0 {
		approveQty = req.Qty
	}
	approveSupplier := s.SupplierID
	if req.SupplierID != "" {
		approveSupplier = req.SupplierID
	}

	if approveSupplier == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no supplier configured for this product — set a preferred supplier first"})
		return
	}

	// ── Find or create a draft PO for this supplier ────────────────────────
	poCol := h.client.Collection("tenants").Doc(tenantID).Collection("purchase_orders")

	var poID, poNumber string
	existingIter := poCol.
		Where("supplier_id", "==", approveSupplier).
		Where("status", "==", "draft").
		Where("source", "==", "reorder_suggestion").
		Limit(1).
		Documents(ctx)
	defer existingIter.Stop()

	existingDoc, e2 := existingIter.Next()
	if e2 == nil {
		// Append line to existing draft PO
		poID = existingDoc.Ref.ID
		poData := existingDoc.Data()
		poNumber, _ = poData["po_number"].(string)

		lineID := "pol_" + uuid.New().String()
		newLine := map[string]interface{}{
			"line_id":      lineID,
			"product_id":   s.ProductID,
			"internal_sku": s.SKU,
			"supplier_sku": s.SupplierSKU,
			"description":  s.ProductName,
			"qty_ordered":  approveQty,
			"qty_received": 0,
			"unit_cost":    s.UnitCost,
			"currency":     s.Currency,
		}

		existingLines, _ := poData["lines"].([]interface{})
		existingLines = append(existingLines, newLine)

		if _, e3 := existingDoc.Ref.Update(ctx, []firestore.Update{
			{Path: "lines", Value: existingLines},
			{Path: "updated_at", Value: time.Now().UTC()},
		}); e3 != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update draft PO"})
			return
		}
	} else {
		// Create a new draft PO
		poID = "po_" + uuid.New().String()
		// Use the PO handler's number generator via a synthetic gin context would
		// be circular — generate directly here
		year := time.Now().Year()
		poNumber = fmt.Sprintf("PO-%d-SUGG-%s", year, poID[3:9])

		// Look up supplier name
		supplierName := s.SupplierName
		if supplierDoc, e3 := h.client.Collection("tenants").Doc(tenantID).
			Collection("suppliers").Doc(approveSupplier).Get(ctx); e3 == nil {
			supplierData := supplierDoc.Data()
			if sn, ok := supplierData["name"].(string); ok {
				supplierName = sn
			}
		}

		now := time.Now().UTC()
		po := map[string]interface{}{
			"po_id":          poID,
			"po_number":      poNumber,
			"tenant_id":      tenantID,
			"supplier_id":    approveSupplier,
			"supplier_name":  supplierName,
			"type":           "standard",
			"order_method":   "manual",
			"status":         "draft",
			"source":         "reorder_suggestion",
			"lines": []map[string]interface{}{
				{
					"line_id":      "pol_" + uuid.New().String(),
					"product_id":   s.ProductID,
					"internal_sku": s.SKU,
					"supplier_sku": s.SupplierSKU,
					"description":  s.ProductName,
					"qty_ordered":  approveQty,
					"qty_received": 0,
					"unit_cost":    s.UnitCost,
					"currency":     s.Currency,
				},
			},
			"total_cost":      float64(approveQty) * s.UnitCost,
			"currency":        s.Currency,
			"internal_notes":  fmt.Sprintf("Created from reorder suggestion %s", suggestionID),
			"created_at":      now,
			"updated_at":      now,
		}

		if _, e3 := poCol.Doc(poID).Set(ctx, po); e3 != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create PO"})
			return
		}
	}

	// ── Mark suggestion as approved ────────────────────────────────────────
	now := time.Now().UTC()
	h.col(tenantID).Doc(suggestionID).Update(ctx, []firestore.Update{
		{Path: "status", Value: "approved"},
		{Path: "approved_po_id", Value: poID},
		{Path: "approved_po_number", Value: poNumber},
		{Path: "updated_at", Value: now},
	})

	c.JSON(http.StatusOK, gin.H{
		"suggestion_id": suggestionID,
		"po_id":         poID,
		"po_number":     poNumber,
		"message":       fmt.Sprintf("Suggestion approved — added to PO %s", poNumber),
	})
}

// ─── POST /purchase-orders/suggestions/:id/dismiss ───────────────────────────

func (h *ReorderSuggestionHandler) DismissSuggestion(c *gin.Context) {
	tenantID := h.tenantID(c)
	suggestionID := c.Param("id")
	ctx := c.Request.Context()

	doc, err := h.col(tenantID).Doc(suggestionID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "suggestion not found"})
		return
	}
	var s ReorderSuggestion
	doc.DataTo(&s)
	if s.Status != "pending" {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("suggestion is already %s", s.Status)})
		return
	}

	h.col(tenantID).Doc(suggestionID).Update(ctx, []firestore.Update{
		{Path: "status", Value: "dismissed"},
		{Path: "updated_at", Value: time.Now().UTC()},
	})

	c.JSON(http.StatusOK, gin.H{"suggestion_id": suggestionID, "status": "dismissed"})
}
