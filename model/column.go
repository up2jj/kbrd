package model

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"kbrd/board"
	"kbrd/events"
	kbrdfs "kbrd/fs"
)

// itemDelegate renders each kanban item inside a bubbles list.
type itemDelegate struct {
	isActive     bool
	mnemonicOf   func(name string) string
	gutterW      int
	colWidth     int
	previewLines int // preview rows per card; <=1 means the compact default
	statFor      func(absPath string) (kbrdfs.DiffStat, bool)
	palette      Palette
}

// cardRows is the fixed height of one card slot for a given preview density:
// title + N preview rows + meta + 2 border rows. It is the single source of
// truth shared by delegate.Height() and the renderers, so the declared and
// drawn heights can never drift apart.
func cardRows(previewLines int) int {
	return max(previewLines, 1) + 4
}

func (d itemDelegate) Height() int                             { return cardRows(d.previewLines) }
func (d itemDelegate) Spacing() int                            { return 0 }
func (d itemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d itemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	item, ok := listItem.(Item)
	if !ok {
		return
	}
	isSelected := index == m.Index()

	if item.Separator {
		d.renderSeparator(w, item)
		return
	}
	if item.Virtual {
		d.renderVirtual(w, item, isSelected)
		return
	}

	gutterW := d.gutterW
	if gutterW < 2 {
		gutterW = 2
	}
	innerW := d.colWidth - 2
	if innerW < 1 {
		innerW = 1
	}
	mnemonic := ""
	if d.mnemonicOf != nil {
		mnemonic = d.mnemonicOf(item.Name)
	}

	// Row palette — every cell on the row shares the same background so the
	// mnemonic cell visually belongs to the row. The card border doubles as a
	// selection cue: accent when selected, muted otherwise.
	p := d.palette
	var rowBg, mnemFg, nameFg, cardBorder lipgloss.Color
	hasRowBg := false
	switch {
	case isSelected && d.isActive:
		rowBg = p.PrimaryStrong
		mnemFg = p.Highlight
		nameFg = p.FgOnAccent
		cardBorder = p.PrimaryStrong
		hasRowBg = true
	case isSelected:
		mnemFg = p.Warning
		nameFg = p.FgEmphasis
		cardBorder = p.BorderActive
	default:
		mnemFg = p.Warning
		nameFg = p.FgEmphasis
		cardBorder = p.BorderMuted
	}
	// Frontmatter accent tints the title, but never overrides the selected
	// row's on-accent foreground (mirrors renderVirtual).
	if item.Accent != "" && !hasRowBg {
		nameFg = lipgloss.Color(item.Accent)
	}

	pinIcon := ""
	if item.Pinned {
		pinIcon = "📌 "
	}

	// Build line 1 as gutter + rest, each rendered with the same row background
	// so they fuse into one continuous bar.
	gutterStyle := lipgloss.NewStyle().Bold(true).Foreground(mnemFg).Width(gutterW)
	restWidth := innerW - gutterW
	if restWidth < 1 {
		restWidth = 1
	}
	restStyle := lipgloss.NewStyle().Bold(true).Foreground(nameFg).Width(restWidth).MaxWidth(restWidth)
	if hasRowBg {
		gutterStyle = gutterStyle.Background(rowBg)
		restStyle = restStyle.Background(rowBg)
	}

	gutterText := mnemonic
	if gutterText == "" {
		if isSelected && d.isActive {
			gutterText = ">"
		}
	}
	title := pinIcon + item.Title
	if item.Icon != "" {
		title = item.Icon + " " + title
	}
	titleLine := gutterStyle.Render(gutterText) + restStyle.Render(truncLine(title, restWidth))

	// Preview block — N rows depending on the layout's density.
	var previewFg, detailBg lipgloss.Color
	switch {
	case isSelected && d.isActive:
		previewFg = p.FgSelectedPreview
		detailBg = p.BgSelectedDetail
	case isSelected:
		previewFg = p.FgMuted
	default:
		previewFg = p.FgSubtle
	}
	previewStyle := lipgloss.NewStyle().Width(innerW).MaxWidth(innerW).PaddingLeft(gutterW).Foreground(previewFg).Italic(true)
	if isSelected && d.isActive {
		previewStyle = previewStyle.Background(detailBg)
	}
	previewLine := previewBlock(item.Preview, d.previewLines, innerW, gutterW, previewStyle)

	// Line 3 — meta: a frontmatter `meta` replaces the computed modified +
	// size + git diff block, matching virtual-item semantics.
	meta := item.Meta
	if meta == "" {
		meta = timeAgo(item.Modified) + "  ·  " + item.HumanSize()
		if d.statFor != nil {
			if s, ok := d.statFor(item.FullPath); ok {
				switch {
				case s.Moved:
					movedStyle := lipgloss.NewStyle().Foreground(p.AccentAlt).Bold(true)
					meta += "  ·  " + movedStyle.Render("→ moved")
				case s.New:
					newStyle := lipgloss.NewStyle().Foreground(p.Success).Bold(true)
					meta += "  ·  " + newStyle.Render("✚ new")
				case s.Added > 0 || s.Deleted > 0:
					addedStyle := lipgloss.NewStyle().Foreground(p.Success)
					deletedStyle := lipgloss.NewStyle().Foreground(p.Danger)
					meta += "  ·  " + addedStyle.Render(fmt.Sprintf("+%d", s.Added)) + deletedStyle.Render(fmt.Sprintf("-%d", s.Deleted))
				}
			}
		}
	}
	if len(item.Tags) > 0 {
		tagStyle := lipgloss.NewStyle().Foreground(p.AccentSoft)
		chips := make([]string, len(item.Tags))
		for i, tag := range item.Tags {
			chips[i] = tagStyle.Render("#" + tag)
		}
		meta += "  " + strings.Join(chips, " ")
	}
	if item.BadFM {
		badStyle := lipgloss.NewStyle().Foreground(p.Warning).Bold(true)
		meta += "  ·  " + badStyle.Render("⚠ yaml")
	}
	var metaFg lipgloss.Color
	switch {
	case isSelected && d.isActive:
		metaFg = p.AccentSoft
	case isSelected:
		metaFg = p.FgSubtle
	default:
		metaFg = p.BorderMuted
	}
	metaStyle := lipgloss.NewStyle().Width(innerW).MaxWidth(innerW).PaddingLeft(gutterW).Foreground(metaFg)
	if isSelected && d.isActive {
		metaStyle = metaStyle.Background(detailBg)
	}
	metaLine := metaStyle.Render(truncLine(meta, innerW-gutterW))

	fmt.Fprint(w, renderCard(titleLine, previewLine, metaLine, innerW, cardBorder))
}

// truncLine clamps s (ANSI-aware) to a single line of at most w cells, ending
// with an ellipsis when cut. Cards have a fixed height, so content must never
// wrap — a line wider than its style's Width would wrap and grow the card.
func truncLine(s string, w int) string {
	if w < 1 {
		w = 1
	}
	return ansi.Truncate(s, w, "…")
}

// previewBlock renders exactly max(n,1) preview rows from lines, padding with
// blank rows so the card height always matches cardRows(n). When there is no
// preview at all, the first row shows the "—" placeholder.
func previewBlock(lines []string, n, innerW, gutterW int, style lipgloss.Style) string {
	n = max(n, 1)
	rows := make([]string, 0, n)
	for i := range n {
		text := ""
		switch {
		case i < len(lines):
			text = lines[i]
		case i == 0:
			text = "—"
		}
		rows = append(rows, style.Render(truncLine(text, innerW-gutterW)))
	}
	return strings.Join(rows, "\n")
}

// renderCard wraps the content lines of an item in a rounded border so it
// reads as a kanban card. The border consumes 2 columns, bringing the total
// width back to colWidth, and 2 rows (delegate Height is cardRows).
func renderCard(titleLine, previewLine, metaLine string, innerW int, borderFg lipgloss.Color) string {
	block := lipgloss.JoinVertical(lipgloss.Left, titleLine, previewLine, metaLine)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderFg).
		Width(innerW).
		Render(block)
}

// renderSeparator draws an inert grouping row: a centered label flanked by rule
// glyphs, vertically padded to fill the fixed cardRows item slot. Separators
// stay borderless — they are grouping rules, not cards.
func (d itemDelegate) renderSeparator(w io.Writer, item Item) {
	p := d.palette
	fg := p.FgMuted
	if item.Accent != "" {
		fg = lipgloss.Color(item.Accent)
	}
	label := strings.TrimSpace(item.Title)
	var line string
	if label != "" {
		label = " " + strings.ToUpper(label) + " "
	}
	dashes := d.colWidth - lipgloss.Width(label) - 2
	if dashes < 0 {
		dashes = 0
	}
	left := dashes / 2
	right := dashes - left
	line = strings.Repeat("─", left) + label + strings.Repeat("─", right)
	style := lipgloss.NewStyle().Width(d.colWidth).MaxWidth(d.colWidth).Foreground(fg)
	blank := lipgloss.NewStyle().Width(d.colWidth).Render("")

	total := cardRows(d.previewLines)
	ruleRow := total / 2
	rows := make([]string, 0, total)
	for i := range total {
		if i == ruleRow {
			rows = append(rows, style.Render(line))
		} else {
			rows = append(rows, blank)
		}
	}
	fmt.Fprint(w, strings.Join(rows, "\n"))
}

// renderVirtual draws a script-supplied item in the fixed card frame: icon +
// title (line 1, tinted by Accent), preview (line 2), and the script-provided
// Meta string (line 3) in place of the filesystem-only mtime/size/diff line.
func (d itemDelegate) renderVirtual(w io.Writer, item Item, isSelected bool) {
	p := d.palette
	gutterW := d.gutterW
	if gutterW < 2 {
		gutterW = 2
	}
	innerW := d.colWidth - 2
	if innerW < 1 {
		innerW = 1
	}
	mnemonic := ""
	if d.mnemonicOf != nil {
		mnemonic = d.mnemonicOf(item.Name)
	}

	var rowBg, mnemFg, nameFg, cardBorder lipgloss.Color
	hasRowBg := false
	switch {
	case isSelected && d.isActive:
		rowBg = p.PrimaryStrong
		mnemFg = p.Highlight
		nameFg = p.FgOnAccent
		cardBorder = p.PrimaryStrong
		hasRowBg = true
	case isSelected:
		mnemFg = p.Warning
		nameFg = p.FgEmphasis
		cardBorder = p.BorderActive
	default:
		mnemFg = p.Warning
		nameFg = p.FgEmphasis
		cardBorder = p.BorderMuted
	}
	if item.Accent != "" && !hasRowBg {
		nameFg = lipgloss.Color(item.Accent)
	}

	gutterStyle := lipgloss.NewStyle().Bold(true).Foreground(mnemFg).Width(gutterW)
	restWidth := innerW - gutterW
	if restWidth < 1 {
		restWidth = 1
	}
	restStyle := lipgloss.NewStyle().Bold(true).Foreground(nameFg).Width(restWidth).MaxWidth(restWidth)
	if hasRowBg {
		gutterStyle = gutterStyle.Background(rowBg)
		restStyle = restStyle.Background(rowBg)
	}
	gutterText := mnemonic
	if gutterText == "" && isSelected && d.isActive {
		gutterText = ">"
	}
	title := item.Title
	if item.Icon != "" {
		title = item.Icon + " " + title
	}
	titleLine := gutterStyle.Render(gutterText) + restStyle.Render(truncLine(title, restWidth))

	var previewFg, detailBg lipgloss.Color
	switch {
	case isSelected && d.isActive:
		previewFg = p.FgSelectedPreview
		detailBg = p.BgSelectedDetail
	default:
		previewFg = p.FgSubtle
	}
	previewStyle := lipgloss.NewStyle().Width(innerW).MaxWidth(innerW).PaddingLeft(gutterW).Foreground(previewFg).Italic(true)
	if isSelected && d.isActive {
		previewStyle = previewStyle.Background(detailBg)
	}
	previewLine := previewBlock(item.Preview, d.previewLines, innerW, gutterW, previewStyle)

	metaFg := p.BorderMuted
	if isSelected && d.isActive {
		metaFg = p.AccentSoft
	}
	metaStyle := lipgloss.NewStyle().Width(innerW).MaxWidth(innerW).PaddingLeft(gutterW).Foreground(metaFg)
	if isSelected && d.isActive {
		metaStyle = metaStyle.Background(detailBg)
	}
	metaLine := metaStyle.Render(truncLine(item.Meta, innerW-gutterW))

	fmt.Fprint(w, renderCard(titleLine, previewLine, metaLine, innerW, cardBorder))
}

// Column represents one kanban column backed by a directory.
type Column struct {
	Name        string
	Path        string
	Items       []Item // unfiltered master list (used by file operations)
	list        list.Model
	itemOpts    ItemOptions
	listYOffset int
	palette     Palette

	// Virtual-column state. A virtual column has no filesystem backing: its
	// Items are pushed by a script via kbrd.column.set, file moves into/out of
	// it are rejected, and its actions come from colCmds rather than the
	// built-in mutation keys. Zero for ordinary filesystem columns.
	Virtual    bool
	VID        string       // Lua-facing id (stable key for set/clear)
	colCmds    []VirtualCmd // column-scoped commands (B)
	defaultCmd string       // id of the Enter/default command (optional)
	emptyText  string       // placeholder shown when there are no items
}

// VirtualCmd is a column-scoped command surfaced in the X menu / status hints
// for a virtual column. Ref is the host dispatch handle.
type VirtualCmd struct {
	ID      string
	Name    string
	Key     string
	Default bool
	Ref     string
}

// NewColumn builds a column over a directory. Widths are not stored: layout
// owns geometry, and every render passes the column its width via RenderCtx.
func NewColumn(name, path string, itemOpts ItemOptions) *Column {
	palette := DarkPalette()
	delegate := itemDelegate{palette: palette}
	l := list.New(nil, delegate, 0, 20)
	l.SetShowTitle(false)
	l.SetShowFilter(false)
	l.SetShowStatusBar(false)
	l.SetShowPagination(false)
	l.SetShowHelp(false)
	l.SetFilteringEnabled(true)
	l.DisableQuitKeybindings()
	l.Styles.NoItems = lipgloss.NewStyle().
		PaddingLeft(2).
		Foreground(palette.FgDim)

	return &Column{Name: name, Path: path, list: l, itemOpts: itemOpts, palette: palette}
}

// NewVirtualColumn builds an empty script-backed column. It reuses NewColumn's
// list setup, then flips the virtual flag and clears the filesystem Path.
func NewVirtualColumn(vid, name string, palette Palette) *Column {
	c := NewColumn(name, "", ItemOptions{})
	c.Virtual = true
	c.VID = vid
	c.palette = palette
	c.list.SetDelegate(itemDelegate{palette: palette})
	return c
}

// ApplyVirtualSpec replaces the column's items and column-scoped commands from a
// script push (kbrd.column.set). Items are shown in the order given (no
// SortItems). The cursor is preserved by item id when the selected item still
// exists, else clamped to its old index.
func (c *Column) ApplyVirtualSpec(spec events.VirtualColumnSpec) {
	if spec.Name != "" {
		c.Name = spec.Name
	}
	c.emptyText = spec.Empty

	c.colCmds = c.colCmds[:0]
	c.defaultCmd = ""
	for _, vc := range spec.Commands {
		c.colCmds = append(c.colCmds, VirtualCmd{
			ID: vc.ID, Name: vc.Name, Key: vc.Key, Default: vc.Default, Ref: vc.Ref,
		})
		if vc.Default && c.defaultCmd == "" {
			c.defaultCmd = vc.ID
		}
	}

	prevName, prevIdx := "", c.list.Index()
	if sel := c.SelectedItem(); sel != nil {
		prevName = sel.Name
	}

	items := make([]Item, 0, len(spec.Items))
	for _, vi := range spec.Items {
		items = append(items, virtualItemToItem(vi))
	}
	c.Items = items // virtual columns control their own order

	listItems := make([]list.Item, len(items))
	for i, it := range items {
		listItems[i] = it
	}
	c.list.SetItems(listItems)
	c.restoreVirtualCursor(prevName, prevIdx)
}

// restoreVirtualCursor re-selects the item named prevName after a re-push; if it
// is gone, it clamps to the previous index position (bounded by the new length).
func (c *Column) restoreVirtualCursor(prevName string, prevIdx int) {
	if prevName != "" {
		for i, it := range c.Items {
			if it.Name == prevName {
				c.list.Select(i)
				return
			}
		}
	}
	if n := len(c.Items); n > 0 {
		if prevIdx >= n {
			prevIdx = n - 1
		}
		if prevIdx < 0 {
			prevIdx = 0
		}
		c.list.Select(prevIdx)
	}
}

// virtualItemToItem converts a script-pushed VirtualItem into a model Item. The
// id (else title) becomes Name so the existing name-keyed selection works.
func virtualItemToItem(vi events.VirtualItem) Item {
	name := vi.ID
	if name == "" {
		name = vi.Title
	}
	var preview []string
	if vi.Preview != "" {
		preview = []string{vi.Preview}
	}
	return Item{
		Name:      name,
		Title:     vi.Title,
		Preview:   preview,
		FullPath:  vi.Path,
		Virtual:   true,
		Separator: vi.Separator,
		Meta:      vi.Meta,
		Icon:      vi.Icon,
		Accent:    vi.Accent,
		Data:      vi.Data,
	}
}

func (c *Column) SetHeight(h int) {
	c.list.SetHeight(h)
}

func (c *Column) UpdateList(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	c.list, cmd = c.list.Update(msg)
	return cmd
}

func (c *Column) renderHeader(isActive bool, leftPad, width int) string {
	p := c.palette
	var nameFg, countFg, sepColor lipgloss.Color
	if isActive {
		nameFg = p.FgStrong
		countFg = p.Primary
		sepColor = p.PrimaryStrong
	} else {
		nameFg = p.FgMuted
		countFg = p.FgDim
		sepColor = p.BorderMuted
	}

	nameLabel := strings.ToUpper(c.Name)
	if c.Virtual {
		// ◇ is the primary "virtual" marker; tint the name with a soft accent in
		// both states so it stays a gentle hint, not a selected-looking highlight.
		nameLabel = "◇ " + nameLabel
		nameFg = c.palette.AccentSoft
	}
	countLabel := strconv.Itoa(c.TotalCount())
	filtered := c.list.IsFiltered() && !c.list.SettingFilter()
	if filtered {
		countLabel = strconv.Itoa(len(c.list.VisibleItems())) + "/" + strconv.Itoa(c.TotalCount())
	}

	indicator := "  "
	if isActive {
		indicator = lipgloss.NewStyle().Foreground(sepColor).Bold(true).Render("▍ ")
	}
	if filtered {
		indicator = lipgloss.NewStyle().Foreground(countFg).Render("⌕ ")
	}

	leftPadStr := ""
	if leftPad > 0 {
		leftPadStr = strings.Repeat(" ", leftPad)
	}

	name := lipgloss.NewStyle().Bold(true).Foreground(nameFg).Render(nameLabel)
	count := lipgloss.NewStyle().Foreground(countFg).Render(countLabel)

	used := lipgloss.Width(leftPadStr) + lipgloss.Width(indicator) + lipgloss.Width(name) + lipgloss.Width(count)
	gap := width - used
	if gap < 1 {
		gap = 1
	}
	spacer := strings.Repeat(" ", gap)

	header := leftPadStr + indicator + name + spacer + count
	sep := lipgloss.NewStyle().Foreground(sepColor).Render(strings.Repeat("─", width))

	return lipgloss.JoinVertical(lipgloss.Left, header, sep)
}

// RenderCtx is the render environment Board hands a column for one frame:
// focus, geometry (from layout), and the board-level lookups cards need.
type RenderCtx struct {
	Active       bool
	Width        int // content width allotted by layout (excludes border)
	PreviewLines int // preview rows per card (Slot.PreviewLines)
	GutterW      int // mnemonic gutter width
	MnemonicOf   func(name string) string
	StatFor      func(absPath string) (kbrdfs.DiffStat, bool)
}

func (c *Column) View(ctx RenderCtx) string {
	c.list.SetWidth(ctx.Width)
	c.list.SetDelegate(itemDelegate{
		isActive:     ctx.Active,
		mnemonicOf:   ctx.MnemonicOf,
		gutterW:      ctx.GutterW,
		colWidth:     ctx.Width,
		previewLines: ctx.PreviewLines,
		statFor:      ctx.StatFor,
		palette:      c.palette,
	})
	c.list.SetShowFilter(c.list.SettingFilter() || c.list.IsFiltered())
	c.list.Styles.NoItems = lipgloss.NewStyle().PaddingLeft(2).Foreground(c.palette.FgDim)

	// Virtual columns signal "virtual" via the double-border shape (and the ◇
	// header glyph), not a high-contrast color — so the border color follows the
	// same focused/unfocused scheme as ordinary columns and an unfocused virtual
	// column doesn't read as selected.
	border := lipgloss.RoundedBorder()
	if c.Virtual {
		border = lipgloss.DoubleBorder()
	}
	borderColor := c.palette.BorderMuted
	if ctx.Active {
		borderColor = c.palette.BorderActive
	}

	leftPad := ctx.GutterW - 2
	if leftPad < 0 {
		leftPad = 0
	}
	header := c.renderHeader(ctx.Active, leftPad, ctx.Width)
	listView := c.list.View()
	if c.Virtual && len(c.Items) == 0 && c.emptyText != "" {
		listView = lipgloss.NewStyle().
			PaddingLeft(2).
			Width(ctx.Width).
			Foreground(c.palette.FgDim).
			Render(c.emptyText)
	}
	c.listYOffset = 1 + lipgloss.Height(header)
	if c.list.ShowFilter() {
		c.listYOffset += lipgloss.Height(listView) - c.list.Height()
	}
	content := lipgloss.JoinVertical(lipgloss.Left,
		header,
		listView,
		c.renderOverflowFooter(ctx.Width),
	)

	return lipgloss.NewStyle().
		Border(border).
		BorderForeground(borderColor).
		Render(content)
}

// renderOverflowFooter shows "▲ N above" / "▼ N below" chips when the current
// page of items doesn't cover the full visible-items list. Always returns a
// single-line string (possibly blank) so column heights stay stable.
func (c *Column) renderOverflowFooter(width int) string {
	style := lipgloss.NewStyle().
		Width(width).
		MaxWidth(width).
		Foreground(c.palette.FgSubtle).
		Italic(true).
		PaddingLeft(2)

	total := len(c.list.VisibleItems())
	start, end := c.list.Paginator.GetSliceBounds(total)
	above, below := start, total-end
	if above <= 0 && below <= 0 {
		return style.Render("")
	}

	parts := make([]string, 0, 3)
	if above > 0 {
		parts = append(parts, fmt.Sprintf("▲ %d above", above))
	}
	if above > 0 && below > 0 {
		parts = append(parts, "·")
	}
	if below > 0 {
		parts = append(parts, fmt.Sprintf("▼ %d below", below))
	}
	return style.Render(strings.Join(parts, " "))
}

func (c *Column) IsFiltering() bool {
	return c.list.SettingFilter()
}

// HitTest maps a y-coordinate (relative to the top of this column's box) to a
// visible item index. Returns ok=false when the click lands outside any item
// (border, header, gap, filter bar, overflow footer, or past the last item).
func (c *Column) HitTest(yInBox int) (int, bool) {
	listY := yInBox - c.listYOffset
	if listY < 0 {
		return 0, false
	}
	d := itemDelegate{}
	slotH := d.Height() + d.Spacing()
	viewportIdx := listY / slotH
	if listY%slotH >= d.Height() {
		return 0, false
	}
	visible := c.list.VisibleItems()
	start, _ := c.list.Paginator.GetSliceBounds(len(visible))
	actualIdx := start + viewportIdx
	if actualIdx < 0 || actualIdx >= len(visible) {
		return 0, false
	}
	return actualIdx, true
}

func (c *Column) SelectIndex(i int) {
	c.list.Select(i)
}

// SelectByName selects the item with the given name, if present.
func (c *Column) SelectByName(name string) {
	for i, item := range c.Items {
		if item.Name == name {
			c.list.Select(i)
			return
		}
	}
}

func (c *Column) LoadItems() error {
	return c.loadItems(c.itemsByPath())
}

// itemsByPath snapshots the column's current items into a reload cache keyed by
// FullPath.
func (c *Column) itemsByPath() itemCache {
	cache := make(itemCache, len(c.Items))
	for _, it := range c.Items {
		cache[it.FullPath] = it
	}
	return cache
}

// loadItems rebuilds the column from disk, reusing any unchanged item from the
// cache so its file is never re-read. cache may be nil for a cold load.
func (c *Column) loadItems(cache itemCache) error {
	names, err := board.Items(c.Path)
	if err != nil {
		return err
	}

	items := []Item{}
	for _, name := range names {
		fullPath := filepath.Join(c.Path, name+".md")
		info, err := os.Stat(fullPath)
		if err != nil {
			continue
		}
		if old, ok := cache.reuse(fullPath, info); ok {
			items = append(items, old)
			continue
		}
		if item, err := NewItem(fullPath, c.itemOpts); err == nil {
			items = append(items, item)
		}
	}

	c.Items = SortItems(items)

	listItems := make([]list.Item, len(c.Items))
	for i, item := range c.Items {
		listItems[i] = item
	}
	c.list.SetItems(listItems)
	return nil
}

func (c *Column) TotalCount() int {
	return len(c.Items)
}

// VisibleItems returns the items currently rendered (post filter+sort), in
// display order.
func (c *Column) VisibleItems() []Item {
	li := c.list.VisibleItems()
	out := make([]Item, 0, len(li))
	for _, it := range li {
		if item, ok := it.(Item); ok {
			out = append(out, item)
		}
	}
	return out
}

func (c *Column) HasSelectedItem() bool {
	return len(c.Items) > 0 && c.list.SelectedItem() != nil
}

func (c *Column) SelectedItem() *Item {
	li := c.list.SelectedItem()
	if li == nil {
		return nil
	}
	item := li.(Item)
	return &item
}

func (c *Column) MoveItemTo(destCol *Column, itemName string) error {
	srcPath := ""
	for _, item := range c.Items {
		if item.Name == itemName {
			srcPath = item.FullPath
			break
		}
	}
	if srcPath == "" {
		return os.ErrNotExist
	}

	destPath := filepath.Join(destCol.Path, filepath.Base(srcPath))
	if _, err := os.Stat(destPath); err == nil {
		return os.ErrExist
	}
	if err := os.Rename(srcPath, destPath); err != nil {
		return err
	}

	c.LoadItems()
	destCol.LoadItems()
	return nil
}

func (c *Column) DeleteItem(itemName string) error {
	for _, item := range c.Items {
		if item.Name == itemName {
			return os.Remove(item.FullPath)
		}
	}
	return os.ErrNotExist
}

// CreateItem creates a new empty <name>.md item in the column. It will not
// overwrite an existing item (board.CreateItem uses O_EXCL). Returns the new
// item's filename.
func (c *Column) CreateItem(name string) (string, error) {
	return c.CreateItemContent(name, "")
}

// CreateItemContent is CreateItem with initial file content (e.g. a rendered
// template body).
func (c *Column) CreateItemContent(name, content string) (string, error) {
	fullPath, err := board.CreateItem(c.Path, name, content)
	if err != nil {
		return "", err
	}
	c.LoadItems()
	return filepath.Base(fullPath), nil
}

func (c *Column) AppendText(itemName, text string) error {
	fullPath := c.fullPathFor(itemName)
	if fullPath == "" {
		return os.ErrNotExist
	}
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return err
	}
	if len(content) > 0 && content[len(content)-1] != '\n' {
		text = "\n" + text
	}
	return os.WriteFile(fullPath, append(content, []byte(text+"\n")...), 0644)
}

func (c *Column) PrependText(itemName, text string) error {
	fullPath := c.fullPathFor(itemName)
	if fullPath == "" {
		return os.ErrNotExist
	}
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return err
	}
	return os.WriteFile(fullPath, append([]byte(text+"\n"), content...), 0644)
}

func (c *Column) ReplaceFile(itemName, text string) error {
	fullPath := c.fullPathFor(itemName)
	if fullPath == "" {
		return os.ErrNotExist
	}
	if len(text) > 0 && text[len(text)-1] != '\n' {
		text += "\n"
	}
	return os.WriteFile(fullPath, []byte(text), 0644)
}

func (c *Column) JournalText(itemName, text string) error {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	return c.AppendText(itemName, timestamp+" - "+text)
}

func (c *Column) CopyContent(itemName string) ([]byte, error) {
	fullPath := c.fullPathFor(itemName)
	if fullPath == "" {
		return nil, os.ErrNotExist
	}
	return os.ReadFile(fullPath)
}

func (c *Column) OpenFile(itemName string) error {
	fullPath := c.fullPathFor(itemName)
	if fullPath == "" {
		return os.ErrNotExist
	}
	return openFile(fullPath)
}

func (c *Column) RenameItem(oldName, newName string) error {
	for i := range c.Items {
		if c.Items[i].Name == oldName {
			newPath := filepath.Join(c.Path, newName+".md")
			if _, err := os.Stat(newPath); err == nil {
				return os.ErrExist
			}
			if err := os.Rename(c.Items[i].FullPath, newPath); err != nil {
				return err
			}
			return c.LoadItems()
		}
	}
	return os.ErrNotExist
}

func (c *Column) Rename(newName string) error {
	parent := filepath.Dir(c.Path)
	newPath := filepath.Join(parent, newName)
	if _, err := os.Stat(newPath); err == nil {
		return os.ErrExist
	}
	if err := os.Rename(c.Path, newPath); err != nil {
		return err
	}
	c.Name = newName
	c.Path = newPath
	return c.LoadItems()
}

func (c *Column) PinItem(itemName string) error {
	for i := range c.Items {
		if c.Items[i].Name == itemName {
			c.Items[i].TogglePin()
			newName := c.Items[i].Name
			newPath := filepath.Join(c.Path, newName+".md")
			if err := os.Rename(c.Items[i].FullPath, newPath); err != nil {
				return err
			}
			return c.LoadItems()
		}
	}
	return os.ErrNotExist
}

func (c *Column) fullPathFor(itemName string) string {
	for _, item := range c.Items {
		if item.Name == itemName {
			return item.FullPath
		}
	}
	return ""
}
