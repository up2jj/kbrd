package model

import (
	"os/exec"
	"path/filepath"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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
	b.commands = cmds
	b.commandWarnings = warnings
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

type CustomCommandMenu struct {
	active   bool
	selected int
	commands []config.Command
	warnings []config.CommandLoadWarning
	vars     map[string]string
}

func (m *CustomCommandMenu) Open(commands []config.Command, warnings []config.CommandLoadWarning, vars map[string]string) {
	m.active = true
	m.commands = commands
	m.warnings = warnings
	m.vars = vars
	m.selected = 0
}

func (m *CustomCommandMenu) Close() {
	m.active = false
	m.commands = nil
	m.warnings = nil
	m.vars = nil
	m.selected = 0
}

func (m *CustomCommandMenu) Active() bool { return m.active }

func (m *CustomCommandMenu) Update(msg tea.KeyMsg) tea.Cmd {
	switch {
	case msg.String() == "esc":
		m.Close()
		return nil
	case key.Matches(msg, Keys.SwitcherPrev):
		if m.selected > 0 {
			m.selected--
		}
		return nil
	case key.Matches(msg, Keys.SwitcherNext):
		if m.selected < len(m.commands)-1 {
			m.selected++
		}
		return nil
	case key.Matches(msg, Keys.SwitcherConfirm):
		if len(m.commands) == 0 {
			m.Close()
			return nil
		}
		return m.run(m.commands[m.selected])
	}

	s := msg.String()
	r := []rune(s)
	if len(r) == 1 {
		for _, c := range m.commands {
			if c.Shortcut == s {
				return m.run(c)
			}
		}
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

func (m *CustomCommandMenu) View() string {
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

	var body string
	if len(m.commands) == 0 {
		body = helpDimStyle.Render("no commands defined — create ~/.config/kbrd/commands.yml or ./.kbrd_commands.yml")
	} else {
		keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#fde047")).Bold(true)
		nameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#e2e8f0"))
		descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#94a3b8"))
		selStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#0f172a")).Background(lipgloss.Color("#60a5fa"))
		gutterSel := lipgloss.NewStyle().Foreground(lipgloss.Color("#60a5fa")).Bold(true).Render("▌")

		rows := make([]string, 0, len(m.commands))
		for i, c := range m.commands {
			gutter := " "
			if i == m.selected {
				gutter = gutterSel
			}
			line := "[" + c.Shortcut + "] " + c.Name
			if c.Description != "" {
				line += "  —  " + c.Description
			}
			if i == m.selected {
				rows = append(rows, gutter+" "+selStyle.Render(" "+line+" "))
				continue
			}
			styled := keyStyle.Render("["+c.Shortcut+"]") + " " + nameStyle.Render(c.Name)
			if c.Description != "" {
				styled += "  " + descStyle.Render(c.Description)
			}
			rows = append(rows, gutter+" "+styled)
		}
		body = lipgloss.JoinVertical(lipgloss.Left, rows...)
	}

	footer := helpDimStyle.Render("shortcut · ↑/↓ select · enter run · esc cancel")
	parts := []string{title, ""}
	if warnSection != "" {
		parts = append(parts, warnSection, "")
	}
	parts = append(parts, body, "", footer)
	content := lipgloss.JoinVertical(lipgloss.Left, parts...)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#3b82f6")).
		Padding(1, 3).
		Render(content)
}
