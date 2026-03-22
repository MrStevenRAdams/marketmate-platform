package models

import "time"

// ============================================================================
// SESSION 7 — APPLICATION STORE MODELS
// ============================================================================

// AppCategory groups apps in the store.
type AppCategory string

const (
	AppCategoryMacros     AppCategory = "macros"
	AppCategoryShipping   AppCategory = "shipping"
	AppCategoryAccounting AppCategory = "accounting"
	AppCategoryInventory  AppCategory = "inventory"
	AppCategoryEmail      AppCategory = "email"
	AppCategoryOther      AppCategory = "other"
)

// App represents an application in the store.
// Stored at /apps/{app_id} (global, not per-tenant).
type App struct {
	AppID       string      `json:"app_id"       firestore:"app_id"`
	Name        string      `json:"name"         firestore:"name"`
	Description string      `json:"description"  firestore:"description"`
	Developer   string      `json:"developer"    firestore:"developer"`
	Category    AppCategory `json:"category"     firestore:"category"`
	Type        string      `json:"type"         firestore:"type"` // "macro" | "integration"
	MacroType   string      `json:"macro_type,omitempty"   firestore:"macro_type,omitempty"`   // links to built-in macro
	IconEmoji   string      `json:"icon_emoji,omitempty"   firestore:"icon_emoji,omitempty"`
	Rating      float64     `json:"rating"       firestore:"rating"`   // display only, 0-5
	Pricing     string      `json:"pricing"      firestore:"pricing"`  // "free" | "included" | "$X/mo"
	IsBuiltIn   bool        `json:"is_built_in"  firestore:"is_built_in"`
	CreatedAt   time.Time   `json:"created_at"   firestore:"created_at"`
}

// InstalledApp records which apps a tenant has installed.
// Stored at /tenants/{tenant_id}/installed_apps/{app_id}.
type InstalledApp struct {
	AppID       string    `json:"app_id"       firestore:"app_id"`
	TenantID    string    `json:"tenant_id"    firestore:"tenant_id"`
	InstalledAt time.Time `json:"installed_at" firestore:"installed_at"`
	InstalledBy string    `json:"installed_by" firestore:"installed_by"` // user_id
	Enabled     bool      `json:"enabled"      firestore:"enabled"`
}

// BuiltInApps is the seed list of built-in macro applications.
var BuiltInApps = []App{
	{
		AppID:       "app_low_stock_notification",
		Name:        "Low Stock Notification",
		Description: "Sends an email alert when any product falls below its reorder point. Configurable per location and SMTP settings.",
		Developer:   "Marketmate",
		Category:    AppCategoryInventory,
		Type:        "macro",
		MacroType:   "low_stock_notification",
		IconEmoji:   "📦",
		Rating:      4.8,
		Pricing:     "included",
		IsBuiltIn:   true,
	},
	{
		AppID:       "app_export_shipping_labels",
		Name:        "Export Shipping Labels",
		Description: "Exports unprinted shipping label PDFs to a Dropbox folder. Supports individual or batched files per order.",
		Developer:   "Marketmate",
		Category:    AppCategoryShipping,
		Type:        "macro",
		MacroType:   "export_shipping_labels",
		IconEmoji:   "🏷️",
		Rating:      4.6,
		Pricing:     "included",
		IsBuiltIn:   true,
	},
	{
		AppID:       "app_import_tracking",
		Name:        "Import Tracking & Process Orders",
		Description: "Fetches tracking updates from shipping carriers and auto-processes orders where tracking is confirmed.",
		Developer:   "Marketmate",
		Category:    AppCategoryShipping,
		Type:        "macro",
		MacroType:   "import_tracking",
		IconEmoji:   "📡",
		Rating:      4.7,
		Pricing:     "included",
		IsBuiltIn:   true,
	},
	{
		AppID:       "app_send_emails",
		Name:        "Send Emails via Rules Engine",
		Description: "Trigger automated emails from rule actions. Configure SMTP, email templates, and trigger events.",
		Developer:   "Marketmate",
		Category:    AppCategoryEmail,
		Type:        "macro",
		MacroType:   "send_emails",
		IconEmoji:   "✉️",
		Rating:      4.9,
		Pricing:     "included",
		IsBuiltIn:   true,
	},
	{
		AppID:       "app_shipping_cost_to_service",
		Name:        "Shipping Cost to Service",
		Description: "Maps order shipping cost to a shipping service name based on configurable cost ranges.",
		Developer:   "Marketmate",
		Category:    AppCategoryShipping,
		Type:        "macro",
		MacroType:   "shipping_cost_to_service",
		IconEmoji:   "🚚",
		Rating:      4.5,
		Pricing:     "included",
		IsBuiltIn:   true,
	},
	{
		AppID:       "app_replace_diacritics",
		Name:        "Replace Diacritics",
		Description: "Automatically replaces accented characters in shipping addresses with ASCII equivalents (e.g. é→e, ü→u).",
		Developer:   "Marketmate",
		Category:    AppCategoryOther,
		Type:        "macro",
		MacroType:   "replace_diacritics",
		IconEmoji:   "🔡",
		Rating:      4.4,
		Pricing:     "included",
		IsBuiltIn:   true,
	},
	{
		AppID:       "app_postcode_spacing",
		Name:        "Postcode Spacing",
		Description: "Applies correct UK postcode formatting by ensuring a single space before the inward code (e.g. SW1A1AA → SW1A 1AA).",
		Developer:   "Marketmate",
		Category:    AppCategoryOther,
		Type:        "macro",
		MacroType:   "format_postcode",
		IconEmoji:   "📮",
		Rating:      4.3,
		Pricing:     "included",
		IsBuiltIn:   true,
	},
	{
		AppID:       "app_default_phone_number",
		Name:        "Default Phone Number",
		Description: "Sets a default phone number on orders where the customer phone is missing or empty.",
		Developer:   "Marketmate",
		Category:    AppCategoryOther,
		Type:        "macro",
		MacroType:   "default_phone_number",
		IconEmoji:   "📞",
		Rating:      4.2,
		Pricing:     "included",
		IsBuiltIn:   true,
	},
}

// ============================================================================
// SESSION 7 — MACRO CONFIGURATION SYSTEM EXTENSIONS
// ============================================================================

// MacroSchedule defines when a scheduled macro should run.
type MacroSchedule struct {
	Type            string     `json:"type"                       firestore:"type"` // one_time|daily|weekly|monthly|interval
	RunAt           *time.Time `json:"run_at,omitempty"           firestore:"run_at,omitempty"`
	IntervalMinutes int        `json:"interval_minutes,omitempty" firestore:"interval_minutes,omitempty"`
	DayOfWeek       int        `json:"day_of_week,omitempty"      firestore:"day_of_week,omitempty"`   // 0=Sunday
	DayOfMonth      int        `json:"day_of_month,omitempty"     firestore:"day_of_month,omitempty"` // 1-31
	TimeOfDay       string     `json:"time_of_day,omitempty"      firestore:"time_of_day,omitempty"`  // "HH:MM"
}

// RuleConfig is a named, independently-enabled parameter set for a macro.
type RuleConfig struct {
	ID      string                 `json:"id"      firestore:"id"`
	Name    string                 `json:"name"    firestore:"name"`
	Enabled bool                   `json:"enabled" firestore:"enabled"`
	Params  map[string]interface{} `json:"params"  firestore:"params"`
}
