// Package clock provides the system and fake implementations of the use-case
// Clock and Rand ports. It imports only the standard library.
package clock

import (
	"context"
	"math/rand/v2"
	"sync"
	"time"
)

// System is the wall clock. Sleep honors context cancellation.
type System struct{}

// Now returns the current time.
func (System) Now() time.Time { return time.Now() }

// Sleep waits for d or until ctx is done, whichever comes first.
func (System) Sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Fake is a manually advanced clock for tests. Sleep advances the clock by the
// requested duration and returns at once, recording the request.
type Fake struct {
	mu     sync.Mutex
	now    time.Time
	sleeps []time.Duration
}

// NewFake returns a Fake starting at t.
func NewFake(t time.Time) *Fake { return &Fake{now: t} }

// Now returns the fake time.
func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// Advance moves the fake time forward by d.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	f.now = f.now.Add(d)
	f.mu.Unlock()
}

// Set moves the fake time to t.
func (f *Fake) Set(t time.Time) {
	f.mu.Lock()
	f.now = t
	f.mu.Unlock()
}

// Sleep records d, advances the clock by it and returns immediately. It
// returns ctx.Err() without advancing when ctx is already done.
func (f *Fake) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sleeps = append(f.sleeps, d)
	if d > 0 {
		f.now = f.now.Add(d)
	}
	return nil
}

// Sleeps returns a copy of every duration passed to Sleep.
func (f *Fake) Sleeps() []time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]time.Duration(nil), f.sleeps...)
}

// Rand is the production jitter source.
type Rand struct{}

// Jitter returns base scaled by a uniform factor in [1-pct, 1+pct]. A
// non-positive base or pct returns base unchanged.
func (Rand) Jitter(base time.Duration, pct float64) time.Duration {
	if base <= 0 || pct <= 0 {
		return base
	}
	return scale(base, pct, rand.Float64()) //nolint:gosec // jitter, not security
}

// FixedRand is a deterministic Rand for tests: U in [0,1) selects the point in
// the jitter range (0 = lowest, 0.5 = base, just under 1 = highest).
type FixedRand struct{ U float64 }

// Jitter returns base scaled by the fixed factor.
func (r FixedRand) Jitter(base time.Duration, pct float64) time.Duration {
	if base <= 0 || pct <= 0 {
		return base
	}
	return scale(base, pct, r.U)
}

func scale(base time.Duration, pct, u float64) time.Duration {
	f := 1 + pct*(2*u-1)
	return time.Duration(float64(base) * f)
}
