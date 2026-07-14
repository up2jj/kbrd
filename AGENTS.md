# Repository Guidelines

## Project Structure

`main.go` is the small CLI entry point; Cobra commands live in `commands/`.
The Bubble Tea terminal UI is in `model/`, while headless board semantics are in
`board/` and `boardops/`. Supporting packages include `config/` (TOML and custom
commands), `fs/` (filesystem and Git helpers), `script/` (Lua API), `mcp/`,
`web/`, `template/`, `frontmatter/`, and `theme/`. Tests are colocated with
their packages as `*_test.go`. Demo fixtures and VHS capture scripts are in
`demo/`; README images are in `docs/screenshots/`. Keep `model/board.go` thin by
placing substantial shared behavior in focused helpers.

## Build, Test, and Development Commands

Use Go 1.26. The main recipes are:

```bash
just build          # build ./kbrd
just test           # go test ./...
just vet            # go vet ./...
just fmt            # format Go files with gofmt
just run -- --help  # run the CLI locally
just screenshots    # rebuild, seed demo data, and capture VHS screenshots
```

Install hooks with `just hooks`; run them manually with `prek run --all-files`.
For a direct build, use `go build -o kbrd ./`. If the default Go cache is not
writable, set `GOCACHE=/private/tmp/kbrd-go-build-cache`.

## Coding Style and Testing

Follow standard Go formatting (`gofmt`) and idioms: concise package names,
exported identifiers documented where required, errors wrapped with context,
and tests named after the behavior they cover. Add or update focused package
tests for behavior changes, then run `go vet ./...` and `go test ./...` before
submitting. UI changes should also be checked through the existing VHS/demo
workflow when they affect screenshots or layout.

## Commits and Pull Requests

Use concise Conventional Commit subjects such as `feat: ...`, `fix(model): ...`,
`docs: ...`, `refactor: ...`, or `test: ...`. Keep commits focused. Pull
requests should explain the user-visible or architectural change, list the
verification commands run, link relevant issues when applicable, and include
updated screenshots for visible TUI or web changes. Ensure CI is green and do
not commit generated build output, local board data, or personal tool settings.

## Security and Configuration

Treat board directories as potentially executable: board-local Lua, hooks, and
template commands can run when a board is opened. Use `kbrd --safe` when working
with untrusted boards, and never commit secrets or private keys.
