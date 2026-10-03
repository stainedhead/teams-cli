# AGENTS.md

Rules for AI agents and human contributors working in this repository.

## Project summary

`teams-cli` builds the `teams` binary: a Go CLI that lets AI agents post to and read Microsoft Teams as their own named Entra user. It calls Microsoft Graph with delegated permissions (application-only permissions cannot send chat messages), obtains short-lived Graph tokens from the `agent-okta-d` daemon (provider `msgraph`), enforces a client-side policy (destinations, senders, rates, content filters), and polls for inbound messages because no public endpoint is hosted. Full requirements: `teams-cli-PRD.md` (Draft v0.2). Status: no implementation yet.

## Layout (planned; see README.md)

- `cmd/teams/` - entry point only; no logic.
- `internal/` - application code, organized by Clean Architecture layers (domain, usecase, adapters, infrastructure).
- `docs/` - product and technical documentation (product-summary, product-details, technical-details, architectural-decision-record).
- `user-docs/` - end-user documentation (see rule below).
- `specs/` - feature specs; completed specs move to `specs/archive/`.
- `teams-cli-PRD.md` - the product requirements document; do not overwrite or rewrite it without being asked.

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
go test ./...
```

`make fmt lint test` runs the same checks.

## `user-docs/` rule

`user-docs/` holds only files that help a user adopt, configure and use the tool: install, getting started, configuration reference, usage examples, troubleshooting. It is NOT for design, requirements, spec or process material, and must not link into `specs/`. Design and requirements belong in `docs/` or `specs/`.

## Git

- Do not force-push. Do not commit generated binaries, `.env` files or local state.
- Commit messages are short and imperative.
