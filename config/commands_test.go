package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadCommands_BothMissing(t *testing.T) {
	cmds, warns, err := loadCommandsFrom(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("loadCommandsFrom: %v", err)
	}
	if len(cmds) != 0 {
		t.Errorf("commands: got %d want 0", len(cmds))
	}
	if len(warns) != 0 {
		t.Errorf("warnings: got %v want none", warns)
	}
}

func TestLoadCommands_GlobalOnly(t *testing.T) {
	globalDir := t.TempDir()
	folder := t.TempDir()
	writeFile(t, filepath.Join(globalDir, GlobalCommandsFile), `
commands:
  - name: Edit
    shortcut: e
    description: edit it
    command: nano {{.filePath}}
`)
	cmds, warns, err := loadCommandsFrom(globalDir, folder)
	if err != nil {
		t.Fatalf("loadCommandsFrom: %v", err)
	}
	if len(warns) != 0 {
		t.Fatalf("warnings: %v", warns)
	}
	if len(cmds) != 1 || cmds[0].Shortcut != "e" || cmds[0].Name != "Edit" {
		t.Fatalf("cmds: %+v", cmds)
	}
}

func TestLoadCommands_FolderOverridesGlobalByShortcut(t *testing.T) {
	globalDir := t.TempDir()
	folder := t.TempDir()
	writeFile(t, filepath.Join(globalDir, GlobalCommandsFile), `
commands:
  - name: Global edit
    shortcut: e
    description: global
    command: nano {{.filePath}}
  - name: Global only
    shortcut: g
    description: global only
    command: echo g
`)
	writeFile(t, filepath.Join(folder, FolderCommandsFile), `
commands:
  - name: Folder edit
    shortcut: e
    description: folder
    command: vim {{.filePath}}
`)
	cmds, warns, err := loadCommandsFrom(globalDir, folder)
	if err != nil {
		t.Fatalf("loadCommandsFrom: %v", err)
	}
	if len(warns) != 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	if len(cmds) != 2 {
		t.Fatalf("cmds: got %d want 2 (%+v)", len(cmds), cmds)
	}
	byShortcut := map[string]Command{}
	for _, c := range cmds {
		byShortcut[c.Shortcut] = c
	}
	if byShortcut["e"].Name != "Folder edit" {
		t.Errorf("shortcut e: got %q want Folder edit", byShortcut["e"].Name)
	}
	if byShortcut["g"].Name != "Global only" {
		t.Errorf("shortcut g: got %q want Global only", byShortcut["g"].Name)
	}
}

func TestLoadCommands_InvalidEntriesWarnAndSkip(t *testing.T) {
	globalDir := t.TempDir()
	folder := t.TempDir()
	writeFile(t, filepath.Join(folder, FolderCommandsFile), `
commands:
  - name: ""
    shortcut: a
    command: echo a
  - name: No shortcut
    shortcut: ""
    command: echo b
  - name: Multi-rune
    shortcut: abc
    command: echo c
  - name: No command
    shortcut: d
    command: ""
  - name: Good
    shortcut: g
    description: ok
    command: echo good
`)
	cmds, warns, err := loadCommandsFrom(globalDir, folder)
	if err != nil {
		t.Fatalf("loadCommandsFrom: %v", err)
	}
	if len(cmds) != 1 || cmds[0].Name != "Good" {
		t.Fatalf("cmds: %+v", cmds)
	}
	if len(warns) != 4 {
		t.Fatalf("warnings: got %d want 4 (%+v)", len(warns), warns)
	}
}

func TestLoadCommands_DuplicateShortcutWithinFolder(t *testing.T) {
	folder := t.TempDir()
	writeFile(t, filepath.Join(folder, FolderCommandsFile), `
commands:
  - name: First
    shortcut: x
    command: echo first
  - name: Second
    shortcut: x
    command: echo second
`)
	cmds, warns, err := loadCommandsFrom(t.TempDir(), folder)
	if err != nil {
		t.Fatalf("loadCommandsFrom: %v", err)
	}
	if len(cmds) != 2 {
		t.Fatalf("cmds: got %d want 2 (both kept; duplicates only warn)", len(cmds))
	}
	if len(warns) != 1 {
		t.Fatalf("warnings: got %d want 1 (%+v)", len(warns), warns)
	}
	if !strings.Contains(warns[0].Message, "duplicate") || !strings.Contains(warns[0].Message, "Second") {
		t.Errorf("warning content: %q", warns[0].Message)
	}
}

func TestLoadCommands_MalformedYAML(t *testing.T) {
	folder := t.TempDir()
	writeFile(t, filepath.Join(folder, FolderCommandsFile), "::: not yaml :::\n- nope")
	cmds, warns, err := loadCommandsFrom(t.TempDir(), folder)
	if err != nil {
		t.Fatalf("loadCommandsFrom: %v", err)
	}
	if len(cmds) != 0 {
		t.Errorf("cmds: got %+v want none", cmds)
	}
	if len(warns) == 0 {
		t.Fatal("expected a parse-error warning")
	}
	if !strings.Contains(warns[0].Message, "parse error") {
		t.Errorf("warning: %q", warns[0].Message)
	}
}

func TestCommand_Render_AllVariables(t *testing.T) {
	c := Command{Template: `{{.filePath}}|{{.fileName}}|{{.fileDir}}|{{.boardPath}}|{{.boardName}}|{{.columnPath}}|{{.columnName}}`}
	out, err := c.Render(map[string]string{
		"filePath":   "/b/c/task.md",
		"fileName":   "task",
		"fileDir":    "/b/c",
		"boardPath":  "/b",
		"boardName":  "MyBoard",
		"columnPath": "/b/c",
		"columnName": "c",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	want := "/b/c/task.md|task|/b/c|/b|MyBoard|/b/c|c"
	if out != want {
		t.Errorf("got %q want %q", out, want)
	}
}

func TestCommand_Render_MissingVariableIsError(t *testing.T) {
	c := Command{Template: `hello {{.unknownVar}}`}
	_, err := c.Render(map[string]string{"filePath": "x"})
	if err == nil {
		t.Fatal("expected error for missing variable, got nil")
	}
}

func TestCommand_Render_BadTemplate(t *testing.T) {
	c := Command{Template: `{{.filePath`}
	_, err := c.Render(map[string]string{"filePath": "x"})
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
}
