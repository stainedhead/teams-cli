package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stainedhead/agent-cli-core/auth"
	"github.com/stainedhead/agent-cli-core/auth/authtest"
	"github.com/stainedhead/agent-cli-core/output"

	"github.com/stainedhead/teams-cli/internal/adapters/cli"
	"github.com/stainedhead/teams-cli/internal/adapters/graph/graphtest"
	"github.com/stainedhead/teams-cli/internal/infra/clock"
)

// Integration tests (E7): the real CLI router, use cases, state, policy file,
// audit sink and Graph adapter against graphtest, with authtest standing in
// for the credential daemon and a fake clock. Every "run" builds a fresh
// object graph, as a new process would, so persistence is what the files hold.

const (
	itChat   = "19:x@thread.v2"
	itNow    = "2026-10-03T12:00:00Z"
	itSocket = "/run/test/agent-okta-d.sock"
)

type itEnv struct {
	t     *testing.T
	cfg   appConfig
	srv   *graphtest.Server
	fake  *authtest.Fake
	dir   string
	audit string
}

func newITEnv(t *testing.T, scenario authtest.Scenario) *itEnv {
	t.Helper()
	cfg := testConfig(t)
	now, _ := time.Parse(time.RFC3339, itNow)
	cfg.Clock = clock.NewFake(now)
	srv := graphtest.New(t)
	srv.SetMe("id-1", "Bot", "bot@corp.example.com")
	cfg.GraphBaseURL = srv.BaseURL()
	fake := authtest.New(scenario, authtest.WithSocket(itSocket))
	cfg.Daemon = fake
	srv.AddChat(itChat, "u2")
	dir := filepath.Dir(cfg.Env.PolicyPath)
	return &itEnv{t: t, cfg: cfg, srv: srv, fake: fake, dir: dir,
		audit: filepath.Join(dir, "audit", "teams.audit.jsonl")}
}

type result struct {
	code output.ExitCode
	out  string
	env  struct {
		OK    bool            `json:"ok"`
		Data  json.RawMessage `json:"data"`
		Error struct {
			Category string `json:"category"`
			Message  string `json:"message"`
		} `json:"error"`
	}
}

// run executes one CLI invocation with a freshly assembled object graph.
func (e *itEnv) run(args ...string) result {
	e.t.Helper()
	var out bytes.Buffer
	d := cli.Deps{
		NewCommands: commandsFor(e.cfg), Selftest: selftestFor(e.cfg),
		Build: cli.BuildInfo{Version: "t"}, Stdout: &out,
	}
	r := result{code: cli.Run(context.Background(), args, d), out: out.String()}
	if r.out != "" && args[0] != "skill" {
		if err := json.Unmarshal([]byte(r.out), &r.env); err != nil {
			e.t.Fatalf("%v: output is not a JSON envelope: %v\n%s", args, err, r.out)
		}
	}
	return r
}

func (e *itEnv) seed(id, content string, minutesAgo int) {
	now, _ := time.Parse(time.RFC3339, itNow)
	e.srv.AddChatMessage(itChat, graphtest.Message{
		ID: id, Content: content, FromUserID: "u2", FromName: "Pat",
		Created: now.Add(-time.Duration(minutesAgo) * time.Minute),
	})
}

func (r result) data(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal(r.env.Data, v); err != nil {
		t.Fatalf("data: %v\n%s", err, r.out)
	}
}

func (e *itEnv) auditLines() []map[string]any {
	e.t.Helper()
	b, err := os.ReadFile(e.audit)
	if err != nil {
		return nil
	}
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if l == "" {
			continue
		}
		m := map[string]any{}
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			e.t.Fatalf("audit line is not JSON: %q", l)
		}
		out = append(out, m)
	}
	return out
}

func (e *itEnv) lastAudit() map[string]any {
	e.t.Helper()
	l := e.auditLines()
	if len(l) == 0 {
		e.t.Fatal("no audit lines")
	}
	return l[len(l)-1]
}

func wantExit(t *testing.T, name string, r result, want output.ExitCode) {
	t.Helper()
	if r.code != want {
		t.Fatalf("%s: exit %d, want %d\n%s", name, r.code, want, r.out)
	}
}

func TestITWhoamiDestinationsVersionSkill(t *testing.T) {
	e := newITEnv(t, authtest.Valid)
	r := e.run("whoami")
	wantExit(t, "whoami", r, 0)
	var who map[string]any
	r.data(t, &who)
	if who["upn"] != "bot@corp.example.com" || who["policy"] != "agent" || who["version"] != "t" ||
		who["policy_path"] != e.cfg.Env.PolicyPath || who["policy_version"] != float64(1) {
		t.Fatalf("whoami: %v", who)
	}
	if got := e.fake.Providers(); len(got) == 0 || got[0] != graphProvider {
		t.Fatalf("daemon asked for %v", got)
	}
	if a := e.lastAudit(); a["verb"] != "whoami" || a["outcome"] != "ok" || a["ts"] != itNow {
		t.Fatalf("audit: %v", a)
	}

	r = e.run("destinations", "list")
	wantExit(t, "destinations", r, 0)
	if strings.Contains(r.out, itChat) {
		t.Fatal("raw chat id leaked")
	}
	var ds []map[string]any
	r.data(t, &ds)
	if len(ds) != 1 || ds[0]["alias"] != "chat:dev" {
		t.Fatalf("destinations: %v", ds)
	}

	n := len(e.srv.Requests())
	wantExit(t, "version", e.run("version"), 0)
	if r := e.run("skill"); r.code != 0 || !strings.Contains(r.out, "### send") {
		t.Fatalf("skill: %d", r.code)
	}
	if len(e.srv.Requests()) != n {
		t.Fatal("version and skill must not touch Graph")
	}
}

func TestITSendDryRunIdempotencyReplay(t *testing.T) {
	e := newITEnv(t, authtest.Valid)
	r := e.run("send", "--to", "chat:dev", "--text", "plan", "--dry-run")
	wantExit(t, "dry-run", r, 0)
	var sr map[string]any
	r.data(t, &sr)
	if sr["dry_run"] != true || e.srv.Count("POST /chats/") != 0 {
		t.Fatalf("dry-run posted: %v", sr)
	}
	if a := e.lastAudit(); a["outcome"] != "dry_run" {
		t.Fatalf("audit: %v", a)
	}

	r = e.run("send", "--to", "chat:dev", "--text", "hello", "--idempotency-key", "k-1")
	wantExit(t, "send", r, 0)
	r.data(t, &sr)
	first := sr["message_id"]
	if first == "" || sr["deduplicated"] != false || sr["thread_id"] != "chat:dev/chat" {
		t.Fatalf("send: %v", sr)
	}
	if got := len(e.srv.Posts()); got != 1 {
		t.Fatalf("posts = %d", got)
	}

	// A new process with the same key replays from the ledger on disk.
	r = e.run("send", "--to", "chat:dev", "--text", "hello", "--idempotency-key", "k-1")
	wantExit(t, "replay", r, 0)
	r.data(t, &sr)
	if sr["deduplicated"] != true || sr["message_id"] != first {
		t.Fatalf("replay: %v", sr)
	}
	if got := len(e.srv.Posts()); got != 1 {
		t.Fatalf("replay re-posted: %d", got)
	}
	if a := e.lastAudit(); a["outcome"] != "deduplicated" {
		t.Fatalf("audit: %v", a)
	}

	// Same key, different payload is refused and still does not post.
	r = e.run("send", "--to", "chat:dev", "--text", "different", "--idempotency-key", "k-1")
	if r.code == 0 || len(e.srv.Posts()) != 1 {
		t.Fatalf("key reuse with new payload: exit %d posts %d", r.code, len(e.srv.Posts()))
	}
	if _, err := os.Stat(filepath.Join(e.cfg.Env.StateDirOverride)); err != nil {
		t.Fatal(err)
	}
}

func TestITReplyInboxAckThread(t *testing.T) {
	e := newITEnv(t, authtest.Valid)
	e.seed("m1", "first question", 10)
	e.seed("m2", "second question", 5)

	r := e.run("inbox")
	wantExit(t, "inbox", r, 0)
	var items []struct {
		ID       string `json:"id"`
		ThreadID string `json:"thread_id"`
	}
	r.data(t, &items)
	if len(items) != 2 || items[0].ID != "chat:dev/m1" || items[1].ID != "chat:dev/m2" {
		t.Fatalf("inbox: %s", r.out)
	}
	if strings.Contains(r.out, itChat) {
		t.Fatal("raw chat id leaked")
	}

	// At-least-once: a new process sees the same unacked items.
	r = e.run("inbox")
	r.data(t, &items)
	if len(items) != 2 {
		t.Fatalf("unacked items must be redelivered: %s", r.out)
	}

	r = e.run("ack", "chat:dev/m1")
	wantExit(t, "ack", r, 0)
	var ar map[string]int
	r.data(t, &ar)
	if ar["acked"] != 1 {
		t.Fatalf("ack: %v", ar)
	}
	r = e.run("ack", "chat:dev/m1")
	r.data(t, &ar)
	if r.code != 0 || ar["already"] != 1 {
		t.Fatalf("ack is idempotent: %d %v", r.code, ar)
	}

	// Ack persisted across runs.
	r = e.run("inbox")
	r.data(t, &items)
	if len(items) != 1 || items[0].ID != "chat:dev/m2" {
		t.Fatalf("after ack: %s", r.out)
	}

	thread := items[0].ThreadID
	r = e.run("reply", "--thread", thread, "--text", "on it")
	wantExit(t, "reply", r, 0)
	if len(e.srv.Posts()) != 1 {
		t.Fatalf("posts = %d", len(e.srv.Posts()))
	}
	r = e.run("thread", "get", thread)
	wantExit(t, "thread get", r, 0)
	var th []struct {
		ID string `json:"id"`
	}
	r.data(t, &th)
	if len(th) != 2 { // the agent's own reply is not an inbound item
		t.Fatalf("thread: %s", r.out)
	}
	// thread get changes no acks.
	r = e.run("inbox")
	r.data(t, &items)
	if len(items) != 1 {
		t.Fatalf("thread get must not ack: %s", r.out)
	}
	if _, err := os.Stat(e.audit); err != nil {
		t.Fatal(err)
	}
	verbs := map[string]bool{}
	for _, a := range e.auditLines() {
		verbs[a["verb"].(string)] = true
	}
	for _, v := range []string{"inbox", "ack", "reply"} {
		if !verbs[v] {
			t.Errorf("no audit line for %s: %v", v, verbs)
		}
	}
}

func TestITSelftest(t *testing.T) {
	e := newITEnv(t, authtest.Valid)
	r := e.run("selftest", "--read-only")
	wantExit(t, "selftest --read-only", r, 0)
	if e.srv.Count("POST") != 0 {
		t.Fatal("--read-only must not post")
	}
	r = e.run("selftest")
	wantExit(t, "selftest", r, 0)
	var res struct{ Passed, Failed int }
	r.data(t, &res)
	if res.Failed != 0 || res.Passed < 6 || len(e.srv.Posts()) != 1 {
		t.Fatalf("selftest: %s posts=%d", r.out, len(e.srv.Posts()))
	}
}

func TestITPolicyDenialExit6(t *testing.T) {
	e := newITEnv(t, authtest.Valid)
	for name, args := range map[string][]string{
		"unlisted alias": {"send", "--to", "chat:nope", "--text", "x"},
		"mention":        {"send", "--to", "chat:dev", "--text", "hi", "--mention", "user:ghost"},
	} {
		r := e.run(args...)
		if r.code == 0 {
			t.Errorf("%s: must be refused", name)
		}
	}
	r := e.run("send", "--to", "chat:nope", "--text", "x")
	wantExit(t, "unlisted", r, output.ExitPolicyDenied)
	if len(e.srv.Posts()) != 0 {
		t.Fatal("a denied send must not post")
	}
	a := e.lastAudit()
	if a["outcome"] != "denied" || !strings.HasPrefix(a["policy_decision"].(string), "deny:") {
		t.Fatalf("audit: %v", a)
	}

	// Identity mismatch: policy upn differs from /me; nothing else is called.
	e2 := newITEnv(t, authtest.Valid)
	e2.srv.SetMe("id-2", "Other", "someone.else@corp.example.com")
	wantExit(t, "upn mismatch", e2.run("inbox"), output.ExitPolicyDenied)
	if n := len(e2.srv.Requests()); n != 1 {
		t.Fatalf("only /me may be called, got %d requests", n)
	}
}

func TestITAuthScenarios(t *testing.T) {
	failing := []authtest.Scenario{authtest.Unreachable, authtest.ReauthRequired, authtest.Revoked}
	for _, sc := range failing {
		t.Run(sc.String(), func(t *testing.T) {
			e := newITEnv(t, sc)
			for _, args := range [][]string{
				{"whoami"}, {"inbox"}, {"send", "--to", "chat:dev", "--text", "x"},
				{"thread", "get", "chat:dev/chat"}, {"ack", "chat:dev/m1"},
			} {
				r := e.run(args...)
				if args[0] == "ack" {
					continue // ack is local state only; see below
				}
				wantExit(t, strings.Join(args, " "), r, output.ExitAuth)
			}
			if len(e.srv.Requests()) != 0 || len(e.srv.Posts()) != 0 {
				t.Fatal("no request may leave without a token")
			}
			wantExit(t, "destinations", e.run("destinations", "list"), 0)
			wantExit(t, "version", e.run("version"), 0)
			if sc == authtest.Unreachable {
				if r := e.run("whoami"); !strings.Contains(r.out, itSocket) {
					t.Fatalf("unreachable must name the socket: %s", r.out)
				}
			}
		})
	}

	t.Run("refresh-and-retry succeeds", func(t *testing.T) {
		e := newITEnv(t, authtest.UnauthorizedThenSuccess)
		e.srv.Unauthorized401(1)
		wantExit(t, "whoami", e.run("whoami"), 0)
		if e.fake.Refreshes() != 1 {
			t.Fatalf("refreshes = %d", e.fake.Refreshes())
		}
	})
	t.Run("refresh-and-retry fails", func(t *testing.T) {
		e := newITEnv(t, authtest.UnauthorizedTwice)
		e.srv.Unauthorized401(2)
		wantExit(t, "whoami", e.run("whoami"), output.ExitAuth)
		if len(e.srv.Posts()) != 0 {
			t.Fatal("no post")
		}
	})
}

func TestITStubDaemonExit3(t *testing.T) {
	e := newITEnv(t, authtest.Valid)
	t.Setenv("AGENT_OKTA_D_SOCKET", itSocket)
	e.cfg.Daemon = newDaemonClient()
	for _, args := range [][]string{{"whoami"}, {"inbox"}, {"send", "--to", "chat:dev", "--text", "x"}} {
		r := e.run(args...)
		wantExit(t, strings.Join(args, " "), r, output.ExitAuth)
	}
	// selftest aggregates per-row probe failures into one general error; it
	// still fails and names the socket.
	if r := e.run("selftest", "--read-only"); r.code == 0 || !strings.Contains(r.out, itSocket) {
		t.Fatalf("selftest with stub daemon: %d %s", r.code, r.out)
	}
	if len(e.srv.Requests()) != 0 {
		t.Fatal("no request may leave")
	}
	wantExit(t, "destinations", e.run("destinations", "list"), 0)
	var ue *auth.UnreachableError
	if _, err := newDaemonClient().Fetch(context.Background(), graphProvider); !errors.As(err, &ue) || ue.Socket != itSocket {
		t.Fatalf("stub: %v", err)
	}
}

func TestITNoTokenInOutputAuditOrState(t *testing.T) {
	e := newITEnv(t, authtest.Valid)
	e.seed("m1", "question", 5)
	var outs []string
	for _, args := range [][]string{
		{"whoami"}, {"destinations", "list"}, {"send", "--to", "chat:dev", "--text", "hello", "--idempotency-key", "k"},
		{"inbox"}, {"ack", "chat:dev/m1"}, {"selftest"}, {"send", "--to", "chat:nope", "--text", "x"},
	} {
		outs = append(outs, e.run(args...).out)
	}
	if e.fake.Fetches() == 0 {
		t.Fatal("token was never fetched")
	}
	for _, r := range e.srv.Requests() {
		if !r.HasAuth {
			t.Fatalf("unauthenticated request %s %s", r.Method, r.Path)
		}
		if strings.Contains(r.Body, "fake-token") {
			t.Fatal("token in a request body")
		}
	}
	for _, o := range outs {
		if strings.Contains(o, "fake-token") || strings.Contains(o, "Bearer") {
			t.Fatalf("token material in output: %s", o)
		}
	}
	files := 0
	err := filepath.WalkDir(e.dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		files++
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		if bytes.Contains(b, []byte("fake-token")) || bytes.Contains(b, []byte("Bearer ")) {
			t.Errorf("token material in %s", p)
		}
		return nil
	})
	if err != nil || files < 3 {
		t.Fatalf("walk: %v files=%d", err, files)
	}
}
