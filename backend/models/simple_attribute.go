package models

import "time"

// Simplified for the UI - matching frontend structure
type SimpleAttribute struct {
	ID          string                  `json:"id" firestore:"id"`
	TenantID    string                  `json:"tenant_id" firestore:"tenant_id"`
	Name        string                  `json:"name" firestore:"name"`
	Code        string                  `json:"code" firestore:"code"`
	DataType    string                  `json:"dataType" firestore:"dataType"` // text, number, boolean, date, list, multiselect
	Required    bool                    `json:"required" firestore:"required"`
	Options     []SimpleAttributeOption `json:"options,omitempty" firestore:"options,omitempty"`
	DefaultValue *string                `json:"defaultValue,omitempty" firestore:"defaultValue,omitempty"`
	Description *string                 `json:"description,omitempty" firestore:"description,omitempty"`
	CreatedAt   time.Time               `json:"created_at" firestore:"created_at"`
	UpdatedAt   time.Time               `json:"updated_at" firestore:"updated_at"`
}

type SimpleAttributeOption struct {
	ID    string `json:"id" firestore:"id"`
	Value string `json:"value" firestore:"value"`
	Label string `json:"label" firestore:"label"`
}

type SimpleAttributeSet struct {
	ID           string    `json:"id" firestore:"id"`
	TenantID     string    `json:"tenant_id" firestore:"tenant_id"`
	Name         string    `json:"name" firestore:"name"`
	Code         string    `json:"code" firestore:"code"`
	Description  *string   `json:"description,omitempty" firestore:"description,omitempty"`
	AttributeIDs []string  `json:"attributeIds" firestore:"attributeIds"` // Note: camelCase for frontend
	CreatedAt    time.Time `json:"created_at" firestore:"created_at"`
	UpdatedAt    time.Time `json:"updated_at" firestore:"updated_at"`
}
