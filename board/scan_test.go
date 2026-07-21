package board

import (
	"errors"
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

func TestScanItemsDoesNotFollowCardSymlinkOutsideColumn(t *testing.T) {
	root := t.TempDir()
	column := filepath.Join(root, "Inbox")
	if err := os.Mkdir(column, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "secret.md")
	if err := os.WriteFile(outside, []byte("secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(column, "linked.md")); err != nil {
		t.Fatal(err)
	}

	_, err := ScanItems(root, nil)
	if err == nil {
		t.Fatal("ScanItems followed a card symlink outside its column")
	}
	if !errors.Is(err, os.ErrPermission) {
		t.Logf("ScanItems rejected symlink with %v", err)
	}
}
