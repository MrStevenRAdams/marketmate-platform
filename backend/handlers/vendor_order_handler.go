package handlers

// ============================================================================
// AMAZON VENDOR CENTRAL ORDER HANDLER
// ============================================================================
// Destination path: platform/backend/handlers/vendor_order_handler.go
//
// Manages Amazon Vendor Central purchase orders — POs where Amazon buys stock
// FROM you. These are separate from your normal Seller Central orders.
//
// The Sync endpoint calls the real Amazon Vendor Orders SP-API using:
//   client.GetVendorOrders()        — defined in sp_api_fba_vendor.go
//   client.AcknowledgeVendorOrder() — defined in sp_api_fba_vendor.go
//
// Credentials: The user must add an "amazon_vendor" connection in Marketplace
// Connections. This uses the same LWA tokens as Seller Central but with
// Vendor Central scope. The handler auto-detects this credential type.
//
// ── main.go wiring ──────────────────────────────────────────────────────────
// Replace:
//   vendorOrderHandler := handlers.NewVendorOrderHandler(firestoreRepo.GetClient())
// With:
//   vendorOrderHandler := handlers.NewVendorOrderHandler(
//       firestoreRepo.GetClient(),
//       marketplaceRepo,
//       marketplaceService,
//   )
// ============================================================================

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"
	amazonClient "module-a/marketplace/clients/amazon"
	"module-a/repository"
	"module-a/services"
)

// ─── Struct & Constructor ─────────────────────────────────────────────────────

type VendorOrderHandler struct {
	client             *firestore.Client
	repo               *repository.MarketplaceRepository
	marketplaceService *services.MarketplaceService
}

func NewVendorOrderHandler(
	client *firestore.Client,
	repo *repository.MarketplaceRepository,
	marketplaceService *services.MarketplaceService,
) *VendorOrderHandler {
	return &VendorOrderHandler{
		client:             client,
		repo:               repo,
		marketplaceService: marketplaceService,
	}
}

// ─── Data model ───────────────────────────────────────────────────────────────

type VendorOrderLine struct {
	ItemSequenceNumber string `firestore:"item_sequence_number" json:"item_sequence_number"`
	ASIN               string `firestore:"asin"                 json:"asin"`
	VendorSKU          string `firestore:"vendor_sku"           json:"vendor_sku"`
	QtyOrdered         int    `firestore:"qty_ordered"          json:"qty_ordered"`
	UnitOfMeasure      string `firestore:"unit_of_measure"      json:"unit_of_measure"`
	CurrencyCode       string `firestore:"currency_code"        json:"currency_code"`
	NetCostAmount      string `firestore:"net_cost_amount"      json:"net_cost_amount"`
}

type VendorOrder struct {
	VendorOrderID       string           `firestore:"vendor_order_id"       json:"vendor_order_id"`
	TenantID            string           `firestore:"tenant_id"             json:"tenant_id"`
	CredentialID        string           `firestore:"credential_id"         json:"credential_id"`
	AmazonPONumber      string           `firestore:"amazon_po_number"      json:"amazon_po_number"`
	Status              string           `firestore:"status"                json:"status"` // new/accepted/declined/closed
	OrderDate           time.Time        `firestore:"order_date"            json:"order_date"`
	ShipToPartyID       string           `firestore:"ship_to_party_id"      json:"ship_to_party_id"`
	ShipToName          string           `firestore:"ship_to_name"          json:"ship_to_name"`
	ShipToAddress       string           `firestore:"ship_to_address"       json:"ship_to_address"`
	ShipWindowStart     *time.Time       `firestore:"ship_window_start"     json:"ship_window_start,omitempty"`
	ShipWindowEnd       *time.Time       `firestore:"ship_window_end"       json:"ship_window_end,omitempty"`
	DeliveryWindowStart *time.Time       `firestore:"delivery_window_start" json:"delivery_window_start,omitempty"`
	DeliveryWindowEnd   *time.Time       `firestore:"delivery_window_end"   json:"delivery_window_end,omitempty"`
	Lines               []VendorOrderLine `firestore:"lines"                json:"lines"`
	DeclineReason       string           `firestore:"decline_reason"        json:"decline_reason,omitempty"`
	Notes               string           `firestore:"notes"                 json:"notes,omitempty"`
	CreatedAt           time.Time        `firestore:"created_at"            json:"created_at"`
	UpdatedAt           time.Time        `firestore:"updated_at"            json:"updated_at"`
}

// ─── Firestore helper ──────────────────────────────────────────────────────────

func (h *VendorOrderHandler) col(tenantID string) *firestore.CollectionRef {
	return h.client.Collection("tenants").Doc(tenantID).Collection("vendor_orders")
}

// ─── Credential resolution ────────────────────────────────────────────────────
// Looks for an "amazon_vendor" credential. Falls back to a plain "amazon"
// credential if no vendor-specific one exists (some accounts use the same tokens).

func (h *VendorOrderHandler) getVendorClient(c *gin.Context, credentialID string) (*amazonClient.SPAPIClient, string, error) {
	tenantID := tenantIDFromCtx(c)

	if credentialID == "" {
		creds, err := h.repo.ListCredentials(c.Request.Context(), tenantID)
		if err != nil {
			return nil, "", fmt.Errorf("list credentials: %w", err)
		}
		// Prefer amazon_vendor, fall back to amazon
		for _, cred := range creds {
			if cred.Channel == "amazon_vendor" && cred.Active {
				credentialID = cred.CredentialID
				break
			}
		}
		if credentialID == "" {
			for _, cred := range creds {
				if cred.Channel == "amazon" && cred.Active {
					credentialID = cred.CredentialID
					break
				}
			}
		}
		if credentialID == "" {
			return nil, "", fmt.Errorf("no Amazon Vendor credential found — add one in Marketplace Connections")
		}
	}

	cred, err := h.repo.GetCredential(c.Request.Context(), tenantID, credentialID)
	if err != nil {
		return nil, "", fmt.Errorf("get credential: %w", err)
	}

	merged, err := h.marketplaceService.GetFullCredentials(c.Request.Context(), cred)
	if err != nil {
		return nil, "", fmt.Errorf("decrypt credentials: %w", err)
	}

	config := &amazonClient.SPAPIConfig{
		LWAClientID:        merged["lwa_client_id"],
		LWAClientSecret:    merged["lwa_client_secret"],
		RefreshToken:       merged["refresh_token"],
		AWSAccessKeyID:     merged["aws_access_key_id"],
		AWSSecretAccessKey: merged["aws_secret_access_key"],
		MarketplaceID:      merged["marketplace_id"],
		Region:             merged["region"],
		SellerID:           merged["vendor_id"], // vendor_id used for Vendor Central
	}
	if config.MarketplaceID == "" {
		config.MarketplaceID = "A1F83G8C2ARO7P"
	}
	if config.Region == "" {
		config.Region = "eu-west-1"
	}
	if config.LWAClientID == "" || config.LWAClientSecret == "" || config.RefreshToken == "" {
		return nil, "", fmt.Errorf("incomplete Amazon Vendor credentials (need lwa_client_id, lwa_client_secret, refresh_token)")
	}

	client, err := amazonClient.NewSPAPIClient(c.Request.Context(), config)
	if err != nil {
		return nil, "", fmt.Errorf("create SP-API client: %w", err)
	}
	return client, credentialID, nil
}

// ─── Endpoint handlers ─────────────────────────────────────────────────────────

// POST /api/v1/vendor-orders/sync
// Calls Amazon Vendor Orders API to fetch new POs and saves them to Firestore.
// De-duplicates by amazon_po_number so repeated syncs are safe.
func (h *VendorOrderHandler) Sync(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	credentialID := c.Query("credential_id")

	client, resolvedCredID, err := h.getVendorClient(c, credentialID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Fetch POs created in the last 90 days
	since := time.Now().AddDate(0, -3, 0)
	log.Printf("[VendorOrders] Syncing POs for tenant=%s since=%s", tenantID, since.Format(time.RFC3339))

	poResp, err := client.GetVendorOrders(c.Request.Context(), since, "")
	if err != nil {
		log.Printf("[VendorOrders] GetVendorOrders API error: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("Amazon Vendor API error: %v", err)})
		return
	}

	imported := 0
	skipped := 0

	for _, amzPO := range poResp.Orders {
		// De-duplicate: skip if we already have this PO number
		existing, _ := h.col(tenantID).Where("amazon_po_number", "==", amzPO.PurchaseOrderNumber).
			Limit(1).Documents(c.Request.Context()).GetAll()
		if len(existing) > 0 {
			skipped++
			continue
		}

		// Map line items from the SP-API response type
		lines := make([]VendorOrderLine, 0, len(amzPO.OrderDetails.Items))
		for _, item := range amzPO.OrderDetails.Items {
			line := VendorOrderLine{
				ItemSequenceNumber: item.ItemSequenceNumber,
				ASIN:               item.BuyerProductIdentifier.ASIN,
				VendorSKU:          item.VendorProductIdentifier.VendorSKU,
				QtyOrdered:         item.OrderedQuantity.Amount,
				UnitOfMeasure:      item.OrderedQuantity.UnitOfMeasure,
			}
			if item.NetCost != nil {
				line.CurrencyCode  = item.NetCost.CurrencyCode
				line.NetCostAmount = item.NetCost.Amount
			}
			lines = append(lines, line)
		}

		// Parse order date
		orderDate := time.Now()
		if t, err := time.Parse(time.RFC3339, amzPO.OrderDetails.PurchaseOrderDate); err == nil {
			orderDate = t
		}

		// Parse delivery window dates
		var deliveryStart, deliveryEnd *time.Time
		if ws, err := time.Parse(time.RFC3339, amzPO.OrderDetails.DeliveryWindow.StartDateTime); err == nil {
			deliveryStart = &ws
		}
		if we, err := time.Parse(time.RFC3339, amzPO.OrderDetails.DeliveryWindow.EndDateTime); err == nil {
			deliveryEnd = &we
		}

		// Build ship-to address string
		addr := amzPO.OrderDetails.ShipToParty.Address
		shipToAddress := fmt.Sprintf("%s, %s %s %s",
			addr.AddressLine1, addr.City, addr.PostalCode, addr.CountryCode)

		now := time.Now()
		order := VendorOrder{
			VendorOrderID:       uuid.New().String(),
			TenantID:            tenantID,
			CredentialID:        resolvedCredID,
			AmazonPONumber:      amzPO.PurchaseOrderNumber,
			Status:              "new",
			OrderDate:           orderDate,
			ShipToPartyID:       amzPO.OrderDetails.ShipToParty.PartyId,
			ShipToName:          addr.Name,
			ShipToAddress:       shipToAddress,
			DeliveryWindowStart: deliveryStart,
			DeliveryWindowEnd:   deliveryEnd,
			Lines:               lines,
			CreatedAt:           now,
			UpdatedAt:           now,
		}

		if _, saveErr := h.col(tenantID).Doc(order.VendorOrderID).Set(c.Request.Context(), order); saveErr != nil {
			log.Printf("[VendorOrders] Failed to save PO %s: %v", amzPO.PurchaseOrderNumber, saveErr)
			continue
		}
		imported++
		log.Printf("[VendorOrders] Imported PO %s (lines=%d)", amzPO.PurchaseOrderNumber, len(lines))
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":       true,
		"imported": imported,
		"skipped":  skipped,
		"total":    len(poResp.Orders),
	})
}

// GET /api/v1/vendor-orders
func (h *VendorOrderHandler) List(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	statusFilter := c.Query("status")

	var iter *firestore.DocumentIterator
	if statusFilter != "" {
		iter = h.col(tenantID).Where("status", "==", statusFilter).
			OrderBy("order_date", firestore.Desc).Limit(200).Documents(c.Request.Context())
	} else {
		iter = h.col(tenantID).OrderBy("order_date", firestore.Desc).Limit(200).Documents(c.Request.Context())
	}

	var orders []VendorOrder
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		var o VendorOrder
		if err := doc.DataTo(&o); err == nil {
			orders = append(orders, o)
		}
	}
	if orders == nil {
		orders = []VendorOrder{}
	}
	c.JSON(http.StatusOK, gin.H{"orders": orders, "total": len(orders)})
}

// GET /api/v1/vendor-orders/:id
func (h *VendorOrderHandler) Get(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	doc, err := h.col(tenantID).Doc(c.Param("id")).Get(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Vendor order not found"})
		return
	}
	var o VendorOrder
	doc.DataTo(&o)
	c.JSON(http.StatusOK, gin.H{"order": o})
}

// POST /api/v1/vendor-orders (manual create, for testing without live sync)
func (h *VendorOrderHandler) Create(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	var req VendorOrder
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	now := time.Now()
	req.VendorOrderID = uuid.New().String()
	req.TenantID = tenantID
	req.Status = "new"
	req.CreatedAt = now
	req.UpdatedAt = now
	if req.Lines == nil {
		req.Lines = []VendorOrderLine{}
	}
	if _, err := h.col(tenantID).Doc(req.VendorOrderID).Set(c.Request.Context(), req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"order": req})
}

// POST /api/v1/vendor-orders/:id/accept
// Sends an "Accepted" acknowledgement to Amazon Vendor Central.
func (h *VendorOrderHandler) Accept(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	doc, err := h.col(tenantID).Doc(c.Param("id")).Get(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Vendor order not found"})
		return
	}
	var order VendorOrder
	doc.DataTo(&order)

	if order.Status == "accepted" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Order has already been accepted"})
		return
	}

	client, _, err := h.getVendorClient(c, order.CredentialID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Build the SP-API VendorOrderItem slice needed by AcknowledgeVendorOrder
	items := make([]amazonClient.VendorOrderItem, 0, len(order.Lines))
	for _, line := range order.Lines {
		item := amazonClient.VendorOrderItem{
			ItemSequenceNumber: line.ItemSequenceNumber,
		}
		item.BuyerProductIdentifier.ASIN = line.ASIN
		item.VendorProductIdentifier.VendorSKU = line.VendorSKU
		item.OrderedQuantity.Amount = line.QtyOrdered
		item.OrderedQuantity.UnitOfMeasure = line.UnitOfMeasure
		if item.OrderedQuantity.UnitOfMeasure == "" {
			item.OrderedQuantity.UnitOfMeasure = "Each"
		}
		items = append(items, item)
	}

	log.Printf("[VendorOrders] Accepting PO %s for tenant=%s", order.AmazonPONumber, tenantID)
	if err := client.AcknowledgeVendorOrder(c.Request.Context(), order.AmazonPONumber, items, "Accepted", ""); err != nil {
		log.Printf("[VendorOrders] Accept error: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("Amazon Vendor ACK error: %v", err)})
		return
	}

	order.Status = "accepted"
	order.UpdatedAt = time.Now()
	h.col(tenantID).Doc(order.VendorOrderID).Set(c.Request.Context(), order)
	c.JSON(http.StatusOK, gin.H{"order": order})
}

// POST /api/v1/vendor-orders/:id/decline
// Sends a "Rejected" acknowledgement to Amazon Vendor Central.
func (h *VendorOrderHandler) Decline(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	doc, err := h.col(tenantID).Doc(c.Param("id")).Get(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Vendor order not found"})
		return
	}
	var order VendorOrder
	doc.DataTo(&order)

	var req struct {
		Reason string `json:"reason"`
	}
	c.ShouldBindJSON(&req)
	if req.Reason == "" {
		req.Reason = "Unable to fulfil"
	}

	if order.Status == "declined" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Order has already been declined"})
		return
	}

	client, _, err := h.getVendorClient(c, order.CredentialID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Build SP-API item list for rejection (zero qty acknowledged)
	items := make([]amazonClient.VendorOrderItem, 0, len(order.Lines))
	for _, line := range order.Lines {
		item := amazonClient.VendorOrderItem{
			ItemSequenceNumber: line.ItemSequenceNumber,
		}
		item.BuyerProductIdentifier.ASIN = line.ASIN
		item.VendorProductIdentifier.VendorSKU = line.VendorSKU
		item.OrderedQuantity.Amount = 0 // zero = rejected
		item.OrderedQuantity.UnitOfMeasure = line.UnitOfMeasure
		if item.OrderedQuantity.UnitOfMeasure == "" {
			item.OrderedQuantity.UnitOfMeasure = "Each"
		}
		items = append(items, item)
	}

	log.Printf("[VendorOrders] Declining PO %s for tenant=%s reason=%q", order.AmazonPONumber, tenantID, req.Reason)
	if err := client.AcknowledgeVendorOrder(c.Request.Context(), order.AmazonPONumber, items, "Rejected", req.Reason); err != nil {
		log.Printf("[VendorOrders] Decline error: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("Amazon Vendor ACK error: %v", err)})
		return
	}

	order.Status = "declined"
	order.DeclineReason = req.Reason
	order.UpdatedAt = time.Now()
	h.col(tenantID).Doc(order.VendorOrderID).Set(c.Request.Context(), order)
	c.JSON(http.StatusOK, gin.H{"order": order})
}
