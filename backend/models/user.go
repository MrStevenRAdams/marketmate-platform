package models

import "time"

// ============================================================================
// MODULE K — USERS, ROLES, BILLING & USAGE
// ============================================================================

// ── Roles ────────────────────────────────────────────────────────────────────

type Role string

const (
	RoleOwner   Role = "owner"   // Full control including billing & delete tenant
	RoleAdmin   Role = "admin"   // Full operational control, manage users, view billing
	RoleManager Role = "manager" // All operational features, no user/billing management
	RoleViewer  Role = "viewer"  // Read-only access to everything
)

func (r Role) IsValid() bool {
	switch r {
	case RoleOwner, RoleAdmin, RoleManager, RoleViewer:
		return true
	}
	return false
}

// Can reports whether this role can perform an action
func (r Role) Can(action string) bool {
	permissions := map[string][]Role{
		"read":               {RoleOwner, RoleAdmin, RoleManager, RoleViewer},
		"write":              {RoleOwner, RoleAdmin, RoleManager},
		"dispatch":           {RoleOwner, RoleAdmin, RoleManager},
		"manage_users":       {RoleOwner, RoleAdmin},
		"view_billing":       {RoleOwner, RoleAdmin},
		"manage_billing":     {RoleOwner},
		"delete_tenant":      {RoleOwner},
		"invite_users":       {RoleOwner, RoleAdmin},
		"change_user_roles":  {RoleOwner, RoleAdmin},
	}
	allowed, ok := permissions[action]
	if !ok {
		return false
	}
	for _, a := range allowed {
		if a == r {
			return true
		}
	}
	return false
}

// ── Plans ─────────────────────────────────────────────────────────────────────

type PlanID string

const (
	PlanStarterS  PlanID = "starter_s"
	PlanStarterM  PlanID = "starter_m"
	PlanStarterL  PlanID = "starter_l"
	PlanPremium   PlanID = "premium"
	PlanEnterprise PlanID = "enterprise"
)

type BillingModel string

const (
	BillingModelCredits   BillingModel = "credits"    // Starter — credit cap
	BillingModelPerOrder  BillingModel = "per_order"  // Premium — base + per order
	BillingModelGMV       BillingModel = "gmv_percent" // Enterprise — base + % GMV
)

// Plan defines a billing plan. Stored in /system/plans/{plan_id} so sales
// team can update prices without code deployment.
type Plan struct {
	PlanID        PlanID       `json:"plan_id"        firestore:"plan_id"`
	Name          string       `json:"name"           firestore:"name"`
	BillingModel  BillingModel `json:"billing_model"  firestore:"billing_model"`
	CreditsPerMonth *int64     `json:"credits_per_month,omitempty" firestore:"credits_per_month,omitempty"` // nil = unlimited
	PriceGBP      float64     `json:"price_gbp"      firestore:"price_gbp"`      // Monthly base price
	PerOrderGBP   *float64    `json:"per_order_gbp,omitempty"  firestore:"per_order_gbp,omitempty"`
	GMVPercent    *float64    `json:"gmv_percent,omitempty"    firestore:"gmv_percent,omitempty"`
	IsActive      bool        `json:"is_active"      firestore:"is_active"`
	SortOrder     int         `json:"sort_order"     firestore:"sort_order"`
	UpdatedAt     time.Time   `json:"updated_at"     firestore:"updated_at"`
	UpdatedBy     string      `json:"updated_by"     firestore:"updated_by"`
}

// PlanOverride holds sales-negotiated values for a specific tenant.
// Stored at /tenants/{tenant_id}/plan_overrides/current
// If present, these values override the base plan for billing calculation.
type PlanOverride struct {
	TenantID       string   `json:"tenant_id"        firestore:"tenant_id"`
	MonthlyBaseGBP *float64 `json:"monthly_base_gbp,omitempty" firestore:"monthly_base_gbp,omitempty"`
	PerOrderGBP    *float64 `json:"per_order_gbp,omitempty"    firestore:"per_order_gbp,omitempty"`
	GMVPercent     *float64 `json:"gmv_percent,omitempty"      firestore:"gmv_percent,omitempty"`
	CustomCreditLimit *int64 `json:"custom_credit_limit,omitempty" firestore:"custom_credit_limit,omitempty"`
	Notes          string   `json:"notes"            firestore:"notes"`
	SetBy          string   `json:"set_by"           firestore:"set_by"` // admin user_id
	SetAt          time.Time `json:"set_at"          firestore:"set_at"`
}

// ── Credit Rates ──────────────────────────────────────────────────────────────

// CreditRates defines how many credits each event type costs.
// Stored at /system/credit_rates — editable without code deployment.
type CreditRates struct {
	AITokensPer1k    float64 `json:"ai_tokens_per_1k"   firestore:"ai_tokens_per_1k"`   // per 1000 tokens
	APICall          float64 `json:"api_call"           firestore:"api_call"`           // per outbound marketplace API call
	OrderSync        float64 `json:"order_sync"         firestore:"order_sync"`         // per order imported
	ListingPublish   float64 `json:"listing_publish"    firestore:"listing_publish"`    // per listing created/updated
	ShipmentLabel    float64 `json:"shipment_label"     firestore:"shipment_label"`     // per label generated
	DataExport       float64 `json:"data_export"        firestore:"data_export"`       // per export job
	UpdatedAt        time.Time `json:"updated_at"       firestore:"updated_at"`
	UpdatedBy        string  `json:"updated_by"         firestore:"updated_by"`
}

// DefaultCreditRates returns the system defaults used when no config doc exists
func DefaultCreditRates() CreditRates {
	return CreditRates{
		AITokensPer1k:  1.0,
		APICall:        0.1,
		OrderSync:      1.0,
		ListingPublish: 2.0,
		ShipmentLabel:  1.0,
		DataExport:     5.0,
	}
}

// ── Usage Events ──────────────────────────────────────────────────────────────

type UsageEventType string

const (
	UsageAITokens      UsageEventType = "ai_tokens"
	UsageAPICall       UsageEventType = "api_call"
	UsageOrderSync     UsageEventType = "order_sync"
	UsageListingPublish UsageEventType = "listing_publish"
	UsageShipmentLabel UsageEventType = "shipment_label"
	UsageDataExport    UsageEventType = "data_export"
)

type ActorType string

const (
	ActorSystem ActorType = "system"
	ActorUser   ActorType = "user"
)

// UsageEvent is passed to RecordUsage() by handlers.
// Contains the raw facts of what happened — metering service computes credits.
type UsageEvent struct {
	Type        UsageEventType `json:"type"`
	SubType     string         `json:"sub_type"`      // e.g. "amazon_order_sync_poll", "gemini_listing_gen"
	Quantity    float64        `json:"quantity"`      // raw units: token count, order count, call count
	Actor       ActorType      `json:"actor"`
	UserID      string         `json:"user_id,omitempty"`
	Endpoint    string         `json:"endpoint,omitempty"`
	Marketplace string         `json:"marketplace,omitempty"`
	OrderCount  int            `json:"order_count,omitempty"` // for ledger order tracking
	GMVValue    float64        `json:"gmv_value,omitempty"`   // for enterprise GMV tracking
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// AuditLogEntry is the immutable record written to /tenants/{id}/audit_log/{event_id}
// NEVER updated after creation.
type AuditLogEntry struct {
	EventID       string         `json:"event_id"        firestore:"event_id"`
	TenantID      string         `json:"tenant_id"       firestore:"tenant_id"`

	// What happened
	Type          UsageEventType `json:"type"            firestore:"type"`
	SubType       string         `json:"sub_type"        firestore:"sub_type"`
	Quantity      float64        `json:"quantity"        firestore:"quantity"`
	Unit          string         `json:"unit"            firestore:"unit"`

	// What it cost
	CreditsCharged float64       `json:"credits_charged" firestore:"credits_charged"`
	RateApplied    float64       `json:"rate_applied"    firestore:"rate_applied"`    // rate at time of event

	// Context
	Actor         ActorType      `json:"actor"           firestore:"actor"`
	UserID        string         `json:"user_id,omitempty" firestore:"user_id,omitempty"`
	Endpoint      string         `json:"endpoint,omitempty" firestore:"endpoint,omitempty"`
	Marketplace   string         `json:"marketplace,omitempty" firestore:"marketplace,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty" firestore:"metadata,omitempty"`

	// Ledger snapshot at time of event
	LedgerPeriod  string         `json:"ledger_period"   firestore:"ledger_period"` // "2026-02"
	BalanceBefore float64        `json:"balance_before"  firestore:"balance_before"`
	BalanceAfter  float64        `json:"balance_after"   firestore:"balance_after"`

	// Immutable timestamp — no updated_at
	OccurredAt    time.Time      `json:"occurred_at"     firestore:"occurred_at"`
}

// ── Credit Ledger ─────────────────────────────────────────────────────────────

type LedgerStatus string

const (
	LedgerActive         LedgerStatus = "active"
	LedgerQuotaExceeded  LedgerStatus = "quota_exceeded"
	LedgerClosed         LedgerStatus = "closed"
	LedgerBilled         LedgerStatus = "billed"
)

// CreditLedger is the running balance document for a tenant's billing period.
// Stored at /tenants/{tenant_id}/credit_ledger/{YYYY-MM}
// Updated atomically via Firestore transaction with every usage event.
type CreditLedger struct {
	TenantID         string       `json:"tenant_id"          firestore:"tenant_id"`
	Period           string       `json:"period"             firestore:"period"`           // "2026-02"
	PlanID           PlanID       `json:"plan_id"            firestore:"plan_id"`

	// Credit balance (starter plans only — nil fields for premium/enterprise)
	CreditsAllocated *float64     `json:"credits_allocated,omitempty" firestore:"credits_allocated,omitempty"`
	CreditsUsed      float64      `json:"credits_used"       firestore:"credits_used"`
	CreditsRemaining *float64     `json:"credits_remaining,omitempty" firestore:"credits_remaining,omitempty"`

	// Operational counters (always tracked, used for premium/enterprise billing)
	OrdersProcessed  int          `json:"orders_processed"   firestore:"orders_processed"`
	GMVTotalGBP      float64      `json:"gmv_total_gbp"      firestore:"gmv_total_gbp"`
	APICallsTotal    int          `json:"api_calls_total"    firestore:"api_calls_total"`
	LabelsGenerated  int          `json:"labels_generated"   firestore:"labels_generated"`
	ListingsPublished int         `json:"listings_published" firestore:"listings_published"`

	// Credit breakdown by type
	Breakdown struct {
		AITokens       float64 `json:"ai_tokens"        firestore:"ai_tokens"`
		APICalls       float64 `json:"api_calls"        firestore:"api_calls"`
		OrderSyncs     float64 `json:"order_syncs"      firestore:"order_syncs"`
		ListingPublish float64 `json:"listing_publish"  firestore:"listing_publish"`
		ShipmentLabels float64 `json:"shipment_labels"  firestore:"shipment_labels"`
		DataExports    float64 `json:"data_exports"     firestore:"data_exports"`
	} `json:"breakdown" firestore:"breakdown"`

	// State
	Status           LedgerStatus `json:"status"             firestore:"status"`
	QuotaExceededAt  *time.Time   `json:"quota_exceeded_at,omitempty" firestore:"quota_exceeded_at,omitempty"`
	WarningSentAt    *time.Time   `json:"warning_sent_at,omitempty"   firestore:"warning_sent_at,omitempty"`

	// Billing
	BillAmountGBP    *float64     `json:"bill_amount_gbp,omitempty"  firestore:"bill_amount_gbp,omitempty"`
	BillComputedAt   *time.Time   `json:"bill_computed_at,omitempty" firestore:"bill_computed_at,omitempty"`
	PayPalInvoiceID  string       `json:"paypal_invoice_id,omitempty" firestore:"paypal_invoice_id,omitempty"`

	// Period
	PeriodStart      time.Time    `json:"period_start"       firestore:"period_start"`
	PeriodEnd        time.Time    `json:"period_end"         firestore:"period_end"`
	CreatedAt        time.Time    `json:"created_at"         firestore:"created_at"`
	UpdatedAt        time.Time    `json:"updated_at"         firestore:"updated_at"`
}

// ── Global Users ──────────────────────────────────────────────────────────────

// GlobalUser is stored at /global_users/{user_id}
// One document per real person, regardless of how many tenants they belong to.
type GlobalUser struct {
	UserID        string    `json:"user_id"        firestore:"user_id"`
	FirebaseUID   string    `json:"firebase_uid"   firestore:"firebase_uid"`
	Email         string    `json:"email"          firestore:"email"`
	DisplayName   string    `json:"display_name"   firestore:"display_name"`
	AvatarURL     string    `json:"avatar_url,omitempty"     firestore:"avatar_url,omitempty"`
	Phone         string    `json:"phone,omitempty"          firestore:"phone,omitempty"`
	PhoneVerified bool      `json:"phone_verified"           firestore:"phone_verified"`
	PhoneChannel  string    `json:"phone_channel,omitempty"  firestore:"phone_channel,omitempty"` // "sms" | "whatsapp"
	NotifPrefs    NotifPrefs `json:"notif_prefs"             firestore:"notif_prefs"`
	CreatedAt     time.Time `json:"created_at"     firestore:"created_at"`
	LastLoginAt   time.Time `json:"last_login_at"  firestore:"last_login_at"`
}

// NotifPrefs controls how a user receives messaging assignment alerts.
type NotifPrefs struct {
	Email    bool `json:"email"    firestore:"email"`
	SMS      bool `json:"sms"      firestore:"sms"`
	WhatsApp bool `json:"whatsapp" firestore:"whatsapp"`
}

// ── Tenant Membership ─────────────────────────────────────────────────────────

type MembershipStatus string

const (
	MembershipActive    MembershipStatus = "active"
	MembershipInvited   MembershipStatus = "invited"   // accepted invitation, not yet logged in
	MembershipSuspended MembershipStatus = "suspended"
)

// AllPermissionKeys lists every granular permission key supported by I-001.
// When a key is absent from a membership's Permissions map the system falls
// back to the role-based default (see middleware.RequirePermission).
var AllPermissionKeys = []string{
	// General
	"general.topbar",
	"general.sync_status",
	"general.account_management",
	"general.notifications",
	// Inventory
	"inventory.adjust",
	"inventory.stock_adjustments",
	"products.delete",
	"inventory.suppliers",
	"inventory.purchase_orders",
	"inventory.stock_takes",
	// Orders
	"orders.create",
	"orders.delete",
	"orders.merge",
	"orders.split",
	"orders.cancel",
	"orders.view",
	"orders.edit",
	"orders.refund",
	// Shipping
	"dispatch.create",
	"shipping.labels",
	"shipping.services",
	"shipping.tracking",
	// Dashboards
	"reports.view",
	"reports.export",
	"reports.financial",
	// Email
	"email.send",
	"email.send_adhoc",
	"email.resend",
	"email.templates",
	"email.view_sent",
	"email.accounts",
	// Apps
	"apps.macros",
	"apps.installed",
	"apps.automation_logs",
	// Settings
	"settings.configurators",
	"settings.import_export",
	"settings.currency",
	"settings.team",
	"settings.channels",
	"settings.general",
	"settings.data_purge",
	"settings.extract",
	"settings.templates",
	"settings.automation_logs",
	"settings.countries",
	// Legacy / billing
	"rmas.authorise",
	"billing.manage",
}

// RoleDefaultPermissions returns the default true/false for a permission key
// based purely on role, used as the fallback when no explicit override exists.
func RoleDefaultPermissions(role Role) map[string]bool {
	isViewer := role == RoleViewer
	isAdminOrOwner := role == RoleOwner || role == RoleAdmin
	isOwner := role == RoleOwner
	defaults := map[string]bool{
		// General
		"general.topbar":           true,
		"general.sync_status":      true,
		"general.account_management": isAdminOrOwner,
		"general.notifications":    true,
		// Inventory
		"inventory.adjust":         !isViewer,
		"inventory.stock_adjustments": !isViewer,
		"products.delete":          isAdminOrOwner,
		"inventory.suppliers":      !isViewer,
		"inventory.purchase_orders": !isViewer,
		"inventory.stock_takes":    !isViewer,
		// Orders
		"orders.create":    !isViewer,
		"orders.delete":    isAdminOrOwner,
		"orders.merge":     !isViewer,
		"orders.split":     !isViewer,
		"orders.cancel":    !isViewer,
		"orders.view":      true,
		"orders.edit":      !isViewer,
		"orders.refund":    isAdminOrOwner,
		// Shipping
		"dispatch.create":  !isViewer,
		"shipping.labels":  !isViewer,
		"shipping.services": isAdminOrOwner,
		"shipping.tracking": !isViewer,
		// Dashboards
		"reports.view":     true,
		"reports.export":   !isViewer,
		"reports.financial": isAdminOrOwner,
		// Email
		"email.send":       !isViewer,
		"email.send_adhoc": !isViewer,
		"email.resend":     !isViewer,
		"email.templates":  isAdminOrOwner,
		"email.view_sent":  !isViewer,
		"email.accounts":   isAdminOrOwner,
		// Apps
		"apps.macros":           isAdminOrOwner,
		"apps.installed":        !isViewer,
		"apps.automation_logs":  !isViewer,
		// Settings
		"settings.configurators":  isAdminOrOwner,
		"settings.import_export":  !isViewer,
		"settings.currency":       isAdminOrOwner,
		"settings.team":           isAdminOrOwner,
		"settings.channels":       isAdminOrOwner,
		"settings.general":        isAdminOrOwner,
		"settings.data_purge":     isOwner,
		"settings.extract":        !isViewer,
		"settings.templates":      isAdminOrOwner,
		"settings.automation_logs": !isViewer,
		"settings.countries":      isAdminOrOwner,
		// Legacy
		"rmas.authorise":  !isViewer,
		"billing.manage":  isOwner,
	}
	return defaults
}

// UserMembership is stored at /user_memberships/{membership_id}
// One per user+tenant combination. Allows one user to be in many tenants.
type UserMembership struct {
	MembershipID   string           `json:"membership_id"   firestore:"membership_id"`
	UserID         string           `json:"user_id"         firestore:"user_id"`
	TenantID       string           `json:"tenant_id"       firestore:"tenant_id"`
	Role           Role             `json:"role"            firestore:"role"`
	Status         MembershipStatus `json:"status"          firestore:"status"`
	InvitedBy      string           `json:"invited_by,omitempty"  firestore:"invited_by,omitempty"`
	InvitedEmail   string           `json:"invited_email,omitempty" firestore:"invited_email,omitempty"`
	JoinedAt       *time.Time       `json:"joined_at,omitempty"   firestore:"joined_at,omitempty"`
	CreatedAt      time.Time        `json:"created_at"      firestore:"created_at"`
	UpdatedAt      time.Time        `json:"updated_at"      firestore:"updated_at"`
	// Permissions holds explicit per-action overrides. Keys from AllPermissionKeys.
	// Absent key → fall back to RoleDefaultPermissions(Role)[key].
	Permissions    map[string]bool  `json:"permissions,omitempty" firestore:"permissions,omitempty"`
	// GroupIDs lists the UserGroup IDs this member belongs to.
	GroupIDs       []string         `json:"group_ids,omitempty" firestore:"group_ids,omitempty"`
	// DisplayNameHint is set at invite time so the invitee name can be pre-populated.
	DisplayNameHint string          `json:"display_name_hint,omitempty" firestore:"display_name_hint,omitempty"`
	// MessagingNotifPrefs holds per-member messaging notification preferences.
	MessagingNotifPrefs *MessagingNotifPrefs `json:"messaging_notif_prefs,omitempty" firestore:"messaging_notif_prefs,omitempty"`
}

// MessagingNotifPrefs controls how a team member receives messaging assignment alerts.
type MessagingNotifPrefs struct {
	Email    string   `json:"email,omitempty"    firestore:"email,omitempty"`    // override email address
	Phone    string   `json:"phone,omitempty"    firestore:"phone,omitempty"`    // phone number for SMS/WhatsApp
	Channels []string `json:"channels,omitempty" firestore:"channels,omitempty"` // ["email","sms","whatsapp"]
}

// ── Invitations ───────────────────────────────────────────────────────────────

// TenantInvitation is stored at /tenant_invitations/{token}
// Token is a 32-byte cryptographically random hex string sent in the invite email.
type TenantInvitation struct {
	Token        string    `json:"token"         firestore:"token"`
	TenantID     string    `json:"tenant_id"     firestore:"tenant_id"`
	TenantName   string    `json:"tenant_name"   firestore:"tenant_name"` // denormalised for invite email
	InvitedEmail string    `json:"invited_email" firestore:"invited_email"`
	Role         Role      `json:"role"          firestore:"role"`
	InvitedBy    string    `json:"invited_by"    firestore:"invited_by"` // user_id
	// DisplayNameHint is optionally set so the invitee display name is pre-filled on acceptance.
	DisplayNameHint     string          `json:"display_name_hint,omitempty" firestore:"display_name_hint,omitempty"`
	// PermissionOverrides are optional per-permission settings applied on invitation acceptance.
	PermissionOverrides map[string]bool `json:"permission_overrides,omitempty" firestore:"permission_overrides,omitempty"`
	ExpiresAt    time.Time `json:"expires_at"    firestore:"expires_at"`
	Used         bool      `json:"used"          firestore:"used"`
	UsedAt       *time.Time `json:"used_at,omitempty" firestore:"used_at,omitempty"`
	CreatedAt    time.Time `json:"created_at"    firestore:"created_at"`
}

// ── Tenant (extended) ─────────────────────────────────────────────────────────

type TenantPlanStatus string

const (
	PlanStatusTrialing  TenantPlanStatus = "trialing"
	PlanStatusActive    TenantPlanStatus = "active"
	PlanStatusPastDue   TenantPlanStatus = "past_due"
	PlanStatusSuspended TenantPlanStatus = "suspended"
	PlanStatusCancelled TenantPlanStatus = "cancelled"
)

// Tenant extends the existing TenantAccount with plan and billing fields.
// Stored at /tenants/{tenant_id} (same collection as before)
type Tenant struct {
	TenantID    string           `json:"tenant_id"    firestore:"tenant_id"`
	Name        string           `json:"name"         firestore:"name"`
	Slug        string           `json:"slug"         firestore:"slug"`
	OwnerUserID string           `json:"owner_user_id" firestore:"owner_user_id"`

	// Plan
	PlanID      PlanID           `json:"plan_id"      firestore:"plan_id"`
	PlanStatus  TenantPlanStatus `json:"plan_status"  firestore:"plan_status"`
	TrialEndsAt *time.Time       `json:"trial_ends_at,omitempty" firestore:"trial_ends_at,omitempty"`
	PlanStartedAt *time.Time     `json:"plan_started_at,omitempty" firestore:"plan_started_at,omitempty"`

	// Presentation
	Initials    string           `json:"initials"     firestore:"initials"`
	Color       string           `json:"color"        firestore:"color"`

	// Onboarding
	SetupComplete  bool   `json:"setup_complete"  firestore:"setup_complete"`
	ReferralSource string `json:"referral_source,omitempty" firestore:"referral_source,omitempty"` // temu, ebay, organic, etc.

	// Temu Wizard State
	TemuWizardStage    string   `json:"temu_wizard_stage,omitempty" firestore:"temu_wizard_stage,omitempty"`       // connected, importing, awaiting_upload, generating, reviewing, completed
	SourceChannel      string   `json:"source_channel,omitempty" firestore:"source_channel,omitempty"`             // amazon, ebay
	AdditionalChannels []string `json:"additional_channels,omitempty" firestore:"additional_channels,omitempty"`   // extra channels connected in wizard
	EstimatedMonthlyOrders int  `json:"estimated_monthly_orders,omitempty" firestore:"estimated_monthly_orders,omitempty"`

	// AI Credits
	FreeCreditsUsed  int `json:"free_credits_used" firestore:"free_credits_used"`
	FreeCreditsLimit int `json:"free_credits_limit" firestore:"free_credits_limit"` // default 100 for temu referrals

	// Subscription / Billing Tier
	SubscriptionPlan string `json:"subscription_plan,omitempty" firestore:"subscription_plan,omitempty"` // starter_s, starter_m, starter_l, premium, enterprise
	MonthlyCreditsUsed int  `json:"monthly_credits_used" firestore:"monthly_credits_used"`
	MonthlyCreditsResetAt *time.Time `json:"monthly_credits_reset_at,omitempty" firestore:"monthly_credits_reset_at,omitempty"`

	CreatedAt   time.Time        `json:"created_at"   firestore:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"   firestore:"updated_at"`
}

// ── Billing ───────────────────────────────────────────────────────────────────

// BillingRecord is stored at /tenants/{tenant_id}/billing/current
type BillingRecord struct {
	TenantID              string    `json:"tenant_id"               firestore:"tenant_id"`
	PayPalSubscriptionID  string    `json:"paypal_subscription_id,omitempty"  firestore:"paypal_subscription_id,omitempty"`
	StripeSubscriptionID  string    `json:"stripe_subscription_id,omitempty"  firestore:"stripe_subscription_id,omitempty"`
	StripeCustomerID      string    `json:"stripe_customer_id,omitempty"      firestore:"stripe_customer_id,omitempty"`
	PayPalPlanID          string    `json:"paypal_plan_id,omitempty"          firestore:"paypal_plan_id,omitempty"`
	PayPalPayerID         string    `json:"paypal_payer_id,omitempty"         firestore:"paypal_payer_id,omitempty"`
	BillingEmail          string    `json:"billing_email"           firestore:"billing_email"`
	BillingName           string    `json:"billing_name"            firestore:"billing_name"`
	NextBillingAt         *time.Time `json:"next_billing_at,omitempty" firestore:"next_billing_at,omitempty"`
	LastPaymentAt         *time.Time `json:"last_payment_at,omitempty" firestore:"last_payment_at,omitempty"`
	LastPaymentAmountGBP  float64   `json:"last_payment_amount_gbp" firestore:"last_payment_amount_gbp"`
	UpdatedAt             time.Time `json:"updated_at"              firestore:"updated_at"`
}

// ── API response types ────────────────────────────────────────────────────────

// MemberWithUser combines membership + user profile for the team management UI
type MemberWithUser struct {
	UserMembership
	Email       string   `json:"email"`
	DisplayName string   `json:"display_name"`
	AvatarURL   string   `json:"avatar_url,omitempty"`
	// GroupNames is populated so the UI can show group badges per member row.
	GroupNames  []string `json:"group_names,omitempty"`
}

// TenantWithPlan is returned to the frontend after login — includes computed plan info
type TenantWithPlan struct {
	Tenant
	Plan            *Plan            `json:"plan,omitempty"`
	PlanOverride    *PlanOverride    `json:"plan_override,omitempty"`
	CurrentLedger   *CreditLedger    `json:"current_ledger,omitempty"`
	UserRole        Role             `json:"user_role"`
	MembershipID    string           `json:"membership_id"`
}

// ── User Groups ───────────────────────────────────────────────────────────────

// UserGroup is stored at /tenants/{tenant_id}/user_groups/{group_id}.
// Permission resolution order: Role defaults → Group permissions (most permissive wins) → Individual overrides.
type UserGroup struct {
	ID          string          `json:"id"          firestore:"id"`
	TenantID    string          `json:"tenant_id"   firestore:"tenant_id"`
	Name        string          `json:"name"        firestore:"name"`
	Description string          `json:"description" firestore:"description"`
	// Permissions holds the set of permission keys this group grants.
	// Only keys set to true elevate permissions; groups never restrict below role defaults.
	Permissions map[string]bool `json:"permissions" firestore:"permissions"`
	// MemberIDs lists membership_id values belonging to this group.
	MemberIDs   []string        `json:"member_ids"  firestore:"member_ids"`
	CreatedAt   time.Time       `json:"created_at"  firestore:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"  firestore:"updated_at"`
}

// ResolvePermissions merges: role defaults → group permissions (most-permissive wins) → individual overrides.
func ResolvePermissions(role Role, groups []UserGroup, individualOverrides map[string]bool) map[string]bool {
	result := RoleDefaultPermissions(role)
	for _, g := range groups {
		for k, v := range g.Permissions {
			if v {
				result[k] = true
			}
		}
	}
	for k, v := range individualOverrides {
		result[k] = v
	}
	return result
}
