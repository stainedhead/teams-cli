// Package usecasetest provides in-memory fakes of every usecase port.
//
// The fakes mimic the contracts of the real adapters closely enough for the
// use cases to be tested end to end: the ledger follows the Reserve state
// machine, the cursor store applies the domain cursor algebra, the Graph fake
// filters by since/limit and the clock advances on Sleep.
package usecasetest

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/stainedhead/teams-cli/internal/domain"
	"github.com/stainedhead/teams-cli/internal/usecase"
)

// Compile-time port conformance.
var (
	_ usecase.Graph          = (*Graph)(nil)
	_ usecase.Ledger         = (*Ledger)(nil)
	_ usecase.CursorStore    = (*CursorStore)(nil)
	_ usecase.PolicyProvider = (*PolicyProvider)(nil)
	_ usecase.AuditSink      = (*AuditSink)(nil)
	_ usecase.Clock          = (*Clock)(nil)
	_ usecase.Rand           = (*Rand)(nil)
)

// PostCall is one recorded post.
type PostCall struct {
	Method    string // PostChat or PostChannel
	ChatID    string
	TeamID    string
	ChannelID string
	ThreadID  string
	Msg       domain.OutMessage
}

// Graph is a fake Graph with scripted data and failures.
type Graph struct {
	mu sync.Mutex

	Profile domain.Profile
	MeErr   error

	// Calls lists every call, by method name, in order.
	Calls []string
	// Hook, if set, runs at the start of every call (outside the lock). Tests
	// use it to make messages appear as the fake clock advances.
	Hook func(method string)

	// Fail holds persistent errors per method; Once holds FIFO one-shot
	// errors per method (a nil element means "succeed this time").
	Fail map[string]error
	Once map[string][]error

	// UserChats maps a lower-cased AAD id to a 1:1 chat id. With create=true
	// a missing chat is created as "created-<aad>".
	UserChats map[string]string
	Created   []string

	ChatMessages    map[string][]domain.RawMessage // by chat id
	ChannelMessages map[string][]domain.RawMessage // by "team/channel"
	Replies         map[string][]domain.RawMessage // by "team/channel/message"
	Delta           string
	DeltaTokensSeen []string
	ListSince       []time.Time

	ChatErr map[string]error  // GetChat result per chat id
	Markers map[string]string // marker -> message id

	Posts   []PostCall
	Posted  []domain.OutMessage
	PostErr error // persistent error for both post methods
	nextID  int
}

func (g *Graph) enter(method string) error {
	g.mu.Lock()
	g.Calls = append(g.Calls, method)
	hook := g.Hook
	g.mu.Unlock()
	if hook != nil {
		hook(method)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if q := g.Once[method]; len(q) > 0 {
		err := q[0]
		g.Once[method] = q[1:]
		return err
	}
	return g.Fail[method]
}

// CallCount returns how many times method was called.
func (g *Graph) CallCount(method string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := 0
	for _, c := range g.Calls {
		if c == method {
			n++
		}
	}
	return n
}

// Me returns the scripted profile.
func (g *Graph) Me(context.Context) (domain.Profile, error) {
	if err := g.enter("Me"); err != nil {
		return domain.Profile{}, err
	}
	return g.Profile, g.MeErr
}

// ResolveUserChat finds, or with create creates, the 1:1 chat.
func (g *Graph) ResolveUserChat(_ context.Context, aad string, create bool) (string, error) {
	if err := g.enter("ResolveUserChat"); err != nil {
		return "", err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	key := strings.ToLower(aad)
	if id, ok := g.UserChats[key]; ok {
		return id, nil
	}
	if !create {
		return "", domain.NewNotFound("no one-on-one chat with that user", "set create_chat: true for the destination to allow creating one")
	}
	id := "created-" + key
	if g.UserChats == nil {
		g.UserChats = map[string]string{}
	}
	g.UserChats[key] = id
	g.Created = append(g.Created, id)
	return id, nil
}

func (g *Graph) post(call PostCall, m domain.OutMessage) (domain.PostResult, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.PostErr != nil {
		return domain.PostResult{}, g.PostErr
	}
	g.nextID++
	id := "m" + itoa(g.nextID)
	call.Msg = m
	g.Posts = append(g.Posts, call)
	g.Posted = append(g.Posted, m)
	res := domain.PostResult{MessageID: id, ThreadID: call.ThreadID}
	if call.Method == "PostChannel" && call.ThreadID == "" {
		res.ThreadID = id
	}
	return res, nil
}

// PostChat records the message.
func (g *Graph) PostChat(_ context.Context, chatID string, m domain.OutMessage) (domain.PostResult, error) {
	if err := g.enter("PostChat"); err != nil {
		return domain.PostResult{}, err
	}
	return g.post(PostCall{Method: "PostChat", ChatID: chatID}, m)
}

// PostChannel records the message.
func (g *Graph) PostChannel(_ context.Context, teamID, channelID, threadID string, m domain.OutMessage) (domain.PostResult, error) {
	if err := g.enter("PostChannel"); err != nil {
		return domain.PostResult{}, err
	}
	return g.post(PostCall{Method: "PostChannel", TeamID: teamID, ChannelID: channelID, ThreadID: threadID}, m)
}

func window(msgs []domain.RawMessage, since time.Time, limit int) []domain.RawMessage {
	var out []domain.RawMessage
	for _, m := range msgs {
		if since.IsZero() || m.Modified.After(since) {
			out = append(out, m)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Modified.Before(out[j].Modified) })
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

// ListChatMessages returns scripted chat messages newer than since.
func (g *Graph) ListChatMessages(_ context.Context, chatID string, since time.Time, limit int) ([]domain.RawMessage, error) {
	if err := g.enter("ListChatMessages"); err != nil {
		return nil, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.ListSince = append(g.ListSince, since)
	return window(g.ChatMessages[chatID], since, limit), nil
}

// ListChannelMessages returns scripted channel messages newer than since.
func (g *Graph) ListChannelMessages(_ context.Context, teamID, channelID, delta string, since time.Time, limit int) ([]domain.RawMessage, string, error) {
	if err := g.enter("ListChannelMessages"); err != nil {
		return nil, "", err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.ListSince = append(g.ListSince, since)
	g.DeltaTokensSeen = append(g.DeltaTokensSeen, delta)
	return window(g.ChannelMessages[teamID+"/"+channelID], since, limit), g.Delta, nil
}

// ListReplies returns scripted replies.
func (g *Graph) ListReplies(_ context.Context, teamID, channelID, messageID string, limit int) ([]domain.RawMessage, error) {
	if err := g.enter("ListReplies"); err != nil {
		return nil, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return window(g.Replies[teamID+"/"+channelID+"/"+messageID], time.Time{}, limit), nil
}

// GetChat returns the scripted probe result (nil by default).
func (g *Graph) GetChat(_ context.Context, chatID string) error {
	if err := g.enter("GetChat"); err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.ChatErr[chatID]
}

// FindByMarker looks the marker up in Markers.
func (g *Graph) FindByMarker(_ context.Context, _ domain.Destination, marker string) (string, bool, error) {
	if err := g.enter("FindByMarker"); err != nil {
		return "", false, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	id, ok := g.Markers[marker]
	return id, ok, nil
}

// Ledger is an in-memory ledger following the real Reserve state machine.
type Ledger struct {
	mu      sync.Mutex
	Entries map[string]domain.LedgerEntry
	Sent    []domain.Sent
	Threads map[string]time.Time
	// Fail holds persistent errors per method name (Reserve, Complete, Fail,
	// SentSince, SentInThread, RecordSent, PutThread, ActiveThreads).
	Errs map[string]error
}

func (l *Ledger) err(m string) error { return l.Errs[m] }

// Reserve claims a key (see adapters/state for the contract).
func (l *Ledger) Reserve(_ context.Context, key, hash string, dest domain.Alias, thread string, now time.Time) (domain.Reservation, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.err("Reserve"); err != nil {
		return domain.Reservation{}, err
	}
	if l.Entries == nil {
		l.Entries = map[string]domain.LedgerEntry{}
	}
	e, ok := l.Entries[key]
	if !ok {
		e = domain.LedgerEntry{Key: key, PayloadHash: hash, Alias: string(dest), ThreadID: thread, State: domain.StatePending, Created: now, Updated: now}
		l.Entries[key] = e
		return domain.Reservation{Outcome: domain.ReserveNew, Entry: e}, nil
	}
	if e.PayloadHash != hash {
		return domain.Reservation{}, domain.NewConflict("idempotency key "+key+" was used with a different payload", "use a new --idempotency-key for a different message")
	}
	switch e.State {
	case domain.StateSent:
		return domain.Reservation{Outcome: domain.ReserveReplay, Entry: e}, nil
	case domain.StateFailed:
		e.State, e.Updated = domain.StatePending, now
		l.Entries[key] = e
		return domain.Reservation{Outcome: domain.ReserveRetry, Entry: e}, nil
	}
	return domain.Reservation{}, domain.NewConflict("idempotency key "+key+" is pending: an earlier attempt may or may not have been delivered", "check the destination for the message")
}

// Complete marks a key sent and records the send in the history.
func (l *Ledger) Complete(_ context.Context, key, msgID string, now time.Time) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.err("Complete"); err != nil {
		return err
	}
	e, ok := l.Entries[key]
	if !ok {
		return domain.NewNotFound("idempotency key "+key+" was never reserved", "")
	}
	if e.State == domain.StateSent {
		return nil
	}
	e.State, e.MessageID, e.Updated = domain.StateSent, msgID, now
	l.Entries[key] = e
	l.Sent = append(l.Sent, domain.Sent{At: now, Alias: domain.Alias(e.Alias), ThreadID: e.ThreadID, MessageID: msgID, Key: key})
	return nil
}

// Fail marks a pending key failed when notSent, else leaves it pending.
func (l *Ledger) Fail(_ context.Context, key string, notSent bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.err("Fail"); err != nil {
		return err
	}
	e, ok := l.Entries[key]
	if !ok {
		return domain.NewNotFound("idempotency key "+key+" was never reserved", "")
	}
	if notSent && e.State == domain.StatePending {
		e.State = domain.StateFailed
		l.Entries[key] = e
	}
	return nil
}

// SentSince returns history plus pending reservations at or after since.
func (l *Ledger) SentSince(_ context.Context, since time.Time) ([]domain.Sent, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.err("SentSince"); err != nil {
		return nil, err
	}
	return l.history(since), nil
}

func (l *Ledger) history(since time.Time) []domain.Sent {
	var out []domain.Sent
	done := map[string]bool{}
	for _, s := range l.Sent {
		if s.Key != "" {
			done[s.Key] = true
		}
		if !s.At.Before(since) {
			out = append(out, s)
		}
	}
	for k, e := range l.Entries {
		if e.State == domain.StatePending && !done[k] && !e.Created.Before(since) {
			out = append(out, domain.Sent{At: e.Created, Alias: domain.Alias(e.Alias), ThreadID: e.ThreadID, Key: k})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}

// SentInThread counts history entries for thread at or after since.
func (l *Ledger) SentInThread(_ context.Context, thread string, since time.Time) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.err("SentInThread"); err != nil {
		return 0, err
	}
	n := 0
	for _, s := range l.history(since) {
		if s.ThreadID == thread {
			n++
		}
	}
	return n, nil
}

// RecordSent appends to the history.
func (l *Ledger) RecordSent(_ context.Context, s domain.Sent) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.err("RecordSent"); err != nil {
		return err
	}
	l.Sent = append(l.Sent, s)
	return nil
}

// PutThread records a thread as active now (the time of the last send).
func (l *Ledger) PutThread(_ context.Context, t string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.err("PutThread"); err != nil {
		return err
	}
	if l.Threads == nil {
		l.Threads = map[string]time.Time{}
	}
	l.Threads[t] = time.Now()
	return nil
}

// ActiveThreads returns recorded threads, newest first, up to max (<=0: all).
func (l *Ledger) ActiveThreads(_ context.Context, _ time.Time, max int) ([]string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.err("ActiveThreads"); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(l.Threads))
	for t := range l.Threads {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		if !l.Threads[out[i]].Equal(l.Threads[out[j]]) {
			return l.Threads[out[i]].After(l.Threads[out[j]])
		}
		return out[i] < out[j]
	})
	if max > 0 && len(out) > max {
		out = out[:max]
	}
	return out, nil
}

// CursorStore is an in-memory cursor store using the domain cursor algebra.
type CursorStore struct {
	mu      sync.Mutex
	States  map[domain.Alias]domain.CursorState
	ChatIDs map[domain.Alias]string
	// Keep is the delivery-index retention (default 2h).
	Keep time.Duration
	// Fail holds persistent errors per method name.
	Errs map[string]error

	Deltas  map[domain.Alias]string
	Dropped []domain.Alias
}

// Get returns the stored state.
func (c *CursorStore) Get(_ context.Context, a domain.Alias) (domain.CursorState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.Errs["Get"]; err != nil {
		return domain.CursorState{}, err
	}
	return c.States[a], nil
}

func (c *CursorStore) put(a domain.Alias, s domain.CursorState) {
	if c.States == nil {
		c.States = map[domain.Alias]domain.CursorState{}
	}
	c.States[a] = s
}

// RecordDeliveries adds to the delivery index.
func (c *CursorStore) RecordDeliveries(_ context.Context, a domain.Alias, d []domain.DeliveryEntry, now time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.Errs["RecordDeliveries"]; err != nil {
		return err
	}
	keep := c.Keep
	if keep == 0 {
		keep = 2 * time.Hour
	}
	c.put(a, c.States[a].Record(d, now, keep))
	return nil
}

// Ack acks delivered ids atomically; unknown ids change nothing.
func (c *CursorStore) Ack(_ context.Context, a domain.Alias, ids []string, _ time.Time) (int, int, []string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.Errs["Ack"]; err != nil {
		return 0, 0, nil, err
	}
	cs := c.States[a]
	prefix := string(a) + "/"
	var entries []domain.AckEntry
	var unknown []string
	seen := map[string]bool{}
	for _, raw := range ids {
		id := strings.TrimPrefix(raw, prefix)
		if seen[id] {
			continue
		}
		seen[id] = true
		var best time.Time
		found := false
		for _, d := range cs.Delivered {
			if d.ID == id && (!found || d.Modified.After(best)) {
				best, found = d.Modified, true
			}
		}
		if !found {
			unknown = append(unknown, raw)
			continue
		}
		entries = append(entries, domain.AckEntry{ID: id, Modified: best})
	}
	if len(unknown) > 0 {
		return 0, 0, unknown, nil
	}
	next, acked, already := cs.Ack(entries)
	c.put(a, next)
	return acked, already, nil, nil
}

// StoreDelta saves the delta token.
func (c *CursorStore) StoreDelta(_ context.Context, a domain.Alias, token string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Deltas == nil {
		c.Deltas = map[domain.Alias]string{}
	}
	c.Deltas[a] = token
	s := c.States[a]
	s.DeltaToken = token
	c.put(a, s)
	return nil
}

// ResolveChat returns a cached chat id.
func (c *CursorStore) ResolveChat(_ context.Context, a domain.Alias) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	id, ok := c.ChatIDs[a]
	return id, ok
}

// CacheChat stores a chat id.
func (c *CursorStore) CacheChat(_ context.Context, a domain.Alias, id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ChatIDs == nil {
		c.ChatIDs = map[domain.Alias]string{}
	}
	c.ChatIDs[a] = id
	return nil
}

// DropChat removes a cached chat id.
func (c *CursorStore) DropChat(_ context.Context, a domain.Alias) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.ChatIDs, a)
	c.Dropped = append(c.Dropped, a)
	return nil
}

// PolicyProvider serves a fixed policy.
type PolicyProvider struct {
	P     domain.Policy
	Err   error
	Calls int
}

// Policy returns the fixed policy.
func (p *PolicyProvider) Policy(context.Context) (domain.Policy, error) {
	p.Calls++
	return p.P, p.Err
}

// AuditSink records events in memory.
type AuditSink struct {
	mu     sync.Mutex
	Events []domain.AuditEvent
	Err    error // returned by Record (event is still kept)
	// ErrAfter lets the first ErrAfter records succeed before Err applies.
	ErrAfter int
	Closed   bool
}

// Record appends an event.
func (a *AuditSink) Record(_ context.Context, e domain.AuditEvent) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Events = append(a.Events, e)
	if len(a.Events) <= a.ErrAfter {
		return nil
	}
	return a.Err
}

// Close marks the sink closed.
func (a *AuditSink) Close() error { a.mu.Lock(); a.Closed = true; a.mu.Unlock(); return nil }

// Clock is a fake clock; Sleep advances time instantly.
type Clock struct {
	mu      sync.Mutex
	T       time.Time
	Slept   []time.Duration
	OnSleep func(d time.Duration) // runs after the time advanced
}

// Now returns the fake time.
func (c *Clock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.T }

// Advance moves the fake time forward.
func (c *Clock) Advance(d time.Duration) { c.mu.Lock(); c.T = c.T.Add(d); c.mu.Unlock() }

// Sleep advances the fake time or returns the context error.
func (c *Clock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	c.Slept = append(c.Slept, d)
	c.T = c.T.Add(d)
	hook := c.OnSleep
	c.mu.Unlock()
	if hook != nil {
		hook(d)
	}
	return nil
}

// Rand returns base plus Offset (no randomness).
type Rand struct{ Offset time.Duration }

// Jitter returns base+Offset.
func (r Rand) Jitter(base time.Duration, _ float64) time.Duration { return base + r.Offset }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}
