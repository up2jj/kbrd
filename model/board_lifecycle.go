package model

import (
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"kbrd/config"
	"kbrd/events"
)

type boardLifecycle struct {
	board *Board
}

func (b *Board) lifecycle() boardLifecycle {
	return boardLifecycle{board: b}
}

// singleDirtyColumn returns the path of the column that contains every changed
// path, or "" when the change spans multiple columns, touches the board root
// (a column added/removed), or came from a watcher error. "" means full reload.
func (l boardLifecycle) singleDirtyColumn(dirty map[string]struct{}) string {
	b := l.board
	if len(dirty) == 0 {
		return ""
	}
	match := ""
	for p := range dirty {
		if p == "" {
			return "" // watcher error → full reload
		}
		dir := filepath.Dir(p)
		found := ""
		for _, col := range b.columns {
			if samePath(col.Path, dir) {
				found = col.Path
				break
			}
		}
		if found == "" {
			return "" // not inside a known column (root-level change)
		}
		switch {
		case match == "":
			match = found
		case !samePath(match, found):
			return "" // spans multiple columns
		}
	}
	return match
}

// shouldReloadConfig reports whether a full reload should also re-read TOML.
// The trigger is intentionally filesystem-shaped, not git-shaped: any process
// that writes kbrd.toml (an editor, sync, rsync, unzip, etc.) gets the same path.
func (l boardLifecycle) shouldReloadConfig(dirty map[string]struct{}) bool {
	b := l.board
	if len(dirty) == 0 {
		return false
	}
	for p := range dirty {
		if p == "" {
			return true
		}
		if samePath(p, filepath.Join(b.cfg.Path, config.FolderConfigFile)) {
			return true
		}
	}
	return false
}

// selectionByPath captures each column's currently selected item name keyed by
// column path, so a reload can restore the cursor after swapping in fresh
// columns (whose list index defaults to 0). Columns with no selection are
// omitted.
func (l boardLifecycle) selectionByPath() map[string]string {
	b := l.board
	sel := make(map[string]string, len(b.columns))
	for _, col := range b.columns {
		if col.HasSelectedItem() {
			sel[col.Path] = col.SelectedItem().Name
		}
	}
	return sel
}

// applyReloadedColumns swaps in freshly built columns on the UI goroutine,
// re-applying height and palette, restoring each column's selected item, and
// clamping the column selection.
func (l boardLifecycle) applyReloadedColumns(columns []*Column) {
	b := l.board
	prevSel := l.selectionByPath()
	for _, col := range columns {
		if b.visibleHeight > 0 {
			col.SetHeight(b.visibleHeight)
		}
		col.palette = b.palette
		if name, ok := prevSel[col.Path]; ok {
			col.SelectByName(name)
		}
	}
	b.columns = columns
	b.appendVirtualColumns()
	if len(b.columns) > 0 && b.selectedCol >= len(b.columns) {
		b.selectedCol = len(b.columns) - 1
	}
}

func (l boardLifecycle) applyReloadedConfig(cfg config.Config) {
	b := l.board
	old := b.cfg
	b.cfg = cfg
	b.theme = cfg.Theme
	if old.NotifyBackend != cfg.NotifyBackend {
		b.notifier = NewNotifier(cfg.NotifyBackend)
		b.templateExec.notifier = b.notifier
	}
	b.git.SetConfig(cfg)
	b.git.SetNotifier(gitNotifier{b.notifier})
	b.applyPalette()
	if b.editor != nil && b.editor.state == editorNone && old.Editor.Vim != cfg.Editor.Vim {
		b.resetEditor()
	}
}

func (l boardLifecycle) DrainPostUpdate(cmd tea.Cmd) tea.Cmd {
	b := l.board
	// Selection events may fire hooks that schedule timers or async work;
	// drain after emitSelectionChanges so newly-scheduled tea.Cmds don't get
	// stranded in the Host's pending queues.
	if tcmd := b.collectTimerCmds(); tcmd != nil {
		cmd = batchCmd(cmd, tcmd)
	}
	if acmd := b.collectAsyncCmds(); acmd != nil {
		cmd = batchCmd(cmd, acmd)
	}
	if hcmd := (boardHooks{board: b}).collectCmd(); hcmd != nil {
		cmd = batchCmd(cmd, hcmd)
	}
	if scmd := b.collectStatusCmd(); scmd != nil {
		cmd = batchCmd(cmd, scmd)
	}
	if ecmd := b.collectEditorOpenCmd(); ecmd != nil {
		cmd = batchCmd(cmd, ecmd)
	}
	// Re-apply any column_items transform that was skipped while a script was
	// running. The script has finished by the time the wrapper runs.
	b.drainColumnTransform()
	return cmd
}

func (l boardLifecycle) HandleWatchStart() (tea.Model, tea.Cmd) {
	b := l.board
	// board_load fires once, after the initial columns are populated and the
	// script host is subscribed. Publishing here keeps Lua single-threaded.
	b.bus.Publish(events.BoardLoad{})
	b.bus.Publish(events.BoardRefresh{Reason: "startup"})
	// Cold-load transform: columns were built in Init's startup goroutine
	// (no VM access there); apply script order now on the UI goroutine.
	b.applyColumnTransforms()
	if b.cfg.GitSyncOnStartup {
		return b, tea.Batch(b.watchCmd(), b.git.SyncOnce())
	}
	return b, b.watchCmd()
}

func (l boardLifecycle) HandleWatchEvent(msg watchEventMsg) (tea.Model, tea.Cmd) {
	b := l.board
	// Coalesce a storm of events into one reload. Only the final debounce tick
	// survives the Seq guard; the watcher is re-armed immediately.
	b.watchSeq++
	if b.watchDirty == nil {
		b.watchDirty = map[string]struct{}{}
	}
	b.watchDirty[msg.Path] = struct{}{}
	b.watchReloadConfig = b.watchReloadConfig || msg.ReloadConfig
	seq := b.watchSeq
	debounce := tea.Tick(150*time.Millisecond, func(time.Time) tea.Msg {
		return watchDebounceMsg{Seq: seq}
	})
	return b, tea.Batch(debounce, b.watchCmd())
}

func (l boardLifecycle) HandleWatchDebounce(msg watchDebounceMsg) (tea.Model, tea.Cmd) {
	b := l.board
	return b, b.debouncedReload(msg.Seq)
}

func (l boardLifecycle) HandleBoardReloaded(msg boardReloadedMsg) (tea.Model, tea.Cmd) {
	b := l.board
	if msg.Seq != b.watchSeq {
		return b, nil
	}
	if msg.err != nil {
		return b, b.notifier.Error("config reload skipped: " + msg.err.Error())
	}
	if msg.cfg != nil {
		l.applyReloadedConfig(*msg.cfg)
	}
	l.applyReloadedColumns(msg.columns)
	b.applyColumnTransforms()
	b.publishItemChanges()
	b.bus.Publish(events.BoardRefresh{Reason: "watcher"})
	return b, b.git.RefreshStats()
}

func (l boardLifecycle) HandleColumnReloaded(msg columnReloadedMsg) (tea.Model, tea.Cmd) {
	b := l.board
	if msg.Seq != b.watchSeq {
		return b, nil
	}
	idx := -1
	for i, col := range b.columns {
		if samePath(col.Path, msg.path) {
			idx = i
			break
		}
	}
	if idx < 0 {
		return b, b.reloadCmd(b.watchSeq)
	}
	prevName := l.selectedItemName(b.columns[idx])
	if b.visibleHeight > 0 {
		msg.col.SetHeight(b.visibleHeight)
	}
	msg.col.palette = b.palette
	if prevName != "" {
		msg.col.SelectByName(prevName)
	}
	b.columns[idx] = msg.col
	b.applyColumnTransform(msg.col)
	b.publishItemChanges()
	b.bus.Publish(events.BoardRefresh{Reason: "watcher"})
	return b, b.git.RefreshStats()
}

func (l boardLifecycle) selectedItemName(col *Column) string {
	if col.HasSelectedItem() {
		return col.SelectedItem().Name
	}
	return ""
}
