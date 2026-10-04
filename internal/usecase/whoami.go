package usecase

import "context"

// Whoami implements FR-1: one GET /me (cached per run) and the policy profile.
func (s *service) Whoami(ctx context.Context) (res WhoamiResult, err error) {
	err = s.exec(ctx, "whoami", "", func(c *call) error {
		p, me, e := s.begin(ctx, c)
		if e != nil {
			return e
		}
		poll := p.Inbound.PollInterval
		if poll <= 0 {
			poll = defaultPollInterval
		}
		res = WhoamiResult{
			Profile: me, Policy: p.Profile, PolicyPath: s.d.Run.PolicyPath, PolicyVersion: p.Version,
			Destinations: destinationViews(p), PollInterval: poll,
			Limits: LimitsView{
				MaxResults: maxResults(p), MaxWritesPerRun: p.Limits.MaxWritesPerRun, MaxBytes: p.Send.MaxBytes,
				RatePerMinute: p.Send.Rate.PerMinute, RatePerHour: p.Send.Rate.PerHour, ReplyDepthMax: p.Send.ReplyDepthMax,
			},
		}
		return nil
	})
	return res, err
}
