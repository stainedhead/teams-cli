package main

import (
	"testing"

	"github.com/stainedhead/agent-cli-core/auth/oktad"
)

func TestNewDaemonClientIsOktadAndHonorsSocketEnv(t *testing.T) {
	t.Setenv("AGENT_OKTA_D_SOCKET", "/tmp/x.sock")
	c, ok := newDaemonClient().(*oktad.Client)
	if !ok {
		t.Fatalf("newDaemonClient returned %T, want *oktad.Client", newDaemonClient())
	}
	if c.SocketPath() != "/tmp/x.sock" {
		t.Errorf("socket = %q, want the AGENT_OKTA_D_SOCKET value", c.SocketPath())
	}
}

func TestNewDaemonClientDefaultSocketWhenEnvEmpty(t *testing.T) {
	t.Setenv("AGENT_OKTA_D_SOCKET", "")
	c := newDaemonClient().(*oktad.Client)
	if c.SocketPath() == "" {
		t.Error("expected the adapter's platform default socket")
	}
}

func TestProdConfigUsesOktadClient(t *testing.T) {
	if _, ok := prodConfig().Daemon.(*oktad.Client); !ok {
		t.Fatalf("prodConfig daemon is %T, want *oktad.Client", prodConfig().Daemon)
	}
}
