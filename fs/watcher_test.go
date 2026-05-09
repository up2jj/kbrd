package fs

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDiscoverPaths(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "alpha"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "bravo"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ignored.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	paths, err := DiscoverPaths(root)
	if err != nil {
		t.Fatalf("DiscoverPaths: %v", err)
	}

	want := map[string]bool{
		root:                              true,
		filepath.Join(root, "alpha"):      true,
		filepath.Join(root, "bravo"):      true,
	}
	if len(paths) != len(want) {
		t.Fatalf("got %d paths, want %d: %v", len(paths), len(want), paths)
	}
	for _, p := range paths {
		if !want[p] {
			t.Errorf("unexpected path %q", p)
		}
	}
	if paths[0] != root {
		t.Errorf("paths[0] = %q, want root %q (root must lead the result)", paths[0], root)
	}
}

func TestDiscoverPaths_EmptyDir(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths, err := DiscoverPaths(root)
	if err != nil {
		t.Fatalf("DiscoverPaths: %v", err)
	}
	if len(paths) != 1 || paths[0] != root {
		t.Errorf("paths = %v, want [%q]", paths, root)
	}
}

func TestDiscoverPaths_MissingRoot(t *testing.T) {
	t.Parallel()
	_, err := DiscoverPaths(filepath.Join(t.TempDir(), "nope"))
	if err == nil {
		t.Fatal("expected error for missing root")
	}
}

func TestNewWatcher_Add_Close(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	w, err := NewWatcher([]string{dir})
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	// Trigger an event: create a file in the watched dir.
	// fsnotify supports this on darwin/linux/windows.
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = os.WriteFile(filepath.Join(dir, "trigger.md"), []byte("x"), 0644)
	}()

	select {
	case <-w.Events():
		// success
	case err := <-w.Errors():
		t.Fatalf("watcher error: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for fsnotify event")
	}

	if err := w.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestNewWatcher_Add(t *testing.T) {
	t.Parallel()
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	w, err := NewWatcher([]string{dir1})
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	if err := w.Add(dir2); err != nil {
		t.Errorf("Add: %v", err)
	}
}

func TestNewWatcher_BadPath(t *testing.T) {
	t.Parallel()
	_, err := NewWatcher([]string{filepath.Join(t.TempDir(), "does-not-exist")})
	if err == nil {
		t.Fatal("expected error for missing path")
	}
}
