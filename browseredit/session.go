package browseredit

import (
	"context"
	"sync"
	"time"
)

const writerLeaseTTL = 45 * time.Second

type sessionStatus uint8

const (
	sessionOpen sessionStatus = iota
	sessionHandingOff
	sessionInvalid
)

// Card identifies a filesystem-backed card. Path remains server-side; the
// display-only labels and link targets are the only fields sent to the page.
type Card struct {
	Path        string
	BoardName   string
	ColumnName  string
	CardName    string
	LinkTargets []LinkTarget
}

// LinkTarget is a filesystem card exposed to the browser for wiki-link
// completion. It deliberately contains display names only, never paths.
type LinkTarget struct {
	Name   string `json:"name"`
	Column string `json:"column"`
}

// OpenedSession identifies a manager-owned browser session.
type OpenedSession struct {
	ID  string
	URL string
}

// SaveRequest is the mutation request delivered to the Board update loop.
type SaveRequest struct {
	SessionID    string
	BaseRevision string
	Body         string
	Reply        chan SaveResult
}

// SaveResult is returned by the Board after its final path and revision checks.
type SaveResult struct {
	Document Document `json:"document"`
	Err      error    `json:"-"`
	Conflict bool     `json:"conflict,omitzero"`
	Gone     bool     `json:"gone,omitzero"`
}

// SessionState is the edit-ownership view consumed by the TUI and Git guard.
type SessionState struct {
	ID         string
	Path       string
	Active     bool
	HandingOff bool
	Dirty      bool
	Invalid    bool
	Reason     string
}

type streamEvent struct {
	Name string
	Data any
}

type session struct {
	mu sync.Mutex

	id, token string
	path      string
	card      Card
	raw       string
	document  Document
	draftBody string
	dirty     bool
	conflict  bool
	status    sessionStatus
	reason    string

	leaseToken      string
	leaseClient     string
	leaseClaimedAt  time.Time
	lastHeartbeat   time.Time
	leaseGeneration uint64

	subscribers  map[chan streamEvent]struct{}
	handoffReady chan struct{}
	handoffOnce  sync.Once
	watchCancel  context.CancelFunc
}

func (s *session) leaseLive(now time.Time) bool {
	return s.leaseToken != "" && !s.lastHeartbeat.IsZero() && now.Sub(s.lastHeartbeat) <= writerLeaseTTL
}

func (s *session) leaseOwned(now time.Time) bool {
	if s.leaseToken == "" {
		return false
	}
	if !s.lastHeartbeat.IsZero() {
		return now.Sub(s.lastHeartbeat) <= writerLeaseTTL
	}
	return !s.leaseClaimedAt.IsZero() && now.Sub(s.leaseClaimedAt) <= writerLeaseTTL
}

func (s *session) active(now time.Time) bool {
	return s.status == sessionHandingOff || (s.status == sessionOpen && s.leaseLive(now))
}

func (s *session) state(now time.Time) SessionState {
	return SessionState{
		ID: s.id, Path: s.path, Active: s.active(now),
		HandingOff: s.status == sessionHandingOff, Dirty: s.dirty,
		Invalid: s.status == sessionInvalid, Reason: s.reason,
	}
}

func (s *session) validateLease(token string, now time.Time, allowHandoff bool) bool {
	if s.status == sessionInvalid || token == "" || token != s.leaseToken {
		return false
	}
	if s.status == sessionHandingOff && !allowHandoff {
		return false
	}
	return s.leaseOwned(now)
}

func (s *session) broadcastLocked(event streamEvent) {
	for ch := range s.subscribers {
		select {
		case ch <- event:
		default:
		}
	}
}
