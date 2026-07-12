package model

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestItemActionLayer_InvokePeekAndEdit(t *testing.T) {
	t.Parallel()
	b := boardWithNCols(t, 1, 1)
	writeColItem(t, b.columns[0], "task")

	cmd, handled := b.itemActions().Invoke(actionPeek, actionSourceKey)
	if !handled {
		t.Fatal("peek was not handled")
	}
	if cmd != nil {
		cmd()
	}
	if !b.peek.Active() || b.peek.title != "task" {
		t.Fatalf("peek active/title = %v/%q, want true/task", b.peek.Active(), b.peek.title)
	}

	b.peek.Close()
	cmd, handled = b.itemActions().Invoke(actionEdit, actionSourceHelp)
	if !handled || cmd == nil {
		t.Fatalf("edit handled=%v cmd nil=%v", handled, cmd == nil)
	}
	cmd()
	if b.editor.state != editorEdit || b.editor.FileName != "task" {
		t.Fatalf("editor = %v %q, want edit task", b.editor.state, b.editor.FileName)
	}
}

func TestItemMarks_ToggleClearAndCrossColumnClearing(t *testing.T) {
	t.Parallel()
	b := boardWithNCols(t, 2, 2)
	writeColItem(t, b.columns[0], "a")
	writeColItem(t, b.columns[1], "b")

	b.selectedCol = 0
	b.columns[0].SelectByName("a")
	b.handleKey(keyPressText("s"))
	if !b.columns[0].IsMarked("a") {
		t.Fatal("expected a to be marked")
	}

	b.selectedCol = 1
	b.columns[1].SelectByName("b")
	b.handleKey(keyPressText("s"))
	if b.columns[0].MarkedCount() != 0 {
		t.Fatalf("previous column marks = %d, want 0", b.columns[0].MarkedCount())
	}
	if !b.columns[1].IsMarked("b") {
		t.Fatal("expected b to be marked")
	}

	b.handleKey(keyPressText("S"))
	if b.columns[1].MarkedCount() != 0 {
		t.Fatalf("marks after S = %d, want 0", b.columns[1].MarkedCount())
	}
}

func TestItemMarks_PruneAndDisplayOrder(t *testing.T) {
	t.Parallel()
	col := newTestColumn(t, map[string]string{"b": "body", "a": "body"})
	col.ToggleMark("a")
	col.ToggleMark("b")

	got := itemNames(col.MarkedItems())
	want := []string{"a", "b"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("marked order = %v, want %v", got, want)
		}
	}

	if err := os.Remove(filepath.Join(col.Path, "a.md")); err != nil {
		t.Fatal(err)
	}
	if err := col.LoadItems(); err != nil {
		t.Fatal(err)
	}
	if col.IsMarked("a") || col.MarkedCount() != 1 {
		t.Fatalf("marks after prune: count=%d a=%v", col.MarkedCount(), col.IsMarked("a"))
	}
}

func TestItemActionLayer_BatchMoveMarkedItems(t *testing.T) {
	t.Parallel()
	b := boardWithNCols(t, 2, 2)
	writeColItem(t, b.columns[0], "a")
	writeColItem(t, b.columns[0], "b")
	b.selectedCol = 0
	b.columns[0].ToggleMark("a")
	b.columns[0].ToggleMark("b")

	b.handleKey(keyPressText("M"))

	if columnHasItem(b.columns[0], "a") || columnHasItem(b.columns[0], "b") {
		t.Fatalf("source still has moved items: %+v", b.columns[0].Items)
	}
	if !columnHasItem(b.columns[1], "a") || !columnHasItem(b.columns[1], "b") {
		t.Fatalf("target missing moved items: %+v", b.columns[1].Items)
	}
	if b.selectedCol != 1 {
		t.Fatalf("selectedCol = %d, want 1", b.selectedCol)
	}
	if b.columns[0].MarkedCount() != 0 {
		t.Fatalf("source marks = %d, want 0", b.columns[0].MarkedCount())
	}
}

func TestItemActionLayer_MoveMenuMovesMarkedItemsToChosenDestination(t *testing.T) {
	t.Parallel()
	b := boardWithNCols(t, 3, 3)
	writeColItem(t, b.columns[0], "a")
	writeColItem(t, b.columns[0], "b")
	b.selectedCol = 0
	b.columns[0].ToggleMark("a")
	b.columns[0].ToggleMark("b")

	b.handleKey(keyPressText("m"))
	if !b.moveMenu.Active() {
		t.Fatal("move key did not open destination picker")
	}
	b.handleKey(keyPressText("/"))
	b.handleKey(keyPressText("c2"))
	if entry, ok := b.moveMenu.SelectedEntry(); !ok || entry.Label != "c2" {
		t.Fatalf("filtered destination = %+v ok=%v filter=%q", entry, ok, b.moveMenu.filter)
	}
	b.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})

	if b.moveMenu.Active() {
		t.Fatal("destination picker remained open after confirmation")
	}
	if columnHasItem(b.columns[0], "a") || columnHasItem(b.columns[0], "b") {
		t.Fatalf("source still has moved items: %+v", b.columns[0].Items)
	}
	if !columnHasItem(b.columns[2], "a") || !columnHasItem(b.columns[2], "b") {
		t.Fatalf("chosen target missing moved items: col1=%+v col2=%+v", b.columns[1].Items, b.columns[2].Items)
	}
	if b.selectedCol != 2 {
		t.Fatalf("selectedCol = %d, want 2", b.selectedCol)
	}
}

func TestItemActionLayer_BatchDeleteMarkedItems(t *testing.T) {
	t.Parallel()
	b := boardWithNCols(t, 1, 1)
	writeColItem(t, b.columns[0], "a")
	writeColItem(t, b.columns[0], "b")
	b.columns[0].ToggleMark("a")
	b.columns[0].ToggleMark("b")
	targets := targetsForItems(b.columns[0], b.columns[0].MarkedItems())

	b.handleKey(keyPressText("d"))
	if !b.dialog.active {
		t.Fatal("batch delete did not open confirmation dialog")
	}
	b.dialog.Close()
	b.itemActions().handleBatchDelete(batchDeleteConfirmMsg{ColIndex: 0, Targets: []itemRefStable{targets[0].Ref, targets[1].Ref}})

	if b.columns[0].TotalCount() != 0 {
		t.Fatalf("items after delete = %+v, want none", b.columns[0].Items)
	}
}

func TestItemActionLayer_CustomCommandMarkedItemsRunsPerTarget(t *testing.T) {
	t.Parallel()
	b := boardWithNCols(t, 1, 1)
	writeColItem(t, b.columns[0], "a")
	writeColItem(t, b.columns[0], "b")
	if err := os.WriteFile(filepath.Join(b.cfg.Path, ".kbrd_commands.yml"), []byte(`
commands:
  - name: Echo
    id: echo
    command: echo {{.fileName}}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	b.columns[0].ToggleMark("a")
	b.columns[0].ToggleMark("b")

	b.handleKey(keyPressText("x"))
	if !b.customCmds.Active() {
		t.Fatal("custom command menu did not open")
	}
	cmd := b.customCmds.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("custom command enter returned nil")
	}
	msg, ok := cmd().(runCustomCommandBatchMsg)
	if !ok {
		t.Fatalf("command message = %T, want runCustomCommandBatchMsg", msg)
	}
	if len(msg.Runs) != 2 {
		t.Fatalf("runs = %d, want 2", len(msg.Runs))
	}
	if msg.Runs[0].Vars["fileName"] != "a" || msg.Runs[1].Vars["fileName"] != "b" {
		t.Fatalf("run fileNames = %q %q, want a b", msg.Runs[0].Vars["fileName"], msg.Runs[1].Vars["fileName"])
	}
}
