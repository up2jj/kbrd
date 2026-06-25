# Maintainability Refactor Handoff

This refactor is at a reasonable stopping point. The codebase is materially better than where it started, and the previously-blocking `kbrd.editor.open` correctness issue has been fixed. Remaining work should stay focused and test-first.

## Completed In Latest Pass

The blocking reviewer finding in `model/scripting.go` is resolved.

- `kbrd.editor.open` now treats absolute paths and path-like relative inputs as specific paths.
- Missing path-like inputs fail instead of falling back to basename/name matching.
- Bare names such as `todo` and `todo.md` still use the existing basename/name behavior.
- Regression coverage now locks stale absolute paths, missing relative paths, valid current-board relative paths, and bare basename fallback.

Two planned helper extractions are also complete:

- `model/editor_swap.go` routes recovery handling through `boardEditorRecovery`.
- `model/hooks.go` routes hook init/drain/done glue through `boardHooks` without changing FIFO scheduling.

The first custom-command extraction pass is complete:

- filesystem vs virtual structured context parity
- structured context forwarding from `CustomCommandMenu`
- line-command menu dispatch to `runLineCommandMsg`
- virtual default command dispatch using stable selected virtual item identity
- command availability/context now lives behind `boardCommandContext`
- virtual-column key/default dispatch now lives behind `boardVirtualCommands`

## Current Stopping Point

The broad maintainability refactor can stop here for review. Major completed pieces include:

- Stable item/column references for delayed mutations.
- Shared `boardops` workflows across web/script/TUI adapters.
- Narrower script capability interfaces.
- Helper boundaries for input routing, lifecycle, presenter/view frame, status, mutations, item actions, paste, frontmatter, line commands, help menu, editor eval, quick commands, search/session/managed files.
- Editor recovery and hook Board-side helper boundaries.
- Custom command context/availability and virtual-command helper boundaries.
- Stronger characterization and stable-ref regression coverage.
- External temporary Go cache discipline for test/vet runs.

## Substantially Unfinished

### `custom_commands.go`

Much improved. It now keeps command loading, shell/Lua execution, completion messages, and `CustomCommandMenu` state/rendering. The context/availability and virtual-column dispatch responsibilities have moved out.

Do not split it further casually. The remaining mixed areas are:

- shell/Lua command execution and terminal restore behavior
- command menu state, filtering, MRU ordering, and rendering
- line-command menu routing via `CustomCommandMenu.OpenLine`
- script UI yielding/resume behavior through `handleScriptResult`

Further extraction should be optional and test-first. The next plausible split would be menu state/rendering, but only if there is a concrete follow-up that needs it.

### `scripting.go`

Still orchestration-heavy and central. Some of that is legitimate because script timers, async work, UI yields, editor-open requests, and selection events cross-cut the Bubble Tea root model. Avoid splitting this until there are clearer seams and tests around each queue/dispatch path.

Possible future extraction, only after `custom_commands.go`:

- a tiny editor-open resolver/helper around `collectEditorOpenCmd` and `resolveEditorTarget`
- queue-drain helpers for timers/status/async only if tests make the ordering constraints explicit

### `model.Board`

`Board` is smaller, but still the root model. The remaining surface should be treated as either:

- true root lifecycle/update/view ownership
- mutation/event compatibility seams
- script/custom-command orchestration
- small helper accessors

Do not chase method count alone.

## Next Planned Changes

### Recommended Next Pass

Stop here for review unless there is a specific follow-up feature or bug. The custom-command refactor now has clear boundaries and coverage for the tricky context/virtual-command paths.

If continuing maintainability work, prefer one of these focused passes:

1. Audit `custom_commands.go` menu rendering/state only.
   - Keep command dispatch and script resume behavior untouched.
   - Add menu-view or state-machine tests before moving rendering helpers.

2. Audit `scripting.go` queue/drain paths.
   - Do not split broadly.
   - Start only with a tiny editor-open resolver/helper or timer/status/async drain helper if tests make ordering constraints explicit.

3. Run the non-gating Board surface audit and use it only as navigation.
   - Do not optimize for method count alone.

### Do Not Do Yet

- Do not split `scripting.go` broadly.
- Do not split `CustomCommandMenu` rendering together with command dispatch changes.
- Do not change command YAML, Lua command registration, scopes, `requiresItem`, or keybindings.
- Do not optimize for Board method count alone.

## Useful Audit

Run:

```sh
tmp=$(mktemp -d /private/tmp/kbrd-gocache.XXXXXX)
GOCACHE=$tmp go test -run TestBoardSurfaceAudit -v ./model
rc=$?
rm -rf "$tmp"
exit $rc
```

The audit test is non-gating. It logs remaining `Board` methods by file as a navigation aid, not as a strict budget.

## Verification Baseline

For future passes:

```sh
tmp=$(mktemp -d /private/tmp/kbrd-gocache.XXXXXX)
GOCACHE=$tmp go test ./boardops ./model ./script ./web ./mcp
rc=$?
rm -rf "$tmp"
exit $rc
```

Then, where local listener tests are allowed:

```sh
tmp=$(mktemp -d /private/tmp/kbrd-gocache.XXXXXX)
GOCACHE=$tmp go test ./...
rc=$?
rm -rf "$tmp"
exit $rc
```

Also run:

```sh
go vet ./...
git diff --check
find . -maxdepth 3 -type d \( -name '.tmp' -o -name 'gocache' -o -name 'gomodcache' \) -print
find /private/tmp -maxdepth 1 -type d -name 'kbrd-gocache.*' -print
```
