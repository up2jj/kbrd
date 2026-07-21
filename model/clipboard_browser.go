package model

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"kbrd/clipboardring"
)

type ClipboardMenu struct {
	active    bool
	filtering bool
	entries   []clipboardring.Entry
	target    pasteMenuTarget
	flatPicker
	palette Palette
}

func (m *ClipboardMenu) Active() bool { return m.active }

func (m *ClipboardMenu) Open(entries []clipboardring.Entry, target pasteMenuTarget) {
	m.active = true
	m.filtering = false
	m.entries = append([]clipboardring.Entry(nil), entries...)
	m.target = target
	m.fuzzyList.Reset(len(m.entries), 0, m.haystack)
}

func (m *ClipboardMenu) Close() {
	m.active = false
	m.filtering = false
	m.entries = nil
	m.target = pasteMenuTarget{}
	m.fuzzyList.Clear()
}

func (m *ClipboardMenu) StartFilter() { m.filtering = true }

func (m *ClipboardMenu) StopFilter() {
	m.filtering = false
	m.filter = ""
	m.recompute()
}

func (m *ClipboardMenu) haystack(i int) string {
	entry := m.entries[i]
	return entryLabel(entry) + "  " + entryMeta(entry) + "  " + string(entry.Kind)
}

func (m *ClipboardMenu) Selected() (clipboardring.Entry, bool) {
	index, ok := m.SelectedIndex()
	if !ok || index < 0 || index >= len(m.entries) {
		return clipboardring.Entry{}, false
	}
	return m.entries[index], true
}

func (m *ClipboardMenu) Update(msg tea.KeyPressMsg) (action string, entry clipboardring.Entry) {
	if m.filtering {
		switch {
		case msg.Code == tea.KeyEsc:
			m.StopFilter()
		case msg.Code == tea.KeyBackspace:
			if !m.Backspace() {
				m.StopFilter()
			}
		case m.HandleInput(msg) == flatPickerInputConfirm:
			return m.confirmSelected()
		}
		return "", clipboardring.Entry{}
	}
	if key.Matches(msg, Keys.CustomCommandsClose) || msg.String() == "q" {
		m.Close()
		return "", clipboardring.Entry{}
	}
	switch msg.String() {
	case "/":
		m.StartFilter()
	case "i", "ctrl+i":
		return "import", clipboardring.Entry{}
	case "p", "ctrl+p":
		entry, ok := m.Selected()
		if ok {
			return "pin", entry
		}
	case "d", "ctrl+d":
		entry, ok := m.Selected()
		if ok {
			return "delete", entry
		}
	case "ctrl+x":
		return "clear", clipboardring.Entry{}
	case "j", "down":
		m.Move(1)
	case "k", "up":
		m.Move(-1)
	case "enter":
		return m.confirmSelected()
	}
	return "", clipboardring.Entry{}
}

func (m *ClipboardMenu) confirmSelected() (string, clipboardring.Entry) {
	entry, ok := m.Selected()
	if !ok {
		return "", clipboardring.Entry{}
	}
	m.Close()
	return "paste", entry
}

func (m *ClipboardMenu) View(termWidth, termHeight int) string {
	p := m.palette
	nameStyle := lipgloss.NewStyle().Foreground(p.FgBase)
	detailStyle := lipgloss.NewStyle().Foreground(p.FgMuted)
	selectedStyle := lipgloss.NewStyle().Bold(true).Foreground(p.FgInverse).Background(p.Primary)
	highlightStyle := lipgloss.NewStyle().Bold(true).Foreground(p.Highlight)
	highlightSelected := lipgloss.NewStyle().Bold(true).Foreground(p.Highlight).Background(p.Primary)
	filterLine := detailStyle.Render("press / to filter")
	if m.filtering {
		filterLine = flatPickerFilterLine(p, m.filter, detailStyle, nameStyle)
	}

	var body string
	if len(m.entries) == 0 {
		body = detailStyle.Render("clipboard history is empty — copy a card with c")
	} else if len(m.matches) == 0 {
		body = detailStyle.Render("no matching clipboard entries")
	} else {
		rows := make([]string, 0, len(m.matches))
		for displayIndex, match := range m.matches {
			entry := m.entries[match.Index]
			selected := displayIndex == m.selected
			base, hi := nameStyle, highlightStyle
			if selected {
				base, hi = selectedStyle, highlightSelected
			}
			labelText := entryLabel(entry)
			labelIndexes, metaIndexes := splitLabelDescMatchIndexes(labelText, match.MatchedIndexes)
			metaText := entryMeta(entry) + "  " + string(entry.Kind)
			metaText = ansi.Truncate(metaText, max(m.contentWidth(termWidth)-18, 1), "…")
			labelWidth := max(m.contentWidth(termWidth)-lipgloss.Width(metaText)-6, 12)
			labelText = ansi.Truncate(labelText, labelWidth, "…")
			label := renderHighlighted(labelText, labelIndexes, base, hi)
			meta := renderHighlighted(metaText, metaIndexes, detailStyle, highlightStyle)
			if selected {
				meta = renderHighlighted(metaText, metaIndexes, selectedStyle, highlightSelected)
			}
			gutter := " "
			if selected {
				gutter = lipgloss.NewStyle().Foreground(p.Primary).Bold(true).Render("▌")
			}
			rows = append(rows, gutter+" "+label+"  "+meta)
		}
		body = lipgloss.JoinVertical(lipgloss.Left, rows...)
	}

	preview := ""
	var hints []Shortcut
	if m.filtering {
		hints = []Shortcut{{Keys: "type", Label: "filter"}, {Keys: "↑/↓", Label: "select"}, {Keys: "enter", Label: "paste"}, {Keys: "esc", Label: "exit search"}}
	} else {
		hints = []Shortcut{
			{Keys: "↑/↓/j/k", Label: "select"},
			{Keys: "enter", Label: "paste"},
			{Keys: "i", Label: "import OS clipboard"},
			{Keys: "p", Label: "pin"},
			{Keys: "d", Label: "delete"},
			{Keys: "ctrl+x", Label: "clear all"},
			{Keys: "/", Label: "filter"},
			{Keys: "esc", Label: "close"},
		}
	}
	footer := RenderInlineHints(hints)
	if entry, ok := m.Selected(); ok {
		preview = m.previewBlockWidth(entry, clipboardPreviewWidth(termWidth, footer))
	}
	parts := []string{filterLine, "", body}
	if preview != "" {
		parts = append(parts, "", preview)
	}
	inner := flatPickerInner(termWidth, parts...)
	return OverlayFrame{Title: "Clipboard history", Body: inner, Footer: footer, Palette: p}.Render()
}

func (m *ClipboardMenu) previewBlock(entry clipboardring.Entry, termWidth int) string {
	return m.previewBlockWidth(entry, m.contentWidth(termWidth))
}

func (m *ClipboardMenu) previewBlockWidth(entry clipboardring.Entry, width int) string {
	return clipboardPreviewBlock(m.palette, fmt.Sprintf("Clipboard preview · %s", entry.Kind), entry.Text, width)
}

func (m *ClipboardMenu) contentWidth(termWidth int) int {
	return clipboardContentWidth(termWidth)
}

func clipboardContentWidth(termWidth int) int {
	const (
		minWidth = 50
		maxWidth = 88
	)
	if termWidth <= 0 {
		return maxWidth
	}
	return min(max(termWidth-16, minWidth), maxWidth)
}

// clipboardPreviewWidth keeps the boxed preview flush with the overlay's
// widest content, including its footer. The footer can be wider than the
// capped row width because it contains the complete shortcut legend.
func clipboardPreviewWidth(termWidth int, footer string) int {
	return max(clipboardContentWidth(termWidth), lipgloss.Width(footer))
}

func clipboardPreviewBlock(p Palette, title, text string, width int) string {
	contentWidth := max(width-4, 1)
	preview := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(p.FgDim).
		Padding(0, 1).
		Width(width).
		Foreground(p.FgBase).
		Render(formatClipboardPreview(text, contentWidth))
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(p.Primary)
	return lipgloss.JoinVertical(lipgloss.Left, titleStyle.Render(title), preview)
}

func entryLabel(entry clipboardring.Entry) string {
	text := strings.TrimSpace(strings.ReplaceAll(entry.Text, "\n", " "))
	if text == "" {
		return "(empty)"
	}
	return formatClipboardPreview(text, 64)
}

func entryMeta(entry clipboardring.Entry) string {
	when := TimeAgo(entry.Time)
	if entry.Pinned {
		when = "★ " + when
	}
	parts := make([]string, 0, 4)
	for _, part := range []string{entry.Source.Board, entry.Source.Column, entry.Source.Card, entry.Source.Heading} {
		if part != "" {
			parts = append(parts, part)
		}
	}
	source := strings.Join(parts, " / ")
	if source != "" {
		return fmt.Sprintf("%s · %s", when, source)
	}
	return when
}

// openBrowser is kept on the existing clipboard action object because it must
// share the terminal/system clipboard request state with paste and the editor.
func (a boardClipboardActions) openBrowser() tea.Cmd {
	b := a.board
	if len(b.columns) == 0 || b.selectedCol < 0 || b.selectedCol >= len(b.columns) {
		return b.notifier.Error("no column selected")
	}
	col := b.columns[b.selectedCol]
	if col.Virtual {
		return b.notifier.ErrorCause("clipboard history", errVirtualColumn)
	}
	store, err := b.clipboardStore()
	if err != nil {
		return b.notifier.ErrorCause("open clipboard history", err)
	}
	target := pasteMenuTarget{Column: refForColumn(col), ColIndex: b.selectedCol}
	if item := col.SelectedItem(); item != nil && !item.Separator {
		target.FileName = item.Name
		target.ItemPath = col.fullPathFor(item.Name)
	}
	b.clipboardMenu.Open(store.Entries(), target)
	return nil
}

func (a boardClipboardActions) openScratchpadBrowser() tea.Cmd {
	b := a.board
	store, err := b.clipboardStore()
	if err != nil {
		return b.notifier.ErrorCause("open clipboard history", err)
	}
	b.clipboardScratchpad = true
	b.clipboardMenu.Open(store.Entries(), pasteMenuTarget{})
	return nil
}

func (a boardClipboardActions) updateBrowser(msg tea.KeyPressMsg) tea.Cmd {
	b := a.board
	scratchTarget := b.clipboardScratchpad
	target := b.clipboardMenu.target
	if msg.String() == "ctrl+i" {
		return a.readRingImport(target)
	}
	action, entry := b.clipboardMenu.Update(msg)
	if !b.clipboardMenu.Active() && action == "" {
		b.clipboardScratchpad = false
	}
	store, err := b.clipboardStore()
	if err != nil {
		return b.notifier.ErrorCause("clipboard history", err)
	}
	switch action {
	case "pin":
		pinned, err := store.TogglePinned(entry.ID)
		if err != nil {
			return b.notifier.ErrorCause("pin clipboard entry", err)
		}
		b.clipboardMenu.Open(store.Entries(), b.clipboardMenu.target)
		if pinned {
			return b.notifier.Success("clipboard entry pinned")
		}
		return b.notifier.Success("clipboard entry unpinned")
	case "delete":
		if err := store.Delete(entry.ID); err != nil {
			return b.notifier.ErrorCause("delete clipboard entry", err)
		}
		b.clipboardMenu.Open(store.Entries(), b.clipboardMenu.target)
		return b.notifier.Success("clipboard entry deleted")
	case "clear":
		if err := store.Clear(); err != nil {
			return b.notifier.ErrorCause("clear clipboard history", err)
		}
		b.clipboardMenu.Open(nil, b.clipboardMenu.target)
		return b.notifier.Success("clipboard history cleared")
	case "import":
		return a.readRingImport(target)
	case "paste":
		if scratchTarget {
			b.clipboardScratchpad = false
			return b.scratchpadActions().insertClipboard(entry.Text)
		}
		_, cmd := b.pasteActions().openMenuWithText(target, entry.Text)
		if b.pasteMenu.Active() {
			b.clipboardReturn = true
			b.clipboardTarget = target
		}
		return cmd
	}
	return nil
}
