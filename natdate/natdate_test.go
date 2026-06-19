package natdate

import (
	"errors"
	"testing"
	"time"
)

// ref is a Friday at 09:00 local time; all expectations below are computed
// relative to it with the default Monday week-start unless noted.
var ref = time.Date(2026, 6, 19, 9, 0, 0, 0, time.Local)

// at builds an expected local time, inheriting ref's 09:00 clock by default.
func at(y int, mo time.Month, d, h, mi int) time.Time {
	return time.Date(y, mo, d, h, mi, 0, 0, time.Local)
}

func TestParse(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		input   string
		opts    []Option
		want    time.Time
		wantErr error
	}{
		// keywords, future and past
		{"en today", "today", nil, at(2026, 6, 19, 9, 0), nil},
		{"pl jutro", "jutro", nil, at(2026, 6, 20, 9, 0), nil},
		{"en yesterday", "yesterday", nil, at(2026, 6, 18, 9, 0), nil},
		{"pl wczoraj", "wczoraj", nil, at(2026, 6, 18, 9, 0), nil},
		{"pl pojutrze", "pojutrze", nil, at(2026, 6, 21, 9, 0), nil},
		{"pl przedwczoraj", "przedwczoraj", nil, at(2026, 6, 17, 9, 0), nil},

		// weekdays + abbreviations
		{"en upcoming wed", "wednesday", nil, at(2026, 6, 24, 9, 0), nil},
		{"en abbr mon", "mon", nil, at(2026, 6, 22, 9, 0), nil},
		{"pl abbr wt", "wt", nil, at(2026, 6, 23, 9, 0), nil},
		{"en this friday", "this friday", nil, at(2026, 6, 19, 9, 0), nil},
		{"en next friday", "next friday", nil, at(2026, 6, 26, 9, 0), nil},
		{"pl next monday", "przyszły poniedziałek", nil, at(2026, 6, 22, 9, 0), nil},
		{"pl w środę", "w środę", nil, at(2026, 6, 24, 9, 0), nil},

		// last weekday (most recent past)
		{"en last friday", "last friday", nil, at(2026, 6, 12, 9, 0), nil},
		{"pl zeszły piątek", "zeszły piątek", nil, at(2026, 6, 12, 9, 0), nil},
		{"pl ostatni poniedziałek", "ostatni poniedziałek", nil, at(2026, 6, 15, 9, 0), nil},

		// relative offsets, future
		{"en in 3 days", "in 3 days", nil, at(2026, 6, 22, 9, 0), nil},
		{"en 2 weeks from now", "2 weeks from now", nil, at(2026, 7, 3, 9, 0), nil},
		{"pl za 2 tygodnie", "za 2 tygodnie", nil, at(2026, 7, 3, 9, 0), nil},
		{"pl za 5 tygodni", "za 5 tygodni", nil, at(2026, 7, 24, 9, 0), nil},
		{"pl za 1 dzień", "za 1 dzień", nil, at(2026, 6, 20, 9, 0), nil},
		{"pl za 5 dni", "za 5 dni", nil, at(2026, 6, 24, 9, 0), nil},
		{"pl za 1 miesiąc", "za 1 miesiąc", nil, at(2026, 7, 19, 9, 0), nil},

		// relative offsets, past
		{"en 2 weeks ago", "2 weeks ago", nil, at(2026, 6, 5, 9, 0), nil},
		{"en 3 days ago", "3 days ago", nil, at(2026, 6, 16, 9, 0), nil},
		{"pl 3 dni temu", "3 dni temu", nil, at(2026, 6, 16, 9, 0), nil},
		{"pl 2 tygodnie temu", "2 tygodnie temu", nil, at(2026, 6, 5, 9, 0), nil},

		// periods
		{"en next week", "next week", nil, at(2026, 6, 22, 9, 0), nil},
		{"pl przyszły tydzień", "przyszły tydzień", nil, at(2026, 6, 22, 9, 0), nil},
		{"en this week", "this week", nil, at(2026, 6, 15, 9, 0), nil},
		{"en last week", "last week", nil, at(2026, 6, 8, 9, 0), nil},
		{"pl w przyszłym tygodniu", "w przyszłym tygodniu", nil, at(2026, 6, 22, 9, 0), nil},
		{"pl w tym tygodniu", "w tym tygodniu", nil, at(2026, 6, 15, 9, 0), nil},
		{"pl w zeszłym roku", "w zeszłym roku", nil, at(2025, 1, 1, 9, 0), nil},

		// time of day + combinations
		{"en at 19:09", "at 19:09", nil, at(2026, 6, 19, 19, 9), nil},
		{"en at 7pm", "at 7pm", nil, at(2026, 6, 19, 19, 0), nil},
		{"en bare clock", "19:09", nil, at(2026, 6, 19, 19, 9), nil},
		{"pl o 19:09", "o 19:09", nil, at(2026, 6, 19, 19, 9), nil},
		{"en wed at 19:09", "wednesday at 19:09", nil, at(2026, 6, 24, 19, 9), nil},
		{"pl środa o 19:09", "środa o 19:09", nil, at(2026, 6, 24, 19, 9), nil},

		// absolute date literals
		{"iso date", "2026-06-24", nil, at(2026, 6, 24, 9, 0), nil},
		{"iso slash date", "2026/06/24", nil, at(2026, 6, 24, 9, 0), nil},
		{"european dotted date", "24.06.2026", nil, at(2026, 6, 24, 9, 0), nil},
		{"european day.month", "24.06", nil, at(2026, 6, 24, 9, 0), nil},
		{"iso date with time", "2026-06-24 at 19:09", nil, at(2026, 6, 24, 19, 9), nil},
		{"iso date bare clock", "2026-06-24 19:09", nil, at(2026, 6, 24, 19, 9), nil},
		{"invalid month", "2026-13-01", nil, time.Time{}, ErrUnparseable},
		{"invalid day", "2026-02-30", nil, time.Time{}, ErrUnparseable},

		// week-start changes "next sunday"
		{"next sunday monday-start", "next sunday", nil, at(2026, 6, 28, 9, 0), nil},
		{"next sunday sunday-start", "next sunday",
			[]Option{WithWeekStart(time.Sunday)}, at(2026, 6, 21, 9, 0), nil},

		// language restriction
		{"pl only rejects english", "tomorrow",
			[]Option{WithLanguages("pl")}, time.Time{}, ErrUnparseable},
		{"en only parses english", "tomorrow",
			[]Option{WithLanguages("en")}, at(2026, 6, 20, 9, 0), nil},

		// failures
		{"garbage", "florble", nil, time.Time{}, ErrUnparseable},
		{"empty", "   ", nil, time.Time{}, ErrUnparseable},
		{"unknown language", "today", []Option{WithLanguages("de")}, time.Time{}, ErrUnknownLanguage},
		{"in without unit", "in 3", nil, time.Time{}, ErrUnparseable},
		{"bad clock", "at 25:00", nil, time.Time{}, ErrUnparseable},
		{"two date clauses", "tomorrow next week", nil, time.Time{}, ErrAmbiguous},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := Parse(tc.input, ref, tc.opts...)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("Parse(%q) err = %v, want %v", tc.input, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q) unexpected err: %v", tc.input, err)
			}
			if !got.Equal(tc.want) {
				t.Errorf("Parse(%q) = %s, want %s", tc.input, got.Format(time.RFC3339), tc.want.Format(time.RFC3339))
			}
		})
	}
}

// TestMonthOverflow documents Go's calendar normalization for month math.
func TestMonthOverflow(t *testing.T) {
	t.Parallel()
	jan31 := time.Date(2026, 1, 31, 9, 0, 0, 0, time.Local)
	got, err := Parse("za 1 miesiąc", jan31)
	if err != nil {
		t.Fatal(err)
	}
	// Jan 31 + 1 month overflows February → March 3 (2026 is not a leap year).
	want := time.Date(2026, 3, 3, 9, 0, 0, 0, time.Local)
	if !got.Equal(want) {
		t.Errorf("got %s, want %s", got.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}
