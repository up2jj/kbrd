package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestNewItem(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	t.Run("plain file", func(t *testing.T) {
		path := writeFile(t, dir, "task.md", "first line\nsecond line\n")
		item, err := NewItem(path, ItemOptions{PreviewLines: 3})
		if err != nil {
			t.Fatalf("NewItem: %v", err)
		}
		if item.Name != "task" {
			t.Errorf("Name = %q, want %q", item.Name, "task")
		}
		if item.Pinned {
			t.Errorf("Pinned = true, want false")
		}
		if item.FullPath != path {
			t.Errorf("FullPath = %q, want %q", item.FullPath, path)
		}
		if item.Size != int64(len("first line\nsecond line\n")) {
			t.Errorf("Size = %d, want %d", item.Size, len("first line\nsecond line\n"))
		}
		if len(item.Preview) != 2 || item.Preview[0] != "first line" || item.Preview[1] != "second line" {
			t.Errorf("Preview = %v, want [first line second line]", item.Preview)
		}
	})

	t.Run("pinned via frontmatter", func(t *testing.T) {
		path := writeFile(t, dir, "urgent.md", "---\npinned: true\n---\ndo me first")
		item, err := NewItem(path, ItemOptions{PreviewLines: 3})
		if err != nil {
			t.Fatalf("NewItem: %v", err)
		}
		if item.Name != "urgent" {
			t.Errorf("Name = %q, want %q", item.Name, "urgent")
		}
		if !item.Pinned {
			t.Errorf("Pinned = false, want true")
		}
	})

	t.Run("pinned via frontmatter yes", func(t *testing.T) {
		path := writeFile(t, dir, "soon.md", "---\npinned: yes\n---\nlater")
		item, err := NewItem(path, ItemOptions{PreviewLines: 3})
		if err != nil {
			t.Fatalf("NewItem: %v", err)
		}
		if !item.Pinned {
			t.Errorf("Pinned = false, want true for `pinned: yes`")
		}
	})

	t.Run("empty file", func(t *testing.T) {
		path := writeFile(t, dir, "empty.md", "")
		item, err := NewItem(path, ItemOptions{PreviewLines: 3})
		if err != nil {
			t.Fatalf("NewItem: %v", err)
		}
		if len(item.Preview) != 0 {
			t.Errorf("Preview = %v, want empty", item.Preview)
		}
	})

	t.Run("preview capped at 3 non-empty lines from first 3 lines", func(t *testing.T) {
		// Loop reads i=0..2 only, skipping empty entries within that window.
		path := writeFile(t, dir, "many.md", "one\ntwo\nthree\nfour\nfive\n")
		item, err := NewItem(path, ItemOptions{PreviewLines: 3})
		if err != nil {
			t.Fatalf("NewItem: %v", err)
		}
		if len(item.Preview) != 3 {
			t.Fatalf("Preview length = %d, want 3", len(item.Preview))
		}
		want := []string{"one", "two", "three"}
		for i := range want {
			if item.Preview[i] != want[i] {
				t.Errorf("Preview[%d] = %q, want %q", i, item.Preview[i], want[i])
			}
		}
	})

	t.Run("blank lines within first 3 are skipped", func(t *testing.T) {
		path := writeFile(t, dir, "blanks.md", "\n\nthird\nfourth\n")
		item, err := NewItem(path, ItemOptions{PreviewLines: 3})
		if err != nil {
			t.Fatalf("NewItem: %v", err)
		}
		if len(item.Preview) != 1 || item.Preview[0] != "third" {
			t.Errorf("Preview = %v, want [third]", item.Preview)
		}
	})

	t.Run("missing file returns error", func(t *testing.T) {
		_, err := NewItem(filepath.Join(dir, "nope.md"), ItemOptions{PreviewLines: 3})
		if err == nil {
			t.Fatal("expected error for missing file")
		}
	})

	t.Run("large trailing body does not affect preview", func(t *testing.T) {
		// Bounded read: only the first previewLines lines are scanned, so a huge
		// tail must neither change Preview nor cause an error. Size still reflects
		// the full file (from Stat).
		body := strings.Repeat("filler line\n", 100000)
		content := "head one\nhead two\n" + body
		path := writeFile(t, dir, "big.md", content)
		item, err := NewItem(path, ItemOptions{PreviewLines: 2})
		if err != nil {
			t.Fatalf("NewItem: %v", err)
		}
		if len(item.Preview) != 2 || item.Preview[0] != "head one" || item.Preview[1] != "head two" {
			t.Errorf("Preview = %v, want [head one head two]", item.Preview)
		}
		if item.Size != int64(len(content)) {
			t.Errorf("Size = %d, want %d (full file)", item.Size, len(content))
		}
	})
}

func TestNewItem_Title(t *testing.T) {
	dir := t.TempDir()

	t.Run("heading off uses filename", func(t *testing.T) {
		path := writeFile(t, dir, "notes.md", "# Real Title\nbody\n")
		item, err := NewItem(path, ItemOptions{PreviewLines: 3})
		if err != nil {
			t.Fatalf("NewItem: %v", err)
		}
		if item.Title != "notes" {
			t.Errorf("Title = %q, want %q", item.Title, "notes")
		}
		if len(item.Preview) == 0 || item.Preview[0] != "# Real Title" {
			t.Errorf("Preview = %v, want heading line retained", item.Preview)
		}
	})

	t.Run("heading on uses first H1 and drops it from preview", func(t *testing.T) {
		path := writeFile(t, dir, "topic.md", "# Real Title\nbody one\nbody two\n")
		item, err := NewItem(path, ItemOptions{PreviewLines: 3, TitleFromHeading: true})
		if err != nil {
			t.Fatalf("NewItem: %v", err)
		}
		if item.Title != "Real Title" {
			t.Errorf("Title = %q, want %q", item.Title, "Real Title")
		}
		want := []string{"body one", "body two"}
		if len(item.Preview) != len(want) || item.Preview[0] != want[0] || item.Preview[1] != want[1] {
			t.Errorf("Preview = %v, want %v", item.Preview, want)
		}
	})

	t.Run("heading on falls back to filename without H1", func(t *testing.T) {
		path := writeFile(t, dir, "plain.md", "just text\nmore\n")
		item, err := NewItem(path, ItemOptions{PreviewLines: 3, TitleFromHeading: true})
		if err != nil {
			t.Fatalf("NewItem: %v", err)
		}
		if item.Title != "plain" {
			t.Errorf("Title = %q, want %q", item.Title, "plain")
		}
		if len(item.Preview) == 0 || item.Preview[0] != "just text" {
			t.Errorf("Preview = %v, want body retained", item.Preview)
		}
	})

	t.Run("h2 is not treated as a title", func(t *testing.T) {
		path := writeFile(t, dir, "sub.md", "## Section\nbody\n")
		item, err := NewItem(path, ItemOptions{PreviewLines: 3, TitleFromHeading: true})
		if err != nil {
			t.Fatalf("NewItem: %v", err)
		}
		if item.Title != "sub" {
			t.Errorf("Title = %q, want %q (## is not H1)", item.Title, "sub")
		}
	})

	t.Run("heading after YAML frontmatter", func(t *testing.T) {
		path := writeFile(t, dir, "fm.md", "---\ntags: [a, b]\n---\n# From Heading\nbody\n")
		item, err := NewItem(path, ItemOptions{PreviewLines: 3, TitleFromHeading: true})
		if err != nil {
			t.Fatalf("NewItem: %v", err)
		}
		if item.Title != "From Heading" {
			t.Errorf("Title = %q, want %q", item.Title, "From Heading")
		}
		if len(item.Preview) == 0 || item.Preview[0] != "body" {
			t.Errorf("Preview = %v, want [body]", item.Preview)
		}
	})
}

func TestNewItem_Frontmatter(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	const fm = "---\naccent: \"#e06c75\"\nicon: \"F\"\nmeta: due soon\ntags: [urgent, backend]\ncustom: hello\n---\n"

	t.Run("fields populated, heading mode off", func(t *testing.T) {
		path := writeFile(t, dir, "meta_off.md", fm+"body one\nbody two\n")
		item, err := NewItem(path, ItemOptions{PreviewLines: 3})
		if err != nil {
			t.Fatalf("NewItem: %v", err)
		}
		if item.Accent != "#e06c75" || item.Icon != "F" || item.Meta != "due soon" {
			t.Errorf("Accent/Icon/Meta = %q/%q/%q, want #e06c75/F/due soon", item.Accent, item.Icon, item.Meta)
		}
		if len(item.Tags) != 2 || item.Tags[0] != "urgent" || item.Tags[1] != "backend" {
			t.Errorf("Tags = %v, want [urgent backend]", item.Tags)
		}
		if item.Data["custom"] != "hello" {
			t.Errorf("Data[custom] = %v, want hello", item.Data["custom"])
		}
		// Behavior guard: frontmatter never leaks into the preview, even with
		// TitleFromHeading off.
		if len(item.Preview) != 2 || item.Preview[0] != "body one" {
			t.Errorf("Preview = %v, want [body one, body two]", item.Preview)
		}
	})

	t.Run("fields populated, heading mode on", func(t *testing.T) {
		path := writeFile(t, dir, "meta_on.md", fm+"# Heading\nbody\n")
		item, err := NewItem(path, ItemOptions{PreviewLines: 3, TitleFromHeading: true})
		if err != nil {
			t.Fatalf("NewItem: %v", err)
		}
		if item.Title != "Heading" {
			t.Errorf("Title = %q, want Heading", item.Title)
		}
		if item.Accent != "#e06c75" || len(item.Tags) != 2 {
			t.Errorf("Accent/Tags = %q/%v, want populated", item.Accent, item.Tags)
		}
		if len(item.Preview) != 1 || item.Preview[0] != "body" {
			t.Errorf("Preview = %v, want [body]", item.Preview)
		}
	})

	t.Run("malformed YAML still loads the card", func(t *testing.T) {
		path := writeFile(t, dir, "bad.md", "---\ntags: [unclosed\n---\nbody\n")
		item, err := NewItem(path, ItemOptions{PreviewLines: 3})
		if err != nil {
			t.Fatalf("NewItem: %v", err)
		}
		if item.Accent != "" || item.Tags != nil || item.Data != nil {
			t.Errorf("metadata = %q/%v/%v, want zero on malformed YAML", item.Accent, item.Tags, item.Data)
		}
		if !item.BadFM {
			t.Error("BadFM = false, want true for malformed frontmatter")
		}
		if len(item.Preview) != 1 || item.Preview[0] != "body" {
			t.Errorf("Preview = %v, want [body]", item.Preview)
		}
	})

	t.Run("valid frontmatter is not flagged", func(t *testing.T) {
		path := writeFile(t, dir, "good.md", fm+"body\n")
		item, err := NewItem(path, ItemOptions{PreviewLines: 3})
		if err != nil {
			t.Fatalf("NewItem: %v", err)
		}
		if item.BadFM {
			t.Error("BadFM = true, want false for valid frontmatter")
		}
	})

	t.Run("refresh clears BadFM when frontmatter fixed", func(t *testing.T) {
		path := writeFile(t, dir, "fixed.md", "---\ntags: [unclosed\n---\nbody\n")
		item, err := NewItem(path, ItemOptions{PreviewLines: 3})
		if err != nil {
			t.Fatalf("NewItem: %v", err)
		}
		if !item.BadFM {
			t.Fatal("precondition: BadFM should be set")
		}
		writeFile(t, dir, "fixed.md", "---\ntags: [ok]\n---\nbody\n")
		if err := item.Refresh(ItemOptions{PreviewLines: 3}); err != nil {
			t.Fatalf("Refresh: %v", err)
		}
		if item.BadFM {
			t.Error("BadFM = true after fix, want false")
		}
		if len(item.Tags) != 1 || item.Tags[0] != "ok" {
			t.Errorf("Tags = %v, want [ok]", item.Tags)
		}
	})

	t.Run("refresh clears fields when frontmatter removed", func(t *testing.T) {
		path := writeFile(t, dir, "clear.md", fm+"body\n")
		item, err := NewItem(path, ItemOptions{PreviewLines: 3})
		if err != nil {
			t.Fatalf("NewItem: %v", err)
		}
		if item.Accent == "" {
			t.Fatal("precondition: Accent should be set")
		}
		writeFile(t, dir, "clear.md", "body only\n")
		if err := item.Refresh(ItemOptions{PreviewLines: 3}); err != nil {
			t.Fatalf("Refresh: %v", err)
		}
		if item.Accent != "" || item.Icon != "" || item.Meta != "" || item.Tags != nil || item.Data != nil {
			t.Errorf("metadata after refresh = %q/%q/%q/%v/%v, want cleared",
				item.Accent, item.Icon, item.Meta, item.Tags, item.Data)
		}
	})
}

func TestItem_FilterValue(t *testing.T) {
	t.Parallel()
	it := Item{Name: "hello"}
	if got := it.FilterValue(); got != "hello" {
		t.Errorf("FilterValue = %q, want %q", got, "hello")
	}

	t.Run("tags join the haystack", func(t *testing.T) {
		it := Item{Name: "hello", Tags: []string{"urgent", "backend"}}
		if got := it.FilterValue(); got != "hello urgent backend" {
			t.Errorf("FilterValue = %q, want %q", got, "hello urgent backend")
		}
	})
}

func TestItem_HumanSize(t *testing.T) {
	t.Parallel()
	cases := []struct {
		size int64
		want string
	}{
		{0, "0 B"},
		{1, "1 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{2048, "2.0 KB"},
		{10240, "10.0 KB"},
	}
	for _, c := range cases {
		it := Item{Size: c.size}
		if got := it.HumanSize(); got != c.want {
			t.Errorf("HumanSize(%d) = %q, want %q", c.size, got, c.want)
		}
	}
}

func TestItem_Refresh(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := writeFile(t, dir, "r.md", "old\n")
	item, err := NewItem(path, ItemOptions{PreviewLines: 3})
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	originalMod := item.Modified

	// Sleep briefly so mtime resolution updates on filesystems with coarse granularity.
	time.Sleep(10 * time.Millisecond)
	newContent := "fresh line one\nfresh line two\n"
	if err := os.WriteFile(path, []byte(newContent), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := item.Refresh(ItemOptions{PreviewLines: 3}); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if item.Size != int64(len(newContent)) {
		t.Errorf("Size = %d, want %d", item.Size, len(newContent))
	}
	if !item.Modified.After(originalMod) && !item.Modified.Equal(originalMod) {
		t.Errorf("Modified = %v not updated from %v", item.Modified, originalMod)
	}
	if len(item.Preview) != 2 || item.Preview[0] != "fresh line one" {
		t.Errorf("Preview = %v, want updated content", item.Preview)
	}
}

func TestItem_Refresh_MissingFile(t *testing.T) {
	t.Parallel()
	it := Item{FullPath: filepath.Join(t.TempDir(), "ghost.md")}
	if err := it.Refresh(ItemOptions{PreviewLines: 3}); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestSortItems(t *testing.T) {
	t.Parallel()

	t.Run("all unpinned alphabetised", func(t *testing.T) {
		in := []Item{{Name: "c"}, {Name: "a"}, {Name: "b"}}
		got := SortItems(in)
		want := []string{"a", "b", "c"}
		for i, w := range want {
			if got[i].Name != w {
				t.Errorf("got[%d] = %q, want %q", i, got[i].Name, w)
			}
		}
	})

	t.Run("pinned kept in original order, unpinned sorted", func(t *testing.T) {
		in := []Item{
			{Name: "z", Pinned: false},
			{Name: "p_b", Pinned: true},
			{Name: "a", Pinned: false},
			{Name: "p_a", Pinned: true},
		}
		got := SortItems(in)
		// pinned in original order: p_b, p_a
		if got[0].Name != "p_b" || got[1].Name != "p_a" {
			t.Errorf("pinned prefix = %q, %q; want p_b, p_a", got[0].Name, got[1].Name)
		}
		// unpinned alphabetised
		if got[2].Name != "a" || got[3].Name != "z" {
			t.Errorf("unpinned suffix = %q, %q; want a, z", got[2].Name, got[3].Name)
		}
	})

	t.Run("empty", func(t *testing.T) {
		got := SortItems(nil)
		if len(got) != 0 {
			t.Errorf("got %d items, want 0", len(got))
		}
	})

	t.Run("does not mutate input", func(t *testing.T) {
		in := []Item{{Name: "b"}, {Name: "a"}}
		_ = SortItems(in)
		if in[0].Name != "b" || in[1].Name != "a" {
			t.Errorf("input mutated: %v", in)
		}
	})
}

func TestSortByName(t *testing.T) {
	t.Parallel()
	in := []Item{{Name: "delta"}, {Name: "alpha"}, {Name: "charlie"}, {Name: "bravo"}}
	got := sortByName(in)
	want := []string{"alpha", "bravo", "charlie", "delta"}
	for i, w := range want {
		if got[i].Name != w {
			t.Errorf("got[%d] = %q, want %q", i, got[i].Name, w)
		}
	}
}

func TestTimeAgo(t *testing.T) {
	t.Parallel()
	now := time.Now()
	cases := []struct {
		name string
		t    time.Time
		want string
	}{
		{"just now", now.Add(-10 * time.Second), "just now"},
		{"minutes", now.Add(-5 * time.Minute), "5m ago"},
		{"hours", now.Add(-2 * time.Hour), "2h ago"},
		{"days", now.Add(-3 * 24 * time.Hour), "3d ago"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := timeAgo(c.t); got != c.want {
				t.Errorf("timeAgo = %q, want %q", got, c.want)
			}
		})
	}

	t.Run("month-old current year", func(t *testing.T) {
		// 30 days ago — formatter "Jan 2", same year.
		past := now.AddDate(0, 0, -30)
		if past.Year() != now.Year() {
			t.Skip("rolled over a year boundary")
		}
		want := past.Format("Jan 2")
		if got := timeAgo(past); got != want {
			t.Errorf("timeAgo = %q, want %q", got, want)
		}
	})

	t.Run("previous year includes year", func(t *testing.T) {
		past := now.AddDate(-1, 0, -1)
		want := past.Format("Jan 2 2006")
		if got := timeAgo(past); got != want {
			t.Errorf("timeAgo = %q, want %q", got, want)
		}
	})
}
