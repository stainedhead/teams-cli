package policyfile

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"

	"github.com/stainedhead/teams-cli/internal/domain"
	"github.com/stainedhead/teams-cli/internal/infra/fstrust"
)

const installHint = "install the policy as root, for example: " +
	"sudo install -d -o root -m 0755 /etc/agent-cli && " +
	"sudo install -o root -m 0644 teams.policy.yaml /etc/agent-cli/teams.policy.yaml"

// Option configures Load and NewProvider.
type Option func(*loadConfig)

type loadConfig struct {
	allowUntrusted bool
	trustedUIDs    []uint32
	checker        *fstrust.Checker
}

// AllowUntrusted disables the ownership check (FR-21). It exists for tests; a
// release build can enable it only through DevOptions, which is empty unless
// built with -tags teamsdev.
func AllowUntrusted() Option { return func(c *loadConfig) { c.allowUntrusted = true } }

// WithTrustedUIDs additionally trusts files owned by the given uids (root is
// always trusted). The effective uid is never trusted.
func WithTrustedUIDs(uids ...uint32) Option {
	return func(c *loadConfig) { c.trustedUIDs = append(c.trustedUIDs, uids...) }
}

// withChecker injects a trust checker (tests).
func withChecker(ch *fstrust.Checker) Option { return func(c *loadConfig) { c.checker = ch } }

// Load reads and parses the policy file at path. Unless AllowUntrusted is set
// it refuses (validation, exit 9, nothing runs) a policy whose file, symlinks
// or parent directories are not owned by root (or a configured trusted uid
// other than the effective uid) or are writable by group or others, because
// the agent must not be able to edit its own guardrails. The content is read
// from the descriptor that was verified.
func Load(path string, opts ...Option) (domain.Policy, error) {
	var cfg loadConfig
	for _, o := range opts {
		o(&cfg)
	}
	f, err := open(path, cfg)
	if err != nil {
		return domain.Policy{}, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxPolicyBytes+1))
	if err != nil {
		return domain.Policy{}, unreadable(path)
	}
	if len(data) > maxPolicyBytes {
		return domain.Policy{}, invalid("file exceeds %d bytes", maxPolicyBytes)
	}
	return Parse(data)
}

func unreadable(path string) error {
	return domain.NewValidation("policy: cannot read "+path, hint)
}

func open(path string, cfg loadConfig) (*os.File, error) {
	if cfg.allowUntrusted {
		f, err := os.Open(path)
		if err != nil {
			return nil, unreadable(path)
		}
		return f, nil
	}
	ch := cfg.checker
	if ch == nil {
		ch = fstrust.New(fstrust.WithTrustedUIDs(cfg.trustedUIDs...))
	}
	f, err := ch.OpenTrusted(path)
	if err == nil {
		return f, nil
	}
	var ue *fstrust.UntrustedError
	if errors.As(err, &ue) {
		return nil, domain.NewValidation("policy file is not trusted: "+ue.Error(), installHint)
	}
	return nil, unreadable(path)
}

// Provider implements usecase.PolicyProvider over a policy file. The first
// result, success or failure, is cached for the life of the process, so a file
// edited mid-run cannot change the rules of a run in progress.
type Provider struct {
	path string
	opts []Option
	once sync.Once
	pol  domain.Policy
	err  error
}

// NewProvider returns a Provider for path.
func NewProvider(path string, opts ...Option) *Provider {
	return &Provider{path: path, opts: opts}
}

// Policy returns the loaded policy. Callers get their own copy of every
// slice and map.
func (p *Provider) Policy(ctx context.Context) (domain.Policy, error) {
	if err := ctx.Err(); err != nil {
		return domain.Policy{}, err
	}
	p.once.Do(func() { p.pol, p.err = Load(p.path, p.opts...) })
	if p.err != nil {
		return domain.Policy{}, p.err
	}
	return clone(p.pol), nil
}

func clone(p domain.Policy) domain.Policy {
	dests := make(map[domain.Alias]domain.Destination, len(p.Destinations))
	for k, v := range p.Destinations {
		dests[k] = v
	}
	p.Destinations = dests
	p.Instruct.Commanders = append([]domain.AADID(nil), p.Instruct.Commanders...)
	p.Instruct.Agents = append([]domain.AADID(nil), p.Instruct.Agents...)
	p.Inbound.Handle = append([]domain.InboundHandle(nil), p.Inbound.Handle...)
	p.Send.Mentions.Allow = append([]domain.Alias(nil), p.Send.Mentions.Allow...)
	p.Send.ContentFilters = append([]string(nil), p.Send.ContentFilters...)
	p.Send.ClassificationMarkers = append([]string(nil), p.Send.ClassificationMarkers...)
	p.Send.LinkAllowlist = append([]string(nil), p.Send.LinkAllowlist...)
	return p
}
