# Implementation Notes: teams-cli
**Date:** 2026-10-03

Purpose: record decisions and deviations as implementation proceeds; update after each task.

## Technical Decisions
## Edge Cases & Solutions
## Deviations from Plan
- Phase I seam fix: WS-E's `cmd/teams/usecase_wire.go` held a `useCaseDeps` struct and a `notWired` placeholder because WS-B was written in parallel. WS-B exports `usecase.Deps` and `usecase.New` with the field names WS-E assumed, so the wiring is now `usecase.New(d)`; the placeholder, its test and the skip guard are removed. No other seam gaps: `assemble()` already builds the real graph client (core `auth.NewAuthorizer` over `NewDaemonTokenSource`), state store (ledger and cursors), policyfile provider, auditlog sink and use cases.
- `teams selftest` with the stub daemon exits 1 (general), not 3: the core selftest runner aggregates per-row probe errors into one general error. The message still names the daemon socket. Other commands exit 3.
- E7 sets the run identity through `appConfig` (fake clock, `authtest.Fake`, `graphtest`), not through a built binary. A built release binary refuses an untrusted (user-owned) policy with exit 9, so a manual run of the binary against graphtest is not possible (graphtest is a test helper, and the daemon is a stub). Manual check done: `teams version` runs with no policy; `whoami` and `destinations list` with the sample policy in a user-owned temp dir exit 9 (untrusted policy), as designed.
- The sample policy exists only as `internal/adapters/policyfile/testdata/teams.policy.sample.yaml`; the `user-docs/` copy and the byte-equality check (I2/ST2) move to W5 (dev-flow step 4).
- I3 benchmark: `teams version` starts in about 5 ms. Real Graph overhead cannot be measured without a tenant.
## Lessons Learned
