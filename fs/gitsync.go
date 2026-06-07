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

// gitRun executes a git command in repoRoot and returns a redacted error that
// includes git's combined output on failure.
func gitRun(repoRoot string, args ...string) error {
	if !GitAvailable() {
		return fmt.Errorf("git not found on PATH")
	}
	full := append([]string{"-C", repoRoot}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("git %s failed: %s", args[0], RedactCredentials(detail))
	}
	return nil
}

// GitCommitAll stages everything and commits with the given message and
// author identity (passed via -c so no git config is required). A clean
// working tree is not an error: it commits only when there is something to
// commit.
func GitCommitAll(repoRoot, message, authorName, authorEmail string) error {
	if err := gitRun(repoRoot, "add", "-A"); err != nil {
		return err
	}
	if GitWorkingTreeClean(repoRoot) {
		return nil
	}
	return gitRun(repoRoot,
		"-c", "user.name="+authorName,
		"-c", "user.email="+authorEmail,
		"commit", "-m", message)
}

// GitPush pushes the current branch to its upstream (or origin HEAD).
func GitPush(repoRoot string) error {
	return gitRun(repoRoot, "push")
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
