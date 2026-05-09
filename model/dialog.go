package model

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type DialogButton struct {
	Label   string
	Primary bool
	Danger  bool
	Msg     tea.Msg
}

type Dialog struct {
	active   bool
	title    string
	body     string
	buttons  []DialogButton
	selected int
}

func (d *Dialog) Open(title, body string, buttons []DialogButton) {
	d.active = true
	d.title = title
	d.body = body
	d.buttons = buttons
	d.selected = 0
}

func (d *Dialog) Close() {
	d.active = false
}

func (d *Dialog) Update(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "left", "h", "shift+tab":
		if d.selected > 0 {
			d.selected--
		}
	case "right", "l", "tab":
		if d.selected < len(d.buttons)-1 {
			d.selected++
		}
	case "enter":
		chosen := d.buttons[d.selected]
		d.Close()
		if chosen.Msg != nil {
			return func() tea.Msg { return chosen.Msg }
		}
	case "esc":
		d.Close()
	}
	return nil
}

func (d *Dialog) View() string {
	if !d.active {
		return ""
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#f1f5f9"))
	bodyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#94a3b8"))

	btnBase := lipgloss.NewStyle().Padding(0, 3)
	activeDanger := btnBase.Bold(true).Background(lipgloss.Color("#ef4444")).Foreground(lipgloss.Color("#ffffff"))
	activePrimary := btnBase.Bold(true).Background(lipgloss.Color("#3b82f6")).Foreground(lipgloss.Color("#ffffff"))
	inactive := btnBase.Foreground(lipgloss.Color("#64748b"))

	btnViews := make([]string, len(d.buttons))
	for i, btn := range d.buttons {
		if i == d.selected {
			if btn.Danger {
				btnViews[i] = activeDanger.Render(btn.Label)
			} else {
				btnViews[i] = activePrimary.Render(btn.Label)
			}
		} else {
			btnViews[i] = inactive.Render(btn.Label)
		}
	}

	// interleave buttons with spacing
	btnRow := btnViews[0]
	for _, b := range btnViews[1:] {
		btnRow = lipgloss.JoinHorizontal(lipgloss.Center, btnRow, "   ", b)
	}

	content := lipgloss.JoinVertical(lipgloss.Center,
		titleStyle.Render(d.title),
		"",
		bodyStyle.Render(d.body),
		"",
		btnRow,
		"",
		RenderInlineHints([]Shortcut{
			{"←/→", "select"},
			{"enter", "confirm"},
			{"esc", "cancel"},
		}),
	)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#475569")).
		Padding(1, 4).
		Render(content)
}
