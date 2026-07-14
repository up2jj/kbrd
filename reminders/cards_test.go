package reminders

import (
	"testing"
	"time"
)

func TestNormalizeDuePreservesDateOrTimePrecision(t *testing.T) {
	location := time.FixedZone("CEST", 2*60*60)
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, location)
	tests := []struct {
		name         string
		value        any
		want         string
		wantRelative bool
	}{
		{name: "date string", value: "2026-07-15", want: "2026-07-15"},
		{name: "yaml date", value: time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC), want: "2026-07-15"},
		{name: "local date and time", value: "2026-07-15 14:30", want: "2026-07-15T12:30:00Z"},
		{name: "local ISO date and time", value: "2026-07-15T14:30", want: "2026-07-15T12:30:00Z"},
		{name: "RFC3339", value: "2026-07-15T14:30:00+02:00", want: "2026-07-15T12:30:00Z"},
		{name: "natural date only", value: "tomorrow", want: "2026-07-15", wantRelative: true},
		{name: "natural date and time", value: "tomorrow at 19:09", want: "2026-07-15T17:09:00Z", wantRelative: true},
		{name: "clock only", value: "19:09", want: "2026-07-14T17:09:00Z", wantRelative: true},
		{name: "absolute natural syntax", value: "2026/07/15 at 19:09", want: "2026-07-15T17:09:00Z"},
		{name: "year-relative European date", value: "15.07 at 19:09", want: "2026-07-15T17:09:00Z", wantRelative: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDue(tt.value, now)
			if err != nil {
				t.Fatal(err)
			}
			if got.Value != tt.want || got.Relative != tt.wantRelative {
				t.Fatalf("parseDue(%v) = %+v, want value=%q relative=%v", tt.value, got, tt.want, tt.wantRelative)
			}
		})
	}
}
