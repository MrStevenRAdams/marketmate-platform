package keyword

import (
	"strings"
	"unicode/utf8"

	"module-a/services"
)

func init() {
	Register("etsy", &etsyAdapter{})
}

type etsyAdapter struct{}

func (a *etsyAdapter) GetConstraints() ChannelConstraints {
	return ChannelConstraints{
		TitleMaxChars:       140,
		BulletCount:         0,
		DescriptionMaxChars: 4000,
		TagCount:            13,
		TagMaxChars:         20,
		HasBackendKeywords:  false,
		Locale:              "en-GB",
		NeedsTranslation:    false,
	}
}

func (a *etsyAdapter) Transform(keywordSet *services.KeywordSet) *services.KeywordContext {
	if keywordSet == nil {
		return nil
	}

	return &services.KeywordContext{
		Keywords:      topKeywordStrings(keywordSet, 10),
		TargetChannel: "etsy",
		TitleMaxChars: 140,
		Tags:          buildEtsyTags(keywordSet),
	}
}

// buildEtsyTags produces up to 13 deduplicated tags, each ≤ 20 characters,
// from the KeywordSet. Tags are drawn from the full keyword list until
// 13 valid tags are gathered or keywords are exhausted.
func buildEtsyTags(ks *services.KeywordSet) []string {
	const maxTags = 13
	const maxTagChars = 20

	seen := make(map[string]struct{})
	tags := make([]string, 0, maxTags)

	for _, entry := range ks.Keywords {
		if len(tags) >= maxTags {
			break
		}

		tag := truncateToEtsyTag(entry.Keyword, maxTagChars)
		if tag == "" {
			continue
		}

		lower := strings.ToLower(tag)
		if _, dup := seen[lower]; dup {
			continue
		}

		seen[lower] = struct{}{}
		tags = append(tags, tag)
	}

	return tags
}

// truncateToEtsyTag trims a keyword to at most maxChars characters while
// keeping the result meaningful. Rules:
//  1. If len ≤ maxChars, use as-is.
//  2. Find the last space before the maxChars boundary.
//     If the resulting phrase is ≥ 2 words OR ≥ 6 chars, use it.
//  3. Otherwise take only the first word.
//  4. If the first word itself exceeds maxChars, hard-truncate at a rune
//     boundary (should be rare given typical keyword lengths).
func truncateToEtsyTag(kw string, maxChars int) string {
	if utf8.RuneCountInString(kw) <= maxChars {
		return kw
	}

	// Build a rune-safe prefix up to maxChars.
	runes := []rune(kw)
	prefix := string(runes[:maxChars])

	// Find last space within the prefix.
	lastSpace := strings.LastIndex(prefix, " ")
	if lastSpace > 0 {
		candidate := strings.TrimSpace(prefix[:lastSpace])
		words := strings.Fields(candidate)
		if len(words) >= 2 || utf8.RuneCountInString(candidate) >= 6 {
			return candidate
		}
	}

	// Fall back to the first word of the original keyword.
	firstWord := strings.Fields(kw)[0]
	if utf8.RuneCountInString(firstWord) > maxChars {
		// Hard truncate at rune boundary.
		return string([]rune(firstWord)[:maxChars])
	}
	return firstWord
}
