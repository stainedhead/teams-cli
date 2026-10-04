package usecase

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"time"

	"github.com/stainedhead/agent-cli-core/output"

	"github.com/stainedhead/teams-cli/internal/domain"
)

// Drop reasons added by the use case (Normalize supplies the others).
const (
	reasonHandle = "handle_not_enabled"
	// reasonTruncated marks a poll that found more than one page of messages;
	// the agent should poll again (FR-R2).
	reasonTruncated = "truncated"
)

// retryAfterer is implemented by throttling errors that know Graph's
// Retry-After; inbox --wait widens its sleep by it (FR-27).
type retryAfterer interface{ RetryAfter() time.Duration }

// pollStats accumulates counts across the poll cycles of one inbox call.
type pollStats struct {
	dropped map[string]int // reason -> count (never content)
	skipped map[string]int // "alias:reason" -> count
}

func newStats() *pollStats {
	return &pollStats{dropped: map[string]int{}, skipped: map[string]int{}}
}

func (st *pollStats) merge(o *pollStats) {
	for k, v := range o.dropped {
		st.dropped[k] += v
	}
	for k, v := range o.skipped {
		st.skipped[k] += v
	}
}

func (st *pollStats) droppedTotal() int {
	n := 0
	for _, v := range st.dropped {
		n += v
	}
	return n
}

// Inbox implements FR-16..FR-18 (D4, D6, D8). It records deliveries but never
// moves a watermark or an acked set.
func (s *service) Inbox(ctx context.Context, r InboxRequest) (res InboxResult, err error) {
	err = s.exec(ctx, "inbox", string(r.Alias), func(c *call) error {
		var e error
		res, e = s.inbox(ctx, c, r)
		return e
	})
	return res, err
}

func (s *service) inbox(ctx context.Context, c *call, r InboxRequest) (InboxResult, error) {
	p, me, err := s.begin(ctx, c)
	if err != nil {
		return InboxResult{}, err
	}
	dests, err := s.inboxTargets(c, p, r.Alias)
	if err != nil {
		return InboxResult{}, err
	}
	limit, err := clampLimit(p, r.Limit)
	if err != nil {
		return InboxResult{}, err
	}
	if r.Wait < 0 {
		return InboxResult{}, domain.NewUsage("wait must not be negative", "")
	}
	wait := r.Wait
	maxWait := p.Inbound.MaxWait
	if maxWait <= 0 {
		maxWait = defaultMaxWait
	}
	if wait > maxWait {
		wait = maxWait
		c.set("wait_clamped", "true")
	}
	interval := p.Inbound.PollInterval
	if interval <= 0 {
		interval = defaultPollInterval
	}
	if interval < pollFloor {
		interval = pollFloor
	}
	deadline := s.d.Clock.Now().Add(wait)
	stats := newStats()
	for {
		items, cycle, perr := s.poll(ctx, p, me.ID, dests, r.Since, limit)
		stats.merge(cycle)
		if perr == nil && len(items) > 0 {
			if err := s.deliverItems(ctx, items); err != nil {
				return InboxResult{}, err
			}
			return s.inboxResult(c, items, stats), nil
		}
		sleep := s.d.Rand.Jitter(interval, pollJitter)
		if sleep < pollFloor {
			sleep = pollFloor
		}
		if perr != nil {
			if output.CategoryOf(perr) != output.CategoryRateLimited {
				return InboxResult{}, perr
			}
			var ra retryAfterer
			if errors.As(perr, &ra) && ra.RetryAfter() > sleep {
				sleep = ra.RetryAfter()
			}
		}
		remaining := deadline.Sub(s.d.Clock.Now())
		if remaining < pollFloor {
			if perr != nil {
				return InboxResult{}, perr
			}
			return s.inboxResult(c, nil, stats), nil
		}
		if sleep > remaining {
			sleep = remaining
		}
		if err := s.d.Clock.Sleep(ctx, sleep); err != nil {
			return InboxResult{}, err
		}
	}
}

// inboxTargets selects the destinations to poll: the watch:true destinations
// in alias order, or the one named by the filter (which must be watchable).
func (s *service) inboxTargets(c *call, p domain.Policy, filter domain.Alias) ([]domain.Destination, error) {
	if filter != "" {
		if _, err := domain.ParseAlias(string(filter)); err != nil {
			return nil, err
		}
		if d := p.CanWatch(filter); !d.Allowed {
			return nil, denyErr(c, d)
		}
		d, _ := p.Destination(filter)
		return []domain.Destination{d}, nil
	}
	return p.Watched(), nil
}

func (s *service) inboxResult(c *call, items []domain.InboundItem, st *pollStats) InboxResult {
	if items == nil {
		items = []domain.InboundItem{}
	}
	skipped := map[string]int{}
	for k, v := range st.dropped {
		skipped[k] = v
	}
	for k, v := range st.skipped {
		skipped[k] = v
	}
	c.set("count", strconv.Itoa(len(items)))
	c.set("dropped", strconv.Itoa(st.droppedTotal()))
	if len(st.skipped) > 0 {
		c.set("skipped", joinSkipped(st.skipped))
	}
	return InboxResult{Items: items, Skipped: skipped}
}

// poll runs one cycle over dests and returns the merged, limited items. A
// failing destination fails the cycle (except an unresolvable user chat).
func (s *service) poll(ctx context.Context, p domain.Policy, agentID string, dests []domain.Destination, since *time.Time, limit int) ([]domain.InboundItem, *pollStats, error) {
	st := newStats()
	var all []domain.InboundItem
	now := s.d.Clock.Now()
	for _, d := range dests {
		items, err := s.pollDest(ctx, p, agentID, d, since, st, now)
		if err != nil {
			var nc *noChatError
			if errors.As(err, &nc) {
				st.skipped[string(d.Alias)+":no_chat"]++
				continue
			}
			return nil, st, err
		}
		all = append(all, items...)
	}
	sort.SliceStable(all, func(i, j int) bool {
		if !all[i].Received.Equal(all[j].Received) {
			return all[i].Received.Before(all[j].Received)
		}
		return all[i].ID < all[j].ID
	})
	if len(all) > limit {
		all = all[:limit]
		st.skipped["all:"+reasonTruncated]++
	}
	return all, st, nil
}

// pollDest fetches, normalizes, classifies and filters one destination.
func (s *service) pollDest(ctx context.Context, p domain.Policy, agentID string, d domain.Destination, override *time.Time, st *pollStats, now time.Time) ([]domain.InboundItem, error) {
	cs, err := s.d.Cursors.Get(ctx, d.Alias)
	if err != nil {
		return nil, err
	}
	lb := lookback(p)
	since := domain.Since(cs, now, lb, override)
	raws, err := s.fetch(ctx, p, d, cs, since)
	if err != nil {
		return nil, err
	}
	var truncated bool
	if raws, truncated = capOldestRaw(raws, maxResults(p)); truncated {
		st.skipped[string(d.Alias)+":"+reasonTruncated]++
	}
	seen := map[string]bool{}
	var items []domain.InboundItem
	for _, m := range raws {
		if seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		it, out := domain.Normalize(p, d, m, agentID)
		if out.Kind != domain.NormalizeOK {
			st.dropped[out.Reason]++
			continue
		}
		if _, ok := domain.SelectHandle(p, d, m, agentID); !ok {
			st.dropped[reasonHandle]++
			continue
		}
		items = append(items, it)
	}
	state := cs
	if override != nil {
		state = domain.CursorState{} // replay ignores watermark and acks (D8)
	}
	return state.Undelivered(items, now, lb), nil
}

// capOldestRaw keeps the oldest n messages (oldest first) so the watermark can
// never advance past an undelivered one, and reports whether it cut anything.
// Messages sharing the last kept modified time are kept too, because a
// watermark covers a whole timestamp.
func capOldestRaw(in []domain.RawMessage, n int) ([]domain.RawMessage, bool) {
	sort.SliceStable(in, func(i, j int) bool { return in[i].Modified.Before(in[j].Modified) })
	if n <= 0 || len(in) <= n {
		return in, false
	}
	end := n
	for end < len(in) && in[end].Modified.Equal(in[n-1].Modified) {
		end++
	}
	return in[:end], end < len(in)
}

// fetch lists one destination: chat/user via the chat id, channels via the
// channel list plus replies of threads the agent posted in (UA-3).
func (s *service) fetch(ctx context.Context, p domain.Policy, d domain.Destination, cs domain.CursorState, since time.Time) ([]domain.RawMessage, error) {
	n := maxResults(p)
	if d.Kind != domain.KindChannel {
		var msgs []domain.RawMessage
		err := s.withChat(ctx, d, func(chatID string) error {
			var e error
			msgs, e = s.d.Graph.ListChatMessages(ctx, chatID, since, 0) // whole window; capped oldest-first in pollDest (FR-R2)
			return e
		})
		return msgs, err
	}
	// The delta token is deliberately not used: a delta read returns only
	// changes since the token, which would hide un-acked messages that D8
	// requires inbox to re-deliver until acked.
	msgs, _, err := s.d.Graph.ListChannelMessages(ctx, d.TeamID, d.ChannelID, "", since, 0)
	if err != nil {
		return nil, err
	}
	replies, err := s.channelReplies(ctx, p, d, n)
	if err != nil {
		return nil, err
	}
	return append(msgs, replies...), nil
}

func (s *service) channelReplies(ctx context.Context, p domain.Policy, d domain.Destination, n int) ([]domain.RawMessage, error) {
	max := p.Inbound.ThreadPollMax
	if max <= 0 {
		return nil, nil
	}
	threads, err := s.d.Ledger.ActiveThreads(ctx, s.d.Clock.Now().Add(-lookback(p)), 0)
	if err != nil {
		return nil, err
	}
	var out []domain.RawMessage
	polled := 0
	for _, t := range threads {
		if polled >= max {
			break
		}
		a, root, err := domain.ParseItemID(t)
		if err != nil || a != d.Alias {
			continue
		}
		polled++
		rs, err := s.d.Graph.ListReplies(ctx, d.TeamID, d.ChannelID, root, n)
		if err != nil {
			return nil, err
		}
		for _, m := range rs {
			if m.ThreadID == "" {
				m.ThreadID = root
			}
			out = append(out, m)
		}
	}
	return out, nil
}

// deliverItems records the delivery index entries so ack can validate ids.
func (s *service) deliverItems(ctx context.Context, items []domain.InboundItem) error {
	now := s.d.Clock.Now()
	by := map[domain.Alias][]domain.DeliveryEntry{}
	var order []domain.Alias
	for _, it := range items {
		a, gid, err := domain.ParseItemID(it.ID)
		if err != nil {
			return err
		}
		mod, derr := domain.DecodeCursor(it.Cursor)
		if derr != nil {
			mod = it.Received
		}
		if _, ok := by[a]; !ok {
			order = append(order, a)
		}
		by[a] = append(by[a], domain.DeliveryEntry{ID: gid, ThreadID: it.ThreadID, Modified: mod, DeliveredAt: now})
	}
	for _, a := range order {
		if err := s.d.Cursors.RecordDeliveries(ctx, a, by[a], now); err != nil {
			return err
		}
	}
	return nil
}
