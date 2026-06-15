package model

import "testing"

func TestSeedValue(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want string
	}{
		{"absent key", nil, ""},
		{"string", "done", "done"},
		{"empty string", "", ""},
		{"bool", true, "true"},
		{"int", 3, "3"},
		{"float", 1.5, "1.5"},
		{"sequence", []any{"a", "b", "c"}, "[a, b, c]"},
		{"empty sequence", []any{}, "[]"},
		{"mixed sequence", []any{"a", 2, true}, "[a, 2, true]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := seedValue(tt.in); got != tt.want {
				t.Errorf("seedValue(%#v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
