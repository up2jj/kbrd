package model

import (
	"strings"

	"charm.land/lipgloss/v2"
)

type boardViewFrame struct {
	b *Board
}

type boardFrameLayout struct {
	header  string
	body    string
	footer  string
	headerH int
	footerH int
}

type boardOverlayCandidate struct {
	active func() bool
	view   func() string
}

func overlayCandidate(active func() bool, view func() string) boardOverlayCandidate {
	return boardOverlayCandidate{active: active, view: view}
}

func (l boardFrameLayout) overlayBandH(totalH int) int {
	return totalH - l.headerH - l.footerH
}

func firstActiveOverlay(candidates ...boardOverlayCandidate) string {
	for _, candidate := range candidates {
		if candidate.active() {
			return candidate.view()
		}
	}
	return ""
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
	base, layout := f.renderBase(w, h)
	overlay := f.activeOverlay(w, h, layout.overlayBandH(h))
	if overlay == "" {
		return base
	}
	return composeOverlay(base, overlay, w, layout.headerH, layout.overlayBandH(h))
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

func (f boardViewFrame) renderBase(w, h int) (string, boardFrameLayout) {
	layout := f.renderFrameParts(w)
	base := f.assembleBaseFrame(layout, h)
	return base, layout
}

func (f boardViewFrame) renderFrameParts(w int) boardFrameLayout {
	b := f.b
	header := b.statusPresenter().renderHeaderLayout(w)
	columnsRegion := boardColumnsRegion{}
	columnsView := columnsRegion.renderColumns(b.renderColumnsRegionContext(), w)
	body := columnsView
	if mnemonicView := f.renderMnemonicJump(w); mnemonicView != "" {
		body += "\n" + mnemonicView
	}

	footer := ""
	if f.boardFooterVisible() {
		footer = f.renderStatusBar()
	}
	footerH := 0
	if footer != "" {
		footerH = lipgloss.Height(footer)
	}

	return boardFrameLayout{
		header:  header.view,
		body:    body,
		footer:  footer,
		headerH: header.height,
		footerH: footerH,
	}
}

func (f boardViewFrame) boardFooterVisible() bool {
	return f.b.editor.state == editorNone
}

func (f boardViewFrame) assembleBaseFrame(layout boardFrameLayout, h int) string {
	base := layout.header + "\n" + layout.body
	if pad := h - lipgloss.Height(base) - layout.footerH; pad > 0 {
		base += strings.Repeat("\n", pad)
	}
	if layout.footer != "" {
		base += "\n" + layout.footer
	}
	return base
}

func (f boardViewFrame) renderStatusBar() string {
	b := f.b
	width := b.termWidth
	if width == 0 {
		width = 80
	}

	ctx := ShortcutContext{MnemonicMode: b.mnemonicMode, Zoomed: b.zoom.Active()}
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

func (f boardViewFrame) renderMnemonicJump(width int) string {
	b := f.b
	if !b.mnemonicMode {
		return ""
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(b.palette.BorderActive).
		Width(b.mnemonicInput.Width()+lipgloss.Width(b.mnemonicInput.Prompt)).
		Padding(0, 1)
	return lipgloss.PlaceHorizontal(width, lipgloss.Center, box.Render(b.mnemonicInput.View()))
}

func (f boardViewFrame) renderEditor(frameH int) string {
	if f.b.editor.state == editorNone {
		return ""
	}
	return f.b.editor.viewInFrame(frameH)
}

// activeOverlay returns the single popup to draw over the board, or "" when none
// is open. Priority mirrors the key-routing order in boardInputRouter.
func (f boardViewFrame) activeOverlay(w, h, frameH int) string {
	b := f.b

	if v := firstActiveOverlay(
		overlayCandidate(b.helpMenu.Active, func() string { return b.helpMenu.View(w, h) }),
		overlayCandidate(func() bool { return b.configMenuOpen }, func() string { return RenderConfigCommandsOverlay(configCommandEntries()) }),
	); v != "" {
		return v
	}
	if v := b.dialog.View(); v != "" {
		return v
	}
	// The command menu and script UI are checked before the editor: a line
	// command's menu (and any kbrd.ui.pick/prompt it yields) opens over a
	// still-open editor and must render on top.
	return firstActiveOverlay(
		overlayCandidate(b.customCmds.Active, func() string { return b.customCmds.View(b.termWidth, b.termHeight) }),
		overlayCandidate(b.scriptUI.Active, b.scriptUI.View),
		overlayCandidate(func() bool { return b.editor.state != editorNone }, func() string { return f.renderEditor(frameH) }),
		overlayCandidate(b.peek.Active, func() string { return b.peek.View(w, h) }),
		overlayCandidate(b.switcher.Active, b.switcher.View),
		overlayCandidate(b.search.Active, func() string { return b.search.View(w, h) }),
		overlayCandidate(b.templateMenu.Active, func() string { return b.templateMenu.View(w, h) }),
		overlayCandidate(b.templateFlow.Active, b.templateFlow.View),
		overlayCandidate(b.frontmatterEdit.Active, b.frontmatterEdit.View),
		overlayCandidate(b.git.Active, b.git.View),
		overlayCandidate(b.zellij.Active, b.zellij.View),
	)
}
