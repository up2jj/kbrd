package model

import (
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"

	"kbrd/config"
)

type runCustomCommandMsg struct {
	Cmd  config.Command
	Vars map[string]string
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

func (b *Board) buildCommandVars(colIdx int, item *Item) map[string]string {
	col := b.columns[colIdx]
	return map[string]string{
		"filePath":   item.FullPath,
		"fileName":   item.Name,
		"fileDir":    filepath.Dir(item.FullPath),
		"boardPath":  b.cfg.Path,
		"boardName":  b.cfg.BoardName,
		"columnPath": col.Path,
		"columnName": col.Name,
	}
}

func (b *Board) handleRunCustomCommand(msg runCustomCommandMsg) (tea.Model, tea.Cmd) {
	if msg.Cmd.Source == config.SourceLua {
		req, err := b.scripts.RunCommand(msg.Cmd.LuaRef, msg.Vars)
		return b, b.handleScriptResult(msg.Cmd.Name, req, err)
	}
	rendered, err := msg.Cmd.Render(msg.Vars)
	if err != nil {
		return b, b.notifier.Send("template error: "+err.Error(), notifyError)
	}
	c := exec.Command("sh", "-c", rendered)
	c.Dir = b.cfg.Path
	name := msg.Cmd.Name
	return b, tea.ExecProcess(c, func(err error) tea.Msg {
		return customCommandFinishedMsg{Name: name, Err: err}
	})
}

func (b *Board) handleCustomCommandFinished(msg customCommandFinishedMsg) (tea.Model, tea.Cmd) {
	_ = b.loadColumns()
	if msg.Err != nil {
		return b, b.notifier.Send(msg.Name+": "+msg.Err.Error(), notifyError)
	}
	return b, b.notifier.Send(msg.Name+" finished", notifySuccess)
}

// commandMatch is one entry in the (possibly filtered) visible list.
// MatchedIndexes are rune offsets into the rendered haystack ("name  description").
type commandMatch struct {
	Index          int
	MatchedIndexes []int
}

type commandSource []config.Command

func (s commandSource) String(i int) string {
	c := s[i]
	if c.Description != "" {
		return c.Name + "  " + c.Description
	}
	return c.Name
}
func (s commandSource) Len() int { return len(s) }

type CustomCommandMenu struct {
	active   bool
	selected int
	filter   string
	commands []config.Command
	matches  []commandMatch
	warnings []config.CommandLoadWarning
	vars     map[string]string
}

func (m *CustomCommandMenu) Open(commands []config.Command, warnings []config.CommandLoadWarning, vars map[string]string) {
	m.active = true
	m.commands = commands
	m.warnings = warnings
	m.vars = vars
	m.selected = 0
	m.filter = ""
	m.recompute()
}

func (m *CustomCommandMenu) Close() {
	m.active = false
	m.commands = nil
	m.matches = nil
	m.warnings = nil
	m.vars = nil
	m.selected = 0
	m.filter = ""
}

func (m *CustomCommandMenu) Active() bool { return m.active }

func (m *CustomCommandMenu) recompute() {
	if m.filter == "" {
		m.matches = make([]commandMatch, len(m.commands))
		for i := range m.commands {
			m.matches[i] = commandMatch{Index: i}
		}
	} else {
		results := fuzzy.FindFrom(m.filter, commandSource(m.commands))
		m.matches = make([]commandMatch, len(results))
		for i, r := range results {
			m.matches[i] = commandMatch{Index: r.Index, MatchedIndexes: r.MatchedIndexes}
		}
	}
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
	vars := m.vars
	m.Close()
	return func() tea.Msg {
		return runCustomCommandMsg{Cmd: c, Vars: vars}
	}
}

// renderHighlighted returns s with the runes at the given indexes wrapped in
// hi-style and the rest in baseStyle. indexes are sorted ascending.
func renderHighlighted(s string, indexes []int, baseStyle, hiStyle lipgloss.Style) string {
	if len(indexes) == 0 {
		return baseStyle.Render(s)
	}
	var b strings.Builder
	idxSet := make(map[int]bool, len(indexes))
	for _, i := range indexes {
		idxSet[i] = true
	}
	runes := []rune(s)
	i := 0
	for i < len(runes) {
		if idxSet[i] {
			// run of matched runes
			j := i
			for j < len(runes) && idxSet[j] {
				j++
			}
			b.WriteString(hiStyle.Render(string(runes[i:j])))
			i = j
		} else {
			j := i
			for j < len(runes) && !idxSet[j] {
				j++
			}
			b.WriteString(baseStyle.Render(string(runes[i:j])))
			i = j
		}
	}
	return b.String()
}

func (m *CustomCommandMenu) View(termWidth, termHeight int) string {
	title := helpTitleStyle.Render("Custom commands")

	var warnSection string
	if len(m.warnings) > 0 {
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#ef4444")).Bold(true)
		srcStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#f59e0b")).Italic(true)
		rows := []string{errStyle.Render("load errors:")}
		for _, w := range m.warnings {
			rows = append(rows, "  "+srcStyle.Render(w.Source)+" "+errStyle.Render(w.Message))
		}
		warnSection = lipgloss.JoinVertical(lipgloss.Left, rows...)
	}

	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#fde047")).Bold(true)
	nameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#e2e8f0"))
	descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#94a3b8"))
	selStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#0f172a")).Background(lipgloss.Color("#60a5fa"))
	hiStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#fde047")).Bold(true)
	hiSelStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#fde047")).Background(lipgloss.Color("#60a5fa"))
	gutterSel := lipgloss.NewStyle().Foreground(lipgloss.Color("#60a5fa")).Bold(true).Render("▌")

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
	parts := []string{title, "", filterLine, ""}
	if warnSection != "" {
		parts = append(parts, warnSection, "")
	}
	parts = append(parts, body, "", footer)
	content := lipgloss.JoinVertical(lipgloss.Left, parts...)

	minInner := 50
	if termWidth > 0 && termWidth-12 < minInner {
		minInner = termWidth - 12
	}
	if lipgloss.Width(content) < minInner {
		content = lipgloss.NewStyle().Width(minInner).Render(content)
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#3b82f6")).
		Padding(1, 4).
		Render(content)
}
