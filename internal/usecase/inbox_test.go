package usecase_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stainedhead/teams-cli/internal/domain"
	"github.com/stainedhead/teams-cli/internal/usecase"
	"github.com/stainedhead/teams-cli/internal/usecase/usecasetest"
)

var t0 = usecasetest.T0

func ago(d time.Duration) time.Time { return t0.Add(-d) }

// watchOnly keeps Watch true only for the named aliases.
func watchOnly(aliases ...domain.Alias) func(*domain.Policy) {
	return func(p *domain.Policy) {
		keep := map[domain.Alias]bool{}
		for _, a := range aliases {
			keep[a] = true
		}
		for a, d := range p.Destinations {
			d.Watch = keep[a]
			p.Destinations[a] = d
		}
	}
}

func devChat(e *usecasetest.Env, msgs ...domain.RawMessage) {
	if e.Graph.ChatMessages == nil {
		e.Graph.ChatMessages = map[string][]domain.RawMessage{}
	}
	e.Graph.ChatMessages[usecasetest.DevChatID] = append(e.Graph.ChatMessages[usecasetest.DevChatID], msgs...)
}

func ids(items []domain.InboundItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.ID
	}
	return out
}

// B8 (AC-12): normalization, classification, own/system/deleted exclusion,
// delivery recording, and no watermark or ack change.
func TestInboxBasic(t *testing.T) {
	e := envWith(t, watchOnly("chat:dev"))
	sys := usecasetest.Msg("sys", "", "joined", ago(9*time.Minute))
	sys.MessageType = "systemEventMessage"
	del := usecasetest.Msg("del", usecasetest.StrangerID, "gone", ago(8*time.Minute))
	del.Deleted = true
	own := usecasetest.Msg("own", usecasetest.AgentID, "my words", ago(7*time.Minute))
	cmd := usecasetest.Msg("m2", usecasetest.CommanderID, "do the thing", ago(5*time.Minute))
	str := usecasetest.Msg("m1", usecasetest.StrangerID, "hello <b>there</b>", ago(6*time.Minute))
	str.BodyType = "html"
	str.BodyContent = `<p>hello <a href="https://x.example/p">link</a></p>`
	devChat(e, cmd, sys, del, own, str)

	res, err := e.Svc.Inbox(ctx, usecase.InboxRequest{})
	wantOK(t, err)
	if got := ids(res.Items); len(got) != 2 || got[0] != "chat:dev/m1" || got[1] != "chat:dev/m2" {
		t.Fatalf("items = %v", got)
	}
	if !res.Items[1].Sender.CanInstruct || res.Items[0].Sender.CanInstruct {
		t.Fatalf("classification: %+v / %+v", res.Items[0].Sender, res.Items[1].Sender)
	}
	if res.Items[0].Text != "hello link" && !strings.Contains(res.Items[0].Text, "hello") {
		t.Fatalf("text = %q", res.Items[0].Text)
	}
	if len(res.Items[0].Links) != 1 {
		t.Fatalf("links = %v", res.Items[0].Links)
	}
	for _, k := range []string{"system_message", "deleted", "own_message"} {
		if res.Skipped[k] != 1 {
			t.Fatalf("skipped = %v", res.Skipped)
		}
	}
	cs, _ := e.Cursors.Get(ctx, "chat:dev")
	if len(cs.Delivered) != 2 || !cs.Watermark.IsZero() || len(cs.Acked) != 0 {
		t.Fatalf("cursor = %+v", cs)
	}
	ev := lastEvent(t, e)
	if ev.Extra["count"] != "2" || ev.Extra["dropped"] != "3" {
		t.Fatalf("audit = %+v", ev)
	}
	mustNotContain(t, "audit", fmt.Sprintf("%+v", ev), "do the thing", "hello")
	// Un-acked messages are re-delivered on every call (at-least-once).
	again, err := e.Svc.Inbox(ctx, usecase.InboxRequest{})
	wantOK(t, err)
	if len(again.Items) != 2 {
		t.Fatalf("redelivery = %v", ids(again.Items))
	}
}

func TestInboxHandleMatrix(t *testing.T) {
	mentionMsg := usecasetest.Msg("men", usecasetest.StrangerID, "hey agent", ago(3*time.Minute))
	mentionMsg.Mentions = []domain.RawMention{{UserID: usecasetest.AgentID}}
	plain := usecasetest.Msg("plain", usecasetest.StrangerID, "chatter", ago(2*time.Minute))
	cases := []struct {
		handle []domain.InboundHandle
		want   []string
	}{
		{[]domain.InboundHandle{domain.HandleMentions}, []string{"chat:dev/men"}},
		{[]domain.InboundHandle{domain.HandleWatched}, []string{"chat:dev/plain"}},
		{[]domain.InboundHandle{domain.HandleDirect}, nil},
		{[]domain.InboundHandle{domain.HandleMentions, domain.HandleWatched}, []string{"chat:dev/men", "chat:dev/plain"}},
	}
	for _, c := range cases {
		e := envWith(t, func(p *domain.Policy) { watchOnly("chat:dev")(p); p.Inbound.Handle = c.handle })
		devChat(e, mentionMsg, plain)
		res, err := e.Svc.Inbox(ctx, usecase.InboxRequest{})
		wantOK(t, err)
		got := ids(res.Items)
		if fmt.Sprint(got) != fmt.Sprint(c.want) && (len(got) != 0 || len(c.want) != 0) {
			t.Errorf("%v: got %v want %v", c.handle, got, c.want)
		}
		if len(c.want) < 2 && res.Skipped["handle_not_enabled"] == 0 {
			t.Errorf("%v: skipped = %v", c.handle, res.Skipped)
		}
	}
}

func TestInboxUnlistedNeverSurfaces(t *testing.T) {
	// D4: only watched policy destinations are read; the fake would serve an
	// unlisted chat's messages, but they are never requested.
	e := envWith(t, watchOnly("chat:dev"))
	e.Graph.ChatMessages = map[string][]domain.RawMessage{"19:unlisted@thread.v2": {usecasetest.Msg("x", usecasetest.CommanderID, "secret", ago(time.Minute))}}
	res, err := e.Svc.Inbox(ctx, usecase.InboxRequest{})
	wantOK(t, err)
	if len(res.Items) != 0 || e.Graph.CallCount("ListChatMessages") != 1 {
		t.Fatalf("items=%v calls=%v", res.Items, e.Graph.Calls)
	}
}

func TestInboxMergeSortLimit(t *testing.T) {
	e := envWith(t, watchOnly("chat:dev", "channel:alerts"))
	devChat(e,
		usecasetest.Msg("c1", usecasetest.StrangerID, "a", ago(10*time.Minute)),
		usecasetest.Msg("c3", usecasetest.StrangerID, "c", ago(4*time.Minute)))
	e.Graph.ChannelMessages = map[string][]domain.RawMessage{usecasetest.TeamID + "/" + usecasetest.ChannelID: {
		usecasetest.Msg("k2", usecasetest.StrangerID, "b", ago(7*time.Minute)),
		usecasetest.Msg("k4", usecasetest.StrangerID, "d", ago(2*time.Minute))}}
	res, err := e.Svc.Inbox(ctx, usecase.InboxRequest{Limit: 3})
	wantOK(t, err)
	if got := fmt.Sprint(ids(res.Items)); got != "[chat:dev/c1 channel:alerts/k2 chat:dev/c3]" {
		t.Fatalf("items = %s", got)
	}
	// Only the returned items are recorded as delivered.
	cs, _ := e.Cursors.Get(ctx, "channel:alerts")
	if len(cs.Delivered) != 1 {
		t.Fatalf("alerts delivered = %+v", cs.Delivered)
	}
	// Channel threads: top-level message id is the root.
	if res.Items[1].ThreadID != "channel:alerts/k2" || res.Items[0].ThreadID != "chat:dev/chat" {
		t.Fatalf("threads = %q %q", res.Items[1].ThreadID, res.Items[0].ThreadID)
	}
	// Limit above the policy cap is clamped; default applies for 0.
	e2 := envWith(t, func(p *domain.Policy) { watchOnly("chat:dev")(p); p.Limits.MaxResults = 2 })
	for i := 0; i < 5; i++ {
		devChat(e2, usecasetest.Msg(fmt.Sprint("m", i), usecasetest.StrangerID, "x", ago(time.Duration(10-i)*time.Minute)))
	}
	r2, err := e2.Svc.Inbox(ctx, usecase.InboxRequest{Limit: 99})
	wantOK(t, err)
	if len(r2.Items) != 2 {
		t.Fatalf("clamped items = %d", len(r2.Items))
	}
}

func TestInboxArgumentErrors(t *testing.T) {
	e := newEnv(t)
	for _, c := range []struct {
		r    usecase.InboxRequest
		want int
	}{
		{usecase.InboxRequest{Alias: "19:raw@thread.v2"}, exitUsage},
		{usecase.InboxRequest{Alias: "chat:nope"}, exitPolicy},
		{usecase.InboxRequest{Alias: "chat:writeonly"}, exitPolicy},
		{usecase.InboxRequest{Limit: -1}, exitUsage},
		{usecase.InboxRequest{Wait: -time.Second}, exitUsage},
	} {
		_, err := e.Svc.Inbox(ctx, c.r)
		wantExit(t, err, c.want)
	}
	// Alias filter restricts the poll to one destination.
	e2 := newEnv(t)
	_, err := e2.Svc.Inbox(ctx, usecase.InboxRequest{Alias: "chat:dev"})
	wantOK(t, err)
	if e2.Graph.CallCount("ListChatMessages") != 1 || e2.Graph.CallCount("ListChannelMessages") != 0 {
		t.Fatalf("calls = %v", e2.Graph.Calls)
	}
	if lastEvent(t, e2).Resource != "chat:dev" {
		t.Fatalf("audit = %+v", lastEvent(t, e2))
	}
}

// B8 (D6): user chat resolution.
func TestInboxUserChatResolution(t *testing.T) {
	e := envWith(t, watchOnly("user:jane", "user:bob", "chat:dev"))
	e.Graph.UserChats = nil
	devChat(e, usecasetest.Msg("c1", usecasetest.StrangerID, "hi", ago(time.Minute)))
	e.Graph.ChatMessages["created-"+usecasetest.BobID] = []domain.RawMessage{usecasetest.Msg("b1", usecasetest.StrangerID, "dm", ago(time.Minute))}
	res, err := e.Svc.Inbox(ctx, usecase.InboxRequest{})
	wantOK(t, err)
	// jane: no chat and create_chat false -> skipped; bob: created; dev: read.
	if got := fmt.Sprint(ids(res.Items)); got != "[chat:dev/c1 user:bob/b1]" && got != "[user:bob/b1 chat:dev/c1]" {
		t.Fatalf("items = %s", got)
	}
	if res.Skipped["user:jane:no_chat"] != 1 {
		t.Fatalf("skipped = %v", res.Skipped)
	}
	if ev := lastEvent(t, e); ev.Extra["skipped"] != "user:jane:no_chat" {
		t.Fatalf("audit = %+v", ev)
	}
	if e.Cursors.ChatIDs["user:bob"] != "created-"+usecasetest.BobID {
		t.Fatalf("not cached: %v", e.Cursors.ChatIDs)
	}
	// Direct items come from user destinations.
	for _, it := range res.Items {
		if it.ID == "user:bob/b1" && it.Conversation.Type != domain.KindUser {
			t.Fatalf("conversation = %+v", it.Conversation)
		}
	}
}

func TestInboxUserChatStaleCacheReResolvedOnce(t *testing.T) {
	e := envWith(t, watchOnly("user:jane"))
	e.Cursors.ChatIDs = map[domain.Alias]string{"user:jane": "stale"}
	e.Graph.UserChats = map[string]string{usecasetest.JaneID: "fresh"}
	e.Graph.ChatMessages = map[string][]domain.RawMessage{"fresh": {usecasetest.Msg("j1", usecasetest.StrangerID, "hi", ago(time.Minute))}}
	e.Graph.Once = map[string][]error{"ListChatMessages": {domain.NewNotFound("chat gone", "")}}
	res, err := e.Svc.Inbox(ctx, usecase.InboxRequest{})
	wantOK(t, err)
	if len(res.Items) != 1 || e.Graph.CallCount("ListChatMessages") != 2 || e.Graph.CallCount("ResolveUserChat") != 1 {
		t.Fatalf("items=%v calls=%v", ids(res.Items), e.Graph.Calls)
	}
	if len(e.Cursors.Dropped) != 1 || e.Cursors.ChatIDs["user:jane"] != "fresh" {
		t.Fatalf("dropped=%v cache=%v", e.Cursors.Dropped, e.Cursors.ChatIDs)
	}
	// Second 404 after re-resolve is not retried again.
	e2 := envWith(t, watchOnly("user:jane"))
	e2.Cursors.ChatIDs = map[domain.Alias]string{"user:jane": "stale"}
	e2.Graph.UserChats = map[string]string{usecasetest.JaneID: "fresh"}
	e2.Graph.Fail = map[string]error{"ListChatMessages": domain.NewNotFound("chat gone", "")}
	_, err = e2.Svc.Inbox(ctx, usecase.InboxRequest{})
	wantExit(t, err, exitNotFound)
	if e2.Graph.CallCount("ListChatMessages") != 2 {
		t.Fatalf("calls = %v", e2.Graph.Calls)
	}
	// Stale cache and the chat no longer exists: skipped, not fatal.
	e3 := envWith(t, watchOnly("user:jane"))
	e3.Cursors.ChatIDs = map[domain.Alias]string{"user:jane": "stale"}
	e3.Graph.Once = map[string][]error{"ListChatMessages": {domain.NewNotFound("chat gone", "")}}
	res3, err := e3.Svc.Inbox(ctx, usecase.InboxRequest{})
	wantOK(t, err)
	if res3.Skipped["user:jane:no_chat"] != 1 {
		t.Fatalf("skipped = %v", res3.Skipped)
	}
}

func TestInboxChatNotFoundPropagates(t *testing.T) {
	e := envWith(t, watchOnly("chat:dev"))
	e.Graph.Fail = map[string]error{"ListChatMessages": domain.NewNotFound("gone", "")}
	_, err := e.Svc.Inbox(ctx, usecase.InboxRequest{})
	wantExit(t, err, exitNotFound)
	if e.Graph.CallCount("ResolveUserChat") != 0 {
		t.Fatal("re-resolved a non-user chat")
	}
}

func TestInboxResolveErrorOtherThanNotFound(t *testing.T) {
	e := envWith(t, watchOnly("user:jane"))
	e.Graph.Fail = map[string]error{"ResolveUserChat": errAmbiguous}
	_, err := e.Svc.Inbox(ctx, usecase.InboxRequest{})
	wantExit(t, err, exitGeneral)
	// Fresh re-resolve error after a stale 404.
	e2 := envWith(t, watchOnly("user:jane"))
	e2.Cursors.ChatIDs = map[domain.Alias]string{"user:jane": "stale"}
	e2.Graph.Once = map[string][]error{"ListChatMessages": {domain.NewNotFound("gone", "")}}
	e2.Graph.Fail = map[string]error{"ResolveUserChat": errAmbiguous}
	_, err = e2.Svc.Inbox(ctx, usecase.InboxRequest{})
	wantExit(t, err, exitGeneral)
}

// B8 (AC-14): edit after ack is re-delivered; ack hides; --since replays.
func TestInboxAckEditAndReplay(t *testing.T) {
	e := envWith(t, watchOnly("chat:dev"))
	m := usecasetest.Msg("m1", usecasetest.StrangerID, "v1", ago(10*time.Minute))
	devChat(e, m)
	res, err := e.Svc.Inbox(ctx, usecase.InboxRequest{})
	wantOK(t, err)
	if len(res.Items) != 1 || res.Items[0].Edited {
		t.Fatalf("items = %+v", res.Items)
	}
	a, err := e.Svc.Ack(ctx, usecase.AckRequest{IDs: []string{res.Items[0].ID}})
	wantOK(t, err)
	if a.Acked != 1 {
		t.Fatalf("ack = %+v", a)
	}
	res, err = e.Svc.Inbox(ctx, usecase.InboxRequest{})
	wantOK(t, err)
	if len(res.Items) != 0 || res.Items == nil {
		t.Fatalf("acked item came back: %+v", res.Items)
	}
	cs, _ := e.Cursors.Get(ctx, "chat:dev")
	wm := cs.Watermark
	// --since replays an acked message without touching state.
	since := ago(20 * time.Minute)
	res, err = e.Svc.Inbox(ctx, usecase.InboxRequest{Since: &since})
	wantOK(t, err)
	if len(res.Items) != 1 {
		t.Fatalf("replay items = %v", ids(res.Items))
	}
	cs2, _ := e.Cursors.Get(ctx, "chat:dev")
	if !cs2.Watermark.Equal(wm) || len(cs2.Acked) != len(cs.Acked) {
		t.Fatalf("replay changed state: %+v vs %+v", cs2, cs)
	}
	// Edit after ack: modified moves forward.
	e.Graph.ChatMessages[usecasetest.DevChatID][0].Modified = ago(2 * time.Minute)
	e.Graph.ChatMessages[usecasetest.DevChatID][0].BodyContent = "v2"
	res, err = e.Svc.Inbox(ctx, usecase.InboxRequest{})
	wantOK(t, err)
	if len(res.Items) != 1 || !res.Items[0].Edited || res.Items[0].Text != "v2" {
		t.Fatalf("edited = %+v", res.Items)
	}
}

func TestInboxSinceFloorIsLookback(t *testing.T) {
	e := envWith(t, watchOnly("chat:dev"))
	old := ago(3 * time.Hour)
	_, err := e.Svc.Inbox(ctx, usecase.InboxRequest{Since: &old})
	wantOK(t, err)
	if got := e.Graph.ListSince[0]; !got.Equal(t0.Add(-30 * time.Minute)) {
		t.Fatalf("since = %v", got)
	}
}

func TestInboxLookbackDefaultsAndCap(t *testing.T) {
	e := envWith(t, func(p *domain.Policy) { watchOnly("chat:dev")(p); p.Inbound.MaxLookback = 0 })
	_, _ = e.Svc.Inbox(ctx, usecase.InboxRequest{})
	if !e.Graph.ListSince[0].Equal(t0.Add(-30 * time.Minute)) {
		t.Fatalf("default lookback: %v", e.Graph.ListSince[0])
	}
	e2 := envWith(t, func(p *domain.Policy) { watchOnly("chat:dev")(p); p.Inbound.MaxLookback = 72 * time.Hour })
	_, _ = e2.Svc.Inbox(ctx, usecase.InboxRequest{})
	if !e2.Graph.ListSince[0].Equal(t0.Add(-24 * time.Hour)) {
		t.Fatalf("capped lookback: %v", e2.Graph.ListSince[0])
	}
}

func TestInboxChannelRepliesAndNoDelta(t *testing.T) {
	e := envWith(t, watchOnly("channel:alerts"))
	e.Graph.Delta = "delta-xyz"
	key := usecasetest.TeamID + "/" + usecasetest.ChannelID
	e.Graph.ChannelMessages = map[string][]domain.RawMessage{key: {usecasetest.Msg("top1", usecasetest.StrangerID, "top", ago(5*time.Minute))}}
	e.Graph.Replies = map[string][]domain.RawMessage{
		key + "/root1": {usecasetest.Msg("rep1", usecasetest.CommanderID, "reply", ago(3*time.Minute)), usecasetest.Msg("top1", usecasetest.StrangerID, "dup", ago(5*time.Minute))},
	}
	e.Ledger.Threads = map[string]time.Time{"channel:alerts/root1": t0, "chat:dev/chat": t0, "channel:other/zzz": t0, "garbage": t0}
	res, err := e.Svc.Inbox(ctx, usecase.InboxRequest{})
	wantOK(t, err)
	if got := fmt.Sprint(ids(res.Items)); got != "[channel:alerts/top1 channel:alerts/rep1]" {
		t.Fatalf("items = %s", got)
	}
	if res.Items[1].ThreadID != "channel:alerts/root1" {
		t.Fatalf("reply thread = %q", res.Items[1].ThreadID)
	}
	if e.Graph.CallCount("ListReplies") != 1 {
		t.Fatalf("calls = %v", e.Graph.Calls)
	}
	// Delta tokens would hide un-acked messages (D8), so none is sent or stored.
	if e.Graph.DeltaTokensSeen[0] != "" || len(e.Cursors.Deltas) != 0 {
		t.Fatalf("delta used: %v %v", e.Graph.DeltaTokensSeen, e.Cursors.Deltas)
	}
	// thread_poll_max 0 disables reply polling.
	e2 := envWith(t, func(p *domain.Policy) { watchOnly("channel:alerts")(p); p.Inbound.ThreadPollMax = 0 })
	e2.Ledger.Threads = map[string]time.Time{"channel:alerts/root1": t0}
	_, _ = e2.Svc.Inbox(ctx, usecase.InboxRequest{})
	if e2.Graph.CallCount("ListReplies") != 0 {
		t.Fatal("polled replies with thread_poll_max 0")
	}
	// Cap per destination.
	e3 := envWith(t, func(p *domain.Policy) { watchOnly("channel:alerts")(p); p.Inbound.ThreadPollMax = 2 })
	e3.Ledger.Threads = map[string]time.Time{"channel:alerts/a": t0, "channel:alerts/b": t0, "channel:alerts/c": t0}
	_, _ = e3.Svc.Inbox(ctx, usecase.InboxRequest{})
	if e3.Graph.CallCount("ListReplies") != 2 {
		t.Fatalf("replies polled %d, want 2", e3.Graph.CallCount("ListReplies"))
	}
}

func TestInboxPortErrors(t *testing.T) {
	for name, mod := range map[string]func(*usecasetest.Env){
		"cursor get":     func(e *usecasetest.Env) { e.Cursors.Errs = map[string]error{"Get": errAmbiguous} },
		"record":         func(e *usecasetest.Env) { e.Cursors.Errs = map[string]error{"RecordDeliveries": errAmbiguous} },
		"channel list":   func(e *usecasetest.Env) { e.Graph.Fail = map[string]error{"ListChannelMessages": errAmbiguous} },
		"active threads": func(e *usecasetest.Env) { e.Ledger.Errs = map[string]error{"ActiveThreads": errAmbiguous} },
		"replies":        func(e *usecasetest.Env) { e.Graph.Fail = map[string]error{"ListReplies": errAmbiguous} },
	} {
		e := envWith(t, watchOnly("channel:alerts", "chat:dev"))
		key := usecasetest.TeamID + "/" + usecasetest.ChannelID
		e.Graph.ChannelMessages = map[string][]domain.RawMessage{key: {usecasetest.Msg("k", usecasetest.StrangerID, "x", ago(time.Minute))}}
		e.Ledger.Threads = map[string]time.Time{"channel:alerts/r": t0}
		mod(e)
		_, err := e.Svc.Inbox(ctx, usecase.InboxRequest{})
		if err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
