package model

import tea "github.com/charmbracelet/bubbletea"

type boardMouseRouter struct {
	board *Board
}

func (b *Board) mouseRouter() boardMouseRouter {
	return boardMouseRouter{board: b}
}

func (r boardMouseRouter) HandleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	b := r.board
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
		b.switcher.Active() || b.search.Active() || b.customCmds.Active() || b.scriptUI.Active() || b.templateFlow.Active() || b.frontmatterEdit.Active() || b.zellij.Active() || b.quickCmdMode || b.zoom.Active() || len(b.columns) == 0 {
		return b, nil
	}
	if b.columns[b.selectedCol].IsFiltering() {
		return b, nil
	}

	// Only the column strip is interactive. Ignore clicks/wheel on the header, the
	// padding, and the bottom keybar so they don't select columns by X alone.
	if !b.presenter.mouseInColumns(msg.Y) {
		return b, nil
	}

	if r.handleWheel(msg) {
		return b, nil
	}

	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return b, nil
	}

	b.presenter.selectAtMouse(b, msg.X, msg.Y)
	return b, nil
}

func (r boardMouseRouter) handleWheel(msg tea.MouseMsg) bool {
	b := r.board
	if msg.Button != tea.MouseButtonWheelUp && msg.Button != tea.MouseButtonWheelDown {
		return false
	}
	if colIdx, ok := b.presenter.columnAtMouse(b, msg.X); ok {
		delta := 3
		if msg.Button == tea.MouseButtonWheelUp {
			delta = -3
		}
		b.columns[colIdx].ScrollBy(delta)
	}
	return true
}
