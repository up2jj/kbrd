// Package events defines the wire types for kbrd's scripting subsystem.
//
// This package is intentionally dependency-free with respect to the rest of
// kbrd: model/ may import events/ to publish, and script/ may import events/
// to subscribe — but events/ never imports either. That one-way arrow lets
// the scripting feature be removed by deleting a single import in main.go.
package events

// ItemRef identifies an item by column + filename. Both fields are the
// human-visible names — column directory name and item Name (without the .md
// suffix), matching kbrd's existing terminology.
type ItemRef struct {
	Column string
	Name   string
}

// Event is the closed sum type published on the Bus.
type Event interface{ eventTag() }

// GitSyncDone fires after a git sync attempt (auto or manual) finishes.
type GitSyncDone struct {
	OK    bool
	Stage string
	Err   string
}

func (GitSyncDone) eventTag() {}

// ItemMoved fires after a file is moved between columns.
type ItemMoved struct {
	Item ItemRef
	From string
	To   string
}

func (ItemMoved) eventTag() {}

// BoardLoad fires after the board's columns are first populated.
type BoardLoad struct{}

func (BoardLoad) eventTag() {}

// Subscriber receives events. Implementations must be safe to call from the
// Bubble Tea goroutine; they should not block. Return errors are logged
// (via Logger) but never propagate up to the board.
type Subscriber interface {
	OnEvent(Event)
}

// Bus is the publish/subscribe entry point. The model publishes; the script
// subsystem subscribes. The default zero value is a no-op bus — safe to use
// even when scripting is disabled.
type Bus struct {
	subs []Subscriber
}

// Subscribe registers a Subscriber. Not safe for concurrent calls; intended
// to be called once during board setup.
func (b *Bus) Subscribe(s Subscriber) {
	if b == nil || s == nil {
		return
	}
	b.subs = append(b.subs, s)
}

// Publish dispatches an event to every subscriber. Panics from subscribers
// are recovered so a broken script can never crash the host.
func (b *Bus) Publish(ev Event) {
	if b == nil {
		return
	}
	for _, s := range b.subs {
		safeDispatch(s, ev)
	}
}

func safeDispatch(s Subscriber, ev Event) {
	defer func() { _ = recover() }()
	s.OnEvent(ev)
}

// BoardAPI is the narrow surface the script subsystem calls into when a
// Lua script wants to mutate the board or talk to the user. Methods MUST
// be safe to invoke from any goroutine — the implementation is responsible
// for routing onto the UI thread if needed.
type BoardAPI interface {
	// Notify shows a non-blocking toast. level is one of "info", "success", "error".
	Notify(msg, level string)
	// MoveItem moves the item identified by item to the column named toColumn.
	MoveItem(item ItemRef, toColumn string) error
}

// Logger is the structured logging sink the script subsystem writes to.
// The default implementation writes to ~/.cache/kbrd/script.log.
type Logger interface {
	Log(level, source, msg string)
}

// NopLogger discards all log entries. Useful in tests.
type NopLogger struct{}

func (NopLogger) Log(_, _, _ string) {}
