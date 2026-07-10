package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"kbrd/config"
	"kbrd/script"
	"kbrd/vimbuf"
)

func runeKey(r rune) tea.KeyPressMsg { return keyPressText(string(r)) }

// feedRunes sends each rune of s as a separate key to the editor.
func feedRunes(e *Editor, s string) {
	for _, r := range s {
		e.Update(runeKey(r))
	}
}

// vimMsg sends one key and returns the message its command produced (if any).
func vimMsg(e *Editor, k tea.KeyPressMsg) tea.Msg {
	cmd, _ := e.Update(k)
	if cmd != nil {
		return cmd()
	}
	return nil
}

func openVimEdit(t *testing.T, content string) (*Editor, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	e := NewEditor(true)
	e.SetSize(120, 40)
	e.OpenEdit(0, "", "note", path)
	return e, path
}

// In the vim path, editing happens through the buffer and ctrl+s / :w emit a
// save message carrying the new content.
func TestVimEditAndSave(t *testing.T) {
	e, _ := openVimEdit(t, "hello")
	// Normal mode: A appends at EOL, type, esc.
	e.Update(runeKey('A'))
	feedRunes(e, " world")
	e.Update(tea.KeyPressMsg{Code: tea.KeyEsc})

	if got := e.buf.Text(); got != "hello world" {
		t.Fatalf("buffer = %q, want %q", got, "hello world")
	}
	if !e.IsDirty() {
		t.Fatalf("editor should be dirty after edit")
	}

	// :w saves (and stays open for an edit).
	e.Update(runeKey(':'))
	e.Update(runeKey('w'))
	msg := vimMsg(e, tea.KeyPressMsg{Code: tea.KeyEnter})
	save, ok := msg.(editorSaveMsg)
	if !ok {
		t.Fatalf(":w produced %T, want editorSaveMsg", msg)
	}
	if save.Content != "hello world" {
		t.Fatalf("save content = %q", save.Content)
	}
	if e.state != editorEdit {
		t.Fatalf(":w should keep the edit open, state = %v", e.state)
	}
	// The dirty baseline is only reset once the write is confirmed (the board
	// calls confirmSaved after a successful ReplaceFileContent); until then the
	// buffer stays dirty so a failed write can't masquerade as clean.
	if !e.IsDirty() {
		t.Fatalf(":w must stay dirty until the save is confirmed")
	}
	e.confirmSaved()
	if e.IsDirty() {
		t.Fatalf("confirmSaved should reset the dirty baseline")
	}
	if e.state != editorEdit {
		t.Fatalf("confirmSaved on :w should keep the edit open, state = %v", e.state)
	}
}

func TestVimOpenSavePreservesTrailingNewlines(t *testing.T) {
	e, _ := openVimEdit(t, "hello\n\n")
	if got := e.buf.Text(); got != "hello\n\n" {
		t.Fatalf("opened buffer = %q, want exact trailing newlines", got)
	}
	if e.IsDirty() {
		t.Fatalf("editor should not be dirty immediately after opening exact file text")
	}

	msg := vimMsg(e, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	save, ok := msg.(editorSaveMsg)
	if !ok {
		t.Fatalf("ctrl+s produced %T, want editorSaveMsg", msg)
	}
	if save.Content != "hello\n\n" {
		t.Fatalf("save content = %q, want exact trailing newlines", save.Content)
	}
}

func TestVimManagedFilePreservesTrailingNewlines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.md")
	if err := os.WriteFile(path, []byte("alpha\n\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	e := NewEditor(true)
	e.SetSize(120, 40)
	e.OpenManagedFile("config", path)
	if got := e.buf.Text(); got != "alpha\n\n" {
		t.Fatalf("managed buffer = %q, want exact trailing newlines", got)
	}
	msg := vimMsg(e, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	save, ok := msg.(managedFileSaveMsg)
	if !ok {
		t.Fatalf("ctrl+s produced %T, want managedFileSaveMsg", msg)
	}
	if save.Content != "alpha\n\n" {
		t.Fatalf("save content = %q, want exact trailing newlines", save.Content)
	}
}

func TestVimViewScrollbarUsesReservedRightLane(t *testing.T) {
	lines := make([]string, 40)
	for i := range lines {
		lines[i] = "x"
	}
	e, _ := openVimEdit(t, strings.Join(lines, "\n"))

	out := ansi.Strip(e.View())
	scrollbarX := 1 + overlayPadH + e.buf.Width() - 1
	foundThumb := false
	for line := range strings.SplitSeq(out, "\n") {
		x := runeIndex(line, '█')
		if x < 0 {
			continue
		}
		foundThumb = true
		if x != scrollbarX {
			t.Fatalf("vim scrollbar thumb x = %d, want %d in line %q", x, scrollbarX, line)
		}
	}
	if !foundThumb {
		t.Fatal("vim scrollbar thumb was not rendered")
	}
}

func TestVimCommandPasteDoesNotMutateBuffer(t *testing.T) {
	e, _ := openVimEdit(t, "hello")
	e.Update(runeKey(':'))
	e.Update(tea.PasteMsg{Content: "w"})
	if got := e.buf.Text(); got != "hello" {
		t.Fatalf("command paste mutated buffer: %q", got)
	}
	if got := e.buf.CommandLine(); got != "w" {
		t.Fatalf("command line after paste = %q, want w", got)
	}
	msg := vimMsg(e, tea.KeyPressMsg{Code: tea.KeyEnter})
	if _, ok := msg.(editorSaveMsg); !ok {
		t.Fatalf("pasted :w produced %T, want editorSaveMsg", msg)
	}
}

func TestVimPasteClipboardUsesCapturedOSC52Content(t *testing.T) {
	e, _ := openVimEdit(t, "hello")
	e.PasteClipboard(" world")
	if got := e.buf.Text(); got != " worldhello" {
		t.Fatalf("normal-mode clipboard paste = %q, want insertion at cursor", got)
	}
}

// :q on a dirty buffer refuses (stays open with a hint); :q! quits.
func TestVimQuitGuard(t *testing.T) {
	e, _ := openVimEdit(t, "hello")
	e.Update(runeKey('x')) // delete a char -> dirty

	e.Update(runeKey(':'))
	e.Update(runeKey('q'))
	e.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if e.state == editorNone {
		t.Fatalf(":q on a dirty buffer must not close")
	}

	// :q! force-quits.
	e.Update(runeKey(':'))
	e.Update(runeKey('q'))
	e.Update(runeKey('!'))
	e.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if e.state != editorNone {
		t.Fatalf(":q! should close, state = %v", e.state)
	}
}

// :lua <expr> emits an editorEvalMsg for the board to evaluate.
func TestVimLuaEvalMsg(t *testing.T) {
	e, _ := openVimEdit(t, "hello")
	e.Update(runeKey(':'))
	feedRunes(e, "lua up(line)")
	msg := vimMsg(e, tea.KeyPressMsg{Code: tea.KeyEnter})
	ev, ok := msg.(editorEvalMsg)
	if !ok {
		t.Fatalf(":lua produced %T, want editorEvalMsg", msg)
	}
	if ev.Expr != "up(line)" || ev.Range != nil {
		t.Fatalf("eval msg = %+v", ev)
	}
}

func TestVimLuaCompletionsFromBoardScripts(t *testing.T) {
	dir := t.TempDir()
	col := filepath.Join(dir, "Todo")
	if err := os.MkdirAll(col, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(col, "note.md")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".kbrd.lua"), []byte(`
kbrd.register("indent", function() return "" end, "indent(n)")
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{Path: dir, ColumnWidth: 32, PreviewLines: 3, Editor: config.EditorConfig{Vim: true}}
	cfg.Scripting = config.ScriptingConfig{Enabled: true, CommandTimeoutMs: 2000, HookTimeoutMs: 500, InstructionLimit: 10_000_000}
	b := NewBoard(cfg)
	if b.scripts != nil {
		t.Fatal("precondition: scripts should initialize after board construction")
	}
	b.initRuntime()
	if b.scripts == nil {
		t.Fatal("scripting host not initialized")
	}

	b.editor.OpenEdit(0, col, "note", path)
	b.editor.Update(runeKey(':'))
	feedRunes(b.editor, "lua ind")
	b.editor.Update(tea.KeyPressMsg{Code: tea.KeyTab})

	if got := b.editor.buf.CommandLine(); got != "lua indent" {
		t.Fatalf("cmdline after completion = %q, want %q", got, "lua indent")
	}
	if name, usage := b.editor.buf.CompletionHint(); name != "indent" || usage != "indent(n)" {
		t.Fatalf("completion hint = %q/%q, want indent/indent(n)", name, usage)
	}
}

// Editing writes a swap sidecar; a fresh open finds it and offers recovery.
func TestVimSwapRecovery(t *testing.T) {
	e, path := openVimEdit(t, "hello")
	e.Update(runeKey('x')) // delete -> dirty -> flushSwap

	swap := filepath.Join(filepath.Dir(path), ".note.md.kbrd-swap")
	if _, err := os.Stat(swap); err != nil {
		t.Fatalf("swap not written: %v", err)
	}

	// Reopen the (unchanged-on-disk) file: openSwapCheck should offer recovery.
	e2 := NewEditor(true)
	e2.SetSize(120, 40)
	cmd := e2.OpenEdit(0, "", "note", path)
	if cmd == nil {
		t.Fatalf("expected a recovery command on reopen")
	}
	msg := cmd()
	rec, ok := msg.(recoverEditorMsg)
	if !ok {
		t.Fatalf("reopen produced %T, want recoverEditorMsg", msg)
	}
	if rec.Content != "ello" {
		t.Fatalf("recovered content = %q, want %q", rec.Content, "ello")
	}

	// Applying recovery seeds the buffer; a successful save clears the swap.
	e2.recover(rec.Content)
	if e2.buf.Text() != "ello" {
		t.Fatalf("after recover, buffer = %q", e2.buf.Text())
	}
	e2.clearSwap()
	if _, err := os.Stat(swap); !os.IsNotExist(err) {
		t.Fatalf("swap should be cleared, stat err = %v", err)
	}
}

func TestVimSwapSkipsMovementAndClearsAfterUndoToClean(t *testing.T) {
	e, path := openVimEdit(t, "hello")
	e.Update(runeKey('x')) // delete -> dirty -> flushSwap
	swap := filepath.Join(filepath.Dir(path), ".note.md.kbrd-swap")
	if _, err := os.Stat(swap); err != nil {
		t.Fatalf("swap not written: %v", err)
	}
	writtenRev := e.lastSwapRevision
	if writtenRev == 0 {
		t.Fatal("swap write should record the written revision")
	}

	e.Update(runeKey('l')) // movement must not flush a duplicate swap
	if e.lastSwapRevision != writtenRev {
		t.Fatalf("movement rewrote swap revision: got %d, want %d", e.lastSwapRevision, writtenRev)
	}

	e.Update(runeKey('u')) // undo restores the clean snapshot revision
	if e.IsDirty() {
		t.Fatal("undo back to opened content should clear dirty state")
	}
	if _, err := os.Stat(swap); !os.IsNotExist(err) {
		t.Fatalf("clean undo should clear swap, stat err = %v", err)
	}
}

// esc from Normal mode closes a clean editor; from Insert it returns to Normal.
func TestVimEscCloses(t *testing.T) {
	e, _ := openVimEdit(t, "hello")
	esc := tea.KeyPressMsg{Code: tea.KeyEsc}

	// From Insert: esc returns to Normal, does not close.
	e.Update(runeKey('i'))
	e.Update(esc)
	if e.state == editorNone {
		t.Fatalf("esc from insert should not close the editor")
	}
	if e.buf.Mode() != 0 { // ModeNormal
		t.Fatalf("esc from insert should return to Normal, mode=%v", e.buf.Mode())
	}

	// From Normal (clean): esc closes.
	e.Update(esc)
	if e.state != editorNone {
		t.Fatalf("esc from Normal (clean) should close, state=%v", e.state)
	}
}

// esc from Normal on a dirty buffer asks for discard confirmation instead of closing.
func TestVimEscDirtyPrompts(t *testing.T) {
	e, _ := openVimEdit(t, "hello")
	e.Update(runeKey('x')) // dirty
	msg := vimMsg(e, tea.KeyPressMsg{Code: tea.KeyEsc})
	if _, ok := msg.(editorConfirmDiscardMsg); !ok {
		t.Fatalf("esc on dirty buffer produced %T, want editorConfirmDiscardMsg", msg)
	}
	if e.state == editorNone {
		t.Fatalf("editor should stay open until the discard is confirmed")
	}
}

// A write that fails (board.ReplaceFileContent errors) must leave the vim editor
// open, still dirty, with its recovery swap intact — otherwise :q could silently
// discard the unsaved buffer.
func TestVimSaveFailureKeepsDirtyAndSwap(t *testing.T) {
	dir := t.TempDir()
	col := filepath.Join(dir, "Todo")
	os.MkdirAll(col, 0o755)
	path := filepath.Join(col, "note.md")
	os.WriteFile(path, []byte("original"), 0o644)

	b := NewBoard(config.Config{Path: dir, ColumnWidth: 32, PreviewLines: 3, Editor: config.EditorConfig{Vim: true}})
	if err := b.loadColumns(); err != nil {
		t.Fatalf("loadColumns: %v", err)
	}
	b.termWidth, b.termHeight = 120, 40
	b.editor.SetSize(120, 40)
	b.editor.OpenEdit(0, b.columns[0].Path, "note", path)

	b.editor.buf.HandleKey("x") // delete a char -> dirty
	b.editor.flushSwap()
	if !b.editor.IsDirty() {
		t.Fatal("editor should be dirty after an edit")
	}
	swap := b.editor.swapFile
	if swap == "" {
		t.Fatal("expected a swap file to be set for an editorEdit")
	}
	if _, err := os.Stat(swap); err != nil {
		t.Fatalf("swap file should exist after flush: %v", err)
	}

	// Delete the underlying file so the existing-only ReplaceFileContent fails.
	os.Remove(path)
	b.mutationHandlers().handleSave(editorSaveMsg{Target: b.editor.itemTarget(), ColIndex: 0, FileName: "note", Content: b.editor.buf.Text()})

	if b.editor.state == editorNone {
		t.Fatal("failed save must not close the editor")
	}
	if !b.editor.IsDirty() {
		t.Fatal("failed save must leave the editor dirty (not silently clean)")
	}
	if _, err := os.Stat(swap); err != nil {
		t.Fatalf("failed save must keep the recovery swap, got: %v", err)
	}
}

// kbrd.editor.open must not replace an editor that has unsaved changes: the open
// request is refused (editor stays on the dirty card) rather than silently
// discarding the buffer.
func TestEditorOpenRefusesWhenDirty(t *testing.T) {
	dir := t.TempDir()
	col := filepath.Join(dir, "Todo")
	os.MkdirAll(col, 0o755)
	os.WriteFile(filepath.Join(col, "note.md"), []byte("note body"), 0o644)
	os.WriteFile(filepath.Join(col, "other.md"), []byte("other body"), 0o644)
	os.WriteFile(filepath.Join(dir, ".kbrd.lua"), []byte(""), 0o644)

	cfg := config.Config{Path: dir, ColumnWidth: 32, PreviewLines: 3, Editor: config.EditorConfig{Vim: true}}
	cfg.Scripting = config.ScriptingConfig{Enabled: true, CommandTimeoutMs: 2000, HookTimeoutMs: 500, InstructionLimit: 10_000_000}
	b := NewBoard(cfg)
	b.initRuntime()
	if b.scripts == nil {
		t.Fatal("scripting host not initialized")
	}
	if err := b.loadColumns(); err != nil {
		t.Fatalf("loadColumns: %v", err)
	}
	b.termWidth, b.termHeight = 120, 40
	b.editor.SetSize(120, 40)

	b.editor.OpenEdit(0, col, "note", filepath.Join(col, "note.md"))
	b.editor.buf.HandleKey("x") // dirty
	if !b.editor.IsDirty() {
		t.Fatal("editor should be dirty after an edit")
	}

	if _, _, err := b.scripts.Eval(`kbrd.editor.open("other.md")`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	b.collectEditorOpenCmd() // would replace the editor if it didn't refuse

	if b.editor.FileName != "note" {
		t.Fatalf("dirty editor was replaced: now editing %q, want note", b.editor.FileName)
	}
	if !b.editor.IsDirty() {
		t.Fatal("refused open must leave the original buffer dirty")
	}
}

// A failed swap write must raise the crash-recovery-off warning, and a later
// successful flush must clear it.
func TestSwapWriteFailureWarns(t *testing.T) {
	dir := t.TempDir()
	col := filepath.Join(dir, "Todo")
	os.MkdirAll(col, 0o755)
	path := filepath.Join(col, "note.md")
	os.WriteFile(path, []byte("body"), 0o644)

	e := NewEditor(true)
	e.SetSize(120, 40)
	e.OpenEdit(0, "", "note", path)
	e.buf.HandleKey("x") // dirty so flushSwap actually writes

	// Point the swap at a path under a missing directory so the write fails.
	e.swapFile = filepath.Join(dir, "no-such-dir", "x.kbrd-swap")
	e.flushSwap()
	if !e.swapWriteFailed {
		t.Fatal("failed swap write should set swapWriteFailed")
	}

	// A subsequent successful flush clears the warning.
	e.swapFile = filepath.Join(col, ".note.md.kbrd-swap")
	e.flushSwap()
	if e.swapWriteFailed {
		t.Fatal("successful swap write should clear swapWriteFailed")
	}
}

// A KeyRunes message that batches several runes (fast typing, IME commit, or an
// unbracketed paste) must insert them all, not drop the chunk. Regression for the
// per-key insert handler ignoring multi-rune keys, which made the first burst of
// typed text "disappear" in the journal/insert-mode editor.
func TestVimMultiRuneKeyInserts(t *testing.T) {
	e := NewEditor(true)
	e.SetSize(120, 40)
	e.OpenAppend(0, "", "", "note") // additive state opens in insert mode
	if e.buf.Mode() != vimbuf.ModeInsert {
		t.Fatalf("expected insert mode, got %v", e.buf.Mode())
	}

	e.Update(keyPressText("Asia"))
	if got := e.buf.Text(); got != "Asia" {
		t.Fatalf("multi-rune key dropped text: got %q want Asia", got)
	}
	// A following single-rune key keeps working.
	e.Update(keyPressText("!"))
	if got := e.buf.Text(); got != "Asia!" {
		t.Fatalf("got %q want Asia!", got)
	}
}

// End-to-end of the reported bug: a journal entry typed as a batched multi-rune
// burst (e.g. "Asia" arriving while the editor is mid-render) followed by a space
// must reach the save message with its capitalization intact — it was being
// dropped/garbled to "asia" before the multi-rune insert fix.
func TestVimJournalMultiRunePreservesCase(t *testing.T) {
	e := NewEditor(true)
	e.SetSize(120, 40)
	e.OpenJournal(0, "", "", "note") // journal opens in insert mode
	if e.buf.Mode() != vimbuf.ModeInsert {
		t.Fatalf("expected insert mode, got %v", e.buf.Mode())
	}

	e.Update(keyPressText("Asia"))
	e.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	if got := e.buf.Text(); got != "Asia " {
		t.Fatalf("buffer = %q, want %q", got, "Asia ")
	}

	// ctrl+s emits the journal save message carrying the buffer text verbatim;
	// board.DetectDate then preserves the remainder's case (see TestDetectDate).
	msg := vimMsg(e, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	j, ok := msg.(editorJournalMsg)
	if !ok {
		t.Fatalf("save produced %T, want editorJournalMsg", msg)
	}
	if j.Text != "Asia " {
		t.Fatalf("journal text = %q, want %q", j.Text, "Asia ")
	}
}

// resolveEditorTarget finds an item by path/name; GoToLine positions the cursor.
func TestEditorOpenResolveAndGoToLine(t *testing.T) {
	dir := t.TempDir()
	col := filepath.Join(dir, "Todo")
	os.MkdirAll(col, 0o755)
	path := filepath.Join(col, "note.md")
	os.WriteFile(path, []byte("l1\nl2\nl3\nl4\nl5"), 0o644)

	b := NewBoard(config.Config{Path: dir, ColumnWidth: 32, PreviewLines: 3, Editor: config.EditorConfig{Vim: true}})
	if err := b.loadColumns(); err != nil {
		t.Fatalf("loadColumns: %v", err)
	}
	b.termWidth, b.termHeight = 120, 40
	b.editor.SetSize(120, 40)

	ci, item := b.resolveEditorTarget(script.EditorOpenReq{Path: "note.md"})
	if item == nil || ci != 0 || item.Name != "note" {
		t.Fatalf("resolve by basename: ci=%d item=%v", ci, item)
	}

	b.editor.OpenEdit(ci, b.columns[ci].Path, item.Name, item.FullPath)
	b.editor.GoToLine(4)
	if got := b.editor.buf.Cursor().Row; got != 3 {
		t.Fatalf("GoToLine(4) row = %d, want 3", got)
	}
}

func TestEditorOpenPathLikeInputDoesNotFallbackToBasename(t *testing.T) {
	dir := t.TempDir()
	col := filepath.Join(dir, "Todo")
	if err := os.MkdirAll(col, 0o755); err != nil {
		t.Fatal(err)
	}
	currentPath := filepath.Join(col, "todo.md")
	if err := os.WriteFile(currentPath, []byte("current"), 0o644); err != nil {
		t.Fatal(err)
	}

	staleBoard := filepath.Join(t.TempDir(), "other-board")
	staleCol := filepath.Join(staleBoard, "Todo")
	if err := os.MkdirAll(staleCol, 0o755); err != nil {
		t.Fatal(err)
	}
	stalePath := filepath.Join(staleCol, "todo.md")
	if err := os.WriteFile(stalePath, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	b := NewBoard(config.Config{Path: dir, ColumnWidth: 32, PreviewLines: 3, Editor: config.EditorConfig{Vim: true}})
	if err := b.loadColumns(); err != nil {
		t.Fatalf("loadColumns: %v", err)
	}

	if ci, item := b.resolveEditorTarget(script.EditorOpenReq{Path: stalePath}); item != nil || ci != -1 {
		t.Fatalf("stale absolute path resolved by basename: ci=%d item=%+v", ci, item)
	}
	if ci, item := b.resolveEditorTarget(script.EditorOpenReq{Path: filepath.Join("Archive", "todo.md")}); item != nil || ci != -1 {
		t.Fatalf("missing relative path resolved by basename: ci=%d item=%+v", ci, item)
	}
	if ci, item := b.resolveEditorTarget(script.EditorOpenReq{Path: filepath.Join("Todo", "todo.md")}); item == nil || ci != 0 || item.FullPath != currentPath {
		t.Fatalf("relative current-board path: ci=%d item=%+v, want %q", ci, item, currentPath)
	}
	if ci, item := b.resolveEditorTarget(script.EditorOpenReq{Path: "todo.md"}); item == nil || ci != 0 || item.FullPath != currentPath {
		t.Fatalf("bare basename fallback: ci=%d item=%+v, want %q", ci, item, currentPath)
	}
}
