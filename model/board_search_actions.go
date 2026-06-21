package model

import (
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"kbrd/recents"
)

type boardSearchActions struct {
	b *Board
}

// openSearch loads the recents store and opens the global search dialog with
// every recent board plus the currently open board as search roots.
func (s boardSearchActions) openSearch() tea.Cmd {
	b := s.b
	store, err := recents.Load()
	if err != nil {
		return b.notifier.Send("failed to load recents: "+err.Error(), notifyError)
	}
	if store.Prune() > 0 {
		_ = store.Save()
	}

	activeAbs, _ := filepath.Abs(b.cfg.Path)
	roots := buildSearchRoots(activeAbs, b.cfg.BoardName, store.Entries)
	b.search.Open(roots, b.palette)
	return nil
}

// activateFile switches to boardPath (if not already active) and selects the
// column/item containing filePath. Used by the global search dialog.
func (s boardSearchActions) activateFile(boardPath, filePath string) (tea.Model, tea.Cmd) {
	b := s.b
	var cmd tea.Cmd
	if !samePath(boardPath, b.cfg.Path) {
		c, err := b.session().loadBoard(boardPath)
		if err != nil {
			return b, b.notifier.Send(err.Error(), notifyError)
		}
		cmd = c
	}

	if colIdx, itemIdx, ok := locateFile(b.columns, filePath); ok {
		b.selectedCol = colIdx
		b.columns[colIdx].SelectIndex(itemIdx)
		return b, cmd
	}
	if cmd != nil {
		return b, tea.Batch(cmd, b.notifier.Send("opened board; file not in a column", notifySuccess))
	}
	return b, b.notifier.Send("file not in a column", notifyError)
}

func (b *Board) searchActions() boardSearchActions {
	return boardSearchActions{b: b}
}
