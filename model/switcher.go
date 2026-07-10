package model

import (
	"os"
	"sort"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"kbrd/recents"
)

type switchBoardMsg struct {
	Path string
}

type pinBoardMsg struct {
	Path   string
	Name   string
	Pinned bool
}

type removeBoardMsg struct {
	Path string
}

type Switcher struct {
	active  bool
	entries []recents.Entry
	flatPicker
	activePath string
	palette    Palette
}

func (s *Switcher) Open(entries []recents.Entry, activePath string) {
	s.active = true
	s.entries = sortPinnedFirst(entries)
	s.activePath = activePath
	s.fuzzyList.Reset(len(s.entries), 0, s.haystack)
	for i, m := range s.matches {
		if s.entries[m.Index].Path == activePath {
			s.fuzzyList.Select(i)
			break
		}
	}
}

func (s *Switcher) Close() {
	s.active = false
	s.entries = nil
	s.fuzzyList.Clear()
	s.activePath = ""
}

func (s *Switcher) Active() bool { return s.active }

// sortPinnedFirst returns a stable reordering with all pinned entries before
// unpinned ones; relative order within each group is preserved.
func sortPinnedFirst(in []recents.Entry) []recents.Entry {
	out := make([]recents.Entry, len(in))
	copy(out, in)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Pinned && !out[j].Pinned
	})
	return out
}

func (s *Switcher) haystack(i int) string {
	e := s.entries[i]
	if e.Name != "" {
		return "[" + e.Name + "] " + e.Path
	}
	return e.Path
}

func (s *Switcher) Update(msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(msg, Keys.SwitcherClose):
		s.Close()
		return nil
	case key.Matches(msg, Keys.SwitcherPrev), key.Matches(msg, Keys.SwitcherNext):
		s.flatPicker.HandleInput(msg)
		return nil
	case key.Matches(msg, Keys.SwitcherPinToggle):
		index, ok := s.fuzzyList.SelectedIndex()
		if !ok {
			return nil
		}
		e := s.entries[index]
		newPinned := !e.Pinned
		return func() tea.Msg {
			return pinBoardMsg{Path: e.Path, Name: e.Name, Pinned: newPinned}
		}
	case key.Matches(msg, Keys.SwitcherConfirm):
		index, ok := s.fuzzyList.SelectedIndex()
		if !ok {
			s.Close()
			return nil
		}
		chosen := s.entries[index]
		s.Close()
		return func() tea.Msg { return switchBoardMsg{Path: chosen.Path} }
	}
	switch msg.Code {
	case tea.KeyBackspace:
		if s.fuzzyList.Backspace() {
			return nil
		}
		index, ok := s.fuzzyList.SelectedIndex()
		if !ok {
			return nil
		}
		e := s.entries[index]
		return func() tea.Msg { return removeBoardMsg{Path: e.Path} }
	default:
		// Arrow movement and typing are shared with the other flat pickers.
		s.flatPicker.HandleInput(msg)
		return nil
	}
}

func (s *Switcher) View() string {
	p := s.palette
	nameStyle := lipgloss.NewStyle().Foreground(p.Highlight)
	pathStyle := lipgloss.NewStyle().Foreground(p.FgMuted)
	descStyle := lipgloss.NewStyle().Foreground(p.FgMuted)
	selStyle := lipgloss.NewStyle().Bold(true).Foreground(p.FgInverse).Background(p.Primary)
	hiStyle := lipgloss.NewStyle().Foreground(p.Highlight).Bold(true)
	hiSelStyle := lipgloss.NewStyle().Bold(true).Foreground(p.Highlight).Background(p.Primary)
	activeMark := lipgloss.NewStyle().Foreground(p.Success).Bold(true).Render("●")
	dimMark := lipgloss.NewStyle().Foreground(p.FgDim).Render("·")
	pinMark := lipgloss.NewStyle().Foreground(p.WarningSoft).Bold(true).Render("▎")
	pinSpace := " "
	gutterSel := lipgloss.NewStyle().Foreground(p.Primary).Bold(true).Render("▌")
	missingStyle := lipgloss.NewStyle().Foreground(p.Danger).Italic(true)
	filterLine := flatPickerFilterLine(p, s.filter, descStyle, nameStyle)

	var body string
	switch {
	case len(s.entries) == 0:
		body = helpDimStyle.Render("no recent boards")
	case len(s.matches) == 0:
		body = helpDimStyle.Render("no matches")
	default:
		rows := make([]string, 0, len(s.matches))
		for i, m := range s.matches {
			e := s.entries[m.Index]
			selected := i == s.selected

			// haystack layout: "[name] path"  OR  "path"
			// rune offsets in MatchedIndexes are into that string.
			var nameIdx, pathIdx []int
			var nameLen, pathOffset int
			if e.Name != "" {
				nameLen = len([]rune("[" + e.Name + "]"))
				pathOffset = nameLen + 1 // separator space
			}
			for _, idx := range m.MatchedIndexes {
				if e.Name != "" && idx < nameLen {
					nameIdx = append(nameIdx, idx)
				} else if idx >= pathOffset {
					pathIdx = append(pathIdx, idx-pathOffset)
				}
			}

			nameBase := nameStyle
			pathBase := pathStyle
			hiName := hiStyle
			hiPath := hiStyle
			if selected {
				nameBase = selStyle
				pathBase = selStyle
				hiName = hiSelStyle
				hiPath = hiSelStyle
			}

			var styled string
			if e.Name != "" {
				styled = renderHighlighted("["+e.Name+"]", nameIdx, nameBase, hiName)
				if selected {
					styled += selStyle.Render(" ")
				} else {
					styled += " "
				}
			}
			styled += renderHighlighted(e.Path, pathIdx, pathBase, hiPath)

			if !e.Pinned {
				if info, err := os.Stat(e.Path); err != nil || !info.IsDir() {
					tag := "  (missing)"
					if selected {
						styled += selStyle.Render(tag)
					} else {
						styled += missingStyle.Render(tag)
					}
				}
			}

			mark := dimMark
			if e.Path == s.activePath {
				mark = activeMark
			}
			pin := pinSpace
			if e.Pinned {
				pin = pinMark
			}
			gutter := " "
			if selected {
				gutter = gutterSel
				styled = selStyle.Render(" ") + styled + selStyle.Render(" ")
			}
			rows = append(rows, gutter+pin+mark+" "+styled)
		}
		body = lipgloss.JoinVertical(lipgloss.Left, rows...)
	}

	footer := RenderInlineHints([]Shortcut{
		{Keys: "type", Label: "filter"},
		{Keys: "↑/↓", Label: "select"},
		{Keys: "enter", Label: "switch"},
		{Keys: "tab", Label: "pin/unpin"},
		{Keys: "bksp", Label: "remove"},
		{Keys: "esc", Label: "cancel"},
	})
	body = lipgloss.JoinVertical(lipgloss.Left, filterLine, "", body)
	return OverlayFrame{Title: "Switch board", Body: body, Footer: footer, Palette: p}.Render()
}
