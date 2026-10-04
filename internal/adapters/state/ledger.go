package state

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/stainedhead/teams-cli/internal/domain"
)

// On-disk shapes. The domain types carry no JSON tags, so the file format is
// owned here. Threads is a map of thread id to last-posted time (the data
// dictionary lists bare ids; ActiveThreads needs the time).
type ledgerFile struct {
	Version int                  `json:"version"`
	Entries map[string]entryDTO  `json:"entries"`
	Sent    []sentDTO            `json:"sent"`
	Threads map[string]time.Time `json:"threads"`
}

type entryDTO struct {
	Key         string    `json:"key"`
	PayloadHash string    `json:"payload_hash"`
	Alias       string    `json:"alias"`
	ThreadID    string    `json:"thread_id,omitempty"`
	MessageID   string    `json:"message_id,omitempty"`
	State       string    `json:"state"`
	Created     time.Time `json:"created"`
	Updated     time.Time `json:"updated"`
}

type sentDTO struct {
	At        time.Time `json:"at"`
	Alias     string    `json:"alias"`
	ThreadID  string    `json:"thread_id,omitempty"`
	MessageID string    `json:"message_id,omitempty"`
	Key       string    `json:"key,omitempty"`
}

func (e entryDTO) domain() domain.LedgerEntry {
	return domain.LedgerEntry{
		Key: e.Key, PayloadHash: e.PayloadHash, Alias: e.Alias, ThreadID: e.ThreadID, MessageID: e.MessageID,
		State: domain.EntryState(e.State), Created: e.Created, Updated: e.Updated,
	}
}

func (s sentDTO) domain() domain.Sent {
	return domain.Sent{At: s.At, Alias: domain.Alias(s.Alias), ThreadID: s.ThreadID, MessageID: s.MessageID, Key: s.Key}
}

func corruptLedger(detail string) error {
	return domain.NewConflict("idempotency ledger is unreadable ("+detail+"): sends are blocked",
		"inspect or remove "+LedgerFile+" in the state directory; removing it forgets sent history and may allow duplicate sends")
}

// loadLedger reads the ledger. Corruption fails closed: the file is left in
// place and every caller gets exit 7 until an operator removes it.
func (s *Store) loadLedger() (*ledgerFile, error) {
	var lf ledgerFile
	exists, err := readJSON(s.path(LedgerFile), &lf)
	switch {
	case errors.Is(err, errCorrupt):
		return nil, corruptLedger("invalid JSON")
	case err != nil:
		return nil, err
	case !exists:
		return &ledgerFile{Version: formatVersion}, nil
	case lf.Version != formatVersion:
		return nil, corruptLedger("unsupported version")
	}
	if lf.Entries == nil {
		lf.Entries = map[string]entryDTO{}
	}
	if lf.Threads == nil {
		lf.Threads = map[string]time.Time{}
	}
	return &lf, nil
}

func (lf *ledgerFile) normalize() {
	if lf.Entries == nil {
		lf.Entries = map[string]entryDTO{}
	}
	if lf.Threads == nil {
		lf.Threads = map[string]time.Time{}
	}
}

// prune drops sent/failed entries, sent history and thread records older than
// the retention. Pending entries never expire.
func (lf *ledgerFile) prune(now time.Time) {
	cut := now.Add(-LedgerRetention)
	for k, e := range lf.Entries {
		// Pending entries never expire, except the slot of an unkeyed send:
		// it has no key to retry and only ever counted toward the windows.
		if (e.State != string(domain.StatePending) || strings.HasPrefix(k, slotPrefix)) && e.Updated.Before(cut) {
			delete(lf.Entries, k)
		}
	}
	kept := lf.Sent[:0]
	for _, s := range lf.Sent {
		if !s.At.Before(cut) {
			kept = append(kept, s)
		}
	}
	lf.Sent = kept
	for t, at := range lf.Threads {
		if at.Before(cut) {
			delete(lf.Threads, t)
		}
	}
}

// mutateLedger loads, applies fn and writes the ledger back under the lock.
// fn returns changed=false to skip the write.
func (s *Store) mutateLedger(now time.Time, fn func(*ledgerFile) (changed bool, err error)) error {
	return s.locked(func() error {
		lf, err := s.loadLedger()
		if err != nil {
			return err
		}
		lf.normalize()
		changed, err := fn(lf)
		if err != nil || !changed {
			return err
		}
		lf.prune(now)
		return writeJSON(s.path(LedgerFile), lf)
	})
}

func (s *Store) readLedger(fn func(*ledgerFile)) error {
	return s.locked(func() error {
		lf, err := s.loadLedger()
		if err != nil {
			return err
		}
		fn(lf)
		return nil
	})
}

// Reserve claims an idempotency key before posting.
//
//	unknown key                         -> ReserveNew (entry written as pending)
//	same key, same hash, sent           -> ReserveReplay with the recorded result
//	same key, same hash, failed         -> ReserveRetry (entry back to pending)
//	same key, same hash, pending        -> exit 7 (ambiguous earlier attempt)
//	same key, different hash (any state)-> exit 7
func (s *Store) Reserve(_ context.Context, key, payloadHash string, dest domain.Alias, thread string, now time.Time) (domain.Reservation, error) {
	var res domain.Reservation
	err := s.mutateLedger(now, func(lf *ledgerFile) (bool, error) {
		e, ok := lf.Entries[key]
		if !ok {
			e = entryDTO{Key: key, PayloadHash: payloadHash, Alias: string(dest), ThreadID: thread,
				State: string(domain.StatePending), Created: now, Updated: now}
			lf.Entries[key] = e
			res = domain.Reservation{Outcome: domain.ReserveNew, Entry: e.domain()}
			return true, nil
		}
		if e.PayloadHash != payloadHash {
			return false, domain.NewConflict("idempotency key "+key+" was used with a different payload",
				"use a new --idempotency-key for a different message")
		}
		switch domain.EntryState(e.State) {
		case domain.StateSent:
			res = domain.Reservation{Outcome: domain.ReserveReplay, Entry: e.domain()}
			return false, nil
		case domain.StateFailed:
			e.State, e.Updated = string(domain.StatePending), now
			lf.Entries[key] = e
			res = domain.Reservation{Outcome: domain.ReserveRetry, Entry: e.domain()}
			return true, nil
		default:
			return false, domain.NewConflict("idempotency key "+key+" is pending: an earlier attempt may or may not have been delivered",
				"check the destination for the message, then use a new --idempotency-key (or enable send.marker_scan)")
		}
	})
	if err != nil {
		return domain.Reservation{}, err
	}
	return res, nil
}

// slotPrefix marks the synthetic key of an unkeyed send's reservation.
const slotPrefix = "~slot:"

func newSlot() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return slotPrefix + hex.EncodeToString(b[:])
}

// Claim checks the limits and reserves in one locked operation (FR-R4). A
// keyed claim replays or conflicts exactly like Reserve (without limit checks
// for a replay); a new, retried or unkeyed claim is denied, with nothing
// written, when a rate window or the reply depth is full. Pending
// reservations count toward the windows.
func (s *Store) Claim(_ context.Context, c domain.SendClaim) (domain.Claim, error) {
	var out domain.Claim
	err := s.mutateLedger(c.Now, func(lf *ledgerFile) (bool, error) {
		key := c.Key
		var prior entryDTO
		exists := false
		if key != "" {
			prior, exists = lf.Entries[key]
			if exists {
				if prior.PayloadHash != c.PayloadHash {
					return false, domain.NewConflict("idempotency key "+key+" was used with a different payload",
						"use a new --idempotency-key for a different message")
				}
				switch domain.EntryState(prior.State) {
				case domain.StateSent:
					out.Reservation = domain.Reservation{Outcome: domain.ReserveReplay, Entry: prior.domain()}
					out.Decision = domain.Decision{Allowed: true}
					return false, nil
				case domain.StatePending:
					return false, domain.NewConflict("idempotency key "+key+" is pending: an earlier attempt may or may not have been delivered",
						"check the destination for the message, then use a new --idempotency-key (or enable send.marker_scan)")
				}
			}
		}
		if d := c.Check(lf.history(c.HistorySince())); !d.Allowed {
			out.Decision = d
			return false, nil
		}
		outcome := domain.ReserveNew
		if exists { // failed entry: back to pending
			outcome = domain.ReserveRetry
		}
		if key == "" {
			key = newSlot()
			out.Slot = key
		}
		e := entryDTO{Key: key, PayloadHash: c.PayloadHash, Alias: string(c.Alias), ThreadID: c.ThreadID,
			State: string(domain.StatePending), Created: c.Now, Updated: c.Now}
		lf.Entries[key] = e
		out.Decision = domain.Decision{Allowed: true}
		out.Reservation = domain.Reservation{Outcome: outcome, Entry: e.domain()}
		return true, nil
	})
	if err != nil {
		return domain.Claim{}, err
	}
	return out, nil
}

// Complete records the posted message id, marks the entry sent and adds it to
// the sent history. Completing a sent entry again is a no-op.
func (s *Store) Complete(_ context.Context, key, msgID string, now time.Time) error {
	return s.mutateLedger(now, func(lf *ledgerFile) (bool, error) {
		e, ok := lf.Entries[key]
		if !ok {
			return false, domain.NewNotFound("idempotency key "+key+" was never reserved", "")
		}
		switch domain.EntryState(e.State) {
		case domain.StateSent:
			return false, nil
		case domain.StateFailed:
			return false, domain.NewConflict("idempotency key "+key+" is marked failed and cannot be completed", "reserve it again first")
		}
		e.State, e.MessageID, e.Updated = string(domain.StateSent), msgID, now
		lf.Entries[key] = e
		lf.addSent(sentDTO{At: now, Alias: e.Alias, ThreadID: e.ThreadID, MessageID: msgID, Key: key})
		return true, nil
	})
}

// Fail records a failed post. notSent=true means the server provably did not
// process it: the entry becomes failed and may be retried. notSent=false is an
// ambiguous failure: the entry stays pending (exit 7 on the next attempt).
func (s *Store) Fail(_ context.Context, key string, notSent bool) error {
	now := s.clock.Now()
	return s.mutateLedger(now, func(lf *ledgerFile) (bool, error) {
		e, ok := lf.Entries[key]
		if !ok {
			return false, domain.NewNotFound("idempotency key "+key+" was never reserved", "")
		}
		if !notSent || domain.EntryState(e.State) != domain.StatePending {
			return false, nil
		}
		e.State, e.Updated = string(domain.StateFailed), now
		lf.Entries[key] = e
		return true, nil
	})
}

// addSent appends s unless an entry with the same non-empty key exists.
func (lf *ledgerFile) addSent(s sentDTO) {
	if s.Key != "" {
		for _, x := range lf.Sent {
			if x.Key == s.Key {
				return
			}
		}
	}
	lf.Sent = append(lf.Sent, s)
}

// history returns sent history plus pending reservations (an ambiguous send
// may have happened, so it counts toward rate and loop guards), oldest first,
// restricted to At >= since.
func (lf *ledgerFile) history(since time.Time) []domain.Sent {
	var out []domain.Sent
	sentKeys := map[string]bool{}
	for _, x := range lf.Sent {
		if x.Key != "" {
			sentKeys[x.Key] = true
		}
		if !x.At.Before(since) {
			out = append(out, x.domain())
		}
	}
	for _, e := range lf.Entries {
		if domain.EntryState(e.State) == domain.StatePending && !sentKeys[e.Key] && !e.Created.Before(since) {
			out = append(out, domain.Sent{At: e.Created, Alias: domain.Alias(e.Alias), ThreadID: e.ThreadID, Key: e.Key})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].At.Equal(out[j].At) {
			return out[i].At.Before(out[j].At)
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// SentSince returns the sent history (including keyless sends and pending
// reservations) at or after since, oldest first.
func (s *Store) SentSince(_ context.Context, since time.Time) ([]domain.Sent, error) {
	var out []domain.Sent
	err := s.readLedger(func(lf *ledgerFile) { out = lf.history(since) })
	return out, err
}

// SentInThread counts history entries for thread at or after since.
func (s *Store) SentInThread(_ context.Context, thread string, since time.Time) (int, error) {
	n := 0
	err := s.readLedger(func(lf *ledgerFile) {
		for _, h := range lf.history(since) {
			if h.ThreadID == thread {
				n++
			}
		}
	})
	return n, err
}

// RecordSent appends a send to the history (keyless sends; keyed sends are
// recorded by Complete and a repeat with the same key is ignored).
func (s *Store) RecordSent(_ context.Context, x domain.Sent) error {
	now := s.clock.Now()
	if x.At.IsZero() {
		x.At = now
	}
	return s.mutateLedger(now, func(lf *ledgerFile) (bool, error) {
		before := len(lf.Sent)
		lf.addSent(sentDTO{At: x.At, Alias: string(x.Alias), ThreadID: x.ThreadID, MessageID: x.MessageID, Key: x.Key})
		return len(lf.Sent) != before, nil
	})
}

// PutThread records that the agent posted in thread, now.
func (s *Store) PutThread(_ context.Context, thread string) error {
	now := s.clock.Now()
	return s.mutateLedger(now, func(lf *ledgerFile) (bool, error) {
		lf.Threads[thread] = now
		return true, nil
	})
}

// ActiveThreads returns up to max thread ids the agent posted in at or after
// since, most recent first (max <= 0 means no limit).
func (s *Store) ActiveThreads(_ context.Context, since time.Time, max int) ([]string, error) {
	var out []string
	err := s.readLedger(func(lf *ledgerFile) {
		type tt struct {
			id string
			at time.Time
		}
		var ts []tt
		for id, at := range lf.Threads {
			if !at.Before(since) {
				ts = append(ts, tt{id, at})
			}
		}
		sort.Slice(ts, func(i, j int) bool {
			if !ts[i].at.Equal(ts[j].at) {
				return ts[i].at.After(ts[j].at)
			}
			return ts[i].id < ts[j].id
		})
		for _, t := range ts {
			if max > 0 && len(out) >= max {
				break
			}
			out = append(out, t.id)
		}
	})
	return out, err
}
