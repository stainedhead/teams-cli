# Architectural Decision Record

Decisions are numbered ADR-n. Each lists context, decision and consequences. Source decisions (D-numbers) are in `specs/261003-teams-cli/spec.md` section 4; the architecture summary is in `specs/261003-teams-cli/architecture.md` section 12.

| ID | Title | Status |
|---|---|---|
| ADR-1 | Delegated user model | Accepted |
| ADR-2 | Typed domain policy instead of the core `policy` package | Accepted |
| ADR-3 | At-least-once inbox with explicit ack | Accepted |
| ADR-4 | Drop inbound messages from unlisted conversations | Accepted |
| ADR-5 | Policy-only mentions | Accepted |
| ADR-6 | Daemon client stub | Superseded by ADR-6a |
| ADR-6a | Daemon adapter wired | Accepted |
| ADR-7 | Standard-library `flag` CLI | Accepted |
| ADR-8 | Ledger fails closed, cursors fail open | Accepted |
| ADR-9 | Poll only listed destinations (no chat discovery) | Accepted |
| ADR-10 | Untrusted sender names and identity guard | Accepted |
| ADR-11 | Trust-by-ownership policy file; dev override only in a tagged build | Accepted |

## ADR-1: Delegated user model

Status: Accepted

Context: The agent must post to Teams as an identity that a tenant can govern, audit and disable. Application permissions can read or post across the whole tenant and cannot post to chats as a user.

Decision: Use delegated Microsoft Graph tokens for a dedicated agent user, enrolled once by a human and refreshed by the `agent-okta-d` daemon (provider `msgraph`). Scopes are limited to those in PRD s5. The CLI never holds long-lived credentials.

Consequences: The agent can reach only chats and channels the agent user is a member of; membership is the true boundary. Kill-switch is disabling the Entra user (window assumed at most 60 minutes, UA-15, drilled in S-8). Re-enrollment under Conditional Access is an operational cost (UA-13, S-4). Entra Agent User is a later runtime alternative (S-10); only the daemon would change.

Source: PRD, spec D3

## ADR-2: Typed domain policy instead of the core `policy` package

Status: Accepted

Context: The core `policy` package is a generic verb/resource/field rule engine with its own schema, no typed destinations, senders or mentions, and in-memory per-process rate limits. Every CLI run is a new process.

Decision: Model Teams policy as a typed structure in `internal/domain` (PRD s7 schema), parsed strictly by the policy-file adapter with go-yaml (unknown keys rejected, fail closed) and evaluated in the domain. Persistent counters (rate, loop guard) are derived from the idempotency ledger.

Consequences: Rules are clear and testable. Parsing and trust checks are partly duplicated from the core; the gaps are listed in `requested-core-changes.md` (typed rules, standalone trusted-file check, audit extension fields).

Source: spec D12

## ADR-3: At-least-once inbox with explicit ack

Status: Accepted

Context: Polling cannot know whether the caller processed a message. Silently advancing a cursor on read could lose instructions after a crash.

Decision: `inbox` records each delivered item in a delivery index and does not move the watermark. `ack` accepts only delivered ids and advances the watermark across the contiguous acked prefix. Unacked messages are re-delivered until acked or older than `inbound.max_lookback`. Edited messages are re-delivered with `edited: true`. Item ids embed the destination alias.

Consequences: Delivery is at-least-once; consumers treat ids as idempotent keys. Loss of the cursor store causes a bounded re-read and one re-delivery of acked messages. No global ordering across destinations is promised.

Source: spec D8

## ADR-4: Drop inbound messages from unlisted conversations

Status: Accepted

Context: Reading every chat of the agent user would expose instructions from conversations that no operator approved, widening the prompt-injection surface.

Decision: `inbox` reads only policy destinations with `watch: true`. Within them, `inbound.handle` selects direct, mention and watched categories; anything else is dropped and only a count is reported. An `inbound.unlisted: surface` mode is not built (see `deferred.md`).

Consequences: A new direct message from an unlisted person is invisible until an operator adds a `user:` destination. This is documented for operators.

Source: spec D4

## ADR-5: Policy-only mentions

Status: Accepted

Context: Resolving user names through Graph would need a directory read scope and would let the agent address arbitrary users.

Decision: `--mention <alias>` accepts only `user:` aliases listed in `send.mentions.allow`. The alias entry supplies the AAD id and a required display name; the CLI builds `mentions[]` and the `<at>` markup from policy alone. Channel, team and tag mentions are rejected (`block_broadcast` cannot be set to false in v1). Message text is HTML-escaped before markup is added.

Consequences: No user-lookup scope (for example `User.ReadBasic.All`) is requested and no Graph call resolves users. Operators maintain display names in policy.

Source: spec D5

## ADR-6: Daemon client stub

Status: Superseded by ADR-6a

Context: `agent-okta-d` has not published `pkg/client` and must not be a dependency of this module.

Decision: `newDaemonClient()` returns a stub that fails with `*auth.UnreachableError{Socket}` (exit 3). Full text in `adr-daemon-client-stub.md`.

Consequences: Replacing the stub is a one-function change. No command reaches Graph until then. Tracked in `deferred.md`.

Source: spec D10

## ADR-6a: Daemon adapter wired

Status: Accepted (supersedes ADR-6)

Context: `agent-cli-core` v0.2.1 provides `auth/oktad`, the `auth.DaemonClient` over the `agent-okta-d` socket.

Decision: `newDaemonClient()` returns `oktad.New` with a 10 s timeout; socket from `AGENT_OKTA_D_SOCKET`, else the adapter default. Full text in `adr-daemon-adapter-wired.md`.

Consequences: Graph-backed commands reach a real daemon. Degraded daemon exits 8, unreachable or not enrolled exits 3. Socket ownership check still deferred.

## ADR-7: Standard-library `flag` CLI

Status: Accepted

Context: The core docgen takes a flat command list, and the sibling `outlook` CLI avoids a CLI framework.

Decision: Use the standard library `flag` package with a small command router in `internal/adapters/cli`. Nested commands are named with a space (for example `thread get`). No cobra.

Consequences: Fewer dependencies. Help and completion are hand-written and covered by tests.

Source: spec D15

## ADR-8: Ledger fails closed, cursors fail open

Status: Accepted

Context: State files can be lost or corrupted. A silent reset of sent history would hide duplicate sends, while a lost cursor only causes a re-read.

Decision: State files are written atomically under an advisory lock. A corrupt file is renamed to `*.corrupt-<timestamp>` and treated as lost. A corrupt idempotency ledger blocks `send` and `reply` with exit 7 until an operator removes it. A corrupt cursor or ack store falls back to a bounded re-read up to `inbound.max_lookback`.

Consequences: Ledger loss can cause a duplicate send on retry (mitigated by the optional marker scan, UA-9). Cursor loss causes one re-delivery of acked messages. `state_dir` must be a persistent volume.

Source: spec D7

## ADR-9: Poll only listed destinations (no chat discovery)

Status: Accepted

Context: Discovering conversations with `GET /me/chats` would enumerate chats the operator never approved and cost extra Graph calls.

Decision: Poll only destinations in policy with `watch: true`, at most one list call per destination per cycle when idle and two when active. A `user:` alias is resolved once to a chat id (cached, re-resolved on 404); a chat is created only when `create_chat: true`.

Consequences: Latency and throttling depend on the number of watched destinations. Chat resolution still lists the agent user's one-to-one chats, bounded by `limits.max_chat_scan` (UA-16).

Source: spec D4, D6, NFR-2

## ADR-10: Untrusted sender names and identity guard

Status: Accepted

Context: Display names and message text are attacker-controlled, and a misconfigured policy could point the CLI at the wrong Entra user.

Decision: Emit both `text` and `sender.name` as `output.Untrusted`; authorize only by the sender's Entra object id (optionally constrained by `tenant_id`); never use names or text. Once per run, require Graph `/me` `userPrincipalName` to equal the policy `upn`, otherwise exit 6 before any other call.

Consequences: Output shape differs from the PRD sketch (`text` carries `author` and `timestamp` as well). The root skill documents the shape. Source: spec D13, D14.

## ADR-11: Trust-by-ownership policy file; dev override only in a tagged build

Status: Accepted

Context: The policy is the agent's guardrail and must not be editable by the agent. The core exposes no standalone trusted-file check, and signed policies are not built.

Decision: The policy file and all ancestors must be owned by root or a configured trusted uid, not group or world writable, opened with `O_NOFOLLOW` and verified with `fstat`; otherwise exit 9. The untrusted-file bypass `TEAMS_POLICY_INSECURE=1` exists only in a build with the `teamsdev` tag; release builds ignore it.

Consequences: Developers need a tagged build or a root-owned test policy. Signature verification stays deferred (M4). Source: spec D12, FR-21.

