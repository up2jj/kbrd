# Scriptable TUI Widgets Plan

## Goal

Expose native-looking terminal UI building blocks to Lua scripts without
exposing Bubble Tea internals. Lua defines data and consumes results; Go owns
rendering, layout, focus, keyboard handling, validation, resizing, and modal
lifecycle.

The first motivating workflow is a machine-local scratchpad that can promote
selected text into cards, but the API should remain general enough for custom
capture flows, review queues, card generators, imports, and other scripted
workflows.

## Current foundation

kbrd already has the essential execution model:

- Lua commands run in gopher-lua coroutines.
- `kbrd.ui.pick`, `kbrd.ui.prompt`, and `kbrd.ui.confirm` yield a UI request.
- Bubble Tea remains responsive while the coroutine is suspended.
- The coroutine resumes after the user submits or cancels the modal.
- Multiple UI calls can be chained in procedural Lua code.
- Script execution timeouts are applied per resume, so time spent waiting for
  user input does not count against the script.

The implementation should extend this bridge rather than replace it.

Baseline at the time this plan was written:

```text
go test ./script ./model
ok kbrd/script
ok kbrd/model
```

## Design principles

1. Lua declares content and behavior; Go controls presentation.
2. Scripts use coroutine-based synchronous calls rather than callbacks.
3. Lua cannot set absolute coordinates, raw terminal styles, focus internals,
   mouse routing, Bubble Tea messages, or animation timing.
4. UI requests and results use a typed, validated protocol.
5. Only one scripted modal may be active globally.
6. Existing scalar `pick`, `prompt`, and `confirm` calls remain compatible.
7. Reusable TUI controls stay independent of Lua and board semantics.
8. Board-specific workflows remain in `model/` even when they render UI.

## Proposed project hierarchy

Adding every widget directly to `model/` would make an already large package
harder to maintain. Introduce one focused top-level `tui/` package and keep the
Lua protocol in `script/`.

```text
kbrd/
├── script/
│   ├── ui_types.go       # UIRequest, UIResult, fields, items, and actions
│   ├── ui_decode.go      # Lua tables to validated Go structures
│   ├── ui_bootstrap.go   # kbrd.ui wrappers and compatibility API
│   ├── ui_test.go
│   ├── host.go           # coroutine ownership and resume
│   └── api.go            # non-UI Lua bindings
│
├── tui/
│   ├── input.go
│   ├── select.go
│   ├── multiselect.go
│   ├── confirm.go
│   ├── actions.go
│   ├── form.go
│   ├── textarea.go
│   ├── viewer.go
│   ├── keymap.go
│   ├── sizing.go
│   └── *_test.go
│
├── model/
│   ├── script_ui.go          # active request coordinator
│   ├── script_ui_open.go     # protocol request to TUI widget
│   ├── script_ui_update.go   # message routing and result production
│   ├── script_ui_view.go     # overlay integration
│   ├── scripting.go          # Board to Lua orchestration
│   ├── modal_layers.go
│   └── ...
│
├── theme/                    # palette and common overlay frame
└── vimbuf/                   # text buffer and selection semantics
```

Dependency direction:

```text
theme ───────┐
vimbuf ──────┼──> tui ───────┐
             │                │
events ──> script ────────────┼──> model
config ─> script ─────────────┘
```

### `script/`: protocol and runtime

`script` owns what Lua may request but knows nothing about rendering.

Move the current UI bootstrap out of `script/api.go` and move UI request parsing
out of `script/host.go`:

- `ui_types.go` defines the typed protocol.
- `ui_decode.go` validates yielded Lua tables and converts results.
- `ui_bootstrap.go` defines public Lua wrappers and legacy compatibility.
- `host.go` retains coroutine start, suspension, resume, timeout, and
  cancellation.

The package must continue to avoid imports of `model` and `tui`.

### `tui/`: reusable terminal controls

`tui` owns self-contained components. They receive ordinary Go options and
produce ordinary Go results. They do not know about Lua, script tokens, boards,
columns, or cards.

A representative component API is:

```go
type SelectOptions struct {
	Title      string
	Items      []SelectItem
	Searchable bool
	InitialID  string
}

type SelectResult struct {
	ID        string
	Submitted bool
	Cancelled bool
}

func (s *Select) Open(opts SelectOptions)
func (s *Select) Active() bool
func (s *Select) SetSize(width, height int)
func (s *Select) SetPalette(theme.Palette)
func (s *Select) Update(tea.Msg) tea.Cmd
func (s *Select) View() string
func (s *Select) TakeResult() (SelectResult, bool)
func (s *Select) Close()
```

Start with concrete types and explicit switching in the coordinator. Do not
introduce a public universal widget interface until multiple consumers require
one.

### `model/`: application integration

`model` remains responsible for:

- Modal priority and input ownership.
- Overlay placement over the board.
- Opening a TUI widget from a `script.UIRequest`.
- Turning widget completion into a coroutine resume message.
- Closing scripted UI during board and process lifecycle changes.

The existing `ScriptUI` becomes a small coordinator holding concrete TUI
components:

```go
type ScriptUI struct {
	request script.UIRequest
	kind    script.UIKind

	input      tui.Input
	selectOne  tui.Select
	selectMany tui.MultiSelect
	confirm    tui.Confirm
	actions    tui.Actions
	form       tui.Form
	textarea   tui.Textarea
	viewer     tui.Viewer
}
```

Split coordinator code by responsibility once it grows beyond a few hundred
lines; do not create a separate file for every trivial method.

### Existing UI code

Extract reusable behavior incrementally:

- Input behavior from `model/script_ui.go`.
- Confirmation behavior from `model/dialog.go`.
- Picker mechanics from `model/flat_picker.go` and
  `model/grouped_picker.go`.
- Continue using `theme.OverlayFrame`; do not create another frame system.

Thin wrappers or temporary type aliases in `model` may keep native screens
compiling while shared mechanics move.

Board-specific workflows stay in `model`, including `TemplateFlow`,
`FrontmatterEditor`, `Search`, `Timeline`, `MoveMenu`, `HelpMenu`, editor
orchestration, modal layers, and overlay composition. They may consume `tui`
controls later, but should not move solely because they display terminal UI.

## Public Lua API

New widgets use table-based options and a common result envelope:

```lua
local result = kbrd.ui.input({
  title = "Rename card",
  label = "New title",
  initial = ctx.fileName,
  required = true,
})

if result.cancelled then return end
kbrd.notify("New title: " .. result.value)
```

Common result fields:

```lua
{
  submitted = true,
  cancelled = false,
  action = "submit",
  value = "...",       -- single-value widgets
  values = {...},      -- forms
  ids = {...},         -- multiselect
  reason = nil,
}
```

Target functions:

```lua
kbrd.ui.input({...})
kbrd.ui.textarea({...})
kbrd.ui.select({...})
kbrd.ui.multiselect({...})
kbrd.ui.confirm({...})
kbrd.ui.form({...})
kbrd.ui.actions({...})
kbrd.ui.viewer({...})
kbrd.ui.notify({...})
```

Keep the existing positional API:

```lua
kbrd.ui.pick(title, choices)   -- string or nil
kbrd.ui.prompt(title, default) -- string or nil
kbrd.ui.confirm(title)         -- boolean
```

Legacy wrappers should normalize their arguments to the new request schema and
unwrap the structured result before returning to existing scripts.

## Protocol

The Go protocol should use typed structures rather than passing arbitrary maps
through the application:

```go
type UIRequest struct {
	Token string
	Kind  UIKind
	Spec  UISpec
}

type UIResult struct {
	Action    string
	Submitted bool
	Cancelled bool
	Value     any
	Values    map[string]any
	IDs       []string
	Selection *TextSelection
	Reason    string
}
```

Add concrete types for items, fields, actions, validation, cursor positions,
and text selections. Decode and validate requests immediately after a coroutine
yields. A malformed request must produce an actionable Lua error rather than be
treated as successful command completion.

## Lifecycle and safety

Complete lifecycle hardening before expanding the widget set:

- Enforce one active scripted request globally.
- Reject UI calls from hooks, timers, and async callbacks, which do not run in
  a resumable command coroutine.
- Close the scripted modal and discard its pending coroutine during board
  switches, script reloads, safe-mode transitions, and shutdown.
- Guard against stale tokens and delayed messages resuming a newer command.
- Preserve the existing timeout-per-resume behavior.
- Return a structured cancellation result when the user presses Escape.
- Resume unknown widget kinds with an error instead of leaving a coroutine
  suspended.

System cancellation should terminate the coroutine without resuming it. Running
an old-board script during a board transition could mutate the wrong board.

## Implementation phases

### Phase 1: Protocol, hierarchy, and compatibility

1. Add `script/ui_types.go`, `ui_decode.go`, and `ui_bootstrap.go`.
2. Preserve existing `pick`, `prompt`, and `confirm` behavior.
3. Add typed structured results and validation errors.
4. Add lifecycle cancellation and stale-token handling.
5. Update modal routing so scripted components can consume arbitrary
   `tea.Msg`, not only keypresses.
6. Create the `tui/` package with common sizing and key conventions.

This phase should not introduce visible behavioral changes for existing
scripts.

### Phase 2: Input, select, confirm, actions, and notify

Implement the lower-risk controls first.

#### Input

Support:

- Title and label.
- Initial value and placeholder.
- Required values.
- Minimum and maximum rune length.
- Declarative RE2 pattern validation and a user-facing pattern hint.

#### Select

Items support:

```lua
{
  id = "doing",
  label = "Doing",
  description = "2 of 3 cards",
  icon = "●",
  disabled = false,
  disabled_reason = nil,
  group = "Current board",
}
```

The widget supports stable IDs, optional fuzzy search, initial selection,
disabled items, and bounded scrolling.

#### Confirm

Support message text, detail lines, custom labels, destructive styling, and a
configurable safe default.

#### Actions

Use select-like items with shortcut labels, disabled states, and disabled
reasons. Action shortcuts are validated against reserved navigation and cancel
keys.

#### Notify

Provide a table-based wrapper around the existing notifier. Notifications are
non-blocking and do not suspend the coroutine.

After this phase, implement legacy `pick`, `prompt`, and `confirm` through the
same controls.

### Phase 3: Multi-select and forms

Use the established `huh/v2` integration from `model/template_flow.go`.

Initial field types:

- `input`
- `textarea`
- `select`
- `multiselect`
- `checkbox`
- `number`
- `label`
- `separator`

Do not support arbitrary nested layouts, per-keystroke Lua callbacks, or
dynamically inserted fields in the first release. Declarative validation should
use the same vocabulary as template fields where possible.

Example:

```lua
local result = kbrd.ui.form({
  title = "Promote to card",
  fields = {
    {
      id = "title",
      type = "input",
      label = "Title",
      required = true,
    },
    {
      id = "column",
      type = "select",
      label = "Column",
      items = {
        { id = "todo", label = "Todo" },
        { id = "doing", label = "Doing" },
      },
    },
    {
      id = "remove",
      type = "checkbox",
      label = "Remove from scratchpad",
      initial = true,
    },
  },
})
```

### Phase 4: Textarea, selection, and viewer

This phase enables the scratchpad promotion workflow.

#### Textarea

The initial release supports:

- Editable multiline content.
- Wrapping and optional line numbers.
- Cursor line, column, and UTF-8 byte offset.
- Script-declared actions such as Save and Promote.
- Reserved Escape cancellation.
- Consistent resize and scrolling behavior.

Expose a public, read-only selection method from `vimbuf` that returns
normalized positions and selected text. A textarea action result may then be:

```lua
{
  action = "promote",
  value = "...",
  cursor = {
    line = 12,
    column = 4,
    offset = 142,
  },
  selection = {
    start_offset = 142,
    end_offset = 238,
    text = "...",
  },
}
```

Offsets are UTF-8 byte offsets because Lua strings and Go file operations are
byte-oriented. Line and column numbers are one-based for script ergonomics.

#### Viewer

Reuse the scrollable document behavior already used by Peek and Timeline.
Initially support:

- `plain`
- `markdown`
- `diff`
- `json`
- `yaml`
- `log`

Viewer actions such as Apply and Back use the shared `UIAction` schema.

### Phase 5: Progress and task execution

Do not expose a mutable `progress:update()` object as part of the initial widget
release. Bubble Tea and the Lua VM are intentionally single-threaded, and
existing async callbacks cannot open UI safely.

Design progress separately around a task API:

```lua
kbrd.task.run({
  title = "Importing cards",
  run = function(ctx)
    -- isolated task execution model
  end,
})
```

This requires explicit decisions about cancellation, Lua VM ownership, board
mutation serialization, error delivery, and shutdown. The other widgets should
not wait for it.

## Scratchpad composition

Once textarea, select, and local storage are available, a scratchpad remains a
script-level workflow rather than a native board feature:

```lua
local result = kbrd.ui.textarea({
  title = "Scratchpad",
  initial = text,
  actions = {
    {
      id = "save",
      label = "Save",
      key = "ctrl+s",
      primary = true,
    },
    {
      id = "promote",
      label = "Promote",
      key = "ctrl+enter",
      requires_selection = true,
    },
  },
})
```

A complete scratchpad also needs a general machine-local storage API. The
current column store is column-configuration-specific, so `kbrd.local` should
be a separate follow-up rather than hidden inside the UI package.

## Testing strategy

### `script` tests

- Valid and malformed Lua table decoding.
- Duplicate item and field IDs.
- Unsupported kinds and field types.
- Structured result conversion.
- Legacy scalar API compatibility.
- Multiple chained widgets.
- User cancellation, system cancellation, and stale tokens.
- Script error immediately after resume.

### `tui` tests

- Navigation and bounded scrolling.
- Search filtering.
- Empty and disabled item behavior.
- Validation errors.
- Resize behavior.
- Escape and action shortcuts.
- Form completion.
- Cursor and selection offsets, including multibyte UTF-8 text.

### `model` integration tests

- Opening each widget from a Lua command.
- Routing keypresses and internal Bubble Tea messages to the active widget.
- Turning widget results into coroutine resumes.
- Chained widget workflows.
- Board switch and script reload while a widget is open.
- Modal priority relative to native screens.
- Line-command return values after multiple UI yields.

## Verification

Run after every phase:

```bash
GOCACHE=/private/tmp/kbrd-go-build-cache go test ./script ./tui ./model
GOCACHE=/private/tmp/kbrd-go-build-cache go test ./...
GOCACHE=/private/tmp/kbrd-go-build-cache go vet ./...
```

Visible widget changes should also receive a focused VHS/demo capture where
that provides useful regression coverage.

## Documentation and release gate

Expand `SCRIPTING.md` with:

- Every option and result schema.
- Cancellation and lifecycle semantics.
- Restrictions on hooks, timers, and async callbacks.
- One runnable example per widget.
- A chained multi-step workflow.
- A complete scratchpad promotion example once local storage exists.

A widget is ready for scripts only when:

- Its schema is documented.
- Invalid input produces an actionable Lua error.
- Cancellation is deterministic.
- Terminal resizing is handled.
- Legacy behavior remains green.
- It has host-level, component-level, and model integration tests.

## Recommended delivery order

1. Protocol extraction and lifecycle hardening.
2. `tui/` package plus input, select, and confirm migration.
3. Actions, notifications, and multi-select.
4. Forms.
5. Textarea actions and selection.
6. Viewer.
7. Machine-local storage and a reference scratchpad script.
8. Progress/task execution as a separate design and implementation effort.

This sequence produces useful, releasable script capabilities at every stage
without committing the project to a large framework or an all-at-once UI
rewrite.
