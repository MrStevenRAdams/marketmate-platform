package models

// FulfilmentNetwork is a named, priority-ordered list of fulfilment sources.
// When an order is assigned to a network, the system walks the list in priority
// order and assigns the first source that passes the stock threshold check.
//
// Firestore: tenants/{tenant_id}/fulfilment_networks/{network_id}
type FulfilmentNetwork struct {
	NetworkID   string               `json:"network_id" firestore:"network_id"`
	TenantID    string               `json:"tenant_id" firestore:"tenant_id"`
	Name        string               `json:"name" firestore:"name"`
	Description string               `json:"description,omitempty" firestore:"description,omitempty"`
	Sources     []NetworkSourceEntry `json:"sources" firestore:"sources"`
	Active      bool                 `json:"active" firestore:"active"`
	CreatedAt   string               `json:"created_at" firestore:"created_at"`
	UpdatedAt   string               `json:"updated_at,omitempty" firestore:"updated_at,omitempty"`
}

// NetworkSourceEntry is a single fulfilment source within a network, with its priority and minimum-stock threshold.
type NetworkSourceEntry struct {
	SourceID string `json:"source_id" firestore:"source_id"`
	Priority int    `json:"priority" firestore:"priority"`  // 1 = highest priority
	MinStock int    `json:"min_stock" firestore:"min_stock"` // minimum available stock units required to use this source
}

// NetworkResolveResult is returned by the /resolve endpoint showing which source was selected and why.
type NetworkResolveResult struct {
	NetworkID      string              `json:"network_id"`
	OrderID        string              `json:"order_id"`
	SelectedSource *FulfilmentSource   `json:"selected_source,omitempty"`
	SkippedSources []ResolveSkipReason `json:"skipped_sources,omitempty"`
	Reason         string              `json:"reason"`
}

// ResolveSkipReason explains why a source was skipped during resolution.
type ResolveSkipReason struct {
	SourceID   string `json:"source_id"`
	SourceName string `json:"source_name"`
	Reason     string `json:"reason"`
}
