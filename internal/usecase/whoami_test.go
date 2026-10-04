package usecase_test

import (
	"testing"
	"time"

	"github.com/stainedhead/teams-cli/internal/domain"
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

// FR-R3 (FR-1): whoami carries destinations, limits, poll interval, policy path and version.
func TestWhoamiDetails(t *testing.T) {
	e := newEnv(t)
	e.Run.PolicyPath = "/etc/agent-cli/teams.policy.yaml"
	svc := e.NewService()
	res, err := svc.Whoami(ctx)
	wantOK(t, err)
	if res.PolicyPath != "/etc/agent-cli/teams.policy.yaml" || res.PolicyVersion != 1 {
		t.Fatalf("policy path/version = %q/%d", res.PolicyPath, res.PolicyVersion)
	}
	if len(res.Destinations) != 6 || res.Destinations[0].Alias != "channel:alerts" {
		t.Fatalf("destinations = %+v", res.Destinations)
	}
	l := res.Limits
	if l.MaxResults != 50 || l.MaxWritesPerRun != 30 || l.MaxBytes != 200 || l.RatePerMinute != 10 || l.RatePerHour != 100 || l.ReplyDepthMax != 3 {
		t.Fatalf("limits = %+v", l)
	}
	if res.PollInterval != 15*time.Second {
		t.Fatalf("poll = %v", res.PollInterval)
	}
}

// FR-R3 (FR-2): mentionable is true only for allow-listed aliases with the id fields a mention needs.
func TestDestinationsMentionable(t *testing.T) {
	e := envWith(t, func(p *domain.Policy) {
		p.Send.Mentions.Allow = []domain.Alias{"user:jane", "user:bob", "chat:dev"}
		b := p.Destinations["user:bob"]
		b.DisplayName = "" // lacks a display name: not mentionable
		p.Destinations["user:bob"] = b
	})
	res, err := e.Svc.Destinations(ctx)
	wantOK(t, err)
	got := map[domain.Alias]bool{}
	for _, d := range res.Destinations {
		got[d.Alias] = d.Mentionable
	}
	if !got["user:jane"] || got["user:bob"] || got["chat:dev"] || got["channel:alerts"] {
		t.Fatalf("mentionable = %v", got)
	}
}
