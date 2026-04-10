package handlers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"

	"module-a/models"
	"module-a/services"
)

// ============================================================================
// BILLING HANDLER
// ============================================================================

type BillingHandler struct {
	client              *firestore.Client
	usageService        *services.UsageService
	templateService     *services.TemplateService
	notificationHandler *NotificationHandler
}

func NewBillingHandler(client *firestore.Client, usageSvc *services.UsageService) *BillingHandler {
	return &BillingHandler{client: client, usageService: usageSvc}
}

// SetTemplateService wires the shared TemplateService so billing events can send email alerts.
func (h *BillingHandler) SetTemplateService(svc *services.TemplateService) {
	h.templateService = svc
}

// SetNotificationHandler wires the shared NotificationHandler for in-app alerts.
func (h *BillingHandler) SetNotificationHandler(nh *NotificationHandler) {
	h.notificationHandler = nh
}

// ============================================================================
// PLAN CATALOGUE
// ============================================================================

// GetPlans GET /api/v1/billing/plans
// Returns all available plans. No auth required — used on the pricing/signup page.
func (h *BillingHandler) GetPlans(c *gin.Context) {
	ctx := c.Request.Context()

	iter := h.client.Collection("system").Doc("plans").
		Collection("plans").
		Where("is_active", "==", true).
		OrderBy("sort_order", firestore.Asc).
		Documents(ctx)
	defer iter.Stop()

	var plans []models.Plan
	for {
		snap, err := iter.Next()
		if err != nil {
			break
		}
		var p models.Plan
		snap.DataTo(&p)
		plans = append(plans, p)
	}

	// Seed defaults if none exist
	if len(plans) == 0 {
		plans = defaultPlans()
		go h.seedPlans(plans)
	}

	c.JSON(http.StatusOK, gin.H{"plans": plans})
}

// ============================================================================
// CURRENT BILLING STATUS
// ============================================================================

// GetBillingStatus GET /api/v1/billing/status
// Returns current plan, ledger, and billing record for the tenant.
func (h *BillingHandler) GetBillingStatus(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	// Tenant record
	tSnap, err := h.client.Collection("tenants").Doc(tenantID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load tenant"})
		return
	}
	var tenant models.Tenant
	tSnap.DataTo(&tenant)

	// Current ledger
	ledger, _ := h.usageService.GetCurrentLedger(ctx, tenantID)

	// Billing record
	var billing *models.BillingRecord
	bSnap, err := h.client.Collection("tenants").Doc(tenantID).
		Collection("billing").Doc("current").Get(ctx)
	if err == nil {
		var b models.BillingRecord
		bSnap.DataTo(&b)
		billing = &b
	}

	// Plan override
	var override *models.PlanOverride
	oSnap, err := h.client.Collection("tenants").Doc(tenantID).
		Collection("plan_overrides").Doc("current").Get(ctx)
	if err == nil {
		var o models.PlanOverride
		oSnap.DataTo(&o)
		override = &o
	}

	// Plan definition
	planSnap, _ := h.client.Collection("system").Doc("plans").
		Collection("plans").Doc(string(tenant.PlanID)).Get(ctx)
	var plan *models.Plan
	if planSnap != nil && planSnap.Exists() {
		var p models.Plan
		planSnap.DataTo(&p)
		plan = &p
	}

	c.JSON(http.StatusOK, gin.H{
		"tenant":        tenant,
		"plan":          plan,
		"plan_override": override,
		"ledger":        ledger,
		"billing":       billing,
	})
}

// ============================================================================
// USAGE DETAIL
// ============================================================================

// GetUsageSummary GET /api/v1/billing/usage?period=2026-02
func (h *BillingHandler) GetUsageSummary(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	period := c.Query("period")
	if period == "" {
		period = time.Now().UTC().Format("2006-01")
	}

	ctx := c.Request.Context()
	ledger, err := h.usageService.GetLedger(ctx, tenantID, period)
	if err != nil || ledger == nil {
		c.JSON(http.StatusOK, gin.H{"ledger": nil, "period": period})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ledger": ledger,
		"period": period,
	})
}

// GetAuditLog GET /api/v1/billing/audit?type=api_call&limit=100
func (h *BillingHandler) GetAuditLog(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	filter := services.AuditLogFilter{
		Type:  models.UsageEventType(c.Query("type")),
		Limit: 100,
	}

	ctx := c.Request.Context()
	entries, err := h.usageService.GetAuditLog(ctx, tenantID, filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load audit log"})
		return
	}

	if entries == nil {
		entries = []models.AuditLogEntry{}
	}

	c.JSON(http.StatusOK, gin.H{
		"entries": entries,
		"count":   len(entries),
	})
}

// ============================================================================
// PAYPAL INTEGRATION
// ============================================================================

// CreatePayPalSubscription POST /api/v1/billing/paypal/subscribe
// Returns a PayPal subscription approval URL the frontend redirects to.
func (h *BillingHandler) CreatePayPalSubscription(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req struct {
		PlanID string `json:"plan_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Look up PayPal plan ID from environment
	// These are set when you create subscription plans in the PayPal dashboard
	paypalPlanID := paypalPlanIDForPlan(req.PlanID)
	if paypalPlanID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no PayPal plan configured for " + req.PlanID})
		return
	}

	// Get PayPal access token
	accessToken, err := getPayPalAccessToken()
	if err != nil {
		log.Printf("[billing] PayPal auth failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "payment provider unavailable"})
		return
	}

	// Create subscription via PayPal API
	baseURL := paypalBaseURL()
	subscriptionPayload := map[string]interface{}{
		"plan_id": paypalPlanID,
		"application_context": map[string]interface{}{
			"brand_name":          "MarketMate",
			"locale":              "en-GB",
			"shipping_preference": "NO_SHIPPING",
			"user_action":         "SUBSCRIBE_NOW",
			"return_url":          frontendURL() + "/settings/billing/success?tenant=" + tenantID,
			"cancel_url":          frontendURL() + "/settings/billing/cancel",
		},
		"custom_id": tenantID, // We use this in the webhook to identify the tenant
	}

	body, _ := json.Marshal(subscriptionPayload)
	req2, _ := http.NewRequestWithContext(c.Request.Context(), "POST",
		baseURL+"/v1/billing/subscriptions", nil)
	req2.Body = io.NopCloser(
		func() io.Reader {
			return ioReaderFromBytes(body)
		}(),
	)
	req2.Header.Set("Authorization", "Bearer "+accessToken)
	req2.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req2)
	if err != nil || resp.StatusCode >= 400 {
		log.Printf("[billing] PayPal subscription creation failed: status %v err %v", resp.StatusCode, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create subscription"})
		return
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	// Extract approval URL from PayPal response links
	approvalURL := ""
	if links, ok := result["links"].([]interface{}); ok {
		for _, link := range links {
			if l, ok := link.(map[string]interface{}); ok {
				if l["rel"] == "approve" {
					approvalURL = l["href"].(string)
				}
			}
		}
	}

	if approvalURL == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no approval URL from PayPal"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"approval_url":    approvalURL,
		"subscription_id": result["id"],
	})
}

// PayPalWebhook POST /webhooks/paypal
// Receives PayPal subscription lifecycle events.
// No auth middleware — verified by signature.
func (h *BillingHandler) PayPalWebhook(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read body"})
		return
	}

	// Verify webhook signature
	webhookSecret := os.Getenv("PAYPAL_WEBHOOK_SECRET")
	if webhookSecret != "" {
		sig := c.GetHeader("Paypal-Transmission-Sig")
		if !verifyPayPalSignature(body, sig, webhookSecret) {
			log.Printf("[billing] PayPal webhook signature verification failed")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid signature"})
			return
		}
	}

	var event struct {
		EventType string          `json:"event_type"`
		Resource  json.RawMessage `json:"resource"`
	}
	if err := json.Unmarshal(body, &event); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
		return
	}

	log.Printf("[billing] PayPal webhook: %s", event.EventType)
	ctx := c.Request.Context()

	switch event.EventType {
	case "BILLING.SUBSCRIPTION.ACTIVATED":
		h.handleSubscriptionActivated(ctx, event.Resource)
	case "BILLING.SUBSCRIPTION.CANCELLED", "BILLING.SUBSCRIPTION.EXPIRED":
		h.handleSubscriptionCancelled(ctx, event.Resource)
	case "BILLING.SUBSCRIPTION.SUSPENDED":
		h.handleSubscriptionSuspended(ctx, event.Resource)
	case "PAYMENT.SALE.COMPLETED":
		h.handlePaymentCompleted(ctx, event.Resource)
	case "PAYMENT.SALE.DENIED", "PAYMENT.SALE.REVERSED":
		h.handlePaymentFailed(ctx, event.Resource)
	}

	c.JSON(http.StatusOK, gin.H{"received": true})
}

func (h *BillingHandler) handleSubscriptionActivated(ctx context.Context, raw json.RawMessage) {
	var sub struct {
		ID       string `json:"id"`
		PlanID   string `json:"plan_id"`
		CustomID string `json:"custom_id"` // This is our tenant_id
		Subscriber struct {
			EmailAddress string `json:"email_address"`
			Name struct {
				GivenName string `json:"given_name"`
				Surname   string `json:"surname"`
			} `json:"name"`
		} `json:"subscriber"`
		BillingInfo struct {
			NextBillingTime string `json:"next_billing_time"`
		} `json:"billing_info"`
	}
	if err := json.Unmarshal(raw, &sub); err != nil {
		log.Printf("[billing] failed to parse subscription activation: %v", err)
		return
	}

	tenantID := sub.CustomID
	if tenantID == "" {
		log.Printf("[billing] subscription %s has no custom_id (tenant_id)", sub.ID)
		return
	}

	// Determine our plan ID from the PayPal plan ID
	ourPlanID := planIDForPayPalPlan(sub.PlanID)

	now := time.Now().UTC()
	var nextBilling *time.Time
	if sub.BillingInfo.NextBillingTime != "" {
		t, err := time.Parse(time.RFC3339, sub.BillingInfo.NextBillingTime)
		if err == nil {
			nextBilling = &t
		}
	}

	batch := h.client.Batch()

	// Compute monthly credits reset (1 month from activation)
	nextResetAt := now.AddDate(0, 1, 0)

	// Update tenant plan status AND subscription_plan (used by GetAICredits for credit allocation)
	// Also reset monthly_credits_used so the new plan's allowance starts fresh.
	batch.Update(h.client.Collection("tenants").Doc(tenantID), []firestore.Update{
		{Path: "plan_id", Value: ourPlanID},
		{Path: "plan_status", Value: string(models.PlanStatusActive)},
		{Path: "subscription_plan", Value: ourPlanID},
		{Path: "monthly_credits_used", Value: int64(0)},
		{Path: "monthly_credits_reset_at", Value: nextResetAt},
		{Path: "trial_ends_at", Value: nil},
		{Path: "plan_started_at", Value: now},
		{Path: "updated_at", Value: now},
	})

	// Upsert billing record
	batch.Set(
		h.client.Collection("tenants").Doc(tenantID).Collection("billing").Doc("current"),
		models.BillingRecord{
			TenantID:             tenantID,
			PayPalSubscriptionID: sub.ID,
			PayPalPlanID:         sub.PlanID,
			BillingEmail:         sub.Subscriber.EmailAddress,
			BillingName:          sub.Subscriber.Name.GivenName + " " + sub.Subscriber.Name.Surname,
			NextBillingAt:        nextBilling,
			LastPaymentAt:        &now,
			UpdatedAt:            now,
		},
	)

	batch.Commit(ctx)
	log.Printf("[billing] subscription activated: tenant=%s plan=%s", tenantID, ourPlanID)
}

func (h *BillingHandler) handleSubscriptionCancelled(ctx context.Context, raw json.RawMessage) {
	var sub struct {
		ID       string `json:"id"`
		CustomID string `json:"custom_id"`
	}
	json.Unmarshal(raw, &sub)
	if sub.CustomID == "" {
		return
	}
	h.client.Collection("tenants").Doc(sub.CustomID).Update(ctx, []firestore.Update{
		{Path: "plan_status", Value: string(models.PlanStatusCancelled)},
		{Path: "subscription_plan", Value: ""},
		{Path: "updated_at", Value: time.Now().UTC()},
	})
	log.Printf("[billing] subscription cancelled: tenant=%s", sub.CustomID)
}

func (h *BillingHandler) handleSubscriptionSuspended(ctx context.Context, raw json.RawMessage) {
	var sub struct {
		CustomID string `json:"custom_id"`
	}
	json.Unmarshal(raw, &sub)
	if sub.CustomID == "" {
		return
	}
	h.client.Collection("tenants").Doc(sub.CustomID).Update(ctx, []firestore.Update{
		{Path: "plan_status", Value: string(models.PlanStatusSuspended)},
		{Path: "updated_at", Value: time.Now().UTC()},
	})
}

func (h *BillingHandler) handlePaymentCompleted(ctx context.Context, raw json.RawMessage) {
	var payment struct {
		Amount struct {
			Total    string `json:"total"`
			Currency string `json:"currency"`
		} `json:"amount"`
		BillingAgreementID string `json:"billing_agreement_id"`
	}
	json.Unmarshal(raw, &payment)
	log.Printf("[billing] payment completed for subscription %s: %s %s",
		payment.BillingAgreementID, payment.Amount.Total, payment.Amount.Currency)
	// TODO: find tenant by subscription ID and update billing record
}

func (h *BillingHandler) handlePaymentFailed(ctx context.Context, raw json.RawMessage) {
	var payment struct {
		BillingAgreementID string `json:"billing_agreement_id"`
	}
	json.Unmarshal(raw, &payment)
	log.Printf("[billing] payment failed for subscription %s", payment.BillingAgreementID)
	// TODO: update tenant to past_due, send notification
}

// ============================================================================
// ADMIN — PLAN OVERRIDES (Sales team use)
// ============================================================================

// SetPlanOverride PUT /api/v1/admin/tenants/:tenant_id/plan-override
// Allows internal admin users to set negotiated pricing for a tenant.
// Protected by internal admin auth — not tenant auth.
func (h *BillingHandler) SetPlanOverride(c *gin.Context) {
	targetTenantID := c.Param("tenant_id")
	callerUserID := c.GetString("user_id")
	ctx := c.Request.Context()

	var req models.PlanOverride
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	req.TenantID = targetTenantID
	req.SetBy = callerUserID
	req.SetAt = time.Now().UTC()

	_, err := h.client.Collection("tenants").Doc(targetTenantID).
		Collection("plan_overrides").Doc("current").Set(ctx, req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to set override"})
		return
	}

	log.Printf("[billing] plan override set for tenant=%s by user=%s", targetTenantID, callerUserID)
	c.JSON(http.StatusOK, gin.H{
		"message":   "Plan override saved",
		"tenant_id": targetTenantID,
		"override":  req,
	})
}

// GetPlanOverride GET /api/v1/admin/tenants/:tenant_id/plan-override
func (h *BillingHandler) GetPlanOverride(c *gin.Context) {
	targetTenantID := c.Param("tenant_id")
	ctx := c.Request.Context()

	snap, err := h.client.Collection("tenants").Doc(targetTenantID).
		Collection("plan_overrides").Doc("current").Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"override": nil})
		return
	}

	var override models.PlanOverride
	snap.DataTo(&override)
	c.JSON(http.StatusOK, gin.H{"override": override})
}

// UpdateCreditRates PUT /api/v1/admin/credit-rates
// Allows admins to adjust the credit cost per event type.
func (h *BillingHandler) UpdateCreditRates(c *gin.Context) {
	callerUserID := c.GetString("user_id")
	ctx := c.Request.Context()

	var rates models.CreditRates
	if err := c.ShouldBindJSON(&rates); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	rates.UpdatedBy = callerUserID

	if err := h.usageService.UpdateRates(ctx, rates); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update rates"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Credit rates updated", "rates": rates})
}

// GetCreditRates GET /api/v1/admin/credit-rates
func (h *BillingHandler) GetCreditRates(c *gin.Context) {
	ctx := c.Request.Context()
	snap, err := h.client.Collection("system").Doc("credit_rates").Get(ctx)
	if err != nil {
		defaults := models.DefaultCreditRates()
		c.JSON(http.StatusOK, gin.H{"rates": defaults, "source": "defaults"})
		return
	}
	var rates models.CreditRates
	snap.DataTo(&rates)
	c.JSON(http.StatusOK, gin.H{"rates": rates, "source": "configured"})
}

// ============================================================================
// HELPERS
// ============================================================================

func paypalBaseURL() string {
	if os.Getenv("PAYPAL_SANDBOX") == "true" {
		return "https://api-m.sandbox.paypal.com"
	}
	return "https://api-m.paypal.com"
}

func getPayPalAccessToken() (string, error) {
	clientID := os.Getenv("PAYPAL_CLIENT_ID")
	secret := os.Getenv("PAYPAL_CLIENT_SECRET")

	req, _ := http.NewRequest("POST", paypalBaseURL()+"/v1/oauth2/token", nil)
	req.SetBasicAuth(clientID, secret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Body = io.NopCloser(ioReaderFromBytes([]byte("grant_type=client_credentials")))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		AccessToken string `json:"access_token"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	return result.AccessToken, nil
}

func paypalPlanIDForPlan(ourPlanID string) string {
	// These env vars are set after you create plans in the PayPal dashboard
	mapping := map[string]string{
		"starter_s": os.Getenv("PAYPAL_PLAN_STARTER_S"),
		"starter_m": os.Getenv("PAYPAL_PLAN_STARTER_M"),
		"starter_l": os.Getenv("PAYPAL_PLAN_STARTER_L"),
		"premium":   os.Getenv("PAYPAL_PLAN_PREMIUM"),
	}
	return mapping[ourPlanID]
}

func planIDForPayPalPlan(paypalPlanID string) string {
	// Reverse lookup
	plans := map[string]string{
		os.Getenv("PAYPAL_PLAN_STARTER_S"): "starter_s",
		os.Getenv("PAYPAL_PLAN_STARTER_M"): "starter_m",
		os.Getenv("PAYPAL_PLAN_STARTER_L"): "starter_l",
		os.Getenv("PAYPAL_PLAN_PREMIUM"):   "premium",
		os.Getenv("PAYPAL_PLAN_ENTERPRISE"): "enterprise",
	}
	if id, ok := plans[paypalPlanID]; ok {
		return id
	}
	return "starter_s"
}

func verifyPayPalSignature(body []byte, sig, secret string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(sig))
}

func ioReaderFromBytes(b []byte) io.Reader {
	return &bytesReader{data: b}
}

type bytesReader struct {
	data   []byte
	offset int
}

func (r *bytesReader) Read(p []byte) (n int, err error) {
	if r.offset >= len(r.data) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.offset:])
	r.offset += n
	return n, nil
}

func defaultPlans() []models.Plan {
	credits10k := int64(10000)
	credits50k := int64(50000)
	credits150k := int64(150000)
	perOrder := 0.10
	gmvPct := 1.0

	return []models.Plan{
		{
			PlanID: models.PlanStarterS, Name: "Starter S",
			BillingModel: models.BillingModelCredits, CreditsPerMonth: &credits10k,
			PriceGBP: 29.00, IsActive: true, SortOrder: 1,
		},
		{
			PlanID: models.PlanStarterM, Name: "Starter M",
			BillingModel: models.BillingModelCredits, CreditsPerMonth: &credits50k,
			PriceGBP: 79.00, IsActive: true, SortOrder: 2,
		},
		{
			PlanID: models.PlanStarterL, Name: "Starter L",
			BillingModel: models.BillingModelCredits, CreditsPerMonth: &credits150k,
			PriceGBP: 149.00, IsActive: true, SortOrder: 3,
		},
		{
			PlanID: models.PlanPremium, Name: "Premium",
			BillingModel: models.BillingModelPerOrder, CreditsPerMonth: nil,
			PriceGBP: 250.00, PerOrderGBP: &perOrder, IsActive: true, SortOrder: 4,
		},
		{
			PlanID: models.PlanEnterprise, Name: "Enterprise",
			BillingModel: models.BillingModelGMV, CreditsPerMonth: nil,
			PriceGBP: 499.00, GMVPercent: &gmvPct, IsActive: true, SortOrder: 5,
		},
	}
}

func (h *BillingHandler) seedPlans(plans []models.Plan) {
	ctx := ctxWithTimeout(10)
	for _, p := range plans {
		p.UpdatedAt = time.Now().UTC()
		p.UpdatedBy = "system"
		h.client.Collection("system").Doc("plans").
			Collection("plans").Doc(string(p.PlanID)).Set(ctx, p)
	}
}

func ctxWithTimeout(seconds int) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(seconds)*time.Second)
	// Cancel is intentionally leaked here — the timeout will fire and clean up.
	// For long-running seedPlans this is acceptable; callers should not rely on cancellation.
	_ = cancel
	return ctx
}

// ============================================================================
// STRIPE INTEGRATION (4.4)
// ============================================================================
// Stripe is an alternative to PayPal. Both providers update subscription_plan
// on the tenant document via their respective webhooks.
//
// Required env vars:
//   STRIPE_SECRET_KEY        — sk_live_... or sk_test_...
//   STRIPE_WEBHOOK_SECRET    — whsec_...
//   STRIPE_PRICE_STARTER_S   — price_...
//   STRIPE_PRICE_STARTER_M   — price_...
//   STRIPE_PRICE_STARTER_L   — price_...
//   STRIPE_PRICE_PREMIUM     — price_...
//   STRIPE_PRICE_ENTERPRISE  — price_...

// CreateStripeCheckoutSession POST /api/v1/billing/stripe/checkout
// Creates a Stripe Checkout session and returns the checkout URL.
func (h *BillingHandler) CreateStripeCheckoutSession(c *gin.Context) {
	tenantID := c.GetString("tenant_id")

	var req struct {
		PlanID string `json:"plan_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	stripeKey := os.Getenv("STRIPE_SECRET_KEY")
	if stripeKey == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Stripe not configured"})
		return
	}

	priceID := stripePriceIDForPlan(req.PlanID)
	if priceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no Stripe price configured for " + req.PlanID})
		return
	}

	baseURL := frontendURL()
	payload := map[string]interface{}{
		"mode":                "subscription",
		"payment_method_types": []string{"card"},
		"line_items": []map[string]interface{}{
			{"price": priceID, "quantity": 1},
		},
		"success_url":    baseURL + "/settings/billing/success?session_id={CHECKOUT_SESSION_ID}&tenant=" + tenantID,
		"cancel_url":     baseURL + "/settings/billing/cancel",
		"client_reference_id": tenantID, // Used in webhook to identify tenant
		"metadata": map[string]string{
			"tenant_id": tenantID,
			"plan_id":   req.PlanID,
		},
	}

	body, _ := json.Marshal(payload)
	stripeReq, _ := http.NewRequestWithContext(c.Request.Context(), "POST",
		"https://api.stripe.com/v1/checkout/sessions",
		ioReaderFromBytes(body))
	stripeReq.SetBasicAuth(stripeKey, "")
	stripeReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(stripeReq)
	if err != nil || resp.StatusCode >= 400 {
		log.Printf("[billing] Stripe checkout creation failed: err=%v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create checkout session"})
		return
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	checkoutURL, _ := result["url"].(string)
	if checkoutURL == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no checkout URL from Stripe"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"checkout_url": checkoutURL,
		"session_id":   result["id"],
	})
}

// StripeWebhook POST /webhooks/stripe
// Handles Stripe subscription lifecycle events.
// No auth middleware — verified by Stripe-Signature header.
func (h *BillingHandler) StripeWebhook(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read body"})
		return
	}

	// Verify webhook signature
	webhookSecret := os.Getenv("STRIPE_WEBHOOK_SECRET")
	if webhookSecret != "" {
		sig := c.GetHeader("Stripe-Signature")
		if !verifyStripeSignature(body, sig, webhookSecret) {
			log.Printf("[billing] Stripe webhook signature verification failed")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid signature"})
			return
		}
	}

	var event struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &event); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
		return
	}

	log.Printf("[billing] Stripe webhook: %s", event.Type)
	ctx := c.Request.Context()

	switch event.Type {
	case "checkout.session.completed":
		h.handleStripeCheckoutCompleted(ctx, event.Data)
	case "customer.subscription.updated":
		h.handleStripeSubscriptionUpdated(ctx, event.Data)
	case "customer.subscription.deleted":
		h.handleStripeSubscriptionDeleted(ctx, event.Data)
	case "invoice.payment_failed":
		h.handleStripePaymentFailed(ctx, event.Data)
	}

	c.JSON(http.StatusOK, gin.H{"received": true})
}

func (h *BillingHandler) handleStripeCheckoutCompleted(ctx context.Context, raw json.RawMessage) {
	var session struct {
		ID                  string `json:"id"`
		ClientReferenceID   string `json:"client_reference_id"` // our tenant_id
		Subscription        string `json:"subscription"`
		Metadata            struct {
			TenantID string `json:"tenant_id"`
			PlanID   string `json:"plan_id"`
		} `json:"metadata"`
		CustomerDetails struct {
			Email string `json:"email"`
			Name  string `json:"name"`
		} `json:"customer_details"`
	}
	var wrapper struct {
		Object json.RawMessage `json:"object"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		log.Printf("[billing] Stripe checkout parse error: %v", err)
		return
	}
	if err := json.Unmarshal(wrapper.Object, &session); err != nil {
		log.Printf("[billing] Stripe checkout session parse error: %v", err)
		return
	}

	tenantID := session.ClientReferenceID
	if tenantID == "" {
		tenantID = session.Metadata.TenantID
	}
	if tenantID == "" {
		log.Printf("[billing] Stripe checkout %s has no tenant reference", session.ID)
		return
	}

	planID := session.Metadata.PlanID
	if planID == "" {
		log.Printf("[billing] Stripe checkout %s has no plan_id metadata", session.ID)
		return
	}

	now := time.Now().UTC()
	nextResetAt := now.AddDate(0, 1, 0)

	batch := h.client.Batch()
	batch.Update(h.client.Collection("tenants").Doc(tenantID), []firestore.Update{
		{Path: "plan_id", Value: planID},
		{Path: "plan_status", Value: string(models.PlanStatusActive)},
		{Path: "subscription_plan", Value: planID},
		{Path: "monthly_credits_used", Value: int64(0)},
		{Path: "monthly_credits_reset_at", Value: nextResetAt},
		{Path: "plan_started_at", Value: now},
		{Path: "updated_at", Value: now},
	})
	batch.Set(
		h.client.Collection("tenants").Doc(tenantID).Collection("billing").Doc("current"),
		models.BillingRecord{
			TenantID:             tenantID,
			StripeSubscriptionID: session.Subscription,
			BillingEmail:         session.CustomerDetails.Email,
			BillingName:          session.CustomerDetails.Name,
			LastPaymentAt:        &now,
			NextBillingAt:        &nextResetAt,
			UpdatedAt:            now,
		},
	)
	batch.Commit(ctx)
	log.Printf("[billing] Stripe checkout completed: tenant=%s plan=%s", tenantID, planID)
}

func (h *BillingHandler) handleStripeSubscriptionUpdated(ctx context.Context, raw json.RawMessage) {
	var wrapper struct {
		Object struct {
			ID       string            `json:"id"`
			Status   string            `json:"status"`
			Metadata map[string]string `json:"metadata"`
		} `json:"object"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil { return }

	tenantID := wrapper.Object.Metadata["tenant_id"]
	if tenantID == "" || wrapper.Object.Status == "" { return }

	var planStatus models.TenantPlanStatus
	switch wrapper.Object.Status {
	case "active":
		planStatus = models.PlanStatusActive
	case "past_due":
		planStatus = models.PlanStatusSuspended
	case "canceled", "unpaid":
		planStatus = models.PlanStatusCancelled
	default:
		return
	}

	updates := []firestore.Update{
		{Path: "plan_status", Value: string(planStatus)},
		{Path: "updated_at", Value: time.Now().UTC()},
	}
	if planStatus == models.PlanStatusCancelled {
		updates = append(updates, firestore.Update{Path: "subscription_plan", Value: ""})
	}
	h.client.Collection("tenants").Doc(tenantID).Update(ctx, updates)
	log.Printf("[billing] Stripe subscription updated: tenant=%s status=%s", tenantID, planStatus)
}

func (h *BillingHandler) handleStripeSubscriptionDeleted(ctx context.Context, raw json.RawMessage) {
	var wrapper struct {
		Object struct {
			Metadata map[string]string `json:"metadata"`
		} `json:"object"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil { return }
	tenantID := wrapper.Object.Metadata["tenant_id"]
	if tenantID == "" { return }

	h.client.Collection("tenants").Doc(tenantID).Update(ctx, []firestore.Update{
		{Path: "plan_status", Value: string(models.PlanStatusCancelled)},
		{Path: "subscription_plan", Value: ""},
		{Path: "updated_at", Value: time.Now().UTC()},
	})
	log.Printf("[billing] Stripe subscription deleted: tenant=%s", tenantID)
}

func (h *BillingHandler) handleStripePaymentFailed(ctx context.Context, raw json.RawMessage) {
	var wrapper struct {
		Object struct {
			Subscription string            `json:"subscription"`
			Metadata     map[string]string `json:"metadata"`
		} `json:"object"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return
	}

	subscriptionID := wrapper.Object.Subscription
	log.Printf("[billing] Stripe payment failed for subscription %s", subscriptionID)

	// ── Find the tenant by matching stripe_subscription_id in billing/current ──
	tenantID := wrapper.Object.Metadata["tenant_id"]
	if tenantID == "" && subscriptionID != "" {
		// Fall back to a collection-group query when metadata doesn't carry tenant_id
		iter := h.client.CollectionGroup("billing").
			Where("stripe_subscription_id", "==", subscriptionID).
			Limit(1).
			Documents(ctx)
		snap, err := iter.Next()
		iter.Stop()
		if err == nil {
			tenantID, _ = snap.Data()["tenant_id"].(string)
		}
	}
	if tenantID == "" {
		log.Printf("[billing] handleStripePaymentFailed: could not resolve tenant for subscription %s", subscriptionID)
		return
	}

	// ── Mark tenant as past_due ──
	h.client.Collection("tenants").Doc(tenantID).Update(ctx, []firestore.Update{
		{Path: "plan_status", Value: string(models.PlanStatusSuspended)},
		{Path: "updated_at", Value: time.Now().UTC()},
	})

	// ── In-app notification ──
	if h.notificationHandler != nil {
		h.notificationHandler.CreateNotification(tenantID, "payment_failed",
			"A Stripe payment failed for your subscription. Please update your payment method to avoid service interruption.")
	}

	// ── Email alert — find the billing email (or owner) ──
	billingEmail := ""
	bSnap, err := h.client.Collection("tenants").Doc(tenantID).
		Collection("billing").Doc("current").Get(ctx)
	if err == nil {
		billingEmail, _ = bSnap.Data()["billing_email"].(string)
	}

	// Fall back to the tenant owner's global_users record
	if billingEmail == "" {
		tSnap, err := h.client.Collection("tenants").Doc(tenantID).Get(ctx)
		if err == nil {
			ownerID, _ := tSnap.Data()["owner_user_id"].(string)
			if ownerID != "" {
				uSnap, err := h.client.Collection("global_users").Doc(ownerID).Get(ctx)
				if err == nil {
					billingEmail, _ = uSnap.Data()["email"].(string)
				}
			}
		}
	}

	if billingEmail != "" && h.templateService != nil {
		subject := "Action required: payment failed for your MarketMate subscription"
		body := `<p>Hi,</p>
<p>We were unable to process your most recent payment for your <strong>MarketMate</strong> subscription.</p>
<p>Please update your payment method to avoid interruption to your service.</p>
<p><a href="` + os.Getenv("FRONTEND_URL") + `/settings/billing">Manage your subscription →</a></p>
<p>If you believe this is an error, please contact <a href="mailto:support@marketmate.com">support@marketmate.com</a>.</p>
<p>— The MarketMate team</p>`

		if err := h.templateService.SendRawEmailForTenant(ctx, tenantID, billingEmail, subject, body); err != nil {
			log.Printf("[billing] failed to send payment-failed email to %s: %v", billingEmail, err)
		} else {
			log.Printf("[billing] payment-failed email sent to %s for tenant=%s", billingEmail, tenantID)
		}
	}
}

func stripePriceIDForPlan(planID string) string {
	mapping := map[string]string{
		"starter_s":  os.Getenv("STRIPE_PRICE_STARTER_S"),
		"starter_m":  os.Getenv("STRIPE_PRICE_STARTER_M"),
		"starter_l":  os.Getenv("STRIPE_PRICE_STARTER_L"),
		"premium":    os.Getenv("STRIPE_PRICE_PREMIUM"),
		"enterprise": os.Getenv("STRIPE_PRICE_ENTERPRISE"),
	}
	return mapping[planID]
}

// CreateStripePortalSession POST /api/v1/billing/stripe/portal
// Creates a Stripe Billing Portal session so the subscriber can manage their
// subscription (cancel, upgrade, update payment method) without contacting support.
// Requires STRIPE_PORTAL_CONFIG env var set to the portal configuration ID from
// the Stripe dashboard (e.g. "bpc_1234...").
func (h *BillingHandler) CreateStripePortalSession(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	secretKey := os.Getenv("STRIPE_SECRET_KEY")
	portalConfig := os.Getenv("STRIPE_PORTAL_CONFIG")
	if secretKey == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Stripe not configured"})
		return
	}

	var req struct {
		ReturnURL string `json:"return_url"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.ReturnURL == "" {
		req.ReturnURL = os.Getenv("FRONTEND_URL") + "/billing"
	}

	// Look up the tenant's Stripe customer ID from the billing sub-document.
	billingDoc, err := h.client.Collection("tenants").Doc(tenantID).
		Collection("billing").Doc("current").Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "No billing record found for this tenant"})
		return
	}

	billingData := billingDoc.Data()
	customerID, _ := billingData["stripe_customer_id"].(string)
	if customerID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No Stripe customer ID on file — subscribe first"})
		return
	}

	// Build the portal session request payload.
	payload := map[string]string{
		"customer":   customerID,
		"return_url": req.ReturnURL,
	}
	if portalConfig != "" {
		payload["configuration"] = portalConfig
	}

	body, _ := json.Marshal(payload)
	httpReq, _ := http.NewRequestWithContext(ctx,
		http.MethodPost, "https://api.stripe.com/v1/billing_portal/sessions",
		bytes.NewReader(body))
	httpReq.SetBasicAuth(secretKey, "")
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		log.Printf("[billing] Stripe portal session request failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create portal session"})
		return
	}
	defer resp.Body.Close()

	var result struct {
		URL   string `json:"url"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil || result.URL == "" {
		msg := "no portal URL returned from Stripe"
		if result.Error != nil {
			msg = result.Error.Message
		}
		log.Printf("[billing] Stripe portal session error: %s", msg)
		c.JSON(http.StatusInternalServerError, gin.H{"error": msg})
		return
	}

	log.Printf("[billing] Stripe portal session created for tenant=%s customer=%s", tenantID, customerID)
	c.JSON(http.StatusOK, gin.H{"portal_url": result.URL})
}

// verifyStripeSignature validates the Stripe-Signature header using the
// timestamp + HMAC-SHA256 scheme documented at stripe.com/docs/webhooks/signatures.
func verifyStripeSignature(body []byte, sigHeader, secret string) bool {
	// sigHeader format: t=<timestamp>,v1=<hash>,...
	var timestamp, sig string
	for _, part := range strings.Split(sigHeader, ",") {
		if strings.HasPrefix(part, "t=") {
			timestamp = part[2:]
		} else if strings.HasPrefix(part, "v1=") {
			sig = part[3:]
		}
	}
	if timestamp == "" || sig == "" {
		return false
	}
	payload := timestamp + "." + string(body)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(sig))
}
