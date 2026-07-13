package model

import (
	"slices"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

type boardVirtualCommands struct {
	board *Board
}

func (b *Board) virtualCommands() boardVirtualCommands {
	return boardVirtualCommands{board: b}
}

// handleKey intercepts keys while a virtual column is focused. It returns
// handled=true when it consumed the key (a column command, Enter's default
// action, or a swallowed built-in mutation key); handled=false lets the shared
// switch process navigation/global keys (and the X menu).
func (v boardVirtualCommands) handleKey(msg tea.KeyPressMsg, col *Column) (tea.Cmd, bool) {
	// Let the shared switch open the X menu (it builds the scoped command list).
	if key.Matches(msg, Keys.CustomCommands) {
		return nil, false
	}

	sel := col.SelectedItem()
	hasItem := sel != nil && !sel.Separator
	var item *Item
	if hasItem {
		item = sel
	}

	if msg.Code == tea.KeyEnter {
		if hasItem {
			return v.runDefault(col, sel), true
		}
		if cmd := v.defaultNoItem(col); cmd != nil {
			return cmd, true
		}
		return nil, true
	}

	s := msg.String()
	for _, vc := range col.colCmds {
		if vc.Key != "" && vc.Key == s {
			if !hasItem && vc.RequiresItem {
				return nil, true
			}
			return v.dispatch(col, item, vc.Ref, vc.Name), true
		}
	}

	if isVirtualBlockedKey(msg) {
		return nil, true
	}
	return nil, false
}

// runDefault runs the column's declared default command, or falls back to
// opening the item's underlying file when it has one, else no-op.
func (v boardVirtualCommands) runDefault(col *Column, item *Item) tea.Cmd {
	if col.defaultCmd != "" {
		for _, vc := range col.colCmds {
			if vc.ID == col.defaultCmd {
				return v.dispatch(col, item, vc.Ref, vc.Name)
			}
		}
	}
	if item.FullPath != "" {
		path := item.FullPath
		name := item.Title
		return func() tea.Msg {
			err := openFile(path)
			return customCommandFinishedMsg{Name: "open " + name, Err: err}
		}
	}
	return nil
}

// defaultNoItem runs the column's declared default command on an empty column,
// but only if that command opts out of needing an item. Returns nil otherwise
// (the file-open fallback in runDefault needs an item).
func (v boardVirtualCommands) defaultNoItem(col *Column) tea.Cmd {
	if col.defaultCmd == "" {
		return nil
	}
	for _, vc := range col.colCmds {
		if vc.ID == col.defaultCmd && !vc.RequiresItem {
			return v.dispatch(col, nil, vc.Ref, vc.Name)
		}
	}
	return nil
}

// dispatch runs a column-scoped (or scope=all) Lua command against a virtual
// item, passing the structured ctx (data/path/title/vid).
func (v boardVirtualCommands) dispatch(col *Column, item *Item, ref, name string) tea.Cmd {
	b := v.board
	req, err := b.scripts.RunVirtualCommand(ref, b.commandContext().virtualVars(col, item))
	return b.handleScriptResult(name, req, err)
}

// virtualBlockedBindings are the built-in item/column actions that require a real
// file and so must not run on a virtual (script-owned, fileless) column. NewFirst
// (N, targets the first real folder) is intentionally allowed. Single source of
// truth, shared by the key handler and the `?` menu (which disables these rows on
// virtual columns).
var virtualBlockedBindings = []key.Binding{
	Keys.Edit, Keys.Append, Keys.Prepend, Keys.Journal, Keys.Copy, Keys.Paste,
	Keys.OpenExternal, Keys.Pin, Keys.MoveMenu, Keys.MoveNext, Keys.RenameItem,
	Keys.Delete, Keys.New, Keys.RenameCol, Keys.EditFrontmatter, Keys.ApplyPreset, Keys.ToggleMark,
}

// isVirtualBlockedKey reports whether a pressed key is virtual-blocked.
func isVirtualBlockedKey(msg tea.KeyPressMsg) bool {
	for _, bnd := range virtualBlockedBindings {
		if key.Matches(msg, bnd) {
			return true
		}
	}
	return false
}

// isVirtualBlockedRunKey reports whether a menu row's run key maps to a
// virtual-blocked binding, so the `?` menu can disable it on virtual columns.
func isVirtualBlockedRunKey(runKey string) bool {
	if runKey == "" {
		return false
	}
	for _, bnd := range virtualBlockedBindings {
		if slices.Contains(bnd.Keys(), runKey) {
			return true
		}
	}
	return false
}
