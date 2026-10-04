package selftestcfg

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/agent-cli-core/selftest"

	"github.com/stainedhead/teams-cli/internal/domain"
	"github.com/stainedhead/teams-cli/internal/usecase"
)

// Row names (spec s8).
const (
	RowIdentity       = "identity"
	RowSendAllowed    = "send-allowed"
	RowSendUnlisted   = "send-unlisted"
	RowBroadcast      = "broadcast-mention"
	RowOversize       = "oversize"
	RowSecret         = "secret-pattern"
	RowClassification = "classification"
	RowMentionUnlist  = "mention-unlisted"
	RowUserChat       = "user-chat-resolve"
	RowNegative       = "negative-membership"
	RowInboxRead      = "inbox-read"
)

const (
	unlistedAlias     = domain.Alias("chat:__selftest_unlisted__")
	unlistedMention   = domain.Alias("user:__selftest_unlisted__")
	broadcastMention  = domain.Alias("channel:__selftest_all__")
	fakeAWSKey        = "AKIAIOSFODNN7EXAMPLE" // the canonical documentation key, not a credential
	selftestKeyPrefix = "selftest-"
)

// Deps is what the probe drives.
type Deps struct {
	Cmds   usecase.Commands
	Graph  usecase.Graph // for user-chat-resolve and negative-membership
	Policy domain.Policy
	RunID  string
}

// Runner returns the core selftest runner for d. readOnly skips the
// send-allowed row.
func Runner(d Deps, readOnly bool) selftest.Runner {
	return selftest.Runner{Rows: Rows(d.Policy), Probe: Probe(d), ReadOnly: readOnly}
}

// sorted returns the policy destinations ordered by alias.
func sorted(p domain.Policy) []domain.Destination {
	out := make([]domain.Destination, 0, len(p.Destinations))
	for _, d := range p.Destinations {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Alias < out[j].Alias })
	return out
}

func firstSend(p domain.Policy) (domain.Destination, bool) {
	for _, d := range sorted(p) {
		if d.Send {
			return d, true
		}
	}
	return domain.Destination{}, false
}

func firstWatch(p domain.Policy) (domain.Destination, bool) {
	if w := p.Watched(); len(w) > 0 {
		return w[0], true
	}
	return domain.Destination{}, false
}

func firstUser(p domain.Policy) (domain.Destination, bool) {
	for _, d := range sorted(p) {
		if d.Kind == domain.KindUser {
			return d, true
		}
	}
	return domain.Destination{}, false
}

func hasFilter(p domain.Policy, name string) bool {
	for _, f := range p.Send.ContentFilters {
		if f == name {
			return true
		}
	}
	return false
}

// Rows derives the matrix from the policy.
func Rows(p domain.Policy) []selftest.Row {
	var rows []selftest.Row
	add := func(name, verb, resource string, expect selftest.Outcome, readOnly bool) {
		rows = append(rows, selftest.Row{Name: name, Verb: verb, Resource: resource, Expect: expect, ReadOnly: readOnly})
	}
	add(RowIdentity, "read", "me", selftest.Allow, true)
	send, canSend := firstSend(p)
	if canSend {
		add(RowSendAllowed, "send", string(send.Alias), selftest.Allow, false)
	}
	add(RowSendUnlisted, "send", string(unlistedAlias), selftest.Deny, true)
	if canSend {
		add(RowBroadcast, "send", string(send.Alias), selftest.Deny, true)
		if p.Send.MaxBytes > 0 {
			add(RowOversize, "send", string(send.Alias), selftest.Deny, true)
		}
		if hasFilter(p, domain.FilterSecretPatterns) {
			add(RowSecret, "send", string(send.Alias), selftest.Deny, true)
		}
		if hasFilter(p, domain.FilterClassificationMarkers) && len(p.Send.ClassificationMarkers) > 0 {
			add(RowClassification, "send", string(send.Alias), selftest.Deny, true)
		}
		add(RowMentionUnlist, "send", string(send.Alias), selftest.Deny, true)
	}
	if u, ok := firstUser(p); ok {
		add(RowUserChat, "read", string(u.Alias), selftest.Allow, true)
	}
	if p.Selftest.NonMemberChatID != "" {
		add(RowNegative, "read", "configured non-member chat", selftest.Deny, true)
	}
	if w, ok := firstWatch(p); ok {
		add(RowInboxRead, "read", string(w.Alias), selftest.Allow, true)
	}
	return rows
}

// outcome maps an error to Allow, Deny (for the given refusal categories) or a
// probe error.
func outcome(err error, deny ...output.Category) (selftest.Outcome, error) {
	if err == nil {
		return selftest.Allow, nil
	}
	cat := output.CategoryOf(err)
	for _, c := range deny {
		if cat == c {
			return selftest.Deny, nil
		}
	}
	return "", err
}

var policyRefusal = []output.Category{output.CategoryPolicyDenied, output.CategoryValidation}

// Probe returns the probe for d.
func Probe(d Deps) selftest.Probe {
	return func(ctx context.Context, row selftest.Row) (selftest.Outcome, error) {
		send, _ := firstSend(d.Policy)
		dry := func(req usecase.SendRequest) (selftest.Outcome, error) {
			req.Alias, req.DryRun = send.Alias, true
			_, err := d.Cmds.Send(ctx, req)
			return outcome(err, policyRefusal...)
		}
		switch row.Name {
		case RowIdentity:
			_, err := d.Cmds.Whoami(ctx)
			return outcome(err, output.CategoryPolicyDenied)
		case RowSendAllowed:
			_, err := d.Cmds.Send(ctx, usecase.SendRequest{
				Alias: send.Alias, Text: "selftest " + d.RunID, IdempotencyKey: selftestKeyPrefix + d.RunID,
			})
			return outcome(err, policyRefusal...)
		case RowSendUnlisted:
			_, err := d.Cmds.Send(ctx, usecase.SendRequest{Alias: unlistedAlias, Text: "selftest", DryRun: true})
			return outcome(err, policyRefusal...)
		case RowBroadcast:
			return dry(usecase.SendRequest{Text: "selftest", Mentions: []domain.Alias{broadcastMention}})
		case RowOversize:
			return dry(usecase.SendRequest{Text: strings.Repeat("a", d.Policy.Send.MaxBytes+1)})
		case RowSecret:
			return dry(usecase.SendRequest{Text: "selftest " + fakeAWSKey})
		case RowClassification:
			return dry(usecase.SendRequest{Text: "selftest " + d.Policy.Send.ClassificationMarkers[0]})
		case RowMentionUnlist:
			return dry(usecase.SendRequest{Text: "selftest", Mentions: []domain.Alias{unlistedMention}})
		case RowUserChat:
			u, _ := firstUser(d.Policy)
			_, err := d.Graph.ResolveUserChat(ctx, u.AADID, false)
			return outcome(err, output.CategoryPolicyDenied)
		case RowNegative:
			// 403 and 404 both mean "not a member" (D9).
			return outcome(d.Graph.GetChat(ctx, d.Policy.Selftest.NonMemberChatID), output.CategoryForbidden, output.CategoryNotFound)
		case RowInboxRead:
			w, _ := firstWatch(d.Policy)
			_, err := d.Cmds.Inbox(ctx, usecase.InboxRequest{Alias: w.Alias, Limit: 1})
			return outcome(err, output.CategoryPolicyDenied)
		}
		return "", fmt.Errorf("selftest: no probe for row %q", row.Name)
	}
}
