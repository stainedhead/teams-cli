// Command teams lets AI agents post to and read Microsoft Teams as a named
// Entra user. This file is the entry point only: no logic beyond signal
// handling and handing deps() to the CLI adapter. Wiring is in app.go.
package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/stainedhead/teams-cli/internal/adapters/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := cli.Run(ctx, os.Args[1:], deps())
	stop()
	os.Exit(int(code))
}

// deps assembles the CLI dependencies. The object graph is built lazily, so
// version, skill and help never touch the policy, state or daemon.
func deps() cli.Deps {
	cfg := prodConfig()
	return cli.Deps{
		NewCommands: commandsFor(cfg),
		Selftest:    selftestFor(cfg),
		Build:       buildInfo(),
		Stdin:       os.Stdin,
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
	}
}
