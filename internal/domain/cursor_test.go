package domain

import (
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

func at(min int) time.Time { return t0.Add(time.Duration(min) * time.Minute) }

func TestItemIDRoundTrip(t *testing.T) {
	for _, g := range []string{"1696341900000", "chat", "a/b/c", "19:x@thread.v2"} {
		id := ItemID("channel:sdlc-alerts", g)
		a, got, err := ParseItemID(id)
		if err != nil || a != "channel:sdlc-alerts" || got != g {
			t.Errorf("%q: %q %q %v", g, a, got, err)
		}
	}
	if ThreadID("chat:x", "chat") != "chat:x/chat" {
		t.Fatal("ThreadID")
	}
}

func TestParseItemIDMalformed(t *testing.T) {
	bad := []string{"", "channel:x", "channel:x/", "/123", "nope/123", "channel:X/1", "19:abc@thread.v2/1", "channel:a/b c", "channel:a/b\x00", "user:a/" + strings.Repeat("x", 513), "chan/nel:x/1"}
	for _, id := range bad {
		_, _, err := ParseItemID(id)
		if err == nil {
			t.Errorf("accepted %q", id)
			continue
		}
		if e, ok := err.(*Error); !ok || e.Category() != "validation" {
			t.Errorf("%q: %v", id, err)
		}
	}
}

func TestCursorEncodeDecode(t *testing.T) {
	for _, tm := range []time.Time{t0, t0.Add(123456789 * time.Nanosecond), t0.In(time.FixedZone("x", 3600))} {
		s := EncodeCursor(tm)
		got, err := DecodeCursor(s)
		if err != nil || !got.Equal(tm) || got.Location() != time.UTC {
			t.Errorf("%v: %q %v %v", tm, s, got, err)
		}
	}
	if EncodeCursor(t0) != "c1:2026-10-03T12:00:00Z" {
		t.Fatal(EncodeCursor(t0))
	}
	for _, bad := range []string{"", "2026-10-03T12:00:00Z", "c1:", "c1:garbage", "c2:2026-10-03T12:00:00Z"} {
		if _, err := DecodeCursor(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestSince(t *testing.T) {
	now := t0
	lb := 30 * time.Minute
	floor := now.Add(-lb)
	cases := []struct {
		name string
		c    CursorState
		ov   *time.Time
		want time.Time
	}{
		{"zero watermark -> floor", CursorState{}, nil, floor},
		{"old watermark -> floor", CursorState{Watermark: now.Add(-time.Hour)}, nil, floor},
		{"recent watermark", CursorState{Watermark: now.Add(-5 * time.Minute)}, nil, now.Add(-5 * time.Minute)},
		{"override replaces watermark", CursorState{Watermark: now.Add(-5 * time.Minute)}, ptr(now.Add(-10 * time.Minute)), now.Add(-10 * time.Minute)},
		{"override bounded by lookback", CursorState{Watermark: now.Add(-5 * time.Minute)}, ptr(now.Add(-5 * time.Hour)), floor},
	}
	for _, c := range cases {
		if got := Since(c.c, now, lb, c.ov); !got.Equal(c.want) {
			t.Errorf("%s: %v want %v", c.name, got, c.want)
		}
	}
}

func ptr(t time.Time) *time.Time { return &t }

func item(g string, recv, mod int) InboundItem {
	return InboundItem{ID: ItemID("chat:a", g), Received: at(recv), Cursor: EncodeCursor(at(mod))}
}

func TestUndelivered(t *testing.T) {
	now := at(100)
	lb := 60 * time.Minute // floor = at(40)
	c := CursorState{
		Watermark: at(50),
		Acked:     []AckEntry{{ID: "acked", Modified: at(60)}, {ID: "edited", Modified: at(61)}},
	}
	items := []InboundItem{
		item("late", 80, 80),
		item("acked", 60, 60),  // dropped
		item("edited", 61, 90), // redelivered edited
		item("old", 10, 10),    // aged out
		item("behind", 45, 45), // at/behind watermark (not acked) dropped
		item("atwm", 50, 50),   // equal watermark dropped
		item("fresh", 55, 55),  // delivered
		{ID: "chat:a/bad", Received: at(70), Cursor: "junk"}, // cursor fallback to Received
	}
	got := c.Undelivered(items, now, lb)
	var ids []string
	for _, it := range got {
		ids = append(ids, it.ID)
	}
	want := []string{"chat:a/fresh", "chat:a/edited", "chat:a/bad", "chat:a/late"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("got %v want %v", ids, want)
	}
	for _, it := range got {
		if it.Edited != (it.ID == "chat:a/edited") {
			t.Errorf("edited flag wrong on %s", it.ID)
		}
	}
	// tie-break by id
	a, b := item("b", 70, 70), item("a", 70, 70)
	got = (CursorState{}).Undelivered([]InboundItem{a, b}, now, lb)
	if got[0].ID != "chat:a/a" {
		t.Fatal("tie order")
	}
	// non-qualified id falls back to whole id
	c2 := CursorState{Acked: []AckEntry{{ID: "raw", Modified: at(60)}}}
	if len((c2).Undelivered([]InboundItem{{ID: "raw", Received: at(60), Cursor: EncodeCursor(at(60))}}, now, lb)) != 0 {
		t.Fatal("raw id ack ignored")
	}
}

func deliveries(ids ...string) []DeliveryEntry {
	var out []DeliveryEntry
	for i, id := range ids {
		out = append(out, DeliveryEntry{ID: id, ThreadID: "t", Modified: at(i + 1), DeliveredAt: at(i + 1)})
	}
	return out
}

func TestAckWatermarkContiguous(t *testing.T) {
	c := CursorState{}.Record(deliveries("m1", "m2", "m3"), at(10), 2*time.Hour)
	// ack out of order: m3 first -> watermark must not move
	c1, acked, already := c.Ack([]AckEntry{{ID: "m3", Modified: at(3)}})
	if acked != 1 || already != 0 || !c1.Watermark.IsZero() {
		t.Fatalf("%+v %d %d", c1, acked, already)
	}
	c2, _, _ := c1.Ack([]AckEntry{{ID: "m1", Modified: at(1)}})
	if !c2.Watermark.Equal(at(1)) {
		t.Fatalf("wm %v", c2.Watermark)
	}
	c3, acked, already := c2.Ack([]AckEntry{{ID: "m2", Modified: at(2)}, {ID: "m1", Modified: at(1)}})
	if acked != 1 || already != 1 || !c3.Watermark.Equal(at(3)) {
		t.Fatalf("%+v %d %d", c3, acked, already)
	}
	// idempotent
	c4, acked, already := c3.Ack([]AckEntry{{ID: "m3", Modified: at(3)}})
	if acked != 0 || already != 1 || !reflect.DeepEqual(c4, c3) {
		t.Fatalf("not idempotent: %d %d", acked, already)
	}
	// Ack does not mutate receiver
	if len(c.Acked) != 0 || !c.Watermark.IsZero() {
		t.Fatal("receiver mutated")
	}
}

func TestAckNewerVersionAndStaleVersion(t *testing.T) {
	c := CursorState{}.Record(deliveries("m1"), at(10), time.Hour)
	c, _, _ = c.Ack([]AckEntry{{ID: "m1", Modified: at(1)}})
	// older ack is already
	_, acked, already := c.Ack([]AckEntry{{ID: "m1", Modified: at(0)}})
	if acked != 0 || already != 1 {
		t.Fatal("stale")
	}
	// newer version re-acks
	c2, acked, _ := c.Ack([]AckEntry{{ID: "m1", Modified: at(5)}})
	if acked != 1 || len(c2.Acked) != 1 || !c2.Acked[0].Modified.Equal(at(5)) {
		t.Fatalf("%+v", c2.Acked)
	}
	// acked at older version than delivered does not advance watermark
	d := CursorState{}.Record([]DeliveryEntry{{ID: "x", Modified: at(9), DeliveredAt: at(10)}}, at(10), time.Hour)
	d, _, _ = d.Ack([]AckEntry{{ID: "x", Modified: at(8)}})
	if !d.Watermark.IsZero() {
		t.Fatal("advanced on stale version")
	}
}

func TestAckEqualTimeGroup(t *testing.T) {
	c := CursorState{}.Record([]DeliveryEntry{
		{ID: "a", Modified: at(1), DeliveredAt: at(2)}, {ID: "b", Modified: at(1), DeliveredAt: at(2)},
	}, at(3), time.Hour)
	c1, _, _ := c.Ack([]AckEntry{{ID: "a", Modified: at(1)}})
	if !c1.Watermark.IsZero() {
		t.Fatal("advanced with half a tie group acked")
	}
	c2, _, _ := c1.Ack([]AckEntry{{ID: "b", Modified: at(1)}})
	if !c2.Watermark.Equal(at(1)) {
		t.Fatal("group complete should advance")
	}
}

func TestAckOrderIndependentProperty(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for iter := 0; iter < 200; iter++ {
		n := 2 + rng.Intn(8)
		var ds []DeliveryEntry
		var acks []AckEntry
		for i := 0; i < n; i++ {
			id := string(rune('a' + i))
			mod := at(1 + rng.Intn(5)) // collisions on purpose
			ds = append(ds, DeliveryEntry{ID: id, Modified: mod, DeliveredAt: at(1)})
			if rng.Intn(4) != 0 {
				acks = append(acks, AckEntry{ID: id, Modified: mod})
			}
		}
		base := CursorState{}.Record(ds, at(2), time.Hour)
		rng.Shuffle(len(ds), func(i, j int) {})
		apply := func(order []AckEntry, batch int) CursorState {
			c := base
			for i := 0; i < len(order); i += batch {
				e := i + batch
				if e > len(order) {
					e = len(order)
				}
				c, _, _ = c.Ack(order[i:e])
			}
			return c
		}
		ref := apply(acks, len(acks)+1)
		for k := 0; k < 5; k++ {
			sh := append([]AckEntry(nil), acks...)
			rng.Shuffle(len(sh), func(i, j int) { sh[i], sh[j] = sh[j], sh[i] })
			if got := apply(sh, 1+rng.Intn(3)); !reflect.DeepEqual(got, ref) {
				t.Fatalf("iter %d: order dependent:\n%+v\n%+v", iter, got, ref)
			}
		}
	}
}

func TestRecordKnownPrune(t *testing.T) {
	now := at(300)
	keep := 90 * time.Minute // cutoff at(210)
	c := CursorState{
		Watermark: at(250),
		Acked:     []AckEntry{{ID: "oldacked", Modified: at(100)}, {ID: "keepacked", Modified: at(260)}, {ID: "oldaheadwm", Modified: at(300)}},
		Delivered: []DeliveryEntry{{ID: "stale", Modified: at(1), DeliveredAt: at(1)}, {ID: "dup", Modified: at(250), DeliveredAt: at(250)}},
	}
	c = c.Record([]DeliveryEntry{
		{ID: "dup", Modified: at(280), DeliveredAt: at(299)}, // newer version replaces
		{ID: "new", Modified: at(290), DeliveredAt: at(299)},
	}, now, keep)
	if _, ok := c.Known("stale"); ok {
		t.Error("stale delivery not pruned")
	}
	if e, ok := c.Known("dup"); !ok || !e.Modified.Equal(at(280)) {
		t.Errorf("dup: %+v", e)
	}
	// older redelivery does not downgrade
	c = c.Record([]DeliveryEntry{{ID: "dup", Modified: at(100), DeliveredAt: at(299)}}, now, keep)
	if e, _ := c.Known("dup"); !e.Modified.Equal(at(280)) {
		t.Error("downgraded")
	}
	if _, ok := c.Known("missing"); ok {
		t.Error("missing found")
	}
	var acked []string
	for _, a := range c.Acked {
		acked = append(acked, a.ID)
	}
	if !reflect.DeepEqual(acked, []string{"keepacked", "oldaheadwm"}) {
		t.Errorf("acked %v", acked)
	}
	if !c.Updated.Equal(now) {
		t.Error("updated")
	}
	if c.Delivered[0].ID != "new" && c.Delivered[0].ID != "dup" {
		t.Error("order")
	}
}
