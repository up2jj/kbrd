package model

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type boardViewFrame struct {
	b *Board
}

func (f boardViewFrame) render() string {
	b := f.b
	if len(b.columns) == 0 {
		return f.renderEmpty()
	}
	if v, ok := f.renderTiny(); ok {
		return v
	}

	w, h := f.size()
	base, headerH, barH := f.renderBase(w, h)
	overlay := f.activeOverlay(w, h)
	if overlay == "" {
		return base
	}
	return composeOverlay(base, overlay, w, headerH, h-headerH-barH)
}

func (f boardViewFrame) size() (int, int) {
	w, h := f.b.termWidth, f.b.termHeight
	if w == 0 {
		w = 80
	}
	if h == 0 {
		h = 24
	}
	return w, h
}

func (f boardViewFrame) renderEmpty() string {
	b := f.b
	w, h := f.size()
	if dialogView := b.dialog.View(); dialogView != "" {
		return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, dialogView)
	}
	return "No columns found in " + b.cfg.Path
}

func (f boardViewFrame) renderTiny() (string, bool) {
	b := f.b
	if b.termWidth > 0 && b.termWidth < minBoardWidth(b.cfg.ColumnWidth) {
		w, h := f.size()
		return f.renderTinyMessage(w, h), true
	}
	if b.termHeight > 0 && b.termHeight < 10 {
		w := b.termWidth
		if w == 0 {
			w = 80
		}
		return f.renderTinyMessage(w, b.termHeight), true
	}
	return "", false
}

func (f boardViewFrame) renderTinyMessage(w, h int) string {
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center,
		lipgloss.NewStyle().Foreground(f.b.palette.FgMuted).Render("terminal too small"))
}

func (f boardViewFrame) renderBase(w, h int) (string, int, int) {
	b := f.b
	header := b.statusPresenter().renderHeader(w)
	columnsView := b.presenter.renderColumns(b, w)
	result := header + "\n" + columnsView
	if quickCmdView := f.renderQuickCommand(); quickCmdView != "" {
		result += "\n" + quickCmdView
	}

	// The board is always rendered with the keybar pinned to the bottom row, so it
	// reads as a persistent bar. An active overlay is composited on top, centered
	// in the band between the header and the keybar, so both stay visible behind
	// it. The open editor has its own footer, so the board keybar is suppressed to
	// avoid two competing footers.
	statusBar := f.renderStatusBar()
	if b.editor.state != editorNone {
		statusBar = ""
	}
	headerH := lipgloss.Height(header)
	barH := 0
	if statusBar != "" {
		barH = lipgloss.Height(statusBar)
	}
	if pad := h - lipgloss.Height(result) - barH; pad > 0 {
		result += strings.Repeat("\n", pad)
	}
	base := result
	if statusBar != "" {
		base += "\n" + statusBar
	}

	// Reserve the header/keybar rows so the editor modal fits the band it is
	// composited into (below the header) instead of overflowing off-screen. This
	// reserve is stable, so the buffer height matches at scroll-time and
	// render-time.
	if b.editor.state != editorNone {
		b.editor.headerReserve = headerH + barH
		b.editor.applySize()
	}
	return base, headerH, barH
}

func (f boardViewFrame) renderStatusBar() string {
	b := f.b
	width := b.termWidth
	if width == 0 {
		width = 80
	}

	ctx := ShortcutContext{QuickCmdMode: b.quickCmdMode, Zoomed: b.zoom.Active()}
	ctx.HasSelectedItem = b.selectedCol < len(b.columns) && b.columns[b.selectedCol].HasSelectedItem()
	if b.selectedCol < len(b.columns) && b.columns[b.selectedCol].Virtual {
		col := b.columns[b.selectedCol]
		ctx.Virtual = true
		for _, vc := range col.colCmds {
			if vc.Key != "" {
				ctx.VirtualCmds = append(ctx.VirtualCmds, Shortcut{Keys: vc.Key, Label: vc.Name})
			}
		}
	}

	// The board/column info and transient activity indicators live in the header
	// cells, so the bottom bar is just keyboard hints.
	secondary := RenderInlineHints(ContextShortcuts(ctx))
	return lipgloss.NewStyle().
		Width(width).
		Align(lipgloss.Center).
		BorderStyle(lipgloss.NormalBorder()).
		BorderTop(true).
		BorderForeground(b.palette.BorderMuted).
		Render(secondary)
}

func (f boardViewFrame) renderQuickCommand() string {
	b := f.b
	if !b.quickCmdMode {
		return ""
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(b.palette.BorderActive).
		Padding(0, 1)
	return box.Render(b.quickCmdInput.View())
}

func (f boardViewFrame) renderEditor() string {
	if f.b.editor.state == editorNone {
		return ""
	}
	return f.b.editor.View()
}

// activeOverlay returns the single popup to draw over the board, or "" when none
// is open. Priority mirrors the key-routing order in boardInputRouter.
func (f boardViewFrame) activeOverlay(w, h int) string {
	b := f.b
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
	// still-open editor and must render on top.
	if b.customCmds.Active() {
		return b.customCmds.View(b.termWidth, b.termHeight)
	}
	if b.scriptUI.Active() {
		return b.scriptUI.View()
	}
	if v := f.renderEditor(); v != "" {
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
