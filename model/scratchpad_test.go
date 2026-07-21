package model

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"kbrd/clipboardring"
	"kbrd/config"
	"kbrd/scratchpad"
)

func newScratchpadBoard(t *testing.T, vim bool) (*Board, *scratchpad.Store, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "Todo"), 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := scratchpad.Open(filepath.Join(t.TempDir(), "scratchpads"))
	if err != nil {
		t.Fatal(err)
	}
	b := NewBoardWithOptions(config.Config{
		Path:          root,
		ColumnWidth:   32,
		PreviewLines:  3,
		NotifyBackend: "none",
		Editor:        config.EditorConfig{Vim: vim},
	}, BoardOptions{Scratchpad: store})
	if err := b.loadColumns(); err != nil {
		t.Fatal(err)
	}
	return b, store, root
}

func TestScratchpadShortcutOpensAndAutosaves(t *testing.T) {
	b, store, root := newScratchpadBoard(t, false)
	model, _ := b.handleBoardKey(keyPressText("q"))
	b = model.(*Board)
	if !b.editor.IsScratchpad() {
		t.Fatal("q did not open the scratchpad")
	}
	b.inputRouter().HandleKey(keyPressText("meeting note"))
	got, err := store.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != "meeting note" {
		t.Fatalf("autosaved scratchpad = %q", got)
	}
}

func TestScratchpadAppendSelectedCard(t *testing.T) {
	b, store, root := newScratchpadBoard(t, false)
	card := filepath.Join(b.columns[0].Path, "context.md")
	if err := os.WriteFile(card, []byte("card context\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := b.columns[0].LoadItems(); err != nil {
		t.Fatal(err)
	}
	b.columns[0].SelectByName("context")

	model, _ := b.handleBoardKey(keyPressText("Q"))
	b = model.(*Board)
	got, err := store.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != "card context" {
		t.Fatalf("scratchpad = %q", got)
	}
	if !b.editor.IsScratchpad() {
		t.Fatal("Q did not open the scratchpad")
	}
}

func TestScratchpadClipboardHistoryInsertsAtCursor(t *testing.T) {
	b, store, root := newScratchpadBoard(t, false)
	ring, err := clipboardring.Open(filepath.Join(t.TempDir(), "clipboard.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ring.Add(clipboardring.Entry{ID: "one", Text: "from clipboard"}); err != nil {
		t.Fatal(err)
	}
	b.clipboardRing = ring
	b.scratchpadActions().open()
	if cmd := b.clipboardActions().openScratchpadBrowser(); cmd != nil {
		cmd()
	}
	if !b.clipboardMenu.Active() {
		t.Fatal("clipboard history did not open")
	}
	if cmd := b.clipboardActions().updateBrowser(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		cmd()
	}
	got, err := store.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != "from clipboard" {
		t.Fatalf("scratchpad after clipboard insert = %q", got)
	}
}

func TestScratchpadYankRecordsClipboardSource(t *testing.T) {
	b, _, _ := newScratchpadBoard(t, true)
	ring, err := clipboardring.Open(filepath.Join(t.TempDir(), "clipboard.json"))
	if err != nil {
		t.Fatal(err)
	}
	b.clipboardRing = ring
	b.editor.OpenScratchpad("copy me")
	b.scratchpadActions().handleKey(tea.KeyPressMsg{Code: tea.KeyEsc})
	b.scratchpadActions().handleKey(keyPressText("y"))
	_, cmd := b.scratchpadActions().handleKey(keyPressText("y"))
	if cmd == nil {
		t.Fatal("yy did not emit a clipboard command")
	}
	b.Update(cmd())
	entries := ring.Entries()
	if len(entries) != 1 {
		t.Fatalf("clipboard entries = %d", len(entries))
	}
	if entries[0].Text != "copy me\n" || entries[0].Source.Heading != "Scratchpad" {
		t.Fatalf("clipboard entry = %+v", entries[0])
	}
	if scratch, _ := entries[0].Metadata["scratchpad"].(bool); !scratch {
		t.Fatalf("clipboard metadata = %+v", entries[0].Metadata)
	}
}

func TestScratchpadUppercaseCTypesInInsertMode(t *testing.T) {
	b, store, root := newScratchpadBoard(t, true)
	b.scratchpadActions().open()
	b.scratchpadActions().handleKey(keyPressText("C"))
	if b.clipboardMenu.Active() {
		t.Fatal("typing C in Insert mode opened clipboard history")
	}
	if got, _ := store.Load(root); got != "C" {
		t.Fatalf("scratchpad after typing C = %q", got)
	}
}

func TestScratchpadReopensAtEnd(t *testing.T) {
	b, store, root := newScratchpadBoard(t, true)
	if err := store.Save(root, "existing"); err != nil {
		t.Fatal(err)
	}
	b.scratchpadActions().open()
	b.scratchpadActions().handleKey(keyPressText(" note"))
	if got, _ := store.Load(root); got != "existing note" {
		t.Fatalf("scratchpad resumed at wrong position: %q", got)
	}
}

func TestScratchpadBracketedPasteAutosaves(t *testing.T) {
	b, store, root := newScratchpadBoard(t, true)
	b.scratchpadActions().open()
	b.Update(tea.PasteMsg{Content: "pasted text"})
	if got, _ := store.Load(root); got != "pasted text" {
		t.Fatalf("scratchpad after bracketed paste = %q", got)
	}
}

func TestScratchpadPromotionClearsOnlyAfterCardCreation(t *testing.T) {
	b, store, root := newScratchpadBoard(t, false)
	if err := store.Save(root, "an unformed idea"); err != nil {
		t.Fatal(err)
	}
	b.scratchpadActions().open()
	model, _ := b.scratchpadActions().handleKey(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	b = model.(*Board)
	if b.editor.state != editorNew || b.editor.NewContent != "an unformed idea" {
		t.Fatalf("promotion editor = %v, content %q", b.editor.state, b.editor.NewContent)
	}
	if got, _ := store.Load(root); got != "an unformed idea" {
		t.Fatalf("scratchpad changed before card creation: %q", got)
	}

	b.editor.textinput.SetValue("promoted")
	submit, _ := b.editor.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if submit == nil {
		t.Fatal("promotion filename did not submit")
	}
	model, _ = b.Update(submit())
	b = model.(*Board)
	data, err := os.ReadFile(filepath.Join(b.columns[0].Path, "promoted.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "an unformed idea\n" {
		t.Fatalf("promoted card = %q", data)
	}
	if got, _ := store.Load(root); got != "" {
		t.Fatalf("scratchpad after promotion = %q", got)
	}
}

func TestScratchpadPromotionCancelPreservesNote(t *testing.T) {
	b, store, root := newScratchpadBoard(t, false)
	if err := store.Save(root, "keep me"); err != nil {
		t.Fatal(err)
	}
	b.scratchpadActions().open()
	b.scratchpadActions().handleKey(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	b.inputRouter().handleEditor(tea.KeyPressMsg{Code: tea.KeyEsc})
	if b.scratchPromotion != nil {
		t.Fatal("cancel left a pending promotion")
	}
	if got, _ := store.Load(root); got != "keep me" {
		t.Fatalf("cancel changed scratchpad to %q", got)
	}
}

func TestScratchpadFailedPromotionPreservesNote(t *testing.T) {
	b, store, root := newScratchpadBoard(t, false)
	if err := store.Save(root, "keep after failure"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b.columns[0].Path, "taken.md"), []byte("existing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b.scratchpadActions().open()
	b.scratchpadActions().handleKey(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	b.editor.textinput.SetValue("taken")
	submit, _ := b.editor.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	b.Update(submit())
	if b.scratchPromotion != nil {
		t.Fatal("failed creation left a pending promotion")
	}
	if got, _ := store.Load(root); got != "keep after failure" {
		t.Fatalf("failed promotion changed scratchpad to %q", got)
	}
}

func TestScratchpadVisualSelectionPromotionLeavesRemainder(t *testing.T) {
	b, store, root := newScratchpadBoard(t, true)
	if err := store.Save(root, "alpha\nbravo\ncharlie"); err != nil {
		t.Fatal(err)
	}
	b.scratchpadActions().open()
	// Scratchpad starts in Insert at the end; return to Normal, jump to the top,
	// and select the first two lines.
	b.scratchpadActions().handleKey(tea.KeyPressMsg{Code: tea.KeyEsc})
	b.scratchpadActions().handleKey(keyPressText("g"))
	b.scratchpadActions().handleKey(keyPressText("g"))
	b.scratchpadActions().handleKey(keyPressText("V"))
	b.scratchpadActions().handleKey(keyPressText("j"))
	b.scratchpadActions().handleKey(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	if b.editor.NewContent != "alpha\nbravo\n" {
		t.Fatalf("selected promotion content = %q", b.editor.NewContent)
	}
	b.editor.textinput.SetValue("selected")
	submit, _ := b.editor.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	b.Update(submit())
	if got, _ := store.Load(root); got != "charlie" {
		t.Fatalf("scratchpad remainder = %q", got)
	}
}

func TestScratchpadNeverEntersGitUntilPromotion(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	b, store, root := newScratchpadBoard(t, false)
	if err := os.WriteFile(filepath.Join(root, "Todo", "seed.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	gitRun("init", "-b", "main")
	gitRun("add", ".")
	gitRun("-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "-m", "initial")
	if err := store.Save(root, "local only"); err != nil {
		t.Fatal(err)
	}
	if status := strings.TrimSpace(gitRun("status", "--porcelain")); status != "" {
		t.Fatalf("scratchpad made repository dirty: %q", status)
	}

	b.scratchpadActions().open()
	b.scratchpadActions().handleKey(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	b.editor.textinput.SetValue("now-a-card")
	submit, _ := b.editor.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	b.Update(submit())
	status := strings.TrimSpace(gitRun("status", "--porcelain"))
	if status != "?? Todo/now-a-card.md" {
		t.Fatalf("git status after promotion = %q", status)
	}
}
