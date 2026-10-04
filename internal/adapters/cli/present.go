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
		out = append(out, presentDestination(v))
	}
	return out
}

func presentDestination(v usecase.DestinationView) obj {
	o := obj{"alias": string(v.Alias), "kind": string(v.Kind), "send": v.Send, "watch": v.Watch, "mentionable": v.Mentionable}
	if v.DisplayName != "" {
		o["display_name"] = v.DisplayName
	}
	return o
}

func presentSend(r usecase.SendResult) obj {
	findings := make([]obj, 0, len(r.Findings))
	for _, f := range r.Findings {
		findings = append(findings, obj{"filter": f.Filter, "pattern_id": f.PatternID})
	}
	o := obj{
		"message_id":   r.MessageID,
		"thread_id":    r.ThreadID,
		"deduplicated": r.Deduplicated,
		"dry_run":      r.DryRun,
		"findings":     findings,
	}
	if r.DryRun {
		o["decision"] = r.Decision
		o["destination"] = obj{"alias": string(r.Destination.Alias), "kind": string(r.Destination.Kind)}
		// The preview is the text this process would post; it contains the
		// caller's own text, so it is marked untrusted like any free text.
		o["preview"] = untrusted(r.Preview, "", time.Time{})
		o["preview_html"] = r.PreviewHTML
	}
	return o
}

func presentWhoami(w usecase.WhoamiResult, b BuildInfo) obj {
	dests := make([]any, 0, len(w.Destinations))
	for _, v := range w.Destinations {
		dests = append(dests, presentDestination(v))
	}
	l := w.Limits
	return obj{
		"id":             w.Profile.ID,
		"display_name":   w.Profile.DisplayName,
		"upn":            w.Profile.UPN,
		"policy":         w.Policy,
		"version":        b.Version,
		"policy_path":    w.PolicyPath,
		"policy_version": w.PolicyVersion,
		"destinations":   dests,
		"limits": obj{
			"max_results": l.MaxResults, "max_writes_per_run": l.MaxWritesPerRun, "max_bytes": l.MaxBytes,
			"rate_per_minute": l.RatePerMinute, "rate_per_hour": l.RatePerHour, "reply_depth_max": l.ReplyDepthMax,
		},
		"poll_interval_seconds": int(w.PollInterval / time.Second),
	}
}
