package model

import (
	"slices"
	"sort"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"kbrd/board"
	"kbrd/config"
	"kbrd/shellcmd"
)

type runCustomCommandMsg struct {
	Cmd  config.Command
	Vars map[string]string
	// VCtx is set only when the command runs against a virtual-column item. It
	// carries the rich Lua ctx (nested `data` table, path, title, vid) that a
	// plain string-var map can't hold. nil for filesystem columns.
	VCtx map[string]any
}

type customCommandFinishedMsg struct {
	Name string
	Err  error
}

func (b *Board) loadCommands() {
	cmds, warnings, err := config.LoadCommands(b.cfg.Path)
	if err != nil {
		b.commands = nil
		b.commandWarnings = []config.CommandLoadWarning{{Source: "commands", Message: err.Error()}}
		return
	}
	// Lua-registered commands come from the script host; they slot into the
	// same menu and follow the same id-precedence rule as YAML entries
	// (later wins, mirroring folder-overrides-global).
	if b.scripts != nil {
		cmds = mergeWithLuaCommands(cmds, b.scripts.Commands())
	}
	b.commands = cmds
	b.commandWarnings = warnings
}

// mergeWithLuaCommands returns shell cmds + lua cmds, with lua entries
// shadowing shell entries that share an id.
func mergeWithLuaCommands(shell, lua []config.Command) []config.Command {
	out := make([]config.Command, 0, len(shell)+len(lua))
	lset := make(map[string]bool, len(lua))
	for _, c := range lua {
		lset[c.ID] = true
	}
	for _, c := range shell {
		if lset[c.ID] {
			continue
		}
		out = append(out, c)
	}
	out = append(out, lua...)
	return out
}

// buildCommandVars renders the flat template vars for a command. item may be
// nil (a requiresItem: false command on an empty column), in which case the
// file fields are omitted and VarContext.Vars drops them so a template that
// references filePath fails cleanly.
func (b *Board) buildCommandVars(colIdx int, item *Item) map[string]string {
	col := b.columns[colIdx]
	vc := board.VarContext{
		BoardPath:  b.cfg.Path,
		BoardName:  b.cfg.BoardName,
		ColumnPath: col.Path,
		ColumnName: col.Name,
	}
	if item != nil {
		vc.FilePath = item.FullPath
		vc.FileName = item.Name
	}
	return vc.Vars()
}

// buildVirtualVars builds the structured Lua ctx for a command dispatched on a
// virtual-column item: the standard board/column fields plus the item's opaque
// `data` table and a common `path`/`filePath` (set only when the item provided
// one). The shared `path` lets a scope="all" command serve real and virtual
// items without branching.
// buildVirtualVars builds the structured ctx for a virtual-column command. item
// may be nil (a requiresItem: false command on an empty column); the item-
// specific fields (title/fileName/path/data) are then omitted, leaving the
// board/column context.
func (b *Board) buildVirtualVars(col *Column, item *Item) map[string]any {
	m := map[string]any{
		"boardPath":  b.cfg.Path,
		"boardName":  b.cfg.BoardName,
		"columnName": col.Name,
		"vid":        col.VID,
	}
	if item != nil {
		m["title"] = item.Title
		m["fileName"] = item.Name
		if item.FullPath != "" {
			m["path"] = item.FullPath
			m["filePath"] = item.FullPath
		}
		if item.Data != nil {
			m["data"] = item.Data
		}
	}
	return m
}

// buildFilesystemCtx builds the structured Lua ctx for a command dispatched on
// a filesystem item that carries frontmatter data: a strict superset of the
// flat string vars (so scripts reading ctx.fileDir etc. keep working) plus
// `title`, the shared `path`, and the nested `data` table that a string map
// can't hold. Items without frontmatter keep the plain string-vars flow.
func (b *Board) buildFilesystemCtx(colIdx int, item *Item) map[string]any {
	ctx := map[string]any{}
	for k, v := range b.buildCommandVars(colIdx, item) {
		ctx[k] = v
	}
	if item != nil {
		ctx["path"] = item.FullPath
		ctx["title"] = item.Title
		ctx["data"] = item.Data
	}
	return ctx
}

// commandsForColumn returns the menu command list for the focused column,
// applying scope: filesystem columns show files/all globals; virtual columns
// show their own column-scoped commands first, then virtual/all globals. When
// the column has no selected item, commands that require one are dropped so
// only requiresItem: false commands remain (possibly none).
func (b *Board) commandsForColumn(col *Column) []config.Command {
	hasItem := col.HasSelectedItem()
	keep := func(c config.Command) bool { return hasItem || !c.NeedsItem() }
	if col.Virtual {
		out := make([]config.Command, 0, len(col.colCmds)+len(b.commands))
		for _, vc := range col.colCmds {
			requiresItem := vc.RequiresItem
			c := config.Command{
				Name:         vc.Name,
				ID:           vc.ID,
				Scope:        "virtual",
				RequiresItem: &requiresItem,
				Source:       config.SourceLua,
				LuaRef:       vc.Ref,
			}
			if keep(c) {
				out = append(out, c)
			}
		}
		for _, c := range b.commands {
			if c.ShowsOnVirtual() && keep(c) {
				out = append(out, c)
			}
		}
		return out
	}
	out := make([]config.Command, 0, len(b.commands))
	for _, c := range b.commands {
		if c.ShowsOnFiles() && keep(c) {
			out = append(out, c)
		}
	}
	return out
}

// handleVirtualColumnKey intercepts keys while a virtual column is focused. It
// returns handled=true when it consumed the key (a column command, Enter's
// default action, or a swallowed built-in mutation key); handled=false lets the
// shared switch process navigation/global keys (and the X menu).
func (b *Board) handleVirtualColumnKey(msg tea.KeyMsg, col *Column) (tea.Cmd, bool) {
	// Let the shared switch open the X menu (it builds the scoped command list).
	if key.Matches(msg, Keys.CustomCommands) {
		return nil, false
	}

	sel := col.SelectedItem()
	hasItem := sel != nil && !sel.Separator
	// item carries the selection through to dispatch; nil when the column is
	// empty so only requiresItem: false commands run.
	var item *Item
	if hasItem {
		item = sel
	}

	// Enter runs the column's default action.
	if msg.Type == tea.KeyEnter {
		if hasItem {
			return b.runVirtualDefault(col, sel), true
		}
		// On an empty column the default action still fires if it opts out of
		// needing an item.
		if cmd := b.virtualDefaultNoItem(col); cmd != nil {
			return cmd, true
		}
		return nil, true
	}

	// A key bound to one of the column's commands wins over built-in actions.
	s := msg.String()
	for _, vc := range col.colCmds {
		if vc.Key != "" && vc.Key == s {
			if !hasItem && vc.RequiresItem {
				return nil, true // needs an item, none selected: swallow
			}
			return b.dispatchVirtualCommand(col, item, vc.Ref, vc.Name), true
		}
	}

	// Built-in item/column mutation keys are blocked on virtual columns.
	if isVirtualBlockedKey(msg) {
		return nil, true
	}
	return nil, false
}

// virtualBlockedBindings are the built-in item/column actions that require a real
// file and so must not run on a virtual (script-owned, fileless) column. NewFirst
// (N, targets the first real folder) is intentionally allowed. Single source of
// truth, shared by the key handler and the `?` menu (which disables these rows on
// virtual columns).
var virtualBlockedBindings = []key.Binding{
	Keys.Edit, Keys.Append, Keys.Prepend, Keys.Journal, Keys.Copy, Keys.Paste,
	Keys.OpenExternal, Keys.Pin, Keys.MoveNext, Keys.MoveFirst, Keys.RenameItem,
	Keys.Delete, Keys.New, Keys.RenameCol, Keys.EditFrontmatter,
}

// isVirtualBlockedKey reports whether a pressed key is virtual-blocked.
func isVirtualBlockedKey(msg tea.KeyMsg) bool {
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

// runVirtualDefault runs the column's declared default command, or falls back to
// opening the item's underlying file when it has one, else no-op.
func (b *Board) runVirtualDefault(col *Column, item *Item) tea.Cmd {
	if col.defaultCmd != "" {
		for _, vc := range col.colCmds {
			if vc.ID == col.defaultCmd {
				return b.dispatchVirtualCommand(col, item, vc.Ref, vc.Name)
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

// virtualDefaultNoItem runs the column's declared default command on an empty
// column, but only if that command opts out of needing an item. Returns nil
// otherwise (the file-open fallback in runVirtualDefault needs an item).
func (b *Board) virtualDefaultNoItem(col *Column) tea.Cmd {
	if col.defaultCmd == "" {
		return nil
	}
	for _, vc := range col.colCmds {
		if vc.ID == col.defaultCmd && !vc.RequiresItem {
			return b.dispatchVirtualCommand(col, nil, vc.Ref, vc.Name)
		}
	}
	return nil
}

// dispatchVirtualCommand runs a column-scoped (or scope=all) Lua command against
// a virtual item, passing the structured ctx (data/path/title/vid).
func (b *Board) dispatchVirtualCommand(col *Column, item *Item, ref, name string) tea.Cmd {
	req, err := b.scripts.RunVirtualCommand(ref, b.buildVirtualVars(col, item))
	return b.handleScriptResult(name, req, err)
}

func (b *Board) handleRunCustomCommand(msg runCustomCommandMsg) (tea.Model, tea.Cmd) {
	if msg.Cmd.Source == config.SourceLua {
		if msg.VCtx != nil {
			// Virtual-column item: dispatch with the structured ctx so the
			// script sees ctx.data, ctx.path, etc.
			req, err := b.scripts.RunVirtualCommand(msg.Cmd.LuaRef, msg.VCtx)
			return b, b.handleScriptResult(msg.Cmd.Name, req, err)
		}
		req, err := b.scripts.RunCommand(msg.Cmd.LuaRef, msg.Vars)
		return b, b.handleScriptResult(msg.Cmd.Name, req, err)
	}
	rendered, err := msg.Cmd.Render(msg.Vars)
	if err != nil {
		return b, b.notifier.ErrorCause("template error", err)
	}
	c := shellcmd.Command(b.cfg.Path, rendered)
	name := msg.Cmd.Name
	return b, tea.ExecProcess(c, func(err error) tea.Msg {
		return customCommandFinishedMsg{Name: name, Err: err}
	})
}

func (b *Board) handleCustomCommandFinished(msg customCommandFinishedMsg) (tea.Model, tea.Cmd) {
	_ = b.loadColumns()
	// ExecProcess handed the terminal to the command; Bubble Tea's restore does
	// not re-arm mouse reporting, so do it ourselves (see git.restoreMouse).
	if msg.Err != nil {
		return b, tea.Batch(tea.EnableMouseCellMotion, b.notifier.ErrorCause(msg.Name, msg.Err))
	}
	return b, tea.Batch(tea.EnableMouseCellMotion, b.notifier.Success(msg.Name+" finished"))
}

type CustomCommandMenu struct {
	active   bool
	selected int
	filter   string
	commands []config.Command
	matches  []FuzzyMatch
	warnings []config.CommandLoadWarning
	vars     map[string]string
	vctx     map[string]any // rich Lua ctx for virtual-column dispatch; nil otherwise
	mru      []string       // command ids in MRU order (index 0 = most recent); session-only
	palette  Palette

	// lineMode marks the menu as the in-editor line-command picker: run() emits
	// runLineCommandMsg (which splices the result into the editor) instead of the
	// board-mutating runCustomCommandMsg. line is the current editor line, also
	// mirrored into vars["line"] for shell templates; lineRow is its 0-based row,
	// carried so a slow/async result replaces that row, not the cursor's later one.
	lineMode bool
	line     string
	lineRow  int
}

func (m *CustomCommandMenu) commandHaystack(i int) string {
	c := m.commands[i]
	if c.Description != "" {
		return c.Name + "  " + c.Description
	}
	return c.Name
}

func (m *CustomCommandMenu) Open(commands []config.Command, warnings []config.CommandLoadWarning, vars map[string]string, vctx map[string]any) {
	m.active = true
	m.commands = sortByUsage(commands, m.mru)
	m.warnings = warnings
	m.vars = vars
	m.vctx = vctx
	m.selected = 0
	m.filter = ""
	m.recompute()
}

// OpenLine opens the menu as the in-editor line-command picker. line is the
// current editor line, handed to the command as ctx.line / vars["line"]; the
// command's return value replaces that line.
func (m *CustomCommandMenu) OpenLine(commands []config.Command, warnings []config.CommandLoadWarning, line string, row int, vars map[string]string) {
	m.Open(commands, warnings, vars, nil)
	m.lineMode = true
	m.line = line
	m.lineRow = row
}

func (m *CustomCommandMenu) Close() {
	m.active = false
	m.commands = nil
	m.matches = nil
	m.warnings = nil
	m.vars = nil
	m.vctx = nil
	m.selected = 0
	m.filter = ""
	m.lineMode = false
	m.line = ""
	m.lineRow = 0
}

func (m *CustomCommandMenu) Active() bool { return m.active }

// recordUse moves id to the front of the session MRU list so the next Open
// floats it to the top of the unfiltered list. Session-only; not persisted.
func (m *CustomCommandMenu) recordUse(id string) {
	if id == "" {
		return // virtual/Lua ids aren't guaranteed non-empty
	}
	next := []string{id} // most-recent first
	for _, x := range m.mru {
		if x != id {
			next = append(next, x)
		}
	}
	m.mru = next
}

// sortByUsage returns cmds reordered so previously-used commands lead in MRU
// order, with never-used commands keeping their original (YAML/Lua) order via a
// stable sort. Only affects the empty-filter view: filterFuzzy returns indexes
// in slice order when the query is empty, while a non-empty query is ranked by
// fuzzy relevance independent of input order.
func sortByUsage(cmds []config.Command, mru []string) []config.Command {
	if len(mru) == 0 {
		return cmds
	}
	rank := make(map[string]int, len(mru))
	for i, id := range mru {
		rank[id] = i
	}
	out := append([]config.Command(nil), cmds...)
	sort.SliceStable(out, func(i, j int) bool {
		ri, oki := rank[out[i].ID]
		rj, okj := rank[out[j].ID]
		if oki && okj {
			return ri < rj // both used: MRU order
		}
		if oki != okj {
			return oki // used before never-used
		}
		return false // both unused: stable -> keep original order
	})
	return out
}

func (m *CustomCommandMenu) recompute() {
	m.matches = filterFuzzy(len(m.commands), m.filter, m.commandHaystack)
	if m.selected >= len(m.matches) {
		m.selected = len(m.matches) - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
}

func (m *CustomCommandMenu) Update(msg tea.KeyMsg) tea.Cmd {
	// Esc closes regardless of filter state.
	if key.Matches(msg, Keys.CustomCommandsClose) {
		m.Close()
		return nil
	}
	switch msg.Type {
	case tea.KeyUp:
		if m.selected > 0 {
			m.selected--
		}
		return nil
	case tea.KeyDown:
		if m.selected < len(m.matches)-1 {
			m.selected++
		}
		return nil
	case tea.KeyEnter:
		if len(m.matches) == 0 {
			m.Close()
			return nil
		}
		return m.run(m.commands[m.matches[m.selected].Index])
	case tea.KeyBackspace:
		if r := []rune(m.filter); len(r) > 0 {
			m.filter = string(r[:len(r)-1])
			m.recompute()
		}
		return nil
	case tea.KeyRunes, tea.KeySpace:
		s := msg.String()
		if s != "" {
			m.filter += s
			m.selected = 0
			m.recompute()
		}
		return nil
	}
	return nil
}

func (m *CustomCommandMenu) run(c config.Command) tea.Cmd {
	m.recordUse(c.ID)
	vars := m.vars
	vctx := m.vctx
	lineMode := m.lineMode
	line := m.line
	lineRow := m.lineRow
	m.Close()
	if lineMode {
		return func() tea.Msg {
			return runLineCommandMsg{Cmd: c, Line: line, Row: lineRow, Vars: vars}
		}
	}
	return func() tea.Msg {
		return runCustomCommandMsg{Cmd: c, Vars: vars, VCtx: vctx}
	}
}

func (m *CustomCommandMenu) View(termWidth, termHeight int) string {
	p := m.palette
	var warnSection string
	if len(m.warnings) > 0 {
		errStyle := lipgloss.NewStyle().Foreground(p.Danger).Bold(true)
		srcStyle := lipgloss.NewStyle().Foreground(p.Warning).Italic(true)
		rows := []string{errStyle.Render("load errors:")}
		for _, w := range m.warnings {
			rows = append(rows, "  "+srcStyle.Render(w.Source)+" "+errStyle.Render(w.Message))
		}
		warnSection = lipgloss.JoinVertical(lipgloss.Left, rows...)
	}

	keyStyle := lipgloss.NewStyle().Foreground(p.Highlight).Bold(true)
	nameStyle := lipgloss.NewStyle().Foreground(p.FgBase)
	descStyle := lipgloss.NewStyle().Foreground(p.FgMuted)
	selStyle := lipgloss.NewStyle().Bold(true).Foreground(p.FgInverse).Background(p.Primary)
	hiStyle := lipgloss.NewStyle().Foreground(p.Highlight).Bold(true)
	hiSelStyle := lipgloss.NewStyle().Bold(true).Foreground(p.Highlight).Background(p.Primary)
	gutterSel := lipgloss.NewStyle().Foreground(p.Primary).Bold(true).Render("▌")

	cursor := keyStyle.Render("> ")
	filterText := m.filter
	if filterText == "" {
		filterText = descStyle.Render("type to filter…")
	} else {
		filterText = nameStyle.Render(filterText)
	}
	filterLine := cursor + filterText

	var body string
	switch {
	case len(m.commands) == 0:
		body = helpDimStyle.Render("no commands defined — create ~/.config/kbrd/commands.yml or ./.kbrd_commands.yml")
	case len(m.matches) == 0:
		body = helpDimStyle.Render("no matches")
	default:
		rows := make([]string, 0, len(m.matches))
		for i, match := range m.matches {
			c := m.commands[match.Index]
			source := string(c.Source)
			if source == "" {
				source = string(config.SourceShell)
			}
			selected := i == m.selected
			// name + optional description form the fuzzy haystack — indexes are
			// rune offsets into "Name  Description" (two-space separator).
			nameLen := len([]rune(c.Name))
			var nameIdx, descIdx []int
			for _, idx := range match.MatchedIndexes {
				if idx < nameLen {
					nameIdx = append(nameIdx, idx)
				} else if idx >= nameLen+2 {
					descIdx = append(descIdx, idx-nameLen-2)
				}
			}
			nameBase := nameStyle
			descBase := descStyle
			hiName := hiStyle
			hiDesc := hiStyle
			if selected {
				nameBase = selStyle
				descBase = selStyle
				hiName = hiSelStyle
				hiDesc = hiSelStyle
			}
			styled := renderHighlighted(c.Name, nameIdx, nameBase, hiName)
			if c.Description != "" {
				sep := "  —  "
				if selected {
					styled += selStyle.Render(sep)
				} else {
					styled += descStyle.Render(sep)
				}
				styled += renderHighlighted(c.Description, descIdx, descBase, hiDesc)
			}
			tag := " (" + source + ")"
			if selected {
				styled += selStyle.Render(tag)
			} else {
				styled += descStyle.Render(tag)
			}
			gutter := " "
			if selected {
				gutter = gutterSel
				styled = selStyle.Render(" ") + styled + selStyle.Render(" ")
			}
			rows = append(rows, gutter+" "+styled)
		}
		body = lipgloss.JoinVertical(lipgloss.Left, rows...)
	}

	footer := RenderInlineHints([]Shortcut{
		{Keys: "type", Label: "filter"},
		{Keys: "↑/↓", Label: "select"},
		{Keys: "enter", Label: "run"},
		{Keys: "esc", Label: "cancel"},
	})
	parts := []string{filterLine, ""}
	if warnSection != "" {
		parts = append(parts, warnSection, "")
	}
	parts = append(parts, body)
	inner := lipgloss.JoinVertical(lipgloss.Left, parts...)

	minInner := 50
	if termWidth > 0 && termWidth-12 < minInner {
		minInner = termWidth - 12
	}
	if lipgloss.Width(inner) < minInner {
		inner = lipgloss.NewStyle().Width(minInner).Render(inner)
	}
	return OverlayFrame{Title: "Custom commands", Body: inner, Footer: footer, Palette: p}.Render()
}
