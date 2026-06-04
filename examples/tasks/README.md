# Tasks — virtual column example

A [virtual column](../../SCRIPTING.md#kbrdcolumnsetid-spec--virtual-columns) that
lists every open `- [ ]` checkbox across your board's `.md` files in one place,
with actions to complete or edit a task in its source file.

- **`☐ <task>`** rows, grouped-feeling by source folder (shown on the meta line).
- **`c`** (or Enter) — mark complete (writes `- [x]` to the source file).
- **`e`** — edit the task text.
- **`x`** — the same actions via the command menu.

It stays in sync on its own: it re-scans on `board_load` (open/switch) and on
`board_refresh` (the file watcher, whenever a `.md` file is added or edited), so
newly typed tasks appear without a manual refresh.

## Install

Copy [`tasks.lua`](./tasks.lua) into either:

- `~/.config/kbrd/init.lua` — available on every board, or
- `<board>/.kbrd.lua` — just that board.

Then open the board and `TAB` right onto the **Tasks** column. Requires
[ripgrep](https://github.com/BurntSushi/ripgrep) (`rg`) on your `PATH`.

## Notes / caveats

- **Scope.** `rg` runs with the active board as its working directory, so `.`
  scans the current board. Change the search root to `~/boards` (or wherever) to
  list tasks across boards.
- **Cross-board refresh.** Auto-refresh is driven by the current board's file
  watcher, so edits in *other* boards won't auto-update a cross-board list — only
  current-board changes and your own `complete`/`edit` actions do.
- **Checkbox syntax.** The matcher is lenient (`- []`, `- [ ]`, `- [  ]` all
  count), but `- [ ]` with a space is the standard Markdown form other tools
  render — and completing a task always normalizes it to `- [x]`.
