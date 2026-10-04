package usecase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/stainedhead/agent-cli-core/output"

	"github.com/stainedhead/teams-cli/internal/domain"
)

// postReq is the shared input of send and reply.
type postReq struct {
	alias    domain.Alias
	root     string // Graph root message id for a channel reply, else ""
	threadID string // alias-qualified thread id used for the loop guard, may be ""
	text     string
	mentions []domain.Alias
	key      string
	dryRun   bool
}

// Send implements FR-4..FR-15.
func (s *service) Send(ctx context.Context, r SendRequest) (res SendResult, err error) {
	err = s.exec(ctx, "send", string(r.Alias), func(c *call) error {
		if _, e := domain.ParseAlias(string(r.Alias)); e != nil {
			return e
		}
		q := postReq{alias: r.Alias, text: r.Text, mentions: r.Mentions, key: r.IdempotencyKey, dryRun: r.DryRun}
		if r.Alias.Kind() != domain.KindChannel {
			q.threadID = domain.ThreadID(r.Alias, "chat")
		}
		var e error
		res, e = s.post(ctx, c, q)
		return e
	})
	return res, err
}

// Reply implements FR-7: the destination comes from the thread id alone.
func (s *service) Reply(ctx context.Context, r ReplyRequest) (res SendResult, err error) {
	alias, root, perr := domain.ParseItemID(r.ThreadID)
	err = s.exec(ctx, "reply", string(alias), func(c *call) error {
		if perr != nil {
			return perr
		}
		q := postReq{alias: alias, threadID: domain.ThreadID(alias, "chat"), text: r.Text, mentions: r.Mentions, key: r.IdempotencyKey, dryRun: r.DryRun}
		if alias.Kind() == domain.KindChannel {
			// Chats have no reply threads: whatever root was passed, the loop
			// guard and ledger key on the single chat thread.
			q.root = root
			q.threadID = domain.ThreadID(alias, root)
		}
		var e error
		res, e = s.post(ctx, c, q)
		return e
	})
	return res, err
}

// post runs the send checks in the FR-5 order; the first failure wins.
func (s *service) post(ctx context.Context, c *call, q postReq) (SendResult, error) {
	p, _, err := s.begin(ctx, c)
	if err != nil {
		return SendResult{}, err
	}
	if d := p.CanSend(q.alias); !d.Allowed {
		return SendResult{}, denyErr(c, d)
	}
	dest, _ := p.Destination(q.alias)
	if q.key != "" {
		if err := domain.ValidateKey(q.key); err != nil {
			return SendResult{}, err
		}
	}
	if err := domain.ValidateText(p, q.text); err != nil {
		return SendResult{}, err
	}
	out, err := domain.BuildMentions(p, q.mentions, q.text)
	if err != nil {
		return SendResult{}, denyAs(c, "mention", err)
	}
	if err := s.checkContent(c, p, q.text); err != nil {
		return SendResult{}, err
	}
	now := s.d.Clock.Now()
	if err := s.checkLimits(ctx, c, p, q.threadID, now); err != nil {
		return SendResult{}, err
	}
	if q.dryRun {
		c.outcome = outcomeDryRun
		return SendResult{DryRun: true, ThreadID: q.threadID}, nil
	}

	chatID := dest.ChatID
	if dest.Kind == domain.KindUser {
		if chatID, _, err = s.resolveChat(ctx, dest, false); err != nil {
			return SendResult{}, err
		}
	}
	if q.key != "" && p.Send.MarkerScan {
		out.MarkerKey = markerHash(q.key, q.alias)
	}
	if q.key != "" {
		res, done, err := s.reserve(ctx, c, p, dest, chatID, q, out, now)
		if err != nil || done {
			return res, err
		}
	}
	return s.deliver(ctx, c, dest, chatID, q, out)
}

// checkContent applies the content filters and link allowlist (FR-9, FR-12).
// The error names filter and pattern ids, never the matched text.
func (s *service) checkContent(c *call, p domain.Policy, text string) error {
	var findings []domain.Finding
	for _, f := range p.Send.ContentFilters {
		switch f {
		case domain.FilterSecretPatterns:
			findings = append(findings, domain.ScanSecrets(text)...)
		case domain.FilterClassificationMarkers:
			findings = append(findings, domain.ScanMarkers(p.Send.ClassificationMarkers, text)...)
		}
	}
	findings = append(findings, domain.CheckLinks(p.Send.LinkAllowlist, text)...)
	if len(findings) == 0 {
		return nil
	}
	ids := make([]string, len(findings))
	for i, f := range findings {
		ids[i] = f.Filter + "/" + f.PatternID
	}
	c.decision = "deny:filter." + findings[0].Filter
	c.set("findings", strings.Join(ids, ","))
	return domain.NewPolicyDenied("content filter hit: "+strings.Join(ids, ", "), "remove the flagged content; the matched text is not echoed")
}

// checkLimits applies rate, per-run write cap and loop guard (FR-10, FR-11).
func (s *service) checkLimits(ctx context.Context, c *call, p domain.Policy, threadID string, now time.Time) error {
	sent, err := s.d.Ledger.SentSince(ctx, now.Add(-time.Hour))
	if err != nil {
		return err
	}
	if d := domain.CheckRate(p.Send.Rate, sent, now, s.runWrites(), p.Limits.MaxWritesPerRun); !d.Allowed {
		return denyErr(c, d)
	}
	if threadID == "" {
		return nil
	}
	window := p.Send.ReplyWindow
	if window <= 0 {
		window = defaultReplyWindow
	}
	n, err := s.d.Ledger.SentInThread(ctx, threadID, now.Add(-window))
	if err != nil {
		return err
	}
	if d := domain.CheckLoop(p.Send.ReplyDepthMax, n); !d.Allowed {
		return denyErr(c, d)
	}
	return nil
}

// markerHash is the FR-15 marker: first 16 hex of SHA-256(key+alias).
func markerHash(key string, a domain.Alias) string {
	sum := sha256.Sum256([]byte(key + string(a)))
	return hex.EncodeToString(sum[:])[:16]
}

// reserve claims the idempotency key. done=true means the result is final
// (replay or marker-scan recovery) and nothing is posted.
func (s *service) reserve(ctx context.Context, c *call, p domain.Policy, dest domain.Destination, chatID string, q postReq, out domain.OutMessage, now time.Time) (SendResult, bool, error) {
	hash := domain.PayloadHash(q.alias, q.threadID, out)
	rsv, err := s.d.Ledger.Reserve(ctx, q.key, hash, q.alias, q.threadID, now)
	if err != nil {
		if p.Send.MarkerScan && output.CategoryOf(err) == output.CategoryConflict && !strings.Contains(err.Error(), "different payload") {
			if res, ok := s.recoverByMarker(ctx, c, dest, chatID, q, now); ok {
				return res, true, nil
			}
		}
		return SendResult{}, false, err
	}
	if rsv.Outcome != domain.ReserveReplay {
		return SendResult{}, false, nil
	}
	c.outcome = outcomeDeduplicated
	c.set("deduplicated", "true")
	c.set("message_id", rsv.Entry.MessageID)
	return SendResult{MessageID: rsv.Entry.MessageID, ThreadID: replayThread(q, rsv.Entry), Deduplicated: true}, true, nil
}

func replayThread(q postReq, e domain.LedgerEntry) string {
	switch {
	case e.ThreadID != "":
		return e.ThreadID
	case q.alias.Kind() == domain.KindChannel:
		return domain.ThreadID(q.alias, e.MessageID)
	}
	return domain.ThreadID(q.alias, "chat")
}

// recoverByMarker resolves a pending key by finding its marker in the
// destination (FR-15, UA-9: unverified against a real tenant).
func (s *service) recoverByMarker(ctx context.Context, c *call, dest domain.Destination, chatID string, q postReq, now time.Time) (SendResult, bool) {
	scan := dest
	scan.ChatID = chatID
	id, found, err := s.d.Graph.FindByMarker(ctx, scan, markerHash(q.key, q.alias))
	if err != nil || !found {
		return SendResult{}, false
	}
	if err := s.d.Ledger.Complete(ctx, q.key, id, now); err != nil {
		return SendResult{}, false
	}
	c.outcome = outcomeDeduplicated
	c.set("deduplicated", "true")
	c.set("message_id", id)
	return SendResult{MessageID: id, ThreadID: replayThread(q, domain.LedgerEntry{MessageID: id, ThreadID: q.threadID}), Deduplicated: true}, true
}

// deliver posts and updates the ledger. A failure that proves nothing was
// processed (domain.NotSent) marks the key failed; any other failure leaves
// it pending (ambiguous, FR-14).
func (s *service) deliver(ctx context.Context, c *call, dest domain.Destination, chatID string, q postReq, out domain.OutMessage) (SendResult, error) {
	s.addWrite()
	var res domain.PostResult
	var err error
	if dest.Kind == domain.KindChannel {
		res, err = s.d.Graph.PostChannel(ctx, dest.TeamID, dest.ChannelID, q.root, out)
	} else {
		res, err = s.d.Graph.PostChat(ctx, chatID, out)
	}
	if err != nil {
		notSent := domain.IsNotSent(err)
		if q.key != "" {
			_ = s.d.Ledger.Fail(ctx, q.key, notSent)
		}
		if notSent {
			return SendResult{}, err
		}
		return SendResult{}, &ambiguousError{cause: err, keyed: q.key != ""}
	}
	thread := q.threadID
	if dest.Kind == domain.KindChannel && thread == "" {
		root := res.ThreadID
		if root == "" {
			root = res.MessageID
		}
		thread = domain.ThreadID(q.alias, root)
	}
	now := s.d.Clock.Now()
	c.set("message_id", res.MessageID)
	if q.key != "" {
		if e := s.d.Ledger.Complete(ctx, q.key, res.MessageID, now); e != nil {
			c.set("warn", "ledger_update_failed")
		}
	} else if e := s.d.Ledger.RecordSent(ctx, domain.Sent{At: now, Alias: q.alias, ThreadID: thread, MessageID: res.MessageID}); e != nil {
		c.set("warn", "ledger_update_failed")
	}
	if dest.Kind == domain.KindChannel {
		_ = s.d.Ledger.PutThread(ctx, thread)
	}
	return SendResult{MessageID: res.MessageID, ThreadID: thread}, nil
}

// ambiguousError marks a write whose outcome is unknown. It keeps the cause's
// category (general for transport failures) and adds an actionable hint.
type ambiguousError struct {
	cause error
	keyed bool
}

func (e *ambiguousError) Error() string { return e.cause.Error() }
func (e *ambiguousError) Unwrap() error { return e.cause }
func (e *ambiguousError) Category() output.Category {
	return output.CategoryOf(e.cause)
}
func (e *ambiguousError) Hint() string {
	if e.keyed {
		return "outcome unknown: the message may have been delivered; check the destination before retrying (the same --idempotency-key will exit 7)"
	}
	return "outcome unknown: the message may have been delivered; check the destination before retrying"
}
