package model

import (
	tea "charm.land/bubbletea/v2"

	"kbrd/harpoon"
)

func (m *HarpoonMenu) reconcileMoves(boardPath string, cols []*Column) error {
	paths := make([]string, 0)
	for _, col := range cols {
		if col.Virtual {
			continue
		}
		for _, item := range col.Items {
			if !item.Virtual {
				paths = append(paths, item.FullPath)
			}
		}
	}

	store, err := harpoon.Load()
	if err != nil {
		return err
	}
	if !store.Reconcile(boardPath, paths) {
		return nil
	}
	if err := store.Save(); err != nil {
		return err
	}
	m.syncSlots(boardPath, store.ForBoard(boardPath))
	return nil
}

func (b *Board) reconcileHarpoonMoves() tea.Cmd {
	if err := b.harpoon.reconcileMoves(b.cfg.Path, b.columns); err != nil {
		return b.notifier.ErrorCause("failed to update moved harpoon slots", err)
	}
	return nil
}
