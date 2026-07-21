package model

import (
	"fmt"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	cardhistory "kbrd/history"
)

type timelineLoadedMsg struct {
	seq    int
	events []cardhistory.Event
	err    error
}

type timelineDocumentMsg struct {
	seq     int
	index   int
	kind    string
	content string
	err     error
}

type timelineRestoreMsg struct {
	seq      int
	event    cardhistory.Event
	contents []byte
	err      error
}

// cardHistoryProvider is defined by its TUI consumer so another history source
// can replace Git without changing the timeline model.
type cardHistoryProvider interface {
	History(cardPath string) ([]cardhistory.Event, error)
	Snapshot(event cardhistory.Event) ([]byte, error)
	Diff(event cardhistory.Event) (string, error)
}

// Timeline is the card-centric Git history modal. It owns presentation and
// navigation; Git interpretation remains in history.GitProvider.
type Timeline struct {
	active   bool
	loading  bool
	seq      int
	name     string
	cardPath string
	provider cardHistoryProvider
	events   []cardhistory.Event
	nav      groupedMenuNav
	palette  Palette
	width    int
	document Peek
	docKind  string
	docIndex int
}

func (t *Timeline) Active() bool { return t.active }

func (t *Timeline) SetPalette(p Palette) { t.palette, t.document.palette = p, p }

func (t *Timeline) SetSize(w, _ int) { t.width = w }

func (t *Timeline) Open(repoRoot, cardPath, name string) tea.Cmd {
	t.seq++
	seq := t.seq
	t.active, t.loading = true, true
	t.name, t.cardPath = name, cardPath
	t.provider = cardhistory.GitProvider{RepoRoot: repoRoot}
	t.events = nil
	t.nav.Reset()
	t.document.Close()
	t.docKind = ""
	provider := t.provider
	return func() tea.Msg {
		events, err := provider.History(cardPath)
		return timelineLoadedMsg{seq: seq, events: events, err: err}
	}
}

func (t *Timeline) Close() {
	t.active, t.loading = false, false
	t.events = nil
	t.nav.Reset()
	t.document.Close()
	t.docKind = ""
}

func (t *Timeline) loaded(msg timelineLoadedMsg) {
	if !t.active || msg.seq != t.seq || msg.err != nil {
		return
	}
	t.loading = false
	t.events = msg.events
	t.nav.Reset()
	for i := range t.events {
		t.nav.nav = append(t.nav.nav, i)
	}
}

func (t *Timeline) selected() (cardhistory.Event, int, bool) {
	row, ok := t.nav.SelectedRow()
	if !ok || row < 0 || row >= len(t.events) {
		return cardhistory.Event{}, 0, false
	}
	return t.events[row], row, true
}

func (t *Timeline) Update(msg tea.KeyPressMsg) tea.Cmd {
	if t.docKind != "" {
		switch {
		case key.Matches(msg, Keys.PeekClose):
			t.document.Close()
			t.docKind = ""
		case t.docKind == "snapshot" && (msg.String() == "left" || msg.String() == "h"):
			return t.openAdjacentSnapshot(1)
		case t.docKind == "snapshot" && (msg.String() == "right" || msg.String() == "l"):
			return t.openAdjacentSnapshot(-1)
		default:
			t.document.Update(msg)
		}
		return nil
	}
	if t.loading {
		if key.Matches(msg, Keys.PeekClose) {
			t.Close()
		}
		return nil
	}
	switch {
	case key.Matches(msg, Keys.PeekClose):
		t.Close()
	case msg.String() == "enter", msg.String() == "s":
		return t.openDocument("snapshot")
	case msg.String() == "d":
		return t.openDocument("diff")
	case msg.String() == "c":
		return t.restoreCopy()
	case key.Matches(msg, Keys.CursorUp):
		t.nav.UpdateKey("up")
	case key.Matches(msg, Keys.CursorDown):
		t.nav.UpdateKey("down")
	case key.Matches(msg, Keys.PeekTop):
		t.nav.UpdateKey("home")
	case key.Matches(msg, Keys.PeekBottom):
		t.nav.UpdateKey("end")
	}
	return nil
}

func (t *Timeline) openDocument(kind string) tea.Cmd {
	event, index, ok := t.selected()
	if !ok {
		return nil
	}
	return t.documentCmd(kind, event, index)
}

func (t *Timeline) documentCmd(kind string, event cardhistory.Event, index int) tea.Cmd {
	provider, seq := t.provider, t.seq
	return func() tea.Msg {
		var content string
		var err error
		if kind == "diff" {
			content, err = provider.Diff(event)
		} else {
			var data []byte
			data, err = provider.Snapshot(event)
			content = string(data)
		}
		return timelineDocumentMsg{seq: seq, index: index, kind: kind, content: content, err: err}
	}
}

func (t *Timeline) showDocument(msg timelineDocumentMsg) {
	if !t.active || msg.seq != t.seq || msg.err != nil || msg.index < 0 || msg.index >= len(t.events) {
		return
	}
	t.docKind, t.docIndex = msg.kind, msg.index
	e := t.events[msg.index]
	title := strings.ToUpper(msg.kind[:1]) + msg.kind[1:] + " · " + e.Time.Format("Jan 2, 2006 15:04")
	t.document.Open(title, msg.content, t.width)
}

func (t *Timeline) openAdjacentSnapshot(delta int) tea.Cmd {
	index := t.docIndex + delta
	for index >= 0 && index < len(t.events) && t.events[index].Type == cardhistory.EventDeleted {
		index += delta
	}
	if index < 0 || index >= len(t.events) {
		return nil
	}
	t.nav.selected = index
	return t.documentCmd("snapshot", t.events[index], index)
}

func (t *Timeline) restoreCopy() tea.Cmd {
	event, _, ok := t.selected()
	if !ok {
		return nil
	}
	provider, seq := t.provider, t.seq
	return func() tea.Msg {
		contents, err := provider.Snapshot(event)
		return timelineRestoreMsg{seq: seq, event: event, contents: contents, err: err}
	}
}

func (t *Timeline) View(termWidth, termHeight int) string {
	if !t.active {
		return ""
	}
	if t.docKind != "" {
		hints := []Shortcut{{"↑/↓", "scroll"}}
		if t.docKind == "snapshot" {
			hints = append(hints, Shortcut{"←/→", "revision"})
		}
		hints = append(hints, Shortcut{"q/esc", "back"})
		return t.document.ViewWithHints(termWidth, termHeight, hints)
	}
	footer := RenderInlineHints([]Shortcut{{"↑/↓", "event"}, {"enter", "snapshot"}, {"d", "diff"}, {"c", "restore copy"}, {"q/esc", "close"}})
	textW := max(lipgloss.Width(footer)+2, 66)
	if termWidth > 0 {
		textW = min(textW, max(termWidth-8, 1))
	}
	if t.loading {
		return OverlayFrame{Title: "Timeline · " + t.name, Body: "Loading card history…", Footer: RenderInlineHints([]Shortcut{{"q/esc", "close"}}), Width: overlayWidthForBody(textW), Palette: t.palette}.Render()
	}
	if len(t.events) == 0 {
		return OverlayFrame{Title: "Timeline · " + t.name, Body: "No committed history for this card.", Footer: RenderInlineHints([]Shortcut{{"q/esc", "close"}}), Width: overlayWidthForBody(textW), Palette: t.palette}.Render()
	}
	body, pos := renderGroupedPickerBody(groupedPickerBody{Palette: t.palette, Rows: len(t.events), TermHeight: termHeight, TextWidth: textW, Compact: true, Nav: &t.nav, RenderRow: func(row int, selected bool) string { return t.renderRow(t.events[row], selected, textW) }})
	gap := max(textW-lipgloss.Width(footer)-lipgloss.Width(pos), 1)
	return OverlayFrame{Title: "Timeline · " + t.name, Body: body, Footer: footer + strings.Repeat(" ", gap) + pos, Width: overlayWidthForBody(textW), Palette: t.palette}.Render()
}

func (t *Timeline) renderRow(e cardhistory.Event, selected bool, width int) string {
	icon := map[cardhistory.EventType]string{cardhistory.EventCreated: "+", cardhistory.EventEdited: "~", cardhistory.EventMetadata: "≡", cardhistory.EventMoved: "→", cardhistory.EventRenamed: "↪", cardhistory.EventDeleted: "−"}[e.Type]
	label := fmt.Sprintf("%s  %-18s  %s", icon, TimeAgo(e.Time), e.Summary)
	if e.Author != "" {
		label += "  · " + e.Author
	}
	prefix := "  "
	style := lipgloss.NewStyle().Foreground(t.palette.FgSoft)
	if selected {
		prefix, style = "> ", lipgloss.NewStyle().Bold(true).Foreground(t.palette.Highlight)
	}
	return lipgloss.NewStyle().Width(max(width, 1)).Render(style.Render(prefix + label))
}

func restoredCopyPath(cardPath string, event cardhistory.Event) string {
	dir := filepath.Dir(cardPath)
	base := strings.TrimSuffix(filepath.Base(cardPath), filepath.Ext(cardPath))
	return filepath.Join(dir, fmt.Sprintf("%s (restored %s).md", base, event.Time.Format("Jan 2")))
}
