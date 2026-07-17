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

type SelectItem struct {
	ID             string
	Label          string
	Description    string
	Icon           string
	Disabled       bool
	DisabledReason string
	Group          string
	Primary        bool
	Destructive    bool
}

type SelectOptions struct {
	Title      string
	Items      []SelectItem
	Searchable bool
	InitialID  string
}

type SelectResult struct {
	ID        string
	Submitted bool
	Cancelled bool
}

type Select struct {
	opts    SelectOptions
	filter  textinput.Model
	visible []int
	cursor  int
	offset  int
	message string
	result  *SelectResult
	active  bool
	size    Size
	palette theme.Palette
	keys    KeyMap
}

func (s *Select) Open(opts SelectOptions) {
	filter := textinput.New()
	filter.Prompt = "/ "
	filter.Placeholder = "Search"
	filter.Focus()
	theme.ApplyTextInputPalette(&filter, s.palette)

	s.opts = opts
	s.filter = filter
	s.result = nil
	s.active = true
	s.keys = DefaultKeyMap()
	s.message = ""
	s.refilter(opts.InitialID)
	s.resizeFilter()
}

func (s *Select) Active() bool { return s.active }

func (s *Select) SetSize(width, height int) {
	s.size.Set(width, height)
	s.resizeFilter()
	s.ensureVisible()
}

func (s *Select) SetPalette(p theme.Palette) {
	s.palette = p
	if s.active && s.opts.Searchable {
		theme.ApplyTextInputPalette(&s.filter, p)
	}
}

func (s *Select) Update(msg tea.Msg) tea.Cmd {
	if !s.active {
		return nil
	}
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case key.Matches(keyMsg, s.keys.Cancel):
			s.finish(SelectResult{Cancelled: true})
			return nil
		case key.Matches(keyMsg, s.keys.Prev) && !(s.opts.Searchable && keyMsg.String() == "k"):
			s.move(-1)
			return nil
		case key.Matches(keyMsg, s.keys.Next) && !(s.opts.Searchable && keyMsg.String() == "j"):
			s.move(1)
			return nil
		case key.Matches(keyMsg, s.keys.Submit):
			s.submit()
			return nil
		}
	}
	if !s.opts.Searchable {
		return nil
	}
	before := s.filter.Value()
	filter, cmd := s.filter.Update(msg)
	s.filter = filter
	if s.filter.Value() != before {
		s.refilter("")
	}
	return cmd
}

func (s *Select) View() string {
	if !s.active {
		return ""
	}
	parts := make([]string, 0, 3)
	if s.opts.Searchable {
		parts = append(parts, s.filter.View())
	}
	parts = append(parts, s.rowsView())
	if s.message != "" {
		parts = append(parts, lipgloss.NewStyle().Foreground(s.palette.Warning).Render(s.message))
	}
	title := s.opts.Title
	if title == "" {
		title = "Select"
	}
	return theme.OverlayFrame{
		Title: title, Body: lipgloss.JoinVertical(lipgloss.Left, parts...),
		Footer:  theme.RenderHints(s.palette, []theme.Hint{{Keys: "↑/↓", Label: "select"}, {Keys: "enter", Label: "confirm"}, {Keys: "esc", Label: "cancel"}}),
		Palette: s.palette, Width: s.frameWidth(),
	}.Render()
}

func (s *Select) TakeResult() (SelectResult, bool) {
	if s.result == nil {
		return SelectResult{}, false
	}
	result := *s.result
	s.result = nil
	return result, true
}

func (s *Select) Close() {
	s.active = false
	s.result = nil
	s.visible = nil
	s.message = ""
	s.filter = textinput.Model{}
}

func (s *Select) SelectedIndex() int {
	if s.cursor < 0 || s.cursor >= len(s.visible) {
		return -1
	}
	return s.visible[s.cursor]
}

func (s *Select) SubmitID(id string) bool {
	for _, index := range s.visible {
		if s.opts.Items[index].ID == id && !s.opts.Items[index].Disabled {
			s.finish(SelectResult{ID: id, Submitted: true})
			return true
		}
	}
	return false
}

func (s *Select) finish(result SelectResult) {
	s.result = &result
	s.active = false
}

func (s *Select) submit() {
	index := s.SelectedIndex()
	if index < 0 {
		s.finish(SelectResult{Cancelled: true})
		return
	}
	item := s.opts.Items[index]
	if item.Disabled {
		s.message = item.DisabledReason
		if s.message == "" {
			s.message = "This item is disabled"
		}
		return
	}
	s.finish(SelectResult{ID: item.ID, Submitted: true})
}

func (s *Select) move(delta int) {
	if len(s.visible) == 0 {
		return
	}
	start := s.cursor
	for step := 1; step <= len(s.visible); step++ {
		next := start + delta*step
		if next < 0 || next >= len(s.visible) {
			break
		}
		if !s.opts.Items[s.visible[next]].Disabled {
			s.cursor = next
			s.message = ""
			s.ensureVisible()
			return
		}
	}
}

func (s *Select) refilter(preferredID string) {
	query := strings.TrimSpace(s.filter.Value())
	if query == "" {
		s.visible = make([]int, len(s.opts.Items))
		for index := range s.opts.Items {
			s.visible[index] = index
		}
	} else {
		haystack := make([]string, len(s.opts.Items))
		for index, item := range s.opts.Items {
			haystack[index] = strings.Join([]string{item.Label, item.Description, item.Group}, " ")
		}
		matches := fuzzy.Find(query, haystack)
		s.visible = make([]int, len(matches))
		for index, match := range matches {
			s.visible[index] = match.Index
		}
	}
	s.cursor = s.firstEnabled()
	if preferredID != "" {
		for cursor, index := range s.visible {
			if s.opts.Items[index].ID == preferredID {
				s.cursor = cursor
				break
			}
		}
	}
	s.offset = 0
	s.message = ""
	s.ensureVisible()
}

func (s *Select) firstEnabled() int {
	for cursor, index := range s.visible {
		if !s.opts.Items[index].Disabled {
			return cursor
		}
	}
	if len(s.visible) > 0 {
		return 0
	}
	return -1
}

func (s *Select) rowsView() string {
	if len(s.visible) == 0 {
		return lipgloss.NewStyle().Foreground(s.palette.FgDim).Render("(no matches)")
	}
	limit := s.visibleLimit()
	end := min(s.offset+limit, len(s.visible))
	rows := make([]string, 0, end-s.offset)
	lastGroup := ""
	for cursor := s.offset; cursor < end; cursor++ {
		item := s.opts.Items[s.visible[cursor]]
		if item.Group != "" && item.Group != lastGroup {
			rows = append(rows, lipgloss.NewStyle().Bold(true).Foreground(s.palette.FgMuted).Render(item.Group))
			lastGroup = item.Group
		}
		rows = append(rows, s.renderItem(item, cursor == s.cursor))
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func (s *Select) renderItem(item SelectItem, selected bool) string {
	label := strings.TrimSpace(strings.TrimSpace(item.Icon + " " + item.Label))
	if item.Description != "" {
		label += "  " + item.Description
	}
	style := lipgloss.NewStyle().Foreground(s.palette.FgBase)
	if item.Disabled {
		style = style.Foreground(s.palette.FgDim)
	} else if item.Destructive {
		style = style.Foreground(s.palette.Danger)
	} else if item.Primary {
		style = style.Foreground(s.palette.Primary).Bold(true)
	}
	gutter := "  "
	if selected {
		gutter = lipgloss.NewStyle().Foreground(s.palette.Primary).Bold(true).Render("▌ ")
		style = style.Foreground(s.palette.FgInverse).Background(s.palette.PrimaryStrong).Bold(true)
		label = " " + label + " "
	}
	return gutter + style.Render(label)
}

func (s *Select) visibleLimit() int {
	if s.size.Height <= 0 {
		return 10
	}
	return max(1, min(12, s.size.Height-10))
}

func (s *Select) ensureVisible() {
	if s.cursor < 0 {
		s.offset = 0
		return
	}
	limit := s.visibleLimit()
	if s.cursor < s.offset {
		s.offset = s.cursor
	}
	if s.cursor >= s.offset+limit {
		s.offset = s.cursor - limit + 1
	}
}

func (s *Select) resizeFilter() {
	width := s.size.Fit(80, 0).Width - 8
	s.filter.SetWidth(max(width, 12))
}

func (s *Select) frameWidth() int { return max(s.size.Fit(80, 0).Width-2, 0) }
