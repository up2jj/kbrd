package model

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

type boardInputRouter struct {
	board *Board
}

func (b *Board) inputRouter() boardInputRouter {
	return boardInputRouter{board: b}
}

func (r boardInputRouter) HandleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	b := r.board
	// While waiting for a sync to finish, a second Ctrl+C force-quits.
	if b.shuttingDown && key.Matches(msg, Keys.Quit) {
		b.quitting = true
		return b, tea.Quit
	}

	if layer := b.activeModalLayer(); layer != nil {
		return layer.key(msg)
	}

	return b.handleBoardKey(msg)
}

func (r boardInputRouter) handleConfigMenu(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	b := r.board
	switch {
	case key.Matches(msg, Keys.Quit):
		b.configMenuOpen = false
		return b.beginShutdown()
	case key.Matches(msg, Keys.ConfigMenuClose):
		b.configMenuOpen = false
	case key.Matches(msg, Keys.ConfigOpenLocal):
		b.configMenuOpen = false
		return b, b.managedFiles().openLocalConfig()
	case key.Matches(msg, Keys.ConfigOpenGlobal):
		b.configMenuOpen = false
		return b, b.managedFiles().openGlobalConfig()
	case key.Matches(msg, Keys.ConfigOpenLocalCommands):
		b.configMenuOpen = false
		return b, b.managedFiles().openLocalCommands()
	case key.Matches(msg, Keys.ConfigCreateLocalMCP):
		b.configMenuOpen = false
		return b, b.managedFiles().createLocalMCP()
	case key.Matches(msg, Keys.ConfigCreateLocalAgents):
		b.configMenuOpen = false
		return b, b.managedFiles().createLocalAgents()
	}
	return b, nil
}

func (r boardInputRouter) handlePeekAction(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	b := r.board
	action := itemActionID("")
	switch {
	case key.Matches(msg, Keys.Edit):
		action = actionEdit
	case key.Matches(msg, Keys.Append):
		action = actionAppend
	case key.Matches(msg, Keys.Prepend):
		action = actionPrepend
	case key.Matches(msg, Keys.Journal):
		action = actionJournal
	case key.Matches(msg, Keys.Timeline):
		action = actionTimeline
	default:
		return nil, false
	}

	b.peek.Close()
	if len(b.columns) == 0 || b.selectedCol < 0 || b.selectedCol >= len(b.columns) {
		return b.notifier.Error("item no longer exists"), true
	}
	col := b.columns[b.selectedCol]
	if !col.HasSelectedItem() {
		return b.notifier.Error("item no longer exists"), true
	}
	cmd, _ := b.itemActions().Invoke(action, actionSourcePeek)
	return cmd, true
}

func (r boardInputRouter) handleEditor(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	b := r.board
	// The textarea path treats esc as cancel (with a discard confirm when dirty);
	// the vim path handles esc itself and quits via :q/:q!.
	if b.editor.usesTextarea() && key.Matches(msg, Keys.EditorCancel) && b.editor.IsDirty() {
		b.dialog.OpenConfirmDestructive("Discard unsaved changes?", "Your edits will be lost.", "Discard", editorDiscardMsg{})
		return b, nil
	}
	cmd, _ := b.editor.Update(msg)
	if b.editor.state == editorNone {
		b.resetEditor()
	}
	return b, cmd
}

func (b *Board) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	return b.inputRouter().HandleKey(msg)
}
