package policyfile

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stainedhead/agent-cli-core/output"

	"github.com/stainedhead/teams-cli/internal/domain"
)

const samplePath = "testdata/teams.policy.sample.yaml"

func sample(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(samplePath)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func mustParse(t *testing.T, s string) domain.Policy {
	t.Helper()
	p, err := Parse([]byte(s))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return p
}

func TestSampleGolden(t *testing.T) {
	p := mustParse(t, sample(t))
	jane := domain.Destination{
		Alias: "user:jane.doe", Kind: domain.KindUser, AADID: "33333333-3333-3333-3333-333333333333",
		DisplayName: "Jane Doe", Send: true, Watch: true,
	}
	want := domain.Policy{
		Version: 1, Profile: "agent", UPN: "sdlc-reviewer-01@corp.example.com",
		TenantID: "11111111-1111-1111-1111-111111111111", StateDir: "/var/lib/agent-cli/teams", StateDirPinned: true,
		Destinations: map[domain.Alias]domain.Destination{
			"channel:sdlc-alerts": {
				Alias: "channel:sdlc-alerts", Kind: domain.KindChannel,
				TeamID: "22222222-2222-2222-2222-222222222222", ChannelID: "19:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa@thread.tacv2",
				Send: true, Watch: true,
			},
			"chat:dev-team": {
				Alias: "chat:dev-team", Kind: domain.KindChat,
				ChatID: "19:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb@thread.v2", Send: true, Watch: true,
			},
			"user:jane.doe": jane,
		},
		Instruct: domain.Instruct{
			Commanders: []domain.AADID{"33333333-3333-3333-3333-333333333333"},
			Agents:     []domain.AADID{"44444444-4444-4444-4444-444444444444"},
		},
		Inbound: domain.Inbound{
			Handle:      []domain.InboundHandle{domain.HandleDirect, domain.HandleMentions, domain.HandleWatched},
			MaxLookback: 30 * time.Minute, PollInterval: 15 * time.Second, MaxWait: 120 * time.Second, ThreadPollMax: 5,
		},
		Send: domain.SendPolicy{
			MaxBytes:              8000,
			Mentions:              domain.MentionPolicy{Allow: []domain.Alias{"user:jane.doe"}, BlockBroadcast: true, Max: 5},
			ContentFilters:        []string{domain.FilterSecretPatterns, domain.FilterClassificationMarkers},
			ClassificationMarkers: []string{"CONFIDENTIAL", "INTERNAL ONLY"},
			Rate:                  domain.Rate{PerMinute: 10, PerHour: 100},
			ReplyDepthMax:         6, ReplyWindow: 24 * time.Hour,
		},
		Limits:   domain.Limits{MaxResults: 50, MaxWritesPerRun: 30, MaxChatScan: 50},
		Audit:    domain.AuditCfg{Path: "/var/log/agent-cli/teams.audit.jsonl"},
		Selftest: domain.SelftestCfg{NonMemberChatID: "19:cccccccccccccccccccccccccccccccc@thread.v2"},
	}
	if !reflect.DeepEqual(p, want) {
		t.Fatalf("sample parsed differently:\n got %+v\nwant %+v", p, want)
	}
}

// minimal is the smallest valid policy: every optional value falls back to
// its documented default.
const minimal = `
version: 1
profile: agent
upn: bot@corp.example.com
destinations:
  chat:dev:
    chat_id: "19:x@thread.v2"
    watch: true
audit:
  path: /var/log/agent-cli/teams.audit.jsonl
`

func TestDefaultsApplied(t *testing.T) {
	p := mustParse(t, minimal)
	if p.StateDir != "/var/lib/agent-cli/teams" {
		t.Errorf("state_dir = %q", p.StateDir)
	}
	in := p.Inbound
	if !reflect.DeepEqual(in.Handle, []domain.InboundHandle{"direct", "mentions", "watched"}) ||
		in.MaxLookback != 30*time.Minute || in.PollInterval != 15*time.Second || in.MaxWait != 120*time.Second || in.ThreadPollMax != 5 {
		t.Errorf("inbound defaults: %+v", in)
	}
	s := p.Send
	if s.MaxBytes != 8000 || s.Mentions.Max != 5 || !s.Mentions.BlockBroadcast || s.Rate != (domain.Rate{PerMinute: 10, PerHour: 100}) ||
		s.ReplyDepthMax != 6 || s.ReplyWindow != 24*time.Hour || s.MarkerScan {
		t.Errorf("send defaults: %+v", s)
	}
	if p.Limits != (domain.Limits{MaxResults: 50, MaxWritesPerRun: 30, MaxChatScan: 50}) {
		t.Errorf("limits defaults: %+v", p.Limits)
	}
	if d := p.Destinations["chat:dev"]; d.Send || !d.Watch {
		t.Errorf("send must default false: %+v", d)
	}
}

func TestExplicitZeroWritesAllowed(t *testing.T) {
	p := mustParse(t, minimal+"limits:\n  max_writes_per_run: 0\n")
	if p.Limits.MaxWritesPerRun != 0 {
		t.Fatalf("explicit 0 writes must be kept, got %d", p.Limits.MaxWritesPerRun)
	}
}

func TestNormalization(t *testing.T) {
	s := strings.NewReplacer(
		"33333333-3333-3333-3333-333333333333", "AAAAAAAA-AAAA-AAAA-AAAA-AAAAAAAAAAAA",
		"link_allowlist: []", "link_allowlist: [Example.COM, \"*.Corp.Example.com\"]",
	).Replace(sample(t))
	p := mustParse(t, s)
	if got := p.Instruct.Commanders[0]; got != "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa" {
		t.Errorf("commander not lowercased: %s", got)
	}
	if p.Destinations["user:jane.doe"].AADID != "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa" {
		t.Error("aad_id not lowercased")
	}
	if !reflect.DeepEqual(p.Send.LinkAllowlist, []string{"example.com", "*.corp.example.com"}) {
		t.Errorf("link allowlist: %v", p.Send.LinkAllowlist)
	}
}

func TestBoundaryValuesAccepted(t *testing.T) {
	s := sample(t)
	s = strings.Replace(s, "max_lookback: 30m ", "max_lookback: 24h ", 1)
	s = strings.Replace(s, "poll_interval: 15s ", "poll_interval: 5s  ", 1)
	p := mustParse(t, s)
	if p.Inbound.MaxLookback != 24*time.Hour || p.Inbound.PollInterval != 5*time.Second {
		t.Fatalf("%+v", p.Inbound)
	}
}

func TestBlockBroadcastTrueAccepted(t *testing.T) {
	mustParse(t, strings.Replace(sample(t), "block_broadcast: true", "block_broadcast: true", 1))
}

// TestInvalidFiles is the AC-15 table: every entry must be rejected with a
// validation error (exit 9) and no policy.
func TestInvalidFiles(t *testing.T) {
	r := func(old, new string) func(string) string {
		return func(s string) string {
			if !strings.Contains(s, old) {
				panic("mutation target missing: " + old)
			}
			return strings.Replace(s, old, new, 1)
		}
	}
	const id = "33333333-3333-3333-3333-333333333333"
	tests := []struct {
		name string
		mut  func(string) string
		want string // substring of the message
	}{
		{"empty file", func(string) string { return "" }, "empty"},
		{"whitespace only", func(string) string { return " \n\t\n" }, "empty"},
		{"not yaml mapping", func(string) string { return "- a\n- b\n" }, ""},
		{"unknown top-level key", r("profile: agent", "profile: agent\nsurprise: 1"), ""},
		{"unknown nested key", r("block_broadcast: true", "block_broadcast: true\n    color: red"), ""},
		{"unknown destination key", r("send: true\n    watch: true\n    create_chat: false", "send: true\n    watch: true\n    bogus: 1"), ""},
		{"duplicate top-level key", r("profile: agent", "profile: agent\nprofile: other"), "duplicate"},
		{"duplicate destination alias", r("  chat:dev-team:", "  chat:dev-team:\n    chat_id: x\n  chat:dev-team:"), ""},
		{"two documents", func(s string) string { return s + "\n---\nversion: 1\n" }, "one YAML document"},
		{"version missing", r("version: 1\n", ""), "version is required"},
		{"version 2", r("version: 1", "version: 2"), "version must be 1"},
		{"profile missing", r("profile: agent\n", ""), "profile is required"},
		{"upn missing", r("upn: sdlc-reviewer-01@corp.example.com", ""), "upn is required"},
		{"upn malformed", r("sdlc-reviewer-01@corp.example.com", "not-an-upn"), "upn must look like"},
		{"tenant_id not guid", r("tenant_id: 11111111-1111-1111-1111-111111111111", "tenant_id: nope"), "tenant_id"},
		{"state_dir relative", r("state_dir: /var/lib/agent-cli/teams", "state_dir: state"), "state_dir"},
		{"audit path missing", r("  path: /var/log/agent-cli/teams.audit.jsonl", "  path: \"\""), "audit.path is required"},
		{"audit path relative", r("path: /var/log/agent-cli/teams.audit.jsonl", "path: audit.jsonl"), "audit.path must be an absolute"},
		{"destinations empty", func(string) string { return strings.Split(minimal, "destinations:")[0] + "audit:\n  path: /a/b\n" }, "at least one destination"},
		{"alias raw thread id", r("chat:dev-team:", "\"19:abc@thread.v2\":"), "invalid alias"},
		{"alias uppercase", r("channel:sdlc-alerts:", "channel:SDLC:"), "invalid alias"},
		{"alias unknown kind", r("chat:dev-team:", "group:dev-team:"), "invalid alias"},
		{"alias name too long", r("chat:dev-team:", "chat:"+strings.Repeat("a", 65)+":"), "invalid alias"},
		{"channel missing team_id", r("    team_id: 22222222-2222-2222-2222-222222222222\n", ""), "requires team_id"},
		{"channel missing channel_id", r("    channel_id: \"19:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa@thread.tacv2\"\n", ""), "requires channel_id"},
		{"channel with chat_id", r("channel_id:", "chat_id: x\n    channel_id:"), "must not set chat_id"},
		{"chat missing chat_id", r("    chat_id: \"19:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb@thread.v2\"\n", ""), "requires chat_id"},
		{"chat with team_id", r("  chat:dev-team:\n", "  chat:dev-team:\n    team_id: t\n"), "must not set team_id"},
		{"chat with create_chat", r("  chat:dev-team:\n", "  chat:dev-team:\n    create_chat: true\n"), "must not set create_chat"},
		{"user missing aad_id", r("    aad_id: "+id+"\n    display_name", "    display_name"), "requires aad_id"},
		{"user aad_id not guid", r("aad_id: "+id+"\n    display_name", "aad_id: jane\n    display_name"), "aad_id must be a GUID"},
		{"user with chat_id", r("    display_name: Jane Doe", "    chat_id: x\n    display_name: Jane Doe"), "must not set chat_id"},
		{"display name control char", r("display_name: Jane Doe", "display_name: \"Jane\\nDoe\""), "display_name"},
		{"commander not guid", r("      - "+id, "      - jane"), "commanders"},
		{"commander duplicate", r("      - "+id, "      - "+id+"\n      - "+strings.ToUpper(id)), "duplicate"},
		{"agent not guid", r("      - 44444444-4444-4444-4444-444444444444", "      - 4444"), "agents"},
		{"handle unknown", r("[direct, mentions, watched]", "[direct, everything]"), "unknown value"},
		{"handle duplicate", r("[direct, mentions, watched]", "[direct, direct]"), "duplicate"},
		{"lookback over cap", r("max_lookback: 30m", "max_lookback: 25h"), "max_lookback"},
		{"lookback negative", r("max_lookback: 30m", "max_lookback: -5m"), "max_lookback"},
		{"lookback not duration", r("max_lookback: 30m", "max_lookback: soon"), ""},
		{"poll below floor", r("poll_interval: 15s", "poll_interval: 4s"), "poll_interval"},
		{"max_wait negative", r("max_wait: 120s", "max_wait: -1s"), "max_wait"},
		{"thread_poll_max negative", r("thread_poll_max: 5", "thread_poll_max: -1"), "thread_poll_max"},
		{"block_broadcast false", r("block_broadcast: true", "block_broadcast: false"), "block_broadcast"},
		{"mention non-user alias", r("allow: [user:jane.doe]", "allow: [chat:dev-team]"), "user: aliases"},
		{"mention unlisted", r("allow: [user:jane.doe]", "allow: [user:nobody]"), "not a destination"},
		{"mention without display_name", r("    display_name: Jane Doe\n", ""), "display_name"},
		{"mention duplicate", r("allow: [user:jane.doe]", "allow: [user:jane.doe, user:jane.doe]"), "duplicate"},
		{"mentions max negative", r("    max: 5", "    max: -1"), "send.mentions.max"},
		{"max_bytes negative", r("max_bytes: 8000", "max_bytes: -1"), "max_bytes"},
		{"unknown filter", r("[secret_patterns, classification_markers]", "[pii]"), "unknown filter"},
		{"duplicate filter", r("[secret_patterns, classification_markers]", "[secret_patterns, secret_patterns]"), "duplicate filter"},
		{"empty marker", r("[\"CONFIDENTIAL\", \"INTERNAL ONLY\"]", "[\"\"]"), "classification_markers"},
		{"link allowlist with scheme", r("link_allowlist: []", "link_allowlist: [\"https://ok.com\"]"), "link_allowlist"},
		{"link allowlist with path", r("link_allowlist: []", "link_allowlist: [\"ok.com/x\"]"), "link_allowlist"},
		{"link allowlist userinfo", r("link_allowlist: []", "link_allowlist: [\"ok.com@evil.com\"]"), "link_allowlist"},
		{"rate negative", r("per_minute: 10", "per_minute: -1"), "per_minute"},
		{"reply window negative", r("reply_window: 24h", "reply_window: -1h"), "reply_window"},
		{"reply depth negative", r("reply_depth_max: 6", "reply_depth_max: -1"), "reply_depth_max"},
		{"prefix with newline", r("prefix: \"\"", "prefix: \"a\\nb\""), "prefix"},
		{"max_results negative", r("max_results: 50", "max_results: -1"), "max_results"},
		{"max_writes negative", r("max_writes_per_run: 30", "max_writes_per_run: -1"), "max_writes_per_run"},
		{"max_chat_scan negative", r("max_chat_scan: 50", "max_chat_scan: -1"), "max_chat_scan"},
		{"non_member_chat_id whitespace", r("non_member_chat_id: \"19:cccccccccccccccccccccccccccccccc@thread.v2\"", "non_member_chat_id: \"a b\""), "non_member_chat_id"},
	}
	if len(tests) < 25 {
		t.Fatalf("need >= 25 invalid files, have %d", len(tests))
	}
	base := sample(t)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, err := Parse([]byte(tc.mut(base)))
			if err == nil {
				t.Fatalf("accepted an invalid policy: %+v", p)
			}
			if output.ExitOf(err) != 9 {
				t.Fatalf("want exit 9 (validation), got %d: %v", output.ExitOf(err), err)
			}
			if tc.want != "" && !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
			if !reflect.DeepEqual(p, domain.Policy{}) {
				t.Fatal("a failed parse must return the zero policy")
			}
		})
	}
}

func TestErrorsNeverEchoValues(t *testing.T) {
	// The message names keys, not the offending value.
	s := strings.Replace(sample(t), "tenant_id: 11111111-1111-1111-1111-111111111111", "tenant_id: hunter2-secret", 1)
	_, err := Parse([]byte(s))
	if err == nil || strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("value leaked or accepted: %v", err)
	}
}

func TestSampleStillMatchesNothingElse(t *testing.T) {
	// Guard the table above: the unmutated sample must itself be valid.
	mustParse(t, sample(t))
}

func TestLoadSizeLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.yaml")
	if err := os.WriteFile(path, []byte(strings.Repeat("# x\n", maxPolicyBytes/4+10)), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path, AllowUntrusted())
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("got %v", err)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope.yaml"), AllowUntrusted())
	if err == nil || output.ExitOf(err) != 9 {
		t.Fatalf("got %v", err)
	}
}

func TestProviderCachesAndIsolates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.yaml")
	if err := os.WriteFile(path, []byte(sample(t)), 0o600); err != nil {
		t.Fatal(err)
	}
	pv := NewProvider(path, AllowUntrusted())
	p1, err := pv.Policy(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Edit the file and mutate the returned copy: neither may affect later calls.
	if err := os.WriteFile(path, []byte("garbage: ["), 0o600); err != nil {
		t.Fatal(err)
	}
	p1.Destinations["chat:evil"] = domain.Destination{Send: true}
	p1.Instruct.Commanders[0] = "evil"
	p1.Send.Mentions.Allow[0] = "user:evil"
	p1.Inbound.Handle[0] = "evil"
	p1.Send.ContentFilters[0] = "evil"
	p1.Send.ClassificationMarkers[0] = "evil"
	p2, err := pv.Policy(context.Background())
	if err != nil {
		t.Fatalf("cached policy must survive file edits: %v", err)
	}
	if _, bad := p2.Destinations["chat:evil"]; bad || p2.Instruct.Commanders[0] == "evil" || p2.Send.Mentions.Allow[0] == "user:evil" ||
		p2.Inbound.Handle[0] == "evil" || p2.Send.ContentFilters[0] == "evil" || p2.Send.ClassificationMarkers[0] == "evil" {
		t.Fatal("provider returned shared state")
	}
}

func TestProviderCachesFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.yaml")
	pv := NewProvider(path, AllowUntrusted())
	if _, err := pv.Policy(context.Background()); err == nil {
		t.Fatal("missing file must fail")
	}
	if err := os.WriteFile(path, []byte(sample(t)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := pv.Policy(context.Background()); err == nil {
		t.Fatal("failure must stay cached for the run (fail closed)")
	}
}

func TestProviderCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewProvider("/nonexistent").Policy(ctx); err == nil {
		t.Fatal("want ctx error")
	}
}
