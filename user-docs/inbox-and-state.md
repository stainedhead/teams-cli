# Inbox, ack and state

## At-least-once delivery

`teams inbox` never decides for you that a message was handled. It records what it handed out, and returns the same messages again on every call until you acknowledge them with `teams ack`. This means no instruction is lost if the agent crashes between reading and acting, and it means your agent must tolerate seeing a message more than once.

Recommended loop:

1. `teams inbox --wait 30 --limit 10`
2. For each item: check `sender.can_instruct`, do the work, and make the work idempotent using `id` as the key.
3. `teams ack <id>...` once the item is fully handled.
4. Repeat.

Rules:

- Ack only after the work is done. Acking early turns at-least-once into at-most-once.
- An edited message that was already acked is delivered again with `edited: true`; `can_instruct` is recomputed from the sender each time.
- Messages older than `inbound.max_lookback` (default 30 minutes, at most 24 hours) that were never acked are dropped from delivery. Ack or process within that window.
- Ordering is by receive time within one destination. There is no global ordering across destinations.
- `ack` advances the read position only across a contiguous run of acked items, so acking out of order is safe but leaves earlier unacked items pending.
- `--since CURSOR` replays without changing acknowledgements or the read position.

## State directory

`teams` keeps two small JSON stores in the state directory (`teams.ledger.json` and `teams.cursors.json`, with a `teams.lock` file used for locking):

| Store | Holds | If lost or corrupt |
|---|---|---|
| Send ledger | Idempotency keys and send history used for rate limits and the loop guard | Retries with the same key can post a duplicate; rate and reply-depth counters restart at zero. A corrupt ledger blocks `send` and `reply` with exit 7 until an operator removes it |
| Cursors and delivery index | Read position, acked ids, delivered ids, resolved one-to-one chat ids | Up to `max_lookback` of messages are re-read, and already-acked messages are delivered once more |

A corrupt file is renamed to `<name>.corrupt-<timestamp>` and treated as lost. Files are written atomically under an advisory lock, so concurrent `teams` processes are safe.

## Volume guidance

- Default location: `/var/lib/agent-cli/teams`. Set `state_dir` in the policy; `TEAMS_STATE_DIR` is honored only when the policy does not set it.
- Use persistent storage. In containers, mount a volume; do not use the container's writable layer, `tmpfs` or any directory cleared on restart. Losing the state directory is the main way to get duplicate sends and re-delivered messages.
- The directory is created with mode 0700 and must be writable by the agent user only.
- One state directory per agent identity. Do not share it between different agent users or policies.
- Back it up if duplicate sends would be costly. The data is small.
- If you must reset it deliberately, stop the agent first, move the directory aside, and expect one bounded re-delivery of recent messages.
- Keep the audit log (`audit.path`) on durable storage too. Writes are blocked if the audit log cannot be written; reads only warn.
