package graph_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stainedhead/agent-cli-core/auth/authtest"
	"github.com/stainedhead/teams-cli/internal/adapters/graph/graphtest"
	"github.com/stainedhead/teams-cli/internal/domain"
)

// TestAssumedMe: UA-2, unverified against a real tenant.
func TestAssumedMe(t *testing.T) {
	e := newEnv(t, authtest.Valid)
	e.srv.SetMe("00000000-0000-0000-0000-0000000000aa", "Agent Smith", "smith@example.com")
	p, err := e.c.Me(ctx())
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "00000000-0000-0000-0000-0000000000aa" || p.DisplayName != "Agent Smith" || p.UPN != "smith@example.com" {
		t.Fatalf("profile = %+v", p)
	}
	r := e.srv.Requests()[0]
	if r.Path != "/me" || !strings.Contains(r.RawQuery, "$select=id,displayName,userPrincipalName") || !r.HasAuth {
		t.Fatalf("request = %+v", r)
	}
}

// TestAssumedListChatMessages: UA-1 and UA-7, unverified against a real tenant.
func TestAssumedListChatMessages(t *testing.T) {
	e := newEnv(t, authtest.Valid)
	for i := 0; i < 4; i++ {
		e.srv.AddChatMessage("19:c1@thread.v2", graphtest.Message{
			ID: string(rune('a' + i)), Created: t0.Add(time.Duration(i) * time.Minute),
			FromUserID: "u1", FromName: "Jane", FromTenantID: "tenant-x", Content: "hello", Mentions: []graphtest.Mention{{ID: 0, Text: "Agent", UserID: "agent"}},
		})
	}
	msgs, err := e.c.ListChatMessages(ctx(), "19:c1@thread.v2", t0.Add(30*time.Second), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 3 || msgs[0].ID != "d" || msgs[2].ID != "b" {
		t.Fatalf("msgs = %+v", msgs)
	}
	m := msgs[0]
	if m.FromKind != domain.SenderUser || m.FromUserID != "u1" || m.FromTenantID != "tenant-x" || m.FromName != "Jane" ||
		m.BodyType != "text" || m.BodyContent != "hello" || m.ChatID != "19:c1@thread.v2" ||
		len(m.Mentions) != 1 || m.Mentions[0].UserID != "agent" || m.MessageType != "message" || m.Deleted {
		t.Fatalf("mapped = %+v", m)
	}
	q := e.srv.Requests()[0].RawQuery
	for _, want := range []string{"$top=10", "$orderby=lastModifiedDateTime%20desc", "$filter=lastModifiedDateTime%20gt%202026-10-03T10%3A00%3A30Z"} {
		if !strings.Contains(q, want) {
			t.Errorf("query %q lacks %q", q, want)
		}
	}
}

func TestListChatMessagesPagingAndLimit(t *testing.T) {
	e := newEnv(t, authtest.Valid)
	e.srv.SetPageSize(2)
	for i := 0; i < 9; i++ {
		e.srv.AddChatMessage("c1", graphtest.Message{Created: t0.Add(time.Duration(i) * time.Minute), Content: "x"})
	}
	msgs, err := e.c.ListChatMessages(ctx(), "c1", time.Time{}, 5)
	if err != nil || len(msgs) != 5 {
		t.Fatalf("limit: %d %v", len(msgs), err)
	}
	if n := e.srv.Count("GET /chats/c1/messages"); n != 3 {
		t.Fatalf("pages = %d", n)
	}
	// page cap: limit 0 means unbounded but pages are capped at 5 (10 items).
	e.srv.SetPageSize(1)
	msgs, err = e.c.ListChatMessages(ctx(), "c1", time.Time{}, 0)
	if err != nil || len(msgs) != 5 {
		t.Fatalf("page cap: %d %v", len(msgs), err)
	}
}

func TestNextLinkOffHostRefused(t *testing.T) {
	e := newEnv(t, authtest.Valid)
	e.srv.AddRule(graphtest.Rule{Match: "GET /chats/c1/messages", Status: 200,
		Body: `{"value":[],"@odata.nextLink":"https://evil.example.com/v1.0/chats/c1/messages?$skiptoken=1"}`})
	if _, err := e.c.ListChatMessages(ctx(), "c1", time.Time{}, 5); err == nil || strings.Contains(err.Error(), "evil.example.com") {
		t.Fatalf("err = %v", err)
	}
	if e.srv.Count("GET /chats/c1/messages") != 1 {
		t.Fatal("followed off-host link")
	}
}

func TestNullSafeMapping(t *testing.T) {
	e := newEnv(t, authtest.Valid)
	e.srv.AddChatMessage("c1", graphtest.Message{ID: "sys", MessageType: "systemEventMessage", FromKind: "none", NullBody: true})
	e.srv.AddChatMessage("c1", graphtest.Message{ID: "bot", FromKind: "bot", FromUserID: "app1", FromName: "Bot", Content: "b"})
	e.srv.AddChatMessage("c1", graphtest.Message{ID: "app", FromKind: "application", FromUserID: "app2", Content: "a"})
	e.srv.AddChatMessage("c1", graphtest.Message{ID: "del", Deleted: true, FromUserID: "u", Content: ""})
	msgs, err := e.c.ListChatMessages(ctx(), "c1", time.Time{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]domain.RawMessage{}
	for _, m := range msgs {
		by[m.ID] = m
	}
	if s := by["sys"]; s.FromKind != domain.SenderUnknown || s.BodyContent != "" || s.MessageType != "systemEventMessage" {
		t.Errorf("sys = %+v", s)
	}
	if by["bot"].FromKind != domain.SenderBot || by["app"].FromKind != domain.SenderApplication || !by["del"].Deleted {
		t.Errorf("kinds: %+v", by)
	}
}

func TestListRepliesBounded(t *testing.T) {
	e := newEnv(t, authtest.Valid)
	e.srv.AddChannelMessage("t1", "ch1", graphtest.Message{ID: "root", Content: "r"})
	for i := 0; i < 4; i++ {
		e.srv.AddChannelMessage("t1", "ch1", graphtest.Message{ReplyToID: "root", Content: "rep", FromUserID: "u"})
	}
	msgs, err := e.c.ListReplies(ctx(), "t1", "ch1", "root", 3)
	if err != nil || len(msgs) != 3 {
		t.Fatalf("%d %v", len(msgs), err)
	}
	if msgs[0].ThreadID != "root" || msgs[0].TeamID != "t1" || msgs[0].ChannelID != "ch1" {
		t.Fatalf("mapped = %+v", msgs[0])
	}
	if !strings.Contains(e.srv.Requests()[0].Path, "/teams/t1/channels/ch1/messages/root/replies") {
		t.Fatalf("path = %s", e.srv.Requests()[0].Path)
	}
}

// TestAssumedChannelDelta: UA-3, unverified against a real tenant.
func TestAssumedChannelDelta(t *testing.T) {
	e := newEnv(t, authtest.Valid)
	e.srv.AddChannelMessage("t1", "ch1", graphtest.Message{ID: "m1", Created: t0, Content: "one", FromUserID: "u"})
	e.srv.AddChannelMessage("t1", "ch1", graphtest.Message{ID: "r1", ReplyToID: "m1", Created: t0, Content: "reply"})
	msgs, delta, err := e.c.ListChannelMessages(ctx(), "t1", "ch1", "", time.Time{}, 10)
	if err != nil || delta == "" {
		t.Fatalf("delta=%q err=%v", delta, err)
	}
	if len(msgs) != 1 || msgs[0].ID != "m1" || msgs[0].ThreadID != "m1" || msgs[0].TeamID != "t1" || msgs[0].ChannelID != "ch1" {
		t.Fatalf("msgs = %+v", msgs)
	}
	e.srv.AddChannelMessage("t1", "ch1", graphtest.Message{ID: "m2", Created: t0.Add(time.Minute), Content: "two", FromUserID: "u"})
	msgs, delta2, err := e.c.ListChannelMessages(ctx(), "t1", "ch1", delta, time.Time{}, 10)
	if err != nil || len(msgs) != 1 || msgs[0].ID != "m2" || delta2 == "" || delta2 == delta {
		t.Fatalf("second: %+v %q err=%v", msgs, delta2, err)
	}
}

func TestDeltaTokenForeignIgnored(t *testing.T) {
	e := newEnv(t, authtest.Valid)
	e.srv.AddChannelMessage("t1", "ch1", graphtest.Message{ID: "m1", Content: "x", FromUserID: "u"})
	for _, tok := range []string{
		"https://evil.example.com/v1.0/teams/t1/channels/ch1/messages/delta?$deltatoken=1",
		e.srv.BaseURL() + "/teams/OTHER/channels/ch1/messages/delta?$deltatoken=1",
		e.srv.BaseURL() + "/me?$deltatoken=1",
	} {
		msgs, _, err := e.c.ListChannelMessages(ctx(), "t1", "ch1", tok, time.Time{}, 10)
		if err != nil || len(msgs) != 1 {
			t.Fatalf("tok %q: %d %v", tok, len(msgs), err)
		}
	}
	for _, r := range e.srv.Requests() {
		if strings.Contains(r.Path, "OTHER") || r.Path == "/me" {
			t.Fatalf("followed foreign token: %s", r.Path)
		}
	}
}

// TestAssumedChannelDeltaFallback: UA-3 fallback, unverified against a real tenant.
func TestAssumedChannelDeltaFallback(t *testing.T) {
	for _, status := range []int{400, 404, 501} {
		e := newEnv(t, authtest.Valid)
		e.srv.DisableDelta(status)
		for i := 0; i < 4; i++ {
			e.srv.AddChannelMessage("t1", "ch1", graphtest.Message{Created: t0.Add(time.Duration(i) * time.Minute), Content: "x", FromUserID: "u"})
		}
		msgs, delta, err := e.c.ListChannelMessages(ctx(), "t1", "ch1", "", t0.Add(30*time.Second), 2)
		if err != nil || delta != "" || len(msgs) != 2 {
			t.Fatalf("status %d: %d %q %v", status, len(msgs), delta, err)
		}
		if !msgs[0].Modified.After(msgs[1].Modified) {
			t.Fatal("not newest first")
		}
		if countPath(e, "/teams/t1/channels/ch1/messages/delta") != 1 || countPath(e, "/teams/t1/channels/ch1/messages") != 1 {
			t.Fatalf("calls: %+v", e.srv.Requests())
		}
	}
}

func TestChannelDeltaErrorNotFallenBack(t *testing.T) {
	e := newEnv(t, authtest.Valid)
	e.srv.DisableDelta(403)
	if _, _, err := e.c.ListChannelMessages(ctx(), "t1", "ch1", "", time.Time{}, 5); err == nil {
		t.Fatal("expected error")
	}
	if countPath(e, "/teams/t1/channels/ch1/messages") != 0 {
		t.Fatal("fell back on 403")
	}
	if _, _, err := e.c.ListChannelMessages(ctx(), "t/1", "ch1", "", time.Time{}, 5); err == nil {
		t.Fatal("bad id accepted")
	}
}

func TestDeletedRemovedInDelta(t *testing.T) {
	e := newEnv(t, authtest.Valid)
	e.srv.AddRule(graphtest.Rule{Match: "GET /teams/t1/channels/ch1/messages/delta", Status: 200,
		Body: `{"value":[{"id":"m9","@removed":{"reason":"deleted"}}],"@odata.deltaLink":"` + e.srv.BaseURL() + `/teams/t1/channels/ch1/messages/delta?$deltatoken=9"}`})
	msgs, delta, err := e.c.ListChannelMessages(ctx(), "t1", "ch1", "", time.Time{}, 5)
	if err != nil || len(msgs) != 1 || !msgs[0].Deleted || delta == "" {
		t.Fatalf("%+v %q %v", msgs, delta, err)
	}
}
