package natdate

import (
	"fmt"
	"time"
)

// Unit is a relative-offset unit.
type Unit int

const (
	UnitDay Unit = iota
	UnitWeek
	UnitMonth
	UnitYear
)

// role tags a function word by the grammar it introduces.
type role int

const (
	roleIn   role = iota // "in" / "za"               → relative future offset follows
	roleAt               // "at" / "o"                → time-of-day follows
	roleNext             // "next" / "przyszły"       → +1 period
	roleThis             // "this" / "w"              → current period
	roleLast             // "last" / "zeszły/ostatni" → −1 period
)

// Lexicon is one language's vocabulary. Every map is keyed by a normalized
// (lowercased, diacritics preserved) surface form. Adding a language is a matter
// of writing one of these — the engine has no per-language code.
type Lexicon struct {
	// Code is the language tag, e.g. "en" or "pl".
	Code string
	// Weekdays maps full names and abbreviations to a weekday.
	Weekdays map[string]time.Weekday
	// Units maps every (inflected) unit noun to its Unit. Polish numeral-noun
	// agreement lives here: tydzień/tygodnie/tygodni all map to UnitWeek.
	Units map[string]Unit
	// Keywords are standalone day anchors as an offset in days from now
	// (today=0, tomorrow=+1, yesterday=-1).
	Keywords map[string]int
	// Prefixes are role-tagged function words.
	Prefixes map[string]role
	// PastTails are trailing words that negate a relative offset ("ago", "temu").
	PastTails map[string]bool
}

// builtins are the lexicons available out of the box.
var builtins = map[string]Lexicon{
	"en": enLexicon,
	"pl": plLexicon,
}

// merged is the runtime matcher assembled from the chosen lexicons.
type merged struct {
	weekdays  map[string]time.Weekday
	units     map[string]Unit
	keywords  map[string]int
	prefixes  map[string]role
	pastTails map[string]bool
}

// mergeLexicons combines the named languages (default: all built-ins) into a
// single matcher. Earlier languages win on a token collision.
func mergeLexicons(codes []string) (*merged, error) {
	if len(codes) == 0 {
		codes = []string{"en", "pl"} // deterministic default order
	}
	m := &merged{
		weekdays:  map[string]time.Weekday{},
		units:     map[string]Unit{},
		keywords:  map[string]int{},
		prefixes:  map[string]role{},
		pastTails: map[string]bool{},
	}
	for _, c := range codes {
		lex, ok := builtins[c]
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrUnknownLanguage, c)
		}
		for k, v := range lex.Weekdays {
			if _, exists := m.weekdays[k]; !exists {
				m.weekdays[k] = v
			}
		}
		for k, v := range lex.Units {
			if _, exists := m.units[k]; !exists {
				m.units[k] = v
			}
		}
		for k, v := range lex.Keywords {
			if _, exists := m.keywords[k]; !exists {
				m.keywords[k] = v
			}
		}
		for k, v := range lex.Prefixes {
			if _, exists := m.prefixes[k]; !exists {
				m.prefixes[k] = v
			}
		}
		for k := range lex.PastTails {
			m.pastTails[k] = true
		}
	}
	return m, nil
}
