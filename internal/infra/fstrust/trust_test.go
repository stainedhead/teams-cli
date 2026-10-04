package fstrust

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// fakeFS maps absolute paths to Info; unlisted paths do not exist.
type fakeFS map[string]Info

func (f fakeFS) lstat(p string) (Info, error) {
	if si, ok := f[p]; ok {
		return si, nil
	}
	return Info{}, fs.ErrNotExist
}

// newChecker builds a checker whose lstat reads fakeFS and whose fstat reports
// the entry for the opened file's name.
func newChecker(f fakeFS, euid uint32, opts ...Option) *Checker {
	fstat := func(file *os.File) (Info, error) { return f[file.Name()], nil }
	return New(append([]Option{WithStat(f.lstat, fstat, euid)}, opts...)...)
}

// tree creates a real file under a temp dir and returns the fake metadata for
// every component, all root-owned and 0755/0644 by default.
func tree(t *testing.T) (path string, fsys fakeFS) {
	t.Helper()
	dir := t.TempDir()
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(dir, "teams.policy.yaml")
	if err := os.WriteFile(path, []byte("version: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fsys = fakeFS{path: {UID: 0, Perm: 0o644, Kind: KindRegular}}
	for _, a := range ancestors(dir) {
		fsys[a] = Info{UID: 0, Perm: 0o755, Kind: KindDir}
	}
	return path, fsys
}

func TestRootOwnedPasses(t *testing.T) {
	path, fsys := tree(t)
	f, err := newChecker(fsys, 1000).OpenTrusted(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
}

func TestTrustedUIDPasses(t *testing.T) {
	path, fsys := tree(t)
	si := fsys[path]
	si.UID = 4242
	fsys[path] = si
	f, err := newChecker(fsys, 1000, WithTrustedUIDs(4242)).OpenTrusted(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
}

func TestViolations(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(path string, f fakeFS)
		euid   uint32
		opts   []Option
	}{
		{"file group-writable", func(p string, f fakeFS) { f[p] = Info{0, 0o664, KindRegular} }, 1000, nil},
		{"file world-writable", func(p string, f fakeFS) { f[p] = Info{0, 0o646, KindRegular} }, 1000, nil},
		{"file agent-owned", func(p string, f fakeFS) { f[p] = Info{1000, 0o644, KindRegular} }, 1000, nil},
		{"file owned by untrusted other", func(p string, f fakeFS) { f[p] = Info{2000, 0o644, KindRegular} }, 1000, nil},
		{"trusted uid equals euid", func(p string, f fakeFS) { f[p] = Info{1000, 0o644, KindRegular} }, 1000, []Option{WithTrustedUIDs(1000)}},
		{"root euid never trusted", func(p string, f fakeFS) {}, 0, nil},
		{"parent dir agent-owned", func(p string, f fakeFS) { f[filepath.Dir(p)] = Info{1000, 0o755, KindDir} }, 1000, nil},
		{"parent dir group-writable", func(p string, f fakeFS) { f[filepath.Dir(p)] = Info{0, 0o775, KindDir} }, 1000, nil},
		{"grandparent world-writable", func(p string, f fakeFS) {
			f[filepath.Dir(filepath.Dir(p))] = Info{0, 0o757, KindDir}
		}, 1000, nil},
		{"not a regular file", func(p string, f fakeFS) { f[p] = Info{0, 0o644, KindOther} }, 1000, nil},
		{"lstat missing ancestor", func(p string, f fakeFS) { delete(f, filepath.Dir(p)) }, 1000, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path, fsys := tree(t)
			tc.mutate(path, fsys)
			f, err := newChecker(fsys, tc.euid, tc.opts...).OpenTrusted(path)
			if err == nil {
				_ = f.Close()
				t.Fatal("want an UntrustedError")
			}
			var ue *UntrustedError
			if !errors.As(err, &ue) {
				t.Fatalf("want UntrustedError, got %T: %v", err, err)
			}
			if ue.Error() == "" {
				t.Fatal("empty message")
			}
		})
	}
}

func TestFstatDisagreementDenied(t *testing.T) {
	// lstat says root-owned, but the opened descriptor is agent-owned (a swap
	// between check and open): the fstat verdict must win.
	path, fsys := tree(t)
	fstat := func(*os.File) (Info, error) { return Info{1000, 0o644, KindRegular}, nil }
	c := New(WithStat(fsys.lstat, fstat, 1000))
	if _, err := c.OpenTrusted(path); err == nil {
		t.Fatal("want denial")
	}
	failing := func(*os.File) (Info, error) { return Info{}, errors.New("boom") }
	c = New(WithStat(fsys.lstat, failing, 1000))
	var ue *UntrustedError
	_, err := c.OpenTrusted(path)
	if !errors.As(err, &ue) || ue.Unwrap() == nil {
		t.Fatalf("want UntrustedError with cause, got %v", err)
	}
}

func TestSymlinkOwnedByAgentDenied(t *testing.T) {
	dir := t.TempDir()
	dir, _ = filepath.EvalSymlinks(dir)
	target := filepath.Join(dir, "real.yaml")
	link := filepath.Join(dir, "link.yaml")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlinks unsupported")
	}
	fsys := fakeFS{
		target: {0, 0o644, KindRegular},
		link:   {1000, 0o777, KindSymlink},
	}
	for _, a := range ancestors(dir) {
		fsys[a] = Info{0, 0o755, KindDir}
	}
	_, err := newChecker(fsys, 1000).OpenTrusted(link)
	var ue *UntrustedError
	if !errors.As(err, &ue) {
		t.Fatalf("want UntrustedError for agent-owned symlink, got %v", err)
	}
	// A root-owned symlink to a root-owned file passes.
	fsys[link] = Info{0, 0o777, KindSymlink}
	f, err := newChecker(fsys, 1000).OpenTrusted(link)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
}

func TestMissingFileIsReadError(t *testing.T) {
	_, err := New().OpenTrusted(filepath.Join(t.TempDir(), "nope.yaml"))
	var re *ReadError
	if !errors.As(err, &re) || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("want ReadError wrapping ErrNotExist, got %v", err)
	}
	if re.Error() == "" {
		t.Fatal("empty message")
	}
}

func TestOpenFailureIsReadError(t *testing.T) {
	path, fsys := tree(t)
	c := newChecker(fsys, 1000)
	c.open = func(string) (*os.File, error) { return nil, errors.New("denied") }
	var re *ReadError
	if _, err := c.OpenTrusted(path); !errors.As(err, &re) {
		t.Fatalf("got %v", err)
	}
}

func TestRealTempFileIsAgentOwned(t *testing.T) {
	// Real stat: a file the test process just created is owned by the current
	// user, so the real check must refuse it. Skipped when running as root.
	if os.Geteuid() == 0 {
		t.Skip("running as root")
	}
	path, _ := tree(t)
	_, err := New().OpenTrusted(path)
	var ue *UntrustedError
	if !errors.As(err, &ue) {
		t.Fatalf("want UntrustedError, got %v", err)
	}
}

func TestOpenNoFollowRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "t")
	link := filepath.Join(dir, "l")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlinks unsupported")
	}
	if f, err := openNoFollow(link); err == nil {
		_ = f.Close()
		t.Fatal("O_NOFOLLOW must refuse a symlink")
	}
	f, err := openNoFollow(target)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
}

func TestCheckOwnedDir(t *testing.T) {
	d := "/var/lib/agent-cli/teams"
	tests := []struct {
		name string
		info Info
		err  bool
	}{
		{"ok", Info{1000, 0o700, KindDir}, false},
		{"ok 0755", Info{1000, 0o755, KindDir}, false},
		{"other owner", Info{0, 0o700, KindDir}, true},
		{"group writable", Info{1000, 0o770, KindDir}, true},
		{"world writable", Info{1000, 0o707, KindDir}, true},
		{"symlink", Info{1000, 0o700, KindSymlink}, true},
		{"file", Info{1000, 0o600, KindRegular}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := newChecker(fakeFS{d: tc.info}, 1000)
			err := c.CheckOwnedDir(d)
			if (err != nil) != tc.err {
				t.Fatalf("err = %v", err)
			}
		})
	}
	if err := newChecker(fakeFS{}, 1000).CheckOwnedDir(d); err == nil {
		t.Fatal("missing dir must fail")
	}
	// Real dir created by this process passes.
	if err := New().CheckOwnedDir(t.TempDir()); err != nil {
		t.Fatalf("own temp dir: %v", err)
	}
}
