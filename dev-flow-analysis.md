# Dev-Flow Process Analysis

**Feature:** teams-cli-auto-review (review fixes for teams-cli)
**Spec directory:** specs/archive/261003-teams-cli-auto-review
**Report generated:** 2026-10-03 (local; 2026-10-04 UTC)

---

## 1. Executive Summary

`teams-cli` is a Go CLI (`teams`) that lets AI agents post to and read Microsoft Teams as a named Entra user through Microsoft Graph, with a client-side policy engine, idempotency ledger and at-least-once inbox. The first pass built the product from the PRD; the auto-review spec then fixed nine findings (FR-R1..R9: policy/profile precedence, inbox ordering, audit intent-before-post, atomic rate claims, untrusted links, CI hardening, deferral tracking).

**Total runtime:** PRD authoring started 2026-10-03T14:49:50-04:00 (initial commit); the orchestrated implementation run (spec creation to final commit) ran 20:24:25 to 21:39:57 -04:00, about 76 minutes. Total with PRD authoring: about 6h50m of wall clock, most of it idle gaps between PRD commits.
**Overall assessment:** The run was orchestrated by AI sub-agents (parallel workstream agents on separate branches, merged into feat/teams-cli), not by a human developer working step by step. It was fast and ended with lint, vet, race tests and cross builds clean, but all Graph behavior is verified only against a fake server.

---

## 2. Step-by-Step Timing

Times are UTC from git (-04:00 local + 4h). Rows in DEV-FLOW-STATUS.md for steps 3, 4, 5 and 9 originally held estimated times that did not match git; they were corrected from commit timestamps.

| Step | Name | Start | End | Runtime (min) | Key Outputs |
|---|---|---|---|---|---|
| 0-1 | Pre-flight, create spec | 00:23Z | 00:24Z | 1 | spec dir, dashboard |
| 2 | Review spec | 00:25Z | 00:35Z | 10 | implementation-ready FRs, workstreams |
| 3 | Implement product | 00:38Z | 01:13Z | 35 | domain, usecases, Graph adapter, state, audit, CLI, integration tests |
| 4 | Docs and user docs | 01:13Z | 01:19Z | 6 | user-docs, product/technical docs |
| 5 | Code and design review | 01:19Z | 01:22Z | 3 | auto-review PRD |
| 6-8 | Review PRD, archive original, review spec | 01:23Z | 01:24Z | 2 | reviewed PRD, new spec |
| 9 | Implement review fixes | 01:24Z | 01:36Z | 13 | one commit per FR-R1..R9 |
| 10 | Archive fixes spec | 01:39Z | 01:39Z | 1 | spec moved to archive |
| 11 | Final quality pass | 01:39Z | 01:40Z | 2 | status/doc link corrections |
| 12-13 | Analysis, archive check | 01:40Z | 01:41Z | 1 | this report |

**Notable observations:**
- Implementation of the whole product took about 35 minutes of wall clock because workstreams C, D, F ran in parallel and were merged at 20:53.
- The orchestrator wrote estimated timestamps into several status rows before the work finished; git disproved them.
- Several FR-R fixes landed within 1-5 minutes each, so test depth per fix is worth sampling in human review.

---

## 3. Commit and Push Summary

**Total commits:** 47 on feat/teams-cli (before this report's commit). Full list: `git log --format="%h %aI %s"`. Milestones:

| Commit | Timestamp | Message |
|---|---|---|
| 0d6e0c7 | 2026-10-03T14:49:50-04:00 | Initial commit: scaffold teams-cli repository |
| 7258144 | 2026-10-03T20:24:25-04:00 | Create teams-cli spec from PRD and dev-flow dashboard |
| 66327c4 | 2026-10-03T20:38:08-04:00 | Add Phase F foundation |
| 28be7ed | 2026-10-03T21:13:32-04:00 | Wire real use cases, end-to-end integration tests |
| 0f2b13f | 2026-10-03T21:19:08-04:00 | Add user docs, update product and technical docs |
| 8f1ab9e | 2026-10-03T21:23:46-04:00 | chore: archive original teams-cli spec |
| b63e00d | 2026-10-03T21:26:12-04:00 | fix(teams): policy state_dir and profile win over agent env (FR-R1) |
| d251bb1 | 2026-10-03T21:35:06-04:00 | fix(teams): atomic rate/loop check-and-claim (FR-R4) |
| ecd6cfd | 2026-10-03T21:38:44-04:00 | ci: use job GITHUB_TOKEN only, no extra secrets |
| d288569 | 2026-10-03T21:39:02-04:00 | chore: archive teams-cli-auto-review spec |

No pull request has been opened yet (step 14 pending). Diff vs the initial commit: 170 files, about 21.6k insertions.

---

## 4. Spec vs. Implementation Comparison

| Phase | Planned (spec) | Actual (git log) | Difference | Notes |
|---|---|---|---|---|
| Spec/research | not time-estimated | ~11 min | n/a | Spec review in the same run |
| Product implementation | multi-workstream | ~35 min | n/a | Parallel branches |
| Review fixes (FR-R1..R9) | 4 phases | ~13 min | n/a | One commit per fix |
| CI/docs/deferrals | Phase 3 | within the 13 min | n/a | CI later reduced to GITHUB_TOKEN only |

**Phases skipped:** none identified.
**Phases added:** a final quality pass correcting status timestamps and stale doc links (README/docs pointed at a PRD path that moved on archive; AGENTS.md status was stale).

---

## 5. Token / Message Usage

Exact token counts unavailable. The run used an orchestrator plus several sub-agents (one per workstream and per step); no per-agent counts were recorded, and none are claimed here.

---

## 6. Process Observations

### What worked well
- Parallel workstreams with a fake Graph server (graphtest) allowed broad testing without a tenant.
- An architecture test enforces layering; per-fix commits make review easy.
- Final gates passed: gofmt clean, go vet clean, golangci-lint 0 issues, `go test -race -count=3 ./...` passing, cross builds succeeded.

### What caused delays or rework
- The first CI draft needed a second change to drop an extra secret and use only GITHUB_TOKEN.
- Status timestamps were approximated and had to be corrected.
- Archiving moved the PRD, breaking links in README and docs.

### Recommendations for future runs
- Have the orchestrator record `date -u` at step boundaries, not estimates.
- Run a link check after archive steps.
- Verify against a real tenant before relying on any Graph behavior; the agent-okta-d adapter is still unreleased, so end-to-end use is blocked (exit code 3).

---

## 7. Manual vs. Automated Comparison

**Estimated manual duration:** roughly 2-3 weeks for a senior developer (about 20k lines including tests and docs, two review rounds), excluding meetings and tenant access.
**Actual automated runtime:** about 76 minutes from spec creation to final commit.
**Efficiency gain:** large in raw elapsed time, but the estimate is rough, the output has had no human code review, and Graph behavior is unverified.
