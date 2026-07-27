package model

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"kbrd/frontmatter"
)

type copyMenuEntry struct {
	Label   string
	Desc    string
	Content string
	Kind    string
}

// CopyMenu lets a card's filesystem identity and Markdown sections be copied
// without conflating them with the clipboard-history browser.
type CopyMenu struct {
	active  bool
	entries []copyMenuEntry
	targets []itemActionTarget
	column  columnRef
	palette Palette
	flatPicker
}

func (m *CopyMenu) Active() bool { return m.active }

func (m *CopyMenu) Open(column columnRef, targets []itemActionTarget, entries []copyMenuEntry) {
	m.active = true
	m.column = column
	m.targets = append([]itemActionTarget(nil), targets...)
	m.entries = append([]copyMenuEntry(nil), entries...)
	m.fuzzyList.Reset(len(m.entries), 0, m.haystack)
}

func (m *CopyMenu) Close() {
	m.active = false
	m.entries = nil
	m.targets = nil
	m.column = columnRef{}
	m.fuzzyList.Clear()
}

func (m *CopyMenu) haystack(i int) string {
	entry := m.entries[i]
	return entry.Label + " " + entry.Desc
}

func (m *CopyMenu) SelectedEntry() (copyMenuEntry, bool) {
	index, ok := m.fuzzyList.SelectedIndex()
	if !ok || index < 0 || index >= len(m.entries) {
		return copyMenuEntry{}, false
	}
	return m.entries[index], true
}

func (m *CopyMenu) Update(msg tea.KeyPressMsg) (confirmed, saveHistory bool) {
	if key.Matches(msg, Keys.CustomCommandsClose) {
		m.Close()
		return false, false
	}
	// Shift+Enter is indistinguishable from Enter in terminals and
	// multiplexers without modified-special-key reporting. Shift+C remains a
	// distinct printable character there, so keep it as the portable history
	// shortcut while also accepting the enhanced keyboard representation.
	if msg.Text == "C" || msg.Keystroke() == "shift+c" {
		return true, true
	}
	if m.flatPicker.HandleInput(msg) != flatPickerInputConfirm {
		return false, false
	}
	return true, msg.Mod&tea.ModShift != 0
}

func (m *CopyMenu) View(termWidth, _ int) string {
	if !m.active {
		return ""
	}
	p := m.palette
	nameStyle := lipgloss.NewStyle().Foreground(p.FgBase)
	descStyle := lipgloss.NewStyle().Foreground(p.FgMuted)
	selStyle := lipgloss.NewStyle().Bold(true).Foreground(p.FgInverse).Background(p.Primary)
	hiStyle := lipgloss.NewStyle().Foreground(p.Highlight).Bold(true)
	hiSelStyle := lipgloss.NewStyle().Bold(true).Foreground(p.Highlight).Background(p.Primary)
	gutterSel := lipgloss.NewStyle().Foreground(p.Primary).Bold(true).Render("▌")

	rows := make([]string, 0, len(m.matches))
	for i, match := range m.matches {
		entry := m.entries[match.Index]
		selected := i == m.selected
		nameIdx, descIdx := splitLabelDescMatchIndexes(entry.Label, match.MatchedIndexes)
		nameBase, descBase := nameStyle, descStyle
		hiName, hiDesc := hiStyle, hiStyle
		if selected {
			nameBase, descBase = selStyle, selStyle
			hiName, hiDesc = hiSelStyle, hiSelStyle
		}
		styled := renderHighlighted(entry.Label, nameIdx, nameBase, hiName)
		if entry.Desc != "" {
			sep := "  —  "
			if selected {
				styled += selStyle.Render(sep)
			} else {
				styled += descStyle.Render(sep)
			}
			styled += renderHighlighted(entry.Desc, descIdx, descBase, hiDesc)
		}
		gutter := " "
		if selected {
			gutter = gutterSel
			styled = selStyle.Render(" ") + styled + selStyle.Render(" ")
		}
		rows = append(rows, gutter+" "+styled)
	}
	body := helpDimStyle.Render("no matches")
	if len(rows) > 0 {
		body = lipgloss.JoinVertical(lipgloss.Left, rows...)
	}
	filter := flatPickerFilterLine(p, m.filter, descStyle, nameStyle)
	footer := RenderInlineHints([]Shortcut{{Keys: "type", Label: "filter"}, {Keys: "↑/↓", Label: "select"}, {Keys: "enter", Label: "copy"}, {Keys: "C", Label: "copy + history"}, {Keys: "esc", Label: "cancel"}})
	inner := flatPickerInner(termWidth, filter, "", body)
	return OverlayFrame{Title: "Copy…", Body: inner, Footer: footer, Palette: p}.Render()
}

type copyMenuActions struct{ board *Board }

func (b *Board) copyMenuActions() copyMenuActions { return copyMenuActions{board: b} }

func (a copyMenuActions) open(ctx itemActionContext) tea.Cmd {
	entries, err := buildCopyMenuEntries(ctx.Column, ctx.Targets)
	if err != nil {
		return a.board.notifier.ErrorCause("failed to prepare copy menu", err)
	}
	a.board.copyMenu.Open(refForColumn(ctx.Column), ctx.Targets, entries)
	return nil
}

func (a copyMenuActions) update(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	b := a.board
	confirmed, saveHistory := b.copyMenu.Update(msg)
	if !confirmed {
		return b, nil
	}
	entry, ok := b.copyMenu.SelectedEntry()
	if !ok {
		return b, nil
	}
	targets := append([]itemActionTarget(nil), b.copyMenu.targets...)
	column := b.copyMenu.column
	b.copyMenu.Close()

	col, err := b.resolveDelayedColumnRef(column)
	if err != nil {
		return b, b.notifier.ErrorCause("failed to copy", err)
	}
	card := targets[0].Item.Name
	if len(targets) > 1 {
		card = strconv.Itoa(len(targets)) + " cards"
	}
	text := entry.Content
	if !saveHistory {
		return b, b.utilityActions().copyToClipboard([]byte(text))
	}
	store, err := b.clipboardStore()
	if err != nil {
		return b, b.notifier.ErrorCause("open clipboard history", err)
	}
	historyEntry := b.newClipboardEntry(text, b.clipboardSource(col, card), map[string]any{
		"bytes":      len(text),
		"lines":      strings.Count(text, "\n") + 1,
		"card_count": len(targets),
		"copy_kind":  entry.Kind,
	})
	return b, b.utilityActions().copyToClipboardWithEntry([]byte(text), store, historyEntry)
}

type copyCardParts struct {
	item        Item
	raw         string
	frontmatter string
	body        string
	hasFront    bool
}

func buildCopyMenuEntries(col *Column, targets []itemActionTarget) ([]copyMenuEntry, error) {
	if col == nil || len(targets) == 0 {
		return nil, fmt.Errorf("no card selected")
	}
	parts := make([]copyCardParts, 0, len(targets))
	for _, target := range targets {
		raw, err := col.CopyContent(target.Item.Name)
		if err != nil {
			return nil, fmt.Errorf("reading %s.md: %w", target.Item.Name, err)
		}
		block, body, fenced := frontmatter.Split(string(raw))
		front := ""
		if fenced {
			front = "---\n" + block + "---\n"
		}
		parts = append(parts, copyCardParts{item: target.Item, raw: string(raw), frontmatter: front, body: body, hasFront: fenced})
	}

	entries := []copyMenuEntry{
		{Label: "Copy content", Desc: "Entire Markdown file", Content: joinCopyParts(parts, func(p copyCardParts) (string, bool) { return p.raw, true }), Kind: "content"},
		{Label: "Copy file path", Desc: pluralCopyDesc(len(parts), "Absolute path", "Absolute paths"), Content: joinCopyLines(parts, func(p copyCardParts) string { return p.item.FullPath }), Kind: "file_path"},
		{Label: "Copy file name", Desc: pluralCopyDesc(len(parts), "Name with extension", "Names with extensions"), Content: joinCopyLines(parts, copyFileName), Kind: "file_name"},
	}
	if hasCopyFrontmatter(parts) {
		entries = append(entries, copyMenuEntry{Label: "Copy frontmatter", Desc: "Leading YAML block", Content: joinCopyParts(parts, func(p copyCardParts) (string, bool) { return p.frontmatter, p.hasFront }), Kind: "frontmatter"})
	}
	entries = append(entries, copyMenuEntry{Label: "Copy body only", Desc: "Markdown without frontmatter", Content: joinCopyParts(parts, func(p copyCardParts) (string, bool) { return p.body, true }), Kind: "body"})
	return entries, nil
}

func pluralCopyDesc(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}

func copyFileName(p copyCardParts) string {
	if p.item.FullPath != "" {
		return filepath.Base(p.item.FullPath)
	}
	return p.item.Name + ".md"
}

func hasCopyFrontmatter(parts []copyCardParts) bool {
	for _, part := range parts {
		if part.hasFront {
			return true
		}
	}
	return false
}

func joinCopyLines(parts []copyCardParts, value func(copyCardParts) string) string {
	lines := make([]string, 0, len(parts))
	for _, part := range parts {
		lines = append(lines, value(part))
	}
	return strings.Join(lines, "\n")
}

func joinCopyParts(parts []copyCardParts, value func(copyCardParts) (string, bool)) string {
	if len(parts) == 1 {
		text, _ := value(parts[0])
		return text
	}
	var out strings.Builder
	for _, part := range parts {
		text, include := value(part)
		if !include {
			continue
		}
		if out.Len() > 0 {
			out.WriteString("\n\n")
		}
		out.WriteString("--- ")
		out.WriteString(copyFileName(part))
		out.WriteString(" ---\n")
		out.WriteString(text)
	}
	return out.String()
}
