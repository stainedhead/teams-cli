//go:build !teamsdev

package policyfile

// DevBuild is false in release builds: the ownership check cannot be relaxed
// by any environment variable (FR-21).
const DevBuild = false

// DevOptions returns no options in a release build.
func DevOptions(func(string) string) []Option { return nil }
