# Root skill update needed

Source for the "Root skill update needed" section of the pull request. The root skill (`skills/teams-cli.md` in https://github.com/stainedhead/agentic-teams) is updated by a manual pull request in that repository; it is not edited from here. The generated skill is produced by `make skill` (file `dist/teams-cli.md`, from the hidden `teams skill` command) and compared below with the current root skill.

## Pull request text

The root skill still describes `teams` as "planned, not released" with PRD-level details. The CLI is now built, and its generated skill differs from the root skill as follows. Everything about Microsoft Graph behavior remains unverified against a real tenant, and the daemon adapter is unreleased, so network commands currently exit 3.

### Status and framing

- Remove the "planned, not released" banner and the "provisional format" note. Keep a short status note: the CLI exists and talks to `agent-okta-d` through core's oktad adapter, but Graph behavior is unverified.
- The generated skill's front matter has `name: teams` and a description that embeds the build version ("Applies to teams <version>") and the untrusted-data warning. The root description is trigger-oriented ("Use when asked to post..."). Decide which form the root keeps; the generated text should not be pasted over the trigger description.

### Command surface

- Generated commands: `ack`, `destinations list`, `inbox`, `reply`, `selftest`, `send`, `thread get`, `version`, `whoami`. A hidden `skill` command exists and is not documented.
- New flag not in the root skill: `inbox --alias ALIAS` (restrict to one destination).
- `reply` takes `--thread ID`, `--text T` or `--file F|-`, `--mention`, `--idempotency-key`, `--dry-run`. The root lists only `--thread` and `--text`.
- `send --thread ID` is accepted (the thread must belong to the same `--to` destination); the root already lists it.
- `--file -` reads standard input. `--file` input over 4 MiB is refused.
- `thread get <thread-id> [--limit N]` (default 20); the root lists `--limit 20` as the default, which matches. `inbox --limit` defaults to 20 and is capped by policy.
- Remove "(unverified: ...)" markers only after a tenant spike; keep them for now.
- Global flags `--format json|table|text`, `--max-bytes`, `--offset` exist on every command and are not in the root skill.

### Ids and ack semantics

- Item `id` and `thread_id` are alias-qualified (for example `chat:dev-team/1696341950000`, and chats use `<alias>/chat`). The root says only `<id>`. Pass them exactly as returned.
- Delivery is at-least-once: unacked items are returned again on every `inbox` call until acked, edited messages that were acked are returned again with `edited: true`, and `ack` of an unknown id exits 9. The root says `ack` "advances the local cursor"; replace with the at-least-once wording and tell the agent to ack only after the work is done and to treat `id` as an idempotent key.
- Add the `cursor` field (used with `--since`) and `links` field (plain strings, never fetched) to the documented item shape. The item also has `edited`.

### Output shape (differences from the root text)

- `text` is `{"untrusted": true, "value": "...", "author": "...", "timestamp": "..."}`, not just `{untrusted, value}`.
- `sender.name` is also an untrusted object, not a plain string. Authorize only on `sender.can_instruct`.
- `whoami` returns `{id, display_name, upn, policy, version}`. It does not list destinations or limits as the root says; use `destinations list` for destinations. Policy limits are not shown to the agent.
- `destinations list` returns `{alias, kind, send, watch}` plus `display_name` for `user:` entries. It needs the policy file but no network.
- `send` and `reply` return `{message_id, thread_id, deduplicated, dry_run, findings}`. `--dry-run` reports the decision and content-filter findings; it does not print a payload preview.
- `ack` returns `{acked, already}`.
- List commands return `data` as a JSON array and use the standard truncation metadata.

### Behavior notes to add or correct

- Inbox reads only destinations the policy marks `watch: true`. Messages in unlisted chats or channels never appear, even when they mention the agent. A new direct message needs a `user:` destination in policy.
- `inbox --wait N` is clamped to the policy `inbound.max_wait`, not rejected.
- Channel replies are polled only for threads the agent posted in.
- Idempotency: the same key with a different message exits 7; a key whose earlier send state is unknown exits 7 and must not be retried blindly; writes are never retried automatically. The root says the marker scan is "a planned P1 feature"; it is built behind `send.marker_scan` (default off) and unverified.
- The loop guard is keyed on the full thread id; the root's `reply_depth_max` text is accurate.
- Exit 9 also covers a missing, untrusted or invalid policy file, and an unknown id passed to `ack`.
- Exit 1 also results from a failed `selftest` (the message still names the socket when the daemon is unreachable). Other network commands exit 3 when the daemon is unreachable or the agent user is not enrolled, and 8 when it is degraded.

### Generated-skill differences worth deciding

- The generated page includes per-command "Never" lists (for example never address a destination by raw id, never include secrets, never loop replies with another agent). The root's Rules section covers most of these; keep the root wording.
- The generated page's exit-code table is the shared core table; the root's `teams`-specific tables are richer. Keep the root's.
- The generated page links the shared conventions to an `agent-cli-core` skill; the root already links `agent-cli-core.md`.
- The generated page has no sections on enrollment being a human step, the daemon, polling latency or the `can_instruct` rules (only a short "Never" line under `inbox`); the root must keep those sections.

### Links

- The root's Links section may now point to `user-docs/` for install, configuration, usage and troubleshooting.
