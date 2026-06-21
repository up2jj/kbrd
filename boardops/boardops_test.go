package boardops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestItemMutations(t *testing.T) {
	root := t.TempDir()
	todo := mkdirCol(t, root, "1. TODO")
	done := mkdirCol(t, root, "2. DONE")

	created, err := CreateItem(todo, "task", "hello")
	if err != nil {
		t.Fatalf("CreateItem: %v", err)
	}
	if created.Item.Name != "task" || created.Item.Path == "" {
		t.Fatalf("created result = %+v", created)
	}

	moved, err := MoveItem(todo, done, "task")
	if err != nil {
		t.Fatalf("MoveItem: %v", err)
	}
	if moved.Column.Name != done.Name {
		t.Fatalf("moved column = %+v, want %+v", moved.Column, done)
	}

	renamed, err := RenameItem(done, "task", "renamed")
	if err != nil {
		t.Fatalf("RenameItem: %v", err)
	}
	if renamed.Item.Name != "renamed" {
		t.Fatalf("renamed item = %+v", renamed.Item)
	}

	if _, err := DeleteItem(done, "renamed"); err != nil {
		t.Fatalf("DeleteItem: %v", err)
	}
	if _, err := os.Stat(filepath.Join(done.Path, "renamed.md")); !os.IsNotExist(err) {
		t.Fatalf("deleted item still exists or unexpected err: %v", err)
	}
}

func TestCreateItemFromTemplateAppliesShellPolicy(t *testing.T) {
	root := t.TempDir()
	col := mkdirCol(t, root, "TODO")
	tmplDir := filepath.Join(col.Path, ".kbrd_templates")
	if err := os.MkdirAll(tmplDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmplDir, "bug.md"), []byte(`---
name: Bug
filename: "{{slug .title}}"
steps:
  - fields:
      - {key: title, type: input, required: true}
---
# {{.title}}

{{shell "echo hi"}}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := CreateItemFromTemplate(
		BoardContext{Root: root, Name: "test"},
		col,
		"Bug",
		map[string]any{"title": "Broken Thing"},
		func(body string) string { return body + "\npolicy-applied\n" },
	)
	if err != nil {
		t.Fatalf("CreateItemFromTemplate: %v", err)
	}
	data, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "policy-applied") {
		t.Fatalf("shell policy did not run: %s", data)
	}
}

func TestColumnConfig(t *testing.T) {
	root := t.TempDir()
	col := mkdirCol(t, root, "TODO")
	if err := ColumnConfigSet(col, "owner", "me"); err != nil {
		t.Fatalf("ColumnConfigSet: %v", err)
	}
	got, ok, err := ColumnConfigGet(col, "owner")
	if err != nil || !ok || got != "me" {
		t.Fatalf("ColumnConfigGet = (%v, %v, %v)", got, ok, err)
	}
	all, err := ColumnConfigAll(col)
	if err != nil {
		t.Fatalf("ColumnConfigAll: %v", err)
	}
	if all["owner"] != "me" {
		t.Fatalf("all = %+v", all)
	}
	if err := ColumnConfigDelete(col, "owner"); err != nil {
		t.Fatalf("ColumnConfigDelete: %v", err)
	}
	if _, ok, err := ColumnConfigGet(col, "owner"); err != nil || ok {
		t.Fatalf("after delete ok=%v err=%v", ok, err)
	}
}

func TestFrontmatterAndPin(t *testing.T) {
	root := t.TempDir()
	col := mkdirCol(t, root, "TODO")
	if _, err := CreateItem(col, "task", "body\n"); err != nil {
		t.Fatalf("CreateItem: %v", err)
	}

	if _, err := SetFrontmatter(col, "task", "status", "active"); err != nil {
		t.Fatalf("SetFrontmatter: %v", err)
	}
	if _, err := SetPinned(col, "task", true); err != nil {
		t.Fatalf("SetPinned(true): %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(col.Path, "task.md"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if !strings.Contains(body, "status: active") || !strings.Contains(body, "pinned: true") {
		t.Fatalf("frontmatter not written:\n%s", body)
	}

	if _, err := DeleteFrontmatter(col, "task", "status"); err != nil {
		t.Fatalf("DeleteFrontmatter: %v", err)
	}
	if _, err := SetPinned(col, "task", false); err != nil {
		t.Fatalf("SetPinned(false): %v", err)
	}
	raw, err = os.ReadFile(filepath.Join(col.Path, "task.md"))
	if err != nil {
		t.Fatal(err)
	}
	body = string(raw)
	if strings.Contains(body, "status:") || strings.Contains(body, "pinned:") {
		t.Fatalf("frontmatter not removed:\n%s", body)
	}
}

func mkdirCol(t *testing.T, root, name string) ColumnRef {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return ColumnRef{Name: name, Path: path}
}
