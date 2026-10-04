package main

import (
	"context"
	"os"

	"github.com/stainedhead/agent-cli-core/auth"

	"github.com/stainedhead/teams-cli/internal/infra/config"
)

// daemonSocket is where agent-okta-d is expected to listen: AGENT_OKTA_D_SOCKET,
// else the default path (unverified, UA-22).
func daemonSocket() string { return config.FromEnv(os.Getenv).Socket }

// unreachableClient is the temporary auth.DaemonClient: agent-okta-d has no Go
// client yet (D10), so every call reports the daemon as unreachable, naming the
// socket (exit 3, no fallback credentials).
type unreachableClient struct{ socket string }

func (c unreachableClient) Fetch(context.Context, string) (auth.Token, error) {
	return auth.Token{}, &auth.UnreachableError{Socket: c.socket}
}

func (c unreachableClient) Refresh(context.Context, string) (auth.Token, error) {
	return auth.Token{}, &auth.UnreachableError{Socket: c.socket}
}

// newDaemonClient returns the credential-daemon client. Swapping this one
// function for the real adapter is the whole change; agent-okta-d is never a
// dependency of this module.
func newDaemonClient() auth.DaemonClient { return unreachableClient{socket: daemonSocket()} }
