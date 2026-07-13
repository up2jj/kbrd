package config

import (
	"strings"
	"testing"
	"time"
)

func TestParsePresetDateExpression(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		input      string
		base       string
		offset     int
		unit       string
		candidate  bool
		wantErrSub string
	}{
		{name: "now hours", input: "now+2h", base: "now", offset: 2, unit: "h", candidate: true},
		{name: "spaced today days", input: "today - 3d", base: "today", offset: -3, unit: "d", candidate: true},
		{name: "now minute shorthand", input: "now+1m", base: "now", offset: 1, unit: "m", candidate: true},
		{name: "now minutes", input: "now+30min", base: "now", offset: 30, unit: "min", candidate: true},
		{name: "today months", input: "today+1mo", base: "today", offset: 1, unit: "mo", candidate: true},
		{name: "ordinary variable", input: "board", candidate: false},
		{name: "invalid unit", input: "today+1x", candidate: true, wantErrSub: "expected"},
		{name: "chained expression", input: "today+1mo-2d", candidate: true, wantErrSub: "expected"},
		{name: "overflow", input: "now+999999999999999999999999h", candidate: true, wantErrSub: "out of range"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr, candidate, err := ParsePresetDateExpression(tt.input)
			if candidate != tt.candidate {
				t.Fatalf("candidate = %v, want %v", candidate, tt.candidate)
			}
			if tt.wantErrSub != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErrSub) {
					t.Fatalf("error = %v, want substring %q", err, tt.wantErrSub)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParsePresetDateExpression: %v", err)
			}
			if expr.Base != tt.base || expr.Offset != tt.offset || expr.Unit != tt.unit {
				t.Fatalf("expression = %+v, want base=%q offset=%d unit=%q", expr, tt.base, tt.offset, tt.unit)
			}
		})
	}
}

func TestPresetDateExpressionEvaluate(t *testing.T) {
	now := time.Date(2026, time.January, 15, 10, 30, 0, 0, time.UTC)
	for _, tt := range []struct {
		input string
		want  string
	}{
		{input: "now+2h", want: "2026-01-15T12:30:00Z"},
		{input: "now+1m", want: "2026-01-15T10:31:00Z"},
		{input: "now-30min", want: "2026-01-15T10:00:00Z"},
		{input: "today+1d", want: "2026-01-16"},
		{input: "today+1w", want: "2026-01-22"},
		{input: "today+1mo", want: "2026-02-15"},
	} {
		t.Run(tt.input, func(t *testing.T) {
			expr, _, err := ParsePresetDateExpression(tt.input)
			if err != nil {
				t.Fatalf("ParsePresetDateExpression: %v", err)
			}
			got, err := expr.Evaluate(now)
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			if got != tt.want {
				t.Fatalf("Evaluate = %q, want %q", got, tt.want)
			}
		})
	}
}
