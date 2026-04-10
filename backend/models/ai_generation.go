package models

import "time"

// ============================================================================
// AI GENERATION JOB MODELS
// ============================================================================

// AIGenerationJob tracks a bulk AI listing generation request
type AIGenerationJob struct {
	JobID            string                  `firestore:"job_id" json:"job_id"`
	TenantID         string                  `firestore:"tenant_id" json:"tenant_id"`

	// Configuration
	ProductIDs       []string                `firestore:"product_ids" json:"product_ids"`
	Channels         []string                `firestore:"channels" json:"channels"`
	ChannelAccountID string                  `firestore:"channel_account_id" json:"channel_account_id"`
	Mode             string                  `firestore:"mode" json:"mode"` // "hybrid", "fast", "quality"
	AutoApply        bool                    `firestore:"auto_apply" json:"auto_apply"`

	// Status
	Status           string                  `firestore:"status" json:"status"` // "pending", "running", "completed", "failed"
	StatusMessage    string                  `firestore:"status_message,omitempty" json:"status_message,omitempty"`

	// Progress
	TotalProducts    int                     `firestore:"total_products" json:"total_products"`
	ProcessedCount   int                     `firestore:"processed_count" json:"processed_count"`
	SuccessCount     int                     `firestore:"success_count" json:"success_count"`
	FailedCount      int                     `firestore:"failed_count" json:"failed_count"`

	// Results
	Results          []AIGenerationJobResult `firestore:"results,omitempty" json:"results,omitempty"`

	// Timestamps
	CreatedAt        time.Time               `firestore:"created_at" json:"created_at"`
	UpdatedAt        time.Time               `firestore:"updated_at" json:"updated_at"`
	StartedAt        *time.Time              `firestore:"started_at,omitempty" json:"started_at,omitempty"`
	CompletedAt      *time.Time              `firestore:"completed_at,omitempty" json:"completed_at,omitempty"`
}

// AIGenerationJobResult holds the AI output for a single product
type AIGenerationJobResult struct {
	ProductID    string               `firestore:"product_id" json:"product_id"`
	ProductTitle string               `firestore:"product_title,omitempty" json:"product_title,omitempty"`
	Status       string               `firestore:"status" json:"status"` // "success", "failed"
	Error        string               `firestore:"error,omitempty" json:"error,omitempty"`
	Listings     []AIGeneratedListing  `firestore:"listings,omitempty" json:"listings,omitempty"`
	DurationMS   int64                `firestore:"duration_ms,omitempty" json:"duration_ms,omitempty"`
}

// AIGeneratedListing is the AI-generated content for one marketplace
type AIGeneratedListing struct {
	Channel      string                 `firestore:"channel" json:"channel"`
	Title        string                 `firestore:"title" json:"title"`
	Description  string                 `firestore:"description" json:"description"`
	BulletPoints []string               `firestore:"bullet_points,omitempty" json:"bullet_points,omitempty"`
	CategoryID   string                 `firestore:"category_id,omitempty" json:"category_id,omitempty"`
	CategoryName string                 `firestore:"category_name,omitempty" json:"category_name,omitempty"`
	Attributes   map[string]interface{} `firestore:"attributes,omitempty" json:"attributes,omitempty"`
	SearchTerms  []string               `firestore:"search_terms,omitempty" json:"search_terms,omitempty"`
	Price        float64                `firestore:"price,omitempty" json:"price,omitempty"`
	Confidence   float64                `firestore:"confidence" json:"confidence"`
	Warnings     []string               `firestore:"warnings,omitempty" json:"warnings,omitempty"`

	// Applied state — set when content is used to create an actual listing
	Applied      bool                   `firestore:"applied,omitempty" json:"applied,omitempty"`
	ListingID    string                 `firestore:"listing_id,omitempty" json:"listing_id,omitempty"`
}
