package model

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
)

type KeyMap struct {
	// Global
	Quit         key.Binding
	ToggleHelp   key.Binding
	MnemonicJump key.Binding
	SwitchBoard  key.Binding
	Search       key.Binding
	Refresh      key.Binding

	// Navigation
	PrevCol     key.Binding
	NextCol     key.Binding
	JumpCol     key.Binding
	PanLeft     key.Binding
	PanRight    key.Binding
	Filter      key.Binding
	CursorUp    key.Binding
	CursorDown  key.Binding
	ColPageUp   key.Binding
	ColPageDown key.Binding

	// Item actions
	Peek            key.Binding
	Edit            key.Binding
	Append          key.Binding
	Prepend         key.Binding
	Journal         key.Binding
	Copy            key.Binding
	Paste           key.Binding
	OpenExternal    key.Binding
	Pin             key.Binding
	MoveNext        key.Binding
	MoveFirst       key.Binding
	RenameItem      key.Binding
	Delete          key.Binding
	CustomCommands  key.Binding
	EditFrontmatter key.Binding

	// Create
	New          key.Binding
	NewFirst     key.Binding
	TemplateMenu key.Binding

	// Column
	RenameCol   key.Binding
	ZoomToggle  key.Binding
	ZoomOff     key.Binding
	CollapseCol key.Binding

	// Editor
	EditorCancel       key.Binding
	EditorSave         key.Binding
	EditorConfirm      key.Binding
	EditorUndo         key.Binding
	EditorRedo         key.Binding
	EditorToggleExpand key.Binding
	EditorCommand      key.Binding
	EditorTaskPrefix   key.Binding

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

	// Search
	SearchClose   key.Binding
	SearchPrev    key.Binding
	SearchNext    key.Binding
	SearchConfirm key.Binding

	// Mnemonic jump
	MnemonicJumpCancel  key.Binding
	MnemonicJumpConfirm key.Binding

	// Help overlay
	HelpClose key.Binding

	// Config menu
	ConfigMenu              key.Binding
	ConfigMenuClose         key.Binding
	ConfigOpenLocal         key.Binding
	ConfigOpenGlobal        key.Binding
	ConfigOpenLocalCommands key.Binding
	ConfigCreateLocalMCP    key.Binding
	ConfigCreateLocalAgents key.Binding

	// Custom commands menu
	CustomCommandsClose key.Binding

	// Git panel (open binding only; in-panel bindings live in the git package)
	GitPanel key.Binding

	// Zellij actions menu (only active when running inside zellij)
	ZellijMenu     key.Binding // opens the menu
	ZellijFloating key.Binding // f — floating editor pane
	ZellijTiled    key.Binding // e — tiled editor pane
	ZellijShell    key.Binding // s — shell in board dir
	ZellijClose    key.Binding
}

var Keys = KeyMap{
	// Global
	Quit:         key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
	ToggleHelp:   key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "toggle this help")),
	MnemonicJump: key.NewBinding(key.WithKeys(":"), key.WithHelp(":", "jump to card mnemonic")),
	SwitchBoard:  key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("ctrl+p", "switch board")),
	Search:       key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "search in boards")),
	Refresh:      key.NewBinding(key.WithKeys("f5"), key.WithHelp("F5", "refresh")),

	// Navigation
	PrevCol:     key.NewBinding(key.WithKeys("[", "shift+tab", "left"), key.WithHelp("← / shift+tab / [", "previous column")),
	NextCol:     key.NewBinding(key.WithKeys("]", "tab", "right"), key.WithHelp("→ / tab / ]", "next column")),
	JumpCol:     key.NewBinding(key.WithKeys("1", "2", "3", "4", "5", "6", "7", "8", "9"), key.WithHelp("1-9", "jump to column N")),
	PanLeft:     key.NewBinding(key.WithKeys("H"), key.WithHelp("H", "pan columns left")),
	PanRight:    key.NewBinding(key.WithKeys("L"), key.WithHelp("L", "pan columns right")),
	Filter:      key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
	CursorUp:    key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑ / k", "move up (wraps)")),
	CursorDown:  key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓ / j", "move down (wraps)")),
	ColPageUp:   key.NewBinding(key.WithKeys("pgup"), key.WithHelp("pgup", "page up")),
	ColPageDown: key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("pgdown", "page down")),

	// Item actions
	Peek:            key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "peek")),
	Edit:            key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")),
	Append:          key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "append")),
	Prepend:         key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "prepend")),
	Journal:         key.NewBinding(key.WithKeys("b", "J"), key.WithHelp("b", "journal entry")),
	Copy:            key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "copy")),
	Paste:           key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "paste…")),
	OpenExternal:    key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open in $EDITOR")),
	Pin:             key.NewBinding(key.WithKeys("!"), key.WithHelp("!", "pin / unpin")),
	MoveNext:        key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "move to next column")),
	MoveFirst:       key.NewBinding(key.WithKeys("M"), key.WithHelp("M", "move to first column")),
	RenameItem:      key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "rename item")),
	Delete:          key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
	CustomCommands:  key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "custom commands")),
	EditFrontmatter: key.NewBinding(key.WithKeys("~"), key.WithHelp("~", "edit frontmatter")),

	// Create
	New:          key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "create item")),
	NewFirst:     key.NewBinding(key.WithKeys("N"), key.WithHelp("N", "new item in first folder")),
	TemplateMenu: key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "templates")),

	// Column
	RenameCol:   key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "rename column")),
	ZoomToggle:  key.NewBinding(key.WithKeys("+", "="), key.WithHelp("+", "zoom column")),
	ZoomOff:     key.NewBinding(key.WithKeys("-", "esc"), key.WithHelp("-/esc", "exit zoom")),
	CollapseCol: key.NewBinding(key.WithKeys("|"), key.WithHelp("|", "collapse column")),

	// Editor
	EditorCancel:       key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	EditorSave:         key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "save")),
	EditorConfirm:      key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm")),
	EditorUndo:         key.NewBinding(key.WithKeys("ctrl+z"), key.WithHelp("ctrl+z", "undo")),
	EditorRedo:         key.NewBinding(key.WithKeys("ctrl+y"), key.WithHelp("ctrl+y", "redo")),
	EditorToggleExpand: key.NewBinding(key.WithKeys("ctrl+e"), key.WithHelp("ctrl+e", "toggle expanded")),
	EditorCommand:      key.NewBinding(key.WithKeys("ctrl+l"), key.WithHelp("ctrl+l", "run line command")),
	EditorTaskPrefix:   key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("ctrl+t", "insert task")),

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

	// Search
	SearchClose:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	SearchPrev:    key.NewBinding(key.WithKeys("up"), key.WithHelp("↑", "previous")),
	SearchNext:    key.NewBinding(key.WithKeys("down"), key.WithHelp("↓", "next")),
	SearchConfirm: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),

	// Mnemonic jump
	MnemonicJumpCancel:  key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	MnemonicJumpConfirm: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "jump")),

	// Help overlay
	HelpClose: key.NewBinding(key.WithKeys("esc", "q", "?"), key.WithHelp("q/esc", "close")),

	// Config menu
	ConfigMenu:              key.NewBinding(key.WithKeys(","), key.WithHelp(",", "config commands")),
	ConfigMenuClose:         key.NewBinding(key.WithKeys("esc", "q"), key.WithHelp("q/esc", "close")),
	ConfigOpenLocal:         key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "open or create local config")),
	ConfigOpenGlobal:        key.NewBinding(key.WithKeys("C"), key.WithHelp("C", "open or create global config")),
	ConfigOpenLocalCommands: key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "open or create local commands")),
	ConfigCreateLocalMCP:    key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "create local .mcp.json")),
	ConfigCreateLocalAgents: key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "create local AGENTS.md")),

	// Custom commands menu
	CustomCommandsClose: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close")),

	// Git panel (open binding only; in-panel bindings live in the git package)
	GitPanel: key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "git panel")),

	// Zellij actions (mnemonics f/e/s only matter while the menu is open, which
	// is routed before the global bindings, so they don't clash with Search/Edit)
	ZellijMenu:     key.NewBinding(key.WithKeys("z"), key.WithHelp("z", "zellij actions")),
	ZellijFloating: key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "floating editor pane")),
	ZellijTiled:    key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "new tiled pane")),
	ZellijShell:    key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "shell in board dir")),
	ZellijClose:    key.NewBinding(key.WithKeys("esc", "q"), key.WithHelp("q/esc", "close")),
}

func bindingShortcut(b key.Binding) Shortcut {
	h := b.Help()
	return Shortcut{Keys: h.Key, Label: h.Desc}
}

// helpEntry builds a keybindings-menu row from a binding and its tooltip. The
// row is executable only when the binding's primary key is a single rune (the
// menu injects that rune); arrow/ctrl/named keys become reference-only rows.
func helpEntry(b key.Binding, tooltip string) HelpEntry {
	h := b.Help()
	run := ""
	if keys := b.Keys(); len(keys) > 0 {
		if r := []rune(keys[0]); len(r) == 1 {
			run = keys[0]
		}
	}
	return HelpEntry{Keys: menuKey(h.Key), Label: h.Desc, Desc: tooltip, RunKey: run}
}

// menuKey condenses a binding's help key to a single token for the menu column.
// Help keys often list every alternative ("← / shift+tab / [") which is too wide
// for an aligned column; the first alternative is the representative one. Splits
// on " / " (not "/") so a binding whose key is literally "/" survives intact.
func menuKey(helpKey string) string {
	if first, _, found := strings.Cut(helpKey, " / "); found {
		return strings.TrimSpace(first)
	}
	return helpKey
}

// HelpMenuGroups returns the grouped, tooltip-annotated rows for the interactive
// keybindings menu (the `?` overlay). Rows in the Item group are flagged
// NeedsItem so the menu can disable them when no card is selected.
func HelpMenuGroups() []HelpGroup {
	item := func(b key.Binding, tip string) HelpEntry {
		e := helpEntry(b, tip)
		e.NeedsItem = true
		return e
	}
	global := []HelpEntry{
		helpEntry(Keys.Refresh, "Reload every column from disk, picking up external edits."),
		helpEntry(Keys.SwitchBoard, "Open the board switcher to jump to a recent or pinned board."),
		helpEntry(Keys.Search, "Full-text search across all known boards."),
		helpEntry(Keys.GitPanel, "Open the git panel to review changes, commit, and sync."),
		helpEntry(Keys.ConfigMenu, "Open config files and command/MCP definitions."),
		helpEntry(Keys.ToggleHelp, "Show this keybindings menu."),
		helpEntry(Keys.Quit, "Quit kbrd."),
	}
	if inZellij() {
		global = append(global, helpEntry(Keys.ZellijMenu, "Open the selected card in a zellij pane or shell."))
	}
	return []HelpGroup{
		{
			Title: "Navigation",
			Items: []HelpEntry{
				helpEntry(Keys.NextCol, "Move focus to the next column."),
				helpEntry(Keys.PrevCol, "Move focus to the previous column."),
				helpEntry(Keys.JumpCol, "Jump straight to a column by its mnemonic letter."),
				helpEntry(Keys.PanLeft, "Scroll the visible column window left."),
				helpEntry(Keys.PanRight, "Scroll the visible column window right."),
				{Keys: "j/k", Label: "move within column", Desc: "Move the selection down or up within the focused column."},
				helpEntry(Keys.ColPageDown, "Jump the selection down by a page."),
				helpEntry(Keys.ColPageUp, "Jump the selection up by a page."),
				helpEntry(Keys.Filter, "Filter cards in the focused column as you type."),
			},
		},
		{
			Title: "Item",
			Items: []HelpEntry{
				item(Keys.Peek, "Preview the selected card's rendered markdown in a reader."),
				item(Keys.Edit, "Open the selected card in the editor to change its body."),
				item(Keys.Append, "Append text to the end of the selected card."),
				item(Keys.Prepend, "Prepend text to the start of the selected card."),
				item(Keys.Journal, "Add a timestamped journal entry to the selected card."),
				item(Keys.Copy, "Copy the selected card to the internal clipboard."),
				item(Keys.Paste, "Paste the copied card into the focused column."),
				item(Keys.OpenExternal, "Open the selected card in your $EDITOR."),
				item(Keys.Pin, "Pin or unpin the selected card to the top of its column."),
				item(Keys.MoveNext, "Move the selected card to the next column."),
				item(Keys.MoveFirst, "Move the selected card to the first column."),
				item(Keys.RenameItem, "Rename the selected card's file."),
				item(Keys.Delete, "Delete the selected card (asks to confirm)."),
				item(Keys.CustomCommands, "Run a custom command against the selected card."),
				item(Keys.EditFrontmatter, "Edit the selected card's YAML frontmatter."),
			},
		},
		{
			Title: "Create & Command",
			Items: []HelpEntry{
				helpEntry(Keys.New, "Open the create menu for an empty card or template."),
				helpEntry(Keys.NewFirst, "Create a new card at the top of the focused column."),
				helpEntry(Keys.TemplateMenu, "Open template management for the focused column."),
				helpEntry(Keys.MnemonicJump, "Open the mnemonic prompt to jump to a visible card."),
			},
		},
		{
			Title: "Column",
			Items: []HelpEntry{
				helpEntry(Keys.RenameCol, "Rename the focused column's directory."),
				helpEntry(Keys.ZoomToggle, "Zoom the focused column to fill the board."),
				helpEntry(Keys.CollapseCol, "Collapse the focused column to a thin bar."),
			},
		},
		{
			Title: "Global",
			Items: global,
		},
	}
}
