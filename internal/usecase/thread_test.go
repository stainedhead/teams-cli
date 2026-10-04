package usecase_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stainedhead/teams-cli/internal/domain"
	"github.com/stainedhead/teams-cli/internal/usecase"
	"github.com/stainedhead/teams-cli/internal/usecase/usecasetest"
)

// B11 (AC-16): channel thread context, oldest-first, classified, bounded.
func TestThreadGetChannel(t *testing.T) {
	e := newEnv(t)
	key := usecasetest.TeamID + "/" + usecasetest.ChannelID + "/root1"
	own := usecasetest.Msg("own", usecasetest.AgentID, "mine", ago(5*time.Minute))
	e.Graph.Replies = map[string][]domain.RawMessage{key: {
		usecasetest.Msg("r3", usecasetest.StrangerID, "c", ago(2*time.Minute)),
		usecasetest.Msg("r1", usecasetest.CommanderID, "a", ago(6*time.Minute)),
		usecasetest.Msg("r2", usecasetest.StrangerID, "b", ago(4*time.Minute)),
		own,
	}}
	res, err := e.Svc.ThreadGet(ctx, usecase.ThreadRequest{ThreadID: "channel:alerts/root1", Limit: 2})
	wantOK(t, err)
	if got := fmt.Sprint(ids(res.Items)); got != "[channel:alerts/r2 channel:alerts/r3]" {
		t.Fatalf("items = %s", got)
	}
	if res.Items[0].ThreadID != "channel:alerts/root1" {
		t.Fatalf("thread = %q", res.Items[0].ThreadID)
	}
	all, err := e.Svc.ThreadGet(ctx, usecase.ThreadRequest{ThreadID: "channel:alerts/root1"})
	wantOK(t, err)
	if len(all.Items) != 3 || !all.Items[0].Sender.CanInstruct { // own message dropped; r1 is the commander
		t.Fatalf("all = %+v", all.Items)
	}
	// No cursor, delivery or ack change.
	cs, _ := e.Cursors.Get(ctx, "channel:alerts")
	if len(cs.Delivered) != 0 || len(cs.Acked) != 0 || !cs.Watermark.IsZero() {
		t.Fatalf("cursor changed: %+v", cs)
	}
	if ev := lastEvent(t, e); ev.Verb != "thread_get" || ev.Resource != "channel:alerts" || ev.Extra["dropped"] != "1" {
		t.Fatalf("audit = %+v", ev)
	}
}

func TestThreadGetChatAndUser(t *testing.T) {
	e := newEnv(t)
	devChat(e, usecasetest.Msg("c1", usecasetest.StrangerID, "a", ago(3*time.Minute)), usecasetest.Msg("c2", usecasetest.StrangerID, "b", ago(2*time.Minute)))
	res, err := e.Svc.ThreadGet(ctx, usecase.ThreadRequest{ThreadID: "chat:dev/chat"})
	wantOK(t, err)
	if len(res.Items) != 2 || res.Items[0].ID != "chat:dev/c1" {
		t.Fatalf("items = %v", ids(res.Items))
	}
	e.Graph.UserChats = map[string]string{usecasetest.JaneID: "jc"}
	e.Graph.ChatMessages["jc"] = []domain.RawMessage{usecasetest.Msg("j1", usecasetest.StrangerID, "dm", ago(time.Minute))}
	res, err = e.Svc.ThreadGet(ctx, usecase.ThreadRequest{ThreadID: "user:jane/chat"})
	wantOK(t, err)
	if len(res.Items) != 1 || res.Items[0].Conversation.Type != domain.KindUser {
		t.Fatalf("items = %+v", res.Items)
	}
}

func TestThreadGetDenials(t *testing.T) {
	e := newEnv(t)
	for _, c := range []struct {
		r    usecase.ThreadRequest
		want int
	}{
		{usecase.ThreadRequest{ThreadID: "junk"}, exitValidation},
		{usecase.ThreadRequest{ThreadID: "chat:nope/chat"}, exitPolicy},
		{usecase.ThreadRequest{ThreadID: "chat:writeonly/chat"}, exitPolicy}, // watch:false
		{usecase.ThreadRequest{ThreadID: "chat:dev/chat", Limit: -3}, exitUsage},
	} {
		_, err := e.Svc.ThreadGet(ctx, c.r)
		wantExit(t, err, c.want)
	}
	if e.Graph.CallCount("ListReplies")+e.Graph.CallCount("ListChatMessages") != 0 {
		t.Fatal("read despite denial")
	}
}

func TestThreadGetGraphErrors(t *testing.T) {
	e := newEnv(t)
	e.Graph.Fail = map[string]error{"ListReplies": domain.NewNotFound("gone", "")}
	_, err := e.Svc.ThreadGet(ctx, usecase.ThreadRequest{ThreadID: "channel:alerts/r"})
	wantExit(t, err, exitNotFound)
	e.Graph.Fail = map[string]error{"ListChatMessages": domain.NewNotFound("gone", "")}
	_, err = e.Svc.ThreadGet(ctx, usecase.ThreadRequest{ThreadID: "chat:dev/chat"})
	wantExit(t, err, exitNotFound)
	// UPN guard applies.
	e2 := newEnv(t)
	e2.Graph.Profile.UPN = "x@y.z"
	_, err = e2.Svc.ThreadGet(ctx, usecase.ThreadRequest{ThreadID: "chat:dev/chat"})
	wantExit(t, err, exitPolicy)
}
