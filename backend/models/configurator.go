package models

import "time"

// ============================================================================
// CONFIGURATOR MODELS — SESSION 1 (CFG-01)
// ============================================================================
// A Configurator is a named, reusable settings container that defines — for a
// specific channel and product type — the category, shipping settings, per-
// attribute data sources, variation schema, and default field values.
//
// Firestore paths:
//   tenants/{tenantID}/configurators/{configuratorID}
//   tenants/{tenantID}/configurator_listings/{configuratorID}_{listingID}
//   tenants/{tenantID}/revise_jobs/{jobID}
// ============================================================================

// AttributeDefault defines how a single attribute's value is sourced when
// the configurator is applied to a listing.
type AttributeDefault struct {
	AttributeName string `firestore:"attribute_name" json:"attribute_name"`
	// Source is "extended_property" (read from product extended data by EPKey)
	// or "default_value" (use the literal DefaultValue string).
	Source       string `firestore:"source" json:"source"`
	EPKey        string `firestore:"ep_key,omitempty" json:"ep_key,omitempty"`
	DefaultValue string `firestore:"default_value,omitempty" json:"default_value,omitempty"`
}

// Configurator is the main settings container stored in Firestore.
type Configurator struct {
	ConfiguratorID      string             `firestore:"configurator_id" json:"configurator_id"`
	TenantID            string             `firestore:"tenant_id" json:"tenant_id"`
	Name                string             `firestore:"name" json:"name"`
	Channel             string             `firestore:"channel" json:"channel"` // e.g. "amazon", "ebay", "shopify"
	ChannelCredentialID string             `firestore:"channel_credential_id,omitempty" json:"channel_credential_id,omitempty"`
	CategoryID          string             `firestore:"category_id,omitempty" json:"category_id,omitempty"`
	CategoryPath        string             `firestore:"category_path,omitempty" json:"category_path,omitempty"`
	ShippingDefaults    map[string]any     `firestore:"shipping_defaults,omitempty" json:"shipping_defaults,omitempty"`
	AttributeDefaults   []AttributeDefault `firestore:"attribute_defaults,omitempty" json:"attribute_defaults,omitempty"`
	VariationSchema     []string           `firestore:"variation_schema,omitempty" json:"variation_schema,omitempty"`
	CreatedAt           time.Time          `firestore:"created_at" json:"created_at"`
	UpdatedAt           time.Time          `firestore:"updated_at" json:"updated_at"`
}

// ConfiguratorListing is the join document linking a configurator to a listing.
// The Firestore document ID is "{configuratorID}_{listingID}".
type ConfiguratorListing struct {
	ConfiguratorID string    `firestore:"configurator_id" json:"configurator_id"`
	ListingID      string    `firestore:"listing_id" json:"listing_id"`
	CreatedAt      time.Time `firestore:"created_at" json:"created_at"`
}

// ConfiguratorWithStats augments Configurator with computed counts derived
// from the linked listings. Returned by ListConfigurators.
type ConfiguratorWithStats struct {
	Configurator
	ListingCount   int `json:"listing_count"`
	ErrorCount     int `json:"error_count"`
	InProcessCount int `json:"in_process_count"`
}

// ConfiguratorDetail is returned by GetConfigurator — includes the full
// Configurator plus its linked listings.
type ConfiguratorDetail struct {
	Configurator
	LinkedListings []map[string]any `json:"linked_listings"`
}

// ReviseJob tracks the progress and result of a bulk configurator revise
// operation. Stored in Firestore; the endpoint returns it synchronously.
type ReviseJob struct {
	JobID          string    `firestore:"job_id" json:"job_id"`
	TenantID       string    `firestore:"tenant_id" json:"tenant_id"`
	ConfiguratorID string    `firestore:"configurator_id" json:"configurator_id"`
	Fields         []string  `firestore:"fields" json:"fields"`
	Status         string    `firestore:"status" json:"status"` // "completed" | "failed"
	Total          int       `firestore:"total" json:"total"`
	Succeeded      int       `firestore:"succeeded" json:"succeeded"`
	Failed         int       `firestore:"failed" json:"failed"`
	Errors         []string  `firestore:"errors,omitempty" json:"errors,omitempty"`
	CreatedAt      time.Time `firestore:"created_at" json:"created_at"`
	UpdatedAt      time.Time `firestore:"updated_at" json:"updated_at"`
}

// ValidReviseFields is the set of field names accepted by the revise endpoint.
var ValidReviseFields = map[string]bool{
	"title":       true,
	"description": true,
	"price":       true,
	"attributes":  true,
	"images":      true,
	"category":    true,
	"shipping":    true,
}
