// Package search provides composable backends for global board search.
package search

import (
	"context"
	"errors"
)

// Root identifies one board whose filesystem columns can be searched.
type Root struct {
	Path string
	Name string
}

// VirtualItem is an immutable snapshot of one active-layer virtual item.
type VirtualItem struct {
	BoardPath string
	BoardName string
	Column    string
	VID       string
	ID        string
	Title     string
	Preview   []string
	Meta      string
	FilePath  string
}

// Match is the common per-occurrence result emitted by every Source.
type Match struct {
	BoardPath  string
	BoardName  string
	FilePath   string
	Column     string
	Item       string
	Line       int
	Text       string
	MatchCol   int
	MatchLen   int
	VirtualVID string
	VirtualID  string
}

// Source adapts one search backend into the common Match shape.
type Source interface {
	Search(ctx context.Context, query string) ([]Match, error)
}

// Collect queries each source in order. It returns partial matches alongside
// joined source errors so the caller can decide whether partial results are
// more useful than surfacing a backend failure.
func Collect(ctx context.Context, query string, sources ...Source) ([]Match, error) {
	var matches []Match
	var errs []error
	for _, source := range sources {
		found, err := source.Search(ctx, query)
		matches = append(matches, found...)
		if err != nil {
			errs = append(errs, err)
		}
	}
	return matches, errors.Join(errs...)
}

func cloneVirtualItems(items []VirtualItem) []VirtualItem {
	cloned := make([]VirtualItem, len(items))
	for i, item := range items {
		cloned[i] = item
		cloned[i].Preview = append([]string(nil), item.Preview...)
	}
	return cloned
}
