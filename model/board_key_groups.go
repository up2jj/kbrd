package model

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

func (b *Board) handleGlobalBoardKey(msg tea.KeyMsg, col *Column) (tea.Model, tea.Cmd, bool) {
	switch {
	case key.Matches(msg, Keys.Quit):
		m, cmd := b.beginShutdown()
		return m, cmd, true
	case key.Matches(msg, Keys.ToggleHelp):
		b.loadCommands()
		b.helpActions().open()
		return b, nil, true
	case key.Matches(msg, Keys.ConfigMenu):
		b.configMenuOpen = true
		return b, nil, true
	case key.Matches(msg, Keys.QuickCmd):
		return b, b.quickCommands().open(), true
	case key.Matches(msg, Keys.SwitchBoard):
		return b, b.session().openSwitcher(), true
	case key.Matches(msg, Keys.Search):
		return b, b.searchActions().openSearch(), true
	case key.Matches(msg, Keys.GitPanel):
		return b, b.git.Open(), true
	case b.zellij.Enabled && key.Matches(msg, Keys.ZellijMenu):
		b.zellij.OpenFor(b.cfg.Path, col)
		return b, nil, true
	case key.Matches(msg, Keys.Refresh):
		return b, b.utilityActions().refresh(), true
	}
	return b, nil, false
}

func (b *Board) handleColumnBoardKey(msg tea.KeyMsg, col *Column) (tea.Model, tea.Cmd, bool) {
	if m, cmd, handled := b.handleColumnActionKey(msg, col); handled {
		return m, cmd, true
	}
	if m, cmd, handled := b.handleColumnDisplayKey(msg, col); handled {
		return m, cmd, true
	}
	if m, cmd, handled := b.handleColumnNavigationKey(msg, col); handled {
		return m, cmd, true
	}
	return b.handleColumnCreateKey(msg, col)
}

func (b *Board) handleColumnActionKey(msg tea.KeyMsg, col *Column) (tea.Model, tea.Cmd, bool) {
	switch {
	case key.Matches(msg, Keys.RenameCol):
		return b, b.editor.OpenRenameColumn(b.selectedCol, col.Path, col.Name), true
	case key.Matches(msg, Keys.Filter):
		return b, col.BeginFilter(), true
	}
	return b, nil, false
}

func (b *Board) handleColumnDisplayKey(msg tea.KeyMsg, col *Column) (tea.Model, tea.Cmd, bool) {
	switch {
	case key.Matches(msg, Keys.ZoomToggle):
		b.zoom.Toggle()
		return b, nil, true
	case key.Matches(msg, Keys.CollapseCol):
		col.ToggleCollapse()
		if col.Collapsed {
			b.selectedCol = collapseFocusShift(b.selectedCol, len(b.columns))
		}
		return b, nil, true
	case key.Matches(msg, Keys.ZoomOff) && b.zoom.Active():
		b.zoom.Off()
		return b, nil, true
	}
	return b, nil, false
}

func (b *Board) handleColumnNavigationKey(msg tea.KeyMsg, col *Column) (tea.Model, tea.Cmd, bool) {
	switch {
	case key.Matches(msg, Keys.PrevCol):
		b.selectedCol--
		if b.selectedCol < 0 {
			b.selectedCol = len(b.columns) - 1
		}
		return b, nil, true
	case key.Matches(msg, Keys.NextCol):
		b.selectedCol++
		if b.selectedCol >= len(b.columns) {
			b.selectedCol = 0
		}
		return b, nil, true
	case key.Matches(msg, Keys.JumpCol):
		idx := int(msg.Runes[0] - '1')
		if idx >= 0 && idx < len(b.columns) {
			b.selectedCol = idx
		}
		return b, nil, true
	case key.Matches(msg, Keys.PanLeft):
		if b.firstVisibleCol > 0 {
			b.firstVisibleCol--
		}
		return b, nil, true
	case key.Matches(msg, Keys.PanRight):
		_, count := b.presenter.visibleColRange(b)
		maxFirst := max(len(b.columns)-count, 0)
		if b.firstVisibleCol < maxFirst {
			b.firstVisibleCol++
		}
		return b, nil, true
	}
	return b, nil, false
}

func (b *Board) handleColumnCreateKey(msg tea.KeyMsg, col *Column) (tea.Model, tea.Cmd, bool) {
	switch {
	case key.Matches(msg, Keys.TemplateMenu):
		m, cmd := b.templateMenuActions().open(col)
		return m, cmd, true
	case key.Matches(msg, Keys.New):
		m, cmd := b.mutationHandlers().openTemplateFlow(col)
		return m, cmd, true
	case key.Matches(msg, Keys.NewFirst):
		if len(b.columns) == 0 {
			return b, b.notifier.Error("no folders available"), true
		}
		return b, b.editor.OpenNew(0, b.columns[0].Name, b.columns[0].Path), true
	}
	return b, nil, false
}

func (b *Board) handleItemBoardKey(msg tea.KeyMsg, col *Column) (tea.Model, tea.Cmd, bool) {
	if !col.HasSelectedItem() {
		return b, nil, false
	}
	item := col.SelectedItem()
	actions := b.itemActions()
	switch {
	case key.Matches(msg, Keys.RenameItem):
		return b, b.editor.OpenRenameItem(b.selectedCol, col.Path, item.FullPath, item.Name), true
	case key.Matches(msg, Keys.CustomCommands):
		return b.handleCustomCommandsKey(col, item)
	case key.Matches(msg, Keys.Edit):
		return b, actions.edit(b.selectedCol, col, item), true
	case key.Matches(msg, Keys.Append):
		return b, actions.append(b.selectedCol, col, item), true
	case key.Matches(msg, Keys.Prepend):
		return b, actions.prepend(b.selectedCol, col, item), true
	case key.Matches(msg, Keys.Journal):
		return b, actions.journal(b.selectedCol, col, item), true
	case key.Matches(msg, Keys.Copy):
		return b, actions.copy(col, item), true
	case key.Matches(msg, Keys.Paste):
		return b, actions.paste(b.selectedCol, item), true
	case key.Matches(msg, Keys.OpenExternal):
		return b, actions.openExternal(col, item), true
	case key.Matches(msg, Keys.Pin):
		return b, actions.togglePin(col, item), true
	case key.Matches(msg, Keys.EditFrontmatter):
		return b, b.frontmatterActions().openEditor(b.selectedCol, col, item), true
	case key.Matches(msg, Keys.Delete):
		return b, actions.confirmDelete(b.selectedCol, col, item), true
	case key.Matches(msg, Keys.MoveNext):
		return b, actions.moveNext(b.selectedCol, col, item, true), true
	case key.Matches(msg, Keys.MoveFirst):
		return b, actions.moveFirst(b.selectedCol, col, item), true
	case key.Matches(msg, Keys.Peek):
		content, err := col.CopyContent(item.Name)
		if err != nil {
			return b, b.notifier.ErrorCause("failed to peek", err), true
		}
		return b, b.peek.Open(item.Title, string(content), b.termWidth), true
	}
	return b, nil, false
}

func (b *Board) handleCustomCommandsKey(col *Column, item *Item) (tea.Model, tea.Cmd, bool) {
	if item != nil && item.Separator {
		return b, nil, true
	}
	b.loadCommands()
	cmds := b.commandsForColumn(col)
	if len(cmds) == 0 {
		return b, nil, true
	}
	var vctx map[string]any
	switch {
	case col.Virtual:
		vctx = b.buildVirtualVars(col, item)
	case item != nil && item.Data != nil:
		vctx = b.buildFilesystemCtx(b.selectedCol, item)
	}
	b.customCmds.Open(cmds, b.commandWarnings, b.buildCommandVars(b.selectedCol, item), vctx)
	return b, nil, true
}

func (b *Board) handleListBoardKey(msg tea.KeyMsg, col *Column) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, Keys.CustomCommands):
		m, cmd, _ := b.handleCustomCommandsKey(col, nil)
		return m, cmd
	case key.Matches(msg, Keys.CursorDown):
		if col.CursorAtBottom() {
			col.SelectFirst()
			return b, nil
		}
	case key.Matches(msg, Keys.CursorUp):
		if col.CursorAtTop() {
			col.SelectLast()
			return b, nil
		}
	}
	return b, col.UpdateList(msg)
}
