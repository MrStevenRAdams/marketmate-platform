package handlers

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"

	"module-a/instrumentation"
	"module-a/services"
)

// KeywordIntelligenceHandler serves the keyword intelligence and SEO endpoints.
type KeywordIntelligenceHandler struct {
	firestoreClient            *firestore.Client
	keywordIntelligenceService *services.KeywordIntelligenceService
	keywordScoreService        *services.KeywordScoreService
	usageService               *services.UsageService
}

// NewKeywordIntelligenceHandler constructs the handler with its dependencies.
func NewKeywordIntelligenceHandler(
	firestoreClient *firestore.Client,
	kwIntelSvc *services.KeywordIntelligenceService,
	kwScoreSvc *services.KeywordScoreService,
) *KeywordIntelligenceHandler {
	return &KeywordIntelligenceHandler{
		firestoreClient:            firestoreClient,
		keywordIntelligenceService: kwIntelSvc,
		keywordScoreService:        kwScoreSvc,
	}
}

// SetUsageService wires in the usage service for credit checks.
// Called from main after construction so we don't break the existing signature.
func (h *KeywordIntelligenceHandler) SetUsageService(svc *services.UsageService) {
	h.usageService = svc
}

// checkAndDeductCredits returns (balance, ok).
// If ok is false the handler should return 402 immediately.
func (h *KeywordIntelligenceHandler) checkAndDeductCredits(
	ctx context.Context,
	tenantID string,
	cost float64,
	eventType string,
	metadata map[string]string,
) (float64, bool) {
	if h.usageService == nil {
		return 0, true
	}
	ledger, err := h.usageService.GetCurrentLedger(ctx, tenantID)
	if err != nil || ledger == nil {
		return 0, true
	}
	var balance float64
	if ledger.CreditsRemaining != nil {
		balance = *ledger.CreditsRemaining
	} else {
		return 0, true // unlimited plan
	}
	if balance < cost {
		return balance, false
	}
	return balance, true
}

// POST /api/v1/products/:id/keyword-intelligence/ingest
func (h *KeywordIntelligenceHandler) Ingest(c *gin.Context) {
	productID := c.Param("id")
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	contentType := c.GetHeader("Content-Type")

	if strings.Contains(contentType, "application/json") {
		var body struct {
			SourceType string `json:"source_type"`
			ASIN       string `json:"asin"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if body.SourceType != "competitor_asin" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "JSON body only supported for source_type=competitor_asin"})
			return
		}
		if body.ASIN == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "asin is required for competitor_asin source type"})
			return
		}

		balance, ok := h.checkAndDeductCredits(ctx, tenantID, 0.5,
			instrumentation.EVTYPE_DATAFORSEO_COMPETITOR_LOOKUP,
			map[string]string{"asin": body.ASIN, "product_id": productID},
		)
		if !ok {
			c.JSON(http.StatusPaymentRequired, gin.H{
				"error":    "insufficient_credits",
				"balance":  balance,
				"required": 0.5,
			})
			return
		}

		_ = instrumentation.LogUsageEvent(ctx, h.firestoreClient, instrumentation.UsageEvent{
			TenantID:        tenantID,
			EventType:       instrumentation.EVTYPE_DATAFORSEO_COMPETITOR_LOOKUP,
			ProductID:       productID,
			CreditCost:      0.5,
			PlatformCostUSD: 0.012,
			DataSource:      "dataforseo",
			Metadata:        map[string]string{"asin": body.ASIN, "product_id": productID},
			Timestamp:       time.Now(),
		})

		if err := h.keywordIntelligenceService.EnsureDataForSEOEnrichment(ctx, body.ASIN, tenantID, true); err != nil {
			log.Printf("[kw-ingest] competitor_asin failed product=%s asin=%s: %v", productID, body.ASIN, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "competitor_analysis_failed", "message": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"status": "ok", "product_id": productID, "source_type": "competitor_asin", "asin": body.ASIN})
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file field required: " + err.Error()})
		return
	}
	defer file.Close()

	sourceType := c.Request.FormValue("source_type")
	if sourceType == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "source_type field required"})
		return
	}
	switch sourceType {
	case "brand_analytics", "terapeak", "generic":
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "unknown source_type: " + sourceType})
		return
	}

	csvBytes, err := io.ReadAll(io.LimitReader(file, 10<<20))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read file"})
		return
	}

	log.Printf("[kw-ingest] CSV upload product=%s source=%s filename=%s size=%d",
		productID, sourceType, header.Filename, len(csvBytes))

	if err := h.keywordIntelligenceService.IngestCSV(ctx, tenantID, productID, csvBytes, sourceType); err != nil {
		log.Printf("[kw-ingest] IngestCSV failed product=%s source=%s: %v", productID, sourceType, err)
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "ingest_failed", "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok", "product_id": productID, "source_type": sourceType})
}

// POST /api/v1/products/:id/keyword-intelligence/refresh
//
// Two branches determined by whether the product has an ASIN:
//
//	No ASIN → Layer 4 AI re-analysis — 0.5 credits (EVTYPE_AI_KEYWORD_REANALYSIS)
//	Has ASIN → DataForSEO refresh     — 0.25 credits (EVTYPE_DATAFORSEO_ASIN_REFRESH)
//
// The frontend distinguishes these by whether it passes ?asin= in the query
// string. For Layer 4 products, pass ?title=<title>&category=<cat> instead.
func (h *KeywordIntelligenceHandler) Refresh(c *gin.Context) {
	productID := c.Param("id")
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	// --- Resolve ASIN (try query param first, then Firestore) ---
	asin := c.Query("asin")
	title := c.Query("title")
	category := c.Query("category")

	if asin == "" {
		pdoc, err := h.firestoreClient.
			Collection("tenants").Doc(tenantID).
			Collection("products").Doc(productID).
			Get(ctx)
		if err == nil {
			data := pdoc.Data()
			if v, ok := data["asin"].(string); ok {
				asin = v
			} else if ids, ok := data["identifiers"].(map[string]interface{}); ok {
				if v, ok := ids["asin"].(string); ok {
					asin = v
				}
			}
			// Also pick up title/category from Firestore if not in query params.
			if title == "" {
				if v, ok := data["title"].(string); ok {
					title = v
				}
			}
			if category == "" {
				if v, ok := data["category"].(string); ok {
					category = v
				}
			}
		}
	}

	// --- Branch: no ASIN → Layer 4 AI re-analysis (0.5 credits) ---
	if asin == "" {
		balance, ok := h.checkAndDeductCredits(ctx, tenantID, 0.5,
			instrumentation.EVTYPE_AI_KEYWORD_REANALYSIS,
			map[string]string{"product_id": productID},
		)
		if !ok {
			c.JSON(http.StatusPaymentRequired, gin.H{
				"error":    "insufficient_credits",
				"balance":  balance,
				"required": 0.5,
			})
			return
		}

		_ = instrumentation.LogUsageEvent(ctx, h.firestoreClient, instrumentation.UsageEvent{
			TenantID:   tenantID,
			EventType:  instrumentation.EVTYPE_AI_KEYWORD_REANALYSIS,
			ProductID:  productID,
			CreditCost: 0.5,
			DataSource: "anthropic",
			Metadata:   map[string]string{"product_id": productID, "layer": "4"},
			Timestamp:  time.Now(),
		})

		// cacheKey for Layer 4 is the product ID (no ASIN available)
		cacheKey := productID
		if _, err := h.keywordIntelligenceService.RefreshFromAI(ctx, category, cacheKey); err != nil {
			log.Printf("[kw-refresh] AI re-analysis failed product=%s: %v", productID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "refresh_failed", "message": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status":     "ok",
			"product_id": productID,
			"layer":      "ai",
			"refreshed":  time.Now(),
		})
		return
	}

	// --- Branch: has ASIN → DataForSEO refresh (0.25 credits) ---
	balance, ok := h.checkAndDeductCredits(ctx, tenantID, 0.25,
		instrumentation.EVTYPE_DATAFORSEO_ASIN_REFRESH,
		map[string]string{"asin": asin, "product_id": productID},
	)
	if !ok {
		c.JSON(http.StatusPaymentRequired, gin.H{
			"error":    "insufficient_credits",
			"balance":  balance,
			"required": 0.25,
		})
		return
	}

	_ = instrumentation.LogUsageEvent(ctx, h.firestoreClient, instrumentation.UsageEvent{
		TenantID:   tenantID,
		EventType:  instrumentation.EVTYPE_DATAFORSEO_ASIN_REFRESH,
		ProductID:  productID,
		CreditCost: 0.25,
		DataSource: "dataforseo",
		Metadata:   map[string]string{"asin": asin, "product_id": productID},
		Timestamp:  time.Now(),
	})

	if err := h.keywordIntelligenceService.ForceRefreshFromDataForSEO(ctx, asin, tenantID); err != nil {
		log.Printf("[kw-refresh] DataForSEO refresh failed product=%s asin=%s: %v", productID, asin, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "refresh_failed", "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":     "ok",
		"product_id": productID,
		"asin":       asin,
		"refreshed":  time.Now(),
	})
}

// GET /api/v1/products/:id/keyword-intelligence
func (h *KeywordIntelligenceHandler) GetKeywordIntelligence(c *gin.Context) {
	ctx := c.Request.Context()
	productID := c.Param("id")

	asin := c.Query("asin")
	title := c.Query("title")
	category := c.Query("category")

	cacheKey := asin
	if cacheKey == "" {
		cacheKey = productID
	}

	productInfo := services.ProductInfo{ASIN: asin, Title: title, Category: category}

	kwSet, err := h.keywordIntelligenceService.GetOrCreateKeywordSet(ctx, cacheKey, productInfo)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "keyword_intelligence_failed", "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"product_id": productID, "keyword_set": kwSet})
}

// GET /api/v1/listings/:id/seo-score
func (h *KeywordIntelligenceHandler) GetSEOScore(c *gin.Context) {
	ctx := c.Request.Context()
	listingID := c.Param("id")
	tenantID := c.GetString("tenant_id")

	listingDoc, err := h.firestoreClient.
		Collection("tenants").Doc(tenantID).
		Collection("listings").Doc(listingID).
		Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "listing_not_found", "message": "listing " + listingID + " not found"})
		return
	}

	data := listingDoc.Data()

	listing := services.Listing{
		Title:       stringField(data, "title"),
		Description: stringField(data, "description"),
		Brand:       stringField(data, "brand"),
		Color:       stringField(data, "color"),
		Size:        stringField(data, "size"),
		Material:    stringField(data, "material"),
	}
	if rawBullets, ok := data["bullet_points"].([]interface{}); ok {
		for _, b := range rawBullets {
			if s, ok := b.(string); ok {
				listing.Bullets = append(listing.Bullets, s)
			}
		}
	}

	asin := stringField(data, "asin")
	productID := stringField(data, "product_id")
	cacheKey := asin
	if cacheKey == "" {
		cacheKey = productID
	}

	productInfo := services.ProductInfo{ASIN: asin, Title: listing.Title, Category: stringField(data, "category")}

	kwSet, err := h.keywordIntelligenceService.GetOrCreateKeywordSet(ctx, cacheKey, productInfo)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "keyword_set_failed", "message": err.Error()})
		return
	}

	score := h.keywordScoreService.ScoreListing(listing, kwSet)

	_ = instrumentation.LogUsageEvent(ctx, h.firestoreClient, instrumentation.UsageEvent{
		TenantID:   tenantID,
		EventType:  instrumentation.EVTYPE_SEO_SCORE_CALCULATION,
		ListingID:  listingID,
		CreditCost: 0,
		Metadata:   map[string]string{"listing_id": listingID, "score": intToStr(score.Total)},
	})

	c.JSON(http.StatusOK, gin.H{"listing_id": listingID, "score": score})
}

// EnrichFromImport handles POST /internal/keyword-intelligence/enrich
// Auth: OIDC — only import-enrich Cloud Run calls this. Registered on bare router.
func (h *KeywordIntelligenceHandler) EnrichFromImport(c *gin.Context) {
	var req struct {
		ASIN        string                 `json:"asin"`
		TenantID    string                 `json:"tenant_id"`
		CatalogData map[string]interface{} `json:"catalog_data"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json", "detail": err.Error()})
		return
	}
	if req.ASIN == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "asin is required"})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"status": "accepted", "asin": req.ASIN})

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := h.keywordIntelligenceService.EnrichFromCatalogData(ctx, req.ASIN, req.CatalogData); err != nil {
			log.Printf("[kw-enrich] catalog extract failed asin=%s: %v", req.ASIN, err)
		}
		if req.TenantID != "" {
			if err := h.keywordIntelligenceService.RefreshFromAmazonAdsAPI(ctx, req.ASIN, req.TenantID); err != nil {
				log.Printf("[kw-enrich] ads enrichment failed asin=%s tenant=%s: %v", req.ASIN, req.TenantID, err)
			}
		}
	}()
}

// GET /api/v1/listings/seo-summary
// Optional query params:
//   ?limit=N  — max docs to read (default 200, max 500)
func (h *KeywordIntelligenceHandler) GetSEOSummary(c *gin.Context) {
	ctx := c.Request.Context()
	tenantID := c.GetString("tenant_id")

	// Parse limit param
	limit := 200
	if lStr := c.Query("limit"); lStr != "" {
		if v, err := parseLimitInt(lStr); err == nil && v > 0 {
			if v > 500 {
				v = 500
			}
			limit = v
		}
	}

	type listingEntry struct {
		ListingID string `json:"listing_id"`
		SEOScore  *int   `json:"seo_score"`
	}

	type scoreDistribution struct {
		Excellent        int `json:"excellent"`
		Good             int `json:"good"`
		NeedsImprovement int `json:"needs_improvement"`
		Poor             int `json:"poor"`
	}

	// Field-masked query — only seo_score fetched from Firestore
	iter := h.firestoreClient.
		Collection("tenants").Doc(tenantID).
		Collection("listings").
		Select("seo_score").
		Limit(limit).
		Documents(ctx)
	defer iter.Stop()

	var entries []listingEntry
	var scoreTotal, scored int
	var dist scoreDistribution

	for {
		doc, err := iter.Next()
		if err != nil {
			break
		}
		data := doc.Data()
		entry := listingEntry{ListingID: doc.Ref.ID}
		if v, ok := data["seo_score"]; ok && v != nil {
			var n int
			switch val := v.(type) {
			case int64:
				n = int(val)
			case float64:
				n = int(val)
			default:
				entries = append(entries, entry)
				continue
			}
			entry.SEOScore = &n
			scoreTotal += n
			scored++
			switch {
			case n >= 90:
				dist.Excellent++
			case n >= 70:
				dist.Good++
			case n >= 40:
				dist.NeedsImprovement++
			default:
				dist.Poor++
			}
		}
		entries = append(entries, entry)
	}

	var avg float64
	if scored > 0 {
		avg = float64(scoreTotal) / float64(scored)
	}
	if entries == nil {
		entries = []listingEntry{}
	}

	c.JSON(http.StatusOK, gin.H{
		"average_score":      avg,
		"listings":           entries,
		"score_distribution": dist,
	})
}

// parseLimitInt parses a base-10 integer string (used for ?limit= param).
func parseLimitInt(s string) (int, error) {
	v := 0
	if len(s) == 0 {
		return 0, fmt.Errorf("empty")
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("non-digit")
		}
		v = v*10 + int(c-'0')
	}
	return v, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func stringField(data map[string]interface{}, key string) string {
	if v, ok := data[key].(string); ok {
		return v
	}
	return ""
}

func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	buf := make([]byte, 0, 4)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	return string(buf)
}
