package search

import (
	"context"
	"errors"
	"testing"
)

func TestVirtualSourceSearchesPresentationFields(t *testing.T) {
	t.Parallel()

	source := NewVirtualSource([]VirtualItem{
		{BoardPath: "/board", Column: "Focus", VID: "focus", ID: "a", Title: "Alpha task", Preview: []string{"due Friday"}, Meta: "work"},
		{BoardPath: "/board", Column: "Focus", VID: "focus", ID: "other", Title: "Unrelated"},
	})

	matches, err := source.Search(t.Context(), "friday")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("matches = %+v, want one preview match", matches)
	}
	if matches[0].VirtualVID != "focus" || matches[0].VirtualID != "a" || matches[0].Text != "due Friday" {
		t.Fatalf("virtual match = %+v", matches[0])
	}
}

func TestVirtualSourceHonorsCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := NewVirtualSource([]VirtualItem{{Title: "task"}}).Search(ctx, "task")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
}
