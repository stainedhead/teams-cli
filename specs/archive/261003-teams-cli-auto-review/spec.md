# Spec: teams CLI Auto-Review Fixes

**Created:** 2026-10-03 | **Source PRD:** specs/261003-teams-cli-auto-review/teams-cli-auto-review-PRD.md | **Parent spec:** specs/archive/261003-teams-cli/

## Executive Summary
Fix the nine findings (FR-R1..FR-R9) from the step-5 code and design review of branch feat/teams-cli. No P0 blockers; three P1 (policy bypass via env, silent inbound loss, output deviations) and six P2.

## Problem Statement
The teams CLI is well built but: the agent-controlled TEAMS_STATE_DIR/AGENT_ID bypass policy guardrails; an inbox backlog larger than the page cap silently drops older messages; whoami/destinations/dry-run omit spec-required fields; rate/loop checks are not atomic; audit is written after the post; inbound links are unmarked; audit lacks HTTP status; CI has correctness risks; production daemon client is a stub.

## Goals / Non-Goals
Goals: meet every acceptance criterion in the PRD sections 2 and 6 (P1 required, P2 met or deferred with owner). Non-goals: implementing the real daemon client (FR-R9 tracks only), new features, verifying real-tenant Graph behavior.

## Functional Requirements
FR-R1 (P1) state dir and agent id cannot be overridden by agent env when policy sets them. FR-R2 (P1) inbox never loses messages when backlog exceeds page cap; reports truncation. FR-R3 (P1) whoami destinations/limits/poll/policy path+version; mentionable; dry-run decision/destination/untrusted preview. FR-R4 (P2) atomic check-and-claim of rate slots, pending counted, RecordSent failure surfaced. FR-R5 (P2) audit intent before POST; delivered-but-unaudited reported with message id. FR-R6 (P2) links emitted as untrusted or sanitized. FR-R7 (P2) HTTP status in audit events. FR-R8 (P2) CI auth, darwin tests, teamsdev vet and default-binary check. FR-R9 (P2) deferral documented, follow-up item. Full acceptance criteria: see source PRD.

## Non-Functional Requirements
Security (fail closed, no widening of agent control), reliability (crash-safe ledger, -race concurrency tests), performance (one bounded page per poll), observability (no message text or tokens in audit), compatibility (additive output changes, deliberate golden updates). See PRD section 7.

## System Architecture
Layers touched: infra/config (env), cmd/teams (app wiring), usecase (send, inbox, service, whoami, destinations), domain (cursor, ratelimit), adapters (graph messages, cli present, auditlog, state ledger), CI workflow, docs. Clean Architecture rules enforced by internal/archtest must remain green.

## Scope of Changes
Files: internal/infra/config/env.go, cmd/teams/app.go, internal/adapters/graph/messages.go, internal/usecase/{inbox,send,service,whoami,destinations}.go, internal/domain/cursor.go, internal/adapters/cli/present.go, internal/adapters/auditlog/sink.go, state ledger, .github/workflows/ci.yml, user-docs/{configuration,usage}.md, docs/deferred.md. No new dependencies.

## Breaking Changes
Env precedence change (policy wins) for TEAMS_STATE_DIR/AGENT_ID. Output fields additive. Audit schema gains optional fields.

## Success / Acceptance Criteria
All PRD criteria met; go build, vet (with and without -tags teamsdev), golangci-lint, gofmt, go test -race -count=1 ./... clean; logic-package coverage not reduced.

## Risks
Ledger locking changes could deadlock (mitigate with concurrency test under -race); inbox ordering depends on unverified Graph assumptions (UA); CI auth needs admin decision.

## Timeline
M1 P1 fixes; M2 P2 code fixes; M3 CI, docs and deferrals. 

## References
specs/261003-teams-cli-auto-review/teams-cli-auto-review-PRD.md; specs/archive/261003-teams-cli/spec.md
