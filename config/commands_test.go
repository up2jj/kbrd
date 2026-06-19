package config

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
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
    id: edit
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
	if len(cmds) != 1 || cmds[0].ID != "edit" || cmds[0].Name != "Edit" {
		t.Fatalf("cmds: %+v", cmds)
	}
}

func TestLoadCommands_FolderOverridesGlobalByID(t *testing.T) {
	globalDir := t.TempDir()
	folder := t.TempDir()
	writeFile(t, filepath.Join(globalDir, GlobalCommandsFile), `
commands:
  - name: Global edit
    id: edit
    description: global
    command: nano {{.filePath}}
  - name: Global only
    id: global-only
    description: global only
    command: echo g
`)
	writeFile(t, filepath.Join(folder, FolderCommandsFile), `
commands:
  - name: Folder edit
    id: edit
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
	byID := map[string]Command{}
	for _, c := range cmds {
		byID[c.ID] = c
	}
	if byID["edit"].Name != "Folder edit" {
		t.Errorf("id edit: got %q want Folder edit", byID["edit"].Name)
	}
	if byID["global-only"].Name != "Global only" {
		t.Errorf("id global-only: got %q want Global only", byID["global-only"].Name)
	}
}

func TestLoadCommands_InvalidEntriesWarnAndSkip(t *testing.T) {
	globalDir := t.TempDir()
	folder := t.TempDir()
	writeFile(t, filepath.Join(folder, FolderCommandsFile), `
commands:
  - name: ""
    id: a
    command: echo a
  - name: No id
    id: ""
    command: echo b
  - name: No command
    id: d
    command: ""
  - name: Good
    id: good
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
	if len(warns) != 3 {
		t.Fatalf("warnings: got %d want 3 (%+v)", len(warns), warns)
	}
}

func TestLoadCommands_DuplicateIDWithinFolder(t *testing.T) {
	folder := t.TempDir()
	writeFile(t, filepath.Join(folder, FolderCommandsFile), `
commands:
  - name: First
    id: x
    command: echo first
  - name: Second
    id: x
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

func TestCommand_Render_Env(t *testing.T) {
	t.Setenv("KBRD_TEST_VAR", "hi")
	c := Command{Template: `{{env "KBRD_TEST_VAR"}}`}
	out, err := c.Render(map[string]string{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if out != "hi" {
		t.Errorf("got %q want %q", out, "hi")
	}
}

func TestCommand_Render_Date(t *testing.T) {
	// Natural-language phrase (EN + PL) resolves and formats; default layout.
	c := Command{Template: `{{date "today"}}`}
	out, err := c.Render(map[string]string{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if want := time.Now().Format("2006-01-02"); out != want {
		t.Errorf("got %q want %q", out, want)
	}

	// Custom layout argument is honored.
	c = Command{Template: `{{date "dziś" "2006/01/02"}}`}
	out, err = c.Render(map[string]string{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if want := time.Now().Format("2006/01/02"); out != want {
		t.Errorf("got %q want %q", out, want)
	}

	// Unparseable phrase fails the render.
	c = Command{Template: `{{date "florble"}}`}
	if _, err := c.Render(map[string]string{}); err == nil {
		t.Error("expected error for unparseable phrase")
	}
}

func TestCommand_Render_EnvUnset(t *testing.T) {
	c := Command{Template: `[{{env "KBRD_DEFINITELY_UNSET"}}]`}
	out, err := c.Render(map[string]string{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if out != "[]" {
		t.Errorf("got %q want %q", out, "[]")
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

func TestCommand_NeedsItem(t *testing.T) {
	tru, fls := true, false
	if !(Command{}).NeedsItem() {
		t.Error("NeedsItem() = false for omitted requiresItem, want true")
	}
	if !(Command{RequiresItem: &tru}).NeedsItem() {
		t.Error("NeedsItem() = false for requiresItem: true")
	}
	if (Command{RequiresItem: &fls}).NeedsItem() {
		t.Error("NeedsItem() = true for requiresItem: false")
	}
}

func TestLoadCommands_RequiresItemParsed(t *testing.T) {
	globalDir := t.TempDir()
	folder := t.TempDir()
	writeFile(t, filepath.Join(globalDir, GlobalCommandsFile), `
commands:
  - name: New card
    id: new-card
    requiresItem: false
    command: touch {{.columnPath}}/new.md
  - name: Edit
    id: edit
    command: nano {{.filePath}}
`)
	cmds, warns, err := loadCommandsFrom(globalDir, folder)
	if err != nil {
		t.Fatalf("loadCommandsFrom: %v", err)
	}
	if len(warns) != 0 {
		t.Fatalf("warnings: %v", warns)
	}
	byID := map[string]Command{}
	for _, c := range cmds {
		byID[c.ID] = c
	}
	if byID["new-card"].NeedsItem() {
		t.Error("new-card NeedsItem() = true, want false (requiresItem: false)")
	}
	if !byID["edit"].NeedsItem() {
		t.Error("edit NeedsItem() = false, want true (omitted defaults true)")
	}
}
