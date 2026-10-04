package auditlog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stainedhead/agent-cli-core/audit"

	"github.com/stainedhead/teams-cli/internal/domain"
	"github.com/stainedhead/teams-cli/internal/infra/clock"
)

var t0 = time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

type rec struct {
	Tool           string    `json:"tool"`
	AgentID        string    `json:"agent_id"`
	RunID          string    `json:"run_id"`
	Verb           string    `json:"verb"`
	Resource       string    `json:"resource"`
	Outcome        string    `json:"outcome"`
	HTTPStatus     int       `json:"http_status"`
	PolicyDecision string    `json:"policy_decision"`
	Ts             time.Time `json:"ts"`
	Duration       string    `json:"duration"`
	SchemaVersion  int       `json:"schema_version"`
}

func decode(t *testing.T, b []byte) []rec {
	t.Helper()
	var out []rec
	for _, line := range bytes.Split(bytes.TrimSpace(b), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var r rec
		if err := json.Unmarshal(line, &r); err != nil {
			t.Fatalf("invalid JSON line %q: %v", line, err)
		}
		out = append(out, r)
	}
	return out
}

func TestRecordFieldsAndFolding(t *testing.T) {
	var buf bytes.Buffer
	s := NewWithWriter(&buf, Config{AgentID: "agent-7", RunID: "run-1", Clock: clock.NewFake(t0)})
	err := s.Record(context.Background(), domain.AuditEvent{
		Verb: "inbox", Resource: "channel:sdlc-alerts", Outcome: "ok", HTTPStatus: 200, Duration: 1500 * time.Millisecond,
		Decision: "allow",
		Extra:    map[string]string{"count": "3", "dropped": "1", "skipped": "user:jane.doe:no_chat", "message_id": "1696341900000", "deduplicated": "false"},
	})
	if err != nil {
		t.Fatal(err)
	}
	r := decode(t, buf.Bytes())[0]
	if r.Tool != "teams" || r.AgentID != "agent-7" || r.RunID != "run-1" || r.Verb != "inbox" || r.Resource != "channel:sdlc-alerts" ||
		r.Outcome != "ok" || r.HTTPStatus != 200 || r.Duration != "1.5s" || r.SchemaVersion != audit.SchemaVersion || !r.Ts.Equal(t0) {
		t.Fatalf("record = %+v", r)
	}
	want := "allow;count=3;deduplicated=false;dropped=1;message_id=1696341900000;skipped=user:jane.doe:no_chat"
	if r.PolicyDecision != want {
		t.Fatalf("decision = %q\nwant      %q", r.PolicyDecision, want)
	}
}

func TestDenyDecision(t *testing.T) {
	var buf bytes.Buffer
	s := NewWithWriter(&buf, Config{})
	_ = s.Record(context.Background(), domain.AuditEvent{Verb: "send", Resource: "chat:dev", Outcome: "denied",
		Decision: "deny:rate limit exceeded (per_minute)", Extra: map[string]string{"retry_after": "30s"}})
	got := decode(t, buf.Bytes())[0].PolicyDecision
	if got != "deny:rate limit exceeded _per_minute_;retry_after=30s" {
		t.Fatalf("decision = %q", got)
	}
}

func TestFileModesAndAppend(t *testing.T) {
	p := filepath.Join(t.TempDir(), "logs", "teams.audit.jsonl")
	cfg := Config{Path: p, AgentID: "a", RunID: "r"}
	for range 2 {
		s, err := Open(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Record(context.Background(), domain.AuditEvent{Verb: "whoami", Outcome: "ok", Decision: "allow"}); err != nil {
			t.Fatal(err)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
	fi, _ := os.Stat(p)
	di, _ := os.Stat(filepath.Dir(p))
	if fi.Mode().Perm() != 0o600 || di.Mode().Perm() != 0o700 {
		t.Fatalf("modes: file %v dir %v", fi.Mode().Perm(), di.Mode().Perm())
	}
	b, _ := os.ReadFile(p)
	if n := len(decode(t, b)); n != 2 {
		t.Fatalf("records = %d", n)
	}
}

func TestOpenErrors(t *testing.T) {
	if _, err := Open(Config{}); err == nil {
		t.Fatal("empty path must fail")
	}
	blocker := filepath.Join(t.TempDir(), "file")
	_ = os.WriteFile(blocker, nil, 0o600)
	if _, err := Open(Config{Path: filepath.Join(blocker, "x.jsonl")}); err == nil {
		t.Fatal("unwritable path must fail")
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestWriteFailureBlocksWritesWarnsReads(t *testing.T) {
	var warned []error
	s := NewWithWriter(failWriter{}, Config{OnWriteError: func(e error) { warned = append(warned, e) }})
	ctx := context.Background()
	for _, verb := range []string{"send", "reply"} {
		err := s.Record(ctx, domain.AuditEvent{Verb: verb, Decision: "allow"})
		if err == nil || !errors.Is(err, audit.ErrWrite) {
			t.Fatalf("%s: write failure must block, got %v", verb, err)
		}
	}
	if len(warned) != 0 {
		t.Fatal("blocking verbs must not be reported as warnings")
	}
	for _, verb := range []string{"inbox", "ack", "whoami", "thread", "selftest"} {
		if err := s.Record(ctx, domain.AuditEvent{Verb: verb, Decision: "allow"}); err != nil {
			t.Fatalf("%s: read failure must only warn, got %v", verb, err)
		}
	}
	if len(warned) != 5 {
		t.Fatalf("warnings = %d", len(warned))
	}
	// Without a hook the warning is silently dropped, not a panic.
	if err := NewWithWriter(failWriter{}, Config{}).Record(ctx, domain.AuditEvent{Verb: "inbox"}); err != nil {
		t.Fatal(err)
	}
}

func TestCustomBlockVerbs(t *testing.T) {
	s := NewWithWriter(failWriter{}, Config{BlockVerbs: []string{"ack"}})
	if err := s.Record(context.Background(), domain.AuditEvent{Verb: "ack"}); err == nil {
		t.Fatal("ack should block")
	}
	if err := s.Record(context.Background(), domain.AuditEvent{Verb: "send"}); err != nil {
		t.Fatal("send was not configured to block")
	}
}

func TestRecordAfterCloseFails(t *testing.T) {
	s, err := Open(Config{Path: filepath.Join(t.TempDir(), "a.jsonl")})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if err := s.Record(context.Background(), domain.AuditEvent{Verb: "send"}); err == nil {
		t.Fatal("record after close must fail for a blocking verb")
	}
}

const (
	planted  = "tok-SuperSecretValue-0123456789"
	guid     = "5f1c2d3e-4a5b-6c7d-8e9f-a0b1c2d3e4f5"
	threadID = "19:abcDEF0123456789abcDEF0123456789@thread.v2"
	jwt      = "eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiIxMjM0In0.c2lnbmF0dXJl"
)

func TestScrubbing(t *testing.T) {
	var buf bytes.Buffer
	s := NewWithWriter(&buf, Config{AgentID: "agent-" + planted, Secrets: []string{planted}})
	_ = s.Record(context.Background(), domain.AuditEvent{
		Verb: "send", Resource: threadID, Outcome: "error " + jwt,
		Decision: "deny:token " + planted + " for " + guid + " Bearer abcdef",
		Extra: map[string]string{
			"chat":       threadID,
			"user":       guid,
			"body":       "hello team, the deploy password is hunter2 and more words here",
			"message_id": "1696341900000",
			"Bad Key":    "x",
			"UPPER":      "x",
			"secret":     planted,
			"long":       strings.Repeat("a", 70),
		},
	})
	out := buf.String()
	for _, bad := range []string{planted, guid, threadID, jwt, "hunter2", "hello team", "Bearer abcdef", "Bad Key", "UPPER"} {
		if strings.Contains(out, bad) {
			t.Errorf("output leaks %q:\n%s", bad, out)
		}
	}
	for _, want := range []string{"message_id=1696341900000", "body=invalid"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

// TestPropertyNoSecretsInAnyLine plants a token, GUIDs, thread ids, JWTs and
// prose in every free field of many randomly assembled events and asserts that
// none of them reaches the file, that every line is valid JSON, and that
// there is exactly one line per record.
func TestPropertyNoSecretsInAnyLine(t *testing.T) {
	pieces := []string{planted, guid, strings.ToUpper(guid), threadID, "19:x@unq.gbl.spaces", jwt, "Bearer " + planted,
		"channel:sdlc-alerts", "user:jane.doe", "ok", "allow", "deny:policy", "\n", "\r\n", "\x00", "\"", "\\", ";", "=",
		"hello", " ", "é", strings.Repeat("Z", 60)}
	rng := rand.New(rand.NewPCG(1, 2))
	gen := func() string {
		var b strings.Builder
		for range rng.IntN(6) {
			b.WriteString(pieces[rng.IntN(len(pieces))])
		}
		return b.String()
	}
	var buf bytes.Buffer
	s := NewWithWriter(&buf, Config{AgentID: gen(), RunID: gen(), Secrets: []string{planted}})
	const n = 500
	for range n {
		ev := domain.AuditEvent{Verb: gen(), Resource: gen(), Outcome: gen(), Decision: gen(), HTTPStatus: rng.IntN(600),
			Extra: map[string]string{gen(): gen(), "count": gen(), "message_id": gen()}}
		_ = s.Record(context.Background(), ev)
	}
	checkLines(t, buf.Bytes(), n)
}

func checkLines(t *testing.T, out []byte, wantLines int) {
	t.Helper()
	lines := bytes.Split(bytes.TrimRight(out, "\n"), []byte("\n"))
	if len(lines) != wantLines {
		t.Fatalf("lines = %d, want %d", len(lines), wantLines)
	}
	for _, l := range lines {
		var m map[string]any
		if err := json.Unmarshal(l, &m); err != nil {
			t.Fatalf("not JSON: %q", l)
		}
		for _, bad := range []string{planted, guid, strings.ToUpper(guid), threadID, "@unq.gbl.spaces", "eyJ", "Bearer"} {
			if bytes.Contains(l, []byte(bad)) {
				t.Fatalf("line leaks %q: %s", bad, l)
			}
		}
	}
}

func FuzzNoSecretsInLine(f *testing.F) {
	f.Add("send", "chat:dev", "ok", "allow", "count", "3")
	f.Add(planted, guid, threadID, jwt, planted, "Bearer "+planted)
	f.Add("a\nb", "c\x00d", "\"", "\\", "k;e=y", "v;x=y")
	f.Fuzz(func(t *testing.T, verb, res, out, dec, k, v string) {
		var buf bytes.Buffer
		s := NewWithWriter(&buf, Config{Secrets: []string{planted}})
		_ = s.Record(context.Background(), domain.AuditEvent{Verb: verb, Resource: res, Outcome: out, Decision: dec,
			Extra: map[string]string{k: v, "message_id": v}})
		if buf.Len() == 0 {
			return // write refused or dropped: nothing leaked
		}
		line := bytes.TrimRight(buf.Bytes(), "\n")
		if bytes.ContainsRune(line, '\n') {
			t.Fatalf("record spans lines: %q", line)
		}
		var m map[string]any
		if err := json.Unmarshal(line, &m); err != nil {
			t.Fatalf("not JSON: %v", err)
		}
		for _, field := range []string{"verb", "resource", "outcome", "policy_decision"} {
			sv, _ := m[field].(string)
			// Only seeds that could contain the planted values are checked
			// for presence; fuzz-generated text cannot know the token, so
			// assert the structural invariants instead.
			if strings.ContainsAny(sv, "\n\r\x00") {
				t.Fatalf("%s has control characters: %q", field, sv)
			}
			if strings.Count(sv, "=") > 2 && field != "policy_decision" {
				t.Fatalf("%s looks like folded fields: %q", field, sv)
			}
		}
		pd, _ := m["policy_decision"].(string)
		for _, part := range strings.Split(pd, ";")[1:] {
			if !strings.Contains(part, "=") {
				t.Fatalf("malformed suffix %q in %q", part, pd)
			}
		}
		for _, bad := range []string{planted, guid, threadID, jwt} {
			if strings.Contains(string(line), bad) {
				t.Fatalf("line leaks %q", bad)
			}
		}
	})
}

// Regression: valid aliases with long names must not be turned into
// "channel:[redacted]" (by this package or by the core's opaque-run redaction).
// Names that reach the 40 character threshold are written in a short stable
// form; shorter ones are verbatim.
func TestLongAliasResources(t *testing.T) {
	long64 := "channel:" + strings.Repeat("a", 64)
	record := func(res string) rec {
		var buf bytes.Buffer
		_ = NewWithWriter(&buf, Config{}).Record(context.Background(), domain.AuditEvent{Verb: "inbox", Resource: res, Outcome: "ok", Decision: "allow",
			Extra: map[string]string{"skipped": res}})
		return decode(t, buf.Bytes())[0]
	}
	for _, res := range []string{
		"channel:platform-engineering-alerts/1696341900000", // 41 chars after the colon, but runs are split by "/" and ":"
		"channel:" + strings.Repeat("a", 39),
		"user:" + strings.Repeat("ab.", 15),
	} {
		if r := record(res); r.Resource != res || !strings.Contains(r.PolicyDecision, "skipped="+res) {
			t.Errorf("resource %q mangled: %+v", res, r)
		}
	}
	for _, res := range []string{long64, long64 + "/1696341900000", "user:" + strings.Repeat("b-c", 21)} {
		r := record(res)
		if strings.Contains(r.Resource, "redacted") || strings.Contains(r.Resource, "invalid") || !strings.Contains(r.Resource, "~") ||
			len(r.Resource) > 64 || strings.Contains(r.PolicyDecision, "invalid") {
			t.Errorf("long alias %q: %+v", res, r)
		}
		if r.Resource != record(res).Resource {
			t.Errorf("short form must be stable")
		}
		if r.Resource == record(res+"x").Resource {
			t.Errorf("distinct aliases must stay distinguishable")
		}
	}
	// A bare long opaque run (token-like) is still redacted.
	var buf bytes.Buffer
	_ = NewWithWriter(&buf, Config{}).Record(context.Background(), domain.AuditEvent{Verb: "inbox", Resource: strings.Repeat("Q", 50), Decision: "allow"})
	if decode(t, buf.Bytes())[0].Resource != "[redacted]" {
		t.Error("opaque run not redacted")
	}
}
