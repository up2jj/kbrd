package web

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"kbrd/fs"
)

// Syncer persists web mutations to git: every mutation commits and pushes
// immediately, serialized under one mutex so a background pull can never merge
// mid-write. A nil *Syncer means "no-sync mode" (not a git repo, or no
// remote): all methods are no-ops and the UI shows a banner.
type Syncer struct {
	mu           sync.Mutex // serializes mutations and background pulls
	root         string
	push         bool // false when the repo has no remote
	author       string
	email        string
	instanceName string // tags conflict-copy filenames; "" falls back to a timestamp

	statusMu    sync.Mutex // guards the last-result fields below
	lastErrText string     // redacted; "" when the last sync succeeded
	lastNote    string     // informational detail on an otherwise-ok sync (e.g. conflict copies)
}

// NewSyncer returns a Syncer for the repo containing dir, or nil when dir is
// not inside a git work tree. Without a remote, commits are still made but
// push/pull are skipped. instanceName tags any conflict copies this server
// creates so a reader can tell which machine's edit was set aside.
func NewSyncer(dir, authorName, authorEmail, instanceName string) *Syncer {
	root := fs.GitRepoRoot(dir)
	if root == "" {
		return nil
	}
	return &Syncer{
		root:         root,
		push:         fs.GitHasRemote(root),
		author:       authorName,
		email:        authorEmail,
		instanceName: instanceName,
	}
}

// CommitPush commits everything with msg and pushes. On a rejected push
// (remote moved ahead) it reconciles with GitMergeResolveSidecar — a merge
// that auto-resolves true conflicts into sidecar copies (local wins) — then
// retries the push. The local commit always exists before any sync is
// attempted, so nothing the user wrote is lost, and the merge leaves origin an
// ancestor of HEAD so the retry fast-forwards (no force).
//
// This self-healing reconcile is the unattended policy shared by every
// automatic flow; the attended TUI sync uses --ff-only unless configured
// otherwise (git.manual_sync_mode).
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
	sidecars, err := fs.GitMergeResolveSidecar(s.root, s.conflictLabel(), s.author, s.email)
	if err != nil {
		return s.record(err)
	}
	s.logSidecars(sidecars)
	if err := fs.GitPush(s.root); err != nil {
		return s.record(err)
	}
	return s.recordSync(sidecars)
}

// PullLoop periodically reconciles with the remote while the worktree is clean,
// using the same merge-with-sidecar resolution as CommitPush. It shares the
// mutation mutex, so it never runs concurrently with a CommitPush. When a
// reconcile creates conflict copies it pushes the resulting merge so they
// propagate to the other machines (including the one whose edit was set aside).
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
				sidecars, err := fs.GitMergeResolveSidecar(s.root, s.conflictLabel(), s.author, s.email)
				if err != nil {
					log.Printf("web: background sync: %v", err)
					s.record(err)
				} else if len(sidecars) > 0 {
					s.logSidecars(sidecars)
					if perr := fs.GitPush(s.root); perr != nil {
						log.Printf("web: background push: %v", perr)
						s.record(perr)
					} else {
						s.recordSync(sidecars)
					}
				} else {
					s.record(nil)
				}
			}
			s.mu.Unlock()
		}
	}
}

// conflictLabel tags conflict-copy filenames: the instance name when set, else
// a timestamp so distinct conflicts don't collide.
func (s *Syncer) conflictLabel() string {
	if s.instanceName != "" {
		return s.instanceName
	}
	return time.Now().Format("2006-01-02 1504")
}

// logSidecars notes each conflict copy to the server log; in headless mode this
// is the operator's record of an automatic resolution.
func (s *Syncer) logSidecars(sidecars []string) {
	for _, p := range sidecars {
		log.Printf("web: sync created conflict copy %s", p)
	}
}

// Status returns the sync state for the header chip: ok is false when the last
// sync attempt failed (with a redacted description); on success, detail carries
// an informational note such as the conflict-copy count, or "".
func (s *Syncer) Status() (ok bool, detail string) {
	if s == nil {
		return true, ""
	}
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	if s.lastErrText != "" {
		return false, s.lastErrText
	}
	return true, s.lastNote
}

// record stores the last sync result (errors already redacted by fs wrappers)
// and clears any prior informational note.
func (s *Syncer) record(err error) error {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	if err != nil {
		s.lastErrText = err.Error()
	} else {
		s.lastErrText = ""
		s.lastNote = ""
	}
	return err
}

// recordSync records a successful sync, surfacing any conflict-copy count on
// the status chip so a headless operator notices the automatic resolution.
func (s *Syncer) recordSync(sidecars []string) error {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	s.lastErrText = ""
	if n := len(sidecars); n > 0 {
		noun := "copy"
		if n > 1 {
			noun = "copies"
		}
		s.lastNote = fmt.Sprintf("%d conflict %s created", n, noun)
	} else {
		s.lastNote = ""
	}
	return nil
}
