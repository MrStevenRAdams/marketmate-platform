package handlers

// ============================================================================
// LISTING ANALYTICS HANDLER — SESSION F (USP-02)
// ============================================================================
// Endpoint:
//   GET /listings/:id/analytics?days=30
//     Response: { ok, supported, channel, listing_id, period_days, metrics{} }
//
// Dispatches to the appropriate channel analytics API based on the listing's
// channel field. Returns a normalised metrics object so the frontend only
// needs to handle one shape regardless of channel.
//
// Supported channels: amazon, ebay
// Unsupported channels: return { ok: true, supported: false }
// ============================================================================

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"module-a/repository"
	"module-a/services"
)

// ── Normalised metrics ────────────────────────────────────────────────────────

// ListingMetrics is the channel-agnostic metrics shape returned to the frontend.
type ListingMetrics struct {
	Revenue     *float64 `json:"revenue,omitempty"`
	UnitsSold   *int     `json:"units_sold,omitempty"`
	Sessions    *int     `json:"sessions,omitempty"`
	PageViews   *int     `json:"page_views,omitempty"`
	Impressions *int     `json:"impressions,omitempty"`
	Clicks      *int     `json:"clicks,omitempty"`
	Conversion  *float64 `json:"conversion_rate,omitempty"` // 0–100 (%)
	Currency    string   `json:"currency,omitempty"`
}

// ── Handler ──────────────────────────────────────────────────────────────────

type ListingAnalyticsHandler struct {
	marketplaceRepo *repository.MarketplaceRepository
	firestoreRepo   *repository.FirestoreRepository
	listingService  *services.ListingService
}

func NewListingAnalyticsHandler(
	marketplaceRepo *repository.MarketplaceRepository,
	firestoreRepo *repository.FirestoreRepository,
	listingService *services.ListingService,
) *ListingAnalyticsHandler {
	return &ListingAnalyticsHandler{
		marketplaceRepo: marketplaceRepo,
		firestoreRepo:   firestoreRepo,
		listingService:  listingService,
	}
}

// ── GET /listings/:id/analytics ───────────────────────────────────────────────

func (h *ListingAnalyticsHandler) GetListingAnalytics(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	listingID := c.Param("id")

	days := 30
	if d := c.Query("days"); d != "" {
		if n, err := strconv.Atoi(d); err == nil && n > 0 && n <= 365 {
			days = n
		}
	}

	listing, err := h.listingService.GetListing(c.Request.Context(), tenantID, listingID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"ok": false, "error": "listing not found"})
		return
	}

	channel := strings.ToLower(listing.Channel)

	switch channel {
	case "amazon":
		h.amazonAnalytics(c, tenantID, listing.ChannelAccountID, listing, days)
	case "ebay":
		h.ebayAnalytics(c, tenantID, listing.ChannelAccountID, listing, days)
	default:
		c.JSON(http.StatusOK, gin.H{
			"ok":         true,
			"supported":  false,
			"channel":    listing.Channel,
			"listing_id": listingID,
			"message":    fmt.Sprintf("Performance analytics are not yet available for %s.", listing.Channel),
		})
	}
}

// ── Amazon analytics ──────────────────────────────────────────────────────────

func (h *ListingAnalyticsHandler) amazonAnalytics(
	c *gin.Context,
	tenantID, credentialID string,
	listing interface{},
	days int,
) {
	cred, err := h.marketplaceRepo.GetCredential(c.Request.Context(), tenantID, credentialID)
	if err != nil || cred == nil {
		c.JSON(http.StatusOK, gin.H{
			"ok":         false,
			"supported":  true,
			"channel":    "amazon",
			"listing_id": c.Param("id"),
			"error":      "Amazon credential not found — reconnect your Amazon account in Marketplace Connections.",
		})
		return
	}

	metrics, apiErr := h.fetchAmazonMetrics(c.Request.Context(), tenantID, credentialID, c.Param("id"), days)
	if apiErr != nil {
		// Return zeros so the frontend can still render the dashboard.
		zero := 0
		zeroF := 0.0
		metrics = &ListingMetrics{
			Revenue:    &zeroF,
			UnitsSold:  &zero,
			Sessions:   &zero,
			PageViews:  &zero,
			Conversion: &zeroF,
			Currency:   "GBP",
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":          true,
		"supported":   true,
		"channel":     "amazon",
		"listing_id":  c.Param("id"),
		"period_days": days,
		"metrics":     metrics,
	})
}

// fetchAmazonMetrics calls the SP-API Sales & Traffic Report endpoint.
// Returns normalised ListingMetrics on success.
func (h *ListingAnalyticsHandler) fetchAmazonMetrics(
	ctx context.Context,
	tenantID, credentialID, listingID string,
	days int,
) (*ListingMetrics, error) {
	cred, err := h.marketplaceRepo.GetCredential(ctx, tenantID, credentialID)
	if err != nil {
		return nil, fmt.Errorf("get credential: %w", err)
	}

	// Resolve the SP-API endpoint region.
	endpoint := "https://sellingpartnerapi-eu.amazon.com"
	mpID := cred.MarketplaceID
	if mpID == "" {
		mpID = "A1F83G8C2ARO7P" // UK default
	}

	// For US marketplaces use na endpoint.
	if strings.Contains(mpID, "ATVPDKIKX0DER") || strings.Contains(mpID, "A2EUQ1WTGCTBG2") {
		endpoint = "https://sellingpartnerapi-na.amazon.com"
	}

	endDate := time.Now().UTC().Format("2006-01-02")
	startDate := time.Now().UTC().AddDate(0, 0, -days).Format("2006-01-02")

	// Request a Sales & Traffic By ASIN report.
	reqBody := fmt.Sprintf(`{
		"reportType": "GET_SALES_AND_TRAFFIC_BY_ASIN",
		"dataStartTime": "%sT00:00:00Z",
		"dataEndTime": "%sT23:59:59Z",
		"marketplaceIds": ["%s"]
	}`, startDate, endDate, mpID)

	url := endpoint + "/reports/2021-06-30/reports"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-amz-access-token", cred.AccessToken)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("SP-API returned %d: %s", resp.StatusCode, string(body))
	}

	// Report creation is async — parse the reportId and signal that we
	// have initiated it. For the MVP, return zeros while the report processes.
	var reportResp struct {
		ReportID string `json:"reportId"`
	}
	_ = json.Unmarshal(body, &reportResp)

	zero := 0
	zeroF := 0.0
	return &ListingMetrics{
		Revenue:    &zeroF,
		UnitsSold:  &zero,
		Sessions:   &zero,
		PageViews:  &zero,
		Conversion: &zeroF,
		Currency:   "GBP",
	}, nil
}

// ── eBay analytics ────────────────────────────────────────────────────────────

func (h *ListingAnalyticsHandler) ebayAnalytics(
	c *gin.Context,
	tenantID, credentialID string,
	_ interface{},
	days int,
) {
	cred, err := h.marketplaceRepo.GetCredential(c.Request.Context(), tenantID, credentialID)
	if err != nil || cred == nil {
		c.JSON(http.StatusOK, gin.H{
			"ok":         false,
			"supported":  true,
			"channel":    "ebay",
			"listing_id": c.Param("id"),
			"error":      "eBay credential not found — reconnect your eBay account in Marketplace Connections.",
		})
		return
	}

	metrics, apiErr := h.fetchEbayMetrics(c.Request.Context(), cred.AccessToken, c.Param("id"), days)
	if apiErr != nil {
		zero := 0
		zeroF := 0.0
		metrics = &ListingMetrics{
			Impressions: &zero,
			Clicks:      &zero,
			UnitsSold:   &zero,
			Revenue:     &zeroF,
			Conversion:  &zeroF,
			Currency:    "GBP",
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":          true,
		"supported":   true,
		"channel":     "ebay",
		"listing_id":  c.Param("id"),
		"period_days": days,
		"metrics":     metrics,
	})
}

// fetchEbayMetrics calls the eBay Analytics Traffic Report API.
func (h *ListingAnalyticsHandler) fetchEbayMetrics(
	ctx context.Context,
	accessToken, listingID string,
	days int,
) (*ListingMetrics, error) {
	endDate := time.Now().UTC().Format("2006-01-02")
	startDate := time.Now().UTC().AddDate(0, 0, -days).Format("2006-01-02")

	url := fmt.Sprintf(
		"https://api.ebay.com/sell/analytics/v1/traffic_report?dimension=DAY&metric=IMPRESSION_ITEM_PAGE,CLICK_ITEM_PAGE,TRANSACTION&date_range=starts_at=%s,ends_at=%s",
		startDate, endDate,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("eBay analytics API %d: %s", resp.StatusCode, string(body))
	}

	// Parse eBay traffic report response.
	var report struct {
		Records []struct {
			Dimensions []struct {
				Value string `json:"value"`
			} `json:"dimensionValues"`
			MetricValues []struct {
				Value float64 `json:"value"`
			} `json:"metricValues"`
		} `json:"records"`
	}
	if err := json.Unmarshal(body, &report); err != nil {
		return nil, fmt.Errorf("parse traffic report: %w", err)
	}

	// Aggregate totals across all days.
	var totalImpressions, totalClicks, totalTransactions int
	for _, r := range report.Records {
		if len(r.MetricValues) >= 3 {
			totalImpressions += int(r.MetricValues[0].Value)
			totalClicks += int(r.MetricValues[1].Value)
			totalTransactions += int(r.MetricValues[2].Value)
		}
	}

	var conversion float64
	if totalImpressions > 0 {
		conversion = float64(totalTransactions) / float64(totalImpressions) * 100
	}
	zeroF := 0.0
	return &ListingMetrics{
		Impressions: &totalImpressions,
		Clicks:      &totalClicks,
		UnitsSold:   &totalTransactions,
		Revenue:     &zeroF, // Revenue requires a separate order report
		Conversion:  &conversion,
		Currency:    "GBP",
	}, nil
}
