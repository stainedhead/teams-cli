package domain

import (
	"strings"
	"testing"
	"time"

	"github.com/stainedhead/agent-cli-core/output"
)

func sentAt(ts ...time.Time) []Sent {
	var s []Sent
	for _, t := range ts {
		s = append(s, Sent{At: t})
	}
	return s
}

func TestCheckRate(t *testing.T) {
	now := t0
	r := Rate{PerMinute: 3, PerHour: 5}
	sec := func(n int) time.Time { return now.Add(-time.Duration(n) * time.Second) }
	cases := []struct {
		name  string
		rate  Rate
		sent  []Sent
		run   int
		maxRn int
		allow bool
		rule  string
		retry time.Duration
	}{
		{"empty", r, nil, 0, 30, true, "", 0},
		{"under minute", r, sentAt(sec(10), sec(20)), 0, 30, true, "", 0},
		{"minute full", r, sentAt(sec(10), sec(20), sec(30)), 0, 30, false, RuleRateMinute, 30 * time.Second},
		{"boundary exactly 60s old is outside", r, sentAt(sec(60), sec(20), sec(30)), 0, 30, true, "", 0},
		{"59s old counts", r, sentAt(sec(59), sec(20), sec(30)), 0, 30, false, RuleRateMinute, time.Second},
		{"future sends ignored", r, sentAt(now.Add(time.Second), sec(1), sec(2)), 0, 30, true, "", 0},
		{"hour full", r, sentAt(sec(100), sec(200), sec(300), sec(400), sec(3000)), 0, 30, false, RuleRateHour, 600 * time.Second},
		{"hour boundary 3600 outside", r, sentAt(sec(3600), sec(200), sec(300), sec(400), sec(500)), 0, 30, true, "", 0},
		{"both full, longest wait wins", Rate{PerMinute: 1, PerHour: 2}, sentAt(sec(10), sec(1800)), 0, 30, false, RuleRateHour, 1800 * time.Second},
		{"over limit uses nth", Rate{PerMinute: 2}, sentAt(sec(5), sec(10), sec(15), sec(50)), 0, 0, false, RuleRateMinute, 50 * time.Second},
		{"pending counted (history is state-agnostic)", Rate{PerMinute: 1}, []Sent{{At: sec(1), Key: "pending"}}, 0, 0, false, RuleRateMinute, 59 * time.Second},
		{"run cap", r, nil, 30, 30, false, RuleRateRun, 0},
		{"run under cap", r, nil, 29, 30, true, "", 0},
		{"run cap disabled", r, nil, 1000, 0, true, "", 0},
		{"limits disabled", Rate{}, sentAt(sec(1), sec(2), sec(3), sec(4)), 0, 0, true, "", 0},
	}
	for _, c := range cases {
		d := CheckRate(c.rate, c.sent, now, c.run, c.maxRn)
		if d.Allowed != c.allow || d.RuleID != c.rule || d.RetryAfter != c.retry {
			t.Errorf("%s: %+v", c.name, d)
			continue
		}
		if !c.allow {
			if d.Category != output.CategoryPolicyDenied || output.ExitOf(d.Err()) != 6 {
				t.Errorf("%s: category", c.name)
			}
			if c.retry > 0 && !strings.Contains(d.Reason, "retry after") {
				t.Errorf("%s: no retry hint: %s", c.name, d.Reason)
			}
		}
	}
}

func TestCheckLoop(t *testing.T) {
	cases := []struct {
		depth, sent int
		allow       bool
	}{{6, 0, true}, {6, 5, true}, {6, 6, false}, {6, 9, false}, {0, 100, true}, {-1, 100, true}, {1, 1, false}}
	for _, c := range cases {
		d := CheckLoop(c.depth, c.sent)
		if d.Allowed != c.allow {
			t.Errorf("%+v: %+v", c, d)
		}
		if !c.allow && (d.RuleID != RuleLoopDepth || d.Category != output.CategoryPolicyDenied) {
			t.Errorf("%+v: %+v", c, d)
		}
	}
}

func TestValidateText(t *testing.T) {
	p := Policy{Send: SendPolicy{MaxBytes: 10, Prefix: "[a] "}}
	ok := []string{"hello", "line1\nline2", "tab\tok", "ü"}
	pOK := Policy{Send: SendPolicy{MaxBytes: 20, Prefix: "[a] "}}
	for _, s := range ok {
		if err := ValidateText(pOK, s); err != nil {
			t.Errorf("%q: %v", s, err)
		}
	}
	bad := []string{"", "   ", "\n\t ", "a\x00b", "a\rb", "a\r\nb", "x\x1b[0m", "a\x7fb", "\xff\xfe", strings.Repeat("a", 7)}
	for _, s := range bad {
		err := ValidateText(p, s)
		if err == nil {
			t.Errorf("%q accepted", s)
			continue
		}
		if output.CategoryOf(err) != output.CategoryValidation {
			t.Errorf("%q: category", s)
		}
	}
	// byte length, not runes: 3 x 3-byte runes + 4-byte prefix = 13 > 10
	if ValidateText(p, "€€€") == nil {
		t.Error("multibyte counted as runes")
	}
	// exactly at the limit: prefix 4 + 6 = 10
	if err := ValidateText(p, strings.Repeat("a", 6)); err != nil {
		t.Errorf("at limit: %v", err)
	}
	// no limit when MaxBytes is zero
	if err := ValidateText(Policy{}, strings.Repeat("a", 100000)); err != nil {
		t.Error(err)
	}
	// errors never echo text
	if err := ValidateText(p, "secret\x00"); err != nil && strings.Contains(err.Error(), "secret") {
		t.Error("echo")
	}
}

func TestValidateKey(t *testing.T) {
	for _, k := range []string{"a", "Key-1.2_3:4", strings.Repeat("k", 128)} {
		if err := ValidateKey(k); err != nil {
			t.Errorf("%q: %v", k, err)
		}
	}
	for _, k := range []string{"", strings.Repeat("k", 129), "has space", "slash/x", "uni\u00e9", "nl\n", "a=b"} {
		if err := ValidateKey(k); err == nil || output.CategoryOf(err) != output.CategoryValidation {
			t.Errorf("%q accepted: %v", k, err)
		}
	}
}

func TestPayloadHash(t *testing.T) {
	base := OutMessage{Text: "hi", HTML: true, Mentions: []OutMention{{ID: 0, AADID: "AA", DisplayName: "J"}}, MarkerKey: "m1"}
	h := PayloadHash("chat:a", "chat:a/chat", base)
	if len(h) != 64 || h != PayloadHash("chat:a", "chat:a/chat", base) {
		t.Fatal("unstable")
	}
	same := base
	same.MarkerKey = "different"
	same.Mentions = []OutMention{{ID: 0, AADID: "aa", DisplayName: "J"}}
	if PayloadHash("chat:a", "chat:a/chat", same) != h {
		t.Error("marker key or aad case changed the hash")
	}
	vary := map[string]string{
		"alias":    PayloadHash("chat:b", "chat:a/chat", base),
		"thread":   PayloadHash("chat:a", "chat:a/other", base),
		"text":     PayloadHash("chat:a", "chat:a/chat", OutMessage{Text: "hi!", HTML: true, Mentions: base.Mentions}),
		"html":     PayloadHash("chat:a", "chat:a/chat", OutMessage{Text: "hi", HTML: false, Mentions: base.Mentions}),
		"nomen":    PayloadHash("chat:a", "chat:a/chat", OutMessage{Text: "hi", HTML: true}),
		"mname":    PayloadHash("chat:a", "chat:a/chat", OutMessage{Text: "hi", HTML: true, Mentions: []OutMention{{ID: 0, AADID: "AA", DisplayName: "K"}}}),
		"mid":      PayloadHash("chat:a", "chat:a/chat", OutMessage{Text: "hi", HTML: true, Mentions: []OutMention{{ID: 1, AADID: "AA", DisplayName: "J"}}}),
		"boundary": PayloadHash("chat:", "a/chat"+"chat:a/chat", base),
	}
	for k, v := range vary {
		if v == h {
			t.Errorf("%s did not change hash", k)
		}
	}
}

func TestBoundaryAmbiguity(t *testing.T) {
	a := PayloadHash("chat:a", "bc", OutMessage{Text: "d"})
	b := PayloadHash("chat:ab", "c", OutMessage{Text: "d"})
	if a == b {
		t.Fatal("field boundary collision")
	}
}
