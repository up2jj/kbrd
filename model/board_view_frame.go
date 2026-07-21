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

func (l boardFrameLayout) overlayBandH(totalH int) int {
	return totalH - l.headerH - l.footerH
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
	if layer := b.activeModalLayer(); layer != nil {
		if overlay := layer.view(w, h, h); overlay != "" {
			return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, overlay)
		}
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

	ctx := ShortcutContext{MnemonicMode: b.mnemonic.active, Zoomed: b.zoom.Active()}
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
	if !b.mnemonic.active {
		return ""
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(b.palette.BorderActive).
		Width(b.mnemonic.input.Width()+lipgloss.Width(b.mnemonic.input.Prompt)).
		Padding(0, 1)
	return lipgloss.PlaceHorizontal(width, lipgloss.Center, box.Render(b.mnemonic.input.View()))
}

// activeOverlay returns the single popup to draw over the board, or "" when none
// is open. Priority is defined once by Board.modalLayers.
func (f boardViewFrame) activeOverlay(w, h, frameH int) string {
	if layer := f.b.activeModalLayer(); layer != nil {
		return layer.view(w, h, frameH)
	}
	return ""
}
