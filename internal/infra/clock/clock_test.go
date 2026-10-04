package clock

import (
	"context"
	"testing"
	"time"
)

func TestFakeAdvanceAndSleep(t *testing.T) {
	t0 := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	f := NewFake(t0)
	f.Advance(time.Minute)
	if !f.Now().Equal(t0.Add(time.Minute)) {
		t.Fatalf("now = %v", f.Now())
	}
	if err := f.Sleep(context.Background(), 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if !f.Now().Equal(t0.Add(time.Minute + 5*time.Second)) {
		t.Fatalf("sleep must advance: %v", f.Now())
	}
	if err := f.Sleep(context.Background(), -time.Second); err != nil {
		t.Fatal(err)
	}
	if got := f.Sleeps(); len(got) != 2 || got[0] != 5*time.Second {
		t.Fatalf("sleeps = %v", got)
	}
	f.Set(t0)
	if !f.Now().Equal(t0) {
		t.Fatal("Set")
	}
}

func TestFakeSleepCancelled(t *testing.T) {
	f := NewFake(time.Unix(0, 0))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := f.Sleep(ctx, time.Second); err == nil {
		t.Fatal("want ctx error")
	}
	if len(f.Sleeps()) != 0 || !f.Now().Equal(time.Unix(0, 0)) {
		t.Fatal("cancelled sleep must not advance")
	}
}

func TestSystemSleep(t *testing.T) {
	var s System
	if time.Since(s.Now()) > time.Minute {
		t.Fatal("Now")
	}
	if err := s.Sleep(context.Background(), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := s.Sleep(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Sleep(ctx, time.Hour); err == nil {
		t.Fatal("want ctx error")
	}
	if err := s.Sleep(ctx, 0); err == nil {
		t.Fatal("want ctx error for zero sleep on cancelled ctx")
	}
}

func TestJitterBounds(t *testing.T) {
	base := 10 * time.Second
	if got := (FixedRand{U: 0.5}).Jitter(base, 0.2); got != base {
		t.Fatalf("mid = %v", got)
	}
	if got := (FixedRand{U: 0}).Jitter(base, 0.2); got != 8*time.Second {
		t.Fatalf("low = %v", got)
	}
	if got := (FixedRand{U: 0.999999}).Jitter(base, 0.2); got < 11900*time.Millisecond || got > 12*time.Second {
		t.Fatalf("high = %v", got)
	}
	if got := (FixedRand{}).Jitter(0, 0.2); got != 0 {
		t.Fatal("zero base")
	}
	if got := (FixedRand{}).Jitter(base, 0); got != base {
		t.Fatal("zero pct")
	}
	var r Rand
	for range 200 {
		got := r.Jitter(base, 0.2)
		if got < 8*time.Second || got > 12*time.Second {
			t.Fatalf("out of range: %v", got)
		}
	}
	if r.Jitter(-1, 0.2) != -1 || r.Jitter(base, -1) != base {
		t.Fatal("degenerate inputs must pass through")
	}
}
