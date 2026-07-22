# Scripted Canvas Surface Plan

## Goal

Allow Lua scripts to publish arbitrary terminal content alongside normal
filesystem columns and item-based virtual columns. The initial canvas is a
persistent, scrollable board surface rather than a modal: users enter it with
normal column navigation, leave it by moving to another column, and can enlarge
or restore it with the existing zoom interaction.

This should support dashboards, calendars, tables, progress views, dependency
diagrams, logs, command output, and script-generated reports without exposing
Bubble Tea or Lip Gloss internals to Lua.

## Product decisions

1. A canvas is an inline board surface, not a modal.
2. `tab` / `shift+tab` move between filesystem columns, virtual lists, and
   canvases.
3. `z` zooms the selected canvas; `z` or `esc` restores the normal board.
4. Scripts publish snapshots. Lua is never called synchronously from `View()`.
5. Go owns clipping, scrolling, focus, terminal-width measurement, colors, and
   input routing.
6. Canvas text is plain text. Styling is declarative, and raw ANSI/control
   sequences are stripped or rejected.
7. The existing `kbrd.column.set` API remains compatible.
8. A separate modal/full-screen `surface.open` API is optional follow-up work,
   not part of the first implementation.

## User interaction

When a canvas is selected:

| Input | Behavior |
| --- | --- |
| `tab` / `shift+tab` | Focus the next or previous board surface |
| `j` / `k`, arrows | Scroll vertically |
| Page Up / Page Down | Scroll by a viewport |
| `h` / `l` or dedicated pan keys | Pan horizontally when content is wider than the viewport |
| Mouse wheel | Scroll the canvas under the pointer |
| `z` | Enter or leave the existing single-surface zoom view |
| `esc` | Leave zoom; otherwise do nothing at board level |
| `|` | Collapse the surface using the existing column behavior |
| `x` | Open commands available to the surface |
| Script command key | Run the matching surface command |

Canvas command keys take precedence over canvas scrolling keys, matching the
current virtual-column command behavior. Item-specific actions, filtering,
marking, mnemonics, card search entries, and file mutations are unavailable on
a canvas.

Removing a persistent canvas is a script lifecycle operation:

```lua
kbrd.surface.clear("sprint-health")
```

The user normally does not need to remove it to return to the board; the board
remains visible around it, and normal navigation leaves the canvas.

## Proposed Lua API

Introduce `kbrd.surface` as the general presentation API. Keep
`kbrd.column.set`, `clear`, and `clear_all` as compatibility entry points for
list surfaces.

```lua
kbrd.surface.set("sprint-health", {
  kind = "canvas",
  name = "Sprint health",
  width = 64,
  header = { fg = "#1a1b26", bg = "#e0af68" },

  content = {
    {
      { text = "SPRINT 42", bold = true, fg = "#e0af68" },
      { text = "  73% complete", fg = "#9ece6a" },
    },
    {},
    {
      { text = "Done     " },
      { text = "██████████████░░░░░", fg = "#9ece6a" },
    },
    {
      { text = "Blocked  " },
      { text = "● 3", fg = "#f7768e", bold = true },
    },
  },

  commands = {
    {
      id = "refresh",
      name = "Refresh",
      key = "r",
      requiresItem = false,
      run = function(ctx)
        publish_sprint_health()
      end,
    },
  },
})
```

Initial API:

```lua
kbrd.surface.set(id, spec)
kbrd.surface.clear(id)
kbrd.surface.clear_all()
```

`kind = "canvas"` requires `content`. A later compatibility cleanup may model
the current virtual list as `kind = "list"`, but existing scripts should not
need to change.

Canvas commands receive board and surface context without item fields:

```lua
{
  boardPath = "...",
  boardName = "...",
  surfaceID = "sprint-health",
  surfaceName = "Sprint health",
  kind = "canvas",
}
```

For canvas commands, `requiresItem` should default to `false`. Existing virtual
list commands retain their current default of `true`.

## Content protocol

Use lines containing styled spans rather than raw ANSI strings or an initial
absolute-coordinate drawing API.

```go
type CanvasSpec struct {
	Name     string
	Width    int
	HeaderFG string
	HeaderBG string
	Lines    []CanvasLine
	Commands []VirtualCommand
}

type CanvasLine struct {
	Spans []CanvasSpan
}

type CanvasSpan struct {
	Text      string
	FG        string
	BG        string
	Bold      bool
	Italic    bool
	Underline bool
}
```

The renderer must:

- measure and clip by terminal cells rather than bytes or runes;
- handle wide Unicode characters without splitting a cell;
- normalize newlines so one span cannot create untracked rows;
- strip terminal control sequences from text;
- validate colors before rendering;
- cap total input size and line count to prevent accidental render or memory
  blowups;
- pad only the visible viewport, not the entire intrinsic canvas.

Lines of spans are sufficient for the first version. Absolute positioning,
overlapping primitives, embedded Bubble Tea models, arbitrary Lip Gloss styles,
and direct terminal escape sequences are out of scope.

## Internal direction

Avoid starting with a broad `Surface` interface. The existing model contains
many valid filesystem and item-list assumptions, so a universal interface would
initially be large and artificial.

Extend `Column` with a concrete canvas content model while retaining `Virtual`
as the permission boundary for script-owned, fileless surfaces:

```go
type Column struct {
	// Existing fields.
	Virtual bool
	VID     string

	canvas *Canvas
}

type Canvas struct {
	Lines   []CanvasLine
	OffsetX int
	OffsetY int
	width   int
	height  int
}
```

Branch at focused methods instead of spreading kind checks across the board:

- `Column.View`
- content update routing (`UpdateContent`, replacing list-only call sites)
- `ScrollBy`
- `HitTest`
- filtering availability
- selection and item-count queries
- header rendering

The current column strip, variable-width slots, collapse handling, zoom layout,
focus selection, visibility projection, and border composition should remain
the shared outer shell.

Internally, evolve the layer-owned virtual-column registry into an ordered
script-surface registry. Lists and canvases must share:

- stable IDs and insertion order;
- base/layer shadowing rules;
- command callback ownership and cleanup;
- replacement and clear semantics;
- survival across filesystem refreshes.

Do this incrementally with adapters for the current virtual-column API rather
than renaming every model concept in one change.

## Implementation steps

### Step 1: Characterize existing virtual-surface behavior

Add focused tests before changing production types:

- virtual columns survive filesystem reloads;
- replacement preserves list selection by stable item ID;
- layer activation shadows and restores a base virtual column;
- clear and clear-all release command callback references;
- variable width, zoom, collapse, and visibility ordering remain stable;
- empty virtual-column commands still honor `requiresItem`.

Record the existing Lua API behavior as the compatibility baseline. Do not
change rendering or public APIs in this step.

### Step 2: Add the canvas protocol and decoder

Add typed canvas structures under `events/` and Lua-table decoding under
`script/`.

- Validate `kind`, IDs, span fields, colors, and bounded content size.
- Preserve command functions through opaque refs, following virtual commands.
- Add decoder tests for valid content, malformed spans, unsupported kinds,
  control characters, oversized content, and command defaults.
- Do not publish canvases to the model yet; this step establishes the protocol.

Acceptance criteria:

- Lua data converts into an immutable Go snapshot.
- No live Lua tables or functions escape the script host.
- Existing `kbrd.column.set` tests remain unchanged and green.

### Step 3: Build a standalone canvas viewport

Create a focused model component, preferably `model/canvas.go`, with no board
or Lua knowledge.

- Render styled lines at a supplied width and height.
- Track horizontal and vertical offsets.
- Clamp offsets after content replacement or resize.
- Implement line/page scrolling and horizontal panning.
- Render optional scroll indicators consistently with the current list gutter.
- Add ANSI-aware golden or explicit rendering tests, including Unicode width,
  clipping, empty content, resize, and style boundaries.

Acceptance criteria:

- Rendering is deterministic and never exceeds its allotted width or height.
- Content replacement preserves offsets when valid and clamps them otherwise.
- Untrusted text cannot emit terminal control sequences.

### Step 4: Integrate canvas content into the board strip

Attach the canvas component to virtual `Column` instances and branch only at
the concentrated content methods.

- Reuse the existing header, virtual marker, double border, width, collapse,
  visibility, and zoom shell.
- Suppress item counts or allow an explicit future header label; do not show a
  misleading `0` card count.
- Route resize state to the canvas.
- Keep filesystem mutations blocked through the existing `Virtual` boundary.
- Exclude canvases from item search, filtering, marks, mnemonics, harpoon, and
  item-selection events.

Acceptance criteria:

- A programmatically constructed canvas appears beside normal columns.
- `tab`, zoom, collapse, horizontal packing, and board resize work.
- Existing filesystem and virtual-list rendering tests remain green.

### Step 5: Add input, mouse, and close/return behavior

Introduce canvas-specific input handling before the list-only fallback.

- Surface command keys run first.
- Remaining navigation keys scroll or pan the viewport.
- Board navigation keys continue to leave the canvas normally.
- Mouse wheel scrolls the canvas beneath the pointer.
- `esc` exits zoom when a canvas is selected; it does not remove the canvas.
- `z` retains its existing toggle behavior.
- `surface.clear` removes a canvas and safely clamps board focus.

Acceptance criteria:

- A user can enter, navigate, zoom, restore, leave, and revisit a canvas
  without opening a modal.
- Clearing the selected canvas selects a valid remaining surface.
- Canvas input never leaks into an inactive column list or filter.

### Step 6: Publish the Lua API and unify lifecycle state

Expose `kbrd.surface.set`, `clear`, and `clear_all`, backed by an ordered
script-surface registry.

- Generalize layer staging and reconciliation to lists and canvases.
- Define ID collision semantics: an active-layer surface shadows a base surface
  of the same ID regardless of kind, and the base surface returns on unload.
- Clean command refs whenever a surface is replaced, cleared, or shadowed.
- Keep `kbrd.column.set` behavior and ordering compatible through an adapter.
- Make canvas updates safe from startup, hooks, commands, timers, and async
  callbacks wherever virtual-column updates are currently allowed.

Acceptance criteria:

- Re-publishing the same canvas replaces its snapshot without duplication.
- Base/layer transitions cannot leak stale canvases or callbacks.
- Existing virtual-column scripts run without modification.

### Step 7: Documentation, examples, and visual verification

- Document the API and interaction model in `SCRIPTING.md`.
- Add a small example such as a sprint-health dashboard or weekly calendar.
- Add the example to the demo board.
- Capture normal, selected, zoomed, and horizontally scrolled screenshots.
- Explicitly document that `esc`/`z` leave zoom, navigation leaves the surface,
  and `surface.clear` removes it.
- Run formatting, tests, vet, and the VHS screenshot workflow.

Suggested verification:

```bash
just fmt
just test
just vet
just screenshots
git diff --check
```

## Follow-up steps, only after the base canvas is proven

### Interactive regions

Add optional stable regions that scripts can associate with commands and
opaque data:

```lua
regions = {
  {
    id = "blocked",
    x = 10,
    y = 3,
    width = 3,
    height = 1,
    label = "Blocked tasks",
    command = "open-blocked",
    data = { status = "blocked" },
  },
}
```

Only add this after real examples establish keyboard focus order, overlap,
clipping, accessibility labels, and mouse semantics. `Enter` would activate the
focused region and pass `regionID` plus `data` to the command.

### Responsive redraw events

If intrinsic canvases plus clipping are insufficient, add a coalesced
`surface_resize` event. The callback publishes a new snapshot after receiving
the allotted width and height. Never invoke Lua directly from `View()`.

### Temporary full-screen canvases

If scripts need reports that temporarily replace the board, add a separate
modal lifecycle:

```lua
kbrd.surface.open("weekly-report", spec)
kbrd.surface.close("weekly-report")
```

In that mode, `esc` or `q` closes the overlay and returns to the unchanged
board. It should reuse the same canvas viewport but integrate with the existing
modal-layer priority and script cancellation rules. Do not overload persistent
`surface.set` with modal behavior.

### Web rendering

The web frontend currently builds its view directly from filesystem columns.
Treat scripted surface rendering on the web as a separate feature with an
explicit safe representation; it should not block the TUI implementation.

## Non-goals for the initial implementation

- Running arbitrary Bubble Tea components supplied by Lua.
- Calling Lua during `View()`.
- Raw ANSI, OSC, hyperlinks, or terminal escape passthrough.
- Absolute-positioned or overlapping drawing primitives.
- Animation driven by render callbacks; scripts may republish snapshots from
  existing timers.
- Editable text fields inside an inline canvas.
- Modal `surface.open` / `close`.
- Automatic web parity.

## Completion criteria

The feature is complete when a Lua script can publish a styled multiline
canvas, update it repeatedly, attach surface-level commands, and clear it; the
user can navigate into it, scroll and pan it, zoom and restore it, leave it for
another column, and return without disrupting ordinary board behavior. Existing
virtual-column scripts and all filesystem-column operations must remain
compatible.
