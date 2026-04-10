package handlers

// ============================================================================
// FBA INBOUND SHIPMENTS HANDLER
// ============================================================================
// Destination path: platform/backend/handlers/fba_inbound_handler.go
//
// Provides a full multi-step FBA inbound workflow:
//   Step 1 – Create draft shipment (saved locally in Firestore)
//   Step 2 – Plan shipment  → calls Amazon SP-API CreateInboundShipmentPlan
//   Step 3 – Update box contents (saved locally)
//   Step 4 – Confirm shipment → calls Amazon SP-API ConfirmInboundShipment
//   Step 5 – Close / mark shipped (local status update)
//
// Credential resolution is identical to amazon_handler.go:
//   - Looks up the tenant's Amazon credentials from Firestore
//   - Decrypts them via MarketplaceService.GetFullCredentials
//   - Builds an authenticated SPAPIClient
//
// The typed SP-API methods (CreateInboundShipmentPlan, ConfirmInboundShipment)
// live in: platform/backend/marketplace/clients/amazon/sp_api_fba_vendor.go
//
// ── main.go wiring ──────────────────────────────────────────────────────────
// NewFBAInboundHandler now requires three arguments. In main.go replace:
//
//   fbaInboundHandler := handlers.NewFBAInboundHandler(firestoreRepo.GetClient())
//
// with:
//
//   fbaInboundHandler := handlers.NewFBAInboundHandler(
//       firestoreRepo.GetClient(),
//       marketplaceRepo,      // already used by amazonHandler
//       marketplaceService,   // already used by amazonHandler
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

type FBAInboundHandler struct {
	client             *firestore.Client
	repo               *repository.MarketplaceRepository
	marketplaceService *services.MarketplaceService
}

func NewFBAInboundHandler(
	client *firestore.Client,
	repo *repository.MarketplaceRepository,
	marketplaceService *services.MarketplaceService,
) *FBAInboundHandler {
	return &FBAInboundHandler{
		client:             client,
		repo:               repo,
		marketplaceService: marketplaceService,
	}
}

// ─── Data models ──────────────────────────────────────────────────────────────

type FBAShipmentLine struct {
	ProductID  string `firestore:"product_id"  json:"product_id"`
	SKU        string `firestore:"sku"         json:"sku"`
	Title      string `firestore:"title"       json:"title"`
	QtyPlanned int    `firestore:"qty_planned" json:"qty_planned"`
	QtyShipped int    `firestore:"qty_shipped" json:"qty_shipped"`
	FNSKU      string `firestore:"fnsku"       json:"fnsku"`
	ASIN       string `firestore:"asin"        json:"asin"`
}

type FBABoxItem struct {
	SKU      string `firestore:"sku"      json:"sku"`
	Quantity int    `firestore:"quantity" json:"quantity"`
	FNSKU    string `firestore:"fnsku"    json:"fnsku"`
}

type FBABox struct {
	BoxNumber int          `firestore:"box_number" json:"box_number"`
	Items     []FBABoxItem `firestore:"items"      json:"items"`
	Weight    float64      `firestore:"weight"     json:"weight"`
}

type FBAShipment struct {
	ShipmentID       string            `firestore:"shipment_id"        json:"shipment_id"`
	TenantID         string            `firestore:"tenant_id"          json:"tenant_id"`
	CredentialID     string            `firestore:"credential_id"      json:"credential_id"`
	Name             string            `firestore:"name"               json:"name"`
	AmazonShipmentID string            `firestore:"amazon_shipment_id" json:"amazon_shipment_id"`
	DestinationFC    string            `firestore:"destination_fc"     json:"destination_fc"`
	Status           string            `firestore:"status"             json:"status"` // draft/planned/shipped/closed
	Lines            []FBAShipmentLine `firestore:"lines"              json:"lines"`
	BoxContents      []FBABox          `firestore:"box_contents"       json:"box_contents"`
	LabelType        string            `firestore:"label_type"         json:"label_type"` // FNSKU or barcode
	CreatedAt        time.Time         `firestore:"created_at"         json:"created_at"`
	UpdatedAt        time.Time         `firestore:"updated_at"         json:"updated_at"`
}

// ─── Firestore collection helper ──────────────────────────────────────────────

func (h *FBAInboundHandler) col(tenantID string) *firestore.CollectionRef {
	return h.client.Collection("tenants").Doc(tenantID).Collection("fba_shipments")
}

// ─── Credential resolution ────────────────────────────────────────────────────
// Identical pattern to amazon_handler.go getAmazonClient.

func (h *FBAInboundHandler) getAmazonClient(c *gin.Context, credentialID string) (*amazonClient.SPAPIClient, string, error) {
	tenantID := tenantIDFromCtx(c)

	if credentialID == "" {
		creds, err := h.repo.ListCredentials(c.Request.Context(), tenantID)
		if err != nil {
			return nil, "", fmt.Errorf("list credentials: %w", err)
		}
		for _, cred := range creds {
			if cred.Channel == "amazon" && cred.Active {
				credentialID = cred.CredentialID
				break
			}
		}
		if credentialID == "" {
			return nil, "", fmt.Errorf("no Amazon credential found — please connect an Amazon account first")
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
		SellerID:           merged["seller_id"],
	}
	if config.MarketplaceID == "" {
		config.MarketplaceID = "A1F83G8C2ARO7P" // Amazon UK
	}
	if config.Region == "" {
		config.Region = "eu-west-1"
	}
	if config.LWAClientID == "" || config.LWAClientSecret == "" || config.RefreshToken == "" {
		return nil, "", fmt.Errorf("incomplete Amazon credentials — please reconnect your Amazon account")
	}

	client, err := amazonClient.NewSPAPIClient(c.Request.Context(), config)
	if err != nil {
		return nil, "", fmt.Errorf("create SP-API client: %w", err)
	}
	return client, credentialID, nil
}

// ─── Endpoint handlers ────────────────────────────────────────────────────────

// POST /api/v1/fba/shipments
func (h *FBAInboundHandler) CreateShipment(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	var req struct {
		Name         string `json:"name"`
		CredentialID string `json:"credential_id"`
		LabelType    string `json:"label_type"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	now := time.Now()
	s := FBAShipment{
		ShipmentID:   uuid.New().String(),
		TenantID:     tenantID,
		CredentialID: req.CredentialID,
		Name:         req.Name,
		Status:       "draft",
		LabelType:    req.LabelType,
		Lines:        []FBAShipmentLine{},
		BoxContents:  []FBABox{},
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if s.LabelType == "" {
		s.LabelType = "FNSKU"
	}
	if _, err := h.col(tenantID).Doc(s.ShipmentID).Set(c.Request.Context(), s); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"shipment": s})
}

// GET /api/v1/fba/shipments
func (h *FBAInboundHandler) ListShipments(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	iter := h.col(tenantID).OrderBy("created_at", firestore.Desc).Limit(100).Documents(c.Request.Context())
	var shipments []FBAShipment
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		var s FBAShipment
		if err := doc.DataTo(&s); err == nil {
			shipments = append(shipments, s)
		}
	}
	if shipments == nil {
		shipments = []FBAShipment{}
	}
	c.JSON(http.StatusOK, gin.H{"shipments": shipments, "total": len(shipments)})
}

// GET /api/v1/fba/shipments/:id
func (h *FBAInboundHandler) GetShipment(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	doc, err := h.col(tenantID).Doc(c.Param("id")).Get(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Shipment not found"})
		return
	}
	var s FBAShipment
	doc.DataTo(&s)
	c.JSON(http.StatusOK, gin.H{"shipment": s})
}

// PUT /api/v1/fba/shipments/:id
// Updates local fields (lines, box contents, name, label type).
func (h *FBAInboundHandler) UpdateShipment(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	var req FBAShipment
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.UpdatedAt = time.Now()
	if _, err := h.col(tenantID).Doc(c.Param("id")).Set(c.Request.Context(), req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"shipment": req})
}

// POST /api/v1/fba/shipments/:id/plan
// Calls Amazon SP-API CreateInboundShipmentPlan.
// Amazon returns a shipment ID and destination fulfilment centre (e.g. "BHX4").
func (h *FBAInboundHandler) PlanShipment(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	doc, err := h.col(tenantID).Doc(c.Param("id")).Get(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Shipment not found"})
		return
	}
	var shipment FBAShipment
	doc.DataTo(&shipment)

	if len(shipment.Lines) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Shipment has no product lines — add products first"})
		return
	}

	client, credentialID, err := h.getAmazonClient(c, shipment.CredentialID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if shipment.CredentialID == "" {
		shipment.CredentialID = credentialID
	}

	// Optional ship-from address in request body
	var planReq struct {
		ShipFromName        string `json:"ship_from_name"`
		ShipFromAddress1    string `json:"ship_from_address1"`
		ShipFromCity        string `json:"ship_from_city"`
		ShipFromPostalCode  string `json:"ship_from_postal_code"`
		ShipFromCountryCode string `json:"ship_from_country_code"`
	}
	c.ShouldBindJSON(&planReq)
	if planReq.ShipFromName == "" {
		planReq.ShipFromName = "Warehouse"
	}
	if planReq.ShipFromCountryCode == "" {
		planReq.ShipFromCountryCode = "GB"
	}

	labelPrepPref := "SELLER_LABEL"
	if shipment.LabelType == "FNSKU" {
		labelPrepPref = "AMAZON_LABEL_ONLY"
	}

	// Build the typed plan request
	planItems := make([]amazonClient.FBAInboundShipmentItem, 0, len(shipment.Lines))
	for _, line := range shipment.Lines {
		planItems = append(planItems, amazonClient.FBAInboundShipmentItem{
			SellerSKU:       line.SKU,
			ASIN:            line.ASIN,
			Condition:       "NewItem",
			QuantityShipped: line.QtyPlanned,
		})
	}

	planRequest := amazonClient.FBAInboundPlanRequest{
		ShipFromAddress: amazonClient.FBAAddress{
			Name:         planReq.ShipFromName,
			AddressLine1: planReq.ShipFromAddress1,
			City:         planReq.ShipFromCity,
			PostalCode:   planReq.ShipFromPostalCode,
			CountryCode:  planReq.ShipFromCountryCode,
		},
		ShipToCountryCode:               "GB",
		LabelPrepPreference:             labelPrepPref,
		InboundShipmentPlanRequestItems: planItems,
	}

	log.Printf("[FBAInbound] Calling Amazon CreateInboundShipmentPlan tenant=%s items=%d", tenantID, len(planItems))

	planResp, apiErr := client.CreateInboundShipmentPlan(c.Request.Context(), planRequest)
	if apiErr != nil {
		log.Printf("[FBAInbound] CreateInboundShipmentPlan error: %v", apiErr)
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("Amazon API error: %v", apiErr)})
		return
	}

	// Amazon may split items across multiple fulfilment centres.
	// We use the first plan; if multiple plans are returned the frontend
	// can display them and the user can create separate shipments for the rest.
	if len(planResp.InboundShipmentPlans) > 0 {
		plan := planResp.InboundShipmentPlans[0]
		shipment.AmazonShipmentID = plan.ShipmentID
		shipment.DestinationFC = plan.DestinationFulfillmentCenterId
		shipment.Status = "planned"
		log.Printf("[FBAInbound] Plan received: AmazonShipmentID=%s DestFC=%s",
			shipment.AmazonShipmentID, shipment.DestinationFC)
	}

	shipment.UpdatedAt = time.Now()
	h.col(tenantID).Doc(shipment.ShipmentID).Set(c.Request.Context(), shipment)

	c.JSON(http.StatusOK, gin.H{
		"shipment":     shipment,
		"amazon_plans": planResp.InboundShipmentPlans,
	})
}

// POST /api/v1/fba/shipments/:id/confirm
// Calls Amazon SP-API ConfirmInboundShipment.
func (h *FBAInboundHandler) ConfirmShipment(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	doc, err := h.col(tenantID).Doc(c.Param("id")).Get(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Shipment not found"})
		return
	}
	var shipment FBAShipment
	doc.DataTo(&shipment)

	if shipment.AmazonShipmentID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Shipment must be planned first — no Amazon shipment ID"})
		return
	}

	client, _, err := h.getAmazonClient(c, shipment.CredentialID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[FBAInbound] Confirming Amazon shipment %s", shipment.AmazonShipmentID)
	if err := client.ConfirmInboundShipment(c.Request.Context(), shipment.AmazonShipmentID); err != nil {
		log.Printf("[FBAInbound] ConfirmInboundShipment error: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("Amazon confirm error: %v", err)})
		return
	}

	shipment.Status = "shipped"
	shipment.UpdatedAt = time.Now()
	h.col(tenantID).Doc(shipment.ShipmentID).Set(c.Request.Context(), shipment)
	c.JSON(http.StatusOK, gin.H{"shipment": shipment})
}

// POST /api/v1/fba/shipments/:id/close
func (h *FBAInboundHandler) CloseShipment(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	doc, err := h.col(tenantID).Doc(c.Param("id")).Get(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Shipment not found"})
		return
	}
	var shipment FBAShipment
	doc.DataTo(&shipment)
	shipment.Status = "closed"
	shipment.UpdatedAt = time.Now()
	h.col(tenantID).Doc(shipment.ShipmentID).Set(c.Request.Context(), shipment)
	c.JSON(http.StatusOK, gin.H{"shipment": shipment})
}
