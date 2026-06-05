# Card templates

Create pre-structured cards from reusable templates. Press `T` in a column to pick a
template, fill in its form (multi-step, powered by [huh](https://github.com/charmbracelet/huh)),
and kbrd renders the result into a new `.md` card in that column.

A template is itself a Markdown file: YAML frontmatter declares the form, the body is a
Go [`text/template`](https://pkg.go.dev/text/template) that receives the answers.

- [Where templates live](#where-templates-live)
- [Template format](#template-format)
- [Field types](#field-types)
- [Filenames](#filenames)
- [Template variables & functions](#template-variables--functions)
- [Shell commands (`{{shell}}`)](#shell-commands-shell)
- [Flow & keys](#flow--keys)
- [Errors & edge cases](#errors--edge-cases)
- [Scripting (Lua)](#scripting-lua)
- [Examples](#examples)

---

## Where templates live

Two locations, merged when you press `T`:

| Location | Scope |
| --- | --- |
| `<board>/.kbrd_templates/*.md` | Every column on the board |
| `<board>/<column>/.kbrd_templates/*.md` | Just that column |

The leading `.` keeps these folders out of the board view — they never appear as columns
or cards.

**Shadowing:** when a column template and a board template share the same display `name`,
the column-local one wins. Board-scoped entries are marked `(board)` in the picker so you
can tell them apart.

---

## Template format

```markdown
---
name: Bug report                 # display name (optional; falls back to the filename)
filename: "bug-{{slug .title}}"  # filename template (optional; see Filenames)
steps:                           # form pages, filled in order
  - title: Basics
    fields:
      - key: title
        type: input
        title: Bug title
        required: true
      - key: severity
        type: select
        title: Severity
        options: [low, medium, high]
        default: medium
  - title: Details
    fields:
      - key: repro
        type: text
        title: Repro steps
---
# Bug: {{.title}}

Severity: {{.severity}}

## Repro
{{.repro}}
```

Each entry in `steps` becomes one form page; the user moves through them in order with
`tab`/`enter` and back with `shift+tab`. Everything after the closing `---` is the card
body template, rendered with the collected answers.

### Field properties

| Property | Applies to | Meaning |
| --- | --- | --- |
| `key` | all but `note` | Variable name used in the body (`{{.key}}`). Must be unique within the template. |
| `type` | all | One of the [field types](#field-types). |
| `title` | all | Label shown above the field. |
| `description` | all | Muted helper text under the title. |
| `placeholder` | `input`, `text` | Ghost text shown while empty. |
| `default` | `input`, `text`, `select`, `confirm` | Initial value. For `select` it must be one of `options`; for `confirm` use `"true"`/`"false"`. |
| `required` | `input`, `text`, `multiselect` | Blocks advancing while empty / nothing selected. |
| `options` | `select`, `multiselect` | The choices. Required for these types. |
| `prefill` | `input`, `text` | Seed the form field with external content; `clipboard` is the only source. Mutually exclusive with `default`. |
| `pattern` | `input`, `text` | RE2 regular expression the value must match. |
| `pattern_hint` | `input`, `text` | Friendly message shown when `pattern` doesn't match (instead of the raw regex). |
| `min_len` / `max_len` | `input`, `text` | Length bounds in characters (0 = unbounded). |

### Prefill from the clipboard

An `input` or `text` field can start out holding the system clipboard's
content — handy for pasting a stack trace or URL you just copied:

```yaml
- key: trace
  type: text
  title: Stack trace
  prefill: clipboard
```

The content lands **in the visible form field**, where you can edit or clear
it before submitting, and field validation applies to it like any typed
value. By design there is no `{{clipboard}}` template function: rendering
never reads anything you didn't see in the form, so a template from an
untrusted board cannot silently capture clipboard contents. `prefill` has no
effect on the Lua path (`createFromTemplate` takes explicit values). When no
clipboard is available (e.g. SSH), the field simply starts empty.

### Validation

`input` and `text` values can be constrained:

```yaml
- key: ticket
  type: input
  title: Ticket ID
  required: true
  pattern: '^KB-[0-9]+$'
  pattern_hint: must look like KB-123
  min_len: 4
  max_len: 16
```

The form blocks advancing past an invalid value and shows `pattern_hint` (or
the raw regex if no hint is set); the Lua path
(`kbrd.board.createFromTemplate`) enforces the same rules and returns the
same message as an error. Constraints bind only when a value is provided —
an empty optional field always passes; combine with `required` to forbid
empty.

A `pattern` that doesn't compile, constraints on non-text field types, or a
`default` that violates its own constraints are template-author errors and
reject the template at load time (warning toast).

> YAML tip: prefer single quotes around regexes — `'^\d+$'` survives intact,
> while in double quotes every backslash must be doubled.

---

## Field types

| Type | Widget | Body value |
| --- | --- | --- |
| `input` | Single-line text | string |
| `text` | Multi-line text (4 lines) | string |
| `select` | Single choice list | string |
| `multiselect` | Checkbox list | list — render with `{{join .key ", "}}` |
| `confirm` | Yes / No buttons | `true` / `false` |
| `note` | Display-only text (no `key`, no value) | — |

---

## Filenames

The new card's filename comes from the frontmatter `filename` template (without `.md`).
If omitted, the form gains a final **Filename** page that asks for one.

Filenames must stay inside the column: path separators and `..` are rejected, and
whitespace runs (including newlines) are collapsed to single spaces. When building a
filename from free-text answers, pass them through `slug`:

```yaml
filename: "bug-{{slug .title}}"   # "Crash on save!" → bug-crash-on-save.md
```

Creation never overwrites: if the file already exists you get an error and nothing is
written.

---

## Template variables & functions

The body and `filename` templates see your form answers **plus** the standard board
variables:

| Variable | Meaning |
| --- | --- |
| `{{.boardPath}}` / `{{.boardName}}` | Board root path / configured name |
| `{{.columnPath}}` / `{{.columnName}}` | Target column path / name |

Form field keys may not reuse these names — that's rejected at load time.

**Functions**

| Function | Meaning |
| --- | --- |
| `{{slug .title}}` | Lowercase, non-alphanumerics → `-` (filename-safe) |
| `{{join .areas ", "}}` | Join a multiselect's values |
| `{{checklist .areas}}` | Multiselect values as markdown tasks: `- [ ] UI`, one per line |
| `{{now "2006-01-02"}}` | Current local time in a [Go layout](https://pkg.go.dev/time#pkg-constants) — works in `filename` too |
| `{{default "unset" .sev}}` | Fallback when the value is empty (pipes: `{{.sev \| default "unset"}}`) |
| `{{upper .v}}` / `{{lower .v}}` / `{{title .v}}` | Case conversion (`title` capitalizes each word) |
| `{{trim .v}}` | Strip surrounding whitespace |
| `{{truncate 50 .v}}` | Cap at N characters, appending `…` when cut (pipes too) |
| `{{env "VAR"}}` | Environment variable (empty if unset) |
| `{{shell "cmd" .a .b}}` | Run a command after the card is created, filling the spot with its output — see [Shell commands](#shell-commands-shell) |

Referencing an undeclared variable is an error (`missingkey=error`), so typos fail loudly
at creation instead of rendering blanks.

### Syntax in 60 seconds

Templates use Go's [`text/template`](https://pkg.go.dev/text/template) syntax — here is
everything you need without reading the Go docs. `{{...}}` is a placeholder; everything
outside is literal markdown.

**Insert a value** — your field keys (and the board variables) with a leading dot:

```
# {{.title}}
```

**Call a function** — name first, arguments after, separated by spaces:

```
{{slug .title}}          {{truncate 40 .title}}
```

**Chain functions** — pipe with `|`; each step's output becomes the *last* argument of
the next:

```
{{.title | trim | truncate 40 | upper}}
{{.severity | default "unset" | upper}}
```

…or nest with parentheses, which is equivalent:

```
{{upper (default "unset" .severity)}}
```

> Exception: `join` cannot be piped (`{{.areas | join ", "}}` fails — the list must be
> the first argument). Use the direct form `{{join .areas ", "}}`.

**Conditionals** — show a line only when a field was filled:

```
{{if .ticket}}Ticket: {{.ticket}}{{end}}
{{if .regression}}⚠ regression{{else}}new bug{{end}}
```

**Loops** — over a multiselect's values; `.` is the current item inside the loop:

```
{{range .areas}}- area: {{.}}
{{end}}
```

(For the common case, `{{checklist .areas}}` and `{{join .areas ", "}}` already do this.)

**Formatting** — `printf` works like in most languages: `{{printf "[%s]" .severity}}`.

**Whitespace control** — `{{-` and `-}}` eat the adjacent whitespace/newline, useful to
keep optional lines from leaving blank gaps:

```
{{- if .ticket}}
Ticket: {{.ticket}}
{{- end}}
```

---

## Shell commands (`{{shell}}`)

A template can offload part of a card to an external command — most usefully a slow one
like an LLM:

```markdown
## Likely causes
{{shell "claude -p 'List 3 likely root causes, terse bullets'" .title " — repro: " .repro}}
```

**How it works.** The command does **not** block the form. On submit the card is created
immediately with a `⏳ running…` placeholder, a background worker runs the command, and the
placeholder is replaced with the output when it finishes (the board live-reloads, so the
card fills in on its own). A header chip `✦ generating` shows while any command is running.

- **Arguments are stdin.** Everything after the command string is concatenated and piped to
  the command's standard input — `{{shell "wc -w" .notes}}` counts the words in `.notes`.
  The command string itself is fixed by the template author; form answers only reach it via
  stdin, so an answer can never inject into the command line.
- **Put `{{shell}}` on its own line or in its own section.** The placeholder and the
  disabled/interrupted states are block-level notes; inline (`Created: {{shell …}}`) works
  for the success case but looks odd in the others.
- **Errors** become an inline note (`⚠ command exited 3`, with any output) — the rest of the
  card is unaffected.
- **Interrupted** (you quit, or the app is killed, while a command runs): the card keeps its
  placeholder, and the next time you open the board it is rewritten to
  `⚠ generation interrupted`. Nothing is left frozen, and a committed mid-run card is just an
  invisible HTML comment plus that line — harmless and recoverable.

**Enabling it — `[template] exec`.** Shell exec is **off by default**. A template's `{{shell}}`
directives render as an inert "disabled" note until you opt in, in `kbrd.toml`:

```toml
[template]
exec = true
```

It is opt-in because a `{{shell}}` command runs with kbrd's **full environment** (including
secrets like `$ANTHROPIC_API_KEY`) and templates are shared/pasted more casually than whole
boards. Commands run **only on `T`-submit** — never at render time, and never implicitly on
the Lua path. To open a board you don't fully trust with everything defused, launch
`kbrd --safe`, which forces scripting, hooks, and template exec off regardless of config
(see [SECURITY.md](./SECURITY.md)).

---

## Flow & keys

| Keys | Action |
| --- | --- |
| `T` | Open the template picker for the current column |
| `↑` / `↓`, `enter` | Pick a template (skipped when there is exactly one) |
| `tab` / `enter` | Next field / next step |
| `shift+tab` | Previous field / step |
| `esc` `esc` | Cancel the form (first `esc` arms, any other key disarms) |

On submit the card is created, selected, and the `item_created` event fires — Lua hooks
and YAML hooks see template-created cards exactly like manually created ones.

---

## Errors & edge cases

- **No templates anywhere** → a toast tells you which folders to create.
- **A template fails to parse or validate** (bad YAML, unknown `type`, `select` without
  `options`, duplicate/reserved `key`, broken `{{...}}` syntax) → it is skipped with a
  warning toast; valid templates still load.
- **No `steps`** → no form: with a `filename` the card is created immediately on pick;
  without one you are asked just for the filename.
- **Existing filename** → "file already exists" error, nothing overwritten.
- **Virtual columns** are read-only — `T` is rejected there.

---

## Scripting (Lua)

Templates can also be filled programmatically — no form involved:

```lua
local tmpls = kbrd.board.templates("1. To do")
-- → {{name="Bug report", scope="column"}, ...}

kbrd.board.createFromTemplate("1. To do", "Bug report", {
  title = "Crash on save", severity = "high",
  areas = {"UI", "data"}, regression = true,
})
```

Omitted keys take the field defaults; `required` fields must be provided. The
card is created through the same path as the interactive flow, so
`item_created` fires identically. See the
[API reference in SCRIPTING.md](./SCRIPTING.md#kbrdboardcreatefromtemplatecolumn-template-values).

`{{shell}}` directives are **not** run on the Lua path — they render as the disabled note.
A script that wants async work should call `kbrd.async.run` itself (it can then
`createFromTemplate` with the result), so shell execution stays explicit rather than
implicit.

---

## Examples

Worked examples live in [`examples/templates/`](./examples/templates/):

- [`task.md`](./examples/templates/task.md) — minimal single-step template
- [`meeting.md`](./examples/templates/meeting.md) — two steps, confirm field, scaffolded sections
- [`bug.md`](./examples/templates/bug.md) — the full field set: select, multiselect with `join`, confirm

Copy them into `<board>/.kbrd_templates/` (or a column's `.kbrd_templates/`) and press `T`.
