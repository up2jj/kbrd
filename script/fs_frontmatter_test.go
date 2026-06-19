package script

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestGetFrontmatter verifies kbrd.fs.get_frontmatter reads a single key and the
// whole table, returns nil for an absent key, and surfaces an unquoted YAML date
// as the string a script can re-parse.
func TestGetFrontmatter(t *testing.T) {
	root := t.TempDir()
	card := filepath.Join(root, "card.md")
	body := "---\ndue: 2026-06-24\nwhen: next friday\nn: 3\npinned: true\n---\nbody\n"
	if err := os.WriteFile(card, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	dir := writeInit(t, fmt.Sprintf(`
CARD = %q
due = kbrd.fs.get_frontmatter(CARD, "due")
when = kbrd.fs.get_frontmatter(CARD, "when")
missing = kbrd.fs.get_frontmatter(CARD, "nope")
missing_is_nil = missing == nil
all = kbrd.fs.get_frontmatter(CARD)
all_due = all.due
all_pinned = all.pinned
all_n = all.n
`, card))
	h, err := New(defaultCfg(), &fakeAPI{}, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	if got := luaString(t, h, "due"); got != "2026-06-24" {
		t.Errorf("due = %q, want 2026-06-24 (YAML date → string)", got)
	}
	if got := luaString(t, h, "when"); got != "next friday" {
		t.Errorf("when = %q, want \"next friday\"", got)
	}
	if !luaBool(t, h, "missing_is_nil") {
		t.Error("absent key should return nil")
	}
	if got := luaString(t, h, "all_due"); got != "2026-06-24" {
		t.Errorf("all.due = %q, want 2026-06-24", got)
	}
	if !luaBool(t, h, "all_pinned") {
		t.Error("all.pinned should be true")
	}
	if got := luaNumber(t, h, "all_n"); got != 3 {
		t.Errorf("all.n = %v, want 3", got)
	}
}

// TestGetFrontmatterMissingFile confirms a read error returns (nil, err).
func TestGetFrontmatterMissingFile(t *testing.T) {
	dir := writeInit(t, `
ok, err = kbrd.fs.get_frontmatter("/no/such/card.md")
ok_is_nil = ok == nil
err_is_string = type(err) == "string"
`)
	h, err := New(defaultCfg(), &fakeAPI{}, nil, dir, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer h.Close()

	if !luaBool(t, h, "ok_is_nil") || !luaBool(t, h, "err_is_string") {
		t.Error("expected (nil, err) for a missing file")
	}
}
