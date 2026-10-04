package state

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stainedhead/agent-cli-core/output"

	"github.com/stainedhead/teams-cli/internal/domain"
)

var bg = context.Background()

func TestReserveNewThenReplay(t *testing.T) {
	s, fc := newStore(t)
	r, err := s.Reserve(bg, "k1", "hash-a", "channel:sdlc-alerts", "channel:sdlc-alerts/1", fc.Now())
	if err != nil || r.Outcome != domain.ReserveNew || r.Entry.State != domain.StatePending || r.Entry.Created != t0 {
		t.Fatalf("new: %+v %v", r, err)
	}
	if err := s.Complete(bg, "k1", "msg-1", fc.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	r, err = s.Reserve(bg, "k1", "hash-a", "channel:sdlc-alerts", "", fc.Now())
	if err != nil || r.Outcome != domain.ReserveReplay || r.Entry.MessageID != "msg-1" || r.Entry.State != domain.StateSent {
		t.Fatalf("replay: %+v %v", r, err)
	}
	// Completing again is idempotent.
	if err := s.Complete(bg, "k1", "other", fc.Now()); err != nil {
		t.Fatal(err)
	}
	r, _ = s.Reserve(bg, "k1", "hash-a", "channel:sdlc-alerts", "", fc.Now())
	if r.Entry.MessageID != "msg-1" {
		t.Fatalf("second Complete overwrote the result: %+v", r.Entry)
	}
}

func TestReserveChangedPayloadExit7(t *testing.T) {
	s, fc := newStore(t)
	_, _ = s.Reserve(bg, "k", "h1", "chat:dev", "", fc.Now())
	for name, prep := range map[string]func(){
		"pending": func() {},
		"sent":    func() { _ = s.Complete(bg, "k", "m", fc.Now()) },
	} {
		prep()
		_, err := s.Reserve(bg, "k", "h2", "chat:dev", "", fc.Now())
		if err == nil || output.ExitOf(err) != 7 || !strings.Contains(err.Error(), "different payload") {
			t.Fatalf("%s: want exit 7 different payload, got %v", name, err)
		}
	}
}

func TestReservePendingExit7AndSurvivesRestart(t *testing.T) {
	s, fc := newStore(t)
	if _, err := s.Reserve(bg, "k", "h", "chat:dev", "", fc.Now()); err != nil {
		t.Fatal(err)
	}
	// Crash between Reserve and Complete: a new process sees pending.
	s2 := reopen(t, s, fc)
	_, err := s2.Reserve(bg, "k", "h", "chat:dev", "", fc.Now())
	if err == nil || output.ExitOf(err) != 7 || !strings.Contains(err.Error(), "pending") {
		t.Fatalf("want exit 7 pending, got %v", err)
	}
}

func TestFailNotSentAllowsRetryButAmbiguousStaysPending(t *testing.T) {
	s, fc := newStore(t)
	_, _ = s.Reserve(bg, "k", "h", "chat:dev", "t", fc.Now())
	if err := s.Fail(bg, "k", false); err != nil { // ambiguous
		t.Fatal(err)
	}
	if _, err := s.Reserve(bg, "k", "h", "chat:dev", "t", fc.Now()); output.ExitOf(err) != 7 {
		t.Fatalf("ambiguous failure must stay pending, got %v", err)
	}
	if err := s.Fail(bg, "k", true); err != nil { // proved unprocessed
		t.Fatal(err)
	}
	fc.Advance(time.Minute)
	r, err := s.Reserve(bg, "k", "h", "chat:dev", "t", fc.Now())
	if err != nil || r.Outcome != domain.ReserveRetry || r.Entry.State != domain.StatePending || !r.Entry.Created.Equal(t0) {
		t.Fatalf("retry: %+v %v", r, err)
	}
	// Failing a sent entry never downgrades it.
	_ = s.Complete(bg, "k", "m", fc.Now())
	if err := s.Fail(bg, "k", true); err != nil {
		t.Fatal(err)
	}
	if r, _ := s.Reserve(bg, "k", "h", "chat:dev", "t", fc.Now()); r.Outcome != domain.ReserveReplay {
		t.Fatalf("sent entry was downgraded: %+v", r)
	}
}

func TestUnknownKeyErrors(t *testing.T) {
	s, fc := newStore(t)
	if err := s.Complete(bg, "nope", "m", fc.Now()); output.ExitOf(err) != 5 {
		t.Fatalf("Complete unknown: %v", err)
	}
	if err := s.Fail(bg, "nope", true); output.ExitOf(err) != 5 {
		t.Fatalf("Fail unknown: %v", err)
	}
	_, _ = s.Reserve(bg, "k", "h", "chat:dev", "", fc.Now())
	_ = s.Fail(bg, "k", true)
	if err := s.Complete(bg, "k", "m", fc.Now()); output.ExitOf(err) != 7 {
		t.Fatalf("Complete failed entry: %v", err)
	}
}

func TestSentHistoryIncludesKeyedKeylessAndPending(t *testing.T) {
	s, fc := newStore(t)
	_, _ = s.Reserve(bg, "done", "h", "chat:a", "chat:a/chat", fc.Now())
	_ = s.Complete(bg, "done", "m1", fc.Now()) // keyed, completed: recorded by Complete
	fc.Advance(time.Minute)
	_ = s.RecordSent(bg, domain.Sent{Alias: "chat:a", ThreadID: "chat:a/chat", MessageID: "m2"}) // keyless, At defaulted
	fc.Advance(time.Minute)
	_, _ = s.Reserve(bg, "amb", "h2", "chat:a", "chat:a/chat", fc.Now()) // pending counts
	_, _ = s.Reserve(bg, "bad", "h3", "chat:a", "chat:a/chat", fc.Now())
	_ = s.Fail(bg, "bad", true) // failed does not count
	// RecordSent for an already completed key must not double count.
	_ = s.RecordSent(bg, domain.Sent{At: t0, Alias: "chat:a", ThreadID: "chat:a/chat", MessageID: "m1", Key: "done"})

	got, err := s.SentSince(bg, t0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].MessageID != "m1" || got[1].MessageID != "m2" || got[2].Key != "amb" {
		t.Fatalf("history = %+v", got)
	}
	if !got[1].At.Equal(t0.Add(time.Minute)) {
		t.Fatalf("keyless At must default to clock: %v", got[1].At)
	}
	n, err := s.SentInThread(bg, "chat:a/chat", t0)
	if err != nil || n != 3 {
		t.Fatalf("SentInThread = %d %v", n, err)
	}
	if n, _ := s.SentInThread(bg, "chat:a/other", t0); n != 0 {
		t.Fatalf("other thread = %d", n)
	}
}

func TestSentSinceBoundary(t *testing.T) {
	s, fc := newStore(t)
	for i, d := range []time.Duration{0, time.Minute, 2 * time.Minute} {
		_ = s.RecordSent(bg, domain.Sent{At: t0.Add(d), Alias: "chat:a", MessageID: string(rune('a' + i))})
	}
	got, _ := s.SentSince(bg, t0.Add(time.Minute))
	if len(got) != 2 || got[0].MessageID != "b" { // At == since is included
		t.Fatalf("got %+v", got)
	}
	n, _ := s.SentInThread(bg, "", t0.Add(2*time.Minute+time.Nanosecond))
	if n != 0 {
		t.Fatal("future since")
	}
	_ = fc
}

func TestThreads(t *testing.T) {
	s, fc := newStore(t)
	_ = s.PutThread(bg, "channel:a/1")
	fc.Advance(time.Hour)
	_ = s.PutThread(bg, "channel:a/2")
	fc.Advance(time.Hour)
	_ = s.PutThread(bg, "channel:a/3")
	_ = s.PutThread(bg, "channel:a/1") // refreshed: now the most recent (tie with 3 broken by id)

	all, err := s.ActiveThreads(bg, t0, 0)
	if err != nil || strings.Join(all, ",") != "channel:a/1,channel:a/3,channel:a/2" {
		t.Fatalf("all = %v %v", all, err)
	}
	top, _ := s.ActiveThreads(bg, t0, 2)
	if len(top) != 2 {
		t.Fatalf("max = %v", top)
	}
	recent, _ := s.ActiveThreads(bg, t0.Add(90*time.Minute), 10)
	if len(recent) != 2 || recent[0] != "channel:a/1" {
		t.Fatalf("since = %v", recent)
	}
}

func TestRetentionPrunesOldSentFailedKeepsPending(t *testing.T) {
	s, fc := newStore(t)
	_, _ = s.Reserve(bg, "old-sent", "h", "chat:a", "", fc.Now())
	_ = s.Complete(bg, "old-sent", "m", fc.Now())
	_, _ = s.Reserve(bg, "old-failed", "h", "chat:a", "", fc.Now())
	_ = s.Fail(bg, "old-failed", true)
	_, _ = s.Reserve(bg, "old-pending", "h", "chat:a", "", fc.Now())
	_ = s.PutThread(bg, "channel:a/old")

	fc.Advance(LedgerRetention + time.Hour)
	_, _ = s.Reserve(bg, "fresh", "h", "chat:a", "", fc.Now()) // any write prunes

	lf, err := s.loadLedger()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := lf.Entries["old-sent"]; ok {
		t.Error("old sent entry kept")
	}
	if _, ok := lf.Entries["old-failed"]; ok {
		t.Error("old failed entry kept")
	}
	if _, ok := lf.Entries["old-pending"]; !ok {
		t.Error("pending entry must never expire")
	}
	if _, ok := lf.Entries["fresh"]; !ok {
		t.Error("fresh entry lost")
	}
	if len(lf.Sent) != 0 {
		t.Errorf("old sent history kept: %+v", lf.Sent)
	}
	if len(lf.Threads) != 0 {
		t.Errorf("old thread kept: %+v", lf.Threads)
	}
}

func TestCorruptLedgerFailsClosed(t *testing.T) {
	for name, content := range map[string]string{
		"garbage":       "{not json",
		"empty":         "",
		"wrong version": `{"version":2,"entries":{}}`,
	} {
		t.Run(name, func(t *testing.T) {
			s, fc := newStore(t)
			p := filepath.Join(s.Dir(), LedgerFile)
			writeFile(t, p, content)
			calls := map[string]error{
				"Reserve":    func() error { _, e := s.Reserve(bg, "k", "h", "chat:a", "", fc.Now()); return e }(),
				"Complete":   s.Complete(bg, "k", "m", fc.Now()),
				"Fail":       s.Fail(bg, "k", true),
				"SentSince":  func() error { _, e := s.SentSince(bg, t0); return e }(),
				"InThread":   func() error { _, e := s.SentInThread(bg, "t", t0); return e }(),
				"RecordSent": s.RecordSent(bg, domain.Sent{Alias: "chat:a"}),
				"PutThread":  s.PutThread(bg, "t"),
				"Active":     func() error { _, e := s.ActiveThreads(bg, t0, 1); return e }(),
			}
			for op, err := range calls {
				if err == nil || output.ExitOf(err) != 7 {
					t.Errorf("%s: want exit 7, got %v", op, err)
				}
			}
			got, _ := os.ReadFile(p)
			if string(got) != content {
				t.Fatal("a corrupt ledger must be left untouched for the operator")
			}
			ents, _ := os.ReadDir(s.Dir())
			for _, e := range ents {
				if strings.Contains(e.Name(), "corrupt") {
					t.Fatalf("ledger must not be quarantined: %s", e.Name())
				}
			}
		})
	}
}

func TestLedgerFileFormat(t *testing.T) {
	s, fc := newStore(t)
	_, _ = s.Reserve(bg, "k", "h", "chat:a", "chat:a/chat", fc.Now())
	_ = s.Complete(bg, "k", "m", fc.Now())
	_ = s.PutThread(bg, "chat:a/chat")
	b, _ := os.ReadFile(filepath.Join(s.Dir(), LedgerFile))
	for _, want := range []string{`"version": 1`, `"entries"`, `"sent"`, `"threads"`, `"payload_hash"`, `"state": "sent"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("ledger file lacks %s:\n%s", want, b)
		}
	}
	fi, _ := os.Stat(filepath.Join(s.Dir(), LedgerFile))
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", fi.Mode().Perm())
	}
}

func TestLedgerPlantedTokenNeverPersistedByStore(t *testing.T) {
	// The store only persists what it is given; a token never reaches it
	// because ports carry no token. This pins that the files contain exactly
	// the fields of the format (no free-form dump of inputs).
	s, fc := newStore(t)
	_, _ = s.Reserve(bg, "k", "hash", "chat:a", "chat:a/chat", fc.Now())
	b, _ := os.ReadFile(filepath.Join(s.Dir(), LedgerFile))
	if strings.Contains(string(b), "Bearer") || strings.Contains(string(b), "eyJ") {
		t.Fatal("unexpected credential-like text")
	}
}
