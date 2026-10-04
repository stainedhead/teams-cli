package usecase_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stainedhead/teams-cli/internal/domain"
	"github.com/stainedhead/teams-cli/internal/usecase"
	"github.com/stainedhead/teams-cli/internal/usecase/usecasetest"
)

// B7 (AC-8): reply to a channel thread uses the replies endpoint; the alias
// comes from the thread id alone and no prior inbox is needed.
func TestReplyChannel(t *testing.T) {
	e := newEnv(t)
	res, err := e.Svc.Reply(ctx, usecase.ReplyRequest{ThreadID: "channel:alerts/1696341900000", Text: "on it"})
	wantOK(t, err)
	if res.ThreadID != "channel:alerts/1696341900000" || res.MessageID == "" {
		t.Fatalf("res = %+v", res)
	}
	p := e.Graph.Posts[0]
	if p.Method != "PostChannel" || p.ThreadID != "1696341900000" || p.TeamID != usecasetest.TeamID {
		t.Fatalf("post = %+v", p)
	}
	if ev := lastEvent(t, e); ev.Verb != "reply" || ev.Resource != "channel:alerts" {
		t.Fatalf("audit = %+v", ev)
	}
	if e.Graph.CallCount("ListChannelMessages")+e.Graph.CallCount("ListChatMessages") != 0 {
		t.Fatal("reply needed a prior inbox")
	}
}

func TestReplyChatIsPlainMessage(t *testing.T) {
	e := newEnv(t)
	res, err := e.Svc.Reply(ctx, usecase.ReplyRequest{ThreadID: "chat:dev/chat", Text: "ok"})
	wantOK(t, err)
	p := e.Graph.Posts[0]
	if p.Method != "PostChat" || p.ChatID != usecasetest.DevChatID || res.ThreadID != "chat:dev/chat" {
		t.Fatalf("post=%+v res=%+v", p, res)
	}
}

func TestReplyErrors(t *testing.T) {
	e := newEnv(t)
	cases := []struct {
		id   string
		want int
	}{
		{"nonsense", exitValidation},
		{"chat:dev/", exitValidation},
		{"19:abc@thread.v2/123", exitValidation},
		{"chat:nope/chat", exitPolicy},
		{"chat:readonly/chat", exitPolicy},
	}
	for _, c := range cases {
		_, err := e.Svc.Reply(ctx, usecase.ReplyRequest{ThreadID: c.id, Text: "x"})
		if got := codeOf(err); got != c.want {
			t.Errorf("%q: exit %d, want %d (%v)", c.id, got, c.want, err)
		}
	}
	if graphWrites(e) != 0 {
		t.Fatal("wrote")
	}
}

// B7: same checks as send (filters, mentions, key, loop guard keyed on the thread).
func TestReplySharedChecks(t *testing.T) {
	e := newEnv(t)
	thread := "channel:alerts/root1"
	_, err := e.Svc.Reply(ctx, usecase.ReplyRequest{ThreadID: thread, Text: fakeAWSKey})
	wantExit(t, err, exitPolicy)
	_, err = e.Svc.Reply(ctx, usecase.ReplyRequest{ThreadID: thread, Text: "x", Mentions: []domain.Alias{"user:bob"}})
	wantExit(t, err, exitPolicy)
	_, err = e.Svc.Reply(ctx, usecase.ReplyRequest{ThreadID: thread, Text: "x", IdempotencyKey: "bad key"})
	wantExit(t, err, exitValidation)
	// Loop guard: depth 3 per thread; another thread is unaffected.
	for i := 0; i < 3; i++ {
		_, err = e.Svc.Reply(ctx, usecase.ReplyRequest{ThreadID: thread, Text: "x"})
		wantOK(t, err)
	}
	_, err = e.Svc.Reply(ctx, usecase.ReplyRequest{ThreadID: thread, Text: "x"})
	wantExit(t, err, exitPolicy)
	_, err = e.Svc.Reply(ctx, usecase.ReplyRequest{ThreadID: "channel:alerts/root2", Text: "x"})
	wantOK(t, err)
	// Dry-run and idempotent replay.
	r, err := e.Svc.Reply(ctx, usecase.ReplyRequest{ThreadID: "channel:alerts/root3", Text: "x", DryRun: true})
	wantOK(t, err)
	if !r.DryRun {
		t.Fatalf("r = %+v", r)
	}
	req := usecase.ReplyRequest{ThreadID: "channel:alerts/root4", Text: "x", IdempotencyKey: "rk"}
	_, err = e.Svc.Reply(ctx, req)
	wantOK(t, err)
	r, err = e.Svc.Reply(ctx, req)
	wantOK(t, err)
	if !r.Deduplicated || r.ThreadID != "channel:alerts/root4" {
		t.Fatalf("r = %+v", r)
	}
}

func TestReplyLoopSimulationTwoAgents(t *testing.T) {
	// Two agent identities replying to each other stop at reply_depth_max.
	e := newEnv(t)
	e.Graph.ChannelMessages = map[string][]domain.RawMessage{}
	thread := "channel:alerts/root"
	sent := 0
	for i := 0; i < 10; i++ {
		if _, err := e.Svc.Reply(ctx, usecase.ReplyRequest{ThreadID: thread, Text: "reply"}); err != nil {
			wantExit(t, err, exitPolicy)
			break
		}
		sent++
		e.Clock.Advance(time.Minute)
	}
	if sent != 3 {
		t.Fatalf("sent %d replies, want 3", sent)
	}
}

func TestReplyMentionBuild(t *testing.T) {
	e := newEnv(t)
	_, err := e.Svc.Reply(ctx, usecase.ReplyRequest{ThreadID: "channel:alerts/r", Text: "hi", Mentions: []domain.Alias{"user:jane"}})
	wantOK(t, err)
	if !strings.Contains(e.Graph.Posts[0].Msg.Text, "Jane Doe") {
		t.Fatalf("msg = %+v", e.Graph.Posts[0].Msg)
	}
}

// The loop guard keys on the chat thread whatever root a chat reply carries,
// so item ids cannot be used to dodge reply_depth_max.
func TestReplyChatLoopGuardIgnoresRoot(t *testing.T) {
	e := newEnv(t) // ReplyDepthMax 3
	for _, id := range []string{"chat:dev/m1", "chat:dev/m2", "chat:dev/chat"} {
		_, err := e.Svc.Reply(ctx, usecase.ReplyRequest{ThreadID: id, Text: "x"})
		wantOK(t, err)
	}
	_, err := e.Svc.Send(ctx, usecase.SendRequest{Alias: "chat:dev", Text: "x"})
	wantExit(t, err, exitPolicy)
	_, err = e.Svc.Reply(ctx, usecase.ReplyRequest{ThreadID: "chat:dev/m99", Text: "x"})
	wantExit(t, err, exitPolicy)
	if ev := lastEvent(t, e); ev.Decision != "deny:loop.reply_depth" {
		t.Fatalf("decision = %q", ev.Decision)
	}
}
