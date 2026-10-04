//go:build !unix

package state

import (
	"errors"
	"time"
)

var errBusy = errors.New("state lock busy")

// acquire is unsupported off unix: the state store requires advisory locks.
func acquire(string, time.Duration) (func(), error) {
	return nil, errors.New("state locking is not supported on this platform")
}
