package handlers

// ============================================================================
// STOCK SCRAP HANDLER
// ============================================================================
// Routes:
//   POST   /api/v1/stock-scraps               — Scrap items (records movement + reduces stock)
//   GET    /api/v1/stock-scraps               — List scrap history (filterable by SKU, date)
//   GET    /api/v1/stock-scraps/stats         — Aggregate scrap stats
// ============================================================================

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"
)

type StockScrapHandler struct {
	client *firestore.Client
}

func NewStockScrapHandler(client *firestore.Client) *StockScrapHandler {
	return &StockScrapHandler{client: client}
}

type ScrapRecord struct {
	ScrapID      string    `firestore:"scrap_id"     json:"scrap_id"`
	TenantID     string    `firestore:"tenant_id"    json:"tenant_id"`
	ProductID    string    `firestore:"product_id"   json:"product_id"`
	SKU          string    `firestore:"sku"          json:"sku"`
	ProductName  string    `firestore:"product_name" json:"product_name"`
	LocationID   string    `firestore:"location_id"  json:"location_id"`
	LocationName string    `firestore:"location_name" json:"location_name"`
	Quantity     int       `firestore:"quantity"     json:"quantity"`
	Reason       string    `firestore:"reason"       json:"reason"`
	Notes        string    `firestore:"notes"        json:"notes,omitempty"`
	ScrapValue   float64   `firestore:"scrap_value"  json:"scrap_value"` // cost value of scrapped stock
	Currency     string    `firestore:"currency"     json:"currency"`
	ScrappedBy   string    `firestore:"scrapped_by"  json:"scrapped_by"`
	CreatedAt    time.Time `firestore:"created_at"   json:"created_at"`
}

var validScrapReasons = map[string]bool{
	"damaged":       true,
	"expired":       true,
	"quality_fail":  true,
	"lost":          true,
	"contaminated":  true,
	"obsolete":      true,
	"other":         true,
}

func (h *StockScrapHandler) col(tenantID string) *firestore.CollectionRef {
	return h.client.Collection(fmt.Sprintf("tenants/%s/stock_scraps", tenantID))
}

// ── POST /api/v1/stock-scraps ────────────────────────────────────────────────

func (h *StockScrapHandler) CreateScrap(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant ID required"})
		return
	}

	var req struct {
		ProductID  string  `json:"product_id"  binding:"required"`
		LocationID string  `json:"location_id" binding:"required"`
		Quantity   int     `json:"quantity"    binding:"required,min=1"`
		Reason     string  `json:"reason"      binding:"required"`
		Notes      string  `json:"notes"`
		ScrapValue float64 `json:"scrap_value"`
		Currency   string  `json:"currency"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if !validScrapReasons[req.Reason] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid reason. valid: damaged, expired, quality_fail, lost, contaminated, obsolete, other"})
		return
	}

	ctx := c.Request.Context()

	// Resolve product details
	sku := ""
	productName := ""
	productDoc, err := h.client.Collection(fmt.Sprintf("tenants/%s/products", tenantID)).
		Doc(req.ProductID).Get(ctx)
	if err == nil {
		sku, _ = productDoc.Data()["sku"].(string)
		productName, _ = productDoc.Data()["title"].(string)
	}

	// Resolve location name
	locationName := req.LocationID
	locDoc, err := h.client.Collection(fmt.Sprintf("tenants/%s/warehouse_locations", tenantID)).
		Doc(req.LocationID).Get(ctx)
	if err == nil {
		if n, ok := locDoc.Data()["name"].(string); ok {
			locationName = n
		}
	}

	// Deduct stock via transaction
	inventoryDocID := req.ProductID + "__" + req.LocationID
	inventoryRef := h.client.Collection(fmt.Sprintf("tenants/%s/inventory", tenantID)).Doc(inventoryDocID)

	var actualDeducted int
	txErr := h.client.RunTransaction(ctx, func(ctx2 context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(inventoryRef)
		currentQty := 0
		if err == nil {
			if v, ok := doc.Data()["quantity"].(int64); ok {
				currentQty = int(v)
			}
		}

		deduct := req.Quantity
		if deduct > currentQty {
			deduct = currentQty // can't scrap more than exists
		}
		actualDeducted = deduct
		newQty := currentQty - deduct
		if newQty < 0 {
			newQty = 0
		}

		return tx.Set(inventoryRef, map[string]interface{}{
			"product_id":    req.ProductID,
			"location_id":   req.LocationID,
			"quantity":      newQty,
			"available_qty": newQty,
			"updated_at":    time.Now(),
		}, firestore.MergeAll)
	})

	if txErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to adjust stock"})
		return
	}

	currency := req.Currency
	if currency == "" {
		currency = "GBP"
	}

	scrap := ScrapRecord{
		ScrapID:      uuid.New().String(),
		TenantID:     tenantID,
		ProductID:    req.ProductID,
		SKU:          sku,
		ProductName:  productName,
		LocationID:   req.LocationID,
		LocationName: locationName,
		Quantity:     actualDeducted,
		Reason:       req.Reason,
		Notes:        req.Notes,
		ScrapValue:   req.ScrapValue,
		Currency:     currency,
		ScrappedBy:   c.GetString("user_id"),
		CreatedAt:    time.Now(),
	}

	if _, err := h.col(tenantID).Doc(scrap.ScrapID).Set(ctx, scrap); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to record scrap"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"scrap":            scrap,
		"quantity_scrapped": actualDeducted,
		"warning": func() string {
			if actualDeducted < req.Quantity {
				return fmt.Sprintf("Only %d units were available to scrap (requested %d)", actualDeducted, req.Quantity)
			}
			return ""
		}(),
	})
}

// ── GET /api/v1/stock-scraps ─────────────────────────────────────────────────

func (h *StockScrapHandler) ListScraps(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant ID required"})
		return
	}

	ctx := c.Request.Context()
	productID := c.Query("product_id")
	sku := c.Query("sku")
	reason := c.Query("reason")

	query := h.col(tenantID).OrderBy("created_at", firestore.Desc).Limit(200)
	if productID != "" {
		query = h.col(tenantID).Where("product_id", "==", productID).OrderBy("created_at", firestore.Desc).Limit(200)
	} else if sku != "" {
		query = h.col(tenantID).Where("sku", "==", sku).OrderBy("created_at", firestore.Desc).Limit(200)
	} else if reason != "" {
		query = h.col(tenantID).Where("reason", "==", reason).OrderBy("created_at", firestore.Desc).Limit(200)
	}

	iter := query.Documents(ctx)
	defer iter.Stop()

	var scraps []ScrapRecord
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list scraps"})
			return
		}
		var s ScrapRecord
		if err := doc.DataTo(&s); err == nil {
			scraps = append(scraps, s)
		}
	}
	if scraps == nil {
		scraps = []ScrapRecord{}
	}

	c.JSON(http.StatusOK, gin.H{"scraps": scraps, "count": len(scraps)})
}

// ── GET /api/v1/stock-scraps/stats ───────────────────────────────────────────

func (h *StockScrapHandler) GetScrapStats(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant ID required"})
		return
	}

	ctx := c.Request.Context()
	iter := h.col(tenantID).OrderBy("created_at", firestore.Desc).Limit(1000).Documents(ctx)
	defer iter.Stop()

	totalQty := 0
	totalValue := 0.0
	byReason := map[string]int{}

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		var s ScrapRecord
		if doc.DataTo(&s) == nil {
			totalQty += s.Quantity
			totalValue += s.ScrapValue
			byReason[s.Reason] += s.Quantity
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"total_quantity_scrapped": totalQty,
		"total_scrap_value":       totalValue,
		"by_reason":               byReason,
	})
}
