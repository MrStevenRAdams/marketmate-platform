package models

import "time"

// ============================================================================
// SESSION 5 — Extended Permission Keys & Role Defaults
// SESSION 6 — User Audit Event model
// ============================================================================

// AllPermissionKeys — expanded from 12 to full set across all tabs.
// Replaces the slim list in user.go; user.go's var is overridden here.
var AllPermissionKeysExpanded = []string{
	// General
	"general.topbar",
	"general.sync_status",
	"general.account_management",
	"general.notifications",

	// Inventory
	"inventory.adjust",
	"inventory.stock_adjustments",
	"products.delete",
	"inventory.supplier_management",
	"inventory.purchase_orders_view",
	"inventory.purchase_orders_edit",
	"inventory.stock_takes",

	// Orders
	"orders.create",
	"orders.delete",
	"orders.merge",
	"orders.split",
	"orders.cancel",
	"orders.view_details",
	"orders.edit_details",
	"orders.refund",

	// Shipping
	"dispatch.create",
	"dispatch.view_labels",
	"dispatch.manage_services",
	"dispatch.edit_tracking",

	// Dashboards
	"reports.view",
	"reports.export",
	"reports.financial",

	// Email
	"email.send",
	"email.adhoc",
	"email.resend",
	"email.templates",
	"email.view_sent",
	"email.manage_accounts",

	// Apps
	"apps.macro_configurations",
	"apps.my_applications",
	"apps.automation_logs",

	// Settings
	"settings.configurators",
	"settings.import_export",
	"settings.currency_rates",
	"settings.team",
	"settings.channel_integration",
	"settings.general",
	"settings.data_purge",
	"settings.extract_inventory",
	"settings.template_designer",
	"settings.automation_logs",
	"settings.countries",

	// Legacy keys retained for backwards compat
	"rmas.authorise",
	"billing.manage",
}

// RoleDefaultPermissionsExpanded returns defaults for the full permission set.
func RoleDefaultPermissionsExpanded(role Role) map[string]bool {
	isOwner := role == RoleOwner
	isAdmin := role == RoleOwner || role == RoleAdmin
	isManager := role == RoleOwner || role == RoleAdmin || role == RoleManager

	d := map[string]bool{
		// General
		"general.topbar":              true,
		"general.sync_status":         true,
		"general.account_management":  isAdmin,
		"general.notifications":       true,

		// Inventory
		"inventory.adjust":              isManager,
		"inventory.stock_adjustments":   isManager,
		"products.delete":               isAdmin,
		"inventory.supplier_management": isManager,
		"inventory.purchase_orders_view": isManager,
		"inventory.purchase_orders_edit": isAdmin,
		"inventory.stock_takes":          isManager,

		// Orders
		"orders.create":       isManager,
		"orders.delete":       isAdmin,
		"orders.merge":        isManager,
		"orders.split":        isManager,
		"orders.cancel":       isManager,
		"orders.view_details": true,
		"orders.edit_details": isManager,
		"orders.refund":       isAdmin,

		// Shipping
		"dispatch.create":          isManager,
		"dispatch.view_labels":     isManager,
		"dispatch.manage_services": isAdmin,
		"dispatch.edit_tracking":   isManager,

		// Dashboards
		"reports.view":      true,
		"reports.export":    isManager,
		"reports.financial": isAdmin,

		// Email
		"email.send":            isManager,
		"email.adhoc":           isManager,
		"email.resend":          isManager,
		"email.templates":       isAdmin,
		"email.view_sent":       isManager,
		"email.manage_accounts": isAdmin,

		// Apps
		"apps.macro_configurations": isAdmin,
		"apps.my_applications":      isAdmin,
		"apps.automation_logs":      isManager,

		// Settings
		"settings.configurators":       isAdmin,
		"settings.import_export":       isAdmin,
		"settings.currency_rates":      isAdmin,
		"settings.team":                isAdmin,
		"settings.channel_integration": isAdmin,
		"settings.general":             isAdmin,
		"settings.data_purge":          isOwner,
		"settings.extract_inventory":   isAdmin,
		"settings.template_designer":   isAdmin,
		"settings.automation_logs":     isManager,
		"settings.countries":           isAdmin,

		// Legacy
		"rmas.authorise": isManager,
		"billing.manage": isOwner,
	}
	return d
}

// ── Session 6: User Audit Event ──────────────────────────────────────────────

// UserAuditEventType enumerates user management audit event types.
type UserAuditEventType string

const (
	UserAuditUserCreated         UserAuditEventType = "user_created"
	UserAuditUserDeleted         UserAuditEventType = "user_deleted"
	UserAuditRoleChanged         UserAuditEventType = "role_changed"
	UserAuditPermissionsChanged  UserAuditEventType = "permissions_changed"
	UserAuditLogin               UserAuditEventType = "login"
	UserAuditInviteSent          UserAuditEventType = "invite_sent"
	UserAuditInviteAccepted      UserAuditEventType = "invite_accepted"
	UserAuditPasswordReset       UserAuditEventType = "password_reset"
	UserAuditGroupCreated        UserAuditEventType = "group_created"
	UserAuditGroupDeleted        UserAuditEventType = "group_deleted"
	UserAuditGroupMemberAdded    UserAuditEventType = "group_member_added"
	UserAuditGroupMemberRemoved  UserAuditEventType = "group_member_removed"
)

// UserAuditEvent is stored at /tenants/{tenant_id}/user_audit_log/{event_id}.
// Immutable after creation.
type UserAuditEvent struct {
	ID          string                 `json:"id"           firestore:"id"`
	TenantID    string                 `json:"tenant_id"    firestore:"tenant_id"`
	ActorUID    string                 `json:"actor_uid"    firestore:"actor_uid"`
	ActorEmail  string                 `json:"actor_email"  firestore:"actor_email"`
	EventType   UserAuditEventType     `json:"event_type"   firestore:"event_type"`
	TargetUID   string                 `json:"target_uid,omitempty"    firestore:"target_uid,omitempty"`
	TargetEmail string                 `json:"target_email,omitempty"  firestore:"target_email,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"      firestore:"metadata,omitempty"`
	CreatedAt   time.Time              `json:"created_at"   firestore:"created_at"`
}

// ── Session 5: Security Settings ─────────────────────────────────────────────

// PasswordComplexity defines enforced password strength levels.
type PasswordComplexity string

const (
	PasswordComplexityNone    PasswordComplexity = "none"
	PasswordComplexityBasic   PasswordComplexity = "basic"   // 8+ chars
	PasswordComplexityNormal  PasswordComplexity = "normal"  // 8+ chars, mixed case + number
	PasswordComplexityStrong  PasswordComplexity = "strong"  // 10+ chars, mixed case + number + symbol
	PasswordComplexityComplex PasswordComplexity = "complex" // 12+ chars, above + no dictionary words
)

// SecuritySettings is stored at /tenants/{tenant_id}/settings/security.
type SecuritySettings struct {
	TenantID string `json:"tenant_id" firestore:"tenant_id"`

	// Password Complexity
	PasswordComplexity PasswordComplexity `json:"password_complexity" firestore:"password_complexity"`

	// Password Expiry
	PasswordExpiryEnabled      bool `json:"password_expiry_enabled"      firestore:"password_expiry_enabled"`
	PasswordExpiryDays         int  `json:"password_expiry_days"         firestore:"password_expiry_days"`
	PreventPasswordReuse       bool `json:"prevent_password_reuse"       firestore:"prevent_password_reuse"`
	PreventPasswordReuseCount  int  `json:"prevent_password_reuse_count" firestore:"prevent_password_reuse_count"`

	// Login Methods
	EmailPasswordEnabled bool `json:"email_password_enabled" firestore:"email_password_enabled"` // always true
	GoogleSSOEnabled     bool `json:"google_sso_enabled"     firestore:"google_sso_enabled"`
	MicrosoftSSOEnabled  bool `json:"microsoft_sso_enabled"  firestore:"microsoft_sso_enabled"`

	// Data & Privacy
	SupportAccessEnabled bool `json:"support_access_enabled" firestore:"support_access_enabled"`

	UpdatedAt time.Time `json:"updated_at" firestore:"updated_at"`
	UpdatedBy string    `json:"updated_by,omitempty" firestore:"updated_by,omitempty"`
}

// DefaultSecuritySettings returns a safe default for new tenants.
func DefaultSecuritySettings(tenantID string) SecuritySettings {
	return SecuritySettings{
		TenantID:              tenantID,
		PasswordComplexity:    PasswordComplexityBasic,
		PasswordExpiryEnabled: false,
		PasswordExpiryDays:    90,
		EmailPasswordEnabled:  true,
		SupportAccessEnabled:  true,
		UpdatedAt:             time.Now(),
	}
}
