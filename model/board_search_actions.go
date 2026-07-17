package model

import (
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"kbrd/events"
	"kbrd/recents"
	searchbackend "kbrd/search"
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
		return b.notifier.ErrorCause("failed to load recents", err)
	}
	if store.Prune() > 0 {
		_ = store.Save()
	}

	activeAbs, _ := filepath.Abs(b.cfg.Path)
	roots := buildSearchRoots(activeAbs, b.cfg.BoardName, store.Entries)
	b.search.Open(roots, b.virtualSearchItems(activeAbs), b.palette)
	return nil
}

func (b *Board) virtualSearchItems(boardPath string) []searchbackend.VirtualItem {
	var items []searchbackend.VirtualItem
	for _, col := range b.virtualCols {
		for _, item := range col.Items {
			if item.Separator {
				continue
			}
			items = append(items, searchbackend.VirtualItem{
				BoardPath: boardPath,
				BoardName: b.cfg.BoardName,
				Column:    col.Name,
				VID:       col.VID,
				ID:        item.Name,
				Title:     item.Title,
				Preview:   append([]string(nil), item.Preview...),
				Meta:      item.Meta,
				FilePath:  item.FullPath,
			})
		}
	}
	return items
}

// activateFile switches to boardPath (if not already active) and selects the
// column/item containing filePath. Used by the global search dialog.
func (s boardSearchActions) activateFile(boardPath, filePath string) (tea.Model, tea.Cmd) {
	b := s.b
	var cmd tea.Cmd
	if !samePath(boardPath, b.cfg.Path) {
		c, err := b.session().loadBoard(boardPath)
		if err != nil {
			return b, b.notifier.ErrorCause("", err)
		}
		cmd = c
	}

	if colIdx, itemIdx, ok := locateVisibleSearchFile(b.columns, b.selectedCol, filePath); ok {
		b.selectedCol = colIdx
		b.columns[colIdx].SelectIndex(itemIdx)
		return b, cmd
	}
	if _, _, ok := locateVirtualSearchFile(b.virtualCols, -1, filePath); ok {
		if err := b.showAllColumnsByKind(events.ColumnKindVirtual); err == nil {
			if colIdx, itemIdx, found := locateVisibleSearchFile(b.columns, b.selectedCol, filePath); found {
				b.selectedCol = colIdx
				b.columns[colIdx].SelectIndex(itemIdx)
				return b, cmd
			}
		}
	}
	if colIdx, _, ok := locateFile(b.allFilesystemColumns(), filePath); ok {
		col := b.allFilesystemColumns()[colIdx]
		if err := b.showColumn(col.Name); err == nil {
			if visibleCol, itemIdx, found := locateFile(b.columns, filePath); found {
				b.selectedCol = visibleCol
				b.columns[visibleCol].SelectIndex(itemIdx)
				return b, cmd
			}
		}
	}
	if cmd != nil {
		return b, tea.Batch(cmd, b.notifier.Success("opened board; file not in a column"))
	}
	return b, b.notifier.Error("file not in a column")
}

func (s boardSearchActions) activateResult(msg searchSelectMsg) (tea.Model, tea.Cmd) {
	if msg.FilePath != "" {
		return s.activateFile(msg.BoardPath, msg.FilePath)
	}
	b := s.b
	if !samePath(msg.BoardPath, b.cfg.Path) {
		return b, b.notifier.Error("virtual search result is no longer active")
	}
	if colIdx, itemIdx, ok := locateVirtualSearchItem(b.columns, msg.VirtualVID, msg.VirtualItem); ok {
		b.selectedCol = colIdx
		b.columns[colIdx].SelectIndex(itemIdx)
		return b, nil
	}
	if err := b.showAllColumnsByKind(events.ColumnKindVirtual); err == nil {
		if colIdx, itemIdx, ok := locateVirtualSearchItem(b.columns, msg.VirtualVID, msg.VirtualItem); ok {
			b.selectedCol = colIdx
			b.columns[colIdx].SelectIndex(itemIdx)
			return b, nil
		}
	}
	return b, b.notifier.Error("virtual search result is no longer available")
}

func locateVisibleSearchFile(columns []*Column, selected int, filePath string) (colIdx, itemIdx int, ok bool) {
	if colIdx, itemIdx, ok = locateVirtualSearchFile(columns, selected, filePath); ok {
		return colIdx, itemIdx, true
	}
	return locateFile(columns, filePath)
}

func locateVirtualSearchFile(columns []*Column, selected int, filePath string) (colIdx, itemIdx int, ok bool) {
	if selected >= 0 && selected < len(columns) && columns[selected].Virtual {
		for i := range columns[selected].Items {
			if columns[selected].Items[i].FullPath != "" && samePath(columns[selected].Items[i].FullPath, filePath) {
				return selected, i, true
			}
		}
	}
	for ci, col := range columns {
		if !col.Virtual || ci == selected {
			continue
		}
		for ii := range col.Items {
			if col.Items[ii].FullPath != "" && samePath(col.Items[ii].FullPath, filePath) {
				return ci, ii, true
			}
		}
	}
	return 0, 0, false
}

func locateVirtualSearchItem(columns []*Column, vid, itemID string) (colIdx, itemIdx int, ok bool) {
	for ci, col := range columns {
		if !col.Virtual || col.VID != vid {
			continue
		}
		if ii, found := col.IndexByName(itemID); found {
			return ci, ii, true
		}
	}
	return 0, 0, false
}

func (b *Board) searchActions() boardSearchActions {
	return boardSearchActions{b: b}
}
