package graphtest_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stainedhead/teams-cli/internal/adapters/graph/graphtest"
)

func get(t *testing.T, url string, auth bool) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	if auth {
		req.Header.Set("Authorization", "Bearer x")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestFakeBasics(t *testing.T) {
	s := graphtest.New(t)
	if c, _ := get(t, s.BaseURL()+"/me", false); c != 401 {
		t.Fatalf("no auth = %d", c)
	}
	c, b := get(t, s.BaseURL()+"/me", true)
	if c != 200 || !strings.Contains(b, "agent.one@example.com") {
		t.Fatalf("me = %d %s", c, b)
	}
	s.Unauthorized401(1)
	if c, _ := get(t, s.BaseURL()+"/me", true); c != 401 {
		t.Fatal("expected 401 once")
	}
	if c, _ := get(t, s.BaseURL()+"/me", true); c != 200 {
		t.Fatal("expected 200 after")
	}
	s.AddRule(graphtest.Rule{Match: "GET /me", Status: 429, RetryAfter: "1", Times: 1})
	if c, b := get(t, s.BaseURL()+"/me", true); c != 429 || !strings.Contains(b, graphtest.BodySentinel) {
		t.Fatalf("rule = %d %s", c, b)
	}
	if c, _ := get(t, s.BaseURL()+"/nope", true); c != 404 {
		t.Fatal("404 expected")
	}
	if s.Count("GET /me") != 5 {
		t.Fatalf("count = %d", s.Count("GET /me"))
	}
	for _, r := range s.Requests() {
		if r.Header.Get("Authorization") != "" {
			t.Fatal("authorization recorded")
		}
	}
}

func TestFakeChatPagingAndPost(t *testing.T) {
	s := graphtest.New(t)
	s.SetPageSize(2)
	for i := 0; i < 5; i++ {
		s.AddChatMessage("c1", graphtest.Message{Content: "m"})
	}
	_, b := get(t, s.BaseURL()+"/chats/c1/messages", true)
	if !strings.Contains(b, "nextLink") {
		t.Fatalf("expected nextLink: %s", b)
	}
	if c, _ := get(t, s.BaseURL()+"/chats/zz/messages", true); c != 404 {
		t.Fatal("unknown chat should 404")
	}
	s.SetChatStatus("c1", 403)
	if c, _ := get(t, s.BaseURL()+"/chats/c1", true); c != 403 {
		t.Fatal("forced status")
	}
}
