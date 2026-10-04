# Troubleshooting

Every failure prints an envelope with `error.code` and `error.hint`, and the process exit code matches. Start with the exit code. Graph behavior referenced here is unverified against a real tenant.

| Exit | `error.code` | Typical causes | What to do |
|---|---|---|---|
| 0 | `ok` | Success. An `inbox` with nothing new returns `[]` | Check `meta.truncated` on large lists |
| 1 | `general` | Unexpected error; a failed `selftest` row; Graph 5xx after retries | Read `error.message`. For `selftest`, read the failing rows. Report if it persists |
| 2 | `usage` | Bad flags, unknown command, a raw id used instead of an alias, bad `--since` | Fix the command; see [Usage](usage.md). Run `teams help` |
| 3 | `auth` | Daemon unreachable; agent user needs re-enrollment or is revoked; credential not configured; token rejected twice | See below. Do not retry in a loop; a human must act |
| 4 | `forbidden` | Graph refused (403): the agent user is not a member, admin consent is missing for channel read scopes, or a Teams policy blocks it | Check membership and consent. This is final until the tenant side changes |
| 5 | `not_found` | Chat, channel or message gone; a `user:` destination has no one-to-one chat and `create_chat` is `false` | Check the ids in the policy; set `create_chat: true` if intended |
| 6 | `policy_denied` | Destination not listed or `send`/`watch` off; mention not allowed; rate limit, per-run write cap or `reply_depth_max` reached; content filter hit; link not allow-listed; UPN does not match the policy | Read `error.message` and the hint. Fix the content or the policy; do not rephrase to evade a filter. Rate limit hints include a retry time |
| 7 | `conflict` | Idempotency key reused with a different message; earlier send with that key is in an unknown state; corrupt send ledger; Graph 409 | See below |
| 8 | `rate_limited` | Graph throttled (429/503) after bounded retries; the credential daemon is degraded or asked for a retry | Wait the hinted time and retry; send less often |
| 9 | `validation` | Policy missing, unreadable, untrusted or invalid; message empty, oversize or contains control characters; unknown id passed to `ack` | See below |

## Exit 3: authentication

- "daemon unreachable": the message names the socket that was tried. Check the daemon is running and that `AGENT_OKTA_D_SOCKET` (default `/var/run/agentd/agentd.sock` on macOS, `/run/agentd/agentd.sock` on Linux, unverified) matches it.
- "not configured" or an access error: the daemon does not serve the `msgraph` credential to this agent. A human must configure or enroll it in the daemon.
- "re-enrollment required" or "revoked", with the remediation `a human must run: agent-okta-d enroll msgraph`: a human must repeat the agent user's sign-in with the daemon. Disabling the Entra user is the intended kill switch and produces this too; how quickly it takes effect is unverified (assumed up to about 60 minutes).
- `teams` holds no fallback credentials and never prints tokens.

## Exit 8: rate limited or daemon degraded

Graph throttling and a degraded credential daemon both exit 8. The message carries a retry hint ("Retry after N seconds." or "Retry later."). Wait that long; do not retry in a tight loop. If it persists for the daemon, ask the operator to check `agent-okta-d` status.

## Exit 6: policy

Run `teams destinations list` to see what is allowed. `teams send ... --dry-run` shows the decision without posting. Audit lines (`audit.path`) record the deciding rule and alias, never message bodies. They also carry the HTTP status of the last Graph call (`http_status`), which tells a 403 from a 5xx. A `send` or `reply` that reaches the post writes an `intent` line first, then its final line.

## Exit 7: conflict

- Different message, same key: use a new key for a new message.
- Unknown earlier state: a previous attempt with this key may have posted. Look in Teams. If it did not post, an operator may remove the entry; if `send.marker_scan` is on, `teams` tries to find the earlier post itself.
- Corrupt ledger: `send` and `reply` stay blocked until the corrupt file (renamed `*.corrupt-<timestamp>` in the state directory) is dealt with and a fresh ledger can be created; expect that duplicates may be possible for keys recorded before.

## Exit 9: validation

- `policy: cannot read ...`: the file is missing or unreadable. Check `TEAMS_POLICY` and the path.
- Untrusted policy: the file or a parent directory is not root-owned, is group or world writable, or is a symlink. Fix ownership and modes ([Install](install.md)).
- `policy invalid: ...`: the message lists each problem; see [Configuration](configuration.md).
- `ack` of an unknown id: pass the ids exactly as `inbox` returned them, and ack within the lookback window.

## Other symptoms

| Symptom | Likely reason |
|---|---|
| `inbox` never shows a direct message from a new person | Only listed `watch: true` destinations are read; add a `user:` destination |
| Messages in a channel thread are missing | Replies are checked only for threads the agent started, up to `inbound.thread_poll_max` |
| Same message returned repeatedly | It has not been acked, or the state directory was lost ([Inbox, ack and state](inbox-and-state.md)) |
| Message text looks garbled | HTML is converted to text; images are ignored |
| Latency of up to 15 seconds | Polling; lower `poll_interval` (minimum 5s) at the cost of more Graph calls |
| Channel read gives 403 | Admin consent for channel read scopes is likely missing |
