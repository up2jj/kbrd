package board

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanItemsFiltersParsedFrontmatter(t *testing.T) {
	root := t.TempDir()
	column := filepath.Join(root, "Inbox")
	if err := os.Mkdir(column, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(column, "due.md"), []byte("---\ndue: 2026-07-15\n---\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(column, "plain.md"), []byte("plain\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	items, err := ScanItems(root, func(item ScannedItem) bool {
		return item.Frontmatter.Data["due"] != nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Name != "due" || items[0].Body != "\nbody\n" {
		t.Fatalf("items = %+v", items)
	}
}
