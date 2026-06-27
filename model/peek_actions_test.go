package model

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
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

func TestPeekActions_SpaceOpensPeek(t *testing.T) {
	t.Parallel()
	b := boardWithNCols(t, 1, 1)
	writeColItem(t, b.columns[0], "task")

	_, cmd := b.inputRouter().HandleKey(keyPressText(" "))
	if cmd != nil {
		cmd()
	}

	if !b.peek.Active() {
		t.Fatal("space key did not open peek")
	}
	if b.peek.title != "task" {
		t.Fatalf("peek title = %q, want task", b.peek.title)
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

	_, cmd = b.inputRouter().HandleKey(keyPressText(" "))
	if cmd != nil {
		cmd()
	}
	if !b.peek.Active() {
		t.Fatal("peek closed on space page-down key")
	}
	if b.peek.offset != 2 {
		t.Fatalf("peek offset after space = %d, want 2", b.peek.offset)
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

func TestPeekViewScrollbarUsesReservedRightGutter(t *testing.T) {
	t.Parallel()

	const (
		termWidth  = 120
		termHeight = 20
	)
	lines := make([]string, 40)
	for i := range lines {
		lines[i] = "x"
	}

	var p Peek
	p.palette = DarkPalette()
	p.Open("task", strings.Join(lines, "\n"), termWidth)

	out := ansi.Strip(p.View(termWidth, termHeight))
	scrollbarX := 1 + overlayPadH + peekBodyWidth(termWidth) + 1
	foundThumb := false
	for _, line := range strings.Split(out, "\n") {
		x := runeIndex(line, '┃')
		if x < 0 {
			continue
		}
		foundThumb = true
		if x != scrollbarX {
			t.Fatalf("scrollbar thumb x = %d, want %d in line %q", x, scrollbarX, line)
		}
	}
	if !foundThumb {
		t.Fatal("peek scrollbar thumb was not rendered")
	}
}

func TestPeekViewLineMarkersUseLeftGutter(t *testing.T) {
	t.Parallel()

	var p Peek
	p.palette = DarkPalette()
	p.Open("task", "one\ntwo\nthree", 120)

	base := ansi.Strip(p.View(120, 30))
	if strings.Contains(base, "+ two") {
		t.Fatalf("unmarked peek rendered marker gutter:\n%s", base)
	}

	p.SetLineMarkers([]PeekLineMarker{
		{Line: 2, Kind: PeekLineAdded},
		{Line: 3, Kind: PeekLineDeleted},
	}, 120)

	out := ansi.Strip(p.View(120, 30))
	for _, want := range []string{"+ two", "- three"} {
		if !strings.Contains(out, want) {
			t.Fatalf("peek missing marker row %q:\n%s", want, out)
		}
	}
}

func TestPeekViewLineMarkerOnlyOnFirstWrappedRow(t *testing.T) {
	t.Parallel()

	var p Peek
	p.palette = DarkPalette()
	p.Open("task", strings.Repeat("wrapped ", 30), 50)
	p.SetLineMarkers([]PeekLineMarker{{Line: 1, Kind: PeekLineModified}}, 50)

	out := ansi.Strip(p.View(50, 24))
	if got := strings.Count(out, "~"); got != 1 {
		t.Fatalf("marker count = %d, want 1:\n%s", got, out)
	}
}

func TestPeekActions_LoadsGitLineMarkers(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	b := boardWithNCols(t, 1, 1)
	writeColItem(t, b.columns[0], "task")
	runPeekGit(t, b.cfg.Path, "init", "-b", "main")
	runPeekGit(t, b.cfg.Path, "add", ".")
	runPeekGit(t, b.cfg.Path, "commit", "-m", "initial")
	b.git.Detect()

	path := filepath.Join(b.columns[0].Path, "task.md")
	if err := os.WriteFile(path, []byte("x\nchanged\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, cmd := b.inputRouter().HandleKey(keyPressText(" "))
	if cmd == nil {
		t.Fatal("peek open returned nil marker command")
	}
	msg := cmd()
	if _, updateCmd := b.Update(msg); updateCmd != nil {
		updateCmd()
	}

	out := ansi.Strip(b.peek.View(120, 30))
	if !strings.Contains(out, "+ changed") {
		t.Fatalf("peek did not render loaded git marker:\n%s", out)
	}
}

func runPeekGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func runeIndex(s string, target rune) int {
	for i, r := range []rune(s) {
		if r == target {
			return i
		}
	}
	return -1
}
