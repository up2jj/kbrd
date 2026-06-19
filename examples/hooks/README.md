# Hooks — declarative event hooks example

A worked [`hooks.yml`](./hooks.yml) that runs a shell command after board
operations — **no Lua required**. It demonstrates the three things people most
often want from hooks:

- **Per-event logging.** Create / move / rename / delete / open each append a
  timestamped line to `<board>/.kbrd_hooks.log`, giving you an audit trail.
- **Serial, ordered execution.** Three `item_created` hooks run in order with a
  deliberate `sleep 1` in the middle, so you can watch the `⚙ hooks` header chip
  and confirm the "AFTER the 1s sleep" line always lands last.
- **Desktop notification.** `Notify on move` fires an `osascript` notification
  when a card changes columns.
- **Natural-language dates.** `Log due date on move` uses `{{date "in 2 weeks"}}`
  to write a resolved date into the log — phrases work in English and Polish
  ([reference](../../TEMPLATES.md#natural-language-dates-date)).

## Install

Copy [`hooks.yml`](./hooks.yml) into either:

- `~/.config/kbrd/hooks.yml` — applies to every board, or
- `<board>/.kbrd_hooks.yml` — just that board (overrides global entries by `id`).

Reload by re-opening the board.

## Watch it work

```sh
tail -f "<board>/.kbrd_hooks.log"
```

Then create a card, move it (`m`/`M`), rename, open, and delete — log lines appear
as each operation completes, and the move pops a notification.

## Notes / caveats

- **After-only.** Hooks observe a completed operation; they cannot cancel it.
- **macOS notification.** `Notify on move` uses `osascript`. On Linux, swap it for
  `notify-send "kbrd hook" "{{.fileName}} — {{.fromColumn}} → {{.toColumn}}"`.
- **YAML literal blocks.** Commands use `|-` so colons, quotes, and `{{...}}`
  braces are taken verbatim. A plain `command: echo a: b` would be misread by YAML
  as a mapping.
- **Action events only.** YAML hooks cover the low-frequency action events
  (`item_created`, `item_moved`, `item_renamed`, `item_deleted`, `item_open`,
  `item_saved`, `item_changed`, `column_created`, `git_sync_done`, `board_load`).
  High-frequency events
  (`item_select`, `column_change`, `board_refresh`) are Lua-only via `kbrd.on(...)`.
  See [SCRIPTING.md](../../SCRIPTING.md#declarative-hooks-no-lua--hooksyml).
