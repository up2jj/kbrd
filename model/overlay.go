package model

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

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
	Border lipgloss.Color
}

func (f OverlayFrame) Render() string {
	border := f.Border
	if border == "" {
		border = f.Palette.BorderActive
	}
	content := f.Body
	if f.Footer != "" {
		content = lipgloss.JoinVertical(lipgloss.Left, f.Body, "", f.Footer)
	}
	return theme.RoundedFrame(f.Title, overlayTitleStyle, content, border, overlayPadV, overlayPadH, f.Width)
}

// activeOverlay returns the single popup to draw over the board, or "" when none
// is open. Priority mirrors the key-routing order in updateInner.
func (b *Board) activeOverlay(w, h int) string {
	if b.helpMenu.Active() {
		return b.helpMenu.View(w, h)
	}
	if b.configMenuOpen {
		return RenderConfigCommandsOverlay(configCommandEntries())
	}
	if v := b.dialog.View(); v != "" {
		return v
	}
	// The command menu and script UI are checked before the editor: a line
	// command's menu (and any kbrd.ui.pick/prompt it yields) opens over a
	// still-open editor and must render on top (mirrors the key routing).
	if b.customCmds.Active() {
		return b.customCmds.View(b.termWidth, b.termHeight)
	}
	if b.scriptUI.Active() {
		return b.scriptUI.View()
	}
	if v := b.renderEditor(); v != "" {
		return v
	}
	if b.peek.Active() {
		return b.peek.View(w, h)
	}
	if b.switcher.Active() {
		return b.switcher.View()
	}
	if b.search.Active() {
		return b.search.View(w, h)
	}
	if b.templateFlow.Active() {
		return b.templateFlow.View()
	}
	if b.frontmatterEdit.Active() {
		return b.frontmatterEdit.View()
	}
	if b.git.Active() {
		return b.git.View()
	}
	if b.zellij.Active() {
		return b.zellij.View()
	}
	return ""
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
