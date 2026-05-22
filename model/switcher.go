package model

import (
	"os"
	"sort"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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

type Switcher struct {
	active     bool
	entries    []recents.Entry
	matches    []FuzzyMatch
	filter     string
	selected   int
	activePath string
}

func (s *Switcher) Open(entries []recents.Entry, activePath string) {
	s.active = true
	s.entries = sortPinnedFirst(entries)
	s.activePath = activePath
	s.filter = ""
	s.selected = 0
	s.recompute()
	for i, m := range s.matches {
		if s.entries[m.Index].Path == activePath {
			s.selected = i
			break
		}
	}
}

func (s *Switcher) Close() {
	s.active = false
	s.entries = nil
	s.matches = nil
	s.filter = ""
	s.selected = 0
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

func (s *Switcher) recompute() {
	s.matches = filterFuzzy(len(s.entries), s.filter, s.haystack)
	if s.selected >= len(s.matches) {
		s.selected = len(s.matches) - 1
	}
	if s.selected < 0 {
		s.selected = 0
	}
}

func (s *Switcher) Update(msg tea.KeyMsg) tea.Cmd {
	switch {
	case key.Matches(msg, Keys.SwitcherClose):
		s.Close()
		return nil
	case key.Matches(msg, Keys.SwitcherPrev):
		if s.selected > 0 {
			s.selected--
		}
		return nil
	case key.Matches(msg, Keys.SwitcherNext):
		if s.selected < len(s.matches)-1 {
			s.selected++
		}
		return nil
	case key.Matches(msg, Keys.SwitcherPinToggle):
		if len(s.matches) == 0 {
			return nil
		}
		e := s.entries[s.matches[s.selected].Index]
		newPinned := !e.Pinned
		return func() tea.Msg {
			return pinBoardMsg{Path: e.Path, Name: e.Name, Pinned: newPinned}
		}
	case key.Matches(msg, Keys.SwitcherConfirm):
		if len(s.matches) == 0 {
			s.Close()
			return nil
		}
		chosen := s.entries[s.matches[s.selected].Index]
		s.Close()
		return func() tea.Msg { return switchBoardMsg{Path: chosen.Path} }
	}
	switch msg.Type {
	case tea.KeyBackspace:
		if r := []rune(s.filter); len(r) > 0 {
			s.filter = string(r[:len(r)-1])
			s.recompute()
		}
		return nil
	case tea.KeyRunes, tea.KeySpace:
		str := msg.String()
		if str != "" {
			s.filter += str
			s.selected = 0
			s.recompute()
		}
		return nil
	}
	return nil
}

func (s *Switcher) View() string {
	title := helpTitleStyle.Render("Switch board")

	nameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#fde047"))
	pathStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#94a3b8"))
	descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#94a3b8"))
	selStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#0f172a")).Background(lipgloss.Color("#60a5fa"))
	hiStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#fde047")).Bold(true)
	hiSelStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#fde047")).Background(lipgloss.Color("#60a5fa"))
	activeMark := lipgloss.NewStyle().Foreground(lipgloss.Color("#22c55e")).Bold(true).Render("●")
	dimMark := lipgloss.NewStyle().Foreground(lipgloss.Color("#475569")).Render("·")
	pinMark := lipgloss.NewStyle().Foreground(lipgloss.Color("#fbbf24")).Bold(true).Render("▎")
	pinSpace := " "
	gutterSel := lipgloss.NewStyle().Foreground(lipgloss.Color("#60a5fa")).Bold(true).Render("▌")
	missingStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#ef4444")).Italic(true)
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#fde047")).Bold(true)

	cursor := keyStyle.Render("> ")
	filterText := s.filter
	if filterText == "" {
		filterText = descStyle.Render("type to filter…")
	} else {
		filterText = nameStyle.Render(filterText)
	}
	filterLine := cursor + filterText

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
		{Keys: "esc", Label: "cancel"},
	})
	content := lipgloss.JoinVertical(lipgloss.Left, title, "", filterLine, "", body, "", footer)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#3b82f6")).
		Padding(1, 3).
		Render(content)
}
