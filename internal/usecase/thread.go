package usecase

import (
	"context"
	"sort"
	"strconv"

	"github.com/stainedhead/teams-cli/internal/domain"
)

// ThreadGet implements FR-23: bounded, oldest-first, classified context. It
// never touches cursors, acks or the delivery index.
func (s *service) ThreadGet(ctx context.Context, r ThreadRequest) (res ThreadResult, err error) {
	alias, root, perr := domain.ParseItemID(r.ThreadID)
	err = s.exec(ctx, "thread_get", string(alias), func(c *call) error {
		if perr != nil {
			return perr
		}
		var e error
		res, e = s.threadGet(ctx, c, alias, root, r.Limit)
		return e
	})
	return res, err
}

func (s *service) threadGet(ctx context.Context, c *call, alias domain.Alias, root string, limit int) (ThreadResult, error) {
	p, me, err := s.begin(ctx, c)
	if err != nil {
		return ThreadResult{}, err
	}
	if d := p.CanWatch(alias); !d.Allowed {
		return ThreadResult{}, denyErr(c, d)
	}
	n, err := clampLimit(p, limit)
	if err != nil {
		return ThreadResult{}, err
	}
	dest, _ := p.Destination(alias)
	var raws []domain.RawMessage
	if dest.Kind == domain.KindChannel {
		raws, err = s.d.Graph.ListReplies(ctx, dest.TeamID, dest.ChannelID, root, n)
		for i := range raws {
			if raws[i].ThreadID == "" {
				raws[i].ThreadID = root
			}
		}
	} else {
		err = s.withChat(ctx, dest, func(chatID string) error {
			var e error
			raws, e = s.d.Graph.ListChatMessages(ctx, chatID, zeroTime, n)
			return e
		})
	}
	if err != nil {
		return ThreadResult{}, err
	}
	items := make([]domain.InboundItem, 0, len(raws))
	dropped := 0
	for _, m := range raws {
		it, out := domain.Normalize(p, dest, m, me.ID)
		if out.Kind != domain.NormalizeOK {
			dropped++
			continue
		}
		items = append(items, it)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if !items[i].Received.Equal(items[j].Received) {
			return items[i].Received.Before(items[j].Received)
		}
		return items[i].ID < items[j].ID
	})
	if len(items) > n {
		items = items[len(items)-n:]
	}
	c.set("count", strconv.Itoa(len(items)))
	c.set("dropped", strconv.Itoa(dropped))
	return ThreadResult{Items: items}, nil
}
