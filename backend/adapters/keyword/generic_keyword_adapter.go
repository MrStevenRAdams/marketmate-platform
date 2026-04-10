package keyword

import "module-a/services"

func init() {
	Register("generic", &genericAdapter{})
}

type genericAdapter struct{}

func (a *genericAdapter) GetConstraints() ChannelConstraints {
	return ChannelConstraints{
		TitleMaxChars:       150,
		BulletCount:         5,
		DescriptionMaxChars: 2000,
		TagCount:            0,
		TagMaxChars:         0,
		HasBackendKeywords:  false,
		Locale:              "en-GB",
		NeedsTranslation:    false,
	}
}

func (a *genericAdapter) Transform(keywordSet *services.KeywordSet) *services.KeywordContext {
	if keywordSet == nil {
		return nil
	}
	return &services.KeywordContext{
		Keywords:      topKeywordStrings(keywordSet, 10),
		TargetChannel: "generic",
		TitleMaxChars: 150,
	}
}
