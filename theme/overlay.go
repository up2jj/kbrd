package theme

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

const (
	OverlayPadV = 1
	OverlayPadH = 3
)

// Hint is one keyboard legend entry shared by UI packages without requiring a
// dependency on model.
type Hint struct {
	Keys  string
	Label string
}

// RenderHints renders a compact `key label · key label` legend from semantic
// palette roles.
func RenderHints(p Palette, hints []Hint) string {
	keyStyle := lipgloss.NewStyle().Bold(true).Foreground(p.FgBase)
	labelStyle := lipgloss.NewStyle().Foreground(p.FgMuted)
	sepStyle := lipgloss.NewStyle().Foreground(p.FgDim)
	parts := make([]string, 0, len(hints))
	for _, hint := range hints {
		parts = append(parts, keyStyle.Render(hint.Keys)+" "+labelStyle.Render(hint.Label))
	}
	return strings.Join(parts, sepStyle.Render(" · "))
}

func OverlayTitleStyle(p Palette) lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(p.Primary)
}

// OverlayFrame is the shared lazygit-style popup chrome. It stays in theme so
// independent UI packages (notably git and model) use the same frame without
// an import cycle.
type OverlayFrame struct {
	Title   string
	Body    string
	Footer  string
	Width   int
	Palette Palette
	Border  color.Color
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
	return RoundedFrame(f.Title, OverlayTitleStyle(f.Palette), content, border, OverlayPadV, OverlayPadH, f.Width)
}
