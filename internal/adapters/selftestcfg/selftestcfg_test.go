package selftestcfg

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/agent-cli-core/selftest"

	"github.com/stainedhead/teams-cli/internal/domain"
	"github.com/stainedhead/teams-cli/internal/usecase"
)

func testPolicy() domain.Policy {
	return domain.Policy{
		UPN: "bot@x.com",
		Destinations: map[domain.Alias]domain.Destination{
			"channel:alerts": {Alias: "channel:alerts", Kind: domain.KindChannel, Send: true, Watch: true},
			"chat:dev":       {Alias: "chat:dev", Kind: domain.KindChat, Send: true},
			"user:jane":      {Alias: "user:jane", Kind: domain.KindUser, AADID: "33333333-3333-3333-3333-333333333333", Send: true, Watch: true},
		},
		Send: domain.SendPolicy{
			MaxBytes:              100,
			Mentions:              domain.MentionPolicy{Allow: []domain.Alias{"user:jane"}, BlockBroadcast: true, Max: 5},
			ContentFilters:        []string{domain.FilterSecretPatterns, domain.FilterClassificationMarkers},
			ClassificationMarkers: []string{"CONFIDENTIAL"},
		},
		Selftest: domain.SelftestCfg{NonMemberChatID: "19:other@thread.v2"},
	}
}

// sim is an independent fake of the use cases: it denies exactly what the
// policy forbids. Methods not overridden panic (nil embedded interface).
type sim struct {
	usecase.Commands
	p     domain.Policy
	skip  map[string]bool // checks the broken variant forgets
	calls []usecase.SendRequest
	real  int
	inbox []usecase.InboxRequest
	who   error
}

func deny() error { return domain.NewPolicyDenied("denied", "") }

func (s *sim) Whoami(context.Context) (usecase.WhoamiResult, error) {
	return usecase.WhoamiResult{}, s.who
}

func (s *sim) Inbox(_ context.Context, r usecase.InboxRequest) (usecase.InboxResult, error) {
	s.inbox = append(s.inbox, r)
	return usecase.InboxResult{}, nil
}

func (s *sim) Send(_ context.Context, r usecase.SendRequest) (usecase.SendResult, error) {
	s.calls = append(s.calls, r)
	if !r.DryRun {
		s.real++
	}
	d, ok := s.p.Destinations[r.Alias]
	if !ok || !d.Send {
		return usecase.SendResult{}, deny()
	}
	for _, m := range r.Mentions {
		if !s.skip["mention"] && (m.Kind() != domain.KindUser || !contains(s.p.Send.Mentions.Allow, m)) {
			return usecase.SendResult{}, deny()
		}
	}
	if !s.skip["size"] && len(r.Text) > s.p.Send.MaxBytes {
		return usecase.SendResult{}, domain.NewValidation("too big", "")
	}
	if !s.skip["secret"] && strings.Contains(r.Text, "AKIA") {
		return usecase.SendResult{}, deny()
	}
	if !s.skip["marker"] && strings.Contains(r.Text, "CONFIDENTIAL") {
		return usecase.SendResult{}, deny()
	}
	return usecase.SendResult{DryRun: r.DryRun}, nil
}

func contains(as []domain.Alias, a domain.Alias) bool {
	for _, x := range as {
		if x == a {
			return true
		}
	}
	return false
}

type simGraph struct {
	usecase.Graph
	chatErr, resolveErr error
	resolved            []string
}

func (g *simGraph) GetChat(context.Context, string) error { return g.chatErr }

func (g *simGraph) ResolveUserChat(_ context.Context, id string, create bool) (string, error) {
	g.resolved = append(g.resolved, id)
	if create {
		panic("selftest must never create chats")
	}
	return "chat", g.resolveErr
}

func run(t *testing.T, s *sim, g *simGraph, readOnly bool) selftest.Result {
	t.Helper()
	res, err := Runner(Deps{Cmds: s, Graph: g, Policy: s.p, RunID: "run1"}, readOnly).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func status(r selftest.Result, name string) selftest.RowResult {
	for _, row := range r.Rows {
		if row.Name == name {
			return row
		}
	}
	return selftest.RowResult{}
}

func TestAllRowsPassAgainstCorrectUseCases(t *testing.T) {
	p := testPolicy()
	s := &sim{p: p}
	g := &simGraph{chatErr: errCat("forbidden")}
	res := run(t, s, g, false)
	if !res.OK() || res.Passed != len(Rows(p)) || len(res.Rows) != 11 {
		t.Fatalf("result: %+v", res)
	}
	if s.real != 1 {
		t.Fatalf("exactly one real send expected, got %d", s.real)
	}
	last := s.calls[0]
	if last.Alias != "channel:alerts" || last.Text != "selftest run1" || last.IdempotencyKey != "selftest-run1" {
		t.Fatalf("send-allowed request: %+v", last)
	}
	if len(g.resolved) != 1 || g.resolved[0] != "33333333-3333-3333-3333-333333333333" {
		t.Fatalf("resolve: %v", g.resolved)
	}
	if len(s.inbox) != 1 || s.inbox[0].Alias != "channel:alerts" || s.inbox[0].Limit != 1 {
		t.Fatalf("inbox probe: %+v", s.inbox)
	}
}

func TestReadOnlySkipsOnlySendAllowed(t *testing.T) {
	s := &sim{p: testPolicy()}
	res := run(t, s, &simGraph{chatErr: errCat("not_found")}, true)
	if res.Skipped != 1 || status(res, RowSendAllowed).Status != selftest.StatusSkip || !res.OK() {
		t.Fatalf("%+v", res)
	}
	if s.real != 0 {
		t.Fatal("read-only run must not post")
	}
}

func TestBrokenPolicyFailsTheRightRow(t *testing.T) {
	for skip, row := range map[string]string{
		"mention": RowBroadcast, "size": RowOversize, "secret": RowSecret, "marker": RowClassification,
	} {
		t.Run(skip, func(t *testing.T) {
			s := &sim{p: testPolicy(), skip: map[string]bool{skip: true}}
			res := run(t, s, &simGraph{chatErr: errCat("forbidden")}, true)
			if status(res, row).Status != selftest.StatusFail {
				t.Fatalf("row %s should fail: %+v", row, res)
			}
			if skip == "mention" && status(res, RowMentionUnlist).Status != selftest.StatusFail {
				t.Fatal("mention-unlisted should fail too")
			}
			if res.ExitCode() != 1 {
				t.Fatalf("exit %d", res.ExitCode())
			}
		})
	}
}

func TestNegativeMembershipMemberFails(t *testing.T) {
	res := run(t, &sim{p: testPolicy()}, &simGraph{}, true) // GetChat succeeds => agent IS a member
	if status(res, RowNegative).Status != selftest.StatusFail {
		t.Fatalf("%+v", res)
	}
}

func TestBackendErrorsAreNotPolicyDecisions(t *testing.T) {
	boom := errors.New("graph down")
	res := run(t, &sim{p: testPolicy(), who: boom}, &simGraph{chatErr: boom, resolveErr: boom}, true)
	for _, n := range []string{RowIdentity, RowNegative, RowUserChat} {
		r := status(res, n)
		if r.Status != selftest.StatusFail || !strings.Contains(r.Detail, "graph down") {
			t.Errorf("%s: %+v", n, r)
		}
	}
	// A UPN mismatch (policy_denied) on identity is a failed Allow expectation.
	res = run(t, &sim{p: testPolicy(), who: deny()}, &simGraph{chatErr: errCat("forbidden")}, true)
	if r := status(res, RowIdentity); r.Status != selftest.StatusFail || r.Actual != selftest.Deny {
		t.Fatalf("%+v", r)
	}
}

func TestRowsAreDerivedFromPolicy(t *testing.T) {
	p := testPolicy()
	p.Send.ClassificationMarkers = nil
	p.Send.ContentFilters = nil
	p.Selftest.NonMemberChatID = ""
	p.Destinations = map[domain.Alias]domain.Destination{"chat:dev": {Alias: "chat:dev", Kind: domain.KindChat, Send: true}}
	var names []string
	for _, r := range Rows(p) {
		names = append(names, r.Name)
	}
	want := []string{RowIdentity, RowSendAllowed, RowSendUnlisted, RowBroadcast, RowOversize, RowMentionUnlist}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("rows = %v", names)
	}
	// Nothing sendable: only policy-only and identity rows remain.
	p.Destinations = map[domain.Alias]domain.Destination{"chat:dev": {Alias: "chat:dev", Kind: domain.KindChat, Watch: true}}
	names = nil
	for _, r := range Rows(p) {
		names = append(names, r.Name)
	}
	if strings.Join(names, ",") != RowIdentity+","+RowSendUnlisted+","+RowInboxRead {
		t.Fatalf("rows = %v", names)
	}
}

func TestReadOnlyFlagsMatchSpec(t *testing.T) {
	for _, r := range Rows(testPolicy()) {
		if r.ReadOnly == (r.Name == RowSendAllowed) {
			t.Errorf("row %s read-only = %v", r.Name, r.ReadOnly)
		}
	}
}

func TestUnknownRowIsProbeError(t *testing.T) {
	_, err := Probe(Deps{Policy: testPolicy()})(context.Background(), selftest.Row{Name: "nope"})
	if err == nil {
		t.Fatal("want error")
	}
}

func TestRowsDeterministic(t *testing.T) {
	a, b := Rows(testPolicy()), Rows(testPolicy())
	for i := range a {
		if a[i] != b[i] {
			t.Fatal("non-deterministic")
		}
	}
}

// errCat returns an error carrying a core category (forbidden/not_found are
// produced by the Graph adapter in production).
func errCat(cat string) error {
	if cat == "not_found" {
		return domain.NewNotFound("gone", "")
	}
	return categoryErr{cat}
}

type categoryErr struct{ c string }

func (e categoryErr) Error() string             { return e.c }
func (e categoryErr) Category() output.Category { return output.Category(e.c) }
