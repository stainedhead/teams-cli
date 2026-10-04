package usecase_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stainedhead/agent-cli-core/output"

	"github.com/stainedhead/teams-cli/internal/domain"
	"github.com/stainedhead/teams-cli/internal/usecase"
	"github.com/stainedhead/teams-cli/internal/usecase/usecasetest"
)

var ctx = context.Background()

const (
	exitGeneral    = int(output.ExitGeneral)
	exitUsage      = int(output.ExitUsage)
	exitForbidden  = int(output.ExitForbidden)
	exitNotFound   = int(output.ExitNotFound)
	exitPolicy     = int(output.ExitPolicyDenied)
	exitConflict   = int(output.ExitConflict)
	exitRateLimit  = int(output.ExitRateLimited)
	exitValidation = int(output.ExitValidation)
	exitAuth       = int(output.ExitAuth)
)

func newEnv(t *testing.T) *usecasetest.Env {
	t.Helper()
	return usecasetest.NewEnv(usecasetest.Policy())
}

// envWith builds an Env after letting mod change the policy.
func envWith(t *testing.T, mod func(*domain.Policy)) *usecasetest.Env {
	t.Helper()
	p := usecasetest.Policy()
	mod(&p)
	return usecasetest.NewEnv(p)
}

func wantExit(t *testing.T, err error, want int) {
	t.Helper()
	if got := int(output.ExitOf(err)); got != want {
		t.Fatalf("exit = %d (err %v), want %d", got, err, want)
	}
}

func wantOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func lastEvent(t *testing.T, e *usecasetest.Env) domain.AuditEvent {
	t.Helper()
	if len(e.Audit.Events) == 0 {
		t.Fatal("no audit event")
	}
	return e.Audit.Events[len(e.Audit.Events)-1]
}

// graphWrites counts post calls.
func graphWrites(e *usecasetest.Env) int {
	return e.Graph.CallCount("PostChat") + e.Graph.CallCount("PostChannel")
}

func mustNotContain(t *testing.T, what, s string, bad ...string) {
	t.Helper()
	for _, b := range bad {
		if b != "" && strings.Contains(s, b) {
			t.Fatalf("%s leaks %q: %s", what, b, s)
		}
	}
}

var errAmbiguous = errors.New("read tcp: i/o timeout")

type retryAfterErr struct{ d time.Duration }

func (e *retryAfterErr) Error() string             { return "throttled" }
func (e *retryAfterErr) Category() output.Category { return output.CategoryRateLimited }
func (e *retryAfterErr) RetryAfter() time.Duration { return e.d }

var _ = usecase.RunInfo{}

func contextCancelled() (context.Context, context.CancelFunc) {
	c, cancel := context.WithCancel(ctx)
	cancel()
	return c, cancel
}

func hint(err error) string {
	var h output.Hinter
	if errors.As(err, &h) {
		return h.Hint()
	}
	return ""
}

func codeOf(err error) int { return int(output.ExitOf(err)) }
