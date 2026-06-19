package natdate

import (
	"errors"
	"fmt"
	"time"
)

// Sentinel errors. Callers should check the error, never the zero time.Time.
var (
	// ErrUnparseable means no rule in the active lexicons matched the input.
	ErrUnparseable = errors.New("natdate: unparseable phrase")
	// ErrUnknownLanguage means WithLanguages named a code with no registered lexicon.
	ErrUnknownLanguage = errors.New("natdate: unknown language")
	// ErrAmbiguous means tokens matched but produced conflicting date/time clauses.
	ErrAmbiguous = errors.New("natdate: ambiguous phrase")
)

// options is the resolved configuration for a Parse call.
type options struct {
	languages    []string
	weekStart    time.Weekday
	weekStartSet bool
}

// Option customizes Parse. The zero configuration means "all built-in languages,
// Monday week-start".
type Option func(*options)

// WithLanguages restricts parsing to the named language codes (e.g. "en", "pl"),
// in priority order — the first language wins on a token collision. The default,
// when unset, is every built-in language.
func WithLanguages(codes ...string) Option {
	return func(o *options) { o.languages = codes }
}

// WithWeekStart sets the first day of the week, used by "next/this/last <weekday>"
// and period math. The default is Monday. Pass time.Sunday explicitly for US weeks.
func WithWeekStart(d time.Weekday) Option {
	return func(o *options) {
		o.weekStart = d
		o.weekStartSet = true
	}
}

func (o options) effectiveWeekStart() time.Weekday {
	if !o.weekStartSet {
		return time.Monday
	}
	return o.weekStart
}

// Parse resolves a natural-language date/time phrase relative to now.
//
// now is required so there is never a hidden time.Now(); its Location is preserved
// in the result. Date-only phrases inherit now's wall clock; a time-of-day phrase
// overrides it. On no match Parse returns the zero time and ErrUnparseable.
func Parse(input string, now time.Time, opts ...Option) (time.Time, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}

	lex, err := mergeLexicons(o.languages)
	if err != nil {
		return time.Time{}, err
	}

	tokens := tokenize(input)
	if len(tokens) == 0 {
		return time.Time{}, fmt.Errorf("%w: %q", ErrUnparseable, input)
	}

	t, err := parseTokens(tokens, now, lex, o.effectiveWeekStart())
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: %q", err, input)
	}
	return t, nil
}
