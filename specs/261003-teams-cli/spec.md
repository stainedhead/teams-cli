# Spec: teams CLI

**Created:** 2026-10-03 | **Status:** Draft | **Source PRD:** `specs/261003-teams-cli/teams-cli-PRD.md` (Draft v0.2)

## Executive Summary
`teams` is a Go CLI that lets AI agents post to and read Microsoft Teams as their own named Entra user. It calls Microsoft Graph with delegated permissions, obtains ~1h Graph tokens from the `agent-okta-d` daemon (provider `msgraph`), enforces client-side policy, and polls for inbound messages (no hosted component, no public endpoint). Shared behavior comes from `agent-cli-core` v0.1.0.

## Problem Statement
Agents need to post updates, answer @mentions/DMs and reply in threads in Teams. Application-only Graph permissions cannot send chat messages, and a hosted bot relay is rejected. Agents run on NAT'd hosts and must not hold any Microsoft credential.

## Goals
- G1 Post to approved destinations and read messages addressed to the agent as its own Teams user, with no Microsoft credential readable by the agent process.
- G2 Distinguishable, attributable presence per agent (own account).
- G3 Only authorized people can instruct an agent; all other text is untrusted data.
- G4 Disabling the agent (Entra user or Okta app) stops access within the measured window.
- G5 Works behind NAT (outbound-only), nothing hosted.

## Non-Goals
Human mode; calls/meetings/voice; file upload/download (P2), tabs, message extensions, bots; real-time delivery; external-tenant/guest messaging; hosting any service.

## Functional Requirements
- FR-001 `teams whoami`: report agent user (GET /me), policy profile, allowed destinations, limits; verify UPN matches policy at start-up.
- FR-002 `teams destinations list`: list policy aliases (`channel:`, `chat:`, `user:`) the agent may post to or read.
- FR-003 `teams send --to <alias> (--text|--file) [--thread] [--mention] [--idempotency-key] [--dry-run]`: post via alias only (never raw IDs); chat or channel Graph endpoint; policy-gated (send).
- FR-004 `teams reply --thread ID --text T`: reply in channel thread or chat; policy-gated (send).
- FR-005 `teams inbox [--wait N] [--limit] [--since CURSOR]`: return new messages (1:1, @mentions, watched destinations); polling loop at `poll_interval` (default 15s, min 5s) with jittered back-off on 429 honoring Retry-After; never returns the agent's own messages.
- FR-006 `teams ack <id...>`: advance local cursor.
- FR-007 `teams thread get <id> [--limit 20]`: recent thread context; read-gated.
- FR-008 `teams selftest`: allow/deny matrix (allowed send OK; unlisted refused; broadcast mention refused; oversize and secret messages refused; UPN matches; negative test reading a non-member chat returns 403/404).
- FR-009 `teams version`: semver, commit, build date via ldflags (REL-4).
- FR-010 Inbound item shape: id, thread_id, received, conversation{type,alias}, sender{name,aad_id,can_instruct,is_agent}, mentioned_you, text{untrusted:true,value}.
- FR-011 Sender classification by AAD object id (`from.user.id`) against `instruct.commanders` / `instruct.agents`; display names never used for authorization; agents never `can_instruct` unless listed.
- FR-012 Policy (YAML, root-owned): profile, upn, destinations, instruct, inbound (handle, max_lookback), send (max_bytes, mentions allow/block_broadcast, content_filters, link_allowlist, rate, reply_depth_max, prefix), limits, audit.
- FR-013 Per-destination cursors (lastModifiedDateTime or delta token) in a local state file; loss causes bounded re-read (`max_lookback`) de-duplicated by message id.
- FR-014 Idempotency ledger keyed by `--idempotency-key`; P1 marker scan of recent messages before re-send.
- FR-015 Loop/spam controls: rate limits, `reply_depth_max`, ignore own messages.
- FR-016 Token path: receive Graph token from daemon `msgraph` provider; on 401 force one daemon refresh then exit 3; `reauth_required` surfaces human-actionable message.
- FR-017 Audit JSONL per command (no bodies by default).
- FR-018 Generated harness skill document via agent-cli-core `docgen` (SKILL-1..7).
- FR-019 CI/CD and release pipeline per PRD section 16 (BLD-1..6, REL-1..14, DEP-1..6) in `.github/workflows/`.

## Non-Functional Requirements
- Performance: <50 ms local overhead per command excluding Graph latency; <2 Graph calls per destination per poll.
- Reliability: bounded re-read on cursor loss; throttling back-off; safe retries.
- Security: no credential files on agent host; tokens never logged/written; untrusted marking; destination allowlist; secret-pattern filter; broadcast mentions blocked; no external/guest chat.
- Observability: audit JSONL; correlate with Entra non-interactive sign-in logs.
- Portability: Go static binary, macOS and Linux (arm64/amd64); no listeners; outbound HTTPS to Graph only.

## System Architecture
Clean Architecture: domain (policy, classification, cursor, ledger entities), usecase (send, reply, inbox, ack, thread, selftest, whoami), adapters (Graph client, daemon token source via agent-cli-core, state file, clock, audit), infrastructure (cobra commands in `cmd/teams`). Components: policy engine, poller, Graph adapter, auth adapter. Shared concerns (envelope, exit codes, bounds, policy engine, audit, daemon client, docgen) come from agent-cli-core, not reimplemented.

## Scope of Changes
Create: `cmd/teams/`, `internal/{domain,usecase,adapters,infrastructure}`, `.github/workflows/`, sample policy, `docs/`, `user-docs/`. Dependencies: `github.com/stainedhead/agent-cli-core` v0.1.0 (released tag; see AGENTS.md for require timing), Microsoft Graph v1.0 (HTTP, no SDK mandated). Skill doc lives in root repo `skills/teams-cli.md` only.

## Breaking Changes
None (greenfield).

## Success and Acceptance Criteria
- M1: agent posts to an approved channel and receives an @mention.
- M2: section 10 security tests pass (prompt-injection corpus only `untrusted`; non-commander never `can_instruct`; loop simulation; token never logged).
- M3: context-aware replies in threads.
- M4: signed policy, release signing, skill doc, kill-switch drill, security sign-off.
- Quality gates: `gofmt -l .` empty, `go vet`, `golangci-lint run`, `go test -race ./...`, `govulncheck`; PR CI uses fakes only, never real Graph.

## Risks and Mitigation
See PRD sections 9 and 14: polling latency; long-lived refresh token; human enrollment; membership as permission boundary; chat as instruction channel; admin consent for channel read; licensing; Agent User preview risk; unverified Graph endpoint shapes (verify before build).

## Timeline and Milestones
M0 Spikes (sandbox tenant; requires real tenant, out of scope for automated build), M1 Core, M2 AuthZ/controls, M3 Threads/context, M4 Hardening, M5 Agent User decision, P2 webhook/files/group commanders. Implementation here targets M1-M3 against fakes; M0/M4/M5 [TBD].

## Open Questions
PRD section 15 (day-one capabilities, licensing, enrollment/CA, commanders, agent-to-agent, retention/DLP, Agent 365 licensing); CI/CD open items 16.8 and 17.1.

## References
- Source PRD: `specs/261003-teams-cli/teams-cli-PRD.md`
- agent-cli-core v0.1.0; `outlook-cli-PRD.md`; `agent-okta-d-PRD.md` sections 7.5-7.7
