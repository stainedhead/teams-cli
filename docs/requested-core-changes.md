# Requested changes to agent-cli-core

Gaps found while building `teams` against `agent-cli-core v0.1.0`. The core is never edited from this repository; each item records the workaround in use. Items 1 to 9 follow the sibling `outlook` CLI's list (`outlook` repository, `docs/requested-core-changes.md`) where the same gap applies; the numbering is local to this file.

| # | Area | Gap in v0.1.0 | Workaround here | Priority |
|---|---|---|---|---|
| 1 | `policy` | The engine is a generic verb/resource/field rule set with its own schema. It has no typed destination, sender, mention or content rules, and rate limits are in memory per process (every CLI run is a new process). | Typed policy modelled in `internal/domain` (ADR-2); persistent send history in the idempotency ledger. | Medium |
| 2 | `policy` | The agent-writable-file check exists only inside `policy.Load`; there is no standalone trusted-file function, and its `access(2)` test checks the current user, not authorship. | The policy-file adapter proves authorship by ownership: file and ancestors owned by root or a configured trusted uid, not group or world writable, symlink owners checked, opened with `O_NOFOLLOW` and verified with `fstat`. | Medium |
| 3 | `audit` | `audit.Record` has no extension fields, so the deciding rule id, destination alias, message id and dropped counts cannot be their own columns. | Fold them into `policy_decision` as `;key=value` suffixes (for example `deny:<rule-id>`, `allow;alias=...;message_id=...`). A core `Record.Extra map[string]string` would let the sink emit real columns. | Medium |
| 4 | `httpx` | `Config.VendorCode` receives only response headers, but Graph puts its error code in the JSON body. No hook reads a bounded response body for the vendor code, and the typed 401, 403 and 429 errors carry no Graph code. | Report the first present of `x-ms-error-code`, `request-id`, `client-request-id` (the last two are correlation ids, not codes); otherwise a generic 403. Request: a `Config.VendorCodeFromBody` hook or an error type exposing the status and a bounded body prefix. Unverified (UA-11). | Medium |
| 5 | `output` | `Meta` has `next_offset` (item index) but no continuation token. Graph paging and the channel delta link use opaque tokens. | Return `next_page_token` inside `data`. A `Meta.next_page_token string` would remove this. | Medium |
| 6 | `output` | `Untrusted` marshals as `{"untrusted":true,"value":...,"author":...,"timestamp":...}`, which differs from the PRD sketch, and bounding cannot cut an object (`ErrBoundTooSmall`). | Use `output.Untrusted` for message text and `sender.name` (spec D13); the shape is noted in the generated skill. List commands emit `data` as an array so bounding can cut it. | Low |
| 7 | clock | `internal/clock` is not importable; `policy`, `audit` and `httpx` each declare a small clock interface, and `audit.WithClock` takes the internal type. | `teams` declares its own `Clock` in `usecase` and adapts it where needed; the audit sink stamps `Record.Timestamp` from it. | Low |
| 8 | `auth` | No real daemon adapter (known; waits for `agent-okta-d` `pkg/client`). | Resolved in core v0.2.1 (`auth/oktad`); wired in `cmd/teams`, see `adr-daemon-adapter-wired.md`. | Resolved |
| 9 | `docgen` | `CommandTree.Commands` is flat. | Nested commands are named with a space (`thread get`). | Low |

## To confirm during implementation

These come from reading the PRD against the core's documented behaviour, not from running code. Confirm each and move it to the table above, or delete it.

| # | Area | Possible gap | Working assumption | Priority |
|---|---|---|---|---|
| 10 | `httpx` | PRD AUTH-2 asks for exactly one forced refresh and retry on 401. Confirm the core retries once only and never retries a non-idempotent POST. | Wrap writes so they are never retried by the core; send retries rely on the idempotency ledger. Test with an `httptest` server. | Medium |
| 11 | `selftest` | Negative probes (non-member chat returns 403 or 404) need a way to expect a denial and treat it as a pass. | Probe code maps 403 and 404 to pass inside the adapter. | Low |
| 12 | `audit` | PRD says no message bodies by default; confirm the record type has no free-form field a caller could fill with content. | Adapter passes metadata only. | Low |
