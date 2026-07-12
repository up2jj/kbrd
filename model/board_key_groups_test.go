package model

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func keyRunes(s string) tea.KeyPressMsg {
	return keyPressText(s)
}

func TestBoardKeyGroups_DisplayAndNavigationKeys(t *testing.T) {
	t.Parallel()
	b := boardWithNCols(t, 4, 2)

	if _, _, handled := b.handleColumnBoardKey(keyRunes("+"), b.columns[0]); !handled {
		t.Fatal("zoom key was not handled")
	}
	if !b.zoom.Active() {
		t.Fatal("zoom key did not activate zoom")
	}

	if _, _, handled := b.handleColumnBoardKey(keyRunes("-"), b.columns[0]); !handled {
		t.Fatal("zoom off key was not handled")
	}
	if b.zoom.Active() {
		t.Fatal("zoom off key did not disable zoom")
	}

	if _, _, handled := b.handleColumnBoardKey(keyRunes("|"), b.columns[0]); !handled {
		t.Fatal("collapse key was not handled")
	}
	if !b.columns[0].Collapsed {
		t.Fatal("collapse key did not collapse the focused column")
	}
	if b.selectedCol == 0 {
		t.Fatal("collapsing focused column should shift focus")
	}

	b.selectedCol = 1
	if _, _, handled := b.handleColumnBoardKey(keyRunes("]"), b.columns[1]); !handled {
		t.Fatal("next-column key was not handled")
	}
	if b.selectedCol != 2 {
		t.Fatalf("selectedCol after next = %d, want 2", b.selectedCol)
	}

	if _, _, handled := b.handleColumnBoardKey(keyRunes("1"), b.columns[2]); !handled {
		t.Fatal("jump-column key was not handled")
	}
	if b.selectedCol != 0 {
		t.Fatalf("selectedCol after jump = %d, want 0", b.selectedCol)
	}
}

func TestBoardKeyGroups_FilteringColumnOwnsBoardKeys(t *testing.T) {
	t.Parallel()
	b := boardWithNCols(t, 2, 2)
	if _, _, handled := b.handleColumnBoardKey(keyRunes("/"), b.columns[0]); !handled {
		t.Fatal("filter key was not handled")
	}
	if !b.columns[0].IsFiltering() {
		t.Fatal("filter key did not put column into filtering mode")
	}

	_, _ = b.handleBoardKey(keyRunes(":"))
	if b.mnemonic.active {
		t.Fatal("mnemonic selector opened while column filter was active")
	}
}

func TestBoardKeyGroups_CustomCommandContextForItemAndEmptyColumn(t *testing.T) {
	t.Parallel()
	b := boardWithNCols(t, 2, 2)
	writeColItem(t, b.columns[0], "task")
	if err := os.WriteFile(filepath.Join(b.cfg.Path, ".kbrd_commands.yml"), []byte(`
commands:
  - name: Item command
    id: item
    command: echo {{.fileName}}
  - name: Empty command
    id: empty
    requiresItem: false
    command: echo {{.columnName}}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	item := b.columns[0].SelectedItem()
	if item == nil {
		t.Fatal("test item not selected")
	}
	if _, _, handled := b.handleItemBoardKey(keyRunes("x"), b.columns[0]); !handled {
		t.Fatal("item custom-command key was not handled")
	}
	if !b.customCmds.Active() {
		t.Fatal("item custom-command key did not open menu")
	}
	if len(b.customCmds.commands) != 2 {
		t.Fatalf("item custom commands = %d, want 2", len(b.customCmds.commands))
	}
	if got := b.customCmds.vars["fileName"]; got != item.Name {
		t.Fatalf("fileName var = %q, want %q", got, item.Name)
	}

	b.customCmds.Close()
	b.selectedCol = 1
	if _, _ = b.handleListBoardKey(keyRunes("x"), b.columns[1]); !b.customCmds.Active() {
		t.Fatal("empty-column custom-command key did not open menu")
	}
	if len(b.customCmds.commands) != 1 || b.customCmds.commands[0].ID != "empty" {
		t.Fatalf("empty-column commands = %+v, want only empty command", b.customCmds.commands)
	}
	if _, ok := b.customCmds.vars["fileName"]; ok {
		t.Fatalf("empty-column vars unexpectedly include fileName: %+v", b.customCmds.vars)
	}
}

func TestBoardKeyGroups_NewOpensCreateMenu(t *testing.T) {
	t.Parallel()
	b := boardWithNCols(t, 1, 2)
	if _, _, handled := b.handleColumnBoardKey(keyRunes("n"), b.columns[0]); !handled {
		t.Fatal("new key was not handled")
	}
	if !b.templateFlow.Active() {
		t.Fatal("new key did not open create menu")
	}
	if b.editor.state != editorNone {
		t.Fatalf("editor state = %v, want none until empty-card choice is selected", b.editor.state)
	}
}

func TestBoardKeyGroups_MarkedArrowsMoveBatch(t *testing.T) {
	for _, tc := range []struct {
		name   string
		left   bool
		source int
		target int
	}{
		{name: "left", left: true, source: 1, target: 0},
		{name: "right", source: 1, target: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b := boardWithNCols(t, 3, 3)
			writeColItem(t, b.columns[tc.source], "a")
			writeColItem(t, b.columns[tc.source], "b")
			b.selectedCol = tc.source
			b.columns[tc.source].ToggleMark("a")
			b.columns[tc.source].ToggleMark("b")

			code := tea.KeyRight
			if tc.left {
				code = tea.KeyLeft
			}
			b.handleKey(tea.KeyPressMsg{Code: code})

			if b.selectedCol != tc.target {
				t.Fatalf("selectedCol = %d, want %d", b.selectedCol, tc.target)
			}
			if columnHasItem(b.columns[tc.source], "a") || columnHasItem(b.columns[tc.source], "b") {
				t.Fatalf("source still has marked items: %+v", b.columns[tc.source].Items)
			}
			if !columnHasItem(b.columns[tc.target], "a") || !columnHasItem(b.columns[tc.target], "b") {
				t.Fatalf("target missing marked items: %+v", b.columns[tc.target].Items)
			}
		})
	}
}
