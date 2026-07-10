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

	// The interactive keybindings menu owns all input while open.
	if b.helpMenu.Active() {
		return b.helpActions().update(msg)
	}

	if b.configMenuOpen {
		return r.handleConfigMenu(msg)
	}
	if b.dialog.active {
		return b, b.dialog.Update(msg)
	}

	// Custom commands and script UI can be layered over an editor.
	if b.customCmds.Active() {
		return b, b.customCmds.Update(msg)
	}
	if b.pasteMenu.Active() {
		return b, b.pasteMenu.Update(msg)
	}
	if b.scriptUI.Active() {
		return b, b.scriptUI.Update(msg)
	}

	if b.editor.state != editorNone {
		return r.handleEditor(msg)
	}
	if b.peek.Active() {
		if cmd, handled := r.handlePeekAction(msg); handled {
			return b, cmd
		}
		b.peek.Update(msg)
		return b, nil
	}
	if b.switcher.Active() {
		return b, b.switcher.Update(msg)
	}
	if b.search.Active() {
		return b, b.search.HandleKey(msg)
	}
	if b.templateMenu.Active() {
		return b.templateMenuActions().update(msg)
	}
	if b.templateFlow.Active() {
		cmd := b.templateFlow.Update(msg)
		if !b.templateFlow.Active() {
			b.clipboardActions().cancelTemplateRead()
		}
		return b, cmd
	}
	if b.frontmatterEdit.Active() {
		return b, b.frontmatterEdit.Update(msg)
	}
	if b.git.Active() {
		return b, b.git.HandleKey(msg)
	}
	if b.zellij.Active() {
		return b, b.zellij.Update(msg)
	}
	if b.mnemonic.active {
		return b.mnemonicSelector().handleKey(msg)
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
