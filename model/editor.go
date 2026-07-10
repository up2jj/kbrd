package model

import (
	"fmt"
	"image/color"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/atotto/clipboard"

	"kbrd/vimbuf"
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
	editorManagedFile
)

func isInputState(s editorState) bool {
	return s == editorNew || s == editorRenameItem || s == editorRenameColumn
}

type Editor struct {
	state         editorState
	textarea      textarea.Model
	textinput     textinput.Model
	ColIndex      int
	ColPath       string
	ColName       string
	FileName      string
	ItemPath      string
	NewContent    string
	ManagedPath   string
	initialValue  string
	undo          []string
	redo          []string
	lastCommitted string
	lastCommitAt  time.Time
	expanded      bool
	termWidth     int
	termHeight    int
	frameHeight   int // overlay band rows from the last board frame; keeps vim sizing stable during input
	palette       Palette

	// vim modal editor (used for text states when cfg.Editor.Vim is true).
	vim              bool
	buf              *vimbuf.Buffer
	showHelp         bool                       // vim cheatsheet overlay (:help) is open
	status           string                     // transient status line (errors, :q hint)
	swapFile         string                     // crash-recovery sidecar path ("" = disabled)
	swapWriteFailed  bool                       // last swap flush failed — crash recovery is unavailable
	pendingSaveClose bool                       // a save is in flight that should close the editor on success
	evalCompletions  func() []vimbuf.Completion // injected by the board for :lua autocomplete
	cleanRevision    int64                      // vim buffer revision matching the saved/on-disk baseline
	lastSwapRevision int64                      // latest vim revision successfully written to the swap sidecar
	lastVimW         int                        // cached modal content width applied to the vim buffer
	lastVimH         int                        // cached modal content height applied to the vim buffer
}

const (
	editorDefaultWidth  = 60
	editorDefaultHeight = 10
	editorMaxWidth      = 120
)

func (e *Editor) SetSize(w, h int) {
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
	maxW := max(e.termWidth-4, 20)
	maxH := max(e.termHeight-6, 4)
	var w, h int
	if e.expanded {
		w = min(maxW, editorMaxWidth)
		h = max(maxH, editorDefaultHeight)
	} else {
		w = min(editorDefaultWidth, maxW)
		h = min(editorDefaultHeight, maxH)
	}
	e.textarea.SetWidth(w)
	e.textarea.SetHeight(h)
	if e.vim && e.buf != nil {
		vw, vh := e.vimSize()
		if vw != e.lastVimW || vh != e.lastVimH {
			e.buf.SetSize(vw, vh)
			e.lastVimW, e.lastVimH = vw, vh
		}
	}
}

func (e *Editor) vimFrameHeight() int {
	if e.frameHeight > 0 {
		return e.frameHeight
	}
	return e.termHeight
}

// vimSize returns the buffer's content (width, height) for the modal. It is a
// fixed generous fraction of the terminal (not fit-to-content) so the editor is
// a consistently large modal regardless of how much text it holds, while a cap
// leaves a margin on big terminals so the board stays visible around it. Long
// files scroll within it; ctrl+e makes it even roomier.
func (e *Editor) vimSize() (int, int) {
	if e.termWidth <= 0 || e.termHeight <= 0 {
		return editorDefaultWidth, editorDefaultHeight
	}
	wCap, hCap := 100, 36
	if e.expanded {
		wCap, hCap = 130, 60
	}
	w := max(min(e.termWidth-8, wCap), 20)
	// Size against the overlay band cached from the last frame so input-time
	// resizing and render-time sizing agree.
	h := max(min(e.vimFrameHeight()-9, hCap), 6)
	return w, h
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

func NewEditor(vim bool) *Editor {
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.SetWidth(editorDefaultWidth)
	ta.SetHeight(editorDefaultHeight)

	ti := textinput.New()
	ti.CharLimit = 120
	ti.SetWidth(60)
	ti.Placeholder = "filename (without .md)"

	return &Editor{textarea: ta, textinput: ti, palette: DarkPalette(), vim: vim}
}

// usesTextarea reports whether the current state is driven by the plain textarea
// (or single-line input) rather than the vim buffer. The board uses this to keep
// the esc→discard-confirm interception for non-vim paths while letting vim
// handle esc itself.
func (e *Editor) usesTextarea() bool { return !e.vim || isInputState(e.state) }

// SetEvalCompletionsFunc injects a provider of :lua autocomplete candidates
// (registered function names + usage), called when a vim buffer is opened.
func (e *Editor) SetEvalCompletionsFunc(fn func() []vimbuf.Completion) { e.evalCompletions = fn }

func (e *Editor) seedCompletions() {
	if e.buf != nil && e.evalCompletions != nil {
		e.buf.SetEvalCompletions(e.evalCompletions())
	}
}

// currentText returns the editable text regardless of backing widget.
func (e *Editor) currentText() string {
	if e.vim && e.buf != nil && !isInputState(e.state) {
		return e.buf.Text()
	}
	if isInputState(e.state) {
		return e.textinput.Value()
	}
	return e.textarea.Value()
}

// setStatus sets the transient status line (shown in the vim footer).
func (e *Editor) setStatus(s string) tea.Cmd {
	e.status = s
	return nil
}

// HandleMouse scrolls the vim buffer on wheel events (mirrors Peek.HandleMouse).
// Other mouse input is ignored; the textarea path has no wheel handling.
func (e *Editor) HandleMouse(msg tea.MouseMsg) {
	if !e.vim || e.buf == nil {
		return
	}
	switch msg.Mouse().Button {
	case tea.MouseWheelUp:
		e.buf.Scroll(-3)
	case tea.MouseWheelDown:
		e.buf.Scroll(3)
	}
}

// GoToLine positions the cursor on the given 1-based line (used when opening the
// editor at a specific line, e.g. from Lua). No-op for the single-line inputs.
func (e *Editor) GoToLine(line int) {
	if line <= 0 || isInputState(e.state) {
		return
	}
	if e.vim && e.buf != nil {
		e.buf.GoToLine(line)
		return
	}
	// textarea path: walk the cursor to the requested row.
	e.textarea.CursorStart()
	for range line - 1 {
		e.textarea.CursorDown()
	}
}

func (e *Editor) OpenEdit(colIdx int, colPath, fileName, fullPath string) tea.Cmd {
	e.state = editorEdit
	e.ColIndex = colIdx
	e.ColPath = colPath
	e.FileName = fileName
	e.ItemPath = fullPath
	content, _ := os.ReadFile(fullPath)
	raw := string(content)
	initial := strings.TrimRight(raw, "\n")
	if e.vim {
		e.initialValue = raw
		e.setSwapTarget(fullPath)
		e.expanded = false
		e.status = ""
		return e.startVim(raw, false)
	}
	e.textarea.SetValue(initial)
	e.textarea.CursorEnd()
	// SetValue runs the textarea's sanitizer (tabs -> spaces, CRLF -> LF), so read
	// the value back to baseline against what the buffer actually holds. Otherwise
	// any tab/CRLF in the file would read as an unsaved edit the moment it opens.
	normalized := e.textarea.Value()
	e.initialValue = normalized
	e.resetHistory(normalized)
	e.expanded = true
	e.applySize()
	return e.textarea.Focus()
}

func (e *Editor) OpenManagedFile(label, path string) tea.Cmd {
	e.state = editorManagedFile
	e.ColIndex = 0
	e.ColPath = ""
	e.ColName = ""
	e.FileName = label
	e.ItemPath = ""
	e.ManagedPath = path
	content, _ := os.ReadFile(path)
	raw := string(content)
	initial := strings.TrimRight(raw, "\n")
	if e.vim {
		e.initialValue = raw
		e.setSwapTarget(path)
		e.expanded = false
		e.status = ""
		return e.startVim(raw, false)
	}
	e.textarea.SetValue(initial)
	e.textarea.CursorEnd()
	normalized := e.textarea.Value()
	e.initialValue = normalized
	e.resetHistory(normalized)
	e.expanded = true
	e.applySize()
	return e.textarea.Focus()
}

func (e *Editor) OpenAppend(colIdx int, colPath, itemPath, fileName string) tea.Cmd {
	e.state = editorAppend
	e.ColIndex = colIdx
	e.ColPath = colPath
	e.FileName = fileName
	e.ItemPath = itemPath
	if e.vim {
		e.initialValue = ""
		e.swapFile = ""
		e.lastSwapRevision = 0
		e.expanded = false
		e.status = ""
		return e.startVim("", true)
	}
	e.textarea.SetValue("")
	e.initialValue = ""
	e.resetHistory("")
	e.expanded = true
	e.applySize()
	return e.textarea.Focus()
}

func (e *Editor) OpenPrepend(colIdx int, colPath, itemPath, fileName string) tea.Cmd {
	e.state = editorPrepend
	e.ColIndex = colIdx
	e.ColPath = colPath
	e.FileName = fileName
	e.ItemPath = itemPath
	if e.vim {
		e.initialValue = ""
		e.swapFile = ""
		e.lastSwapRevision = 0
		e.expanded = false
		e.status = ""
		return e.startVim("", true)
	}
	e.textarea.SetValue("")
	e.initialValue = ""
	e.resetHistory("")
	e.expanded = true
	e.applySize()
	return e.textarea.Focus()
}

func (e *Editor) OpenJournal(colIdx int, colPath, itemPath, fileName string) tea.Cmd {
	e.state = editorJournal
	e.ColIndex = colIdx
	e.ColPath = colPath
	e.FileName = fileName
	e.ItemPath = itemPath
	if e.vim {
		e.initialValue = ""
		e.swapFile = ""
		e.lastSwapRevision = 0
		e.expanded = false
		e.status = ""
		return e.startVim("", true)
	}
	e.textarea.SetValue("")
	e.initialValue = ""
	e.resetHistory("")
	e.expanded = true
	e.applySize()
	return e.textarea.Focus()
}

func (e *Editor) OpenNew(colIdx int, colName, colPath string) tea.Cmd {
	e.state = editorNew
	e.ColIndex = colIdx
	e.ColPath = colPath
	e.ColName = colName
	e.FileName = ""
	e.ItemPath = ""
	e.NewContent = ""
	e.textinput.SetValue("")
	e.initialValue = ""
	return e.textinput.Focus()
}

func (e *Editor) OpenNewWithContent(colIdx int, colName, colPath, content string) tea.Cmd {
	cmd := e.OpenNew(colIdx, colName, colPath)
	e.NewContent = content
	return cmd
}

func (e *Editor) OpenRenameItem(colIdx int, colPath, itemPath, fileName string) tea.Cmd {
	e.state = editorRenameItem
	e.ColIndex = colIdx
	e.ColPath = colPath
	e.FileName = fileName
	e.ItemPath = itemPath
	e.textinput.SetValue(fileName)
	e.textinput.CursorEnd()
	e.initialValue = fileName
	return e.textinput.Focus()
}

func (e *Editor) OpenRenameColumn(colIdx int, colPath, colName string) tea.Cmd {
	e.state = editorRenameColumn
	e.ColIndex = colIdx
	e.ColPath = colPath
	e.ColName = colName
	e.FileName = ""
	e.ItemPath = ""
	e.textinput.SetValue(colName)
	e.textinput.CursorEnd()
	e.initialValue = colName
	return e.textinput.Focus()
}

// CurrentLine returns the text of the logical line the cursor is on (the line
// a line command runs against). Empty when there is no buffer.
func (e *Editor) CurrentLine() string {
	if e.vim && e.buf != nil && !isInputState(e.state) {
		return e.buf.CurrentLine()
	}
	lines := strings.Split(e.textarea.Value(), "\n")
	row := e.textarea.Line()
	if row < 0 || row >= len(lines) {
		return ""
	}
	return lines[row]
}

// CurrentRow returns the 0-based logical row the cursor is on — the row a line
// command captures so its (possibly async) result lands on that line and not
// wherever the cursor moved to meanwhile. Pairs with ReplaceLine.
func (e *Editor) CurrentRow() int {
	if e.vim && e.buf != nil && !isInputState(e.state) {
		return e.buf.CursorRow()
	}
	return e.textarea.Line()
}

// ReplaceCurrentLine swaps the cursor's logical line for s (which may itself
// contain newlines, splitting it into several lines) and leaves the cursor at
// the end of the replacement. The swap is a single undo step: one ctrl+z
// restores the line as it was before the command ran.
func (e *Editor) ReplaceCurrentLine(s string) {
	e.ReplaceLine(e.CurrentRow(), s)
}

// ReplaceLine is ReplaceCurrentLine targeted at a specific row, so a line
// command's deferred result replaces the line it was dispatched from even if the
// cursor has since moved. A row that no longer exists (the buffer shrank) is a
// safe no-op rather than corrupting a different line.
func (e *Editor) ReplaceLine(row int, s string) {
	if e.vim && e.buf != nil && !isInputState(e.state) {
		e.buf.ReplaceLineRange(row, row, s)
		e.flushSwap()
		return
	}
	prev := e.textarea.Value()
	lines := strings.Split(prev, "\n")
	if row < 0 || row >= len(lines) {
		return
	}
	// Fold any uncommitted typing into history first, then record prev so a
	// single undo reverts exactly this replacement (mirrors the commit model in
	// Update: lastCommitted is the baseline the buffer reverts to).
	if e.lastCommitted != prev {
		e.pushUndo(e.lastCommitted)
		e.lastCommitted = prev
	}
	e.pushUndo(prev)

	lines[row] = s
	e.textarea.SetValue(strings.Join(lines, "\n"))
	// SetValue runs the sanitizer (tabs/CRLF), so baseline against what the
	// buffer actually holds — otherwise the next keystroke reads as a phantom edit.
	e.lastCommitted = e.textarea.Value()
	e.lastCommitAt = time.Now()

	// SetValue parks the cursor at the buffer end; walk it back onto the last
	// row of the replacement, then to that row's end.
	desired := row + strings.Count(s, "\n")
	for e.textarea.Line() > desired {
		e.textarea.CursorUp()
	}
	e.textarea.SetCursorColumn(1 << 30) // clamps to end of the current line

	// SetValue runs the textarea's Reset, which jumps the viewport to the top.
	// The viewport only re-follows the cursor inside textarea.Update (never in
	// View), so without this nudge the editor stays scrolled to the first line
	// even though the cursor is back on the edited line.
	e.repositionViewport()
}

// repositionViewport forces the textarea to scroll its viewport back onto the
// current cursor line. The textarea exposes no method for this, so feed it a
// benign message: Update ignores an unknown message but still calls its internal
// repositionView at the end.
func (e *Editor) repositionViewport() {
	e.textarea, _ = e.textarea.Update(editorRepositionMsg{})
}

// editorRepositionMsg is the inert message used to trigger a viewport reposition
// (see repositionViewport). The textarea's Update switch ignores it.
type editorRepositionMsg struct{}

func (e *Editor) IsDirty() bool {
	if e.state == editorNone {
		return false
	}
	if isInputState(e.state) {
		return e.textinput.Value() != e.initialValue
	}
	if e.vim && e.buf != nil {
		return e.buf.ChangedSince(e.cleanRevision)
	}
	return e.textarea.Value() != e.initialValue
}

// ReplaceLineRange replaces buffer rows [start,end] (inclusive, 0-based) with s
// as one undo step — used by a range :lua command. No-op outside the vim path.
func (e *Editor) ReplaceLineRange(start, end int, s string) {
	if e.vim && e.buf != nil && !isInputState(e.state) {
		e.buf.ReplaceLineRange(start, end, s)
		e.flushSwap()
	}
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

	if paste, ok := msg.(tea.PasteMsg); ok && e.vim && !isInputState(e.state) {
		return e.updateVimPaste(paste.Content)
	}

	keyStr := ""
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		keyStr = keyMsg.String()
		if e.vim && !isInputState(e.state) {
			return e.updateVim(keyMsg, keyStr)
		}
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
		case key.Matches(keyMsg, Keys.EditorCommand):
			if !isInputState(e.state) {
				// Hand the current line to the board, which opens the line-command
				// menu over the still-open editor. No buffer mutation here.
				line, row, col, fn, target := e.CurrentLine(), e.CurrentRow(), e.ColIndex, e.FileName, e.itemTarget()
				return func() tea.Msg {
					return newStableOpenLineCommandsMsg(target, col, fn, line, row)
				}, nil
			}
		case key.Matches(keyMsg, Keys.EditorTaskPrefix):
			if !isInputState(e.state) {
				e.insertTextareaTaskPrefix()
				return nil, nil
			}
		}
	}

	if isInputState(e.state) {
		ti, cmd := e.textinput.Update(msg)
		e.textinput = ti
		return cmd, nil
	}

	if e.vim {
		// The vim buffer consumes only key messages (handled above); other
		// messages (resize, ticks) have no per-frame state to update here.
		return nil, nil
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

func (e *Editor) insertTextareaTaskPrefix() {
	if line, ok := toggleTaskLine(e.CurrentLine()); ok {
		e.ReplaceCurrentLine(line)
		return
	}
	prev := e.textarea.Value()
	if e.lastCommitted != prev {
		e.pushUndo(e.lastCommitted)
		e.lastCommitted = prev
	}
	e.redo = e.redo[:0]
	e.textarea.InsertString(vimbuf.TaskPrefix)
	e.lastCommitAt = time.Now()
}

func toggleTaskLine(line string) (string, bool) {
	i := len(line) - len(strings.TrimLeft(line, " \t"))
	if i+5 > len(line) || (line[i] != '-' && line[i] != '*' && line[i] != '+') || line[i+1] != ' ' || line[i+2] != '[' || line[i+4] != ']' {
		return "", false
	}
	if i+5 < len(line) && line[i+5] != ' ' {
		return "", false
	}
	switch line[i+3] {
	case ' ':
		return line[:i+3] + "x" + line[i+4:], true
	case 'x', 'X':
		return line[:i+3] + " " + line[i+4:], true
	default:
		return "", false
	}
}

func (e *Editor) submit() (tea.Cmd, tea.Msg) {
	var msg tea.Msg
	switch e.state {
	case editorEdit:
		msg = newStableEditorSaveMsg(e.itemTarget(), e.ColIndex, e.FileName, e.textarea.Value())
	case editorManagedFile:
		msg = newStableManagedFileSaveMsg(e.ManagedPath, e.FileName, e.textarea.Value())
	case editorAppend:
		msg = newStableEditorAppendMsg(e.itemTarget(), e.ColIndex, e.FileName, e.textarea.Value())
	case editorPrepend:
		msg = newStableEditorPrependMsg(e.itemTarget(), e.ColIndex, e.FileName, e.textarea.Value())
	case editorJournal:
		msg = newStableEditorJournalMsg(e.itemTarget(), e.ColIndex, e.FileName, e.textarea.Value())
	case editorNew:
		name := strings.TrimSpace(e.textinput.Value())
		if name == "" {
			e.Close()
			return nil, nil
		}
		msg = newStableEditorNewMsg(e.columnTarget(), e.ColIndex, name, e.NewContent)
	case editorRenameItem:
		name := strings.TrimSpace(e.textinput.Value())
		if name == "" || name == e.initialValue {
			e.Close()
			return nil, nil
		}
		msg = newStableRenameItemRequestMsg(e.itemTarget(), e.ColIndex, e.initialValue, name)
	case editorRenameColumn:
		name := strings.TrimSpace(e.textinput.Value())
		if name == "" || name == e.initialValue {
			e.Close()
			return nil, nil
		}
		msg = newStableRenameColumnRequestMsg(e.columnTarget(), e.ColIndex, e.initialValue, name)
	}
	if e.state != editorManagedFile {
		e.Close()
	}
	return func() tea.Msg { return msg }, nil
}

// startVim builds the vim buffer for the current state, seeds autocomplete, sizes
// it, and (for an existing file) checks for a recoverable swap.
func (e *Editor) startVim(initial string, insert bool) tea.Cmd {
	e.buf = vimbuf.New(initial)
	e.cleanRevision = e.buf.Revision()
	e.lastSwapRevision = 0
	e.lastVimW, e.lastVimH = 0, 0
	if insert {
		e.buf.StartInsert()
	}
	e.seedCompletions()
	e.applySize()
	return e.openSwapCheck()
}

// updateVim routes a key in the vim path: chrome chords first, then the buffer.
func (e *Editor) updateVim(keyMsg tea.KeyPressMsg, keyStr string) (tea.Cmd, tea.Msg) {
	// The cheatsheet swallows the next key to dismiss itself.
	if e.showHelp {
		e.showHelp = false
		return nil, nil
	}
	// ctrl+v pastes the system clipboard at the cursor.
	if keyStr == "ctrl+v" {
		return func() tea.Msg { return editorClipboardReadMsg{} }, nil
	}
	// esc closes the editor from Normal mode (a convenience over vim's :q); in
	// Insert/Visual/Command it falls through to the buffer, which returns to
	// Normal. A dirty buffer prompts the discard confirm instead of closing.
	if key.Matches(keyMsg, Keys.EditorCancel) && e.buf.Mode() == vimbuf.ModeNormal {
		if e.IsDirty() {
			return func() tea.Msg { return editorConfirmDiscardMsg{} }, nil
		}
		e.clearSwap()
		e.Close()
		return nil, nil
	}
	switch {
	case key.Matches(keyMsg, Keys.EditorSave): // ctrl+s = save (and stay, for edit)
		return e.vimSave(false), nil
	case key.Matches(keyMsg, Keys.EditorToggleExpand):
		e.toggleExpanded()
		return nil, nil
	case key.Matches(keyMsg, Keys.EditorCommand): // ctrl+l line command menu
		line, row, col, fn, target := e.buf.CurrentLine(), e.buf.CursorRow(), e.ColIndex, e.FileName, e.itemTarget()
		return func() tea.Msg {
			return newStableOpenLineCommandsMsg(target, col, fn, line, row)
		}, nil
	case key.Matches(keyMsg, Keys.EditorTaskPrefix) && e.buf.Mode() != vimbuf.ModeCommand:
		if e.withVimRevisionChange(e.buf.InsertTaskPrefix) {
			e.flushSwap()
		}
		return nil, nil
	}
	e.status = ""
	// A key press can carry several runes at once — fast typing or an IME
	// commit. The per-key handlers take one key at a time and would otherwise
	// drop a multi-rune chunk entirely, so feed the runes individually. Special
	// keys arrive with empty Text and are unaffected.
	if rs := []rune(keyMsg.Text); len(rs) > 1 {
		var eff vimbuf.Effect
		changed := e.withVimRevisionChange(func() {
			eff = e.feedVimRunes(rs)
		})
		return e.applyVimEffect(eff, changed)
	}
	var eff vimbuf.Effect
	changed := e.withVimRevisionChange(func() {
		eff = e.buf.HandleKey(keyStr)
	})
	return e.applyVimEffect(eff, changed)
}

// editorClipboardReadMsg asks the Board to request the terminal clipboard.
// It keeps OSC52 response routing out of Editor, which has no Update case for
// arbitrary clipboard messages.
type editorClipboardReadMsg struct{}

// PasteClipboard applies an OSC52 response using the same semantics ctrl+v
// previously had for Vim's normal, insert, and command modes.
func (e *Editor) PasteClipboard(text string) tea.Cmd {
	if text == "" || e.state == editorNone || !e.vim || isInputState(e.state) || e.buf == nil {
		return nil
	}
	if e.buf.Mode() == vimbuf.ModeCommand {
		var eff vimbuf.Effect
		changed := e.withVimRevisionChange(func() {
			eff = e.feedVimRunes([]rune(text))
		})
		cmd, _ := e.applyVimEffect(eff, changed)
		return cmd
	}
	if e.buf.Mode() == vimbuf.ModeNormal || e.buf.Mode() == vimbuf.ModeInsert {
		if e.withVimRevisionChange(func() { e.buf.InsertText(text) }) {
			e.flushSwap()
		}
	}
	return nil
}

func (e *Editor) updateVimPaste(text string) (tea.Cmd, tea.Msg) {
	if text == "" {
		return nil, nil
	}
	switch e.buf.Mode() {
	case vimbuf.ModeInsert:
		if e.withVimRevisionChange(func() { e.buf.InsertText(text) }) {
			e.flushSwap()
		}
	case vimbuf.ModeCommand:
		var eff vimbuf.Effect
		changed := e.withVimRevisionChange(func() {
			eff = e.feedVimRunes([]rune(text))
		})
		return e.applyVimEffect(eff, changed)
	}
	return nil, nil
}

func (e *Editor) withVimRevisionChange(fn func()) bool {
	before := e.buf.Revision()
	fn()
	return e.buf.ChangedSince(before)
}

func (e *Editor) feedVimRunes(rs []rune) vimbuf.Effect {
	var eff vimbuf.Effect
	for _, r := range rs {
		switch r {
		case ' ':
			eff = e.buf.HandleKey("space")
		case '\n', '\r':
			eff = e.buf.HandleKey("enter")
		default:
			eff = e.buf.HandleKey(string(r))
		}
	}
	return eff
}

// applyVimEffect carries out the host-level Effect a key produced.
func (e *Editor) applyVimEffect(eff vimbuf.Effect, contentChanged bool) (tea.Cmd, tea.Msg) {
	if eff.Status != "" {
		e.status = eff.Status
	}
	if eff.Yank != "" {
		_ = clipboard.WriteAll(eff.Yank) // mirror yanks to the system clipboard
	}
	switch {
	case eff.Help:
		e.showHelp = true
		return nil, nil
	case eff.ForceQuit:
		e.Close() // swap left in place for next-open recovery
		return nil, nil
	case eff.Submit && eff.Quit:
		return e.vimSave(true), nil
	case eff.Submit:
		return e.vimSave(false), nil
	case eff.Quit:
		if e.IsDirty() {
			e.status = "unsaved changes — :w to save, :q! to discard"
			return nil, nil
		}
		e.clearSwap()
		e.Close()
		return nil, nil
	case eff.EvalExpr != "":
		expr := eff.EvalExpr
		var rng *evalRange
		if eff.EvalRange != nil {
			rng = &evalRange{Start: eff.EvalRange.Start, End: eff.EvalRange.End}
		}
		return func() tea.Msg { return editorEvalMsg{Expr: expr, Range: rng} }, nil
	}
	if contentChanged {
		e.flushSwap()
	}
	return nil, nil
}

// vimSave emits the save message for the current state. It does NOT clear the
// swap, rebaseline the dirty marker, or close the editor — those happen only in
// confirmSaved, after the board has actually written the file. Otherwise a failed
// write would leave the editor open but marked clean with its recovery swap gone,
// so :q could silently discard the unsaved buffer. forceClose records that a
// successful save should also close (e.g. :wq).
func (e *Editor) vimSave(forceClose bool) tea.Cmd {
	m := e.saveMsg()
	if m == nil {
		e.Close()
		return nil
	}
	e.pendingSaveClose = forceClose
	return func() tea.Msg { return m }
}

// confirmSaved finalizes a vim-path save after the board has successfully written
// the file: it drops the crash-recovery swap, rebaselines the dirty marker, and
// closes the editor for one-shot (append/prepend/journal) or forced (:wq) saves.
// No-op once the editor is closed (the textarea path closes before its write) or
// outside the vim path.
func (e *Editor) confirmSaved() {
	if e == nil || e.state == editorNone {
		return
	}
	if e.state == editorManagedFile && !e.vim {
		e.initialValue = e.textarea.Value()
		e.resetHistory(e.initialValue)
		return
	}
	if !e.vim || e.buf == nil {
		return
	}
	e.clearSwap()
	if e.pendingSaveClose || (e.state != editorEdit && e.state != editorManagedFile) {
		e.Close()
	} else {
		e.initialValue = e.buf.Text()
		e.cleanRevision = e.buf.Revision()
	}
	e.pendingSaveClose = false
}

func (e *Editor) saveMsg() tea.Msg {
	text := e.buf.Text()
	switch e.state {
	case editorEdit:
		return newStableEditorSaveMsg(e.itemTarget(), e.ColIndex, e.FileName, text)
	case editorManagedFile:
		return newStableManagedFileSaveMsg(e.ManagedPath, e.FileName, text)
	case editorAppend:
		return newStableEditorAppendMsg(e.itemTarget(), e.ColIndex, e.FileName, text)
	case editorPrepend:
		return newStableEditorPrependMsg(e.itemTarget(), e.ColIndex, e.FileName, text)
	case editorJournal:
		return newStableEditorJournalMsg(e.itemTarget(), e.ColIndex, e.FileName, text)
	}
	return nil
}

func (e *Editor) columnTarget() columnRef {
	return columnRef{Name: e.ColName, Path: e.ColPath}
}

func (e *Editor) itemTarget() itemRefStable {
	return itemRefStable{
		Column:   columnRef{Name: e.ColName, Path: e.ColPath},
		FileName: e.FileName,
		ItemPath: e.ItemPath,
	}
}

func (e *Editor) vimView() string {
	e.applySize() // re-fit the modal height to the current content before rendering
	frameW := overlayWidthForBody(e.buf.Width())
	if e.showHelp {
		return OverlayFrame{
			Title:   "Vim cheatsheet",
			Body:    vimCheatsheet(e.palette, e.buf.Width()),
			Footer:  lipgloss.NewStyle().Foreground(e.palette.FgMuted).Render("press any key to close"),
			Width:   frameW,
			Palette: e.palette,
		}.Render()
	}
	label := e.vimLabel()
	if e.IsDirty() {
		label = "● " + label
	}
	body := e.buf.View(e.palette)
	return OverlayFrame{Title: label, Body: body, Footer: e.vimFooter(), Width: frameW, Palette: e.palette}.Render()
}

// vimCheatsheet renders a grouped, two-column reference of the editor's vim
// coverage, sized to width.
func vimCheatsheet(p Palette, width int) string {
	type row struct{ keys, desc string }
	type group struct {
		title string
		rows  []row
	}
	groups := []group{
		{"Modes", []row{
			{"i a / I A", "insert (at / after · BOL / EOL)"},
			{"o O", "open line below / above"},
			{"v V", "visual char / line"},
			{": ", "command-line"},
			{"esc", "→ Normal · close from Normal"},
		}},
		{"Motion", []row{
			{"h j k l", "left down up right"},
			{"w b e", "word fwd / back / end"},
			{"0 ^ $", "line start / first / end"},
			{"gg G", "first / last line"},
			{"{ }", "paragraph up / down"},
			{"f F t T", "find char ; , to repeat"},
		}},
		{"Edit", []row{
			{"x dd yy", "del char / line · yank line"},
			{"p P", "paste after / before"},
			{"C D s S cc", "change/del EOL · subst"},
			{"r J ~", "replace char · join · case"},
			{"u  ctrl+r", "undo / redo"},
			{".", "repeat last change"},
		}},
		{"Operators", []row{
			{"d c y", "delete change yank +motion"},
			{"> <", "indent / dedent"},
			{"gu gU g~", "lower / upper / toggle"},
			{"iw i\" i( ip", "text objects (i=inner a=around)"},
			{"ctrl+a ctrl+x", "increment / decrement"},
		}},
		{"Surround / Markdown", []row{
			{"S{c} (visual)", "wrap selection"},
			{"ds{c} cs{o}{n}", "delete / change surround"},
			{"ctrl+t", "insert/toggle task"},
			{"tab", "toggle [ ] checkbox"},
			{"enter o", "continue list / checkbox"},
		}},
		{"Search / Command", []row{
			{"/pat ?pat n N", "search · next / prev"},
			{":w :q :q! :wq", "save / quit"},
			{":N  :N,M", "goto line · select range"},
			{":s/p/r/g  :%s", "substitute · & repeats"},
			{":lua expr", "eval Lua (ctx.line/lines)"},
		}},
		{"Editor", []row{
			{"ctrl+s", "save (edit stays open)"},
			{"ctrl+l", "line command menu"},
			{"ctrl+e", "expand / shrink modal"},
			{":help :h", "this cheatsheet"},
		}},
	}

	keyStyle := lipgloss.NewStyle().Bold(true).Foreground(p.FgBase)
	descStyle := lipgloss.NewStyle().Foreground(p.FgMuted)
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(p.Primary)

	colW := 17
	render := func(g group) string {
		var b strings.Builder
		b.WriteString(titleStyle.Render(g.title) + "\n")
		for _, r := range g.rows {
			keys := r.keys
			if lipgloss.Width(keys) < colW {
				keys += strings.Repeat(" ", colW-lipgloss.Width(keys))
			}
			b.WriteString(keyStyle.Render(keys) + descStyle.Render(r.desc) + "\n")
		}
		return strings.TrimRight(b.String(), "\n")
	}

	// Two columns of groups side by side.
	cols := []string{"", ""}
	for i, g := range groups {
		blk := render(g)
		if cols[i%2] != "" {
			cols[i%2] += "\n\n"
		}
		cols[i%2] += blk
	}
	gap := "    "
	return lipgloss.JoinHorizontal(lipgloss.Top, cols[0], gap, cols[1])
}

func (e *Editor) vimLabel() string {
	switch e.state {
	case editorEdit:
		return "Edit: " + e.FileName
	case editorAppend:
		return "Append to: " + e.FileName
	case editorPrepend:
		return "Prepend to: " + e.FileName
	case editorJournal:
		return "Journal entry for: " + e.FileName
	}
	return e.FileName
}

func (e *Editor) vimFooter() string {
	p := e.palette
	badge := e.modeBadge()
	var rest string
	switch {
	case e.buf.Mode() == vimbuf.ModeCommand:
		cl := e.buf.CommandLine()
		prompt, text := ":", cl
		if strings.HasPrefix(cl, "/") || strings.HasPrefix(cl, "?") {
			prompt, text = string(cl[0]), cl[1:]
		}
		// Render the command-line prominently: a bright prompt + bold text + a
		// block caret so it reads as an active input, not a status hint.
		rest = lipgloss.NewStyle().Bold(true).Foreground(p.Primary).Render(prompt) +
			lipgloss.NewStyle().Bold(true).Foreground(p.FgStrong).Render(text) +
			lipgloss.NewStyle().Foreground(p.Highlight).Render("▏")
		if name, usage := e.buf.CompletionHint(); name != "" {
			hint := usage
			if hint == "" {
				hint = name
			}
			rest += lipgloss.NewStyle().Foreground(p.FgMuted).Render("   " + hint)
		}
	case e.status != "":
		rest = lipgloss.NewStyle().Foreground(p.Warning).Render(e.status)
	}
	status := badge
	if rest != "" {
		status += "  " + rest
	}
	if pc := e.buf.PendingCommand(); pc != "" {
		status += "  " + lipgloss.NewStyle().Bold(true).Foreground(p.Highlight).Render(pc)
	}
	cur := e.buf.Cursor()
	pos := fmt.Sprintf("Ln %d/%d, Col %d", cur.Row+1, e.buf.LineCount(), cur.Col+1)
	status += "   " + lipgloss.NewStyle().Foreground(p.FgMuted).Render(pos)
	if e.swapWriteFailed {
		status += "  " + lipgloss.NewStyle().Bold(true).Foreground(p.Danger).
			Render("⚠ swap write failed — crash recovery off")
	}
	hints := RenderInlineHints([]Shortcut{{":w", "save"}, {":q", "quit"}, {"ctrl+t", "task"}, {"ctrl+l", "line cmd"}, {":help", "keys"}})
	return status + "\n" + hints
}

// modeBadge renders the current mode as a bright, padded, per-mode-colored pill
// so the active mode is unmistakable at a glance.
func (e *Editor) modeBadge() string {
	p := e.palette
	var bg color.Color
	switch e.buf.Mode() {
	case vimbuf.ModeInsert:
		bg = p.Success
	case vimbuf.ModeVisual, vimbuf.ModeVisualLine:
		bg = p.Warning
	case vimbuf.ModeCommand:
		bg = p.AccentAlt
	default:
		bg = p.Primary
	}
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(p.FgOnAccent).
		Background(bg).
		Padding(0, 1).
		Render(e.buf.ModeName())
}

func (e *Editor) View() string {
	return e.view()
}

func (e *Editor) viewInFrame(frameH int) string {
	if frameH > 0 {
		e.frameHeight = frameH
	}
	return e.view()
}

func (e *Editor) view() string {
	if e.state == editorNone {
		return ""
	}
	if e.vim && e.buf != nil && !isInputState(e.state) {
		return e.vimView()
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
	textareaHints = append(textareaHints, Shortcut{"ctrl+t", "task"}, Shortcut{"ctrl+e", expandLabel}, Shortcut{"ctrl+l", "line cmd"}, Shortcut{"esc", "cancel"})
	switch e.state {
	case editorEdit:
		label = "Edit: " + e.FileName
		hints = textareaHints
	case editorManagedFile:
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
		if e.NewContent != "" {
			label = "Paste as new item in: " + e.ColName
		} else {
			label = "New item in: " + e.ColName
		}
		hints = []Shortcut{{"enter", "confirm"}, {"esc", "cancel"}}
	case editorRenameItem:
		label = "Rename item: " + e.initialValue
		hints = []Shortcut{{"enter", "confirm"}, {"esc", "cancel"}}
	case editorRenameColumn:
		label = "Rename column: " + e.initialValue
		hints = []Shortcut{{"enter", "confirm"}, {"esc", "cancel"}}
	}

	if e.IsDirty() {
		label = "● " + label
	}

	var input string
	if isInputState(e.state) {
		input = e.textinput.View()
	} else {
		input = e.textarea.View()
	}

	return OverlayFrame{Title: label, Body: input, Footer: RenderInlineHints(hints), Palette: e.palette}.Render()
}

// openLineCommandsMsg asks the board to open the line-command menu over the
// still-open editor. Line is the current line's text, handed to the command as
// ctx.line; the command's return value (if any) replaces that line. Row is the
// line's 0-based row, carried through dispatch so a slow/async result replaces
// that row even if the cursor has since moved.
type openLineCommandsMsg struct {
	Target   itemRefStable
	ColIndex int
	FileName string
	Line     string
	Row      int
}

type editorSaveMsg struct {
	Target   itemRefStable
	ColIndex int
	FileName string
	Content  string
}

type managedFileSaveMsg struct {
	Path    string
	Label   string
	Content string
}

type editorAppendMsg struct {
	Target   itemRefStable
	ColIndex int
	FileName string
	Text     string
}

type editorPrependMsg struct {
	Target   itemRefStable
	ColIndex int
	FileName string
	Text     string
}

type editorJournalMsg struct {
	Target   itemRefStable
	ColIndex int
	FileName string
	Text     string
}

type editorNewMsg struct {
	Column   columnRef
	ColIndex int
	FileName string
	Content  string
}

type editorDiscardMsg struct{}

// editorConfirmDiscardMsg asks the board to open the discard-confirm dialog when
// esc is pressed on a dirty vim buffer in Normal mode.
type editorConfirmDiscardMsg struct{}

type deleteConfirmMsg struct {
	Target   itemRefStable
	ColIndex int
	FileName string
}

type renameItemRequestMsg struct {
	Target   itemRefStable
	ColIndex int
	OldName  string
	NewName  string
}

type renameColumnRequestMsg struct {
	Column   columnRef
	ColIndex int
	OldName  string
	NewName  string
}

type renameItemConfirmMsg struct {
	Target   itemRefStable
	ColIndex int
	OldName  string
	NewName  string
}

type renameColumnConfirmMsg struct {
	Column   columnRef
	ColIndex int
	OldName  string
	NewName  string
}
