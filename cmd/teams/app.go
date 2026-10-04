package main

import (
	"context"
	"os"
	"time"

	"github.com/stainedhead/agent-cli-core/auth"
	"github.com/stainedhead/agent-cli-core/httpx"
	"github.com/stainedhead/agent-cli-core/selftest"

	"github.com/stainedhead/teams-cli/internal/adapters/auditlog"
	"github.com/stainedhead/teams-cli/internal/adapters/graph"
	"github.com/stainedhead/teams-cli/internal/adapters/policyfile"
	"github.com/stainedhead/teams-cli/internal/adapters/selftestcfg"
	"github.com/stainedhead/teams-cli/internal/adapters/state"
	"github.com/stainedhead/teams-cli/internal/domain"
	"github.com/stainedhead/teams-cli/internal/infra/clock"
	"github.com/stainedhead/teams-cli/internal/infra/config"
	"github.com/stainedhead/teams-cli/internal/usecase"
)

// Provider name the credential daemon knows the Graph credential by
// (agent-okta-d PRD section 7.5), and the human-actionable remediation text.
const (
	graphProvider = "msgraph"
	remediation   = "a human must run: agent-okta-d enroll msgraph"
)

// appConfig holds everything the composition root can be pointed elsewhere
// for. Production uses prodConfig(); integration tests inject fakes and never
// touch a real network.
type appConfig struct {
	Env config.Env
	// PolicyOpts are policyfile load options. Production passes only the
	// build-tagged developer override (policyfile.DevOptions, a no-op in
	// release builds); tests pass policyfile.AllowUntrusted().
	PolicyOpts []policyfile.Option
	// Daemon is the credential-daemon client; production uses newDaemonClient().
	Daemon auth.DaemonClient
	// GraphBaseURL empty means https://graph.microsoft.com/v1.0.
	GraphBaseURL string
	// HTTP carries retry tuning (clock, jitter); zero in production.
	HTTP httpx.Config
	// Clock and Rand default to the system implementations.
	Clock usecase.Clock
	Rand  usecase.Rand
}

func prodConfig() appConfig {
	return appConfig{
		Env:        config.FromEnv(os.Getenv),
		PolicyOpts: policyfile.DevOptions(os.Getenv),
		Daemon:     newDaemonClient(),
		Clock:      clock.System{},
		Rand:       clock.Rand{},
	}
}

// app is the assembled object graph.
type app struct {
	cmds   usecase.Commands
	graph  usecase.Graph
	policy domain.Policy
	run    usecase.RunInfo
	audit  *auditlog.Sink
}

func (a *app) close() { _ = a.audit.Close() }

// assemble builds every adapter and the use cases (architecture s6). The
// policy loads first: one that cannot be loaded (missing, invalid, untrusted)
// stops the run before any other adapter is built.
func assemble(ctx context.Context, cfg appConfig) (*app, error) {
	provider := policyfile.NewProvider(cfg.Env.PolicyPath, cfg.PolicyOpts...)
	pol, err := provider.Policy(ctx)
	if err != nil {
		return nil, err
	}
	if cfg.Clock == nil {
		cfg.Clock = clock.System{}
	}
	if cfg.Rand == nil {
		cfg.Rand = clock.Rand{}
	}
	if cfg.Daemon == nil {
		cfg.Daemon = newDaemonClient()
	}
	run := usecase.RunInfo{AgentID: cfg.Env.AgentID, RunID: cfg.Env.RunID}
	if run.AgentID == "" {
		run.AgentID = pol.Profile
	}
	if run.RunID == "" {
		run.RunID = config.NewRunID()
	}

	store, err := state.Open(cfg.Env.StateDir(pol.StateDir), cfg.Clock,
		state.WithCursorRetention(pol.Inbound.MaxLookback+time.Hour))
	if err != nil {
		return nil, err
	}

	src, err := auth.NewDaemonTokenSource(cfg.Daemon, graphProvider, auth.WithRemediation(remediation))
	if err != nil {
		return nil, domain.NewValidation("credential source cannot be built: "+err.Error(), "")
	}
	gc, err := graph.New(graph.Config{
		Refresher:   auth.NewAuthorizer(src),
		BaseURL:     cfg.GraphBaseURL,
		HTTP:        cfg.HTTP,
		MaxChatScan: pol.Limits.MaxChatScan,
	})
	if err != nil {
		return nil, err
	}

	sink, err := auditlog.Open(auditlog.Config{
		Path: pol.Audit.Path, AgentID: run.AgentID, RunID: run.RunID, Clock: cfg.Clock,
	})
	if err != nil {
		return nil, err
	}

	cmds := newCommands(useCaseDeps{
		Policy: provider, Graph: gc, Ledger: store, Cursors: store,
		Audit: sink, Clock: cfg.Clock, Rand: cfg.Rand, Run: run,
	})
	return &app{cmds: cmds, graph: gc, policy: pol, run: run, audit: sink}, nil
}

// commandsFor returns the lazy use-case constructor the CLI calls. The audit
// file stays open for the (short) life of the process.
func commandsFor(cfg appConfig) func(context.Context) (usecase.Commands, error) {
	return func(ctx context.Context) (usecase.Commands, error) {
		a, err := assemble(ctx, cfg)
		if err != nil {
			return nil, err
		}
		return a.cmds, nil
	}
}

// selftestFor returns the `teams selftest` runner (E5). Only the send-allowed
// row posts, and --read-only skips it.
func selftestFor(cfg appConfig) func(context.Context, bool) (selftest.Result, error) {
	return func(ctx context.Context, readOnly bool) (selftest.Result, error) {
		a, err := assemble(ctx, cfg)
		if err != nil {
			return selftest.Result{}, err
		}
		defer a.close()
		return selftestcfg.Runner(selftestcfg.Deps{
			Cmds: a.cmds, Graph: a.graph, Policy: a.policy, RunID: a.run.RunID,
		}, readOnly).Run(ctx)
	}
}
