package graph_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stainedhead/agent-cli-core/auth"
	"github.com/stainedhead/agent-cli-core/auth/authtest"
	"github.com/stainedhead/agent-cli-core/httpx"
	"github.com/stainedhead/teams-cli/internal/adapters/graph"
	"github.com/stainedhead/teams-cli/internal/adapters/graph/graphtest"
)

type fakeClock struct {
	mu     sync.Mutex
	sleeps []time.Duration
}

func (c *fakeClock) Now() time.Time { return time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC) }
func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	c.mu.Lock()
	c.sleeps = append(c.sleeps, d)
	c.mu.Unlock()
	return ctx.Err()
}
func (c *fakeClock) count() int { c.mu.Lock(); defer c.mu.Unlock(); return len(c.sleeps) }

type env struct {
	srv  *graphtest.Server
	c    *graph.Client
	clk  *fakeClock
	fake *authtest.Fake
}

func newEnv(t *testing.T, sc authtest.Scenario) *env {
	t.Helper()
	srv := graphtest.New(t)
	fake := authtest.New(sc)
	src, err := auth.NewDaemonTokenSource(fake, "msgraph")
	if err != nil {
		t.Fatal(err)
	}
	clk := &fakeClock{}
	c, err := graph.New(graph.Config{
		Refresher: auth.NewAuthorizer(src),
		BaseURL:   srv.BaseURL(),
		HTTP:      httpx.Config{Clock: clk, Jitter: -1},
	})
	if err != nil {
		t.Fatal(err)
	}
	return &env{srv: srv, c: c, clk: clk, fake: fake}
}

func ctx() context.Context { return context.Background() }

var t0 = time.Date(2026, 10, 3, 10, 0, 0, 0, time.UTC)

func contextCancelled() (context.Context, context.CancelFunc) {
	c, cancel := context.WithCancel(context.Background())
	cancel()
	return c, cancel
}

func countPath(e *env, path string) int {
	n := 0
	for _, r := range e.srv.Requests() {
		if r.Path == path {
			n++
		}
	}
	return n
}

func contextTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

func zeroT() time.Time { return time.Time{} }
