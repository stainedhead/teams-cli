package usecase

import (
	"context"
	"sort"
	"strconv"
)

// Destinations implements FR-2: policy destinations, no network, no raw ids.
func (s *service) Destinations(ctx context.Context) (res DestinationsResult, err error) {
	err = s.exec(ctx, "destinations", "", func(c *call) error {
		p, e := s.local(ctx)
		if e != nil {
			return e
		}
		views := make([]DestinationView, 0, len(p.Destinations))
		for a, d := range p.Destinations {
			views = append(views, DestinationView{Alias: a, Kind: a.Kind(), DisplayName: d.DisplayName, Send: d.Send, Watch: d.Watch})
		}
		sort.Slice(views, func(i, j int) bool { return views[i].Alias < views[j].Alias })
		res = DestinationsResult{Destinations: views}
		c.set("count", strconv.Itoa(len(views)))
		return nil
	})
	return res, err
}
