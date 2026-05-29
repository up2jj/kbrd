# Scripting kbrd with Lua

kbrd embeds a Lua VM (gopher-lua, Lua 5.1) so you can extend it beyond YAML
shell commands. Scripts can register new entries in the Custom Commands menu,
react to events, move items between columns, read/write files, and create new
columns on the fly.

Lua is **additive** — your existing `commands.yml` / `.kbrd_commands.yml`
shell commands keep working unchanged.

---

## Quick start

Create one of these files and kbrd will load it at boot:

| File                                | Scope            |
| ----------------------------------- | ---------------- |
| `~/.config/kbrd/init.lua`           | Global (all boards) |
| `<board>/.kbrd.lua`                 | Folder-local        |

> **⚠️ Security & trust:** A folder-local `.kbrd.lua` is **executed automatically when
> you open that board** — no prompt. It runs with the full Lua stdlib, can shell out via
> `kbrd.async.run`, and `kbrd.fs.*` is **not** sandboxed to the board root. Opening a
> board you cloned or synced runs its author's code as you. Review `.kbrd.lua` in boards
> you didn't write, or set `[scripting] enabled = false`. See
> **[SECURITY.md](./SECURITY.md)** for the full trust model.

Minimal example:

```lua
kbrd.command("A", "Archive", function(ctx)
  if not kbrd.fs.exists("archive") then
    kbrd.board.createColumn("archive")
  end
  kbrd.board.move(ctx, "archive")
  kbrd.notify("archived " .. ctx.fileName, "success")
end)
```

Open kbrd, press `x` on any item, and `[A] Archive (lua)` will appear in the
menu alongside your shell commands.

---

## Configuration

Scripting is on by default. Tunables live in `~/.config/kbrd/config.toml`
(or `<board>/kbrd.toml`):

```toml
[scripting]
enabled            = true     # master switch — false disables the whole subsystem
command_timeout_ms = 2000     # wall-clock budget for kbrd.command callbacks
hook_timeout_ms    = 500      # stricter budget for event hooks (they fire on hot paths)
instruction_limit  = 10000000 # backstop against pure-CPU infinite loops
error_threshold    = 3        # auto-disable a timer/hook after N consecutive errors (0 = never)
```

When `enabled = false`, no Lua VM is created and `init.lua` is not read.
Equivalent to compiling kbrd without scripting.

### Environment variables

Scripts inherit kbrd's environment, and there are three ways to read it:

- **Lua:** the standard library is available, so `os.getenv("HOME")` works directly
  (including inside `kbrd.async.run` callbacks).
- **Shell commands run by a script** (`kbrd.async.run`) and **YAML shell commands** execute via
  `sh -c`, so plain `$VAR` expands normally.
- **YAML command templates** can substitute a value before the shell runs with `{{env "VAR"}}`
  (empty string if unset).

---

## Two ways to plug in

A script can do either or both:

### 1. Menu commands — `kbrd.command(...)`

Adds an entry to the `x` Custom Commands menu. Runs on user action.

```lua
kbrd.command("a", "Archive", function(ctx) ... end)
```

The entry appears next to your YAML shell commands, marked `(lua)` vs `(sh)`
so you can tell them apart.

### 2. Event hooks — `kbrd.on(...)`

Subscribes to an event. Runs reactively when the event is published.

```lua
kbrd.on("git_sync_done", function(evt)
  if not evt.ok then kbrd.notify("sync failed: " .. evt.error, "error") end
end)
```

Currently emitted events:

| Event           | Payload                                                  | Fired at                                                  |
| --------------- | -------------------------------------------------------- | --------------------------------------------------------- |
| `board_load`    | `{}`                                                     | After the board's columns first load                      |
| `board_refresh` | `{reason}` — `"watcher"` / `"refresh"` / `"command"`     | After columns are reloaded from disk                      |
| `item_select`   | `{item = {column, name}, prev = {column, name}}`         | Cursor lands on a different item                          |
| `column_change` | `{column, prev}`                                         | Active column changes (left/right keys, mouse, etc.)      |
| `item_open`     | `{item, kind}` — kind is `"edit"` / `"external"`         | User opens an item for editing                            |
| `item_created`  | `{item}`                                                 | After a new item is created                               |
| `item_renamed`  | `{item, oldName}`                                        | After an item is renamed                                  |
| `item_deleted`  | `{column, name}`                                         | After delete confirmation completes                       |
| `item_moved`    | `{item, from, to}`                                       | After `kbrd.board.move` succeeds                          |
| `git_sync_done` | `{ok, stage, error}`                                     | After manual or auto git sync finishes                    |
| `git_post_commit` | (not yet emitted)                                      | reserved                                                  |

Events fired by script-driven mutations (e.g. `kbrd.board.move` inside a
command callback) are **deferred** until the script returns, then dispatched
in order. This prevents re-entering the Lua VM mid-call.

---

## The `ctx` table

Every `kbrd.command` callback receives a `ctx` with the currently-selected
item's coordinates:

```lua
ctx.fileName     -- "todo-item"        (without .md)
ctx.filePath     -- "/abs/path/to/todo-item.md"
ctx.fileDir      -- "/abs/path/to"
ctx.columnName   -- "1. TO DO"
ctx.columnPath   -- "/abs/path/to/1. TO DO"
ctx.boardName    -- configured board name, or ""
ctx.boardPath    -- "/abs/path/to/board"
```

`kbrd.board.move(ctx, "done")` works directly — the function looks at
`ctx.columnName` and `ctx.fileName` automatically. You can also build a
table by hand: `kbrd.board.move({column="todo", name="foo"}, "done")`.

---

## API reference

### `kbrd.notify(msg, level)`

Show a toast. `level` is one of `"info"`, `"success"`, `"error"` (defaults
to `"info"`). Uses your configured `notify.backend` (osascript / OSC9 / OSC777).

```lua
kbrd.notify("hello", "success")
```

### `kbrd.command(id, name, fn)` — short form
### `kbrd.command{id=, name=, description=, run=}` — table form

Register a menu entry. `id` is a unique identifier (any non-empty string —
e.g. `"archive"` or `"word-count"`). `name` is what shows in the menu, and
is what the fuzzy filter matches against. `description` is optional dim text
shown after the name. `fn` / `run` is called with `ctx`.

```lua
kbrd.command("priority", "Priority", function(ctx) ... end)

kbrd.command{
  id="archive", name="Archive", description="Move to archive",
  run=function(ctx) ... end,
}
```

Re-registering the same id replaces the previous binding (useful when
iterating on a script).

### `kbrd.has_command(id)`

Returns `true` if a Lua command with this id is currently registered, `false`
otherwise. Useful for guarded re-registration or feature-detection. Only sees
Lua-registered commands — YAML/shell entries are not visible here.

```lua
if not kbrd.has_command("archive") then
  kbrd.command("archive", "Archive", function(ctx) ... end)
end
```

### `kbrd.on(event, fn)`

Subscribe to an event. See the table above for available events. The
callback receives an event-specific payload table.

### `kbrd.board.move(item, columnName)`

Move an item. Returns `true` on success, or `nil, err` on failure.

`item` may be:
- the `ctx` table directly,
- an explicit `{column = "...", name = "..."}` table.

```lua
local ok, err = kbrd.board.move(ctx, "done")
if not ok then kbrd.notify(err, "error") end
```

### `kbrd.board.refresh()`

Re-read columns and git stats from disk. Synchronous. Returns `true` or
`nil, err`.

You rarely need to call this manually — `createColumn` already refreshes
internally, and the file watcher picks up external changes. Useful after
batch shell operations.

### `kbrd.board.createColumn(name)`

Create a new column directory under the board root, then refresh. Returns
`true` or `nil, err`. The name must be non-empty, must not contain `/` or
`\`, and must not already exist.

```lua
kbrd.board.createColumn("archive")
```

### `kbrd.ui.pick(title, choices)`

Open a list picker. Blocks the script (via a Lua coroutine) until the user
chooses or cancels. Returns the chosen string, or `nil` on cancel.

```lua
local pri = kbrd.ui.pick("Priority?", {"P0", "P1", "P2"})
if pri == nil then return end   -- user pressed esc
```

`choices` is a list of strings. Arrow keys / `j`/`k` move; enter confirms;
esc / `q` cancels.

### `kbrd.ui.prompt(title, default)`

Open a single-line text input. Returns the entered string, or `nil` on cancel.
`default` is optional; pass `""` (or omit) for an empty box.

```lua
local name = kbrd.ui.prompt("New name", ctx.fileName)
if name == nil or name == "" then return end
```

Enter submits; esc cancels.

### `kbrd.ui.confirm(title)`

Open a yes/no dialog (the same primitive kbrd uses for its own
delete-confirms). Returns `true` for yes, `false` for no.

```lua
if not kbrd.ui.confirm("Delete " .. ctx.fileName .. "?") then return end
```

### How UI calls work (briefly)

UI primitives are blocking from your script's perspective, but they don't
freeze kbrd. Internally, calling `kbrd.ui.*` suspends your script's
coroutine, kbrd opens the matching UI, and once the user responds the
script resumes with the answer. While the UI is open:

- The watchdog timer is paused (waiting for input doesn't count).
- Other key presses go to the UI, not the board.
- Multiple `kbrd.ui.*` calls in a row work — the script suspends and
  resumes once per call.

Hooks (`kbrd.on`) **cannot** call `kbrd.ui.*` — they run synchronously
and have nowhere to yield to. A yield from a hook is dropped silently.

### `kbrd.timer.every(intervalMs, fn)` / `kbrd.timer.after(delayMs, fn)`

Schedule a callback to run repeatedly (`every`) or once (`after`). Returns
an opaque handle string. The callback receives one argument — a table with
the timer's `token` (handy if you want to cancel from inside).

```lua
local h = kbrd.timer.every(30000, function()
  kbrd.notify("30 seconds passed")
end)

kbrd.timer.after(2000, function()
  kbrd.notify("reminder")
end)
```

Semantics:

- **Minimum interval: 100 ms.** Smaller values are silently clamped.
- **No drift correction.** `every` schedules the next tick after the
  previous callback returns, so a 1 s timer that takes 1.5 s to run fires
  again 1 s after that — total ~2.5 s.
- Timers run as **hooks** — they can mutate the board, but cannot open
  `kbrd.ui.*` (no coroutine context). Use a flag set by the timer and a
  command for the interactive part if you need that.
- Subject to the `scripting.hook_timeout_ms` watchdog (default 500 ms).
- **No nested registration.** A timer callback (and any hook fired by its
  side effects) cannot call `kbrd.timer.every/after`, `kbrd.command`, or
  `kbrd.on`. This prevents exponentially-growing timer pyramids, hidden
  command surface, and other footguns. Each of those calls raises a Lua
  error — wrap with `pcall` if you genuinely want to attempt it. Repeating
  timers (`every`) re-arm internally, so they keep firing without needing
  to call back into Lua.
- **No UI from a timer.** `kbrd.ui.pick / prompt / confirm` cannot run from
  a timer body — they rely on a coroutine that timers don't have. Set a
  flag from the timer and open the UI from a command if you need
  interaction.
- **`kbrd.timer.cancel(handle)` is allowed from inside a timer**, so
  self-cancelling patterns work (`if condition then kbrd.timer.cancel(token) end`).

### `kbrd.timer.cancel(handle)`

Stop a timer. Any tick already in flight becomes a no-op. Safe to call on
an unknown handle.

```lua
kbrd.timer.cancel(h)
```

### `kbrd.async.run(shellCmd, fn)`

Run a shell command on a worker goroutine. Returns immediately with an
opaque handle. When the command finishes, `fn(result)` is called on the
UI thread. `result` is a table:

```lua
{
  out      = "<stdout + stderr combined>",
  exitCode = 0,           -- nonzero on failure
  error    = "",          -- non-empty only when the process failed to start
}
```

Use this for anything slow enough to make the TUI feel laggy if run
synchronously: `curl`, `find` on a huge tree, `git log` on a big repo,
external API calls, etc.

```lua
kbrd.command("W", "Word count", function(ctx)
  kbrd.async.run("wc -w " .. string.format("%q", ctx.filePath), function(r)
    if r.exitCode ~= 0 then return end
    local n = r.out:match("(%d+)")
    kbrd.notify(ctx.fileName .. ": " .. n .. " words", "success")
  end)
end)
```

Semantics:

- Multiple async jobs run **in parallel** (each gets its own goroutine).
- Callbacks always run **sequentially on the UI thread** — same single-
  threaded execution model as commands, hooks, and timers. No locking
  needed in callback bodies.
- Callbacks run as **hooks** — they cannot open `kbrd.ui.*` (no coroutine).
  Subject to `scripting.hook_timeout_ms` watchdog (default 500 ms).
- Working directory is the board root, same as YAML shell commands.
- **Forbidden from inside timer callbacks** (use timers for the "do this
  later" part, then async for the slow work — chain them via a flag).
- **Allowed from inside async callbacks** — chained / waterfall async
  patterns work as expected.

### `kbrd.async.cancel(handle)`

Drops the registered callback. The shell process keeps running (Go can't
easily signal-kill running subprocesses), but its result is discarded when
it finishes. Useful for "if the user navigates away, ignore the result".

```lua
local h = kbrd.async.run("slow-cmd", function(r) ... end)
-- later:
kbrd.async.cancel(h)
```

### `kbrd.cell.set(id, opts)`

Add or replace a **header cell** — a small chip shown on the same row as the
`kbrd` logo, right-aligned. Cells are content-only (the `id` is just a handle
you use to update or clear the chip; it is not displayed). Chips render in
ascending `id` order. `opts` is a table:

| field  | type    | meaning                                  |
| ------ | ------- | ---------------------------------------- |
| `text` | string  | chip content                             |
| `fg`   | string  | foreground color, `"#rrggbb"` (optional) |
| `bg`   | string  | background color, `"#rrggbb"` (optional) |
| `bold` | boolean | bold text (optional)                     |

```lua
kbrd.cell.set(1, {text = "ready", fg = "#7fd962", bold = true})
```

Calling `set` again with the same `id` replaces the chip — this is the
supported way to animate a cell (see the clock and flicker recipes). It is
safe to call from a `kbrd.timer.every` callback.

Negative ids are reserved for kbrd's built-in cells (item count, git status),
so use positive ids for your own.

### `kbrd.cell.clear(id)`

Remove a single header cell. No-op if the id isn't set.

### `kbrd.cell.clear_all()`

Remove every cell **your scripts** set. Built-in cells (negative ids) are kept.

### `kbrd.fs.read(path)`

Read a file. Returns the content as a string, or `nil, err`.

```lua
local body, err = kbrd.fs.read("notes.md")
```

### `kbrd.fs.write(path, body)`

Write `body` to `path`, overwriting if it exists. Returns `true` or
`nil, err`. **Does not** create parent directories — call `kbrd.fs.mkdir`
first if needed.

```lua
kbrd.fs.write("scratch/log.md", "hello\n")
```

### `kbrd.fs.exists(path)`

Returns a boolean.

```lua
if kbrd.fs.exists("archive") then ... end
```

### `kbrd.fs.mkdir(path)`

`mkdir -p` semantics. Returns `true` or `nil, err`.

### `kbrd.fs.glob(pattern)`

Returns a list of absolute paths matching the glob. Uses Go's `filepath.Glob`
syntax (`*`, `?`, `[abc]`, no `**`). Returns `nil, err` only on malformed
patterns; an empty match is just an empty list.

```lua
for _, path in ipairs(kbrd.fs.glob("*.md")) do
  kbrd.notify(path)
end
```

### Path resolution

`kbrd.fs.*` and `kbrd.board.createColumn` accept either absolute or relative
paths. Relative paths are resolved against the **board root** (the directory
kbrd was opened in), *not* the process cwd. This matches how YAML shell
commands run with `cwd = boardPath`.

---

## Error handling

A broken script can never crash kbrd. Every Lua call is wrapped:

- **Parse error in `init.lua`** — surfaced as a notification at boot; shell
  commands and the rest of kbrd keep working.
- **Runtime error in a command** — surfaced as `<name>: <error message>` and
  the command menu closes. Board state is unchanged.
- **Runtime error in a hook** — surfaced as a notification, hook continues
  to be invoked on future events.
- **Infinite loops / runaway CPU** — the watchdog kills the call after
  `command_timeout_ms` / `hook_timeout_ms` (or `instruction_limit`, whichever
  fires first) and shows a timeout notification.
- **Errors from API calls** — most `kbrd.*` functions return `nil, err`
  instead of throwing. Use the conventional Lua pattern:

  ```lua
  local ok, err = kbrd.board.move(ctx, "done")
  if not ok then kbrd.notify(err, "error") end
  ```

### Auto-disable on consecutive errors

Repeating timers and event hooks fire many times, so a single broken
callback could spam notifications and waste CPU forever. The host tracks
each timer's and each hook function's consecutive error count and
auto-disables it after `scripting.error_threshold` (default 3) errors in
a row.

- Every error fires a normal `timer: ...` / `hook ...: ...` notification.
- On the Nth consecutive error, the timer is removed from the timer map
  (or the hook is removed from its event's subscriber list) and a final
  notification fires: `timer disabled after 3 errors` /
  `hook git_sync_done disabled after 3 errors`.
- A successful run resets the counter — flaky callbacks that mostly
  succeed keep running indefinitely.
- Set `scripting.error_threshold = 0` in `config.toml` to disable the
  auto-disable behavior — useful if you want a known-flaky callback to
  retry forever (e.g. a network poller you'd rather have keep trying than
  silently die after a few transient failures).
- The detailed error (including Lua stack trace) is appended to
  `~/.cache/kbrd/script.log` regardless.

The same threshold applies to both timers and hooks. Commands are not
auto-disabled — they only run on explicit user action, so error spam
isn't a risk.

Detailed errors (stack traces, hook failures) are appended to
`~/.cache/kbrd/script.log`.

**`os.exit` is disabled.** Calling it from any script raises a Lua error
rather than terminating kbrd. There's no legitimate use for tearing down
the process from a config script, and an accidental call would corrupt
the terminal mid-render.

---

## What's not yet available

These are planned but not in the current build:

- `kbrd.shell.run / exec` — synchronous capture / take-over (async via
  `kbrd.async.run` ships already)
- `kbrd.git.*` — read-only mirrors of kbrd's git helpers
- `kbrd.log.*` — structured logging from scripts
- `kbrd.inspect` — table pretty-printer
- `kbrd.config.get / all` — read kbrd config from Lua
- Bundled `require("json")`, `require("re")`, `require("http")`
- `~/.config/kbrd/lua/?.lua` package path for `require`

The full Lua standard library that ships with gopher-lua *is* available
(`string`, `table`, `math`, `io`, `os`), so most things are doable today —
you'll just have a chunkier script.

---

## Recipes

### Archive command

```lua
kbrd.command("A", "Archive", function(ctx)
  if not kbrd.fs.exists("archive") then
    kbrd.board.createColumn("archive")
  end
  kbrd.board.move(ctx, "archive")
  kbrd.notify("archived " .. ctx.fileName, "success")
end)
```

### Dated archive column

```lua
kbrd.command("A", "Archive (dated)", function(ctx)
  local col = os.date("archive-%Y-%m")
  if not kbrd.fs.exists(col) then kbrd.board.createColumn(col) end
  kbrd.board.move(ctx, col)
end)
```

### Save a file outside the board

```lua
kbrd.command("E", "Export", function(ctx)
  local body, err = kbrd.fs.read(ctx.filePath)
  if not body then kbrd.notify(err, "error"); return end
  kbrd.fs.write("/tmp/kbrd-export.md", body)
  kbrd.notify("exported to /tmp/kbrd-export.md", "success")
end)
```

### Priority picker

```lua
kbrd.command("P", "Priority", function(ctx)
  local choice = kbrd.ui.pick("Priority?", {"P0", "P1", "P2"})
  if choice == nil then return end
  kbrd.notify(ctx.fileName .. " marked " .. choice, "success")
end)
```

### Quick capture with prompt

```lua
kbrd.command("N", "New (prompted)", function()
  local name = kbrd.ui.prompt("Item name", "")
  if name == nil or name == "" then return end
  kbrd.fs.write("1. TO DO/" .. name .. ".md", "")
  kbrd.board.refresh()
end)
```

### Confirmed archive

```lua
kbrd.command("X", "Archive (confirmed)", function(ctx)
  if not kbrd.ui.confirm("Archive " .. ctx.fileName .. "?") then return end
  if not kbrd.fs.exists("archive") then kbrd.board.createColumn("archive") end
  kbrd.board.move(ctx, "archive")
end)
```

### Async word count

```lua
kbrd.command("W", "Word count", function(ctx)
  kbrd.async.run("wc -w " .. string.format("%q", ctx.filePath), function(r)
    if r.exitCode ~= 0 then kbrd.notify(r.error, "error"); return end
    local n = r.out:match("(%d+)")
    kbrd.notify(ctx.fileName .. ": " .. n .. " words", "success")
  end)
end)
```

### Periodic stats dump

```lua
kbrd.timer.every(30000, function()
  local lines = {}
  for _, dir in ipairs(kbrd.fs.glob("*")) do
    local name = dir:match("([^/]+)$")
    if name and not name:match("^%.") then
      local items = kbrd.fs.glob(name .. "/*.md")
      table.insert(lines, name .. ": " .. #items)
    end
  end
  kbrd.fs.write("/tmp/kbdr-stats.txt", table.concat(lines, "\n") .. "\n")
end)
```

### Live clock cell

A header cell that updates every second. kbrd ships no built-in clock — this
recipe is the recommended way to get one, keeping all animation timer-driven.

```lua
kbrd.timer.every(1000, function()
  kbrd.cell.set(10, {text = os.date("%H:%M:%S"), fg = "#94a3b8"})
end)
```

### Flicker / alert cell

Toggle a cell's color on a timer to draw attention.

```lua
local on = false
kbrd.timer.every(400, function()
  on = not on
  kbrd.cell.set(20, {
    text = "ALERT",
    bold = true,
    bg = on and "#ff0000" or "#330000",
    fg = "#ffffff",
  })
end)
```

### Auto-pin on item open

```lua
kbrd.on("item_open", function(evt)
  -- pin recently-edited items so they float to the top of the column
  -- (illustrative — needs kbrd.board.pin which is planned)
  kbrd.notify("opened: " .. evt.item.name)
end)
```

### Notify on failed auto-sync

```lua
kbrd.on("git_sync_done", function(evt)
  if not evt.ok then
    kbrd.notify("sync failed (" .. evt.stage .. "): " .. evt.error, "error")
  end
end)
```

### Count items in each column on board load

```lua
kbrd.on("board_load", function()
  local lines = {}
  -- We don't have ctx.board.columns yet, so do it with the filesystem.
  for _, dir in ipairs(kbrd.fs.glob("*")) do
    local items = kbrd.fs.glob(dir:match("([^/]+)$") .. "/*.md")
    table.insert(lines, dir .. ": " .. #items)
  end
  kbrd.fs.write("/tmp/kbrd-summary.txt", table.concat(lines, "\n") .. "\n")
end)
```

---

## Debugging tips

- Errors and panic traces go to `~/.cache/kbrd/script.log`. `tail -f` it
  while developing.
- Re-registering the same id overrides the previous binding —
  iteration is just edit-save-restart.
- Wrap suspicious lines in `pcall` to see the exact error message:
  ```lua
  local ok, err = pcall(function() ... end)
  if not ok then kbrd.notify(tostring(err), "error") end
  ```
- The watchdog defaults are conservative. If a legitimate script needs more
  time, raise `command_timeout_ms` in `config.toml`.
