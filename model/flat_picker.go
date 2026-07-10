package model

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// flatPicker adds the common text-entry cursor behavior on top of fuzzyList.
// Screens keep their own close bindings and selected-item actions, while this
// owns the otherwise identical arrow/backspace/type handling.
type flatPicker struct{ fuzzyList }

type flatPickerInput int

const (
	flatPickerInputNone flatPickerInput = iota
	flatPickerInputConfirm
)

func (p *flatPicker) HandleInput(msg tea.KeyPressMsg) flatPickerInput {
	switch msg.Code {
	case tea.KeyUp:
		p.Move(-1)
	case tea.KeyDown:
		p.Move(1)
	case tea.KeyEnter:
		return flatPickerInputConfirm
	case tea.KeyBackspace:
		p.Backspace()
	default:
		p.Append(msg.Text)
	}
	return flatPickerInputNone
}

// flatPickerFilterLine is the common searchable-menu prompt. The caller
// supplies context-specific normal styles while the cursor remains consistent
// across every flat picker.
func flatPickerFilterLine(p Palette, filter string, placeholder, query lipgloss.Style) string {
	cursor := lipgloss.NewStyle().Foreground(p.Highlight).Bold(true).Render("> ")
	if filter == "" {
		return cursor + placeholder.Render("type to filter…")
	}
	return cursor + query.Render(filter)
}

// flatPickerInner gives simple overlays the same bounded minimum width without
// forcing screens with richer layouts (such as the board switcher) into it.
func flatPickerInner(termWidth int, parts ...string) string {
	inner := lipgloss.JoinVertical(lipgloss.Left, parts...)
	minInner := 50
	if termWidth > 0 && termWidth-12 < minInner {
		minInner = termWidth - 12
	}
	if lipgloss.Width(inner) < minInner {
		inner = lipgloss.NewStyle().Width(minInner).Render(inner)
	}
	return inner
}
