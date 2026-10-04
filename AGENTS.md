# AGENTS.md

Rules for AI agents and human contributors working in this repository.

## Project summary

`teams-cli` builds the `teams` binary: a Go CLI that lets AI agents post to and read Microsoft Teams as their own named Entra user. It calls Microsoft Graph with delegated permissions (application-only permissions cannot send chat messages), obtains short-lived Graph tokens from the `agent-okta-d` daemon (provider `msgraph`), enforces a client-side policy (destinations, senders, rates, content filters), and polls for inbound messages because no public endpoint is hosted. Requirements and decisions: `docs/product-details.md` and `docs/architectural-decision-record.md`; the PRD and specs are under `specs/archive/`. Shared behavior comes from [`agent-cli-core`](https://github.com/stainedhead/agent-cli-core), a separate repository and build dependency. Status: implemented and tested against fakes; the `agent-okta-d` client adapter is not yet released, so Graph-backed commands exit 3 until it is. See `docs/deferred.md`.

## Layout (planned; see README.md)

- `INTENT.md` - why this repo exists, the wider agentic-teams context, goals, non-goals and scope. A shift in goal, direction or scope means updating INTENT.md first.
- `cmd/teams/` - entry point only; no logic.
- `internal/` - application code, organized by Clean Architecture layers (domain, usecase, adapters, infrastructure).
- `docs/` - product and technical documentation (product-summary, product-details, technical-details, architectural-decision-record).
- `user-docs/` - end-user documentation (see rule below).
- `specs/` - feature specs; completed specs move to `specs/archive/`.
- `specs/archive/261003-teams-cli/teams-cli-PRD.md` - the product requirements document; do not overwrite or rewrite it without being asked.

## Architecture and engineering standards

- Clean Architecture: dependencies point inward. Domain has no imports from outer layers; use cases depend on interfaces; Graph, daemon socket, filesystem and clock access live behind adapters.
- TDD: write a failing test first, make it pass, then refactor. Every behavior change ships with tests. Prefer table-driven tests; no network access in unit tests.
- Keep functions small, return errors with context (`fmt.Errorf("...: %w", err)`), pass `context.Context` first.
- Never write, log or commit credentials, tokens, refresh tokens or tenant secrets. Token values must never appear in logs, audit output or test fixtures.
- Record significant design decisions in `docs/architectural-decision-record.md`.

## Verification

Run before every commit and fix all findings:

```
gofmt -l .
go vet ./...
golangci-lint run
go test -race ./...
```

`make check` (fmt-check, vet, lint, race) runs the same checks.

## Dependency on agent-cli-core

- Changes to shared behavior (envelope, exit codes, bounds, policy engine, audit, daemon-client wrapper) are made in [agent-cli-core](https://github.com/stainedhead/agent-cli-core), never copied into this repository.
- Depend on released tags only: no pseudo-versions, no `replace` directives on `main`.
- `go.mod` requires `agent-cli-core v0.1.0` (and `goccy/go-yaml`); bumps are ordinary PRs. No `replace` directive, ever. Set `GOPRIVATE=github.com/stainedhead/*` to fetch it.

## `user-docs/` rule

`user-docs/` holds only files that help a user adopt, configure and use the tool: install, getting started, configuration reference, usage examples, troubleshooting. It is NOT for design, requirements, spec or process material, and must not link into `specs/`. Design and requirements belong in `docs/` or `specs/`.

## Git

- Do not force-push. Do not commit generated binaries, `.env` files or local state.
- Commit messages are short and imperative.

## Agent skill

How agents use this tool is documented in the root repository's skill document, `skills/teams-cli.md`, in https://github.com/stainedhead/agentic-teams (see `skills/README.md`). That is its only home; do not copy it here. A change to the command surface, flags, exit codes, policy verbs or write modes, or forbidden actions is not finished until that skill is updated (see SKILL-1..7 in the PRD).
