package board

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"kbrd/recents"
)

// makeBoard builds a board directory with the given columns, each mapping to a
// slice of item base-names (created as <name>.md). Returns the board path.
func makeBoard(t *testing.T, cols map[string][]string) string {
	t.Helper()
	root := t.TempDir()
	for col, items := range cols {
		dir := filepath.Join(root, col)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, it := range items {
			if err := os.WriteFile(filepath.Join(dir, it+".md"), nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	return root
}

func TestColumns(t *testing.T) {
	root := makeBoard(t, map[string][]string{
		"2. doing": nil,
		"1. todo":  nil,
		"3. done":  nil,
	})
	// Hidden + non-dir entries must be skipped.
	os.MkdirAll(filepath.Join(root, ".git"), 0o755)
	os.MkdirAll(filepath.Join(root, "_archive"), 0o755)
	os.WriteFile(filepath.Join(root, "README.md"), nil, 0o644)

	got, err := Columns(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"1. todo", "2. doing", "3. done"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v (sorted)", got, want)
		}
	}
}

func TestItems(t *testing.T) {
	root := makeBoard(t, map[string][]string{"todo": {"b", "a"}})
	col := filepath.Join(root, "todo")
	os.WriteFile(filepath.Join(col, "_hidden.md"), nil, 0o644)
	os.WriteFile(filepath.Join(col, "notes.txt"), nil, 0o644) // non-md skipped
	os.MkdirAll(filepath.Join(col, "sub"), 0o755)             // dir skipped

	got, err := Items(col)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("got %v, want [a b]", got)
	}
}

func TestResolveColumn(t *testing.T) {
	root := makeBoard(t, map[string][]string{"1. todo": nil, "2. doing": nil})

	// Empty folder -> first column alphabetically.
	got, err := ResolveColumn(root, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != "1. todo" {
		t.Fatalf("default folder = %q, want '1. todo'", filepath.Base(got))
	}

	// Case-insensitive named match.
	got, err = ResolveColumn(root, "2. DOING", false)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != "2. doing" {
		t.Fatalf("named folder = %q", filepath.Base(got))
	}

	// Missing without autoCreate.
	if _, err := ResolveColumn(root, "nope", false); !errors.Is(err, ErrFolderNotFound) {
		t.Fatalf("err = %v, want ErrFolderNotFound", err)
	}

	// Missing with autoCreate.
	got, err = ResolveColumn(root, "3. done", true)
	if err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(got); err != nil || !fi.IsDir() {
		t.Fatalf("autoCreate did not make a dir: %v", err)
	}

	// Empty board, no folder -> ErrNoColumns.
	empty := t.TempDir()
	if _, err := ResolveColumn(empty, "", false); !errors.Is(err, ErrNoColumns) {
		t.Fatalf("err = %v, want ErrNoColumns", err)
	}
}

func TestSanitizeName(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"  task  ", "task", false},
		{"task.md", "task", false},
		{"my note.md", "my note", false},
		{"", "", true},
		{"   ", "", true},
		{".md", "", true},
		{"../escape", "", true},
		{"a/b", "", true},
		{`a\b`, "", true},
		{"..", "", true},
	}
	for _, c := range cases {
		got, err := SanitizeName(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("SanitizeName(%q) = %q, want error", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("SanitizeName(%q) error: %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("SanitizeName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSanitizeGeneratedName(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{" Fix / OAuth: P1! ", "fix-oauth-p1", false},
		{"Zażółć gęślą.md", "zażółć-gęślą", false},
		{"already.MD", "already", false},
		{"!!!", "", true},
		{"", "", true},
	}
	for _, c := range cases {
		got, err := SanitizeGeneratedName(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("SanitizeGeneratedName(%q) = %q, want error", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("SanitizeGeneratedName(%q) error: %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("SanitizeGeneratedName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCreateItem(t *testing.T) {
	col := t.TempDir()

	path, err := CreateItem(col, "hello", "world")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "hello.md" {
		t.Fatalf("path = %q", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "world\n" {
		t.Fatalf("content = %q, want %q (trailing newline added)", data, "world\n")
	}

	// Duplicate must fail.
	if _, err := CreateItem(col, "hello", "again"); !errors.Is(err, os.ErrExist) {
		t.Fatalf("duplicate err = %v, want os.ErrExist", err)
	}

	// Empty content -> empty file, no spurious newline.
	p2, err := CreateItem(col, "empty", "")
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(p2); len(data) != 0 {
		t.Fatalf("empty item content = %q, want empty", data)
	}

	// Bad name rejected before touching disk.
	if _, err := CreateItem(col, "../oops", "x"); !errors.Is(err, ErrBadName) {
		t.Fatalf("bad name err = %v, want ErrBadName", err)
	}
}

func TestResolveFrom(t *testing.T) {
	refs := []Ref{
		{Name: "Work", Path: "/a/work"},
		{Name: "Personal", Path: "/a/personal"},
		{Name: "", Path: "/a/notes-board"}, // no friendly name -> matched by base
	}

	// Exact case-insensitive.
	if got, err := resolveFrom("work", refs); err != nil || got.Path != "/a/work" {
		t.Fatalf("exact: got %+v err %v", got, err)
	}
	// Base-name match for unnamed board.
	if got, err := resolveFrom("notes-board", refs); err != nil || got.Path != "/a/notes-board" {
		t.Fatalf("base match: got %+v err %v", got, err)
	}
	// Fuzzy single candidate.
	if got, err := resolveFrom("persnl", refs); err != nil || got.Path != "/a/personal" {
		t.Fatalf("fuzzy: got %+v err %v", got, err)
	}
	// Not found.
	if _, err := resolveFrom("zzzzz", refs); !errors.Is(err, ErrBoardNotFound) {
		t.Fatalf("notfound err = %v", err)
	}
	// Ambiguous exact (two boards same label).
	dup := []Ref{{Name: "Dup", Path: "/x"}, {Name: "dup", Path: "/y"}}
	_, err := resolveFrom("dup", dup)
	if !errors.Is(err, ErrBoardAmbiguous) {
		t.Fatalf("ambiguous err = %v", err)
	}
	var ambiguous *AmbiguousBoardError
	if !errors.As(err, &ambiguous) {
		t.Fatalf("ambiguous err type = %T, want *AmbiguousBoardError", err)
	}
	if len(ambiguous.Candidates) != 2 || ambiguous.Candidates[0].Path != "/x" || ambiguous.Candidates[1].Path != "/y" {
		t.Fatalf("ambiguous candidates = %+v", ambiguous.Candidates)
	}
	// Empty store.
	if _, err := resolveFrom("anything", nil); !errors.Is(err, ErrBoardNotFound) {
		t.Fatalf("empty store err = %v", err)
	}
}

func TestResolveExisting(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	recent := makeBoard(t, map[string][]string{"todo": nil})
	store := recents.Store{Entries: []recents.Entry{{Name: "Work", Path: recent}}}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}

	got, err := ResolveExisting("work")
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != recent || got.Name != "Work" {
		t.Fatalf("recent resolution = %+v", got)
	}

	direct := makeBoard(t, map[string][]string{"todo": nil})
	got, err = ResolveExisting(direct)
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != direct || got.Name != "" {
		t.Fatalf("filesystem fallback = %+v", got)
	}

	if _, err := ResolveExisting(filepath.Join(t.TempDir(), "missing")); !errors.Is(err, ErrBoardNotFound) {
		t.Fatalf("missing board err = %v, want ErrBoardNotFound", err)
	}
}
