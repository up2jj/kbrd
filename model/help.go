package model

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type Shortcut struct {
	Keys  string
	Label string
}

type ShortcutGroup struct {
	Title string
	Items []Shortcut
}

type ShortcutContext struct {
	HasSelectedItem bool
	QuickCmdMode    bool
	Filtering       bool
}

var (
	helpKeyStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#e2e8f0"))
	helpLabelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#94a3b8"))
	helpSepStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#475569"))
	helpTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#60a5fa"))
	helpDimStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#475569")).Italic(true)
)

// GlobalShortcuts returns the full grouped registry, used by the help overlay.
func GlobalShortcuts(ctx ShortcutContext) []ShortcutGroup {
	return ShortcutGroups()
}

// ContextShortcuts returns the small subset shown on the secondary footer line.
func ContextShortcuts(ctx ShortcutContext) []Shortcut {
	short := func(keys, label string) Shortcut { return Shortcut{Keys: keys, Label: label} }
	if ctx.QuickCmdMode {
		return []Shortcut{bindingShortcut(Keys.QuickCmdCancel)}
	}
	if ctx.HasSelectedItem {
		return []Shortcut{
			bindingShortcut(Keys.Peek),
			bindingShortcut(Keys.Edit),
			bindingShortcut(Keys.Append),
			bindingShortcut(Keys.Delete),
			short(Keys.MoveNext.Help().Key, "move"),
			bindingShortcut(Keys.CustomCommands),
			short(Keys.QuickCmd.Help().Key, "cmd"),
			short(Keys.ToggleHelp.Help().Key, "more"),
		}
	}
	return []Shortcut{
		short(Keys.New.Help().Key, "new"),
		bindingShortcut(Keys.Filter),
		short(Keys.RenameCol.Help().Key, "rename col"),
		short(Keys.QuickCmd.Help().Key, "cmd"),
		short(Keys.ToggleHelp.Help().Key, "more"),
	}
}

// RenderInlineHints renders a single line of `key label · key label · …` hints.
func RenderInlineHints(items []Shortcut) string {
	parts := make([]string, 0, len(items))
	for _, s := range items {
		parts = append(parts, helpKeyStyle.Render(s.Keys)+" "+helpLabelStyle.Render(s.Label))
	}
	return strings.Join(parts, helpSepStyle.Render(" · "))
}

// RenderHelpOverlay renders the full grouped shortcut panel.
func RenderHelpOverlay(maxWidth, maxHeight int, groups []ShortcutGroup) string {
	keyColWidth := 0
	for _, g := range groups {
		for _, s := range g.Items {
			if w := lipgloss.Width(s.Keys); w > keyColWidth {
				keyColWidth = w
			}
		}
	}

	keyCol := lipgloss.NewStyle().Width(keyColWidth + 2).Bold(true).Foreground(lipgloss.Color("#e2e8f0"))

	var sections []string
	for _, g := range groups {
		rows := []string{helpTitleStyle.Render(g.Title)}
		for _, s := range g.Items {
			rows = append(rows, "  "+keyCol.Render(s.Keys)+helpLabelStyle.Render(s.Label))
		}
		sections = append(sections, lipgloss.JoinVertical(lipgloss.Left, rows...))
	}

	body := lipgloss.JoinVertical(lipgloss.Left, sections...)
	footer := helpDimStyle.Render("? or esc to close")

	content := lipgloss.JoinVertical(lipgloss.Left,
		helpTitleStyle.Render("Shortcuts"),
		"",
		body,
		"",
		footer,
	)

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#3b82f6")).
		Padding(1, 3)

	return box.Render(content)
}

// RenderConfigCommandsOverlay renders the config commands dialog with paths + existence.
func RenderConfigCommandsOverlay(entries []ConfigCommandEntry) string {
	keyColWidth := 0
	labelColWidth := 0
	for _, e := range entries {
		if w := lipgloss.Width(e.Key); w > keyColWidth {
			keyColWidth = w
		}
		if w := lipgloss.Width(e.Label); w > labelColWidth {
			labelColWidth = w
		}
	}
	keyCol := lipgloss.NewStyle().Width(keyColWidth + 2).Bold(true).Foreground(lipgloss.Color("#e2e8f0"))
	labelCol := lipgloss.NewStyle().Width(labelColWidth + 2).Foreground(lipgloss.Color("#94a3b8"))

	existsStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#22c55e")).Bold(true)
	missingStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#f59e0b")).Bold(true)
	errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#ef4444"))
	pathStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#cbd5e1")).Italic(true)

	rows := []string{helpTitleStyle.Render("Config commands"), ""}
	for _, e := range entries {
		var status, path string
		if e.Err != nil {
			status = errStyle.Render("error")
			path = errStyle.Render(e.Err.Error())
		} else {
			path = pathStyle.Render(e.Path)
			if e.Exists {
				status = existsStyle.Render("exists ")
			} else {
				status = missingStyle.Render("missing")
			}
		}
		rows = append(rows,
			"  "+keyCol.Render(e.Key)+labelCol.Render(e.Label)+status,
			"  "+keyCol.Render("")+labelCol.Render("")+path,
		)
	}
	rows = append(rows, "", helpDimStyle.Render("esc to go back"))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#a78bfa")).
		Padding(1, 3)

	return box.Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
}
