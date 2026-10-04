package graph_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/stainedhead/agent-cli-core/auth/authtest"
	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/teams-cli/internal/adapters/graph"
	"github.com/stainedhead/teams-cli/internal/adapters/graph/graphtest"
	"github.com/stainedhead/teams-cli/internal/domain"
)

func TestNewRejectsBadBaseURL(t *testing.T) {
	for _, u := range []string{"ftp://x", "not a url", "https://"} {
		if _, err := graph.New(graph.Config{BaseURL: u}); err == nil {
			t.Errorf("%q accepted", u)
		}
	}
	if _, err := graph.New(graph.Config{}); err != nil {
		t.Fatalf("default base: %v", err)
	}
}

func TestUnauthorizedRefreshThenSuccess(t *testing.T) {
	e := newEnv(t, authtest.Valid)
	e.srv.Unauthorized401(1)
	if _, err := e.c.Me(ctx()); err != nil {
		t.Fatal(err)
	}
	if e.fake.Refreshes() != 1 {
		t.Fatalf("refreshes = %d", e.fake.Refreshes())
	}
}

func TestUnauthorizedTwiceIsExit3(t *testing.T) {
	e := newEnv(t, authtest.Valid)
	e.srv.Unauthorized401(5)
	_, err := e.c.Me(ctx())
	if output.ExitOf(err) != 3 {
		t.Fatalf("exit = %d (%v)", output.ExitOf(err), err)
	}
}

func TestForbiddenIsExit4(t *testing.T) {
	e := newEnv(t, authtest.Valid)
	e.srv.AddRule(graphtest.Rule{Match: "GET /me", Status: 403})
	_, err := e.c.Me(ctx())
	if output.ExitOf(err) != 4 {
		t.Fatalf("exit = %d (%v)", output.ExitOf(err), err)
	}
}

func TestUnreachableDaemonIsExit3(t *testing.T) {
	e := newEnv(t, authtest.Unreachable)
	if _, err := e.c.Me(ctx()); output.ExitOf(err) != 3 {
		t.Fatalf("exit = %d (%v)", output.ExitOf(err), err)
	}
}

func TestThrottleRetriesReadsNotWrites(t *testing.T) {
	e := newEnv(t, authtest.Valid)
	e.srv.AddRule(graphtest.Rule{Match: "GET /me", Status: 429, RetryAfter: "2", Times: 1})
	if _, err := e.c.Me(ctx()); err != nil {
		t.Fatalf("read should retry: %v", err)
	}
	if e.clk.count() != 1 || e.srv.Count("GET /me") != 2 {
		t.Fatalf("sleeps=%d requests=%d", e.clk.count(), e.srv.Count("GET /me"))
	}

	e.srv.AddRule(graphtest.Rule{Match: "POST /chats/c1/messages", Status: 429, RetryAfter: "2"})
	_, err := e.c.PostChat(ctx(), "c1", domain.OutMessage{Text: "hi"})
	if output.ExitOf(err) != 8 || !domain.IsNotSent(err) {
		t.Fatalf("write 429: exit=%d notsent=%v err=%v", output.ExitOf(err), domain.IsNotSent(err), err)
	}
	if n := e.srv.Count("POST /chats/c1/messages"); n != 1 {
		t.Fatalf("write attempted %d times", n)
	}
}

func TestStatusMapping(t *testing.T) {
	cases := []struct {
		status int
		exit   int
	}{{404, 5}, {410, 5}, {409, 7}, {412, 7}, {400, 9}, {500, 1}}
	for _, tc := range cases {
		e := newEnv(t, authtest.Valid)
		e.srv.AddRule(graphtest.Rule{Match: "GET /chats/c1", Status: tc.status})
		err := e.c.GetChat(ctx(), "c1")
		if int(output.ExitOf(err)) != tc.exit {
			t.Errorf("%d: exit %d (%v)", tc.status, output.ExitOf(err), err)
		}
	}
}

func TestMalformedAndOversizeJSON(t *testing.T) {
	e := newEnv(t, authtest.Valid)
	e.srv.AddRule(graphtest.Rule{Match: "GET /me", Status: 200, Body: "{not json"})
	if _, err := e.c.Me(ctx()); err == nil || !strings.Contains(err.Error(), "malformed") {
		t.Fatalf("err = %v", err)
	}
}

func TestInvalidIDsRefusedBeforeRequest(t *testing.T) {
	e := newEnv(t, authtest.Valid)
	for _, id := range []string{"", " ", "a/b", "..", "a?b", "a b", "x'y", "a%2fb", strings.Repeat("a", 600)} {
		if err := e.c.GetChat(ctx(), id); err == nil {
			t.Errorf("%q accepted", id)
		}
	}
	if len(e.srv.Requests()) != 0 {
		t.Fatalf("requests made: %d", len(e.srv.Requests()))
	}
	if _, err := e.c.ListChatMessages(ctx(), "a/b", t0, 5); err == nil {
		t.Error("list accepted bad id")
	}
	if _, err := e.c.ListReplies(ctx(), "t", "c", "../x", 5); err == nil {
		t.Error("replies accepted bad id")
	}
	if _, err := e.c.PostChannel(ctx(), "t", "c", "x/y", domain.OutMessage{Text: "a"}); err == nil {
		t.Error("post accepted bad thread")
	}
	if len(e.srv.Requests()) != 0 {
		t.Fatal("requests made")
	}
	var de *domain.Error
	if err := e.c.GetChat(ctx(), "a/b"); !errors.As(err, &de) {
		t.Fatalf("want domain.Error, got %T", err)
	}
}

func TestContextCancelled(t *testing.T) {
	e := newEnv(t, authtest.Valid)
	c, cancel := contextCancelled()
	defer cancel()
	if _, err := e.c.Me(c); !errors.Is(err, c.Err()) {
		t.Fatalf("err = %v", err)
	}
}

// FR-R7: every Graph failure exposes its HTTP status for the audit record.
func TestErrorsCarryHTTPStatus(t *testing.T) {
	type httpStatusError interface{ HTTPStatus() int }
	for _, status := range []int{403, 404, 429, 500} {
		e := newEnv(t, authtest.Valid)
		e.srv.AddRule(graphtest.Rule{Match: "POST /chats/c1/messages", Status: status, RetryAfter: "1"})
		_, err := e.c.PostChat(ctx(), "c1", domain.OutMessage{Text: "hi"})
		var hs httpStatusError
		if !errors.As(err, &hs) || hs.HTTPStatus() != status {
			t.Errorf("status %d: err %v does not expose it", status, err)
		}
	}
}
