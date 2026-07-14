package clipboardring

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStorePersistsAndKeepsPinnedEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "clipboard.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < MaxEntries+3; i++ {
		if err := store.Add(Entry{ID: string(rune('a' + i)), Time: time.Now(), Text: "entry"}); err != nil {
			t.Fatal(err)
		}
	}
	entries := store.Entries()
	if len(entries) != MaxEntries {
		t.Fatalf("entries = %d, want %d", len(entries), MaxEntries)
	}
	if _, err := store.TogglePinned(entries[len(entries)-1].ID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := store.Add(Entry{Text: "new"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Entries()) != MaxEntries {
		t.Fatalf("loaded entries = %d, want %d", len(loaded.Entries()), MaxEntries)
	}
	foundPinned := false
	for _, entry := range loaded.Entries() {
		if entry.ID == entries[len(entries)-1].ID && entry.Pinned {
			foundPinned = true
		}
	}
	if !foundPinned {
		t.Fatal("pinned entry was pruned")
	}
}

func TestDetectKind(t *testing.T) {
	for _, test := range []struct {
		name string
		text string
		want Kind
	}{
		{"checklist", "- [ ] ship it\n- [x] test it", KindChecklist},
		{"frontmatter", "---\npriority: high\n---\n", KindFrontmatter},
		{"code", "```go\nfmt.Println()\n```", KindCodeBlock},
		{"link", "https://example.com", KindLink},
		{"markdown", "# Heading", KindMarkdown},
		{"text", "plain words", KindText},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := DetectKind(test.text); got != test.want {
				t.Fatalf("DetectKind = %q, want %q", got, test.want)
			}
		})
	}
}
