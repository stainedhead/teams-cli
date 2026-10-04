# Plan: teams-cli
**Date:** 2026-10-03 | **Status:** Planning

## Development Approach
TDD, Clean Architecture, fakes for Graph/daemon/clock.
## Phase Breakdown
P1 foundation, P2 core commands, P3 authz/controls, P4 threads, docs, CI/CD.
## Critical Path
agent-cli-core integration -> policy -> Graph adapter -> send/inbox.
## Testing Strategy
Table-driven unit tests; no network; selftest on-demand only.
## Rollout Strategy
Release pipeline per PRD 16; unsigned builds labelled pre-release.
## Success Metrics
Acceptance criteria in spec.md.
