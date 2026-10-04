// Package config resolves environment variables and defaults for the teams
// CLI. It imports only the standard library and never reads secrets.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
)

// Environment variable names.
const (
	EnvPolicy   = "TEAMS_POLICY"
	EnvStateDir = "TEAMS_STATE_DIR"
	EnvAgentID  = "AGENT_ID"
	EnvRunID    = "AGENT_RUN_ID"
	EnvSocket   = "AGENT_OKTA_D_SOCKET"
	// EnvPolicyInsecure is honored only by a teamsdev build (policyfile).
	EnvPolicyInsecure = "TEAMS_POLICY_INSECURE"
)

// Defaults.
const (
	DefaultPolicyPath = "/etc/agent-cli/teams.policy.yaml"
	DefaultStateDir   = "/var/lib/agent-cli/teams"
	// DefaultSocket is unverified (UA-22).
	DefaultSocket = "/run/agent-okta-d/agent-okta-d.sock"
)

// Env is the resolved process environment.
type Env struct {
	PolicyPath string
	// StateDirOverride is TEAMS_STATE_DIR, empty when unset. It wins over the
	// policy's state_dir; see StateDir.
	StateDirOverride string
	AgentID          string
	RunID            string
	Socket           string
}

// FromEnv resolves the environment through getenv (os.Getenv in production).
// Blank values count as unset. RunID is generated when AGENT_RUN_ID is unset.
func FromEnv(getenv func(string) string) Env {
	get := func(k, def string) string {
		if v := strings.TrimSpace(getenv(k)); v != "" {
			return v
		}
		return def
	}
	e := Env{
		PolicyPath:       get(EnvPolicy, DefaultPolicyPath),
		StateDirOverride: get(EnvStateDir, ""),
		AgentID:          get(EnvAgentID, ""),
		RunID:            get(EnvRunID, ""),
		Socket:           get(EnvSocket, DefaultSocket),
	}
	if e.RunID == "" {
		e.RunID = NewRunID()
	}
	return e
}

// StateDir returns the state directory: the environment override, else the
// policy value, else the default.
func (e Env) StateDir(policyValue string) string {
	switch {
	case e.StateDirOverride != "":
		return e.StateDirOverride
	case strings.TrimSpace(policyValue) != "":
		return policyValue
	}
	return DefaultStateDir
}

// NewRunID returns a random 16 hex character run id.
func NewRunID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "0000000000000000"
	}
	return hex.EncodeToString(b[:])
}
