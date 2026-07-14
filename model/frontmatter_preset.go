package model

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"kbrd/boardops"
	"kbrd/config"
	"kbrd/events"
	"kbrd/frontmatter"
)

type frontmatterPresetMenuRow struct {
	header   bool
	title    string
	preset   config.FrontmatterPreset
	matchIdx []int
	disabled bool
}

// frontmatterPresetMenu is the board-local, fuzzy-searchable preset picker.
// It captures stable targets when opened so config or watcher reloads cannot
// redirect a delayed apply to another card.
type frontmatterPresetMenu struct {
	active       bool
	palette      Palette
	column       columnRef
	colIndex     int
	presets      []config.FrontmatterPreset
	targets      []itemRefStable
	selectedName string
	rows         []frontmatterPresetMenuRow
	groupedPicker
}

func (m *frontmatterPresetMenu) Active() bool { return m.active }

func (m *frontmatterPresetMenu) SetPalette(p Palette) { m.palette = p }

func (m *frontmatterPresetMenu) Close() {
	m.active = false
	m.column = columnRef{}
	m.colIndex = 0
	m.presets = nil
	m.targets = nil
	m.selectedName = ""
	m.rows = nil
	m.groupedPicker.Reset()
}

func (m *frontmatterPresetMenu) Open(colIndex int, column columnRef, presets []config.FrontmatterPreset, targets []itemActionTarget, selectedName string) {
	m.active = true
	m.column = column
	m.colIndex = colIndex
	m.presets = append([]config.FrontmatterPreset(nil), presets...)
	m.targets = make([]itemRefStable, 0, len(targets))
	for _, target := range targets {
		m.targets = append(m.targets, target.Ref)
	}
	m.selectedName = selectedName
	m.groupedPicker.Reset()
	m.recompute()
}

func (m *frontmatterPresetMenu) recompute() {
	m.rows = m.rows[:0]
	m.groupedMenuNav.BeginRebuild()

	if m.filtering && m.filter != "" {
		entries := m.entries()
		matches := filterFuzzy(len(entries), m.filter, func(i int) string {
			return presetHaystack(entries[i])
		})
		for _, match := range matches {
			m.rows = append(m.rows, frontmatterPresetMenuRow{
				preset:   entries[match.Index],
				matchIdx: match.MatchedIndexes,
			})
			m.nav = append(m.nav, len(m.rows)-1)
		}
	} else {
		m.appendGroup("Column presets", m.presetsForScope(true))
		m.appendGroup("Board presets", m.presetsForScope(false))
	}
	m.groupedMenuNav.Clamp(len(m.rows))
}

func (m *frontmatterPresetMenu) appendGroup(title string, presets []config.FrontmatterPreset) {
	m.rows = append(m.rows, frontmatterPresetMenuRow{header: true, title: title})
	if len(presets) == 0 {
		m.rows = append(m.rows, frontmatterPresetMenuRow{
			preset:   config.FrontmatterPreset{Name: "No presets", Description: "Nothing in this scope"},
			disabled: true,
		})
		return
	}
	for _, preset := range presets {
		m.rows = append(m.rows, frontmatterPresetMenuRow{preset: preset})
		m.nav = append(m.nav, len(m.rows)-1)
	}
}

func (m *frontmatterPresetMenu) entries() []config.FrontmatterPreset {
	entries := m.presetsForScope(true)
	return append(entries, m.presetsForScope(false)...)
}

func (m *frontmatterPresetMenu) presetsForScope(columnScoped bool) []config.FrontmatterPreset {
	presets := make([]config.FrontmatterPreset, 0, len(m.presets))
	for _, preset := range m.presets {
		if (len(preset.Columns) > 0) == columnScoped {
			presets = append(presets, preset)
		}
	}
	return presets
}

func presetHaystack(preset config.FrontmatterPreset) string {
	if preset.Description == "" {
		return preset.Name
	}
	return preset.Name + "  " + preset.Description
}

func (m *frontmatterPresetMenu) Filtering() bool { return m.filtering }

func (m *frontmatterPresetMenu) StartFilter() {
	m.groupedPicker.StartFilter()
	m.recompute()
}

func (m *frontmatterPresetMenu) StopFilter() {
	m.groupedPicker.StopFilter()
	m.recompute()
}

func (m *frontmatterPresetMenu) AppendFilter(s string) {
	m.groupedPicker.AppendFilter(s)
	m.recompute()
}

func (m *frontmatterPresetMenu) Backspace() {
	if m.groupedPicker.Backspace() {
		m.recompute()
		return
	}
	m.StopFilter()
}

func (m *frontmatterPresetMenu) Update(msg tea.KeyPressMsg) {
	m.groupedMenuNav.UpdateKey(msg.String())
}

func (m *frontmatterPresetMenu) SelectedPreset() (config.FrontmatterPreset, bool) {
	row, ok := m.groupedMenuNav.SelectedRow()
	if !ok || row < 0 || row >= len(m.rows) || m.rows[row].disabled || m.rows[row].header {
		return config.FrontmatterPreset{}, false
	}
	return m.rows[row].preset, true
}

func (m *frontmatterPresetMenu) View(termWidth, termHeight int) string {
	if !m.active {
		return ""
	}
	footer := m.footerHints()
	textW := m.contentWidth(termWidth)
	overview := m.overview(textW)
	bodyHeight := termHeight
	if overview != "" {
		bodyHeight -= lipgloss.Height(overview) + 1
	}
	body, pos := renderGroupedPickerBody(groupedPickerBody{
		Palette: m.palette, Rows: len(m.rows), TermHeight: bodyHeight, TextWidth: textW,
		Filtering: m.filtering, Filter: m.filter, Nav: &m.groupedMenuNav,
		RenderRow: func(row int, selected bool) string { return m.renderRow(m.rows[row], selected) },
	})
	if overview != "" {
		body = lipgloss.JoinVertical(lipgloss.Left, body, "", overview)
	}
	gap := max(textW-lipgloss.Width(footer)-lipgloss.Width(pos), 1)
	title := "Frontmatter presets"
	if m.column.Name != "" {
		title += " · " + m.column.Name
	}
	return OverlayFrame{
		Title: title, Body: body,
		Footer: footer + strings.Repeat(" ", gap) + pos,
		Width:  overlayWidthForBody(textW), Palette: m.palette,
	}.Render()
}

func (m *frontmatterPresetMenu) overview(textW int) string {
	preset, ok := m.SelectedPreset()
	if !ok {
		return ""
	}

	p := m.palette
	width := max(textW-helpScrollbarGutter, 1)
	description := strings.Join(strings.Fields(preset.Description), " ")
	if description == "" {
		description = "(no description)"
	}

	boxContentWidth := max(width-4, 1)
	descriptionBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(p.FgDim).
		Padding(0, 1).
		Width(width).
		Render(ansi.Truncate(description, boxContentWidth, "…"))

	changes := presetChanges(preset)
	const maxVisibleChanges = 8
	visibleChanges := changes
	if len(visibleChanges) > maxVisibleChanges {
		visibleChanges = visibleChanges[:maxVisibleChanges]
	}

	table := presetChangeTable(visibleChanges, width, p)
	if extra := len(changes) - len(visibleChanges); extra > 0 {
		table = append(table, lipgloss.NewStyle().Foreground(p.FgDim).Render(
			ansi.Truncate(fmt.Sprintf("  … %d more change(s)", extra), width, "…"),
		))
	}

	title := lipgloss.NewStyle().Bold(true).Foreground(p.Primary).Render(
		ansi.Truncate("Preset: "+preset.Name, width, "…"),
	)
	return lipgloss.JoinVertical(lipgloss.Left, title, descriptionBox, strings.Join(table, "\n"))
}

type frontmatterPresetChange struct {
	operation string
	key       string
	value     string
}

func presetChanges(preset config.FrontmatterPreset) []frontmatterPresetChange {
	changes := make([]frontmatterPresetChange, 0, len(preset.Set)+len(preset.Unset))
	setKeys := make([]string, 0, len(preset.Set))
	for key := range preset.Set {
		setKeys = append(setKeys, key)
	}
	sort.Strings(setKeys)
	for _, key := range setKeys {
		changes = append(changes, frontmatterPresetChange{
			operation: "set",
			key:       key,
			value:     fmt.Sprintf("%v", preset.Set[key]),
		})
	}

	unsetKeys := append([]string(nil), preset.Unset...)
	sort.Strings(unsetKeys)
	for _, key := range unsetKeys {
		changes = append(changes, frontmatterPresetChange{operation: "unset", key: key, value: "—"})
	}
	return changes
}

func presetChangeTable(changes []frontmatterPresetChange, width int, p Palette) []string {
	actionWidth := 6
	keyWidth := len("key")
	for _, change := range changes {
		keyWidth = max(keyWidth, lipgloss.Width(change.key))
	}
	keyWidth = min(keyWidth, max(width-actionWidth-8, len("key")))
	valueWidth := max(width-2-actionWidth-1-keyWidth-1, 1)

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(p.FgBase)
	detailStyle := lipgloss.NewStyle().Foreground(p.FgSubtle)
	lines := []string{
		headerStyle.Render(ansi.Truncate(presetTableRow("action", "key", "value", actionWidth, keyWidth, valueWidth), width, "…")),
	}
	for _, change := range changes {
		row := ansi.Truncate(presetTableRow(change.operation, change.key, change.value, actionWidth, keyWidth, valueWidth), width, "…")
		rowStyle := detailStyle
		if change.operation == "unset" {
			rowStyle = lipgloss.NewStyle().Foreground(p.Danger)
		}
		lines = append(lines, rowStyle.Render(row))
	}
	if len(changes) == 0 {
		lines = append(lines, detailStyle.Render("  (no frontmatter changes)"))
	}
	return lines
}

func presetTableRow(action, key, value string, actionWidth, keyWidth, valueWidth int) string {
	return "  " + presetTableCell(action, actionWidth) + " " +
		presetTableCell(key, keyWidth) + " " + ansi.Truncate(value, valueWidth, "…")
}

func presetTableCell(value string, width int) string {
	value = ansi.Truncate(value, width, "…")
	return value + strings.Repeat(" ", max(width-lipgloss.Width(value), 0))
}

func (m *frontmatterPresetMenu) footerHints() string {
	if m.filtering {
		return RenderInlineHints([]Shortcut{{"type", "filter"}, {"↑/↓", "select"}, {"enter", "apply"}, {"esc", "clear"}})
	}
	return RenderInlineHints([]Shortcut{{"↑/↓", "select"}, {"/", "search"}, {"enter", "apply"}, {"esc/q", "close"}})
}

func (m *frontmatterPresetMenu) contentWidth(termWidth int) int {
	textW := lipgloss.Width(m.footerHints()) + 8
	for _, row := range m.sizingRows() {
		textW = max(textW, lipgloss.Width(m.renderRow(row, false))+helpScrollbarGutter)
		textW = max(textW, lipgloss.Width(m.renderRow(row, true))+helpScrollbarGutter)
	}
	if m.filtering {
		query := m.filter
		if query == "" {
			query = "type to filter…"
		}
		textW = max(textW, lipgloss.Width("> "+query))
	}
	if m.filtering && len(m.nav) == 0 {
		textW = max(textW, lipgloss.Width("  no matches")+helpScrollbarGutter)
	}
	if termWidth > 0 {
		textW = min(textW, max(termWidth-8, 1))
	}
	return max(textW, 1)
}

func (m *frontmatterPresetMenu) sizingRows() []frontmatterPresetMenuRow {
	rows := make([]frontmatterPresetMenuRow, 0, len(m.presets)+4)
	appendGroup := func(title string, presets []config.FrontmatterPreset) {
		rows = append(rows, frontmatterPresetMenuRow{header: true, title: title})
		if len(presets) == 0 {
			rows = append(rows, frontmatterPresetMenuRow{
				preset:   config.FrontmatterPreset{Name: "No presets", Description: "Nothing in this scope"},
				disabled: true,
			})
			return
		}
		for _, preset := range presets {
			rows = append(rows, frontmatterPresetMenuRow{preset: preset})
		}
	}
	appendGroup("Column presets", m.presetsForScope(true))
	appendGroup("Board presets", m.presetsForScope(false))
	return rows
}

func (m *frontmatterPresetMenu) renderRow(row frontmatterPresetMenuRow, selected bool) string {
	p := m.palette
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(p.Primary)
	nameStyle := lipgloss.NewStyle().Foreground(p.FgMuted)
	descStyle := lipgloss.NewStyle().Foreground(p.FgSubtle)
	selStyle := lipgloss.NewStyle().Bold(true).Foreground(p.FgInverse).Background(p.Primary)
	disabledStyle := lipgloss.NewStyle().Foreground(p.FgDim).Italic(true)
	hiStyle := lipgloss.NewStyle().Bold(true).Foreground(p.Highlight)
	hiSelStyle := lipgloss.NewStyle().Bold(true).Foreground(p.Highlight).Background(p.Primary)
	gutterSel := lipgloss.NewStyle().Foreground(p.Primary).Bold(true).Render("▌")
	if row.header {
		return headerStyle.Render("── " + row.title + " ──")
	}
	if row.disabled {
		label := row.preset.Name
		if row.preset.Description != "" {
			label += "  —  " + row.preset.Description
		}
		return "  " + disabledStyle.Render(label)
	}
	labelIdx, descIdx := splitLabelDescMatchIndexes(row.preset.Name, row.matchIdx)
	labelStyle, detailStyle := nameStyle, descStyle
	hiLabel, hiDesc := hiStyle, hiStyle
	gutter := " "
	if selected {
		labelStyle, detailStyle = selStyle, selStyle
		hiLabel, hiDesc = hiSelStyle, hiSelStyle
		gutter = gutterSel
	}
	label := renderHighlighted(row.preset.Name, labelIdx, labelStyle, hiLabel)
	if row.preset.Description != "" {
		sep := "  —  "
		if selected {
			label += selStyle.Render(sep)
		} else {
			label += descStyle.Render(sep)
		}
		label += renderHighlighted(row.preset.Description, descIdx, detailStyle, hiDesc)
	}
	if selected {
		label = selStyle.Render(" ") + label + selStyle.Render(" ")
	}
	return gutter + " " + label
}

type frontmatterPresetApplyMsg struct {
	Preset       config.FrontmatterPreset
	Column       columnRef
	Targets      []itemRefStable
	SelectedName string
}

type frontmatterPresetActions struct{ board *Board }

func (b *Board) frontmatterPresetActions() frontmatterPresetActions {
	return frontmatterPresetActions{board: b}
}

func (a frontmatterPresetActions) open(ctx itemActionContext) tea.Cmd {
	b := a.board
	if ctx.Column == nil || ctx.Column.Virtual {
		return b.notifier.Error("frontmatter presets require a filesystem column")
	}
	presets := make([]config.FrontmatterPreset, 0, len(b.cfg.FrontmatterPresets))
	for _, preset := range b.cfg.FrontmatterPresets {
		if preset.AppliesTo(ctx.Column.Name, ctx.ColIdx+1) {
			presets = append(presets, preset)
		}
	}
	if len(presets) == 0 {
		return b.notifier.Error("no frontmatter presets available for " + ctx.Column.Name)
	}
	selectedName := ""
	if ctx.Item != nil {
		selectedName = ctx.Item.Name
	}
	b.presetMenu.Open(ctx.ColIdx, refForColumn(ctx.Column), presets, ctx.Targets, selectedName)
	return nil
}

func (a frontmatterPresetActions) update(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	b := a.board
	if b.presetMenu.Filtering() {
		switch msg.Code {
		case tea.KeyEsc:
			b.presetMenu.StopFilter()
		case tea.KeyEnter:
			return a.applySelected()
		case tea.KeyBackspace:
			b.presetMenu.Backspace()
		default:
			if msg.Text != "" {
				b.presetMenu.AppendFilter(msg.Text)
			} else {
				b.presetMenu.Update(msg)
			}
		}
		return b, nil
	}

	switch {
	case key.Matches(msg, Keys.HelpClose) || msg.String() == "q":
		b.presetMenu.Close()
	case msg.String() == "/":
		b.presetMenu.StartFilter()
	case msg.Code == tea.KeyEnter:
		return a.applySelected()
	default:
		b.presetMenu.Update(msg)
	}
	return b, nil
}

func (a frontmatterPresetActions) applySelected() (tea.Model, tea.Cmd) {
	b := a.board
	preset, ok := b.presetMenu.SelectedPreset()
	if !ok {
		return b, nil
	}
	msg := frontmatterPresetApplyMsg{
		Preset:       preset,
		Column:       b.presetMenu.column,
		Targets:      append([]itemRefStable(nil), b.presetMenu.targets...),
		SelectedName: b.presetMenu.selectedName,
	}
	b.presetMenu.Close()
	return b, func() tea.Msg { return msg }
}

func (a frontmatterPresetActions) handleApply(msg frontmatterPresetApplyMsg) (tea.Model, tea.Cmd) {
	b := a.board
	now := time.Now()
	vars := presetVariables(b, now)
	succeeded := 0
	failed := 0
	var lastErr error
	touched := make(map[*Column]struct{})

	for _, ref := range msg.Targets {
		col, item, err := b.resolveDelayedItemRef(ref)
		if err != nil {
			failed++
			lastErr = err
			continue
		}
		if col.Virtual {
			failed++
			lastErr = errVirtualColumn
			continue
		}
		patch, err := buildPresetPatch(msg.Preset, col, item, vars, now)
		if err != nil {
			failed++
			lastErr = err
			continue
		}
		result, err := b.applyFrontmatterPatch(col, item.Name, patch)
		if err != nil {
			failed++
			lastErr = err
			continue
		}
		succeeded++
		touched[col] = struct{}{}
		if result.Changed {
			b.bus.Publish(events.ItemSaved{
				Item: events.ItemRef{Column: col.Name, Name: item.Name},
				Kind: "frontmatter_preset",
			})
		}
	}

	for col := range touched {
		b.reloadColumnAfterMutation(col)
		if col == nil || msg.SelectedName == "" {
			continue
		}
		col.SelectByName(msg.SelectedName)
	}

	if failed > 0 {
		message := fmt.Sprintf("applied %d, failed %d", succeeded, failed)
		if lastErr != nil {
			message += ": " + lastErr.Error()
		}
		return b, b.notifier.Error(message)
	}
	return b, b.notifier.Success(fmt.Sprintf("applied %q to %d item(s)", msg.Preset.Name, succeeded))
}

func presetVariables(b *Board, now time.Time) map[string]string {
	boardName := b.cfg.BoardName
	if boardName == "" {
		boardName = filepath.Base(b.cfg.Path)
	}
	username := ""
	if current, err := user.Current(); err == nil {
		username = current.Username
	}
	if username == "" {
		username = os.Getenv("USER")
	}
	return map[string]string{
		"now":   now.Format(time.RFC3339),
		"today": now.Format("2006-01-02"),
		"board": boardName,
		"user":  username,
	}
}

func buildPresetPatch(preset config.FrontmatterPreset, col *Column, item *Item, vars map[string]string, now time.Time) (frontmatter.Patch, error) {
	patch := frontmatter.Patch{Set: make(map[string]string, len(preset.Set)), Unset: append([]string(nil), preset.Unset...)}
	localVars := make(map[string]string, len(vars)+2)
	for key, value := range vars {
		localVars[key] = value
	}
	localVars["column"] = col.Name
	localVars["filename"] = item.Name
	for key, value := range preset.Set {
		expanded, err := expandPresetValue(value, localVars, now)
		if err != nil {
			return frontmatter.Patch{}, fmt.Errorf("set.%s: %w", key, err)
		}
		encoded, err := frontmatter.EncodeValue(expanded)
		if err != nil {
			return frontmatter.Patch{}, fmt.Errorf("set.%s: encode value: %w", key, err)
		}
		patch.Set[key] = encoded
	}
	return patch, nil
}

func expandPresetValue(value any, vars map[string]string, now time.Time) (any, error) {
	switch v := value.(type) {
	case string:
		return expandPresetString(v, vars, now)
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			expanded, err := expandPresetValue(item, vars, now)
			if err != nil {
				return nil, err
			}
			out[i] = expanded
		}
		return out, nil
	case []string:
		out := make([]string, len(v))
		for i, item := range v {
			expanded, err := expandPresetString(item, vars, now)
			if err != nil {
				return nil, err
			}
			out[i] = expanded
		}
		return out, nil
	default:
		return value, nil
	}
}

func expandPresetString(value string, vars map[string]string, now time.Time) (string, error) {
	for {
		start := strings.Index(value, "{{")
		if start < 0 {
			return value, nil
		}
		end := strings.Index(value[start+2:], "}}")
		if end < 0 {
			return "", fmt.Errorf("unterminated variable in %q", value)
		}
		end += start + 2
		name := strings.TrimSpace(value[start+2 : end])
		replacement, ok := vars[name]
		if !ok {
			expr, candidate, err := config.ParsePresetDateExpression(name)
			if !candidate {
				return "", fmt.Errorf("unknown variable {{%s}}", name)
			}
			if err != nil {
				return "", fmt.Errorf("invalid date expression {{%s}}: %w", name, err)
			}
			replacement, err = expr.Evaluate(now)
			if err != nil {
				return "", fmt.Errorf("evaluate date expression {{%s}}: %w", name, err)
			}
		}
		value = value[:start] + replacement + value[end+2:]
	}
}

func (b *Board) applyFrontmatterPatch(col *Column, name string, patch frontmatter.Patch) (boardops.MutationResult, error) {
	if col == nil || col.Virtual {
		return boardops.MutationResult{}, errVirtualColumn
	}
	return boardops.ApplyFrontmatterPatch(boardops.ColumnRef{Name: col.Name, Path: col.Path}, name, patch)
}
