package model

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"kbrd/config"
)

// updateHelpMenu routes a key to the open `?` keybindings menu. Search mode
// (entered with `/`) captures typing; otherwise keys run a row, switch the
// focused column, or navigate.
func (b *Board) updateHelpMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, Keys.Quit) {
		b.helpMenu.Close()
		return b.beginShutdown()
	}

	// Search mode: typing filters; everything else navigates or runs the
	// selection.
	if b.helpMenu.Filtering() {
		switch msg.Type {
		case tea.KeyEsc:
			b.helpMenu.StopFilter()
		case tea.KeyEnter:
			return b.runHelpSelected()
		case tea.KeyBackspace:
			b.helpMenu.Backspace()
		case tea.KeyRunes, tea.KeySpace:
			b.helpMenu.AppendFilter(msg.String())
		default:
			// ↑/↓, ctrl+n/p, page keys move the selection.
			b.helpMenu.Update(msg)
		}
		return b, nil
	}

	switch {
	case key.Matches(msg, Keys.HelpClose):
		b.helpMenu.Close()
	case msg.String() == "/":
		b.helpMenu.StartFilter()
	case msg.Type == tea.KeyEnter:
		return b.runHelpSelected()
	case msg.Type == tea.KeyLeft:
		// ←/→ switch the focused column (wrapping) so the menu shows that
		// column's commands; the selection stays in the menu.
		idx := b.selectedCol - 1
		if idx < 0 {
			idx = len(b.columns) - 1
		}
		b.helpFocusColumn(idx)
	case msg.Type == tea.KeyRight:
		idx := b.selectedCol + 1
		if idx >= len(b.columns) {
			idx = 0
		}
		b.helpFocusColumn(idx)
	case len(msg.Runes) == 1 && msg.Runes[0] >= '1' && msg.Runes[0] <= '9':
		// 1-9 jump straight to a column (including virtual ones), keeping the
		// menu open on the target column.
		b.helpFocusColumn(int(msg.Runes[0] - '1'))
	default:
		// Execution gets first claim: a key that names a runnable row runs it
		// (e.g. `e` edit, `g` git, a column-command key) — matching what the menu
		// shows. Any other key navigates (↑/↓, j/k, page keys).
		if run := b.helpMenu.RunKeyFor(msg.String()); run != "" {
			b.helpMenu.Close()
			return b.updateInner(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(run)})
		}
		b.helpMenu.Update(msg)
	}
	return b, nil
}

// openHelpMenu (re)builds the keybindings menu for the focused column, labelling
// it with the column name so column switching (←/→, 1-9) is visible.
func (b *Board) openHelpMenu() {
	label := ""
	if b.selectedCol < len(b.columns) {
		label = b.columns[b.selectedCol].Name
	}
	b.helpMenu.SetContext(label)
	b.helpMenu.Open(b.helpGroups())
}

// helpFocusColumn switches the menu's focused column (if idx is valid) and
// rebuilds it so its local commands and disabled rows reflect that column.
func (b *Board) helpFocusColumn(idx int) {
	if idx < 0 || idx >= len(b.columns) {
		return
	}
	b.selectedCol = idx
	b.openHelpMenu()
}

// helpGroups composes the keybindings menu from local context: a leading
// "Column commands" section for a focused virtual column, then the static
// catalog with item-scoped rows disabled when no card is selected. The board
// owns this decision so the menu stays a dumb renderer (mirrors ContextShortcuts
// for the keybar).
func (b *Board) helpGroups() []HelpGroup {
	var col *Column
	if b.selectedCol < len(b.columns) {
		col = b.columns[b.selectedCol]
	}
	hasItem := col != nil && col.HasSelectedItem()
	virtual := col != nil && col.Virtual

	groups := HelpMenuGroups()
	for gi := range groups {
		for ei := range groups[gi].Items {
			e := &groups[gi].Items[ei]
			// Item-scoped rows need a selected card; file-mutation rows don't
			// apply on virtual (script-owned, fileless) columns.
			if e.NeedsItem && !hasItem {
				e.Disabled = true
			}
			if virtual && isVirtualBlockedRunKey(e.RunKey) {
				e.Disabled = true
			}
		}
	}

	var local []HelpGroup
	if col != nil {
		if col.Virtual {
			var items []HelpEntry
			for _, vc := range col.colCmds {
				if vc.Key == "" {
					continue
				}
				run := ""
				if r := []rune(vc.Key); len(r) == 1 {
					run = vc.Key
				}
				items = append(items, HelpEntry{
					Keys:   vc.Key,
					Label:  vc.Name,
					Desc:   "Column command for this script column: " + vc.Name + ".",
					RunKey: run,
				})
			}
			if len(items) > 0 {
				local = append(local, HelpGroup{Title: "Column commands", Items: items})
			}
		}

		// User-defined custom commands available in this context. Scope filtering
		// (ShowsOnFiles/ShowsOnVirtual) keeps e.g. files-only commands off virtual
		// columns, mirroring the `x` menu's commandsForColumn.
		var cmds []HelpEntry
		for _, c := range b.commands {
			avail := c.ShowsOnVirtual()
			if !col.Virtual {
				avail = c.ShowsOnFiles()
			}
			if !avail || (c.NeedsItem() && !hasItem) {
				continue
			}
			desc := c.Description
			if desc == "" {
				desc = "Run the custom command \"" + c.Name + "\"."
			}
			cmds = append(cmds, HelpEntry{Keys: "↵", Label: c.Name, Desc: desc, CmdID: c.ID})
		}
		if len(cmds) > 0 {
			local = append(local, HelpGroup{Title: "Custom commands", Items: cmds})
		}
	}
	return append(local, groups...)
}

// runHelpSelected closes the menu and runs the highlighted row — injecting its
// key through the normal path, or dispatching its custom command.
func (b *Board) runHelpSelected() (tea.Model, tea.Cmd) {
	e := b.helpMenu.SelectedEntry()
	b.helpMenu.Close()
	switch {
	case e.RunKey != "":
		return b.updateInner(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(e.RunKey)})
	case e.CmdID != "":
		return b, b.runHelpCustomCommand(e.CmdID)
	}
	return b, nil
}

// runHelpCustomCommand dispatches a custom command chosen from the `?` menu
// through the normal custom-command path, building vars/context for the focused
// column and selected item exactly as the `x` menu does.
func (b *Board) runHelpCustomCommand(id string) tea.Cmd {
	if b.selectedCol >= len(b.columns) {
		return nil
	}
	col := b.columns[b.selectedCol]
	var cmd config.Command
	found := false
	for _, c := range b.commands {
		if c.ID == id {
			cmd, found = c, true
			break
		}
	}
	if !found {
		return nil
	}
	var item *Item
	if col.HasSelectedItem() {
		item = col.SelectedItem()
	}
	var vctx map[string]any
	switch {
	case col.Virtual:
		vctx = b.buildVirtualVars(col, item)
	case item != nil && item.Data != nil:
		vctx = b.buildFilesystemCtx(b.selectedCol, item)
	}
	vars := b.buildCommandVars(b.selectedCol, item)
	return func() tea.Msg { return runCustomCommandMsg{Cmd: cmd, Vars: vars, VCtx: vctx} }
}
