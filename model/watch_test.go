package model

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fsnotify/fsnotify"

	"kbrd/config"
)

// card returns a synthetic file path inside the given column directory.
func card(colPath, name string) string {
	return filepath.Join(colPath, name+".md")
}

func TestBoard_SingleDirtyColumn(t *testing.T) {
	b := boardWithNCols(t, 3, 3)
	c0, c1 := b.columns[0].Path, b.columns[1].Path
	root := b.cfg.Path

	tests := []struct {
		name  string
		dirty []string
		want  string // column path, or "" for full reload
	}{
		{"empty set", nil, ""},
		{"one file in one column", []string{card(c0, "a")}, c0},
		{"two files same column", []string{card(c0, "a"), card(c0, "b")}, c0},
		{"files spanning columns", []string{card(c0, "a"), card(c1, "b")}, ""},
		{"watcher error (empty path)", []string{""}, ""},
		{"root-level change", []string{filepath.Join(root, "newcol")}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dirty := map[string]struct{}{}
			for _, p := range tt.dirty {
				dirty[p] = struct{}{}
			}
			got := b.lifecycle().singleDirtyColumn(dirty)
			if !samePath(got, tt.want) && got != tt.want {
				t.Errorf("singleDirtyColumn = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIgnoreWatchEvent(t *testing.T) {
	if !ignoreWatchEvent(fsnotify.Event{Name: "/board/1. todo/.note.md.kbrd-swap", Op: fsnotify.Write}) {
		t.Fatal("swap sidecar write should be ignored")
	}
	if !ignoreWatchEvent(fsnotify.Event{Name: "/board/1. todo/note.md", Op: fsnotify.Chmod}) {
		t.Fatal("chmod-only event should be ignored")
	}
	if ignoreWatchEvent(fsnotify.Event{Name: "/board/1. todo/note.md", Op: fsnotify.Write}) {
		t.Fatal("normal markdown write should not be ignored")
	}
}

func TestBoard_DebouncedReload_DropsStaleTick(t *testing.T) {
	b := boardWithNCols(t, 2, 2)
	b.watchSeq = 5
	b.watchDirty = map[string]struct{}{card(b.columns[0].Path, "a"): {}}

	if cmd := b.debouncedReload(3); cmd != nil {
		t.Fatal("stale debounce tick (seq 3 != watchSeq 5) should produce no reload")
	}
	// The dirty set must survive a dropped stale tick so the live tick can use it.
	if len(b.watchDirty) != 1 {
		t.Fatalf("watchDirty cleared by stale tick: %v", b.watchDirty)
	}

	if cmd := b.debouncedReload(5); cmd == nil {
		t.Fatal("live debounce tick (seq 5 == watchSeq 5) should produce a reload")
	}
	if b.watchDirty != nil {
		t.Errorf("watchDirty not cleared after live reload: %v", b.watchDirty)
	}
}

func TestBoard_WatchEvent_CoalescesStorm(t *testing.T) {
	b := boardWithNCols(t, 2, 2)
	col := b.columns[0].Path

	// Simulate a storm: each event bumps watchSeq and records its path.
	for range 10 {
		b.updateInner(watchEventMsg{Path: card(col, "a")})
	}
	if b.watchSeq != 10 {
		t.Fatalf("watchSeq = %d, want 10 (one bump per event)", b.watchSeq)
	}

	// Every tick scheduled before the final event is now stale and drops.
	for seq := 1; seq < 10; seq++ {
		if cmd := b.debouncedReload(seq); cmd != nil {
			t.Errorf("debounce tick seq %d should be stale", seq)
		}
	}
	// Only the final tick survives — one coalesced reload for the whole storm.
	if cmd := b.debouncedReload(10); cmd == nil {
		t.Fatal("final debounce tick should produce exactly one reload")
	}
}

func TestBoard_WatchEvent_RootChangeForcesFullReload(t *testing.T) {
	b := boardWithNCols(t, 2, 2)

	// A new column dir at the board root cannot be an incremental reload.
	b.updateInner(watchEventMsg{Path: filepath.Join(b.cfg.Path, "3. NEW")})
	cmd := b.debouncedReload(b.watchSeq)
	if cmd == nil {
		t.Fatal("expected a reload cmd")
	}
	msg := cmd()
	if _, ok := msg.(boardReloadedMsg); !ok {
		t.Fatalf("root-level change should yield boardReloadedMsg, got %T", msg)
	}
}

func TestBoard_WatchEvent_ConfigChangeReloadsTOML(t *testing.T) {
	b := boardWithNCols(t, 2, 2)
	b.cfg.InstanceName = "local"
	b.safeMode = true
	cfgPath := filepath.Join(b.cfg.Path, config.FolderConfigFile)
	if err := os.WriteFile(cfgPath, []byte(`
[display]
column_width = 41
preview_lines = 7
theme = "light"

[scripting]
enabled = true

[hooks]
enabled = true

[template]
exec = true
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	b.updateInner(watchEventMsg{Path: cfgPath})
	cmd := b.debouncedReload(b.watchSeq)
	if cmd == nil {
		t.Fatal("expected a reload cmd")
	}
	msg := cmd()
	if _, ok := msg.(boardReloadedMsg); !ok {
		t.Fatalf("config change should yield boardReloadedMsg, got %T", msg)
	}
	b.updateInner(msg)

	if b.cfg.ColumnWidth != 41 {
		t.Fatalf("column_width: got %d want 41", b.cfg.ColumnWidth)
	}
	if b.cfg.PreviewLines != 7 {
		t.Fatalf("preview_lines: got %d want 7", b.cfg.PreviewLines)
	}
	if b.cfg.Theme != "light" || b.theme != "light" {
		t.Fatalf("theme not applied: cfg=%q board=%q", b.cfg.Theme, b.theme)
	}
	if b.cfg.InstanceName != "local" {
		t.Fatalf("instance name: got %q want local", b.cfg.InstanceName)
	}
	if b.cfg.Scripting.Enabled || b.cfg.Hooks.Enabled || b.cfg.Template.Exec {
		t.Fatalf("safe mode overrides not preserved: scripting=%v hooks=%v template=%v",
			b.cfg.Scripting.Enabled, b.cfg.Hooks.Enabled, b.cfg.Template.Exec)
	}
}

func TestBoard_WatchEvent_InvalidConfigKeepsCurrentConfig(t *testing.T) {
	b := boardWithNCols(t, 2, 2)
	cfgPath := filepath.Join(b.cfg.Path, config.FolderConfigFile)
	if err := os.WriteFile(cfgPath, []byte("[display]\ncolumn_width = "), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	b.updateInner(watchEventMsg{Path: cfgPath})
	cmd := b.debouncedReload(b.watchSeq)
	if cmd == nil {
		t.Fatal("expected a reload cmd")
	}
	b.updateInner(cmd())

	if b.cfg.ColumnWidth != 20 {
		t.Fatalf("invalid config mutated column_width: got %d want 20", b.cfg.ColumnWidth)
	}
}

func TestBoard_WatchEvent_SingleColumnIsIncremental(t *testing.T) {
	b := boardWithNCols(t, 2, 2)
	col := b.columns[0].Path

	b.updateInner(watchEventMsg{Path: card(col, "a")})
	cmd := b.debouncedReload(b.watchSeq)
	if cmd == nil {
		t.Fatal("expected a reload cmd")
	}
	msg := cmd()
	m, ok := msg.(columnReloadedMsg)
	if !ok {
		t.Fatalf("single-column change should yield columnReloadedMsg, got %T", msg)
	}
	if !samePath(m.path, col) {
		t.Errorf("reloaded column = %q, want %q", m.path, col)
	}
}

func TestBoard_ReloadedMsg_DropsStale(t *testing.T) {
	b := boardWithNCols(t, 2, 2)
	b.watchSeq = 7
	before := b.columns

	// A result tagged with an older generation must not overwrite columns.
	b.updateInner(boardReloadedMsg{Seq: 4, columns: nil})
	if b.columns == nil || len(b.columns) != len(before) {
		t.Errorf("stale boardReloadedMsg mutated columns: %v", b.columns)
	}
}
