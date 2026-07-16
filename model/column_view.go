package model

import (
	"image/color"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	kbrdfs "kbrd/fs"
)

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
	IsHarpooned   func(absPath string) bool
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
		isHarpooned:   ctx.IsHarpooned,
		palette:       c.palette,
		isMarked:      c.IsMarked,
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
