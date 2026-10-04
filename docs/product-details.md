# Product Details

Describes behavior as built. Graph behavior is unverified against a real tenant. Requirements: `../teams-cli-PRD.md`; user-facing usage: `../user-docs/usage.md`.

## Command surface

| Command | Network | Purpose |
|---|---|---|
| `version` | no | Version, commit, build date. No policy needed |
| `destinations list` | no | Policy aliases with send/watch flags; never raw ids |
| `whoami` | Graph `/me` | Agent identity, policy profile, build version; enforces the UPN guard |
| `send --to ALIAS (--text T / --file F or -) [--thread ID] [--mention ...] [--idempotency-key K] [--dry-run]` | Graph | Post to a destination |
| `reply --thread ID ...` | Graph | Reply using an inbox `thread_id` |
| `inbox [--alias] [--wait N] [--limit N] [--since CURSOR]` | Graph | Poll watched destinations |
| `ack <id>...` | no | Acknowledge delivered items |
| `thread get <thread-id> [--limit N]` | Graph | Thread context, oldest first |
| `selftest [--read-only]` | Graph | Policy allow/deny matrix |
| `skill` (hidden) | no | Prints the generated agent skill document |

Global flags: `--format json|table|text`, `--max-bytes`, `--offset`. Output is the shared envelope from `agent-cli-core` and exit codes come from its table (0 ok, 1 general, 2 usage, 3 auth, 4 forbidden, 5 not found, 6 policy denied, 7 conflict, 8 rate limited, 9 validation).

## Behavior

- Aliases: `channel:<name>`, `chat:<name>`, `user:<name>`; raw ids are never accepted on the command line.
- Identity guard: once per run, Graph `/me` `userPrincipalName` must equal policy `upn`, else exit 6 before other calls.
- Send checks, first failure wins: policy and destination, size, empty or control characters, mentions, content filters, link allow-list, rate and write caps, loop guard, ledger reservation, post. `--dry-run` runs all checks with no write, no ledger entry and no rate use. Its result carries `message_id`, `thread_id`, `deduplicated`, `dry_run` and `findings`; it does not render a payload preview.
- Mentions come from policy only (`send.mentions.allow`, `display_name`); no directory lookup. Broadcast mentions are unsupported.
- Idempotency: local ledger with `pending`, `sent` and `failed` states. Writes are never retried automatically. Corrupt ledger blocks writes (exit 7). Optional marker scan is off by default and unverified.
- Inbox: reads only `watch: true` destinations; `inbound.handle` filters direct, mention and watched categories; unlisted conversations are dropped. At-least-once with explicit `ack`; edited messages are re-delivered with `edited: true`; own messages are excluded. Item ids embed the alias (`<alias>/<graph-id>`). Polling interval default 15 s, floor 5 s, jittered; `--wait` is clamped to `inbound.max_wait`.
- Sender classification uses only the Entra object id (and optionally tenant id): `can_instruct` requires membership in `instruct.commanders`; other agents never gain it. Message text and sender names are emitted as untrusted objects.
- Audit: one JSON Lines record per command with no bodies and no tokens; a failed audit write blocks `send` and `reply`.
- Throttling: reads retry on 429/503 with `Retry-After`; writes do not.
- State: atomic writes under an advisory lock; ledger loss fails closed, cursor loss fails open (bounded re-read).

## Policy

Typed YAML described in `../user-docs/configuration.md`, with a sample at `../user-docs/teams.policy.sample.yaml`. Trust is by file ownership (root-owned, not group or world writable, no symlink). Release builds cannot disable the trust check.

## Out of scope or deferred

See `deferred.md`: attachments, webhooks, group commanders, unlisted-conversation surfacing, signed policy, native Windows, release engineering, real daemon adapter, real-tenant verification.

## Acceptance

Acceptance criteria AC-1..AC-26 are in the spec and are verified with fakes only; none was run against a real tenant.
