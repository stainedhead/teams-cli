package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stainedhead/agent-cli-core/output"

	"github.com/stainedhead/teams-cli/internal/infra/clock"
)

func TestOpenCreatesDir0700(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b")
	s, err := Open(dir, clock.NewFake(t0))
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(s.Dir())
	if err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("%v %v", fi, err)
	}
}

func TestOpenRejectsBadDirs(t *testing.T) {
	fc := clock.NewFake(t0)
	if _, err := Open("", fc); output.ExitOf(err) != 7 {
		t.Fatalf("empty: %v", err)
	}
	// World-writable directory.
	d := t.TempDir()
	if err := os.Chmod(d, 0o777); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(d, fc); output.ExitOf(err) != 7 || !strings.Contains(err.Error(), "not trusted") {
		t.Fatalf("world-writable: %v", err)
	}
	// Path under a regular file cannot be created.
	f := filepath.Join(t.TempDir(), "file")
	writeFile(t, f, "x")
	if _, err := Open(filepath.Join(f, "sub"), fc); output.ExitOf(err) != 7 {
		t.Fatalf("under file: %v", err)
	}
	// A symlinked state dir is refused.
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err == nil {
		if _, err := Open(link, fc); err == nil {
			t.Fatal("symlinked state dir accepted")
		}
	}
}

func TestLockFileMode(t *testing.T) {
	s, _ := newStore(t)
	_ = s.PutThread(bg, "t")
	fi, err := os.Stat(filepath.Join(s.Dir(), LockFile))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("%v %v", fi, err)
	}
}
