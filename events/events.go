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

// Event name constants are the string identifiers used by the scripting layer
// (Lua kbrd.on) and by declarative YAML hooks. They are the single source of
// truth for the wire names — both script/ and the model's hook runner reference
// these instead of repeating literals.
const (
	NameBoardLoad     = "board_load"
	NameBoardRefresh  = "board_refresh"
	NameItemSelect    = "item_select"
	NameColumnChange  = "column_change"
	NameItemOpen      = "item_open"
	NameItemCreated   = "item_created"
	NameItemRenamed   = "item_renamed"
	NameItemDeleted   = "item_deleted"
	NameItemMoved     = "item_moved"
	NameColumnCreated = "column_created"
	NameGitSyncDone   = "git_sync_done"
	// NameColumnItems is the Lua-only transform hook fired when a filesystem
	// column's items are (re)built. Unlike the action events above it expects a
	// return value (the reordered/filtered item list), so it is never part of
	// actionEvents and cannot be bound by declarative YAML hooks.
	NameColumnItems = "column_items"
)

// actionEvents is the set of low-frequency "action" events that declarative
// YAML hooks may bind to. The high-frequency events (item_select, column_change,
// board_refresh) are deliberately excluded: a slow hook on a per-keystroke or
// per-watcher-tick event would back up the serial hook queue. Those remain
// Lua-only via kbrd.on, where the user can add their own throttling.
var actionEvents = map[string]bool{
	NameBoardLoad:     true,
	NameItemOpen:      true,
	NameItemCreated:   true,
	NameItemRenamed:   true,
	NameItemDeleted:   true,
	NameItemMoved:     true,
	NameColumnCreated: true,
	NameGitSyncDone:   true,
}

// IsHookable reports whether a declarative YAML hook may bind to the given event
// name. See actionEvents for the rationale behind the allowed set.
func IsHookable(name string) bool { return actionEvents[name] }

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

// ColumnCreated fires after a new column directory is created.
type ColumnCreated struct {
	Name string
}

func (ColumnCreated) eventTag() {}

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
// Keep this narrow. The mutation methods intentionally mirror the user-facing
// board actions (move/create/rename/delete an item, create a column) and route
// through the model's centralized mutators so every entry point publishes the
// same events. Adding a method here widens the script↔model contract and forces
// a matching change in the implementation and its test doubles — so add one
// only for a genuine board mutation that should also fire events, not for
// incidental conveniences (those belong behind FS* or a script-side helper).
//
// FS* paths may be absolute or relative; relative paths are resolved
// against the board root by the implementation.
type BoardAPI interface {
	// Notify shows a non-blocking toast. level is one of "info", "success", "error".
	Notify(msg, level string)
	// MoveItem moves the item identified by item to the column named toColumn.
	MoveItem(item ItemRef, toColumn string) error
	// CreateItem creates a new (empty) item named name in the named column.
	CreateItem(column, name string) error
	// ListTemplates lists the card templates available to the named column
	// (column-local merged with board-level; column wins on a name clash).
	ListTemplates(column string) ([]TemplateInfo, error)
	// CreateItemFromTemplate renders the named template with the given field
	// values and creates the resulting card in the named column. Field
	// defaults apply for omitted keys; required fields must be non-empty.
	CreateItemFromTemplate(column, template string, values map[string]interface{}) error
	// RenameItem renames the item identified by item to newName (same column).
	RenameItem(item ItemRef, newName string) error
	// DeleteItem deletes the item identified by item.
	DeleteItem(item ItemRef) error

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

	// VirtualColumnSet creates or replaces a script-supplied (virtual) column
	// identified by id. VirtualColumnClear removes one; VirtualColumnClearAll
	// removes every virtual column. Virtual columns hold script-pushed items and
	// have no filesystem backing — file moves into them are rejected.
	VirtualColumnSet(id string, spec VirtualColumnSpec)
	VirtualColumnClear(id string)
	VirtualColumnClearAll()
}

// TemplateInfo describes one card template available to a column, as exposed
// to scripts. Scope is "column" or "board" depending on where the template
// file lives.
type TemplateInfo struct {
	Name  string
	Scope string
}

// VirtualItem is one entry a script pushes into a virtual column via
// kbrd.column.set. All fields except Title are optional. Separator marks an
// inert grouping row (only Title/Accent apply). Data is an opaque payload that
// round-trips back into a command's ctx so the producing script can act on it.
type VirtualItem struct {
	ID        string
	Title     string
	Preview   string
	Meta      string
	Icon      string
	Accent    string
	Path      string
	Separator bool
	Data      map[string]interface{}
}

// VirtualCommand is a column-scoped command (B) declared inside kbrd.column.set.
// Ref is the opaque dispatch handle the host resolves back to the Lua run fn.
type VirtualCommand struct {
	ID      string
	Name    string
	Key     string
	Default bool
	Ref     string
}

// VirtualColumnSpec is the full payload of a kbrd.column.set call.
type VirtualColumnSpec struct {
	Name     string
	Empty    string
	Items    []VirtualItem
	Commands []VirtualCommand
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
