package theme

import (
	"image/color"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestRoundedFrameWidthRoundTrip(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		contentW int
		padH     int
	}{
		{name: "no padding", contentW: 12, padH: 0},
		{name: "overlay padding", contentW: 80, padH: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frameW := RoundedFrameWidthForContent(tc.contentW, tc.padH)
			if got := RoundedFrameContentWidth(frameW, tc.padH); got != tc.contentW {
				t.Fatalf("content width round trip = %d, want %d", got, tc.contentW)
			}

			out := RoundedFrame("", lipgloss.NewStyle(), strings.Repeat("x", tc.contentW), color.White, 0, tc.padH, frameW)
			if got := lipgloss.Width(out); got != frameW {
				t.Fatalf("rendered frame width = %d, want %d", got, frameW)
			}
		})
	}
}

func TestStyleContentWidth(t *testing.T) {
	t.Parallel()

	style := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		Padding(0, 1)
	if got := StyleContentWidth(style, 40); got != 36 {
		t.Fatalf("style content width = %d, want 36", got)
	}
}
