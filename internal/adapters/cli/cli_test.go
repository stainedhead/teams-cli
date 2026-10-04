package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/agent-cli-core/selftest"

	"github.com/stainedhead/teams-cli/internal/domain"
	"github.com/stainedhead/teams-cli/internal/usecase"
)

var update = flag.Bool("update", false, "rewrite golden files")

// fake is a test double of usecase.Commands recording every request.
type fake struct {
	send    []usecase.SendRequest
	reply   []usecase.ReplyRequest
	inbox   []usecase.InboxRequest
	ack     []usecase.AckRequest
	thread  []usecase.ThreadRequest
	items   []domain.InboundItem
	err     error
	sendRes usecase.SendResult
	who     usecase.WhoamiResult
}

func (f *fake) Whoami(context.Context) (usecase.WhoamiResult, error) {
	if f.who.Policy != "" {
		return f.who, f.err
	}
	return usecase.WhoamiResult{Profile: domain.Profile{ID: "id1", DisplayName: "Bot", UPN: "bot@x.com"}, Policy: "agent"}, f.err
}

func (f *fake) Destinations(context.Context) (usecase.DestinationsResult, error) {
	return usecase.DestinationsResult{Destinations: []usecase.DestinationView{
		{Alias: "chat:dev", Kind: domain.KindChat, DisplayName: "Dev", Send: true, Watch: true},
		{Alias: "user:jane", Kind: domain.KindUser},
	}}, f.err
}

func (f *fake) Send(_ context.Context, r usecase.SendRequest) (usecase.SendResult, error) {
	f.send = append(f.send, r)
	return f.sendRes, f.err
}

func (f *fake) Reply(_ context.Context, r usecase.ReplyRequest) (usecase.SendResult, error) {
	f.reply = append(f.reply, r)
	return f.sendRes, f.err
}

func (f *fake) Inbox(_ context.Context, r usecase.InboxRequest) (usecase.InboxResult, error) {
	f.inbox = append(f.inbox, r)
	return usecase.InboxResult{Items: f.items}, f.err
}

func (f *fake) Ack(_ context.Context, r usecase.AckRequest) (usecase.AckResult, error) {
	f.ack = append(f.ack, r)
	return usecase.AckResult{Acked: len(r.IDs) - 1, Already: 1}, f.err
}

func (f *fake) ThreadGet(_ context.Context, r usecase.ThreadRequest) (usecase.ThreadResult, error) {
	f.thread = append(f.thread, r)
	return usecase.ThreadResult{Items: f.items}, f.err
}

func (f *fake) Selftest(context.Context, usecase.SelftestRequest) (usecase.SelftestResult, error) {
	return usecase.SelftestResult{}, f.err
}

var _ usecase.Commands = (*fake)(nil)

type result struct {
	code output.ExitCode
	out  string
}

func exec(t *testing.T, f *fake, stdin string, args ...string) result {
	t.Helper()
	return execDeps(t, Deps{NewCommands: func(context.Context) (usecase.Commands, error) { return f, nil }}, stdin, args...)
}

func execDeps(t *testing.T, d Deps, stdin string, args ...string) result {
	t.Helper()
	var out bytes.Buffer
	d.Stdout = &out
	d.Stdin = strings.NewReader(stdin)
	if d.Build == (BuildInfo{}) {
		d.Build = BuildInfo{Version: "1.2.3", Commit: "abc", Date: "2026-01-01T00:00:00Z"}
	}
	code := Run(context.Background(), args, d)
	return result{code, out.String()}
}

type env struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Meta  map[string]any  `json:"meta"`
	Error *struct {
		Code, Message, Hint string
	} `json:"error"`
}

func parse(t *testing.T, s string) env {
	t.Helper()
	var e env
	if err := json.Unmarshal([]byte(s), &e); err != nil {
		t.Fatalf("not an envelope: %v\n%s", err, s)
	}
	return e
}

func TestUsageErrorsExit2(t *testing.T) {
	cases := [][]string{
		{"bogus"},
		{"thread"},
		{"thread", "nope"},
		{"whoami", "extra"},
		{"whoami", "--nope"},
		{"destinations", "list", "x"},
		{"send"},
		{"send", "--to", "19:abc@thread.v2", "--text", "x"},
		{"send", "--to", "chat:dev"},
		{"send", "--to", "chat:dev", "--text", "a", "--file", "f"},
		{"send", "--to", "chat:dev", "--text", "a", "--mention", "BAD"},
		{"send", "--to", "chat:dev", "--text", "a", "--thread", "chat:other/chat"},
		{"reply", "--text", "a"},
		{"reply", "--thread", "chat:dev/chat"},
		{"inbox", "--limit", "0"},
		{"inbox", "--wait", "soon"},
		{"inbox", "--wait", "-5"},
		{"inbox", "--since", "garbage"},
		{"inbox", "--alias", "nope"},
		{"inbox", "extra"},
		{"ack"},
		{"thread", "get"},
		{"thread", "get", "chat:dev/chat", "--limit", "0"},
		{"version", "x"},
		{"whoami", "--format", "xml"},
		{"whoami", "--max-bytes", "-1"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			f := &fake{}
			r := exec(t, f, "", args...)
			if r.code != output.ExitUsage {
				t.Fatalf("code = %d, want 2; out=%s", r.code, r.out)
			}
			if e := parse(t, r.out); e.OK || e.Error == nil || e.Error.Code != "usage" {
				t.Fatalf("envelope = %s", r.out)
			}
			if len(f.send)+len(f.reply)+len(f.inbox)+len(f.ack)+len(f.thread) != 0 {
				t.Fatal("use case reached on usage error")
			}
		})
	}
}

func TestAckCountBounds(t *testing.T) {
	ids := make([]string, 101)
	for i := range ids {
		ids[i] = "chat:dev/x"
	}
	if r := exec(t, &fake{}, "", append([]string{"ack"}, ids...)...); r.code != output.ExitUsage {
		t.Fatalf("101 ids: %d", r.code)
	}
	f := &fake{}
	r := exec(t, f, "", append([]string{"ack"}, ids[:100]...)...)
	if r.code != 0 || len(f.ack[0].IDs) != 100 {
		t.Fatalf("100 ids: %d %s", r.code, r.out)
	}
}

func TestSendRoutesToUseCase(t *testing.T) {
	f := &fake{sendRes: usecase.SendResult{MessageID: "m1", ThreadID: "chat:dev/chat", DryRun: true, Findings: []domain.Finding{{Filter: "secret_patterns", PatternID: "aws"}}}}
	r := exec(t, f, "", "send", "--to", "chat:dev", "--text", "hi", "--mention", "user:jane,user:bob", "--mention", "user:amy", "--idempotency-key", "k1", "--dry-run")
	if r.code != 0 {
		t.Fatalf("code %d: %s", r.code, r.out)
	}
	got := f.send[0]
	if got.Alias != "chat:dev" || got.Text != "hi" || got.IdempotencyKey != "k1" || !got.DryRun || len(got.Mentions) != 3 {
		t.Fatalf("request = %+v", got)
	}
	var d map[string]any
	_ = json.Unmarshal(parse(t, r.out).Data, &d)
	if d["message_id"] != "m1" || d["dry_run"] != true {
		t.Fatalf("data = %v", d)
	}
}

func TestSendWithThreadIsReply(t *testing.T) {
	f := &fake{}
	r := exec(t, f, "", "send", "--to", "channel:alerts", "--thread", "channel:alerts/123", "--text", "ok")
	if r.code != 0 || len(f.send) != 0 || len(f.reply) != 1 || f.reply[0].ThreadID != "channel:alerts/123" {
		t.Fatalf("code=%d send=%v reply=%v", r.code, f.send, f.reply)
	}
	if r := exec(t, f, "", "send", "--to", "channel:alerts", "--thread", "malformed", "--text", "ok"); r.code != output.ExitValidation {
		t.Fatalf("malformed thread: %d", r.code)
	}
}

func TestReply(t *testing.T) {
	f := &fake{}
	r := exec(t, f, "", "reply", "--thread", "chat:dev/chat", "--text", "yo", "--dry-run")
	if r.code != 0 || f.reply[0].ThreadID != "chat:dev/chat" || !f.reply[0].DryRun {
		t.Fatalf("%d %+v", r.code, f.reply)
	}
}

func TestTextSources(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "m.txt")
	if err := os.WriteFile(file, []byte("from file"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := &fake{}
	if r := exec(t, f, "", "send", "--to", "chat:dev", "--file", file); r.code != 0 || f.send[0].Text != "from file" {
		t.Fatalf("file: %d %s", r.code, r.out)
	}
	if r := exec(t, f, "from stdin", "send", "--to", "chat:dev", "--file", "-"); r.code != 0 || f.send[1].Text != "from stdin" {
		t.Fatalf("stdin: %d %s", r.code, r.out)
	}
	// An explicit empty --text reaches the use case (it owns the empty-text rule).
	if r := exec(t, f, "", "send", "--to", "chat:dev", "--text", ""); r.code != 0 || f.send[2].Text != "" {
		t.Fatalf("empty text: %d", r.code)
	}
	for name, args := range map[string][]string{
		"missing file": {"send", "--to", "chat:dev", "--file", filepath.Join(dir, "nope")},
		"directory":    {"send", "--to", "chat:dev", "--file", dir},
	} {
		if r := exec(t, f, "", args...); r.code != output.ExitUsage {
			t.Errorf("%s: code %d", name, r.code)
		}
	}
}

func TestFileSymlinkAndFIFOHandling(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.txt")
	_ = os.WriteFile(real, []byte("x"), 0o600)
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("no symlinks")
	}
	b, err := readInputFile(link)
	if err != nil || string(b) != "x" {
		t.Fatalf("symlink to regular file: %q %v", b, err)
	}
}

func TestOversizeInputIsValidation(t *testing.T) {
	big := strings.Repeat("a", maxTextInput+1)
	r := exec(t, &fake{}, big, "send", "--to", "chat:dev", "--file", "-")
	if r.code != output.ExitValidation {
		t.Fatalf("code %d: %s", r.code, r.out)
	}
	d := Deps{NewCommands: func(context.Context) (usecase.Commands, error) { return &fake{}, nil },
		ReadFile: func(string) ([]byte, error) { return []byte(big), nil }}
	if r := execDeps(t, d, "", "send", "--to", "chat:dev", "--file", "x"); r.code != output.ExitValidation {
		t.Fatalf("file oversize: %d", r.code)
	}
	d.ReadFile = func(string) ([]byte, error) { return nil, errors.New("boom") }
	if r := execDeps(t, d, "", "send", "--to", "chat:dev", "--file", "x"); r.code != output.ExitUsage {
		t.Fatalf("read error: %d", r.code)
	}
}

func TestStdinTTYRefused(t *testing.T) {
	d := Deps{NewCommands: func(context.Context) (usecase.Commands, error) { return &fake{}, nil }, StdinIsTTY: func() bool { return true }}
	r := execDeps(t, d, "x", "send", "--to", "chat:dev", "--file", "-")
	if r.code != output.ExitUsage || !strings.Contains(r.out, "terminal") {
		t.Fatalf("code %d: %s", r.code, r.out)
	}
}

func TestIsTerminal(t *testing.T) {
	if isTerminal(strings.NewReader("")) {
		t.Fatal("reader is not a tty")
	}
	f, err := os.Open(os.DevNull) // a character device
	if err != nil {
		t.Skip()
	}
	defer func() { _ = f.Close() }()
	if !isTerminal(f) {
		t.Fatal("/dev/null is a char device")
	}
	r, w, _ := os.Pipe()
	defer func() { _ = r.Close(); _ = w.Close() }()
	if isTerminal(r) {
		t.Fatal("pipe is not a tty")
	}
}

func TestInboxRequestMapping(t *testing.T) {
	f := &fake{}
	cur := domain.EncodeCursor(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	r := exec(t, f, "", "inbox", "--wait", "30", "--limit", "5", "--alias", "chat:dev", "--since", cur)
	if r.code != 0 {
		t.Fatalf("%d %s", r.code, r.out)
	}
	got := f.inbox[0]
	if got.Wait != 30*time.Second || got.Limit != 5 || got.Alias != "chat:dev" || got.Since == nil || got.Since.Year() != 2026 {
		t.Fatalf("%+v", got)
	}
	exec(t, f, "", "inbox", "--wait", "2m")
	if f.inbox[1].Wait != 2*time.Minute || f.inbox[1].Limit != 20 {
		t.Fatalf("%+v", f.inbox[1])
	}
	if e := parse(t, exec(t, &fake{}, "", "inbox").out); string(e.Data) != "[]" {
		t.Fatalf("empty inbox must be [], got %s", e.Data)
	}
}

func TestUseCaseErrorMapsToExit(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		code output.ExitCode
	}{
		"policy":   {domain.NewPolicyDenied("no", "h"), output.ExitPolicyDenied},
		"conflict": {domain.NewConflict("c", ""), output.ExitConflict},
		"notfound": {domain.NewNotFound("n", ""), output.ExitNotFound},
		"plain":    {errors.New("boom"), output.ExitGeneral},
	} {
		t.Run(name, func(t *testing.T) {
			r := exec(t, &fake{err: tc.err}, "", "send", "--to", "chat:dev", "--text", "x")
			if r.code != tc.code {
				t.Fatalf("code %d", r.code)
			}
			if e := parse(t, r.out); e.OK || e.Error == nil {
				t.Fatal("expected failure envelope")
			}
		})
	}
}

func TestNewCommandsFailureExits(t *testing.T) {
	d := Deps{NewCommands: func(context.Context) (usecase.Commands, error) {
		return nil, domain.NewValidation("policy invalid", "")
	}}
	if r := execDeps(t, d, "", "whoami"); r.code != output.ExitValidation {
		t.Fatalf("code %d", r.code)
	}
	if r := execDeps(t, Deps{}, "", "whoami"); r.code != output.ExitValidation {
		t.Fatalf("unwired: %d", r.code)
	}
}

func TestLocalCommandsNeverBuildUseCases(t *testing.T) {
	d := Deps{NewCommands: func(context.Context) (usecase.Commands, error) {
		t.Fatal("NewCommands called")
		return nil, nil
	}}
	for _, args := range [][]string{{"version"}, {"skill"}, {}, {"help"}, {"--help"}, {"send", "-h"}} {
		if r := execDeps(t, d, "", args...); r.code != 0 {
			t.Fatalf("%v: %d %s", args, r.code, r.out)
		}
	}
}

func TestHelp(t *testing.T) {
	r := execDeps(t, Deps{}, "", "help")
	var d struct{ Commands []string }
	if err := json.Unmarshal(parse(t, r.out).Data, &d); err != nil || len(d.Commands) == 0 {
		t.Fatalf("%v %s", err, r.out)
	}
	for _, c := range d.Commands {
		if strings.Contains(c, "teams skill") {
			t.Fatal("hidden command listed")
		}
	}
	r = execDeps(t, Deps{}, "", "inbox", "-h")
	if !strings.Contains(r.out, "teams inbox") {
		t.Fatal(r.out)
	}
}

func TestVersion(t *testing.T) {
	r := execDeps(t, Deps{}, "", "version")
	var d map[string]string
	_ = json.Unmarshal(parse(t, r.out).Data, &d)
	if r.code != 0 || d["version"] != "1.2.3" || d["commit"] != "abc" || d["date"] != "2026-01-01T00:00:00Z" {
		t.Fatalf("%d %v", r.code, d)
	}
}

func TestWhoamiAndDestinations(t *testing.T) {
	r := exec(t, &fake{}, "", "whoami")
	var w map[string]any
	_ = json.Unmarshal(parse(t, r.out).Data, &w)
	if w["upn"] != "bot@x.com" || w["policy"] != "agent" || w["version"] != "1.2.3" {
		t.Fatalf("%v", w)
	}
	r = exec(t, &fake{}, "", "destinations", "list")
	var ds []map[string]any
	_ = json.Unmarshal(parse(t, r.out).Data, &ds)
	if len(ds) != 2 || ds[0]["alias"] != "chat:dev" || ds[0]["display_name"] != "Dev" {
		t.Fatalf("%v", ds)
	}
	if _, has := ds[1]["display_name"]; has {
		t.Fatal("empty display name must be omitted")
	}
}

func TestSelftestCommand(t *testing.T) {
	var gotRO bool
	res := selftest.Result{Rows: []selftest.RowResult{{Row: selftest.Row{Name: "identity"}, Status: selftest.StatusPass}}, Passed: 1}
	d := Deps{Selftest: func(_ context.Context, ro bool) (selftest.Result, error) { gotRO = ro; return res, nil }}
	if r := execDeps(t, d, "", "selftest", "--read-only"); r.code != 0 || !gotRO {
		t.Fatalf("%d ro=%v %s", r.code, gotRO, r.out)
	}
	res.Failed, res.Rows[0].Status = 1, selftest.StatusFail
	if r := execDeps(t, d, "", "selftest"); r.code != output.ExitGeneral {
		t.Fatalf("failing selftest must exit 1, got %d", r.code)
	}
	d.Selftest = func(context.Context, bool) (selftest.Result, error) {
		return selftest.Result{}, domain.NewValidation("policy invalid", "")
	}
	if r := execDeps(t, d, "", "selftest"); r.code != output.ExitValidation {
		t.Fatalf("%d", r.code)
	}
	if r := execDeps(t, Deps{}, "", "selftest"); r.code != output.ExitValidation {
		t.Fatalf("unavailable: %d", r.code)
	}
}

func TestSkillDeterministicAndComplete(t *testing.T) {
	a, err := Skill(BuildInfo{Version: "1.2.3"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Skill(BuildInfo{Version: "1.2.3"})
	if !bytes.Equal(a, b) {
		t.Fatal("not deterministic")
	}
	doc := string(a)
	for _, c := range commands() {
		if c.hidden {
			if strings.Contains(doc, "### "+c.name+"\n") {
				t.Errorf("hidden command %q documented", c.name)
			}
			continue
		}
		if !strings.Contains(doc, "### "+c.name+"\n") || !strings.Contains(doc, c.usage) {
			t.Errorf("command %q missing from skill", c.name)
		}
	}
	for _, want := range []string{"--dry-run", "--idempotency-key", "--mention", "--wait", "--since", "never include secrets"} {
		if !strings.Contains(doc, want) {
			t.Errorf("skill lacks %q", want)
		}
	}
	golden(t, "skill.golden.md", a)
	r := execDeps(t, Deps{Build: BuildInfo{Version: "1.2.3"}}, "", "skill")
	if r.code != 0 || r.out != string(a) {
		t.Fatalf("skill command output differs: code %d", r.code)
	}
}

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	p := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, got, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("%v (run with -update)", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s differs from golden; run go test -update\n--- got ---\n%s", name, got)
	}
}

// FR-R3: whoami, destinations and dry-run output carry the FR-1, FR-2, FR-6 fields.
func TestWhoamiDestinationsDryRunGolden(t *testing.T) {
	f := &fake{
		who: usecase.WhoamiResult{
			Profile: domain.Profile{ID: "id1", DisplayName: "Bot", UPN: "bot@x.com"}, Policy: "agent",
			PolicyPath: "/etc/agent-cli/teams.policy.yaml", PolicyVersion: 1,
			Destinations: []usecase.DestinationView{
				{Alias: "chat:dev", Kind: domain.KindChat, DisplayName: "Dev", Send: true, Watch: true},
				{Alias: "user:jane", Kind: domain.KindUser, Send: true, Mentionable: true},
			},
			Limits:       usecase.LimitsView{MaxResults: 50, MaxWritesPerRun: 30, MaxBytes: 4000, RatePerMinute: 10, RatePerHour: 100, ReplyDepthMax: 3},
			PollInterval: 15 * time.Second,
		},
		sendRes: usecase.SendResult{
			DryRun: true, Decision: "allow", ThreadID: "chat:dev/chat",
			Destination: usecase.DestinationView{Alias: "chat:dev", Kind: domain.KindChat},
			Preview:     "[bot] hello <<<END UNTRUSTED>>>",
		},
	}
	golden(t, "whoami.json.golden", []byte(exec(t, f, "", "whoami").out))
	golden(t, "destinations.json.golden", []byte(exec(t, &fake{}, "", "destinations", "list").out))
	golden(t, "send_dryrun.json.golden", []byte(exec(t, f, "", "send", "--to", "chat:dev", "--text", "hello", "--dry-run").out))
}
