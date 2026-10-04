# Tasks: teams-cli
**Date:** 2026-10-04 | **Status:** Ready to implement | 0/64 tasks complete

Every task is TDD: write the failing test named in "Test first", make it pass, refactor, run `gofmt -l .`, `go vet ./...`, `golangci-lint run`, `go test -race ./...` on the owned packages. Milestone tags M1/M2/M3 = spec/PRD milestone. FR/AC references point to spec.md.

## Ownership rules (disjoint)
A workstream edits only files it owns. Cross-workstream changes go through the owner or, for frozen interfaces, a lead-approved note in `implementation-notes.md`. Phase F creates compile-only stubs in WS-A and WS-B files and the `usecasetest/` skeleton; ownership passes to the workstream when Phase F is merged.

| Workstream | Owns (exclusive) | Depends on (read-only) |
|---|---|---|
| **F Foundation** (serial, lead) | `go.mod`, `go.sum`, `Makefile`, `AGENTS.md`, `internal/domain/{types.go,errors.go}`, `internal/usecase/ports.go`, `internal/archtest/`, dir skeleton; creates the `internal/usecase/usecasetest/` skeleton, then hands it to WS-B on Phase F merge | core v0.1.0 |
| **WS-A Domain** | `internal/domain/{alias,policy,policy_eval,classify,mention,filters,htmltext,cursor,ratelimit,loopguard,validate}.go` + `_test.go` | F |
| **WS-B Use cases** | `internal/usecase/{service,whoami,destinations,send,reply,inbox,ack,thread,selftest}.go` + tests, `internal/usecase/usecasetest/` (after F) | F, WS-A signatures |
| **WS-C Graph adapter** | `internal/adapters/graph/**` (incl. `graphtest/`) | F |
| **WS-D State, policy file, audit** | `internal/adapters/{state,policyfile,auditlog}/**`, `internal/infra/**` | F |
| **WS-E CLI and composition** | `internal/adapters/{cli,selftestcfg}/**`, `cmd/teams/**` | F; WS-A..D at integration |
| **WS-F Docs and CI** | `docs/**`, `user-docs/**`, `.github/**`, `README.md`, `user-docs/teams.policy.sample.yaml` | spec |

## Domain API (frozen by Phase F; WS-A implements, WS-B/WS-E call)

```go
// alias.go
func ParseAlias(s string) (Alias, error)                      // usage error on raw ids / bad grammar
func (a Alias) Kind() Kind; func (a Alias) Name() string
// policy_eval.go
func (p Policy) Destination(a Alias) (Destination, bool)
func (p Policy) CanSend(a Alias) Decision                      // deny: unlisted, send:false
func (p Policy) CanWatch(a Alias) Decision
func (p Policy) EvalUPN(me Profile) Decision                    // D14
func (p Policy) Watched() []Destination                        // sorted by alias, watch:true
// classify.go
func Classify(p Policy, s RawSender) Sender                    // FR-19
func SelectHandle(p Policy, d Destination, m RawMessage, agentID string) (InboundHandle, bool)  // D4
func Normalize(p Policy, d Destination, m RawMessage, agentID string) (InboundItem, NormalizeOutcome) // drops: system, deleted, own, null fields
// mention.go
func BuildMentions(p Policy, aliases []Alias, text string) (OutMessage, error)   // D5, FR-8
// filters.go
func ScanSecrets(text string) []Finding; func ScanMarkers(markers []string, text string) []Finding
func CheckLinks(allow []string, text string) []Finding
// htmltext.go
func HTMLToText(html string) (text string, links []string)     // UA-10
// cursor.go
func (c CursorState) Undelivered(items []InboundItem, now time.Time, lookback time.Duration) []InboundItem  // D8
func (c CursorState) Ack(entries []AckEntry) (next CursorState, acked, already int)
func (c CursorState) Record(d []DeliveryEntry, now time.Time, keep time.Duration) CursorState   // delivery index
func (c CursorState) Known(id string) (DeliveryEntry, bool)
func ParseItemID(id string) (alias Alias, graphID string, err error)   // D8
func ItemID(a Alias, graphID string) string; func ThreadID(a Alias, root string) string
func Since(c CursorState, now time.Time, lookback time.Duration, override *time.Time) time.Time
func EncodeCursor(t time.Time) string; func DecodeCursor(s string) (time.Time, error)
// ratelimit.go
func CheckRate(r Rate, sent []Sent, now time.Time, runWrites, maxRun int) Decision                 // FR-10
// loopguard.go
func CheckLoop(depthMax int, sentInThread int) Decision                                          // FR-11
// validate.go
func ValidateText(p Policy, text string) error; func ValidateKey(k string) error; func PayloadHash(alias Alias, thread string, o OutMessage) string
```

## Phase F: Foundation (serial)
- [ ] **F1** go.mod: `require github.com/stainedhead/agent-cli-core v0.1.0` and `github.com/goccy/go-yaml v1.19.2`; no replace; AGENTS.md rule "do not add a require yet" replaced by "core v0.1.0 required; bumps are ordinary PRs"; Makefile targets `build race cover cross skill`. Test first: `go mod tidy` leaves no diff; `go list -m all` shows no `agent-okta-d`. (FR-32, D11)
- [ ] **F2** `internal/domain/types.go`, `errors.go`: all data-dictionary entities/enums; `domain.Error{Category,Message,Hint}` (+ `NewUsage/Validation/PolicyDenied/Conflict/NotFound/NotSent`, `IsNotSent`). Test first: each constructor maps to the expected `output.ExitOf` code per spec D9. (FR-29)
- [ ] **F3** Domain function stubs (signatures above) returning zero values; compile only.
- [ ] **F4** `internal/usecase/ports.go`: ports of architecture s4, `Commands` interface, DTOs. Test first: compile-time assertions that `usecasetest` fakes satisfy every port.
- [ ] **F5** `internal/usecase/usecasetest/`: in-memory fakes skeletons (Graph with scripted responses, Ledger, CursorStore, PolicyProvider, AuditSink recorder, fake clock/rand).
- [ ] **F6** `internal/archtest`: import-graph, allowed third-party modules (core, go-yaml), no `replace`, no `agent-okta-d`, no core `auth|httpx|audit|selftest|docgen` import outside `cmd/teams` and `adapters/*`. Test first: a deliberately bad fixture fails the check.
- [ ] **F7** Directory skeleton with `doc.go` per package; CI passes on empty implementation.

## WS-A: Domain (internal/domain)
- [ ] **A1** (M1) Alias parsing: table of >= 15 valid/invalid inputs incl. `19:abc@thread.v2`, GUID, uppercase, empty, 65-char names. (FR-3, AC-3)
- [ ] **A2** (M1) Policy evaluation: unlisted -> denied (6), `send:false`, `watch:false`, UPN case-insensitive match/mismatch, `Watched()` ordering. (FR-1, FR-5, D14, AC-1)
- [ ] **A3** (M1) `Normalize`: drops non-`message` types, deleted, own messages, null `from`/`body`/`modified`; assigns thread id (channel root id; chat synthetic); HTML via A9. Counts per drop reason. (FR-17, FR-18, AC-12)
- [ ] **A4** (M2) `Classify`: commanders, agents, id in both, empty id, non-user sender kinds, cross-tenant with `tenant_id` set, display-name spoof (name equals commander name -> still false). Table + fuzz on names. (FR-19, AC-13)
- [ ] **A5** (M1) `SelectHandle`: matrix of destination kind x `handle` set x mentioned/not -> surfaced/dropped (D4). (FR-16)
- [ ] **A6** (M1) Cursor algebra and item ids (`ItemID`/`ParseItemID` round trip, malformed ids, alias with slash-like input): `Undelivered` (acked, edited-after-ack with `edited:true`, lookback age-out), `Ack` (contiguous watermark advance, idempotent, unknown handled by caller), `Since` with override, cursor encode/decode round trip and malformed input. Property test: any ack order ends at the same state. `Record`/`Known`/pruning. (D8, AC-14)
- [ ] **A7** (M2) Mentions: policy-only build, escaping (`<`, `&`, quotes, `<at>` injection attempts in text and display_name), max count, duplicates, broadcast/unlisted/non-`user:` rejection. (D5, FR-8, AC-6)
- [ ] **A8** (M2) Filters: secret-pattern corpus (>= 12 positives, >= 12 negatives incl. prose like "token budget"), classification markers (case-insens.), link allowlist (exact, `*.suffix`, `<a href>`, userinfo trick `https://ok.com@evil.com`); findings never contain matched text. (FR-9, FR-12, AC-7)
- [ ] **A9** (M1) `HTMLToText`: `<at>`, `<p>`, `<br>`, lists, `<a href>` -> links, `<img>` ignored, entities, script/style stripped, nested/unbalanced tags, 1 MiB input bound. (UA-10)
- [ ] **A10** (M2) `CheckRate` (minute/hour sliding windows, boundary instants, pending counted, per-run cap, retry-after) and `CheckLoop`. (FR-10, FR-11, AC-9, AC-10)
- [ ] **A11** (M1) `ValidateText` (empty, whitespace, NUL/CR control chars, max bytes on UTF-8 byte length, prefix counted), `ValidateKey`, `PayloadHash` stability and sensitivity. (FR-5, FR-13, FR-14)

## WS-B: Use cases (internal/usecase)
- [ ] **B1** (M1) `service.go`: `begin()` loads policy, UPN guard once per run (`Graph.Me` cached), run counters, audit wrapper that records every command incl. failures. Test: mismatch -> 6 and no further Graph calls; audit line per command. (FR-1, FR-28, D14, AC-1, AC-21)
- [ ] **B2** (M1) `whoami`, `destinations` (no network; ids never in output). (FR-1, FR-2, AC-2)
- [ ] **B3** (M1) `send` happy path chat/channel: ordering of checks per FR-5; first failure wins (table of 12 inputs each failing exactly one check). (AC-4)
- [ ] **B4** (M1) `--dry-run`: zero writes/ledger entries/rate consumption; same decision as real. (FR-6, AC-5)
- [ ] **B5** (M2) Mentions, filters, link allowlist, rate/write caps, loop guard wired into send with correct categories and no matched text in errors. (AC-6, AC-7, AC-9, AC-10)
- [ ] **B6** (M2) Ledger flow: new/replay/changed-payload (7)/pending (7)/NotSent retry/ambiguous failure stays pending/corrupt ledger blocks/marker scan on and off. Concurrency test: two goroutines same key -> one post. (FR-14, FR-15, AC-11)
- [ ] **B7** (M3) `reply`: alias parsed from `thread_id` (D8), policy check, malformed id (9), no prior inbox needed, channel vs chat paths, same checks. (FR-7, AC-8)
- [ ] **B8** (M1) `inbox`: per-destination fetch, user chat resolution (D6: found, create_chat false -> skipped with reason, create true, 404 re-resolve once), normalization + classification + handle selection + undelivered, merge/sort/limit, `RecordDeliveries` after selection, no watermark/ack change. (FR-16, FR-17, AC-12)
- [ ] **B9** (M1) `inbox --wait`: fake clock; returns within `poll_interval + 1 s`; floor 5 s; `Retry-After` widening; ctx cancel; empty -> `[]`. Call-count assertions (NFR-2). (AC-12, AC-20)
- [ ] **B10** (M1) `ack`: split ids by alias, policy check, `CursorStore.Ack`; id never delivered or pruned -> 9 and nothing changes; idempotent; 1-100 ids; end-to-end inbox -> ack -> inbox on fakes. (FR-20, AC-14)
- [ ] **B11** (M3) `thread get`: alias from `thread_id`, bounded, oldest-first, classified, no cursor change, `watch:false` denied. (FR-23, AC-16)
- [ ] **B12** (M2) Selftest rows (spec s8) as use case returning `[]selftest.Row` + probe logic over ports; broken-policy cases fail the right row. (FR-24, AC-17)
- [ ] **B13** (M2) Injection corpus (>= 20) through `inbox`: all text untrusted-flagged data, non-commanders never `can_instruct`. (AC-13)
- [ ] **B14** (M2) Token hygiene: planted token never in DTOs/errors. (AC-24)

## WS-C: Graph adapter (internal/adapters/graph)
- [ ] **C1** (M1) `graphtest` fake Graph server (httptest): routes of the data-dictionary table, seeding, failure/throttle knobs, request recorder, 401-then-200 scenario; checks `Authorization` presence only. Published first (others depend on it).
- [ ] **C2** (M1) `client.go`: build on core `httpx.NewClient` (AllowedHosts = base host, VendorCode from headers), `auth.Authorizer` injection, reads marked safe to retry, writes never; bounded JSON read (16 MiB); error mapping 404->NotFound, 409->Conflict, others pass through typed. Tests: 401->refresh->success, 401x2 -> exit 3, 403 -> 4, 429 w/ Retry-After on read retries and on write does not. (FR-26, FR-27, AC-19, AC-20)
- [ ] **C3** (M1) `Me`; `TestAssumedMe`. (UA-2)
- [ ] **C4** (M1) Chat messages list (`since`, `$top`, order, paging to same host max 5 pages) and `RawMessage` DTO mapping (null-safe). `TestAssumedListChatMessages`. (UA-1, UA-7)
- [ ] **C5** (M1) Channel messages: delta first, fallback list, delta link returned; replies list. `TestAssumedChannelDelta`, `...Fallback`. (UA-3)
- [ ] **C6** (M1) `PostChat`, `PostChannel` (+reply), text vs html + mentions JSON; NotSent classification (proved-unprocessed vs ambiguous). `TestAssumedPost*`. (FR-4, FR-8)
- [ ] **C7** (M1) `ResolveUserChat` (list+match, create opt-in, bounded scan) and `GetChat` probe. (D6, UA-6, UA-8)
- [ ] **C8** (M2) `FindByMarker` (last 20 messages, marker substring; 400 inconclusive). (FR-15, UA-9)
- [ ] **C9** (M1) Hygiene: URLs never contain ids in errors beyond path templates; no body echoed; no token anywhere (planted-token test). (FR-31)

## WS-D: State, policy file, audit (internal/adapters/{state,policyfile,auditlog}, internal/infra)
- [ ] **ST1** (M1) `infra/clock` (System, Fake), `infra/config` (env resolution/defaults), `infra/fstrust` (ownership/mode/O_NOFOLLOW, injectable stat; ancestors walk). Tests: group/world-writable, agent-owned, symlink, root-owned pass. (FR-21)
- [ ] **ST2** (M1) `policyfile`: strict YAML (go-yaml, unknown/duplicate keys rejected), defaults, validation per FR-22, `block_broadcast:false` rejected, caps (`max_lookback` <= 24h), build-tagged dev override. Table of >= 25 invalid files; golden valid file = a testdata copy of `user-docs/teams.policy.sample.yaml` (Phase I I2 checks the two are byte-equal). (AC-15)
- [ ] **ST3** (M1) `state/atomic+lock`: temp+fsync+rename, advisory flock with timeout (-> exit 7), corrupt file quarantined; multi-process test using `os/exec` re-entry. (D7, NFR-7)
- [ ] **ST4** (M1) `state/cursors`: Get/RecordDeliveries/Ack/StoreDelta/chat cache per D8 using domain `CursorState` and the delivery index; pruning; fail-open on corruption; persistence across processes. (AC-14)
- [ ] **ST5** (M2) `state/ledger`: Reserve/Complete/Fail/SentSince/SentInThread/RecordSent/PutThread+ActiveThreads, retention, fail-closed on corruption (exit 7), crash-between-reserve-and-complete leaves pending. (FR-14, AC-11)
- [ ] **ST6** (M2) `auditlog`: core `audit.Open` (0600/0700), `policy_decision` suffix encoding, Block mode for writes, secrets option; property/fuzz test: no token/body/raw id. (FR-28, AC-21)

## WS-E: CLI and composition (internal/adapters/{cli,selftestcfg}, cmd/teams)
- [ ] **E1** (M1) `cli` router on stdlib `flag`: global flags (`--format --max-bytes --offset`), per-command flags and validation (exactly one of `--text/--file`, `--file -` TTY check, id counts), usage errors exit 2, help text from the command table. (FR-29)
- [ ] **E2** (M1) Presenters: inbox items with `output.Untrusted` for text and `sender.name`; arrays as `data`; table/text rendering; golden files. (FR-17, D13, AC-22)
- [ ] **E3** (M1) `version` command and build stamp; works with no policy/network. (FR-25, AC-18)
- [ ] **E4** (M1) `cmd/teams/daemon.go` stub, `app.go` assemble() per architecture s6, `main.go`. Tests: stub -> exit 3 with socket name for every network command; `version`/`destinations` need no daemon. ADR text handed to WS-F. (FR-26, D10, AC-19)
- [ ] **E5** (M2) `selftestcfg` probes + `selftest` command incl. `--read-only`. (FR-24, AC-17)
- [ ] **E6** (M3) `skill` command via `docgen.Generate` from the same table; golden test; `make skill`. (FR-30, AC-23)
- [ ] **E7** (M3) End-to-end integration tests in `cmd/teams/integration_test.go` (Phase I): `authtest` scenarios x commands, `graphtest`, fake clock, planted token, audit lines, state files across runs (ack persistence, ledger replay). (AC-1..AC-24)

## WS-F: Docs and CI (docs/, user-docs/, .github/, README)
- [ ] **W1** (M1) `.github/workflows/ci.yml` to BLD-1..6 and DEP-1..6: tidy-diff, gofmt, vet, pinned golangci-lint, `-race`, govulncheck, cross-compile 3 targets, `permissions: contents: read, packages: read`, job-token git config, remove the "skip while no .go files" guards. No release workflow. (FR-32, AC-25)
- [ ] **W2** (M1) `docs/adr-daemon-client-stub.md` and ADR-1..9 in `docs/architectural-decision-record.md` (from architecture s12). (D10)
- [ ] **W3** (M1) `docs/deferred.md`, `docs/unverified-assumptions.md` (UA-1..22 with code/test refs filled in during Phase I), `docs/m0-spike-checklist.md` (S-1 shapes, S-2 enrollment, S-3 non-member 403/404, S-4 CA/refresh, S-5 send/mentions/markers, S-6 polling latency + throttling + delta, S-7 1:1 chat create, S-8 kill-switch drill, S-9 selftest dry run, S-10 Agent User spike plan from PRD s11), `docs/requested-core-changes.md` (typed policy rules, standalone trust check, audit extension fields, body-reading VendorCode hook, `Meta.next_page_token`, importable clock; copy workarounds from the outlook list). 
- [ ] **W4** (M3) `docs/product-summary.md`, `product-details.md`, `technical-details.md` updated to the built code (Phase I reconciliation). 
- [ ] **W5** (M3) `user-docs/`: install, getting started (enrollment is a human step), policy reference + `teams.policy.sample.yaml` (placeholders only), usage per command, troubleshooting by exit code, state-dir volume guidance (D7), at-least-once consumption guide. No links into `specs/`. (FR-33, AC-26)
- [ ] **W6** (M3) README status/links; root-skill update notes (manual PR text: generated skill, version, banner removal rule SKILL-5). 

## Phase I: Integration (lead)
- [ ] **I1** Merge workstreams in train order M1, M2, M3; resolve interface changes via implementation-notes.
- [ ] **I2** Run E7 and all gates; fill UA code/test references; coverage report vs targets.
- [ ] **I3** Local overhead benchmark; `make cross`; confirm no `replace`, no `agent-okta-d`, no token strings.
- [ ] **I4** Update `status.md`, `implementation-notes.md`; hand off to dev-flow step 4+.

## Task counts
F 7, WS-A 11, WS-B 14, WS-C 9, WS-D 6, WS-E 7, WS-F 6, Phase I 4 = 64 items (task header count to be recomputed as items complete).
