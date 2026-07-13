package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	kbrdfs "kbrd/fs"
)

func TestConflictReviewOpensAndQueuesKeepOriginal(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "task.md"), []byte("local\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "task (conflict laptop).md"), []byte("incoming\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var review ConflictReview
	review.SetPalette(DarkPalette())
	review.SetSize(100, 30)
	if err := review.Open(root); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(review.View(100, 30), "Review Changes") {
		t.Fatal("review view did not contain its title")
	}
	cmd := review.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	if cmd == nil {
		t.Fatal("keep-original did not produce an action command")
	}
	msg, ok := cmd().(conflictReviewActionMsg)
	if !ok || msg.Action != conflictKeepOriginal {
		t.Fatalf("action message = %#v", msg)
	}
	if msg.Conflict.IncomingPath != "task (conflict laptop).md" {
		t.Fatalf("conflict = %#v", msg.Conflict)
	}
}

func TestConflictReviewEditQueuesManagedFileRequest(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "task (conflict laptop).md")
	if err := os.WriteFile(path, []byte("incoming\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var review ConflictReview
	if err := review.Open(root); err != nil {
		t.Fatal(err)
	}
	cmd := review.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	msg, ok := cmd().(conflictReviewEditMsg)
	if !ok || msg.Conflict.IncomingPath != filepath.Base(path) {
		t.Fatalf("edit message = %#v", msg)
	}
}

func TestConflictReviewUsesConflictTypeFromFS(t *testing.T) {
	conflict := kbrdfs.Conflict{IncomingPath: "task (conflict laptop).md"}
	if !kbrdfs.IsConflictCopy(filepath.Base(conflict.IncomingPath)) {
		t.Fatal("expected generated sidecar to be recognized")
	}
}

func TestConflictReviewListIsCompactAndHumanReadable(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("local\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README (conflict laptop).md"), []byte("incoming\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var review ConflictReview
	review.SetPalette(DarkPalette())
	review.SetSize(100, 30)
	if err := review.Open(root); err != nil {
		t.Fatal(err)
	}

	view := ansi.Strip(review.View(100, 30))
	if !strings.Contains(view, "README.md  ←  incoming from laptop") {
		t.Fatalf("review row is not human-readable:\n%s", view)
	}
	if strings.Contains(view, "README (conflict laptop).md") {
		t.Fatalf("review row exposes generated conflict filename:\n%s", view)
	}
	if height := lipgloss.Height(view); height >= 20 {
		t.Fatalf("compact review height = %d, want less than 20:\n%s", height, view)
	}
}
