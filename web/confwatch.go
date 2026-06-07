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
}

// NewConfigWatcher watches the parent directories of files; directories that
// do not exist are skipped. It fails only when the underlying fsnotify
// watcher cannot be created at all.
func NewConfigWatcher(files []string, load func() (ReloadableConfig, error), apply func(ReloadableConfig)) (*ConfigWatcher, error) {
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
	}
	dirs := map[string]bool{}
	for _, f := range files {
		clean := filepath.Clean(f)
		cw.files[clean] = true
		dirs[filepath.Dir(clean)] = true
	}
	for dir := range dirs {
		if err := w.Add(dir); err != nil {
			log.Printf("web: not watching %s: %v", dir, err)
		}
	}
	return cw, nil
}

// Run blocks until ctx is cancelled, debouncing watched-file events and
// calling load+apply once per burst. A load error (e.g. a half-written TOML)
// keeps the current config; the next save retries.
func (cw *ConfigWatcher) Run(ctx context.Context) {
	defer cw.w.Close()

	var timer *time.Timer
	var fire <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case ev, ok := <-cw.w.Events():
			if !ok {
				return
			}
			if !cw.files[filepath.Clean(ev.Name)] {
				continue
			}
			if ev.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Rename|fsnotify.Remove) == 0 {
				continue
			}
			if timer != nil {
				timer.Stop()
			}
			timer = time.NewTimer(cw.debounce)
			fire = timer.C
		case <-fire:
			timer, fire = nil, nil
			rc, err := cw.load()
			if err != nil {
				log.Printf("web: config reload skipped: %v", err)
				continue
			}
			cw.apply(rc)
		case err, ok := <-cw.w.Errors():
			if !ok {
				return
			}
			log.Printf("web: config watcher: %v", err)
		}
	}
}
