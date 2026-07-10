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
		{Label: "Append at end", Msg: pasteRequestMsg{Mode: pasteAtEnd, Content: "previewed body"}},
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
	if msg.Content != "previewed body" {
		t.Fatalf("content = %q, want previewed body", msg.Content)
	}
	if m.Active() {
		t.Fatal("paste menu stayed open after selection")
	}
}

func TestPasteMenuOpensFromClipboardMsgWithPreview(t *testing.T) {
	b := boardWithNCols(t, 1, 1)
	col := b.columns[0]
	cmd := b.pasteActions().openMenu(0, col, nil)
	if cmd == nil {
		t.Fatal("opening paste menu returned nil OSC52 request")
	}
	if b.pasteMenu.Active() {
		t.Fatal("paste menu opened before clipboard response")
	}

	model, _ := b.Update(tea.ClipboardMsg{Content: "one\ntwo\nthree\nfour"})
	b = model.(*Board)
	if !b.pasteMenu.Active() {
		t.Fatal("paste menu did not open after clipboard response")
	}
	view := ansi.Strip(b.pasteMenu.View(80, 40))
	for _, want := range []string{"Clipboard preview", "one", "two", "three"} {
		if !strings.Contains(view, want) {
			t.Fatalf("preview missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "four") {
		t.Fatalf("preview should be compact:\n%s", view)
	}

	selected := b.pasteMenu.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	msg, ok := selected().(pasteNewItemMsg)
	if !ok {
		t.Fatalf("selected message = %T, want pasteNewItemMsg", msg)
	}
	if msg.Content != "one\ntwo\nthree\nfour" {
		t.Fatalf("selected content = %q, want exact clipboard content", msg.Content)
	}
}

func TestPasteMenuOpensFromSystemClipboardFallback(t *testing.T) {
	b := boardWithNCols(t, 1, 1)
	cmd := b.pasteActions().openMenu(0, b.columns[0], nil)
	if cmd == nil {
		t.Fatal("opening paste menu returned nil request")
	}
	request := b.clipboardRead.request

	model, fallbackCmd := b.Update(clipboardFallbackMsg{request: request})
	b = model.(*Board)
	if fallbackCmd == nil {
		t.Fatal("clipboard fallback did not request a system read")
	}
	model, _ = b.Update(clipboardSystemReadMsg{request: request, content: "from system clipboard"})
	b = model.(*Board)
	if !b.pasteMenu.Active() {
		t.Fatal("paste menu did not open from system clipboard fallback")
	}
}

func TestFormatClipboardPreview(t *testing.T) {
	got := formatClipboardPreview("\x1b[31mred\x1b[0m\tvalue\nsecond\nthird\nfourth", 12)
	if strings.Contains(got, "\x1b") || strings.Contains(got, "fourth") {
		t.Fatalf("preview should be safe and compact: %q", got)
	}
	if !strings.Contains(got, "red") || !strings.Contains(got, "second") {
		t.Fatalf("preview lost content: %q", got)
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
