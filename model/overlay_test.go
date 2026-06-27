package model

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestOverlayWidthForBody(t *testing.T) {
	t.Parallel()

	const bodyW = 42
	out := OverlayFrame{
		Title:   "Geometry",
		Body:    strings.Repeat("x", bodyW),
		Width:   overlayWidthForBody(bodyW),
		Palette: DarkPalette(),
	}.Render()

	if got := lipgloss.Width(out); got != overlayWidthForBody(bodyW) {
		t.Fatalf("overlay width = %d, want %d", got, overlayWidthForBody(bodyW))
	}
	if got := overlayBodyWidth(overlayWidthForBody(bodyW)); got != bodyW {
		t.Fatalf("overlay body width round trip = %d, want %d", got, bodyW)
	}
}
