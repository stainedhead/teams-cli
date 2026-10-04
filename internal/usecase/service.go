package usecase

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/stainedhead/agent-cli-core/output"

	"github.com/stainedhead/teams-cli/internal/domain"
)

// Audit outcomes (AuditEvent.Outcome).
const (
	outcomeOK           = "ok"
	outcomeDenied       = "denied"
	outcomeError        = "error"
	outcomeDryRun       = "dry_run"
	outcomeDeduplicated = "deduplicated"
)

// Built-in defaults, applied only when a policy value is unset (the policy
// loader normally fills them in).
const (
	defaultMaxResults   = 50
	defaultListLimit    = 20
	defaultReplyWindow  = 24 * time.Hour
	defaultMaxWait      = 120 * time.Second
	defaultPollInterval = 15 * time.Second
	defaultLookback     = 30 * time.Minute
	maxLookbackCap      = 24 * time.Hour
	pollFloor           = 5 * time.Second
	pollJitter          = 0.2
	deliveryKeepExtra   = time.Hour
	maxAckIDs           = 100
)

// Deps are the ports a service needs.
type Deps struct {
	Policy  PolicyProvider
	Graph   Graph
	Ledger  Ledger
	Cursors CursorStore
	Audit   AuditSink
	Clock   Clock
	Rand    Rand
	Run     RunInfo
}

// service implements Commands. One instance serves one process run: it caches
// the policy, the identity check and the per-run write counter.
type service struct {
	d Deps

	mu      sync.Mutex
	policy  *domain.Policy
	me      domain.Profile
	guarded bool
	guard   *domain.Decision
	writes  int
}

// New builds the use-case facade. A missing required port does not panic:
// every command then fails with a general error naming the port.
func New(d Deps) Commands { return &service{d: d} }

// call collects what one command wants written to its single audit event.
type call struct {
	resource string
	outcome  string
	decision string
	extra    map[string]string
}

func (c *call) set(k, v string) {
	if c.extra == nil {
		c.extra = map[string]string{}
	}
	c.extra[k] = v
}

// denyErr converts a denied Decision into a policy error and records the
// decision (rule id only) for the audit event. The error never carries text
// from the message.
func denyErr(c *call, d domain.Decision) error {
	c.decision = "deny:" + d.RuleID
	cat := d.Category
	if cat == "" || cat == output.CategoryOK {
		cat = output.CategoryPolicyDenied
	}
	hint := ""
	if d.RetryAfter > 0 {
		hint = "retry after " + d.RetryAfter.Round(time.Second).String()
	}
	return &domain.Error{Cat: cat, Msg: d.Reason, Hnt: hint}
}

// denyAs records a rule id for an error produced by a domain function.
func denyAs(c *call, rule string, err error) error {
	if err != nil {
		c.decision = "deny:" + rule
	}
	return err
}

// exec runs fn and records exactly one audit event for it, failures included.
// The event is recorded even if ctx was cancelled.
func (s *service) exec(ctx context.Context, verb, resource string, fn func(c *call) error) error {
	if err := s.checkDeps(); err != nil {
		return err
	}
	start := s.d.Clock.Now()
	c := &call{resource: resource}
	err := fn(c)
	ev := domain.AuditEvent{
		Verb:     verb,
		Resource: c.resource,
		Duration: s.d.Clock.Now().Sub(start),
		Decision: "allow",
		Extra:    c.extra,
	}
	denied := output.CategoryOf(err) == output.CategoryPolicyDenied && err != nil
	switch {
	case c.decision != "":
		ev.Decision = c.decision
	case denied:
		ev.Decision = "deny:policy"
	}
	switch {
	case err != nil && denied:
		ev.Outcome = outcomeDenied
	case err != nil:
		ev.Outcome = outcomeError
	case c.outcome != "":
		ev.Outcome = c.outcome
	default:
		ev.Outcome = outcomeOK
	}
	aerr := s.d.Audit.Record(context.WithoutCancel(ctx), ev)
	if err != nil {
		return err
	}
	if aerr != nil {
		return fmt.Errorf("audit write failed: %w", aerr)
	}
	return nil
}

func (s *service) checkDeps() error {
	missing := ""
	switch {
	case s.d.Policy == nil:
		missing = "Policy"
	case s.d.Graph == nil:
		missing = "Graph"
	case s.d.Ledger == nil:
		missing = "Ledger"
	case s.d.Cursors == nil:
		missing = "Cursors"
	case s.d.Audit == nil:
		missing = "Audit"
	case s.d.Clock == nil:
		missing = "Clock"
	case s.d.Rand == nil:
		missing = "Rand"
	}
	if missing != "" {
		return errors.New("internal error: use-case dependency not configured: " + missing)
	}
	return nil
}

// local loads the policy (once per run) without any network call.
func (s *service) local(ctx context.Context) (domain.Policy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked(ctx)
}

func (s *service) loadLocked(ctx context.Context) (domain.Policy, error) {
	if s.policy != nil {
		return *s.policy, nil
	}
	p, err := s.d.Policy.Policy(ctx)
	if err != nil {
		return domain.Policy{}, err
	}
	s.policy = &p
	return p, nil
}

// begin loads the policy and performs the identity guard (D14): GET /me must
// match policy upn. Both happen once per run; a mismatch is sticky and no
// further Graph call is made. Every network command calls it first.
func (s *service) begin(ctx context.Context, c *call) (domain.Policy, domain.Profile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.loadLocked(ctx)
	if err != nil {
		return domain.Policy{}, domain.Profile{}, err
	}
	if s.guarded {
		if s.guard != nil {
			return p, s.me, denyErr(c, *s.guard)
		}
		return p, s.me, nil
	}
	me, err := s.d.Graph.Me(ctx)
	if err != nil {
		return domain.Policy{}, domain.Profile{}, err
	}
	s.guarded, s.me = true, me
	if d := p.EvalUPN(me); !d.Allowed {
		s.guard = &d
		return p, me, denyErr(c, d)
	}
	return p, me, nil
}

func (s *service) runWrites() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writes
}

func (s *service) addWrite() {
	s.mu.Lock()
	s.writes++
	s.mu.Unlock()
}

// maxResults is the effective listing cap.
func maxResults(p domain.Policy) int {
	if p.Limits.MaxResults > 0 {
		return p.Limits.MaxResults
	}
	return defaultMaxResults
}

// clampLimit applies the --limit rules: unset means the default, never above
// the policy cap, negative is a usage error.
func clampLimit(p domain.Policy, n int) (int, error) {
	if n < 0 {
		return 0, domain.NewUsage("limit must not be negative", "")
	}
	if n == 0 {
		n = defaultListLimit
	}
	if m := maxResults(p); n > m {
		n = m
	}
	return n, nil
}

func lookback(p domain.Policy) time.Duration {
	l := p.Inbound.MaxLookback
	if l <= 0 {
		l = defaultLookback
	}
	if l > maxLookbackCap {
		l = maxLookbackCap
	}
	return l
}

// resolveChat returns the chat id of a chat or user destination. User chats
// come from the cursor-store cache, else from Graph (creating only when the
// destination allows it, D6); the result is cached.
func (s *service) resolveChat(ctx context.Context, d domain.Destination, fresh bool) (string, bool, error) {
	if d.Kind == domain.KindChat {
		return d.ChatID, false, nil
	}
	if !fresh {
		if id, ok := s.d.Cursors.ResolveChat(ctx, d.Alias); ok && id != "" {
			return id, true, nil
		}
	}
	id, err := s.d.Graph.ResolveUserChat(ctx, d.AADID, d.CreateChat)
	if err != nil {
		return "", false, err
	}
	_ = s.d.Cursors.CacheChat(ctx, d.Alias, id) // cache only; re-resolved on miss
	return id, false, nil
}

// withChat runs fn against the destination's chat id. A 404 on a cached user
// chat id drops the cache and re-resolves once (D6).
func (s *service) withChat(ctx context.Context, d domain.Destination, fn func(chatID string) error) error {
	id, cached, err := s.resolveChat(ctx, d, false)
	if err != nil {
		if output.CategoryOf(err) == output.CategoryNotFound {
			return &noChatError{cause: err}
		}
		return err
	}
	err = fn(id)
	if err == nil || !cached || output.CategoryOf(err) != output.CategoryNotFound {
		return err
	}
	_ = s.d.Cursors.DropChat(ctx, d.Alias)
	id, _, err = s.resolveChat(ctx, d, true)
	if err != nil {
		if output.CategoryOf(err) == output.CategoryNotFound {
			return &noChatError{cause: err}
		}
		return err
	}
	return fn(id)
}

// noChatError marks a user destination whose 1:1 chat does not exist and may
// not be created (D6). It keeps the not_found category of the cause.
type noChatError struct{ cause error }

func (e *noChatError) Error() string { return e.cause.Error() }
func (e *noChatError) Unwrap() error { return e.cause }

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func joinSkipped(m map[string]int) string {
	return strings.Join(sortedKeys(m), ",")
}

var zeroTime time.Time
