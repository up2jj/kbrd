# Scripting kbrd with Lua

kbrd embeds a Lua VM (gopher-lua, Lua 5.1) so you can extend it beyond YAML
shell commands. Scripts can register new entries in the Custom Commands menu,
react to events, move items between columns, read/write files, and create new
columns on the fly.

Lua is **additive** — your existing `commands.yml` / `.kbrd_commands.yml`
shell commands keep working unchanged.

---

## Contents

- [Quick start](#quick-start)
- [Runtime layers](#runtime-layers)
- [Configuration](#configuration) · [Environment variables](#environment-variables)
- [Two ways to plug in](#two-ways-to-plug-in) — [menu commands](#1-menu-commands--kbrdcommand), [event hooks](#2-event-hooks--kbrdon)
- [Remote scripts (`require` from a URL)](#remote-scripts-require-from-a-url)
- [Declarative hooks (`hooks.yml`)](#declarative-hooks-no-lua--hooksyml)
- [The `ctx` table](#the-ctx-table)
- [API reference](#api-reference)
  - Core — [layer](#kbrdlayer--runtime-layer), [debug / inspect](#kbrddebug--kbrdinspectvalue), [notify](#kbrdnotifymsg-level), [status](#kbrdstatusmsg-ttl), [instance.name](#kbrdinstancename), [command](#kbrdcommandid-name-fn--short-form), [has_command](#kbrdhas_commandid), [register](#kbrdregistername-fn--kbrdregistername-fn-usage), [editor.open](#kbrdeditoropentarget-line), [on](#kbrdonevent-fn)
  - Transform hooks — [column_items](#kbrdoncolumn_items-fn--column-transform-hook), [frontmatter_suggestions](#kbrdonfrontmatter_suggestions-fn--frontmatter-editor-completions), [http_request / http_response](#kbrdonhttp_request-fn--kbrdonhttp_response-fn--serve-middleware)
  - [`kbrd.board.*`](#kbrdboardmoveitem-columnname) — move, create, templates, createFromTemplate, rename, delete, refresh, createColumn
  - [`kbrd.ui.*`](#scripted-ui) — input, textarea, viewer, select, multiselect, form, confirm, actions, notify, plus legacy pick/prompt
  - [`kbrd.timer.*`](#kbrdtimereveryintervalms-fn--kbrdtimerafterdelayms-fn) — every, after, cancel
  - [`kbrd.async.*`](#kbrdasyncrunshellcmd-fn) — run, cancel
  - [`kbrd.cell.*`](#kbrdcellsetid-opts) — set, clear, clear_all
  - [`kbrd.column.*`](#kbrdcolumnsetid-spec--virtual-columns) — set (virtual columns), clear, hide/show, hide_all/show_all, indicator
  - [`kbrd.column.store.*`](#kbrdcolumnstore--per-column-keyvalue-storage) — get, set, all, delete
  - [`kbrd.fs.*`](#kbrdfsreadpath) — read, write, get_frontmatter, set_frontmatter, delete_frontmatter, exists, mkdir, glob
- [Error handling](#error-handling) · [Auto-disable](#auto-disable-on-consecutive-errors)
- [What's not yet available](#whats-not-yet-available)
- [Recipes](#recipes)
- [Debugging tips](#debugging-tips)

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

## Runtime layers

Folder-local `.kbrd.lua` files can declare exclusive runtime layers. A layer is
a named setup callback that creates a related set of commands, timers, async
jobs, and virtual columns. Exactly one declared layer must set `default = true`;
it activates when the board opens. Press `l` in the TUI to search and switch.
The header shows `◆ layer <name>` for the active layer, or `◇ layer none` when
the declarations are valid but no layer setup has activated successfully.

```lua
-- Persistent base resources stay loaded in every layer.
kbrd.command("refresh-all", "Refresh everything", function()
  kbrd.board.refresh()
end)

kbrd.layer{
  id = "work",
  name = "Work",
  description = "Work queues and automation",
  default = true,
  setup = function()
    kbrd.command("focus", "Focus work", function()
      kbrd.status("work layer")
    end)
    kbrd.timer.every("15m", function()
      kbrd.status("work timer refreshed")
    end)
    kbrd.async.run("printf 'Review release notes'", function(result)
      kbrd.column.set("work-items", {
        name = "Work items",
        items = { { id = "review", title = result.out } },
      })
    end)
  end,
}

kbrd.layer{
  id = "personal",
  name = "Personal",
  description = "Personal tasks",
  setup = function()
    kbrd.command("focus", "Focus personal", function()
      kbrd.status("personal layer")
    end)
    kbrd.column.set("personal", { name = "Personal", items = {} })
  end,
}
```

Switching reruns the target `setup` and unloads the previous layer's managed
resources. Commands or virtual columns with the same id temporarily shadow a
base resource; the base version returns when the layer is left. Timer ticks and
async results that arrive after their layer was unloaded are ignored. An
already-running async shell process is not forcibly killed.

Layer selection is session-only. Lua globals, required modules, and closure
upvalues remain alive while the board is open; only the four managed resource
types are unloaded. Hooks, `kbrd.register` functions, cells, indicators, and
other side effects keep their normal global lifetime, even if called by a layer
setup. A failing setup leaves the previously active layer selected, although
unmanaged side effects that ran before the error cannot be rolled back.
The layer picker reopens with the setup error so the failure cannot look like a
successful switch. Script-load, default-layer, and interactive switch failures
also keep a red `✕ lua` indicator in the header; open the custom-command menu
with `x` to inspect the full warning after dismissing the picker.

`kbrd.layer` may only be declared while the folder-local `.kbrd.lua` is loading
(modules required by that file are included). Global `init.lua` can still
declare persistent base resources, but it cannot declare layers. Under
`kbrd serve --scripting`, the default layer activates so timers and async jobs
run; virtual-column updates are accepted but have no headless presentation.

### `kbrd.layer{...}` — runtime layer

Fields:

- `id` — required unique identifier.
- `name` — switcher label; defaults to `id`.
- `description` — optional searchable detail.
- `default` — exactly one layer must set this to `true`.
- `setup` — required callback run on activation under the normal command timeout
  and instruction limit.

---

## Configuration

Scripting is on by default. Tunables live in `~/.config/kbrd/config.toml`
(or `<board>/kbrd.toml`):

```toml
[scripting]
enabled            = true     # master switch — false disables the whole subsystem
command_timeout_ms = 2000     # wall-clock budget for kbrd.command callbacks
hook_timeout_ms    = 500      # stricter budget for event hooks (they fire on hot paths)
error_threshold    = 3        # auto-disable a timer/hook after N consecutive errors (0 = never)
remote_require     = false    # allow require() of scripts from remote URLs — see "Remote scripts"
http_timeout_ms    = 10000    # maximum per-request timeout for kbrd.http (10 seconds)
http_max_response_bytes = 2097152 # maximum response body buffered for Lua (2 MiB)
```

When `enabled = false`, no Lua VM is created and `init.lua` is not read.
Equivalent to compiling kbrd without scripting.

Launching with **`kbrd --safe`** forces `enabled = false` here, skips direnv, disables declarative hooks
(including hooks after `kbrd ingest`), and disables template `{{shell}}` exec — overriding config, including a board's folder-local
`kbrd.toml`. Use it to open a board you don't fully trust. See [SECURITY.md](./SECURITY.md).

### Environment variables

Scripts inherit kbrd's environment, and there are three ways to read it:

When direnv is installed, the TUI loads the current board's approved `.envrc`
before configuration and scripting start, and updates the environment on board
switches. `--safe` skips this integration.

- **Lua:** the standard library is available, so `os.getenv("HOME")` works directly
  (including inside `kbrd.async.run` callbacks).
- **Shell commands run by a script** (`kbrd.async.run`) and **YAML shell commands** execute via
  `sh -c`, so plain `$VAR` expands normally.
- **YAML command templates** can substitute a value before the shell runs with `{{env "VAR"}}`
  (empty string if unset) or resolve a natural-language date with `{{date "next friday"}}`
  (English/Polish; optional Go layout — see the [phrase reference](./TEMPLATES.md#natural-language-dates-date)).
  The same funcs work in [declarative hooks](#declarative-hooks-no-lua--hooksyml).

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
| `item_saved`    | `{item, kind}` — kind is `"save"` / `"append"` / `"prepend"` / `"journal"` | After an in-app save writes a card                    |
| `item_changed`  | `{item}`                                                 | Watcher saw an external edit to a card (see loop note)    |
| `item_created`  | `{item}`                                                 | After a new item is created                               |
| `item_renamed`  | `{item, oldName}`                                        | After an item is renamed                                  |
| `item_deleted`  | `{column, name}`                                         | After delete confirmation completes                       |
| `item_moved`    | `{item, from, to}`                                       | After a move (`m` / `M` keys or `kbrd.board.move`)        |
| `column_created`| `{name}`                                                 | After `kbrd.board.createColumn` succeeds                  |
| `git_sync_done` | `{ok, stage, error}`                                     | After manual or auto git sync finishes                    |
| `git_post_commit` | (not yet emitted)                                      | reserved                                                  |
| `column_items`  | `{column, pinned, items}` — **expects a return value**  | When a filesystem column's items are (re)built — see below |
| `frontmatter_suggestions` | `{column, item}` — **expects a return value** | When the frontmatter editor (`~`) opens — offers key completions and value defaults — see below |
| `http_request`  | `{method, path, query, headers, remote_addr}` — **expects a verdict** | `serve` only: before built-in auth, per HTTP request — see below |
| `http_response` | `{method, path, status, headers, body}` — **expects a verdict** | `serve` only: after the handler, before the response is sent — see below |

Events fired by script-driven mutations (e.g. `kbrd.board.move` inside a
command callback) are **deferred** until the script returns, then dispatched
in order. This prevents re-entering the Lua VM mid-call.

#### Custom events — `kbrd.emit(name, payload)`

Scripts can talk to each other. `kbrd.emit` publishes a custom event that every
`kbrd.on(name, ...)` listener receives, with the optional `payload` table passed
as the listener's argument:

```lua
kbrd.on("indexed", function(p) kbrd.status("indexed " .. p.count .. " cards") end)

kbrd.command("reindex", "Reindex", function()
  -- ...do work...
  kbrd.emit("indexed", { count = 42 })
end)
```

Notes:

- Built-in event names (the table above) are **reserved** — `kbrd.emit` returns
  `nil, err` if you try to spoof one. Pick your own names.
- Like engine events, listeners fire **after** the emitting script returns
  (deferred), so a listener that itself calls `kbrd.emit` is safe. A runaway
  ping-pong between two listeners is capped and dropped rather than hanging.

---

## Remote scripts (`require` from a URL)

You can `require` a script straight from a remote location — handy for sharing a
helper across boards and machines without copy-pasting. The fetched module runs
in the **same** VM, so it has the full `kbrd` API and can register commands,
hooks, and timers at load time.

This is **off by default**. A remote module runs with the same trust level as
your own `init.lua` — it is arbitrary code execution. Opt in explicitly:

```toml
[scripting]
remote_require = true
```

> **⚠️ Pin what you trust.** Prefer a tag or commit SHA (`@v1.2.3`, `@a1b2c3d`)
> over a moving branch, and only `require` URLs you control or have reviewed. See
> **[SECURITY.md](./SECURITY.md)**.

### Two syntaxes

```lua
-- Raw URL (you build the raw.githubusercontent.com link yourself):
require("https://raw.githubusercontent.com/owner/repo/v1.0/util.lua")

-- github: shorthand → expands to raw.githubusercontent.com. @ref is a branch,
-- tag, or commit SHA; omit it to use the repo's default branch (HEAD):
require("github:owner/repo/util.lua@v1.0")
```

### Library style — module returns a table

The module `return`s a table; the caller keeps the handle and calls its functions:

```lua
-- util.lua (remote)
local M = {}
function M.is_overdue(item)
  local due = kbrd.fs.get_frontmatter(item.path, "due")
  return due ~= nil and kbrd.date.parse(due) < os.time()
end
return M
```

```lua
-- your .kbrd.lua
local util = require("github:owner/repo/util.lua@v1.0")
kbrd.on("item_moved", function(ev)
  if util.is_overdue(ev.item) then kbrd.notify("overdue: " .. ev.item.name) end
end)
```

### Side-effect style — module registers things itself

The module just calls `kbrd.*` at load time and returns nothing; you `require` it
purely for the registration:

```lua
-- my-module.lua (remote)
kbrd.on("item_moved", function(ev) kbrd.notify("moved " .. ev.item.name) end)
kbrd.command("rc", "Remote command", function() kbrd.status("hello from a remote script") end)
```

```lua
-- your .kbrd.lua
require("https://example.com/my-module.lua")
```

Because `require` uses Lua's `package.loaded` memoization, requiring the same URL
twice returns the **same** table — it's fetched, compiled, and run once.

### Caching and purging

Fetched modules are cached on disk (under your OS cache dir, or `$KBRD_CACHE_DIR`
if set) and reused on every later start, so only the first load touches the
network. The cache is **purge-only**: a pinned tag/SHA never changes, but a
branch ref like `@main` won't pick up upstream changes until you clear the cache.

```sh
kbrd cache script list    # show cached modules and their original URLs
kbrd cache script purge   # remove them all (they re-fetch on next use)
```

The cache files are an opaque implementation detail (content-addressed names) —
don't edit them; a purge wipes them and a branch ref re-fetches over them. To
customize a remote module:

- **Runtime override** — monkeypatch the returned table from your own init file:
  ```lua
  local m = require("github:owner/repo/util.lua@v1.0")
  local orig = m.is_overdue
  m.is_overdue = function(item) return orig(item) and not item.snoozed end
  ```
- **Vendor** — for a permanent fork, copy the file into your board and
  `require("./vendor/util.lua")` (a local path); it's then a normal local script
  with no remote dependency.

### When a remote require fails

A failed fetch (network error, timeout, non-200) caches nothing, so a transient
blip can't get stuck — the next load just retries. A fetch/compile error during
load propagates like any Lua error: it stops **that** init file at the failing
line (anything registered earlier stays active) and is surfaced to you; the rest
of kbrd keeps running. If you'd rather a remote dependency's failure not abort the
rest of your init file, guard it:

```lua
local ok, util = pcall(require, "github:owner/repo/util.lua@v1.0")
if not ok then kbrd.notify("util unavailable, skipping", "warn") end
```

---

## Declarative hooks (no Lua) — `hooks.yml`

If all you want is to **run a shell command when something happens**, you don't
need Lua. Declarative hooks live in YAML, in the same format and locations as
custom commands:

- Global: `~/.config/kbrd/hooks.yml`
- Folder-local: `<board>/.kbrd_hooks.yml` (overrides global entries by `id`)

```yaml
hooks:
  - name: Stage moved card
    id: stage-on-move
    event: item_moved
    command: git -C "{{.boardPath}}" add "{{.toColumn}}/{{.fileName}}.md"
```

See **[examples/hooks/](./examples/hooks/hooks.yml)** (and its
[README](./examples/hooks/README.md)) for a fuller set: per-event logging, a
serial-execution ordering demo, and a desktop notification on move.

How they behave:

- **After-only.** Hooks observe a completed operation; they cannot cancel it.
- **Synchronous and ordered.** Hooks run one at a time, in the order listed.
  The TUI uses a single queue and shows a `⚙ hooks` indicator while they run;
  `kbrd ingest` runs matching `item_created` hooks before reporting success.
  Each hook is bounded by `hooks.timeout_ms` (default 2000); a non-zero exit or
  timeout is reported and the chain continues.
- **Lua-independent.** Hooks work even with `scripting.enabled = false`.

Variables are the same shared set as custom commands (`{{.boardPath}}`,
`{{.fileName}}`, `{{.columnName}}`, …, and `{{env "VAR"}}`), plus per-event
extras. The `{{date "..."}}` function is available here too — resolve a
natural-language date (English/Polish) with an optional Go layout, e.g.
`kbrd-cli set "{{.filePath}}" due "{{date "in 2 weeks"}}"` (see the
[phrase reference](./TEMPLATES.md#natural-language-dates-date)). Only these
low-frequency **action** events can be hooked from YAML:

| Event           | Extra variables                          |
| --------------- | ---------------------------------------- |
| `item_created`  | — (also after `kbrd ingest`)             |
| `item_open`     | `{{.kind}}`                              |
| `item_saved`    | `{{.kind}}` (`"save"` / `"append"` / `"prepend"` / `"journal"`) |
| `item_changed`  | — (external edit; see loop note below)   |
| `item_moved`    | `{{.fromColumn}}` `{{.toColumn}}`        |
| `item_renamed`  | `{{.oldName}}`                           |
| `item_deleted`  | — (`fileName`/`filePath` point to where it was) |
| `column_created`| `{{.columnName}}` `{{.columnPath}}`      |
| `git_sync_done` | `{{.ok}}` `{{.stage}}` `{{.error}}`      |
| `board_load`    | —                                        |

**Post-save rewriting & the loop hazard.** A hook bound to `item_saved` or
`item_changed` may rewrite the card file itself (a formatter, a frontmatter
stamper); the watcher re-reads it and the board shows the new content.
`item_saved` is loop-free — the hook's write is not an in-app save, so it never
re-fires `item_saved`. `item_changed` **can loop**, because the hook's own write
is exactly the kind of external change it fires on. It is gated on a content
hash, so an *idempotent* rewrite (identical bytes, e.g. `prettier --write`)
settles after one extra pass; a hook that changes the file on every run (e.g.
appending a timestamp) loops — keep it idempotent or guard with a sentinel.

The high-frequency events (`item_select`, `column_change`, `board_refresh`)
are **Lua-only**: they fire per keystroke / per watcher tick, so a slow shell
hook would back up the serial queue. Use `kbrd.on(...)` for those — Lua runs
hook logic inline (time-boxed) and you can add your own throttling.

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

When the card has a YAML frontmatter block, the ctx additionally carries:

```lua
ctx.title        -- display title (heading or file name)
ctx.path         -- same as ctx.filePath (shared key with virtual items)
ctx.data         -- the full frontmatter map, e.g. ctx.data.assignee
```

`ctx.data` includes every frontmatter key — the display ones (`accent`,
`icon`, `meta`, `tags`) and any custom keys you add. Cards without
frontmatter omit these fields.

`kbrd.board.move(ctx, "done")` works directly — the function looks at
`ctx.columnName` and `ctx.fileName` automatically. You can also build a
table by hand: `kbrd.board.move({column="todo", name="foo"}, "done")`.

---

## API reference

### `kbrd.debug(...)` / `kbrd.inspect(value)`

Use `kbrd.debug` for source-aware development output. It accepts any number of
values, formats tables deterministically, and appends the result (including the
calling Lua line) to `~/.cache/kbrd/script.log`. The global `print(...)` function
uses the same sink, so printing never corrupts the terminal UI.

`kbrd.inspect(value)` returns the same bounded, cycle-safe representation for
embedding in another string:

```lua
local state = {column = "Backlog", counts = {open = 3}}
state.self = state
kbrd.debug("startup state", state)
kbrd.notify(kbrd.inspect(state), "info")
```

The log rotates at 5 MiB and retains `script.log.1` through `script.log.3`.

### `kbrd.notify(msg, level)`

Show a desktop notification. `level` is one of `"info"`, `"success"`,
`"warning"`, `"error"` (defaults to `"info"`; unknown values also become
`"info"`). Uses your configured `notify.backend`: Kitty OSC 99 for structured
title/body notifications, WezTerm OSC 777, OSC 9 for iTerm/Ghostty-compatible
terminals, or the native macOS Notification Center companion. Install the
macOS fallback once with `kbrd companion install`.

```lua
kbrd.notify("hello", "success")
```

### `kbrd.status(msg, ttl)`

Write a message to the **status line** (the bottom bar), rather than a transient
toast. `ttl` is optional — a duration after which the message clears, given as a
number of milliseconds or a Go duration string (`"5s"`, `"500ms"`); omit it (or
pass `0`) to leave the message until something else replaces it.

```lua
kbrd.status("indexing…")
kbrd.status("done", "3s")     -- clears itself after 3 seconds
kbrd.status("done", 3000)     -- same, milliseconds
```

Use `kbrd.notify` for one-off events worth a toast; use `kbrd.status` for
ambient state you want to linger in view.

### `kbrd.instance.name`

A read-only string identifying **this** running kbrd process (a machine-local
name; empty when none is configured). It lets the *same* board script behave
differently per machine — most usefully to route an instance-scoped timer so a
periodic job runs on only one of several machines syncing the board (see the
`instance` option on [`kbrd.timer.every`](#kbrdtimereveryintervalms-fn--kbrdtimerafterdelayms-fn)).

```lua
if kbrd.instance.name == "laptop" then
  kbrd.notify("running on the laptop")
end
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

**`scope`** (optional) controls which columns the command appears on:

| `scope`     | shown on                                  |
| ----------- | ----------------------------------------- |
| `"files"`   | filesystem columns only — **the default** |
| `"virtual"` | [virtual columns](#kbrdcolumnsetid-spec--virtual-columns) only |
| `"all"`     | both                                       |
| `"line"`    | the in-editor [line-command menu](#line-commands--scope-line) only |

The default keeps file-assuming commands (which read `ctx.filePath` or call
`kbrd.board.move`) off fileless virtual columns. Set `scope = "all"` for a
command that works on any item via the shared `ctx.path`:

```lua
kbrd.command{ id="reveal", name="Reveal", scope="all",
  run=function(ctx) kbrd.async.run("open -R "..ctx.path) end }
```

The same `scope:` key works in YAML command files (`commands.yml` /
`.kbrd_commands.yml`).

### Line commands — `scope = "line"`

A **line command** runs against the line your cursor is on **inside the editor**
(edit / append / prepend / journal). Press **ctrl+l** while editing to open a
menu of line commands; the selected command receives the current line as
`ctx.line` and its **return value replaces that line**. Line commands appear only
in this menu — never in the board's **X** menu.

```lua
kbrd.command{ id="upper", name="Uppercase line", scope="line",
  run=function(ctx) return ctx.line:upper() end }
```

Return semantics:

- **Return a string** → it replaces the line. A string containing `\n` splits the
  line into several.
- **Return `nil`** (or nothing) → the line is left unchanged.

A line command may call `kbrd.ui.prompt` / `pick` / `confirm` and *then* return —
the result is applied once the prompt is answered:

```lua
kbrd.command{ id="tag", name="Append tag", scope="line",
  run=function(ctx)
    local tag = kbrd.ui.prompt("Tag", "")
    if tag == "" then return end          -- nil/"" leaves the line as-is
    return ctx.line .. " #" .. tag
  end }
```

**Shell line filters** work too: the line is fed on **stdin** and as `{{.line}}`,
and the command's **stdout** replaces the line (stderr is shown on failure, and a
non-zero exit leaves the line untouched). The replacement is undoable with one
`ctrl+z`.

```yaml
commands:
  - name: Format date
    id: fmt-date
    scope: line
    command: date -d "{{.line}}" +%Y-%m-%d
```

**`requiresItem`** (optional, default `true`) is the orthogonal axis: whether
the command needs a selected item. The default keeps item-assuming commands off
**empty columns** (where there is no selection). Set `requiresItem = false` for a
command that operates on the column itself — creating a card, a column-level
sync — so it can be invoked with no item in context. When no item is selected,
only `requiresItem = false` commands appear in the **X** menu, and the item
fields (`ctx.path`/`ctx.title`/`ctx.data`, or the `{{.filePath}}` template vars)
are omitted — a template that references them still fails cleanly. Works on both
filesystem and virtual columns, in Lua and in YAML:

```lua
kbrd.command{ id="new", name="New card", requiresItem=false,
  run=function(ctx) kbrd.async.run("touch "..ctx.columnPath.."/new.md") end }
```

```yaml
commands:
  - name: New card
    id: new-card
    requiresItem: false
    command: touch {{.columnPath}}/new.md
```

### `kbrd.has_command(id)`

Returns `true` if a Lua command with this id is currently registered, `false`
otherwise. Useful for guarded re-registration or feature-detection. Only sees
Lua-registered commands — YAML/shell entries are not visible here.

```lua
if not kbrd.has_command("archive") then
  kbrd.command("archive", "Archive", function(ctx) ... end)
end
```

### `kbrd.register(name, fn)` / `kbrd.register(name, fn, usage)`

Register a **named function** that kbrd can invoke later by *evaluating an
expression string* — for example `indent(2)`. The primary caller is the editor's
`:lua` command (see [EDITOR.md](./EDITOR.md)). Unlike `kbrd.command`, a
registered function takes **its own arguments** and never appears in a board menu;
the only way to call it is by evaling an expression that references it by name.

```lua
-- ctx carries the editor operand; the typed args are the parameters
kbrd.register("indent", function(n)
  return string.rep(" ", n) .. ctx.line
end)

-- optional usage string drives the :lua autocomplete hint
kbrd.register("wrap", function(width) return wrapText(ctx.line, width) end,
  "wrap(width) — hard-wrap the line")

-- table form
kbrd.register{ name = "bullets", usage = "bullets()", fn = function()
  local out = {}
  for _, l in ipairs(ctx.lines) do out[#out+1] = "- " .. l end
  return table.concat(out, "\n")
end }
```

When called from the editor's `:lua`, a `ctx` table is in scope describing the
operand:

| field | single line (no selection) | range (`V`-select or `:N,M`) |
|---|---|---|
| `ctx.line`  | the current line (string) | nil |
| `ctx.lines` | nil | array of selected lines |
| `ctx.text`  | the line | selected lines joined with `\n` |
| `ctx.range` | nil | `{from, to}` (1-based, inclusive) |
| `ctx.fileName` / `ctx.filePath` / `ctx.columnName` / `ctx.boardName` / `ctx.data` | the edited card's context | same |

For convenience, `line`, `lines`, and `text` are also exposed as bare globals, so
`string.rep(" ", n) .. line` works too. The expression runs in a small environment
where every registered name is visible alongside the standard library (`string`,
`math`, …) and the `kbrd` global, so a function body can call other registered
functions and use `kbrd.fs`, `kbrd.status`, etc. Re-registering the same name
**replaces** the previous function, so reloads are safe.

The first return value (a string) **replaces the operand** — the current line, or
the selected range. A `nil`/absent return leaves the buffer unchanged.
Registration is rejected from inside a timer callback, like `kbrd.command`.

> Evaluation is driven from kbrd itself — there is no `kbrd.eval` you call from
> Lua. `kbrd.register` only makes a function *available* to be evaled.

### `kbrd.editor.open(target[, line])`

Open a card in the editor, optionally at a specific 1-based line. `target` is a
path string (matched against full path, basename, or card name), or a table:

```lua
kbrd.editor.open("ideas.md", 12)                       -- by path/name, at line 12
kbrd.editor.open{ column = "Todo", name = "ideas", line = 12 }
kbrd.editor.open{ path = "/abs/path/ideas.md" }        -- line omitted = top
```

With no resolvable target (empty table) it opens the current selection. Handy from
a command or hook to jump straight to a heading or a search hit. The editor opens
after the script finishes (it is queued like `kbrd.status`).

### `kbrd.on(event, fn)`

Subscribe to an event. See the table above for available events. The
callback receives an event-specific payload table.

### `kbrd.on("column_items", fn)` — column transform hook

Unlike the other (fire-and-forget) events, `column_items` is a **transform**:
its return value becomes the column's item order. It fires whenever a
filesystem column's items are (re)built — startup, watcher reloads, manual
refresh, and after item mutations — letting a script sort, filter, or group a
column's cards.

```lua
kbrd.on("column_items", function(ev)
  if ev.column ~= "1. TO DO" then return nil end  -- decline other columns
  table.sort(ev.items, function(a, b)
    return (a.data.priority or 99) < (b.data.priority or 99)
  end)
  return ev.items
end)
```

Payload:

- `ev.column` — the column name.
- `ev.pinned` — the pinned items (those with `pinned: true` frontmatter),
  **read-only context**: they always render on top in default order and cannot
  be reordered or hidden.
- `ev.items` — the unpinned items, the transform target. Each item table has
  `name`, `title`, `pinned`, `tags`, `meta`, `icon`, `accent`, `path`, and
  `data` (the card's full YAML frontmatter, including custom keys like
  `priority`).

Return value:

- a new array of item tables — reordered and/or a subset (**omitted items are
  hidden** from the column until the hook stops hiding them; the files are
  untouched),
- entries of the form `{separator = true, title = "..."}` to inject inert
  grouping rows,
- or `nil` to decline and leave the column alone.

Returned entries are matched back to real items by `name` (then `path`);
unknown or duplicate entries are ignored, so the hook can never corrupt or
clone a card. Hooks fire in registration order and the first one returning a
table wins. Errors fall back to the default name order (and count toward the
hook's `error_threshold`). Virtual columns are exempt — they already control
their own order via `kbrd.column.set`.

A column whose order is currently script-defined shows a soft `ƒ` glyph next
to its header name (the filesystem cousin of the `◇` virtual marker), so a
hidden or reordered card is always explainable at a glance.

### `kbrd.on("frontmatter_suggestions", fn)` — frontmatter editor completions

Fired when the in-app frontmatter editor opens on a card (the `~` key). Like
`column_items` this is a **transform**: the hook returns a table of key
suggestions (or `nil` to decline). The editor surfaces these keys as
completions in its key field — alongside every key already present on any card
across the board — and uses each suggestion's default to seed the value field
**when the card does not already carry that key** (an existing value on the
card always wins).

The event payload is `{column, item}` (the target column name and the card's
file name, without `.md`). Two return shapes are accepted:

```lua
-- Map shape: { key = default_value, ... }. Order is undefined.
kbrd.on("frontmatter_suggestions", function(ev)
  return { status = "todo", priority = "2", due = "" }
end)

-- Array shape: ordered { key=, default= } entries (use when order matters).
kbrd.on("frontmatter_suggestions", function(ev)
  if ev.column ~= "TODO" then return nil end
  return {
    { key = "status",   default = "todo" },
    { key = "assignee", default = "" },
  }
end)
```

Unlike `column_items` (first non-nil wins), **every** registered
`frontmatter_suggestions` hook contributes — the results are merged, so several
scripts can each add their own keys. Keys with an empty default complete the
key but leave the value field blank. Errors count toward the hook's
`error_threshold` exactly like the other hooks.

### `kbrd.on("http_request", fn)` / `kbrd.on("http_response", fn)` — serve middleware

These two transform hooks let a script act as **request middleware** for
`kbrd serve`. They only fire under `serve --scripting`; in the TUI they are
inert. Like `column_items`, each **returns a verdict table** (or `nil` to
decline). They turn the board server into a small programmable pipeline:
custom auth, IP allow-lists, maintenance mode, vanity redirects, access
logging, header injection, response rewriting.

`http_request` fires for every request **before the built-in cookie auth**, so
it can gate even `/login`:

```lua
kbrd.on("http_request", function(req)
  -- req = { method, path, query, headers, remote_addr }
  -- headers are multi-value: req.headers["Cookie"] = { "a=1", "b=2" }
  if req.path == "/metrics" and not allowed(req.remote_addr) then
    return { action = "respond", status = 403, body = "forbidden\n" }
  end
  if req.path == "/old" then
    return { action = "redirect", location = "/", status = 302 }
  end
  return nil  -- decline → continue to the next hook / normal handling
end)
```

Request verdicts:

- `{ action = "respond", status =, body =, headers = {} }` — short-circuit with
  your own response (status defaults to 200).
- `{ action = "redirect", location =, status = }` — send a redirect (status
  defaults to 303).
- `{ action = "continue", rewrite = {...} }` (or just `nil`) — let the request
  proceed, optionally mutating it first. `rewrite` may set `path`, `query`,
  `set_headers = {}`, and `del_headers = {}`. The built-in handlers read the
  rewritten path/query/headers.

`http_response` fires **after** the handler runs but **before** the response is
sent, so it can decorate or rewrite the result:

```lua
kbrd.on("http_response", function(resp)
  -- resp = { method, path, status, headers, body }
  if resp.path == "/" then
    return { set_headers = { ["X-Frame-Options"] = "DENY" } }
  end
  return nil
end)
```

Response verdicts (any subset; `nil` leaves the response untouched):

- `set_headers = {}` — merged onto the response headers.
- `status =` — overrides the status when non-zero.
- `body =` — replaces the body (`Content-Length` is recomputed). An empty
  string `""` is a real override; omit the key to leave the body alone.

Behavior and limits:

- **Fail-open.** A hook error, timeout, or busy VM lets the request through the
  normal chain (auth still runs) rather than locking you out. Repeated errors
  auto-disable the hook per `error_threshold`, like any other hook.
- **Serialized.** The Lua VM is single-threaded, so every request that matches
  a hook is evaluated one at a time, bounded by `hook_timeout_ms`. Fine for a
  single-operator board; not a high-throughput gateway. Requests that match no
  registered hook never touch the VM (zero overhead) — and when an
  `http_response` hook exists the response is buffered, which defeats
  streaming, so register one only when you need it.
- **No form-body rewrite (v1).** `rewrite` covers path/query/headers only;
  altering POST form fields is not supported — use `action = "respond"` to
  fully take over such a request.

### `kbrd.http.request(opts, fn)` — outbound HTTP client

Schedule an HTTP or HTTPS request without blocking the TUI or serve scheduler.
It returns an opaque handle immediately (or `nil, err` when the options are
invalid), then invokes `fn(result)` on the Lua-owning goroutine:

```lua
local handle, err = kbrd.http.request({
  url = "https://api.example.com/cards",
  method = "POST",                         -- default GET
  headers = { Authorization = "Bearer " .. token },
  json = { title = "Review", tags = {"work"} },
  decode_json = true,
  timeout_ms = 5000,
}, function(res)
  if not res.ok then
    kbrd.notify("request failed: " .. res.error, "error")
    return
  end
  kbrd.status("created card " .. res.json.id)
end)
```

Request options:

- `url` — required absolute `http://` or `https://` URL.
- `method` — defaults to `GET`; any valid HTTP method token is accepted.
- `headers` — string keys whose values are a string or an array of strings.
- `body` — raw string request body.
- `json` — Lua value encoded as JSON; mutually exclusive with `body`. It sets
  `Content-Type: application/json` unless the header was supplied explicitly.
- `decode_json` — decode the response into `result.json` while retaining the
  raw `result.body`.
- `timeout_ms` — positive timeout no greater than the configured
  `http_timeout_ms` maximum.

The result is `{ok, status, headers, body, url, error?, json?}`. Response
headers preserve repeated values as arrays. HTTP 4xx/5xx responses still have
`ok = true`; `ok = false` means a transport, size-limit, timeout, or requested
JSON-decoding failure. Status/body/final URL remain available when the server
responded. Callbacks use hook semantics: they run sequentially, cannot open
`kbrd.ui.*`, and are bounded by `hook_timeout_ms`. Requests are cancelled when
the scripting host closes. Starting one inside a timer callback is rejected,
matching `kbrd.async.run`.

### `kbrd.json` / `require("json")`

Both names expose the same JSON module:

```lua
local json = require("json")
local text, err = json.encode({name = "card", missing = json.null})
local value, err = json.decode(text)
```

- `encode(value)` returns `string` or `nil, err`.
- `decode(text)` returns a Lua value or `nil, err`.
- `null` is a singleton preserving JSON null inside objects and arrays.
- `array(table?)` and `object(table?)` tag a table's JSON container type.

An untagged empty table encodes as `{}`; use `json.array()` for `[]`. Decoded
containers retain their type, including when empty. Cyclic, sparse, mixed-key,
non-finite, and unsupported values are rejected. Lua numbers are IEEE-754
doubles, so JSON integers above `2^53` may lose precision.

### `kbrd.board.move(item, columnName)`

Move an item. Returns `true` on success, or `nil, err` on failure.

`item` may be:
- the `ctx` table directly,
- an explicit `{column = "...", name = "..."}` table.

```lua
local ok, err = kbrd.board.move(ctx, "done")
if not ok then kbrd.notify(err, "error") end
```

### `kbrd.board.create(column, name)`

Create a new (empty) item named `name` in the named column. Returns `true`
or `nil, err`. Fires `item_created`.

```lua
kbrd.board.create("1. TO DO", "follow up")
```

### `kbrd.board.templates(column)`

List the card templates available to the named column — its own
`.kbrd_templates/` merged with the board-level one (column wins on a name
clash; see [TEMPLATES.md](./TEMPLATES.md)). Returns an array of
`{name=, scope=}` tables (`scope` is `"column"` or `"board"`), or `nil, err`.

```lua
local tmpls, err = kbrd.board.templates("1. TO DO")
for _, t in ipairs(tmpls) do print(t.name, t.scope) end
```

### `kbrd.board.createFromTemplate(column, template, values)`

Render the template named `template` (its display name, as returned by
`kbrd.board.templates`) with `values` and create the resulting card in the
named column. Returns `true` or `nil, err`. Fires `item_created` — the same
event a `T`-key or `n`-key creation fires.

`values` maps field keys to answers:

| Field type | Lua value |
| --- | --- |
| `input` / `text` / `select` | string |
| `multiselect` | array of strings |
| `confirm` | boolean |

Omitted keys take the field's `default` (a `select` without one takes its
first option); `required` fields must be provided and non-empty. Unknown keys
and out-of-options values are errors, and `pattern`/`min_len`/`max_len`
constraints declared on a field are enforced exactly as in the interactive
form (see [TEMPLATES.md](./TEMPLATES.md#validation)). When the template
declares no `filename`, pass the card name as `values._filename`.

```lua
local ok, err = kbrd.board.createFromTemplate("1. TO DO", "Bug report", {
  title      = "Crash on save",
  severity   = "high",
  areas      = {"UI", "data"},   -- multiselect
  regression = true,             -- confirm
})
```

Combine with `kbrd.ui.*` for interactive flows the static form can't do —
e.g. branch on earlier answers:

```lua
kbrd.command("bug", "File a bug", function(ctx)
  local title = kbrd.ui.prompt("Bug title", "")
  if not title then return end
  local sev = kbrd.ui.pick("Severity", {"low", "medium", "high"})
  if not sev then return end
  kbrd.board.createFromTemplate(ctx.columnName, "Bug report",
    {title = title, severity = sev})
end)
```

### `kbrd.board.rename(item, newName)`

Rename an item (same column). `item` may be the `ctx` table or an explicit
`{column=, name=}` table — same as `move`. Returns `true` or `nil, err`.
Fires `item_renamed`.

### `kbrd.board.delete(item)`

Delete an item. `item` may be the `ctx` table or `{column=, name=}`. Returns
`true` or `nil, err`. Fires `item_deleted`.

```lua
local ok, err = kbrd.board.delete(ctx)
```

### `kbrd.board.refresh()`

Re-read columns and git stats from disk. Synchronous. Returns `true` or
`nil, err`. Fires `board_refresh` (`reason = "command"`).

You rarely need to call this manually — `createColumn` already refreshes
internally, and the file watcher picks up external changes. Useful after
batch shell operations.

### `kbrd.board.createColumn(name)`

Create a new column directory under the board root, then refresh. Returns
`true` or `nil, err`. The name must be non-empty, must not contain `/` or
`\`, and must not already exist. Fires `column_created`.

```lua
kbrd.board.createColumn("archive")
```

### `kbrd.board.focus(column)`

Move the board's focus to the named column. Returns `true` or `nil, err` (the
column must exist). The resulting `column_change` / `item_select` hooks fire
after the script returns, so a focus hook won't re-enter mid-call.

```lua
kbrd.board.focus("Done")
```

### `kbrd.board.select(column, name)`

Focus `column` and move its cursor onto the item `name`. Returns `true` or
`nil, err` (both the column and the item must exist).

```lua
local ok, err = kbrd.board.select("Todo", "buy-milk")
if not ok then kbrd.notify(err, "error") end
```

### Scripted UI

Blocking widgets return a common result table:

```lua
{
  submitted = true,
  cancelled = false,
  action = "submit", -- an action id for kbrd.ui.actions
  value = "...",     -- input text, selected item id, or confirm boolean
  ids = {...},        -- selected IDs from multiselect
  values = {...},     -- values keyed by field ID from form
}
```

Pressing Escape returns `{cancelled=true, submitted=false, action="cancel"}`.
Item and action IDs are stable script-owned values; labels are presentation.

#### `kbrd.ui.input(options)`

Open a single-line input. Options are `title`, `label`, `initial`,
`placeholder`, `required`, `min_length`, `max_length`, `pattern`, and
`pattern_hint`. Lengths count Unicode characters. `pattern` uses Go's RE2
regular-expression syntax; `pattern_hint` is shown when it does not match.

```lua
local result = kbrd.ui.input({
  title = "Rename card",
  label = "New title",
  initial = ctx.fileName,
  required = true,
  max_length = 80,
  pattern = "^\\S.*$",
  pattern_hint = "Start with a non-space character",
})
if result.cancelled then return end
kbrd.notify("New title: " .. result.value)
```

#### `kbrd.ui.select(options)`

Options are `title`, `items`, `searchable`, and `initial_id`. Each item requires
unique string `id` and `label` fields and may set `description`, `icon`,
`disabled`, `disabled_reason`, and `group`. Arrow keys or `j`/`k` move; Enter
submits the selected ID. Typing filters a searchable select.

```lua
local result = kbrd.ui.select({
  title = "Move to column",
  searchable = true,
  initial_id = "todo",
  items = {
    {id="todo", label="Todo", icon="○", group="Board"},
    {id="doing", label="Doing", description="2 of 3 cards", group="Board"},
    {id="done", label="Done", disabled=true, disabled_reason="Archived"},
  },
})
if not result.cancelled then kbrd.board.move(ctx, result.value) end
```

#### `kbrd.ui.textarea(options)`

Open an editable multiline buffer. Options are `title`, `initial`,
`line_numbers` (default `false`), and `actions`.
Every action needs a unique `id`, `label`, and shortcut `key`; it may also set
`primary`, `destructive`, `disabled`, and `disabled_reason`. At least one
action is required. Shortcuts must use `ctrl+` or `alt+`.

The widget uses standard textarea editing. Escape cancels it. An action returns
the full edited text in `value` and its ID in `action`.

```lua
local result = kbrd.ui.textarea({
  title = "Scratchpad",
  initial = "Ideas go here\n",
  line_numbers = true,
  actions = {
    {id="save", label="Save", key="ctrl+s", primary=true},
  },
})
if result.cancelled then return end
if result.action == "save" then kbrd.notify("Saved scratchpad") end
```

#### `kbrd.ui.viewer(options)`

Open a read-only scrollable document. Options are `title`, `content`, `format`,
`wrap` (default `true`), `line_numbers` (default `false`), and optional
`actions`. Supported formats are `plain` (default), `markdown`, `diff`, `json`,
`yaml`, and `log`. JSON is pretty-printed when valid; the other formats receive
format-aware terminal styling without changing their content. Use `j`/`k`,
Page Up/Page Down, `g`/`G`, or the mouse wheel to scroll. When wrapping is
disabled, use `h`/`l` or Left/Right to pan horizontally.

Viewer actions use the shared action schema, require shortcut keys, and return
their ID in `action`. They cannot use the viewer's navigation keys.

```lua
local result = kbrd.ui.viewer({
  title = "Generated patch",
  content = patch,
  format = "diff",
  line_numbers = true,
  actions = {
    {id="apply", label="Apply", key="ctrl+a", primary=true},
    {id="back", label="Back", key="ctrl+b"},
  },
})
if result.action == "apply" then kbrd.notify("Patch approved") end
```

#### `kbrd.ui.confirm(options)`

Options are `title`, `message`, `detail` (a list of lines), `confirm_label`,
`reject_label`, `default`, and `destructive`. The safe default is rejection;
set `default=true` only when confirmation should be preselected. A destructive
confirm uses danger styling. Submitting either button returns its boolean in
`value`; Escape is cancellation.

```lua
local result = kbrd.ui.confirm({
  title = "Delete card",
  message = "Delete " .. ctx.fileName .. "?",
  detail = {"This cannot be undone."},
  confirm_label = "Delete",
  reject_label = "Keep",
  destructive = true,
})
if result.submitted and result.value then kbrd.board.delete(ctx) end
```

#### `kbrd.ui.multiselect(options)`

Options are `title`, `items`, `searchable`, and `initial_ids`. Items use the
same stable-ID schema as `kbrd.ui.select`. `initial_ids` is an optional list of
enabled item IDs. Arrow keys move, Space toggles the focused item, and Enter
submits the selected IDs in declaration order as `result.ids`. An empty
selection is valid.

```lua
local result = kbrd.ui.multiselect({
  title = "Areas",
  searchable = true,
  initial_ids = {"ui"},
  items = {
    {id="ui", label="UI"},
    {id="data", label="Data"},
    {id="ops", label="Operations", disabled=true, disabled_reason="Not available"},
  },
})
if result.cancelled then return end
for _, id in ipairs(result.ids) do kbrd.notify("Selected " .. id) end
```

#### `kbrd.ui.form(options)`

A form requires `fields` and accepts an optional `title`. Every value-producing
field requires a unique string `id`; `label` and `separator` are display-only
and do not require one. Submission returns `result.values`, keyed by field ID.

| Field type | Options | Result |
|---|---|---|
| `input` | `label`, `description`, `initial`, `placeholder`, `required`, `min_length`, `max_length`, `pattern`, `pattern_hint` | string |
| `textarea` | same as `input` | string |
| `select` | `label`, `description`, `initial` ID, `items` | string ID |
| `multiselect` | `label`, `description`, `initial` IDs, `items`, `required` | array of string IDs |
| `checkbox` | `label`, `description`, boolean `initial`, `required` | boolean |
| `number` | `label`, `description`, numeric `initial`, `placeholder`, `required` | number, or absent when optional and empty |
| `label` | `label`, `description` | no value |
| `separator` | `label` | no value |

Select and multiselect items use the same schema as `kbrd.ui.select`. Disabled
items are shown by the standalone multi-select but omitted from form fields;
an initial value must name an enabled item. Input lengths count Unicode
characters and patterns use RE2. A required checkbox must be checked.

```lua
local result = kbrd.ui.form({
  title = "Promote to card",
  fields = {
    {id="title", type="input", label="Title", required=true, max_length=80},
    {id="body", type="textarea", label="Body"},
    {id="column", type="select", label="Column", items={
      {id="todo", label="Todo"}, {id="doing", label="Doing"},
    }},
    {id="tags", type="multiselect", label="Tags", items={
      {id="ui", label="UI"}, {id="data", label="Data"},
    }},
    {id="remove", type="checkbox", label="Remove from source", initial=true},
  },
})
if result.cancelled then return end
kbrd.notify("Promoting " .. result.values.title .. " to " .. result.values.column)
```

#### `kbrd.ui.actions(options)`

Options are `title` and `actions`. Each action requires unique string `id` and
`label` fields and may set `key`, `primary`, `destructive`, `disabled`, and
`disabled_reason`. Shortcut keys must be unique and cannot replace reserved
navigation, Enter, or Escape keys. The chosen ID is returned as both `action`
and `value`.

```lua
local result = kbrd.ui.actions({
  title = "Card action",
  actions = {
    {id="open", label="Open"},
    {id="archive", label="Archive", key="ctrl+a", destructive=true},
  },
})
if result.action == "archive" then kbrd.board.move(ctx, "Archive") end
```

#### `kbrd.ui.notify(options)`

Send a non-blocking notification. It does not suspend the command and returns
no value. `message` is required; `level` is `info` (default), `success`,
`warning`, or `error`.

```lua
kbrd.ui.notify({message="Card saved", level="success"})
```

#### Legacy scalar calls

##### `kbrd.ui.pick(title, choices)`

Open a list picker. Blocks the script (via a Lua coroutine) until the user
chooses or cancels. Returns the chosen string, or `nil` on cancel.

```lua
local pri = kbrd.ui.pick("Priority?", {"P0", "P1", "P2"})
if pri == nil then return end   -- user pressed esc
```

`choices` is a list of strings. Arrow keys / `j`/`k` move; enter confirms;
Escape or `ctrl+p` cancels.

##### `kbrd.ui.prompt(title, default)`

Open a single-line text input. Returns the entered string, or `nil` on cancel.
`default` is optional; pass `""` (or omit) for an empty box.

```lua
local name = kbrd.ui.prompt("New name", ctx.fileName)
if name == nil or name == "" then return end
```

Enter submits; esc cancels.

##### `kbrd.ui.confirm(title)`

Open a yes/no confirm and return `true` for yes, `false` for no or Escape.

```lua
if not kbrd.ui.confirm("Delete " .. ctx.fileName .. "?") then return end
```

#### Chained workflow

```lua
local title = kbrd.ui.input({title="New card", required=true})
if title.cancelled then return end
local column = kbrd.ui.select({title="Column", items={
  {id="todo", label="Todo"}, {id="doing", label="Doing"},
}})
if column.cancelled then return end
local ok = kbrd.ui.confirm({title="Create?", default=true})
if ok.submitted and ok.value then
  kbrd.board.create(column.value, title.value)
end
```

#### How UI calls work (briefly)

UI primitives are blocking from your script's perspective, but they don't
freeze kbrd. Internally, calling `kbrd.ui.*` suspends your script's
coroutine, kbrd opens the matching UI, and once the user responds the
script resumes with the answer. While the UI is open:

- The watchdog timer is paused (waiting for input doesn't count).
- Other key presses go to the UI, not the board.
- Multiple `kbrd.ui.*` calls in a row work — the script suspends and
  resumes once per call.

Only command coroutines may open blocking UI. Hooks (`kbrd.on`), timers, and
async callbacks cannot call `input`, `textarea`, `viewer`, `select`,
`multiselect`, `confirm`, `form`, `actions`, `pick`, or `prompt`; those contexts have nowhere to yield
and receive an actionable Lua error. `kbrd.ui.notify` remains safe because it
is non-blocking. Switching
boards, reloading scripts, entering safe mode, or shutting down cancels an open
widget and discards its coroutine, so an old script cannot resume against a new
board.

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

**Instance routing (optional third arg).** Pass `{instance = "<name>"}` to make a
timer fire **only** on the machine whose [`kbrd.instance.name`](#kbrdinstancename)
matches. On every other machine the call is a no-op (you still get a handle back),
so a board synced across several machines can run a periodic job on exactly one of
them — without branching the script per host.

```lua
-- only the machine named "server" runs this hourly job
kbrd.timer.every(3600000, function()
  kbrd.async.run("./scripts/backup.sh")
end, { instance = "server" })
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

### `kbrd.column.set(id, spec)` — virtual columns

A **virtual column** is a column whose items come from your script instead of a
filesystem directory. It renders to the right of the real columns (with a
distinct double border and a `◇` marker), `tab`/`shift+tab` navigate into it like
any column, but **files can't be moved into or out of it** — it's a read-only
view. Use it to show a cross-cutting list, e.g. every open task across all your
boards.

`kbrd.column.set` creates or replaces a virtual column. It's idempotent and safe
to call from a `board_load` hook, an `kbrd.async.run` callback, or a command body
— call it again to refresh the contents.

```lua
kbrd.column.set("tasks", {
  name  = "Tasks",            -- header label (defaults to the id)
  empty = "no open tasks",    -- placeholder shown while there are no items
  width = 48,                 -- content width override (omit/0 → display.column_width)
  header = { fg = "#1a1b26", bg = "#e0af68" }, -- header bar colors (hex; omit for default)
  items = {
    { separator = true, title = "Work" },          -- inert grouping row
    { id    = "t1",                                 -- stable key (cursor survives a re-push)
      title = "ship release notes",                 -- line 1
      preview = "due friday",                       -- line 2
      meta  = "work-board",                         -- line 3 (replaces mtime/size)
      icon  = "☐",                                  -- glyph before the title
      accent = "#e0af68",                           -- title/icon color (hex)
      path  = "/abs/path/notes.md",                 -- optional backing file → ctx.path
      data  = { line = 12, board = "work" },        -- opaque payload → ctx.data
    },
  },
  commands = {                                       -- column-scoped actions (the X menu / hint bar)
    { id = "open", name = "Open source", key = "o", default = true,
      run = function(ctx) ... end },                 -- default=true → bound to Enter
  },
})
```

Item fields are all optional except `title`. `separator = true` makes an inert
grouping row (only `title`/`accent` apply — no actions, no quick-jump tag, hidden
from the filter). `id` is the stable cursor key across re-pushes; it falls back
to `title`.

`width`, `header.fg`, and `header.bg` are optional appearance overrides. `width`
sets this column's content width independently of the global `display.column_width`
(the rest of the strip keeps the default); `header.fg`/`header.bg` paint the
column's header bar (both hex, applied in focused and unfocused states alike).
Each is reset to its default when a later `kbrd.column.set` for the same id omits
it.

**Actions.** Virtual items have no built-in mutation keys. Instead:

- Column-scoped `commands` appear first in the **X** menu and (if they set `key`)
  in the bottom hint bar. The one with `default = true` also runs on **Enter**
  (otherwise Enter opens `path`, if set).
- A command's `run(ctx)` receives the item payload: `ctx.data` (your table),
  `ctx.path`/`ctx.filePath`, `ctx.title`, `ctx.columnName`, `ctx.vid`.
- Global `kbrd.command` / YAML commands appear too **only if** they opt in with
  `scope = "virtual"` or `scope = "all"` (see `scope` below).
- A column command set with `requiresItem = false` also runs on an **empty**
  column (its `run(ctx)` then gets only `ctx.columnName`/`ctx.vid`, no item
  fields). If it's also `default = true`, **Enter** fires it on an empty column.

The command does whatever it does (typically writing the *source* file via
`kbrd.fs.write(ctx.data.path, …)`); to reflect the change, call `kbrd.column.set`
again. The cursor is preserved by `id`.

### `kbrd.column.clear(id)` / `kbrd.column.clear_all()`

Remove one virtual column, or all of them.

### `kbrd.column.hide(name)` / `show(name)` / `hide_all(type)` / `show_all(type)`

Hide and restore **filesystem** columns by their exact, case-sensitive names.
Visibility is session-scoped: it survives manual and watcher refreshes, but is
reset on restart or when switching boards. Top-level calls work during script
startup, before the board's columns have been rendered:

```lua
local ok, err = kbrd.column.hide("Archive")
if not ok then kbrd.notify(err, "warning") end

kbrd.column.show("Archive")   -- restore it in its original position
kbrd.column.hide_all("real") -- hide every current filesystem column
kbrd.column.show_all("real") -- restore every filesystem column
kbrd.column.show_all()       -- legacy shorthand for show_all("real")
```

Hidden columns are removed from the TUI, navigation, and menus, but remain
loaded, watched, and discoverable through global search. Opening a search hit
prefers an active virtual item backed by the same path; otherwise kbrd reveals
the hidden filesystem column. Explicit named operations can still use hidden
columns, so a script may move a completed card into a hidden archive. Named
`hide` and `show` reject virtual columns.

`hide` and `show` return `true` on success, including repeated calls that make
no change, or `nil, err` for an unknown/virtual name. Hiding the final visible
filesystem column is allowed when a virtual column keeps the board operable;
otherwise it is rejected.

The bulk functions accept exactly `"real"` or `"virtual"` and return `true`
on success (including idempotent calls), or `nil, err`. They are atomic when
hiding would leave the board with no visible columns:

```lua
kbrd.column.hide_all("virtual")
-- Existing and subsequently created virtual columns stay hidden this session.
kbrd.column.show_all("virtual")
```

Bulk-hiding real columns records their current names, so an individually named
`show` can restore one; a new filesystem column created later is visible.
Bulk-hiding virtual columns is a session mode that also covers later script
pushes and layer reconciliation. `show_all("virtual")` reveals the definitions
that still exist. It does not recreate definitions removed by
`kbrd.column.clear` / `clear_all`, which remain destructive operations.

Use the existing column store when a command-driven preference should survive
an app restart:

```lua
local archive = "Archive"
local hidden = kbrd.column.store.get(archive, "hidden") == true
if hidden then kbrd.column.hide(archive) end

kbrd.command{
  id = "archive-visibility", name = "Toggle archive column", scope = "all",
  run = function()
    local ok, err
    if hidden then
      ok, err = kbrd.column.show(archive)
    else
      ok, err = kbrd.column.hide(archive)
    end
    if not ok then return kbrd.notify(err, "error") end
    hidden = not hidden
    kbrd.column.store.set(archive, "hidden", hidden)
  end,
}
```

### `kbrd.column.indicator(name, opts)` — header label

Attach a short, styled label to a **filesystem** column's header (the
per-column analogue of `kbrd.cell.*`). It renders just after the column title,
next to the `ƒ`/`◇` markers. Purely cosmetic and entirely script-driven — kbrd
never sets one itself — so it's the generic way to surface per-column state
(a sort mode, a sync status, an over-limit warning).

`name` is the column name. The second argument is a **string**, a **table**, or
**nil**:

```lua
kbrd.column.indicator("1. To do", "↓ prio")                       -- text, default accent color
kbrd.column.indicator("1. To do", { text = "↓ prio", fg = "#e0af68", bold = true })
kbrd.column.indicator("1. To do", nil)                            -- clear (also: "" clears)
```

- `text` — the label. An empty string (or `nil`) clears the column's indicator.
- `fg` — `"#rrggbb"`; omitted uses the same soft accent as the `ƒ` marker.
- `bold` — optional.

The header has flexible width, so the label compresses the spacer before the
count; keep it short (a couple of glyphs/words) so it doesn't crowd a narrow
column. The indicator lives in memory and survives column reloads, but not an
app restart — re-apply it from a `board_load` hook (or wherever you set the
state it reflects).

### `kbrd.column.store.*` — per-column key/value storage

A small persistent key/value store scoped to each **filesystem** column, for
scripts that need to remember per-column state between runs (a view mode, a
last-sync timestamp, a cached id). It is **separate from board config**: it
governs nothing in the app and is never shown in the UI. Each column's store is
a hidden `<column>/.kbrd.toml` that travels with the column when its directory
is renamed.

`column` is a filesystem column **name**. Virtual columns (created via
`kbrd.column.set`) have no directory and return `nil, err`.

```lua
kbrd.column.store.set("To Do", "view", "compact")     -- → true | nil, err
kbrd.column.store.set("To Do", "last_sync", os.time())
kbrd.column.store.set("To Do", "tags", { "urgent", "review" })  -- tables/arrays too

local view = kbrd.column.store.get("To Do", "view")    -- "compact"; nil if unset
for k, v in pairs(kbrd.column.store.all("To Do")) do   -- whole table as a Lua table
  kbrd.notify(k .. " = " .. tostring(v))
end

kbrd.column.store.delete("To Do", "last_sync")          -- → true | nil, err
```

- `kbrd.column.store.get(column, key)` returns the value, or a single `nil` when the
  key is unset (distinct from the `nil, err` pair returned on failure).
- Values round-trip through TOML. Lua numbers come back as numbers, but note a
  Lua integer like `3` may persist as `3.0` (Lua has one number type) — `== 3`
  still holds. Strings, booleans, arrays, and nested tables round-trip as-is.
- Writes are atomic and serialized per column, so concurrent timer/async
  callbacks won't corrupt or lose each other's keys.

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

### `kbrd.fs.get_frontmatter(path)` / `kbrd.fs.get_frontmatter(path, key)`

Read a card's frontmatter **without modifying it**. With a `key`, returns just that
key's value (`nil` if absent); without a key, returns a table of every top-level key
(an empty table when the card has no frontmatter). YAML scalars come back as Lua
strings / numbers / booleans. An unquoted date such as `due: 2026-06-24` is returned
as the string `"2026-06-24"`, ready to hand to [`kbrd.date.parse`](#kbrddateparsephrase-layout).
A read error (e.g. a missing file) or malformed YAML returns `nil, err`.

```lua
local due = kbrd.fs.get_frontmatter(ctx.path, "due")   -- "next friday" | "2026-06-24" | nil
local fm  = kbrd.fs.get_frontmatter(ctx.path)          -- { due = "...", pinned = true, ... }
```

This pairs with `set_frontmatter` to resolve a natural-language date field in place —
read the phrase, parse it, write the concrete date back (idempotent, since a resolved
ISO date re-parses to itself):

```lua
kbrd.on("item_saved", function(ctx)
  local path = ctx.item.column .. "/" .. ctx.item.name .. ".md"  -- resolved vs board root
  local phrase = kbrd.fs.get_frontmatter(path, "due")
  if not phrase then return end
  local resolved = kbrd.date.parse(phrase)               -- "2006-01-02"
  if resolved and resolved ~= phrase then
    kbrd.fs.set_frontmatter(path, "due", resolved)
  end
end)
```

### `kbrd.fs.set_frontmatter(path, key, value)` / `kbrd.fs.set_frontmatter(path, table)`

Set one or more top-level YAML frontmatter keys on the card at `path`, **merging
them into the existing block** — an existing key is replaced in place, a new key
is appended, and every other line is preserved (the block is created if the file
has none). The table form sets all of its keys in a single write, in sorted key
order. Values may be strings (written verbatim, so a string scalar owns its own
quoting), numbers, or booleans. The card must exist — a missing path returns
`nil, err`. Returns `true` or `nil, err`. The change lands on disk; the file
watcher re-renders the card (or call `kbrd.board.refresh()`).

```lua
kbrd.fs.set_frontmatter(ctx.path, "accent", "red")
kbrd.fs.set_frontmatter(ctx.path, "pinned", true)

-- merge several keys at once, keeping any other existing keys intact
kbrd.fs.set_frontmatter(ctx.path, {
  accent   = "red",
  pinned   = true,
  priority = 1,
})
```

### `kbrd.fs.delete_frontmatter(path, key)`

Remove a top-level frontmatter `key` from the card at `path`; an absent key
leaves the file unchanged. Returns `true` or `nil, err`.

```lua
kbrd.fs.delete_frontmatter(ctx.path, "pinned")
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

### `kbrd.date.parse(phrase [, layout])`

Resolves a natural-language date `phrase` (**English or Polish**) relative to now
and returns it formatted with an optional Go `layout` (default `"2006-01-02"`). On
an unparseable phrase it returns `nil, err` (the standard error tuple), so a typo
surfaces rather than producing a wrong date.

```lua
kbrd.command("d", "Set due date", function(ctx)
  local phrase = kbrd.ui.prompt("Due (e.g. 'next friday', 'za 2 tygodnie'):", "")
  if not phrase or phrase == "" then return end
  local due, err = kbrd.date.parse(phrase)
  if not due then
    kbrd.notify("bad date: " .. err, "error")
    return
  end
  kbrd.fs.set_frontmatter(ctx.filePath, "due", due)
  kbrd.notify("due " .. due, "success")
end)
```

See the [phrase reference](./TEMPLATES.md#natural-language-dates-date) for the full
list of supported forms (keywords, weekdays, `next/last`, `in N`/`za N`, `N ago`/`N temu`,
periods, and times). The same `date` function is also available in card templates and in
YAML commands/hooks.

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
  `command_timeout_ms` / `hook_timeout_ms` and shows a timeout notification.
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
- `kbrd.config.get / all` — read kbrd config from Lua
- Bundled `require("re")` and standalone `require("http")` compatibility module
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
  kbrd.fs.write("/tmp/kbrd-stats.txt", table.concat(lines, "\n") .. "\n")
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

Pinning is just a `pinned: true` frontmatter key, so a hook can pin the card you
open so it floats to the top of its column. Paths in `kbrd.fs.*` resolve against
the board root, so `column/name.md` is enough.

```lua
kbrd.on("item_open", function(evt)
  local path = evt.item.column .. "/" .. evt.item.name .. ".md"
  kbrd.fs.set_frontmatter(path, "pinned", true)
end)
```

### Cyclable, persisted column sort

A real-world combo of four newer APIs working together: a
[`kbrd.command`](#kbrdcommandid-name-fn--short-form) cycles a sort mode,
[`kbrd.column.store`](#kbrdcolumnstore--per-column-keyvalue-storage) remembers the choice
**per column across restarts**, a [`column_items`](#kbrdoncolumn_items-fn--column-transform-hook)
transform applies it, and a [`column.indicator`](#kbrdcolumnindicatorname-opts--header-label)
labels the header with the active sort. Run **Cycle sort** from the `x` menu to
rotate priority → name → newest on the focused column; the order survives a
restart because the mode lives in the column's hidden `.kbrd.toml`.

```lua
local SORT_MODES = { "priority", "name", "newest" }
local SORT_LABEL = { priority = "↓ prio", name = "A→Z", newest = "↓ new" }

local function next_mode(cur)
  for i, m in ipairs(SORT_MODES) do
    if m == cur then return SORT_MODES[(i % #SORT_MODES) + 1] end
  end
  return SORT_MODES[1]
end

-- a table.sort comparator for the given mode
local function comparator(mode)
  if mode == "name" then
    return function(a, b) return a.name < b.name end
  elseif mode == "newest" then          -- ISO `created:` date sorts fine as text
    return function(a, b) return (a.data.created or "") > (b.data.created or "") end
  else                                  -- "priority": lower number floats up
    return function(a, b) return (a.data.priority or 99) < (b.data.priority or 99) end
  end
end

-- `x` menu entry: cycle the focused column's mode and persist it
kbrd.command{
  id = "cycle-sort", name = "Cycle sort",
  description = "priority → name → newest, per column",
  run = function(ctx)
    local nxt = next_mode(kbrd.column.store.get(ctx.columnName, "sort") or "priority")
    kbrd.column.store.set(ctx.columnName, "sort", nxt)
    kbrd.notify(ctx.columnName .. " → sort by " .. nxt, "success")
    kbrd.board.refresh()                -- re-runs the transform with the new mode
  end,
}

-- apply the stored mode whenever a column is (re)built
kbrd.on("column_items", function(ev)
  local mode = kbrd.column.store.get(ev.column, "sort")
  kbrd.column.indicator(ev.column, mode and SORT_LABEL[mode] or nil)
  if not mode then return nil end       -- no mode chosen → leave the default order
  table.sort(ev.items, comparator(mode))
  return ev.items
end)
```

**Banding with separators.** A `column_items` transform can also inject inert
`{separator = true, title = ...}` rows to group a column. Swap the hook body
above for this to float bug cards (named `bug-*` or tagged `bug`) into their own
band, sorted within each group by the same comparator:

```lua
local function is_bug(it)
  if it.name:match("^bug%-") then return true end
  for _, t in ipairs(it.tags or {}) do if t == "bug" then return true end end
  return false
end

kbrd.on("column_items", function(ev)
  local cmp = comparator(kbrd.column.store.get(ev.column, "sort") or "priority")
  local bugs, rest = {}, {}
  for _, it in ipairs(ev.items) do
    table.insert(is_bug(it) and bugs or rest, it)
  end
  table.sort(bugs, cmp); table.sort(rest, cmp)

  local out = {}
  if #bugs > 0 then
    out[#out + 1] = { separator = true, title = "🐞 bugs" }
    for _, it in ipairs(bugs) do out[#out + 1] = it end
  end
  for _, it in ipairs(rest) do out[#out + 1] = it end
  return out
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

### Virtual column — open tasks, with "mark complete"

A virtual **Tasks** column built from open `- [ ]` checkboxes, with `complete`
and `edit` commands that rewrite the source file. `rg` runs with the active board
as its working directory, so `.` scans the current board; use `~/boards` to go
cross-board.

It stays in sync by hooking two events — `board_load` (open / board switch) and
`board_refresh` (the file watcher fires this whenever a `.md` file is added or
edited) — so newly added tasks appear without a manual refresh. (`kbrd.async.run`
can't be called from a timer, so events are how you get periodic-feeling updates.)

The full, ready-to-use script lives in
**[examples/tasks/tasks.lua](./examples/tasks/tasks.lua)** (see its
[README](./examples/tasks/README.md) for install + caveats). The shape is:

```lua
local function refresh()
  kbrd.async.run([[rg --line-number --no-heading -e '^\s*- \[ *\]\s' . --glob '*.md']],
    function(r)
      -- …parse `path:line:text` rows into items…
      kbrd.column.set("tasks", { name = "Tasks", items = items, commands = { … } })
    end)
end

kbrd.on("board_load", refresh)     -- open / board switch
kbrd.on("board_refresh", refresh)  -- file watcher: added / edited / completed tasks
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

- `.kbrd.lua` is initialized before the board opens. A syntax, top-level,
  layer-validation, or default-layer setup error opens a recovery screen instead
  of the board. Press `e` to edit, `r` to retry, or `enter` for the traceback.
- Errors, panic traces, `print(...)`, and `kbrd.debug(...)` output go to
  `~/.cache/kbrd/script.log`. `tail -f` it while developing. The file rotates at
  5 MiB and retains three archives.
- Re-registering the same id overrides the previous binding —
  iteration is just edit-save-restart.
- Wrap suspicious lines in `pcall` to see the exact error message:
  ```lua
  local ok, err = pcall(function() ... end)
  if not ok then kbrd.notify(tostring(err), "error") end
  ```
- The watchdog defaults are conservative. If a legitimate script needs more
  time, raise `command_timeout_ms` in `config.toml`.
