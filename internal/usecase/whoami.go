package usecase

import "context"

// Whoami implements FR-1: one GET /me (cached per run) and the policy profile.
func (s *service) Whoami(ctx context.Context) (res WhoamiResult, err error) {
	err = s.exec(ctx, "whoami", "", func(c *call) error {
		p, me, e := s.begin(ctx, c)
		if e != nil {
			return e
		}
		res = WhoamiResult{Profile: me, Policy: p.Profile}
		return nil
	})
	return res, err
}
