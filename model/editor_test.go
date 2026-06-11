package model

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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

	e := NewEditor()
	e.SetTermSize(120, 40)
	e.OpenEdit(0, "note", path)

	if e.IsDirty() {
		t.Fatalf("editor reports dirty immediately on open of a tabbed file; value=%q initial=%q", e.textarea.Value(), e.initialValue)
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

	e := NewEditor()
	e.SetTermSize(120, 40)
	e.OpenEdit(0, "note", path)

	before := e.textarea.Width()
	ctrlE := tea.KeyMsg{Type: tea.KeyCtrlE}
	e.Update(ctrlE)
	after := e.textarea.Width()

	if before == after {
		t.Fatalf("ctrl+e did not change textarea width: stayed at %d", before)
	}
	if e.IsDirty() {
		t.Fatalf("ctrl+e marked the buffer dirty")
	}
}
