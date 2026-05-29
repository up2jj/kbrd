package fs

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

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
