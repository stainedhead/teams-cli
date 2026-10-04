// Package fstrust verifies that a file was provably authored by someone other
// than the agent (FR-21) and that the state directory belongs to the agent.
//
// The policy trust test is ownership, not mode bits: the agent can chmod its
// own files but cannot make a file root-owned. The file and every directory
// above it (and every symlink on the way) must be owned by root or by a
// configured uid other than the effective uid, and must not be writable by
// group or others. The content is read from the descriptor that was verified
// with fstat, so there is no check-then-read race. The package imports only
// the standard library; callers map its typed errors to exit codes.
package fstrust

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Kind is the file type as far as the trust check cares.
type Kind int

// File kinds.
const (
	KindOther Kind = iota
	KindRegular
	KindDir
	KindSymlink
)

// Info is the part of a stat result the trust check needs.
type Info struct {
	UID  uint32
	Perm fs.FileMode // permission bits only
	Kind Kind
}

// UntrustedError reports a trust violation (wrong owner, writable, symlink,
// not a regular file, or a path that could not be checked).
type UntrustedError struct {
	Path   string
	Reason string
	Cause  error
}

// Error describes the violation.
func (e *UntrustedError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Path, e.Reason, e.Cause)
	}
	return e.Path + ": " + e.Reason
}

// Unwrap returns the underlying cause, if any.
func (e *UntrustedError) Unwrap() error { return e.Cause }

// ReadError reports that the file could not be opened (missing, a symlink in
// the last component, permission). It is not a trust verdict.
type ReadError struct {
	Path string
	Err  error
}

// Error describes the failure.
func (e *ReadError) Error() string { return "cannot read " + e.Path + ": " + e.Err.Error() }

// Unwrap returns the underlying error.
func (e *ReadError) Unwrap() error { return e.Err }

// Checker performs the checks. Build one with New.
type Checker struct {
	lstat   func(string) (Info, error)
	fstat   func(*os.File) (Info, error)
	open    func(string) (*os.File, error)
	euid    uint32
	trusted map[uint32]bool
}

// Option configures a Checker.
type Option func(*Checker)

// WithTrustedUIDs additionally trusts files owned by the given uids (root is
// always trusted). The effective uid is never trusted.
func WithTrustedUIDs(uids ...uint32) Option {
	return func(c *Checker) {
		for _, u := range uids {
			c.trusted[u] = true
		}
	}
}

// WithStat injects the stat functions and the effective uid (tests).
func WithStat(lstat func(string) (Info, error), fstat func(*os.File) (Info, error), euid uint32) Option {
	return func(c *Checker) { c.lstat, c.fstat, c.euid = lstat, fstat, euid }
}

// New returns a Checker using the real file system.
func New(opts ...Option) *Checker {
	c := &Checker{
		lstat:   realLstat,
		fstat:   realFstat,
		open:    openNoFollow,
		euid:    realEUID(),
		trusted: map[uint32]bool{},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

func (c *Checker) isTrusted(uid uint32) bool {
	if uid == c.euid {
		return false // the agent's own uid never vouches for itself
	}
	return uid == 0 || c.trusted[uid]
}

func (c *Checker) checkInfo(what, path string, si Info) error {
	if !c.isTrusted(si.UID) {
		return &UntrustedError{Path: path, Reason: what + " is not owned by a trusted account (root)"}
	}
	// A symlink's own mode is meaningless on most systems; its owner is what
	// decides who could repoint it.
	if si.Kind != KindSymlink && si.Perm&0o022 != 0 {
		return &UntrustedError{Path: path, Reason: what + " is writable by group or others"}
	}
	return nil
}

// ancestors lists p and every parent directory up to the root, root first.
func ancestors(p string) []string {
	var out []string
	for {
		out = append([]string{p}, out...)
		parent := filepath.Dir(p)
		if parent == p {
			return out
		}
		p = parent
	}
}

func (c *Checker) checkChain(path string, seen map[string]bool) error {
	for _, p := range ancestors(path) {
		if seen[p] {
			continue
		}
		seen[p] = true
		si, err := c.lstat(p)
		if err != nil {
			return &UntrustedError{Path: p, Reason: "cannot check ownership", Cause: err}
		}
		what := "directory"
		switch {
		case p == path && si.Kind != KindDir:
			what = "file"
		case si.Kind == KindSymlink:
			what = "symlink"
		}
		if err := c.checkInfo(what, p, si); err != nil {
			return err
		}
	}
	return nil
}

// OpenTrusted checks the lexical path and its symlink-resolved target, then
// opens the target with O_NOFOLLOW and verifies the open descriptor. The
// caller reads from, and closes, the returned file.
func (c *Checker) OpenTrusted(path string) (*os.File, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, &UntrustedError{Path: path, Reason: "cannot resolve path", Cause: err}
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, &ReadError{Path: path, Err: err}
	}
	seen := map[string]bool{}
	if err := c.checkChain(abs, seen); err != nil {
		return nil, err
	}
	if err := c.checkChain(resolved, seen); err != nil {
		return nil, err
	}
	f, err := c.open(resolved)
	if err != nil {
		return nil, &ReadError{Path: path, Err: err}
	}
	si, err := c.fstat(f)
	if err != nil {
		_ = f.Close()
		return nil, &UntrustedError{Path: resolved, Reason: "cannot check ownership", Cause: err}
	}
	if si.Kind != KindRegular {
		_ = f.Close()
		return nil, &UntrustedError{Path: resolved, Reason: "not a regular file"}
	}
	if err := c.checkInfo("file", resolved, si); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

// CheckOwnedDir verifies a state directory: a real directory (not a symlink),
// owned by the effective uid, and not writable by group or others.
func (c *Checker) CheckOwnedDir(path string) error {
	si, err := c.lstat(path)
	if err != nil {
		return &UntrustedError{Path: path, Reason: "cannot check directory", Cause: err}
	}
	switch {
	case si.Kind == KindSymlink:
		return &UntrustedError{Path: path, Reason: "directory is a symlink"}
	case si.Kind != KindDir:
		return &UntrustedError{Path: path, Reason: "not a directory"}
	case si.UID != c.euid:
		return &UntrustedError{Path: path, Reason: "directory is not owned by the current user"}
	case si.Perm&0o022 != 0:
		return &UntrustedError{Path: path, Reason: "directory is writable by group or others"}
	}
	return nil
}
