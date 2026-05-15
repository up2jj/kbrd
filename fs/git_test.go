package fs

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
)

func requireGit(t *testing.T) {
	t.Helper()
	if !GitAvailable() {
		t.Skip("git not installed")
	}
}

// run executes a git command in dir and fails the test on error.
func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	// Insulate the test from the user's global git config so commits succeed
	// in CI/sandbox environments without identity configured.
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

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// initRepo creates a fresh repo with one committed file ("seed.md")
// and returns the repo root path.
func initRepo(t *testing.T) string {
	t.Helper()
	requireGit(t)
	dir := t.TempDir()
	run(t, dir, "init", "-b", "main")
	writeFile(t, filepath.Join(dir, "seed.md"), "seed\n")
	run(t, dir, "add", ".")
	run(t, dir, "commit", "-m", "initial")
	return dir
}

func TestGitAvailable(t *testing.T) {
	// Sanity: GitAvailable returns the same value across calls (sync.Once).
	a := GitAvailable()
	b := GitAvailable()
	if a != b {
		t.Fatalf("GitAvailable inconsistent: %v then %v", a, b)
	}
}

func TestGitRepoRoot_InsideRepo(t *testing.T) {
	root := initRepo(t)
	got := GitRepoRoot(root)
	// macOS tempdirs are symlinked under /private/var; resolve both sides.
	wantResolved, _ := filepath.EvalSymlinks(root)
	gotResolved, _ := filepath.EvalSymlinks(got)
	if gotResolved != wantResolved {
		t.Errorf("GitRepoRoot = %q, want %q", got, root)
	}
}

func TestGitRepoRoot_OutsideRepo(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	if got := GitRepoRoot(dir); got != "" {
		t.Errorf("GitRepoRoot of non-repo = %q, want empty", got)
	}
}

func TestGitInit(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	if err := GitInit(dir); err != nil {
		t.Fatalf("GitInit: %v", err)
	}
	if GitRepoRoot(dir) == "" {
		t.Fatalf("expected %q to be a repo after GitInit", dir)
	}
}

func TestGitCurrentBranch(t *testing.T) {
	root := initRepo(t)
	got := GitCurrentBranch(root)
	if got != "main" {
		t.Errorf("GitCurrentBranch = %q, want %q", got, "main")
	}
}

func TestGitCurrentBranch_NotARepo(t *testing.T) {
	requireGit(t)
	if got := GitCurrentBranch(t.TempDir()); got != "" {
		t.Errorf("GitCurrentBranch of non-repo = %q, want empty", got)
	}
}

func TestGitDiffStats_TracksModifiedFile(t *testing.T) {
	root := initRepo(t)
	// Modify the seeded file: +2 lines, -0.
	writeFile(t, filepath.Join(root, "seed.md"), "seed\nplus one\nplus two\n")

	stats := GitDiffStats(root)
	key := filepath.Join(root, "seed.md")
	got, ok := stats[key]
	if !ok {
		t.Fatalf("expected entry for %q in %v", key, stats)
	}
	if got.Added != 2 || got.Deleted != 0 {
		t.Errorf("stats for seed.md = %+v, want {Added:2 Deleted:0}", got)
	}
}

func TestGitDiffStats_IgnoresUntracked(t *testing.T) {
	root := initRepo(t)
	writeFile(t, filepath.Join(root, "new.md"), "fresh\n")
	stats := GitDiffStats(root)
	if _, ok := stats[filepath.Join(root, "new.md")]; ok {
		t.Errorf("GitDiffStats should not list untracked files; got %v", stats)
	}
}

func TestGitDiffStats_EmptyRepoRoot(t *testing.T) {
	if got := GitDiffStats(""); got != nil {
		t.Errorf("GitDiffStats(\"\") = %v, want nil", got)
	}
}

func TestGitChangedFiles_ModifiedAndUntracked(t *testing.T) {
	root := initRepo(t)
	// Modify tracked file (+1 -1).
	writeFile(t, filepath.Join(root, "seed.md"), "replaced\n")
	// Add a new untracked file.
	writeFile(t, filepath.Join(root, "new.md"), "fresh\n")

	files := GitChangedFiles(root)
	if len(files) != 2 {
		t.Fatalf("GitChangedFiles len = %d, want 2: %+v", len(files), files)
	}
	// Result is sorted by path: "new.md" then "seed.md".
	paths := []string{files[0].Path, files[1].Path}
	wantPaths := []string{"new.md", "seed.md"}
	sort.Strings(paths)
	for i := range paths {
		if paths[i] != wantPaths[i] {
			t.Fatalf("paths = %v, want %v", paths, wantPaths)
		}
	}

	byPath := map[string]FileChange{}
	for _, f := range files {
		byPath[f.Path] = f
	}

	newFC := byPath["new.md"]
	if newFC.Status != "??" {
		t.Errorf("new.md status = %q, want %q", newFC.Status, "??")
	}
	if newFC.Added != 0 || newFC.Deleted != 0 {
		t.Errorf("new.md counts = +%d -%d, want zeros", newFC.Added, newFC.Deleted)
	}

	seedFC := byPath["seed.md"]
	if seedFC.Status[1] != 'M' {
		t.Errorf("seed.md status = %q, want worktree-modified ( M)", seedFC.Status)
	}
	if seedFC.Added != 1 || seedFC.Deleted != 1 {
		t.Errorf("seed.md counts = +%d -%d, want +1 -1", seedFC.Added, seedFC.Deleted)
	}
}

func TestGitChangedFiles_CleanWorkingTree(t *testing.T) {
	root := initRepo(t)
	files := GitChangedFiles(root)
	if len(files) != 0 {
		t.Errorf("expected empty result on clean tree, got %+v", files)
	}
}

func TestGitChangedFiles_HandlesRename(t *testing.T) {
	root := initRepo(t)
	// Rename + stage to produce an "R " porcelain entry.
	run(t, root, "mv", "seed.md", "renamed.md")
	run(t, root, "add", "-A")

	files := GitChangedFiles(root)
	// Should produce exactly one entry (the rename) — not two records
	// from the trailing "old path" porcelain segment.
	if len(files) != 1 {
		t.Fatalf("rename: want 1 entry, got %d: %+v", len(files), files)
	}
	if files[0].Status[0] != 'R' {
		t.Errorf("rename status = %q, want leading 'R'", files[0].Status)
	}
	if files[0].Path != "renamed.md" {
		t.Errorf("rename path = %q, want %q", files[0].Path, "renamed.md")
	}
}

func TestGitChangedFiles_PathWithSpaces(t *testing.T) {
	root := initRepo(t)
	// Create a file with whitespace in its name; porcelain -z keeps it raw.
	writeFile(t, filepath.Join(root, "1. To do", "foo bar.md"), "x\n")
	files := GitChangedFiles(root)
	if len(files) != 1 {
		t.Fatalf("want 1 entry, got %d: %+v", len(files), files)
	}
	want := filepath.Join("1. To do", "foo bar.md")
	if files[0].Path != want {
		t.Errorf("path = %q, want %q", files[0].Path, want)
	}
}

func TestGitChangedFiles_NonRepo(t *testing.T) {
	if got := GitChangedFiles(t.TempDir()); got != nil {
		t.Errorf("GitChangedFiles of non-repo = %v, want nil", got)
	}
}
