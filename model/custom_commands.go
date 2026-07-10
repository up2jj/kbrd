package model

import (
	"sort"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

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

type customCommandRunContext struct {
	Vars map[string]string
	VCtx map[string]any
}

type runCustomCommandBatchMsg struct {
	Cmd  config.Command
	Runs []customCommandRunContext
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

func commandByID(cmds []config.Command, id string) (config.Command, bool) {
	for _, cmd := range cmds {
		if cmd.ID == id {
			return cmd, true
		}
	}
	return config.Command{}, false
}

func customCommandContextForItem(b *Board, col *Column, item *Item) (map[string]string, map[string]any, bool) {
	colIdx := b.indexOfColumn(col)
	if colIdx < 0 {
		return nil, nil, false
	}
	var vctx map[string]any
	switch {
	case col.Virtual:
		vctx = b.commandContext().virtualVars(col, item)
	case item != nil && item.Data != nil:
		vctx = b.commandContext().filesystemCtx(colIdx, item)
	}
	return b.commandContext().vars(colIdx, item), vctx, true
}

func customCommandRunsForTargets(b *Board, ctx itemActionContext, targets []itemActionTarget) []customCommandRunContext {
	runs := make([]customCommandRunContext, 0, len(targets))
	for _, target := range targets {
		it := target.Item
		vars, vctx, ok := customCommandContextForItem(b, ctx.Column, &it)
		if !ok {
			continue
		}
		runs = append(runs, customCommandRunContext{Vars: vars, VCtx: vctx})
	}
	return runs
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

func (b *Board) handleRunCustomCommandBatch(msg runCustomCommandBatchMsg) (tea.Model, tea.Cmd) {
	if len(msg.Runs) == 0 {
		return b, nil
	}
	cmds := make([]tea.Cmd, 0, len(msg.Runs))
	for _, run := range msg.Runs {
		run := run
		cmds = append(cmds, func() tea.Msg {
			return runCustomCommandMsg{Cmd: msg.Cmd, Vars: run.Vars, VCtx: run.VCtx}
		})
	}
	return b, tea.Sequence(cmds...)
}

func (b *Board) handleCustomCommandFinished(msg customCommandFinishedMsg) (tea.Model, tea.Cmd) {
	_ = b.loadColumns()
	if msg.Err != nil {
		return b, b.notifier.ErrorCause(msg.Name, msg.Err)
	}
	return b, b.notifier.Success(msg.Name + " finished")
}

type CustomCommandMenu struct {
	active bool
	flatPicker
	commands []config.Command
	warnings []config.CommandLoadWarning
	vars     map[string]string
	vctx     map[string]any // rich Lua ctx for virtual-column dispatch; nil otherwise
	batch    []customCommandRunContext
	mru      []string // command ids in MRU order (index 0 = most recent); session-only
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
	m.OpenWithBatch(commands, warnings, vars, vctx, nil)
}

func (m *CustomCommandMenu) OpenWithBatch(commands []config.Command, warnings []config.CommandLoadWarning, vars map[string]string, vctx map[string]any, batch []customCommandRunContext) {
	m.active = true
	m.commands = sortByUsage(commands, m.mru)
	m.warnings = warnings
	m.vars = vars
	m.vctx = vctx
	m.batch = batch
	m.fuzzyList.Reset(len(m.commands), 0, m.commandHaystack)
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
	m.warnings = nil
	m.vars = nil
	m.vctx = nil
	m.batch = nil
	m.fuzzyList.Clear()
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

func (m *CustomCommandMenu) Update(msg tea.KeyPressMsg) tea.Cmd {
	// Esc closes regardless of filter state.
	if key.Matches(msg, Keys.CustomCommandsClose) {
		m.Close()
		return nil
	}
	switch m.flatPicker.HandleInput(msg) {
	case flatPickerInputConfirm:
		index, ok := m.fuzzyList.SelectedIndex()
		if !ok {
			m.Close()
			return nil
		}
		return m.run(m.commands[index])
	}
	return nil
}

func (m *CustomCommandMenu) run(c config.Command) tea.Cmd {
	m.recordUse(c.ID)
	vars := m.vars
	vctx := m.vctx
	batch := append([]customCommandRunContext(nil), m.batch...)
	lineMode := m.lineMode
	line := m.line
	lineRow := m.lineRow
	m.Close()
	if lineMode {
		return func() tea.Msg {
			return runLineCommandMsg{Cmd: c, Line: line, Row: lineRow, Vars: vars}
		}
	}
	if c.NeedsItem() && len(batch) > 0 {
		return func() tea.Msg {
			return runCustomCommandBatchMsg{Cmd: c, Runs: batch}
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

	nameStyle := lipgloss.NewStyle().Foreground(p.FgBase)
	descStyle := lipgloss.NewStyle().Foreground(p.FgMuted)
	selStyle := lipgloss.NewStyle().Bold(true).Foreground(p.FgInverse).Background(p.Primary)
	hiStyle := lipgloss.NewStyle().Foreground(p.Highlight).Bold(true)
	hiSelStyle := lipgloss.NewStyle().Bold(true).Foreground(p.Highlight).Background(p.Primary)
	gutterSel := lipgloss.NewStyle().Foreground(p.Primary).Bold(true).Render("▌")

	filterLine := flatPickerFilterLine(p, m.filter, descStyle, nameStyle)

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
			nameIdx, descIdx := splitLabelDescMatchIndexes(c.Name, match.MatchedIndexes)
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
	inner := flatPickerInner(termWidth, parts...)
	return OverlayFrame{Title: "Custom commands", Body: inner, Footer: footer, Palette: p}.Render()
}
