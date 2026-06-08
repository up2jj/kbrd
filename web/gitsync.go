package web

import (
	"context"
	"log"
	"sync"
	"time"

	"kbrd/fs"
)

// Syncer persists web mutations to git: every mutation commits and pushes
// immediately, serialized under one mutex so a background pull can never
// rebase mid-write. A nil *Syncer means "no-sync mode" (not a git repo, or no
// remote): all methods are no-ops and the UI shows a banner.
type Syncer struct {
	mu     sync.Mutex // serializes mutations and background pulls
	root   string
	push   bool // false when the repo has no remote
	author string
	email  string

	statusMu    sync.Mutex // guards the last-result fields below
	lastErrText string     // redacted; "" when the last sync succeeded
}

// NewSyncer returns a Syncer for the repo containing dir, or nil when dir is
// not inside a git work tree. Without a remote, commits are still made but
// push/pull are skipped.
func NewSyncer(dir, authorName, authorEmail string) *Syncer {
	root := fs.GitRepoRoot(dir)
	if root == "" {
		return nil
	}
	return &Syncer{
		root:   root,
		push:   fs.GitHasRemote(root),
		author: authorName,
		email:  authorEmail,
	}
}

// CommitPush commits everything with msg and pushes. On a rejected push
// (remote moved ahead) it pulls --rebase once and retries the push once. The
// local commit always exists before any sync is attempted, so a conflicting
// rebase aborts back to it and nothing the user wrote is lost.
//
// Rebase-and-retry is the unattended policy: no human is present to resolve
// divergence, so the server must make progress on its own checkout. The
// attended TUI sync deliberately uses --ff-only instead — do not harmonize
// the two.
func (s *Syncer) CommitPush(msg string) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := fs.GitCommitAll(s.root, msg, s.author, s.email); err != nil {
		return s.record(err)
	}
	if !s.push {
		return s.record(nil)
	}
	if err := fs.GitPush(s.root); err == nil {
		return s.record(nil)
	}
	if err := fs.GitPullRebase(s.root); err != nil {
		return s.record(err)
	}
	return s.record(fs.GitPush(s.root))
}

// PullLoop periodically pulls --rebase while the worktree is clean. It shares
// the mutation mutex, so it never runs concurrently with a CommitPush.
func (s *Syncer) PullLoop(ctx context.Context, every time.Duration) {
	if s == nil || !s.push || every <= 0 {
		return
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.mu.Lock()
			if fs.GitWorkingTreeClean(s.root) {
				if err := fs.GitPullRebase(s.root); err != nil {
					log.Printf("web: background pull: %v", err)
					s.record(err)
				} else {
					s.record(nil)
				}
			}
			s.mu.Unlock()
		}
	}
}

// Status returns the sync state for the header chip: ok is false when the
// last sync attempt failed, with a redacted description.
func (s *Syncer) Status() (ok bool, detail string) {
	if s == nil {
		return true, ""
	}
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	return s.lastErrText == "", s.lastErrText
}

// record stores the last sync result (errors already redacted by fs wrappers).
func (s *Syncer) record(err error) error {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	if err != nil {
		s.lastErrText = err.Error()
	} else {
		s.lastErrText = ""
	}
	return err
}
