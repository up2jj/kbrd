package fs

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initRepoPair creates a bare "remote" plus a clone with one initial commit
// pushed, and returns (bareDir, cloneDir).
func initRepoPair(t *testing.T) (string, string) {
	t.Helper()
	if !GitAvailable() {
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
	return bare, clone
}

func TestGitCommitAllAndPush(t *testing.T) {
	bare, clone := initRepoPair(t)

	os.WriteFile(filepath.Join(clone, "card.md"), []byte("hello\n"), 0o644)
	if err := GitCommitAll(clone, "web: create card", "kbrd-web", "kbrd@localhost"); err != nil {
		t.Fatal(err)
	}
	if !GitWorkingTreeClean(clone) {
		t.Fatal("working tree dirty after GitCommitAll")
	}
	// Clean tree: commit-all is a no-op, not an error.
	if err := GitCommitAll(clone, "noop", "kbrd-web", "kbrd@localhost"); err != nil {
		t.Fatalf("GitCommitAll on clean tree: %v", err)
	}
	if err := GitPush(clone); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("git", "-C", bare, "log", "--format=%s", "-1").Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != "web: create card" {
		t.Fatalf("remote HEAD commit %q", got)
	}
}

func TestGitPullRebaseConflictAborts(t *testing.T) {
	bare, clone := initRepoPair(t)

	// Second clone pushes a conflicting change to seed.md.
	other := filepath.Join(t.TempDir(), "other")
	if out, err := exec.Command("git", "clone", bare, other).CombinedOutput(); err != nil {
		t.Fatalf("git clone: %v\n%s", err, out)
	}
	os.WriteFile(filepath.Join(other, "seed.md"), []byte("theirs\n"), 0o644)
	if err := GitCommitAll(other, "their edit", "o", "o@o"); err != nil {
		t.Fatal(err)
	}
	if err := GitPush(other); err != nil {
		t.Fatal(err)
	}

	// Local conflicting commit.
	os.WriteFile(filepath.Join(clone, "seed.md"), []byte("ours\n"), 0o644)
	if err := GitCommitAll(clone, "our edit", "u", "u@u"); err != nil {
		t.Fatal(err)
	}

	if err := GitPullRebase(clone); err == nil {
		t.Fatal("expected rebase conflict error")
	}
	// The abort must leave the worktree clean on our local commit.
	if !GitWorkingTreeClean(clone) {
		t.Fatal("worktree not restored after aborted rebase")
	}
	data, _ := os.ReadFile(filepath.Join(clone, "seed.md"))
	if string(data) != "ours\n" {
		t.Fatalf("local content lost: %q", data)
	}
}

func TestGitPullRebaseFastForward(t *testing.T) {
	bare, clone := initRepoPair(t)

	other := filepath.Join(t.TempDir(), "other")
	if out, err := exec.Command("git", "clone", bare, other).CombinedOutput(); err != nil {
		t.Fatalf("git clone: %v\n%s", err, out)
	}
	os.WriteFile(filepath.Join(other, "new.md"), []byte("x\n"), 0o644)
	if err := GitCommitAll(other, "their add", "o", "o@o"); err != nil {
		t.Fatal(err)
	}
	if err := GitPush(other); err != nil {
		t.Fatal(err)
	}

	if err := GitPullRebase(clone); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(clone, "new.md")); err != nil {
		t.Fatal("pulled file missing")
	}
}

func TestRedactCredentials(t *testing.T) {
	in := "fatal: unable to access 'https://x-access-token:ghp_secret123@github.com/u/r.git/'"
	got := RedactCredentials(in)
	if strings.Contains(got, "ghp_secret123") {
		t.Fatalf("credential not redacted: %s", got)
	}
	if !strings.Contains(got, "://***@github.com") {
		t.Fatalf("unexpected redaction: %s", got)
	}
	// URLs without credentials pass through untouched.
	if plain := "https://github.com/u/r.git"; RedactCredentials(plain) != plain {
		t.Fatal("redactor mangled a credential-free URL")
	}
}

func TestGitCloneStreaming(t *testing.T) {
	bare, _ := initRepoPair(t)
	dest := filepath.Join(t.TempDir(), "dest")
	var buf bytes.Buffer
	if err := GitCloneStreaming(bare, dest, &buf); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "seed.md")); err != nil {
		t.Fatal("clone missing content")
	}

	// Failure path: error and progress output must be redacted.
	var errBuf bytes.Buffer
	err := GitCloneStreaming("https://user:tok3n@invalid.invalid/x.git", filepath.Join(t.TempDir(), "d2"), &errBuf)
	if err == nil {
		t.Fatal("expected clone failure")
	}
	if strings.Contains(err.Error()+errBuf.String(), "tok3n") {
		t.Fatalf("credential leaked: %v / %s", err, errBuf.String())
	}
}
