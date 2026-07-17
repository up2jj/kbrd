package tui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"kbrd/theme"
)

type ConfirmOptions struct {
	Title        string
	Message      string
	Detail       []string
	ConfirmLabel string
	RejectLabel  string
	Default      bool
	Destructive  bool
}

type ConfirmResult struct {
	Value     bool
	Submitted bool
	Cancelled bool
}

type Confirm struct {
	opts     ConfirmOptions
	selected bool
	result   *ConfirmResult
	active   bool
	size     Size
	palette  theme.Palette
	keys     KeyMap
	prev     key.Binding
	next     key.Binding
}

func (c *Confirm) Open(opts ConfirmOptions) {
	if opts.ConfirmLabel == "" {
		opts.ConfirmLabel = "Yes"
	}
	if opts.RejectLabel == "" {
		opts.RejectLabel = "No"
	}
	c.opts = opts
	c.selected = opts.Default
	c.result = nil
	c.active = true
	c.keys = DefaultKeyMap()
	c.prev = key.NewBinding(key.WithKeys("left", "h", "up", "k"))
	c.next = key.NewBinding(key.WithKeys("right", "l", "down", "j", "tab"))
}

func (c *Confirm) Active() bool { return c.active }

func (c *Confirm) SetSize(width, height int) { c.size.Set(width, height) }

func (c *Confirm) SetPalette(p theme.Palette) { c.palette = p }

func (c *Confirm) Update(msg tea.Msg) tea.Cmd {
	if !c.active {
		return nil
	}
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	switch {
	case key.Matches(keyMsg, c.keys.Cancel):
		c.finish(ConfirmResult{Cancelled: true})
	case key.Matches(keyMsg, c.prev):
		c.selected = true
	case key.Matches(keyMsg, c.next):
		c.selected = false
	case key.Matches(keyMsg, c.keys.Submit):
		c.finish(ConfirmResult{Value: c.selected, Submitted: true})
	}
	return nil
}

func (c *Confirm) View() string {
	if !c.active {
		return ""
	}
	parts := make([]string, 0, 3)
	if c.opts.Message != "" {
		parts = append(parts, lipgloss.NewStyle().Foreground(c.palette.FgBase).Render(c.opts.Message))
	}
	if len(c.opts.Detail) > 0 {
		parts = append(parts, lipgloss.NewStyle().Foreground(c.palette.FgMuted).Render(strings.Join(c.opts.Detail, "\n")))
	}
	parts = append(parts, c.buttonsView())
	title := c.opts.Title
	if title == "" {
		title = "Confirm?"
	}
	return theme.OverlayFrame{
		Title: title, Body: lipgloss.JoinVertical(lipgloss.Left, parts...),
		Footer:  theme.RenderHints(c.palette, []theme.Hint{{Keys: "←/→", Label: "choose"}, {Keys: "enter", Label: "confirm"}, {Keys: "esc", Label: "cancel"}}),
		Palette: c.palette, Width: max(c.size.Fit(72, 0).Width-2, 0),
	}.Render()
}

func (c *Confirm) TakeResult() (ConfirmResult, bool) {
	if c.result == nil {
		return ConfirmResult{}, false
	}
	result := *c.result
	c.result = nil
	return result, true
}

func (c *Confirm) Close() {
	c.active = false
	c.result = nil
}

func (c *Confirm) finish(result ConfirmResult) {
	c.result = &result
	c.active = false
}

func (c *Confirm) buttonsView() string {
	confirmColor := c.palette.Primary
	if c.opts.Destructive {
		confirmColor = c.palette.Danger
	}
	button := func(label string, selected bool, color theme.Color) string {
		style := lipgloss.NewStyle().Padding(0, 1).Foreground(color).Bold(true)
		if selected {
			style = style.Foreground(c.palette.FgOnAccent).Background(color)
		}
		return style.Render(label)
	}
	confirm := button(c.opts.ConfirmLabel, c.selected, confirmColor)
	reject := button(c.opts.RejectLabel, !c.selected, c.palette.FgMuted)
	return lipgloss.JoinHorizontal(lipgloss.Top, confirm, "  ", reject)
}
