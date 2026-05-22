package model

import "github.com/charmbracelet/bubbles/key"

type KeyMap struct {
	// Global
	Quit         key.Binding
	ToggleHelp   key.Binding
	QuickCmd     key.Binding
	SwitchBoard  key.Binding
	ToggleTheme  key.Binding
	Refresh      key.Binding

	// Navigation
	PrevCol  key.Binding
	NextCol  key.Binding
	PanLeft  key.Binding
	PanRight key.Binding
	Filter   key.Binding

	// Item actions
	Peek         key.Binding
	Edit         key.Binding
	Append       key.Binding
	Prepend      key.Binding
	Journal      key.Binding
	Copy         key.Binding
	Paste        key.Binding
	OpenExternal key.Binding
	Pin          key.Binding
	MoveNext     key.Binding
	MoveFirst    key.Binding
	RenameItem   key.Binding
	Delete       key.Binding
	CustomCommands key.Binding

	// Create
	New      key.Binding
	NewFirst key.Binding

	// Column
	RenameCol key.Binding

	// Editor
	EditorCancel       key.Binding
	EditorSave         key.Binding
	EditorConfirm      key.Binding
	EditorUndo         key.Binding
	EditorRedo         key.Binding
	EditorToggleExpand key.Binding

	// Dialog
	DialogPrev    key.Binding
	DialogNext    key.Binding
	DialogConfirm key.Binding
	DialogCancel  key.Binding

	// Peek
	PeekClose    key.Binding
	PeekPageDown key.Binding
	PeekDown     key.Binding
	PeekUp       key.Binding
	PeekTop      key.Binding
	PeekBottom   key.Binding

	// Switcher
	SwitcherClose     key.Binding
	SwitcherPrev      key.Binding
	SwitcherNext      key.Binding
	SwitcherConfirm   key.Binding
	SwitcherPinToggle key.Binding

	// Quick command
	QuickCmdCancel  key.Binding
	QuickCmdConfirm key.Binding

	// Help overlay
	HelpClose key.Binding

	// Config menu
	ConfigMenu              key.Binding
	ConfigMenuClose         key.Binding
	ConfigOpenLocal         key.Binding
	ConfigOpenGlobal        key.Binding
	ConfigOpenLocalCommands key.Binding

	// Custom commands menu
	CustomCommandsClose key.Binding

	// Git panel
	GitPanel            key.Binding
	GitDiff             key.Binding
	GitCommit           key.Binding
	GitSync             key.Binding
	GitCommitSync       key.Binding
	GitLog              key.Binding
	GitAddRemote        key.Binding
	GitPanelClose       key.Binding
	GitCommitCancel     key.Binding
	GitPanelFocusToggle key.Binding
}

var Keys = KeyMap{
	// Global
	Quit:        key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
	ToggleHelp:  key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "toggle this help")),
	QuickCmd:    key.NewBinding(key.WithKeys("."), key.WithHelp(".", "quick command")),
	SwitchBoard: key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("ctrl+p", "switch board")),
	ToggleTheme: key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "toggle theme")),
	Refresh:     key.NewBinding(key.WithKeys("f5"), key.WithHelp("F5", "refresh")),

	// Navigation
	PrevCol:  key.NewBinding(key.WithKeys("[", "shift+tab"), key.WithHelp("shift+tab / [", "previous column")),
	NextCol:  key.NewBinding(key.WithKeys("]", "tab"), key.WithHelp("tab / ]", "next column")),
	PanLeft:  key.NewBinding(key.WithKeys("H"), key.WithHelp("H", "pan columns left")),
	PanRight: key.NewBinding(key.WithKeys("L"), key.WithHelp("L", "pan columns right")),
	Filter:   key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),

	// Item actions
	Peek:         key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "peek")),
	Edit:         key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")),
	Append:       key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "append")),
	Prepend:      key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "prepend")),
	Journal:      key.NewBinding(key.WithKeys("J"), key.WithHelp("J", "journal entry")),
	Copy:         key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "copy")),
	Paste:        key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "paste…")),
	OpenExternal: key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open in $EDITOR")),
	Pin:          key.NewBinding(key.WithKeys("!"), key.WithHelp("!", "pin / unpin")),
	MoveNext:     key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "move to next column")),
	MoveFirst:    key.NewBinding(key.WithKeys("M"), key.WithHelp("M", "move to first column")),
	RenameItem:   key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "rename item")),
	Delete:       key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
	CustomCommands: key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "custom commands")),

	// Create
	New:      key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new item in current folder")),
	NewFirst: key.NewBinding(key.WithKeys("N"), key.WithHelp("N", "new item in first folder")),

	// Column
	RenameCol: key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "rename column")),

	// Editor
	EditorCancel:       key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	EditorSave:         key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "save")),
	EditorConfirm:      key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm")),
	EditorUndo:         key.NewBinding(key.WithKeys("ctrl+z"), key.WithHelp("ctrl+z", "undo")),
	EditorRedo:         key.NewBinding(key.WithKeys("ctrl+y"), key.WithHelp("ctrl+y", "redo")),
	EditorToggleExpand: key.NewBinding(key.WithKeys("ctrl+e"), key.WithHelp("ctrl+e", "toggle expanded")),

	// Dialog
	DialogPrev:    key.NewBinding(key.WithKeys("left", "h", "shift+tab"), key.WithHelp("←/h", "previous")),
	DialogNext:    key.NewBinding(key.WithKeys("right", "l", "tab"), key.WithHelp("→/l", "next")),
	DialogConfirm: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm")),
	DialogCancel:  key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),

	// Peek
	PeekClose:    key.NewBinding(key.WithKeys("esc", "q"), key.WithHelp("q/esc", "close")),
	PeekPageDown: key.NewBinding(key.WithKeys("enter", " ", "pgdown"), key.WithHelp("enter", "page down")),
	PeekDown:     key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j/↓", "scroll down")),
	PeekUp:       key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k/↑", "scroll up")),
	PeekTop:      key.NewBinding(key.WithKeys("g", "home"), key.WithHelp("g", "top")),
	PeekBottom:   key.NewBinding(key.WithKeys("G", "end"), key.WithHelp("G", "bottom")),

	// Switcher
	SwitcherClose:     key.NewBinding(key.WithKeys("esc", "ctrl+p"), key.WithHelp("esc", "cancel")),
	SwitcherPrev:      key.NewBinding(key.WithKeys("up"), key.WithHelp("↑", "previous")),
	SwitcherNext:      key.NewBinding(key.WithKeys("down"), key.WithHelp("↓", "next")),
	SwitcherConfirm:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "switch")),
	SwitcherPinToggle: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "pin/unpin")),

	// Quick command
	QuickCmdCancel:  key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	QuickCmdConfirm: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "run")),

	// Help overlay
	HelpClose: key.NewBinding(key.WithKeys("esc", "q", "?"), key.WithHelp("q/esc", "close")),

	// Config menu
	ConfigMenu:              key.NewBinding(key.WithKeys(","), key.WithHelp(",", "config commands")),
	ConfigMenuClose:         key.NewBinding(key.WithKeys("esc", "q"), key.WithHelp("q/esc", "close")),
	ConfigOpenLocal:         key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "open or create local config")),
	ConfigOpenGlobal:        key.NewBinding(key.WithKeys("C"), key.WithHelp("C", "open or create global config")),
	ConfigOpenLocalCommands: key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "open or create local commands")),

	// Custom commands menu
	CustomCommandsClose: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close")),

	// Git panel
	GitPanel:        key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "git panel")),
	GitDiff:         key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "diff")),
	GitCommit:       key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "commit")),
	GitSync:         key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sync (pull+push)")),
	GitCommitSync:   key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "commit + sync")),
	GitLog:          key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "log")),
	GitAddRemote:    key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add remote")),
	GitPanelClose:   key.NewBinding(key.WithKeys("esc", "q"), key.WithHelp("q/esc", "close")),
	GitCommitCancel:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	GitPanelFocusToggle: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "focus pane")),
}

func bindingShortcut(b key.Binding) Shortcut {
	h := b.Help()
	return Shortcut{Keys: h.Key, Label: h.Desc}
}

func bindingShortcuts(bs ...key.Binding) []Shortcut {
	out := make([]Shortcut, 0, len(bs))
	for _, b := range bs {
		out = append(out, bindingShortcut(b))
	}
	return out
}

// ShortcutGroups returns the help-overlay groups derived from the registry.
func ShortcutGroups() []ShortcutGroup {
	return []ShortcutGroup{
		{
			Title: "Navigation",
			Items: append(
				bindingShortcuts(Keys.NextCol, Keys.PrevCol, Keys.PanLeft, Keys.PanRight),
				Shortcut{"j / k", "move within column"},
				bindingShortcut(Keys.Filter),
			),
		},
		{
			Title: "Item",
			Items: bindingShortcuts(
				Keys.Peek, Keys.Edit, Keys.Append, Keys.Prepend, Keys.Journal,
				Keys.Copy, Keys.Paste, Keys.OpenExternal, Keys.Pin,
				Keys.MoveNext, Keys.MoveFirst, Keys.RenameItem, Keys.Delete,
				Keys.CustomCommands,
			),
		},
		{
			Title: "Create & Command",
			Items: bindingShortcuts(Keys.New, Keys.NewFirst, Keys.QuickCmd),
		},
		{
			Title: "Column",
			Items: bindingShortcuts(Keys.RenameCol),
		},
		{
			Title: "Global",
			Items: bindingShortcuts(Keys.Refresh, Keys.ToggleTheme, Keys.SwitchBoard, Keys.GitPanel, Keys.ConfigMenu, Keys.ToggleHelp, Keys.Quit),
		},
	}
}
