package model

import (
	"fmt"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"kbrd/board"
	"kbrd/config"
	"kbrd/events"
	kbrdfs "kbrd/fs"
	"kbrd/recents"
)

type boardSession struct {
	b *Board
}

func (s boardSession) openSwitcher() tea.Cmd {
	b := s.b
	store, err := recents.Load()
	if err != nil {
		return b.notifier.Send("failed to load recents: "+err.Error(), notifyError)
	}
	removed := store.Prune()
	if removed > 0 {
		_ = store.Save()
	}
	activeAbs, _ := filepath.Abs(b.cfg.Path)
	b.switcher.Open(store.Entries, activeAbs)
	return nil
}

func (s boardSession) handlePinBoard(msg pinBoardMsg) (tea.Model, tea.Cmd) {
	b := s.b
	store, err := recents.Load()
	if err != nil {
		return b, b.notifier.Send("failed to load recents: "+err.Error(), notifyError)
	}
	store.SetPinned(msg.Path, msg.Name, msg.Pinned)
	if err := store.Save(); err != nil {
		return b, b.notifier.Send("failed to save recents: "+err.Error(), notifyError)
	}
	s.reopenSwitcher(store.Entries)
	return b, nil
}

func (s boardSession) handleRemoveBoard(msg removeBoardMsg) (tea.Model, tea.Cmd) {
	b := s.b
	store, err := recents.Load()
	if err != nil {
		return b, b.notifier.Send("failed to load recents: "+err.Error(), notifyError)
	}
	store.Remove(msg.Path)
	if err := store.Save(); err != nil {
		return b, b.notifier.Send("failed to save recents: "+err.Error(), notifyError)
	}
	s.reopenSwitcher(store.Entries)
	return b, nil
}

func (s boardSession) handleSwitchBoard(msg switchBoardMsg) (tea.Model, tea.Cmd) {
	b := s.b
	cmd, err := s.loadBoard(msg.Path)
	if err != nil {
		return b, b.notifier.Send(err.Error(), notifyError)
	}
	return b, cmd
}

func (s boardSession) reopenSwitcher(entries []recents.Entry) {
	b := s.b
	activeAbs, _ := filepath.Abs(b.cfg.Path)
	b.switcher.Open(entries, activeAbs)
}

// loadBoard switches the board to path: closes the old watcher, reloads config,
// columns, scripting, git state and a fresh watcher, and records the board in
// recents. selectedCol is reset to 0. Returns the watch+notify command. Errors
// are returned without sending a notification so callers can phrase them.
func (s boardSession) loadBoard(path string) (tea.Cmd, error) {
	b := s.b
	newCfg, err := config.Load(path)
	if err != nil {
		return nil, fmt.Errorf("failed to load board: %w", err)
	}

	if b.watcher != nil {
		_ = b.watcher.Close()
		b.watcher = nil
	}

	b.cfg = newCfg
	b.theme = newCfg.Theme
	b.initGit()
	b.applyPalette()
	b.selectedCol = 0
	// Virtual columns belong to the previous board's (now-closed) script host;
	// drop them so they don't leak onto the new board before its board_load runs.
	b.virtualCols = nil
	b.initScripting()
	b.loadCommands()
	b.initHooks()

	if err := b.loadColumns(); err != nil {
		return nil, fmt.Errorf("failed to load columns: %w", err)
	}
	b.applyPalette()
	b.git.Detect()
	// Re-fire board_load on the new board so its init script can repopulate any
	// virtual columns (runs on the UI goroutine, host already subscribed).
	b.bus.Publish(events.BoardLoad{})
	b.applyColumnTransforms()

	if paths, err := board.DiscoverPaths(b.cfg.Path); err == nil {
		if w, err := kbrdfs.NewWatcher(paths); err == nil {
			b.watcher = w
		}
	}

	store, _ := recents.Load()
	store.Touch(b.cfg.Path, b.cfg.BoardName)
	_ = store.Save()

	label := b.cfg.Path
	if b.cfg.BoardName != "" {
		label = "[" + b.cfg.BoardName + "] " + b.cfg.Path
	}
	return tea.Batch(b.watchCmd(), b.notifier.Send("switched to "+label, notifySuccess)), nil
}

func (b *Board) session() boardSession {
	return boardSession{b: b}
}
