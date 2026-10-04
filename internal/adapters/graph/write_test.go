package graph_test

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stainedhead/agent-cli-core/auth"
	"github.com/stainedhead/agent-cli-core/auth/authtest"
	"github.com/stainedhead/agent-cli-core/httpx"
	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/teams-cli/internal/adapters/graph"
	"github.com/stainedhead/teams-cli/internal/adapters/graph/graphtest"
	"github.com/stainedhead/teams-cli/internal/domain"
)

type postReq struct {
	Body struct {
		ContentType string `json:"contentType"`
		Content     string `json:"content"`
	} `json:"body"`
	Mentions []struct {
		ID          int    `json:"id"`
		MentionText string `json:"mentionText"`
		Mentioned   struct {
			User struct {
				ID, DisplayName, UserIdentityType string
			} `json:"user"`
		} `json:"mentioned"`
	} `json:"mentions"`
}

func lastPost(t *testing.T, e *env) (graphtest.Request, postReq) {
	t.Helper()
	var last graphtest.Request
	for _, r := range e.srv.Requests() {
		if r.Method == http.MethodPost {
			last = r
		}
	}
	var p postReq
	if err := json.Unmarshal([]byte(last.Body), &p); err != nil {
		t.Fatalf("post body %q: %v", last.Body, err)
	}
	return last, p
}

// TestAssumedPostChat: UA-1, FR-4, unverified against a real tenant.
func TestAssumedPostChat(t *testing.T) {
	e := newEnv(t, authtest.Valid)
	e.srv.AddChat("19:c1@thread.v2")
	res, err := e.c.PostChat(ctx(), "19:c1@thread.v2", domain.OutMessage{Text: "hello"})
	if err != nil || res.MessageID == "" || res.Created.IsZero() {
		t.Fatalf("%+v %v", res, err)
	}
	r, p := lastPost(t, e)
	if r.Path != "/chats/19:c1@thread.v2/messages" || p.Body.ContentType != "text" || p.Body.Content != "hello" || len(p.Mentions) != 0 {
		t.Fatalf("req = %+v %+v", r, p)
	}
	if !strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		t.Fatal("content type")
	}
}

// TestAssumedPostChatMentions: UA-1, FR-8, unverified against a real tenant.
func TestAssumedPostChatMentions(t *testing.T) {
	e := newEnv(t, authtest.Valid)
	e.srv.AddChat("c1")
	m := domain.OutMessage{Text: `<at id="0">Jane</at> hi`, HTML: true,
		Mentions: []domain.OutMention{{ID: 0, AADID: "11111111-1111-1111-1111-111111111111", DisplayName: "Jane"}}}
	if _, err := e.c.PostChat(ctx(), "c1", m); err != nil {
		t.Fatal(err)
	}
	_, p := lastPost(t, e)
	if p.Body.ContentType != "html" || p.Body.Content != m.Text || len(p.Mentions) != 1 ||
		p.Mentions[0].Mentioned.User.ID != "11111111-1111-1111-1111-111111111111" ||
		p.Mentions[0].MentionText != "Jane" || p.Mentions[0].Mentioned.User.UserIdentityType != "aadUser" {
		t.Fatalf("p = %+v", p)
	}
}

func TestPostValidation(t *testing.T) {
	e := newEnv(t, authtest.Valid)
	if _, err := e.c.PostChat(ctx(), "c1", domain.OutMessage{Text: "  "}); output.ExitOf(err) != 9 {
		t.Fatalf("empty: %v", err)
	}
	bad := domain.OutMessage{Text: "x", Mentions: []domain.OutMention{{AADID: "a"}}}
	if _, err := e.c.PostChat(ctx(), "c1", bad); output.ExitOf(err) != 9 {
		t.Fatalf("mentions plain: %v", err)
	}
	if len(e.srv.Requests()) != 0 {
		t.Fatal("request made")
	}
}

func TestPostMarkerPlainTextEscapedAndHTML(t *testing.T) {
	e := newEnv(t, authtest.Valid)
	e.srv.AddChat("c1")
	if _, err := e.c.PostChat(ctx(), "c1", domain.OutMessage{Text: "a <b> & c\nd", MarkerKey: "abc123"}); err != nil {
		t.Fatal(err)
	}
	_, p := lastPost(t, e)
	want := `a &lt;b&gt; &amp; c<br>d<span data-teams-cli-key="abc123"></span>`
	if p.Body.ContentType != "html" || p.Body.Content != want {
		t.Fatalf("body = %+v", p.Body)
	}
	if _, err := e.c.PostChat(ctx(), "c1", domain.OutMessage{Text: "<p>x</p>", HTML: true, MarkerKey: "k"}); err != nil {
		t.Fatal(err)
	}
	_, p = lastPost(t, e)
	if p.Body.Content != `<p>x</p><span data-teams-cli-key="k"></span>` {
		t.Fatalf("body = %+v", p.Body)
	}
}

// TestAssumedPostChannel: UA-1, FR-4, unverified against a real tenant.
func TestAssumedPostChannel(t *testing.T) {
	e := newEnv(t, authtest.Valid)
	res, err := e.c.PostChannel(ctx(), "t1", "ch1", "", domain.OutMessage{Text: "top"})
	if err != nil || res.MessageID == "" || res.ThreadID != res.MessageID {
		t.Fatalf("%+v %v", res, err)
	}
	r, _ := lastPost(t, e)
	if r.Path != "/teams/t1/channels/ch1/messages" {
		t.Fatalf("path %s", r.Path)
	}
}

// TestAssumedPostChannelReply: UA-1 reply route, unverified against a real tenant.
func TestAssumedPostChannelReply(t *testing.T) {
	e := newEnv(t, authtest.Valid)
	res, err := e.c.PostChannel(ctx(), "t1", "ch1", "root1", domain.OutMessage{Text: "re"})
	if err != nil || res.ThreadID != "root1" || res.MessageID == "" {
		t.Fatalf("%+v %v", res, err)
	}
	r, _ := lastPost(t, e)
	if r.Path != "/teams/t1/channels/ch1/messages/root1/replies" {
		t.Fatalf("path %s", r.Path)
	}
}

func TestNotSentClassification(t *testing.T) {
	cases := []struct {
		name    string
		rule    graphtest.Rule
		notSent bool
		exit    int
	}{
		{"400", graphtest.Rule{Status: 400}, true, 9},
		{"403", graphtest.Rule{Status: 403}, true, 4},
		{"404", graphtest.Rule{Status: 404}, true, 5},
		{"429", graphtest.Rule{Status: 429, RetryAfter: "1"}, true, 8},
		{"500", graphtest.Rule{Status: 500}, false, 1},
		{"502", graphtest.Rule{Status: 502}, false, 8},
		{"503", graphtest.Rule{Status: 503}, false, 8},
	}
	for _, tc := range cases {
		e := newEnv(t, authtest.Valid)
		tc.rule.Match = "POST /chats/c1/messages"
		e.srv.AddRule(tc.rule)
		_, err := e.c.PostChat(ctx(), "c1", domain.OutMessage{Text: "x"})
		if err == nil || domain.IsNotSent(err) != tc.notSent || int(output.ExitOf(err)) != tc.exit {
			t.Errorf("%s: notSent=%v exit=%d err=%v", tc.name, domain.IsNotSent(err), output.ExitOf(err), err)
		}
		if e.srv.Count("POST /chats/c1/messages") != 1 {
			t.Errorf("%s: retried a write", tc.name)
		}
	}
}

func TestPost401TwiceIsNotSentAuth(t *testing.T) {
	e := newEnv(t, authtest.Valid)
	e.srv.Unauthorized401(5)
	_, err := e.c.PostChat(ctx(), "c1", domain.OutMessage{Text: "x"})
	if !domain.IsNotSent(err) || output.ExitOf(err) != 3 {
		t.Fatalf("%v", err)
	}
}

func TestPost401RefreshesAndPosts(t *testing.T) {
	e := newEnv(t, authtest.Valid)
	e.srv.AddChat("c1")
	e.srv.Unauthorized401(1)
	if _, err := e.c.PostChat(ctx(), "c1", domain.OutMessage{Text: "x"}); err != nil {
		t.Fatal(err)
	}
	if e.fake.Refreshes() != 1 {
		t.Fatal("no refresh")
	}
}

func TestPostDialFailureIsNotSent(t *testing.T) {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := l.Addr().String()
	_ = l.Close()
	src, _ := auth.NewDaemonTokenSource(authtest.New(authtest.Valid), "msgraph")
	c, err := graph.New(graph.Config{Refresher: auth.NewAuthorizer(src), BaseURL: "http://" + addr + "/v1.0",
		HTTP: httpx.Config{Clock: &fakeClock{}, MaxRetries: -1}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.PostChat(ctx(), "c1", domain.OutMessage{Text: "x"})
	if err == nil || !domain.IsNotSent(err) {
		t.Fatalf("dial: notSent=%v err=%v", domain.IsNotSent(err), err)
	}
}

func TestPostTimeoutIsAmbiguous(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-block }))
	defer srv.Close()
	defer close(block)
	src, _ := auth.NewDaemonTokenSource(authtest.New(authtest.Valid), "msgraph")
	c, _ := graph.New(graph.Config{Refresher: auth.NewAuthorizer(src), BaseURL: srv.URL + "/v1.0",
		HTTP: httpx.Config{Clock: &fakeClock{}, MaxRetries: -1}})
	cx, cancel := contextTimeout(50 * time.Millisecond)
	defer cancel()
	_, err := c.PostChat(cx, "c1", domain.OutMessage{Text: "x"})
	if err == nil || domain.IsNotSent(err) {
		t.Fatalf("ambiguous expected: %v", err)
	}
}

func TestPostMalformedSuccessIsAmbiguous(t *testing.T) {
	e := newEnv(t, authtest.Valid)
	e.srv.AddRule(graphtest.Rule{Match: "POST /chats/c1/messages", Status: 201, Body: "{broken"})
	_, err := e.c.PostChat(ctx(), "c1", domain.OutMessage{Text: "x"})
	if err == nil || domain.IsNotSent(err) {
		t.Fatalf("err = %v", err)
	}
}
