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

	if layer := b.activeModalLayer(); layer != nil {
		if layer.mouse != nil {
			return b, layer.mouse(msg)
		}
		return b, nil
	}

	mouse := msg.Mouse()

	// Zoom is excluded because click hit-testing assumes the normal multi-column
	// slot geometry and card height. Active modals were handled above.
	if b.zoom.Active() || len(b.columns) == 0 {
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
	columnsCtx := b.columnsRegionContext()
	columnsRegion := boardColumnsRegion{}
	columnsRegion.measure(columnsCtx, width)
	mouseYInColumns := mouse.Y - header.height
	if !columnsRegion.mouseInColumns(mouseYInColumns) {
		return b, nil
	}

	if r.handleWheel(msg, columnsCtx, columnsRegion) {
		return b, nil
	}

	if _, ok := msg.(tea.MouseClickMsg); !ok || mouse.Button != tea.MouseLeft {
		return b, nil
	}

	_, col, item, ok := columnsRegion.selectItemAtMouse(columnsCtx, mouse.X, mouseYInColumns)
	if !ok || item == nil || item.Separator {
		return b, nil
	}
	resetClickState = false
	if b.mouseClicks.record(refForItem(col, item), time.Now()) {
		return b, r.handleItemDoubleClick()
	}
	return b, nil
}

func (r boardMouseRouter) handleItemDoubleClick() tea.Cmd {
	if r.board.cfg.BoardItemDoubleClick == "edit" {
		cmd, _ := r.board.itemActions().Invoke(actionEdit, actionSourceMouse)
		return cmd
	}
	cmd, _ := r.board.itemActions().Invoke(actionPeek, actionSourceMouse)
	return cmd
}

func (r boardMouseRouter) handleWheel(msg tea.MouseMsg, columnsCtx boardColumnsRegionContext, columnsRegion boardColumnsRegion) bool {
	b := r.board
	wheel, ok := msg.(tea.MouseWheelMsg)
	if !ok {
		return false
	}
	mouse := wheel.Mouse()
	if mouse.Button != tea.MouseWheelUp && mouse.Button != tea.MouseWheelDown {
		return false
	}
	if colIdx, ok := columnsRegion.columnAtMouse(columnsCtx, mouse.X); ok {
		delta := 3
		if mouse.Button == tea.MouseWheelUp {
			delta = -3
		}
		b.columns[colIdx].ScrollBy(delta)
	}
	return true
}
