# Commands — custom shell commands example

A worked [`commands.yml`](./commands.yml) showing shell commands that run against
the selected card, column, or board. Press `x` on a card to open the menu and
fuzzy-search by name.

What it demonstrates:

- **Open in an editor** — `Edit in nano` / `Edit in vim` pass `{{.filePath}}` to a
  terminal editor.
- **Pause-to-read output** — `Word count` runs `wc` then `read -n1` so the result
  stays on screen before the TUI redraws. Reuse this idiom for any command whose
  output you want to see.
- **Reveal in the file manager** — `Reveal in Finder` opens `{{.columnPath}}`.
- **Discover the variables** — `Print info` dumps every template variable, so you
  can see exactly what each one resolves to on a real card.

## Install

Copy [`commands.yml`](./commands.yml) into either:

- `~/.config/kbrd/commands.yml` — available on every board, or
- `<board>/.kbrd_commands.yml` — just that board (overrides global commands by `id`).

Reload by re-opening the board.

## Template variables

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

Quote variables to handle paths with spaces. The rendered command runs in a shell,
so plain `$VAR` works too — use `{{env "VAR"}}` only when you need kbrd to
substitute the value *before* the shell sees it. See
[README.md](../../README.md#custom-shell-commands) for more.
