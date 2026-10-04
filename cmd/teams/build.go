package main

import (
	"runtime/debug"

	"github.com/stainedhead/teams-cli/internal/adapters/cli"
)

// Build metadata, stamped by the Makefile with
// -ldflags "-X main.version=... -X main.commit=... -X main.date=..." (REL-4).
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// buildInfo returns the stamped values, falling back to the module and VCS
// data the Go toolchain embeds (go install module@version, go build in a
// checkout) for any value left at its default.
func buildInfo() cli.BuildInfo {
	return resolveBuild(version, commit, date, debug.ReadBuildInfo)
}

func resolveBuild(v, c, d string, read func() (*debug.BuildInfo, bool)) cli.BuildInfo {
	b := cli.BuildInfo{Version: v, Commit: c, Date: d}
	needV, needC, needD := v == "" || v == "dev", c == "" || c == "none", d == "" || d == "unknown"
	if !needV && !needC && !needD {
		return b
	}
	bi, ok := read()
	if !ok || bi == nil {
		return b
	}
	if needV && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		b.Version = bi.Main.Version
	}
	var rev, when string
	dirty := false
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.time":
			when = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if needC && rev != "" {
		if len(rev) > 12 {
			rev = rev[:12]
		}
		if dirty {
			rev += "-dirty"
		}
		b.Commit = rev
	}
	if needD && when != "" {
		b.Date = when
	}
	return b
}
