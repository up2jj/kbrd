package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadHooks_BothMissing(t *testing.T) {
	hooks, warns, err := loadHooksFrom(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("loadHooksFrom: %v", err)
	}
	if len(hooks) != 0 {
		t.Errorf("hooks: got %d want 0", len(hooks))
	}
	if len(warns) != 0 {
		t.Errorf("warnings: got %v want none", warns)
	}
}

func TestLoadHooks_GlobalOnly(t *testing.T) {
	globalDir := t.TempDir()
	writeFile(t, filepath.Join(globalDir, GlobalHooksFile), `
hooks:
  - name: Stage
    id: stage
    event: item_moved
    command: git add {{.toColumn}}/{{.fileName}}.md
`)
	hooks, warns, err := loadHooksFrom(globalDir, t.TempDir())
	if err != nil {
		t.Fatalf("loadHooksFrom: %v", err)
	}
	if len(warns) != 0 {
		t.Fatalf("warnings: %v", warns)
	}
	if len(hooks) != 1 || hooks[0].ID != "stage" || hooks[0].Event != "item_moved" {
		t.Fatalf("hooks: %+v", hooks)
	}
}

func TestLoadHooks_FolderOverridesGlobalByID(t *testing.T) {
	globalDir := t.TempDir()
	folder := t.TempDir()
	writeFile(t, filepath.Join(globalDir, GlobalHooksFile), `
hooks:
  - name: Global notify
    id: notify
    event: item_created
    command: echo global
  - name: Global only
    id: global-only
    event: item_deleted
    command: echo g
`)
	writeFile(t, filepath.Join(folder, FolderHooksFile), `
hooks:
  - name: Folder notify
    id: notify
    event: item_created
    command: echo folder
`)
	hooks, warns, err := loadHooksFrom(globalDir, folder)
	if err != nil {
		t.Fatalf("loadHooksFrom: %v", err)
	}
	if len(warns) != 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	byID := map[string]Hook{}
	for _, h := range hooks {
		byID[h.ID] = h
	}
	if byID["notify"].Name != "Folder notify" {
		t.Errorf("id notify: got %q want Folder notify", byID["notify"].Name)
	}
	if byID["global-only"].Name != "Global only" {
		t.Errorf("id global-only: got %q want Global only", byID["global-only"].Name)
	}
}

func TestLoadHooks_InvalidEntriesWarnAndSkip(t *testing.T) {
	folder := t.TempDir()
	writeFile(t, filepath.Join(folder, FolderHooksFile), `
hooks:
  - name: ""
    id: a
    event: item_created
    command: echo a
  - name: No id
    id: ""
    event: item_created
    command: echo b
  - name: No command
    id: c
    event: item_created
    command: ""
  - name: No event
    id: d
    event: ""
    command: echo d
  - name: Good
    id: good
    event: item_created
    command: echo good
`)
	hooks, warns, err := loadHooksFrom(t.TempDir(), folder)
	if err != nil {
		t.Fatalf("loadHooksFrom: %v", err)
	}
	if len(hooks) != 1 || hooks[0].Name != "Good" {
		t.Fatalf("hooks: %+v", hooks)
	}
	if len(warns) != 4 {
		t.Fatalf("warnings: got %d want 4 (%+v)", len(warns), warns)
	}
}

func TestLoadHooks_DisallowedEventRejected(t *testing.T) {
	folder := t.TempDir()
	writeFile(t, filepath.Join(folder, FolderHooksFile), `
hooks:
  - name: On select
    id: sel
    event: item_select
    command: echo nope
  - name: Good
    id: good
    event: item_moved
    command: echo ok
`)
	hooks, warns, err := loadHooksFrom(t.TempDir(), folder)
	if err != nil {
		t.Fatalf("loadHooksFrom: %v", err)
	}
	if len(hooks) != 1 || hooks[0].ID != "good" {
		t.Fatalf("hooks: got %+v want only the item_moved hook", hooks)
	}
	if len(warns) != 1 {
		t.Fatalf("warnings: got %d want 1 (%+v)", len(warns), warns)
	}
	if !strings.Contains(warns[0].Message, "not hookable") || !strings.Contains(warns[0].Message, "kbrd.on") {
		t.Errorf("warning should name kbrd.on as the alternative: %q", warns[0].Message)
	}
}

func TestLoadHooks_DuplicateIDWithinFolder(t *testing.T) {
	folder := t.TempDir()
	writeFile(t, filepath.Join(folder, FolderHooksFile), `
hooks:
  - name: First
    id: x
    event: item_created
    command: echo first
  - name: Second
    id: x
    event: item_created
    command: echo second
`)
	hooks, warns, err := loadHooksFrom(t.TempDir(), folder)
	if err != nil {
		t.Fatalf("loadHooksFrom: %v", err)
	}
	if len(hooks) != 2 {
		t.Fatalf("hooks: got %d want 2 (both kept; duplicates only warn)", len(hooks))
	}
	if len(warns) != 1 || !strings.Contains(warns[0].Message, "duplicate") {
		t.Fatalf("warnings: %+v", warns)
	}
}

func TestLoadHooks_MalformedYAML(t *testing.T) {
	folder := t.TempDir()
	writeFile(t, filepath.Join(folder, FolderHooksFile), "::: not yaml :::\n- nope")
	hooks, warns, err := loadHooksFrom(t.TempDir(), folder)
	if err != nil {
		t.Fatalf("loadHooksFrom: %v", err)
	}
	if len(hooks) != 0 {
		t.Errorf("hooks: got %+v want none", hooks)
	}
	if len(warns) == 0 || !strings.Contains(warns[0].Message, "parse error") {
		t.Fatalf("expected a parse-error warning, got %+v", warns)
	}
}

func TestHook_Render(t *testing.T) {
	h := Hook{Template: `git -C "{{.boardPath}}" add "{{.toColumn}}/{{.fileName}}.md"`}
	out, err := h.Render(map[string]string{
		"boardPath": "/b",
		"toColumn":  "Done",
		"fileName":  "task",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	want := `git -C "/b" add "Done/task.md"`
	if out != want {
		t.Errorf("got %q want %q", out, want)
	}
}

func TestHook_Render_MissingVariableIsError(t *testing.T) {
	h := Hook{Template: `echo {{.unknownVar}}`}
	if _, err := h.Render(map[string]string{}); err == nil {
		t.Fatal("expected error for missing variable, got nil")
	}
}
