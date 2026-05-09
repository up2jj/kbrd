package model

import (
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type editorState int

const (
	editorNone editorState = iota
	editorEdit
	editorAppend
	editorPrepend
	editorJournal
	editorNew
)

type Editor struct {
	state     editorState
	textarea  textarea.Model
	textinput textinput.Model
	ColIndex  int
	FileName  string
}

func NewEditor() *Editor {
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.SetWidth(60)
	ta.SetHeight(10)

	ti := textinput.New()
	ti.CharLimit = 120
	ti.Width = 60
	ti.Placeholder = "filename (without .md)"

	return &Editor{textarea: ta, textinput: ti}
}

func (e *Editor) OpenEdit(colIdx int, fileName, fullPath string) tea.Cmd {
	e.state = editorEdit
	e.ColIndex = colIdx
	e.FileName = fileName
	content, _ := os.ReadFile(fullPath)
	e.textarea.SetValue(strings.TrimRight(string(content), "\n"))
	e.textarea.CursorEnd()
	return e.textarea.Focus()
}

func (e *Editor) OpenAppend(colIdx int, fileName string) tea.Cmd {
	e.state = editorAppend
	e.ColIndex = colIdx
	e.FileName = fileName
	e.textarea.SetValue("")
	return e.textarea.Focus()
}

func (e *Editor) OpenPrepend(colIdx int, fileName string) tea.Cmd {
	e.state = editorPrepend
	e.ColIndex = colIdx
	e.FileName = fileName
	e.textarea.SetValue("")
	return e.textarea.Focus()
}

func (e *Editor) OpenJournal(colIdx int, fileName string) tea.Cmd {
	e.state = editorJournal
	e.ColIndex = colIdx
	e.FileName = fileName
	e.textarea.SetValue("")
	return e.textarea.Focus()
}

func (e *Editor) OpenNew(colIdx int) tea.Cmd {
	e.state = editorNew
	e.ColIndex = colIdx
	e.FileName = ""
	e.textinput.SetValue("")
	return e.textinput.Focus()
}

func (e *Editor) Close() {
	e.state = editorNone
	e.textarea.Blur()
	e.textinput.Blur()
}

func (e *Editor) Update(msg tea.Msg) (tea.Cmd, tea.Msg) {
	if e.state == editorNone {
		return nil, nil
	}

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "esc":
			e.Close()
			return nil, nil
		case "ctrl+s":
			if e.state != editorNew {
				return e.submit()
			}
		case "enter":
			if e.state == editorNew {
				return e.submit()
			}
		}
	}

	if e.state == editorNew {
		ti, cmd := e.textinput.Update(msg)
		e.textinput = ti
		return cmd, nil
	}

	ta, cmd := e.textarea.Update(msg)
	e.textarea = ta
	return cmd, nil
}

func (e *Editor) submit() (tea.Cmd, tea.Msg) {
	var msg tea.Msg
	switch e.state {
	case editorEdit:
		msg = editorSaveMsg{ColIndex: e.ColIndex, FileName: e.FileName, Content: e.textarea.Value()}
	case editorAppend:
		msg = editorAppendMsg{ColIndex: e.ColIndex, FileName: e.FileName, Text: e.textarea.Value()}
	case editorPrepend:
		msg = editorPrependMsg{ColIndex: e.ColIndex, FileName: e.FileName, Text: e.textarea.Value()}
	case editorJournal:
		msg = editorJournalMsg{ColIndex: e.ColIndex, FileName: e.FileName, Text: e.textarea.Value()}
	case editorNew:
		name := strings.TrimSpace(e.textinput.Value())
		if name == "" {
			e.Close()
			return nil, nil
		}
		msg = editorNewMsg{ColIndex: e.ColIndex, FileName: name}
	}
	e.Close()
	return func() tea.Msg { return msg }, nil
}

func (e *Editor) View() string {
	if e.state == editorNone {
		return ""
	}

	var label string
	var hints []Shortcut
	saveHints := []Shortcut{{"ctrl+s", "save"}, {"esc", "cancel"}}
	switch e.state {
	case editorEdit:
		label = "Edit: " + e.FileName
		hints = saveHints
	case editorAppend:
		label = "Append to: " + e.FileName
		hints = saveHints
	case editorPrepend:
		label = "Prepend to: " + e.FileName
		hints = saveHints
	case editorJournal:
		label = "Journal entry for: " + e.FileName
		hints = saveHints
	case editorNew:
		label = "New item in column " + string(rune('1'+e.ColIndex))
		hints = []Shortcut{{"enter", "confirm"}, {"esc", "cancel"}}
	}

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#94a3b8"))
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#3b82f6")).
		Padding(0, 1)

	var input string
	if e.state == editorNew {
		input = e.textinput.View()
	} else {
		input = e.textarea.View()
	}

	return headerStyle.Render(label) + "\n" +
		boxStyle.Render(input) + "\n" +
		RenderInlineHints(hints)
}

type editorSaveMsg struct {
	ColIndex int
	FileName string
	Content  string
}

type editorAppendMsg struct {
	ColIndex int
	FileName string
	Text     string
}

type editorPrependMsg struct {
	ColIndex int
	FileName string
	Text     string
}

type editorJournalMsg struct {
	ColIndex int
	FileName string
	Text     string
}

type editorNewMsg struct {
	ColIndex int
	FileName string
}

type deleteConfirmMsg struct {
	ColIndex int
	FileName string
}
