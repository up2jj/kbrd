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
	// Zoomed is set while a column is zoomed, to surface the exit hint.
	Zoomed bool
	// Virtual is set when the focused column is a virtual (script) column;
	// VirtualCmds holds its declared command key/label hints to surface inline.
	Virtual     bool
	VirtualCmds []Shortcut
}

var (
	helpKeyStyle      lipgloss.Style
	helpLabelStyle    lipgloss.Style
	helpSepStyle      lipgloss.Style
	helpTitleStyle    lipgloss.Style
	helpDimStyle      lipgloss.Style
	helpOverlayBorder lipgloss.Color
	helpConfigBorder  lipgloss.Color
	helpRowKey        lipgloss.Color
	helpRowLabel      lipgloss.Color
	helpExistsColor   lipgloss.Color
	helpMissingColor  lipgloss.Color
	helpErrorColor    lipgloss.Color
	helpPathColor     lipgloss.Color
)

func setHelpStyles(p Palette) {
	helpKeyStyle = lipgloss.NewStyle().Bold(true).Foreground(p.FgBase)
	helpLabelStyle = lipgloss.NewStyle().Foreground(p.FgMuted)
	helpSepStyle = lipgloss.NewStyle().Foreground(p.FgDim)
	helpTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(p.Primary)
	helpDimStyle = lipgloss.NewStyle().Foreground(p.FgDim).Italic(true)
	helpOverlayBorder = p.BorderActive
	helpConfigBorder = p.AccentAlt
	helpRowKey = p.FgBase
	helpRowLabel = p.FgMuted
	helpExistsColor = p.Success
	helpMissingColor = p.Warning
	helpErrorColor = p.Danger
	helpPathColor = p.FgSoft
}

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
	var prefix []Shortcut
	if ctx.Zoomed {
		prefix = []Shortcut{bindingShortcut(Keys.ZoomOff)}
	}
	if ctx.Virtual {
		out := append(prefix, short("enter", "default"))
		out = append(out, ctx.VirtualCmds...)
		out = append(out,
			bindingShortcut(Keys.CustomCommands),
			short(Keys.QuickCmd.Help().Key, "cmd"),
			short(Keys.ToggleHelp.Help().Key, "more"),
		)
		return out
	}
	if ctx.HasSelectedItem {
		return append(prefix,
			bindingShortcut(Keys.Peek),
			bindingShortcut(Keys.Edit),
			bindingShortcut(Keys.Append),
			bindingShortcut(Keys.Delete),
			short(Keys.MoveNext.Help().Key, "move"),
			bindingShortcut(Keys.CustomCommands),
			short(Keys.GitPanel.Help().Key, "git"),
			short(Keys.QuickCmd.Help().Key, "cmd"),
			short(Keys.ToggleHelp.Help().Key, "more"),
		)
	}
	return append(prefix,
		short(Keys.New.Help().Key, "new"),
		short(Keys.NewFromTemplate.Help().Key, "template"),
		bindingShortcut(Keys.Filter),
		short(Keys.RenameCol.Help().Key, "rename col"),
		short(Keys.GitPanel.Help().Key, "git"),
		short(Keys.QuickCmd.Help().Key, "cmd"),
		short(Keys.ToggleHelp.Help().Key, "more"),
	)
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

	keyCol := lipgloss.NewStyle().Width(keyColWidth + 2).Bold(true).Foreground(helpRowKey)

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
		BorderForeground(helpOverlayBorder).
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
	keyCol := lipgloss.NewStyle().Width(keyColWidth + 2).Bold(true).Foreground(helpRowKey)
	labelCol := lipgloss.NewStyle().Width(labelColWidth + 2).Foreground(helpRowLabel)

	existsStyle := lipgloss.NewStyle().Foreground(helpExistsColor).Bold(true)
	missingStyle := lipgloss.NewStyle().Foreground(helpMissingColor).Bold(true)
	errStyle := lipgloss.NewStyle().Foreground(helpErrorColor)
	pathStyle := lipgloss.NewStyle().Foreground(helpPathColor).Italic(true)

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
		BorderForeground(helpConfigBorder).
		Padding(1, 3)

	return box.Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
}
