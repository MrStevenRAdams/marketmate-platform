package keyword

import (
	"strings"

	"module-a/services"
)

func init() {
	Register("amazon", &amazonAdapter{})
}

type amazonAdapter struct{}

func (a *amazonAdapter) GetConstraints() ChannelConstraints {
	return ChannelConstraints{
		TitleMaxChars:       200,
		BulletCount:         5,
		DescriptionMaxChars: 2000,
		TagCount:            0,
		TagMaxChars:         0,
		HasBackendKeywords:  true,
		Locale:              "en-GB",
		NeedsTranslation:    false,
	}
}

func (a *amazonAdapter) Transform(keywordSet *services.KeywordSet) *services.KeywordContext {
	if keywordSet == nil {
		return nil
	}

	// Keywords 11–20 go into the Amazon backend search terms field.
	backendKWs := keywordStringsRange(keywordSet, 10, 20)

	return &services.KeywordContext{
		Keywords:        topKeywordStrings(keywordSet, 10),
		TargetChannel:   "amazon",
		TitleMaxChars:   200,
		BackendKeywords: strings.Join(backendKWs, " "),
	}
}
