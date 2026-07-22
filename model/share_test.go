package model

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestItemActionShare(t *testing.T) {
	b := boardWithNCols(t, 1, 1)
	writeColItem(t, b.columns[0], "task")
	b.columns[0].SelectByName("task")

	oldShareFile := shareFile
	t.Cleanup(func() { shareFile = oldShareFile })

	var gotPath string
	shareFile = func(path string) error {
		gotPath = path
		return nil
	}
	cmd, handled := b.itemActions().Invoke(actionShare, actionSourceKey)
	if !handled || cmd == nil {
		t.Fatalf("share handled=%v cmd nil=%v", handled, cmd == nil)
	}
	msg := cmd().(notifyMsg)
	if msg.Type != notifyInfo || msg.Message != "opened share sheet for task" {
		t.Fatalf("share notification = %+v", msg)
	}
	wantPath := filepath.Join(b.columns[0].Path, "task.md")
	if gotPath != wantPath {
		t.Fatalf("share path = %q, want %q", gotPath, wantPath)
	}

	shareFile = func(string) error { return errors.New("boom") }
	cmd, _ = b.itemActions().Invoke(actionShare, actionSourceKey)
	msg = cmd().(notifyMsg)
	if msg.Type != notifyError || msg.Message != "failed to share: boom" {
		t.Fatalf("failure notification = %+v", msg)
	}
}
