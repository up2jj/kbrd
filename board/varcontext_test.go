package board

import (
	"path/filepath"
	"testing"
)

func TestVarContext_Vars_FullSet(t *testing.T) {
	t.Parallel()
	vc := VarContext{
		BoardPath:  "/b",
		BoardName:  "Demo",
		ColumnPath: "/b/1. TODO",
		ColumnName: "1. TODO",
		FilePath:   "/b/1. TODO/task.md",
		FileName:   "task",
	}
	got := vc.Vars()
	want := map[string]string{
		"boardPath":  "/b",
		"boardName":  "Demo",
		"columnPath": "/b/1. TODO",
		"columnName": "1. TODO",
		"filePath":   "/b/1. TODO/task.md",
		"fileName":   "task",
		"fileDir":    filepath.Dir("/b/1. TODO/task.md"),
	}
	if len(got) != len(want) {
		t.Fatalf("Vars() has %d keys, want %d: %v", len(got), len(want), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("Vars()[%q] = %q, want %q", k, got[k], v)
		}
	}
}

func TestVarContext_Vars_OmitsEmptyGroups(t *testing.T) {
	t.Parallel()
	// Board only: no column, no file.
	board := VarContext{BoardPath: "/b", BoardName: "Demo"}.Vars()
	for _, k := range []string{"columnPath", "columnName", "filePath", "fileName", "fileDir"} {
		if _, ok := board[k]; ok {
			t.Errorf("board-only Vars() should omit %q, got %q", k, board[k])
		}
	}

	// Column but no file: file group omitted, column group present.
	col := VarContext{BoardPath: "/b", BoardName: "Demo", ColumnPath: "/b/c", ColumnName: "c"}.Vars()
	if col["columnPath"] != "/b/c" {
		t.Errorf("columnPath = %q, want /b/c", col["columnPath"])
	}
	for _, k := range []string{"filePath", "fileName", "fileDir"} {
		if _, ok := col[k]; ok {
			t.Errorf("column-only Vars() should omit %q", k)
		}
	}
}

func TestVarContext_Vars_DerivesFileDir(t *testing.T) {
	t.Parallel()
	vc := VarContext{BoardPath: "/b", FilePath: "/b/col/deep/task.md", FileName: "task"}
	got := vc.Vars()["fileDir"]
	if want := filepath.Dir("/b/col/deep/task.md"); got != want {
		t.Errorf("fileDir = %q, want %q", got, want)
	}
}
