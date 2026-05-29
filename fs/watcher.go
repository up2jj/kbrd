package fs

import (
	"github.com/fsnotify/fsnotify"
)

type Watcher struct {
	watcher *fsnotify.Watcher
	paths   []string
}

func NewWatcher(paths []string) (*Watcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	watcher := &Watcher{watcher: w}
	for _, path := range paths {
		if err := w.Add(path); err != nil {
			return nil, err
		}
	}
	return watcher, nil
}

func (w *Watcher) Add(path string) error {
	return w.watcher.Add(path)
}

func (w *Watcher) Events() <-chan fsnotify.Event {
	return w.watcher.Events
}

func (w *Watcher) Errors() <-chan error {
	return w.watcher.Errors
}

func (w *Watcher) Close() error {
	return w.watcher.Close()
}
