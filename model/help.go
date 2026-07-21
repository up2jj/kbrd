package model

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

type Shortcut struct {
	Keys  string
	Label string
}

// HelpEntry is one row of the interactive keybindings menu. RunKey is the single
// rune to inject when the row is executed (empty for non-actionable rows like
// "j / k"). NeedsItem marks rows that only apply when a card is selected; the
// board uses it to set Disabled (struck-through, non-executable) at open time.
type HelpEntry struct {
	Keys      string
	Label     string
	Desc      string
	RunKey    string
	NeedsItem bool
	Disabled  bool
	// UsesMarkedCards identifies actions that operate on the focused column's
	// marked cards when marks exist. The board uses it to collect those actions
	// into an explicit contextual section of the help menu.
	UsesMarkedCards bool
	// CmdID names a custom command to run on Enter (no single-key binding); the
	// board dispatches it through the normal custom-command path.
	CmdID string
	// ActionID names a built-in action with no dedicated key binding.
	ActionID string
}

// HelpGroup is a titled section of the keybindings menu.
type HelpGroup struct {
	Title string
	Items []HelpEntry
}

type ShortcutContext struct {
	HasSelectedItem bool
	MnemonicMode    bool
	Filtering       bool
	// Zoomed is set while a column is zoomed, to surface the exit hint.
	Zoomed bool
	// Virtual is set when the focused column is a virtual (script) column;
	// VirtualCmds holds its declared command key/label hints to surface inline.
	Virtual     bool
	VirtualCmds []Shortcut
}

var (
	helpKeyStyle     lipgloss.Style
	helpLabelStyle   lipgloss.Style
	helpSepStyle     lipgloss.Style
	helpTitleStyle   lipgloss.Style
	helpDimStyle     lipgloss.Style
	helpConfigBorder color.Color
	helpRowKey       color.Color
	helpRowLabel     color.Color
	helpExistsColor  color.Color
	helpMissingColor color.Color
	helpErrorColor   color.Color
	helpPathColor    color.Color
)

func setHelpStyles(p Palette) {
	helpKeyStyle = lipgloss.NewStyle().Bold(true).Foreground(p.FgBase)
	helpLabelStyle = lipgloss.NewStyle().Foreground(p.FgMuted)
	helpSepStyle = lipgloss.NewStyle().Foreground(p.FgDim)
	helpTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(p.Primary)
	helpDimStyle = lipgloss.NewStyle().Foreground(p.FgDim).Italic(true)
	helpConfigBorder = p.AccentAlt
	helpRowKey = p.FgBase
	helpRowLabel = p.FgMuted
	helpExistsColor = p.Success
	helpMissingColor = p.Warning
	helpErrorColor = p.Danger
	helpPathColor = p.FgSoft
}

// ContextShortcuts returns the small subset shown on the secondary footer line.
func ContextShortcuts(ctx ShortcutContext) []Shortcut {
	short := func(keys, label string) Shortcut { return Shortcut{Keys: keys, Label: label} }
	if ctx.MnemonicMode {
		return []Shortcut{
			bindingShortcut(Keys.MnemonicJumpConfirm),
			bindingShortcut(Keys.MnemonicJumpCancel),
		}
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
			short(Keys.MnemonicJump.Help().Key, "jump"),
			short(Keys.Harpoon.Help().Key, "harpoon"),
			short(Keys.ToggleHelp.Help().Key, "more"),
		)
		return out
	}
	if ctx.HasSelectedItem {
		return append(prefix,
			bindingShortcut(Keys.Peek),
			bindingShortcut(Keys.Scratchpad),
			bindingShortcut(Keys.Edit),
			bindingShortcut(Keys.Append),
			bindingShortcut(Keys.Timeline),
			bindingShortcut(Keys.Delete),
			short(Keys.MoveMenu.Help().Key, "move"),
			short(Keys.MoveNext.Help().Key, "next"),
			bindingShortcut(Keys.TemplateMenu),
			bindingShortcut(Keys.CustomCommands),
			short(Keys.GitPanel.Help().Key, "git"),
			short(Keys.MnemonicJump.Help().Key, "jump"),
			short(Keys.Harpoon.Help().Key, "harpoon"),
			short(Keys.ToggleHelp.Help().Key, "more"),
		)
	}
	return append(prefix,
		short(Keys.New.Help().Key, "new"),
		bindingShortcut(Keys.Scratchpad),
		bindingShortcut(Keys.TemplateMenu),
		bindingShortcut(Keys.Filter),
		short(Keys.RenameCol.Help().Key, "rename col"),
		short(Keys.GitPanel.Help().Key, "git"),
		short(Keys.MnemonicJump.Help().Key, "jump"),
		short(Keys.Harpoon.Help().Key, "harpoon"),
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

	var rows []string
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
	return OverlayFrame{
		Title:  "Config commands",
		Body:   lipgloss.JoinVertical(lipgloss.Left, rows...),
		Footer: helpDimStyle.Render("esc to go back"),
		Border: helpConfigBorder,
	}.Render()
}
