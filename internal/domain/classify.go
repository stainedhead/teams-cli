package domain

import (
	"strings"
)

// Normalize drop reasons (counted by the use case, never content).
const (
	ReasonSystem          = "system_message"
	ReasonDeleted         = "deleted"
	ReasonOwn             = "own_message"
	ReasonMissingID       = "missing_id"
	ReasonMissingFrom     = "missing_from"
	ReasonMissingBody     = "missing_body"
	ReasonMissingModified = "missing_modified"
)

func normID(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func hasID(list []AADID, id string) bool {
	for _, c := range list {
		if normID(string(c)) == id {
			return true
		}
	}
	return false
}

// Classify classifies a sender (FR-19) from ids only; display names are never
// an input. Non-user senders and senders without an id never instruct. A
// sender instructs only if listed as a commander and, when policy tenant_id is
// set, from that tenant. Agents never gain instruct except by commander
// listing.
func Classify(p Policy, s RawSender) Sender {
	out := Sender{Name: s.Name}
	id := normID(s.UserID)
	if s.Kind != SenderUser || id == "" {
		return out
	}
	out.AADID = id
	out.IsAgent = hasID(p.Instruct.Agents, id)
	tenantOK := p.TenantID == "" || normID(s.TenantID) == normID(p.TenantID)
	out.CanInstruct = tenantOK && hasID(p.Instruct.Commanders, id)
	return out
}

func mentionsAgent(m RawMessage, agentID string) bool {
	id := normID(agentID)
	if id == "" {
		return false
	}
	for _, mm := range m.Mentions {
		if normID(mm.UserID) == id {
			return true
		}
	}
	return false
}

// SelectHandle applies the inbound handle rules (D4) within a destination:
// user: destinations are direct; others are mentions when the message
// mentions the agent, else watched. The message is surfaced only if that
// handle is enabled in policy.
func SelectHandle(p Policy, d Destination, m RawMessage, agentID string) (InboundHandle, bool) {
	h := HandleWatched
	switch {
	case d.Kind == KindUser:
		h = HandleDirect
	case mentionsAgent(m, agentID):
		h = HandleMentions
	}
	for _, e := range p.Inbound.Handle {
		if e == h {
			return h, true
		}
	}
	return h, false
}

// Normalize turns a raw message into an inbound item or a drop outcome.
// Drops, in order: non-message types, deleted, own messages, then messages
// with a null id, from, body or modified time.
func Normalize(p Policy, d Destination, m RawMessage, agentID string) (InboundItem, NormalizeOutcome) {
	drop := func(k NormalizeKind, reason string) (InboundItem, NormalizeOutcome) {
		return InboundItem{}, NormalizeOutcome{Kind: k, Reason: reason}
	}
	if m.MessageType != "message" && m.MessageType != "" {
		return drop(NormalizeSystem, ReasonSystem)
	}
	if m.Deleted {
		return drop(NormalizeDeleted, ReasonDeleted)
	}
	if id := normID(agentID); id != "" && normID(m.FromUserID) == id {
		return drop(NormalizeOwn, ReasonOwn)
	}
	switch {
	case m.MessageType == "":
		return drop(NormalizeIncomplete, ReasonSystem)
	case m.ID == "":
		return drop(NormalizeIncomplete, ReasonMissingID)
	case m.FromKind == "" && m.FromUserID == "" && m.FromName == "":
		return drop(NormalizeIncomplete, ReasonMissingFrom)
	case m.BodyContent == "":
		return drop(NormalizeIncomplete, ReasonMissingBody)
	case m.Modified.IsZero():
		return drop(NormalizeIncomplete, ReasonMissingModified)
	}
	var text string
	var links []string
	if strings.EqualFold(m.BodyType, "html") {
		text, links = HTMLToText(m.BodyContent)
	} else {
		text = m.BodyContent
		links = extractURLs(text)
	}
	if links == nil {
		links = []string{}
	}
	kind := m.FromKind
	if kind == "" {
		kind = SenderUser
	}
	root := "chat"
	if d.Kind == KindChannel {
		root = m.ThreadID
		if root == "" {
			root = m.ID
		}
	}
	return InboundItem{
		ID:           ItemID(d.Alias, m.ID),
		ThreadID:     ThreadID(d.Alias, root),
		Received:     m.Created,
		Cursor:       EncodeCursor(m.Modified),
		Conversation: Conversation{Type: d.Kind, Alias: d.Alias},
		Sender:       Classify(p, RawSender{UserID: m.FromUserID, TenantID: m.FromTenantID, Name: m.FromName, Kind: kind}),
		MentionedYou: mentionsAgent(m, agentID),
		Text:         text,
		Links:        links,
	}, NormalizeOutcome{Kind: NormalizeOK}
}
