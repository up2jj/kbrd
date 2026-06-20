# The vim editor

kbrd's card editor is a **modal, vim-like** editor. It opens when you edit (`e`),
append (`a`), prepend (`p`), or journal (`b`) a card. Press **`:help`** inside the
editor at any time for an in-app cheatsheet.

It is on by default; set it off in `kbrd.toml`:

```toml
[editor]
vim = false   # use the plain textarea editor instead
```

The footer shows the current **mode** (colored badge), the in-progress command
("showcmd", e.g. `2d`, `ci`, `f`), the cursor position (`Ln/Col`), and the active
`:` command-line.

---

## Modes

| Key | Mode |
| --- | --- |
| `i` `a` | Insert at / after cursor |
| `I` `A` | Insert at first non-blank / end of line |
| `o` `O` | Open a new line below / above and insert |
| `v` `V` | Visual (charwise) / Visual line |
| `:` | Ex command-line |
| `esc` | Back to Normal; from Normal, **close the editor** (prompts if unsaved) |

## Motions

| Keys | Motion |
| --- | --- |
| `h j k l` | left / down / up / right |
| `w b e` | word forward / back / end |
| `0 ^ $` | line start / first non-blank / end |
| `gg G` | first / last line |
| `{ }` | paragraph up / down |
| `f F t T` | find char forward/back, till; `;` `,` repeat |

## Editing

| Keys | Action |
| --- | --- |
| `x` `dd` `yy` | delete char / line · yank line |
| `p` `P` | paste after / before |
| `C` `D` `s` `S` `cc` | change/delete to EOL · substitute char/line |
| `r` `J` `~` | replace char · join lines · toggle case |
| `u` `ctrl+r` | undo / redo |
| `.` | repeat last change |
| `ctrl+a` `ctrl+x` | increment / decrement the number under the cursor |

## Operators + text objects

Operators combine with a motion or a text object, with optional counts
(`3dd`, `d2w`, `c$`):

| Operator | |
| --- | --- |
| `d` `c` `y` | delete / change / yank |
| `>` `<` | indent / dedent |
| `gu` `gU` `g~` | lowercase / uppercase / toggle case |

Text objects (use with an operator or in visual mode): `iw`/`aw` (word),
`i"` `i'` `` i` `` (quotes), `i(` `i[` `i{` (brackets), `ip`/`ap` (paragraph).
`i` = inner, `a` = around. Example: `ci"` change inside quotes, `dap` delete a
paragraph, `>ip` indent a paragraph, `viw` select a word.

## Surround

| Keys | Action |
| --- | --- |
| `S{c}` (visual) | wrap the selection with `{c}` |
| `ds{c}` | delete the surrounding `{c}` |
| `cs{old}{new}` | change surrounding `{old}` to `{new}` |

Pairs: `( ) [ ] { } < >` (brackets), `" ' \`` (quotes), `*` `_` (markdown).
Example: select a word and `S*` → `*word*`; `cs"'` turns `"x"` into `'x'`.

## Markdown helpers

| Keys | Action |
| --- | --- |
| `tab` (Normal) | toggle a `- [ ]` ↔ `- [x]` checkbox on the line |
| `enter` (Insert), `o` | continue the list marker (`-`, `*`, `1.`, `- [ ]`); ordered numbers increment |

Pressing `enter` on an empty list marker ends the list.

## Search & command-line

| Keys | Action |
| --- | --- |
| `/pat` `?pat` `n` `N` | search forward / back · next / previous |
| `:w` `:q` `:q!` `:wq` `:x` | save / quit (`:q` refuses on unsaved changes) |
| `:N` | jump to line N |
| `:N,M` | select lines N–M (linewise visual) |
| `:s/pat/repl/[g]` | substitute (first / `g` = all); `&` repeats on the current line |
| `:%s/…` · `:'<,'>s/…` | substitute over the whole file / a range |
| `:lua <expr>` | evaluate Lua against the line/selection (see below) |
| `:help` `:h` | open the cheatsheet |

The command-line autocompletes with `tab`: ex-commands, and registered function
names after `:lua`.

Long lines **soft-wrap** to the next visual row (the line number shows only on a
line's first row). A vertical scrollbar on the right edge indicates your position
in the file, and the **mouse wheel** scrolls the viewport.

## Clipboard

- Yanks (`yy`, `yiw`, visual `y`, …) are mirrored to the **system clipboard**.
- `ctrl+v` pastes the system clipboard at the cursor (Normal or Insert).
- Terminal paste (bracketed paste) inserts at the cursor in Insert mode.

## `:lua` — evaluate Lua on the buffer

`:lua <expr>` runs a Lua expression — usually a function you registered with
`kbrd.register` — against the current line or selection. A `ctx` table describes
the operand (`ctx.line`, `ctx.lines`, `ctx.text`, `ctx.range`, plus the card's
`fileName`/`columnName`/…), and the string you return **replaces** the operand.

```lua
-- in init.lua / .kbrd.lua
kbrd.register("wrap", function(width) return wrapText(ctx.line, width) end,
  "wrap(width) — hard-wrap the line")
```

```
:lua wrap(72)            -- transform the current line
V<select>:lua bullets()  -- transform the selected lines
:'<,'>lua sort()         -- same, explicit range
```

This complements `ctrl+l` (the menu of registered **line commands**): `ctrl+l` is
line-only and menu-driven; `:lua` is ad-hoc, takes arguments, and scales from a
line to a range. See **[SCRIPTING.md](./SCRIPTING.md#kbrdregistername-fn--kbrdregistername-fn-usage)**.

## Crash recovery

While editing an existing card, the buffer is flushed to a hidden swap file in the
column folder (`.<name>.md.kbrd-swap`). Saving clears it; a crash or `:q!` leaves
it, and the next time you open that card the editor offers to recover the unsaved
changes. There is no autosave — only `:w`/`ctrl+s` writes the card.

## Other

| Keys | Action |
| --- | --- |
| `ctrl+s` | save (keeps the editor open) |
| `ctrl+l` | run a line command |
| `ctrl+e` | expand / shrink the editor |
