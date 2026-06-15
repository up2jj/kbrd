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
	"strings"

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
	Render []string       // frontmatter keys to surface on the card (`render:`)
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
	p.Render = tagsKey(m, "render")
	return p, nil
}

// Bool coerces a frontmatter value into a boolean. A real bool passes through;
// a string is matched case-insensitively against the truthy set
// (true/yes/y/on/1) and is false otherwise; any other type is false. The string
// arm exists because go-yaml v3 follows the YAML 1.2 core schema, where only
// true/false resolve to a bool — yes/no/on/off stay strings — so a key written
// as `pinned: yes` arrives here as the string "yes".
func Bool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "true", "yes", "y", "on", "1":
			return true
		}
		return false
	default:
		return false
	}
}

// Split separates raw content into its leading YAML frontmatter block (the bytes
// between the opening "---" fence and the first closing "---"/"..." fence,
// excluding the fences) and the body that follows. fenced reports whether a
// complete leading block was found; when it is false block is "" and body is
// raw unchanged. Only a block that starts the file counts — a "---" later in the
// document (a horizontal rule, a second block) is left in the body. An opening
// fence with no closing fence is treated as no block at all (fenced=false), so a
// malformed file is never mistaken for one carrying metadata. The block is not
// length-capped: Split operates on an in-memory string, and Set/Delete rebuild
// from it, so truncation would lose content (the MaxBytes bound belongs to the
// streaming loader that captures frontmatter for Parse).
func Split(raw string) (block, body string, fenced bool) {
	rest, ok := strings.CutPrefix(raw, "---\n")
	if !ok {
		return "", raw, false
	}
	for i := 0; i <= len(rest); {
		end := strings.Index(rest[i:], "\n")
		line := ""
		next := len(rest)
		if end >= 0 {
			line = rest[i : i+end]
			next = i + end + 1
		} else {
			line = rest[i:]
		}
		if t := strings.TrimRight(line, " \t\r"); t == "---" || t == "..." {
			return rest[:i], rest[next:], true
		}
		if end < 0 {
			break
		}
		i = next
	}
	// No closing fence: not a well-formed block.
	return "", raw, false
}

// Validate reports whether setting key to value would produce a well-formed
// frontmatter line. Set writes value verbatim as `key: value`, so it must be a
// valid single-line YAML scalar or flow collection; this composes that same line
// and unmarshals it, returning a descriptive error when it would not parse — an
// embedded newline, an unbalanced flow list ("[1, 2"), or a stray colon that
// turns the value into a nested mapping ("foo: bar"). A nil error means Set will
// write a line that round-trips back to a value under key.
func Validate(key, value string) error {
	if strings.ContainsAny(value, "\n\r") {
		return fmt.Errorf("value must be a single line")
	}
	var m map[string]any
	if err := yaml.Unmarshal([]byte(key+": "+value), &m); err != nil {
		return fmt.Errorf("not a valid YAML value: %w", err)
	}
	if _, ok := m[key]; !ok {
		// The line parsed, but not as a single `key:` entry — e.g. value
		// introduced its own top-level key.
		return fmt.Errorf("not a valid value for %q", key)
	}
	return nil
}

// Set returns raw with the top-level key set to value in its leading
// frontmatter block. An existing top-level `<key>:` line is replaced in place,
// preserving every other line; otherwise `<key>: <value>` is appended to the
// block. When raw has no well-formed leading block one is created
// ("---\n<key>: <value>\n---\n\n" ahead of the original content). value is
// written verbatim, so callers pass an already-valid YAML scalar.
func Set(raw, key, value string) string {
	line := key + ": " + value
	block, body, fenced := Split(raw)
	if !fenced {
		return "---\n" + line + "\n---\n\n" + raw
	}
	lines := blockLines(block)
	for i, l := range lines {
		if topLevelKey(l) == key {
			lines[i] = line
			return assemble(lines, body)
		}
	}
	return assemble(append(lines, line), body)
}

// Delete returns raw with any top-level `<key>:` line removed from its leading
// frontmatter block. Other lines are preserved; if removal empties the block the
// fences (and a single blank line that followed them) are dropped too. A file
// with no well-formed block, or no such key, is returned unchanged.
func Delete(raw, key string) string {
	block, body, fenced := Split(raw)
	if !fenced {
		return raw
	}
	lines := blockLines(block)
	kept := make([]string, 0, len(lines))
	for _, l := range lines {
		if topLevelKey(l) == key {
			continue
		}
		kept = append(kept, l)
	}
	if len(kept) == len(lines) {
		return raw // key absent — leave the file untouched
	}
	if blockEmpty(kept) {
		return strings.TrimPrefix(body, "\n") // drop one blank line after the block
	}
	return assemble(kept, body)
}

// blockLines splits a frontmatter block into its content lines. The block
// (everything between the fences) carries a trailing newline, so a naive
// strings.Split would yield a spurious empty final element; trimming one
// trailing "\n" first keeps the line count honest. An empty block yields no
// lines.
func blockLines(block string) []string {
	if block == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(block, "\n"), "\n")
}

// assemble rebuilds raw content from frontmatter block lines and the body,
// re-adding the fences and the block's trailing newline. The block lines never
// include the fences themselves.
func assemble(lines []string, body string) string {
	block := ""
	if len(lines) > 0 {
		block = strings.Join(lines, "\n") + "\n"
	}
	return "---\n" + block + "---\n" + body
}

// blockEmpty reports whether the block lines carry no content (all blank).
func blockEmpty(lines []string) bool {
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			return false
		}
	}
	return true
}

// topLevelKey returns the mapping key a block line declares when it is written
// at column 0 (no leading indentation), else "". Indented lines belong to a
// nested mapping or a multi-line value and never match, so those structures are
// left untouched by Set/Delete.
func topLevelKey(line string) string {
	line = strings.TrimRight(line, "\r")
	if line == "" || line[0] == ' ' || line[0] == '\t' || line[0] == '#' {
		return ""
	}
	k, _, ok := strings.Cut(line, ":")
	if !ok {
		return ""
	}
	return strings.TrimSpace(k)
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
