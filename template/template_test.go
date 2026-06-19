package template

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kbrd/board"
)

func writeTemplate(t *testing.T, dir, name, content string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const validTemplate = `---
name: Bug report
filename: "bug-{{.slug}}"
steps:
  - title: Basics
    fields:
      - key: slug
        type: input
        title: Short slug
        required: true
      - key: severity
        type: select
        title: Severity
        options: [low, medium, high]
        default: medium
  - title: Details
    fields:
      - key: repro
        type: text
        title: Repro steps
---
# Bug: {{.slug}}

Severity: {{.severity}}

## Repro
{{.repro}}
`

func TestParseTemplateValid(t *testing.T) {
	path := writeTemplate(t, t.TempDir(), "bug.md", validTemplate)
	tmpl, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if tmpl.Name != "Bug report" {
		t.Errorf("Name = %q", tmpl.Name)
	}
	if tmpl.Filename != "bug-{{.slug}}" {
		t.Errorf("Filename = %q", tmpl.Filename)
	}
	if len(tmpl.Steps) != 2 || len(tmpl.Steps[0].Fields) != 2 {
		t.Fatalf("Steps = %+v", tmpl.Steps)
	}
	if !strings.HasPrefix(tmpl.Body, "# Bug: {{.slug}}") {
		t.Errorf("Body = %q", tmpl.Body)
	}
}

func TestParseTemplateNameFallback(t *testing.T) {
	path := writeTemplate(t, t.TempDir(), "chore.md", "---\n---\nbody\n")
	tmpl, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if tmpl.Name != "chore" {
		t.Errorf("Name = %q, want file base name fallback", tmpl.Name)
	}
}

func TestParseTemplateErrors(t *testing.T) {
	cases := []struct {
		name, content, wantErr string
	}{
		{"no frontmatter", "# just markdown\n", "missing frontmatter"},
		{"unterminated", "---\nname: x\n", "unterminated frontmatter"},
		{"bad yaml", "---\nname: [\n---\nbody\n", "frontmatter"},
		{"unknown type", "---\nsteps:\n  - fields:\n      - {key: a, type: dropdown}\n---\n", "unknown type"},
		{"missing key", "---\nsteps:\n  - fields:\n      - {type: input}\n---\n", "missing key"},
		{"reserved key", "---\nsteps:\n  - fields:\n      - {key: boardName, type: input}\n---\n", "reserved"},
		{"duplicate key", "---\nsteps:\n  - fields:\n      - {key: a, type: input}\n      - {key: a, type: input}\n---\n", "duplicate key"},
		{"select no options", "---\nsteps:\n  - fields:\n      - {key: a, type: select}\n---\n", "requires options"},
		{"default not in options", "---\nsteps:\n  - fields:\n      - {key: a, type: select, options: [x], default: y}\n---\n", "not in options"},
		{"bad filename template", "---\nfilename: \"{{.x\"\n---\n", "filename template"},
		{"bad body template", "---\n---\n{{.x\n", "body template"},
	}
	dir := t.TempDir()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := writeTemplate(t, dir, "t.md", c.content)
			_, err := Parse(path)
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("err = %v, want containing %q", err, c.wantErr)
			}
		})
	}
}

func TestParseTemplateNoteNeedsNoKey(t *testing.T) {
	path := writeTemplate(t, t.TempDir(), "n.md", "---\nsteps:\n  - fields:\n      - {type: note, title: Heads up}\n---\nbody\n")
	if _, err := Parse(path); err != nil {
		t.Fatal(err)
	}
}

func TestListTemplatesMergeAndShadow(t *testing.T) {
	boardDir := t.TempDir()
	colDir := filepath.Join(boardDir, "todo")
	writeTemplate(t, filepath.Join(colDir, Dir), "bug.md",
		"---\nname: Bug report\nfilename: col-bug\n---\ncolumn version\n")
	writeTemplate(t, filepath.Join(boardDir, Dir), "bug.md",
		"---\nname: Bug report\nfilename: board-bug\n---\nboard version\n")
	writeTemplate(t, filepath.Join(boardDir, Dir), "chore.md",
		"---\nname: Chore\nfilename: chore\n---\nchore\n")

	tmpls, warns, err := List(boardDir, colDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 0 {
		t.Errorf("warns = %v", warns)
	}
	if len(tmpls) != 2 {
		t.Fatalf("got %d templates: %+v", len(tmpls), tmpls)
	}
	// Column-local "Bug report" shadows the board one.
	if tmpls[0].Name != "Bug report" || tmpls[0].Scope != ScopeColumn || tmpls[0].Filename != "col-bug" {
		t.Errorf("tmpls[0] = %+v", tmpls[0])
	}
	if tmpls[1].Name != "Chore" || tmpls[1].Scope != ScopeBoard {
		t.Errorf("tmpls[1] = %+v", tmpls[1])
	}
}

func TestListTemplatesMissingDirsAndWarnings(t *testing.T) {
	boardDir := t.TempDir()
	colDir := filepath.Join(boardDir, "todo")
	// No template dirs at all: empty result, no error.
	tmpls, warns, err := List(boardDir, colDir)
	if err != nil || len(tmpls) != 0 || len(warns) != 0 {
		t.Fatalf("tmpls=%v warns=%v err=%v", tmpls, warns, err)
	}

	writeTemplate(t, filepath.Join(boardDir, Dir), "broken.md", "no frontmatter\n")
	writeTemplate(t, filepath.Join(boardDir, Dir), "ok.md", "---\nname: OK\nfilename: ok\n---\nfine\n")
	writeTemplate(t, filepath.Join(boardDir, Dir), "_hidden.md", "ignored\n")
	tmpls, warns, err = List(boardDir, colDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(tmpls) != 1 || tmpls[0].Name != "OK" {
		t.Errorf("tmpls = %+v", tmpls)
	}
	if len(warns) != 1 || !strings.HasSuffix(warns[0].Path, "broken.md") {
		t.Errorf("warns = %+v", warns)
	}
}

func TestRender(t *testing.T) {
	path := writeTemplate(t, t.TempDir(), "bug.md", validTemplate)
	tmpl, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	vctx := board.VarContext{BoardPath: "/b", BoardName: "demo", ColumnPath: "/b/todo", ColumnName: "todo"}
	name, body, err := Render(tmpl, vctx, map[string]any{
		"slug": "crash", "severity": "high", "repro": "run it",
	})
	if err != nil {
		t.Fatal(err)
	}
	if name != "bug-crash" {
		t.Errorf("filename = %q", name)
	}
	want := "# Bug: crash\n\nSeverity: high\n\n## Repro\nrun it\n"
	if body != want {
		t.Errorf("body = %q, want %q", body, want)
	}
}

func TestRenderTemplateVarsAndFuncs(t *testing.T) {
	path := writeTemplate(t, t.TempDir(), "t.md",
		"---\nfilename: \"{{slug .title}}\"\n---\n{{.columnName}}: {{join .tags \", \"}}\n")
	tmpl, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	vctx := board.VarContext{BoardPath: "/b", BoardName: "demo", ColumnPath: "/b/todo", ColumnName: "todo"}
	name, body, err := Render(tmpl, vctx, map[string]any{
		"title": "Fix the  CI!!", "tags": []string{"a", "b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if name != "fix-the-ci" {
		t.Errorf("filename = %q", name)
	}
	if body != "todo: a, b\n" {
		t.Errorf("body = %q", body)
	}
}

func TestRenderTemplateDateFunc(t *testing.T) {
	vctx := board.VarContext{BoardPath: "/b", BoardName: "b"}

	// "today" with the default layout resolves to the current date.
	path := writeTemplate(t, t.TempDir(), "t.md", "---\nfilename: x\n---\n{{date \"today\"}}\n")
	tmpl, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	_, body, err := Render(tmpl, vctx, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Now().Format("2006-01-02") + "\n"; body != want {
		t.Errorf("body = %q, want %q", body, want)
	}

	// An optional Go layout is honored (Polish phrase, custom format).
	path = writeTemplate(t, t.TempDir(), "t2.md", "---\nfilename: x\n---\n{{date \"dziś\" \"2006/01/02\"}}\n")
	tmpl, err = Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	_, body, err = Render(tmpl, vctx, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Now().Format("2006/01/02") + "\n"; body != want {
		t.Errorf("body = %q, want %q", body, want)
	}

	// An unparseable phrase fails the render.
	path = writeTemplate(t, t.TempDir(), "t3.md", "---\nfilename: x\n---\n{{date \"florble\"}}\n")
	tmpl, err = Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Render(tmpl, vctx, map[string]any{}); err == nil {
		t.Error("expected error for unparseable phrase")
	}
}

func TestRenderTemplateMissingKey(t *testing.T) {
	path := writeTemplate(t, t.TempDir(), "t.md", "---\nfilename: x\n---\n{{.undeclared}}\n")
	tmpl, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = Render(tmpl, board.VarContext{BoardPath: "/b", BoardName: "b"}, map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "body") {
		t.Errorf("err = %v, want body render error", err)
	}
}

func TestRenderTemplateSyntheticFilename(t *testing.T) {
	path := writeTemplate(t, t.TempDir(), "t.md", "---\n---\nhello\n")
	tmpl, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	name, _, err := Render(tmpl, board.VarContext{BoardPath: "/b", BoardName: "b"},
		map[string]any{FilenameKey: "my\ncard\tname"})
	if err != nil {
		t.Fatal(err)
	}
	if name != "my card name" {
		t.Errorf("filename = %q, want whitespace flattened", name)
	}
	// A bad rendered name is still caught downstream by SanitizeName.
	if _, err := board.SanitizeName("../escape"); !errors.Is(err, board.ErrBadName) {
		t.Errorf("SanitizeName guard = %v", err)
	}
}

func prepareTestTemplate(t *testing.T) Template {
	t.Helper()
	path := writeTemplate(t, t.TempDir(), "t.md", `---
filename: "x-{{.title}}"
steps:
  - fields:
      - {key: title, type: input, required: true}
      - {key: severity, type: select, options: [low, high], default: low}
      - {key: areas, type: multiselect, options: [a, b], required: true}
      - {key: urgent, type: confirm, default: "true"}
      - {key: notes, type: text, default: "n/a"}
      - {type: note, title: heads up}
---
body
`)
	tmpl, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	return tmpl
}

func TestPrepareValuesDefaultsAndCoercion(t *testing.T) {
	tmpl := prepareTestTemplate(t)
	// Minimal provided set: required fields only; everything else defaults.
	// areas arrives as []any, the shape a Lua table converts to.
	out, err := PrepareValues(tmpl, map[string]any{
		"title": "hello",
		"areas": []any{"a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"title":    "hello",
		"severity": "low",
		"areas":    []string{"a"},
		"urgent":   true,
		"notes":    "n/a",
	}
	if len(out) != len(want) {
		t.Fatalf("out = %#v", out)
	}
	for k, w := range want {
		switch wv := w.(type) {
		case []string:
			got, ok := out[k].([]string)
			if !ok || len(got) != len(wv) || got[0] != wv[0] {
				t.Errorf("%s = %#v, want %#v", k, out[k], w)
			}
		default:
			if out[k] != w {
				t.Errorf("%s = %#v, want %#v", k, out[k], w)
			}
		}
	}
}

func TestPrepareValuesErrors(t *testing.T) {
	tmpl := prepareTestTemplate(t)
	base := map[string]any{"title": "x", "areas": []any{"a"}}
	with := func(k string, v any) map[string]any {
		m := map[string]any{}
		maps.Copy(m, base)
		m[k] = v
		return m
	}
	cases := []struct {
		name    string
		values  map[string]any
		wantErr string
	}{
		{"unknown key", with("typo", "x"), "unknown field key"},
		{"missing required input", map[string]any{"areas": []any{"a"}}, `field "title": required`},
		{"blank required input", with("title", "  "), `field "title": required`},
		{"empty required multiselect", map[string]any{"title": "x", "areas": []any{}}, `field "areas": required`},
		{"select not in options", with("severity", "nope"), "not one of the options"},
		{"multiselect not in options", with("areas", []any{"z"}), "not one of the options"},
		{"confirm wrong type", with("urgent", "yes"), "expected a boolean"},
		{"input wrong type", with("title", 42.0), "expected a string"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := PrepareValues(tmpl, c.values)
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("err = %v, want containing %q", err, c.wantErr)
			}
		})
	}
}

func TestPrepareValuesSyntheticFilename(t *testing.T) {
	path := writeTemplate(t, t.TempDir(), "t.md", "---\n---\nbody\n")
	tmpl, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareValues(tmpl, map[string]any{}); err == nil ||
		!strings.Contains(err.Error(), FilenameKey) {
		t.Errorf("err = %v, want missing-filename error", err)
	}
	out, err := PrepareValues(tmpl, map[string]any{FilenameKey: "card"})
	if err != nil {
		t.Fatal(err)
	}
	if out[FilenameKey] != "card" {
		t.Errorf("out = %#v", out)
	}
}

func TestParseConstraintErrors(t *testing.T) {
	cases := []struct {
		name, fields, wantErr string
	}{
		{"bad regex", `- {key: a, type: input, pattern: "["}`, "pattern:"},
		{"constraints on select", `- {key: a, type: select, options: [x], max_len: 3}`, "apply only to input and text"},
		{"hint without pattern", `- {key: a, type: input, pattern_hint: "nope"}`, "pattern_hint without pattern"},
		{"min exceeds max", `- {key: a, type: input, min_len: 5, max_len: 3}`, "exceeds max_len"},
		{"negative bound", `- {key: a, type: input, min_len: -1}`, "negative"},
		{"default fails pattern", `- {key: a, type: input, pattern: "^[0-9]+$", default: abc}`, "default"},
		{"default fails max_len", `- {key: a, type: input, max_len: 2, default: abc}`, "default"},
	}
	dir := t.TempDir()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := writeTemplate(t, dir, "t.md", "---\nsteps:\n  - fields:\n      "+c.fields+"\n---\n")
			_, err := Parse(path)
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("err = %v, want containing %q", err, c.wantErr)
			}
		})
	}
}

func TestParsePrefill(t *testing.T) {
	// Valid: prefill clipboard on input and text.
	path := writeTemplate(t, t.TempDir(), "ok.md", `---
filename: x
steps:
  - fields:
      - {key: a, type: input, prefill: clipboard}
      - {key: b, type: text, prefill: clipboard}
---
`)
	tmpl, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if tmpl.Steps[0].Fields[0].Prefill != PrefillClipboard {
		t.Errorf("Prefill = %q", tmpl.Steps[0].Fields[0].Prefill)
	}

	cases := []struct {
		name, fields, wantErr string
	}{
		{"unknown source", `- {key: a, type: input, prefill: selection}`, "unknown prefill source"},
		{"typo", `- {key: a, type: input, prefill: clipbord}`, "unknown prefill source"},
		{"on select", `- {key: a, type: select, options: [x], prefill: clipboard}`, "only to input and text"},
		{"with default", `- {key: a, type: input, prefill: clipboard, default: x}`, "cannot be combined"},
	}
	dir := t.TempDir()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := writeTemplate(t, dir, "t.md", "---\nsteps:\n  - fields:\n      "+c.fields+"\n---\n")
			_, err := Parse(path)
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("err = %v, want containing %q", err, c.wantErr)
			}
		})
	}
}

func TestFieldValidator(t *testing.T) {
	f := Field{Key: "ticket", Type: "input",
		Pattern: "^KB-[0-9]+$", PatternHint: "must look like KB-123",
		MinLen: 4, MaxLen: 8}
	v := f.Validator()
	if err := v("KB-42"); err != nil {
		t.Errorf("valid value rejected: %v", err)
	}
	if err := v("nope"); err == nil || err.Error() != "must look like KB-123" {
		t.Errorf("pattern err = %v, want hint", err)
	}
	if err := v("KB-1234567"); err == nil || !strings.Contains(err.Error(), "at most 8") {
		t.Errorf("max_len err = %v", err)
	}
	if err := v("KB1"); err == nil || !strings.Contains(err.Error(), "at least 4") {
		t.Errorf("min_len err = %v", err)
	}
	// Optional + empty: constraints don't bind.
	if err := v(""); err != nil {
		t.Errorf("empty optional rejected: %v", err)
	}
	// Required + empty: rejected before constraints.
	f.Required = true
	if err := f.Validator()(""); err == nil || err.Error() != "required" {
		t.Errorf("required err = %v", err)
	}
	// Without a hint the raw pattern is named.
	f.PatternHint = ""
	if err := f.Validator()("nope"); err == nil || !strings.Contains(err.Error(), "must match ^KB-") {
		t.Errorf("raw pattern err = %v", err)
	}
}

func TestPrepareValuesAppliesConstraints(t *testing.T) {
	path := writeTemplate(t, t.TempDir(), "t.md", `---
filename: x
steps:
  - fields:
      - {key: ticket, type: input, pattern: "^KB-[0-9]+$", pattern_hint: "must look like KB-123"}
---
{{.ticket}}
`)
	tmpl, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareValues(tmpl, map[string]any{"ticket": "garbage"}); err == nil ||
		!strings.Contains(err.Error(), "must look like KB-123") {
		t.Errorf("err = %v, want pattern hint", err)
	}
	if _, err := PrepareValues(tmpl, map[string]any{"ticket": "KB-7"}); err != nil {
		t.Errorf("valid value rejected: %v", err)
	}
}

// TestInstantiate covers the shared TUI/Lua entry point: prepare → render →
// sanitize in one call.
func TestInstantiate(t *testing.T) {
	tmpl := prepareTestTemplate(t)
	vctx := board.VarContext{BoardPath: "/b", BoardName: "demo"}
	name, body, err := Instantiate(tmpl, vctx, map[string]any{
		"title": "Hi", "areas": []string{"a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if name != "x-Hi" || body != "body\n" {
		t.Errorf("name=%q body=%q", name, body)
	}
	// Required violations surface through the same call.
	if _, _, err := Instantiate(tmpl, vctx, map[string]any{"areas": []string{"a"}}); err == nil {
		t.Error("want required error")
	}
	// A filename that renders to something path-escaping is rejected.
	if _, _, err := Instantiate(tmpl, vctx, map[string]any{
		"title": "../up", "areas": []string{"a"},
	}); err == nil || !errors.Is(err, board.ErrBadName) {
		t.Errorf("err = %v, want ErrBadName", err)
	}
}

// renderBody parses a template whose body is src and renders it with values.
func renderBody(t *testing.T, src string, values map[string]any) string {
	t.Helper()
	path := writeTemplate(t, t.TempDir(), "t.md", "---\nfilename: x\n---\n"+src)
	tmpl, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	_, body, err := Render(tmpl, board.VarContext{BoardPath: "/b", BoardName: "b"}, values)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestTemplateFuncs(t *testing.T) {
	cases := []struct {
		name, src, want string
		values          map[string]any
	}{
		{"checklist", `{{checklist .areas}}`, "- [ ] UI\n- [ ] backend\n",
			map[string]any{"areas": []string{"UI", "backend"}}},
		{"checklist empty", `[{{checklist .areas}}]`, "[]\n",
			map[string]any{"areas": []string{}}},
		{"default used", `{{default "unset" .sev}}`, "unset\n",
			map[string]any{"sev": "  "}},
		{"default bypassed", `{{default "unset" .sev}}`, "high\n",
			map[string]any{"sev": "high"}},
		{"default piped", `{{.sev | default "unset"}}`, "unset\n",
			map[string]any{"sev": ""}},
		{"upper", `{{upper .v}}`, "ABC\n", map[string]any{"v": "abc"}},
		{"lower", `{{lower .v}}`, "abc\n", map[string]any{"v": "ABC"}},
		{"title", `{{title .v}}`, "Fix The CI\n", map[string]any{"v": "fix the CI"}},
		{"trim", `[{{trim .v}}]`, "[x]\n", map[string]any{"v": "  x\n"}},
		{"truncate cuts", `{{truncate 3 .v}}`, "abc…\n", map[string]any{"v": "abcdef"}},
		{"truncate fits", `{{truncate 10 .v}}`, "abcdef\n", map[string]any{"v": "abcdef"}},
		{"truncate runes", `{{truncate 2 .v}}`, "żó…\n", map[string]any{"v": "żółć"}},
		{"truncate piped", `{{.v | truncate 3}}`, "abc…\n", map[string]any{"v": "abcdef"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := renderBody(t, c.src+"\n", c.values); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestTemplateFuncNow(t *testing.T) {
	got := renderBody(t, `{{now "2006-01-02"}}`+"\n", nil)
	want := time.Now().Format("2006-01-02") + "\n"
	// A midnight rollover between render and check is the only flake window;
	// re-derive once if they disagree.
	if got != want {
		want = time.Now().Format("2006-01-02") + "\n"
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// now also works in filenames.
	path := writeTemplate(t, t.TempDir(), "t.md", "---\nfilename: '{{now \"2006\"}}-x'\n---\nb\n")
	tmpl, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	name, _, err := Render(tmpl, board.VarContext{BoardPath: "/b", BoardName: "b"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Now().Format("2006") + "-x"; name != want {
		t.Errorf("filename = %q, want %q", name, want)
	}
}

func TestTitleCase(t *testing.T) {
	cases := map[string]string{
		"fix the CI":   "Fix The CI",
		"żółta kaczka": "Żółta Kaczka",
		"":             "",
		"  spaced  ":   "Spaced",
	}
	for in, want := range cases {
		if got := titleCase(in); got != want {
			t.Errorf("titleCase(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestShellFuncEmitsMarker(t *testing.T) {
	body := renderBody(t, `{{shell "cat -" .a .b}}`, map[string]any{"a": "x", "b": "y"})
	markers := ParseShellMarkers(body)
	if len(markers) != 1 {
		t.Fatalf("got %d markers: %q", len(markers), body)
	}
	if markers[0].Cmd != "cat -" {
		t.Errorf("cmd = %q", markers[0].Cmd)
	}
	if markers[0].Stdin != "xy" {
		t.Errorf("stdin = %q", markers[0].Stdin)
	}
	if markers[0].ID != 1 {
		t.Errorf("id = %d", markers[0].ID)
	}
}

func TestShellMarkersDistinctIDs(t *testing.T) {
	body := renderBody(t, "{{shell \"a\"}}\n{{shell \"b\"}}", nil)
	markers := ParseShellMarkers(body)
	if len(markers) != 2 || markers[0].ID == markers[1].ID {
		t.Fatalf("markers = %+v", markers)
	}
	if markers[0].Cmd != "a" || markers[1].Cmd != "b" {
		t.Errorf("cmds = %q, %q", markers[0].Cmd, markers[1].Cmd)
	}
}

func TestShellMarkerRoundTripSpecialChars(t *testing.T) {
	// A command with --> and stdin with newlines must survive base64 round-trip
	// without breaking the marker.
	cmd := `sh -c 'echo --> done'`
	stdin := "line1\nline2\n-->\nline3"
	body := "before\n" + RenderShellMarker(7, cmd, stdin) + "\nafter"
	markers := ParseShellMarkers(body)
	if len(markers) != 1 {
		t.Fatalf("got %d markers", len(markers))
	}
	if markers[0].ID != 7 || markers[0].Cmd != cmd || markers[0].Stdin != stdin {
		t.Errorf("round-trip mismatch: %+v", markers[0])
	}
}

func TestRewriteShellMarker(t *testing.T) {
	body := "A\n" + RenderShellMarker(1, "cmd1", "") + "\nB\n" + RenderShellMarker(2, "cmd2", "") + "\nC"
	got := RewriteShellMarker(body, 1, "RESULT1")
	if !strings.Contains(got, "RESULT1") {
		t.Errorf("id=1 not replaced: %q", got)
	}
	// id=2 marker must survive intact.
	rest := ParseShellMarkers(got)
	if len(rest) != 1 || rest[0].ID != 2 || rest[0].Cmd != "cmd2" {
		t.Errorf("id=2 disturbed: %+v", rest)
	}
	// Unknown id leaves body unchanged.
	if RewriteShellMarker(body, 99, "x") != body {
		t.Error("unknown id altered body")
	}
}

func TestParseShellMarkersIgnoresMalformed(t *testing.T) {
	// Mismatched open/close ids → not a marker.
	body := "<!-- kbrd:shell id=1 cmd=Y2Q= stdin= -->\nx\n<!-- kbrd:/shell id=2 -->"
	if m := ParseShellMarkers(body); len(m) != 0 {
		t.Errorf("expected no markers, got %+v", m)
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Hello World":   "hello-world",
		"  --weird--  ": "weird",
		"CamelCase42":   "camelcase42",
		"":              "",
		"!!!":           "",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}
