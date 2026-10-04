# ADR: newDaemonClient() returns core's oktad adapter

Status: accepted. Supersedes `adr-daemon-client-stub.md` (ADR-6), which stays as history.

Context: `agent-cli-core` v0.2.1 ships `auth/oktad`, an `auth.DaemonClient` over the `agent-okta-d` unix socket. v0.2.0 had a bug in which `DaemonTokenSource` wrapped the adapter's typed errors (a degraded daemon exited 3 instead of 8); v0.2.1 fixes it, so no local wrapper exists.

Decision: `cmd/teams/daemon.go` `newDaemonClient()` returns `oktad.New(oktad.WithTimeout(10s))`. The socket is `AGENT_OKTA_D_SOCKET` if set, else the adapter's platform default (`/var/run/agentd/agentd.sock` on macOS, `/run/agentd/agentd.sock` on Linux). teams has no config-file socket setting, so there is no further precedence level, and `config.Env` no longer carries a socket. The provider stays `msgraph` and the remediation stays `a human must run: agent-okta-d enroll msgraph`. No fallback credentials exist.

Consequences: `go.mod` now requires `agent-okta-d` indirectly through core. `internal/archtest` allows that requirement and allows one direct import: `agent-okta-d/pkg/client/clienttest` (the fake daemon), from `_test.go` files in `cmd/teams` only. Exit codes: daemon unreachable, `reauth_required`, revoked, `not_configured` and `unauthorized` exit 3; degraded or retry-hinted exit 8; a cancelled context exits 1. Tests run the CLI against `clienttest` on a real unix socket. The socket ownership check remains deferred (`deferred.md`). The default socket path is now the adapter's, not the earlier assumption UA-22.
