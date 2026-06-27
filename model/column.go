package model

import (
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"kbrd/board"
	"kbrd/boardops"
	"kbrd/colstore"
	"kbrd/events"
	"kbrd/frontmatter"
	kbrdfs "kbrd/fs"
	"kbrd/vlist"
)

// renderConfig is the per-frame render context a column hands its cards. It was
// previously the fields of the bubbles-list itemDelegate; it now travels in a
// cardDelegate (see vlist.Delegate) so the list engine stays card-agnostic.
type renderConfig struct {
	isActive      bool
	mnemonicOf    func(name string) string
	gutterW       int
	colWidth      int
	previewLines  int  // preview rows per card; <=1 means the compact default
	wrapTitles    bool // word-wrap titles across rows instead of truncating
	titleMaxLines int  // cap on wrapped title rows (<=1 disables wrapping)
	statFor       func(absPath string) (kbrdfs.DiffStat, bool)
	palette       Palette
}

// cardDelegate adapts a column's items to vlist.Delegate: the list addresses
// items by index, reads their height/filter/selectable, and asks this delegate
// to render them. It is rebuilt cheaply each frame to carry fresh renderConfig.
type cardDelegate struct {
	items []Item
	cfg   renderConfig
}

func (d cardDelegate) Len() int                 { return len(d.items) }
func (d cardDelegate) Height(i int) int         { return itemHeight(d.items[i], d.cfg) }
func (d cardDelegate) FilterValue(i int) string { return d.items[i].FilterValue() }
func (d cardDelegate) Selectable(i int) bool    { return !d.items[i].Separator }
func (d cardDelegate) Render(i int, selected bool) string {
	return renderItem(d.items[i], selected, d.cfg)
}

// cardRows is the base height of one card slot for a given preview density:
// title + N preview rows + meta + 2 border rows. itemHeight adds the optional
// frontmatter `render:` row on top.
func cardRows(previewLines int) int {
	return max(previewLines, 1) + 4
}

// itemHeight is the rendered height of one item. A card that declares a
// frontmatter `render:` list is one row taller (the fields line), and a wrapped
// title adds one row per extra title line; these are the variable heights vlist
// stacks. It must match what renderItem / renderVirtualStr draw — both derive
// the title row count from wrapTitle, so they cannot disagree. Separators keep
// their single-line rule layout.
func itemHeight(item Item, cfg renderConfig) int {
	h := cardRows(cfg.previewLines)
	if item.Separator {
		return h
	}
	extraTitleRows := len(wrapTitle(composeTitle(item), titleWidth(cfg), cfg.titleMaxLines, cfg.wrapTitles)) - 1
	h += extraTitleRows
	if len(item.Render) > 0 {
		h++
	}
	return h
}

// renderItem draws one item to a string, dispatching by kind. selected marks the
// cursor row; cfg carries the per-frame render context.
func renderItem(item Item, selected bool, cfg renderConfig) string {
	if item.Separator {
		return renderSeparatorStr(item, cfg)
	}
	if item.Virtual {
		return renderVirtualStr(item, selected, cfg)
	}

	isSelected := selected
	d := cfg
	gutterW := max(d.gutterW, 2)
	innerW := max(d.colWidth-2, 1)
	mnemonic := ""
	if d.mnemonicOf != nil {
		mnemonic = d.mnemonicOf(item.Name)
	}

	// Row palette — every cell on the row shares the same background so the
	// mnemonic cell visually belongs to the row. The card border doubles as a
	// selection cue: accent when selected, muted otherwise.
	p := d.palette
	var rowBg, mnemFg, nameFg, cardBorder color.Color
	hasRowBg := false
	switch {
	case isSelected && d.isActive:
		rowBg = p.PrimaryStrong
		mnemFg = p.Highlight
		nameFg = p.FgOnAccent
		cardBorder = p.PrimaryStrong
		hasRowBg = true
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

	// Build the title row(s) as gutter + rest, each rendered with the same row
	// background so they fuse into one continuous bar. A wrapped title spans
	// several rest rows; only the first carries the gutter (mnemonic / cursor).
	gutterStyle := lipgloss.NewStyle().Bold(true).Foreground(mnemFg).Width(gutterW)
	restWidth := max(innerW-gutterW, 1)
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
	titleRows := wrapTitle(composeTitle(item), restWidth, d.titleMaxLines, d.wrapTitles)
	titleLines := make([]string, len(titleRows))
	for i, row := range titleRows {
		gt := ""
		if i == 0 {
			gt = gutterText
		}
		titleLines[i] = gutterStyle.Render(gt) + restStyle.Render(row)
	}

	// Preview block — N rows depending on the layout's density.
	var previewFg, detailBg color.Color
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
	var metaFg color.Color
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

	// A frontmatter `render:` list adds one row above meta — the variable height.
	// The row is reserved whenever render is declared (even if no key resolves),
	// matching itemHeight so the drawn and declared heights agree.
	lines := append(titleLines, previewLine)
	if len(item.Render) > 0 {
		lines = append(lines, fieldsRow(item, isSelected && d.isActive, innerW, gutterW, detailBg, p))
	}
	lines = append(lines, metaLine)
	return renderCard(innerW, cardBorder, lines...)
}

// fieldsRow renders the frontmatter `render:` line ("key: value  ·  …") styled
// to sit in the card body, mirroring the preview/meta selected treatment.
func fieldsRow(item Item, selectedActive bool, innerW, gutterW int, detailBg color.Color, p Palette) string {
	style := lipgloss.NewStyle().Width(innerW).MaxWidth(innerW).PaddingLeft(gutterW).Foreground(p.FgEmphasis)
	if selectedActive {
		style = style.Foreground(p.FgSelectedPreview).Background(detailBg)
	}
	return style.Render(truncLine(fieldsLine(item.Data, item.Render), innerW-gutterW))
}

// fieldsLine formats the named keys present in data as "key: value" segments
// joined by "  ·  ". Missing or nil keys are skipped; scalars use fmt.Sprint and
// a []any is joined with ", ". Returns "" when nothing resolves. Formatting lives
// here at the render site, never in package frontmatter.
func fieldsLine(data map[string]any, keys []string) string {
	if len(data) == 0 || len(keys) == 0 {
		return ""
	}
	segs := make([]string, 0, len(keys))
	for _, k := range keys {
		v, ok := data[k]
		if !ok || v == nil {
			continue
		}
		segs = append(segs, k+": "+formatFieldValue(v))
	}
	return strings.Join(segs, "  ·  ")
}

func formatFieldValue(v any) string {
	if list, ok := v.([]any); ok {
		parts := make([]string, len(list))
		for i, e := range list {
			parts[i] = fmt.Sprint(e)
		}
		return strings.Join(parts, ", ")
	}
	return fmt.Sprint(v)
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

// composeTitle builds the full title string drawn on a card's first line(s):
// the pin icon, optional frontmatter icon, then the title text. itemHeight and
// renderItem both call it so the height they compute and draw stay identical.
func composeTitle(item Item) string {
	title := item.Title
	if item.Pinned {
		title = "📌 " + title
	}
	if item.Icon != "" {
		title = item.Icon + " " + title
	}
	return title
}

// titleWidth is the cell budget the title text has on one row: the inner card
// width minus the (clamped) mnemonic gutter. It mirrors restWidth in renderItem.
func titleWidth(cfg renderConfig) int {
	gutterW := max(cfg.gutterW, 2)
	innerW := max(cfg.colWidth-2, 1)
	w := max(innerW-gutterW, 1)
	return w
}

// wrapTitle splits title into the rows a card draws for it. When wrap is false
// (or maxLines <= 1) it returns a single ellipsis-truncated line, preserving the
// original behavior. Otherwise it word-wraps to width and returns at most
// maxLines rows, ellipsis-truncating the last kept row when content overflows.
// It is the single source of truth for title row count: itemHeight measures the
// returned length and renderItem draws the returned rows.
func wrapTitle(title string, width, maxLines int, wrap bool) []string {
	if width < 1 {
		width = 1
	}
	if !wrap || maxLines <= 1 {
		return []string{truncLine(title, width)}
	}
	wrapped := ansi.Wordwrap(title, width, " -/")
	lines := strings.Split(wrapped, "\n")
	if len(lines) <= maxLines {
		for i, ln := range lines {
			lines[i] = truncLine(ln, width)
		}
		return lines
	}
	kept := lines[:maxLines]
	// The last visible row absorbs everything that didn't fit, then truncates,
	// so the ellipsis signals there is more title than shown.
	rest := strings.Join(lines[maxLines-1:], " ")
	kept[maxLines-1] = truncLine(rest, width)
	for i := range maxLines - 1 {
		kept[i] = truncLine(kept[i], width)
	}
	return kept
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
// width back to colWidth, and 2 rows; the body lines determine the rest of the
// height (title + preview + optional fields + meta).
func renderCard(innerW int, borderFg color.Color, lines ...string) string {
	block := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderFg).
		Render(block)
}

// renderSeparatorStr draws an inert grouping row: a centered label flanked by
// rule glyphs, vertically padded to fill the cardRows item slot. Separators
// stay borderless — they are grouping rules, not cards.
func renderSeparatorStr(item Item, d renderConfig) string {
	p := d.palette
	var fg color.Color = p.FgMuted
	if item.Accent != "" {
		fg = lipgloss.Color(item.Accent)
	}
	label := strings.TrimSpace(item.Title)
	var line string
	if label != "" {
		label = " " + strings.ToUpper(label) + " "
	}
	dashes := max(d.colWidth-lipgloss.Width(label)-2, 0)
	left := dashes / 2
	right := dashes - left
	line = strings.Repeat("─", left) + label + strings.Repeat("─", right)
	style := lipgloss.NewStyle().Width(d.colWidth).MaxWidth(d.colWidth).Foreground(fg)
	blank := lipgloss.NewStyle().Width(d.colWidth).Render("")

	total := itemHeight(item, d)
	ruleRow := total / 2
	rows := make([]string, 0, total)
	for i := range total {
		if i == ruleRow {
			rows = append(rows, style.Render(line))
		} else {
			rows = append(rows, blank)
		}
	}
	return strings.Join(rows, "\n")
}

// renderVirtualStr draws a script-supplied item in the card frame: icon + title
// (line 1, tinted by Accent), preview, the optional frontmatter `render:` line,
// and the script-provided Meta string in place of the filesystem meta line.
func renderVirtualStr(item Item, isSelected bool, d renderConfig) string {
	p := d.palette
	gutterW := max(d.gutterW, 2)
	innerW := max(d.colWidth-2, 1)
	mnemonic := ""
	if d.mnemonicOf != nil {
		mnemonic = d.mnemonicOf(item.Name)
	}

	var rowBg, mnemFg, nameFg, cardBorder color.Color
	hasRowBg := false
	switch {
	case isSelected && d.isActive:
		rowBg = p.PrimaryStrong
		mnemFg = p.Highlight
		nameFg = p.FgOnAccent
		cardBorder = p.PrimaryStrong
		hasRowBg = true
	default:
		mnemFg = p.Warning
		nameFg = p.FgEmphasis
		cardBorder = p.BorderMuted
	}
	if item.Accent != "" && !hasRowBg {
		nameFg = lipgloss.Color(item.Accent)
	}

	gutterStyle := lipgloss.NewStyle().Bold(true).Foreground(mnemFg).Width(gutterW)
	restWidth := max(innerW-gutterW, 1)
	restStyle := lipgloss.NewStyle().Bold(true).Foreground(nameFg).Width(restWidth).MaxWidth(restWidth)
	if hasRowBg {
		gutterStyle = gutterStyle.Background(rowBg)
		restStyle = restStyle.Background(rowBg)
	}
	gutterText := mnemonic
	if gutterText == "" && isSelected && d.isActive {
		gutterText = ">"
	}
	titleRows := wrapTitle(composeTitle(item), restWidth, d.titleMaxLines, d.wrapTitles)
	titleLines := make([]string, len(titleRows))
	for i, row := range titleRows {
		gt := ""
		if i == 0 {
			gt = gutterText
		}
		titleLines[i] = gutterStyle.Render(gt) + restStyle.Render(row)
	}

	var previewFg, detailBg color.Color
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

	lines := append(titleLines, previewLine)
	if len(item.Render) > 0 {
		lines = append(lines, fieldsRow(item, isSelected && d.isActive, innerW, gutterW, detailBg, p))
	}
	lines = append(lines, metaLine)
	return renderCard(innerW, cardBorder, lines...)
}

// Column represents one kanban column backed by a directory.
type Column struct {
	Name        string
	Path        string
	Items       []Item // unfiltered master list (used by file operations)
	list        vlist.Model
	itemOpts    ItemOptions
	listYOffset int
	palette     Palette

	// width/height are the last geometry the layout handed this column; renderCfg
	// is the last per-frame render context. They are kept so the list's delegate
	// (and so its item heights) stay valid for calls made between frames — e.g.
	// HitTest during a mouse event, which runs before the next View.
	width     int
	height    int
	renderCfg renderConfig

	// transformed marks a filesystem column whose item order is currently
	// script-defined (a column_items hook returned a table for it). Rendered
	// as a ƒ glyph in the header so a hidden/reordered card is explainable.
	// Maintained by Board.applyColumnTransform.
	transformed bool

	// Virtual-column state. A virtual column has no filesystem backing: its
	// Items are pushed by a script via kbrd.column.set, file moves into/out of
	// it are rejected, and its actions come from colCmds rather than the
	// built-in mutation keys. Zero for ordinary filesystem columns.
	Virtual    bool
	VID        string       // Lua-facing id (stable key for set/clear)
	colCmds    []VirtualCmd // column-scoped commands (B)
	defaultCmd string       // id of the Enter/default command (optional)
	emptyText  string       // placeholder shown when there are no items

	// Script-set appearance overrides (virtual columns). Zero values mean
	// "use the cfg/palette default". Width participates in layout geometry;
	// headerFG/headerBG paint the header bar in renderHeader.
	Width    int
	headerFG color.Color
	headerBG color.Color

	// Collapsed is the user's persisted intent to shrink this column to a thin
	// vertical bar (toggled with "|", restored from colstore on load). It is
	// intent, not the rendered state: the focused column auto-expands, so the
	// live decision is made in Board.colWidthOf (Collapsed && not selected),
	// which drives both the bar width and Column.View's collapsed path.
	Collapsed bool
}

// collapsedContentWidth is the content width of a collapsed column's bar: wide
// enough to set the vertical name/count off the border with a cell of padding
// each side. Its on-screen width is this plus columnSlotPadding (border +
// margin).
const collapsedContentWidth = 3

// collapsedStoreKey is the colstore key that persists a filesystem column's
// collapse intent across restarts.
const collapsedStoreKey = "collapsed"

// ContentWidth is the column's content width: its script-set override when one
// is given, else the supplied default (cfg.ColumnWidth). Layout owns geometry,
// so this is the single place an override feeds the slot math.
func (c *Column) ContentWidth(def int) int {
	if c.Width > 0 {
		return c.Width
	}
	return def
}

// ToggleCollapse flips this column's collapse intent and persists it. The board
// only dispatches the key; the flag, its persistence, and how a collapsed column
// renders all live here in the column component.
func (c *Column) ToggleCollapse() {
	c.Collapsed = !c.Collapsed
	c.persistCollapsed()
}

// collapseFocusShift returns the column index to focus after collapsing the one
// at `selected` (of `n` columns). The focused column always auto-expands, so the
// just-folded bar only shows once focus leaves it; this keeps focus adjacent —
// the previous column at the right edge, otherwise the next. With a single
// column there is nowhere to go, so focus stays put.
func collapseFocusShift(selected, n int) int {
	switch {
	case n <= 1:
		return selected
	case selected == n-1:
		return selected - 1
	default:
		return selected + 1
	}
}

// Expand clears the column's collapse intent for this session when something
// explicitly surfaces its content — e.g. a script selecting one of its items —
// so the column opens and stays open instead of re-collapsing on the next
// keypress (unlike the transient auto-expand a focused column gets). It does not
// persist: a script revealing an item must not overwrite the user's saved
// collapse preference, which returns on the next launch.
func (c *Column) Expand() { c.Collapsed = false }

// persistCollapsed best-effort writes the collapse intent to the column's
// colstore. Virtual columns have no backing dir, so their collapse is
// session-only. A failed write only means the state won't survive a restart, so
// it is swallowed rather than surfaced.
func (c *Column) persistCollapsed() {
	if c.Virtual || c.Path == "" {
		return
	}
	_ = colstore.Update(c.Path, func(s *colstore.Store) error {
		s.Set(collapsedStoreKey, c.Collapsed)
		return nil
	})
}

// RestoreCollapsed loads the persisted collapse intent for a filesystem column.
// Missing/invalid state leaves the column expanded. Called once at build time.
func (c *Column) RestoreCollapsed() {
	if c.Virtual || c.Path == "" {
		return
	}
	s, err := colstore.Read(c.Path)
	if err != nil {
		return
	}
	if v, ok := s.Get(collapsedStoreKey); ok {
		if b, ok := v.(bool); ok {
			c.Collapsed = b
		}
	}
}

// VirtualCmd is a column-scoped command surfaced in the X menu / status hints
// for a virtual column. Ref is the host dispatch handle.
type VirtualCmd struct {
	ID           string
	Name         string
	Key          string
	Default      bool
	RequiresItem bool // false lets the command run on an empty column
	Ref          string
}

// vlistKeys maps the board's cursor bindings into the list engine so j/k and the
// arrows are defined in exactly one place (model/keys.go).
func vlistKeys() vlist.KeyMap {
	return vlist.KeyMap{
		Up: Keys.CursorUp, Down: Keys.CursorDown,
		PageUp: Keys.ColPageUp, PageDown: Keys.ColPageDown,
	}
}

// NewColumn builds a column over a directory. Widths are not stored: layout
// owns geometry, and every render passes the column its width via RenderCtx.
func NewColumn(name, path string, itemOpts ItemOptions) *Column {
	palette := DarkPalette()
	c := &Column{
		Name:      name,
		Path:      path,
		list:      vlist.New(vlistKeys()),
		itemOpts:  itemOpts,
		palette:   palette,
		renderCfg: renderConfig{palette: palette, gutterW: 2, previewLines: 1},
	}
	c.syncDelegate()
	return c
}

// NewVirtualColumn builds an empty script-backed column, then flips the virtual
// flag and clears the filesystem Path.
func NewVirtualColumn(vid, name string, palette Palette) *Column {
	c := NewColumn(name, "", ItemOptions{})
	c.Virtual = true
	c.VID = vid
	c.palette = palette
	c.renderCfg.palette = palette
	c.syncDelegate()
	return c
}

// syncDelegate hands the list a fresh cardDelegate over the current items and
// render context. Called whenever items or the render context change.
func (c *Column) syncDelegate() {
	c.list.SetDelegate(cardDelegate{items: c.Items, cfg: c.renderCfg})
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
	// set replaces the column wholesale, so an absent field resets to default.
	c.Width = spec.Width
	c.headerFG = lipgloss.Color(spec.HeaderFG)
	c.headerBG = lipgloss.Color(spec.HeaderBG)

	c.colCmds = c.colCmds[:0]
	c.defaultCmd = ""
	for _, vc := range spec.Commands {
		c.colCmds = append(c.colCmds, VirtualCmd{
			ID: vc.ID, Name: vc.Name, Key: vc.Key, Default: vc.Default,
			RequiresItem: vc.RequiresItem, Ref: vc.Ref,
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
	c.syncDelegate()
	c.list.Reload()
	c.restoreVirtualCursor(prevName, prevIdx)
}

// restoreVirtualCursor re-selects the item named prevName after a re-push; if it
// is gone, it clamps to the previous index position (bounded by the new length).
func (c *Column) restoreVirtualCursor(prevName string, prevIdx int) {
	if prevName != "" {
		for i, it := range c.Items {
			if it.Name == prevName {
				c.list.SelectUnderlying(i)
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
	c.height = h
	c.list.SetSize(c.width, c.height)
}

func setColumnHeights(cols []*Column, h int) {
	for _, col := range cols {
		col.SetHeight(h)
	}
}

func (c *Column) UpdateList(msg tea.Msg) tea.Cmd {
	return c.list.Update(msg)
}

// BeginFilter focuses the filter input; the returned command drives its cursor
// blink.
func (c *Column) BeginFilter() tea.Cmd {
	return c.list.BeginFilter()
}

// ScrollBy scrolls the list content by n rows (positive = down) without moving
// the cursor — used by mouse-wheel handling.
func (c *Column) ScrollBy(n int) {
	c.list.ScrollBy(n)
}

func (c *Column) renderHeader(isActive bool, leftPad, width int, ind colIndicator) string {
	p := c.palette
	var nameFg, countFg, sepColor color.Color
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
	// Script-set header colors win over the focus-derived defaults in both
	// states, giving the column a fixed look. lipgloss resets the background
	// after each rendered segment, so the bar can't be filled by wrapping the
	// finished line — every piece (including the plain pad/spacer) must carry
	// the bg itself, else the gaps between segments show through. withBG adds it
	// to a styled segment; fill paints a plain run. Both are no-ops when unset.
	if c.headerFG != nil {
		nameFg = c.headerFG
	}
	withBG := func(s lipgloss.Style) lipgloss.Style {
		if c.headerBG != nil {
			return s.Background(c.headerBG)
		}
		return s
	}
	fill := func(s string) string { return withBG(lipgloss.NewStyle()).Render(s) }
	countLabel := strconv.Itoa(c.TotalCount())
	filtered := c.list.Filtered() && !c.list.Filtering()
	if filtered {
		countLabel = strconv.Itoa(len(c.list.Visible())) + "/" + strconv.Itoa(c.TotalCount())
	}

	indicator := fill("  ")
	if isActive {
		indicator = withBG(lipgloss.NewStyle().Foreground(sepColor).Bold(true)).Render("▍ ")
	}
	if filtered {
		indicator = withBG(lipgloss.NewStyle().Foreground(countFg)).Render("⌕ ")
	}

	leftPadStr := ""
	if leftPad > 0 {
		leftPadStr = fill(strings.Repeat(" ", leftPad))
	}

	name := withBG(lipgloss.NewStyle().Bold(true).Foreground(nameFg)).Render(nameLabel)
	if c.transformed {
		// ƒ marks a script-defined order (column_items hook), mirroring the
		// ◇ virtual marker: a soft-accent hint, not a selection cue.
		name += withBG(lipgloss.NewStyle().Foreground(c.palette.AccentSoft)).Render(" ƒ")
	}
	if ind.Text != "" {
		// Script-set per-column label (kbrd.column.indicator). Default to the
		// same soft accent as ƒ; a script-supplied fg/bold wins.
		var fg color.Color = c.palette.AccentSoft
		if ind.FG != "" {
			fg = lipgloss.Color(ind.FG)
		}
		name += withBG(lipgloss.NewStyle().Foreground(fg).Bold(ind.Bold)).Render(" " + ind.Text)
	}
	count := withBG(lipgloss.NewStyle().Foreground(countFg)).Render(countLabel)

	used := lipgloss.Width(leftPadStr) + lipgloss.Width(indicator) + lipgloss.Width(name) + lipgloss.Width(count)
	gap := max(width-used, 1)
	spacer := fill(strings.Repeat(" ", gap))

	header := leftPadStr + indicator + name + spacer + count
	sep := lipgloss.NewStyle().Foreground(sepColor).Render(strings.Repeat("─", width))

	return lipgloss.JoinVertical(lipgloss.Left, header, sep)
}

// RenderCtx is the render environment Board hands a column for one frame:
// focus, geometry (from layout), and the board-level lookups cards need.
type RenderCtx struct {
	Active        bool
	Width         int  // content width allotted by layout (excludes border)
	PreviewLines  int  // preview rows per card (Slot.PreviewLines)
	WrapTitles    bool // word-wrap titles across rows instead of truncating
	TitleMaxLines int  // cap on wrapped title rows (<=1 disables wrapping)
	GutterW       int  // mnemonic gutter width
	MnemonicOf    func(name string) string
	StatFor       func(absPath string) (kbrdfs.DiffStat, bool)
	Indicator     colIndicator // script-set header label for this column (empty Text = none)
}

// scrollGutterW is the column count reserved on the right edge of the list area
// for the scrollbar. Reserved permanently (even when the column fits) so card
// text width stays stable as a column crosses the overflow threshold.
const scrollGutterW = 1

func (c *Column) View(ctx RenderCtx) string {
	// A collapsed column is allotted collapsedContentWidth by layout; render it
	// as a thin vertical bar instead of the normal header/list (which a width-1
	// box can't hold). Keyed off the incoming width so View stays self-contained
	// — it never needs to know which column is selected.
	if ctx.Width <= collapsedContentWidth {
		return c.viewCollapsed(ctx)
	}

	listW := max(ctx.Width-scrollGutterW, 1)
	c.renderCfg = renderConfig{
		isActive:      ctx.Active,
		mnemonicOf:    ctx.MnemonicOf,
		gutterW:       ctx.GutterW,
		colWidth:      listW,
		previewLines:  ctx.PreviewLines,
		wrapTitles:    ctx.WrapTitles,
		titleMaxLines: ctx.TitleMaxLines,
		statFor:       ctx.StatFor,
		palette:       c.palette,
	}
	c.width = ctx.Width
	c.list.SetSize(listW, c.height)
	c.syncDelegate()

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

	leftPad := max(ctx.GutterW-2, 0)
	header := c.renderHeader(ctx.Active, leftPad, ctx.Width, ctx.Indicator)
	listView := c.list.View()
	if len(c.Items) == 0 {
		// vlist draws nothing for an empty list; show the placeholder text the
		// bubbles list used to ("No items."), or the script-set empty text, while
		// keeping the same body height a populated column gets from vlist.
		text := "No items."
		if c.Virtual && c.emptyText != "" {
			text = c.emptyText
		}
		bodyH := max(c.height-c.list.HeaderLines(), 0)
		placeholder := lipgloss.NewStyle().
			PaddingLeft(2).
			Width(listW).
			Height(bodyH).
			Foreground(c.palette.FgDim).
			Render(text)
		if c.list.HeaderLines() > 0 {
			filterView, _, _ := strings.Cut(listView, "\n")
			listView = lipgloss.JoinVertical(lipgloss.Left, filterView, placeholder)
		} else {
			listView = placeholder
		}
	}
	c.listYOffset = 1 + lipgloss.Height(header) + c.list.HeaderLines()

	// Attach the scrollbar gutter on the right edge of the list area. The bar is
	// painted only when the column overflows; otherwise the gutter is blank.
	offset, vp, total := c.list.ScrollMetrics()
	gutter := c.renderScrollbar(offset, vp, total, lipgloss.Height(listView), c.list.HeaderLines(), ctx.Active)
	listArea := lipgloss.JoinHorizontal(lipgloss.Top, listView, gutter)

	content := lipgloss.JoinVertical(lipgloss.Left,
		header,
		listArea,
	)

	return lipgloss.NewStyle().
		Border(border).
		BorderForeground(borderColor).
		Render(content)
}

// viewCollapsed renders a collapsed column as a one-cell-wide bar: the column
// name stacked top-to-bottom (centered), with its item count at the bottom
// under a rule. It mirrors the normal column's height and border so it sits
// flush beside its expanded neighbors. A collapsed column is never the focused
// one (the focus auto-expands), so it always paints in the muted palette.
func (c *Column) viewCollapsed(ctx RenderCtx) string {
	c.width = ctx.Width

	p := c.palette
	var nameFg color.Color = p.FgMuted
	if c.headerFG != nil {
		nameFg = c.headerFG
	} else if c.Virtual {
		nameFg = p.AccentSoft
	}

	// Inner height matches the expanded column's content: 2 header rows + the
	// list area, so both wrap to the same bordered height under JoinHorizontal.
	innerH := max(c.height+2, 1)

	nameRunes := []rune(strings.ToUpper(c.Name))
	countRunes := []rune(strconv.Itoa(c.TotalCount()))

	// Reserve the bottom for the count under a rule, but only when at least one
	// name row would still fit above it.
	var bottom []string
	if len(countRunes)+2 <= innerH {
		bottom = append(bottom, strings.Repeat("─", collapsedContentWidth))
		for _, r := range countRunes {
			bottom = append(bottom, string(r))
		}
	}

	nameBudget := innerH - len(bottom)
	if len(nameRunes) > nameBudget {
		// The bar is a hint; the full name is one keystroke away on expand.
		nameRunes = nameRunes[:nameBudget]
	}

	rows := make([]string, 0, innerH)
	pad := (nameBudget - len(nameRunes)) / 2
	for range pad {
		rows = append(rows, " ")
	}
	for _, r := range nameRunes {
		rows = append(rows, string(r))
	}
	for len(rows)+len(bottom) < innerH {
		rows = append(rows, " ")
	}
	rows = append(rows, bottom...)

	body := lipgloss.NewStyle().
		Foreground(nameFg).
		Width(collapsedContentWidth).
		Align(lipgloss.Center).
		Render(strings.Join(rows, "\n"))

	border := lipgloss.RoundedBorder()
	if c.Virtual {
		border = lipgloss.DoubleBorder()
	}
	return lipgloss.NewStyle().
		Border(border).
		BorderForeground(p.BorderMuted).
		Render(body)
}

// renderScrollbar draws the scrollbar gutter: a scrollGutterW-wide, height-tall
// vertical strip. When the content fits the viewport (content <= viewport) the
// strip is blank — the gutter stays reserved but unpainted. Otherwise a track
// (│) fills the viewport rows with a proportionally sized, positioned thumb (█).
// headerLines blank rows are prepended so the painted region lines up with the
// scrollable viewport, not the filter bar that sits above it.
func (c *Column) renderScrollbar(offset, viewport, content, height, headerLines int, active bool) string {
	blank := strings.Repeat(" ", scrollGutterW)
	rows := make([]string, height)
	for i := range rows {
		rows[i] = blank
	}

	vpRows := height - headerLines
	if content <= viewport || vpRows <= 0 || content <= 0 {
		return strings.Join(rows, "\n")
	}

	thumb := min(
		// round(viewport/content * vpRows)
		max(

			(viewport*vpRows+content/2)/content, 1), vpRows)
	pos := max(
		// round(offset/content * vpRows)
		min(

			(offset*vpRows+content/2)/content, vpRows-thumb), 0)

	trackStyle := lipgloss.NewStyle().Width(scrollGutterW).Foreground(c.palette.FgDim)
	thumbFg := c.palette.FgDim
	if active {
		thumbFg = c.palette.FgMuted
	}
	thumbStyle := lipgloss.NewStyle().Width(scrollGutterW).Foreground(thumbFg)
	track := strings.Repeat("│", scrollGutterW)
	bar := strings.Repeat("┃", scrollGutterW)
	for i := range vpRows {
		if i >= pos && i < pos+thumb {
			rows[headerLines+i] = thumbStyle.Render(bar)
		} else {
			rows[headerLines+i] = trackStyle.Render(track)
		}
	}
	return strings.Join(rows, "\n")
}

func (c *Column) IsFiltering() bool {
	return c.list.Filtering()
}

// HitTest maps a y-coordinate (relative to the top of this column's box) to a
// visible item index. Returns ok=false when the click lands outside any item
// (border, header, gap, filter bar, overflow footer, or past the last item).
func (c *Column) HitTest(yInBox int) (int, bool) {
	return c.list.HitTest(yInBox - c.listYOffset)
}

func (c *Column) SelectIndex(i int) {
	c.list.Select(i)
}

// SelectByName selects the item with the given name, if present.
func (c *Column) SelectByName(name string) {
	for i, item := range c.Items {
		if item.Name == name {
			c.list.SelectUnderlying(i)
			return
		}
	}
}

// CursorAtTop reports whether there is no selectable row above the cursor.
func (c *Column) CursorAtTop() bool { return c.list.AtTop() }

// CursorAtBottom reports whether there is no selectable row below the cursor.
func (c *Column) CursorAtBottom() bool { return c.list.AtBottom() }

// SelectFirst selects the first selectable (non-separator) visible item.
func (c *Column) SelectFirst() { c.list.SelectFirst() }

// SelectLast selects the last selectable (non-separator) visible item.
func (c *Column) SelectLast() { c.list.SelectLast() }

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
	c.syncDelegate()
	c.list.Reload()
	return nil
}

// SetItems replaces the master item slice and the underlying list, preserving
// the cursor by item name. Used by the column_items transform to apply a
// script-defined order after a (re)load.
func (c *Column) SetItems(items []Item) {
	prevName := ""
	if sel := c.SelectedItem(); sel != nil {
		prevName = sel.Name
	}
	c.Items = items
	c.syncDelegate()
	c.list.Reload()
	if prevName != "" {
		c.SelectByName(prevName)
	}
}

func (c *Column) TotalCount() int {
	return len(c.Items)
}

// VisibleItems returns the items currently rendered (post filter+sort), in
// display order.
func (c *Column) VisibleItems() []Item {
	vis := c.list.Visible()
	out := make([]Item, 0, len(vis))
	for _, ui := range vis {
		if ui >= 0 && ui < len(c.Items) {
			out = append(out, c.Items[ui])
		}
	}
	return out
}

func (c *Column) HasSelectedItem() bool {
	_, ok := c.list.Selected()
	return len(c.Items) > 0 && ok
}

func (c *Column) SelectedItem() *Item {
	ui, ok := c.list.Selected()
	if !ok || ui < 0 || ui >= len(c.Items) {
		return nil
	}
	item := c.Items[ui]
	return &item
}

func (c *Column) MoveItemTo(destCol *Column, itemName string) error {
	src := boardops.ColumnRef{Name: c.Name, Path: c.Path}
	dst := boardops.ColumnRef{Name: destCol.Name, Path: destCol.Path}
	if _, err := boardops.MoveItem(src, dst, itemName); err != nil {
		return err
	}
	c.LoadItems()
	destCol.LoadItems()
	return nil
}

func (c *Column) DeleteItem(itemName string) error {
	if _, err := boardops.DeleteItem(boardops.ColumnRef{Name: c.Name, Path: c.Path}, itemName); err != nil {
		return err
	}
	return nil
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
	res, err := boardops.CreateItem(boardops.ColumnRef{Name: c.Name, Path: c.Path}, name, content)
	if err != nil {
		return "", err
	}
	c.LoadItems()
	return filepath.Base(res.Path), nil
}

// The content mutations below resolve the item's path through the in-memory
// list (so virtual-column items mutate wherever they really live) and
// delegate the content semantics to package board, shared with the web
// frontend.

func (c *Column) AppendText(itemName, text string) error {
	fullPath := c.fullPathFor(itemName)
	if fullPath == "" {
		return os.ErrNotExist
	}
	return board.AppendLine(fullPath, text)
}

func (c *Column) PrependText(itemName, text string) error {
	fullPath := c.fullPathFor(itemName)
	if fullPath == "" {
		return os.ErrNotExist
	}
	return board.PrependLine(fullPath, text)
}

func (c *Column) ReplaceFile(itemName, text string) error {
	fullPath := c.fullPathFor(itemName)
	if fullPath == "" {
		return os.ErrNotExist
	}
	return board.ReplaceFileContent(fullPath, text)
}

func (c *Column) JournalText(itemName string, at time.Time, text string) error {
	fullPath := c.fullPathFor(itemName)
	if fullPath == "" {
		return os.ErrNotExist
	}
	return board.JournalLine(fullPath, at, text)
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
	if _, err := boardops.RenameItem(boardops.ColumnRef{Name: c.Name, Path: c.Path}, oldName, newName); err != nil {
		return err
	}
	return c.LoadItems()
}

func (c *Column) Rename(newName string) error {
	newPath := filepath.Join(filepath.Dir(c.Path), newName)
	if err := board.RenameNoClobber(c.Path, newPath); err != nil {
		return err
	}
	c.Name = newName
	c.Path = newPath
	return c.LoadItems()
}

// PinItem toggles an item's pin state by rewriting its `pinned` frontmatter
// key: pinning sets `pinned: true`, unpinning removes the key. The file name is
// left untouched. LoadItems re-derives Pinned from the new frontmatter.
func (c *Column) PinItem(itemName string) error {
	for i := range c.Items {
		if c.Items[i].Name == itemName {
			raw, err := os.ReadFile(c.Items[i].FullPath)
			if err != nil {
				return err
			}
			var updated string
			if c.Items[i].Pinned {
				updated = frontmatter.Delete(string(raw), "pinned")
			} else {
				updated = frontmatter.Set(string(raw), "pinned", "true")
			}
			if err := board.ReplaceFileContent(c.Items[i].FullPath, updated); err != nil {
				return err
			}
			if err := c.LoadItems(); err != nil {
				return err
			}
			// Pinning re-sorts the column; keep the cursor on the toggled item
			// so it stays selected at its new position. The name is unchanged.
			c.SelectByName(itemName)
			return nil
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

// ItemByName returns the loaded item with the given name, or nil if absent.
// Unlike SelectedItem it does not depend on the cursor, so it resolves the right
// card even when board selection has moved on (e.g. a still-open editor whose
// line command must bind to the file it was opened against).
func (c *Column) ItemByName(name string) *Item {
	for i := range c.Items {
		if c.Items[i].Name == name {
			item := c.Items[i]
			return &item
		}
	}
	return nil
}

func (c *Column) IndexByName(name string) (int, bool) {
	for i := range c.Items {
		if c.Items[i].Name == name {
			return i, true
		}
	}
	return 0, false
}

// ItemByPath returns the loaded item at path, or nil if absent.
func (c *Column) ItemByPath(path string) *Item {
	for i := range c.Items {
		if samePath(c.Items[i].FullPath, path) {
			item := c.Items[i]
			return &item
		}
	}
	return nil
}

// FrontmatterKeys returns the parsed frontmatter keys currently loaded for this
// column. It keeps callers from reaching into the item slice when they only
// need aggregate card metadata.
func (c *Column) FrontmatterKeys() []string {
	seen := map[string]struct{}{}
	for i := range c.Items {
		for k := range c.Items[i].Data {
			seen[k] = struct{}{}
		}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	return keys
}
