package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/stainedhead/agent-cli-core/auth"
	"github.com/stainedhead/agent-cli-core/auth/authtest"
	"github.com/stainedhead/agent-cli-core/httpx"
	"github.com/stainedhead/agent-cli-core/output"

	"github.com/stainedhead/teams-cli/internal/adapters/cli"
	"github.com/stainedhead/teams-cli/internal/adapters/graph/graphtest"
	"github.com/stainedhead/teams-cli/internal/adapters/policyfile"
	"github.com/stainedhead/teams-cli/internal/infra/clock"
	"github.com/stainedhead/teams-cli/internal/infra/config"
)

const policyYAML = `
version: 1
profile: agent
upn: bot@corp.example.com
destinations:
  chat:dev:
    chat_id: "19:x@thread.v2"
    send: true
    watch: true
audit:
  path: %AUDIT%
`

func testConfig(t *testing.T) appConfig {
	t.Helper()
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	pp := filepath.Join(dir, "teams.policy.yaml")
	yaml := strings.ReplaceAll(policyYAML, "%AUDIT%", filepath.Join(dir, "audit", "teams.audit.jsonl"))
	if err := os.WriteFile(pp, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	return appConfig{
		Env:        config.Env{PolicyPath: pp, StateDirOverride: filepath.Join(dir, "state"), RunID: "run-test"},
		PolicyOpts: []policyfile.Option{policyfile.AllowUntrusted()},
		Daemon:     unreachableClient{socket: "/run/test/agent-okta-d.sock"},
		Clock:      clock.NewFake(time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)),
		HTTP:       httpx.Config{Jitter: -1},
	}
}

func TestDaemonStubIsUnreachableExit3NamingSocket(t *testing.T) {
	t.Setenv("AGENT_OKTA_D_SOCKET", "/tmp/custom-agent.sock")
	c := newDaemonClient()
	for name, fn := range map[string]func() (auth.Token, error){
		"fetch":   func() (auth.Token, error) { return c.Fetch(context.Background(), "msgraph") },
		"refresh": func() (auth.Token, error) { return c.Refresh(context.Background(), "msgraph") },
	} {
		_, err := fn()
		var ue *auth.UnreachableError
		if !errors.As(err, &ue) || ue.Socket != "/tmp/custom-agent.sock" {
			t.Fatalf("%s: %v", name, err)
		}
		if output.ExitOf(err) != output.ExitAuth || !strings.Contains(err.Error(), "/tmp/custom-agent.sock") {
			t.Fatalf("%s: exit %d, %q", name, output.ExitOf(err), err)
		}
	}
	t.Setenv("AGENT_OKTA_D_SOCKET", "")
	if daemonSocket() != config.DefaultSocket {
		t.Fatalf("default socket = %q", daemonSocket())
	}
}

func TestStubDaemonMakesEveryGraphCallExit3(t *testing.T) {
	cfg := testConfig(t)
	srv := graphtest.New(t)
	cfg.GraphBaseURL = srv.BaseURL()
	a, err := assemble(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer a.close()
	_, err = a.graph.Me(context.Background())
	if output.ExitOf(err) != output.ExitAuth || !strings.Contains(err.Error(), "/run/test/agent-okta-d.sock") {
		t.Fatalf("Me through stub: exit %d err %v", output.ExitOf(err), err)
	}
	if len(srv.Requests()) != 0 {
		t.Fatal("no request may leave without a token")
	}
}

func TestAssembleWithAuthFakeReachesGraph(t *testing.T) {
	cfg := testConfig(t)
	srv := graphtest.New(t)
	srv.SetMe("id-1", "Bot", "bot@corp.example.com")
	cfg.GraphBaseURL = srv.BaseURL()
	cfg.Daemon = authtest.New(authtest.Valid)
	a, err := assemble(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer a.close()
	p, err := a.graph.Me(context.Background())
	if err != nil || p.UPN != "bot@corp.example.com" {
		t.Fatalf("%+v %v", p, err)
	}
	if a.run.RunID != "run-test" || a.run.AgentID != "agent" {
		t.Fatalf("run = %+v", a.run)
	}
	if _, err := os.Stat(filepath.Join(cfg.Env.StateDirOverride)); err != nil {
		t.Fatalf("state dir not created: %v", err)
	}
}

func TestAssembleFailsClosedOnPolicy(t *testing.T) {
	cfg := testConfig(t)
	cfg.Env.PolicyPath = filepath.Join(t.TempDir(), "missing.yaml")
	_, err := assemble(context.Background(), cfg)
	if output.ExitOf(err) != output.ExitValidation {
		t.Fatalf("missing policy: exit %d (%v)", output.ExitOf(err), err)
	}
	if _, serr := os.Stat(cfg.Env.StateDirOverride); serr == nil {
		t.Fatal("state must not be created when the policy is rejected")
	}
	// Release-style options (no AllowUntrusted): a user-owned file is refused.
	cfg = testConfig(t)
	cfg.PolicyOpts = nil
	if _, err := assemble(context.Background(), cfg); output.ExitOf(err) != output.ExitValidation {
		t.Fatalf("untrusted policy: exit %d", output.ExitOf(err))
	}
}

func TestAssembleDefaults(t *testing.T) {
	cfg := testConfig(t)
	cfg.Clock, cfg.Rand, cfg.Daemon = nil, nil, nil
	cfg.Env.RunID, cfg.Env.AgentID = "", "agent-7"
	a, err := assemble(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer a.close()
	if a.run.AgentID != "agent-7" || a.run.RunID == "" {
		t.Fatalf("run = %+v", a.run)
	}
}

func TestAssembleBadBaseURL(t *testing.T) {
	cfg := testConfig(t)
	cfg.GraphBaseURL = "ftp://nope"
	if _, err := assemble(context.Background(), cfg); output.ExitOf(err) != output.ExitUsage {
		t.Fatalf("exit %d", output.ExitOf(err))
	}
}

func TestAssembleBadAuditPath(t *testing.T) {
	cfg := testConfig(t)
	b, _ := os.ReadFile(cfg.Env.PolicyPath)
	blocker := filepath.Join(filepath.Dir(cfg.Env.PolicyPath), "blocker")
	_ = os.WriteFile(blocker, nil, 0o600)
	_ = os.WriteFile(cfg.Env.PolicyPath, bytes.ReplaceAll(b, []byte(filepath.Join(filepath.Dir(cfg.Env.PolicyPath), "audit")), []byte(blocker)), 0o600)
	if _, err := assemble(context.Background(), cfg); err == nil {
		t.Fatal("unwritable audit path must fail")
	}
}

func runCLI(t *testing.T, d cli.Deps, args ...string) (output.ExitCode, string) {
	t.Helper()
	var out bytes.Buffer
	d.Stdout = &out
	return cli.Run(context.Background(), args, d), out.String()
}

func TestVersionAndSkillNeedNoPolicyNoDaemon(t *testing.T) {
	t.Setenv("TEAMS_POLICY", filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	t.Setenv("AGENT_OKTA_D_SOCKET", filepath.Join(t.TempDir(), "no.sock"))
	d := deps()
	code, out := runCLI(t, d, "version")
	var e struct {
		Data map[string]string
	}
	if err := json.Unmarshal([]byte(out), &e); err != nil || code != 0 || e.Data["version"] == "" {
		t.Fatalf("version: %d %s", code, out)
	}
	if code, out = runCLI(t, d, "skill"); code != 0 || !strings.Contains(out, "### send") {
		t.Fatalf("skill: %d", code)
	}
}

func TestNetworkCommandsFailClosedWithoutPolicy(t *testing.T) {
	t.Setenv("TEAMS_POLICY", filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	d := deps()
	for _, args := range [][]string{{"whoami"}, {"destinations", "list"}, {"inbox"}, {"send", "--to", "chat:dev", "--text", "x"}} {
		if code, out := runCLI(t, d, args...); code != output.ExitValidation {
			t.Errorf("%v: exit %d %s", args, code, out)
		}
	}
}

func TestCLIEndToEndStubDaemon(t *testing.T) {
	cfg := testConfig(t)
	d := cli.Deps{NewCommands: commandsFor(cfg), Selftest: selftestFor(cfg), Build: cli.BuildInfo{Version: "t"}}
	code, out := runCLI(t, d, "whoami")
	if code != output.ExitAuth || !strings.Contains(out, "/run/test/agent-okta-d.sock") {
		t.Fatalf("whoami with stub daemon: %d %s", code, out)
	}
	if code, _ := runCLI(t, d, "destinations", "list"); code != 0 {
		t.Fatalf("destinations needs no daemon: %d", code)
	}
}

func TestSelftestRunsThroughAssembly(t *testing.T) {
	cfg := testConfig(t)
	d := cli.Deps{Selftest: selftestFor(cfg)}
	code, out := runCLI(t, d, "selftest", "--read-only")
	// With unwired use cases (or the stub daemon) rows fail, never panic;
	// the command must yield a well-formed failure envelope, not crash.
	if code == 0 {
		t.Skip("selftest passes end to end; covered by integration tests")
	}
	if !strings.Contains(out, `"ok":false`) {
		t.Fatalf("not an envelope: %s", out)
	}
	cfg.Env.PolicyPath = filepath.Join(t.TempDir(), "missing")
	if code, _ := runCLI(t, cli.Deps{Selftest: selftestFor(cfg)}, "selftest"); code != output.ExitValidation {
		t.Fatalf("selftest without policy: %d", code)
	}
}

func TestResolveBuild(t *testing.T) {
	read := func(settings ...debug.BuildSetting) func() (*debug.BuildInfo, bool) {
		return func() (*debug.BuildInfo, bool) {
			return &debug.BuildInfo{Main: debug.Module{Version: "v1.4.0"}, Settings: settings}, true
		}
	}
	vcs := []debug.BuildSetting{
		{Key: "vcs.revision", Value: "0123456789abcdef0123"}, {Key: "vcs.time", Value: "2026-02-03T04:05:06Z"}, {Key: "vcs.modified", Value: "true"},
	}
	cases := []struct {
		name       string
		v, c, d    string
		read       func() (*debug.BuildInfo, bool)
		wantV, wC  string
		wantD      string
		notCalling bool
	}{
		{"stamped wins", "1.2.3", "abc", "2026-01-01", read(vcs...), "1.2.3", "abc", "2026-01-01", true},
		{"module version fallback", "dev", "abc", "d", read(), "v1.4.0", "abc", "d", false},
		{"vcs fallback", "1.0.0", "none", "unknown", read(vcs...), "1.0.0", "0123456789ab-dirty", "2026-02-03T04:05:06Z", false},
		{"devel module stays dev", "dev", "none", "unknown", func() (*debug.BuildInfo, bool) {
			return &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, true
		}, "dev", "none", "unknown", false},
		{"no build info", "dev", "none", "unknown", func() (*debug.BuildInfo, bool) { return nil, false }, "dev", "none", "unknown", false},
		{"clean tree", "dev", "none", "unknown", read(vcs[0]), "v1.4.0", "0123456789ab", "unknown", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			read := tc.read
			if tc.notCalling {
				read = func() (*debug.BuildInfo, bool) {
					t.Fatal("build info must not be read when fully stamped")
					return nil, false
				}
			}
			got := resolveBuild(tc.v, tc.c, tc.d, read)
			if got.Version != tc.wantV || got.Commit != tc.wC || got.Date != tc.wantD {
				t.Fatalf("got %+v", got)
			}
		})
	}
	if b := buildInfo(); b.Version == "" {
		t.Fatal("empty version")
	}
}
