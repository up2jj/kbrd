package fs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// credentialRe matches userinfo embedded in URLs (e.g. a PAT in
// https://x-access-token:TOKEN@host/...) so it never leaks into logs or
// error messages surfaced to a UI.
var credentialRe = regexp.MustCompile(`://[^/@\s]+@`)

// RedactCredentials masks URL-embedded credentials in s.
func RedactCredentials(s string) string {
	return credentialRe.ReplaceAllString(s, "://***@")
}

// GitCommitAll stages everything and commits with the given message. A
// non-empty author identity is passed via -c so no git config is required
// (headless daemons); an empty identity uses the user's ambient git config
// (interactive callers). A clean working tree is not an error: committed
// reports whether a commit was actually made.
func GitCommitAll(repoRoot, message, authorName, authorEmail string) (committed bool, err error) {
	return GitCommitAllContext(context.Background(), repoRoot, message, authorName, authorEmail)
}

// GitCommitAllContext is GitCommitAll with a caller-owned deadline/cancellation.
func GitCommitAllContext(ctx context.Context, repoRoot, message, authorName, authorEmail string) (committed bool, err error) {
	if err := gitRunContext(ctx, repoRoot, "add", "-A"); err != nil {
		return false, err
	}
	if GitWorkingTreeClean(repoRoot) {
		return false, nil
	}
	var args []string
	if authorName != "" || authorEmail != "" {
		args = append(args,
			"-c", "user.name="+authorName,
			"-c", "user.email="+authorEmail)
	}
	args = append(args, "commit", "-m", message)
	if err := gitRunContext(ctx, repoRoot, args...); err != nil {
		return false, err
	}
	return true, nil
}

// GitPush pushes the current branch to its upstream (or origin HEAD).
func GitPush(repoRoot string) error {
	return GitPushContext(context.Background(), repoRoot)
}

const gitPushMaxAttempts = 3

var gitPushRetryDelays = [...]time.Duration{500 * time.Millisecond, time.Second}

type gitPushAttemptsError struct {
	attempts int
	err      error
}

func (e *gitPushAttemptsError) Error() string {
	var gitErr *gitCommandError
	if errors.As(e.err, &gitErr) {
		return fmt.Sprintf("git push failed after %d attempts: %s", e.attempts, gitErr.detail)
	}
	return fmt.Sprintf("git push failed after %d attempts: %v", e.attempts, e.err)
}

func (e *gitPushAttemptsError) Unwrap() error { return e.err }

// GitPushContext is GitPush with a caller-owned deadline/cancellation. It
// retries transient transport failures twice, while repository, auth,
// non-fast-forward, and cancellation failures return immediately.
func GitPushContext(ctx context.Context, repoRoot string) error {
	return gitPushContext(ctx, repoRoot, waitForGitPushRetry)
}

func gitPushContext(ctx context.Context, repoRoot string, waitForRetry func(context.Context, time.Duration) error) error {
	for attempt := 1; attempt <= gitPushMaxAttempts; attempt++ {
		err := gitRunContext(ctx, repoRoot, "push")
		if err == nil {
			return nil
		}
		if !isTransientPushError(err) {
			return err
		}
		if attempt == gitPushMaxAttempts {
			return &gitPushAttemptsError{attempts: attempt, err: err}
		}
		if err := waitForRetry(ctx, gitPushRetryDelays[attempt-1]); err != nil {
			return &gitCommandError{verb: "push", detail: err.Error(), cause: err}
		}
	}
	return nil
}

func waitForGitPushRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func isTransientPushError(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || IsNonFastForwardPush(err) {
		return false
	}
	var gitErr *gitCommandError
	if !errors.As(err, &gitErr) || gitErr.verb != "push" {
		return false
	}
	detail := strings.ToLower(gitErr.detail)
	for _, marker := range []string{
		"connection reset",
		"connection refused",
		"connection timed out",
		"operation timed out",
		"tls handshake timeout",
		"temporary failure",
		"temporarily unavailable",
		"could not resolve host",
		"could not resolve hostname",
		"name or service not known",
		"network is unreachable",
		"remote end hung up unexpectedly",
		"remote host closed the connection",
		"connection closed by remote host",
		"early eof",
		"rpc failed",
		"returned error: 429",
		"returned error: 500",
		"returned error: 502",
		"returned error: 503",
		"returned error: 504",
	} {
		if strings.Contains(detail, marker) {
			return true
		}
	}
	return false
}

// GitAheadOfUpstreamContext reports whether HEAD has commits that its upstream
// does not. Call it after a fetch/merge to avoid a needless no-op push while
// still publishing locally-created commits and merge resolutions.
func GitAheadOfUpstreamContext(ctx context.Context, repoRoot string) (bool, error) {
	out, err := gitCombinedOutputContext(ctx, repoRoot, "rev-list", "--count", "@{u}..HEAD")
	if err != nil {
		return false, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return false, fmt.Errorf("parse commits ahead of upstream: %w", err)
	}
	return n > 0, nil
}

// GitPullFFOnly runs `git pull --ff-only`: fast-forward or fail loudly.
// This is the attended-sync policy — on divergence the user resolves it
// themselves. Unattended callers want GitPullRebase instead.
func GitPullFFOnly(repoRoot string) error {
	return gitRun(repoRoot, "pull", "--ff-only")
}

// GitAddRemoteOrigin adds url as the "origin" remote and enables
// push.autoSetupRemote so the first push sets upstream tracking
// automatically (works even against an empty remote).
func GitAddRemoteOrigin(repoRoot, url string) error {
	if err := gitRun(repoRoot, "remote", "add", "origin", url); err != nil {
		return err
	}
	return gitRun(repoRoot, "config", "push.autoSetupRemote", "true")
}

// GitFetch updates remote-tracking refs without touching the working tree.
func GitFetch(repoRoot string) error {
	return GitFetchContext(context.Background(), repoRoot)
}

// GitFetchContext is GitFetch with a caller-owned deadline/cancellation.
func GitFetchContext(ctx context.Context, repoRoot string) error {
	return gitRunContext(ctx, repoRoot, "fetch")
}

// GitMergeResolveSidecar fetches and merges the upstream branch (@{u}) into the
// current branch. A clean merge — fast-forward, "already up to date", or
// non-overlapping edits git auto-merges — applies with no further action and
// returns no sidecars. A true content conflict is resolved automatically: the
// local version keeps the original path and the incoming version is written to
// a sibling "<name> (conflict <label>)<ext>" file, then the merge is committed
// with a message naming each copy. This never halts and never loses data — the
// overwritten edit survives in the sidecar and propagates on the next push.
//
// Because the merge runs on top of a committed local HEAD, origin stays an
// ancestor of the result, so the follow-up push fast-forwards — force is never
// needed. conflictLabel tags the sidecar filename (an instance name or
// timestamp, chosen by the caller). authorName/authorEmail inject a commit
// identity via -c when non-empty, mirroring GitCommitAll; empty uses the repo's
// git config. Returns the repo-relative sidecar paths created.
func GitMergeResolveSidecar(repoRoot, conflictLabel, authorName, authorEmail string) (sidecars []string, err error) {
	return GitMergeResolveSidecarContext(context.Background(), repoRoot, conflictLabel, authorName, authorEmail)
}

// GitMergeResolveSidecarContext is GitMergeResolveSidecar with a caller-owned
// deadline/cancellation.
func GitMergeResolveSidecarContext(ctx context.Context, repoRoot, conflictLabel, authorName, authorEmail string) (sidecars []string, err error) {
	if err := GitFetchContext(ctx, repoRoot); err != nil {
		return nil, err
	}
	// The identity is injected on the merge too: a clean or fast-forward-blocked
	// merge auto-creates a merge commit, which would otherwise fail without an
	// ambient git identity (headless daemon, CI runner).
	mergeCmd := append(identityArgs(authorName, authorEmail), "merge", "--no-edit", "@{u}")
	mOut, mErr := gitCombinedOutputContext(ctx, repoRoot, mergeCmd...)
	if mErr == nil {
		return nil, nil // clean: fast-forward, up-to-date, or auto-merged
	}
	// From this point Git may have written MERGE_HEAD and conflicted index
	// entries. Every failure path below must restore the pre-merge checkout;
	// otherwise one failed sidecar write or timed-out command blocks all future
	// sync attempts behind an unresolved merge.
	mergeActive := true
	defer func() {
		if err == nil || !mergeActive {
			return
		}
		if abortErr := abortMerge(repoRoot); abortErr != nil {
			err = fmt.Errorf("%w; additionally failed to abort merge: %v", err, abortErr)
		}
	}()

	// A non-zero merge means either resolvable conflicts or a hard error
	// (dirty tree, unrelated histories). Unmerged entries distinguish the two.
	conflicted, lsErr := unmergedPaths(repoRoot)
	if lsErr != nil || len(conflicted) == 0 {
		if lsErr != nil {
			return nil, lsErr
		}
		return nil, fmt.Errorf("git merge failed with no conflicts to resolve: %s", strings.TrimSpace(mOut))
	}

	var mappings []string
	for _, path := range conflicted {
		oursOK := gitStageExists(repoRoot, 2, path)
		theirs, theirsOK := gitShowStage(repoRoot, 3, path)

		switch {
		case oursOK:
			// Local wins the original path.
			if err := gitRunContext(ctx, repoRoot, "checkout", "--ours", "--", path); err != nil {
				return sidecars, err
			}
			if err := gitRunContext(ctx, repoRoot, "add", "--", path); err != nil {
				return sidecars, err
			}
			if theirsOK { // modify/modify or add/add: preserve incoming copy
				rel, werr := writeSidecar(repoRoot, path, conflictLabel, theirs)
				if werr != nil {
					return sidecars, werr
				}
				if err := gitRunContext(ctx, repoRoot, "add", "--", rel); err != nil {
					return sidecars, err
				}
				sidecars = append(sidecars, rel)
				mappings = append(mappings, path+" → "+rel)
			}
		case theirsOK:
			// Local deleted, remote modified: keep the deletion (local wins)
			// but preserve the incoming version as a sidecar.
			if err := gitRunContext(ctx, repoRoot, "rm", "-f", "--", path); err != nil {
				return sidecars, err
			}
			rel, werr := writeSidecar(repoRoot, path, conflictLabel, theirs)
			if werr != nil {
				return sidecars, werr
			}
			if err := gitRunContext(ctx, repoRoot, "add", "--", rel); err != nil {
				return sidecars, err
			}
			sidecars = append(sidecars, rel)
			mappings = append(mappings, path+" → "+rel)
		default:
			// Neither side has content (rare, e.g. some rename/rename cases):
			// clear the unmerged entry so the merge can be committed.
			if err := gitRunContext(ctx, repoRoot, "rm", "-f", "--", path); err != nil {
				return sidecars, err
			}
		}
	}

	if err := gitRunContext(ctx, repoRoot, commitArgs(authorName, authorEmail, mergeMessage(mappings))...); err != nil {
		return sidecars, err
	}
	mergeActive = false
	return sidecars, nil
}

// abortMerge restores the pre-merge checkout even when the caller's context
// has expired. A fresh, short cleanup context is necessary because a cancelled
// context cannot run the abort command that makes the repository usable again.
func abortMerge(repoRoot string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return gitRunContext(ctx, repoRoot, "merge", "--abort")
}

// unmergedPaths returns the repo-relative paths with unresolved merge conflicts.
func unmergedPaths(repoRoot string) ([]string, error) {
	out, err := gitOutput(repoRoot, "diff", "--name-only", "--diff-filter=U", "-z")
	if err != nil {
		return nil, err
	}
	var paths []string
	for p := range strings.SplitSeq(out, "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

// gitStageExists reports whether the given merge stage (2=ours, 3=theirs) holds
// a blob for path in the index.
func gitStageExists(repoRoot string, stage int, path string) bool {
	_, err := gitOutput(repoRoot, "rev-parse", "--verify", "-q", fmt.Sprintf(":%d:%s", stage, path))
	return err == nil
}

// gitShowStage returns the blob bytes for a merge stage of path, and whether it
// exists.
func gitShowStage(repoRoot string, stage int, path string) ([]byte, bool) {
	out, err := gitOutput(repoRoot, "show", fmt.Sprintf(":%d:%s", stage, path))
	if err != nil {
		return nil, false
	}
	return []byte(out), true
}

// writeSidecar writes content next to path as "<stem> (conflict <label>)<ext>",
// disambiguating with a counter if that name already exists, and returns the
// repo-relative path written.
func writeSidecar(repoRoot, path, label string, content []byte) (string, error) {
	dir := filepath.Dir(path) // forward-slash, repo-relative
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	label = safeConflictLabel(label)

	build := func(name string) string {
		if dir == "." || dir == "" {
			return name
		}
		return dir + "/" + name
	}
	rel := build(fmt.Sprintf("%s (conflict %s)%s", stem, label, ext))
	target, err := sidecarTarget(repoRoot, rel)
	if err != nil {
		return "", err
	}
	for i := 2; ; i++ {
		_, statErr := os.Stat(target)
		if os.IsNotExist(statErr) {
			break
		}
		if statErr != nil {
			return "", fmt.Errorf("stat conflict copy: %w", statErr)
		}
		rel = build(fmt.Sprintf("%s (conflict %s %d)%s", stem, label, i, ext))
		target, err = sidecarTarget(repoRoot, rel)
		if err != nil {
			return "", err
		}
	}
	if err := os.WriteFile(target, content, 0o644); err != nil {
		return "", fmt.Errorf("write conflict copy: %w", err)
	}
	return rel, nil
}

// safeConflictLabel turns the machine-local instance label into one portable
// filename component. Labels originate in flags, environment variables, or
// hostnames; they must never introduce path separators or traversal segments.
func safeConflictLabel(label string) string {
	var b strings.Builder
	separator := false
	for _, r := range strings.TrimSpace(label) {
		safe := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-'
		if safe {
			b.WriteRune(r)
			separator = false
			continue
		}
		if !separator {
			b.WriteByte('-')
			separator = true
		}
	}
	clean := strings.Trim(b.String(), ".-_")
	if clean == "" {
		return "instance"
	}
	const maxLen = 80
	if len(clean) > maxLen {
		return clean[:maxLen]
	}
	return clean
}

// sidecarTarget lexically verifies a generated repo-relative path before it is
// passed to the filesystem. This is redundant with safeConflictLabel for
// current callers, but keeps writeSidecar safe if a future caller changes the
// source of either component.
func sidecarTarget(repoRoot, rel string) (string, error) {
	target := filepath.Join(repoRoot, filepath.FromSlash(rel))
	inside, err := filepath.Rel(repoRoot, target)
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("conflict copy path escapes repository: %q", rel)
	}
	return target, nil
}

// mergeMessage builds a self-explaining merge commit subject (and body listing
// each conflict copy, when any).
func mergeMessage(mappings []string) string {
	if len(mappings) == 0 {
		return "sync: merge origin"
	}
	noun := "copy"
	if len(mappings) > 1 {
		noun = "copies"
	}
	return fmt.Sprintf("sync: merge origin; %d conflict %s\n\n%s",
		len(mappings), noun, strings.Join(mappings, "\n"))
}

// identityArgs returns the leading `-c user.name=/-c user.email=` flags when a
// non-empty name/email is given, else nil. Prefixed onto any git command that
// may create a commit so it never depends on an ambient identity (which a
// headless daemon or CI runner may lack). gitVerb skips these when naming the
// failing verb in errors.
func identityArgs(authorName, authorEmail string) []string {
	if authorName == "" && authorEmail == "" {
		return nil
	}
	return []string{"-c", "user.name=" + authorName, "-c", "user.email=" + authorEmail}
}

// commitArgs assembles `commit -m msg`, injecting the identity flags (mirrors
// GitCommitAll).
func commitArgs(authorName, authorEmail, msg string) []string {
	return append(identityArgs(authorName, authorEmail), "commit", "-m", msg)
}

// GitCloneStreaming clones url into dir like GitClone, but streams git's
// progress output (object counts, percentages) to progress as it happens —
// meant for long-running server boots where the operator watches logs.
// Credentials embedded in url are redacted from the returned error.
func GitCloneStreaming(url, dir string, progress io.Writer) error {
	if !GitAvailable() {
		return fmt.Errorf("git not found on PATH")
	}
	w := &redactingWriter{dst: progress}
	cmd := exec.Command("git", "clone", "--progress", url, dir)
	cmd.Stdout = w
	cmd.Stderr = w // git writes progress to stderr
	err := cmd.Run()
	w.Flush()
	if err != nil {
		return fmt.Errorf("git clone failed: %s", RedactCredentials(err.Error()))
	}
	return nil
}

// redactingWriter applies RedactCredentials line by line (git's fatal errors
// echo the clone URL, credentials included). Progress updates end in \r, full
// lines in \n; both flush the buffer.
type redactingWriter struct {
	dst io.Writer
	buf []byte
}

func (w *redactingWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := strings.IndexAny(string(w.buf), "\r\n")
		if i < 0 {
			break
		}
		line := w.buf[:i+1]
		w.buf = w.buf[i+1:]
		if _, err := w.dst.Write([]byte(RedactCredentials(string(line)))); err != nil {
			return len(p), err
		}
	}
	return len(p), nil
}

// Flush writes any buffered partial line.
func (w *redactingWriter) Flush() {
	if len(w.buf) > 0 {
		w.dst.Write([]byte(RedactCredentials(string(w.buf))))
		w.buf = nil
	}
}
