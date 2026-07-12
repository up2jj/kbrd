package model

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

type moveMenuActions struct {
	board *Board
}

func (b *Board) moveMenuActions() moveMenuActions { return moveMenuActions{board: b} }

func (a moveMenuActions) open(ctx itemActionContext) tea.Cmd {
	b := a.board
	if ctx.Column == nil || ctx.Column.Virtual {
		return b.notifier.Error("virtual columns cannot move cards")
	}
	if len(ctx.Targets) == 0 {
		return b.notifier.Error("no card selected")
	}

	// Keep the picker focused on the current workflow order. Virtual columns
	// are omitted because the canonical move primitive only accepts filesystem
	// columns.
	if !hasMoveDestination(ctx.ColIdx, b.columns) {
		return b.notifier.Error("no other folders available")
	}
	b.moveMenu.Open(refForColumn(ctx.Column), ctx.ColIdx, b.columns, ctx.Targets)
	return nil
}

func hasMoveDestination(sourceIndex int, columns []*Column) bool {
	for i, col := range columns {
		if i != sourceIndex && !col.Virtual {
			return true
		}
	}
	return false
}

func (a moveMenuActions) update(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	b := a.board
	if b.moveMenu.Filtering() {
		switch msg.Code {
		case tea.KeyEsc:
			b.moveMenu.StopFilter()
		case tea.KeyEnter:
			return a.runSelected()
		case tea.KeyBackspace:
			if !b.moveMenu.Backspace() {
				b.moveMenu.StopFilter()
			}
		default:
			if msg.Text != "" {
				b.moveMenu.AppendFilter(msg.Text)
			} else {
				b.moveMenu.Update(msg)
			}
		}
		b.moveMenu.recomputeForBoard(b)
		return b, nil
	}

	switch {
	case key.Matches(msg, Keys.HelpClose):
		b.moveMenu.Close()
	case msg.String() == "/":
		b.moveMenu.StartFilter()
		b.moveMenu.recomputeForBoard(b)
	case msg.Code == tea.KeyEnter:
		return a.runSelected()
	default:
		b.moveMenu.Update(msg)
	}
	return b, nil
}

func (m *MoveMenu) recomputeForBoard(b *Board) {
	source, err := b.resolveDelayedColumnRef(m.source)
	if err != nil {
		m.Close()
		return
	}
	m.recompute(m.source, b.indexOfColumn(source), b.columns)
}

func (a moveMenuActions) runSelected() (tea.Model, tea.Cmd) {
	b := a.board
	entry, ok := b.moveMenu.SelectedEntry()
	if !ok {
		return b, nil
	}
	sourceRef := b.moveMenu.source
	targets := append([]itemActionTarget(nil), b.moveMenu.targets...)
	b.moveMenu.Close()

	source, err := b.resolveDelayedColumnRef(sourceRef)
	if err != nil {
		return b, b.notifier.ErrorCause("failed to move", err)
	}
	destination, err := b.resolveDelayedColumnRef(entry.Column)
	if err != nil {
		return b, b.notifier.ErrorCause("failed to move", err)
	}
	sourceIndex := b.indexOfColumn(source)
	destinationIndex := b.indexOfColumn(destination)
	if sourceIndex < 0 || destinationIndex < 0 || sourceIndex == destinationIndex {
		return b, b.notifier.Error("move destination is no longer available")
	}
	return b, b.itemActions().moveTargets(sourceIndex, source, targets, destinationIndex, true)
}
