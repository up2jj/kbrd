package model

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

func (b *Board) handleGlobalBoardKey(msg tea.KeyPressMsg, col *Column) (tea.Model, tea.Cmd, bool) {
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
	case key.Matches(msg, Keys.MnemonicJump):
		return b, b.mnemonicSelector().open(), true
	case key.Matches(msg, Keys.SwitchBoard):
		return b, b.session().openSwitcher(), true
	case key.Matches(msg, Keys.SwitchLayer):
		return b, b.openLayerSwitcher(), true
	case key.Matches(msg, Keys.Search):
		return b, b.searchActions().openSearch(), true
	case key.Matches(msg, Keys.GitPanel):
		return b, b.git.Open(), true
	case b.terminal.Enabled() && key.Matches(msg, Keys.TerminalMenu):
		b.terminal.OpenFor(b.cfg.Path, col)
		return b, nil, true
	case key.Matches(msg, Keys.Refresh):
		return b, b.utilityActions().refresh(), true
	case key.Matches(msg, Keys.Clipboard):
		return b, b.clipboardActions().openBrowser(), true
	}
	return b, nil, false
}

func (b *Board) handleColumnBoardKey(msg tea.KeyPressMsg, col *Column) (tea.Model, tea.Cmd, bool) {
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

func (b *Board) handleColumnActionKey(msg tea.KeyPressMsg, col *Column) (tea.Model, tea.Cmd, bool) {
	switch {
	case key.Matches(msg, Keys.ToggleMark):
		return b, b.toggleFocusedMark(), true
	case key.Matches(msg, Keys.ClearMarks):
		return b, b.clearFocusedMarks(), true
	case key.Matches(msg, Keys.RenameCol):
		return b, b.editor.OpenRenameColumn(b.selectedCol, col.Path, col.Name), true
	case key.Matches(msg, Keys.Filter):
		return b, col.BeginFilter(), true
	}
	return b, nil, false
}

func (b *Board) handleColumnDisplayKey(msg tea.KeyPressMsg, col *Column) (tea.Model, tea.Cmd, bool) {
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

func (b *Board) handleColumnNavigationKey(msg tea.KeyPressMsg, col *Column) (tea.Model, tea.Cmd, bool) {
	if cmd, handled := b.moveMarkedByArrow(msg, col); handled {
		return b, cmd, true
	}
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
		idx := int(msg.Text[0] - '1')
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
		_, count := boardColumnsRegion{}.visibleColRange(b.columnsRegionContext())
		maxFirst := max(len(b.columns)-count, 0)
		if b.firstVisibleCol < maxFirst {
			b.firstVisibleCol++
		}
		return b, nil, true
	}
	return b, nil, false
}

func (b *Board) handleColumnCreateKey(msg tea.KeyPressMsg, col *Column) (tea.Model, tea.Cmd, bool) {
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

func (b *Board) handleItemBoardKey(msg tea.KeyPressMsg, col *Column) (tea.Model, tea.Cmd, bool) {
	cmd, handled := b.itemActions().InvokeKey(msg, actionSourceKey)
	return b, cmd, handled
}

func (b *Board) handleCustomCommandsKey(col *Column, item *Item) (tea.Model, tea.Cmd, bool) {
	ctx, ok := b.itemActions().contextFor(itemActionSpec{ID: actionCustomCommands, Cardinality: actionMultiEach}, actionSourceKey)
	if !ok {
		return b, nil, false
	}
	ctx.Column = col
	ctx.Item = item
	ctx.ColIdx = b.selectedCol
	return b.openCustomCommands(ctx)
}

func (b *Board) openCustomCommands(ctx itemActionContext) (tea.Model, tea.Cmd, bool) {
	col := ctx.Column
	item := ctx.Item
	if item != nil && item.Separator {
		return b, nil, true
	}
	b.loadCommands()
	cmdCtx := b.commandContext()
	cmds := cmdCtx.commandsForColumn(col)
	if len(cmds) == 0 {
		return b, nil, true
	}
	vars, vctx, ok := customCommandContextForItem(b, col, item)
	if !ok {
		return b, nil, false
	}
	var batch []customCommandRunContext
	if len(ctx.Targets) > 0 && col.MarkedCount() > 0 {
		batch = customCommandRunsForTargets(b, ctx, ctx.Targets)
	}
	b.customCmds.OpenWithBatch(cmds, b.commandWarnings, vars, vctx, batch)
	return b, nil, true
}

func (b *Board) handleListBoardKey(msg tea.KeyPressMsg, col *Column) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, Keys.CustomCommands):
		cmd, _ := b.itemActions().Invoke(actionCustomCommands, actionSourceKey)
		return b, cmd
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
