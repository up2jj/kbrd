package model

import (
	"strings"

	"charm.land/lipgloss/v2"

	"kbrd/theme"
)

// Standardized popup chrome. Every centered overlay routes through OverlayFrame
// so padding, border, title treatment, and footer legend stay identical.
const (
	overlayPadV = theme.OverlayPadV
	overlayPadH = theme.OverlayPadH
)

func overlayWidthForBody(bodyW int) int {
	return theme.RoundedFrameWidthForContent(bodyW, overlayPadH)
}

func overlayBodyWidth(frameW int) int {
	return theme.RoundedFrameContentWidth(frameW, overlayPadH)
}

// OverlayFrame is the model alias for the cross-package popup chrome.
type OverlayFrame = theme.OverlayFrame

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
