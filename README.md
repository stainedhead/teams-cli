# teams-cli

`teams` is a planned Go CLI that lets AI agents talk to people in Microsoft Teams: post updates and alerts, read direct messages and @mentions, and reply in threads.

**Status: Draft PRD (v0.2). There is no implementation yet.** This repository currently holds the product requirements document and project scaffolding only.

## Why

Agents need to reach the team in Teams, but Microsoft Graph application-only permissions cannot send chat messages (only `Teamwork.Migrate.All`, for import). An earlier draft proposed a bot behind a relay service we would host; that was dropped, because we will not run a relay.

## Key design points

- **Delegated, as the agent's own user.** The agent is a licensed, person-like Entra user, and `teams` calls Microsoft Graph delegated as that user. Delegated `ChatMessage.Send` / `Chat.ReadWrite` can send chat messages.
- **No Microsoft credential on the agent host.** The `agent-okta-d` daemon holds the agent user's delegated refresh token and serves short-lived (~1 h) Graph access tokens over a unix socket (provider `msgraph`, shared with `outlook-cli`).
- **Polling, not push.** There is no public endpoint, so the CLI polls for new messages (default 15 s, minimum 5 s) instead of receiving change notifications. Inbound latency equals the polling interval.
- **Client-side policy.** A root-owned YAML policy defines allowed destinations (by alias, never raw IDs), commanders, rate limits, mention rules, content filters and a loop guard. Membership of the agent user in Teams remains the real access boundary; the policy is a second layer.
- **Untrusted by default.** Only senders classified `can_instruct` (by Entra object id from Graph, never display name) may instruct an agent; all other message text is marked untrusted.
- **Planned commands:** `whoami`, `destinations list`, `send`, `reply`, `inbox`, `ack`, `thread get`, `selftest`.
- **Trade-offs accepted:** a human enrolls the agent user once (device-code sign-in); each agent needs a Teams/Exchange license; Conditional Access must tolerate non-interactive refresh-token use.
- **Alternative runtime:** the Entra Agent User model (Agent 365) has the same runtime shape with a better enrollment story, but relies on Graph beta, per-agent licensing and a preview program. It is a spike (M0/M5), and only the daemon would change.

## Evidence caveats

The PRD marks claims as confirmed against vendor documentation (checkmark) or not confirmed (warning sign: engineering judgment or secondary sources). Many Graph endpoint shapes, the admin-consent requirements for channel read scopes, the Conditional Access behavior and several throttling and idempotency assumptions are in the second category and must be validated in a sandbox tenant before anything depends on them. Endpoint shapes in the PRD are from general Graph v1.0 knowledge and need checking against the current reference before build. See the PRD for the per-claim markers.

## Related repositories

Part of the set rooted at [stainedhead/agentic-teams](https://github.com/stainedhead/agentic-teams).

- [agent-okta-d](https://github.com/stainedhead/agent-okta-d): the daemon that holds credentials and serves tokens, including the `msgraph` provider this CLI uses.
- [snow-cli](https://github.com/stainedhead/snow-cli): its PRD (section 5) defines the shared `agent-cli-core` module (daemon client, policy, envelope, bounds, audit, untrusted marking, exit codes) that `teams` builds on. Where `agent-cli-core` will live is an open question and is not decided here.
- [outlook-cli](https://github.com/stainedhead/outlook-cli): sibling CLI sharing the `msgraph` provider and the AUTH-1..4 requirements.
- [teams-cli](https://github.com/stainedhead/teams-cli): this repository.
- [agentic-team-w-paperclip](https://github.com/stainedhead/agentic-team-w-paperclip): a companion repository in the same set.

## Planned layout

Nothing below exists yet except the documentation and tooling files.

```
cmd/teams/        entry point
internal/         domain, use cases, adapters (Graph, daemon client, poller, policy)
docs/             product and technical documentation, ADRs
user-docs/        install, configuration and usage help (once there is something to use)
specs/            feature specs; finished ones in specs/archive/
teams-cli-PRD.md  product requirements
```

## Documentation

- Requirements: [teams-cli-PRD.md](teams-cli-PRD.md)
- Product and technical notes: [docs/](docs/)
- User documentation: [user-docs/](user-docs/) (empty until the tool exists)
- Contributor and agent rules: [AGENTS.md](AGENTS.md)

## Development

```
make fmt lint test
```
