package web

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"kbrd/fs"
)

func TestSyncerLockRespectsCancellation(t *testing.T) {
	s := &Syncer{mu: make(chan struct{}, 1)}
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- s.withLock(context.Background(), func(context.Context) error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := s.withLock(ctx, func(context.Context) error {
		t.Fatal("cancelled caller acquired the sync lock")
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled lock wait = %v, want context cancellation", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("first lock holder: %v", err)
	}
}

// TestSyncerCommitPushReconcilesConflict drives the unattended daemon path: a
// rejected push triggers the self-healing merge, which sets the incoming edit
// aside in a labelled sidecar, pushes, and surfaces the copy count on Status.
func TestSyncerCommitPushReconcilesConflict(t *testing.T) {
	if !fs.GitAvailable() {
		t.Skip("git not on PATH")
	}
	root := t.TempDir()
	bare := filepath.Join(root, "remote.git")
	clone := filepath.Join(root, "clone")
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if out, err := exec.Command("git", "init", "--bare", bare).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}
	if out, err := exec.Command("git", "clone", bare, clone).CombinedOutput(); err != nil {
		t.Fatalf("git clone: %v\n%s", err, out)
	}
	os.WriteFile(filepath.Join(clone, "seed.md"), []byte("seed\n"), 0o644)
	run(clone, "add", "-A")
	run(clone, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "seed")
	run(clone, "push")

	// Another machine pushes a conflicting change.
	other := filepath.Join(root, "other")
	if out, err := exec.Command("git", "clone", bare, other).CombinedOutput(); err != nil {
		t.Fatalf("git clone other: %v\n%s", err, out)
	}
	os.WriteFile(filepath.Join(other, "seed.md"), []byte("theirs\n"), 0o644)
	run(other, "add", "-A")
	run(other, "-c", "user.name=o", "-c", "user.email=o@o", "commit", "-m", "their edit")
	run(other, "push")

	// Local conflicting edit, synced through the Syncer.
	os.WriteFile(filepath.Join(clone, "seed.md"), []byte("ours\n"), 0o644)
	s := NewSyncer(clone, "kbrd-web", "kbrd@localhost", "server-1")
	if s == nil {
		t.Fatal("NewSyncer returned nil for a git repo")
	}
	if err := s.CommitPush("web: edit seed"); err != nil {
		t.Fatalf("CommitPush: %v", err)
	}

	// Local wins the card; the incoming version lands in a labelled sidecar.
	if data, _ := os.ReadFile(filepath.Join(clone, "seed.md")); string(data) != "ours\n" {
		t.Fatalf("local card lost: %q", data)
	}
	sidecar := filepath.Join(clone, "seed (conflict server-1).md")
	if data, err := os.ReadFile(sidecar); err != nil || string(data) != "theirs\n" {
		t.Fatalf("sidecar = %q err=%v, want incoming version", data, err)
	}
	// Status stays ok but reports the conflict copy for the headless operator.
	ok, detail := s.Status()
	if !ok {
		t.Fatalf("status not ok: %s", detail)
	}
	if !strings.Contains(detail, "conflict cop") {
		t.Fatalf("status detail %q does not mention the conflict copy", detail)
	}
	// The merge (and sidecar) reached the remote.
	out, _ := exec.Command("git", "-C", bare, "ls-tree", "--name-only", "HEAD").Output()
	if !strings.Contains(string(out), "seed (conflict server-1).md") {
		t.Fatalf("sidecar not pushed to remote; remote tree:\n%s", out)
	}
}

func TestSyncerCommitPushDoesNotReconcilePermanentPushFailure(t *testing.T) {
	if !fs.GitAvailable() {
		t.Skip("git not on PATH")
	}
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if out, err := exec.Command("git", "-C", root, "remote", "add", "origin", "https://example.com/board.git").CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v\n%s", err, out)
	}
	s := NewSyncer(root, "kbrd-web", "kbrd@localhost", "server-1")
	if s == nil {
		t.Fatal("NewSyncer returned nil for a git repo")
	}

	fakeDir := t.TempDir()
	pushMarker := filepath.Join(t.TempDir(), "push-called")
	reconcileMarker := filepath.Join(t.TempDir(), "fetch-called")
	script := `#!/bin/sh
if [ "$1" = "--no-optional-locks" ]; then
	shift
fi
if [ "$1" = "-C" ]; then
	shift 2
fi
case "$1" in
	add|status)
		exit 0
		;;
	push)
		touch "$PUSH_MARKER"
		echo "fatal: Authentication failed for remote" >&2
		exit 128
		;;
	fetch)
		touch "$RECONCILE_MARKER"
		exit 1
		;;
	*)
		exit 0
		;;
esac
`
	if err := os.WriteFile(filepath.Join(fakeDir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("PUSH_MARKER", pushMarker)
	t.Setenv("RECONCILE_MARKER", reconcileMarker)

	err := s.CommitPush("noop")
	if err == nil || !strings.Contains(err.Error(), "Authentication failed") {
		t.Fatalf("CommitPush error = %v, want authentication failure", err)
	}
	if _, err := os.Stat(pushMarker); err != nil {
		t.Fatalf("push was not attempted: %v", err)
	}
	if _, err := os.Stat(reconcileMarker); !os.IsNotExist(err) {
		t.Fatalf("permanent push failure triggered reconciliation: %v", err)
	}
	ok, detail := s.Status()
	if ok || !strings.Contains(detail, "Authentication failed") {
		t.Fatalf("Status = (%v, %q), want recorded authentication failure", ok, detail)
	}
}
