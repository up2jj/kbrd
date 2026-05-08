package model

import (
	"strings"

	"github.com/charmbracelet/bubbletea"
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
	editorConfirmDelete
)

type Editor struct {
	state    editorState
	input    string
	cursor   int
	quitting bool
	ColIndex int
	FileName string
}

func NewEditor() *Editor {
	return &Editor{
		state: editorNone,
	}
}

func (e *Editor) OpenEdit(colIdx int, fileName string) tea.Cmd {
	e.state = editorEdit
	e.ColIndex = colIdx
	e.FileName = fileName
	e.input = ""
	e.cursor = 0
	return nil
}

func (e *Editor) OpenAppend(colIdx int, fileName string) tea.Cmd {
	e.state = editorAppend
	e.ColIndex = colIdx
	e.FileName = fileName
	e.input = ""
	e.cursor = 0
	return nil
}

func (e *Editor) OpenPrepend(colIdx int, fileName string) tea.Cmd {
	e.state = editorPrepend
	e.ColIndex = colIdx
	e.FileName = fileName
	e.input = ""
	e.cursor = 0
	return nil
}

func (e *Editor) OpenJournal(colIdx int, fileName string) tea.Cmd {
	e.state = editorJournal
	e.ColIndex = colIdx
	e.FileName = fileName
	e.input = ""
	e.cursor = 0
	return nil
}

func (e *Editor) OpenNew(colIdx int) tea.Cmd {
	e.state = editorNew
	e.ColIndex = colIdx
	e.FileName = ""
	e.input = ""
	e.cursor = 0
	return nil
}

func (e *Editor) OpenConfirmDelete(colIdx int, fileName string) tea.Cmd {
	e.state = editorConfirmDelete
	e.ColIndex = colIdx
	e.FileName = fileName
	e.input = ""
	e.cursor = 0
	return nil
}

func (e *Editor) Close() {
	e.state = editorNone
}

func (e *Editor) Update(msg tea.Msg) (tea.Cmd, tea.Msg) {
	if e.state == editorNone {
		return nil, ""
	}

	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil, ""
	}
	key := keyMsg.String()

	switch key {
	case "esc":
		e.Close()
		return nil, ""
	case "enter":
		cmd, msg := e.handleEnter()
		if msg != nil {
			return func() tea.Msg { return msg }, nil
		}
		return cmd, nil
	case "backspace":
		if e.cursor > 0 {
			e.input = e.input[:e.cursor-1] + e.input[e.cursor:]
			e.cursor--
		}
	case "delete":
		if e.cursor < len(e.input) {
			e.input = e.input[:e.cursor] + e.input[e.cursor+1:]
		}
	case "left":
		if e.cursor > 0 {
			e.cursor--
		}
	case "right":
		if e.cursor < len(e.input) {
			e.cursor++
		}
	case "a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m", "n", "o", "p", "q", "r", "s", "t", "u", "v", "w", "x", "y", "z":
		e.input = e.input[:e.cursor] + key + e.input[e.cursor:]
		e.cursor++
	case "0", "1", "2", "3", "4", "5", "6", "7", "8", "9":
		e.input = e.input[:e.cursor] + key + e.input[e.cursor:]
		e.cursor++
	case "!":
		e.input = e.input[:e.cursor] + "!" + e.input[e.cursor:]
		e.cursor++
	}

	return nil, ""
}

func (e *Editor) handleEnter() (tea.Cmd, tea.Msg) {
	text := strings.TrimSpace(e.input)
	if text == "" {
		e.Close()
		return nil, nil
	}

	if e.state == editorConfirmDelete {
		if text == "yes" {
			e.Close()
			return nil, deleteConfirmMsg{ColIndex: e.ColIndex, FileName: e.FileName}
		}
		e.Close()
		return nil, nil
	}

	var msg tea.Msg
	switch e.state {
	case editorEdit:
		msg = editorSaveMsg{ColIndex: e.ColIndex, FileName: e.FileName, Content: text}
	case editorAppend:
		msg = editorAppendMsg{ColIndex: e.ColIndex, FileName: e.FileName, Text: text}
	case editorPrepend:
		msg = editorPrependMsg{ColIndex: e.ColIndex, FileName: e.FileName, Text: text}
	case editorJournal:
		msg = editorJournalMsg{ColIndex: e.ColIndex, FileName: e.FileName, Text: text}
	case editorNew:
		msg = editorNewMsg{ColIndex: e.ColIndex, FileName: text}
	}
	e.Close()
	return nil, msg
}

func (e *Editor) View() string {
	if e.state == editorNone {
		return ""
	}

	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#3b82f6")).
		Padding(1, 2).
		Width(60).
		Height(10)

	var label string
	var placeholder string
	switch e.state {
	case editorEdit:
		label = "Edit: " + e.FileName
		placeholder = "Edit content..."
	case editorAppend:
		label = "Append to: " + e.FileName
		placeholder = "Append text..."
	case editorPrepend:
		label = "Prepend to: " + e.FileName
		placeholder = "Prepend text..."
	case editorJournal:
		label = "Journal to: " + e.FileName
		placeholder = "Journal entry..."
	case editorNew:
		label = "New file in column " + string(rune('1'+e.ColIndex))
		placeholder = "New file name (without .md)..."
	case editorConfirmDelete:
		label = "Delete: " + e.FileName + " (type 'yes' to confirm)"
		placeholder = "Type 'yes' to confirm..."
	}

	header := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#94a3b8")).
		Render(label)

	cursor := e.input[:e.cursor] + "█" + e.input[e.cursor:]
	if cursor == "" {
		cursor = lipgloss.NewStyle().Foreground(lipgloss.Color("#64748b")).Render(placeholder)
	}

	body := borderStyle.Render(cursor)
	return header + "\n" + body
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
