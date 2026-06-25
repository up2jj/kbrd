package model

import (
	"sort"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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
