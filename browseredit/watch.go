package browseredit

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"

	"kbrd/editdraft"
)

const watchDebounce = 80 * time.Millisecond

func (m *Manager) watchLoop(watcher *fsnotify.Watcher) {
	defer m.watchWG.Done()
	pending := make(map[string]struct{})
	var timer *time.Timer
	var timerC <-chan time.Time
	for {
		select {
		case ev, ok := <-watcher.Events:
			if !ok {
				return
			}
			if ev.Op == fsnotify.Chmod || strings.HasSuffix(filepath.Base(ev.Name), ".kbrd-swap") {
				continue
			}
			if ev.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Rename|fsnotify.Remove) == 0 {
				continue
			}
			pending[filepath.Clean(ev.Name)] = struct{}{}
			if timer == nil {
				timer = time.NewTimer(watchDebounce)
			} else {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(watchDebounce)
			}
			timerC = timer.C
		case <-timerC:
			paths := pending
			pending = make(map[string]struct{})
			timerC = nil
			for path := range paths {
				m.handleWatchedPath(path)
			}
		case _, ok := <-watcher.Errors:
			if !ok {
				return
			}
		case <-m.ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		}
	}
}

func (m *Manager) handleWatchedPath(path string) {
	m.mu.RLock()
	s := m.byPath[path]
	m.mu.RUnlock()
	if s == nil {
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			m.Invalidate(s.id, "card was deleted or moved")
		}
		return
	}
	doc := ParseDocument(string(raw))
	m.reconcileDiskDocument(s, string(raw), doc)
}

// reconcileDiskDocument applies a newly observed disk document without ever
// advancing a dirty browser session's baseline. Both fsnotify and document
// GETs use this path so an intervening GET cannot hide a pending conflict from
// the watcher or leave the recovery sidecar with stale frontmatter.
func (m *Manager) reconcileDiskDocument(s *session, raw string, doc Document) {
	s.mu.Lock()
	if s.status == sessionInvalid || doc.Revision == s.document.Revision {
		s.mu.Unlock()
		return
	}
	if !s.dirty {
		s.raw, s.document, s.conflict = raw, doc, false
		s.broadcastLocked(streamEvent{Name: "document", Data: doc})
		s.mu.Unlock()
		return
	}
	body := s.draftBody
	s.conflict = true
	s.mu.Unlock()

	merged, mergeErr := MergeBody(raw, body)
	if mergeErr == nil {
		_ = editdraft.Write(s.path, []byte(merged))
	}
	s.mu.Lock()
	if s.status != sessionInvalid {
		data := map[string]any{"document": doc, "malformedFrontmatter": mergeErr != nil}
		s.broadcastLocked(streamEvent{Name: "conflict", Data: data})
	}
	s.mu.Unlock()
}
