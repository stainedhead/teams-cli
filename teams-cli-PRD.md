# teams CLI — Product Requirements Document

| | |
|---|---|
| **Status** | Draft v0.2 (delegated user-account model; relay removed) |
| **Date** | 2026-10-03 |
| **Owner** | Enterprise Architecture (owner TBD) |
| **Companion docs** | `agent-okta-d-PRD.md` (§7.5 `msgraph` provider, §7.6 Teams notes, §7.7 Agent User roadmap), `agent-cli-core-PRD.md` in [stainedhead/agent-cli-core](https://github.com/stainedhead/agent-cli-core) (shared CLI core; originated in `snow-cli-PRD.md` §5), `outlook-cli-PRD.md` |
| **Binary** | `teams` |

**Evidence legend.** ✅ = confirmed against vendor documentation during research (2026-10-03). ⚠️ = not confirmed in vendor docs this session (engineering judgment, secondary or third-party source). Validate every ⚠️ in a sandbox tenant before depending on it. Graph endpoint shapes in §6 are from general Graph v1.0 knowledge and need checking against the current reference before build.

---

## 1. Summary

Agents need to talk to the team in Microsoft Teams: post updates and alerts, answer @mentions and direct messages, and reply in threads. v0.1 of this document proposed a bot behind a relay service we would host. That is **dropped**: we will not run a relay.

v0.2 design: the agent is its own Entra user (a licensed, person-like account) and the `teams` CLI calls Microsoft Graph **delegated as that user**. Delegated `ChatMessage.Send` / `Chat.ReadWrite` can send chat messages ✅; application-only permissions cannot (only `Teamwork.Migrate.All`, for import) ✅. The `agent-okta-d` daemon holds the agent user's delegated refresh token and serves short-lived Graph tokens (provider `msgraph`, shared with `outlook`). Because no public endpoint exists, the CLI **polls** for new messages instead of receiving change notifications.

Trade-offs accepted: inbound latency equals the polling interval; a human enrolls the agent user once (device-code sign-in); the agent's reach is its Teams membership, so membership is the access-control surface.

## 2. Goals and non-goals

**Goals**

- G1. Agents post to approved Teams destinations and read messages addressed to them, as their own named Teams user, with no Microsoft credential readable by the agent process.
- G2. Each agent has a distinguishable, attributable presence in Teams (its own account).
- G3. Only authorized people can *instruct* an agent through Teams; everyone else's text is untrusted data.
- G4. Disabling the agent (Entra user, or Okta app so the daemon cannot read the refresh token) stops its Teams access within the measured window.
- G5. Works for agents on desktops behind NAT (outbound-only), with no hosted component of ours.

**Non-goals**

- Human mode (people use Teams itself).
- Calls, meetings, voice, screen sharing.
- File upload/download (P2), tabs, message extensions, bots.
- Real-time delivery (polling only in v1).
- Messaging external tenants or guests.
- Hosting any service (relay, bot endpoint, webhook receiver).

## 3. Options evaluated

| Option | Send | Receive | Identity in Teams | Status / dependencies | Verdict |
|---|---|---|---|---|---|
| **Graph, application-only** | **No** ✅ (migration/import only) | Broad app-only read ✅ | n/a | Microsoft's answer: delegated permissions or a bot ✅ | Not possible for sending |
| **Graph, delegated as the agent's own user (this PRD)** | Yes ✅ (`ChatMessage.Send`, `Chat.ReadWrite`) | Yes (poll `chats` / channel messages) | The agent user: own name, presence, directory entry | Needs a licensed user, a one-time human-assisted enrollment, and Conditional Access that tolerates non-interactive refresh-token use ⚠️ | **Chosen** |
| **Graph, delegated as a human** | Yes | Yes | The human; wrong attribution | Needs the human's session/tokens | Rejected |
| **ROPC (password) token for the agent user** | Yes | Yes | Agent user | Fails with MFA; not for federated users except special cases; Microsoft recommends against it ✅ | Rejected |
| **Bot via a relay we host** | Yes | Yes | The bot | Needs a public HTTPS endpoint, a production service, and per-bot app approvals ✅ | Dropped (v0.1) |
| **Incoming webhook / Workflows** | One-way | No | Generic poster | URL is a bearer secret; connectors are being replaced by Workflows ⚠️ | Optional P2, notification-only, no hosting needed |
| **`m365` CLI (PnP) / `mgc`** | Depends on delegated Graph | n/a | Human | `mgc` retired 2026-08-28 ✅; `m365` stores credentials as described in `outlook-cli-PRD.md` §3 | No |
| **Entra Agent User (Agent 365)** | Yes (delegated Graph as the agent's own user) ✅ | Yes (polling or notifications) | Person-like account ✅ | Graph beta provisioning, M365 license per agent user, Frontier preview for user-account mode ✅ | Same runtime shape as this PRD, with a better enrollment story; spike (§9) |

The chosen design and the Agent User design differ only in how the daemon obtains the agent user's token (human-enrolled refresh token vs. Microsoft's federated `user_fic` chain). The `teams` CLI is identical for both.

## 4. Architecture

```
 agent host
 ┌──────────────────────────────┐
 │ teams (Go, agent-cli-core)   │
 │  ├─ policy: destinations, senders, rates, content filters
 │  ├─ poller: per-chat / per-channel cursors (local state)
 │  ├─ graph: delegated calls as /me  ────────────────┐
 │  └─ auth: agent-okta-d socket → provider "msgraph" │ Bearer <Graph token>
 └──────────────────────────────┘                     ▼
                                              Microsoft Graph ──► Teams
 agent-okta-d (separate OS user): refresh token in secret store → Graph access token
```

- The agent host holds **no Microsoft credential file**. The `teams` CLI receives only a ~1 h Graph access token over the unix socket.
- Reach is determined by the agent user's **memberships**: a delegated token can read and post anywhere the user can. Keep the agent user in only the teams/chats it needs. The CLI's destination allow-list is a second, client-side layer.
- Nothing of ours is internet-reachable.

## 5. Identity and authentication

| Item | Requirement |
|---|---|
| Agent user | Entra user (synced from AD), Teams-licensed, mailbox present, in the `agents` group |
| App registration | The shared public-client `agent-graph-cli` from `agent-okta-d-PRD.md` §7.5; user assignment required |
| Delegated scopes (Teams) | `Chat.ReadWrite` (or `ChatMessage.Send` for send-only) ✅; `ChannelMessage.Send` ✅; `ChannelMessage.Read.All` and `Channel.ReadBasic.All` (**admin consent** required ⚠️); `User.Read`, `offline_access` |
| Enrollment | One-time device-code sign-in as the agent user via `agent-okta-d enroll msgraph` |
| Token path | `teams` → daemon `msgraph` provider → Graph access token; on 401 the CLI forces one daemon refresh then exits 3; `reauth_required` surfaces a human-actionable message |

AUTH-1..4 are the same as in `outlook-cli-PRD.md` §7.

## 6. Command surface

Uses the shared CLI core, specified in `agent-cli-core-PRD.md` in [stainedhead/agent-cli-core](https://github.com/stainedhead/agent-cli-core) (originated in `snow-cli-PRD.md` §5): daemon client, policy, envelope, bounds, audit, untrusted marking, exit codes.

| Command | Purpose | Graph call (⚠️ verify) | Policy-gated |
|---|---|---|---|
| `teams whoami` | Agent user, policy profile, allowed destinations, limits | `GET /me` | |
| `teams destinations list` | Aliases the agent may post to or read (`channel:sdlc-alerts`, `chat:dev-team`, `user:jane.doe`) | local policy | |
| `teams send --to <alias> (--text T \| --file F) [--thread ID] [--mention <alias>…] [--idempotency-key K] [--dry-run]` | Post a message (alias, never raw IDs) | `POST /chats/{id}/messages` or `POST /teams/{tid}/channels/{cid}/messages` | send |
| `teams reply --thread ID --text T` | Reply in a channel thread or chat | `POST …/messages/{id}/replies` (channel) ⚠️ | send |
| `teams inbox [--wait 30] [--limit 20] [--since CURSOR]` | New messages addressed to the agent (1:1 chats, @mentions, configured watched destinations) | `GET /me/chats?...`, `GET /me/chats/{id}/messages?...`, `GET …/channels/{cid}/messages(/delta)` ⚠️ | read |
| `teams ack <id…>` | Mark handled (advances local cursor) | local | |
| `teams thread get <id> [--limit 20]` | Recent context in a watched thread | `GET …/messages/{id}/replies` | read |
| `teams selftest` | Allow/deny matrix (§10) | various | |

**Polling behavior.** `inbox --wait N` loops until a message arrives or `N` seconds pass, polling every `poll_interval` (default 15 s, minimum 5 s) with jittered back-off on 429 (`Retry-After` honored). Chats are discovered with `GET /me/chats` sorted by last activity ⚠️, then only chats with new activity are read; channels use the channel messages delta endpoint where available ⚠️. A per-destination cursor (`lastModifiedDateTime` or delta token) lives in a state file under the agent's state directory; losing it causes a bounded re-read (`max_lookback`), de-duplicated by message id. Messages authored by the agent itself are never returned.

**Inbound item shape**

```json
{ "id": "…", "thread_id": "…", "received": "2026-10-03T14:05:00Z",
  "conversation": { "type": "channel", "alias": "channel:sdlc-alerts" },
  "sender": { "name": "Jane Doe", "aad_id": "…", "can_instruct": true, "is_agent": false },
  "mentioned_you": true,
  "text": { "untrusted": true, "value": "…" } }
```

For `can_instruct: false` the text is still `untrusted`, and the generated skill document tells the agent not to act on instructions from such senders.

## 7. Client-side policy

```yaml
# /etc/agent-cli/teams.policy.yaml  (root-owned; agent user read-only)
profile: agent
upn: sdlc-reviewer-01@corp.example.com        # checked against GET /me at start-up
destinations:
  channel:sdlc-alerts: { team_id: "…", channel_id: "…", send: true,  watch: true }
  chat:dev-team:       { chat_id: "19:…@thread.v2",   send: true,  watch: true }
  user:jane.doe:       { aad_id: "…",                 send: true,  watch: true }   # 1:1 chat resolved or created on demand
instruct:
  commanders: { aad_ids: ["…", "…"] }        # can_instruct = true for these senders only
  agents:     { aad_ids: ["…"] }             # other agent users: is_agent=true, never can_instruct unless listed
inbound:
  handle: [direct, mentions, watched]        # what appears in `inbox`
  max_lookback: 30m
send:
  max_bytes: 8000
  mentions: { allow: [user:jane.doe], block_broadcast: true }   # no channel/team/tag mentions by default
  content_filters: [secret_patterns, classification_markers]
  link_allowlist: []                         # empty = no restriction
  rate: { per_minute: 10, per_hour: 100 }
  reply_depth_max: 6                         # loop guard per thread
  prefix: ""                                 # agent is already a named user; optional marker
limits: { max_results: 50, max_writes_per_run: 30 }
audit: { path: /var/log/agent-cli/teams.audit.jsonl }
```

Commander groups: resolving an Entra group at runtime needs a group-read scope (`GroupMember.Read.All`, admin consent) ⚠️; v1 uses a static list of AAD object ids maintained by config management. Revisit if the list becomes unwieldy.

**Idempotency.** Graph has no idempotency key for chat sends ⚠️. The CLI keeps a local ledger keyed by `--idempotency-key` and, for P1, scans recent messages for an embedded marker before re-sending, so a retried command does not post twice.

## 8. Teams, Entra and tenant configuration (hand to platform/Teams admins)

1. **Agent user**: licensed (Teams, Exchange), named per convention, member of the `agents` group; created by the normal AD/Entra process.
2. **Shared app** `agent-graph-cli` with Teams scopes consented (§5). Channel read scopes need admin consent ⚠️.
3. **Membership** is the access boundary: add the agent user only to the teams/channels and chats it must see; prefer private channels or dedicated agent channels for sensitive topics. Document owners who may add or remove it.
4. **Conditional Access** for the agent group tested against non-interactive refresh-token use (see `agent-okta-d-PRD.md` §7.5). It decides the human re-enrollment interval.
5. **Teams policies**: messaging policy and external-access policy for the agent user as for any employee; block guest/external chat for it ⚠️.
6. **Retention/eDiscovery**: messages sent by the agent user appear as that user's Teams messages and fall under normal Teams retention ✅ (concept; confirm with compliance ⚠️).
7. **Enrollment runbook**: who holds the agent user's sign-in factors, how an operator completes the device-code sign-in, how often it recurs.

## 9. Threat model and controls

| Threat | Control |
|---|---|
| **Prompt injection from chat participants.** Anyone who can @mention the agent or post in a watched thread can try to instruct it | `can_instruct` classification by AAD object id from the Graph message (`from.user.id`), not display text; untrusted marking; narrow tool permissions in other systems |
| **Impersonating a commander** | Sender id comes from Graph's `from.user.id`, which cannot be spoofed in message text; display names are never used for authorization |
| **Agent reads or posts beyond its role** | The delegated token reaches every chat/channel the agent user belongs to, so (a) keep membership minimal and (b) the CLI refuses any destination not in policy |
| **Data exfiltration via Teams** | Destination allowlist, no external/guest chat, secret-pattern filter, size limit, audit; consider DLP for Teams |
| **Spam / loops** (agent ↔ agent, retries) | Rate limits, `reply_depth_max`, idempotency ledger, ignore own messages, agents classified `is_agent` and never `can_instruct` by default |
| **Broadcast abuse** | Block channel/team/tag mentions by default |
| **Stolen refresh token** | Held by the daemon behind the Okta-federated secret store; Conditional Access (location, CAE) ⚠️; revoke sign-in sessions |
| **Overbroad consent** | Request only the Teams scopes needed; channel read only where required; inventory the app's consented scopes regularly |
| **Throttling / abuse of Graph** | Polling interval floor, jittered back-off, honor `Retry-After`; Teams message APIs have their own throttling limits ⚠️ |

**Kill switch.** Disable the agent user and revoke sign-in sessions (Entra), and/or disable the agent's Okta app so the daemon can no longer read the stored refresh token. Measure the access-token and CAE windows in the drill (`agent-okta-d-PRD.md` §13).

## 10. Testing and validation

- `teams selftest`: allowed destination send OK; unlisted destination refused; broadcast mention refused; oversize and secret-pattern messages refused; `GET /me` UPN matches policy; **negative test: reading a chat the agent user is not in returns 403/404**.
- Unit tests: policy evaluation, mention parsing, sender classification, cursor/de-dup logic, rate limiter, ledger.
- Integration (sandbox tenant): device-code enrollment, send to a chat and a channel, receive a 1:1 message and an @mention by polling, refresh after one hour and after a 7-day idle window, behavior under Conditional Access.
- Security: prompt-injection corpus surfaces only as `untrusted`; non-commander never `can_instruct`; loop simulation between two agent users; token never logged or written to the agent's files.
- Compliance: verify agent messages appear in retention/eDiscovery.

## 11. Alternative runtime: Entra Agent User (spike plan)

Microsoft's agent identity model has an Agent Identity Blueprint, Agent Identity and an optional **Agent User** (user object with mailbox, Teams presence and directory entry) ✅. The token chain is blueprint credential → agent identity token → agent-user token via `user_fic` ✅, and a federated credential can sit on the blueprint ✅, which could let Okta be the trust root and remove the human enrollment.

Why not v1: Agent User creation uses Graph beta ✅, each agent user needs a Microsoft 365 license ✅, user-account mode is described as requiring the Frontier preview program ✅, and Microsoft's reference implementation is a research implementation ✅.

Spike (M0/M5): provision blueprint, identity and user in a sandbox; obtain tokens; send channel and chat messages; receive a DM and @mention by polling; record licensing, latency, throttling and admin steps. Move to it when provisioning is GA (non-beta), license cost is acceptable, security signs off on the chain, and admins accept person-like agents. Because the `msgraph` provider hides the token source, only the daemon changes.

## 12. Non-functional requirements

- Go, static binary, macOS and Linux; shares [`agent-cli-core`](https://github.com/stainedhead/agent-cli-core) (its own repository; see `agent-cli-core-PRD.md`) and the daemon client.
- No hosted components; no listeners; outbound HTTPS to Graph only.
- Polling cost: bounded by `poll_interval`, number of watched destinations and Graph throttling; target < 2 Graph calls per destination per poll.
- Audit JSONL per command (no bodies by default); correlate with Entra sign-in logs (non-interactive) for the agent user.
- Local overhead < 50 ms per command excluding Graph latency.

## 13. Delivery plan and acceptance

| Milestone | Scope | Acceptance |
|---|---|---|
| **M0 Spikes** | Device-code enrollment as an agent user; Conditional Access behavior and refresh-token lifetime; send to chat and channel; polling for 1:1, @mention and channel messages (delta vs list); Agent User feasibility; confirm every ⚠️ | Spike report with measured poll latency, throttling and re-enrollment interval |
| **M1 Core** | `whoami`, `destinations`, `send`, `inbox`, `ack`, policy, cursors, audit | Agent posts to an approved channel and receives an @mention |
| **M2 AuthZ and controls** | `can_instruct`, destination allowlist, rate limits, content filters, loop guard, idempotency | Security tests in §10 pass |
| **M3 Threads/context** | `reply`, `thread get`, watched channels | Context-aware replies in threads |
| **M4 Hardening** | Signed policy, release signing, harness skill doc, kill-switch drill | Drill report; security sign-off |
| **M5 Agent User decision** | Spike results vs §11 criteria | Decision memo |
| **P2** | Webhook for notification-only destinations, file attachments, group-based commanders | As needed |

## 14. Concerns and recommendations

1. **Polling, not push.** Without a public endpoint there are no change notifications ✅ (they require a reachable HTTPS endpoint). *Recommendation:* accept 15–60 s latency; design workflows around @mentions and DMs; tune polling per destination.
2. **A person-like account with a long-lived refresh token.** *Recommendation:* secret store behind the Okta-federated role, Conditional Access scoping, session-revocation drill, and a published exposure-window table.
3. **Human-assisted enrollment.** Someone must sign in as the agent user. *Recommendation:* assign the enrollment to the agent's owner with a runbook; evaluate Agent User to remove it.
4. **Membership is the real permission boundary.** *Recommendation:* review agent membership quarterly; use dedicated channels; keep the CLI allow-list as defense in depth.
5. **Chat is an instruction channel.** *Recommendation:* enforce `can_instruct`; make the skill document explicit; never let chat text trigger privileged actions without the target system's own controls.
6. **Admin consent for channel read scopes** (`ChannelMessage.Read.All`) lets the agent read every channel it is a member of. *Recommendation:* prefer chats and @mention-only workflows in v1; request channel read only where justified.
7. **Licensing and cost.** Each agent needs Teams/Exchange licenses ⚠️. *Recommendation:* include in the onboarding cost model.
8. **Preview risk for Agent User** ✅. *Recommendation:* no production dependency on beta APIs; keep the token source behind the daemon.
9. **Webhook shortcut.** Workflows/incoming webhooks give one-way notifications quickly. *Recommendation:* only for non-sensitive notifications, URL treated as a secret and rotated.

## 15. Open questions

1. Which Teams capabilities are needed on day one: notifications only, or two-way commands? (Notifications-only could use the webhook path and skip delegated tokens entirely.)
2. Is a licensed Teams user per agent acceptable (cost, provisioning, naming)?
3. Who may sign in as the agent user to enroll, and what Conditional Access applies to the agent group?
4. Who are the "commanders" for each agent (team lead group, on-call group), and is a static AAD id list acceptable?
5. Are agent-to-agent conversations in Teams wanted?
6. Retention, legal hold and DLP requirements for agent-posted content.
7. Is Agent 365 / Entra Agent ID licensed in your tenant, and is a preview dependency ever acceptable?

## 16. CI/CD and release requirements

Applies to this repository only; the four Go repositories in the set (`agent-okta-d`, `snow-cli`, `outlook-cli`, `teams-cli`) use the same pipeline shape so a pipeline change is made once and copied. Pipelines are GitHub Actions workflows under `.github/workflows/`. The scaffolded `ci.yml` is a starting point and must be brought in line with this section. Items marked ⚠️ are not confirmed against vendor documentation and need a spike before the pipeline depends on them.

**Terminology.** *CI* verifies a change. *CD* produces and publishes a **release**: a semver-versioned set of signed artifacts. **Publishing a release is the whole of "deploy" in this section.** Rolling a release out to agent hosts, harness images or AWS accounts is the swarm owner's job (see REL-12).

### 16.1 Continuous integration

| ID | Requirement |
|---|---|
| BLD-1 | CI runs on **every pull request targeting `main`** and **on demand** (`workflow_dispatch`, optionally against a chosen ref). CI also runs as the first stage of every release (REL-9), so nothing is released untested. |
| BLD-2 | Checks: `gofmt -l .` is empty; `go mod tidy` leaves no diff; `go vet ./...`; `golangci-lint` at a pinned version; `go test -race ./...`; `govulncheck ./...`. |
| BLD-3 | Every release target (REL-1) is **cross-compiled on each PR**, so a portability break is found before merge, not at release time. |
| BLD-4 | PR CI needs **no credentials and no network access to real systems**: tests use fakes, mock endpoints and fake clocks. `teams selftest` (§10) and the sandbox-tenant integration tests need a Teams-licensed test agent user, so they run **only on demand**, never in PR CI. CI must never post to real chats or channels. |
| BLD-5 | The CI workflow is a **required status check** on `main` once branch protection is enabled. Branch protection is not configured yet; enabling it is a separate step. |
| BLD-6 | Workflows use least privilege (`permissions: contents: read` for CI), pin the Go version from `go.mod`, and pin third-party actions to a version or commit SHA. |

### 16.2 Release targets and artifacts

| ID | Target | Build | Artifact |
|---|---|---|---|
| REL-1a | **macOS, Apple silicon** | `darwin/arm64` | `.tar.gz` containing the `teams` binary, signed and notarized with an Apple Developer ID ⚠️ (see 16.8 item 1). |
| REL-1b | **Windows via WSL2** | `linux/amd64` (and `linux/arm64` for WSL on Arm, see 16.8) | `.tar.gz`; WSL runs Linux binaries, so **this is the Linux build** and no native Windows `.exe` is produced. Native Windows is not a target. |
| REL-1c | **Linux, AWS-hosted container** | `linux/amd64` and `linux/arm64` (Graviton) | Multi-arch **OCI image** `ghcr.io/stainedhead/teams-cli:vX.Y.Z`, non-root, minimal base, plus the same Linux binaries as `.tar.gz` |

Common to all targets:

- REL-2. Each release also publishes `SHA256SUMS`, an SBOM (SPDX or CycloneDX), a build-provenance attestation, and a signature for every artifact. Linux and container artifacts are signed with `cosign` keyless signing from the workflow's GitHub OIDC identity ⚠️. The install documentation in `user-docs/` states how to verify them.
- REL-3. Builds are reproducible as far as Go allows: pinned toolchain, `-trimpath`, `CGO_ENABLED=0` where possible, and a build timestamp taken from the commit.
- REL-4. The binary reports its version (`teams version`: semver, commit, build date), stamped with `-ldflags`. The version is also surfaced in the generated harness skill document.
- REL-4a. `teams` is deployed into the agent's host or container, so the **tarball is the primary artifact** for baking into a harness image. The OCI image is also published for use as a build stage (`COPY --from`). The CLI keeps local polling state (per-chat cursors); the container image must expect a writable state directory.

### 16.3 Versioning

| ID | Requirement |
|---|---|
| REL-5 | Releases follow **semantic versioning** (`MAJOR.MINOR.PATCH`). The git tag `vX.Y.Z` on `main` is the release identity. Tags are immutable: a version is never re-tagged or re-published. |
| REL-6 | Releases start at `0.1.0` and stay `0.y.z` while this PRD is a draft. `1.0.0` is cut by an explicit decision, never automatically. |
| REL-7 | The bump is taken from a **PR label** (`release:major`, `release:minor`, `release:patch`). An unlabeled PR that changes shipped code defaults to `patch`. A PR that touches only `docs/`, `user-docs/`, `specs/`, `*.md` or `INTENT.md` does **not** cause a release. This tool builds on the shared `agent-cli-core`, which is its own repository ([stainedhead/agent-cli-core](https://github.com/stainedhead/agent-cli-core), specified in `agent-cli-core-PRD.md`; it originated in `snow-cli-PRD.md` §5). `teams` pins a released version of it (see 16.6). |

### 16.4 Continuous delivery

| ID | Requirement |
|---|---|
| REL-8 | CD runs **on merge of a pull request to `main`** and **on demand** (`workflow_dispatch` with a `bump` of `major`, `minor` or `patch`, an optional explicit `version`, and a `dry_run` option that builds and verifies but publishes nothing). |
| REL-9 | Stages, in order: CI gate (all of 16.1), compute version, cross-build every target, package, checksum, SBOM, sign and attest, **smoke-verify**, publish. Publishing creates the tag, a GitHub Release with notes generated from merged PR titles, and pushes the container image tagged `vX.Y.Z` and `vX.Y`. No `latest` tag is relied on; consumers pin a version. |
| REL-10 | Smoke-verify runs the built artifact before anything is published: the `linux/amd64` binary and the container image on a Linux runner, the `darwin/arm64` binary on an Apple-silicon runner. Each must run `teams version` and report the expected version. |
| REL-11 | **All-or-nothing:** if any target fails to build, sign or verify, nothing is published. A failed run is safe to re-run, and a version is never published twice. |
| REL-12 | CD **does not roll out** a release. It does not deploy to AWS accounts, restart daemons, or rebuild harness images. The harness images in `agentic-team-w-paperclip` are intended to consume a released artifact by pinned version ⚠️ (to be agreed with that repository), rather than build this tool from source. |
| REL-13 | The release job gets only what it needs (`contents: write`, `packages: write`, `id-token: write`, attestations) from a protected `release` environment. Apple signing material lives only in that environment's secrets. On-demand runs require write access to the repository, and a `major` bump on demand should require a reviewer approval on the environment. No long-lived cloud credentials are stored in the repository. |
| REL-14 | A bad release is not deleted. It is superseded by a newer patch release and marked as withdrawn in its release notes; its tags and images stay in place. |

### 16.5 Repository-specific requirements

- **Release contents:** the `teams` binary, a sample policy file (destinations, commanders, rates) with placeholder values only, and the generated harness skill document.
- **No Microsoft credentials in CI:** no refresh token or Graph token exists in the pipeline; device-code enrollment is a human step done outside CI.
- **Runtime alternatives:** adopting the Entra Agent User model (§11) changes only how the daemon obtains the token, so it does not change this pipeline.

### 16.6 Dependency on agent-cli-core

`agent-cli-core` ([stainedhead/agent-cli-core](https://github.com/stainedhead/agent-cli-core), a Go library with no binary) is its own repository and a build dependency of this tool. The dependency chain is `agent-okta-d` (`pkg/client`) <- `agent-cli-core` <- `snow-cli`, `outlook-cli`, `teams-cli`.

| ID | Requirement |
|---|---|
| DEP-1 | `go.mod` declares `github.com/stainedhead/agent-cli-core` at a **released semver tag**. No pseudo-versions and no `replace` directives on `main`. |
| DEP-2 | Every workflow job that builds or tests resolves dependencies with the job's dynamic `GITHUB_TOKEN`: no personal access token and no stored secret. Such jobs set `permissions: contents: read` and `packages: read`. |
| DEP-3 | Before `go mod download`, the job sets `GOPRIVATE=github.com/stainedhead/*` and configures git `url."https://x-access-token:${GITHUB_TOKEN}@github.com/".insteadOf "https://github.com/"` from the job token. The token is never echoed and never written to caches or artifacts. |
| DEP-4 | The repositories are public today, so the token is not strictly needed. The step is standard so that behavior is identical if visibility changes. |
| DEP-5 | ⚠️ `GITHUB_TOKEN` is scoped to the repository running the workflow, so it cannot read a different private repository's contents. If `agent-cli-core` or `agent-okta-d` ever become private, they must be published through GitHub Packages with consumer repositories granted read on the package, decided before any visibility change. GitHub Packages has no Go module registry (unconfirmed). |
| DEP-6 | Bumping the `agent-cli-core` version is an ordinary PR and must pass CI. |

### 16.7 Milestone placement

BLD-1 to BLD-6 are in place before the first milestone that merges Go code. The release pipeline (REL-1 to REL-14) is in place before the first tagged build, and no later than the first milestone that produces a runnable binary. Release signing and notarization may land later, in the hardening milestone, but unsigned builds are labelled pre-release until then.

`teams` cannot compile against `agent-cli-core` until the core has a tagged release, which itself needs `agent-okta-d` to tag a release containing `pkg/client`. No code and no releases exist yet, so `go.mod` has no `require` for the core.

### 16.8 Open items (CI/CD)

1. **Apple signing.** Is an Apple Developer ID and notarization account available for CD? Until it is, darwin artifacts carry only the `cosign` signature and users must clear the quarantine attribute themselves ⚠️.
2. **Registry.** `ghcr.io` is assumed, matching `agentic-team-w-paperclip`. Should images also be pushed to Amazon ECR for the AWS-hosted container case?
3. **What "deploy" means.** This section treats it as publishing a release (REL-12). Confirm that no automatic rollout into an AWS environment is wanted.
4. **Version bump rule.** PR labels are assumed (REL-7). Conventional commits are the alternative.
5. **WSL on Arm.** Is `linux/arm64` for WSL wanted, or `linux/amd64` only?
6. **Shared pipeline.** Should the common workflow steps live in one reusable workflow? `agent-cli-core` is its own repository (decided), but whether a reusable workflow is worth having, and where it would live, is still open.
7. **WSL service support.** Running the daemon's service definition under WSL needs systemd in the WSL distribution ⚠️; confirm before documenting it as supported. Applies only where this tool installs a service.

## 17. Agent skill document

Agents that adopt this tool need to know how to use it. That knowledge is a **skill document**, published in one place for the whole set: the root repository's `skills/` folder (https://github.com/stainedhead/agentic-teams/tree/main/skills), one file per repository, named `<repo-name>.md`. The root repository is where agents find and adopt it.

| ID | Requirement |
|---|---|
| SKILL-1 | **One home.** The skill for this repository is `skills/teams-cli.md` in the root `agentic-teams` repository. This repository does not keep a second copy. The root `README.md` and `skills/README.md` tell agents to adopt it from there. |
| SKILL-2 | **Minimum content.** An availability banner (how to check the tool is installed with `command -v`, and the version the skill applies to); when to use the tool and when not to; the commands the tool really has, each with a short example and whether it reads or writes; the output shape and exit codes (shared conventions are in `skills/agent-cli-core.md`); the rules and forbidden actions; how untrusted content and instructions found in it are treated; the rule that the agent never asks for, reads, prints or stores credentials; a table mapping each error to the action the agent should take; and links to this repository. |
| SKILL-3 | **Source of truth.** The command tree in this repository, through `agent-cli-core`'s `docgen` (`agent-cli-core-PRD.md`, 6.7). Each release publishes the generated skill as an artifact named `teams-cli.md`, and the root copy is updated from it, so the commands, flags, forbidden actions and exit codes in the skill cannot drift from the code. Hand-written guidance that `docgen` cannot derive lives in the root copy and is preserved when it is updated. |
| SKILL-4 | **Currency.** A change to the command surface, flags, exit codes, policy verbs or write modes, or forbidden actions is not complete until the root skill is updated and names the version it applies to. Release notes link to the skill revision for that version. |
| SKILL-5 | **Honest availability.** Until a release exists the skill carries a banner saying the tool is planned and not installed, and tells agents to report that instead of building or reimplementing it. The banner is removed only after a release is published and the skill's examples have been run against it. |
| SKILL-6 | **Format.** Plain Markdown with `name` and `description` frontmatter. The skill format each harness (Hermes, or the CLI harness we provide) expects is not defined yet ⚠️ (`agent-cli-core-PRD.md`, CORE-DOC-4); the format may be adapted without changing the content. |
| SKILL-7 | **Milestone placement.** A reviewed skeleton skill exists by the first milestone that produces a runnable binary, and a complete skill is an acceptance item of that milestone and of the hardening milestone, not only the latter. |

### 17.1 Open items (agent skill)

1. **Updating the root from this repository's release.** Publishing a change to another repository's `skills/` folder needs write access to that repository. The workflow's dynamic `GITHUB_TOKEN` is scoped to the repository running the workflow ⚠️, so release CD cannot do it with the token this PRD otherwise requires. Options: a manual pull request opened from the release artifact (assumed until decided), a GitHub App installation token, or a fine-grained personal access token. Decide before automating.
2. **Skill for the library and for the daemon.** `agent-cli-core` has a shared-conventions skill, `skills/agent-cli-core.md`, that the three CLI skills link to instead of repeating the envelope, exit codes and untrusted-content rules; `agent-okta-d` has an awareness skill for what agents must never do on a host where the daemon runs.

## Appendix — Sources consulted

- Microsoft Graph: [Send message in a chat (delegated `ChatMessage.Send`/`Chat.ReadWrite`; application only for migration)](https://learn.microsoft.com/en-us/graph/api/chat-post-messages?view=graph-rest-1.0) · [Send message in a channel](https://learn.microsoft.com/en-us/graph/api/channel-post-messages?view=graph-rest-1.0) · [Get access on behalf of a user (`offline_access`)](https://learn.microsoft.com/en-us/graph/auth-v2-user) · [Chat/channel change notifications (need a reachable endpoint)](https://github.com/microsoftgraph/microsoft-graph-docs-contrib/blob/main/concepts/teams-changenotifications-chatmessage.md) · [Permissions reference](https://learn.microsoft.com/en-us/graph/permissions-reference)
- Entra: [ROPC limitations](https://learn.microsoft.com/en-us/entra/identity-platform/v2-oauth-ropc) · [Workload identity federation](https://learn.microsoft.com/en-us/entra/workload-id/workload-identity-federation)
- Agent identity: [Agent 365 identity](https://learn.microsoft.com/en-us/microsoft-agent-365/developer/identity) · [Agent User platform notes](https://microsoft.github.io/entrabot/platform-learnings/entra-agent-users/) · [Blueprints, identities, users](https://github.com/microsoft/entrabot/blob/main/docs/platform-docs/agent-id-blueprints-and-users.md) · [Entrabot status (research implementation)](https://microsoft.github.io/entrabot/project/status/)
- Tools: [Graph CLI retirement](https://github.com/microsoftgraph/msgraph-cli/issues/585) · [CLI for Microsoft 365](https://github.com/pnp/cli-microsoft365)
