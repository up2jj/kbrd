package fs

// This file is the single place git subprocesses are spawned from. The rules:
// errors are always credential-redacted (RedactCredentials) before they leave
// this package; read-only queries pass --no-optional-locks, mutations do not;
// GitCommand is the only escape hatch for callers that must own the process
// themselves (e.g. an interactive terminal handoff) — redaction is their job.

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// gitCommandError keeps command failures machine-readable inside this package
// while presenting only credential-redacted detail to callers.
type gitCommandError struct {
	verb   string
	detail string
	cause  error
}

func (e *gitCommandError) Error() string {
	return fmt.Sprintf("git %s failed: %s", e.verb, e.detail)
}

func (e *gitCommandError) Unwrap() error { return e.cause }

// gitOutput runs a read-only git query in repoRoot and returns its stdout.
// Bakes in the GitAvailable guard and --no-optional-locks; on failure the
// error carries git's stderr, redacted.
func gitOutput(repoRoot string, args ...string) (string, error) {
	if !GitAvailable() {
		return "", fmt.Errorf("git not found on PATH")
	}
	full := append([]string{"--no-optional-locks", "-C", repoRoot}, args...)
	out, err := exec.Command("git", full...).Output()
	if err != nil {
		var detail []byte
		if ee, ok := err.(*exec.ExitError); ok {
			detail = ee.Stderr
		}
		return "", gitError(args, detail, err)
	}
	return string(out), nil
}

// GitOutput runs a read-only git query. It is exported for sibling packages
// that own higher-level git behavior but still route subprocesses through fs.
func GitOutput(repoRoot string, args ...string) (string, error) {
	return gitOutput(repoRoot, args...)
}

// gitCombined runs a mutating git command in repoRoot and returns its combined
// output. No --no-optional-locks (mutations may take the index lock); on
// failure the error carries the combined output, redacted.
func gitCombined(repoRoot string, args ...string) (string, error) {
	return gitCombinedOutputContext(context.Background(), repoRoot, args...)
}

// gitCombinedOutputContext is gitCombined with a caller-owned deadline/cancellation.
func gitCombinedOutputContext(ctx context.Context, repoRoot string, args ...string) (string, error) {
	if !GitAvailable() {
		return "", fmt.Errorf("git not found on PATH")
	}
	full := append([]string{"-C", repoRoot}, args...)
	out, err := exec.CommandContext(ctx, "git", full...).CombinedOutput()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return string(out), &gitCommandError{
				verb:   gitVerb(args),
				detail: ctxErr.Error(),
				cause:  ctxErr,
			}
		}
		return string(out), gitError(args, out, err)
	}
	return string(out), nil
}

// gitRun is gitCombined for callers that only care about success.
func gitRun(repoRoot string, args ...string) error {
	return gitRunContext(context.Background(), repoRoot, args...)
}

// gitRunContext is gitCombinedOutputContext for callers that only care about success.
func gitRunContext(ctx context.Context, repoRoot string, args ...string) error {
	_, err := gitCombinedOutputContext(ctx, repoRoot, args...)
	return err
}

// GitCommand builds `git -C repoRoot args...` for callers that must own the
// process (e.g. tea.ExecProcess handing the terminal to git so it can prompt
// for credentials). Output is NOT redacted — that is the caller's job.
func GitCommand(repoRoot string, args ...string) *exec.Cmd {
	full := append([]string{"-C", repoRoot}, args...)
	return exec.Command("git", full...)
}

// GitDiffFiles renders a unified diff between two filesystem paths. Git exits
// with status 1 when the files differ; that expected status is not returned as
// an error when diff output exists.
func GitDiffFiles(repoRoot, left, right string) (string, error) {
	if repoRoot == "" || left == "" || right == "" {
		return "", fmt.Errorf("diff paths are incomplete")
	}
	out, err := GitCommand(repoRoot, "--no-pager", "diff", "--no-index", "--no-color", "--unified=3", "--", left, right).CombinedOutput()
	text := strings.TrimRight(string(out), "\n")
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 && text != "" {
			return text, nil
		}
		return "", gitError([]string{"diff", "--no-index"}, out, err)
	}
	if text != "" {
		return text, nil
	}
	return "(no differences)", nil
}

// gitError formats a redacted "git <verb> failed: <detail>" error, preferring
// the command's own output over the bare exit status.
func gitError(args []string, out []byte, err error) error {
	detail := strings.TrimSpace(string(out))
	if detail == "" {
		detail = err.Error()
	}
	return &gitCommandError{
		verb:   gitVerb(args),
		detail: RedactCredentials(detail),
		cause:  err,
	}
}

// IsNonFastForwardPush reports whether err is a push rejection caused by the
// remote branch moving ahead. Callers can use it to decide whether a fetch and
// merge is appropriate without parsing the rendered, redacted error.
func IsNonFastForwardPush(err error) bool {
	var gitErr *gitCommandError
	if !errors.As(err, &gitErr) || gitErr.verb != "push" {
		return false
	}
	detail := strings.ToLower(gitErr.detail)
	return strings.Contains(detail, "non-fast-forward") ||
		strings.Contains(detail, "(fetch first)")
}

// gitVerb returns the subcommand name from args, skipping any leading
// `-c key=val` pairs so identity-injecting commits report "commit", not "-c".
func gitVerb(args []string) string {
	for i := 0; i < len(args); i++ {
		if args[i] == "-c" {
			i++
			continue
		}
		return args[i]
	}
	return "git"
}
