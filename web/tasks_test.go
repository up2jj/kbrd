package web

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"kbrd/board"
	"kbrd/fs"
)

// TestTaskCreateItemCommits verifies a task-driven create lands on disk and is
// committed by the Syncer (working tree clean afterwards), and that a repeat
// create of the same name is rejected — the idempotency seatbelt that lets a
// time-bucketed task run twice without producing duplicates.
func TestTaskCreateItemCommits(t *testing.T) {
	if !fs.GitAvailable() {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "todo"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "init", "-b", "main")
	if _, err := fs.GitCommitAll(dir, "initial board", "Tester", "t@example.com"); err != nil {
		t.Fatal(err)
	}

	api := boardTaskAPI{root: dir, boardName: "demo", sync: NewSyncer(dir, "Tester", "t@example.com")}
	if api.sync == nil {
		t.Fatal("syncer not created for git-backed board")
	}

	if err := api.CreateItem("todo", "standup-2026-06-08"); err != nil {
		t.Fatalf("CreateItem: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "todo", "standup-2026-06-08.md")); err != nil {
		t.Fatalf("card not written: %v", err)
	}
	if !fs.GitWorkingTreeClean(dir) {
		t.Fatal("create was not committed: working tree is dirty")
	}

	// Second create of the same name must fail (no silent overwrite), so a
	// double-fired task degrades to a harmless error rather than a duplicate.
	if err := api.CreateItem("todo", "standup-2026-06-08"); !errors.Is(err, os.ErrExist) {
		t.Fatalf("expected os.ErrExist on duplicate create, got %v", err)
	}
}

// TestTaskCreateItemNoSync confirms the API works without git (nil Syncer): the
// file is still created and commit is a no-op rather than a nil-pointer panic.
func TestTaskCreateItemNoSync(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "todo"), 0o755); err != nil {
		t.Fatal(err)
	}
	api := boardTaskAPI{root: dir, boardName: "demo", sync: nil}
	if err := api.CreateItem("todo", "card"); err != nil {
		t.Fatalf("CreateItem without sync: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "todo", "card.md")); err != nil {
		t.Fatalf("card not written: %v", err)
	}
}

// TestTaskCreateItemUnknownColumn ensures a task targeting a missing column gets
// a clear error instead of silently creating one.
func TestTaskCreateItemUnknownColumn(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "todo"), 0o755); err != nil {
		t.Fatal(err)
	}
	api := boardTaskAPI{root: dir, sync: nil}
	if err := api.CreateItem("nope", "card"); !errors.Is(err, board.ErrFolderNotFound) {
		t.Fatalf("expected ErrFolderNotFound, got %v", err)
	}
}
