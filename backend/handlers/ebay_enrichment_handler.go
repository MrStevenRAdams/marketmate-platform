package handlers

// ============================================================================
// EBAY BROWSE ENRICHMENT HANDLER
// ============================================================================
//
// Routes:
//   POST /api/v1/ebay/enrich/product         — Enrich a single product now
//   POST /api/v1/ebay/enrich/bulk            — Queue bulk enrichment for all
//                                              eBay-imported products
//   GET  /api/v1/ebay/enrich/status          — Status of last bulk run
//   POST /api/v1/internal/ebay/enrich/task   — Cloud Tasks callback (internal)
//
// BULK FLOW
// ─────────
//   1. Client calls POST /bulk with optional filters
//   2. Handler finds all products with an ebay_item_* extended_data source
//      (meaning they were imported from eBay) that lack an ebay_epid_* source
//      (meaning they haven't been enriched yet)
//   3. Queues one Cloud Task per product (or processes in-process if
//      Cloud Tasks not configured)
//   4. Each task calls POST /internal/ebay/enrich/task
//
// SINGLE PRODUCT FLOW
// ───────────────────
//   Client sends: { product_id, ebay_item_id, ean, credential_id }
//   Handler runs enrichment immediately and returns results synchronously.
// ============================================================================

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"

	ebayClient "module-a/marketplace/clients/ebay"
	"module-a/models"
	"module-a/repository"
	"module-a/services"
)

type EbayEnrichmentHandler struct {
	enrichService *services.EbayEnrichmentService
	repo          *repository.MarketplaceRepository
	fsClient      *firestore.Client
}

func NewEbayEnrichmentHandler(
	enrichService *services.EbayEnrichmentService,
	repo *repository.MarketplaceRepository,
	fsClient *firestore.Client,
) *EbayEnrichmentHandler {
	return &EbayEnrichmentHandler{
		enrichService: enrichService,
		repo:          repo,
		fsClient:      fsClient,
	}
}

// ─── POST /api/v1/ebay/enrich/product ────────────────────────────────────────
// Enriches a single product immediately (synchronous, use for on-demand).

func (h *EbayEnrichmentHandler) EnrichProduct(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing tenant_id"})
		return
	}

	var req struct {
		ProductID    string `json:"product_id" binding:"required"`
		EbayItemID   string `json:"ebay_item_id"`
		EAN          string `json:"ean"`
		CredentialID string `json:"credential_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	client, err := h.buildEbayClient(c.Request.Context(), tenantID, req.CredentialID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("eBay client: %v", err)})
		return
	}

	result, err := h.enrichService.EnrichProduct(
		c.Request.Context(), tenantID,
		req.ProductID, req.EbayItemID, req.EAN, req.CredentialID,
		client,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"product_id":      result.ProductID,
		"branches":        result.BranchesWritten,
		"epid_found":      result.EpidFound,
		"eans_found":      result.EANsFound,
		"cross_listings":  result.CrossListings,
	})
}

// ─── POST /api/v1/ebay/enrich/bulk ───────────────────────────────────────────
// Queues enrichment for all eBay products that haven't been enriched yet.

func (h *EbayEnrichmentHandler) BulkEnrich(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing tenant_id"})
		return
	}

	var req struct {
		CredentialID string `json:"credential_id" binding:"required"`
		Force        bool   `json:"force"` // re-enrich even if already enriched
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Find all products that have eBay import data but lack EPID enrichment
	unenriched, err := h.findUnenrichedEbayProducts(c.Request.Context(), tenantID, req.Force)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if len(unenriched) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"queued":  0,
			"message": "All eBay products are already enriched",
		})
		return
	}

	// Write a bulk job record
	jobID := fmt.Sprintf("ebay_enrich_%d", time.Now().UnixNano())
	h.saveBulkJob(c.Request.Context(), tenantID, jobID, len(unenriched))

	// Process in background goroutine (Cloud Tasks can be wired in later)
	go h.processBulkEnrichment(tenantID, jobID, req.CredentialID, unenriched)

	c.JSON(http.StatusAccepted, gin.H{
		"job_id":  jobID,
		"queued":  len(unenriched),
		"message": fmt.Sprintf("Queued %d products for eBay Browse enrichment", len(unenriched)),
	})
}

// ─── GET /api/v1/ebay/enrich/status ──────────────────────────────────────────

func (h *EbayEnrichmentHandler) EnrichStatus(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing tenant_id"})
		return
	}

	// Get last 5 enrichment jobs
	iter := h.fsClient.Collection("tenants").Doc(tenantID).
		Collection("ebay_enrich_jobs").
		OrderBy("created_at", firestore.Desc).
		Limit(5).
		Documents(c.Request.Context())
	defer iter.Stop()

	var jobs []map[string]interface{}
	for {
		doc, err := iter.Next()
		if err != nil {
			break
		}
		jobs = append(jobs, doc.Data())
	}

	c.JSON(http.StatusOK, gin.H{"jobs": jobs})
}

// ─── POST /api/v1/internal/ebay/enrich/task ───────────────────────────────────
// Cloud Tasks callback — processes a single enrichment task.

func (h *EbayEnrichmentHandler) ProcessTask(c *gin.Context) {
	var req services.EbayEnrichmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	client, err := h.buildEbayClient(c.Request.Context(), req.TenantID, req.CredentialID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	result, err := h.enrichService.EnrichProduct(
		c.Request.Context(), req.TenantID,
		req.ProductID, req.EbayItemID, req.EAN, req.CredentialID,
		client,
	)
	if err != nil {
		log.Printf("[EbayEnrich] Task failed for %s: %v", req.ProductID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"product_id": result.ProductID,
		"branches":   len(result.BranchesWritten),
	})
}

// ─── Private helpers ──────────────────────────────────────────────────────────

type unenrichedProduct struct {
	ProductID  string
	EbayItemID string
	EAN        string
}

// findUnenrichedEbayProducts finds all products with ebay_item_* extended data
// but without ebay_epid_* extended data (i.e. not yet Browse-enriched).
func (h *EbayEnrichmentHandler) findUnenrichedEbayProducts(
	ctx context.Context, tenantID string, force bool,
) ([]unenrichedProduct, error) {

	// CollectionGroup query across all products/{id}/extended_data subcollections
	// for this tenant. Finds all eBay-imported extended data docs regardless of
	// which product they belong to. Requires a Firestore composite index on
	// (collection_group=extended_data, tenant_id ASC, source ASC).
	iter := h.fsClient.CollectionGroup("extended_data").
		Where("tenant_id", "==", tenantID).
		Where("source", "==", "ebay").
		Documents(ctx)
	defer iter.Stop()

	// Collect all products that have eBay import data
	productMap := make(map[string]*unenrichedProduct)
	for {
		doc, err := iter.Next()
		if err != nil {
			break
		}
		var ext models.ExtendedProductData
		if err := doc.DataTo(&ext); err != nil {
			continue
		}
		if ext.ProductID == "" {
			continue
		}

		if _, exists := productMap[ext.ProductID]; !exists {
			p := &unenrichedProduct{ProductID: ext.ProductID}
			// Extract eBay item ID from source_key pattern "ebay_{itemID}"
			if len(ext.SourceID) > 0 {
				p.EbayItemID = ext.SourceID
			}
			// Extract EAN if stored in data
			if ean, ok := ext.Data["ean"].(string); ok && ean != "" && ean != "Does not apply" {
				p.EAN = ean
			}
			productMap[ext.ProductID] = p
		}
	}

	if len(productMap) == 0 {
		return nil, nil
	}

	if force {
		// Return all eBay products
		result := make([]unenrichedProduct, 0, len(productMap))
		for _, p := range productMap {
			result = append(result, *p)
		}
		return result, nil
	}

	// CollectionGroup query to find products already Browse-enriched
	enrichedIter := h.fsClient.CollectionGroup("extended_data").
		Where("tenant_id", "==", tenantID).
		Where("source", "in", []string{"ebay_browse", "ebay_browse_epid", "ebay_browse_ean"}).
		Documents(ctx)
	defer enrichedIter.Stop()

	alreadyEnriched := make(map[string]bool)
	for {
		doc, err := enrichedIter.Next()
		if err != nil {
			break
		}
		var ext models.ExtendedProductData
		if err := doc.DataTo(&ext); err != nil {
			continue
		}
		if ext.ProductID != "" {
			alreadyEnriched[ext.ProductID] = true
		}
	}

	var unenriched []unenrichedProduct
	for pid, p := range productMap {
		if !alreadyEnriched[pid] {
			unenriched = append(unenriched, *p)
		}
	}

	return unenriched, nil
}

// processBulkEnrichment runs in a goroutine, enriching products one by one
// with a small delay between each to avoid rate limits.
func (h *EbayEnrichmentHandler) processBulkEnrichment(
	tenantID, jobID, credentialID string,
	products []unenrichedProduct,
) {
	ctx := context.Background()

	client, err := h.buildEbayClient(ctx, tenantID, credentialID)
	if err != nil {
		log.Printf("[EbayEnrich] Bulk job %s: failed to build eBay client: %v", jobID, err)
		h.updateBulkJob(ctx, tenantID, jobID, "failed", 0, len(products), err.Error())
		return
	}

	succeeded := 0
	failed := 0

	for i, p := range products {
		log.Printf("[EbayEnrich] Bulk job %s: processing %d/%d — product %s",
			jobID, i+1, len(products), p.ProductID)

		_, err := h.enrichService.EnrichProduct(
			ctx, tenantID,
			p.ProductID, p.EbayItemID, p.EAN, credentialID,
			client,
		)
		if err != nil {
			log.Printf("[EbayEnrich] Failed product %s: %v", p.ProductID, err)
			failed++
		} else {
			succeeded++
		}

		// Rate limit: ~2 requests/second (Browse API allows 5000/day per app)
		time.Sleep(500 * time.Millisecond)

		// Update progress every 10 items
		if i%10 == 0 {
			h.updateBulkJob(ctx, tenantID, jobID, "running", succeeded, failed, "")
		}
	}

	status := "completed"
	if failed > 0 && succeeded == 0 {
		status = "failed"
	} else if failed > 0 {
		status = "completed_with_errors"
	}

	h.updateBulkJob(ctx, tenantID, jobID, status, succeeded, failed, "")
	log.Printf("[EbayEnrich] Bulk job %s done: %d succeeded, %d failed", jobID, succeeded, failed)
}

func (h *EbayEnrichmentHandler) saveBulkJob(ctx context.Context, tenantID, jobID string, total int) {
	h.fsClient.Collection("tenants").Doc(tenantID).
		Collection("ebay_enrich_jobs").Doc(jobID).
		Set(ctx, map[string]interface{}{
			"job_id":     jobID,
			"status":     "queued",
			"total":      total,
			"succeeded":  0,
			"failed":     0,
			"created_at": time.Now(),
			"updated_at": time.Now(),
		})
}

func (h *EbayEnrichmentHandler) updateBulkJob(ctx context.Context, tenantID, jobID, status string, succeeded, failed int, errMsg string) {
	updates := map[string]interface{}{
		"status":     status,
		"succeeded":  succeeded,
		"failed":     failed,
		"updated_at": time.Now(),
	}
	if status == "completed" || status == "failed" || status == "completed_with_errors" {
		updates["completed_at"] = time.Now()
	}
	if errMsg != "" {
		updates["error"] = errMsg
	}
	h.fsClient.Collection("tenants").Doc(tenantID).
		Collection("ebay_enrich_jobs").Doc(jobID).
		Set(ctx, updates, firestore.MergeAll)
}

// buildEbayClient constructs an authenticated eBay client from stored credentials
func (h *EbayEnrichmentHandler) buildEbayClient(ctx context.Context, tenantID, credentialID string) (*ebayClient.Client, error) {
	cred, err := h.repo.GetCredential(ctx, tenantID, credentialID)
	if err != nil {
		return nil, fmt.Errorf("credential not found: %w", err)
	}
	if cred.Channel != "ebay" {
		return nil, fmt.Errorf("credential %s is not an eBay credential", credentialID)
	}

	clientID := cred.CredentialData["client_id"]
	clientSecret := cred.CredentialData["client_secret"]
	devID := cred.CredentialData["dev_id"]
	production := cred.Environment == "production"

	client := ebayClient.NewClient(clientID, clientSecret, devID, production)

	if refresh := cred.CredentialData["refresh_token"]; refresh != "" {
		client.SetTokens("", refresh)
	} else if token := cred.CredentialData["oauth_token"]; token != "" {
		client.SetTokens(token, "")
	} else {
		return nil, fmt.Errorf("no OAuth token found in credential")
	}

	if username := cred.CredentialData["seller_username"]; username != "" {
		client.SellerUsername = username
	}

	return client, nil
}

// JSON serialisation helper for extended data
func toJSON(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}
