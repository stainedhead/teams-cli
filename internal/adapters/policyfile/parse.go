package policyfile

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/goccy/go-yaml"

	"github.com/stainedhead/teams-cli/internal/domain"
	"github.com/stainedhead/teams-cli/internal/infra/config"
)

// Defaults applied when the file omits a bound (data-dictionary "Policy file
// fields"). Omitting a value never widens access beyond these.
const (
	DefaultMaxLookback   = 30 * time.Minute
	MaxLookbackCap       = 24 * time.Hour
	DefaultPollInterval  = 15 * time.Second
	MinPollInterval      = 5 * time.Second
	DefaultMaxWait       = 120 * time.Second
	DefaultThreadPollMax = 5
	DefaultMaxBytes      = 8000
	DefaultMentionsMax   = 5
	DefaultRatePerMinute = 10
	DefaultRatePerHour   = 100
	DefaultReplyDepthMax = 6
	DefaultReplyWindow   = 24 * time.Hour
	DefaultMaxResults    = 50
	DefaultMaxWrites     = 30
	DefaultMaxChatScan   = 50
)

// maxPolicyBytes bounds the policy file read.
const maxPolicyBytes = 1 << 20

const hint = "fix the policy file (see the sample policy in the user docs); a policy that cannot be loaded blocks every command"

// The alias grammar is repeated here (not taken from domain.ParseAlias) so the
// loader's validation is self-contained and independent of domain evolution.
var (
	aliasRe = regexp.MustCompile(`^(channel|chat|user):[a-z0-9._-]{1,64}$`)
	guidRe  = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	upnRe   = regexp.MustCompile(`^[^@\s<>,;"]+@[^@\s<>,;"]+\.[^@\s<>,;"]+$`)
)

func invalid(format string, a ...any) error {
	return domain.NewValidation(fmt.Sprintf("policy invalid: "+format, a...), hint)
}

// Parse validates policy YAML and returns the typed policy. It never touches
// the file system. Error text names keys and problems, never values from the
// file.
func Parse(data []byte) (domain.Policy, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return domain.Policy{}, invalid("file is empty")
	}
	var doc fileDoc
	dec := yaml.NewDecoder(bytes.NewReader(data), yaml.Strict())
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return domain.Policy{}, invalid("file is empty")
		}
		return domain.Policy{}, invalid("%s", firstLine(err.Error()))
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return domain.Policy{}, invalid("exactly one YAML document is allowed")
	}
	return convert(doc)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if strings.Contains(s, "already defined") {
		s = "duplicate key: " + s
	}
	return s
}

type problems []string

func (p *problems) add(format string, a ...any) { *p = append(*p, fmt.Sprintf(format, a...)) }

func convert(d fileDoc) (domain.Policy, error) {
	var pr problems
	p := domain.Policy{
		Profile:  strings.TrimSpace(d.Profile),
		UPN:      strings.TrimSpace(d.UPN),
		TenantID: strings.ToLower(strings.TrimSpace(d.TenantID)),
		StateDir: strings.TrimSpace(d.StateDir),
		Audit:    domain.AuditCfg{Path: strings.TrimSpace(d.Audit.Path)},
		Selftest: domain.SelftestCfg{NonMemberChatID: strings.TrimSpace(d.Selftest.NonMemberChatID)},
	}
	switch {
	case d.Version == nil:
		pr.add("version is required")
	case *d.Version != 1:
		pr.add("version must be 1")
	}
	p.Version = 1
	if p.Profile == "" {
		pr.add("profile is required")
	}
	if p.UPN == "" {
		pr.add("upn is required")
	} else if !upnRe.MatchString(p.UPN) {
		pr.add("upn must look like name@domain")
	}
	if p.TenantID != "" && !guidRe.MatchString(p.TenantID) {
		pr.add("tenant_id must be a GUID")
	}
	switch {
	case p.StateDir == "":
		p.StateDir = config.DefaultStateDir
	case !filepath.IsAbs(p.StateDir):
		pr.add("state_dir must be an absolute path")
	default:
		p.StateDir = filepath.Clean(p.StateDir)
	}
	if p.Audit.Path == "" {
		pr.add("audit.path is required")
	} else if !filepath.IsAbs(p.Audit.Path) {
		pr.add("audit.path must be an absolute path")
	}
	if strings.ContainsAny(p.Selftest.NonMemberChatID, " \t\r\n\x00") {
		pr.add("selftest.non_member_chat_id must not contain whitespace")
	}

	p.Destinations = convertDestinations(&pr, d.Destinations)
	p.Instruct = domain.Instruct{
		Commanders: guids(&pr, "instruct.commanders.aad_ids", d.Instruct.Commanders.AADIDs),
		Agents:     guids(&pr, "instruct.agents.aad_ids", d.Instruct.Agents.AADIDs),
	}
	p.Inbound = convertInbound(&pr, d.Inbound)
	p.Send = convertSend(&pr, d.Send, p.Destinations)
	p.Limits = convertLimits(&pr, d.Limits)

	if len(pr) > 0 {
		return domain.Policy{}, invalid("%s", strings.Join(pr, "; "))
	}
	return p, nil
}

func hasCtl(s string) bool { return strings.ContainsAny(s, "\x00\r\n\t") }

func convertDestinations(pr *problems, in map[string]destDoc) map[domain.Alias]domain.Destination {
	if len(in) == 0 {
		pr.add("destinations must list at least one destination")
		return nil
	}
	out := make(map[domain.Alias]domain.Destination, len(in))
	for key, dd := range in {
		if !aliasRe.MatchString(key) {
			pr.add("destinations has an invalid alias %q (want channel:|chat:|user: plus [a-z0-9._-]{1,64})", key)
			continue
		}
		a := domain.Alias(key)
		kind := domain.Kind(key[:strings.IndexByte(key, ':')])
		d := domain.Destination{
			Alias: a, Kind: kind,
			TeamID: strings.TrimSpace(dd.TeamID), ChannelID: strings.TrimSpace(dd.ChannelID),
			ChatID: strings.TrimSpace(dd.ChatID), AADID: strings.ToLower(strings.TrimSpace(dd.AADID)),
			DisplayName: strings.TrimSpace(dd.DisplayName),
			Send:        dd.Send, Watch: dd.Watch, CreateChat: dd.CreateChat,
		}
		checkDestination(pr, key, d)
		out[a] = d
	}
	return out
}

// checkDestination enforces "exactly the id fields for its kind" (FR-22).
func checkDestination(pr *problems, key string, d domain.Destination) {
	has := func(field, v string) bool {
		if hasCtl(v) {
			pr.add("%s.%s must not contain control characters", key, field)
		}
		return v != ""
	}
	team, channel, chat, aad := has("team_id", d.TeamID), has("channel_id", d.ChannelID), has("chat_id", d.ChatID), has("aad_id", d.AADID)
	need := func(field string, ok bool) {
		if !ok {
			pr.add("%s requires %s", key, field)
		}
	}
	forbid := func(field string, present bool) {
		if present {
			pr.add("%s must not set %s", key, field)
		}
	}
	switch d.Kind {
	case domain.KindChannel:
		need("team_id", team)
		need("channel_id", channel)
		forbid("chat_id", chat)
		forbid("aad_id", aad)
		forbid("create_chat", d.CreateChat)
	case domain.KindChat:
		need("chat_id", chat)
		forbid("team_id", team)
		forbid("channel_id", channel)
		forbid("aad_id", aad)
		forbid("create_chat", d.CreateChat)
	case domain.KindUser:
		need("aad_id", aad)
		if aad && !guidRe.MatchString(d.AADID) {
			pr.add("%s.aad_id must be a GUID", key)
		}
		forbid("team_id", team)
		forbid("channel_id", channel)
		forbid("chat_id", chat)
	}
	if hasCtl(d.DisplayName) || len(d.DisplayName) > 256 {
		pr.add("%s.display_name must be at most 256 characters without control characters", key)
	}
}

func guids(pr *problems, key string, in []string) []domain.AADID {
	var out []domain.AADID
	seen := map[string]bool{}
	for _, s := range in {
		s = strings.ToLower(strings.TrimSpace(s))
		switch {
		case !guidRe.MatchString(s):
			pr.add("%s entries must be GUIDs", key)
		case seen[s]:
			pr.add("%s has a duplicate entry", key)
		default:
			out = append(out, domain.AADID(s))
		}
		seen[s] = true
	}
	return out
}

func convertInbound(pr *problems, d inboundDoc) domain.Inbound {
	in := domain.Inbound{
		MaxLookback: d.MaxLookback, PollInterval: d.PollInterval, MaxWait: d.MaxWait, ThreadPollMax: d.ThreadPollMax,
	}
	if len(d.Handle) == 0 {
		in.Handle = []domain.InboundHandle{domain.HandleDirect, domain.HandleMentions, domain.HandleWatched}
	}
	seen := map[string]bool{}
	for _, h := range d.Handle {
		h = strings.TrimSpace(h)
		switch domain.InboundHandle(h) {
		case domain.HandleDirect, domain.HandleMentions, domain.HandleWatched:
			if seen[h] {
				pr.add("inbound.handle has duplicate %q", h)
				continue
			}
			seen[h] = true
			in.Handle = append(in.Handle, domain.InboundHandle(h))
		default:
			pr.add("inbound.handle has unknown value %q (known: direct, mentions, watched)", h)
		}
	}
	in.MaxLookback = dflt(pr, "inbound.max_lookback", d.MaxLookback, DefaultMaxLookback)
	if in.MaxLookback > MaxLookbackCap {
		pr.add("inbound.max_lookback must be at most %s", MaxLookbackCap)
	}
	in.PollInterval = dflt(pr, "inbound.poll_interval", d.PollInterval, DefaultPollInterval)
	if in.PollInterval < MinPollInterval {
		pr.add("inbound.poll_interval must be at least %s", MinPollInterval)
	}
	in.MaxWait = dflt(pr, "inbound.max_wait", d.MaxWait, DefaultMaxWait)
	in.ThreadPollMax = dfltInt(pr, "inbound.thread_poll_max", d.ThreadPollMax, DefaultThreadPollMax)
	return in
}

// dflt returns def for zero and flags negatives.
func dflt(pr *problems, key string, v, def time.Duration) time.Duration {
	if v < 0 {
		pr.add("%s must not be negative", key)
		return def
	}
	if v == 0 {
		return def
	}
	return v
}

func dfltInt(pr *problems, key string, v, def int) int {
	if v < 0 {
		pr.add("%s must not be negative", key)
		return def
	}
	if v == 0 {
		return def
	}
	return v
}

// KnownFilters lists the content filter names the policy accepts.
func KnownFilters() []string {
	return []string{domain.FilterSecretPatterns, domain.FilterClassificationMarkers}
}

func convertSend(pr *problems, d sendDoc, dests map[domain.Alias]domain.Destination) domain.SendPolicy {
	s := domain.SendPolicy{
		MaxBytes: dfltInt(pr, "send.max_bytes", d.MaxBytes, DefaultMaxBytes),
		Prefix:   d.Prefix, MarkerScan: d.MarkerScan,
		ReplyDepthMax: dfltInt(pr, "send.reply_depth_max", d.ReplyDepthMax, DefaultReplyDepthMax),
		ReplyWindow:   dflt(pr, "send.reply_window", d.ReplyWindow, DefaultReplyWindow),
		Rate: domain.Rate{
			PerMinute: dfltInt(pr, "send.rate.per_minute", d.Rate.PerMinute, DefaultRatePerMinute),
			PerHour:   dfltInt(pr, "send.rate.per_hour", d.Rate.PerHour, DefaultRatePerHour),
		},
	}
	if strings.ContainsAny(s.Prefix, "\x00\r\n") {
		pr.add("send.prefix must not contain control characters")
	}
	// block_broadcast: accepted for schema compatibility, but false is rejected.
	s.Mentions.BlockBroadcast = true
	if b := d.Mentions.BlockBroadcast; b != nil && !*b {
		pr.add("send.mentions.block_broadcast must be true (broadcast mentions are not supported in v1)")
	}
	s.Mentions.Max = dfltInt(pr, "send.mentions.max", d.Mentions.Max, DefaultMentionsMax)
	seen := map[string]bool{}
	for _, m := range d.Mentions.Allow {
		m = strings.TrimSpace(m)
		dest, ok := dests[domain.Alias(m)]
		switch {
		case !aliasRe.MatchString(m) || !strings.HasPrefix(m, "user:"):
			pr.add("send.mentions.allow entries must be user: aliases")
		case seen[m]:
			pr.add("send.mentions.allow has duplicate %q", m)
		case !ok:
			pr.add("send.mentions.allow entry %q is not a destination", m)
		case dest.DisplayName == "":
			pr.add("send.mentions.allow entry %q needs display_name on its destination", m)
		default:
			s.Mentions.Allow = append(s.Mentions.Allow, domain.Alias(m))
		}
		seen[m] = true
	}
	s.ContentFilters = filters(pr, d.ContentFilters)
	for _, m := range d.ClassificationMarkers {
		m = strings.TrimSpace(m)
		if m == "" || hasCtl(m) {
			pr.add("send.classification_markers entries must be non-empty text without control characters")
			continue
		}
		s.ClassificationMarkers = append(s.ClassificationMarkers, m)
	}
	for _, l := range d.LinkAllowlist {
		l = strings.ToLower(strings.TrimSpace(l))
		if !validLinkEntry(l) {
			pr.add("send.link_allowlist entries must be bare hosts such as example.com or *.example.com")
			continue
		}
		s.LinkAllowlist = append(s.LinkAllowlist, l)
	}
	return s
}

func filters(pr *problems, in []string) []string {
	known := map[string]bool{}
	for _, k := range KnownFilters() {
		known[k] = true
	}
	var out []string
	seen := map[string]bool{}
	for _, f := range in {
		f = strings.TrimSpace(f)
		switch {
		case !known[f]:
			pr.add("send.content_filters has unknown filter %q (known: %s)", f, strings.Join(KnownFilters(), ", "))
		case seen[f]:
			pr.add("send.content_filters has duplicate filter %q", f)
		default:
			out = append(out, f)
		}
		seen[f] = true
	}
	return out
}

var hostRe = regexp.MustCompile(`^(\*\.)?[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$`)

func validLinkEntry(l string) bool {
	return l != "" && len(l) <= 253 && hostRe.MatchString(l)
}

func convertLimits(pr *problems, d limitsDoc) domain.Limits {
	l := domain.Limits{
		MaxResults:  dfltInt(pr, "limits.max_results", d.MaxResults, DefaultMaxResults),
		MaxChatScan: dfltInt(pr, "limits.max_chat_scan", d.MaxChatScan, DefaultMaxChatScan),
	}
	l.MaxWritesPerRun = DefaultMaxWrites
	if d.MaxWritesPerRun != nil {
		if *d.MaxWritesPerRun < 0 {
			pr.add("limits.max_writes_per_run must not be negative")
		} else {
			l.MaxWritesPerRun = *d.MaxWritesPerRun
		}
	}
	return l
}
