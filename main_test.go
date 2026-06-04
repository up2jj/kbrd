package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	fsutil "kbrd/fs"
	"kbrd/recents"
)

// requireGit skips the test when git is unavailable, reusing the same
// production check (fs.GitAvailable) that fs's own requireGit wraps — the latter
// is unexported, so package main can't call it directly.
func requireGit(t *testing.T) {
	t.Helper()
	if !fsutil.GitAvailable() {
		t.Skip("git not installed")
	}
}

// gitRun runs a git command in dir, insulated from the user's global config so
// commits succeed in sandboxed environments.
func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// sourceRepo creates a committed board repo (one card under "Todo") and returns
// its path, skipping the test if git is unavailable.
func sourceRepo(t *testing.T) string {
	t.Helper()
	requireGit(t)
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	card := filepath.Join(dir, "Todo", "task1.md")
	if err := os.MkdirAll(filepath.Dir(card), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(card, []byte("# task one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "seed")
	return dir
}

// isolateConfig redirects the per-user config dir (where recents lives) into a
// temp dir so tests never touch the real ~/.config / Application Support store.
func isolateConfig(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
}

func recentsHas(t *testing.T, path string) bool {
	t.Helper()
	store, err := recents.Load()
	if err != nil {
		t.Fatalf("recents.Load: %v", err)
	}
	for _, e := range store.Entries {
		if e.Path == path {
			return true
		}
	}
	return false
}

func TestRunClone_Success(t *testing.T) {
	isolateConfig(t)
	src := sourceRepo(t)
	dst := filepath.Join(t.TempDir(), "board")

	abs, err := runClone(src, dst)
	if err != nil {
		t.Fatalf("runClone: %v", err)
	}

	wantAbs, _ := filepath.Abs(dst)
	if abs != wantAbs {
		t.Errorf("returned path = %q, want %q", abs, wantAbs)
	}
	if _, err := os.Stat(filepath.Join(abs, "Todo", "task1.md")); err != nil {
		t.Errorf("expected cloned board contents: %v", err)
	}
	if !recentsHas(t, abs) {
		t.Errorf("cloned board not registered in recents")
	}
}

func TestRunClone_DerivesDirFromURL(t *testing.T) {
	isolateConfig(t)
	src := sourceRepo(t)

	// Run from a temp working dir so the derived relative dir lands there.
	work := t.TempDir()
	t.Chdir(work)

	// A trailing ".git" in the URL must be stripped from the derived dir.
	url := src + ".git"
	_ = os.Rename(src, src+".git") // make the .git-suffixed source actually exist

	abs, err := runClone(url, "")
	if err != nil {
		t.Fatalf("runClone: %v", err)
	}
	want := filepath.Join(work, filepath.Base(src)) // ".git" stripped
	if abs != want {
		t.Errorf("derived dir = %q, want %q", abs, want)
	}
}

func TestRunClone_ExistingDir(t *testing.T) {
	isolateConfig(t)
	src := sourceRepo(t)
	dst := t.TempDir() // already exists

	if _, err := runClone(src, dst); err == nil {
		t.Fatal("expected error when target directory already exists")
	} else if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error = %q, want it to mention 'already exists'", err)
	}
}

func TestWriteLocalTemplate(t *testing.T) {
	dir := t.TempDir()
	if err := writeLocalTemplate(dir); err != nil {
		t.Fatalf("writeLocalTemplate: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "kbrd.toml")); err != nil {
		t.Errorf("expected kbrd.toml to be written: %v", err)
	}
	// A second run must refuse to clobber the existing file.
	if err := writeLocalTemplate(dir); err == nil {
		t.Error("expected error on second writeLocalTemplate (overwrite)")
	} else if !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Errorf("error = %q, want it to mention 'refusing to overwrite'", err)
	}
}

func TestWriteGlobalTemplate(t *testing.T) {
	isolateConfig(t)
	if err := writeGlobalTemplate(); err != nil {
		t.Fatalf("writeGlobalTemplate: %v", err)
	}
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("UserConfigDir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfgDir, "kbrd", "config.toml")); err != nil {
		t.Errorf("expected global config template to be written: %v", err)
	}
}

func TestRunClone_BadURL(t *testing.T) {
	isolateConfig(t)
	requireGit(t)
	missing := filepath.Join(t.TempDir(), "nope")
	dst := filepath.Join(t.TempDir(), "board")

	if _, err := runClone(missing, dst); err == nil {
		t.Fatal("expected error cloning a nonexistent source")
	} else if !strings.Contains(err.Error(), "git clone failed") {
		t.Errorf("error = %q, want it to mention 'git clone failed'", err)
	}
}
