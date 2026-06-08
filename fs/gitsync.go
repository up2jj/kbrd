package fs

import (
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
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
	if err := gitRun(repoRoot, "add", "-A"); err != nil {
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
	if err := gitRun(repoRoot, args...); err != nil {
		return false, err
	}
	return true, nil
}

// GitPush pushes the current branch to its upstream (or origin HEAD).
func GitPush(repoRoot string) error {
	return gitRun(repoRoot, "push")
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

// GitPullRebase runs `git pull --rebase`. If it fails (e.g. the rebase
// conflicts), any in-progress rebase is aborted so the working tree is
// restored to the local committed state, and the original error is returned.
func GitPullRebase(repoRoot string) error {
	err := gitRun(repoRoot, "pull", "--rebase")
	if err != nil {
		// Best effort: restore the worktree if a rebase was left in progress.
		_ = gitRun(repoRoot, "rebase", "--abort")
	}
	return err
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
