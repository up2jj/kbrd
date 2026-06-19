package shellcmd

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRun_CapturesCombinedOutput(t *testing.T) {
	t.Parallel()
	res, err := Run(context.Background(), "", "echo out; echo err 1>&2")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", res.ExitCode)
	}
	if !strings.Contains(res.Output, "out") || !strings.Contains(res.Output, "err") {
		t.Errorf("Output = %q, want both stdout and stderr", res.Output)
	}
}

// RunStdinStdout pipes stdin to the command and captures stdout only — stderr
// must not leak into the result, since stdout replaces the edited line.
func TestRunStdinStdout_StdoutOnly(t *testing.T) {
	t.Parallel()
	res, err := RunStdinStdout(context.Background(), "", "tr a-z A-Z; echo noise 1>&2", "hello")
	if err != nil {
		t.Fatalf("RunStdinStdout: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", res.ExitCode)
	}
	if strings.TrimSpace(res.Output) != "HELLO" {
		t.Errorf("Output = %q, want %q (stderr must be excluded)", res.Output, "HELLO")
	}
}

// On a non-zero exit, stderr is surfaced in Output so the caller has something to
// report, and ExitCode is non-zero so the line is left unchanged.
func TestRunStdinStdout_FailureSurfacesStderr(t *testing.T) {
	t.Parallel()
	res, err := RunStdinStdout(context.Background(), "", "echo boom 1>&2; exit 2", "x")
	if err != nil {
		t.Fatalf("RunStdinStdout returned error for non-zero exit: %v", err)
	}
	if res.ExitCode != 2 {
		t.Errorf("ExitCode = %d, want 2", res.ExitCode)
	}
	if !strings.Contains(res.Output, "boom") {
		t.Errorf("Output = %q, want it to contain the stderr text", res.Output)
	}
}

func TestRun_NonZeroExitIsNotAnError(t *testing.T) {
	t.Parallel()
	res, err := Run(context.Background(), "", "echo boom; exit 3")
	if err != nil {
		t.Fatalf("Run returned error for non-zero exit: %v", err)
	}
	if res.ExitCode != 3 {
		t.Errorf("ExitCode = %d, want 3", res.ExitCode)
	}
	if !strings.Contains(res.Output, "boom") {
		t.Errorf("Output = %q, want it to contain boom", res.Output)
	}
}

func TestRun_RunsInDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	res, err := Run(context.Background(), dir, "pwd")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// macOS /tmp is a symlink to /private/tmp, so compare suffixes.
	if !strings.Contains(res.Output, strings.TrimPrefix(dir, "/private")) {
		t.Errorf("pwd = %q, want it to reflect dir %q", strings.TrimSpace(res.Output), dir)
	}
}

func TestRun_TimeoutYieldsErrTimeout(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := Run(ctx, "", "sleep 5")
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
}

func TestCommand_SetsDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	c := Command(dir, "true")
	if c.Dir != dir {
		t.Errorf("Command Dir = %q, want %q", c.Dir, dir)
	}
}
