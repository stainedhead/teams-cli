# Research: teams-cli
**Date:** 2026-10-04 | **Source PRD:** teams-cli-PRD.md | Nothing here was verified against a real tenant; see spec s9.

## Research questions and findings

1. **Graph v1.0 shapes (chat send, channel send, channel reply, chat/channel listing).** From general Graph v1.0 knowledge and the PRD appendix sources: chat send `POST /chats/{id}/messages` (delegated `ChatMessage.Send`/`Chat.ReadWrite`, application permissions only for migration), channel send `POST /teams/{id}/channels/{id}/messages` (`ChannelMessage.Send`), channel replies `.../messages/{id}/replies`. Listing chat messages supports `$top`, `$orderby` on `lastModifiedDateTime`/`createdDateTime` and a `$filter` on `lastModifiedDateTime` (documented as the only supported filter/order pair; unverified for delegated 1:1 chats). **Decision:** one adapter package isolates these; every shape has `TestAssumed*` tests so M0 deviations are mechanical fixes. (UA-1)
2. **Channel delta with delegated tokens.** Channel message delta exists (`/messages/delta`) and is preview-throttled in some docs (unverified). **Decision:** use delta when it answers 200 and keep the delta link in the cursor store; on 400/501 fall back to list + client-side `lastModified` filter. Replies are not part of the channel delta, so v1 polls replies only for threads the agent posted in (bounded by `thread_poll_max`). (UA-3)
3. **What the core v0.1.0 provides and how outlook consumes it.** Core (`output`, `auth`/`authtest`, `httpx`, `audit`, `selftest`, `docgen`, `policy`) is vendor-neutral; no daemon adapter; `policy` is a generic verb/resource engine. `outlook` uses all but `policy`, models its typed policy in `internal/domain`, loads it strictly with go-yaml and proves authorship by ownership checks, keeps an idempotency ledger in a locked JSON file, wraps free text with `output.Untrusted` only in the CLI presenter, stubs `newDaemonClient()` returning `*auth.UnreachableError`, and records core gaps in `docs/requested-core-changes.md`. **Decision:** `teams` follows the same shape (D10, D12) so the three CLIs stay comparable.
4. **Idempotency without a Graph key.** Graph chat send has no idempotency key. Options: local ledger (reliable while state persists), embedded marker scan (unverified: Teams may strip attributes), accept duplicates. **Decision:** ledger is the control; marker scan optional and off by default; failure modes documented (D7, D2).
5. **1:1 chat resolution.** List `oneOnOne` chats with members expanded and match the other member; creating a oneOnOne chat is believed idempotent. **Decision:** D6; creation is opt-in per destination.
6. **How to discover inbound without enumerating every chat.** `GET /me/chats` ordered by activity would expose messages from unlisted chats and cost calls per poll. **Decision:** poll only policy destinations (ADR-9); unlisted inbound is dropped by construction (D4).
7. **Mentions without a user-lookup scope.** Graph needs the mentioned user's id and display name. **Decision:** both from policy (D5), so no `User.ReadBasic.All`.
8. **Delivery semantics.** Options: at-most-once (advance on read), at-least-once with ack. **Decision:** at-least-once with explicit ack, watermark over contiguous acked prefix, edit re-delivery (D8).
9. **Policy engine.** Core `policy` cannot express typed destinations, senders or mentions, and keeps rate state per process. **Decision:** typed domain policy plus ledger-backed counters (D12).

## Industry standards and references
Microsoft Graph chatMessage resource and message send/list docs (links in PRD appendix); RFC 7231 `Retry-After`; JSON Lines for audit; 12-factor style env overrides; at-least-once consumer pattern with idempotent processing.

## Existing implementations
Sibling `outlook-cli` (feat branch): composition root (`cmd/outlook/app.go`), graph client on `httpx` with writes never retried, file ledger with lock/quarantine, policyfile trust check, selftestcfg, docgen skill, ADR for the daemon stub, `docs/requested-core-changes.md`, `m0-spike-checklist.md`, `unverified-assumptions.md`. Reuse the patterns, not the code (no cross-repo imports).

## Best practices adopted
Fail closed on policy/ledger errors; untrusted marking at the boundary; no secret in logs; deterministic generated docs; fakes only in PR CI; one fake Graph server shared by adapter and integration tests.

## Open questions
Spec s13. Real-tenant answers come from M0 (`docs/m0-spike-checklist.md`): Graph shapes, delta behavior, 403 vs 404 for non-members, `Chat.Create` scope, CA/refresh lifetime, throttling, marker survival, kill-switch window.

## References
- `teams-cli-PRD.md`, `prd-review.md`
- agent-cli-core v0.1.0 README and `docs/technical-details.md`
- outlook-cli feat branch docs and cmd/outlook
- `agentic-teams/skills/teams-cli.md`
