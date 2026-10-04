# Install

## Status

There is no tagged release and no published binary. You build `teams` from source. Release signing, notarization and SBOMs are not set up yet.

## Platforms

- macOS (Apple silicon, darwin/arm64)
- Linux (amd64 and arm64)
- Windows is not supported natively; use WSL.

The binary is static (`CGO_ENABLED=0`) and has no runtime dependencies.

## Build from source

Requirements: Go 1.27 or newer, git, and access to the `stainedhead` GitHub organization because the module depends on `github.com/stainedhead/agent-cli-core`, which is fetched as a private module.

```
export GOPRIVATE='github.com/stainedhead/*'
git clone https://github.com/stainedhead/teams-cli.git
cd teams-cli
make build
./bin/teams version
```

`make cross` builds all three platform binaries into `dist/`.

Install the binary somewhere on the agent's `PATH`, for example `/usr/local/bin/teams`. The agent user must not be able to replace it.

## Files and directories

| Item | Default | Override | Who should own it |
|---|---|---|---|
| Policy file | `/etc/agent-cli/teams.policy.yaml` | `TEAMS_POLICY` | root, not writable by the agent |
| State directory | `/var/lib/agent-cli/teams` | `state_dir` in policy (`TEAMS_STATE_DIR` only if the policy omits it) | the agent user, mode 0700 |
| Audit log | set by `audit.path` in policy | none | the agent user can append; mode 0600 |
| Daemon socket | `/var/run/agentd/agentd.sock` (macOS), `/run/agentd/agentd.sock` (Linux) | `AGENT_OKTA_D_SOCKET` | the daemon |

The default daemon socket path is the daemon client's default and unverified and may differ in your deployment; set `AGENT_OKTA_D_SOCKET` if so.

Install the policy as root so the agent cannot edit its own guardrails:

```
sudo install -d -o root -m 0755 /etc/agent-cli
sudo install -o root -m 0644 teams.policy.sample.yaml /etc/agent-cli/teams.policy.yaml
```

`teams` refuses to run (exit 9) if the policy file or any directory above it is not owned by root, is group or world writable, or is a symlink. A policy in your home directory will be refused.

## Verify

```
teams version
```

`version` needs no policy file and no network. Every other command needs a valid, trusted policy file.
