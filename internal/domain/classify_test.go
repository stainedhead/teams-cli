package domain

import (
	"testing"
	"time"
	"unicode/utf8"
)

const (
	idCmd   = "aaaaaaaa-0000-0000-0000-000000000001"
	idAgent = "bbbbbbbb-0000-0000-0000-000000000002"
	idBoth  = "cccccccc-0000-0000-0000-000000000003"
	idMe    = "dddddddd-0000-0000-0000-000000000004"
	idOther = "eeeeeeee-0000-0000-0000-000000000005"
)

func classifyPolicy(tenant string) Policy {
	p := testPolicy()
	p.TenantID = tenant
	p.Instruct = Instruct{
		Commanders: []AADID{idCmd, AADID("CCCCCCCC-0000-0000-0000-000000000003")},
		Agents:     []AADID{idAgent, idBoth},
	}
	return p
}

func TestClassify(t *testing.T) {
	const tenant = "t-1"
	cases := []struct {
		name         string
		tenant       string
		s            RawSender
		instr, agent bool
		aad          string
	}{
		{"commander", "", RawSender{UserID: idCmd, Kind: SenderUser, Name: "C"}, true, false, idCmd},
		{"commander uppercase id", "", RawSender{UserID: "AAAAAAAA-0000-0000-0000-000000000001", Kind: SenderUser}, true, false, idCmd},
		{"agent only", "", RawSender{UserID: idAgent, Kind: SenderUser}, false, true, idAgent},
		{"in agents and commanders", "", RawSender{UserID: idBoth, Kind: SenderUser}, true, true, idBoth},
		{"unknown user", "", RawSender{UserID: idOther, Kind: SenderUser}, false, false, idOther},
		{"empty id", "", RawSender{UserID: "", Kind: SenderUser, Name: "x"}, false, false, ""},
		{"application kind", "", RawSender{UserID: idCmd, Kind: SenderApplication}, false, false, ""},
		{"bot kind", "", RawSender{UserID: idCmd, Kind: SenderBot}, false, false, ""},
		{"unknown kind", "", RawSender{UserID: idCmd, Kind: SenderUnknown}, false, false, ""},
		{"tenant match", tenant, RawSender{UserID: idCmd, TenantID: "T-1", Kind: SenderUser}, true, false, idCmd},
		{"tenant mismatch", tenant, RawSender{UserID: idCmd, TenantID: "t-2", Kind: SenderUser}, false, false, idCmd},
		{"tenant missing on sender", tenant, RawSender{UserID: idCmd, Kind: SenderUser}, false, false, idCmd},
		{"display name spoof", "", RawSender{UserID: idOther, Kind: SenderUser, Name: "Commander Jane"}, false, false, idOther},
	}
	for _, c := range cases {
		got := Classify(classifyPolicy(c.tenant), c.s)
		if got.CanInstruct != c.instr || got.IsAgent != c.agent || got.AADID != c.aad || got.Name != c.s.Name {
			t.Errorf("%s: %+v", c.name, got)
		}
	}
}

func TestClassifyAgentNeverInstructsByAgentListing(t *testing.T) {
	got := Classify(classifyPolicy(""), RawSender{UserID: idAgent, Kind: SenderUser})
	if got.CanInstruct {
		t.Fatal("agent gained instruct")
	}
}

func FuzzClassifyNamesIgnored(f *testing.F) {
	f.Add("Commander Jane")
	f.Add("")
	f.Add(idCmd)
	p := classifyPolicy("")
	f.Fuzz(func(t *testing.T, name string) {
		if !utf8.ValidString(name) {
			t.Skip()
		}
		got := Classify(p, RawSender{UserID: idOther, Kind: SenderUser, Name: name})
		if got.CanInstruct || got.IsAgent {
			t.Fatalf("name %q conferred privilege", name)
		}
		if Classify(p, RawSender{UserID: idCmd, Kind: SenderUser, Name: name}).CanInstruct != true {
			t.Fatal("commander lost privilege by name")
		}
	})
}

func TestSelectHandleMatrix(t *testing.T) {
	all := []InboundHandle{HandleDirect, HandleMentions, HandleWatched}
	mention := []RawMention{{UserID: idOther}, {UserID: "DDDDDDDD-0000-0000-0000-000000000004"}}
	cases := []struct {
		kind      Kind
		handles   []InboundHandle
		mentioned bool
		want      InboundHandle
		surfaced  bool
	}{
		{KindUser, all, false, HandleDirect, true},
		{KindUser, all, true, HandleDirect, true},
		{KindUser, []InboundHandle{HandleMentions, HandleWatched}, false, HandleDirect, false},
		{KindChat, all, true, HandleMentions, true},
		{KindChat, all, false, HandleWatched, true},
		{KindChat, []InboundHandle{HandleDirect, HandleWatched}, true, HandleMentions, false},
		{KindChat, []InboundHandle{HandleDirect, HandleMentions}, false, HandleWatched, false},
		{KindChannel, all, true, HandleMentions, true},
		{KindChannel, all, false, HandleWatched, true},
		{KindChannel, []InboundHandle{HandleMentions}, false, HandleWatched, false},
		{KindChannel, []InboundHandle{HandleMentions}, true, HandleMentions, true},
		{KindChannel, nil, true, HandleMentions, false},
	}
	for i, c := range cases {
		p := testPolicy()
		p.Inbound.Handle = c.handles
		m := RawMessage{}
		if c.mentioned {
			m.Mentions = mention
		} else {
			m.Mentions = mention[:1]
		}
		h, ok := SelectHandle(p, Destination{Kind: c.kind}, m, idMe)
		if h != c.want || ok != c.surfaced {
			t.Errorf("case %d: got %q %v want %q %v", i, h, ok, c.want, c.surfaced)
		}
	}
	// empty agent id never counts as mentioned
	h, _ := SelectHandle(testPolicy(), Destination{Kind: KindChat}, RawMessage{Mentions: []RawMention{{UserID: ""}}}, "")
	if h != HandleWatched {
		t.Fatal("empty agent id matched a mention")
	}
}

func baseRaw() RawMessage {
	return RawMessage{
		ID: "1696341900000", MessageType: "message",
		Created: time.Date(2026, 10, 3, 14, 5, 0, 0, time.UTC), Modified: time.Date(2026, 10, 3, 14, 6, 0, 0, time.UTC),
		FromUserID: idCmd, FromKind: SenderUser, FromName: "Jane",
		BodyType: "text", BodyContent: "hello https://example.com/x.",
	}
}

func TestNormalizeKept(t *testing.T) {
	p := classifyPolicy("")
	dCh := Destination{Alias: "channel:ops", Kind: KindChannel}
	m := baseRaw()
	m.ThreadID = "root-1"
	m.Mentions = []RawMention{{UserID: idMe}}
	it, out := Normalize(p, dCh, m, idMe)
	if out.Kind != NormalizeOK {
		t.Fatalf("%+v", out)
	}
	if it.ID != "channel:ops/1696341900000" || it.ThreadID != "channel:ops/root-1" || !it.MentionedYou || !it.Sender.CanInstruct {
		t.Fatalf("%+v", it)
	}
	if it.Cursor != "c1:2026-10-03T14:06:00Z" || !it.Received.Equal(m.Created) || it.Edited {
		t.Fatalf("%+v", it)
	}
	if len(it.Links) != 1 || it.Links[0] != "https://example.com/x" || it.Conversation.Alias != "channel:ops" || it.Conversation.Type != KindChannel {
		t.Fatalf("%+v", it)
	}

	// channel top-level message: thread root is its own id
	m2 := baseRaw()
	it2, _ := Normalize(p, dCh, m2, idMe)
	if it2.ThreadID != "channel:ops/1696341900000" {
		t.Fatal(it2.ThreadID)
	}

	// chat: synthetic thread; html body converted
	m3 := baseRaw()
	m3.BodyType, m3.BodyContent = "html", `<p>Hi <at id="0">Bot</at></p><a href="https://a.example/p">l</a>`
	it3, out3 := Normalize(p, Destination{Alias: "chat:team", Kind: KindChat}, m3, idMe)
	if out3.Kind != NormalizeOK || it3.ThreadID != "chat:team/chat" || it3.Text != "Hi @Bot\nl" || len(it3.Links) != 1 {
		t.Fatalf("%+v %+v", it3, out3)
	}

	// no links => empty non-nil slice; empty from kind with user id counts as user
	m4 := baseRaw()
	m4.BodyContent, m4.FromKind = "plain", ""
	it4, _ := Normalize(p, dCh, m4, idMe)
	if it4.Links == nil || !it4.Sender.CanInstruct {
		t.Fatalf("%+v", it4)
	}
}

func TestNormalizeDrops(t *testing.T) {
	p := classifyPolicy("")
	d := Destination{Alias: "chat:team", Kind: KindChat}
	mut := func(f func(*RawMessage)) RawMessage { m := baseRaw(); f(&m); return m }
	cases := []struct {
		name   string
		m      RawMessage
		kind   NormalizeKind
		reason string
	}{
		{"chatEvent", mut(func(m *RawMessage) { m.MessageType = "chatEvent" }), NormalizeSystem, ReasonSystem},
		{"systemEvent", mut(func(m *RawMessage) { m.MessageType = "systemEventMessage" }), NormalizeSystem, ReasonSystem},
		{"deleted", mut(func(m *RawMessage) { m.Deleted = true }), NormalizeDeleted, ReasonDeleted},
		{"own", mut(func(m *RawMessage) { m.FromUserID = "DDDDDDDD-0000-0000-0000-000000000004" }), NormalizeOwn, ReasonOwn},
		{"null type", mut(func(m *RawMessage) { m.MessageType = "" }), NormalizeIncomplete, ReasonSystem},
		{"null id", mut(func(m *RawMessage) { m.ID = "" }), NormalizeIncomplete, ReasonMissingID},
		{"null from", mut(func(m *RawMessage) { m.FromUserID, m.FromName, m.FromKind = "", "", "" }), NormalizeIncomplete, ReasonMissingFrom},
		{"null body", mut(func(m *RawMessage) { m.BodyContent = "" }), NormalizeIncomplete, ReasonMissingBody},
		{"null modified", mut(func(m *RawMessage) { m.Modified = time.Time{} }), NormalizeIncomplete, ReasonMissingModified},
	}
	for _, c := range cases {
		it, out := Normalize(p, d, c.m, idMe)
		if out.Kind != c.kind || out.Reason != c.reason || it.ID != "" {
			t.Errorf("%s: %+v %+v", c.name, out, it)
		}
	}
	// bot sender without user id is kept, non-instructing
	m := baseRaw()
	m.FromUserID, m.FromKind, m.FromName = "", SenderApplication, "Connector"
	it, out := Normalize(p, d, m, idMe)
	if out.Kind != NormalizeOK || it.Sender.CanInstruct || it.Sender.AADID != "" {
		t.Fatalf("%+v %+v", it, out)
	}
}
