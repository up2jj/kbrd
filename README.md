<div align="center">

# kbrd

### A terminal-based, keyboard-driven **Kanban board** for the command line

[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Bubble Tea](https://img.shields.io/badge/TUI-Bubble%20Tea-FF75B7)](https://github.com/charmbracelet/bubbletea)
[![Lua](https://img.shields.io/badge/Scripting-Lua%205.1-000080?logo=lua&logoColor=white)](./SCRIPTING.md)
[![MCP](https://img.shields.io/badge/MCP-ready-6E56CF)](https://modelcontextprotocol.io)
![Version](https://img.shields.io/badge/version-v0.10.0-brightgreen)

*Your board is just a folder. Columns are directories. Cards are Markdown files.*

</div>

---

kbrd stores everything as plain files on disk — there is no database and no lock-in. A
**board** is a directory, each **column** is a sub-directory, and each **card** is a
Markdown (`.md`) file. Because the board *is* the filesystem, you can edit it with any
editor, version it with git, sync it with your usual tools, and let kbrd render it as a
live, navigable board.

Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea) and friends, kbrd
adds git integration, fuzzy board switching, cross-board search, an embedded Lua scripting
engine, custom shell commands, and a built-in MCP server for LLM/agent tooling.

<p align="center">
  <img src="docs/screenshots/board.png" alt="kbrd board view" width="900">
</p>

---

## Table of contents

- [Features](#features)
- [Screenshots](#screenshots)
- [Installation](#installation)
- [Getting started](#getting-started)
- [Keyboard shortcuts](#keyboard-shortcuts)
- [Clipboard ring](./CLIPBOARD.md)
- [Browser extension](./EXTENSION.md)
- [Configuration](#configuration)
- [Terminal multiplexer integration](#terminal-multiplexer-integration)
- [Card templates](#card-templates)
- [Card frontmatter](#card-frontmatter)
- [Frontmatter presets](#frontmatter-presets)
- [Extensibility](#extensibility)
  - [Custom shell commands](#custom-shell-commands)
  - [Lua scripting](#lua-scripting)
  - [MCP server](#mcp-server)
  - [Theming](#theming)
- [Web server (headless)](#web-server-headless)
- [Limitations & known gaps](#limitations--known-gaps)
- [Project layout](#project-layout)
- [Development](#development)

---

## Features

A quick, scannable rundown of everything kbrd does:

**Files & storage**

- **Plain-files storage** — directories are columns, `.md` files are cards, zero database.
- **Live reload** — the board updates instantly when files change on disk (`fsnotify`).
- **Release updates** — on board launch, kbrd checks GitHub's latest stable release in the background and notifies you when a newer version is available.

**Cards**

- **Create cards** — in the current column (`n`) or the first column (`N`).
- **Card templates** — create pre-structured cards from per-column or board-wide templates through the create menu (`n`); see [TEMPLATES.md](./TEMPLATES.md).
- **Card frontmatter** — YAML metadata on a card sets its accent color, icon, meta line, and filterable `#tags`; custom keys flow into Lua commands as `ctx.data`.
- **Frontmatter presets** — apply reusable metadata changes to selected or marked cards (`P`) from board-local configuration.
- **Peek** — rendered Markdown preview in a scrollable viewport (`space`).
- **Card timeline** — semantic Git history for the selected card (`u`): browse
  edits, metadata changes, moves, renames, snapshots, and diffs without working
  with commit hashes; old revisions can only be restored as a new copy.
- **Edit inline** — a modal **vim-like** editor (Normal/Insert/Visual, motions & operators, `:` command-line, surround, markdown list/checkbox helpers, system clipboard, `:lua` eval); press `:help` for the cheatsheet. See [EDITOR.md](./EDITOR.md). Or open in `$EDITOR` (`o`).
- **Append / prepend** — add content to existing cards (`a` / `p`).
- **Journal entries** — append timestamped notes to a card (`b`), with optional natural-date prefixes like `yesterday` or `next monday`.
- **Clipboard ring** — copy card content into a machine-local, searchable history
  of up to 100 typed entries (`c`), browse it with `C`, pin or delete snippets,
  and paste a selected entry using the existing append/prepend/journal/new-card
  choices. See [Clipboard ring](./CLIPBOARD.md).
- **System sharing** — open the native macOS share sheet for the selected Markdown
  card (`y`), with AirDrop, Mail, Messages, and installed sharing extensions.
- **Scratchpad** — keep an autosaved, board-scoped quick note outside the board
  repository (`q`), append the selected card to it (`Q`), exchange text with the
  system clipboard or clipboard ring, and promote a selection or the whole note
  into a card.
- **Pin cards** — float important cards to the top of a column (`!`).
- **Move cards** — choose any destination with fuzzy search (`m`), or move quickly to the next column (`M`).
- **Rename & delete** — cards (`r` / `d`) and columns (`R`), with confirmation on delete.

**Search & navigation**

- **Fully keyboard-driven** — every action has a binding; mouse optional.
- **Global search** — full-text search across all recent boards via `ripgrep`, merged with the active layer's virtual items (`f`).
- **Board switcher** — fuzzy switch, pin favorites, and remove boards (`Ctrl+P`).
- **Harpoon slots** — keep five board-scoped file jumps at hand (`h`).
- **Help overlay** — discover every shortcut without leaving the app (`?`).

**Git & sync**

- **Git panel** — diff, commit, log, sync (pull+push), and add remotes in-app (`g`).
- **Auto-sync** — reconciles with the remote when the board opens (and optionally
  on an interval), with automatic upstream setup; it resolves divergence on its
  own, setting aside a conflict copy only when two machines edit the same lines
  (never blocking the sync). A header cell shows the sync state; pending conflict
  copies can be reviewed from the Git panel without showing them as ordinary cards.
- **README generation** — optionally regenerate `README.md` from the board before commits.

**Extensibility**

- **Custom shell commands** — run templated shell commands against any card (`x`).
- **Lua scripting** — extend kbrd with commands, event hooks, timers, and async tasks.
- **Virtual columns** — Lua-driven columns showing a computed view (e.g. open tasks across boards); `tab` switches into them, with script-declared item actions ([example](./examples/tasks/tasks.lua)).
- **Built-in MCP server** — let external tools and LLM agents operate on your boards.

**Interface & integrations**

- **Bundled Chrome extension** — convert the main article or a right-clicked text selection to editable Markdown and capture it directly into a board through Native Messaging; it ships inside the kbrd binary for unpacked installation and requires no Web Store, running TUI, or MCP server. See [EXTENSION.md](./EXTENSION.md).
- **Themes** — terminal-aware light / dark palettes, with optional override.
- **In-app config menu** — open or scaffold config & command files (`,`).
- **Terminal multiplexer integration** — inside [Zellij](https://zellij.dev) or [tmux](https://github.com/tmux/tmux), open a card in an editor pane/window or a shell scoped to the board (`z`); the current tab/window is named after the board.

---

## Screenshots

<table>
  <tr>
    <td width="50%"><img src="docs/screenshots/peek.png" alt="Markdown peek" width="100%"></td>
    <td width="50%"><img src="docs/screenshots/git-panel.png" alt="Git panel with diff" width="100%"></td>
  </tr>
  <tr>
    <td align="center"><em>Peek a card's rendered Markdown (<code>space</code>)</em></td>
    <td align="center"><em>Diff, commit, and sync in the git panel (<code>g</code>)</em></td>
  </tr>
  <tr>
    <td width="50%"><img src="docs/screenshots/search.png" alt="Global search across boards" width="100%"></td>
    <td width="50%"><img src="docs/screenshots/switcher.png" alt="Board switcher" width="100%"></td>
  </tr>
  <tr>
    <td align="center"><em>Fuzzy global search across boards (<code>f</code>)</em></td>
    <td align="center"><em>Switch, pin, and remove boards (<code>Ctrl+P</code>)</em></td>
  </tr>
  <tr>
    <td width="50%"><img src="docs/screenshots/custom-commands.png" alt="Custom commands menu" width="100%"></td>
    <td width="50%"><img src="docs/screenshots/help.png" alt="Keyboard shortcut help overlay" width="100%"></td>
  </tr>
  <tr>
    <td align="center"><em>Run templated shell commands on a card (<code>x</code>)</em></td>
    <td align="center"><em>Discover every shortcut with the help overlay (<code>?</code>)</em></td>
  </tr>
  <tr>
    <td width="50%"><img src="docs/screenshots/template.png" alt="Multi-step card template form" width="100%"></td>
    <td width="50%"><img src="docs/screenshots/virtual-column.png" alt="Lua-driven virtual column" width="100%"></td>
  </tr>
  <tr>
    <td align="center"><em>Create cards from an empty file or template (<code>n</code>)</em></td>
    <td align="center"><em>Lua-driven virtual columns, e.g. open tasks (<code>tab</code>)</em></td>
  </tr>
  <tr>
    <td width="50%"><img src="docs/screenshots/zoom.png" alt="Zoomed single column" width="100%"></td>
    <td width="50%"><img src="docs/screenshots/frontmatter.png" alt="In-app frontmatter editor" width="100%"></td>
  </tr>
  <tr>
    <td align="center"><em>Zoom a single column for focused reading (<code>+</code>)</em></td>
    <td align="center"><em>Edit a card's frontmatter in-app (<code>~</code>)</em></td>
  </tr>
  <tr>
    <td width="50%"><img src="docs/screenshots/filter.png" alt="Filtering a column by tag" width="100%"></td>
    <td width="50%"></td>
  </tr>
  <tr>
    <td align="center"><em>Filter a column by text or <code>#tag</code> (<code>/</code>)</em></td>
    <td></td>
  </tr>
</table>

---

## Installation

### Homebrew (macOS)

```bash
brew install up2jj/tap/kbrd
```

This installs a prebuilt binary onto your `PATH`. Upgrade later with `brew upgrade kbrd`.

### From source

Requires Go **1.26+**:

```bash
git clone https://github.com/up2jj/kbrd.git
cd kbrd
go build -o kbrd ./
```

Move the resulting `kbrd` binary somewhere on your `PATH`. See [Development](#development) for the test/build workflow.

**Runtime dependencies**
- `git` — for the git panel and sync features.
- Optional: [`ripgrep`](https://github.com/BurntSushi/ripgrep) (`rg`) — required for filesystem content in global search; active-layer virtual metadata remains searchable without it.
- Optional: [`difft`](https://github.com/Wilfred/difftastic) or `diff-so-fancy` — nicer diffs (falls back to `git`).
- Optional: [`zellij`](https://zellij.dev) or [`tmux`](https://github.com/tmux/tmux) — enables the `z` terminal actions menu when kbrd runs inside that multiplexer.
- Optional: [`direnv`](https://direnv.net) — loads an approved board `.envrc` when opening or switching boards.

---

## Getting started

Run kbrd from any directory you want to use as a board:

```bash
./kbrd
```

On first run kbrd sets up default columns. Press `?` at any time for the full shortcut
overlay. To scaffold configuration files:

```bash
./kbrd init            # write a kbrd.toml template into the current directory
./kbrd init --global   # write the global config template to ~/.config/kbrd/
```

When `direnv` is installed, kbrd applies the environment for the current board
before loading its configuration. Switching boards unloads variables from the
previous board and loads the new board's approved `.envrc`; run
`direnv allow <board>` explicitly before opening a trusted file. kbrd never
approves `.envrc` files itself, and `--safe` skips direnv entirely. Changes to an
`.envrc` take effect the next time the board is opened or switched to. A green
`◆ direnv` header cell is shown while a direnv environment is active.

To ingest a card from a script without opening the TUI, pipe its Markdown to
`kbrd ingest`. Every ingested card receives a UTC `created_at` YAML frontmatter
value. Its format defaults to RFC 3339 and is configurable through
`[ingest].created_at_format`. The board can be a known recent-board name or an
existing path; omit `--column` for the first real column, or use its 1-based
position. After creation it also runs matching declarative `item_created`
hooks; use `--safe` to skip all board-supplied hooks.

```bash
git log -1 --format=%B | kbrd ingest --board Work --column 2 --name "Latest commit"
kbrd ingest --board ~/boards/work --name "Daily note" --file note.md
```

**Commands**

| Command | Description |
| --- | --- |
| `kbrd` | Open the current directory as a board (default). |
| `kbrd init [--global]` | Write a config template — local `kbrd.toml` by default, or the global template under `~/.config/kbrd/` with `--global`. |
| `kbrd clone <repo-url> [dir]` | Clone a board repository and open it. `dir` defaults to the repo name; pass `--no-open` to clone without launching the TUI. |
| `kbrd ingest --board <name-or-path> --name <name>` | Create a card from stdin, `--content`, or `--file`; sets `created_at` to the current UTC timestamp using `[ingest].created_at_format` (RFC 3339 by default). `--column` accepts a column name or 1-based position and defaults to the first column. |
| `kbrd reminders sync [--dry-run]` | Synchronize due-bearing cards with the board's configured Apple Reminders list (macOS only). Use `--create-list` to create a missing list and `--import-existing` to adopt unmarked reminders on the first sync. |
| `kbrd extension install [--dir]` | Install or update the bundled unpacked browser extension and its Native Messaging host; see [EXTENSION.md](./EXTENSION.md). |
| `kbrd serve eject [--dir]` | Write the default web templates and static assets into `.kbrd_web_templates/` for customizing (see [Web server](#web-server-headless)). |

**Flags**

| Flag | Description |
| --- | --- |
| `--mcp` | Start the built-in MCP server for this run (off by default). |
| `--mcp-addr <addr>` | Override the MCP listen address (default `127.0.0.1:7777`). |
| `--safe` | Disable all board-supplied code — direnv, Lua scripting, event hooks, and template `{{shell}}` exec — overriding config. Use when opening a board you don't fully trust. |

---

## Keyboard shortcuts

All bindings below are the defaults from the in-app help (`?`).

### Board view

**Navigation**

| Keys | Action |
| --- | --- |
| `→` / `tab` / `]` | Next column; with marked cards, move them right |
| `←` / `shift+tab` / `[` | Previous column; with marked cards, move them left |
| `j` / `k` | Move within a column |
| `H` / `L` | Pan columns left / right |
| `/` | Filter cards in the current column |

**Item**

| Keys | Action |
| --- | --- |
| `space` | Peek (rendered Markdown) |
| `e` | Edit |
| `a` / `p` | Append / prepend content |
| `b` | Journal entry |
| `c` / `v` | Copy / paste |
| `y` | Share through the native macOS share sheet |
| `o` | Open in `$EDITOR` |
| `!` | Pin / unpin |
| `m` | Choose a destination column and move the selected or marked cards |
| `M` | Move to the next column |
| `r` | Rename item |
| `d` | Delete |
| `x` | Custom commands menu |
| `~` | Edit a frontmatter key/value (`ctrl+e` completes a key, `ctrl+d` removes it) |
| `P` | Apply a board-local frontmatter preset to the selected or marked cards |
| `u` | Open the selected card's timeline |

**Create & command**

| Keys | Action |
| --- | --- |
| `n` | Create item |
| `N` | New item in first column |
| `:` | Jump to card mnemonic |

**Column**

| Keys | Action |
| --- | --- |
| `R` | Rename column |

**Global**

| Keys | Action |
| --- | --- |
| `F5` | Refresh |
| `h` | Open five Harpoon file slots |
| `Ctrl+P` | Switch board |
| `l` | Switch `.kbrd.lua` runtime layer (when declared) |
| `f` | Search across boards |
| `g` | Git panel |
| `C` | Browse clipboard history |
| `q` | Open the current board's machine-local scratchpad |
| `Q` | Append the selected card to the scratchpad and open it |
| `z` | Terminal actions menu (inside Zellij or tmux) |
| `,` | Config menu |
| `?` | Toggle help |
| `Ctrl+C` | Quit |

### Harpoon slots (`h`)

Harpoon keeps five machine-local slots for the current board, so they survive restarts
without adding state to the board repository. Open the list with `h`, use `↑` / `↓`
to choose a slot, then press `a` to assign the currently selected file. Press `1`–`5`
or `enter` to jump to a slot, and `d` to clear it. Jumping selects the file and opens
its column when it was collapsed.

### Scratchpad (`q`)

Press `q` to open an autosaved quick note for the current board. Scratchpads live
in the machine-local kbrd config directory, outside the board checkout, so edits
never appear in Git. Press `Q` on a card to append its contents and open the note.

Inside the scratchpad, `ctrl+v` pastes the system clipboard, `C` in Vim Normal or
Visual mode (`ctrl+g` everywhere) opens clipboard history, and `ctrl+c` copies the
visual selection or whole note into both clipboard surfaces. Press `ctrl+n` to
promote the visual selection—or the entire note when there is no selection—into
a new card in the focused column. Promoted text is removed only after the card is
created successfully; cancelling or failing creation leaves the note unchanged.

### macOS card sharing (`y`)

Select a card and press `y` to open the native macOS share sheet. kbrd extracts
an embedded universal AppKit helper into the user cache on first use, signs it
locally, and passes the Markdown card as a file. There is no Shortcut to install,
and the Go application remains cgo-free. The helper supports both Apple silicon
and Intel Macs.

### Inline editor (vim-like)

The card editor is a **modal, vim-like** editor: Normal / Insert / Visual modes,
motions and operators, `:` command-line, markdown list/checkbox helpers, system
clipboard, and `:lua` evaluation. Press **`:help`** inside the editor for the full
cheatsheet, or see **[EDITOR.md](./EDITOR.md)**. Set `[editor] vim = false` to fall
back to a plain textarea editor.

| Keys | Action |
| --- | --- |
| `i` `a` `o` / `v` `V` / `:` | Insert / Visual / Command-line |
| `:w` `:q` `:q!` `:wq` · `ctrl+s` | Save / quit (`ctrl+s` saves and stays) |
| `esc` | Back to Normal · close from Normal |
| `/regex` `?regex` `n` `N` | Search forward / back · next / previous |
| `:lua <expr>` | Evaluate Lua against the line/selection (see [SCRIPTING.md](./SCRIPTING.md)) |
| `ctrl+t` | Insert `- [ ] ` or toggle a task; in Visual/Visual Line mode, prefix selected lines |
| `ctrl+l` | Run a line command · `ctrl+e` expand · `ctrl+v` paste |

> The non-vim fallback (`[editor] vim = false`) uses `ctrl+s`/`enter` to save,
> `ctrl+z`/`ctrl+y` to undo/redo, `ctrl+t` to insert/toggle a task, `ctrl+e` to expand,
> and `esc` to cancel.

### Journal entries (`b`)

Press `b` on a card to open the editor for a one-shot journal entry. Saving appends a
line like `2026-06-24 19:09:00 - called client` to the card.

By default, `[journal] detect_date = true` treats a leading natural-language date as
the entry timestamp and drops that date phrase from the saved text:

| Entry text | Saved timestamp | Saved body |
| --- | --- | --- |
| `yesterday fixed login` | yesterday | `fixed login` |
| `next monday call client` | next Monday | `call client` |
| `2 weeks ago reviewed PR` | two weeks ago | `reviewed PR` |
| `wczoraj naprawilem blad` | yesterday | `naprawilem blad` |
| `za 2 tygodnie sprawdzic raport` | two weeks from now | `sprawdzic raport` |

Only a leading phrase is parsed, and the longest valid leading date wins. If the
phrase is not recognized, or the entry would become empty (`tomorrow` alone), kbrd
keeps the full text and stamps it with the current time. Date-only phrases inherit
the current time of day; include a time (`next monday at 19:09`, `wczoraj o 8:30`) to
override it.

### Peek

| Keys | Action |
| --- | --- |
| `j` / `k` (`↓` / `↑`) | Scroll down / up |
| `enter` / `space` / `pgdn` | Page down |
| `g` / `home`, `G` / `end` | Top / bottom |
| `q` / `esc` | Close |

### Create menu and template form (`n`)

| Keys | Action |
| --- | --- |
| `↑` / `↓`, `enter` | Pick an empty card or template |
| `/` | Fuzzy-search create options |
| `tab` / `enter` | Next field / step |
| `shift+tab` | Previous field / step |
| `esc` `esc` | Cancel (first `esc` arms, second confirms) |

### Board switcher (`Ctrl+P`)

| Keys | Action |
| --- | --- |
| `↑` / `↓` | Previous / next board |
| `enter` | Switch |
| `tab` | Pin / unpin |
| `esc` / `ctrl+p` | Cancel |

### Global search (`f`)

Search merges filesystem content from all recent boards with titles, previews,
and metadata from the active board's virtual columns. Results backed by the
same file are shown once. Column visibility does not limit global search;
opening a hidden hit selects its virtual representation when possible or
reveals its filesystem column.

| Keys | Action |
| --- | --- |
| `↑` / `↓` | Previous / next result |
| `enter` | Open result |
| `esc` | Cancel |

### Git panel (`g`)

| Keys | Action |
| --- | --- |
| `c` | Save changes, then sync when a remote is connected |
| `s` | Pull remote changes without publishing local commits |
| `l` | View log on narrow terminals (wide terminals show a recent-commits rail) |
| `a` | Connect a remote (then sync) |
| `d` | Return from history to changes |
| `r` | Review pending synchronization changes |
| `tab` | Switch between the file list and current diff |
| `↑` / `↓` or `j` / `k` | Select a file or scroll the focused diff |
| `pgup` / `pgdn` or mouse wheel | Scroll the current diff or history |
| `q` / `esc` | Close |

### Card timeline (`u`)

The timeline follows the selected card across Git renames and column moves. It
shows card-level events instead of a repository log and never overwrites the
current card when recovering an old revision.

| Keys | Action |
| --- | --- |
| `↑` / `↓` | Select an event |
| `enter` / `s` | View that revision as a card snapshot |
| `d` | View its unified diff |
| `c` | Restore the snapshot as a new, uniquely named card copy |
| `←` / `→` | Browse older / newer snapshots |
| `q` / `esc` | Go back or close |

### Review changes (Git panel `r`)

Sync conflict copies are kept as plain files but hidden from the normal board
columns until reviewed. Open the Git panel with `g`, then press `r` when pending
conflict copies are present. The review screen keeps the workflow local and explicit:

| Keys | Action |
| --- | --- |
| `d` | View a unified diff |
| `e` | Edit the incoming version |
| `k` | Keep the original and discard the incoming copy |
| `r` | Replace the original with the incoming version |
| `b` | Keep both, choosing a clean card name |
| `s` | Skip to the next conflict |
| `q` / `esc` | Close |

Resolution changes files in the working tree; kbrd does not silently commit
them. Use the Git panel to commit or sync the result. `git.auto_commit = true`
can publish the resolution during a later automatic sync.

### Config menu (`,`)

| Keys | Action |
| --- | --- |
| `c` | Open or create local `kbrd.toml` |
| `C` | Open or create global `~/.config/kbrd/config.toml` |
| `x` | Open or create local `.kbrd_commands.yml` |
| `m` | Create local `.mcp.json` |
| `a` | Create local `AGENTS.md` |

### Terminal actions (`z`)

Available inside [Zellij](https://zellij.dev) or [tmux](https://github.com/tmux/tmux).

| Keys | Action |
| --- | --- |
| `f` | Open the card in a floating pane (Zellij) or new window (tmux) |
| `e` | Open the card in a new tiled pane |
| `s` | Open a shell in the board directory |
| `q` / `esc` | Close |

---

## Configuration

kbrd reads two TOML files, with the folder-local one overriding the global one:

- **Global:** `~/.config/kbrd/config.toml`
- **Folder-local:** `<board>/kbrd.toml`

Generate templates with `kbrd init` / `kbrd init --global`, or from the config menu (`,`).

```toml
[display]
column_width  = 32          # width of each column
preview_lines = 3           # lines shown in a card preview
title_from_heading = false  # use the first "# " heading as the card title
theme         = "auto"      # auto follows terminal background; light | dark force a palette

[notify]
backend = "auto"            # auto | osc99 / kitty | osc777 | osc9 | osascript | none

[board]
name = ""                    # optional label shown in the board switcher
item_double_click = "peek"   # peek | edit

# Repeat this block to declare multiple board-local presets.
[[frontmatter_presets]]
id = "start-work"
name = "Start work"
description = "Mark the card as actively being worked on"
columns = [2, "2. IN PROGRESS"] # names or 1-based positions; omit for all columns
unset = ["blocked_by"]
set.status = "doing"
set.started_at = "{{now}}"
set.started_by = "{{user}}"

[git]
diff_tool          = "auto"     # auto | difft | diff-so-fancy | git
auto_sync_interval = ""         # empty / "0" disables; e.g. "30s", "5m", "1h"
generate_readme    = false      # regenerate README.md from the board before each commit
manual_sync_mode   = "attended" # Save & sync: attended (ff-only, fail loud) | auto (merge + conflict copy)
sync_on_startup    = true       # reconcile with the remote when the board opens (no-op without a remote)
auto_commit        = false      # TUI: commit pending edits before auto-sync (pauses while editor is open)

[scripting]
enabled            = true     # master switch for the Lua VM
command_timeout_ms = 2000     # timeout for command callbacks
hook_timeout_ms    = 500      # timeout for event hooks and timers
instruction_limit  = 10000000 # CPU backstop per script run
error_threshold    = 3        # auto-disable a failing hook/timer after N errors (0 = never)
http_timeout_ms    = 10000    # maximum timeout for outbound kbrd.http requests
http_max_response_bytes = 2097152 # maximum response body buffered for Lua (2 MiB)

[journal]
detect_date = true          # parse a leading natural-language date in journal entries

[ingest]
created_at_format = "2006-01-02T15:04:05Z07:00" # Go time layout; e.g. "2006-01-02" for date-only

[reminders]
enabled        = false       # macOS only; sync manually from the CLI or the `?` menu
account        = "iCloud"    # optional exact account name
list           = "kbrd · Work"
inbox_column   = "Inbox"     # imported and reopened reminders
done_columns   = ["Done"]    # first entry receives completed reminders
delete_remote_on_card_delete = false # opt in to two-step remote deletion

[mcp]
enabled          = false      # built-in MCP server; off by default (start with --mcp or enabled = true)
addr             = "127.0.0.1:7777"  # Streamable HTTP listen address; loopback only without auth
allow_commands   = false      # allow run_custom_command shell execution (disabled by --safe)
allow_card_reads = false      # expose complete card Markdown through MCP tools and resources
```

### Apple Reminders sync

Apple Reminders sync is available on macOS and does not require a separate helper.

1. In Apple Reminders, create a list named `kbrd`. Make sure the board also
   contains the columns that will receive incomplete and completed reminders.
   In this example they are `Inbox` and `Done`.

2. Add the integration to the board's `kbrd.toml`:

   ```toml
   [reminders]
   enabled = true
   list = "kbrd"
   inbox_column = "Inbox"
   done_columns = ["Done"]
   ```

   `list` is the exact Apple Reminders list name. Add `account = "iCloud"` only
   when you need to select a specific account.

3. Create a card with a `due` frontmatter key, for example
   `Inbox/Take medicine.md`:

   ```markdown
   ---
   due: "tomorrow at 7am"
   ---

   Take one tablet with breakfast.
   ```

   The filename becomes the reminder title and the Markdown body becomes its
   note. Use `due: 2026-07-15` for an all-day reminder or
   `due: "2026-07-15 07:00"` for a timed reminder.

4. From the board directory, preview the first synchronization:

   ```sh
   kbrd reminders sync --dry-run
   ```

5. Apply it:

   ```sh
   kbrd reminders sync
   ```

   macOS may ask for permission for kbrd or your terminal to control Reminders.
   If you did not create the Apple list manually, use
   `kbrd reminders sync --create-list` instead. That command creates the list and
   immediately performs the first sync, so it cannot be combined with
   `--dry-run`.

6. For later synchronizations, use the same CLI command or open the TUI, press
   `?`, search for **reminders**, and press Enter. Progress is shown while the
   synchronization runs.

On the first successful sync, kbrd replaces relative dates with fixed values and
adds `kbrd_reminder_id` automatically. Do not add or edit this key yourself:

```markdown
---
due: "2026-07-15T05:00:00Z"
kbrd_reminder_id: "generated-sync-id"
---

Take one tablet with breakfast.
```

The ID links the card to the same reminder on every later sync, even if its title,
due date, or column changes. It also prevents overdue cards from creating
duplicates. Moving a linked card into `Done` completes the reminder; completing
the reminder moves the card to the first configured done column.

To import unmarked reminders during the first sync, add `--import-existing`.
Remote deletion is intentionally disabled by default. To remove a linked Apple
reminder after its card is deleted, set
`delete_remote_on_card_delete = true`; deletion requires two consecutive syncs
with the card missing.

---

## Terminal multiplexer integration

When kbrd detects [Zellij](https://zellij.dev) (`ZELLIJ`) or
[tmux](https://github.com/tmux/tmux) (`TMUX`), it names the current tab/window after the
board and adds a **`z` terminal menu** so you can jump into a card without leaving the board.
Zellij takes precedence if both environment variables are present.

| Key | Action |
| --- | --- |
| `f` | Open the card in a **floating pane** (Zellij) or **new window** (tmux) |
| `e` | Open the card in a new **tiled** pane |
| `s` | Open a **shell** in the board directory |

Editor panes use `$VISUAL` → `$EDITOR` → `vi`. Reopening a card you already have open
**focuses the existing pane** instead of spawning a duplicate. The `z` binding and its
help entry appear only inside a supported multiplexer; everywhere else it does nothing.
Outside a multiplexer, use `o` to open a card with the system's external opener.

### Extending zellij usage with custom commands

The built-in menu covers the common cases; for anything else, kbrd's
[custom shell commands](#custom-shell-commands) already give you the whole zellij CLI.
Because commands run inside the session with the board
[template variables](#custom-shell-commands), you can script any pane / tab / layout
behavior and run it from the `x` menu — no kbrd config required:

```yaml
commands:
  - name: Edit in big floating pane
    id: zellij-edit-big
    command: zellij edit -f --width 80% --height 80% --cwd "{{.boardPath}}" "{{.filePath}}"

  - name: Lazygit in the card's column
    id: zellij-lazygit
    command: zellij run --cwd "{{.columnPath}}" -- lazygit

  - name: Tail the card's log
    id: zellij-tail-log
    command: zellij run --name logs -- tail -f "{{.fileDir}}/run.log"
```

---

## Card templates

Press `n` in a column to open the create menu: start an empty Markdown card, or
choose from column-level and board-level templates. A template is a Markdown file
whose YAML frontmatter declares a multi-step form (text inputs, selects,
multi-selects, confirms — rendered with
[huh](https://github.com/charmbracelet/huh)); the body is a Go `text/template` that
receives your answers plus the standard board variables.

```markdown
---
name: Task
filename: "{{slug .title}}"
steps:
  - title: Task
    fields:
      - {key: title, type: input, title: Title, required: true}
      - {key: priority, type: select, title: Priority, options: [low, normal, high], default: normal}
---
# {{.title}}

- Priority: {{.priority}}
```

Templates live in `<board>/.kbrd_templates/` (every column) and
`<board>/<column>/.kbrd_templates/` (column-only; shadows board templates with the same
name). Created cards fire the normal `item_created` event, so hooks apply.

Template bodies and filenames can resolve natural-language dates with
`{{date "next friday"}}` / `{{date "za 2 tygodnie"}}` — English and Polish, with an
optional Go layout. The same `date` function is available in custom commands, hooks,
and Lua (`kbrd.date.parse`).

See **[TEMPLATES.md](./TEMPLATES.md)** for the full format reference and
[`examples/templates/`](./examples/templates/) for worked examples.

---

## Card frontmatter

A card may open with a YAML frontmatter block; kbrd parses it for display
metadata and keeps it out of the card preview:

```markdown
---
accent: "#e06c75"       # color for the card title
icon: "🔥"              # glyph shown before the title
meta: due tomorrow      # replaces the modified/size/git meta line
tags: [urgent, backend] # rendered as #tag chips; matched by the column filter
assignee: kuba          # custom keys are allowed
---
# Card title
Body text…
```

All keys are optional and parsing is always on. `tags` accepts a list or a
single string, and tag names participate in the column filter (`/`). The full
frontmatter map — including custom keys like `assignee` — is exposed to Lua
custom commands as `ctx.data`, so scripts can act on it. Malformed YAML never
breaks a card: it loads without metadata and shows a `⚠ yaml` badge on its
meta line so the mistake is visible.

Press `~` on a card to edit a single key without leaving the board: type the
key (`ctrl+e` completes one already used anywhere on the board), then edit its
value — pre-filled with the card's current value when the key exists, with
`ctrl+e` completing common scalars like `true`/`false`/`yes`/`no` — or press
`ctrl+d` to remove the key. A `frontmatter_suggestions` Lua hook can add its own
keys and default values to the completion list (see [SCRIPTING.md](./SCRIPTING.md)).

---

## Frontmatter presets

Presets are reusable frontmatter patches defined in the board’s local
`kbrd.toml`, so the metadata vocabulary travels with the board. They are not
loaded from the global configuration. Select or mark one or more cards, press
`P`, choose a preset from the fuzzy-searchable menu, and press Enter to apply it.

Each `[[frontmatter_presets]]` table needs a unique `id` and a display `name`.
Use `description` for searchable help text, `set.<key>` entries to write
frontmatter, and `unset` to remove keys. Omit `columns` to make a preset
available in every filesystem column; otherwise list column names, 1-based
filesystem-column positions, or both.

For example:

```toml
[[frontmatter_presets]]
id = "start-work"
name = "Start work"
description = "Mark cards as actively being worked on"
columns = ["In Progress"]
unset = ["blocked_by"]
set.status = "doing"
set.started_at = "{{now}}"
set.started_by = "{{user}}"
set.tags = ["active", "{{column}}"]
```

Preset values can use `{{now}}`, `{{today}}`, `{{board}}`, `{{column}}`,
`{{filename}}`, and `{{user}}`; these are expanded separately for each card.
`now` produces an RFC3339 timestamp and `today` produces a date. Both support
one offset, such as `{{now+2h}}`, `{{today-3d}}`, or `{{today+1mo}}`; supported
units are `m`/`min`, `h`, `d`, `w`, and `mo`. Values may be scalars or flat
arrays, and a key cannot be present in both `set` and `unset`.

---

## Extensibility

kbrd is extensible at three levels: simple **shell commands**, full **Lua scripting**, and
an **MCP server** for external tools.

> **⚠️ Security:** A board is just a folder, and opening one **runs any folder-local
> `.kbrd.lua` and `.kbrd_commands.yml` it contains** — automatically, with no prompt.
> Cloning or syncing a board authored by someone else executes their code with your
> privileges. Only open boards you trust, or disable scripting for untrusted ones. See
> **[SECURITY.md](./SECURITY.md)** for the trust model and mitigations.

### Custom shell commands

Define shell commands that run against the selected card, column, or board. Press `x` on a
card to open the menu and fuzzy-search by name ([example](./examples/commands/commands.yml)).

- **Global:** `~/.config/kbrd/commands.yml`
- **Folder-local:** `<board>/.kbrd_commands.yml` (overrides global commands with the same `id`)

```yaml
commands:
  - name: Edit in vim
    id: edit-vim
    description: Open file in vim
    command: vim "{{.filePath}}"

  - name: Reveal in Finder
    id: reveal-finder
    description: Open the column folder in Finder
    command: open "{{.columnPath}}"
```

**Template variables**

| Variable | Meaning |
| --- | --- |
| `{{.filePath}}` | Absolute path to the selected file |
| `{{.fileName}}` | Base name without `.md` |
| `{{.fileDir}}` | Directory containing the file |
| `{{.boardPath}}` | Board root path |
| `{{.boardName}}` | Board name from config |
| `{{.columnPath}}` | Column folder path |
| `{{.columnName}}` | Column folder name |
| `{{env "VAR"}}` | Value of environment variable `VAR` (empty string if unset) |

Quote variables to handle paths with spaces. Reload by re-opening the board.

The rendered command also runs in a shell, so plain `$VAR` works too — use `{{env "VAR"}}` only
when you need kbrd to substitute the value *before* the shell sees it (e.g. to build the command
itself rather than have the shell expand it).

### Lua scripting

kbrd embeds a Lua 5.1 VM ([gopher-lua](https://github.com/yuin/gopher-lua)). Scripts are
loaded at startup from:

- **Global:** `~/.config/kbrd/init.lua`
- **Folder-local:** `<board>/.kbrd.lua`

The API surface includes:

- `kbrd.layer{...}` — declare exclusive folder-local runtime layers of commands, timers, async work, and virtual columns; switch them with `l` and see the active layer in the header.
- `kbrd.command(...)` — register a custom command (appears in the `x` menu; shadows a shell command with the same id).
- `kbrd.on(event, fn)` — hook lifecycle events (`board_load`, `board_refresh`, `item_select`, `column_change`, `item_open`, `item_saved`, `item_changed`, `item_created`, `item_renamed`, `item_deleted`, `item_moved`, `git_sync_done`), plus the `column_items` transform hook to sort/filter/group a column's cards (e.g. by a `priority` frontmatter key), and the serve-only `http_request`/`http_response` middleware hooks to gate, redirect, or rewrite web requests.
- `kbrd.board.move / create / rename / delete / refresh / createColumn` — board operations.
- `kbrd.board.templates / createFromTemplate` — list card templates and create cards from them ([TEMPLATES.md](./TEMPLATES.md)).
- `kbrd.ui.pick / prompt / confirm` — interactive dialogs (commands only, not hooks/timers).
- `kbrd.fs.read / write / exists / mkdir / glob` — filesystem helpers (paths resolve against the board root).
- `kbrd.async.run / cancel` — run shell commands on a worker goroutine.
- `kbrd.http.request` — make bounded asynchronous HTTP(S) requests with optional JSON encoding/decoding.
- `kbrd.json` / `require("json")` — encode and decode JSON with explicit null, array, and object values.
- `kbrd.timer.every / after / cancel` — schedule recurring or one-shot callbacks.
- `kbrd.notify(msg, level)` / `kbrd.status(...)` — toasts and status line.
- `kbrd.debug(...)` / `kbrd.inspect(value)` — source-aware, table-friendly debug output; `print(...)` is captured too.

Scripting can be disabled and tuned via the `[scripting]` config section. See
**[SCRIPTING.md](./SCRIPTING.md)** for the full API reference, examples, and safety model.

Before the board UI opens, kbrd fully initializes `.kbrd.lua`. A syntax,
top-level, layer-validation, or default-layer setup failure opens a recovery
screen with captured output and source context; edit the file and retry without
restarting kbrd. Logs rotate under `~/.cache/kbrd/script.log`.

### MCP server

kbrd can run a built-in [Model Context Protocol](https://modelcontextprotocol.io) server over
Streamable HTTP, letting external tools and LLM agents operate on your boards headlessly.
It is **off by default** — start it with `--mcp` for a single run, or set `enabled = true` in
the `[mcp]` config section to opt a board in permanently. It listens on `127.0.0.1:7777`
(override with `--mcp-addr` or `[mcp].addr`) and refuses non-loopback addresses unless MCP
authentication support is added. The header strip shows its state: `◆ mcp` (green) when
running, `✕ mcp` (red) when it was requested but couldn't bind — e.g. the port is already in
use — and `◇ mcp` (muted) when off. Its scope covers boards in your recents plus any
folder-local `.mcp.json`.

**Tools exposed**

| Tool | Purpose |
| --- | --- |
| `list_boards` | List known boards; MCP Apps clients render a picker that opens the selected board |
| `list_folders` | List the columns in a board |
| `list_files` | List the cards in a column |
| `show_board` | Return a board snapshot; MCP Apps clients render a Kanban view with clickable card details when card reads are enabled |
| `add_file_to_board` | Create a card in a board/column, with optional content |
| `get_card` | Read raw Markdown, parsed frontmatter, column, and revision; requires `[mcp] allow_card_reads = true` |
| `search_cards` | Search card names, bodies, tags, and frontmatter; requires `[mcp] allow_card_reads = true` |
| `update_card` | Replace complete card content using an optimistic `expected_revision` |
| `move_card` | Move a card to an existing column without overwriting |
| `rename_card` | Rename a card without overwriting |
| `delete_card` | Delete a card only when its `expected_revision` matches |
| `create_column` | Create a durable empty column |
| `list_custom_commands` | List shell commands, optionally filtered by folder/item context |
| `run_custom_command` | Run a shell custom command with full context; requires `[mcp] allow_commands = true` and is disabled by `--safe` |

When the connected client advertises MCP form elicitation, tool calls can recover interactively
from ambiguous board names, unknown named folders, and unknown custom-command ids. The client
presents titled choices to the user and returns the stable selection to the pending tool call. A
missing folder can be explicitly created during card creation or replaced with an existing folder;
read-only tools never offer creation. Clients without form elicitation support keep the existing
descriptive tool errors, so callers can still recover by listing boards, folders, or commands.

**Resources exposed**

| Resource | Purpose |
| --- | --- |
| `kbrd://boards` | JSON index of known boards, availability, and board resource URIs |
| `kbrd://board/{board}` | JSON snapshot of a board's columns and cards |
| `kbrd://card/{board}/{column}/{card}` | Complete card Markdown, including frontmatter; requires `[mcp] allow_card_reads = true` |
| `ui://kbrd/board-v4.html` | Embedded MCP App used by `list_boards` and `show_board` |

Resource names are exact and URI path segments are percent-encoded. Unlike conversational tool
inputs, resource URIs never use fuzzy board matching. Reads reflect the current filesystem state;
stale entries from the recents registry remain visible in `kbrd://boards` with
`"available": false`. Card reads are separately opt-in because the server covers every board in
the recents registry and its loopback HTTP endpoint is not authenticated. MCP completion responses
populate resource-template board, column, and card selectors in clients such as MCP Inspector.

**Prompts exposed**

The server advertises three built-in prompts through MCP `prompts/list`: `board_summary`,
`board_triage`, and `plan_board_work`. Their `board` and optional `column` arguments offer
context-aware completion in compatible clients. Omit `column` to operate on the whole board.
Built-in prompts inspect and plan by default; they tell the agent not to mutate a board without
user approval.

A board can add text prompts in `<board>/.kbrd_mcp_prompts.yml`. Local names are advertised with a
board prefix so the same prompt can exist on several boards: `weekly_review` on a board named
`Work` becomes `work__weekly_review`. Files are loaded when the MCP server starts.

```yaml
prompts:
  - name: weekly_review
    title: Weekly review
    description: Review progress and decide what matters next
    arguments:
      - name: focus
        description: Optional area to emphasize
        required: false
      - name: column
        description: Optional column to review
        required: false
    content: |
      Review the {{.boardName}} board. Pay special attention to {{.focus}}.
      {{if .column}}Limit the review to column {{.column}}.{{end}}
      Inspect the board before proposing changes.
```

`content` creates one user message. For a multi-message prompt, replace it with `messages`, whose
entries have `role` (`user` or `assistant`) and `content`. Message text uses Go template syntax.
Every declared argument is available as `{{.argumentName}}`; optional omitted arguments render as
an empty string. `{{.boardName}}` and `{{.boardPath}}` are always available and cannot be declared
as arguments. A declared `column` argument receives completion from the owning board. Prompt and
argument names may contain ASCII letters, digits, underscores, and hyphens. Malformed files or
entries are ignored with a warning instead of preventing MCP startup.

Create a local `AGENTS.md` (config menu → `a`) to give agents orientation about a board,
and a local `.mcp.json` (config menu → `m`) for per-board MCP configuration. Note that a
folder-local `.mcp.json` can point the MCP server at external processes, so it carries the
same trust caveat as folder-local scripts — see **[SECURITY.md](./SECURITY.md)**.

The server also advertises a working-directory-independent operating guide in its MCP
initialize response. Compatible clients receive the board model, tool workflow, card and
frontmatter conventions, ambiguity handling, and command safety rules even when they connect
globally or over HTTP and never inspect a board-local `AGENTS.md`.

> Note: the MCP server sees **shell** custom commands only — Lua-registered commands are
> not visible to or runnable via MCP.

### Theming

kbrd defaults to `display.theme = "auto"`, asking the terminal whether its background is
light or dark and choosing a matching palette. Set `display.theme = "light"` or
`display.theme = "dark"` to force a palette when a terminal reports the wrong background.

---

## Web server (headless)

`kbrd serve` runs the board as a mobile-first web app — no TUI, no JavaScript build step
(plain HTML + [htmx](https://htmx.org), vendored). View columns and cards; create, edit,
and delete cards from a phone. **Every mutation becomes a git commit and is pushed
immediately**, so the git remote stays the source of truth and the server is disposable.

```bash
# Local: plain HTTP on :8080 (no TLS, no git remote needed — "no sync" banner)
KBRD_TOKEN=$(openssl rand -base64 24) kbrd serve --dir ~/boards/work
```

Dockerized — clones the board repo on first boot, serves it over Let's Encrypt TLS:

```bash
cp .env.example .env   # set KBRD_TOKEN, GIT_TOKEN, KBRD_DOMAIN, and the GIT_URL repo
docker compose up -d
```

Flags / env (flag wins): `--addr`/`KBRD_ADDR` (default `:8080`), `--domain`/`KBRD_DOMAIN`
(enables Let's Encrypt on `:443`+`:80`; needs public reachability under that domain),
`--token`/`KBRD_TOKEN` (required, ≥12 chars), `--git-url`/`GIT_URL` (clone when `--dir` is
missing/empty), `--pull-interval`/`KBRD_PULL_INTERVAL` (default `60s`, `0` disables).
Commit author comes from `GIT_AUTHOR_NAME`/`GIT_AUTHOR_EMAIL`.

Security notes:

- The web token and the git credential are **separate secrets**; use a fine-grained PAT
  scoped to the single board repo (contents read/write) embedded in `GIT_URL`.
- Login is a single shared token behind a rate limiter; sessions are HMAC cookies that
  all invalidate on restart. For LAN/Tailscale-only use, omit `--domain` and terminate
  TLS yourself (e.g. `tailscale serve`).
- Web mode is **safe by default**: folder-local Lua, hooks, and template exec do not
  run, so serving an untrusted board cannot execute its code. The one opt-in is
  `serve --scripting`, which runs the board's `init.lua`/`.kbrd.lua` — its timers and
  the `http_request`/`http_response` request-middleware hooks (custom auth, redirects,
  header/response rewriting). Enable it only for boards you trust.

Conflict stance: each mutation commits locally first, then pushes. When the push is
rejected (the remote moved ahead) the server reconciles with `fetch → merge → push` —
no rebase, no force. Non-overlapping edits to a card auto-merge; a *true* line conflict
keeps your local version as the card and writes the incoming version to a sibling
`<card> (conflict <name|timestamp>).md`, so sync never halts and nothing is lost — the
set-aside edit propagates back to every machine on the next push. All **automatic**
flows (the web daemon and the TUI auto-sync) self-heal this way; the **manual** TUI sync
follows `git.manual_sync_mode` (`attended` fails loud on divergence, `auto` reconciles
like the rest). The TUI auto-sync pauses while the in-app editor is open, and otherwise
only runs on a clean tree (a merge can't run over uncommitted edits); set
`git.auto_commit = true` to have it commit pending edits first on later ticks. Edits
carry a content hash, so a card changed upstream mid-edit is flagged instead of
silently overwritten.

### Customizing the web UI

The HTML templates and static assets (`style.css`, the vendored htmx) are baked into the
binary, but a `.kbrd_web_templates/` folder inside the board **overrides them
file-by-file**, mirroring the embedded layout:

```
<board>/.kbrd_web_templates/
  templates/   *.html — shadow the embedded template of the same name
  static/      style.css, htmx.min.js, or any new file
```

Scaffold the defaults to edit from with:

```bash
kbrd serve eject --dir ~/boards/work   # writes .kbrd_web_templates/ (never clobbers existing files)
```

Overrides **hot-reload on save** — a saved template re-parses live (a syntax error is
logged and the last-good markup keeps serving), and static files are read straight from
disk. The folder is committed and pulled with the board like any other content. Note the
Content-Security-Policy is `default-src 'self'`, so custom styles must be served from
`static/` (or inlined) — external stylesheet/script URLs are blocked.

---

## Limitations & known gaps

- Several scripting helpers are **planned but not yet available**: synchronous shell
  capture (`kbrd.shell.run/exec`), `kbrd.git.*`, `kbrd.log.*`, `kbrd.config.get/all`,
  bundled `require("re")`, a standalone `require("http")` compatibility module, and a
  `lua/?.lua` `require` path. JSON is available as both `kbrd.json` and `require("json")`;
  outbound HTTP is available through `kbrd.http.request`.
- The `git_post_commit` event is reserved but not yet emitted.
- `kbrd.ui.*` dialogs cannot be called from hooks or timers (no coroutine context), and
  new commands/hooks/timers cannot be registered from inside a timer callback or hook.
- The MCP server cannot run Lua-registered commands — only shell custom commands.
- Filesystem content in global search requires `ripgrep`; active-layer virtual
  titles, previews, and metadata do not.

---

## Project layout

| Directory | Role |
| --- | --- |
| `commands/` | CLI command tree (cobra): root/default, `init`, `clone`, `serve` (+ `eject`), and their run logic |
| `board/` | Headless board semantics: discovery, column/item enumeration, name sanitizing |
| `template/` | Card templates: discovery, frontmatter schema, validation, rendering |
| `config/` | TOML config + custom-command loading and templates |
| `events/` | Event bus that feeds the scripting hooks |
| `fs/` | Filesystem and git CLI helpers, plus the file watcher |
| `mcp/` | MCP server: protocol, tools, command bridge, agents template |
| `extension/` | Embedded unpacked Chrome extension and its safe extraction logic |
| `web/` | Headless web frontend (`kbrd serve`): handlers, templates, auth, git sync |
| `model/` | The Bubble Tea TUI — board state, dialogs, git panel, search, switcher, theming, keys |
| `recents/` | Persisted list of recent/pinned boards |
| `script/` | Lua VM host and the `kbrd.*` API bindings |
| `main.go` | Entry point — calls `commands.NewRootCmd().Execute()` |

---

## Development

```bash
go build -o kbrd ./   # build
go test ./...         # run the test suite
```

Common tasks are wrapped in a [`justfile`](justfile) (install with `brew install just`):

```bash
just              # list all recipes
just build        # build ./kbrd
just test         # run the test suite
just hooks        # install git hooks via prek
just snapshot     # build a local release into ./dist (no publish)
just check        # validate the GoReleaser config
just release 0.2.0  # tag v0.2.0 and push, triggering the release workflow
```

Git hooks are managed with [prek](https://prek.j178.dev) (`brew install prek`), configured in
[`prek.toml`](prek.toml). Install them once with `just hooks` — commits then run gofmt, go vet,
a `go mod tidy` check, and file hygiene checks; pushes additionally run the full test suite.
Run all hooks manually with `prek run --all-files`.

Releases are automated with [GoReleaser](https://goreleaser.com) (see [`.goreleaser.yaml`](.goreleaser.yaml)).
Pushing a `v*` tag runs `.github/workflows/release.yml`, which builds the macOS binaries,
publishes a GitHub Release, and updates the Homebrew cask in `up2jj/homebrew-tap`.

The CLI command tree lives in `commands/` (one file per command), the TUI in `model/`,
keybindings are declared in `model/keys.go`, configuration in `config/`, and the Lua API
in `script/` (documented in `SCRIPTING.md`).
