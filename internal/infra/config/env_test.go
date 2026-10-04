package config

import (
	"regexp"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestFromEnvDefaults(t *testing.T) {
	e := FromEnv(env(nil))
	if e.PolicyPath != DefaultPolicyPath || e.Socket != DefaultSocket || e.AgentID != "" || e.StateDirOverride != "" {
		t.Fatalf("defaults: %+v", e)
	}
	if !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(e.RunID) {
		t.Fatalf("run id %q", e.RunID)
	}
	if FromEnv(env(nil)).RunID == e.RunID {
		t.Fatal("run ids must differ")
	}
}

func TestFromEnvOverrides(t *testing.T) {
	e := FromEnv(env(map[string]string{
		EnvPolicy: " /tmp/p.yaml ", EnvStateDir: "/tmp/s", EnvAgentID: "a1", EnvRunID: "r1", EnvSocket: "/tmp/x.sock",
	}))
	want := Env{"/tmp/p.yaml", "/tmp/s", "a1", "r1", "/tmp/x.sock"}
	if e != want {
		t.Fatalf("got %+v", e)
	}
}

func TestBlankIsUnset(t *testing.T) {
	e := FromEnv(env(map[string]string{EnvPolicy: "  ", EnvStateDir: " "}))
	if e.PolicyPath != DefaultPolicyPath || e.StateDirOverride != "" {
		t.Fatalf("%+v", e)
	}
}

func TestStateDirPrecedence(t *testing.T) {
	// FR-R1: a state_dir the policy sets wins over the agent-controlled env.
	if got := (Env{StateDirOverride: "/env"}).StateDir("/pol", true); got != "/pol" {
		t.Fatalf("pinned policy must win, got %q", got)
	}
	// The env applies only when the policy left state_dir unset.
	if got := (Env{StateDirOverride: "/env"}).StateDir("", false); got != "/env" {
		t.Fatal(got)
	}
	if got := (Env{}).StateDir("/pol", true); got != "/pol" {
		t.Fatal(got)
	}
	if got := (Env{}).StateDir(" ", false); got != DefaultStateDir {
		t.Fatal(got)
	}
}
