package marketplace

import (
	"context"
	"fmt"
	"sync"
)

// ============================================================================
// MARKETPLACE ADAPTER REGISTRY
// ============================================================================
// Central registry for all marketplace adapters. New marketplaces are
// registered here and can be retrieved by ID. This allows dynamic
// marketplace selection and easy addition of new integrations.
// ============================================================================

// AdapterFactory is a function that creates a marketplace adapter instance
type AdapterFactory func(ctx context.Context, credentials Credentials) (MarketplaceAdapter, error)

// Registry manages all registered marketplace adapters
type Registry struct {
	adapters map[string]AdapterFactory
	metadata map[string]AdapterMetadata
	mu       sync.RWMutex
}

// AdapterMetadata contains information about a marketplace adapter.
// Fields marked "Firestore-managed" are stored in marketplaces/{id} and
// override the in-process defaults when the registry endpoint is called.
type AdapterMetadata struct {
	ID               string            `json:"id"                firestore:"id"`
	Name             string            `json:"name"              firestore:"name"`
	DisplayName      string            `json:"display_name"      firestore:"display_name"`
	Icon             string            `json:"icon"              firestore:"icon"`
	Color            string            `json:"color"             firestore:"color"`
	RequiresOAuth    bool              `json:"requires_oauth"    firestore:"requires_oauth"`
	SupportedRegions []string          `json:"supported_regions" firestore:"supported_regions"`
	Features         []string          `json:"features"          firestore:"features"`
	IsActive         bool              `json:"is_active"         firestore:"is_active"`
	// Firestore-managed fields — editable via admin UI / Firestore console
	Description      string            `json:"description,omitempty"      firestore:"description,omitempty"`
	ThumbnailURL     string            `json:"thumbnail_url,omitempty"    firestore:"thumbnail_url,omitempty"`
	ImageURL         string            `json:"image_url,omitempty"        firestore:"image_url,omitempty"`
	SortOrder        int               `json:"sort_order"                 firestore:"sort_order"`
	CredentialFields []CredentialField `json:"credential_fields,omitempty" firestore:"credential_fields,omitempty"`
	// AdapterType groups channels for the connections UI filter
	// Values: "direct" (marketplace) | "third_party" (platform / storefront)
	AdapterType      string            `json:"adapter_type,omitempty"     firestore:"adapter_type,omitempty"`
}

// CredentialField describes a single field the user must supply when connecting.
type CredentialField struct {
	Key      string   `json:"key"               firestore:"key"`
	Label    string   `json:"label"             firestore:"label"`
	Type     string   `json:"type"              firestore:"type"` // text | password | select
	Required bool     `json:"required"          firestore:"required"`
	Hint     string   `json:"hint,omitempty"    firestore:"hint,omitempty"`
	Options  []string `json:"options,omitempty" firestore:"options,omitempty"`
}

// Global registry instance
var globalRegistry = &Registry{
	adapters: make(map[string]AdapterFactory),
	metadata: make(map[string]AdapterMetadata),
}

// Register adds a marketplace adapter to the registry
func Register(id string, factory AdapterFactory, metadata AdapterMetadata) {
	globalRegistry.mu.Lock()
	defer globalRegistry.mu.Unlock()
	
	globalRegistry.adapters[id] = factory
	globalRegistry.metadata[id] = metadata
}

// GetAdapter retrieves and initializes a marketplace adapter by ID
func GetAdapter(ctx context.Context, id string, credentials Credentials) (MarketplaceAdapter, error) {
	globalRegistry.mu.RLock()
	factory, exists := globalRegistry.adapters[id]
	globalRegistry.mu.RUnlock()
	
	if !exists {
		return nil, fmt.Errorf("marketplace adapter not found: %s", id)
	}
	
	return factory(ctx, credentials)
}

// ListAdapters returns all registered adapter IDs
func ListAdapters() []string {
	globalRegistry.mu.RLock()
	defer globalRegistry.mu.RUnlock()
	
	ids := make([]string, 0, len(globalRegistry.adapters))
	for id := range globalRegistry.adapters {
		ids = append(ids, id)
	}
	return ids
}

// GetMetadata returns metadata for a specific adapter
func GetMetadata(id string) (AdapterMetadata, error) {
	globalRegistry.mu.RLock()
	defer globalRegistry.mu.RUnlock()
	
	metadata, exists := globalRegistry.metadata[id]
	if !exists {
		return AdapterMetadata{}, fmt.Errorf("adapter metadata not found: %s", id)
	}
	
	return metadata, nil
}

// ListAllMetadata returns metadata for all registered adapters
func ListAllMetadata() []AdapterMetadata {
	globalRegistry.mu.RLock()
	defer globalRegistry.mu.RUnlock()
	
	metadataList := make([]AdapterMetadata, 0, len(globalRegistry.metadata))
	for _, meta := range globalRegistry.metadata {
		metadataList = append(metadataList, meta)
	}
	return metadataList
}

// IsRegistered checks if an adapter with given ID exists
func IsRegistered(id string) bool {
	globalRegistry.mu.RLock()
	defer globalRegistry.mu.RUnlock()
	
	_, exists := globalRegistry.adapters[id]
	return exists
}

// GetActiveAdapters returns metadata for all active adapters
func GetActiveAdapters() []AdapterMetadata {
	globalRegistry.mu.RLock()
	defer globalRegistry.mu.RUnlock()
	
	active := make([]AdapterMetadata, 0)
	for _, meta := range globalRegistry.metadata {
		if meta.IsActive {
			active = append(active, meta)
		}
	}
	return active
}
