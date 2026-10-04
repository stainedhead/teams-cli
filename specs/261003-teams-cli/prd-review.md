# PRD Review: teams-cli-PRD.md (Draft v0.2)

Reviewed 2026-10-03 against `INTENT.md` and the `agent-cli-core` README (read-only). Skill: `dev-flow:review-prd`.

**Verdict: Needs revision (minor gaps).** Strong, specific PRD; the gaps are traceability and a few undefined terms, not missing substance. Speccing can proceed if the items marked "before spec" are resolved or carried into the spec as explicit open items.

| Dimension | Status | Note |
|---|---|---|
| Problem clarity | Pass | Agents need Teams presence; app-only Graph cannot send; no relay allowed. Matches INTENT.md. |
| Scope definition | Pass | G1-G5 and explicit non-goals. |
| Functional completeness | Warn | No numbered FRs; the command table (section 6) and policy (section 7) are the de facto requirements and are not linked to G1-G5. Some bundled behavior (polling, discovery, cursors in one paragraph). |
| NFR coverage | Pass | Performance (<2 calls/destination/poll, <50 ms), security (section 9), reliability (back-off, bounded re-read), observability (audit JSONL). Reliability and availability targets are thin. |
| Acceptance criteria | Warn | Per-milestone acceptance exists (section 13) but is coarse ("Agent posts to an approved channel"); no measurable thresholds (poll latency, kill-switch window, max duplicate rate). |
| Dependency identification | Pass | agent-okta-d `msgraph`, agent-cli-core tags, Entra/CA, licensing, admin consent, paperclip images all named. Strong evidence legend. |
| Open questions | Pass | Section 15 (7 items) and 16.8/17.1. |

## Defects and ambiguities

Before spec:
1. No requirement IDs for the product behavior (only BLD/REL/DEP/SKILL have them). Add FR-n IDs mapped to goals so tasks and tests can trace.
2. "P1" / "P2" used (idempotency marker scan, file upload, webhook) but priority levels are never defined, and milestones M0-M5 are a different axis. Define or replace with milestone references.
3. Acceptance criteria not measurable: "measured window" (G4) and "measured poll latency" have no target values or pass thresholds.
4. Conflicting wording on the destination allowlist for inbound: `inbound.handle: [direct, mentions, watched]` implies unlisted 1:1 chats and @mentions in unlisted channels can appear, yet section 9 says the CLI "refuses any destination not in policy". State whether inbound from non-policy chats is dropped, surfaced as `unlisted`, or allowed (with `can_instruct=false`).
5. Mention resolution: `--mention <alias>` requires mapping alias to AAD id and building Graph mention objects; the scope list has no user-lookup scope (e.g. User.ReadBasic.All) and policy only has `aad_id` for `user:` aliases. Specify.
6. `user:<alias>` 1:1 chat "resolved or created on demand" needs chat-creation permission and behavior when the chat already exists; not in the scope table or selftest.
7. Idempotency ledger relies on local state, but the container image state dir (REL-4a) may be ephemeral; state loss implies duplicate sends and re-read. State the consequence and recommended volume.
8. Cursor semantics: `ack` advances a local cursor, yet `inbox` returns "new messages"; define whether un-acked messages are re-delivered and how `--since` interacts with ack (at-least-once vs at-most-once).
9. Exit code mapping beyond "401 then exit 3" is left to core (codes 0-9); PRD should reference the core's table for 403 (not a member), 404, 429, policy deny, so selftest expectations are testable.

Lower priority:
10. Policy file path `/etc/agent-cli/teams.policy.yaml` and "signed policy" (M4) are not specified (who signs, verification).
11. Section 12 says "macOS and Linux"; section 16 adds WSL2 and the OCI image. Consistent but worth one sentence.
12. Rate default `per_minute: 10` vs Teams throttling not reconciled; marked unconfirmed.
13. Threat model lacks an item for message edit/delete after the agent has read it (edited message after `can_instruct` check) and for chat membership changes mid-session.
14. Appendix cites Graph shapes that are flagged unverified; M0 correctly gates this.

## Consistency with INTENT.md and agent-cli-core
Consistent: own-Entra-user model, no relay, daemon-held refresh token, `msgraph` provider shared with outlook, kill switch (Entra and Okta), core pinned at a released tag, untrusted marking, exit codes 0-9 owned by core, docgen-derived skill. No contradictions found.

## Minimal fixes applied to the PRD
1. Section 6 `teams send`: clarified that `--file` reads message text from a file and is not an attachment upload (avoids conflict with the file-upload non-goal).
2. Section 6 `teams reply`: clarified that chats have no reply threads, so `reply` in a chat posts a normal chat message.

All other items are left for the author/decision makers.

## Strengths
Explicit evidence legend; honest trade-offs; options table with verdicts; policy sketch is concrete; sender authorization by AAD id rather than display name; thorough CI/CD and skill sections; kill-switch and negative selftest (403/404 for non-member chat).

## Next step
Resolve defects 1-9 (or carry them as spec open items), then run `/create-spec` (a spec dir already exists; use `/review-spec`).
