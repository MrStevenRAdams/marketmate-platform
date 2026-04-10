package handlers

import (
	"crypto/rand"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"

	"module-a/models"
)

// ============================================================================
// USER HANDLER — Team management within a tenant
// ============================================================================

type UserHandler struct {
	client *firestore.Client
}

func NewUserHandler(client *firestore.Client) *UserHandler {
	return &UserHandler{client: client}
}

// ── Twilio helpers ─────────────────────────────────────────────────────────────
// Uses direct Twilio Messages API (existing credentials) rather than Twilio Verify.
// OTP codes are generated here and stored in Firestore with a 10-minute expiry.

func generateOTP() string {
	n, err := rand.Int(rand.Reader, big.NewInt(900000))
	if err != nil {
		return "123456" // fallback (very unlikely)
	}
	return fmt.Sprintf("%06d", n.Int64()+100000)
}

// sendTwilioOTP sends a 6-digit OTP via SMS or WhatsApp using existing Twilio credentials.
// channel must be "sms" or "whatsapp".
// Returns the OTP code so the caller can store it for verification.
func sendTwilioOTP(toPhone, channel, code string) error {
	sid   := os.Getenv("TWILIO_ACCOUNT_SID")
	token := os.Getenv("TWILIO_AUTH_TOKEN")
	if sid == "" || token == "" {
		return fmt.Errorf("Twilio not configured (TWILIO_ACCOUNT_SID / TWILIO_AUTH_TOKEN)")
	}

	// Use TWILIO_SMS_FROM for SMS if set, otherwise strip whatsapp: prefix from TWILIO_FROM
	to   := toPhone
	from := os.Getenv("TWILIO_FROM")
	if channel == "whatsapp" {
		// Ensure both have whatsapp: prefix for WhatsApp channel
		if !strings.HasPrefix(to, "whatsapp:") {
			to = "whatsapp:" + to
		}
		if !strings.HasPrefix(from, "whatsapp:") {
			from = "whatsapp:" + from
		}
	} else {
		// SMS — use TWILIO_SMS_FROM if set, otherwise strip whatsapp: prefix
		smsFrom := os.Getenv("TWILIO_SMS_FROM")
		if smsFrom != "" {
			from = smsFrom
		} else {
			// Strip whatsapp: prefix if present
			from = strings.TrimPrefix(from, "whatsapp:")
		}
		if from == "" {
			return fmt.Errorf("no SMS-capable number configured (set TWILIO_SMS_FROM)")
		}
	}

	body := fmt.Sprintf("Your MarketMate verification code is: %s\nThis code expires in 10 minutes.", code)

	params := url.Values{}
	params.Set("To", to)
	params.Set("From", from)
	params.Set("Body", body)

	twiURL := fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s/Messages.json", sid)
	req, _ := http.NewRequest("POST", twiURL, strings.NewReader(params.Encode()))
	req.SetBasicAuth(sid, token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("twilio request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("twilio error %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// ============================================================================
// LIST MEMBERS
// Returns all active + invited members of the current tenant.
// SESSION 4: also annotates each member with group_ids they belong to.
// ============================================================================

func (h *UserHandler) ListMembers(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	iter := h.client.Collection("user_memberships").
		Where("tenant_id", "==", tenantID).
		OrderBy("created_at", firestore.Asc).
		Documents(ctx)
	defer iter.Stop()

	// Pre-load groups so we can annotate members without N+1 queries
	groupMembership := map[string][]string{} // membershipID → []groupID
	gIter := h.client.Collection("user_groups").
		Where("tenant_id", "==", tenantID).
		Documents(ctx)
	defer gIter.Stop()
	for {
		gSnap, err := gIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		var g models.UserGroup
		if err := gSnap.DataTo(&g); err != nil {
			continue
		}
		for _, mid := range g.MemberIDs {
			groupMembership[mid] = append(groupMembership[mid], g.ID)
		}
	}

	var members []models.MemberWithUser
	for {
		snap, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list members"})
			return
		}

		var m models.UserMembership
		if err := snap.DataTo(&m); err != nil {
			continue
		}

		callerRole := models.Role(c.GetString("role"))
		if m.Status == models.MembershipSuspended && !callerRole.Can("manage_users") {
			continue
		}

		mwu := models.MemberWithUser{UserMembership: m}
		mwu.GroupIDs = groupMembership[m.MembershipID]

		if m.UserID != "" {
			uSnap, err := h.client.Collection("global_users").Doc(m.UserID).Get(ctx)
			if err == nil {
				var u models.GlobalUser
				uSnap.DataTo(&u)
				mwu.Email = u.Email
				mwu.DisplayName = u.DisplayName
				mwu.AvatarURL = u.AvatarURL
			}
		} else {
			mwu.Email = m.InvitedEmail
			mwu.DisplayName = m.InvitedEmail
		}

		members = append(members, mwu)
	}

	if members == nil {
		members = []models.MemberWithUser{}
	}

	c.JSON(http.StatusOK, gin.H{
		"members": members,
		"count":   len(members),
	})
}

// ============================================================================
// CHANGE ROLE — PUT /api/v1/users/members/:membership_id/role
// ============================================================================

func (h *UserHandler) ChangeRole(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	callerUserID := c.GetString("user_id")
	membershipID := c.Param("membership_id")

	var req struct {
		Role models.Role `json:"role" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !req.Role.IsValid() || req.Role == models.RoleOwner {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid role — owner role cannot be assigned via this endpoint"})
		return
	}

	ctx := c.Request.Context()

	snap, err := h.client.Collection("user_memberships").Doc(membershipID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "membership not found"})
		return
	}
	var m models.UserMembership
	snap.DataTo(&m)

	if m.TenantID != tenantID {
		c.JSON(http.StatusForbidden, gin.H{"error": "membership not in your tenant"})
		return
	}
	if m.Role == models.RoleOwner {
		c.JSON(http.StatusForbidden, gin.H{"error": "cannot change the owner's role — use transfer ownership"})
		return
	}
	if m.UserID == callerUserID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot change your own role"})
		return
	}

	_, err = h.client.Collection("user_memberships").Doc(membershipID).Update(ctx, []firestore.Update{
		{Path: "role", Value: string(req.Role)},
		{Path: "updated_at", Value: time.Now().UTC()},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update role"})
		return
	}

	WriteUserAuditEvent(h.client, tenantID, callerUserID, "", "role_changed", m.UserID, m.InvitedEmail, map[string]interface{}{
		"old_role": string(m.Role),
		"new_role": string(req.Role),
	})

	c.JSON(http.StatusOK, gin.H{
		"message":       "Role updated",
		"membership_id": membershipID,
		"new_role":      req.Role,
	})
}

// ============================================================================
// REMOVE MEMBER — DELETE /api/v1/users/members/:membership_id
// ============================================================================

func (h *UserHandler) RemoveMember(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	callerUserID := c.GetString("user_id")
	membershipID := c.Param("membership_id")

	ctx := c.Request.Context()

	snap, err := h.client.Collection("user_memberships").Doc(membershipID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "membership not found"})
		return
	}
	var m models.UserMembership
	snap.DataTo(&m)

	if m.TenantID != tenantID {
		c.JSON(http.StatusForbidden, gin.H{"error": "membership not in your tenant"})
		return
	}
	if m.Role == models.RoleOwner {
		c.JSON(http.StatusForbidden, gin.H{"error": "cannot remove the owner"})
		return
	}
	if m.UserID == callerUserID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot remove yourself"})
		return
	}

	_, err = h.client.Collection("user_memberships").Doc(membershipID).Update(ctx, []firestore.Update{
		{Path: "status", Value: string(models.MembershipSuspended)},
		{Path: "updated_at", Value: time.Now().UTC()},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to remove member"})
		return
	}

	WriteUserAuditEvent(h.client, tenantID, callerUserID, "", "user_deleted", m.UserID, m.InvitedEmail, nil)

	c.JSON(http.StatusOK, gin.H{
		"message":       "Member removed",
		"membership_id": membershipID,
	})
}

// ============================================================================
// REVOKE INVITATION — DELETE /api/v1/users/invitations/:token
// ============================================================================

func (h *UserHandler) RevokeInvitation(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	token := c.Param("token")
	ctx := c.Request.Context()

	snap, err := h.client.Collection("tenant_invitations").Doc(token).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "invitation not found"})
		return
	}
	var inv models.TenantInvitation
	snap.DataTo(&inv)

	if inv.TenantID != tenantID {
		c.JSON(http.StatusForbidden, gin.H{"error": "invitation not in your tenant"})
		return
	}
	if inv.Used {
		c.JSON(http.StatusGone, gin.H{"error": "invitation already used"})
		return
	}

	now := time.Now().UTC()
	_, err = h.client.Collection("tenant_invitations").Doc(token).Update(ctx, []firestore.Update{
		{Path: "used", Value: true},
		{Path: "used_at", Value: now},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to revoke invitation"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Invitation revoked"})
}

// ============================================================================
// UPDATE PROFILE — PATCH /api/v1/users/profile
// ============================================================================

func (h *UserHandler) UpdateProfile(c *gin.Context) {
	userID := c.GetString("user_id")
	ctx := c.Request.Context()

	var req struct {
		DisplayName string `json:"display_name"`
		AvatarURL   string `json:"avatar_url"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	updates := []firestore.Update{
		{Path: "updated_at", Value: time.Now().UTC()},
	}
	if req.DisplayName != "" {
		updates = append(updates, firestore.Update{Path: "display_name", Value: req.DisplayName})
	}
	if req.AvatarURL != "" {
		updates = append(updates, firestore.Update{Path: "avatar_url", Value: req.AvatarURL})
	}

	if _, err := h.client.Collection("global_users").Doc(userID).Update(ctx, updates); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update profile"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Profile updated"})
}

// ============================================================================
// LIST PENDING INVITATIONS — GET /api/v1/users/invitations
// ============================================================================

func (h *UserHandler) ListInvitations(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	iter := h.client.Collection("tenant_invitations").
		Where("tenant_id", "==", tenantID).
		Where("used", "==", false).
		OrderBy("created_at", firestore.Desc).
		Documents(ctx)
	defer iter.Stop()

	var invitations []models.TenantInvitation
	for {
		snap, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		var inv models.TenantInvitation
		snap.DataTo(&inv)
		if time.Now().Before(inv.ExpiresAt) {
				// Do NOT truncate the token — the full token is needed for the cancel (revoke) action.
				// This endpoint is authenticated so only tenant members can see it.
				invitations = append(invitations, inv)
			}
	}

	if invitations == nil {
		invitations = []models.TenantInvitation{}
	}

	c.JSON(http.StatusOK, gin.H{
		"invitations": invitations,
		"count":       len(invitations),
	})
}

// ============================================================================
// UPDATE PERMISSIONS — PUT /api/v1/users/members/:membership_id/permissions
// ============================================================================

func (h *UserHandler) UpdatePermissions(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	callerRole := models.Role(c.GetString("role"))
	if !callerRole.Can("manage_users") {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	membershipID := c.Param("membership_id")
	ctx := c.Request.Context()

	snap, err := h.client.Collection("user_memberships").Doc(membershipID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "membership not found"})
		return
	}
	var m models.UserMembership
	snap.DataTo(&m)

	if m.TenantID != tenantID {
		c.JSON(http.StatusForbidden, gin.H{"error": "membership not in your tenant"})
		return
	}
	if m.Role == models.RoleOwner {
		c.JSON(http.StatusForbidden, gin.H{"error": "cannot modify owner permissions"})
		return
	}

	var req struct {
		Permissions map[string]bool `json:"permissions" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	filtered := make(map[string]bool)
	allowed := make(map[string]bool)
	for _, k := range models.AllPermissionKeys {
		allowed[k] = true
	}
	for k, v := range req.Permissions {
		if allowed[k] {
			filtered[k] = v
		}
	}

	_, err = h.client.Collection("user_memberships").Doc(membershipID).Update(ctx, []firestore.Update{
		{Path: "permissions", Value: filtered},
		{Path: "updated_at", Value: time.Now().UTC()},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update permissions"})
		return
	}

	callerUID := c.GetString("user_id")
	WriteUserAuditEvent(h.client, tenantID, callerUID, "", "permissions_changed", m.UserID, m.InvitedEmail, map[string]interface{}{
		"permissions": filtered,
	})

	c.JSON(http.StatusOK, gin.H{
		"message":       "Permissions updated",
		"membership_id": membershipID,
		"permissions":   filtered,
	})
}

// ============================================================================
// GET PROFILE — GET /api/v1/user/profile
// ============================================================================

func (h *UserHandler) GetProfile(c *gin.Context) {
	userID := c.GetString("user_id")
	ctx := c.Request.Context()

	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	snap, err := h.client.Collection("global_users").Doc(userID).Get(ctx)
	if err != nil || !snap.Exists() {
		c.JSON(http.StatusNotFound, gin.H{"error": "profile not found"})
		return
	}

	var u models.GlobalUser
	snap.DataTo(&u)
	c.JSON(http.StatusOK, gin.H{"profile": u})
}

// ============================================================================
// PUT PROFILE — PUT /api/v1/user/profile
// ============================================================================

func (h *UserHandler) PutProfile(c *gin.Context) {
	userID := c.GetString("user_id")
	ctx := c.Request.Context()

	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	var req struct {
		DisplayName string `json:"display_name"`
		AvatarURL   string `json:"avatar_url"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	updates := []firestore.Update{
		{Path: "updated_at", Value: time.Now().UTC()},
	}
	if req.DisplayName != "" {
		updates = append(updates, firestore.Update{Path: "display_name", Value: req.DisplayName})
	}
	if req.AvatarURL != "" {
		updates = append(updates, firestore.Update{Path: "avatar_url", Value: req.AvatarURL})
	}

	if _, err := h.client.Collection("global_users").Doc(userID).Update(ctx, updates); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update profile"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "profile updated"})
}


// ============================================================================
// PHONE VERIFICATION
// POST /api/v1/user/phone/send-otp
// POST /api/v1/user/phone/verify-otp
// ============================================================================

// SendPhoneOTP sends a Twilio Verify OTP to the supplied phone number.
func (h *UserHandler) SendPhoneOTP(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}
	var req struct {
		Phone   string `json:"phone"   binding:"required"`
		Channel string `json:"channel"` // "sms" | "whatsapp"
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Channel == "" {
		req.Channel = "sms"
	}
	// Save phone + channel to profile (unverified) so we know what to verify
	_, err := h.client.Collection("global_users").Doc(userID).Update(c.Request.Context(), []firestore.Update{
		{Path: "phone", Value: req.Phone},
		{Path: "phone_channel", Value: req.Channel},
		{Path: "phone_verified", Value: false},
		{Path: "updated_at", Value: time.Now().UTC()},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save phone"})
		return
	}
	// Generate OTP and store in Firestore with 10-min expiry
	code := generateOTP()
	_, err = h.client.Collection("global_users").Doc(userID).Update(c.Request.Context(), []firestore.Update{
		{Path: "phone_otp", Value: code},
		{Path: "phone_otp_expires", Value: time.Now().UTC().Add(10 * time.Minute)},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to store OTP"})
		return
	}
	if err := sendTwilioOTP(req.Phone, req.Channel, code); err != nil {
		log.Printf("[Profile] Twilio OTP failed for user %s: %v", userID, err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to send verification code: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "Verification code sent"})
}

// VerifyPhoneOTP checks the OTP and marks the phone as verified on success.
func (h *UserHandler) VerifyPhoneOTP(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}
	var req struct {
		Phone string `json:"phone"   binding:"required"`
		Code  string `json:"code"    binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// Check OTP against Firestore
	ctx := c.Request.Context()
	snap, err := h.client.Collection("global_users").Doc(userID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load profile"})
		return
	}
	storedCode, _ := snap.Data()["phone_otp"].(string)
	var otpExpires time.Time
	if t, ok := snap.Data()["phone_otp_expires"].(time.Time); ok {
		otpExpires = t
	}
	if storedCode == "" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "No verification code found — please request a new one"})
		return
	}
	if time.Now().UTC().After(otpExpires) {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "Verification code has expired — please request a new one"})
		return
	}
	if req.Code != storedCode {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "Incorrect code — please try again"})
		return
	}
	// Mark verified and clear OTP
	_, err = h.client.Collection("global_users").Doc(userID).Update(ctx, []firestore.Update{
		{Path: "phone_verified", Value: true},
		{Path: "phone_otp", Value: firestore.Delete},
		{Path: "phone_otp_expires", Value: firestore.Delete},
		{Path: "updated_at", Value: time.Now().UTC()},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to mark phone verified"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "Phone number verified successfully"})
}

// UpdateNotifPrefs saves notification preferences for the user.
// PUT /api/v1/user/notif-prefs
// Writes to global_users (for profile display) AND user_memberships (for messaging notifier).
func (h *UserHandler) UpdateNotifPrefs(c *gin.Context) {
	userID   := c.GetString("user_id")
	tenantID := c.GetString("tenant_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}
	var req struct {
		Email    bool   `json:"email"`
		SMS      bool   `json:"sms"`
		WhatsApp bool   `json:"whatsapp"`
		Phone    string `json:"phone"`   // phone to use for SMS/WhatsApp alerts
		EmailAddr string `json:"email_address"` // override email for alerts
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx := c.Request.Context()

	// 1. Update GlobalUser notif_prefs
	_, err := h.client.Collection("global_users").Doc(userID).Update(ctx, []firestore.Update{
		{Path: "notif_prefs.email", Value: req.Email},
		{Path: "notif_prefs.sms", Value: req.SMS},
		{Path: "notif_prefs.whatsapp", Value: req.WhatsApp},
		{Path: "updated_at", Value: time.Now().UTC()},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save notification preferences"})
		return
	}

	// 2. Update UserMembership.MessagingNotifPrefs so the messaging notifier picks it up
	if tenantID != "" {
		channels := []string{}
		if req.Email    { channels = append(channels, "email") }
		if req.SMS      { channels = append(channels, "sms") }
		if req.WhatsApp { channels = append(channels, "whatsapp") }

		// Find the membership doc for this user+tenant
		iter := h.client.Collection("user_memberships").
			Where("user_id", "==", userID).
			Where("tenant_id", "==", tenantID).
			Limit(1).Documents(ctx)
		if doc, err2 := iter.Next(); err2 == nil {
			doc.Ref.Update(ctx, []firestore.Update{
				{Path: "messaging_notif_prefs.channels", Value: channels},
				{Path: "messaging_notif_prefs.phone", Value: req.Phone},
				{Path: "messaging_notif_prefs.email", Value: req.EmailAddr},
				{Path: "updated_at", Value: time.Now().UTC()},
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ============================================================================
// SESSION 4 — SEND PASSWORD RESET
// POST /api/v1/users/members/:membership_id/send-password-reset
// Uses Firebase Auth's password reset email via the REST API.
// ============================================================================

func (h *UserHandler) SendPasswordReset(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	callerRole := models.Role(c.GetString("role"))
	if !callerRole.Can("manage_users") {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	membershipID := c.Param("membership_id")
	ctx := c.Request.Context()

	// Fetch the membership to get user_id
	snap, err := h.client.Collection("user_memberships").Doc(membershipID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "membership not found"})
		return
	}
	var m models.UserMembership
	snap.DataTo(&m)

	if m.TenantID != tenantID {
		c.JSON(http.StatusForbidden, gin.H{"error": "membership not in your tenant"})
		return
	}
	if m.Role == models.RoleOwner {
		c.JSON(http.StatusForbidden, gin.H{"error": "cannot reset owner password via this endpoint"})
		return
	}
	if m.Status == models.MembershipSuspended {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot reset password for a suspended member"})
		return
	}

	// Fetch the user's email from GlobalUser
	var email string
	if m.UserID != "" {
		uSnap, err := h.client.Collection("global_users").Doc(m.UserID).Get(ctx)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load user record"})
			return
		}
		var u models.GlobalUser
		uSnap.DataTo(&u)
		email = u.Email
	} else {
		// Pending invite — use invited_email
		email = m.InvitedEmail
	}

	if email == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no email address found for this member"})
		return
	}

	// Send password reset via Firebase Admin SDK REST API.
	// The Firebase Admin SDK for Go does not expose generatePasswordResetLink
	// in all versions, so we use the identitytoolkit REST endpoint with the
	// service-account-derived token obtained from the default credential.
	// NOTE: firebaseAuthSendPasswordReset is a package-level helper defined below.
	if err := firebaseAuthSendPasswordReset(ctx, email); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to send password reset: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Password reset email sent",
		"email":   email,
	})
}

// ============================================================================
// SESSION 4 — USER GROUPS CRUD
// ============================================================================

// ListUserGroups GET /api/v1/user-groups
func (h *UserHandler) ListUserGroups(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	ctx := c.Request.Context()

	iter := h.client.Collection("user_groups").
		Where("tenant_id", "==", tenantID).
		OrderBy("created_at", firestore.Asc).
		Documents(ctx)
	defer iter.Stop()

	var groups []models.UserGroup
	for {
		snap, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list groups"})
			return
		}
		var g models.UserGroup
		if err := snap.DataTo(&g); err != nil {
			continue
		}
		groups = append(groups, g)
	}

	if groups == nil {
		groups = []models.UserGroup{}
	}

	c.JSON(http.StatusOK, gin.H{"groups": groups, "count": len(groups)})
}

// CreateUserGroup POST /api/v1/user-groups
func (h *UserHandler) CreateUserGroup(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	callerRole := models.Role(c.GetString("role"))
	if !callerRole.Can("manage_users") {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	var req struct {
		Name        string          `json:"name"        binding:"required"`
		Description string          `json:"description"`
		Permissions map[string]bool `json:"permissions"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Sanitise name
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "group name is required"})
		return
	}

	// Filter permissions to known keys
	filtered := filterPermissions(req.Permissions)

	ctx := c.Request.Context()
	now := time.Now().UTC()
	groupID := "grp_" + uuid.New().String()

	g := models.UserGroup{
		ID:          groupID,
		TenantID:    tenantID,
		Name:        req.Name,
		Description: req.Description,
		Permissions: filtered,
		MemberIDs:   []string{},
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if _, err := h.client.Collection("user_groups").Doc(groupID).Set(ctx, g); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create group"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"group": g})
}

// UpdateUserGroup PUT /api/v1/user-groups/:group_id
func (h *UserHandler) UpdateUserGroup(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	callerRole := models.Role(c.GetString("role"))
	if !callerRole.Can("manage_users") {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	groupID := c.Param("group_id")
	ctx := c.Request.Context()

	snap, err := h.client.Collection("user_groups").Doc(groupID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "group not found"})
		return
	}
	var g models.UserGroup
	snap.DataTo(&g)

	if g.TenantID != tenantID {
		c.JSON(http.StatusForbidden, gin.H{"error": "group not in your tenant"})
		return
	}

	var req struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Permissions map[string]bool `json:"permissions"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	updates := []firestore.Update{
		{Path: "updated_at", Value: time.Now().UTC()},
	}
	if req.Name != "" {
		updates = append(updates, firestore.Update{Path: "name", Value: strings.TrimSpace(req.Name)})
	}
	updates = append(updates, firestore.Update{Path: "description", Value: req.Description})
	if req.Permissions != nil {
		updates = append(updates, firestore.Update{Path: "permissions", Value: filterPermissions(req.Permissions)})
	}

	if _, err := h.client.Collection("user_groups").Doc(groupID).Update(ctx, updates); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update group"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Group updated", "group_id": groupID})
}

// DeleteUserGroup DELETE /api/v1/user-groups/:group_id
func (h *UserHandler) DeleteUserGroup(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	callerRole := models.Role(c.GetString("role"))
	if !callerRole.Can("manage_users") {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	groupID := c.Param("group_id")
	ctx := c.Request.Context()

	snap, err := h.client.Collection("user_groups").Doc(groupID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "group not found"})
		return
	}
	var g models.UserGroup
	snap.DataTo(&g)

	if g.TenantID != tenantID {
		c.JSON(http.StatusForbidden, gin.H{"error": "group not in your tenant"})
		return
	}

	if _, err := h.client.Collection("user_groups").Doc(groupID).Delete(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete group"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Group deleted", "group_id": groupID})
}

// AddGroupMember POST /api/v1/user-groups/:group_id/members
// Body: { "membership_id": "mem_..." }
func (h *UserHandler) AddGroupMember(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	callerRole := models.Role(c.GetString("role"))
	if !callerRole.Can("manage_users") {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	groupID := c.Param("group_id")
	ctx := c.Request.Context()

	var req struct {
		MembershipID string `json:"membership_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Verify group belongs to tenant
	gSnap, err := h.client.Collection("user_groups").Doc(groupID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "group not found"})
		return
	}
	var g models.UserGroup
	gSnap.DataTo(&g)
	if g.TenantID != tenantID {
		c.JSON(http.StatusForbidden, gin.H{"error": "group not in your tenant"})
		return
	}

	// Verify membership belongs to tenant
	mSnap, err := h.client.Collection("user_memberships").Doc(req.MembershipID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "membership not found"})
		return
	}
	var m models.UserMembership
	mSnap.DataTo(&m)
	if m.TenantID != tenantID {
		c.JSON(http.StatusForbidden, gin.H{"error": "membership not in your tenant"})
		return
	}

	// Append membership_id to group's member_ids (Firestore ArrayUnion)
	if _, err := h.client.Collection("user_groups").Doc(groupID).Update(ctx, []firestore.Update{
		{Path: "member_ids", Value: firestore.ArrayUnion(req.MembershipID)},
		{Path: "updated_at", Value: time.Now().UTC()},
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to add member to group"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Member added to group"})
}

// RemoveGroupMember DELETE /api/v1/user-groups/:group_id/members/:membership_id
func (h *UserHandler) RemoveGroupMember(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	callerRole := models.Role(c.GetString("role"))
	if !callerRole.Can("manage_users") {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}

	groupID := c.Param("group_id")
	membershipID := c.Param("membership_id")
	ctx := c.Request.Context()

	gSnap, err := h.client.Collection("user_groups").Doc(groupID).Get(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "group not found"})
		return
	}
	var g models.UserGroup
	gSnap.DataTo(&g)
	if g.TenantID != tenantID {
		c.JSON(http.StatusForbidden, gin.H{"error": "group not in your tenant"})
		return
	}

	if _, err := h.client.Collection("user_groups").Doc(groupID).Update(ctx, []firestore.Update{
		{Path: "member_ids", Value: firestore.ArrayRemove(membershipID)},
		{Path: "updated_at", Value: time.Now().UTC()},
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to remove member from group"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Member removed from group"})
}

// ============================================================================
// HELPERS
// ============================================================================

// filterPermissions removes any keys not in AllPermissionKeys.
func filterPermissions(raw map[string]bool) map[string]bool {
	if raw == nil {
		return map[string]bool{}
	}
	allowed := make(map[string]bool)
	for _, k := range models.AllPermissionKeys {
		allowed[k] = true
	}
	result := make(map[string]bool)
	for k, v := range raw {
		if allowed[k] {
			result[k] = v
		}
	}
	return result
}
