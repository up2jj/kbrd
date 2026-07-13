package fs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseConflictCopy(t *testing.T) {
	tests := []struct {
		name     string
		original string
		label    string
		sequence int
		ok       bool
	}{
		{"task (conflict laptop).md", "task.md", "laptop", 0, true},
		{"task (conflict laptop 2).md", "task.md", "laptop", 2, true},
		{"task (conflict macbook-1).md", "task.md", "macbook-1", 0, true},
		{"task (conflict laptop 1).md", "", "", 0, false},
		{"task (conflict ../laptop).md", "", "", 0, false},
		{"task.md", "", "", 0, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			original, label, sequence, ok := ParseConflictCopy(test.name)
			if original != test.original || label != test.label || sequence != test.sequence || ok != test.ok {
				t.Fatalf("ParseConflictCopy(%q) = %q, %q, %d, %v", test.name, original, label, sequence, ok)
			}
		})
	}
}

func TestListConflictsAndActions(t *testing.T) {
	root := t.TempDir()
	col := filepath.Join(root, "Doing")
	if err := os.MkdirAll(col, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(col, "task.md"), []byte("local\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(col, "task (conflict laptop).md"), []byte("incoming\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	conflicts, err := ListConflicts(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(conflicts) != 1 || conflicts[0].OriginalPath != "Doing/task.md" {
		t.Fatalf("conflicts = %#v", conflicts)
	}
	if err := ReplaceConflict(root, conflicts[0]); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(col, "task.md"))
	if err != nil || string(content) != "incoming\n" {
		t.Fatalf("original content = %q, err=%v", content, err)
	}
	remaining, err := ListConflicts(root)
	if err != nil || len(remaining) != 0 {
		t.Fatalf("remaining conflicts = %#v, err=%v", remaining, err)
	}
}

func TestRenameConflictRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	conflict := Conflict{IncomingPath: "task (conflict laptop).md", OriginalPath: "task.md"}
	if _, err := RenameConflict(root, conflict, "../escape"); err == nil {
		t.Fatal("expected traversal name to be rejected")
	}
}
