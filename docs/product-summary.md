# Product Summary

`teams` is a Go CLI that lets AI agents post updates and read messages addressed to them in Microsoft Teams, acting as their own named Entra user through delegated Microsoft Graph calls. There is no relay or hosted component: the CLI polls Graph, takes short-lived tokens from the `agent-okta-d` daemon, and enforces a client-side policy on destinations, senders, rates and content. Only authorized senders can instruct an agent; all other message text is treated as untrusted data.

## Status

Built and tested against fakes: the command surface (`whoami`, `destinations list`, `send`, `reply`, `inbox`, `ack`, `thread get`, `selftest`, `version`, hidden `skill`), the typed policy engine, the idempotency ledger, the at-least-once inbox with ack, content filters, mentions, loop guard, audit log and CI.

Not usable end to end yet: the `agent-okta-d` client adapter is unreleased, so every command that needs a Graph token exits 3. Every Graph endpoint shape and tenant behavior is unverified against a real tenant (`unverified-assumptions.md`). Real-tenant spikes (M0, M5), release engineering, and the items in `deferred.md` are not done.

## Who it is for

Operators who run AI agents and need them to reach people in Teams under a policy the agent cannot edit. Adoption help is in `../user-docs/`.

Requirements source: `../specs/archive/261003-teams-cli/teams-cli-PRD.md` (Draft v0.2). Intent: `../INTENT.md`.
