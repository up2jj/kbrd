package companion

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestAppendScratchpadRejectsEmptyText(t *testing.T) {
	if err := AppendScratchpad(t.TempDir(), "  \n"); err == nil {
		t.Fatal("expected empty text error")
	}
}

func TestGitStatusOutsideRepository(t *testing.T) {
	if got := gitStatus(t.TempDir()); got != "not a repository" {
		t.Fatalf("gitStatus = %q", got)
	}
}

func TestGitStatusReportsChanges(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "card.md"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial")
	if got := gitStatus(dir); got != "clean · local" {
		t.Fatalf("clean gitStatus = %q", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "card.md"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := gitStatus(dir); got != "1 changed" {
		t.Fatalf("dirty gitStatus = %q", got)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}
