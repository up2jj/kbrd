// Package shellcmd is the single chokepoint for running rendered shell strings.
// Custom commands (YAML/Lua) and the MCP server all funnel through here so that
// "run sh -c <user/LLM-supplied script>" has one place to reason about timeouts,
// working directory, and stdin.
package shellcmd

import (
	"context"
	"errors"
	"os/exec"
	"strings"
)

// ErrTimeout is returned by Run when ctx's deadline fired or it was cancelled
// before the command finished.
var ErrTimeout = errors.New("command timed out")

// Result is the outcome of a captured shell command. A non-zero ExitCode is a
// command result, not an error — Run returns a nil error in that case.
type Result struct {
	Output   string // combined stdout+stderr
	ExitCode int
}

// Run executes `sh -c script` in dir with stdin from /dev/null (non-interactive,
// so interactive prompts return immediately rather than blocking) and captures
// combined output. The caller sets any timeout via context.WithTimeout; a
// deadline or cancellation yields ErrTimeout. Other start/IO failures propagate
// as-is.
func Run(ctx context.Context, dir, script string) (Result, error) {
	return run(ctx, dir, script, "")
}

// RunStdin is Run with stdin fed from the given string instead of /dev/null —
// used to pipe template form values into a {{shell}} command. Like Run, it
// leaves the command's environment unset, so it inherits the parent process's
// full environment.
func RunStdin(ctx context.Context, dir, script, stdin string) (Result, error) {
	return run(ctx, dir, script, stdin)
}

func run(ctx context.Context, dir, script, stdin string) (Result, error) {
	c := exec.CommandContext(ctx, "sh", "-c", script)
	c.Dir = dir
	if stdin == "" {
		c.Stdin = nil // /dev/null
	} else {
		c.Stdin = strings.NewReader(stdin)
	}
	out, err := c.CombinedOutput()
	res := Result{Output: string(out)}
	if err != nil {
		if ctx.Err() != nil {
			// Deadline or cancellation; the exec error is just "signal: killed".
			return res, ErrTimeout
		}
		if ee, ok := errors.AsType[*exec.ExitError](err); ok {
			res.ExitCode = ee.ExitCode()
			return res, nil
		}
		return res, err
	}
	return res, nil
}

// Command builds the non-captured *exec.Cmd for `sh -c script` in dir, for
// callers that drive it interactively (e.g. Bubble Tea's tea.ExecProcess, which
// hands the terminal over). No timeout is applied: an interactive command may
// run arbitrarily long.
func Command(dir, script string) *exec.Cmd {
	c := exec.Command("sh", "-c", script)
	c.Dir = dir
	return c
}
