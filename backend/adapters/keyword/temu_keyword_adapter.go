package keyword

import "module-a/services"

func init() {
	Register("temu", &temuAdapter{})
}

type temuAdapter struct{}

func (a *temuAdapter) GetConstraints() ChannelConstraints {
	return ChannelConstraints{
		TitleMaxChars:       500,
		BulletCount:         6,
		DescriptionMaxChars: 10000,
		TagCount:            0,
		TagMaxChars:         0,
		HasBackendKeywords:  false,
		Locale:              "en-GB",
		NeedsTranslation:    false,
	}
}

func (a *temuAdapter) Transform(keywordSet *services.KeywordSet) *services.KeywordContext {
	if keywordSet == nil {
		return nil
	}

	return &services.KeywordContext{
		Keywords:      topKeywordStrings(keywordSet, 10),
		TargetChannel: "temu",
		TitleMaxChars: 500,
		// Temu titles follow a strict template. The generation prompt embeds
		// keywords into the appropriate segments of this structure.
		TitleTemplate: "[Brand] + [Product details] + [Application range] + [Product type] + [Main features]",
	}
}
