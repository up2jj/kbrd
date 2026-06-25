package model

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func openTestPeek(t *testing.T, b *Board) {
	t.Helper()
	writeColItem(t, b.columns[0], "task")
	item := b.columns[0].SelectedItem()
	if item == nil {
		t.Fatal("test item not selected")
	}
	if cmd := b.peek.Open(item.Title, "one\ntwo\nthree", b.termWidth); cmd != nil {
		cmd()
	}
}

func TestPeekActions_OpenEditorModes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		key  string
		want editorState
	}{
		{name: "edit", key: "e", want: editorEdit},
		{name: "append", key: "a", want: editorAppend},
		{name: "prepend", key: "p", want: editorPrepend},
		{name: "journal lowercase", key: "b", want: editorJournal},
		{name: "journal uppercase", key: "J", want: editorJournal},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b := boardWithNCols(t, 1, 1)
			openTestPeek(t, b)

			_, cmd := b.inputRouter().HandleKey(keyRunes(tc.key))
			if cmd != nil {
				cmd()
			}

			if b.peek.Active() {
				t.Fatal("peek remained open after item action")
			}
			if b.editor.state != tc.want {
				t.Fatalf("editor state = %v, want %v", b.editor.state, tc.want)
			}
			if b.editor.FileName != "task" {
				t.Fatalf("editor FileName = %q, want task", b.editor.FileName)
			}
		})
	}
}

func TestPeekActions_PreserveScrollAndCloseKeys(t *testing.T) {
	t.Parallel()
	b := boardWithNCols(t, 1, 1)
	openTestPeek(t, b)
	b.peek.pageSize = 1

	_, cmd := b.inputRouter().HandleKey(keyRunes("j"))
	if cmd != nil {
		cmd()
	}
	if !b.peek.Active() {
		t.Fatal("peek closed on scroll key")
	}
	if b.peek.offset != 1 {
		t.Fatalf("peek offset = %d, want 1", b.peek.offset)
	}
	if b.editor.state != editorNone {
		t.Fatalf("scroll key opened editor state %v", b.editor.state)
	}

	_, cmd = b.inputRouter().HandleKey(keyRunes("q"))
	if cmd != nil {
		cmd()
	}
	if b.peek.Active() {
		t.Fatal("peek remained open after close key")
	}
}

func TestPeekActions_MissingSelectionClosesPeekAndNotifies(t *testing.T) {
	t.Parallel()
	b := boardWithNCols(t, 1, 1)
	openTestPeek(t, b)
	b.columns[0].Items = nil

	_, cmd := b.inputRouter().HandleKey(keyRunes("e"))
	if cmd == nil {
		t.Fatal("missing item action returned nil command")
	}
	cmd()
	if b.peek.Active() {
		t.Fatal("peek remained open after missing item action")
	}
	if b.editor.state != editorNone {
		t.Fatalf("missing item opened editor state %v", b.editor.state)
	}
}

func TestPeekFooterIncludesItemActions(t *testing.T) {
	t.Parallel()
	var p Peek
	p.palette = DarkPalette()
	p.Open("task", "body", 120)

	out := p.View(120, 30)
	for _, want := range []string{"edit", "append/prepend", "journal", "close"} {
		if !strings.Contains(out, want) {
			t.Fatalf("peek footer missing %q:\n%s", want, out)
		}
	}
}

func TestPeekViewMaintainsHeightWhileScrolling(t *testing.T) {
	t.Parallel()

	const (
		termWidth  = 120
		termHeight = 18
	)
	lineCount := 30
	lines := make([]string, lineCount)
	longLine := strings.Repeat("wrapped ", 18)
	for i := range lines {
		lines[i] = longLine
	}

	var p Peek
	p.palette = DarkPalette()
	p.Open("task", strings.Join(lines, "\n"), termWidth)

	top := lipgloss.Height(p.View(termWidth, termHeight))
	if p.pageSize <= 0 {
		t.Fatal("peek pageSize was not established by View")
	}

	for _, offset := range []int{1, p.pageSize / 2, p.pageSize, lineCount - 1} {
		p.offset = offset
		if got := lipgloss.Height(p.View(termWidth, termHeight)); got != top {
			t.Fatalf("height at offset %d = %d, want %d", offset, got, top)
		}
	}

	var short Peek
	short.palette = DarkPalette()
	short.Open("short", "only one line", termWidth)
	if got := lipgloss.Height(short.View(termWidth, termHeight)); got != top {
		t.Fatalf("short content height = %d, want %d", got, top)
	}
}
