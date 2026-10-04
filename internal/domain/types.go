// Package domain holds the teams-cli entities, value objects, policy model and
// pure evaluation logic. It imports only the standard library and the core
// output package (for the error category taxonomy).
package domain

import (
	"time"

	"github.com/stainedhead/agent-cli-core/output"
)

// Alias is a policy alias of the form kind:name (kind in channel, chat, user).
type Alias string

// AADID is a GUID string, compared lowercase-normalized.
type AADID string

// Kind is the destination kind; ConvType shares the same values.
type Kind string

// ConvType is the conversation type of an inbound item.
type ConvType = Kind

// Destination kinds.
const (
	KindChannel Kind = "channel"
	KindChat    Kind = "chat"
	KindUser    Kind = "user"
)

// InboundHandle selects which inbound messages are surfaced.
type InboundHandle string

// Inbound handles.
const (
	HandleDirect   InboundHandle = "direct"
	HandleMentions InboundHandle = "mentions"
	HandleWatched  InboundHandle = "watched"
)

// SenderKind is the Graph sender kind.
type SenderKind string

// Sender kinds.
const (
	SenderUser        SenderKind = "user"
	SenderApplication SenderKind = "application"
	SenderBot         SenderKind = "bot"
	SenderUnknown     SenderKind = "unknown"
)

// EntryState is a ledger entry state.
type EntryState string

// Ledger entry states.
const (
	StatePending EntryState = "pending"
	StateSent    EntryState = "sent"
	StateFailed  EntryState = "failed"
)

// ReserveOutcome is the result of reserving an idempotency key.
type ReserveOutcome string

// Reserve outcomes.
const (
	ReserveNew    ReserveOutcome = "new"
	ReserveReplay ReserveOutcome = "replay"
	ReserveRetry  ReserveOutcome = "retry"
)

// Content filter names.
const (
	FilterSecretPatterns        = "secret_patterns"
	FilterClassificationMarkers = "classification_markers"
)

// Profile is the agent's own Graph profile (GET /me).
type Profile struct {
	ID, DisplayName, UPN string
}

// Destination is one policy destination.
type Destination struct {
	Alias                                         Alias
	Kind                                          Kind
	TeamID, ChannelID, ChatID, AADID, DisplayName string
	Send, Watch, CreateChat                       bool
}

// Instruct lists the principals whose messages may instruct the agent.
type Instruct struct {
	Commanders, Agents []AADID
}

// Inbound is the inbound policy section.
type Inbound struct {
	Handle                             []InboundHandle
	MaxLookback, PollInterval, MaxWait time.Duration
	ThreadPollMax                      int
}

// MentionPolicy is the send.mentions policy section.
type MentionPolicy struct {
	Allow          []Alias
	BlockBroadcast bool
	Max            int
}

// Rate is a sliding-window send rate.
type Rate struct {
	PerMinute, PerHour int
}

// SendPolicy is the send policy section.
type SendPolicy struct {
	MaxBytes              int
	Mentions              MentionPolicy
	ContentFilters        []string
	ClassificationMarkers []string
	LinkAllowlist         []string
	Rate                  Rate
	ReplyDepthMax         int
	ReplyWindow           time.Duration
	Prefix                string
	MarkerScan            bool
}

// Limits are per-run and per-listing bounds.
type Limits struct {
	MaxResults, MaxWritesPerRun, MaxChatScan int
}

// AuditCfg is the audit policy section.
type AuditCfg struct {
	Path string
}

// SelftestCfg is the selftest policy section.
type SelftestCfg struct {
	NonMemberChatID string
}

// Policy is the immutable, loaded policy.
type Policy struct {
	Version                          int
	Profile, UPN, TenantID, StateDir string
	// StateDirPinned is true when the policy file sets state_dir explicitly;
	// the agent-controlled TEAMS_STATE_DIR then cannot override it (FR-R1).
	StateDirPinned bool
	Destinations   map[Alias]Destination
	Instruct       Instruct
	Inbound        Inbound
	Send           SendPolicy
	Limits         Limits
	Audit          AuditCfg
	Selftest       SelftestCfg
}

// RawMention is a mention as read from Graph.
type RawMention struct {
	UserID, Text string
}

// RawSender is the sender of a raw message.
type RawSender struct {
	UserID, TenantID, Name string
	Kind                   SenderKind
}

// RawMessage is adapter output with no policy applied.
type RawMessage struct {
	ID, ThreadID, MessageType          string
	Created, Modified                  time.Time
	Deleted                            bool
	FromUserID, FromTenantID, FromName string
	FromKind                           SenderKind
	BodyType, BodyContent              string
	Mentions                           []RawMention
	Alias                              Alias
	ChatID, TeamID, ChannelID          string
}

// Sender is a classified sender.
type Sender struct {
	Name                 string
	AADID                string
	CanInstruct, IsAgent bool
}

// Conversation identifies where an item came from.
type Conversation struct {
	Type  ConvType
	Alias Alias
}

// InboundItem is a normalized, classified inbound message.
type InboundItem struct {
	ID, ThreadID string
	Received     time.Time
	Cursor       string
	Edited       bool
	Conversation Conversation
	Sender       Sender
	MentionedYou bool
	Text         string
	Links        []string
}

// NormalizeKind is the outcome class of Normalize.
type NormalizeKind string

// Normalize outcome kinds.
const (
	NormalizeOK         NormalizeKind = "ok"
	NormalizeSystem     NormalizeKind = "system"
	NormalizeDeleted    NormalizeKind = "deleted"
	NormalizeOwn        NormalizeKind = "own"
	NormalizeIncomplete NormalizeKind = "incomplete"
)

// NormalizeOutcome reports whether a message was kept and why not.
type NormalizeOutcome struct {
	Kind   NormalizeKind
	Reason string
}

// OutMention is a mention in an outgoing message.
type OutMention struct {
	ID                 int
	AADID, DisplayName string
}

// OutMessage is what the adapter posts.
type OutMessage struct {
	Text      string
	HTML      bool
	Mentions  []OutMention
	MarkerKey string
}

// PostResult is the result of a post.
type PostResult struct {
	MessageID, ThreadID string
	Created             time.Time
}

// LedgerEntry is an idempotency ledger entry.
type LedgerEntry struct {
	Key, PayloadHash, Alias, ThreadID, MessageID string
	State                                        EntryState
	Created, Updated                             time.Time
}

// Reservation is the result of Ledger.Reserve.
type Reservation struct {
	Outcome ReserveOutcome
	Entry   LedgerEntry
}

// Sent is a sent-history record used for rate and loop guards.
type Sent struct {
	At                       time.Time
	Alias                    Alias
	ThreadID, MessageID, Key string
}

// AckEntry is an acked message id and the version acked.
type AckEntry struct {
	ID       string
	Modified time.Time
}

// DeliveryEntry is a delivery index entry.
type DeliveryEntry struct {
	ID, ThreadID          string
	Modified, DeliveredAt time.Time
}

// CursorState is the per-destination cursor.
type CursorState struct {
	Watermark  time.Time
	Acked      []AckEntry
	Delivered  []DeliveryEntry
	DeltaToken string
	ChatID     string
	Updated    time.Time
}

// AuditEvent is one audit record before folding into the core audit record.
type AuditEvent struct {
	Verb, Resource, Outcome string
	HTTPStatus              int
	Duration                time.Duration
	Decision                string
	Extra                   map[string]string
}

// Decision is the result of a pure policy evaluation.
type Decision struct {
	Allowed    bool
	Category   output.Category
	Reason     string
	RuleID     string
	RetryAfter time.Duration
}

// Finding is a content-filter hit; it never carries matched text.
type Finding struct {
	Filter, PatternID string
}
