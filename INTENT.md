# Intent

## Purpose
`teams` is the Go CLI that lets an AI agent talk to people in Microsoft Teams: post updates and
alerts, read direct messages and @mentions, and reply in threads. It does this as the agent's
**own named Entra user**, never as a shared bot or as a human, and without the agent process ever
holding a Microsoft credential.

**No code exists yet.** The repository holds a draft PRD (v0.2) and project scaffolding only. Claims in
the PRD marked with a warning sign are unconfirmed against vendor documentation; treat them as
hypotheses to validate in a sandbox tenant, not as facts.

**The wider project.** The agentic-teams set lets autonomous SDLC agents work as real teammates. The
agent runs as a harness (Hermes, or a CLI harness we provide) in a container whose image comes from
`agentic-team-w-paperclip`. The Go tools in the set exist to use **Okta to secure access to the key
tooling given to those teammates**. `teams` is deployed to the machine or container the agent
identity runs inside, next to the `agent-okta-d` daemon:

```
harness in container -> teams / snow / outlook / gh / aws / git -> agent-okta-d -> Okta
                                                                -> AWS, GitHub, ServiceNow, M365, Atlassian
```

**Why identity is per-agent and attributable.** Each agent has its own Okta application and its own
user accounts downstream, so every action traces to exactly one agent, one agent can be disabled
without touching the others, and humans can see who said what. In Teams that means the agent has its
own directory entry and presence. Messages it sends are ordinary messages from that user, under
normal Teams retention. Microsoft Graph cannot send chat messages with application-only permissions,
and we will not host a relay, so `teams` calls Graph delegated as that user, with short-lived tokens
served by the daemon.

## Where this fits
| Repository | Role | To / from `teams` |
|---|---|---|
| [agentic-teams](https://github.com/stainedhead/agentic-teams) | Documentation-only root that maps the set | Explains how the pieces relate |
| [agentic-team-w-paperclip](https://github.com/stainedhead/agentic-team-w-paperclip) | Container images with the harnesses and Paperclip | Provides the runtime `teams` is deployed into |
| [agent-okta-d](https://github.com/stainedhead/agent-okta-d) | Credential daemon rooted in Okta | Provides short-lived Graph tokens (`msgraph` provider) over a unix socket |
| [snow-cli](https://github.com/stainedhead/snow-cli) | `snow`, ServiceNow CLI | Its PRD defines the shared `agent-cli-core` that `teams` is built from |
| [outlook-cli](https://github.com/stainedhead/outlook-cli) | `outlook`, mail as the agent's Entra user | Sibling: shares the `msgraph` provider and the AUTH-1..4 requirements |
| [teams-cli](https://github.com/stainedhead/teams-cli) | This repository | |

## Goals
- **Give agents a real, attributable presence in Teams.** Post to approved destinations and read what
  is addressed to them as their own user, with no Microsoft credential readable by the agent process.
- **Make only authorized people able to instruct an agent.** Authority comes from the sender's Entra
  object id, never from display text. Everyone else's text is untrusted data.
- **Keep access revocable and bounded.** Disabling the Entra user or the agent's Okta app stops Teams
  access within a measured window. Team membership is the real permission boundary, and the
  client-side policy (destinations, rates, mentions, content filters, loop guard) is a second layer.
- **Work anywhere an agent runs.** Desktops behind NAT and containers need only outbound HTTPS to Graph,
  with nothing of ours hosted or internet-reachable.
- **Share one foundation with its sibling CLIs.** Daemon client, policy, output envelope, audit and
  exit codes come from `agent-cli-core`, not from this repository.

## Non-goals
- Human mode, calls, meetings, voice, tabs, message extensions or bots.
- File upload and download (deferred), real-time delivery (v1 polls), and messaging external tenants or guests.
- Hosting any service: relay, bot endpoint or webhook receiver.
- Holding credentials or talking to Okta. That is the daemon's job.
- Being the authorization control. Graph and Teams policy enforce server side; CLI policy is a guardrail.
- Deciding where `agent-cli-core` lives. That is an open question owned by the set, not this repo.

## Scope boundary in one line
> `teams` is the agent's Teams client, a policy-bounded caller of Microsoft Graph as its own user, not
> a credential store, a bot service, or the access-control boundary.

## How this file is used
INTENT.md records *why* this repository exists and where it sits in the wider project. The *how* is in
[teams-cli-PRD.md](teams-cli-PRD.md), and the PRD wins on detail. Update this file when the goal,
direction or scope shifts (for example, adopting the Entra Agent User model or taking on notification-only
webhooks), not when implementation details change.
