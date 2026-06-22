package model

import (
	"testing"
	"time"

	"kbrd/git"
)

func TestSyncCell(t *testing.T) {
	p := DarkPalette()
	now := time.Now()

	tests := []struct {
		name         string
		ss           git.SyncStatus
		dirty        int
		shuttingDown bool
		editorActive bool
		autoCommit   bool
		wantShow     bool
		wantText     string
		wantFG       string
	}{
		{
			name:     "no remote hides the cell",
			ss:       git.SyncStatus{HasRemote: false},
			wantShow: false,
		},
		{
			name:         "shutdown wins over everything",
			ss:           git.SyncStatus{HasRemote: true, Syncing: true},
			shuttingDown: true,
			wantShow:     true,
			wantText:     "⟳ finishing sync…",
			wantFG:       string(p.AccentSoft),
		},
		{
			name:     "syncing spinner",
			ss:       git.SyncStatus{HasRemote: true, Syncing: true},
			wantShow: true,
			wantText: "⟳ syncing",
			wantFG:   string(p.AccentSoft),
		},
		{
			name:     "failure is danger",
			ss:       git.SyncStatus{HasRemote: true, Failed: true},
			wantShow: true,
			wantText: "✕ sync",
			wantFG:   string(p.Danger),
		},
		{
			name:         "failure wins over editor pause",
			ss:           git.SyncStatus{HasRemote: true, Failed: true},
			editorActive: true,
			wantShow:     true,
			wantText:     "✕ sync",
			wantFG:       string(p.Danger),
		},
		{
			name:     "conflicts pluralize and warn",
			ss:       git.SyncStatus{HasRemote: true, Conflicts: 2},
			wantShow: true,
			wantText: "⚠ 2 conflicts",
			wantFG:   string(p.Warning),
		},
		{
			name:     "single conflict is singular",
			ss:       git.SyncStatus{HasRemote: true, Conflicts: 1},
			wantShow: true,
			wantText: "⚠ 1 conflict",
		},
		{
			name:         "active editor explains auto-sync pause",
			ss:           git.SyncStatus{HasRemote: true, LastSync: now},
			editorActive: true,
			wantShow:     true,
			wantText:     "⇅ sync paused",
			wantFG:       string(p.FgMuted),
		},
		{
			name:         "editor pause wins over dirty hint",
			ss:           git.SyncStatus{HasRemote: true, LastSync: now},
			dirty:        3,
			editorActive: true,
			wantShow:     true,
			wantText:     "⇅ sync paused",
		},
		{
			name:     "dirty tree explains the pause",
			ss:       git.SyncStatus{HasRemote: true, LastSync: now},
			dirty:    3,
			wantShow: true,
			wantText: "⇅ commit to sync",
		},
		{
			name:       "auto_commit suppresses the dirty hint",
			ss:         git.SyncStatus{HasRemote: true, LastSync: now},
			dirty:      3,
			autoCommit: true,
			wantShow:   true,
			wantText:   "⇅ synced just now",
		},
		{
			name:     "clean and synced shows relative time",
			ss:       git.SyncStatus{HasRemote: true, LastSync: now},
			wantShow: true,
			wantText: "⇅ synced just now",
		},
		{
			name:     "remote but never synced",
			ss:       git.SyncStatus{HasRemote: true},
			wantShow: true,
			wantText: "⇅ sync",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cell, ok := syncCell(tc.ss, tc.dirty, tc.shuttingDown, tc.editorActive, tc.autoCommit, p)
			if ok != tc.wantShow {
				t.Fatalf("show = %v, want %v", ok, tc.wantShow)
			}
			if !tc.wantShow {
				return
			}
			if cell.ID != syncCellID {
				t.Errorf("cell ID = %d, want %d", cell.ID, syncCellID)
			}
			if cell.Text != tc.wantText {
				t.Errorf("text = %q, want %q", cell.Text, tc.wantText)
			}
			if tc.wantFG != "" && cell.FG != tc.wantFG {
				t.Errorf("FG = %q, want %q", cell.FG, tc.wantFG)
			}
		})
	}
}
