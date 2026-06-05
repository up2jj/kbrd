package fs

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
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

func TestGitHasRemote(t *testing.T) {
	dir := initRepo(t)
	if GitHasRemote(dir) {
		t.Fatalf("expected no remote on fresh repo")
	}
	run(t, dir, "remote", "add", "origin", "https://example.com/x.git")
	if !GitHasRemote(dir) {
		t.Fatalf("expected remote after `remote add`")
	}
}

func TestGitHasRemote_NotARepo(t *testing.T) {
	requireGit(t)
	if GitHasRemote(t.TempDir()) {
		t.Fatalf("expected false for non-repo dir")
	}
	if GitHasRemote("") {
		t.Fatalf("expected false for empty repoRoot")
	}
}

func TestGitWorkingTreeClean(t *testing.T) {
	dir := initRepo(t)
	if !GitWorkingTreeClean(dir) {
		t.Fatalf("fresh repo should be clean")
	}

	writeFile(t, filepath.Join(dir, "untracked.md"), "x\n")
	if GitWorkingTreeClean(dir) {
		t.Fatalf("expected dirty after adding untracked file")
	}

	run(t, dir, "add", ".")
	run(t, dir, "commit", "-m", "add untracked")
	if !GitWorkingTreeClean(dir) {
		t.Fatalf("expected clean after commit")
	}

	writeFile(t, filepath.Join(dir, "seed.md"), "modified\n")
	if GitWorkingTreeClean(dir) {
		t.Fatalf("expected dirty after modifying tracked file")
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

func TestGitClone(t *testing.T) {
	src := initRepo(t)
	dst := filepath.Join(t.TempDir(), "clone")
	if err := GitClone(src, dst); err != nil {
		t.Fatalf("GitClone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "seed.md")); err != nil {
		t.Errorf("expected seed.md in clone: %v", err)
	}
	if GitRepoRoot(dst) == "" {
		t.Errorf("expected clone destination to be a repo")
	}
}

func TestGitClone_BadSource(t *testing.T) {
	requireGit(t)
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	dst := filepath.Join(t.TempDir(), "clone")
	err := GitClone(missing, dst)
	if err == nil {
		t.Fatal("expected error cloning a nonexistent source")
	}
	if !strings.Contains(err.Error(), "git clone failed") {
		t.Errorf("error = %q, want it to mention 'git clone failed'", err)
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
	if got.New {
		t.Errorf("modified tracked file flagged New; stat = %+v", got)
	}
}

func TestGitDiffStats_UntrackedIsNew(t *testing.T) {
	root := initRepo(t)
	writeFile(t, filepath.Join(root, "new.md"), "fresh\n")
	stats := GitDiffStats(root)
	s, ok := stats[filepath.Join(root, "new.md")]
	if !ok {
		t.Fatalf("expected untracked entry in stats; got %v", stats)
	}
	// Untracked files carry the New flag: zero counts, not flagged as moved.
	if s.Added != 0 || s.Deleted != 0 || s.Moved || !s.New {
		t.Errorf("untracked stat = %+v, want zero counts, Moved=false, New=true", s)
	}
}

func TestGitDiffStats_StagedAddIsNew(t *testing.T) {
	root := initRepo(t)
	writeFile(t, filepath.Join(root, "new.md"), "fresh\n")
	run(t, root, "add", "new.md")
	stats := GitDiffStats(root)
	s, ok := stats[filepath.Join(root, "new.md")]
	if !ok {
		t.Fatalf("expected staged-add entry in stats; got %v", stats)
	}
	if !s.New {
		t.Errorf("staged-add stat = %+v, want New=true", s)
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

func TestGitChangedFiles_HandlesStagedRename(t *testing.T) {
	root := initRepo(t)
	// Stage the rename to produce an index-side R entry.
	run(t, root, "mv", "seed.md", "renamed.md")
	run(t, root, "add", "-A")

	files := GitChangedFiles(root)
	if len(files) != 1 {
		t.Fatalf("rename: want 1 entry, got %d: %+v", len(files), files)
	}
	f := files[0]
	if f.Status[0] != 'R' {
		t.Errorf("rename status = %q, want leading 'R'", f.Status)
	}
	if f.Path != "renamed.md" {
		t.Errorf("rename path = %q, want %q", f.Path, "renamed.md")
	}
	if f.OrigPath != "seed.md" {
		t.Errorf("rename OrigPath = %q, want %q", f.OrigPath, "seed.md")
	}
	if f.Added != 0 || f.Deleted != 0 {
		t.Errorf("rename counts = +%d -%d, want zeros", f.Added, f.Deleted)
	}
}

func TestGitChangedFiles_UnstagedMove(t *testing.T) {
	root := initRepo(t)
	// kbrd's MoveItemTo does os.Rename to a different dir but keeps the
	// basename. We pair such ` D` + `??` entries into one synthetic R.
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(root, "seed.md")
	dst := filepath.Join(root, "sub", "seed.md")
	if err := os.Rename(src, dst); err != nil {
		t.Fatal(err)
	}

	files := GitChangedFiles(root)
	if len(files) != 1 {
		t.Fatalf("unstaged move: want 1 entry, got %d: %+v", len(files), files)
	}
	f := files[0]
	if f.Status[1] != 'R' {
		t.Errorf("unstaged move status = %q, want worktree-side 'R'", f.Status)
	}
	want := filepath.Join("sub", "seed.md")
	if f.Path != want {
		t.Errorf("unstaged move path = %q, want %q", f.Path, want)
	}
	if f.OrigPath != "seed.md" {
		t.Errorf("unstaged move OrigPath = %q, want %q", f.OrigPath, "seed.md")
	}
}

func TestGitChangedFiles_RenameAcrossDirsWithSpaces(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	run(t, dir, "init", "-b", "main")
	writeFile(t, filepath.Join(dir, "1. To do", "foo.md"), "x\n")
	run(t, dir, "add", ".")
	run(t, dir, "commit", "-m", "seed")
	// Unstaged move across dirs that contain spaces — exercises the
	// porcelain v2 path-with-spaces parsing.
	src := filepath.Join(dir, "1. To do", "foo.md")
	dst := filepath.Join(dir, "2. In progress", "foo.md")
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(src, dst); err != nil {
		t.Fatal(err)
	}

	files := GitChangedFiles(dir)
	if len(files) != 1 {
		t.Fatalf("want 1 entry, got %d: %+v", len(files), files)
	}
	f := files[0]
	if f.Status[1] != 'R' {
		t.Errorf("status = %q, want worktree-side R", f.Status)
	}
	wantPath := filepath.Join("2. In progress", "foo.md")
	wantOrig := filepath.Join("1. To do", "foo.md")
	if f.Path != wantPath {
		t.Errorf("Path = %q, want %q", f.Path, wantPath)
	}
	if f.OrigPath != wantOrig {
		t.Errorf("OrigPath = %q, want %q", f.OrigPath, wantOrig)
	}
}

func TestGitDiffStats_ReportsMoved(t *testing.T) {
	root := initRepo(t)
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(root, "seed.md")
	dst := filepath.Join(root, "sub", "seed.md")
	if err := os.Rename(src, dst); err != nil {
		t.Fatal(err)
	}
	stats := GitDiffStats(root)
	s, ok := stats[dst]
	if !ok {
		t.Fatalf("expected entry for moved destination %q in %v", dst, stats)
	}
	if !s.Moved {
		t.Errorf("Moved = false, want true for moved file")
	}
	if s.Added != 0 || s.Deleted != 0 {
		t.Errorf("counts = +%d -%d, want zeros for move", s.Added, s.Deleted)
	}
}

func TestGitChangedFiles_UnstagedRenameSameDir(t *testing.T) {
	root := initRepo(t)
	src := filepath.Join(root, "seed.md")
	dst := filepath.Join(root, "renamed.md")
	if err := os.Rename(src, dst); err != nil {
		t.Fatal(err)
	}

	files := GitChangedFiles(root)
	if len(files) != 1 {
		t.Fatalf("same-dir rename: want 1 entry, got %d: %+v", len(files), files)
	}
	f := files[0]
	if f.Status[1] != 'R' {
		t.Errorf("status = %q, want worktree-side R", f.Status)
	}
	if f.Path != "renamed.md" {
		t.Errorf("Path = %q, want %q", f.Path, "renamed.md")
	}
	if f.OrigPath != "seed.md" {
		t.Errorf("OrigPath = %q, want %q", f.OrigPath, "seed.md")
	}
}

func TestGitChangedFiles_UnstagedRenameWithModification(t *testing.T) {
	root := initRepo(t)
	// Rename AND modify — content hashes won't match, so we expect the
	// pre-existing two-row behavior (D + ??), not a synthetic rename.
	if err := os.Remove(filepath.Join(root, "seed.md")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "renamed.md"), "seed\nmodified\n")

	files := GitChangedFiles(root)
	if len(files) != 2 {
		t.Fatalf("rename+modify: want 2 entries (no false pairing), got %d: %+v", len(files), files)
	}
}

func TestGitChangedFiles_MultipleUnstagedRenames(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	run(t, dir, "init", "-b", "main")
	writeFile(t, filepath.Join(dir, "a.md"), "alpha\n")
	writeFile(t, filepath.Join(dir, "b.md"), "beta\n")
	run(t, dir, "add", ".")
	run(t, dir, "commit", "-m", "seed")

	if err := os.Rename(filepath.Join(dir, "a.md"), filepath.Join(dir, "x.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, "b.md"), filepath.Join(dir, "y.md")); err != nil {
		t.Fatal(err)
	}

	files := GitChangedFiles(dir)
	if len(files) != 2 {
		t.Fatalf("multi-rename: want 2 entries, got %d: %+v", len(files), files)
	}
	pairs := map[string]string{}
	for _, f := range files {
		if f.Status[1] != 'R' {
			t.Errorf("entry %+v: want worktree-side R", f)
		}
		pairs[f.OrigPath] = f.Path
	}
	if pairs["a.md"] != "x.md" {
		t.Errorf("a.md should pair with x.md, got %q", pairs["a.md"])
	}
	if pairs["b.md"] != "y.md" {
		t.Errorf("b.md should pair with y.md, got %q", pairs["b.md"])
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
