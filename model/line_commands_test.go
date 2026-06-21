package model

import (
	"strings"
	"testing"
)

// A line command's ctx must bind to the card the editor was opened against
// (msg.FileName), not the column's current selection — a script/timer/hook can
// move selection while the editor stays open. lineCommandVars resolves by name.
func TestLineCommandVars_BindsToEditorFileNotSelection(t *testing.T) {
	col := newTestColumn(t, map[string]string{"a": "alpha", "b": "bravo"})
	b := &Board{columns: []*Column{col}}

	// Selection has moved to "b", but the editor is open on "a".
	col.SelectByName("b")
	if sel := col.SelectedItem(); sel == nil || sel.Name != "b" {
		t.Fatalf("precondition: expected selection on b, got %+v", sel)
	}

	vars := b.lineCommandVars(openLineCommandsMsg{ColIndex: 0, FileName: "a", Line: "x"})

	if vars["fileName"] != "a" {
		t.Fatalf("fileName = %q, want a (bound to the editor's file, not the selection)", vars["fileName"])
	}
	if !strings.HasSuffix(vars["filePath"], "a.md") {
		t.Fatalf("filePath = %q, want it to point at a.md", vars["filePath"])
	}
	if vars["line"] != "x" {
		t.Fatalf("line = %q, want x", vars["line"])
	}
}

// handleLineShellDone must splice a shell line filter's stdout into the row the
// command was dispatched from (msg.Row), not whichever line the cursor wandered
// to while the async command ran. This is the board-wiring half of the "line
// filter can replace the wrong row" fix.
func TestHandleLineShellDone_TargetsDispatchRow(t *testing.T) {
	e := openEditorWith(t, "alpha\nbravo\ncharlie\n") // cursor on charlie (row 2)
	b := &Board{editor: e}

	// The command was dispatched against row 0 ("alpha"); meanwhile the cursor is
	// still on row 2. The result must land on row 0.
	b.handleLineShellDone(lineShellDoneMsg{Name: "upper", Out: "ALPHA\n", Row: 0})

	if got := e.textarea.Value(); got != "ALPHA\nbravo\ncharlie" {
		t.Fatalf("shell result hit the wrong row: value = %q", got)
	}
}

// A failed run (non-zero exit) leaves every line untouched, regardless of row.
func TestHandleLineShellDone_NonZeroExitLeavesLine(t *testing.T) {
	e := openEditorWith(t, "alpha\nbravo\n")
	b := &Board{editor: e}

	_, _ = b.handleLineShellDone(lineShellDoneMsg{Name: "boom", Out: "err detail", Row: 0, Exit: 1})

	if got := e.textarea.Value(); got != "alpha\nbravo" {
		t.Fatalf("a failed filter must not edit any line, value = %q", got)
	}
}

// Updating the cursor away and back must not change the captured target: feeding
// a stale row that no longer exists is a safe no-op (the buffer may have shrunk).
func TestHandleLineShellDone_StaleRowNoOps(t *testing.T) {
	e := openEditorWith(t, "only\n")
	b := &Board{editor: e}

	b.handleLineShellDone(lineShellDoneMsg{Name: "f", Out: "x", Row: 7})

	if got := e.textarea.Value(); got != "only" {
		t.Fatalf("stale out-of-range row should no-op, value = %q", got)
	}
}
