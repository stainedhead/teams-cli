package usecase_test

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stainedhead/teams-cli/internal/domain"
	"github.com/stainedhead/teams-cli/internal/usecase"
	"github.com/stainedhead/teams-cli/internal/usecase/usecasetest"
)

const fakeAWSKey = "AKIAIOSFODNN7EXAMPLE"

// B3 (AC-4): happy path to a chat.
func TestSendChat(t *testing.T) {
	e := envWith(t, func(p *domain.Policy) { p.Send.Prefix = "[agent] " })
	res, err := e.Svc.Send(ctx, usecase.SendRequest{Alias: "chat:dev", Text: "hello"})
	wantOK(t, err)
	if res.MessageID != "m1" || res.ThreadID != "chat:dev/chat" || res.Deduplicated || res.DryRun {
		t.Fatalf("res = %+v", res)
	}
	if len(e.Graph.Posts) != 1 {
		t.Fatalf("posts = %+v", e.Graph.Posts)
	}
	got := e.Graph.Posts[0]
	if got.Method != "PostChat" || got.ChatID != usecasetest.DevChatID || got.Msg.Text != "[agent] hello" || got.Msg.HTML {
		t.Fatalf("post = %+v", got)
	}
	if len(e.Ledger.Sent) != 1 || e.Ledger.Sent[0].ThreadID != "chat:dev/chat" {
		t.Fatalf("sent history = %+v", e.Ledger.Sent)
	}
	ev := lastEvent(t, e)
	if ev.Verb != "send" || ev.Resource != "chat:dev" || ev.Outcome != "ok" || ev.Extra["message_id"] != "m1" {
		t.Fatalf("audit = %+v", ev)
	}
	mustNotContain(t, "audit", fmt.Sprintf("%+v", ev), "hello", usecasetest.DevChatID)
}

// B3 (AC-4): happy path to a channel; the new message starts a thread.
func TestSendChannel(t *testing.T) {
	e := newEnv(t)
	res, err := e.Svc.Send(ctx, usecase.SendRequest{Alias: "channel:alerts", Text: "deploy done"})
	wantOK(t, err)
	if res.ThreadID != "channel:alerts/m1" {
		t.Fatalf("res = %+v", res)
	}
	p := e.Graph.Posts[0]
	if p.Method != "PostChannel" || p.TeamID != usecasetest.TeamID || p.ChannelID != usecasetest.ChannelID || p.ThreadID != "" {
		t.Fatalf("post = %+v", p)
	}
	if len(e.Ledger.Threads) != 1 {
		t.Fatalf("threads = %v", e.Ledger.Threads)
	}
}

func TestSendUserChatResolvedAndCached(t *testing.T) {
	e := newEnv(t)
	e.Graph.UserChats = map[string]string{usecasetest.JaneID: "jane-chat"}
	_, err := e.Svc.Send(ctx, usecase.SendRequest{Alias: "user:jane", Text: "hi"})
	wantOK(t, err)
	_, err = e.Svc.Send(ctx, usecase.SendRequest{Alias: "user:jane", Text: "again"})
	wantOK(t, err)
	if e.Graph.CallCount("ResolveUserChat") != 1 {
		t.Fatalf("resolved %d times, want 1 (cache)", e.Graph.CallCount("ResolveUserChat"))
	}
	if e.Graph.Posts[1].ChatID != "jane-chat" {
		t.Fatalf("posts = %+v", e.Graph.Posts)
	}
}

func TestSendUserChatMissingNotCreated(t *testing.T) {
	e := newEnv(t)
	_, err := e.Svc.Send(ctx, usecase.SendRequest{Alias: "user:jane", Text: "hi"})
	wantExit(t, err, exitNotFound)
	if graphWrites(e) != 0 || len(e.Ledger.Entries) != 0 {
		t.Fatal("wrote despite missing chat")
	}
	// create_chat: true creates it.
	_, err = e.Svc.Send(ctx, usecase.SendRequest{Alias: "user:bob", Text: "hi"})
	wantOK(t, err)
	if len(e.Graph.Created) != 1 || e.Graph.Posts[0].ChatID != "created-"+usecasetest.BobID {
		t.Fatalf("created=%v posts=%+v", e.Graph.Created, e.Graph.Posts)
	}
}

// B3: table of inputs, each failing exactly one check, first failure wins.
func TestSendCheckOrder(t *testing.T) {
	long := strings.Repeat("a", 201)
	cases := []struct {
		name string
		mod  func(*domain.Policy)
		req  usecase.SendRequest
		want int
		rule string
	}{
		{"raw id alias", nil, usecase.SendRequest{Alias: "19:abc@thread.v2", Text: "x"}, exitUsage, ""},
		{"bad kind", nil, usecase.SendRequest{Alias: "team:x", Text: "x"}, exitUsage, ""},
		{"unlisted", nil, usecase.SendRequest{Alias: "chat:nope", Text: "x"}, exitPolicy, "destination.unlisted"},
		{"send false", nil, usecase.SendRequest{Alias: "chat:readonly", Text: "x"}, exitPolicy, "destination.send_denied"},
		{"bad key", nil, usecase.SendRequest{Alias: "chat:dev", Text: "x", IdempotencyKey: "bad key!"}, exitValidation, ""},
		{"oversize", nil, usecase.SendRequest{Alias: "chat:dev", Text: long}, exitValidation, ""},
		{"empty", nil, usecase.SendRequest{Alias: "chat:dev", Text: "  \n\t "}, exitValidation, ""},
		{"control char", nil, usecase.SendRequest{Alias: "chat:dev", Text: "a\rb"}, exitValidation, ""},
		{"mention unlisted", nil, usecase.SendRequest{Alias: "chat:dev", Text: "x", Mentions: []domain.Alias{"user:bob"}}, exitPolicy, "deny:mention"},
		{"mention broadcast", nil, usecase.SendRequest{Alias: "chat:dev", Text: "x", Mentions: []domain.Alias{"channel:alerts"}}, exitPolicy, "deny:mention"},
		{"secret", nil, usecase.SendRequest{Alias: "chat:dev", Text: "key " + fakeAWSKey}, exitPolicy, "deny:filter.secret_patterns"},
		{"marker", nil, usecase.SendRequest{Alias: "chat:dev", Text: "this is confidential stuff"}, exitPolicy, "deny:filter.classification_markers"},
		{"link", func(p *domain.Policy) { p.Send.LinkAllowlist = []string{"ok.example.com"} },
			usecase.SendRequest{Alias: "chat:dev", Text: "see https://evil.example.net/x"}, exitPolicy, "deny:filter.link_allowlist"},
		{"run cap", func(p *domain.Policy) { p.Limits.MaxWritesPerRun = 1 }, usecase.SendRequest{Alias: "chat:dev", Text: "x"}, 0, ""},
		{"rate minute", func(p *domain.Policy) { p.Send.Rate.PerMinute = 1 }, usecase.SendRequest{Alias: "chat:dev", Text: "x"}, 0, ""},
		// first failure wins: unlisted beats oversize beats secret.
		{"unlisted beats oversize", nil, usecase.SendRequest{Alias: "chat:nope", Text: long}, exitPolicy, "destination.unlisted"},
		{"oversize beats secret", nil, usecase.SendRequest{Alias: "chat:dev", Text: fakeAWSKey + long}, exitValidation, ""},
		{"empty beats mention", nil, usecase.SendRequest{Alias: "chat:dev", Text: " ", Mentions: []domain.Alias{"user:bob"}}, exitValidation, ""},
		{"mention beats secret", nil, usecase.SendRequest{Alias: "chat:dev", Text: fakeAWSKey, Mentions: []domain.Alias{"user:bob"}}, exitPolicy, "deny:mention"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mod := c.mod
			if mod == nil {
				mod = func(*domain.Policy) {}
			}
			e := envWith(t, mod)
			if c.want == 0 { // limit cases: succeed first, then fail the second send
				_, err := e.Svc.Send(ctx, c.req)
				wantOK(t, err)
				_, err = e.Svc.Send(ctx, c.req)
				wantExit(t, err, exitPolicy)
				return
			}
			_, err := e.Svc.Send(ctx, c.req)
			wantExit(t, err, c.want)
			if graphWrites(e) != 0 || len(e.Ledger.Entries) != 0 || len(e.Ledger.Sent) != 0 {
				t.Fatal("a refused send must not write anything")
			}
			if c.rule != "" {
				ev := lastEvent(t, e)
				if !strings.HasSuffix(ev.Decision, c.rule) {
					t.Fatalf("decision = %q, want suffix %q", ev.Decision, c.rule)
				}
			}
			// B5: errors never echo the matched text.
			mustNotContain(t, "error", fmt.Sprint(err), fakeAWSKey, "confidential", "evil.example.net")
		})
	}
}

// B4 (AC-5): dry-run decides like a real send but writes nothing.
func TestDryRunNoWritesSameDecision(t *testing.T) {
	e := envWith(t, func(p *domain.Policy) { p.Send.ReplyDepthMax = 0 })
	req := usecase.SendRequest{Alias: "channel:alerts", Text: "hello", IdempotencyKey: "k1", DryRun: true}
	res, err := e.Svc.Send(ctx, req)
	wantOK(t, err)
	if !res.DryRun || res.MessageID != "" {
		t.Fatalf("res = %+v", res)
	}
	if graphWrites(e) != 0 || len(e.Ledger.Entries) != 0 || len(e.Ledger.Sent) != 0 || len(e.Ledger.Threads) != 0 {
		t.Fatal("dry-run wrote")
	}
	if e.Graph.CallCount("ResolveUserChat") != 0 {
		t.Fatal("dry-run resolved a chat")
	}
	if ev := lastEvent(t, e); ev.Outcome != "dry_run" {
		t.Fatalf("audit = %+v", ev)
	}
	// Dry-run consumes no rate: ten more real sends still fit in 10/min.
	for i := 0; i < 10; i++ {
		d := req
		d.DryRun = true
		_, err = e.Svc.Send(ctx, d)
		wantOK(t, err)
	}
	for i := 0; i < 10; i++ {
		_, err = e.Svc.Send(ctx, usecase.SendRequest{Alias: "chat:dev", Text: "x"})
		wantOK(t, err)
	}
	// ...and the 11th is denied, for dry-run too (same decision as real).
	_, err = e.Svc.Send(ctx, usecase.SendRequest{Alias: "chat:dev", Text: "x", DryRun: true})
	wantExit(t, err, exitPolicy)
	_, err = e.Svc.Send(ctx, usecase.SendRequest{Alias: "chat:dev", Text: "x"})
	wantExit(t, err, exitPolicy)
}

func TestDryRunDeniedLikeRealSend(t *testing.T) {
	e := newEnv(t)
	_, err := e.Svc.Send(ctx, usecase.SendRequest{Alias: "chat:readonly", Text: "x", DryRun: true})
	wantExit(t, err, exitPolicy)
	_, err = e.Svc.Send(ctx, usecase.SendRequest{Alias: "chat:dev", Text: fakeAWSKey, DryRun: true})
	wantExit(t, err, exitPolicy)
}

// B5 (AC-6): mentions are built from policy only.
func TestSendMentions(t *testing.T) {
	e := newEnv(t)
	_, err := e.Svc.Send(ctx, usecase.SendRequest{Alias: "channel:alerts", Text: "<b>hi</b> & bye", Mentions: []domain.Alias{"user:jane", "user:jane"}})
	wantOK(t, err)
	m := e.Graph.Posts[0].Msg
	if !m.HTML || len(m.Mentions) != 1 || m.Mentions[0].AADID != usecasetest.JaneID || m.Mentions[0].DisplayName != "Jane Doe" {
		t.Fatalf("msg = %+v", m)
	}
	if !strings.Contains(m.Text, `<at id="0">Jane Doe</at>`) || !strings.Contains(m.Text, "&lt;b&gt;hi&lt;/b&gt; &amp; bye") {
		t.Fatalf("text = %q", m.Text)
	}
	if e.Graph.CallCount("ResolveUserChat") != 0 {
		t.Fatal("mention must not call Graph")
	}
}

// B5 (AC-9): rate windows on the fake clock and the per-run cap.
func TestSendRateLimitWindows(t *testing.T) {
	e := envWith(t, func(p *domain.Policy) { p.Send.Rate = domain.Rate{PerMinute: 2, PerHour: 3}; p.Send.ReplyDepthMax = 0 })
	send := func() error {
		_, err := e.Svc.Send(ctx, usecase.SendRequest{Alias: "chat:dev", Text: "x"})
		return err
	}
	wantOK(t, send())
	wantOK(t, send())
	err := send()
	wantExit(t, err, exitPolicy)
	if h := hint(err); !strings.Contains(h, "retry after") {
		t.Fatalf("hint = %q", h)
	}
	e.Clock.Advance(61 * time.Second)
	wantOK(t, send()) // minute window freed, hour has 2 of 3
	e.Clock.Advance(61 * time.Second)
	wantExit(t, send(), exitPolicy) // hour window full
	e.Clock.Advance(time.Hour)
	wantOK(t, send())
	if ev := lastEvent(t, e); ev.Outcome != "ok" {
		t.Fatalf("audit = %+v", ev)
	}
}

func TestSendRunCapPerProcessNotPerCommandLedger(t *testing.T) {
	e := envWith(t, func(p *domain.Policy) { p.Limits.MaxWritesPerRun = 2; p.Send.ReplyDepthMax = 0 })
	for i := 0; i < 2; i++ {
		_, err := e.Svc.Send(ctx, usecase.SendRequest{Alias: "chat:dev", Text: "x"})
		wantOK(t, err)
	}
	_, err := e.Svc.Send(ctx, usecase.SendRequest{Alias: "chat:dev", Text: "x"})
	wantExit(t, err, exitPolicy)
	if ev := lastEvent(t, e); ev.Decision != "deny:rate.run_cap" {
		t.Fatalf("decision = %q", ev.Decision)
	}
	// A new run (service) starts at zero.
	_, err = e.NewService().Send(ctx, usecase.SendRequest{Alias: "chat:dev", Text: "x"})
	wantOK(t, err)
}

// B5 (AC-10): loop guard keyed on the thread.
func TestLoopGuardOnChatThread(t *testing.T) {
	e := newEnv(t) // ReplyDepthMax 3
	for i := 0; i < 3; i++ {
		_, err := e.Svc.Send(ctx, usecase.SendRequest{Alias: "chat:dev", Text: "ping"})
		wantOK(t, err)
	}
	_, err := e.Svc.Send(ctx, usecase.SendRequest{Alias: "chat:dev", Text: "ping"})
	wantExit(t, err, exitPolicy)
	if ev := lastEvent(t, e); ev.Decision != "deny:loop.reply_depth" {
		t.Fatalf("decision = %q", ev.Decision)
	}
	// Outside the reply window the count resets.
	e.Clock.Advance(25 * time.Hour)
	_, err = e.Svc.Send(ctx, usecase.SendRequest{Alias: "chat:dev", Text: "ping"})
	wantOK(t, err)
}

func TestLedgerErrorsBlockSend(t *testing.T) {
	for _, m := range []string{"SentSince", "SentInThread", "Reserve"} {
		e := newEnv(t)
		e.Ledger.Errs = map[string]error{m: domain.NewConflict("ledger corrupt", "remove it")}
		_, err := e.Svc.Send(ctx, usecase.SendRequest{Alias: "chat:dev", Text: "x", IdempotencyKey: "k"})
		wantExit(t, err, exitConflict)
		if graphWrites(e) != 0 {
			t.Fatalf("%s: posted despite ledger failure", m)
		}
	}
}

// B6 (AC-11): ledger flow.
func TestIdempotencyReplay(t *testing.T) {
	e := newEnv(t)
	req := usecase.SendRequest{Alias: "channel:alerts", Text: "once", IdempotencyKey: "key-1"}
	first, err := e.Svc.Send(ctx, req)
	wantOK(t, err)
	second, err := e.Svc.Send(ctx, req)
	wantOK(t, err)
	if !second.Deduplicated || second.MessageID != first.MessageID || second.ThreadID != first.ThreadID {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	if graphWrites(e) != 1 {
		t.Fatalf("posts = %d", graphWrites(e))
	}
	if ev := lastEvent(t, e); ev.Outcome != "deduplicated" || ev.Extra["deduplicated"] != "true" {
		t.Fatalf("audit = %+v", ev)
	}
	// Replay from a new process run too.
	third, err := e.NewService().Send(ctx, req)
	wantOK(t, err)
	if !third.Deduplicated {
		t.Fatalf("third = %+v", third)
	}
}

func TestIdempotencyReplayChatThread(t *testing.T) {
	e := newEnv(t)
	req := usecase.SendRequest{Alias: "chat:dev", Text: "once", IdempotencyKey: "key-c"}
	_, err := e.Svc.Send(ctx, req)
	wantOK(t, err)
	r, err := e.Svc.Send(ctx, req)
	wantOK(t, err)
	if r.ThreadID != "chat:dev/chat" {
		t.Fatalf("r = %+v", r)
	}
}

func TestIdempotencyChangedPayloadConflicts(t *testing.T) {
	e := newEnv(t)
	_, err := e.Svc.Send(ctx, usecase.SendRequest{Alias: "chat:dev", Text: "one", IdempotencyKey: "k"})
	wantOK(t, err)
	_, err = e.Svc.Send(ctx, usecase.SendRequest{Alias: "chat:dev", Text: "two", IdempotencyKey: "k"})
	wantExit(t, err, exitConflict)
	if graphWrites(e) != 1 {
		t.Fatal("changed payload posted")
	}
}

func TestAmbiguousFailureStaysPending(t *testing.T) {
	e := newEnv(t)
	e.Graph.PostErr = errAmbiguous
	req := usecase.SendRequest{Alias: "chat:dev", Text: "x", IdempotencyKey: "k"}
	_, err := e.Svc.Send(ctx, req)
	wantExit(t, err, exitGeneral)
	if h := hint(err); !strings.Contains(h, "outcome unknown") {
		t.Fatalf("hint = %q", h)
	}
	if st := e.Ledger.Entries["k"].State; st != domain.StatePending {
		t.Fatalf("state = %s", st)
	}
	// Pending counts toward the rate window.
	sent, _ := e.Ledger.SentSince(ctx, usecasetest.T0.Add(-time.Hour))
	if len(sent) != 1 {
		t.Fatalf("history = %+v", sent)
	}
	// The next attempt with the same key exits 7 and posts nothing.
	e.Graph.PostErr = nil
	_, err = e.Svc.Send(ctx, req)
	wantExit(t, err, exitConflict)
	if len(e.Graph.Posts) != 0 {
		t.Fatal("retried a pending key")
	}
}

func TestAmbiguousFailureWithoutKeyHint(t *testing.T) {
	e := newEnv(t)
	e.Graph.PostErr = errAmbiguous
	_, err := e.Svc.Send(ctx, usecase.SendRequest{Alias: "chat:dev", Text: "x"})
	wantExit(t, err, exitGeneral)
	if h := hint(err); strings.Contains(h, "idempotency") || !strings.Contains(h, "outcome unknown") {
		t.Fatalf("hint = %q", h)
	}
	if len(e.Ledger.Sent) != 0 {
		t.Fatal("recorded a failed send")
	}
}

func TestNotSentAllowsRetry(t *testing.T) {
	e := newEnv(t)
	e.Graph.PostErr = domain.NotSent(domain.NewValidation("throttled before processing", ""))
	req := usecase.SendRequest{Alias: "chat:dev", Text: "x", IdempotencyKey: "k"}
	_, err := e.Svc.Send(ctx, req)
	wantExit(t, err, exitValidation)
	if st := e.Ledger.Entries["k"].State; st != domain.StateFailed {
		t.Fatalf("state = %s", st)
	}
	e.Graph.PostErr = nil
	res, err := e.Svc.Send(ctx, req)
	wantOK(t, err)
	if res.Deduplicated || res.MessageID == "" || e.Ledger.Entries["k"].State != domain.StateSent {
		t.Fatalf("res=%+v entry=%+v", res, e.Ledger.Entries["k"])
	}
}

func TestLedgerUpdateFailureAfterPostIsWarned(t *testing.T) {
	e := newEnv(t)
	e.Ledger.Errs = map[string]error{"Complete": errAmbiguous}
	res, err := e.Svc.Send(ctx, usecase.SendRequest{Alias: "chat:dev", Text: "x", IdempotencyKey: "k"})
	wantOK(t, err)
	if res.MessageID == "" || lastEvent(t, e).Extra["warn"] != "ledger_update_failed" {
		t.Fatalf("res=%+v audit=%+v", res, lastEvent(t, e))
	}
	e2 := newEnv(t)
	e2.Ledger.Errs = map[string]error{"RecordSent": errAmbiguous}
	_, err = e2.Svc.Send(ctx, usecase.SendRequest{Alias: "chat:dev", Text: "x"})
	wantOK(t, err)
	if lastEvent(t, e2).Extra["warn"] != "ledger_update_failed" {
		t.Fatalf("audit = %+v", lastEvent(t, e2))
	}
}

func TestMarkerScanOnAndOff(t *testing.T) {
	for _, scan := range []bool{false, true} {
		e := envWith(t, func(p *domain.Policy) { p.Send.MarkerScan = scan })
		e.Graph.PostErr = errAmbiguous
		req := usecase.SendRequest{Alias: "chat:dev", Text: "x", IdempotencyKey: "k"}
		_, err := e.Svc.Send(ctx, req)
		wantExit(t, err, exitGeneral)
		e.Graph.PostErr = nil
		_, err = e.Svc.Send(ctx, req)
		wantExit(t, err, exitConflict) // marker not found / scan off
		if scan && e.Graph.CallCount("FindByMarker") != 1 {
			t.Fatalf("scan on but FindByMarker called %d times", e.Graph.CallCount("FindByMarker"))
		}
		if !scan && e.Graph.CallCount("FindByMarker") != 0 {
			t.Fatal("scan off but FindByMarker called")
		}
	}
}

// TestAssumedMarkerSurvivesInBody ASSUMPTION (UA-9, unverified against a real tenant).
func TestAssumedMarkerSurvivesInBody(t *testing.T) {
	e := envWith(t, func(p *domain.Policy) { p.Send.MarkerScan = true })
	var marker string
	e.Graph.PostErr = errAmbiguous
	req := usecase.SendRequest{Alias: "channel:alerts", Text: "x", IdempotencyKey: "k"}
	_, err := e.Svc.Send(ctx, req)
	wantExit(t, err, exitGeneral)
	e.Graph.PostErr = nil
	// The failed post's marker is not recorded by the fake; capture it from a
	// successful sibling post with the same key+alias hash.
	e2 := envWith(t, func(p *domain.Policy) { p.Send.MarkerScan = true })
	_, err = e2.Svc.Send(ctx, usecase.SendRequest{Alias: "channel:alerts", Text: "x", IdempotencyKey: "k"})
	wantOK(t, err)
	marker = e2.Graph.Posts[0].Msg.MarkerKey
	if len(marker) != 16 {
		t.Fatalf("marker = %q", marker)
	}
	e.Graph.Markers = map[string]string{marker: "m-found"}
	res, err := e.Svc.Send(ctx, req)
	wantOK(t, err)
	if !res.Deduplicated || res.MessageID != "m-found" || res.ThreadID != "channel:alerts/m-found" {
		t.Fatalf("res = %+v", res)
	}
	if e.Ledger.Entries["k"].State != domain.StateSent || len(e.Graph.Posts) != 0 {
		t.Fatalf("entry=%+v posts=%d", e.Ledger.Entries["k"], len(e.Graph.Posts))
	}
}

func TestMarkerScanSkippedOnPayloadConflict(t *testing.T) {
	e := envWith(t, func(p *domain.Policy) { p.Send.MarkerScan = true })
	_, err := e.Svc.Send(ctx, usecase.SendRequest{Alias: "chat:dev", Text: "one", IdempotencyKey: "k"})
	wantOK(t, err)
	_, err = e.Svc.Send(ctx, usecase.SendRequest{Alias: "chat:dev", Text: "two", IdempotencyKey: "k"})
	wantExit(t, err, exitConflict)
	if e.Graph.CallCount("FindByMarker") != 0 {
		t.Fatal("scanned on a payload conflict")
	}
}

func TestMarkerScanLookupErrorFallsBackToConflict(t *testing.T) {
	e := envWith(t, func(p *domain.Policy) { p.Send.MarkerScan = true })
	e.Graph.PostErr = errAmbiguous
	req := usecase.SendRequest{Alias: "chat:dev", Text: "x", IdempotencyKey: "k"}
	_, err := e.Svc.Send(ctx, req)
	wantExit(t, err, exitGeneral)
	e.Graph.PostErr = nil
	e.Graph.Fail = map[string]error{"FindByMarker": errAmbiguous}
	_, err = e.Svc.Send(ctx, req)
	wantExit(t, err, exitConflict)
}

func TestMarkerScanCompleteFailure(t *testing.T) {
	e := envWith(t, func(p *domain.Policy) { p.Send.MarkerScan = true })
	e.Graph.PostErr = errAmbiguous
	req := usecase.SendRequest{Alias: "chat:dev", Text: "x", IdempotencyKey: "k"}
	_, _ = e.Svc.Send(ctx, req)
	e.Graph.PostErr = nil
	// Find the key hash via a sibling env.
	sib := envWith(t, func(p *domain.Policy) { p.Send.MarkerScan = true })
	_, _ = sib.Svc.Send(ctx, req)
	e.Graph.Markers = map[string]string{sib.Graph.Posts[0].Msg.MarkerKey: "m9"}
	e.Ledger.Errs = map[string]error{"Complete": errAmbiguous}
	_, err := e.Svc.Send(ctx, req)
	wantExit(t, err, exitConflict)
}

func TestMarkerScanUserChat(t *testing.T) {
	e := envWith(t, func(p *domain.Policy) { p.Send.MarkerScan = true })
	e.Graph.UserChats = map[string]string{usecasetest.JaneID: "jc"}
	req := usecase.SendRequest{Alias: "user:jane", Text: "x", IdempotencyKey: "k"}
	e.Graph.PostErr = errAmbiguous
	_, _ = e.Svc.Send(ctx, req)
	sib := envWith(t, func(p *domain.Policy) { p.Send.MarkerScan = true })
	sib.Graph.UserChats = e.Graph.UserChats
	_, _ = sib.Svc.Send(ctx, req)
	e.Graph.PostErr = nil
	e.Graph.Markers = map[string]string{sib.Graph.Posts[0].Msg.MarkerKey: "m5"}
	res, err := e.Svc.Send(ctx, req)
	wantOK(t, err)
	if res.MessageID != "m5" || res.ThreadID != "user:jane/chat" {
		t.Fatalf("res = %+v", res)
	}
}

func TestNoMarkerWithoutKeyOrFlag(t *testing.T) {
	e := envWith(t, func(p *domain.Policy) { p.Send.MarkerScan = true })
	_, err := e.Svc.Send(ctx, usecase.SendRequest{Alias: "chat:dev", Text: "x"})
	wantOK(t, err)
	if e.Graph.Posts[0].Msg.MarkerKey != "" {
		t.Fatal("marker on a keyless send")
	}
	e2 := newEnv(t)
	_, err = e2.Svc.Send(ctx, usecase.SendRequest{Alias: "chat:dev", Text: "x", IdempotencyKey: "k"})
	wantOK(t, err)
	if e2.Graph.Posts[0].Msg.MarkerKey != "" {
		t.Fatal("marker with scan off")
	}
}

// B6: two goroutines, same key -> exactly one post.
func TestConcurrentSameKeyOnePost(t *testing.T) {
	e := newEnv(t)
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = e.Svc.Send(ctx, usecase.SendRequest{Alias: "chat:dev", Text: "x", IdempotencyKey: "same"})
		}(i)
	}
	wg.Wait()
	if n := graphWrites(e); n != 1 {
		t.Fatalf("posts = %d, want 1", n)
	}
	ok := 0
	for _, err := range errs {
		if err == nil {
			ok++
		} else {
			wantExit(t, err, exitConflict)
		}
	}
	if ok < 1 { // late arrivals may legitimately see the sent entry and dedupe
		t.Fatal("no send succeeded")
	}
}

// B14 (AC-24): a planted token never reaches DTOs, errors or audit events.
func TestTokenHygiene(t *testing.T) {
	const planted = "eyJPLANTED.TOKEN.VALUE-1234567890"
	e := newEnv(t)
	e.Graph.PostErr = fmt.Errorf("transport failure for %s", "host")
	e.Graph.Profile.DisplayName = "Agent"
	var all []string
	res, err := e.Svc.Send(ctx, usecase.SendRequest{Alias: "chat:dev", Text: "x"})
	all = append(all, fmt.Sprintf("%+v %v", res, err))
	e.Graph.PostErr = nil
	res, err = e.Svc.Send(ctx, usecase.SendRequest{Alias: "chat:dev", Text: "x", IdempotencyKey: "k"})
	all = append(all, fmt.Sprintf("%+v %v", res, err))
	_, err = e.Svc.Send(ctx, usecase.SendRequest{Alias: "chat:dev", Text: "Bearer " + planted + planted})
	all = append(all, fmt.Sprint(err))
	for _, ev := range e.Audit.Events {
		all = append(all, fmt.Sprintf("%+v", ev))
	}
	all = append(all, fmt.Sprintf("%+v", e.Ledger.Entries), fmt.Sprintf("%+v", e.Ledger.Sent))
	// The planted token was only ever sent as message text in the last call,
	// which the filter refuses; it must not appear in any error or audit.
	joined := strings.Join(all, "\n")
	mustNotContain(t, "outputs", joined, planted)
}

// FR-R3 (FR-6): dry-run reports the decision, destination and a rendered preview.
func TestDryRunReportsDecisionDestinationPreview(t *testing.T) {
	e := envWith(t, func(p *domain.Policy) { p.Send.Prefix = "[bot] " })
	res, err := e.Svc.Send(ctx, usecase.SendRequest{Alias: "user:jane", Text: "hi <b>", Mentions: []domain.Alias{"user:jane"}, DryRun: true})
	wantOK(t, err)
	if res.Decision != "allow" || res.Destination.Alias != "user:jane" || res.Destination.Kind != domain.KindUser {
		t.Fatalf("res = %+v", res)
	}
	want := `<at id="0">Jane Doe</at> [bot] hi &lt;b&gt;`
	if res.Preview != want || !res.PreviewHTML {
		t.Fatalf("preview = %q html=%v", res.Preview, res.PreviewHTML)
	}
	if e.Graph.CallCount("ResolveUserChat") != 0 || graphWrites(e) != 0 {
		t.Fatal("dry-run had side effects")
	}
	// A real send does not carry a preview.
	real, err := e.Svc.Send(ctx, usecase.SendRequest{Alias: "chat:dev", Text: "x"})
	wantOK(t, err)
	if real.Preview != "" || real.Decision != "" {
		t.Fatalf("real send = %+v", real)
	}
}
