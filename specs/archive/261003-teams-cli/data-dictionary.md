# Data Dictionary: teams-cli
**Date:** 2026-10-04 | **Status:** Implementation-ready | Go types live in `internal/domain` unless noted. Created in Phase F (types only), logic in WS-A.

## Entities

| Type | Fields | Notes |
|---|---|---|
| `Profile` | `ID, DisplayName, UPN string` | From `GET /me`. `ID` is the agent's AAD object id (own-message filter). |
| `Destination` | `Alias Alias; Kind Kind; TeamID, ChannelID, ChatID, AADID, DisplayName string; Send, Watch, CreateChat bool` | One per policy destination. Kind decides which id fields are required. |
| `Policy` | `Version int; Profile, UPN, TenantID, StateDir string; Destinations map[Alias]Destination; Instruct Instruct; Inbound Inbound; Send SendPolicy; Limits Limits; Audit AuditCfg; Selftest SelftestCfg` | Immutable after load. Defaults filled by the loader. |
| `RawMessage` | `ID, ThreadID, MessageType string; Created, Modified time.Time; Deleted bool; FromUserID, FromTenantID, FromName string; FromKind SenderKind; BodyType, BodyContent string; Mentions []RawMention; Alias Alias; ChatID, TeamID, ChannelID string` | Adapter output; no policy applied. |
| `InboundItem` | `ID, ThreadID string` (alias-qualified, D8)`; Received time.Time; Cursor string; Edited bool; Conversation Conversation; Sender Sender; MentionedYou bool; Text string; Links []string` | Use-case output before presentation wraps free text. |
| `OutMessage` | `Text string; HTML bool; Mentions []OutMention; MarkerKey string` | What the adapter posts. `HTML` true only with mentions or marker. |
| `PostResult` | `MessageID, ThreadID string; Created time.Time` | |
| `LedgerEntry` | `Key, PayloadHash, Alias, ThreadID, MessageID string; State EntryState; Created, Updated time.Time` | Keyed by idempotency key. |
| `Sent` | `At time.Time; Alias Alias; ThreadID, MessageID, Key string` | Sent history used for rate and loop guard. Includes keyless sends. |
| `CursorState` | `Watermark time.Time; Acked []AckEntry; Delivered []DeliveryEntry; DeltaToken string; ChatID string; Updated time.Time` | Per destination alias. `DeltaToken` is an opaque Graph delta link (channels). |
| `DeliveryEntry` | `ID, ThreadID string; Modified, DeliveredAt time.Time` | Delivery index (D8): written by `inbox`, checked by `ack`. `ID` is the Graph message id (the CLI id is `<alias>/<ID>`). |
| `AckEntry` | `ID string; Modified time.Time` | Message id plus the `lastModified` version acked. |
| `AuditEvent` | `Verb, Resource, Outcome string; HTTPStatus int; Duration time.Duration; Decision string; Extra map[string]string` | Folded into core `audit.Record` (`policy_decision` carries `;k=v`). |

## Supporting types (referenced by signatures)

| Type | Fields |
|---|---|
| `RawSender` | `UserID, TenantID, Name string; Kind SenderKind` |
| `RawMention` | `UserID, Text string` |
| `OutMention` | `ID int; AADID, DisplayName string` |
| `NormalizeOutcome` | `Kind (ok, system, deleted, own, incomplete)` plus a drop-reason string for counting |
| `Instruct` | `Commanders, Agents []AADID` |
| `Inbound` | `Handle []InboundHandle; MaxLookback, PollInterval, MaxWait time.Duration; ThreadPollMax int` |
| `SendPolicy` | `MaxBytes int; Mentions MentionPolicy{Allow []Alias; BlockBroadcast bool; Max int}; ContentFilters []string; ClassificationMarkers, LinkAllowlist []string; Rate Rate; ReplyDepthMax int; ReplyWindow time.Duration; Prefix string; MarkerScan bool` |
| `Rate` | `PerMinute, PerHour int` |
| `Limits` | `MaxResults, MaxWritesPerRun, MaxChatScan int` |
| `AuditCfg` | `Path string` |
| `SelftestCfg` | `NonMemberChatID string` |

## Value objects

| Type | Definition |
|---|---|
| `Alias` | string `kind:name`, kind in {channel, chat, user}, name `[a-z0-9._-]{1,64}`. `ParseAlias` rejects everything else (raw ids included). |
| `AADID` | GUID string, lowercase-normalized for comparison. |
| `Sender` | `Name output.Untrusted-wrapped at presentation; AADID string; CanInstruct, IsAgent bool` |
| `Conversation` | `Type ConvType; Alias Alias` |
| `Reservation` | `Outcome ReserveOutcome; Entry LedgerEntry` (`New`, `Replay` with recorded result, `Retry` after failed) |
| `Decision` | `Allowed bool; Category output.Category; Reason string; RuleID string; RetryAfter time.Duration` (domain evaluation result; `Err()` returns `domain.Error`) |
| `Finding` | `Filter, PatternID string` (never matched text) |

## Enumerations

| Enum | Values |
|---|---|
| `Kind` / `ConvType` | `channel`, `chat`, `user` |
| `InboundHandle` | `direct`, `mentions`, `watched` |
| `SenderKind` | `user`, `application`, `bot`, `unknown` |
| `EntryState` | `pending`, `sent`, `failed` |
| `ReserveOutcome` | `new`, `replay`, `retry` |
| Content filters | `secret_patterns`, `classification_markers` |
| `domain.Error` categories | the core `output.Category` set (`usage`, `auth`, `forbidden`, `not_found`, `policy_denied`, `conflict`, `rate_limited`, `validation`, `general`) |

## Policy file (YAML) fields

See spec s6 for the schema; defaults: `inbound.handle` = all three, `max_lookback` 30m (cap 24h), `poll_interval` 15s (min 5s), `max_wait` 120s, `send.max_bytes` 8000, `mentions.max` 5, `rate` 10/min 100/h, `reply_depth_max` 6, `reply_window` 24h, `limits.max_results` 50, `max_writes_per_run` 30, `max_chat_scan` 50, `marker_scan` false, `state_dir` `/var/lib/agent-cli/teams`, `thread_poll_max` 5. Required: `version`, `profile`, `upn`, `destinations`, `audit.path`. Optional `selftest.non_member_chat_id`.

## State file formats (version 1)

`teams.ledger.json`: `{"version":1,"entries":{"<key>":{LedgerEntry}},"sent":[{Sent}]"threads":["<thread_id>"]}`. Retention: `sent`/`failed` and `sent[]` 90 days; `pending` never expires.
`teams.cursors.json`: `{"version":1,"destinations":{"<alias>":{CursorState}}}` (includes the delivery index `delivered[]`; entries pruned after `max_lookback` + 1 h). Acked entries older than `max_lookback` plus one hour are pruned on write (watermark already past them).

## Inbox item JSON (CLI contract, extends PRD s6)

```json
{ "id": "channel:sdlc-alerts/1696341900000", "thread_id": "channel:sdlc-alerts/1696341900000", "received": "2026-10-03T14:05:00Z",
  "cursor": "c1:2026-10-03T14:05:00Z", "edited": false,
  "conversation": { "type": "channel", "alias": "channel:sdlc-alerts" },
  "sender": { "name": { "untrusted": true, "value": "Jane Doe" }, "aad_id": "...", "can_instruct": true, "is_agent": false },
  "mentioned_you": true,
  "text": { "untrusted": true, "value": "...", "author": "Jane Doe", "timestamp": "2026-10-03T14:05:00Z" },
  "links": ["https://example.com/x"] }
```

`cursor` format: `c1:` + RFC 3339 UTC modified time (opaque to callers). `id` and `thread_id` are alias-qualified CLI ids (D8): `<alias>/<graph-id>`; for a channel the thread root id follows the slash, for a chat `thread_id` is `<alias>/chat` (chats have no threads). `ack`, `reply` and `thread get` parse the alias from the id and re-check policy.

## Interfaces

Ports are listed in `architecture.md` s4 (`Graph`, `Ledger`, `CursorStore`, `PolicyProvider`, `AuditSink`, `Clock`, `Rand`, `RunInfo`). Core interfaces used: `auth.DaemonClient`, `auth.TokenSource`, `httpx.TokenRefresher`, `selftest.Probe`.

## Graph request/response mapping (all unverified against a real tenant; UA-1)

| Operation | Method and path | Notes |
|---|---|---|
| Me | `GET /me?$select=id,displayName,userPrincipalName` | |
| Post chat | `POST /chats/{id}/messages` body `{"body":{"contentType":"text\|html","content":...},"mentions":[...]}` | |
| Post channel | `POST /teams/{t}/channels/{c}/messages` | |
| Post reply | `POST /teams/{t}/channels/{c}/messages/{id}/replies` | |
| List chat msgs | `GET /chats/{id}/messages?$top=N&$orderby=lastModifiedDateTime desc&$filter=lastModifiedDateTime gt T` | |
| List channel msgs | `GET /teams/{t}/channels/{c}/messages/delta` (fallback `.../messages?$top=N`) | |
| List replies | `GET .../messages/{id}/replies?$top=N` | |
| Find user chat | `GET /me/chats?$filter=chatType eq 'oneOnOne'&$expand=members&$top=50` | |
| Create chat | `POST /chats` `{"chatType":"oneOnOne","members":[...]}` | only with `create_chat: true` |
| Probe chat | `GET /chats/{id}` | selftest negative row |
