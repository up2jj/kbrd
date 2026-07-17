package search

import (
	"context"
	"strings"
)

// VirtualSource searches presentation fields supplied by active-layer virtual
// columns. Opaque item data is deliberately excluded.
type VirtualSource struct {
	items []VirtualItem
}

// NewVirtualSource snapshots the virtual items used by an asynchronous query.
func NewVirtualSource(items []VirtualItem) VirtualSource {
	return VirtualSource{items: cloneVirtualItems(items)}
}

// Search matches titles, previews, and metadata using case-insensitive fixed
// strings, mirroring the filesystem source's ripgrep options.
func (s VirtualSource) Search(ctx context.Context, query string) ([]Match, error) {
	matches := make([]Match, 0, len(s.items))
	for _, item := range s.items {
		if err := ctx.Err(); err != nil {
			return matches, err
		}
		fields := make([]string, 0, len(item.Preview)+3)
		fields = append(fields, item.Title)
		fields = append(fields, item.Preview...)
		fields = append(fields, item.Meta)
		if item.Title == "" {
			fields = append(fields, item.ID)
		}
		for _, field := range fields {
			col, length, ok := fixedFoldMatch(field, query)
			if !ok {
				continue
			}
			title := item.Title
			if title == "" {
				title = item.ID
			}
			matches = append(matches, Match{
				BoardPath:  item.BoardPath,
				BoardName:  item.BoardName,
				FilePath:   item.FilePath,
				Column:     item.Column,
				Item:       title,
				Text:       field,
				MatchCol:   col,
				MatchLen:   length,
				VirtualVID: item.VID,
				VirtualID:  item.ID,
			})
		}
	}
	return matches, nil
}

func fixedFoldMatch(text, query string) (col, length int, ok bool) {
	textRunes := []rune(text)
	queryRunes := []rune(query)
	if len(queryRunes) == 0 || len(queryRunes) > len(textRunes) {
		return 0, 0, false
	}
	for i := 0; i+len(queryRunes) <= len(textRunes); i++ {
		candidate := string(textRunes[i : i+len(queryRunes)])
		if strings.EqualFold(candidate, query) {
			return i, len(queryRunes), true
		}
	}
	return 0, 0, false
}
