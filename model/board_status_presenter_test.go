package model

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"kbrd/config"
)

func TestBoardStatusPresenter_RenderHeaderIncludesLogoAndCells(t *testing.T) {
	b := NewBoard(config.Config{BoardName: "Demo", NotifyBackend: "none"})
	b.columns = []*Column{newTestColumn(t, map[string]string{"a": "alpha"})}

	header := b.statusPresenter().renderHeaderLayout(120)
	for _, want := range []string{"kbrd", "Demo", "1 items", "◇ mcp"} {
		if !strings.Contains(header.view, want) {
			t.Fatalf("header missing %q:\n%s", want, header.view)
		}
	}
	if header.height <= 0 {
		t.Fatalf("header height = %d, want positive", header.height)
	}
}

func TestBoardStatusPresenter_BuiltinCountAndActivityCells(t *testing.T) {
	colA := newTestColumn(t, map[string]string{"a": "alpha", "b": "bravo"})
	colB := newTestColumn(t, map[string]string{"c": "charlie"})
	b := NewBoard(config.Config{NotifyBackend: "none"})
	b.columns = []*Column{colA, colB}
	b.asyncInflight = 2
	b.templateExec.inflight = 3
	b.hooks = &hookRunner{
		running: true,
		queue:   []pendingHook{{name: "a"}, {name: "b"}},
	}
	b.scriptStatus = "lua busy"
	b.mcpStatus = MCPRunning

	b.statusPresenter().updateBuiltinCells()

	assertCellText(t, b, -2, "3 items")
	assertCellText(t, b, -4, "⟳ 2 running")
	assertCellText(t, b, -8, "✦ 3 generating")
	assertCellText(t, b, -6, "⚙ hooks 3")
	assertCellText(t, b, -3, "lua busy")
	assertCellText(t, b, -7, "◆ mcp")
	if got := b.cells.cells[-4].FG; got != string(b.palette.AccentSoft) {
		t.Fatalf("activity FG = %q, want accent %q", got, b.palette.AccentSoft)
	}
}

func TestBoardStatusPresenter_GitCleanAndDirtyCells(t *testing.T) {
	repo := initStatusGitRepo(t)
	colDir := filepath.Join(repo, "1. todo")
	if err := os.MkdirAll(colDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cardPath := filepath.Join(colDir, "a.md")
	if err := os.WriteFile(cardPath, []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	statusGitRun(t, repo, "add", ".")
	statusGitRun(t, repo, "commit", "-m", "card")

	b := NewBoard(config.Config{Path: repo, NotifyBackend: "none"})
	b.git.Detect()
	b.statusPresenter().updateBuiltinCells()
	assertCellText(t, b, -1, "✓ clean")

	if err := os.WriteFile(cardPath, []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b.git.RefreshStatsNow()
	b.statusPresenter().updateBuiltinCells()
	assertCellText(t, b, -1, "● 1")
}

func assertCellText(t *testing.T, b *Board, id int, want string) {
	t.Helper()
	cell := b.cells.cells[id]
	if cell == nil {
		t.Fatalf("cell %d missing; cells=%v", id, b.cells.cells)
	}
	if cell.Text != want {
		t.Fatalf("cell %d text = %q, want %q", id, cell.Text, want)
	}
}

func initStatusGitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	statusGitRun(t, dir, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "seed.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	statusGitRun(t, dir, "add", ".")
	statusGitRun(t, dir, "commit", "-m", "initial")
	return dir
}

func statusGitRun(t *testing.T, dir string, args ...string) {
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
