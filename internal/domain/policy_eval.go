package domain

import (
	"sort"
	"strings"

	"github.com/stainedhead/agent-cli-core/output"
)

// Stable rule ids recorded in audit and Decision.RuleID.
const (
	RuleDestinationUnlisted = "destination.unlisted"
	RuleSendDenied          = "destination.send_denied"
	RuleWatchDenied         = "destination.watch_denied"
	RuleIdentityUPN         = "identity.upn_mismatch"
)

func deny(rule, reason string) Decision {
	return Decision{Allowed: false, Category: output.CategoryPolicyDenied, RuleID: rule, Reason: reason}
}

// Destination looks up a destination by alias.
func (p Policy) Destination(a Alias) (Destination, bool) {
	d, ok := p.Destinations[a]
	return d, ok
}

// CanSend denies unlisted destinations and destinations with send:false.
func (p Policy) CanSend(a Alias) Decision {
	d, ok := p.Destination(a)
	if !ok {
		return deny(RuleDestinationUnlisted, "destination is not listed in policy")
	}
	if !d.Send {
		return deny(RuleSendDenied, "policy does not allow sending to this destination")
	}
	return Decision{Allowed: true, Category: output.CategoryOK}
}

// CanWatch denies unlisted destinations and destinations with watch:false.
func (p Policy) CanWatch(a Alias) Decision {
	d, ok := p.Destination(a)
	if !ok {
		return deny(RuleDestinationUnlisted, "destination is not listed in policy")
	}
	if !d.Watch {
		return deny(RuleWatchDenied, "policy does not allow reading this destination")
	}
	return Decision{Allowed: true, Category: output.CategoryOK}
}

// EvalUPN compares the authenticated profile's UPN with policy, ignoring case
// (D14). An empty UPN on either side is a mismatch.
func (p Policy) EvalUPN(me Profile) Decision {
	got, want := strings.TrimSpace(me.UPN), strings.TrimSpace(p.UPN)
	if got == "" || want == "" || !strings.EqualFold(got, want) {
		return deny(RuleIdentityUPN, "authenticated identity does not match the policy upn")
	}
	return Decision{Allowed: true, Category: output.CategoryOK}
}

// Watched returns the watch:true destinations sorted by alias.
func (p Policy) Watched() []Destination {
	var out []Destination
	for _, d := range p.Destinations {
		if d.Watch {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Alias < out[j].Alias })
	return out
}
