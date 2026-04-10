// Package keyword provides the keyword transfer adapter registry and
// channel-specific adapters. Each adapter transforms a global KeywordSet
// into a channel-specific KeywordContext ready for injection into listing
// generation prompts.
//
// All adapters self-register via init(). Import this package with a blank
// import in main.go to ensure all init() functions run:
//
//	import _ "module-a/adapters/keyword"
package keyword

import "module-a/services"

// KeywordTransferAdapter adapts a global KeywordSet for a specific marketplace channel.
type KeywordTransferAdapter interface {
	// Transform returns a KeywordContext ready for injection into the listing
	// generation prompt.
	Transform(keywordSet *services.KeywordSet) *services.KeywordContext

	// GetConstraints returns the channel's field constraints.
	GetConstraints() ChannelConstraints
}

// ChannelConstraints describes the field limits and locale for a marketplace channel.
type ChannelConstraints struct {
	TitleMaxChars       int
	BulletCount         int    // 0 = no bullet field on this channel
	DescriptionMaxChars int
	TagCount            int    // 0 = no tag field
	TagMaxChars         int    // 0 = no tag field
	HasBackendKeywords  bool   // Amazon search terms field
	Locale              string // e.g. "en-GB", "fr-FR"
	NeedsTranslation    bool   // true if locale differs from keyword set source locale
}

// registry maps channel name → adapter.
var registry = map[string]KeywordTransferAdapter{}

// Register associates a channel name with its adapter.
// Called from each adapter's init() function.
func Register(channel string, adapter KeywordTransferAdapter) {
	registry[channel] = adapter
}

// Get returns the adapter for a channel, falling back to the generic adapter
// for any channel not explicitly registered.
func Get(channel string) KeywordTransferAdapter {
	if a, ok := registry[channel]; ok {
		return a
	}
	return registry["generic"]
}
