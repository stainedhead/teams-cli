package domain

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// FilterLinkAllowlist names the link allowlist in findings.
const FilterLinkAllowlist = "link_allowlist"

type secretPattern struct {
	id string
	re *regexp.Regexp
}

var secretPatterns = []secretPattern{
	{"private_key", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
	{"aws_access_key", regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`)},
	{"github_token", regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{36,}|\bgithub_pat_[A-Za-z0-9_]{22,}`)},
	{"slack_token", regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}`)},
	{"jwt", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`)},
	{"bearer", regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/-]{20,}`)},
	{"generic_secret", regexp.MustCompile(`(?i)\b(?:password|passwd|secret|token|api[_-]?key)\s*[:=]\s*\S{8,}`)},
}

// ScanSecrets scans text for built-in secret patterns (FR-9). Findings carry
// the pattern id only, never matched text; each pattern reports once.
func ScanSecrets(text string) []Finding {
	var out []Finding
	for _, p := range secretPatterns {
		if p.re.MatchString(text) {
			out = append(out, Finding{Filter: FilterSecretPatterns, PatternID: p.id})
		}
	}
	return out
}

// ScanMarkers finds classification markers (case-insensitive substring). The
// pattern id is the marker's position, so findings never echo marker or text.
func ScanMarkers(markers []string, text string) []Finding {
	lower := strings.ToLower(text)
	var out []Finding
	for i, m := range markers {
		m = strings.ToLower(strings.TrimSpace(m))
		if m != "" && strings.Contains(lower, m) {
			out = append(out, Finding{Filter: FilterClassificationMarkers, PatternID: "marker_" + strconv.Itoa(i)})
		}
	}
	return out
}

// CheckLinks verifies every http(s) URL host in text (including <a href>
// values) against the allowlist: exact host or "*.suffix" (subdomains only).
// An empty allowlist is unrestricted. Hosts are taken from the parsed URL, so
// userinfo tricks such as https://ok.com@evil.com resolve to evil.com.
func CheckLinks(allow []string, text string) []Finding {
	if len(allow) == 0 {
		return nil
	}
	var out []Finding
	seen := map[string]bool{}
	add := func(id string) {
		if !seen[id] {
			seen[id] = true
			out = append(out, Finding{Filter: FilterLinkAllowlist, PatternID: id})
		}
	}
	for _, raw := range extractURLs(text) {
		if strings.ContainsAny(raw, `\`) {
			add("link_unparseable")
			continue
		}
		u, err := url.Parse(raw)
		if err != nil || u.Hostname() == "" {
			add("link_unparseable")
			continue
		}
		if !hostAllowed(allow, strings.ToLower(u.Hostname())) {
			add("host_not_allowed")
		}
	}
	return out
}

func hostAllowed(allow []string, host string) bool {
	for _, a := range allow {
		a = strings.ToLower(strings.TrimSpace(a))
		if suffix, ok := strings.CutPrefix(a, "*."); ok {
			if suffix != "" && strings.HasSuffix(host, "."+suffix) {
				return true
			}
			continue
		}
		if a != "" && host == a {
			return true
		}
	}
	return false
}
