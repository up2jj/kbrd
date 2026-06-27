package model

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"

	"kbrd/theme"
)

// Standardized popup chrome. Every centered overlay routes through OverlayFrame
// so padding, border, title treatment, and footer legend stay identical.
const (
	overlayPadV = 1
	overlayPadH = 3
)

// overlayTitleStyle styles the title embedded in every overlay's top border.
// Set from the palette in setHelpStyles so it follows theme switches.
var overlayTitleStyle lipgloss.Style

// OverlayFrame renders a popup with unified chrome: a rounded border carrying
// the Title in its top edge, the caller-built Body, and a Footer legend (built
// by the caller via RenderInlineHints, or a status/warning line).
type OverlayFrame struct {
	Title  string
	Body   string
	Footer string
	// Width is the outer content width (0 = fit content), forwarded to lipgloss.
	Width   int
	Palette Palette
	// Border overrides the border color; empty uses Palette.BorderActive.
	Border color.Color
}

func (f OverlayFrame) Render() string {
	border := f.Border
	if border == nil {
		border = f.Palette.BorderActive
	}
	content := f.Body
	if f.Footer != "" {
		content = lipgloss.JoinVertical(lipgloss.Left, f.Body, "", f.Footer)
	}
	return theme.RoundedFrame(f.Title, overlayTitleStyle, content, border, overlayPadV, overlayPadH, f.Width)
}

// composeOverlay centers overlay over base within the band of height bandH that
// sits between the header (height headerH) and the bottom keybar, so both stay
// visible behind the popup. base is padded with blank lines when a tall overlay
// would otherwise overflow it.
func composeOverlay(base, overlay string, w, headerH, bandH int) string {
	ow := lipgloss.Width(overlay)
	oh := lipgloss.Height(overlay)
	x := max((w-ow)/2, 0)
	y := headerH + max((bandH-oh)/2, 0)

	if baseH := lipgloss.Height(base); y+oh > baseH {
		base += strings.Repeat("\n", y+oh-baseH)
	}
	return theme.PlaceOverlay(x, y, overlay, base)
}
