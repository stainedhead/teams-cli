# ADR: newDaemonClient() returns an unreachable stub

Status: superseded by `adr-daemon-adapter-wired.md`

Context: tokens come only from agent-okta-d (provider `msgraph`). agent-cli-core v0.1.0 defines the `auth.DaemonClient` interface but ships no socket adapter, and agent-okta-d has not published `pkg/client`. agent-okta-d must not be a dependency of this module.

Decision: `cmd/teams/daemon.go` defines `newDaemonClient()`, which returns a client whose `Fetch` and `Refresh` return `*auth.UnreachableError{Socket}`. Every network command therefore exits 3 with core's message naming the socket (`AGENT_OKTA_D_SOCKET`, default `/run/agent-okta-d/agent-okta-d.sock`, unverified: UA-22). `version` and `destinations` need no daemon. No fallback credentials exist. Tests use `auth/authtest.Fake` through the same `auth.DaemonClient` seam.

Consequences: replacing the stub with the real client is a one-function change in the composition root. Until then no command reaches Graph. The real adapter and the socket ownership check are tracked in `deferred.md`.
