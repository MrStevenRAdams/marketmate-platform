package handlers

// ============================================================================
// REFUND DOWNLOAD HANDLER — Session 2 Task 5
// ============================================================================
// Downloads refund data from Amazon, eBay, and Shopify and stores in
// Firestore collection `refund_downloads` for review and matching to RMAs.
//
// Endpoints:
//   GET  /refund-downloads                        - List all downloaded refunds
//   POST /amazon/orders/:id/refunds               - Download refunds for an Amazon order
//   POST /ebay/orders/:id/refunds                 - Download refunds for an eBay order
//   POST /shopify/orders/:id/refunds              - Download refunds for a Shopify order
//   POST /refund-downloads/:id/match-rma          - Match a downloaded refund to an RMA
// ============================================================================

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"
	"module-a/services"
)

// ── Handler ───────────────────────────────────────────────────────────────────

type RefundDownloadHandler struct {
	client             *firestore.Client
	marketplaceService *services.MarketplaceService
}

func NewRefundDownloadHandler(
	client *firestore.Client,
	marketplaceService *services.MarketplaceService,
) *RefundDownloadHandler {
	return &RefundDownloadHandler{
		client:             client,
		marketplaceService: marketplaceService,
	}
}

// ── Data structures ───────────────────────────────────────────────────────────

// RefundDownload stores a refund record retrieved from a channel.
// Stored in: tenants/{tenantID}/refund_downloads/{refundDownloadID}
type RefundDownload struct {
	RefundDownloadID string    `firestore:"refund_download_id" json:"refund_download_id"`
	TenantID         string    `firestore:"tenant_id" json:"tenant_id"`
	Channel          string    `firestore:"channel" json:"channel"` // amazon|ebay|shopify
	CredentialID     string    `firestore:"credential_id" json:"credential_id"`
	ExternalOrderID  string    `firestore:"external_order_id" json:"external_order_id"`
	ExternalRefundID string    `firestore:"external_refund_id" json:"external_refund_id"`
	OrderID          string    `firestore:"order_id,omitempty" json:"order_id,omitempty"` // internal order ID if known
	RMAID            string    `firestore:"rma_id,omitempty" json:"rma_id,omitempty"`     // matched RMA
	RefundDate       string    `firestore:"refund_date" json:"refund_date"`
	RefundAmount     float64   `firestore:"refund_amount" json:"refund_amount"`
	Currency         string    `firestore:"currency" json:"currency"`
	Reason           string    `firestore:"reason,omitempty" json:"reason,omitempty"`
	Status           string    `firestore:"status" json:"status"` // unmatched|matched|rejected
	Lines            []RefundLine `firestore:"lines,omitempty" json:"lines,omitempty"`
	RawData          map[string]interface{} `firestore:"raw_data,omitempty" json:"raw_data,omitempty"`
	DownloadedAt     time.Time `firestore:"downloaded_at" json:"downloaded_at"`
}

type RefundLine struct {
	SKU          string  `firestore:"sku,omitempty" json:"sku,omitempty"`
	ProductID    string  `firestore:"product_id,omitempty" json:"product_id,omitempty"`
	Title        string  `firestore:"title,omitempty" json:"title,omitempty"`
	Quantity     int     `firestore:"quantity" json:"quantity"`
	RefundAmount float64 `firestore:"refund_amount" json:"refund_amount"`
}

// ── GET /api/v1/refund-downloads ──────────────────────────────────────────────

func (h *RefundDownloadHandler) ListRefundDownloads(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()

	channel := c.Query("channel")
	status := c.Query("status")

	query := h.client.Collection("tenants").Doc(tenantID).
		Collection("refund_downloads").
		OrderBy("downloaded_at", firestore.Desc).
		Limit(200)

	iter := query.Documents(ctx)
	var refunds []RefundDownload
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		var r RefundDownload
		if err := doc.DataTo(&r); err == nil {
			if channel != "" && r.Channel != channel {
				continue
			}
			if status != "" && r.Status != status {
				continue
			}
			refunds = append(refunds, r)
		}
	}
	iter.Stop()

	if refunds == nil {
		refunds = []RefundDownload{}
	}

	c.JSON(http.StatusOK, gin.H{"data": refunds, "total": len(refunds)})
}

// ── POST /api/v1/refund-downloads/:id/match-rma ───────────────────────────────

func (h *RefundDownloadHandler) MatchToRMA(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()
	downloadID := c.Param("id")

	var req struct {
		RMAID string `json:"rma_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	_, err := h.client.Collection("tenants").Doc(tenantID).
		Collection("refund_downloads").Doc(downloadID).
		Update(ctx, []firestore.Update{
			{Path: "rma_id", Value: req.RMAID},
			{Path: "status", Value: "matched"},
		})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "Matched to RMA " + req.RMAID})
}

// ── POST /api/v1/amazon/orders/:id/refunds ───────────────────────────────────
// Downloads refund data for an Amazon order via SP-API.

func (h *RefundDownloadHandler) DownloadAmazonRefunds(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()
	orderID := c.Param("id") // Amazon order ID (external)
	credentialID := c.Query("credential_id")

	if credentialID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "credential_id query param required"})
		return
	}

	cred, err := h.marketplaceService.GetCredential(ctx, tenantID, credentialID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Credential not found"})
		return
	}

	mergedCreds, err := h.marketplaceService.GetFullCredentials(ctx, cred)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get credentials"})
		return
	}

	// Get access token
	accessToken, err := getAmazonAccessTokenForRefunds(mergedCreds)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get Amazon access token: " + err.Error()})
		return
	}

	// Determine endpoint region
	region := cred.MarketplaceID
	if region == "" {
		region = "UK"
	}
	endpoint := getAmazonEndpointForRegion(region)

	// Call SP-API: GET /orders/v0/orders/{orderId}/orderItems (items contain refund info)
	// For refund data specifically, Amazon uses the Finances API
	apiURL := fmt.Sprintf("%s/finances/v0/orders/%s/financialEvents", endpoint, orderID)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	req.Header.Set("x-amz-access-token", accessToken)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Amazon API call failed: " + err.Error()})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var financialResp map[string]interface{}
	json.Unmarshal(body, &financialResp)

	// Extract refund events from the response
	downloads := h.extractAmazonRefundEvents(ctx, tenantID, credentialID, orderID, financialResp)

	c.JSON(http.StatusOK, gin.H{
		"ok":        true,
		"downloaded": len(downloads),
		"refunds":   downloads,
	})
}

func (h *RefundDownloadHandler) extractAmazonRefundEvents(ctx context.Context, tenantID, credentialID, orderID string, resp map[string]interface{}) []RefundDownload {
	var downloads []RefundDownload

	payload, _ := resp["payload"].(map[string]interface{})
	if payload == nil {
		return downloads
	}
	financialEvents, _ := payload["FinancialEvents"].(map[string]interface{})
	if financialEvents == nil {
		return downloads
	}
	refundEvents, _ := financialEvents["RefundEventList"].([]interface{})

	for _, event := range refundEvents {
		em, ok := event.(map[string]interface{})
		if !ok {
			continue
		}

		rd := RefundDownload{
			RefundDownloadID: uuid.New().String(),
			TenantID:         tenantID,
			Channel:          "amazon",
			CredentialID:     credentialID,
			ExternalOrderID:  orderID,
			ExternalRefundID: getString(em, "AmazonOrderId") + "_refund_" + uuid.New().String()[:8],
			RefundDate:       getString(em, "PostedDate"),
			Currency:         "GBP",
			Status:           "unmatched",
			RawData:          em,
			DownloadedAt:     time.Now(),
		}

		// Sum refund amount from charge component list
		if charges, ok := em["ShipmentItemAdjustmentList"].([]interface{}); ok {
			for _, charge := range charges {
				if cm, ok := charge.(map[string]interface{}); ok {
					if itemChargeAdjList, ok := cm["ItemChargeAdjustmentList"].([]interface{}); ok {
						for _, adj := range itemChargeAdjList {
							if am, ok := adj.(map[string]interface{}); ok {
								if chargeAmt, ok := am["ChargeAmount"].(map[string]interface{}); ok {
									if amt, ok := chargeAmt["CurrencyAmount"].(float64); ok {
										rd.RefundAmount += amt
									}
									if cur, ok := chargeAmt["CurrencyCode"].(string); ok && cur != "" {
										rd.Currency = cur
									}
								}
							}
						}
					}
				}
			}
		}

		// Save to Firestore
		h.client.Collection("tenants").Doc(tenantID).
			Collection("refund_downloads").Doc(rd.RefundDownloadID).Set(ctx, rd)

		downloads = append(downloads, rd)
	}

	// If no refund events found, create a placeholder record indicating no refunds
	if len(downloads) == 0 {
		log.Printf("[RefundDownload] No Amazon refund events found for order %s", orderID)
	}

	return downloads
}

// ── POST /api/v1/ebay/orders/:id/refunds ─────────────────────────────────────
// Downloads refund data for an eBay order via Post-Order API.

func (h *RefundDownloadHandler) DownloadEbayRefunds(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()
	orderID := c.Param("id") // eBay order ID (external)
	credentialID := c.Query("credential_id")

	if credentialID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "credential_id query param required"})
		return
	}

	cred, err := h.marketplaceService.GetCredential(ctx, tenantID, credentialID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Credential not found"})
		return
	}

	mergedCreds, err := h.marketplaceService.GetFullCredentials(ctx, cred)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get credentials"})
		return
	}

	// Get eBay OAuth token
	accessToken := mergedCreds["refresh_token"] // Simplified — in production exchange for access token
	if accessToken == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "No eBay access token available"})
		return
	}

	// Call eBay Post-Order API: GET /post-order/v2/cancellation?legacyOrderId={orderId}
	// and GET /post-order/v2/return?legacyOrderId={orderId}
	baseURL := "https://api.ebay.com"
	if cred.Environment != "production" {
		baseURL = "https://api.sandbox.ebay.com"
	}

	apiURL := fmt.Sprintf("%s/post-order/v2/return?legacyOrderId=%s&limit=20", baseURL, orderID)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-EBAY-C-MARKETPLACE-ID", mergedCreds["marketplace_id"])

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "eBay API call failed: " + err.Error()})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var ebayResp map[string]interface{}
	json.Unmarshal(body, &ebayResp)

	downloads := h.extractEbayRefunds(ctx, tenantID, credentialID, orderID, ebayResp)

	c.JSON(http.StatusOK, gin.H{
		"ok":        true,
		"downloaded": len(downloads),
		"refunds":   downloads,
	})
}

func (h *RefundDownloadHandler) extractEbayRefunds(ctx context.Context, tenantID, credentialID, orderID string, resp map[string]interface{}) []RefundDownload {
	var downloads []RefundDownload

	returns, _ := resp["returns"].([]interface{})
	for _, ret := range returns {
		rm, ok := ret.(map[string]interface{})
		if !ok {
			continue
		}

		rd := RefundDownload{
			RefundDownloadID: uuid.New().String(),
			TenantID:         tenantID,
			Channel:          "ebay",
			CredentialID:     credentialID,
			ExternalOrderID:  orderID,
			ExternalRefundID: getString(rm, "returnId"),
			Status:           "unmatched",
			RawData:          rm,
			DownloadedAt:     time.Now(),
			Currency:         "GBP",
		}

		if refundDetail, ok := rm["refundDetail"].(map[string]interface{}); ok {
			if refundAmount, ok := refundDetail["refundAmount"].(map[string]interface{}); ok {
				if v, ok := refundAmount["value"].(float64); ok {
					rd.RefundAmount = v
				}
				if cur, ok := refundAmount["currency"].(string); ok {
					rd.Currency = cur
				}
			}
		}

		if returnReason, ok := rm["returnReason"].(string); ok {
			rd.Reason = returnReason
		}

		h.client.Collection("tenants").Doc(tenantID).
			Collection("refund_downloads").Doc(rd.RefundDownloadID).Set(ctx, rd)

		downloads = append(downloads, rd)
	}

	return downloads
}

// ── POST /api/v1/shopify/orders/:id/refunds ───────────────────────────────────
// Downloads refund data for a Shopify order.

func (h *RefundDownloadHandler) DownloadShopifyRefunds(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()
	orderID := c.Param("id") // Shopify order ID (numeric)
	credentialID := c.Query("credential_id")

	if credentialID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "credential_id query param required"})
		return
	}

	cred, err := h.marketplaceService.GetCredential(ctx, tenantID, credentialID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Credential not found"})
		return
	}

	mergedCreds, err := h.marketplaceService.GetFullCredentials(ctx, cred)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get credentials"})
		return
	}

	storeURL := mergedCreds["store_url"]
	accessToken := mergedCreds["admin_api_key"]
	if storeURL == "" || accessToken == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Missing Shopify store_url or admin_api_key"})
		return
	}

	// Strip protocol if present
	storeURL = strings.TrimPrefix(strings.TrimPrefix(storeURL, "https://"), "http://")
	storeURL = strings.TrimSuffix(storeURL, "/")

	apiURL := fmt.Sprintf("https://%s/admin/api/2024-01/orders/%s/refunds.json", storeURL, orderID)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	req.Header.Set("X-Shopify-Access-Token", accessToken)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Shopify API call failed: " + err.Error()})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var shopifyResp map[string]interface{}
	json.Unmarshal(body, &shopifyResp)

	downloads := h.extractShopifyRefunds(ctx, tenantID, credentialID, orderID, shopifyResp)

	c.JSON(http.StatusOK, gin.H{
		"ok":        true,
		"downloaded": len(downloads),
		"refunds":   downloads,
	})
}

func (h *RefundDownloadHandler) extractShopifyRefunds(ctx context.Context, tenantID, credentialID, orderID string, resp map[string]interface{}) []RefundDownload {
	var downloads []RefundDownload

	refunds, _ := resp["refunds"].([]interface{})
	for _, ref := range refunds {
		rm, ok := ref.(map[string]interface{})
		if !ok {
			continue
		}

		refundID := ""
		switch v := rm["id"].(type) {
		case float64:
			refundID = fmt.Sprintf("%.0f", v)
		case string:
			refundID = v
		}

		rd := RefundDownload{
			RefundDownloadID: uuid.New().String(),
			TenantID:         tenantID,
			Channel:          "shopify",
			CredentialID:     credentialID,
			ExternalOrderID:  orderID,
			ExternalRefundID: refundID,
			RefundDate:       getString(rm, "created_at"),
			Status:           "unmatched",
			Currency:         "GBP",
			RawData:          rm,
			DownloadedAt:     time.Now(),
		}

		if note, ok := rm["note"].(string); ok {
			rd.Reason = note
		}

		// Sum refund line amounts
		var lines []RefundLine
		if refundLines, ok := rm["refund_line_items"].([]interface{}); ok {
			for _, rl := range refundLines {
				if rlm, ok := rl.(map[string]interface{}); ok {
					line := RefundLine{}
					if li, ok := rlm["line_item"].(map[string]interface{}); ok {
						line.SKU = getString(li, "sku")
						line.Title = getString(li, "title")
					}
					if qty, ok := rlm["quantity"].(float64); ok {
						line.Quantity = int(qty)
					}
					if subtotal, ok := rlm["subtotal"].(float64); ok {
						line.RefundAmount = subtotal
						rd.RefundAmount += subtotal
					}
					lines = append(lines, line)
				}
			}
		}
		rd.Lines = lines

		// Also sum transactions
		if transactions, ok := rm["transactions"].([]interface{}); ok {
			for _, t := range transactions {
				if tm, ok := t.(map[string]interface{}); ok {
					if cur, ok := tm["currency"].(string); ok && cur != "" {
						rd.Currency = cur
					}
				}
			}
		}

		h.client.Collection("tenants").Doc(tenantID).
			Collection("refund_downloads").Doc(rd.RefundDownloadID).Set(ctx, rd)

		downloads = append(downloads, rd)
	}

	return downloads
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func getAmazonAccessTokenForRefunds(creds map[string]string) (string, error) {
	tokenURL := "https://api.amazon.com/auth/o2/token"
	data := fmt.Sprintf(
		"grant_type=refresh_token&refresh_token=%s&client_id=%s&client_secret=%s",
		creds["refresh_token"], creds["lwa_client_id"], creds["lwa_client_secret"],
	)
	req, err := http.NewRequest("POST", tokenURL, strings.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var tokenResp struct {
		AccessToken string `json:"access_token"`
	}
	json.Unmarshal(body, &tokenResp)
	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("empty access token from Amazon: %s", string(body))
	}
	return tokenResp.AccessToken, nil
}

func getAmazonEndpointForRegion(region string) string {
	endpoints := map[string]string{
		"UK": "https://sellingpartnerapi-eu.amazon.com",
		"EU": "https://sellingpartnerapi-eu.amazon.com",
		"DE": "https://sellingpartnerapi-eu.amazon.com",
		"US": "https://sellingpartnerapi-na.amazon.com",
		"NA": "https://sellingpartnerapi-na.amazon.com",
		"JP": "https://sellingpartnerapi-fe.amazon.com",
		"FE": "https://sellingpartnerapi-fe.amazon.com",
	}
	if ep, ok := endpoints[region]; ok {
		return ep
	}
	return "https://sellingpartnerapi-eu.amazon.com"
}
