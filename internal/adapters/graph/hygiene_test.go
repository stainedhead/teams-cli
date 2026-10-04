package graph_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/stainedhead/agent-cli-core/auth/authtest"
	"github.com/stainedhead/agent-cli-core/httpx"
	"github.com/stainedhead/teams-cli/internal/adapters/graph"
	"github.com/stainedhead/teams-cli/internal/adapters/graph/graphtest"
	"github.com/stainedhead/teams-cli/internal/domain"
)

// plantedToken is a fake token that must never appear in any error (FR-31).
const plantedToken = "PLANTED-eyJhbGciOi-token-zq9xk2"

type plantedAuth struct{}

func (plantedAuth) Authorize(_ context.Context, r *http.Request) error {
	r.Header.Set("Authorization", "Bearer "+plantedToken)
	return nil
}
func (plantedAuth) Refresh(context.Context) error { return nil }

func TestHygieneNoIDsBodiesOrTokensInErrors(t *testing.T) {
	const secretID = "19:SECRETCHATID@thread.v2"
	srv := graphtest.New(t)
	c, err := graph.New(graph.Config{Refresher: plantedAuth{}, BaseURL: srv.BaseURL(),
		HTTP: httpx.Config{Clock: &fakeClock{}, Jitter: -1, MaxRetries: -1}})
	if err != nil {
		t.Fatal(err)
	}
	// The server echoes the token it was sent in a header and body.
	for _, status := range []int{400, 401, 403, 404, 409, 429, 500, 503} {
		srv.AddRule(graphtest.Rule{Match: "", Status: status, Times: 1,
			Header: map[string]string{"X-Ms-Error-Code": plantedToken, "Request-Id": plantedToken},
			Body:   `{"error":{"message":"` + plantedToken + ` ` + graphtest.BodySentinel + `"}}`})
		var errs []error
		errs = append(errs, c.GetChat(ctx(), secretID))
		srv.AddRule(graphtest.Rule{Match: "", Status: status, Times: 1,
			Header: map[string]string{"X-Ms-Error-Code": plantedToken},
			Body:   graphtest.BodySentinel})
		_, e2 := c.PostChat(ctx(), secretID, domain.OutMessage{Text: "x"})
		errs = append(errs, e2)
		for _, e := range errs {
			if e == nil {
				t.Fatalf("status %d: nil error", status)
			}
			msg := e.Error()
			for _, bad := range []string{plantedToken, graphtest.BodySentinel, "SECRETCHATID", srv.BaseURL(), "127.0.0.1"} {
				if strings.Contains(msg, bad) {
					t.Errorf("status %d: error leaks %q: %s", status, bad, msg)
				}
			}
		}
	}
}

func TestHygieneChannelAndTransportErrors(t *testing.T) {
	e := newEnv(t, authtest.Valid)
	e.srv.AddRule(graphtest.Rule{Match: "GET /teams/TEAMSECRET", Status: 404})
	_, _, err := e.c.ListChannelMessages(ctx(), "TEAMSECRET", "CHANSECRET", "", zeroT(), 5)
	if err == nil || strings.Contains(err.Error(), "TEAMSECRET") || strings.Contains(err.Error(), "CHANSECRET") {
		t.Fatalf("err = %v", err)
	}
	// Invalid-id usage errors do not echo the id either.
	if err := e.c.GetChat(ctx(), "bad/ID-SECRET"); err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("err = %v", err)
	}
}
