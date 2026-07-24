# Telegram Ingestion Plan

## Goal

Allow a user to create cards by sending a private Telegram message from a
phone, without exposing kbrd or the board to the internet. kbrd connects
outbound to the official Telegram Bot API, pulls pending messages, converts
eligible messages into cards, records local idempotency state, and exits.

The first implementation is a manually or externally scheduled one-shot
command:

```bash
KBRD_TELEGRAM_TOKEN='123456:bot-token' \
  kbrd --safe ingest telegram --board work --column Inbox
```

`--safe` is recommended for unattended imports because it prevents an incoming
message from indirectly triggering board-local hooks or other executable board
configuration. Users who intentionally want normal `item_created` hooks can
omit it.

Telegram is less durable than the planned mail importer. Bot API updates are
retained for no longer than 24 hours, and the Bot API has no endpoint for a bot
to recover older private-chat history. The command should normally run every
5–15 minutes and warn when the interval since the last successful poll suggests
that messages may have expired.

## Why Telegram fits

The official Bot API supports two mutually exclusive delivery modes:
`getUpdates` polling and webhooks. `getUpdates` is an outbound HTTPS request, so
it does not require a public hostname, open port, TLS certificate, tunnel, or
hosted callback. The Bot Platform is free for normal bot use.

The implementation will use a dedicated bot and `getUpdates`. It will not log
in as a Telegram user or automate the Telegram client protocol. This keeps the
integration inside Telegram's supported bot surface.

## Non-goals

The first version will not:

- run an inbound HTTP or webhook server;
- use Telegram's MTProto user-account API;
- send replies, confirmations, or other outbound chat messages;
- run continuously or implement long polling as a daemon;
- import photos, documents, audio, video, or voice-message bodies;
- transcribe voice messages or run OCR;
- synchronize card changes back to Telegram;
- process edited messages, deleted messages, reactions, or callback queries;
- interpret message content as commands, Lua, templates, or frontmatter;
- discover arbitrary chats or accept messages from unconfigured users;
- commit or push Git changes automatically; or
- promise recovery of messages that expired from Telegram before polling.

Telegram import follows existing `kbrd ingest` Git behavior: it creates a card
in the working tree. Existing TUI Git configuration and explicit user actions
remain responsible for commits and synchronization.

## User experience

### Initial bot setup

The user performs these steps once:

1. Create a dedicated bot with Telegram's `@BotFather` and copy its token.
2. Open a private chat with the bot and send `/start`.
3. Send a sample capture message.
4. Run a dry run to discover the numeric user and chat identifiers:

   ```bash
   KBRD_TELEGRAM_TOKEN='123456:bot-token' \
     kbrd --safe ingest telegram --board work --dry-run
   ```

5. Add the displayed identifiers to `kbrd.toml` or pass them as flags.
6. Run the real importer, then schedule it.

Dry run may display identifiers from pending updates but must redact the bot
token and avoid cards, hooks, state writes, API acknowledgement, and other
remote mutation. A non-dry-run import requires both a chat ID and at least one
allowed user ID. This prevents a newly created or accidentally shared bot from
becoming an open board-ingestion endpoint.

The setup documentation must also instruct the user to disable group addition
for the bot in `@BotFather` when the bot is intended only for personal capture.
The importer independently rejects non-private chats, so this setting is
defense in depth rather than the authorization boundary.

### Capture format

A user sends a private message to the bot:

```text
Buy train tickets

Check Warsaw–Prague connections for September.
```

The first non-empty line becomes the card name. The remaining text becomes the
card content:

```text
Inbox/buy-train-tickets.md
```

```markdown
---
created_at: "2026-07-24T10:20:00Z"
source: "telegram"
source_id: "sha256:..."
telegram_sent_at: "2026-07-24T10:19:31Z"
---

Check Warsaw–Prague connections for September.
```

Leading and trailing blank lines are removed from the title/body boundary, but
body whitespace is otherwise preserved after line-ending normalization. A
single-line message creates a card with that line as its name and an empty
body. A message whose text contains no usable title is rejected rather than
creating an ambiguous card.

Names pass through `board.SanitizeGeneratedName`. Telegram imports use a
collision policy that tries a numeric suffix rather than overwriting an
existing card. Direct `kbrd ingest` retains its current duplicate-name error.

Bot-control messages `/start`, `/help`, and `/id`, with an optional `@botname`
suffix, are setup traffic and never become cards. Other slash-prefixed text is
treated as ordinary card content in version one; kbrd does not implement a
remote command language.

Messages with text are accepted. A media message with a caption uses the
caption as its text and appends a short note describing the unimported media
type and sanitized filename when available. A media message without a caption
is skipped with a clear reason. No media download endpoint is called in the
first version.

### Command surface

Turn the existing executable `ingest` command into a parent that retains its
current `RunE` behavior and adds a `telegram` child:

```text
kbrd ingest ...           # unchanged direct text ingestion
kbrd ingest telegram ...  # Telegram Bot API ingestion
```

Proposed initial flags:

| Flag | Meaning |
| --- | --- |
| `--board` | Required board path or recent-board name, matching `kbrd ingest` |
| `--column` | Destination column; overrides config and defaults to `Inbox` |
| `--chat-id` | Required private-chat ID; overrides config |
| `--user-id` | Repeatable allowed sender ID; adds to or overrides config as defined below |
| `--limit` | Maximum updates returned per request, default `50`, range `1–100` |
| `--dry-run` | Show pending updates and identities without cards, state, hooks, or acknowledgement |

Flag/config precedence should be unsurprising: when at least one `--user-id`
flag is present, the complete flag list replaces `allowed_user_ids`; otherwise
the configured list is used. `--chat-id` replaces the configured value when
set. Numeric IDs use signed 64-bit integers because Telegram identifiers may
exceed 32-bit ranges and chat IDs may be negative for non-private chat types.

There is deliberately no `--token` flag because command lines leak through
shell history and process inspection. The bot token is accepted only through:

```text
KBRD_TELEGRAM_TOKEN
```

The command copies the token into its connection options and immediately
unsets `KBRD_TELEGRAM_TOKEN` from the process environment before loading or
executing any board-local hook. The concrete API client receives the copied
secret directly and must not recover it from ambient environment variables.

The token must never be printed, included in errors, serialized to state, or
placed in `kbrd.toml`. Telegram places the token in the Bot API URL path, so the
HTTP adapter must never log request URLs and must reject redirects rather than
risk forwarding a token-bearing path to another host.

### Output and exit status

Human-readable output follows the existing headless integration style:

```text
IMPORT  4821  Inbox/buy-train-tickets.md
SKIP    4822  setup command
SKIP    4823  sender 998877 is not allowed
telegram: imported 1, skipped 2, failed 0
```

Dry-run output includes the sender ID, chat ID, chat type, timestamp, and a
bounded title preview so the user can configure authorization without exposing
the entire message body.

Authentication, webhook conflicts, malformed API responses, state corruption,
and lock failures are fatal. A malformed individual update is reported and
terminally skipped so it cannot block the queue forever. A transient card or
filesystem failure is retained as retryable, causes a non-zero command status,
and prevents acknowledgement past that update. Successfully created later
cards are retained and deduplicated on retry.

## Configuration

Only portable, non-secret policy belongs in `kbrd.toml`:

```toml
[ingest.telegram]
column = "Inbox"
chat_id = 1122334455
allowed_user_ids = [1122334455]
max_text_bytes = 65536
```

Proposed typed configuration:

```go
type TelegramIngestConfig struct {
	Column         string
	ChatID         int64
	AllowedUserIDs []int64
	MaxTextBytes   int64
}

type IngestConfig struct {
	CreatedAtFormat string
	Telegram       TelegramIngestConfig
}
```

Defaults must be registered explicitly in `config.Load`:

```text
ingest.telegram.column = "Inbox"
ingest.telegram.chat_id = 0
ingest.telegram.allowed_user_ids = []
ingest.telegram.max_text_bytes = 65536
```

Non-dry-run validation rejects a zero chat ID or an empty user allowlist. It
also rejects a configured `ingest.telegram.token` key with a message directing
the user to `KBRD_TELEGRAM_TOKEN`. The token is machine-secret state and must
not be committed with the board.

The configured column is resolved only after the allowed chat and sender
policy has been validated. Remote text cannot select a board, column, hook,
template, or other behavior in version one.

## Architecture

### Package layout

```text
commands/
├── ingest.go                  # existing direct-ingest command
└── ingest_telegram.go         # Cobra flags, env resolution, output

ingest/
├── ingest.go                  # extracted card-ingestion operation
└── ingest_test.go

telegramingest/
├── service.go                 # polling, authorization, and state transitions
├── client.go                  # narrow Bot API client interface
├── http.go                    # concrete HTTPS Bot API adapter
├── message.go                 # update-to-card normalization
├── state.go                   # durable cursor and idempotency ledger
├── lock.go                    # per-bot/board process and file lock
├── types.go
└── *_test.go
```

`commands` remains a routing layer. It resolves flags and environment
variables, constructs typed options, supplies command input/output, and invokes
the service. Neither `ingest` nor `telegramingest` imports Cobra or Viper.

Extract the reusable portion of `commands/runIngest` into package `ingest`
before adding Telegram. This is the same prerequisite described by the mail
ingestion plan. The package should own:

- board and column resolution;
- generated-name sanitization with an explicit collision policy (`error` for
  direct ingestion and `suffix` for remote ingestion);
- `created_at`, `source`, and `source_id` frontmatter;
- atomic no-clobber card creation; and
- a typed result containing the final board, column, name, and path.

The command layer remains responsible for printing and for constructing the
`item_created` hook runner. Direct, mail, and Telegram ingestion should all use
the extracted operation so they cannot drift in naming or metadata behavior.

`telegramingest` owns update polling, authorization, parsing, ordering,
idempotency, and acknowledgement policy. It consumes narrow interfaces defined
where they are used:

```go
type Client interface {
	Me(context.Context) (Bot, error)
	WebhookInfo(context.Context) (WebhookInfo, error)
	Updates(context.Context, UpdateRequest) ([]Update, error)
}

type CardWriter interface {
	Create(context.Context, CardRequest) (CardResult, error)
}

type HookRunner interface {
	ItemCreated(context.Context, CardResult) []error
}
```

Production wiring adapts package `ingest` to `CardWriter` and the existing hook
dispatcher to `HookRunner`. Under root `--safe`, the command injects a no-op
runner. Keeping card creation and hooks separate lets the service durably
record the card before starting an external side effect. Tests use small hand-
written fakes rather than a mocking framework.

### Bot API dependency

Do not add a Telegram SDK in version one. The required Bot API surface is three
small JSON-over-HTTPS methods: `getMe`, `getWebhookInfo`, and `getUpdates`.
Go's `net/http` and `encoding/json` packages are sufficient and avoid coupling
the workflow to a broad third-party model or polling framework.

The adapter uses only POST requests with JSON bodies, except where Telegram
requires otherwise. It constructs the fixed official API origin internally;
callers cannot supply an alternate base URL in production. Tests inject an
`httptest.Server` URL through an unexported/test-only constructor or an
explicitly injected transport, not through user configuration.

The concrete adapter must:

- use `https://api.telegram.org` with normal certificate and hostname
  verification;
- set connect, response-header, per-request, and total-command deadlines;
- disable HTTP redirects;
- cap response bodies before JSON decoding;
- decode Telegram's top-level `ok`, `result`, `error_code`, `description`, and
  retry parameters without including the request URL in errors;
- honor `429 retry_after` only within the command's bounded retry policy;
- never log the token, full URL, request headers, or raw message bodies; and
- close response bodies on every path.

There is no insecure TLS flag and no configurable production Bot API host.

### Command construction

`newIngestCmd` remains a factory and attaches `newTelegramIngestCmd` as a
child. The parent retains its current `RunE`, flags, help, and direct-ingest
behavior. This preserves:

```text
kbrd ingest --board ... --name ...
```

while enabling:

```text
kbrd ingest telegram --board ...
```

The Telegram command uses `cobra.NoArgs`, passes `cmd.Context()` into all
network and filesystem work, and prints only through `cmd.OutOrStdout()` and
`cmd.ErrOrStderr()`. Business logic receives typed options and has no Cobra or
Viper dependency.

## Polling and acknowledgement

### Startup sequence

Each non-dry-run invocation performs these steps:

1. Read `KBRD_TELEGRAM_TOKEN` into a local option and unset the environment
   variable.
2. Resolve the board and load non-secret configuration.
3. Validate the chat ID, sender allowlist, limits, and destination column.
4. Call `getMe` and record the stable bot user ID, never the token, as account
   identity.
5. Call `getWebhookInfo`; fail if a webhook URL is configured.
6. Acquire the process-local and cross-process state lock.
7. Load and validate state for the absolute board path and bot user ID.
8. Warn if the last successful poll was more than 24 hours ago.
9. Call `getUpdates` with the saved next offset, `limit`, zero timeout, and
   `allowed_updates=["message"]`.
10. Process updates in ascending `update_id` order.
11. Persist the highest contiguous terminal cursor and successful-poll time.

The importer does not call `deleteWebhook`. Removing a webhook is a meaningful
remote configuration change and must be an explicit user action. The error
should explain that polling and webhooks are mutually exclusive and link to the
official `deleteWebhook` method.

Only one consumer may poll a dedicated bot. The local lock prevents concurrent
processes on one machine, but it cannot protect against a second computer or
service using the same token. Documentation must warn that another poller can
acknowledge and permanently consume updates before kbrd sees them.

### Telegram acknowledgement semantics

Telegram considers updates confirmed when a later `getUpdates` request carries
an `offset` greater than their `update_id`. The service must therefore treat
the stored next offset as a commit cursor, not advance it when updates are
merely received, and never issue a speculative higher offset.

For every update, processing reaches one of three outcomes:

```text
complete        card durably created; safe to acknowledge
terminal_skip   intentionally ignored or permanently invalid; safe to acknowledge
retryable       transient failure; do not acknowledge this update or later updates
```

The cursor advances only across a contiguous prefix of `complete` and
`terminal_skip` records. Later updates may still be processed and cards may be
created, but the cursor remains behind a retryable gap. Their ledger records
make the repeated delivery a no-op on the next run.

Examples of terminal skips:

- update type is not a message;
- sender, chat, or chat type is not allowed;
- setup command;
- empty message or uncaptioned media;
- malformed message that cannot become a safe card; or
- text exceeds the configured permanent size limit.

Examples of retryable failures:

- card directory temporarily unavailable;
- atomic state write fails;
- command context is canceled;
- hook recovery cannot complete its local transition; or
- transient Telegram response or filesystem error.

Dry run calls `getUpdates` using the stored offset when state exists, or without
an offset on first use. It never writes a new offset, so Telegram will return
the same updates to a later real run.

## Update selection and mapping

The request sets `allowed_updates` to `message`, but Telegram notes that this
filter does not affect already queued updates. The decoder must tolerate and
terminally skip other update types.

For a message to be eligible, all of these checks must pass locally:

- `chat.type` is exactly `private`;
- `chat.id` equals the configured chat ID;
- `from.id` is in the exact numeric user allowlist;
- the sender is not a bot;
- `message_id`, `date`, `chat`, and sender fields are present and valid; and
- usable `text` or `caption` is within the configured decoded byte limit.

Authorization is based on immutable numeric identifiers, not usernames,
display names, phone numbers, or message text. Usernames can change and must be
used only for bounded diagnostics when present.

Telegram returns Unix timestamps in UTC. Store the normalized send time as
`telegram_sent_at`; use the current local creation time, formatted according to
existing ingest configuration, for `created_at`.

The content normalizer must:

- validate UTF-8 and replace invalid sequences deterministically;
- normalize CRLF and CR to LF;
- strip NUL and unsafe control characters while preserving tabs/newlines;
- bound title length before filename sanitization;
- preserve message text as inert Markdown content;
- avoid interpreting Telegram Markdown/HTML, entities, links, or mentions; and
- never fetch URLs embedded in messages.

Forwarded-message origin metadata, contact details, location, and Telegram
profile data are not copied to the card. The card contains only the capture
text and minimal source/timestamp metadata.

## Identity and state

### Source identity

`update_id` is the Bot API delivery identity and cursor. The canonical source
identity combines:

```text
telegram | bot user ID | update ID
```

Hash this canonical form with SHA-256 and store the result as `source_id` in
frontmatter. Store the raw bot/update components only in machine-local state.
This avoids exposing private chat or user identifiers in a board that may be
committed to Git.

Retain `chat_id` and `message_id` in the local ledger for diagnostics and
future optional reply support, but do not use them as the sole idempotency key.

### Durable ledger

Store state under the user configuration directory:

```text
<user-config>/kbrd/telegram/<board-bot-hash>.json
```

The filename hash includes the absolute board path and bot user ID, never the
token. Create the directory as `0700` and the atomic state file as `0600`, using
the same durable-write and lock approach as the Reminders integration. Acquire
both a process-local mutex and a cross-process file lock before a non-dry-run
import.

Top-level state contains:

```text
schema version
bot user ID
absolute board path hash
next offset
last successful poll time
per-update recovery records
```

Each accepted update has a small state machine:

```text
discovered -> pending_card -> card_created -> complete
```

Terminal skips record a bounded reason and terminal status so the cursor can
advance without retaining raw message content.

Required card sequence:

1. Parse and authorize the update.
2. Choose the desired card path and persist `pending_card`, including
   `source_id`, a normalized-content hash, and desired path.
3. Create the card atomically with `source_id` frontmatter.
4. Persist `card_created` with the actual path.
5. Run optional `item_created` hooks.
6. Persist `complete` even when a hook reports a warning; hooks never undo a
   successfully written card.
7. Advance and persist the contiguous next offset.

Recovery rules:

- If `pending_card` has no card, retry creation.
- If the desired path exists with the same `source_id`, adopt it and continue.
- If the path exists with another identity, choose the next collision-safe
  name, update state, and create there.
- If `card_created` points to a matching card, finish hook/complete recovery.
- If state says `complete`, never create another card.
- If state is lost but Telegram redelivers the update, scan candidate
  destination cards for matching `source_id` before creating.

Record whether hooks have started, but do not promise exactly-once hook
execution across process crashes; arbitrary external commands cannot be made
transactional. Card creation itself must converge to exactly one card while
the source update or retained state remains available.

Completed and terminal-skip records older than a conservative retention window
may be compacted only after the persisted next offset is beyond them. Keep
enough recent records for diagnostics and crash testing. Never compact
`pending_card` or `card_created` records automatically.

## Failure and safety policy

### Retention-gap warning

State records `last_successful_poll_at`. When the gap exceeds 24 hours, emit:

```text
warning: Telegram was last polled 27h ago; updates older than 24h may have expired
```

This is a warning, not proof of loss: there may have been no messages during
the gap. On first use, explain the 24-hour limitation in dry-run/setup output.
Do not invent a cursor or silently claim the backlog is complete.

### Resource limits

Apply conservative defaults before decoding or allocating:

- at most 50 updates per run by default and never more than Telegram's 100;
- at most 64 KiB of decoded capture text per card;
- at most 8 MiB for the complete API response body;
- bounded diagnostic strings and title previews;
- no media downloads; and
- a total command deadline plus per-request deadlines.

An oversized individual message is a terminal failure with a visible reason.
Silent truncation is unsafe for task data. A future explicit truncation option
may change that behavior.

### Secret handling

- Require normal TLS certificate verification.
- Never accept the bot token as a flag or board configuration key.
- Never print or log token-bearing Bot API URLs.
- Disable redirects for requests whose path contains the token.
- Redact Telegram error text if it unexpectedly contains the token.
- Never store the token in state, cards, logs, Git remotes, or hook input.
- Remove `KBRD_TELEGRAM_TOKEN` from the environment before hooks can inherit it.
- Recommend a dedicated, revocable bot token.
- Explain that anyone with the token controls the bot and that rotation through
  `@BotFather` is required after exposure.

Go strings cannot provide a hard memory-erasure guarantee. The implementation
should minimize copies and lifetime without claiming secure erasure.

### Trust boundary

Numeric chat and sender checks are the authorization boundary. Apply them
before parsing content into a card request. Imported text remains untrusted,
inert data. It must not trigger network requests, shell execution, Lua,
template expansion, frontmatter injection, or local file reads.

Board hooks are the only behavior that may execute after creation and remain
governed by the existing root `--safe` policy. Documentation and scheduler
examples should use `--safe` by default.

## Testing strategy

### Pure mapping tests

Add table-driven tests for:

- multiline, single-line, blank, and whitespace-only captures;
- CRLF normalization, Unicode, invalid UTF-8, and control characters;
- title bounds and generated-name sanitization;
- exact numeric sender/chat authorization;
- private versus group, supergroup, and channel chat types;
- setup commands with and without a bot username suffix;
- text, captioned media, and uncaptioned media;
- forwarded messages without metadata leakage;
- oversized text and response limits;
- deterministic source identity; and
- name collisions.

### HTTP adapter tests

Use `httptest.Server` and an injected transport/base URL available only to
tests. Cover:

- successful `getMe`, `getWebhookInfo`, and `getUpdates` decoding;
- Telegram `ok=false` responses and error-code preservation;
- malformed, truncated, and oversized JSON;
- non-2xx HTTP status;
- `429 retry_after` bounded retry behavior;
- context cancellation and timeouts;
- redirect rejection;
- response-body closure; and
- proof that errors and captured logs never contain the token.

Do not make live Telegram calls in the normal unit suite.

### Service and recovery tests

Use a small fake client and real temporary board directories to cover:

- first import and repeat no-op;
- sequential ascending update processing;
- unauthorized and malformed updates advancing the cursor as terminal skips;
- a retryable gap preventing acknowledgement of later updates;
- later cards surviving and deduplicating across that retry;
- card collision suffixes;
- dry run performing no card, state, hook, or remote mutation;
- webhook-configured rejection;
- token removal before a hook subprocess starts;
- cancellation during polling, state writes, and card creation;
- concurrent process-lock rejection;
- state schema and bot-identity mismatch; and
- crash injection before and after every ledger transition.

Tests must prove that every simulated crash point converges to one card and
that the next offset never advances past a retryable update.

### Command and config tests

Drive commands through a fresh `NewRootCmd`, inject a fake importer/client
factory, and assert stdout, stderr, and exit status. Preserve every existing
`kbrd ingest` test to prove the parent-command conversion is backward
compatible.

Add command cases for missing token, missing board, invalid IDs, invalid limit,
flag/config precedence, dry-run identity discovery, and safe-mode hook
suppression. Use `t.Setenv` for the token and verify it is absent from the hook
environment.

Add config tests for defaults, folder/global override behavior, invalid sizes,
and rejection of any token key. Configuration tests must not contain a token
that resembles a real credential.

### Optional interoperability test

Maintain a manually run smoke-test checklist using a dedicated disposable bot:

1. Start a private bot chat and send text, captioned media, and `/start`.
2. Verify dry-run identity discovery without acknowledgement.
3. Import once and confirm card content/frontmatter.
4. Import again and confirm no duplicate.
5. Stop after card creation at an injected crash point and verify recovery.
6. Configure a webhook temporarily and verify the importer refuses to poll.
7. Rotate the token after the test.

Real bot tokens must never enter the repository or normal CI.

## Delivery phases

### Phase 1: reusable card ingestion

1. Extract card creation from `commands/ingest.go` into package `ingest`.
2. Preserve direct-ingest CLI behavior, output, hooks, and tests.
3. Add explicit collision and source-identity policies without changing direct
   ingest's duplicate-name behavior.
4. Convert `ingest` into a parent-with-children command while retaining its
   existing `RunE` path.

Exit criterion: all existing tests pass and direct `kbrd ingest` behavior is
unchanged.

### Phase 2: Bot API client and pure mapping

1. Add `telegramingest` API types and the narrow client interface.
2. Implement `getMe`, `getWebhookInfo`, and `getUpdates` with `net/http`.
3. Implement exact authorization, text normalization, title/body mapping, and
   source identity.
4. Add dry-run planning without local or remote mutation.

Exit criterion: fake-server and mapping tests cover the complete API-to-card
plan without creating cards.

### Phase 3: durable import and cursor recovery

1. Add locked, atomic local state modeled after `reminders/state.go`.
2. Implement pending/card-created/complete recovery and terminal skips.
3. Implement contiguous next-offset advancement.
4. Adapt package `ingest` as the card writer and dispatch hooks through the
   existing command-layer path.
5. Add crash-point, retryable-gap, and duplicate-prevention tests.

Exit criterion: retries after every simulated interruption converge to exactly
one card per accepted update, and no update past a retryable gap is
acknowledged.

### Phase 4: CLI, scheduling, and documentation

1. Add `kbrd ingest telegram`, flags, configuration, and token resolution.
2. Add README setup and security guidance for `@BotFather` and private chats.
3. Add cron, systemd timer, and macOS launchd examples that run every 5–15
   minutes with `--safe`.
4. Document the 24-hour retention limitation and one-consumer rule prominently.
5. Run `gofmt`, `go vet ./...`, and `go test ./...`.

Exit criterion: a user can configure a dedicated bot, discover IDs safely,
schedule outbound-only imports, and repeatedly create cards without a public
listener or duplicates.

### Later extensions

Only after the one-shot text importer is proven:

- `--watch` using bounded `getUpdates` long polling and clean cancellation;
- optional success/error replies after durable state transitions;
- photo and document import with explicit size/type policy;
- voice download and opt-in transcription adapters;
- OS keychain token lookup;
- multiple named bot accounts in machine-local configuration;
- configurable prefix or topic routing to columns; and
- metrics for poll gaps and expired-update risk.

These extensions must reuse the same normalizer, ledger, cursor, and
card-writer boundary. They must not introduce an inbound kbrd server.

## Open decisions for implementation

Resolve these with focused tests or small spikes before Phase 3 coding:

1. Whether terminal-skip records should be retained for 7 or 30 days after the
   cursor passes them.
2. Whether a captioned media note should include only media type or also a
   sanitized filename and reported size.
3. Whether hook recovery records only `not_started`/`started`, or also a local
   completion marker for better diagnostics without claiming exactly-once
   external effects.
4. Whether the default total command deadline should be 30 or 60 seconds for a
   scheduled one-shot run.

None of these decisions changes the command shape or the outbound-only,
allowlisted, crash-recoverable architecture.

## Official references

- [Telegram bots introduction](https://core.telegram.org/bots)
- [Telegram Bot API](https://core.telegram.org/bots/api)
- [`getUpdates`](https://core.telegram.org/bots/api#getupdates)
- [`getWebhookInfo`](https://core.telegram.org/bots/api#getwebhookinfo)
- [`deleteWebhook`](https://core.telegram.org/bots/api#deletewebhook)
- [Telegram Bots FAQ](https://core.telegram.org/bots/faq)

