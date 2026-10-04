package domain

import (
	"fmt"
	"html"
	"strings"
)

const defaultMaxMentions = 5

// BuildMentions renders an outgoing message from policy alone (D5): each alias
// must be a user: destination in send.mentions.allow with an AAD id and
// display name. Duplicates collapse. The policy prefix is prepended. With no
// mentions the message is plain text; otherwise it is HTML with the text
// escaped (so injected markup, including <at>, is inert) and the mention
// markup placed first. No network lookup is involved.
func BuildMentions(p Policy, aliases []Alias, text string) (OutMessage, error) {
	text = p.Send.Prefix + text
	if len(aliases) == 0 {
		return OutMessage{Text: text}, nil
	}
	max := p.Send.Mentions.Max
	if max <= 0 {
		max = defaultMaxMentions
	}
	allowed := map[Alias]bool{}
	for _, a := range p.Send.Mentions.Allow {
		allowed[a] = true
	}
	seen := map[Alias]bool{}
	var mentions []OutMention
	for _, a := range aliases {
		if seen[a] {
			continue
		}
		seen[a] = true
		if _, err := ParseAlias(string(a)); err != nil {
			return OutMessage{}, err
		}
		if a.Kind() != KindUser {
			return OutMessage{}, NewPolicyDenied("only user: aliases can be mentioned", "channel, team and tag mentions are not allowed")
		}
		d, listed := p.Destination(a)
		if !allowed[a] || !listed {
			return OutMessage{}, NewPolicyDenied("mention is not allowed by policy", "add the alias to send.mentions.allow")
		}
		if d.AADID == "" || strings.TrimSpace(d.DisplayName) == "" {
			return OutMessage{}, NewPolicyDenied("mention target lacks aad_id or display_name in policy", "set display_name on the destination")
		}
		mentions = append(mentions, OutMention{ID: len(mentions), AADID: strings.ToLower(d.AADID), DisplayName: d.DisplayName})
	}
	if len(mentions) > max {
		return OutMessage{}, NewPolicyDenied(fmt.Sprintf("more than %d mentions", max), "reduce the number of --mention flags")
	}
	var b strings.Builder
	for _, m := range mentions {
		fmt.Fprintf(&b, `<at id="%d">%s</at> `, m.ID, html.EscapeString(m.DisplayName))
	}
	body := html.EscapeString(text)
	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\n", "<br>")
	b.WriteString(body)
	return OutMessage{Text: b.String(), HTML: true, Mentions: mentions}, nil
}
