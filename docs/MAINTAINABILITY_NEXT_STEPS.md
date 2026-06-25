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

Custom-command extraction is still deferred, but several characterization tests were added first:

- filesystem vs virtual structured context parity
- structured context forwarding from `CustomCommandMenu`
- line-command menu dispatch to `runLineCommandMsg`
- virtual default command dispatch using stable selected virtual item identity

## Current Stopping Point

The broad maintainability refactor can stop here for review. Major completed pieces include:

- Stable item/column references for delayed mutations.
- Shared `boardops` workflows across web/script/TUI adapters.
- Narrower script capability interfaces.
- Helper boundaries for input routing, lifecycle, presenter/view frame, status, mutations, item actions, paste, frontmatter, line commands, help menu, editor eval, quick commands, search/session/managed files.
- Editor recovery and hook Board-side helper boundaries.
- Stronger characterization and stable-ref regression coverage.
- External temporary Go cache discipline for test/vet runs.

## Substantially Unfinished

### `custom_commands.go`

Still the largest maintainability concern. Do not casually extract it, but it is now the best next focused cleanup. It mixes:

- filesystem command context
- virtual-column command context
- shell/Lua command dispatch
- command menu state
- script UI yielding/resume behavior
- virtual item identity

The first layer of characterization exists, but add or confirm coverage for any specific behavior before moving it. The safest next extraction order is:

1. Extract command context building into a small helper.
   - Move `buildCommandVars`, `buildFilesystemCtx`, `buildVirtualVars`, and `commandsForColumn` together only if the resulting helper owns command availability and dispatch context clearly.
   - Keep public YAML/Lua command behavior unchanged.

2. Extract virtual command dispatch and virtual key handling.
   - Move `handleVirtualColumnKey`, `runVirtualDefault`, `virtualDefaultNoItem`, and `dispatchVirtualCommand` behind a virtual-command helper.
   - Preserve the stable selected item identity behavior covered by `TestVirtualColumn_DefaultCommandUsesStableSelectedItemIdentity`.

3. Leave `CustomCommandMenu` in place unless a later pass has a concrete reason to split menu state/rendering.
   - It already has direct menu behavior coverage.
   - Avoid mixing menu rendering refactors with command dispatch changes.

Before changing this file further, keep these scenarios covered:

- filesystem command vars/context parity
- virtual command vars/context parity
- `runCustomCommandMsg` construction from menus/help
- command completion routing
- virtual default command dispatch by stable virtual item identity

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

Start with `custom_commands.go`, but only extract one behavior group at a time.

Suggested first patch:

1. Introduce a `boardCommandContext` or similarly small helper in a new file.
2. Move command availability/context methods into it.
3. Keep `runCustomCommandMsg`, dispatch, virtual key handling, and `CustomCommandMenu` in `custom_commands.go`.
4. Run the targeted custom-command and virtual-column tests before considering the next extraction.

Suggested second patch:

1. Introduce a virtual command helper only after the context helper lands cleanly.
2. Move virtual key/default dispatch into it.
3. Preserve the current `handleKey` and help-menu entrypoints.

Stop there for another review before touching menu rendering or script resume behavior.

### Do Not Do Yet

- Do not split `scripting.go` broadly.
- Do not split `CustomCommandMenu` rendering while moving dispatch/context code.
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
