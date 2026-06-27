package model

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
)

type boardStatusPresenter struct {
	b *Board
}

func (p boardStatusPresenter) renderLogo() string {
	b := p.b
	name := lipgloss.NewStyle().
		Foreground(b.palette.Primary).
		Bold(true).
		Render("kbrd")
	version := lipgloss.NewStyle().
		Foreground(b.palette.FgSubtle).
		Italic(true).
		Render(Version)
	board := lipgloss.NewStyle().
		Foreground(b.palette.FgMuted).
		Render(b.boardLabel())
	// ⌨️ is a wide (2-cell) emoji; keep it as a literal prefix and let lipgloss
	// measure widths downstream rather than counting runes by hand.
	return "⌨️  " + name + "  " + version + "  " + board
}

func (p boardStatusPresenter) renderHeader(width int) string {
	b := p.b
	p.updateBuiltinCells()
	logo := p.renderLogo()
	header := logo
	if !b.cells.Empty() {
		avail := width - lipgloss.Width(logo) - 2
		if strip := b.cells.render(avail); lipgloss.Width(strip) > 0 {
			pad := max(width-lipgloss.Width(logo)-lipgloss.Width(strip), 1)
			header = logo + strings.Repeat(" ", pad) + strip
		}
	}
	// Tint the whole header line with a subtle surface background, padded to the
	// full terminal width, and underline it with a muted rule to separate the
	// header from the columns. Chips with their own bg keep it; bare text and
	// the gap inherit the tint. The rule adds a row, which lipgloss.Height picks
	// up so logoHeight (and thus mouse hit-testing) stays correct.
	header = lipgloss.NewStyle().
		Background(b.palette.BgCodeInline).
		Width(width).
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		BorderForeground(b.palette.BorderMuted).
		Render(header)
	b.presenter.logoHeight = lipgloss.Height(header)
	return header
}

// updateBuiltinCells recomputes the internal (negative-id) cells from current
// board state on every render. They are cheap to derive and event-free, so
// deriving them here keeps the strip always-accurate without any host ticker.
// Script-set cells (positive ids) are untouched. Ids are ordered so the
// persistent metrics (count, git) sit to the right and the transient activity
// indicators (sync, jobs, kbrd.status) flow in to their left as they appear.
func (p boardStatusPresenter) updateBuiltinCells() {
	b := p.b
	// Sync indicator (id -5): transient spinner while reconciling, else the
	// persistent remote-sync status. The mapping lives in syncCell.
	editorActive := b.editor != nil && b.editor.state != editorNone
	if cell, ok := syncCell(b.git.SyncState(), b.git.DirtyCount(), b.shuttingDown, editorActive, b.cfg.GitAutoCommit, b.palette); ok {
		b.cells.SetInternal(cell)
	} else {
		b.cells.Clear(syncCellID)
	}

	if b.asyncInflight > 0 {
		label := "⟳ 1 running"
		if b.asyncInflight > 1 {
			label = "⟳ " + strconv.Itoa(b.asyncInflight) + " running"
		}
		p.setActivityCell(-4, label)
	} else {
		b.cells.Clear(-4)
	}

	if n := b.templateExec.Inflight(); n > 0 {
		label := "✦ generating"
		if n > 1 {
			label = "✦ " + strconv.Itoa(n) + " generating"
		}
		p.setActivityCell(-8, label)
	} else {
		b.cells.Clear(-8)
	}

	if b.hooks.busy() {
		label := "⚙ hooks"
		if n := b.hooks.pending(); n > 1 {
			label = "⚙ hooks " + strconv.Itoa(n)
		}
		p.setActivityCell(-6, label)
	} else {
		b.cells.Clear(-6)
	}

	if b.scriptStatus != "" {
		b.cells.SetInternal(Cell{ID: -3, Text: b.scriptStatus, FG: string(b.palette.FgMuted)})
	} else {
		b.cells.Clear(-3)
	}

	// Persistent MCP indicator: filled+green when bound, danger when requested
	// but the bind failed (e.g. the port is already in use), hollow+muted when
	// off. Leftmost (most negative) id so it survives header truncation alongside
	// the other built-ins.
	switch b.mcpStatus {
	case MCPRunning:
		b.cells.SetInternal(Cell{ID: -7, Text: "◆ mcp", FG: string(b.palette.Success)})
	case MCPFailed:
		b.cells.SetInternal(Cell{ID: -7, Text: "✕ mcp", FG: string(b.palette.Danger)})
	default:
		b.cells.SetInternal(Cell{ID: -7, Text: "◇ mcp", FG: string(b.palette.FgMuted)})
	}

	total := 0
	for _, c := range b.columns {
		total += c.TotalCount()
	}
	b.cells.SetInternal(Cell{
		ID:   -2,
		Text: strconv.Itoa(total) + " items",
		FG:   string(b.palette.FgMuted),
	})

	if b.git.RepoRoot() != "" {
		if dirty := b.git.DirtyCount(); dirty > 0 {
			b.cells.SetInternal(Cell{
				ID:   -1,
				Text: "● " + strconv.Itoa(dirty),
				FG:   string(b.palette.Warning),
			})
		} else {
			b.cells.SetInternal(Cell{
				ID:   -1,
				Text: "✓ clean",
				FG:   string(b.palette.Success),
			})
		}
	} else {
		b.cells.Clear(-1)
	}
}

// setActivityCell sets a transient activity indicator cell in the accent color.
func (p boardStatusPresenter) setActivityCell(id int, text string) {
	b := p.b
	b.cells.SetInternal(Cell{ID: id, Text: text, FG: string(b.palette.AccentSoft)})
}

func (b *Board) statusPresenter() boardStatusPresenter {
	return boardStatusPresenter{b: b}
}
