# Architecture: teams-cli
**Date:** 2026-10-04 | **Status:** Implementation-ready | Spec: `spec.md` (FR/D references)

## 1. Overview

```
 agent host
 ┌─────────────────────────────────────────────────────────────┐
 │ teams (cmd/teams: composition root only)                      │
 │  adapters/cli ──> usecase.Commands ──> domain (policy, classify)│
 │                      │ ports                                   │
 │   ┌──────────┬───────┴─────┬───────────┬───────────┐          │
 │   graph    state        policyfile   auditlog   selftestcfg   │
 │   │ (httpx+auth)  (ledger,cursors)                             │
 └───┼─────────────────────────────────────────────────────────┘
     │ Bearer (set by core auth.Authorizer; adapter never sees it)
     ▼                                   agent-okta-d (stub today: exit 3)
  graph.microsoft.com/v1.0
```

Nothing listens; the only network egress is HTTPS to `graph.microsoft.com` (core `AllowedHosts`).

## 2. Layers and dependency rule

Dependencies point inward only; enforced by `internal/archtest` (task F4).

| Layer | Path | May import | Responsibility |
|---|---|---|---|
| Entry | `cmd/teams` | everything under `internal/`, core | `main`, `assemble()` composition root, `newDaemonClient()`, build stamp. No logic beyond wiring. |
| Adapters | `internal/adapters/{cli,graph,state,policyfile,auditlog,selftestcfg}` | `usecase`, `domain`, `internal/infra/*`, core packages | I/O: argv/stdout, Graph HTTP, files, audit. Adapters never import each other (cli imports only `usecase`/`domain`; wiring happens in `cmd/teams`). |
| Infrastructure | `internal/infra/{clock,fstrust,config}` | stdlib only | System clock/sleeper, filesystem trust check, env/default resolution. |
| Use cases | `internal/usecase` | `domain`, core `output` (categories only) | Orchestration, port interfaces (`ports.go`), `Commands` facade. |
| Domain | `internal/domain` | stdlib, core `output` (only `output.Category` for the error taxonomy) | Entities, value objects, policy model and pure evaluation, classification, filters, cursor/ack algebra, HTML-to-text, mention building, error taxonomy. |

Rules enforced by archtest: domain imports no `usecase`/`adapter`/`infra`/`cmd`; usecase imports no adapters/infra/cmd; no package outside `cmd/teams` and `adapters/*` imports core `auth`, `httpx`, `audit`, `selftest`, `docgen`; nothing imports `agent-okta-d`; go.mod third-party modules are exactly `agent-cli-core` and `goccy/go-yaml`; no `replace` directive.

## 3. Package layout

```
cmd/teams/
  main.go                 signal ctx -> cli.Run(os.Args, deps()) -> os.Exit(code); build stamp vars
  app.go                  appConfig, prodConfig(), assemble(): builds adapters + usecases
  daemon.go               defaultDaemonSocket, daemonSocket(), unreachableClient, newDaemonClient()
  devpolicy_dev.go        //go:build !release  - AllowUntrusted for TEAMS_DEV_POLICY=1 (tests/dev)
  devpolicy_release.go    //go:build release   - no-op
  integration_test.go     end-to-end through cli.Run with authtest.Fake + graphtest fake server
internal/domain/
  types.go  alias.go  policy.go  policy_eval.go  errors.go          (F: types+errors; A: logic)
  classify.go  mention.go  filters.go  htmltext.go  cursor.go  ratelimit.go  loopguard.go  validate.go
internal/usecase/
  ports.go                ports + Commands + request/response DTOs (F)
  service.go  whoami.go  destinations.go  send.go  reply.go  inbox.go  ack.go  thread.go  selftest.go
  usecasetest/            in-memory fakes of every port (F creates the skeleton, then ownership passes to WS-B)
internal/adapters/graph/
  client.go  chats.go  channels.go  messages.go  dto.go  errors.go  paging.go
  graphtest/              httptest fake Graph server used by graph, cli and cmd tests
internal/adapters/state/   ledger.go  cursors.go  lock.go  atomic.go
internal/adapters/policyfile/  provider.go  load.go  schema.go  trust_unix.go  trust_other.go
internal/adapters/auditlog/    sink.go
internal/adapters/selftestcfg/ selftestcfg.go
internal/adapters/cli/     cli.go  commands.go  flags.go  present.go  skill.go  meta.go
internal/infra/clock/     clock.go (System, Fake for tests)
internal/infra/fstrust/   trust.go (ownership + mode + O_NOFOLLOW checks, injectable stat)
internal/infra/config/    env.go (TEAMS_POLICY, TEAMS_STATE_DIR, AGENT_ID, AGENT_RUN_ID, AGENT_OKTA_D_SOCKET)
internal/archtest/        archtest_test.go
docs/  user-docs/  .github/workflows/ci.yml
```

## 4. Ports (internal/usecase/ports.go, defined first in Phase F)

| Port | Methods (summary) | Implemented by |
|---|---|---|
| `Graph` | `Me(ctx) (domain.Profile, error)`; `ResolveUserChat(ctx, aadID, create bool) (chatID, error)`; `PostChat(ctx, chatID, domain.OutMessage) (domain.PostResult, error)`; `PostChannel(ctx, teamID, channelID, threadID, domain.OutMessage)`; `ListChatMessages(ctx, chatID, since time.Time, limit int) ([]domain.RawMessage, error)`; `ListChannelMessages(ctx, teamID, channelID, deltaToken string, since time.Time, limit int) ([]domain.RawMessage, newDelta string, error)`; `ListReplies(ctx, teamID, channelID, messageID string, limit int)`; `GetChat(ctx, chatID) error` (negative probe); `FindByMarker(ctx, dest, marker) (msgID string, found bool, err error)` | `adapters/graph` |
| `Ledger` | `Reserve(ctx, key, payloadHash, dest, thread string, now) (Reservation, error)`; `Complete(ctx, key, msgID string, now)`; `Fail(ctx, key, notSent bool)`; `SentSince(ctx, since time.Time) ([]Sent, error)`; `SentInThread(ctx, thread string, since time.Time) (int, error)`; `RecordSent(ctx, Sent)` (keyless sends); `PutThread(ctx, thread)` / `ActiveThreads(ctx, since, max) []string` (threads the agent posted in, for reply polling, UA-3) | `adapters/state` |
| `CursorStore` | `Get(ctx, alias) (domain.CursorState, error)`; `RecordDeliveries(ctx, alias, []domain.DeliveryEntry, now) error`; `Ack(ctx, alias, ids []string, now) (acked, already int, unknown []string, err error)` (validates against the delivery index, advances the watermark); `StoreDelta(ctx, alias, token string)`; `ResolveChat(ctx, alias) (chatID string, ok bool)`; `CacheChat(ctx, alias, chatID)`; `DropChat(ctx, alias)` | `adapters/state` |
| `PolicyProvider` | `Policy(ctx) (domain.Policy, error)` (strict, trust-checked, fail closed) | `adapters/policyfile` |
| `AuditSink` | `Record(ctx, domain.AuditEvent) error`; `Close()` | `adapters/auditlog` (core `audit`) |
| `Clock` | `Now() time.Time`; `Sleep(ctx, d) error` | `infra/clock` |
| `Rand` | `Jitter(base time.Duration, pct float64) time.Duration` | `infra/clock` |
| `RunInfo` | `AgentID`, `RunID` strings | `cmd/teams` |

`usecase.Commands` is the facade the CLI calls (`Whoami`, `Destinations`, `Send`, `Reply`, `Inbox`, `Ack`, `ThreadGet`, `Selftest`); request/response DTOs live next to it so the CLI never touches Graph types. Errors cross ports as `domain.Error` or wrapped core errors (`%w`), both reach an exit code through `output.CategoryError`.

Port rule for writes: the Graph adapter returns `domain.NotSent(err)` when it can prove the server did not process the POST (401/429/400/403/404 before processing, URL/TLS/dial errors); any other write failure is ambiguous and the use case leaves the ledger entry `pending`.

## 5. How core packages are used

| Core package | Used in | How |
|---|---|---|
| `output` | cli, domain (Category only), usecase | `Envelope`, `Success`, `FromError`, `Write`, `Untrusted`, `ExitFor`. `domain.Error` implements `CategoryError` and `Hinter`. |
| `auth` | `cmd/teams`, `adapters/graph` | `NewDaemonTokenSource(newDaemonClient(), "msgraph", WithRemediation(...))`, `NewAuthorizer`. Tests: `auth/authtest.Fake`. |
| `httpx` | `adapters/graph` | `NewClient(Config{Refresher: authorizer, AllowedHosts: ["graph.microsoft.com"], VendorCode: headerCode})`; reads marked safe to retry (`MarkSafeToRetry`), writes not. Typed errors pass through unchanged to exit codes 3/4/8. |
| `audit` | `adapters/auditlog` | `audit.Open` / `Logger.Handle`; `policy_decision` carries `allow;k=v` suffixes (core Record has no extension fields: requested change). Block mode for writes. |
| `selftest` | `adapters/selftestcfg`, cli | `Runner{Rows, Probe}` with the matrix in spec s8. |
| `docgen` | `adapters/cli/skill.go` | `docgen.Generate(CommandTree)` from the same command table the router uses (single source). |
| `policy` | not used | Typed domain policy instead (D12); gaps in `docs/requested-core-changes.md`. |

## 6. Composition root (cmd/teams/app.go)

```
assemble(ctx, cfg):
  1. policy  = policyfile.NewProvider(cfg.PolicyPath, cfg.PolicyOpts...).Policy(ctx)   // fail closed; stops the run
  2. state   = state.Open(policy.StateDir, clock)       // ledger + cursors, advisory lock
  3. daemon  = cfg.Daemon (prod: newDaemonClient())
     src     = auth.NewDaemonTokenSource(daemon, "msgraph", auth.WithRemediation("a human must run: agent-okta-d enroll msgraph"))
     authz   = auth.NewAuthorizer(src)
  4. graph   = graph.New(graph.Config{Refresher: authz, BaseURL: cfg.GraphBaseURL, HTTP: cfg.Graph})
  5. audit   = auditlog.New(policy.Audit.Path, agentID, runID, clock)
  6. cmds    = usecase.New(usecase.Deps{Policy, Graph: graph, Ledger: state, Cursors: state, Audit: audit, Clock, Rand, Run})
  7. return app{cmds, audit}
```

`version`, `skill` and `destinations list` skip steps 3-4 and need no network; `version` also skips the policy. `appConfig` is injectable so integration tests substitute `authtest.Fake`, a `graphtest` server URL, a fake clock and `AllowUntrusted` policy loading.

## 7. Data flow

Send: `cli flags -> Commands.Send -> begin (policy, UPN guard once per run) -> alias lookup -> validation (size, text) -> mentions (domain) -> filters (domain) -> rate/write caps (Ledger.SentSince + per-run counter) -> loop guard (Ledger.SentInThread) -> Ledger.Reserve -> Graph.Post* -> Ledger.Complete (or Fail / stay pending) -> AuditSink.Record -> presenter -> output.Write -> exit code`.

Inbox: `begin -> for each watch:true destination (fixed order): resolve chat id if user: (CursorStore cache, D6) -> Graph.List*Messages(since = max(watermark, now-max_lookback)) -> domain.Normalize (drop non-message, deleted, own) -> domain.Classify -> domain.SelectHandle (direct|mentions|watched) -> domain.Undelivered(acked set) -> merge, sort by received,id -> limit -> CursorStore.RecordDeliveries (delivery index) -> present`. If empty and `--wait` remains: `Clock.Sleep(Jitter(poll_interval))`, repeat. `inbox` moves neither watermark nor acked set; it only records deliveries so `ack` can validate ids.

Ack: `begin(no UPN call; local only) -> split ids by alias -> policy check -> CursorStore.Ack` (ids not in the delivery index -> exit 9, nothing changes; watermark advance in `domain.CursorState.Ack`).
Reply/thread get: the `thread_id` embeds the alias, so policy is checked from the id alone; no state lookup.

Sequence (send, ambiguous failure):
```
CLI -> Commands.Send -> Ledger.Reserve(key) [pending]
    -> Graph.PostChat -> timeout (ambiguous)
    <- error (not NotSent)  => entry stays pending, exit 1, hint "outcome unknown"
retry same key -> Ledger.Reserve -> pending => exit 7 (or marker scan if enabled)
```

## 8. Error and exit-code model

All use-case errors are `domain.Error{Category, Message, Hint}` (implements `output.CategoryError`, `output.Hinter`). Core errors (`auth.*`, `httpx.*`) pass through with `%w`; `output.FromError` maps them. The mapping table is spec D9; there is no teams-specific numeric code. Graph 404 and 409 are mapped in `adapters/graph/errors.go` (`NotFound`, `Conflict`); 401/403/429 come typed from `httpx`.

## 9. State files

`state_dir/teams.ledger.json` and `state_dir/teams.cursors.json`: versioned JSON, 0600 in 0700 dir, atomic rename, `flock` advisory lock shared by both (`state.Open` holds one lock file, 10 s timeout -> exit 7). Contents specified in `data-dictionary.md`. Directory trust: not world-writable, owned by the current user (`fstrust`).

## 10. Untrusted content

Untrusted: message text, `sender.name`, mention display names read from Graph (not policy). Wrapped only in the presenter with `output.Untrusted`. HTML is converted to text in the domain; image tags ignored; nothing is fetched.

## 11. Build

`make build|test|race|cover|lint|fmt|vet|cross|skill`. `-ldflags -X main.version/commit/date`; release tag `-tags release` removes the dev trust override. `make skill` runs hidden `teams skill > dist/teams-cli.md`.

## 12. Architectural decisions

Recorded in `docs/architectural-decision-record.md`: ADR-1 delegated user model (PRD), ADR-2 typed domain policy rather than core `policy` (D12), ADR-3 at-least-once inbox with explicit ack (D8), ADR-4 drop unlisted inbound (D4), ADR-5 policy-only mentions (D5), ADR-6 daemon client stub (D10, also `docs/adr-daemon-client-stub.md`), ADR-7 stdlib `flag` CLI (D15), ADR-8 ledger fail-closed, cursors fail-open (D7), ADR-9 poll only listed destinations (no `GET /me/chats` discovery).
