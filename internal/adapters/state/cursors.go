package state

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/stainedhead/teams-cli/internal/domain"
)

type cursorsFile struct {
	Version      int                  `json:"version"`
	Destinations map[string]cursorDTO `json:"destinations"`
}

type cursorDTO struct {
	Watermark  time.Time     `json:"watermark,omitzero"`
	Acked      []ackDTO      `json:"acked,omitempty"`
	Delivered  []deliveryDTO `json:"delivered,omitempty"`
	DeltaToken string        `json:"delta_token,omitempty"`
	ChatID     string        `json:"chat_id,omitempty"`
	Updated    time.Time     `json:"updated,omitzero"`
}

type ackDTO struct {
	ID       string    `json:"id"`
	Modified time.Time `json:"modified"`
}

type deliveryDTO struct {
	ID          string    `json:"id"`
	ThreadID    string    `json:"thread_id,omitempty"`
	Modified    time.Time `json:"modified"`
	DeliveredAt time.Time `json:"delivered_at"`
}

func (c cursorDTO) domain() domain.CursorState {
	cs := domain.CursorState{Watermark: c.Watermark, DeltaToken: c.DeltaToken, ChatID: c.ChatID, Updated: c.Updated}
	for _, a := range c.Acked {
		cs.Acked = append(cs.Acked, domain.AckEntry{ID: a.ID, Modified: a.Modified})
	}
	for _, d := range c.Delivered {
		cs.Delivered = append(cs.Delivered, domain.DeliveryEntry{ID: d.ID, ThreadID: d.ThreadID, Modified: d.Modified, DeliveredAt: d.DeliveredAt})
	}
	return cs
}

func toCursorDTO(cs domain.CursorState) cursorDTO {
	c := cursorDTO{Watermark: cs.Watermark, DeltaToken: cs.DeltaToken, ChatID: cs.ChatID, Updated: cs.Updated}
	for _, a := range cs.Acked {
		c.Acked = append(c.Acked, ackDTO{ID: a.ID, Modified: a.Modified})
	}
	for _, d := range cs.Delivered {
		c.Delivered = append(c.Delivered, deliveryDTO{ID: d.ID, ThreadID: d.ThreadID, Modified: d.Modified, DeliveredAt: d.DeliveredAt})
	}
	return c
}

// algebra is the seam to the domain cursor algebra (D8). Production uses the
// domain methods; tests of the store's own responsibilities inject simple
// stand-ins so they do not depend on algebra behavior owned by the domain.
type algebra struct {
	record func(cs domain.CursorState, d []domain.DeliveryEntry, now time.Time, keep time.Duration) domain.CursorState
	ack    func(cs domain.CursorState, entries []domain.AckEntry) (domain.CursorState, int, int)
}

var domainAlgebra = algebra{
	record: func(cs domain.CursorState, d []domain.DeliveryEntry, now time.Time, keep time.Duration) domain.CursorState {
		return cs.Record(d, now, keep)
	},
	ack: func(cs domain.CursorState, entries []domain.AckEntry) (domain.CursorState, int, int) {
		return cs.Ack(entries)
	},
}

// loadCursors reads the cursor file. Corruption fails open: the file is
// quarantined and an empty set returned, so the next poll re-reads at most
// max_lookback (acked messages may be re-delivered once; D7).
func (s *Store) loadCursors(now time.Time) (*cursorsFile, error) {
	var cf cursorsFile
	exists, err := readJSON(s.path(CursorsFile), &cf)
	switch {
	case errors.Is(err, errCorrupt) || (err == nil && exists && cf.Version != formatVersion):
		if _, qerr := quarantine(s.path(CursorsFile), now); qerr != nil {
			return nil, qerr
		}
		return &cursorsFile{Version: formatVersion, Destinations: map[string]cursorDTO{}}, nil
	case err != nil:
		return nil, err
	}
	cf.Version = formatVersion
	if cf.Destinations == nil {
		cf.Destinations = map[string]cursorDTO{}
	}
	return &cf, nil
}

func (s *Store) mutateCursors(now time.Time, fn func(*cursorsFile) (changed bool, err error)) error {
	return s.locked(func() error {
		cf, err := s.loadCursors(now)
		if err != nil {
			return err
		}
		changed, err := fn(cf)
		if err != nil || !changed {
			return err
		}
		return writeJSON(s.path(CursorsFile), cf)
	})
}

// Get returns the cursor state for alias (zero if none).
func (s *Store) Get(_ context.Context, alias domain.Alias) (domain.CursorState, error) {
	var cs domain.CursorState
	err := s.locked(func() error {
		cf, err := s.loadCursors(s.clock.Now())
		if err != nil {
			return err
		}
		cs = cf.Destinations[string(alias)].domain()
		return nil
	})
	return cs, err
}

// pruneAcked drops acked entries that are both behind the watermark and older
// than the retention; the watermark already hides them.
func (s *Store) pruneAcked(cs domain.CursorState, now time.Time) domain.CursorState {
	cut := now.Add(-s.keep)
	var kept []domain.AckEntry
	for _, a := range cs.Acked {
		if !a.Modified.After(cs.Watermark) && a.Modified.Before(cut) {
			continue
		}
		kept = append(kept, a)
	}
	cs.Acked = kept
	return cs
}

// RecordDeliveries adds items to the delivery index (so ack can validate them).
func (s *Store) RecordDeliveries(_ context.Context, alias domain.Alias, d []domain.DeliveryEntry, now time.Time) error {
	if len(d) == 0 {
		return nil
	}
	return s.mutateCursors(now, func(cf *cursorsFile) (bool, error) {
		cs := s.alg.record(cf.Destinations[string(alias)].domain(), d, now, s.keep)
		cs = s.pruneAcked(cs, now)
		cs.Updated = now
		cf.Destinations[string(alias)] = toCursorDTO(cs)
		return true, nil
	})
}

// Ack marks delivered ids as acked. ids are Graph message ids; a CLI id
// ("<alias>/<id>") for the same alias is accepted too. An id missing from the
// delivery index is returned in unknown and nothing at all changes.
func (s *Store) Ack(_ context.Context, alias domain.Alias, ids []string, now time.Time) (acked, already int, unknown []string, err error) {
	prefix := string(alias) + "/"
	err = s.mutateCursors(now, func(cf *cursorsFile) (bool, error) {
		cs := cf.Destinations[string(alias)].domain()
		var entries []domain.AckEntry
		seen := map[string]bool{}
		for _, raw := range ids {
			id := strings.TrimPrefix(raw, prefix)
			if seen[id] {
				continue
			}
			seen[id] = true
			mod, ok := deliveredVersion(cs, id)
			if !ok {
				unknown = append(unknown, raw)
				continue
			}
			entries = append(entries, domain.AckEntry{ID: id, Modified: mod})
		}
		if len(unknown) > 0 {
			return false, nil
		}
		var next domain.CursorState
		next, acked, already = s.alg.ack(cs, entries)
		next = s.pruneAcked(next, now)
		next.Updated = now
		cf.Destinations[string(alias)] = toCursorDTO(next)
		return true, nil
	})
	if err != nil || len(unknown) > 0 {
		return 0, 0, unknown, err
	}
	return acked, already, nil, nil
}

// deliveredVersion finds the newest delivered version of id in the index.
func deliveredVersion(cs domain.CursorState, id string) (time.Time, bool) {
	var best time.Time
	found := false
	for _, d := range cs.Delivered {
		if d.ID == id && (!found || d.Modified.After(best)) {
			best, found = d.Modified, true
		}
	}
	return best, found
}

// StoreDelta saves the channel delta link for alias.
func (s *Store) StoreDelta(_ context.Context, alias domain.Alias, token string) error {
	return s.setCursor(alias, func(cs *domain.CursorState) { cs.DeltaToken = token })
}

// CacheChat saves the resolved 1:1 chat id for a user alias (D6).
func (s *Store) CacheChat(_ context.Context, alias domain.Alias, chatID string) error {
	return s.setCursor(alias, func(cs *domain.CursorState) { cs.ChatID = chatID })
}

// DropChat forgets the cached chat id (for example after a 404).
func (s *Store) DropChat(_ context.Context, alias domain.Alias) error {
	return s.setCursor(alias, func(cs *domain.CursorState) { cs.ChatID = "" })
}

// ResolveChat returns the cached chat id. Any read problem reports a miss
// (fail open: the caller re-resolves through Graph).
func (s *Store) ResolveChat(ctx context.Context, alias domain.Alias) (string, bool) {
	cs, err := s.Get(ctx, alias)
	if err != nil || cs.ChatID == "" {
		return "", false
	}
	return cs.ChatID, true
}

func (s *Store) setCursor(alias domain.Alias, fn func(*domain.CursorState)) error {
	now := s.clock.Now()
	return s.mutateCursors(now, func(cf *cursorsFile) (bool, error) {
		cs := cf.Destinations[string(alias)].domain()
		fn(&cs)
		cs.Updated = now
		cf.Destinations[string(alias)] = toCursorDTO(cs)
		return true, nil
	})
}
