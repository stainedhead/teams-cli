# Product Summary

`teams` is a Go CLI that lets AI agents post updates and read messages addressed to them in Microsoft Teams, acting as their own named Entra user through delegated Microsoft Graph calls. There is no relay or hosted component: the CLI polls Graph, takes short-lived tokens from the `agent-okta-d` daemon, and enforces a client-side policy on destinations, senders, rates and content. Only authorized senders can instruct an agent; all other message text is treated as untrusted data.

Status: Draft PRD v0.2; no implementation yet. Source: `../teams-cli-PRD.md`.
