# Browser WYSIWYG Markdown Editor Plan

## Status

Ready to implement.

This plan adds an optional browser-based WYSIWYG editor for one selected card
while the normal kbrd TUI remains running. The browser is a new editing surface,
not a new mutation subsystem: recovery drafts may be written by the browser
service, but every real card save must return to the Bubble Tea model and use
the existing card-save, refresh, event, hook, watcher, and Git-state machinery.

## Goals

- Let a user open the selected filesystem-backed card in a browser editor.
- Start in WYSIWYG mode and retain a Markdown/source-mode switch.
- Synchronize explicit browser saves to the existing local card file.
- Reflect external card-file changes in the browser without polling.
- Preserve frontmatter byte-for-byte while WYSIWYG edits only the Markdown body.
- Persist frequent recovery drafts locally without triggering hooks.
- Prevent the browser and TUI editors from independently editing the same card.
- Embed all browser assets in the kbrd binary.
- Keep the build and test workflow Go-only; introduce no JavaScript build
  pipeline or runtime CDN dependency.

## Non-goals

The first version will not:

- replace the existing in-terminal editor or `$EDITOR` action;
- change `kbrd serve` or reuse its public-server authentication and Git commit
  behavior;
- expose the browser editor beyond the local machine;
- edit card names, columns, or frontmatter fields in the WYSIWYG page;
- upload or manage card attachments;
- guarantee byte-for-byte preservation of body Markdown after a deliberate
  WYSIWYG edit (equivalent Markdown may be normalized);
- provide collaborative or multi-user editing;
- prevent another process from editing the file;
- provide a portable atomic compare-and-swap against arbitrary, uncooperative
  filesystem writers (the final revision check narrows this race but cannot
  eliminate a write that lands between that check and the atomic rename); or
- load board-provided HTML, JavaScript, CSS, or web-template overrides.

## Product decisions

1. Add a dedicated `edit in browser` action bound to `B`. Existing `e` and `o`
   behavior remains unchanged.
2. The browser service starts lazily on the first browser-edit action and binds
   only to `127.0.0.1` on an operating-system-assigned port.
3. Browser sessions use unguessable capability URLs and remain valid only for
   the lifetime of the current Board.
4. Browser changes are persisted frequently to the existing hidden
   `.<filename>.kbrd-swap` recovery file, never to the card itself.
5. The real card is written only when the user selects Save or presses
   `Ctrl/Cmd+S`.
6. A real browser save is handled on the Bubble Tea update loop through the
   same mutation boundary as a TUI editor save.
7. A real save publishes one `item_saved` event with `kind = "browser"`.
   Subsequent watcher refresh behavior remains the same as for an existing
   in-app save.
8. Browser and TUI editing are mutually exclusive per card. An attempted TUI
   edit while the browser session is active opens an explicit handoff dialog.
9. Server-to-browser file updates use Server-Sent Events (SSE). Browser-to-server
   draft and save operations use ordinary HTTP JSON requests.
10. The first editor implementation vendors the pinned plain-JavaScript TOAST
    UI Editor distribution. Assets are committed and embedded with `go:embed`.
11. A card session has at most one browser writer lease. Additional tabs for the
    same capability are read-only observers until the writer closes or its
    application heartbeat expires; they can never submit a draft or save.
12. Optimistic concurrency rejects every disk change visible at the Board's
    final pre-write revision check. It does not claim a cross-platform atomic
    compare-and-swap against an arbitrary process racing the final replacement.

## Current foundations to preserve

The implementation must build on these existing paths rather than duplicate
them:

- `model/board_mutation_handlers.go`: resolves stable targets and owns the
  in-app mutation completion path.
- `board.ReplaceFileContent`: refuses to recreate a deleted card and preserves
  atomic, durable replacement semantics.
- `model.finalizeItemSave`: reloads the affected column, restores selection,
  and publishes `events.ItemSaved` for hooks.
- `model/editor_swap.go`: defines the current hidden recovery sidecar behavior.
- `model/watch.go`: ignores `*.kbrd-swap` and debounces real filesystem changes.
- `frontmatter.Split`: supplies the existing frontmatter semantics; add an exact
  prefix-preserving splitter in that package for browser documents, including
  LF and CRLF fences.
- `model/open.go`: opens a file or URL through the platform browser/application
  launcher.
- `model.Board.Close` and `boardSession.loadBoard`: own process and board-switch
  resource cleanup.
- `git.Deps.EditorActive`: pauses automatic synchronization while an editor is
  active.

## User experience

### Opening a browser editor

With a filesystem card selected, the user presses `B` or selects
`edit in browser` from the action menu.

kbrd:

1. Captures the same stable item reference used by delayed TUI mutations.
2. Rejects virtual/non-filesystem cards.
3. Creates or reuses a browser session for the canonical card path.
4. Starts the loopback server if it is not already running.
5. Opens the session URL with the existing platform launcher.
6. Publishes `events.ItemOpen{Kind: "browser"}`.
7. Shows a short notification such as `opened task-name in browser`.

Opening the same card again reuses its active session URL. If that URL opens in a
second tab while the original writer is active, the second tab displays the
document read-only with `already open in another browser tab`; it does not share
the writer lease. Opening another card creates another session under the same
loopback server.

### Browser page

The page contains:

- board, column, and card labels;
- a WYSIWYG/Markdown mode switch;
- a deliberately limited formatting toolbar;
- a read-only indication that frontmatter is preserved outside the editor;
- a Save button;
- `saving draft`, `unsaved`, `saved`, `conflict`, `offline`, and `read-only`
  status states; and
- conflict and handoff banners when required.

Each page creates a random browser-client ID and claims the session's writer
lease before enabling the editor. The manager returns a separate random lease
token, retained only in that tab's `sessionStorage` and sent in the
`X-Kbrd-Editor-Lease` header. A tab that cannot claim the lease remains a
read-only observer. A writer serializes its own draft/save requests so only one
state-changing request is in flight.

Do not include the image-upload toolbar action in version one. Existing image
Markdown remains text, but kbrd does not add an attachment-serving or upload
surface in this feature.

Opening a page, changing editor mode, or applying a remote `setMarkdown` must
not write a draft or card until an actual user edit occurs. The JavaScript must
use explicit `loading` and `applyingRemote` guards around editor change events.

### Draft and save behavior

After a user edit, the page submits a recovery draft after 500-750 ms of
inactivity. The status may show `draft saved locally`, but must not call the
card `saved` until the real save succeeds.

The user selects Save or presses `Ctrl/Cmd+S` to write the card. A successful
save:

1. passes a revision-checked request to the Board;
2. writes the card through the shared existing-card mutation path;
3. reloads the column and restores selection;
4. publishes `item_saved` with `kind = "browser"`;
5. clears the recovery sidecar;
6. updates the browser revision and clean baseline; and
7. reports `saved` in both browser and TUI notification surfaces.

There is no unconditional save-on-tab-close. Browser close events are not
reliable enough to be a durability boundary. A dirty close leaves the recovery
sidecar so the next browser or TUI edit can offer recovery.

## Package and file layout

Introduce one focused top-level package for the loopback editor and one small
shared package for editor recovery drafts:

```text
kbrd/
├── browseredit/
│   ├── assets.go
│   ├── document.go
│   ├── manager.go
│   ├── server.go
│   ├── session.go
│   ├── watch.go
│   ├── *_test.go
│   ├── index.html
│   └── static/
│       ├── app.js
│       ├── style.css
│       ├── toastui-editor.min.js
│       ├── toastui-editor.min.css
│       ├── toastui-editor.LICENSE
│       └── VENDOR.md
├── editdraft/
│   ├── draft.go
│   └── draft_test.go
└── model/
    ├── browser_editor.go
    ├── browser_editor_test.go
    ├── board_mutation_handlers.go
    ├── board_item_actions.go
    ├── item_action_specs.go
    ├── keys.go
    ├── editor_swap.go
    └── ...
```

Dependency direction:

```text
frontmatter ─┐
             ├──> browseredit ──> net/http + fsnotify
editdraft ───┘

board + events + browseredit ──> model
```

`browseredit` must not import `model`. `model` owns stable board references,
mutation semantics, notifications, and event publication. Start with concrete
types; do not introduce an interface until a second implementation requires
one.

## Shared recovery drafts

Move only the generic sidecar operations from `model/editor_swap.go` into
`editdraft`; retain terminal-editor state and recovery dialogs in `model`.

Proposed API:

```go
package editdraft

func Path(documentPath string) string
func Read(documentPath string) ([]byte, error)
func Write(documentPath string, content []byte) error
func Clear(documentPath string) error
```

`Path` returns the existing format:

```text
<column>/.<filename>.kbrd-swap
```

`Write` uses `fs.WriteFileAtomicDurable`. `Clear` treats a missing sidecar as
success. Refactor the TUI Vim editor to use these helpers without changing its
observable behavior or recovery tests.

The browser draft contains a complete recoverable card, not body-only data. On
every draft persistence, dirty external change, and handoff preparation, it is
assembled from the newest valid exact disk frontmatter prefix and the manager's
last accepted browser body. Consequently, taking over or recovering after a
crash in the TUI can reuse the existing recovery prompt without a
browser-specific draft format and without restoring an older valid metadata
prefix. Apparent malformed metadata is the fail-closed exception described
below.

## Document representation

`browseredit/document.go` owns pure document conversion and revision helpers.
Add `frontmatter.SplitExact` so the frontmatter package, rather than the browser
package, continues to own fence recognition:

```go
func SplitExact(raw string) (prefix, block, body string, fenced, apparent bool)
```

`SplitExact` recognizes opening and closing fences terminated by either LF or
CRLF and returns `prefix` as an exact slice of `raw`, including the original
fences, line endings, comments, and spacing. `apparent` is true when the first
logical line is `---` but no complete closing fence exists. Keep the existing
`Split` API for current callers, implementing it from the same scanner so the
two functions cannot disagree about whether a block is complete.

The browser helpers are:

```go
type Document struct {
	Body               string `json:"body"`
	Revision           string `json:"revision"`
	FrontmatterPresent bool   `json:"frontmatterPresent"`
	WYSIWYGSafe        bool   `json:"wysiwygSafe"`
	Warning            string `json:"warning,omitzero"`
}

func ParseDocument(raw string) Document
func MergeBody(currentRaw, body string) (string, error)
func Revision(raw string) string
```

`Revision` is the lowercase SHA-256 of the complete raw card bytes. It is the
optimistic-concurrency token and HTTP ETag value.

For a well-formed leading frontmatter block, `ParseDocument` exposes only its
body. `MergeBody` uses `frontmatter.SplitExact` and combines its exact current
prefix with the new edited body. This preserves LF or CRLF fences, comments,
ordering, spacing, unknown keys, and current external metadata bytes.

If a file begins with an apparent `---` fence but `frontmatter.SplitExact`
cannot find a complete block, mark it unsafe and default to read-only source
mode with a warning. `MergeBody` returns an error for that state. Do not expose
apparent malformed metadata as an editable body or silently send it through
WYSIWYG.

Normalize only the trailing newline through the established
`board.ReplaceFileContent` behavior. Do not separately reformat frontmatter.

## Browser editor manager

`browseredit.Manager` owns:

- one lazy `net.Listener` bound to `127.0.0.1:0`;
- one `http.Server` with bounded read/idle and non-streaming handler timeouts
  plus per-write SSE deadlines;
- immutable embedded assets;
- session lookup by random capability token;
- canonical-path-to-session lookup for reuse and edit guards;
- SSE subscribers;
- parent-directory fsnotify watches for active cards;
- a bounded channel of real-save requests addressed to the Board; and
- cancellation and graceful shutdown.

Representative API:

```go
type Card struct {
	Path       string
	BoardName  string
	ColumnName string
	CardName   string
}

type OpenedSession struct {
	ID  string
	URL string
}

type SaveRequest struct {
	SessionID    string
	BaseRevision string
	Body         string
	Reply        chan SaveResult
}

type SaveResult struct {
	Document Document
	Err      error
	Conflict bool
	Gone     bool
}

func New() *Manager
func (m *Manager) Open(card Card) (OpenedSession, error)
func (m *Manager) SaveRequests() <-chan SaveRequest
func (m *Manager) SessionForPath(path string) (SessionState, bool)
func (m *Manager) URL(id string) (string, bool)
func (m *Manager) RequestHandoff(ctx context.Context, id string) error
func (m *Manager) PrepareRecovery(id string) error
func (m *Manager) Invalidate(id, reason string)
func (m *Manager) Active() bool
func (m *Manager) Close() error
```

`SaveRequest.Reply` is buffered with capacity one. HTTP handlers select among
the reply, request cancellation, and manager shutdown so a canceled client
cannot leak a goroutine or block the Bubble Tea loop.

The manager's goroutines all stop from its root context. `Close` is idempotent,
closes the listener, cancels SSE streams and watchers, invalidates sessions,
and closes the save-request channel only after producers have stopped.

## Session state

Each session holds, behind its own mutex:

- immutable ID and capability token;
- canonical absolute card path and display labels;
- last accepted complete raw baseline and revision;
- current draft body and dirty flag;
- one browser writer lease token, client ID, and lease generation;
- last successful application heartbeat from that writer;
- active/inactive/handing-off/invalid state;
- SSE subscribers; and
- the latest invalidation reason.

Generate capability and writer-lease tokens with `crypto/rand`, using at least
24 random bytes encoded with base64url without padding. Browser-client IDs use
at least 16 random bytes from `crypto.getRandomValues`. Do not derive any of
them from paths, names, timestamps, `math/rand`, or process identifiers.

Only a valid writer application heartbeat marks a session active. The writer
sends one every 15 seconds; after 45 seconds without one, the lease expires and
the session becomes inactive for edit-lock and Git-pause purposes. An open SSE
connection, SSE comment heartbeat, ordinary document GET, or observer-tab
heartbeat never extends writer ownership. The draft and capability may remain
until invalidated, board switch, or Board close. A new tab may claim a writer
lease only when no lease exists or the previous lease has expired.

## HTTP protocol

All routes live under the capability path:

```text
GET    /s/{token}/
GET    /s/{token}/document
POST   /s/{token}/claim
PUT    /s/{token}/draft
POST   /s/{token}/save
DELETE /s/{token}/draft
GET    /s/{token}/events
POST   /s/{token}/heartbeat
POST   /s/{token}/handoff-ready
POST   /s/{token}/close
GET    /static/{asset...}
```

`POST claim` accepts a random browser-client ID. It returns a random writer
lease when no live writer owns the session, resumes the same unexpired lease
when the request proves possession of it, and otherwise returns `409 Conflict`
with read-only session state. Every draft, save, draft-delete, heartbeat,
handoff-ready, and close request must present that lease in
`X-Kbrd-Editor-Lease`. Missing, expired, superseded, or wrong leases are
rejected without changing session state. `close` releases only the presenting
writer's lease. Observer tabs may use document and SSE routes but cannot mutate.

### `GET document`

Read the current card, return its parsed body and full-file revision, and set a
quoted ETag. If a recoverable swap differs from the card, return draft metadata
so the writer page can offer `Recover` or `Discard` before editing; observer
pages display its presence but cannot act on it. Parse a recoverable sidecar as
a complete document and expose its body separately from its old prefix. On
Recover, immediately rewrite it by merging that body with the newest valid disk
prefix before enabling edits. Malformed current or recovery metadata fails
closed instead of exposing metadata as the editable body.

### `PUT draft`

Request:

```json
{
  "baseRevision": "sha256...",
  "body": "edited markdown"
}
```

The manager records the supplied browser body, reads the current card, and calls
`MergeBody(currentRaw, body)` so the recovery file carries the newest valid disk
frontmatter prefix rather than the session's opening prefix. It writes that
complete candidate to `editdraft.Write`, updates session dirty state, sends no
Board message, and publishes no event. Limit the request body to 1 MiB.

Draft persistence is permitted even after an external card change so the
browser version is not lost. The response must separately indicate that the
baseline is stale and the UI must enter conflict state. If the current disk
document has apparent malformed metadata, keep the previously durable sidecar,
return the malformed-metadata conflict, and do not construct a replacement from
ambiguous bytes.

### `POST save`

Request data matches `PUT draft`. The HTTP handler does not write the card. It
sends a `SaveRequest` to the manager channel and waits for the Board result.

Responses:

| Status | Meaning |
| --- | --- |
| `200 OK` | Saved through the Board; returns new document and ETag |
| `412 Precondition Failed` | Whole-file revision changed; returns current document |
| `409 Conflict` | Writer lease is absent/stale, or the session is handing off |
| `404 Not Found` | Unknown capability/session |
| `410 Gone` | Known session was invalidated, or its card was deleted or moved |
| `413 Content Too Large` | Request exceeded the body limit |
| `500 Internal Server Error` | Existing mutation failed; draft remains |
| `503 Service Unavailable` | Board loop or manager is shutting down |

After a successful Board result, the manager updates the session baseline,
clears dirty state and the recovery sidecar, and broadcasts the saved document
to read-only observer tabs for that session. Observers never apply an event to
an editable buffer because only the lease holder may enable editing.

### `GET events`

Use `text/event-stream` with events:

```text
document       clean external/saved document revision
conflict       disk changed while a browser draft is dirty
handoff        TUI requested ownership; page becomes read-only and flushes draft
invalidated    board switched, TUI took over, card disappeared, or app closed
```

Send the current state immediately after connection and a comment heartbeat at
least every 20 seconds. Flush after each event. The handler exits on request or
manager context cancellation. SSE comments are transport liveness only and do
not renew the writer lease.

Because SSE responses are intentionally unbounded, configure the shared
`http.Server` with `WriteTimeout: 0`. Keep bounded `ReadHeaderTimeout`,
`ReadTimeout`, and `IdleTimeout`; wrap every non-streaming handler with a bounded
handler context. For SSE, use `http.NewResponseController` to set a short write
deadline immediately before each event/comment write, flush it, then clear the
deadline. A slow or disconnected client therefore cannot block a stream
goroutine, while a healthy stream is not killed by a request-wide deadline.

## Real-save integration with Bubble Tea

The HTTP goroutine must never call `board.ReplaceFileContent` directly.

When the first browser session starts, arm one Bubble Tea command that waits on
`Manager.SaveRequests()`. Route each request through a
`browserEditorSaveRequestMsg`, handle it on the update loop, and immediately
re-arm the wait command. Ensure only one wait command is armed at a time.

`model` maintains a session-ID-to-target record containing both the captured
`itemRefStable` and the expected canonical absolute path. The stable reference
is captured when the browser action opens the session; HTTP-provided paths and
names are never trusted as mutation targets. Browser saves require the resolved
item path to equal the captured canonical path exactly; do not use
`resolveItemRef`'s filename fallback after an `ItemPath` miss.

Refactor `boardMutationHandlers.writeExistingItem` into a shared core that:

1. resolves the stable target;
2. refuses missing, renamed, moved-to-an-unexpected-target, or virtual items;
3. performs the supplied existing-file write;
4. reloads the affected column;
5. restores selection by item identity; and
6. publishes `events.ItemSaved` with the requested kind.

Keep frontend-specific behavior outside the core:

- TUI editor: `editor.confirmSaved()` and the existing notification text.
- Browser editor: send `SaveResult`, clear the draft after success, and use a
  browser-specific notification.

Browser save handling performs these checks before invoking the core:

1. Read the currently resolved item path.
2. Return `Gone` if it no longer exists or resolves to another item.
3. Read the complete current file immediately before replacement and compare
   its revision with `BaseRevision`.
4. On mismatch, return `Conflict` with the current parsed document and do not
   write, refresh, publish, clear the draft, or run hooks.
5. Merge the edited body into the current raw document.
6. Invoke the shared core with `kind = "browser"` and
   `board.ReplaceFileContent`.

This final read and the replacement run consecutively on the Bubble Tea update
loop, so all kbrd Board mutations and Lua timer callbacks have a deterministic
order and cannot interleave. Portable filesystems do not provide a content-based
compare-and-swap for an arbitrary process: `$EDITOR`, Git, or another kbrd
process can still land a write in the narrow interval between the final read and
atomic rename and have that write replaced. The implementation must not claim
otherwise. A future cooperative cross-process lock may narrow kbrd-to-kbrd
races, but is not part of version one and cannot constrain uncooperative tools.

Update event comments and hook variables to accept `browser` as an
`ItemOpen.Kind` and `ItemSaved.Kind` value. No new hook event name is needed.

### Hook guarantees

The following behavior is normative and must have regression tests:

| Operation | `item_saved` | `item_changed` | `board_refresh` |
| --- | ---: | ---: | ---: |
| Browser draft write | 0 | 0 | 0 |
| Successful explicit browser save | 1 (`browser`) | 0 | existing watcher behavior |
| Stale/conflicting browser save | 0 | 0 | 0 from the rejected save |
| External editor write | 0 | 1 | 1 |
| Lua timer `kbrd.fs.write` with changed bytes | 0 | 1 | 1 |
| Hook rewrite after browser save | 0 additional | existing convergent behavior | existing watcher behavior |

Immediate column reload in the shared in-app save path must keep the later
watcher pass from reclassifying the browser save as `item_changed`, matching
the existing TUI editor contract.

## Browser/TUI mutual-exclusion guard

The same card must never have independent live browser and TUI buffers.

### Board-owned card-editor acquisition

Add one Board-owned acquisition helper in `model/browser_editor.go`. It accepts a
captured stable target plus the requested TUI surface and is the only supported
way to open an interactive buffer for an existing card. Route all of these paths
through it:

- `boardItemActions.edit`, `append`, `prepend`, and `journal`;
- `kbrd.editor.open`, including requests drained after Lua callbacks;
- the item frontmatter editor; and
- action-menu, help-menu, search, peek, mouse, and future call sites that lead to
  any of those surfaces.

The helper resolves the target, compares its canonical path with
`Manager.SessionForPath`, and either opens the requested surface or runs the
ownership dialog. Keep `Editor.OpenEdit` and the other low-level constructors
free of browser-manager dependencies, but do not call them directly from Board
features. `$EDITOR` remains an uncoordinated external editor and is deliberately
outside this acquisition API.

### TUI buffer attempted while browser is active

If the browser session is active, show a dialog:

```text
This card is being edited in the browser.

[Open browser] [Take over in TUI] [Cancel]
```

- **Open browser** reopens the existing capability URL and changes no state. The
  platform launcher cannot guarantee focusing an existing tab; if it creates a
  new tab, that tab is a read-only observer while the writer lease is live.
- **Cancel** changes no state.
- **Take over in TUI** starts an asynchronous handoff; it must not immediately
  open a competing buffer.

A takeover always opens the full-card TUI editor because it is the only TUI
surface that can present and resolve a complete recovery sidecar. If takeover
was initiated from append, prepend, journal, or frontmatter editing, do not
resume that specialized buffer automatically; the user may invoke it after
recovering, saving, or discarding the handed-off content.

### Handoff protocol

1. The Board asks `Manager.RequestHandoff` from a `tea.Cmd`.
2. The manager marks the session `handing-off` and sends an SSE `handoff` event.
3. Browser JavaScript immediately makes the editor read-only, flushes its
   current body to `PUT draft`, then calls `POST handoff-ready`.
4. After acknowledgement, the manager calls `PrepareRecovery`: it re-reads the
   card and durably rewrites the sidecar by merging the manager's last accepted
   browser body with the newest valid disk frontmatter prefix.
5. The manager completes the handoff only after that durable rebase succeeds.
6. Bubble Tea receives `browserHandoffReadyMsg`, invalidates the browser
   session, and opens the TUI editor for the captured stable target.
7. The normal TUI swap recovery prompt offers the rebased browser draft.

If current disk content has apparent malformed metadata, `PrepareRecovery`
cannot safely identify a prefix and the automatic handoff fails closed. Keep the
browser read-only and show the explicit Continue/Cancel confirmation below with
a warning that the last sidecar may contain an older metadata prefix.

Bound the acknowledgement wait to two seconds. On timeout, show a second
confirmation:

```text
The browser did not respond. Continue with the last locally recovered draft?

[Continue] [Cancel]
```

On Continue, call `PrepareRecovery` with the manager's last accepted browser
body before invalidating the session. This preserves the newest valid disk
frontmatter even when the browser did not acknowledge, while making possible
loss of its most recent unflushed keystrokes visible rather than silent. If the
rebase fails because current metadata is malformed, require a further explicit
confirmation before using the last sidecar unchanged.

Once handoff begins, reject real browser saves with `409 Conflict`; allow only
the final draft flush and handoff acknowledgement. After invalidation, all
browser routes except embedded static assets return `410 Gone`, and connected
pages display the invalidation reason.

### Browser edit attempted while TUI editor owns the card

Before opening a browser session, use the same Board acquisition state to check
the active full editor, append/prepend/journal inputs, and frontmatter editor for
the selected canonical path. If any owns it, refuse the action and notify:

```text
This card is already open in the TUI editor.
```

Normal key routing means this is uncommon, but the centralized guard is still
required for action-menu, mouse, script, and future call paths.

### Inactive or abandoned browser session

If the browser session is no longer active:

- call `PrepareRecovery` to rebase the last accepted browser body onto the
  newest valid disk frontmatter;
- invalidate its capability before opening the TUI;
- open the normal TUI editor;
- offer recovery when its sidecar differs from the card; and
- retain the sidecar until the user recovers, discards, or successfully saves.

### Uncoordinated external editors

`$EDITOR`, another kbrd process, Git, and arbitrary filesystem tools cannot use
the in-process ownership guard. Treat them as external writers:

- clean browser session: apply the new document automatically;
- dirty browser session: retain the draft and enter conflict state;
- save against a changed revision: return `412` without writing; and
- explicit `Keep my body`: retry using the latest revision while merging the
  user's body with the latest frontmatter.

These guarantees cover changes visible when the Board performs its final
revision read. They do not promise atomic exclusion of an uncooperative writer
that races the final atomic replacement; document that narrow limitation in the
user-facing conflict help rather than presenting the revision token as a file
lock.

### Lua timers and script-driven mutations

Do not pause Lua timers or remove their existing filesystem and board mutation
capabilities while a browser editor is open. The same-card ownership guard is
for the two interactive editors; it is not a lock on automation. Timer-driven
changes are authoritative concurrent mutations from the browser session's
perspective.

Timer callbacks arrive as `scriptTimerMsg` and execute on Bubble Tea's owning
goroutine. Browser `SaveRequest` handling must execute on that same update loop,
so a timer callback and an explicit browser save have a deterministic order.
The browser save handler must still re-read the card and compare its complete
file revision immediately before calling the shared mutation core.

Handle script mutation paths as follows:

- `kbrd.fs.write`, `kbrd.fs.set_frontmatter`, and
  `kbrd.fs.delete_frontmatter` remain direct filesystem mutations. Their
  watcher events feed the normal browser external-change flow: a clean session
  adopts the result; a dirty session retains its draft and enters conflict.
- A frontmatter-only timer change still changes the complete-file revision. If
  the user selects `Keep my body`, merge the browser body with the timer's
  latest frontmatter rather than restoring the stale frontmatter.
- If a timer rewrites the body, `Keep my body` is an explicit user decision to
  replace that new body. Never perform that overwrite automatically.
- A script-driven move, rename, or delete makes the session's captured path
  `Gone`. Invalidate that session, retain its recovery sidecar, and never
  silently follow the moved card or recreate the old path.
- If the timer mutation is processed first, a browser save based on the old
  revision returns `412` and retains its draft. If the browser save is
  processed first, the subsequent timer mutation remains effective and is
  broadcast by the watcher as the next document or conflict state.

Preserve current script semantics and event delivery. Direct script filesystem
writes continue through watcher classification; board API operations continue
to publish their existing move, delete, and refresh events. Do not add a
second `item_saved` event or a browser-specific hook for either path, and do not
introduce a long-lived file lock that delays timers.

## Filesystem watching and conflicts

Watch parent directories rather than card inodes because
`WriteExistingFileAtomicDurable` and common editors replace files by rename.
Reuse the repository's fsnotify wrapper and the Create/Write/Rename/Remove
operation policy already used by web configuration watching.

Coalesce bursts before reading. On an accepted card event:

1. Read the exact session path.
2. Mark the session gone if the path disappeared.
3. Calculate the complete-file revision.
4. Ignore an identical revision.
5. Parse the current body and frontmatter status.
6. If the browser session is clean, update its baseline and broadcast
   `document`.
7. If dirty and the new disk document has a safe exact prefix, keep the browser
   buffer unchanged but durably rebase the sidecar by merging the recorded
   browser body with that newest prefix, then broadcast `conflict`.
8. If dirty and the new disk document has apparent malformed metadata, keep the
   browser buffer and previous sidecar unchanged, mark the recovery prefix as
   potentially stale, and broadcast the stronger malformed-metadata conflict.

Self-generated swap events are already ignored by the TUI watcher and must also
be ignored by the browser card watcher. A successful real save may produce an
fsnotify event; revision equality deduplicates the browser-side echo.
Sidecar rebases are also ignored, so updating recovery frontmatter cannot create
a refresh loop. This eager rebase ensures that a process crash followed by
ordinary TUI recovery does not restore an older valid frontmatter prefix.

Conflict resolution is explicit:

- `Reload disk`: discard the browser draft, clear the sidecar, adopt current
  disk content and revision.
- `Keep my body`: retain the browser body, adopt the latest disk revision, and
  submit a new explicit save. The server merges it with the latest frontmatter.
- `Stay conflicted`: keep editing the local draft without touching the card.

## Git lifecycle

Extend `git.Deps.EditorActive` to return true when either:

- the existing TUI `EditorActive` condition is true, preserving every current
  editor state; or
- the browser manager has an active or handing-off session.

This preserves the current rule that automatic Git synchronization pauses
while an editor owns working content. Heartbeat expiry prevents an abandoned
browser tab from pausing sync forever. A dirty inactive sidecar alone does not
pause Git because it is not board content; recovery remains available later.

On board switch:

1. invalidate and close all browser sessions before replacing `b.cfg`;
2. stop their watchers and server;
3. clear the model session-target map; and
4. start a fresh lazy manager context for the new board.

On Board close, perform the same shutdown idempotently. Never clear dirty
sidecars merely because the process or browser manager closes.

## Embedded assets and editor dependency

Vendor the pinned plain-JavaScript distribution of TOAST UI Editor 3.2.2:

- project: <https://github.com/nhn/tui.editor>
- release: <https://github.com/nhn/tui.editor/releases/tag/editor%403.2.2>
- license: MIT

Use only the standalone browser bundle and required CSS. Do not add npm,
package.json, package-lock.json, yarn, pnpm, a bundler, transpiler, or generated
Go wrapper.

`browseredit/static/VENDOR.md` records:

- exact upstream release and download URLs;
- retrieval date;
- SHA-256 of every vendored file;
- upstream license path; and
- the manual update procedure.

Embed assets immutably:

```go
//go:embed index.html static
var assets embed.FS
```

Set `usageStatistics: false` when constructing the editor. Use
`initialEditType: "wysiwyg"`, keep the mode switch visible, and configure only
toolbar operations with dependable GFM output.

The browser assets are trusted application code. Do not layer board-local
`.kbrd_web_templates` files over them and do not share the `web.staticFS`
override mechanism.

## Security requirements

The browser editor exposes local card contents and a mutation path, so all of
these are release blockers:

- Bind the literal IPv4 loopback address `127.0.0.1`, never `:port`,
  `0.0.0.0`, hostname, or configurable board-provided address.
- Use a random capability token in every session route.
- Require the current random writer lease on every state-changing session
  request; possession of the capability alone grants read-only access.
- Validate `Host` against the actual listener address.
- Require same-origin `Origin` on state-changing requests.
- Send no CORS headers.
- Set `Referrer-Policy: no-referrer`.
- Set `X-Content-Type-Options: nosniff`.
- Set `Cross-Origin-Resource-Policy: same-origin`.
- Cap JSON bodies at 1 MiB.
- Serve no arbitrary path, directory listing, or board-local asset.
- Do not put absolute filesystem paths into HTML, browser-visible JSON, logs, or
  page titles.
- Escape display labels through `html/template`.
- Use the editor's sanitizer and never inject Markdown-derived HTML manually.
- Disable runtime telemetry and remote plugin loading.
- Do not log tokens or full card content.
- Run the HTTP server with bounded read-header, read, and idle timeouts;
  non-streaming handler deadlines; and per-write SSE deadlines. Keep the
  server-wide `WriteTimeout` zero because a finite request-wide deadline is
  incompatible with intentional long-lived SSE responses.
- Shut down SSE handlers through request/manager contexts.

Recommended CSP:

```text
default-src 'none';
script-src 'self';
style-src 'self' 'unsafe-inline';
connect-src 'self';
img-src 'self' data: blob:;
font-src 'self';
base-uri 'none';
frame-ancestors 'none';
form-action 'self'
```

If the vendored editor requires broader script execution such as
`'unsafe-eval'`, do not weaken CSP silently. Stop and select or patch a bundle
that works under `script-src 'self'`.

The feature remains available under `kbrd --safe`: it serves only embedded
trusted assets, does not execute board code, and retains safe mode's existing
hook/script disabling.

## Implementation sequence

### Phase 1: shared drafts and document semantics

1. Add `editdraft` with path/read/write/clear helpers.
2. Refactor `model/editor_swap.go` to use it without changing behavior.
3. Add `frontmatter.SplitExact` with byte-exact LF/CRLF prefix handling.
4. Add `browseredit/document.go` with parsing, exact-prefix preservation,
   revision, and malformed-frontmatter handling.
5. Lock behavior with table-driven tests before adding HTTP.

Exit criteria:

- all existing TUI recovery tests pass unchanged;
- browser document tests prove LF and CRLF frontmatter byte preservation; and
- writes to the sidecar generate no accepted TUI watcher event.

### Phase 2: embedded page and loopback session server

1. Vendor the pinned editor distribution and licenses.
2. Add embedded HTML, CSS, and vanilla JS.
3. Implement the lazy listener, hardened middleware, session capability lookup,
   single-writer claim/lease, document/draft/heartbeat routes, the SSE-specific
   deadline policy, and graceful shutdown.
4. Implement draft recovery on page open.

Exit criteria:

- the editor loads with network access disabled;
- invalid tokens, origins, hosts, methods, MIME types, and oversized bodies are
  rejected;
- a second tab cannot obtain a writer lease or mutate the shared draft while the
  first writer heartbeat is live; and
- editing produces only the hidden swap file.

### Phase 3: shared real-save pipeline

1. Extract the frontend-neutral existing-card mutation completion helper.
2. Add the bounded manager save-request channel and Bubble Tea wait command.
3. Add exact captured-path validation, final revision validation, and
   frontmatter/body merge on the Board loop.
4. Publish `ItemSaved{Kind: "browser"}` and return structured results to HTTP.
5. Clear the sidecar only after the shared save succeeds.
6. Update hook/event documentation and tests.

Exit criteria:

- browser saves use `board.ReplaceFileContent` through the Board;
- deleted, moved, path-substituted, and stale-at-final-check cards cannot be
  overwritten or resurrected;
- one explicit save triggers exactly one `item_saved` browser event;
- draft writes trigger no hooks; and
- existing TUI save behavior remains unchanged.

### Phase 4: external synchronization and conflict UI

1. Add parent-directory watchers for active cards.
2. Add SSE state, heartbeat, reconnect snapshot, and revision deduplication.
3. Implement clean remote reload, dirty conflict behavior, and eager recovery
   sidecar rebasing onto the newest valid disk frontmatter.
4. Implement Reload disk, Keep my body, and Stay conflicted actions.

Exit criteria:

- external atomic and in-place saves appear in a clean browser session;
- dirty browser content is never replaced automatically; and
- conflict resolution, crash recovery, and TUI handoff preserve the newest
  valid frontmatter.

### Phase 5: TUI action, guards, and lifecycle

1. Add `B` to `KeyMap` and register `actionEditBrowser`.
2. Add session-to-stable-target plus expected-canonical-path ownership in
   `model/browser_editor.go`.
3. Add browser-open notifications and `ItemOpen{Kind: "browser"}`.
4. Add the centralized Board card-editor acquisition guard and Open browser /
   Take over / Cancel dialog for edit, append, prepend, journal, frontmatter,
   Lua, action-menu, search, peek, and mouse entry points.
5. Add the SSE handoff/flush/acknowledgement protocol, `PrepareRecovery` rebase,
   and timeout confirmation.
6. Extend Git `EditorActive` and Board switch/close cleanup.

Exit criteria:

- no supported in-process action can create simultaneous TUI/browser buffers
  for the same card;
- dirty handoff recovers the browser draft in the TUI;
- conflict-to-TUI handoff retains the newest valid disk frontmatter;
- timeout takeover requires an explicit second confirmation;
- session URLs become unusable after takeover, board switch, or process close;
  and
- abandoned sessions stop pausing Git after heartbeat expiry.

### Phase 6: documentation and browser verification

1. Document the new key/action, save versus draft semantics, conflict handling,
   frontmatter behavior, and recovery handoff in README/help surfaces.
2. Document vendored dependency maintenance and security expectations.
3. Manually verify current Chrome, Firefox, and Safari behavior.
4. Capture screenshots only if the README/help layout changes warrant them.

## Test plan

### `editdraft`

- Existing path format remains `.<filename>.kbrd-swap`.
- Atomic write creates a complete sidecar.
- Clear is idempotent.
- TUI and browser helpers read the same draft.

### Document semantics

- Plain Markdown round-trip.
- Empty file and empty body.
- Well-formed `---` and `...` closing fences with LF and CRLF line endings.
- Frontmatter comments, spacing, nested YAML, unknown keys, and blank lines are
  byte-identical after body replacement.
- Malformed/unterminated apparent frontmatter defaults to read-only source mode.
- Current frontmatter wins when Keep my body resolves a conflict.
- Revision changes for any raw-byte change.

### HTTP and security

- Listener address is loopback and port is dynamic.
- Capability and writer-lease tokens are unique and sufficiently random.
- A second tab is read-only while the first writer lease is live; wrong,
  expired, and superseded leases cannot draft, save, clear, acknowledge, or
  release another writer's state.
- Application heartbeat alone renews the writer lease; an open SSE stream and
  its comment heartbeats do not prevent expiry.
- Unknown and invalidated sessions are rejected.
- Host and Origin validation.
- No CORS and no board-local asset override.
- CSP and security headers are exact.
- Request body cap and strict JSON decoding.
- Handler cancellation does not leak or block save requests.
- A healthy SSE stream remains connected beyond ordinary handler timeouts, and
  a blocked SSE write exits through its per-write deadline.
- Server close terminates SSE and watcher goroutines.

### Draft, save, events, and hooks

- Draft creation/update does not modify the card.
- Draft creation and dirty external changes rebase the recovery sidecar onto
  the newest valid disk frontmatter without replacing the browser body.
- Browser recovery extracts the sidecar body and rebases it onto the newest
  valid disk frontmatter before enabling edits; observers cannot recover or
  discard it.
- Draft writes are ignored by TUI watcher classification.
- Successful save clears the draft.
- Failed or stale save retains the draft.
- Deleted card returns Gone and is not recreated.
- A captured path miss cannot fall back to a same-named replacement card.
- Browser save resolves the captured stable target after selection moves.
- Successful browser save emits one `ItemSaved` with kind `browser`.
- Browser save does not emit `ItemChanged`.
- Existing terminal edit/append/prepend/journal events are unchanged.
- Hook rewrite behavior remains convergent.

### External synchronization

- In-place write, atomic rename, removal, and recreation are detected.
- Clean session adopts an external change.
- Dirty session enters conflict without losing its sidecar.
- A dirty external frontmatter change durably rebases the sidecar before a
  simulated process crash and normal TUI recovery.
- SSE reconnect receives the latest state.
- A server-originated save is deduplicated by revision.
- A timer `kbrd.fs.write` processed before browser save causes a `412`, retains
  the browser draft, and does not emit `ItemSaved`.
- A timer frontmatter update during a dirty browser edit enters conflict;
  `Keep my body` preserves the timer's newest frontmatter.
- A timer write processed after browser save remains effective and is sent to
  the clean browser, or becomes a conflict if the browser is dirty again.
- A timer-driven move, rename, or delete marks the original-path session Gone,
  preserves the sidecar, and never resurrects the old card.
- Timer mutations retain their existing watcher, board event, and hook counts;
  tests assert deterministic update-loop ordering without timing sleeps.

### Mutual exclusion and handoff

- `e`, append, prepend, journal, frontmatter edit, and `kbrd.editor.open` on an
  active browser-owned card all pass through the guard dialog.
- Open browser reuses the correct URL and any new tab is read-only.
- Cancel leaves both states unchanged.
- Successful handoff flushes, invalidates browser, and opens TUI recovery.
- A dirty conflict followed by handoff and TUI save preserves the newest valid
  disk frontmatter.
- Handoff timeout does not take ownership without confirmation.
- Confirmed timeout uses the last durable sidecar.
- Browser save is rejected once handoff starts.
- Browser action is rejected for the active TUI card.
- Browser action is rejected for every Board-owned same-card TUI buffer, not
  only full edit mode.
- An inactive session is invalidated before TUI recovery opens.
- Different cards may have separate browser sessions.
- Same-card observer tabs cannot race drafts or clear another tab's recovery.

Use channels, contexts, and `testing/synctest` where appropriate. Do not use
timing sleeps to test debounce, heartbeat, handoff, or shutdown behavior.

## Verification commands

Run after every implementation phase:

```bash
GOCACHE=/private/tmp/kbrd-go-build-cache go test ./editdraft ./browseredit ./model
GOCACHE=/private/tmp/kbrd-go-build-cache go test ./...
GOCACHE=/private/tmp/kbrd-go-build-cache go vet ./...
gofmt -w .
git diff --check
```

The final manual browser matrix covers:

- WYSIWYG and Markdown mode changes;
- draft status and crash recovery;
- explicit save and hook count;
- external clean reload;
- dirty conflict resolution;
- same-card second-tab read-only behavior and writer heartbeat expiry;
- SSE operation beyond ordinary request deadlines;
- browser-to-TUI handoff, including timeout;
- board switch and application shutdown; and
- operation with network access disabled and `kbrd --safe` enabled.

## Release acceptance criteria

The feature is complete only when all of the following are true:

- `B` opens the selected filesystem card in a loopback browser editor.
- The app starts no browser listener until the action is used.
- No browser asset requires the network or a JavaScript build tool.
- Merely opening the browser never changes a card or draft.
- Frequent typing persists a local recovery sidecar and triggers no hooks.
- Explicit save uses the existing Board mutation and hook machinery.
- Frontmatter is preserved from the current disk version.
- Stale, deleted, moved, path-substituted, and externally changed cards visible
  at the final Board revision check are never silently overwritten. The
  documented narrow race with an arbitrary writer between that check and atomic
  rename is not represented as a portable filesystem guarantee.
- Lua timer and script-driven mutations retain existing semantics and are never
  silently overwritten by a stale browser save.
- Browser/TUI same-card ownership is guarded in both directions.
- One browser writer lease prevents same-session tabs from racing drafts or
  saves; additional tabs are read-only observers.
- Dirty takeover and crash recovery preserve the newest valid frontmatter and
  do not silently lose the manager's last durably accepted browser body.
- Writer ownership expires from application heartbeat only, and long-lived SSE
  uses per-write rather than request-wide deadlines.
- Browser sessions and goroutines stop on takeover, board switch, and shutdown.
- Existing terminal editor, `$EDITOR`, web server, hooks, watcher, Git sync, and
  safe-mode tests remain green.
