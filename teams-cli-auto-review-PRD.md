# teams CLI: Code and Design Review Findings (Review PRD)

**Branch:** feat/teams-cli vs main | **Spec:** specs/261003-teams-cli/spec.md | **Date:** 2026-10-04 | **Reviewer:** dev-flow:review-code (step 5)

## 1. Executive summary

Overall quality is high. The branch (about 20k lines, 156 files) builds, `go vet` is clean, `golangci-lint` reports 0 issues, `gofmt` is clean, and `go test -race -count=1 ./...` passes with 88-100% coverage on every logic package. Clean Architecture is respected and enforced by `internal/archtest`: domain is pure, use cases depend on ports, adapters implement ports, and `cmd/teams` is the only composition root. The security-critical paths are well built: policy trust by ownership with an fd-verified read (fstrust), a build-tagged dev override that cannot ship, mention injection closed by HTML-escaping and policy-only mention targets, sender classification by AAD id (never display name), Graph next-link same-origin check, error messages that never carry message text, no token ever visible to the graph adapter, and a ledger that fails closed on corruption.

There are **no P0 blockers**. The findings are 3 P1 items (one policy bypass, one silent message-loss path, one group of spec deviations in user-visible output) and 6 P2 items. The most important one is that `TEAMS_STATE_DIR`, an environment variable the agent controls, silently overrides the policy `state_dir`, so an agent can point the CLI at an empty directory and reset its rate, loop-guard and idempotency history.

Context that is not a defect but affects readiness: the production daemon client is a deliberate stub (decision D10) that always reports exit 3, so nothing can reach a real tenant yet. This is documented in `user-docs/getting-started.md`. All Graph shapes are unverified assumptions (UA-n), tracked in `docs/unverified-assumptions.md`.

Minor non-findings noted for the record: `internal/domain` imports `agent-cli-core/output` for the `Category` type (a library dependency in the innermost layer; acceptable but worth an ADR line); `usecasetest` and `graphtest` show low coverage because they are test support.

## 2. Findings

### FR-R1 (P1): Agent-controlled environment can reset the state ledger (policy bypass)

`internal/infra/config/env.go:65-73` makes `TEAMS_STATE_DIR` win over the policy `state_dir`, and `cmd/teams/app.go:97` uses it directly. The policy file is root-owned precisely so the agent cannot edit its guardrails, but rate limits (`send.rate`), the loop guard (`reply_depth_max`), the idempotency ledger and ack/cursor state all live in the state directory. An agent (or injected instructions running `TEAMS_STATE_DIR=$(mktemp -d) teams send ...`) gets a fresh ledger on every call and bypasses FR-10, FR-11 and FR-14. The same applies to `AGENT_ID` (`app.go:89-92`), which lets the caller choose the identity written to every audit record.

Acceptance criteria:
- When the policy sets `state_dir`, the environment variable cannot override it (policy wins); the env var applies only when the policy value is empty, or is honored only in `teamsdev` builds.
- `AGENT_ID` does not override the audit identity when the policy sets `profile`, or the audit record also carries the policy profile so a spoofed id is detectable.
- A test proves a send, with `TEAMS_STATE_DIR` pointing at an empty directory, still counts against the policy-pinned ledger.
- `user-docs/configuration.md` states the precedence and that state is agent-writable (so rate limits are a guardrail against mistakes and prompt injection, not against a hostile process with file access).

### FR-R2 (P1): Silent loss of inbound messages when the backlog exceeds the page cap

`internal/adapters/graph/messages.go:50-68` fetches `lastModifiedDateTime desc` and `capNewest` keeps the newest N; `internal/usecase/inbox.go:236-259` uses `maxResults` (default 50) as N. If more than N messages arrived since the watermark, the oldest are never fetched. After the caller acks the newest ones, `CursorState.advance` (`internal/domain/cursor.go:~168-190`) moves the watermark over the delivered range and the skipped older messages are permanently outside the window. This can silently drop an instruction from a commander. The `skipped` map does not report it.

Acceptance criteria:
- Inbox either fetches in ascending order from the watermark, or detects that the page was truncated (more results than N) and does not advance the watermark past the oldest undelivered message.
- A truncated poll reports a `truncated` indicator in `skipped`/audit extras so the agent can poll again.
- A use-case test with a backlog of N+10 messages shows all N+10 are eventually delivered across successive inbox/ack cycles, in order, with none lost.

### FR-R3 (P1): whoami, destinations and dry-run output deviate from FR-1, FR-2 and FR-6

- FR-1 requires whoami to show destination aliases with capabilities, limits, poll interval, policy path and version. `internal/usecase/whoami.go:7-15` and `internal/adapters/cli/present.go:100-107` return only id, display name, upn, policy profile and version.
- FR-2 requires `mentionable` per destination. `internal/usecase/destinations.go:21` and `present.go:77-81` omit it.
- FR-6 requires `--dry-run` to print the decision, a rendered, untrusted-marked payload preview, the destination alias and the resolved kind. `internal/usecase/send.go:98-100` returns only `DryRun` and `ThreadID`; `presentSend` adds nothing else. An agent cannot see what would be posted (including the prefix and mention markup).

Acceptance criteria:
- whoami output includes destinations (alias, kind, send, watch), key limits, poll interval, policy path and version, with a test against a golden.
- destinations list items include `mentionable` (true when the alias is in `send.mentions.allow` and has the id fields a mention needs).
- dry-run output includes `decision`, `destination` (alias and kind) and a `preview` wrapped as `output.Untrusted`, and no ledger or rate side effects remain (existing behavior kept).

### FR-R4 (P2): Rate and loop checks are not atomic with the post, and in-flight sends are not counted

`internal/usecase/send.go:140-165` reads `SentSince`/`SentInThread` in one lock section; the ledger records the send in a later section (`Complete`/`RecordSent`, `send.go:223-233`). Two concurrent `teams send` processes can both pass the check and both post, exceeding `per_minute` or `reply_depth_max`. FR-10 says "success and pending only", but a pending key is not counted in the sent history until completion, and an unkeyed send whose `RecordSent` fails only sets a `warn` extra (`send.go:228-231`) so the send is invisible to later rate checks.

Acceptance criteria:
- The check-and-claim is a single locked ledger operation (reserve a rate slot, also for unkeyed sends), released or marked failed on a NotSent error.
- Pending reservations count toward the windows.
- A test runs N concurrent sends against a limit of K and shows exactly K succeed.
- A `RecordSent` failure on an unkeyed send is surfaced (warning in the envelope or exit code), not only an audit extra.

### FR-R5 (P2): Audit is written after the post, so "block writes on audit failure" cannot block

`internal/usecase/service.go:112-150` records the single audit event after `fn` returns. For send and reply the message is already posted when `Record` fails; the command then exits non-zero (`service.go:147-149`) although the message was delivered, and the `res` with the message id is discarded by the caller. FR-28 intends that an unauditable write does not happen.

Acceptance criteria:
- For send/reply, an intent record (or an availability probe of the audit sink) is written before the Graph POST; failure there blocks the post with a clear error.
- If the post succeeds and the final record fails, the result still carries the message id and the error says the message was delivered but not audited.
- Tests cover both cases.

### FR-R6 (P2): Inbound `links[]` are plain strings from untrusted senders

`internal/adapters/cli/present.go:58-60` and `FR-17` leave `links` unmarked. Link text and URLs are attacker-controlled and can carry instruction text (for example in a query string or path), and the text/table renderers show them outside any untrusted delimiter. The `forbidden` hint tells the agent not to follow links, but the output does not mark them.

Acceptance criteria:
- Either each link is emitted as `output.Untrusted`, or links are limited to scheme, host and a length-capped path with control characters stripped.
- A presenter test with a link containing `<<<` and an instruction sentence shows it cannot break out of the delimiters or read as unmarked prose.
- `user-docs/usage.md` documents the field.

### FR-R7 (P2): Audit records never carry the HTTP status and some FR-28 fields are not populated

`HTTPStatus` is defined on `domain.AuditEvent` and passed to the sink (`internal/adapters/auditlog/sink.go:102`) but no use case ever sets it (`service.go:119-126`). FR-28 lists http status. Without it, operators cannot tell a 403 from a 5xx in the audit trail.

Acceptance criteria:
- The status of the last Graph call (from the typed error or success) reaches the audit event.
- A use-case test asserts the value for a 403, a 429 and a success.

### FR-R8 (P2): CI correctness risks

`.github/workflows/ci.yml`:
- Module fetch (`lines ~30-36`, `GOPRIVATE`) authenticates with `GITHUB_TOKEN` and `packages: read`. A job token cannot read a different private repository (`agent-cli-core`) unless that repo grants Actions access; this will likely fail on the first PR. DEP-1..6 call for a dedicated read-only token or deploy key.
- Only `ubuntu-latest` runs tests; darwin/arm64 is build-only, yet the state and fstrust code has unix-specific behavior (flock, ownership, `/etc` symlinks on macOS).
- The release-safety property (a default build has `DevBuild == false` and honors no policy-insecure env) has a unit test but CI never vets or builds with and without `-tags teamsdev`.
- Action majors (`checkout@v7`, `setup-go@v7`, `golangci-lint-action@v9`) and tool versions are not verified in this review.

Acceptance criteria:
- Documented dependency-auth secret (or confirmed repo access setting) and a run of CI on the PR that passes the download step.
- `go test` runs on a darwin runner, or the gap is recorded in `docs/deferred.md`.
- CI includes `go vet -tags teamsdev ./...` and a check that the default binary lacks the dev override.

### FR-R9 (P2): Production credential path is a stub (accepted deferral, track it)

`cmd/teams/daemon.go:19-32` returns `auth.UnreachableError` for every call (decision D10), so every network command exits 3 in production. This matches the spec and is honestly documented in `user-docs/getting-started.md`, but it means FR-26 and FR-34 are verified only against `authtest` fakes, and no end-to-end path exists.

Acceptance criteria:
- `docs/deferred.md` and the PR description state that the CLI is not usable against a tenant until the real daemon client lands, and list the swap point (`newDaemonClient`).
- A follow-up spec item exists for the real adapter and the M0 spikes, with the integration test that replaces the stub.

## 3. Positive observations

- Exactly-once audit design (one event per command, even on failure, context-detached) and message text never reaching errors or audit.
- Ledger design: reserve before post, `NotSent` classification so ambiguous failures stay pending, fail-closed on corruption, flock plus in-process mutex, atomic writes.
- Strong test discipline: golden files for inbox and skill, `TestAssumed...` for every unverified Graph shape, planted-token scans, arch tests, fake Graph server.
- Bounded reads everywhere (policy 1 MiB, JSON 16 MiB, input 4 MiB, ids validated before use as path segments or OData literals).

## 4. Verification performed

`go build ./...`, `go vet ./...`, `go vet -tags teamsdev ./...`, `golangci-lint run` (0 issues), `gofmt -l .` (clean), `go test -race -count=1 -cover ./...` (all pass). Code read: usecase (send, inbox, ack, thread, service, selftest), domain (policy_eval, mention, classify, validate, ratelimit, loopguard, cursor), graph (client, errors, post, messages), policyfile loader, fstrust, state store and ledger, auditlog sink, cli router/flags/presenters, cmd/teams wiring, CI workflow, spec FR table. No real-tenant behavior was or could be verified.
