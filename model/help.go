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
	return []ShortcutGroup{
		{
			Title: "Navigation",
			Items: []Shortcut{
				{"tab / ]", "next column"},
				{"shift+tab / [", "previous column"},
				{"j / k", "move within column"},
				{"/", "filter"},
			},
		},
		{
			Title: "Item",
			Items: []Shortcut{
				{"space", "peek"},
				{"e", "edit"},
				{"a", "append"},
				{"p", "prepend"},
				{"J", "journal entry"},
				{"c", "copy"},
				{"V", "paste"},
				{"o", "open in $EDITOR"},
				{"!", "pin / unpin"},
				{"m", "move to next column"},
				{"d", "delete"},
			},
		},
		{
			Title: "Create & Command",
			Items: []Shortcut{
				{"n", "new item in current folder"},
				{"N", "new item in first folder"},
				{".", "quick command"},
			},
		},
		{
			Title: "Global",
			Items: []Shortcut{
				{"R", "refresh"},
				{"t", "toggle theme"},
				{"?", "toggle this help"},
				{"ctrl+c", "quit"},
			},
		},
	}
}

// ContextShortcuts returns the small subset shown on the secondary footer line.
func ContextShortcuts(ctx ShortcutContext) []Shortcut {
	if ctx.QuickCmdMode {
		return []Shortcut{{"esc", "cancel"}}
	}
	if ctx.HasSelectedItem {
		return []Shortcut{
			{"space", "peek"},
			{"e", "edit"},
			{"a", "append"},
			{"d", "delete"},
			{"m", "move"},
			{".", "cmd"},
			{"?", "more"},
		}
	}
	return []Shortcut{
		{"n", "new"},
		{"/", "filter"},
		{"R", "refresh"},
		{".", "cmd"},
		{"?", "more"},
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
