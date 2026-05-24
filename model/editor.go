package model

import (
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	undoStackLimit  = 200
	undoIdlePauseMs = 600
)

type editorState int

const (
	editorNone editorState = iota
	editorEdit
	editorAppend
	editorPrepend
	editorJournal
	editorNew
	editorRenameItem
	editorRenameColumn
)

func isInputState(s editorState) bool {
	return s == editorNew || s == editorRenameItem || s == editorRenameColumn
}

type Editor struct {
	state         editorState
	textarea      textarea.Model
	textinput     textinput.Model
	ColIndex      int
	ColName       string
	FileName      string
	initialValue  string
	undo          []string
	redo          []string
	lastCommitted string
	lastCommitAt  time.Time
	expanded      bool
	termWidth     int
	termHeight    int
	palette       Palette
}

const (
	editorDefaultWidth  = 60
	editorDefaultHeight = 10
	editorMaxWidth      = 120
)

func (e *Editor) SetTermSize(w, h int) {
	e.termWidth = w
	e.termHeight = h
	e.applySize()
}

func (e *Editor) applySize() {
	if e.termWidth <= 0 || e.termHeight <= 0 {
		e.textarea.SetWidth(editorDefaultWidth)
		e.textarea.SetHeight(editorDefaultHeight)
		return
	}
	maxW := e.termWidth - 4
	if maxW < 20 {
		maxW = 20
	}
	maxH := e.termHeight - 6
	if maxH < 4 {
		maxH = 4
	}
	var w, h int
	if e.expanded {
		w = maxW
		if w > editorMaxWidth {
			w = editorMaxWidth
		}
		h = maxH
		if h < editorDefaultHeight {
			h = editorDefaultHeight
		}
	} else {
		w = editorDefaultWidth
		if w > maxW {
			w = maxW
		}
		h = editorDefaultHeight
		if h > maxH {
			h = maxH
		}
	}
	e.textarea.SetWidth(w)
	e.textarea.SetHeight(h)
}

func (e *Editor) toggleExpanded() {
	e.expanded = !e.expanded
	e.applySize()
}

func (e *Editor) resetHistory(initial string) {
	e.undo = e.undo[:0]
	e.redo = e.redo[:0]
	e.lastCommitted = initial
	e.lastCommitAt = time.Now()
}

func (e *Editor) pushUndo(prev string) {
	e.undo = append(e.undo, prev)
	if len(e.undo) > undoStackLimit {
		e.undo = e.undo[len(e.undo)-undoStackLimit:]
	}
	e.redo = e.redo[:0]
}

func isCommitBoundary(key string) bool {
	switch key {
	case " ", "enter", "tab", "backspace", "delete", "ctrl+w", "ctrl+u", "ctrl+k":
		return true
	}
	return false
}

func NewEditor() *Editor {
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.SetWidth(editorDefaultWidth)
	ta.SetHeight(editorDefaultHeight)

	ti := textinput.New()
	ti.CharLimit = 120
	ti.Width = 60
	ti.Placeholder = "filename (without .md)"

	return &Editor{textarea: ta, textinput: ti, palette: DarkPalette()}
}

func (e *Editor) OpenEdit(colIdx int, fileName, fullPath string) tea.Cmd {
	e.state = editorEdit
	e.ColIndex = colIdx
	e.FileName = fileName
	content, _ := os.ReadFile(fullPath)
	initial := strings.TrimRight(string(content), "\n")
	e.textarea.SetValue(initial)
	e.textarea.CursorEnd()
	e.initialValue = initial
	e.resetHistory(initial)
	e.expanded = false
	e.applySize()
	return e.textarea.Focus()
}

func (e *Editor) OpenAppend(colIdx int, fileName string) tea.Cmd {
	e.state = editorAppend
	e.ColIndex = colIdx
	e.FileName = fileName
	e.textarea.SetValue("")
	e.initialValue = ""
	e.resetHistory("")
	e.expanded = false
	e.applySize()
	return e.textarea.Focus()
}

func (e *Editor) OpenPrepend(colIdx int, fileName string) tea.Cmd {
	e.state = editorPrepend
	e.ColIndex = colIdx
	e.FileName = fileName
	e.textarea.SetValue("")
	e.initialValue = ""
	e.resetHistory("")
	e.expanded = false
	e.applySize()
	return e.textarea.Focus()
}

func (e *Editor) OpenJournal(colIdx int, fileName string) tea.Cmd {
	e.state = editorJournal
	e.ColIndex = colIdx
	e.FileName = fileName
	e.textarea.SetValue("")
	e.initialValue = ""
	e.resetHistory("")
	e.expanded = false
	e.applySize()
	return e.textarea.Focus()
}

func (e *Editor) OpenNew(colIdx int, colName string) tea.Cmd {
	e.state = editorNew
	e.ColIndex = colIdx
	e.ColName = colName
	e.FileName = ""
	e.textinput.SetValue("")
	e.initialValue = ""
	return e.textinput.Focus()
}

func (e *Editor) OpenRenameItem(colIdx int, fileName string) tea.Cmd {
	e.state = editorRenameItem
	e.ColIndex = colIdx
	e.FileName = fileName
	e.textinput.SetValue(fileName)
	e.textinput.CursorEnd()
	e.initialValue = fileName
	return e.textinput.Focus()
}

func (e *Editor) OpenRenameColumn(colIdx int, colName string) tea.Cmd {
	e.state = editorRenameColumn
	e.ColIndex = colIdx
	e.ColName = colName
	e.FileName = ""
	e.textinput.SetValue(colName)
	e.textinput.CursorEnd()
	e.initialValue = colName
	return e.textinput.Focus()
}

func (e *Editor) IsDirty() bool {
	if e.state == editorNone {
		return false
	}
	if isInputState(e.state) {
		return e.textinput.Value() != e.initialValue
	}
	return e.textarea.Value() != e.initialValue
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

	keyStr := ""
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		keyStr = keyMsg.String()
		switch {
		case key.Matches(keyMsg, Keys.EditorCancel):
			e.Close()
			return nil, nil
		case key.Matches(keyMsg, Keys.EditorSave):
			if !isInputState(e.state) {
				return e.submit()
			}
		case key.Matches(keyMsg, Keys.EditorConfirm):
			if isInputState(e.state) {
				return e.submit()
			}
		case key.Matches(keyMsg, Keys.EditorUndo):
			if !isInputState(e.state) {
				e.undoOnce()
				return nil, nil
			}
		case key.Matches(keyMsg, Keys.EditorRedo):
			if !isInputState(e.state) {
				e.redoOnce()
				return nil, nil
			}
		case key.Matches(keyMsg, Keys.EditorToggleExpand):
			if !isInputState(e.state) {
				e.toggleExpanded()
				return nil, nil
			}
		}
	}

	if isInputState(e.state) {
		ti, cmd := e.textinput.Update(msg)
		e.textinput = ti
		return cmd, nil
	}

	prev := e.textarea.Value()
	ta, cmd := e.textarea.Update(msg)
	e.textarea = ta
	cur := e.textarea.Value()

	if cur != prev {
		idle := time.Since(e.lastCommitAt) >= undoIdlePauseMs*time.Millisecond
		boundary := isCommitBoundary(keyStr)
		if (boundary || idle) && e.lastCommitted != prev {
			e.pushUndo(e.lastCommitted)
			e.lastCommitted = prev
			e.lastCommitAt = time.Now()
		} else if len(e.undo) == 0 && e.lastCommitted == "" && prev == "" {
			e.lastCommitAt = time.Now()
		}
	}

	return cmd, nil
}

func (e *Editor) undoOnce() {
	cur := e.textarea.Value()
	if cur != e.lastCommitted {
		e.redo = append(e.redo, cur)
		e.textarea.SetValue(e.lastCommitted)
		e.textarea.CursorEnd()
		e.lastCommitAt = time.Now()
		return
	}
	if len(e.undo) == 0 {
		return
	}
	target := e.undo[len(e.undo)-1]
	e.undo = e.undo[:len(e.undo)-1]
	e.redo = append(e.redo, cur)
	e.textarea.SetValue(target)
	e.textarea.CursorEnd()
	e.lastCommitted = target
	e.lastCommitAt = time.Now()
}

func (e *Editor) redoOnce() {
	if len(e.redo) == 0 {
		return
	}
	target := e.redo[len(e.redo)-1]
	e.redo = e.redo[:len(e.redo)-1]
	e.undo = append(e.undo, e.lastCommitted)
	e.textarea.SetValue(target)
	e.textarea.CursorEnd()
	e.lastCommitted = target
	e.lastCommitAt = time.Now()
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
	case editorRenameItem:
		name := strings.TrimSpace(e.textinput.Value())
		if name == "" || name == e.initialValue {
			e.Close()
			return nil, nil
		}
		msg = renameItemRequestMsg{ColIndex: e.ColIndex, OldName: e.initialValue, NewName: name}
	case editorRenameColumn:
		name := strings.TrimSpace(e.textinput.Value())
		if name == "" || name == e.initialValue {
			e.Close()
			return nil, nil
		}
		msg = renameColumnRequestMsg{ColIndex: e.ColIndex, OldName: e.initialValue, NewName: name}
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
	textareaHints := []Shortcut{{"ctrl+s", "save"}, {"ctrl+z", "undo"}}
	if len(e.redo) > 0 {
		textareaHints = append(textareaHints, Shortcut{"ctrl+y", "redo"})
	}
	expandLabel := "expand"
	if e.expanded {
		expandLabel = "collapse"
	}
	textareaHints = append(textareaHints, Shortcut{"ctrl+e", expandLabel}, Shortcut{"esc", "cancel"})
	switch e.state {
	case editorEdit:
		label = "Edit: " + e.FileName
		hints = textareaHints
	case editorAppend:
		label = "Append to: " + e.FileName
		hints = textareaHints
	case editorPrepend:
		label = "Prepend to: " + e.FileName
		hints = textareaHints
	case editorJournal:
		label = "Journal entry for: " + e.FileName
		hints = textareaHints
	case editorNew:
		label = "New item in: " + e.ColName
		hints = []Shortcut{{"enter", "confirm"}, {"esc", "cancel"}}
	case editorRenameItem:
		label = "Rename item: " + e.initialValue
		hints = []Shortcut{{"enter", "confirm"}, {"esc", "cancel"}}
	case editorRenameColumn:
		label = "Rename column: " + e.initialValue
		hints = []Shortcut{{"enter", "confirm"}, {"esc", "cancel"}}
	}

	p := e.palette
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(p.FgMuted)
	dirtyMark := ""
	if e.IsDirty() {
		dirtyStyle := lipgloss.NewStyle().Bold(true).Foreground(p.Warning)
		dirtyMark = dirtyStyle.Render("● ")
	}
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(p.BorderActive).
		Padding(0, 1)

	var input string
	if isInputState(e.state) {
		input = e.textinput.View()
	} else {
		input = e.textarea.View()
	}

	return dirtyMark + headerStyle.Render(label) + "\n" +
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

type editorDiscardMsg struct{}

type deleteConfirmMsg struct {
	ColIndex int
	FileName string
}

type renameItemRequestMsg struct {
	ColIndex int
	OldName  string
	NewName  string
}

type renameColumnRequestMsg struct {
	ColIndex int
	OldName  string
	NewName  string
}

type renameItemConfirmMsg struct {
	ColIndex int
	OldName  string
	NewName  string
}

type renameColumnConfirmMsg struct {
	ColIndex int
	OldName  string
	NewName  string
}
