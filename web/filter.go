package web

import "strings"

// filterColumns returns columns containing only the cards matching q
// (case-insensitive, against title, tags, and body). Empty or blank q is a
// no-op. Columns with zero matches are kept so the layout and nav anchors
// stay stable; the rendered count reflects the matches.
func filterColumns(cols []Column, q string) []Column {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return cols
	}
	out := make([]Column, len(cols))
	for i, col := range cols {
		out[i] = Column{Name: col.Name, Cards: filterCards(col.Cards, q)}
	}
	return out
}

// filterCards keeps cards whose search haystack contains q (already
// lowercased by filterColumns). Pinned cards get no special treatment;
// relative order is preserved.
func filterCards(cards []Card, q string) []Card {
	out := make([]Card, 0, len(cards))
	for _, c := range cards {
		if strings.Contains(c.search, q) {
			out = append(out, c)
		}
	}
	return out
}
