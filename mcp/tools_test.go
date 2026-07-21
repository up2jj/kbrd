package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"kbrd/recents"
)

// seedRecents points recents at a temp config dir and records the given boards.
// Returns nothing; the temp HOME/XDG dirs persist for the test via t.Setenv.
func seedRecents(t *testing.T, entries []recents.Entry) {
	t.Helper()
	cfg := t.TempDir()
	t.Setenv("HOME", cfg)            // darwin: os.UserConfigDir -> $HOME/Library/...
	t.Setenv("XDG_CONFIG_HOME", cfg) // linux
	store := recents.Store{Entries: entries}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
}

func makeBoardDir(t *testing.T, cols ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, c := range cols {
		if err := os.MkdirAll(filepath.Join(root, c), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestAddFileToBoard(t *testing.T) {
	boardPath := makeBoardDir(t, "1. todo", "2. doing")
	seedRecents(t, []recents.Entry{{Path: boardPath, Name: "Work"}})

	ctx := context.Background()

	// Default folder (first column), with content.
	res, out, err := addFileToBoard(ctx, nil, AddFileInput{Board: "Work", Name: "buy milk", Content: "2L"})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if filepath.Base(out.Folder) != "1. todo" {
		t.Fatalf("folder = %q, want '1. todo'", out.Folder)
	}
	if res == nil || len(res.Content) == 0 {
		t.Fatal("expected text content")
	}
	data, _ := os.ReadFile(out.Path)
	if string(data) != "2L\n" {
		t.Fatalf("content = %q", data)
	}

	// Named folder.
	_, out, err = addFileToBoard(ctx, nil, AddFileInput{Board: "Work", Name: "fix bug", Folder: "2. DOING"})
	if err != nil {
		t.Fatalf("named folder add: %v", err)
	}
	if filepath.Base(out.Folder) != "2. doing" {
		t.Fatalf("folder = %q", out.Folder)
	}

	// Duplicate -> error.
	if _, _, err := addFileToBoard(ctx, nil, AddFileInput{Board: "Work", Name: "buy milk"}); err == nil {
		t.Fatal("expected duplicate error")
	}

	// Unknown board -> error.
	if _, _, err := addFileToBoard(ctx, nil, AddFileInput{Board: "Nope", Name: "x"}); err == nil {
		t.Fatal("expected board-not-found error")
	}

	// Missing folder without create_folder -> error.
	if _, _, err := addFileToBoard(ctx, nil, AddFileInput{Board: "Work", Name: "x", Folder: "ghost"}); err == nil {
		t.Fatal("expected folder-not-found error")
	}

	// Missing folder with create_folder -> success.
	_, out, err = addFileToBoard(ctx, nil, AddFileInput{Board: "Work", Name: "x", Folder: "3. done", CreateFolder: true})
	if err != nil {
		t.Fatalf("create_folder add: %v", err)
	}
	if fi, err := os.Stat(filepath.Join(boardPath, "3. done")); err != nil || !fi.IsDir() {
		t.Fatalf("folder not created: %v", err)
	}
}

func TestListTools(t *testing.T) {
	b1 := makeBoardDir(t, "todo")
	os.WriteFile(filepath.Join(b1, "todo", "a.md"), nil, 0o644)
	os.WriteFile(filepath.Join(b1, "todo", "b.md"), nil, 0o644)
	seedRecents(t, []recents.Entry{{Path: b1, Name: "Work", Pinned: true}})

	ctx := context.Background()

	_, boards, err := listBoards(ctx, nil, struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if len(boards.Boards) != 1 || boards.Boards[0].Name != "Work" || !boards.Boards[0].Pinned {
		t.Fatalf("boards = %+v", boards.Boards)
	}

	_, folders, err := listFolders(ctx, nil, ListFoldersInput{Board: "Work"})
	if err != nil {
		t.Fatal(err)
	}
	if len(folders.Folders) != 1 || folders.Folders[0] != "todo" {
		t.Fatalf("folders = %+v", folders.Folders)
	}

	_, files, err := listFiles(ctx, nil, ListFilesInput{Board: "Work"})
	if err != nil {
		t.Fatal(err)
	}
	if len(files.Files) != 2 || files.Files[0] != "a" || files.Files[1] != "b" {
		t.Fatalf("files = %+v", files.Files)
	}

	_, snapshot, err := showBoard(ctx, nil, ShowBoardInput{Board: "Work"})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.SchemaVersion != resourceSchemaV1 || snapshot.Board != "Work" || len(snapshot.Columns) != 1 {
		t.Fatalf("board snapshot = %+v", snapshot)
	}
	if got := snapshot.Columns[0].Cards; len(got) != 2 || got[0].Name != "a" || got[0].URI != "" {
		t.Fatalf("board cards = %+v", got)
	}
}

// TestOutputMarshals guards that the structured output types serialize to JSON
// objects (required by the MCP spec for structured content).
func TestOutputMarshals(t *testing.T) {
	for _, v := range []any{
		AddFileOutput{Path: "/p", Board: "B", Folder: "F"},
		ListBoardsOutput{},
		ListFoldersOutput{},
		ListFilesOutput{},
		ShowBoardOutput{},
		CardOutput{},
		SearchCardsOutput{},
		MutationOutput{},
		CreateColumnOutput{},
	} {
		if _, err := json.Marshal(v); err != nil {
			t.Fatalf("marshal %T: %v", v, err)
		}
	}
}
