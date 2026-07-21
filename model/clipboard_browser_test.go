package model

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"kbrd/clipboardring"
	"kbrd/config"
)

func TestClipboardMenuFiltersAndSelectsEntries(t *testing.T) {
	var menu ClipboardMenu
	entries := []clipboardring.Entry{
		{ID: "one", Time: time.Now(), Kind: clipboardring.KindChecklist, Text: "- [ ] ship it", Source: clipboardring.Source{Card: "Release"}},
		{ID: "two", Time: time.Now(), Kind: clipboardring.KindText, Text: "meeting notes", Source: clipboardring.Source{Card: "Meeting"}},
	}
	menu.Open(entries, pasteMenuTarget{})
	menu.Update(tea.KeyPressMsg{Code: '/'})
	menu.Update(tea.KeyPressMsg{Text: "meet", Code: 'm'})
	selected, ok := menu.Selected()
	if !ok || selected.ID != "two" {
		t.Fatalf("selected entry = %+v, want two", selected)
	}
	action, selected := menu.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if action != "paste" || selected.ID != "two" {
		t.Fatalf("enter = %q/%q, want paste/two", action, selected.ID)
	}
	if menu.Active() {
		t.Fatal("menu should close after selecting an entry")
	}
}

func TestClipboardMenuActions(t *testing.T) {
	var menu ClipboardMenu
	entry := clipboardring.Entry{ID: "one", Text: "saved"}
	menu.Open([]clipboardring.Entry{entry}, pasteMenuTarget{})
	action, got := menu.Update(tea.KeyPressMsg{Text: "p", Code: 'p'})
	if action != "pin" || got.ID != entry.ID {
		t.Fatalf("pin = %q/%q, want pin/one", action, got.ID)
	}
	action, got = menu.Update(tea.KeyPressMsg{Text: "d", Code: 'd'})
	if action != "delete" || got.ID != entry.ID {
		t.Fatalf("delete = %q/%q, want delete/one", action, got.ID)
	}
	action, _ = menu.Update(tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
	if action != "clear" {
		t.Fatalf("clear = %q, want clear", action)
	}
}

func TestClipboardMenuDoesNotClearWithC(t *testing.T) {
	var menu ClipboardMenu
	menu.Open([]clipboardring.Entry{{ID: "one", Text: "saved"}}, pasteMenuTarget{})

	action, _ := menu.Update(tea.KeyPressMsg{Text: "c", Code: 'c'})
	if action != "" {
		t.Fatalf("c action = %q, want no action", action)
	}
}

func TestClipboardEntryMetaUsesHumanReadableAge(t *testing.T) {
	entry := clipboardring.Entry{Time: time.Now().Add(-5 * 24 * time.Hour)}
	if got, want := entryMeta(entry), "5d ago"; got != want {
		t.Fatalf("entryMeta() = %q, want %q", got, want)
	}
}

func TestClipboardMenuPreviewIsBoxed(t *testing.T) {
	menu := ClipboardMenu{palette: DarkPalette()}
	preview := menu.previewBlock(clipboardring.Entry{Kind: clipboardring.KindMarkdown, Text: "# Notes"}, 80)
	if !strings.Contains(preview, "Clipboard preview · markdown") {
		t.Fatalf("preview title = %q", preview)
	}
	if !strings.Contains(preview, "╭") || !strings.Contains(preview, "╯") {
		t.Fatalf("preview is not rounded boxed: %q", preview)
	}
}

func TestClipboardMenuPreviewMatchesOverlayWidth(t *testing.T) {
	var menu ClipboardMenu
	menu.palette = DarkPalette()
	menu.Open([]clipboardring.Entry{{ID: "one", Time: time.Now(), Kind: clipboardring.KindText, Text: "saved text"}}, pasteMenuTarget{})
	lines := strings.Split(menu.View(80, 40), "\n")
	frameWidth := 0
	previewWidth := 0
	for _, line := range lines {
		plain := ansi.Strip(line)
		if strings.Contains(plain, "Clipboard history") {
			frameWidth = lipgloss.Width(line)
		}
		if strings.Contains(plain, "   ╰") && previewWidth == 0 {
			previewWidth = lipgloss.Width(line) - 8
		}
	}
	if frameWidth == 0 || previewWidth == 0 {
		t.Fatalf("could not find frame and preview borders in view:\n%s", strings.Join(lines, "\n"))
	}
	if previewWidth != frameWidth-8 {
		t.Fatalf("preview width = %d, frame width = %d; want preview flush with frame content", previewWidth, frameWidth)
	}
}

func TestPasteClipboardPreviewIsBoxed(t *testing.T) {
	preview := clipboardPreviewBlock(DarkPalette(), "Clipboard preview", "saved text", 80)
	if !strings.Contains(preview, "Clipboard preview") || !strings.Contains(preview, "╭") || !strings.Contains(preview, "╯") {
		t.Fatalf("paste preview is not boxed: %q", preview)
	}
}

func TestClipboardMenuContentWidthIsCapped(t *testing.T) {
	var menu ClipboardMenu
	if got := menu.contentWidth(240); got != 88 {
		t.Fatalf("wide terminal content width = %d, want 88", got)
	}
	if got := menu.contentWidth(72); got != 56 {
		t.Fatalf("normal terminal content width = %d, want 56", got)
	}
}

func TestClipboardRingImportsSystemClipboardContent(t *testing.T) {
	store, err := clipboardring.Open(filepath.Join(t.TempDir(), "clipboard.json"))
	if err != nil {
		t.Fatal(err)
	}
	b := NewBoard(config.Config{NotifyBackend: "none"})
	b.clipboardRing = store
	target := pasteMenuTarget{}
	b.clipboardRead = clipboardReadState{kind: clipboardReadRingImport, ring: target}
	if _, _ = b.clipboardActions().handleContent("external notes"); len(store.Entries()) != 1 {
		t.Fatalf("entries after import = %d, want 1", len(store.Entries()))
	}
	entry := store.Entries()[0]
	if entry.Text != "external notes" || entry.Metadata["system_clipboard"] != true {
		t.Fatalf("imported entry = %+v", entry)
	}
}

func TestClipboardBrowserImportRequestsClipboardRead(t *testing.T) {
	store, err := clipboardring.Open(filepath.Join(t.TempDir(), "clipboard.json"))
	if err != nil {
		t.Fatal(err)
	}
	b := NewBoard(config.Config{NotifyBackend: "none"})
	b.clipboardRing = store
	b.clipboardMenu.Open(nil, pasteMenuTarget{})
	b.clipboardActions().updateBrowser(tea.KeyPressMsg{Text: "i", Code: 'i'})
	if b.clipboardRead.kind != clipboardReadRingImport {
		t.Fatalf("clipboard read kind = %v, want ring import", b.clipboardRead.kind)
	}
}

func TestClipboardPasteCancelReturnsToRing(t *testing.T) {
	store, err := clipboardring.Open(filepath.Join(t.TempDir(), "clipboard.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Add(clipboardring.Entry{ID: "ring-entry", Text: "saved text"}); err != nil {
		t.Fatal(err)
	}
	b := NewBoard(config.Config{NotifyBackend: "none"})
	b.clipboardRing = store
	b.pasteMenu.Open([]pasteMenuEntry{{Label: "Append", Msg: pasteNewItemMsg{Content: "saved text"}}}, 0)
	b.clipboardReturn = true
	b.clipboardTarget = pasteMenuTarget{}
	if layer := b.activeModalLayer(); layer == nil {
		t.Fatal("paste menu should be the active modal")
	} else {
		layer.key(tea.KeyPressMsg{Code: tea.KeyEsc})
	}
	if !b.clipboardMenu.Active() {
		t.Fatal("esc should return to the clipboard ring")
	}
	if b.pasteMenu.Active() {
		t.Fatal("paste menu should close on esc")
	}
}

func TestClipboardPasteConfirmDoesNotReturnToRing(t *testing.T) {
	b := NewBoard(config.Config{NotifyBackend: "none"})
	b.pasteMenu.Open([]pasteMenuEntry{{Label: "Append", Msg: pasteNewItemMsg{Content: "saved text"}}}, 0)
	b.clipboardReturn = true
	if layer := b.activeModalLayer(); layer == nil {
		t.Fatal("paste menu should be the active modal")
	} else {
		layer.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	}
	if b.clipboardMenu.Active() {
		t.Fatal("confirmed paste should not reopen the clipboard ring")
	}
	if b.clipboardReturn {
		t.Fatal("clipboard return state should clear after confirmation")
	}
}
