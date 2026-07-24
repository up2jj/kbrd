# Mail Ingestion Plan

## Goal

Allow a user to create cards by sending email from a phone without exposing a
kbrd listener to the internet. The mailbox is the durable queue: kbrd connects
outbound over IMAPS, converts eligible messages into cards, records local
idempotency state, and exits.

The first implementation is a manually or externally scheduled one-shot
command:

```bash
KBRD_MAIL_HOST=imap.mailbox.org \
KBRD_MAIL_USER=capture@example.org \
KBRD_MAIL_PASSWORD='app-password' \
kbrd --safe ingest mail --board work --column Inbox
```

`--safe` is recommended for unattended imports because it prevents an incoming
message from indirectly triggering board-local hooks or other executable board
configuration. Users who intentionally want normal `item_created` hooks can
omit it.

## Non-goals

The first version will not:

- run an inbound SMTP, HTTP, or webhook server;
- send mail or reply to messages;
- continuously run as a daemon or use IMAP IDLE;
- support OAuth provider setup flows;
- import or save attachments;
- synchronize edits or deletions back to mail;
- interpret message content as commands, Lua, templates, or frontmatter;
- commit or push Git changes automatically; or
- attempt to be a general-purpose mail client.

Mail import should follow the existing `kbrd ingest` Git behavior: it creates a
card in the working tree. Existing TUI Git configuration and explicit user
actions remain responsible for commits and synchronization.

## User experience

### Capture

A user sends a message to a dedicated mailbox:

```text
To: capture@example.org
Subject: Buy train tickets

Check Warsaw–Prague connections for September.
```

The next import creates a card similar to:

```text
Inbox/buy-train-tickets.md
```

```markdown
---
created_at: "2026-07-24T10:20:00Z"
source: "mail"
source_id: "sha256:..."
mail_from: "me@example.org"
mail_received_at: "2026-07-24T10:19:31Z"
---

Check Warsaw–Prague connections for September.
```

The subject becomes the generated card name. An empty subject falls back to
`Email from <sender> <timestamp>`. Names pass through the same
`board.SanitizeGeneratedName` path as `kbrd ingest`. Mail imports use a
collision policy that tries a numeric suffix rather than overwriting an
existing card. Direct `kbrd ingest` retains its current duplicate-name error.

The first version prefers the first `text/plain` MIME part. If a message has
only HTML, a conservative HTML-to-text fallback preserves visible text and
links without retaining active markup. Multipart alternatives, quoted text,
and Unicode headers must be decoded correctly. Signatures and quoted replies
are preserved; automatic trimming is too subjective for the initial release.

Attachments are not downloaded. If the MIME structure advertises attachments,
the card ends with a short note listing sanitized filenames and sizes when
available.

### Command surface

Turn the existing executable `ingest` command into a parent that retains its
current `RunE` behavior and adds a `mail` child:

```text
kbrd ingest ...       # unchanged direct text ingestion
kbrd ingest mail ...  # IMAP ingestion
```

Proposed initial flags:

| Flag | Meaning |
| --- | --- |
| `--board` | Required board path or recent-board name, matching `kbrd ingest` |
| `--column` | Destination column; overrides config and defaults to `Inbox` |
| `--mailbox` | Remote mailbox name, default `INBOX` |
| `--since` | Maximum message age, default `7d` on first use |
| `--limit` | Maximum messages examined per run, default `50` |
| `--unread-only` | Consider only messages without `\\Seen` |
| `--from` | Repeatable exact sender-address filter |
| `--dry-run` | Fetch and show the plan without cards, state, flags, or moves |

There is deliberately no `--password` flag because command lines leak through
shell history and process inspection. Connection settings use environment
variables in the first release:

| Variable | Required | Default |
| --- | --- | --- |
| `KBRD_MAIL_HOST` | yes | none |
| `KBRD_MAIL_PORT` | no | `993` |
| `KBRD_MAIL_USER` | yes | none |
| `KBRD_MAIL_PASSWORD` | yes | none |

The password should be a provider-issued app password for a dedicated capture
mailbox. Normal account passwords should be discouraged. The implementation
must never print credentials, include them in errors, serialize them to state,
or place them in `kbrd.toml`.

The command must copy `KBRD_MAIL_PASSWORD` into its connection options and
immediately unset it from the process environment before loading or executing
any board-local hook. Otherwise an enabled hook subprocess could inherit the
mail credential. The concrete IMAP client receives the copied secret directly;
it must not recover it from the ambient environment.

Provider-specific OAuth2 can be added later behind an authentication interface.
It is not required to validate the generic IMAP workflow.

### Output and exit status

Human-readable output follows the existing headless integration style:

```text
IMPORT  1942  Inbox/buy-train-tickets.md
SKIP    1941  already imported
SKIP    1940  sender not allowed
mail: imported 1, skipped 2, failed 0
```

Authentication, TLS, mailbox-selection, and state-corruption errors are fatal.
A malformed individual message is reported and skipped so later messages can
still import. The command returns a non-zero status after processing if any
message failed, while retaining successfully created cards and their state.

## Configuration

Only portable, non-secret policy belongs in `kbrd.toml`:

```toml
[ingest.mail]
column = "Inbox"
allowed_senders = ["me@example.org"]
max_body_bytes = 262144
```

Proposed typed configuration:

```go
type MailIngestConfig struct {
	Column         string
	AllowedSenders []string
	MaxBodyBytes   int64
}

type IngestConfig struct {
	CreatedAtFormat string
	Mail            MailIngestConfig
}
```

Defaults must be registered explicitly in `config.Load` so environment/config
decoding remains deterministic:

```text
ingest.mail.column = "Inbox"
ingest.mail.allowed_senders = []
ingest.mail.max_body_bytes = 262144
```

An empty sender list accepts all senders but emits a warning. Sender filtering
is spam reduction, not authentication: the `From` header can be spoofed.
Because imported content is inert data and a dedicated mailbox is recommended,
this is an acceptable default for the first version.

Connection host, username, and password are intentionally not TOML keys. This
avoids committing personal identifiers and credentials with a board. A later
machine-local account file may live under the user configuration directory,
but must remain separate from the merged board configuration.

## Architecture

### Package layout

```text
commands/
├── ingest.go              # existing direct-ingest command
└── ingest_mail.go         # Cobra flags, env resolution, output

ingest/
├── ingest.go              # extracted card-ingestion operation
└── ingest_test.go

mailingest/
├── service.go             # import orchestration and state transitions
├── imap.go                # concrete TLS IMAP adapter
├── message.go             # MIME selection and normalization
├── state.go               # durable local idempotency ledger
├── lock.go                # per-account/board process and file lock
├── types.go
└── *_test.go
```

`commands` remains a routing layer. It resolves flags and environment variables,
constructs typed options, supplies command input/output, and invokes the
service. Neither `ingest` nor `mailingest` imports Cobra or Viper.

Extract the reusable portion of `commands/runIngest` into package `ingest`
before adding IMAP. It should own:

- board and column resolution;
- generated-name sanitization with an explicit collision policy (`error` for
  direct ingestion and `suffix` for mail);
- `created_at`, `source`, and `source_id` frontmatter;
- atomic no-clobber card creation; and
- a typed result containing the final board, column, name, and path.

The command layer remains responsible for printing and for constructing the
`item_created` hook runner. Direct ingestion and mail ingestion both use the
extracted operation, so they cannot drift in naming or metadata behavior.

`mailingest` owns remote-message selection, MIME parsing, idempotency, limits,
and acknowledgement policy. It consumes a narrow card-writer interface defined
where it is used:

```go
type CardWriter interface {
	Create(context.Context, CardRequest) (CardResult, error)
}

type HookRunner interface {
	ItemCreated(context.Context, CardResult) []error
}
```

Production wiring adapts package `ingest` to `CardWriter` and the existing hook
dispatcher to `HookRunner`. Under root `--safe`, the command injects a no-op
runner. Keeping the two operations separate lets the service durably record
card creation before starting any external hook side effect. Tests use small
fakes; the package does not need a mock framework.

### IMAP dependency

Go's standard library parses RFC 5322 messages but does not implement IMAP.
Use a maintained IMAP client rather than implementing the protocol. During the
dependency spike, compare the stable `github.com/emersion/go-imap` v1 API with
`github.com/emersion/go-imap/v2`; v2 is currently pre-v1/beta and should only be
selected if its required fetch, UID, MIME body-section, and cancellation paths
are demonstrably reliable. Pin an exact module version and keep it behind the
small internal client interface so it can be replaced without affecting the
workflow.

Use the standard `net/mail`, `mime`, and `mime/multipart` packages where
possible. Use `golang.org/x/net/html` for the HTML-to-text fallback and promote
`golang.org/x/net` from a transitive to a direct module requirement. Do not
introduce a large mail framework or an HTML renderer.

### IMAP client boundary

The workflow needs only a small subset of IMAP:

```go
type Client interface {
	Select(context.Context, string) (MailboxInfo, error)
	Search(context.Context, SearchQuery) ([]UID, error)
	FetchMeta(context.Context, []UID) ([]MessageMeta, error)
	FetchPart(context.Context, UID, PartSpecifier, int64) (io.ReadCloser, error)
	Close() error
}
```

The concrete adapter must:

- use implicit TLS on port 993 in version one;
- verify the server certificate and hostname;
- set connect, command, and total-run deadlines;
- authenticate only after TLS is established;
- address messages by UID, never unstable sequence number;
- use `BODY.PEEK` so inspection does not implicitly mark mail as read;
- fetch MIME structure before bodies and download only the selected text part;
- reject a part whose advertised size exceeds the limit, then enforce the same
  limit on the streaming reader before allocation; and
- close/logout on cancellation without leaking goroutines.

There will be no insecure TLS flag. STARTTLS on port 143 can be considered
later if a legitimate provider requires it.

## Message selection and mapping

### Initial selection

On every run:

1. Select the configured mailbox and record its `UIDVALIDITY`.
2. Recover any non-complete ledger records by fetching their UIDs directly.
3. Search by server-side `SINCE` and optional unread/sender constraints.
4. Sort candidates oldest first so the queue is stable and predictable.
5. Exclude messages already completed in local state.
6. Apply `--limit` before downloading bodies.
7. Fetch headers and MIME structure for the remaining candidates.
8. Produce a dry-run plan or import each candidate sequentially.

Sequential writes are intentional. Typical capture volume is small, ordering is
useful, and it avoids unnecessary races around card naming, hooks, and state.

On the first run, `--since=7d` prevents accidentally importing an entire old
mailbox. After state has been initialized, the service may use its last-seen UID
as an optimization, but correctness must continue to come from UID identity and
the durable ledger. A future explicit `--all` flag may remove the first-run
window.

IMAP `FROM` matching is provider-defined substring matching, so a server-side
sender condition is only an optimization. Always parse the fetched `From`
header and apply the exact normalized-address allowlist locally before creating
a card. `--since` needs a small age parser supporting `d` in addition to Go's
standard duration units; convert it to an IMAP calendar date in UTC.

### Identity

An IMAP UID is unique only within a `(host, user, mailbox, UIDVALIDITY)` scope.
The canonical source identity is therefore a hash of:

```text
normalized host | normalized user | mailbox | UIDVALIDITY | UID
```

Store the hash as `source_id` in the card and the complete components in the
machine-local ledger. Preserve the RFC `Message-ID` for diagnostics when
present, but never use it as the sole key because it can be absent or duplicated.

### Card metadata

Mail headers are untrusted. Decode and normalize them before use:

- parse the sender with `net/mail` and retain only the normalized address;
- decode RFC 2047 encoded subjects;
- reject control characters and normalize whitespace;
- use the server/internal date for `mail_received_at`, falling back to the
  parsed `Date` header and then current time;
- add frontmatter through the existing formatting-preserving helpers rather
  than concatenating YAML; and
- ensure remote text cannot inject additional frontmatter keys.

The message body is card content, never a template. HTML, URLs, attachment
names, and headers must not cause network access, shell execution, Lua
evaluation, or local file reads.

## Idempotency and crash recovery

Remote `\\Seen` state and `Message-ID` are insufficient for exactly-once local
creation. Maintain a local ledger under the user configuration directory:

```text
<user-config>/kbrd/mail/<board-account-hash>.json
```

The hash includes the absolute board path and normalized account/mailbox scope.
Create the directory as `0700` and the atomic state file as `0600`, using the
same durable-write approach as the Reminders integration. Acquire both a
process-local mutex and a cross-process file lock before a non-dry-run import.

Each message record has a small state machine:

```text
discovered -> pending_card -> card_created -> complete
```

Required sequence:

1. Parse and validate the message.
2. Choose the desired card path and persist `pending_card`, including
   `source_id`, body hash, and desired path.
3. Create the card atomically with `source_id` frontmatter.
4. Persist `card_created` with the actual path.
5. Run optional `item_created` hooks.
6. Persist `complete` even when a hook reports a warning; hooks never undo a
   successfully written card.

Recovery rules:

- If `pending_card` has no card, retry creation.
- If the desired path exists with the same `source_id`, adopt it and continue.
- If the path exists with another identity, choose the next collision-safe
  name, update state, and create there.
- If `card_created` points to a matching card, finish hooks/complete according
  to the recorded hook state.
- If state says `complete`, never create another card.
- If local state is lost, scan candidate destination cards for `source_id`
  before creating. This is a bounded recovery path, not the normal lookup.

Record whether hooks have started, but do not promise exactly-once hook
execution across process crashes; arbitrary external commands cannot be made
transactional. Card creation itself must remain exactly-once under the retained
ledger and source metadata.

Version one does not need to modify remote messages. The local ledger is the
source of truth, and `BODY.PEEK` leaves read state unchanged. A later optional
acknowledgement policy may add a provider-supported keyword or move completed
messages to a configured folder, but it must happen only after `complete` and
must not be required for deduplication.

## Failure and safety policy

### Resource limits

Apply conservative defaults before parsing:

- at most 50 candidates per run;
- at most 256 KiB of imported text per card;
- bounded decoded header lengths;
- bounded MIME nesting and part count;
- no attachment body downloads; and
- a total command deadline plus per-operation deadlines.

Oversized or malformed messages are reported as failures without partially
created cards. Truncation should be explicit in the card only when a future
flag enables it; silently truncating task data is unsafe.

### Secret handling

- Require TLS certificate verification.
- Redact usernames and credentials from connection errors where libraries may
  echo connection strings.
- Never log raw messages or authentication exchanges.
- Do not store credentials in state, cards, TOML, Git remotes, or command
  arguments.
- Remove `KBRD_MAIL_PASSWORD` from the environment before any hook subprocess
  can inherit it.
- Recommend a dedicated mailbox and revocable app password.
- Clear password byte buffers where practical, while acknowledging that Go
  strings cannot provide a hard memory-erasure guarantee.

### Trust boundary

Email sender filtering is not a security boundary. Imported email may be spam
or spoofed, so it is always inert text. Board hooks are the only behavior that
may execute after creation and remain governed by the existing root `--safe`
policy. Documentation and scheduled examples should use `--safe` by default.

## Testing strategy

### Pure and package tests

Add table-driven tests for:

- encoded and malformed subjects;
- empty subjects and sender fallbacks;
- plain, HTML-only, multipart/alternative, nested, and malformed MIME;
- attachment metadata without attachment-body reads;
- Unicode, CRLF normalization, and control-character stripping;
- sender filtering and address normalization;
- UID/UIDVALIDITY identity construction;
- body and header size limits;
- deterministic oldest-first planning; and
- name collisions.

### Service tests

Use a small fake IMAP client and real temporary board directories to cover:

- first import and repeat no-op;
- two different messages with the same subject;
- missing or duplicate `Message-ID` headers;
- UID reuse after `UIDVALIDITY` changes;
- dry-run performing no card, state, hook, or remote writes;
- hook subprocesses never receiving `KBRD_MAIL_PASSWORD`;
- per-message failure with later-message progress;
- cancellation during search, fetch, and card creation;
- concurrent process-lock rejection; and
- crash injection before and after every ledger transition.

Tests must prove that every crash point converges to one card after retry.

### Command and config tests

Drive the command through a fresh `NewRootCmd`, inject a fake importer factory,
and assert stdout/stderr and exit status. Preserve all existing `kbrd ingest`
tests to prove the parent-command conversion is backward compatible.

Add config tests for defaults, folder/global override behavior, invalid limits,
and the absence of any accepted password key. If `ingest.mail.password` appears
in TOML, configuration validation should fail with a message directing the
user to `KBRD_MAIL_PASSWORD`.

### Optional interoperability tests

After unit behavior is stable, add a manually run or CI service test against a
local Dovecot container. Maintain a provider smoke-test checklist for Gmail,
iCloud Mail, mailbox.org, Migadu, Purelymail, and Fastmail. Provider credentials
must never enter the repository or normal CI.

## Delivery phases

### Phase 1: reusable ingestion operation

1. Extract card creation from `commands/ingest.go` into package `ingest`.
2. Preserve direct-ingest CLI behavior, output, hooks, and tests.
3. Add an explicit collision policy and typed `source_id` metadata without
   changing direct-ingest duplicate behavior.
4. Document that ingestion itself does not commit Git changes.

Exit criterion: all existing tests pass and direct `kbrd ingest` behavior is
unchanged.

### Phase 2: message parsing and planning

1. Add `mailingest` types, MIME parsing, HTML-to-text fallback, filtering, and
   resource limits.
2. Add the IMAP dependency behind the narrow client interface.
3. Implement strict TLS connection, UID search, metadata fetch, and selected
   part fetch.
4. Implement `--dry-run` planning without local or remote mutation.

Exit criterion: fixture and fake-client tests cover the complete selection and
mapping pipeline without creating cards.

### Phase 3: durable import

1. Add locked, atomic local state modeled after `reminders/state.go`.
2. Implement the pending/card-created/complete recovery state machine.
3. Adapt package `ingest` as the card writer.
4. Dispatch hooks through the same command-layer path as direct ingestion.
5. Add crash-point and duplicate-prevention tests.

Exit criterion: retries after every simulated interruption converge to exactly
one card per IMAP UID identity.

### Phase 4: CLI and documentation

1. Add `kbrd ingest mail` and environment resolution.
2. Add config template entries and README usage/security guidance.
3. Add examples for manual use, cron, systemd timers, and macOS launchd, all
   using `--safe` by default.
4. Run `gofmt`, `go vet ./...`, and `go test ./...`.

Exit criterion: a user with a generic app-password IMAP account can configure
and repeatedly import mail without running a network listener or creating
duplicates.

### Later extensions

Only after the one-shot importer is proven:

- `--watch` with polling or IMAP IDLE and clean context cancellation;
- OAuth2 authentication adapters for Gmail and Microsoft accounts;
- OS keychain credential lookup;
- optional remote keyword/move acknowledgement;
- configurable subject-prefix routing to columns;
- attachment import with explicit size/type policy; and
- provider-specific setup helpers.

These extensions must reuse the same parser, planner, ledger, and card-writer
boundary. They should not introduce an inbound kbrd server.

## Open decisions for implementation

Resolve these with small spikes before Phase 2 coding:

1. Whether the stable go-imap v1 API or the v2 beta has the safer cancellation
   and MIME-section fetch behavior with Go 1.26.
2. Whether hook recovery records only `not_started`/`started`, or also a local
   completion marker for better diagnostics without claiming exactly-once
   external effects.
3. Whether the HTML fallback should preserve link targets inline or append a
   compact links section.
4. Whether an empty `allowed_senders` list should warn or be a hard error for
   unattended execution.

None of these decisions changes the command shape or the core outbound-only,
idempotent architecture.
