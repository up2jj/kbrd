package model

import (
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fsnotify/fsnotify"

	"kbrd/config"
)

// watchStartMsg is emitted once after Init's startup load completes. Columns
// and git stats are already populated; this just publishes the initial refresh
// and arms the watcher loop on the UI goroutine.
type watchStartMsg struct{}

// watchEventMsg carries a single fsnotify event out of the watcher loop. Path
// is ev.Name (empty for watcher errors, which force a full reload). Like
// search's typing events, these are debounced before any disk work runs.
type watchEventMsg struct {
	Path         string
	ReloadConfig bool
}

// watchDebounceMsg fires after the watcher debounce window. The board runs the
// reload only if Seq still matches b.watchSeq (no newer event has arrived).
type watchDebounceMsg struct{ Seq int }

// boardReloadedMsg carries the result of an off-goroutine full board rescan.
// Stale results (Seq != b.watchSeq) are discarded.
type boardReloadedMsg struct {
	Seq     int
	cfg     *config.Config
	columns []*Column
	err     error
}

// columnReloadedMsg carries the result of an off-goroutine single-column
// rescan, used when a change is local to one column. Stale results are
// discarded.
type columnReloadedMsg struct {
	Seq  int
	path string
	col  *Column
}

func (b *Board) watchCmd() tea.Cmd {
	if b.watcher == nil {
		return nil
	}
	return func() tea.Msg {
		for {
			select {
			case ev, ok := <-b.watcher.Events():
				if !ok {
					return nil
				}
				if ignoreWatchEvent(ev) {
					continue
				}
				msg, ok := b.watchEventForPath(ev.Name)
				if !ok {
					continue
				}
				return msg
			case _, ok := <-b.watcher.Errors():
				if !ok {
					return nil
				}
				return watchEventMsg{Path: ""}
			}
		}
	}
}

func ignoreWatchEvent(ev fsnotify.Event) bool {
	// Chmod-only events fire on atime updates (e.g. when git diff reads tracked
	// files). Ignore them — we only care about real content changes.
	if ev.Op == fsnotify.Chmod {
		return true
	}
	// Vim-mode crash-recovery sidecars are written while typing. They are not
	// board content, and treating them as changes causes board_refresh hooks
	// (notably async virtual-column scans) to run on every editor keystroke.
	return strings.HasSuffix(filepath.Base(ev.Name), ".kbrd-swap")
}

// debouncedReload runs when a watcher debounce tick fires. It drops stale ticks
// (a newer event has since bumped watchSeq) and otherwise launches one async
// reload, scoped to a single column when every changed path lives in it.
func (b *Board) debouncedReload(seq int) tea.Cmd {
	if seq != b.watchSeq {
		return nil // stale — a newer event scheduled a later tick
	}
	dirty := b.watchDirty
	reloadConfig := b.watchReloadConfig
	b.watchDirty = nil
	b.watchReloadConfig = false
	b.changes.snapshot(dirty, b.columns)
	if colPath := b.lifecycle().singleDirtyColumn(dirty); colPath != "" {
		return b.reloadColumnCmd(seq, colPath)
	}
	return b.reloadCmd(seq, reloadConfig || b.lifecycle().shouldReloadConfig(dirty))
}

// reloadCmd builds a full board rescan off the UI goroutine. It captures config
// and palette by value so it touches no Board state; the result is applied by
// the boardReloadedMsg handler.
func (b *Board) reloadCmd(seq int, reloadConfig ...bool) tea.Cmd {
	cfg := b.cfg
	currentPalette := b.palette
	shouldReloadConfig := len(reloadConfig) > 0 && reloadConfig[0]
	// Snapshot current items by value on the UI goroutine; the closure only
	// reads it. Preview slices are shared but only ever reassigned (never
	// mutated in place) by NewItem/Refresh, so the concurrent read is safe.
	cache := b.itemsByPath()
	safeMode := b.safeMode
	return func() tea.Msg {
		var reloadedCfg *config.Config
		if shouldReloadConfig {
			next, err := config.Load(cfg.Path)
			if err != nil {
				return boardReloadedMsg{Seq: seq, err: err}
			}
			next.InstanceName = cfg.InstanceName
			if safeMode {
				applySafeMode(&next)
			}
			cfg = next
			reloadedCfg = &next
		}
		palette := currentPalette
		if shouldReloadConfig {
			palette = PaletteFor(cfg.Theme)
		}
		columns, err := buildColumns(cfg, palette, cache)
		if err != nil {
			return nil // leave the board as-is, matching the old silent path
		}
		return boardReloadedMsg{Seq: seq, cfg: reloadedCfg, columns: columns}
	}
}

// reloadColumnCmd rebuilds a single column off the UI goroutine. Git stats are
// board-wide, so they are refreshed too (a single edit can change one file's
// diff). Result is applied by the columnReloadedMsg handler.
func (b *Board) reloadColumnCmd(seq int, colPath string) tea.Cmd {
	cfg := b.cfg
	palette := b.palette
	name := filepath.Base(colPath)
	cache := b.itemsByPath()
	return func() tea.Msg {
		col := buildColumn(colPath, name, cfg, palette, cache)
		if col == nil {
			return nil
		}
		return columnReloadedMsg{Seq: seq, path: colPath, col: col}
	}
}
