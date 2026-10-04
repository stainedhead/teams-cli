package usecase

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/stainedhead/teams-cli/internal/domain"
)

// destinationViews lists the policy destinations sorted by alias, without raw ids.
func destinationViews(p domain.Policy) []DestinationView {
	allowed := map[domain.Alias]bool{}
	for _, a := range p.Send.Mentions.Allow {
		allowed[a] = true
	}
	views := make([]DestinationView, 0, len(p.Destinations))
	for a, d := range p.Destinations {
		views = append(views, DestinationView{
			Alias: a, Kind: a.Kind(), DisplayName: d.DisplayName, Send: d.Send, Watch: d.Watch,
			Mentionable: allowed[a] && a.Kind() == domain.KindUser && d.AADID != "" && strings.TrimSpace(d.DisplayName) != "",
		})
	}
	sort.Slice(views, func(i, j int) bool { return views[i].Alias < views[j].Alias })
	return views
}

// Destinations implements FR-2: policy destinations, no network, no raw ids.
func (s *service) Destinations(ctx context.Context) (res DestinationsResult, err error) {
	err = s.exec(ctx, "destinations", "", func(c *call) error {
		p, e := s.local(ctx)
		if e != nil {
			return e
		}
		views := destinationViews(p)
		res = DestinationsResult{Destinations: views}
		c.set("count", strconv.Itoa(len(views)))
		return nil
	})
	return res, err
}
