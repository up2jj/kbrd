package model

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/sahilm/fuzzy"
)

// FuzzyMatch is one result of filterFuzzy: an index into the original slice
// plus rune offsets within the haystack string for highlighting.
type FuzzyMatch struct {
	Index          int
	MatchedIndexes []int
}

// fuzzyList owns the common state and behavior of a flat, fuzzy-searchable
// list. Callers keep their entries and presentation locally, supplying a
// haystack for each source index whenever their entries change.
//
// It deliberately does not handle keys, opening, closing, or selection
// actions: menus differ at those boundaries, while query editing, matching,
// and cycling cursor movement must remain consistent.
type fuzzyList struct {
	selected int
	filter   string
	matches  []FuzzyMatch
	n        int
	haystack func(int) string
}

// Reset clears the query, selects selected, and rebuilds the unfiltered list.
func (l *fuzzyList) Reset(n, selected int, haystack func(int) string) {
	l.n = n
	l.haystack = haystack
	l.filter = ""
	l.selected = selected
	l.recompute()
}

// Clear drops query and match state when the owning menu closes.
func (l *fuzzyList) Clear() {
	l.selected = 0
	l.filter = ""
	l.matches = nil
	l.n = 0
	l.haystack = nil
}

// recompute refreshes matches for the current query and keeps the cursor in
// range. It must be called after the caller replaces its entry slice.
func (l *fuzzyList) recompute() {
	if l.haystack == nil || l.n <= 0 {
		l.matches = nil
		l.selected = 0
		return
	}
	l.matches = filterFuzzy(l.n, l.filter, l.haystack)
	l.selected = min(max(l.selected, 0), max(len(l.matches)-1, 0))
}

// Append adds typed text, resets the cursor to the best result, and refreshes
// fuzzy matches.
func (l *fuzzyList) Append(text string) {
	if text == "" {
		return
	}
	l.filter += text
	l.selected = 0
	l.recompute()
}

// Backspace removes one rune from the query and refreshes matches. It reports
// whether it consumed a query character, so callers can reserve an empty-query
// backspace for a menu-specific action.
func (l *fuzzyList) Backspace() bool {
	runes := []rune(l.filter)
	if len(runes) == 0 {
		return false
	}
	l.filter = string(runes[:len(runes)-1])
	l.recompute()
	return true
}

// Move shifts the cursor by delta, wrapping around the result set.
func (l *fuzzyList) Move(delta int) {
	if len(l.matches) == 0 {
		l.selected = 0
		return
	}
	l.selected = (l.selected + delta) % len(l.matches)
	if l.selected < 0 {
		l.selected += len(l.matches)
	}
}

// Select sets the result cursor, clamped to the current result set.
func (l *fuzzyList) Select(index int) {
	l.selected = min(max(index, 0), max(len(l.matches)-1, 0))
}

// SelectedIndex returns the selected source-slice index, if a result exists.
func (l *fuzzyList) SelectedIndex() (int, bool) {
	if l.selected < 0 || l.selected >= len(l.matches) {
		return 0, false
	}
	return l.matches[l.selected].Index, true
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

// splitLabelDescMatchIndexes splits rune offsets from a fuzzy haystack built as
// "label  description" into offsets for each displayed field. A missing
// description simply has no offsets after the two-rune separator.
func splitLabelDescMatchIndexes(label string, indexes []int) (labelIdx, descIdx []int) {
	labelLen := len([]rune(label))
	for _, idx := range indexes {
		switch {
		case idx < labelLen:
			labelIdx = append(labelIdx, idx)
		case idx >= labelLen+2:
			descIdx = append(descIdx, idx-labelLen-2)
		}
	}
	return labelIdx, descIdx
}
