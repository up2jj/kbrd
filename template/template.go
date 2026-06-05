// Package template implements card templates: markdown files whose YAML
// frontmatter declares a form (steps of typed fields) and whose body is a Go
// text/template rendered with the collected answers.
//
// The package is headless — it owns template discovery, schema validation,
// programmatic value preparation, and rendering — and is shared by the TUI
// form flow (package model) and the Lua API. It imports package board for
// the filesystem vocabulary (Hidden, VarContext, SanitizeName); board never
// imports it back, mirroring board's one-way dependency rule.
package template

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	texttemplate "text/template"
	"time"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"kbrd/board"
)

// Dir is the subfolder (of a column or the board root) that holds card
// templates. The "." prefix means board.Hidden already keeps it out of
// column and item discovery.
const Dir = ".kbrd_templates"

// FilenameKey is the synthetic form-field key used when a template omits the
// `filename` frontmatter entry and the user is asked for one instead.
const FilenameKey = "_filename"

// Template scopes, recorded so a picker can annotate where a template came from.
const (
	ScopeColumn = "column"
	ScopeBoard  = "board"
)

// fieldTypes is the set of form field kinds a template may declare.
var fieldTypes = map[string]bool{
	"input":       true,
	"text":        true,
	"select":      true,
	"multiselect": true,
	"confirm":     true,
	"note":        true,
}

// Template is a parsed card template: frontmatter-declared form steps plus a
// Go text/template markdown body rendered with the collected values.
type Template struct {
	Name     string // display name (frontmatter `name`, fallback: file base name)
	Filename string // optional template for the new card's filename ("" = ask the user)
	Steps    []Step
	Body     string // markdown after the frontmatter
	Path     string // source file, for error messages
	Scope    string // ScopeColumn or ScopeBoard
}

// Step is one form page; the user fills steps in declaration order.
type Step struct {
	Title  string  `yaml:"title"`
	Fields []Field `yaml:"fields"`
}

// Field is a single form field within a step.
type Field struct {
	Key         string   `yaml:"key"`
	Type        string   `yaml:"type"` // input|text|select|multiselect|confirm|note
	Title       string   `yaml:"title"`
	Description string   `yaml:"description"`
	Placeholder string   `yaml:"placeholder"`
	Default     string   `yaml:"default"`
	Required    bool     `yaml:"required"`
	Options     []string `yaml:"options"` // for select/multiselect

	// Input/text constraints. Pattern is an RE2 regular expression the value
	// must match; PatternHint is shown instead of the raw regex when it
	// doesn't. MinLen/MaxLen bound the value's length in runes (0 = unbounded).
	Pattern     string `yaml:"pattern"`
	PatternHint string `yaml:"pattern_hint"`
	MinLen      int    `yaml:"min_len"`
	MaxLen      int    `yaml:"max_len"`

	// Prefill names a source whose content seeds the field in the interactive
	// form, where the user sees and can edit or clear it before submitting.
	// PrefillClipboard is the only source today. It deliberately has no effect
	// on the programmatic (Lua) path — scripts pass values explicitly — and is
	// never read at render time, so a template cannot capture anything the
	// user didn't see in the form.
	Prefill string `yaml:"prefill"`
}

// PrefillClipboard seeds the field with the system clipboard's content.
const PrefillClipboard = "clipboard"

// LoadWarning records a template file that was skipped because it failed to
// parse or validate; valid siblings still load.
type LoadWarning struct {
	Path string
	Err  error
}

// meta is the frontmatter shape.
type meta struct {
	Name     string `yaml:"name"`
	Filename string `yaml:"filename"`
	Steps    []Step `yaml:"steps"`
}

// List returns the templates available for a column: the column's own
// .kbrd_templates folder merged with the board-level one. A column template
// shadows a board template with the same display name. Missing directories
// are not an error. Files that fail to parse or validate are reported as
// warnings and excluded.
func List(boardPath, columnPath string) ([]Template, []LoadWarning, error) {
	var warns []LoadWarning
	colTmpls, w, err := readDir(filepath.Join(columnPath, Dir), ScopeColumn)
	if err != nil {
		return nil, nil, err
	}
	warns = append(warns, w...)
	boardTmpls, w, err := readDir(filepath.Join(boardPath, Dir), ScopeBoard)
	if err != nil {
		return nil, nil, err
	}
	warns = append(warns, w...)

	seen := make(map[string]bool, len(colTmpls))
	for _, t := range colTmpls {
		seen[t.Name] = true
	}
	merged := colTmpls
	for _, t := range boardTmpls {
		if !seen[t.Name] {
			merged = append(merged, t)
		}
	}
	return merged, warns, nil
}

// readDir parses every .md file in dir, sorted by display name.
// A missing dir yields no templates and no error.
func readDir(dir, scope string) ([]Template, []LoadWarning, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	var tmpls []Template
	var warns []LoadWarning
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || board.Hidden(name) || !strings.HasSuffix(name, ".md") {
			continue
		}
		path := filepath.Join(dir, name)
		t, err := Parse(path)
		if err != nil {
			warns = append(warns, LoadWarning{Path: path, Err: err})
			continue
		}
		t.Scope = scope
		tmpls = append(tmpls, t)
	}
	sort.Slice(tmpls, func(i, j int) bool { return tmpls[i].Name < tmpls[j].Name })
	return tmpls, warns, nil
}

// Parse reads and validates one template file. The file must start with a
// `---` YAML frontmatter block; everything after the closing `---` is the
// body. Both the filename and body templates are compiled here so syntax
// errors surface at load time rather than at submit time.
func Parse(path string) (Template, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Template{}, err
	}
	front, body, err := splitFrontmatter(string(raw))
	if err != nil {
		return Template{}, err
	}
	var m meta
	if err := yaml.Unmarshal([]byte(front), &m); err != nil {
		return Template{}, fmt.Errorf("frontmatter: %w", err)
	}
	t := Template{
		Name:     m.Name,
		Filename: m.Filename,
		Steps:    m.Steps,
		Body:     body,
		Path:     path,
	}
	if t.Name == "" {
		t.Name = strings.TrimSuffix(filepath.Base(path), ".md")
	}
	if err := validate(t); err != nil {
		return Template{}, err
	}
	return t, nil
}

// splitFrontmatter separates the leading `---` YAML block from the body.
func splitFrontmatter(s string) (front, body string, err error) {
	const delim = "---"
	rest, ok := strings.CutPrefix(s, delim+"\n")
	if !ok {
		return "", "", fmt.Errorf("missing frontmatter: file must start with %q", delim)
	}
	if body, ok := strings.CutPrefix(rest, delim+"\n"); ok { // empty frontmatter
		return "", body, nil
	}
	if rest == delim {
		return "", "", nil
	}
	if idx := strings.Index(rest, "\n"+delim+"\n"); idx >= 0 {
		return rest[:idx], rest[idx+len(delim)+2:], nil
	}
	if trimmed, ok := strings.CutSuffix(rest, "\n"+delim); ok {
		return trimmed, "", nil
	}
	return "", "", fmt.Errorf("unterminated frontmatter: missing closing %q", delim)
}

// validate enforces the field rules: known types, options where required,
// unique non-reserved keys, defaults consistent with options, and compilable
// filename/body templates.
func validate(t Template) error {
	reserved := map[string]bool{
		"boardPath": true, "boardName": true,
		"columnPath": true, "columnName": true,
		"filePath": true, "fileName": true, "fileDir": true,
		FilenameKey: true,
	}
	keys := map[string]bool{}
	for si, step := range t.Steps {
		for fi, f := range step.Fields {
			where := fmt.Sprintf("step %d field %d", si+1, fi+1)
			if !fieldTypes[f.Type] {
				return fmt.Errorf("%s: unknown type %q", where, f.Type)
			}
			if f.Type == "note" {
				continue // display-only: no key, no value
			}
			if f.Key == "" {
				return fmt.Errorf("%s: missing key", where)
			}
			if reserved[f.Key] {
				return fmt.Errorf("%s: key %q is reserved", where, f.Key)
			}
			if keys[f.Key] {
				return fmt.Errorf("%s: duplicate key %q", where, f.Key)
			}
			keys[f.Key] = true
			switch f.Type {
			case "select", "multiselect":
				if len(f.Options) == 0 {
					return fmt.Errorf("%s: %s requires options", where, f.Type)
				}
				if f.Default != "" && !slices.Contains(f.Options, f.Default) {
					return fmt.Errorf("%s: default %q not in options", where, f.Default)
				}
			}
			if err := validateConstraints(f); err != nil {
				return fmt.Errorf("%s: %w", where, err)
			}
			if err := validatePrefill(f); err != nil {
				return fmt.Errorf("%s: %w", where, err)
			}
		}
	}
	if t.Filename != "" {
		if _, err := compile(t.Filename); err != nil {
			return fmt.Errorf("filename template: %w", err)
		}
	}
	if _, err := compile(t.Body); err != nil {
		return fmt.Errorf("body template: %w", err)
	}
	return nil
}

// validateConstraints checks the pattern/min_len/max_len declarations on one
// field at load time: they apply only to input/text, the regex must compile,
// the bounds must be coherent, and a declared default must itself pass — so
// authoring mistakes surface as load warnings, not at fill time.
func validateConstraints(f Field) error {
	hasConstraints := f.Pattern != "" || f.PatternHint != "" || f.MinLen != 0 || f.MaxLen != 0
	if !hasConstraints {
		return nil
	}
	if f.Type != "input" && f.Type != "text" {
		return fmt.Errorf("pattern/min_len/max_len apply only to input and text fields, not %s", f.Type)
	}
	if f.Pattern != "" {
		if _, err := regexp.Compile(f.Pattern); err != nil {
			return fmt.Errorf("pattern: %w", err)
		}
	}
	if f.PatternHint != "" && f.Pattern == "" {
		return fmt.Errorf("pattern_hint without pattern")
	}
	if f.MinLen < 0 || f.MaxLen < 0 {
		return fmt.Errorf("min_len/max_len cannot be negative")
	}
	if f.MaxLen > 0 && f.MinLen > f.MaxLen {
		return fmt.Errorf("min_len %d exceeds max_len %d", f.MinLen, f.MaxLen)
	}
	if f.Default != "" {
		if err := f.Validator()(f.Default); err != nil {
			return fmt.Errorf("default %q: %w", f.Default, err)
		}
	}
	return nil
}

// validatePrefill checks the prefill declaration: known source, text-like
// field, and not combined with a default (the two would fight over the
// initial value).
func validatePrefill(f Field) error {
	if f.Prefill == "" {
		return nil
	}
	if f.Prefill != PrefillClipboard {
		return fmt.Errorf("unknown prefill source %q (only %q is supported)", f.Prefill, PrefillClipboard)
	}
	if f.Type != "input" && f.Type != "text" {
		return fmt.Errorf("prefill applies only to input and text fields, not %s", f.Type)
	}
	if f.Default != "" {
		return fmt.Errorf("prefill and default cannot be combined")
	}
	return nil
}

// Validator returns the value check for an input/text field — required,
// length bounds, and pattern. It is the single source of fill-time
// validation, attached per-field to the huh form and applied by
// PrepareValues, so the interactive and Lua paths cannot drift. An empty
// value on a non-required field passes (constraints bind only when the user
// provides something).
func (f Field) Validator() func(string) error {
	var re *regexp.Regexp
	var reErr error
	if f.Pattern != "" {
		re, reErr = regexp.Compile(f.Pattern)
	}
	return func(s string) error {
		if strings.TrimSpace(s) == "" {
			if f.Required {
				return fmt.Errorf("required")
			}
			return nil
		}
		if reErr != nil {
			return fmt.Errorf("pattern: %w", reErr)
		}
		n := utf8.RuneCountInString(s)
		if f.MinLen > 0 && n < f.MinLen {
			return fmt.Errorf("must be at least %d characters", f.MinLen)
		}
		if f.MaxLen > 0 && n > f.MaxLen {
			return fmt.Errorf("must be at most %d characters", f.MaxLen)
		}
		if re != nil && !re.MatchString(s) {
			if f.PatternHint != "" {
				return fmt.Errorf("%s", f.PatternHint)
			}
			return fmt.Errorf("must match %s", f.Pattern)
		}
		return nil
	}
}

// compile parses a Go text/template with the template FuncMap and the same
// missingkey=error policy as custom commands (config.renderTemplate).
func compile(s string) (*texttemplate.Template, error) {
	return texttemplate.New("tmpl").
		Funcs(funcMap()).
		Option("missingkey=error").
		Parse(s)
}

// funcMap is the function set available to filename and body templates.
// Documented in TEMPLATES.md — extend both together.
func funcMap() texttemplate.FuncMap {
	return texttemplate.FuncMap{
		"env":  os.Getenv,
		"join": strings.Join,
		"slug": Slugify,

		// now formats the current local time with a Go layout, e.g.
		// {{now "2006-01-02"}} → 2026-06-05. Makes renders intentionally
		// non-deterministic; parse-time compile checks stay syntax-only.
		"now": func(layout string) string { return time.Now().Format(layout) },

		// checklist renders a multiselect's values as markdown task items:
		// {{checklist .areas}} → "- [ ] UI\n- [ ] backend".
		"checklist": func(items []string) string {
			lines := make([]string, len(items))
			for i, it := range items {
				lines[i] = "- [ ] " + it
			}
			return strings.Join(lines, "\n")
		},

		// default substitutes a fallback for empty optional answers:
		// {{default "unset" .severity}}. Argument order (fallback first)
		// allows piping: {{.severity | default "unset"}}.
		"default": func(fallback, v string) string {
			if strings.TrimSpace(v) == "" {
				return fallback
			}
			return v
		},

		"upper": strings.ToUpper,
		"lower": strings.ToLower,
		"title": titleCase,
		"trim":  strings.TrimSpace,

		// truncate bounds v to n runes, appending … when it cut anything:
		// {{truncate 50 .title}}. Argument order allows piping.
		"truncate": func(n int, v string) string {
			if n <= 0 || utf8.RuneCountInString(v) <= n {
				return v
			}
			return string([]rune(v)[:n]) + "…"
		},
	}
}

// titleCase uppercases the first letter of every space-separated word.
// Deliberately simple (no small-word rules); strings.Title is deprecated and
// x/text/cases is a heavyweight dependency for a template nicety.
func titleCase(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		r := []rune(w)
		r[0] = unicode.ToUpper(r[0])
		words[i] = string(r)
	}
	return strings.Join(words, " ")
}

// Slugify lowercases s and maps every run of non-alphanumeric characters to a
// single "-", trimming leading/trailing dashes. Exposed to templates as
// {{slug .var}} so free-text answers can be used safely in filenames.
func Slugify(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		default:
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}

// PrepareValues normalizes an answer set for t. It rejects unknown keys
// (likely typos), validates types and select/multiselect options, applies
// field defaults for omitted keys, and enforces Required. When the template
// declares no filename, values[FilenameKey] is required. The returned map is
// ready for Render.
func PrepareValues(t Template, values map[string]any) (map[string]any, error) {
	known := map[string]bool{}
	for _, step := range t.Steps {
		for _, f := range step.Fields {
			if f.Type != "note" {
				known[f.Key] = true
			}
		}
	}
	for k := range values {
		if k == FilenameKey && t.Filename == "" {
			continue
		}
		if !known[k] {
			return nil, fmt.Errorf("unknown field key %q", k)
		}
	}

	out := make(map[string]any, len(known)+1)
	for _, step := range t.Steps {
		for _, f := range step.Fields {
			if f.Type == "note" {
				continue
			}
			v, err := prepareFieldValue(f, values[f.Key])
			if err != nil {
				return nil, fmt.Errorf("field %q: %w", f.Key, err)
			}
			out[f.Key] = v
		}
	}

	if t.Filename == "" {
		name, _ := values[FilenameKey].(string)
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("template declares no filename: pass %s", FilenameKey)
		}
		out[FilenameKey] = name
	}
	return out, nil
}

// prepareFieldValue resolves one field: the provided value when present
// (type-checked), otherwise the field default — mirroring what the
// interactive form would pre-fill.
func prepareFieldValue(f Field, v any) (any, error) {
	switch f.Type {
	case "confirm":
		if v == nil {
			return f.Default == "true", nil
		}
		b, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("expected a boolean, got %T", v)
		}
		return b, nil
	case "select":
		if v == nil {
			if f.Default != "" {
				return f.Default, nil
			}
			return f.Options[0], nil // the form preselects the first option
		}
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("expected a string, got %T", v)
		}
		if !slices.Contains(f.Options, s) {
			return nil, fmt.Errorf("%q is not one of the options %v", s, f.Options)
		}
		return s, nil
	case "multiselect":
		sel, err := toStringSlice(v)
		if err != nil {
			return nil, err
		}
		if sel == nil && f.Default != "" {
			sel = []string{f.Default}
		}
		for _, s := range sel {
			if !slices.Contains(f.Options, s) {
				return nil, fmt.Errorf("%q is not one of the options %v", s, f.Options)
			}
		}
		if f.Required && len(sel) == 0 {
			return nil, fmt.Errorf("required")
		}
		if sel == nil {
			sel = []string{}
		}
		return sel, nil
	default: // input, text
		s := f.Default
		if v != nil {
			var ok bool
			if s, ok = v.(string); !ok {
				return nil, fmt.Errorf("expected a string, got %T", v)
			}
		}
		if err := f.Validator()(s); err != nil {
			return nil, err
		}
		return s, nil
	}
}

// toStringSlice accepts []string or []any-of-strings (the shape a Lua table
// arrives as). nil stays nil so the caller can apply defaults.
func toStringSlice(v any) ([]string, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case []string:
		return x, nil
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			s, ok := e.(string)
			if !ok {
				return nil, fmt.Errorf("expected a list of strings, got element %T", e)
			}
			out = append(out, s)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("expected a list of strings, got %T", v)
	}
}

// Render expands the template's filename and body against the prepared
// values overlaid on the VarContext variables. When the template declares no
// filename, values[FilenameKey] is used instead. The filename is flattened
// to a single line; final validation stays with board.SanitizeName at create
// time.
func Render(t Template, vctx board.VarContext, values map[string]any) (filename, body string, err error) {
	data := make(map[string]any)
	for k, v := range vctx.Vars() {
		data[k] = v
	}
	maps.Copy(data, values)

	if t.Filename != "" {
		filename, err = execute(t.Filename, data)
		if err != nil {
			return "", "", fmt.Errorf("filename: %w", err)
		}
	} else {
		filename, _ = values[FilenameKey].(string)
	}
	filename = strings.Join(strings.Fields(filename), " ")

	body, err = execute(t.Body, data)
	if err != nil {
		return "", "", fmt.Errorf("body: %w", err)
	}
	return filename, body, nil
}

// Instantiate is the one-call entry point shared by the TUI form flow and
// the Lua API: PrepareValues → Render → board.SanitizeName. It returns the
// sanitized card name (without .md) and the rendered body, ready for
// board.CreateItem.
func Instantiate(t Template, vctx board.VarContext, values map[string]any) (name, body string, err error) {
	prepared, err := PrepareValues(t, values)
	if err != nil {
		return "", "", err
	}
	rawName, body, err := Render(t, vctx, prepared)
	if err != nil {
		return "", "", err
	}
	name, err = board.SanitizeName(rawName)
	if err != nil {
		return "", "", fmt.Errorf("invalid filename from template: %w", err)
	}
	return name, body, nil
}

func execute(s string, data map[string]any) (string, error) {
	tmpl, err := compile(s)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
