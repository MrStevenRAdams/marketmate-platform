package handlers

// ============================================================================
// TEMU WEBHOOK EVENT SUBSCRIPTION — Step 4 of the webhook setup process
// ============================================================================
// Calls bg.tmc.message.update for each active Temu credential to subscribe
// the seller's shop to the required event codes on behalf of the seller.
//
// Prerequisites:
//   Step 1: Callback URL registered and approved in Partner Platform ✅
//   Step 2: Event topics subscribed in Partner Platform ✅
//   Step 3: Seller authorised topics in Temu Seller Center ✅ (done manually)
//   Step 4: THIS — call bg.tmc.message.update per seller access_token
//
// Event codes subscribed:
//   bg_cancel_order_status_change      — buyer cancellation requests
//   bg_aftersales_status_change        — refund/return requests
//   bg_order_status_change_event       — order status updates
//   bg_trade_logistics_address_changed — shipping address changes
//
// Routes:
//   POST /api/v1/temu/subscribe-webhook-events      — subscribe one or all credentials for tenant
//   POST /api/v1/temu/subscribe-webhook-events/all  — subscribe ALL active Temu credentials (all tenants)
// ============================================================================

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"module-a/marketplace/clients/temu"
	"module-a/models"
)

// temuWebhookEventCodes are the event codes we subscribe to for each seller.
var temuWebhookEventCodes = []string{
	"bg_cancel_order_status_change",
	"bg_aftersales_status_change",
	"bg_order_status_change_event",
	"bg_trade_logistics_address_changed",
}

// TemuSubscribeResult holds the outcome for one credential subscription attempt.
type TemuSubscribeResult struct {
	TenantID     string   `json:"tenant_id"`
	CredentialID string   `json:"credential_id"`
	AccountName  string   `json:"account_name"`
	Success      bool     `json:"success"`
	Error        string   `json:"error,omitempty"`
	EventCodes   []string `json:"event_codes,omitempty"`
	AlreadyDone  bool     `json:"already_done,omitempty"`
}

// ── POST /api/v1/temu/subscribe-webhook-events ────────────────────────────────
// Subscribes one or all Temu credentials for the authenticated tenant.
// Body (optional): { "credential_id": "..." }
// If credential_id is omitted, all active Temu credentials for the tenant are subscribed.

func (h *TemuHandler) SubscribeWebhookEvents(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	var req struct {
		CredentialID string `json:"credential_id"`
	}
	c.ShouldBindJSON(&req)

	if req.CredentialID != "" {
		// Single credential
		cred, err := h.repo.GetCredential(ctx, tenantID, req.CredentialID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "credential not found"})
			return
		}
		result := h.subscribeTemuCredential(ctx, cred)
		if !result.Success {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": result.Error, "result": result})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "result": result})
		return
	}

	// All Temu credentials for this tenant
	creds, err := h.repo.ListCredentials(ctx, tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list credentials"})
		return
	}

	var results []TemuSubscribeResult
	for _, cred := range creds {
		if cred.Channel != "temu" && cred.Channel != "temu_sandbox" {
			continue
		}
		credCopy := cred
		results = append(results, h.subscribeTemuCredential(ctx, &credCopy))
	}
	if results == nil {
		results = []TemuSubscribeResult{}
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "results": results, "count": len(results)})
}

// ── POST /api/v1/temu/subscribe-webhook-events/all ───────────────────────────
// Admin endpoint — subscribes ALL active Temu credentials across ALL tenants.

func (h *TemuHandler) SubscribeWebhookEventsAll(c *gin.Context) {
	ctx := c.Request.Context()
	results := h.SubscribeAllTemuCredentials(ctx)
	c.JSON(http.StatusOK, gin.H{"ok": true, "results": results, "count": len(results)})
}

// ── SubscribeAllTemuCredentials ───────────────────────────────────────────────
// Called at startup and from the admin endpoint.
// Iterates all active Temu credentials across all tenants and subscribes each.

func (h *TemuHandler) SubscribeAllTemuCredentials(ctx context.Context) []TemuSubscribeResult {
	creds, err := h.repo.ListAllActiveCredentials(ctx)
	if err != nil {
		log.Printf("[TemuWebhookSub] Failed to list credentials: %v", err)
		return nil
	}

	var results []TemuSubscribeResult
	for _, cred := range creds {
		if cred.Channel != "temu" && cred.Channel != "temu_sandbox" {
			continue
		}
		credCopy := cred
		result := h.subscribeTemuCredential(ctx, &credCopy)
		results = append(results, result)
		// Small delay to avoid rate limiting (110020007: too many requests)
		time.Sleep(300 * time.Millisecond)
	}
	if results == nil {
		results = []TemuSubscribeResult{}
	}
	return results
}

// ── subscribeTemuCredential ───────────────────────────────────────────────────
// Calls bg.tmc.message.update for one credential using its access_token.

func (h *TemuHandler) subscribeTemuCredential(ctx context.Context, cred *models.MarketplaceCredential) TemuSubscribeResult {
	result := TemuSubscribeResult{
		TenantID:     cred.TenantID,
		CredentialID: cred.CredentialID,
		AccountName:  cred.AccountName,
	}

	// Merge global platform keys with per-tenant access_token
	mergedCreds, err := h.marketplaceService.GetFullCredentials(ctx, cred)
	if err != nil {
		result.Error = "failed to resolve credentials: " + err.Error()
		log.Printf("[TemuWebhookSub] %s/%s: %s", cred.TenantID, cred.CredentialID, result.Error)
		return result
	}

	baseURL := mergedCreds["base_url"]
	appKey := mergedCreds["app_key"]
	appSecret := mergedCreds["app_secret"]
	accessToken := mergedCreds["access_token"]

	if appKey == "" || appSecret == "" || accessToken == "" {
		result.Error = "incomplete credentials (need app_key, app_secret, access_token)"
		log.Printf("[TemuWebhookSub] %s/%s: %s", cred.TenantID, cred.CredentialID, result.Error)
		return result
	}
	if baseURL == "" {
		baseURL = temu.TemuBaseURLEU
	}

	client := temu.NewClient(baseURL, appKey, appSecret, accessToken)

	// Call bg.tmc.message.update to subscribe this seller to all event codes
	resp, err := client.Post(map[string]interface{}{
		"type": "bg.tmc.message.update",
		"request": map[string]interface{}{
			"permitEventCodeList": temuWebhookEventCodes,
		},
	})
	if err != nil {
		result.Error = "API call failed: " + err.Error()
		log.Printf("[TemuWebhookSub] %s/%s: %s", cred.TenantID, cred.CredentialID, result.Error)
		return result
	}

	if !resp.Success {
		result.Error = resp.ErrorMsg
		// Error 110020008 means seller hasn't authorised yet in Seller Center
		// Error 110020009 means app hasn't subscribed to this event in Partner Platform
		log.Printf("[TemuWebhookSub] %s/%s: API error code=%d msg=%s",
			cred.TenantID, cred.CredentialID, resp.ErrorCode, resp.ErrorMsg)
		return result
	}

	result.Success = true
	result.EventCodes = temuWebhookEventCodes
	log.Printf("[TemuWebhookSub] ✅ Subscribed %s/%s (%s) to %d event codes",
		cred.TenantID, cred.CredentialID, cred.AccountName, len(temuWebhookEventCodes))

	// Persist subscription status on credential for visibility
	h.repo.UpdateCredentialField(ctx, cred.TenantID, cred.CredentialID, map[string]interface{}{
		"temu_webhook_subscribed":    true,
		"temu_webhook_subscribed_at": time.Now(),
	})

	return result
}
