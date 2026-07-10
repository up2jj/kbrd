package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestPasteMenuRunsSelectedEntry(t *testing.T) {
	var m PasteMenu
	m.Open([]pasteMenuEntry{
		{Label: "Paste as new file", Msg: pasteNewItemMsg{Content: "body"}},
		{Label: "Append at end", Msg: pasteRequestMsg{Mode: pasteAtEnd}},
	}, 1)

	if !m.Active() {
		t.Fatal("paste menu did not open")
	}
	view := ansi.Strip(m.View(120, 40))
	if strings.Contains(view, "[") || strings.Contains(view, "]") {
		t.Fatalf("paste menu should render as a list, not buttons:\n%s", view)
	}
	if strings.Contains(view, "Replace whole file") {
		t.Fatalf("paste menu should not offer replacement:\n%s", view)
	}

	cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter returned nil")
	}
	msg, ok := cmd().(pasteRequestMsg)
	if !ok {
		t.Fatalf("message = %T, want pasteRequestMsg", msg)
	}
	if msg.Mode != pasteAtEnd {
		t.Fatalf("mode = %v, want pasteAtEnd", msg.Mode)
	}
	if m.Active() {
		t.Fatal("paste menu stayed open after selection")
	}
}

func TestPasteMenuFiltersToNewFileEntry(t *testing.T) {
	var m PasteMenu
	m.Open([]pasteMenuEntry{
		{Label: "Paste as new file", Desc: "Create a .md card", Msg: pasteNewItemMsg{Content: "body"}},
		{Label: "Append at end", Desc: "Into task.md", Msg: pasteRequestMsg{Mode: pasteAtEnd}},
	}, 1)

	m.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	m.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	m.Update(tea.KeyPressMsg{Code: 'w', Text: "w"})

	cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter returned nil")
	}
	msg, ok := cmd().(pasteNewItemMsg)
	if !ok {
		t.Fatalf("message = %T, want pasteNewItemMsg", msg)
	}
	if msg.Content != "body" {
		t.Fatalf("content = %q, want body", msg.Content)
	}
}

func TestPasteNewItemCreatesFileWithClipboardContent(t *testing.T) {
	b := boardWithNCols(t, 1, 1)
	col := b.columns[0]

	model, cmd := b.pasteActions().openNewItem(pasteNewItemMsg{
		Column:   refForColumn(col),
		ColIndex: 0,
		Content:  "clipboard body",
	})
	if cmd == nil {
		t.Fatal("open new item returned nil command")
	}
	b = model.(*Board)
	cmd()

	if b.editor.state != editorNew || b.editor.NewContent != "clipboard body" {
		t.Fatalf("editor state/content = %v/%q, want editorNew/clipboard body", b.editor.state, b.editor.NewContent)
	}

	b.editor.textinput.SetValue("from-clipboard")
	submit, _ := b.editor.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if submit == nil {
		t.Fatal("editor submit returned nil")
	}
	msg := submit()
	updated, _ := b.Update(msg)
	b = updated.(*Board)

	got, err := os.ReadFile(filepath.Join(col.Path, "from-clipboard.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "clipboard body\n" {
		t.Fatalf("created content = %q, want clipboard body newline", got)
	}
	if !columnHasItem(b.columns[0], "from-clipboard") {
		t.Fatal("created item was not loaded into the column")
	}
}
