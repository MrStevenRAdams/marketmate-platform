// ============================================================================
// MODEL CHANGES REQUIRED — product.go
// ============================================================================
//
// Add the following fields to the Product struct.
// The Variant struct already has Alias *string — no change needed there.
//
// CHANGE 1: Add soft-delete and alias fields to the Product struct
// Location: directly after the `UpdatedBy` field at the bottom of Product struct
//
//   // Alias — alternate SKU for order routing. Available on all product types.
//   // Previously only surfaced on the variants UI; now also on simple/bundle products.
//   Alias     *string    `json:"alias,omitempty" firestore:"alias,omitempty"`
//
//   // Soft delete support for import
//   DeletedAt *time.Time `json:"deleted_at,omitempty" firestore:"deleted_at,omitempty"`
//   DeletedBy *string    `json:"deleted_by,omitempty" firestore:"deleted_by,omitempty"`
//
// ============================================================================
// REASONING
// ============================================================================
//
// 1. Alias on Product:
//    The Variant model already has `Alias *string`. Simple and bundle products
//    currently have no alias field on the struct — they store it in Attributes map.
//    Adding it as a first-class field makes export/import clean and avoids the
//    attribute map lookup hack currently in the import handler.
//
// 2. DeletedAt / DeletedBy:
//    The current codebase has no soft-delete on products — status = "archived" is
//    the closest thing. The import `delete=Y` column should set:
//      - status = "archived"
//      - deleted_at = time.Now()
//      - deleted_by = the importing user's ID (or "csv_import" if no user context)
//    This is recoverable via the UI (change status back to active/draft).
//    No schema migration is needed — Firestore is schemaless; existing docs
//    simply won't have these fields until they are written.
//
// 3. No changes needed to:
//    - ProductIdentifiers (unchanged)
//    - BundleComponent (unchanged)
//    - Variant (already has Alias)
//    - VariantPricing (unchanged)
//    - ProductAsset (unchanged)
//
// ============================================================================
// FULL DIFF (add these lines to the Product struct in models/product.go)
// ============================================================================

package models

// ADD to Product struct — after UpdatedBy *string field:

/*
	// Alias — alternate SKU used for order routing and de-duplication.
	// Mirrors the same field on Variant; available for all product types.
	Alias *string `json:"alias,omitempty" firestore:"alias,omitempty"`

	// Soft delete — set by import when delete=Y column is present.
	// Status is also set to "archived". Recoverable via the UI.
	DeletedAt *time.Time `json:"deleted_at,omitempty" firestore:"deleted_at,omitempty"`
	DeletedBy *string    `json:"deleted_by,omitempty" firestore:"deleted_by,omitempty"`
*/

// Also update UpdateProductRequest to expose these fields:
/*
type UpdateProductRequest struct {
	// ... existing fields ...
	Alias   *string `json:"alias,omitempty"`
	// (DeletedAt is internal — never in the update request DTO)
}
*/
