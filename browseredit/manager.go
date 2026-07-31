package browseredit

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"kbrd/editdraft"
)

const saveQueueSize = 16

// Manager owns the lazy loopback service and all browser editor sessions for
// one Board lifetime.
type Manager struct {
	mu sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc
	closed bool

	listener net.Listener
	server   *http.Server
	address  string
	baseURL  string

	sessions map[string]*session
	byPath   map[string]*session
	saves    chan SaveRequest

	watcher     *fsnotify.Watcher
	watchedDirs map[string]struct{}
	watchWG     sync.WaitGroup
	serverWG    sync.WaitGroup
	closeOnce   sync.Once
}

// New creates a lazy manager. No listener or watcher exists until Open.
func New() *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		ctx: ctx, cancel: cancel,
		sessions: make(map[string]*session), byPath: make(map[string]*session),
		saves: make(chan SaveRequest, saveQueueSize), watchedDirs: make(map[string]struct{}),
	}
}

// SaveRequests returns the bounded Board mutation channel.
func (m *Manager) SaveRequests() <-chan SaveRequest { return m.saves }

// Open creates or reuses a session for an existing card and starts the service
// on literal IPv4 loopback when needed.
func (m *Manager) Open(card Card) (OpenedSession, error) {
	path, err := canonicalPath(card.Path)
	if err != nil {
		return OpenedSession{}, err
	}
	rawBytes, err := os.ReadFile(path)
	if err != nil {
		return OpenedSession{}, fmt.Errorf("read card: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		if err == nil {
			err = fmt.Errorf("not a regular file")
		}
		return OpenedSession{}, fmt.Errorf("open card: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return OpenedSession{}, errors.New("browser editor is closed")
	}
	if existing := m.byPath[path]; existing != nil {
		existing.mu.Lock()
		valid := existing.status != sessionInvalid
		if valid {
			existing.card.BoardName = card.BoardName
			existing.card.ColumnName = card.ColumnName
			existing.card.CardName = card.CardName
			existing.card.LinkTargets = append([]LinkTarget(nil), card.LinkTargets...)
		}
		existing.mu.Unlock()
		if valid {
			return OpenedSession{ID: existing.id, URL: m.sessionURL(existing.token)}, nil
		}
	}
	if err := m.startLocked(); err != nil {
		return OpenedSession{}, err
	}
	if err := m.watchDirLocked(filepath.Dir(path)); err != nil {
		return OpenedSession{}, err
	}
	id, err := randomToken(24)
	if err != nil {
		return OpenedSession{}, err
	}
	token, err := randomToken(32)
	if err != nil {
		return OpenedSession{}, err
	}
	card.Path = ""
	card.LinkTargets = append([]LinkTarget(nil), card.LinkTargets...)
	s := &session{
		id: id, token: token, path: path, card: card,
		raw: string(rawBytes), document: ParseDocument(string(rawBytes)),
		subscribers: make(map[chan streamEvent]struct{}), handoffReady: make(chan struct{}),
	}
	m.sessions[id] = s
	m.sessions[token] = s
	m.byPath[path] = s
	return OpenedSession{ID: id, URL: m.sessionURL(token)}, nil
}

func (m *Manager) startLocked() error {
	if m.listener != nil {
		return nil
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("start browser editor: %w", err)
	}
	m.listener = listener
	m.address = listener.Addr().String()
	m.baseURL = "http://" + m.address
	m.server = &http.Server{
		Handler: m.routes(), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 10 * time.Second, WriteTimeout: 0, IdleTimeout: 60 * time.Second,
	}
	m.serverWG.Add(1)
	go func() {
		defer m.serverWG.Done()
		_ = m.server.Serve(listener)
	}()
	return nil
}

func (m *Manager) watchDirLocked(dir string) error {
	if _, ok := m.watchedDirs[dir]; ok {
		return nil
	}
	if m.watcher == nil {
		watcher, err := fsnotify.NewWatcher()
		if err != nil {
			return fmt.Errorf("watch browser card: %w", err)
		}
		m.watcher = watcher
		m.watchWG.Add(1)
		go m.watchLoop(watcher)
	}
	if err := m.watcher.Add(dir); err != nil {
		return fmt.Errorf("watch browser card directory: %w", err)
	}
	m.watchedDirs[dir] = struct{}{}
	return nil
}

func (m *Manager) sessionURL(token string) string { return m.baseURL + "/s/" + token + "/" }

// URL returns the capability URL for a model-owned session ID.
func (m *Manager) URL(id string) (string, bool) {
	m.mu.RLock()
	s := m.sessions[id]
	m.mu.RUnlock()
	if s == nil {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status == sessionInvalid {
		return "", false
	}
	return m.sessionURL(s.token), true
}

// SessionForPath returns the current ownership state for a canonical path.
func (m *Manager) SessionForPath(path string) (SessionState, bool) {
	canonical, err := canonicalPath(path)
	if err != nil {
		return SessionState{}, false
	}
	m.mu.RLock()
	s := m.byPath[canonical]
	m.mu.RUnlock()
	if s == nil {
		return SessionState{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state(time.Now()), s.status != sessionInvalid
}

// Active reports whether any live writer heartbeat or handoff owns a card.
func (m *Manager) Active() bool {
	now := time.Now()
	m.mu.RLock()
	defer m.mu.RUnlock()
	seen := make(map[*session]struct{})
	for _, s := range m.sessions {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		s.mu.Lock()
		active := s.active(now)
		s.mu.Unlock()
		if active {
			return true
		}
	}
	return false
}

// RequestHandoff makes the browser read-only and waits for its flush ack.
func (m *Manager) RequestHandoff(ctx context.Context, id string) error {
	s := m.sessionByID(id)
	if s == nil {
		return os.ErrNotExist
	}
	s.mu.Lock()
	if s.status != sessionOpen {
		s.mu.Unlock()
		return errors.New("browser session is unavailable")
	}
	s.status = sessionHandingOff
	s.broadcastLocked(streamEvent{Name: "handoff", Data: map[string]string{"reason": "TUI requested ownership"}})
	ready := s.handoffReady
	s.mu.Unlock()
	select {
	case <-ready:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-m.ctx.Done():
		return errors.New("browser editor is closed")
	}
}

// CancelHandoff returns ownership to the existing browser writer after the TUI
// declines a timed-out or failed takeover.
func (m *Manager) CancelHandoff(id string) {
	s := m.sessionByID(id)
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.status == sessionHandingOff {
		s.status = sessionOpen
		s.handoffReady = make(chan struct{})
		s.handoffOnce = sync.Once{}
		s.broadcastLocked(streamEvent{Name: "resume", Data: s.document})
	}
	s.mu.Unlock()
}

// PrepareRecovery durably rebases the accepted browser body onto current disk
// metadata before a TUI takeover.
func (m *Manager) PrepareRecovery(id string) error {
	s := m.sessionByID(id)
	if s == nil {
		return os.ErrNotExist
	}
	s.mu.Lock()
	body, dirty, path := s.draftBody, s.dirty, s.path
	s.mu.Unlock()
	if !dirty {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	merged, err := MergeBody(string(raw), body)
	if err != nil {
		return err
	}
	return editdraft.Write(path, []byte(merged))
}

// Invalidate revokes a capability while retaining its recovery sidecar.
func (m *Manager) Invalidate(id, reason string) {
	s := m.sessionByID(id)
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.status != sessionInvalid {
		s.status = sessionInvalid
		s.reason = reason
		s.leaseToken = ""
		s.broadcastLocked(streamEvent{Name: "invalidated", Data: map[string]string{"reason": reason}})
	}
	s.mu.Unlock()
	m.mu.Lock()
	delete(m.byPath, s.path)
	m.mu.Unlock()
}

func (m *Manager) sessionByID(id string) *session {
	m.mu.RLock()
	s := m.sessions[id]
	m.mu.RUnlock()
	return s
}

func (m *Manager) sessionByToken(token string) *session { return m.sessionByID(token) }

// Close stops the listener, streams, watchers, and save producers. It never
// removes dirty recovery sidecars.
func (m *Manager) Close() error {
	var result error
	m.closeOnce.Do(func() {
		m.mu.Lock()
		m.closed = true
		m.cancel()
		server := m.server
		watcher := m.watcher
		unique := make(map[*session]struct{})
		for _, s := range m.sessions {
			unique[s] = struct{}{}
		}
		m.mu.Unlock()
		for s := range unique {
			s.mu.Lock()
			s.status = sessionInvalid
			s.reason = "application closed"
			s.broadcastLocked(streamEvent{Name: "invalidated", Data: map[string]string{"reason": s.reason}})
			s.mu.Unlock()
		}
		if watcher != nil {
			_ = watcher.Close()
		}
		if server != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			result = server.Shutdown(ctx)
			cancel()
		}
		m.watchWG.Wait()
		m.serverWG.Wait()
		close(m.saves)
	})
	return result
}

func canonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	return filepath.Clean(resolved), nil
}

func randomToken(bytes int) (string, error) {
	b := make([]byte, bytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate editor capability: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
