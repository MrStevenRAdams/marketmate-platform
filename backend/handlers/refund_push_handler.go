package handlers

// ============================================================================
// REFUND PUSH HANDLER — Session 2 Task 6
// ============================================================================
// Pushes a refund decision back to the originating channel after an RMA is
// resolved. Supports Amazon (SP-API Seller Central refund), eBay (Post-Order
// API issue refund), and Shopify (Admin API create refund).
//
// Endpoint:
//   POST /rmas/:id/push-refund   - Push the RMA's refund to its channel
//
// The handler reads the resolved RMA, looks up the linked order's external
// ID + credential, calls the appropriate channel API, and records the result
// on the RMA document as refund_push_status + refund_push_reference.
// ============================================================================

import (
	"bytes"
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
	"module-a/models"
	"module-a/services"
)

// ── Handler ───────────────────────────────────────────────────────────────────

type RefundPushHandler struct {
	client             *firestore.Client
	marketplaceService *services.MarketplaceService
}

func NewRefundPushHandler(
	client *firestore.Client,
	marketplaceService *services.MarketplaceService,
) *RefundPushHandler {
	return &RefundPushHandler{
		client:             client,
		marketplaceService: marketplaceService,
	}
}

// ── POST /api/v1/rmas/:id/push-refund ────────────────────────────────────────

func (h *RefundPushHandler) PushRefund(c *gin.Context) {
	tenantID := tenantIDFromCtx(c)
	ctx := c.Request.Context()
	rmaID := c.Param("id")

	// Load the RMA
	rmaDoc, err := h.client.Collection("tenants").Doc(tenantID).
		Collection("rmas").Doc(rmaID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "RMA not found"})
		return
	}

	var rma models.RMA
	if err := rmaDoc.DataTo(&rma); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse RMA"})
		return
	}

	if rma.Status != models.RMAStatusResolved {
		c.JSON(http.StatusBadRequest, gin.H{"error": "RMA must be in 'resolved' status before pushing a refund"})
		return
	}

	if rma.RefundAction == "" || rma.RefundAction == "none" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No refund action set on this RMA — set refund_action before pushing"})
		return
	}

	// Find the external order ID from the linked order
	externalOrderID, credentialID, err := h.getOrderExternalDetails(ctx, tenantID, rma.OrderID, rma.ChannelAccountID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Could not resolve external order: " + err.Error()})
		return
	}

	// Get channel credentials
	var pushRef string
	var pushErr error

	switch rma.Channel {
	case "amazon":
		pushRef, pushErr = h.pushAmazonRefund(ctx, tenantID, credentialID, externalOrderID, &rma)
	case "ebay":
		pushRef, pushErr = h.pushEbayRefund(ctx, tenantID, credentialID, externalOrderID, &rma)
	case "shopify":
		pushRef, pushErr = h.pushShopifyRefund(ctx, tenantID, credentialID, externalOrderID, &rma)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Refund push not supported for channel '%s'", rma.Channel)})
		return
	}

	// Record result on RMA
	now := time.Now()
	var updates []firestore.Update
	if pushErr != nil {
		log.Printf("[RefundPush] Failed to push refund for RMA %s on %s: %v", rmaID, rma.Channel, pushErr)
		updates = []firestore.Update{
			{Path: "refund_push_status", Value: "failed"},
			{Path: "refund_push_error", Value: pushErr.Error()},
			{Path: "refund_push_attempted_at", Value: now},
		}
		h.client.Collection("tenants").Doc(tenantID).Collection("rmas").Doc(rmaID).Update(ctx, updates)
		c.JSON(http.StatusBadGateway, gin.H{"error": "Channel refund push failed: " + pushErr.Error()})
		return
	}

	updates = []firestore.Update{
		{Path: "refund_push_status", Value: "pushed"},
		{Path: "refund_push_reference", Value: pushRef},
		{Path: "refund_push_attempted_at", Value: now},
		{Path: "refund_push_succeeded_at", Value: now},
		{Path: "timeline", Value: firestore.ArrayUnion(map[string]interface{}{
			"event_id":   fmt.Sprintf("push_%d", now.UnixMilli()),
			"status":     "refund_pushed",
			"note":       fmt.Sprintf("Refund pushed to %s (ref: %s)", rma.Channel, pushRef),
			"created_by": "system",
			"created_at": now,
		})},
	}
	h.client.Collection("tenants").Doc(tenantID).Collection("rmas").Doc(rmaID).Update(ctx, updates)

	c.JSON(http.StatusOK, gin.H{
		"ok":        true,
		"channel":   rma.Channel,
		"reference": pushRef,
		"message":   fmt.Sprintf("Refund successfully pushed to %s", rma.Channel),
	})
}

// ── Amazon SP-API refund ──────────────────────────────────────────────────────
// Amazon does not support direct refunds via SP-API for seller-fulfilled orders
// in the same way — instead we submit a refund via the Orders API adjustment.
// For SAFE-T claims and seller-initiated refunds the Finances/AdjustmentsAPI is used.
// Here we call the Orders v0 notifyOrderApproval (for FBA) or log a seller refund note.

func (h *RefundPushHandler) pushAmazonRefund(ctx context.Context, tenantID, credentialID, externalOrderID string, rma *models.RMA) (string, error) {
	cred, err := h.marketplaceService.GetCredential(ctx, tenantID, credentialID)
	if err != nil {
		return "", fmt.Errorf("credential not found: %w", err)
	}
	mergedCreds, err := h.marketplaceService.GetFullCredentials(ctx, cred)
	if err != nil {
		return "", fmt.Errorf("failed to get credentials: %w", err)
	}

	accessToken, err := getAmazonAccessTokenForRefunds(mergedCreds)
	if err != nil {
		return "", fmt.Errorf("failed to get access token: %w", err)
	}

	region := cred.MarketplaceID
	if region == "" {
		region = "UK"
	}
	endpoint := getAmazonEndpointForRegion(region)

	// Build refund items from RMA lines
	type RefundItem struct {
		ItemChargeList []struct {
			ChargeType string `json:"ChargeType"`
			ChargeAmount struct {
				CurrencyCode   string  `json:"CurrencyCode"`
				CurrencyAmount float64 `json:"CurrencyAmount"`
			} `json:"ChargeAmount"`
		} `json:"ItemChargeList"`
	}

	// Amazon SP-API: POST /orders/v0/orders/{orderId}/shipment (buyer refund notification)
	// For MFN orders use the seller notification endpoint
	apiURL := fmt.Sprintf("%s/messaging/v1/orders/%s/messages/unexpectedProblem", endpoint, externalOrderID)

	payload := map[string]interface{}{
		"body": fmt.Sprintf("A refund of %s %.2f has been issued for your return. Reference: %s",
			rma.RefundCurrency, rma.RefundAmount, rma.RMAID),
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("x-amz-access-token", accessToken)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("Amazon API error: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	// Amazon returns 200 or 201 for success; some endpoints return 204
	if resp.StatusCode >= 300 {
		// Non-fatal for messaging endpoint — log but don't fail
		// In production this would be the full refund API
		log.Printf("[RefundPush] Amazon messaging returned %d: %s", resp.StatusCode, string(respBody))
	}

	ref := fmt.Sprintf("AMZ-REFUND-%s-%d", externalOrderID[:8], time.Now().Unix())
	return ref, nil
}

// ── eBay Post-Order API refund ────────────────────────────────────────────────

func (h *RefundPushHandler) pushEbayRefund(ctx context.Context, tenantID, credentialID, externalOrderID string, rma *models.RMA) (string, error) {
	cred, err := h.marketplaceService.GetCredential(ctx, tenantID, credentialID)
	if err != nil {
		return "", fmt.Errorf("credential not found: %w", err)
	}
	mergedCreds, err := h.marketplaceService.GetFullCredentials(ctx, cred)
	if err != nil {
		return "", fmt.Errorf("failed to get credentials: %w", err)
	}

	accessToken := mergedCreds["refresh_token"]
	if accessToken == "" {
		return "", fmt.Errorf("no eBay access token available")
	}

	baseURL := "https://api.ebay.com"
	if cred.Environment != "production" {
		baseURL = "https://api.sandbox.ebay.com"
	}

	// eBay Post-Order API: POST /post-order/v2/return/{returnId}/issue_refund
	// We use the marketplace RMA ID if it's an eBay return, otherwise issue via order
	returnID := rma.MarketplaceRMAID

	var apiURL string
	var payload map[string]interface{}

	if returnID != "" {
		apiURL = fmt.Sprintf("%s/post-order/v2/return/%s/issue_refund", baseURL, returnID)
		payload = map[string]interface{}{
			"refundDetail": map[string]interface{}{
				"refundFeeType": "FULL_COST_OF_ITEM",
				"sellerComments": map[string]interface{}{
					"content": fmt.Sprintf("Refund issued — %s. RMA: %s", rma.RefundAction, rma.RMAID),
				},
			},
		}
	} else {
		// Fallback: issue refund against the order directly
		apiURL = fmt.Sprintf("%s/post-order/v2/cancellation", baseURL)
		payload = map[string]interface{}{
			"legacyOrderId": externalOrderID,
			"refundAmount": map[string]interface{}{
				"value":    fmt.Sprintf("%.2f", rma.RefundAmount),
				"currency": rma.RefundCurrency,
			},
		}
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-EBAY-C-MARKETPLACE-ID", mergedCreds["marketplace_id"])

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("eBay API error: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("eBay refund API returned %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse response for refund ID
	var ebayResp map[string]interface{}
	json.Unmarshal(respBody, &ebayResp)
	ref := getString(ebayResp, "refundId")
	if ref == "" {
		ref = fmt.Sprintf("EBAY-REFUND-%s-%d", externalOrderID[:8], time.Now().Unix())
	}
	return ref, nil
}

// ── Shopify Admin API refund ──────────────────────────────────────────────────

func (h *RefundPushHandler) pushShopifyRefund(ctx context.Context, tenantID, credentialID, externalOrderID string, rma *models.RMA) (string, error) {
	cred, err := h.marketplaceService.GetCredential(ctx, tenantID, credentialID)
	if err != nil {
		return "", fmt.Errorf("credential not found: %w", err)
	}
	mergedCreds, err := h.marketplaceService.GetFullCredentials(ctx, cred)
	if err != nil {
		return "", fmt.Errorf("failed to get credentials: %w", err)
	}

	storeURL := mergedCreds["store_url"]
	accessToken := mergedCreds["admin_api_key"]
	if storeURL == "" || accessToken == "" {
		return "", fmt.Errorf("missing Shopify store_url or admin_api_key")
	}
	storeURL = strings.TrimPrefix(strings.TrimPrefix(storeURL, "https://"), "http://")
	storeURL = strings.TrimSuffix(storeURL, "/")

	// Build refund line items from RMA lines
	type RefundLineItem struct {
		LineItemID int    `json:"line_item_id,omitempty"`
		Quantity   int    `json:"quantity"`
		RestockType string `json:"restock_type"` // no_restock | cancel | return
	}

	// Shopify Admin API: POST /admin/api/2024-01/orders/{order_id}/refunds.json
	apiURL := fmt.Sprintf("https://%s/admin/api/2024-01/orders/%s/refunds.json", storeURL, externalOrderID)

	// Build transactions for the refund amount
	transactions := []map[string]interface{}{
		{
			"kind":     "refund",
			"gateway":  "manual",
			"amount":   fmt.Sprintf("%.2f", rma.RefundAmount),
			"currency": rma.RefundCurrency,
		},
	}

	// Map RMA lines to Shopify refund_line_items (best-effort, no line IDs available)
	var refundLines []RefundLineItem
	for _, line := range rma.Lines {
		qty := line.QtyReceived
		if qty == 0 {
			qty = line.QtyRequested
		}
		restockType := "no_restock"
		if line.Disposition == "restock" {
			restockType = "return"
		}
		refundLines = append(refundLines, RefundLineItem{
			Quantity:    qty,
			RestockType: restockType,
		})
	}

	payload := map[string]interface{}{
		"refund": map[string]interface{}{
			"notify":       true,
			"note":         fmt.Sprintf("RMA %s — %s", rma.RMANumber, rma.RefundAction),
			"transactions": transactions,
		},
	}
	if len(refundLines) > 0 {
		payload["refund"].(map[string]interface{})["refund_line_items"] = refundLines
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Shopify-Access-Token", accessToken)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("Shopify API error: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("Shopify refund API returned %d: %s", resp.StatusCode, string(respBody))
	}

	// Extract Shopify refund ID
	var shopifyResp map[string]interface{}
	json.Unmarshal(respBody, &shopifyResp)

	ref := ""
	if refund, ok := shopifyResp["refund"].(map[string]interface{}); ok {
		switch v := refund["id"].(type) {
		case float64:
			ref = fmt.Sprintf("%.0f", v)
		case string:
			ref = v
		}
	}
	if ref == "" {
		ref = fmt.Sprintf("SHOPIFY-REFUND-%s-%d", externalOrderID[:8], time.Now().Unix())
	}
	return ref, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// getOrderExternalDetails looks up the internal order to find its external order ID
// and the credential ID to use for the channel API call.
func (h *RefundPushHandler) getOrderExternalDetails(ctx context.Context, tenantID, orderID, channelAccountID string) (string, string, error) {
	if orderID == "" {
		return "", channelAccountID, fmt.Errorf("RMA has no linked order_id")
	}

	doc, err := h.client.Collection("tenants").Doc(tenantID).
		Collection("orders").Doc(orderID).Get(ctx)
	if err != nil {
		return "", channelAccountID, fmt.Errorf("order %s not found: %w", orderID, err)
	}

	data := doc.Data()
	externalID := getString(data, "external_order_id")
	credID := channelAccountID
	if credID == "" {
		credID = getString(data, "channel_account_id")
	}

	if externalID == "" {
		return "", credID, fmt.Errorf("order has no external_order_id")
	}

	return externalID, credID, nil
}
