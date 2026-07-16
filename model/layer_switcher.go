package model

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"kbrd/script"
)

type switchLayerMsg struct{ ID string }

type LayerSwitcher struct {
	active  bool
	layers  []script.LayerInfo
	current string
	palette Palette
	flatPicker
}

func (s *LayerSwitcher) Open(layers []script.LayerInfo, current string) {
	s.active = len(layers) > 0
	s.layers = append(s.layers[:0], layers...)
	s.current = current
	s.fuzzyList.Reset(len(s.layers), 0, s.haystack)
	for i, match := range s.matches {
		if s.layers[match.Index].ID == current {
			s.Select(i)
			break
		}
	}
}

func (s *LayerSwitcher) Close() {
	s.active = false
	s.layers = nil
	s.current = ""
	s.fuzzyList.Clear()
}

func (s *LayerSwitcher) Active() bool { return s.active }

func (s *LayerSwitcher) haystack(i int) string {
	layer := s.layers[i]
	return strings.TrimSpace(layer.Name + " " + layer.ID + " " + layer.Description)
}

func (s *LayerSwitcher) Update(msg tea.KeyPressMsg) tea.Cmd {
	if key.Matches(msg, Keys.LayerSwitcherClose) {
		s.Close()
		return nil
	}
	if s.flatPicker.HandleInput(msg) != flatPickerInputConfirm {
		return nil
	}
	index, ok := s.SelectedIndex()
	if !ok {
		return nil
	}
	id := s.layers[index].ID
	s.Close()
	return func() tea.Msg { return switchLayerMsg{ID: id} }
}

func (s *LayerSwitcher) View(termWidth, _ int) string {
	p := s.palette
	nameStyle := lipgloss.NewStyle().Foreground(p.FgBase)
	descStyle := lipgloss.NewStyle().Foreground(p.FgMuted)
	selectedStyle := lipgloss.NewStyle().Bold(true).Foreground(p.FgInverse).Background(p.Primary)
	highlightStyle := lipgloss.NewStyle().Bold(true).Foreground(p.Highlight)
	highlightSelected := lipgloss.NewStyle().Bold(true).Foreground(p.Highlight).Background(p.Primary)
	activeMark := lipgloss.NewStyle().Bold(true).Foreground(p.Success).Render("●")
	inactiveMark := lipgloss.NewStyle().Foreground(p.FgDim).Render("·")
	defaultMark := lipgloss.NewStyle().Foreground(p.WarningSoft).Render("★")

	rows := make([]string, 0, len(s.matches))
	for i, match := range s.matches {
		layer := s.layers[match.Index]
		selected := i == s.selected
		nameIndexes, descIndexes := layerMatchIndexes(layer, match.MatchedIndexes)
		baseName, baseDesc := nameStyle, descStyle
		hiName, hiDesc := highlightStyle, highlightStyle
		if selected {
			baseName, baseDesc = selectedStyle, selectedStyle
			hiName, hiDesc = highlightSelected, highlightSelected
		}
		mark := inactiveMark
		if layer.ID == s.current {
			mark = activeMark
		}
		star := " "
		if layer.Default {
			star = defaultMark
		}
		row := mark + " " + star + " " + renderHighlighted(layer.Name, nameIndexes, baseName, hiName)
		if layer.Description != "" {
			separator := "  —  "
			if selected {
				separator = selectedStyle.Render(separator)
			}
			row += separator + renderHighlighted(layer.Description, descIndexes, baseDesc, hiDesc)
		}
		if selected {
			row = selectedStyle.Render(" ") + row + selectedStyle.Render(" ")
		} else {
			row = " " + row + " "
		}
		rows = append(rows, row)
	}
	list := helpDimStyle.Render("no matches")
	if len(rows) > 0 {
		list = lipgloss.JoinVertical(lipgloss.Left, rows...)
	}
	list = lipgloss.NewStyle().Height(max(len(s.layers), 1)).Render(list)
	filter := flatPickerFilterLine(p, s.filter, descStyle, nameStyle)
	body := flatPickerInner(termWidth, filter, "", list)
	footer := RenderInlineHints([]Shortcut{
		{Keys: "type", Label: "filter"},
		{Keys: "↑/↓", Label: "select"},
		{Keys: "enter", Label: "switch"},
		{Keys: "esc", Label: "cancel"},
	})
	return OverlayFrame{
		Title:   "Switch layer",
		Body:    body,
		Footer:  footer,
		Width:   overlayWidthForBody(s.contentWidth(termWidth, footer)),
		Palette: p,
	}.Render()
}

func (s *LayerSwitcher) contentWidth(termWidth int, footer string) int {
	textW := max(50, lipgloss.Width(footer))
	for _, layer := range s.layers {
		row := "·   " + layer.Name
		if layer.Description != "" {
			row += "  —  " + layer.Description
		}
		// Rows reserve one background-padding cell on each side whether selected
		// or not, so moving the cursor cannot change their rendered dimensions.
		textW = max(textW, lipgloss.Width(row)+2)
	}
	if termWidth > 0 {
		textW = min(textW, max(termWidth-8, 1))
	}
	return max(textW, 1)
}

func layerMatchIndexes(layer script.LayerInfo, indexes []int) (name, description []int) {
	nameLen := len([]rune(layer.Name))
	idLen := len([]rune(layer.ID))
	descriptionOffset := nameLen + 1 + idLen + 1
	for _, index := range indexes {
		switch {
		case index < nameLen:
			name = append(name, index)
		case index >= descriptionOffset:
			description = append(description, index-descriptionOffset)
		}
	}
	return name, description
}

func (b *Board) openLayerSwitcher() tea.Cmd {
	if b.scripts == nil {
		return nil
	}
	layers := b.scripts.Layers()
	if len(layers) == 0 {
		return nil
	}
	active, _ := b.scripts.ActiveLayer()
	b.layerSwitcher.Open(layers, active.ID)
	return nil
}

func (b *Board) handleSwitchLayer(msg switchLayerMsg) (tea.Model, tea.Cmd) {
	if b.scripts == nil {
		return b, nil
	}
	selectedVID := ""
	if b.selectedCol >= 0 && b.selectedCol < len(b.columns) && b.columns[b.selectedCol].Virtual {
		selectedVID = b.columns[b.selectedCol].VID
	}
	if err := b.scripts.ActivateLayer(msg.ID); err != nil {
		return b, b.notifier.ErrorCause("failed to switch layer", err)
	}
	b.loadCommands()
	if selectedVID != "" && b.virtualColumn(selectedVID) == nil {
		b.zoom.Off()
		b.clampSelectedCol()
	}
	active, _ := b.scripts.ActiveLayer()
	return b, b.notifier.Success("switched to layer " + active.Name)
}
