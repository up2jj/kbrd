# Immutable Operation Log Plan

## Goal

Replace snapshot-oriented Git conflict handling with an append-only operation
log that converges automatically across compatible kbrd clients. Git remains the
transport, durability, and commit-history layer; kbrd owns board semantics,
validation, and deterministic reduction.

Users continue working with ordinary Markdown cards and retain useful `+N/-N`,
new, deleted, renamed, moved, and pending-sync indicators. A filesystem edit is
imported into the operation log before kbrd commits or synchronizes it. Normal
concurrent card changes do not create conflict sidecars or require a review
inbox.

This is a breaking storage rewrite. There is no in-place production migration,
dual-write period, legacy reader, or automatic rollback. Development boards and
fixtures will be recreated. Before release, kbrd must provide a clean export
path so a damaged or secret-bearing log can be replaced by a new repository
without copying immutable history.

## Product decisions

1. Cards and columns have stable UUIDs independent of presentation paths.
2. Immutable operations and content-addressed blobs are authoritative.
3. Markdown cards and column directories remain editable, tracked projections.
4. Working-tree drift is imported before a kbrd-managed commit. A committed
   projection change without corresponding operations is invalid input: sync
   must diagnose it and stop, never silently overwrite it.
5. Projection conflicts are disposable inside a kbrd-owned candidate merge.
   After operation union and validation, kbrd regenerates every affected
   projection before it creates the candidate commit.
6. The same valid operation set, format version, reducer version, and diff
   version must produce a byte-identical board. A reducer upgrade is an explicit
   repository-format transition, not an implicit local choice.
7. Concurrent changes are resolved automatically using card-aware rules.
8. Body text inserted by a concurrent operation remains visible until a
   causally later delete explicitly names its element IDs. Concurrent identical
   insertions remain distinct operations.
9. Git uses one shared branch with ordinary commits, fetches, merge commits,
   fast-forwards, and bounded non-fast-forward retries. Normal sync never
   rebases or force-pushes.
10. TUI, web, MCP, scripts, reminders, ingest, notifications, and filesystem
    drift use one mutation and synchronization engine.
11. Wall-clock time is informational. It never decides causal order, winners,
    element placement, or filenames.
12. Reduction is pure: it never writes operations, invokes hooks, runs Lua, or
    accesses Git. Version 1 has no synthetic merge operations.
13. Integrity is not authenticity. A configured Git remote is a trust boundary,
    and safe mode continues to govern whether synchronized executable content
    may run.
14. The first end-to-end implementation covers cards and columns. Arbitrary
    resources are enabled only after the card protocol and Git transaction pass
    their release gates.

## Format and compatibility

Tracked `.kbrd/format.json` pins every behavior that can change output:

```json
{
  "protocol": 1,
  "canonical_encoding": "kbrd-json-v1",
  "reducer": "kbrd-sequence-v1",
  "snapshot_diff": "kbrd-rune-diff-v1"
}
```

Clients must reject an unknown value before importing drift, reducing records,
or changing the worktree. Two branches with different format values cannot be
merged by normal sync.

A format upgrade is an explicit command that requires a clean, synchronized
branch and the repository lock. It writes the new format, regenerates
projections, commits, and pushes as one transition. Older clients then fail
closed with an upgrade-required error. Any reducer needed to interpret a
retained revision remains available until no supported external-editor base can
refer to that revision. Format changes are the only allowed exception to the
append-only metadata rule described below.

## Repository and local-state layout

```text
.kbrd/
  format.json
  log/
    ab/
      <remaining-operation-hash>.json
  blobs/
    cd/
      <remaining-content-hash>
  revisions/
    ef/
      <remaining-revision-hash>.json

Todo/
  .kbrd-column
  task.md
Doing/
  .kbrd-column
  other.md
```

Two hexadecimal fan-out characters keep large histories out of a single
directory. Hashes use lower-case hexadecimal SHA-256 without a `sha256:` prefix
in paths. The prefix may appear in user-facing or JSON values.

`.kbrd-column` is generated projection metadata containing the stable column ID
and revision. It makes empty-column moves observable. It is reserved and hidden
from normal UI and user-facing diffs.

Machine-local checkpoints, verified-commit caches, journals, indexes, locks,
actor identity, path history, and displaced-file backups are not tracked:

- the repository-wide advisory lock lives below `git rev-parse
  --git-common-dir` so linked worktrees and separate kbrd processes serialize;
- each worktree's actor, completed manifest, journal, temporary index, and path
  history live below `git rev-parse --git-path kbrd`; and
- state directories and files are created with private permissions.

Use an OS advisory lock, not a PID lockfile requiring stale-owner guesses. Every
mutation, drift import, materialization, format upgrade, and sync transaction
holds the same repository lock. External editors and raw Git commands do not
honor it, so all worktree writes still use compare-and-preserve semantics and
all ref changes still use compare-and-swap.

### Append-only Git invariant

Files under `.kbrd/log/`, `.kbrd/blobs/`, and `.kbrd/revisions/` are immutable
and append-only across Git history:

- for a one-parent commit, every immutable path in the parent must exist with
  identical bytes in the child;
- for a merge commit, the child must contain the byte-identical union of the
  immutable paths from every parent; and
- a path collision with different bytes is corruption, even if both files are
  otherwise valid.

This rule catches deletion of an unreferenced leaf operation, which graph
closure validation alone cannot detect. Candidate construction unions immutable
trees explicitly; it does not delegate deletion semantics to Git's textual
merge.

On first open, kbrd verifies append-only edges in reachable history and caches
verified commit IDs locally. Later opens verify only unseen edges. Sync always
verifies the local and fetched histories from their merge base through both
tips. A rewritten or shallow history that cannot establish the invariant is a
diagnostic error until the user deepens, repairs, or explicitly re-adopts it.

Extra unreferenced immutable blobs are allowed but reported by `kbrd doctor`.
They never influence reduction. Normal synchronization never deletes them.

### Projection consistency invariant

Every local or fetched tip used as a sync parent must have tracked projections
equal to the byte-identical materialization of that tip's operation set and
pinned format. Every commit created by kbrd has the same property and carries
recomputed `Kbrd-Format` and `Kbrd-Frontier` trailers; the frontier trailer is
the hash of the sorted global target-head set and is an optimization, not a
trusted assertion.

A raw commit that changes a card or column projection without adding the
corresponding operation therefore becomes an inconsistent tip and is rejected
before merge. This prevents regeneration from silently discarding its Markdown
edit. Unlike immutable-path validation, projection consistency need not hold at
every historical raw commit: an explicit repair child may import that commit's
projection delta, restore a consistent tip, and record a
`Kbrd-Imported-Commit` trailer. The original raw commit remains visible in Git
history.

Ordinary filesystem editing remains supported: `kbrd sync`, the running app,
and kbrd's commit path inventory the worktree and import drift first. A
diagnostic repair command may translate an unambiguous local, single-parent raw
commit into operations in a new child commit. Remote or ambiguous committed
drift is never guessed during automatic sync; the error names the offending
commit and paths.

## Operation model

The wire envelope is deliberately small. Kind-specific payloads have concrete
schemas; arbitrary maps are not part of the canonical protocol.

```go
type Operation struct {
	Protocol   int
	ActorID    string
	Nonce      string
	OccurredAt string // optional RFC 3339; informational only
	Source     string // tui, web, mcp, lua, filesystem, repair, ...
	BatchID    string // optional correlation for one multi-target user action
	Target     Target
	Parents    []string
	Kind       Kind
	Payload    json.RawMessage // decoded immediately into the Kind's concrete type
}

type Target struct {
	Type TargetType // card, column, or resource
	ID   string     // stable UUID
}

// Record is an in-memory value. ID is not part of the hashed bytes.
type Record struct {
	ID string
	Operation
}
```

- An operation ID is the SHA-256 hash of the canonical operation bytes. Its
  filename contains the hash; the ID is never included in the bytes it hashes.
- `ActorID` is a random per-worktree identity stored privately. It is not a
  hostname, user identity, trust credential, or ordering source.
- `Nonce` is fresh 128-bit randomness for every locally requested operation.
  It ensures two identical requests by the same actor against the same parents
  remain distinct. Deterministic maintenance operations, if introduced by a
  later protocol, must specify a deterministic nonce derivation.
- `OccurredAt`, `Source`, and `BatchID` support history and event grouping. They
  do not participate in conflict-resolution comparisons except that they are
  ordinary hashed payload bytes.
- `Parents` is the sorted set of operation heads for the same target visible
  when the operation was created. It supplies causal order.
- Paths, blobs, modes, fields, inserts, and deletes live only in payloads for
  kinds that use them. A body edit cannot accidentally act as a path write.

Initial kinds include card create, body edit, scalar-field set/delete, tag
add/remove, checklist-state set, card path set, card delete/restore, column
create, column path set, and column delete/restore. One operation may contain
the related changes to a single target needed for an atomic edit. `BatchID`
groups bulk actions across targets without pretending that Git provides an
atomic distributed transaction.

Writers durably create referenced blobs before operations and create operation
files with no-clobber semantics. Reusing an existing hash is valid only when the
existing bytes match exactly.

### Canonical encoding

`kbrd-json-v1` must have a standalone specification and golden vectors before
implementation. It defines:

- valid UTF-8 and exact string escaping;
- lexicographic object-key order;
- integers only, with no floats or alternate number spellings;
- required, omitted, and null fields;
- sorted set fields such as parents and deleted element ranges;
- exact payload schema for every operation kind;
- rejection of unknown or duplicate object fields; and
- one trailing newline for JSON record files.

User Markdown is not Unicode-normalized: visually identical scalar sequences
remain distinct bytes. Card text converts CRLF and bare CR to LF on import and
materializes with LF. Invalid UTF-8 is preserved in a displaced backup/blob and
reported as an import error rather than replaced or partially decoded.

## Card semantic model

Generated `kbrd_id`, `kbrd_revision`, checklist IDs, and column markers are
projection metadata. They are excluded from semantic blobs, user-facing diffs,
and CRDT text.

### Body sequence protocol

The body uses a span-oriented, YATA-style sequence CRDT. The protocol is not
defined merely by the summary below: before reducer implementation, Phase 0
must add a normative integration algorithm with pseudocode, tie-breaking rules,
worked examples, and language-neutral conformance fixtures. The algorithm name
and fixture digest become part of `kbrd-sequence-v1`.

The normative design must provide these properties:

- the sequence operates on Unicode scalar values grouped into immutable insert
  spans;
- an insert records its original visible left and right origins plus inserted
  UTF-8 text;
- each element has identity `(operation ID, scalar offset)`;
- integration uses both origins and a specified recursive ordering rule, so
  nested concurrent insertions and non-adjacent or tombstoned origins have one
  result;
- a delete names observed element IDs or canonical contiguous ranges and is
  valid only when those elements are visible from the operation's parents;
- replacement is observed deletion plus insertion in one body-edit payload;
- tombstones retain origin identity; and
- arrival order, map iteration, local/remote role, wall time, and Git commit
  order never affect output.

`kbrd-rune-diff-v1` likewise specifies its exact algorithm, tie-breaking, and
normalization. An external save is compared with the exact embedded revision,
then translated into insert/delete spans over that revision's element IDs.
Concurrent equal text is never deduplicated.

### Revision manifests

Markdown cards carry:

```yaml
---
kbrd_id: 019c0000-0000-7000-8000-000000000001
kbrd_revision: sha256:8f17...
---
```

A card revision manifest contains:

- protocol and reducer version;
- target ID;
- sorted heads for that target only;
- semantic Markdown blob hash; and
- a sequence-index blob mapping semantic scalar ranges to element IDs and
  recording tombstoned origins required by stale editors.

The revision does not contain the rendered path or global board frontier.
Unrelated card changes therefore do not churn every card's revision token. Path
and exact final projection-byte hashes live in the local completed-generation
manifest.

Revision IDs hash canonical manifest bytes. Revision files and their referenced
blobs are retained append-only so a stale external editor does not depend solely
on an optional checkpoint or on reconstructing an old element map. The importer
still checks that its binary supports the manifest's reducer and diff versions.

If a buffer saves to a card's former path after a move, path history resolves it
as a stale save when that path, target ID, and embedded revision match a
previously materialized generation. Its content is applied to the existing
target while the current winning path is retained. Otherwise a duplicate card
ID is a copy: the manifest path keeps the ID and other occurrences receive new
IDs in deterministic path order. This unavoidable ambiguity intentionally
prefers preservation of an identifiable stale edit; `kbrd copy` is the
unambiguous copy operation.

Removing or changing generated identity metadata at a currently managed path is
an import error, not an implicit delete-and-create. A new unmanaged Markdown
file without an ID receives a new card ID.

### Frontmatter and checklists

Version 1 treats supported top-level frontmatter semantically:

- `kbrd_*` keys are reserved generated metadata;
- tags are an observed-remove set;
- each other valid top-level key is an independent register whose value is a
  canonical YAML node; causal descendants win and concurrent writes use
  operation-ID order;
- deleting a key writes a tombstone rather than an absent snapshot; and
- duplicate keys, aliases that escape supported value limits, and malformed
  frontmatter fail import while preserving the edited bytes.

Comments, quoting style, key order, and other YAML presentation choices are not
authoritative and may be canonicalized by materialization. This breaking
behavior must be documented rather than implying formatting preservation.

Checklist recognition uses one pinned Markdown subset. Generated invisible HTML
comments carry stable entry UUIDs. Import removes the generated comment and
completion marker from CRDT label text, records completion as structured state,
and leaves indentation, label text, line position, and surrounding Markdown in
the body sequence. Removing an existing ID means remove-old plus add-new; the
importer never guesses identity from label text. Duplicate IDs are resolved in
deterministic path and sequence order.

### Columns

Columns are first-class targets. Their operations define create, path/name set,
delete, and restore. `.kbrd-column` carries identity for external moves and
empty directories. Concurrent column paths use the same deterministic winner
and collision-suffix rules as cards. Moving a card between columns remains a
card path operation; deleting a column does not implicitly delete its cards.
The reducer deterministically moves surviving cards to the configured fallback
column or a generated recovery column.

## Resource model and staged scope

Resources eventually cover an explicit allowlist of board configuration,
templates, Lua, hooks, and attachments. They use complete content blobs and
create, replace, path set, mode set, delete, and restore operations.

Resource projection formats generally cannot carry identity. Therefore:

- a kbrd API rename preserves resource identity;
- an external rename is inferred only when the old path disappears and the new
  path has identical bytes and supported mode;
- an external rename combined with an edit is deterministically represented as
  delete plus create because identity cannot be proven; and
- copies receive new IDs.

The plan does not claim perfect external resource rename inference. Resource
history may split across an ambiguous rename, but content remains preserved.

Until the resource phase is enabled, automatic sync refuses divergent changes
to security-sensitive or otherwise managed non-card files rather than creating
sidecars or guessing. Ordinary non-board files are merged by Git outside kbrd's
managed tree and may still require normal Git handling.

## Validation and trust boundary

Every local or fetched candidate is validated before graph construction,
reduction, ref advancement, index installation, or worktree materialization:

- format values are known and identical across merge parents;
- every unseen Git edge satisfies the append-only invariant;
- filenames equal canonical-content hashes;
- referenced blobs and revisions exist and match their hashes;
- versions, kinds, payload schemas, target types, UUIDs, nonces, required
  fields, UTF-8, and configured size/count/depth limits are valid;
- parents exist, address the same target, are sorted and unique, and form a
  valid causal graph;
- body origins and element ranges exist on the same card, and deleted elements
  are causally visible from the declared parents;
- local and fetched tip projections exactly match reduction, all kbrd-created
  commit trailers recompute correctly, and any claimed raw-commit repair names
  its imported ancestor;
- paths are normalized relative slash paths within configured length limits,
  avoid reserved names and case-fold collisions, and do not address `.git`,
  `.kbrd`, generated metadata, `.gitmodules`, or another protected path; and
- resource kinds, media types, sizes, and modes are allowlisted.

Filesystem containment must use Go's root-relative filesystem APIs and
descriptor-relative operations, not a check-then-open symlink scan. Low-level
Git commands run with repository hooks disabled and never initialize or update
submodules.

Validation is fail-closed for the complete transaction. An error identifies the
commit, record, and path and leaves local committed operations publishable after
repair. No invalid subset is partially reduced.

Hash validation proves integrity, not authorship. Normal mode treats configured
remote content like a checkout from that remote. Safe mode never executes Lua,
hooks, template commands, or newly synchronized executable content. Even in
normal mode, sync and materialization themselves never execute content; changed
executable resources take effect only on a later explicit reload/open and must
produce a visible warning.

Because deleted text and blobs remain in immutable history, documentation and
the UI warn against storing secrets. `kbrd export --clean` writes only the
current semantic board to a new directory without `.git` or immutable history;
repository replacement and remote rotation remain explicit operator actions,
outside normal sync.

## Deterministic resolution policy

| Concurrent change | Resolution |
| --- | --- |
| Different scalar fields | Apply both |
| Same scalar field set/delete | Causal descendant, else operation-ID winner |
| Tags | Observed-remove set |
| Checklist completion | Stable entry-ID register |
| Move or rename plus content edit | Apply both |
| Concurrent body insertions | Preserve all using the pinned sequence protocol |
| Concurrent body replacements | Preserve every inserted span |
| Body delete concurrent with insertion | Delete observed elements; keep insertion |
| Two card or column paths | Causal descendant, else operation-ID winner |
| Delete concurrent with edit | Edit/restore wins |
| Delete that causally observed the edit | Delete wins |
| Same requested projection path | Deterministic ID-derived collision suffixes |
| Opaque text resource conflict | Pinned deterministic three-way merge, then whole-file winner |
| Binary resource conflict | Causal descendant, else operation-ID winner |

The path collision suffix includes a stable short target-ID component and obeys
portable length, reserved-name, Unicode, and case-fold rules. The protocol
defines comparison on normalized slash paths even when the host filesystem is
case-sensitive. Unsupported host filesystems fail capability checks rather than
materializing a different tree.

Causal descendants win over ancestors. Truly concurrent register choices use
the lexicographic operation ID. The reducer emits no operations, so repeated
reduction is a pure no-op over the same input set.

## Mutation, projection, and recovery

### Local mutation transaction

Every frontend calls one concrete engine while holding the repository lock:

1. Recover an interrupted transaction.
2. Inventory affected projections and import pre-existing drift.
3. Read current target heads and validate the requested mutation.
4. Write referenced blobs and one or more immutable operations durably.
5. Reduce the new local frontier and write deterministic revision manifests.
6. Materialize affected projections with compare-and-preserve writes.
7. Atomically advance the completed-generation manifest.
8. Release the lock, then publish semantic events and trigger the coalescing
   commit/sync worker.

If the process dies before Git commit, the immutable records remain untracked or
unstaged and are recovered as local pending operations. kbrd stages only its
verified immutable records and managed projections through a private index; it
does not reuse the existing `git add -A` behavior or capture unrelated user
files.

### Projection generations

The per-worktree completed manifest contains the operation frontier, format,
and every managed path's target ID, target revision, byte hash, and supported
mode. Path history retains enough previous path/revision pairs to recognize an
open editor saving after a move.

The journal records the previous generation, desired generation, expected live
HEAD and index checksum, prepared commit/tree when applicable, expected input
path hashes, and transaction-owned backup paths.

Recovery runs under the lock before drift import. Bytes matching the completed
or journaled desired manifest are recognized as generated output. Other bytes
are preserved and imported against their embedded revision before recovery
continues. Transaction backups are removed only after their bytes are either
known generated output or durably represented by an operation; retention and
manual cleanup are exposed through `kbrd doctor`.

Materialization never blindly overwrites or deletes a path. It uses a supported
atomic exchange or backup-first rename primitive, verifies the displaced bytes
against the inventory hash, and retains unexpected bytes. Creation uses
no-clobber semantics. Deletion first renames the old path to a private backup.
If an editor races any step, the transaction imports the preserved edit,
reduces again, and retries.

The filesystem primitive set is capability-tested on each supported operating
system before automatic sync is enabled. A platform without safe no-clobber,
rename, directory-sync, and root-contained operations fails closed instead of
falling back to a racy check-then-write implementation.

Watcher events are rescan triggers only. Generated writes need no correctness-
critical event suppression; hashes and manifests distinguish output from drift.

## Synchronization transaction

One cross-process transaction is shared by all frontends:

1. Acquire the repository-wide advisory lock.
2. Recover any journaled local mutation, prepared commit, index installation,
   or projection generation. Refuse unexplained `MERGE_HEAD`, index changes, or
   user-created Git state; kbrd never aborts a merge it cannot prove it owns.
3. Inventory and import working-tree drift, reduce it, install its projection,
   and create a local commit through a private index. Record the resulting local
   tip. Unrelated index/worktree entries remain untouched.
4. Fetch the configured upstream with Git hooks disabled and record the exact
   fetched tip.
5. Find the merge base and validate unseen append-only history edges, format,
   immutable records, commit trailers, and projection consistency at both tips.
6. If either side contains invalid committed projection drift, stop with a
   repair diagnostic. Never regenerate over it.
7. Create a transaction-owned temporary index from the merge base. Populate its
   immutable paths with the byte-identical union of both tips. Merge unmanaged
   paths according to their declared policy; managed projections are omitted at
   this point because they are disposable.
8. Validate and reduce the complete candidate operation set. Write deterministic
   revision manifests and add them to the temporary index.
9. Materialize every managed projection into a private staging tree and add its
   exact bytes and modes to the temporary index.
10. Write the candidate Git tree. If histories diverged, create an ordinary
    two-parent commit with `git commit-tree`; if the local tip is an ancestor and
    the fetched commit already has an exact valid generated tree, use that
    commit as a fast-forward. No commit is created before its final tree exists.
11. Write and durably sync a journal containing old tip, candidate commit/tree,
    prepared index checksum, and desired projection manifest.
12. Acquire Git's canonical index lock, then verify that the live index checksum
    and branch tip still match the journal. While retaining the index lock,
    compare-and-swap the branch to the candidate and atomically promote the
    prepared index. A failed check or ref CAS changes neither live index nor
    worktree; discard the candidate and restart from fetch. A crash between ref
    CAS and index promotion leaves the prepared lockfile and journal for recovery.
13. Verify the branch still names the candidate, then materialize its projection
    generation into the live worktree with compare-and-preserve writes. If an
    external raw ref update raced after the CAS, do not overwrite projections;
    retain the journal and restart reconciliation from the new tip. A crash after
    ref advancement is recovered by completing or superseding these steps.
14. Atomically advance the completed-generation manifest and remove the journal.
15. Push normally. On non-fast-forward, repeat from fetch with bounded retries
    and jitter. Transport failure leaves the candidate commit and operations
    local and pending.

Cancellation stops at defined safe points. Cleanup uses a fresh bounded context
after caller cancellation. Once the branch CAS succeeds, recovery completes the
journaled generation rather than attempting to roll the branch backward.

## Status, history, and hooks

Status compares materialized frontiers rather than only worktree versus `HEAD`:

```text
base     = operation frontier at the configured upstream tip
current  = local committed and pending operations plus imported filesystem drift
```

For each stable target, the semantic differ reports new/deleted state, exact
rendered additions and deletions, old/new path, move/rename, and committed but
unpublished state. Internal `.kbrd` paths and generated metadata are hidden.

```text
deploy API   Todo -> Doing   +5 -2   pending sync
```

Markers clear only when the operation is reachable from upstream. History uses
operation causal order, informational occurrence time, source, and batch ID.
Concurrent register values not selected for the current projection remain
inspectable as recovery history. Body tombstones and replaced spans remain
inspectable without being rendered.

The reducer, validator, candidate builder, and materializer never invoke Lua or
hooks. After a local mutation and projection are durable and the lock is
released, the engine emits one semantic event per `BatchID`. Hook-requested
mutations queue a new transaction rather than entering recursively. Replaying
remote operations, recovery, and generated writes do not fire local mutation
hooks. Event delivery is at-most-once in version 1; durable exactly-once hook
delivery is not implied.

## Diagnostics and recovery commands

The storage rewrite is not releasable without operator tooling:

- `kbrd doctor` verifies format compatibility, append-only Git edges, hashes,
  graph closure, projections, local journal state, filesystem capabilities, and
  unreachable immutable objects;
- `kbrd doctor --repair-local-commit <commit>` imports an unambiguous local
  single-parent projection-only commit into a new child operation commit;
- `kbrd rebuild` regenerates projections and the local manifest from a validated
  operation frontier without changing semantics;
- `kbrd inspect-op <id>` and `kbrd inspect-card <id>` explain parents, source,
  batches, losing register values, and blob reachability;
- `kbrd backups` lists, restores, or removes transaction-owned displaced bytes;
  and
- `kbrd export --clean <dir>` writes the current semantic board without Git or
  immutable history for emergency repository rotation.

Automatic repair never rewrites published history, force-pushes, deletes
immutable objects, or guesses an ambiguous identity.

## Package boundaries

### `boardlog/`

Owns the pure protocol and board semantics:

- concrete operation and revision types;
- canonical encoding, hashing, and protocol validation;
- causal graph and append-only set validation over supplied commit metadata;
- the pinned body-sequence integration and snapshot-diff algorithms;
- deterministic reduction and semantic rendering; and
- semantic comparison between frontiers.

It receives bytes and records from callers and returns concrete results. It does
not access the host filesystem, spawn Git, hold locks, manage journals, invoke
hooks, or import Bubble Tea, Cobra, Viper, web, or scripting packages.

### `boardsync/`

Owns the concrete stateful `Engine` used by every frontend:

- immutable file storage;
- repository/worktree state resolution and advisory locking;
- inventories and external-drift translation;
- revision and projection storage;
- generation journals, recovery, and compare-and-preserve materialization;
- temporary Git indexes, append-only union, commit construction, ref CAS, and
  bounded push retry;
- the coalescing worker; and
- diagnostics, rebuild, and clean export.

`Engine` is the primary resource object. Its methods perform mutations and
return concrete semantic results. Start with concrete dependencies; introduce
small consumer-owned interfaces only when a real second implementation or test
fake requires one.

### Adapters

- `board/` retains focused Markdown/card helpers that do not own persistence.
- `boardops/` resolves frontend intent and calls `boardsync.Engine`; direct file
  writes stop being authoritative.
- `git/` retains TUI presentation but delegates synchronization and semantic
  status.
- `web/`, MCP, Lua, reminders, ingest, and notifications use the same engine.
- `commands/` contains Cobra routing for sync, doctor, rebuild, inspect, backup,
  export, and explicit format-upgrade commands.
- `fs/` contains only low-level durable filesystem and Git subprocess
  primitives, including mandatory credential redaction and hook disabling.

## Implementation phases

### Phase 0: Protocol and feasibility gates

1. Write the canonical encoding, operation payload, revision, sequence-CRDT,
   snapshot-diff, path-normalization, and append-only Git specifications.
2. Publish language-neutral golden vectors and CRDT conformance scenarios.
3. Spike the exact Git plumbing: two tips, explicit immutable union, temporary
   index, final tree, `commit-tree`, ref CAS, prepared-index installation, and
   push retry.
4. Spike compare-and-preserve create/replace/delete and crash recovery on every
   supported operating system.
5. Establish representative 10k-card/100k-operation benchmark fixtures and
   record startup, reduction, sync, memory, repository-size, and Git-object
   baselines. Set release budgets before product integration.

No frontend rewrite begins until these risks have executable proofs.

### Phase 1: Card/column CLI vertical slice

1. Implement pure `boardlog` types, validation, sequence integration, reduction,
   rendering, and semantic diff for cards and columns only.
2. Implement `boardsync.Engine` immutable storage and local transaction lock.
3. Support create, edit, move, rename, delete, and restore through a minimal
   `kbrd sync`/diagnostic CLI path.
4. Synchronize three independent clones through the complete candidate commit
   algorithm, including concurrent body and path changes.
5. Keep the old application path behind a development-only format gate; do not
   run the new log through the old sidecar synchronizer.

### Phase 2: Filesystem drift and crash safety

1. Add IDs and revisions to projections and column markers.
2. Add deterministic snapshot-to-CRDT translation and stale-editor path history.
3. Add completed manifests, journals, backups, safe filesystem primitives, and
   fault injection at every durable boundary.
4. Implement doctor, rebuild, backup inspection, and clean export.
5. Prove local pending operations survive process death before Git commit.

### Phase 3: Structured card semantics

1. Add scalar frontmatter registers, tag sets, and checklist entry state.
2. Specify and test malformed metadata, generated-key edits, duplicate IDs,
   arbitrary YAML values, and canonical rendering.
3. Add column deletion fallback behavior and portable collision rules.

### Phase 4: Frontend integration and semantic UX

1. Route TUI, web, MCP, Lua, reminders, ingest, and notification mutations
   through the engine.
2. Replace `GitDiffStats` badges and raw Git panel entries with semantic status.
3. Drive the timeline and recovery inspection from operation history.
4. Replace TUI and web synchronization orchestration with the shared engine.
5. Add explicit format-upgrade UX and actionable validation errors.

### Phase 5: Resource protocol

1. Define the resource allowlist, identity limitations, deterministic text and
   binary policies, modes, security warnings, and safe-mode behavior.
2. Add configuration and non-executable templates first.
3. Add Lua, hooks, and executable templates only after explicit trust-boundary
   tests pass.
4. Add attachment and binary-resource size/performance tests.

### Phase 6: Event-driven automatic sync and old-model removal

Run one coalescing worker triggered by startup, local append, imported drift,
network recovery, and a low-frequency fallback timer. Multiple triggers during
an active transaction collapse into one next pass. Shutdown either completes a
safe point or leaves a recoverable journal.

Only after all release gates pass, delete sidecar resolution, conflict-review
UI, attended/manual merge policy, auto-commit configuration, duplicate web Git
orchestration, snapshot-only fixtures, and obsolete tests/documentation.

## Verification and release gates

### Protocol conformance

- Go encoding matches every language-neutral operation/revision golden vector.
- Unknown, duplicate, noncanonical, or reordered set data is rejected.
- Every CRDT fixture produces exact element order, tombstones, revision index,
  and Markdown bytes.
- Historical supported revisions translate the same external snapshot into the
  same operations.
- Mixed reducer/diff/format versions fail before mutation.

### Append-only and Git properties

- A deleted leaf operation is rejected even when no child references it.
- Every candidate contains the byte-identical union of both parents' immutable
  trees.
- A same-path/different-byte immutable collision is rejected.
- Raw committed projection drift is reported and never overwritten.
- The candidate commit is created only after its final generated tree exists.
- Ref CAS failure leaves the live index/worktree unchanged.
- A crash after ref CAS recovers by completing the journaled index and
  projection generation.
- Repeating successful sync creates no commit.
- Another clone advancing upstream during every push attempt eventually
  converges within bounded retries or leaves clear pending state.

### Reducer properties

- Same valid operation set and pinned format produces a byte-identical tree for
  every load order.
- Duplicate record loading has no effect.
- Reduction never creates operations.
- Body integration is associative, commutative, and idempotent over every valid
  operation set.
- Every inserted element remains visible until a causally later delete names it.
- Concurrent identical insertions from the same or different actors remain
  distinct spans.
- Revision manifests round-trip to exact target heads, semantic blob, and
  element index.
- Unrelated target operations do not change a card revision.

### Concurrent scenarios

Exercise at least three clones for insert/insert, nested origins,
replace/replace, delete/insert, move/edit, stale-save-after-move, move/move,
delete/edit in both causal directions, create/create path collisions, copied and
duplicated IDs, column rename/delete, scalar/tag/checklist conflicts, and later
resource conflicts. Every compatible clone must converge without sidecars or a
content-review inbox.

### Crash, cancellation, and containment

- Fault injection covers every blob/operation/revision write, backup/exchange,
  projection create/replace/delete, manifest advance, fetch, tree write, commit
  creation, ref CAS, index install, and push boundary.
- An editor racing every materialization boundary is preserved and imported.
- Unexplained Git state is never aborted or overwritten.
- Absolute, traversal, reserved, symlink, case-fold, mode, and size violations
  cannot write outside the root or private transaction state.
- Cancellation never reuses its canceled context for mandatory cleanup.

### Security

- Every Git mutation disables repository hooks; clone/fetch does not initialize
  submodules.
- Safe mode never executes synchronized content.
- Normal sync does not execute newly fetched content as a side effect.
- Executable-resource changes are visible before an explicit reload/open.
- Clean export contains current semantic content but no tombstones, losing
  values, deleted blobs, Git metadata, or local state.

### Performance

- The Phase 0 benchmark corpus stays within the agreed startup, incremental
  reduction, sync, memory, and repository-growth budgets.
- Checkpoints are local, versioned caches and never convergence inputs.
- Cache deletion changes performance only, never results.
- Fan-out directories stay bounded for the benchmark corpus.
- Benchmarks include cold clone, cold validation, cached validation, one-card
  edit, large paste, and three-way sync.

### User-visible behavior

- Built-in and external editing remain file-oriented.
- `+N/-N`, new, deleted, moved, renamed, and pending markers remain correct
  before and after automatic local commit.
- Selection survives materialization and reload.
- Generated metadata and immutable files do not pollute normal diffs/history.
- Validation errors identify a repair action rather than exposing a partial
  board.
- No valid concurrent card change creates a sidecar or mandatory review flow.

## Non-goals

The first implementation will not:

- migrate or read an existing production board in place;
- provide real-time cursor or presence synchronization;
- implement a Markdown-AST CRDT beyond structured frontmatter and checklist
  state;
- infer identity for an external resource rename that also changes bytes;
- coordinate through a central always-online service;
- authenticate operation authors independently of Git remote trust;
- use per-device branches, Git notes, remote locks, force pushes, or rebases in
  normal synchronization;
- garbage-collect or compact published immutable history; or
- guarantee safe behavior for raw `git pull`, raw merge commits, or committed
  projection edits that bypass kbrd's importer.

History compaction or snapshot branches may be considered only after the
append-only implementation has proven convergence and acceptable performance.
Emergency secret removal uses clean export and repository rotation rather than
being disguised as normal garbage collection.
