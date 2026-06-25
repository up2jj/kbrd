package model

import (
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
)

// The swap sidecar gives the vim editor crash safety without ever auto-writing
// the real file: while editing, the buffer is flushed to a hidden dotfile beside
// the item in its column folder. A successful save (or a clean :q) clears it; a
// force-quit (:q!) or crash leaves it, so the next open can offer recovery.

// setSwapTarget derives the swap path for an edited file:
// <columnDir>/.<filename>.kbrd-swap. The dotfile name and non-".md" suffix keep
// it out of the card listing (board.Hidden / the .md filter).
func (e *Editor) setSwapTarget(fullPath string) {
	dir := filepath.Dir(fullPath)
	base := filepath.Base(fullPath)
	e.swapFile = filepath.Join(dir, "."+base+".kbrd-swap")
}

// flushSwap writes the current buffer to the swap file when there are unsaved
// changes, or removes a stale swap when the buffer matches the saved baseline.
func (e *Editor) flushSwap() {
	if e.swapFile == "" || e.buf == nil {
		return
	}
	if e.buf.Text() == e.initialValue {
		e.clearSwap()
		return
	}
	// A failed swap write means unsaved edits aren't crash-protected; surface it
	// (vimFooter) so the user knows recovery is off rather than failing silently.
	if err := os.WriteFile(e.swapFile, []byte(e.buf.Text()), 0o644); err != nil {
		e.swapWriteFailed = true
		return
	}
	e.swapWriteFailed = false
}

// clearSwap removes the swap file (after a successful save or clean quit). With
// the swap gone there is nothing left unprotected, so the warning is cleared too.
func (e *Editor) clearSwap() {
	if e.swapFile != "" {
		_ = os.Remove(e.swapFile)
	}
	e.swapWriteFailed = false
}

// openSwapCheck returns a command that prompts for recovery when a swap with
// content differing from the on-disk baseline exists, else clears any stale swap.
func (e *Editor) openSwapCheck() tea.Cmd {
	if e.swapFile == "" {
		return nil
	}
	data, err := os.ReadFile(e.swapFile)
	if err != nil {
		return nil
	}
	recovered := string(data)
	if recovered == e.initialValue {
		e.clearSwap()
		return nil
	}
	return func() tea.Msg { return recoverEditorMsg{Content: recovered} }
}

// recover seeds the buffer with recovered swap content, leaving the on-disk
// value as the dirty baseline so the editor shows unsaved and keeps the swap.
func (e *Editor) recover(content string) {
	if e.buf != nil {
		e.buf.SetText(content)
	}
}

// recoverEditorMsg asks the board to prompt the user to recover swap content.
type recoverEditorMsg struct{ Content string }

// recoverApplyMsg / recoverDiscardMsg carry the user's choice from the prompt.
type recoverApplyMsg struct{ Content string }
type recoverDiscardMsg struct{}

type boardEditorRecovery struct {
	board *Board
}

func (b *Board) editorRecovery() boardEditorRecovery {
	return boardEditorRecovery{board: b}
}

func (r boardEditorRecovery) handleRecoverEditor(msg recoverEditorMsg) (tea.Model, tea.Cmd) {
	b := r.board
	b.dialog.Open(DialogOptions{
		Title: "Recover unsaved changes?",
		Body:  "An earlier editing session left unsaved changes for this card.",
		Buttons: []DialogButton{
			{Label: "Recover", Kind: ButtonPrimary, Msg: recoverApplyMsg{Content: msg.Content}},
			{Label: "Discard", Kind: ButtonDanger, Msg: recoverDiscardMsg{}},
		},
		DefaultIndex: 0,
	})
	return b, nil
}

func (r boardEditorRecovery) handleRecoverApply(msg recoverApplyMsg) (tea.Model, tea.Cmd) {
	b := r.board
	b.editor.recover(msg.Content)
	return b, nil
}

func (r boardEditorRecovery) handleRecoverDiscard() (tea.Model, tea.Cmd) {
	b := r.board
	b.editor.clearSwap()
	return b, nil
}
