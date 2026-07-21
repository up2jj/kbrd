package model

import (
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"

	"kbrd/clipboardring"
	"kbrd/scratchpad"
	"kbrd/vimbuf"
)

type scratchpadPromotion struct {
	boardPath string
	remainder string
}

type boardScratchpadActions struct {
	board *Board
}

func (b *Board) scratchpadActions() boardScratchpadActions {
	return boardScratchpadActions{board: b}
}

func (a boardScratchpadActions) store() (*scratchpad.Store, error) {
	b := a.board
	if b.scratchpads != nil {
		return b.scratchpads, nil
	}
	store, err := scratchpad.Open("")
	if err != nil {
		return nil, err
	}
	b.scratchpads = store
	return store, nil
}

func (a boardScratchpadActions) open() (tea.Model, tea.Cmd) {
	b := a.board
	store, err := a.store()
	if err != nil {
		return b, b.notifier.ErrorCause("open scratchpad", err)
	}
	content, err := store.Load(b.cfg.Path)
	if err != nil {
		return b, b.notifier.ErrorCause("open scratchpad", err)
	}
	return b, b.editor.OpenScratchpad(content)
}

func (a boardScratchpadActions) appendSelectedCard() (tea.Model, tea.Cmd) {
	b := a.board
	if len(b.columns) == 0 || b.selectedCol < 0 || b.selectedCol >= len(b.columns) {
		return b, b.notifier.Error("no card selected")
	}
	col := b.columns[b.selectedCol]
	if col.Virtual {
		return b, b.notifier.ErrorCause("append to scratchpad", errVirtualColumn)
	}
	item := col.SelectedItem()
	if item == nil || item.Separator || item.FullPath == "" {
		return b, b.notifier.Error("no card selected")
	}
	data, err := os.ReadFile(item.FullPath)
	if err != nil {
		return b, b.notifier.ErrorCause("read selected card", err)
	}
	store, err := a.store()
	if err != nil {
		return b, b.notifier.ErrorCause("open scratchpad", err)
	}
	content, err := store.Append(b.cfg.Path, strings.TrimRight(string(data), "\n"))
	if err != nil {
		return b, b.notifier.ErrorCause("append to scratchpad", err)
	}
	return b, tea.Batch(b.editor.OpenScratchpad(content), b.notifier.Success("added "+item.Name+" to scratchpad"))
}

func (a boardScratchpadActions) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	b := a.board
	key := msg.String()
	switch {
	case key == "ctrl+v":
		return b, b.clipboardActions().readEditor()
	case key == "ctrl+n":
		return a.promote()
	case key == "ctrl+c":
		return b, a.copy()
	case key == "ctrl+g" || (key == "C" && b.editor.vim && b.editor.buf != nil && b.editor.buf.Mode() != vimbuf.ModeInsert && b.editor.buf.Mode() != vimbuf.ModeCommand):
		return b, b.clipboardActions().openScratchpadBrowser()
	}

	return a.updateEditor(msg)
}

func (a boardScratchpadActions) updateEditor(msg tea.Msg) (tea.Model, tea.Cmd) {
	b := a.board
	before := b.editor.ScratchpadContent()
	cmd, _ := b.editor.Update(msg)
	after := b.editor.ScratchpadContent()
	if b.editor.IsScratchpad() && after != before {
		cmd = batchCmd(cmd, a.save(after))
	}
	if b.editor.state == editorNone {
		b.resetEditor()
	}
	return b, cmd
}

func (a boardScratchpadActions) save(content string) tea.Cmd {
	b := a.board
	store, err := a.store()
	if err != nil {
		return b.notifier.ErrorCause("save scratchpad", err)
	}
	if err := store.Save(b.cfg.Path, content); err != nil {
		return b.notifier.ErrorCause("save scratchpad", err)
	}
	b.editor.confirmScratchpadSaved()
	return nil
}

func (a boardScratchpadActions) handleSave(msg scratchpadSaveMsg) (tea.Model, tea.Cmd) {
	b := a.board
	cmd := a.save(msg.Content)
	if cmd == nil {
		b.editor.confirmSaved()
	}
	if b.editor.state == editorNone {
		b.resetEditor()
	}
	return b, cmd
}

func (a boardScratchpadActions) insertClipboard(text string) tea.Cmd {
	b := a.board
	if !b.editor.IsScratchpad() || text == "" {
		return nil
	}
	b.editor.InsertText(text)
	return a.save(b.editor.ScratchpadContent())
}

func (a boardScratchpadActions) copy() tea.Cmd {
	b := a.board
	content := b.editor.SelectedOrAllText()
	if content == "" {
		return b.notifier.Error("scratchpad is empty")
	}
	_ = clipboard.WriteAll(content)
	return func() tea.Msg {
		return editorYankMsg{content: content, scratchpad: true}
	}
}

func (a boardScratchpadActions) promote() (tea.Model, tea.Cmd) {
	b := a.board
	if len(b.columns) == 0 || b.selectedCol < 0 || b.selectedCol >= len(b.columns) {
		return b, b.notifier.Error("no destination column")
	}
	col := b.columns[b.selectedCol]
	if col.Virtual {
		return b, b.notifier.ErrorCause("promote scratchpad", errVirtualColumn)
	}
	content, remainder := b.editor.TakePromotion()
	if strings.TrimSpace(content) == "" {
		return b, b.notifier.Error("scratchpad selection is empty")
	}
	b.scratchPromotion = &scratchpadPromotion{boardPath: b.cfg.Path, remainder: remainder}
	return b, b.editor.OpenNewWithContent(b.selectedCol, col.Name, col.Path, content)
}

func (a boardScratchpadActions) finishPromotion() error {
	b := a.board
	pending := b.scratchPromotion
	if pending == nil {
		return nil
	}
	b.scratchPromotion = nil
	store, err := a.store()
	if err != nil {
		return err
	}
	return store.Save(pending.boardPath, pending.remainder)
}

func (a boardScratchpadActions) cancelPromotion() {
	a.board.scratchPromotion = nil
}

func (a boardScratchpadActions) clipboardSource() clipboardring.Source {
	b := a.board
	name := b.cfg.BoardName
	if name == "" {
		name = b.boardLabel()
	}
	return clipboardring.Source{Board: name, Heading: "Scratchpad"}
}
