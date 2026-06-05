# Templates — card templates example

Three worked card templates for the `T` (new item from template) flow. Each is a
Markdown file whose YAML frontmatter declares a form; the body is rendered with the
answers into a new card. Full reference: [TEMPLATES.md](../../TEMPLATES.md).

What they demonstrate:

- **[`task.md`](./task.md)** — the minimal useful template: one step with a required
  `input`, a `select` with a default, and an optional `text` field. The filename is
  derived from the title with `{{slug .title}}`.
- **[`meeting.md`](./meeting.md)** — two steps (the form paginates), a `confirm`
  field, a dated filename via `{{now "2006-01-02"}}`, `title`/`default` functions,
  and a body that scaffolds empty sections (Decisions, Action items) to fill in later.
- **[`bug.md`](./bug.md)** — the full field set: `select`, `multiselect` rendered both
  as `{{join .areas ", "}}` and as a `{{checklist .areas}}` task list, `confirm`, and
  validation (`required`, `min_len`, and a `pattern` with a friendly `pattern_hint`
  on the optional ticket field).

## Install

Copy any of these into:

- `<board>/.kbrd_templates/` — available in every column, or
- `<board>/<column>/.kbrd_templates/` — just that column (shadows a board template
  with the same `name`).

Then press `T` in a column. No reload needed — templates are read when the picker opens.
