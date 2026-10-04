//go:build unix

package state

import (
	"errors"
	"os"
	"syscall"
	"time"
)

var errBusy = errors.New("state lock busy")

// acquire takes an exclusive advisory flock on path (created 0600), polling
// until timeout. The returned release function unlocks and closes. flock is
// per open file description, so two goroutines or two processes exclude each
// other.
func acquire(path string, timeout time.Duration) (release func(), err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, fileMode)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(timeout)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() {
				_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
				_ = f.Close()
			}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			_ = f.Close()
			return nil, err
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, errBusy
		}
		time.Sleep(10 * time.Millisecond)
	}
}
