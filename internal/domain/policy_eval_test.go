package domain

import (
	"testing"

	"github.com/stainedhead/agent-cli-core/output"
)

func testPolicy() Policy {
	mk := func(a Alias, k Kind, send, watch bool) Destination {
		return Destination{Alias: a, Kind: k, Send: send, Watch: watch, DisplayName: "N", AADID: "11111111-1111-1111-1111-111111111111"}
	}
	return Policy{
		UPN: "Agent@Corp.Example.com",
		Destinations: map[Alias]Destination{
			"channel:b": mk("channel:b", KindChannel, true, true),
			"chat:a":    mk("chat:a", KindChat, false, true),
			"user:z":    mk("user:z", KindUser, true, false),
			"channel:a": mk("channel:a", KindChannel, false, false),
		},
		Inbound: Inbound{Handle: []InboundHandle{HandleDirect, HandleMentions, HandleWatched}},
	}
}

func TestPolicyDestination(t *testing.T) {
	p := testPolicy()
	if d, ok := p.Destination("chat:a"); !ok || d.Alias != "chat:a" {
		t.Fatal("listed")
	}
	if _, ok := p.Destination("chat:nope"); ok {
		t.Fatal("unlisted found")
	}
}

func TestCanSendWatch(t *testing.T) {
	p := testPolicy()
	cases := []struct {
		name  string
		d     Decision
		allow bool
		rule  string
	}{
		{"send ok", p.CanSend("channel:b"), true, ""},
		{"send false", p.CanSend("chat:a"), false, RuleSendDenied},
		{"send unlisted", p.CanSend("chat:x"), false, RuleDestinationUnlisted},
		{"watch ok", p.CanWatch("chat:a"), true, ""},
		{"watch false", p.CanWatch("user:z"), false, RuleWatchDenied},
		{"watch unlisted", p.CanWatch("chat:x"), false, RuleDestinationUnlisted},
	}
	for _, c := range cases {
		if c.d.Allowed != c.allow || c.d.RuleID != c.rule {
			t.Errorf("%s: %+v", c.name, c.d)
		}
		if c.allow {
			if c.d.Err() != nil {
				t.Errorf("%s: Err non-nil", c.name)
			}
			continue
		}
		if c.d.Category != output.CategoryPolicyDenied || output.ExitOf(c.d.Err()) != 6 {
			t.Errorf("%s: category/exit", c.name)
		}
	}
}

func TestEvalUPN(t *testing.T) {
	p := testPolicy()
	cases := []struct {
		upn   string
		allow bool
	}{
		{"agent@corp.example.com", true},
		{"AGENT@CORP.EXAMPLE.COM", true},
		{" agent@corp.example.com ", true},
		{"other@corp.example.com", false},
		{"", false},
	}
	for _, c := range cases {
		d := p.EvalUPN(Profile{UPN: c.upn})
		if d.Allowed != c.allow {
			t.Errorf("%q: %+v", c.upn, d)
		}
		if !c.allow && (d.RuleID != RuleIdentityUPN || d.Category != output.CategoryPolicyDenied) {
			t.Errorf("%q: %+v", c.upn, d)
		}
	}
	if (Policy{}).EvalUPN(Profile{UPN: "x"}).Allowed {
		t.Error("empty policy upn must deny")
	}
}

func TestWatchedSorted(t *testing.T) {
	got := testPolicy().Watched()
	if len(got) != 2 || got[0].Alias != "channel:b" || got[1].Alias != "chat:a" {
		t.Fatalf("got %+v", got)
	}
	if (Policy{}).Watched() != nil {
		t.Fatal("empty")
	}
}
