package web

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// startWatcher builds a ConfigWatcher with a short debounce whose apply bumps
// a counter, and runs it until the test ends.
func startWatcher(t *testing.T, files []string) *atomic.Int32 {
	t.Helper()
	var applies atomic.Int32
	cw, err := NewConfigWatcher(files,
		func() (ReloadableConfig, error) { return ReloadableConfig{}, nil },
		func(ReloadableConfig) { applies.Add(1) },
	)
	if err != nil {
		t.Fatalf("NewConfigWatcher: %v", err)
	}
	cw.debounce = 50 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go cw.Run(ctx)
	return &applies
}

func waitFor(t *testing.T, applies *atomic.Int32, want int32) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if applies.Load() == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("applies: got %d want %d", applies.Load(), want)
}

func TestConfigWatcher_DebouncesBurst(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "kbrd.toml")
	applies := startWatcher(t, []string{cfg})

	// Two saves in quick succession must coalesce into one reload.
	for range 2 {
		if err := os.WriteFile(cfg, []byte("[board]\nname = \"x\"\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	waitFor(t, applies, 1)

	// Settle past the debounce window, then make sure no extra apply leaked.
	time.Sleep(150 * time.Millisecond)
	if got := applies.Load(); got != 1 {
		t.Fatalf("applies after settle: got %d want 1", got)
	}
}

func TestConfigWatcher_FileCreatedLater(t *testing.T) {
	// The --git-url flow: the watched kbrd.toml does not exist when the
	// watcher starts (only its directory does).
	dir := t.TempDir()
	cfg := filepath.Join(dir, "kbrd.toml")
	applies := startWatcher(t, []string{cfg})

	if err := os.WriteFile(cfg, []byte("[board]\nname = \"x\"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	waitFor(t, applies, 1)
}

func TestConfigWatcher_IgnoresOtherFiles(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "kbrd.toml")
	applies := startWatcher(t, []string{cfg})

	if err := os.WriteFile(filepath.Join(dir, "card.md"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	if got := applies.Load(); got != 0 {
		t.Fatalf("applies: got %d want 0 for unrelated file", got)
	}
}

func TestConfigWatcher_MissingDirSkipped(t *testing.T) {
	// A nonexistent global config dir must not prevent watching the board dir.
	dir := t.TempDir()
	cfg := filepath.Join(dir, "kbrd.toml")
	missing := filepath.Join(dir, "no-such-dir", "config.toml")
	applies := startWatcher(t, []string{cfg, missing})

	if err := os.WriteFile(cfg, []byte("[board]\nname = \"x\"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	waitFor(t, applies, 1)
}
