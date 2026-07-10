package model

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/atotto/clipboard"

	"kbrd/board"
)

// pasteMode selects how a clipboard paste merges into the target card.
type pasteMode int

const (
	pasteAtEnd pasteMode = iota
	pasteAtStart
	pasteReplace
	pasteJournal
)

// pasteRequestMsg dispatches a chosen paste mode to the async paste command. It
// carries a target resolved on the UI goroutine — the column name/path and the
// item's full path — rather than a column index, so the write (and the later
// finalize) bind to a stable identity even if columns are reloaded, reordered,
// or the board is switched before the command runs.
type pasteRequestMsg struct {
	ColName  string
	ColPath  string
	ItemPath string
	FileName string
	Mode     pasteMode
}

// pasteDoneMsg reports a successful clipboard paste back to the Update loop so
// the reload, the ItemSaved publish, and the toast all run on the UI goroutine
// (the event bus and in-memory column state are not goroutine-safe). It carries
// the same stable identity as the request; handlePasteDone resolves it against
// the current board and no-ops if the target is gone. Kind is the ItemSaved kind
// for the paste mode (prepend/append/journal map directly; a whole-file replace
// is reported as "save").
type pasteDoneMsg struct {
	ColName  string
	ColPath  string
	FileName string
	Kind     string
	Verb     string
}

type pasteNewItemMsg struct {
	Column   columnRef
	ColIndex int
	Content  string
}

type boardPasteActions struct {
	board *Board
}

func (b *Board) pasteActions() boardPasteActions {
	return boardPasteActions{board: b}
}

// openMenu reads the clipboard and opens the paste-mode picker. The target is
// resolved here, on the UI goroutine, into stable identities carried by each
// request, so nothing downstream depends on the column's current index. An
// empty/unavailable clipboard is reported without opening.
func (a boardPasteActions) openMenu(colIdx int, col *Column, item *Item) tea.Cmd {
	b := a.board
	text, err := clipboard.ReadAll()
	if err != nil || text == "" {
		return b.notifier.Error("clipboard empty or unavailable")
	}
	if colIdx < 0 || colIdx >= len(b.columns) {
		return b.notifier.Error("column not found")
	}
	if col == nil {
		col = b.columns[colIdx]
	}
	if col.Virtual {
		return b.notifier.ErrorCause("", errVirtualColumn)
	}

	entries := []pasteMenuEntry{
		{
			Label: "Paste as new file",
			Desc:  "Create a .md card in " + col.Name + " from clipboard text",
			Msg:   pasteNewItemMsg{Column: refForColumn(col), ColIndex: colIdx, Content: text},
		},
	}

	defaultIndex := 0
	if item != nil {
		fileName := item.Name
		itemPath := col.fullPathFor(fileName)
		if itemPath == "" {
			return b.notifier.Error("item not found: " + fileName)
		}
		req := func(mode pasteMode) pasteRequestMsg {
			return pasteRequestMsg{ColName: col.Name, ColPath: col.Path, ItemPath: itemPath, FileName: fileName, Mode: mode}
		}
		into := "Into " + fileName + ".md"
		entries = append(entries,
			pasteMenuEntry{Label: "Prepend", Desc: into, Msg: req(pasteAtStart)},
			pasteMenuEntry{Label: "Append at end", Desc: into, Msg: req(pasteAtEnd)},
			pasteMenuEntry{Label: "Journal entry", Desc: into, Msg: req(pasteJournal)},
		)
		defaultIndex = 2
	}

	b.pasteMenu.Open(entries, defaultIndex)
	return nil
}

func (a boardPasteActions) openNewItem(msg pasteNewItemMsg) (tea.Model, tea.Cmd) {
	b := a.board
	col, err := b.resolveDelayedColumnRef(msg.Column)
	if err != nil {
		return b, b.notifier.ErrorCause("", err)
	}
	return b, b.editor.OpenNewWithContent(msg.ColIndex, col.Name, col.Path, msg.Content)
}

// pasteToItem performs the clipboard write for a chosen mode on a goroutine,
// writing directly to the request's captured item path (never touching
// b.columns, which may have changed). Completion is reported via pasteDoneMsg so
// handlePasteDone can finish on the UI thread.
func (a boardPasteActions) pasteToItem(msg pasteRequestMsg) tea.Cmd {
	b := a.board
	// Capture journal config on the UI goroutine; the worker must not read live
	// Board state (a board switch / config reload could race it).
	detectDate := b.cfg.Journal.DetectDate
	return func() tea.Msg {
		text, err := clipboard.ReadAll()
		if err != nil || text == "" {
			return notifyMsg{Message: "clipboard empty or unavailable", Type: notifyError}
		}
		var verb, kind string
		switch msg.Mode {
		case pasteAtStart:
			err = board.PrependLine(msg.ItemPath, text)
			verb, kind = "prepended to ", "prepend"
		case pasteJournal:
			at, body := journalStampWith(detectDate, text)
			err = board.JournalLine(msg.ItemPath, at, body)
			verb, kind = "journaled to ", "journal"
		case pasteReplace:
			err = board.ReplaceFileContent(msg.ItemPath, text)
			verb, kind = "replaced ", "save"
		default:
			err = board.AppendLine(msg.ItemPath, text)
			verb, kind = "appended to ", "append"
		}
		if err != nil {
			return notifyMsg{Message: "failed to paste: " + err.Error(), Type: notifyError}
		}
		// Reload, publish ItemSaved, and toast on the UI goroutine — see pasteDoneMsg.
		return pasteDoneMsg{ColName: msg.ColName, ColPath: msg.ColPath, FileName: msg.FileName, Kind: kind, Verb: verb}
	}
}

// handlePasteDone finalizes a successful clipboard paste on the UI goroutine:
// resolve the target column by stable identity, reload it, keep the pasted item
// selected, publish ItemSaved (so item_saved hooks fire for pastes exactly as
// they do for editor append/prepend/journal/save), and toast. If the board no
// longer holds the target (reloaded away, reordered to nothing, or the board was
// switched), it no-ops — the disk write already landed and the fs watcher will
// reconcile the view.
func (a boardPasteActions) handleDone(msg pasteDoneMsg) (tea.Model, tea.Cmd) {
	b := a.board
	col := a.resolveColumn(msg)
	if col == nil {
		return b, nil
	}
	b.finalizeItemSave(col, msg.FileName, msg.Kind)
	return b, b.notifier.Success(msg.Verb + msg.FileName)
}

// resolvePasteColumn finds the column a completed paste belongs to by stable
// identity: a real column matches by directory path (unique within and across
// boards, and stable across reload/reorder); a virtual column (no path) matches
// by name only if it still holds the written file. Returns nil when the target
// is no longer present.
func (a boardPasteActions) resolveColumn(msg pasteDoneMsg) *Column {
	b := a.board
	for _, c := range b.columns {
		if msg.ColPath != "" {
			if c.Path == msg.ColPath {
				return c
			}
			continue
		}
		if c.Name == msg.ColName && c.fullPathFor(msg.FileName) != "" {
			return c
		}
	}
	return nil
}

type pasteMenuEntry struct {
	Label  string
	Desc   string
	Msg    tea.Msg
	Danger bool
}

type PasteMenu struct {
	active bool
	fuzzyList
	entries []pasteMenuEntry
	palette Palette
}

func (m *PasteMenu) Active() bool { return m.active }

func (m *PasteMenu) Open(entries []pasteMenuEntry, defaultIndex int) {
	m.active = true
	m.entries = append([]pasteMenuEntry(nil), entries...)
	m.fuzzyList.Reset(len(m.entries), defaultIndex, m.haystack)
}

func (m *PasteMenu) Close() {
	m.active = false
	m.entries = nil
	m.fuzzyList.Clear()
}

func (m *PasteMenu) haystack(i int) string {
	e := m.entries[i]
	if e.Desc != "" {
		return e.Label + "  " + e.Desc
	}
	return e.Label
}

func (m *PasteMenu) Update(msg tea.KeyPressMsg) tea.Cmd {
	if key.Matches(msg, Keys.CustomCommandsClose) {
		m.Close()
		return nil
	}
	switch msg.Code {
	case tea.KeyUp:
		m.fuzzyList.Move(-1)
	case tea.KeyDown:
		m.fuzzyList.Move(1)
	case tea.KeyEnter:
		index, ok := m.fuzzyList.SelectedIndex()
		if !ok {
			m.Close()
			return nil
		}
		entry := m.entries[index]
		m.Close()
		return func() tea.Msg { return entry.Msg }
	case tea.KeyBackspace:
		m.fuzzyList.Backspace()
	default:
		m.fuzzyList.Append(msg.Text)
	}
	return nil
}

func (m *PasteMenu) View(termWidth, termHeight int) string {
	p := m.palette
	keyStyle := lipgloss.NewStyle().Foreground(p.Highlight).Bold(true)
	nameStyle := lipgloss.NewStyle().Foreground(p.FgBase)
	descStyle := lipgloss.NewStyle().Foreground(p.FgMuted)
	dangerStyle := lipgloss.NewStyle().Foreground(p.Danger)
	selStyle := lipgloss.NewStyle().Bold(true).Foreground(p.FgInverse).Background(p.Primary)
	hiStyle := lipgloss.NewStyle().Foreground(p.Highlight).Bold(true)
	hiSelStyle := lipgloss.NewStyle().Bold(true).Foreground(p.Highlight).Background(p.Primary)
	gutterSel := lipgloss.NewStyle().Foreground(p.Primary).Bold(true).Render("▌")

	filterText := m.filter
	if filterText == "" {
		filterText = descStyle.Render("type to filter…")
	} else {
		filterText = nameStyle.Render(filterText)
	}
	filterLine := keyStyle.Render("> ") + filterText

	var body string
	switch {
	case len(m.entries) == 0:
		body = helpDimStyle.Render("no paste operations available")
	case len(m.matches) == 0:
		body = helpDimStyle.Render("no matches")
	default:
		rows := make([]string, 0, len(m.matches))
		for i, match := range m.matches {
			e := m.entries[match.Index]
			selected := i == m.selected
			nameIdx, descIdx := splitLabelDescMatchIndexes(e.Label, match.MatchedIndexes)
			nameBase := nameStyle
			if e.Danger {
				nameBase = dangerStyle
			}
			descBase := descStyle
			hiName := hiStyle
			hiDesc := hiStyle
			if selected {
				nameBase = selStyle
				descBase = selStyle
				hiName = hiSelStyle
				hiDesc = hiSelStyle
			}
			styled := renderHighlighted(e.Label, nameIdx, nameBase, hiName)
			if e.Desc != "" {
				sep := "  —  "
				if selected {
					styled += selStyle.Render(sep)
				} else {
					styled += descStyle.Render(sep)
				}
				styled += renderHighlighted(e.Desc, descIdx, descBase, hiDesc)
			}
			gutter := " "
			if selected {
				gutter = gutterSel
				styled = selStyle.Render(" ") + styled + selStyle.Render(" ")
			}
			rows = append(rows, gutter+" "+styled)
		}
		body = lipgloss.JoinVertical(lipgloss.Left, rows...)
	}

	footer := RenderInlineHints([]Shortcut{
		{Keys: "type", Label: "filter"},
		{Keys: "↑/↓", Label: "select"},
		{Keys: "enter", Label: "paste"},
		{Keys: "esc", Label: "cancel"},
	})
	inner := lipgloss.JoinVertical(lipgloss.Left, filterLine, "", body)
	minInner := 50
	if termWidth > 0 && termWidth-12 < minInner {
		minInner = termWidth - 12
	}
	if lipgloss.Width(inner) < minInner {
		inner = lipgloss.NewStyle().Width(minInner).Render(inner)
	}
	return OverlayFrame{Title: "Paste from clipboard", Body: inner, Footer: footer, Palette: p}.Render()
}
