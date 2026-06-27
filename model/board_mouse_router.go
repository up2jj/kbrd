package model

import tea "charm.land/bubbletea/v2"

type boardMouseRouter struct {
	board *Board
}

func (b *Board) mouseRouter() boardMouseRouter {
	return boardMouseRouter{board: b}
}

func (r boardMouseRouter) HandleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	b := r.board
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

	// Only the column strip is interactive. Ignore clicks/wheel on the header, the
	// padding, and the bottom keybar so they don't select columns by X alone.
	if !b.presenter.mouseInColumns(mouse.Y) {
		return b, nil
	}

	if r.handleWheel(msg) {
		return b, nil
	}

	if _, ok := msg.(tea.MouseClickMsg); !ok || mouse.Button != tea.MouseLeft {
		return b, nil
	}

	b.presenter.selectAtMouse(b, mouse.X, mouse.Y)
	return b, nil
}

func (r boardMouseRouter) handleWheel(msg tea.MouseMsg) bool {
	b := r.board
	wheel, ok := msg.(tea.MouseWheelMsg)
	if !ok {
		return false
	}
	mouse := wheel.Mouse()
	if mouse.Button != tea.MouseWheelUp && mouse.Button != tea.MouseWheelDown {
		return false
	}
	if colIdx, ok := b.presenter.columnAtMouse(b, mouse.X); ok {
		delta := 3
		if mouse.Button == tea.MouseWheelUp {
			delta = -3
		}
		b.columns[colIdx].ScrollBy(delta)
	}
	return true
}
