package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stainedhead/teams-cli/internal/domain"
)

const alias = domain.Alias("channel:sdlc-alerts")

func del(id string, mod time.Time) domain.DeliveryEntry {
	return domain.DeliveryEntry{ID: id, ThreadID: "channel:sdlc-alerts/" + id, Modified: mod, DeliveredAt: t0}
}

func TestGetUnknownAliasIsZero(t *testing.T) {
	s, _ := newStore(t)
	cs, err := s.Get(bg, alias)
	if err != nil || !cs.Watermark.IsZero() || len(cs.Acked) != 0 || len(cs.Delivered) != 0 {
		t.Fatalf("%+v %v", cs, err)
	}
}

func TestRecordAndGetPersistAcrossReopen(t *testing.T) {
	s, fc := newStore(t, withStandIn())
	m1, m2 := t0.Add(-time.Minute), t0.Add(-2*time.Minute)
	if err := s.RecordDeliveries(bg, alias, []domain.DeliveryEntry{del("a", m1), del("b", m2)}, fc.Now()); err != nil {
		t.Fatal(err)
	}
	s2 := reopen(t, s, fc, withStandIn())
	cs, err := s2.Get(bg, alias)
	if err != nil || len(cs.Delivered) != 2 || cs.Delivered[0].ID != "a" || !cs.Delivered[0].Modified.Equal(m1) ||
		cs.Delivered[1].ThreadID != "channel:sdlc-alerts/b" || !cs.Updated.Equal(t0) {
		t.Fatalf("%+v %v", cs, err)
	}
	other, _ := s2.Get(bg, "channel:other")
	if len(other.Delivered) != 0 {
		t.Fatal("deliveries leaked across destinations")
	}
	if err := s.RecordDeliveries(bg, alias, nil, fc.Now()); err != nil {
		t.Fatal(err) // empty batch is a no-op
	}
}

func TestAckKnownAndUnknown(t *testing.T) {
	s, fc := newStore(t, withStandIn())
	m := []time.Time{t0.Add(-3 * time.Minute), t0.Add(-2 * time.Minute), t0.Add(-time.Minute)}
	_ = s.RecordDeliveries(bg, alias, []domain.DeliveryEntry{del("a", m[0]), del("b", m[1]), del("c", m[2])}, fc.Now())

	acked, already, unknown, err := s.Ack(bg, alias, []string{"a", "channel:sdlc-alerts/b", "a"}, fc.Now())
	if err != nil || acked != 2 || already != 0 || len(unknown) != 0 {
		t.Fatalf("ack: %d %d %v %v", acked, already, unknown, err)
	}
	cs, _ := s.Get(bg, alias)
	if len(cs.Acked) != 2 || !cs.Watermark.Equal(m[1]) {
		t.Fatalf("state: %+v", cs)
	}
	// Idempotent.
	acked, already, unknown, err = s.Ack(bg, alias, []string{"a"}, fc.Now())
	if err != nil || acked != 0 || already != 1 || unknown != nil {
		t.Fatalf("repeat: %d %d %v %v", acked, already, unknown, err)
	}
	// One unknown id: error list returned, nothing at all changes (even "c").
	before, _ := os.ReadFile(filepath.Join(s.Dir(), CursorsFile))
	acked, already, unknown, err = s.Ack(bg, alias, []string{"c", "ghost", "channel:sdlc-alerts/ghost2"}, fc.Now().Add(time.Hour))
	if err != nil || acked != 0 || already != 0 || len(unknown) != 2 || unknown[0] != "ghost" || unknown[1] != "channel:sdlc-alerts/ghost2" {
		t.Fatalf("unknown: %d %d %v %v", acked, already, unknown, err)
	}
	after, _ := os.ReadFile(filepath.Join(s.Dir(), CursorsFile))
	if string(before) != string(after) {
		t.Fatal("a rejected ack must change nothing")
	}
	// An id delivered to another alias is unknown here.
	_ = s.RecordDeliveries(bg, "chat:dev", []domain.DeliveryEntry{del("z", m[0])}, fc.Now())
	if _, _, unknown, _ = s.Ack(bg, alias, []string{"z"}, fc.Now()); len(unknown) != 1 {
		t.Fatalf("cross-alias ack accepted: %v", unknown)
	}
}

func TestAckUsesNewestDeliveredVersion(t *testing.T) {
	s, fc := newStore(t)
	var got []domain.AckEntry
	s.alg = algebra{record: standIn.record, ack: func(cs domain.CursorState, e []domain.AckEntry) (domain.CursorState, int, int) {
		got = e
		return cs, len(e), 0
	}}
	// Two delivered versions of the same id (edit after first delivery).
	cs := domain.CursorState{Delivered: []domain.DeliveryEntry{del("a", t0.Add(-time.Hour)), del("a", t0.Add(-time.Minute)), del("a", t0.Add(-2*time.Hour))}}
	if v, ok := deliveredVersion(cs, "a"); !ok || !v.Equal(t0.Add(-time.Minute)) {
		t.Fatalf("deliveredVersion = %v %v", v, ok)
	}
	_ = s.mutateCursors(t0, func(cf *cursorsFile) (bool, error) {
		cf.Destinations[string(alias)] = toCursorDTO(cs)
		return true, nil
	})
	if _, _, _, err := s.Ack(bg, alias, []string{"a"}, fc.Now()); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got[0].Modified.Equal(t0.Add(-time.Minute)) {
		t.Fatalf("acked entry = %+v", got)
	}
}

func TestPruneAcked(t *testing.T) {
	s, fc := newStore(t, WithCursorRetention(2*time.Hour), withStandIn())
	wm := t0.Add(-time.Hour)
	cs := domain.CursorState{Watermark: wm, Acked: []domain.AckEntry{
		{ID: "old-behind", Modified: t0.Add(-5 * time.Hour)},       // behind watermark and old: pruned
		{ID: "recent-behind", Modified: t0.Add(-90 * time.Minute)}, // behind watermark but inside retention: kept
		{ID: "old-ahead", Modified: t0.Add(-30 * time.Minute)},     // ahead of watermark: kept
	}}
	got := s.pruneAcked(cs, fc.Now())
	if len(got.Acked) != 2 || got.Acked[0].ID != "recent-behind" || got.Acked[1].ID != "old-ahead" {
		t.Fatalf("%+v", got.Acked)
	}
	// A zero watermark never prunes anything.
	if n := len(s.pruneAcked(domain.CursorState{Acked: cs.Acked}, fc.Now()).Acked); n != 3 {
		t.Fatalf("zero watermark pruned %d", 3-n)
	}
}

func TestWithCursorRetentionIgnoresNonPositive(t *testing.T) {
	s, _ := newStore(t, WithCursorRetention(0), WithCursorRetention(-time.Hour))
	if s.keep != DefaultCursorRetention {
		t.Fatalf("keep = %v", s.keep)
	}
}

func TestDeltaAndChatCache(t *testing.T) {
	s, _ := newStore(t)
	if err := s.StoreDelta(bg, alias, "https://graph/delta?token=abc"); err != nil {
		t.Fatal(err)
	}
	if err := s.CacheChat(bg, "user:jane.doe", "19:chat@unq"); err != nil {
		t.Fatal(err)
	}
	if id, ok := s.ResolveChat(bg, "user:jane.doe"); !ok || id != "19:chat@unq" {
		t.Fatalf("%q %v", id, ok)
	}
	if _, ok := s.ResolveChat(bg, "user:nobody"); ok {
		t.Fatal("unexpected hit")
	}
	if err := s.DropChat(bg, "user:jane.doe"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.ResolveChat(bg, "user:jane.doe"); ok {
		t.Fatal("DropChat did not drop")
	}
	cs, _ := s.Get(bg, alias)
	if cs.DeltaToken != "https://graph/delta?token=abc" {
		t.Fatalf("delta lost: %q", cs.DeltaToken)
	}
}

func TestSetCursorKeepsOtherFields(t *testing.T) {
	s, fc := newStore(t, withStandIn())
	_ = s.RecordDeliveries(bg, alias, []domain.DeliveryEntry{del("a", t0.Add(-time.Minute))}, fc.Now())
	_ = s.StoreDelta(bg, alias, "d1")
	_ = s.CacheChat(bg, alias, "c1")
	cs, _ := s.Get(bg, alias)
	if len(cs.Delivered) != 1 || cs.DeltaToken != "d1" || cs.ChatID != "c1" {
		t.Fatalf("%+v", cs)
	}
}

func TestCorruptCursorsFailOpenAndQuarantine(t *testing.T) {
	for name, content := range map[string]string{
		"garbage":       "{{{",
		"empty":         "",
		"wrong version": `{"version":9,"destinations":{}}`,
	} {
		t.Run(name, func(t *testing.T) {
			s, fc := newStore(t, withStandIn())
			p := filepath.Join(s.Dir(), CursorsFile)
			writeFile(t, p, content)
			cs, err := s.Get(bg, alias)
			if err != nil || len(cs.Delivered) != 0 {
				t.Fatalf("must fail open: %+v %v", cs, err)
			}
			if _, err := os.Stat(p); !os.IsNotExist(err) {
				t.Fatal("corrupt file must be moved aside")
			}
			var q []string
			ents, _ := os.ReadDir(s.Dir())
			for _, e := range ents {
				if strings.HasPrefix(e.Name(), CursorsFile+".corrupt-") {
					q = append(q, e.Name())
				}
			}
			if len(q) != 1 {
				t.Fatalf("quarantine files = %v", q)
			}
			// The store keeps working afterwards.
			if err := s.RecordDeliveries(bg, alias, []domain.DeliveryEntry{del("a", t0)}, fc.Now()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCursorFileFormat(t *testing.T) {
	s, fc := newStore(t, withStandIn())
	_ = s.RecordDeliveries(bg, alias, []domain.DeliveryEntry{del("a", t0)}, fc.Now())
	_, _, _, _ = s.Ack(bg, alias, []string{"a"}, fc.Now())
	b, _ := os.ReadFile(filepath.Join(s.Dir(), CursorsFile))
	for _, want := range []string{`"version": 1`, `"destinations"`, `"channel:sdlc-alerts"`, `"delivered"`, `"acked"`, `"watermark"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("cursor file lacks %s:\n%s", want, b)
		}
	}
	fi, _ := os.Stat(filepath.Join(s.Dir(), CursorsFile))
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", fi.Mode().Perm())
	}
}

// TestDomainAlgebraEndToEnd runs the real domain algebra through the store. It
// is skipped while the domain cursor functions are still stubs.
func TestDomainAlgebraEndToEnd(t *testing.T) {
	probe := domain.CursorState{}.Record([]domain.DeliveryEntry{del("p", t0)}, t0, time.Hour)
	if len(probe.Delivered) == 0 {
		t.Skip("domain.CursorState.Record is still a stub (WS-A)")
	}
	s, fc := newStore(t)
	m1, m2 := t0.Add(-2*time.Minute), t0.Add(-time.Minute)
	if err := s.RecordDeliveries(bg, alias, []domain.DeliveryEntry{del("a", m1), del("b", m2)}, fc.Now()); err != nil {
		t.Fatal(err)
	}
	acked, already, unknown, err := s.Ack(bg, alias, []string{"a", "b"}, fc.Now())
	if err != nil || acked != 2 || already != 0 || len(unknown) != 0 {
		t.Fatalf("%d %d %v %v", acked, already, unknown, err)
	}
	acked, already, _, _ = s.Ack(bg, alias, []string{"a"}, fc.Now())
	if acked != 0 || already != 1 {
		t.Fatalf("idempotency: %d %d", acked, already)
	}
	cs, _ := s.Get(bg, alias)
	if !cs.Watermark.Equal(m2) {
		t.Fatalf("watermark = %v, want %v", cs.Watermark, m2)
	}
}
