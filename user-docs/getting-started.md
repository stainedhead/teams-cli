# Getting started

## Read this first: what works today

- `teams version` works with no setup.
- `teams destinations list` works offline once a valid, trusted policy file is in place.
- Every command that talks to Microsoft Graph (`whoami`, `send`, `reply`, `inbox`, `ack`, `thread get`, `selftest`) needs a Graph token from the `agent-okta-d` daemon. The client adapter for that daemon has not been released, so `teams` currently reports the daemon as unreachable and exits with code 3, naming the socket path it tried. There are no fallback credentials and none will be added.
- Because of that, nothing in this documentation about Graph behavior has been run against a real Microsoft 365 tenant. Endpoint shapes, permissions, consent requirements, throttling and Conditional Access behavior are all unverified. Treat them as expectations, not guarantees.

Use this time to prepare the policy, the agent user and the tenant so that adoption is quick once the daemon adapter ships.

## How it fits together

1. A dedicated, licensed Entra user (the agent user) represents the agent in Teams.
2. A human enrolls that user once with the `agent-okta-d` daemon (provider `msgraph`). The daemon holds the refresh token; `teams` never sees it.
3. `teams` asks the daemon for a short-lived Graph access token on each run and calls Microsoft Graph as that user.
4. A root-owned policy file limits which chats, channels and people the agent can post to or read, how fast it can post, and what content is allowed.
5. `teams` polls for new messages; there is no push and no hosted component.

## Prerequisites

- A Microsoft 365 tenant with Teams, and a dedicated Entra user for the agent with a Teams-capable license. Each agent needs its own license.
- That user must already be a member of every chat and channel it should use. Membership is the real access boundary; the policy is a second layer.
- Tenant admin consent for the delegated Graph permissions the tool needs. Reading channel messages is expected to need admin consent (unverified). Direct and group chat use is expected to need `Chat.ReadWrite`; channel posting `ChannelMessage.Send`; channel reading `ChannelMessage.Read.All` and `Channel.ReadBasic.All`. Confirm against current Microsoft documentation for your tenant.
- Conditional Access that tolerates non-interactive refresh-token use for the agent user. If it does not, a human must re-enroll often (unverified).
- The `agent-okta-d` daemon running where the agent runs, once its client adapter is released.

## Enrollment is a human step

Enrollment of the agent user (a device-code sign-in as that user, performed with the daemon) must be done by a person. The agent must never do it. If the daemon reports that re-enrollment is required, `teams` exits 3 and a human must repeat the sign-in. The exact enrollment command belongs to `agent-okta-d`; follow its documentation.

## Steps

1. Build and install `teams` ([Install](install.md)).
2. Copy [teams.policy.sample.yaml](teams.policy.sample.yaml) and replace every placeholder (GUIDs, team, channel and chat ids, the agent UPN) with values from your tenant. Remove destinations you do not want. Install it as root at `/etc/agent-cli/teams.policy.yaml` ([Configuration](configuration.md)).
3. Create the state directory on persistent storage ([Inbox, ack and state](inbox-and-state.md)).
4. Check the setup that does not need the daemon:

```
teams version
teams destinations list
```

`destinations list` prints aliases only, such as `channel:sdlc-alerts`. Raw ids never appear in output, and you must always use aliases on the command line.

5. Once the daemon adapter is available and the agent user is enrolled:

```
teams whoami
teams selftest --read-only
teams send --to channel:sdlc-alerts --text "Hello from the agent" --dry-run
teams send --to channel:sdlc-alerts --text "Hello from the agent"
teams inbox --wait 30
```

Until then, steps 5 fail with exit 3 as described above. See [Troubleshooting](troubleshooting.md).

## Things to know before you rely on it

- Inbound latency equals the polling interval (default 15 seconds, minimum 5).
- Messages in chats or channels not listed in the policy as watched destinations are never read, even if they mention the agent. A new direct message from a person is invisible until you add a `user:` destination for them.
- Message text, sender display names and links are marked untrusted. Only senders whose Entra object id is in `instruct.commanders` get `can_instruct: true`.
- Not supported: files and attachments, webhooks or change notifications, group-based commander lists, calls and meetings, external tenants or guests, native Windows.
