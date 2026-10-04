# Tasks: teams CLI Auto-Review Fixes
Status: Planning

## Progress Summary
0/12 tasks complete

Rule for every task: write the failing test first (T), then implement (I), then reviewer-teammate pass (R), then run gates (go vet, golangci-lint, go test -race ./...). Update status.md after each task.

### Phase 1: P1 fixes
- [ ] **R1.1** Test: send with TEAMS_STATE_DIR set to empty dir still counts against policy-pinned ledger; AGENT_ID cannot override policy profile. Deps: none. Est: 1h
- [ ] **R1.2** Implement policy-wins precedence in internal/infra/config/env.go and cmd/teams/app.go; audit carries policy profile; update user-docs/configuration.md. Deps: R1.1. Est: 1.5h
- [ ] **R2.1** Test: use-case backlog of N+10 messages delivered in order across inbox/ack cycles with none lost; truncated flag reported. Deps: none. Est: 1.5h
- [ ] **R2.2** Implement ascending fetch or truncation detection with no watermark advance past oldest undelivered (graph/messages.go, usecase/inbox.go, domain/cursor.go). Deps: R2.1. Est: 2h
- [ ] **R3.1** Tests (golden): whoami destinations/limits/poll/policy path/version; destinations mentionable; dry-run decision/destination/untrusted preview. Deps: none. Est: 1.5h
- [ ] **R3.2** Implement in usecase/whoami.go, destinations.go, send.go, cli/present.go; update goldens deliberately. Deps: R3.1. Est: 2h

### Phase 2: P2 code fixes
- [ ] **R4** Test: N concurrent sends vs limit K, exactly K succeed; pending counted; RecordSent failure surfaced. Then implement atomic reserve in ledger and send.go. Deps: R1.2 merged (shares send/service). Est: 4h
- [ ] **R5** Test both cases (intent failure blocks post; post ok but final audit fails returns message id with delivered-not-audited error). Then implement in usecase/service.go. Deps: R1.2. Est: 3h
- [ ] **R6** Test: link with `<<<` and instruction text cannot break delimiters. Then emit links as output.Untrusted or sanitize; document in user-docs/usage.md. Deps: R3.2 (present.go). Est: 2h
- [ ] **R7** Test HTTPStatus for 403, 429, success. Then propagate status from typed error to audit event. Deps: R5. Est: 2h

### Phase 3: CI and docs
- [ ] **R8** CI: dependency-auth secret documented, darwin test job or docs/deferred.md entry, `go vet -tags teamsdev ./...` and default-binary-lacks-dev-override check. Test by CI run. Deps: none. Est: 2h
- [ ] **R9** Document stub and newDaemonClient swap point in docs/deferred.md and PR description; add follow-up item for real adapter and M0 spikes. Deps: none. Est: 0.5h

### Phase 4: Final quality pass (separate dev-flow step)

## Parallel workstreams (worktrees, since fixes touch disjoint files)
A: R1, R5, R7 (config, service.go, auditlog). B: R2 (inbox). C: R3, R6 (usecase presenters, present.go). D: R4 (starts after A merges). E: R8, R9 (CI, docs). Merge one at a time, full gates after each.
