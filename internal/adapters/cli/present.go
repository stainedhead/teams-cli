package cli

import (
	"strings"
	"time"

	"github.com/stainedhead/agent-cli-core/output"

	"github.com/stainedhead/teams-cli/internal/domain"
	"github.com/stainedhead/teams-cli/internal/usecase"
)

// The presenter marks everything written by other people as untrusted (D13):
// message text and the sender's display name. Ids, aliases, booleans, times
// and the link list (plain strings, never fetched) stay plain.

type obj = map[string]any

func ts(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func untrusted(value, author string, at time.Time) output.Untrusted {
	if !at.IsZero() {
		at = at.UTC()
	}
	// Core neutralizes "<<<" in the value but not in the author attribute of the
	// text/table delimiter, and the author is a sender display name (attacker
	// controlled), so neutralize it here the same way.
	author = strings.ReplaceAll(author, "<<<", "<< <")
	return output.Untrusted{Value: value, Author: author, Timestamp: at}
}

func presentItem(it domain.InboundItem) obj {
	links := it.Links
	if links == nil {
		links = []string{}
	}
	return obj{
		"id":        it.ID,
		"thread_id": it.ThreadID,
		"received":  ts(it.Received),
		"cursor":    it.Cursor,
		"edited":    it.Edited,
		"conversation": obj{
			"type":  string(it.Conversation.Type),
			"alias": string(it.Conversation.Alias),
		},
		"sender": obj{
			"name":         untrusted(it.Sender.Name, it.Sender.Name, it.Received),
			"aad_id":       it.Sender.AADID,
			"can_instruct": it.Sender.CanInstruct,
			"is_agent":     it.Sender.IsAgent,
		},
		"mentioned_you": it.MentionedYou,
		"text":          untrusted(it.Text, it.Sender.Name, it.Received),
		"links":         links,
	}
}

// presentItems renders items as a JSON ARRAY (never null) so core's output
// bounding can cut it by whole items.
func presentItems(items []domain.InboundItem) []any {
	out := make([]any, 0, len(items))
	for _, it := range items {
		out = append(out, presentItem(it))
	}
	return out
}

func presentDestinations(d usecase.DestinationsResult) []any {
	out := make([]any, 0, len(d.Destinations))
	for _, v := range d.Destinations {
		o := obj{"alias": string(v.Alias), "kind": string(v.Kind), "send": v.Send, "watch": v.Watch}
		if v.DisplayName != "" {
			o["display_name"] = v.DisplayName
		}
		out = append(out, o)
	}
	return out
}

func presentSend(r usecase.SendResult) obj {
	findings := make([]obj, 0, len(r.Findings))
	for _, f := range r.Findings {
		findings = append(findings, obj{"filter": f.Filter, "pattern_id": f.PatternID})
	}
	return obj{
		"message_id":   r.MessageID,
		"thread_id":    r.ThreadID,
		"deduplicated": r.Deduplicated,
		"dry_run":      r.DryRun,
		"findings":     findings,
	}
}

func presentWhoami(w usecase.WhoamiResult, b BuildInfo) obj {
	return obj{
		"id":           w.Profile.ID,
		"display_name": w.Profile.DisplayName,
		"upn":          w.Profile.UPN,
		"policy":       w.Policy,
		"version":      b.Version,
	}
}
