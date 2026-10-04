# Technical Details

Describes the implementation as built. Design rationale is in `architectural-decision-record.md`; the full design is in `../specs/261003-teams-cli/architecture.md`. Graph shapes are unverified against a real tenant (`unverified-assumptions.md`).

## Layers

Clean Architecture; dependencies point inward and `internal/archtest` enforces the rules (including the dependency allow-list: only `agent-cli-core` v0.1.0 and `goccy/go-yaml` v1.19.2, no `replace`, `agent-okta-d` never imported).

| Layer | Path | Role |
|---|---|---|
| Entry | `cmd/teams` | `main.go` (signal context, exit code, build stamp), `app.go` (`assemble()` composition root), `daemon.go` (`newDaemonClient()` stub), `usecase_wire.go` |
| Adapters | `internal/adapters/{cli,graph,state,policyfile,auditlog,selftestcfg}` | I/O. `graph/graphtest` is the `httptest` fake Graph used by tests |
| Infrastructure | `internal/infra/{clock,fstrust,config}` | System clock, filesystem trust check, environment and defaults |
| Use cases | `internal/usecase` | `Commands` facade (`Whoami`, `Destinations`, `Send`, `Reply`, `Inbox`, `Ack`, `ThreadGet`, `Selftest`), ports in `ports.go`, fakes in `usecasetest` |
| Domain | `internal/domain` | Alias grammar, typed policy and evaluation, sender classification, mentions, content filters, HTML-to-text, cursor and ack algebra, rate and loop guards, validation, error taxonomy |

## Core integration

`agent-cli-core` v0.1.0 supplies the output envelope and exit codes (`output`), token handling (`auth`: `NewDaemonTokenSource` for provider `msgraph`, `NewAuthorizer`), HTTP with retry and host allow-list (`httpx`, only `graph.microsoft.com`), audit (`audit`), the selftest runner and skill generation (`docgen`). The core `policy` package is not used (ADR-2). Gaps and workarounds: `requested-core-changes.md`.

## Token path

`newDaemonClient()` is the single seam to the credential daemon. It currently returns a stub whose `Fetch` and `Refresh` return `*auth.UnreachableError`, so network commands exit 3 (`adr-daemon-client-stub.md`). Tests substitute `auth/authtest.Fake`. The Authorizer attaches the bearer header; adapters never see the token. One forced refresh on 401, then exit 3.

## Graph client

`internal/adapters/graph` (v1.0 base URL): `Me`; chat resolution (`ResolveUserChat`, `CreateChat`, bounded by `limits.max_chat_scan`); `PostChat`, `PostChannel` and channel replies; `ListChatMessages`; channel message list with delta and a list-plus-filter fallback; `GetChat` for the selftest negative probe. Reads are marked safe to retry; writes are not. A write failure that provably did not reach the server is `NotSent` (ledger entry `failed`, retryable); any other write failure leaves the entry `pending`. 403 maps to exit 4, 404 to 5, 409 to 7, 401 to 3, 429/503 to 8 after bounded retries.

## Policy

`policyfile` reads the file with `O_NOFOLLOW`, verifies the descriptor with `fstat`, and checks ownership of the file and every ancestor through `infra/fstrust`. It parses strictly with go-yaml (unknown and duplicate keys rejected), applies defaults and validates (`parse.go`). The `teamsdev` build tag enables `TEAMS_POLICY_INSECURE=1` for local testing; release builds have no such switch. The `domain` package evaluates policy as pure functions.

## State

`state` keeps `teams.ledger.json` and `teams.cursors.json` in `state_dir` (directory 0700), written atomically (temp file, fsync, rename) under an advisory file lock (`teams.lock`, bounded wait, then the command fails). The ledger holds idempotency entries and sent history (rate counters and loop guard are derived from it; 90-day retention for `sent` and `failed`, `pending` never expires). The cursor store holds watermarks, the delivery index, acked ids, cached `user:` chat ids and channel delta links. Corrupt files are renamed `*.corrupt-<timestamp>`; ledger corruption fails closed, cursor corruption fails open.

## Inbox flow

Per watched destination in fixed order: resolve chat (for `user:`), list messages since `max(watermark, now - max_lookback)`, normalize (drop non-message, deleted, own), classify the sender, select by `inbound.handle`, remove acked, merge and sort, apply the limit, record deliveries. If empty and `--wait` remains, sleep a jittered `poll_interval` (floor 5 s) and repeat. Channel replies are polled only for threads the agent posted in, up to `inbound.thread_poll_max`.

## Output

Presenters mark `text` and `sender.name` with `output.Untrusted`. HTML bodies become text, with links listed as plain strings and never fetched. List commands emit arrays so core bounding cuts whole items.

## Audit

`auditlog` wraps the core audit logger: one JSON Lines record per command, 0600, no bodies, no tokens. The deciding rule, alias, message id and counts are folded into `policy_decision` as `;key=value` suffixes. Audit failure blocks `send` and `reply` and only warns for reads.

## Build and CI

`make build|test|race|cover|lint|vet|fmt|check|cross|skill|clean`. Version stamping via `-ldflags -X main.version/commit/date`. `make skill` writes the generated skill to `dist/teams-cli.md` from the hidden `teams skill` command, built from the same command table the router uses. `make cross` builds darwin/arm64, linux/amd64 and linux/arm64. CI (`.github/workflows/ci.yml`) runs format, tidy, vet, lint, race tests, govulncheck and cross-compilation against fakes only; there is no release workflow (`deferred.md`).

## Tests

All tests use fakes: `authtest.Fake`, the `graphtest` fake server, fake clocks. Tests named `TestAssumed...` carry Graph-shape assumptions. `TestUserDocsSampleMatchesGolden` keeps `user-docs/teams.policy.sample.yaml` byte-equal to the golden sample used by the loader tests.
