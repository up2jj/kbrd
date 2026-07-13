package board

import (
	"os"
	"path/filepath"
	"testing"
)

func TestItemsSkipsConflictCopies(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"task.md", "task (conflict laptop).md", "notes.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	items, err := Items(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0] != "notes" || items[1] != "task" {
		t.Fatalf("items = %#v", items)
	}
}
