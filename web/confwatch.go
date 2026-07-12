package web

import (
	"context"
	"log"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"

	fsutil "kbrd/fs"
)

// configDebounce coalesces editor rename-saves and git-pull checkout churn
// into one reload.
const configDebounce = 300 * time.Millisecond

// writeOps are the fsnotify operations that count as a content change. Editors
// save via rename and may delete-then-recreate, so all four are watched.
const writeOps = fsnotify.Create | fsnotify.Write | fsnotify.Rename | fsnotify.Remove

// watchLoop debounces accepted events from w and calls onChange once per burst,
// blocking until ctx is cancelled. It does not own w (the caller closes it).
// Shared by ConfigWatcher and the web-template watcher, whose only differences
// are which events they accept and what they do on change.
func watchLoop(ctx context.Context, w *fsutil.Watcher, debounce time.Duration, accept func(fsnotify.Event) bool, onChange func(), report func(error)) {
	var timer *time.Timer
	var fire <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case ev, ok := <-w.Events():
			if !ok {
				return
			}
			if !accept(ev) {
				continue
			}
			if timer != nil {
				timer.Stop()
			}
			timer = time.NewTimer(debounce)
			fire = timer.C
		case <-fire:
			timer, fire = nil, nil
			onChange()
		case err, ok := <-w.Errors():
			if !ok {
				return
			}
			report(err)
		}
	}
}

// ConfigWatcher watches config files for saves and hands a freshly loaded
// config to apply. It watches the parent directories rather than the files
// themselves: editors save via rename (a file-level watch dies with the old
// inode) and a board's kbrd.toml may not exist yet when watching starts
// (the --git-url clone case).
type ConfigWatcher struct {
	w        *fsutil.Watcher
	files    map[string]bool // cleaned absolute paths that trigger a reload
	load     func() (ReloadableConfig, error)
	apply    func(ReloadableConfig)
	debounce time.Duration
	logger   *log.Logger
}

// NewConfigWatcher watches the parent directories of files; directories that
// do not exist are skipped. It fails only when the underlying fsnotify
// watcher cannot be created at all.
func NewConfigWatcher(files []string, load func() (ReloadableConfig, error), apply func(ReloadableConfig)) (*ConfigWatcher, error) {
	return newConfigWatcher(files, load, apply, nil)
}

func newConfigWatcher(files []string, load func() (ReloadableConfig, error), apply func(ReloadableConfig), logger *log.Logger) (*ConfigWatcher, error) {
	w, err := fsutil.NewWatcher(nil)
	if err != nil {
		return nil, err
	}
	cw := &ConfigWatcher{
		w:        w,
		files:    make(map[string]bool, len(files)),
		load:     load,
		apply:    apply,
		debounce: configDebounce,
		logger:   defaultLogger(logger),
	}
	dirs := map[string]bool{}
	for _, f := range files {
		clean := filepath.Clean(f)
		cw.files[clean] = true
		dirs[filepath.Dir(clean)] = true
	}
	for dir := range dirs {
		if err := w.Add(dir); err != nil {
			logf(cw.logger, "web: not watching %s: %v", dir, err)
		}
	}
	return cw, nil
}

// Run blocks until ctx is cancelled, debouncing watched-file events and
// calling load+apply once per burst. A load error (e.g. a half-written TOML)
// keeps the current config; the next save retries.
func (cw *ConfigWatcher) Run(ctx context.Context) {
	defer cw.w.Close()
	accept := func(ev fsnotify.Event) bool {
		return cw.files[filepath.Clean(ev.Name)] && ev.Op&writeOps != 0
	}
	onChange := func() {
		rc, err := cw.load()
		if err != nil {
			logf(cw.logger, "web: config reload skipped: %v", err)
			return
		}
		cw.apply(rc)
	}
	watchLoop(ctx, cw.w, cw.debounce, accept, onChange, func(err error) {
		logf(cw.logger, "web: watcher: %v", err)
	})
}
