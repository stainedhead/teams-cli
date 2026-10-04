# teams user documentation

These pages help you install, configure and use `teams`, a command line tool that lets an AI agent post to and read Microsoft Teams as its own named Entra user.

Status: the command surface, policy enforcement and local state are built and tested against fakes. The credential daemon adapter is not released yet, so every command that talks to Microsoft Graph currently exits with code 3. All Microsoft Graph behavior described here is unverified against a real tenant. See [Getting started](getting-started.md) for what works today.

| Page | Contents |
|---|---|
| [Install](install.md) | Building the binary, supported platforms, file locations |
| [Getting started](getting-started.md) | Prerequisites, enrollment, first commands, current limits |
| [Configuration and policy reference](configuration.md) | Environment variables, every policy field, defaults and validation |
| [Sample policy](teams.policy.sample.yaml) | Annotated starting policy with placeholder values only |
| [Usage](usage.md) | Every command with flags, examples and output |
| [Inbox, ack and state](inbox-and-state.md) | At-least-once delivery, the ack workflow, state directory and volume guidance |
| [Troubleshooting](troubleshooting.md) | Exit codes and what to do about each |
