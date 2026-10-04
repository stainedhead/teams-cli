# Plan: teams-cli
**Date:** 2026-10-04 | **Status:** Implementation-ready | Spec: `spec.md`; tasks: `tasks.md`

## Development approach
TDD (failing test, pass, refactor), Clean Architecture (architecture.md), table-driven tests, no network in unit tests. Fakes: `auth/authtest.Fake` for the daemon, `graphtest` (httptest) for Graph, `usecasetest` for ports, a fake clock for time. Phase F defines every interface and type first; Phase P runs six workstreams (WS-A..WS-F) in parallel with disjoint file ownership against those interfaces; Phase I integrates.

## Phases

| Phase | Name | Who | Output | Exit criterion |
|---|---|---|---|---|
| F | Foundation (serial, one engineer) | lead | go.mod requires core v0.1.0 and go-yaml; layer dirs; `domain` types + error taxonomy + function stubs; `usecase/ports.go` + `Commands` + DTOs; `usecasetest` fakes skeleton; archtest; Makefile targets; AGENTS.md rule updated | `go build ./... && go vet ./... && go test ./internal/archtest` green; interfaces frozen (changes after F need a lead-approved note in `implementation-notes.md`) |
| P | Parallel build | WS-A..WS-F (six) | See `tasks.md` | Each workstream's gates green on its own packages |
| I | Integration and hardening | lead | merge; `cmd/teams` integration tests with `authtest.Fake` + `graphtest`; coverage; docs reconciliation; skill generation; root-skill PR text | All AC-n pass; quality gates green; `make cross` builds 3 targets |

Spec milestones inside Phase P: M1 (whoami, destinations, send, inbox, ack, policy load, cursors, audit, version) is the first merge train; M2 (mentions, filters, rate/loop, idempotency, can_instruct, selftest) second; M3 (reply, thread get, skill doc, docs polish) third. Each workstream lists its tasks tagged M1/M2/M3 so partial merges stay releasable.

## Critical path
F (types, ports) -> WS-A domain policy/classify/cursor -> WS-B send/inbox use cases -> WS-E CLI + composition -> Phase I integration. WS-C (Graph), WS-D (state/policy files) are off the use-case path until Phase I because use cases test against `usecasetest` fakes.

## Interfaces between workstreams (frozen in Phase F)
- `domain` public function signatures: `tasks.md` "Domain API".
- `usecase.Commands`, request/response DTOs and ports: `architecture.md` s4, code in `internal/usecase/ports.go`.
- `graphtest` server contract (WS-C publishes in Phase P week 1): routes mirror `data-dictionary.md` Graph table; knobs: `Fail(path, status)`, `Throttle(path, retryAfter)`, `Seed(chat/channel, []msg)`, `Requests()` recorder (headers redacted to presence of `Authorization`).
- State file formats: `data-dictionary.md`.

## Testing strategy
Unit: domain (>= 85%), use cases (>= 85%) with fakes. Adapter tests: graph vs `graphtest`; state with temp dirs and parallel-process lock test; policyfile with trust injection. Contract: `TestAssumed*` per Graph shape. Integration (Phase I): `cli.Run` through `cmd/teams` with `authtest` scenarios and `graphtest`. Security: injection corpus, planted-token scan, loop simulation, audit property test. Benchmarks: local overhead (informational). `teams selftest` and tenant integration run only on demand, never in PR CI (BLD-4).

## Rollout strategy
Library of work merges to `main` by PR (CI required once branch protection is enabled). No release workflow in this build (deferred; REL-* in `docs/deferred.md`); unsigned local builds are pre-release. The real daemon adapter swap and the M0 spike report are the two gating follow-ups before production use.

## Risks to schedule
Graph shape drift (isolated in WS-C), ledger/cursor semantics (WS-A/WS-D contract tests agreed in F), core gaps (log in `docs/requested-core-changes.md`, never edit the core).

## Success metrics
All AC-1..AC-26 (spec s7) green in CI; gates: `gofmt -l .` empty, `go vet`, `golangci-lint run`, `go test -race ./...`, `go mod tidy` no diff, `govulncheck`; coverage targets in spec s7; zero `replace` directives; `agent-okta-d` absent from go.mod.
