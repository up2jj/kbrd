package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kbrd/recents"
)

// writeCommands drops a .kbrd_commands.yml into boardPath.
func writeCommands(t *testing.T, boardPath, yaml string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(boardPath, ".kbrd_commands.yml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestListCustomCommands(t *testing.T) {
	boardPath := makeBoardDir(t, "todo")
	seedRecents(t, []recents.Entry{{Path: boardPath, Name: "Work"}})
	writeCommands(t, boardPath, `commands:
  - name: Echo board
    id: echo-board
    description: print board path
    command: echo "{{.boardPath}}"
`)

	_, out, err := listCustomCommands(context.Background(), nil, ListCommandsInput{Board: "Work"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Commands) != 1 || out.Commands[0].ID != "echo-board" {
		t.Fatalf("commands = %+v", out.Commands)
	}
}

func TestListCustomCommandsCanSkipFolderLocalCommands(t *testing.T) {
	boardPath := makeBoardDir(t, "todo")
	seedRecents(t, []recents.Entry{{Path: boardPath, Name: "Work"}})
	writeCommands(t, boardPath, `commands:
  - name: Local
    id: local
    command: echo local
`)

	_, out, err := listCustomCommandsWithPolicy(context.Background(), nil, ListCommandsInput{Board: "Work"}, Policy{AllowFolderCommands: false})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Commands) != 0 {
		t.Fatalf("commands = %+v, want no folder-local commands", out.Commands)
	}
}

func TestRunCustomCommandRequiresPolicy(t *testing.T) {
	boardPath := makeBoardDir(t, "todo")
	seedRecents(t, []recents.Entry{{Path: boardPath, Name: "Work"}})
	writeCommands(t, boardPath, `commands:
  - name: Local
    id: local
    command: echo local
`)

	if _, _, err := runCustomCommandWithPolicy(context.Background(), nil, RunCommandInput{Board: "Work", Command: "local"}, Policy{}); err == nil {
		t.Fatal("expected command execution to require explicit policy")
	}
	if _, _, err := runCustomCommandWithPolicy(context.Background(), nil, RunCommandInput{Board: "Work", Command: "local"}, Policy{AllowCommands: true, AllowFolderCommands: false}); err == nil {
		t.Fatal("expected folder-local command to be unavailable when folder commands are disabled")
	}
}

func TestRunCustomCommand(t *testing.T) {
	boardPath := makeBoardDir(t, "1. todo")
	os.WriteFile(filepath.Join(boardPath, "1. todo", "card.md"), []byte("hi"), 0o644)
	seedRecents(t, []recents.Entry{{Path: boardPath, Name: "Work"}})
	writeCommands(t, boardPath, `commands:
  - name: Board echo
    id: board-echo
    description: board scoped
    command: echo "board={{.boardName}}"
  - name: File echo
    id: file-echo
    description: item scoped
    command: echo "file={{.fileName}}"; cat "{{.filePath}}"
  - name: Fails
    id: fails
    description: nonzero exit
    command: exit 3
`)
	ctx := context.Background()

	// Board-scoped command, no item needed.
	_, out, err := runCustomCommand(ctx, nil, RunCommandInput{Board: "Work", Command: "board-echo"})
	if err != nil {
		t.Fatalf("board-echo: %v", err)
	}
	if out.ExitCode != 0 || !strings.Contains(out.Output, "board=Work") {
		t.Fatalf("board-echo out = %+v", out)
	}

	// Item-scoped command with folder + item.
	_, out, err = runCustomCommand(ctx, nil, RunCommandInput{Board: "Work", Command: "file-echo", Folder: "1. todo", Item: "card"})
	if err != nil {
		t.Fatalf("file-echo: %v", err)
	}
	if !strings.Contains(out.Output, "file=card") || !strings.Contains(out.Output, "hi") {
		t.Fatalf("file-echo out = %q", out.Output)
	}

	// Item-scoped command without an item -> render error (missingkey).
	if _, _, err := runCustomCommand(ctx, nil, RunCommandInput{Board: "Work", Command: "file-echo"}); err == nil {
		t.Fatal("expected missing-variable error")
	}

	// Non-zero exit is reported, not an error.
	_, out, err = runCustomCommand(ctx, nil, RunCommandInput{Board: "Work", Command: "fails"})
	if err != nil {
		t.Fatalf("fails returned tool error: %v", err)
	}
	if out.ExitCode != 3 {
		t.Fatalf("exit code = %d, want 3", out.ExitCode)
	}

	// Unknown command id.
	if _, _, err := runCustomCommand(ctx, nil, RunCommandInput{Board: "Work", Command: "nope"}); err == nil {
		t.Fatal("expected unknown-command error")
	}

	// Unknown board.
	if _, _, err := runCustomCommand(ctx, nil, RunCommandInput{Board: "Ghost", Command: "board-echo"}); err == nil {
		t.Fatal("expected board-not-found error")
	}
}

func TestRunCustomCommandTruncatesOutput(t *testing.T) {
	out := limitCommandOutput(strings.Repeat("x", maxCommandOutputBytes+1))
	if len(out) <= maxCommandOutputBytes || !strings.Contains(out, "output truncated") {
		t.Fatalf("output was not truncated with marker: len=%d", len(out))
	}
}
