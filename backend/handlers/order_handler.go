package handlers

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"module-a/models"
	"module-a/services"
)

type OrderHandler struct {
	orderService         *services.OrderService
	amazonOrdersHandler  *AmazonOrdersHandler
	ebayOrdersHandler    *EbayOrdersHandler
	temuOrdersHandler    *TemuOrdersHandler
	tescoOrdersHandler   *TescoOrdersHandler
	tiktokOrdersHandler  *TikTokOrdersHandler
	etsyOrdersHandler    *EtsyOrdersHandler
	wooOrdersHandler     *WooCommerceOrdersHandler
	walmartOrdersHandler  *WalmartOrdersHandler
	kauflandOrdersHandler *KauflandOrdersHandler
	magentoOrdersHandler  *MagentoOrdersHandler
	bigcommerceOrdersHandler *BigCommerceOrdersHandler
	onbuyOrdersHandler       *OnBuyOrdersHandler
	// Session 4 channels
	backmarketOrdersHandler  *BackMarketOrdersHandler
	zalandoOrdersHandler     *ZalandoOrdersHandler
	bolOrdersHandler         *BolOrdersHandler
	lazadaOrdersHandler      *LazadaOrdersHandler
	// Session 19/20
	shopifyHandler          *ShopifyHandler
	shopwiredOrdersHandler  *ShopWiredOrdersHandler
}

func NewOrderHandler(
	orderService *services.OrderService,
	amazonOrdersHandler *AmazonOrdersHandler,
	ebayOrdersHandler *EbayOrdersHandler,
	temuOrdersHandler *TemuOrdersHandler,
) *OrderHandler {
	return &OrderHandler{
		orderService:        orderService,
		amazonOrdersHandler: amazonOrdersHandler,
		ebayOrdersHandler:   ebayOrdersHandler,
		temuOrdersHandler:   temuOrdersHandler,
	}
}

// SetTescoOrdersHandler injects the Tesco orders handler (called after construction in main.go)
func (h *OrderHandler) SetTescoOrdersHandler(handler *TescoOrdersHandler) {
	h.tescoOrdersHandler = handler
}

// SetTikTokOrdersHandler injects the TikTok orders handler (called after construction in main.go)
func (h *OrderHandler) SetTikTokOrdersHandler(handler *TikTokOrdersHandler) {
	h.tiktokOrdersHandler = handler
}

// SetEtsyOrdersHandler injects the Etsy orders handler (called after construction in main.go)
func (h *OrderHandler) SetEtsyOrdersHandler(handler *EtsyOrdersHandler) {
	h.etsyOrdersHandler = handler
}

// SetWooCommerceOrdersHandler injects the WooCommerce orders handler (called after construction in main.go)
func (h *OrderHandler) SetWooCommerceOrdersHandler(handler *WooCommerceOrdersHandler) {
	h.wooOrdersHandler = handler
}

// SetWalmartOrdersHandler injects the Walmart orders handler (called after construction in main.go)
func (h *OrderHandler) SetWalmartOrdersHandler(handler *WalmartOrdersHandler) {
	h.walmartOrdersHandler = handler
}

// SetKauflandOrdersHandler injects the Kaufland orders handler (called after construction in main.go)
func (h *OrderHandler) SetKauflandOrdersHandler(handler *KauflandOrdersHandler) {
	h.kauflandOrdersHandler = handler
}

// SetMagentoOrdersHandler injects the Magento orders handler (called after construction in main.go)
func (h *OrderHandler) SetMagentoOrdersHandler(handler *MagentoOrdersHandler) {
	h.magentoOrdersHandler = handler
}

// SetBigCommerceOrdersHandler injects the BigCommerce orders handler (called after construction in main.go)
func (h *OrderHandler) SetBigCommerceOrdersHandler(handler *BigCommerceOrdersHandler) {
	h.bigcommerceOrdersHandler = handler
}

// SetOnBuyOrdersHandler injects the OnBuy orders handler (called after construction in main.go)
func (h *OrderHandler) SetOnBuyOrdersHandler(handler *OnBuyOrdersHandler) {
	h.onbuyOrdersHandler = handler
}

// SetBackMarketOrdersHandler injects the Back Market orders handler
func (h *OrderHandler) SetBackMarketOrdersHandler(handler *BackMarketOrdersHandler) {
	h.backmarketOrdersHandler = handler
}

// SetZalandoOrdersHandler injects the Zalando orders handler
func (h *OrderHandler) SetZalandoOrdersHandler(handler *ZalandoOrdersHandler) {
	h.zalandoOrdersHandler = handler
}

// SetBolOrdersHandler injects the Bol.com orders handler
func (h *OrderHandler) SetBolOrdersHandler(handler *BolOrdersHandler) {
	h.bolOrdersHandler = handler
}

// SetLazadaOrdersHandler injects the Lazada orders handler
func (h *OrderHandler) SetLazadaOrdersHandler(handler *LazadaOrdersHandler) {
	h.lazadaOrdersHandler = handler
}

func (h *OrderHandler) SetShopifyHandler(handler *ShopifyHandler) {
	h.shopifyHandler = handler
}

func (h *OrderHandler) SetShopWiredOrdersHandler(handler *ShopWiredOrdersHandler) {
	h.shopwiredOrdersHandler = handler
}

// ListOrders handles GET /api/v1/orders
func (h *OrderHandler) ListOrders(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing tenant_id"})
		return
	}

	// Task 5: If free-text search, skip PII token path
	if search := c.Query("search"); search != "" {
		searchField := c.DefaultQuery("search_field", "pii_email_token")
		// Check if it's free-text (not a PII token field)
		piiFields := map[string]bool{
			"pii_email_token":    true,
			"pii_name_token":     true,
			"pii_postcode_token": true,
			"pii_phone_token":    true,
		}
		if !piiFields[searchField] || searchField == "free_text" {
			// Free-text search across order_id, external_ref, SKU
			orders, total, err := h.orderService.ListOrders(c.Request.Context(), tenantID, services.OrderListOptions{
				FreeTextSearch: search,
				Limit:          c.DefaultQuery("limit", "50"),
				Offset:         c.DefaultQuery("offset", "0"),
				SortBy:         c.DefaultQuery("sort_by", "created_at"),
				SortOrder:      c.DefaultQuery("sort_order", "desc"),
			})
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"orders": orders, "total": total, "search": search})
			return
		}
		// PII token search
		orders, err := h.orderService.SearchOrdersByPII(c.Request.Context(), tenantID, searchField, search)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"orders": orders, "total": len(orders), "search": search})
		return
	}

	// Special filter: unlinked or composite items
	if sf := c.Query("special_filter"); sf == "unlinked" || sf == "composite" {
		orders, err := h.orderService.GetOrdersBySpecialFilter(c.Request.Context(), tenantID, sf)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"orders": orders, "total": len(orders), "special_filter": sf})
		return
	}

	// Task 4: parse multi-column sort params (sort_fields=field:dir,field2:dir2)
	var sortFields []services.SortField
	if sf := c.Query("sort_fields"); sf != "" {
		for _, part := range strings.Split(sf, ",") {
			kv := strings.SplitN(part, ":", 2)
			field := kv[0]
			dir := "desc"
			if len(kv) == 2 {
				dir = kv[1]
			}
			sortFields = append(sortFields, services.SortField{Field: field, Direction: dir})
		}
	}

	orders, total, err := h.orderService.ListOrders(c.Request.Context(), tenantID, services.OrderListOptions{
		Status:             c.Query("status"),
		Channel:            c.Query("channel"),
		Limit:              c.DefaultQuery("limit", "50"),
		Offset:             c.DefaultQuery("offset", "0"),
		SortBy:             c.DefaultQuery("sort_by", "created_at"),
		SortOrder:          c.DefaultQuery("sort_order", "desc"),
		SortFields:         sortFields,
		// Task 7: date range filters
		ReceivedDateFrom:   c.Query("received_from"),
		ReceivedDateTo:     c.Query("received_to"),
		DespatchByDateFrom: c.Query("despatch_from"),
		DespatchByDateTo:   c.Query("despatch_to"),
		DeliveryDateFrom:   c.Query("delivery_from"),
		DeliveryDateTo:     c.Query("delivery_to"),
		// Task 8: shipping/destination filters
		ShippingService:    c.Query("shipping_service"),
		Carrier:            c.Query("carrier"),
		DestinationCountry: c.Query("destination_country"),
		PackagingType:      c.Query("packaging_type"),
		// Fix 1B: folder filter
		FolderID:           c.Query("folder_id"),
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"orders": orders,
		"total":  total,
		"limit":  c.DefaultQuery("limit", "50"),
		"offset": c.DefaultQuery("offset", "0"),
	})
}

// GetOrder handles GET /api/v1/orders/:id
func (h *OrderHandler) GetOrder(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	orderID := c.Param("id")

	if tenantID == "" || orderID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing required parameters"})
		return
	}

	order, err := h.orderService.GetOrder(c.Request.Context(), tenantID, orderID)
	if err != nil {
		if err.Error() == "order not found" {
			c.JSON(http.StatusNotFound, gin.H{"error": "Order not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, order)
}

// GetOrderLines handles GET /api/v1/orders/:id/lines
func (h *OrderHandler) GetOrderLines(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	orderID := c.Param("id")

	if tenantID == "" || orderID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing required parameters"})
		return
	}

	lines, err := h.orderService.GetOrderLines(c.Request.Context(), tenantID, orderID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"lines": lines,
	})
}

// UpdateOrderStatus handles PATCH /api/v1/orders/:id/status
func (h *OrderHandler) UpdateOrderStatus(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	orderID := c.Param("id")

	var req struct {
		Status    string `json:"status" binding:"required"`
		SubStatus string `json:"sub_status"`
		Notes     string `json:"notes"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	err := h.orderService.UpdateOrderStatus(c.Request.Context(), tenantID, orderID, req.Status, req.SubStatus, req.Notes)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Order status updated successfully"})
}

// ImportOrders handles POST /api/v1/orders/import
func (h *OrderHandler) ImportOrders(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req struct {
		Channel          string `json:"channel" binding:"required"`
		ChannelAccountID string `json:"channel_account_id" binding:"required"`
		DateFrom         string `json:"date_from"`
		DateTo           string `json:"date_to"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Create the import job
	jobID, err := h.orderService.StartOrderImport(c.Request.Context(), tenantID, req.Channel, req.ChannelAccountID, req.DateFrom, req.DateTo)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Start async import based on channel
	go h.processChannelImport(tenantID, jobID, req.Channel, req.ChannelAccountID, req.DateFrom, req.DateTo)

	c.JSON(http.StatusAccepted, gin.H{
		"job_id":  jobID,
		"status":  "started",
		"message": "Import job started successfully",
	})
}

// processChannelImport handles async import for different channels
func (h *OrderHandler) processChannelImport(tenantID, jobID, channel, channelAccountID, dateFrom, dateTo string) {
	ctx := context.Background()
	
	// Update job to running
	h.orderService.UpdateImportJobStatus(ctx, tenantID, jobID, "running", 0, 0, nil)

	// Parse dates
	var createdAfter, createdBefore time.Time
	var err error

	if dateFrom != "" {
		createdAfter, err = time.Parse("2006-01-02", dateFrom)
		if err != nil {
			log.Printf("Invalid date_from: %v", err)
			h.orderService.UpdateImportJobStatus(ctx, tenantID, jobID, "failed", 0, 0, []string{err.Error()})
			return
		}
	} else {
		// Default to last 30 days
		createdAfter = time.Now().AddDate(0, 0, -30)
	}

	if dateTo != "" {
		createdBefore, err = time.Parse("2006-01-02", dateTo)
		if err != nil {
			log.Printf("Invalid date_to: %v", err)
			h.orderService.UpdateImportJobStatus(ctx, tenantID, jobID, "failed", 0, 0, []string{err.Error()})
			return
		}
	} else {
		createdBefore = time.Now()
	}

	// Route to appropriate channel handler
	var imported int
	var importErr error

	switch channel {
	case "amazon":
		imported, importErr = h.amazonOrdersHandler.ImportAmazonOrders(ctx, tenantID, channelAccountID, createdAfter, createdBefore)
	case "ebay":
		imported, importErr = h.ebayOrdersHandler.ImportEbayOrders(ctx, tenantID, channelAccountID, createdAfter, createdBefore)
	case "temu":
		imported, importErr = h.temuOrdersHandler.ImportTemuOrders(ctx, tenantID, channelAccountID, createdAfter, createdBefore)
	case "tiktok":
		if h.tiktokOrdersHandler != nil {
			imported, importErr = h.tiktokOrdersHandler.ImportTikTokOrders(ctx, tenantID, channelAccountID, createdAfter, createdBefore)
		} else {
			importErr = fmt.Errorf("TikTok orders handler not initialised")
		}
	case "etsy":
		if h.etsyOrdersHandler != nil {
			imported, importErr = h.etsyOrdersHandler.ImportEtsyOrders(ctx, tenantID, channelAccountID, createdAfter, createdBefore)
		} else {
			importErr = fmt.Errorf("Etsy orders handler not initialised")
		}
	case "woocommerce":
		if h.wooOrdersHandler != nil {
			imported, importErr = h.wooOrdersHandler.ImportWooCommerceOrders(ctx, tenantID, channelAccountID, createdAfter, createdBefore)
		} else {
			importErr = fmt.Errorf("WooCommerce orders handler not initialised")
		}
	case "walmart":
		if h.walmartOrdersHandler != nil {
			imported, importErr = h.walmartOrdersHandler.ImportWalmartOrders(ctx, tenantID, channelAccountID, createdAfter, createdBefore)
		} else {
			importErr = fmt.Errorf("Walmart orders handler not initialised")
		}
	case "kaufland":
		if h.kauflandOrdersHandler != nil {
			imported, importErr = h.kauflandOrdersHandler.ImportKauflandOrders(ctx, tenantID, channelAccountID, createdAfter, createdBefore)
		} else {
			importErr = fmt.Errorf("Kaufland orders handler not initialised")
		}
	case "magento":
		if h.magentoOrdersHandler != nil {
			imported, importErr = h.magentoOrdersHandler.ImportMagentoOrders(ctx, tenantID, channelAccountID, createdAfter, createdBefore)
		} else {
			importErr = fmt.Errorf("Magento orders handler not initialised")
		}
	case "bigcommerce":
		if h.bigcommerceOrdersHandler != nil {
			imported, importErr = h.bigcommerceOrdersHandler.ImportBigCommerceOrders(ctx, tenantID, channelAccountID, createdAfter, createdBefore)
		} else {
			importErr = fmt.Errorf("BigCommerce orders handler not initialised")
		}
	case "onbuy":
		if h.onbuyOrdersHandler != nil {
			imported, importErr = h.onbuyOrdersHandler.ImportOnBuyOrders(ctx, tenantID, channelAccountID, createdAfter, createdBefore)
		} else {
			importErr = fmt.Errorf("OnBuy orders handler not initialised")
		}
	case "backmarket":
		if h.backmarketOrdersHandler != nil {
			imported, importErr = h.backmarketOrdersHandler.ImportBackMarketOrders(ctx, tenantID, channelAccountID, createdAfter, createdBefore)
		} else {
			importErr = fmt.Errorf("Back Market orders handler not initialised")
		}
	case "zalando":
		if h.zalandoOrdersHandler != nil {
			imported, importErr = h.zalandoOrdersHandler.ImportZalandoOrders(ctx, tenantID, channelAccountID, createdAfter, createdBefore)
		} else {
			importErr = fmt.Errorf("Zalando orders handler not initialised")
		}
	case "bol":
		if h.bolOrdersHandler != nil {
			imported, importErr = h.bolOrdersHandler.ImportBolOrders(ctx, tenantID, channelAccountID, createdAfter, createdBefore)
		} else {
			importErr = fmt.Errorf("Bol.com orders handler not initialised")
		}
	case "lazada":
		if h.lazadaOrdersHandler != nil {
			imported, importErr = h.lazadaOrdersHandler.ImportLazadaOrders(ctx, tenantID, channelAccountID, createdAfter, createdBefore)
		} else {
			importErr = fmt.Errorf("Lazada orders handler not initialised")
		}
	case "shopify":
		if h.shopifyHandler != nil {
			imported, importErr = h.shopifyHandler.ImportShopifyOrders(ctx, tenantID, channelAccountID, createdAfter, createdBefore)
		} else {
			importErr = fmt.Errorf("Shopify handler not initialised")
		}
	case "shopwired":
		if h.shopwiredOrdersHandler != nil {
			imported, importErr = h.shopwiredOrdersHandler.ImportShopWiredOrders(ctx, tenantID, channelAccountID, createdAfter, createdBefore)
		} else {
			importErr = fmt.Errorf("ShopWired orders handler not initialised")
		}
	default:
		importErr = fmt.Errorf("unsupported channel: %s", channel)
	}

	// Update job status
	if importErr != nil {
		log.Printf("Import failed for job %s: %v", jobID, importErr)
		h.orderService.UpdateImportJobStatus(ctx, tenantID, jobID, "failed", imported, 0, []string{importErr.Error()})
	} else {
		log.Printf("Import completed for job %s: %d orders imported", jobID, imported)
		h.orderService.UpdateImportJobStatus(ctx, tenantID, jobID, "completed", imported, 0, nil)
	}
}

// GetImportJob handles GET /api/v1/orders/import/jobs/:id
func (h *OrderHandler) GetImportJob(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	jobID := c.Param("id")

	job, err := h.orderService.GetImportJob(c.Request.Context(), tenantID, jobID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Job not found"})
		return
	}

	c.JSON(http.StatusOK, job)
}

// ListImportJobs handles GET /api/v1/orders/import/jobs
func (h *OrderHandler) ListImportJobs(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	jobs, err := h.orderService.ListImportJobs(c.Request.Context(), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"jobs": jobs,
	})
}

// GetOrderStats handles GET /api/v1/orders/stats
func (h *OrderHandler) GetOrderStats(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	stats, err := h.orderService.GetOrderStats(c.Request.Context(), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// BulkUpdateStatus handles POST /api/v1/orders/bulk/status
func (h *OrderHandler) BulkUpdateStatus(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req struct {
		OrderIDs  []string `json:"order_ids" binding:"required"`
		Status    string   `json:"status" binding:"required"`
		SubStatus string   `json:"sub_status"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	results, err := h.orderService.BulkUpdateStatus(c.Request.Context(), tenantID, req.OrderIDs, req.Status, req.SubStatus)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"updated": results,
	})
}

// ── S3: Order import config helpers ──────────────────────────────────────────

// ApplyOrderImportConfig mutates an order in-place to apply per-channel
// configuration set in the credential's ChannelOrderConfig:
//
//   - OrderPrefix: prepended to ExternalOrderID (used for dedup) and
//     also stored on the order so it's visible in the UI.
//   - DownloadUnpaidOrders / ReserveUnpaidStock: handled by callers
//     when filtering by payment_status.
//   - ChannelTaxEnabled: stored as a flag on the order for downstream use.
//
// Call this immediately after converting the raw channel order to an
// internal models.Order, before calling orderService.CreateOrder.
func ApplyOrderImportConfig(order *models.Order, cfg models.ChannelOrderConfig) {
	if cfg.OrderPrefix != "" {
		order.ExternalOrderID = cfg.OrderPrefix + order.ExternalOrderID
	}
	if cfg.ChannelTaxEnabled {
		// Store a tag so the order totals are not recalculated downstream
		if order.Tags == nil {
			order.Tags = []string{}
		}
		alreadyTagged := false
		for _, t := range order.Tags {
			if t == "channel_tax" {
				alreadyTagged = true
				break
			}
		}
		if !alreadyTagged {
			order.Tags = append(order.Tags, "channel_tax")
		}
	}
}
