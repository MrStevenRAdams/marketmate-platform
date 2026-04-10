package keyword

import "module-a/services"

func init() {
	Register("ebay", &ebayAdapter{})
}

type ebayAdapter struct{}

func (a *ebayAdapter) GetConstraints() ChannelConstraints {
	return ChannelConstraints{
		TitleMaxChars:       80,
		BulletCount:         0,
		DescriptionMaxChars: 4000,
		TagCount:            0,
		TagMaxChars:         0,
		HasBackendKeywords:  false,
		Locale:              "en-GB",
		NeedsTranslation:    false,
	}
}

func (a *ebayAdapter) Transform(keywordSet *services.KeywordSet) *services.KeywordContext {
	if keywordSet == nil {
		return nil
	}

	// eBay titles are 80 chars — only top 3 keywords so the title stays natural.
	// Keywords 4–8 become item specific suggestions; eBay's algorithm weights
	// item specifics heavily and the generation prompt will embed these there,
	// not force them into the title.
	return &services.KeywordContext{
		Keywords:                 topKeywordStrings(keywordSet, 3),
		TargetChannel:            "ebay",
		TitleMaxChars:            80,
		ItemSpecificsSuggestions: keywordStringsRange(keywordSet, 3, 8),
	}
}
