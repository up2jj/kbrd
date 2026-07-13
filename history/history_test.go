package history

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Jakub", "GIT_AUTHOR_EMAIL=test@example.com", "GIT_COMMITTER_NAME=Jakub", "GIT_COMMITTER_EMAIL=test@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commit(t *testing.T, repo, subject string) {
	t.Helper()
	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-m", subject)
}

func TestGitProviderSemanticHistorySnapshotAndDiff(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init", "-b", "main")
	path := filepath.Join(repo, "Todo", "task.md")
	write(t, path, "---\npriority: low\n---\n\n# Task\n\nBody\n")
	commit(t, repo, "create")
	write(t, path, "---\npriority: high\n---\n\n# Task\n\nBody\n")
	commit(t, repo, "metadata")
	write(t, path, "---\npriority: high\n---\n\n# Task\n\nChanged body\n")
	commit(t, repo, "edit")
	moved := filepath.Join(repo, "Doing", "task.md")
	if err := os.MkdirAll(filepath.Dir(moved), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, moved); err != nil {
		t.Fatal(err)
	}
	commit(t, repo, "move")
	renamed := filepath.Join(repo, "Doing", "renamed.md")
	if err := os.Rename(moved, renamed); err != nil {
		t.Fatal(err)
	}
	commit(t, repo, "rename")
	if err := os.Remove(renamed); err != nil {
		t.Fatal(err)
	}
	commit(t, repo, "delete")

	p := GitProvider{RepoRoot: repo}
	events, err := p.History(renamed)
	if err != nil {
		t.Fatal(err)
	}
	want := []EventType{EventDeleted, EventRenamed, EventMoved, EventEdited, EventMetadata, EventCreated}
	if len(events) != len(want) {
		t.Fatalf("events = %+v", events)
	}
	for i := range want {
		if events[i].Type != want[i] {
			t.Fatalf("event %d type = %s, want %s", i, events[i].Type, want[i])
		}
	}
	if events[2].Summary != "Moved: Todo → Doing" {
		t.Fatalf("move summary = %q", events[2].Summary)
	}
	deletedSnapshot, err := p.Snapshot(events[0])
	if err != nil || !strings.Contains(string(deletedSnapshot), "Changed body") {
		t.Fatalf("deleted snapshot: %v %q", err, deletedSnapshot)
	}
	snapshot, err := p.Snapshot(events[3])
	if err != nil || !strings.Contains(string(snapshot), "Changed body") {
		t.Fatalf("snapshot: %v %q", err, snapshot)
	}
	diff, err := p.Diff(events[4])
	if err != nil || !strings.Contains(diff, "priority: high") {
		t.Fatalf("diff: %v %q", err, diff)
	}
}
