package graph_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stainedhead/agent-cli-core/auth/authtest"
	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/teams-cli/internal/adapters/graph/graphtest"
	"github.com/stainedhead/teams-cli/internal/domain"
)

const (
	userA = "aaaaaaaa-0000-0000-0000-00000000000a"
	userB = "bbbbbbbb-0000-0000-0000-00000000000b"
)

// TestAssumedResolveUserChat: D6, UA-6, unverified against a real tenant.
func TestAssumedResolveUserChat(t *testing.T) {
	e := newEnv(t, authtest.Valid)
	e.srv.AddChat("19:other@unq", userB)
	e.srv.AddChat("19:mine@unq", userA)
	id, err := e.c.ResolveUserChat(ctx(), strings.ToUpper(userA), false)
	if err != nil || id != "19:mine@unq" {
		t.Fatalf("%q %v", id, err)
	}
	q := e.srv.Requests()[0].RawQuery
	if !strings.Contains(q, "$expand=members") || !strings.Contains(q, "chatType%20eq%20%27oneOnOne%27") {
		t.Fatalf("query %q", q)
	}
}

func TestResolveUserChatNotFoundNoCreate(t *testing.T) {
	e := newEnv(t, authtest.Valid)
	_, err := e.c.ResolveUserChat(ctx(), userA, false)
	if output.ExitOf(err) != 5 {
		t.Fatalf("%v", err)
	}
	if e.srv.Count("POST") != 0 {
		t.Fatal("created without opt-in")
	}
}

// TestAssumedCreateChat: UA-6 / UA-16 create opt-in, unverified against a real tenant.
func TestAssumedCreateChat(t *testing.T) {
	e := newEnv(t, authtest.Valid)
	id, err := e.c.ResolveUserChat(ctx(), userA, true)
	if err != nil || id == "" {
		t.Fatalf("%q %v", id, err)
	}
	var body struct {
		ChatType string
		Members  []map[string]any
	}
	for _, r := range e.srv.Requests() {
		if r.Method == "POST" {
			_ = json.Unmarshal([]byte(r.Body), &body)
		}
	}
	if body.ChatType != "oneOnOne" || len(body.Members) != 2 {
		t.Fatalf("body %+v", body)
	}
	if b := body.Members[1]["user@odata.bind"].(string); !strings.HasSuffix(b, "/users('"+userA+"')") {
		t.Fatalf("bind %s", b)
	}
	// a second call finds the created chat without another POST
	id2, err := e.c.ResolveUserChat(ctx(), userA, true)
	if err != nil || id2 != id || e.srv.Count("POST /chats") != 1 {
		t.Fatalf("second %q %v", id2, err)
	}
}

func TestCreateChatFailures(t *testing.T) {
	e := newEnv(t, authtest.Valid)
	e.srv.FailChatCreate(403)
	_, err := e.c.ResolveUserChat(ctx(), userA, true)
	if !domain.IsNotSent(err) || output.ExitOf(err) != 4 {
		t.Fatalf("%v", err)
	}
	e2 := newEnv(t, authtest.Valid)
	e2.srv.AddRule(graphtest.Rule{Match: "POST /chats", Status: 201, Body: "{}"})
	if _, err := e2.c.ResolveUserChat(ctx(), userA, true); err == nil {
		t.Fatal("empty id accepted")
	}
	e3 := newEnv(t, authtest.Valid)
	e3.srv.AddRule(graphtest.Rule{Match: "GET /me", Status: 200, Body: `{"id":"bad id"}`})
	if _, err := e3.c.ResolveUserChat(ctx(), userA, true); err == nil || e3.srv.Count("POST") != 0 {
		t.Fatalf("bad own id: %v", err)
	}
}

func TestResolveUserChatBadGUID(t *testing.T) {
	e := newEnv(t, authtest.Valid)
	for _, g := range []string{"", "x", userA + "x", "aaaaaaaa_0000-0000-0000-00000000000a", "gggggggg-0000-0000-0000-00000000000a", "aaaaaaaa-0000-0000-0000-00000000000a') or ('1"} {
		if _, err := e.c.ResolveUserChat(ctx(), g, true); output.ExitOf(err) != 2 {
			t.Errorf("%q: %v", g, err)
		}
	}
	if len(e.srv.Requests()) != 0 {
		t.Fatal("request made")
	}
}

func TestResolveUserChatScanBounded(t *testing.T) {
	e := newEnv(t, authtest.Valid)
	e.srv.SetPageSize(1)
	for i := 0; i < 12; i++ {
		e.srv.AddChat("19:c"+string(rune('a'+i))+"@unq", userB)
	}
	e.srv.AddChat("19:zzz@unq", userA) // sorted last: beyond the scan bound
	if _, err := e.c.ResolveUserChat(ctx(), userA, false); output.ExitOf(err) != 5 {
		t.Fatalf("%v", err)
	}
	if n := e.srv.Count("GET /me/chats"); n != 5 {
		t.Fatalf("pages = %d", n)
	}
}

func TestResolveUserChatOffHostLink(t *testing.T) {
	e := newEnv(t, authtest.Valid)
	e.srv.AddRule(graphtest.Rule{Match: "GET /me/chats", Status: 200, Body: `{"value":[],"@odata.nextLink":"https://evil.example.com/x"}`})
	if _, err := e.c.ResolveUserChat(ctx(), userA, false); err == nil || output.ExitOf(err) == 5 {
		t.Fatalf("%v", err)
	}
}

// TestAssumedGetChatProbe: UA-8, unverified against a real tenant.
func TestAssumedGetChatProbe(t *testing.T) {
	e := newEnv(t, authtest.Valid)
	e.srv.AddChat("c1")
	if err := e.c.GetChat(ctx(), "c1"); err != nil {
		t.Fatal(err)
	}
	e.srv.SetChatStatus("c1", 403)
	if output.ExitOf(e.c.GetChat(ctx(), "c1")) != 4 {
		t.Fatal("403 should be exit 4")
	}
	if output.ExitOf(e.c.GetChat(ctx(), "nochat")) != 5 {
		t.Fatal("404 should be exit 5")
	}
}

// TestAssumedFindByMarker: UA-9, unverified against a real tenant.
func TestAssumedFindByMarker(t *testing.T) {
	e := newEnv(t, authtest.Valid)
	for i := 0; i < 25; i++ {
		e.srv.AddChatMessage("c1", graphtest.Message{ID: "m" + string(rune('A'+i)), Created: t0.Add(time.Duration(i) * time.Minute), BodyType: "html", Content: "<p>x</p>"})
	}
	e.srv.AddChatMessage("c1", graphtest.Message{ID: "hit", Created: t0.Add(time.Hour), BodyType: "html", Content: `<p>x</p><span data-teams-cli-key="0123456789abcdef"></span>`})
	d := domain.Destination{Kind: domain.KindChat, ChatID: "c1"}
	id, ok, err := e.c.FindByMarker(ctx(), d, "0123456789abcdef")
	if err != nil || !ok || id != "hit" {
		t.Fatalf("%q %v %v", id, ok, err)
	}
	if !strings.Contains(e.srv.Requests()[0].RawQuery, "$top=20") {
		t.Fatalf("query %q", e.srv.Requests()[0].RawQuery)
	}
	if _, ok, _ := e.c.FindByMarker(ctx(), d, "ffffffffffffffff"); ok {
		t.Fatal("false positive")
	}
	if _, _, err := e.c.FindByMarker(ctx(), d, ""); err == nil {
		t.Fatal("empty marker accepted")
	}
}

func TestFindByMarkerChannelAndInconclusive(t *testing.T) {
	e := newEnv(t, authtest.Valid)
	e.srv.AddChannelMessage("t1", "ch1", graphtest.Message{ID: "p1", BodyType: "html", Content: `<span data-teams-cli-key="abc"></span>`, FromUserID: "u"})
	d := domain.Destination{Kind: domain.KindChannel, TeamID: "t1", ChannelID: "ch1"}
	if id, ok, err := e.c.FindByMarker(ctx(), d, "abc"); err != nil || !ok || id != "p1" {
		t.Fatalf("%q %v %v", id, ok, err)
	}
	e.srv.AddRule(graphtest.Rule{Match: "GET /teams/t1/channels/ch1/messages", Status: 400})
	if _, ok, err := e.c.FindByMarker(ctx(), d, "abc"); ok || err != nil {
		t.Fatalf("400 should be inconclusive: %v %v", ok, err)
	}
	e.srv.AddChat("c9")
	e.srv.SetChatStatus("c9", 403)
	if _, _, err := e.c.FindByMarker(ctx(), domain.Destination{Kind: domain.KindChat, ChatID: "c9"}, "abc"); output.ExitOf(err) != 4 {
		t.Fatalf("403 should surface: %v", err)
	}
	// unresolved user chat is inconclusive
	if _, ok, err := e.c.FindByMarker(ctx(), domain.Destination{Kind: domain.KindUser}, "abc"); ok || err != nil {
		t.Fatal("unresolved user chat")
	}
}
