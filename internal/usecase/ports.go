// Package usecase orchestrates teams-cli commands over ports. It depends on
// domain and interfaces only.
package usecase

import (
	"context"
	"time"

	"github.com/stainedhead/teams-cli/internal/domain"
)

// Graph is the Microsoft Graph port (implemented by adapters/graph).
type Graph interface {
	Me(ctx context.Context) (domain.Profile, error)
	ResolveUserChat(ctx context.Context, aadID string, create bool) (chatID string, err error)
	PostChat(ctx context.Context, chatID string, m domain.OutMessage) (domain.PostResult, error)
	PostChannel(ctx context.Context, teamID, channelID, threadID string, m domain.OutMessage) (domain.PostResult, error)
	ListChatMessages(ctx context.Context, chatID string, since time.Time, limit int) ([]domain.RawMessage, error)
	ListChannelMessages(ctx context.Context, teamID, channelID, deltaToken string, since time.Time, limit int) (msgs []domain.RawMessage, newDelta string, err error)
	ListReplies(ctx context.Context, teamID, channelID, messageID string, limit int) ([]domain.RawMessage, error)
	GetChat(ctx context.Context, chatID string) error
	FindByMarker(ctx context.Context, dest domain.Destination, marker string) (msgID string, found bool, err error)
}

// Ledger is the idempotency ledger and sent-history port (adapters/state).
type Ledger interface {
	// Claim atomically checks rate and loop limits against sent plus pending
	// history and reserves the key (or a slot for an unkeyed send) in one
	// locked operation (FR-R4).
	Claim(ctx context.Context, c domain.SendClaim) (domain.Claim, error)
	Reserve(ctx context.Context, key, payloadHash string, dest domain.Alias, thread string, now time.Time) (domain.Reservation, error)
	Complete(ctx context.Context, key, msgID string, now time.Time) error
	Fail(ctx context.Context, key string, notSent bool) error
	SentSince(ctx context.Context, since time.Time) ([]domain.Sent, error)
	SentInThread(ctx context.Context, thread string, since time.Time) (int, error)
	RecordSent(ctx context.Context, s domain.Sent) error
	PutThread(ctx context.Context, thread string) error
	ActiveThreads(ctx context.Context, since time.Time, max int) ([]string, error)
}

// CursorStore is the per-destination cursor and delivery index port.
type CursorStore interface {
	Get(ctx context.Context, alias domain.Alias) (domain.CursorState, error)
	RecordDeliveries(ctx context.Context, alias domain.Alias, d []domain.DeliveryEntry, now time.Time) error
	Ack(ctx context.Context, alias domain.Alias, ids []string, now time.Time) (acked, already int, unknown []string, err error)
	StoreDelta(ctx context.Context, alias domain.Alias, token string) error
	ResolveChat(ctx context.Context, alias domain.Alias) (chatID string, ok bool)
	CacheChat(ctx context.Context, alias domain.Alias, chatID string) error
	DropChat(ctx context.Context, alias domain.Alias) error
}

// PolicyProvider loads the trusted policy, failing closed.
type PolicyProvider interface {
	Policy(ctx context.Context) (domain.Policy, error)
}

// AuditSink records audit events.
type AuditSink interface {
	Record(ctx context.Context, e domain.AuditEvent) error
	Close() error
}

// Clock abstracts time.
type Clock interface {
	Now() time.Time
	Sleep(ctx context.Context, d time.Duration) error
}

// Rand supplies jitter.
type Rand interface {
	Jitter(base time.Duration, pct float64) time.Duration
}

// RunInfo identifies the agent and run.
type RunInfo struct {
	// PolicyPath is the policy file path, reported by whoami.
	PolicyPath string
	// AgentID is the audit identity: the policy profile.
	AgentID string
	// ClaimedAgentID is the caller-supplied AGENT_ID when it differs from
	// AgentID; it is audited so a spoofed id is detectable (FR-R1).
	ClaimedAgentID string
	RunID          string
}

// Commands is the facade the CLI calls.
type Commands interface {
	Whoami(ctx context.Context) (WhoamiResult, error)
	Destinations(ctx context.Context) (DestinationsResult, error)
	Send(ctx context.Context, r SendRequest) (SendResult, error)
	Reply(ctx context.Context, r ReplyRequest) (SendResult, error)
	Inbox(ctx context.Context, r InboxRequest) (InboxResult, error)
	Ack(ctx context.Context, r AckRequest) (AckResult, error)
	ThreadGet(ctx context.Context, r ThreadRequest) (ThreadResult, error)
	Selftest(ctx context.Context, r SelftestRequest) (SelftestResult, error)
}

// WhoamiResult is the whoami response.
type WhoamiResult struct {
	Profile       domain.Profile
	Policy        string // policy profile name
	PolicyPath    string
	PolicyVersion int
	Destinations  []DestinationView
	Limits        LimitsView
	PollInterval  time.Duration
}

// LimitsView is the subset of policy limits an agent plans around.
type LimitsView struct {
	MaxResults, MaxWritesPerRun, MaxBytes int
	RatePerMinute, RatePerHour            int
	ReplyDepthMax                         int
}

// DestinationView is a destination without any raw ids.
type DestinationView struct {
	Alias       domain.Alias
	Kind        domain.Kind
	DisplayName string
	Send, Watch bool
	// Mentionable is true when the alias is in send.mentions.allow and has
	// the id fields a mention needs.
	Mentionable bool
}

// DestinationsResult lists policy destinations.
type DestinationsResult struct {
	Destinations []DestinationView
}

// SendRequest is a send command.
type SendRequest struct {
	Alias          domain.Alias
	Text           string
	Mentions       []domain.Alias
	IdempotencyKey string
	DryRun         bool
}

// ReplyRequest is a reply command; ThreadID is the alias-qualified thread id.
type ReplyRequest struct {
	ThreadID       string
	Text           string
	Mentions       []domain.Alias
	IdempotencyKey string
	DryRun         bool
}

// SendResult is the response of send and reply.
type SendResult struct {
	MessageID    string
	ThreadID     string
	Deduplicated bool
	DryRun       bool
	Findings     []domain.Finding
	// Dry-run only (FR-6): the decision, resolved destination and the
	// rendered payload the real send would post.
	Decision    string
	Destination DestinationView
	Preview     string
	PreviewHTML bool
}

// InboxRequest is an inbox command.
type InboxRequest struct {
	Alias domain.Alias // optional filter
	Limit int
	Wait  time.Duration
	Since *time.Time
}

// InboxResult is the inbox response.
type InboxResult struct {
	Items   []domain.InboundItem
	Skipped map[string]int
}

// AckRequest is an ack command.
type AckRequest struct {
	IDs []string
}

// AckResult is the ack response.
type AckResult struct {
	Acked, Already int
}

// ThreadRequest is a thread get command.
type ThreadRequest struct {
	ThreadID string
	Limit    int
}

// ThreadResult is the thread get response.
type ThreadResult struct {
	Items []domain.InboundItem
}

// SelftestRequest is a selftest command.
type SelftestRequest struct {
	ReadOnly bool
}

// SelftestRow is one selftest result row.
type SelftestRow struct {
	Name   string
	Status string
	Detail string
}

// SelftestResult is the selftest response.
type SelftestResult struct {
	Rows []SelftestRow
}
