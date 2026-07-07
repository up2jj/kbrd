package model

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"kbrd/config"
)

func openTestEditor(t *testing.T, b *Board) {
	t.Helper()
	writeColItem(t, b.columns[0], "task")
	item := b.columns[0].ItemByName("task")
	if item == nil {
		t.Fatal("test item not loaded")
	}
	_ = b.editor.OpenEdit(0, b.columns[0].Path, item.Name, item.FullPath)
}

func TestBoardViewFrame_OverlayPriorityCustomAndScriptOverEditor(t *testing.T) {
	t.Parallel()
	b := boardWithNCols(t, 1, 1)
	openTestEditor(t, b)
	b.scriptUI.OpenPicker("cmd", "tok", "Script picker", []string{"script choice"})
	b.customCmds.Open([]config.Command{{Name: "Custom first"}}, nil, nil, nil)

	overlay := boardViewFrame{b: b}.activeOverlay(120, 30, 28)
	if !strings.Contains(overlay, "Custom commands") || !strings.Contains(overlay, "Custom first") {
		t.Fatalf("custom command menu should render above script/editor:\n%s", overlay)
	}
	if strings.Contains(overlay, "Script picker") {
		t.Fatalf("script UI leaked above custom command menu:\n%s", overlay)
	}

	b.customCmds.Close()
	overlay = boardViewFrame{b: b}.activeOverlay(120, 30, 28)
	if !strings.Contains(overlay, "Script picker") || !strings.Contains(overlay, "script choice") {
		t.Fatalf("script UI should render above editor when custom menu is closed:\n%s", overlay)
	}
}

func TestBoardViewFrame_OverlayPriorityEditorBeforePassivePanels(t *testing.T) {
	t.Parallel()
	b := boardWithNCols(t, 1, 1)
	openTestEditor(t, b)

	overlay := boardViewFrame{b: b}.activeOverlay(120, 30, 28)
	if overlay == "" {
		t.Fatal("editor overlay is empty")
	}
	if strings.Contains(overlay, "Search") || strings.Contains(overlay, "Templates") {
		t.Fatalf("unexpected passive panel rendered over editor:\n%s", overlay)
	}
}

func TestBoardViewFrame_RenderEmptyUsesDialogWhenActive(t *testing.T) {
	t.Parallel()
	b := NewBoard(config.Config{Path: t.TempDir(), NotifyBackend: "none"})
	b.termWidth = 80
	b.termHeight = 24
	b.dialog.OpenConfirm("Initialize board?", "Create default columns", initBoardConfirmMsg{})

	out := boardViewFrame{b: b}.render()
	if !strings.Contains(out, "Initialize board?") || !strings.Contains(out, "Create default columns") {
		t.Fatalf("empty board did not render active dialog:\n%s", out)
	}
	if strings.Contains(out, "No columns found") {
		t.Fatalf("empty board rendered fallback behind active dialog:\n%s", out)
	}
}

func TestBoardViewFrame_RenderEmptyFallback(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	b := NewBoard(config.Config{Path: dir, NotifyBackend: "none"})

	out := boardViewFrame{b: b}.render()
	if !strings.Contains(out, "No columns found in "+dir) {
		t.Fatalf("empty fallback = %q, want board path", out)
	}
}

func TestBoardViewFrame_RenderTinyShortCircuits(t *testing.T) {
	t.Parallel()
	b := boardWithNCols(t, 2, 1)
	b.termWidth = 20
	b.termHeight = 24

	out := boardViewFrame{b: b}.render()
	if !strings.Contains(out, "terminal too small") {
		t.Fatalf("tiny width view missing placeholder:\n%s", out)
	}

	b.termWidth = 120
	b.termHeight = 5
	out = boardViewFrame{b: b}.render()
	if !strings.Contains(out, "terminal too small") {
		t.Fatalf("tiny height view missing placeholder:\n%s", out)
	}
}

func TestBoardViewFrame_RenderBaseIncludesMnemonicJump(t *testing.T) {
	t.Parallel()
	b := boardWithNCols(t, 2, 2)
	b.mnemonic.active = true
	b.mnemonic.input.SetValue("sf")

	out, _ := boardViewFrame{b: b}.renderBase(120, 30)
	stripped := ansi.Strip(out)
	if !strings.Contains(stripped, ": sf") {
		t.Fatalf("base view missing mnemonic input:\n%s", out)
	}
	for line := range strings.SplitSeq(stripped, "\n") {
		if strings.Contains(line, ": sf") {
			if !strings.HasPrefix(line, " ") {
				t.Fatalf("mnemonic input line is not centered:\n%s", out)
			}
			return
		}
	}
	t.Fatalf("base view missing mnemonic input line:\n%s", out)
}

func TestBoardViewFrame_MnemonicJumpWidthStable(t *testing.T) {
	t.Parallel()
	b := boardWithNCols(t, 2, 2)
	b.mnemonic.active = true

	empty := strings.TrimSpace(boardViewFrame{b: b}.renderMnemonicJump(120))
	b.mnemonic.input.SetValue("sf")
	filled := strings.TrimSpace(boardViewFrame{b: b}.renderMnemonicJump(120))

	if lipgloss.Width(empty) != lipgloss.Width(filled) {
		t.Fatalf("mnemonic input width changed: empty=%d filled=%d\nempty: %q\nfilled: %q", lipgloss.Width(empty), lipgloss.Width(filled), empty, filled)
	}
}

func TestBoardViewFrame_ActiveOverlayComposesOverBoard(t *testing.T) {
	t.Parallel()
	b := boardWithNCols(t, 2, 2)
	b.dialog.OpenConfirm("Delete item?", "task.md", deleteConfirmMsg{})

	out := boardViewFrame{b: b}.render()
	for _, want := range []string{"kbrd", "Delete item?", "task.md"} {
		if !strings.Contains(out, want) {
			t.Fatalf("overlay view missing %q:\n%s", want, out)
		}
	}
}

func TestBoardViewFrame_EditorSuppressesBoardStatusBar(t *testing.T) {
	t.Parallel()
	b := boardWithNCols(t, 1, 1)

	_, layout := boardViewFrame{b: b}.renderBase(120, 30)
	if layout.footerH == 0 {
		t.Fatal("footer height = 0 before editor opens, want visible keybar")
	}
	if layout.footer == "" {
		t.Fatal("footer is empty before editor opens, want visible keybar")
	}

	openTestEditor(t, b)
	_, layout = boardViewFrame{b: b}.renderBase(120, 30)
	if layout.footerH != 0 {
		t.Fatalf("footer height = %d with editor open, want 0", layout.footerH)
	}
	if layout.footer != "" {
		t.Fatalf("footer rendered with editor open:\n%s", layout.footer)
	}
	frameH := layout.overlayBandH(30)
	overlay := boardViewFrame{b: b}.activeOverlay(120, 30, frameH)
	if overlay == "" {
		t.Fatal("editor overlay is empty")
	}
	if b.editor.frameHeight != frameH {
		t.Fatalf("editor frameHeight = %d, want overlay band height %d", b.editor.frameHeight, frameH)
	}
}

func TestBoardViewFrame_BaseFramePinsFooterToTerminalHeight(t *testing.T) {
	t.Parallel()
	b := boardWithNCols(t, 1, 1)
	const termH = 60

	base, layout := boardViewFrame{b: b}.renderBase(120, termH)
	if layout.footerH == 0 {
		t.Fatal("footer height = 0, want visible keybar")
	}
	if got := lipgloss.Height(base); got != termH {
		t.Fatalf("base height = %d, want terminal height %d", got, termH)
	}
}

func TestBoardViewFrame_OverlayBandExcludesFooter(t *testing.T) {
	t.Parallel()
	b := boardWithNCols(t, 1, 1)

	_, layout := boardViewFrame{b: b}.renderBase(120, 30)
	if layout.footerH == 0 {
		t.Fatal("footer height = 0, want visible keybar")
	}
	want := 30 - layout.headerH - layout.footerH
	if got := layout.overlayBandH(30); got != want {
		t.Fatalf("overlay band height = %d, want %d", got, want)
	}
}
