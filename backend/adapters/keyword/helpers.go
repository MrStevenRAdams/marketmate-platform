package keyword

import "module-a/services"

// topKeywordStrings returns up to n keyword strings from the KeywordSet,
// in existing order (already sorted by commercial priority by the intelligence service).
func topKeywordStrings(ks *services.KeywordSet, n int) []string {
	if ks == nil {
		return nil
	}
	count := n
	if len(ks.Keywords) < count {
		count = len(ks.Keywords)
	}
	out := make([]string, count)
	for i := 0; i < count; i++ {
		out[i] = ks.Keywords[i].Keyword
	}
	return out
}

// keywordStringsRange returns keyword strings from index start (inclusive) to
// end (exclusive), clamped to the available slice length.
func keywordStringsRange(ks *services.KeywordSet, start, end int) []string {
	if ks == nil || start >= len(ks.Keywords) {
		return nil
	}
	if end > len(ks.Keywords) {
		end = len(ks.Keywords)
	}
	out := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		out = append(out, ks.Keywords[i].Keyword)
	}
	return out
}
