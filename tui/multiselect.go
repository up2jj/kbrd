package tui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/sahilm/fuzzy"

	"kbrd/theme"
)

type MultiSelectOptions struct {
	Title      string
	Items      []SelectItem
	Searchable bool
	InitialIDs []string
}

type MultiSelectResult struct {
	IDs       []string
	Submitted bool
	Cancelled bool
}

// MultiSelect is a stable-ID list control with filtering and bounded scrolling.
type MultiSelect struct {
	opts     MultiSelectOptions
	filter   textinput.Model
	visible  []int
	selected map[string]bool
	cursor   int
	offset   int
	message  string
	result   *MultiSelectResult
	active   bool
	size     Size
	palette  theme.Palette
	keys     KeyMap
}

func (m *MultiSelect) Open(opts MultiSelectOptions) {
	filter := textinput.New()
	filter.Prompt = "/ "
	filter.Placeholder = "Search"
	filter.Focus()
	theme.ApplyTextInputPalette(&filter, m.palette)

	m.opts = opts
	m.filter = filter
	m.selected = make(map[string]bool, len(opts.InitialIDs))
	for _, id := range opts.InitialIDs {
		m.selected[id] = true
	}
	m.result = nil
	m.active = true
	m.keys = DefaultKeyMap()
	m.message = ""
	m.refilter()
	m.resizeFilter()
}

func (m *MultiSelect) Active() bool { return m.active }

func (m *MultiSelect) SetSize(width, height int) {
	m.size.Set(width, height)
	m.resizeFilter()
	m.ensureVisible()
}

func (m *MultiSelect) SetPalette(p theme.Palette) {
	m.palette = p
	if m.active && m.opts.Searchable {
		theme.ApplyTextInputPalette(&m.filter, p)
	}
}

func (m *MultiSelect) Update(msg tea.Msg) tea.Cmd {
	if !m.active {
		return nil
	}
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case key.Matches(keyMsg, m.keys.Cancel):
			m.finish(MultiSelectResult{Cancelled: true})
			return nil
		case key.Matches(keyMsg, m.keys.Prev) && !(m.opts.Searchable && keyMsg.String() == "k"):
			m.move(-1)
			return nil
		case key.Matches(keyMsg, m.keys.Next) && !(m.opts.Searchable && keyMsg.String() == "j"):
			m.move(1)
			return nil
		case keyMsg.Code == tea.KeySpace:
			m.toggle()
			return nil
		case key.Matches(keyMsg, m.keys.Submit):
			m.finish(MultiSelectResult{IDs: m.selectedIDs(), Submitted: true})
			return nil
		}
	}
	if !m.opts.Searchable {
		return nil
	}
	before := m.filter.Value()
	filter, cmd := m.filter.Update(msg)
	m.filter = filter
	if m.filter.Value() != before {
		m.refilter()
	}
	return cmd
}

func (m *MultiSelect) View() string {
	if !m.active {
		return ""
	}
	parts := make([]string, 0, 3)
	if m.opts.Searchable {
		parts = append(parts, m.filter.View())
	}
	parts = append(parts, m.rowsView())
	if m.message != "" {
		parts = append(parts, lipgloss.NewStyle().Foreground(m.palette.Warning).Render(m.message))
	}
	title := m.opts.Title
	if title == "" {
		title = "Select"
	}
	return theme.OverlayFrame{
		Title: title, Body: lipgloss.JoinVertical(lipgloss.Left, parts...),
		Footer: theme.RenderHints(m.palette, []theme.Hint{
			{Keys: "↑/↓", Label: "move"}, {Keys: "space", Label: "toggle"},
			{Keys: "enter", Label: "confirm"}, {Keys: "esc", Label: "cancel"},
		}),
		Palette: m.palette, Width: m.frameWidth(),
	}.Render()
}

func (m *MultiSelect) TakeResult() (MultiSelectResult, bool) {
	if m.result == nil {
		return MultiSelectResult{}, false
	}
	result := *m.result
	m.result = nil
	return result, true
}

func (m *MultiSelect) Close() {
	m.active = false
	m.result = nil
	m.visible = nil
	m.selected = nil
	m.message = ""
	m.filter = textinput.Model{}
}

func (m *MultiSelect) SelectedIndex() int {
	if m.cursor < 0 || m.cursor >= len(m.visible) {
		return -1
	}
	return m.visible[m.cursor]
}

func (m *MultiSelect) SelectedIDs() []string { return m.selectedIDs() }

func (m *MultiSelect) finish(result MultiSelectResult) {
	m.result = &result
	m.active = false
}

func (m *MultiSelect) toggle() {
	index := m.SelectedIndex()
	if index < 0 {
		return
	}
	item := m.opts.Items[index]
	if item.Disabled {
		m.message = item.DisabledReason
		if m.message == "" {
			m.message = "This item is disabled"
		}
		return
	}
	m.selected[item.ID] = !m.selected[item.ID]
	m.message = ""
}

func (m *MultiSelect) selectedIDs() []string {
	ids := make([]string, 0, len(m.selected))
	for _, item := range m.opts.Items {
		if m.selected[item.ID] {
			ids = append(ids, item.ID)
		}
	}
	return ids
}

func (m *MultiSelect) move(delta int) {
	if len(m.visible) == 0 {
		return
	}
	next := m.cursor + delta
	if next < 0 || next >= len(m.visible) {
		return
	}
	m.cursor = next
	m.message = ""
	m.ensureVisible()
}

func (m *MultiSelect) refilter() {
	query := strings.TrimSpace(m.filter.Value())
	if query == "" {
		m.visible = make([]int, len(m.opts.Items))
		for index := range m.opts.Items {
			m.visible[index] = index
		}
	} else {
		haystack := make([]string, len(m.opts.Items))
		for index, item := range m.opts.Items {
			haystack[index] = strings.Join([]string{item.Label, item.Description, item.Group}, " ")
		}
		matches := fuzzy.Find(query, haystack)
		m.visible = make([]int, len(matches))
		for index, match := range matches {
			m.visible[index] = match.Index
		}
	}
	if len(m.visible) == 0 {
		m.cursor = -1
	} else {
		m.cursor = 0
	}
	m.offset = 0
	m.message = ""
	m.ensureVisible()
}

func (m *MultiSelect) rowsView() string {
	if len(m.visible) == 0 {
		return lipgloss.NewStyle().Foreground(m.palette.FgDim).Render("(no matches)")
	}
	limit := m.visibleLimit()
	end := min(m.offset+limit, len(m.visible))
	rows := make([]string, 0, end-m.offset)
	lastGroup := ""
	for cursor := m.offset; cursor < end; cursor++ {
		item := m.opts.Items[m.visible[cursor]]
		if item.Group != "" && item.Group != lastGroup {
			rows = append(rows, lipgloss.NewStyle().Bold(true).Foreground(m.palette.FgMuted).Render(item.Group))
			lastGroup = item.Group
		}
		rows = append(rows, m.renderItem(item, cursor == m.cursor))
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func (m *MultiSelect) renderItem(item SelectItem, focused bool) string {
	mark := "○"
	if m.selected[item.ID] {
		mark = "●"
	}
	label := strings.TrimSpace(strings.TrimSpace(item.Icon + " " + item.Label))
	if item.Description != "" {
		label += "  " + item.Description
	}
	style := lipgloss.NewStyle().Foreground(m.palette.FgBase)
	if item.Disabled {
		style = style.Foreground(m.palette.FgDim)
	}
	if focused {
		style = style.Foreground(m.palette.FgInverse).Background(m.palette.PrimaryStrong).Bold(true)
		label = " " + label + " "
	}
	return lipgloss.NewStyle().Foreground(m.palette.Primary).Render(mark) + " " + style.Render(label)
}

func (m *MultiSelect) visibleLimit() int {
	if m.size.Height <= 0 {
		return 10
	}
	return max(1, min(12, m.size.Height-10))
}

func (m *MultiSelect) ensureVisible() {
	if m.cursor < 0 {
		m.offset = 0
		return
	}
	limit := m.visibleLimit()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+limit {
		m.offset = m.cursor - limit + 1
	}
}

func (m *MultiSelect) resizeFilter() {
	width := m.size.Fit(80, 0).Width - 8
	m.filter.SetWidth(max(width, 12))
}

func (m *MultiSelect) frameWidth() int { return max(m.size.Fit(80, 0).Width-2, 0) }
