// Package frontmatter parses the YAML metadata block that may lead a card
// file. The card loader captures the raw block (the lines between the `---`
// fences) during its bounded scan and hands it here; Parse extracts the known
// display keys into typed fields and keeps the full map for scripts.
//
// Parsing is deliberately lenient: a wrongly-typed known key is ignored rather
// than failing the card, and callers must treat any returned error as "no
// metadata" — a malformed block must never break item loading.
package frontmatter

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// MaxBytes caps the captured frontmatter block. The card loader stops
// accumulating past this point (while still consuming to the closing fence),
// so a pathological file cannot balloon memory or YAML parse time.
const MaxBytes = 16 * 1024

// Parsed holds the typed known keys plus the full frontmatter map.
type Parsed struct {
	Accent string         // color key/name for the card title/icon
	Icon   string         // glyph prefixed on the title line
	Meta   string         // replaces the filesystem meta line (mtime/size/git)
	Tags   []string       // rendered as #tag chips and matched by the filter
	Data   map[string]any // full map, known + unknown keys; nil when none
}

// Parse unmarshals a frontmatter block (the bytes between the fences, without
// the fences themselves). An empty or all-whitespace block yields the zero
// Parsed with a nil error. Malformed YAML, or YAML whose top level is not a
// mapping, returns the zero Parsed with a non-nil error.
func Parse(block []byte) (Parsed, error) {
	if len(block) == 0 {
		return Parsed{}, nil
	}
	var m map[string]any
	if err := yaml.Unmarshal(block, &m); err != nil {
		return Parsed{}, err
	}
	if m == nil { // whitespace/comment-only block
		return Parsed{}, nil
	}
	p := Parsed{Data: m}
	p.Accent = stringKey(m, "accent")
	p.Icon = stringKey(m, "icon")
	p.Meta = stringKey(m, "meta")
	p.Tags = tagsKey(m, "tags")
	return p, nil
}

// stringKey returns m[key] when it is a string, else "".
func stringKey(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

// tagsKey coerces m[key] into a tag list: a YAML sequence keeps its string
// (or stringable scalar) entries, and a bare string becomes a single tag.
// Anything else yields nil.
func tagsKey(m map[string]any, key string) []string {
	switch v := m[key].(type) {
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	case []any:
		tags := make([]string, 0, len(v))
		for _, e := range v {
			switch t := e.(type) {
			case string:
				tags = append(tags, t)
			case int, int64, float64, bool:
				tags = append(tags, fmt.Sprint(t))
			}
		}
		if len(tags) == 0 {
			return nil
		}
		return tags
	default:
		return nil
	}
}
