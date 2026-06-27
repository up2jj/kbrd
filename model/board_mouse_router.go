package model

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

const boardDoubleClickWindow = 500 * time.Millisecond

type boardMouseClickState struct {
	ref itemRefStable
	at  time.Time
}

func (s *boardMouseClickState) reset() {
	*s = boardMouseClickState{}
}

func (s *boardMouseClickState) record(ref itemRefStable, at time.Time) bool {
	if s.ref == ref && !s.at.IsZero() && at.Sub(s.at) <= boardDoubleClickWindow {
		s.reset()
		return true
	}
	s.ref = ref
	s.at = at
	return false
}

type boardMouseRouter struct {
	board *Board
}

func (b *Board) mouseRouter() boardMouseRouter {
	return boardMouseRouter{board: b}
}

func (r boardMouseRouter) HandleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	b := r.board
	resetClickState := true
	defer func() {
		if resetClickState {
			b.mouseClicks.reset()
		}
	}()

	mouse := msg.Mouse()
	if b.helpMenu.Active() {
		b.helpMenu.HandleMouse(msg)
		return b, nil
	}

	// Peek is a scrollable modal: let it consume the wheel; block everything else.
	if b.peek.Active() {
		b.peek.HandleMouse(msg)
		return b, nil
	}

	// The editor is a scrollable modal too: let the vim buffer consume the wheel.
	if b.editor.state != editorNone {
		b.editor.HandleMouse(msg)
		return b, nil
	}

	if b.git.Active() {
		return b, b.git.HandleMouse(msg)
	}

	// Zoom is excluded because click hit-testing assumes the normal multi-column
	// slot geometry and card height.
	// (peek and the editor are already handled by the early returns above.)
	if b.configMenuOpen || b.dialog.active ||
		b.switcher.Active() || b.search.Active() || b.customCmds.Active() || b.scriptUI.Active() || b.templateFlow.Active() || b.frontmatterEdit.Active() || b.zellij.Active() || b.mnemonicMode || b.zoom.Active() || len(b.columns) == 0 {
		return b, nil
	}
	if b.columns[b.selectedCol].IsFiltering() {
		return b, nil
	}

	// Terminals commonly emit release/motion events between two click presses.
	// They should not cancel the pending first click; the next actual click is
	// still re-hit-tested against the selected item before any action runs.
	switch msg.(type) {
	case tea.MouseReleaseMsg, tea.MouseMotionMsg:
		resetClickState = false
		return b, nil
	}

	// Only the columns region is interactive. Ignore clicks/wheel on the header,
	// the padding, and the bottom keybar so they don't select columns by X alone.
	width := b.termWidth
	if width == 0 {
		width = 80
	}
	header := b.statusPresenter().renderHeaderLayout(width)
	columnsRegion := boardColumnsRegion{}
	columnsRegion.measure(b, width)
	mouseYInColumns := mouse.Y - header.height
	if !columnsRegion.mouseInColumns(mouseYInColumns) {
		return b, nil
	}

	if r.handleWheel(msg, columnsRegion) {
		return b, nil
	}

	if _, ok := msg.(tea.MouseClickMsg); !ok || mouse.Button != tea.MouseLeft {
		return b, nil
	}

	colIdx, col, item, ok := columnsRegion.selectItemAtMouse(b, mouse.X, mouseYInColumns)
	if !ok || item == nil || item.Separator {
		return b, nil
	}
	resetClickState = false
	if b.mouseClicks.record(refForItem(col, item), time.Now()) {
		return b, r.handleItemDoubleClick(colIdx, col, item)
	}
	return b, nil
}

func (r boardMouseRouter) handleItemDoubleClick(colIdx int, col *Column, item *Item) tea.Cmd {
	actions := r.board.itemActions()
	if r.board.cfg.BoardItemDoubleClick == "edit" {
		return actions.edit(colIdx, col, item)
	}
	return actions.peek(col, item)
}

func (r boardMouseRouter) handleWheel(msg tea.MouseMsg, columnsRegion boardColumnsRegion) bool {
	b := r.board
	wheel, ok := msg.(tea.MouseWheelMsg)
	if !ok {
		return false
	}
	mouse := wheel.Mouse()
	if mouse.Button != tea.MouseWheelUp && mouse.Button != tea.MouseWheelDown {
		return false
	}
	if colIdx, ok := columnsRegion.columnAtMouse(b, mouse.X); ok {
		delta := 3
		if mouse.Button == tea.MouseWheelUp {
			delta = -3
		}
		b.columns[colIdx].ScrollBy(delta)
	}
	return true
}
