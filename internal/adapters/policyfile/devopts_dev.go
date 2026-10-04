//go:build teamsdev

package policyfile

import "github.com/stainedhead/teams-cli/internal/infra/config"

// DevBuild is true only when built with -tags teamsdev (never released).
const DevBuild = true

// DevOptions returns AllowUntrusted when TEAMS_POLICY_INSECURE=1, so a
// developer can use a policy in their own home directory. This file is
// compiled only with -tags teamsdev.
func DevOptions(getenv func(string) string) []Option {
	if getenv(config.EnvPolicyInsecure) == "1" {
		return []Option{AllowUntrusted()}
	}
	return nil
}
