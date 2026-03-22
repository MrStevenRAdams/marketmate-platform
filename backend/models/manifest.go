package models

import "time"

// ============================================================================
// MANIFEST MODEL
// ============================================================================
// A Manifest represents an end-of-day carrier manifest / close-out record.
// Each carrier produces one manifest per session; it bundles all shipments
// despatched that day into a single collection document for the carrier driver.
//
// Firestore: tenants/{tenant_id}/manifests/{manifest_id}
// ============================================================================

// ManifestStatus lifecycle values
const (
	ManifestStatusPending    = "pending"    // Requested but not yet confirmed by carrier
	ManifestStatusGenerated  = "generated"  // Successfully produced; document available
	ManifestStatusFailed     = "failed"     // Carrier rejected or API error
)

// Manifest is the Firestore-persisted record for one carrier's end-of-day manifest.
type Manifest struct {
	ManifestID string `json:"manifest_id" firestore:"manifest_id"`
	TenantID   string `json:"tenant_id"   firestore:"tenant_id"`

	// CarrierID identifies which carrier this manifest belongs to (e.g. "royal-mail").
	CarrierID   string `json:"carrier_id"   firestore:"carrier_id"`
	CarrierName string `json:"carrier_name" firestore:"carrier_name"`

	// DocumentFormat is "pdf" or "csv".
	DocumentFormat string `json:"document_format" firestore:"document_format"`

	// DownloadURL is the GCS signed URL to the manifest document.
	// Empty if not yet uploaded or upload failed.
	DownloadURL string `json:"download_url,omitempty" firestore:"download_url,omitempty"`

	// StoragePath is the internal GCS object path; used to regenerate signed URLs.
	StoragePath string `json:"storage_path,omitempty" firestore:"storage_path,omitempty"`

	// ShipmentIDs are the MarketMate shipment IDs included in this manifest.
	ShipmentIDs []string `json:"shipment_ids" firestore:"shipment_ids"`

	// ShipmentCount is the number of shipments in this manifest.
	ShipmentCount int `json:"shipment_count" firestore:"shipment_count"`

	// TotalWeightKg is the aggregate weight across all included shipments.
	TotalWeightKg float64 `json:"total_weight_kg" firestore:"total_weight_kg"`

	// TotalCost is the aggregate cost across all included shipments.
	TotalCost float64 `json:"total_cost"     firestore:"total_cost"`
	Currency  string  `json:"currency"       firestore:"currency"`

	// Status is one of ManifestStatus* constants above.
	Status string `json:"status" firestore:"status"`

	// ErrorMessage is populated when Status == ManifestStatusFailed.
	ErrorMessage string `json:"error_message,omitempty" firestore:"error_message,omitempty"`

	// ManifestDate is the despatch date this manifest covers (YYYY-MM-DD).
	ManifestDate string `json:"manifest_date" firestore:"manifest_date"`

	// CreatedAt is when the manifest record was first created.
	CreatedAt time.Time `json:"created_at" firestore:"created_at"`

	// UpdatedAt is the last modification time (e.g. when download URL was set).
	UpdatedAt time.Time `json:"updated_at" firestore:"updated_at"`
}
