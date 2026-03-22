package models

import "time"

// Product represents a product in the PIM system
type Product struct {
	// Identity
	ProductID string `json:"product_id" firestore:"product_id"`
	TenantID  string `json:"tenant_id" firestore:"tenant_id"`

	// Core Data
	Status      string  `json:"status" firestore:"status"` // draft, active, archived
	SKU         string  `json:"sku" firestore:"sku"`
	Title       string  `json:"title" firestore:"title"`
	Subtitle    *string `json:"subtitle,omitempty" firestore:"subtitle,omitempty"`
	Description *string `json:"description,omitempty" firestore:"description,omitempty"`
	Brand       *string `json:"brand,omitempty" firestore:"brand,omitempty"`

	// Product Type
	ProductType string  `json:"product_type" firestore:"product_type"` // simple, parent, variant, bundle
	ParentID    *string `json:"parent_id,omitempty" firestore:"parent_id,omitempty"`

	// Identifiers
	Identifiers *ProductIdentifiers `json:"identifiers,omitempty" firestore:"identifiers,omitempty"`

	// Classification
	CategoryIDs     []string               `json:"category_ids,omitempty" firestore:"category_ids,omitempty"`
	Tags            []string               `json:"tags,omitempty" firestore:"tags,omitempty"`
	KeyFeatures     []string               `json:"key_features,omitempty" firestore:"key_features,omitempty"`
	AttributeSetID  *string                `json:"attribute_set_id,omitempty" firestore:"attribute_set_id,omitempty"`
	Attributes      map[string]interface{} `json:"attributes,omitempty" firestore:"attributes,omitempty"`

	// Media
	Assets []ProductAsset `json:"assets,omitempty" firestore:"assets,omitempty"`

	// Dimensions & Weight (product-level)
	Dimensions *Dimensions `json:"dimensions,omitempty" firestore:"dimensions,omitempty"`
	Weight     *Weight     `json:"weight,omitempty" firestore:"weight,omitempty"`

	// Shipping Dimensions & Weight (package-level)
	ShippingDimensions *Dimensions `json:"shipping_dimensions,omitempty" firestore:"shipping_dimensions,omitempty"`
	ShippingWeight     *Weight     `json:"shipping_weight,omitempty" firestore:"shipping_weight,omitempty"`

	// Bundle-specific
	BundleComponents []BundleComponent `json:"bundle_components,omitempty" firestore:"bundle_components,omitempty"`

	// Suppliers — sourcing information (used for dropship / PO auto-generation)
	Suppliers []ProductSupplier `json:"suppliers,omitempty" firestore:"suppliers,omitempty"`

	// WMS / Storage
	StorageGroupID string `json:"storage_group_id,omitempty" firestore:"storage_group_id,omitempty"`

	// Lifecycle & Tracking
	// UseSerialNumbers — when true every unit must be assigned a unique serial
	// number on stock receipt and tracked individually (batch_type = "Serial").
	UseSerialNumbers bool `json:"use_serial_numbers" firestore:"use_serial_numbers"`
	// EndOfLife — when true the product is discontinued; new POs are blocked
	// and a warning is shown on listings and picking screens.
	EndOfLife bool `json:"end_of_life" firestore:"end_of_life"`

	// Compliance
	Compliance *ComplianceStatus `json:"compliance,omitempty" firestore:"compliance,omitempty"`

	// Readiness Flags
	ReadinessFlags *ReadinessFlags `json:"readiness_flags,omitempty" firestore:"readiness_flags,omitempty"`

	// Timestamps
	CreatedAt time.Time `json:"created_at" firestore:"created_at"`
	UpdatedAt time.Time `json:"updated_at" firestore:"updated_at"`
	CreatedBy *string   `json:"created_by,omitempty" firestore:"created_by,omitempty"`
	UpdatedBy *string   `json:"updated_by,omitempty" firestore:"updated_by,omitempty"`
}

type ProductIdentifiers struct {
	EAN  *string `json:"ean,omitempty" firestore:"ean,omitempty"`
	UPC  *string `json:"upc,omitempty" firestore:"upc,omitempty"`
	ASIN *string `json:"asin,omitempty" firestore:"asin,omitempty"`
	ISBN *string `json:"isbn,omitempty" firestore:"isbn,omitempty"`
	MPN  *string `json:"mpn,omitempty" firestore:"mpn,omitempty"`
	GTIN *string `json:"gtin,omitempty" firestore:"gtin,omitempty"`
}

type ProductAsset struct {
	AssetID   string `json:"asset_id" firestore:"asset_id"`
	URL       string `json:"url" firestore:"url"`
	Path      string `json:"path" firestore:"path"`
	Role      string `json:"role" firestore:"role"` // primary_image, gallery, manual, compliance
	SortOrder int    `json:"sort_order" firestore:"sort_order"`
}

type ComplianceStatus struct {
	Status               string    `json:"status" firestore:"status"`
	BlockingIssues       []string  `json:"blocking_issues,omitempty" firestore:"blocking_issues,omitempty"`
	ExpiringWithinDays   *int      `json:"expiring_within_days,omitempty" firestore:"expiring_within_days,omitempty"`
	MissingRequiredTypes []string  `json:"missing_required_types,omitempty" firestore:"missing_required_types,omitempty"`
	LastReviewedAt       time.Time `json:"last_reviewed_at,omitempty" firestore:"last_reviewed_at,omitempty"`
}

type ReadinessFlags struct {
	ReadyForListing     bool `json:"ready_for_listing" firestore:"ready_for_listing"`
	ReadyForShipping    bool `json:"ready_for_shipping" firestore:"ready_for_shipping"`
	HasComplianceIssues bool `json:"has_compliance_issues" firestore:"has_compliance_issues"`
}

// BundleComponent represents a component of a bundle product
type BundleComponent struct {
	ComponentID string  `json:"component_id" firestore:"component_id"`
	ProductID   string  `json:"product_id" firestore:"product_id"`
	Quantity    int     `json:"quantity" firestore:"quantity"`
	IsRequired  bool    `json:"is_required" firestore:"is_required"`
	SortOrder   int     `json:"sort_order" firestore:"sort_order"`
	Title       *string `json:"title,omitempty" firestore:"title,omitempty"`
}

// Variant represents a product variant
type Variant struct {
	VariantID string `json:"variant_id" firestore:"variant_id"`
	TenantID  string `json:"tenant_id" firestore:"tenant_id"`
	ProductID string `json:"product_id" firestore:"product_id"`

	SKU     string  `json:"sku" firestore:"sku"`
	Alias   *string `json:"alias,omitempty" firestore:"alias,omitempty"` // Maps to an existing product SKU for order processing
	Barcode *string `json:"barcode,omitempty" firestore:"barcode,omitempty"`

	Identifiers *ProductIdentifiers    `json:"identifiers,omitempty" firestore:"identifiers,omitempty"`
	Title       *string                `json:"title,omitempty" firestore:"title,omitempty"`
	Attributes  map[string]interface{} `json:"attributes,omitempty" firestore:"attributes,omitempty"`
	Status      string                 `json:"status" firestore:"status"`

	Pricing    *VariantPricing `json:"pricing,omitempty" firestore:"pricing,omitempty"`
	Dimensions *Dimensions     `json:"dimensions,omitempty" firestore:"dimensions,omitempty"`
	Weight     *Weight         `json:"weight,omitempty" firestore:"weight,omitempty"`

	CreatedAt time.Time `json:"created_at" firestore:"created_at"`
	UpdatedAt time.Time `json:"updated_at" firestore:"updated_at"`
	CreatedBy *string   `json:"created_by,omitempty" firestore:"created_by,omitempty"`
	UpdatedBy *string   `json:"updated_by,omitempty" firestore:"updated_by,omitempty"`
}



type VariantPricing struct {
	ListPrice *Money     `json:"list_price,omitempty" firestore:"list_price,omitempty"`
	RRP       *Money     `json:"rrp,omitempty" firestore:"rrp,omitempty"`
	Cost      *Money     `json:"cost,omitempty" firestore:"cost,omitempty"`
	Sale      *SalePrice `json:"sale,omitempty" firestore:"sale,omitempty"`
}

type SalePrice struct {
	SalePrice Money      `json:"sale_price" firestore:"sale_price"`
	From      *time.Time `json:"from,omitempty" firestore:"from,omitempty"`
	To        *time.Time `json:"to,omitempty" firestore:"to,omitempty"`
}

type Dimensions struct {
	Length *float64 `json:"length,omitempty" firestore:"length,omitempty"`
	Width  *float64 `json:"width,omitempty" firestore:"width,omitempty"`
	Height *float64 `json:"height,omitempty" firestore:"height,omitempty"`
	Unit   string   `json:"unit" firestore:"unit"` // mm, cm, m, in
}

type Weight struct {
	Value *float64 `json:"value,omitempty" firestore:"value,omitempty"`
	Unit  string   `json:"unit" firestore:"unit"` // g, kg, lb, oz
}

// CategoryImage represents an image associated with a category
type CategoryImage struct {
	URL       string `json:"url" firestore:"url"`
	Path      string `json:"path" firestore:"path"`
	SortOrder int    `json:"sort_order" firestore:"sort_order"`
}

// Category represents a product category
type Category struct {
	CategoryID     string          `json:"category_id" firestore:"category_id"`
	TenantID       string          `json:"tenant_id" firestore:"tenant_id"`
	Name           string          `json:"name" firestore:"name"`
	Slug           string          `json:"slug" firestore:"slug"`
	ParentID       *string         `json:"parent_id,omitempty" firestore:"parent_id,omitempty"`
	Description    *string         `json:"description,omitempty" firestore:"description,omitempty"`
	AttributeSetID *string         `json:"attribute_set_id,omitempty" firestore:"attribute_set_id,omitempty"`
	Images         []CategoryImage `json:"images,omitempty" firestore:"images,omitempty"`
	SortOrder      int             `json:"sort_order" firestore:"sort_order"`
	Level          int             `json:"level" firestore:"level"`
	Path           string          `json:"path" firestore:"path"`
	Active         bool            `json:"active" firestore:"active"`
	CreatedAt      time.Time       `json:"created_at" firestore:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at" firestore:"updated_at"`
}

// Job represents an async job
type Job struct {
	JobID      string       `json:"job_id" firestore:"job_id"`
	TenantID   string       `json:"tenant_id" firestore:"tenant_id"`
	Type       string       `json:"type" firestore:"type"`
	Status     string       `json:"status" firestore:"status"`
	Progress   *int         `json:"progress,omitempty" firestore:"progress,omitempty"`
	Summary    *JobSummary  `json:"summary,omitempty" firestore:"summary,omitempty"`
	Errors     []JobError   `json:"errors,omitempty" firestore:"errors,omitempty"`
	ResultURL  *string      `json:"result_url,omitempty" firestore:"result_url,omitempty"`
	CreatedAt  time.Time    `json:"created_at" firestore:"created_at"`
	StartedAt  *time.Time   `json:"started_at,omitempty" firestore:"started_at,omitempty"`
	FinishedAt *time.Time   `json:"finished_at,omitempty" firestore:"finished_at,omitempty"`
	ExpiresAt  time.Time    `json:"expires_at" firestore:"expires_at"`
}

type JobSummary struct {
	Total     int `json:"total" firestore:"total"`
	Succeeded int `json:"succeeded" firestore:"succeeded"`
	Failed    int `json:"failed" firestore:"failed"`
	Skipped   int `json:"skipped" firestore:"skipped"`
}

type JobError struct {
	ItemID       *string `json:"item_id,omitempty" firestore:"item_id,omitempty"`
	ErrorCode    string  `json:"error_code" firestore:"error_code"`
	ErrorMessage string  `json:"error_message" firestore:"error_message"`
}

// Request/Response DTOs
type CreateProductRequest struct {
	SKU                string                 `json:"sku" binding:"required"`
	Title              string                 `json:"title" binding:"required"`
	Subtitle           *string                `json:"subtitle,omitempty"`
	Description        *string                `json:"description,omitempty"`
	Brand              *string                `json:"brand,omitempty"`
	ProductType        string                 `json:"product_type" binding:"required,oneof=simple parent variant bundle"`
	ParentID           *string                `json:"parent_id,omitempty"`
	Identifiers        *ProductIdentifiers    `json:"identifiers,omitempty"`
	CategoryIDs        []string               `json:"category_ids,omitempty"`
	Tags               []string               `json:"tags,omitempty"`
	KeyFeatures        []string               `json:"key_features,omitempty"`
	AttributeSetID     *string                `json:"attribute_set_id,omitempty"`
	Attributes         map[string]interface{} `json:"attributes,omitempty"`
	BundleComponents   []BundleComponent      `json:"bundle_components,omitempty"`
	Assets             []ProductAsset         `json:"assets,omitempty"`
	Dimensions         *Dimensions            `json:"dimensions,omitempty"`
	Weight             *Weight                `json:"weight,omitempty"`
	ShippingDimensions *Dimensions            `json:"shipping_dimensions,omitempty"`
	ShippingWeight     *Weight                `json:"shipping_weight,omitempty"`
}

type UpdateProductRequest struct {
	SKU                *string                `json:"sku,omitempty"`
	Title              *string                `json:"title,omitempty"`
	Subtitle           *string                `json:"subtitle,omitempty"`
	Description        *string                `json:"description,omitempty"`
	Brand              *string                `json:"brand,omitempty"`
	Status             *string                `json:"status,omitempty"`
	CategoryIDs        []string               `json:"category_ids,omitempty"`
	Tags               []string               `json:"tags,omitempty"`
	KeyFeatures        []string               `json:"key_features,omitempty"`
	Attributes         map[string]interface{} `json:"attributes,omitempty"`
	Assets             []ProductAsset         `json:"assets,omitempty"`
	Dimensions         *Dimensions            `json:"dimensions,omitempty"`
	Weight             *Weight                `json:"weight,omitempty"`
	ShippingDimensions *Dimensions            `json:"shipping_dimensions,omitempty"`
	ShippingWeight     *Weight                `json:"shipping_weight,omitempty"`
	UseSerialNumbers   *bool                  `json:"use_serial_numbers,omitempty"`
	EndOfLife          *bool                  `json:"end_of_life,omitempty"`
}

type CreateVariantRequest struct {
	SKU         string                 `json:"sku" binding:"required"`
	Alias       *string                `json:"alias,omitempty"`
	Barcode     *string                `json:"barcode,omitempty"`
	Title       *string                `json:"title,omitempty"`
	Identifiers *ProductIdentifiers    `json:"identifiers,omitempty"`
	Attributes  map[string]interface{} `json:"attributes,omitempty"`
	Pricing     *VariantPricing        `json:"pricing,omitempty"`
	Dimensions  *Dimensions            `json:"dimensions,omitempty"`
	Weight      *Weight                `json:"weight,omitempty"`
}

type GenerateVariantsRequest struct {
	Attributes map[string][]string `json:"attributes" binding:"required"`
	SKUPattern string              `json:"sku_pattern"`
}

type ListProductsResponse struct {
	Data       []Product      `json:"data"`
	Pagination PaginationMeta `json:"pagination"`
}

type PaginationMeta struct {
	Total      int64   `json:"total"`
	Page       int     `json:"page"`
	PageSize   int     `json:"page_size"`
	TotalPages int     `json:"total_pages"`
	NextCursor *string `json:"next_cursor,omitempty"`
}
// ============================================================================
// PRODUCT SUPPLIER RELATIONSHIPS
// ============================================================================

// ProductSupplier links a product to a supplier with sourcing details.
// Stored as an array on the Product.Attributes map or as a typed field.
type ProductSupplier struct {
	SupplierID   string  `json:"supplier_id" firestore:"supplier_id"`
	SupplierName string  `json:"supplier_name" firestore:"supplier_name"`
	SupplierSKU  string  `json:"supplier_sku" firestore:"supplier_sku"`
	UnitCost     float64 `json:"unit_cost" firestore:"unit_cost"`
	Currency     string  `json:"currency" firestore:"currency"`
	LeadTimeDays int     `json:"lead_time_days" firestore:"lead_time_days"`
	Priority     int     `json:"priority" firestore:"priority"` // 1=highest priority
	IsDefault    bool    `json:"is_default" firestore:"is_default"`
}
