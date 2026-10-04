package usecase_test

import (
	"strings"
	"testing"

	"github.com/stainedhead/agent-cli-core/output"

	"github.com/stainedhead/teams-cli/internal/domain"
	"github.com/stainedhead/teams-cli/internal/usecase"
	"github.com/stainedhead/teams-cli/internal/usecase/usecasetest"
)

func rowMap(res usecase.SelftestResult) map[string]usecase.SelftestRow {
	m := map[string]usecase.SelftestRow{}
	for _, r := range res.Rows {
		m[r.Name] = r
	}
	return m
}

var allRows = []string{"identity", "send-allowed", "send-unlisted", "broadcast-mention", "oversize", "secret-pattern",
	"classification", "mention-unlisted", "user-chat-resolve", "negative-membership", "inbox-read"}

func selftestEnv(t *testing.T, mod func(*domain.Policy)) *usecasetest.Env {
	return envWith(t, func(p *domain.Policy) {
		p.Selftest.NonMemberChatID = "19:stranger@thread.v2"
		p.Destinations["user:jane"] = withChat(p.Destinations["user:jane"])
		if mod != nil {
			mod(p)
		}
	})
}

func withChat(d domain.Destination) domain.Destination { d.CreateChat = true; return d }

func requireRows(t *testing.T, res usecase.SelftestResult) map[string]usecase.SelftestRow {
	t.Helper()
	if len(res.Rows) != len(allRows) {
		t.Fatalf("rows = %+v", res.Rows)
	}
	for i, n := range allRows {
		if res.Rows[i].Name != n {
			t.Fatalf("row %d = %s, want %s", i, res.Rows[i].Name, n)
		}
	}
	return rowMap(res)
}

func wantStatus(t *testing.T, rows map[string]usecase.SelftestRow, want map[string]string) {
	t.Helper()
	for n, st := range want {
		if rows[n].Status != st {
			t.Errorf("row %s = %s (%s), want %s", n, rows[n].Status, rows[n].Detail, st)
		}
	}
}

// B12 (AC-17): the matrix passes on the fake; send-allowed posts a marked message.
func TestSelftestAllPass(t *testing.T) {
	e := selftestEnv(t, nil)
	e.Graph.ChatErr = map[string]error{"19:stranger@thread.v2": domain.NewNotFound("not a member", "")}
	res, err := e.Svc.Selftest(ctx, usecase.SelftestRequest{})
	wantOK(t, err)
	rows := requireRows(t, res)
	for _, r := range res.Rows {
		if r.Status != "pass" {
			t.Errorf("row %s = %s (%s)", r.Name, r.Status, r.Detail)
		}
	}
	if len(e.Graph.Posts) != 1 || e.Graph.Posts[0].Msg.Text != "selftest run-1" {
		t.Fatalf("posts = %+v", e.Graph.Posts)
	}
	_ = rows
	if ev := lastEvent(t, e); ev.Verb != "selftest" || ev.Outcome != "ok" {
		t.Fatalf("audit = %+v", ev)
	}
	if len(e.Cursors.Dropped) != 0 || len(e.Cursors.States) != 0 {
		t.Fatalf("inbox-read changed state: %+v", e.Cursors.States)
	}
}

func TestSelftestReadOnlySkipsOnlySendAllowed(t *testing.T) {
	e := selftestEnv(t, nil)
	e.Graph.UserChats = map[string]string{usecasetest.BobID: "bc", usecasetest.JaneID: "jc"}
	e.Graph.ChatErr = map[string]error{"19:stranger@thread.v2": domain.NewNotFound("x", "")}
	res, err := e.Svc.Selftest(ctx, usecase.SelftestRequest{ReadOnly: true})
	wantOK(t, err)
	rows := requireRows(t, res)
	skipped := 0
	for _, r := range res.Rows {
		if r.Status == "skip" {
			skipped++
		}
	}
	if rows["send-allowed"].Status != "skip" || skipped != 1 || graphWrites(e) != 0 {
		t.Fatalf("rows = %+v", res.Rows)
	}
}

func TestNegativeMembershipForbiddenPassesMemberFails(t *testing.T) {
	for _, c := range []struct {
		err  error
		want string
	}{
		{domain.NewNotFound("x", ""), "pass"},
		{nil, "fail"},
		{errAmbiguous, "fail"},
	} {
		e := selftestEnv(t, nil)
		e.Graph.ChatErr = map[string]error{"19:stranger@thread.v2": c.err}
		res, _ := e.Svc.Selftest(ctx, usecase.SelftestRequest{ReadOnly: true})
		wantStatus(t, rowMap(res), map[string]string{"negative-membership": c.want})
	}
	e := selftestEnv(t, nil)
	e.Graph.Fail = map[string]error{"GetChat": &forbidden{}}
	res, _ := e.Svc.Selftest(ctx, usecase.SelftestRequest{ReadOnly: true})
	wantStatus(t, rowMap(res), map[string]string{"negative-membership": "pass"})
}

type forbidden struct{}

func (*forbidden) Error() string             { return "forbidden" }
func (*forbidden) Category() output.Category { return output.CategoryForbidden }

// B12: deliberately broken policies fail the right row.
func TestSelftestBrokenPolicies(t *testing.T) {
	cases := []struct {
		name string
		mod  func(*domain.Policy)
		row  string
		want string
	}{
		{"no secret filter", func(p *domain.Policy) { p.Send.ContentFilters = []string{domain.FilterClassificationMarkers} }, "secret-pattern", "fail"},
		{"no marker filter", func(p *domain.Policy) { p.Send.ContentFilters = []string{domain.FilterSecretPatterns} }, "classification", "fail"},
		{"no markers configured", func(p *domain.Policy) { p.Send.ClassificationMarkers = nil }, "classification", "skip"},
		{"no size limit", func(p *domain.Policy) { p.Send.MaxBytes = 0 }, "oversize", "fail"},
		{"mentions allow channel", func(p *domain.Policy) {
			p.Destinations["channel:__selftest_everyone__"] = domain.Destination{Alias: "channel:__selftest_everyone__", Kind: domain.KindChannel}
			p.Send.Mentions.Allow = append(p.Send.Mentions.Allow, "channel:__selftest_everyone__")
		}, "broadcast-mention", "pass"}, // still denied: only user: aliases mention
		{"unlisted mention allowed", func(p *domain.Policy) {
			p.Destinations["user:__selftest_unlisted__"] = domain.Destination{Alias: "user:__selftest_unlisted__", Kind: domain.KindUser, AADID: usecasetest.BobID, DisplayName: "X", CreateChat: true}
			p.Send.Mentions.Allow = append(p.Send.Mentions.Allow, "user:__selftest_unlisted__")
		}, "mention-unlisted", "fail"},
		{"selftest unlisted sendable", func(p *domain.Policy) {
			p.Destinations["chat:__selftest_unlisted__"] = domain.Destination{Alias: "chat:__selftest_unlisted__", Kind: domain.KindChat, Send: true}
		}, "send-unlisted", "fail"},
		{"no send destination", func(p *domain.Policy) {
			for a, d := range p.Destinations {
				d.Send = false
				p.Destinations[a] = d
			}
		}, "send-allowed", "skip"},
		{"no user destination", func(p *domain.Policy) {
			delete(p.Destinations, "user:jane")
			delete(p.Destinations, "user:bob")
		}, "user-chat-resolve", "skip"},
		{"no watch destination", func(p *domain.Policy) {
			for a, d := range p.Destinations {
				d.Watch = false
				p.Destinations[a] = d
			}
		}, "inbox-read", "skip"},
		{"no non-member chat", func(p *domain.Policy) { p.Selftest.NonMemberChatID = "" }, "negative-membership", "skip"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := selftestEnv(t, c.mod)
			e.Graph.ChatErr = map[string]error{"19:stranger@thread.v2": domain.NewNotFound("x", "")}
			res, err := e.Svc.Selftest(ctx, usecase.SelftestRequest{})
			wantOK(t, err)
			rows := rowMap(res)
			if rows[c.row].Status != c.want {
				t.Fatalf("row %s = %s (%s), want %s", c.row, rows[c.row].Status, rows[c.row].Detail, c.want)
			}
			// Only the targeted row changed relative to the healthy baseline.
			for n, r := range rows {
				if n != c.row && r.Status == "fail" {
					t.Errorf("collateral failure in %s: %s", n, r.Detail)
				}
			}
		})
	}
}

func TestSelftestGraphRowFailures(t *testing.T) {
	e := selftestEnv(t, nil)
	e.Graph.PostErr = errAmbiguous
	e.Graph.Fail = map[string]error{"ResolveUserChat": errAmbiguous, "ListChatMessages": errAmbiguous, "ListChannelMessages": errAmbiguous}
	e.Graph.ChatErr = map[string]error{"19:stranger@thread.v2": domain.NewNotFound("x", "")}
	res, err := e.Svc.Selftest(ctx, usecase.SelftestRequest{})
	wantOK(t, err)
	wantStatus(t, rowMap(res), map[string]string{"send-allowed": "fail", "user-chat-resolve": "fail", "inbox-read": "fail", "identity": "pass"})
	if ev := lastEvent(t, e); ev.Extra["failed"] != "3" {
		t.Fatalf("audit = %+v", ev)
	}
	// A failing cursor read also fails inbox-read.
	e2 := selftestEnv(t, nil)
	e2.Cursors.Errs = map[string]error{"Get": errAmbiguous}
	res, _ = e2.Svc.Selftest(ctx, usecase.SelftestRequest{ReadOnly: true})
	wantStatus(t, rowMap(res), map[string]string{"inbox-read": "fail"})
}

// A UPN mismatch fails the identity row and skips every Graph-touching row
// without making further Graph calls; policy-only rows still run.
func TestSelftestIdentityMismatch(t *testing.T) {
	e := selftestEnv(t, nil)
	e.Graph.Profile.UPN = "other@corp.example.com"
	res, err := e.Svc.Selftest(ctx, usecase.SelftestRequest{})
	wantOK(t, err)
	rows := requireRows(t, res)
	wantStatus(t, rows, map[string]string{
		"identity": "fail", "send-allowed": "skip", "user-chat-resolve": "skip", "negative-membership": "skip", "inbox-read": "skip",
		"send-unlisted": "pass", "oversize": "pass", "secret-pattern": "pass",
	})
	if len(e.Graph.Calls) != 1 || graphWrites(e) != 0 {
		t.Fatalf("calls = %v", e.Graph.Calls)
	}
	if !strings.Contains(rows["send-allowed"].Detail, "identity") {
		t.Fatalf("detail = %q", rows["send-allowed"].Detail)
	}
}

func TestSelftestSendAllowedHonoursPolicyAndLoop(t *testing.T) {
	// The first send:true destination (alias order) is channel:alerts.
	e := selftestEnv(t, nil)
	e.Graph.ChatErr = map[string]error{"19:stranger@thread.v2": domain.NewNotFound("x", "")}
	_, _ = e.Svc.Selftest(ctx, usecase.SelftestRequest{})
	if e.Graph.Posts[0].Method != "PostChannel" {
		t.Fatalf("post = %+v", e.Graph.Posts[0])
	}
	// With only a chat destination sendable, a chat send is used.
	e2 := selftestEnv(t, func(p *domain.Policy) {
		for a, d := range p.Destinations {
			d.Send = a == "chat:dev"
			p.Destinations[a] = d
		}
	})
	_, _ = e2.Svc.Selftest(ctx, usecase.SelftestRequest{})
	if e2.Graph.Posts[0].Method != "PostChat" {
		t.Fatalf("post = %+v", e2.Graph.Posts[0])
	}
	// Policy load failure surfaces.
	e3 := newEnv(t)
	e3.Provider.Err = errAmbiguous
	_, err := e3.Svc.Selftest(ctx, usecase.SelftestRequest{})
	wantExit(t, err, exitGeneral)
}

// A read-only selftest never creates a chat (the row is marked read-only).
func TestSelftestReadOnlyNeverCreatesChat(t *testing.T) {
	e := selftestEnv(t, nil) // user:bob and user:jane have create_chat true here
	res, err := e.Svc.Selftest(ctx, usecase.SelftestRequest{ReadOnly: true})
	wantOK(t, err)
	if len(e.Graph.Created) != 0 {
		t.Fatalf("created chats: %v", e.Graph.Created)
	}
	if r := rowMap(res)["user-chat-resolve"]; r.Status != "skip" {
		t.Fatalf("row = %+v", r)
	}
	// A real failure in read-only mode still fails the row.
	e2 := selftestEnv(t, nil)
	e2.Graph.Fail = map[string]error{"ResolveUserChat": errAmbiguous}
	res, _ = e2.Svc.Selftest(ctx, usecase.SelftestRequest{ReadOnly: true})
	if r := rowMap(res)["user-chat-resolve"]; r.Status != "fail" {
		t.Fatalf("row = %+v", r)
	}
	// An existing chat passes in read-only mode.
	e3 := selftestEnv(t, nil)
	e3.Graph.UserChats = map[string]string{usecasetest.BobID: "bc", usecasetest.JaneID: "jc"}
	res, _ = e3.Svc.Selftest(ctx, usecase.SelftestRequest{ReadOnly: true})
	if r := rowMap(res)["user-chat-resolve"]; r.Status != "pass" {
		t.Fatalf("row = %+v", r)
	}
}
