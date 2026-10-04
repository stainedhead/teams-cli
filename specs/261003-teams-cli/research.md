# Research: teams-cli
**Date:** 2026-10-03 | **Source PRD:** teams-cli-PRD.md

## Research Questions
1. What is the exact Graph v1.0 shape for chat send, channel send, channel reply, `GET /me/chats` ordering and `/messages` filtering (PRD flags all as unverified)?
2. Is the channel messages delta endpoint usable with delegated tokens, and what are its throttling limits?
3. What does agent-cli-core v0.1.0 provide (envelope, exit codes, policy, audit, daemon client, docgen) and how does outlook-cli consume it?
4. How can idempotency be approximated without a Graph idempotency key (embedded marker scan)?
5. How are 1:1 chats resolved/created on demand for `user:` aliases?

## Industry Standards
[TBD]
## Existing Implementations
[TBD] (sibling outlook-cli)
## API Documentation
[TBD]
## Best Practices
[TBD]
## Open Questions
See spec.md and PRD section 15.
## References
- teams-cli-PRD.md
