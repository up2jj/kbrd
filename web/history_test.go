package web

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kbrd/fs"
)

// gitify turns a test server's board into a git repo with everything
// committed, and wires the Syncer (mirrors finishInit).
func gitify(t *testing.T, s *Server, dir string) {
	t.Helper()
	if !fs.GitAvailable() {
		t.Skip("git not on PATH")
	}
	gitRun(t, dir, "init", "-b", "main")
	if _, err := fs.GitCommitAll(dir, "initial board", "Tester", "t@example.com"); err != nil {
		t.Fatal(err)
	}
	s.sync = NewSyncer(s.opts.BoardPath, "Tester", "t@example.com")
	if s.sync == nil {
		t.Fatal("syncer not created for git-backed board")
	}
}

// gitRun executes git in dir with a hermetic identity/config.
func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestHistoryPage(t *testing.T) {
	s, h, boardDir := newTestServer(t)
	gitify(t, s, boardDir)
	c := loginCookie(t, h)

	rec := get(h, "/history", c)
	if rec.Code != http.StatusOK {
		t.Fatalf("/history: %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "initial board") {
		t.Error("history missing commit subject")
	}
	if !strings.Contains(body, "Tester") || !strings.Contains(body, "just now") {
		t.Error("history missing author or relative time")
	}

	// Topbar links to /history when git-backed.
	if !strings.Contains(get(h, "/", c).Body.String(), `href="/history"`) {
		t.Error("topbar missing history link")
	}
}

func TestHistoryWithoutGit(t *testing.T) {
	_, h, _ := newTestServer(t) // no git → Syncer nil
	c := loginCookie(t, h)

	if rec := get(h, "/history", c); rec.Code != http.StatusNotFound {
		t.Fatalf("/history on non-git board: %d", rec.Code)
	}
	body := get(h, "/", c).Body.String()
	if strings.Contains(body, `href="/history"`) {
		t.Error("topbar shows history link without git")
	}
	if strings.Contains(body, `class="changed"`) {
		t.Error("badge shown without git")
	}
}

func TestChangedIndicator(t *testing.T) {
	s, h, boardDir := newTestServer(t)
	gitify(t, s, boardDir)
	c := loginCookie(t, h)

	// Initial commit touched everything → task-a carries the badge.
	body := get(h, "/", c).Body.String()
	if !cardHasBadge(body, "task-a") {
		t.Error("task-a missing badge after initial commit")
	}

	// Commit a new card; only it should be flagged afterward.
	os.WriteFile(filepath.Join(boardDir, "1. todo", "task-b.md"), []byte("# Task B\n"), 0o644)
	if _, err := fs.GitCommitAll(boardDir, "add task-b", "Tester", "t@example.com"); err != nil {
		t.Fatal(err)
	}
	body = get(h, "/", c).Body.String()
	if !cardHasBadge(body, "task-b") {
		t.Error("task-b missing badge")
	}
	if cardHasBadge(body, "task-a") {
		t.Error("task-a still badged after unrelated commit")
	}

	// Column fragment carries the badge too.
	if frag := get(h, "/c/1.%20todo", c).Body.String(); !cardHasBadge(frag, "task-b") {
		t.Error("column fragment missing badge")
	}
}

// TestChangedIndicatorBoardInSubdir covers a board living below the repo root:
// HEAD paths are repo-relative and must be re-rooted onto the board.
func TestChangedIndicatorBoardInSubdir(t *testing.T) {
	if !fs.GitAvailable() {
		t.Skip("git not on PATH")
	}
	repo := t.TempDir()
	boardDir := filepath.Join(repo, "boards", "main")
	os.MkdirAll(filepath.Join(boardDir, "1. todo"), 0o755)
	os.WriteFile(filepath.Join(boardDir, "1. todo", "task-a.md"), []byte("# Task A\n"), 0o644)

	gitRun(t, repo, "init", "-b", "main")
	if _, err := fs.GitCommitAll(repo, "initial", "Tester", "t@example.com"); err != nil {
		t.Fatal(err)
	}

	s, h, _ := newTestServer(t)
	s.opts.BoardPath = boardDir
	s.sync = NewSyncer(boardDir, "Tester", "t@example.com")
	if s.sync == nil {
		t.Fatal("syncer not created")
	}

	c := loginCookie(t, h)
	if body := get(h, "/", c).Body.String(); !cardHasBadge(body, "task-a") {
		t.Error("subdir board: task-a missing badge")
	}
}

// cardHasBadge reports whether the card link for name contains the changed dot.
func cardHasBadge(body, name string) bool {
	_, rest, found := strings.Cut(body, "/i/"+name+`"`)
	if !found {
		return false
	}
	block, _, _ := strings.Cut(rest, "</a>")
	return strings.Contains(block, `class="changed"`)
}

func TestRelTime(t *testing.T) {
	now := time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		ago  time.Duration
		want string
	}{
		{30 * time.Second, "just now"},
		{90 * time.Second, "1 minute ago"},
		{45 * time.Minute, "45 minutes ago"},
		{90 * time.Minute, "1 hour ago"},
		{5 * time.Hour, "5 hours ago"},
		{30 * time.Hour, "1 day ago"},
		{6 * 24 * time.Hour, "6 days ago"},
		{20 * 24 * time.Hour, "2 weeks ago"},
		{60 * 24 * time.Hour, "2026-04-08"},
	}
	for _, tc := range cases {
		if got := relTime(now.Add(-tc.ago), now); got != tc.want {
			t.Errorf("relTime(-%v) = %q, want %q", tc.ago, got, tc.want)
		}
	}
}
