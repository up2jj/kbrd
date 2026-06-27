package theme

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func roundedBorderWidth() int {
	b := lipgloss.RoundedBorder()
	return lipgloss.Width(b.Left + b.Right)
}

// RoundedFrameWidthForContent returns the width to pass to RoundedFrame when
// the caller needs exactly contentW cells inside the frame padding.
func RoundedFrameWidthForContent(contentW, padH int) int {
	return max(contentW+2*padH+roundedBorderWidth(), 1)
}

// RoundedFrameContentWidth returns the usable content width inside a RoundedFrame
// rendered with the given frame width and horizontal padding.
func RoundedFrameContentWidth(frameW, padH int) int {
	return max(frameW-2*padH-roundedBorderWidth(), 1)
}

// StyleContentWidth returns the usable content width inside a styled box with
// borders, margins, and padding applied.
func StyleContentWidth(style lipgloss.Style, outerW int) int {
	return max(outerW-style.GetHorizontalFrameSize(), 1)
}

// RoundedFrame wraps content in a rounded border whose top edge carries an
// embedded title (lazygit style: ╭─ Title ─────╮). titleStyle styles the title
// text; border is the border color; padV/padH are the inner padding; width is
// the outer content width passed to lipgloss.Width (0 = fit content). The title
// is truncated with an ellipsis when it would overflow the top edge.
func RoundedFrame(title string, titleStyle lipgloss.Style, content string, border color.Color, padV, padH, width int) string {
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderTop(false).
		BorderForeground(border).
		Padding(padV, padH)
	if width > 0 {
		box = box.Width(width)
	}
	rendered := box.Render(content)
	top := titledTopBorder(title, titleStyle, border, lipgloss.Width(rendered))
	return top + "\n" + rendered
}

// titledTopBorder builds the top border line of a rounded box, embedding title
// after a single dash. outerW is the full box width including both corners.
func titledTopBorder(title string, titleStyle lipgloss.Style, border color.Color, outerW int) string {
	b := lipgloss.RoundedBorder()
	bs := lipgloss.NewStyle().Foreground(border)
	inner := max(outerW-2, 0) // cells between the two corners

	// No room for a title (or none given): a plain dashed top edge.
	if title == "" || inner < 5 {
		return bs.Render(b.TopLeft + strings.Repeat(b.Top, inner) + b.TopRight)
	}

	// Layout between corners: "─ " + title + " " + fill("─"). Reserve one
	// leading dash, two spaces, and at least one trailing dash.
	maxTitle := inner - 4
	t := ansi.Truncate(title, maxTitle, "…")
	fill := max(inner-3-lipgloss.Width(t), 0)

	return bs.Render(b.TopLeft+b.Top+" ") +
		titleStyle.Render(t) +
		bs.Render(" "+strings.Repeat(b.Top, fill)+b.TopRight)
}

// PlaceOverlay composites fg over bg with fg's top-left at (x, y), returning the
// merged string. Background cells covered by fg are replaced; ANSI styling on
// the uncovered left/right portions of each background line is preserved. Lines
// of bg shorter than x are padded with spaces so fg lands at the right column.
func PlaceOverlay(x, y int, fg, bg string) string {
	fgLines := strings.Split(fg, "\n")
	bgLines := strings.Split(bg, "\n")
	for i, fgLine := range fgLines {
		by := y + i
		if by < 0 || by >= len(bgLines) {
			continue
		}
		bgLines[by] = overlayLine(bgLines[by], fgLine, x)
	}
	return strings.Join(bgLines, "\n")
}

// overlayLine splices fg into bg starting at visible column x.
func overlayLine(bg, fg string, x int) string {
	fgW := ansi.StringWidth(fg)
	bgW := ansi.StringWidth(bg)

	left := ansi.Truncate(bg, x, "")
	if pad := x - ansi.StringWidth(left); pad > 0 {
		left += strings.Repeat(" ", pad)
	}

	var right string
	if bgW > x+fgW {
		right = ansi.TruncateLeft(bg, x+fgW, "")
	}
	return left + fg + right
}
