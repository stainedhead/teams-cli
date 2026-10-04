// Package policyfile loads the strict YAML policy file; implemented in Phase P
// (see specs/261003-teams-cli/tasks.md).
package policyfile

// The YAML parser is pinned here until the loader lands (tasks ST2).
import _ "github.com/goccy/go-yaml"
