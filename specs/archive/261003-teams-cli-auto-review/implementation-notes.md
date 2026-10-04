# Implementation Notes: teams CLI Auto-Review Fixes
Date: 2026-10-03. Update after each task with decisions, edge cases and deviations.

## Technical Decisions
## Edge Cases & Solutions
## Deviations from Plan
## Lessons Learned

## Step 9 notes
- R1: policy `state_dir` set explicitly pins the state dir (`Policy.StateDirPinned`); `TEAMS_STATE_DIR` applies only when the policy omits it. Audit identity is the policy profile; a differing `AGENT_ID` is audited as `claimed_agent`.
- R2: inbox reads the whole window (up to MaxPages) and keeps the oldest `max_results`, so the watermark never passes an undelivered message; `<alias>:truncated` / `all:truncated` in `skipped`. Residual: a backlog beyond MaxPages x page size inside max_lookback is still capped by the page scan (needs UA-1/UA-7 ascending order to remove).
- R4: new `Ledger.Claim` (check + reserve in one lock); unkeyed sends get a `~slot:` entry (completed or failed like a key). Ledger update failure after a post now returns an error naming the message id.
- R5: a send/reply writes an `intent` audit record before the POST; the final record is still exactly one per command.
- R6: links are `output.Untrusted` objects (shape change from plain strings).
- R7: Graph errors expose `HTTPStatus()`; success is 200 (reads) or 201 (post), 0 when no Graph call was made.
- R8: dependency-auth decision and first CI run left to a repo admin (docs/deferred.md).
## Deviations from Plan
- Tests were written together with each fix in the same change rather than as separate failing commits (FR-R2's test was run against the unfixed code and failed as expected).
