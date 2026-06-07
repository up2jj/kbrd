package fs

import (
	"path/filepath"
	"testing"
)

// initEmptyRepo creates a repo with no commits (no HEAD).
func initEmptyRepo(t *testing.T) string {
	t.Helper()
	requireGit(t)
	dir := t.TempDir()
	run(t, dir, "init", "-b", "main")
	return dir
}

func TestGitLog(t *testing.T) {
	repo := initEmptyRepo(t)

	// Empty repo: no error, no commits.
	if commits, err := GitLog(repo, 10); err != nil || commits != nil {
		t.Fatalf("empty repo: %v, %v", commits, err)
	}

	writeFile(t, filepath.Join(repo, "a.md"), "one\n")
	run(t, repo, "add", ".")
	run(t, repo, "commit", "-m", "first commit")
	writeFile(t, filepath.Join(repo, "b.md"), "two\n")
	run(t, repo, "add", ".")
	run(t, repo, "commit", "-m", "second commit")

	commits, err := GitLog(repo, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 {
		t.Fatalf("got %d commits, want 2", len(commits))
	}
	// Newest first.
	if commits[0].Subject != "second commit" || commits[1].Subject != "first commit" {
		t.Fatalf("order wrong: %q, %q", commits[0].Subject, commits[1].Subject)
	}
	c := commits[0]
	if c.Author != "test" || c.Hash == "" || c.Short == "" || len(c.Short) >= len(c.Hash) {
		t.Fatalf("fields wrong: %+v", c)
	}
	if c.Time.IsZero() || c.Time.Before(commits[1].Time) {
		t.Fatalf("times wrong: %v, %v", c.Time, commits[1].Time)
	}

	// Limit respected.
	if commits, _ := GitLog(repo, 1); len(commits) != 1 || commits[0].Subject != "second commit" {
		t.Fatalf("limit: %+v", commits)
	}
}

func TestGitHeadChangedFiles(t *testing.T) {
	repo := initEmptyRepo(t)

	// Empty repo: no error, no files.
	if files, err := GitHeadChangedFiles(repo); err != nil || files != nil {
		t.Fatalf("empty repo: %v, %v", files, err)
	}

	writeFile(t, filepath.Join(repo, "a.md"), "one\n")
	run(t, repo, "add", ".")
	run(t, repo, "commit", "-m", "first")
	// Root commit lists its files.
	if files, err := GitHeadChangedFiles(repo); err != nil || len(files) != 1 || files[0] != "a.md" {
		t.Fatalf("root commit: %v, %v", files, err)
	}

	writeFile(t, filepath.Join(repo, "col", "b.md"), "two\n")
	run(t, repo, "add", ".")
	run(t, repo, "commit", "-m", "second")
	files, err := GitHeadChangedFiles(repo)
	if err != nil {
		t.Fatal(err)
	}
	// Only the latest commit's file; slash paths.
	if len(files) != 1 || files[0] != "col/b.md" {
		t.Fatalf("head files: %v", files)
	}
}
