package model

import (
	"fmt"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	kbrdfs "kbrd/fs"
)

// handleConflictReviewMessage keeps conflict-review routing out of the
// board's general update switch.
func (b *Board) handleConflictReviewMessage(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case conflictReviewDiffMsg:
		if msg.Err != nil {
			return b, b.notifier.ErrorCause("diff", msg.Err), true
		}
		b.conflictReview.showDiff(msg)
		return b, nil, true

	case conflictReviewEditMsg:
		return b, b.openConflictEdit(msg.Conflict), true

	case conflictReviewActionMsg:
		model, cmd := b.handleConflictReviewAction(msg)
		return model, cmd, true
	}

	return b, nil, false
}

func (b *Board) openConflictReview() tea.Cmd {
	root := b.git.RepoRoot()
	if root == "" {
		return b.notifier.Error("no git repository")
	}
	if err := b.conflictReview.Open(root); err != nil {
		return b.notifier.Error(err.Error())
	}
	return nil
}

func (b *Board) openConflictEdit(conflict kbrdfs.Conflict) tea.Cmd {
	root := b.git.RepoRoot()
	path := filepath.Join(root, filepath.FromSlash(conflict.IncomingPath))
	if _, err := os.Stat(path); err != nil {
		return b.notifier.ErrorCause("open incoming version", err)
	}
	return b.editor.OpenManagedFile("incoming "+filepath.Base(path), path)
}

func (b *Board) handleConflictReviewAction(msg conflictReviewActionMsg) (tea.Model, tea.Cmd) {
	root := b.git.RepoRoot()
	if root == "" {
		return b, b.notifier.Error("no git repository")
	}
	var err error
	var success string
	switch msg.Action {
	case conflictKeepOriginal:
		err = kbrdfs.DeleteConflict(root, msg.Conflict)
		success = "kept original"
	case conflictReplaceOriginal:
		err = kbrdfs.ReplaceConflict(root, msg.Conflict)
		success = "replaced original"
	case conflictKeepBoth:
		var path string
		path, err = kbrdfs.RenameConflict(root, msg.Conflict, msg.Name)
		if err == nil {
			success = "kept both as " + filepath.Base(path)
		}
	default:
		err = fmt.Errorf("unknown conflict action")
	}
	if err != nil {
		return b, b.notifier.ErrorCause("resolve conflict", err)
	}
	if err := b.loadColumns(); err != nil {
		return b, b.notifier.ErrorCause("reload board", err)
	}
	b.applyColumnTransforms()
	b.git.RefreshStatsNow()
	if err := b.conflictReview.Refresh(); err != nil {
		return b, b.notifier.ErrorCause("refresh conflicts", err)
	}
	return b, b.notifier.Success(success)
}
