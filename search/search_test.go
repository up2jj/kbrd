package search

import (
	"context"
	"errors"
	"testing"
)

type stubSource struct {
	matches []Match
	err     error
}

func (s stubSource) Search(context.Context, string) ([]Match, error) {
	return s.matches, s.err
}

func TestCollectKeepsPartialMatchesAndJoinsErrors(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("source failed")
	matches, err := Collect(t.Context(), "query",
		stubSource{matches: []Match{{Text: "virtual"}}},
		stubSource{err: wantErr},
	)
	if len(matches) != 1 || matches[0].Text != "virtual" {
		t.Fatalf("matches = %+v", matches)
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want source failure", err)
	}
}
