package handlers

// ============================================================================
// ORDER SEED HANDLER — Dev Tools
// ============================================================================
// POST /api/v1/dev/orders/seed
//
// Generates realistic dummy orders directly in Firestore, pulling real
// products from the PIM so that HS codes, weights, SKUs and descriptions
// are accurate for customs label testing.
//
// Request body:
//   {
//     "count":            5,          // number of orders to create (1–50)
//     "lines_per_order":  2,          // line items per order (1–10)
//     "destination":      "domestic", // "domestic" | "international" | "mixed"
//     "country":          "DE",       // override country (international only)
//     "postcode":         "SW1A 1AA", // override postcode (custom testing)
//     "channel":          "shopify",  // override channel (default: mixed)
//     "status":           "processing", // override status (default: processing)
//     "tag":              "LABEL-TEST" // tag all generated orders
//   }
// ============================================================================

import (
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"module-a/models"
	"module-a/repository"
	"module-a/services"
)

type OrderSeedHandler struct {
	orderService *services.OrderService
	productRepo  *repository.FirestoreRepository
}

func NewOrderSeedHandler(orderService *services.OrderService, productRepo *repository.FirestoreRepository) *OrderSeedHandler {
	return &OrderSeedHandler{
		orderService: orderService,
		productRepo:  productRepo,
	}
}

// ── Seed request / response ────────────────────────────────────────────────

type OrderSeedRequest struct {
	Count          int    `json:"count"`
	LinesPerOrder  int    `json:"lines_per_order"`
	Destination    string `json:"destination"`    // domestic | international | mixed
	Country        string `json:"country"`         // ISO 2-letter override
	Postcode       string `json:"postcode"`        // postcode override
	Channel        string `json:"channel"`         // channel override
	Status         string `json:"status"`          // status override
	Tag            string `json:"tag"`             // tag to apply to all orders
	ProductIDs     []string `json:"product_ids"` // optional specific product IDs to use
}

type OrderSeedResponse struct {
	OK       bool     `json:"ok"`
	Created  int      `json:"created"`
	OrderIDs []string `json:"order_ids"`
	Errors   []string `json:"errors,omitempty"`
}

// ── Address pool ───────────────────────────────────────────────────────────

type seedAddress struct {
	Name         string
	Company      string
	Line1        string
	Line2        string
	City         string
	State        string
	PostalCode   string
	Country      string
	Phone        string
	Email        string
	IsInternational bool
}

var domesticAddresses = []seedAddress{
	{Name: "James Wilson", Line1: "14 Victoria Road", City: "Leeds", State: "West Yorkshire", PostalCode: "LS1 5AE", Country: "GB", Phone: "07700900001", Email: "james.wilson@example.com"},
	{Name: "Sophie Turner", Line1: "7 Baker Street", Line2: "Flat 3", City: "London", State: "", PostalCode: "W1U 3BW", Country: "GB", Phone: "07700900002", Email: "sophie.turner@example.com"},
	{Name: "David Chen", Line1: "22 Princes Street", City: "Edinburgh", State: "Scotland", PostalCode: "EH2 2AN", Country: "GB", Phone: "07700900003", Email: "david.chen@example.com"},
	{Name: "Emma Thompson", Line1: "5 Broad Street", City: "Birmingham", State: "West Midlands", PostalCode: "B1 2HS", Country: "GB", Phone: "07700900004", Email: "emma.thompson@example.com"},
	{Name: "Oliver Brown", Line1: "33 Park Lane", City: "Manchester", State: "Greater Manchester", PostalCode: "M1 4JX", Country: "GB", Phone: "07700900005", Email: "oliver.brown@example.com"},
	{Name: "Aoife Murphy", Line1: "12 O'Connell Street", City: "Belfast", State: "Northern Ireland", PostalCode: "BT1 5EA", Country: "GB", Phone: "07700900006", Email: "aoife.murphy@example.com"},
	{Name: "Gareth Evans", Line1: "8 Cardiff Road", City: "Cardiff", State: "Wales", PostalCode: "CF10 2EU", Country: "GB", Phone: "07700900007", Email: "gareth.evans@example.com"},
	{Name: "Lucy Patel", Line1: "91 Colmore Row", City: "Birmingham", State: "West Midlands", PostalCode: "B3 2BB", Country: "GB", Phone: "07700900008", Email: "lucy.patel@example.com"},
	{Name: "Mohammed Ali", Line1: "55 Crown Street", City: "Aberdeen", State: "Scotland", PostalCode: "AB11 6EX", Country: "GB", Phone: "07700900009", Email: "m.ali@example.com"},
	{Name: "Hannah Clarke", Line1: "18 Deansgate", City: "Manchester", State: "Greater Manchester", PostalCode: "M3 4LY", Country: "GB", Phone: "07700900010", Email: "hannah.clarke@example.com"},
	// Scottish Highlands — some couriers charge surcharges
	{Name: "Alasdair MacLeod", Line1: "3 High Street", City: "Inverness", State: "Scotland", PostalCode: "IV1 1HH", Country: "GB", Phone: "07700900011", Email: "a.macleod@example.com"},
	// Islands — many couriers don't ship or charge extra
	{Name: "Fiona MacDonald", Line1: "1 Main Street", City: "Stornoway", State: "Outer Hebrides", PostalCode: "HS1 2AA", Country: "GB", Phone: "07700900012", Email: "f.macdonald@example.com"},
	{Name: "John Stewart", Line1: "4 Lerwick Lane", City: "Lerwick", State: "Shetland", PostalCode: "ZE1 0AA", Country: "GB", Phone: "07700900013", Email: "j.stewart@example.com"},
	// Channel Islands — outside UK customs territory
	{Name: "Peter Le Brun", Line1: "6 King Street", City: "St Helier", State: "Jersey", PostalCode: "JE2 4WB", Country: "JE", Phone: "01534000001", Email: "p.lebrun@example.com"},
	// Isle of Man — same issue
	{Name: "Thomas Kelly", Line1: "2 Prospect Hill", City: "Douglas", State: "Isle of Man", PostalCode: "IM1 1EF", Country: "IM", Phone: "01624000001", Email: "t.kelly@example.com"},
	// BFPO
	{Name: "Cpl R. Jones", Company: "BFPO 801", Line1: "RAF Brize Norton", City: "Carterton", State: "Oxfordshire", PostalCode: "OX18 3LX", Country: "GB", Phone: "", Email: ""},
}

var internationalAddresses = []seedAddress{
	// EU — no customs required for shipments under thresholds but IOSS may apply
	{Name: "Hans Müller", Line1: "Hauptstrasse 12", City: "Hamburg", State: "Hamburg", PostalCode: "20095", Country: "DE", Phone: "+4915000000001", Email: "hans.muller@example.de", IsInternational: true},
	{Name: "Marie Dupont", Line1: "12 Rue de Rivoli", City: "Paris", State: "Île-de-France", PostalCode: "75001", Country: "FR", Phone: "+33600000001", Email: "marie.dupont@example.fr", IsInternational: true},
	{Name: "Carlo Rossi", Line1: "Via Roma 45", City: "Milan", State: "Lombardy", PostalCode: "20121", Country: "IT", Phone: "+39300000001", Email: "c.rossi@example.it", IsInternational: true},
	{Name: "Ana García", Line1: "Calle Mayor 8", City: "Madrid", State: "Community of Madrid", PostalCode: "28013", Country: "ES", Phone: "+34600000001", Email: "a.garcia@example.es", IsInternational: true},
	{Name: "Jan de Vries", Line1: "Keizersgracht 100", City: "Amsterdam", State: "North Holland", PostalCode: "1015 CS", Country: "NL", Phone: "+31600000001", Email: "j.devries@example.nl", IsInternational: true},
	{Name: "Lars Andersen", Line1: "Nørregade 5", City: "Copenhagen", State: "", PostalCode: "1165", Country: "DK", Phone: "+4540000001", Email: "l.andersen@example.dk", IsInternational: true},
	{Name: "Anna Kowalski", Line1: "ul. Nowy Świat 15", City: "Warsaw", State: "", PostalCode: "00-029", Country: "PL", Phone: "+48500000001", Email: "a.kowalski@example.pl", IsInternational: true},
	// Non-EU international — full customs declaration required
	{Name: "Bob Johnson", Company: "Acme Corp", Line1: "350 5th Avenue", Line2: "Suite 1500", City: "New York", State: "NY", PostalCode: "10118", Country: "US", Phone: "+12125550001", Email: "bob.johnson@acme.com", IsInternational: true},
	{Name: "Yuki Tanaka", Line1: "1-2-3 Shinjuku", City: "Tokyo", State: "Tokyo", PostalCode: "160-0022", Country: "JP", Phone: "+81901234567", Email: "y.tanaka@example.jp", IsInternational: true},
	{Name: "Chen Wei", Line1: "100 Nanjing Road", City: "Shanghai", State: "Shanghai", PostalCode: "200001", Country: "CN", Phone: "+8613900000001", Email: "c.wei@example.cn", IsInternational: true},
	{Name: "Priya Sharma", Line1: "42 MG Road", City: "Bangalore", State: "Karnataka", PostalCode: "560001", Country: "IN", Phone: "+919800000001", Email: "p.sharma@example.in", IsInternational: true},
	{Name: "Sarah Mitchell", Line1: "10 George Street", City: "Sydney", State: "NSW", PostalCode: "2000", Country: "AU", Phone: "+61400000001", Email: "s.mitchell@example.au", IsInternational: true},
	{Name: "Mohammed Al-Rashid", Line1: "King Fahd Road", City: "Riyadh", State: "", PostalCode: "12271", Country: "SA", Phone: "+966500000001", Email: "m.alrashid@example.sa", IsInternational: true},
	// Canada
	{Name: "Pierre Tremblay", Line1: "100 Rue Sainte-Catherine", City: "Montreal", State: "QC", PostalCode: "H2X 1Z3", Country: "CA", Phone: "+15140000001", Email: "p.tremblay@example.ca", IsInternational: true},
}

var channels = []string{"amazon", "ebay", "shopify", "shopline", "temu", "etsy", "woocommerce"}

// ── SeedOrders handler ─────────────────────────────────────────────────────

// SeedOrders creates dummy orders for courier label testing.
// POST /api/v1/dev/orders/seed
func (h *OrderSeedHandler) SeedOrders(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req OrderSeedRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	// Validate and clamp params
	if req.Count < 1 {
		req.Count = 1
	}
	if req.Count > 50 {
		req.Count = 50
	}
	if req.LinesPerOrder < 1 {
		req.LinesPerOrder = 1
	}
	if req.LinesPerOrder > 10 {
		req.LinesPerOrder = 10
	}
	if req.Destination == "" {
		req.Destination = "domestic"
	}
	if req.Status == "" {
		req.Status = "processing"
	}
	if req.Tag == "" {
		req.Tag = "LABEL-TEST"
	}

	ctx := c.Request.Context()

	// ── Step 1: Load products from PIM ──────────────────────────────────────
	products, _, err := h.productRepo.ListProducts(ctx, tenantID, map[string]interface{}{"status": "active"}, 100, 0)
	if err != nil || len(products) == 0 {
		// Try without status filter
		products, _, err = h.productRepo.ListProducts(ctx, tenantID, map[string]interface{}{}, 100, 0)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"ok": false, "error": fmt.Sprintf("failed to load products: %v", err)})
			return
		}
	}

	// Filter to specific product IDs if provided
	if len(req.ProductIDs) > 0 {
		filtered := make([]models.Product, 0)
		pidSet := map[string]bool{}
		for _, pid := range req.ProductIDs {
			pidSet[pid] = true
		}
		for _, p := range products {
			if pidSet[p.ProductID] {
				filtered = append(filtered, p)
			}
		}
		if len(filtered) > 0 {
			products = filtered
		}
	}

	if len(products) == 0 {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": "no products found in PIM — please create some products first"})
		return
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	var createdIDs []string
	var errs []string

	for i := 0; i < req.Count; i++ {
		// ── Pick address ────────────────────────────────────────────────────
		var addr seedAddress

		if req.Postcode != "" || req.Country != "" {
			// Custom override
			addr = seedAddress{
				Name:       randomName(rng),
				Line1:      "1 Test Street",
				City:       "Test City",
				PostalCode: req.Postcode,
				Country:    req.Country,
				Phone:      "07700900000",
				Email:      "test@example.com",
			}
			if addr.Country == "" {
				addr.Country = "GB"
			}
			addr.IsInternational = addr.Country != "GB" && addr.Country != "JE" && addr.Country != "IM"
		} else {
			switch req.Destination {
			case "international":
				addr = internationalAddresses[rng.Intn(len(internationalAddresses))]
			case "mixed":
				if rng.Intn(2) == 0 {
					addr = internationalAddresses[rng.Intn(len(internationalAddresses))]
				} else {
					addr = domesticAddresses[rng.Intn(len(domesticAddresses))]
				}
			default: // domestic
				addr = domesticAddresses[rng.Intn(len(domesticAddresses))]
			}
		}

		// ── Pick channel ────────────────────────────────────────────────────
		channel := req.Channel
		if channel == "" {
			channel = channels[rng.Intn(len(channels))]
		}

		// ── Build order ─────────────────────────────────────────────────────
		now := time.Now().UTC()
		externalID := fmt.Sprintf("TEST-%d-%04d", now.Unix(), rng.Intn(9999))
		orderID := fmt.Sprintf("seed-%s-%d-%04d", tenantID, now.UnixMilli(), rng.Intn(9999))

		totalAmount := 0.0
		var lines []models.OrderLine

		// ── Pick line items from PIM ─────────────────────────────────────────
		// Shuffle products to get random selection without repeats
		perm := rng.Perm(len(products))
		lineCount := req.LinesPerOrder
		if lineCount > len(products) {
			lineCount = len(products)
		}

		for j := 0; j < lineCount; j++ {
			prod := products[perm[j]]
			qty := rng.Intn(3) + 1

			// Extract price
			price := 9.99 // fallback
			if prod.Attributes != nil {
				if p, ok := prod.Attributes["retail_price"].(float64); ok && p > 0 {
					price = p
				}
			}

			lineTotal := price * float64(qty)
			totalAmount += lineTotal

			line := models.OrderLine{
				LineID:    fmt.Sprintf("line-%s-%d", orderID, j+1),
				SKU:       prod.SKU,
				ProductID: prod.ProductID,
				Title:     prod.Title,
				Quantity:  qty,
				UnitPrice: models.Money{Amount: price, Currency: "GBP"},
				LineTotal: models.Money{Amount: lineTotal, Currency: "GBP"},
				Status:    "pending",
			}
			lines = append(lines, line)
		}

		tags := []string{req.Tag}
		if addr.IsInternational {
			tags = append(tags, "INTERNATIONAL")
		}

		order := &models.Order{
			OrderID:         orderID,
			TenantID:        tenantID,
			Channel:         channel,
			ExternalOrderID: externalID,
			Status:          req.Status,
			PaymentStatus:   "captured",
			Customer: models.Customer{
				Name:  addr.Name,
				Email: addr.Email,
				Phone: addr.Phone,
			},
			ShippingAddress: models.Address{
				Name:         addr.Name,
				AddressLine1: addr.Line1,
				AddressLine2: addr.Line2,
				City:         addr.City,
				State:        addr.State,
				PostalCode:   addr.PostalCode,
				Country:      addr.Country,
			},
			Totals: models.OrderTotals{
				GrandTotal: models.Money{Amount: totalAmount, Currency: "GBP"},
				Subtotal:   models.Money{Amount: totalAmount, Currency: "GBP"},
			},
			Lines:      lines,
			Tags:       tags,
			OrderDate:  now.Format(time.RFC3339),
			CreatedAt:  now.Format(time.RFC3339),
			UpdatedAt:  now.Format(time.RFC3339),
			ImportedAt: now.Format(time.RFC3339),
		}

		savedID, _, err := h.orderService.CreateOrder(ctx, tenantID, order)
		if err != nil {
			log.Printf("[OrderSeed] Failed to create order %s: %v", orderID, err)
			errs = append(errs, fmt.Sprintf("order %d: %v", i+1, err))
			continue
		}

		// Save lines
		for _, line := range lines {
			lineCopy := line
			if err := h.orderService.CreateOrderLine(ctx, tenantID, savedID, &lineCopy); err != nil {
				log.Printf("[OrderSeed] Failed to save line for order %s: %v", savedID, err)
			}
		}

		createdIDs = append(createdIDs, savedID)
	}

	c.JSON(http.StatusOK, OrderSeedResponse{
		OK:       true,
		Created:  len(createdIDs),
		OrderIDs: createdIDs,
		Errors:   errs,
	})
}

// ── helpers ────────────────────────────────────────────────────────────────

var firstNames = []string{"James", "Sophie", "Oliver", "Emma", "William", "Isabella", "Liam", "Charlotte", "Noah", "Amelia", "George", "Ava", "Harry", "Mia", "Jack", "Lily", "Thomas", "Grace"}
var lastNames = []string{"Smith", "Jones", "Williams", "Taylor", "Brown", "Davies", "Evans", "Wilson", "Thomas", "Roberts", "Johnson", "Walker", "Wright", "Thompson", "White", "Martin"}

func randomName(rng *rand.Rand) string {
	return firstNames[rng.Intn(len(firstNames))] + " " + lastNames[rng.Intn(len(lastNames))]
}
