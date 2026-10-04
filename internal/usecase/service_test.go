package usecase_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/stainedhead/agent-cli-core/output"

	"github.com/stainedhead/teams-cli/internal/domain"
	"github.com/stainedhead/teams-cli/internal/usecase"
	"github.com/stainedhead/teams-cli/internal/usecase/usecasetest"
)

// B1: UPN mismatch exits 6 and no further Graph call is made.
func TestUPNMismatchBlocksEverything(t *testing.T) {
	e := newEnv(t)
	e.Graph.Profile.UPN = "someone-else@corp.example.com"
	_, err := e.Svc.Send(ctx, usecase.SendRequest{Alias: "chat:dev", Text: "hi"})
	wantExit(t, err, exitPolicy)
	if got := e.Graph.Calls; len(got) != 1 || got[0] != "Me" {
		t.Fatalf("graph calls = %v, want only Me", got)
	}
	// Sticky: later commands in the same run make no further Graph call.
	_, err = e.Svc.Inbox(ctx, usecase.InboxRequest{})
	wantExit(t, err, exitPolicy)
	_, err = e.Svc.Whoami(ctx)
	wantExit(t, err, exitPolicy)
	if len(e.Graph.Calls) != 1 {
		t.Fatalf("graph calls = %v", e.Graph.Calls)
	}
	if ev := lastEvent(t, e); ev.Decision != "deny:identity.upn_mismatch" || ev.Outcome != "denied" {
		t.Fatalf("audit = %+v", ev)
	}
}

func TestUPNCaseInsensitiveAndMeCachedOncePerRun(t *testing.T) {
	e := newEnv(t)
	e.Graph.Profile.UPN = strings.ToUpper(usecasetest.AgentUPN)
	for i := 0; i < 3; i++ {
		_, err := e.Svc.Whoami(ctx)
		wantOK(t, err)
	}
	if n := e.Graph.CallCount("Me"); n != 1 {
		t.Fatalf("Me called %d times, want 1", n)
	}
	if e.Provider.Calls != 1 {
		t.Fatalf("policy loaded %d times, want 1", e.Provider.Calls)
	}
}

func TestPolicyLoadFailureStopsRun(t *testing.T) {
	e := newEnv(t)
	e.Provider.Err = domain.NewValidation("policy invalid", "fix it")
	_, err := e.Svc.Whoami(ctx)
	wantExit(t, err, exitValidation)
	if len(e.Graph.Calls) != 0 {
		t.Fatalf("graph called: %v", e.Graph.Calls)
	}
	if ev := lastEvent(t, e); ev.Outcome != "error" || ev.Verb != "whoami" {
		t.Fatalf("audit = %+v", ev)
	}
}

func TestMeFailurePropagates(t *testing.T) {
	e := newEnv(t)
	e.Graph.Fail = map[string]error{"Me": domain.NewNotFound("gone", "")}
	_, err := e.Svc.Whoami(ctx)
	wantExit(t, err, exitNotFound)
}

// B1: every command, including failures, records exactly one audit event.
func TestAuditLinePerCommand(t *testing.T) {
	e := newEnv(t)
	e.Graph.Profile.UPN = usecasetest.AgentUPN
	steps := []struct {
		verb string
		run  func() error
	}{
		{"whoami", func() error { _, err := e.Svc.Whoami(ctx); return err }},
		{"destinations", func() error { _, err := e.Svc.Destinations(ctx); return err }},
		{"send", func() error {
			_, err := e.Svc.Send(ctx, usecase.SendRequest{Alias: "chat:dev", Text: "hi"})
			return err
		}},
		{"send", func() error {
			_, err := e.Svc.Send(ctx, usecase.SendRequest{Alias: "chat:nope", Text: "hi"})
			return err
		}},
		{"reply", func() error {
			_, err := e.Svc.Reply(ctx, usecase.ReplyRequest{ThreadID: "bad", Text: "hi"})
			return err
		}},
		{"inbox", func() error { _, err := e.Svc.Inbox(ctx, usecase.InboxRequest{}); return err }},
		{"ack", func() error { _, err := e.Svc.Ack(ctx, usecase.AckRequest{IDs: []string{"chat:dev/zzz"}}); return err }},
		{"thread_get", func() error {
			_, err := e.Svc.ThreadGet(ctx, usecase.ThreadRequest{ThreadID: "chat:dev/chat"})
			return err
		}},
		{"selftest", func() error { _, err := e.Svc.Selftest(ctx, usecase.SelftestRequest{ReadOnly: true}); return err }},
	}
	for i, st := range steps {
		_ = st.run()
		// Sends and replies that reach the POST also write an "intent" record
		// first (FR-R5); there is still exactly one final event per command.
		var finals []domain.AuditEvent
		for _, ev := range e.Audit.Events {
			if ev.Outcome != "intent" {
				finals = append(finals, ev)
			}
		}
		if len(finals) != i+1 || finals[i].Verb != st.verb {
			t.Fatalf("step %d (%s): events = %+v", i, st.verb, e.Audit.Events)
		}
	}
	// Failure outcomes are classified.
	var finals []domain.AuditEvent
	for _, ev := range e.Audit.Events {
		if ev.Outcome != "intent" {
			finals = append(finals, ev)
		}
	}
	if finals[3].Outcome != "denied" || finals[4].Outcome != "error" {
		t.Fatalf("outcomes: %+v / %+v", finals[3], finals[4])
	}
}

func TestAuditFailureBlocksSuccessButNotOriginalError(t *testing.T) {
	e := newEnv(t)
	e.Audit.Err = errors.New("disk full")
	_, err := e.Svc.Whoami(ctx)
	if err == nil || !strings.Contains(err.Error(), "audit write failed") {
		t.Fatalf("err = %v", err)
	}
	e.Provider.Err = domain.NewValidation("bad policy", "")
	e2 := usecasetest.NewEnv(usecasetest.Policy())
	e2.Audit.Err = errors.New("disk full")
	e2.Provider.Err = domain.NewValidation("bad policy", "")
	_, err = e2.Svc.Whoami(ctx)
	wantExit(t, err, exitValidation)
}

func TestMissingDependencyDoesNotPanic(t *testing.T) {
	deps := []func(*usecase.Deps){
		func(d *usecase.Deps) { d.Policy = nil }, func(d *usecase.Deps) { d.Graph = nil },
		func(d *usecase.Deps) { d.Ledger = nil }, func(d *usecase.Deps) { d.Cursors = nil },
		func(d *usecase.Deps) { d.Audit = nil }, func(d *usecase.Deps) { d.Clock = nil },
		func(d *usecase.Deps) { d.Rand = nil },
	}
	for i, mod := range deps {
		e := newEnv(t)
		d := usecase.Deps{Policy: e.Provider, Graph: e.Graph, Ledger: e.Ledger, Cursors: e.Cursors, Audit: e.Audit, Clock: e.Clock, Rand: e.Rand}
		mod(&d)
		_, err := usecase.New(d).Whoami(ctx)
		if err == nil || !strings.Contains(err.Error(), "not configured") {
			t.Fatalf("case %d: err = %v", i, err)
		}
	}
}

func TestAuditRecordedEvenWhenContextCancelled(t *testing.T) {
	e := newEnv(t)
	c, cancel := contextCancelled()
	defer cancel()
	_, _ = e.Svc.Destinations(c)
	if len(e.Audit.Events) != 1 {
		t.Fatalf("events = %d", len(e.Audit.Events))
	}
}

// statusErr is a Graph failure carrying its HTTP status (FR-R7).
type statusErr struct {
	cat    output.Category
	status int
}

func (e statusErr) Error() string             { return "graph failed" }
func (e statusErr) Category() output.Category { return e.cat }
func (e statusErr) HTTPStatus() int           { return e.status }

// FR-R7: the status of the last Graph call reaches the audit event.
func TestAuditCarriesHTTPStatus(t *testing.T) {
	cases := []struct {
		name string
		fail error
		want int
	}{
		{"success", nil, 200},
		{"403", statusErr{output.CategoryForbidden, 403}, 403},
		{"429", statusErr{output.CategoryRateLimited, 429}, 429},
	}
	for _, tc := range cases {
		e := envWith(t, watchOnly("chat:dev"))
		if tc.fail != nil {
			e.Graph.Fail = map[string]error{"ListChatMessages": tc.fail}
		}
		_, _ = e.Svc.Inbox(ctx, usecase.InboxRequest{})
		evs := e.Audit.Events
		if got := evs[len(evs)-1].HTTPStatus; got != tc.want {
			t.Errorf("%s: HTTPStatus = %d, want %d", tc.name, got, tc.want)
		}
	}
	// A successful post records 201; a command with no Graph call records 0.
	e := newEnv(t)
	_, _ = e.Svc.Send(ctx, usecase.SendRequest{Alias: "chat:dev", Text: "hi"})
	if got := e.Audit.Events[len(e.Audit.Events)-1].HTTPStatus; got != 201 {
		t.Errorf("send HTTPStatus = %d", got)
	}
	_, _ = e.Svc.Destinations(ctx)
	if got := e.Audit.Events[len(e.Audit.Events)-1].HTTPStatus; got != 0 {
		t.Errorf("destinations HTTPStatus = %d", got)
	}
}
