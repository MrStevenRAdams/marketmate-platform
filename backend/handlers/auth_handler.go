package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"module-a/models"
	"module-a/services"
)

// ============================================================================
// AUTH HANDLER
// ============================================================================
// Registration flow:
//   POST /auth/register        → create Firebase user + global_user + tenant + membership
//   POST /auth/login           → Firebase handles auth, this returns tenant list
//   GET  /auth/me              → current user profile + tenant memberships
//   POST /auth/switch-tenant   → validate membership, return tenant context
//   POST /auth/invite/accept   → accept invitation token
//
// Note: Firebase authentication (email/password sign-in, token issuance,
// password reset) is handled client-side by the Firebase JS SDK.
// The backend only needs to verify tokens and manage the Firestore records.
// ============================================================================

type AuthHandler struct {
	client       *firestore.Client
	usageService *services.UsageService
}

func NewAuthHandler(client *firestore.Client, usageSvc *services.UsageService) *AuthHandler {
	return &AuthHandler{client: client, usageService: usageSvc}
}

// ============================================================================
// REGISTER
// ============================================================================
// Called after the user has signed up via Firebase JS SDK client-side.
// The client sends the Firebase ID token + registration details.
// We create the global_user record, the tenant, and the owner membership.

type RegisterRequest struct {
	FirebaseUID    string `json:"firebase_uid" binding:"required"`
	Email          string `json:"email"        binding:"required"`
	DisplayName    string `json:"display_name" binding:"required"`
	CompanyName    string `json:"company_name" binding:"required"`
	PlanID         string `json:"plan_id"      binding:"required"` // starter_s, starter_m, etc.
	ReferralSource string `json:"referral_source"`                 // temu, ebay, organic, etc.
}

func (h *AuthHandler) Register(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	req.CompanyName = strings.TrimSpace(req.CompanyName)

	// Validate plan ID
	planID := models.PlanID(req.PlanID)
	validPlans := map[models.PlanID]bool{
		models.PlanStarterS: true,
		models.PlanStarterM: true,
		models.PlanStarterL: true,
	}
	// Only starter plans available at self-service registration
	// Premium/Enterprise require sales contact
	if !validPlans[planID] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid plan — premium and enterprise plans require contacting sales"})
		return
	}

	ctx := c.Request.Context()

	// Check if Firebase UID already registered
	existing := h.client.Collection("global_users").
		Where("firebase_uid", "==", req.FirebaseUID).
		Limit(1).Documents(ctx)
	defer existing.Stop()
	if snap, err := existing.Next(); err == nil && snap.Exists() {
		c.JSON(http.StatusConflict, gin.H{"error": "account already registered"})
		return
	}

	now := time.Now().UTC()
	userID := "usr_" + uuid.New().String()
	membershipID := "mem_" + uuid.New().String()

	// Generate sequential numeric tenant ID starting at 10000
	tenantID, err := nextTenantID(ctx, h.client)
	if err != nil {
		log.Printf("[auth] failed to generate tenant ID: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate tenant ID"})
		return
	}

	// Trial ends in 14 days
	trialEnd := now.AddDate(0, 0, 14)

	// ── Create all documents in a batch ─────────────────────────────────────
	batch := h.client.Batch()

	// 1. Global user
	userRef := h.client.Collection("global_users").Doc(userID)
	batch.Set(userRef, models.GlobalUser{
		UserID:      userID,
		FirebaseUID: req.FirebaseUID,
		Email:       req.Email,
		DisplayName: req.DisplayName,
		CreatedAt:   now,
		LastLoginAt: now,
	})

	// 2. Tenant
	freeCreditsLimit := 0
	if req.ReferralSource == "temu" {
		freeCreditsLimit = 100
	}
	tenantRef := h.client.Collection("tenants").Doc(tenantID)
	batch.Set(tenantRef, models.Tenant{
		TenantID:         tenantID,
		Name:             req.CompanyName,
		Slug:             slugify(req.CompanyName),
		OwnerUserID:      userID,
		PlanID:           planID,
		PlanStatus:       models.PlanStatusTrialing,
		TrialEndsAt:      &trialEnd,
		Initials:         initials(req.CompanyName),
		Color:            pickColor(tenantID),
		ReferralSource:   req.ReferralSource,
		FreeCreditsLimit: freeCreditsLimit,
		CreatedAt:        now,
		UpdatedAt:        now,
	})

	// 3. Owner membership
	memberRef := h.client.Collection("user_memberships").Doc(membershipID)
	joinedAt := now
	batch.Set(memberRef, models.UserMembership{
		MembershipID: membershipID,
		UserID:       userID,
		TenantID:     tenantID,
		Role:         models.RoleOwner,
		Status:       models.MembershipActive,
		JoinedAt:     &joinedAt,
		CreatedAt:    now,
		UpdatedAt:    now,
	})

	if _, err := batch.Commit(ctx); err != nil {
		log.Printf("[auth] registration batch failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "registration failed"})
		return
	}

	// Initialise ledger for this billing period (non-blocking)
	go h.usageService.EnsureLedger(context.Background(), tenantID)

	// Create Default warehouse (fulfilment source) for the new tenant
	go func() {
		bgCtx := context.Background()
		now2 := time.Now().UTC()
		sourceID := "src_default_" + tenantID
		defaultSource := map[string]interface{}{
			"source_id":          sourceID,
			"tenant_id":          tenantID,
			"name":               "Default Warehouse",
			"code":               "DEFAULT",
			"type":               "own_warehouse",
			"active":             true,
			"default":            true,
			"inventory_tracked":  true,
			"inventory_mode":     "manual",
			"created_at":         now2,
			"updated_at":         now2,
		}
		_, err := h.client.Collection("tenants").Doc(tenantID).
			Collection("fulfilment_sources").Doc(sourceID).Set(bgCtx, defaultSource)
		if err != nil {
			log.Printf("[auth] failed to create default warehouse for tenant %s: %v", tenantID, err)
		} else {
			log.Printf("[auth] created Default Warehouse for tenant %s", tenantID)
		}
	}()

	c.JSON(http.StatusCreated, gin.H{
		"user_id":       userID,
		"tenant_id":     tenantID,
		"membership_id": membershipID,
		"plan_id":       planID,
		"trial_ends_at": trialEnd,
		"message":       "Registration successful — 14 day trial started",
	})
}

// ============================================================================
// ME — current user profile + all tenant memberships
// ============================================================================
// Called after login. Returns the user's profile and the list of tenants
// they belong to, so the frontend can present a tenant switcher.

func (h *AuthHandler) Me(c *gin.Context) {
	// Auth middleware has already verified the token.
	// We need the firebase_uid from the token to find the user.
	// The middleware sets user_id for the *active* tenant; for /me we want
	// all memberships, so we look up by the userID from context.
	userID := c.GetString("user_id")
	if userID == "" {
		// /me can also be called without a tenant header (pre-tenant-selection)
		// In this case, we need the firebase UID from the token directly.
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	ctx := c.Request.Context()

	// Get user profile
	userSnap, err := h.client.Collection("global_users").Doc(userID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	var user models.GlobalUser
	userSnap.DataTo(&user)

	// Update last login timestamp and write audit event (non-blocking)
	go UpdateLastLogin(h.client, userID)
	go WriteUserAuditEvent(h.client, "", userID, user.Email, "login", "", "", nil)

	// Get all active memberships
	memberIter := h.client.Collection("user_memberships").
		Where("user_id", "==", userID).
		Where("status", "==", string(models.MembershipActive)).
		Documents(ctx)
	defer memberIter.Stop()

	type TenantSummary struct {
		TenantID   string           `json:"tenant_id"`
		Name       string           `json:"name"`
		Initials   string           `json:"initials"`
		Color      string           `json:"color"`
		Role       models.Role      `json:"role"`
		PlanID     models.PlanID    `json:"plan_id"`
		PlanStatus models.TenantPlanStatus `json:"plan_status"`
	}

	var tenants []TenantSummary
	for {
		snap, err := memberIter.Next()
		if err != nil {
			break
		}
		var m models.UserMembership
		snap.DataTo(&m)

		// Fetch tenant details
		tSnap, err := h.client.Collection("tenants").Doc(m.TenantID).Get(ctx)
		if err != nil {
			continue
		}
		var t models.Tenant
		tSnap.DataTo(&t)

		tenants = append(tenants, TenantSummary{
			TenantID:   t.TenantID,
			Name:       t.Name,
			Initials:   t.Initials,
			Color:      t.Color,
			Role:       m.Role,
			PlanID:     t.PlanID,
			PlanStatus: t.PlanStatus,
		})
	}

	if tenants == nil {
		tenants = []TenantSummary{}
	}

	c.JSON(http.StatusOK, gin.H{
		"user": gin.H{
			"user_id":      user.UserID,
			"email":        user.Email,
			"display_name": user.DisplayName,
			"avatar_url":   user.AvatarURL,
			"created_at":   user.CreatedAt,
		},
		"tenants": tenants,
	})
}

// MeByFirebaseUID is used when X-Tenant-Id is not yet known (initial login).
// Accepts the Firebase UID directly in the body.
func (h *AuthHandler) MeByFirebaseUID(c *gin.Context) {
	var req struct {
		FirebaseUID string `json:"firebase_uid" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()

	iter := h.client.Collection("global_users").
		Where("firebase_uid", "==", req.FirebaseUID).
		Limit(1).Documents(ctx)
	defer iter.Stop()

	snap, err := iter.Next()
	if err != nil {
		// iterator.Done means the query returned zero results — user not yet registered.
		// This is a normal race condition: Firebase creates the user before the backend
		// /register call has finished writing to Firestore. The frontend handles 404
		// gracefully and will retry or redirect to registration.
		if err == iterator.Done || status.Code(err) == codes.NotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not registered", "code": "not_registered"})
			return
		}
		log.Printf("[auth/me] Firestore lookup error for uid=%s: %v", req.FirebaseUID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "lookup failed"})
		return
	}

	var user models.GlobalUser
	if err := snap.DataTo(&user); err != nil {
		log.Printf("[auth/me] failed to deserialise user for uid=%s: %v", req.FirebaseUID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "user data error"})
		return
	}

	// Reuse Me logic — set user_id in context and call through
	c.Set("user_id", user.UserID)
	h.Me(c)
}

// ============================================================================
// INVITATIONS
// ============================================================================

type InviteRequest struct {
	Email               string          `json:"email" binding:"required"`
	Role                models.Role     `json:"role"  binding:"required"`
	// DisplayName is optionally provided so the invitee's name is pre-populated on acceptance.
	DisplayName         string          `json:"display_name,omitempty"`
	// PermissionOverrides are optional individual permission settings applied when the invite is accepted.
	PermissionOverrides map[string]bool `json:"permission_overrides,omitempty"`
}

// InviteUser creates an invitation for a new team member.
// POST /api/v1/users/invite
func (h *AuthHandler) InviteUser(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	inviterUserID := c.GetString("user_id")

	var req InviteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Cannot invite as owner
	if req.Role == models.RoleOwner {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot invite as owner role"})
		return
	}
	if !req.Role.IsValid() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid role"})
		return
	}

	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	ctx := c.Request.Context()

	// Check if already a member
	existingIter := h.client.Collection("user_memberships").
		Where("tenant_id", "==", tenantID).
		Where("invited_email", "==", req.Email).
		Limit(1).Documents(ctx)
	defer existingIter.Stop()
	if snap, err := existingIter.Next(); err == nil && snap.Exists() {
		c.JSON(http.StatusConflict, gin.H{"error": "user already invited or is a member"})
		return
	}

	// Get tenant name for the invitation email
	tSnap, err := h.client.Collection("tenants").Doc(tenantID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load tenant"})
		return
	}
	var tenant models.Tenant
	tSnap.DataTo(&tenant)

	// Generate secure invitation token
	tokenBytes := make([]byte, 32)
	rand.Read(tokenBytes)
	token := hex.EncodeToString(tokenBytes)

	now := time.Now().UTC()
	invitation := models.TenantInvitation{
		Token:               token,
		TenantID:            tenantID,
		TenantName:          tenant.Name,
		InvitedEmail:        req.Email,
		Role:                req.Role,
		InvitedBy:           inviterUserID,
		DisplayNameHint:     req.DisplayName,
		PermissionOverrides: req.PermissionOverrides,
		ExpiresAt:           now.Add(7 * 24 * time.Hour), // 7 days
		Used:                false,
		CreatedAt:           now,
	}

	if _, err := h.client.Collection("tenant_invitations").Doc(token).Set(ctx, invitation); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create invitation"})
		return
	}

	log.Printf("[auth] invitation created for %s to tenant %s (token: %s)", req.Email, tenantID, token[:8]+"...")

	inviteLink := fmt.Sprintf("%s/invite/%s", frontendURL(), token)

	// Send the invitation email asynchronously so it doesn't block the response.
	go func() {
		subject := fmt.Sprintf("You've been invited to join %s on MarketMate", tenant.Name)
		htmlBody := fmt.Sprintf(`<!DOCTYPE html>
<html><body style="font-family:Arial,sans-serif;max-width:600px;margin:0 auto;padding:24px">
<h2 style="color:#1a1a2e">You've been invited to join MarketMate</h2>
<p>You have been invited to join the <strong>%s</strong> workspace on MarketMate as <strong>%s</strong>.</p>
<p>Click the link below to accept your invitation (valid for 7 days):</p>
<p><a href="%s" style="background:#4f46e5;color:#fff;padding:12px 24px;border-radius:6px;text-decoration:none;display:inline-block">Accept Invitation</a></p>
<p style="color:#6b7280;font-size:12px">Or copy this link: %s</p>
<p style="color:#6b7280;font-size:12px">If you did not expect this invitation, you can safely ignore this email.</p>
</body></html>`, tenant.Name, req.Role, inviteLink, inviteLink)

		if emailErr := services.SendRawEmail(req.Email, subject, htmlBody); emailErr != nil {
			log.Printf("[auth] ERROR: failed to send invitation email to %s: %v", req.Email, emailErr)
		} else {
			log.Printf("[auth] invitation email sent successfully to %s", req.Email)
		}
	}()

	go WriteUserAuditEvent(h.client, tenantID, inviterUserID, "", "invite_sent", "", req.Email, map[string]interface{}{
		"role": string(req.Role),
	})

	c.JSON(http.StatusCreated, gin.H{
		"message":      "Invitation created",
		"invited_email": req.Email,
		"role":         req.Role,
		"expires_at":   invitation.ExpiresAt,
		"invite_link":  fmt.Sprintf("%s/invite/%s", frontendURL(), token),
		// token only returned in dev — in prod this would be emailed only
	})
}

// AcceptInvitation processes an invitation token.
// POST /auth/invite/accept
func (h *AuthHandler) AcceptInvitation(c *gin.Context) {
	var req struct {
		Token       string `json:"token"        binding:"required"`
		FirebaseUID string `json:"firebase_uid" binding:"required"`
		DisplayName string `json:"display_name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()

	// Fetch and validate invitation
	invSnap, err := h.client.Collection("tenant_invitations").Doc(req.Token).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "invitation not found or expired"})
		return
	}

	var inv models.TenantInvitation
	invSnap.DataTo(&inv)

	if inv.Used {
		c.JSON(http.StatusGone, gin.H{"error": "invitation already used"})
		return
	}
	if time.Now().After(inv.ExpiresAt) {
		c.JSON(http.StatusGone, gin.H{"error": "invitation expired"})
		return
	}

	// Find or create global user
	userIter := h.client.Collection("global_users").
		Where("firebase_uid", "==", req.FirebaseUID).
		Limit(1).Documents(ctx)
	defer userIter.Stop()

	var userID string
	now := time.Now().UTC()

	if snap, err := userIter.Next(); err == nil && snap.Exists() {
		// Existing user accepting invite to a new tenant
		var existing models.GlobalUser
		snap.DataTo(&existing)
		userID = existing.UserID
	} else {
		// New user — create global_user record
		userID = "usr_" + uuid.New().String()
		displayName := req.DisplayName
		if displayName == "" {
			displayName = inv.DisplayNameHint // pre-filled from invite
		}
		if displayName == "" {
			displayName = inv.InvitedEmail
		}
		h.client.Collection("global_users").Doc(userID).Set(ctx, models.GlobalUser{
			UserID:      userID,
			FirebaseUID: req.FirebaseUID,
			Email:       inv.InvitedEmail,
			DisplayName: displayName,
			CreatedAt:   now,
			LastLoginAt: now,
		})
	}

	membershipID := "mem_" + uuid.New().String()
	joinedAt := now

	// Batch: create membership + mark invitation used
	batch := h.client.Batch()

	batch.Set(h.client.Collection("user_memberships").Doc(membershipID), models.UserMembership{
		MembershipID:    membershipID,
		UserID:          userID,
		TenantID:        inv.TenantID,
		Role:            inv.Role,
		Status:          models.MembershipActive,
		InvitedBy:       inv.InvitedBy,
		InvitedEmail:    inv.InvitedEmail,
		DisplayNameHint: inv.DisplayNameHint,
		Permissions:     inv.PermissionOverrides, // apply invite-time permission overrides
		JoinedAt:        &joinedAt,
		CreatedAt:       now,
		UpdatedAt:       now,
	})

	batch.Update(h.client.Collection("tenant_invitations").Doc(req.Token), []firestore.Update{
		{Path: "used", Value: true},
		{Path: "used_at", Value: now},
	})

	if _, err := batch.Commit(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to accept invitation"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":       "Invitation accepted",
		"user_id":       userID,
		"tenant_id":     inv.TenantID,
		"tenant_name":   inv.TenantName,
		"role":          inv.Role,
		"membership_id": membershipID,
	})
}

// GetInvitation returns invitation details for the accept page (no auth required)
// GET /auth/invite/:token
func (h *AuthHandler) GetInvitation(c *gin.Context) {
	token := c.Param("token")
	ctx := c.Request.Context()

	snap, err := h.client.Collection("tenant_invitations").Doc(token).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "invitation not found"})
		return
	}

	var inv models.TenantInvitation
	snap.DataTo(&inv)

	if inv.Used {
		c.JSON(http.StatusGone, gin.H{"error": "invitation already used", "used": true})
		return
	}
	if time.Now().After(inv.ExpiresAt) {
		c.JSON(http.StatusGone, gin.H{"error": "invitation has expired", "expired": true})
		return
	}

	// Return only what the frontend needs to show the accept page
	c.JSON(http.StatusOK, gin.H{
		"tenant_name":   inv.TenantName,
		"invited_email": inv.InvitedEmail,
		"role":          inv.Role,
		"expires_at":    inv.ExpiresAt,
	})
}

// ============================================================================
// HELPERS
// ============================================================================

// nextTenantID atomically increments a counter in Firestore and returns a
// sequential numeric tenant ID in the format "tenant-10000", "tenant-10001", etc.
func nextTenantID(ctx context.Context, client *firestore.Client) (string, error) {
	counterRef := client.Collection("system_counters").Doc("tenant_id_seq")
	var nextNum int64
	err := client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		snap, err := tx.Get(counterRef)
		if err != nil {
			// Document doesn't exist yet — start at 10000
			nextNum = 10000
			return tx.Set(counterRef, map[string]interface{}{"next": int64(10001), "created_at": time.Now().UTC()})
		}
		var counter struct {
			Next int64 `firestore:"next"`
		}
		if err := snap.DataTo(&counter); err != nil || counter.Next < 10000 {
			nextNum = 10000
			return tx.Set(counterRef, map[string]interface{}{"next": int64(10001)})
		}
		nextNum = counter.Next
		return tx.Update(counterRef, []firestore.Update{{Path: "next", Value: counter.Next + 1}})
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("tenant-%d", nextNum), nil
}

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "-")
	var result strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			result.WriteRune(r)
		}
	}
	return result.String()
}

func initials(name string) string {
	words := strings.Fields(name)
	result := ""
	for i, w := range words {
		if i >= 2 {
			break
		}
		if len(w) > 0 {
			result += strings.ToUpper(string(w[0]))
		}
	}
	if result == "" {
		return "?"
	}
	return result
}

func pickColor(seed string) string {
	colors := []string{"#3b82f6", "#10b981", "#f59e0b", "#ef4444", "#8b5cf6", "#06b6d4", "#f97316", "#14b8a6"}
	sum := 0
	for _, ch := range seed {
		sum += int(ch)
	}
	return colors[sum%len(colors)]
}

func frontendURL() string {
	if url := os.Getenv("FRONTEND_URL"); url != "" {
		return url
	}
	return "https://marketmate-486116.web.app"
}
