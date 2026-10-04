# Spec: teams CLI

**Created:** 2026-10-03 | **Revised:** 2026-10-04 (review-spec pass) | **Status:** Implementation-ready
**Source PRD:** `specs/261003-teams-cli/teams-cli-PRD.md` (Draft v0.2) | **PRD review:** `prd-review.md` (gaps 1-9 resolved in section 4 below)

Conventions. "PRD s6" means PRD section 6. Goals G1-G5 are PRD s2. Milestones M0-M5 are PRD s13. The PRD's undefined "P1/P2" labels are replaced (decision D2). Every Graph endpoint shape is an assumption "unverified against a real tenant" and is listed in section 9; the code carries a comment `ASSUMPTION(unverified against a real tenant): UA-n` and a test named `TestAssumed...` for each.

## 1. Executive summary

`teams` is a Go CLI that lets an AI agent post to and read Microsoft Teams as its own named Entra user. It calls Microsoft Graph with delegated permissions, takes ~1 h Graph tokens from the `agent-okta-d` daemon (provider `msgraph`) through `agent-cli-core`'s `auth` package, enforces a typed client-side policy, and polls for inbound messages (no hosted component, no listener). Envelope, exit codes, bounds, untrusted marking, audit, HTTP retry and skill generation come from `github.com/stainedhead/agent-cli-core` **v0.1.0** and are not reimplemented.

This spec delivers M1-M3 (core commands, authorization and controls, threads) against fakes, plus the CI pipeline. M0 (real-tenant spikes), M4 hardening items that need external accounts, and M5 (Agent User) are not built; see section 10.

## 2. Problem statement

Agents need to post updates, answer @mentions and DMs and reply in threads. Graph application-only permissions cannot send chat messages, and a hosted bot relay is rejected. Agents run behind NAT and must never hold a Microsoft credential. Chat is an instruction channel, so the CLI must separate people who may instruct the agent from everyone else.

## 3. Goals and non-goals

Goals G1-G5: PRD s2 (restated in the trace table, section 5).

Non-goals / out of scope for this spec:
- Human mode, calls, meetings, voice, screen sharing, tabs, message extensions, bots.
- File upload/download and attachments. `--file` reads message **text** from a local file; it is not an upload.
- Real-time delivery, change notifications, **webhooks** (incoming webhook / Workflows notification path is deferred), hosting any service.
- Messaging external tenants or guests.
- **M0 real-tenant spikes.** A checklist document is delivered instead (`docs/m0-spike-checklist.md`); no spike is run.
- **Entra Agent User runtime.** Spike plan only (PRD s11), recorded in `docs/m0-spike-checklist.md` (S-10) and ADR; no code. Only the daemon would change (PRD s11), so the CLI is already compatible.
- **Native Windows.** Targets are darwin/arm64, linux/amd64, linux/arm64 (CGO off). Windows users run the Linux build under WSL2.
- Release workflows, signing, notarization, SBOM, attestations (PRD s16.2-16.4 REL-*). Deferred (`docs/deferred.md`); CI (BLD-1..6) is built. `make cross` proves the three targets compile.
- Signed policy files (M4). Policy trust is by file ownership (FR-021); signature verification is deferred.
- Real `agent-okta-d` client adapter (decision D10).

## 4. Decisions resolving PRD review gaps 1-9

Conservative defaults were chosen throughout. Each decision is also recorded in `docs/architectural-decision-record.md` by the implementation.

**D1 (gap 1) Requirement IDs.** Section 5 defines FR-1..FR-34, each traced to PRD sections and goals G1-G5.

**D2 (gap 2) P1/P2 replaced by milestones.** Idempotency marker scan (PRD s7 "P1") becomes optional behavior behind policy flag `send.marker_scan` (default `false`), built in M2, marked "built, unverified" (UA-12). File attachments, webhooks, group-based commanders are the PRD's "P2": **deferred, not built**.

**D3 (gap 3) Measurable thresholds.** Verified against fakes and fake clocks in CI; real-tenant values are M0 outputs.
- Poll latency: with `poll_interval` = I, a message that appears on a fake Graph is returned by `inbox --wait` within I + 1 s on the fake clock (jitter is bounded to +/-20% of I, never below the 5 s floor). Real-tenant latency is measured in S-1/S-3 and recorded in `docs/unverified-assumptions.md`.
- Graph calls per poll: at most 1 list call per watched destination per poll cycle when the destination has no new activity, at most 2 when it has (list + at most one continuation), asserted in tests (NFR-2).
- Local overhead: p95 below 50 ms per command excluding Graph, measured by a Go benchmark against in-memory fakes (informational, not a gate; the core records the same as "not measured").
- Kill-switch window: the CLI itself adds no caching of tokens beyond one process run, so the window is the daemon token lifetime plus CAE behavior. Target recorded as an **assumption, not a guarantee: at most 60 min** (UA-19), measured in drill S-8.
- Duplicate rate: zero duplicate sends when the state directory is intact and the same `--idempotency-key` is used; zero duplicate deliveries of an acked message while the cursor store is intact. Under state loss, see D7.

**D4 (gap 4) Inbound from non-policy conversations is DROPPED.** `inbox` reads only destinations present in policy with `watch: true`. It never enumerates the agent user's other chats. Therefore a message in an unlisted chat or channel can never surface, even if it @mentions the agent or comes from a commander. `inbound.handle` filters messages **within** watched destinations only:
- `direct`: messages in `user:<alias>` destinations (resolved 1:1 chats);
- `mentions`: messages that @mention the agent in a `chat:` or `channel:` destination;
- `watched`: every other message in a `chat:` or `channel:` destination with `watch: true`.
A message that matches none of the enabled categories is dropped. Dropped counts (never content) are reported in the envelope data trailer and audit (`dropped=n`). A sender who is not a commander still surfaces (it is a policy destination) with `can_instruct: false`. An `inbound.unlisted: surface` mode is **not built** (listed in `docs/deferred.md`). Consequence: a new human DM from an unlisted person is invisible until an operator adds a `user:` destination; documented in user-docs.

**D5 (gap 5) Mention resolution.** `--mention <alias>` accepts only `user:` aliases that appear in `send.mentions.allow`. The alias's policy entry supplies `aad_id` and a mandatory `display_name` (new policy field, required when the alias is mentionable). The CLI builds the Graph `mentions[]` entry and an `<at id="N">display_name</at>` HTML body from policy alone: **no user-lookup scope is requested** (no `User.ReadBasic.All`), no Graph call is made to resolve users. Channel, team and tag mentions are rejected (`block_broadcast: true` is enforced and cannot be set to `false` in v1; the field is accepted for schema compatibility but a `false` value is rejected at policy load, validation, exit 9). Message text is HTML-escaped before the mention markup is added.

**D6 (gap 6) `user:` 1:1 chat resolution.** Per `user:` destination, resolve once and cache the chat id in the cursor store (cache is a convenience, re-resolved when Graph returns 404 for it):
1. `GET /me/chats?$filter=chatType eq 'oneOnOne'&$expand=members` (paged, bounded to `limits.max_chat_scan`, default 50 pages of 50) and match the single other member whose `userId` equals the alias `aad_id`.
2. If none is found and policy `create_chat: true` for that destination (default `false`), `POST /chats` with `chatType: oneOnOne` and the two members. Graph is believed to return the existing chat when one exists (UA-6), so the call is safe to repeat.
3. If none is found and `create_chat` is false: exit 5 (`not_found`) with a hint naming the policy field. `send` and `inbox` treat this identically; `inbox` skips that destination (reports `skipped=alias:no_chat` in the trailer) and continues with the others.
Required scope: `Chat.ReadWrite` is assumed to cover create (UA-6); if M0 shows `Chat.Create` is separate, it joins the scope table (PRD s5) and `docs/requested-core-changes.md` is unaffected. `selftest` includes a row for resolving a listed `user:` alias.

**D7 (gap 7) State loss consequences.** State lives in `state_dir` (policy field, env `TEAMS_STATE_DIR`, default `/var/lib/agent-cli/teams`; must be a writable **persistent volume** in containers; created 0700; must not be on `tmpfs`-like ephemeral storage in production, documented in user-docs/installation).
- **Ledger loss** (idempotency + sent history): a retried command with the same `--idempotency-key` can post a **duplicate message**; rate and `reply_depth_max` counters restart at zero. Mitigation: optional marker scan (D2). Documented as accepted risk; there is no remote source of truth.
- **Cursor/ack store loss**: bounded re-read of up to `inbound.max_lookback` (default 30 m, cap 24 h) per destination; **already-acked messages are re-delivered** once. Consumers must treat message ids as idempotent keys.
- Both stores are written atomically (write temp, fsync, rename) under an advisory file lock; a corrupt file is renamed to `*.corrupt-<ts>` and treated as lost (fail open for cursors with the bounded re-read, **fail closed for the ledger**: a corrupt ledger blocks `send`/`reply` with exit 7 until an operator removes it, because silent reset would hide duplicates).

**D8 (gap 8) Delivery semantics: at-least-once with ack.**
- **Opaque ids.** Item `id` and `thread_id` are CLI ids that embed the destination alias: `id = "<alias>/<graph-message-id>"` (e.g. `channel:sdlc-alerts/1696341900000`), `thread_id = "<alias>/<root-message-id>"` (chats have no threads: `"<alias>/chat"`). Graph ids are therefore never ambiguous across destinations, and `ack`, `reply` and `thread get` recover the destination (and re-check policy) from the id alone.
- **Delivery index.** `inbox` does not move the watermark or the acked set, but it **records each delivered item** (alias, Graph id, thread root, `lastModified`, delivered-at) in the cursor store. `ack` accepts only ids present in that index.
- `inbox` returns every message in a watched destination that is newer than the destination watermark, not in the acked set (at the same or a newer `lastModified`), not authored by the agent, and within `max_lookback`. Un-acked messages are **re-delivered on every `inbox` call** until acked (or until they age past `max_lookback`, after which they are dropped with a count).
- `ack <id...>` marks delivered ids as acked and advances the per-destination watermark across the contiguous acked prefix (ordered by `lastModified`). An id not in the delivery index (never delivered, pruned, state lost, malformed, unlisted alias) is an error (exit 9) listing the ids and changes nothing; already-acked ids are a no-op (idempotent).
- A message edited after it was acked (Graph `lastModifiedDateTime` greater than the acked version) is **re-delivered** with `edited: true` and re-classified (`can_instruct` is recomputed from the current sender each delivery; deleted messages are skipped). This closes review item 13 for edits; chat membership changes mid-session are bounded by the per-run policy re-check and are otherwise a documented residual risk.
- `--since CURSOR` (opaque, taken from an item's `cursor` field) overrides the watermark and acked-set for that call only (replay); it is still bounded by `max_lookback`, still records deliveries, and does not change the watermark or acks.
- Delivery is therefore **at-least-once**; ordering is by `received` then id within a destination; no global order across destinations is promised. Delivery-index entries are pruned after `max_lookback` plus one hour.

**D9 (gap 9) Exit-code mapping via the core table.** `output.ExitFor(category)`; the CLI never invents numbers.

| Situation | Category | Exit |
|---|---|---|
| Success, including `inbox` with nothing new after `--wait` expires (empty array) | ok | 0 |
| Unexpected internal error; failed `selftest` (core: general); other Graph 5xx after bounded retries | general | 1 |
| Bad flags, unknown alias syntax, bad `--since`, invalid bounds (core `output` sentinels) | usage | 2 |
| Daemon unreachable (core message names the socket); `reauth_required`; revoked; second 401 after one forced refresh; Graph 401 | auth | 3 |
| Graph 403: not a member, consent missing, Teams policy block (selftest negative probe treats 403 as PASS) | forbidden | 4 |
| Graph 404 (chat/channel/message gone, or non-member chat where Graph answers 404; negative probe passes on 403 or 404); `user:` alias with no chat and `create_chat` false | not_found | 5 |
| Client policy denial: unlisted destination, send/watch not allowed, broadcast or unlisted mention, rate limit, `reply_depth_max`, `max_writes_per_run`, content filter hit, identity (UPN) mismatch | policy_denied | 6 |
| Idempotency key reused with a different payload; key `pending` (ambiguous earlier send); corrupt ledger; Graph 409 | conflict | 7 |
| Graph 429/503 after bounded retries (core `httpx.RateLimitedError`) | rate_limited | 8 |
| Policy file invalid/untrusted/missing; oversize message; malformed mention; empty text; unknown message id in `ack`; `dry_run` request invalid | validation | 9 |

Rate-limit denials by client policy are 6 (with `RetryAfter` in the hint); server throttling is 8. A `dry_run` of an otherwise allowed send exits 0.

**D10 Daemon client.** The core defines `auth.DaemonClient` and `auth.TokenSource`; the adapter over `agent-okta-d` `pkg/client` is unreleased. `cmd/teams/daemon.go` provides `newDaemonClient()` returning a stub whose `Fetch` and `Refresh` return `*auth.UnreachableError{Socket}` (message names the socket; exit 3), socket from `AGENT_OKTA_D_SOCKET` (default `/run/agent-okta-d/agent-okta-d.sock`, unverified UA-22). Replacing the stub is a one-function change. `agent-okta-d` is **never** in `go.mod`. Tests use `auth/authtest.Fake` through the same `auth.DaemonClient` seam. ADR: `docs/adr-daemon-client-stub.md` plus an entry in the ADR file; `docs/deferred.md` lists the real adapter and the socket ownership check (apply the policy-file trust rule to the socket path when it lands).

**D11 Core dependency.** `go.mod` requires `github.com/stainedhead/agent-cli-core v0.1.0` (released tag) and `github.com/goccy/go-yaml v1.19.2` (policy parsing, same version the core uses). No `replace`, no pseudo-versions. This supersedes AGENTS.md "do not add a require yet": v0.1.0 now exists, and AGENTS.md is updated in task F1. Gaps found in the core are written to `docs/requested-core-changes.md` with a workaround; the core is never edited.

**D12 Policy engine choice.** The core `policy` package is a generic verb/resource rule engine with its own schema, no typed destinations, senders or mentions, and per-process rate limits. Teams policy is a **typed domain model** (PRD s7 schema) evaluated in `internal/domain`, parsed strictly (unknown keys rejected, fail closed) by the policyfile adapter with go-yaml, mirroring the sibling `outlook`. Persistent counters (rate, loop guard) are derived from the ledger. Requests logged in `docs/requested-core-changes.md` (typed rules, standalone trusted-file check, audit extension fields, body-reading VendorCode hook).

**D13 Untrusted handling.** Every field that carries other people's free text is emitted as `output.Untrusted`: message text (`text`, author = sender display name, timestamp = created) **and `sender.name`** (display names are attacker-controlled; this is stricter than the PRD sketch, which shows a plain string). Because core `Untrusted` marshals `{untrusted, value, author, timestamp}`, the `text` object has those fields rather than only `{untrusted, value}`; extra fields are additive. The root skill is regenerated from docgen and the manual PR notes the shape (SKILL-4).

**D14 Identity guard.** On every network command, once per run, `GET /me` must return a `userPrincipalName` (case-insensitive) equal to policy `upn`; otherwise exit 6 before any other Graph call.

**D15 CLI framework.** Standard library `flag` with a small command router in `internal/adapters/cli` (as `outlook`). No cobra: fewer dependencies, and docgen takes a flat command list.

## 5. Functional requirements

Trace column: PRD sections and goals. Test column names the acceptance IDs in section 7.

| ID | Requirement | PRD / goal | AC |
|---|---|---|---|
| FR-1 | `teams whoami`: one `GET /me`; output agent display name, UPN, `id`, policy profile, destination aliases with capabilities (send/watch), limits, poll interval, policy path and build version. UPN mismatch exits 6 (D14). | s6, s7 / G1, G2 | AC-1 |
| FR-2 | `teams destinations list`: local only, no network. Items `{alias, kind, send, watch, mentionable}`; never prints raw ids. | s6 / G1 | AC-2 |
| FR-3 | Alias grammar: `channel:<name>`, `chat:<name>`, `user:<name>` with `<name>` = `[a-z0-9._-]{1,64}`; anything else (including raw ids, `19:...@thread`, GUIDs) is a usage error (exit 2) and an unknown alias is policy_denied (6). | s6, s9 / G1 | AC-3 |
| FR-4 | `teams send --to ALIAS (--text T \| --file F) [--thread ID] [--mention ALIAS...] [--idempotency-key K] [--dry-run]`. Exactly one of `--text`/`--file` (`-` = stdin). Chat alias -> `POST /chats/{id}/messages`; channel alias -> `POST /teams/{tid}/channels/{cid}/messages`; with `--thread` on a channel -> `.../messages/{id}/replies`. | s6 / G1 | AC-4 |
| FR-5 | Send checks in order: parse; UPN guard; destination listed and `send: true`; size `<= send.max_bytes` (UTF-8 bytes, checked before reading more than max+1 bytes from file/stdin); non-empty after trim; CR/NUL control chars rejected; mentions (FR-8); content filters (FR-9); link allowlist; rate and write caps (FR-10); loop guard (FR-11); ledger reserve (FR-14); post; ledger complete. First failure wins, with its category. | s7, s9 / G1, G3 | AC-4, AC-7 |
| FR-6 | `--dry-run`: run every check and print the decision, rendered payload preview (untrusted-marked), destination alias and resolved kind; make no Graph write, no ledger entry, no rate consumption. Exit 0 if allowed, otherwise the denial code. | s6 | AC-5 |
| FR-7 | `teams reply --thread ID --text T`: `ID` is an inbox item `thread_id` (`<alias>/<root>`, D8); the alias is parsed from it and must be a policy destination with `send: true` (else 6; malformed or unknown alias 9). No prior `inbox` call or local thread map is needed. Channel: `POST .../messages/{root}/replies`. Chat: ordinary chat message (chats have no reply threads, PRD s6). Same checks as FR-5, including loop guard keyed on the full `thread_id`. Accepts `--mention`, `--idempotency-key`, `--dry-run`. | s6 / G1, G3 | AC-8 |
| FR-8 | Mentions: D5. Plain text body is HTML-escaped; `contentType` is `html` only when mentions exist, else `text`. Max 5 mentions per message (policy `send.mentions.max`, default 5). Duplicates collapsed. | s7, s9 / G1 | AC-6 |
| FR-9 | Content filters: `secret_patterns` (built-in set: private key blocks, AWS access key ids, GitHub tokens, bearer/JWT-looking strings, Slack tokens, generic `password|secret|token\s*[:=]\s*\S{8,}`; plus the core `output` redaction is not used for this) and `classification_markers` (policy list `send.classification_markers: [...]`, case-insensitive substring; empty list = filter inactive and `selftest` warns). A hit refuses the send (exit 6) and the error/audit names the filter and pattern id, **never the matched text**. | s7, s9 / G1 | AC-7 |
| FR-10 | Rate and write caps: `send.rate.per_minute` / `per_hour` (sliding windows over ledger sent history, success and pending only) and `limits.max_writes_per_run` (per process). Denial carries retry-after in the hint. Defaults when absent: 10/min, 100/h, 30 per run (PRD s7 sample values; reconciliation with Teams throttling is a documented assumption UA-14). | s7, s9 / G1 | AC-9 |
| FR-11 | Loop guard: before a send/reply into thread T, count agent sends to T within `send.reply_window` (default 24 h) in the ledger; `>= reply_depth_max` (default 6) -> exit 6. Messages from `is_agent` senders never gain `can_instruct`. | s7, s9 / G3 | AC-10 |
| FR-12 | `link_allowlist`: if non-empty, every `http(s)://` host in the text (and every `<a>` href) must match an entry (exact or `*.suffix`); otherwise exit 6. Empty list = unrestricted. | s7 | AC-7 |
| FR-13 | `send.prefix`: prepended verbatim (counts toward `max_bytes`). Empty by default. | s7 | AC-4 |
| FR-14 | Idempotency ledger (D7). `--idempotency-key K` (1-128 chars `[A-Za-z0-9._:-]`): states `pending`, `sent`, `failed`. Reserve before posting. Same key + same payload hash + `sent` -> return the recorded result, exit 0, `deduplicated: true`, no post. Same key + different payload -> exit 7. Key `pending` -> exit 7 with hint (ambiguous earlier send; operator may clear or use the marker scan). Key `failed` (NotSent: auth, 429, 4xx before processing) -> retry allowed. Sends without a key are still recorded in sent history (for FR-10/11) but never deduplicated. Writes are never auto-retried (never marked safe to retry in `httpx`). Retention of `sent`/`failed` entries 90 days; `pending` never expires. | s7 / G1 | AC-11 |
| FR-15 | Optional marker scan (D2, UA-12), `send.marker_scan: true`: before re-posting a `pending` key, list the last 20 messages of the destination and look for the marker embedded in the previous post (`<span data-teams-cli-key="HASH"></span>` in HTML bodies, hash = first 16 hex of SHA-256 of key+alias). Found -> mark `sent`, exit 0. Not found or inconclusive (400, attribute stripped) -> fall back to exit 7. Default off. | s7 / G1 | AC-11 |
| FR-16 | `teams inbox [--wait N] [--limit N] [--since CURSOR]`: D4/D8. `--wait` default 0 (single poll), max `inbound.max_wait` (default 120 s); `--limit` default 20, capped by `limits.max_results` (default 50). Loops at `inbound.poll_interval` (default 15 s, floor 5 s, jitter +/-20%) until at least one item or `N` elapsed. Records each returned item in the delivery index (D8) and returns a JSON array of items (FR-17) so core bounding works; dropped and skipped counts (never content) go to the audit record (`dropped=n`, `skipped=alias:reason`), not the data. | s6 / G1, G5 | AC-12 |
| FR-17 | Inbound item (D13): `id` and `thread_id` (alias-qualified, D8), `received` (RFC 3339 UTC), `cursor`, `edited`, `conversation{type: channel\|chat\|user, alias}`, `sender{name (untrusted), aad_id, can_instruct, is_agent}`, `mentioned_you`, `text` (untrusted, HTML converted to text; URLs found in the text or in `<a href>` are also listed in `links[]` as plain strings, never fetched; images are never fetched). Non-user senders (bots, connectors, applications): `aad_id: ""`, `can_instruct: false`, `is_agent: false`. | s6 / G3 | AC-13 |
| FR-18 | Own-message exclusion: any message whose `from.user.id` equals the agent's own id (from `GET /me`, once per run) is never returned. | s6 / G3 | AC-12 |
| FR-19 | Sender classification (pure domain function): `can_instruct = aad_id in instruct.commanders.aad_ids`, only if `aad_id` non-empty and (policy `tenant_id` unset or sender tenant equals it). `is_agent = aad_id in instruct.agents.aad_ids`. An id listed in both is `is_agent: true, can_instruct: true` only if explicitly in commanders; agents never gain instruct otherwise. Display names are never an input. | s7, s9 / G3 | AC-13 |
| FR-20 | `teams ack <id...>`: D8. 1-100 alias-qualified ids from the delivery index; unknown id exits 9 and nothing changes. Output `{acked: n, already: m}`. | s6 | AC-14 |
| FR-21 | Policy file: path `TEAMS_POLICY` env else `/etc/agent-cli/teams.policy.yaml`. Trust: the file and every ancestor directory owned by root (or a configured trusted uid, never the effective uid), not group- or world-writable, opened with `O_NOFOLLOW` and `fstat` on the descriptor; otherwise exit 9 and nothing runs. Strict YAML (unknown/duplicate keys rejected). Tests inject the trust check (`AllowUntrusted` option) so temp dirs work; release builds cannot enable it (build-tag, as `outlook`). | s7, s9 / G3 | AC-15 |
| FR-22 | Policy schema (section 6) with defaults and validation (version, profile, upn required; destinations non-empty; each destination exactly the id fields for its kind; aliases unique and valid; commander/agent ids are GUIDs; numeric limits non-negative; poll_interval >= 5 s). | s7 | AC-15 |
| FR-23 | `teams thread get ID [--limit 20]`: `ID` is a `thread_id` (`<alias>/<root>`); the alias must be a destination with `watch: true`. Returns the most recent `limit` messages (cap `limits.max_results`) oldest-first as inbound items. Channel: `GET .../messages/{root}/replies`; chat: recent chat messages. Same classification and untrusted marking; does not touch cursors, acks or the delivery index. | s6 / G3 | AC-16 |
| FR-24 | `teams selftest` (core `selftest` runner): rows listed in section 8; tenant-touching rows run only on explicit invocation; `--read-only` skips write rows (core `ReadOnly`). Failing exit 1. | s10 / G3, G4 | AC-17 |
| FR-25 | `teams version`: semver, commit, build date via `-ldflags -X main.version/commit/date` (REL-4); no network, no policy load. | s16 REL-4 | AC-18 |
| FR-26 | Token path: `auth.NewDaemonTokenSource(newDaemonClient(), "msgraph", WithRemediation("a human must run: agent-okta-d enroll msgraph"))` -> `auth.NewAuthorizer` -> `httpx.NewClient(Config{Refresher, AllowedHosts: graph.microsoft.com})`. 401 -> one forced refresh (core httpx) then exit 3; `reauth_required`/revoked -> exit 3 with the human-actionable message. No credentials stored, no `token` command. | s5, s6 / G1, G4 | AC-19 |
| FR-27 | Throttling: reads retried by core `httpx` (429/503, `Retry-After`, jitter); `inbox` additionally widens its wait by `Retry-After` and never polls faster than the floor; writes never retried. Exhausted -> exit 8. | s6, s9 | AC-20 |
| FR-28 | Audit: one JSONL record per command via core `audit` (path from policy `audit.path`; mode 0600): verb, resource alias (never raw ids), outcome, http status, duration, policy decision `allow` / `deny:<reason>` plus `;key=value` suffixes (`count`, `dropped`, `message_id`, `deduplicated`). No bodies, no tokens. Audit write failure: warn for reads, **block writes** (`WriteFailureMode` = Block for send/reply). | s12 / G2, G4 | AC-21 |
| FR-29 | Output via core: envelope JSON by default, `--format json\|table\|text`, `--max-bytes`, `--offset`; list commands emit a JSON array as `data`; errors via `output.FromError`; exit code from `Envelope.ExitCode()`. | s6 | AC-22 |
| FR-30 | Generated skill: hidden command `teams skill` prints `docgen.Generate` output for the command tree; `make skill` writes `dist/teams-cli.md`. The root `skills/teams-cli.md` is updated by manual PR (PRD s17.1, task W6). | s17 SKILL-1..7 | AC-23 |
| FR-31 | Secrets hygiene: no code path logs, prints or persists a token; `auth.Token` is never converted to string; tests scan envelopes, audit lines, errors and state files for a planted token string. | s9 / G1 | AC-24 |
| FR-32 | CI: `.github/workflows/ci.yml` implements BLD-1..6 (gofmt, `go mod tidy` no diff, vet, golangci-lint pinned, `go test -race`, govulncheck, cross-compile 3 targets, dependency auth per DEP-1..6). PR CI uses fakes only. | s16.1, s16.6 | AC-25 |
| FR-33 | Docs: `docs/` (product summary/details, technical details, ADR, deferred, unverified assumptions, M0 checklist, requested core changes) and `user-docs/` (install, getting started, policy reference with sample policy, usage, troubleshooting). `user-docs/` never links to `specs/`. | AGENTS.md | AC-26 |
| FR-34 | Kill-switch behavior: any Graph 401 after refresh, `reauth_required`, revoked, or 403 on `GET /me` yields exit 3/4 on the first command; a runbook section in `docs/m0-spike-checklist.md` (S-8) defines the drill. | s9 / G4 | AC-19 |

## 6. Policy model

```yaml
version: 1
profile: agent
upn: sdlc-reviewer-01@corp.example.com        # checked against GET /me (D14)
tenant_id: ""                                  # optional GUID; senders from another tenant never instruct
state_dir: /var/lib/agent-cli/teams            # D7; env TEAMS_STATE_DIR overrides
destinations:
  channel:sdlc-alerts: { team_id: "...", channel_id: "...", send: true,  watch: true }
  chat:dev-team:       { chat_id: "19:...@thread.v2",       send: true,  watch: true }
  user:jane.doe:       { aad_id: "...", display_name: "Jane Doe", send: true, watch: true, create_chat: false }
instruct:
  commanders: { aad_ids: ["..."] }
  agents:     { aad_ids: ["..."] }
inbound:
  handle: [direct, mentions, watched]
  max_lookback: 30m          # cap 24h
  poll_interval: 15s         # floor 5s
  max_wait: 120s
send:
  max_bytes: 8000
  mentions: { allow: [user:jane.doe], block_broadcast: true, max: 5 }
  content_filters: [secret_patterns, classification_markers]
  classification_markers: ["CONFIDENTIAL", "INTERNAL ONLY"]
  link_allowlist: []
  rate: { per_minute: 10, per_hour: 100 }
  reply_depth_max: 6
  reply_window: 24h
  prefix: ""
  marker_scan: false
limits: { max_results: 50, max_writes_per_run: 30, max_chat_scan: 50 }
audit: { path: /var/log/agent-cli/teams.audit.jsonl }
```

Evaluation rules (pure functions in `internal/domain`): deny by default; an alias absent from `destinations` is denied for every verb; `send` needs `send: true`; `inbox` and `thread get` need `watch: true`; mention allow-list entries must be `user:` aliases that exist in `destinations` with `display_name`; a policy that fails validation never loads (exit 9). Policy is a guardrail, not the authorization boundary: Teams membership and Graph consent are (PRD s4, s14.4).

## 7. Acceptance criteria

All verified with fakes (`authtest.Fake`, an `httptest` fake Graph, a fake clock); no PR test touches a real tenant. "Milestone" ties to PRD s13.

| ID | Criterion | Milestone |
|---|---|---|
| AC-1 | `whoami` returns identity and policy summary; a UPN mismatch exits 6 and no other Graph call is made. | M1 |
| AC-2 | `destinations list` prints aliases only, no ids, no network. | M1 |
| AC-3 | Raw ids, malformed and unknown aliases are rejected with exit 2 / 6; table-driven over >= 15 inputs. | M1 |
| AC-4 | `send` to an allowed chat and an allowed channel hits the exact fake-Graph path with a bearer header set only by the authorizer; prefix and size rules hold. | M1 |
| AC-5 | `--dry-run` makes zero write calls, zero ledger entries, and reports the same decision as a real send. | M1 |
| AC-6 | Mentions build correct `mentions[]` and `<at>` HTML from policy only; broadcast, unlisted and over-limit mentions exit 6; text is HTML-escaped. | M2 |
| AC-7 | Oversize, secret-pattern, classification-marker, link-allowlist and empty/control-character messages are refused with the right category and without echoing the matched text. | M2 |
| AC-8 | `reply` posts to a channel thread (replies endpoint) and to a chat (plain message); a thread from an unlisted or send-denied destination is refused. | M3 |
| AC-9 | Rate (minute/hour sliding window on the fake clock) and per-run write caps deny with exit 6. | M2 |
| AC-10 | Loop simulation between two agent identities stops at `reply_depth_max`; agent senders never `can_instruct`. | M2 |
| AC-11 | Idempotency: replay returns the recorded result with zero extra posts; changed payload exits 7; ambiguous failure leaves `pending` and the next attempt exits 7; `NotSent` failures allow retry; corrupt ledger blocks writes; marker scan (flagged) resolves a pending key on the fake. | M2 |
| AC-12 | `inbox` returns new messages from watched destinations only, excludes own messages, honors `--limit`, and with `--wait` returns within `poll_interval + 1 s` of fake-clock time of a message appearing; zero-result wait returns `[]` with exit 0. | M1 |
| AC-13 | Prompt-injection corpus (>= 20 strings including fake "system" text and display names equal to a commander's) always arrives as `untrusted`; a non-commander is never `can_instruct`; spoofed display names do not matter; non-user senders never instruct; cross-tenant sender never instructs when `tenant_id` set. | M2 |
| AC-14 | Ack semantics (D8): un-acked messages re-delivered; acked not; watermark advances over contiguous prefix; unknown id exits 9; edited-after-ack re-delivered with `edited: true`; `--since` replays without changing state. | M1 |
| AC-15 | Policy loader: untrusted file (group-writable, agent-owned, symlink) refused; unknown keys, duplicates, bad ids, `block_broadcast: false` rejected; defaults applied. | M1 |
| AC-16 | `thread get` returns bounded, classified context without changing cursors. | M3 |
| AC-17 | `selftest` matrix (section 8) passes on the fake and a deliberately broken policy fails the right row. | M2 |
| AC-18 | `teams version` prints stamped values and runs without policy or network. | M1 |
| AC-19 | Daemon stub -> exit 3 with message naming the socket; `authtest` scenarios `ReauthRequired`, `Revoked`, `UnauthorizedThenSuccess` (one refresh, then success), `UnauthorizedTwice` (exit 3) produce the section 4 D9 codes. | M1 |
| AC-20 | Fake Graph 429 with `Retry-After`: reads retry and succeed or exit 8; a 429 on a write posts nothing extra and leaves the ledger `failed` (NotSent); poll interval never below the 5 s floor. | M1 |
| AC-21 | One audit line per command; fuzz/property test: no token, message body, or raw id in any line; write blocked when the audit sink fails. | M2 |
| AC-22 | JSON, table and text render; truncation yields `next_offset` and resumes; errors are never truncated. | M1 |
| AC-23 | `teams skill` output is deterministic and lists every command with accurate flags and the forbidden actions. | M3 |
| AC-24 | Planted-token test finds the token string nowhere in output, audit, state files or errors. | M2 |
| AC-25 | CI workflow passes on a PR; `make cross` builds darwin/arm64, linux/amd64, linux/arm64. | M1 |
| AC-26 | Docs exist, accurate to the built behavior; user-docs has no link into `specs/`. | M3 |

Quality gates before each commit: `gofmt -l .` empty, `go vet ./...`, `golangci-lint run`, `go test -race ./...`, `go mod tidy` no diff; `govulncheck` in CI. Statement coverage target: >= 85% for `internal/domain` and `internal/usecase`, >= 70% elsewhere (informational for adapters).

## 8. Selftest matrix (FR-24)

Core `selftest.Row` list; each row is `Expect` Allow or Deny and a probe in `internal/adapters/selftestcfg`.

| Row | Verb / resource | Expect | Read-only |
|---|---|---|---|
| identity | `GET /me` UPN equals policy | Allow | yes |
| send-allowed | send to the first `send: true` destination alias with a clearly marked test message (`selftest <run-id>`) | Allow | no |
| send-unlisted | send to `chat:__selftest_unlisted__` | Deny | yes (policy-only) |
| broadcast-mention | send with a channel-wide mention | Deny | yes |
| oversize | text of `max_bytes + 1` | Deny | yes |
| secret-pattern | text containing a canned fake AWS key | Deny | yes |
| classification | text containing the first configured marker (skipped with a warning if none) | Deny | yes |
| mention-unlisted | mention of an alias not in `mentions.allow` | Deny | yes |
| user-chat-resolve | resolve the first `user:` destination (D6) | Allow | yes |
| negative-membership | `GET /chats/{id}` of the config-provided `selftest.non_member_chat_id` (policy field, optional; row skipped if absent); Deny means 403 or 404 | Deny | yes |
| inbox-read | poll the first watched destination, no ack | Allow | yes |

Policy-only rows never contact Graph. The negative row passes on 403 **or** 404 (D9). `--read-only` skips only `send-allowed`.

## 9. Unverified assumptions (unverified against a real tenant)

Everything the PRD marks with the warning sign, plus Graph shapes from general knowledge. Tracked in `docs/unverified-assumptions.md` with code and test references and the M0 spike that settles each.

| ID | Assumption | PRD |
|---|---|---|
| UA-1 | Graph v1.0 shapes: `POST /chats/{id}/messages`, `POST /teams/{t}/channels/{c}/messages`, `.../messages/{id}/replies` (POST and GET), body/mentions JSON, `GET /chats/{id}/messages` with `$top`, `$orderby=lastModifiedDateTime desc`, `$filter=lastModifiedDateTime gt ...`. | s1, s6 |
| UA-2 | `GET /me` `userPrincipalName` matches policy `upn`. | s7 |
| UA-3 | Channel message **delta** works with delegated tokens (`GET .../channels/{c}/messages/delta`); fallback is list + client-side filter. Top-level channel messages only; replies surface only for threads the agent has posted in (ledger thread list, up to `inbound.thread_poll_max`, default 5). | s6 |
| UA-4 | `ChannelMessage.Read.All` + `Channel.ReadBasic.All` need admin consent; absent consent -> 403 on channel read (exit 4, hint). | s5, s14.6 |
| UA-5 | Delegated `Chat.ReadWrite` + `ChannelMessage.Send` suffice to send; `from.user.id` is present and `from.user.tenantId` exposes the sender tenant for external users. | s5, s9 |
| UA-6 | Creating a `oneOnOne` chat returns the existing chat when one exists; `Chat.ReadWrite` covers creation. | s7 |
| UA-7 | Message `lastModifiedDateTime` changes on edit; deleted messages carry `deletedDateTime`; `mentions[]` identifies the agent by `mentioned.user.id`. | s6 |
| UA-8 | Graph returns 403 or 404 (either) for a chat the agent is not in. | s10 |
| UA-9 | Embedded `<span data-teams-cli-key>` markers survive in the message body (marker scan only). | s7 |
| UA-10 | Teams HTML bodies may be sanitized; HTML-to-text conversion handles `<at>`, `<p>`, `<br>`, `<a>`, `<blockquote>`, `<img>` (images ignored). | s6 |
| UA-11 | 403 vendor error code is not available from headers (body only); the CLI reports `x-ms-error-code`/`request-id` when present. | s7 AUTH-3 |
| UA-12 | Chat membership changes mid-session are not detected until Graph rejects (403/404). | s14 |
| UA-13 | Conditional Access tolerates non-interactive refresh; re-enrollment interval acceptable. | s5, s8 |
| UA-14 | Default `per_minute: 10` / `per_hour: 100` stay under Teams message throttling for one user. | s7, s9 |
| UA-15 | Kill-switch propagation <= 60 min (D3). | s9 |
| UA-16 | `create_chat` rows and `Chat.Create` scope questions (D6). | s7 |
| UA-17 | Commander lists as static AAD ids suffice (open question 4). | s7, s15 |
| UA-18 | Teams retention/eDiscovery covers agent messages as normal user messages. | s8 |
| UA-19 | The daemon honors the 401 -> refresh semantics modeled by `authtest`. | s5 |
| UA-20 | Skill format required by each harness (SKILL-6). | s17 |
| UA-21 | Teams channel `Retry-After` semantics and message-API throttling limits. | s9 |
| UA-22 | Default daemon socket path `/run/agent-okta-d/agent-okta-d.sock`. | s5 |

## 10. Scope of changes (files)

Create (full layout in `architecture.md`): `cmd/teams/{main.go,app.go,daemon.go,devpolicy_dev.go,devpolicy_release.go}`; `internal/domain/*`; `internal/usecase/*`; `internal/adapters/{graph,cli,policyfile,state,auditlog,selftestcfg}/*`; `internal/infra/{clock,fstrust,config}/*`; `internal/archtest`; `.github/workflows/ci.yml` (update); `docs/*`, `user-docs/*`, `user-docs/teams.policy.sample.yaml` (single sample; WS-D keeps a testdata copy checked for equality in Phase I); `Makefile` (cross, skill, race, cover, lint); `go.mod`/`go.sum`; AGENTS.md (remove the "no require yet" rule); README status line. Unchanged: `teams-cli-PRD.md` (never rewritten), INTENT.md (no shift of goal or scope). Root repository `skills/teams-cli.md`: updated by manual PR from the generated skill (not edited in this repo).

External dependencies: `github.com/stainedhead/agent-cli-core` v0.1.0 (packages `output`, `auth`, `auth/authtest`, `httpx`, `audit`, `selftest`, `docgen`), `github.com/goccy/go-yaml` v1.19.2, Microsoft Graph v1.0 over plain HTTP (no Graph SDK).

## 11. Non-functional requirements

- NFR-1 Portability: static Go binary, darwin/arm64, linux/amd64, linux/arm64, CGO off; no listeners; outbound HTTPS to `graph.microsoft.com` only (core `AllowedHosts`).
- NFR-2 Polling cost: at most 1 list call per watched destination per cycle when idle, at most 2 when active (D3).
- NFR-3 Local overhead under 50 ms p95 (informational benchmark).
- NFR-4 Reliability: bounded re-read on state loss; atomic state writes; reads retried, writes never.
- NFR-5 Security: no credential on disk; `auth.Token` opaque; destination allow-list; untrusted marking; secret filter; broadcast mention block; trust-checked policy; audit without bodies.
- NFR-6 Observability: audit JSONL correlates with Entra non-interactive sign-in logs by time and UPN (documentation only).
- NFR-7 Concurrency: two `teams` processes on one state dir are safe (advisory flock, 10 s timeout then exit 7 "state busy").

## 12. Edge cases and error paths

External API failures: 401 (one refresh, then 3), 403 (4), 404 (5; for cached `user:` chat ids, drop the cache and re-resolve once), 409 (7), 429/503 (retry, then 8), network error on read (retry, then 1), network error or timeout on write (ambiguous: ledger stays `pending`, exit 1 with hint "outcome unknown", next attempt with the same key exits 7), malformed JSON (1, no body echoed), truncated paging (`@odata.nextLink` followed only to the same host, max 5 pages).
Empty/null inputs: empty text, whitespace-only text, empty file, empty `--to`, zero ids to `ack`, `--limit 0`/negative, `--wait` above max (clamped with a warning field in audit), `--since` malformed (2), Graph messages with null `from`, null `body`, null `lastModifiedDateTime` (skipped with a count), deleted messages (skipped), system event messages (`messageType != message`, skipped).
Concurrent writes: ledger/cursor file lock; two sends with the same key: second sees `pending` -> 7.
Permission boundaries: non-member chat (4/5), missing channel-read consent (4 with hint), unlisted alias (6), mention of a non-listed user (6), cross-tenant sender (never instructs), guest/external participation in a listed chat is not checked client-side beyond the cross-tenant `can_instruct` rule (Teams external-access policy owns blocking it; PRD s8.5).
Other: clock skew (watermark compares Graph timestamps only; wall clock only for `max_lookback` age), very large Graph pages (`$top` capped at 50), output over 32 KiB (core truncation, `next_offset`), SIGINT during `--wait` (context cancel, exit 1, no state change), stdin `--file -` when stdin is a TTY (usage error).

## 13. Open questions

| # | Question | Owner | Resolution path |
|---|---|---|---|
| OQ-1 | PRD s15 items 1-7 (day-one scope, licensing, enrollment/CA, commanders, agent-to-agent, retention/DLP, Agent 365 licensing) | Enterprise Architecture / platform admins | Operator decisions; commanders are config (static list, UA-17) |
| OQ-2 | Apple Developer ID, registry (ghcr vs ECR), version-bump rule, WSL arm64, reusable workflow (PRD s16.8) | Release owner | Release workflows deferred until decided |
| OQ-3 | Should `--file` be confined to a directory (`send.file_roots`)? A prompt-injected agent can send any readable file's text to an allowed destination; filters and destination allow-list are the only brake. Default: unconfined, documented, deferred (same as outlook OQ-3). | Security | Decision before M4 |
| OQ-4 | `inbound.unlisted: surface` mode for DMs from unlisted people | Product | Revisit after M0 data |
| OQ-5 | Retry vs. throttling reconciliation for rate defaults (UA-14) | Platform | M0 spike S-6 |
| OQ-6 | Real daemon adapter timing (D10) | agent-okta-d owner | When `pkg/client` is tagged |

## 14. Risks and mitigation

PRD s9 and s14 apply. Spec-level additions: Graph shape drift (isolated in one adapter package behind ports, `TestAssumed*` tests flag deviations); state loss causing duplicates (D7, documented); channel replies not polled (UA-3, narrow v1 contract); policy as sole client guardrail (membership is the true boundary); unverified edit/deletion semantics (UA-7).

## 15. Timeline and milestones

M1 (core commands) -> M2 (authorization and controls) -> M3 (threads, docs, skill, CI) as phases in `plan.md`. M0, M4 (signed policy, release signing, kill-switch drill) and M5 are not part of this build; `docs/m0-spike-checklist.md` carries M0/M5 and the S-8 drill.

## 16. References

- Source PRD and review: `teams-cli-PRD.md`, `prd-review.md` (this directory)
- `agent-cli-core` v0.1.0: README, `docs/technical-details.md`
- Sibling `outlook-cli` (composition root, graph client, ledger, selftest, docs/requested-core-changes.md)
- Root skill: `agentic-teams/skills/teams-cli.md`
