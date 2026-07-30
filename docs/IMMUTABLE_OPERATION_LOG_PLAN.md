# Immutable Operation Log Plan

## Goal

Replace snapshot-oriented Git conflict handling with an immutable operation log
that converges automatically across machines. Git becomes the transport,
durability, and history layer; kbrd owns card semantics and deterministic
resolution.

The user should continue working with ordinary Markdown cards and retain useful
`+N/-N`, new, deleted, renamed, moved, and pending-sync indicators. Concurrent
changes must never create conflict sidecars or require a review inbox.

This is a breaking storage rewrite. There is no production migration,
dual-write compatibility period, legacy format reader, or rollback command.
Development boards and fixtures will be recreated in the new format.

## Product decisions

1. A card has a stable UUID independent of its filename and column.
2. Immutable operations and content-addressed blobs are the source of truth.
3. Markdown card files remain normal, editable, tracked projections.
4. Projection conflicts are disposable: after merging operations, kbrd
   deterministically regenerates the affected Markdown files.
5. The same operation set must always produce a byte-identical board.
6. Concurrent changes are resolved automatically using card-aware rules.
7. Body text inserted or supplied as a replacement by a concurrent operation
   must remain present in the current materialized card. It may disappear only
   after a causally later operation observes and explicitly deletes it.
8. Git uses one shared branch with ordinary commits, fetches, merges, pushes,
   and bounded non-fast-forward retries. It never rebases or force-pushes.
9. TUI, web, MCP, scripts, and filesystem edits use the same mutation and sync
   engine.
10. Wall-clock timestamps are informational. They are not the sole ordering or
    conflict-resolution mechanism.

## Repository layout

```text
.kbrd/
  format.json
  log/
    <operation-id>.json
  blobs/
    <content-hash>
  revisions/
    <revision-id>.json
  checkpoints/
    <reducer-version>/
      <frontier-hash>.json

Todo/
  task.md
Doing/
  other.md
```

Machine-local manifests, journals, locks, temporary indexes, and displaced-file
backups live in a private state path resolved through Git rather than in the
tracked `.kbrd/` tree. They are never staged or used as convergence inputs.

Files below `.kbrd/log/`, `.kbrd/blobs/`, and `.kbrd/revisions/` are immutable.
Their names derive from canonical content, so independent machines that produce
the same logical record also produce the same path and bytes. Checkpoint paths
include the reducer version because the same operation frontier may materialize
differently after a reducer upgrade. Checkpoints are disposable caches and are
never authoritative.

Markdown projections carry stable identity metadata:

```yaml
---
kbrd_id: 019c0000-0000-7000-8000-000000000001
kbrd_revision: sha256:8f17...
---
```

The revision token is the content hash of an immutable revision manifest. A
manifest contains the target identity, reducer version, sorted operation heads,
semantic Markdown blob hash, and rendered path. The semantic blob excludes the
generated `kbrd_revision` token and other projection-only metadata, avoiding a
self-referential hash. The materializer injects those values only after hashing
the revision manifest; exact final projection-byte hashes live in the local
materialization manifest. The revision therefore identifies the exact causal and
textual base of a stale external-editor save even when the card had multiple
concurrent heads. Revision manifests are retained with history and must not rely
on an optional checkpoint for reconstruction. Generated kbrd metadata must not
participate in user-facing diffs or user-content blobs.

## Operation model

The canonical operation payload should contain:

```go
type Operation struct {
	Version int
	ActorID string
	Target  Target
	Parents []string
	Kind    Kind
	Blob    string
	Path    string
	Data    map[string]json.RawMessage
}

type Target struct {
	Type TargetType // card or resource
	ID   string     // stable UUID
}

// Record is an in-memory value. It is not itself canonically hashed.
type Record struct {
	ID string
	Operation
}
```

- An operation ID is the SHA-256 hash of the canonical encoding of `Operation`.
  It is stored in the filename and supplied separately as `Record.ID`; the ID is
  never included in the bytes from which it is derived.
- `ActorID` is machine-local and is never inferred from hostname or wall time.
- `Target` distinguishes cards from other mutable board resources. Both have a
  stable identity independent of presentation path.
- `Parents` contains operation IDs for the same target that were visible when
  the operation was created and supplies causal ordering.
- `Blob` references a complete derived Markdown revision or opaque file content
  when required; body-edit authority remains in CRDT operations.
- `Path` is presentation state, not card identity.
- `Data` carries kind-specific structured values, including resource media type
  and executable-bit state where applicable.

Canonical JSON must define UTF-8 handling, object-key ordering, number encoding,
newline normalization, omitted versus null fields, and set ordering. Writers
write a blob durably before any operation that references it, then write the
operation with create-only semantics.

Card bodies use a sequence CRDT stored as immutable operations. Complete
semantic Markdown revisions remain useful for external-editor merge bases,
history, checkpoints, and inspection, but they are derived views rather than the
authoritative representation of body edits. Opaque resources continue to use
complete content blobs and deterministic three-way or whole-file resolution.

### Body sequence model

The body CRDT covers user-authored Markdown body text after frontmatter and
generated projection metadata are separated. Checklist completion markers are
structured entry state, while checklist labels use the same CRDT text semantics
as the rest of the body. The sequence operates on Unicode scalar values grouped
into immutable insertion spans:

- a body-insert operation names the visible left and right element anchors at
  its base revision and carries the inserted UTF-8 text;
- each inserted element has the stable identity `(operation ID, element offset)`;
- a body-delete operation names observed element IDs or compact contiguous ID
  ranges and cannot delete elements that were not visible at its base revision;
- replacement is represented as deletion of the observed elements plus an
  insertion span, so two concurrent replacements preserve both inserted values;
- concurrent insertions at the same anchors are ordered by operation ID and
  element offset, never arrival order; and
- tombstoned elements remain addressable so later operations with old anchors
  still reduce deterministically.

External saves are compared with the exact `kbrd_revision` base. A versioned,
deterministic diff translates the saved snapshot into insert and delete
operations over that base's element IDs. Concurrent inserted spans are never
text-deduplicated, even when their bytes are identical: two independently added
lines remain two distinct lines. A later save that has observed both lines may
explicitly delete either or both.

Resource targets cover configuration, Lua, hooks, templates, and other text or
binary files. The materialized-generation manifest maps each resource ID to its
last path, blob, and mode so external moves and copies can be distinguished even
when the file format cannot carry inline metadata. A copied resource receives a
new ID; a rename preserves it. Resource operations define create, replace,
rename/move, mode change, and delete semantics using the same parent rules as
cards. This avoids leaving non-card mutations on a separate Git-conflict path.

### Validation and trust boundary

Every local or fetched record must be validated before graph construction or
materialization:

- the log filename must equal the hash of the canonical operation bytes;
- every referenced blob and revision must exist and match its content hash;
- versions, kinds, target types, UUIDs, required fields, and size limits must be
  valid;
- parents must exist, address the same target, and form a valid causal graph;
- body anchors and element ranges must exist on the same card with valid scalar
  offsets, and every deleted element must be causally visible from the
  operation's declared parents;
- projection paths must be normalized relative paths contained by the board,
  must not address `.git`, `.kbrd`, or another reserved path, and must not
  traverse symlinked parent components; and
- resource modes must be limited to supported portable values.

Validation is fail-closed for the entire synchronization transaction. Unknown
protocol versions or invalid remote records discard the candidate merge before
the live branch or projection generation advances; kbrd never partially reduces
or materializes an invalid operation set. This is a sync error, not a content
conflict requiring a review inbox.

## Deterministic resolution policy

| Concurrent change | Resolution |
| --- | --- |
| Different card fields | Apply both |
| Move or rename plus content edit | Apply both |
| Tags | Observed-remove set |
| Checklist entries | Merge by stable entry ID carried in projection metadata |
| Concurrent body insertions | Preserve all in deterministic CRDT order |
| Concurrent body replacements | Preserve every replacement's inserted text |
| Body delete concurrent with insertion | Delete observed elements; keep insertion |
| Two moves or renames | Deterministic winning operation |
| Delete concurrent with edit | Edit wins |
| Delete that causally observed the edit | Delete wins |
| Opaque text resource conflict | Three-way merge, then deterministic whole-file winner |
| Binary resource conflict | Deterministic whole-file winner |

Causal descendants win over their ancestors. Truly concurrent choices use a
stable total order derived from operation identity, never the local/remote role
or merge direction. That ensures two machines make the same choice.

When useful, the reducer may emit a synthetic merge operation referencing all
competing heads. Its ID must derive from the reducer version, sorted parent IDs,
and canonical result so independent machines generate exactly the same record.
Synthetic operations use a reserved deterministic actor value and contain no
machine-local or wall-clock fields.

Checklist projections carry an invisible HTML comment containing a stable entry
UUID. New checklist lines without an ID receive one during drift import. If an
editor removes an existing ID, the importer treats the old entry as removed and
the unmarked line as a newly added entry; it does not guess identity from text or
position. Duplicate entry IDs are resolved like copied card IDs, with one
deterministically selected occurrence retaining the ID. Checklist metadata is
excluded from user-facing diffs.

## User experience

### Editing files

Users continue editing files such as `Todo/task.md`. A genuine filesystem
change is converted into an operation before synchronization. Materializer
writes are atomic and tagged/suppressed so the watcher does not interpret them
as user edits.

The built-in editor supplies the exact parent revision directly. For an
external editor, `kbrd_revision` identifies the base even when synchronization
updated the projection while the editor buffer was open.

### Projection generations and crash recovery

kbrd keeps a machine-local, untracked materialization manifest and transaction
journal in a Git-resolved private state directory. The completed manifest names
the last fully materialized frontier and records every managed path's target ID,
revision ID, and byte hash. The journal records the previous generation, desired
generation, expected input hashes, and transaction-owned backup paths.

Recovery always runs while holding the repository lock and before ordinary drift
import. If a transaction was interrupted, bytes matching either the completed or
desired manifest are recognized as kbrd output. Any other bytes are preserved as
blobs and imported against their embedded revision before materialization
resumes. This prevents a partial create, move, rename, or delete from becoming a
spurious inverse user operation. After all projection files and containing
directories are durable, kbrd atomically advances the completed manifest and
then removes the journal.

Materialization never blindly replaces a path. Drift import records the exact
hash or absence observed at every affected path. Each replacement or deletion
uses a no-clobber/exchange-and-backup primitive that preserves the displaced
bytes, verifies them against the expected hash, and retains unexpected bytes
until they have been stored as a user operation. If a path changed after the
scan, the transaction stops, imports the preserved edit, reduces again, and
retries. This compare-and-preserve rule covers editors that do not participate
in the repository lock and is required for file creation and deletion as well as
replacement.

### Moves, renames, copies, and deletes

- The same card UUID appearing at a different path becomes a move/rename.
- A content edit on another machine commutes with that move.
- A duplicate UUID at two paths is treated as a copy; one projection receives a
  fresh card UUID.
- Watcher events are only rescan triggers, never authoritative mutations. A
  missing projection becomes a delete only after a debounced full inventory is
  compared with the completed materialization manifest and no projection with
  the same UUID exists at another path.
- Concurrent create operations that want the same path receive deterministic
  collision suffixes so both cards remain visible.

### Change markers

Markers compare two materialized operation frontiers instead of only comparing
the worktree to `HEAD`:

```text
base     = upstream operation frontier
current  = local operation frontier plus pending filesystem edits
```

For each card UUID, the semantic differ reports:

- new or deleted;
- exact rendered Markdown additions and deletions;
- previous and current column/path;
- renamed or moved;
- committed locally but not yet published.

For example:

```text
deploy API   Todo -> Doing   +5 -2   pending sync
```

Markers clear only when the corresponding operations reach upstream, not when
the automatic local commit is created. The Git panel should group internal log
records into these semantic card/resource changes and hide `.kbrd/log` noise.

### History

The existing card timeline should render operation history. Body inserts and
replacements remain in the current document unless causally deleted. Concurrent
values discarded by non-body policies, such as competing paths or opaque
resources, remain inspectable as recovery history, but there is no conflict
notification or required review action.

### Scripts and hooks

The reducer and materializer never invoke Lua or hooks. Semantic events for a
local mutation are delivered only after its operation and projection generation
are durable and the repository transaction lock has been released. A mutation
requested by a Lua callback or hook starts or queues a new transaction rather
than recursively entering the active one. Direct filesystem writes made by a
script are imported by the normal inventory pass. Replaying remote operations or
materializer writes does not re-fire local mutation hooks, preventing feedback
loops.

## Package boundaries

### `boardlog/`

Owns the storage format and board semantics:

- atomic operation and blob storage;
- canonical encoding and hashing;
- validation of local and fetched records;
- causal graph construction;
- deterministic reduction;
- immutable revision manifests;
- Markdown materialization;
- projection-generation recovery;
- semantic comparison between frontiers; and
- optional checkpoints for faster startup.

It must not import Bubble Tea, Cobra, Viper, web, or Git orchestration packages.
Start with concrete types and introduce narrow consumer-owned interfaces only
where tests or multiple implementations require them.

### `boardsync/`

Owns one synchronization transaction shared by every frontend:

1. Acquire a repository-wide local lock.
2. Recover or roll forward any interrupted materialization and discard any
   transaction-owned temporary Git state. Abort a live Git merge only when the
   journal proves kbrd created it; refuse to touch an unexplained user merge.
3. Inventory projections against the completed-generation manifest, import
   genuine drift as immutable operations, and retain the observed path hashes.
4. Validate and reduce the local operation closure, write immutable revision
   manifests, and install the local projection generation with the
   compare-and-preserve protocol. If a path raced the inventory, preserve and
   import it and repeat from step 3.
5. Commit the new records and the verified installed projection generation.
6. Fetch the configured upstream and record the local and upstream tips in the
   transaction journal.
7. Construct the candidate Git merge in a transaction-owned temporary index or
   worktree. Git must never write merge results or conflict markers into the live
   projection tree. The candidate produces an ordinary two-parent merge commit
   when histories diverge and a fast-forward when they do not.
8. Validate the complete candidate operation closure. Invalid or unsupported
   records discard the candidate without changing the live branch or completed
   projection generation.
9. Reduce the candidate operation set, write immutable revision manifests, and
   replace all managed entries in the candidate index with the generated tree.
10. Install that tree in the live worktree using compare-and-preserve writes. If
    an editor raced the transaction, preserve and import its bytes, commit the
    resulting local operation, discard the candidate, and repeat from fetch.
11. After verifying that the recorded local branch tip has not changed, install
    the prepared index/commit and advance the branch with compare-and-swap. Then
    atomically advance the completed-generation manifest and remove the journal.
12. Push normally. On non-fast-forward, repeat from fetch with bounded retries
    and jitter.

A transport failure leaves local operations committed and pending. The
background worker retries them later. Ordinary cancellation or failure cleans
up temporary Git state and releases the application lock. Process death may
leave a journaled candidate or partial projection generation, but the next lock
holder must recover it before accepting mutations or importing drift; it must
never continue with an unexplained `MERGE_HEAD`, stale index, branch-tip change,
or projection state.

### Adapters

- `board/` and `boardops/` call `boardlog` mutations rather than treating direct
  file writes as authoritative.
- `git/` retains the TUI panel/controller but delegates synchronization and
  semantic change calculation.
- `web/` delegates mutation persistence and synchronization to the same engine.
- `commands/` contains only Cobra argument/flag routing for any new diagnostic
  or explicit retry command.
- `fs/` retains low-level Git execution and durable filesystem primitives.

## Implementation phases

### Phase 1: Protocol and pure reducer

1. Specify canonical JSON, content normalization, operation and revision
   hashing, target/resource identity, body-element IDs and anchors, operation
   kinds, validation limits, and reducer versioning.
2. Implement the immutable `Store`, body sequence CRDT, `Validator`, `Reducer`,
   `Materializer`, and semantic `Differ` in `boardlog/`.
3. Implement automatic merge policies for card fields, paths, body insertion
   spans and tombstones, checklist IDs, deletion, modes, and opaque resources.
4. Prove permutation invariance: every ordering of the same valid operation set
   must produce the same tree and revision manifests.

This phase has no TUI or Git integration. It is the highest-risk foundation and
must be complete before wiring mutations.

### Phase 2: Make mutations log-first

1. Replace create, edit, move, rename, delete, frontmatter, paste, and template
   workflows with log-first operations.
2. Route web, MCP, Lua, reminders, ingest, and notification actions through the
   same mutation API.
3. Add stable UUIDs to generated projections.
4. Convert external filesystem edits, moves, copies, and deletes into
   operations, including deterministic snapshot-to-CRDT translation against the
   exact revision base.
5. Add the completed-generation manifest, transaction journal, and
   compare-and-preserve materialization primitives.
6. Ensure materializer writes do not feed back into the watcher and that
   watcher events trigger inventory rather than directly implying operations.

### Phase 3: Semantic status and history

1. Materialize local and upstream frontiers.
2. Calculate card-aware additions, deletions, moves, and publication state.
3. Replace the existing `GitDiffStats`-driven card badges.
4. Replace raw file entries in the Git panel with semantic card/resource
   changes.
5. Drive the timeline from immutable revision history.

### Phase 4: Shared synchronization engine

1. Implement the repository transaction lock, interrupted-candidate recovery,
   temporary Git index/worktree, branch compare-and-swap, and bounded retry loop.
2. Merge and validate immutable operations outside the live worktree, then
   regenerate projections with generation journaling and compare-and-preserve
   writes.
3. Replace TUI manual/startup/periodic sync orchestration.
4. Replace the web syncer's separate commit/pull/push implementation.
5. Add an explicit `kbrd sync` command as "retry now," not as a separate merge
   policy.

### Phase 5: Event-driven automatic sync

Run one coalescing worker triggered by:

- startup;
- every local operation append;
- external edit, move, rename, copy, or delete;
- network recovery; and
- a low-frequency fallback timer.

Multiple triggers during an in-flight transaction collapse into one subsequent
pass. Shutdown waits for or safely cancels the active transaction.

### Phase 6: Remove the old model

Delete:

- merge-with-sidecar resolution;
- conflict-copy parsing and filesystem actions;
- conflict review UI and status;
- attended/manual sync mode;
- auto-commit configuration;
- duplicate web Git orchestration;
- old behavior tests and documentation; and
- demo fixtures based on the snapshot-only format.

## Verification and release gates

### Reducer properties

- Same operation set produces a byte-identical tree regardless of load order.
- Applying an operation more than once has no effect.
- Repeated reduction creates no additional operations.
- Every submitted content blob remains reachable from history.
- Every submitted body insertion remains represented by its immutable operation,
  including after a causally later deletion tombstones it.
- Synthetic merges are byte-identical on independent machines.
- Body insert/delete reduction is associative, commutative, and idempotent for
  every valid operation set.
- Every inserted body element remains visible until a causally later delete
  explicitly names that element; a concurrent delete can never remove it.
- Concurrent identical insertions remain distinct CRDT spans and materialize
  twice in deterministic order.
- Operation and revision IDs round-trip through canonical encoding and reject
  any filename/content mismatch.
- Every emitted `kbrd_revision` resolves to the exact reducer version, sorted
  heads, rendered blob, and path used as its merge base.

### Concurrent scenarios

Exercise at least three independent clones for:

- insert/insert at different and identical anchors;
- replace/replace of the same body range, preserving both replacement texts;
- delete/insert and replace/insert at the same body range, preserving every
  concurrently inserted span;
- move/edit and rename/edit;
- move/move and rename/rename;
- delete/edit in both causal directions;
- create/create with the same requested filename;
- duplicate/copy detection;
- frontmatter, checklist, and tag conflicts;
- checklist edits with removed, duplicated, and reordered entry-ID metadata;
- config, Lua, template, and binary resource conflicts; and
- repeated push races where another clone advances the remote between fetch and
  push.

All clones must eventually produce the same board without conflict files or
manual intervention.

### Crash and cancellation safety

- Partial operation or blob files are never observable.
- Fault injection at every operation-write, projection-write, rename/delete,
  manifest-advance, Git-merge, and commit boundary recovers to the authoritative
  operation frontier without generating inverse drift operations.
- A partially installed projection generation contains only bytes described by
  the completed or journaled desired manifest; recovery finishes it before
  normal drift import.
- Interrupted Git commands are either cleaned up immediately or identified by
  the journal and recovered by the next lock holder.
- An external editor save racing every materialization boundary is either left
  at its path or preserved as a blob and imported; it is never overwritten
  without preservation.
- Network failures preserve local operations and retry automatically.
- Repeating a successful sync is a no-op and creates no commits.

### Validation and containment

- Reject altered operation filenames or bytes, altered blobs, missing parents,
  cross-target parents, invalid or causally invisible body element references,
  unknown versions or kinds, oversized records, and malformed revision
  manifests before reduction.
- Reject absolute paths, traversal, reserved `.git`/`.kbrd` paths, symlink-parent
  escapes, and unsupported file modes without writing outside the transaction
  journal.
- An invalid candidate is discarded without advancing the live branch or
  completed projection generation and leaves locally committed operations
  publishable after the remote is repaired.

### User-visible behavior

- Built-in and external editing remain file-oriented.
- `+N/-N`, new, deleted, moved, and renamed markers remain correct before and
  after the automatic local commit.
- Pending markers clear only after publication.
- Card selection survives materialization and sync reloads.
- The Git panel and timeline contain no internal operation-file noise.
- Every concurrent body insertion or replacement is visible on every converged
  clone until a later edit that observed it explicitly deletes it.
- No normal concurrent change opens a conflict review workflow.

## Non-goals

The first implementation will not:

- migrate or read existing production boards;
- provide real-time cursor or presence synchronization;
- implement a Markdown-AST CRDT or semantic merge for arbitrary Markdown
  constructs beyond structured frontmatter and checklist state;
- coordinate through a central always-online service;
- use per-device branches, Git notes, remote locks, force pushes, or rebases;
- garbage-collect immutable history; or
- guarantee conflict-free behavior for raw `git pull` before kbrd has configured
  or executed its projection-aware synchronization.

History/tombstone compaction and a separately published snapshot branch can be
considered after the immutable-log implementation has proven convergence and
acceptable performance.
