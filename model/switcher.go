package model

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"kbrd/recents"
)

type switchBoardMsg struct {
	Path string
}

type Switcher struct {
	active     bool
	entries    []recents.Entry
	selected   int
	activePath string
}

func (s *Switcher) Open(entries []recents.Entry, activePath string) {
	s.active = true
	s.entries = entries
	s.activePath = activePath
	s.selected = 0
	for i, e := range entries {
		if e.Path == activePath {
			s.selected = i
			break
		}
	}
}

func (s *Switcher) Close() {
	s.active = false
	s.entries = nil
	s.selected = 0
	s.activePath = ""
}

func (s *Switcher) Active() bool { return s.active }

func (s *Switcher) Update(msg tea.KeyMsg) tea.Cmd {
	switch {
	case key.Matches(msg, Keys.SwitcherClose):
		s.Close()
		return nil
	case key.Matches(msg, Keys.SwitcherPrev):
		if s.selected > 0 {
			s.selected--
		}
	case key.Matches(msg, Keys.SwitcherNext):
		if s.selected < len(s.entries)-1 {
			s.selected++
		}
	case key.Matches(msg, Keys.SwitcherConfirm):
		if len(s.entries) == 0 {
			s.Close()
			return nil
		}
		chosen := s.entries[s.selected]
		s.Close()
		return func() tea.Msg { return switchBoardMsg{Path: chosen.Path} }
	}
	return nil
}

func (s *Switcher) View() string {
	title := helpTitleStyle.Render("Switch board")
	var body string
	if len(s.entries) == 0 {
		body = helpDimStyle.Render("no recent boards")
	} else {
		nameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#fde047"))
		pathStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#94a3b8"))
		selStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#0f172a")).Background(lipgloss.Color("#60a5fa"))
		activeMark := lipgloss.NewStyle().Foreground(lipgloss.Color("#22c55e")).Bold(true).Render("●")
		dimMark := lipgloss.NewStyle().Foreground(lipgloss.Color("#475569")).Render("·")
		gutterSel := lipgloss.NewStyle().Foreground(lipgloss.Color("#60a5fa")).Bold(true).Render("▌")

		rows := make([]string, 0, len(s.entries))
		for i, e := range s.entries {
			mark := dimMark
			if e.Path == s.activePath {
				mark = activeMark
			}
			gutter := " "
			if i == s.selected {
				gutter = gutterSel
			}
			if i == s.selected {
				rows = append(rows, gutter+" "+mark+" "+selStyle.Render(" "+entryPlain(e)+" "))
				continue
			}
			var line string
			if e.Name != "" {
				line = nameStyle.Render("["+e.Name+"]") + " " + pathStyle.Render(e.Path)
			} else {
				line = pathStyle.Render(e.Path)
			}
			rows = append(rows, gutter+" "+mark+" "+line)
		}
		body = lipgloss.JoinVertical(lipgloss.Left, rows...)
	}

	footer := helpDimStyle.Render("↑/↓ select · enter switch · esc cancel")
	content := lipgloss.JoinVertical(lipgloss.Left, title, "", body, "", footer)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#3b82f6")).
		Padding(1, 3).
		Render(content)
}

func entryPlain(e recents.Entry) string {
	if e.Name != "" {
		return "[" + e.Name + "] " + e.Path
	}
	return e.Path
}
