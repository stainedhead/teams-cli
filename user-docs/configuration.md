# Configuration and policy reference

All behavior limits live in one YAML policy file. A starting point is [teams.policy.sample.yaml](teams.policy.sample.yaml); every identifier in it is a placeholder.

## Environment variables

| Variable | Default | Purpose |
|---|---|---|
| `TEAMS_POLICY` | `/etc/agent-cli/teams.policy.yaml` | Path to the policy file |
| `TEAMS_STATE_DIR` | none | State directory, used only when the policy does not set `state_dir`. A `state_dir` in the policy always wins |
| `AGENT_OKTA_D_SOCKET` | `/run/agent-okta-d/agent-okta-d.sock` (assumed) | Daemon unix socket |
| `AGENT_ID` | the policy `profile` | Does not change the audit identity, which is always the policy `profile`. A different value is recorded in the audit line as `claimed_agent=<id>` |
| `AGENT_RUN_ID` | random per run | Optional run identifier recorded in audit lines |

Blank values count as unset. Tokens are never read from the environment.

Environment variables are controlled by the agent, so they cannot widen what the policy fixes. Set `state_dir` in the policy to pin the state directory. The state directory is still writable by the agent user, so the send rate limit, the reply-depth guard and the idempotency ledger protect against mistakes and prompt injection, not against a hostile process with file access to that directory.

## Global flags

Accepted by every command: `--format json|table|text` (default `json`), `--max-bytes N` to bound output size, `--offset N` to resume truncated list output.

## Policy file trust

The policy file is the agent's guardrail, so the agent must not be able to edit it. The file and every directory above it must be owned by root (or a configured trusted uid), must not be group or world writable, and the file must not be reached through a symlink. Otherwise `teams` exits 9 and runs nothing. The file must be at most 1 MiB, contain exactly one YAML document, and use only known keys (unknown or duplicate keys are rejected).

## Policy fields

Durations use Go syntax such as `15s`, `30m`, `24h`.

### Top level

| Field | Required | Default | Notes |
|---|---|---|---|
| `version` | yes | none | Must be `1` |
| `profile` | yes | none | Free-text profile name shown by `whoami` |
| `upn` | yes | none | Agent user principal name. Compared (case-insensitive) with Graph `/me` before any other call; a mismatch exits 6 |
| `tenant_id` | no | unset | Tenant GUID. If set, senders from another tenant never get `can_instruct` |
| `state_dir` | no | `/var/lib/agent-cli/teams` | Absolute path. When set it cannot be overridden by `TEAMS_STATE_DIR` |
| `audit.path` | yes | none | Absolute path of the JSON Lines audit log |

### `destinations`

A map from alias to definition. Aliases are `channel:<name>`, `chat:<name>` or `user:<name>`, where `<name>` is 1 to 64 characters from `a-z 0-9 . _ -`. Anything not listed is denied. At least one destination is required.

| Kind | Fields |
|---|---|
| `channel:` | `team_id`, `channel_id` |
| `chat:` | `chat_id` |
| `user:` | `aad_id`, `display_name` (required if the alias is mentionable), `create_chat` (default `false`) |

Every kind also takes `send` (may post) and `watch` (inbox and `thread get` may read). Each kind accepts only its own id fields. `create_chat: true` lets `teams` create a one-to-one chat when none exists; otherwise a missing chat is reported as not found (exit 5).

### `instruct`

| Field | Meaning |
|---|---|
| `commanders.aad_ids` | Entra object ids (GUIDs) of senders allowed to instruct the agent. A static list; groups are not supported |
| `agents.aad_ids` | Object ids of other agents. These are flagged `is_agent` and never gain `can_instruct` unless also listed as commanders |

Authorization uses the sender's object id from Graph, never a display name or message text.

### `inbound`

| Field | Default | Limit | Notes |
|---|---|---|---|
| `handle` | all three | `direct`, `mentions`, `watched` | Which categories of message inside watched destinations are returned |
| `max_lookback` | `30m` | at most `24h` | How far back inbox re-reads after state loss and how long unacked items stay deliverable |
| `poll_interval` | `15s` | at least `5s` | Time between polls during `inbox --wait` (jittered about 20 percent) |
| `max_wait` | `120s` | | Upper bound for `--wait`; a larger request is clamped, not rejected |
| `thread_poll_max` | `5` | | Channel threads the agent started that are checked for replies |

### `send`

| Field | Default | Notes |
|---|---|---|
| `max_bytes` | `8000` | UTF-8 bytes, including `prefix` |
| `prefix` | empty | Prepended to every message |
| `mentions.allow` | empty | `user:` aliases that may be mentioned; each needs a `display_name` |
| `mentions.block_broadcast` | `true` | Must be `true`; channel, team and tag mentions are not supported |
| `mentions.max` | `5` | Mentions per message |
| `content_filters` | none | Any of `secret_patterns`, `classification_markers` |
| `classification_markers` | empty | Case-insensitive substrings that block a send when `classification_markers` filter is on |
| `link_allowlist` | empty (unrestricted) | Bare hosts such as `example.com` or `*.example.com`; when non-empty every link host must match |
| `rate.per_minute` / `rate.per_hour` | `10` / `100` | Sliding windows over the local send history |
| `reply_depth_max` | `6` | Maximum agent messages per thread within `reply_window` (loop guard) |
| `reply_window` | `24h` | |
| `marker_scan` | `false` | Optional duplicate-send check; relies on unverified Graph behavior |

### `limits`

| Field | Default | Notes |
|---|---|---|
| `max_results` | `50` | Cap on items per `inbox` or `thread get` |
| `max_writes_per_run` | `30` | Posts per process |
| `max_chat_scan` | `50` | Bound on chat pages scanned when resolving a `user:` destination |

### `selftest`

| Field | Notes |
|---|---|
| `non_member_chat_id` | Optional. A chat id the agent user is not a member of. `selftest` expects Graph to refuse it (403 or 404) |

## Content filters

`secret_patterns` blocks messages containing private key blocks, AWS access key ids, GitHub tokens, JWT or bearer-looking strings, Slack tokens, and `password`, `secret` or `token` assignments with a value. `classification_markers` blocks messages containing any configured marker. A refusal exits 6 and names the filter and pattern id, never the matched text. If `classification_markers` is enabled but the list is empty, the filter does nothing and `selftest` warns.

## Validation

A policy that fails validation never loads and every command exits 9 with the problems listed in the message. Typical causes: missing `version`, `upn` or `audit.path`, a non-GUID id, a relative path, `poll_interval` under 5 seconds, `block_broadcast: false`, an unknown filter name, an unknown key.

## Not configurable in this version

Unlisted-conversation reading, file attachments, group-based commanders, signed policy files and a file-path allow-list for `--file` do not exist. `--file` reads any file the agent user can read, so rely on the destination allow-list and content filters, and run the agent with least privilege.
