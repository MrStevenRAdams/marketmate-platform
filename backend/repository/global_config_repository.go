package repository

import (
	"context"
	"fmt"

	"cloud.google.com/go/firestore"
)

// ============================================================================
// GLOBAL CONFIG REPOSITORY
// ============================================================================
// Stores company-wide configuration (API keys, shared secrets) in a
// top-level Firestore collection that is NOT tenant-specific.
//
// Firestore path: platform_config/{document_id}
//
// For marketplace keys the document ID is the channel name, e.g.:
//   platform_config/amazon  → { keys: { lwa_client_id: "...", ... } }
//   platform_config/ebay    → { keys: { app_id: "...", ... } }
// ============================================================================

type GlobalConfigRepository struct {
	client *firestore.Client
}

func NewGlobalConfigRepository(client *firestore.Client) *GlobalConfigRepository {
	return &GlobalConfigRepository{client: client}
}

// MarketplaceGlobalConfig represents company-wide keys for a marketplace channel
type MarketplaceGlobalConfig struct {
	Channel string            `firestore:"channel" json:"channel"`
	Keys    map[string]string `firestore:"keys" json:"keys"`
}

// SaveMarketplaceKeys stores or overwrites global keys for a given channel
func (r *GlobalConfigRepository) SaveMarketplaceKeys(ctx context.Context, channel string, keys map[string]string) error {
	docRef := r.client.Collection("platform_config").Doc(channel)
	_, err := docRef.Set(ctx, MarketplaceGlobalConfig{
		Channel: channel,
		Keys:    keys,
	})
	return err
}

// GetMarketplaceKeys retrieves global keys for a given channel
// Returns an empty map (not an error) if no keys are stored yet
func (r *GlobalConfigRepository) GetMarketplaceKeys(ctx context.Context, channel string) (map[string]string, error) {
	docRef := r.client.Collection("platform_config").Doc(channel)
	doc, err := docRef.Get(ctx)
	if err != nil {
		// If document doesn't exist, return empty map
		if fmt.Sprintf("%v", err) == "rpc error: code = NotFound" || !doc.Exists() {
			return map[string]string{}, nil
		}
		return nil, err
	}

	var config MarketplaceGlobalConfig
	if err := doc.DataTo(&config); err != nil {
		return nil, err
	}

	return config.Keys, nil
}

// DeleteMarketplaceKeys removes global keys for a channel
func (r *GlobalConfigRepository) DeleteMarketplaceKeys(ctx context.Context, channel string) error {
	docRef := r.client.Collection("platform_config").Doc(channel)
	_, err := docRef.Delete(ctx)
	return err
}

// ListAllMarketplaceKeys returns global keys for all channels
func (r *GlobalConfigRepository) ListAllMarketplaceKeys(ctx context.Context) (map[string]map[string]string, error) {
	iter := r.client.Collection("platform_config").Documents(ctx)
	result := make(map[string]map[string]string)

	for {
		doc, err := iter.Next()
		if err != nil {
			break // iterator.Done or real error — either way, return what we have
		}
		var config MarketplaceGlobalConfig
		if err := doc.DataTo(&config); err != nil {
			continue
		}
		result[config.Channel] = config.Keys
	}

	return result, nil
}

// SeedIfNotExists creates a platform_config document only if it doesn't already exist
func (r *GlobalConfigRepository) SeedIfNotExists(ctx context.Context, docID string, data map[string]interface{}) error {
	docRef := r.client.Collection("platform_config").Doc(docID)
	doc, err := docRef.Get(ctx)
	if err == nil && doc.Exists() {
		return nil // Already exists, don't overwrite
	}
	_, err = docRef.Set(ctx, data)
	return err
}
