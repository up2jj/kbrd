package model

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// A file containing a tab character must not read as dirty the moment it opens:
// the textarea sanitizer rewrites tabs to spaces, so the dirty baseline has to be
// the normalized buffer value rather than the raw file bytes.
func TestOpenEditTabbedFileNotDirty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	if err := os.WriteFile(path, []byte("line one\n\tindented with a tab\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	e := NewEditor(false)
	e.SetSize(120, 40)
	e.OpenEdit(0, "", "note", path)

	if e.IsDirty() {
		t.Fatalf("editor reports dirty immediately on open of a tabbed file; value=%q initial=%q", e.textarea.Value(), e.initialValue)
	}
}

// openEditorWith writes content to a temp file and opens it for editing with the
// cursor parked at the end (OpenEdit calls CursorEnd), i.e. on the last line.
func openEditorWith(t *testing.T, content string) *Editor {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	e := NewEditor(false)
	e.SetSize(120, 40)
	e.OpenEdit(0, "", "note", path)
	return e
}

// A line command reads the cursor's line via CurrentLine and writes it back via
// ReplaceCurrentLine; the swap is one undo step.
func TestReplaceCurrentLine(t *testing.T) {
	e := openEditorWith(t, "alpha\nbravo\ncharlie\n")

	if got := e.CurrentLine(); got != "charlie" {
		t.Fatalf("CurrentLine = %q, want %q", got, "charlie")
	}
	e.ReplaceCurrentLine("CHARLIE")
	if got := e.textarea.Value(); got != "alpha\nbravo\nCHARLIE" {
		t.Fatalf("after replace, value = %q", got)
	}
	// One undo restores the line exactly.
	e.undoOnce()
	if got := e.textarea.Value(); got != "alpha\nbravo\ncharlie" {
		t.Fatalf("after undo, value = %q", got)
	}
}

// ReplaceCurrentLine targets whichever logical line the cursor is on, not just
// the last one.
func TestReplaceMiddleLine(t *testing.T) {
	e := openEditorWith(t, "alpha\nbravo\ncharlie\n")
	e.Update(tea.KeyPressMsg{Code: tea.KeyUp}) // move from charlie up to bravo

	if got := e.CurrentLine(); got != "bravo" {
		t.Fatalf("CurrentLine = %q, want %q", got, "bravo")
	}
	e.ReplaceCurrentLine("B")
	if got := e.textarea.Value(); got != "alpha\nB\ncharlie" {
		t.Fatalf("after replace, value = %q", got)
	}
}

// A line command captures its target row at dispatch, so its (possibly async)
// result must replace that row even after the cursor has moved — ReplaceLine
// targets the captured row, never the cursor's current one. This is the editor
// half of the "line filter can replace the wrong row" fix.
func TestReplaceLineTargetsCapturedRow(t *testing.T) {
	e := openEditorWith(t, "alpha\nbravo\ncharlie\n") // cursor parked on charlie (row 2)
	targetRow := e.CurrentRow()
	if targetRow != 2 {
		t.Fatalf("CurrentRow = %d, want 2", targetRow)
	}

	// Cursor moves away before the result lands (the slow-filter race).
	e.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	e.Update(tea.KeyPressMsg{Code: tea.KeyUp}) // now on alpha (row 0)
	if got := e.CurrentLine(); got != "alpha" {
		t.Fatalf("precondition CurrentLine = %q, want alpha", got)
	}

	e.ReplaceLine(targetRow, "CHARLIE")
	if got := e.textarea.Value(); got != "alpha\nbravo\nCHARLIE" {
		t.Fatalf("ReplaceLine hit the wrong row: value = %q", got)
	}

	// A stale row past the end of a shrunken buffer is a safe no-op.
	e.ReplaceLine(99, "X")
	if got := e.textarea.Value(); got != "alpha\nbravo\nCHARLIE" {
		t.Fatalf("out-of-range ReplaceLine should no-op, value = %q", got)
	}
}

// A replacement containing a newline splits the line into several.
func TestReplaceCurrentLineMultiLine(t *testing.T) {
	e := openEditorWith(t, "alpha\nbravo\ncharlie\n")
	e.ReplaceCurrentLine("c1\nc2")
	if got := e.textarea.Value(); got != "alpha\nbravo\nc1\nc2" {
		t.Fatalf("after multi-line replace, value = %q", got)
	}
	// Cursor lands on the last line of the replacement.
	if got := e.CurrentLine(); got != "c2" {
		t.Fatalf("CurrentLine after multi-line replace = %q, want %q", got, "c2")
	}
}

// Replacing a line far down a buffer that overflows the viewport must keep that
// line visible — SetValue resets the viewport to the top, so the splice has to
// scroll it back onto the cursor (otherwise the editor "bumps to the first line").
func TestReplaceCurrentLineKeepsLineVisible(t *testing.T) {
	var sb strings.Builder
	for i := range 60 {
		fmt.Fprintf(&sb, "line-%02d\n", i)
	}
	e := openEditorWith(t, sb.String()) // cursor parked at the last line
	e.SetSize(120, 16)                  // small viewport so the buffer scrolls
	// Establish the real precondition: a render has primed the viewport content
	// (so it knows its scroll bounds) and the cursor is followed at the bottom.
	_ = e.textarea.View()
	e.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
	if got := e.CurrentLine(); got != "line-59" {
		t.Fatalf("CurrentLine = %q, want line-59", got)
	}
	if strings.Contains(e.textarea.View(), "line-00") {
		t.Fatalf("precondition: viewport should be scrolled past the top")
	}

	e.ReplaceCurrentLine("DONE-59")

	view := e.textarea.View()
	if !strings.Contains(view, "DONE-59") {
		t.Fatalf("edited line not visible after replace — viewport stuck at top:\n%s", view)
	}
}

// ctrl+e must resize the textarea, including on a freshly constructed editor that
// has been seeded with a terminal size (the second-open regression).
func TestToggleExpandResizesTextarea(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	if err := os.WriteFile(path, []byte("hello\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	e := NewEditor(false)
	e.SetSize(120, 40)
	e.OpenEdit(0, "", "note", path)

	before := e.textarea.Width()
	ctrlE := tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl}
	e.Update(ctrlE)
	after := e.textarea.Width()

	if before == after {
		t.Fatalf("ctrl+e did not change textarea width: stayed at %d", before)
	}
	if e.IsDirty() {
		t.Fatalf("ctrl+e marked the buffer dirty")
	}
}

func TestTextareaTaskPrefixShortcutInsertOrToggle(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain", in: "body", want: "- [ ] body"},
		{name: "unchecked", in: "- [ ] body", want: "- [x] body"},
		{name: "checked", in: "- [x] body", want: "- [ ] body"},
		{name: "checked uppercase", in: "- [X] body", want: "- [ ] body"},
		{name: "indented", in: "  - [ ] body", want: "  - [x] body"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := openEditorWith(t, tt.in)
			e.textarea.CursorStart()

			e.Update(tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
			if got := e.textarea.Value(); got != tt.want {
				t.Fatalf("after ctrl+t, value = %q, want %q", got, tt.want)
			}
			if !e.IsDirty() {
				t.Fatal("ctrl+t did not mark the buffer dirty")
			}
		})
	}
}

func TestTextareaTaskPrefixShortcutUndo(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{name: "insert", in: "body"},
		{name: "toggle", in: "- [ ] body"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := openEditorWith(t, tt.in)
			e.textarea.CursorStart()

			e.Update(tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
			e.Update(tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl})
			if got := e.textarea.Value(); got != tt.in {
				t.Fatalf("after undo, value = %q, want %q", got, tt.in)
			}
		})
	}
}
