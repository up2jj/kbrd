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
```

When `enabled = false`, no Lua VM is created and `init.lua` is not read.
Equivalent to compiling kbrd without scripting.

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

| Event           | Payload                                          | Fired at                              |
| --------------- | ------------------------------------------------ | ------------------------------------- |
| `git_sync_done` | `{ok, stage, error}`                             | After manual or auto git sync finishes |
| `item_moved`    | `{item = {column, name}, from, to}`              | After `kbrd.board.move`               |
| `board_load`    | `{}`                                             | After board's columns first load      |

(More events — `item_select`, `column_change`, `item_open`, etc. — are
planned but not yet emitted.)

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

### `kbrd.command(shortcut, name, fn)` — short form
### `kbrd.command{shortcut=, name=, description=, run=}` — table form

Register a menu entry. `shortcut` is a single character. `name` is what
shows in the menu. `description` is optional dim text shown after the name.
`fn` / `run` is called with `ctx`.

```lua
kbrd.command("p", "Priority", function(ctx) ... end)

kbrd.command{
  shortcut="A", name="Archive", description="Move to archive",
  run=function(ctx) ... end,
}
```

Re-registering the same shortcut replaces the previous binding (useful when
iterating on a script).

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

Detailed errors (stack traces, hook failures) are appended to
`~/.cache/kbrd/script.log`.

---

## What's not yet available

These are planned but not in the current build:

- `kbrd.shell.run / exec` — capture or take over a shell command
- `kbrd.git.*` — read-only mirrors of kbrd's git helpers
- `kbrd.timer.every / after` — scheduled callbacks
- `kbrd.async` — background work
- `kbrd.log.*` — structured logging from scripts
- `kbrd.inspect` — table pretty-printer
- `kbrd.config.get / all` — read kbrd config from Lua
- Bundled `require("json")`, `require("re")`, `require("http")`
- `~/.config/kbrd/lua/?.lua` package path for `require`
- More events: `item_select`, `column_change`, `item_open`,
  `item_created/renamed/deleted`, `board_refresh`, etc.

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
- Re-registering the same shortcut overrides the previous binding —
  iteration is just edit-save-restart.
- Wrap suspicious lines in `pcall` to see the exact error message:
  ```lua
  local ok, err = pcall(function() ... end)
  if not ok then kbrd.notify(tostring(err), "error") end
  ```
- The watchdog defaults are conservative. If a legitimate script needs more
  time, raise `command_timeout_ms` in `config.toml`.
