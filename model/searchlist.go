package model

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"
)

// FuzzyMatch is one result of filterFuzzy: an index into the original slice
// plus rune offsets within the haystack string for highlighting.
type FuzzyMatch struct {
	Index          int
	MatchedIndexes []int
}

type stringSource struct {
	n   int
	str func(int) string
}

func (s stringSource) String(i int) string { return s.str(i) }
func (s stringSource) Len() int            { return s.n }

// filterFuzzy runs sahilm/fuzzy against haystack(i) for i in [0,n). If query is
// empty, returns one match per item in order with no highlight indexes.
func filterFuzzy(n int, query string, haystack func(int) string) []FuzzyMatch {
	if query == "" {
		out := make([]FuzzyMatch, n)
		for i := range out {
			out[i] = FuzzyMatch{Index: i}
		}
		return out
	}
	results := fuzzy.FindFrom(query, stringSource{n: n, str: haystack})
	out := make([]FuzzyMatch, len(results))
	for i, r := range results {
		out[i] = FuzzyMatch{Index: r.Index, MatchedIndexes: r.MatchedIndexes}
	}
	return out
}

// renderHighlighted returns s with the runes at the given indexes wrapped in
// hiStyle and the rest in baseStyle. indexes are rune offsets, ascending.
func renderHighlighted(s string, indexes []int, baseStyle, hiStyle lipgloss.Style) string {
	if len(indexes) == 0 {
		return baseStyle.Render(s)
	}
	var b strings.Builder
	idxSet := make(map[int]bool, len(indexes))
	for _, i := range indexes {
		idxSet[i] = true
	}
	runes := []rune(s)
	i := 0
	for i < len(runes) {
		if idxSet[i] {
			j := i
			for j < len(runes) && idxSet[j] {
				j++
			}
			b.WriteString(hiStyle.Render(string(runes[i:j])))
			i = j
		} else {
			j := i
			for j < len(runes) && !idxSet[j] {
				j++
			}
			b.WriteString(baseStyle.Render(string(runes[i:j])))
			i = j
		}
	}
	return b.String()
}
