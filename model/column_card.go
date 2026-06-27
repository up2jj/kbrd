package model

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	kbrdfs "kbrd/fs"
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
// stacks. It must match what renderItem / renderCardBody draw — both derive the
// title row count from wrapTitle, so they cannot disagree. Separators keep their
// single-line rule layout.
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
		return renderCardBody(item, selected, cfg, item.Meta, cfg.palette.BorderMuted)
	}
	return renderCardBody(item, selected, cfg, filesystemMeta(item, cfg), cfg.palette.FgSubtle)
}

// renderCardBody draws the shared card frame for filesystem and virtual items.
// The callers supply the already-resolved meta text and the selected-but-
// inactive meta color, which are the only rendering differences between kinds.
func renderCardBody(item Item, selected bool, cfg renderConfig, meta string, inactiveSelectedMetaFg color.Color) string {
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

	var metaFg color.Color
	switch {
	case isSelected && d.isActive:
		metaFg = p.AccentSoft
	case isSelected:
		metaFg = inactiveSelectedMetaFg
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

// filesystemMeta builds the meta row for a filesystem-backed card. A frontmatter
// `meta` replaces the computed modified + size + git diff block, while tags and
// malformed-YAML badges are still appended as card annotations.
func filesystemMeta(item Item, d renderConfig) string {
	p := d.palette
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
	return meta
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
