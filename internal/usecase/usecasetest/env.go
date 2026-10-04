package usecasetest

import (
	"time"

	"github.com/stainedhead/agent-cli-core/output"

	"github.com/stainedhead/teams-cli/internal/domain"
	"github.com/stainedhead/teams-cli/internal/usecase"
)

// Well-known ids used by Policy and the tests.
const (
	AgentID     = "aaaaaaaa-0000-0000-0000-000000000001"
	AgentUPN    = "agent@corp.example.com"
	CommanderID = "cccccccc-0000-0000-0000-000000000001"
	OtherAgent  = "dddddddd-0000-0000-0000-000000000001"
	StrangerID  = "eeeeeeee-0000-0000-0000-000000000001"
	JaneID      = "11111111-0000-0000-0000-00000000aaaa"
	BobID       = "22222222-0000-0000-0000-00000000bbbb"
	TenantID    = "99999999-0000-0000-0000-000000000009"
	TeamID      = "team-guid-1"
	ChannelID   = "19:chan@thread.tacv2"
	DevChatID   = "19:devchat@thread.v2"
)

// T0 is the fake clock's start time.
var T0 = time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)

// Policy returns a complete, valid policy for tests:
//
//	channel:alerts   send+watch        chat:dev       send+watch
//	chat:readonly    watch only        chat:writeonly send only
//	user:jane        send+watch, create_chat false, mentionable
//	user:bob         send+watch, create_chat true
func Policy() domain.Policy {
	d := func(a domain.Alias, x domain.Destination) domain.Destination { x.Alias, x.Kind = a, a.Kind(); return x }
	dests := map[domain.Alias]domain.Destination{}
	for _, x := range []domain.Destination{
		d("channel:alerts", domain.Destination{TeamID: TeamID, ChannelID: ChannelID, Send: true, Watch: true}),
		d("chat:dev", domain.Destination{ChatID: DevChatID, Send: true, Watch: true}),
		d("chat:readonly", domain.Destination{ChatID: "19:ro@thread.v2", Watch: true}),
		d("chat:writeonly", domain.Destination{ChatID: "19:wo@thread.v2", Send: true}),
		d("user:jane", domain.Destination{AADID: JaneID, DisplayName: "Jane Doe", Send: true, Watch: true}),
		d("user:bob", domain.Destination{AADID: BobID, DisplayName: "Bob Roe", Send: true, Watch: true, CreateChat: true}),
	} {
		dests[x.Alias] = x
	}
	return domain.Policy{
		Version: 1, Profile: "agent", UPN: AgentUPN, StateDir: "/tmp/state",
		Destinations: dests,
		Instruct:     domain.Instruct{Commanders: []domain.AADID{CommanderID}, Agents: []domain.AADID{OtherAgent}},
		Inbound: domain.Inbound{
			Handle:      []domain.InboundHandle{domain.HandleDirect, domain.HandleMentions, domain.HandleWatched},
			MaxLookback: 30 * time.Minute, PollInterval: 15 * time.Second, MaxWait: 120 * time.Second, ThreadPollMax: 5,
		},
		Send: domain.SendPolicy{
			MaxBytes:              200,
			Mentions:              domain.MentionPolicy{Allow: []domain.Alias{"user:jane"}, BlockBroadcast: true, Max: 5},
			ContentFilters:        []string{domain.FilterSecretPatterns, domain.FilterClassificationMarkers},
			ClassificationMarkers: []string{"CONFIDENTIAL"},
			Rate:                  domain.Rate{PerMinute: 10, PerHour: 100},
			ReplyDepthMax:         3,
			ReplyWindow:           24 * time.Hour,
		},
		Limits: domain.Limits{MaxResults: 50, MaxWritesPerRun: 30, MaxChatScan: 50},
		Audit:  domain.AuditCfg{Path: "/tmp/audit.jsonl"},
	}
}

// Env wires every fake into a usecase service.
type Env struct {
	Graph    *Graph
	Ledger   *Ledger
	Cursors  *CursorStore
	Provider *PolicyProvider
	Audit    *AuditSink
	Clock    *Clock
	Rand     *Rand
	Run      usecase.RunInfo
	Svc      usecase.Commands
}

// NewEnv builds an Env around p whose Graph identity matches the policy UPN.
func NewEnv(p domain.Policy) *Env {
	e := &Env{
		Graph:    &Graph{Profile: domain.Profile{ID: AgentID, DisplayName: "Agent", UPN: p.UPN}},
		Ledger:   &Ledger{},
		Cursors:  &CursorStore{},
		Provider: &PolicyProvider{P: p},
		Audit:    &AuditSink{},
		Clock:    &Clock{T: T0},
		Rand:     &Rand{},
		Run:      usecase.RunInfo{AgentID: "agent-7", RunID: "run-1"},
	}
	e.Svc = e.NewService()
	return e
}

// NewService builds a fresh service (a new "process run") over the same fakes.
func (e *Env) NewService() usecase.Commands {
	return usecase.New(usecase.Deps{
		Policy: e.Provider, Graph: e.Graph, Ledger: e.Ledger, Cursors: e.Cursors,
		Audit: e.Audit, Clock: e.Clock, Rand: e.Rand, Run: e.Run,
	})
}

// Msg builds a plain-text RawMessage from a user, created and modified at t.
func Msg(id, fromID, text string, t time.Time) domain.RawMessage {
	return domain.RawMessage{
		ID: id, MessageType: "message", Created: t, Modified: t,
		FromUserID: fromID, FromTenantID: TenantID, FromName: "Sender " + id, FromKind: domain.SenderUser,
		BodyType: "text", BodyContent: text,
	}
}

// ErrCategory returns the category of err (output.CategoryOK for nil).
func ErrCategory(err error) output.Category { return output.CategoryOf(err) }
