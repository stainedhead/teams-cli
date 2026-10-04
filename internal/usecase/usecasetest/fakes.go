// Package usecasetest provides in-memory fakes of every usecase port.
package usecasetest

import (
	"context"
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

// Graph is a fake Graph with scripted responses. Skeleton: WS-B extends it.
type Graph struct {
	mu       sync.Mutex
	Profile  domain.Profile
	MeErr    error
	Calls    []string
	Messages map[string][]domain.RawMessage
	PostErr  error
	Posted   []domain.OutMessage
}

func (g *Graph) rec(s string) { g.mu.Lock(); g.Calls = append(g.Calls, s); g.mu.Unlock() }

// Me returns the scripted profile.
func (g *Graph) Me(context.Context) (domain.Profile, error) { g.rec("Me"); return g.Profile, g.MeErr }

// ResolveUserChat is a skeleton.
func (g *Graph) ResolveUserChat(context.Context, string, bool) (string, error) {
	g.rec("ResolveUserChat")
	return "", nil
}

// PostChat records the message.
func (g *Graph) PostChat(_ context.Context, _ string, m domain.OutMessage) (domain.PostResult, error) {
	g.rec("PostChat")
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.PostErr != nil {
		return domain.PostResult{}, g.PostErr
	}
	g.Posted = append(g.Posted, m)
	return domain.PostResult{}, nil
}

// PostChannel records the message.
func (g *Graph) PostChannel(_ context.Context, _, _, _ string, m domain.OutMessage) (domain.PostResult, error) {
	g.rec("PostChannel")
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.PostErr != nil {
		return domain.PostResult{}, g.PostErr
	}
	g.Posted = append(g.Posted, m)
	return domain.PostResult{}, nil
}

// ListChatMessages is a skeleton.
func (g *Graph) ListChatMessages(context.Context, string, time.Time, int) ([]domain.RawMessage, error) {
	g.rec("ListChatMessages")
	return nil, nil
}

// ListChannelMessages is a skeleton.
func (g *Graph) ListChannelMessages(context.Context, string, string, string, time.Time, int) ([]domain.RawMessage, string, error) {
	g.rec("ListChannelMessages")
	return nil, "", nil
}

// ListReplies is a skeleton.
func (g *Graph) ListReplies(context.Context, string, string, string, int) ([]domain.RawMessage, error) {
	g.rec("ListReplies")
	return nil, nil
}

// GetChat is a skeleton.
func (g *Graph) GetChat(context.Context, string) error { g.rec("GetChat"); return nil }

// FindByMarker is a skeleton.
func (g *Graph) FindByMarker(context.Context, domain.Destination, string) (string, bool, error) {
	g.rec("FindByMarker")
	return "", false, nil
}

// Ledger is an in-memory ledger skeleton.
type Ledger struct {
	mu      sync.Mutex
	Entries map[string]domain.LedgerEntry
	Sent    []domain.Sent
	Threads []string
}

// Reserve is a skeleton that always reserves a new entry.
func (l *Ledger) Reserve(_ context.Context, key, hash string, dest domain.Alias, thread string, now time.Time) (domain.Reservation, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.Entries == nil {
		l.Entries = map[string]domain.LedgerEntry{}
	}
	e := domain.LedgerEntry{Key: key, PayloadHash: hash, Alias: string(dest), ThreadID: thread, State: domain.StatePending, Created: now, Updated: now}
	l.Entries[key] = e
	return domain.Reservation{Outcome: domain.ReserveNew, Entry: e}, nil
}

// Complete is a skeleton.
func (l *Ledger) Complete(context.Context, string, string, time.Time) error { return nil }

// Fail is a skeleton.
func (l *Ledger) Fail(context.Context, string, bool) error { return nil }

// SentSince returns recorded sends at or after since.
func (l *Ledger) SentSince(_ context.Context, since time.Time) ([]domain.Sent, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []domain.Sent
	for _, s := range l.Sent {
		if !s.At.Before(since) {
			out = append(out, s)
		}
	}
	return out, nil
}

// SentInThread counts recorded sends in a thread at or after since.
func (l *Ledger) SentInThread(_ context.Context, thread string, since time.Time) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, s := range l.Sent {
		if s.ThreadID == thread && !s.At.Before(since) {
			n++
		}
	}
	return n, nil
}

// RecordSent appends to the history.
func (l *Ledger) RecordSent(_ context.Context, s domain.Sent) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.Sent = append(l.Sent, s)
	return nil
}

// PutThread records a thread.
func (l *Ledger) PutThread(_ context.Context, t string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.Threads = append(l.Threads, t)
	return nil
}

// ActiveThreads returns recorded threads up to max.
func (l *Ledger) ActiveThreads(_ context.Context, _ time.Time, max int) ([]string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if max >= 0 && len(l.Threads) > max {
		return append([]string(nil), l.Threads[:max]...), nil
	}
	return append([]string(nil), l.Threads...), nil
}

// CursorStore is an in-memory cursor store skeleton.
type CursorStore struct {
	mu      sync.Mutex
	States  map[domain.Alias]domain.CursorState
	ChatIDs map[domain.Alias]string
}

// Get returns the stored state.
func (c *CursorStore) Get(_ context.Context, a domain.Alias) (domain.CursorState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.States[a], nil
}

// RecordDeliveries is a skeleton.
func (c *CursorStore) RecordDeliveries(context.Context, domain.Alias, []domain.DeliveryEntry, time.Time) error {
	return nil
}

// Ack is a skeleton.
func (c *CursorStore) Ack(context.Context, domain.Alias, []string, time.Time) (int, int, []string, error) {
	return 0, 0, nil, nil
}

// StoreDelta is a skeleton.
func (c *CursorStore) StoreDelta(context.Context, domain.Alias, string) error { return nil }

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
	return nil
}

// PolicyProvider serves a fixed policy.
type PolicyProvider struct {
	P   domain.Policy
	Err error
}

// Policy returns the fixed policy.
func (p *PolicyProvider) Policy(context.Context) (domain.Policy, error) { return p.P, p.Err }

// AuditSink records events in memory.
type AuditSink struct {
	mu     sync.Mutex
	Events []domain.AuditEvent
	Closed bool
}

// Record appends an event.
func (a *AuditSink) Record(_ context.Context, e domain.AuditEvent) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Events = append(a.Events, e)
	return nil
}

// Close marks the sink closed.
func (a *AuditSink) Close() error { a.mu.Lock(); a.Closed = true; a.mu.Unlock(); return nil }

// Clock is a fake clock; Sleep advances time instantly.
type Clock struct {
	mu    sync.Mutex
	T     time.Time
	Slept []time.Duration
}

// Now returns the fake time.
func (c *Clock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.T }

// Sleep advances the fake time or returns the context error.
func (c *Clock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Slept = append(c.Slept, d)
	c.T = c.T.Add(d)
	return nil
}

// Rand returns base unchanged (no jitter).
type Rand struct{}

// Jitter returns base.
func (Rand) Jitter(base time.Duration, _ float64) time.Duration { return base }
