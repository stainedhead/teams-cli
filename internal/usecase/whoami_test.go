package usecase_test

import (
	"testing"

	"github.com/stainedhead/teams-cli/internal/usecase/usecasetest"
)

// B2 (AC-1): whoami returns identity and policy profile with one GET /me.
func TestWhoami(t *testing.T) {
	e := newEnv(t)
	res, err := e.Svc.Whoami(ctx)
	wantOK(t, err)
	if res.Profile.UPN != usecasetest.AgentUPN || res.Profile.ID != usecasetest.AgentID || res.Policy != "agent" {
		t.Fatalf("res = %+v", res)
	}
	if e.Graph.CallCount("Me") != 1 {
		t.Fatalf("calls = %v", e.Graph.Calls)
	}
	if ev := lastEvent(t, e); ev.Verb != "whoami" || ev.Outcome != "ok" || ev.Decision != "allow" {
		t.Fatalf("audit = %+v", ev)
	}
}

// B2 (AC-2): destinations are local-only, sorted, and never expose raw ids.
func TestDestinationsNoNetworkNoIDs(t *testing.T) {
	e := newEnv(t)
	res, err := e.Svc.Destinations(ctx)
	wantOK(t, err)
	if len(e.Graph.Calls) != 0 {
		t.Fatalf("network used: %v", e.Graph.Calls)
	}
	if len(res.Destinations) != 6 {
		t.Fatalf("destinations = %+v", res.Destinations)
	}
	for i := 1; i < len(res.Destinations); i++ {
		if res.Destinations[i-1].Alias >= res.Destinations[i].Alias {
			t.Fatalf("not sorted: %+v", res.Destinations)
		}
	}
	first := res.Destinations[0]
	if first.Alias != "channel:alerts" || first.Kind != "channel" || !first.Send || !first.Watch {
		t.Fatalf("first = %+v", first)
	}
	// The view type has no id fields; check the rendered values too.
	dump := ""
	for _, d := range res.Destinations {
		dump += string(d.Alias) + d.DisplayName
	}
	mustNotContain(t, "destinations", dump, usecasetest.ChannelID, usecasetest.DevChatID, usecasetest.JaneID, usecasetest.TeamID)
}

func TestDestinationsPolicyError(t *testing.T) {
	e := newEnv(t)
	e.Provider.Err = errAmbiguous
	_, err := e.Svc.Destinations(ctx)
	wantExit(t, err, exitGeneral)
}
