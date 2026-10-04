package usecase

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/stainedhead/agent-cli-core/output"

	"github.com/stainedhead/teams-cli/internal/domain"
)

// Selftest row statuses.
const (
	statusPass = "pass"
	statusFail = "fail"
	statusSkip = "skip"
)

// Selftest implements FR-24 over the ports. Policy-only rows never contact
// Graph; the others are skipped when the identity row fails.
func (s *service) Selftest(ctx context.Context, r SelftestRequest) (res SelftestResult, err error) {
	err = s.exec(ctx, "selftest", "", func(c *call) error {
		p, e := s.local(ctx)
		if e != nil {
			return e
		}
		res = s.selftest(ctx, p, r)
		failed := 0
		for _, row := range res.Rows {
			if row.Status == statusFail {
				failed++
			}
		}
		if failed > 0 {
			c.set("failed", strconv.Itoa(failed))
		}
		return nil
	})
	return res, err
}

func (s *service) selftest(ctx context.Context, p domain.Policy, r SelftestRequest) SelftestResult {
	ic := &call{}
	_, _, idErr := s.begin(ctx, ic)
	graphOK := idErr == nil
	needGraph := func(name string, fn func() SelftestRow) SelftestRow {
		if !graphOK {
			return SelftestRow{Name: name, Status: statusSkip, Detail: "skipped: identity check failed"}
		}
		return fn()
	}
	rows := []SelftestRow{
		identityRow(idErr),
		needGraph("send-allowed", func() SelftestRow { return s.rowSendAllowed(ctx, p, r.ReadOnly) }),
		rowDeny("send-unlisted", p.CanSend("chat:__selftest_unlisted__").Err()),
		s.rowBroadcast(p),
		rowDeny("oversize", domain.ValidateText(p, strings.Repeat("a", p.Send.MaxBytes+1))),
		s.rowSecret(p),
		s.rowClassification(p),
		s.rowMentionUnlisted(p),
		needGraph("user-chat-resolve", func() SelftestRow { return s.rowUserChat(ctx, p) }),
		needGraph("negative-membership", func() SelftestRow { return s.rowNegative(ctx, p) }),
		needGraph("inbox-read", func() SelftestRow { return s.rowInbox(ctx, p) }),
	}
	return SelftestResult{Rows: rows}
}

func identityRow(err error) SelftestRow {
	if err != nil {
		return SelftestRow{Name: "identity", Status: statusFail, Detail: err.Error()}
	}
	return SelftestRow{Name: "identity", Status: statusPass}
}

// rowDeny passes when err is a denial or validation refusal.
func rowDeny(name string, err error) SelftestRow {
	if err == nil {
		return SelftestRow{Name: name, Status: statusFail, Detail: "expected a refusal but the request was allowed"}
	}
	return SelftestRow{Name: name, Status: statusPass, Detail: "refused as expected"}
}

func (s *service) rowSendAllowed(ctx context.Context, p domain.Policy, readOnly bool) SelftestRow {
	const name = "send-allowed"
	if readOnly {
		return SelftestRow{Name: name, Status: statusSkip, Detail: "skipped: read-only"}
	}
	var first domain.Alias
	for _, a := range sortedAliases(p) {
		if p.Destinations[a].Send {
			first = a
			break
		}
	}
	if first == "" {
		return SelftestRow{Name: name, Status: statusSkip, Detail: "no destination with send: true"}
	}
	q := postReq{alias: first, text: "selftest " + s.d.Run.RunID}
	if first.Kind() != domain.KindChannel {
		q.threadID = domain.ThreadID(first, "chat")
	}
	if _, err := s.post(ctx, &call{}, q); err != nil {
		return SelftestRow{Name: name, Status: statusFail, Detail: err.Error()}
	}
	return SelftestRow{Name: name, Status: statusPass}
}

func (s *service) rowBroadcast(p domain.Policy) SelftestRow {
	_, err := domain.BuildMentions(p, []domain.Alias{"channel:__selftest_everyone__"}, "selftest")
	return rowDeny("broadcast-mention", err)
}

func (s *service) rowMentionUnlisted(p domain.Policy) SelftestRow {
	_, err := domain.BuildMentions(p, []domain.Alias{"user:__selftest_unlisted__"}, "selftest")
	return rowDeny("mention-unlisted", err)
}

func (s *service) rowSecret(p domain.Policy) SelftestRow {
	// Canned fake AWS key (the documented example value).
	err := s.checkContent(&call{}, p, "selftest AKIAIOSFODNN7EXAMPLE")
	if err == nil {
		return SelftestRow{Name: "secret-pattern", Status: statusFail, Detail: "secret text was not refused; enable content_filters: secret_patterns"}
	}
	return SelftestRow{Name: "secret-pattern", Status: statusPass, Detail: "refused as expected"}
}

func (s *service) rowClassification(p domain.Policy) SelftestRow {
	const name = "classification"
	if len(p.Send.ClassificationMarkers) == 0 {
		return SelftestRow{Name: name, Status: statusSkip, Detail: "warning: no classification_markers configured; the filter is inactive"}
	}
	err := s.checkContent(&call{}, p, "selftest "+p.Send.ClassificationMarkers[0])
	if err == nil {
		return SelftestRow{Name: name, Status: statusFail, Detail: "marker text was not refused; enable content_filters: classification_markers"}
	}
	return SelftestRow{Name: name, Status: statusPass, Detail: "refused as expected"}
}

func (s *service) rowUserChat(ctx context.Context, p domain.Policy) SelftestRow {
	const name = "user-chat-resolve"
	for _, a := range sortedAliases(p) {
		if d := p.Destinations[a]; d.Kind == domain.KindUser {
			if _, _, err := s.resolveChat(ctx, d, true); err != nil {
				return SelftestRow{Name: name, Status: statusFail, Detail: err.Error()}
			}
			return SelftestRow{Name: name, Status: statusPass}
		}
	}
	return SelftestRow{Name: name, Status: statusSkip, Detail: "no user: destination"}
}

func (s *service) rowNegative(ctx context.Context, p domain.Policy) SelftestRow {
	const name = "negative-membership"
	id := p.Selftest.NonMemberChatID
	if id == "" {
		return SelftestRow{Name: name, Status: statusSkip, Detail: "selftest.non_member_chat_id not set"}
	}
	err := s.d.Graph.GetChat(ctx, id)
	switch output.CategoryOf(err) {
	case output.CategoryForbidden, output.CategoryNotFound:
		return SelftestRow{Name: name, Status: statusPass, Detail: "access refused as expected"}
	}
	if err == nil {
		return SelftestRow{Name: name, Status: statusFail, Detail: "the agent can read a chat it should not be a member of"}
	}
	return SelftestRow{Name: name, Status: statusFail, Detail: "unexpected result: " + err.Error()}
}

func (s *service) rowInbox(ctx context.Context, p domain.Policy) SelftestRow {
	const name = "inbox-read"
	w := p.Watched()
	if len(w) == 0 {
		return SelftestRow{Name: name, Status: statusSkip, Detail: "no watch: true destination"}
	}
	d := w[0]
	cs, err := s.d.Cursors.Get(ctx, d.Alias)
	if err == nil {
		now := s.d.Clock.Now()
		_, err = s.fetch(ctx, p, d, cs, domain.Since(cs, now, lookback(p), nil))
	}
	if err != nil {
		return SelftestRow{Name: name, Status: statusFail, Detail: err.Error()}
	}
	return SelftestRow{Name: name, Status: statusPass}
}

func sortedAliases(p domain.Policy) []domain.Alias {
	out := make([]domain.Alias, 0, len(p.Destinations))
	for a := range p.Destinations {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
