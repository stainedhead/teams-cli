// Package auditlog writes one JSONL audit record per command through the core
// audit package (FR-28). It adds the defences the core cannot know about:
// raw Graph ids, JWT-looking strings and free text are scrubbed from every
// field, extension fields are folded into policy_decision as ";key=value"
// suffixes, and a failed write blocks send and reply while only warning for
// reads. No token, message body or raw id is ever written.
package auditlog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/stainedhead/agent-cli-core/audit"

	"github.com/stainedhead/teams-cli/internal/domain"
	"github.com/stainedhead/teams-cli/internal/usecase"
)

// Tool is the tool name written to every record.
const Tool = "teams"

// Config configures a Sink.
type Config struct {
	// Path is the JSONL file (policy audit.path). Open only.
	Path string
	// AgentID and RunID identify the caller; they are written to every record.
	AgentID, RunID string
	// Clock stamps records; nil leaves stamping to the core (system clock).
	Clock usecase.Clock
	// BlockVerbs are the verbs whose audit write failure fails the command.
	// Nil means send and reply; reads only warn.
	BlockVerbs []string
	// OnWriteError receives write failures for non-blocking verbs.
	OnWriteError func(error)
	// Secrets are literal values to redact from every field. The process
	// should never hold one (tokens stay inside auth.Authorizer); this is a
	// second line of defence.
	Secrets []string
}

// Sink implements usecase.AuditSink.
type Sink struct {
	log   *audit.Logger
	cfg   Config
	block map[string]bool
}

var _ usecase.AuditSink = (*Sink)(nil)

// Open creates (0700 directory, 0600 file) or appends to cfg.Path.
func Open(cfg Config) (*Sink, error) {
	l, err := audit.Open(audit.Config{Path: cfg.Path}, options(cfg)...)
	if err != nil {
		return nil, fmt.Errorf("audit log cannot be opened (check audit.path in the policy): %w", err)
	}
	return newSink(l, cfg), nil
}

// NewWithWriter builds a Sink over any writer; used by tests and by callers
// that own the file. Close closes w if it is an io.Closer.
func NewWithWriter(w io.Writer, cfg Config) *Sink {
	return newSink(audit.NewLogger(w, options(cfg)...), cfg)
}

func options(cfg Config) []audit.Option {
	if len(cfg.Secrets) == 0 {
		return nil
	}
	return []audit.Option{audit.WithSecrets(cfg.Secrets...)}
}

func newSink(l *audit.Logger, cfg Config) *Sink {
	verbs := cfg.BlockVerbs
	if verbs == nil {
		verbs = []string{"send", "reply"}
	}
	b := make(map[string]bool, len(verbs))
	for _, v := range verbs {
		b[v] = true
	}
	return &Sink{log: l, cfg: cfg, block: b}
}

// Record writes one record. The context is not consulted: the action has
// already happened and must be recorded even if the command was cancelled.
// A write failure is returned for blocking verbs and reported through
// OnWriteError otherwise.
func (s *Sink) Record(_ context.Context, e domain.AuditEvent) error {
	rec := audit.Record{
		Tool:           Tool,
		AgentID:        token(s.cfg.AgentID),
		RunID:          token(s.cfg.RunID),
		Verb:           token(e.Verb),
		Resource:       token(e.Resource),
		Outcome:        token(e.Outcome),
		HTTPStatus:     e.HTTPStatus,
		Duration:       e.Duration,
		PolicyDecision: foldDecision(e),
	}
	if s.cfg.Clock != nil {
		rec.Timestamp = s.cfg.Clock.Now()
	}
	err := s.log.Log(rec)
	if err == nil {
		return nil
	}
	if s.block[e.Verb] {
		return err
	}
	if s.cfg.OnWriteError != nil {
		s.cfg.OnWriteError(err)
	}
	return nil
}

// Close closes the underlying file. Records after Close fail as write errors.
func (s *Sink) Close() error { return s.log.Close() }

// Scrubbing. Raw Graph ids, GUIDs, JWTs and bearer strings are replaced
// before any other rule, so even a value that passes the shape rules cannot
// carry them.
var (
	threadIDRe = regexp.MustCompile(`19:[A-Za-z0-9_.=@-]+(?:@thread\.[A-Za-z0-9]+|@unq\.gbl\.spaces)`)
	guidRe     = regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	jwtRe      = regexp.MustCompile(`eyJ[A-Za-z0-9_-]*(?:\.[A-Za-z0-9_-]*){0,2}`)
	bearerRe   = regexp.MustCompile(`(?i)bearer\s+\S+`)
	// longRunRe mirrors the core's opaque-run redaction.
	longRunRe = regexp.MustCompile(`[A-Za-z0-9_-]{40,}`)
	aliasRe   = regexp.MustCompile(`\b(?:channel|chat|user):[a-z0-9._-]+`)
	nameRunRe = regexp.MustCompile(`[A-Za-z0-9_-]{40,}`)
)

const redacted = "[redacted]"

// shortenAliases rewrites alias names whose runs reach the core's opaque-run
// threshold (40 characters) to "<first 24>~<sha256 prefix>". The core redacts
// such runs inside every field, which would turn a valid long alias into
// "channel:[redacted]"; the short form stays recognizable and correlatable.
func shortenAliases(s string) string {
	return aliasRe.ReplaceAllStringFunc(s, func(a string) string {
		return nameRunRe.ReplaceAllStringFunc(a, func(run string) string {
			sum := sha256.Sum256([]byte(run))
			return run[:24] + "~" + hex.EncodeToString(sum[:4])
		})
	})
}

func scrub(s string) string {
	s = shortenAliases(s)
	s = bearerRe.ReplaceAllString(s, redacted)
	s = jwtRe.ReplaceAllString(s, redacted)
	s = threadIDRe.ReplaceAllString(s, redacted)
	s = guidRe.ReplaceAllString(s, redacted)
	return longRunRe.ReplaceAllString(s, redacted)
}

var (
	tokenRe = regexp.MustCompile(`^[A-Za-z0-9._:/,~-]{0,128}$`)
	keyRe   = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
)

// token returns s if, after scrubbing, it is a short identifier-like token
// (an alias, a verb, a count); anything else, such as prose, becomes "invalid".
func token(s string) string {
	s = scrub(s)
	if s == redacted {
		return s
	}
	if !tokenRe.MatchString(s) {
		return "invalid"
	}
	return s
}

// reason keeps the human part of a deny decision: scrubbed, restricted to a
// plain character set, and capped.
func reason(s string) string {
	s = scrub(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', strings.ContainsRune("._:/-~ ,[]", r):
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	if len(out) > 96 {
		out = out[:96]
	}
	return out
}

// foldDecision appends the extension fields to the policy decision as
// ";key=value" pairs, sorted by key. audit.Record (core v0.1.0) has no
// extension fields; this keeps them in an existing field until the core grows
// them. Keys and values are restricted to short identifier-like tokens.
func foldDecision(e domain.AuditEvent) string {
	var b strings.Builder
	b.WriteString(reason(e.Decision))
	keys := make([]string, 0, len(e.Extra))
	for k := range e.Extra {
		if keyRe.MatchString(k) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := token(e.Extra[k])
		if len(v) > 64 {
			v = "invalid"
		}
		b.WriteString(";" + k + "=" + v)
	}
	return b.String()
}
