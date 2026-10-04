package domain

import (
	"strings"
	"testing"
)

func TestScanSecretsPositives(t *testing.T) {
	pos := map[string]string{
		"-----BEGIN RSA PRIVATE KEY-----\nabc":                             "private_key",
		"-----BEGIN PRIVATE KEY-----":                                      "private_key",
		"-----BEGIN OPENSSH PRIVATE KEY-----":                              "private_key",
		"key AKIAIOSFODNN7EXAMPLE end":                                     "aws_access_key",
		"ASIAIOSFODNN7EXAMPLE":                                             "aws_access_key",
		"ghp_" + strings.Repeat("a1B2", 9):                                 "github_token",
		"gho_" + strings.Repeat("Z", 36):                                   "github_token",
		"github_pat_" + strings.Repeat("a", 30):                            "github_token",
		"xoxb-123456789012-abcdefghij":                                     "slack_token",
		"xoxp-1234567890-abcdefghijkl":                                     "slack_token",
		"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkw.SflKxwRJSMeKKF2QT4": "jwt",
		"Authorization: Bearer abcdefghijklmnopqrstuvwxyz":                 "bearer",
		"password: hunter2hunter2":                                         "generic_secret",
		"API_KEY=sk_live_abcdefgh":                                         "generic_secret",
		"my token = abcdefgh12345":                                         "generic_secret",
		"Secret:   zzzzzzzzzz":                                             "generic_secret",
	}
	for text, id := range pos {
		fs := ScanSecrets(text)
		found := false
		for _, f := range fs {
			if f.Filter != FilterSecretPatterns {
				t.Errorf("filter %q", f.Filter)
			}
			if f.PatternID == id {
				found = true
			}
			if strings.Contains(text, f.PatternID) && len(f.PatternID) > 12 {
				t.Errorf("suspicious pattern id %q", f.PatternID)
			}
		}
		if !found {
			t.Errorf("%q: want %s got %+v", text, id, fs)
		}
	}
	if len(pos) < 12 {
		t.Fatal("corpus too small")
	}
}

func TestScanSecretsNegatives(t *testing.T) {
	neg := []string{
		"", "hello world", "the token budget for this sprint", "password reset instructions attached",
		"secret santa on Friday", "use a bearer token in the header", "AKIA is a prefix", "ghp_short",
		"xoxb-short", "eyJ is how JSON base64 starts", "api key rotation policy", "token: short",
		"BEGIN PRIVATE KEY is a PEM marker without dashes", "deploy finished at 12:00", "https://example.com/path?x=1",
		"commit abcdef0123456789abcdef0123456789abcdef01",
	}
	for _, s := range neg {
		if fs := ScanSecrets(s); len(fs) != 0 {
			t.Errorf("%q flagged: %+v", s, fs)
		}
	}
	if len(neg) < 12 {
		t.Fatal("corpus too small")
	}
}

func TestFindingsNeverContainMatchedText(t *testing.T) {
	secret := "AKIAIOSFODNN7EXAMPLE"
	for _, f := range ScanSecrets("x " + secret) {
		if strings.Contains(f.Filter+f.PatternID, secret) {
			t.Fatal("leak")
		}
	}
	for _, f := range ScanMarkers([]string{"TOP SECRET"}, "this is top secret") {
		if strings.Contains(strings.ToLower(f.Filter+f.PatternID), "secret") && f.PatternID != "marker_0" {
			t.Fatal("leak")
		}
	}
	for _, f := range CheckLinks([]string{"ok.com"}, "https://evil.example/leak") {
		if strings.Contains(f.PatternID, "evil") {
			t.Fatal("leak")
		}
	}
}

func TestScanMarkers(t *testing.T) {
	m := []string{"CONFIDENTIAL", " internal only ", "", "Project Zed"}
	cases := []struct {
		text string
		want []string
	}{
		{"nothing here", nil},
		{"this is confidential", []string{"marker_0"}},
		{"Confidential and INTERNAL ONLY", []string{"marker_0", "marker_1"}},
		{"project ZED", []string{"marker_3"}},
	}
	for _, c := range cases {
		fs := ScanMarkers(m, c.text)
		if len(fs) != len(c.want) {
			t.Errorf("%q: %+v", c.text, fs)
			continue
		}
		for i, f := range fs {
			if f.Filter != FilterClassificationMarkers || f.PatternID != c.want[i] {
				t.Errorf("%q: %+v", c.text, fs)
			}
		}
	}
	if ScanMarkers(nil, "anything") != nil {
		t.Fatal("empty markers must be inactive")
	}
}

func TestCheckLinks(t *testing.T) {
	allow := []string{"example.com", "*.corp.example", "Docs.Foo.org"}
	cases := []struct {
		name, text string
		bad        bool
	}{
		{"no links", "plain text", false},
		{"exact", "see https://example.com/a?b=1", false},
		{"exact upper scheme and host", "see HTTPS://EXAMPLE.COM/a", false},
		{"upper scheme bad host", "HTTPS://EVIL.COM/a", true},
		{"exact host upper", "see https://EXAMPLE.com/a", false},
		{"suffix sub", "https://wiki.corp.example/x", false},
		{"suffix deep", "https://a.b.corp.example/x", false},
		{"suffix apex not matched", "https://corp.example/x", true},
		{"suffix lookalike", "https://evilcorp.example/x", true},
		{"other host", "https://evil.example/x", true},
		{"subdomain of exact not allowed", "https://sub.example.com/x", true},
		{"userinfo trick", "https://example.com@evil.com/x", true},
		{"userinfo trick 2", "https://example.com:pw@evil.com/x", true},
		{"allowed with userinfo", "https://user@example.com/x", false},
		{"port", "https://example.com:8443/x", false},
		{"backslash trick", `https://example.com\@evil.com/x`, true},
		{"href double quotes", `<a href="https://evil.com/x">click</a>`, true},
		{"href allowed", `<a href="https://docs.foo.org/x">click</a>`, false},
		{"http scheme", "http://evil.com", true},
		{"mixed one bad", "https://example.com/ok and https://evil.com/no", true},
		{"trailing punctuation", "visit https://example.com.", false},
		{"no host", "https:///path", true},
	}
	for _, c := range cases {
		fs := CheckLinks(allow, c.text)
		if (len(fs) > 0) != c.bad {
			t.Errorf("%s: %+v", c.name, fs)
		}
		for _, f := range fs {
			if f.Filter != FilterLinkAllowlist {
				t.Errorf("%s: filter %q", c.name, f.Filter)
			}
		}
	}
	if CheckLinks(nil, "https://anything.example") != nil {
		t.Fatal("empty allowlist must be unrestricted")
	}
	// findings are deduplicated
	if fs := CheckLinks(allow, "https://a.evil.com https://b.evil.com"); len(fs) != 1 {
		t.Errorf("not deduped: %+v", fs)
	}
	if hostAllowed([]string{"*."}, "x.") || hostAllowed([]string{""}, "") {
		t.Error("degenerate allow entries matched")
	}
}
