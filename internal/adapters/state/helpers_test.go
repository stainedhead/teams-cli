package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stainedhead/teams-cli/internal/domain"
	"github.com/stainedhead/teams-cli/internal/infra/clock"
)

var t0 = time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

func newStore(t *testing.T, opts ...Option) (*Store, *clock.Fake) {
	t.Helper()
	fc := clock.NewFake(t0)
	s, err := Open(filepath.Join(t.TempDir(), "state"), fc, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return s, fc
}

// reopen opens a second Store on the same directory (a new "process").
func reopen(t *testing.T, s *Store, fc *clock.Fake, opts ...Option) *Store {
	t.Helper()
	n, err := Open(s.Dir(), fc, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// standIn is a minimal cursor algebra used to test the store's own
// responsibilities (locking, persistence, id validation, pruning) without
// depending on algebra behavior owned by the domain package.
var standIn = algebra{
	record: func(cs domain.CursorState, d []domain.DeliveryEntry, now time.Time, keep time.Duration) domain.CursorState {
		for _, e := range d {
			replaced := false
			for i := range cs.Delivered {
				if cs.Delivered[i].ID == e.ID {
					cs.Delivered[i] = e
					replaced = true
				}
			}
			if !replaced {
				cs.Delivered = append(cs.Delivered, e)
			}
		}
		return cs
	},
	ack: func(cs domain.CursorState, entries []domain.AckEntry) (domain.CursorState, int, int) {
		acked, already := 0, 0
		for _, e := range entries {
			dup := false
			for _, a := range cs.Acked {
				if a.ID == e.ID && !e.Modified.After(a.Modified) {
					dup = true
				}
			}
			if dup {
				already++
				continue
			}
			cs.Acked = append(cs.Acked, e)
			if e.Modified.After(cs.Watermark) {
				cs.Watermark = e.Modified
			}
			acked++
		}
		return cs, acked, already
	},
}

func withStandIn() Option { return func(s *Store) { s.alg = standIn } }
