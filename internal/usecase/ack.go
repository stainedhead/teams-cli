package usecase

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/stainedhead/teams-cli/internal/domain"
)

// Ack implements FR-20 / D8. It is local only (no Graph call). Every id must
// be in the delivery index of a watchable destination or nothing changes.
func (s *service) Ack(ctx context.Context, r AckRequest) (res AckResult, err error) {
	err = s.exec(ctx, "ack", "", func(c *call) error {
		var e error
		res, e = s.ack(ctx, c, r)
		return e
	})
	return res, err
}

func (s *service) ack(ctx context.Context, c *call, r AckRequest) (AckResult, error) {
	if len(r.IDs) < 1 || len(r.IDs) > maxAckIDs {
		return AckResult{}, domain.NewUsage("ack takes 1 to "+strconv.Itoa(maxAckIDs)+" ids", "pass ids exactly as returned by `teams inbox`")
	}
	groups, aliases, err := splitIDs(r.IDs)
	if err != nil {
		return AckResult{}, err
	}
	if len(aliases) == 1 {
		c.resource = string(aliases[0])
	}
	p, err := s.local(ctx)
	if err != nil {
		return AckResult{}, err
	}
	for _, a := range aliases {
		if d := p.CanWatch(a); !d.Allowed {
			return AckResult{}, denyErr(c, d)
		}
	}
	// Validate every id before changing anything, across all aliases.
	var unknown []string
	for _, a := range aliases {
		cs, err := s.d.Cursors.Get(ctx, a)
		if err != nil {
			return AckResult{}, err
		}
		for _, id := range groups[a] {
			_, gid, _ := domain.ParseItemID(id)
			if _, ok := cs.Known(gid); !ok {
				unknown = append(unknown, id)
			}
		}
	}
	if len(unknown) > 0 {
		return AckResult{}, unknownIDs(unknown)
	}
	now := s.d.Clock.Now()
	var res AckResult
	for _, a := range aliases {
		acked, already, unk, err := s.d.Cursors.Ack(ctx, a, groups[a], now)
		if err != nil {
			return AckResult{}, err
		}
		if len(unk) > 0 {
			return AckResult{}, unknownIDs(unk)
		}
		res.Acked += acked
		res.Already += already
	}
	c.set("count", strconv.Itoa(res.Acked))
	c.set("already", strconv.Itoa(res.Already))
	return res, nil
}

func unknownIDs(ids []string) error {
	const show = 10
	list := ids
	more := ""
	if len(list) > show {
		more = " (and " + strconv.Itoa(len(list)-show) + " more)"
		list = list[:show]
	}
	return domain.NewValidation("ids were not delivered by inbox (or were pruned): "+strings.Join(list, ", ")+more,
		"nothing was acked; run `teams inbox` and ack the ids it returns")
}

// splitIDs de-duplicates ids and groups them by alias (sorted).
func splitIDs(ids []string) (map[domain.Alias][]string, []domain.Alias, error) {
	groups := map[domain.Alias][]string{}
	seen := map[string]bool{}
	for _, id := range ids {
		a, _, err := domain.ParseItemID(id)
		if err != nil {
			return nil, nil, err
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		groups[a] = append(groups[a], id)
	}
	aliases := make([]domain.Alias, 0, len(groups))
	for a := range groups {
		aliases = append(aliases, a)
	}
	sort.Slice(aliases, func(i, j int) bool { return aliases[i] < aliases[j] })
	return groups, aliases, nil
}
