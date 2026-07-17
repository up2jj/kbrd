package model

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"

	searchbackend "kbrd/search"
)

// runSearch bridges package-level search adapters into Bubble Tea messages.
func runSearch(seq int, query string, sources []searchbackend.Source) tea.Cmd {
	return func() tea.Msg {
		if strings.TrimSpace(query) == "" {
			return searchResultsMsg{Seq: seq}
		}
		ctx, cancel := context.WithTimeout(context.Background(), searchTimeout)
		defer cancel()

		matches, err := searchbackend.Collect(ctx, query, sources...)
		results := make([]searchResult, 0, len(matches))
		for _, match := range matches {
			results = append(results, searchResult{
				BoardPath:   match.BoardPath,
				BoardName:   match.BoardName,
				FilePath:    match.FilePath,
				Column:      match.Column,
				Item:        match.Item,
				Line:        match.Line,
				Text:        match.Text,
				matchCol:    match.MatchCol,
				matchLen:    match.MatchLen,
				virtualVID:  match.VirtualVID,
				virtualItem: match.VirtualID,
			})
		}
		if len(results) > 0 {
			return searchResultsMsg{Seq: seq, Results: results}
		}
		if err != nil {
			return searchResultsMsg{Seq: seq, Err: err.Error()}
		}
		return searchResultsMsg{Seq: seq}
	}
}
