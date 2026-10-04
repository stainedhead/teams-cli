# Deferred work

Items that are intentionally not built in this repository now, with the reason and the condition for picking them up again.

| Item | Status | Rationale | Pick up when |
|---|---|---|---|
| Release workflows (signing, notarization, SBOM, attestations) | Deferred | PRD s16.2-16.4 (REL-*) need a signing account and registry that are open questions (spec OQ-2). Only CI exists; no secrets are added and there is no release workflow. `make cross` proves the three targets compile. | The open items in PRD s16.8 are decided. |
| CI dependency authentication | Needs repo-admin decision (FR-R8) | The workflow reads the private `agent-cli-core` module with the `MODULE_READ_TOKEN` secret, falling back to `GITHUB_TOKEN`, which can read another private repository only if that repository grants Actions access. Which one to use (fine-grained read-only token or deploy key) is an open admin decision (review OQ-3). Owner: repo admin. | Before the first PR run: add `MODULE_READ_TOKEN` (Contents:read on `agent-cli-core`) or enable Actions access on `agent-cli-core`, then confirm the "Download modules" step passes. |
| Real daemon adapter (FR-R9, accepted deferral) | Deferred | **The CLI is not usable against a tenant until the real daemon client lands:** every network command exits 3 in production. FR-26 and FR-34 are verified only against `authtest` fakes. The swap point is `newDaemonClient()` in `cmd/teams/daemon.go`. `agent-okta-d` has not published `pkg/client`; `newDaemonClient()` returns the core's unreachable error (exit 3). `agent-okta-d` is never in `go.mod`. See `adr-daemon-client-stub.md`. | `agent-okta-d` tags a release with `pkg/client`; the swap is one function. Follow-up item (owner: tool maintainer): implement the real adapter behind `newDaemonClient()`, run the M0 spikes (`m0-spike-checklist.md`), and replace `TestStubDaemonMakesEveryGraphCallExit3` with an integration test against the real daemon or its published fake. |
| Daemon socket ownership check | Deferred | The credential socket path is not checked while the client is a stub. | The real client lands (see section below). |
| Signed policy files (M4) | Deferred | Policy trust is by file ownership (FR-21). Signature verification is not built. | The hardening milestone (M4). |
| M0 and M5 real-tenant spikes | Deferred | They need a sandbox tenant, a Teams-licensed test agent user and a human to enroll. Checklist: `m0-spike-checklist.md`. Until they run, every Graph shape stays in `unverified-assumptions.md`. | A sandbox tenant is provisioned. |
| Kill-switch drill (S-8) | Deferred | Needs the sandbox tenant and the real daemon. | M4, with the sandbox tenant. |
| Entra Agent User runtime | Spike plan only | Plan is in `m0-spike-checklist.md` (S-10), from PRD s11. No code; only the daemon would change. | Provisioning is GA, license cost is acceptable and security signs off. |
| File attachments | Deferred (PRD "P2") | Not built (spec D2). | An operator need and a policy model for attachments. |
| Webhooks and change notifications | Deferred (PRD "P2") | Incoming webhook or Workflows notification path and real-time delivery are not built; inbound is polling. | A decision to host a receiving service. |
| Group-based commanders | Deferred (PRD "P2") | Commanders are a static list of AAD ids (UA-17). | Operators need group membership. |
| `inbound.unlisted: surface` mode | Not built | Inbound from unlisted conversations is dropped (ADR-4). | A decision to read conversations outside policy. |
| `send.file_roots` for `--file` | Deferred (spec OQ-3) | `--file` reads any file the agent user can read. A prompt-injected agent could send a readable file's text to an allowed destination. Filters and the destination allow-list are the only brake. | A decision before M4. |
| Marker scan verification | Built, unverified | Optional behind `send.marker_scan` (default false); depends on UA-9. | S-5 confirms marker survival. |
| Channel reply polling beyond agent-started threads | Not built | Replies are polled only for threads the agent posted in, bounded by `inbound.thread_poll_max` (UA-3). | S-6 shows broader polling is affordable. |
| Root skill update and `docs` automation | Manual | Updating the root repository's skill is a manual PR (PRD s17.1). | A cross-repository token approach is agreed. |
| Windows native | Out of scope | Supported platforms are macOS and Linux (WSL for Windows). No `windows/*` target; file and socket assumptions are POSIX. | A decision to support native Windows. |

## AGENT_OKTA_D_SOCKET ownership check

The daemon client is a stub, so the credential socket path is not yet checked. When the real `agent-okta-d` client lands, apply the same trust rule as the policy file to the socket and its parent directories (owned by root or a configured trusted uid, not group or world writable, never the agent's own uid) so that `AGENT_OKTA_D_SOCKET` cannot redirect token requests to an agent-controlled socket.
