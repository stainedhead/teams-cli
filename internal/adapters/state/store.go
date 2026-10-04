package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/stainedhead/teams-cli/internal/domain"
	"github.com/stainedhead/teams-cli/internal/infra/fstrust"
	"github.com/stainedhead/teams-cli/internal/usecase"
)

// File names inside the state directory.
const (
	LedgerFile  = "teams.ledger.json"
	CursorsFile = "teams.cursors.json"
	LockFile    = "teams.lock"
)

// Defaults.
const (
	DefaultLockTimeout     = 10 * time.Second
	DefaultCursorRetention = 25 * time.Hour // max_lookback cap (24h) plus one hour
	LedgerRetention        = 90 * 24 * time.Hour
	formatVersion          = 1
)

// Store implements usecase.Ledger and usecase.CursorStore over one state
// directory. Every operation takes the shared advisory lock, reads the file,
// applies the change and writes it atomically, so concurrent processes and
// goroutines serialize and never lose updates.
type Store struct {
	dir         string
	clock       usecase.Clock
	lockTimeout time.Duration
	keep        time.Duration
	alg         algebra

	mu sync.Mutex
}

var (
	_ usecase.Ledger      = (*Store)(nil)
	_ usecase.CursorStore = (*Store)(nil)
)

// Option customizes a Store.
type Option func(*Store)

// WithLockTimeout sets how long an operation waits for the state lock before
// failing with exit 7 "state busy" (default 10 s).
func WithLockTimeout(d time.Duration) Option { return func(s *Store) { s.lockTimeout = d } }

// WithCursorRetention sets how long delivery-index and acked entries are kept.
// The composition root passes policy max_lookback plus one hour.
func WithCursorRetention(d time.Duration) Option {
	return func(s *Store) {
		if d > 0 {
			s.keep = d
		}
	}
}

// Open prepares the state directory (created 0700) and returns a Store. The
// directory must be owned by the current user and not group- or
// world-writable. clock supplies "now" for operations that do not take one.
func Open(dir string, clock usecase.Clock, opts ...Option) (*Store, error) {
	if dir == "" {
		return nil, domain.NewConflict("state directory is not configured", "set state_dir in the policy or TEAMS_STATE_DIR")
	}
	s := &Store{dir: dir, clock: clock, lockTimeout: DefaultLockTimeout, keep: DefaultCursorRetention, alg: domainAlgebra}
	for _, o := range opts {
		o(s)
	}
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return nil, domain.NewConflict("state directory cannot be created: "+dir, "the state directory must be a writable persistent volume")
	}
	if err := fstrust.New().CheckOwnedDir(dir); err != nil {
		var ue *fstrust.UntrustedError
		if errors.As(err, &ue) {
			return nil, domain.NewConflict("state directory is not trusted: "+ue.Error(), "the state directory must be owned by the agent user and not writable by group or others (chmod 0700)")
		}
		return nil, err
	}
	return s, nil
}

// Dir returns the state directory.
func (s *Store) Dir() string { return s.dir }

func (s *Store) path(name string) string { return filepath.Join(s.dir, name) }

// locked runs fn holding the in-process mutex and the cross-process flock.
func (s *Store) locked(fn func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	release, err := acquire(s.path(LockFile), s.lockTimeout)
	if err != nil {
		if errors.Is(err, errBusy) {
			return domain.NewConflict("state busy: another teams process holds the state lock",
				fmt.Sprintf("retry in a moment; the lock in %s was not released within %s", s.dir, s.lockTimeout))
		}
		return fmt.Errorf("state lock: %w", err)
	}
	defer release()
	return fn()
}
