package model

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// scriptResumeMsg carries the user's response back to the Lua coroutine that
// asked for it. Result is the value passed to coroutine.resume — a string
// for pick/prompt, a bool for confirm, or nil on cancel.
type scriptResumeMsg struct {
	Name   string // command name, for error reporting
	Token  string // coroutine token returned by Host.RunCommand / ResumeWith
	Result interface{}
}

// scriptUIKind matches script.UIRequest.Kind.
type scriptUIKind int

const (
	scriptUINone scriptUIKind = iota
	scriptUIPick
	scriptUIPrompt
)

// ScriptUI holds the state for a single in-flight kbrd.ui.pick / prompt call.
// kbrd.ui.confirm is delegated to the existing Dialog primitive, so it doesn't
// appear here.
type ScriptUI struct {
	kind     scriptUIKind
	name     string
	token    string
	title    string
	choices  []string
	selected int
	input    textinput.Model
	palette  Palette
}

// SetPalette updates the UI's palette and restyles the active input.
func (s *ScriptUI) SetPalette(p Palette) {
	s.palette = p
	if s.input.Prompt != "" {
		applyInputPalette(&s.input, p)
	}
}

func (s *ScriptUI) Active() bool { return s.kind != scriptUINone }

func (s *ScriptUI) Close() {
	s.kind = scriptUINone
	s.name = ""
	s.token = ""
	s.title = ""
	s.choices = nil
	s.selected = 0
	s.input = textinput.Model{}
}

func (s *ScriptUI) OpenPicker(name, token, title string, choices []string) {
	s.kind = scriptUIPick
	s.name = name
	s.token = token
	s.title = title
	s.choices = choices
	s.selected = 0
}

func (s *ScriptUI) OpenPrompt(name, token, title, def string) {
	ti := textinput.New()
	ti.Prompt = "› "
	ti.CharLimit = 256
	ti.Width = 50
	ti.SetValue(def)
	ti.Focus()
	applyInputPalette(&ti, s.palette)

	s.kind = scriptUIPrompt
	s.name = name
	s.token = token
	s.title = title
	s.input = ti
}

// Update routes a key event and, when the user resolves the UI, returns a
// tea.Cmd that emits a scriptResumeMsg with the appropriate result.
func (s *ScriptUI) Update(msg tea.KeyMsg) tea.Cmd {
	switch s.kind {
	case scriptUIPick:
		return s.updatePicker(msg)
	case scriptUIPrompt:
		return s.updatePrompt(msg)
	}
	return nil
}

func (s *ScriptUI) updatePicker(msg tea.KeyMsg) tea.Cmd {
	switch {
	case key.Matches(msg, Keys.SwitcherClose):
		name, token := s.name, s.token
		s.Close()
		return func() tea.Msg { return scriptResumeMsg{Name: name, Token: token, Result: nil} }
	case key.Matches(msg, Keys.SwitcherPrev):
		if s.selected > 0 {
			s.selected--
		}
	case key.Matches(msg, Keys.SwitcherNext):
		if s.selected < len(s.choices)-1 {
			s.selected++
		}
	case key.Matches(msg, Keys.SwitcherConfirm):
		if len(s.choices) == 0 {
			name, token := s.name, s.token
			s.Close()
			return func() tea.Msg { return scriptResumeMsg{Name: name, Token: token, Result: nil} }
		}
		chosen := s.choices[s.selected]
		name, token := s.name, s.token
		s.Close()
		return func() tea.Msg { return scriptResumeMsg{Name: name, Token: token, Result: chosen} }
	}
	return nil
}

func (s *ScriptUI) updatePrompt(msg tea.KeyMsg) tea.Cmd {
	switch {
	case key.Matches(msg, Keys.SwitcherClose):
		name, token := s.name, s.token
		s.Close()
		return func() tea.Msg { return scriptResumeMsg{Name: name, Token: token, Result: nil} }
	case key.Matches(msg, Keys.SwitcherConfirm):
		val := s.input.Value()
		name, token := s.name, s.token
		s.Close()
		return func() tea.Msg { return scriptResumeMsg{Name: name, Token: token, Result: val} }
	}
	ti, cmd := s.input.Update(msg)
	s.input = ti
	return cmd
}

func (s *ScriptUI) View() string {
	if s.kind == scriptUINone {
		return ""
	}
	title := helpTitleStyle.Render(s.title)
	if s.title == "" {
		if s.kind == scriptUIPick {
			title = helpTitleStyle.Render("Pick")
		} else {
			title = helpTitleStyle.Render("Input")
		}
	}

	var body string
	var footer string
	switch s.kind {
	case scriptUIPick:
		body = s.renderChoices()
		footer = helpDimStyle.Render("↑/↓ select · enter confirm · esc cancel")
	case scriptUIPrompt:
		body = s.input.View()
		footer = helpDimStyle.Render("enter confirm · esc cancel")
	}

	content := lipgloss.JoinVertical(lipgloss.Left, title, "", body, "", footer)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(s.palette.BorderActive).
		Padding(1, 3).
		Render(content)
}

func (s *ScriptUI) renderChoices() string {
	return renderPickerChoices(s.palette, s.choices, s.selected)
}

// renderPickerChoices renders a vertical pick list with the shared gutter +
// inverted-selection look. Used by both ScriptUI (kbrd.ui.pick) and the
// template picker so the two overlays stay visually identical.
func renderPickerChoices(p Palette, choices []string, selected int) string {
	if len(choices) == 0 {
		return helpDimStyle.Render("(no choices)")
	}
	nameStyle := lipgloss.NewStyle().Foreground(p.FgBase)
	selStyle := lipgloss.NewStyle().Bold(true).Foreground(p.FgInverse).Background(p.Primary)
	gutterSel := lipgloss.NewStyle().Foreground(p.Primary).Bold(true).Render("▌")

	rows := make([]string, 0, len(choices))
	for i, c := range choices {
		gutter := " "
		if i == selected {
			gutter = gutterSel
		}
		if i == selected {
			rows = append(rows, gutter+" "+selStyle.Render(" "+c+" "))
			continue
		}
		rows = append(rows, gutter+" "+nameStyle.Render(c))
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}
