# Maintainability Refactor Handoff

This refactor is at a reasonable stopping point. The codebase is materially better than where it started, but there are a few unfinished or deliberately deferred areas that should be handled in later, focused passes.

## Before More Refactoring

Fix the reviewer finding in `model/scripting.go` around `kbrd.editor.open` path resolution.

- Problem: an invalid absolute/relative path such as `/other-board/todo.md` can fall back to basename matching and open an unrelated current-board card named `todo`.
- Expected behavior: full-path-like inputs should only match exact current-board item paths. If the path misses, fail instead of falling back to basename/name matching.
- Add a regression test for stale/cross-board paths with duplicate basenames.

This is a correctness issue and should be resolved before continuing architectural cleanup.

## Good Stopping Point

The broad maintainability refactor can stop here for review. Major completed pieces include:

- Stable item/column references for delayed mutations.
- Shared `boardops` workflows across web/script/TUI adapters.
- Narrower script capability interfaces.
- Helper boundaries for input routing, lifecycle, presenter/view frame, status, mutations, item actions, paste, frontmatter, line commands, help menu, editor eval, quick commands, search/session/managed files.
- Stronger characterization and stable-ref regression coverage.
- External temporary Go cache discipline for test/vet runs.

## Substantially Unfinished

### `custom_commands.go`

Still the largest maintainability concern. Do not casually extract it. It mixes:

- filesystem command context
- virtual-column command context
- shell/Lua command dispatch
- command menu state
- script UI yielding/resume behavior
- virtual item identity

Recommended next pass should be test-first:

- filesystem command vars/context parity
- virtual command vars/context parity
- `runCustomCommandMsg` construction from menus/help
- command completion routing
- virtual default command dispatch by stable virtual item identity

### `scripting.go`

Still orchestration-heavy and central. Some of that is legitimate because script timers, async work, UI yields, and selection events cross-cut the Bubble Tea root model. Avoid splitting this until there are clearer seams and tests around each queue/dispatch path.

### `model.Board`

`Board` is smaller, but still the root model. The remaining surface should be treated as either:

- true root lifecycle/update/view ownership
- mutation/event compatibility seams
- script/custom-command orchestration
- small helper accessors

Do not chase method count alone.

## Planned But Not Implemented

### Editor Recovery Helper

Candidate: extract `model/editor_swap.go` into `boardEditorRecovery`.

Current methods:

- `handleRecoverEditor`
- `handleRecoverApply`
- `handleRecoverDiscard`

This looks low-risk and isolated, but was not implemented.

### Hook Helper

Candidate: extract hook message handling/collection into `boardHooks`.

Be careful: hook scheduling is part of post-update draining behavior. Add/confirm tests before moving.

### Custom Command Extraction

Deferred intentionally. This should be a dedicated pass after the reviewer finding is fixed.

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
