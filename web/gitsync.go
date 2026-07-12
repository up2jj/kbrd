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
	mu           chan struct{} // serializes mutations and background pulls; context-cancelable while waiting
	root         string
	push         bool // false when the repo has no remote
	author       string
	email        string
	instanceName string // tags conflict-copy filenames; "" falls back to a timestamp
	logger       *log.Logger

	statusMu    sync.Mutex // guards the last-result fields below
	lastErrText string     // redacted; "" when the last sync succeeded
	lastNote    string     // informational detail on an otherwise-ok sync (e.g. conflict copies)
}

var webSyncGitTimeout = 2 * time.Minute

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
		mu:           make(chan struct{}, 1),
		root:         root,
		push:         fs.GitHasRemote(root),
		author:       authorName,
		email:        authorEmail,
		instanceName: instanceName,
		logger:       defaultLogger(nil),
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
	return s.CommitPushContext(context.Background(), msg)
}

// CommitPushContext is CommitPush with caller cancellation and a bounded git
// operation. It remains nil-safe for no-sync mode.
func (s *Syncer) CommitPushContext(ctx context.Context, msg string) error {
	if s == nil {
		return nil
	}
	return s.withLock(ctx, func(ctx context.Context) error {
		return s.commitPushLocked(ctx, msg)
	})
}

// MutateAndCommit runs mutate, commits its result, and reconciles/pushes while
// holding the same lock as the pull loop. Callers must put their read-check-
// write operation inside mutate so a pulled revision cannot interleave between
// an optimistic-concurrency check and its write. It is nil-safe for no-sync
// mode, where it still runs mutate.
func (s *Syncer) MutateAndCommit(ctx context.Context, msg string, mutate func() error) error {
	if s == nil {
		return mutate()
	}
	return s.withLock(ctx, func(ctx context.Context) error {
		if err := mutate(); err != nil {
			return err
		}
		return s.commitPushLocked(ctx, msg)
	})
}

func (s *Syncer) commitPushLocked(ctx context.Context, msg string) error {

	if _, err := fs.GitCommitAllContext(ctx, s.root, msg, s.author, s.email); err != nil {
		return s.record(err)
	}
	if !s.push {
		return s.record(nil)
	}
	if err := fs.GitPushContext(ctx, s.root); err == nil {
		return s.record(nil)
	}
	sidecars, err := fs.GitMergeResolveSidecarContext(ctx, s.root, s.conflictLabel(), s.author, s.email)
	if err != nil {
		return s.record(err)
	}
	s.logSidecars(sidecars)
	if err := fs.GitPushContext(ctx, s.root); err != nil {
		return s.record(err)
	}
	return s.recordSync(sidecars)
}

// withLock bounds a full git transaction, including contention on the local
// worktree lock. It lets request cancellation and server shutdown interrupt a
// wait or an in-flight git subprocess instead of wedging future mutations.
func (s *Syncer) withLock(parent context.Context, fn func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(parent, webSyncGitTimeout)
	defer cancel()
	select {
	case s.mu <- struct{}{}:
		defer func() { <-s.mu }()
		return fn(ctx)
	case <-ctx.Done():
		return fmt.Errorf("web git sync: %w", ctx.Err())
	}
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
			if err := s.pullOnce(ctx); err != nil && ctx.Err() == nil {
				logf(s.logger, "web: background sync: %v", err)
				s.record(err)
			}
		}
	}
}

func (s *Syncer) pullOnce(parent context.Context) error {
	return s.withLock(parent, func(ctx context.Context) error {
		if !fs.GitWorkingTreeClean(s.root) {
			return nil
		}
		sidecars, err := fs.GitMergeResolveSidecarContext(ctx, s.root, s.conflictLabel(), s.author, s.email)
		if err != nil {
			return err
		}
		if len(sidecars) == 0 {
			s.record(nil)
			return nil
		}
		s.logSidecars(sidecars)
		if err := fs.GitPushContext(ctx, s.root); err != nil {
			return err
		}
		return s.recordSync(sidecars)
	})
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
		logf(s.logger, "web: sync created conflict copy %s", p)
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
