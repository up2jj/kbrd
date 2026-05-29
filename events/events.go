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

// BoardRefresh fires after columns are reloaded from disk (watcher event,
// explicit refresh, or after a custom command).
type BoardRefresh struct {
	Reason string // "watcher" | "command" | "refresh"
}

func (BoardRefresh) eventTag() {}

// ItemSelect fires when the cursor lands on a different item than before.
// Prev is the zero ItemRef on the very first selection.
type ItemSelect struct {
	Item ItemRef
	Prev ItemRef
}

func (ItemSelect) eventTag() {}

// ColumnChange fires when the active column index changes.
type ColumnChange struct {
	Column string
	Prev   string
}

func (ColumnChange) eventTag() {}

// ItemOpen fires when the user opens an item for editing (internally or
// externally). Append/Prepend/Journal also count.
type ItemOpen struct {
	Item ItemRef
	Kind string // "edit" | "append" | "prepend" | "journal" | "external"
}

func (ItemOpen) eventTag() {}

// ItemCreated fires after Column.CreateItem succeeds.
type ItemCreated struct {
	Item ItemRef
}

func (ItemCreated) eventTag() {}

// ItemRenamed fires after Column.RenameItem succeeds.
type ItemRenamed struct {
	Item    ItemRef
	OldName string
}

func (ItemRenamed) eventTag() {}

// ItemDeleted fires after a delete confirmation completes.
type ItemDeleted struct {
	Column string
	Name   string
}

func (ItemDeleted) eventTag() {}

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
//
// FS* paths may be absolute or relative; relative paths are resolved
// against the board root by the implementation.
type BoardAPI interface {
	// Notify shows a non-blocking toast. level is one of "info", "success", "error".
	Notify(msg, level string)
	// MoveItem moves the item identified by item to the column named toColumn.
	MoveItem(item ItemRef, toColumn string) error

	// Filesystem primitives.
	FSRead(path string) (string, error)
	FSWrite(path, body string) error
	FSExists(path string) bool
	FSMkdir(path string) error
	FSGlob(pattern string) ([]string, error)

	// Refresh re-reads columns and git stats from disk.
	Refresh() error
	// CreateColumn creates a new column directory under the board root and
	// refreshes. Validates name (no separators, not . or ..).
	CreateColumn(name string) error

	// CellSet adds or replaces a header cell identified by id. CellClear removes
	// one; CellClearAll removes every script-set cell (built-ins are kept).
	CellSet(id int, opts CellOpts)
	CellClear(id int)
	CellClearAll()
}

// CellOpts is the appearance/content of a header cell set from a script.
// FG/BG are "#rrggbb" hex strings (or "" for the terminal default).
type CellOpts struct {
	Text string
	FG   string
	BG   string
	Bold bool
}

// Logger is the structured logging sink the script subsystem writes to.
// The default implementation writes to ~/.cache/kbrd/script.log.
type Logger interface {
	Log(level, source, msg string)
}

// NopLogger discards all log entries. Useful in tests.
type NopLogger struct{}

func (NopLogger) Log(_, _, _ string) {}
