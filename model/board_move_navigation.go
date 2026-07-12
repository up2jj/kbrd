package model

import tea "charm.land/bubbletea/v2"

// moveMarkedByArrow turns the physical left/right arrows into adjacent-column
// moves while the focused column has marked cards. Other column-navigation
// aliases keep their normal focus-changing behavior.
func (b *Board) moveMarkedByArrow(msg tea.KeyPressMsg, col *Column) (tea.Cmd, bool) {
	if col == nil || col.Virtual || col.MarkedCount() == 0 {
		return nil, false
	}

	direction := 0
	switch msg.Code {
	case tea.KeyLeft:
		direction = -1
	case tea.KeyRight:
		direction = 1
	default:
		return nil, false
	}

	targets := targetsForItems(col, col.MarkedItems())
	if len(targets) == 0 {
		return nil, false
	}
	dstIdx := adjacentRealColumn(b.columns, b.selectedCol, direction)
	if dstIdx < 0 {
		return b.notifier.Error("no other folders available"), true
	}
	return b.itemActions().moveTargets(b.selectedCol, col, targets, dstIdx, true), true
}

func adjacentRealColumn(columns []*Column, start, direction int) int {
	if len(columns) < 2 || direction == 0 {
		return -1
	}
	for steps := 1; steps < len(columns); steps++ {
		idx := (start + direction*steps) % len(columns)
		if idx < 0 {
			idx += len(columns)
		}
		if !columns[idx].Virtual {
			return idx
		}
	}
	return -1
}
