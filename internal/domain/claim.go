package domain

import "time"

// SendClaim is the input of an atomic check-and-claim (FR-R4): the ledger
// evaluates the rate windows and the reply-depth guard against the sent
// history (which includes pending reservations) and, only if allowed, records
// the reservation in the same locked operation.
type SendClaim struct {
	// Key is the idempotency key; empty for an unkeyed send, which gets a slot.
	Key         string
	PayloadHash string
	Alias       Alias
	ThreadID    string
	Now         time.Time

	Rate          Rate
	RunWrites     int // sends already made by this process
	MaxRun        int
	ReplyDepthMax int
	ReplyWindow   time.Duration
}

// Claim is the result of a claim. Decision.Allowed is false when a limit is hit
// (nothing was reserved). Slot names the reservation of an unkeyed send.
type Claim struct {
	Decision    Decision
	Reservation Reservation
	Slot        string
}

// HistorySince is the oldest history entry a claim needs.
func (c SendClaim) HistorySince() time.Time {
	w := c.ReplyWindow
	if w < time.Hour {
		w = time.Hour
	}
	return c.Now.Add(-w)
}

// Check evaluates the rate and loop limits over history (sent plus pending).
func (c SendClaim) Check(history []Sent) Decision {
	if d := CheckRate(c.Rate, history, c.Now, c.RunWrites, c.MaxRun); !d.Allowed {
		return d
	}
	n := 0
	if c.ThreadID != "" {
		since := c.Now.Add(-c.ReplyWindow)
		for _, h := range history {
			if h.ThreadID == c.ThreadID && !h.At.Before(since) {
				n++
			}
		}
	}
	return CheckLoop(c.ReplyDepthMax, n)
}
