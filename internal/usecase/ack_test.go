package usecase_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stainedhead/teams-cli/internal/domain"
	"github.com/stainedhead/teams-cli/internal/usecase"
	"github.com/stainedhead/teams-cli/internal/usecase/usecasetest"
)

func seedAndInbox(t *testing.T, e *usecasetest.Env) []domain.InboundItem {
	t.Helper()
	devChat(e,
		usecasetest.Msg("m1", usecasetest.StrangerID, "one", ago(9*time.Minute)),
		usecasetest.Msg("m2", usecasetest.StrangerID, "two", ago(8*time.Minute)),
		usecasetest.Msg("m3", usecasetest.StrangerID, "three", ago(7*time.Minute)))
	res, err := e.Svc.Inbox(ctx, usecase.InboxRequest{})
	wantOK(t, err)
	if len(res.Items) != 3 {
		t.Fatalf("items = %v", ids(res.Items))
	}
	return res.Items
}

// B10 (AC-14): end-to-end inbox -> ack -> inbox on fakes.
func TestAckEndToEnd(t *testing.T) {
	e := envWith(t, watchOnly("chat:dev"))
	items := seedAndInbox(t, e)
	res, err := e.Svc.Ack(ctx, usecase.AckRequest{IDs: []string{items[0].ID, items[1].ID}})
	wantOK(t, err)
	if res.Acked != 2 || res.Already != 0 {
		t.Fatalf("res = %+v", res)
	}
	cs, _ := e.Cursors.Get(ctx, "chat:dev")
	if !cs.Watermark.Equal(ago(8 * time.Minute)) {
		t.Fatalf("watermark = %v (contiguous prefix)", cs.Watermark)
	}
	next, err := e.Svc.Inbox(ctx, usecase.InboxRequest{})
	wantOK(t, err)
	if got := ids(next.Items); len(got) != 1 || got[0] != "chat:dev/m3" {
		t.Fatalf("after ack: %v", got)
	}
	if ev := lastEvent(t, e); ev.Verb != "inbox" {
		t.Fatalf("audit = %+v", ev)
	}
	// Idempotent: re-acking is a no-op counted as already.
	res, err = e.Svc.Ack(ctx, usecase.AckRequest{IDs: []string{items[0].ID}})
	wantOK(t, err)
	if res.Acked != 0 || res.Already != 1 {
		t.Fatalf("res = %+v", res)
	}
	ev := lastEvent(t, e)
	if ev.Verb != "ack" || ev.Resource != "chat:dev" || ev.Extra["already"] != "1" {
		t.Fatalf("audit = %+v", ev)
	}
	if e.Graph.CallCount("Me") != 1 {
		t.Fatal("ack must not need the identity call beyond inbox's")
	}
}

func TestAckIsLocalOnly(t *testing.T) {
	e := envWith(t, watchOnly("chat:dev"))
	items := seedAndInbox(t, e)
	e2 := e.NewService() // fresh run, no Me cached
	before := len(e.Graph.Calls)
	_, err := e2.Ack(ctx, usecase.AckRequest{IDs: []string{items[2].ID}})
	wantOK(t, err)
	if len(e.Graph.Calls) != before {
		t.Fatalf("ack made graph calls: %v", e.Graph.Calls[before:])
	}
}

func TestAckUnknownIDsChangeNothing(t *testing.T) {
	e := envWith(t, watchOnly("chat:dev", "channel:alerts"))
	items := seedAndInbox(t, e)
	_, err := e.Svc.Ack(ctx, usecase.AckRequest{IDs: []string{items[0].ID, "chat:dev/never-delivered", "channel:alerts/also-unknown"}})
	wantExit(t, err, exitValidation)
	if !strings.Contains(err.Error(), "chat:dev/never-delivered") || !strings.Contains(err.Error(), "channel:alerts/also-unknown") {
		t.Fatalf("err = %v", err)
	}
	cs, _ := e.Cursors.Get(ctx, "chat:dev")
	if len(cs.Acked) != 0 || !cs.Watermark.IsZero() {
		t.Fatalf("state changed: %+v", cs)
	}
}

func TestAckManyUnknownIsBounded(t *testing.T) {
	e := envWith(t, watchOnly("chat:dev"))
	var in []string
	for i := 0; i < 30; i++ {
		in = append(in, "chat:dev/x"+strings.Repeat("a", i+1))
	}
	_, err := e.Svc.Ack(ctx, usecase.AckRequest{IDs: in})
	wantExit(t, err, exitValidation)
	if !strings.Contains(err.Error(), "and 20 more") {
		t.Fatalf("err = %v", err)
	}
}

func TestAckInputValidation(t *testing.T) {
	e := newEnv(t)
	over := make([]string, 101)
	for i := range over {
		over[i] = "chat:dev/a"
	}
	for _, c := range []struct {
		ids  []string
		want int
	}{
		{nil, exitUsage},
		{over, exitUsage},
		{[]string{"garbage"}, exitValidation},
		{[]string{"chat:nope/abc"}, exitPolicy},
		{[]string{"chat:writeonly/abc"}, exitPolicy},
	} {
		_, err := e.Svc.Ack(ctx, usecase.AckRequest{IDs: c.ids})
		wantExit(t, err, c.want)
	}
}

func TestAckAcrossAliasesAtomic(t *testing.T) {
	e := envWith(t, watchOnly("chat:dev", "chat:readonly"))
	devChat(e, usecasetest.Msg("a1", usecasetest.StrangerID, "x", ago(5*time.Minute)))
	e.Graph.ChatMessages["19:ro@thread.v2"] = []domain.RawMessage{usecasetest.Msg("b1", usecasetest.StrangerID, "y", ago(4*time.Minute))}
	res, err := e.Svc.Inbox(ctx, usecase.InboxRequest{})
	wantOK(t, err)
	if len(res.Items) != 2 {
		t.Fatalf("items = %v", ids(res.Items))
	}
	// Duplicated ids collapse; two aliases at once.
	ack, err := e.Svc.Ack(ctx, usecase.AckRequest{IDs: []string{"chat:dev/a1", "chat:dev/a1", "chat:readonly/b1"}})
	wantOK(t, err)
	if ack.Acked != 2 {
		t.Fatalf("ack = %+v", ack)
	}
	if lastEvent(t, e).Resource != "" {
		t.Fatalf("resource = %q", lastEvent(t, e).Resource)
	}
	// One unknown alias group blocks the whole call, including known ids.
	e2 := envWith(t, watchOnly("chat:dev", "chat:readonly"))
	devChat(e2, usecasetest.Msg("a1", usecasetest.StrangerID, "x", ago(5*time.Minute)))
	_, _ = e2.Svc.Inbox(ctx, usecase.InboxRequest{})
	_, err = e2.Svc.Ack(ctx, usecase.AckRequest{IDs: []string{"chat:dev/a1", "chat:readonly/zz"}})
	wantExit(t, err, exitValidation)
	cs, _ := e2.Cursors.Get(ctx, "chat:dev")
	if len(cs.Acked) != 0 {
		t.Fatal("partial ack")
	}
}

func TestAckPortErrors(t *testing.T) {
	e := envWith(t, watchOnly("chat:dev"))
	items := seedAndInbox(t, e)
	e.Cursors.Errs = map[string]error{"Get": errAmbiguous}
	_, err := e.Svc.Ack(ctx, usecase.AckRequest{IDs: []string{items[0].ID}})
	wantExit(t, err, exitGeneral)
	e.Cursors.Errs = map[string]error{"Ack": errAmbiguous}
	_, err = e.Svc.Ack(ctx, usecase.AckRequest{IDs: []string{items[0].ID}})
	wantExit(t, err, exitGeneral)
	e.Provider.Err = nil
	e3 := envWith(t, watchOnly("chat:dev"))
	e3.Provider.Err = errAmbiguous
	_, err = e3.Svc.Ack(ctx, usecase.AckRequest{IDs: []string{"chat:dev/a"}})
	wantExit(t, err, exitGeneral)
}

// The store may still report unknown ids (a race with pruning); nothing is acked.
type racyCursors struct{ *usecasetest.CursorStore }

func (r racyCursors) Ack(c context.Context, a domain.Alias, ids []string, now time.Time) (int, int, []string, error) {
	return 0, 0, ids[:1], nil
}

func TestAckStoreReportsUnknown(t *testing.T) {
	e := envWith(t, watchOnly("chat:dev"))
	items := seedAndInbox(t, e)
	svc := usecase.New(usecase.Deps{Policy: e.Provider, Graph: e.Graph, Ledger: e.Ledger, Cursors: racyCursors{e.Cursors}, Audit: e.Audit, Clock: e.Clock, Rand: e.Rand})
	_, err := svc.Ack(ctx, usecase.AckRequest{IDs: []string{items[0].ID}})
	wantExit(t, err, exitValidation)
}
